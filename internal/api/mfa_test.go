package api

import (
	"encoding/base32"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// enrollMFA completes the member enrollment flow on a session and returns the
// recovery codes. Any account may enroll itself; no administrator is involved.
func enrollMFA(t *testing.T, handler http.Handler, cookie *http.Cookie, password string) []string {
	t.Helper()
	w := request(t, handler, "POST", "/api/me/mfa/setup", map[string]string{"password": password}, cookie)
	if w.Code != 200 {
		t.Fatalf("mfa setup: %d %s", w.Code, w.Body.String())
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	json.Unmarshal(w.Body.Bytes(), &setup)
	w = request(t, handler, "POST", "/api/me/mfa/enable", map[string]string{"code": totpCode(setup.Secret, time.Now().Unix()/30)}, cookie)
	if w.Code != 200 {
		t.Fatalf("mfa enable: %d %s", w.Code, w.Body.String())
	}
	var enabled struct {
		Codes []string `json:"recovery_codes"`
	}
	json.Unmarshal(w.Body.Bytes(), &enabled)
	if len(enabled.Codes) != 10 {
		t.Fatalf("enrollment returned %d recovery codes, want 10", len(enabled.Codes))
	}
	return enabled.Codes
}

func TestTOTPVectorsAndReplay(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	// Six-digit truncation of the RFC 6238 SHA-1 test vectors.
	for _, test := range []struct {
		at   int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if got := totpCode(secret, test.at/30); got != test.code {
			t.Fatalf("%d: got %s, want %s", test.at, got, test.code)
		}
		counter, ok := matchTOTP(secret, test.code, time.Unix(test.at, 0), -1)
		if !ok {
			t.Fatal("valid vector rejected")
		}
		if _, ok := matchTOTP(secret, test.code, time.Unix(test.at, 0), counter); ok {
			t.Fatal("accepted a reused counter")
		}
	}
	if _, ok := matchTOTP("", "", time.Now(), -1); ok {
		t.Fatal("accepted empty factor")
	}
}

// TestOperatorRemovesALostSecondFactor covers the audited emergency path for an
// account whose authenticator and recovery codes are both gone. A password reset
// deliberately keeps the factor, so without this route the account could never
// sign in again and the only answer left would be to abandon it.
func TestOperatorRemovesALostSecondFactor(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	recovery := enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	created := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "lost_phone", "password": "a private member passphrase", "display_name": "Lost phone"}, admin)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	member := readJSON[db.User](t, created)
	login := map[string]string{"username": member.Username, "password": "a private member passphrase"}
	memberCookie := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	enrollMFA(t, handler, memberCookie, login["password"])
	path := "/api/admin/users/" + member.ID + "/mfa"

	// Ordinary moderation cannot reach a credential, even its own.
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": member.Username}, memberCookie); w.Code != 403 {
		t.Fatalf("a member removed a second factor: %d %s", w.Code, w.Body.String())
	}
	// An owner recovers its own factor on the host, not from a panel another
	// operator could reach.
	me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin))
	if w := request(t, handler, "DELETE", "/api/admin/users/"+me.ID+"/mfa", map[string]string{"confirm": me.Username}, admin); w.Code != 403 {
		t.Fatalf("an owner's factor was removable from the panel: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": "wrong"}, admin); w.Code != 400 {
		t.Fatalf("unconfirmed removal: %d", w.Code)
	}
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": member.Username}, admin); w.Code != 200 {
		t.Fatalf("removal: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, memberCookie); w.Code != 401 {
		t.Fatal("the recovered account kept its sessions")
	}
	// The account can sign in with its password alone, which is the point: it can
	// then enrol a new factor.
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("the recovered account could not sign in: %d %s", w.Code, w.Body.String())
	}
	// Removing an absent factor is reported, not refused, so a retried recovery
	// cannot look like a failure.
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": member.Username}, admin); w.Code != 200 {
		t.Fatalf("second removal: %d %s", w.Code, w.Body.String())
	}
	// The operator's own factor is untouched, and the event is attributed to it.
	status := readJSON[map[string]any](t, request(t, handler, "GET", "/api/me/mfa", nil, admin))
	if status["enabled"] != true {
		t.Fatalf("removal took the operator's own factor: %v", status)
	}
	entries := readJSON[[]map[string]string](t, request(t, handler, "GET", "/api/admin/audit-logs", nil, admin))
	removed := false
	for _, entry := range entries {
		if entry["action"] == "user.mfa_removed" {
			removed = entry["actor"] == "admin" && entry["target_id"] == member.ID
		}
	}
	if !removed {
		t.Fatalf("the removal was not recorded against its actor: %v", entries)
	}
	// A stale operator session is refused before the handler runs, exactly like a
	// chosen-password reset.
	staleSession(t, s, admin)
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": member.Username}, admin); w.Code != 403 {
		t.Fatalf("a stale session removed a second factor: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/me/reauth", map[string]string{"password": "correct horse battery", "code": recovery[0]}, admin); w.Code != 200 {
		t.Fatalf("reauthentication: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "DELETE", path, map[string]string{"confirm": member.Username}, admin); w.Code != 200 {
		t.Fatalf("confirmed operator was still refused: %d %s", w.Code, w.Body.String())
	}
}

func TestMFAEnrollmentRecoveryAndSessionRevocation(t *testing.T) {
	s, handler := testServer(t)
	cookie := setupAdmin(t, s, handler)
	login := map[string]string{"username": "admin", "password": "correct horse battery"}
	other := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	w := request(t, handler, "POST", "/api/me/mfa/setup", map[string]string{"password": login["password"]}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var setup struct {
		Secret string `json:"secret"`
	}
	json.Unmarshal(w.Body.Bytes(), &setup)
	code := totpCode(setup.Secret, time.Now().Unix()/30)
	w = request(t, handler, "POST", "/api/me/mfa/enable", map[string]string{"code": code}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var enabled struct {
		Codes []string `json:"recovery_codes"`
	}
	json.Unmarshal(w.Body.Bytes(), &enabled)
	if len(enabled.Codes) != 10 {
		t.Fatal("missing recovery codes")
	}
	if w := request(t, handler, "GET", "/api/me", nil, other); w.Code != 401 {
		t.Fatal("other session survived MFA enrollment")
	}
	if w := request(t, handler, "GET", "/api/me", nil, cookie); w.Code != 200 {
		t.Fatal("enrolling session was removed")
	}
	w = request(t, handler, "POST", "/api/login", login, nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatal("password alone issued a session")
	}
	var challenge map[string]bool
	json.Unmarshal(w.Body.Bytes(), &challenge)
	if !challenge["second_factor_required"] {
		t.Fatal("missing challenge")
	}
	login["code"] = code
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 401 {
		t.Fatal("enrollment code replay succeeded")
	}
	login["code"] = enabled.Codes[0]
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("recovery sign-in: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 401 {
		t.Fatal("recovery code used twice")
	}
	w = request(t, handler, "POST", "/api/me/mfa/disable", map[string]string{"password": login["password"], "code": enabled.Codes[1]}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	delete(login, "code")
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("MFA did not disable")
	}
}
