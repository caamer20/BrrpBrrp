package db

import "context"

// UnreadCount is the client-facing unread shape for one conversation. It is
// defined next to the query so state and the lightweight endpoint cannot drift.
type UnreadCount struct {
	ChannelID      string `json:"channel_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Count          int    `json:"count"`
	Mentions       int    `json:"mentions"`
	LastRead       string `json:"last_read_message_id"`
}

// UnreadCounts counts unseen activity per readable channel. The access condition
// is applied before counting, so a restricted channel contributes no count, no
// mention total and no cursor to an account that cannot read it.
//
// Thread replies are deliberately not counted here. They carry their channel's id so
// that a reply can always be traced to the channel it lives in, but a thread is a
// conversation with its own reading position: counting replies against the channel
// cursor left "1 unread" on a channel whose only unseen activity was a thread the
// reader then opened and read (review 17, V17-03). Channel counts and mentions are
// therefore channel messages only; a thread's own projection is measured against
// thread_reads for the same reader.
func (s *Store) UnreadCounts(ctx context.Context, viewer Viewer) ([]UnreadCount, error) {
	condition, conditionArgs := viewer.accessibleChannelCondition()
	rows, err := s.QueryContext(ctx, `SELECT c.id,
  (SELECT count(*) FROM messages m WHERE m.channel_id=c.id AND COALESCE(m.thread_id,'')='' AND m.id>COALESCE(cr.last_read_message_id,'') AND m.user_id<>?),
  (SELECT count(*) FROM mentions mn WHERE mn.channel_id=c.id AND mn.target_user_id=? AND mn.message_id>COALESCE(cr.last_read_message_id,'')
     AND EXISTS(SELECT 1 FROM messages m WHERE m.id=mn.message_id AND COALESCE(m.thread_id,'')='')),
  COALESCE(cr.last_read_message_id,'')
  FROM channels c LEFT JOIN channel_reads cr ON cr.channel_id=c.id AND cr.user_id=?
  WHERE c.type='text' AND `+condition,
		append([]any{viewer.UserID, viewer.UserID, viewer.UserID}, conditionArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make([]UnreadCount, 0)
	for rows.Next() {
		var count UnreadCount
		if err := rows.Scan(&count.ChannelID, &count.Count, &count.Mentions, &count.LastRead); err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}
