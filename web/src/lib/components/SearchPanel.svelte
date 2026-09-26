<script lang="ts">
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import HighlightedText from './HighlightedText.svelte';
  let query = $state('');
  let channel = $state(workspace.direct?.id || '');
  let results = $state<Message[]>([]);
  let searched = $state(false);
  let busy = $state(false);
  let error = $state('');
  let resultQuery = $state('');
  let resultChannel = $state('');
  let changed = $derived(searched && (query.trim() !== resultQuery || channel !== resultChannel));
  // Search results are a private view this account may lose access to. When the
  // authorization generation moves — a role change, a revocation, a workspace event —
  // the results on screen are dropped immediately, and an answer that was already in
  // flight is discarded when it lands, so a revoked hit cannot reappear.
  let generation = $derived(workspace.authorizationGeneration);
  $effect(() => {
    generation;
    results = [];
    searched = false;
    resultQuery = '';
    resultChannel = '';
    busy = false;
  });
  async function search(event: SubmitEvent) {
    event.preventDefault();
    if (!query.trim() || busy) return;
    const searchQuery = query.trim();
    const searchChannel = channel;
    const admitted = workspace.authorizationGeneration;
    busy = true;
    error = '';
    results = [];
    searched = false;
    try {
      const hits = await api<Message[]>(
        `/search?q=${encodeURIComponent(searchQuery)}&${workspace.isDirect(searchChannel) ? 'conversation_id' : 'channel_id'}=${encodeURIComponent(searchChannel)}`
      );
      if (admitted !== workspace.authorizationGeneration) return;
      results = hits;
      resultQuery = searchQuery;
      resultChannel = searchChannel;
      searched = true;
    } catch (failure) {
      if (admitted !== workspace.authorizationGeneration) return;
      error = errorMessage(failure);
    } finally {
      if (admitted === workspace.authorizationGeneration) busy = false;
    }
  }
  async function jump(message: Message) {
    try {
      await workspace.jumpToMessage(message.id);
    } catch (failure) {
      error = errorMessage(failure);
    }
  }
</script>

<Modal title="Search messages" initialFocus="input" close={() => (workspace.searchOpen = false)}>
  <form class="search-form" onsubmit={search}>
    <label
      >Search<input
        bind:value={query}
        required
        maxlength="200"
        placeholder="Find something in the conversation…"
      /></label
    ><label
      >Channel<select bind:value={channel}
        ><option value="">All text channels</option
        >{#each workspace.state?.channels.filter((channel) => channel.type === 'text') || [] as item}<option
            value={item.id}>#{item.name}</option
          >{/each}{#each workspace.directConversations as direct}<option value={direct.id}
            >Direct · {direct.peer.display_name || direct.peer.username}</option
          >{/each}</select
      ></label
    ><button class="primary" disabled={busy || !query.trim()}
      ><Icon name="search" size={18} />{busy ? 'Searching…' : 'Search'}</button
    >
  </form>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if busy}<p class="search-status muted" role="status">Searching conversations…</p>
  {:else if !searched && !error}<div class="panel-empty">
      <Icon name="search" size={28} />
      <h3>Find a moment in the conversation</h3>
      <p>Search for a word or phrase, or narrow it to one channel.</p>
    </div>
  {:else if changed}<p class="search-status muted" role="status">
      Search again to update the results.
    </p>
  {:else if searched && !error}<p class="search-status muted" role="status">
      {results.length === 50
        ? 'First 50 matches. Refine your search for more.'
        : `${results.length} ${results.length === 1 ? 'message' : 'messages'} found`}
    </p>{/if}
  {#if searched && !changed && !results.length && !error}<div class="panel-empty">
      <Icon name="search" size={28} />
      <h3>No matches for “{resultQuery}”</h3>
      <p>Try a different word or search across all text channels.</p>
    </div>{/if}
  <div class="search-results" aria-busy={busy}>
    {#if !changed}
      {#each results as message (message.id)}<button onclick={() => jump(message)}
          ><small
            >{workspace.isDirect(workspace.conversationId(message))
              ? 'Direct conversation'
              : '#' +
                (workspace.state?.channels.find((channel) => channel.id === message.channel_id)
                  ?.name || 'channel')} ·
            {new Date(message.created_at).toLocaleDateString()}</small
          ><strong
            >{workspace.state?.users.find((user) => user.id === message.user_id)?.display_name ||
              'Member'}</strong
          ><span><HighlightedText text={message.content} query={resultQuery} /></span></button
        >{/each}
    {/if}
  </div>
</Modal>
