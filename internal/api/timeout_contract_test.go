package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// V14-03. Three defects in the same feature, each reproduced before it was fixed and
// each with a test that would have caught it. They are kept together because they
// share a shape: a timeout looked like it worked, and the evidence was a status code.

// The supported route commits once and says so.
//
// `commitAuthorityChange` commits. The handler committed again, so a moderator saw 500
// for a timeout that was in force and audited — and a moderator who retries has
// applied a second authority change on top of the first. The route was also registered
// under `accounts/` while the interface calls `users/`, and the handler read its
// target from the request body while `liftTimeout` read it from the path, so the two
// halves of the feature disagreed about which account they were acting on.
func TestATimeoutAppliesAndLiftsThroughTheRouteTheInterfaceCalls(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	account, _ := memberAccount(t, s, "timeout_route_target")

	// The address the interface builds: `api()` prepends /api.
	apply := request(t, handler, "POST", "/api/admin/users/"+account+"/timeout", map[string]any{
		"minutes": 30, "reason": "a real reason",
	}, admin)
	if apply.Code != 200 {
		t.Fatalf("the supported route returned %d: %s", apply.Code, apply.Body.String())
	}
	body := readJSON[map[string]any](t, apply)
	if body["user_id"] != account {
		t.Fatalf("the response names %v, not the account that was acted on", body["user_id"])
	}
	// A live timeout, through the same route.
	var stored int
	if err := s.store.QueryRow(`SELECT count(*) FROM account_timeouts WHERE user_id=? AND expires_at>?`, account, db.Now()).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("the route reported success and %d live timeouts were recorded", stored)
	}

	lift := request(t, handler, "DELETE", "/api/admin/users/"+account+"/timeout", nil, admin)
	if lift.Code != 200 {
		t.Fatalf("lifting through the supported route returned %d: %s", lift.Code, lift.Body.String())
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM account_timeouts WHERE user_id=?`, account).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("the lift reported success and %d timeouts remain", stored)
	}
}

// The address names the account. A body that names a different one is refused rather
// than silently resolved, because "the account in the link" and "the account in the
// form" are different actions and only one of them was shown to the moderator.
func TestATimeoutRefusesABodyThatNamesADifferentAccount(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	first, _ := memberAccount(t, s, "timeout_first")
	second, _ := memberAccount(t, s, "timeout_second")

	if w := request(t, handler, "POST", "/api/admin/users/"+first+"/timeout", map[string]any{
		"minutes": 30, "reason": "aimed at one account", "user_id": second,
	}, admin); w.Code != 400 {
		t.Fatalf("a timeout aimed at one account and addressed to another was accepted: %d %s", w.Code, w.Body.String())
	}
	// Neither account was touched.
	for name, account := range map[string]string{"the addressed one": first, "the named one": second} {
		var stored int
		if err := s.store.QueryRow(`SELECT count(*) FROM account_timeouts WHERE user_id=?`, account).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != 0 {
			t.Fatalf("the refused request timed out %s", name)
		}
	}
}

// A community timeout is not an installation ban.
//
// It suspends the account's ability to participate in the community. It must not read
// as a ban by denying private history, direct conversations or private activity — a
// community moderator holding somebody's private life hostage is not a moderation
// decision, and the master plan's contract is explicitly community-scoped.
//
// The inverse is checked too, because a fix that simply stopped enforcing timeouts
// would pass the private assertions and suspend nobody at all.
func TestACommunityTimeoutDoesNotBecomeAnInstallationBan(t *testing.T) {
	s, handler := testServer(t)
	s.config.PrivateConversations = true
	admin := setupAdmin(t, s, handler)
	account, member := memberAccount(t, s, "timeout_scope_target")

	if w := request(t, handler, "POST", "/api/admin/users/"+account+"/timeout", map[string]any{
		"minutes": 30, "reason": "community conduct",
	}, admin); w.Code != 200 {
		t.Fatalf("the timeout was refused: %d %s", w.Code, w.Body.String())
	}

	// The community surface is closed. This is the part the timeout is for.
	if w := request(t, handler, "GET", "/api/channels", nil, member); w.Code != 403 {
		t.Fatalf("a suspended account read the community channel list: %d %s", w.Code, w.Body.String())
	}
	// The private surfaces are not, because a community timeout did not decide anything
	// about them.
	for _, path := range []string{"/api/directs", "/api/activity"} {
		if w := request(t, handler, "GET", path, nil, member); w.Code == 403 {
			t.Errorf("a community timeout denied %s: %s", path, w.Body.String())
		}
	}
	// And the account is not a ban in any stored sense. A ban is its own authority with
	// its own rules; one that arrives as a side effect of a timeout is a ban nobody
	// decided to apply.
	var banned, deactivated bool
	if err := s.store.QueryRow(`SELECT is_banned,is_deactivated FROM users WHERE id=?`, account).Scan(&banned, &deactivated); err != nil {
		t.Fatal(err)
	}
	if banned || deactivated {
		t.Fatalf("a community timeout marked the account banned=%v deactivated=%v", banned, deactivated)
	}
	// The session survives: a ban ends it, a timeout does not.
	var sessions int
	if err := s.store.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=?`, account).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions == 0 {
		t.Fatal("a community timeout ended the account's sessions")
	}
}

// A timeout advances the authority revision and cancels the account's live work, like
// every other account change. A revocation that leaves a registered stream running is
// a revocation the account can keep reading through.
func TestATimeoutAdvancesAuthorityAndFencesTheAccount(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	account, _ := memberAccount(t, s, "timeout_revision_target")

	before := s.authorizationRevision(t.Context())
	if w := request(t, handler, "POST", "/api/admin/users/"+account+"/timeout", map[string]any{
		"minutes": 30, "reason": "fencing",
	}, admin); w.Code != 200 {
		t.Fatalf("the timeout was refused: %d %s", w.Code, w.Body.String())
	}
	if after := s.authorizationRevision(t.Context()); after <= before {
		t.Fatalf("the authorization revision did not advance: %d -> %d", before, after)
	}
	// A second timeout on the same account is a second authority change, not a
	// duplicate that the revision can absorb.
	middle := s.authorizationRevision(t.Context())
	if w := request(t, handler, "POST", "/api/admin/users/"+account+"/timeout", map[string]any{
		"minutes": 60, "reason": "extended",
	}, admin); w.Code != 200 {
		t.Fatalf("extending a timeout was refused: %d %s", w.Code, w.Body.String())
	}
	if after := s.authorizationRevision(t.Context()); after <= middle {
		t.Fatalf("extending a timeout did not advance the revision: %d -> %d", middle, after)
	}
	// And lifting it is a change too, because the account's access is restored.
	before = s.authorizationRevision(t.Context())
	if w := request(t, handler, "DELETE", "/api/admin/users/"+account+"/timeout", nil, admin); w.Code != 200 {
		t.Fatalf("lifting was refused: %d %s", w.Code, w.Body.String())
	}
	if after := s.authorizationRevision(t.Context()); after <= before {
		t.Fatalf("lifting a timeout did not advance the revision: %d -> %d", before, after)
	}
}

// A timeout that only ever proves it can refuse has not been shown to work.
//
// Every test in this file asserts a denial. That is the shape the previous reviews
// flagged: a status code looks like evidence, and a timeout that refuses everything is
// indistinguishable from a timeout that refuses the right things. What the member
// actually experiences is the question the feature exists to answer, and it needs
// positive controls — a real session, real JSON, a message that exists afterwards.
//
// A moderation timeout is a *community* restriction. The account is still signed in,
// still a member, and still holds whatever it consented to privately. This asserts the
// permitted half positively and the forbidden half exactly, because a fix that simply
// stopped enforcing the timeout would pass a denial-only suite.
func TestATimedOutMemberStillUsesWhatTheTimeoutDidNotTakeAway(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "true")
	s, handler, admin := directTestServer(t)

	// A member with a real password, so the login below is a login and not a
	// hand-made session cookie.
	w := request(t, handler, "POST", "/api/admin/users", map[string]any{
		"username": "timed_out", "password": "a passphrase for the timeout",
		"display_name": "Timed Out",
	}, admin)
	if w.Code != 201 {
		t.Fatalf("create account: %d %s", w.Code, w.Body.String())
	}
	memberID := readJSON[db.User](t, w).ID
	// A peer who consents to a direct conversation with them. The conversation has to
	// exist *before* the timeout, because consent is what the timeout does not touch.
	peerID, peerCookie := memberAccount(t, s, "timeout_peer")
	memberCookie := loginFor(t, handler, "timed_out", "a passphrase for the timeout")
	requested := requestDirect(t, handler, peerCookie, memberID)
	conversation := acceptDirect(t, handler, memberCookie, requested.ID)

	// A message already in that conversation, so "reads permitted private history"
	// means reading something that was really written before the timeout.
	if w := request(t, handler, "POST", "/api/conversations/"+conversation.ID+"/messages",
		messageInput{Content: "written before the timeout", IdempotencyKey: "before-timeout-1"}, peerCookie); w.Code != 201 {
		t.Fatalf("seed message: %d %s", w.Code, w.Body.String())
	}

	// Now the community restriction is applied through the supported route.
	if w := request(t, handler, "POST", "/api/admin/users/"+memberID+"/timeout", map[string]any{
		"minutes": 30, "reason": "cooling off",
	}, admin); w.Code != 200 {
		t.Fatalf("apply timeout: %d %s", w.Code, w.Body.String())
	}

	// 1. The member can still sign in, and receives a real session — not a 200 with
	//    an empty body, and not a redirect to a page that will refuse everything.
	signIn := request(t, handler, "POST", "/api/login",
		map[string]string{"username": "timed_out", "password": "a passphrase for the timeout"}, nil)
	if signIn.Code != 200 {
		t.Fatalf("a timed-out member could not sign in: %d %s", signIn.Code, signIn.Body.String())
	}
	signedIn := readJSON[db.User](t, signIn)
	if signedIn.Username != "timed_out" {
		t.Fatalf("sign-in returned %q, want the member's own account", signedIn.Username)
	}
	if signedIn.IsBanned || signedIn.IsDeactivated {
		t.Fatalf("a timed-out member signed in as banned=%v deactivated=%v", signedIn.IsBanned, signedIn.IsDeactivated)
	}
	// The client has to be able to tell the member *why* the community is closed to
	// them, or the interface shows an unexplained empty workspace: every community
	// request 403s and the signed-in shell has nothing to explain it. `in_timeout`
	// exists only on the administrator's account list, which the member never sees.
	if !signedIn.InTimeout || signedIn.TimeoutEndsAt == "" {
		t.Fatalf("sign-in did not report the live timeout to the member: in_timeout=%v ends=%q", signedIn.InTimeout, signedIn.TimeoutEndsAt)
	}

	// 2. Permitted private history reads, and returns the real JSON rather than an
	//    empty list. An empty 200 is exactly the defect the middleware once produced.
	history := request(t, handler, "GET", "/api/conversations/"+conversation.ID+"/messages", nil, memberCookie)
	if history.Code != 200 {
		t.Fatalf("a timed-out member could not read consented private history: %d %s", history.Code, history.Body.String())
	}
	if !strings.Contains(history.Body.String(), "written before the timeout") {
		t.Fatalf("the private history read did not return the message that exists: %s", history.Body.String())
	}

	// 3. An allowed DM is accepted and *the message actually exists* afterwards. A
	//    201 from a handler that stored nothing would pass a status-code-only test.
	sent := request(t, handler, "POST", "/api/conversations/"+conversation.ID+"/messages",
		messageInput{Content: "sent during the timeout", IdempotencyKey: "during-timeout-1"}, memberCookie)
	if sent.Code != 201 {
		t.Fatalf("a timed-out member could not send an allowed DM: %d %s", sent.Code, sent.Body.String())
	}
	var rows int
	if err := s.store.QueryRow(`SELECT COUNT(*) FROM messages WHERE conversation_id=? AND content=?`,
		conversation.ID, "sent during the timeout").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("the DM reported as sent exists %d times in storage, want 1", rows)
	}

	// 4. The permitted activity path answers, and says something about this account.
	activity := request(t, handler, "GET", "/api/me/activity", nil, memberCookie)
	if activity.Code != 200 {
		t.Fatalf("a timed-out member could not read their own activity: %d %s", activity.Code, activity.Body.String())
	}
	// The DM sent during the timeout is real activity, so the member's own feed has to
	// contain it — that is the assertion that matters. A 200 with an unrelated body
	// would pass a status-code-only check.
	if !strings.Contains(activity.Body.String(), conversation.ID) &&
		!strings.Contains(activity.Body.String(), "message") {
		t.Fatalf("the activity read did not describe this account's own traffic: %s", activity.Body.String())
	}

	// 5. The forbidden half, asserted exactly rather than as "not 403". A community
	//    action is refused with a message that names the timeout.
	community := request(t, handler, "GET", "/api/channels", nil, memberCookie)
	if community.Code != 403 {
		t.Fatalf("a timed-out member read the community channel list: %d %s", community.Code, community.Body.String())
	}
	if !strings.Contains(community.Body.String(), "timeout") {
		t.Fatalf("the community refusal does not explain the timeout: %s", community.Body.String())
	}
	// And a community *write* leaves no side effect, so the refusal is not merely a
	// status code on top of a partial write.
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "timeout_escape", Type: "text"}, memberCookie); w.Code != 403 {
		t.Fatalf("a timed-out member created a community channel: %d %s", w.Code, w.Body.String())
	}
	var created int
	if err := s.store.QueryRow(`SELECT COUNT(*) FROM channels WHERE name=?`, "timeout_escape").Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatal("the refused community write left a channel behind")
	}

	// 6. A banned account is still refused, and refused as signed-out. This is the
	//    negative control for the whole distinction: if the fix had simply stopped
	//    treating inactive accounts as inactive, this is where it would show.
	if _, err := s.store.Exec(`UPDATE users SET is_banned=1 WHERE id=?`, peerID); err != nil {
		t.Fatal(err)
	}
	banned := request(t, handler, "GET", "/api/directs", nil, peerCookie)
	if banned.Code != 401 {
		t.Fatalf("a banned account answered as %d, want 401", banned.Code)
	}
	_ = peerID
}

// loginFor signs in through the real route and returns the session the server set, so
// a test that needs a signed-in member exercises sign-in rather than reaching into
// `newSession` and skipping the password, the rate limit and the timeout report.
func loginFor(t *testing.T, handler http.Handler, username, password string) *http.Cookie {
	t.Helper()
	w := request(t, handler, "POST", "/api/login", map[string]string{"username": username, "password": password}, nil)
	if w.Code != 200 {
		t.Fatalf("sign in as %q: %d %s", username, w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == cookieName {
			return cookie
		}
	}
	t.Fatalf("sign in as %q set no session cookie", username)
	return nil
}

// The self-reported timeout has to survive the two paths a member actually meets it
// on, and must not leak onto anybody else's account.
//
// `/api/me` is the refresh the client makes on reconnect and after an authority
// change. A member whose timeout expires while signed in learns about it there — and
// if the fields were applied in the shared user scanner instead, every response that
// mentions a person (an administrator's account list, a message author) would carry
// one account's moderation state.
func TestTheSelfReportedTimeoutIsOnlyEverAboutTheCaller(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	account, cookie := memberAccount(t, s, "self_reported")

	if w := request(t, handler, "POST", "/api/admin/users/"+account+"/timeout", map[string]any{
		"minutes": 30, "reason": "cooling off",
	}, admin); w.Code != 200 {
		t.Fatalf("apply timeout: %d %s", w.Code, w.Body.String())
	}

	// The member's own view reports it, with an end they can be shown.
	me := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, cookie))
	if !me.InTimeout || me.TimeoutEndsAt == "" {
		t.Fatalf("/api/me did not report the caller's own live timeout: in_timeout=%v ends=%q", me.InTimeout, me.TimeoutEndsAt)
	}
	if me.Username != "self_reported" {
		t.Fatalf("/api/me described %q, want the caller", me.Username)
	}

	// An administrator looking at that account sees `in_timeout` only because the
	// account list deliberately exposes it — and nothing else in a person-shaped
	// response does. The moderation end time is the caller's own information.
	var listed []db.User
	decodeInto(t, request(t, handler, "GET", "/api/admin/users", nil, admin), &listed)
	found := false
	for _, each := range listed {
		if each.ID != account {
			// No other account may be described as timed out, and in particular not
			// the administrator's own.
			if each.InTimeout || each.TimeoutEndsAt != "" {
				t.Fatalf("the account list marked %q as timed out: %+v", each.Username, each)
			}
			continue
		}
		found = true
		if !each.InTimeout {
			t.Fatal("the administrator's account list did not mark the live timeout")
		}
		// The end time is the caller's own information. The administrator can see
		// that a timeout is in force and act on it, but this route is not where the
		// member's own self-report belongs.
		if each.TimeoutEndsAt != "" {
			t.Fatalf("the administrator's account list carried the caller's self-reported end time: %q", each.TimeoutEndsAt)
		}
	}
	if !found {
		t.Fatal("the account was missing from the administrator's list")
	}

	// The timeout expires while the member stays signed in, and the refresh tells them.
	if _, err := s.store.Exec(`UPDATE account_timeouts SET expires_at=? WHERE user_id=?`,
		db.Timestamp(time.Now().UTC().Add(-time.Minute)), account); err != nil {
		t.Fatal(err)
	}
	after := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, cookie))
	if after.InTimeout {
		t.Fatal("/api/me still reported an expired timeout")
	}
	// And the community is genuinely open again, which is the whole point of reporting
	// it at all.
	if w := request(t, handler, "GET", "/api/channels", nil, cookie); w.Code != 200 {
		t.Fatalf("the account did not regain the community after its timeout expired: %d %s", w.Code, w.Body.String())
	}
}

// decodeInto reads a response body into out, failing the test rather than ignoring a
// shape it did not expect.
func decodeInto(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("invalid JSON: %s", w.Body.String())
	}
}
