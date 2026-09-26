package ws

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type Event struct {
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

// Audience selects the connections an invalidation applies to. An empty Audience is
// every connection, which is what a workspace-wide change means.
type Audience struct {
	UserIDs       []string
	SessionHashes []string
}

func (a Audience) matches(client *Client) bool {
	if len(a.UserIDs) == 0 && len(a.SessionHashes) == 0 {
		return true
	}
	for _, userID := range a.UserIDs {
		if client.UserID == userID {
			return true
		}
	}
	for _, hash := range a.SessionHashes {
		if client.SessionHash == hash {
			return true
		}
	}
	return false
}

// Queued is one event waiting for a connection, together with the authorization
// generation it was queued under. The writer compares the two before it sends
// anything, so an event that was eligible when it was queued does not escape after
// the authority behind it has changed. Bytes already written cannot be recalled;
// this is the queue boundary, and it is the same boundary the file and push paths
// use.
//
// Invalidate raises the generation of every connection it matches and queues one
// refetch signal at the new generation, so what was queued under the old one is
// dropped rather than delivered.
type Queued struct {
	Generation uint64
	// Revision is the committed authorization revision the event was resolved at, or
	// zero for an event that is not scoped to an audience. It is carried with the event
	// so write admission can compare it with the revision the connection has observed:
	// a scoped event resolved before a change the connection already knows about is
	// refused at the writer, which is the explicit contract for the window between
	// dequeue and the network write.
	Revision int64
	Event    []byte
}

type Client struct {
	ID          string
	UserID      string
	SessionHash string
	Conn        *websocket.Conn
	Out         chan Queued
	generation  atomic.Uint64
	// sessionEnded is terminal for this connection. A generation change alone
	// rejects old queue entries but cannot reject a newly queued event after the
	// session's deletion has committed and before transport cleanup completes.
	sessionEnded atomic.Bool
	// authorityRevision is the committed authorization revision this connection has
	// observed: the one it connected under, or a newer one an invalidation told it
	// about. A restricted event resolved at an older revision than this is refused at
	// queue time, which is what keeps an audience computed before a revocation from
	// being delivered to a connection that has already been told its authority moved.
	authorityRevision atomic.Int64
	activeAt          time.Time
}

// Stale reports whether an event has outlived the authorization generation it was
// queued under.
func (client *Client) Stale(queued Queued) bool {
	return queued.Generation != client.generation.Load()
}

// Admitted is the write-admission decision the socket writer makes immediately before
// it sends bytes. An event is written only when the connection has not left the
// generation the event was queued under and, for an event scoped to an audience, has
// not observed a newer authorization revision than the one the audience was resolved
// at.
//
// Dequeue and the network write are two steps, and no lock is held across the write,
// so this is the last decision before bytes leave. A revocation that commits after this
// check has admitted a write under the authority that was in force when it was
// admitted; the invalidation queued with that change is what removes it from the
// client, and the change's own event is the one the connection sees instead. This
// contract is deliberately narrower than "queue filtering proves the boundary": what
// is proven here is that an event the connection has already been told is stale is
// never written, not that a change can never follow an admitted write.
func (client *Client) Admitted(queued Queued) bool {
	if client.sessionEnded.Load() || queued.Generation != client.generation.Load() {
		return false
	}
	return queued.Revision == 0 || client.AuthorityRevision() <= queued.Revision
}

// ObserveRevision records the committed authorization revision this connection has
// been told about. The value only moves forward.
func (client *Client) ObserveRevision(revision int64) {
	for {
		current := client.authorityRevision.Load()
		if revision <= current || client.authorityRevision.CompareAndSwap(current, revision) {
			return
		}
	}
}

// AuthorityRevision reports the committed authorization revision this connection has
// observed.
func (client *Client) AuthorityRevision() int64 { return client.authorityRevision.Load() }

type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
}

func New() *Hub { return &Hub{clients: make(map[*Client]struct{})} }

func (h *Hub) Add(client *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for connected := range h.clients {
		if connected.UserID == client.UserID {
			count++
		}
	}
	if count >= 5 || len(h.clients) >= 100 {
		return false
	}
	client.activeAt = time.Now()
	h.clients[client] = struct{}{}
	return true
}

func (h *Hub) Remove(client *Client) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

func (h *Hub) Activity(client *Client) {
	h.mu.Lock()
	if _, ok := h.clients[client]; ok {
		client.activeAt = time.Now()
	}
	h.mu.Unlock()
}

func encode(kind string, data any) []byte {
	payload, _ := json.Marshal(data)
	event, _ := json.Marshal(Event{Type: kind, Data: payload})
	return event
}

func (client *Client) Send(kind string, data any) { client.queue(encode(kind, data)) }

func (client *Client) queue(event []byte) { client.queueRevision(event, 0) }

// queueRevision enqueues an event together with the revision it was resolved at, so
// write admission can compare the two. revision is zero for an event that is not
// scoped to an audience.
func (client *Client) queueRevision(event []byte, revision int64) {
	if client.sessionEnded.Load() {
		return
	}
	queued := Queued{Generation: client.generation.Load(), Revision: revision, Event: event}
	select {
	case client.Out <- queued:
	default:
		// A reconnect refetches state. Never grow an unbounded slow-client queue.
		_ = client.Conn.CloseNow()
	}
}

// SessionIdentities snapshots the affected connections without holding the hub
// lock while their sessions are checked in a database transaction. The server's
// authority fence orders registration and commit, so a new connection cannot
// enter between this snapshot and the commit that uses it.
func (h *Hub) SessionIdentities(audience Audience) map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	sessions := make(map[string]string)
	for client := range h.clients {
		if audience.matches(client) {
			sessions[client.SessionHash] = client.UserID
		}
	}
	return sessions
}

// EndSessions permanently refuses admission for sessions the committing
// transaction removed or invalidated. It performs no database or network I/O;
// disconnection remains separate cleanup after the authority fence is released.
func (h *Hub) EndSessions(hashes []string) {
	ended := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		ended[hash] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if ended[client.SessionHash] {
			client.sessionEnded.Store(true)
		}
	}
}

func (h *Hub) Broadcast(kind string, data any) {
	event := encode(kind, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		client.queue(event)
	}
}

// There is deliberately no resource-scoped broadcast that omits the revision: a
// delivery decision and the audience decision it was based on must share one authority
// revision, and a variant without one would let a caller reintroduce the window where an
// audience resolved before a revocation is delivered after it.

// Invalidate raises the authorization generation of every matching connection and
// queues one refetch signal, so an event that was queued before the change is
// dropped by the writer instead of being delivered under authority that no longer
// exists. It reports how many connections were invalidated.
//
// revision is the authorization revision the change committed, and it is recorded on
// each connection it reaches. A restricted event that was resolved against an older
// revision is then refused at queue time as well as at the writer, which closes the
// window where an audience resolved before the change would otherwise be queued after
// it and delivered under the new generation.
//
// The caller's revision is a floor, never a ceiling: a connection that has already
// observed a newer revision keeps it, because ObserveRevision only moves forward. The
// refetch signal itself is therefore queued at the revision the connection actually
// ends up on, not at the one passed in, so a signal that arrives after a newer change
// is still delivered rather than refused by the very admission rule it exists to
// satisfy. It carries no resource content, so delivering it under the connection's own
// revision discloses nothing.
func (h *Hub) Invalidate(audience Audience, revision int64, kind string, data any) int {
	event := encode(kind, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := 0
	for client := range h.clients {
		if !audience.matches(client) {
			continue
		}
		client.ObserveRevision(revision)
		client.generation.Add(1)
		client.queueRevision(event, client.AuthorityRevision())
		changed++
	}
	return changed
}

// BroadcastToRevision delivers an event only to the listed accounts and only to the
// connections that have not already observed a newer authorization revision than the
// one the audience was resolved at.
//
// The two decisions have to agree. An audience is a database answer about who may read
// a resource, and a revocation can commit between resolving it and queueing the event;
// queueing it anyway would tag it with the connection's current generation and deliver
// it to an account the server has already refused. A connection that has observed a
// newer revision has been told to refetch, so this event is refused instead.
func (h *Hub) BroadcastToRevision(userIDs []string, revision int64, kind string, data any) {
	if len(userIDs) == 0 || revision <= 0 {
		return
	}
	event := encode(kind, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	allowed := make(map[string]struct{}, len(userIDs))
	for _, userID := range userIDs {
		allowed[userID] = struct{}{}
	}
	for client := range h.clients {
		if _, ok := allowed[client.UserID]; !ok {
			continue
		}
		if client.AuthorityRevision() > revision {
			continue
		}
		// The revision travels with the event so the writer re-checks it at write
		// admission, after the queue was drained and before the socket write.
		client.queueRevision(event, revision)
	}
}

func (h *Hub) SendUser(userID, kind string, data any) {
	event := encode(kind, data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.UserID == userID {
			client.queue(event)
		}
	}
}

func (h *Hub) Disconnect(userID, sessionHash string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if (userID == "" || client.UserID == userID) && (sessionHash == "" || client.SessionHash == sessionHash) {
			// A connection is always established before a client is added, so this guard
			// is for a client registered without one rather than for production traffic.
			if client.Conn != nil {
				_ = client.Conn.CloseNow()
			}
		}
	}
}

func (h *Hub) Presence() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	states := make(map[string]string)
	for client := range h.clients {
		state := "online"
		if time.Since(client.activeAt) > 5*time.Minute {
			state = "idle"
		}
		if states[client.UserID] != "online" {
			states[client.UserID] = state
		}
	}
	return states
}

func (h *Hub) Online(userID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.UserID == userID {
			return true
		}
	}
	return false
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
