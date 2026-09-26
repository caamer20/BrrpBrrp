import { storage } from './storage';
import { consumeUpdateDrafts, clearUpdateDrafts } from './reload-drafts';
import { api, APIError, errorMessage } from './api';
import { disableNotifications } from './notifications';
import { readDrafts, writeDrafts } from './drafts';
import {
  clearOutbox,
  operationExpired,
  recoveryEntry,
  outcomeExplanation,
  readOutbox,
  retryableStatus,
  writeOutbox,
  type OutboxEntry
} from './outbox';
import type { Attachment, Channel, Message, RealtimeEvent, WorkspaceState } from './types';

export interface ScrollPosition {
  id: string;
  offset: number;
  top: number;
}
interface ChannelView {
  messages: Message[];
  hasOlder: boolean;
  viewingHistory: boolean;
  atLatest: boolean;
  unreadAfter: string | null;
  position: ScrollPosition | null;
}
type MessageChange = { channel: string; kind: string; revision: number };

// RequestIdentity is the identity a request was issued under. It is captured before
// the request is sent and compared again when it completes, so a completion is only
// applied to the account and authority that asked for it.
interface RequestIdentity {
  session: number;
  account: string;
  generation: number;
}

class Workspace {
  state = $state<WorkspaceState | null>(null);
  setupRequired = $state(false);
  inviteToken = $state(new URLSearchParams(location.hash.slice(1)).get('invite') || '');
  loading = $state(true);
  error = $state('');
  activeId = $state('');
  messages = $state<Message[]>([]);
  messagesLoading = $state(false);
  olderLoading = $state(false);
  messagesError = $state('');
  messagesErrorOlder = $state(false);
  hasOlder = $state(false);
  viewingHistory = $state(false);
  connection = $state<'connected' | 'connecting' | 'offline'>('offline');
  sidebarOpen = $state(false);
  membersOpen = $state(window.innerWidth > 900 && storage.getItem('brrp.members-open') !== 'false');
  searchOpen = $state(false);
  settingsOpen = $state(false);
  // The Preferences section is store state so a refused privileged action can
  // send the account straight to the panel that fixes it.
  preferencesSection = $state('Appearance');
  adminOpen = $state(false);
  adminTab = $state('People');
  toast = $state('');
  channelNotificationMutes = $state<Record<string, boolean>>({});
  channelNotificationsReady = $state(false);
  theme = $state(storage.getItem('brrp.theme') || 'system');
  density = $state(storage.getItem('brrp.density') || 'comfortable');
  saveDrafts = $state(false);
  drafts = $state<Record<string, string>>({});
  replyTo = $state<Message | null>(null);
  highlightId = $state('');
  unreadAfter = $state<string | null>(null);
  atLatest = $state(true);
  pinsOpen = $state(false);
  switcherOpen = $state(false);
  threadId = $state('');
  onConversationEvent: ((event: RealtimeEvent) => void) | null = null;
  scrollRestore = $state<ScrollPosition | null>(null);
  jumpingUnread = $state(false);
  uploadCount = $state(0);
  get uploading() {
    return this.uploadCount > 0;
  }
  composerDrafts = new Map<string, { text: string; attachments: Attachment[] }>();
  composerCaptures = new Set<() => void>();
  captureComposerDrafts = () => {
    for (const capture of this.composerCaptures) capture();
    return Object.fromEntries(this.composerDrafts);
  };
  // The last cursor this client had confirmed for one conversation. A cursor only ever
  // advances, so an answer or an event that arrives out of order — and a retry after a
  // reconnect — cannot move it backward. It is the client half of the rule the server
  // enforces on the write side (ADR 0010, Decision 6).
  private readCursors: Record<string, string> = {};
  // Sends whose acknowledgement has not arrived, restored from the account's own scope of
  // the device store. They are the same user-authored text as a draft, but they were already
  // submitted once, so restoring one is a reconciliation against the operation's durable
  // record rather than a new send (ADR 0010, Decision 5).
  private outbox = $state<OutboxEntry[]>([]);
  get recoveredMessages() {
    return this.outbox.filter((entry) => entry.recovery);
  }

  dismissRecovery(key: string) {
    this.releaseOutbox(key);
  }
  private position: ScrollPosition | null = null;
  private views = new Map<string, ChannelView>();
  private messageChanges = new Map<string, MessageChange>();
  private messageRevisions = new Map<string, number>();
  private eventRevision = 0;
  private snapshots = new Set<number>();
  private snapshotChanges = new Map<string, MessageChange & { message?: Message | null }>();
  private refreshUnread = false;
  private refreshWorkspace = false;
  private flushing = false;
  private flushTimer?: ReturnType<typeof setTimeout>;
  private unreadVersion = 0;
  private stateVersion = 0;
  lowResource = $state(storage.getItem('brrp.low-resource') === 'true');
  privacy = $state(storage.getItem('brrp.privacy') === 'true');
  typing = $state<Record<string, { channel_id: string; expires_at: number }>>({});
  onVoiceEvent: ((event: RealtimeEvent) => void) | null = null;
  leaveVoice: (() => void) | null = null;
  voiceDisconnected: (() => void) | null = null;
  voiceReconnected: (() => void) | null = null;
  private socket: WebSocket | null = null;
  private reconnectTimer?: ReturnType<typeof setTimeout>;
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private toastTimer?: ReturnType<typeof setTimeout>;
  private retry = 0;
  private stopped = false;
  private loadVersion = 0;
  private sessionVersion = 0;
  private lastTyping = 0;
  private readMarker = '';
  private pending = $state<Record<string, Message>>({});
  get hasPendingMessages() {
    return Object.keys(this.pending).length > 0;
  }
  get canUpdate() {
    return !this.uploading && !this.hasPendingMessages;
  }

  get channel() {
    return this.state?.channels.find((channel) => channel.id === this.activeId);
  }
  get user() {
    return this.state?.user;
  }

  // The authorization generation advances whenever the server's view of this
  // account's authority changes: a new snapshot with a different revision, or an
  // authorization.changed event. Panels that hold their own results (search, pins,
  // a thread, a preview) capture it before they ask for data and discard an answer
  // that arrives after it moved, so a revoked view cannot be repopulated by a reply
  // that was already in flight.
  authorizationGeneration = $state(0);

  // An authorization change is only a change until a snapshot issued under the new
  // authority is applied. Until then the snapshot on screen belongs to the previous
  // authority, so the metadata derived from it — unread counts and mention badges —
  // is withheld (see unreadFor) while the structure itself is kept so navigation and
  // the recovery action stay reachable. The flag clears in refreshState once a
  // snapshot admitted under the current generation lands, which is why a failed
  // refresh leaves the view empty and unverified rather than stale.
  authorizationUnverified = $state(false);

  // identity captures the caller's identity before a request is sent. It is what
  // admitted compares a completion against: the account and session epoch the request
  // belonged to, and the authorization generation it was admitted under. There is
  // deliberately no default that reads the current session, because that is exactly
  // how a delayed response from a previous account used to be applied to the account
  // that signed in while it was in flight.
  private identity(): RequestIdentity {
    return {
      session: this.sessionVersion,
      account: this.user?.id ?? '',
      generation: this.authorizationGeneration
    };
  }

  // admitted reports whether a request that has just completed may still write into
  // the view. Anything else is an answer to a question another account or another
  // authority asked, so it is dropped instead of applied. Every asynchronous
  // completion in this class applies this one rule — history, context, incremental
  // lookups, unread counts, the snapshot, parent refreshes, channel moves, send
  // acknowledgements and their error paths.
  private admitted(request: RequestIdentity): boolean {
    return (
      request.session === this.sessionVersion &&
      request.account === (this.user?.id ?? '') &&
      request.generation === this.authorizationGeneration
    );
  }

  // unreadFor is the unread entry the interface shows for one channel. It returns
  // nothing while an authorization change is unverified, because a count or a mention
  // badge is server-derived metadata about activity in a channel this account may no
  // longer be allowed to read. A draft or a pending send is the user's own text and is
  // unaffected.
  unreadFor(channelID: string) {
    if (this.authorizationUnverified) return undefined;
    return this.state?.unread.find((item) => item.channel_id === channelID);
  }

  // Effective capabilities, straight from the snapshot. The server is the
  // authority and refuses anything it did not grant; this only keeps the interface
  // from offering a control that would always fail.
  get capabilities(): string[] {
    return this.state?.authorization?.capabilities ?? [];
  }
  get isOwner(): boolean {
    return this.state?.authorization?.is_owner ?? false;
  }
  can(capability: string): boolean {
    return this.capabilities.includes(capability);
  }
  canAny(...capability: string[]): boolean {
    return capability.some((name) => this.can(name));
  }

  // The capabilities that make the server tools meaningful. A delegated manager
  // with only member.manage still gets the panel; a plain member does not.
  static readonly managementCapabilities = [
    'member.manage',
    'message.delete_any',
    'role.manage',
    'category.manage',
    'channel.manage',
    'invite.manage',
    'settings.manage',
    'audit.read',
    'operator.backup',
    'operator.credential_recovery'
  ];
  get canManageAnything(): boolean {
    return this.canAny(...Workspace.managementCapabilities);
  }

  // Channel grouping follows the durable category order the server returns, not the
  // alphabetical order of the denormalized names on the channels, so a reordering
  // survives a reload wherever it was performed. A channel whose category no longer
  // exists still appears, after the durable groups.
  private channelGroups(type: 'text' | 'voice') {
    const channels = (this.state?.channels ?? []).filter((channel) => channel.type === type);
    const positions = new Map(
      (this.state?.categories ?? []).map((entry) => [entry.name, entry.position])
    );
    const grouped = new Map<string, Channel[]>();
    for (const channel of channels) {
      const name = channel.category || '';
      if (!grouped.has(name)) grouped.set(name, []);
      grouped.get(name)!.push(channel);
    }
    return [...grouped.entries()]
      .map(([name, list]) => ({
        name,
        channels: [...list].sort(
          (left, right) => left.position - right.position || left.name.localeCompare(right.name)
        )
      }))
      .sort((left, right) => {
        const leftPosition = positions.get(left.name) ?? Number.MAX_SAFE_INTEGER;
        const rightPosition = positions.get(right.name) ?? Number.MAX_SAFE_INTEGER;
        return leftPosition - rightPosition || left.name.localeCompare(right.name);
      });
  }
  get textGroups() {
    return this.channelGroups('text');
  }
  get voiceGroups() {
    return this.channelGroups('voice');
  }

  async start() {
    if (this.inviteToken) history.replaceState(null, '', '/');
    this.loading = true;
    this.error = '';
    try {
      const status = await api<{ setup_required: boolean }>('/setup/status');
      this.setupRequired = status.setup_required;
      if (!status.setup_required) {
        try {
          await this.enter();
        } catch (error) {
          if (!(error instanceof APIError && error.status === 401)) throw error;
        }
      }
    } catch (error) {
      this.error = errorMessage(error);
    } finally {
      this.loading = false;
    }
  }

  async enter() {
    await this.refreshState();
    this.saveDrafts = !!this.user && storage.getItem(`brrp.save-drafts.${this.user.id}`) === 'true';
    if (this.saveDrafts && this.user) this.drafts = readDrafts(storage, this.user.id);
    let restored: ReturnType<typeof consumeUpdateDrafts> = null;
    try {
      if (this.user) restored = consumeUpdateDrafts(sessionStorage, this.user.id);
    } catch {
      /* No temporary drafts. */
    }
    if (restored) {
      this.composerDrafts = new Map(Object.entries(restored.drafts));
      for (const [channel, draft] of Object.entries(restored.drafts))
        this.drafts[channel] = draft.text;
    }
    this.setupRequired = false;
    this.stopped = false;
    const openSettings = location.hash === '#server-settings';
    const requested = new URLSearchParams(location.search).get('channel');
    const initial =
      this.state?.channels.find((channel) => channel.id === requested && channel.type === 'text') ||
      this.state?.channels.find((channel) => channel.type === 'text');
    if (initial) await this.selectChannel(initial.id);
    if (restored?.reply) {
      const channel = this.activeId;
      const request = this.identity();
      try {
        const reply =
          this.messages.find((message) => message.id === restored.reply) ||
          (await api<Message>(`/messages/${restored.reply}`));
        if (channel === this.activeId && this.admitted(request) && reply.channel_id === channel)
          this.replyTo = reply;
      } catch {
        this.showToast('Your draft was kept, but the original reply is no longer available.');
      }
    }
    // Unsent sends are restored once the snapshot says which destinations this account may
    // still write to, and each one is reconciled against its own operation record before the
    // person sees it as a pending message again.
    if (this.saveDrafts && this.user) await this.restoreOutbox();
    if (openSettings && (this.can('settings.manage') || this.can('audit.read'))) {
      this.adminTab = 'Settings';
      this.adminOpen = true;
    }
    this.connect();
  }

  channelMoving = $state(false);
  draggedChannel = $state('');
  channelDropTarget = $state('');
  channelDropAfter = $state(false);

  async moveChannel(id: string, targetId: string, after: boolean) {
    if (this.channelMoving || !this.can('channel.manage') || id === targetId) return;
    // The account and session are captured before the await. A move issued by one
    // account must never replace the channel list of whichever account is signed in
    // when it returns, so the default-session admission is gone: the answer is applied
    // only if it is still the same account, session and authorization generation.
    const request = this.identity();
    this.channelMoving = true;
    try {
      const channels = await api<Channel[]>(`/channels/${id}/move`, 'POST', {
        target_id: targetId,
        after
      });
      // The returned channel list is the structure this account may see under the
      // authority that admitted the move. A completion that belongs to a previous
      // account, session or authority writes nothing at all: not the channel list, which
      // may name a channel the account can no longer read, and not the toast either, which
      // would be the previous account's action reported in the next account's view.
      if (!this.admitted(request) || !this.state) return;
      this.state.channels = channels;
      this.showToast('Channel position saved.');
    } catch (error) {
      if (this.admitted(request)) this.showToast(errorMessage(error));
    } finally {
      // The busy flag belongs to the session, so a revocation during the move does not
      // leave the next account's move control disabled forever.
      if (request.session === this.sessionVersion) this.channelMoving = false;
    }
  }

  async refreshState() {
    const request = this.identity();
    const version = ++this.stateVersion;
    // A snapshot is admitted under one authorization generation. A revocation that
    // arrives while this one is in flight makes the answer describe authority the
    // account no longer has, so it is dropped rather than applied: adopting it would
    // restore the previous channel list, capabilities and counts. The invalidation
    // that overtook it has already asked for a fresh snapshot, and that request is
    // the one that will be admitted.
    const state = await api<WorkspaceState>('/state');
    // A snapshot that belongs to a previous account, session or authority is dropped:
    // adopting it would show another account's channels, capabilities and counts.
    if (version !== this.stateVersion || !this.admitted(request)) return;
    this.authorizationUnverified = false;
    const previous = this.state?.authorization;
    this.state = state;
    // A confirmed snapshot carries each conversation's cursor, and it only ever moves one
    // forward: a snapshot that was read before this client's last mark cannot pull the
    // cursor back, and a snapshot read after it is the server agreeing with us.
    for (const unread of state.unread || [])
      if ((this.readCursors[unread.channel_id] || '') < (unread.last_read_message_id || ''))
        this.readCursors[unread.channel_id] = unread.last_read_message_id;
    // A snapshot whose authorization differs from the one on screen is a change of
    // authority even when no authorization.changed event preceded it (a refused
    // refresh that later succeeds, or a reconnect after missed events), so it
    // invalidates the panels that hold their own data — and the active conversation
    // — before anything from the old authority is shown again.
    const next = state.authorization;
    if (
      previous &&
      (previous.revision !== next?.revision ||
        (previous.capabilities ?? []).length !== (next?.capabilities ?? []).length ||
        (previous.capabilities ?? []).some((name) => !(next?.capabilities ?? []).includes(name)))
    )
      this.invalidateAuthorization();
    this.forgetUnauthorizedChannels();
    for (const message of Object.values(this.pending)) {
      if (
        !this.can('message.send') ||
        !state.channels.some((channel) => channel.id === message.channel_id)
      )
        this.finishUnsendable(message, 'You no longer have access to send to that conversation.');
    }
    if (!storage.getItem('brrp.privacy'))
      this.privacy = this.state.settings.privacy_mode_embeds_default;
    if (!storage.getItem('brrp.low-resource'))
      this.lowResource = this.state.settings.low_resource_default;
    if (this.activeId && !this.state.channels.some((channel) => channel.id === this.activeId)) {
      await this.selectChannel(
        this.state.channels.find((channel) => channel.type === 'text')?.id || ''
      );
      // Navigating away caches the conversation it leaves behind, so a channel
      // that just stopped being authorized is swept a second time.
      this.forgetUnauthorizedChannels();
    } else if (this.activeId && !this.messages.length) {
      // The conversation was cleared by an authorization change and the channel is
      // still readable, so it is fetched again. A failed fetch leaves the loading
      // state cleared and the error shown by the caller.
      await this.loadMessages();
    }
  }

  // forgetUnauthorizedChannels drops cached conversations for channels the latest
  // snapshot no longer returns. The snapshot is the authority for what this
  // account may read, so a revocation cannot be undone from local memory.
  private forgetUnauthorizedChannels() {
    if (!this.state) return;
    for (const id of [...this.views.keys()])
      if (!this.state.channels.some((channel) => channel.id === id)) this.views.delete(id);
  }

  // forgetCachedConversations discards locally held message history. It runs when
  // the server says authorization changed, so a channel this account lost access
  // to cannot be re-read from memory; drafts are the user's own text and stay.
  private forgetCachedConversations() {
    this.views.clear();
    this.messageChanges.clear();
    this.snapshotChanges.clear();
    // The per-message revisions admit an incremental lookup's answer. They belong to
    // the authority that asked for it as much as the generation does, so they are
    // dropped here too: a lookup already in flight then fails both comparisons
    // instead of writing into a conversation that was cleared for a reason.
    this.messageRevisions.clear();
  }

  // invalidateAuthorization is the single place a client-side authorization change is
  // applied. It advances the generation so panels and in-flight requests that were
  // admitted under the old authority cannot repopulate a restricted view, drops the
  // cached conversations, and clears the conversation on screen before the refetch is
  // attempted. The refetch is not guaranteed to succeed, so the restricted content is
  // removed first: a failed refresh then leaves an empty, recoverable view instead of
  // a stale one.
  //
  // This covers a revision-only change as well as a capability change. Losing read
  // access to one restricted channel does not change the capability list, so deciding
  // by capabilities alone would leave that channel's messages on screen whenever the
  // following refresh failed. A role edit elsewhere therefore reloads the open
  // conversation too: one extra fetch is the price of never showing content the
  // account may no longer read.
  invalidateAuthorization() {
    ++this.authorizationGeneration;
    this.forgetCachedConversations();
    // Anything already in flight was admitted under the old authority, and every
    // completion path compares this version as well as the generation.
    ++this.loadVersion;
    this.authorizationUnverified = true;
    this.channelNotificationMutes = {};
    this.channelNotificationsReady = false;
    this.clearActiveConversation();
    this.refreshWorkspace = true;
    this.scheduleChanges();
  }

  // invalidateWorkspace applies a structural change: channels and categories moved,
  // were renamed or were removed, but this account's own authority did not change. The
  // generation still advances so panels holding their own results cannot restore a
  // view of the old structure, and the cached conversations follow it, while the
  // conversation on screen is kept because the channel itself was not narrowed.
  invalidateWorkspace() {
    ++this.authorizationGeneration;
    this.forgetCachedConversations();
    // A reply thread renders channel content, so it is closed rather than trusted
    // until the new snapshot says the channel is still readable. Search and pins hold
    // their own results and follow authorizationGeneration instead of being closed,
    // so the query a person typed is not thrown away.
    this.threadId = '';
    this.refreshWorkspace = true;
    this.scheduleChanges();
  }

  // clearActiveConversation removes everything the current view holds that was
  // admitted under the previous authority: message content, the quoted reply it
  // renders, an open thread, the unread marker and the typing view of the channel.
  //
  // The channel selection itself is kept so navigation is not lost. The conversation
  // reloads if this account may still read it, and the next snapshot moves to a
  // permitted channel if it may not. The account's own unsent messages stay in
  // `pending`, which is the same recovery contract a draft has: they are the user's
  // own text, not server content.
  private clearActiveConversation() {
    this.messages = [];
    this.messagesLoading = !!this.activeId;
    this.olderLoading = false;
    this.messagesError = '';
    this.hasOlder = false;
    this.viewingHistory = false;
    this.atLatest = true;
    this.unreadAfter = null;
    this.position = null;
    this.scrollRestore = null;
    this.highlightId = '';
    this.readMarker = '';
    this.replyTo = null;
    this.threadId = '';
    this.typing = {};
  }

  recordScroll(channel: string, position: ScrollPosition) {
    if (channel === this.activeId) this.position = position;
  }

  private rememberChannel() {
    if (!this.activeId || !this.messages.length) return;
    this.views.delete(this.activeId);
    this.views.set(this.activeId, {
      messages: this.messages.filter((message) => !message.pending).slice(-250),
      hasOlder: this.hasOlder,
      viewingHistory: this.viewingHistory,
      atLatest: this.atLatest,
      unreadAfter: this.unreadAfter,
      position: this.position
    });
    while (this.views.size > 5) this.views.delete(this.views.keys().next().value!);
  }

  async selectChannel(id: string) {
    this.threadId = '';
    if (id === this.activeId && this.messages.length) {
      this.sidebarOpen = false;
      return;
    }
    this.rememberChannel();
    ++this.loadVersion;
    this.activeId = id;
    const cached = this.views.get(id);
    this.views.delete(id);
    const unread = this.state?.unread.find((item) => item.channel_id === id);
    this.unreadAfter =
      cached?.unreadAfter ?? (unread?.count ? unread.last_read_message_id || '' : null);
    this.replyTo = null;
    this.highlightId = '';
    this.atLatest = cached?.atLatest ?? true;
    this.viewingHistory = cached?.viewingHistory ?? false;
    this.position = cached?.position ?? null;
    this.scrollRestore = this.atLatest ? null : this.position;
    this.sidebarOpen = false;
    this.messages = [
      ...(cached?.messages || []),
      ...Object.values(this.pending).filter((message) => message.channel_id === id)
    ];
    this.messagesLoading = false;
    this.olderLoading = false;
    this.messagesError = '';
    this.hasOlder = cached?.hasOlder ?? false;
    this.readMarker = '';
    history.replaceState(null, '', `/?channel=${encodeURIComponent(id)}`);
    await this.loadMessages(false, !!cached);
  }

  async loadMessages(older = false, preserveWindow = false) {
    if (!this.activeId || (older && (this.olderLoading || this.messagesLoading))) return false;
    const id = this.activeId;
    const version = ++this.loadVersion;
    const eventRevision = this.eventRevision;
    // A history response is admitted under one account, session and authorization
    // generation. A change while it is in flight — a revocation or a switch to another
    // account — makes the answer something this account may no longer read, so it is
    // dropped instead of displayed.
    const request = this.identity();
    this.snapshots.add(version);
    const position = !this.atLatest ? this.position : null;
    const historyWindow =
      preserveWindow && !this.atLatest && this.messages.some((message) => !message.pending);
    this.olderLoading = older;
    this.messagesError = '';
    if (!this.messages.length) this.messagesLoading = true;
    try {
      const saved = this.messages.filter((message) => !message.pending);
      const before = older
        ? `?before=${saved[0]?.id || ''}`
        : historyWindow
          ? `?from=${saved[0].id}&to=${saved.at(-1)!.id}`
          : '';
      let loaded = await api<Message[]>(`/channels/${id}/messages${before}`);
      if (id !== this.activeId || version !== this.loadVersion || !this.admitted(request))
        return false;
      // Reconcile events received while this snapshot was in flight before displaying it.
      const known = new Set(loaded.map((message) => message.id));
      const changed = [...this.snapshotChanges.entries()].filter(
        ([messageID, change]) =>
          change.channel === id &&
          change.revision > eventRevision &&
          (known.has(messageID) || (!older && !historyWindow && change.kind === 'message.created'))
      );
      for (let i = 0; i < changed.length; i += 4) {
        await Promise.all(
          changed.slice(i, i + 4).map(async ([messageID, change]) => {
            if (change.kind === 'message.deleted' || change.message !== undefined) return;
            try {
              const message = await api<Message>(`/messages/${messageID}`);
              if (
                this.snapshotChanges.get(messageID)?.revision === change.revision &&
                this.admitted(request)
              )
                change.message = message;
            } catch (error) {
              if (error instanceof APIError && error.status === 404) change.message = null;
              else throw error;
            }
          })
        );
      }
      for (const [messageID, change] of this.snapshotChanges) {
        if (change.channel !== id || change.revision <= eventRevision) continue;
        const deleted = change.kind === 'message.deleted' || change.message === null;
        if (deleted)
          loaded = loaded
            .filter((message) => message.id !== messageID)
            .map((message) =>
              message.reply_to_id === messageID ? { ...message, reply_to_id: undefined } : message
            );
        else if (
          change.message &&
          (known.has(messageID) || (!older && !historyWindow && change.kind === 'message.created'))
        ) {
          loaded = [...loaded.filter((message) => message.id !== messageID), change.message].sort(
            (a, b) => a.id.localeCompare(b.id)
          );
        }
      }
      if (id !== this.activeId || version !== this.loadVersion || !this.admitted(request))
        return false;
      if (older) {
        this.viewingHistory = true;
        const known = new Set(this.messages.map((message) => message.id));
        this.messages = [
          ...loaded.filter((message) => !known.has(message.id)),
          ...this.messages
        ].slice(0, 250);
      } else {
        const previous = this.messages.filter((message) => !message.pending).at(-1);
        if (
          !this.atLatest &&
          this.unreadAfter === null &&
          previous &&
          loaded.some((message) => message.id > previous.id && message.user_id !== this.user?.id)
        )
          this.unreadAfter = previous.id;
        const keys = new Set(
          loaded
            .filter((message) => message.user_id === this.user?.id)
            .map((message) => message.idempotency_key)
        );
        for (const key of keys) delete this.pending[key];
        const pending = Object.values(this.pending).filter((message) => message.channel_id === id);
        this.messages = [...loaded, ...pending];
      }
      if (!historyWindow) this.hasOlder = loaded.length === 50;
      if (position) this.scrollRestore = { ...position };
      if (!older) await this.markRead();
      return id === this.activeId && version === this.loadVersion;
    } catch (error) {
      if (id === this.activeId && version === this.loadVersion && this.admitted(request)) {
        this.messagesError = errorMessage(error);
        this.messagesErrorOlder = older;
      }
      return false;
    } finally {
      this.snapshots.delete(version);
      if (!this.snapshots.size) this.snapshotChanges.clear();
      // A load whose authorization moved belongs to a conversation that was already
      // cleared. It reports nothing and does not touch the loading state, because
      // the clearing path owns that state now.
      if (version === this.loadVersion && this.admitted(request)) {
        this.messagesLoading = false;
        this.olderLoading = false;
      }
    }
  }

  async markRead() {
    if (document.hidden || !document.hasFocus() || !this.atLatest || this.viewingHistory) return;
    const last = this.messages.filter((message) => !message.pending).at(-1);
    if (!last || last.id === this.readMarker) return;
    // The cursor is advanced, never moved: an answer that arrives after a newer one, or a
    // conversation reloaded from an older cached window, must not mark unseen content read
    // (ADR 0010, Decision 6).
    if (last.id <= (this.readCursors[last.channel_id] || '')) return;
    this.readMarker = last.id;
    // Read state is metadata about a conversation, so it follows the same rule: a
    // read acknowledged under authority that has since changed does not rewrite the
    // counts the new snapshot will deliver.
    const request = this.identity();
    try {
      await api(`/channels/${last.channel_id}/read`, 'POST', { last_read_message_id: last.id });
      if (!this.admitted(request)) return;
      if (last.id <= (this.readCursors[last.channel_id] || '')) return;
      // The cursor moves only where the server confirmed it. A refused mark leaves it where it
      // was, so the same message is offered again rather than an assumed progress.
      this.readCursors[last.channel_id] = last.id;
      if (this.state)
        this.state.unread = this.state.unread.map((unread) =>
          unread.channel_id === last.channel_id ? { ...unread, count: 0, mentions: 0 } : unread
        );
    } catch {
      // The cursor stays where it was: a refused mark is one the server did not confirm, and
      // the next one is sent from the same place rather than from an assumed progress.
      if (this.admitted(request)) this.readMarker = '';
    }
  }

  // persistOutbox keeps the device scope in step with the sends in flight. It is the only
  // writer, so an account's scope contains exactly the entries this client still owes an
  // acknowledgement for, bounded by the store itself.
  private persistOutbox() {
    if (!this.saveDrafts || !this.user) return;
    writeOutbox(storage, this.user.id, this.outbox);
  }

  // trackOutbox records one submitted send so a lost acknowledgement is recoverable.
  private trackOutbox(message: Message, attachments: string[]) {
    if (!this.user) return;
    const entry: OutboxEntry = {
      key: message.idempotency_key,
      conversation: message.channel_id,
      content: message.content,
      attachments: attachments.slice(0, 5),
      reply_to: message.reply_to_id || '',
      created: message.created_at
    };
    this.outbox = [...this.outbox.filter((item) => item.key !== entry.key), entry];
    this.persistOutbox();
  }

  // A terminal outcome becomes an explicit recovery record. It is never inserted into a
  // different conversation's composer, and keeps the complete author text for copy/discard.
  private releaseOutbox(key: string, explanation = '') {
    const entry = this.outbox.find((item) => item.key === key);
    if (!entry) return;
    this.outbox = explanation
      ? this.outbox.map((item) => (item.key === key ? recoveryEntry(item, explanation) : item))
      : this.outbox.filter((item) => item.key !== key);
    this.persistOutbox();
    if (explanation)
      this.showToast(
        'A message needs your attention. Your text is available in Recovered messages.'
      );
  }

  private finishUnsendable(message: Message, explanation: string) {
    if (!this.outbox.some((entry) => entry.key === message.idempotency_key))
      this.trackOutbox(message, []);
    this.releaseOutbox(message.idempotency_key, explanation);
    delete this.pending[message.idempotency_key];
    this.messages = this.messages.filter(
      (item) => !item.pending || item.idempotency_key !== message.idempotency_key
    );
  }

  private async restoreOutbox() {
    if (!this.saveDrafts || !this.user) return;
    const request = this.identity();
    const entries = readOutbox(storage, this.user.id);
    // Keep the entire queue durable before the first await. A reload during reconciliation
    // must not lose the entries after the request currently in flight.
    this.outbox = entries;
    this.persistOutbox();
    for (const entry of entries) {
      if (!this.admitted(request)) return;
      if (entry.recovery) continue;
      const destination = this.state?.channels.find(
        (channel) => channel.id === entry.conversation && channel.type === 'text'
      );
      if (!destination || !this.can('message.send')) {
        this.releaseOutbox(entry.key, 'You no longer have access to send to that conversation.');
        continue;
      }
      await this.reconcileOutboxEntry(entry);
    }
    if (this.admitted(request)) this.persistOutbox();
  }

  // reconcileOutboxEntry asks the operation's durable record again, through the same send
  // path. The answer either returns the committed message — which is what an acknowledgement
  // would have been — or names the outcome that means this operation is over.
  private async reconcileOutboxEntry(entry: OutboxEntry) {
    if (!this.user) return;
    const message: Message = {
      id: entry.key,
      idempotency_key: entry.key,
      channel_id: entry.conversation,
      user_id: this.user.id,
      content: entry.content,
      reply_to_id: entry.reply_to,
      attachments: [],
      reactions: [],
      embeds: [],
      created_at: entry.created,
      pending: 'sending'
    };
    this.pending[entry.key] = message;
    // A restored send for the conversation on screen belongs in it, exactly as a send from
    // the composer does, so its outcome is visible where it happened.
    if (
      message.channel_id === this.activeId &&
      !this.messages.some((item) => item.id === entry.key)
    )
      this.messages = [...this.messages, message];
    await this.retryMessage(message);
  }

  // outboxAttachments is what a retry should claim for one operation. A restored entry knows
  // the ids it was submitted with; the component's own attachments are used when the send is
  // still in this session.
  private outboxAttachments(message: Message) {
    const entry = this.outbox.find((item) => item.key === message.idempotency_key);
    return entry ? entry.attachments : message.attachments.map((attachment) => attachment.id);
  }

  pendingReplies(id: string) {
    return Object.values(this.pending).filter((message) => message.reply_to_id === id);
  }

  discardPending(message: Message) {
    if (this.pending[message.idempotency_key]?.pending !== 'failed') return;
    delete this.pending[message.idempotency_key];
    this.releaseOutbox(message.idempotency_key);
    this.messages = this.messages.filter(
      (item) => !item.pending || item.idempotency_key !== message.idempotency_key
    );
  }

  acknowledgeMessages(messages: Message[]) {
    for (const message of messages) {
      if (message.user_id !== this.user?.id) continue;
      delete this.pending[message.idempotency_key];
      // An acknowledgement is what clears an entry, whichever path delivered it.
      this.releaseOutbox(message.idempotency_key);
      this.messages = this.messages.filter(
        (item) => !item.pending || item.idempotency_key !== message.idempotency_key
      );
    }
  }

  async send(
    content: string,
    attachments: Attachment[],
    replyToID?: string,
    preservePosition = false
  ) {
    if (!this.user || !this.activeId) return;
    const key = crypto.randomUUID();
    const message: Message = {
      id: key,
      idempotency_key: key,
      channel_id: this.activeId,
      user_id: this.user.id,
      content,
      reply_to_id: replyToID,
      preserve_position: preservePosition,
      attachments,
      reactions: [],
      embeds: [],
      created_at: new Date().toISOString(),
      pending: 'sending'
    };
    this.pending[key] = message;
    this.trackOutbox(
      message,
      attachments.map((attachment) => attachment.id)
    );
    if (!preservePosition) this.messages = [...this.messages, message];
    this.sendEvent('typing.stop', { channel_id: this.activeId });
    // A reply sent from search/history belongs at the live end of the channel.
    if (!preservePosition) {
      if (this.viewingHistory) void this.latest();
      else this.atLatest = true;
    }
    await this.retryMessage(message);
  }

  async retryMessage(message: Message) {
    if (!this.user || message.user_id !== this.user.id) return;
    if (this.outbox.some((entry) => entry.key === message.idempotency_key && entry.recovery))
      return;
    if (operationExpired({ created: message.created_at })) {
      this.finishUnsendable(
        message,
        'This send is too old to retry safely. Check the conversation before sending its text again.'
      );
      return;
    }
    if (
      this.pending[message.idempotency_key]?.pending === 'sending' &&
      message.pending === 'failed'
    )
      return;
    const sender = this.user?.id;
    // A send that completes after this account's authority moved does not write its
    // result into a conversation that was cleared: the message exists on the server
    // and returns with the refetched history if the account may still read it.
    const request = this.identity();
    this.pending[message.idempotency_key] = { ...message, pending: 'sending' };
    this.messages = this.messages.map((item) =>
      item.id === message.id ? { ...item, pending: 'sending' } : item
    );
    try {
      const saved = await api<Message>(`/channels/${message.channel_id}/messages`, 'POST', {
        content: message.content,
        reply_to_id: message.reply_to_id || '',
        idempotency_key: message.idempotency_key,
        attachment_ids: this.outboxAttachments(message)
      });
      if (this.user?.id !== sender || request.session !== this.sessionVersion) return;
      if (request.generation !== this.authorizationGeneration) {
        delete this.pending[message.idempotency_key];
        this.releaseOutbox(message.idempotency_key);
        this.refreshWorkspace = true;
        this.scheduleChanges();
        return;
      }
      delete this.pending[message.idempotency_key];
      this.releaseOutbox(message.idempotency_key);
      this.onConversationEvent?.({ t: 'message.created', d: saved });
      if (saved.reply_to_id) this.queueParentRefresh(saved.reply_to_id, saved.channel_id);
      if (
        this.activeId === message.channel_id &&
        !(message.preserve_position && (this.viewingHistory || !this.atLatest))
      ) {
        this.messages = this.messages.filter(
          (item) => item.idempotency_key !== message.idempotency_key
        );
        this.messages = [
          ...[...this.messages.filter((item) => !item.pending), saved].sort((a, b) =>
            a.id.localeCompare(b.id)
          ),
          ...this.messages.filter((item) => item.pending)
        ];
        if (!message.preserve_position) await this.markRead();
      }
    } catch (error) {
      if (this.user?.id !== sender || !this.admitted(request)) return;
      // A socket refresh can acknowledge delivery before the POST connection fails.
      if (!this.pending[message.idempotency_key]) return;
      const status = error instanceof APIError ? error.status : 0;
      if (!retryableStatus(status)) {
        // The server will answer this operation the same way again, so it is not retried: the
        // entry is released with what happened — in the server's own words, which distinguish
        // committed-and-removed from expired — and the text is offered back to the person.
        this.finishUnsendable(message, outcomeExplanation(status, errorMessage(error)));
        return;
      } else {
        this.showToast(errorMessage(error));
      }
      this.pending[message.idempotency_key] = { ...message, pending: 'failed' };
      this.messages = this.messages.map((item) =>
        item.idempotency_key === message.idempotency_key ? { ...item, pending: 'failed' } : item
      );
    }
  }

  typingStarted() {
    if (!this.state?.settings.enable_typing_indicators || Date.now() - this.lastTyping < 3000)
      return;
    this.lastTyping = Date.now();
    this.sendEvent('typing.start', { channel_id: this.activeId });
  }

  sendEvent(type: string, data: unknown) {
    if (this.socket?.readyState === WebSocket.OPEN)
      this.socket.send(JSON.stringify({ t: type, d: data }));
  }

  private connect() {
    if (this.stopped || !this.state) return;
    this.connection = 'connecting';
    const socket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws`
    );
    this.socket = socket;
    socket.onopen = () => {
      if (socket !== this.socket) return;
      this.connection = 'connected';
      this.retry = 0;
      this.voiceReconnected?.();
    };
    socket.onmessage = (event) => {
      if (socket !== this.socket) return;
      try {
        this.receive(JSON.parse(event.data));
      } catch {
        this.showToast('An update could not be read. Reconnect if messages stop arriving.');
      }
    };
    socket.onclose = () => {
      if (socket !== this.socket) return;
      this.connection = 'offline';
      this.voiceDisconnected?.();
      if (this.stopped) return;
      const session = this.sessionVersion;
      const account = this.user?.id ?? '';
      // A reconnect belongs to the account that scheduled it: neither a sign-out nor a
      // switch to another account may be followed by the previous account's retry.
      const stillCurrent = () =>
        session === this.sessionVersion && account === (this.user?.id ?? '');
      this.reconnectTimer = setTimeout(
        async () => {
          try {
            await this.refreshState();
            if (!stillCurrent()) return;
            this.connect();
          } catch (error) {
            if (!stillCurrent()) return;
            if (error instanceof APIError && error.status === 401) {
              this.stop();
              this.state = null;
            } else this.connect();
          }
        },
        Math.min(30000, 1000 * 2 ** this.retry++) + Math.random() * 400
      );
    };
  }

  private receive(event: RealtimeEvent) {
    this.onConversationEvent?.(event);
    if (event.t === 'presence.update' && this.state) this.state.presence = event.d;
    // Voice presence is delivered one channel at a time, so an update for a
    // restricted channel replaces only that channel's entries and can never
    // disturb what is shown for a channel the account is allowed to see.
    if (event.t === 'voice.state_updated' && this.state) {
      const channel = event.d.channel_id;
      this.state.voice_states = [
        ...this.state.voice_states.filter((entry) => entry.channel_id !== channel),
        ...event.d.states
      ];
    }
    if (event.t.startsWith('voice.') || event.t === 'rtc.signal') this.onVoiceEvent?.(event);
    if (event.t === 'typing.update') {
      if (event.d.typing) this.typing[event.d.user_id] = event.d;
      else delete this.typing[event.d.user_id];
    }
    if (event.t === 'notification.mention') {
      if (!this.channelNotificationsReady || this.channelNotificationMutes[event.d.channel_id])
        return;
      this.showToast(event.d.body);
      if (document.hidden && 'Notification' in window && Notification.permission === 'granted') {
        const request = this.identity();
        navigator.serviceWorker?.ready
          .then((registration) => {
            if (
              !this.admitted(request) ||
              !this.channelNotificationsReady ||
              this.channelNotificationMutes[event.d.channel_id]
            )
              return;
            return registration.showNotification(event.d.title, {
              body: event.d.body,
              tag: event.d.message_id,
              data: { url: event.d.url }
            });
          })
          .catch(() => {});
      }
    }
    if (event.t.startsWith('message.') || event.t === 'reaction.updated') {
      const { id, channel_id: channel } = event.d;
      if (typeof id === 'string' && typeof channel === 'string') {
        const revision = ++this.eventRevision;
        this.messageRevisions.set(id, revision);
        if (this.snapshots.size) this.snapshotChanges.set(id, { channel, kind: event.t, revision });
        this.messageChanges.set(id, { channel, kind: event.t, revision });
        if ((event.t === 'message.created' || event.t === 'message.deleted') && event.d.reply_to_id)
          this.queueParentRefresh(event.d.reply_to_id, channel);
        if (
          event.t === 'message.created' ||
          event.t === 'message.deleted' ||
          event.t === 'message.updated'
        )
          this.refreshUnread = true;
        this.scheduleChanges();
      }
    } else if (event.t === 'unread.updated') {
      this.refreshUnread = true;
      this.scheduleChanges();
    } else if (event.t === 'hello' || event.t === 'reconnect.required') {
      // Reload after reconnect to repair events missed while the socket was down.
      this.refreshWorkspace = true;
      this.scheduleChanges(true);
    } else if (event.t === 'workspace.changed') {
      // Structure moved, but this account's own authority did not: the open
      // conversation is kept and the panel generations still advance.
      this.invalidateWorkspace();
    } else if (event.t === 'authorization.changed') {
      // Roles, ownership, categories and channel access are not per-message data:
      // the whole snapshot is refetched. Everything derived from the old authority —
      // cached conversations, search and pin results and the generations the panels
      // captured — is invalidated first, because access can be narrowed as well as
      // widened and a copy of a channel this account can no longer open must not be
      // shown again.
      this.invalidateAuthorization();
    } else if (
      event.t.startsWith('channel.') ||
      event.t.startsWith('user.') ||
      event.t === 'settings.updated'
    ) {
      this.refreshWorkspace = true;
      this.scheduleChanges();
    }
    if (event.t === 'error') this.showToast(event.d.error);
  }

  private reconnectSync = false;
  private queueParentRefresh(id: string, channel: string) {
    const revision = ++this.eventRevision;
    // A late reply event must never replace a queued deletion of its parent.
    if (this.messageChanges.get(id)?.kind === 'message.deleted') return;
    this.messageRevisions.set(id, revision);
    const change = { channel, kind: 'message.updated', revision };
    this.messageChanges.set(id, change);
    if (this.snapshots.size) this.snapshotChanges.set(id, change);
    this.scheduleChanges();
  }

  private scheduleChanges(reconnect = false) {
    this.reconnectSync ||= reconnect;
    if (this.flushTimer || this.flushing) return;
    // Throttle rather than debounce so sustained traffic cannot postpone updates forever.
    this.flushTimer = setTimeout(() => {
      this.flushTimer = undefined;
      void this.flushChanges();
    }, 60);
  }

  private async refreshUnreadCounts() {
    const version = ++this.unreadVersion;
    // Unread state names channels and counts activity in them, so a list admitted
    // before an authorization change — or by another account — must not be written
    // after it: it can carry an entry for a channel this account may no longer read.
    const request = this.identity();
    const unread = await api<WorkspaceState['unread']>('/unread');
    if (version === this.unreadVersion && this.admitted(request) && this.state)
      this.state.unread = unread;
  }

  private async flushChanges() {
    if (!this.state || this.flushing) return;
    this.flushing = true;
    // Every lookup this flush performs — and every error it might report — belongs to
    // the account, session and authority that scheduled it. A revocation or an account
    // switch while one is in flight makes the answer something this account may no
    // longer read, and its failure something the cleared view should not be told: the
    // refetch that follows owns both now.
    const request = this.identity();
    const changes = [...this.messageChanges.entries()];
    this.messageChanges.clear();
    const refresh = this.refreshWorkspace,
      unread = this.refreshUnread,
      reconnect = this.reconnectSync;
    this.refreshWorkspace = this.refreshUnread = this.reconnectSync = false;
    try {
      if (refresh) await this.refreshState();
      if (!this.admitted(request)) return;
      if (reconnect) {
        // Cached channels retain their reading positions and revalidate when revisited.
        await this.loadMessages(false, true);
      } else {
        // At most four concurrent lookups; events in unvisited channels need only counts.
        for (let i = 0; i < changes.length; i += 4) {
          await Promise.all(
            changes.slice(i, i + 4).map(async ([id, change]) => {
              if (!this.admitted(request)) return;
              const view = change.channel === this.activeId ? this : this.views.get(change.channel);
              if (!view) return;
              const exists = view.messages.some((message) => message.id === id);
              if (change.kind === 'message.deleted') {
                this.applyMessageChange(id, change.channel, null);
                return;
              }
              if (
                !exists &&
                (change.kind !== 'message.created' || view.viewingHistory) &&
                !(view === this && this.messagesLoading)
              )
                return;
              try {
                const message = await api<Message>(`/messages/${id}`);
                if (this.admitted(request) && this.messageRevisions.get(id) === change.revision)
                  this.applyMessageChange(id, change.channel, message);
              } catch (error) {
                if (!this.admitted(request)) return;
                if (error instanceof APIError && error.status === 404) {
                  if (this.messageRevisions.get(id) === change.revision)
                    this.applyMessageChange(id, change.channel, null);
                } else throw error;
              }
            })
          );
        }
      }
      if (!this.admitted(request)) return;
      if (unread && !refresh) await this.refreshUnreadCounts();
      if (this.admitted(request)) await this.markRead();
    } catch (error) {
      if (!this.admitted(request)) return;
      if (error instanceof APIError && error.status === 401) {
        this.stop();
        this.state = null;
      } else {
        // A refresh that fails after an authorization change leaves an empty
        // conversation, not a stale one: the content was cleared before this
        // request was attempted, and the error is what the account sees instead.
        this.messagesError = errorMessage(error);
        this.messagesErrorOlder = false;
        this.messagesLoading = false;
        this.olderLoading = false;
      }
    } finally {
      if (request.session === this.sessionVersion) {
        // The in-flight flag is cleared for the session, not for the generation: a
        // revocation during this flush must not leave the worker permanently busy, or
        // the refetch it asked for could never run.
        this.flushing = false;
        // Version guards only need to live while an event's request is in flight.
        for (const [id, change] of changes)
          if (this.messageRevisions.get(id) === change.revision) this.messageRevisions.delete(id);
        if (
          this.messageChanges.size ||
          this.refreshWorkspace ||
          this.refreshUnread ||
          this.reconnectSync
        )
          this.scheduleChanges();
      }
    }
  }

  private applyMessageChange(id: string, channel: string, message: Message | null) {
    const change = this.snapshotChanges.get(id);
    if (change) change.message = message;
    const view = channel === this.activeId ? this : this.views.get(channel);
    if (!view) return;
    const existing = view.messages.some(
      (item) =>
        item.id === id ||
        (!!message &&
          item.idempotency_key === message.idempotency_key &&
          item.user_id === message.user_id)
    );
    if (message && message.user_id === this.user?.id) delete this.pending[message.idempotency_key];
    if (!existing && message && view.viewingHistory) return;
    if (!existing && message && !view.atLatest && view.messages.length >= 250) {
      view.viewingHistory = true;
      return;
    }
    if (
      message &&
      !existing &&
      !view.atLatest &&
      view.unreadAfter === null &&
      message.user_id !== this.user?.id
    )
      view.unreadAfter = view.messages.filter((item) => !item.pending).at(-1)?.id || '';
    const kept = view.messages
      .filter(
        (item) =>
          item.id !== id &&
          !(
            message &&
            item.idempotency_key === message.idempotency_key &&
            item.user_id === message.user_id
          )
      )
      .map((item) =>
        !message && item.reply_to_id === id ? { ...item, reply_to_id: undefined } : item
      );
    view.messages = [
      ...[...kept.filter((item) => !item.pending), ...(message ? [message] : [])]
        .sort((a, b) => a.id.localeCompare(b.id))
        .slice(-250),
      ...kept.filter((item) => item.pending)
    ];
  }

  async jumpToFirstUnread() {
    if (this.unreadAfter === null || this.jumpingUnread) return;
    const channel = this.activeId,
      request = this.identity(),
      after = this.unreadAfter;
    this.jumpingUnread = true;
    try {
      const result = await api<{ message_id: string }>(
        `/channels/${channel}/first-unread?after=${encodeURIComponent(after)}`
      );
      if (!this.admitted(request) || channel !== this.activeId) return;
      if (result.message_id) {
        await this.jumpToMessage(result.message_id);
        if (
          this.activeId === channel &&
          this.admitted(request) &&
          this.highlightId === result.message_id
        )
          this.unreadAfter = after;
      } else {
        this.unreadAfter = null;
        this.showToast('You’re all caught up.');
      }
    } catch (error) {
      if (this.admitted(request)) this.showToast(errorMessage(error));
    } finally {
      this.jumpingUnread = false;
    }
  }

  savePreferences() {
    storage.setItem('brrp.theme', this.theme);
    storage.setItem('brrp.low-resource', String(this.lowResource));
    storage.setItem('brrp.privacy', String(this.privacy));
    storage.setItem('brrp.density', this.density);
    if (this.user) {
      storage.setItem(`brrp.save-drafts.${this.user.id}`, String(this.saveDrafts));
      if (this.saveDrafts) {
        writeDrafts(storage, this.user.id, this.drafts);
        this.persistOutbox();
      } else {
        storage.removeItem(`brrp.drafts.${this.user.id}`);
        clearOutbox(storage, this.user.id);
      }
    }
  }

  saveDraft(channel: string, text: string) {
    this.drafts[channel] = text;
    if (this.saveDrafts && this.user) writeDrafts(storage, this.user.id, this.drafts);
  }

  async jumpToMessage(id: string) {
    const version = ++this.loadVersion;
    // The context window is channel content, so a response admitted before an
    // authorization change — or by a previous account — is dropped rather than
    // restored.
    const request = this.identity();
    this.olderLoading = false;
    try {
      const context = await api<Message[]>(`/messages/${id}/context`);
      if (version !== this.loadVersion || !this.admitted(request)) return;
      const target = context.find((message) => message.id === id);
      if (!target) return;
      if (target.channel_id !== this.activeId) this.rememberChannel();
      this.activeId = target.channel_id;
      this.position = null;
      this.scrollRestore = null;
      this.viewingHistory = true;
      this.messages = context;
      this.messagesLoading = false;
      this.messagesError = '';
      this.hasOlder = context.findIndex((message) => message.id === id) === 25;
      this.atLatest = false;
      this.unreadAfter = null;
      this.highlightId = id;
      this.replyTo = null;
      this.searchOpen = false;
      this.pinsOpen = false;
      this.threadId = '';
      history.replaceState(null, '', `/?channel=${encodeURIComponent(target.channel_id)}`);
    } catch (error) {
      if (version === this.loadVersion && this.admitted(request))
        this.showToast(errorMessage(error));
    } finally {
      if (version === this.loadVersion && this.admitted(request)) this.messagesLoading = false;
    }
  }

  async latest() {
    const channel = this.activeId;
    const loaded = await this.loadMessages();
    if (!loaded || channel !== this.activeId) return false;
    this.highlightId = '';
    this.scrollRestore = null;
    this.unreadAfter = null;
    this.viewingHistory = false;
    this.atLatest = true;
    await this.markRead();
    return true;
  }

  async refreshMessage(id: string, removed = false) {
    const channel = this.activeId;
    if (removed) {
      const message = this.messages.find((message) => message.id === id);
      this.applyMessageChange(id, channel, null);
      this.onConversationEvent?.({ t: 'message.deleted', d: { id, channel_id: channel } });
      if (message?.reply_to_id) this.queueParentRefresh(message.reply_to_id, channel);
      return;
    }
    const request = this.identity();
    const updated = await api<Message>(`/messages/${id}`);
    if (this.admitted(request) && channel === this.activeId) {
      this.messages = this.messages.map((message) => (message.id === id ? updated : message));
      this.onConversationEvent?.({ t: 'message.updated', d: updated });
    }
  }

  setMembersOpen(open: boolean) {
    this.membersOpen = open;
    if (window.innerWidth > 900) storage.setItem('brrp.members-open', String(open));
  }

  showToast(message: string) {
    this.toast = message;
    clearTimeout(this.toastTimer);
    this.toastTimer = setTimeout(() => {
      this.toast = '';
    }, 6000);
  }

  async logout() {
    this.leaveVoice?.();
    try {
      await disableNotifications().catch(() => {});
      await api('/logout', 'POST', {});
      if (this.user) {
        storage.removeItem(`brrp.drafts.${this.user.id}`);
        clearOutbox(storage, this.user.id);
      }
      this.outbox = [];
      this.drafts = {};
      try {
        clearUpdateDrafts(sessionStorage);
      } catch {
        /* Storage may be unavailable. */
      }
      this.stop();
      this.state = null;
      this.messages = [];
      this.activeId = '';
      this.replyTo = null;
      this.searchOpen = false;
      this.pinsOpen = false;
      this.settingsOpen = false;
      this.adminOpen = false;
    } catch (error) {
      this.showToast(errorMessage(error));
    }
  }

  stop() {
    ++this.sessionVersion;
    ++this.loadVersion;
    this.authorizationUnverified = false;
    this.messagesLoading = false;
    this.olderLoading = false;
    this.messagesError = '';
    this.drafts = {};
    this.messages = [];
    this.replyTo = null;
    this.typing = {};
    this.stopped = true;
    this.pending = {};
    this.outbox = [];
    this.readCursors = {};
    this.channelNotificationMutes = {};
    this.channelNotificationsReady = false;
    this.saveDrafts = false;
    this.views.clear();
    this.snapshots.clear();
    this.snapshotChanges.clear();
    this.messageChanges.clear();
    this.messageRevisions.clear();
    this.refreshWorkspace = this.refreshUnread = this.reconnectSync = this.flushing = false;
    // Busy and drag state belong to the session that started the work. A move issued by
    // the previous account must not leave the next account's reorder control disabled,
    // so the flags are cleared here as well as in the request's own completion.
    this.channelMoving = false;
    this.draggedChannel = '';
    this.channelDropTarget = '';
    this.channelDropAfter = false;
    this.position = this.scrollRestore = null;
    this.composerCaptures.clear();
    this.composerDrafts = new Map();
    this.threadId = '';
    this.switcherOpen = false;
    this.onConversationEvent = null;
    clearTimeout(this.flushTimer);
    this.flushTimer = undefined;
    this.readMarker = '';
    clearTimeout(this.reconnectTimer);
    clearTimeout(this.refreshTimer);
    const socket = this.socket;
    this.socket = null;
    this.connection = 'offline';
    socket?.close();
    this.leaveVoice?.();
  }
}

export const workspace = new Workspace();
