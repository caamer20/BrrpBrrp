package db

import "context"

// UserColumns is the canonical column list for reading an account. Every query
// that produces a db.User must select exactly these columns in this order.
const UserColumns = "id, username, display_name, is_admin, is_owner, is_banned, avatar_color, created_at, is_deactivated, is_deleted"

// QualifiedUserColumns is UserColumns with the users table named. Use it in any
// query that joins another table holding an id column, because an unqualified id
// would be ambiguous.
const QualifiedUserColumns = "users.id, users.username, users.display_name, users.is_admin, users.is_owner, users.is_banned, users.avatar_color, users.created_at, users.is_deactivated, users.is_deleted"

// ScanUser reads one account row selected with UserColumns.
func ScanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.IsAdmin, &user.IsOwner, &user.IsBanned, &user.AvatarColor, &user.CreatedAt, &user.IsDeactivated, &user.IsDeleted)
	return user, err
}

// Account reads one account by identifier. Authorization resolves an account's
// authority through the connection or transaction it is about to act with, so a
// copy read before the transaction cannot supply authority the commit no longer
// has.
func (s *Store) Account(ctx context.Context, q Queryer, id string) (User, error) {
	return ScanUser(q.QueryRowContext(ctx, `SELECT `+UserColumns+` FROM users WHERE id=?`, id))
}

// HighestAuthorityPosition returns the highest role position an account holds at
// the moment it is read, including the default role every active account holds
// implicitly. It is the account's rank for hierarchy decisions such as
// "may this actor manage that account".
func (s *Store) HighestAuthorityPosition(ctx context.Context, q Queryer, userID string) (int, error) {
	var position int
	err := q.QueryRowContext(ctx, `SELECT COALESCE(max(r.position),0) FROM roles r
        WHERE r.is_default=1 OR r.id IN (SELECT role_id FROM role_assignments WHERE user_id=?)`, userID).Scan(&position)
	return position, err
}
