package db

import "encoding/json"

type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	IsAdmin       bool   `json:"is_admin"`
	IsOwner       bool   `json:"is_owner"`
	IsBanned      bool   `json:"is_banned"`
	IsDeactivated bool   `json:"is_deactivated"`
	IsDeleted     bool   `json:"is_deleted"`
	AvatarColor   string `json:"avatar_color"`
	CreatedAt     string `json:"created_at"`

	// InTimeout and TimeoutEndsAt are the member's own view of a live moderation
	// timeout. They are here rather than only on the administrator's account list
	// because a timeout closes the community to that member while leaving everything
	// else they consented to working: without these, their interface shows an
	// unexplained workspace in which every community request is refused and nothing
	// says why. The client needs to be able to render "you are in a voice-only
	// timeout until 14:05" instead of an empty screen.
	//
	// They are populated on the routes that describe the caller to themselves — sign-in
	// and /api/me — and are always zero on every other account's row, so they cannot
	// become a way to learn somebody else's moderation state.
	InTimeout     bool   `json:"in_timeout"`
	TimeoutEndsAt string `json:"timeout_ends_at,omitempty"`
}

type Channel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Category      string `json:"category"`
	Position      int    `json:"position"`
	Topic         string `json:"topic"`
	MaxVoiceUsers int    `json:"max_voice_users"`
	// SlowModeSeconds is the minimum interval between new messages from one
	// account. Zero disables it; channel managers can bypass the interval.
	SlowModeSeconds int `json:"slow_mode_seconds"`
	// Visibility is "public" or "private". A private channel is reachable only
	// through an explicit role or member access grant (and by community
	// administrators, who manage community channels).
	Visibility string `json:"visibility"`
	// Posting is "members" or "announcement". An announcement channel restricts
	// ordinary sends to role holders with the channel.manage capability.
	Posting               string `json:"posting"`
	AuthorizationRevision int    `json:"authorization_revision"`
}

// Category is a durable, ordered group of channels. Renaming a category
// updates the denormalized channels.category name and never changes visibility.
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Channels int    `json:"channels"`
}

// Role is an additive grant. Position orders roles for delegation: an actor may
// only act on roles strictly below its own highest position.
type Role struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Position    int      `json:"position"`
	IsDefault   bool     `json:"is_default"`
	IsSystem    bool     `json:"is_system"`
	Permissions []string `json:"permissions"`
	Members     int      `json:"members"`
}

type Attachment struct {
	ID         string `json:"id"`
	MessageID  string `json:"message_id,omitempty"`
	UploaderID string `json:"uploader_id"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	MIMEType   string `json:"mime_type"`
}

type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
}

type Message struct {
	ID string `json:"id"`
	// ConversationID is the message's conversation scope. For a channel conversation it is the
	// channel id, so legacy identifiers, links and the authorization path are unchanged. It is
	// resolved through COALESCE(conversation_id, channel_id): a row written before the schema 6
	// migration and a row written after it answer with the same conversation.
	ConversationID string          `json:"conversation_id,omitempty"`
	ChannelID      string          `json:"channel_id"`
	UserID         string          `json:"user_id"`
	Content        string          `json:"content"`
	CreatedAt      string          `json:"created_at"`
	EditedAt       string          `json:"edited_at,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Embeds         json.RawMessage `json:"embeds"`
	Attachments    []Attachment    `json:"attachments"`
	Reactions      []Reaction      `json:"reactions"`
	ReplyToID      string          `json:"reply_to_id,omitempty"`
	ReplyCount     int             `json:"reply_count"`
	Pinned         bool            `json:"pinned"`
	// ThreadID is the named thread this message belongs to, empty for an unthreaded
	// message (ADR 0017). It is part of the message rather than of the view because a
	// client that cannot see it has no way to keep a thread reply out of the channel
	// list, which deliberately excludes thread messages.
	ThreadID string `json:"thread_id,omitempty"`
}

type Settings struct {
	ServerName     string `json:"server_name"`
	LinkPreviews   bool   `json:"enable_link_previews"`
	GIFEmbeds      bool   `json:"enable_gif_embeds"`
	Typing         bool   `json:"enable_typing_indicators"`
	Presence       bool   `json:"enable_presence"`
	Voice          bool   `json:"enable_voice"`
	TURN           bool   `json:"enable_turn_server"`
	TURNPort       int    `json:"turn_port"`
	TURNExternalIP string `json:"turn_external_ip"`
	// The fallback transports. UDP alone leaves a client behind a network that blocks
	// it with no path at all, which is the ordinary case on corporate and some mobile
	// networks (ADR 0019, §7). A port of 0 means that transport is not offered.
	//
	// This field is named `turn_tls_port` and has always been used as the *plain TCP*
	// port — the `turn:...?transport=tcp` listener. The name is wrong, and the code
	// saying so plainly is better than renaming the JSON key under an operator who may
	// already have a saved configuration: a renamed key silently resets their setting
	// to the default, which for a port is a relay that no longer listens where it did.
	// The TLS listener's own port is TURNTLSListenerPort below.
	TURNTLSPort     int  `json:"turn_tls_port"`
	TURNTCPFallback bool `json:"turn_tcp_fallback"`
	// TURNTLSListenerPort is the port the `turns:` (TLS) listener binds. It is
	// distinct from TURNTLSPort above, which despite its name is the plain-TCP port.
	// A port of 0 means "choose one", which is UDP + 2.
	TURNTLSListenerPort int `json:"turn_tls_listener_port"`
	// TURNTLSFallback offers the relay over TLS as well, using the server's own
	// certificate. It is the last path a client has: a network that blocks both UDP
	// and plain TCP leaves a `turn:` relay nothing to connect on, and a `turns:` one
	// is reachable because almost nothing blocks 443.
	//
	// It is off by default because it costs the operator a port and depends on the
	// certificate being readable. When it is requested and the certificate cannot be
	// read, the relay is still offered over UDP and plain TCP and the reason is
	// logged — a fallback that silently does not exist is the failure this exists to
	// prevent.
	TURNTLSFallback bool `json:"turn_tls_fallback"`
	// TURNAdvertisedHost is what clients are told to relay through. It is separate
	// from TURNExternalIP because a host or container behind NAT has an address it
	// listens on and a different one clients can reach, and getting this wrong produces
	// a call that works locally and fails for everybody else.
	TURNAdvertisedHost string `json:"turn_advertised_host"`
	// STUNServer is the STUN URL clients are offered for server-reflexive candidates.
	//
	// It is configurable and *empty by default* rather than pointing at a public
	// server. A hardcoded third-party STUN server in a self-hosted product sends
	// every call's address discovery to a vendor, and leaves the call with no path at
	// all on an air-gapped or offline installation. An operator who wants it names
	// their own.
	STUNServer string `json:"stun_server"`
	// ForceRelay makes every call use the TURN relay and forbid direct connections.
	//
	// It exists because a direct path and a relayed path are different products, not
	// because either is better. A server that cannot be tested on a direct path is not
	// a server whose relay path has been tested either: without a switch, "the call
	// worked" never says which of the two carried it. It is off by default because
	// relaying everything costs the server bandwidth and adds latency to a call that
	// had a direct path.
	ForceRelay     bool   `json:"force_relay"`
	VoiceBitrate   int    `json:"voice_bitrate_kbps"`
	MaxUploadBytes int64  `json:"max_upload_bytes"`
	RetentionDays  int    `json:"message_retention_days"`
	Purge          bool   `json:"purge_enabled"`
	Audit          bool   `json:"audit_logs_enabled"`
	Push           bool   `json:"push_enabled"`
	Privacy        bool   `json:"privacy_mode_embeds_default"`
	LowResource    bool   `json:"low_resource_default"`
	Theme          string `json:"theme_default"`
	BackupEnabled  bool   `json:"backup_enabled"`
	BackupHours    int    `json:"backup_interval_hours"`
	BackupKeep     int    `json:"backup_keep"`
	BackupUploads  bool   `json:"backup_include_uploads"`
	// RequirePrivilegedMFA requires an owner or administrator to enroll a second
	// factor before exercising operator-tier authority. It never blocks ordinary
	// members, and the offline server commands remain available as a break-glass
	// path when an authenticator is lost.
	RequirePrivilegedMFA bool `json:"require_privileged_mfa"`
}

func DefaultSettings() Settings {
	return Settings{ServerName: "BrrpBrrp", LinkPreviews: true, GIFEmbeds: true, Typing: true, Presence: true, Voice: true, TURNPort: 3478, VoiceBitrate: 24, MaxUploadBytes: 12 << 20, Audit: true, Push: true, Theme: "dark", BackupHours: 24, BackupKeep: 7, BackupUploads: true, RequirePrivilegedMFA: true}
}

// TURNTCPPort is the plain-TCP fallback port the server will actually listen on:
// the named one, or the UDP port plus one when the operator asked for a fallback
// without naming a port. The validation and the listener have to agree on this, or a
// setting that validates is one the server then cannot bind.
func (settings Settings) TURNTCPPort() int {
	if !settings.TURNTCPFallback {
		return 0
	}
	if settings.TURNTLSPort != 0 {
		return settings.TURNTLSPort
	}
	return settings.TURNPort + 1
}
