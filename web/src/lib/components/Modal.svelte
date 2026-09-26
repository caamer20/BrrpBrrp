<script lang="ts">
  import { onMount } from 'svelte';
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  let {
    title,
    close,
    children,
    wide = false,
    initialFocus
  }: {
    title: string;
    close: () => void;
    children: Snippet;
    wide?: boolean;
    initialFocus?: string;
  } = $props();
  const titleId = $props.id();
  let dialog: HTMLDialogElement;
  let mounted = false;
  onMount(() => {
    mounted = true;
    const previous = document.activeElement as HTMLElement | null;
    dialog.showModal();
    if (initialFocus) dialog.querySelector<HTMLElement>(initialFocus)?.focus();
    return () => {
      mounted = false;
      dialog.close();
      queueMicrotask(() => {
        if (
          previous?.isConnected &&
          !previous.closest('[inert]') &&
          !document.querySelector('dialog[open]')
        )
          previous.focus();
      });
    };
  });
</script>

<dialog
  aria-labelledby={titleId}
  bind:this={dialog}
  class:wide
  oncancel={close}
  onclose={() => {
    if (mounted) close();
  }}
>
  <header class="modal-header">
    <h2 id={titleId}>{title}</h2>
    <button class="icon-button" aria-label="Close dialog" onclick={close}
      ><Icon name="close" /></button
    >
  </header>
  <div class="modal-body">{@render children()}</div>
</dialog>
