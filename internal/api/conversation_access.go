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
		// A community timeout reaches a community channel, even on a path that also
		// serves private conversations. The middleware cannot make this call — the same
		// URL is a channel for one id and a private conversation for another — so the
		// handler makes it, and it is refused here with the same explanation the
		// middleware uses so the two answers are the same words.
		//
		// `Inactive` is deliberately not consulted: a ban already ended the session, so
		// a request that got this far is either not a ban or arrived from an admission
		// decided before it. The middleware owns that refusal.
		if principal.Suspended {
			return scope, refuse(403, "This account is suspended until a moderation timeout expires. Contact a moderator if you think this is a mistake.")
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
	// A private conversation is a direct pair or a group, and the only thing that
	// distinguishes them for access is the shape of the membership: a direct pair
	// holds exactly two accepted members, a group holds up to ten. Membership is
	// checked once for both, rather than the direct case being written out and the
	// group case added beside it — a second copy of this check is exactly how the
	// two would come to disagree about who may read what (ADR 0016).
	// `Inactive` is not consulted here either. A banned or deactivated account is
	// refused at the middleware and cannot reach this point with a live session, and a
	// timeout is *not* a reason to refuse private access — that conflation is what
	// made a community timeout an installation ban (ADR 0018).
	if !s.config.PrivateConversations || (scope.Kind != "direct" && scope.Kind != "group") || state != "active" {
		return scope, authz.ErrUnavailable
	}
	var member bool
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m JOIN users u ON u.id=m.user_id
		WHERE m.conversation_id=? AND m.user_id=? AND m.state='accepted' AND u.is_deleted=0 AND u.is_banned=0 AND u.is_deactivated=0)`, id, principal.UserID).Scan(&member)
	if err != nil {
		return scope, err
	}
	if !member {
		return scope, authz.ErrUnavailable
	}
	// A direct pair additionally needs a partner who is still usable, so a
	// conversation cannot keep accepting messages for one person while the other is
	// deactivated. A group does not: its membership is the consent, and a member who
	// is deactivated simply stops being counted.
	// A direct pair needs its other half to still be a member, and only a member:
	// erasure cascades the membership row away, so the erased peer is represented by
	// the conversation's surviving half plus the tombstone the read path renders.
	//
	// The check does not ask whether that member is *usable*. Blocking is not
	// erasure, and deactivation retains history, so requiring usability here would
	// undo ADR 0011 and the deactivation contract. The cases where an unusable peer
	// should stop a *write* are handled in the write branch below, where they belong.
	if scope.Kind == "direct" {
		var partner int
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m
			WHERE m.conversation_id=? AND m.user_id<>? AND m.state='accepted' AND m.user_id IN (
				SELECT CASE WHEN user_low=? THEN user_high ELSE user_low END FROM direct_pairs WHERE conversation_id=?))`,
			id, principal.UserID, principal.UserID, id).Scan(&partner); err != nil {
			return scope, err
		}
		if partner != 1 {
			// The peer is gone, which is a state this product has: a conversation
			// whose other account was erased keeps its history for the survivor, so
			// the read proceeds and the write branch refuses.
			var survivor int
			if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM direct_pairs p WHERE p.conversation_id=? AND (p.user_low=? OR p.user_high=?))`,
				id, principal.UserID, principal.UserID).Scan(&survivor); err != nil {
				return scope, err
			}
			if survivor != 1 {
				return scope, authz.ErrUnavailable
			}
		}
	}
	if write {
		if !principal.Allowed(authz.MessageSend) {
			return scope, authz.ErrForbidden
		}
		// The write check is per kind, and the reason differs. A direct pair is
		// read-only when the two are blocked or one is unavailable. A group is not a
		// pair, so there is no two-person relationship to read as blocked: a member
		// who blocked another member still writes into the group, because a block
		// governs their contact and not a shared room (ADR 0016). What a group does
		// require is at least one other usable member, or it is a room with nobody in
		// it that still accepts messages.
		if scope.Kind == "direct" {
			var permitted bool
			err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM conversation_members m JOIN users u ON u.id=m.user_id WHERE m.conversation_id=p.conversation_id AND m.state='accepted' AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0 AND m.user_id IN(p.user_low,p.user_high))=2 AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=p.user_low AND b.blocked_id=p.user_high) OR (b.blocker_id=p.user_high AND b.blocked_id=p.user_low)) FROM direct_pairs p WHERE p.conversation_id=?`, id).Scan(&permitted)
			if err != nil {
				return scope, err
			}
			if !permitted {
				return scope, refuse(403, "This conversation is read-only. Contact is blocked or the other account is unavailable.")
			}
		} else {
			var usable int
			if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m JOIN users u ON u.id=m.user_id
				WHERE m.conversation_id=? AND m.user_id<>? AND m.state='accepted' AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0)`,
				id, principal.UserID).Scan(&usable); err != nil {
				return scope, err
			}
			if usable != 1 {
				return scope, refuse(403, "This group has nobody else in it to talk to.")
			}
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
	// A group is the other kind of private conversation and its audience is its
	// accepted, usable members. It was refused here, so a message sent into a group
	// reached nobody's socket: the sender saw it locally and every other member saw
	// nothing until they reloaded (completion review 13, V13-01).
	if !s.config.PrivateConversations || (kind != "direct" && kind != "group") {
		return nil, 0, authz.ErrUnavailable
	}
	if kind == "group" {
		rows, err := q.QueryContext(ctx, `SELECT m.user_id FROM conversation_members m
			JOIN conversations c ON c.id=m.conversation_id
			JOIN users u ON u.id=m.user_id
			WHERE c.id=? AND c.state='active' AND m.state='accepted'
			  AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0`, id)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		audience := []string{}
		for rows.Next() {
			var member string
			if err := rows.Scan(&member); err != nil {
				return nil, 0, err
			}
			audience = append(audience, member)
		}
		return audience, int64(len(audience)), rows.Err()
	}
	// A direct pair is two accounts, and the pair table is what says which two.
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
		// A direct conversation is read-only when the pair is blocked; a group is not a
		// pair, so there is nothing here that a block between two members would change.
		err := tx.QueryRowContext(ctx, `SELECT c.kind='direct',c.kind='direct' AND EXISTS(SELECT 1 FROM direct_pairs p JOIN user_blocks b ON (b.blocker_id=p.user_low AND b.blocked_id=p.user_high) OR (b.blocker_id=p.user_high AND b.blocked_id=p.user_low) WHERE p.conversation_id=c.id) FROM conversations c WHERE c.id=?`, id).Scan(&direct, &blocked)
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
