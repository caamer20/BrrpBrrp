package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"brrpbrrp/internal/db"
)

// delegatedManager creates an account whose only privileged grant is
// member.manage, at a position strictly below the built-in administrator. It is
// the role F-01 used to promote itself to full operator authority.
func delegatedManager(t *testing.T, s *Server, username string) (string, *http.Cookie, string) {
	t.Helper()
	roleID := "role-delegated-manager"
	now := db.Now()
	if _, err := s.store.Exec(`INSERT INTO roles(id,role_key,name,position,is_default,is_system,permissions,created_at,updated_at) VALUES(?,?,?,?,0,0,?,?,?)`,
		roleID, "delegated-manager", "Delegated member manager", 30, `["channel.read","message.send","member.manage"]`, now, now); err != nil {
		t.Fatal(err)
	}
	id, cookie := memberAccount(t, s, username)
	if _, err := s.store.Exec(`INSERT INTO role_assignments(user_id,role_id,assigned_by,created_at) VALUES(?,?,?,?)`, id, roleID, id, now); err != nil {
		t.Fatal(err)
	}
	return id, cookie, roleID
}

// capabilitiesOf reads the caller's effective grants from the bootstrap
// snapshot, which is what the client is told it may do.
func capabilitiesOf(t *testing.T, handler http.Handler, cookie *http.Cookie) map[string]bool {
	t.Helper()
	state := readJSON[map[string]any](t, request(t, handler, "GET", "/api/state", nil, cookie))
	authorization, _ := state["authorization"].(map[string]any)
	granted, _ := authorization["capabilities"].([]any)
	capabilities := make(map[string]bool, len(granted))
	for _, name := range granted {
		capabilities[name.(string)] = true
	}
	return capabilities
}

func accountIsAdmin(t *testing.T, s *Server, id string) bool {
	t.Helper()
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		t.Fatal(err)
	}
	return user.IsAdmin
}

// TestDelegatedMemberManagerCannotEscalateAuthority is the permanent regression
// for F-01: a caller holding only member.manage used to PATCH its own account to
// is_admin=true and then delete the designated owner, after which the operator
// bridge gave it backup and credential-recovery authority.
//
// Every entry point is exercised, and the same caller must still be able to do
// the ordinary delegated work it was granted.
func TestDelegatedMemberManagerCannotEscalateAuthority(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	managerID, manager, managerRoleID := delegatedManager(t, s, "manager")
	memberID, _ := memberAccount(t, s, "ordinary")
	adminID := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin)).ID

	// Self-promotion through the account endpoint.
	for _, attempt := range []struct {
		name string
		body map[string]any
	}{
		{"promote itself", map[string]any{"is_admin": true, "confirm": "manager"}},
		{"unban itself into admin", map[string]any{"is_admin": true, "is_banned": false, "confirm": "manager"}},
		{"deactivate itself into admin", map[string]any{"is_admin": true, "is_deactivated": false, "confirm": "manager"}},
	} {
		w := request(t, handler, "PATCH", "/api/admin/users/"+managerID, attempt.body, manager)
		if w.Code != http.StatusForbidden && w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", attempt.name, w.Code, w.Body.String())
		}
	}
	if accountIsAdmin(t, s, managerID) {
		t.Fatal("a delegated member manager promoted itself to administrator")
	}

	// A new administrator is administrator authority, so it is refused too.
	if w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "accomplice", "password": "a private initial passphrase", "is_admin": true}, manager); w.Code != http.StatusForbidden {
		t.Fatalf("delegated manager created an administrator: %d %s", w.Code, w.Body.String())
	}

	// Assigning itself a higher role is the same escalation by another route.
	if w := request(t, handler, "PUT", "/api/admin/users/"+managerID+"/roles", map[string]any{"role_ids": []string{managerRoleID, db.AdministratorRoleID}}, manager); w.Code != http.StatusForbidden {
		t.Fatalf("delegated manager granted itself the administrator role: %d %s", w.Code, w.Body.String())
	}
	if accountIsAdmin(t, s, managerID) {
		t.Fatal("role assignment promoted a delegated member manager")
	}

	// It cannot act on an account that outranks it, including the owner.
	for _, attempt := range []struct {
		name, method string
		body         any
	}{
		{"ban an administrator", "PATCH", map[string]any{"is_banned": true, "confirm": "admin"}},
		{"demote an administrator", "PATCH", map[string]any{"is_admin": false, "confirm": "admin"}},
		{"delete an administrator", "DELETE", map[string]any{"confirm": "admin"}},
		{"kick an administrator", "POST", map[string]any{"confirm": "admin"}},
	} {
		path := "/api/admin/users/" + adminID
		if attempt.name == "kick an administrator" {
			path += "/kick"
		}
		if w := request(t, handler, attempt.method, path, attempt.body, manager); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", attempt.name, w.Code, w.Body.String())
		}
	}

	// Legitimate delegated work still functions.
	if w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "recruit", "password": "a private initial passphrase", "display_name": "Recruit"}, manager); w.Code != http.StatusCreated {
		t.Fatalf("delegated manager could not create an ordinary member: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"display_name": "Renamed member"}, manager); w.Code != http.StatusOK {
		t.Fatalf("delegated manager could not rename an ordinary member: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"is_banned": true, "confirm": "ordinary"}, manager); w.Code != http.StatusOK {
		t.Fatalf("delegated manager could not ban an ordinary member: %d %s", w.Code, w.Body.String())
	}
}

// TestOwnershipIsNotEditableThroughAccountManagement proves the owner record is
// reachable only through the ownership ceremony: no delegation rule, and not the
// owner itself, may end ownership by editing or deleting an account.
func TestOwnershipIsNotEditableThroughAccountManagement(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	ownerID := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin)).ID
	// The owner may appoint another administrator; that is delegation the owner
	// still controls, not escalation.
	if w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "second_admin", "password": "a private initial passphrase", "is_admin": true}, admin); w.Code != http.StatusCreated {
		t.Fatalf("the owner could not appoint an administrator: %d %s", w.Code, w.Body.String())
	}
	secondID, second, _ := delegatedManager(t, s, "manager")
	_ = secondID

	for _, attempt := range []struct {
		name, method, path string
		body               any
		actor              *http.Cookie
	}{
		{"owner bans itself", "PATCH", "/api/admin/users/" + ownerID, map[string]any{"is_banned": true, "confirm": "admin"}, admin},
		{"owner demotes itself", "PATCH", "/api/admin/users/" + ownerID, map[string]any{"is_admin": false, "confirm": "admin"}, admin},
		{"owner deletes itself", "DELETE", "/api/admin/users/" + ownerID, map[string]any{"confirm": "admin"}, admin},
		{"delegated manager bans the owner", "PATCH", "/api/admin/users/" + ownerID, map[string]any{"is_banned": true, "confirm": "admin"}, second},
		{"delegated manager deletes the owner", "DELETE", "/api/admin/users/" + ownerID, map[string]any{"confirm": "admin"}, second},
		{"delegated manager re-writes the owner's roles", "PUT", "/api/admin/users/" + ownerID + "/roles", map[string]any{"role_ids": []string{db.MemberRoleID}}, second},
	} {
		w := request(t, handler, attempt.method, attempt.path, attempt.body, attempt.actor)
		if w.Code == http.StatusOK {
			t.Fatalf("%s: %d %s", attempt.name, w.Code, w.Body.String())
		}
	}
	var owners int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE is_owner=1`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("ownership after the attempts = %d, want 1", owners)
	}
}

// TestOwnerlessBootstrapCannotBeReEntered is the permanent regression for the
// second half of F-01. The pre-ownership compatibility bridge is on only while
// the installation has never completed the ceremony. Deleting the owner record —
// which the endpoint now refuses, so this simulates direct database access or an
// installation damaged by the old defect — must not hand installation authority
// back to an administrator.
func TestOwnerlessBootstrapCannotBeReEntered(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	collector := func() map[string]bool { return capabilitiesOf(t, handler, admin) }

	// Before any ceremony: the compatibility bridge gives the working
	// administrator the operator tier, which is what keeps a fresh installation
	// administrable.
	if !collector()["operator.backup"] {
		t.Fatal("a fresh installation's administrator lacks the pre-ownership bridge")
	}
	enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	if !collector()["operator.credential_recovery"] {
		t.Fatal("the owner does not hold the operator tier")
	}

	// The owner record disappears. A pre-ownership installation looks exactly
	// like this, and the durable record is the only thing that tells them apart.
	if _, err := s.store.Exec(`UPDATE users SET is_owner=0`); err != nil {
		t.Fatal(err)
	}
	granted := collector()
	for _, capability := range []string{"operator.backup", "operator.credential_recovery"} {
		if granted[capability] {
			t.Fatalf("deleting the owner record recreated %s for an administrator", capability)
		}
	}
	if !granted["member.manage"] {
		t.Fatal("the administrator lost its ordinary administrator authority")
	}
	// Completing the ceremony stays reachable, and it is the only operator-tier
	// action that does: it grants nothing by itself and creates an owner.
	if !granted["operator.ownership_transfer"] {
		t.Fatal("a stranded installation has no way to complete the ceremony again")
	}
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("a stranded installation could not complete the ceremony again: %d %s", w.Code, w.Body.String())
	}
	if !collector()["operator.backup"] {
		t.Fatal("ownership was not restored")
	}
}

// TestAuthorityChangesCarryTheirAuditRecord covers F-07: the audit log cannot be
// switched off, and an authority change is recorded in the same transaction, so
// the two cannot disagree.
func TestAuthorityChangesCarryTheirAuditRecord(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	manager, managerCookie, _ := delegatedManager(t, s, "manager")

	settings := s.Settings()
	settings.Audit = false
	if w := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, admin); w.Code != http.StatusBadRequest {
		t.Fatalf("the audit log was switched off: %d %s", w.Code, w.Body.String())
	}
	settings = s.Settings()
	settings.RequirePrivilegedMFA = false
	if w := request(t, handler, "PUT", "/api/admin/settings", map[string]any{"settings": settings}, managerCookie); w.Code != http.StatusForbidden {
		t.Fatalf("routine settings access weakened the privileged second-factor policy: %d %s", w.Code, w.Body.String())
	}

	var before int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "PUT", "/api/admin/users/"+manager+"/roles", map[string]any{"role_ids": []string{}}, managerCookie); w.Code != http.StatusForbidden {
		t.Fatalf("refused role change: %d %s", w.Code, w.Body.String())
	}
	var recorded int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.roles_changed'`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatalf("a refused role change wrote %d audit rows", recorded)
	}
	if _, err := s.store.Exec(`INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,NULL,'probe','',?)`, db.ID(), db.Now()); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("audit rows = %d, want %d", after, before+1)
	}
	// A real authority change lands with its record.
	if w := request(t, handler, "PUT", "/api/admin/users/"+manager+"/roles", map[string]any{"role_ids": []string{}}, admin); w.Code != http.StatusOK {
		t.Fatalf("admin role removal: %d %s", w.Code, w.Body.String())
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.roles_changed'`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 1 {
		t.Fatalf("authority change wrote %d audit rows, want 1", recorded)
	}
}

// revokeDuringRequest is a request body that performs a change the first time the
// handler reads it.
//
// The route guard has already resolved authority by then, and the handler has not
// opened its transaction, so this places the change exactly where the guard and
// the commit would otherwise disagree — deterministically, without a sleep or a
// race.
type revokeDuringRequest struct {
	body   []byte
	offset int
	revoke func() error
	once   sync.Once
}

func bodyThatRevokes(t *testing.T, body any, revoke func() error) *revokeDuringRequest {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return &revokeDuringRequest{body: encoded, revoke: revoke}
}

func (r *revokeDuringRequest) Read(p []byte) (int, error) {
	var err error
	r.once.Do(func() { err = r.revoke() })
	if err != nil {
		return 0, err
	}
	if r.offset >= len(r.body) {
		return 0, io.EOF
	}
	n := copy(p, r.body[r.offset:])
	r.offset += n
	return n, nil
}

// pausedRequest sends a mutation whose body is released only by the revocation it
// carries, and returns the handler's answer.
func pausedRequest(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie, body *revokeDuringRequest) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestAuthorityRevokedDuringARequestCannotCommit is the F-03 acceptance case: a
// privileged request is paused after authentication, the session, the role, the
// account or the ownership is changed, and the resumed request must not commit.
// The matching control case proves each request succeeds when nothing changed, so
// a refusal below is about the revocation rather than a broken request.
func TestAuthorityRevokedDuringARequestCannotCommit(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "ordinary")

	if w := request(t, handler, "POST", "/api/roles", map[string]any{"name": "control role", "position": 40, "permissions": []string{"channel.read"}}, admin); w.Code != http.StatusCreated {
		t.Fatalf("control request: %d %s", w.Code, w.Body.String())
	}

	// The role that authorized the mutation is revoked mid-request.
	demoted := bodyThatRevokes(t, map[string]any{"name": "late channel", "type": "text", "max_voice_users": 8}, func() error {
		if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, adminUserID(t, handler, admin), db.AdministratorRoleID); err != nil {
			return err
		}
		_, err := s.store.Exec(`UPDATE users SET is_admin=0 WHERE id=?`, adminUserID(t, handler, admin))
		return err
	})
	if w := pausedRequest(t, handler, "POST", "/api/channels", admin, demoted); w.Code != http.StatusForbidden {
		t.Fatalf("a revoked channel manager created a channel: %d %s", w.Code, w.Body.String())
	}
	// The same request without the revocation is allowed, and the authority comes
	// back so the remaining cases run as an administrator.
	if _, err := s.store.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, adminUserID(t, handler, admin)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT OR IGNORE INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, adminUserID(t, handler, admin), db.AdministratorRoleID, db.Now()); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "POST", "/api/channels", map[string]any{"name": "allowed channel", "type": "text", "max_voice_users": 8}, admin); w.Code != http.StatusCreated {
		t.Fatalf("control channel: %d %s", w.Code, w.Body.String())
	}

	// Ownership moves away from the acting account mid-request, so an operator
	// action it was authorized for is no longer its authority to exercise.
	enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	transferred := bodyThatRevokes(t, map[string]any{"new_password": "a replaced passphrase", "confirm": "ordinary"}, func() error {
		if _, err := s.store.Exec(`UPDATE users SET is_owner=0`); err != nil {
			return err
		}
		_, err := s.store.Exec(`UPDATE users SET is_owner=1 WHERE id=?`, memberID)
		return err
	})
	if w := pausedRequest(t, handler, "POST", "/api/admin/users/"+memberID+"/reset-password", admin, transferred); w.Code != http.StatusForbidden {
		t.Fatalf("a former owner reset a password: %d %s", w.Code, w.Body.String())
	}
	var replaced int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE id=? AND password_hash='unused-test-hash'`, memberID).Scan(&replaced); err != nil {
		t.Fatal(err)
	}
	if replaced != 1 {
		t.Fatal("the refused request changed the target password")
	}
	_ = member

	// The account is deactivated mid-request.
	deactivated := bodyThatRevokes(t, map[string]any{"name": "too late", "position": 41, "permissions": []string{"channel.read"}}, func() error {
		_, err := s.store.Exec(`UPDATE users SET is_deactivated=1 WHERE id=?`, adminUserID(t, handler, admin))
		return err
	})
	if w := pausedRequest(t, handler, "POST", "/api/roles", admin, deactivated); w.Code != http.StatusUnauthorized {
		t.Fatalf("a deactivated account committed a mutation: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=0 WHERE id=?`, adminUserID(t, handler, admin)); err != nil {
		t.Fatal(err)
	}

	// The session this request was authenticated with ends mid-request. It runs
	// last because the cookie cannot be used again afterwards.
	endedSession := bodyThatRevokes(t, map[string]any{"name": "too late", "position": 41, "permissions": []string{"channel.read"}}, func() error {
		_, err := s.store.Exec(`DELETE FROM sessions WHERE user_id=?`, adminUserID(t, handler, admin))
		return err
	})
	if w := pausedRequest(t, handler, "POST", "/api/roles", admin, endedSession); w.Code != http.StatusUnauthorized {
		t.Fatalf("a request whose session ended committed: %d %s", w.Code, w.Body.String())
	}
	var roles int
	if err := s.store.QueryRow(`SELECT count(*) FROM roles WHERE name='too late'`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != 0 {
		t.Fatal("a request with an ended session created a role")
	}
}

func adminUserID(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	return readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, cookie)).ID
}

// TestPolicyReportsOwnershipState keeps the client contract honest: the snapshot
// distinguishes "no owner yet" from "ownership was removed", so the setup task
// cannot be used to re-open the bootstrap bridge.
func TestPolicyReportsOwnershipState(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	block := func() map[string]any {
		state := readJSON[map[string]any](t, request(t, handler, "GET", "/api/state", nil, admin))
		return state["authorization"].(map[string]any)
	}
	if block()["owner_designated"] != false || block()["ownership_established"] != false {
		t.Fatalf("a fresh installation reported an ownership ceremony: %v", block())
	}
	enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	if block()["owner_designated"] != true || block()["ownership_established"] != true {
		t.Fatalf("the ceremony did not record ownership: %v", block())
	}
}
