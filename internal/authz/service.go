package authz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"brrpbrrp/internal/db"
)

// Service resolves authority from the store and enforces the invariants that
// keep the role model from ever escalating by accident.
type Service struct {
	store *db.Store
}

func NewService(store *db.Store) *Service { return &Service{store: store} }

// Resolve loads an account's authority: the role catalog, the roles assigned to
// the account and the ownership state, all at one revision. Callers inside a
// write transaction pass that transaction, so the decision sees the uncommitted
// change it is validating rather than a stale pre-transaction snapshot.
//
// The default role applies to every active account and is added implicitly when
// it is not assigned. It is not deletable and cannot be removed from an account,
// which is what keeps an account with no assignment from silently resolving
// with no authority at all.
func (s *Service) Resolve(ctx context.Context, q db.Queryer, account db.User) (Principal, error) {
	// Only the identifier is taken from the caller's value. Owner, administrator
	// and active state are re-read through q, so a request that was authorized
	// before a change cannot commit using authority the transaction no longer
	// observes.
	current, err := s.store.Account(ctx, q, account.ID)
	if err != nil {
		return Principal{}, err
	}
	catalog, err := s.store.Roles(ctx, q)
	if err != nil {
		return Principal{}, err
	}
	assigned, err := s.store.AssignedRoleIDs(ctx, q, current.ID)
	if err != nil {
		return Principal{}, err
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, q)
	if err != nil {
		return Principal{}, err
	}
	granted := make(map[string]bool, len(assigned))
	for _, roleID := range assigned {
		granted[roleID] = true
	}
	// A moderation timeout suspends the account without touching its roles, so it
	// cannot be a property of the role set: the roles are still there, and they must be
	// exactly what expiry restores. It is read here, in the one place every request,
	// delivery check and scoped query resolves its authority, so no path can answer
	// "may this account act" while ignoring it. The row's own expiry decides, not a
	// maintenance job having run.
	suspended := false
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_timeouts WHERE user_id=? AND expires_at>?)`, current.ID, db.Now()).Scan(&suspended); err != nil {
		return Principal{}, err
	}
	principal := Principal{
		UserID: current.ID, IsOwner: current.IsOwner, AdminBridge: current.IsAdmin,
		OwnerDesignated: owners > 0, OwnershipEstablished: established, Revision: revision,
		Inactive: current.IsBanned || current.IsDeactivated || suspended,
		Roles:    make([]Role, 0, len(catalog)),
	}
	for _, role := range catalog {
		if granted[role.ID] || role.IsDefault {
			principal.Roles = append(principal.Roles, RoleFrom(role))
		}
	}
	return principal, nil
}

// Rank reports the highest authority level an account holds, including the
// default role, so account management can apply the same "authority moves
// downward only" rule that role assignment uses.
func (s *Service) Rank(ctx context.Context, q db.Queryer, userID string) (int, error) {
	return s.store.HighestAuthorityPosition(ctx, q, userID)
}

// RoleByName returns a stored role by its stable key.
func (s *Service) RoleByName(ctx context.Context, q db.Queryer, key string) (db.Role, error) {
	roles, err := s.store.Roles(ctx, q)
	if err != nil {
		return db.Role{}, err
	}
	for _, role := range roles {
		if role.Key == key {
			return role, nil
		}
	}
	return db.Role{}, sql.ErrNoRows
}

// StrandedAuthority explains, in operator language, why a change would leave the
// installation without an owner or without any active administrator. An empty
// explanation means the change is safe.
func (s *Service) StrandedAuthority(ctx context.Context, q db.Queryer, excludeUserID string) (string, error) {
	var owners, remaining int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_owner=1`).Scan(&owners); err != nil {
		return "", err
	}
	if owners > 0 {
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_owner=1 AND id<>?`, excludeUserID).Scan(&remaining); err != nil {
			return "", err
		}
		if remaining == 0 {
			return "This would leave the installation with no owner. Transfer ownership first.", nil
		}
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_admin=1 AND is_banned=0 AND is_deactivated=0 AND id<>?`, excludeUserID).Scan(&remaining); err != nil {
		return "", err
	}
	if remaining == 0 {
		return "At least one active administrator must remain.", nil
	}
	return "", nil
}

// ChannelAccessSet converts stored grants into lookup sets.
type ChannelAccessSet struct {
	Roles map[string]bool
	Users map[string]bool
}

func AccessSet(access db.ChannelAccess) ChannelAccessSet {
	set := ChannelAccessSet{Roles: map[string]bool{}, Users: map[string]bool{}}
	for _, roleID := range access.Roles {
		set.Roles[roleID] = true
	}
	for _, userID := range access.Users {
		set.Users[userID] = true
	}
	return set
}

// EffectiveChannelMembers lists the active accounts that can currently read a
// channel, for the "who can access this?" inspector.
//
// The answer is computed by the same rule the delivery paths apply, account by
// account, rather than by a second SQL approximation of it. The previous version
// guessed with pattern matching over the stored permission list and ignored the
// default role, so it could both promise access the server refuses and hide
// access it grants.
func (s *Service) EffectiveChannelMembers(ctx context.Context, q db.Queryer, channel db.Channel, access db.ChannelAccess) ([]db.User, error) {
	authorities, err := s.store.AccountAuthorities(ctx, q)
	if err != nil {
		return nil, err
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, q)
	if err != nil {
		return nil, err
	}
	members := make([]db.User, 0, len(authorities))
	for _, authority := range authorities {
		if PrincipalFor(authority.User, authority.Roles, owners, established, revision).MayReadChannel(channel, access) {
			members = append(members, authority.User)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Username < members[j].Username })
	return members, nil
}

// PrincipalFor converts a stored account, its roles and the installation's
// ownership state into a policy principal. It is the one conversion used by
// requests, delivery, mention eligibility, the access inspector and scoped
// queries, so none of them can disagree about the rules.
func PrincipalFor(user db.User, roles []db.Role, owners int, established bool, revision int64) Principal {
	principal := Principal{
		UserID: user.ID, IsOwner: user.IsOwner, AdminBridge: user.IsAdmin,
		OwnerDesignated: owners > 0, OwnershipEstablished: established,
		Inactive: user.IsBanned || user.IsDeactivated, Revision: revision,
		Roles: make([]Role, 0, len(roles)),
	}
	for _, role := range roles {
		principal.Roles = append(principal.Roles, RoleFrom(role))
	}
	return principal
}

// GrantRole assigns a role inside the caller's transaction. It refuses
// escalation, self-elevation and any grant the actor does not hold.
func (s *Service) GrantRole(ctx context.Context, tx *sql.Tx, actor Principal, target db.User, role db.Role) (int64, error) {
	resolved := RoleFrom(role)
	if !actor.CanActOnRole(resolved) {
		return actor.Revision, fmt.Errorf("you cannot assign the %s role", role.Name)
	}
	if err := actor.CanGrantCapabilities(Capabilities(role.Permissions)); err != nil {
		return actor.Revision, err
	}
	if target.IsBanned || target.IsDeactivated {
		return actor.Revision, errors.New("that account is not active")
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO role_assignments(user_id,role_id,assigned_by,created_at) VALUES(?,?,?,?)`, target.ID, role.ID, actor.UserID, db.Now()); err != nil {
		return actor.Revision, err
	}
	// The legacy column and the administrator role must never disagree.
	if role.Key == "admin" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_admin=1, updated_at=? WHERE id=?`, db.Now(), target.ID); err != nil {
			return actor.Revision, err
		}
	}
	return db.BumpAuthorizationRevision(ctx, tx)
}

// RevokeRole removes a role assignment inside the caller's transaction and
// refuses to strand the installation.
func (s *Service) RevokeRole(ctx context.Context, tx *sql.Tx, actor Principal, target db.User, role db.Role) (int64, error) {
	resolved := RoleFrom(role)
	if role.ID == "" {
		return actor.Revision, errors.New("that role is not assigned to this account")
	}
	if !actor.CanActOnRole(resolved) {
		return actor.Revision, fmt.Errorf("you cannot change the %s role", role.Name)
	}
	if role.Key == "admin" && target.IsAdmin {
		explanation, err := s.StrandedAuthority(ctx, tx, target.ID)
		if err != nil {
			return actor.Revision, err
		}
		if explanation != "" {
			return actor.Revision, errors.New(explanation)
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, target.ID, role.ID)
	if err != nil {
		return actor.Revision, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return actor.Revision, errors.New("that role is not assigned to this account")
	}
	if role.Key == "admin" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_admin=0, updated_at=? WHERE id=?`, db.Now(), target.ID); err != nil {
			return actor.Revision, err
		}
	}
	return db.BumpAuthorizationRevision(ctx, tx)
}

// CreateRole adds a custom role strictly below the actor's own authority.
func (s *Service) CreateRole(ctx context.Context, tx *sql.Tx, actor Principal, name string, position int, permissions []Capability) (db.Role, int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return db.Role{}, actor.Revision, errors.New("use a role name of 1-60 characters")
	}
	if position < 1 || position >= actor.Position() {
		return db.Role{}, actor.Revision, errors.New("a role you create must sit below your own authority")
	}
	if err := actor.CanGrantCapabilities(permissions); err != nil {
		return db.Role{}, actor.Revision, err
	}
	key := roleKey(name)
	id, now := db.ID(), db.Now()
	encoded, err := encodePermissions(permissions)
	if err != nil {
		return db.Role{}, actor.Revision, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO roles(id,role_key,name,position,is_default,is_system,permissions,created_at,updated_at) VALUES(?,?,?,?,0,0,?,?,?)`, id, key, name, position, encoded, now, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return db.Role{}, actor.Revision, errors.New("a role with that name already exists")
		}
		return db.Role{}, actor.Revision, err
	}
	revision, err := db.BumpAuthorizationRevision(ctx, tx)
	return db.Role{ID: id, Key: key, Name: name, Position: position, Permissions: permissionNames(permissions)}, revision, err
}

// UpdateRole renames a custom role or replaces its grants. Built-in roles keep
// their name and position so an upgrade cannot silently move authority.
func (s *Service) UpdateRole(ctx context.Context, tx *sql.Tx, actor Principal, role db.Role, name string, position int, permissions []Capability) (int64, error) {
	resolved := RoleFrom(role)
	if !actor.CanActOnRole(resolved) {
		return actor.Revision, fmt.Errorf("you cannot edit the %s role", role.Name)
	}
	if position < 1 || position >= actor.Position() {
		return actor.Revision, errors.New("a role must sit below your own authority")
	}
	if err := actor.CanGrantCapabilities(permissions); err != nil {
		return actor.Revision, err
	}
	name = strings.TrimSpace(name)
	if role.IsSystem {
		name = role.Name
		position = role.Position
	} else if name == "" || len(name) > 60 {
		return actor.Revision, errors.New("use a role name of 1-60 characters")
	}
	encoded, err := encodePermissions(permissions)
	if err != nil {
		return actor.Revision, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET name=?,position=?,permissions=?,updated_at=? WHERE id=?`, name, position, encoded, db.Now(), role.ID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return actor.Revision, errors.New("a role with that name already exists")
		}
		return actor.Revision, err
	}
	return db.BumpAuthorizationRevision(ctx, tx)
}

// DeleteRole removes an unassigned custom role.
func (s *Service) DeleteRole(ctx context.Context, tx *sql.Tx, actor Principal, q db.Queryer, role db.Role) (int64, error) {
	holders, err := s.store.RoleHolders(ctx, q, role.ID)
	if err != nil {
		return actor.Revision, err
	}
	if !actor.CanDeleteRole(RoleFrom(role), len(holders)) {
		return actor.Revision, errors.New("that role cannot be deleted while it is built in, is the default role, or still has members")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id=?`, role.ID); err != nil {
		return actor.Revision, err
	}
	return db.BumpAuthorizationRevision(ctx, tx)
}

// DesignateOwner completes the ownership ceremony. Before an owner exists, an
// authenticated administrator may claim ownership; afterwards only the current
// owner may transfer it.
// RefuseOwnerChange explains why account management may not touch a designated
// owner. Ownership is changed by the ceremony, or offline with brrpbrrp
// recover-owner.
func RefuseOwnerChange(target db.User) error {
	if !target.IsOwner {
		return nil
	}
	return fmt.Errorf("ownership changes only through the ownership ceremony; transfer ownership first, or use brrpbrrp recover-owner on the server host")
}

func (s *Service) DesignateOwner(ctx context.Context, tx *sql.Tx, actor Principal, target db.User) (int64, error) {
	if actor.OwnerDesignated {
		if !actor.IsOwner {
			return actor.Revision, errors.New("only the owner can transfer ownership")
		}
		if !actor.Allowed(OperatorOwnershipTransfer) {
			return actor.Revision, errors.New("transferring ownership requires owner authority")
		}
	} else if !actor.AdminBridge {
		return actor.Revision, errors.New("an administrator must complete the first ownership ceremony")
	}
	if target.IsBanned || target.IsDeactivated {
		return actor.Revision, errors.New("that account is not active")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_owner=0 WHERE is_owner=1`); err != nil {
		return actor.Revision, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_owner=1, updated_at=? WHERE id=?`, db.Now(), target.ID); err != nil {
		return actor.Revision, err
	}
	// The ceremony is recorded durably and in the same transaction, so the
	// pre-ownership bootstrap state cannot be entered again by a later deletion.
	if err := db.MarkOwnershipEstablished(ctx, tx, target.ID); err != nil {
		return actor.Revision, err
	}
	return db.BumpAuthorizationRevision(ctx, tx)
}

func encodePermissions(capabilities []Capability) (string, error) {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if !Valid(capability) {
			return "", fmt.Errorf("%q is not a capability this server knows", capability)
		}
		names = append(names, string(capability))
	}
	encoded, err := json.Marshal(names)
	return string(encoded), err
}

func permissionNames(capabilities []Capability) []string {
	names := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		names = append(names, string(capability))
	}
	return names
}

func roleKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	trimmed := strings.Trim(builder.String(), "-")
	if trimmed == "" {
		trimmed = "role"
	}
	if len(trimmed) > 40 {
		trimmed = trimmed[:40]
	}
	return trimmed
}
