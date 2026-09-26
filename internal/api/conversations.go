package api

import (
	"net/http"

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
	clause := "WHERE reply_to_id=? AND channel_id=?"
	args := []any{message.ID, message.ChannelID}
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
	before, err := s.store.Messages(r.Context(), principal.UserID, "WHERE channel_id=? AND id<? ORDER BY id DESC LIMIT 25", message.ChannelID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	for l, h := 0, len(before)-1; l < h; l, h = l+1, h-1 {
		before[l], before[h] = before[h], before[l]
	}
	after, err := s.store.Messages(r.Context(), principal.UserID, "WHERE channel_id=? AND id>? ORDER BY id ASC LIMIT 25", message.ChannelID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, append(append(before, message), after...))
}

func (s *Server) pins(w http.ResponseWriter, r *http.Request, user db.User) {
	channel, _, ok := s.requireChannel(w, r, principalOf(r), r.PathValue("id"))
	if !ok {
		return
	}
	messages, err := s.store.Messages(r.Context(), user.ID, "WHERE channel_id=? AND id IN (SELECT message_id FROM message_pins) ORDER BY id DESC LIMIT 100", channel.ID)
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
	if input.Pinned {
		result, err := s.store.Exec(`INSERT OR IGNORE INTO message_pins(message_id,pinned_by,created_at) SELECT ?,?,? WHERE (SELECT count(*) FROM message_pins WHERE message_id IN (SELECT id FROM messages WHERE channel_id=?))<100`, message.ID, user.ID, db.Now(), message.ChannelID)
		if err != nil {
			s.failed(w, err)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 && !message.Pinned {
			problem(w, 409, "This channel already has 100 pinned messages.")
			return
		}
	} else if _, err := s.store.Exec(`DELETE FROM message_pins WHERE message_id=?`, message.ID); err != nil {
		s.failed(w, err)
		return
	}
	s.audit(user.ID, "message.pin_updated", message.ID)
	s.publishMessage("message.updated", message)
	respond(w, 200, map[string]bool{"ok": true})
}
