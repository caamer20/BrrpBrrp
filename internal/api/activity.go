package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Durable account activity (ADR 0015).
//
// The row records that something happened and is created inside the transaction
// that creates the thing it describes. It stores identifiers, a kind and a
// timestamp — never a second copy of message text or of a request introduction — so
// a row cannot become a private-history store that outlives the conversation's own
// authorization. What it may be read is decided by the conversation it points at,
// every time, not by what the row remembers.
//
// Unread is not stored here and is not derived from this table. Unread comes from
// the read cursor, so it cannot disagree with history; this table exists so an
// account can catch up after being offline, which a derived count cannot do.

const (
	// activityPageLimit bounds a page. It is a limit on work per request, not on
	// history: older rows stay reachable through the cursor.
	activityPageLimit = 50
	// activityRetention is how long an activity row is kept. It is deliberately
	// shorter than the 90-day evidence window: activity is a catch-up affordance
	// and evidence is a submitted record someone may appeal, so one clock for both
	// would either over-retain notifications or under-retain evidence.
	activityRetention = 30 * 24 * time.Hour
	// activityUnacknowledgedLimit bounds what one account may accumulate without
	// ever looking. The bound refuses new activity rather than evicting unread rows:
	// dropping a row the person has not seen is the same quiet failure as evicting
	// evidence mid-triage.
	activityUnacknowledgedLimit = 2000
	// activityPruneBatch bounds one reclamation pass so maintenance never holds a
	// write lock long enough to stall a busy installation.
	activityPruneBatch = 500
)

// activityKinds are the sources this build can record. Group and thread sources
// arrive later as more values here, not as a change to the table: the identity and
// acknowledgment rules already accommodate them.
const (
	activityMessage = "message"
	activityRequest = "request"
)

// privateActivityKinds maps an activity kind to the private notification
// preference that governs interrupting it. A kind with no entry is not a private
// kind and is not gated by these preferences.
var privateActivityKinds = map[string]string{
	activityMessage: "direct_message",
	activityRequest: "incoming_request",
}

// recordActivity writes one activity row for one recipient, inside the caller's
// transaction so it commits with the thing it describes.
//
// It is deliberately idempotent: a retried send, a replayed request, or a
// recomputation of the same event finds the existing row and leaves it alone. The
// unique index on (user_id, source_kind, source_id) is what makes that true, and
// the conflict is swallowed rather than surfaced, because a duplicate here means the
// same event was observed twice, not that anything is wrong.
//
// `interruptible` is decided by the caller's own eligibility, not by a parameter
// the caller could forget to compute: see notifyableActivity.
func (s *Server) recordActivity(ctx context.Context, tx *sql.Tx, userID, sourceKind, sourceID, conversationID, actorID string, now time.Time) error {
	if userID == "" || sourceID == "" {
		return nil
	}
	interruptible, err := s.notifyableActivity(ctx, tx, userID, sourceKind, conversationID, actorID, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_activity(id,user_id,source_kind,source_id,conversation_id,actor_id,created_at,acknowledged_at,interruptible)
		VALUES(?,?,?,?,?,?,?,'',?) ON CONFLICT(user_id,source_kind,source_id) DO NOTHING`,
		db.ID(), userID, sourceKind, sourceID, conversationID, nullIfEmpty(actorID), db.Timestamp(now), boolToInt(interruptible))
	return err
}

// notifyableActivity answers whether a row may interrupt its recipient right now.
// It is the write-time half of the dispatch-time check, and both call the same
// underlying eligibility so the two cannot disagree about a fact that matters.
//
// The ordering is the one ADR 0015 states: the account must still be usable, the
// destination must be one this build knows how to check, current access must hold,
// and only then do preferences apply. A preference is never allowed to grant access
// that the authority check would refuse.
func (s *Server) notifyableActivity(ctx context.Context, tx *sql.Tx, userID, sourceKind, conversationID, actorID string, now time.Time) (bool, error) {
	if !s.accountCanBeNotified(ctx, tx, userID) {
		return false, nil
	}
	// A destination is mandatory. There is no "no destination" case: an empty one is
	// how a private notice would otherwise be dispatched with no authorization check
	// at all, which is the bypass the consent work exists to prevent (ADR 0015,
	// "Push destinations are typed").
	if sourceKind == "" || conversationID == "" {
		return false, nil
	}
	allowed, err := conversationCurrentlyReadable(ctx, tx, userID, conversationID, actorID, sourceKind)
	if err != nil || !allowed {
		return false, err
	}
	preferenceKind, private := privateActivityKinds[sourceKind]
	if !private {
		// A community channel's own audience and mute rules decide this, and they
		// already recompute at dispatch. Nothing is added here.
		return true, nil
	}
	var muted int
	if err := tx.QueryRowContext(ctx, `SELECT muted FROM private_notification_preferences WHERE user_id=? AND kind=?`, userID, preferenceKind).Scan(&muted); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return muted == 0, nil
}

// deliverableActivity is the dispatch-time half: a queued notice cannot disclose
// activity the recipient lost access to while it waited in the backlog, so this is
// recomputed per device rather than trusted from enqueue time. It is the same
// decision as notifyableActivity, re-read at the moment the bytes would leave.
func (s *Server) deliverableActivity(userID, sourceKind, conversationID, actorID string) bool {
	if !s.accountCanBeNotified(s.ctx, s.store, userID) {
		return false
	}
	if sourceKind == "" || conversationID == "" {
		return false
	}
	allowed, err := conversationCurrentlyReadable(s.ctx, s.store, userID, conversationID, actorID, sourceKind)
	if err != nil || !allowed {
		return false
	}
	preferenceKind, private := privateActivityKinds[sourceKind]
	if !private {
		return true
	}
	// An absent row is the documented default, which is "not muted" — the same
	// reading the write-time path gives it. A row that exists and says muted is a
	// suppression. Only a read that *fails* is a refusal, because failing open would
	// mean a broken preference lookup produces an interruption.
	var muted int
	switch err := s.store.QueryRow(`SELECT muted FROM private_notification_preferences WHERE user_id=? AND kind=?`, userID, preferenceKind).Scan(&muted); {
	case err == nil:
		return muted == 0
	case errors.Is(err, sql.ErrNoRows):
		return true
	default:
		s.log.Warn("private notification preference lookup failed", "error", err)
		return false
	}
}

// accountCanBeNotified is the account-usable check. A banned, deactivated or erased
// account is not told about anything, and its queued work is dropped rather than
// delivered to a device that has no session to act on.
func (s *Server) accountCanBeNotified(ctx context.Context, q db.Queryer, userID string) bool {
	var usable int
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0 AND is_deleted=0)`, userID).Scan(&usable); err != nil {
		return false
	}
	return usable == 1
}

// conversationCurrentlyReadable answers whether userID may still read conversationID
// right now, honouring current consent, blocking and account availability rather
// than what was true when the activity was written. It refuses a missing or
// unknown conversation identically, so it is not an existence oracle.
func conversationCurrentlyReadable(ctx context.Context, q db.Queryer, userID, conversationID, actorID, sourceKind string) (bool, error) {
	// A pending request is not a readable conversation: the recipient has not accepted
	// it, so the ordinary direct-membership predicate would refuse the very event
	// they are entitled to know about. The request's own liveness is the test instead
	// — it is still pending, and it is addressed to this account. Eligibility is
	// re-read at dispatch, so a request accepted, rejected or withdrawn in the
	// meantime stops being deliverable (ADR 0015).
	if sourceKind == activityRequest {
		var pending int
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM direct_requests r JOIN users ru ON ru.id=r.requester_id
			WHERE r.conversation_id=? AND r.recipient_id=? AND r.state='pending' AND r.attempt_id<>''
			  AND ru.is_deleted=0 AND ru.is_deactivated=0 AND ru.is_banned=0)`,
			conversationID, userID).Scan(&pending); err != nil {
			return false, err
		}
		if pending == 0 {
			return false, nil
		}
		var blocked int
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_blocks b
			WHERE (b.blocker_id=? AND b.blocked_id=?) OR (b.blocker_id=? AND b.blocked_id=?))`,
			userID, actorID, actorID, userID).Scan(&blocked); err != nil {
			return false, err
		}
		return blocked == 0, nil
	}
	if conversationID == "" {
		return false, nil
	}
	var permitted int
	// A direct conversation requires current accepted membership on both sides and
	// no block in either direction. A community channel requires membership and the
	// channel's own read capability. Both are decided here, now, not remembered.
	err := q.QueryRowContext(ctx, `SELECT EXISTS(
        SELECT 1 FROM conversations c
        WHERE c.id=? AND c.kind='direct'
          AND EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=c.id AND m.user_id=? AND m.state='accepted')
          AND EXISTS(SELECT 1 FROM conversation_members m2 JOIN users u2 ON u2.id=m2.user_id
                     WHERE m2.conversation_id=c.id AND m2.user_id<>? AND m2.state='accepted'
                       AND u2.is_deleted=0 AND u2.is_deactivated=0 AND u2.is_banned=0)
          AND NOT EXISTS(SELECT 1 FROM user_blocks b
                         WHERE (b.blocker_id=? AND b.blocked_id=?) OR (b.blocker_id=? AND b.blocked_id=?)))`,
		conversationID, userID, userID, actorID, userID, actorID, userID).Scan(&permitted)
	if err != nil {
		return false, err
	}
	if permitted == 1 {
		return true, nil
	}
	// A community channel is checked separately so a channel notice is not refused
	// merely because the direct-conversation predicate did not match.
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(
        SELECT 1 FROM conversations c JOIN channels ch ON ch.id=c.id
        WHERE c.id=? AND c.kind='channel'
          AND EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=c.id AND m.user_id=? AND m.state='accepted'))`,
		conversationID, userID).Scan(&permitted); err != nil {
		return false, err
	}
	return permitted == 1, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// listActivity is the account's own activity, newest first, cursor paginated.
//
// Cursors are opaque and account-scoped: a cursor is a position in *this* account's
// activity, so it cannot be replayed against another account's list. Filters are
// applied before the limit so a filtered-out row cannot occupy a page slot or move
// the cursor, which is the same discipline the audit feeds use.
func (s *Server) listActivity(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	before := r.URL.Query().Get("before")
	if before == "" {
		before = "z"
	}
	if len(before) > 100 {
		problem(w, http.StatusBadRequest, "Invalid activity cursor.")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != activityMessage && kind != activityRequest {
		problem(w, http.StatusBadRequest, "Choose a known activity kind.")
		return
	}
	unacknowledgedOnly := r.URL.Query().Get("unacknowledged") == "1"

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
	// Eligibility is filtered before the limit, and against current access, so a row
	// the account may no longer read is neither returned nor able to shift the
	// cursor. The conversation is not returned: an activity list is not a way to
	// learn the shape of a private inbox.
	rows, err := tx.QueryContext(r.Context(), `SELECT a.id,a.source_kind,a.source_id,u.username,a.created_at,a.acknowledged_at,
        (CASE WHEN a.source_kind='request'
              THEN EXISTS(SELECT 1 FROM direct_requests r JOIN users ru ON ru.id=r.requester_id
                          WHERE r.conversation_id=a.conversation_id AND r.recipient_id=a.user_id AND r.state='pending'
                            AND ru.is_deleted=0 AND ru.is_deactivated=0 AND ru.is_banned=0)
              ELSE EXISTS(SELECT 1 FROM conversation_members m JOIN users cu ON cu.id=m.user_id
                          WHERE m.conversation_id=a.conversation_id AND m.user_id=a.user_id AND m.state='accepted'
                            AND cu.is_deleted=0 AND cu.is_deactivated=0 AND cu.is_banned=0)
         END) AS member,
        (SELECT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=a.user_id AND b.blocked_id=a.actor_id) OR (b.blocker_id=a.actor_id AND b.blocked_id=a.user_id)))
        FROM account_activity a LEFT JOIN users u ON u.id=a.actor_id
        WHERE a.user_id=? AND a.id<? AND a.conversation_id<>''
          AND (`+kindIs(kind)+`) AND (`+unacknowledgedIs(unacknowledgedOnly)+`)
        ORDER BY a.id DESC LIMIT ?`,
		principal.UserID, before, activityPageLimit+1)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()

	type entry struct {
		id, kind, sourceID, actor, createdAt, acknowledgedAt string
		blocked                                               bool
	}
	items := make([]map[string]any, 0, activityPageLimit)
	cursor := ""
	more := false
	for rows.Next() {
		var e entry
		var member int
		if err := rows.Scan(&e.id, &e.kind, &e.sourceID, &e.actor, &e.createdAt, &e.acknowledgedAt, &member, &e.blocked); err != nil {
			s.failed(w, err)
			return
		}
		if member == 0 || e.blocked {
			// Written before access was lost. It is not shown, and it is not able to
			// occupy a page slot, so the cursor below is not moved by a row this
			// account cannot use.
			continue
		}
		if len(items) == activityPageLimit {
			// The cursor is the last row *returned*, not the first one withheld.
			// `before` is exclusive, so pointing it at the withheld row would skip
			// that row entirely: with 51 rows and a limit of 50 the walk would stop at
			// 50 and never deliver the fifty-first (completion review 13, R13-01).
			more = true
			break
		}
		items = append(items, map[string]any{
			"id":              e.id,
			"kind":            e.kind,
			"source_id":       e.sourceID,
			"actor":           e.actor,
			"created_at":      e.createdAt,
			"acknowledged_at": e.acknowledgedAt,
			"acknowledged":    e.acknowledgedAt != "",
		})
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	if more && len(items) > 0 {
		cursor, _ = items[len(items)-1]["id"].(string)
	}
	counts := s.activityCounts(principal.UserID)
	respond(w, http.StatusOK, map[string]any{
		"activity":             items,
		"next_cursor":          cursor,
		"outstanding":          counts.outstanding,
		"unacknowledged_count": counts.unacknowledged,
	})
}

// acknowledgeActivity marks activity seen. It is a separate call rather than a side
// effect of reading, because a read that fails halfway must not have consumed the
// acknowledgment, and because the acknowledgment has to survive the page it was made
// from.
//
// The write is monotonic: an account that acknowledges from a second device does not
// un-acknowledge on the first, and a stale client acknowledging an older position
// cannot move the record backwards.
func (s *Server) acknowledgeActivity(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Before string `json:"before"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Before = strings.TrimSpace(input.Before)
	if input.Before == "" || len(input.Before) > 100 {
		problem(w, http.StatusBadRequest, "Choose where the acknowledgment starts.")
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
	result, err := tx.ExecContext(r.Context(), `UPDATE account_activity SET acknowledged_at=?
        WHERE user_id=? AND id<? AND acknowledged_at=''`,
		db.Now(), principal.UserID, input.Before)
	if err != nil {
		s.failed(w, err)
		return
	}
	affected, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	counts := s.activityCounts(principal.UserID)
	respond(w, http.StatusOK, map[string]any{
		"acknowledged":         affected,
		"unacknowledged_count": counts.unacknowledged,
		"outstanding":          counts.outstanding,
	})
}

// activityCounts reports the two derived numbers the client shows alongside the
// list. Neither is stored: both are functions of current state, so neither can
// disagree with history.
func (s *Server) activityCounts(userID string) struct{ outstanding, unacknowledged int } {
	var counts struct{ outstanding, unacknowledged int }
	_ = s.store.QueryRow(`SELECT
        (SELECT count(*) FROM account_activity WHERE user_id=? AND acknowledged_at=''),
        (SELECT count(*) FROM direct_requests r JOIN users u ON u.id=r.recipient_id
          WHERE r.recipient_id=? AND r.state='pending' AND r.attempt_id<>''
            AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0)`, userID, userID).
		Scan(&counts.unacknowledged, &counts.outstanding)
	return counts
}

func kindIs(kind string) string {
	if kind == "" {
		return "1=1"
	}
	return "a.source_kind='" + kind + "'"
}

func unacknowledgedIs(only bool) string {
	if only {
		return "a.acknowledged_at=''"
	}
	return "1=1"
}

// pruneActivity reclaims in bounded batches: acknowledged rows first, on the
// activity clock; unacknowledged rows only once they are both old and past the
// per-account bound, so an account that never looks cannot grow the table without
// limit and a busy account does not lose rows it has not seen.
func (s *Server) pruneActivity(now time.Time) error {
	if _, err := s.store.Exec(`DELETE FROM account_activity WHERE id IN (
        SELECT id FROM account_activity WHERE acknowledged_at<>'' AND created_at<?
        ORDER BY created_at LIMIT ?)`, db.Timestamp(now.Add(-activityRetention)), activityPruneBatch); err != nil {
		return err
	}
	_, err := s.store.Exec(`DELETE FROM account_activity WHERE id IN (
        SELECT a.id FROM account_activity a
        WHERE a.acknowledged_at='' AND a.created_at<?
          AND (SELECT count(*) FROM account_activity b WHERE b.user_id=a.user_id AND b.acknowledged_at='') > ?
        ORDER BY a.created_at LIMIT ?)`, db.Timestamp(now.Add(-activityRetention)), activityUnacknowledgedLimit, activityPruneBatch)
	return err
}

// setPrivateNotificationPreference sets one account's own preference for one
// private kind. It cannot grant anything: a muted preference suppresses an
// interruption, and no value of any field changes whether the account may read a
// conversation.
func (s *Server) setPrivateNotificationPreference(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	var input struct {
		Kind        string `json:"kind"`
		Muted       *bool  `json:"muted"`
		ShowSender  *bool  `json:"show_sender"`
	}
	if !decode(w, r, &input) {
		return
	}
	known := false
	for _, preferenceKind := range privateActivityKinds {
		if preferenceKind == input.Kind {
			known = true
		}
	}
	if input.Kind == "" || !known {
		problem(w, http.StatusBadRequest, "Choose a private notification kind.")
		return
	}
	if input.Muted == nil && input.ShowSender == nil {
		problem(w, http.StatusBadRequest, "Choose what to change.")
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
	var muted, showSender int
	if err := tx.QueryRowContext(r.Context(), `SELECT muted,show_sender FROM private_notification_preferences WHERE user_id=? AND kind=?`, principal.UserID, input.Kind).Scan(&muted, &showSender); err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	if input.Muted != nil {
		muted = boolToInt(*input.Muted)
	}
	if input.ShowSender != nil {
		showSender = boolToInt(*input.ShowSender)
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO private_notification_preferences(user_id,kind,muted,show_sender,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(user_id,kind) DO UPDATE SET muted=excluded.muted,show_sender=excluded.show_sender,updated_at=excluded.updated_at`,
		principal.UserID, input.Kind, muted, showSender, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	// Existing rows are brought into line so the change takes effect for activity
	// already waiting rather than only for what arrives next. This is interruption
	// state, so moving it is not a change to history.
	// The preference is keyed by its own kind; the rows it governs are keyed by the
	// activity kind that preference governs. Conflating the two would update nothing
	// and leave a muted account still being interrupted.
	activityKind := ""
	for candidate, preferenceKind := range privateActivityKinds {
		if preferenceKind == input.Kind {
			activityKind = candidate
		}
	}
	if activityKind == "" {
		respond(w, http.StatusOK, map[string]any{
			"kind": input.Kind, "muted": muted == 1, "show_sender": showSender == 1,
		})
		return
	}
	if _, err := s.store.Exec(`UPDATE account_activity SET interruptible=? WHERE user_id=? AND source_kind=?`,
		boolToInt(muted == 0), principal.UserID, activityKind); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"kind":         input.Kind,
		"muted":        muted == 1,
		"show_sender":  showSender == 1,
	})
}

// privateNotificationPreference reads one account's own preference, defaulting to
// the documented quiet defaults rather than to an absent row.
func (s *Server) privateNotificationPreference(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	rows, err := s.store.QueryContext(r.Context(), `SELECT kind,muted,show_sender FROM private_notification_preferences WHERE user_id=? ORDER BY kind`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer rows.Close()
	found := map[string]bool{}
	preferences := []map[string]any{}
	for rows.Next() {
		var kind string
		var muted, showSender int
		if err := rows.Scan(&kind, &muted, &showSender); err != nil {
			s.failed(w, err)
			return
		}
		found[kind] = true
		preferences = append(preferences, map[string]any{"kind": kind, "muted": muted == 1, "show_sender": showSender == 1})
	}
	if err := rows.Err(); err != nil {
		s.failed(w, err)
		return
	}
	// Every known kind is reported, so the interface is not left guessing a default
	// that the server would then apply differently.
	for _, kind := range sortedPrivateKinds() {
		if !found[kind] {
			preferences = append(preferences, map[string]any{"kind": kind, "muted": false, "show_sender": false})
		}
	}
	respond(w, http.StatusOK, map[string]any{"preferences": preferences})
}

func sortedPrivateKinds() []string {
	kinds := make([]string, 0, len(privateActivityKinds))
	for _, kind := range privateActivityKinds {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

// capabilityForActivity is the authority an activity reader needs. Activity is the
// account's own record of what it was told, so it is readable by any signed-in
// account about itself and is not a privileged view of anyone else. The constant is
// referenced so the route policy and this function cannot drift apart.
func capabilityForActivity() authz.Capability { return authz.MessageSend }

// recordDirectMessageActivity addresses a new direct message to the other member.
//
// Only the other member is addressed. The sender is not notified about their own
// message, and a community channel is left to the existing channel notification path
// — this is the private half of the contract, and duplicating the channel one would
// give the two different answers to the same question.
func (s *Server) recordDirectMessageActivity(ctx context.Context, tx *sql.Tx, scope messageScope, senderID, messageID string, now time.Time) error {
	if scope.Kind != "direct" {
		return nil
	}
	var recipientID string
	// Current membership, not the conversation's members list as a whole: a settled or
	// removed member is not addressed by a message sent afterwards.
	err := tx.QueryRowContext(ctx, `SELECT m.user_id FROM conversation_members m JOIN users u ON u.id=m.user_id
		WHERE m.conversation_id=? AND m.user_id<>? AND m.state='accepted' AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0
		LIMIT 1`, scope.ID, senderID).Scan(&recipientID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.recordActivity(ctx, tx, recipientID, activityMessage, messageID, scope.ID, senderID, now)
}

// recordRequestActivity addresses a newly created pending request to its recipient.
//
// The source is the request's attempt, not the conversation: a pair that renews its
// request produces a new attempt and therefore a new event, which is what keeps a
// renewed introduction from being silently folded into the first one (ADR 0015,
// "Stable event identity").
func (s *Server) recordRequestActivity(ctx context.Context, tx *sql.Tx, conversationID, attemptID, requesterID, recipientID string, now time.Time) error {
	if attemptID == "" || recipientID == "" {
		return nil
	}
	return s.recordActivity(ctx, tx, recipientID, activityRequest, attemptID, conversationID, requesterID, now)
}

// deliverableDestination is the dispatch-time authorization check, and it is
// exhaustive by construction: every kind is named, and a kind this build does not
// recognise is refused rather than treated as unchecked. That is the whole point of
// typing the destination — the previous shape read an empty channel as "no check
// needed", which is precisely the case a private notice would arrive in.
func (s *Server) deliverableDestination(job pushJob) bool {
	destination := job.destination
	switch destination.kind {
	case "channel":
		if destination.conversationID == "" {
			return false
		}
		return s.mayNotifyChannel(s.ctx, job.userID, destination.conversationID)
	case activityMessage, activityRequest:
		// A direct conversation is delivered under the message rule and a pending
		// request under the request rule; both re-check current access at dispatch
		// rather than trusting what was true when the notice was queued.
		return s.deliverableActivity(job.userID, destination.kind, destination.conversationID, destination.actorID)
	default:
		return false
	}
}

// payloadForJob builds the body actually sent. A private payload is content-free by
// construction: it says that something happened and, only if the recipient asked for
// it, who did it. A lock-screen preview is the one place private text most easily
// leaks, so the quieter shape is the default and the payload cannot be made to
// carry a message or an introduction (ADR 0015).
func (s *Server) payloadForJob(job pushJob) map[string]string {
	if job.destination.kind == "channel" {
		return job.payload
	}
	payload := map[string]string{
		"url":    "/messages",
		"title":  "BrrpBrrp",
		"body":   "You have new activity",
		// The tag is the client-side de-duplication key and it travels in the push
		// body, so it is built from the source kind and the sender's identity rather
		// than from the conversation: a lock screen can read this string, and a
		// conversation id would tell a bystander which private conversation exists.
		"tag":  job.destination.activityID,
		"kind": job.destination.kind,
	}
	if s.senderMayBeNamed(s.ctx, job.userID, job.destination.kind) {
		if name, ok := s.usernameFor(s.ctx, job.destination.actorID); ok {
			payload["body"] = name + " sent you something"
		}
	}
	return payload
}

// senderMayBeNamed reads the recipient's own preference. An unreadable preference is
// a refusal: a failed read must not widen what a notification says.
func (s *Server) senderMayBeNamed(ctx context.Context, userID, sourceKind string) bool {
	preferenceKind, private := privateActivityKinds[sourceKind]
	if !private {
		return true
	}
	var show int
	if err := s.store.QueryRowContext(ctx, `SELECT show_sender FROM private_notification_preferences WHERE user_id=? AND kind=?`, userID, preferenceKind).Scan(&show); err != nil {
		// No row and an unreadable row are the same case: the documented default is
		// the quiet one, so a lookup that cannot answer does not widen the payload.
		return false
	}
	return show == 1
}

func (s *Server) usernameFor(ctx context.Context, userID string) (string, bool) {
	if userID == "" {
		return "", false
	}
	var username string
	if err := s.store.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, userID).Scan(&username); err != nil {
		return "", false
	}
	return username, true
}

// queueActivityNotice queues an interruption for an activity row that was just
// created. It is a no-op when the row is not interruptible, so a muted account
// produces no queued work at all rather than work that is dropped at dispatch.
func (s *Server) queueActivityNotice(userID, sourceKind, sourceID, conversationID, actorID string) {
	var interruptible int
	var activityID string
	if err := s.store.QueryRow(`SELECT interruptible,id FROM account_activity WHERE user_id=? AND source_kind=? AND source_id=?`, userID, sourceKind, sourceID).Scan(&interruptible, &activityID); err != nil || interruptible == 0 {
		return
	}
	select {
	case s.pushJobs <- pushJob{
		userID: userID,
		destination: pushDestination{
			kind:           sourceKind,
			conversationID: conversationID,
			actorID:        actorID,
			activityID:     activityID,
		},
		payload: map[string]string{},
	}:
	default:
		// A full queue drops the notice rather than blocking the sender. The row
		// already exists, so the fact is not lost — it is waiting in the list, which
		// is what the durable record is for.
	}
}
