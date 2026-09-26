<script lang="ts">
  import { tick, onDestroy } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { canGroup, firstUnread } from '../conversations';
  import MessageItem from './MessageItem.svelte';
  import Icon from './Icon.svelte';
  let scroller: HTMLDivElement;
  let sticky = $state(true);
  let lastChannel = '';
  let restoring = false;
  let scrollFrame = 0;
  onDestroy(() => cancelAnimationFrame(scrollFrame));
  function recordPosition() {
    cancelAnimationFrame(scrollFrame);
    scrollFrame = requestAnimationFrame(() => {
      if (!scroller || restoring) return;
      const bounds = scroller.getBoundingClientRect();
      const first = Array.from(scroller.querySelectorAll<HTMLElement>('.message')).find(
        (message) => message.getBoundingClientRect().bottom > bounds.top
      );
      workspace.recordScroll(lastChannel, {
        id: first?.id.slice(8) || '',
        offset: first ? first.getBoundingClientRect().top - bounds.top : 0,
        top: scroller.scrollTop
      });
    });
  }
  const unreadID = $derived(
    firstUnread(workspace.messages, workspace.unreadAfter, workspace.user?.id)
  );
  $effect(() => {
    const count = workspace.messages.length;
    const channel = workspace.activeId;
    const highlight = workspace.highlightId;
    const atLatest = workspace.atLatest;
    const restore = workspace.scrollRestore;
    lastChannel = channel;
    if (restore) {
      restoring = true;
      tick().then(() => {
        if (!scroller || workspace.activeId !== channel) {
          restoring = false;
          return;
        }
        const anchor = document.getElementById('message-' + restore.id);
        scroller.scrollTop = anchor
          ? scroller.scrollTop +
            anchor.getBoundingClientRect().top -
            scroller.getBoundingClientRect().top -
            restore.offset
          : restore.top;
        sticky = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
        workspace.scrollRestore = null;
        requestAnimationFrame(() => {
          restoring = false;
          recordPosition();
        });
      });
    } else if (highlight) {
      tick().then(() =>
        document.getElementById('message-' + highlight)?.scrollIntoView({ block: 'center' })
      );
    } else if (atLatest && count) {
      tick().then(() => {
        if (scroller) scroller.scrollTop = scroller.scrollHeight;
      });
    }
  });
  async function loadOlder() {
    if (workspace.olderLoading || workspace.messagesLoading) return;
    const channel = workspace.activeId;
    const height = scroller.scrollHeight;
    const top = scroller.scrollTop;
    workspace.atLatest = false;
    workspace.highlightId = '';
    await workspace.loadMessages(true);
    await tick();
    if (channel === workspace.activeId) scroller.scrollTop = top + scroller.scrollHeight - height;
  }
  async function latest() {
    if (!(await workspace.latest())) return;
    await tick();
    scroller.scrollTop = scroller.scrollHeight;
  }
</script>

<div class="messages-region">
  {#if workspace.unreadAfter !== null && !workspace.viewingHistory}<div class="unread-navigation">
      <span>Pick up where you left off</span>
      <button onclick={() => workspace.jumpToFirstUnread()} disabled={workspace.jumpingUnread}
        >{workspace.jumpingUnread ? 'Finding your place…' : 'Jump to first unread'}</button
      >
      <button
        class="icon-button"
        aria-label="Dismiss unread shortcut"
        onclick={() => (workspace.unreadAfter = null)}><Icon name="close" size={16} /></button
      >
    </div>{/if}
  <div
    class="message-list"
    bind:this={scroller}
    onscroll={() => {
      if (restoring || lastChannel !== workspace.activeId) return;
      sticky = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
      workspace.atLatest = sticky && !workspace.viewingHistory;
      recordPosition();
      if (workspace.atLatest) void workspace.markRead();
    }}
    role="log"
    aria-label={workspace.direct ? 'Direct messages' : 'Channel messages'}
    aria-live="polite"
    aria-relevant="additions"
    aria-busy={workspace.messagesLoading || workspace.olderLoading}
  >
    {#if workspace.hasOlder}<button
        class="load-older"
        onclick={loadOlder}
        disabled={workspace.olderLoading}
        >{workspace.olderLoading ? 'Loading earlier messages…' : 'Load earlier messages'}</button
      >{/if}
    {#if workspace.messagesError}<div class="message-load-error" role="alert">
        <Icon name="connection" size={22} />
        <div>
          <strong>Messages couldn’t load</strong>
          <p>{workspace.messagesError}</p>
        </div>
        <button
          class="secondary"
          onclick={() => (workspace.messagesErrorOlder ? loadOlder() : latest())}>Try again</button
        >
      </div>{/if}
    {#if workspace.messagesLoading}<div class="loading-state">Loading messages…</div>
    {:else}
      {#if !workspace.hasOlder && !workspace.viewingHistory && !workspace.messagesError}<div
          class="channel-welcome"
        >
          <div class="channel-symbol">
            <Icon name={workspace.direct ? 'users' : 'hash'} size={35} />
          </div>
          <h1>{workspace.conversationName}</h1>
          <p>{workspace.channel?.topic || 'This is the beginning of the conversation.'}</p>
          <span class="channel-start"
            >{workspace.messages.length ? 'THE CONVERSATION' : 'A FRESH START'}</span
          >
        </div>{/if}
      {#each workspace.messages as message, index (message.id)}
        {#if index > 0 && new Date(message.created_at).toDateString() !== new Date(workspace.messages[index - 1].created_at).toDateString()}<div
            class="date-divider"
          >
            <span
              >{new Date(message.created_at).toLocaleDateString([], {
                month: 'long',
                day: 'numeric',
                year: 'numeric'
              })}</span
            >
          </div>{/if}
        {#if message.id === unreadID}<div class="unread-divider">
            <span>New messages</span>
          </div>{/if}
        <MessageItem
          {message}
          grouped={message.id !== unreadID && canGroup(message, workspace.messages[index - 1])}
        />
      {/each}
    {/if}
  </div>
  {#if !sticky || workspace.viewingHistory}<button class="jump-latest" onclick={latest}
      >Jump to latest <Icon name="arrow" size={16} /></button
    >{/if}
</div>
