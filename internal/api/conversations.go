package api

import (
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

// Replies use the existing reply relationship, so old conversations immediately
// gain a focused view. Every reply remains searchable in its original channel.
func (s *Server) messageReplies(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	message, _, ok := s.requireMessage(w, r, principal, r.PathValue("id"))
	if !ok {
		return
	}
	clause := "WHERE reply_to_id=? AND conversation_id=?"
	args := []any{message.ID, message.ConversationID}
	if before := r.URL.Query().Get("before"); before != "" {
		if _, err := uuid.Parse(before); err != nil {
			problem(w, 400, "Choose a valid reply position.")
			return
		}
		clause += " AND id<?"
		args = append(args, before)
	}
	limit := pageLimit(r)
	args = append(args, limit+1)
	replies, err := s.store.Messages(r.Context(), principal.UserID, clause+" ORDER BY id DESC LIMIT ?", args...)
	if err != nil {
		s.failed(w, err)
		return
	}
	hasOlder := len(replies) > limit
	if hasOlder {
		replies = replies[:limit]
	}
	for l, h := 0, len(replies)-1; l < h; l, h = l+1, h-1 {
		replies[l], replies[h] = replies[h], replies[l]
	}
	respond(w, 200, map[string]any{"parent": message, "replies": replies, "has_older": hasOlder})
}

func (s *Server) messageContext(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	message, _, ok := s.requireMessage(w, r, principal, r.PathValue("id"))
	if !ok {
		return
	}
	before, err := s.store.Messages(r.Context(), principal.UserID, "WHERE conversation_id=? AND id<? ORDER BY id DESC LIMIT 25", message.ConversationID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	for l, h := 0, len(before)-1; l < h; l, h = l+1, h-1 {
		before[l], before[h] = before[h], before[l]
	}
	after, err := s.store.Messages(r.Context(), principal.UserID, "WHERE conversation_id=? AND id>? ORDER BY id ASC LIMIT 25", message.ConversationID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, append(append(before, message), after...))
}

func (s *Server) pins(w http.ResponseWriter, r *http.Request, user db.User) {
	scope, _, ok := s.requireScope(w, r, user, r.PathValue("id"), false)
	if !ok {
		return
	}
	messages, err := s.store.Messages(r.Context(), user.ID, "WHERE conversation_id=? AND id IN (SELECT message_id FROM message_pins) ORDER BY id DESC LIMIT 100", scope.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, messages)
}

func (s *Server) pinMessage(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Pinned bool `json:"pinned"`
	}
	if !decode(w, r, &input) {
		return
	}
	message, _, ok := s.requireMessage(w, r, principalOf(r), r.PathValue("id"))
	if !ok {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	scope, current, err := s.scopeInTx(r.Context(), tx, user, message.ConversationID, true)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	if !current.Allowed(authz.MessagePin) {
		s.scopeFailed(w, authz.ErrForbidden)
		return
	}
	if input.Pinned {
		result, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO message_pins(message_id,pinned_by,created_at) SELECT ?,?,? WHERE (SELECT count(*) FROM message_pins WHERE message_id IN (SELECT id FROM messages WHERE conversation_id=?))<100`, message.ID, user.ID, db.Now(), scope.ID)
		if err != nil {
			s.failed(w, err)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 && !message.Pinned {
			problem(w, 409, "This conversation already has 100 pinned messages.")
			return
		}
	} else if _, err := tx.ExecContext(r.Context(), `DELETE FROM message_pins WHERE message_id=?`, message.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditCommunityMessageTx(r.Context(), tx, scope, user.ID, "message.pin_updated", message.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	s.publishMessage("message.updated", message)
	respond(w, 200, map[string]bool{"ok": true})
}
