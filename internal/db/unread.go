package db

import "context"

// UnreadCount is the client-facing unread shape for one conversation. It is
// defined next to the query so state and the lightweight endpoint cannot drift.
type UnreadCount struct {
	ChannelID string `json:"channel_id"`
	Count     int    `json:"count"`
	Mentions  int    `json:"mentions"`
	LastRead  string `json:"last_read_message_id"`
}

// UnreadCounts counts unseen activity per readable channel. The access condition
// is applied before counting, so a restricted channel contributes no count, no
// mention total and no cursor to an account that cannot read it.
func (s *Store) UnreadCounts(ctx context.Context, viewer Viewer) ([]UnreadCount, error) {
	condition, conditionArgs := viewer.accessibleChannelCondition()
	rows, err := s.QueryContext(ctx, `SELECT c.id,
  (SELECT count(*) FROM messages m WHERE m.channel_id=c.id AND m.id>COALESCE(cr.last_read_message_id,'') AND m.user_id<>?),
  (SELECT count(*) FROM mentions mn WHERE mn.channel_id=c.id AND mn.target_user_id=? AND mn.message_id>COALESCE(cr.last_read_message_id,'')),
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
