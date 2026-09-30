package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// textChannelID returns the workspace's first text channel, which is where a send test posts.
func textChannelID(t *testing.T, s *Server) string {
	t.Helper()
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		if channel.Type == "text" {
			return channel.ID
		}
	}
	t.Fatal("the workspace has no text channel")
	return ""
}

// messagesIn counts one conversation's stored messages, so a test asserts on what was stored
// rather than on what a response claimed.
func messagesIn(t *testing.T, s *Server, channelID string) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM messages WHERE channel_id=?`, channelID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// sendMessage posts one message with an explicit operation key.
func sendMessage(handler http.Handler, channelID, key, content string) *httptest.ResponseRecorder {
	return performRequest(handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: content, IdempotencyKey: key}, nil)
}

// TestASendReceiptIsIdempotentAndNeverRecreatesDeletedContent is the core WP-09 receipt
// contract: one acknowledged send maps to one stored message, a retry after a lost response
// returns that message, and a retry after the content was deleted returns a conflict instead of
// creating the message again. The old design relied on the message row's own unique index, which
// disappears with the row it guards.
func TestASendReceiptIsIdempotentAndNeverRecreatesDeletedContent(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)

	created := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the acknowledged content", IdempotencyKey: "receipt-lost-ack-1"}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", created.Code, created.Body.String())
	}
	message := readJSON[db.Message](t, created)
	if message.ConversationID != channelID {
		t.Fatalf("a new message did not record its conversation scope: %+v", message)
	}
	var scope string
	if err := s.store.QueryRow(`SELECT COALESCE(conversation_id,'') FROM messages WHERE id=?`, message.ID).Scan(&scope); err != nil {
		t.Fatal(err)
	}
	if scope != channelID {
		t.Fatalf("stored conversation scope = %q, want %q", scope, channelID)
	}

	// The response was lost and the client retries: the committed message comes back, and no
	// second message is created.
	replay := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the acknowledged content", IdempotencyKey: "receipt-lost-ack-1"}, admin)
	if replay.Code != http.StatusOK {
		t.Fatalf("a replay of a committed send answered %d %s", replay.Code, replay.Body.String())
	}
	if again := readJSON[db.Message](t, replay); again.ID != message.ID {
		t.Fatalf("a replay returned a different message: %s, want %s", again.ID, message.ID)
	}
	if count := messagesIn(t, s, channelID); count != 1 {
		t.Fatalf("a replay created %d messages, want 1", count)
	}

	// The message is deleted before the retry arrives. The receipt outlives it: the retry is a
	// conflict and the message is not recreated.
	if w := request(t, handler, "DELETE", "/api/messages/"+message.ID, map[string]any{"confirm": message.ID}, admin); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	retry := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the acknowledged content", IdempotencyKey: "receipt-lost-ack-1"}, admin)
	if retry.Code != http.StatusConflict {
		t.Fatalf("a retry after deletion answered %d %s", retry.Code, retry.Body.String())
	}
	if !strings.Contains(retry.Body.String(), "gone") {
		t.Fatalf("the retry after deletion did not state the committed outcome: %s", retry.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 0 {
		t.Fatalf("a retry after deletion recreated %d messages", count)
	}

	// The permitted control: a new operation key in the same conversation still sends.
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "a genuinely new message", IdempotencyKey: "receipt-new-2"}, admin); w.Code != http.StatusCreated {
		t.Fatalf("a new key was refused: %d %s", w.Code, w.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 1 {
		t.Fatalf("the control left %d messages, want 1", count)
	}
}

// TestAnOperationKeyIsBoundToItsIntent covers the conflict half of the contract: the operation
// identity is the account and the key, and the destination travels in the intent, so reusing a
// key for other content or another conversation conflicts instead of silently returning or
// creating unrelated work (review 6, R6-04).
func TestAnOperationKeyIsBoundToItsIntent(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	other := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "second", Type: "text", MaxVoiceUsers: 8, Visibility: "public"}, admin))

	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "first intent", IdempotencyKey: "intent-key-1"}, admin); w.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	changed := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "different content under the same key", IdempotencyKey: "intent-key-1"}, admin)
	if changed.Code != http.StatusConflict {
		t.Fatalf("a reused key with different content answered %d %s", changed.Code, changed.Body.String())
	}
	if !strings.Contains(changed.Body.String(), "different message") {
		t.Fatalf("the intent conflict did not explain itself: %s", changed.Body.String())
	}
	// A different destination under the same key is the same conflict, not a second message in
	// the other conversation.
	moved := request(t, handler, "POST", "/api/channels/"+other.ID+"/messages",
		messageInput{Content: "first intent", IdempotencyKey: "intent-key-1"}, admin)
	if moved.Code != http.StatusConflict {
		t.Fatalf("a reused key in another conversation answered %d %s", moved.Code, moved.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 1 {
		t.Fatalf("the destination conflict left %d messages in the first conversation, want 1", count)
	}
	if count := messagesIn(t, s, other.ID); count != 0 {
		t.Fatalf("the destination conflict created %d messages in the second conversation", count)
	}
	// The receipt records the conversation the operation was committed in.
	var recorded string
	if err := s.store.QueryRow(`SELECT conversation_id FROM message_operations WHERE account_id=? AND idempotency_key=?`, sessionUserID(t, s, admin), "intent-key-1").Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != channelID {
		t.Fatalf("receipt conversation = %q, want %q", recorded, channelID)
	}
}

// TestAnExpiredReceiptIsNeverNewWork covers server-enforced expiry: once the retry window has
// closed and the content is gone, the outcome is a distinct expired answer that tells the client
// to recover its text and send again with a new key. The retry never becomes a new send by
// accident, and the key stays claimed until pruning.
func TestAnExpiredReceiptIsNeverNewWork(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	created := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "an old acknowledged send", IdempotencyKey: "expired-key-1"}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("send: %d %s", created.Code, created.Body.String())
	}
	message := readJSON[db.Message](t, created)
	accountID := sessionUserID(t, s, admin)

	// The window has long closed and the content is gone: exactly the state a delayed retry of a
	// purged message finds.
	old := db.Timestamp(time.Now().Add(-48 * time.Hour))
	if _, err := s.store.Exec(`UPDATE message_operations SET created_at=?,expires_at=? WHERE account_id=? AND idempotency_key=?`, old, old, accountID, "expired-key-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`DELETE FROM messages WHERE id=?`, message.ID); err != nil {
		t.Fatal(err)
	}
	retry := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "an old acknowledged send", IdempotencyKey: "expired-key-1"}, admin)
	if retry.Code != http.StatusConflict {
		t.Fatalf("an expired operation answered %d %s", retry.Code, retry.Body.String())
	}
	if !strings.Contains(retry.Body.String(), "expired") {
		t.Fatalf("an expired operation did not answer with the expired outcome: %s", retry.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 0 {
		t.Fatalf("an expired retry recreated %d messages", count)
	}
	// The permitted control: an explicit new send with a new key is a new operation.
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the recovered text, sent deliberately", IdempotencyKey: "expired-key-2"}, admin); w.Code != http.StatusCreated {
		t.Fatalf("an explicit new send was refused: %d %s", w.Code, w.Body.String())
	}
	// Retention must not turn a delayed original request into new work. Inside the
	// detailed-receipt window the original metadata is retained.
	if _, err := s.store.PruneSendReceipts(t.Context(), s.store, db.Timestamp(time.Now().Add(-receiptPruneAfter))); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=? AND idempotency_key=?`, accountID, "expired-key-1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatal("a receipt inside the tombstone window was pruned, which would re-admit its key")
	}
	// Past the detailed-receipt window it becomes a minimal terminal tombstone.
	if _, err := s.store.PruneSendReceipts(t.Context(), s.store, db.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=? AND idempotency_key=?`, accountID, "expired-key-1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatal("compaction released an old operation key")
	}
	compacted, err := s.store.Receipt(t.Context(), s.store, accountID, "expired-key-1")
	if err != nil || compacted.ConversationID != "" || compacted.IntentDigest != "" || compacted.CreatedAt != "" || compacted.ExpiresAt != "" || !compacted.Deleted() {
		t.Fatalf("terminal receipt kept unnecessary metadata: %+v, %v", compacted, err)
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "an old acknowledged send", IdempotencyKey: "expired-key-1"}, admin); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "expired") {
		t.Fatalf("a compacted operation was not refused: %d %s", w.Code, w.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 1 {
		t.Fatalf("late retry recreated content after compaction: %d messages", count)
	}
}

func TestLegacyReceiptCannotReplayIntoAnotherConversation(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "legacy-sender")
	private := privateTextChannel(t, handler, admin, "legacy-private", memberID)
	public := textChannelID(t, s)
	now, messageID := db.Now(), db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?,?)`, messageID, private.ID, private.ID, memberID, "private legacy content", now, "legacy-private-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO message_operations(account_id,idempotency_key,conversation_id,intent_digest,state,result_message_id,created_at,expires_at) VALUES(?,?,?,'','committed',?,?,?)`, memberID, "legacy-private-key", private.ID, messageID, now, now); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "PUT", "/api/channels/"+private.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	w := request(t, handler, "POST", "/api/channels/"+public+"/messages", messageInput{Content: "different destination", IdempotencyKey: "legacy-private-key"}, member)
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "private legacy content") {
		t.Fatalf("legacy receipt disclosed old room content: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/channels/"+public+"/messages", messageInput{Content: "permitted new send", IdempotencyKey: "legacy-permitted-key"}, member); w.Code != 201 {
		t.Fatalf("control: %d %s", w.Code, w.Body.String())
	}
}

func TestSendIntentCanonicalizesAttachmentOrder(t *testing.T) {
	ids := []string{"second", "first"}
	if sendIntentDigest("room", "", "text", ids, "reply") != sendIntentDigest("room", "", "text", []string{"first", "second"}, "reply") {
		t.Fatal("the same attachment set changed the send intent")
	}
	if ids[0] != "second" {
		t.Fatal("canonicalization mutated the input")
	}
	if sendIntentDigest("room", "", "text", ids, "reply") == sendIntentDigest("other", "", "text", ids, "reply") {
		t.Fatal("destination is not bound to intent")
	}
	// The thread is part of the destination, not decoration: the same words under the
	// same key in two threads of one channel are two sends (V14-04).
	if sendIntentDigest("room", "", "text", ids, "reply") == sendIntentDigest("room", "thread-1", "text", ids, "reply") {
		t.Fatal("the thread is not bound to the send intent")
	}
}

// TestConcurrentRetriesOfOneOperationCreateOneMessage is the race the operation claim exists for:
// simultaneous requests with the same key must produce exactly one message, and every one of them
// answers from the committed record rather than with a server error.
func TestConcurrentRetriesOfOneOperationCreateOneMessage(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)

	const attempts = 8
	start := make(chan struct{})
	statuses := make([]int, attempts)
	var waiting sync.WaitGroup
	for i := range statuses {
		waiting.Add(1)
		go func(slot int) {
			defer waiting.Done()
			<-start
			statuses[slot] = performRequest(handler, "POST", "/api/channels/"+channelID+"/messages",
				messageInput{Content: "one logical send", IdempotencyKey: "race-key-1"}, admin).Code
		}(i)
	}
	close(start)
	waiting.Wait()
	for slot, status := range statuses {
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("attempt %d answered %d, want 201 for the winner or 200 for a replay", slot, status)
		}
	}
	if count := messagesIn(t, s, channelID); count != 1 {
		t.Fatalf("%d concurrent retries created %d messages, want 1", attempts, count)
	}
	var receipts int
	if err := s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=? AND idempotency_key=?`, sessionUserID(t, s, admin), "race-key-1").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("concurrent retries left %d operation records, want 1", receipts)
	}
}

// TestALegacyBackfilledReceiptNeverRecreatesContent simulates exactly what the schema 6 migration
// writes for a message that predates receipts: a receipt with an empty intent digest. A replay is
// answered from the message, and once the message is gone the receipt answers a conflict, so a
// historical key cannot recreate deleted content either.
func TestALegacyBackfilledReceiptNeverRecreatesContent(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	accountID := sessionUserID(t, s, admin)

	legacyID, now := db.ID(), db.Now()
	if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?,?)`,
		legacyID, channelID, channelID, accountID, "a message from before the migration", now, "legacy-key-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO message_operations(account_id,idempotency_key,conversation_id,intent_digest,state,result_message_id,created_at,expires_at) VALUES(?,?,?,'','committed',?,?,?)`,
		accountID, "legacy-key-1", channelID, legacyID, now, now); err != nil {
		t.Fatal(err)
	}

	replay := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "a message from before the migration", IdempotencyKey: "legacy-key-1"}, admin)
	if replay.Code != http.StatusOK {
		t.Fatalf("a legacy replay answered %d %s", replay.Code, replay.Body.String())
	}
	if again := readJSON[db.Message](t, replay); again.ID != legacyID {
		t.Fatalf("a legacy replay returned %s, want %s", again.ID, legacyID)
	}
	// The legacy row also proves the compatibility reader: it has no conversation_id, so its
	// conversation scope is resolved from its channel.
	if again := readJSON[db.Message](t, replay); again.ConversationID != channelID {
		t.Fatalf("a legacy message resolved conversation %q, want %q", again.ConversationID, channelID)
	}
	if _, err := s.store.Exec(`DELETE FROM messages WHERE id=?`, legacyID); err != nil {
		t.Fatal(err)
	}
	var state, result string
	if err := s.store.QueryRow(`SELECT state,COALESCE(result_message_id,'') FROM message_operations WHERE account_id=? AND idempotency_key=?`, accountID, "legacy-key-1").Scan(&state, &result); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" || result != "" {
		t.Fatalf("deleting the message did not mark its receipt: %+v", state)
	}
	retry := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "a message from before the migration", IdempotencyKey: "legacy-key-1"}, admin)
	if retry.Code != http.StatusConflict {
		t.Fatalf("a legacy retry after deletion answered %d %s", retry.Code, retry.Body.String())
	}
	if count := messagesIn(t, s, channelID); count != 0 {
		t.Fatalf("a legacy retry recreated %d messages", count)
	}
}

// TestAChannelConversationIsCreatedAndReadable covers the mapping half of the slice: every channel
// has exactly one conversation whose id is the channel id, a message written through the API
// records it, and a channel created after the migration gets its own conversation in the same
// transaction.
func TestAChannelConversationIsCreatedAndReadable(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	created := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "later", Type: "text", MaxVoiceUsers: 8, Visibility: "public"}, admin))

	var conversations int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversations WHERE kind='channel'`).Scan(&conversations); err != nil {
		t.Fatal(err)
	}
	var channels int
	if err := s.store.QueryRow(`SELECT count(*) FROM channels`).Scan(&channels); err != nil {
		t.Fatal(err)
	}
	if conversations != channels {
		t.Fatalf("%d conversations for %d channels", conversations, channels)
	}
	var mapped int
	if err := s.store.QueryRow(`SELECT count(*) FROM conversations WHERE id=? AND channel_id=?`, created.ID, created.ID).Scan(&mapped); err != nil {
		t.Fatal(err)
	}
	if mapped != 1 {
		t.Fatal("a channel created after the migration has no conversation of its own")
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM conversations WHERE id=?`, channelID).Scan(&mapped); err != nil {
		t.Fatal(err)
	}
	if mapped != 1 {
		t.Fatal("the migrated channel has no conversation")
	}
}
