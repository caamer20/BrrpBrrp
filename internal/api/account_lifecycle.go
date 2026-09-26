package api

import (
	"net/http"

	"brrpbrrp/internal/db"
)

// anonymizeUser ends an identity while retaining its authored conversation history.
// Deactivation is the reversible default; erasing the account and its messages remains
// the separate DELETE action. No live data is rewritten until this explicit ceremony.
func (s *Server) anonymizeUser(w http.ResponseWriter, r *http.Request, actor db.User) {
	var input struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &input) {
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
	if err := s.requirePrivilegedSecurityPolicyInTx(r.Context(), tx, actor.ID, principal.IsOwner || principal.IsAdministrator()); err != nil {
		s.authorityFailed(w, err)
		return
	}
	target, err := s.store.Account(r.Context(), tx, r.PathValue("id"))
	if !s.found(w, err) {
		return
	}
	if target.ID == actor.ID || target.IsOwner {
		problem(w, http.StatusForbidden, "You cannot remove yourself or the installation owner. Transfer ownership first.")
		return
	}
	if err := s.mayAdminister(r.Context(), tx, principal, target); err != nil {
		problem(w, http.StatusForbidden, err.Error())
		return
	}
	if target.IsDeleted {
		problem(w, http.StatusConflict, "This account has already been removed.")
		return
	}
	if input.Confirm != target.Username {
		problem(w, http.StatusBadRequest, "Type the current username to confirm account removal.")
		return
	}
	if !hasOtherAdmin(tx, target.ID) {
		problem(w, http.StatusConflict, "At least one active administrator must remain.")
		return
	}
	// The reserved hyphenated username cannot be selected by signup. The password is
	// deliberately not a usable bcrypt hash; all authentication also refuses deactivation.
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET username=?,display_name='Deleted member',password_hash='',avatar_color='#808080',is_admin=0,is_banned=0,is_deactivated=1,is_deleted=1,ban_reason='',last_seen_at=NULL,updated_at=? WHERE id=? AND is_owner=0 AND is_deleted=0`, "deleted-"+target.ID, db.Now(), target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := cancelPendingDirectRequests(r.Context(), tx, target.ID); err != nil {
		s.failed(w, err)
		return
	}
	for _, query := range []string{
		`DELETE FROM sessions WHERE user_id=?`,
		`DELETE FROM push_subscriptions WHERE user_id=?`,
		`DELETE FROM user_mfa WHERE user_id=?`,
		`DELETE FROM recovery_codes WHERE user_id=?`,
		`DELETE FROM role_assignments WHERE user_id=?`,
		`DELETE FROM channel_user_access WHERE user_id=?`,
		`DELETE FROM conversation_members WHERE user_id=?`,
		`DELETE FROM channel_notification_preferences WHERE user_id=?`,
		`DELETE FROM channel_reads WHERE user_id=?`,
		`DELETE FROM conversation_reads WHERE user_id=?`,
		`DELETE FROM contact_preferences WHERE user_id=?`,
		`DELETE FROM direct_preferences WHERE user_id=?`,
		`DELETE FROM user_blocks WHERE blocker_id=?`,
		`DELETE FROM mentions WHERE target_user_id=?`,
		`DELETE FROM message_operations WHERE account_id=?`,
		`DELETE FROM attachments WHERE uploader_id=? AND message_id IS NULL`,
		`DELETE FROM invitations WHERE created_by=?`,
	} {
		if _, err := tx.ExecContext(r.Context(), query, target.ID); err != nil {
			s.failed(w, err)
			return
		}
	}
	if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, actor.ID, "user.anonymized", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitAuthorityChange(r.Context(), tx, "Your account was removed.", target.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.deliveries.revokeAccount(target.ID)
	s.leaveVoice(target.ID, "")
	s.hub.Disconnect(target.ID, "")
	s.hub.Broadcast("user.updated", map[string]string{"id": target.ID})
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}
