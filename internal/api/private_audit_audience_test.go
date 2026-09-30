package api

import (
	"net/http"
	"strings"
	"testing"

	"brrpbrrp/internal/authz"
)

// V14-01, second half. The restricted feed now requires the *evidence* capability
// rather than the community audit one, which closed the ordinary-auditor leak. It
// opened a different one: the query selects every restricted prefix, so an account
// holding `report.review_private` is also handed private group membership — a grant
// made to see submitted text, used to browse who is in whose group.
//
// The two are separate policies on one feed, and both directions are asserted. A
// policy only ever checked in the denying direction is a policy nobody has confirmed
// still works.

// auditFixture produces one real evidence review *and* one real group invitation, so
// the feed has both kinds of row and a test can tell which one leaked.
type auditFixture struct {
	server   *Server
	handler  http.Handler
	owner    *http.Cookie
	reviewer *http.Cookie
	auditor  *http.Cookie
	groupID  string
}

func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, h := testServer(t)
	// The config field, not only the environment variable: the private routes read
	// `s.config.PrivateConversations`, so an env-only fixture has them refusing.
	s.config.PrivateConversations = true
	admin := setupAdmin(t, s, h)
	// The owner designation is what makes the operator's broader view a *policy*
	// rather than a different grant, and it is the real ceremony rather than a
	// test-only shortcut.
	enrollMFA(t, h, admin, "correct horse battery")
	if w := request(t, h, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatalf("designate the owner: %d %s", w.Code, w.Body.String())
	}
	_, reviewer := evidenceRole(t, s, h, admin, "evidence reviewer", string(authz.ReportReviewPrivate))
	_, auditor := evidenceRole(t, s, h, admin, "community auditor", string(authz.AuditRead))

	// A real private conversation with a real submitted report and a real review.
	// `memberAccount` keeps the default role; `namedUser` strips role assignments, so
	// an account made with it holds no capability at all and cannot send anything.
	recipientID, recipient := memberAccount(t, s, "audit_recipient")
	_, sender := memberAccount(t, s, "audit_sender")
	created := request(t, h, "POST", "/api/directs",
		map[string]string{"user_id": recipientID, "introduction": "may I join"}, sender)
	if created.Code < 200 || created.Code >= 300 {
		t.Fatalf("create the direct request: %d %s", created.Code, created.Body.String())
	}
	directID, _ := readJSON[map[string]any](t, created)["id"].(string)
	if directID == "" {
		t.Fatalf("no direct conversation was created: %s", created.Body.String())
	}
	if accepted := request(t, h, "POST", "/api/directs/"+directID+"/accept", map[string]any{}, recipient); accepted.Code != 200 {
		t.Fatalf("accept the direct request: %d %s", accepted.Code, accepted.Body.String())
	}
	if w := request(t, h, "POST", "/api/channels/"+directID+"/messages",
		map[string]any{"content": "words worth reporting", "idempotency_key": "audit-dm-1"}, sender); w.Code != 201 {
		t.Fatalf("seed the private message: %d %s", w.Code, w.Body.String())
	}
	preview := request(t, h, "GET", "/api/messages/"+messageIDFor(t, s, directID)+"/private-report-preview", nil, recipient)
	if preview.Code != 200 {
		t.Fatalf("preview the private report: %d %s", preview.Code, preview.Body.String())
	}
	previewBody := readJSON[map[string]any](t, preview)
	submitted := request(t, h, "POST", "/api/messages/"+messageIDFor(t, s, directID)+"/private-report", map[string]any{
		"version": previewBody["version"], "reason": "submitted for review",
	}, recipient)
	if submitted.Code < 200 || submitted.Code >= 300 {
		t.Fatalf("submit the evidence: %d %s", submitted.Code, submitted.Body.String())
	}
	reportID, _ := readJSON[map[string]any](t, submitted)["id"].(string)
	if reportID == "" {
		t.Fatalf("no evidence record was created: %s", submitted.Body.String())
	}
	reviewed := request(t, h, "PATCH", "/api/admin/private-reports/"+reportID, map[string]any{
		"status": "resolved", "note": "reviewed for the audit fixture",
	}, admin)
	if reviewed.Code >= 300 {
		t.Fatalf("review the evidence: %d %s", reviewed.Code, reviewed.Body.String())
	}

	// A real group with a real invitation, so a `group.` row exists.
	createdGroup := request(t, h, "POST", "/api/groups",
		map[string]any{"name": "the private group"}, recipient)
	if createdGroup.Code < 200 || createdGroup.Code >= 300 {
		t.Fatalf("create the group: %d %s", createdGroup.Code, createdGroup.Body.String())
	}
	groupID, _ := readJSON[map[string]any](t, createdGroup)["id"].(string)
	if groupID == "" {
		t.Fatalf("no group was created: %s", createdGroup.Body.String())
	}
	inviteeID, _ := memberAccount(t, s, "audit_invitee")
	invite(t, h, recipient, groupID, inviteeID)

	return &auditFixture{server: s, handler: h, owner: admin, reviewer: reviewer, auditor: auditor, groupID: groupID}
}

func messageIDFor(t *testing.T, s *Server, conversationID string) string {
	t.Helper()
	var id string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT id FROM messages WHERE conversation_id=? ORDER BY id LIMIT 1`, conversationID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// feedActions returns the set of action prefixes present in the private audit feed.
func feedActions(t *testing.T, cookie *http.Cookie, h http.Handler) map[string]bool {
	t.Helper()
	w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, cookie)
	if w.Code != 200 {
		t.Fatalf("read the private audit feed: %d %s", w.Code, w.Body.String())
	}
	seen := map[string]bool{}
	for _, entry := range readJSON[[]map[string]any](t, w) {
		if action, _ := entry["action"].(string); action != "" {
			prefix := action
			if i := strings.Index(action, "."); i > 0 {
				prefix = action[:i]
			}
			seen[prefix] = true
		}
	}
	return seen
}

// An evidence reviewer sees the evidence audit and *not* private group membership.
//
// A grant made to see submitted text must not become a grant to browse who is in
// whose private group. The two are separate facts about separate people, and the
// group one is disclosed by the row itself — the invitee's username and the group's
// identifier.
func TestAnEvidenceReviewerDoesNotAcquirePrivateGroupMembership(t *testing.T) {
	f := newAuditFixture(t)

	seen := feedActions(t, f.reviewer, f.handler)
	if !seen["private_report"] {
		t.Fatalf("an evidence reviewer cannot see the evidence audit it exists to be accountable for: %v", seen)
	}
	if seen["group"] {
		t.Fatalf("AN EVIDENCE REVIEWER WAS HANDED PRIVATE GROUP MEMBERSHIP: %v", seen)
	}
	// The refusal is not merely an absence of the `group` prefix: the body must not
	// name the invitee or the group anywhere.
	body := request(t, f.handler, "GET", "/api/admin/private-reports/audit", nil, f.reviewer).Body.String()
	for _, leak := range []string{"audit_invitee", f.groupID, "group.invited", "group.joined"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the evidence reviewer's feed disclosed %q: %s", leak, body)
		}
	}
}

// The owner administers the installation and sees both. This is a policy, not a
// different grant, and it has to keep working — otherwise the operator has lost the
// accountability record for group lifecycle events.
func TestTheOperatorSeesBothKindsOfRestrictedAudit(t *testing.T) {
	f := newAuditFixture(t)
	seen := feedActions(t, f.owner, f.handler)
	if !seen["private_report"] {
		t.Fatalf("the operator cannot see the evidence audit: %v", seen)
	}
	if !seen["group"] {
		t.Fatalf("THE OPERATOR CANNOT SEE PRIVATE GROUP LIFECYCLE AUDIT: %v", seen)
	}
}

// An ordinary community auditor still gets nothing, and the refusal body describes
// nothing either.
func TestAnOrdinaryAuditorIsRefusedWithoutDisclosure(t *testing.T) {
	f := newAuditFixture(t)
	w := request(t, f.handler, "GET", "/api/admin/private-reports/audit", nil, f.auditor)
	if w.Code != http.StatusForbidden {
		t.Fatalf("an ordinary auditor read the private feed: %d %s", w.Code, w.Body.String())
	}
	for _, leak := range []string{"audit_invitee", f.groupID, "private_report", "group.invited"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Fatalf("the refusal disclosed %q: %s", leak, w.Body.String())
		}
	}
}
