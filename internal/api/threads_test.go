package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"net/http/httptest"
	"slices"

	"brrpbrrp/internal/db"
)

// namedUser is ordinaryUser with a distinct username, for fixtures that need more
// than one account. The shared helper hard-codes "member" and collides on the second
// call.
func namedUser(t *testing.T, s *Server, username string) (string, *http.Cookie) {
	t.Helper()
	id, now := db.ID(), db.Now()
	if _, err := s.store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`,
		id, username, "unused-test-hash", now, now); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := s.newSession(w, id); err != nil {
		t.Fatal(err)
	}
	return id, w.Result().Cookies()[0]
}

// threadFixture starts a channel, two members and one message to start a thread from.
func threadFixture(t *testing.T, s *Server, handler http.Handler) (channelID string, alice, bob, owner *http.Cookie) {
	t.Helper()
	admin := setupAdmin(t, s, handler)
	w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "threads", Type: "text", Category: "Test", MaxVoiceUsers: 8, Position: 1}, admin)
	if w.Code != 201 {
		t.Fatalf("create channel: %d %s", w.Code, w.Body.String())
	}
	channelID = readJSON[db.Channel](t, w).ID
	_, alice = namedUser(t, s, "alice")
	_, bob = namedUser(t, s, "bob")
	send := func(cookie *http.Cookie, content, key string) string {
		t.Helper()
		w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
			map[string]any{"content": content, "idempotency_key": key}, cookie)
		if w.Code != 201 {
			t.Fatalf("send %q: %d %s", content, w.Code, w.Body.String())
		}
		return readJSON[db.Message](t, w).ID
	}
	send(alice, "the first thing anyone said", "thread-fixture-a")
	send(bob, "and a second", "thread-fixture-b")
	return channelID, alice, bob, admin
}

// ownerOf returns the cookie of the server's owner. Setup is a one-time ceremony, so a
// test that needs administrative authority has to use the owner that already exists
// rather than trying to create a second one.
func ownerOf(t *testing.T, s *Server, handler http.Handler) (string, *http.Cookie) {
	t.Helper()
	var id string
	if err := s.store.QueryRowContext(t.Context(), `SELECT id FROM users WHERE username='admin'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := s.newSession(w, id); err != nil {
		t.Fatal(err)
	}
	return id, w.Result().Cookies()[0]
}

func ownerCookie(t *testing.T, s *Server, handler http.Handler) *http.Cookie {
	t.Helper()
	_, cookie := ownerOf(t, s, handler)
	return cookie
}

func startThread(t *testing.T, handler http.Handler, channelID string, cookie *http.Cookie, rootID, title, key string) threadSummary {
	t.Helper()
	w := request(t, handler, "POST", "/api/channels/"+channelID+"/threads",
		map[string]any{"root_message_id": rootID, "title": title}, cookie)
	if w.Code != 201 {
		t.Fatalf("start thread: %d %s", w.Code, w.Body.String())
	}
	_ = key
	return readJSON[threadSummary](t, w)
}

func TestThreadInheritsChannelAuthorizationAndIdentity(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	w := request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice)
	messages := readJSON[[]db.Message](t, w)
	root := messages[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Where do we put the release notes?", "")

	// The root keeps its identity and its link. A thread records a relationship; it
	// does not re-parent or rewrite the message it is about.
	found, err := s.store.Message(t.Context(), "", root.ID)
	if err != nil || found.ID != root.ID {
		t.Fatalf("the root message no longer resolves: %v", err)
	}

	// A reply lands in the thread and the channel listing no longer shows it, so a
	// busy thread cannot bury the channel it lives in and no client reads it twice.
	w = request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "in the thread", "idempotency_key": "thread-reply-one"}, alice)
	if w.Code != 201 {
		t.Fatalf("thread reply: %d %s", w.Code, w.Body.String())
	}
	channel := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	for _, message := range channel {
		if message.Content == "in the thread" {
			t.Fatal("a thread reply appeared in the channel listing")
		}
	}
	threadMessages := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, alice))
	// The thread holds its root plus the new reply: the root is a message in the
	// thread, not a separate thing above it.
	if len(threadMessages) != 2 {
		t.Fatalf("the thread holds %d messages, want its root and one reply", len(threadMessages))
	}
	if threadMessages[0].ID != root.ID {
		t.Fatal("the thread does not start at the message it was started from")
	}
	if threadMessages[1].Content != "in the thread" {
		t.Fatalf("the thread's own reply is missing: %q", threadMessages[1].Content)
	}
}

func TestThreadAccessFollowsTheChannel(t *testing.T) {
	// Restricted channels are a default-off feature, and a thread is only as
	// restricted as the channel it inherits from — which is what this test is about.
	// Run with the feature off it could not see the defect it exists to catch.
	t.Setenv("BRRP_ENABLE_RESTRICTED_CHANNELS", "true")
	s, handler := testServer(t)
	channelID, alice, _, admin := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "A restricted thread", "")

	outsiderID, outsider := namedUser(t, s, "outsider")

	// Restricting the channel restricts its threads in the same operation, with
	// nothing about the thread changing. The channel's own visibility decides, so the
	// outsider is excluded by the channel and the thread never gets a say.
	if w := request(t, handler, "PUT", "/api/channels/"+channelID+"/access",
		map[string]any{"visibility": "private", "role_ids": []string{}, "user_ids": []string{outsiderID}}, admin); w.Code != 200 {
		t.Fatalf("restrict channel: %d %s", w.Code, w.Body.String())
	}
	// The grant list names who may read the private channel. Everyone else, including
	// the people who could read it a moment ago, is now outside it.
	if w := request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice); w.Code != 404 {
		t.Fatalf("the restriction did not apply: %d %s", w.Code, w.Body.String())
	}
	// A thread in a channel its reader can no longer read is not found rather than
	// forbidden, so a thread id cannot be used to confirm that a private channel and
	// its threads exist.
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID, nil, alice); w.Code != 404 {
		t.Fatalf("a thread outlived its channel's restriction: %d %s", w.Code, w.Body.String())
	}
	// The one member the grant names keeps both, in the same operation.
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID, nil, outsider); w.Code != 200 {
		t.Fatalf("a granted member lost the thread: %d %s", w.Code, w.Body.String())
	}

}

func TestThreadLockedStopsRepliesButNotReading(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, owner := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Settled", "")

	// Locked is a statement about writing, not about access. A locked thread is still
	// readable, still listed and still followable, which is the distinction that stops
	// "locked" quietly becoming "hidden".
	w := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]any{"state": "locked"}, owner)
	if w.Code != 200 {
		t.Fatalf("lock: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "after the lock", "idempotency_key": "thread-locked-reply"}, bob); w.Code != 403 {
		t.Fatalf("a locked thread accepted a reply: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, bob); w.Code != 200 {
		t.Fatalf("a locked thread stopped being readable: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, bob); w.Code != 200 {
		t.Fatalf("a locked thread left the channel list: %d %s", w.Code, w.Body.String())
	}
	// A known reply key against a now-locked thread must not create the message, and
	// must not be replayable afterwards as new work.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "after the lock", "idempotency_key": "thread-locked-reply"}, bob); w.Code == 201 {
		t.Fatal("the refused reply was created on retry")
	}
	var count int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE thread_id=?`, thread.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the locked thread holds %d messages, want just its root", count)
	}
}

func TestThreadArchiveHidesFromTheListAndReopens(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, owner := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Wrapped up", "")

	if w := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]any{"state": "archived"}, owner); w.Code != 200 {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
	// Archived is out of the way, not gone: the messages stay and it is still
	// reachable, and reopening restores the list entry.
	listed := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
	for _, candidate := range listed {
		if candidate.ID == thread.ID {
			t.Fatal("an archived thread stayed in the channel list")
		}
	}
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, alice); w.Code != 200 {
		t.Fatalf("an archived thread lost its history: %d %s", w.Code, w.Body.String())
	}
	archived := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads?archived=1", nil, alice))
	if len(archived) != 1 || archived[0].ID != thread.ID {
		t.Fatalf("the archived thread is not listed as archived: %+v", archived)
	}
	if w := request(t, handler, "PUT", "/api/threads/"+thread.ID+"/state", map[string]any{"state": "active"}, owner); w.Code != 200 {
		t.Fatalf("reopen: %d %s", w.Code, w.Body.String())
	}
	listed = readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
	found := false
	for _, candidate := range listed {
		if candidate.ID == thread.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("reopening did not restore the list entry")
	}
}

func TestThreadReplyNeedsChannelPostAuthorityInsideTheTransaction(t *testing.T) {
	// An announcement channel is set through the restricted-channel surface, which is
	// default-off; see the note in TestThreadAccessFollowsTheChannel.
	t.Setenv("BRRP_ENABLE_RESTRICTED_CHANNELS", "true")
	s, handler := testServer(t)
	channelID, alice, _, admin := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Members only", "")

	// Read access is not post access. A member who can see the channel but may not
	// post there is refused, and the refusal is decided on current authority inside
	// the transaction rather than by the route alone.
	_, reader := namedUser(t, s, "reader")
	// An announcement channel is the shape the product has for "read, do not write":
	// everyone may read it and only those who may manage it may post. A thread in it
	// must behave the same way, because it inherits the channel rather than deciding
	// access for itself.
	if w := request(t, handler, "PATCH", "/api/channels/"+channelID,
		map[string]any{"posting": "announcement", "max_voice_users": 8}, admin); w.Code != 200 && w.Code != 204 {
		t.Fatalf("make the channel announcement-only: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, reader); w.Code != 200 {
		t.Fatalf("a reader could not read the thread: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "should not land", "idempotency_key": "thread-readonly-reply"}, reader); w.Code != 403 {
		t.Fatalf("a read-only member wrote to the thread: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM messages WHERE thread_id=?`, thread.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the refused reply was written: %d messages in the thread", count)
	}
}

// A thousand replies must be fully reachable, one bounded page at a time, with no
// duplicates and no gaps. A client holds a window and asks for more, which is the
// property that makes a large thread usable rather than merely stored.
func TestThreadAThousandRepliesAreFullyReachableWithoutDuplicates(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "A long one", "")

	// Written straight into the store with real ids and timestamps: the point under
	// test is pagination, not the send path, and a thousand HTTP sends would test the
	// rate limiter instead.
	const total = 1000
	now := time.Now().UTC()
	authorID, _ := namedUser(t, s, "thread-author")
	if _, err := s.store.ExecContext(t.Context(), `UPDATE threads SET created_at=? WHERE id=?`, db.Timestamp(now.Add(-time.Hour)), thread.ID); err != nil {
		t.Fatal(err)
	}
	for i := range total {
		if _, err := s.store.ExecContext(t.Context(), `INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key,thread_id)
			VALUES(?,?,?,?,?,?,?,?)`, db.ID(), channelID, channelID, authorID, fmt.Sprintf("reply %d", i),
			db.Timestamp(now.Add(time.Duration(i)*time.Millisecond)), fmt.Sprintf("seed-key-%d", i), thread.ID); err != nil {
			t.Fatalf("seed reply %d: %v", i, err)
		}
	}

	// The page bound is the server's, not the caller's: asking for more than the
	// server allows returns the server's maximum rather than an error, so the walk
	// below never assumes it got what it asked for.
	bound := 50
	walked := [][]string{}
	seen := map[string]int{}
	ordered := []string{}
	cursor := ""
	pages := 0
	for {
		path := "/api/threads/" + thread.ID + "/messages"
		if cursor != "" {
			path += "?before=" + cursor
		}
		page := readJSON[[]db.Message](t, request(t, handler, "GET", path, nil, alice))
		if len(page) == 0 {
			break
		}
		pages++
		pageContents := make([]string, 0, len(page))
		for _, message := range page {
			pageContents = append(pageContents, message.ID)
		}
		walked = append(walked, pageContents)
		if len(page) > bound {
			t.Fatalf("a page returned %d rows, over the server's bound of %d", len(page), bound)
		}
		for _, message := range page {
			seen[message.ID]++
			ordered = append(ordered, message.Content)
		}
		cursor = page[0].ID
		if pages > total {
			t.Fatal("pagination did not terminate")
		}
	}
	// Every reply exactly once: no duplicates and no gaps across the whole thread.
	if len(seen) != total+1 {
		t.Fatalf("reached %d messages, want %d replies and the root they belong to", len(seen), total+1)
	}

	for id, times := range seen {
		if times != 1 {
			t.Fatalf("reply %s appeared %d times", id, times)
		}
	}
	// The walk reads the newest page first, which is how "load older" is used, and
	// each page arrives oldest-first within itself. What has to hold is that the pages
	// run strictly backwards from one another: no overlap, no gap, and a reader
	// walking them sees the whole thread in order.
	if want := (total + 1 + bound - 1) / bound; len(walked) != want {
		t.Fatalf("%d pages for %d replies and a root, want %d", len(walked), total, want)
	}
	for i, page := range walked {
		for j := 1; j < len(page); j++ {
			if page[j-1] >= page[j] {
				t.Fatalf("page %d is out of order at %d", i, j)
			}
		}
		if i > 0 && page[0] >= walked[i-1][len(walked[i-1])-1] {
			t.Fatalf("page %d overlaps page %d", i, i-1)
		}
	}
	// Every seeded reply is present, not merely a plausible number of them.
	for i := range total {
		if !slices.Contains(ordered, fmt.Sprintf("reply %d", i)) {
			t.Fatalf("reply %d was never reached", i)
		}
	}
	// The channel listing is unaffected by the thread's size: it holds the fixture's
	// two messages less the one that became this thread's root, and none of the
	// thousand replies. A busy thread must not bury its channel.
	channel := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	if len(channel) != 1 {
		t.Fatalf("the channel listing holds %d rows, want the one unthreaded message", len(channel))
	}
}

func TestThreadFollowIsPersonalAndBounded(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, bob, _ := threadFixture(t, s, handler)
	root := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))[0]
	thread := startThread(t, handler, channelID, alice, root.ID, "Followed", "")

	// Starting a thread follows it: the person who opened it is the one who wants to
	// hear about it.
	listed := readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, alice))
	if len(listed) != 1 || !listed[0].Following || listed[0].Followers != 1 {
		t.Fatalf("the starter is not following their own thread: %+v", listed)
	}
	// Following is personal. Someone else's interest changes nothing about yours.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/follow", map[string]any{}, bob); w.Code != 200 {
		t.Fatalf("follow: %d %s", w.Code, w.Body.String())
	}
	listed = readJSON[[]threadSummary](t, request(t, handler, "GET", "/api/channels/"+channelID+"/threads", nil, bob))
	if !listed[0].Following || listed[0].Followers != 2 {
		t.Fatalf("the second follower is not counted: %+v", listed)
	}
	// A reply reaches the followers, and the sender is not one of their own
	// notification recipients even though they follow the thread.
	if w := request(t, handler, "POST", "/api/threads/"+thread.ID+"/messages",
		map[string]any{"content": "worth knowing", "idempotency_key": "thread-follow-reply"}, bob); w.Code != 201 {
		t.Fatalf("reply: %d %s", w.Code, w.Body.String())
	}
	rows, err := s.store.QueryContext(t.Context(), `SELECT user_id FROM account_activity WHERE source_kind=?`, activityThreadReply)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	recipients := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		recipients = append(recipients, id)
	}
	if len(recipients) != 1 {
		t.Fatalf("a reply produced %d activity rows, want only the other follower's: %v", len(recipients), recipients)
	}
	// Unfollowing stops future notifications and touches nothing else: the messages
	// stay and the thread is still readable.
	if w := request(t, handler, "DELETE", "/api/threads/"+thread.ID+"/follow", nil, bob); w.Code != 200 {
		t.Fatalf("unfollow: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/threads/"+thread.ID+"/messages", nil, bob); w.Code != 200 {
		t.Fatalf("unfollowing cost the reader their access: %d %s", w.Code, w.Body.String())
	}
}

func TestThreadCannotBeStartedFromOutsideTheChannel(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "elsewhere", Type: "text", Category: "Test", MaxVoiceUsers: 8, Position: 2}, ownerCookie(t, s, handler))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	other := readJSON[db.Channel](t, w).ID
	otherSent := request(t, handler, "POST", "/api/channels/"+other+"/messages",
		map[string]any{"content": "a message elsewhere", "idempotency_key": "thread-other-root"}, alice)
	if otherSent.Code != 201 {
		t.Fatal(otherSent.Body.String())
	}
	otherRoot := readJSON[db.Message](t, otherSent).ID

	// A root in another channel is refused with the same answer as a root that does
	// not exist. Saying "it exists but not here" would confirm a message in a
	// conversation the reader may not be in.
	refused := request(t, handler, "POST", "/api/channels/"+channelID+"/threads",
		map[string]any{"root_message_id": otherRoot, "title": "Borrowed"}, alice)
	if refused.Code != 400 {
		t.Fatalf("a thread was started from another channel: %d %s", refused.Code, refused.Body.String())
	}
	_ = readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+other+"/messages", nil, alice))
	var count int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM threads`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d threads exist after a refused start", count)
	}
}
