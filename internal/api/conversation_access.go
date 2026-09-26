package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// messageScope identifies the real storage destination. Direct conversations are
// never represented by community channels, so community authority cannot grant
// access to private membership.
type messageScope struct {
	ID        string
	Kind      string
	ChannelID string
	Channel   db.Channel
}

func (s *Server) conversationAccess(ctx context.Context, q db.Queryer, principal authz.Principal, id string, write bool) (messageScope, error) {
	var scope messageScope
	var state string
	err := q.QueryRowContext(ctx, `SELECT id,kind,COALESCE(channel_id,''),state FROM conversations WHERE id=?`, id).Scan(&scope.ID, &scope.Kind, &scope.ChannelID, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, authz.ErrUnavailable
	}
	if err != nil {
		return scope, err
	}
	if scope.Kind == "channel" {
		channel, err := s.store.Channel(ctx, q, scope.ChannelID)
		if err != nil {
			return scope, err
		}
		access, err := s.store.ChannelAccessLists(ctx, q, channel.ID)
		if err != nil {
			return scope, err
		}
		if !principal.MayReadChannel(channel, access[channel.ID]) {
			return scope, authz.ErrUnavailable
		}
		if write && !principal.MayPostChannel(channel, access[channel.ID]) {
			return scope, authz.ErrForbidden
		}
		if channel.Type != "text" {
			return scope, refuse(400, "Messages belong in text conversations.")
		}
		scope.Channel = channel
		return scope, nil
	}
	if !s.config.PrivateConversations || scope.Kind != "direct" || state != "active" || principal.Inactive {
		return scope, authz.ErrUnavailable
	}
	var member bool
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m JOIN users u ON u.id=m.user_id JOIN direct_pairs p ON p.conversation_id=m.conversation_id WHERE m.conversation_id=? AND m.user_id=? AND m.state='accepted' AND u.is_deleted=0 AND u.is_banned=0 AND u.is_deactivated=0 AND (p.user_low=m.user_id OR p.user_high=m.user_id))`, id, principal.UserID).Scan(&member)
	if err != nil {
		return scope, err
	}
	if !member {
		return scope, authz.ErrUnavailable
	}
	if write {
		if !principal.Allowed(authz.MessageSend) {
			return scope, authz.ErrForbidden
		}
		var permitted bool
		err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM conversation_members m JOIN users u ON u.id=m.user_id WHERE m.conversation_id=p.conversation_id AND m.state='accepted' AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0 AND m.user_id IN(p.user_low,p.user_high))=2 AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=p.user_low AND b.blocked_id=p.user_high) OR (b.blocker_id=p.user_high AND b.blocked_id=p.user_low)) FROM direct_pairs p WHERE p.conversation_id=?`, id).Scan(&permitted)
		if err != nil {
			return scope, err
		}
		if !permitted {
			return scope, refuse(403, "This conversation is read-only. Contact is blocked or the other account is unavailable.")
		}
	}
	return scope, nil
}

func (s *Server) scopeInTx(ctx context.Context, tx *sql.Tx, user db.User, id string, write bool) (messageScope, authz.Principal, error) {
	principal, err := s.authorizeInTx(ctx, tx, user)
	if err != nil {
		return messageScope{}, principal, err
	}
	scope, err := s.conversationAccess(ctx, tx, principal, id, write)
	return scope, principal, err
}

func (s *Server) scopeFailed(w http.ResponseWriter, err error) {
	var refused problemError
	switch {
	case errors.Is(err, authz.ErrUnavailable), errors.Is(err, sql.ErrNoRows):
		hidden(w)
	case errors.As(err, &refused):
		problem(w, refused.Status, refused.Message)
	default:
		s.authorityFailed(w, err)
	}
}

func (s *Server) requireScope(w http.ResponseWriter, r *http.Request, user db.User, id string, write bool) (messageScope, authz.Principal, bool) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return messageScope{}, authz.Principal{}, false
	}
	defer tx.Rollback()
	scope, principal, err := s.scopeInTx(r.Context(), tx, user, id, write)
	if err != nil {
		s.scopeFailed(w, err)
		return scope, principal, false
	}
	if err = tx.Commit(); err != nil {
		s.failed(w, err)
		return scope, principal, false
	}
	return scope, principal, true
}

func (s *Server) conversationAudienceIDs(ctx context.Context, q db.Queryer, id string) ([]string, int64, error) {
	var kind string
	if err := q.QueryRowContext(ctx, `SELECT kind FROM conversations WHERE id=?`, id).Scan(&kind); err != nil {
		return nil, 0, err
	}
	if kind == "channel" {
		return s.channelAudienceIDs(ctx, q, id)
	}
	if !s.config.PrivateConversations || kind != "direct" {
		return nil, 0, authz.ErrUnavailable
	}
	rows, err := q.QueryContext(ctx, `SELECT m.user_id FROM conversation_members m JOIN conversations c ON c.id=m.conversation_id JOIN direct_pairs p ON p.conversation_id=c.id JOIN users u ON u.id=m.user_id WHERE c.id=? AND c.state='active' AND m.state='accepted' AND m.user_id IN(p.user_low,p.user_high) AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0`, id)
	if err != nil {
		return nil, 0, err
	}
	audience := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		audience = append(audience, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	revision, err := s.store.AuthorizationRevision(ctx, q)
	return audience, revision, err
}

func (s *Server) publishToConversation(ctx context.Context, id, kind string, data any) {
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	audience, revision, err := s.conversationAudienceIDs(ctx, tx, id)
	if err != nil {
		return
	}
	// Retained-history readers are not necessarily eligible for new contact.
	// A message may have committed before a block/deactivation and reached this
	// publisher afterward. Resolve this distinction with the audience and revision
	// in one snapshot, rather than stamping that delayed event with a fresh revision.
	// Deletion updates still reach history readers so erased content can disappear.
	switch kind {
	case "message.created", "message.updated", "reaction.updated", "message.embed_updated":
		var direct, blocked bool
		err := tx.QueryRowContext(ctx, `SELECT c.kind='direct',EXISTS(SELECT 1 FROM direct_pairs p JOIN user_blocks b ON (b.blocker_id=p.user_low AND b.blocked_id=p.user_high) OR (b.blocker_id=p.user_high AND b.blocked_id=p.user_low) WHERE p.conversation_id=c.id) FROM conversations c WHERE c.id=?`, id).Scan(&direct, &blocked)
		if err != nil || (direct && (len(audience) != 2 || blocked)) {
			return
		}
	}
	if tx.Commit() != nil {
		return
	}
	if s.authorizationRevision(ctx) != revision {
		return
	}
	s.hub.BroadcastToRevision(audience, revision, kind, data)
}

// Community audit readers do not acquire private-conversation activity metadata.
func (s *Server) auditCommunityMessageTx(ctx context.Context, tx *sql.Tx, scope messageScope, actor, action, target string) error {
	if scope.Kind != "channel" {
		return nil
	}
	return s.auditTx(ctx, tx, actor, action, target)
}
