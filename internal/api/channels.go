package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/limits"
)

// channelColumns and scanChannel are the full channel shape, kept in the storage
// layer so an access-scoped read and an administrative read cannot drift apart.
const channelColumns = db.ChannelColumns

func scanChannel(row interface{ Scan(...any) error }) (db.Channel, error) {
	return db.ScanChannel(row)
}

// channels lists every channel. It is for administrative and test paths that have
// already established authority; anything a member can see must use
// s.channelsFor or s.store.AccessibleChannels.
func (s *Server) channels() ([]db.Channel, error) {
	// An administrative read: the caller has already established the authority
	// this is used to exercise.
	return s.channelsFor(db.Viewer{CommunityAdmin: true, Reads: true})
}

func (s *Server) channelsFor(viewer db.Viewer) ([]db.Channel, error) {
	return s.store.AccessibleChannels(context.Background(), viewer, "")
}

func (s *Server) channel(id string) (db.Channel, error) {
	return s.store.Channel(context.Background(), s.store, id)
}

// listChannels returns only the channels the caller may read. A hidden channel's
// name, topic and existence are private data.
func (s *Server) listChannels(w http.ResponseWriter, r *http.Request, user db.User) {
	channels, err := s.channelsFor(viewer(principalOf(r)))
	if err != nil {
		s.failed(w, err)
		return
	}
	respond(w, 200, channels)
}

func validChannel(channel db.Channel) bool {
	return strings.TrimSpace(channel.Name) != "" && len(channel.Name) <= 60 && len(channel.Category) <= 60 && len(channel.Topic) <= 500 &&
		(channel.Type == "text" || channel.Type == "voice") && channel.MaxVoiceUsers >= 1 && channel.MaxVoiceUsers <= 10 && channel.Position >= 0 && channel.Position <= 1000 &&
		channel.SlowModeSeconds >= 0 && channel.SlowModeSeconds <= 21600 && (channel.Type == "text" || channel.SlowModeSeconds == 0) &&
		(channel.Visibility == "public" || channel.Visibility == "private") && (channel.Posting == "members" || channel.Posting == "announcement")
}

// normalizeChannel fills defaults and rejects a restricted channel when the
// privacy surface is disabled.
func (s *Server) normalizeChannel(w http.ResponseWriter, channel *db.Channel) bool {
	if channel.Visibility == "" {
		channel.Visibility = "public"
	}
	if channel.Posting == "" {
		channel.Posting = "members"
	}
	if channel.Visibility == "private" || channel.Posting == "announcement" {
		return s.requireRestrictedChannels(w)
	}
	return true
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, user db.User) {
	channel := db.Channel{MaxVoiceUsers: 8}
	if !decode(w, r, &channel) {
		return
	}
	if !s.normalizeChannel(w, &channel) {
		return
	}
	if !validChannel(channel) {
		problem(w, 400, "Check the channel settings. Voice capacity is 1–10; text channel slow mode is 0–21,600 seconds.")
		return
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM channels`).Scan(&count); err != nil {
		s.failed(w, err)
		return
	}
	if count >= limits.Channels {
		problem(w, 409, fmt.Sprintf("This workspace has reached its %d-channel limit.", limits.Channels))
		return
	}
	channel.ID = db.ID()
	now := db.Now()
	if !s.mutateReply(w, r, user, authz.ChannelManage, func(tx *sql.Tx, principal authz.Principal) error {
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM channels`).Scan(&count); err != nil {
			return err
		}
		if count >= limits.Channels {
			return refuse(409, fmt.Sprintf("This workspace has reached its %d-channel limit.", limits.Channels))
		}
		if _, err := tx.Exec(`INSERT INTO channels(id,name,type,category,position,topic,max_voice_users,visibility,posting,slow_mode_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, channel.ID, strings.TrimSpace(channel.Name), channel.Type, channel.Category, channel.Position, channel.Topic, channel.MaxVoiceUsers, channel.Visibility, channel.Posting, channel.SlowModeSeconds, now, now); err != nil {
			return err
		}
		return s.auditTx(r.Context(), tx, user.ID, "channel.created", channel.ID)
	}) {
		return
	}
	s.publishChannelChange(r.Context(), channel, "channel.created", "A channel was created.")
	respond(w, 201, channel)
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request, user db.User) {
	channel, err := s.channel(r.PathValue("id"))
	if !s.found(w, err) {
		return
	}
	kind, wasPrivate := channel.Type, channel.Visibility == "private"
	if !decode(w, r, &channel) {
		return
	}
	if !s.normalizeChannel(w, &channel) {
		return
	}
	// Moving a channel between public and private changes who can read it, so the
	// privacy switch applies in both directions.
	if (channel.Visibility == "private") != wasPrivate && !s.requireRestrictedChannels(w) {
		return
	}
	if !validChannel(channel) || channel.Type != kind {
		problem(w, 400, "Invalid channel settings. A channel's type cannot change; text channel slow mode is 0–21,600 seconds.")
		return
	}
	channel.ID = r.PathValue("id")
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ChannelManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	if _, err := tx.Exec(`UPDATE channels SET name=?,category=?,position=?,topic=?,max_voice_users=?,visibility=?,posting=?,slow_mode_seconds=?,updated_at=? WHERE id=?`, strings.TrimSpace(channel.Name), channel.Category, channel.Position, channel.Topic, channel.MaxVoiceUsers, channel.Visibility, channel.Posting, channel.SlowModeSeconds, db.Now(), channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	// A visibility or posting change is an authorization change: it commits with the
	// revision so already-admitted access cannot continue on the old policy.
	visibilityChanged := (channel.Visibility == "private") != wasPrivate
	authorizationChanged := visibilityChanged || channel.Posting != "members"
	if authorizationChanged {
		if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "channel.updated", channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	if authorizationChanged {
		// A visibility change reaches every connection, so the commit and its refresh
		// signal are one step under the authority fence.
		if err := s.commitAuthorityChange(r.Context(), tx, "A channel's visibility changed."); err != nil {
			s.failed(w, err)
			return
		}
	} else if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	if visibilityChanged {
		// A channel that just became restricted must not keep streaming to the
		// accounts that lost access in the same change — neither media nor files.
		s.deliveries.revoke(channel.ID)
		s.enforceVoiceAccess(r.Context(), user.ID, "A channel's visibility changed.")
	}
	s.publishChannelChange(r.Context(), channel, "channel.updated", "A channel changed.")
	respond(w, 200, channel)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request, user db.User) {
	channel, channelErr := s.channel(r.PathValue("id"))
	if !s.found(w, channelErr) || !confirmed(w, r, channel.Name) {
		return
	}
	// The audience that may learn about the deletion is the audience that could
	// see the channel, so it is resolved before the row disappears, from one coherent
	// snapshot so the revision it is delivered under is the one it was resolved at.
	audience, audienceRevision, err := s.coherentAudience(r.Context(), channel.ID)
	if err != nil {
		s.log.Warn("removal audience lookup failed; the event is not addressed", "error", err)
		audience, audienceRevision = nil, 0
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ChannelManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM channels WHERE id=?`, channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
		s.failed(w, err)
		return
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "channel.deleted", channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Removal is a workspace-wide authority change, so the commit publishes the refresh
	// signal to every connection under the fence; the event that names the channel is
	// then addressed to the accounts that could see it, and carries the revision it was
	// resolved at.
	if err := s.commitAuthorityChange(r.Context(), tx, "A channel was removed."); err != nil {
		s.failed(w, err)
		return
	}
	s.hub.BroadcastToRevision(audience, audienceRevision, "channel.deleted", channel)
	s.deliveries.revoke(channel.ID)
	s.voice.CloseChannel(channel.ID)
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) found(w http.ResponseWriter, err error) bool {
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Not found.")
		return false
	}
	if err != nil {
		s.failed(w, err)
		return false
	}
	return true
}

func confirmed(w http.ResponseWriter, r *http.Request, expected string) bool {
	var input struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return false
	}
	if input.Confirm != expected {
		problem(w, 400, "Confirmation does not match.")
		return false
	}
	return true
}

// moveChannel reads and rewrites the order in one transaction so clients never
// observe a partially reordered list. Only ordering and category are changed.
func (s *Server) moveChannel(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		TargetID string `json:"target_id"`
		After    bool   `json:"after"`
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
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ChannelManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT `+channelColumns+` FROM channels ORDER BY position,id`)
	if err != nil {
		s.failed(w, err)
		return
	}
	channels := make([]db.Channel, 0)
	var source, target db.Channel
	for rows.Next() {
		channel, scanErr := scanChannel(rows)
		if scanErr != nil {
			rows.Close()
			s.failed(w, scanErr)
			return
		}
		if channel.ID == r.PathValue("id") {
			source = channel
		}
		if channel.ID == input.TargetID {
			target = channel
		}
		channels = append(channels, channel)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.failed(w, err)
		return
	}
	if source.ID == "" || target.ID == "" {
		problem(w, 404, "A channel no longer exists. Refresh and try again.")
		return
	}
	if source.ID == target.ID || source.Type != target.Type {
		problem(w, 400, "Move a channel next to another channel of the same type.")
		return
	}
	source.Category = target.Category
	ordered := make([]db.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.ID == source.ID {
			continue
		}
		if channel.ID == target.ID && !input.After {
			ordered = append(ordered, source)
		}
		ordered = append(ordered, channel)
		if channel.ID == target.ID && input.After {
			ordered = append(ordered, source)
		}
	}
	for i := range ordered {
		ordered[i].Position = i
		if _, err := tx.ExecContext(r.Context(), `UPDATE channels SET position=?,category=?,updated_at=? WHERE id=?`, i, ordered[i].Category, db.Now(), ordered[i].ID); err != nil {
			s.failed(w, err)
			return
		}
	}
	if err := s.auditTx(r.Context(), tx, user.ID, "channel.moved", source.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	// The caller, who manages channels, receives the new order. Everyone else is
	// told that workspace structure moved and refetches its own scoped state,
	// because an ordered list of every channel would name private channels to
	// accounts that may not read them.
	s.invalidateWorkspace("Channels were reordered.")
	respond(w, 200, ordered)
}

// publishChannelChange delivers channel metadata to the accounts that may read
// the channel, and a bare refresh signal to everyone else. A channel's name,
// topic and existence are private data, so the event that carries them is
// addressed; the signal that says only "workspace structure moved" is not.
func (s *Server) publishChannelChange(ctx context.Context, channel db.Channel, kind, reason string) {
	s.publishToChannel(ctx, channel.ID, kind, channel)
	s.hub.Broadcast("workspace.changed", map[string]string{"reason": reason})
}
