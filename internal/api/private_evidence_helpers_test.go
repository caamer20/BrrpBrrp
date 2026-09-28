package api

import (
	"database/sql"
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

// Helpers shared by the private-evidence tests (ADR 0013).
//
// Roles are created and edited through the real role API rather than by writing
// role rows, so a test cannot grant a capability in a way an operator could not,
// and a revocation goes through the same code path that bumps the authorization
// revision and fences live connections.

// evidenceRole creates a role holding exactly the given capabilities and returns
// it with a cookie for an account that holds only that role.
func evidenceRole(t *testing.T, s *Server, h http.Handler, admin *http.Cookie, name string, capabilities ...string) (db.Role, *http.Cookie) {
	t.Helper()
	w := request(t, h, "POST", "/api/roles", map[string]any{
		"name": name, "position": 20, "permissions": capabilities,
	}, admin)
	if w.Code != 201 {
		t.Fatalf("create role: %d %s", w.Code, w.Body.String())
	}
	role := readJSON[db.Role](t, w)
	id, cookie := memberAccount(t, s, name+"_holder")
	// The account holds only the new role, so "this grant is what opened the
	// queue" is a fact about the fixture rather than an assumption.
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := db.AssignAccountRole(t.Context(), tx, id, role.ID); err != nil {
		t.Fatalf("assign %s: %v", role.ID, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return role, cookie
}

// evidenceUserID is the account's own id, for the few places a test writes a
// durable row directly rather than through an API.
func evidenceUserID(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	state := readJSON[map[string]any](t, request(t, h, "GET", "/api/state", nil, cookie))
	return state["user"].(map[string]any)["id"].(string)
}

// revokeEvidenceRole removes a grant through the role API, which is what makes
// the revocation a real authority change rather than a test-only edit.
func revokeEvidenceRole(t *testing.T, h http.Handler, admin *http.Cookie, role db.Role) {
	t.Helper()
	setRolePermissions(t, h, admin, role, nil)
}

// restoreEvidenceRole puts a grant back: the permitted control for a fencing test.
func restoreEvidenceRole(t *testing.T, h http.Handler, admin *http.Cookie, role db.Role, capabilities ...string) {
	t.Helper()
	setRolePermissions(t, h, admin, role, capabilities)
}

// peerOf returns the other participant in the account's conversation. A pending
// request is not readable, so the readable filter that suited the message fixtures
// would fail here; the first conversation will do.
func peerOf(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	directs := readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, cookie))
	for _, direct := range directs {
		if direct.CanRead || direct.RequestState == "pending" {
			return direct.Peer.ID
		}
	}
	t.Fatal("the account has no direct conversation")
	return ""
}

// readableConversation returns the conversation the account can currently read.
func readableConversation(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	for _, direct := range readJSON[[]directConversation](t, request(t, h, "GET", "/api/directs", nil, cookie)) {
		if direct.CanRead {
			return direct.ID
		}
	}
	t.Fatal("the account has no readable direct conversation")
	return ""
}

var _ = sql.ErrNoRows

// conversationFor finds the conversation between two accounts, for a case that needs
// it by identity rather than from a response.
func conversationFor(t *testing.T, s *Server, a, b string) string {
	t.Helper()
	low, high := orderedDirectPair(a, b)
	var id string
	if err := s.store.QueryRow(`SELECT conversation_id FROM direct_pairs WHERE user_low=? AND user_high=?`, low, high).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
