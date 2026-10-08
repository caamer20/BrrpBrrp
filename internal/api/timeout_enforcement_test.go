package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// The timeout contracts review 17 left open: a committed apply or lift is audited
// exactly once and a rolled-back one records nothing; a suspension is enforced live
// where it applies — community voice and community streams end — while private reads,
// messages and calls keep working, because a timeout is not an account-wide ban
// (ADR 0018).

func auditCount(t *testing.T, s *Server, action, target string) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action=? AND target_id=?`, action, target).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestATimeoutIsAuditedOnceAndOnlyWhenItCommits(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, _ := memberAccount(t, s, "timeout_audit_member")
	_, bystander := memberAccount(t, s, "timeout_audit_bystander")
	moderator := sessionUserID(t, s, admin)

	// A non-moderator's attempt commits nothing, so it records nothing: an audit row
	// for a rolled-back mutation is the misleading half of the same defect.
	refused := request(t, handler, "POST", "/api/admin/users/"+memberID+"/timeout",
		map[string]any{"minutes": 10, "reason": "not mine to give"}, bystander)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a non-moderator's timeout = %d, want 403", refused.Code)
	}
	if n := auditCount(t, s, "account.timed_out", memberID); n != 0 {
		t.Fatalf("a refused timeout was audited %d times", n)
	}

	applied := request(t, handler, "POST", "/api/admin/users/"+memberID+"/timeout",
		map[string]any{"minutes": 10, "reason": "cooling off"}, admin)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
	}
	if n := auditCount(t, s, "account.timed_out", memberID); n != 1 {
		t.Fatalf("a committed timeout was audited %d times, want 1", n)
	}
	var actor string
	if err := s.store.QueryRow(`SELECT actor_id FROM audit_logs WHERE action='account.timed_out' AND target_id=?`, memberID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != moderator {
		t.Fatalf("the timeout audit names actor %q, want the moderator", actor)
	}

	lifted := request(t, handler, "DELETE", "/api/admin/users/"+memberID+"/timeout", nil, admin)
	if lifted.Code != http.StatusOK {
		t.Fatalf("lift: %d %s", lifted.Code, lifted.Body.String())
	}
	if n := auditCount(t, s, "account.timeout_lifted", memberID); n != 1 {
		t.Fatalf("a committed lift was audited %d times, want 1", n)
	}
	// Lifting nothing records nothing.
	if again := request(t, handler, "DELETE", "/api/admin/users/"+memberID+"/timeout", nil, admin); again.Code != http.StatusNotFound {
		t.Fatalf("a second lift = %d, want 404", again.Code)
	}
	if n := auditCount(t, s, "account.timeout_lifted", memberID); n != 1 {
		t.Fatalf("lifting nothing was audited %d times", n)
	}
}

func TestALapsedTimeoutIsAuditedByTheSweepExactlyOnce(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "timeout_expiry_member")
	if w := request(t, handler, "POST", "/api/admin/users/"+memberID+"/timeout",
		map[string]any{"minutes": 5, "reason": "brief"}, admin); w.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	// The clock reaches the expiry while the account is still suspended.
	if _, err := s.store.Exec(`UPDATE account_timeouts SET expires_at=? WHERE user_id=?`,
		db.Timestamp(time.Now().UTC().Add(-time.Minute)), memberID); err != nil {
		t.Fatal(err)
	}
	swept, err := s.expireTimeouts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("the sweep expired %d timeouts, want 1", swept)
	}
	if n := auditCount(t, s, "account.timeout_expired", memberID); n != 1 {
		t.Fatalf("a lapsed timeout was audited %d times, want 1", n)
	}
	// No account did this: the audit row names no actor, so the view can say the clock.
	var actor string
	if err := s.store.QueryRow(`SELECT COALESCE(actor_id,'') FROM audit_logs WHERE action='account.timeout_expired' AND target_id=?`, memberID).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if actor != "" {
		t.Fatalf("the expiry audit names actor %q, want none", actor)
	}
	// The suspension is actually over: community reads work again without a login
	// having cleared the row first, and a second sweep finds nothing to record.
	channel := textChannelID(t, s)
	if read := request(t, handler, "GET", "/api/channels/"+channel+"/messages", nil, member); read.Code != http.StatusOK {
		t.Fatalf("after the lapse a community read = %d: %s", read.Code, read.Body.String())
	}
	again, err := s.expireTimeouts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("the second sweep expired %d timeouts, want 0", again)
	}
	if n := auditCount(t, s, "account.timeout_expired", memberID); n != 1 {
		t.Fatalf("the lapse was audited %d times", n)
	}

	// The point-of-use lift is the same change by the other path, and it must not
	// double-record: apply, lapse the row, and let a state read clear it.
	if w := request(t, handler, "POST", "/api/admin/users/"+memberID+"/timeout",
		map[string]any{"minutes": 5, "reason": "again"}, admin); w.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.store.Exec(`UPDATE account_timeouts SET expires_at=? WHERE user_id=?`,
		db.Timestamp(time.Now().UTC().Add(-time.Minute)), memberID); err != nil {
		t.Fatal(err)
	}
	if suspended, _ := s.timeoutState(t.Context(), memberID); suspended {
		t.Fatal("a lapsed timeout was still reported as suspended")
	}
	if n := auditCount(t, s, "account.timeout_expired", memberID); n != 2 {
		t.Fatalf("the lapse was audited %d times, want one more", n)
	}
}

func TestACommunityTimeoutEndsCommunityVoiceAndKeepsPrivateCalls(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, bob, _ := privateCallPair(t, s, handler)
	admin := ownerCookie(t, s, handler)
	aliceID, bobID := memberIDOf(t, s, "call_alice"), memberIDOf(t, s, "call_bob")
	voiceChannel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "community-voice", Type: "voice", MaxVoiceUsers: 4}, admin))

	// Alice is in a community room; Bob is in the private call they consented to.
	if err := s.voice.Join(aliceID, "alice-tab", voiceChannel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.voice.Leave(aliceID, "") })
	if err := s.voice.Join(bobID, "bob-tab", directID, maxPrivateCallParticipants, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.voice.Leave(bobID, "") })
	inVoice := func(userID string) bool {
		for _, state := range s.voice.States() {
			if state.UserID == userID {
				return true
			}
		}
		return false
	}
	if !inVoice(aliceID) || !inVoice(bobID) {
		t.Fatal("the fixture did not put both accounts in their rooms")
	}

	// Suspending Bob reaches the community half and nothing else: he stays in the
	// private call, and a private read and a private message still work.
	if w := request(t, handler, "POST", "/api/admin/users/"+bobID+"/timeout",
		map[string]any{"minutes": 15, "reason": "community cooling off"}, admin); w.Code != http.StatusOK {
		t.Fatalf("timeout Bob: %d %s", w.Code, w.Body.String())
	}
	if !inVoice(bobID) {
		t.Fatal("a community timeout evicted Bob from a private call")
	}
	privateRead := request(t, handler, "GET", "/api/conversations/"+directID+"/messages", nil, bob)
	if privateRead.Code != http.StatusOK {
		t.Fatalf("a suspended account cannot read its private conversation: %d %s", privateRead.Code, privateRead.Body.String())
	}
	privateSend := request(t, handler, "POST", "/api/conversations/"+directID+"/messages",
		map[string]any{"content": "still able to answer privately", "idempotency_key": "timeout-private-send"}, bob)
	if privateSend.Code != 201 {
		t.Fatalf("a suspended account cannot send privately: %d %s", privateSend.Code, privateSend.Body.String())
	}

	// The community half is live, not next-request: Alice is evicted the moment her
	// suspension commits, and community reads are refused with the explanation.
	if w := request(t, handler, "POST", "/api/admin/users/"+aliceID+"/timeout",
		map[string]any{"minutes": 15, "reason": "community cooling off"}, admin); w.Code != http.StatusOK {
		t.Fatalf("timeout Alice: %d %s", w.Code, w.Body.String())
	}
	if inVoice(aliceID) {
		t.Fatal("a suspended account kept its place in a community voice room")
	}
	communityRead := request(t, handler, "GET", "/api/channels/"+voiceChannel.ID+"/messages", nil, alice)
	if communityRead.Code != http.StatusForbidden {
		t.Fatalf("a suspended account's community read = %d, want 403", communityRead.Code)
	}
}

// conversationUpload stages a file bound to a private conversation, with real bytes
// big enough to still be in flight when the test acts.
func conversationUpload(t *testing.T, h http.Handler, cookie *http.Cookie, conversation, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/uploads?conversation_id="+conversation, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "https://localhost:8080")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestASuspendedAccountStopsStreamingCommunityContentAndKeepsPrivateStreams(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := restrictedServer(t)
	directID, alice, _, _ := privateCallPair(t, s, handler)
	admin := ownerCookie(t, s, handler)
	aliceID := memberIDOf(t, s, "call_alice")

	// A private file: staged against the conversation and claimed by a message in it.
	privateBlob := strings.Repeat("private-bytes", 256*1024)
	privateUpload := readJSON[db.Attachment](t, conversationUpload(t, handler, alice, directID, "private.bin", privateBlob))
	privateMessage := request(t, handler, "POST", "/api/conversations/"+directID+"/messages",
		messageInput{Content: "the private file", IdempotencyKey: "timeout-private-file", AttachmentIDs: []string{privateUpload.ID}}, alice)
	if privateMessage.Code != http.StatusCreated {
		t.Fatalf("private send: %d %s", privateMessage.Code, privateMessage.Body.String())
	}

	// A community file, in a channel both accounts can read.
	communityBlob := strings.Repeat("community-bytes", 256*1024)
	communityUpload := readJSON[db.Attachment](t, sendFile(t, handler, admin, "community.bin", communityBlob))
	channelID := textChannelID(t, s)
	communityMessage := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the community file", IdempotencyKey: "timeout-community-file", AttachmentIDs: []string{communityUpload.ID}}, admin)
	if communityMessage.Code != http.StatusCreated {
		t.Fatalf("community send: %d %s", communityMessage.Code, communityMessage.Body.String())
	}

	privateStream, privateHead, resumePrivateStream := startDownload(t, s, handler, alice, privateUpload.ID)
	communityStream, communityHead, resumeCommunityStream := startDownload(t, s, handler, alice, communityUpload.ID)
	privateSize, communitySize := privateUpload.FileSize, communityUpload.FileSize

	// The suspension lands while both responses are open.
	if w := request(t, handler, "POST", "/api/admin/users/"+aliceID+"/timeout",
		map[string]any{"minutes": 15, "reason": "community cooling off"}, admin); w.Code != http.StatusOK {
		t.Fatalf("timeout: %d %s", w.Code, w.Body.String())
	}
	resumePrivateStream()
	resumeCommunityStream()
	if delivered := drain(t, communityStream, communityHead); delivered >= communitySize {
		t.Fatalf("community content kept streaming through a suspension: %d of %d bytes", delivered, communitySize)
	}
	if delivered := drain(t, privateStream, privateHead); delivered != privateSize {
		t.Fatalf("a private stream stopped with the community one: %d of %d bytes", delivered, privateSize)
	}
}
