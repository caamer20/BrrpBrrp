package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

const directRequestCooldown = 24 * time.Hour

type directConversation struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	ChannelID         string  `json:"channel_id,omitempty"`
	State             string  `json:"state"`
	Peer              db.User `json:"peer"`
	CanRead           bool    `json:"can_read"`
	CanWrite          bool    `json:"can_write"`
	BlockedByMe       bool    `json:"blocked_by_me"`
	Blocked           bool    `json:"blocked"`
	RequestState      string  `json:"request_state"`
	RequestDirection  string  `json:"request_direction"`
	Introduction      string  `json:"introduction"`
	Hidden            bool    `json:"hidden"`
	Count             int     `json:"count"`
	Mentions          int     `json:"mentions"`
	LastReadMessageID string  `json:"last_read_message_id"`
	UpdatedAt         string  `json:"updated_at"`
}

func (s *Server) requirePrivateConversations(w http.ResponseWriter) bool {
	if s.config.PrivateConversations {
		return true
	}
	problem(w, http.StatusConflict, "Private conversations are not enabled on this server.")
	return false
}

func (s *Server) directTransaction(w http.ResponseWriter, r *http.Request, user db.User) (*sql.Tx, authz.Principal, bool) {
	if !s.requirePrivateConversations(w) {
		return nil, authz.Principal{}, false
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return nil, authz.Principal{}, false
	}
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		tx.Rollback()
		s.authorityFailed(w, err)
		return nil, principal, false
	}
	return tx, principal, true
}

// Permission comes from current accepted membership, never the pair's stable IDs
// or an administrator role. Pair IDs survive account erasure only to identify history.
func (s *Server) describeDirect(ctx context.Context, q db.Queryer, user db.User, principal authz.Principal, id string) (directConversation, error) {
	d := directConversation{Kind: "direct", RequestDirection: "none"}
	var low, high, requester, recipient string
	err := q.QueryRowContext(ctx, `SELECT c.id,c.state,c.updated_at,p.user_low,p.user_high,r.requester_id,r.recipient_id,r.state,r.introduction,COALESCE(pref.hidden,0) FROM conversations c JOIN direct_pairs p ON p.conversation_id=c.id JOIN direct_requests r ON r.conversation_id=c.id LEFT JOIN direct_preferences pref ON pref.conversation_id=c.id AND pref.user_id=? WHERE c.id=? AND c.kind='direct' AND (p.user_low=? OR p.user_high=?)`, user.ID, id, user.ID, user.ID).Scan(&d.ID, &d.State, &d.UpdatedAt, &low, &high, &requester, &recipient, &d.RequestState, &d.Introduction, &d.Hidden)
	if err != nil {
		return d, err
	}
	peerID := low
	if peerID == user.ID {
		peerID = high
	}
	d.Peer, err = s.store.Account(ctx, q, peerID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && d.Peer.IsDeleted) {
		d.Peer = db.User{ID: peerID, DisplayName: "Deleted member", IsDeleted: true, IsDeactivated: true}
	} else if err != nil {
		return d, err
	}
	if requester == user.ID {
		d.RequestDirection = "outgoing"
	} else if recipient == user.ID {
		d.RequestDirection = "incoming"
	}
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_blocks WHERE blocker_id=? AND blocked_id=?),EXISTS(SELECT 1 FROM user_blocks WHERE (blocker_id=? AND blocked_id=?) OR (blocker_id=? AND blocked_id=?))`, user.ID, peerID, user.ID, peerID, peerID, user.ID).Scan(&d.BlockedByMe, &d.Blocked); err != nil {
		return d, err
	}
	_, readErr := s.conversationAccess(ctx, q, principal, id, false)
	if readErr == nil {
		d.CanRead = true
	} else if !errors.Is(readErr, authz.ErrUnavailable) {
		return d, readErr
	}
	if d.CanRead {
		_, writeErr := s.conversationAccess(ctx, q, principal, id, true)
		var refused problemError
		if writeErr == nil {
			d.CanWrite = true
		} else if !errors.Is(writeErr, authz.ErrForbidden) && !errors.As(writeErr, &refused) {
			return d, writeErr
		}
		if err := q.QueryRowContext(ctx, `SELECT COALESCE((SELECT last_read_message_id FROM conversation_reads WHERE user_id=? AND conversation_id=?),'')`, user.ID, id).Scan(&d.LastReadMessageID); err != nil {
			return d, err
		}
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE conversation_id=? AND id>? AND user_id<>?`, id, d.LastReadMessageID, user.ID).Scan(&d.Count); err != nil {
			return d, err
		}
	}
	return d, nil
}

func (s *Server) directConversations(ctx context.Context, user db.User) ([]directConversation, error) {
	result := make([]directConversation, 0)
	if !s.config.PrivateConversations {
		return result, nil
	}
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.conversation_id FROM direct_pairs p JOIN conversations c ON c.id=p.conversation_id WHERE p.user_low=? OR p.user_high=? ORDER BY c.updated_at DESC,c.id`, user.ID, user.ID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		d, err := s.describeDirect(ctx, tx, user, principal, id)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// outstandingIncomingRequests counts the requests this account can still act on.
//
// It is derived from durable current state rather than accumulated, so it is
// correct on a cold start, after a restart, and for a device that missed every
// event while it was closed. Only requests addressed to this account, still
// pending, from an available identity that has not opted out and is not blocked
// in either direction are counted: a request the recipient could not act on
// anyway must not sit in a badge forever.
//
// It is a count, not an activity history. It deliberately does not read request
// introductions, so a closed workspace can draw a badge without the account's
// pending requests being pulled into a panel that is not open (ADR 0013).
func (s *Server) outstandingIncomingRequests(ctx context.Context, q db.Queryer, user db.User) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM direct_requests r
		JOIN conversations c ON c.id=r.conversation_id AND c.kind='direct' AND c.state='pending'
		JOIN direct_pairs p ON p.conversation_id=r.conversation_id
		JOIN users u ON u.id=r.requester_id
		WHERE r.recipient_id=? AND r.state='pending' AND r.requester_id IN(p.user_low,p.user_high)
		AND u.is_deleted=0 AND u.is_banned=0 AND u.is_deactivated=0
		AND COALESCE((SELECT cp.allow_requests FROM contact_preferences cp WHERE cp.user_id=r.requester_id),1)=1
		AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=r.requester_id AND b.blocked_id=?) OR (b.blocker_id=? AND b.blocked_id=r.requester_id))`,
		user.ID, user.ID, user.ID).Scan(&count)
	return count, err
}

func (s *Server) listDirectConversations(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requirePrivateConversations(w) {
		return
	}
	directs, err := s.directConversations(r.Context(), user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	respond(w, http.StatusOK, directs)
}

func orderedDirectPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func (s *Server) commitDirectChange(ctx context.Context, tx *sql.Tx, accounts ...string) error {
	if _, err := db.BumpAuthorizationRevision(ctx, tx); err != nil {
		return err
	}
	return s.commitAuthorityChange(ctx, tx, "Your direct conversation access changed.", accounts...)
}

func (s *Server) signalDirectChange(accounts ...string) {
	for _, id := range accounts {
		s.hub.SendUser(id, "direct.changed", map[string]bool{"refresh": true})
	}
}

func (s *Server) createDirectConversation(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		UserID       string `json:"user_id"`
		Introduction string `json:"introduction"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Introduction = strings.TrimSpace(input.Introduction)
	if input.UserID == "" || input.UserID == user.ID || !utf8.ValidString(input.Introduction) || utf8.RuneCountInString(input.Introduction) > 500 {
		problem(w, 400, "Choose another member and an introduction of up to 500 characters.")
		return
	}
	tx, principal, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	low, high := orderedDirectPair(user.ID, input.UserID)
	var id, state, retryAfter string
	err := tx.QueryRowContext(r.Context(), `SELECT p.conversation_id,r.state,r.retry_after FROM direct_pairs p JOIN direct_requests r ON r.conversation_id=p.conversation_id WHERE p.user_low=? AND p.user_high=?`, low, high).Scan(&id, &state, &retryAfter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.failed(w, err)
		return
	}
	// Opening an existing pair never resends its introduction or converts a
	// reciprocal request into consent. It also reopens accepted retained history.
	if err == nil && state != "rejected" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO direct_preferences(user_id,conversation_id,hidden,updated_at) VALUES(?,?,0,?) ON CONFLICT(user_id,conversation_id) DO UPDATE SET hidden=0,updated_at=excluded.updated_at`, user.ID, id, db.Now()); err != nil {
			s.failed(w, err)
			return
		}
		d, err := s.describeDirect(r.Context(), tx, user, principal, id)
		if err != nil {
			s.failed(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			s.failed(w, err)
			return
		}
		s.signalDirectChange(user.ID)
		respond(w, 200, d)
		return
	}
	if !s.requireCapability(w, principal, authz.MessageSend) {
		return
	}
	peer, err := s.store.Account(r.Context(), tx, input.UserID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (peer.IsBanned || peer.IsDeactivated || peer.IsDeleted)) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	var blocked, allow bool
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_blocks WHERE (blocker_id=? AND blocked_id=?) OR (blocker_id=? AND blocked_id=?)),COALESCE((SELECT allow_requests FROM contact_preferences WHERE user_id=?),1)`, user.ID, peer.ID, peer.ID, user.ID, peer.ID).Scan(&blocked, &allow); err != nil {
		s.failed(w, err)
		return
	}
	if blocked || !allow {
		hidden(w)
		return
	}
	nowTime := time.Now().UTC()
	now := db.Timestamp(nowTime)
	if retryAfter != "" {
		deadline, err := time.Parse(time.RFC3339Nano, retryAfter)
		if err != nil {
			s.failed(w, err)
			return
		}
		if remaining := deadline.Sub(nowTime); remaining > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int((remaining+time.Second-1)/time.Second)))
			problem(w, 429, "This request was declined or withdrawn. Wait 24 hours before requesting again.")
			return
		}
	}
	var outgoing, incoming, recent int
	if err := tx.QueryRowContext(r.Context(), `SELECT (SELECT count(*) FROM direct_requests WHERE requester_id=? AND state='pending'),(SELECT count(*) FROM direct_requests WHERE recipient_id=? AND state='pending'),(SELECT count(*) FROM direct_requests WHERE requester_id=? AND requested_at>?)`, user.ID, peer.ID, user.ID, db.Timestamp(nowTime.Add(-24*time.Hour))).Scan(&outgoing, &incoming, &recent); err != nil {
		s.failed(w, err)
		return
	}
	if outgoing >= 5 || recent >= 10 {
		w.Header().Set("Retry-After", "86400")
		problem(w, 429, "You can have five pending requests and start ten requests in 24 hours. Try again later.")
		return
	}
	if incoming >= 20 {
		problem(w, 429, "This member cannot receive another request right now.")
		return
	}
	created := id == ""
	if created {
		id = db.ID()
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO conversations(id,kind,state,created_at,updated_at) VALUES(?,'direct','pending',?,?)`, id, now, now); err != nil {
			s.failed(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO direct_pairs(conversation_id,user_low,user_high) VALUES(?,?,?)`, id, low, high); err != nil {
			s.failed(w, err)
			return
		}
	} else if _, err := tx.ExecContext(r.Context(), `UPDATE conversations SET state='pending',updated_at=? WHERE id=?`, now, id); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO direct_requests(conversation_id,requester_id,recipient_id,state,introduction,requested_at,updated_at,retry_after) VALUES(?,?,?,'pending',?,?,?,'') ON CONFLICT(conversation_id) DO UPDATE SET requester_id=excluded.requester_id,recipient_id=excluded.recipient_id,state='pending',introduction=excluded.introduction,requested_at=excluded.requested_at,updated_at=excluded.updated_at,retry_after=''`, id, user.ID, peer.ID, input.Introduction, now, now); err != nil {
		s.failed(w, err)
		return
	}
	for _, member := range []struct{ id, state string }{{user.ID, "accepted"}, {peer.ID, "pending"}} {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO conversation_members(conversation_id,user_id,state,invited_by,joined_at,updated_at) VALUES(?,?,?,?,NULL,?) ON CONFLICT(conversation_id,user_id) DO UPDATE SET state=excluded.state,invited_by=excluded.invited_by,joined_at=NULL,updated_at=excluded.updated_at`, id, member.id, member.state, user.ID, now); err != nil {
			s.failed(w, err)
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE direct_preferences SET hidden=0,updated_at=? WHERE conversation_id=?`, now, id); err != nil {
		s.failed(w, err)
		return
	}
	d, err := s.describeDirect(r.Context(), tx, user, principal, id)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitDirectChange(r.Context(), tx, user.ID, peer.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.signalDirectChange(user.ID, peer.ID)
	status := 200
	if created {
		status = 201
	}
	respond(w, status, d)
}

func (s *Server) acceptDirectConversation(w http.ResponseWriter, r *http.Request, user db.User) {
	s.answerDirectConversation(w, r, user, true)
}
func (s *Server) rejectDirectConversation(w http.ResponseWriter, r *http.Request, user db.User) {
	s.answerDirectConversation(w, r, user, false)
}

func (s *Server) answerDirectConversation(w http.ResponseWriter, r *http.Request, user db.User, accept bool) {
	tx, principal, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	d, err := s.describeDirect(r.Context(), tx, user, principal, id)
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if accept && d.RequestDirection != "incoming" {
		problem(w, 403, "Only the recipient can accept this request.")
		return
	}
	if (accept && d.RequestState == "accepted") || (!accept && d.RequestState == "rejected") {
		tx.Rollback()
		respond(w, 200, d)
		return
	}
	if d.RequestState != "pending" {
		problem(w, 409, "This request is no longer pending.")
		return
	}
	now := db.Now()
	if accept {
		if d.Blocked || d.Peer.IsDeleted || d.Peer.IsBanned || d.Peer.IsDeactivated {
			problem(w, 409, "This request cannot be accepted while contact is blocked or the member is unavailable.")
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE direct_requests SET state='accepted',updated_at=?,retry_after='' WHERE conversation_id=?`, now, id); err != nil {
			s.failed(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE conversations SET state='active',updated_at=? WHERE id=?`, now, id); err != nil {
			s.failed(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE conversation_members SET state='accepted',joined_at=COALESCE(joined_at,?),updated_at=? WHERE conversation_id=?`, now, now, id); err != nil {
			s.failed(w, err)
			return
		}
	} else if err := rejectDirectRequest(r.Context(), tx, id); err != nil {
		s.failed(w, err)
		return
	}
	d, err = s.describeDirect(r.Context(), tx, user, principal, id)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitDirectChange(r.Context(), tx, user.ID, d.Peer.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.signalDirectChange(user.ID, d.Peer.ID)
	respond(w, 200, d)
}

func rejectDirectRequest(ctx context.Context, tx *sql.Tx, id string) error {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE direct_requests SET state='rejected',introduction='',updated_at=?,retry_after=? WHERE conversation_id=?`, db.Timestamp(now), db.Timestamp(now.Add(directRequestCooldown)), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET state='closed',updated_at=? WHERE id=?`, db.Timestamp(now), id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE conversation_members SET state='removed',updated_at=? WHERE conversation_id=?`, db.Timestamp(now), id)
	return err
}

func (s *Server) setDirectVisibility(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Hidden *bool `json:"hidden"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Hidden == nil {
		problem(w, 400, "Choose whether to hide this conversation.")
		return
	}
	tx, principal, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	d, err := s.describeDirect(r.Context(), tx, user, principal, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO direct_preferences(user_id,conversation_id,hidden,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id,conversation_id) DO UPDATE SET hidden=excluded.hidden,updated_at=excluded.updated_at`, user.ID, d.ID, *input.Hidden, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	d.Hidden = *input.Hidden
	s.signalDirectChange(user.ID)
	respond(w, 200, d)
}
