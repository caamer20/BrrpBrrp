<script lang="ts">
  import { updates } from '../updates.svelte';
  import { workspace } from '../workspace.svelte';
  import { voice } from '../voice.svelte';
  import Icon from './Icon.svelte';
  const voiceActive = $derived(voice.status !== 'disconnected' || !!voice.channelId);
</script>

{#if workspace.protocolNotice}
  <!-- A protocol the server cannot answer is not a suggestion to update later: sending is
       refused until it is resolved, so the notice says why and keeps its action in front
       of the person. It cannot be dismissed — there is nothing to dismiss. -->
  <section class="update-notice" role="alert" aria-label="Update required">
    <div>
      <strong>This app needs updating</strong>
      <p>{workspace.protocolNotice}</p>
      {#if updates.error}<p class="error" role="alert">{updates.error}</p>{/if}
    </div>
    <button
      class="primary"
      disabled={updates.busy || voiceActive || !workspace.canUpdate}
      onclick={() => updates.apply(voiceActive)}>{updates.busy ? 'Updating…' : 'Update now'}</button
    >
  </section>
{:else if updates.ready && !updates.dismissed}
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
