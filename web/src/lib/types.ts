export interface User {
  id: string;
  username: string;
  display_name: string;
  is_admin: boolean;
  // The owner holds the powers no role can delegate, so an owner's second factor
  // is recovered on the server host instead of from this panel.
  is_owner: boolean;
  is_banned: boolean;
  // Present only on the administrator's account list. A timeout is a moderation
  // state, not part of identity, so it is not on the user record other readers see.
  in_timeout?: boolean;
  // The caller's own live timeout, on sign-in and on /api/me and nowhere else. The
  // member is the only person entitled to be told when their community access
  // returns, and they cannot be told by a screen of refusals.
  timeout_ends_at?: string;
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
  conversation_id?: string;
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
  // The named thread this message belongs to, absent for an unthreaded message
  // (ADR 0017). A channel's view excludes these: they are read from the thread.
  thread_id?: string;
}

export interface ReplyPage {
  parent: Message;
  replies: Message[];
  has_older: boolean;
}

// What a participant is sending (ADR 0019, §4). A publication is an object with an
// identity, not a position in a fixed list of tracks, so a screen share and a
// microphone are separate things that can exist at the same time and be stopped
// independently.
export type PublicationKind = 'microphone' | 'screen_video' | 'screen_audio';

export interface VoicePublication {
  id: string;
  owner: string;
  kind: PublicationKind;
  /** The effective state: the participant's own choice and the server's permission
   * decision combined. A muted publication is still published. */
  muted: boolean;
  /** Increments on every stop and restart, so a client reconnecting mid-publication
   * can tell a resume from a new publication that reused the slot. */
  generation: number;
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
  stun_server: string;
  force_relay: boolean;
  turn_external_ip: string;
  turn_tcp_fallback: boolean;
  turn_tls_fallback: boolean;
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

// Private destinations are conversations, never fabricated community channels.
export interface DirectConversation {
  id: string;
  kind: 'direct';
  state: string;
  peer: User;
  can_read: boolean;
  can_write: boolean;
  blocked_by_me: boolean;
  blocked: boolean;
  request_state: 'pending' | 'accepted' | 'rejected';
  request_direction: 'incoming' | 'outgoing' | 'none';
  introduction: string;
  hidden: boolean;
  updated_at: string;
  count: number;
  mentions: number;
  last_read_message_id?: string;
}

export interface WorkspaceState {
  user: User;
  channels: Channel[];
  users: User[];
  settings: Settings;
  unread: {
    channel_id: string;
    conversation_id?: string;
    count: number;
    mentions: number;
    last_read_message_id: string;
  }[];
  presence: Record<string, string>;
  voice_states: VoiceState[];
  reaction_palette: string[];
  push_public_key: string;
  categories: Category[];
  authorization: Authorization;
  features?: { private_conversations: boolean };
  conversations?: DirectConversation[];
  groups?: GroupConversation[];
  // outstanding_incoming is the count of pending requests addressed to this
  // account that are still actionable. It is a badge, not an activity feed: no
  // request introduction is carried here, so a closed workspace never pulls
  // pending requests into a panel that is not open (ADR 0013).
  requests?: { outstanding_incoming: number };
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
  // Assignment is a claim about who is expected to review this, not a grant: the
  // review itself still requires the reviewer's own current capability (ADR 0018).
  assigned_to: string;
  assigned_at: string;
}

// A consenting group (ADR 0016). A group is a conversation the account has been
// accepted into, not a list of people, so what the interface needs is who runs it,
// how many are in it and what this account may do — never its messages, which are
// read through the conversation the same shared model every other message is.
export interface Group {
  id: string;
  name: string;
  owner_id: string;
  owner_name: string;
  members: number;
  pending_invitations: number;
  created_at: string;
  my_state: string;
  can_invite: boolean;
  can_transfer: boolean;
}

// A group invitation, in one of three states that are genuinely different: one the
// caller can act on, one they already answered, and one their own group sent that
// they have no part in. `my_state` is what distinguishes them.
export interface GroupInvitation {
  id: string;
  group_id: string;
  group_name: string;
  inviter_name: string;
  state: 'pending' | 'accepted' | 'rejected' | 'revoked' | 'expired';
  created_at: string;
  expires_at: string;
  my_state: 'invited' | 'member' | 'sent';
}

// A group member, as far as the owner needs to know for the three actions that
// ownership governs: invite, remove and transfer.
export interface GroupMember {
  id: string;
  username: string;
  display_name: string;
  state: string;
  is_owner: boolean;
}

// A group in the workspace snapshot. A group is not a direct conversation with a
// synthesised second participant — it has no peer — so it carries its own identity
// and the facts the interface needs about it. It resolves through the same
// navigation, read cursor, outbox and composer as a direct pair (ADR 0016).
export interface GroupConversation {
  id: string;
  kind: 'group';
  state: string;
  name: string;
  owner_id: string;
  owner_name: string;
  members: number;
  can_read: boolean;
  can_write: boolean;
  created_at: string;
  updated_at: string;
  can_invite: boolean;
  can_transfer: boolean;
  pending_invitations: number;
}

// A named community thread (ADR 0017). It is a grouping inside a channel
// conversation, not a second message store, so it has no messages of its own in the
// snapshot: its replies are read from the thread.
export interface Thread {
  id: string;
  conversation_id: string;
  root_message_id: string;
  title: string;
  state: 'active' | 'archived' | 'locked';
  created_by: string;
  created_at: string;
  updated_at: string;
  reply_count: number;
  last_reply_at: string;
  followers: number;
  following: boolean;
  // This reader's own position in the thread, measured against the thread cursor —
  // never the channel's. A thread badge and a channel badge answer different
  // questions and each moves only when its own position moves.
  unread: number;
  unread_mentions: number;
}
