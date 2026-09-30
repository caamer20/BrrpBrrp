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
