<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { ThreadView } from '../threads.svelte';
  import { threadSubmitState, canSendThreadReply } from '../thread-state';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';
  import MessageItem from './MessageItem.svelte';
  import Icon from './Icon.svelte';

  let { id }: { id: string } = $props();
  // One view per open thread, keyed by the workspace, so opening a second thread does
  // not cancel the first one's page and switching back does not refetch what is held.
  const view = new ThreadView(untrack(() => id));
  let log = $state<HTMLDivElement>();
  let atLatest = $state(true);
  // The thread's own draft slot in the shared store, keyed by thread id. A panel's
  // local state dies with the panel, so closing a thread to look at a channel and
  // coming back used to lose an unsent reply. The store is already account-scoped and
  // already survives a reload when drafts are saved, so a thread reply is now held
  // the same way a channel message is.
  const draftSlot = $derived(`thread:${id}`);
  let draft = $state('');
  // The send key is frozen for the life of an unresolved send. It is only replaced
  // after the server has said what happened, so a lost response is reconciled rather
  // than retried as new work.
  let operationKey = $state(crypto.randomUUID());
  let reason = $state('');
  let stateError = $state('');
  let focusTarget: HTMLTextAreaElement | null = $state(null);

  // Restored when the panel opens, and captured when it closes or unmounts, so the
  // text is in the store for as long as it is unsent.
  let capturedSlot = '';
  $effect(() => {
    // The slot follows the prop rather than being fixed at mount, so a panel reused
    // for a different thread reads that thread's draft instead of the first one's.
    if (draftSlot === capturedSlot) return;
    capturedSlot = draftSlot;
    draft = workspace.composerDrafts.get(draftSlot)?.text ?? workspace.drafts[draftSlot] ?? '';
  });
  function captureDraft() {
    if (!draft.trim()) return;
    workspace.composerDrafts.set(draftSlot, { text: draft, attachments: [] });
    workspace.saveDraft(draftSlot, draft);
  }

  const locked = $derived(view.thread?.state === 'locked');
  const sendState = $derived(threadSubmitState(view.sending, view.sendError));
  const canSend = $derived(
    canSendThreadReply({
      open: !!view.thread,
      locked,
      missing: view.missing,
      sending: view.sending,
      draft
    })
  );

  // The thread's own window, not the channel's: opening a thread does not mark the
  // channel read, and this view never touches the channel's messages or cursor.
  const pending = $derived(
    workspace
      .pendingReplies(id)
      .filter(
        (message) =>
          !view.messages.some(
            (saved) =>
              saved.idempotency_key === message.idempotency_key && saved.user_id === message.user_id
          )
      )
  );

  async function focusComposer() {
    await tick();
    focusTarget?.focus();
  }

  async function send() {
    if (!canSend) return;
    const key = operationKey;
    const text = draft;
    view.sendError = null;
    // The text is cleared *after* the outcome, not before. Clearing it first meant a
    // refusal and a lost response both left an empty composer, and the key rotation
    // below then made the lost response a second message on retry.
    await view.send(text, key);
    // Only a committed send clears the composer. Everything else keeps the text, so
    // the author can copy it, and keeps the key, so a retry reconciles against the
    // operation's receipt instead of becoming new work.
    if (view.sendError) {
      draft = text;
      return;
    }
    draft = '';
    operationKey = crypto.randomUUID();
  }

  async function older() {
    const container = log;
    const anchor = container?.querySelector<HTMLElement>('[data-thread-message]');
    const anchorId = anchor?.dataset.threadMessage;
    const offset = anchor?.getBoundingClientRect().top;
    atLatest = false;
    await view.older();
    await tick();
    if (!container) return;
    const restored = Array.from(
      container.querySelectorAll<HTMLElement>('[data-thread-message]')
    ).find((element) => element.dataset.threadMessage === anchorId);
    // The reader's position is restored against the row they were looking at, so
    // loading older replies does not jump them somewhere else in the thread.
    if (restored && offset !== undefined)
      container.scrollTop += restored.getBoundingClientRect().top - offset;
  }

  async function setState(state: 'active' | 'archived' | 'locked') {
    stateError = '';
    try {
      await view.setState(state);
    } catch (error) {
      stateError = error instanceof Error ? error.message : 'That did not work. Try again.';
    }
  }

  $effect(() => {
    view.messages.length;
    pending.length;
    if (atLatest)
      void tick().then(() => {
        if (log) log.scrollTop = log.scrollHeight;
      });
  });

  // A live reply or a state change for this thread. Anything for another thread, or
  // for a thread the person no longer has, is ignored here rather than re-rendered.
  $effect(() => {
    const events = workspace.conversationEvents;
    const last = events.at(-1);
    if (!last) return;
    const data = last.d as { thread_id?: string; conversation_id?: string };
    if (data?.thread_id === view.id) void view.load();
  });

  onMount(() => {
    void view.load();
    return () => {
      // The author keeps their unsent text when the panel closes. The store is
      // account-scoped, so this cannot carry into the next account, and the key the
      // reply was attempted under is not stored with it — an unsent reply is a draft,
      // and the next send from this panel is a new key for new work.
      captureDraft();
      view.dispose();
    };
  });
</script>

<Modal title="Thread" close={() => (workspace.namedThreadId = '')} wide>
  <div class="thread-panel">
    <div class="thread-toolbar">
      <span class="thread-title">
        {#if view.thread}{view.thread.title}{:else}Loading thread…{/if}
      </span>
      {#if view.thread}
        <span class="thread-following">
          {view.thread.followers}
          {view.thread.followers === 1 ? 'person follows' : 'people follow'}
        </span>
        <button
          class="text-button"
          onclick={() => view.setFollow(!view.thread?.following)}
          aria-pressed={view.thread.following}
        >
          {view.thread.following ? 'Following' : 'Follow'}
        </button>
        {#if workspace.canManageChannels}
          <button class="text-button" onclick={() => setState(locked ? 'active' : 'locked')}>
            {locked ? 'Unlock' : 'Lock'}
          </button>
          <button class="text-button" onclick={() => setState('archived')}>Archive</button>
        {/if}
      {/if}
    </div>

    {#if stateError}<p class="thread-error" role="alert">{stateError}</p>{/if}

    {#if view.thread?.state === 'archived'}
      <p class="thread-notice" role="status">
        This thread is archived. Its messages are still here, and reopening it puts it back in the
        channel list.
      </p>
    {/if}

    {#if view.missing}
      <div class="thread-error" role="alert">
        <p>
          This thread is no longer available. It may have been archived away, its channel closed, or
          you may no longer be able to read the channel it was in.
        </p>
        <button class="secondary" onclick={() => (workspace.namedThreadId = '')}>Close</button>
      </div>
    {:else}
      {#if view.error}<div class="thread-error" role="alert">
          <p>{view.error}</p>
          <button class="secondary" onclick={() => view.load()}>Try again</button>
        </div>{/if}

      <div
        class="thread-replies"
        bind:this={log}
        role="log"
        aria-label="Thread messages"
        aria-busy={view.loading}
        onscroll={() =>
          (atLatest =
            (log?.scrollHeight ?? 0) - (log?.scrollTop ?? 0) - (log?.clientHeight ?? 0) < 64)}
      >
        {#if view.hasOlder}
          <button class="history-button" disabled={view.loading} onclick={older}>
            {view.loading ? 'Loading…' : 'Load earlier replies'}
          </button>
        {/if}
        {#if view.loading && !view.messages.length}<p class="panel-empty" role="status">
            Loading the thread…
          </p>
        {:else if !view.messages.length && !pending.length}<div class="thread-empty">
            <Icon name="hash" size={28} />
            <h3>No replies yet</h3>
            <p>Be the first to continue this discussion.</p>
          </div>{/if}
        {#each [...view.messages, ...pending] as message (message.id)}
          <div data-thread-message={message.id}>
            <MessageItem {message} scope="thread" onreply={focusComposer} />
          </div>
        {/each}
      </div>

      {#if locked}
        <p class="thread-notice" role="status">
          This thread is locked. You can still read everything here and follow it, but no new
          replies can be added.
        </p>
      {:else if view.thread}
        <form
          class="thread-composer"
          onsubmit={(event) => {
            event.preventDefault();
            void send();
          }}
        >
          <label class="visually-hidden" for="thread-reply">Reply to this thread</label>
          <textarea
            id="thread-reply"
            bind:this={focusTarget}
            bind:value={draft}
            placeholder="Reply to this thread…"
            rows="2"
            maxlength="4000"></textarea>
          {#if sendState === 'refused' && view.sendError}
            <p class="thread-send-error" role="alert">{view.sendError.message}</p>
          {:else if sendState === 'uncertain' && view.sendError}
            <p class="thread-send-error" role="alert">
              {view.sendError.message} This may or may not have been sent. Check the thread before sending
              it again.
            </p>
          {/if}
          <button type="submit" disabled={!canSend}>Reply</button>
        </form>
      {/if}
    {/if}
  </div>
</Modal>

<style>
  .thread-title {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .thread-following {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .thread-notice {
    color: var(--muted);
    font-size: 0.9rem;
    margin: 0.25rem 0;
  }
  .thread-send-error {
    color: var(--danger);
    font-size: 0.85rem;
    margin: 0.25rem 0;
  }
  .thread-composer {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    border-top: 1px solid var(--border);
    padding-top: 0.6rem;
  }
  .thread-composer button {
    align-self: flex-end;
  }
</style>
