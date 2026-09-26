<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';

  let { message, close }: { message: Message; close: () => void } = $props();
  const account = untrack(() => workspace.user?.id);
  const generation = untrack(() => workspace.authorizationGeneration);
  let alive = true;
  onDestroy(() => {
    alive = false;
  });
  let reason = $state('');
  let busy = $state(false);
  let error = $state('');
  let sent = $state(false);
  let duplicate = $state(false);
  const admitted = () =>
    alive &&
    account === workspace.user?.id &&
    generation === workspace.authorizationGeneration &&
    !workspace.authorizationUnverified &&
    !!workspace.state?.channels.some((channel) => channel.id === message.channel_id);
  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy || !admitted() || !reason.trim()) return;
    busy = true;
    error = '';
    try {
      const result = await api<{ duplicate: boolean }>(`/messages/${message.id}/report`, 'POST', {
        reason: reason.trim()
      });
      if (!admitted()) return;
      duplicate = result.duplicate;
      sent = true;
      reason = '';
    } catch (failure) {
      if (admitted()) error = errorMessage(failure);
    } finally {
      if (admitted()) busy = false;
    }
  }
</script>

<Modal title="Report message" {close} initialFocus="textarea">
  {#if !admitted()}
    <p role="status">
      Your access changed. Close this report and open the message again if it is still available.
    </p>
    <button class="secondary" onclick={close}>Close report</button>
  {:else if sent}
    <p role="status">
      {duplicate
        ? 'You already have an open report for this message. Its original explanation has been kept.'
        : 'Report sent. Moderators who can read this channel can review it.'}
    </p>
    <button class="primary" onclick={close}>Done</button>
  {:else}
    <p>
      The selected message text, your identity and your explanation will be shared with moderators
      who can read this channel. Reports remain available for 90 days, including if the original
      message is deleted. Server backups may retain copies.
    </p>
    <section aria-label="Selected message">
      <h3>Selected message</h3>
      <blockquote>
        {message.content || 'This message has no text. Attachment contents will not be included.'}
      </blockquote>
      <p class="muted">
        Only this message’s text is included. Files, previews and surrounding conversation are not
        included.
      </p>
    </section>
    <form onsubmit={submit}>
      <label
        >Why are you reporting this message?<textarea
          bind:value={reason}
          required
          maxlength="2000"
          rows="4"
          placeholder="Explain what moderators should review."></textarea></label
      >
      <p class="muted">
        {reason.length}/2000 characters. Reporting does not notify the author or take action against
        their account.
      </p>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div class="button-row">
        <button class="secondary" type="button" onclick={close}>Cancel</button>
        <button class="primary" disabled={busy || !reason.trim()}
          >{busy ? 'Sending report…' : 'Send report'}</button
        >
      </div>
    </form>
  {/if}
</Modal>

<style>
  blockquote {
    margin: 12px 0;
    padding: 12px;
    border-left: 3px solid var(--border);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 28vh;
    overflow: auto;
  }
</style>
