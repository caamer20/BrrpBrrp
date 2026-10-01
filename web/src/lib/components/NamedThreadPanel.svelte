<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { ThreadView } from '../threads.svelte';
  import { threadSubmitState, canSendThreadReply, retryOperationKey } from '../thread-state';
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
  // The identity this panel was mounted under. A cleanup, an effect or a completion
  // that runs after a sign-out or an account switch presents it, and the store refuses
  // to let this panel's text or its saved draft move into the account that replaced it.
  const owner = untrack(() => workspace.composerOwner());
  // The operations this panel's own composer submitted into this thread and has not
  // seen settled, oldest first. Each one's words are its immutable intent: submitting
  // exactly those words again retries *that* operation under its own key and
  // reconciles against its receipt — whether it is the newest submission or an older
  // one still waiting. Selection is this composer's explicit association (account,
  // destination, submitted snapshot, key), never the first outbox entry and never a
  // text search over the queue: identical words can be two deliberate sends, and the
  // association is what tells a retry of one from a new copy of another
  // (review 17, V17-01). The key is never adopted blindly, and never into a composer
  // that no longer belongs to the session that submitted it.
  const unresolvedOperations = $derived(workspace.threadComposerOperations(id));
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
    if (draft.trim()) workspace.saveComposerDraft(draftSlot, draft, [], owner);
    else workspace.clearComposerDraft(draftSlot, owner);
  }

  const locked = $derived(view.thread?.state === 'locked');
  // Archived and locked both stop replies, and the interface distinguishes them:
  // archived is out of the way until a manager reopens it, locked is a pause.
  const archived = $derived(view.thread?.state === 'archived');
  const sendState = $derived(threadSubmitState(view.sending, view.sendError));
  const canSend = $derived(
    canSendThreadReply({
      open: !!view.thread,
      locked,
      archived,
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
  // Whether the current words are new work beside an unconfirmed reply. The words
  // retry their own operation while they match its submitted snapshot; edited words
  // are new work, and the notice says so rather than letting an older uncertain reply
  // look replaced. Unconfirmed replies the store still shows as rows count too, so the
  // warning does not depend on which session submitted them.
  const newReplyNotice = $derived(
    draft.trim().length > 0 &&
      !unresolvedOperations.some((operation) => operation.content === draft.trim()) &&
      (unresolvedOperations.length > 0 || pending.length > 0)
  );

  async function focusComposer() {
    await tick();
    focusTarget?.focus();
  }

  async function send() {
    if (!canSend) return;
    const text = draft;
    const submitted = text.trim();
    view.sendError = null;
    // The words being submitted select their own operation: exactly the words this
    // composer already submitted as an unresolved reply retry under that operation's
    // key — every unresolved submission is a candidate, so retrying the newest reply
    // no longer rotates its key because an older one happened to be first. Anything
    // else — edited words, or words whose operation has settled — is new work under a
    // fresh key.
    const key = retryOperationKey(unresolvedOperations, submitted) ?? crypto.randomUUID();
    // The text is cleared *after* the outcome, not before. Clearing it first meant a
    // refusal and a lost response both left an empty composer, and the next attempt then
    // made the lost response a second message.
    const outcome = await view.send(text, key);
    if (outcome === 'sent') {
      // Only the text that was sent is cleared. Anything typed while the request was in
      // flight — or newer text already in the slot — is left alone, and the saved draft
      // is cleared only when it still holds what was sent, so reopening cannot resurrect
      // a reply that already exists.
      if (draft === text) draft = '';
      workspace.clearSentDraft(draftSlot, submitted, owner);
      return;
    }
    if (outcome === 'dropped') return;
    // A definite refusal and an unknown outcome both keep the text. If the author emptied
    // the composer while it was in flight, their words are put back rather than lost.
    if (!draft.trim()) draft = text;
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

  // The reader's position follows what is actually on screen: while the newest
  // replies are shown — whether by scrolling to the bottom or by arriving there
  // through a deep link — the thread's own cursor moves to the newest stored one. It
  // never touches the channel's cursor, and a newer draft being typed is not a
  // message to point at.
  $effect(() => {
    view.messages.length;
    view.atNewest;
    if ((!atLatest && !view.atNewest) || !view.messages.length) return;
    void view.markRead(view.messages[view.messages.length - 1].id);
  });

  // A live reply or a state change for this thread. Anything for another thread, or
  // for a thread the person no longer has, is ignored here rather than re-rendered.
  // A reply arriving while the reader is at the newest replies is folded in; while
  // they are reading history it must not yank them to the bottom, so the window says
  // that something new exists and offers the jump instead.
  let newerReplies = $state(false);
  $effect(() => {
    const events = workspace.conversationEvents;
    const last = events.at(-1);
    if (!last) return;
    const data = last.d as { thread_id?: string; conversation_id?: string };
    if (data?.thread_id !== view.id) return;
    if (atLatest) void view.load();
    else newerReplies = true;
  });

  function jumpToLatest() {
    newerReplies = false;
    atLatest = true;
    void view.load();
  }

  onMount(() => {
    // A deep link names one reply to open the thread at. It is consumed once, and the
    // bounded window is positioned around it rather than at the newest replies, so a
    // link to the thousandth reply lands on that reply.
    const jump = workspace.threadJumpId === id ? workspace.threadJumpId : '';
    if (jump) workspace.threadJumpId = '';
    if (jump) {
      atLatest = false;
      void view.loadAround(jump).then(async () => {
        await tick();
        const target = log?.querySelector<HTMLElement>(
          `[data-thread-message="${CSS.escape(jump)}"]`
        );
        target?.scrollIntoView({ block: 'center' });
      });
    } else {
      void view.load();
    }
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
          {#if archived}
            <button class="text-button" onclick={() => setState('active')}>Reopen</button>
          {:else}
            <button class="text-button" onclick={() => setState(locked ? 'active' : 'locked')}>
              {locked ? 'Unlock' : 'Lock'}
            </button>
            <button class="text-button" onclick={() => setState('archived')}>Archive</button>
          {/if}
        {/if}
      {/if}
    </div>

    {#if stateError}<p class="thread-error" role="alert">{stateError}</p>{/if}

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
        onscroll={() => {
          const atBottom =
            (log?.scrollHeight ?? 0) - (log?.scrollTop ?? 0) - (log?.clientHeight ?? 0) < 64;
          atLatest = atBottom;
          if (atBottom && newerReplies) jumpToLatest();
        }}
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

      {#if newerReplies}
        <div class="thread-newer">
          <button class="secondary" onclick={jumpToLatest}>New replies in this thread</button>
        </div>
      {/if}

      {#if locked}
        <p class="thread-notice" role="status">
          This thread is locked. You can still read everything here and follow it, but no new
          replies can be added.
        </p>
      {:else if archived}
        <p class="thread-notice" role="status">
          This thread is archived, so new replies are closed. Its messages are still here and it
          stays in the channel's thread list. {workspace.canManageChannels
            ? 'Reopen it to reply.'
            : 'A channel manager can reopen it.'}
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
          {#if newReplyNotice}
            <p class="thread-notice" role="status">
              An earlier reply here was not confirmed. It stays in this thread for you to retry or
              discard, and this text will be sent as a new reply.
            </p>
          {/if}
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
  .thread-newer {
    display: flex;
    justify-content: center;
    padding: 0.3rem 0;
  }
  .thread-composer button {
    align-self: flex-end;
  }
</style>
