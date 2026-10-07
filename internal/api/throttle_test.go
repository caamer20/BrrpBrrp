package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/app"
)

// The credential throttle (decision D-17) counts failures and never attempts, keeps the
// counters an unauthenticated request can create away from the ones every signed-in
// request is checked against, and never lets a full table refuse a caller it already
// knows. Each test below is one of the ways the earlier throttle went wrong.

const adminPassword = "correct horse battery"

// requestFrom is `request` from a chosen address, because the throttle's whole subject is
// where a request came from.
func requestFrom(t *testing.T, handler http.Handler, address, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.RemoteAddr = address
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://localhost:8080")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func signIn(t *testing.T, handler http.Handler, address, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	return requestFrom(t, handler, address, "POST", "/api/login", map[string]string{"username": username, "password": password}, nil)
}

const (
	household = "198.51.100.7:4000"
	elsewhere = "203.0.113.9:4000"
)

// The old budget was spent by success. With a budget of three, setup and two sign-ins
// used it up and the next person behind the same address was refused although nobody
// had typed anything wrong.
func TestSuccessfulSignInsSpendNothing(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	// Past the default ceiling of twenty, which is where the old budget refused.
	for attempt := 1; attempt <= app.LoginAttemptsDefault+5; attempt++ {
		if w := signIn(t, handler, household, "admin", adminPassword); w.Code != 200 {
			t.Fatalf("correct sign-in %d from one address: %d %s", attempt, w.Code, w.Body.String())
		}
	}
	// And a failure after all of them is the address's first, not its twenty-sixth.
	if w := signIn(t, handler, household, "admin", "not the right passphrase"); w.Code != 401 {
		t.Fatalf("the first wrong password after %d right ones: %d", app.LoginAttemptsDefault+5, w.Code)
	}
	if w := signIn(t, handler, household, "admin", adminPassword); w.Code != 200 {
		t.Fatalf("a correct sign-in after one failure: %d", w.Code)
	}
}

// Success is not counted as failure, but it is not unlimited: every correct password
// costs a hash and leaves a session behind. One account is rated at three times the
// address ceiling per window — six here — and the rate is the account's own, so it says
// nothing to anybody who does not already hold the password and costs nobody else
// anything.
func TestACorrectPasswordIsRatedPerAccount(t *testing.T) {
	t.Setenv("BRRP_LOGIN_ATTEMPTS", "2")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	if w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "second_member", "password": "a private initial passphrase"}, admin); w.Code != 201 {
		t.Fatalf("create member: %d %s", w.Code, w.Body.String())
	}
	for attempt := 1; attempt <= 6; attempt++ {
		if w := signIn(t, handler, household, "admin", adminPassword); w.Code != 200 {
			t.Fatalf("sign-in %d of a rate of six: %d %s", attempt, w.Code, w.Body.String())
		}
	}
	w := signIn(t, handler, household, "admin", adminPassword)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("sign-in past the account's rate: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	// The same address, another account: its own rate, untouched.
	if w := signIn(t, handler, household, "second_member", "a private initial passphrase"); w.Code != 200 {
		t.Fatalf("another account behind the same address: %d %s", w.Code, w.Body.String())
	}
}

// BRRP_LOGIN_ATTEMPTS is the ceiling on *failures* from one address. An operator who sets
// it gets that number; a Config that carries none gets the shipped default, never "no
// limit"; and once an address reaches it, the address is refused whatever it sends —
// which is the point of a ceiling, and why the operator of a shared address raises it.
func TestTheSignInBudgetIsTheConfiguredCeilingOnFailures(t *testing.T) {
	t.Setenv("BRRP_LOGIN_ATTEMPTS", "3")
	s, handler := testServer(t)
	if s.config.LoginAttempts != 3 {
		t.Fatalf("configured budget: %d", s.config.LoginAttempts)
	}
	setupAdmin(t, s, handler)
	for failure := 1; failure <= 3; failure++ {
		w := signIn(t, handler, household, fmt.Sprintf("nobody_%d", failure), "not the right passphrase")
		if w.Code != 401 {
			t.Fatalf("failure %d of a budget of three: %d %s", failure, w.Code, w.Body.String())
		}
	}
	w := signIn(t, handler, household, "admin", adminPassword)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("an address at its ceiling: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	// Another address is another budget.
	if w := signIn(t, handler, elsewhere, "admin", adminPassword); w.Code != 200 {
		t.Fatalf("a correct sign-in from an address that never failed: %d %s", w.Code, w.Body.String())
	}

	// A zero value is what a Config built in a test looks like; it must mean the default.
	fresh := &Server{config: app.Config{}, guests: newRateLimiterOf(64)}
	probe := httptest.NewRequest("POST", "/api/login", nil)
	address, open := fresh.guestOpen(httptest.NewRecorder(), probe)
	if !open {
		t.Fatal("a zero budget must mean the default, not no attempts")
	}
	for failure := 1; failure < app.LoginAttemptsDefault; failure++ {
		fresh.guestFailed(address)
	}
	if _, open := fresh.guestOpen(httptest.NewRecorder(), probe); !open {
		t.Fatalf("an address one failure short of the default ceiling of %d was refused", app.LoginAttemptsDefault)
	}
	fresh.guestFailed(address)
	if _, open := fresh.guestOpen(httptest.NewRecorder(), probe); open {
		t.Fatalf("an address at the default ceiling of %d was still admitted", app.LoginAttemptsDefault)
	}
}

// Failures against one name are counted where they came from. Ten wrong passwords for
// `admin` from one address close that name to that address — and to nobody else: the
// member signs in from their own address as if nothing had happened.
func TestFailingAgainstANameFromOneAddressDoesNotLockItOutFromAnother(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	for failure := 1; failure <= credentialFailuresPerKey; failure++ {
		if w := signIn(t, handler, elsewhere, "admin", "not the right passphrase"); w.Code != 401 {
			t.Fatalf("failure %d against admin: %d %s", failure, w.Code, w.Body.String())
		}
	}
	// The attacker's address is done with this name, even if it now guesses right.
	if w := signIn(t, handler, elsewhere, "admin", adminPassword); w.Code != 429 {
		t.Fatalf("the address that failed %d times against admin: %d", credentialFailuresPerKey, w.Code)
	}
	// A different spelling is the same account and the same counter.
	if w := signIn(t, handler, elsewhere, "ADMIN", adminPassword); w.Code != 429 {
		t.Fatalf("a re-spelt name escaped its counter: %d", w.Code)
	}
	// The member is untouched.
	if w := signIn(t, handler, household, "admin", adminPassword); w.Code != 200 {
		t.Fatalf("the member's own sign-in after somebody else's failures: %d %s", w.Code, w.Body.String())
	}
}

// A name that does not exist is counted exactly as one that does, so the throttle cannot
// be used to find out which names are accounts.
func TestAnUnknownNameIsThrottledExactlyLikeAKnownOne(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	outcome := func(username, address string) []int {
		codes := []int{}
		for attempt := 0; attempt <= credentialFailuresPerKey; attempt++ {
			codes = append(codes, signIn(t, handler, address, username, "not the right passphrase").Code)
		}
		return codes
	}
	known, unknown := outcome("admin", "198.51.100.20:1"), outcome("not_an_account", "198.51.100.21:1")
	if fmt.Sprint(known) != fmt.Sprint(unknown) {
		t.Fatalf("a known and an unknown name answer differently:\n  known %v\nunknown %v", known, unknown)
	}
	if last := known[len(known)-1]; last != 429 {
		t.Fatalf("attempt %d against one name answered %d, want 429", len(known), last)
	}
}

// Keying sign-in by name is what would have let one address fill the table. It cannot:
// the address ceiling is checked first, so an address that has reached it creates no
// further keys — and a member who is already signed in is not asked about any of it.
func TestAFloodOfNamesStopsAtTheAddressCeilingAndLeavesMembersAlone(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	refused := 0
	for attempt := 0; attempt < app.LoginAttemptsDefault+25; attempt++ {
		w := signIn(t, handler, household, fmt.Sprintf("spray_%03d", attempt), "not the right passphrase")
		switch w.Code {
		case 401:
			if refused > 0 {
				t.Fatalf("attempt %d was evaluated after the address had been refused", attempt)
			}
		case 429:
			refused++
		default:
			t.Fatalf("attempt %d: %d %s", attempt, w.Code, w.Body.String())
		}
	}
	if refused != 25 {
		t.Fatalf("%d attempts were refused, want the 25 past a ceiling of %d", refused, app.LoginAttemptsDefault)
	}
	s.guests.Lock()
	keys := len(s.guests.buckets)
	s.guests.Unlock()
	// One counter for the address and one per name that was evaluated.
	if keys > app.LoginAttemptsDefault+1 {
		t.Fatalf("the flood left %d counters, want at most %d", keys, app.LoginAttemptsDefault+1)
	}
	// The same address, with a session: a member's requests are not this throttle's.
	if w := requestFrom(t, handler, household, "GET", "/api/me", nil, admin); w.Code != 200 {
		t.Fatalf("a signed-in member behind the flooding address: %d %s", w.Code, w.Body.String())
	}
}

// A full table is not a refusal. It used to be one — for everybody, including callers the
// table already knew — which let unauthenticated traffic close the server to its members.
func TestAFullTableRefusesNobodyItAlreadyKnows(t *testing.T) {
	// The table itself: a known key is answered from its own counter; only a key the
	// table has never seen is turned away while there is no room.
	table := newRateLimiterOf(2)
	if !table.allow("first", 5, time.Minute) || !table.allow("second", 5, time.Minute) {
		t.Fatal("two keys did not fit a table of two")
	}
	if table.allow("third", 5, time.Minute) {
		t.Fatal("a third key fitted a full table of two")
	}
	if !table.allow("first", 5, time.Minute) {
		t.Fatal("a key the table already holds was refused because the table is full")
	}
	if table.note("fourth", time.Minute) {
		t.Fatal("a failure against a new key was recorded in a full table")
	}
	if table.blocked("fourth", 1) {
		t.Fatal("a key that could not be recorded reads as blocked")
	}
	// An expired counter makes room.
	short := newRateLimiterOf(1)
	short.allow("gone", 1, time.Nanosecond)
	time.Sleep(time.Millisecond)
	if !short.allow("new", 1, time.Minute) {
		t.Fatal("an expired counter did not make room for a new key")
	}

	// The server: fill the guests' table with other callers' counters.
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.guests = newRateLimiterOf(4)
	for index := 0; index < 4; index++ {
		s.guests.note(fmt.Sprintf("address:192.0.2.%d", index+10), time.Hour)
	}
	// A member's request is checked against the other table and is unaffected.
	if w := requestFrom(t, handler, household, "GET", "/api/me", nil, admin); w.Code != 200 {
		t.Fatalf("a signed-in member with the guests' table full: %d", w.Code)
	}
	// A correct sign-in needs no counter, so it is not refused either.
	if w := signIn(t, handler, household, "admin", adminPassword); w.Code != 200 {
		t.Fatalf("a correct sign-in with the guests' table full: %d %s", w.Code, w.Body.String())
	}
	// A wrong one is answered as a wrong one — not as a throttle.
	if w := signIn(t, handler, household, "admin", "not the right passphrase"); w.Code != 401 {
		t.Fatalf("a wrong sign-in with the guests' table full: %d", w.Code)
	}
}

// One invitation may admit fifty people, and they often accept it from the same room.
// The old budget refused the twenty-first request from an address whether or not anybody
// had failed. Twenty-five is past that budget and keeps the test's hashing bill small.
func TestMoreThanTwentyPeopleAcceptOneInvitationFromOneAddress(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	created := request(t, handler, "POST", "/api/admin/invitations", map[string]int{"hours": 24, "max_uses": 50}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("create invitation: %d %s", created.Code, created.Body.String())
	}
	_, token, found := strings.Cut(readJSON[map[string]any](t, created)["url"].(string), "#invite=")
	if !found || len(token) != 43 {
		t.Fatalf("the invitation carries no token: %s", created.Body.String())
	}
	for guest := 1; guest <= 25; guest++ {
		w := requestFrom(t, handler, household, "POST", "/api/invitations/accept", map[string]string{
			"token":    token,
			"username": fmt.Sprintf("guest_%02d", guest),
			"password": "a guest's own passphrase",
		}, nil)
		if w.Code != http.StatusCreated {
			t.Fatalf("guest %d accepting a fifty-use invitation from one address: %d %s", guest, w.Code, w.Body.String())
		}
	}
	// A token that admits nobody is what is counted, and the ceiling still holds for it.
	wrong := strings.Repeat("A", 43)
	for failure := 1; failure <= app.LoginAttemptsDefault; failure++ {
		w := requestFrom(t, handler, elsewhere, "POST", "/api/invitations/accept", map[string]string{
			"token": wrong, "username": fmt.Sprintf("prober_%02d", failure), "password": "a prober's own passphrase",
		}, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid token %d: %d %s", failure, w.Code, w.Body.String())
		}
	}
	w := requestFrom(t, handler, elsewhere, "POST", "/api/invitations/accept", map[string]string{
		"token": token, "username": "late_guest", "password": "a guest's own passphrase",
	}, nil)
	if w.Code != 429 {
		t.Fatalf("an address that guessed %d tokens: %d, want 429", app.LoginAttemptsDefault, w.Code)
	}
}

// A signed-in member's password re-checks are counted against the account. Wrong ones
// close that account's re-checks for a window; they do not spend anything for the other
// people behind the same address, which the shared budget did.
func TestAWrongCurrentPasswordIsCountedAgainstTheAccount(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	if w := request(t, handler, "POST", "/api/admin/users", map[string]any{"username": "second_member", "password": "a private initial passphrase"}, admin); w.Code != 201 {
		t.Fatalf("create member: %d %s", w.Code, w.Body.String())
	}
	login := signIn(t, handler, household, "second_member", "a private initial passphrase")
	if login.Code != 200 {
		t.Fatalf("member sign-in: %d %s", login.Code, login.Body.String())
	}
	member := login.Result().Cookies()[0]

	wrong := map[string]string{"current_password": "not the right passphrase", "new_password": "a brand new passphrase"}
	for failure := 1; failure <= credentialFailuresPerKey; failure++ {
		if w := requestFrom(t, handler, household, "POST", "/api/me/password", wrong, admin); w.Code != 400 {
			t.Fatalf("wrong current password %d: %d %s", failure, w.Code, w.Body.String())
		}
	}
	right := map[string]string{"current_password": adminPassword, "new_password": "a brand new passphrase"}
	if w := requestFrom(t, handler, household, "POST", "/api/me/password", right, admin); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("the account that failed %d re-checks: %d", credentialFailuresPerKey, w.Code)
	}
	// The other member behind the same address re-enters their own password freely.
	if w := requestFrom(t, handler, household, "POST", "/api/me/reauth", map[string]string{"password": "a private initial passphrase"}, member); w.Code == 429 {
		t.Fatal("one account's wrong passwords closed another account's re-check")
	}
	// And sign-in from that address is a different counter altogether.
	if w := signIn(t, handler, household, "second_member", "a private initial passphrase"); w.Code != 200 {
		t.Fatalf("sign-in behind an address whose other account failed re-checks: %d", w.Code)
	}
}

func TestAnIPv6SubscriberIsOneCallerAndAnIPv4AddressIsItself(t *testing.T) {
	for address, want := range map[string]string{
		"198.51.100.7":         "198.51.100.7",
		"::ffff:198.51.100.7":  "198.51.100.7",
		"2001:db8:1:2::1":      "2001:db8:1:2::/64",
		"2001:db8:1:2:a:b:c:d": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":      "2001:db8:1:3::/64",
		"unknown":              "unknown",
	} {
		if got := addressKey(address); got != want {
			t.Errorf("addressKey(%q) = %q, want %q", address, got, want)
		}
	}
}
