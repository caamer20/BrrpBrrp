<script lang="ts">
  import type { Attachment } from '../types';
  import { workspace } from '../workspace.svelte';
  import Icon from './Icon.svelte';
  let { attachment }: { attachment: Attachment } = $props();
  let revealed = $state(false);
  let failed = $state(false);
  let loaded = $state(false);
  let attempt = $state(0);
  const original = $derived(`/api/uploads/${attachment.id}`);
</script>

<div class="image-preview" class:loaded>
  {#if workspace.lowResource && !revealed}
    <button class="image-preview-placeholder" onclick={() => (revealed = true)}>
      <Icon name="file" size={28} /><strong>Show image preview</strong>
      <span>{attachment.file_name}</span><small>Data saver is on</small>
    </button>
  {:else if failed}
    <div class="image-preview-placeholder">
      <Icon name="file" size={28} /><span>Preview couldn’t load</span>
      <button
        class="secondary"
        onclick={() => {
          failed = false;
          attempt++;
        }}>Try again</button
      >
      <a href={original} target="_blank" rel="noreferrer">Open original image</a>
    </div>
  {:else}
    {#key attempt}<a
        href={original}
        target="_blank"
        rel="noreferrer"
        aria-label={`Open original: ${attachment.file_name}`}
      >
        <img
          src={`${original}?preview=1`}
          alt={attachment.file_name}
          width="640"
          height="480"
          loading="lazy"
          decoding="async"
          onload={() => (loaded = true)}
          onerror={() => (failed = true)}
        />
      </a>{/key}
    {#if attachment.mime_type === 'image/gif'}<span class="image-preview-badge"
        >GIF · Open to play</span
      >{/if}
  {/if}
</div>
