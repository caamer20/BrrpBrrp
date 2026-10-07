package db

import (
	"brrpbrrp/internal/limits"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"regexp"
	"strings"
)

// CreateAdministrator is the offline recovery path for a machine's operator.
// Existing usernames and passwords are never changed by this command.
func (s *Store) CreateAdministrator(ctx context.Context, username, password string, maxUpload int64) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`).MatchString(username) || strings.EqualFold(username, "everyone") || len(password) < 12 || len(password) > 72 {
		return errors.New("use a 3–32 character username and 12–72 byte password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Removed identities no longer consume capacity. Initialization must still
	// consider every retained identity: tombstones do not make an old community
	// a new installation or authorize reseeding its channels.
	var count, identities int
	if err = tx.QueryRow(`SELECT (SELECT count(*) FROM users WHERE is_deleted=0),(SELECT count(*) FROM users)`).Scan(&count, &identities); err != nil {
		return err
	}
	if count >= limits.Accounts {
		return errors.New("account limit reached")
	}
	id, now := ID(), Now()
	if _, err = tx.Exec(`INSERT INTO users(id,username,display_name,password_hash,is_admin,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`, id, strings.ToLower(username), username, string(hash), now, now); err != nil {
		return errors.New("could not create account; the username may already exist")
	}
	// The account receives the administrator role in the same transaction, so an
	// offline-created operator is never left without resolved authority.
	if err = AssignAccountRole(ctx, tx, id, AdministratorRoleID); err != nil {
		return err
	}
	var initialized int
	if err = tx.QueryRow(`SELECT count(*) FROM settings WHERE key='setup_complete'`).Scan(&initialized); err != nil {
		return err
	}
	if initialized == 0 && identities == 0 {
		for _, channel := range []struct{ name, kind string }{{"general", "text"}, {"Lounge", "voice"}} {
			if _, err = tx.Exec(`INSERT INTO channels(id,name,type,created_at,updated_at) VALUES(?,?,?,?,?)`, ID(), channel.name, channel.kind, now, now); err != nil {
				return err
			}
		}
	}
	// INSERT OR IGNORE: an installation that already has a settings row keeps it, so this
	// value reaches only a database that is being set up offline for the first time.
	settings := NewInstallationSettings()
	settings.MaxUploadBytes = maxUpload
	encoded, _ := json.Marshal(settings)
	if _, err = tx.Exec(`INSERT OR IGNORE INTO settings(key,value,updated_at) VALUES('setup_complete','true',?),('public',?,?)`, now, string(encoded), now); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,?,'user.created_offline',?,?)`, ID(), id, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

// OwnerRecovery reports exactly what an offline recovery changed, so the operator
// can read and record it instead of inferring it from a silent success.
type OwnerRecovery struct {
	Username string
	// FactorRemoved reports that the account's enrolled second factor was
	// deleted. It is the part of recovery that cannot be undone by signing in.
	FactorRemoved bool
	// CodesRemoved counts the single-use recovery codes that went with it.
	CodesRemoved int64
	// SessionsEnded counts the signed-in devices that were revoked.
	SessionsEnded int64
	// Reactivated and BanCleared report moderation state this command had to
	// undo, because a deactivated or banned account cannot sign in either.
	Reactivated bool
	BanCleared  bool
}

// RecoverOwner is the offline break-glass path for an installation whose owner
// can no longer authenticate: the authenticator is gone, the recovery codes are
// gone, and no other account holds the operator tier, so nothing inside the
// running application can help. It runs only while the service is stopped (the
// caller holds the data lock) and only against the account that is currently the
// owner.
//
// It replaces the password, removes the enrolled factor and its recovery codes,
// ends every session and push subscription, and lifts a deactivation or ban that
// would otherwise still refuse the sign-in. It deliberately does not touch role
// assignments, ownership, messages or files: recovery restores the owner's own
// credentials, not their authority over anyone else. The whole change is one
// transaction, and it records one audit row attributed to no account, because the
// person acting is whoever holds the host, not an authenticated user.
func (s *Store) RecoverOwner(ctx context.Context, username, password string) (OwnerRecovery, error) {
	var recovery OwnerRecovery
	if len(password) < 12 || len(password) > 72 || strings.TrimSpace(password) == "" {
		return recovery, errors.New("choose a 12–72 byte password")
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return recovery, errors.New("recover-owner requires --username naming the installation owner")
	}
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return recovery, err
	}
	defer tx.Rollback()
	var id string
	var deactivated, banned bool
	err = tx.QueryRowContext(ctx, `SELECT id,is_deactivated,is_banned FROM users WHERE username=? AND is_owner=1`, username).Scan(&id, &deactivated, &banned)
	if errors.Is(err, sql.ErrNoRows) {
		var owners, named int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM users WHERE is_owner=1), (SELECT count(*) FROM users WHERE username=?)`, username).Scan(&owners, &named); err != nil {
			return recovery, err
		}
		switch {
		case owners == 0:
			return recovery, errors.New("this installation has no owner yet; run brrpbrrp create-admin to restore administrative access, then complete ownership in the web interface")
		case named == 1:
			return recovery, fmt.Errorf("%q is not the installation owner; this command is deliberately limited to the owner account", username)
		default:
			return recovery, fmt.Errorf("no account is named %q", username)
		}
	}
	if err != nil {
		return recovery, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return recovery, err
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, string(hash), now, id); err != nil {
		return recovery, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND secret!='')`, id).Scan(&recovery.FactorRemoved); err != nil {
		return recovery, err
	}
	// Removing the factor is the point of the command: an owner who still had the
	// authenticator or a recovery code would not need it. Leaving a lost factor in
	// place would restore the password and still refuse the sign-in.
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_mfa WHERE user_id=?`, id); err != nil {
		return recovery, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, id)
	if err != nil {
		return recovery, err
	}
	if recovery.CodesRemoved, err = result.RowsAffected(); err != nil {
		return recovery, err
	}
	// A password change ends every device, so a session that was stolen alongside
	// the credential does not survive the recovery that was meant to remove it.
	result, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id)
	if err != nil {
		return recovery, err
	}
	if recovery.SessionsEnded, err = result.RowsAffected(); err != nil {
		return recovery, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id=?`, id); err != nil {
		return recovery, err
	}
	if deactivated || banned {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET is_deactivated=0,is_banned=0,ban_reason='',updated_at=? WHERE id=?`, now, id); err != nil {
			return recovery, err
		}
		recovery.Reactivated, recovery.BanCleared = deactivated, banned
	}
	// actor_id stays NULL: no account performed this. The audit surface labels
	// that row as the host operator so it is never mistaken for the owner's own
	// action or for an actor whose account was erased.
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,NULL,'owner.recovered_offline',?,?)`, ID(), id, now); err != nil {
		return recovery, err
	}
	if err = tx.Commit(); err != nil {
		return recovery, err
	}
	recovery.Username = username
	return recovery, nil
}
