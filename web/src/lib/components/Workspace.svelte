<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import ChannelOrder from './ChannelOrder.svelte';
  import Avatar from './Avatar.svelte';
  import Icon from './Icon.svelte';
  import Composer from './Composer.svelte';
  import RecoveredMessages from './RecoveredMessages.svelte';
  import ChannelNotifications from './ChannelNotifications.svelte';
  import MessagesInbox from './MessagesInbox.svelte';
  import DirectControls from './DirectControls.svelte';
  import MessageList from './MessageList.svelte';
  import SearchPanel from './SearchPanel.svelte';
  import PinsPanel from './PinsPanel.svelte';
  import ThreadPanel from './ThreadPanel.svelte';
  import NamedThreadPanel from './NamedThreadPanel.svelte';
  import ThreadListPanel from './ThreadListPanel.svelte';
  import ChannelSwitcher from './ChannelSwitcher.svelte';

  import LazyPanel from './LazyPanel.svelte';
  import { drawerFocus } from '../drawer-focus';
  import { voice } from '../voice.svelte';
  import VoicePanel from './VoicePanel.svelte';
  import ExportPanel from './ExportPanel.svelte';
  import VoiceControls from './VoiceControls.svelte';
  let narrow = $state(window.innerWidth <= 640);
  let overlayMembers = $state(window.innerWidth <= 900);
  let channelDrawer: HTMLElement;
  let memberDrawer = $state<HTMLElement>();
  let menuButton: HTMLButtonElement;
  const searchShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K';
  $effect(() => {
    if (narrow && workspace.sidebarOpen && channelDrawer)
      return drawerFocus(channelDrawer, () => (workspace.sidebarOpen = false), menuButton);
  });
  $effect(() => {
    if (overlayMembers && workspace.membersOpen && memberDrawer)
      return drawerFocus(
        memberDrawer,
        () => workspace.setMembersOpen(false),
        document.querySelector<HTMLButtonElement>('#members-toggle')
      );
  });
  let clock = $state(Date.now());
  let members = $derived(
    workspace.direct
      ? [workspace.user!, workspace.direct.peer]
      : workspace.state?.users.filter((user) => !user.is_banned && !user.is_deactivated) || []
  );
  async function toggleMembers(open: boolean) {
    if (open) workspace.sidebarOpen = false;
    workspace.setMembersOpen(open);
    await tick();
    const selector = open
      ? window.innerWidth > 900
        ? '.members-collapse'
        : '.members-close'
      : '#members-toggle';
    document.querySelector<HTMLButtonElement>(selector)?.focus();
  }
  // Grouping and ordering come from the store, which follows the durable category
  // positions the server returns rather than the insertion order of the channels, so
  // a category reordering survives a reload.
  let categories = $derived(workspace.textGroups);
  let voiceCategories = $derived(workspace.voiceGroups);
  let typingNames = $derived(
    Object.entries(workspace.typing)
      .filter(
        ([id, state]) =>
          id !== workspace.user?.id &&
          workspace.conversationId(state) === workspace.activeId &&
          state.expires_at > clock
      )
      .map(
        ([id]) => workspace.state?.users.find((user) => user.id === id)?.display_name || 'Someone'
      )
  );
  let online = $derived(members.filter((user) => workspace.state?.presence[user.id]));
  let offline = $derived(members.filter((user) => !workspace.state?.presence[user.id]));
  onMount(() => {
    const phone = matchMedia('(max-width: 640px)');
    const tablet = matchMedia('(max-width: 900px)');
    const resize = () => {
      narrow = phone.matches;
      overlayMembers = tablet.matches;
      workspace.sidebarOpen = false;
      if (tablet.matches) workspace.setMembersOpen(false);
    };
    phone.addEventListener('change', resize);
    tablet.addEventListener('change', resize);
    const timer = setInterval(() => (clock = Date.now()), 1000);
    return () => {
      clearInterval(timer);
      phone.removeEventListener('change', resize);
      tablet.removeEventListener('change', resize);
    };
  });
</script>

<div class="workspace" class:members-visible={workspace.membersOpen}>
  {#if workspace.sidebarOpen}<button
      class="sidebar-scrim"
      aria-label="Close channels"
      onclick={() => (workspace.sidebarOpen = false)}
    ></button>{/if}
  <aside
    bind:this={channelDrawer}
    id="channels-panel"
    class="channel-sidebar"
    class:open={workspace.sidebarOpen}
    aria-label="Channels"
    inert={(narrow && !workspace.sidebarOpen) || (overlayMembers && workspace.membersOpen)}
  >
    <div class="server-header">
      <button
        class="icon-button mobile-menu"
        aria-label="Close channels"
        onclick={() => (workspace.sidebarOpen = false)}><Icon name="close" /></button
      >
      <span class="brand-mark small">b.</span>
      <div>
        <strong title={workspace.state?.settings.server_name}
          >{workspace.state?.settings.server_name}</strong
        ><small>Your community</small>
      </div>
      <!-- The capability decides whether this button exists, and the panel itself is
           mounted on demand: an account whose last management capability is withdrawn
           while the panel is open keeps the panel open and is told why it is empty
           instead of watching it disappear (review R2-06). -->
      {#if workspace.canManageAnything}<button
          class="icon-button"
          aria-label="Manage server"
          title="Manage server"
          onclick={() => (workspace.adminOpen = true)}><Icon name="settings" size={18} /></button
        >{/if}
    </div>
    <button class="sidebar-search" onclick={() => (workspace.switcherOpen = true)}
      ><Icon name="search" size={16} /><span>Jump to a channel</span><kbd>{searchShortcut}</kbd
      ></button
    >
    {#if workspace.privateConversationsEnabled}
      {@const outstanding = workspace.outstandingIncomingRequests}
      <button
        class="sidebar-search"
        aria-label={outstanding > 0
          ? `Messages and requests, ${outstanding} waiting for you`
          : 'Messages and requests'}
        onclick={() => {
          // The badge is a way into Requests, so a pending count leads straight
          // there. With nothing outstanding the entry opens the conversation list
          // as it always did: a member keeping up with messages should not be
          // dropped into a requests section they did not ask for (ADR 0013).
          workspace.openMessagesTab(outstanding > 0 ? 'requests' : 'conversations');
        }}
        ><Icon name="users" size={16} /><span>Messages and requests</span>{#if outstanding > 0}<b
            class="unread-badge"
            aria-hidden="true">{outstanding}</b
          >{/if}</button
      >
      {#if outstanding > 0}<span class="visually-hidden" role="status"
          >{outstanding} message {outstanding === 1 ? 'request is' : 'requests are'} waiting for you.</span
        >{/if}
    {/if}
    <nav class="channel-navigation">
      {#if workspace.privateConversationsEnabled}<div class="channel-group">
          <h2>DIRECT MESSAGES</h2>
          {#each workspace.directConversations.filter((direct) => !direct.hidden) as direct (direct.id)}
            {@const unread = workspace.unreadFor(direct.id)}
            <button
              class="channel-link"
              class:active={workspace.activeId === direct.id}
              class:unread={!!unread?.count}
              aria-current={workspace.activeId === direct.id ? 'page' : undefined}
              onclick={() => workspace.selectChannel(direct.id)}
            >
              <Avatar user={direct.peer} size={22} /><span
                >{direct.peer.display_name || direct.peer.username}</span
              >{#if !direct.can_write}<small>Read-only</small>{/if}{#if unread?.count}<i
                  class="unread-dot"
                ></i>{/if}
            </button>
          {:else}<button class="channel-link" onclick={() => (workspace.messagesOpen = true)}
              ><Icon name="plus" /><span>Start a conversation</span></button
            >{/each}
          <!-- Groups are the other kind of private conversation and are listed
               beside the direct ones, because a member opens them the same way. -->
          {#if workspace.groups.length}
            <h2>GROUPS</h2>
            {#each workspace.groups as group (group.id)}
              {@const unread = workspace.unreadFor(group.id)}
              <button
                class="channel-link"
                class:active={workspace.activeId === group.id}
                class:unread={!!unread?.count}
                aria-current={workspace.activeId === group.id ? 'page' : undefined}
                onclick={() => workspace.selectChannel(group.id)}
              >
                <span>{group.name}</span>{#if !group.can_write}<small>Read-only</small
                  >{/if}{#if unread?.count}<i class="unread-dot"></i>{/if}
              </button>
            {/each}
          {/if}
        </div>{/if}
      {#each categories as category}<div class="channel-group">
          <h2>{category.name || 'TEXT CHANNELS'}</h2>
          {#each category.channels as channel (channel.id)}{@const unread = workspace.unreadFor(
              channel.id
            )}<ChannelOrder {channel}
              ><button
                class="channel-link"
                class:active={workspace.activeId === channel.id}
                class:unread={!!unread?.count}
                aria-current={workspace.activeId === channel.id ? 'page' : undefined}
                onclick={() => workspace.selectChannel(channel.id)}
                ><Icon name="hash" /><span>{channel.name}</span
                >{#if channel.visibility === 'private'}<i
                    class="channel-flag"
                    title="Private channel — only the roles and accounts listed in its access panel can read it."
                    ><Icon name="shield" size={13} /></i
                  >{/if}{#if channel.posting === 'announcement'}<i
                    class="channel-flag"
                    title="Announcements — only accounts that can manage channels may post."
                    ><Icon name="bell" size={13} /></i
                  >{/if}{#if unread?.mentions}<b class="unread-badge">{unread.mentions}</b
                  >{:else if unread?.count}<i class="unread-dot"></i>{/if}</button
              ></ChannelOrder
            >{/each}
        </div>{/each}
      {#if workspace.state?.settings.enable_voice}{#each voiceCategories as category}<div
            class="channel-group"
          >
            <h2>{category.name || 'VOICE CHANNELS'}</h2>
            {#each category.channels as channel (channel.id)}{@const participants =
                workspace.state.voice_states.filter(
                  (state) => state.channel_id === channel.id
                )}<ChannelOrder {channel}
                ><button
                  class="channel-link voice-link"
                  class:active={voice.channelId === channel.id}
                  aria-current={voice.channelId === channel.id ? 'true' : undefined}
                  disabled={voice.status === 'requesting' ||
                    voice.status === 'connecting' ||
                    !!voice.movingTo}
                  onclick={() => voice.selectChannel(channel.id)}
                  ><Icon name="volume" /><span>{channel.name}</span
                  >{#if channel.visibility === 'private'}<i
                      class="channel-flag"
                      title="Private channel — only the roles and accounts listed in its access panel can read it."
                      ><Icon name="shield" size={13} /></i
                    >{/if}<small>{participants.length}/{channel.max_voice_users}</small></button
                ></ChannelOrder
              >{#each participants as participant (participant.user_id)}{@const user =
                  workspace.state?.users.find((user) => user.id === participant.user_id)}
                <div class="voice-member" class:speaking={participant.speaking}>
                  <Avatar {user} size={22} /><span>{user?.display_name || user?.username}</span
                  >{#if participant.muted || participant.admin_muted}<Icon
                      name="micOff"
                      size={14}
                    />{/if}
                </div>{/each}{/each}
          </div>{/each}{/if}
    </nav>
    <VoiceControls />
    <div class="sidebar-bottom">
      <span class="connection-dot" class:connected={workspace.connection === 'connected'}
      ></span><span
        >{workspace.connection === 'connected'
          ? 'Connected'
          : workspace.connection === 'connecting'
            ? 'Connecting…'
            : 'Reconnecting…'}</span
      ><span class="version-label">BrrpBrrp</span>
    </div>
    <div class="user-footer">
      <Avatar user={workspace.user} size={32} />
      <div>
        <strong>{workspace.user?.display_name || workspace.user?.username}</strong><small
          >@{workspace.user?.username}</small
        >
      </div>
      <button
        class="icon-button"
        aria-label="Your data export"
        title="Your data export"
        onclick={() => (workspace.exportOpen = true)}><Icon name="download" size={18} /></button
      >
      <button
        class="icon-button"
        aria-label="Your preferences"
        onclick={() => (workspace.settingsOpen = true)}><Icon name="settings" size={18} /></button
      ><button class="icon-button" aria-label="Sign out" onclick={() => workspace.logout()}
        ><Icon name="leave" size={18} /></button
      >
    </div>
  </aside>
  <main
    class="conversation"
    inert={(narrow && workspace.sidebarOpen) || (overlayMembers && workspace.membersOpen)}
  >
    <header class="channel-header">
      <button
        bind:this={menuButton}
        class="icon-button mobile-menu"
        aria-expanded={workspace.sidebarOpen}
        aria-controls="channels-panel"
        aria-label="Open channels"
        onclick={() => {
          workspace.setMembersOpen(false);
          workspace.sidebarOpen = true;
        }}><Icon name="menu" /></button
      ><Icon name={workspace.direct ? 'users' : 'hash'} size={25} /><strong
        >{workspace.activeId ? workspace.conversationName : 'Welcome'}</strong
      ><span class="header-divider"></span>
      <p>
        {workspace.direct
          ? 'Direct conversation · only accepted participants'
          : workspace.channel?.topic}
      </p>
      <div class="header-actions">
        {#if workspace.direct}<DirectControls />{:else}<ChannelNotifications />{/if}
        {#if workspace.channel}<button
            class="icon-button"
            aria-label="Threads in this channel"
            title="Threads in this channel"
            disabled={!workspace.activeId}
            onclick={() => (workspace.threadsOpen = true)}><Icon name="hash" /></button
          >{/if}
        <button
          class="icon-button"
          aria-label="Pinned messages"
          title="Pinned messages"
          disabled={!workspace.activeId}
          onclick={() => (workspace.pinsOpen = true)}><Icon name="pin" /></button
        >
        <button
          class="icon-button"
          aria-label="Search messages"
          onclick={() => (workspace.searchOpen = true)}><Icon name="search" /></button
        ><button
          id="members-toggle"
          class="icon-button members-header-toggle"
          class:collapsed={!workspace.membersOpen}
          class:selected={workspace.membersOpen}
          aria-label={workspace.membersOpen ? 'Hide members' : 'Show members'}
          title={workspace.membersOpen ? 'Hide members' : 'Show members'}
          aria-expanded={workspace.membersOpen}
          aria-controls="members-panel"
          onclick={() => toggleMembers(!workspace.membersOpen)}><Icon name="users" /></button
        >
      </div>
    </header>
    <div class="mobile-voice"><VoiceControls /></div>
    {#if workspace.connection !== 'connected'}<div class="connection-banner" role="status">
        <Icon name="connection" size={18} /><span
          ><strong>Reconnecting to your server…</strong> You can keep writing while we reconnect.</span
        >
      </div>{/if}
    <RecoveredMessages />
    {#if workspace.activeId}<MessageList />
      <div class="typing-line" aria-live="polite">
        {#if typingNames.length}<span class="typing-dots">•••</span><span
            ><strong>{typingNames.slice(0, 3).join(', ')}</strong>
            {typingNames.length === 1 ? 'is' : 'are'} typing</span
          >{/if}
      </div>
      <Composer />{:else}<div class="empty-state">
        <Icon name="hash" size={40} />
        <h1>Room for a conversation.</h1>
        <p>
          {workspace.can('channel.manage')
            ? 'Create a text channel to bring everyone together.'
            : 'Your administrator can create a text channel to get started.'}
        </p>
        {#if workspace.can('channel.manage')}<button
            class="primary"
            onclick={() => {
              workspace.adminTab = 'Channels';
              workspace.adminOpen = true;
            }}>Create a channel</button
          >{/if}
      </div>{/if}
  </main>
  {#if workspace.membersOpen}<button
      class="members-scrim"
      aria-label="Close members"
      onclick={() => toggleMembers(false)}
    ></button>
    <aside bind:this={memberDrawer} id="members-panel" class="member-sidebar" aria-label="Members">
      <header>
        <h2>MEMBERS</h2>
        <span>{members.length}</span>
        <button
          class="icon-button members-collapse"
          aria-label="Hide members"
          title="Hide members"
          aria-expanded="true"
          aria-controls="members-panel"
          onclick={() => toggleMembers(false)}><Icon name="chevronRight" size={18} /></button
        >
        <button
          class="icon-button members-close"
          aria-label="Close members"
          onclick={() => toggleMembers(false)}><Icon name="close" /></button
        >
      </header>
      {#each workspace.state?.settings.enable_presence ? [{ label: 'ONLINE', users: online }, { label: 'OFFLINE', users: offline }] : [{ label: 'MEMBERS', users: members }] as group}<h3
        >
          {group.label} <span>— {group.users.length}</span>
        </h3>
        {#each group.users as user (user.id)}<div
            class="member"
            class:offline={workspace.state?.settings.enable_presence &&
              !workspace.state.presence[user.id]}
          >
            <div class="avatar-wrap">
              <Avatar {user} size={32} />{#if workspace.state?.settings.enable_presence}<i
                  class:online={workspace.state.presence[user.id] === 'online'}
                  class:idle={workspace.state.presence[user.id] === 'idle'}
                ></i>{/if}
            </div>
            <div>
              <strong>{user.display_name || user.username}</strong><small
                >{user.is_admin ? 'Administrator' : 'Member'}</small
              >
            </div>
            {#if user.is_admin}<Icon name="shield" size={14} />{/if}
          </div>{/each}{/each}
      <div class="members-footnote">
        <span class="tiny-mark">b.</span>Small circle. Good company.
      </div>
    </aside>{/if}
</div>
{#if workspace.messagesOpen && workspace.privateConversationsEnabled}<MessagesInbox />{/if}
{#if workspace.searchOpen}<SearchPanel />{/if}
{#if workspace.pinsOpen}<PinsPanel />{/if}
{#if workspace.switcherOpen}<ChannelSwitcher />{/if}
{#if workspace.threadId}{#key workspace.threadId}<ThreadPanel id={workspace.threadId} />{/key}{/if}
{#if workspace.threadsOpen}<ThreadListPanel />{/if}
{#if workspace.namedThreadId}{#key workspace.namedThreadId}<NamedThreadPanel
      id={workspace.namedThreadId}
    />{/key}{/if}
{#if workspace.settingsOpen}<LazyPanel
    title="Your preferences"
    close={() => (workspace.settingsOpen = false)}
    load={() => import('./Preferences.svelte')}
  />{/if}
{#if workspace.adminOpen}<LazyPanel
    title="Server tools"
    close={() => (workspace.adminOpen = false)}
    load={() => import('./AdminPanel.svelte')}
  />{/if}
{#if workspace.exportOpen}<ExportPanel />{/if}
{#if voice.panelOpen}<VoicePanel />{/if}
