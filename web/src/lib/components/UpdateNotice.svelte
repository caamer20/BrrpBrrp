<script lang="ts">
  import { updates } from '../updates.svelte';
  import { workspace } from '../workspace.svelte';
  import { voice } from '../voice.svelte';
  import Icon from './Icon.svelte';
  const voiceActive = $derived(voice.status !== 'disconnected' || !!voice.channelId);
</script>

{#if updates.ready && !updates.dismissed}
  <section class="update-notice" aria-label="App update">
    <div>
      <strong>An update is ready</strong>
      <p>
        {voiceActive
          ? 'Leave voice when you’re ready to update.'
          : !workspace.canUpdate
            ? 'Finish uploads and retry unsent messages before updating.'
            : 'Your drafts will be kept for this refresh.'}
      </p>
      {#if updates.error}<p class="error" role="alert">{updates.error}</p>{/if}
    </div>
    <button
      class="primary"
      disabled={updates.busy || voiceActive || !workspace.canUpdate}
      onclick={() => updates.apply(voiceActive)}>{updates.busy ? 'Updating…' : 'Update now'}</button
    >
    <button
      class="icon-button"
      aria-label="Remind me about this update later"
      onclick={() => (updates.dismissed = true)}><Icon name="close" size={18} /></button
    >
  </section>
{/if}
