<script lang="ts">
  import Modal from './Modal.svelte';
  import { workspace } from '../workspace.svelte';
  import { reactionName, reactionKeywords } from '../reactions';
  let { close, select }: { close: () => void; select: (emoji: string) => void } = $props();
  let query = $state('');
  let emojis = $derived(
    (workspace.state?.reaction_palette || []).filter((emoji) =>
      `${emoji} ${reactionKeywords(emoji)}`.toLowerCase().includes(query.trim().toLowerCase())
    )
  );
</script>

<Modal title="Add a reaction" {close} initialFocus="input">
  <label
    >Find an emoji<input
      type="search"
      bind:value={query}
      placeholder="Try heart, laugh, celebrate…"
    /></label
  >
  <div class="reaction-results" aria-label="Reactions">
    {#each emojis as emoji}
      <button
        title={reactionName(emoji)}
        aria-label={`React with ${reactionName(emoji)}`}
        onclick={() => {
          select(emoji);
          close();
        }}>{emoji}</button
      >
    {/each}
  </div>
  {#if !emojis.length}<p role="status" class="muted">
      No matching reactions. Try another word.
    </p>{/if}
</Modal>
