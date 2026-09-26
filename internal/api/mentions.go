package api

import (
	"context"
	"database/sql"
	"regexp"
	"slices"
	"strings"

	"brrpbrrp/internal/db"
)

var mentionPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])@([a-z0-9_]{3,32})\b`)

// mentionNames extracts mentioned account names. A broad @everyone mention only
// counts when the author holds the mention.everyone capability; otherwise it is
// treated as ordinary text and notifies nobody.
func mentionNames(content string, everyone bool) map[string]bool {
	names := make(map[string]bool)
	for _, match := range mentionPattern.FindAllStringSubmatch(content, -1) {
		name := strings.ToLower(match[1])
		if name != "everyone" || everyone {
			names[name] = true
		}
	}
	return names
}

// syncMentions records mentions for accounts that may read the conversation.
// Rendering a mention never grants access: an ineligible recipient is recorded as
// no mention at all, so it produces no count, no notification and no push job.
func syncMentions(ctx context.Context, tx *sql.Tx, messageID, channelID string, actor db.User, content string, eligible []string, everyone bool) ([]string, error) {
	names := mentionNames(content, everyone)
	if _, err := tx.ExecContext(ctx, `DELETE FROM mentions WHERE message_id=?`, messageID); err != nil {
		return nil, err
	}
	targets := make([]string, 0)
	if len(names) == 0 {
		return targets, nil
	}
	query := `SELECT id,username FROM users WHERE is_banned=0 AND id<>?`
	args := []any{actor.ID}
	if len(eligible) > 0 {
		query += ` AND id IN (` + placeholders(len(eligible)) + `)`
		for _, id := range eligible {
			args = append(args, id)
		}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, username string
		if err := rows.Scan(&id, &username); err != nil {
			rows.Close()
			return nil, err
		}
		if names["everyone"] || names[strings.ToLower(username)] {
			targets = append(targets, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range targets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mentions(message_id,channel_id,target_user_id,actor_id,created_at) VALUES(?,?,?,?,?)`, messageID, channelID, id, actor.ID, db.Now()); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func placeholders(count int) string {
	if count <= 0 {
		return "NULL"
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

// notifyMentions delivers a mention notice. Access is re-evaluated here, at
// dispatch time, because the recipient's access may have been removed after the
// message was committed. The notice carries no message text.
func (s *Server) notifyMentions(message db.Message, actor db.User, targets []string) {
	if len(targets) == 0 {
		return
	}
	audience, revision, err := s.notificationAudience(context.Background(), message.ChannelID)
	if err != nil {
		s.log.Warn("mention eligibility lookup failed", "error", err)
		return
	}
	payload := map[string]string{
		"type":       "mention",
		"title":      s.Settings().ServerName,
		"body":       actor.Username + " mentioned you",
		"channel_id": message.ChannelID,
		"message_id": message.ID,
		"url":        "/?channel=" + message.ChannelID + "&message=" + message.ID,
	}
	for _, target := range targets {
		if !slices.Contains(audience, target) {
			continue
		}
		s.hub.BroadcastToRevision([]string{target}, revision, "notification.mention", payload)
		if !s.hub.Online(target) && s.Settings().Push {
			select {
			case s.pushJobs <- pushJob{userID: target, channelID: message.ChannelID, payload: payload}:
			default:
				s.log.Warn("push queue is full")
			}
		}
	}
}
