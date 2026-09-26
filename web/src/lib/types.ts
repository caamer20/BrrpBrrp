export interface User {
  id: string;
  username: string;
  display_name: string;
  is_admin: boolean;
  // The owner holds the powers no role can delegate, so an owner's second factor
  // is recovered on the server host instead of from this panel.
  is_owner: boolean;
  is_banned: boolean;
  is_deactivated: boolean;
  is_deleted?: boolean;
  avatar_color: string;
  created_at: string;
}

export interface Channel {
  id: string;
  name: string;
  type: 'text' | 'voice';
  category: string;
  position: number;
  topic: string;
  max_voice_users: number;
  slow_mode_seconds?: number;
  // A private channel is reachable only through an explicit role or member
  // grant, or by a community manager. The server never lists one this account
  // may not read, so a title that arrives is a title this account can open.
  visibility: 'public' | 'private';
  // An announcement channel restricts ordinary sends to role holders that can
  // manage channels.
  posting: 'members' | 'announcement';
  authorization_revision: number;
}

// Category is a durable, ordered group of channels. It is the authority for a
// category's name; channels carry a denormalized copy for display.
export interface Category {
  id: string;
  name: string;
  position: number;
  channels: number;
}

// Role is an additive grant. Position orders roles for delegation: an account
// may only act on roles strictly below its own highest position.
export interface Role {
  id: string;
  key: string;
  name: string;
  position: number;
  is_default: boolean;
  is_system: boolean;
  permissions: string[];
  members: number;
  // What the signed-in account may do with this role. The server computes them
  // from the same policy the engine enforces, so the UI does not offer a control
  // that would always be refused.
  grantable: boolean;
  editable: boolean;
  deletable: boolean;
}

// ChannelAccess answers "who can access this?" with the same rule the server
// applies when it delivers content.
export interface ChannelAccess {
  channel_id: string;
  visibility: 'public' | 'private';
  posting: 'members' | 'announcement';
  roles: string[];
  users: string[];
  effective_members: string[];
  effective_count: number;
  // Community managers can read every community channel, so a restricted
  // channel is never private from them. The inspector says so rather than
  // implying a secrecy the server does not provide.
  managers_can_read: boolean;
  // False means the server refuses restricted-channel changes until its privacy
  // checks pass, whatever this build's UI can render.
  restricted_enabled: boolean;
  authorization_revision: number;
}

export interface Invitation {
  id: string;
  created_at: string;
  expires_at: string;
  roles: string[];
  role_ids: string[];
}

export interface Attachment {
  id: string;
  file_name: string;
  file_size: number;
  mime_type: string;
}

export interface Embed {
  url: string;
  title: string;
  description?: string;
  site?: string;
  image?: string;
  video?: string;
}

export interface Message {
  id: string;
  channel_id: string;
  user_id: string;
  content: string;
  created_at: string;
  edited_at?: string;
  reply_to_id?: string;
  reply_count?: number;
  pinned?: boolean;
  idempotency_key: string;
  attachments: Attachment[];
  reactions: { emoji: string; count: number; mine: boolean }[];
  embeds: Embed[];
  pending?: 'sending' | 'failed';
  preserve_position?: boolean;
}

export interface ReplyPage {
  parent: Message;
  replies: Message[];
  has_older: boolean;
}

export interface VoiceState {
  user_id: string;
  channel_id: string;
  muted: boolean;
  deafened: boolean;
  speaking: boolean;
  admin_muted: boolean;
  admin_deafened: boolean;
}

export interface Settings {
  server_name: string;
  enable_link_previews: boolean;
  enable_gif_embeds: boolean;
  enable_typing_indicators: boolean;
  enable_presence: boolean;
  enable_voice: boolean;
  enable_turn_server: boolean;
  turn_port: number;
  turn_external_ip: string;
  voice_bitrate_kbps: number;
  max_upload_bytes: number;
  message_retention_days: number;
  purge_enabled: boolean;
  audit_logs_enabled: boolean;
  push_enabled: boolean;
  privacy_mode_embeds_default: boolean;
  low_resource_default: boolean;
  theme_default: 'dark' | 'light';
  backup_enabled: boolean;
  backup_interval_hours: number;
  backup_keep: number;
  backup_include_uploads: boolean;
}

// Authorization is the caller's effective authority, decided by the server. Every
// management control reads it instead of inferring authority from a role label, so a
// delegated manager sees exactly the functions the server would allow.
export interface Authorization {
  revision: number;
  is_owner: boolean;
  capabilities: string[];
  // owner_designated says an owner exists now; ownership_established says the
  // ceremony has ever completed, which is what distinguishes "not set up yet" from
  // "ownership was removed".
  owner_designated: boolean;
  ownership_established: boolean;
}

export interface WorkspaceState {
  user: User;
  channels: Channel[];
  users: User[];
  settings: Settings;
  unread: { channel_id: string; count: number; mentions: number; last_read_message_id: string }[];
  presence: Record<string, string>;
  voice_states: VoiceState[];
  reaction_palette: string[];
  push_public_key: string;
  categories: Category[];
  authorization: Authorization;
}

export interface RealtimeEvent {
  t: string;
  d: any;
}

export interface MessageReport {
  id: string;
  message_id: string;
  channel_id: string;
  reporter_id: string;
  author_id: string;
  reason: string;
  snapshot_text: string;
  status: 'open' | 'resolved';
  review_note: string;
  reviewed_by: string;
  created_at: string;
  updated_at: string;
  expires_at: string;
}
