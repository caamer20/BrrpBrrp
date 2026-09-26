package db

import (
	"context"
	"database/sql"
	"strings"
)

// Messages accepts a server-owned WHERE/ORDER/LIMIT clause, never user SQL.
func (s *Store) Messages(ctx context.Context, viewer, clause string, args ...any) ([]Message, error) {
	// The conversation scope is read through COALESCE(conversation_id, channel_id), which is the
	// compatibility bridge ADR 0010 defines: a message written before the schema 6 migration
	// still resolves to its channel's conversation, and one written after it resolves to the
	// conversation the send recorded.
	rows, err := s.QueryContext(ctx, `SELECT id,COALESCE(conversation_id,channel_id),channel_id,user_id,content,created_at,COALESCE(edited_at,''),idempotency_key,COALESCE(embed_json,'[]'),COALESCE(reply_to_id,''),EXISTS(SELECT 1 FROM message_pins WHERE message_id=messages.id),
        (SELECT count(*) FROM messages replies WHERE replies.reply_to_id=messages.id)
        FROM messages `+clause, args...)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var embeds string
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.ChannelID, &message.UserID, &message.Content, &message.CreatedAt, &message.EditedAt, &message.IdempotencyKey, &embeds, &message.ReplyToID, &message.Pinned, &message.ReplyCount); err != nil {
			rows.Close()
			return nil, err
		}
		message.Embeds = []byte(embeds)
		message.Attachments = []Attachment{}
		message.Reactions = []Reaction{}
		messages = append(messages, message)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(messages) == 0 {
		return messages, err
	}

	indexes := make(map[string]int, len(messages))
	ids := make([]any, 0, len(messages))
	for i, message := range messages {
		indexes[message.ID] = i
		ids = append(ids, message.ID)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	attachments, err := s.QueryContext(ctx, `SELECT id,message_id,uploader_id,file_name,file_size,mime_type FROM attachments WHERE message_id IN (`+placeholders+`)`, ids...)
	if err != nil {
		return nil, err
	}
	for attachments.Next() {
		var attachment Attachment
		if err := attachments.Scan(&attachment.ID, &attachment.MessageID, &attachment.UploaderID, &attachment.FileName, &attachment.FileSize, &attachment.MIMEType); err != nil {
			attachments.Close()
			return nil, err
		}
		i := indexes[attachment.MessageID]
		messages[i].Attachments = append(messages[i].Attachments, attachment)
	}
	err = attachments.Err()
	attachments.Close()
	if err != nil {
		return nil, err
	}

	reactionArgs := append([]any{viewer}, ids...)
	reactions, err := s.QueryContext(ctx, `SELECT message_id,emoji,count(*),max(user_id=?) FROM reactions WHERE message_id IN (`+placeholders+`) GROUP BY message_id,emoji ORDER BY emoji`, reactionArgs...)
	if err != nil {
		return nil, err
	}
	defer reactions.Close()
	for reactions.Next() {
		var id string
		var reaction Reaction
		if err := reactions.Scan(&id, &reaction.Emoji, &reaction.Count, &reaction.Mine); err != nil {
			return nil, err
		}
		i := indexes[id]
		messages[i].Reactions = append(messages[i].Reactions, reaction)
	}
	return messages, reactions.Err()
}

func (s *Store) Message(ctx context.Context, viewer, id string) (Message, error) {
	messages, err := s.Messages(ctx, viewer, "WHERE id=?", id)
	if err != nil {
		return Message{}, err
	}
	if len(messages) == 0 {
		return Message{}, sql.ErrNoRows
	}
	return messages[0], nil
}
