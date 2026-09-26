package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func TestInvitationSingleUseAndDeactivation(t *testing.T) {
	s, handler := testServer(t)
	cookie := setupAdmin(t, s, handler)
	w := request(t, handler, "POST", "/api/admin/invitations", map[string]int{"hours": 24}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var invitation struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &invitation)
	token := strings.Split(invitation.URL, "#invite=")[1]
	var stored string
	if err := s.store.QueryRow(`SELECT token_hash FROM invitations WHERE id=?`, invitation.ID).Scan(&stored); err != nil || stored == token {
		t.Fatal("invitation token not hashed")
	}
	input := map[string]string{"username": "invited", "password": "a long invitation password", "display_name": "Invited", "token": token}
	w = request(t, handler, "POST", "/api/invitations/accept", input, nil)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	memberCookie := w.Result().Cookies()[0]
	input["username"] = "second_user"
	if w := request(t, handler, "POST", "/api/invitations/accept", input, nil); w.Code != 400 {
		t.Fatalf("reused invitation: %d", w.Code)
	}
	var id string
	s.store.QueryRow(`SELECT id FROM users WHERE username='invited'`).Scan(&id)
	if w := request(t, handler, "PATCH", "/api/admin/users/"+id, map[string]any{"confirm": "invited", "is_deactivated": true}, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, memberCookie); w.Code != 401 {
		t.Fatal("deactivated session survived")
	}
	if w := request(t, handler, "POST", "/api/login", map[string]string{"username": "invited", "password": input["password"]}, nil); w.Code != 401 {
		t.Fatal("deactivated user signed in")
	}
	var count int
	s.store.QueryRow(`SELECT count(*) FROM users WHERE id=?`, id).Scan(&count)
	if count != 1 {
		t.Fatal("deactivation erased account")
	}
}

// acceptInvitationToken consumes an invitation URL and returns the new account's
// id and session.
func acceptInvitationToken(t *testing.T, s *Server, handler http.Handler, url, username, password string) (string, *http.Cookie) {
	t.Helper()
	parts := strings.Split(url, "#invite=")
	if len(parts) != 2 || len(parts[1]) != 43 {
		t.Fatalf("invitation URL is not a token: %q", url)
	}
	w := request(t, handler, "POST", "/api/invitations/accept", map[string]string{"username": username, "password": password, "display_name": username, "token": parts[1]}, nil)
	if w.Code != 201 {
		t.Fatalf("accept %s: %d %s", username, w.Code, w.Body.String())
	}
	var id string
	if err := s.store.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, w.Result().Cookies()[0]
}

// TestRoleListReportsWhatTheCallerMayDo pins the flags the management UI reads:
// the client should not offer a control the engine would always refuse.
func TestRoleListReportsWhatTheCallerMayDo(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	views := readJSON[[]struct {
		Key       string `json:"key"`
		Grantable bool   `json:"grantable"`
		Editable  bool   `json:"editable"`
		Deletable bool   `json:"deletable"`
	}](t, request(t, handler, "GET", "/api/roles", nil, admin))
	// The administrator holds position 80, so it may act on every role below it:
	// it may grant and edit the member and moderator roles, but the default role is
	// never deletable, built-in roles are protected, and it cannot act on a role at
	// its own position.
	want := map[string][3]bool{
		"admin":     {false, false, false},
		"moderator": {true, true, false},
		"member":    {true, true, false},
	}
	seen := map[string]bool{}
	for _, view := range views {
		expected, known := want[view.Key]
		if !known {
			t.Fatalf("unexpected seeded role %s", view.Key)
		}
		seen[view.Key] = true
		got := [3]bool{view.Grantable, view.Editable, view.Deletable}
		if got != expected {
			t.Fatalf("administrator controls on %s = %v, want %v", view.Key, got, expected)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("seeded roles missing from the list: %v", views)
	}
}

// roleIDsByKey indexes the seeded roles so a test can name a role by what it is
// rather than by a hard-coded identifier.
func roleIDsByKey(t *testing.T, s *Server) map[string]string {
	t.Helper()
	catalog, err := s.store.Roles(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string, len(catalog))
	for _, role := range catalog {
		ids[role.Key] = role.ID
	}
	return ids
}

// TestInvitationCarriesRoleGrants is the invite-time role-selection contract: the
// roles an invitation promises are the roles the new account receives, and the
// promise can never exceed its author's delegation authority — not when it is
// made, and not when it is kept.
func TestInvitationCarriesRoleGrants(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	roles := roleIDsByKey(t, s)

	// An invitation that names no roles keeps the previous behaviour exactly.
	plainResponse := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24}, admin)
	if plainResponse.Code != 201 {
		t.Fatal(plainResponse.Body.String())
	}
	plain := readJSON[struct {
		URL   string   `json:"url"`
		Roles []string `json:"roles"`
	}](t, plainResponse)
	if len(plain.Roles) != 0 {
		t.Fatalf("an invitation with no roles promised %v", plain.Roles)
	}
	plainID, _ := acceptInvitationToken(t, s, handler, plain.URL, "plain_user", "a long invitation password")
	if keys := assignedRoleKeys(t, s, plainID); strings.Join(keys, ",") != "member" {
		t.Fatalf("an invitation with no roles granted %v", keys)
	}

	// A role the author does not outrank cannot be promised, and an unknown role
	// is a client mistake rather than a refusal of authority.
	if w := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24, "role_ids": []string{roles["admin"]}}, admin); w.Code != 403 {
		t.Fatalf("an administrator promised the administrator role: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24, "role_ids": []string{"role-not-real"}}, admin); w.Code != 400 {
		t.Fatalf("unknown role: %d %s", w.Code, w.Body.String())
	}

	// A role the author does outrank is carried through to the new account.
	moderatorResponse := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24, "role_ids": []string{roles["moderator"]}}, admin)
	if moderatorResponse.Code != 201 {
		t.Fatal(moderatorResponse.Body.String())
	}
	moderator := readJSON[struct {
		URL     string   `json:"url"`
		RoleIDs []string `json:"role_ids"`
	}](t, moderatorResponse)
	if len(moderator.RoleIDs) != 1 || moderator.RoleIDs[0] != roles["moderator"] {
		t.Fatalf("invitation role_ids = %v", moderator.RoleIDs)
	}
	// The pending list says what each invitation will grant, so an administrator
	// can see a promise they made weeks ago.
	if list := request(t, handler, "GET", "/api/admin/invitations", nil, admin); !strings.Contains(list.Body.String(), "Moderator") {
		t.Fatalf("pending invitation omitted its roles: %s", list.Body.String())
	}
	moderatorID, moderatorCookie := acceptInvitationToken(t, s, handler, moderator.URL, "helpful_user", "another long invitation password")
	if keys := assignedRoleKeys(t, s, moderatorID); strings.Join(keys, ",") != "member,moderator" {
		t.Fatalf("invited moderator holds %v", keys)
	}
	// The grant is real authority, not a row: reading the audit log is what a
	// moderator has and a member does not.
	if w := request(t, handler, "GET", "/api/admin/audit-logs", nil, moderatorCookie); w.Code != 200 {
		t.Fatalf("invited moderator lacked audit access: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24}, moderatorCookie); w.Code != 403 {
		t.Fatalf("invited moderator could invite: %d %s", w.Code, w.Body.String())
	}

	// A promise is re-checked when it is kept: an invitation that outlives its
	// author's demotion delivers the default role and nothing more.
	deputy := readJSON[db.User](t, request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "deputy", "password": "a different private passphrase", "display_name": "Deputy", "is_admin": true}, admin))
	deputyCookie := request(t, handler, "POST", "/api/login", map[string]string{"username": "deputy", "password": "a different private passphrase"}, nil).Result().Cookies()[0]
	queuedResponse := request(t, handler, "POST", "/api/admin/invitations", map[string]any{"hours": 24, "role_ids": []string{roles["moderator"]}}, deputyCookie)
	if queuedResponse.Code != 201 {
		t.Fatal(queuedResponse.Body.String())
	}
	queued := readJSON[struct {
		URL string `json:"url"`
	}](t, queuedResponse)
	if w := request(t, handler, "PATCH", "/api/admin/users/"+deputy.ID, map[string]any{"is_admin": false, "confirm": "deputy"}, admin); w.Code != 200 {
		t.Fatalf("demotion: %d %s", w.Code, w.Body.String())
	}
	staleID, _ := acceptInvitationToken(t, s, handler, queued.URL, "stale_promise", "one more long invitation password")
	if keys := assignedRoleKeys(t, s, staleID); strings.Join(keys, ",") != "member" {
		t.Fatalf("a demoted inviter still delivered a promise: %v", keys)
	}
}

// assignedRoleKeys lists an account's explicit role grants.
func assignedRoleKeys(t *testing.T, s *Server, userID string) []string {
	t.Helper()
	assigned, err := s.store.AssignedRoles(t.Context(), s.store, userID)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(assigned))
	for _, role := range assigned {
		keys = append(keys, role.Key)
	}
	sort.Strings(keys)
	return keys
}
