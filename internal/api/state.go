package api

import (
	"context"
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/rtc"
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
	if s.config.PrivateConversations {
		directs, err := s.directConversations(r.Context(), user)
		if err != nil {
			s.failed(w, err)
			return
		}
		for _, c := range directs {
			if c.CanRead {
				counts = append(counts, db.UnreadCount{ConversationID: c.ID, Count: c.Count, Mentions: 0, LastRead: c.LastReadMessageID})
			}
		}
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

// adminUserStates decorates the administrator's account list with whether each account
// is currently inside a moderation timeout, so the panel can offer to end one. It is a
// separate projection rather than a column on the user: the timeout is a moderation
// state, and putting it on the user row would make it part of identity — visible to
// every reader of a user rather than only to the administrator who may lift it.
func (s *Server) adminUserStates(ctx context.Context, users []db.User) (map[string]bool, error) {
	states := map[string]bool{}
	rows, err := s.store.QueryContext(ctx, `SELECT user_id FROM account_timeouts WHERE expires_at>?`, db.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		states[id] = true
	}
	return states, rows.Err()
}

// state returns the bootstrap snapshot. Every part of it is scoped to the caller:
// a channel it cannot read contributes neither a title, a count nor a cursor.
func (s *Server) state(w http.ResponseWriter, r *http.Request, user db.User) {
	principal := principalOf(r)
	// A member in a community timeout still gets a snapshot, because the snapshot is
	// not only the channel list: it carries the server settings, the feature flags,
	// this account's private conversations, groups and request counts — none of which
	// the restriction touches. Refusing it left the client unable to finish signing
	// in, which turned a community timeout into "you cannot use this account at all".
	//
	// The snapshot's own account is described to the caller, so it carries the caller's
	// timeout state for the same reason sign-in and `/api/me` do: the client reads
	// `user` from here rather than from a separate call, so without it the sidebar
	// notice never appears for a member who was signed in before the timeout started.
	s.describeOwnTimeout(r.Context(), &user)
	// The community half is emptied rather than served: a timed-out member is told what
	// is in force by the snapshot's own account and the sidebar notice, and must not be
	// handed a readable channel list on the way past. Every other route still refuses.
	channels := []db.Channel{}
	if !principal.Suspended {
		var err error
		channels, err = s.store.AccessibleChannels(r.Context(), viewer(principal), "")
		if err != nil {
			s.failed(w, err)
			return
		}
	}
	users := []db.User{}
	if !principal.Suspended {
		var err error
		users, err = s.users(false)
		if err != nil {
			s.failed(w, err)
			return
		}
	}
	// The remaining community-derived reads — unread counts, categories, presence and
	// voice states — describe the community, so they are not read for a member in a
	// community timeout. Presence in particular is not the timed-out member's to
	// observe: they cannot enter a channel to be seen in one, so telling them who is
	// around would describe a room they are not in.
	unread := []db.UnreadCount{}
	categories := []db.Category{}
	presence := map[string]string{}
	voiceStates := []rtc.State{}
	if !principal.Suspended {
		var err error
		if unread, err = s.store.UnreadCounts(r.Context(), viewer(principal)); err != nil {
			s.failed(w, err)
			return
		}
		// Category names are shared workspace structure, but the channel counts they
		// carry are scoped to what the caller may read, so a category holding only
		// restricted channels reports no count to an account that cannot open them.
		if categories, err = s.store.Categories(r.Context(), s.store, viewer(principal)); err != nil {
			s.failed(w, err)
			return
		}
		presence = s.visiblePresence()
		voiceStates = s.visibleVoiceStates(r.Context(), principal)
	}
	directs := []directConversation{}
	// Groups are the other kind of private conversation and are listed alongside the
	// direct ones, because the workspace resolves both through the same navigation,
	// read cursor, outbox and draft state. Membership is the consent, so this returns
	// only groups the account has been accepted into (ADR 0016).
	groups := []groupConversation{}
	// outstanding_incoming_requests is the badge: how many pending requests are
	// addressed to this account and still actionable. It is derived from durable
	// state, so a cold start, a restart and a device that missed every event all
	// agree, and it reads no request introduction, so a closed workspace never has
	// to pull pending requests into a panel that is not open (ADR 0013).
	incomingRequests := 0
	if s.config.PrivateConversations {
		all, err := s.directConversations(r.Context(), user)
		if err != nil {
			s.failed(w, err)
			return
		}
		for _, c := range all {
			if c.CanRead {
				directs = append(directs, c)
			}
		}
		incomingRequests, err = s.outstandingIncomingRequests(r.Context(), s.store, user)
		if err != nil {
			s.failed(w, err)
			return
		}
		groups, err = s.groupConversations(r.Context(), user)
		if err != nil {
			s.failed(w, err)
			return
		}
	}
	respond(w, 200, map[string]any{
		"conversations": directs, "groups": groups, "features": map[string]bool{"private_conversations": s.config.PrivateConversations},
		"requests": map[string]int{"outstanding_incoming": incomingRequests},
		"user":     user, "channels": channels, "users": users, "settings": s.Settings(), "unread": unread,
		"reaction_palette": reactionPalette, "presence": presence, "push_public_key": s.vapidPublic,
		"voice_states": voiceStates, "categories": categories,
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
	scope, _, err := s.scopeInTx(r.Context(), tx, user, r.PathValue("id"), false)
	if err != nil {
		s.scopeFailed(w, err)
		return
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM messages WHERE id=? AND conversation_id=?`, input.MessageID, scope.ID).Scan(&id)
	if !s.found(w, err) {
		return
	}
	if scope.Kind == "channel" {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO channel_reads(user_id,channel_id,last_read_message_id,last_read_at) VALUES(?,?,?,?) ON CONFLICT(user_id,channel_id) DO UPDATE SET last_read_message_id=max(last_read_message_id,excluded.last_read_message_id),last_read_at=excluded.last_read_at`, user.ID, r.PathValue("id"), id, db.Now())
	} else {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO conversation_reads(user_id,conversation_id,last_read_message_id,last_read_at) VALUES(?,?,?,?) ON CONFLICT(user_id,conversation_id) DO UPDATE SET last_read_message_id=max(last_read_message_id,excluded.last_read_message_id),last_read_at=excluded.last_read_at`, user.ID, scope.ID, id, db.Now())
	}
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
	s.hub.BroadcastToRevision([]string{user.ID}, revision, "unread.updated", map[string]string{"channel_id": scope.ChannelID, "conversation_id": scope.ID})
	respond(w, 200, map[string]bool{"ok": true})
}
