package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Queryer is satisfied by both *sql.DB and *sql.Tx, so authorized reads can run
// in the same transaction as the change they validate. The single-connection
// store deadlocks if a helper opens a second connection while a transaction
// holds the first, so every helper here accepts one explicitly.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Viewer is the identity a scoped query runs as. It is built from the policy
// engine's own decision, never re-derived here: CommunityAdmin is true only when
// the account holds channel.manage, Reads only when it holds channel.read, and
// RoleIDs is every role it holds, including the default role it holds without an
// assignment. The legacy users.is_admin column is not consulted, so a scoped read
// cannot disagree with a single-resource read.
type Viewer struct {
	UserID         string
	CommunityAdmin bool
	Reads          bool
	RoleIDs        []string
}

// accessibleChannelCondition renders the single authorization condition for
// community channel content, written against the alias c, together with the
// arguments it needs, in order. Every query that returns channel content, counts
// or metadata must include it before pagination, counting or limits.
//
// It mirrors Principal.MayReadChannel for every state a scoped read can be reached
// with: the account must hold channel.read, a public channel is readable by any such
// account, a community manager may read community channels, and otherwise an explicit
// member grant or a grant through one of the account's roles is required. The role set
// is passed in rather than re-derived from role_assignments because the default role is
// held without an assignment. A live moderation suspension is not repeated here because
// it cannot arrive: the middleware refuses community routes for a suspended account and
// the snapshot withholds the community-derived reads, so no scoped query is ever
// reached with it — while Principal.MayReadChannel still enforces it for every direct
// decision (review 17, V17-04).
func (v Viewer) accessibleChannelCondition() (string, []any) {
	truth := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	roles := "''"
	args := []any{truth(v.Reads), truth(v.CommunityAdmin)}
	if len(v.RoleIDs) > 0 {
		roles = strings.TrimSuffix(strings.Repeat("?,", len(v.RoleIDs)), ",")
		for _, roleID := range v.RoleIDs {
			args = append(args, roleID)
		}
	}
	args = append(args, v.UserID)
	return `(? AND (c.visibility='public' OR ? OR
    EXISTS (SELECT 1 FROM channel_role_access cra WHERE cra.channel_id=c.id AND cra.role_id IN (` + roles + `)) OR
    EXISTS (SELECT 1 FROM channel_user_access cua WHERE cua.channel_id=c.id AND cua.user_id=?)))`, args
}

// AccessibleChannelColumns is the SELECT list that pairs with
// AccessibleChannelCondition and satisfies ScanChannel.
const AccessibleChannelColumns = "c.id,c.name,c.type,c.category,c.position,c.topic,c.max_voice_users,c.visibility,c.posting,c.authorization_revision,c.slow_mode_seconds"

// ChannelColumns is the same list without the table alias, for reads that are
// not filtered by access.
const ChannelColumns = "id,name,type,category,position,topic,max_voice_users,visibility,posting,authorization_revision,slow_mode_seconds"

// ScanChannel reads a channel row selected with ChannelColumns or
// AccessibleChannelColumns.
func ScanChannel(row interface{ Scan(...any) error }) (Channel, error) {
	var channel Channel
	err := row.Scan(&channel.ID, &channel.Name, &channel.Type, &channel.Category, &channel.Position, &channel.Topic, &channel.MaxVoiceUsers, &channel.Visibility, &channel.Posting, &channel.AuthorizationRevision, &channel.SlowModeSeconds)
	return channel, err
}

// AccessibleChannels returns only the channels the viewer may read, in display
// order. A private title is private data, so it never appears in this result.
func (s *Store) AccessibleChannels(ctx context.Context, viewer Viewer, kind string) ([]Channel, error) {
	condition, args := viewer.accessibleChannelCondition()
	clause := "WHERE " + condition
	if kind != "" {
		clause += " AND c.type=?"
		args = append(args, kind)
	}
	rows, err := s.QueryContext(ctx, `SELECT `+AccessibleChannelColumns+` FROM channels c `+clause+` ORDER BY c.position,c.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	channels := make([]Channel, 0)
	for rows.Next() {
		channel, err := ScanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

// Channel reads one channel without filtering, for administrative paths that
// have already established authority. Content paths must use ChannelAccessible
// or AccessibleChannels instead.
func (s *Store) Channel(ctx context.Context, q Queryer, channelID string) (Channel, error) {
	return ScanChannel(q.QueryRowContext(ctx, `SELECT `+ChannelColumns+` FROM channels WHERE id=?`, channelID))
}

// ReadableChannelScope returns the WHERE clause that restricts content to the
// channels a viewer may read, with the arguments it needs. Search and any other
// query that reaches content through a channel identifier uses it, so a global
// search cannot be more permissive than a direct read.
func ReadableChannelScope(viewer Viewer) (string, []any) {
	condition, args := viewer.accessibleChannelCondition()
	return `WHERE channel_id IN (SELECT c.id FROM channels c WHERE ` + condition + `)`, args
}

// ViewerFor is the access identity for an account whose grants are already known
// to include channel.read: the caller's own reads and administrative reads.
func ViewerFor(userID string, communityAdmin bool) Viewer {
	return Viewer{UserID: userID, CommunityAdmin: communityAdmin, Reads: true}
}

// ViewerForPrincipal is the access identity for a request, built from the policy
// engine's resolved principal so the SQL predicate and the per-resource decision
// cannot drift apart.
func ViewerForPrincipal(userID string, communityAdmin, reads bool, roleIDs []string) Viewer {
	return Viewer{UserID: userID, CommunityAdmin: communityAdmin, Reads: reads, RoleIDs: roleIDs}
}

// ChannelAccessible reports whether a viewer may read one channel. A hidden
// channel is reported as absent, never as forbidden, so an outsider cannot
// confirm that a guessed identifier exists.
func (s *Store) ChannelAccessible(ctx context.Context, q Queryer, viewer Viewer, channelID string) (bool, error) {
	condition, args := viewer.accessibleChannelCondition()
	var exists int
	err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM channels c WHERE c.id=? AND `+condition+`)`,
		append([]any{channelID}, args...)...).Scan(&exists)
	return exists == 1, err
}

const roleColumns = "id,role_key,name,position,is_default,is_system,permissions"

// qualifiedRoleColumns names the roles table so a joined query cannot confuse its
// id with another table's.
const qualifiedRoleColumns = "roles.id, roles.role_key, roles.name, roles.position, roles.is_default, roles.is_system, roles.permissions"

func scanRole(row interface{ Scan(...any) error }) (Role, error) {
	var role Role
	var permissions string
	if err := row.Scan(&role.ID, &role.Key, &role.Name, &role.Position, &role.IsDefault, &role.IsSystem, &permissions); err != nil {
		return Role{}, err
	}
	if strings.TrimSpace(permissions) != "" {
		if err := json.Unmarshal([]byte(permissions), &role.Permissions); err != nil {
			return Role{}, fmt.Errorf("role %s has unreadable permissions: %w", role.ID, err)
		}
	}
	if role.Permissions == nil {
		role.Permissions = []string{}
	}
	return role, nil
}

// Roles lists roles in authority order with their current member counts.
func (s *Store) Roles(ctx context.Context, q Queryer) ([]Role, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+roleColumns+`,
        (SELECT count(*) FROM role_assignments ra WHERE ra.role_id=roles.id)
        FROM roles ORDER BY position DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]Role, 0)
	for rows.Next() {
		var role Role
		var permissions string
		if err := rows.Scan(&role.ID, &role.Key, &role.Name, &role.Position, &role.IsDefault, &role.IsSystem, &permissions, &role.Members); err != nil {
			return nil, err
		}
		if strings.TrimSpace(permissions) != "" {
			if err := json.Unmarshal([]byte(permissions), &role.Permissions); err != nil {
				return nil, err
			}
		}
		if role.Permissions == nil {
			role.Permissions = []string{}
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *Store) Role(ctx context.Context, q Queryer, id string) (Role, error) {
	role, err := scanRole(q.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM roles WHERE id=?`, id))
	role.Members = 0
	return role, err
}

// AssignedRoles returns the roles granted to one account.
func (s *Store) AssignedRoles(ctx context.Context, q Queryer, userID string) ([]Role, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+roleColumns+` FROM roles JOIN role_assignments ra ON ra.role_id=roles.id
        WHERE ra.user_id=? ORDER BY position DESC, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]Role, 0)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// AssignedRoleIDs is the set form used by authorization checks.
func (s *Store) AssignedRoleIDs(ctx context.Context, q Queryer, userID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT role_id FROM role_assignments WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AccountAuthority is one active account with the roles it holds. Delivery and
// audience decisions use it so they evaluate the same rules as a request
// instead of duplicating permission logic in SQL.
type AccountAuthority struct {
	User  User
	Roles []Role
	// Suspended is set while a moderation timeout is live for the account. It lives
	// here because the revalidation passes — voice enforcement, stream revocation —
	// build principals in bulk, and a bulk principal that omits suspension answers a
	// different question than a request would: a timed-out account kept speaking in a
	// community call and kept streaming community content because nobody had asked
	// "is this account suspended" on those paths (review 17, V17-04).
	Suspended bool
}

// AccountAuthorities returns every active account with its roles.
func (s *Store) AccountAuthorities(ctx context.Context, q Queryer) ([]AccountAuthority, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+UserColumns+` FROM users WHERE is_banned=0 AND is_deactivated=0 ORDER BY username`)
	if err != nil {
		return nil, err
	}
	accounts := make([]AccountAuthority, 0)
	index := make(map[string]int)
	for rows.Next() {
		user, err := ScanUser(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[user.ID] = len(accounts)
		accounts = append(accounts, AccountAuthority{User: user, Roles: []Role{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return accounts, nil
	}
	// Live suspensions, read in the same pass. The row's own expiry decides — the same
	// rule `authz.Resolve` applies — so a lapsed timeout is not a suspension here either,
	// and the two paths cannot disagree about who is suspended.
	suspended, err := q.QueryContext(ctx, `SELECT user_id FROM account_timeouts WHERE expires_at>?`, Now())
	if err != nil {
		return nil, err
	}
	for suspended.Next() {
		var userID string
		if err := suspended.Scan(&userID); err != nil {
			suspended.Close()
			return nil, err
		}
		if at, ok := index[userID]; ok {
			accounts[at].Suspended = true
		}
	}
	err = suspended.Err()
	suspended.Close()
	if err != nil {
		return nil, err
	}
	// The default role is the floor an account has until it is assigned something else,
	// and it applies only then. Appending it unconditionally made it a grant on top of
	// assignments, which meant an account whose roles had been set to exactly one
	// listen-only role still resolved with the member role's full capability set — the
	// restricted roles the product offers could not restrict anything.
	//
	// The floor is the same rule `authz.Resolve` applies, and it has to be: these are two
	// paths to the same answer, and they disagreed the moment one of them was corrected.
	// An account with no assignment row at all — created outside the administrator's
	// route — still gets the default here.
	defaultRole, err := s.DefaultRole(ctx, q)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		hasAssignment := make([]bool, len(accounts))
		rows, err := q.QueryContext(ctx, `SELECT DISTINCT user_id FROM role_assignments`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return nil, err
			}
			if at, ok := index[userID]; ok {
				hasAssignment[at] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for i := range accounts {
			if !hasAssignment[i] {
				accounts[i].Roles = append(accounts[i].Roles, defaultRole)
			}
		}
	}
	assignments, err := q.QueryContext(ctx, `SELECT ra.user_id, `+qualifiedRoleColumns+` FROM role_assignments ra JOIN roles ON roles.id=ra.role_id ORDER BY roles.position DESC`)
	if err != nil {
		return nil, err
	}
	defer assignments.Close()
	for assignments.Next() {
		var userID string
		var role Role
		var permissions string
		if err := assignments.Scan(&userID, &role.ID, &role.Key, &role.Name, &role.Position, &role.IsDefault, &role.IsSystem, &permissions); err != nil {
			return nil, err
		}
		if strings.TrimSpace(permissions) != "" {
			if err := json.Unmarshal([]byte(permissions), &role.Permissions); err != nil {
				return nil, err
			}
		}
		if role.Permissions == nil {
			role.Permissions = []string{}
		}
		at, ok := index[userID]
		if !ok {
			continue
		}
		// Never add the same role twice when it is also the default.
		for _, existing := range accounts[at].Roles {
			if existing.ID == role.ID {
				role.ID = ""
				break
			}
		}
		if role.ID != "" {
			accounts[at].Roles = append(accounts[at].Roles, role)
		}
	}
	return accounts, assignments.Err()
}

// InvitationRoles returns the roles an invitation will grant on acceptance, in
// authority order.
func (s *Store) InvitationRoles(ctx context.Context, q Queryer, invitationID string) ([]Role, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+qualifiedRoleColumns+` FROM invitation_roles ir JOIN roles ON roles.id=ir.role_id
        WHERE ir.invitation_id=? ORDER BY roles.position DESC, roles.name`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]Role, 0)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// EffectiveRoleHolders lists the accounts a role applies to as the authorization
// engine resolves them, not just the ones with an assignment row.
//
// The default role is held by every account without an assignment, so a change to
// that role changes what those accounts may read. Enumerating explicit assignments
// alone leaves the accounts that hold it implicitly out of stream cancellation and
// out of the refetch signal, and a registered download then keeps streaming under
// authority that was withdrawn. Every other role is held only through an
// assignment, so its explicit holders are its effective holders.
func (s *Store) EffectiveRoleHolders(ctx context.Context, q Queryer, role Role) ([]User, error) {
	if !role.IsDefault {
		return s.RoleHolders(ctx, q, role.ID)
	}
	rows, err := q.QueryContext(ctx, `SELECT `+UserColumns+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := ScanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// RoleHolders lists the active accounts holding a role.
func (s *Store) RoleHolders(ctx context.Context, q Queryer, roleID string) ([]User, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+UserColumns+` FROM users WHERE id IN (SELECT user_id FROM role_assignments WHERE role_id=?) ORDER BY username`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := ScanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// Built-in role identifiers. Role membership is created together with the
// account, never lazily: an account with no assignment would resolve with no
// authority at all, so every path that inserts a user assigns a role in the
// same transaction.
const (
	AdministratorRoleID = "role-admin"
	MemberRoleID        = "role-member"
)

// AssignAccountRole grants one built-in role to an account inside the caller's
// transaction.
func AssignAccountRole(ctx context.Context, tx *sql.Tx, userID, roleID string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, userID, roleID, Now())
	return err
}

// OwnerCount reports how many owners exist. Zero means no owner is designated
// now; OwnershipEstablished decides whether that is still the pre-ceremony
// bootstrap state or a stranded installation.
func (s *Store) OwnerCount(ctx context.Context, q Queryer) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_owner=1`).Scan(&count)
	return count, err
}

// OwnershipEstablishedKey is the durable record that this installation's
// ownership ceremony has completed at least once.
//
// The compatibility bridge that keeps pre-ownership administrators on their
// operator powers applies only while ownership has never been established. A
// count of current owners is not enough to decide that: an installation whose
// owner record was removed would look identical to a fresh one and would hand
// the operator tier back to whoever happens to be an administrator.
const OwnershipEstablishedKey = "ownership-established"

// OwnershipEstablished reports whether the ceremony has ever completed, either
// because the durable record exists or because an owner is designated now (an
// installation upgraded from a release that predates the record).
func (s *Store) OwnershipEstablished(ctx context.Context, q Queryer) (bool, error) {
	var established bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM settings WHERE key=?) OR EXISTS(SELECT 1 FROM users WHERE is_owner=1)`, OwnershipEstablishedKey).Scan(&established)
	return established, err
}

// MarkOwnershipEstablished records the ceremony inside the transaction that
// designates the owner, so the bootstrap state can never be re-entered by a
// later deletion and the record can never exist without the ownership change it
// describes.
func MarkOwnershipEstablished(ctx context.Context, tx *sql.Tx, userID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, OwnershipEstablishedKey, userID, Now())
	return err
}

// AuthorizationSnapshot returns the number of designated owners, whether
// ownership has ever been established, and the current authorization revision in
// one round trip. Policy evaluation calls it for every request and inside
// sensitive transactions, so it stays a single query.
func (s *Store) AuthorizationSnapshot(ctx context.Context, q Queryer) (int, bool, int64, error) {
	var owners int
	var established bool
	var revision int64
	err := q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM users WHERE is_owner=1),
        (EXISTS(SELECT 1 FROM settings WHERE key=?) OR EXISTS(SELECT 1 FROM users WHERE is_owner=1)),
        (SELECT revision FROM authorization_state WHERE id=1)`, OwnershipEstablishedKey).Scan(&owners, &established, &revision)
	return owners, established, revision, err
}

// DefaultRole returns the role every account holds even without an explicit
// assignment.
func (s *Store) DefaultRole(ctx context.Context, q Queryer) (Role, error) {
	return scanRole(q.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM roles WHERE is_default=1 LIMIT 1`))
}

// AuthorizationRevision is the committed linearization point for admitting new
// reads, streams and subscriptions.
func (s *Store) AuthorizationRevision(ctx context.Context, q Queryer) (int64, error) {
	var revision int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM authorization_state WHERE id=1`).Scan(&revision)
	return revision, err
}

// BumpAuthorizationRevision advances the revision inside the caller's write
// transaction, so a change is never acknowledged before it is the admitted
// policy.
func BumpAuthorizationRevision(ctx context.Context, tx *sql.Tx) (int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE authorization_state SET revision=revision+1, updated_at=? WHERE id=1`, Now()); err != nil {
		return 0, err
	}
	return (&Store{}).revisionInTx(ctx, tx)
}

func (s *Store) revisionInTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM authorization_state WHERE id=1`).Scan(&revision)
	return revision, err
}

// Categories lists durable categories with the number of channels the viewer may
// read in each one.
//
// Category names are shared workspace structure, and that disclosure is recorded
// and deliberate. Channel counts are not: a count that included a channel the
// viewer cannot open would report the existence of restricted conversations, so
// the count is scoped with the same condition the channel list uses.
func (s *Store) Categories(ctx context.Context, q Queryer, viewer Viewer) ([]Category, error) {
	condition, args := viewer.accessibleChannelCondition()
	rows, err := q.QueryContext(ctx, `SELECT categories.id, categories.name, categories.position,
        (SELECT count(*) FROM channels c WHERE trim(c.category)=categories.name AND `+condition+`)
        FROM categories ORDER BY categories.position, categories.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.ID, &category.Name, &category.Position, &category.Channels); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

var CategoryNameInvalid = errors.New("category name must be 1-60 characters")

// NormalizeCategoryName validates and trims a category name.
func NormalizeCategoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return "", CategoryNameInvalid
	}
	return name, nil
}

// ChannelAccess is the explicit access list of one private channel.
type ChannelAccess struct {
	ChannelID  string   `json:"channel_id"`
	Visibility string   `json:"visibility"`
	Roles      []string `json:"roles"`
	Users      []string `json:"users"`
}

// ChannelAccessLists returns the explicit grants for the given channels. Public
// channels carry no grants, because every member may read them.
func (s *Store) ChannelAccessLists(ctx context.Context, q Queryer, channelIDs ...string) (map[string]ChannelAccess, error) {
	lists := make(map[string]ChannelAccess, len(channelIDs))
	if len(channelIDs) == 0 {
		return lists, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(channelIDs)), ",")
	args := make([]any, 0, len(channelIDs))
	for _, id := range channelIDs {
		args = append(args, id)
		lists[id] = ChannelAccess{ChannelID: id, Roles: []string{}, Users: []string{}}
	}
	rows, err := q.QueryContext(ctx, `SELECT channel_id, role_id FROM channel_role_access WHERE channel_id IN (`+placeholders+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var channelID, roleID string
		if err := rows.Scan(&channelID, &roleID); err != nil {
			rows.Close()
			return nil, err
		}
		entry := lists[channelID]
		entry.Roles = append(entry.Roles, roleID)
		lists[channelID] = entry
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT channel_id, user_id FROM channel_user_access WHERE channel_id IN (`+placeholders+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var channelID, userID string
		if err := rows.Scan(&channelID, &userID); err != nil {
			return nil, err
		}
		entry := lists[channelID]
		entry.Users = append(entry.Users, userID)
		lists[channelID] = entry
	}
	return lists, rows.Err()
}

// SetChannelAccess replaces a channel's explicit grants in the caller's
// transaction and bumps the channel's authorization revision with it.
func SetChannelAccess(ctx context.Context, tx *sql.Tx, channelID string, roleIDs, userIDs []string, grantedBy string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_role_access WHERE channel_id=?`, channelID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_user_access WHERE channel_id=?`, channelID); err != nil {
		return err
	}
	now := Now()
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO channel_role_access(channel_id,role_id,granted_by,created_at) VALUES(?,?,?,?)`, channelID, roleID, nullify(grantedBy), now); err != nil {
			return err
		}
	}
	for _, userID := range userIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO channel_user_access(channel_id,user_id,granted_by,created_at) VALUES(?,?,?,?)`, channelID, userID, nullify(grantedBy), now); err != nil {
			return err
		}
	}
	return bumpChannelRevision(ctx, tx, channelID)
}

func bumpChannelRevision(ctx context.Context, tx *sql.Tx, channelID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE channels SET authorization_revision=authorization_revision+1 WHERE id=?`, channelID)
	return err
}

// ChannelRevision reads one channel's revision, used to reject stale events.
func (s *Store) ChannelRevision(ctx context.Context, q Queryer, channelID string) (int, error) {
	var revision int
	err := q.QueryRowContext(ctx, `SELECT authorization_revision FROM channels WHERE id=?`, channelID).Scan(&revision)
	return revision, err
}

func nullify(value string) any {
	if value == "" {
		return nil
	}
	return value
}
