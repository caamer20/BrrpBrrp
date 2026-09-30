package api

import (
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// Reading a thread moves the reader's position in *that thread*, and nowhere else.
//
// `conversation_reads` is keyed by conversation, so every message in a channel — a
// thread reply included — shared one cursor. That is the wrong shape for a thread: a
// member who read forty replies in a thread and then opened the channel had, on the
// only cursor that existed, read the channel as far as the thread's newest reply, and
// a member who read the *channel* had marked a thread they never opened as read.
//
// A thread is a conversation with a subject, and it moves at its own rate: a quiet
// thread beside a busy channel, or a busy thread nobody has opened, are the ordinary
// cases rather than the edge ones.
func TestReadingAThreadMovesOnlyThatThreadsPosition(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Read state", "")

	reply := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "inside the thread", "idempotency_key": "readstate-1"}, alice)
	if reply.Code != 201 {
		t.Fatalf("reply: %d %s", reply.Code, reply.Body.String())
	}
	replyID := readJSON[db.Message](t, reply).ID
	// An unrelated channel message, sent *after* the thread reply, so the only way its
	// id could become the channel's cursor is by reading the thread.
	other := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]any{"content": "unrelated channel message", "idempotency_key": "readstate-2"}, alice))

	marked := request(t, handler, "POST", "/api/threads/"+thread.ID+"/read",
		map[string]any{"last_read_message_id": replyID}, alice)
	if marked.Code != 200 {
		t.Fatalf("reading a thread: %d %s", marked.Code, marked.Body.String())
	}

	// The thread has its own position, at the reply.
	var threadCursor string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT COALESCE(last_read_message_id,'') FROM thread_reads WHERE user_id=? AND thread_id=?`,
		sessionUserID(t, s, alice), thread.ID).Scan(&threadCursor); err != nil {
		t.Fatal(err)
	}
	if threadCursor != replyID {
		t.Fatalf("the thread's position is %q, want the reply that was read", threadCursor)
	}
	// And the channel's position is untouched, so the unrelated message is still
	// unread. The row may not exist at all here — a member who has never marked the
	// channel read has no position — so this asserts it did not move rather than that
	// a particular value is present.
	var channelCursor string
	err := s.store.QueryRowContext(t.Context(),
		`SELECT COALESCE(last_read_message_id,'') FROM channel_reads WHERE user_id=? AND channel_id=?`,
		sessionUserID(t, s, alice), channelID).Scan(&channelCursor)
	if err == nil && channelCursor == other.ID {
		t.Fatalf("READING A THREAD MARKED AN UNRELATED CHANNEL MESSAGE READ (cursor at %q)", channelCursor)
	}
}

// The permitted control: reading the channel still moves the channel's own position,
// and does not reach into a thread.
func TestReadingTheChannelMovesTheChannelAndNotAThread(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Independent", "")

	channelMessage := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]any{"content": "channel content", "idempotency_key": "readstate-3"}, alice))
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/read",
		map[string]any{"last_read_message_id": channelMessage.ID}, alice); w.Code != 200 {
		t.Fatalf("read the channel: %d %s", w.Code, w.Body.String())
	}
	var channelCursor string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT COALESCE(last_read_message_id,'') FROM channel_reads WHERE user_id=? AND channel_id=?`,
		sessionUserID(t, s, alice), channelID).Scan(&channelCursor); err != nil {
		t.Fatal(err)
	}
	if channelCursor != channelMessage.ID {
		t.Fatalf("the channel's position is %q, want the message that was read", channelCursor)
	}
	// The thread was never opened, so it has no position at all — rather than one
	// borrowed from the channel.
	var threadCursors int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM thread_reads WHERE user_id=? AND thread_id=?`,
		sessionUserID(t, s, alice), thread.ID).Scan(&threadCursors); err != nil {
		t.Fatal(err)
	}
	if threadCursors != 0 {
		t.Fatal("reading the channel created a reading position in a thread nobody opened")
	}
	_ = root
}

// A position may only be a message of the thread it is set on. A cursor in one thread
// pointing at a message in another would make the next unread count meaningless.
func TestAThreadsPositionMayOnlyBeAMessageOfThatThread(t *testing.T) {
	// Restricted channels are a default-off feature, and the last case here is about
	// a reader who cannot reach the channel — which is only a meaningful state on a
	// server that has them. Run with the feature off it could not see the defect.
	t.Setenv("BRRP_ENABLE_RESTRICTED_CHANNELS", "true")
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	messages := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	thread := startThread(t, handler, channelID, alice, messages[0].ID, "Scoped", "")

	// A channel message, which is not in the thread.
	outside := messages[1]
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/read",
		map[string]any{"last_read_message_id": outside.ID}, alice); w.Code != http.StatusNotFound {
		t.Fatalf("a thread accepted a position pointing at a channel message: %d %s", w.Code, w.Body.String())
	}
	var cursors int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM thread_reads WHERE user_id=? AND thread_id=?`,
		sessionUserID(t, s, alice), thread.ID).Scan(&cursors); err != nil {
		t.Fatal(err)
	}
	if cursors != 0 {
		t.Fatal("the refused position was written anyway")
	}
	// And a member who cannot read the channel cannot move a cursor in a thread of
	// it, because a thread is only as restricted as the channel it inherits from.
	// The fixture channel is public, so this uses a restricted one — otherwise the
	// outsider below would be a legitimate reader and the assertion would be testing
	// nothing.
	// The fixture channel is public, so this restricts a second one — otherwise the
	// outsider would be a legitimate reader and the assertion would be testing nothing.
	second := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "restricted", Type: "text", MaxVoiceUsers: 8}, ownerCookie(t, s, handler)))
	if w := request(t, handler, "PUT", "/api/channels/"+second.ID+"/access",
		map[string]any{"visibility": "private", "user_ids": []string{sessionUserID(t, s, alice)}},
		ownerCookie(t, s, handler)); w.Code != 200 {
		t.Fatalf("restrict the second channel: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/channels/"+second.ID+"/messages",
		map[string]any{"content": "restricted words", "idempotency_key": "readstate-4"}, alice); w.Code != 201 {
		t.Fatalf("seed the restricted channel: %d %s", w.Code, w.Body.String())
	}
	hiddenRoot := readJSON[[]db.Message](t, request(t, handler, "GET",
		"/api/channels/"+second.ID+"/messages", nil, alice))[0]
	hiddenThread := startThread(t, handler, second.ID, alice, hiddenRoot.ID, "Restricted", "")
	_, outsider := memberAccount(t, s, "readstate_outsider")
	if w := request(t, handler, "POST", "/api/threads/"+hiddenThread.ID+"/read",
		map[string]any{"last_read_message_id": hiddenRoot.ID}, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("an account that cannot read the channel moved a position in its thread: %d %s", w.Code, w.Body.String())
	}
}
