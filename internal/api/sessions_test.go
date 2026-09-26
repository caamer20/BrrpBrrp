package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// sessionRecord mirrors the client-facing session contract. Token material must
// never appear in it: a session the account owns is still not its credential.
type sessionRecord struct {
	ID                    string `json:"id"`
	UserAgent             string `json:"user_agent"`
	LastSeenAt            string `json:"last_seen_at"`
	Current               bool   `json:"current"`
	RecentlyAuthenticated bool   `json:"recently_authenticated"`
}

func sessionsOf(t *testing.T, handler http.Handler, cookie *http.Cookie) []sessionRecord {
	t.Helper()
	w := request(t, handler, "GET", "/api/me/sessions", nil, cookie)
	if w.Code != 200 {
		t.Fatalf("session list: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("session list exposed token material")
	}
	return readJSON[[]sessionRecord](t, w)
}

// currentSession returns the record the server marks as the caller's own, which
// is how a test maps a cookie onto a revocable session identifier.
func currentSession(t *testing.T, handler http.Handler, cookie *http.Cookie) sessionRecord {
	t.Helper()
	for _, record := range sessionsOf(t, handler, cookie) {
		if record.Current {
			return record
		}
	}
	t.Fatal("the caller's own session was not marked current")
	return sessionRecord{}
}

// staleSession pushes a session's last strong authentication outside the
// privileged window, reproducing a tab left open since yesterday.
func staleSession(t *testing.T, s *Server, cookie *http.Cookie) {
	t.Helper()
	if _, err := s.store.Exec(`UPDATE sessions SET privileged_at=? WHERE token_hash=?`, db.Timestamp(time.Now().Add(-2*recentAuthenticationWindow)), hashToken(cookie.Value)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionListingAndRevocation(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	login := map[string]string{"username": "admin", "password": "correct horse battery"}
	laptop := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	phone := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	if records := sessionsOf(t, handler, admin); len(records) != 3 {
		t.Fatalf("setup plus two sign-ins listed %d sessions, want 3", len(records))
	}
	// Revoking one device signs out exactly that device.
	laptopSession := currentSession(t, handler, laptop)
	if w := request(t, handler, "DELETE", "/api/me/sessions/"+laptopSession.ID, nil, admin); w.Code != 200 {
		t.Fatalf("revoke one: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, laptop); w.Code != 401 {
		t.Fatal("a revoked device kept REST access")
	}
	if w := request(t, handler, "GET", "/api/me", nil, phone); w.Code != 200 {
		t.Fatal("revoking one device signed out another")
	}
	// Another account's session is not addressable at all.
	_, member := ordinaryUser(t, s)
	memberSession := currentSession(t, handler, member)
	if w := request(t, handler, "DELETE", "/api/me/sessions/"+memberSession.ID, nil, admin); w.Code != 404 {
		t.Fatalf("another account's session was addressable: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/me", nil, member); w.Code != 200 {
		t.Fatal("another account's session was revoked")
	}
	// Signing out the other devices keeps the current one.
	revoked := readJSON[struct {
		Revoked int64 `json:"revoked"`
	}](t, request(t, handler, "DELETE", "/api/me/sessions", nil, admin))
	if revoked.Revoked != 1 {
		t.Fatalf("signed out %d other devices, want 1", revoked.Revoked)
	}
	if w := request(t, handler, "GET", "/api/me", nil, phone); w.Code != 401 {
		t.Fatal("sign out other devices left a device signed in")
	}
	if w := request(t, handler, "GET", "/api/me", nil, admin); w.Code != 200 {
		t.Fatal("sign out other devices signed out the caller")
	}
	if records := sessionsOf(t, handler, admin); len(records) != 1 {
		t.Fatalf("after signing out other devices %d sessions remained, want 1", len(records))
	}
}

// TestRecentAuthenticationGatesOperatorActions is T-AUTH-04 at the route level: a
// valid session is not enough to open a full archive or take over an account, and
// the refresh path itself stays usable so a stale tab can recover.
func TestRecentAuthenticationGatesOperatorActions(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	password := "correct horse battery"
	codes := enrollMFA(t, handler, admin, password)
	staleSession(t, s, admin)
	status := readJSON[struct {
		Enabled               bool `json:"enabled"`
		Required              bool `json:"required"`
		RecentlyAuthenticated bool `json:"recently_authenticated"`
	}](t, request(t, handler, "GET", "/api/me/mfa", nil, admin))
	if !status.Enabled || !status.Required || status.RecentlyAuthenticated {
		t.Fatalf("stale operator session reported %+v", status)
	}
	backup := map[string]bool{"include_uploads": false}
	if w := request(t, handler, "POST", "/api/admin/backup", backup, admin); w.Code != 403 || !strings.Contains(w.Body.String(), "Confirm your password") {
		t.Fatalf("stale session archived the server: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/me/reauth", map[string]string{"password": "not the password"}, admin); w.Code != 400 {
		t.Fatalf("reauth accepted a wrong password: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/me/reauth", map[string]string{"password": password}, admin); w.Code != 401 {
		t.Fatalf("reauth skipped the enrolled factor: %d %s", w.Code, w.Body.String())
	}
	refreshed := readJSON[struct {
		RecentlyAuthenticated bool `json:"recently_authenticated"`
	}](t, request(t, handler, "POST", "/api/me/reauth", map[string]string{"password": password, "code": codes[0]}, admin))
	if !refreshed.RecentlyAuthenticated {
		t.Fatal("a confirmed reauthentication did not unlock privileged actions")
	}
	if w := request(t, handler, "POST", "/api/admin/backup", backup, admin); w.Code != 201 {
		t.Fatalf("refreshed operator could not archive: %d %s", w.Code, w.Body.String())
	}
	// The ownership ceremony follows the same rule.
	staleSession(t, s, admin)
	if w := request(t, handler, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 403 {
		t.Fatalf("stale session completed the ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/me/reauth", map[string]string{"password": password, "code": codes[1]}, admin); w.Code != 200 {
		t.Fatalf("recovery code did not refresh the session: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/owner", map[string]string{"username": "admin", "confirm": "admin"}, admin); w.Code != 200 {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	if me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin)); !me.IsOwner {
		t.Fatal("ownership ceremony did not record the owner")
	}
}

// TestPrivilegedActionsRequireAnEnrolledSecondFactor covers D-10: an account that
// can reach every private boundary must enrol a factor first, while ordinary
// members keep working without one.
func TestPrivilegedActionsRequireAnEnrolledSecondFactor(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := ordinaryUser(t, s)
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
			break
		}
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages", messageInput{Content: "no second factor needed", IdempotencyKey: "enrollment-1"}, member); w.Code != 201 {
		t.Fatalf("the enrolment policy blocked an ordinary member: %d %s", w.Code, w.Body.String())
	}
	w := request(t, handler, "POST", "/api/admin/backup", map[string]bool{}, admin)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Enroll two-factor authentication") {
		t.Fatalf("unenrolled operator archived the server: %d %s", w.Code, w.Body.String())
	}
	// The requirement is a setting, so a host that cannot run a second factor can
	// still be administered deliberately rather than by weakening the default.
	s.settingsMu.Lock()
	s.settings.RequirePrivilegedMFA = false
	s.settingsMu.Unlock()
	t.Cleanup(func() {
		s.settingsMu.Lock()
		s.settings.RequirePrivilegedMFA = true
		s.settingsMu.Unlock()
	})
	if w := request(t, handler, "POST", "/api/admin/backup", map[string]bool{}, admin); w.Code != 201 {
		t.Fatalf("policy-off archive: %d %s", w.Code, w.Body.String())
	}
}

func sessionCount(t *testing.T, s *Server, userID string) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=?`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestStaleCredentialsCannotCreateASession covers the sign-in race the password
// re-check inside the session transaction exists for: a credential that changed
// while a sign-in was in flight must never become a session, whether it changed
// by a reset or by enrolling a factor.
func TestStaleCredentialsCannotCreateASession(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enrollMFA(t, handler, admin, "correct horse battery")
	created := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "target", "password": "an original private passphrase", "display_name": "Target"}, admin)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	target := readJSON[db.User](t, created)
	readHash := func() string {
		var hash string
		if err := s.store.QueryRow(`SELECT password_hash FROM users WHERE id=?`, target.ID).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		return hash
	}
	stale := readHash()
	// An operator resets the password while a sign-in is in flight.
	if w := request(t, handler, "POST", "/api/admin/users/"+target.ID+"/reset-password", map[string]string{"new_password": "a completely new passphrase", "confirm": "target"}, admin); w.Code != 200 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	signIn := func(hash, code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if s.completeLogin(w, r, target.ID, hash, code) {
			t.Fatal("a session was created from a credential that had already changed")
		}
		return w
	}
	if w := signIn(stale, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("a replaced password still signed in: %d %s", w.Code, w.Body.String())
	}
	if count := sessionCount(t, s, target.ID); count != 0 {
		t.Fatalf("a stale sign-in left %d sessions", count)
	}
	// A factor enrolled mid-flight cannot be skipped either.
	login := map[string]string{"username": "target", "password": "a completely new passphrase"}
	cookie := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	enrollMFA(t, handler, cookie, login["password"])
	if w := signIn(readHash(), ""); w.Code != 200 || !strings.Contains(w.Body.String(), "second_factor_required") {
		t.Fatalf("a sign-in skipped a factor enrolled while it was in flight: %d %s", w.Code, w.Body.String())
	}
	if count := sessionCount(t, s, target.ID); count != 1 {
		t.Fatalf("%d sessions exist, want only the enrolled session", count)
	}
}

// TestConcurrentRecoveryCodeUseSucceedsOnce is the T-IDENT-01 recovery race: the
// same recovery code presented to simultaneous transactions must consume exactly
// one code.
func TestConcurrentRecoveryCodeUseSucceedsOnce(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	codes := enrollMFA(t, handler, admin, "correct horse battery")
	account := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin))

	const attempts = 8
	results := make([]bool, attempts)
	start := make(chan struct{})
	var waiting sync.WaitGroup
	for i := range results {
		waiting.Add(1)
		go func(slot int) {
			defer waiting.Done()
			<-start
			tx, err := s.store.BeginTx(context.Background(), nil)
			if err != nil {
				return
			}
			defer tx.Rollback()
			used, err := consumeFactor(tx, account.ID, codes[0])
			if err != nil || !used {
				return
			}
			if err := tx.Commit(); err != nil {
				return
			}
			results[slot] = true
		}(i)
	}
	close(start)
	waiting.Wait()
	succeeded := 0
	for _, result := range results {
		if result {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of %d concurrent uses of one recovery code succeeded, want 1", succeeded, attempts)
	}
	var remaining int
	if err := s.store.QueryRow(`SELECT count(*) FROM recovery_codes WHERE user_id=?`, account.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 9 {
		t.Fatalf("%d recovery codes remain, want 9", remaining)
	}
	// The consumed code cannot sign in afterwards.
	login := map[string]string{"username": "admin", "password": "correct horse battery", "code": codes[0]}
	if w := request(t, handler, "POST", "/api/login", login, nil); w.Code != 401 {
		t.Fatalf("a consumed recovery code signed in: %d", w.Code)
	}
}

// TestDemotionKeepsSecondFactorAndDropsAuthority covers the WP-05 completion
// clause: a role change takes effect on the next request without discarding the
// account's own enrollment, and peers cannot strip each other's administrator
// role.
func TestDemotionKeepsSecondFactorAndDropsAuthority(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enrollMFA(t, handler, admin, "correct horse battery")
	created := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "deputy", "password": "a different private passphrase", "display_name": "Deputy", "is_admin": true}, admin)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	deputy := readJSON[db.User](t, created)
	login := map[string]string{"username": "deputy", "password": "a different private passphrase"}
	deputyCookie := request(t, handler, "POST", "/api/login", login, nil).Result().Cookies()[0]
	enrollMFA(t, handler, deputyCookie, login["password"])
	if w := request(t, handler, "GET", "/api/admin/users", nil, deputyCookie); w.Code != 200 {
		t.Fatalf("deputy lacked administrator access: %d %s", w.Code, w.Body.String())
	}
	// A peer administrator cannot change an equal administrator's role.
	roles, err := s.store.Roles(t.Context(), s.store)
	if err != nil {
		t.Fatal(err)
	}
	var memberRoleID string
	for _, role := range roles {
		if role.ID == db.MemberRoleID {
			memberRoleID = role.ID
		}
	}
	if w := request(t, handler, "PUT", "/api/admin/users/"+deputy.ID+"/roles", map[string]any{"role_ids": []string{memberRoleID}}, admin); w.Code != 403 {
		t.Fatalf("a peer administrator re-wrote another administrator's roles: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/admin/users/"+deputy.ID, map[string]any{"is_admin": false, "confirm": "deputy"}, admin); w.Code != 200 {
		t.Fatalf("demotion: %d %s", w.Code, w.Body.String())
	}
	// Authority is re-resolved on every request, so the existing session cannot
	// keep exercising administration.
	if w := request(t, handler, "GET", "/api/admin/users", nil, deputyCookie); w.Code != 403 {
		t.Fatalf("a demoted session kept administrator access: %d %s", w.Code, w.Body.String())
	}
	// The second factor survives the demotion.
	challenge := request(t, handler, "POST", "/api/login", login, nil)
	if challenge.Code != 200 || len(challenge.Result().Cookies()) != 0 {
		t.Fatalf("a demoted account signed in on its password alone: %d %s", challenge.Code, challenge.Body.String())
	}
	if !readJSON[struct {
		SecondFactor bool `json:"second_factor_required"`
	}](t, challenge).SecondFactor {
		t.Fatal("demotion discarded the account's second factor")
	}
}

// TestRevokingOneSessionClosesOnlyItsSockets proves the revocation reaches the
// live connection at the moment it commits, not at the revoked client's next
// request.
func TestRevokingOneSessionClosesOnlyItsSockets(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	other := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": "correct horse battery"}, nil).Result().Cookies()[0]
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func(cookie *http.Cookie) *websocket.Conn {
		headers := http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{s.config.BaseURL}}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		readEvent(t, ctx, conn, "hello")
		return conn
	}
	adminConn, otherConn := connect(admin), connect(other)
	if w := request(t, handler, "DELETE", "/api/me/sessions/"+currentSession(t, handler, other).ID, nil, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, otherConn, &event); err != nil {
			if ctx.Err() != nil {
				t.Fatal("revocation did not close the revoked session's socket")
			}
			break
		}
	}
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
			break
		}
	}
	if err := wsjson.Write(ctx, adminConn, map[string]any{"t": "message.send", "d": map[string]string{"channel_id": channelID, "content": "still here", "idempotency_key": "session-revoke-1"}}); err != nil {
		t.Fatalf("the surviving session's socket was closed: %v", err)
	}
	readEvent(t, ctx, adminConn, "ack")
}
