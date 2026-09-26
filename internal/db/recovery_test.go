package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// recoveryFixture builds the smallest installation that can be locked out: an
// owner with an enrolled factor, recovery codes, two signed-in devices and a push
// subscription; a second administrator with a device of their own; and an ordinary
// member with messages and a role. Recovery must restore the owner and leave every
// one of those other things exactly where it was.
type recoveryFixture struct {
	store     *Store
	ownerID   string
	adminID   string
	memberID  string
	ownerPass string
}

const ownerUsername = "owner_account"

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	fixture := &recoveryFixture{store: store, ownerPass: "the owner's original passphrase"}
	if err := store.CreateAdministrator(ctx, ownerUsername, fixture.ownerPass, 12582912); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAdministrator(ctx, "second_admin", "a second administrator passphrase", 12582912); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT id FROM users WHERE username=?`, ownerUsername).Scan(&fixture.ownerID); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRow(`SELECT id FROM users WHERE username='second_admin'`).Scan(&fixture.adminID); err != nil {
		t.Fatal(err)
	}
	fixture.memberID = "u-member"
	memberHash, err := bcrypt.GenerateFromPassword([]byte("the member's private passphrase"), 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`INSERT INTO users(id,username,display_name,password_hash,is_admin,created_at,updated_at) VALUES(?,?,?,?,0,?,?)`,
		fixture.memberID, "member_account", "Member Account", string(memberHash), Now(), Now()); err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AssignAccountRole(ctx, tx, fixture.memberID, MemberRoleID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`UPDATE users SET is_owner=1 WHERE id=?`, fixture.ownerID); err != nil {
		t.Fatal(err)
	}
	var channel string
	if err := store.QueryRow(`SELECT id FROM channels WHERE name='general'`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := store.Exec(`INSERT INTO messages(id,channel_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?)`,
			ID(), channel, fixture.memberID, "member history", Now(), ID()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Exec(`INSERT INTO user_mfa(user_id,secret,last_counter) VALUES(?,?,7)`, fixture.ownerID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"owner-code-a", "owner-code-b", "owner-code-c"} {
		if _, err := store.Exec(`INSERT INTO recovery_codes(user_id,code_hash) VALUES(?,?)`, fixture.ownerID, code); err != nil {
			t.Fatal(err)
		}
	}
	for name, user := range map[string]string{"s-owner-1": fixture.ownerID, "s-owner-2": fixture.ownerID, "s-admin-1": fixture.adminID, "s-member-1": fixture.memberID} {
		if _, err := store.Exec(`INSERT INTO sessions(id,user_id,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)`,
			name, user, "hash-"+name, Now(), Timestamp(time.Now().Add(30*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Exec(`INSERT INTO push_subscriptions(id,user_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?)`,
		"push-owner", fixture.ownerID, "https://push.example/owner", "key", "auth", Now()); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func passwordIs(t *testing.T, store *Store, userID, password string) bool {
	t.Helper()
	var hash string
	if err := store.QueryRow(`SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func TestOfflineOwnerRecoveryRestoresOnlyTheOwnersCredentials(t *testing.T) {
	fixture := newRecoveryFixture(t)
	store := fixture.store
	ctx := context.Background()
	replacement := "the owner's replacement passphrase"
	recovery, err := store.RecoverOwner(ctx, "Owner_Account", replacement)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Username != ownerUsername || !recovery.FactorRemoved || recovery.CodesRemoved != 3 || recovery.SessionsEnded != 2 {
		t.Fatalf("unexpected recovery report: %+v", recovery)
	}
	if recovery.Reactivated || recovery.BanCleared {
		t.Fatalf("recovery touched moderation state it was not asked to: %+v", recovery)
	}
	if !passwordIs(t, store, fixture.ownerID, replacement) || passwordIs(t, store, fixture.ownerID, fixture.ownerPass) {
		t.Fatal("the owner's password was not replaced")
	}
	for _, check := range []struct {
		what     string
		query    string
		args     []any
		expected int
	}{
		{"second factor", `SELECT count(*) FROM user_mfa WHERE user_id=?`, []any{fixture.ownerID}, 0},
		{"recovery codes", `SELECT count(*) FROM recovery_codes WHERE user_id=?`, []any{fixture.ownerID}, 0},
		{"owner sessions", `SELECT count(*) FROM sessions WHERE user_id=?`, []any{fixture.ownerID}, 0},
		{"owner push subscriptions", `SELECT count(*) FROM push_subscriptions WHERE user_id=?`, []any{fixture.ownerID}, 0},
		{"audit rows", `SELECT count(*) FROM audit_logs WHERE action='owner.recovered_offline'`, nil, 1},
	} {
		if count := countRows(t, store, check.query, check.args...); count != check.expected {
			t.Fatalf("%s: got %d, want %d", check.what, count, check.expected)
		}
	}
	// Everyone else, and the owner's own authority and content, are untouched.
	if count := countRows(t, store, `SELECT count(*) FROM sessions WHERE user_id IN (?,?)`, fixture.adminID, fixture.memberID); count != 2 {
		t.Fatalf("other accounts' sessions changed: %d", count)
	}
	if count := countRows(t, store, `SELECT count(*) FROM messages WHERE user_id=?`, fixture.memberID); count != 2 {
		t.Fatalf("member history changed: %d", count)
	}
	var owner bool
	if err := store.QueryRow(`SELECT is_owner FROM users WHERE id=?`, fixture.ownerID).Scan(&owner); err != nil || !owner {
		t.Fatalf("ownership was not preserved: %v %v", owner, err)
	}
	roles, err := store.AssignedRoleIDs(ctx, store, fixture.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, role := range roles {
		found = found || role == AdministratorRoleID
	}
	if !found {
		t.Fatalf("role assignments changed: %v", roles)
	}
	if !passwordIs(t, store, fixture.memberID, "the member's private passphrase") {
		t.Fatal("another account's password changed")
	}
}

func TestOfflineOwnerRecoveryRefusesEveryOtherAccount(t *testing.T) {
	fixture := newRecoveryFixture(t)
	store := fixture.store
	ctx := context.Background()
	if _, err := store.Exec(`UPDATE users SET is_owner=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverOwner(ctx, ownerUsername, "a replacement passphrase"); err == nil || !strings.Contains(err.Error(), "create-admin") {
		t.Fatalf("an installation without an owner was not redirected: %v", err)
	}
	if _, err := store.Exec(`UPDATE users SET is_owner=1 WHERE id=?`, fixture.ownerID); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		what     string
		username string
		password string
		want     string
	}{
		{"administrator", "second_admin", "a replacement passphrase", "not the installation owner"},
		{"ordinary member", "member_account", "a replacement passphrase", "not the installation owner"},
		{"unknown account", "nobody_here", "a replacement passphrase", "no account is named"},
		{"missing username", "", "a replacement passphrase", "requires --username"},
		{"short password", ownerUsername, "too short", "12–72 byte password"},
		{"unbounded password", ownerUsername, strings.Repeat("x", 73), "12–72 byte password"},
	} {
		if _, err := store.RecoverOwner(ctx, attempt.username, attempt.password); err == nil || !strings.Contains(err.Error(), attempt.want) {
			t.Fatalf("%s: got %v, want a refusal mentioning %q", attempt.what, err, attempt.want)
		}
	}
	// A refusal must not be a half-applied change: nobody's credentials moved.
	if !passwordIs(t, store, fixture.ownerID, fixture.ownerPass) || !passwordIs(t, store, fixture.adminID, "a second administrator passphrase") {
		t.Fatal("a refused recovery changed a password")
	}
	if count := countRows(t, store, `SELECT count(*) FROM user_mfa WHERE user_id=?`, fixture.ownerID); count != 1 {
		t.Fatalf("a refused recovery removed the factor: %d", count)
	}
	if count := countRows(t, store, `SELECT count(*) FROM sessions WHERE user_id=?`, fixture.ownerID); count != 2 {
		t.Fatalf("a refused recovery ended sessions: %d", count)
	}
	if count := countRows(t, store, `SELECT count(*) FROM audit_logs WHERE action='owner.recovered_offline'`); count != 0 {
		t.Fatalf("a refused recovery was audited as a success: %d", count)
	}
}

func TestOfflineOwnerRecoveryLiftsALockoutAndRecordsIt(t *testing.T) {
	fixture := newRecoveryFixture(t)
	store := fixture.store
	ctx := context.Background()
	if _, err := store.Exec(`UPDATE users SET is_deactivated=1,is_banned=1,ban_reason='locked out' WHERE id=?`, fixture.ownerID); err != nil {
		t.Fatal(err)
	}
	recovery, err := store.RecoverOwner(ctx, ownerUsername, "a replacement passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Reactivated || !recovery.BanCleared {
		t.Fatalf("a locked-out owner stayed locked out: %+v", recovery)
	}
	var deactivated, banned bool
	var reason string
	if err := store.QueryRow(`SELECT is_deactivated,is_banned,ban_reason FROM users WHERE id=?`, fixture.ownerID).Scan(&deactivated, &banned, &reason); err != nil {
		t.Fatal(err)
	}
	if deactivated || banned || reason != "" {
		t.Fatalf("moderation state survived recovery: %v %v %q", deactivated, banned, reason)
	}
	// The record names the account it restored and claims no actor, because the
	// person acting was whoever held the host, not an authenticated user.
	var action, target string
	var actor *string
	if err := store.QueryRow(`SELECT action,target_id,actor_id FROM audit_logs WHERE target_id=? AND action='owner.recovered_offline'`, fixture.ownerID).Scan(&action, &target, &actor); err != nil {
		t.Fatal(err)
	}
	if actor != nil {
		t.Fatalf("an offline recovery claimed an actor: %q", *actor)
	}
}
