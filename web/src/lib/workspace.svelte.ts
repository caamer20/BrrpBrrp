import { watchingNow } from './active-view';
import { storage } from './storage';
import { consumeUpdateDrafts, clearUpdateDrafts } from './reload-drafts';
import { api, APIError, errorMessage } from './api';
import { disableNotifications } from './notifications';
import { readDrafts, writeDrafts } from './drafts';
import {
  readOperations,
  writeOperations,
  clearOperations,
  type ComposerOperation
} from './operations';
import {
  clearOutbox,
  isOperationKey,
  operationExpired,
  recoveryEntry,
  outcomeExplanation,
  readOutbox,
  retryableStatus,
  writeOutbox,
  type OutboxEntry
} from './outbox';
import type { Attachment, Channel, Message, RealtimeEvent, WorkspaceState } from './types';

// resourceForMessages is the path segment every message operation uses. It is named
// once because the two places that build a message URL are the two places that would
// otherwise drift apart on a rename.
const resourceForMessages = 'messages';

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

// ThreadSendOutcome is what one thread send left behind, as the caller needs it.
// `sent` is the only outcome that resolved as a stored message. `refused` provably
// wrote nothing — a definite answer, a missing destination or no capability — so the
// operation key is still free. `uncertain` may have committed and must be reconciled
// under the same key and text. `dropped` belongs to a view that no longer exists, so
// there is nothing to show in it.
export type ThreadSendOutcome =
  | { state: 'sent'; message: Message }
  | { state: 'refused'; error?: unknown }
  | { state: 'uncertain'; error: unknown }
  | { state: 'dropped' };

// RequestIdentity is the identity a request was issued under. It is captured before
// the request is sent and compared again when it completes, so a completion is only
// applied to the account and authority that asked for it.
export interface RequestIdentity {
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
  messagesOpen = $state(false);
  // Which section the messages dialog opens on, so the sidebar badge can offer a
  // direct way into Requests rather than making a member hunt through tabs to act
  // on a count they were just shown.
  messagesTab = $state<'conversations' | 'requests' | 'activity' | 'groups' | 'people' | 'blocked'>(
    'conversations'
  );
  // openMessagesTab is the single way the sidebar chooses which section the
  // messages dialog opens on. Doing it in one place means the dialog's own
  // read-on-open cannot disagree with what the entry just set, which is what
  // made a stale tab outlive the click that was supposed to change it.
  openMessagesTab(
    tab: 'conversations' | 'requests' | 'activity' | 'groups' | 'people' | 'blocked'
  ) {
    this.messagesTab = tab;
    this.messagesOpen = true;
  }
  contactsVersion = $state(0);
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
  // The member's own data export. It is a read path that leaves the building, so it
  // is a deliberate action with its own panel rather than a link hidden somewhere.
  exportOpen = $state(false);
  // The Preferences section is store state so a refused privileged action can
  // send the account straight to the panel that fixes it.
  preferencesSection = $state('Appearance');
  adminOpen = $state(false);
  adminTab = $state('People');
  toast = $state('');
  channelNotificationMutes = $state<Record<string, boolean>>({});
  channelNotificationsReady = $state(false);
  // Mention policy is account state, not conversation-header state: it loads
  // independently of the selected conversation, so a session that stays in a
  // direct conversation still verifies it (review 8, R8-02). Unverified policy
  // stays fail-closed.
  channelNotificationError = $state('');
  private notificationsLoadVersion = 0;
  private notificationMutations = 0;
  private notificationRetry = 0;
  // A preference refresh that has been asked for but not yet performed. It is
  // separate from `channelNotificationsReady` so a scheduled flush carries the
  // request even when the ready flag was already true, and it is a latch rather
  // than a count: several signals arriving inside one throttle window are one
  // re-read, not one per event.
  private preferencesRefreshPending = false;
  private preferencesFocusedAt = 0;
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
  // A named thread is a different thing from a reply thread: it is a channel
  // conversation with a subject, its own window and its own state. It is tracked
  // separately so closing a reply thread does not close the named one, and so the two
  // can never be confused for each other's history.
  namedThreadId = $state('');
  // A channel's thread list, opened from the channel header.
  threadsOpen = $state(false);

  // Whether this account may manage channels. This only decides which controls are
  // offered; every one of them is re-checked on the server, so hiding a control is a
  // courtesy and never the authorization.
  get canManageChannels() {
    return this.isOwner || this.can('channel.manage');
  }
  // The most recent conversation events, so an open thread view can notice a reply
  // addressed to it. Bounded: this is a nudge to refetch, not a history to keep.
  conversationEvents = $state<RealtimeEvent[]>([]);
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
  // The account the queue in memory belongs to. Entries are loaded and written for one
  // account, and a session that has moved must not write them under the new account's
  // key: that would move one account's unconfirmed sends into another's device scope.
  private outboxAccount = '';
  // Which unresolved operation each composer's submitted words belong to, by slot. The
  // outbox says what the account still owes an answer for; this says which of those
  // operations a draft is a retry of. It is reactive ($state) because a component's key
  // selection derives from it: a register written while the composer is open must
  // invalidate that derivation, and an untracked write would leave the first (empty)
  // answer cached forever (review 17, V17-01 — caught by the rendered journey).
  private composerOperations = $state(new Map<string, ComposerOperation[]>());
  private operationsAccount = '';
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
  typing = $state<
    Record<string, { channel_id?: string; conversation_id?: string; expires_at: number }>
  >({});
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
  // The account whose persisted drafts were deliberately cleared in this page session:
  // a sign-out, or draft saving turned off. A mounted composer that unmounts afterwards
  // must not write its text back into a scope the person just asked to clear.
  private draftsClearedFor = '';
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
  /**
   * Follows an activity entry to where it happened.
   *
   * Called directly rather than by setting `location.hash`: the hash is read once, when
   * the workspace enters, so writing it while the app is already running would change
   * the address bar and nothing else. A link that changes the address bar and not the
   * page is worse than a button that says nothing happened.
   *
   * The destination is a route, not a grant. Authority is re-read when the conversation
   * is opened, so a stale or forged destination opens whatever that account may now read
   * and nothing more.
   */
  async followActivityDestination(entry: { kind: string; conversation_id?: string }) {
    if (entry.kind === 'request') {
      this.openMessagesTab('requests');
      return;
    }
    const wanted = entry.conversation_id;
    if (!wanted) return;
    if (
      this.isPrivate(wanted) ||
      this.state?.channels.some((c) => c.id === wanted && c.type === 'text')
    ) {
      // The dialog closes because the conversation it was covering is now behind it.
      this.messagesOpen = false;
      await this.selectChannel(wanted);
    }
  }

  get privateConversationsEnabled() {
    return this.state?.features?.private_conversations === true;
  }

  // A live community timeout. The server reports this on sign-in and on `/api/me`, so
  // it is the account's own state rather than something inferred from a failed request.
  // Without it the interface shows an empty workspace with no explanation, because
  // every community call is refused and the member has no way to know why.
  //
  // It gates only the community. Direct messages, groups and activity are untouched,
  // which is the whole content of the restriction and is why the notice says so.
  get inTimeout() {
    return this.user?.in_timeout === true && !!this.user.timeout_ends_at;
  }
  // The end time, formatted for the member's locale and time zone. A raw timestamp
  // here would be technically correct and practically useless.
  get timeoutEndsLabel() {
    const ends = this.user?.timeout_ends_at;
    if (!ends) return '';
    const when = new Date(ends);
    if (Number.isNaN(when.getTime())) return '';
    return when.toLocaleString(undefined, {
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit'
    });
  }
  // The outstanding incoming-request count. It comes from the account snapshot and
  // is therefore durable: a cold start, a restart and a reconnect all agree. It
  // counts pending requests this account can still act on, and nothing else — an
  // outgoing, rejected, withdrawn, blocked or unavailable request is not in it, and
  // opening the messages dialog does not clear it (ADR 0013).
  //
  // Zero is also the value before the first snapshot arrives, so the badge stays
  // hidden while the account or its authority is unverified rather than claiming
  // there is nothing outstanding when nothing has been confirmed.
  get outstandingIncomingRequests() {
    if (this.authorizationUnverified || !this.privateConversationsEnabled) return 0;
    return this.state?.requests?.outstanding_incoming ?? 0;
  }
  get directConversations() {
    return this.privateConversationsEnabled
      ? (this.state?.conversations ?? []).filter((item) => item.kind === 'direct' && item.can_read)
      : [];
  }
  get direct() {
    return this.directConversations.find((item) => item.id === this.activeId);
  }
  // A group is the other kind of private conversation. It resolves through the same
  // navigation, read cursor, outbox, drafts and composer as a direct pair, so the
  // distinction is carried once here rather than re-decided at each use — which is
  // how a route ends up sending a group to the community channel path (V13-01).
  get groups() {
    return this.privateConversationsEnabled ? (this.state?.groups ?? []) : [];
  }
  get group() {
    return this.groups.find((item) => item.id === this.activeId);
  }
  isGroup(id: string) {
    return this.groups.some((item) => item.id === id);
  }
  isPrivate(id: string) {
    return this.isDirect(id) || this.isGroup(id);
  }
  userFor(id: string) {
    return (
      this.state?.users.find((user) => user.id === id) ||
      this.directConversations.find((item) => item.peer.id === id)?.peer
    );
  }
  get conversationName() {
    return (
      this.channel?.name ||
      this.group?.name ||
      this.direct?.peer.display_name ||
      this.direct?.peer.username ||
      'Conversation'
    );
  }
  conversationId(value: { conversation_id?: string; channel_id?: string }) {
    return value.conversation_id || value.channel_id || '';
  }
  isDirect(id: string) {
    return this.directConversations.some((item) => item.id === id);
  }
  canReadConversation(id: string) {
    return (
      !!this.state?.channels.some((item) => item.id === id && item.type === 'text') ||
      this.isPrivate(id)
    );
  }
  canWriteConversation(id: string) {
    const direct = this.directConversations.find((item) => item.id === id);
    if (direct) return direct.can_write;
    const group = this.groups.find((item) => item.id === id);
    if (group) return group.can_write;
    return (
      !!this.state?.channels.some((item) => item.id === id && item.type === 'text') &&
      this.can('message.send')
    );
  }
  conversationPath(id: string, resource: string) {
    return `/${this.isPrivate(id) ? 'conversations' : 'channels'}/${id}/${resource}`;
  }
  // messagePath is where one message's operation actually goes. A thread reply is a
  // *destination* like any other: the same route, the same receipts, the same
  // outbox, and a different place for the result to land.
  //
  // Thread sends used to have their own HTTP call, which meant they had their own
  // outbox (none), their own draft handling (none) and their own account fence
  // (none) — so a lost response, a refused send and an account switch all behaved
  // differently in a thread than in the channel it lives in. Routing through the
  // shared path is what makes those three cases the same case.
  messagePath(message: Pick<Message, 'conversation_id' | 'channel_id' | 'thread_id'>) {
    const conversation = this.conversationId(message);
    if (message.thread_id)
      return `/threads/${encodeURIComponent(message.thread_id)}/${resourceForMessages}`;
    return this.conversationPath(conversation, resourceForMessages);
  }
  conversationURL(id: string) {
    return `/?${this.isPrivate(id) ? 'conversation' : 'channel'}=${encodeURIComponent(id)}`;
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
    const confirmed = this.state?.unread.find((item) => this.conversationId(item) === channelID);
    if (confirmed) return confirmed;
    const direct = this.directConversations.find((item) => item.id === channelID);
    if (direct)
      return {
        channel_id: direct.id,
        count: direct.count ?? 0,
        mentions: 0,
        last_read_message_id: direct.last_read_message_id ?? ''
      };
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
    // A private-evidence reviewer manages nothing about the community, but the
    // panel is their only way to reach the queue at all, so this capability has to
    // open it. It grants the server tools, not the conversations the evidence
    // came from — those stay unreachable however the account is signed in.
    'report.review_private',
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
    this.restoreComposerOperations();
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
    // A push notification's payload says `/messages`, and a notification click
    // navigates the app there. Nothing opened that path, so clicking a private
    // notification landed on whatever the account happened to be looking at — or on
    // nothing at all. It is handled here, on a supported route under current
    // authority, and the hash is cleared so a reload does not reopen it.
    // A message permalink is resolved before anything else opens, so a link somebody
    // was sent lands on the message rather than on whatever the account last had open.
    if (location.hash.startsWith('#message=')) {
      await this.openPermalink(location.hash);
      history.replaceState(null, '', location.pathname + location.search);
    }
    const notifyActivity = location.hash === '#messages' || location.hash === '#activity';
    if (notifyActivity) {
      this.openMessagesTab('activity');
      history.replaceState(null, '', location.pathname + location.search);
    }
    // An activity entry's own destination. `#requests` opens the tab a request belongs
    // to; `#conversation=<id>` opens that conversation, which is how a message entry
    // takes the person to the message rather than to the inbox they came from.
    //
    // Both are acted on before the history is cleared, and both fall through to the
    // ordinary conversation selection below when they name nothing openable — a stale
    // link lands on the workspace rather than on an error.
    const entryTarget = /^(#requests|#conversation=)/.exec(location.hash);
    if (entryTarget) {
      if (location.hash === '#requests') this.openMessagesTab('requests');
      else {
        const wanted = decodeURIComponent(location.hash.slice('#conversation='.length));
        if (
          this.isPrivate(wanted) ||
          this.state?.channels.some((c) => c.id === wanted && c.type === 'text')
        ) {
          await this.selectChannel(wanted);
        }
      }
      history.replaceState(null, '', location.pathname + location.search);
    }
    const params = new URLSearchParams(location.search);
    const requested = params.get('conversation') || params.get('channel');
    const initial =
      this.state?.channels.find((channel) => channel.id === requested && channel.type === 'text') ||
      this.state?.channels.find((channel) => channel.type === 'text');
    if (requested && this.isPrivate(requested)) await this.selectChannel(requested);
    else if (initial) await this.selectChannel(initial.id);
    if (restored?.reply) {
      const channel = this.activeId;
      const request = this.identity();
      try {
        const reply =
          this.messages.find((message) => message.id === restored.reply) ||
          (await api<Message>(`/messages/${restored.reply}`));
        if (
          channel === this.activeId &&
          this.admitted(request) &&
          this.conversationId(reply) === channel
        )
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
    const privateAccess = (snapshot: WorkspaceState | null) =>
      JSON.stringify([
        snapshot?.features?.private_conversations === true,
        (snapshot?.conversations || [])
          .map((item) => [item.id, item.can_read, item.can_write])
          .sort((a, b) => String(a[0]).localeCompare(String(b[0])))
      ]);
    const previousPrivateAccess = privateAccess(this.state);
    this.state = state;
    // A confirmed snapshot carries each conversation's cursor, and it only ever moves one
    // forward: a snapshot that was read before this client's last mark cannot pull the
    // cursor back, and a snapshot read after it is the server agreeing with us.
    for (const unread of state.unread || [])
      if (
        (this.readCursors[this.conversationId(unread)] || '') < (unread.last_read_message_id || '')
      )
        this.readCursors[this.conversationId(unread)] = unread.last_read_message_id;
    for (const direct of this.directConversations)
      if ((this.readCursors[direct.id] || '') < (direct.last_read_message_id || ''))
        this.readCursors[direct.id] = direct.last_read_message_id || '';
    // A snapshot whose authorization differs from the one on screen is a change of
    // authority even when no authorization.changed event preceded it (a refused
    // refresh that later succeeds, or a reconnect after missed events), so it
    // invalidates the panels that hold their own data — and the active conversation
    // — before anything from the old authority is shown again.
    const next = state.authorization;
    if (
      previous &&
      (previousPrivateAccess !== privateAccess(state) ||
        previous.revision !== next?.revision ||
        (previous.capabilities ?? []).length !== (next?.capabilities ?? []).length ||
        (previous.capabilities ?? []).some((name) => !(next?.capabilities ?? []).includes(name)))
    )
      this.invalidateAuthorization();
    this.forgetUnauthorizedChannels();
    for (const message of Object.values(this.pending)) {
      if (!this.canWriteConversation(this.conversationId(message)))
        this.finishUnsendable(message, 'You no longer have access to send to that conversation.');
    }
    if (!storage.getItem('brrp.privacy'))
      this.privacy = this.state.settings.privacy_mode_embeds_default;
    if (!storage.getItem('brrp.low-resource'))
      this.lowResource = this.state.settings.low_resource_default;
    if (this.activeId && !this.canReadConversation(this.activeId)) {
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
    // Mention policy verifies here rather than in a header component, so a session
    // that spends its life in a direct conversation still learns it (R8-02).
    if (!this.channelNotificationsReady || this.preferencesRefreshPending)
      void this.loadChannelNotifications();
  }

  // requestPreferenceRefresh asks for the account's cached policy to be re-read.
  //
  // The map is dropped and the ready flag cleared before the read is scheduled, so
  // a mention arriving in the gap is refused (fail-closed) rather than answered
  // from an answer this session already knows is stale. The read itself runs on the
  // ordinary throttle, so a burst of signals costs one fetch.
  private requestPreferenceRefresh() {
    this.channelNotificationMutes = {};
    this.channelNotificationsReady = false;
    // The latch is set before the read starts: the read clears it only when its
    // answer is admitted, so a signal arriving while this read is in flight leaves
    // the request owed rather than silently dropped when this read is discarded.
    this.preferencesRefreshPending = true;
    if (this.state) void this.loadChannelNotifications();
    else ++this.notificationsLoadVersion;
  }

  // reverifyPreferences re-reads account-owned policy when a window regains focus.
  //
  // This used to live in the channel header, which is unmounted while a direct
  // conversation is open — the exact place a stale mute survived indefinitely
  // (review 9, R9-02). A tab in the background receives no focus event while
  // hidden, so the one it raises on return is when a change made elsewhere has
  // most likely gone unseen. It is rate-limited rather than fired per event, and
  // the elapsed-time gate is not a security control: the read is authorized and
  // the answer is admitted like any other.
  reverifyPreferences() {
    if (this.stopped || !this.state) return;
    const now = Date.now();
    if (now - this.preferencesFocusedAt < 1000) return;
    this.preferencesFocusedAt = now;
    this.requestPreferenceRefresh();
  }

  // loadChannelNotifications fetches the account's channel mute map. The answer is
  // applied only while the account, session and authority that asked are still
  // current, and a mute write that began during the read owns the result over the
  // older list. Failures keep the policy unverified (fail-closed) with a bounded
  // retry so a transient error does not silence allowed mentions for the view's life.
  async loadChannelNotifications(): Promise<boolean> {
    if (this.stopped || !this.user || !this.state || this.authorizationUnverified) return false;
    const request = this.identity();
    const version = ++this.notificationsLoadVersion;
    const mutations = this.notificationMutations;
    try {
      const preferences = await api<{ channel_id: string; muted: boolean }[]>(
        '/me/channel-notifications'
      );
      if (this.stopped || !this.admitted(request) || version !== this.notificationsLoadVersion)
        return false;
      if (mutations !== this.notificationMutations)
        // A mute write landed while this read was in flight; its answer is newer.
        return false;
      this.channelNotificationMutes = Object.fromEntries(
        preferences.map((entry) => [entry.channel_id, entry.muted])
      );
      this.channelNotificationsReady = true;
      this.channelNotificationError = '';
      this.notificationRetry = 0;
      // The refresh this read was answering is now answered. A later signal clears
      // it again, so a request that arrived while this read was in flight is not
      // lost and one that never arrived is not retried.
      this.preferencesRefreshPending = false;
      return true;
    } catch (error) {
      if (this.stopped || !this.admitted(request) || version !== this.notificationsLoadVersion)
        return false;
      this.channelNotificationError = errorMessage(error);
      if (this.notificationRetry < 3) {
        const attempt = ++this.notificationRetry;
        setTimeout(() => {
          if (!this.stopped) void this.loadChannelNotifications();
        }, attempt * 2000);
      }
      return false;
    }
  }

  // setChannelNotificationMuted records one channel's mute. The mutation count moves
  // before the request, so a preference read already in flight cannot be applied over
  // this change's own answer when it completes second. An admitted failure is thrown
  // for the caller to show; a stale one is swallowed because its view is gone.
  async setChannelNotificationMuted(channel: string, muted: boolean): Promise<boolean> {
    if (this.stopped || !this.user || this.authorizationUnverified) return false;
    const request = this.identity();
    const mutation = ++this.notificationMutations;
    try {
      const result = await api<{ channel_id: string; muted: boolean }>(
        `/channels/${channel}/notifications`,
        'PUT',
        { muted }
      );
      if (this.stopped || !this.admitted(request) || mutation !== this.notificationMutations)
        return false;
      this.channelNotificationMutes[result.channel_id] = result.muted;
      this.channelNotificationsReady = true;
      return true;
    } catch (error) {
      if (!this.stopped && this.admitted(request) && mutation === this.notificationMutations)
        throw error;
      return false;
    }
  }

  // forgetUnauthorizedChannels drops cached conversations for channels the latest
  // snapshot no longer returns. The snapshot is the authority for what this
  // account may read, so a revocation cannot be undone from local memory.
  private forgetUnauthorizedChannels() {
    if (!this.state) return;
    for (const id of [...this.views.keys()])
      if (!this.canReadConversation(id)) this.views.delete(id);
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
    this.channelNotificationError = '';
    // In-flight preference reads answered under the old authority are dropped; the
    // retry budget resets so the replacement load is not throttled by old failures.
    ++this.notificationsLoadVersion;
    this.notificationRetry = 0;
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
    this.namedThreadId = '';
    this.threadsOpen = false;
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
    this.namedThreadId = '';
    this.threadsOpen = false;
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
    // A named thread belongs to the channel it is in. Opening another channel closes
    // it, the same as a reply thread, because the view it holds is that channel's.
    this.namedThreadId = '';
    if (id === this.activeId && this.messages.length) {
      this.sidebarOpen = false;
      return;
    }
    this.rememberChannel();
    ++this.loadVersion;
    this.activeId = id;
    const cached = this.views.get(id);
    this.views.delete(id);
    const unread = this.unreadFor(id);
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
      ...Object.values(this.pending).filter((message) => this.conversationId(message) === id)
    ];
    this.messagesLoading = false;
    this.olderLoading = false;
    this.messagesError = '';
    this.hasOlder = cached?.hasOlder ?? false;
    this.readMarker = '';
    history.replaceState(null, '', this.conversationURL(id));
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
      let loaded = await api<Message[]>(`${this.conversationPath(id, 'messages')}${before}`);
      if (id !== this.activeId || version !== this.loadVersion || !this.admitted(request))
        return false;
      // A channel's view is the channel's own messages. A thread reply belongs to the
      // thread and is read from there, and the server's page already excludes it — but
      // a message folded in by the in-flight reconciliation below is not filtered by
      // the server, so it has to be excluded here too.
      //
      // The filter sits *after* the reconciliation, not before. Placing it before let a
      // thread reply that arrived while the page was loading slip straight back in,
      // which is the same defect the filter was added for, reappearing one step
      // later.
      //
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
      // A channel's view is the channel's own messages. A thread reply belongs to the
      // thread and is read from there, and the server's page already excludes it — but
      // a message folded in by the in-flight reconciliation above is not something the
      // server filtered, so it is excluded here too.
      //
      // The filter sits *after* the reconciliation, not before. Placed before it, a
      // thread reply that arrived while the page was loading slipped straight back in:
      // the same defect the filter exists to prevent, reappearing one step later.
      if (this.channel) loaded = loaded.filter((message) => !message.thread_id);
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
        const pending = Object.values(this.pending).filter(
          (message) => this.conversationId(message) === id
        );
        // A channel's view is the channel's own messages. A thread reply belongs to
        // the thread and is read from there; the server's page already excludes it, and
        // a live event has to be excluded here for the same reason, or the two views
        // disagree about the same message.
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
    if (
      !watchingNow({
        visible: !document.hidden,
        focused: document.hasFocus(),
        atLatest: this.atLatest,
        viewingHistory: this.viewingHistory
      })
    )
      return;
    const last = this.messages.filter((message) => !message.pending).at(-1);
    if (!last || last.id === this.readMarker) return;
    // The cursor is advanced, never moved: an answer that arrives after a newer one, or a
    // conversation reloaded from an older cached window, must not mark unseen content read
    // (ADR 0010, Decision 6).
    if (last.id <= (this.readCursors[this.conversationId(last)] || '')) return;
    this.readMarker = last.id;
    // Read state is metadata about a conversation, so it follows the same rule: a
    // read acknowledged under authority that has since changed does not rewrite the
    // counts the new snapshot will deliver.
    const request = this.identity();
    try {
      await api(this.conversationPath(this.conversationId(last), 'read'), 'POST', {
        last_read_message_id: last.id
      });
      if (!this.admitted(request)) return;
      if (last.id <= (this.readCursors[this.conversationId(last)] || '')) return;
      // The cursor moves only where the server confirmed it. A refused mark leaves it where it
      // was, so the same message is offered again rather than an assumed progress.
      this.readCursors[this.conversationId(last)] = last.id;
      const direct = this.directConversations.find((item) => item.id === this.conversationId(last));
      if (direct) {
        direct.count = 0;
        direct.last_read_message_id = last.id;
      }
      if (this.state)
        this.state.unread = this.state.unread.map((unread) =>
          this.conversationId(unread) === this.conversationId(last)
            ? { ...unread, count: 0, mentions: 0 }
            : unread
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
    if (this.outboxAccount !== '' && this.outboxAccount !== this.user.id) return;
    this.outboxAccount = this.user.id;
    writeOutbox(storage, this.user.id, this.outbox);
  }

  // liveComposerOperations is the association record with the dead operations removed:
  // an association is meaningful only while the durable entry it names is still
  // unresolved, at the same destination and with the same words. A restored entry that
  // expired, a released operation, or storage that dropped the newest entries all end
  // the association the same way — by no longer being able to answer for it.
  private liveComposerOperations() {
    const live = new Map<string, ComposerOperation[]>();
    for (const [slot, operations] of this.composerOperations) {
      const destination = slot.slice('thread:'.length);
      const kept = operations.filter((operation) =>
        this.outbox.some(
          (entry) =>
            entry.key === operation.key &&
            !entry.recovery &&
            entry.thread_id === destination &&
            entry.content === operation.content
        )
      );
      if (kept.length) live.set(slot, kept);
    }
    return live;
  }

  // persistComposerOperations writes the associations beside the drafts and the queue,
  // under the same opt-in, so a reload can resume a retry under its original key.
  private persistComposerOperations() {
    if (!this.saveDrafts || !this.user) return;
    if (this.operationsAccount !== '' && this.operationsAccount !== this.user.id) return;
    this.operationsAccount = this.user.id;
    writeOperations(storage, this.user.id, this.liveComposerOperations());
  }

  // restoreComposerOperations reads them back, with the drafts they describe. A
  // restored draft whose operation is still unresolved has to retry under that
  // operation's own key; without the association the same words would be submitted as
  // new work and could commit twice (review 17, V17-01). Without draft saving there is
  // nothing restored — neither drafts nor associations — so the map is emptied.
  private restoreComposerOperations() {
    this.composerOperations = new Map();
    this.operationsAccount = '';
    if (!this.saveDrafts || !this.user) return;
    this.composerOperations = new Map(Object.entries(readOperations(storage, this.user.id)));
    this.operationsAccount = this.user.id;
  }

  // recordComposerOperation is the explicit association between one composer's submitted
  // words and the operation key they were submitted under: author (this session's
  // account), destination (the slot), snapshot (the words) and key. Retry selection uses
  // this record, never a search of the outbox for matching text — identical words can be
  // two deliberate sends, and only the submission a composer recorded is a retry of it.
  private recordComposerOperation(threadID: string, key: string, content: string) {
    if (!isOperationKey(key)) return;
    const slot = `thread:${threadID}`;
    const existing = this.composerOperations.get(slot) ?? [];
    // A retry records the same association again; it keeps its place so the list stays
    // in submission order and the operation a composer is working on cannot be
    // reordered by pressing Reply. Only a genuinely new key is appended.
    const operations = existing.some((operation) => operation.key === key)
      ? existing.map((operation) => (operation.key === key ? { key, content } : operation))
      : [...existing, { key, content }];
    // The slot itself is re-inserted, so when the store is at its slot bound the
    // threads whose composers are active are the ones that stay.
    this.composerOperations.delete(slot);
    this.composerOperations.set(slot, operations);
    this.persistComposerOperations();
  }

  // forgetComposerOperations ends the retryability of one operation in the composer's
  // own record. Every path that releases an entry — acknowledged, refused, expired,
  // discarded, dropped — ends it here too, so released work cannot be replayed as a
  // retry of something that is over.
  private forgetComposerOperations(key: string) {
    let changed = false;
    for (const [slot, operations] of [...this.composerOperations]) {
      const kept = operations.filter((operation) => operation.key !== key);
      if (kept.length === operations.length) continue;
      if (kept.length) this.composerOperations.set(slot, kept);
      else this.composerOperations.delete(slot);
      changed = true;
    }
    if (changed) this.persistComposerOperations();
  }

  // acknowledgeComposerOperation is what every path that receives a stored message runs:
  // the operation is over, so its association goes, and any composer slot still holding
  // exactly the acknowledged snapshot is cleared because those words are now a message.
  // A slot holding anything newer is left alone — only the matching snapshot clears
  // (review 17, V17-01). The panel's own successful response performs the same clearing
  // through clearSentDraft; this covers the acknowledgements that arrive by websocket or
  // by reconciliation after a reload, when no panel was open to hear the answer.
  private acknowledgeComposerOperation(key: string) {
    const owner = this.identity();
    for (const [slot, operations] of [...this.composerOperations]) {
      for (const operation of operations) {
        if (operation.key === key) this.clearSentDraft(slot, operation.content, owner);
      }
    }
    this.forgetComposerOperations(key);
  }

  // threadComposerOperations lists this composer's still-unresolved operations for a
  // thread, oldest first. The composer uses it to select the key the current words were
  // already submitted under and to say honestly that other words here are unconfirmed.
  //
  // The live outbox is read before anything else so a derivation over this answer
  // depends on the entries even while this composer has no operations recorded yet;
  // an early return that skipped the read would cache the first empty answer and never
  // see the register that follows the next send (review 17, V17-01).
  threadComposerOperations(threadID: string): ComposerOperation[] {
    const live = this.outbox;
    const operations = this.composerOperations.get(`thread:${threadID}`);
    if (!operations?.length) return [];
    return operations.filter((operation) =>
      live.some(
        (entry) =>
          entry.key === operation.key &&
          !entry.recovery &&
          entry.thread_id === threadID &&
          entry.content === operation.content
      )
    );
  }

  // trackOutbox records one submitted send so a lost acknowledgement is recoverable.
  // The destination is recorded whole — thread and conversation — so a restored
  // operation is retried where it was going, never where the app happens to be.
  //
  // The first record of an operation is its immutable intent. A later write under the
  // same key — an attempted edit, a retry, a restored entry — must not be able to
  // rewrite what the key means: the server reconciles the key against the work it
  // recorded, and the client has to keep the same answer to give back to it.
  private trackOutbox(message: Message, attachments: string[]) {
    if (!this.user) return;
    if (this.outbox.some((item) => item.key === message.idempotency_key && !item.recovery)) return;
    // The queue keeps only keys this client can submit and reconcile, and those are the
    // UUIDs it generates. A key it cannot validate is not persisted as a recoverable
    // operation, because restoring it could not be trusted to mean the same send.
    if (!isOperationKey(message.idempotency_key)) return;
    const entry: OutboxEntry = {
      key: message.idempotency_key,
      conversation: this.conversationId(message),
      thread_id: message.thread_id || '',
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
    // Whatever ended this operation, it is not retryable any more: the composer's
    // association to it ends with the entry, so the same words are new work again.
    this.forgetComposerOperations(key);
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
    this.outboxAccount = this.user.id;
    // Keep the entire queue durable before the first await. A reload during reconciliation
    // must not lose the entries after the request currently in flight.
    this.outbox = entries;
    this.persistOutbox();
    for (const entry of entries) {
      if (!this.admitted(request)) return;
      if (entry.recovery) continue;
      if (!this.canWriteConversation(entry.conversation)) {
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
      channel_id: this.isDirect(entry.conversation) ? '' : entry.conversation,
      conversation_id: entry.conversation,
      // A restored thread operation retries into its thread, through the same path
      // builder a live send uses. Restoring it into the conversation instead posted
      // the original text and key to the channel it lives in.
      thread_id: entry.thread_id || undefined,
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
    // the composer does, so its outcome is visible where it happened. A thread reply is
    // not part of that conversation's listing: it is shown in the thread's own panel.
    if (
      !message.thread_id &&
      this.conversationId(message) === this.activeId &&
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

  // One thread's outstanding operations, matched by destination: a pending reply is
  // the one named by `reply_to_id` in a channel, and the one filed into `thread_id` in
  // a named thread. Matching only `reply_to_id` left a pending named-thread send
  // invisible in the thread it belonged to.
  pendingReplies(id: string) {
    return Object.values(this.pending).filter(
      (message) => message.reply_to_id === id || message.thread_id === id
    );
  }

  // threadComposerOperations above is this client's record of which unresolved
  // operations a thread composer's words belong to. The panel selects a key from that
  // association — not from the first outbox entry, and not by matching text against
  // the outbox — so a retry of the newest reply reconciles under its own key while an
  // older unresolved reply keeps its independent retry, discard and recovery state.

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
      // An acknowledgement is what clears an entry, whichever path delivered it. The
      // composer's association ends with it too, and a draft still holding exactly the
      // acknowledged words is cleared rather than offered as a duplicate.
      this.acknowledgeComposerOperation(message.idempotency_key);
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
    if (
      !this.user ||
      !this.activeId ||
      this.authorizationUnverified ||
      !this.canWriteConversation(this.activeId)
    )
      return;
    const key = crypto.randomUUID();
    const message: Message = {
      id: key,
      idempotency_key: key,
      channel_id: this.direct ? '' : this.activeId,
      conversation_id: this.activeId,
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
    this.sendEvent(
      'typing.stop',
      this.direct ? { conversation_id: this.activeId } : { channel_id: this.activeId }
    );
    // A reply sent from search/history belongs at the live end of the channel.
    if (!preservePosition) {
      if (this.viewingHistory) void this.latest();
      else this.atLatest = true;
    }
    await this.retryMessage(message);
  }

  /**
   * Sends one reply into a named thread, through the same machinery as a channel
   * message: the same operation key, the same receipt, the same outbox, the same
   * account and authority fence.
   *
   * The caller's key is supplied rather than generated, because a thread composer
   * freezes it for the life of an unresolved send. That is the whole reason this
   * exists: rotating a key after a caught failure turns a send that may well have
   * committed into a second message, which is the one outcome a recoverable send
   * exists to prevent.
   *
   * The initiating account, session and authority are captured before the destination
   * lookup — the first asynchronous step — and checked again before anything is
   * persisted or posted. A lookup that resolves after another account took the view
   * can therefore never send the first account's text as the second, and a lookup
   * that fails never falls back to the conversation that happens to be open
   * (review 15, V15-01).
   *
   * The result names what happened, because the caller must treat them differently:
   * `sent` is the stored message; `refused` provably wrote nothing, so the key is
   * still free and the composer keeps the text; `uncertain` may have committed and
   * must be reconciled under the same key; `dropped` belongs to a view that no
   * longer exists.
   */
  async sendIntoThread(
    threadID: string,
    content: string,
    idempotencyKey: string
  ): Promise<ThreadSendOutcome> {
    if (!this.user || this.authorizationUnverified || !this.can('message.send'))
      return { state: 'refused' };
    if (!threadID) return { state: 'refused' };
    const sender = this.user.id;
    const request = this.identity();
    const key = idempotencyKey;
    // A key already has a meaning. An unresolved operation's key identifies the words it
    // was submitted with, and it cannot be reused for other words: the server reconciles
    // the key against the intent it recorded, and the client must not rewrite its own
    // copy of that intent to match an edit. Edited text is new work under a new key,
    // while the original stays for its own retry or discard.
    const recorded = this.outbox.find((item) => item.key === key && !item.recovery);
    if (recorded && recorded.content !== content.trim())
      return {
        state: 'refused',
        error: new Error('This reply was already submitted with its own text.')
      };
    let thread: { conversation_id: string } | null = null;
    try {
      thread = await api<{ conversation_id: string } | null>(
        `/threads/${encodeURIComponent(threadID)}`,
        'GET'
      );
    } catch (error) {
      if (!this.admitted(request)) return { state: 'dropped' };
      // No destination, no send. The failed lookup used to fall back to the active
      // conversation, which filed a thread reply into whatever channel happened to
      // be open.
      this.showToast(errorMessage(error));
      return { state: 'refused', error };
    }
    // The account, session or authority moved while the destination was being
    // resolved. Nothing has been persisted and no key has been claimed, so dropping
    // the operation cannot send one account's text under another.
    if (!this.admitted(request)) return { state: 'dropped' };
    const conversationID = thread?.conversation_id || '';
    if (!conversationID) return { state: 'refused' };
    const message: Message = {
      id: key,
      idempotency_key: key,
      channel_id: this.isPrivate(conversationID) ? '' : conversationID,
      conversation_id: conversationID,
      thread_id: threadID,
      user_id: sender,
      content,
      attachments: [],
      reactions: [],
      embeds: [],
      created_at: new Date().toISOString(),
      pending: 'sending'
    };
    this.pending[key] = message;
    this.trackOutbox(message, []);
    // The submitted words and the key they were submitted under are associated here, at
    // the one place a thread send is claimed. A later submission of exactly these words
    // retries this operation under this key; anything else is new work.
    this.recordComposerOperation(threadID, key, message.content);
    try {
      const saved = await api<Message>(
        `/threads/${encodeURIComponent(threadID)}/messages`,
        'POST',
        {
          content: message.content,
          idempotency_key: message.idempotency_key
        }
      );
      // A completion that arrives after the account, session or authority moved
      // belongs to a view that no longer exists. The message is on the server and
      // returns with the refetched thread if this account may still read it, so
      // nothing is written into the cleared view — and the outbox entry is released
      // rather than left retryable, because the operation did complete.
      if (!this.admitted(request)) {
        this.releaseOutbox(key);
        return { state: 'dropped' };
      }
      this.acknowledgeComposerOperation(key);
      this.releaseOutbox(key);
      delete this.pending[key];
      this.onConversationEvent?.({ t: 'message.created', d: saved });
      return { state: 'sent', message: saved };
    } catch (error) {
      if (!this.admitted(request)) {
        // Same rule as the channel path: keep the author's operation retryable under
        // its own key rather than leaving it in `sending` forever, and do not apply an
        // answer that belongs to a view that has been cleared.
        if (this.pending[key]) this.pending[key] = { ...message, pending: 'failed' };
        return { state: 'dropped' };
      }
      const status = error instanceof APIError ? error.status : 0;
      if (!retryableStatus(status)) {
        // The server answered definitely and will answer the same way again, so the
        // operation is over: the text is handed back for the author to recover, and
        // the caller keeps their draft. Treating this as an ordinary failure left the
        // composer looking as if nothing had been attempted.
        this.finishUnsendable(message, outcomeExplanation(status, errorMessage(error)));
        return { state: 'refused', error };
      }
      this.showToast(errorMessage(error));
      this.pending[key] = { ...message, pending: 'failed' };
      return { state: 'uncertain', error };
    }
  }

  async retryMessage(message: Message) {
    if (!this.user || message.user_id !== this.user.id || this.authorizationUnverified) return;
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
    if (!this.canWriteConversation(this.conversationId(message))) {
      this.finishUnsendable(
        message,
        'This conversation is read-only. Your text has been kept for you to copy.'
      );
      return;
    }
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
      const saved = await api<Message>(this.messagePath(message), 'POST', {
        content: message.content,
        reply_to_id: message.reply_to_id || '',
        idempotency_key: message.idempotency_key,
        attachment_ids: this.outboxAttachments(message)
      });
      if (this.user?.id !== sender || request.session !== this.sessionVersion) return;
      if (request.generation !== this.authorizationGeneration) {
        delete this.pending[message.idempotency_key];
        this.acknowledgeComposerOperation(message.idempotency_key);
        this.releaseOutbox(message.idempotency_key);
        this.refreshWorkspace = true;
        this.scheduleChanges();
        return;
      }
      delete this.pending[message.idempotency_key];
      // The stored message is the acknowledgement: the operation is over, and a draft
      // still holding exactly its words is cleared — a slot holding anything newer is
      // not. This is the acknowledgement path for a reconciliation that no open panel
      // heard, and for a retry the author pressed on a pending row.
      this.acknowledgeComposerOperation(message.idempotency_key);
      this.releaseOutbox(message.idempotency_key);
      this.onConversationEvent?.({ t: 'message.created', d: saved });
      if (saved.reply_to_id) this.queueParentRefresh(saved.reply_to_id, this.conversationId(saved));
      if (
        this.activeId === this.conversationId(message) &&
        // A thread reply belongs to its thread's window. The channel listing does not
        // carry thread replies, so inserting one here would put it back where the
        // server deliberately stopped showing it.
        !saved.thread_id &&
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
      if (this.user?.id !== sender || request.session !== this.sessionVersion) return;
      if (!this.admitted(request)) {
        // A contact/role refresh can overtake an uncertain send. Keep the author's
        // operation retryable under its original key instead of leaving Sending forever;
        // do not apply an old server error or resurrect cleared conversation content.
        if (this.pending[message.idempotency_key]) {
          this.pending[message.idempotency_key] = { ...message, pending: 'failed' };
          this.messages = this.messages.map((item) =>
            item.pending && item.idempotency_key === message.idempotency_key
              ? { ...item, pending: 'failed' }
              : item
          );
        }
        return;
      }
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
    if (!this.canWriteConversation(this.activeId)) return;
    this.sendEvent(
      'typing.start',
      this.direct ? { conversation_id: this.activeId } : { channel_id: this.activeId }
    );
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
    this.conversationEvents = [...this.conversationEvents, event].slice(-50);
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
      // A notification is for a conversation the member is *not* looking at. Checking
      // only `document.hidden` covered the tab being in the background and nothing
      // else, so a member reading another channel — or reading back through this one —
      // got an operating system banner for a message they could see arrive. The same
      // question that decides whether the read cursor moves decides this: a member
      // watching the live tail already sees the toast above and needs no banner, and a
      // member scrolled away or reading history still gets one.
      //
      // `conversationId` matters as much as the view state: the mention may name a
      // conversation other than the open one, and a member reading *this* channel is
      // watching that conversation even when the mention is in another.
      const watching = watchingNow({
        visible: !document.hidden,
        focused: document.hasFocus(),
        atLatest: this.atLatest,
        viewingHistory: this.viewingHistory
      });
      const viewingThisOne = this.activeId === this.conversationId(event.d);
      if (watching && viewingThisOne) {
        // The toast already said it, in the conversation the member is in.
      } else if ('Notification' in window && Notification.permission === 'granted') {
        const request = this.identity();
        navigator.serviceWorker?.ready
          .then((registration) => {
            if (
              !this.admitted(request) ||
              !this.channelNotificationsReady ||
              this.channelNotificationMutes[event.d.channel_id]
            )
              return;
            const notice = {
              title: event.d.title,
              body: event.d.body,
              tag: event.d.message_id,
              url: event.d.url
            };
            // The worker owns admission for both paths, so a page notice and a push
            // for the same event claim one key and only one of them shows. Without a
            // live worker there is no shared claim to make, so the notice is shown
            // directly rather than dropped.
            const worker = registration.active;
            if (worker && typeof worker.postMessage === 'function') {
              try {
                worker.postMessage({ type: 'BRRP_NOTIFY', notification: notice });
                return;
              } catch {
                // A worker that cannot be reached falls through to the local notice.
              }
            }
            return registration.showNotification(notice.title, {
              body: notice.body,
              tag: notice.tag,
              data: { url: notice.url }
            });
          })
          .catch(() => {});
      }
    }
    if (event.t.startsWith('message.') || event.t === 'reaction.updated') {
      const { id } = event.d;
      const channel = this.conversationId(event.d);
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
    } else if (
      event.t.startsWith('direct.') ||
      event.t.startsWith('contact.') ||
      event.t.startsWith('conversation.')
    ) {
      this.contactsVersion++;
      this.invalidateAuthorization();
    } else if (event.t === 'unread.updated') {
      this.refreshUnread = true;
      this.scheduleChanges();
    } else if (event.t === 'hello' || event.t === 'reconnect.required') {
      // Reload after reconnect to repair events missed while the socket was down.
      // The mention policy is re-read too, not just the workspace: a mute written
      // on another device while this socket was down has no event left to carry
      // it, and a same-revision reconnect produces none either (review 9, R9-02).
      this.refreshWorkspace = true;
      this.requestPreferenceRefresh();
      this.scheduleChanges(true);
    } else if (event.t === 'preferences.changed') {
      // Another device (or another tab) changed account-owned policy this session
      // caches. The event carries no authority, so it is only a reason to re-read
      // the account's own preferences; what the new answer says is decided by the
      // server. The cached map is dropped first so a session that was filtering
      // mentions on the old answer stops doing so before the read lands, and the
      // version bump refuses a read that was already in flight.
      if (event.d?.channel_notifications) this.requestPreferenceRefresh();
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
    if (version === this.unreadVersion && this.admitted(request) && this.state) {
      this.state.unread = unread;
    }
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
      // A preference re-read is issued where the request was made, not here: the
      // policy is not part of the workspace snapshot, and folding it into this flush
      // would delay it behind whatever else the flush is already waiting on.
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
    // A thread reply does not belong in a channel's view. The server's page excludes
    // one, and the page filter in loadMessages excludes one, but this path is a
    // different one: it fetches a single message by id in response to a live event and
    // has never passed through either. It is the one place every live event ends up,
    // so the check belongs here — and filtering in the two other places left this
    // route to put a thread reply back into the channel it was branched off from.
    //
    // A null message (a deletion) is still applied: content that has been removed
    // must be able to disappear from anywhere it was shown.
    if (message?.thread_id && this.channel?.id === channel) return;
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
        `${this.conversationPath(channel, 'first-unread')}?after=${encodeURIComponent(after)}`
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
        // Saving drafts was re-enabled deliberately, so a late cleanup from before is
        // no longer something to hold back.
        this.draftsClearedFor = '';
        writeDrafts(storage, this.user.id, this.drafts);
        this.persistOutbox();
        this.persistComposerOperations();
      } else {
        storage.removeItem(`brrp.drafts.${this.user.id}`);
        clearOutbox(storage, this.user.id);
        clearOperations(storage, this.user.id);
        // The device scope was just cleared on purpose; a composer that unmounts now
        // must not write the text back into it.
        this.draftsClearedFor = this.user.id;
      }
    }
  }

  saveDraft(channel: string, text: string) {
    this.drafts[channel] = text;
    if (this.saveDrafts && this.user) writeDrafts(storage, this.user.id, this.drafts);
  }

  // composerOwner is the identity a composer belongs to. A component captures it when
  // it mounts and presents it when it writes, so a cleanup, effect or completion that
  // runs after a sign-out or an account switch is recognized as belonging to a session
  // that no longer exists.
  composerOwner(): RequestIdentity {
    return this.identity();
  }

  // saveComposerDraft records one mounted composer's unsent draft under the identity it
  // was typed in. While that identity is still the session's, the draft goes into the
  // shared store exactly as a keystroke would. After it has moved — a stop, a sign-out, a
  // new account — nothing is written into the live store: the text goes into the account
  // it belongs to instead, and only when that account had draft saving on. One account's
  // unsent words are never handed to the next.
  saveComposerDraft(slot: string, text: string, attachments: Attachment[], owner: RequestIdentity) {
    if (!text.trim() && !attachments.length) return;
    if (this.admitted(owner)) {
      this.composerDrafts.set(slot, { text, attachments });
      this.saveDraft(slot, text);
      return;
    }
    if (!owner.account || owner.account === this.draftsClearedFor) return;
    if (storage.getItem(`brrp.save-drafts.${owner.account}`) !== 'true') return;
    const saved = readDrafts(storage, owner.account);
    saved[slot] = text.slice(0, 4000);
    writeDrafts(storage, owner.account, saved);
  }

  // clearComposerDraft removes one composer slot. Like writes, a cleanup for a session
  // that has moved removes nothing: the slot in the live store now belongs to whoever
  // is signed in.
  clearComposerDraft(slot: string, owner: RequestIdentity) {
    if (!this.admitted(owner)) return;
    this.composerDrafts.delete(slot);
    delete this.drafts[slot];
    if (this.saveDrafts && this.user) writeDrafts(storage, this.user.id, this.drafts);
  }

  // clearSentDraft clears one composer slot after the text it held has been committed.
  // Only the text that was actually sent is cleared: anything newer — typed while the
  // request was in flight, or restored into the slot since — is the author's current
  // work, and clearing it would be deleting a draft nobody sent.
  clearSentDraft(slot: string, sent: string, owner: RequestIdentity) {
    if (!this.admitted(owner)) return;
    const live = this.composerDrafts.get(slot)?.text ?? this.drafts[slot] ?? '';
    if (live.trim() && live.trim() !== sent) return;
    this.clearComposerDraft(slot, owner);
  }

  /**
   * The stable address of one message, and what to do when somebody opens it.
   *
   * A permalink has to resolve under *current* authority rather than carrying the
   * permissions the link was created with, so following one uses the same
   * context-resolution path as clicking a reply reference. A link to a message in a
   * conversation the opener can no longer read lands somewhere that refuses, which is
   * the honest answer — and the failure mode this avoids is a link that opens
   * somebody else's history on a shared machine.
   */
  messagePermalink(id: string) {
    return `${location.origin}/#message=${encodeURIComponent(id)}`;
  }

  async openPermalink(hash: string) {
    const match = /^#message=([^&]+)/.exec(hash);
    if (!match) return false;
    await this.jumpToMessage(decodeURIComponent(match[1]));
    // The hash is cleared so a reload does not yank somebody back to a message they
    // have since scrolled away from.
    history.replaceState(null, '', location.pathname + location.search);
    return true;
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
      if (this.conversationId(target) !== this.activeId) this.rememberChannel();
      this.activeId = this.conversationId(target);
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
      history.replaceState(null, '', this.conversationURL(this.conversationId(target)));
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
      this.onConversationEvent?.({
        t: 'message.deleted',
        d: { id, channel_id: this.channel?.id || '', conversation_id: channel }
      });
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
        clearOperations(storage, this.user.id);
        // A sign-out clears this account's drafts and unconfirmed sends from the
        // device; a composer unmounting afterwards must not put them back.
        this.draftsClearedFor = this.user.id;
      }
      this.outbox = [];
      this.drafts = {};
      this.composerOperations = new Map();
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
    this.messagesOpen = false;
    this.contactsVersion++;
    this.messages = [];
    this.replyTo = null;
    this.typing = {};
    this.stopped = true;
    this.pending = {};
    this.outbox = [];
    this.outboxAccount = '';
    this.composerOperations = new Map();
    this.operationsAccount = '';
    this.readCursors = {};
    this.channelNotificationMutes = {};
    this.channelNotificationsReady = false;
    this.channelNotificationError = '';
    ++this.notificationsLoadVersion;
    this.notificationRetry = 0;
    this.saveDrafts = false;
    this.views.clear();
    this.snapshots.clear();
    this.snapshotChanges.clear();
    this.messageChanges.clear();
    this.messageRevisions.clear();
    this.refreshWorkspace = this.refreshUnread = this.reconnectSync = this.flushing = false;
    // A preference refresh belongs to the session that asked for it. Carrying it
    // into the next account would make the first flush after a switch fetch the
    // new account's policy for a reason the previous account created.
    this.preferencesRefreshPending = false;
    this.preferencesFocusedAt = 0;
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
    this.namedThreadId = '';
    this.conversationEvents = [];
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
