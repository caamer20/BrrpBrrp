package api

import (
	"bytes"
	"context"
	"fmt"
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

// waitForSessionCount polls the session table until an account holds want sessions. It is
// how a test observes that a commit has landed without asserting on wall-clock timing: the
// poll can only succeed once the committing transaction is visible.
func waitForSessionCount(t *testing.T, s *Server, userID string, want int, window time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		var count int
		if err := s.store.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=?`, userID).Scan(&count); err == nil && count == want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// Synthetic hub connections still represent real, current sessions. Keeping
// their identity valid lets the tests isolate role/grant changes from session
// revocation instead of silently depending on an impossible registration.
func activeSessionHash(t *testing.T, s *Server, userID string) string {
	t.Helper()
	var hash string
	if err := s.store.QueryRow(`SELECT token_hash FROM sessions WHERE user_id=? AND expires_at>? LIMIT 1`, userID, db.Now()).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestSessionCommitKeepsTheSurvivingDeviceAdmitted(t *testing.T) {
	s, handler := testServer(t)
	ended := setupAdmin(t, s, handler)
	userID := sessionUserID(t, s, ended)
	fresh := httptest.NewRecorder()
	if err := s.newSession(fresh, userID); err != nil {
		t.Fatal(err)
	}
	survivor := fresh.Result().Cookies()[0]
	clients := []*ws.Client{
		{ID: db.ID(), UserID: userID, SessionHash: hashToken(ended.Value), Out: make(chan ws.Queued, 8)},
		{ID: db.ID(), UserID: userID, SessionHash: hashToken(survivor.Value), Out: make(chan ws.Queued, 8)},
	}
	for _, client := range clients {
		if _, err := s.registerConnection(withSession(t.Context(), client.SessionHash), db.User{ID: userID}, client); err != nil {
			t.Fatal(err)
		}
		defer s.hub.Remove(client)
	}
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := s.store.DeleteOtherSessions(t.Context(), tx, userID, clients[1].SessionHash); err != nil {
		t.Fatal(err)
	}
	if err := s.commitAuthorityChangeFor(t.Context(), tx, "Other devices signed out.", ws.Audience{UserIDs: []string{userID}}); err != nil {
		t.Fatal(err)
	}
	// A newly resolved event addresses the same account on both devices. Only
	// the session that the transaction kept is eligible for new admission.
	revision := s.authorizationRevision(t.Context())
	s.hub.BroadcastToRevision([]string{userID}, revision, "session.test", map[string]string{"value": "current account data"})
	for i, client := range clients {
		admitted := false
		for len(client.Out) > 0 {
			event := <-client.Out
			if bytes.Contains(event.Event, []byte("session.test")) && s.admitted(client, event) {
				admitted = true
			}
		}
		if admitted != (i == 1) {
			t.Errorf("device %d admission=%v; want survivor only", i, admitted)
		}
	}
}

// A completed route guard is not enough for a credential mutation: a request can
// still be reading its body when another device ends its session. Each mutation
// must re-establish that session inside the transaction that changes the account.
func TestSelfServiceSecurityMutationsRejectAnEndedSession(t *testing.T) {
	for _, action := range []string{"password", "mfa/setup", "mfa/enable", "mfa/disable", "reauth"} {
		t.Run(action, func(t *testing.T) {
			s, handler := testServer(t)
			actor := setupAdmin(t, s, handler)
			password := "correct horse battery"
			other := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": password}, nil).Result().Cookies()[0]
			body := map[string]string{"password": password}
			var codes []string
			switch action {
			case "password":
				body = map[string]string{"current_password": password, "new_password": "a forbidden replacement password"}
			case "mfa/enable":
				setup := request(t, handler, "POST", "/api/me/mfa/setup", body, actor)
				if setup.Code != http.StatusOK {
					t.Fatal(setup.Body.String())
				}
				secret := readJSON[map[string]string](t, setup)["secret"]
				body = map[string]string{"code": totpCode(secret, time.Now().Unix()/30)}
			case "mfa/disable", "reauth":
				codes = enrollMFA(t, handler, actor, password)
				// Enrollment ends other devices, so establish the revoking device with
				// its own recovery code before attempting the concurrent operation.
				login := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": password, "code": codes[0]}, nil)
				if login.Code != http.StatusOK || len(login.Result().Cookies()) != 1 {
					t.Fatalf("control sign-in: %d %s", login.Code, login.Body.String())
				}
				other = login.Result().Cookies()[0]
				body["code"] = codes[1]
			}
			var actorSessionID string
			if err := s.store.QueryRow(`SELECT id FROM sessions WHERE token_hash=?`, hashToken(actor.Value)).Scan(&actorSessionID); err != nil {
				t.Fatal(err)
			}
			paused := bodyThatRevokes(t, body, func() error {
				w := request(t, handler, "DELETE", "/api/me/sessions/"+actorSessionID, nil, other)
				if w.Code != http.StatusOK {
					return fmt.Errorf("revoking device: %d %s", w.Code, w.Body.String())
				}
				return nil
			})
			w := pausedRequest(t, handler, "POST", "/api/me/"+action, actor, paused)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("an ended session completed %s: %d %s", action, w.Code, w.Body.String())
			}
			// The authorized device remains active and the denied request must not
			// change the factor, consume its recovery code, or replace the password.
			if w := request(t, handler, "GET", "/api/me", nil, other); w.Code != http.StatusOK {
				t.Errorf("the current device was ended by the refused mutation: %d", w.Code)
			}
			var enabled, pending, remaining int
			if err := s.store.QueryRow(`SELECT (SELECT count(*) FROM user_mfa WHERE secret!=''), (SELECT count(*) FROM user_mfa WHERE pending_secret!=''), (SELECT count(*) FROM recovery_codes)`).Scan(&enabled, &pending, &remaining); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "mfa/setup":
				if pending != 0 {
					t.Error("an ended session created a pending factor")
				}
			case "mfa/enable":
				if enabled != 0 || remaining != 0 {
					t.Error("an ended session enrolled a factor")
				}
			case "mfa/disable", "reauth":
				if enabled != 1 || remaining != 9 {
					t.Errorf("refused mutation changed factor state: enabled=%d recovery codes=%d", enabled, remaining)
				}
			case "password":
				if w := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": password}, nil); w.Code != http.StatusOK {
					t.Error("an ended session replaced the account password")
				}
			}
		})
	}
}

func TestSessionManagementRechecksTheActingSession(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("other_devices=%v", all), func(t *testing.T) {
			s, handler := testServer(t)
			actor := setupAdmin(t, s, handler)
			userID := sessionUserID(t, s, actor)
			fresh := httptest.NewRecorder()
			if err := s.newSession(fresh, userID); err != nil {
				t.Fatal(err)
			}
			survivor := fresh.Result().Cookies()[0]
			var actorID, survivorID string
			if err := s.store.QueryRow(`SELECT id FROM sessions WHERE token_hash=?`, hashToken(actor.Value)).Scan(&actorID); err != nil {
				t.Fatal(err)
			}
			if err := s.store.QueryRow(`SELECT id FROM sessions WHERE token_hash=?`, hashToken(survivor.Value)).Scan(&survivorID); err != nil {
				t.Fatal(err)
			}
			// These DELETE routes have no body to pause. Interpose at the same
			// guard/handler boundary and end the actor through the ordinary route.
			paused := s.protect(routePolicy{}, func(w http.ResponseWriter, r *http.Request, user db.User) {
				ended := request(t, handler, "DELETE", "/api/me/sessions/"+actorID, nil, survivor)
				if ended.Code != http.StatusOK {
					t.Fatalf("revoke actor: %d %s", ended.Code, ended.Body.String())
				}
				r.SetPathValue("id", survivorID)
				if all {
					s.revokeOtherSessions(w, r, user)
				} else {
					s.revokeSession(w, r, user)
				}
			})
			if w := request(t, paused, "DELETE", "/api/me/sessions", nil, actor); w.Code != http.StatusUnauthorized {
				t.Fatalf("ended actor changed other devices: %d %s", w.Code, w.Body.String())
			}
			if w := request(t, handler, "GET", "/api/me", nil, survivor); w.Code != http.StatusOK {
				t.Fatalf("the surviving device lost its session: %d", w.Code)
			}
		})
	}
}

// TestAStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence is the maintained R6-01
// regression. One acquisition order covers commits, standalone invalidation and connection
// admission: the store's connection is taken first and the authority fence is taken second,
// so nothing waits for the connection while holding the fence. Before the fix a standalone
// invalidation took the fence and then read the committed revision through the store: a
// transaction holding the connection (as a committing authorization change does) and the
// invalidation waited on each other in opposite order, and only request cancellation broke
// the cycle.
//
// The hook fires exactly where the revision read is about to happen. The fence must still be
// free there, the concurrent commit must complete without cancellation, and the
// invalidation must then finish. A timeout is a failure bound here, not the proof: the proof
// is that the fence is not held while the connection is waited for.
func TestAStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)

	ctx := context.Background()
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := db.BumpAuthorizationRevision(ctx, tx); err != nil {
		t.Fatal(err)
	}

	reached := make(chan struct{})
	var once sync.Once
	hook := func() { once.Do(func() { close(reached) }) }
	s.standaloneInvalidation.Store(&hook)
	defer s.standaloneInvalidation.Store(nil)

	invalidated := make(chan struct{})
	go func() {
		s.invalidateWorkspace("Channels were reordered.")
		close(invalidated)
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the invalidation never reached its revision read")
	}
	// The invalidation is now waiting for the connection this transaction holds. If it held
	// the fence while it waited, this TryLock would fail and the commit below could not
	// proceed: that is the cycle.
	if !s.authority.TryLock() {
		t.Fatal("a standalone invalidation holds the authority fence while waiting for the store's connection")
	}
	s.authority.Unlock()

	committed := make(chan error, 1)
	go func() { committed <- s.commitAuthorityChange(ctx, tx, "Concurrent authorization change.") }()
	select {
	case err := <-committed:
		if err != nil {
			t.Fatalf("the concurrent commit did not complete: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the concurrent commit waited on a lock cycle instead of completing")
	}
	select {
	case <-invalidated:
	case <-time.After(10 * time.Second):
		t.Fatal("the invalidation did not finish after the commit released the connection")
	}
}

// TestACredentialCommitRefusesAQueuedWriteOnTheRevokedSession is the maintained R6-02
// regression for the operator password reset reviewed as review 6. The reset deleted every
// session of the target and then, after the commit, disconnected the socket and published
// the invalidation. A queued private event could be admitted in that window: the database
// no longer recognized the session, the old cookie already answered 401, and the socket
// still received a newly admitted payload.
//
// The delivery registry is held for the duration of the commit, so the reset's post-commit
// cleanup is still waiting when the writer is released. With the commit and the invalidation
// under one fence, the queued event belongs to the generation the commit replaced and is
// refused. The permitted controls are in the same window: another member of the channel
// receives its event, and the reset still answers success and the new credential signs in.
func TestACredentialCommitRefusesAQueuedWriteOnTheRevokedSession(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	becomeOwner(t, s, handler, admin)
	targetID, targetCookie := memberAccount(t, s, "target")
	bystanderID, bystanderCookie := memberAccount(t, s, "bystander")
	channel := privateTextChannel(t, handler, admin, "credential-gap", targetID)
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
		map[string]any{"visibility": "private", "user_ids": []string{targetID, bystanderID}}, admin); w.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	targetConn := voiceSocket(t, s, server, targetCookie)
	bystanderConn := voiceSocket(t, s, server, bystanderCookie)

	paused, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	hook := func(client *ws.Client, queued ws.Queued) {
		if client.UserID != targetID || !bytes.Contains(queued.Event, []byte("review6.private")) {
			return
		}
		once.Do(func() { close(paused) })
		<-release
	}
	s.writerAdmission.Store(&hook)
	defer s.writerAdmission.Store(nil)

	s.publishToChannel(t.Context(), channel.ID, "review6.private", map[string]string{"secret": "old-session payload"})
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("the target's writer never reached the paused admission decision")
	}
	// The other member of the same restricted channel receives its event while the
	// revoked session's own write is still pending: the change refuses the affected
	// connection only.
	if payload := readEventOfKind(t, bystanderConn, "review6.private", 5*time.Second); payload == nil {
		t.Fatal("a permitted member of the channel did not receive the event")
	}

	// Hold the delivery registry so the reset's post-commit cleanup cannot run. This is
	// where the pre-fix ordering left the window open: the commit had landed, the
	// invalidation was still behind this lock.
	s.deliveries.mu.Lock()
	resetDone := make(chan int, 1)
	go func() {
		resetDone <- performRequest(handler, "POST", "/api/admin/users/"+targetID+"/reset-password",
			map[string]any{"new_password": "a replacement passphrase", "confirm": "target"}, admin).Code
	}()
	committed := waitForSessionCount(t, s, targetID, 0, 10*time.Second)
	if !committed {
		close(release)
		s.deliveries.mu.Unlock()
		t.Fatal("the password reset did not remove the target's sessions")
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, targetCookie); w.Code != http.StatusUnauthorized {
		close(release)
		s.deliveries.mu.Unlock()
		t.Fatalf("the old cookie still reads the channel after the reset: %d %s", w.Code, w.Body.String())
	}
	// The writer resumes while the reset handler is still waiting to disconnect the socket.
	close(release)
	if payload := readEventOfKind(t, targetConn, "review6.private", 500*time.Millisecond); payload != nil {
		s.deliveries.mu.Unlock()
		t.Fatalf("a revoked session was handed a newly admitted private payload: %s", payload)
	}
	s.deliveries.mu.Unlock()
	if code := <-resetDone; code != http.StatusOK {
		t.Fatalf("operator password reset: %d", code)
	}

	// The permitted control: the new credential signs in and reads the channel it may read.
	signedIn := request(t, handler, "POST", "/api/login", map[string]string{"username": "target", "password": "a replacement passphrase"}, nil)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("the reset password does not sign in: %d %s", signedIn.Code, signedIn.Body.String())
	}
	fresh := signedIn.Result().Cookies()[0]
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, fresh); w.Code != http.StatusOK {
		t.Fatalf("the replacement session cannot read the channel: %d %s", w.Code, w.Body.String())
	}
}

// TestASocketRegisteredAfterItsSessionEndedIsRefused is the registration half of R6-02. The
// route guard admits the upgrade request, and a credential change commits between that
// decision and the moment the connection is inserted into the hub. The invalidation that
// accompanied the change could not reach a connection that was not registered yet, so the
// connection would carry authority the server had already withdrawn.
//
// Registration re-establishes the account and its session from the store inside a read
// transaction and inserts the connection under the fence, so the change that landed in
// between is observed and the connection is refused. The control dials with a session that
// did not move and receives the ordinary hello frame.
func TestASocketRegisteredAfterItsSessionEndedIsRefused(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	becomeOwner(t, s, handler, admin)
	targetID, targetCookie := memberAccount(t, s, "target")

	hook := func() {
		w := performRequest(handler, "POST", "/api/admin/users/"+targetID+"/reset-password",
			map[string]any{"new_password": "a replacement passphrase", "confirm": "target"}, admin)
		if w.Code != http.StatusOK {
			t.Errorf("the reset that lands during registration failed: %d %s", w.Code, w.Body.String())
		}
	}
	s.connectionAdmission.Store(&hook)
	defer s.connectionAdmission.Store(nil)

	server := httptest.NewServer(handler)
	defer server.Close()
	dial := func(cookie *http.Cookie) (*websocket.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		headers := http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{s.config.BaseURL}}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
		return conn, err
	}

	refused, err := dial(targetCookie)
	if err == nil {
		defer refused.CloseNow()
		// The upgrade can complete before the server refuses the registration, so the
		// refusal is observed as a connection that is closed instead of admitted.
		quiet, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var event ws.Event
		if readErr := wsjson.Read(quiet, refused, &event); readErr == nil {
			t.Fatalf("a connection whose session had ended was admitted and received %s", event.Type)
		}
	}
	if count := s.hub.Count(); count != 0 {
		t.Fatalf("a session whose credential had changed was registered on the hub: %d connections", count)
	}

	// The permitted control: a session that did not move registers normally.
	allowed, err := dial(admin)
	if err != nil {
		t.Fatalf("a current session could not connect: %v", err)
	}
	defer allowed.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hello ws.Event
	if err := wsjson.Read(ctx, allowed, &hello); err != nil {
		t.Fatalf("a current session received no hello frame: %v", err)
	}
	if hello.Type != "hello" {
		t.Fatalf("a current session received %q instead of hello", hello.Type)
	}
}

// An authorization generation only rejects events queued before invalidation. A
// socket whose session ended must also reject events resolved and queued after
// the commit while disconnect or stream cleanup is still waiting.
func TestEndedSocketsRefuseEventsResolvedAfterTheRevocationCommit(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	becomeOwner(t, s, handler, admin)
	memberID, ended := memberAccount(t, s, "reader")
	bystanderID, survivor := memberAccount(t, s, "bystander")
	channel := privateTextChannel(t, handler, admin, "session-boundary", memberID)
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private", "user_ids": []string{memberID, bystanderID}}, admin); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	endedConn := voiceSocket(t, s, server, ended)
	survivorConn := voiceSocket(t, s, server, survivor)

	// Password reset ends every session. Delay its post-commit stream cleanup,
	// which precedes disconnect, while publishing a newly resolved private event.
	s.deliveries.mu.Lock()
	revoked := make(chan int, 1)
	go func() {
		revoked <- performRequest(handler, "POST", "/api/admin/users/"+memberID+"/reset-password", map[string]string{"new_password": "a replacement passphrase", "confirm": "reader"}, admin).Code
	}()
	if !waitForSessionCount(t, s, memberID, 0, 10*time.Second) {
		s.deliveries.mu.Unlock()
		t.Fatal("session revocation never committed")
	}
	s.publishToChannel(t.Context(), channel.ID, "post-revocation.private", map[string]string{"secret": "created after session revocation"})
	if payload := readEventOfKind(t, survivorConn, "post-revocation.private", 5*time.Second); payload == nil {
		s.deliveries.mu.Unlock()
		t.Fatal("the unaffected reader did not receive the new private event")
	}
	if payload := readEventOfKind(t, endedConn, "post-revocation.private", 500*time.Millisecond); payload != nil {
		s.deliveries.mu.Unlock()
		t.Errorf("a revoked socket received a newly resolved private payload: %s", payload)
		return
	}
	s.deliveries.mu.Unlock()
	if code := <-revoked; code != http.StatusOK {
		t.Fatalf("password reset: %d", code)
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, survivor); w.Code != http.StatusOK {
		t.Fatalf("the surviving device lost access: %d %s", w.Code, w.Body.String())
	}
}
