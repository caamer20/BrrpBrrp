package authz

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/db"
)

func role(id, key string, position int, capabilities ...Capability) Role {
	permissions := map[Capability]bool{}
	for _, capability := range capabilities {
		permissions[capability] = true
	}
	return Role{ID: id, Key: key, Name: key, Position: position, Permissions: permissions}
}

func admin() Principal {
	return Principal{UserID: "u-admin", AdminBridge: true, Roles: []Role{role("role-admin", "admin", AdministrativePosition, ChannelRead, MessageSend, ChannelManage, RoleManage, MemberManage)}}
}

func member() Principal {
	return Principal{UserID: "u-member", Roles: []Role{role("role-member", "member", 10, ChannelRead, MessageSend, MessageReact, MessageUpload, MessagePin, VoiceConnect, VoiceSpeak, VoiceShareScreen)}}
}

func TestCatalogIsCompleteAndTyped(t *testing.T) {
	if len(Catalog) != len(catalogSet) {
		t.Fatalf("catalog has %d entries but %d are addressable", len(Catalog), len(catalogSet))
	}
	for _, capability := range Catalog {
		if !Valid(capability) {
			t.Fatalf("%s is listed but not valid", capability)
		}
		if IsOperator(capability) && len(capability) < len("operator.") {
			t.Fatalf("operator capability %s is malformed", capability)
		}
	}
	if Valid("channel.explode") {
		t.Fatal("an unknown capability was accepted")
	}
	if Valid("") {
		t.Fatal("the empty capability was accepted")
	}
}

func TestCapabilityEvaluation(t *testing.T) {
	ownerless := admin()
	granted := member()
	for _, tc := range []struct {
		name       string
		principal  Principal
		capability Capability
		want       bool
	}{
		{"member reads channels", granted, ChannelRead, true},
		{"member cannot manage channels", granted, ChannelManage, false},
		{"member cannot delete others' messages", granted, MessageDeleteAny, false},
		{"administrator manages channels", ownerless, ChannelManage, true},
		{"administrator keeps operator powers before ownership exists", ownerless, OperatorBackup, true},
		{"administrator cannot name-check an unknown capability", ownerless, Capability("root.everything"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.principal.Allowed(tc.capability); got != tc.want {
				t.Fatalf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
	// Designating an owner removes the compatibility bridge: the delegated
	// administrator is no longer able to export the whole server.
	designated := ownerless
	designated.OwnerDesignated = true
	designated.OwnershipEstablished = true
	if designated.Allowed(OperatorBackup) {
		t.Fatal("ownerless bridge survived ownership designation")
	}
	// The bridge is durable, not a function of the current owner count: an
	// installation whose owner record is removed must not hand the operator tier
	// back to whoever is an administrator. Completing the ceremony stays
	// reachable, because that is how such an installation is repaired.
	stranded := ownerless
	stranded.OwnershipEstablished = true
	if stranded.Allowed(OperatorBackup) || stranded.Allowed(OperatorCredentialRecovery) {
		t.Fatal("removing the owner recreated operator authority")
	}
	if !stranded.Allowed(OperatorOwnershipTransfer) {
		t.Fatal("a stranded installation cannot complete the ownership ceremony")
	}
	owner := Principal{UserID: "u-owner", IsOwner: true, OwnerDesignated: true}
	for _, capability := range Catalog {
		if !owner.Allowed(capability) {
			t.Fatalf("owner lacks %s", capability)
		}
	}
	if owner.Position() != OwnerPosition {
		t.Fatalf("owner position = %d", owner.Position())
	}
	if len(member().Capabilities()) != 8 {
		t.Fatalf("member capabilities = %v", member().Capabilities())
	}
}

func TestChannelAccessRules(t *testing.T) {
	public := db.Channel{ID: "c-public", Visibility: "public", Posting: "members"}
	private := db.Channel{ID: "c-private", Visibility: "private", Posting: "members"}
	announcement := db.Channel{ID: "c-news", Visibility: "public", Posting: "announcement"}
	empty := db.ChannelAccess{}
	memberGranted := db.ChannelAccess{Users: []string{"u-member"}}
	roleGranted := db.ChannelAccess{Roles: []string{"role-moderator"}}

	for _, tc := range []struct {
		name      string
		principal Principal
		channel   db.Channel
		access    db.ChannelAccess
		read      bool
		post      bool
	}{
		{"member reads a public channel", member(), public, empty, true, true},
		{"member cannot read an unshared private channel", member(), private, empty, false, false},
		{"explicit member grant opens a private channel", member(), private, memberGranted, true, true},
		{"administrator sees community channels", admin(), private, empty, true, true},
		{"a grant through another role does not leak", member(), private, roleGranted, false, false},
		{"announcement channels stay readable but not postable", member(), announcement, empty, true, false},
		{"announcement posting follows channel.manage", admin(), announcement, empty, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.principal.MayReadChannel(tc.channel, tc.access); got != tc.read {
				t.Fatalf("read = %v, want %v", got, tc.read)
			}
			if got := tc.principal.MayPostChannel(tc.channel, tc.access); got != tc.post {
				t.Fatalf("post = %v, want %v", got, tc.post)
			}
		})
	}

	// A role holder reaches the channel through that role only.
	moderator := Principal{UserID: "u-mod", Roles: []Role{role("role-moderator", "moderator", 60, ChannelRead, MessageSend)}}
	if !moderator.MayReadChannel(private, roleGranted) {
		t.Fatal("role grant did not open the channel for its holder")
	}
	if moderator.MayReadChannel(private, db.ChannelAccess{Users: []string{"u-member"}}) {
		t.Fatal("a member grant leaked to another account")
	}
	// Revocation is immediate: the same principal without the grant loses access.
	if moderator.MayReadChannel(private, empty) {
		t.Fatal("private channel stayed readable without a grant")
	}
}

func TestHierarchyAndDelegation(t *testing.T) {
	owner := Principal{UserID: "u-owner", IsOwner: true, OwnerDesignated: true}
	administrator := Principal{UserID: "u-admin", OwnerDesignated: true, Roles: []Role{role("role-admin", "admin", AdministrativePosition, ChannelRead, ChannelManage, RoleManage, MemberManage, AuditRead)}}
	moderator := Principal{UserID: "u-mod", OwnerDesignated: true, Roles: []Role{role("role-moderator", "moderator", 60, ChannelRead, ChannelManage, RoleManage, MemberManage)}}
	peer := Principal{UserID: "u-peer", OwnerDesignated: true, Roles: []Role{role("role-admin", "admin", AdministrativePosition, ChannelRead, ChannelManage, RoleManage)}}
	moderatorRoles := Principal{UserID: "u-mod", OwnerDesignated: true, Roles: []Role{role("role-moderator", "moderator", 60, ChannelRead, ChannelManage)}}
	custom := role("role-news", "news", 30, ChannelRead, MessageSend)
	equalRole := role("role-equal", "equal", AdministrativePosition, ChannelRead)
	aboveRole := role("role-above", "above", 90, ChannelRead)

	for _, tc := range []struct {
		name      string
		principal Principal
		role      Role
		want      bool
	}{
		{"owner manages anything", owner, aboveRole, true},
		{"administrator manages lower roles", administrator, custom, true},
		{"administrator cannot manage peers", peer, equalRole, false},
		{"administrator cannot manage above itself", administrator, role("role-above", "above", 90, ChannelRead), false},
		{"moderator without role.manage is refused", moderatorRoles, custom, false},
		{"a moderate role manager is refused above its position", moderator, aboveRole, false},
		{"a plain member manages nothing", member(), custom, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.principal.CanActOnRole(tc.role); got != tc.want {
				t.Fatalf("can act on role = %v, want %v", got, tc.want)
			}
		})
	}

	defaultRole := role("role-member", "member", 10, ChannelRead)
	defaultRole.IsDefault = true
	if owner.CanDeleteRole(defaultRole, 0) {
		t.Fatal("the default role was deletable")
	}
	builtIn := role("role-admin", "admin", AdministrativePosition, ChannelRead)
	builtIn.IsSystem = true
	if owner.CanDeleteRole(builtIn, 0) {
		t.Fatal("a built-in role was deletable")
	}
	if administrator.CanDeleteRole(custom, 2) {
		t.Fatal("a role with members was deletable")
	}
	if !administrator.CanDeleteRole(custom, 0) {
		t.Fatal("an empty lower role was not deletable")
	}
}

func TestCapabilityDelegationNeverEscalates(t *testing.T) {
	administrator := Principal{UserID: "u-admin", OwnerDesignated: true, Roles: []Role{role("role-admin", "admin", AdministrativePosition, ChannelRead, ChannelManage, RoleManage)}}
	for _, tc := range []struct {
		name         string
		capabilities []Capability
		wantError    bool
	}{
		{"delegating a held capability", []Capability{ChannelRead}, false},
		{"delegating an unheld capability", []Capability{SettingsManage}, true},
		{"delegating the operator tier", []Capability{OperatorCredentialRecovery}, true},
		{"delegating an unknown name", []Capability{Capability("channel.god")}, true},
		{"delegating nothing", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := administrator.CanGrantCapabilities(tc.capabilities)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v", err, tc.wantError)
			}
		})
	}
	owner := Principal{UserID: "u-owner", IsOwner: true, OwnerDesignated: true}
	if err := owner.CanGrantCapabilities([]Capability{OperatorBackup}); err != nil {
		t.Fatalf("owner could not grant the operator tier: %v", err)
	}
}

func TestDenialClassification(t *testing.T) {
	hidden := Denial{Hidden: true, Reason: "This conversation is not available."}
	forbidden := Denial{Reason: "You cannot edit that message."}
	if !errors.Is(hidden, ErrUnavailable) {
		t.Fatal("hidden denial is not classified as unavailable")
	}
	if errors.Is(hidden, ErrForbidden) {
		t.Fatal("hidden denial leaks a forbidden result")
	}
	if !errors.Is(forbidden, ErrForbidden) || errors.Is(forbidden, ErrUnavailable) {
		t.Fatal("visible denial is misclassified")
	}
}

// TestSeededRolesAreUnderstood proves the migrated presets only reference
// capabilities this build implements, so an upgrade cannot leave a role holding
// a permission nothing enforces.
func TestSeededRolesAreUnderstood(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	roles, err := store.Roles(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 3 {
		t.Fatalf("seeded roles = %d", len(roles))
	}
	defaults := 0
	for _, role := range roles {
		if role.IsDefault {
			defaults++
		}
		for _, permission := range role.Permissions {
			if !Valid(Capability(permission)) {
				t.Fatalf("role %s holds unknown capability %s", role.Key, permission)
			}
			if IsOperator(Capability(permission)) {
				t.Fatalf("seeded role %s holds operator capability %s", role.Key, permission)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("default roles = %d, want exactly one", defaults)
	}
	// Every account-creation path resolves authority without a repair step.
	service := NewService(store)
	if err := store.CreateAdministrator(context.Background(), "alex", "a-long-enough-password", 1<<20); err != nil {
		t.Fatal(err)
	}
	administrator, err := dbScanUser(store, "alex")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.Resolve(context.Background(), store, administrator)
	if err != nil {
		t.Fatal(err)
	}
	if principal.IsOwner {
		t.Fatal("ownership was inferred for an administrator")
	}
	if !principal.AdminBridge {
		t.Fatalf("resolved state = %+v", principal)
	}
	if !principal.Allowed(ChannelManage) {
		t.Fatal("offline-created administrator did not resolve channel management")
	}
	// Before the ownership ceremony the compatibility bridge is active, so the
	// upgraded installation behaves exactly as it did before roles existed.
	if principal.OwnerDesignated {
		t.Fatal("ownership was assumed on a fresh installation")
	}
	if !principal.Allowed(OperatorBackup) {
		t.Fatal("ownerless administrator lost its existing operator powers")
	}
	// The ceremony designates exactly one owner and closes the bridge for every
	// delegated administrator.
	if err := store.CreateAdministrator(context.Background(), "sam", "another-long-password", 1<<20); err != nil {
		t.Fatal(err)
	}
	sam, err := dbScanUser(store, "sam")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DesignateOwner(context.Background(), tx, principal, sam); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	sam, err = dbScanUser(store, "sam")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.Resolve(context.Background(), store, sam)
	if err != nil {
		t.Fatal(err)
	}
	if !owner.IsOwner || !owner.OwnerDesignated {
		t.Fatalf("designated owner resolved as %+v", owner)
	}
	if owner.Revision <= principal.Revision {
		t.Fatal("ownership change did not advance the authorization revision")
	}
	delegated, err := service.Resolve(context.Background(), store, administrator)
	if err != nil {
		t.Fatal(err)
	}
	if delegated.IsOwner || delegated.Allowed(OperatorBackup) || !delegated.Allowed(ChannelManage) {
		t.Fatalf("delegated administrator resolved as %+v", delegated)
	}
}

func dbScanUser(store *db.Store, username string) (db.User, error) {
	return db.ScanUser(store.QueryRow(`SELECT `+db.UserColumns+` FROM users WHERE username=?`, username))
}
