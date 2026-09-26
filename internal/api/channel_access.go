package api

import (
	"database/sql"
	"errors"
	"net/http"

	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// channelAccessList answers "who can access this?" for one channel. It reports
// the same rule the delivery paths apply, so the answer cannot promise access the
// server would refuse.
func (s *Server) channelAccessList(w http.ResponseWriter, r *http.Request, user db.User) {
	channel, err := s.store.Channel(r.Context(), s.store, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Channel not found.")
			return
		}
		s.failed(w, err)
		return
	}
	lists, err := s.store.ChannelAccessLists(r.Context(), s.store, channel.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	access := lists[channel.ID]
	members, err := s.authz.EffectiveChannelMembers(r.Context(), s.store, channel, access)
	if err != nil {
		s.failed(w, err)
		return
	}
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Username)
	}
	respond(w, 200, map[string]any{
		"channel_id":        channel.ID,
		"visibility":        channel.Visibility,
		"posting":           channel.Posting,
		"roles":             access.Roles,
		"users":             access.Users,
		"effective_members": names,
		"effective_count":   len(names),
		// Community managers can read every community channel, so a restricted
		// channel is never private from them. Saying so is part of the contract.
		"managers_can_read":      true,
		"restricted_enabled":     s.restrictedChannelsEnabled(),
		"authorization_revision": channel.AuthorizationRevision,
	})
}

// setChannelAccessList replaces a channel's visibility and explicit grants. The
// change commits with the authorization revision, and clients whose access moved
// in either direction are told to refetch.
func (s *Server) setChannelAccessList(w http.ResponseWriter, r *http.Request, user db.User) {
	if !s.requireRestrictedChannels(w) {
		return
	}
	var input struct {
		Visibility string   `json:"visibility"`
		RoleIDs    []string `json:"role_ids"`
		UserIDs    []string `json:"user_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Visibility != "public" && input.Visibility != "private" {
		problem(w, 400, "Choose public or private visibility.")
		return
	}
	tx, err := s.store.BeginTx(r.Context(), nil)
	if err != nil {
		s.failed(w, err)
		return
	}
	defer tx.Rollback()
	// Authority is re-established inside the transaction: access grants are
	// exactly the change a revoked channel manager must not be able to land.
	if _, err := s.requireCapabilityInTx(r.Context(), tx, user, authz.ChannelManage); err != nil {
		s.authorityFailed(w, err)
		return
	}
	channel, err := s.store.Channel(r.Context(), tx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Channel not found.")
			return
		}
		s.failed(w, err)
		return
	}
	for _, roleID := range input.RoleIDs {
		if _, err := s.store.Role(r.Context(), tx, roleID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				problem(w, 400, "That role does not exist.")
				return
			}
			s.failed(w, err)
			return
		}
	}
	for _, userID := range input.UserIDs {
		var active int
		if err := tx.QueryRow(`SELECT count(*) FROM users WHERE id=? AND is_banned=0 AND is_deactivated=0`, userID).Scan(&active); err != nil {
			s.failed(w, err)
			return
		}
		if active != 1 {
			problem(w, 400, "That account is not active.")
			return
		}
	}
	previous, err := s.store.ChannelAccessLists(r.Context(), tx, channel.ID)
	if err != nil {
		s.failed(w, err)
		return
	}
	before, _ := channelAudienceFor(s, r, tx, channel, previous[channel.ID])
	if _, err := tx.Exec(`UPDATE channels SET visibility=?,updated_at=? WHERE id=?`, input.Visibility, db.Now(), channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	if err := db.SetChannelAccess(r.Context(), tx, channel.ID, input.RoleIDs, input.UserIDs, user.ID); err != nil {
		s.failed(w, err)
		return
	}
	if _, err := db.BumpAuthorizationRevision(r.Context(), tx); err != nil {
		s.failed(w, err)
		return
	}
	// The audience after the change is resolved from the same transaction that commits it,
	// so the accounts told to refetch are exactly those the committed change affects and no
	// separate read can see an interleaved revision.
	updated := channel
	updated.Visibility = input.Visibility
	after, _ := channelAudienceFor(s, r, tx, updated, db.ChannelAccess{ChannelID: channel.ID, Roles: input.RoleIDs, Users: input.UserIDs})
	if err := s.auditTx(r.Context(), tx, user.ID, "channel.access_changed", channel.ID); err != nil {
		s.failed(w, err)
		return
	}
	// Both directions matter: a newly authorized account refetches, and an account that
	// lost access clears what it cached. The commit and the signal are one step under the
	// authority fence, so a writer deciding whether to deliver an event resolved before
	// this change cannot admit it after the change commits.
	if err := s.commitAuthorityChange(r.Context(), tx, "Channel access changed.", append(before, after...)...); err != nil {
		s.failed(w, err)
		return
	}
	channel.Visibility = input.Visibility
	// The change reaches work already in flight, not only the next request: an open
	// voice stream and a file still being streamed both end here.
	s.deliveries.revoke(channel.ID)
	s.enforceVoiceAccess(r.Context(), user.ID, "Channel access changed.")
	respond(w, 200, map[string]bool{"ok": true})
}

// channelAudienceFor resolves an audience without failing the request when the
// lookup cannot be completed; notification is best effort, authorization is not.
func channelAudienceFor(s *Server, r *http.Request, q db.Queryer, channel db.Channel, access db.ChannelAccess) ([]string, int64) {
	audience, revision, err := s.channelAudience(r.Context(), q, channel, access)
	if err != nil {
		s.log.Warn("audience lookup failed", "error", err)
		return nil, 0
	}
	return audience, revision
}
