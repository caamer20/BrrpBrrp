package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Session describes one of an account's own sessions. Token material is
// deliberately absent: the hash is never returned, logged or exported. Whether an
// authentication is still "recent" depends on a policy window, so it is judged by
// the caller rather than stored as a flag here.
type Session struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	LastSeen  string `json:"last_seen_at"`
	UserAgent string `json:"user_agent"`
	Current   bool   `json:"current"`
	// PrivilegedAt is the last strong authentication; empty means none recorded.
	PrivilegedAt string `json:"-"`
}

// Sessions lists an account's unexpired sessions, newest first, marking the
// caller's own session.
func (s *Store) Sessions(ctx context.Context, q Queryer, userID, currentHash string) ([]Session, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,created_at,expires_at,COALESCE(last_seen_at,''),COALESCE(user_agent,''),COALESCE(privileged_at,''),token_hash
        FROM sessions WHERE user_id=? AND expires_at>? ORDER BY created_at DESC, id`, userID, Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]Session, 0)
	for rows.Next() {
		var session Session
		var privileged, tokenHash string
		if err := rows.Scan(&session.ID, &session.CreatedAt, &session.ExpiresAt, &session.LastSeen, &session.UserAgent, &privileged, &tokenHash); err != nil {
			return nil, err
		}
		session.Current = currentHash != "" && tokenHash == currentHash
		session.PrivilegedAt = privileged
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// SessionTokenHash reads the hash of one of an account's own sessions. It exists
// so a revocation can close the sockets bound to that session without the caller
// ever learning the hash.
func (s *Store) SessionTokenHash(ctx context.Context, q Queryer, userID, sessionID string) (string, error) {
	var tokenHash string
	err := q.QueryRowContext(ctx, `SELECT token_hash FROM sessions WHERE id=? AND user_id=?`, sessionID, userID).Scan(&tokenHash)
	return tokenHash, err
}

// DeleteSession revokes one of an account's own sessions and reports the hash of
// the removed row so its sockets can be closed.
func (s *Store) DeleteSession(ctx context.Context, tx *sql.Tx, userID, sessionID string) (string, error) {
	var tokenHash string
	err := tx.QueryRowContext(ctx, `SELECT token_hash FROM sessions WHERE id=? AND user_id=?`, sessionID, userID).Scan(&tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=? AND user_id=?`, sessionID, userID); err != nil {
		return "", err
	}
	return tokenHash, nil
}

// DeleteOtherSessions revokes every session except the one being used, which is
// what "sign out my other devices" means.
func (s *Store) DeleteOtherSessions(ctx context.Context, tx *sql.Tx, userID, keepHash string) (int64, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, userID, keepHash)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// TouchSession records that a session was recently active.
func (s *Store) TouchSession(ctx context.Context, q Queryer, tokenHash string) {
	_, _ = q.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE token_hash=?`, Now(), tokenHash)
}

// MarkSessionPrivileged records a completed strong authentication on a session.
func MarkSessionPrivileged(ctx context.Context, tx *sql.Tx, tokenHash string) error {
	_, err := tx.ExecContext(ctx, `UPDATE sessions SET privileged_at=?,last_seen_at=? WHERE token_hash=?`, Now(), Now(), tokenHash)
	return err
}

// PrivilegedAt returns the last strong-authentication timestamp of a session.
func (s *Store) PrivilegedAt(ctx context.Context, q Queryer, tokenHash string) (string, error) {
	var privileged string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(privileged_at,'') FROM sessions WHERE token_hash=?`, tokenHash).Scan(&privileged)
	return privileged, err
}

// SanitizeUserAgent prepares a client description for storage. It is untrusted
// input shown only to the account that owns the session, so control characters are
// removed and the length is bounded; it is never used for authorization.
func SanitizeUserAgent(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	if len(value) > 200 {
		value = value[:200]
	}
	return value
}
