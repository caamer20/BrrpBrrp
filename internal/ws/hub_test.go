package ws

import (
	"encoding/json"
	"testing"
)

// newClient registers a connection for one account under an observed revision.
func newClient(t *testing.T, hub *Hub, userID string, revision int64) *Client {
	t.Helper()
	client := &Client{ID: userID + "-connection", UserID: userID, SessionHash: "session", Out: make(chan Queued, 8)}
	client.ObserveRevision(revision)
	if !hub.Add(client) {
		t.Fatal("the hub refused the connection")
	}
	t.Cleanup(func() { hub.Remove(client) })
	return client
}

// eventsOf drains a connection's queue and reports which event kinds it would write,
// skipping the ones the writer would already drop as stale. A test asserts on what was
// delivered, not on how many events happened to be queued first.
func eventsOf(client *Client) map[string]bool {
	kinds := map[string]bool{}
	for {
		select {
		case queued := <-client.Out:
			if !client.Admitted(queued) {
				continue
			}
			kinds[kind(queued.Event)] = true
		default:
			return kinds
		}
	}
}

// kind reports the event type of an encoded event.
func kind(encoded []byte) string {
	var event Event
	if err := json.Unmarshal(encoded, &event); err != nil {
		return ""
	}
	return event.Type
}

// TestAnAudienceResolvedBeforeARacknowledgedChangeIsRefused is the deterministic half of
// R3-05's first interleaving. An audience is a database answer about who may read a
// resource; a change can commit between resolving it and queueing the event. The
// connection that was told its authority moved must not receive the event that was
// authorized before it.
func TestAnAudienceResolvedBeforeARacknowledgedChangeIsRefused(t *testing.T) {
	hub := New()
	insider := newClient(t, hub, "insider", 7)
	bystander := newClient(t, hub, "bystander", 7)

	// The event is authorized for both accounts at revision 7, then the revocation
	// commits at revision 8 and reaches the insider before the event is queued.
	hub.Invalidate(Audience{UserIDs: []string{"insider"}}, 8, "authorization.changed", map[string]string{"reason": "A role changed."})
	hub.BroadcastToRevision([]string{"insider", "bystander"}, 7, "message.created", map[string]string{"id": "m1"})

	if eventsOf(insider)["message.created"] {
		t.Fatal("a restricted event authorized before the revocation reached the revoked connection")
	}
	// The connection that lost nothing receives it: the rule is about authority, not
	// about withholding restricted events.
	if !eventsOf(bystander)["message.created"] {
		t.Fatal("an unaffected connection did not receive the event")
	}

	// The audience decision and the delivery decision agree again once both are made at
	// the same revision, which is the permitted path.
	hub.BroadcastToRevision([]string{"insider"}, 8, "message.created", map[string]string{"id": "m2"})
	permitted := drain(insider)
	if permitted == nil || kind(permitted.Event) != "message.created" {
		t.Fatalf("a permitted event was not delivered: %v", permitted)
	}
	if !insider.Admitted(*permitted) {
		t.Fatal("a permitted event was not admitted at the writer")
	}
}

// TestAQueuedEventCannotOutliveTheAuthorityItWasQueuedUnder is the writer's half: the
// generation an event was queued under is compared before it is written, so the
// invalidation signal is what the connection sees instead.
func TestAQueuedEventCannotOutliveTheAuthorityItWasQueuedUnder(t *testing.T) {
	hub := New()
	client := newClient(t, hub, "insider", 3)
	client.Send("message.created", map[string]string{"id": "m1"})
	queued := drain(client)
	if queued == nil || client.Stale(*queued) {
		t.Fatal("an event was not deliverable before any authority changed")
	}
	if changed := hub.Invalidate(Audience{UserIDs: []string{"insider"}}, 4, "authorization.changed", map[string]any{}); changed != 1 {
		t.Fatalf("invalidate reached %d connections, want 1", changed)
	}
	if !client.Stale(*queued) {
		t.Fatal("an event queued before the change was still written")
	}
	if client.AuthorityRevision() != 4 {
		t.Fatalf("observed revision = %d, want 4", client.AuthorityRevision())
	}
}

// TestWriteAdmissionComparesTheRevisionTheEventCarries is the explicit writer contract
// that queue filtering alone did not state. An audience-scoped event carries the
// revision its audience was resolved at, and the writer compares that revision with the
// one the connection has observed at the moment of the write, independently of the
// authorization generation. A connection that has observed a newer revision must not
// have the event written, because it has already been told its authority moved; an
// event that is not scoped to an audience is unaffected by an unrelated revision.
func TestWriteAdmissionComparesTheRevisionTheEventCarries(t *testing.T) {
	hub := New()
	client := newClient(t, hub, "insider", 5)
	hub.BroadcastToRevision([]string{"insider"}, 5, "message.created", map[string]string{"id": "m1"})
	client.Send("presence.update", map[string]string{"state": "online"})

	scoped := drain(client)
	unscoped := drain(client)
	if scoped == nil || unscoped == nil {
		t.Fatal("the events were not queued")
	}
	if scoped.Revision != 5 {
		t.Fatalf("audience event revision = %d, want 5", scoped.Revision)
	}
	if unscoped.Revision != 0 {
		t.Fatalf("unscoped event revision = %d, want 0", unscoped.Revision)
	}
	// The event is dequeued, and only then does the connection observe the newer
	// revision a revocation would have announced. Write admission decides again.
	client.ObserveRevision(6)
	if client.Admitted(*scoped) {
		t.Fatal("an event resolved at revision 5 was admitted after the connection observed 6")
	}
	if !client.Admitted(*unscoped) {
		t.Fatal("a connection-level event was refused because an unrelated revision moved")
	}
}

// drain takes the next queued event for a connection, or nil when its queue is empty.
func drain(client *Client) *Queued {
	select {
	case queued := <-client.Out:
		return &queued
	default:
		return nil
	}
}
