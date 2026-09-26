package api

import (
	"context"
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
		// A deleted channel is gone; the next snapshot removes it everywhere.
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
			_, _, readable, err := s.channelAccess(ctx, principal, state.ChannelID)
			if err != nil {
				s.log.Warn("voice visibility lookup failed", "error", err)
			}
			allowed = err == nil && readable
			decided[state.ChannelID] = allowed
		}
		if allowed {
			visible = append(visible, state)
		}
	}
	return visible
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
		principals[authority.User.ID] = principalFor(authority, owners, established, revision)
	}
	channels := make(map[string]db.Channel, len(states))
	access := make(map[string]db.ChannelAccess, len(states))
	for _, state := range states {
		if len(restricted) > 0 && !restricted[state.UserID] {
			continue
		}
		principal, known := principals[state.UserID]
		if !known {
			s.evictVoice(actor, reason, state)
			continue
		}
		channel, seen := channels[state.ChannelID]
		if !seen {
			channel, err = s.store.Channel(ctx, s.store, state.ChannelID)
			if err != nil {
				s.evictVoice(actor, reason, state)
				continue
			}
			lists, listErr := s.store.ChannelAccessLists(ctx, s.store, state.ChannelID)
			if listErr != nil {
				s.log.Warn("voice enforcement lookup failed", "error", listErr)
				continue
			}
			channels[state.ChannelID] = channel
			access[state.ChannelID] = lists[state.ChannelID]
		}
		if !principal.MayReadChannel(channel, access[state.ChannelID]) || !principal.Allowed(authz.VoiceConnect) {
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
