package api

import (
	"errors"
	"net/http"
	"slices"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// A curated Unicode palette is shared with clients through workspace state.
var reactionPalette = []string{"👍", "❤️", "😂", "🎉", "👀", "🔥", "✅", "👎", "🤔", "😢", "🙌", "🚀", "😀", "😅", "🤣", "😊", "🥰", "😍", "😎", "🤩", "🥳", "😮", "😱", "😭", "🥺", "😴", "🤯", "🫡", "🤝", "👏", "💪", "🙏", "🫶", "✌️", "🤞", "💯", "⭐", "✨", "💡", "🎯", "🏆", "💜", "💙", "💚", "💔", "👋", "☕", "🍕", "🎮", "🐛", "🛠️", "📌", "❌", "⚠️", "🤷", "🙈", "🫠", "🥲", "🍿", "🎵"}

func (s *Server) toggleReaction(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Emoji string `json:"emoji"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !slices.Contains(reactionPalette, input.Emoji) {
		problem(w, 400, "Choose an emoji from the reaction palette.")
		return
	}
	principal := principalOf(r)
	message, _, ok := s.requireMessage(w, r, principal, r.PathValue("id"))
	if !ok {
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// A reaction is a write against the message's conversation, so access is
	// re-checked in the transaction that records it.
	if _, err := s.requireChannelAccessInTx(r.Context(), tx, user, message.ChannelID); err != nil {
		if errors.Is(err, authz.ErrUnavailable) {
			hidden(w)
			return
		}
		problem(w, 403, "You cannot react in this channel.")
		return
	}
	result, err := tx.Exec(`DELETE FROM reactions WHERE message_id=? AND user_id=? AND emoji=?`, message.ID, user.ID, input.Emoji)
	if err != nil {
		s.failed(w, err)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		s.failed(w, err)
		return
	}
	if count == 0 {
		if _, err := tx.Exec(`INSERT INTO reactions(message_id,user_id,emoji,created_at) VALUES(?,?,?,?)`, message.ID, user.ID, input.Emoji, db.Now()); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	message, err = s.store.Message(r.Context(), user.ID, message.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	s.publishMessage("reaction.updated", message)
	respond(w, 200, message.Reactions)
}
