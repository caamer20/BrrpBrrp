<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { Replies } from '../replies.svelte';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';
  import MessageItem from './MessageItem.svelte';
  import Composer from './Composer.svelte';
  import Icon from './Icon.svelte';

  let { id }: { id: string } = $props();
  // Workspace keys this component by message ID, giving each view its own requests.
  const replies = new Replies(
    untrack(() => id),
    (messages) => workspace.acknowledgeMessages(messages)
  );
  let log: HTMLDivElement;
  let atLatest = $state(true);
  let composerParent = $state<Message | null>(null);
  $effect(() => {
    if (replies.parent) composerParent = replies.parent;
  });
  let pending = $derived(
    workspace
      .pendingReplies(id)
      .filter(
        (message) =>
          !replies.messages.some(
            (saved) =>
              saved.idempotency_key === message.idempotency_key && saved.user_id === message.user_id
          )
      )
  );

  async function focusComposer() {
    await tick();
    document.querySelector<HTMLTextAreaElement>('.thread-composer textarea')?.focus();
  }

  async function older() {
    const anchor = log?.querySelector<HTMLElement>('[data-reply-id]');
    const anchorId = anchor?.dataset.replyId;
    const offset = anchor?.getBoundingClientRect().top;
    atLatest = false;
    await replies.older();
    await tick();
    const restored = Array.from(log.querySelectorAll<HTMLElement>('[data-reply-id]')).find(
      (element) => element.dataset.replyId === anchorId
    );
    if (restored && offset !== undefined)
      log.scrollTop += restored.getBoundingClientRect().top - offset;
  }

  $effect(() => {
    replies.messages.length;
    pending.length;
    if (atLatest)
      void tick().then(() => {
        if (log) log.scrollTop = log.scrollHeight;
      });
  });

  onMount(() => {
    const receive = (event: Parameters<Replies['receive']>[0]) => replies.receive(event);
    workspace.onConversationEvent = receive;
    void replies.load();
    return () => {
      replies.dispose();
      if (workspace.onConversationEvent === receive) workspace.onConversationEvent = null;
    };
  });
</script>

<Modal title="Reply thread" close={() => (workspace.threadId = '')} wide>
  <div class="thread-panel">
    <div class="thread-toolbar">
      <span><Icon name="hash" size={16} />{workspace.conversationName}</span>
      {#if replies.parent}<button class="text-button" onclick={() => workspace.jumpToMessage(id)}
          >Show in conversation <Icon name="chevronRight" size={14} /></button
        >{/if}
    </div>
    {#if replies.error}<div class="thread-error" role="alert">
        <p>{replies.error}</p>
        {#if !replies.missing}<button class="secondary" onclick={() => replies.load()}
            >Try again</button
          >{/if}
      </div>{/if}
    {#if replies.parent}
      <div class="thread-original">
        {#if replies.parent.reply_to_id}<button
            class="text-button"
            onclick={() => (workspace.threadId = replies.parent!.reply_to_id!)}
            ><Icon name="reply" size={15} /> Back to parent discussion</button
          >{/if}
        <MessageItem
          message={replies.parent}
          scope="thread-root"
          onreply={focusComposer}
          showThread={false}
        />
      </div>
      <div class="thread-divider">
        <span
          >{replies.parent.reply_count || 0}
          {replies.parent.reply_count === 1 ? 'reply' : 'replies'}</span
        ><span>Visible to this channel</span>
      </div>
    {/if}
    <div
      class="thread-replies"
      bind:this={log}
      role="log"
      aria-label="Thread replies"
      aria-busy={replies.loading}
      onscroll={() => (atLatest = log.scrollHeight - log.scrollTop - log.clientHeight < 64)}
    >
      {#if replies.hasOlder}
        {#if replies.pages < 5}<button
            class="history-button"
            disabled={replies.loading}
            onclick={older}>{replies.loading ? 'Loading…' : 'Load earlier replies'}</button
          >
        {:else}<p class="muted thread-limit">
            Showing the latest 250 replies. Use channel search for earlier messages.
          </p>{/if}
      {/if}
      {#if replies.loading && !replies.parent}<p class="panel-empty" role="status">
          Loading discussion…
        </p>
      {:else if replies.parent && !replies.messages.length && !pending.length}<div
          class="thread-empty"
        >
          <Icon name="reply" size={28} />
          <h3>Keep the conversation going</h3>
          <p>Reply with a question, an idea, or the next step.</p>
        </div>{/if}
      {#each [...replies.messages, ...pending] as message (message.id)}<div
          data-reply-id={message.id}
        >
          <MessageItem {message} scope="thread" onreply={() => (workspace.threadId = message.id)} />
        </div>{/each}
    </div>
    {#if composerParent}<Composer thread={composerParent} unavailable={replies.missing} />{/if}
  </div>
</Modal>
