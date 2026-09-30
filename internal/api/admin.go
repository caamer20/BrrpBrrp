package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"golang.org/x/crypto/bcrypt"
)

var avatarColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request, user db.User) {
	users, err := s.users(true)
	if err != nil {
		s.failed(w, err)
		return
	}
	suspended, err := s.adminUserStates(r.Context(), users)
	if err != nil {
		s.failed(w, err)
		return
	}
	// The timeout is a separate field rather than part of the user, so it is only
	// visible here — to an account that may lift it — and never rides along on the
	// user record that every other reader sees.
	listed := make([]map[string]any, 0, len(users))
	for _, member := range users {
		entry := map[string]any{}
		raw, err := json.Marshal(member)
		if err != nil {
			s.failed(w, err)
			return
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			s.failed(w, err)
			return
		}
		entry["in_timeout"] = suspended[member.ID]
		listed = append(listed, entry)
	}
	respond(w, 200, listed)
}

// administratorRole loads the built-in administrator role inside the caller's
// transaction, so a promotion is decided against the role as it is committed and
// not against a catalog read before the guards ran.
func (s *Server) administratorRole(ctx context.Context, q db.Queryer) (db.Role, error) {
	return s.store.Role(ctx, q, db.AdministratorRoleID)
}

// requireMayGrantAdministrator applies the same rule to administrator authority
// that role assignment applies to every other role: authority moves downward
// only, so an administrator may not create, promote or demote a peer
// administrator. The owner is the single account that can.
//
// This is the check the account CRUD endpoints used to skip, which let a
// delegated member manager promote itself and then delete the owner.
func (s *Server) requireMayGrantAdministrator(ctx context.Context, q db.Queryer, principal authz.Principal) error {
	role, err := s.administratorRole(ctx, q)
	if err != nil {
		return err
	}
	// Administrator authority is granted by the owner or by an existing
	// administrator, and only to the extent that the granter already holds it.
	// A delegated member manager holds neither, which is the escalation this
	// endpoint used to permit.
	refusal := errors.New("only the owner or an existing administrator can grant administrator authority")
	if !principal.IsOwner && !principal.IsAdministrator() {
		return refusal
	}
	if principal.Position() < authz.RoleFrom(role).Position {
		return refusal
	}
	return principal.CanGrantCapabilities(authz.Capabilities(role.Permissions))
}

// mayAdminister refuses an account change that the actor's authority does not
// cover. Ownership is deliberately not editable here at all: it moves only
// through the ownership ceremony, which needs a recent strong sign-in.
func (s *Server) mayAdminister(ctx context.Context, q db.Queryer, principal authz.Principal, target db.User) error {
	if !principal.Allowed(authz.MemberManage) {
		return errors.New("your role does not include member.manage")
	}
	if target.IsOwner && target.ID != principal.UserID {
		return errors.New("ownership changes only through the ownership ceremony; transfer ownership first, or use brrpbrrp recover-owner on the server host")
	}
	rank, err := s.authz.Rank(ctx, q, target.ID)
	if err != nil {
		return err
	}
	if !principal.CanActOnAccount(target.IsOwner, rank) {
		return errors.New("you cannot change an account that holds the same or higher authority than you do")
	}
	return nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, actor db.User) {
	var input struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		IsAdmin     bool   `json:"is_admin"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !usernamePattern.MatchString(input.Username) || strings.EqualFold(input.Username, "everyone") || !validPassword(input.Password) || len(input.DisplayName) > 80 {
		problem(w, 400, "Use a valid username, a 12–72 byte password and a display name up to 80 characters.")
		return
	}
	if !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		s.failed(w, err)
		return
	}
	id, now := db.ID(), db.Now()
	colors := []string{"#89a8f4", "#e9af7e", "#b3d693", "#c5a4e8", "#7ac5c5", "#e59cae"}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, actor)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if !principal.Allowed(authz.MemberManage) {
		problem(w, 403, "Your role does not include member.manage.")
		return
	}
	// Capacity counts identities that can still be recovered, including banned
	// or deactivated accounts. Immutable tombstones retain history without taking
	// a live slot. Count and insert share this transaction so concurrent creators
	// cannot both consume the last slot.
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM users WHERE is_deleted=0`).Scan(&count); err != nil {
		s.failed(w, err)
		return
	}
	if count >= 100 {
		problem(w, 409, "This server supports up to 100 accounts and is intended for 10–20 concurrent users.")
		return
	}
	role := db.MemberRoleID
	if input.IsAdmin {
		// A new administrator is administrator authority, so it is decided by
		// the same rule as promoting an existing account.
		if err := s.requireMayGrantAdministrator(r.Context(), tx, principal); err != nil {
			problem(w, 403, err.Error())
			return
		}
		role = db.AdministratorRoleID
	}
	_, err = tx.Exec(`INSERT INTO users(id,username,display_name,password_hash,is_admin,avatar_color,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, strings.ToLower(input.Username), strings.TrimSpace(input.DisplayName), string(hash), input.IsAdmin, colors[count%len(colors)], now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			problem(w, 409, "That username is already in use.")
		} else {
			s.failed(w, err)
		}
		return
	}
	// The role assignment is created with the account, in the same transaction,
	// so the new account's authority is never undefined for a single request.
	if err := db.AssignAccountRole(r.Context(), tx, id, role); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.created", id); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	created, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if err != nil {
		s.failed(w, err)
		return
	}
	s.hub.Broadcast("user.updated", map[string]string{"id": id})
	respond(w, 201, created)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, actor db.User) {
	id := r.PathValue("id")
	current, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, id))
	if !s.found(w, err) {
		return
	}
	var input struct {
		DisplayName   *string `json:"display_name"`
		AvatarColor   *string `json:"avatar_color"`
		IsAdmin       *bool   `json:"is_admin"`
		IsBanned      *bool   `json:"is_banned"`
		IsDeactivated *bool   `json:"is_deactivated"`
		BanReason     string  `json:"ban_reason"`
		Confirm       string  `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.BanReason) > 300 {
		problem(w, 400, "Ban reason is too long.")
		return
	}
	displayName := current.DisplayName
	if input.DisplayName != nil {
		if len(*input.DisplayName) > 80 {
			problem(w, 400, "Display name is too long.")
			return
		}
		displayName = strings.TrimSpace(*input.DisplayName)
	}
	avatarColor := current.AvatarColor
	if input.AvatarColor != nil {
		if !avatarColorPattern.MatchString(*input.AvatarColor) {
			problem(w, 400, "Choose a six-digit hex color.")
			return
		}
		avatarColor = *input.AvatarColor
	}
	if (input.IsBanned != nil || input.IsAdmin != nil || input.IsDeactivated != nil) && input.Confirm != current.Username {
		problem(w, 400, "Confirm the account username before changing access.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// The target is read inside the committing transaction. The read above only
	// supplied the username for confirmation and the fields the request keeps; it
	// is not authority. A request paused before the transaction resumes against
	// the account as it is committed, so a transfer, promotion, ban or deletion
	// that landed in between is what this change is decided against.
	target, err := s.store.Account(r.Context(), tx, id)
	if !s.found(w, err) {
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "A removed account cannot be edited or reactivated.")
		return
	}
	// The legacy column is not written directly any more: administrator authority
	// moves only through the role service, which applies the hierarchy rule this
	// endpoint used to skip.
	storedAdmin := target.IsAdmin
	wantAdmin := storedAdmin
	if input.IsAdmin != nil {
		wantAdmin = *input.IsAdmin
	}
	wantBanned, wantDeactivated := target.IsBanned, target.IsDeactivated
	if input.IsBanned != nil {
		wantBanned = *input.IsBanned
	}
	if input.IsDeactivated != nil {
		wantDeactivated = *input.IsDeactivated
	}
	changesAuthority := wantAdmin != storedAdmin || wantBanned != target.IsBanned || wantDeactivated != target.IsDeactivated
	if changesAuthority && target.ID == actor.ID {
		problem(w, 400, "You cannot change your own authority or access. Another administrator or the owner must do it.")
		return
	}
	// Authority is re-resolved inside the transaction, so a session, role or
	// ownership change that committed while this request was being prepared
	// cannot still authorize it.
	principal, err := s.authorizeInTx(r.Context(), tx, actor)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if wantAdmin != storedAdmin {
		if err := s.requireMayGrantAdministrator(r.Context(), tx, principal); err != nil {
			problem(w, 403, err.Error())
			return
		}
	}
	if !wantAdmin || wantBanned || wantDeactivated {
		if !hasOtherAdmin(tx, target.ID) {
			problem(w, 409, "At least one active administrator must remain.")
			return
		}
	}
	// Ending or reducing an owner's access is not account management even if an
	// authority decision above were wrong: the statement itself refuses the
	// designated owner, so ownership only ends through the ceremony.
	guard := ""
	if wantBanned != target.IsBanned || wantDeactivated != target.IsDeactivated || (wantAdmin == false && storedAdmin) {
		guard = " AND is_owner=0"
	}
	result, err := tx.Exec(`UPDATE users SET display_name=?,avatar_color=?,is_banned=?,is_deactivated=?,ban_reason=?,updated_at=? WHERE id=?`+guard, displayName, avatarColor, wantBanned, wantDeactivated, input.BanReason, db.Now(), target.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		problem(w, 403, "Ownership changes only through the ownership ceremony; transfer ownership first, or use brrpbrrp recover-owner on the server host")
		return
	}
	if wantBanned || wantDeactivated {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, target.ID); err != nil {
			s.failed(w, err)
			return
		}
		if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE user_id=?`, target.ID); err != nil {
			s.failed(w, err)
			return
		}
	}
	// The legacy is_admin column, the administrator role assignment and the
	// authorization revision move together, so an already-admitted decision
	// cannot outlive the demotion. The permission to grant the authority and the
	// last-administrator rule were both checked above.
	administratorChanged := wantAdmin != storedAdmin
	if administratorChanged {
		if err := setAdministrator(r.Context(), tx, target.ID, wantAdmin); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.updated", id); err != nil {
		s.failed(w, err)
		return
	}
	switch {
	case administratorChanged:
		// The commit and the invalidation of the account whose authority moved are one
		// step under the fence, so a writer cannot admit an event resolved before the
		// demotion after it.
		if err := s.commitAuthorityChange(r.Context(), tx, "Your account authority changed.", target.ID); err != nil {
			s.failed(w, err)
			return
		}
	case wantBanned || wantDeactivated:
		// A ban or deactivation removed every session of the account above, so the
		// account's connections are invalidated with that commit rather than after the
		// slower disconnect below.
		if err := s.commitAuthorityChange(r.Context(), tx, "Your account access changed.", target.ID); err != nil {
			s.failed(w, err)
			return
		}
	default:
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
	}
	target.DisplayName, target.AvatarColor = displayName, avatarColor
	target.IsBanned, target.IsDeactivated, target.IsAdmin = wantBanned, wantDeactivated, wantAdmin
	if wantBanned || wantDeactivated {
		s.deliveries.revokeAccount(target.ID)
		s.leaveVoice(id, "")
		s.hub.Disconnect(id, "")
	}
	if administratorChanged {
		// Losing the administrator role also withdraws the manager view of every
		// restricted channel, including any voice channel this account is in and any
		// file it is still downloading.
		s.deliveries.revokeAccount(target.ID)
		s.enforceVoiceAccess(r.Context(), actor.ID, "Your account authority changed.", target.ID)
	}
	s.hub.Broadcast("user.updated", map[string]string{"id": id})
	respond(w, 200, target)
}

// setAdministrator writes administrator authority in one place: the legacy
// column that the policy bridge reads, the role assignment that the policy
// engine reads, and the revision every admitted decision is compared against.
func setAdministrator(ctx context.Context, tx *sql.Tx, userID string, admin bool) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_admin=?, updated_at=? WHERE id=?`, admin, db.Now(), userID); err != nil {
		return err
	}
	if admin {
		if err := db.AssignAccountRole(ctx, tx, userID, db.AdministratorRoleID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, userID, db.AdministratorRoleID); err != nil {
		return err
	}
	_, err := db.BumpAuthorizationRevision(ctx, tx)
	return err
}

func hasOtherAdmin(tx *sql.Tx, id string) bool {
	var count int
	err := tx.QueryRow(`SELECT count(*) FROM users WHERE is_admin=1 AND is_banned=0 AND is_deactivated=0 AND id<>?`, id).Scan(&count)
	return err == nil && count > 0
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, actor db.User) {
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if !s.found(w, err) || !confirmed(w, r, user.Username) {
		return
	}
	if user.ID == actor.ID {
		problem(w, 400, "You cannot delete your own account.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, actor)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// The target is read again inside the committing transaction. The read before
	// it only produced the username for confirmation; it is not authority. A
	// request paused there resumes against the account as it is committed, so a
	// transfer that made this target the owner is refused here instead of being
	// deleted on its stale record.
	target, err := s.store.Account(r.Context(), tx, user.ID)
	if !s.found(w, err) {
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if !hasOtherAdmin(tx, target.ID) {
		problem(w, 409, "At least one active administrator must remain.")
		return
	}
	// Ownership is not deletable even if a decision above were wrong: the
	// statement itself refuses the designated owner, so no account-management
	// request can remove it.
	if err := cancelPendingDirectRequests(r.Context(), tx, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Physical erasure cascades the account row away, which would take the owner row
	// with it and leave the group naming an account that does not exist. The
	// ownership is dispositioned first, while the membership rows are still readable,
	// so a surviving member becomes the owner instead (completion review 13, V13-09).
	if err := disposeGroupOwnershipForDepartingAccount(r.Context(), tx, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	result, err := tx.Exec(`DELETE FROM users WHERE id=? AND is_owner=0`, target.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		problem(w, 403, "Ownership changes only through the ownership ceremony; transfer ownership first, or use brrpbrrp recover-owner on the server host")
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.deleted", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Removing the account removed its sessions with it (the foreign key cascades), so
	// its connections are invalidated with the same commit before any cleanup runs.
	if err := s.commitAuthorityChange(r.Context(), tx, "Your account was removed.", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.deliveries.revokeAccount(target.ID)
	s.leaveVoice(target.ID, "")
	s.hub.Disconnect(target.ID, "")
	s.hub.Broadcast("user.deleted", map[string]string{"id": target.ID})
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request, actor db.User) {
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if !s.found(w, err) {
		return
	}
	var input struct {
		Password string `json:"new_password"`
		Confirm  string `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validPassword(input.Password) || input.Confirm != user.Username {
		problem(w, 400, "Confirm the username and choose a 12–72 byte password.")
		return
	}
	if !s.acquirePasswordSlot(w) {
		return
	}
	defer s.releasePasswordSlot()
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		s.failed(w, err)
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Chosen-password takeover is operator authority, and it is re-established
	// inside the transaction: if ownership moved away while this request was
	// being prepared, the authority that admitted it is gone with it.
	principal, err := s.requireCapabilityInTx(r.Context(), tx, actor, authz.OperatorCredentialRecovery)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// A chosen password would let the caller sign in as that account. It is never
	// an account-management action: the target must be below the actor, and an
	// owner's credentials are recovered on the server host instead. Both facts are
	// decided against the row this transaction commits, not the one the route read.
	target, err := s.store.Account(r.Context(), tx, user.ID)
	if !s.found(w, err) {
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "This account has been removed and cannot be changed.")
		return
	}
	if target.IsOwner {
		problem(w, http.StatusForbidden, "An owner's password is recovered on the server host with brrpbrrp recover-owner, not from the administration panel.")
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, string(hash), db.Now(), target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.password_reset", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// The old credential's sessions were deleted above, and the account's connections
	// are invalidated with that same commit. The reset used to commit first and invalidate
	// later, which left a window in which a queued private event could still be admitted
	// for a session the database no longer recognized; the invalidation is one step with
	// the commit now, so the window is closed even while the stream and socket cleanup
	// below is delayed.
	if err := s.commitAuthorityChange(r.Context(), tx, "Your password was reset by an operator.", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// A chosen password ends every stream the old credential was admitted for, on
	// every device, together with the sessions and subscriptions removed above. A
	// response already in flight carries no request to re-check, so the credential
	// change has to end it here.
	s.deliveries.revokeAccount(target.ID)
	s.leaveVoice(target.ID, "")
	s.hub.Disconnect(target.ID, "")
	respond(w, 200, map[string]bool{"ok": true})
}

// removeLostFactor is the audited emergency path for an account whose
// authenticator and recovery codes are both gone. A password reset deliberately
// keeps the factor, so without this the account could never sign in again and the
// only remaining answer would be to abandon it. It is operator authority
// (`operator.credential_recovery`, the same capability as a chosen-password
// reset), never ordinary moderation, and it needs a recent strong
// authentication.
//
// An owner's factor is not reachable here at all: that account holds the
// non-delegable operator tier, so its credentials are recovered on the server
// host with `brrpbrrp recover-owner`, where the action is recorded as the host
// operator rather than as a peer account.
func (s *Server) removeLostFactor(w http.ResponseWriter, r *http.Request, actor db.User) {
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if !s.found(w, err) {
		return
	}
	var input struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Confirm != user.Username {
		problem(w, 400, "Confirm the username to remove that account's second factor.")
		return
	}
	if user.IsOwner {
		problem(w, http.StatusForbidden, "An owner's second factor is recovered on the server host with brrpbrrp recover-owner, not from the administration panel.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, actor, authz.OperatorCredentialRecovery)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	target, err := s.store.Account(r.Context(), tx, user.ID)
	if !s.found(w, err) {
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "This account has been removed and cannot be changed.")
		return
	}
	if target.IsOwner {
		problem(w, http.StatusForbidden, "An owner's second factor is recovered on the server host with brrpbrrp recover-owner, not from the administration panel.")
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	var enrolled bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND secret!='')`, target.ID).Scan(&enrolled); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM user_mfa WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Removing a factor is a credential change, so it ends the account's devices
	// exactly like a password reset does. The person recovering the account then
	// signs in with the password alone and enrolls a new factor at once.
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.mfa_removed", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Removing the factor deleted the account's sessions above, so their connections are
	// invalidated with the same commit, exactly as in the operator password reset.
	if err := s.commitAuthorityChange(r.Context(), tx, "Your second factor was removed by an operator.", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.deliveries.revokeAccount(target.ID)
	s.leaveVoice(target.ID, "")
	s.hub.Disconnect(target.ID, "")
	respond(w, 200, map[string]bool{"removed": enrolled})
}

// kickUser ends an account's sessions. It is account management, so it obeys the
// same hierarchy rule: a delegated member manager cannot sign out an account
// that holds the same or higher authority than it does.
func (s *Server) kickUser(w http.ResponseWriter, r *http.Request, actor db.User) {
	user, err := scanUser(s.store.QueryRow(`SELECT `+userColumns+` FROM users WHERE id=?`, r.PathValue("id")))
	if !s.found(w, err) || !confirmed(w, r, user.Username) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, actor)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	target, err := s.store.Account(r.Context(), tx, user.ID)
	if !s.found(w, err) {
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "This account has been removed and cannot be changed.")
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, 403, err.Error())
		return
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM push_subscriptions WHERE user_id=?`, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.kicked", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	// The removed sessions' connections are invalidated with the commit, so no event
	// resolved for them is admitted while the cleanup below runs.
	if err := s.commitAuthorityChange(r.Context(), tx, "Your sessions were ended by an operator.", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.deliveries.revokeAccount(target.ID)
	s.leaveVoice(target.ID, "")
	s.hub.Disconnect(target.ID, "")
	respond(w, 200, map[string]bool{"ok": true})
}

// audit records an ordinary activity event. Security-relevant changes use
// auditTx instead, which refuses to let the change commit without its record.
// The audit log is not switchable off: an installation whose administrators can
// silence the record of their own authority changes cannot be reviewed.
func (s *Server) audit(actor, action, target string) {
	if _, err := s.store.Exec(`INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,?,?,?,?)`, db.ID(), actor, action, target, db.Now()); err != nil {
		s.log.Warn("audit event could not be saved")
	}
}

// auditTx writes a required audit record inside the caller's transaction. A
// mutation that changes authority, credentials or access is never acknowledged
// without its record: the two commit together or the request fails.
func (s *Server) auditTx(ctx context.Context, tx *sql.Tx, actor, action, target string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(id,actor_id,action,target_id,created_at) VALUES(?,?,?,?,?)`, db.ID(), actor, action, target, db.Now())
	return err
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request, user db.User) {
	before := r.URL.Query().Get("before")
	if before == "" {
		before = "z"
	}
	// The filter is applied before the limit, so a private review cannot occupy a
	// page slot in the community feed or shift what the cursor points at. An
	// event whose actor_id is NULL was performed on the host command line while
	// the server was stopped, not by an account; every other missing actor is an
	// account that has since been erased. The two must not look alike.
	rows, err := s.store.Query(`SELECT a.id,
        CASE WHEN a.actor_id IS NULL AND a.action='owner.recovered_offline' THEN 'host operator (offline)'
             ELSE COALESCE(u.username,'former member') END,
        a.action,a.target_id,a.created_at
        FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id
        WHERE a.id<? AND `+restrictedAuditNotLike("a.action")+` ORDER BY a.id DESC LIMIT 50`, before)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	entries := make([]map[string]string, 0)
	for rows.Next() {
		var id, actor, action, target, created string
		if err := rows.Scan(&id, &actor, &action, &target, &created); err != nil {
			s.failed(w, err)
			return
		}
		entries = append(entries, map[string]string{"id": id, "actor": actor, "action": action, "target_id": target, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, entries)
}

// privateEvidenceAuditLogs returns the restricted audit feed: the records for
// private evidence review *and* for private group membership.
//
// The records are not hidden from audit altogether — a review and an invitation
// are administrative acts and must be accountable — but they are kept out of the
// community feed, so an ordinary audit reader never learns that a named person
// reported a specific private message, or that a private group exists at all.
//
// The audience is the audit capability rather than report.review_private, and that
// is the point of the split. Before this, the feed was gated on the evidence
// capability, which meant the only accounts able to read who had joined a group
// were evidence reviewers — a grant made to see submitted text, used to browse
// group membership instead. An evidence reviewer and an auditor now read
// different things (completion review 13, V13-02).
func (s *Server) privateEvidenceAuditLogs(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	before := r.URL.Query().Get("before")
	if before == "" {
		before = "z"
	}
	if len(before) > 100 {
		problem(w, http.StatusBadRequest, "Invalid audit cursor.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Read under the *evidence* capability, checked inside the transaction like every
	// other authority decision, so a grant revoked between the route guard and this
	// point is refused rather than acted on. The row names the reporter and the
	// reviewer, so `audit.read` is not sufficient: the community audit capability
	// must not disclose that a specific person reported a specific private message
	// (completion review 14, V14-01).
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ReportReviewPrivate); err != nil {
		s.authorityFailed(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT a.id,
        COALESCE(u.username,'former member'),a.action,a.target_id,a.created_at
        FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id
        WHERE a.id<? AND `+restrictedAuditLike("a.action")+` ORDER BY a.id DESC LIMIT 50`, before)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	entries := make([]map[string]string, 0)
	for rows.Next() {
		var id, actor, action, target, created string
		if err := rows.Scan(&id, &actor, &action, &target, &created); err != nil {
			s.failed(w, err)
			return
		}
		entries = append(entries, map[string]string{"id": id, "actor": actor, "action": action, "target_id": target, "created_at": created})
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, entries)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request, user db.User) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	var messages int
	if err := s.store.QueryRow(`SELECT count(*) FROM messages WHERE channel_id IS NOT NULL`).Scan(&messages); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]any{"uptime_seconds": int(time.Since(s.started).Seconds()), "connections": s.hub.Count(), "voice_users": len(s.voice.States()), "memory_bytes": memory.Alloc, "database_bytes": fileSize(s.config.DBFile), "uploads_bytes": directorySize(s.config.UploadDir), "messages": messages, "version": app.Version})
}
