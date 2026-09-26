package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

func ownersIn(t *testing.T, s *Server) int {
	t.Helper()
	var owners int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE is_owner=1`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	return owners
}

func accountExists(t *testing.T, s *Server, id string) bool {
	t.Helper()
	var exists bool
	if err := s.store.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=?)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

// becomeOwner equips an administrator to act with installation authority: it
// enrolls a factor and completes the ownership ceremony, which is what operator
// actions require.
func becomeOwner(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	enrollMFA(t, handler, cookie, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, cookie); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	owner := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, cookie))
	if !owner.IsOwner {
		t.Fatal("the ceremony did not make the caller the owner")
	}
	return owner.ID
}

// administratorAccount creates a second administrator that a transfer can move
// ownership to.
func administratorAccount(t *testing.T, handler http.Handler, actor *http.Cookie, username string) (string, *http.Cookie) {
	t.Helper()
	w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": username, "password": "a private initial passphrase", "is_admin": true}, actor)
	if w.Code != http.StatusCreated {
		t.Fatalf("create administrator: %d %s", w.Code, w.Body.String())
	}
	created := readJSON[db.User](t, w)
	signedIn := request(t, handler, "POST", "/api/login", map[string]string{"username": username, "password": "a private initial passphrase"}, nil)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("sign in as %s: %d %s", username, signedIn.Code, signedIn.Body.String())
	}
	return created.ID, signedIn.Result().Cookies()[0]
}

// TestAPendingDeleteCannotRemoveTheNewOwner is the permanent regression for the
// reproduced half of R2-01. An account change loads its target before it enters
// the transaction that commits it, so a transfer completed in between used to let
// the change land on the account that had just become the owner: the probe deleted
// the designated owner and left the installation without one.
//
// The body of the request performs the transfer, so the change lands exactly where
// the route read and the commit disagreed, with no sleep and no race.
func TestAPendingDeleteCannotRemoveTheNewOwner(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	ownerID := becomeOwner(t, s, handler, admin)
	successorID, successor := administratorAccount(t, handler, admin, "successor")

	transfer := func() error {
		w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "successor", "confirm": "successor"}, admin)
		if w.Code != http.StatusOK {
			return fmt.Errorf("transfer ownership: %d %s", w.Code, w.Body.String())
		}
		return nil
	}
	del := bodyThatRevokes(t, map[string]any{"confirm": "successor"}, transfer)
	deleted := pausedRequest(t, handler, http.MethodDelete, "/api/admin/users/"+successorID, admin, del)
	if deleted.Code == http.StatusOK {
		t.Fatalf("a pending delete removed the new owner: %d %s", deleted.Code, deleted.Body.String())
	}
	if ownersIn(t, s) != 1 {
		t.Fatal("the transfer did not leave exactly one owner")
	}
	if !accountExists(t, s, successorID) {
		t.Fatal("a pending delete removed the account that had become the owner")
	}
	var deletedAudits int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.deleted' AND target_id=?`, successorID).Scan(&deletedAudits); err != nil {
		t.Fatal(err)
	}
	if deletedAudits != 0 {
		t.Fatal("a refused delete left its audit record behind")
	}

	// A ban in the same window is refused too, and the owner is untouched.
	ban := bodyThatRevokes(t, map[string]any{"is_banned": true, "confirm": "successor"}, func() error {
		if _, err := s.store.Exec(`UPDATE users SET is_owner=0 WHERE id=?`, ownerID); err != nil {
			return err
		}
		_, err := s.store.Exec(`UPDATE users SET is_owner=1 WHERE id=?`, successorID)
		return err
	})
	if w := pausedRequest(t, handler, http.MethodPatch, "/api/admin/users/"+successorID, admin, ban); w.Code == http.StatusOK {
		t.Fatalf("a pending ban applied to an account that had just become the owner: %d %s", w.Code, w.Body.String())
	}
	var banned int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE id=? AND is_banned=1`, successorID).Scan(&banned); err != nil {
		t.Fatal(err)
	}
	if banned != 0 {
		t.Fatal("a pending account change banned the owner")
	}
	if ownersIn(t, s) != 1 {
		t.Fatal("a pending account change removed the designated owner")
	}

	// The same requests issued after the transfer are refused as well, so the
	// refusal is not an artefact of the paused body. The acting account is a plain
	// administrator now, so none of these are its authority to exercise.
	for _, attempt := range []struct {
		name, method string
		body         any
	}{
		{"delete", http.MethodDelete, map[string]any{"confirm": "successor"}},
		{"ban", http.MethodPatch, map[string]any{"is_banned": true, "confirm": "successor"}},
		{"reset the owner's password", http.MethodPost, map[string]any{"new_password": "a replacement passphrase", "confirm": "successor"}},
	} {
		path := "/api/admin/users/" + successorID
		if attempt.name == "reset the owner's password" {
			path += "/reset-password"
		}
		if w := request(t, handler, attempt.method, path, attempt.body, admin); w.Code == http.StatusOK {
			t.Fatalf("%s of the owner was allowed: %d %s", attempt.name, w.Code, w.Body.String())
		}
	}
	if !accountExists(t, s, successorID) || ownersIn(t, s) != 1 {
		t.Fatal("a refused account change removed the owner")
	}

	// The permitted control: the owner still manages ordinary members.
	memberID, _ := memberAccount(t, s, "ordinary")
	if w := request(t, handler, http.MethodDelete, "/api/admin/users/"+memberID, map[string]any{"confirm": "ordinary"}, successor); w.Code != http.StatusOK {
		t.Fatalf("the owner could not delete an ordinary member: %d %s", w.Code, w.Body.String())
	}
	if ownersIn(t, s) != 1 {
		t.Fatal("the installation lost its owner while performing permitted work")
	}
	_ = successor
}

// TestAnAccountRaisedOutOfReachCannotBeChanged is the rank half of R2-01, and the
// case the earlier F-01 work did not cover: the acting account keeps its authority,
// but the target is promoted while the request is in progress. The delegated
// manager's pending ban used to land on an account that had outgrown it.
func TestAnAccountRaisedOutOfReachCannotBeChanged(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, _ := memberAccount(t, s, "ordinary")
	bystanderID, _ := memberAccount(t, s, "bystander")
	_, manager, _ := delegatedManager(t, s, "manager")

	promote := func() error {
		w := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{"is_admin": true, "confirm": "ordinary"}, admin)
		if w.Code != http.StatusOK {
			return fmt.Errorf("promote: %d %s", w.Code, w.Body.String())
		}
		return nil
	}
	ban := bodyThatRevokes(t, map[string]any{"is_banned": true, "confirm": "ordinary"}, promote)
	if w := pausedRequest(t, handler, http.MethodPatch, "/api/admin/users/"+memberID, manager, ban); w.Code == http.StatusOK {
		t.Fatalf("a delegated manager banned an account that had become an administrator: %d %s", w.Code, w.Body.String())
	}
	var banned int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE id=? AND is_banned=1`, memberID).Scan(&banned); err != nil {
		t.Fatal(err)
	}
	if banned != 0 {
		t.Fatal("a pending ban applied to an account that had been promoted")
	}

	// The promoted account is out of the manager's reach from the next request on,
	// and its permitted work is unaffected: the same manager still bans an
	// ordinary member.
	if w := request(t, handler, http.MethodPost, "/api/admin/users/"+memberID+"/kick", map[string]any{"confirm": "ordinary"}, manager); w.Code != http.StatusForbidden {
		t.Fatalf("a delegated manager acted on an administrator: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, http.MethodPatch, "/api/admin/users/"+bystanderID, map[string]any{"is_banned": true, "confirm": "bystander"}, manager); w.Code != http.StatusOK {
		t.Fatalf("a delegated manager could not ban an ordinary member: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, http.MethodPatch, "/api/admin/users/"+memberID, map[string]any{"display_name": "Renamed", "confirm": "ordinary"}, manager); w.Code != http.StatusForbidden {
		t.Fatalf("a delegated manager renamed an administrator: %d %s", w.Code, w.Body.String())
	}
}

// settingsChange sends one settings update with its own deadline, so the request
// that used to wait on its own transaction fails quickly instead of holding the
// test suite open.
func settingsChange(t *testing.T, handler http.Handler, cookie *http.Cookie, settings db.Settings, timeout time.Duration) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"settings": settings})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(w, r)
	return w, time.Since(started)
}

// TestAnOwnerSecurityPolicyChangeDoesNotWaitOnItsOwnTransaction is the permanent
// regression for R2-02. The store holds a single connection; the settings handler
// began a transaction and then asked the store for the caller's session freshness
// and factor enrollment, so the check waited for the connection its own caller was
// holding. A legitimate owner request eventually failed with a server error after
// the request context expired.
//
// The same change must now finish promptly, commit its record, and still refuse a
// stale session or an account without installation authority.
func TestAnOwnerSecurityPolicyChangeDoesNotWaitOnItsOwnTransaction(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	becomeOwner(t, s, handler, admin)

	before := s.Settings().RequirePrivilegedMFA
	flip := func(cookie *http.Cookie, timeout time.Duration) (*httptest.ResponseRecorder, time.Duration) {
		settings := s.Settings()
		settings.RequirePrivilegedMFA = !before
		return settingsChange(t, handler, cookie, settings, timeout)
	}

	// A routine settings manager that is not the owner cannot move the policy.
	managerID, manager := memberAccount(t, s, "manager")
	if _, err := s.store.Exec(`INSERT INTO roles(id,role_key,name,position,is_default,is_system,permissions,created_at,updated_at) VALUES('role-settings','settings-manager','Settings manager',50,0,0,?,?,?)`,
		`["channel.read","message.send","settings.manage"]`, db.Now(), db.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, managerID, "role-settings", db.Now()); err != nil {
		t.Fatal(err)
	}
	if w, _ := flip(manager, 5*time.Second); w.Code != http.StatusForbidden {
		t.Fatalf("a non-owner settings manager changed the security policy: %d %s", w.Code, w.Body.String())
	}
	if s.Settings().RequirePrivilegedMFA != before {
		t.Fatal("a refused request changed the policy")
	}

	// A stale session cannot change it either.
	staleSession(t, s, admin)
	if w, _ := flip(admin, 5*time.Second); w.Code != http.StatusForbidden {
		t.Fatalf("a stale session changed the security policy: %d %s", w.Code, w.Body.String())
	}
	if s.Settings().RequirePrivilegedMFA != before {
		t.Fatal("a stale session changed the policy")
	}

	// The owner's own strong sign-in is accepted, and the request finishes while
	// its transaction is open rather than waiting for a second connection.
	if _, err := s.store.Exec(`UPDATE sessions SET privileged_at=? WHERE token_hash=?`, db.Timestamp(time.Now()), hashToken(admin.Value)); err != nil {
		t.Fatal(err)
	}
	done, elapsed := flip(admin, 5*time.Second)
	if done.Code != http.StatusOK {
		t.Fatalf("the owner's security policy change failed: %d %s", done.Code, done.Body.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("the security policy change took %s, which means it waited on its own transaction", elapsed)
	}
	if s.Settings().RequirePrivilegedMFA == before {
		t.Fatal("the accepted request did not change the policy")
	}
	var audits int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='settings.security_policy_changed'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("security policy audit records = %d, want 1", audits)
	}
}

// TestCredentialTransitionsCommitWithTheirAuditRecords covers the audit half of
// R2-03: a successful credential change used to leave no record at all, because
// the audit was written after the transaction through a warning-only path.
//
// The record is now part of the transaction, so an unwritable audit log means the
// change does not commit. The forced failure proves that, rather than assuming it.
func TestCredentialTransitionsCommitWithTheirAuditRecords(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	count := func(action string) int {
		t.Helper()
		var audits int
		if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action=?`, action).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		return audits
	}

	if w := request(t, handler, "POST", "/api/me/password", map[string]string{"current_password": "correct horse battery", "new_password": "a different long passphrase"}, admin); w.Code != http.StatusOK {
		t.Fatalf("password change: %d %s", w.Code, w.Body.String())
	}
	if count("user.password_changed") != 1 {
		t.Fatal("a successful password change left no audit record")
	}

	// Changing a password ends every session, so the rest of the test signs in
	// again with the new credential. Enrolling and removing a factor are credential
	// transitions too.
	fresh := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": "a different long passphrase"}, nil)
	if fresh.Code != http.StatusOK {
		t.Fatalf("sign in with the new password: %d %s", fresh.Code, fresh.Body.String())
	}
	admin = fresh.Result().Cookies()[0]
	codes := enrollMFA(t, handler, admin, "a different long passphrase")
	if count("mfa.enabled") != 1 {
		t.Fatal("enrolling a second factor left no audit record")
	}
	if w := request(t, handler, "POST", "/api/me/mfa/disable", map[string]any{"password": "a different long passphrase", "code": codes[0]}, admin); w.Code != http.StatusOK {
		t.Fatalf("mfa disable: %d %s", w.Code, w.Body.String())
	}
	if count("mfa.disabled") != 1 {
		t.Fatal("removing a second factor left no audit record")
	}

	// With the audit log unable to accept the record, the credential change must
	// not commit either.
	if _, err := s.store.Exec(`CREATE TRIGGER refuse_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	w := request(t, handler, "POST", "/api/me/password", map[string]string{"current_password": "a different long passphrase", "new_password": "yet another long passphrase"}, admin)
	if w.Code == http.StatusOK {
		t.Fatalf("a password change committed without its required audit record: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.store.Exec(`DROP TRIGGER refuse_audit`); err != nil {
		t.Fatal(err)
	}
	if count("user.password_changed") != 1 {
		t.Fatal("the refused password change still wrote its audit record")
	}
	// The old credential is unchanged: the mutation rolled back with its record.
	signedIn := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": "a different long passphrase"}, nil)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("the refused change altered the password: %d %s", signedIn.Code, signedIn.Body.String())
	}
	if w := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": "yet another long passphrase"}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("the refused change applied the new password")
	}
}
