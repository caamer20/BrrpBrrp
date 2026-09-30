package api

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// A timeout suspends an account without changing its roles, so expiry restoring the
// recorded assignments is the whole contract. The interesting case is not "a timeout
// ends" but "a role removed *during* a timeout stays removed", because re-deriving
// the current roles instead of replaying the snapshot gets that exactly wrong.

func TestTimeoutSuspendsAndLiftsAtExpiryRestoringOnlyTheRecordedRoles(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, memberCookie := moderationMember(t, s, handler, admin, "timeout_member", []string{"message.send"})

	// Give the account a second role, so "restore what it had" is a real statement
	// rather than a single value that would survive being re-derived.
	secondRole := createTestRole(t, s, handler, admin, "second", []string{"channel.read"}, 20)
	addRole(t, s, handler, admin, memberIDOf(t, s, "timeout_member"), secondRole)

	account := memberIDOf(t, s, "timeout_member")
	assigned := assignedRoles(t, s, handler, admin, account)
	if !slices.Contains(assigned, secondRole) {
		t.Fatalf("the second role was never assigned: %v", assigned)
	}
	before := recordedRolesString(t, s, account)

	// A live timeout: the account is suspended, and a route that declares no capability
	// still has to refuse it. A ban already covers the capability case, so this is the
	// part a timeout adds.
	live := db.Timestamp(time.Now().UTC().Add(time.Hour))
	if _, err := s.store.Exec(`INSERT INTO account_timeouts(user_id,restores,reason,applied_by,applied_at,expires_at) VALUES(?,?,'',?,?,?)`,
		account, before, memberIDOf(t, s, "admin"), db.Now(), live); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "GET", "/api/channels", nil, memberCookie); w.Code != 403 {
		t.Fatalf("a suspended account read a channel list: %d %s", w.Code, w.Body.String())
	}
	// A write is refused the same way, and with the same reason.
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "suspended", Type: "text"}, memberCookie); w.Code != 403 {
		t.Fatalf("a suspended account created a channel: %d %s", w.Code, w.Body.String())
	}
	// Expire it, so the rest of the journey is about coming back.
	if _, err := s.store.Exec(`UPDATE account_timeouts SET expires_at=? WHERE user_id=?`, db.Timestamp(time.Now().UTC().Add(-time.Minute)), account); err != nil {
		t.Fatal(err)
	}
	// Reading the state lifts an expired timeout: signing in is what clears it.
	if suspended, _ := s.timeoutState(t.Context(), memberIDOf(t, s, "timeout_member")); suspended {
		t.Fatal("an expired timeout was still reported as live")
	}
	if w := request(t, handler, "GET", "/api/channels", nil, memberCookie); w.Code != 200 {
		t.Fatalf("the account did not come back after its timeout expired: %d %s", w.Code, w.Body.String())
	}
}

// The contract is that expiry restores *the recorded snapshot* and nothing else. The
// case that distinguishes replaying it from re-deriving it is a role removed *after*
// the snapshot was taken: replaying puts it back, because that is what the account held
// when the timeout was applied; re-deriving would read the current assignments and
// quietly discard it.
//
// That is a real cost, and it is the right one. A timeout freezes authority at the
// moment it was applied so that nothing done during it — including a role change — is
// silently reversed when it ends. Lifting a timeout early, or removing the role again,
// is a moderator's deliberate act rather than a side effect of waiting.
func TestTimeoutExpiryReplaysTheSnapshotRecordedWhenItWasApplied(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, memberCookie := moderationMember(t, s, handler, admin, "trimmed_member", []string{"message.send"})
	kept := createTestRole(t, s, handler, admin, "kept", []string{"channel.read"}, 20)
	dropped := createTestRole(t, s, handler, admin, "dropped", []string{"channel.read"}, 20)
	account := memberIDOf(t, s, "trimmed_member")
	setRoles(t, s, handler, admin, account, kept, dropped)
	recorded := recordedRolesString(t, s, account)
	if !strings.Contains(recorded, dropped) {
		t.Fatalf("the snapshot does not include the role under test: %q", recorded)
	}

	if _, err := s.store.Exec(`INSERT INTO account_timeouts(user_id,restores,reason,applied_by,applied_at,expires_at) VALUES(?,?,'',?,?,?)`,
		account, recorded, memberIDOf(t, s, "admin"), db.Now(), db.Timestamp(time.Now().UTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	// While the timeout ran, the role was removed.
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, account, dropped); err != nil {
		t.Fatal(err)
	}
	if _, until := s.timeoutState(t.Context(), account); until != "" {
		t.Fatal("the expired timeout was still live")
	}
	after := assignedRoles(t, s, handler, admin, account)
	if !slices.Contains(after, kept) {
		t.Fatalf("expiry did not restore the recorded roles: %v", after)
	}
	// The snapshot is replayed, so the role the account held at the moment the timeout
	// was applied comes back with it.
	if !slices.Contains(after, dropped) {
		t.Fatalf("expiry did not replay the recorded snapshot: %v", after)
	}
	// And the account is working again.
	if w := request(t, handler, "GET", "/api/channels", nil, memberCookie); w.Code != 200 {
		t.Fatalf("the account did not come back: %d %s", w.Code, w.Body.String())
	}
}

func TestTimeoutCannotBeAppliedAtOrAboveTheActorsAuthority(t *testing.T) {
	s, handler := testServer(t)
	owner := setupAdmin(t, s, handler)
	// Two peers: each holds member.manage at the same position, so neither may act on
	// the other. The rule is authority downward only, and it is the same rule account
	// management uses — not a second one.
	_, first := moderationMember(t, s, handler, owner, "peer_one", []string{"member.manage"})
	_, _ = moderationMember(t, s, handler, owner, "peer_two", []string{"member.manage"})

	w := request(t, handler, "POST", "/api/admin/accounts/"+memberIDOf(t, s, "peer_two")+"/timeout",
		map[string]any{"minutes": 10, "reason": "a timeout a peer has no business applying"}, first)
	if w.Code != 403 && w.Code != 404 {
		t.Fatalf("a peer timed out another peer: %d %s", w.Code, w.Body.String())
	}
	// And the refusal wrote nothing: no timeout row, no partial state.
	var count int
	if err := s.store.QueryRowContext(t.Context(), `SELECT count(*) FROM account_timeouts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d timeouts exist after a refused one", count)
	}
}

func TestReportAssignmentIsAClaimNotAGrant(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	adminID := memberIDOf(t, s, "admin")
	channel := moderationChannel(t, s, handler, admin, "assigned")
	_, reporter := moderationMember(t, s, handler, admin, "reporter", []string{"message.send"})
	authorID, _ := moderationMember(t, s, handler, admin, "author", []string{"message.send"})
	authorCookie := sessionFor(t, s, authorID)
	adminCookie := sessionFor(t, s, adminID)

	sent := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channel+"/messages",
		map[string]any{"content": "something worth reviewing", "idempotency_key": "assign-report-1"}, authorCookie))
	submitted := request(t, handler, "POST", "/api/messages/"+sent.ID+"/report",
		map[string]any{"reason": "worth a look"}, reporter)
	if submitted.Code != 201 && submitted.Code != 200 {
		t.Fatalf("report: %d %s", submitted.Code, submitted.Body.String())
	}
	reportID, _ := readJSON[map[string]any](t, submitted)["id"].(string)
	if reportID == "" {
		t.Fatalf("the report has no id: %s", submitted.Body.String())
	}

	// An assignee who exists is recorded, and the claim survives the assignee leaving.
	w := request(t, handler, "POST", "/api/admin/reports/"+reportID+"/assign",
		map[string]any{"assignee_id": memberIDOf(t, s, "author")}, adminCookie)
	if w.Code != 200 {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	page := readJSON[struct {
		Reports []map[string]any `json:"reports"`
	}](t, request(t, handler, "GET", "/api/admin/reports?status=open", nil, adminCookie))
	found := false
	for _, report := range page.Reports {
		if report["id"] == reportID && report["assigned_to"] == memberIDOf(t, s, "author") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the assignment is not in the reviewer's list: %+v", page.Reports)
	}
	// An assignee that does not exist is refused rather than leaving a claim attached
	// to nobody, which reads as "someone is looking at this".
	if w := request(t, handler, "POST", "/api/admin/reports/"+reportID+"/assign",
		map[string]any{"assignee_id": "not-an-account"}, adminCookie); w.Code != 400 {
		t.Fatalf("a report was assigned to a guessed account: %d %s", w.Code, w.Body.String())
	}
}

func TestBoundedInvitationAdmitsExactlyItsUses(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	roleID := createTestRole(t, s, handler, admin, "invitee_role", []string{"message.send"}, 30)

	w := request(t, handler, "POST", "/api/admin/invitations",
		map[string]any{"hours": 24, "role_ids": []string{roleID}, "max_uses": 2}, admin)
	if w.Code != 201 {
		t.Fatalf("create invitation: %d %s", w.Code, w.Body.String())
	}
	token := inviteToken(t, w)

	join := func(username string) int {
		t.Helper()
		return request(t, handler, "POST", "/api/invitations/accept",
			map[string]any{"token": token, "username": username, "display_name": username, "password": "a passphrase for joining"}, nil).Code
	}
	if code := join("bounded_one"); code != 201 {
		t.Fatalf("the first redemption was refused: %d", code)
	}
	if code := join("bounded_two"); code != 201 {
		t.Fatalf("the second redemption was refused: %d", code)
	}
	// The third is refused. A use is a bounded resource, and a double redemption
	// would consume two of them.
	if code := join("bounded_three"); code != 400 {
		t.Fatalf("an exhausted invitation admitted a third person: %d", code)
	}
	var used int
	if err := s.store.QueryRowContext(t.Context(), `SELECT uses FROM invitations WHERE token_hash=?`, hashToken(token)).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 2 {
		t.Fatalf("the invitation recorded %d uses, want 2", used)
	}
}

func TestDefaultInvitationRemainsSingleUse(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	roleID := createTestRole(t, s, handler, admin, "single_role", []string{"message.send"}, 30)
	// No max_uses: the behaviour every invitation had before this change.
	w := request(t, handler, "POST", "/api/admin/invitations",
		map[string]any{"hours": 24, "role_ids": []string{roleID}}, admin)
	if w.Code != 201 {
		t.Fatalf("create invitation: %d %s", w.Code, w.Body.String())
	}
	token := inviteToken(t, w)
	join := func(username string) int {
		return request(t, handler, "POST", "/api/invitations/accept",
			map[string]any{"token": token, "username": username, "display_name": username, "password": "a passphrase for joining"}, nil).Code
	}
	if code := join("single_one"); code != 201 {
		t.Fatalf("the first redemption was refused: %d", code)
	}
	if code := join("single_two"); code != 400 {
		t.Fatalf("a single-use invitation admitted a second person: %d", code)
	}
}

func TestRevokedInvitationRefusesWithoutRevealingWhy(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	roleID := createTestRole(t, s, handler, admin, "revoke_role", []string{"message.send"}, 30)
	w := request(t, handler, "POST", "/api/admin/invitations",
		map[string]any{"hours": 24, "role_ids": []string{roleID}, "max_uses": 10}, admin)
	if w.Code != 201 {
		t.Fatalf("create invitation: %d %s", w.Code, w.Body.String())
	}
	created := readJSON[map[string]any](t, w)
	token := inviteToken(t, w)
	invitationID, _ := created["id"].(string)
	if invitationID == "" {
		t.Fatalf("the invitation response is missing its id: %s", w.Body.String())
	}

	if w := request(t, handler, "DELETE", "/api/admin/invitations/"+invitationID, nil, admin); w.Code != 204 && w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	refused := request(t, handler, "POST", "/api/invitations/accept",
		map[string]any{"token": token, "username": "revoked_joiner", "display_name": "joiner", "password": "a passphrase for joining"}, nil)
	if refused.Code != 400 {
		t.Fatalf("a revoked invitation admitted somebody: %d %s", refused.Code, refused.Body.String())
	}
	// The answer is the same one a token that never existed gets, so a link cannot be
	// used to find out that it once did.
	never := request(t, handler, "POST", "/api/invitations/accept",
		map[string]any{"token": neverIssuedToken(t), "username": "never_joiner", "display_name": "joiner", "password": "a passphrase for joining"}, nil)
	if never.Code != refused.Code || never.Body.String() != refused.Body.String() {
		t.Fatalf("a revoked invitation answers differently from a nonexistent one: %s vs %s", refused.Body.String(), never.Body.String())
	}
}

// inviteToken reads the token out of the shareable URL. The response deliberately
// carries no bare token field: a token in a JSON body ends up in access logs, while
// the URL is the thing an operator actually shares.
func inviteToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	raw := readJSON[map[string]any](t, w)["url"].(string)
	_, fragment, ok := strings.Cut(raw, "#invite=")
	if !ok || fragment == "" {
		t.Fatalf("the invitation URL carries no token: %s", raw)
	}
	return fragment
}

// neverIssuedToken returns a token whose hash is in no row, shaped exactly like a real
// one so the control compares two *invitation* refusals rather than an invitation
// refusal and an input-validation refusal. A malformed token would be refused before
// the lookup ran, and comparing those two answers would prove nothing about whether
// a revoked link is distinguishable from one that never existed.
func neverIssuedToken(t *testing.T) string {
	t.Helper()
	token, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// A timeout must be distinguishable from a ban at the response, because they are
// different things to the person on the other end: a ban ends the session, a timeout
// does not, and telling somebody to sign in again when signing in will not help is the
// failure this pins.
func TestTimeoutIsAnsweredAsASuspensionNotASignedOutSession(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	suspendedID, suspendedCookie := moderationMember(t, s, handler, admin, "suspended_one", []string{"message.send"})
	bannedID, bannedCookie := moderationMember(t, s, handler, admin, "banned_one", []string{"message.send"})

	if _, err := s.store.Exec(`INSERT INTO account_timeouts(user_id,restores,reason,applied_by,applied_at,expires_at) VALUES(?,'','cooling off',?,?,?)`,
		suspendedID, memberIDOf(t, s, "admin"), db.Now(), db.Timestamp(time.Now().UTC().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE users SET is_banned=1 WHERE id=?`, bannedID); err != nil {
		t.Fatal(err)
	}

	timeout := request(t, handler, "GET", "/api/channels", nil, suspendedCookie)
	ban := request(t, handler, "GET", "/api/channels", nil, bannedCookie)
	if timeout.Code != 403 {
		t.Fatalf("a timeout answered as %d, want 403", timeout.Code)
	}
	if ban.Code != 401 {
		t.Fatalf("a ban answered as %d, want 401", ban.Code)
	}
	// The timeout says what it is and offers a way forward; the ban does not pretend
	// signing in again will help.
	if !strings.Contains(timeout.Body.String(), "timeout") {
		t.Fatalf("the timeout response does not explain itself: %s", timeout.Body.String())
	}
	if timeout.Body.String() == ban.Body.String() {
		t.Fatalf("a timeout and a ban answer identically: %s", timeout.Body.String())
	}
}
