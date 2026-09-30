package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

// Fixtures for the moderation journeys. They go through the supported routes wherever
// a route exists, because a fixture that seeds SQL directly cannot qualify a route's
// authorization — it only proves the handler reads what the fixture wrote.

func memberIDOf(t *testing.T, s *Server, username string) string {
	t.Helper()
	var id string
	if err := s.store.QueryRowContext(t.Context(), `SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		t.Fatalf("no account named %q: %v", username, err)
	}
	return id
}

func sessionFor(t *testing.T, s *Server, userID string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if err := s.newSession(w, userID); err != nil {
		t.Fatal(err)
	}
	return w.Result().Cookies()[0]
}

// assignedRoles reads an account's role assignments through the real route, so the
// fixture sees what an administrator would see rather than what a query returns.
func assignedRoles(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, userID string) []string {
	t.Helper()
	var listing struct {
		Roles []struct {
			ID string `json:"id"`
		} `json:"roles"`
	}
	w := request(t, handler, "GET", "/api/admin/users/"+userID+"/roles", nil, admin)
	if w.Code != 200 {
		t.Fatalf("read roles: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, role := range listing.Roles {
		ids = append(ids, role.ID)
	}
	return ids
}

// setRoles writes the whole set, which is what the route takes. Adding one role to a
// set the fixture cannot read is how a fixture ends up looking set up while the
// account holds a single role.
func setRoles(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, userID string, roleIDs ...string) {
	t.Helper()
	w := request(t, handler, "PUT", "/api/admin/users/"+userID+"/roles",
		map[string]any{"role_ids": roleIDs}, admin)
	if w.Code >= 400 {
		t.Fatalf("set roles: %d %s", w.Code, w.Body.String())
	}
}

func addRole(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, userID, roleID string) {
	t.Helper()
	current := assignedRoles(t, s, handler, admin, userID)
	for _, id := range current {
		if id == roleID {
			return
		}
	}
	setRoles(t, s, handler, admin, userID, append(current, roleID)...)
}

// createTestRole creates a role through the real route, so its permissions are
// validated the way an operator's would be.
func createTestRole(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, name string, permissions []string, position int) string {
	t.Helper()
	w := request(t, handler, "POST", "/api/roles",
		map[string]any{"name": name, "permissions": permissions, "position": position}, admin)
	if w.Code != 201 {
		t.Fatalf("create role %q: %d %s", name, w.Code, w.Body.String())
	}
	return readJSON[db.Role](t, w).ID
}

// recordedRolesString is the snapshot a timeout stores, read straight from the store
// because that is the value expiry will replay.
func recordedRolesString(t *testing.T, s *Server, userID string) string {
	t.Helper()
	rows, err := s.store.QueryContext(t.Context(), `SELECT role_id FROM role_assignments WHERE user_id=? ORDER BY role_id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(ids, ",")
}

// moderationMember creates an account through the real account route and returns its
// id and a session for it.
func moderationMember(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, username string, permissions []string) (string, *http.Cookie) {
	t.Helper()
	w := request(t, handler, "POST", "/api/admin/users",
		map[string]any{"username": username, "password": "a passphrase for moderation", "display_name": username}, admin)
	if w.Code != 201 {
		t.Fatalf("create account %q: %d %s", username, w.Code, w.Body.String())
	}
	id := readJSON[db.User](t, w).ID
	roleID := createTestRole(t, s, handler, admin, username+"_role", permissions, 30)
	addRole(t, s, handler, admin, id, roleID)
	return id, sessionFor(t, s, id)
}

func moderationChannel(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, name string) string {
	t.Helper()
	w := request(t, handler, "POST", "/api/channels",
		db.Channel{Name: name, Type: "text", Category: "Moderation", MaxVoiceUsers: 8, Position: 50}, admin)
	if w.Code != 201 {
		t.Fatalf("create channel: %d %s", w.Code, w.Body.String())
	}
	return readJSON[db.Channel](t, w).ID
}
