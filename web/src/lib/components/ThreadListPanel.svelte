<script lang="ts">
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import type { Thread } from '../types';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let threads = $state<Thread[]>([]);
  let loading = $state(true);
  let error = $state('');
  let showArchived = $state(false);

  // A channel's threads are channel content, so they follow the same authorization
  // generation the pins do: a change clears what is shown and an answer admitted under
  // the old authority is dropped rather than displayed. Without the re-fetch the
  // panel would sit on its loading state forever after a generation change.
  let generation = $derived(workspace.authorizationGeneration);
  $effect(() => {
    generation;
    workspace.activeId;
    showArchived;
    void load();
  });

  async function load() {
    if (!workspace.activeId || !workspace.channel) {
      threads = [];
      loading = false;
      return;
    }
    error = '';
    loading = true;
    const admitted = workspace.authorizationGeneration;
    const suffix = showArchived ? '?archived=1' : '';
    try {
      const result = await api<Thread[]>(
        `/channels/${encodeURIComponent(workspace.activeId)}/threads${suffix}`
      );
      if (admitted !== workspace.authorizationGeneration) return;
      threads = result;
    } catch (failure) {
      if (admitted !== workspace.authorizationGeneration) return;
      error = errorMessage(failure);
    } finally {
      if (admitted === workspace.authorizationGeneration) loading = false;
    }
  }

  function when(value: string) {
    if (!value) return '';
    const then = new Date(value).getTime();
    if (Number.isNaN(then)) return '';
    const days = Math.floor((Date.now() - then) / 86_400_000);
    if (days <= 0) return 'today';
    if (days === 1) return 'yesterday';
    if (days < 30) return `${days} days ago`;
    const months = Math.floor(days / 30);
    return months === 1 ? 'a month ago' : `${months} months ago`;
  }
</script>

<Modal
  title="Threads in {workspace.channel?.name ?? 'this channel'}"
  close={() => (workspace.threadsOpen = false)}
  wide
>
  <div class="thread-list">
    <div class="thread-list-controls">
      <label class="checkbox">
        <input type="checkbox" bind:checked={showArchived} />
        Show archived threads
      </label>
      <button class="secondary" onclick={() => void load()} disabled={loading}>Refresh</button>
    </div>

    {#if error}<p role="alert" class="thread-list-error">{error}</p>{/if}

    {#if loading}
      <p class="panel-empty" role="status">Loading threads…</p>
    {:else if !threads.length}
      <div class="thread-list-empty">
        <Icon name="hash" size={28} />
        <h3>{showArchived ? 'No archived threads' : 'No threads yet'}</h3>
        <p>
          {showArchived
            ? 'Threads you archive will be listed here.'
            : 'Open a message’s menu and choose “Start a named thread” to keep a discussion together without burying the channel.'}
        </p>
      </div>
    {:else}
      <ul class="thread-rows">
        {#each threads as thread (thread.id)}
          <li>
            <button
              class="thread-row"
              onclick={() => {
                workspace.namedThreadId = thread.id;
                workspace.threadsOpen = false;
              }}
            >
              <span class="thread-row-title">
                {thread.title}
                {#if thread.state === 'archived'}<span class="badge">Archived</span>{/if}
                {#if thread.state === 'locked'}<span class="badge">Locked</span>{/if}
              </span>
              <span class="thread-row-meta">
                {thread.reply_count}
                {thread.reply_count === 1 ? 'reply' : 'replies'}
                {#if thread.last_reply_at}· {when(thread.last_reply_at)}{/if}
                {#if thread.followers}· {thread.followers} following{/if}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Modal>

<style>
  .thread-list-controls {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    margin-bottom: 0.6rem;
  }
  .thread-list-error {
    color: var(--danger);
  }
  .thread-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .thread-row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.15rem;
    width: 100%;
    text-align: left;
    background: none;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.5rem 0.6rem;
    cursor: pointer;
  }
  .thread-row:hover,
  .thread-row:focus-visible {
    background: var(--surface-hover, rgba(127, 127, 127, 0.08));
  }
  .thread-row-title {
    font-weight: 600;
  }
  .thread-row-meta {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .badge {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--muted);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 0.3rem;
    margin-left: 0.3rem;
  }
</style>
