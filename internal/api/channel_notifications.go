package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"

	"brrpbrrp/internal/db"
)

type channelNotificationPreference struct {
	ChannelID string `json:"channel_id"`
	Muted     bool   `json:"muted"`
}

func (s *Server) listChannelNotifications(w http.ResponseWriter, r *http.Request, user db.User) {
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT channel_id,muted FROM channel_notification_preferences WHERE user_id=? ORDER BY channel_id`, user.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	preferences := make([]channelNotificationPreference, 0)
	for rows.Next() {
		var preference channelNotificationPreference
		if err := rows.Scan(&preference.ChannelID, &preference.Muted); err != nil {
			rows.Close()
			s.failed(w, err)
			return
		}
		preferences = append(preferences, preference)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.failed(w, err)
		return
	}
	channelIDs := make([]string, 0, len(preferences))
	for _, preference := range preferences {
		channelIDs = append(channelIDs, preference.ChannelID)
	}
	access, err := s.store.ChannelAccessLists(r.Context(), tx, channelIDs...)
	if err != nil {
		s.failed(w, err)
		return
	}
	visible := make([]channelNotificationPreference, 0, len(preferences))
	for _, preference := range preferences {
		channel, err := s.store.Channel(r.Context(), tx, preference.ChannelID)
		if err != nil {
			s.failed(w, err)
			return
		}
		if principal.MayReadChannel(channel, access[channel.ID]) {
			visible = append(visible, preference)
		}
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	respond(w, http.StatusOK, visible)
}

func (s *Server) setChannelNotifications(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Muted *bool `json:"muted"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Muted == nil {
		problem(w, http.StatusBadRequest, "Choose whether to mute this channel.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	principal, err := s.authorizeInTx(r.Context(), tx, user)
	if err != nil {
		s.authorityFailed(w, err)
		return
	}
	channel, err := s.store.Channel(r.Context(), tx, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		hidden(w)
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	access, err := s.store.ChannelAccessLists(r.Context(), tx, channel.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	if !principal.MayReadChannel(channel, access[channel.ID]) {
		hidden(w)
		return
	}
	preference := channelNotificationPreference{ChannelID: channel.ID, Muted: *input.Muted}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO channel_notification_preferences(user_id,channel_id,muted,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id,channel_id) DO UPDATE SET muted=excluded.muted,updated_at=excluded.updated_at`, user.ID, channel.ID, preference.Muted, db.Now()); err != nil {
		s.failed(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.failed(w, err)
		return
	}
	// A mute is account-owned policy, not a per-connection view, and this write
	// comes from one device. Without this signal the account's other sessions keep
	// a cached answer and silently drop mentions the account is no longer muting
	// (or keep muting ones it stopped): the flag is only re-read on first load, on
	// an authority change, and on window focus inside a mounted channel header,
	// none of which a session sitting in a direct conversation reaches (review 9,
	// R9-02). It is a hint carrying no authority: the client re-reads its own
	// preference, and the revision is not bumped because no authorization changed.
	s.hub.SendUser(user.ID, "preferences.changed", map[string]any{"channel_notifications": true})
	respond(w, http.StatusOK, preference)
}

// notificationAudience resolves access, active account state, notification
// preferences and the delivery revision together. Muting affects interruption
// only: syncMentions and unread/history queries keep their original records.
func (s *Server) notificationAudience(ctx context.Context, channelID string) ([]string, int64, error) {
	tx, err := s.store.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	audience, revision, err := s.channelAudienceIDs(ctx, tx, channelID)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM channel_notification_preferences WHERE channel_id=? AND muted=1`, channelID)
	if err != nil {
		return nil, 0, err
	}
	muted := make(map[string]bool)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		muted[userID] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	audience = slices.DeleteFunc(audience, func(userID string) bool { return muted[userID] })
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return audience, revision, nil
}

func (s *Server) mayNotifyChannel(ctx context.Context, userID, channelID string) bool {
	audience, _, err := s.notificationAudience(ctx, channelID)
	if err != nil {
		s.log.Warn("notification eligibility lookup failed", "error", err)
		return false
	}
	return slices.Contains(audience, userID)
}
