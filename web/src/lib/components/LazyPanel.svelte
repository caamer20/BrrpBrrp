<script lang="ts">
  import type { Component } from 'svelte';
  import { untrack } from 'svelte';
  import Modal from './Modal.svelte';
  let {
    load,
    title,
    close,
    componentProps = {}
  }: {
    load: () => Promise<{ default: Component<any> }>;
    title: string;
    close?: () => void;
    componentProps?: Record<string, unknown>;
  } = $props();
  let request = $state(untrack(() => load()));
</script>

{#snippet progress(failed = false)}
  <div class="lazy-panel-status" role={failed ? 'alert' : 'status'}>
    <p>
      {failed
        ? 'This screen couldn’t load. Check your connection and try again.'
        : `Opening ${title.toLowerCase()}…`}
    </p>
    {#if failed}<button class="secondary" onclick={() => (request = load())}>Try again</button>{/if}
  </div>
{/snippet}
{#await request}
  {#if close}<Modal {title} {close}>{@render progress()}</Modal>{:else}<div class="app-loading">
      {@render progress()}
    </div>{/if}
{:then module}
  <module.default {...componentProps} />
{:catch}
  {#if close}<Modal {title} {close}>{@render progress(true)}</Modal>{:else}<div class="app-loading">
      {@render progress(true)}
    </div>{/if}
{/await}
