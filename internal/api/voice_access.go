package api

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/rtc"
)

// voiceGate is held while a voice connection is admitted and while access is
// enforced, so the two cannot interleave. See enforceVoiceAccess.

// voicePublisher remembers which channels had a published participant list. When
// the last participant leaves, the channel still has to be announced as empty, or
// every client that may read it keeps showing someone who has gone.
type voicePublisher struct {
	mu       sync.Mutex
	channels map[string]bool
}

// publishVoiceStates delivers voice presence one channel at a time. Voice
// presence is channel metadata: a channel's participants, mute flags and speaking
// state are as private as its messages, so every update goes only to the accounts
// that may read that channel under the capability the authorization engine resolves
// now. A public channel is not a shortcut around channel.read: an account whose role
// no longer carries it must not learn who is in a room its own /state hides.
func (s *Server) publishVoiceStates(states []rtc.State) {
	byChannel := make(map[string][]rtc.State, len(states))
	for _, state := range states {
		byChannel[state.ChannelID] = append(byChannel[state.ChannelID], state)
	}
	s.voicePublished.mu.Lock()
	previous := s.voicePublished.channels
	current := make(map[string]bool, len(byChannel))
	for channelID := range byChannel {
		current[channelID] = true
	}
	s.voicePublished.channels = current
	s.voicePublished.mu.Unlock()
	for channelID, participants := range byChannel {
		s.publishVoiceChannel(channelID, participants)
	}
	for channelID := range previous {
		if !current[channelID] {
			s.publishVoiceChannel(channelID, []rtc.State{})
		}
	}
}

// publishVoiceChannel sends one channel's participant list to the accounts that may
// read it, whether the channel is public or restricted.
//
// A public channel used to take a shortcut that broadcast to every socket, on the
// assumption that every authenticated account can read a public channel. The
// authorization engine additionally requires channel.read, so that assumption is false
// the moment a default or assigned role is narrowed: the denied account's /state hides
// the channel while its socket still received the room id, the participant ids and
// their mute, deafen and speaking flags. Visibility selects an audience inside the
// policy engine; it is not a substitute for the capability, so both visibilities go
// through the same coherent, capability-aware resolution and the same revision-checked
// delivery.
func (s *Server) publishVoiceChannel(channelID string, participants []rtc.State) {
	ctx := context.Background()
	payload := map[string]any{"channel_id": channelID, "states": participants}
	if _, err := s.store.Channel(ctx, s.store, channelID); err != nil {
		// A private call's room id is a conversation, not a channel, so this lookup
		// fails for it. That is the right refusal for a *channel* — a deleted channel
		// is gone, and the next snapshot removes it everywhere — but a private call's
		// participants are exactly the people on the line, so they go to the members
		// and to nobody else.
		s.publishPrivateVoiceRoom(ctx, channelID, payload)
		return
	}
	// The audience and the revision it is delivered under come from one coherent
	// snapshot, so a revocation cannot leave a stale participant list stamped with the
	// revision that revoked it.
	audience, revision, err := s.coherentAudience(ctx, channelID)
	if err != nil {
		s.log.Warn("voice audience lookup failed", "error", err)
		return
	}
	if committed := s.authorizationRevision(ctx); committed != revision {
		s.log.Info("voice state withheld: authority changed while the audience was dispatched", "resolved", revision, "committed", committed)
		return
	}
	s.hub.BroadcastToRevision(audience, revision, "voice.state_updated", payload)
}

// visibleVoiceStates returns the participants one principal may see. A channel it
// cannot read contributes nothing: not a name, not a mute flag, not a count.
func (s *Server) visibleVoiceStates(ctx context.Context, principal authz.Principal) []rtc.State {
	states := s.voice.States()
	visible := make([]rtc.State, 0, len(states))
	decided := make(map[string]bool, len(states))
	for _, state := range states {
		allowed, known := decided[state.ChannelID]
		if !known {
			allowed = s.canSeeVoiceRoom(ctx, principal, state.ChannelID)
			decided[state.ChannelID] = allowed
		}
		if allowed {
			visible = append(visible, state)
		}
	}
	return visible
}

// canSeeVoiceRoom decides whether one principal may see who is in a room. It is the
// same rule admission uses, so the participant list a member sees and the call they
// may join cannot disagree — and a private room's list is visible to its members
// rather than to whoever can read a channel, which would otherwise disclose a call to
// every account on the server.
func (s *Server) canSeeVoiceRoom(ctx context.Context, principal authz.Principal, roomID string) bool {
	// A channel is visible on the channel's own read rule.
	if _, _, readable, err := s.channelAccess(ctx, principal, roomID); err == nil && readable {
		return true
	}
	if !s.config.PrivateConversations {
		return false
	}
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	room, refusal := s.resolveVoiceRoom(ctx, tx, principal, roomID)
	return refusal == "" && room.ID != ""
}

// voiceChannel resolves a channel for a caller that wants to join it. A channel
// the caller may not read is answered exactly like a missing one, so a guessed
// identifier cannot confirm that a restricted voice channel exists.
func (s *Server) voiceChannel(ctx context.Context, principal authz.Principal, channelID string) (db.Channel, bool) {
	channel, _, readable, err := s.channelAccess(ctx, principal, channelID)
	if err != nil {
		s.log.Warn("voice channel lookup failed", "error", err)
		return db.Channel{}, false
	}
	if !readable || channel.Type != "voice" {
		return db.Channel{}, false
	}
	return channel, true
}

// enforceVoiceAccess reconciles every account that is in voice with the authority
// that is committed now, or — when only is given — every named account that lost
// access.
//
// Two outcomes are possible for one peer, because a voice call grants two separate
// things. Losing channel read authority or voice.connect ends the admission: media
// flows over an established peer connection, so waiting for the next request would
// leave a demoted account listening to a conversation it may no longer read. Losing
// voice.speak only takes the microphone: the peer stays in the call, and the server
// records it as muted and stops forwarding its audio, so a client that never
// reports a new mute state still cannot be heard.
// The gate serializes admission against enforcement. Resolving access and then
// admitting a peer are two steps; without this, a revocation that committed
// between them would finish before the new peer appeared in the state list and
// the admitted connection would survive it.
func (s *Server) enforceVoiceAccess(ctx context.Context, actor, reason string, only ...string) {
	s.voiceGate.Lock()
	defer s.voiceGate.Unlock()
	states := s.voice.States()
	if len(states) == 0 {
		return
	}
	// The authorization change has already committed, so the enforcement is not
	// best effort: a caller that closes the connection must not cancel it.
	ctx = context.WithoutCancel(ctx)
	restricted := make(map[string]bool, len(only))
	for _, userID := range only {
		restricted[userID] = true
	}
	authorities, err := s.store.AccountAuthorities(ctx, s.store)
	if err != nil {
		s.log.Warn("voice enforcement lookup failed", "error", err)
		return
	}
	owners, established, revision, err := s.store.AuthorizationSnapshot(ctx, s.store)
	if err != nil {
		s.log.Warn("voice enforcement lookup failed", "error", err)
		return
	}
	principals := make(map[string]authz.Principal, len(authorities))
	for _, authority := range authorities {
		principals[authority.User.ID] = principalForAuthority(authority, owners, established, revision)
	}
	for _, state := range states {
		if len(restricted) > 0 && !restricted[state.UserID] {
			continue
		}
		principal, known := principals[state.UserID]
		if !known {
			s.evictVoice(actor, reason, state)
			continue
		}
		// The room is resolved through the same function that admits a join, so
		// revalidation and admission cannot disagree about what a room is.
		//
		// They did disagree. This pass looked the room up as a *community channel*
		// unconditionally, so a direct conversation had no channel row, the lookup
		// failed, and every authorized member of every private call was evicted by
		// any unrelated authority change anywhere in the installation — a moderator
		// renaming a channel, a role granted three rooms away. Reproduced: two
		// participants in a live direct call, an unrelated change, both gone
		// (completion review 14, V14-07).
		//
		// The same pass decides whether a block now ends the call, which is the other
		// half of the same finding and has to be asked here too: admission and
		// revalidation are the same question asked at two moments.
		// `voice.connect` is a capability and the room is a place: the resolver decides
		// the second, and the first is separate. Dropping it here let an account that
		// had lost the capability keep an established call — the resolver answers
		// "may this account be in this room", not "may this account make calls at all".
		if !principal.Allowed(authz.VoiceConnect) {
			s.evictVoice(actor, reason, state)
			continue
		}
		if _, refusal := s.resolveVoiceRoom(ctx, s.store, principal, state.ChannelID); refusal != "" {
			s.evictVoice(actor, reason, state)
			continue
		}
		// The peer keeps its connection. Whether it may transmit is decided here, in
		// the same pass that decides whether it may stay, so one authority decision
		// governs the whole call rather than only its admission.
		s.voice.SetSpeakAllowed(state.UserID, "", principal.Allowed(authz.VoiceSpeak))
	}
}

// evictVoice ends one voice session and says why. The account keeps its history
// and its roles; it loses the media connection it no longer has a right to.
func (s *Server) evictVoice(actor, reason string, state rtc.State) {
	s.leaveVoice(state.UserID, "")
	s.hub.SendUser(state.UserID, "voice.access_revoked", map[string]any{"channel_id": state.ChannelID, "reason": reason})
	s.audit(actor, "voice.access_revoked", state.UserID)
}

// voiceRoom is a place a call can happen: a community voice channel, or a private
// conversation (ADR 0019, §5).
//
// It is deliberately a small value rather than either kind of row. A call does not
// need a channel's topic, position or capacity, and a private conversation has no
// channel row at all — it is a `conversations` row with `channel_id IS NULL`. Reading
// one as the other is the mistake this type exists to prevent, and the message-scope
// trigger in migration 11 already forbids a conversation masquerading as a channel.
type voiceRoom struct {
	// ID is the room's identity in the SFU. It is a channel id for a community call
	// and a conversation id for a private one, and the SFU treats it as opaque.
	ID string
	// Kind is "channel", "direct" or "group", and decides the authorization path.
	Kind string
	// Capacity is how many participants the room carries, or 0 for the default.
	Capacity int
	// DisplayName is what a participant list shows. A private call names the other
	// member, not a channel, because there is no channel.
	DisplayName string
	// Private is true for a direct or group call. It gates every event the room
	// publishes: a private call's participants are as private as its messages.
	Private bool
}

// maxPrivateCallParticipants bounds a private call. A group has up to ten members, and
// a call where everybody is already present is not a call. Two is the number that
// makes a private call private: one other person.
const maxPrivateCallParticipants = 2

// resolveVoiceRoom admits a caller to a call in `roomID`, which may be a community
// voice channel or a private conversation.
//
// The two go through different authorization on purpose. A channel uses the channel's
// own read and post rules, exactly as its messages do. A private conversation uses the
// membership resolver at conversation_access.go, so a direct pair needs its accepted
// partner and a group needs its accepted members — and a call ends when that
// membership does, rather than on a timer.
//
// Both refusals are the same sentence. A caller must not be able to learn whether a
// private conversation exists by being told it is the wrong kind of thing.
func (s *Server) resolveVoiceRoom(ctx context.Context, q db.Queryer, principal authz.Principal, roomID string) (voiceRoom, string) {
	if roomID == "" {
		return voiceRoom{}, "Voice room not found."
	}
	// A channel conversation is a voice channel, admitted by the channel's own rules.
	var kind string
	err := q.QueryRowContext(ctx, `SELECT kind FROM conversations WHERE id=?`, roomID).Scan(&kind)
	switch {
	case err == nil && kind == "channel":
		// Resolved through the caller's queryer rather than through `channelAccess`,
		// which reads through the store: the store is one connection, so asking it while
		// a transaction is open waits for the connection that transaction is holding.
		channel, err := s.store.Channel(ctx, q, roomID)
		if err != nil {
			return voiceRoom{}, "Voice room not found."
		}
		lists, err := s.store.ChannelAccessLists(ctx, q, channel.ID)
		if err != nil {
			return voiceRoom{}, "Voice room not found."
		}
		access := lists[channel.ID]
		// The channel's own read rule, exactly as its messages use. A voice channel is
		// not readable because it is a call: it is readable because the caller may read
		// the channel.
		if !principal.MayReadChannel(channel, access) || channel.Type != "voice" {
			return voiceRoom{}, "Voice room not found."
		}
		return voiceRoom{
			ID: channel.ID, Kind: "channel", Capacity: channel.MaxVoiceUsers, DisplayName: channel.Name,
		}, ""
	case err == nil:
		// A private conversation. The same membership decision a private message
		// makes, resolved now rather than when the message is read.
		if !s.config.PrivateConversations {
			return voiceRoom{}, "Voice room not found."
		}
		scope, err := s.conversationAccess(ctx, q, principal, roomID, false)
		if err != nil {
			return voiceRoom{}, "Voice room not found."
		}
		// A call is available on the direct pair or the group, and not on anything
		// else — which today means not on a channel, handled above.
		if scope.Kind != "direct" && scope.Kind != "group" {
			return voiceRoom{}, "Voice room not found."
		}
		// A block ends contact between *two people*, and a call is the most immediate
		// contact there is. On a **direct pair** that is the whole relationship, so
		// refusing the call is right and was the V14-07 fix: the conversation's read
		// admission is deliberately *retained*-history, so reusing it for a live call
		// was reading a privacy decision about the past as an admission rule for the
		// present, and a blocked pair could open a call the whole time.
		//
		// On a **group** it is one relationship among several. The group has other
		// members, the block removes nobody from it, and both blocked and blocking
		// members keep reading the history. `blockedBetween` asks whether *any* accepted
		// member blocks the caller, so applied to a group it excluded the parties from
		// each other's call for as long as the block stood — and let either of them keep
		// a third member out of the room that third member is standing in. That is not a
		// narrower block; it is the group deciding who may talk to whom.
		//
		// So the live-contact gate is a direct pair's property. What a block still
		// governs on a group is the message path — which refuses a new message to the
		// blocker — and the deletion path. Both are unchanged, and
		// `TestABlockBetweenTwoGroupMembersDoesNotRemoveThemFromTheGroupCall` asserts
		// that the relaxation did not extend to them.
		if scope.Kind == "direct" {
			if blocked, err := s.blockedBetween(ctx, q, principal.UserID, roomID); err != nil {
				return voiceRoom{}, "Voice room not found."
			} else if blocked {
				return voiceRoom{}, "Voice room not found."
			}
		}
		// Both parties must be usable accounts. `conversationAccess` only requires a
		// *usable other member* on the write path, so reading a direct conversation
		// still works after a peer is erased, deactivated or banned — which is correct:
		// the surviving member keeps their history. A call is different in kind: it is
		// live contact with somebody who is not there, and the membership row outlives
		// all three states because they are account lifecycle rather than conversation
		// membership. Admitting it means a live room containing an account that cannot
		// sign in and never will.
		if usable, err := s.conversationHasUsablePeer(ctx, q, principal.UserID, roomID); err != nil {
			return voiceRoom{}, "Voice room not found."
		} else if !usable {
			return voiceRoom{}, "Voice room not found."
		}
		return voiceRoom{
			ID: roomID, Kind: scope.Kind, Capacity: maxPrivateCallParticipants,
			DisplayName: s.privateRoomName(ctx, q, scope, principal.UserID), Private: true,
		}, ""
	case errors.Is(err, sql.ErrNoRows):
		return voiceRoom{}, "Voice room not found."
	default:
		return voiceRoom{}, "Voice room not found."
	}
}

// conversationHasUsablePeer reports whether somebody other than the caller is still a
// usable member of this conversation.
//
// It is the call path's own question, and it is deliberately narrower than the read
// path's. Reading retained history after a peer is erased, deactivated or banned is
// correct and must keep working; joining a *live* call with them is not contact with
// them at all. The membership row survives all three states, so membership alone
// cannot answer this.
func (s *Server) conversationHasUsablePeer(ctx context.Context, q db.Queryer, userID, conversationID string) (bool, error) {
	var usable bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members m JOIN users u ON u.id=m.user_id
		WHERE m.conversation_id=? AND m.user_id<>? AND m.state='accepted'
		AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0)`, conversationID, userID).Scan(&usable)
	return usable, err
}

// privateRoomName names a private call for a participant list. A direct pair is named
// for the *other* member and a group for its own name — a person reading "calling Ada"
// knows who is on the line, which a channel's name never told them. The caller's own
// id is passed in because a direct pair is a name relative to whoever is looking.
func (s *Server) privateRoomName(ctx context.Context, q db.Queryer, scope messageScope, viewerID string) string {
	if scope.Kind == "group" {
		// The group's name is read from the same row a member sees in their
		// conversation list, not from a second copy that could disagree with it.
		var name string
		if err := q.QueryRowContext(ctx, `SELECT d.name FROM group_details d JOIN conversations c ON c.id=d.conversation_id
			WHERE d.conversation_id=? AND c.kind='group'`, scope.ID).Scan(&name); err == nil && name != "" {
			return name
		}
		return "group call"
	}
	var name string
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(u.display_name,''),u.username,'member')
		FROM conversation_members m JOIN users u ON u.id=m.user_id
		WHERE m.conversation_id=? AND m.state='accepted' AND m.user_id<>? LIMIT 1`,
		scope.ID, viewerID).Scan(&name); err == nil {
		return name
	}
	return "direct call"
}

// publishPrivateVoiceRoom delivers a private call's participant list to the members of
// that conversation and to no one else.
//
// This is the one place a private call's existence could leak. A channel's presence
// goes to whoever can read the channel, which is a correct answer for a channel; the
// same audience for a conversation would tell an account that cannot read a single
// message in it who is on a call. The audience is the conversation's accepted,
// usable members, resolved now — so a member removed during a call stops receiving
// its participant list on the next change, which is the same rule their messages
// follow.
func (s *Server) publishPrivateVoiceRoom(ctx context.Context, roomID string, payload map[string]any) {
	if !s.config.PrivateConversations {
		return
	}
	var kind string
	if err := s.store.QueryRowContext(ctx, `SELECT kind FROM conversations WHERE id=?`, roomID).Scan(&kind); err != nil {
		// Not a conversation and not a channel: a deleted room. The next snapshot
		// removes it everywhere.
		return
	}
	if kind != "direct" && kind != "group" {
		return
	}
	rows, err := s.store.QueryContext(ctx, `SELECT m.user_id FROM conversation_members m
		JOIN users u ON u.id=m.user_id JOIN conversations c ON c.id=m.conversation_id
		WHERE m.conversation_id=? AND m.state='accepted' AND c.state='active'
		  AND u.is_deleted=0 AND u.is_deactivated=0 AND u.is_banned=0`, roomID)
	if err != nil {
		s.log.Warn("private voice audience lookup failed", "error", err)
		return
	}
	defer rows.Close()
	recipients := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return
		}
		recipients = append(recipients, id)
	}
	if len(recipients) == 0 {
		return
	}
	// The participant list is published alongside the room's identity, so a client can
	// tell a private call from a channel one — the payload is otherwise identical and
	// a client that could not tell would render a conversation's participants in a
	// channel sidebar.
	payload["private"] = true
	revision := s.authorizationRevision(ctx)
	s.hub.BroadcastToRevision(recipients, revision, "voice.state_updated", payload)
}

// blockedBetween reports whether one of the account's peers in a private conversation
// has blocked it, or it has blocked them.
//
// A block is a one-directional fact recorded against the blocker, so the question is
// asked in both directions: the caller's own block list, and the block lists of the
// other accepted members. A conversation with one member is the direct-call case and
// one direction of the pair is the whole of it.
func (s *Server) blockedBetween(ctx context.Context, q db.Queryer, userID, conversationID string) (bool, error) {
	var blocked bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_blocks
		WHERE blocker_id=? AND blocked_id IN (SELECT user_id FROM conversation_members
			WHERE conversation_id=? AND user_id<>? AND state='accepted'))`, userID, conversationID, userID).Scan(&blocked)
	if err != nil || blocked {
		return blocked, err
	}
	// The other direction: somebody on the line has blocked this account. The subquery
	// finds the blocker, and the block list is what says whether they did.
	err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_blocks b
		JOIN conversation_members m ON m.user_id=b.blocker_id AND m.conversation_id=?
		WHERE b.blocked_id=? AND m.state='accepted')`, conversationID, userID).Scan(&blocked)
	return blocked, err
}
