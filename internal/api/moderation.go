package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Delegated moderation, expiring timeouts and bounded invitations (ADR 0018).
//
// The rule that runs through all of it: an action is decided by the actor's own
// current authority, inside the transaction that performs it. Nothing here trusts a
// route guard, a prior check, or an assignment.

// assignReport records who is expected to review a report. It is a claim, not a
// grant: reviewing still requires the reviewer's own current capability, so an
// assignment to someone who has since lost it is a stale claim rather than a way to
// review. The assignee need not be able to review today, and nothing is refused on
// that account — a claim can outlive the person who made it.
func (s *Server) assignReport(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		AssigneeID string `json:"assignee_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.AssigneeID = strings.TrimSpace(input.AssigneeID)
	if input.AssigneeID != "" {
		// An assignee must exist. A claim attached to a guessed id reads as "someone is
		// looking at this" to everyone who sees it, and is attached to nobody.
		var exists bool
		if err := s.store.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND is_deleted=0)`, input.AssigneeID).Scan(&exists); err != nil {
			s.failed(w, err)
			return
		}
		if !exists {
			problem(w, 400, "Assign the report to an existing account, or clear the assignment.")
			return
		}
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.MessageDeleteAny)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	report, err := scanReport(tx.QueryRowContext(r.Context(), `SELECT `+reportColumns+` FROM message_reports WHERE id=? AND expires_at>?`, r.PathValue("id"), db.Now()))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	// Assignment is moderation of a report, so it needs the same read access the review
	// itself needs: an account that cannot see the channel has no business deciding
	// who looks at a report in it.
	readable, err := s.store.ChannelAccessible(r.Context(), tx, viewer(principal), report.ChannelID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !readable {
		hidden(w)
		return
	}
	now := db.Now()
	assignedAt := now
	if input.AssigneeID == "" {
		assignedAt = ""
	}
	result, err := tx.ExecContext(r.Context(), `UPDATE message_reports SET assigned_to=NULLIF(?,''),assigned_at=? WHERE id=?`, input.AssigneeID, assignedAt, report.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count != 1 {
		hidden(w)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "report.assigned", report.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	report.AssignedTo, report.AssignedAt = input.AssigneeID, assignedAt
	respond(w, http.StatusOK, report)
}

// applyTimeout suspends an account until a time. It changes the account's ability to
// act and nothing else: roles are not touched, so expiry has nothing to restore and a
// role revoked while the timeout runs stays revoked (ADR 0018).
func (s *Server) applyTimeout(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		// Accepted for compatibility with the interface that sent it, and deliberately
		// not authoritative. Two targets for one action is how a request ends up
		// timing out an account the operator did not name, and the two fields do not
		// have to agree.
		UserID    string `json:"user_id"`
		Minutes   int    `json:"minutes"`
		Reason    string `json:"reason"`
		ExpiresAt string `json:"expires_at"`
	}
	if !decode(w, r, &input) {
		return
	}
	// The route names the account. A body that names a different one is refused rather
	// than silently resolved, because "I timed out the account in the link" and "I
	// timed out the account in the form" are different actions and only one of them
	// was displayed to the operator.
	target := r.PathValue("id")
	if target == "" {
		problem(w, http.StatusNotFound, "Name the account in the address.")
		return
	}
	if input.UserID != "" && input.UserID != target {
		problem(w, 400, "The account in the address and the account in the request disagree. Send the one you meant.")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 2000 {
		problem(w, 400, "Give a reason of at most 2,000 characters.")
		return
	}
	var expiresAt time.Time
	switch {
	case input.ExpiresAt != "":
		parsed, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil {
			problem(w, 400, "Choose when the timeout ends.")
			return
		}
		expiresAt = parsed.UTC()
	case input.Minutes > 0:
		// Bounded on the write. An unbounded timeout is a ban wearing a different
		// name, and banning is a separate decision with its own rules.
		if input.Minutes > 60*24*30 {
			problem(w, 400, "A timeout can last at most 30 days.")
			return
		}
		expiresAt = time.Now().UTC().Add(time.Duration(input.Minutes) * time.Minute)
	default:
		problem(w, 400, "Choose how long the timeout lasts, or when it ends.")
		return
	}
	if !expiresAt.After(time.Now().UTC()) {
		problem(w, 400, "A timeout has to end in the future.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.MemberManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	// The same downward-only rule every other account change uses. It is checked
	// against current authority inside this transaction: a demotion that lands between
	// the route and the write must not complete against the older level.
	if err := s.mayTimeout(r.Context(), tx, principal, target); err != nil {
		if errors.Is(err, authz.ErrUnavailable) {
			hidden(w)
			return
		}
		problem(w, 403, err.Error())
		return
	}
	restores, err := s.recordedCapabilities(r.Context(), tx, target)
	if err != nil {
		s.failed(w, err)
		return
	}
	now := db.Now()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO account_timeouts(user_id,restores,reason,applied_by,applied_at,expires_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET restores=excluded.restores,reason=excluded.reason,applied_by=excluded.applied_by,applied_at=excluded.applied_at,expires_at=excluded.expires_at`,
		target, restores, input.Reason, principal.UserID, now, db.Timestamp(expiresAt)); err != nil {
		s.failed(w, err)
		return
	}
	// The revision has to advance here, before the commit that publishes it. The
	// commit protocol *reads* the revision the change produces; it does not create one.
	// A timeout that reached the protocol without this call reported success, committed
	// the row, audited it, and left the revision where it was — so work already queued
	// for the account was still admissible, and a writer that decided afterwards could
	// not tell the difference (completion review 14, V14-03: 1 -> 1).
	if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
		s.failed(w, err)
		return
	}
	// A timeout is a change of state, so it goes through the authority commit protocol
	// rather than a bare write: the revision advances under the fence, the account's
	// own connections are invalidated, and queued work resolved against the older
	// revision is refused.
	// `commitAuthorityChange` commits. Calling `tx.Commit` again afterwards is not a
	// redundant safety net: the second call fails, and the handler reports a 500 for
	// work that already succeeded. A moderator sees "the server couldn't complete that
	// request" for a timeout that is in force, and retries — and the retry is a second
	// authority change on top of the first.
	if err := s.commitAuthorityChange(r.Context(), tx, "account.timed_out", target); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"user_id": target, "expires_at": db.Timestamp(expiresAt), "reason": input.Reason,
	})
}

// liftTimeout ends a timeout early. It is separate from expiry so that lifting one
// early is a decision somebody made and recorded, rather than a silence.
func (s *Server) liftTimeout(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.MemberManage)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	if err := s.mayTimeout(r.Context(), tx, principal, r.PathValue("id")); err != nil {
		if errors.Is(err, authz.ErrUnavailable) {
			hidden(w)
			return
		}
		problem(w, 403, err.Error())
		return
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM account_timeouts WHERE user_id=?`, r.PathValue("id"))
	if err != nil {
		s.failed(w, err)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		hidden(w)
		return
	}
	// Lifting restores the account's access, so it advances the revision for the same
	// reason applying did. A lift that leaves the revision where it was tells a writer
	// that nothing changed when something did.
	if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
		s.failed(w, err)
		return
	}
	// Commits, as above; a second `tx.Commit` here reported 500 for a lift that had
	// already been applied and audited.
	if err := s.commitAuthorityChange(r.Context(), tx, "account.timeout_lifted", r.PathValue("id")); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"user_id": r.PathValue("id"), "timeout": "lifted"})
}

// mayTimeout checks the same downward-only rule every other account change uses,
// against current authority inside the caller's transaction: a demotion that lands
// between the route and the write must not complete against the older level.
func (s *Server) mayTimeout(ctx context.Context, q db.Queryer, principal authz.Principal, targetID string) error {
	target, err := s.store.Account(ctx, q, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authz.ErrUnavailable
		}
		return err
	}
	if target.IsDeleted {
		return authz.ErrUnavailable
	}
	return s.mayAdminister(ctx, q, principal, target)
}

// recordedCapabilities records what an account held when the timeout was applied.
//
// The value is still written, for an installation upgrading from a build whose expiry
// did replay it, and for an operator reading a row to see what was true at the time.
// Nothing replays it any more, and that is the point: a snapshot is a statement about
// the past, and authority is a statement about the present.
func (s *Server) recordedCapabilities(ctx context.Context, q db.Queryer, userID string) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT role_id FROM role_assignments WHERE user_id=? ORDER BY role_id`, userID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	roles := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		roles = append(roles, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(roles, ","), nil
}

// expireTimeouts is the bounded sweep. It restores the recorded assignments and
// clears the row in the same transaction, so there is no window in which an account is
// both suspended and unsuspended and a login in that window cannot observe one
// without the other.
func (s *Server) expireTimeouts(ctx context.Context, tx *sql.Tx) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT user_id,restores FROM account_timeouts WHERE expires_at<=? LIMIT 200`, db.Now())
	if err != nil {
		return 0, err
	}
	expired := []struct{ id, restores string }{}
	for rows.Next() {
		var row struct{ id, restores string }
		if err := rows.Scan(&row.id, &row.restores); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, row := range expired {
		// Nothing is restored, because a timeout never took anything away.
		//
		// This used to replay a snapshot of the account's role assignments, on the
		// reasoning that a role removed *during* the timeout must stay removed. It did
		// the opposite: the snapshot predated the removal, so restoring it put the role
		// back. Reproduced — a role revoked through `/api/admin/users/{id}/roles` while
		// a timeout ran came back when the timeout expired, with the operator's
		// revocation silently undone by a maintenance sweep nobody was watching.
		//
		// A timeout suspends an account. It does not change what the account holds, so
		// expiry has nothing to undo. The snapshot is kept in the column for rows written
		// by older builds and is simply not acted on.
		if _, err := tx.ExecContext(ctx, `DELETE FROM account_timeouts WHERE user_id=?`, row.id); err != nil {
			return 0, err
		}
	}
	return len(expired), nil
}

// timeoutState reports whether an account is inside a timeout, and lifts an expired
// one. Lifting at the point of use is deliberate: a timeout whose moment has passed
// should not need a maintenance job to have run before the account works again, and
// doing it here means a person signing in is what clears it.
//
// The clear and the read happen in one transaction, so a request cannot observe an
// account as both suspended and not.
func (s *Server) timeoutState(ctx context.Context, userID string) (suspended bool, until string) {
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return false, ""
	}
	defer tx.Rollback()
	var expires string
	if err := tx.QueryRowContext(ctx, `SELECT expires_at FROM account_timeouts WHERE user_id=?`, userID).Scan(&expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ""
		}
		return false, ""
	}
	if expires <= db.Now() {
		// Lifting the row is the whole action. See `expireTimeouts` for why nothing is
		// restored, and why replaying the recorded snapshot put back roles that had
		// been revoked while the timeout ran.
		if _, err := tx.ExecContext(ctx, `DELETE FROM account_timeouts WHERE user_id=?`, userID); err != nil {
			return false, ""
		}
		if err := tx.Commit(); err != nil {
			return false, ""
		}
		return false, ""
	}
	// The time is shown as stored rather than reformatted: it is the server's value and
	// a person deciding whether to appeal needs it exactly, not re-rendered.
	_ = tx.Rollback()
	return true, expires
}
