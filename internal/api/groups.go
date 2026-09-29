package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Consenting groups (ADR 0016).
//
// A group is a conversations row with kind='group' and members in
// conversation_members. Nothing here is a second message store, a second
// authorization path or a second set of counters: the same rows, read cursor,
// receipts, search, attachments, activity and evidence rules already apply to
// kind='direct', and they apply here because they were written against membership
// rather than against "is this a direct pair".
//
// What this file adds is the part the shared model does not decide: who may invite,
// who may accept, who owns the group, and what happens when people leave. Each of
// those has more than one defensible answer, so each is stated in the ADR and
// implemented once here.

const (
	// groupMemberLimit is the number of accepted members a group holds initially. It
	// is named rather than configured, and it is stated in the invitation so nobody
	// joins expecting a different number (ADR 0016, "Bounds").
	groupMemberLimit = 10
	// groupInvitationLimit bounds open invitations per group, so a group cannot be
	// turned into a distribution list by repeated inviting.
	groupInvitationLimit = 10
	// groupInvitationLifetime is how long an invitation stays acceptable. A
	// withdrawn invitation is not re-offerable, so this is the only window.
	groupInvitationLifetime = 7 * 24 * time.Hour
	// groupInvitationsPerDay bounds what one account may send, which is the
	// difference between a group and unsolicited contact at volume.
	groupInvitationsPerDay = 20
)

// groupInvitationLifetimeWindow bounds the input, so a caller cannot mint an
// invitation that outlives every plausible answer.
const groupInvitationMaxDays = 30

// groupSummary is what a member is told about a group they can see. It carries no
// message content: a group's existence is not disclosed to someone who is not a
// member, and a non-member's listing contains no groups at all.
type groupSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnerID     string `json:"owner_id"`
	OwnerName   string `json:"owner_name"`
	Members     int    `json:"members"`
	Pending     int    `json:"pending_invitations"`
	CreatedAt   string `json:"created_at"`
	MyState     string `json:"my_state"`
	CanInvite   bool   `json:"can_invite"`
	CanTransfer bool   `json:"can_transfer"`
}

// createGroup opens a group with the creator as its owner and its only member.
//
// The creator is a member because opening a group and being its first member is one
// act, not two: there is nobody else to consent. Everything after that is the
// recipient's decision alone.
func (s *Server) createGroup(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 80 {
		problem(w, http.StatusBadRequest, "Choose a name of up to 80 characters.")
		return
	}
	now := db.Now()
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	id := db.ID()
	// kind='group' with a null channel: a group is not a community channel and must
	// never acquire one, which is what keeps community authority from granting access
	// to a private group.
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO conversations(id,kind,channel_id,state,created_at,updated_at) VALUES(?,'group',NULL,'active',?,?)`, id, now, now); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO conversation_members(conversation_id,user_id,state,invited_by,joined_at,updated_at) VALUES(?,?,'accepted',?,?,?)`, id, principal.UserID, principal.UserID, now, now); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO group_owners(conversation_id,user_id,updated_at) VALUES(?,?,?)`, id, principal.UserID, now); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO group_details(conversation_id,name) VALUES(?,?)`, id, name); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	summary, err := s.groupSummary(r.Context(), principal.UserID, id)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusCreated, summary)
}

// listGroups returns the groups the caller is a member of, and nothing else. A
// group an account was invited to and has not accepted is not listed: it is pending,
// and the invitation is where it is visible.
func (s *Server) listGroups(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	rows, err := s.store.QueryContext(r.Context(), `SELECT c.id FROM conversations c
		JOIN conversation_members m ON m.conversation_id=c.id
		WHERE c.kind='group' AND m.user_id=? AND m.state='accepted' AND c.state='active'
		ORDER BY c.updated_at DESC, c.id DESC LIMIT 200`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			s.failed(w, err)
			return
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	groups := make([]groupSummary, 0, len(ids))
	for _, id := range ids {
		summary, err := s.groupSummary(r.Context(), user.ID, id)
		if err != nil {
			s.failed(w, err)
			return
		}
		groups = append(groups, summary)
	}
	respond(w, http.StatusOK, map[string]any{"groups": groups})
}

// groupSummary reads one group's state for one account. A group that is not visible
// to that account is indistinguishable from one that does not exist, so the caller's
// membership is the first thing resolved and a failure never discloses the group.
func (s *Server) groupSummary(ctx context.Context, userID, groupID string) (groupSummary, error) {
	var summary groupSummary
	summary.ID = groupID
	err := s.store.QueryRowContext(ctx, `SELECT d.name,o.user_id,COALESCE(u.username,''),c.created_at,m.state
		FROM conversations c
		JOIN group_details d ON d.conversation_id=c.id
		JOIN group_owners o ON o.conversation_id=c.id
		LEFT JOIN users u ON u.id=o.user_id
		JOIN conversation_members m ON m.conversation_id=c.id AND m.user_id=?
		WHERE c.id=? AND c.kind='group' AND m.state='accepted'`, userID, groupID).
		Scan(&summary.Name, &summary.OwnerID, &summary.OwnerName, &summary.CreatedAt, &summary.MyState)
	if err != nil {
		return groupSummary{}, err
	}
	_ = s.store.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM conversation_members m JOIN users mu ON mu.id=m.user_id
		  WHERE m.conversation_id=? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0),
		(SELECT count(*) FROM group_invitations WHERE conversation_id=? AND state='pending')`,
		groupID, groupID).Scan(&summary.Members, &summary.Pending)
	// Only the owner may invite, and only while there is room. A non-owner is told so
	// rather than offered an affordance that will be refused.
	summary.CanInvite = summary.OwnerID == userID && summary.Pending < groupInvitationLimit
	summary.CanTransfer = summary.OwnerID == userID
	return summary, nil
}

// inviteToGroup invites one person. Only the owner may, and the invitation names the
// group and the history rule so the recipient is consenting to both (ADR 0016,
// "History: the D-07 rule").
func (s *Server) inviteToGroup(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	groupID := r.PathValue("id")
	var input struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.UserID == "" || input.UserID == user.ID {
		problem(w, http.StatusBadRequest, "Choose another member of this server.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if err := s.expireInvitations(r.Context(), tx, now16()); err != nil {
		s.failed(w, err)
		return
	}
	summary, err := s.groupSummaryInTx(r.Context(), tx, principal.UserID, groupID)
	if err != nil {
		hidden(w)
		return
	}
	if summary.OwnerID != principal.UserID {
		// Only the owner invites. A member cannot grow a group they do not run, and
		// the refusal does not say who the owner is.
		problem(w, http.StatusForbidden, "Only the group owner can invite.")
		return
	}
	var usable int
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND is_deleted=0 AND is_deactivated=0 AND is_banned=0)`, input.UserID).Scan(&usable); err != nil {
		s.failed(w, err)
		return
	}
	if usable != 1 {
		hidden(w)
		return
	}
	// Contact policy, checked before anything is written so a refusal leaves no
	// record at all. A block governs whether a person may be approached; it does not
	// hide a group's existing history, which is stated separately (ADR 0016).
	var blocked int
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_blocks
		WHERE (blocker_id=? AND blocked_id=?) OR (blocker_id=? AND blocked_id=?))`,
		principal.UserID, input.UserID, input.UserID, principal.UserID).Scan(&blocked); err != nil {
		s.failed(w, err)
		return
	}
	if blocked == 1 {
		problem(w, http.StatusConflict, "That person cannot be invited while contact is blocked.")
		return
	}
	// The invitee's own contact preference governs whether they may be approached
	// at all, which is the same rule a direct request honours.
	// An absent preference row is the documented default, which is "accepting"; only a
	// row that exists and says otherwise refuses.
	var acceptsRequests int
	switch err := tx.QueryRowContext(r.Context(), `SELECT allow_requests FROM contact_preferences WHERE user_id=?`, input.UserID).Scan(&acceptsRequests); {
	case err == nil:
		if acceptsRequests == 0 {
			problem(w, http.StatusConflict, "That person is not accepting new contact.")
			return
		}
	case errors.Is(err, sql.ErrNoRows):
		// The default accepts, so nothing to refuse.
	default:
		s.failed(w, err)
		return
	}
	var already int
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=? AND user_id=? AND state='accepted')`, groupID, input.UserID).Scan(&already); err != nil {
		s.failed(w, err)
		return
	}
	if already == 1 {
		problem(w, http.StatusConflict, "That person is already in this group.")
		return
	}
	// The per-group and per-account bounds are counted here, in the same transaction
	// as the insert, so two concurrent invites cannot both see the last slot free.
	var pending int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM group_invitations WHERE conversation_id=? AND state='pending'`, groupID).Scan(&pending); err != nil {
		s.failed(w, err)
		return
	}
	if pending >= groupInvitationLimit {
		problem(w, http.StatusConflict, "That group already has as many open invitations as it allows.")
		return
	}
	var sent int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM group_invitations WHERE inviter_id=? AND created_at>?`, principal.UserID, db.Timestamp(now16().Add(-24*time.Hour))).Scan(&sent); err != nil {
		s.failed(w, err)
		return
	}
	if sent >= groupInvitationsPerDay {
		problem(w, http.StatusTooManyRequests, "You have sent as many invitations today as this server allows.")
		return
	}
	now := now16()
	// One open invitation per (group, invitee): a retried invite is the same
	// invitation, so a lost response cannot produce two to reconcile.
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO group_invitations(id,conversation_id,inviter_id,invitee_id,state,created_at,expires_at,responded_at)
		VALUES(?,?,?,?,'pending',?,?,'') ON CONFLICT DO NOTHING`, db.ID(), groupID, principal.UserID, input.UserID, db.Timestamp(now), db.Timestamp(now.Add(groupInvitationLifetime))); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "group.invited", groupID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	// The invitation is addressed activity, so the recipient can find it after being
	// offline. It carries no group name in the payload: a lock screen should not
	// disclose which groups someone belongs to.
	s.queueActivityNotice(input.UserID, activityRequest, groupID, groupID, principal.UserID)
	respond(w, http.StatusCreated, map[string]any{"invited": true, "expires_at": db.Timestamp(now.Add(groupInvitationLifetime))})
}

func (s *Server) groupSummaryInTx(ctx context.Context, tx *sql.Tx, userID, groupID string) (groupSummary, error) {
	var summary groupSummary
	summary.ID = groupID
	err := tx.QueryRowContext(ctx, `SELECT d.name,o.user_id,COALESCE(u.username,''),c.created_at,m.state
		FROM conversations c
		JOIN group_details d ON d.conversation_id=c.id
		JOIN group_owners o ON o.conversation_id=c.id
		LEFT JOIN users u ON u.id=o.user_id
		JOIN conversation_members m ON m.conversation_id=c.id AND m.user_id=?
		WHERE c.id=? AND c.kind='group' AND m.state='accepted'`, userID, groupID).
		Scan(&summary.Name, &summary.OwnerID, &summary.OwnerName, &summary.CreatedAt, &summary.MyState)
	if err != nil {
		return groupSummary{}, err
	}
	_ = tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM conversation_members m JOIN users mu ON mu.id=m.user_id
		  WHERE m.conversation_id=? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0),
		(SELECT count(*) FROM group_invitations WHERE conversation_id=? AND state='pending')`,
		groupID, groupID).Scan(&summary.Members, &summary.Pending)
	summary.CanInvite = summary.OwnerID == userID && summary.Pending < groupInvitationLimit
	summary.CanTransfer = summary.OwnerID == userID
	return summary, nil
}

// acceptGroupInvitation is the recipient's act alone. The inviter cannot accept for
// them, and a cross-accepted pair does not admit either side automatically
// (ADR 0016, "Consent is per group and per act").
func (s *Server) acceptGroupInvitation(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	invitationID := r.PathValue("id")
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	now := now16()
	if err := s.expireInvitations(r.Context(), tx, now); err != nil {
		s.failed(w, err)
		return
	}
	var groupID, inviteeID, state, expiresAt string
	if err := tx.QueryRowContext(r.Context(), `SELECT conversation_id,invitee_id,state,expires_at FROM group_invitations WHERE id=?`,
		invitationID).Scan(&groupID, &inviteeID, &state, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			hidden(w)
			return
		}
		s.failed(w, err)
		return
	}
	// An invitation addressed to someone else is refused identically to one that does
	// not exist, so accepting is not a way to discover a group.
	if inviteeID != principal.UserID {
		hidden(w)
		return
	}
	if state != "pending" {
		problem(w, http.StatusConflict, "That invitation is no longer open.")
		return
	}
	// The group has to still be one. It is emptied by its last member leaving, and an
	// invitation can outlive that; accepting into a group marked removed used to
	// answer 200 and then 404 on the conversation, which is the worst of both
	// answers (completion review 13, V13-04).
	var groupState string
	if err := tx.QueryRowContext(r.Context(), `SELECT state FROM conversations WHERE id=? AND kind='group'`, groupID).Scan(&groupState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, http.StatusGone, "That group no longer exists.")
			return
		}
		s.failed(w, err)
		return
	}
	if groupState != "active" {
		problem(w, http.StatusGone, "That group has ended, so the invitation cannot be accepted.")
		return
	}
	// A block in either direction ends the invitation's usefulness: contact policy that
	// governs direct requests has to govern an invitation, or a block is bypassed by
	// starting a group. The group's existing history is not hidden — that is a
	// separate promise this product does not make (ADR 0016).
	var blocked int
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_blocks
		WHERE (blocker_id=? AND blocked_id=?) OR (blocker_id=? AND blocked_id=?))`,
		inviteeID, principal.UserID, principal.UserID, inviteeID).Scan(&blocked); err != nil {
		s.failed(w, err)
		return
	}
	if blocked == 1 {
		// Close the invitation rather than leaving it open against a block, so a later
		// unblock cannot surface a stale offer.
		if _, err := tx.ExecContext(r.Context(), `UPDATE group_invitations SET state='revoked',responded_at=?
			WHERE id=? AND state='pending'`, db.Timestamp(now), invitationID); err != nil {
			s.failed(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		problem(w, http.StatusConflict, "That invitation cannot be accepted while contact is blocked.")
		return
	}
	if expiresAt <= db.Timestamp(now) {
		problem(w, http.StatusConflict, "That invitation has expired. Ask for another one.")
		return
	}
	// The member bound is counted in this transaction, so two concurrent acceptances
	// cannot both see the last slot free.
	var members int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM conversation_members m JOIN users mu ON mu.id=m.user_id
		WHERE m.conversation_id=? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0`, groupID).Scan(&members); err != nil {
		s.failed(w, err)
		return
	}
	if members >= groupMemberLimit {
		problem(w, http.StatusConflict, "That group is full. It holds ten members.")
		return
	}
	// Acceptance is idempotent per (group, account): a replayed acceptance is the
	// same acceptance, not a second membership.
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO conversation_members(conversation_id,user_id,state,invited_by,joined_at,updated_at)
		VALUES(?,?,'accepted',(SELECT inviter_id FROM group_invitations WHERE id=?),?,?)
		ON CONFLICT(conversation_id,user_id) DO UPDATE SET state='accepted',joined_at=excluded.joined_at,updated_at=excluded.updated_at`,
		groupID, principal.UserID, invitationID, db.Timestamp(now), db.Timestamp(now)); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE group_invitations SET state='accepted',responded_at=? WHERE id=? AND state='pending'`,
		db.Timestamp(now), invitationID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "group.joined", groupID); err != nil {
		s.failed(w, err)
		return
	}
	// Acceptance grants access to a private conversation, so it is an authority
	// change: the revision advances, the participants' open responses are ended, and
	// they are told their view changed.
	participants := s.groupParticipants(r.Context(), tx, groupID)
	if err := s.commitGroupChange(r.Context(), tx, "Your group membership changed.", participants...); err != nil {
		s.failed(w, err)
		return
	}
	s.afterGroupChange(participants...)
	summary, err := s.groupSummary(r.Context(), principal.UserID, groupID)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, summary)
}

// rejectGroupInvitation is the recipient declining. It is recorded rather than
// deleted so the group owner can see that somebody declined, and so the invocation
// is auditable; the invitation then stops being open either way.
func (s *Server) rejectGroupInvitation(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	invitationID := r.PathValue("id")
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	now := db.Timestamp(now16())
	result, err := tx.ExecContext(r.Context(), `UPDATE group_invitations SET state='rejected',responded_at=?
		WHERE id=? AND invitee_id=? AND state='pending'`, now, invitationID, principal.UserID)
	if err != nil {
		s.failed(w, err)
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		hidden(w)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"declined": true})
}

// leaveGroup removes the caller from a group. The owner cannot simply leave: a group
// must never reach a state where nobody can administer it, so the owner transfers
// first or names a successor in the same act.
func (s *Server) leaveGroup(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	groupID := r.PathValue("id")
	var input struct {
		SuccessorID string `json:"successor_id"`
	}
	if r.ContentLength > 0 && !decode(w, r, &input) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	summary, err := s.groupSummaryInTx(r.Context(), tx, principal.UserID, groupID)
	if err != nil {
		hidden(w)
		return
	}
	now := now16()
	if summary.OwnerID == principal.UserID {
		// The last member leaving is not the same problem as an owner leaving a group
		// that still has people in it. Here the group is about to have nobody, and the
		// answer is to remove it rather than to demand a successor who does not
		// exist. Otherwise the only way out of a group one has created alone would be
		// to invite somebody first, which is a worse product than the problem.
		var others int
		if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM conversation_members m JOIN users mu ON mu.id=m.user_id
			WHERE m.conversation_id=? AND m.user_id<>? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0`,
			groupID, principal.UserID).Scan(&others); err != nil {
			s.failed(w, err)
			return
		}
		if others > 0 {
			if input.SuccessorID == "" {
				// The refusal is actionable rather than a dead end: it names what to do.
				problem(w, http.StatusConflict, "Transfer ownership to another member before you leave, or name one here.")
				return
			}
			if err := s.transferGroupOwnershipInTx(r.Context(), tx, groupID, principal.UserID, input.SuccessorID, now); err != nil {
				problem(w, http.StatusConflict, "Name another member of this group as its owner.")
				return
			}
		}
	}
	if err := s.removeMemberInTx(r.Context(), tx, groupID, principal.UserID, "left", now); err != nil {
		s.failed(w, err)
		return
	}
	// A group with no accepted members left is removed with its messages, files and
	// activity. An orphan that a later joiner could inherit is not a state this
	// product has (ADR 0016, "Group ownership is deterministic").
	if err := s.discardGroupIfEmpty(r.Context(), tx, groupID, now); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "group.left", groupID); err != nil {
		s.failed(w, err)
		return
	}
	participants := append(s.groupParticipants(r.Context(), tx, groupID), principal.UserID)
	if err := s.commitGroupChange(r.Context(), tx, "Your group membership changed.", participants...); err != nil {
		s.failed(w, err)
		return
	}
	s.afterGroupChange(participants...)
	respond(w, http.StatusOK, map[string]any{"left": true})
}

// removeGroupMember is the owner's act, and it is distinct from leaving: the member
// is marked removed and loses everything from now on, while what they read before
// stays theirs (ADR 0016, "History").
func (s *Server) removeGroupMember(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	groupID, targetID := r.PathValue("id"), r.PathValue("user")
	if targetID == "" {
		problem(w, http.StatusBadRequest, "Choose a member to remove.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	summary, err := s.groupSummaryInTx(r.Context(), tx, principal.UserID, groupID)
	if err != nil {
		hidden(w)
		return
	}
	if summary.OwnerID != principal.UserID {
		problem(w, http.StatusForbidden, "Only the group owner can remove a member.")
		return
	}
	if targetID == principal.UserID {
		problem(w, http.StatusConflict, "Use leaving to remove yourself from your own group.")
		return
	}
	// Removing the owner is refused: ownership governs who may invite and remove, and
	// a group whose owner can be removed has an owner who can do neither. The owner
	// hands the role over first, which is the same requirement as leaving. The
	// comparison is against the owner row read here, not the summary computed before
	// this transaction, so a transfer that landed in between cannot be answered
	// against the previous owner.
	var currentOwner string
	if err := tx.QueryRowContext(r.Context(), `SELECT user_id FROM group_owners WHERE conversation_id=?`, groupID).Scan(&currentOwner); err != nil {
		s.failed(w, err)
		return
	}
	if targetID == currentOwner {
		problem(w, http.StatusConflict, "Transfer ownership to another member before removing them.")
		return
	}
	now := now16()
	if err := s.removeMemberInTx(r.Context(), tx, groupID, targetID, "removed", now); err != nil {
		s.failed(w, err)
		return
	}
	// A removal that empties a group leaves nobody to run it, so the group goes with
	// its members rather than surviving as a room one person can no longer leave.
	if err := s.discardGroupIfEmpty(r.Context(), tx, groupID, now); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "group.member_removed", groupID); err != nil {
		s.failed(w, err)
		return
	}
	// A removal withdraws access to a private conversation, so the removed account's
	// open responses end and the remaining members are told the membership changed.
	participants := append(s.groupParticipants(r.Context(), tx, groupID), targetID)
	if err := s.commitGroupChange(r.Context(), tx, "Your group membership changed.", participants...); err != nil {
		s.failed(w, err)
		return
	}
	s.afterGroupChange(participants...)
	respond(w, http.StatusOK, map[string]any{"removed": true})
}

// transferGroupOwnership moves ownership atomically. The new owner must be an
// accepted member, and the old owner stops being owner in the same commit: a
// transfer that is not atomic would leave two accounts able to invite.
func (s *Server) transferGroupOwnership(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	groupID := r.PathValue("id")
	var input struct {
		NewOwnerID string `json:"new_owner_id"`
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
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	summary, err := s.groupSummaryInTx(r.Context(), tx, principal.UserID, groupID)
	if err != nil {
		hidden(w)
		return
	}
	if summary.OwnerID != principal.UserID {
		problem(w, http.StatusForbidden, "Only the group owner can transfer ownership.")
		return
	}
	if err := s.transferGroupOwnershipInTx(r.Context(), tx, groupID, principal.UserID, input.NewOwnerID, now16()); err != nil {
		problem(w, http.StatusConflict, "Choose another member of this group as its owner.")
		return
	}
	if err := s.auditTx(r.Context(), tx, principal.UserID, "group.ownership_transferred", groupID); err != nil {
		s.failed(w, err)
		return
	}
	// Ownership governs who may invite and who may remove, so a transfer moves
	// authority and both accounts' open responses end.
	participants := []string{principal.UserID, input.NewOwnerID}
	if err := s.commitGroupChange(r.Context(), tx, "Your group role changed.", participants...); err != nil {
		s.failed(w, err)
		return
	}
	s.afterGroupChange(participants...)
	respond(w, http.StatusOK, map[string]any{"owner_id": input.NewOwnerID})
}

func (s *Server) transferGroupOwnershipInTx(ctx context.Context, tx *sql.Tx, groupID, currentOwnerID, newOwnerID string, now time.Time) error {
	if newOwnerID == "" || newOwnerID == currentOwnerID {
		return authz.ErrForbidden
	}
	var member int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m JOIN users mu ON mu.id=m.user_id
		WHERE m.conversation_id=? AND m.user_id=? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0)`,
		groupID, newOwnerID).Scan(&member); err != nil {
		return err
	}
	if member != 1 {
		return authz.ErrForbidden
	}
	// The new owner must be an accepted member before the old owner stops being one,
	// so the group is never momentarily unowned inside the transaction.
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_members SET state='accepted',updated_at=?
		WHERE conversation_id=? AND user_id=? AND state<>'accepted'`, db.Timestamp(now), groupID, newOwnerID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE group_owners SET user_id=?,updated_at=? WHERE conversation_id=? AND user_id=?`,
		newOwnerID, db.Timestamp(now), groupID, currentOwnerID)
	return err
}

// removeMemberInTx marks a member as left or removed. The row is kept rather than
// deleted so their read position and their earlier access are not rewritten, which
// is the ADR 0011 distinction applied to groups.
func (s *Server) removeMemberInTx(ctx context.Context, tx *sql.Tx, groupID, userID, state string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE conversation_members SET state=?,joined_at=NULL,updated_at=?
		WHERE conversation_id=? AND user_id=? AND state='accepted'`, state, db.Timestamp(now), groupID, userID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return authz.ErrUnavailable
	}
	return nil
}

// discardGroupIfEmpty removes a group with no accepted members, together with its
// messages, files, activity and invitations by cascade.
func (s *Server) discardGroupIfEmpty(ctx context.Context, tx *sql.Tx, groupID string, now time.Time) error {
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM conversation_members m JOIN users mu ON mu.id=m.user_id
		WHERE m.conversation_id=? AND m.state='accepted' AND mu.is_deleted=0 AND mu.is_deactivated=0 AND mu.is_banned=0`, groupID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 0 {
		return nil
	}
	// The invitations close with the group, in the same transaction. An invitation
	// left open against a group that will never exist is a standing offer to join
	// nothing, and it is exactly what the accepting path then had to refuse after
	// answering 200 (completion review 13, V13-04).
	if _, err := tx.ExecContext(ctx, `UPDATE group_invitations SET state='revoked',responded_at=?
		WHERE conversation_id=? AND state='pending'`, db.Timestamp(now), groupID); err != nil {
		return err
	}
	// The owner row goes too. Leaving it behind is what made the groups list fail
	// after a physical erasure cascaded the owner's user row away: a group with
	// members and no owner is a state this product does not have.
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_owners WHERE conversation_id=?`, groupID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE conversations SET state='removed',updated_at=? WHERE id=? AND kind='group'`, db.Timestamp(now), groupID)
	return err
}

// expireInvitations closes invitations whose window has passed, and withdraws the
// membership row any of them had already created. It is called from every path that
// acts on invitations, so an expiry is observed without waiting for a sweeper.
func (s *Server) expireInvitations(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE group_invitations SET state='expired',responded_at=?
		WHERE state='pending' AND expires_at<=?`, db.Timestamp(now), db.Timestamp(now))
	return err
}

// listGroupInvitations shows the caller the invitations addressed to them, or, for
// the owner, the ones their group has sent. Neither discloses a group the caller is
// not party to.
func (s *Server) listGroupInvitations(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if err := s.expireInvitations(r.Context(), tx, now16()); err != nil {
		s.failed(w, err)
		return
	}
	// Addressed to me, plus the groups I own. The group name is included for the
	// former because the recipient has to be able to decide, and omitted for the
	// latter's invitee list because the inviter already knows the group.
	rows, err := tx.QueryContext(r.Context(), `SELECT i.id,i.conversation_id,COALESCE(d.name,''),COALESCE(u.username,''),i.state,i.created_at,i.expires_at,i.invitee_id,
        EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=i.conversation_id AND m.user_id=? AND m.state='accepted') AS mine,
        EXISTS(SELECT 1 FROM group_owners o WHERE o.conversation_id=i.conversation_id AND o.user_id=?) AS i_own
        FROM group_invitations i
        LEFT JOIN group_details d ON d.conversation_id=i.conversation_id
        LEFT JOIN users u ON u.id=i.inviter_id
        WHERE (i.invitee_id=? OR (i.state='pending' AND EXISTS(SELECT 1 FROM group_owners o2 WHERE o2.conversation_id=i.conversation_id AND o2.user_id=?)))
        ORDER BY i.created_at DESC LIMIT 200`,
		principal.UserID, principal.UserID, principal.UserID, principal.UserID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	type invitation struct {
		ID          string `json:"id"`
		GroupID     string `json:"group_id"`
		GroupName   string `json:"group_name"`
		InviterName string `json:"inviter_name"`
		State       string `json:"state"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
		MyState     string `json:"my_state"`
	}
	// The three states a caller can be in, which are genuinely different: an
	// invitation they can act on, one they already accepted, and one their group
	// sent that they have no part in.
	out := []invitation{}
	for rows.Next() {
		var i invitation
		var inviteeID string
		var mine, iOwn int
		if err := rows.Scan(&i.ID, &i.GroupID, &i.GroupName, &i.InviterName, &i.State, &i.CreatedAt, &i.ExpiresAt, &inviteeID, &mine, &iOwn); err != nil {
			s.failed(w, err)
			return
		}
		// A group name is disclosed only to the invitee and to the owner, so a
		// listing cannot become a directory of private groups.
		if iOwn == 1 && mine == 0 {
			i.GroupName = ""
		}
		switch {
		case mine == 1:
			i.MyState = "member"
		case inviteeID == principal.UserID && i.State == "pending":
			i.MyState = "invited"
		default:
			i.MyState = "sent"
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"invitations": out})
}

func now16() time.Time { return time.Now() }

// listGroupMembers shows the members of a group the caller belongs to.
//
// It is a membership list, not a message list: the interface needs it to offer the
// three actions ownership governs, and it must not become a way to enumerate the
// people in a private group. A caller who is not an accepted member gets the same
// answer as one asking about a group that does not exist.
func (s *Server) listGroupMembers(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	groupID := r.PathValue("id")
	var member int
	if err := s.store.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversation_members m
		WHERE m.conversation_id=? AND m.user_id=? AND m.state='accepted')`, groupID, user.ID).Scan(&member); err != nil {
		s.failed(w, err)
		return
	}
	if member != 1 {
		hidden(w)
		return
	}
	rows, err := s.store.QueryContext(r.Context(), `SELECT u.id,u.username,u.display_name,m.state,
        EXISTS(SELECT 1 FROM group_owners o WHERE o.conversation_id=m.conversation_id AND o.user_id=u.id) AS owned
        FROM conversation_members m JOIN users u ON u.id=m.user_id
        WHERE m.conversation_id=? AND m.state='accepted' AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0
        ORDER BY owned DESC, u.username`, groupID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	type groupMemberRow struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		State       string `json:"state"`
		IsOwner     bool   `json:"is_owner"`
	}
	out := []groupMemberRow{}
	for rows.Next() {
		var m groupMemberRow
		var owned int
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.State, &owned); err != nil {
			s.failed(w, err)
			return
		}
		m.IsOwner = owned == 1
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"members": out})
}

// commitGroupChange is the one commit path for a group mutation, and it is the
// direct-conversation protocol applied to a group.
//
// A group change is an authority change: membership, ownership and the ability to
// read a private conversation all move together, so a plain tx.Commit leaves the
// authorization revision where it was, no open response is ended, and no invalidation
// reaches the accounts whose view just changed. That was reproduced: removing an
// accepted member returned 200 with the revision unchanged and a registered
// attachment stream still running (completion review 13, V13-03).
//
// The order is fixed and is the same one every other authority mutation uses: take
// the write lock, read the revision from the transaction that is about to commit,
// fence the affected sockets' sessions through that same transaction, commit, and
// only then end streams and publish. Nothing is published for a commit that failed.
func (s *Server) commitGroupChange(ctx context.Context, tx *sql.Tx, reason string, accounts ...string) error {
	if _, err := db.BumpAuthorizationRevision(ctx, tx); err != nil {
		return err
	}
	return s.commitAuthorityChange(ctx, tx, reason, accounts...)
}

// afterGroupChange is the post-commit side: streams the affected accounts had open
// are ended, and the accounts themselves are told their view changed.
//
// It is separate from the commit because it must not run for a transaction that
// rolled back, and because cancelling a response before the commit that authorises
// the cancellation would drop a legitimate download on a failed write.
func (s *Server) afterGroupChange(accounts ...string) {
	for _, accountID := range accounts {
		if accountID == "" {
			continue
		}
		s.deliveries.revokeAccount(accountID)
		s.leaveVoice(accountID, "")
	}
}

// groupParticipants reads the accounts a group mutation affects: the members who
// remain and the one who is being removed. It is read inside the transaction so the
// revocation set is the membership as of the change rather than as of before it.
func (s *Server) groupParticipants(ctx context.Context, q db.Queryer, groupID string) []string {
	rows, err := q.QueryContext(ctx, `SELECT m.user_id FROM conversation_members m
		WHERE m.conversation_id=? AND m.state IN ('accepted','pending')`, groupID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids
		}
		ids = append(ids, id)
	}
	return ids
}
