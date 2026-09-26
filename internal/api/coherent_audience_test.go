package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// performRequest serves one request without touching a *testing.T, so a test can run a
// real authorization change on another goroutine while the calling goroutine holds the
// audience snapshot.
func performRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://localhost:8080")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// deliveredIDs drains a connection's queue and reports the identifiers the writer would
// actually send, so an assertion is about delivery rather than about queue contents.
func deliveredIDs(t *testing.T, client *ws.Client) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for {
		select {
		case queued := <-client.Out:
			if !client.Admitted(queued) {
				continue
			}
			var event ws.Event
			if err := json.Unmarshal(queued.Event, &event); err != nil {
				t.Fatalf("event %q: %v", queued.Event, err)
			}
			var payload struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(event.Data, &payload)
			// An event with no identifier is the invalidation or a presence update, which
			// is not the payload this test is about.
			if payload.ID != "" {
				ids[payload.ID] = true
			}
		default:
			return ids
		}
	}
}

// TestAnAuthorizationChangeCannotInterleaveAnAudienceSnapshot is the maintained R4-01
// regression. Review 4 reproduced the defect by wrapping the query interface and
// revoking the member at the final revision read: the audience still named the member
// while the revision was the one the revocation committed, so the delivery check saw a
// pair that agreed with itself and delivered the restricted payload on a real socket.
//
// The permanent regression forces the same point through the audited seam. While the
// audience snapshot is open, a revocation is attempted on another goroutine. A coherent
// snapshot holds the store's only connection, so the change cannot commit inside it and
// the resolved revision is the pre-revocation one. The consequence is then proved at the
// delivery boundary: the resolved pair is refused for the connection the change
// invalidated, the permitted manager still receives its event, and a regrant returns the
// account to the audience without a reconnect.
func TestAnAuthorizationChangeCannotInterleaveAnAudienceSnapshot(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, _ := memberAccount(t, s, "reader")
	channel := privateTextChannel(t, handler, admin, "snapshot", memberID)
	adminID := sessionUserID(t, s, admin)

	before := s.authorizationRevision(t.Context())

	// Two connections admitted at the revision the resolution will use, as connections
	// that were already open when the change commits.
	member := &ws.Client{ID: db.ID(), UserID: memberID, SessionHash: activeSessionHash(t, s, memberID), Out: make(chan ws.Queued, 8)}
	member.ObserveRevision(before)
	if !s.hub.Add(member) {
		t.Fatal("the hub refused the member's connection")
	}
	defer s.hub.Remove(member)
	manager := &ws.Client{ID: db.ID(), UserID: adminID, SessionHash: activeSessionHash(t, s, adminID), Out: make(chan ws.Queued, 8)}
	manager.ObserveRevision(before)
	if !s.hub.Add(manager) {
		t.Fatal("the hub refused the manager's connection")
	}
	defer s.hub.Remove(manager)

	// The revocation is attempted while the snapshot is open. It must not be able to
	// commit until the snapshot releases the connection.
	var once sync.Once
	revocationDone := make(chan int, 1)
	hook := func() {
		once.Do(func() {
			go func() {
				revocationDone <- performRequest(handler, "PUT", "/api/channels/"+channel.ID+"/access",
					map[string]any{"visibility": "private"}, admin).Code
			}()
			select {
			case code := <-revocationDone:
				t.Errorf("an authorization change committed inside the audience snapshot (status %d)", code)
				revocationDone <- code
			case <-time.After(200 * time.Millisecond):
				// Blocked on the connection the snapshot holds, as it must be.
			}
		})
	}
	s.audienceInterleave.Store(&hook)
	audience, revision, err := s.coherentAudience(t.Context(), channel.ID)
	s.audienceInterleave.Store(nil)
	if err != nil {
		t.Fatal(err)
	}
	if code := <-revocationDone; code != http.StatusOK {
		t.Fatalf("the revocation answered %d", code)
	}

	// The resolved pair is the pre-revocation one: the member is in the audience and the
	// revision is the one that audience was computed at, not the one the revocation
	// committed.
	if !contains(audience, memberID) {
		t.Fatalf("the member is not in the audience resolved before the change: %v", audience)
	}
	if revision != before {
		t.Fatalf("revision resolved = %d, want the pre-revocation %d", revision, before)
	}
	after := s.authorizationRevision(t.Context())
	if after <= revision {
		t.Fatalf("the revocation did not advance the revision: %d -> %d", revision, after)
	}
	if observed := member.AuthorityRevision(); observed != after {
		t.Fatalf("the member's connection observed revision %d, want %d", observed, after)
	}

	// The pre-revocation pair is refused for the member and the committed audience is
	// delivered to the manager.
	s.hub.BroadcastToRevision(audience, revision, "message.created", map[string]string{"id": "stale"})
	current, currentRevision, err := s.coherentAudience(t.Context(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if contains(current, memberID) {
		t.Fatalf("the committed audience still names the revoked member: %v", current)
	}
	s.hub.BroadcastToRevision([]string{adminID}, currentRevision, "message.created", map[string]string{"id": "permitted"})

	if got := deliveredIDs(t, member); len(got) != 0 {
		t.Fatalf("the revoked connection received %v", got)
	}
	if got := deliveredIDs(t, manager); !got["permitted"] {
		t.Fatalf("the permitted manager received %v, want the event published at the committed revision", got)
	}

	// A regrant is a new change at a newer revision: the account is back in the audience
	// and receives events under the revision its connection observed, with no
	// reconnect-only workaround.
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
		map[string]any{"visibility": "private", "user_ids": []string{memberID}}, admin); w.Code != http.StatusOK {
		t.Fatalf("regrant: %d %s", w.Code, w.Body.String())
	}
	regranted, regrantedRevision, err := s.coherentAudience(t.Context(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(regranted, memberID) {
		t.Fatalf("the regranted account is not in the committed audience: %v", regranted)
	}
	s.hub.BroadcastToRevision(regranted, regrantedRevision, "message.created", map[string]string{"id": "regranted"})
	if got := deliveredIDs(t, member); !got["regranted"] {
		t.Fatalf("a regranted account received %v under the current revision", got)
	}
}

// assertAudienceChangeIsCoherent resolves a channel's audience, applies one authority
// change, and asserts the two properties the coherent snapshot exists to provide: the
// audience under the committed revision no longer names the revoked account, and the pair
// resolved under the old revision is refused at delivery once the connection observes the
// new one. The permitted manager still receives events.
func assertAudienceChangeIsCoherent(t *testing.T, s *Server, channelID, memberID, managerID string, change func()) {
	t.Helper()
	before, beforeRevision, err := s.coherentAudience(t.Context(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(before, memberID) {
		t.Fatalf("fixture: %s is not in the audience before the change: %v", memberID, before)
	}
	member := &ws.Client{ID: db.ID(), UserID: memberID, SessionHash: activeSessionHash(t, s, memberID), Out: make(chan ws.Queued, 8)}
	member.ObserveRevision(beforeRevision)
	if !s.hub.Add(member) {
		t.Fatal("the hub refused the member's connection")
	}
	defer s.hub.Remove(member)
	manager := &ws.Client{ID: db.ID(), UserID: managerID, SessionHash: activeSessionHash(t, s, managerID), Out: make(chan ws.Queued, 8)}
	manager.ObserveRevision(beforeRevision)
	if !s.hub.Add(manager) {
		t.Fatal("the hub refused the manager's connection")
	}
	defer s.hub.Remove(manager)

	change()

	after, afterRevision, err := s.coherentAudience(t.Context(), channelID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRevision <= beforeRevision {
		t.Fatalf("the change did not advance the authorization revision: %d -> %d", beforeRevision, afterRevision)
	}
	if contains(after, memberID) {
		t.Fatalf("the audience resolved under the committed revision still names the revoked account: %v", after)
	}
	if observed := member.AuthorityRevision(); observed != afterRevision {
		t.Fatalf("the revoked connection observed revision %d, want %d", observed, afterRevision)
	}

	// The pair resolved before the change is refused for the revoked connection; the
	// permitted manager receives an event published at the committed revision.
	s.hub.BroadcastToRevision(before, beforeRevision, "message.created", map[string]string{"id": "stale"})
	s.hub.BroadcastToRevision(after, afterRevision, "message.created", map[string]string{"id": "permitted"})

	if got := deliveredIDs(t, member); len(got) != 0 {
		t.Fatalf("the revoked connection received %v", got)
	}
	if got := deliveredIDs(t, manager); !got["permitted"] {
		t.Fatalf("the permitted manager received %v, want the event published at the committed revision", got)
	}
}

// TestAResolvedAudienceAgreesWithItsRevisionAcrossAuthorityChanges covers the change
// kinds review 4 named as applicable that advance the authorization revision: a direct
// grant, a role that grants read implicitly to every account, and a channel's
// visibility. Each change is applied through the real API, so the audience is resolved
// against committed authority rather than a fixture. Account status and session
// revocation are different in kind: they end the affected account's sessions and close
// its sockets instead of re-labeling authority, and that path is covered by the
// effective-role and revocation-boundary regressions.
func TestAResolvedAudienceAgreesWithItsRevisionAcrossAuthorityChanges(t *testing.T) {
	t.Run("direct grant revoked", func(t *testing.T) {
		s, handler := restrictedServer(t)
		admin := setupAdmin(t, s, handler)
		memberID, _ := memberAccount(t, s, "reader")
		channel := privateTextChannel(t, handler, admin, "grant", memberID)
		assertAudienceChangeIsCoherent(t, s, channel.ID, memberID, sessionUserID(t, s, admin), func() {
			if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
				map[string]any{"visibility": "private"}, admin); w.Code != http.StatusOK {
				t.Fatalf("revoke the grant: %d %s", w.Code, w.Body.String())
			}
		})
	})

	t.Run("implicit default role narrowed", func(t *testing.T) {
		s, handler := restrictedServer(t)
		admin := setupAdmin(t, s, handler)
		memberID, _ := implicitMember(t, s, handler, admin, "implicit")
		channels, err := s.channels()
		if err != nil {
			t.Fatal(err)
		}
		var channelID string
		for _, channel := range channels {
			if channel.Type == "text" && channel.Visibility != "private" {
				channelID = channel.ID
				break
			}
		}
		if channelID == "" {
			t.Fatal("the installation has no public text channel")
		}
		role := defaultRole(t, handler, admin)
		narrowed := withoutCapability(role.Permissions, "channel.read")
		assertAudienceChangeIsCoherent(t, s, channelID, memberID, sessionUserID(t, s, admin), func() {
			if w := request(t, handler, "PATCH", "/api/roles/"+role.ID,
				map[string]any{"name": role.Name, "position": role.Position, "permissions": narrowed}, admin); w.Code != http.StatusOK {
				t.Fatalf("narrow the default role: %d %s", w.Code, w.Body.String())
			}
		})
	})

	// A visibility change is the third committed authority change: everyone keeps
	// channel.read, but the channel is no longer public, so the audience shrinks to the
	// accounts that hold an explicit grant.
	t.Run("channel made private", func(t *testing.T) {
		s, handler := restrictedServer(t)
		admin := setupAdmin(t, s, handler)
		memberID, _ := memberAccount(t, s, "reader")
		channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
			db.Channel{Name: "opened", Type: "text", Visibility: "public", MaxVoiceUsers: 8}, admin))
		assertAudienceChangeIsCoherent(t, s, channel.ID, memberID, sessionUserID(t, s, admin), func() {
			if w := request(t, handler, "PATCH", "/api/channels/"+channel.ID,
				map[string]any{"name": "opened", "visibility": "private", "posting": "members"}, admin); w.Code != http.StatusOK {
				t.Fatalf("restrict the channel: %d %s", w.Code, w.Body.String())
			}
		})
	})
}

// TestARevocationCommittingDuringAPendingWriteIsRefused is the maintained R5-01
// regression. Review 5 reproduced it by pausing the socket writer immediately before its
// admission decision, committing a revocation through the real channel-access API while
// the existing voice gate delayed the handler's follow-up work, and releasing the writer:
// the queued private payload was still admitted. Client.Admitted compared the event only
// with the revision the connection had observed, and the invalidation that would have
// raised it had not been published yet.
//
// The fix makes the commit and the invalidation one step under the authority fence, so a
// writer released after the commit observes the invalidation and refuses; the permitted
// manager, who keeps access, still receives the event that was queued for it, and a
// regrant returns the account to delivery without a reconnect.
func TestARevocationCommittingDuringAPendingWriteIsRefused(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, memberCookie := memberAccount(t, s, "r5-writer-reader")
	channel := privateTextChannel(t, handler, admin, "r5-writer", memberID)

	server := httptest.NewServer(handler)
	defer server.Close()
	memberSocket := voiceSocket(t, s, server, memberCookie)
	managerSocket := voiceSocket(t, s, server, admin)

	// The writer of the member's connection is paused exactly where the review paused it:
	// after the queue was drained and before the admission decision. Only the member is
	// paused, so the manager's delivery is free to proceed.
	var once sync.Once
	paused, release := make(chan struct{}), make(chan struct{})
	hook := func(client *ws.Client, queued ws.Queued) {
		if client.UserID == memberID && bytes.Contains(queued.Event, []byte("r5-01-marker")) {
			once.Do(func() { close(paused) })
			<-release
		}
	}
	s.writerAdmission.Store(&hook)
	defer s.writerAdmission.Store(nil)

	before := s.authorizationRevision(t.Context())
	s.publishToChannel(t.Context(), channel.ID, "message.created", map[string]string{"id": "r5-01-marker"})
	select {
	case <-paused:
	case <-time.After(3 * time.Second):
		t.Fatal("the writer never reached its admission decision")
	}

	// The permitted control: the manager is in the same audience and its writer was not
	// paused, so the event queued before the change is delivered to it normally.
	if payload := readEventOfKind(t, managerSocket, "message.created", 2*time.Second); payload == nil {
		t.Fatal("the permitted manager did not receive the event queued before the change")
	} else if !bytes.Contains(payload, []byte("r5-01-marker")) {
		t.Fatalf("the manager received a different event: %s", payload)
	}

	// The existing voice gate is held so the handler's stream and voice enforcement waits.
	// The commit itself is not delayed: the invalidation is part of it now.
	s.voiceGate.Lock()
	revoked := make(chan int, 1)
	go func() {
		revoked <- performRequest(handler, "PUT", "/api/channels/"+channel.ID+"/access",
			map[string]any{"visibility": "private"}, admin).Code
	}()
	deadline := time.Now().Add(3 * time.Second)
	for s.authorizationRevision(t.Context()) <= before {
		if time.Now().After(deadline) {
			s.voiceGate.Unlock()
			close(release)
			t.Fatal("the revocation never committed")
		}
		time.Sleep(time.Millisecond)
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, memberCookie); w.Code != http.StatusNotFound {
		s.voiceGate.Unlock()
		close(release)
		t.Fatalf("the revocation was not committed: history answered %d", w.Code)
	}

	// The writer is released with the enforcement still delayed. The admission decision is
	// taken against the committed authority, so the queued payload is refused and the next
	// event the connection receives is the invalidation that told it its authority moved.
	// Reading until that signal proves the payload was passed over rather than merely
	// delayed; a bounded read that times out would close the connection instead.
	close(release)
	for _, kind := range readKindsUntil(t, memberSocket, "authorization.changed", 3*time.Second) {
		if kind == "message.created" {
			t.Fatal("the revoked member's socket received the payload queued before the revocation")
		}
	}
	s.voiceGate.Unlock()
	if code := <-revoked; code != http.StatusOK {
		t.Fatalf("revoke status %d", code)
	}

	// A regrant is a new change: the account is back in the audience and receives events
	// published after it, without a reconnect-only workaround.
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
		map[string]any{"visibility": "private", "user_ids": []string{memberID}}, admin); w.Code != http.StatusOK {
		t.Fatalf("regrant: %d %s", w.Code, w.Body.String())
	}
	s.publishToChannel(t.Context(), channel.ID, "message.created", map[string]string{"id": "r5-01-regranted"})
	if payload := readEventOfKind(t, memberSocket, "message.created", 2*time.Second); payload == nil {
		t.Fatal("a regranted account did not receive a permitted event")
	} else if !bytes.Contains(payload, []byte("r5-01-regranted")) {
		t.Fatalf("the regranted account received the stale payload: %s", payload)
	}
}

// readEventOfKind reads from a real socket until one event of the wanted kind arrives, and
// returns its payload. It returns nil when none arrives inside the window. The window must
// be long enough that reaching it is not expected: an expired read context closes the
// connection, so a caller that intends to keep using the socket should use readKindsUntil.
func readEventOfKind(t *testing.T, socket *websocket.Conn, kind string, window time.Duration) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, socket, &event); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil
			}
			t.Fatalf("socket read: %v", err)
		}
		if event.Type == kind {
			return event.Data
		}
	}
}

// readKindsUntil reads a real socket until one event of the wanted kind arrives and returns
// every kind seen in order, including it. The window is generous on purpose: reaching it is
// a failure rather than an expected silence, so the connection is not closed by a timeout
// and the caller can keep reading from it.
func readKindsUntil(t *testing.T, socket *websocket.Conn, want string, window time.Duration) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	var kinds []string
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, socket, &event); err != nil {
			t.Fatalf("waiting for %s: %v (saw %v)", want, err, kinds)
		}
		kinds = append(kinds, event.Type)
		if event.Type == want {
			return kinds
		}
	}
}

// TestTheRevokedAudienceIsNotDeliveredOnARealSocket is the delivery half of R4-01: the
// audience resolved while the member still had access is broadcast after the revocation.
// The real connection was served the invalidation, so the revision it observed moved past
// the one the audience was resolved at and the payload never reaches it.
func TestTheRevokedAudienceIsNotDeliveredOnARealSocket(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, memberCookie := memberAccount(t, s, "socket-reader")
	channel := privateTextChannel(t, handler, admin, "socket", memberID)

	server := httptest.NewServer(handler)
	defer server.Close()
	socket := voiceSocket(t, s, server, memberCookie)

	audience, revision, err := s.coherentAudience(t.Context(), channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(audience, memberID) {
		t.Fatalf("the member is not in the audience: %v", audience)
	}

	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
		map[string]any{"visibility": "private"}, admin); w.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, memberCookie); w.Code != http.StatusNotFound {
		t.Fatalf("the revoked member can still read history: %d %s", w.Code, w.Body.String())
	}

	s.hub.BroadcastToRevision(audience, revision, "message.created", map[string]string{"id": "restricted"})

	// The socket receives the invalidation, and must not receive the restricted payload.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, socket, &event); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return
			}
			t.Fatalf("socket read: %v", err)
		}
		if event.Type == "message.created" {
			t.Fatalf("the revoked member's socket received %s", event.Data)
		}
	}
}
