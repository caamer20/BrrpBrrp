package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"brrpbrrp/internal/db"
)

// Pending introductions are not retained conversation history. Ending either
// identity cancels these requests in the same transaction as account removal.
func cancelPendingDirectRequests(ctx context.Context, tx *sql.Tx, userID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT conversation_id FROM direct_requests WHERE state='pending' AND (requester_id=? OR recipient_id=?)`, userID, userID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := rejectDirectRequest(ctx, tx, id); err != nil {
			return err
		}
	}
	// Introductions are auxiliary request text, not retained message history.
	// Account erasure must not leave an author's text outside message cleanup.
	_, err = tx.ExecContext(ctx, `UPDATE direct_requests SET introduction='' WHERE requester_id=?`, userID)
	return err
}

func (s *Server) contactPreferences(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, _, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	allow := true
	if err := tx.QueryRowContext(r.Context(), `SELECT COALESCE((SELECT allow_requests FROM contact_preferences WHERE user_id=?),1)`, user.ID).Scan(&allow); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]bool{"allow_requests": allow})
}

func (s *Server) setContactPreferences(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Allow *bool `json:"allow_requests"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Allow == nil {
		problem(w, 400, "Choose whether to receive new direct requests.")
		return
	}
	tx, _, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO contact_preferences(user_id,allow_requests,updated_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET allow_requests=excluded.allow_requests,updated_at=excluded.updated_at`, user.ID, *input.Allow, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.signalDirectChange(user.ID)
	respond(w, 200, map[string]bool{"allow_requests": *input.Allow})
}

func (s *Server) listBlocks(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, _, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), `SELECT `+db.QualifiedUserColumns+` FROM user_blocks JOIN users ON users.id=user_blocks.blocked_id WHERE user_blocks.blocker_id=? ORDER BY users.username`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	blocked := make([]db.User, 0)
	for rows.Next() {
		peer, err := db.ScanUser(rows)
		if err != nil {
			rows.Close()
			s.failed(w, err)
			return
		}
		if peer.IsDeleted {
			peer = db.User{ID: peer.ID, DisplayName: "Deleted member", IsDeleted: true, IsDeactivated: true}
		}
		blocked = append(blocked, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, blocked)
}

func (s *Server) blockContact(w http.ResponseWriter, r *http.Request, user db.User) {
	s.changeContactBlock(w, r, user, true)
}
func (s *Server) unblockContact(w http.ResponseWriter, r *http.Request, user db.User) {
	s.changeContactBlock(w, r, user, false)
}

func (s *Server) changeContactBlock(w http.ResponseWriter, r *http.Request, user db.User, block bool) {
	target := r.PathValue("id")
	if target == "" || target == user.ID {
		problem(w, 400, "Choose another member.")
		return
	}
	tx, _, ok := s.directTransaction(w, r, user)
	if !ok {
		return
	}
	defer tx.Rollback()
	if block {
		peer, err := s.store.Account(r.Context(), tx, target)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && peer.IsDeleted) {
			hidden(w)
			return
		}
		if err != nil {
			s.failed(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO user_blocks(blocker_id,blocked_id,created_at) VALUES(?,?,?)`, user.ID, target, db.Now()); err != nil {
			s.failed(w, err)
			return
		}
		// Blocking a pending request closes it and removes the introduction. An
		// accepted pair remains accepted: its history is retained but read-only.
		low, high := orderedDirectPair(user.ID, target)
		var id string
		err = tx.QueryRowContext(r.Context(), `SELECT r.conversation_id FROM direct_pairs p JOIN direct_requests r ON r.conversation_id=p.conversation_id WHERE p.user_low=? AND p.user_high=? AND r.state='pending'`, low, high).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			s.failed(w, err)
			return
		}
		if err == nil {
			if err := rejectDirectRequest(r.Context(), tx, id); err != nil {
				s.failed(w, err)
				return
			}
		}
	} else if _, err := tx.ExecContext(r.Context(), `DELETE FROM user_blocks WHERE blocker_id=? AND blocked_id=?`, user.ID, target); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.commitDirectChange(r.Context(), tx, user.ID, target); err != nil {
		s.failed(w, err)
		return
	}
	s.signalDirectChange(user.ID, target)
	respond(w, 200, map[string]bool{"blocked": block})
}
