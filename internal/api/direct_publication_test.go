package api

import (
	"testing"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

// A writer can commit before a block but reach publication after it. Resolving
// the new revision cannot turn that delayed contact event into permitted work.
func TestDelayedDirectPublicationHonorsCurrentContactState(t *testing.T) {
	for _, transition := range []string{"block", "deactivate"} {
		t.Run(transition, func(t *testing.T) {
			s, h, admin, aid, a, bid, b, id := privatePipeline(t)
			client := &ws.Client{ID: db.ID(), UserID: aid, SessionHash: activeSessionHash(t, s, aid), Out: make(chan ws.Queued, 32)}
			if !s.hub.Add(client) {
				t.Fatal("register recipient")
			}
			defer s.hub.Remove(client)
			// Commit the shared message row, then deliberately hold its publication.
			// This is the actual interval between createMessage's commit and publish.
			message := db.Message{ID: db.ID(), ConversationID: id, UserID: bid, Content: "committed before contact ended", CreatedAt: db.Now()}
			tx, err := s.store.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key) VALUES(?,?,NULL,?,?,?,?)`, message.ID, id, bid, message.Content, message.CreatedAt, "delayed-direct-publication"); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if transition == "block" {
				if w := request(t, h, "PUT", "/api/me/blocks/"+aid, map[string]bool{}, b); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			} else {
				if w := request(t, h, "PATCH", "/api/admin/users/"+bid, map[string]any{"is_deactivated": true, "confirm": "bob_dm"}, admin); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			drainNotificationEvents(t, client)
			for _, kind := range []string{"message.created", "message.updated", "reaction.updated"} {
				s.publishMessage(kind, message)
			}
			if events := drainNotificationEvents(t, client); len(events) != 0 {
				t.Fatalf("delayed contact events escaped after %s: %v", transition, events)
			}
			// Explicit retained-history reads remain permitted; blocking is not erasure.
			if w := request(t, h, "GET", "/api/messages/"+message.ID, nil, a); w.Code != 200 {
				t.Fatalf("retained history became unreadable: %d %s", w.Code, w.Body.String())
			}
			if _, err := s.store.Exec(`DELETE FROM messages WHERE id=?`, message.ID); err != nil {
				t.Fatal(err)
			}
			s.publishMessage("message.deleted", message)
			if events := drainNotificationEvents(t, client); events["message.deleted"] != 1 {
				t.Fatalf("necessary deletion update was suppressed: %v", events)
			}
			if transition == "block" {
				if w := request(t, h, "DELETE", "/api/me/blocks/"+aid, map[string]bool{}, b); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			} else {
				if w := request(t, h, "PATCH", "/api/admin/users/"+bid, map[string]any{"is_deactivated": false, "confirm": "bob_dm"}, admin); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			drainNotificationEvents(t, client)
			// Restoring contact admits a new publication, without replaying the ones dropped.
			s.publishToConversation(s.ctx, id, "message.created", map[string]string{"id": "new-permitted-event"})
			if events := drainNotificationEvents(t, client); events["message.created"] != 1 {
				t.Fatalf("restored contact did not permit new events: %v", events)
			}
		})
	}
}
