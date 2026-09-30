package api

import (
	"testing"

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
