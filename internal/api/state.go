package api

import (
	"context"
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// unreadCount is the storage layer's shape, aliased so the JSON contract has a
// single definition.
type unreadCount = db.UnreadCount

// unread is the snapshot for one account, scoped to what that account may read.
func (s *Server) unread(userID string) ([]db.UnreadCount, error) {
	return s.store.UnreadCounts(context.Background(), db.ViewerFor(userID, false))
}

func (s *Server) unreadState(w http.ResponseWriter, r *http.Request, user db.User) {
	counts, err := s.store.UnreadCounts(r.Context(), viewer(principalOf(r)))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, counts)
}

func (s *Server) users(includeBanned bool) ([]db.User, error) {
	query := `SELECT ` + userColumns + ` FROM users`
	if !includeBanned {
		query += ` WHERE is_banned=0`
	}
	rows, err := s.store.Query(query + ` ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]db.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// state returns the bootstrap snapshot. Every part of it is scoped to the caller:
// a channel it cannot read contributes neither a title, a count nor a cursor.
func (s *Server) state(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	channels, err := s.store.AccessibleChannels(r.Context(), viewer(principal), "")
	if err != nil {
		s.failed(w, err)
		return
	}
	users, err := s.users(false)
	if err != nil {
		s.failed(w, err)
		return
	}
	unread, err := s.store.UnreadCounts(r.Context(), viewer(principal))
	if err != nil {
		s.failed(w, err)
		return
	}
	// Category names are shared workspace structure, but the channel counts they
	// carry are scoped to what the caller may read, so a category holding only
	// restricted channels reports no count to an account that cannot open them.
	categories, err := s.store.Categories(r.Context(), s.store, viewer(principal))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, map[string]any{
		"user": user, "channels": channels, "users": users, "settings": s.Settings(), "unread": unread,
		"reaction_palette": reactionPalette, "presence": s.visiblePresence(), "push_public_key": s.vapidPublic,
		"voice_states": s.visibleVoiceStates(r.Context(), principal), "categories": categories,
		"authorization": map[string]any{
			"revision":     principal.Revision,
			"is_owner":     principal.IsOwner,
			"capabilities": capNames(principal),
			// owner_designated reports whether an owner exists now, and
			// ownership_established whether the ceremony has ever completed. The
			// second is what decides the pre-ownership compatibility bridge, so
			// the client can tell "not yet set up" from "ownership was removed".
			"owner_designated":      principal.OwnerDesignated,
			"ownership_established": principal.OwnershipEstablished,
		},
	})
}

// capNames exposes the caller's effective grants so the client can hide what the
// server would refuse. It is presentation only; the server never trusts it.
func capNames(principal authz.Principal) []string {
	names := make([]string, 0, len(authz.Catalog))
	for _, capability := range principal.Capabilities() {
		names = append(names, string(capability))
	}
	return names
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		MessageID string `json:"last_read_message_id"`
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
	// Read state is personal data too: a request admitted before sign-out or
	// account removal cannot recreate it after the identity cleanup committed.
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	readable, err := s.store.ChannelAccessible(r.Context(), tx, viewer(principal), r.PathValue("id"))
	if err != nil {
		s.failed(w, err)
		return
	}
	if !readable {
		hidden(w)
		return
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM messages WHERE id=? AND channel_id=?`, input.MessageID, r.PathValue("id")).Scan(&id)
	if !s.found(w, err) {
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO channel_reads(user_id,channel_id,last_read_message_id,last_read_at) VALUES(?,?,?,?) ON CONFLICT(user_id,channel_id) DO UPDATE SET last_read_message_id=max(last_read_message_id,excluded.last_read_message_id),last_read_at=excluded.last_read_at`, user.ID, r.PathValue("id"), id, db.Now())
	if err != nil {
		s.failed(w, err)
		return
	}
	revision, err := s.store.AuthorizationRevision(r.Context(), tx)
	if err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.hub.BroadcastToRevision([]string{user.ID}, revision, "unread.updated", map[string]string{"channel_id": r.PathValue("id")})
	respond(w, 200, map[string]bool{"ok": true})
}
