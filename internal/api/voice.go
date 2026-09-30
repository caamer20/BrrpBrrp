package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/rtc"
	"brrpbrrp/internal/ws"
	"github.com/pion/webrtc/v4"
)

// voiceEvent handles one realtime voice command. Joining and moving resolve the
// channel through the same access rule the rest of the product uses, so a voice
// channel is never a way around a restricted channel's boundary.
func (s *Server) voiceEvent(ctx context.Context, client *ws.Client, user db.User, event ws.Event) {
	report := func(message string) { client.Send("voice.error", map[string]string{"error": message}) }
	// Voice obeys the same resolved authority as every other surface, so the
	// capabilities are read once here rather than re-derived in each branch.
	principal, err := s.resolvePrincipal(ctx, s.store, user)
	if err != nil {
		s.log.Warn("voice authority lookup failed", "error", err)
		report("Your authority could not be resolved. Try again.")
		return
	}
	switch event.Type {
	case "voice.resume":
		var input struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(event.Data, &input) != nil {
			return
		}
		// A resume reinstates a media connection, so it re-establishes both the
		// channel access and the connect capability rather than trusting the token.
		// The gate makes this atomic with a revocation: either the revocation sees
		// the resumed peer, or the resume sees the revoked access. Authority is
		// resolved inside the gate for the same reason — a decision made before it
		// could belong to a capability that has already been withdrawn.
		s.voiceGate.Lock()
		channelID, pending := s.voice.PendingChannel(user.ID, input.Token)
		if !pending {
			s.voiceGate.Unlock()
			report("your previous voice connection has ended")
			return
		}
		principal, err = s.voiceAuthorityInGate(ctx, user)
		if err != nil {
			s.voiceGate.Unlock()
			report("Your authority could not be resolved. Try again.")
			return
		}
		channel, refusal := s.voiceJoin(ctx, principal, channelID)
		if refusal != "" {
			s.voiceGate.Unlock()
			s.leaveVoice(user.ID, "")
			report(refusal)
			return
		}
		state, err := s.voice.Resume(user.ID, input.Token, client.ID, client.Send)
		if err == nil {
			s.voice.SetSpeakAllowed(user.ID, client.ID, principal.Allowed(authz.VoiceSpeak))
		}
		s.voiceGate.Unlock()
		if err != nil {
			report(err.Error())
			return
		}
		client.Send("voice.resumed", map[string]string{"channel_id": state.ChannelID, "resume_token": s.voice.ResumeToken(user.ID, client.ID)})
		_ = channel
	case "voice.move":
		if !s.limiter.allow("voice-move:"+user.ID, 5, 5*time.Second) {
			client.Send("voice.move.error", map[string]string{"error": "Wait a moment before switching rooms again."})
			return
		}
		var input struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(event.Data, &input) != nil {
			return
		}
		s.voiceGate.Lock()
		if principal, err = s.voiceAuthorityInGate(ctx, user); err != nil {
			s.voiceGate.Unlock()
			client.Send("voice.move.error", map[string]string{"error": "Your authority could not be resolved. Try again."})
			return
		}
		room, refusal := s.voiceJoin(ctx, principal, input.ChannelID)
		if refusal != "" {
			s.voiceGate.Unlock()
			client.Send("voice.move.error", map[string]string{"error": refusal})
			return
		}
		err = s.voice.Move(user.ID, client.ID, room.ID, room.Capacity)
		if err == nil {
			s.voice.SetSpeakAllowed(user.ID, client.ID, principal.Allowed(authz.VoiceSpeak))
		}
		s.voiceGate.Unlock()
		if err != nil {
			client.Send("voice.move.error", map[string]string{"error": err.Error()})
			return
		}
		client.Send("voice.moved", map[string]string{"channel_id": room.ID})
	case "voice.join":
		if !s.Settings().Voice {
			report("Voice is disabled by an administrator.")
			return
		}
		var input struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(event.Data, &input) != nil {
			return
		}
		s.voiceGate.Lock()
		if principal, err = s.voiceAuthorityInGate(ctx, user); err != nil {
			s.voiceGate.Unlock()
			report("Your authority could not be resolved. Try again.")
			return
		}
		room, refusal := s.voiceJoin(ctx, principal, input.ChannelID)
		if refusal != "" {
			s.voiceGate.Unlock()
			report(refusal)
			return
		}
		// The room's capacity is the bound the SFU enforces, and it is the room's own:
		// a voice channel's configured maximum, and a private call's two. The SFU
		// treats the id as opaque, so a private conversation needs no special casing
		// here beyond the admission decision that got us to this point.
		err = s.voice.Join(user.ID, client.ID, room.ID, room.Capacity, client.Send)
		if err == nil {
			s.voice.SetSpeakAllowed(user.ID, client.ID, principal.Allowed(authz.VoiceSpeak))
		}
		s.voiceGate.Unlock()
		if err != nil {
			report(err.Error())
			return
		}
		iceServers := []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
		turn, enabled := s.turn.Credentials(user.ID)
		if enabled {
			iceServers = append(iceServers, turn)
		}
		// `private` is in the acceptance so the client can tell a call from a
		// conversation. A private call's events must not be broadcast to a channel
		// audience, and the client needs to know which it is in to avoid rendering a
		// conversation's participants in a channel sidebar.
		client.Send("voice.join.accept", map[string]any{"channel_id": room.ID, "private": room.Private, "display_name": room.DisplayName, "connection_id": s.voice.ConnectionID(user.ID, client.ID), "resume_token": s.voice.ResumeToken(user.ID, client.ID), "ice_servers": iceServers, "turn_enabled": enabled, "bitrate": s.Settings().VoiceBitrate * 1000, "receive_slots": rtc.MaxParticipants - 1, "may_speak": principal.Allowed(authz.VoiceSpeak)})
	case "voice.leave":
		s.leaveVoice(user.ID, client.ID)
	case "voice.state", "voice.speaking":
		var state struct {
			Muted    bool `json:"muted"`
			Deafened bool `json:"deafened"`
			Speaking bool `json:"speaking"`
		}
		if json.Unmarshal(event.Data, &state) != nil {
			return
		}
		if !s.limiter.allow("voice-state:"+user.ID, 5, 1000000000) {
			return
		}
		// Preserve the member's own mute intent separately from the server's
		// permission restriction. Forcing muted=true into client state would keep
		// the peer muted after a later regrant. Resolve inside the same gate as
		// permission enforcement so an older state event cannot restore a stale
		// permission decision; the SFU still applies denial to every RTP packet.
		s.voiceGate.Lock()
		principal, err = s.voiceAuthorityInGate(ctx, user)
		if err != nil {
			s.voiceGate.Unlock()
			report("Your voice access could not be verified. Try again.")
			return
		}
		s.voice.SetSpeakAllowed(user.ID, client.ID, principal.Allowed(authz.VoiceSpeak))
		s.voice.SetState(user.ID, client.ID, state.Muted, state.Deafened, state.Speaking)
		s.voiceGate.Unlock()
	case "rtc.signal":
		var signal rtc.Signal
		if json.Unmarshal(event.Data, &signal) != nil {
			return
		}
		if err := s.voice.Signal(user.ID, client.ID, signal); err != nil {
			report("Voice negotiation failed. Leave and try joining again.")
			s.leaveVoice(user.ID, client.ID)
		}
	case "admin.voice_kick", "admin.voice_mute", "admin.voice_deafen":
		if !principal.Allowed(authz.VoiceModerate) {
			report("Your role does not include voice.moderate.")
			return
		}
		var input struct {
			UserID   string `json:"user_id"`
			Muted    *bool  `json:"muted"`
			Deafened *bool  `json:"deafened"`
			Confirm  string `json:"confirm"`
		}
		if json.Unmarshal(event.Data, &input) != nil || input.Confirm != input.UserID {
			report("Confirm the user before moderating voice.")
			return
		}
		if err := s.voiceModerationAllowed(ctx, principal, input.UserID); err != nil {
			report(err.Error())
			return
		}
		s.audit(user.ID, event.Type, input.UserID)
		if event.Type == "admin.voice_kick" {
			s.leaveVoice(input.UserID, "")
		} else {
			s.voice.Moderate(input.UserID, input.Muted, input.Deafened)
		}
	}
}

// voiceAuthorityInGate re-resolves the caller's authority while the voice gate is
// held. Admission and enforcement are serialized by that gate, so the decision that
// admits a peer belongs inside it: a capability withdrawn in between is then seen
// either by the admission itself or by the enforcement that follows it. Resolving
// authority before taking the gate left the two able to miss each other.
func (s *Server) voiceAuthorityInGate(ctx context.Context, user db.User) (authz.Principal, error) {
	return s.resolvePrincipal(ctx, s.store, user)
}

// voiceJoin resolves a voice channel for a caller that wants to enter it,
// applying both halves of the advertised contract: the channel must be readable,
// and the account must hold voice.connect. The two refusals are distinct, because
// a hidden channel must not be confirmable while a visible channel with a missing
// grant is a clear refusal rather than a mysterious absence. A listen-only role
// holds voice.connect without voice.speak and is admitted, muted.
// voiceJoin admits a caller to a call. The room may be a community voice channel or a
// private conversation; the capability that gates the microphone gates both, because
// speaking in a private call is the same act as speaking in a channel.
func (s *Server) voiceJoin(ctx context.Context, principal authz.Principal, roomID string) (voiceRoom, string) {
	if !principal.Allowed(authz.VoiceConnect) {
		// Checked before the room is resolved, so a caller without the capability is
		// not told whether the room exists.
		return voiceRoom{}, "Your role does not include voice.connect."
	}
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return voiceRoom{}, "The call could not be started."
	}
	defer tx.Rollback()
	room, refusal := s.resolveVoiceRoom(ctx, tx, principal, roomID)
	if refusal != "" {
		return voiceRoom{}, refusal
	}
	if err := tx.Commit(); err != nil {
		return voiceRoom{}, "The call could not be started."
	}
	return room, ""
}

// voiceModerationAllowed applies the moderation hierarchy: voice moderation is
// moderation, so a delegated moderator acts only on accounts below its own
// authority and never on the owner.
func (s *Server) voiceModerationAllowed(ctx context.Context, principal authz.Principal, targetID string) error {
	target, err := s.store.Account(ctx, s.store, targetID)
	if err != nil {
		return errors.New("That account is not in voice.")
	}
	if target.IsOwner && target.ID != principal.UserID {
		return errors.New("You cannot moderate the installation owner.")
	}
	if target.ID == principal.UserID {
		return errors.New("Use the voice controls to change your own microphone.")
	}
	if principal.IsOwner || principal.IsAdministrator() {
		return nil
	}
	rank, err := s.authz.Rank(ctx, s.store, target.ID)
	if err != nil {
		return err
	}
	if principal.Position() <= rank {
		return errors.New("You cannot moderate an account that holds the same or higher authority than you do.")
	}
	return nil
}

func (s *Server) leaveVoice(userID, owner string) {
	s.voice.Leave(userID, owner)
	for _, state := range s.voice.States() {
		if state.UserID == userID {
			return
		}
	}
	s.turn.RevokeUser(userID)
}
