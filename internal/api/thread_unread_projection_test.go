package api

import (
	"net/http"
	"sync"
	"testing"

	"brrpbrrp/internal/db"
)

// The visible projections of thread reading: a channel's badge counts channel
// messages only, a thread's own count is measured against the reader's thread cursor,
// and each cursor moves the other's number not at all. Review 17 reproduced the
// opposite — reading the sole unread thread reply left the channel at 1 — because the
// channel query counted every message carrying the channel's id, thread replies
// included (review 17, V17-03).

// channelUnreadFor returns one channel's unread row as the state snapshot would see it.
func channelUnreadFor(t *testing.T, s *Server, userID, channelID string) (int, int) {
	t.Helper()
	counts, err := s.unread(userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range counts {
		if row.ChannelID == channelID {
			return row.Count, row.Mentions
		}
	}
	return 0, 0
}

func TestChannelUnreadExcludesThreadRepliesAndThreadUnreadIsIndependent(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, _ := threadFixture(t, s, handler)
	aliceID := sessionUserID(t, s, alice)
	messages := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	thread := startThread(t, handler, channelID, alice, messages[0].ID, "Independent counts", "")

	// Alice starts read: everything the fixture wrote is behind her cursor, so a count
	// of one can only come from what this test sends next.
	read := request(t, handler, "POST", "/api/channels/"+channelID+"/read",
		map[string]any{"last_read_message_id": messages[len(messages)-1].ID}, alice)
	if read.Code != 200 {
		t.Fatalf("read the channel: %d %s", read.Code, read.Body.String())
	}

	// Bob replies in the thread and posts one channel message. The channel's count
	// must see only the channel message; the thread's count must see the reply.
	reply := readJSON[db.Message](t, request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "the only thread reply", "idempotency_key": "unread-thread-1"}, bob))
	channelMessage := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]any{"content": "the only channel message", "idempotency_key": "unread-channel-1"}, bob))

	if count, _ := channelUnreadFor(t, s, aliceID, channelID); count != 1 {
		t.Fatalf("the channel badge shows %d with one channel message unread and one thread reply", count)
	}
	threadUnread := func() (int, int) {
		threads := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
		for _, summary := range threads {
			if summary.ID == thread.ID {
				return summary.Unread, summary.UnreadMentions
			}
		}
		t.Fatal("the thread is not in the channel's list")
		return 0, 0
	}
	if unread, _ := threadUnread(); unread != 1 {
		t.Fatalf("the thread shows %d unread with one unseen reply", unread)
	}

	// Reading the thread clears the thread's count and leaves the channel's alone:
	// these are two positions, and neither is the other's shortcut.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/read",
		map[string]any{"last_read_message_id": reply.ID}, alice); w.Code != 200 {
		t.Fatalf("read the thread: %d %s", w.Code, w.Body.String())
	}
	if unread, _ := threadUnread(); unread != 0 {
		t.Fatalf("reading the thread left its own count at %d", unread)
	}
	if count, _ := channelUnreadFor(t, s, aliceID, channelID); count != 1 {
		t.Fatalf("reading a thread changed the channel badge to %d", count)
	}

	// Reading the channel clears its count, and a new thread reply afterwards moves
	// only the thread's number: no reply ever becomes a phantom channel unread again.
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/read",
		map[string]any{"last_read_message_id": channelMessage.ID}, alice); w.Code != 200 {
		t.Fatalf("read the channel: %d %s", w.Code, w.Body.String())
	}
	if count, _ := channelUnreadFor(t, s, aliceID, channelID); count != 0 {
		t.Fatalf("the channel badge stayed at %d after being read", count)
	}
	request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "one more thread reply", "idempotency_key": "unread-thread-2"}, bob)
	if count, _ := channelUnreadFor(t, s, aliceID, channelID); count != 0 {
		t.Fatalf("a thread reply became channel unread %d", count)
	}
	if unread, _ := threadUnread(); unread != 1 {
		t.Fatalf("the thread count after a new reply is %d, want 1", unread)
	}
}

func TestThreadMentionsCountForTheThreadAndNotTheChannel(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, _ := threadFixture(t, s, handler)
	aliceID := sessionUserID(t, s, alice)
	messages := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	thread := startThread(t, handler, channelID, alice, messages[0].ID, "Mentions", "")
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/read",
		map[string]any{"last_read_message_id": messages[len(messages)-1].ID}, alice); w.Code != 200 {
		t.Fatalf("read the channel: %d %s", w.Code, w.Body.String())
	}

	// A mention inside a thread reply is the thread's business.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "hello @alice, in here", "idempotency_key": "unread-mention-1"}, bob); w.Code != 201 {
		t.Fatalf("thread reply: %d %s", w.Code, w.Body.String())
	}
	if count, mentions := channelUnreadFor(t, s, aliceID, channelID); count != 0 || mentions != 0 {
		t.Fatalf("a thread mention surfaced as channel unread/mеntions %d/%d", count, mentions)
	}
	threads := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
	for _, summary := range threads {
		if summary.ID != thread.ID {
			continue
		}
		if summary.Unread != 1 || summary.UnreadMentions != 1 {
			t.Fatalf("thread counts = %d unread / %d mentions, want 1/1", summary.Unread, summary.UnreadMentions)
		}
	}

	// The permitted control: a mention in an ordinary channel message still counts as
	// one, so the exclusion above is about the thread, not about mentions at all.
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]any{"content": "hello @alice, out here", "idempotency_key": "unread-mention-2"}, bob); w.Code != 201 {
		t.Fatalf("channel message: %d %s", w.Code, w.Body.String())
	}
	if count, mentions := channelUnreadFor(t, s, aliceID, channelID); count != 1 || mentions != 1 {
		t.Fatalf("channel counts after a mention = %d/%d, want 1/1", count, mentions)
	}
}

func TestArchivedThreadRefusesRepliesUntilReopened(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, owner := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Archiving", "")

	// Control: while active, a reply is stored.
	sent := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "before the archive", "idempotency_key": "archive-before"}, bob)
	if sent.Code != 201 {
		t.Fatalf("active reply: %d %s", sent.Code, sent.Body.String())
	}

	archived := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]string{"state": "archived"}, owner)
	if archived.Code != 200 {
		t.Fatalf("archive: %d %s", archived.Code, archived.Body.String())
	}
	refused := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "after the archive", "idempotency_key": "archive-after"}, bob)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("an archived thread accepted a reply with %d: %s", refused.Code, refused.Body.String())
	}
	// The refusal is about writing: the history is still readable and still holds the
	// reply that predates the archive.
	history := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, bob)
	if history.Code != 200 {
		t.Fatalf("an archived thread's history = %d: %s", history.Code, history.Body.String())
	}
	if !containsContent(readJSON[[]db.Message](t, history), "before the archive") {
		t.Fatal("the archived thread lost the reply it had")
	}
	// And the refused send left nothing behind: no message, no claimed receipt that a
	// later retry could answer with.
	var stored int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM messages WHERE content='after the archive'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatal("the refused reply was stored anyway")
	}

	// Reopening is an explicit, usable action and restores writing; locking remains a
	// different state with a different refusal.
	reopened := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]string{"state": "active"}, owner)
	if reopened.Code != 200 {
		t.Fatalf("reopen: %d %s", reopened.Code, reopened.Body.String())
	}
	after := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "after the reopen", "idempotency_key": "archive-reopened"}, bob)
	if after.Code != 201 {
		t.Fatalf("a reopened thread refused a reply: %d %s", after.Code, after.Body.String())
	}
	locked := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]string{"state": "locked"}, owner)
	if locked.Code != 200 {
		t.Fatalf("lock: %d %s", locked.Code, locked.Body.String())
	}
	lockedReply := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "while locked", "idempotency_key": "archive-locked"}, bob)
	if lockedReply.Code != http.StatusForbidden {
		t.Fatalf("a locked thread answered %d: %s", lockedReply.Code, lockedReply.Body.String())
	}
}

// The archive race, at the only place it can be produced: the state is archived after
// the send's admission committed and before the send's own transaction re-reads it.
// The committing transaction is the decision that counts, or a reply lands in a thread
// that stopped accepting replies before it was written.
func TestAnArchiveThatLandsMidSendStillRefusesTheReply(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, owner := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Raced archive", "")

	archived := make(chan struct{})
	var once sync.Once
	hook := func(threadID string) {
		if threadID != thread.ID {
			return
		}
		once.Do(func() {
			w := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]string{"state": "archived"}, owner)
			if w.Code != 200 {
				t.Errorf("archive inside the send: %d %s", w.Code, w.Body.String())
			}
			close(archived)
		})
	}
	s.threadCommitHook.Store(&hook)
	defer s.threadCommitHook.Store(nil)

	raced := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "this raced the archive", "idempotency_key": "raced-archive-1"}, bob)
	<-archived
	if raced.Code != http.StatusForbidden {
		t.Fatalf("a reply raced an archive and was answered %d: %s", raced.Code, raced.Body.String())
	}
	var count int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM messages WHERE thread_id=? AND content='this raced the archive'`, thread.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("a reply landed in a thread archived while it was being sent")
	}
	// The key was not claimed either: once the thread is reopened, the same words are
	// a fresh attempt rather than a conflict against work that never happened.
	if w := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]string{"state": "active"}, owner); w.Code != 200 {
		t.Fatalf("reopen: %d %s", w.Code, w.Body.String())
	}
	retried := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "this raced the archive", "idempotency_key": "raced-archive-1"}, bob)
	if retried.Code != 201 {
		t.Fatalf("the same words after a reopen = %d: %s", retried.Code, retried.Body.String())
	}
}

func containsContent(messages []db.Message, content string) bool {
	for _, message := range messages {
		if message.Content == content {
			return true
		}
	}
	return false
}
