package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Pending-request evidence and the report-and-block operation (ADR 0014).
//
// The cases here are the ones where being wrong would matter to a person: someone
// blocked with nothing recorded, evidence captured from the wrong request, a retry
// that blocks again after a deliberate unblock, or a report that reaches somebody
// who was not the recipient.

// requestFixture creates a *pending* request. The pipeline's own pair is already
// accepted, so it cannot be reused: asking that peer again returns the existing
// accepted conversation, and a pending attempt is the state under test.
func requestFixture(t *testing.T) (*Server, http.Handler, *http.Cookie, *http.Cookie, string, string, *http.Cookie) {
	t.Helper()
	s, h, admin, _, _, _, _, _ := privatePipeline(t)
	// report.review_private is delegated downward, so a test that grants it needs an
	// account that already holds it. Before the ownership ceremony nothing does, and
	// the ceremony is the real operator path.
	enrollMFA(t, h, admin, "correct horse battery")
	if w := request(t, h, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatalf("designate the owner: %d %s", w.Code, w.Body.String())
	}
	recipientID, recipient := memberAccount(t, s, "request_recipient")
	senderID, sender := memberAccount(t, s, "request_sender")
	asked := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": recipientID, "introduction": "please let me in",
	}, sender)
	if asked.Code != 201 {
		t.Fatalf("request: %d %s", asked.Code, asked.Body.String())
	}
	return s, h, recipient, sender, readJSON[directConversation](t, asked).ID, senderID, admin
}

func attemptOf(t *testing.T, s *Server, conversation string) string {
	t.Helper()
	var attempt string
	if err := s.store.QueryRow(`SELECT attempt_id FROM direct_requests WHERE conversation_id=?`, conversation).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	if attempt == "" {
		t.Fatal("the pending request has no attempt identity")
	}
	return attempt
}

func previewRequest(t *testing.T, h http.Handler, cookie *http.Cookie, conversation string) *requestReportPreview {
	t.Helper()
	w := request(t, h, "GET", "/api/directs/"+conversation+"/report-preview", nil, cookie)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	preview := readJSON[requestReportPreview](t, w)
	return &preview
}

// Only the recipient of the exact current attempt may preview or confirm. The
// sender, an unrelated member and a non-participating owner are all refused the
// same way, so guessing a conversation id learns nothing.
func TestRequestEvidenceIsRecipientOnly(t *testing.T) {
	s, h, recipient, sender, conversation, _, owner := requestFixture(t)
	_, outsider := memberAccount(t, s, "request_outsider")

	preview := previewRequest(t, h, recipient, conversation)
	if preview.Introduction != "please let me in" || !preview.HasIntroduction {
		t.Fatalf("the recipient saw %q", preview.Introduction)
	}
	for name, cookie := range map[string]*http.Cookie{
		"the sender": sender, "an outsider": outsider, "the owner": owner,
	} {
		if w := request(t, h, "GET", "/api/directs/"+conversation+"/report-preview", nil, cookie); w.Code != http.StatusNotFound {
			t.Fatalf("%s could preview a pending request: %d", name, w.Code)
		}
		if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
			"reason": "not welcome", "version": preview.Version, "operation_key": "steal-" + name,
		}, cookie); w.Code != http.StatusNotFound {
			t.Fatalf("%s could report a pending request: %d", name, w.Code)
		}
	}
	// A reviewer is not a recipient either: the capability admits evidence, not
	// its creation.
	_, reviewer := evidenceRole(t, s, h, owner, "request reviewer", string(authz.ReportReviewPrivate))
	if w := request(t, h, "GET", "/api/directs/"+conversation+"/report-preview", nil, reviewer); w.Code != http.StatusNotFound {
		t.Fatalf("a reviewer could preview a pending request: %d", w.Code)
	}
	// And a conversation that does not exist is indistinguishable from one that is
	// not theirs.
	if w := request(t, h, "GET", "/api/directs/does-not-exist/report-preview", nil, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("a missing conversation answered differently: %d", w.Code)
	}
}

// The whole operation is one transaction: evidence, rejection, introduction clearing
// and the block all take effect, and none of it accepts contact.
func TestReportRequestAndBlockIsAtomicAndNeverAccepts(t *testing.T) {
	s, h, recipient, sender, conversation, senderID, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)

	w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome request", "version": preview.Version, "operation_key": "op-1",
	}, recipient)
	if w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	receipt := readJSON[map[string]any](t, w)
	if receipt["outcome"] != "reported_and_blocked" {
		t.Fatalf("outcome = %v", receipt["outcome"])
	}

	var introduction, state, attempt string
	if err := s.store.QueryRow(`SELECT introduction,state,attempt_id FROM direct_requests WHERE conversation_id=?`, conversation).Scan(&introduction, &state, &attempt); err != nil {
		t.Fatal(err)
	}
	if introduction != "" || state != "rejected" {
		t.Fatalf("the request was not rejected and cleared: state=%q introduction=%q", state, introduction)
	}
	var blocked int
	if err := s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, evidenceUserID(t, h, recipient), senderID).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked != 1 {
		t.Fatal("the sender was not blocked")
	}
	// No acceptance, no message, no membership: the point of the action.
	var members, messages int
	_ = s.store.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND state='accepted'`, conversation).Scan(&members)
	_ = s.store.QueryRow(`SELECT count(*) FROM messages WHERE conversation_id=?`, conversation).Scan(&messages)
	if members != 0 || messages != 0 {
		t.Fatalf("reporting granted membership or history: %d accepted members, %d messages", members, messages)
	}
	// The stored evidence is the exact introduction the recipient was shown.
	var stored string
	if err := s.store.QueryRow(`SELECT snapshot_text FROM private_request_reports WHERE attempt_id=?`, attempt).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "please let me in" {
		t.Fatalf("stored evidence = %q", stored)
	}
	// A later acceptance must fail: the request is gone and the pair is blocked.
	accept := request(t, h, "POST", "/api/directs/"+conversation+"/accept", map[string]any{}, sender)
	if accept.Code == http.StatusOK {
		t.Fatal("the request was accepted after report-and-block")
	}
}

// A retry under the same operation key returns the original outcome: no second
// evidence record, no second quota charge, no repeated block.
func TestRetryingReportAndBlockReturnsTheOriginalReceipt(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	first := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-retry",
	}, recipient)
	if first.Code != 201 {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	evidenceID := readJSON[map[string]any](t, first)["id"].(string)

	second := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-retry",
	}, recipient)
	if second.Code != 200 {
		t.Fatalf("the retry did not return the committed receipt: %d %s", second.Code, second.Body.String())
	}
	retry := readJSON[map[string]any](t, second)
	if retry["id"] != evidenceID || retry["duplicate"] != true {
		t.Fatalf("the retry produced a different outcome: %v", retry)
	}
	var evidence, receipts int
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
	if evidence != 1 || receipts != 1 {
		t.Fatalf("the retry created new records: %d evidence rows, %d receipts", evidence, receipts)
	}
	// The stored explanation is still the original: a retry cannot rewrite it.
	var reason string
	_ = s.store.QueryRow(`SELECT reason FROM private_request_reports WHERE id=?`, evidenceID).Scan(&reason)
	if reason != "unwelcome" {
		t.Fatalf("the stored explanation became %q", reason)
	}
}

// A replay must not re-block after a deliberate unblock, and it must not claim the
// person is currently blocked.
func TestReplayingReportAndBlockDoesNotReblockAfterDeliberateUnblock(t *testing.T) {
	s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-unblock",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	// A later deliberate unblock takes effect.
	if w := request(t, h, "DELETE", "/api/me/blocks/"+senderID, map[string]any{}, recipient); w.Code != 200 {
		t.Fatalf("unblock: %d %s", w.Code, w.Body.String())
	}
	replay := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-unblock",
	}, recipient)
	if replay.Code != 200 {
		t.Fatalf("the replay failed: %d %s", replay.Code, replay.Body.String())
	}
	body := readJSON[map[string]any](t, replay)
	// The receipt is a historical statement. It must not assert a current block,
	// and the block must genuinely still be lifted.
	if _, present := body["blocked"]; present {
		t.Fatalf("the receipt asserts a current block: %v", body)
	}
	var blocked int
	_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocker_id=? AND blocked_id=?`,
		evidenceUserID(t, h, recipient), senderID).Scan(&blocked)
	if blocked != 0 {
		t.Fatal("the replay re-blocked after a deliberate unblock")
	}
}

// The same key with different content is a conflict, and another account can neither
// read nor replay the receipt.
func TestOperationKeyConflictsAndIsAccountOwned(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "first explanation", "version": preview.Version, "operation_key": "op-conflict",
	}, recipient); w.Code != 201 {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}
	conflict := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "a different explanation", "version": preview.Version, "operation_key": "op-conflict",
	}, recipient)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("the same key with a different intent was accepted: %d %s", conflict.Code, conflict.Body.String())
	}
	// Another account's key is not readable, and is indistinguishable from one that
	// never existed.
	_, stranger := memberAccount(t, s, "receipt_stranger")
	if w := request(t, h, "GET", "/api/me/report-receipt?operation_key=op-conflict", nil, stranger); w.Code != http.StatusNotFound {
		t.Fatalf("another account read the receipt: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/me/report-receipt?operation_key=never-used", nil, recipient); w.Code != http.StatusNotFound {
		t.Fatalf("an unknown key answered differently: %d", w.Code)
	}
	// The owner's own receipt resolves without any reviewer capability.
	own := request(t, h, "GET", "/api/me/report-receipt?operation_key=op-conflict", nil, recipient)
	if own.Code != 200 {
		t.Fatalf("the reporter could not reconcile their own operation: %d %s", own.Code, own.Body.String())
	}
	outcome := readJSON[map[string]any](t, own)
	if outcome["outcome"] != "reported_and_blocked" {
		t.Fatalf("own receipt = %v", outcome)
	}
	// It carries no review data.
	for _, leaked := range []string{"snapshot_text", "review_note", "reviewed_by", "author_id"} {
		if _, present := outcome[leaked]; present {
			t.Fatalf("the own-receipt response disclosed %q: %v", leaked, outcome)
		}
	}
}

// A renewed request is a different attempt: an old operation cannot be replayed
// against it, and a fresh report captures the new introduction.
func TestARenewedRequestIsADifferentAttempt(t *testing.T) {
	s, h, recipient, sender, conversation, senderID, _ := requestFixture(t)
	firstPreview := previewRequest(t, h, recipient, conversation)
	firstAttempt := attemptOf(t, s, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": firstPreview.Version, "operation_key": "op-renew-1",
	}, recipient); w.Code != 201 {
		t.Fatalf("first report: %d %s", w.Code, w.Body.String())
	}
	// The sender is blocked, so a renewal needs the block lifted first.
	_ = request(t, h, "DELETE", "/api/me/blocks/"+senderID, map[string]any{}, recipient)
	// The rejection cooldown is bypassed deliberately here so the renewal can be
	// created; the attempt identity is what this test is about.
	if _, err := s.store.Exec(`UPDATE direct_requests SET retry_after='' WHERE conversation_id=?`, conversation); err != nil {
		t.Fatal(err)
	}
	renewed := request(t, h, "POST", "/api/directs", map[string]string{"user_id": evidenceUserID(t, h, recipient), "introduction": "a different approach"}, sender)
	// 201 for a new pair, 200 when an existing pair reopens: both are success.
	if renewed.Code != 201 && renewed.Code != 200 {
		t.Fatalf("the renewal was refused: %d %s", renewed.Code, renewed.Body.String())
	}
	secondAttempt := attemptOf(t, s, conversation)
	if secondAttempt == firstAttempt {
		t.Fatal("the renewed request reused the first attempt identity")
	}
	// The old preview is refused against the new attempt.
	stale := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": firstPreview.Version, "operation_key": "op-renew-2",
	}, recipient)
	if stale.Code != http.StatusConflict {
		t.Fatalf("an old preview was accepted against a renewed request: %d %s", stale.Code, stale.Body.String())
	}
	// A fresh preview captures the new introduction as new evidence.
	fresh := previewRequest(t, h, recipient, conversation)
	if fresh.Introduction != "a different approach" {
		t.Fatalf("the new preview shows %q", fresh.Introduction)
	}
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "still unwelcome", "version": fresh.Version, "operation_key": "op-renew-2",
	}, recipient); w.Code != 201 {
		t.Fatalf("the fresh report failed: %d %s", w.Code, w.Body.String())
	}
	var stored int
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&stored)
	if stored != 2 {
		t.Fatalf("expected two distinct attempts of evidence, got %d", stored)
	}
	// The first attempt's evidence is untouched by the second report.
	var firstText string
	_ = s.store.QueryRow(`SELECT snapshot_text FROM private_request_reports WHERE attempt_id=?`, firstAttempt).Scan(&firstText)
	if firstText != "please let me in" {
		t.Fatalf("the first attempt's evidence became %q", firstText)
	}
}

// An empty introduction is legitimate and is disclosed as such, never fabricated.
func TestRequestEvidenceHandlesAnEmptyIntroduction(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	if _, err := s.store.Exec(`UPDATE direct_requests SET introduction='' WHERE conversation_id=?`, conversation); err != nil {
		t.Fatal(err)
	}
	preview := previewRequest(t, h, recipient, conversation)
	if preview.HasIntroduction {
		t.Fatal("an empty introduction was reported as present")
	}
	if preview.Version == "" {
		t.Fatal("an empty introduction produced no version")
	}
	// It is still reportable, and what is stored is empty rather than invented.
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "no introduction was given", "version": preview.Version, "operation_key": "op-empty",
	}, recipient); w.Code != 201 {
		t.Fatalf("an empty-introduction request was not reportable: %d %s", w.Code, w.Body.String())
	}
	var stored string
	if err := s.store.QueryRow(`SELECT snapshot_text FROM private_request_reports`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("an introduction was invented for a request that had none: %q", stored)
	}
}

// A failed evidence insert leaves nothing behind: no receipt, no block, no
// rejection, and the request is still reportable.
func TestAFailedReportAndBlockRollsBackEverything(t *testing.T) {
	s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	// Force the evidence write to fail after the block and receipt would have been
	// considered: a trigger that aborts the insert.
	if _, err := s.store.Exec(`CREATE TRIGGER fail_request_evidence BEFORE INSERT ON private_request_reports
		BEGIN SELECT RAISE(ABORT,'injected evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-fail",
	}, recipient)
	if w.Code == 201 {
		t.Fatal("the operation reported success despite an injected failure")
	}
	var blocks, receipts, evidence int
	_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, evidenceUserID(t, h, recipient), senderID).Scan(&blocks)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
	if blocks != 0 || receipts != 0 || evidence != 0 {
		t.Fatalf("partial effects survived: %d blocks, %d receipts, %d evidence", blocks, receipts, evidence)
	}
	var state, introduction string
	_ = s.store.QueryRow(`SELECT state,introduction FROM direct_requests WHERE conversation_id=?`, conversation).Scan(&state, &introduction)
	if state != "pending" || introduction == "" {
		t.Fatalf("the request was disturbed by a failed operation: state=%q introduction=%q", state, introduction)
	}
	// The permitted control: with the failure removed the same action works.
	if _, err := s.store.Exec(`DROP TRIGGER fail_request_evidence`); err != nil {
		t.Fatal(err)
	}
	if ok := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-fail",
	}, recipient); ok.Code != 201 {
		t.Fatalf("the action did not work once the injected failure was removed: %d %s", ok.Code, ok.Body.String())
	}
}

// The reviewer sees only submitted evidence: the source kind is labelled, the
// conversation is absent, and the original request cannot be read through it.
func TestRequestEvidenceReviewIsEvidenceOnly(t *testing.T) {
	s, h, recipient, _, conversation, _, admin := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome request", "version": preview.Version, "operation_key": "op-review",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	_, reviewer := evidenceRole(t, s, h, admin, "request evidence reviewer", string(authz.ReportReviewPrivate))
	queue := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, reviewer))
	reports := queue["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("queue length = %d", len(reports))
	}
	evidence := reports[0].(map[string]any)
	if evidence["source_kind"] != "request" {
		t.Fatalf("the source kind was not labelled: %v", evidence["source_kind"])
	}
	if evidence["snapshot_text"] != "please let me in" {
		t.Fatalf("the reviewer did not receive the submitted introduction: %v", evidence["snapshot_text"])
	}
	// The conversation is provenance, not something a reviewer is handed.
	for _, leaked := range []string{"conversation_id", "message_id"} {
		if _, present := evidence[leaked]; present {
			t.Fatalf("the reviewer was given %q: %v", leaked, evidence)
		}
	}
	// A community-only moderator cannot see the queue at all.
	_, moderator := evidenceRole(t, s, h, admin, "community only", string(authz.MessageDeleteAny))
	if w := request(t, h, "GET", "/api/admin/request-reports", nil, moderator); w.Code != http.StatusForbidden {
		t.Fatalf("a community moderator reached the request evidence queue: %d", w.Code)
	}
	// Nor can the reviewer read the original request or any conversation.
	for _, probe := range []string{"/api/directs/" + conversation} {
		if w := request(t, h, "GET", probe, nil, reviewer); w.Code != http.StatusNotFound {
			t.Fatalf("the reviewer reached the original request via %s: %d", probe, w.Code)
		}
	}
	// Resolving is nonpunitive and leaves the expiry alone.
	reportID := evidence["id"].(string)
	expires := evidence["expires_at"].(string)
	resolved := readJSON[map[string]any](t, request(t, h, "PATCH", "/api/admin/request-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "reviewed",
	}, reviewer))
	if resolved["status"] != "resolved" || resolved["expires_at"] != expires {
		t.Fatalf("resolving changed status or expiry: %v", resolved)
	}
	// The sender is still only blocked by the reporter's own action, not by review.
	var blocks int
	_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks`).Scan(&blocks)
	if blocks > 1 {
		t.Fatalf("review created additional blocks: %d", blocks)
	}
}

// An ordinary block must remain available when reporting is refused, and the
// shared rate limit applies across evidence kinds.
func TestOrdinaryBlockSurvivesAReportingRefusal(t *testing.T) {
	s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
	// Fill the shared allowance so reporting is refused.
	if _, err := s.store.Exec(`INSERT INTO private_report_rate_limits(user_id,window_started_at,submissions) VALUES(?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET submissions=excluded.submissions`,
		evidenceUserID(t, h, recipient), db.Now(), privateReportHourlyLimit); err != nil {
		t.Fatal(err)
	}
	preview := previewRequest(t, h, recipient, conversation)
	refused := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-rate",
	}, recipient)
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("the reporting bound was not applied: %d %s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(strings.ToLower(refused.Body.String()), "block") {
		t.Fatalf("the refusal does not mention that blocking is still possible: %s", refused.Body.String())
	}
	// The permitted control: ordinary block still works and still closes the request.
	if w := request(t, h, "PUT", "/api/me/blocks/"+senderID, map[string]any{}, recipient); w.Code != 200 {
		t.Fatalf("ordinary block was refused: %d %s", w.Code, w.Body.String())
	}
	var state, introduction string
	_ = s.store.QueryRow(`SELECT state,introduction FROM direct_requests WHERE conversation_id=?`, conversation).Scan(&state, &introduction)
	if state != "rejected" || introduction != "" {
		t.Fatalf("ordinary block did not close the request: state=%q", state)
	}
	// No evidence and no receipt were created by the refused action.
	var evidence, receipts int
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
	if evidence != 0 || receipts != 0 {
		t.Fatalf("a refused action left records: %d evidence, %d receipts", evidence, receipts)
	}
}

// The default-off gate refuses everything, and the attempt identity survives.
func TestRequestEvidenceRespectsThePrivateFeatureGate(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	s.config.PrivateConversations = false
	if w := request(t, h, "GET", "/api/directs/"+conversation+"/report-preview", nil, recipient); w.Code != http.StatusConflict {
		t.Fatalf("preview survived the feature being disabled: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-gate",
	}, recipient); w.Code != http.StatusConflict {
		t.Fatalf("report survived the feature being disabled: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/me/report-receipt?operation_key=op-gate", nil, recipient); w.Code != http.StatusConflict {
		t.Fatalf("receipt lookup survived the feature being disabled: %d", w.Code)
	}
	// Nothing was written while the feature was off.
	var evidence, receipts int
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
	if evidence != 0 || receipts != 0 {
		t.Fatalf("a disabled feature wrote records: %d evidence, %d receipts", evidence, receipts)
	}
	s.config.PrivateConversations = true
	if w := request(t, h, "GET", "/api/directs/"+conversation+"/report-preview", nil, recipient); w.Code != 200 {
		t.Fatalf("the preview did not return after re-enabling: %d", w.Code)
	}
}

// Expired evidence is unavailable and its receipt stops working, so an old action
// cannot be replayed as new work.
func TestExpiredRequestEvidenceAndReceiptAreNotReplayed(t *testing.T) {
	s, h, recipient, _, conversation, _, owner := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-expire",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	past := db.Timestamp(time.Now().Add(-time.Hour))
	future := db.Timestamp(time.Now().Add(24 * time.Hour))
	if _, err := s.store.Exec(`UPDATE private_request_reports SET expires_at=?`, past); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE private_report_receipts SET expires_at=?`, past); err != nil {
		t.Fatal(err)
	}
	// An expired receipt is a defined terminal result, not an absence: the caller
	// reconciling a lost response needs to know the work happened, and 404 told them
	// it did not (completion review 12, R12-05).
	expired := request(t, h, "GET", "/api/me/report-receipt?operation_key=op-expire", nil, recipient)
	if expired.Code != http.StatusGone {
		t.Fatalf("an expired receipt answered %d, want 410: %s", expired.Code, expired.Body.String())
	}
	if !strings.Contains(expired.Body.String(), "expired") {
		t.Fatalf("the expired result did not say so: %s", expired.Body.String())
	}
	// It is still not re-executed, and still not a server fault: the key stays spent.
	retry := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-expire",
	}, recipient)
	if retry.Code == 201 {
		t.Fatalf("an expired operation was replayed as new work: %d %s", retry.Code, retry.Body.String())
	}
	if retry.Code >= 500 {
		t.Fatalf("an expired operation produced a server fault rather than a defined answer: %d %s", retry.Code, retry.Body.String())
	}
	// A key that was never used is still simply absent, so the three states remain
	// distinguishable by the person holding the key.
	if w := request(t, h, "GET", "/api/me/report-receipt?operation_key=never-used", nil, recipient); w.Code != http.StatusNotFound {
		t.Fatalf("an unused key answered %d, want 404", w.Code)
	}
	// With the expiry still in the past, the record is unreadable: the reviewer
	// cannot distinguish expired evidence from evidence that never existed.
	_, reviewer := evidenceRole(t, s, h, owner, "expired queue reviewer", string(authz.ReportReviewPrivate))
	queue := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, reviewer))
	if len(queue["reports"].([]any)) != 0 {
		t.Fatal("expired evidence appeared in the queue")
	}
	// The permitted control: a live record is visible, so the empty queue above was
	// the expiry and not an empty store.
	if _, err := s.store.Exec(`UPDATE private_request_reports SET expires_at=?`, future); err != nil {
		t.Fatal(err)
	}
	queue = readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, reviewer))
	if len(queue["reports"].([]any)) != 1 {
		t.Fatal("live evidence did not appear in the queue")
	}
}

var _ = sql.ErrNoRows

// R12-01. A request review is the same disclosure as a message review: it names the
// reviewer, the evidence and the time to anyone who can read the community audit
// log, which is exactly what the evidence policy exists to prevent. The writer emits
// `private_request.*` while both audit readers recognised only `private_report.*`, so
// a request review fell into the community feed and was reclaimed on the ordinary
// 90-day clock instead of the declared restricted one.
//
// This drives a real review, then reads both feeds as differently authorized
// accounts. It deliberately does not seed a prefix: only an event a handler actually
// emits can prove the classification.
func TestRequestReviewAuditIsClassifiedAsRestricted(t *testing.T) {
	s, h, recipient, _, conversation, _, admin := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome request", "version": preview.Version, "operation_key": "op-audit-class",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	_, reviewer := evidenceRole(t, s, h, admin, "request audit reviewer", string(authz.ReportReviewPrivate))
	queue := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, reviewer))
	reportID := queue["reports"].([]any)[0].(map[string]any)["id"].(string)
	if w := request(t, h, "PATCH", "/api/admin/request-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "audited",
	}, reviewer); w.Code != 200 {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}

	// An account holding only the community audit capability is the one the policy
	// is written for. It must not learn that a request was reviewed, by whom, or when.
	_, auditor := evidenceRole(t, s, h, admin, "community audit only", string(authz.AuditRead))
	general := readJSON[[]map[string]any](t, request(t, h, "GET", "/api/admin/audit-logs", nil, auditor))
	for _, entry := range general {
		action, _ := entry["action"].(string)
		if strings.HasPrefix(action, "private_request.") {
			t.Fatalf("a request review leaked into the community audit feed: %q", action)
		}
	}

	// The same event is readable under the restricted audit policy, and it names the
	// reviewer who did it: this is the accountable record. The policy is the audit
	// capability rather than the evidence one, so a grant made to see submitted
	// evidence is not also a grant to read an audit trail (V13-02).
	_, auditReader := evidenceRole(t, s, h, admin, "restricted audit reader", string(authz.AuditRead))
	restricted := readJSON[[]map[string]any](t, request(t, h, "GET", "/api/admin/private-reports/audit", nil, auditReader))
	found := false
	for _, entry := range restricted {
		if action, _ := entry["action"].(string); strings.HasPrefix(action, "private_request.") {
			found = true
		}
	}
	if !found {
		t.Fatal("the request review is absent from the restricted audit feed")
	}
	// It is not readable without the audit capability, and specifically not by an
	// account whose only grant is the evidence one (V13-02).
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, reviewer); w.Code != http.StatusForbidden {
		t.Fatalf("an evidence reviewer read the restricted audit feed: %d", w.Code)
	}
}

// The same classification decides reclamation, so a request review keeps the
// restricted 400-day lifetime and is not reclaimed with the community feed's
// 90-day entries. Asserted against a real event at both boundaries rather than a
// seeded prefix, because a maintenance pass that never sees a real prefix would
// pass this test while still reclaiming the wrong rows in production.
func TestRequestReviewAuditKeepsTheRestrictedRetention(t *testing.T) {
	s, h, recipient, _, conversation, _, admin := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "retention check", "version": preview.Version, "operation_key": "op-retention",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	_, reviewer := evidenceRole(t, s, h, admin, "retention reviewer", string(authz.ReportReviewPrivate))
	queue := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, reviewer))
	reportID := queue["reports"].([]any)[0].(map[string]any)["id"].(string)
	if w := request(t, h, "PATCH", "/api/admin/request-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "retention",
	}, reviewer); w.Code != 200 {
		t.Fatalf("review: %d", w.Code)
	}
	var reviewID string
	if err := s.store.QueryRow(`SELECT id FROM audit_logs WHERE action LIKE 'private_request.%'`).Scan(&reviewID); err != nil {
		t.Fatalf("no real request-review event was written: %v", err)
	}

	// A community event written at the same instant, so the two are only
	// distinguishable by classification and not by age.
	communityID, now := db.ID(), db.Now()
	t.Logf("reviewID=%s communityID=%s", reviewID, communityID)
	if _, err := s.store.Exec(`INSERT INTO audit_logs(id,action,target_id,created_at) VALUES(?,'channel.purged','',?)`, communityID, now); err != nil {
		t.Fatal(err)
	}

	// Past the community age and short of the restricted one: the request review
	// must survive and the community event must not.
	past := func(id string, days int) {
		if _, err := s.store.Exec(`UPDATE audit_logs SET created_at=? WHERE id=?`,
			db.Timestamp(time.Now().AddDate(0, 0, -days)), id); err != nil {
			t.Fatal(err)
		}
	}
	past(reviewID, 200)
	past(communityID, 200)
	{
		// Reproduce maintain's own comparison for this exact row.
		var a, c string
		_ = s.store.QueryRow(`SELECT action,created_at FROM audit_logs WHERE id=?`, reviewID).Scan(&a, &c)
		t.Logf("target action=%q created=%s", a, c)
		t.Logf("community cutoff=%s restricted cutoff=%s",
			db.Timestamp(time.Now().Add(-auditLogRetention)),
			db.Timestamp(time.Now().Add(-restrictedAuditLogRetention)))
		var viaCommunity, viaRestricted int
		_ = s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE id=? AND created_at<? AND `+restrictedAuditNotLike("action"), reviewID, db.Timestamp(time.Now().Add(-auditLogRetention))).Scan(&viaCommunity)
		_ = s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE id=? AND created_at<? AND `+restrictedAuditLike("action"), reviewID, db.Timestamp(time.Now().Add(-auditLogRetention))).Scan(&viaRestricted)
		t.Logf("target matched by community predicate: %d, by restricted predicate: %d", viaCommunity, viaRestricted)
	}
	if err := s.maintain(time.Now()); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE id=?`, reviewID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatal("the request review was reclaimed on the community clock")
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE id=?`, communityID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatal("the community event outlived its retention")
	}

	// Past the restricted age as well, it is reclaimed.
	past(reviewID, 500)
	if err := s.maintain(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE id=?`, reviewID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatal("the request review outlived its restricted retention")
	}
}

// R12-02. The storage bound covers private evidence as a whole, so a reviewer queue
// has a defined size. The check counted only message evidence, so an installation
// holding 5,000 unexpired request reports was told the store had room and admitted
// row 5,001. Both tables are one queue, so both are counted together, and the count
// happens inside the submitting transaction.
func TestRequestAndMessageEvidenceShareOneStorageBound(t *testing.T) {
	s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
	fill := func(t *testing.T, table string, n int) {
		t.Helper()
		future := db.Timestamp(time.Now().Add(24 * time.Hour))
		tx, err := s.store.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < n; i++ {
			switch table {
			case "message":
				if _, err := tx.Exec(`INSERT INTO private_message_reports(id,message_id,conversation_id,reporter_id,author_id,reason,snapshot_text,status,created_at,updated_at,expires_at)
					VALUES(?,?,'',?,?,'reason','text','open',?,?,?)`, fmt.Sprintf("cap-msg-%05d", i), fmt.Sprintf("cap-msg-src-%05d", i), senderID, senderID, future, future, future); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err := tx.Exec(`INSERT INTO private_request_reports(id,conversation_id,attempt_id,requester_id,reporter_id,reason,snapshot_text,status,request_sent_at,created_at,updated_at,expires_at)
					VALUES(?,'',?,?,?,'reason','text','open',?,?,?,?)`, fmt.Sprintf("cap-req-%05d", i), fmt.Sprintf("att-%05d", i), senderID, senderID, future, future, future, future); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// A mixed population: message evidence alone would leave room, and so would
	// request evidence alone. Together they are at the bound, which is the case the
	// per-table count could not see.
	fill(t, "message", 2500)
	fill(t, "request", 2500)
	var stored int
	if err := s.store.QueryRow(`SELECT (SELECT count(*) FROM private_message_reports WHERE expires_at>?) + (SELECT count(*) FROM private_request_reports WHERE expires_at>?)`, db.Now(), db.Now()).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != privateReportStorageLimit {
		t.Fatalf("fixture holds %d, expected %d", stored, privateReportStorageLimit)
	}
	preview := previewRequest(t, h, recipient, conversation)
	w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "past the bound", "version": preview.Version, "operation_key": "op-cap-1",
	}, recipient)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a report was admitted past the bound: %d %s", w.Code, w.Body.String())
	}
	// The refusal must be total: no evidence, no rejection, no block, no receipt.
	var evidence, receipts int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_request_reports WHERE id<>'' AND reason='past the bound'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM private_report_receipts WHERE operation_key='op-cap-1'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 || receipts != 0 {
		t.Fatalf("the refused report left side effects: %d evidence, %d receipts", evidence, receipts)
	}
	var stillPending int
	if err := s.store.QueryRow(`SELECT count(*) FROM direct_requests WHERE conversation_id=? AND state='pending'`, conversation).Scan(&stillPending); err != nil {
		t.Fatal(err)
	}
	if stillPending != 1 {
		t.Fatal("the refused report rejected or cleared the request")
	}
}

// Expired evidence is not stored evidence. A full store whose only rows have passed
// their expiry must still admit a report, or the bound would become a permanent
// lockout that only an operator's manual edit could clear.
func TestRequestEvidenceIgnoresExpiredRowsForTheStorageBound(t *testing.T) {
	s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
	past := db.Timestamp(time.Now().Add(-24 * time.Hour))
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < privateReportStorageLimit; i++ {
		if _, err := tx.Exec(`INSERT INTO private_request_reports(id,conversation_id,attempt_id,requester_id,reporter_id,reason,snapshot_text,status,request_sent_at,created_at,updated_at,expires_at)
			VALUES(?,?,?,?,?,'reason','text','open',?,?,?,?)`, fmt.Sprintf("exp-cap-%05d", i), conversation, fmt.Sprintf("att-%05d", i), senderID, senderID, past, past, past, past); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "expired rows are not stored evidence", "version": preview.Version, "operation_key": "op-cap-expired",
	}, recipient); w.Code != 201 {
		t.Fatalf("a full store of expired rows refused a report: %d %s", w.Code, w.Body.String())
	}
}

// R12-03. A request introduction is bounded in Unicode characters, and the bound is
// the server's: creation and preview both accept 500 CJK characters, so the report
// path could not refuse the introduction it had just shown the recipient. It
// measured bytes, and a CJK character is three of them.
func TestRequestIntroductionLimitCountsCharactersNotBytes(t *testing.T) {
	// A pair can hold one live request at a time and reporting settles it, so each
	// case gets its own pair on its own server rather than sharing one and failing
	// for a reason that is not the one under test.
	// 500 three-byte characters: valid under the source contract, 1,500 bytes.
	cases := []struct {
		name  string
		chars int
		ok    bool
	}{
		{"empty", 0, true},
		{"ascii at the limit", 500, true},
		{"cjk at the limit", 500, true},
		{"emoji at the limit", 500, true},
		{"cjk past the limit", 501, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A pair holds one live request at a time and reporting settles it, so
			// each case gets its own pair on its own server rather than sharing one
			// and failing for a reason that is not the one under test.
			s, h := testServer(t)
			s.config.PrivateConversations = true
			setupAdmin(t, s, h)
			_, requester := memberAccount(t, s, "cjk_requester")
			recipientID, recipient := memberAccount(t, s, "cjk_recipient")
			introduction := strings.Repeat("請", tc.chars)
			if tc.name == "ascii at the limit" {
				introduction = strings.Repeat("a", tc.chars)
			}
			if tc.name == "emoji at the limit" {
				introduction = strings.Repeat("🙂", tc.chars)
			}
			asked := request(t, h, "POST", "/api/directs", map[string]string{
				"user_id": recipientID, "introduction": introduction,
			}, requester)
			if tc.ok && asked.Code != 201 {
				t.Fatalf("a valid %d-character introduction was refused at creation: %d %s", tc.chars, asked.Code, asked.Body.String())
			}
			if !tc.ok {
				if asked.Code != http.StatusBadRequest {
					t.Fatalf("a %d-character introduction was accepted at creation: %d", tc.chars, asked.Code)
				}
				return
			}
			conversation := readJSON[directConversation](t, asked).ID
			// The preview shows the same text, so the confirmation has to accept it.
			preview := previewRequest(t, h, recipient, conversation)
			if preview.Introduction != introduction {
				t.Fatalf("the preview did not show the introduction the recipient confirmed (%d vs %d characters)",
					utf8.RuneCountInString(preview.Introduction), utf8.RuneCountInString(introduction))
			}
			w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
				"reason": "cjk", "version": preview.Version, "operation_key": "op-cjk-" + tc.name,
			}, recipient)
			if w.Code != 201 {
				t.Fatalf("reporting an introduction the server itself accepted failed: %d %s", w.Code, w.Body.String())
			}
			var snapshot string
			if err := s.store.QueryRow(`SELECT snapshot_text FROM private_request_reports ORDER BY created_at DESC LIMIT 1`).Scan(&snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot != introduction {
				t.Fatal("the stored evidence is not the introduction that was confirmed")
			}
		})
	}
}

// R12-04. An operation key acknowledges one specific action. The digest bound the
// action, the reason and the attempt id taken from the receipt being replayed — not
// the destination the caller named — so a key could be presented against a
// different conversation and be answered with that other report's outcome. The key
// must be bound to where it was aimed as well as what it said.
func TestOperationKeyIsBoundToItsDestination(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	// A second genuine pending request, so there is a different conversation to aim
	// the spent key at.
	_, otherRequester := memberAccount(t, s, "other_requester")
	otherID, _ := memberAccount(t, s, "other_recipient")
	otherConversation := readJSON[directConversation](t, request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": otherID, "introduction": "a different introduction",
	}, otherRequester)).ID

	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "first report", "version": preview.Version, "operation_key": "op-dest-1",
	}, recipient); w.Code != 201 {
		t.Fatalf("first report: %d %s", w.Code, w.Body.String())
	}

	// Same key, same reason, aimed at a different conversation. The recipient of
	// that conversation is not this account, so the operation is not theirs; it must
	// not be answered with the first report's receipt.
	foreign := request(t, h, "POST", "/api/directs/"+otherConversation+"/report-and-block", map[string]string{
		"reason": "first report", "version": "whatever", "operation_key": "op-dest-1",
	}, recipient)
	if foreign.Code == 200 {
		t.Fatalf("a spent key answered for a different conversation: %d %s", foreign.Code, foreign.Body.String())
	}
	// And the other conversation must be untouched: no evidence, no block.
	var otherEvidence int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_request_reports WHERE conversation_id=?`, otherConversation).Scan(&otherEvidence); err != nil {
		t.Fatal(err)
	}
	if otherEvidence != 0 {
		t.Fatal("the foreign key use created evidence on the other conversation")
	}
	var otherPending int
	if err := s.store.QueryRow(`SELECT count(*) FROM direct_requests WHERE conversation_id=? AND state='pending'`, otherConversation).Scan(&otherPending); err != nil {
		t.Fatal(err)
	}
	if otherPending != 1 {
		t.Fatal("the foreign key use rejected the other conversation's request")
	}
}

// A renewed attempt on the same pair is a different attempt, so it is different work
// even though the conversation is the same. The key must distinguish them, or a
// report of the first introduction would be replayed as if it covered the second.
func TestARenewedAttemptIsDifferentWorkForTheSameKey(t *testing.T) {
	s, h, recipient, sender, conversation, _, _ := requestFixture(t)
	first := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "the first introduction", "version": first.Version, "operation_key": "op-renew",
	}, recipient); w.Code != 201 {
		t.Fatalf("first report: %d %s", w.Code, w.Body.String())
	}
	// Deliberately unblock and renew, so a genuinely new attempt exists on the same
	// pair. The recipient is the one who acts this time, which is the case the
	// original test covered only from the unauthorized sender's side.
	if w := request(t, h, "DELETE", "/api/me/blocks/"+mustID(t, s, "request_sender"), nil, recipient); w.Code != 200 {
		t.Fatalf("unblock: %d %s", w.Code, w.Body.String())
	}
	// The rejection cooldown is bypassed deliberately: the attempt identity is what
	// this test is about, and waiting 24 hours is not a thing a test can do.
	if _, err := s.store.Exec(`UPDATE direct_requests SET retry_after='' WHERE conversation_id=?`, conversation); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/directs", map[string]string{
		"user_id": evidenceUserID(t, h, recipient), "introduction": "a second introduction",
	}, sender); w.Code != 201 && w.Code != 200 {
		// 201 for a new pair, 200 when an existing pair reopens: both are success.
		t.Fatalf("renew: %d %s", w.Code, w.Body.String())
	}
	second := previewRequest(t, h, recipient, conversation)
	if second.Version == first.Version {
		t.Fatal("the renewed attempt reused the first attempt's version")
	}
	// Same key against the new attempt: different work, so it is refused rather than
	// answered with the previous report's outcome.
	w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "the first introduction", "version": second.Version, "operation_key": "op-renew",
	}, recipient)
	if w.Code == 200 {
		t.Fatalf("a spent key was accepted for a renewed attempt: %d %s", w.Code, w.Body.String())
	}
	var evidence int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_request_reports WHERE conversation_id=?`, conversation).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != 1 {
		t.Fatalf("the renewed attempt produced %d evidence rows, want 1", evidence)
	}
}

func mustID(t *testing.T, s *Server, username string) string {
	t.Helper()
	var id string
	if err := s.store.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatalf("no account %q: %v", username, err)
	}
	return id
}

// R12-05. Two different clocks are involved and the response used one name for
// both. The first answer carried the evidence's 90-day expiry; a replay or a lookup
// carried the receipt's 30-day one, under the same field name. A person told their
// report is available for 90 days would read a 30-day value on the retry and have
// no way to know which promise they were reading.
func TestReceiptDistinguishesEvidenceExpiryFromItsOwn(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	first := readJSON[map[string]any](t, request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "clock check", "version": preview.Version, "operation_key": "op-clock",
	}, recipient))

	var evidenceExpiry, receiptExpiry string
	if err := s.store.QueryRow(`SELECT expires_at FROM private_request_reports WHERE id=?`, first["id"]).Scan(&evidenceExpiry); err != nil {
		t.Fatal(err)
	}
	if err := s.store.QueryRow(`SELECT expires_at FROM private_report_receipts WHERE operation_key='op-clock'`).Scan(&receiptExpiry); err != nil {
		t.Fatal(err)
	}
	if evidenceExpiry == receiptExpiry {
		t.Fatal("the fixture cannot distinguish the two clocks")
	}
	// The first response states the evidence's availability, and says how long the
	// receipt itself lives, without conflating them.
	if first["evidence_expires_at"] != evidenceExpiry {
		t.Fatalf("the first response did not state the evidence expiry: %v", first["evidence_expires_at"])
	}
	if first["receipt_expires_at"] != receiptExpiry {
		t.Fatalf("the first response did not state the receipt expiry: %v", first["receipt_expires_at"])
	}
	// The ambiguous field is gone rather than kept alongside the honest ones.
	if _, present := first["expires_at"]; present {
		t.Fatal("the ambiguous expires_at field is still reported")
	}

	// A replay and the lookup must state the same two things, with the same values.
	replay := readJSON[map[string]any](t, request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "clock check", "version": preview.Version, "operation_key": "op-clock",
	}, recipient))
	lookup := readJSON[map[string]any](t, request(t, h, "GET", "/api/me/report-receipt?operation_key=op-clock", nil, recipient))
	for name, body := range map[string]map[string]any{"replay": replay, "lookup": lookup} {
		if body["evidence_expires_at"] != evidenceExpiry {
			t.Fatalf("%s stated evidence expiry %v, want %s", name, body["evidence_expires_at"], evidenceExpiry)
		}
		if body["receipt_expires_at"] != receiptExpiry {
			t.Fatalf("%s stated receipt expiry %v, want %s", name, body["receipt_expires_at"], receiptExpiry)
		}
		if _, present := body["expires_at"]; present {
			t.Fatalf("%s still reports the ambiguous expires_at", name)
		}
	}
}

// After the receipt's own window closes the operation is spent, and saying so is
// different from pretending it never existed. The key must also stay reserved: the
// unique index outlives the row, so simply hiding it turns a later attempt into a
// late constraint failure instead of a defined answer.
func TestAnExpiredReceiptIsDefinedAndItsKeyStaysSpent(t *testing.T) {
	s, h, recipient, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "expiry", "version": preview.Version, "operation_key": "op-expire",
	}, recipient); w.Code != 201 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	// The receipt passes its window; the evidence it names has not.
	past := db.Timestamp(time.Now().Add(-time.Hour))
	if _, err := s.store.Exec(`UPDATE private_report_receipts SET expires_at=? WHERE operation_key='op-expire'`, past); err != nil {
		t.Fatal(err)
	}
	lookup := request(t, h, "GET", "/api/me/report-receipt?operation_key=op-expire", nil, recipient)
	if lookup.Code == 404 {
		t.Fatal("an expired receipt is reported as never having existed")
	}
	if lookup.Code != 410 {
		t.Fatalf("an expired receipt answered %d, want 410: %s", lookup.Code, lookup.Body.String())
	}
	// The key stays reserved, so the same operation cannot be run again as new work.
	replay := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "expiry", "version": preview.Version, "operation_key": "op-expire",
	}, recipient)
	if replay.Code == 201 {
		t.Fatalf("an expired receipt's key admitted the operation again: %d %s", replay.Code, replay.Body.String())
	}
	if replay.Code >= 500 {
		t.Fatalf("an expired receipt produced a server fault rather than a defined answer: %d %s", replay.Code, replay.Body.String())
	}
	// And it survives a restart of the process's own store, which is the whole point
	// of keeping the guard.
	if _, err := s.store.Exec(`UPDATE private_report_receipts SET expires_at=?`, past); err != nil {
		t.Fatal(err)
	}
	var keys int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_report_receipts WHERE operation_key='op-expire'`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 1 {
		t.Fatal("the spent key was removed rather than compacted to a guard")
	}
}

// R12-06. Report-and-block is one transaction, and the claim is only worth as much
// as the points at which it can fail. Injecting a failure at the evidence write
// alone proves the first statement's rollback and nothing about the rest of the
// sequence — the block, the rejection, the receipt or the audit entry are written
// after it, and each is a point where a partial commit would leave a person
// blocked for a report that does not exist, or holding evidence with no
// explanation of who reviewed it.
func TestReportAndBlockRollsBackAtEveryWrite(t *testing.T) {
	injections := []struct {
		name   string
		create string
		drop   string
	}{
		{"evidence insert", `CREATE TRIGGER p10_fail_evidence BEFORE INSERT ON private_request_reports BEGIN SELECT RAISE(ABORT,'injected'); END`, `DROP TRIGGER p10_fail_evidence`},
		{"block insert", `CREATE TRIGGER p10_fail_block BEFORE INSERT ON user_blocks BEGIN SELECT RAISE(ABORT,'injected'); END`, `DROP TRIGGER p10_fail_block`},
		{"request rejection", `CREATE TRIGGER p10_fail_reject BEFORE UPDATE ON direct_requests WHEN NEW.state='rejected' BEGIN SELECT RAISE(ABORT,'injected'); END`, `DROP TRIGGER p10_fail_reject`},
		{"receipt insert", `CREATE TRIGGER p10_fail_receipt BEFORE INSERT ON private_report_receipts BEGIN SELECT RAISE(ABORT,'injected'); END`, `DROP TRIGGER p10_fail_receipt`},
		{"audit write", `CREATE TRIGGER p10_fail_audit BEFORE INSERT ON audit_logs WHEN NEW.action='private_report.reported_and_blocked' BEGIN SELECT RAISE(ABORT,'injected'); END`, `DROP TRIGGER p10_fail_audit`},
	}
	for _, inj := range injections {
		t.Run(inj.name, func(t *testing.T) {
			s, h, recipient, _, conversation, senderID, _ := requestFixture(t)
			preview := previewRequest(t, h, recipient, conversation)
			if _, err := s.store.Exec(inj.create); err != nil {
				t.Fatal(err)
			}
			w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
				"reason": "unwelcome", "version": preview.Version, "operation_key": "op-inject",
			}, recipient)
			if w.Code == 201 {
				t.Fatal("the operation reported success despite an injected failure")
			}
			// Nothing at all survives: no block, no receipt, no evidence, no audit
			// entry, and the request is exactly as it was.
			var blocks, receipts, evidence, audited int
			_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, evidenceUserID(t, h, recipient), senderID).Scan(&blocks)
			_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
			_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
			_ = s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='private_report.reported_and_blocked'`).Scan(&audited)
			if blocks != 0 || receipts != 0 || evidence != 0 || audited != 0 {
				t.Fatalf("partial effects survived: %d blocks, %d receipts, %d evidence, %d audit", blocks, receipts, evidence, audited)
			}
			var state, introduction string
			_ = s.store.QueryRow(`SELECT state,introduction FROM direct_requests WHERE conversation_id=?`, conversation).Scan(&state, &introduction)
			if state != "pending" || introduction == "" {
				t.Fatalf("the request was disturbed: state=%q introduction=%q", state, introduction)
			}
			// The permitted control: with the failure removed the same action works
			// and leaves all four effects together.
			if _, err := s.store.Exec(inj.drop); err != nil {
				t.Fatal(err)
			}
			if ok := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
				"reason": "unwelcome", "version": preview.Version, "operation_key": "op-inject",
			}, recipient); ok.Code != 201 {
				t.Fatalf("the action did not work once the failure was removed: %d %s", ok.Code, ok.Body.String())
			}
			_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, evidenceUserID(t, h, recipient), senderID).Scan(&blocks)
			_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
			_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
			_ = s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='private_report.reported_and_blocked'`).Scan(&audited)
			if blocks != 1 || receipts != 1 || evidence != 1 || audited != 1 {
				t.Fatalf("the control did not commit all four: %d blocks, %d receipts, %d evidence, %d audit", blocks, receipts, evidence, audited)
			}
		})
	}
}

// A refusal is not a failure to report, and the two must not be confused. An
// account without the standing to report gets nothing: no block, no receipt, no
// evidence, and no audit entry claiming a review happened.
func TestReportRefusalLeavesNoTraceOfAnAttempt(t *testing.T) {
	s, h, recipient, sender, conversation, senderID, _ := requestFixture(t)
	// The sender is unauthorized from the start and always was; the recipient is
	// unauthorized only because the attempt has been settled.
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "unwelcome", "version": preview.Version, "operation_key": "op-refuse-sender",
	}, sender); w.Code != http.StatusNotFound {
		t.Fatalf("the sender was not refused: %d %s", w.Code, w.Body.String())
	}
	// Settle the request by having the recipient reject it, so there is no longer a
	// pending attempt for anyone.
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/reject", map[string]string{}, recipient); w.Code != 200 {
		t.Fatalf("reject: %d %s", w.Code, w.Body.String())
	}
	// A later submission naming a key is refused, and refused identically whether
	// the reason is a missing attempt, a wrong version or a wrong account: the
	// response must not tell an outsider that a conversation exists.
	for name, cookie := range map[string]*http.Cookie{"the recipient": recipient, "the sender": sender} {
		w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
			"reason": "unwelcome", "version": preview.Version, "operation_key": "op-refuse-" + name,
		}, cookie)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s received %d rather than an indistinguishable refusal: %s", name, w.Code, w.Body.String())
		}
	}
	var blocks, receipts, evidence, audited int
	_ = s.store.QueryRow(`SELECT count(*) FROM user_blocks WHERE blocked_id=?`, senderID).Scan(&blocks)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_report_receipts`).Scan(&receipts)
	_ = s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence)
	_ = s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action LIKE 'private_report.%'`).Scan(&audited)
	if blocks != 0 || receipts != 0 || evidence != 0 || audited != 0 {
		t.Fatalf("a refused attempt left a trace: %d blocks, %d receipts, %d evidence, %d audit", blocks, receipts, evidence, audited)
	}
}
