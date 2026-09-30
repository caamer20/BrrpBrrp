package api

import (
	"testing"

	"brrpbrrp/internal/db"
)

// The same key and the same words, sent to a *different* thread in the same channel,
// are two sends.
//
// The operation's identity is the account and the key, and the destination travels in
// the intent — so reusing a key for other work is a conflict rather than a silent
// replay. The thread is part of the destination: a member who asks for the same words
// in two threads has asked for two messages.
//
// **On how much this test proves.** The message table carries
// `UNIQUE(user_id, idempotency_key)` as a second guard, and that guard is why the
// observed failure was a 409 rather than a wrong-thread replay. So the thread in the
// digest is not the only thing standing between the two cases, and removing it alone
// does not make this test fail.
//
// It is still required. The unique index is the *legacy* guard the receipts
// documentation describes as the second of two: it disappears with the row it guards,
// so a retry after deletion could recreate content, which is exactly what the receipt
// exists to prevent. A receipt whose intent omits the thread would accept a replay
// across two threads the moment the first message was deleted — the index would no
// longer be there to refuse it. The digest is the durable guard; the index is the
// accident that has been catching the everyday case.
func TestAThreadIsPartOfTheSendIntent(t *testing.T) {
	s, handler := testServer(t)
	channelID, alice, _, _ := threadFixture(t, s, handler)
	roots := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, alice))
	first := startThread(t, handler, channelID, alice, roots[0].ID, "the first thread", "")
	second := startThread(t, handler, channelID, alice, roots[1].ID, "the second thread", "")

	const key = "thread-intent-key"
	const words = "identical words in either thread"

	intoSecond := request(t, handler, "POST", "/api/threads/"+second.ID+"/messages",
		map[string]any{"content": words, "idempotency_key": key}, alice)
	if intoSecond.Code != 201 {
		t.Fatalf("send into the second thread: %d %s", intoSecond.Code, intoSecond.Body.String())
	}
	secondMessageID := readJSON[map[string]any](t, intoSecond)["id"].(string)

	intoFirst := request(t, handler, "POST", "/api/threads/"+first.ID+"/messages",
		map[string]any{"content": words, "idempotency_key": key}, alice)
	if intoFirst.Code != 409 {
		t.Fatalf("the same key in another thread answered %d, want 409: %s", intoFirst.Code, intoFirst.Body.String())
	}
	if strings := intoFirst.Body.String(); strings != "" && !stringsContain(strings, "different message") {
		t.Fatalf("the conflict did not explain itself: %s", strings)
	}
	// The thread that received the message holds it, and the other one does not. The
	// failure this prevents is a member looking at a thread and finding nothing in it
	// after a send the server accepted.
	for _, thread := range []struct {
		id      string
		label   string
		present bool
	}{{second.ID, "the second", true}, {first.ID, "the first", false}} {
		messages := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/threads/"+thread.id+"/messages", nil, alice))
		found := false
		for _, message := range messages {
			if message.Content == words {
				found = true
			}
			if message.ID == secondMessageID && !thread.present {
				t.Fatalf("the second thread's message is inside %s", thread.label)
			}
		}
		if found != thread.present {
			t.Fatalf("%s thread: message present = %v, want %v", thread.label, found, thread.present)
		}
	}
	// A *new* key in the first thread is an ordinary send, which is the permitted
	// control: the conflict is about the key, not about the thread.
	if w := request(t, handler, "POST", "/api/threads/"+first.ID+"/messages",
		map[string]any{"content": "a new key, a new message", "idempotency_key": "thread-intent-key-2"}, alice); w.Code != 201 {
		t.Fatalf("a new key in the first thread was refused: %d %s", w.Code, w.Body.String())
	}
	// And the same key against the *same* thread is still a replay, not a conflict —
	// the retry contract is unchanged by the thread becoming part of the destination.
	replay := request(t, handler, "POST", "/api/threads/"+second.ID+"/messages",
		map[string]any{"content": words, "idempotency_key": key}, alice)
	if replay.Code != 200 {
		t.Fatalf("a replay into its own thread answered %d, want 200: %s", replay.Code, replay.Body.String())
	}
	if again := readJSON[map[string]any](t, replay)["id"].(string); again != secondMessageID {
		t.Fatalf("a replay returned a different message: %s, want %s", again, secondMessageID)
	}
}

func stringsContain(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOfString(haystack, needle) >= 0
}

func indexOfString(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
