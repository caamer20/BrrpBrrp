package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	c, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Prepare(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(c.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	s, err := New(c, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, s.Handler()
}

func request(t *testing.T, handler http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://localhost:8080")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func setupAdmin(t *testing.T, s *Server, handler http.Handler) *http.Cookie {
	t.Helper()
	token, err := os.ReadFile(s.config.SetupTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, handler, "POST", "/api/setup", map[string]string{"token": string(token), "server_name": "Test server", "username": "admin", "password": "correct horse battery"}, nil)
	if w.Code != 201 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestBootstrapLoginAndLogout(t *testing.T) {
	s, handler := testServer(t)
	bad := map[string]string{"token": "wrong", "server_name": "Test", "username": "admin", "password": "correct horse battery"}
	if w := request(t, handler, "POST", "/api/setup", bad, nil); w.Code != 403 {
		t.Fatalf("unprotected setup: %d", w.Code)
	}
	cookie := setupAdmin(t, s, handler)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	if _, err := os.Stat(s.config.SetupTokenFile); !os.IsNotExist(err) {
		t.Fatal("setup token still exists")
	}
	if w := request(t, handler, "POST", "/api/setup", bad, nil); w.Code != 409 {
		t.Fatal("setup not closed")
	}
	if w := request(t, handler, "GET", "/api/me", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/logout", map[string]any{}, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, cookie); w.Code != 401 {
		t.Fatal("logout did not revoke session")
	}
	login := map[string]string{"username": "ADMIN", "password": "correct horse battery"}
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := s.store.Exec(`UPDATE users SET is_banned=1`); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 401 {
		t.Fatal("banned login accepted")
	}
	var tokenHash string
	if err := s.store.QueryRow(`SELECT token_hash FROM sessions LIMIT 1`).Scan(&tokenHash); err != nil {
		t.Fatal(err)
	}
	if tokenHash == cookie.Value || len(tokenHash) != 64 {
		t.Fatal("session token stored unprotected")
	}
}

// TestTheSignInBudgetIsTheConfiguredOne pins the address-wide sign-in budget to the
// configured value. It exists because the budget is shared by every account behind one
// address, so an operator who raises it for an office, a VPN or a reverse proxy has to
// actually get the larger budget; and because a Server whose Config carries no value must
// fall back to the shipped default rather than to an unlimited budget.
func TestTheSignInBudgetIsTheConfiguredOne(t *testing.T) {
	t.Setenv("BRRP_LOGIN_ATTEMPTS", "3")
	s, handler := testServer(t)
	if s.config.LoginAttempts != 3 {
		t.Fatalf("configured budget: %d", s.config.LoginAttempts)
	}
	// Setup is an attempt too, which is why the budget here is three.
	setupAdmin(t, s, handler)
	account := map[string]string{"username": "admin", "password": "correct horse battery"}
	for attempt := 2; attempt <= 3; attempt++ {
		if w := request(t, handler, "POST", "/api/login", account, nil); w.Code != 200 {
			t.Fatalf("sign-in %d of a budget of three: %d %s", attempt, w.Code, w.Body.String())
		}
	}
	w := request(t, handler, "POST", "/api/login", account, nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("sign-in over the budget: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	// A zero value is what a Config built in a test looks like; it must mean the default.
	s.config.LoginAttempts = 0
	fresh := &Server{config: s.config, limiter: newRateLimiter()}
	if !fresh.limitLogin(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/login", nil)) {
		t.Fatal("a zero budget must mean the default, not no attempts")
	}
	for attempt := 1; attempt < app.LoginAttemptsDefault; attempt++ {
		if !fresh.limitLogin(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/login", nil)) {
			t.Fatalf("attempt %d of the default budget was refused", attempt)
		}
	}
	if fresh.limitLogin(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/login", nil)) {
		t.Fatalf("attempt %d past the default budget was allowed", app.LoginAttemptsDefault+1)
	}
}

func TestCrossOriginAndPasswordChange(t *testing.T) {
	s, handler := testServer(t)
	cookie := setupAdmin(t, s, handler)
	r := httptest.NewRequest("POST", "/api/logout", bytes.NewBufferString(`{}`))
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	change := map[string]string{"current_password": "correct horse battery", "new_password": "another secure password"}
	if w := request(t, handler, "POST", "/api/me/password", change, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, cookie); w.Code != 401 {
		t.Fatal("password change did not revoke sessions")
	}
}
