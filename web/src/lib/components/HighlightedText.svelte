<script lang="ts">
  let { text, query }: { text: string; query: string } = $props();
  let terms = $derived(
    query
      .trim()
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 12)
      .map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  );
  let parts = $derived(
    terms.length ? text.split(new RegExp(`(${terms.join('|')})`, 'gi')) : [text]
  );
</script>

{#each parts as part, index}{#if terms.length && index % 2}<mark>{part}</mark
    >{:else}{part}{/if}{/each}
