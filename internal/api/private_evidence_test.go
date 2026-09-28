package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Private message evidence (ADR 0013). These are the behaviors the contract turns
// on: a reporter discloses exactly what they saw, a reviewer sees only the
// snapshot, and neither the reporter nor an unrelated moderator can widen that.

func privateEvidenceFixture(t *testing.T) (*Server, http.Handler, *http.Cookie, *http.Cookie, *http.Cookie, string) {
	t.Helper()
	s, h, admin, _, a, _, b, conversation := privatePipeline(t)
	// The owner is designated, not merely the administrator role: report.review_private
	// is delegated downward, so an actor may grant only what it holds, and before
	// the ceremony the admin role does not hold every capability. This is the real
	// delegation path an operator would use, not a test-only shortcut, so the
	// ceremony's own prerequisites are satisfied here too.
	enrollMFA(t, h, admin, "correct horse battery")
	if w := request(t, h, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatalf("designate the owner: %d %s", w.Code, w.Body.String())
	}
	w := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", messageInput{
		Content: "the words being reported", IdempotencyKey: "evidence-1",
	}, a)
	if w.Code != 201 {
		t.Fatalf("seed message: %d %s", w.Code, w.Body.String())
	}
	return s, h, admin, a, b, readJSON[map[string]any](t, w)["id"].(string)
}

// privateEvidenceFixtureWithConversation is privateEvidenceFixture plus the
// conversation id, for cases that post a second message into the same pair.
func privateEvidenceFixtureWithConversation(t *testing.T) (*Server, http.Handler, *http.Cookie, *http.Cookie, *http.Cookie, string) {
	t.Helper()
	s, h, admin, _, a, _, b, conversation := privatePipeline(t)
	// The owner is designated so a test may delegate the review capability: an
	// account may only grant what it holds, and nothing holds it before the
	// ceremony. This is the path an operator would take, not a test shortcut.
	enrollMFA(t, h, admin, "correct horse battery")
	if w := request(t, h, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatalf("designate the owner: %d %s", w.Code, w.Body.String())
	}
	return s, h, admin, a, b, conversation
}
func privatePreview(t *testing.T, h http.Handler, cookie *http.Cookie, messageID string) *privateEvidencePreview {
	t.Helper()
	w := request(t, h, "GET", "/api/messages/"+messageID+"/private-report-preview", nil, cookie)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	preview := readJSON[privateEvidencePreview](t, w)
	return &preview
}

func submitPrivate(t *testing.T, h http.Handler, cookie *http.Cookie, messageID, version, reason string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, h, "POST", "/api/messages/"+messageID+"/private-report", map[string]string{
		"reason": reason, "version": version,
	}, cookie)
}

// The saved evidence is exactly what the reporter confirmed, and the receipt names
// nothing beyond their own report.
func TestPrivateEvidenceSavesExactlyTheConfirmedServerSnapshot(t *testing.T) {
	_, h, _, a, _, messageID := privateEvidenceFixture(t)
	preview := privatePreview(t, h, a, messageID)
	if preview.SnapshotText != "the words being reported" {
		t.Fatalf("preview text = %q", preview.SnapshotText)
	}
	w := submitPrivate(t, h, a, messageID, preview.Version, "unwelcome")
	if w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	receipt := readJSON[map[string]any](t, w)
	// The submitter learns their report's identity and expiry, and nothing else: no
	// author, no conversation, no reviewer note and no reason echoed back.
	for _, leaked := range []string{"snapshot_text", "author_id", "conversation_id", "review_note", "reviewed_by", "reason"} {
		if _, present := receipt[leaked]; present {
			t.Fatalf("the submit receipt disclosed %q: %v", leaked, receipt)
		}
	}
}

// The whole point of the preview: an edit between preview and confirmation is
// refused, and the replacement is not saved behind the reporter's back.
func TestPrivateEvidenceRefusesAMessageEditedAfterPreview(t *testing.T) {
	_, h, _, a, _, messageID := privateEvidenceFixture(t)
	preview := privatePreview(t, h, a, messageID)

	// The reported message is edited by its own author between the reporter's
	// preview and their confirmation, which is the sequence the preview contract
	// exists to catch. A second participant cannot edit it, so the author is the
	// only account that can produce this race.
	edited := request(t, h, "PATCH", "/api/messages/"+messageID, map[string]string{"content": "different words entirely"}, a)
	if edited.Code != 200 && edited.Code != 201 {
		t.Fatalf("edit: %d %s", edited.Code, edited.Body.String())
	}

	stale := submitPrivate(t, h, a, messageID, preview.Version, "unwelcome")
	if stale.Code != http.StatusConflict {
		t.Fatalf("a stale preview was accepted: %d %s", stale.Code, stale.Body.String())
	}
	body := readJSON[changedPreview](t, stale)
	if !body.Changed || body.Preview == nil || body.Preview.SnapshotText != "different words entirely" {
		t.Fatalf("the refusal did not return a fresh preview: %+v", body)
	}
	// The permitted control: confirming the new preview works, so the refusal is
	// a consent step rather than evidence being permanently unreportable.
	fresh := privatePreview(t, h, a, messageID)
	if w := submitPrivate(t, h, a, messageID, fresh.Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("re-confirmed submit: %d %s", w.Code, w.Body.String())
	}
}

// A blocked participant keeps readable history and must still be able to report it.
// This is why submission uses read access and not write access: the write path
// would deny a blocked victim the one action that matters most.
func TestABlockedParticipantCanStillReportRetainedHistory(t *testing.T) {
	_, h, _, a, b, messageID := privateEvidenceFixture(t)
	conversation := readableConversation(t, h, a)
	if w := request(t, h, "PUT", "/api/me/blocks/"+peerOf(t, h, b), map[string]any{}, b); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	write := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", messageInput{
		Content: "should not send", IdempotencyKey: "evidence-blocked-write",
	}, a)
	if write.Code != http.StatusForbidden {
		t.Fatalf("the blocked write was not refused: %d", write.Code)
	}
	preview := privatePreview(t, h, a, messageID)
	if preview.SnapshotText != "the words being reported" {
		t.Fatalf("preview after block = %q", preview.SnapshotText)
	}
	if w := submitPrivate(t, h, a, messageID, preview.Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("a blocked participant could not report retained history: %d %s", w.Code, w.Body.String())
	}
}

// A pending requester, an outsider and a non-participating owner are refused
// identically, so guessing a message id discloses nothing about its existence.
func TestPrivateEvidenceDeniesEveryoneWithoutAcceptedReadAccess(t *testing.T) {
	s, h, owner, a, _, messageID := privateEvidenceFixture(t)
	_, outsider := memberAccount(t, s, "outsider_evidence")
	carolID, carol := memberAccount(t, s, "carol_evidence")

	// carol holds an unanswered request from alice. There is no conversation to
	// report from yet, so she is the pending-requester case the contract names.
	pending := request(t, h, "POST", "/api/directs", map[string]string{"user_id": carolID, "introduction": "hello"}, a)
	if pending.Code != 201 {
		t.Fatalf("pending request: %d %s", pending.Code, pending.Body.String())
	}
	carolRequestID := readJSON[directConversation](t, pending).ID
	if w := request(t, h, "GET", "/api/conversations/"+carolRequestID+"/messages", nil, carol); w.Code != http.StatusNotFound {
		t.Fatalf("the pending requester could read a conversation: %d", w.Code)
	}

	// A community message is not eligible either: it belongs in the community
	// queue, and admitting it here would give a private reviewer a second route
	// into content their grant does not scope.
	channel := textChannelID(t, s)
	community := request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{
		Content: "ordinary public wording", IdempotencyKey: "evidence-community",
	}, owner)
	if community.Code != 201 {
		t.Fatalf("seed a community message: %d %s", community.Code, community.Body.String())
	}
	communityMessage := readJSON[map[string]any](t, community)["id"].(string)
	if w := request(t, h, "GET", "/api/messages/"+communityMessage+"/private-report-preview", nil, a); w.Code != http.StatusNotFound {
		t.Fatalf("a community message was admitted to private evidence: %d", w.Code)
	}

	for name, cookie := range map[string]*http.Cookie{"outsider": outsider, "pending requester": carol, "the owner": owner} {
		if w := request(t, h, "GET", "/api/messages/"+messageID+"/private-report-preview", nil, cookie); w.Code != http.StatusNotFound {
			t.Fatalf("%s could preview a private message: %d %s", name, w.Code, w.Body.String())
		}
		if w := submitPrivate(t, h, cookie, messageID, "anything", "guessing"); w.Code != http.StatusNotFound {
			t.Fatalf("%s could submit a report: %d %s", name, w.Code, w.Body.String())
		}
	}
	// A message that does not exist answers exactly the same way.
	if w := request(t, h, "GET", "/api/messages/does-not-exist/private-report-preview", nil, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("a missing message answered differently: %d", w.Code)
	}
}

// A community-only moderator must not learn that a private queue exists, while an
// explicitly delegated private reviewer sees the snapshot and nothing more.
func TestPrivateEvidenceNeedsItsOwnCapabilityAndGrantsNoConversationAccess(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	conversation := readableConversation(t, h, a)

	_, moderator := memberAccount(t, s, "community_moderator")
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, moderator); w.Code != http.StatusForbidden {
		t.Fatalf("a community moderator reached the private queue: %d", w.Code)
	}

	_, reviewer := evidenceRole(t, s, h, admin, "private reviewer", string(authz.ReportReviewPrivate))
	if w := submitPrivate(t, h, a, messageID, privatePreview(t, h, a, messageID).Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	queue := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer)
	if queue.Code != 200 {
		t.Fatalf("the delegated reviewer could not read the queue: %d %s", queue.Code, queue.Body.String())
	}
	reports := readJSON[map[string]any](t, queue)["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("queue length = %d", len(reports))
	}
	if reports[0].(map[string]any)["snapshot_text"] != "the words being reported" {
		t.Fatalf("the reviewer did not receive the submitted snapshot: %v", reports[0])
	}
	// Holding the evidence grants nothing about the conversation it came from.
	for _, probe := range []struct{ method, path string }{
		{"GET", "/api/conversations/" + conversation + "/messages"},
		{"GET", "/api/messages/" + messageID},
		{"GET", "/api/conversations/" + conversation},
	} {
		if w := request(t, h, probe.method, probe.path, nil, reviewer); w.Code != http.StatusNotFound {
			t.Fatalf("the reviewer reached the original conversation via %s: %d", probe.path, w.Code)
		}
	}
	// Global search is community-only by contract, so it answers 200 with an
	// empty result rather than 404; the point is that it cannot surface the text.
	hits := readJSON[[]map[string]any](t, request(t, h, "GET", "/api/search?q=reported", nil, reviewer))
	for _, hit := range hits {
		if content, _ := hit["content"].(string); strings.Contains(content, "reported") {
			t.Fatalf("the reviewer found the private text through search: %v", hit)
		}
	}
	// A private reviewer is not a community moderator: the community queue, which
	// is scoped to channels they can read, stays closed.
	if w := request(t, h, "GET", "/api/admin/reports", nil, reviewer); w.Code != http.StatusForbidden {
		t.Fatalf("the private reviewer reached the community queue: %d", w.Code)
	}
}

// The grant is checked in the transaction that reads and mutates the queue, so
// revoking it fences later reads and later reviews.
func TestRevokingPrivateReviewFencesTheQueueAndLaterReviews(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	role, reviewer := evidenceRole(t, s, h, admin, "private reviewer two", string(authz.ReportReviewPrivate))
	if w := submitPrivate(t, h, a, messageID, privatePreview(t, h, a, messageID).Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != 200 {
		t.Fatalf("the reviewer could not read the queue before revocation: %d", w.Code)
	}
	reportID := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))["reports"].([]any)[0].(map[string]any)["id"].(string)

	revokeEvidenceRole(t, h, admin, role)
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != http.StatusForbidden {
		t.Fatalf("the revoked reviewer still read the queue: %d", w.Code)
	}
	review := request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "triaged",
	}, reviewer)
	if review.Code != http.StatusForbidden {
		t.Fatalf("the revoked reviewer still reviewed evidence: %d %s", review.Code, review.Body.String())
	}
	// The permitted control: restoring the grant restores both, so the refusals
	// above are a consequence of the revocation and not of a broken role.
	restoreEvidenceRole(t, h, admin, role, string(authz.ReportReviewPrivate))
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != 200 {
		t.Fatalf("the regranted reviewer could not read the queue: %d", w.Code)
	}
	if w := request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "triaged",
	}, reviewer); w.Code != 200 {
		t.Fatalf("the regranted reviewer could not review: %d %s", w.Code, w.Body.String())
	}
}

// Resolving is triage: it deletes nothing, bans nobody and does not move expiry.
func TestResolvingPrivateEvidenceIsNonpunitiveAndKeepsExpiry(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	role, reviewer := evidenceRole(t, s, h, admin, "private reviewer three", string(authz.ReportReviewPrivate))
	_ = role
	if w := submitPrivate(t, h, a, messageID, privatePreview(t, h, a, messageID).Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	evidence := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))["reports"].([]any)[0].(map[string]any)
	reportID, createdExpiry := evidence["id"].(string), evidence["expires_at"].(string)

	resolved := readJSON[privateMessageReport](t, request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "reviewed, no action needed",
	}, reviewer))
	if resolved.Status != "resolved" || resolved.ExpiresAt != createdExpiry {
		t.Fatalf("resolving changed status or expiry: %+v", resolved)
	}
	// The conversation is untouched: a review records triage, it does not act.
	if w := request(t, h, "GET", "/api/conversations/"+readableConversation(t, h, a)+"/messages", nil, a); w.Code != 200 {
		t.Fatalf("resolving broke the conversation: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/state", nil, a); w.Code != 200 {
		t.Fatalf("the reporter lost access after review: %d", w.Code)
	}
	// Reopen returns it to the queue without extending expiry either.
	reopened := readJSON[privateMessageReport](t, request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]string{
		"status": "open", "note": "reopened",
	}, reviewer))
	if reopened.Status != "open" || reopened.ExpiresAt != createdExpiry {
		t.Fatalf("reopen moved expiry or status: %+v", reopened)
	}
}

// A retry deduplicates onto the original evidence instead of replacing the
// explanation the reporter already submitted.
func TestRetriedPrivateEvidenceKeepsTheOriginalExplanation(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	_, reviewer := evidenceRole(t, s, h, admin, "private reviewer four", string(authz.ReportReviewPrivate))
	preview := privatePreview(t, h, a, messageID)
	first := submitPrivate(t, h, a, messageID, preview.Version, "the original explanation")
	if first.Code != 201 {
		t.Fatalf("first submit: %d %s", first.Code, first.Body.String())
	}
	firstReceipt := readJSON[map[string]any](t, first)

	second := submitPrivate(t, h, a, messageID, preview.Version, "a different explanation")
	if second.Code != 200 {
		t.Fatalf("retry: %d %s", second.Code, second.Body.String())
	}
	secondReceipt := readJSON[map[string]any](t, second)
	if secondReceipt["id"] != firstReceipt["id"] || secondReceipt["duplicate"] != true {
		t.Fatalf("the retry did not deduplicate onto the original: %v vs %v", secondReceipt, firstReceipt)
	}
	// The stored explanation is still the original: a retry never overwrites it.
	evidence := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))["reports"].([]any)[0].(map[string]any)
	if evidence["reason"] != "the original explanation" {
		t.Fatalf("the retry replaced the stored explanation: %v", evidence["reason"])
	}
}

// Disabling the private feature refuses submission and review, and keeps the
// evidence until its normal expiry. Disabling a feature is not erasure.
func TestDisablingPrivateConversationsRefusesEvidenceWithoutErasingIt(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	if w := submitPrivate(t, h, a, messageID, privatePreview(t, h, a, messageID).Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	// The reviewer is granted first, so the refusals below are about the feature
	// gate and not about a missing capability: the route guard answers 403 before
	// a handler runs, and a non-reviewer would never reach the gate at all.
	_, reviewer := evidenceRole(t, s, h, admin, "private reviewer five", string(authz.ReportReviewPrivate))
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != 200 {
		t.Fatalf("the reviewer could not read the queue while enabled: %d", w.Code)
	}
	s.config.PrivateConversations = false
	if w := request(t, h, "GET", "/api/messages/"+messageID+"/private-report-preview", nil, a); w.Code != http.StatusConflict {
		t.Fatalf("preview survived the feature being disabled: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != http.StatusConflict {
		t.Fatalf("review survived the feature being disabled: %d", w.Code)
	}
	var stored int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_message_reports`).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("disabling the feature changed stored evidence: count=%d err=%v", stored, err)
	}
	// The permitted control: re-enabling restores the routes and the evidence is
	// still there, and an account without the grant still cannot read the queue.
	s.config.PrivateConversations = true
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, reviewer); w.Code != 200 {
		t.Fatalf("the reviewer could not read the queue after re-enabling: %d", w.Code)
	}
	restored := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))["reports"].([]any)
	if len(restored) != 1 || restored[0].(map[string]any)["snapshot_text"] != "the words being reported" {
		t.Fatalf("the retained evidence did not survive the feature being turned off and on: %v", restored)
	}
}

// The durable hourly bound is enforced, and it survives the account's next
// submission rather than resetting on a restart the way an in-memory window does.
func TestPrivateEvidenceEnforcesADurableHourlySubmissionBound(t *testing.T) {
	s, h, _, a, _, messageID := privateEvidenceFixture(t)
	conversation := readableConversation(t, h, a)
	// Fill the window directly, so the test proves the stored counter is what the
	// limiter reads rather than an in-memory value that happens to agree.
	if _, err := s.store.Exec(`INSERT INTO private_report_rate_limits(user_id,window_started_at,submissions) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET submissions=excluded.submissions`,
		evidenceUserID(t, h, a), db.Now(), privateReportHourlyLimit); err != nil {
		t.Fatal(err)
	}
	// A second, distinct message so the refusal is about the limit and not the
	// existing open report.
	second := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", messageInput{
		Content: "another reported message", IdempotencyKey: "evidence-2",
	}, a)
	if second.Code != 201 {
		t.Fatalf("seed a second message: %d %s", second.Code, second.Body.String())
	}
	secondID := readJSON[map[string]any](t, second)["id"].(string)

	refused := submitPrivate(t, h, a, secondID, privatePreview(t, h, a, secondID).Version, "unwelcome")
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("the durable bound was not enforced: %d %s", refused.Code, refused.Body.String())
	}
	if refused.Header().Get("Retry-After") == "" {
		t.Fatal("a refused submission gave no Retry-After")
	}
	// The permitted control: an account with room in its window still submits.
	_, other := memberAccount(t, s, "room_in_window")
	otherPeerID, otherPeer := memberAccount(t, s, "room_in_window_peer")
	accepted := request(t, h, "POST", "/api/directs", map[string]string{"user_id": otherPeerID, "introduction": "hi"}, other)
	if accepted.Code != 201 {
		t.Fatalf("control request: %d %s", accepted.Code, accepted.Body.String())
	}
	otherConversation := readJSON[directConversation](t, accepted).ID
	if w := request(t, h, "POST", "/api/directs/"+otherConversation+"/accept", map[string]any{}, otherPeer); w.Code != 200 {
		t.Fatalf("control accept: %d %s", w.Code, w.Body.String())
	}
	otherMessage := request(t, h, "POST", "/api/conversations/"+otherConversation+"/messages", messageInput{
		Content: "control wording", IdempotencyKey: "evidence-control",
	}, other)
	if otherMessage.Code != 201 {
		t.Fatalf("control message: %d %s", otherMessage.Code, otherMessage.Body.String())
	}
	control := submitPrivate(t, h, other, readJSON[map[string]any](t, otherMessage)["id"].(string), "", "unwelcome")
	if control.Code == http.StatusTooManyRequests {
		t.Fatalf("an account with room in its window was refused: %s", control.Body.String())
	}
	_ = messageID
}

// R10-01: a valid message is reportable whatever script it is written in. The
// preview version used to be the whole content, so a 4,000-character CJK or emoji
// message produced a version over the submission bound and was refused with a
// message claiming the preview was merely stale (completion review 10). The
// version is now a fixed-size digest, which is asserted here by the byte length
// being the same for every script.
func TestEveryScriptIsReportableAtTheSupportedLength(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"ascii", strings.Repeat("a", 4000)},
		{"cjk", strings.Repeat("需", 4000)},
		{"emoji", strings.Repeat("🔒", 4000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, admin, a, _, conversation := privateEvidenceFixtureWithConversation(t)
			sent := request(t, h, "POST", "/api/conversations/"+conversation+"/messages", messageInput{
				Content: tc.body, IdempotencyKey: "unicode-report-" + tc.name,
			}, a)
			if sent.Code != 201 {
				t.Fatalf("send: %d %s", sent.Code, sent.Body.String())
			}
			id := readJSON[map[string]any](t, sent)["id"].(string)
			preview := privatePreview(t, h, a, id)
			// The version identifies the text without carrying it, so its size is
			// the same whatever the message is written in.
			if len(preview.Version) != privateEvidenceVersionLength {
				t.Fatalf("version is %d bytes, want %d: %s", len(preview.Version), privateEvidenceVersionLength, preview.Version)
			}
			if w := submitPrivate(t, h, a, id, preview.Version, "unwelcome"); w.Code != 201 {
				t.Errorf("a %s message was not reportable: %d %s", tc.name, w.Code, w.Body.String())
			}
			// The permitted control that the same text still has to be the saved
			// evidence, so a bounded version did not quietly weaken the disclosure.
			_, reviewer := evidenceRole(t, s, h, admin, "unicode reviewer", string(authz.ReportReviewPrivate))
			queue := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))
			records := queue["reports"].([]any)
			if len(records) != 1 {
				t.Fatalf("queue length = %d", len(records))
			}
			stored := records[0].(map[string]any)["snapshot_text"].(string)
			if len([]rune(stored)) != 4000 {
				t.Errorf("stored %d characters, want the 4,000 the message carried", len([]rune(stored)))
			}
		})
	}
}

// R10-02: a review of private evidence is an administrative act and must stay
// accountable, but naming the reviewer, the report and the time discloses that a
// specific person reported a specific private message. It is therefore readable
// only under the evidence policy, and absent from the community audit feed that an
// audit-only reader can read (completion review 10).
func TestPrivateReviewAuditIsRestrictedToTheEvidencePolicy(t *testing.T) {
	s, h, admin, a, _, messageID := privateEvidenceFixture(t)
	_, auditor := evidenceRole(t, s, h, admin, "audit only reader", string(authz.AuditRead))
	_, reviewer := evidenceRole(t, s, h, admin, "private reviewer audited", string(authz.ReportReviewPrivate))

	preview := privatePreview(t, h, a, messageID)
	if w := submitPrivate(t, h, a, messageID, preview.Version, "unwelcome"); w.Code != 201 {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	reportID := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/private-reports", nil, reviewer))["reports"].([]any)[0].(map[string]any)["id"].(string)
	if w := request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]string{
		"status": "resolved", "note": "triaged",
	}, reviewer); w.Code != 200 {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}

	// The community audit reader is refused the queue, and the community audit
	// describes no private review.
	if w := request(t, h, "GET", "/api/admin/private-reports", nil, auditor); w.Code != http.StatusForbidden {
		t.Fatalf("the audit-only reader reached the private queue: %d", w.Code)
	}
	for _, entry := range readJSON[[]map[string]string](t, request(t, h, "GET", "/api/admin/audit-logs", nil, auditor)) {
		if strings.HasPrefix(entry["action"], "private_report") || entry["target_id"] == reportID {
			t.Errorf("private review metadata reached an audit-only reader: %+v", entry)
		}
	}
	// The review is still accountable: it is recorded and readable under the
	// evidence policy rather than simply dropped.
	restricted := readJSON[[]map[string]string](t, request(t, h, "GET", "/api/admin/private-reports/audit", nil, reviewer))
	if len(restricted) == 0 {
		t.Fatal("the private review was not recorded at all")
	}
	found := false
	for _, entry := range restricted {
		if entry["target_id"] == reportID && entry["action"] == "private_report.resolved" {
			found = true
		}
	}
	if !found {
		t.Errorf("the private review is not readable under the evidence policy: %+v", restricted)
	}
	// And the community audit reader cannot reach that feed either.
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, auditor); w.Code != http.StatusForbidden {
		t.Fatalf("the audit-only reader reached the private audit feed: %d", w.Code)
	}
}

// R10-06: a request the recipient can act on must appear in the count. The
// requester's own contact preference controls whether they may *receive* new
// requests; it does not withdraw one they already sent, so filtering on it made the
// badge read zero while the inbox held an actionable request.
func TestRequesterOptingOutDoesNotWithdrawAnOutstandingRequest(t *testing.T) {
	s, h, _, _, b, _ := privateEvidenceFixtureWithConversation(t)
	bobID := evidenceUserID(t, h, b)
	// The fixture pair is already accepted, so the pending request below is made
	// by a fresh account: re-requesting an accepted peer would not be pending.
	_, requester := memberAccount(t, s, "opt_out_requester")
	asked := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bobID, "introduction": "may I"}, requester)
	if asked.Code != 201 {
		t.Fatalf("request: %d %s", asked.Code, asked.Body.String())
	}
	// The requester opts out of receiving new requests afterwards.
	if w := request(t, h, "PUT", "/api/me/contact-preferences", map[string]any{"allow_requests": false}, requester); w.Code != 200 {
		t.Fatalf("opt out: %d %s", w.Code, w.Body.String())
	}
	count, err := s.outstandingIncomingRequests(t.Context(), s.store, db.User{ID: bobID})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("an actionable request counted %d: opting out of receiving requests did not withdraw the one that was sent", count)
	}
	// The permitted control: accepting it clears the count.
	accepted := request(t, h, "POST", "/api/directs/"+readJSON[directConversation](t, asked).ID+"/accept", map[string]any{}, b)
	if accepted.Code != 200 {
		t.Fatalf("accept: %d %s", accepted.Code, accepted.Body.String())
	}
	if count, err := s.outstandingIncomingRequests(t.Context(), s.store, db.User{ID: bobID}); err != nil || count != 0 {
		t.Fatalf("count after accepting = %d (err %v), want 0", count, err)
	}
	// And a block does clear it, so the count is not simply unconditional.
	blockerID, blocker := memberAccount(t, s, "second_requester")
	if other := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bobID, "introduction": "again"}, blocker); other.Code != 201 {
		t.Fatalf("second request: %d %s", other.Code, other.Body.String())
	}
	if w := request(t, h, "PUT", "/api/me/blocks/"+blockerID, map[string]any{}, b); w.Code != 200 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	if count, err := s.outstandingIncomingRequests(t.Context(), s.store, db.User{ID: bobID}); err != nil || count != 0 {
		t.Fatalf("count after blocking = %d (err %v), want 0", count, err)
	}
}
