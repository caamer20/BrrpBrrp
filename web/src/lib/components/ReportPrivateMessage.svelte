<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { APIError, api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';

  // Reporting a direct message discloses something, so this dialog is built around
  // one rule: the person confirms the exact text the server will save, and that
  // text comes from the server, never from the copy in this browser (ADR 0013).
  //
  // The preview is fetched on open and carries a version. Submitting sends the
  // version, not the text; if the message changed in between, the server refuses
  // and returns a fresh preview, which must be confirmed again. Nothing here ever
  // posts message text as evidence.
  let { message, close }: { message: Message; close: () => void } = $props();
  const account = untrack(() => workspace.user?.id);
  const generation = untrack(() => workspace.authorizationGeneration);
  let alive = true;
  onDestroy(() => {
    alive = false;
  });

  interface Preview {
    message_id: string;
    author_name: string;
    snapshot_text: string;
    has_attachment: boolean;
    version: string;
    expires_at: string;
    reviewer_count: number;
  }
  let preview = $state<Preview | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  // `changed` marks a preview the reporter has not confirmed yet. It keeps the
  // explanation so a re-read costs a glance rather than retyping, which is the
  // point of showing the new text instead of failing silently.
  let changed = $state(false);
  let reason = $state('');
  let busy = $state(false);
  let error = $state('');
  let sent = $state(false);
  let duplicate = $state(false);

  // Read access is what admits a reporter, and a blocked participant keeps it, so
  // the guard is the conversation rather than a channel list. A stale answer from
  // another account or authority is dropped rather than displayed.
  const admitted = () =>
    alive &&
    account === workspace.user?.id &&
    generation === workspace.authorizationGeneration &&
    !workspace.authorizationUnverified;

  async function load() {
    loading = true;
    loadError = '';
    try {
      const result = await api<Preview>(`/messages/${message.id}/private-report-preview`);
      if (!admitted()) return;
      preview = result;
    } catch (failure) {
      if (admitted()) loadError = errorMessage(failure);
    } finally {
      if (admitted()) loading = false;
    }
  }
  void load();

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy || !admitted() || !preview || changed || !reason.trim()) return;
    busy = true;
    error = '';
    try {
      const result = await api<{ duplicate: boolean }>(
        `/messages/${message.id}/private-report`,
        'POST',
        { reason: reason.trim(), version: preview.version }
      );
      if (!admitted()) return;
      duplicate = result.duplicate;
      sent = true;
      reason = '';
    } catch (failure) {
      if (!admitted()) return;
      if (failure instanceof APIError && failure.status === 409) {
        // The message changed after the preview. The answer carries the current
        // text; showing it is the new consent step, and the explanation is kept so
        // a re-read costs a glance rather than retyping.
        const refreshed = failure as unknown as { preview?: Preview };
        if (refreshed.preview) {
          preview = refreshed.preview;
          changed = true;
        } else {
          error = 'This message changed. Close and report it again.';
        }
      } else {
        error = errorMessage(failure);
      }
    } finally {
      if (admitted()) busy = false;
    }
  }
</script>

<Modal title="Report a private message" {close} initialFocus="textarea">
  {#if !admitted()}
    <p role="status">Your access changed. Nothing from this message is sent.</p>
  {:else if sent}
    <p role="status">
      {duplicate
        ? 'You already have an open report for this message. Nothing further was saved.'
        : 'Report sent. Only the text shown here, who wrote it, who reported it and your explanation are saved.'}
    </p>
    <button onclick={close}>Done</button>
  {:else if loading}
    <p role="status">Loading the exact text a report would save…</p>
  {:else if loadError}
    <p role="alert">{loadError}</p>
    <div class="button-row">
      <button class="secondary" onclick={() => void load()}>Try again</button><button
        class="secondary"
        onclick={close}>Close</button
      >
    </div>
  {:else if preview}
    <section aria-label="What will be saved">
      <h3>What will be saved</h3>
      <p>
        One message, exactly as shown below, from {preview.author_name}. Your explanation is saved
        with it. Reviewers see only this: not the rest of the conversation, not replies, not files,
        and nothing you did not choose to send here.
      </p>
      {#if preview.has_attachment}
        <p class="muted">
          This message has a file attached. The file is not included — only any text above.
        </p>
      {/if}
      <blockquote>{preview.snapshot_text || 'This message has no text.'}</blockquote>
      <p class="muted">
        Anyone who holds the private-evidence review permission
        {preview.reviewer_count === 1 ? '(1 account)' : `(${preview.reviewer_count} accounts)`} can read
        this after you send it. It is kept until {new Date(
          preview.expires_at
        ).toLocaleDateString()}. Reviewing it does not delete the message, block anyone, or give
        anyone access to your conversation.
      </p>
    </section>
    {#if changed}
      <p role="status">
        This message changed after you first read it. The text above is what would be saved now —
        read it before sending.
      </p>
    {/if}
    <form onsubmit={submit}>
      <label
        >Why are you reporting this message?<textarea
          bind:value={reason}
          maxlength="2000"
          required
          rows="4"></textarea></label
      >
      {#if error}<p role="alert">{error}</p>{/if}
      <div class="button-row">
        <button class="secondary" onclick={close}>Cancel</button><button
          disabled={busy || changed || !reason.trim()}
          type="submit">{busy ? 'Sending report…' : 'Send report'}</button
        >
      </div>
    </form>
  {/if}
</Modal>

<style>
  section[aria-label='What will be saved'] {
    margin-bottom: 1rem;
  }
  blockquote {
    border-inline-start: 3px solid var(--accent);
    margin: 0.5rem 0;
    padding-inline-start: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
