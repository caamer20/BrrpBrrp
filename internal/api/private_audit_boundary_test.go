package api

import (
	"net/http"
	"strings"
	"testing"

	"brrpbrrp/internal/authz"
)

// V14-01. The private audit feed is read under the *evidence* capability, not the
// community audit one.
//
// The feed names the reporter and the reviewer, so an account refused the evidence
// itself must not learn from the audit row that a specific person reported a specific
// private message, who reviewed it, or when. That is the fact the evidence policy
// exists to withhold, and an audit row states it plainly.
//
// This walks real report and review mutations and then reads the feed as each kind of
// principal, because the defect was in the *reader policy* and only a test that
// mutates something first can tell a policy from an empty table.
func TestPrivateAuditIsReadUnderTheEvidencePolicyNotTheCommunityOne(t *testing.T) {
	s, h, recipient, _, conversation, _, owner := requestFixture(t)

	// A real report and a real review, so the feed has rows an auditor could read.
	preview := previewRequest(t, h, recipient, conversation)
	if w := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "not welcome", "version": preview.Version, "operation_key": "boundary-key",
	}, recipient); w.Code != 201 {
		t.Fatalf("the report was refused: %d %s", w.Code, w.Body.String())
	}
	rows := readJSON[map[string]any](t, request(t, h, "GET", "/api/admin/request-reports", nil, owner))
	list, _ := rows["reports"].([]any)
	if len(list) == 0 {
		t.Fatalf("no request evidence was recorded, so the feed has nothing to leak: %v", rows)
	}
	first, _ := list[0].(map[string]any)
	evidenceID, _ := first["id"].(string)
	if w := request(t, h, "PATCH", "/api/admin/request-reports/"+evidenceID, map[string]any{
		"status": "resolved", "note": "boundary review",
	}, owner); w.Code >= 300 {
		t.Fatalf("the review was refused: %d %s", w.Code, w.Body.String())
	}

	// An ordinary community auditor: refused the evidence, and refused the feed too.
	_, auditor := evidenceRole(t, s, h, owner, "community audit only", string(authz.AuditRead))
	if w := request(t, h, "GET", "/api/admin/request-reports", nil, auditor); w.Code != http.StatusForbidden {
		t.Fatalf("an audit-only reader reached the evidence queue: %d", w.Code)
	}
	feed := request(t, h, "GET", "/api/admin/private-reports/audit", nil, auditor)
	if feed.Code != http.StatusForbidden {
		t.Fatalf("an audit-only reader read a feed naming private reporters: %d %s", feed.Code, feed.Body.String())
	}
	// The refusal body must not itself describe the event.
	for _, leak := range []string{"request_recipient", "admin", evidenceID, "private_request"} {
		if strings.Contains(feed.Body.String(), leak) {
			t.Fatalf("the refusal disclosed %q: %s", leak, feed.Body.String())
		}
	}

	// An unrelated administrator holds neither capability and is refused both.
	_, unrelated := evidenceRole(t, s, h, owner, "community moderator", string(authz.MessageDeleteAny))
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, unrelated); w.Code != http.StatusForbidden {
		t.Fatalf("an unrelated moderator read the private audit feed: %d", w.Code)
	}
	// An ordinary member is refused as well: a restricted feed any member could read
	// would be a community feed with a filter on it.
	memberID, member := memberAccount(t, s, "boundary_member")
	_ = memberID
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, member); w.Code != http.StatusForbidden {
		t.Fatalf("an ordinary member read the private audit feed: %d", w.Code)
	}

	// The owner and an authorized evidence reviewer can read it, and the row they get
	// is the accountable record — the review, the reviewer, the time.
	for name, cookie := range map[string]*http.Cookie{"the owner": owner} {
		w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, cookie)
		if w.Code != 200 {
			t.Fatalf("%s could not read the accountable record: %d %s", name, w.Code, w.Body.String())
		}
		entries := readJSON[[]map[string]any](t, w)
		found := false
		for _, entry := range entries {
			if action, _ := entry["action"].(string); strings.HasPrefix(action, "private_request.") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s could not see the request review they are entitled to: %v", name, entries)
		}
	}

	// Revocation is read-time on this feed, not a filter that forgets.
	revokedID, revoked := evidenceRole(t, s, h, owner, "revoked evidence reviewer", string(authz.ReportReviewPrivate))
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, revoked); w.Code != 200 {
		t.Fatalf("an evidence reviewer could not read the feed before revocation: %d", w.Code)
	}
	revokeEvidenceRole(t, h, owner, revokedID)
	if w := request(t, h, "GET", "/api/admin/private-reports/audit", nil, revoked); w.Code != http.StatusForbidden {
		t.Fatalf("a revoked reviewer still read the private audit feed: %d", w.Code)
	}
}
