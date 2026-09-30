package api

import (
	"testing"

	"brrpbrrp/internal/db"
)

// Deleting a thread's root tombstones the thread; it does not remove it.
//
// The schema cascaded the thread row away with the root, so the replies — acknowledged,
// read history that people had linked to — were left carrying a `thread_id` that
// resolved to nothing. They were absent from the thread (404), absent from the channel
// listing, and still returned by search with a link that 404s. A member who deleted
// one message lost the rest of the conversation, and nothing said so.
//
// The subject is withdrawn, not the discussion.
func TestDeletingAThreadRootTombstonesTheThreadAndKeepsItsReplies(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "A thread with replies", "")

	reply := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "an acknowledged reply", "idempotency_key": "tombstone-reply-1"}, alice)
	if reply.Code != 201 {
		t.Fatalf("reply: %d %s", reply.Code, reply.Body.String())
	}
	replyID := readJSON[db.Message](t, reply).ID

	if w := request(t, handler, "DELETE", "/api/messages/"+root.ID, map[string]any{"confirm": root.ID}, alice); w.Code != 200 {
		t.Fatalf("delete the root: %d %s", w.Code, w.Body.String())
	}

	// The subject is gone.
	var rootStill string
	if err := s.store.QueryRowContext(t.Context(), `SELECT COALESCE(root_message_id,'') FROM threads WHERE id=?`, thread.ID).Scan(&rootStill); err != nil {
		t.Fatalf("the thread row went with its root: %v", err)
	}
	if rootStill != "" {
		t.Fatalf("the thread still points at a deleted root: %s", rootStill)
	}
	var deletedAt string
	if err := s.store.QueryRowContext(t.Context(), `SELECT COALESCE(root_deleted_at,'') FROM threads WHERE id=?`, thread.ID).Scan(&deletedAt); err != nil {
		t.Fatal(err)
	}
	if deletedAt == "" {
		t.Fatal("the thread does not record that its subject was removed, so it cannot say why")
	}

	// And the discussion stands: the thread is readable, the reply is in it, and it
	// says what it was attached to.
	page := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, alice)
	if page.Code != 200 {
		t.Fatalf("a tombstoned thread is not readable: %d %s", page.Code, page.Body.String())
	}
	var found bool
	for _, message := range readJSON[[]db.Message](t, page) {
		if message.ID == replyID {
			found = true
		}
	}
	if !found {
		t.Fatal("THE REPLIES WENT WITH THE ROOT")
	}
	// The thread's own summary is reachable, so a deep link still resolves and the
	// interface can render the tombstone rather than a 404.
	summary := request(t, handler, "GET", "/api/threads/"+thread.ID, nil, alice)
	if summary.Code != 200 {
		t.Fatalf("a deep link to a tombstoned thread answers %d: %s", summary.Code, summary.Body.String())
	}
	body := readJSON[map[string]any](t, summary)
	if when, _ := body["root_deleted_at"].(string); when == "" {
		t.Fatal("the thread's summary does not tell the interface its subject was removed")
	}
	// The subject's *text* is not retained: the tombstone records that it went, not
	// what it said, and nothing here keeps a copy of it.
	var rootRows int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE id=?`, root.ID).Scan(&rootRows); err != nil {
		t.Fatal(err)
	}
	if rootRows != 0 {
		t.Fatal("the tombstone kept the subject's content")
	}
	// The permitted control: the tombstoned thread still takes replies, because a
	// discussion whose subject was withdrawn is still a discussion.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "still going", "idempotency_key": "tombstone-reply-2"}, alice); w.Code != 201 {
		t.Fatalf("a tombstoned thread refused a reply: %d %s", w.Code, w.Body.String())
	}
	// And it still appears in the channel's thread list, or a member would have no way
	// back to a conversation they were part of.
	listed := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
	var inList bool
	for _, candidate := range listed {
		if candidate.ID == thread.ID {
			inList = true
		}
	}
	if !inList {
		t.Fatal("a tombstoned thread left the channel's thread list")
	}
}
