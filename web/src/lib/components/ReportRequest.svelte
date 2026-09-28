<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { APIError, api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import Modal from './Modal.svelte';

  // Reporting an unwanted request, and the block that goes with it (ADR 0014).
  //
  // The disclosure is the point. The person is shown the exact introduction the
  // server holds — not the browser's copy — along with who it is from, who can
  // review it, when it expires, and that blocking is part of the same action. Only
  // after confirming that does anything happen, and it happens as one transaction:
  // either the evidence is saved, the request rejected and the sender blocked, or
  // none of it happened.
  //
  // Ordinary Block stays a separate, quieter control in the dialog that opened
  // this one, so someone who wants to block without reporting never has to.

  let {
    conversationId,
    requesterName,
    close
  }: { conversationId: string; requesterName: string; close: () => void } = $props();

  const account = untrack(() => workspace.user?.id);
  const generation = untrack(() => workspace.authorizationGeneration);
  let alive = true;
  onDestroy(() => {
    alive = false;
  });

  interface RequestPreview {
    conversation_id: string;
    attempt_id: string;
    requester_id: string;
    requester_name: string;
    introduction: string;
    has_introduction: boolean;
    request_sent_at: string;
    version: string;
    expires_at: string;
    reviewer_count: number;
  }
  let preview = $state<RequestPreview | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  let reason = $state('');
  let busy = $state(false);
  let error = $state('');
  let sent = $state(false);
  // `uncertain` is its own state, not an error message: the operation may well
  // have committed, and the person is told to check rather than being told nothing
  // was sent (ADR 0014, "Own-outcome reconciliation").
  let uncertain = $state(false);
  // The operation key identifies this attempt, so a retry is recognised as the
  // same action rather than new work. It is generated once per dialog.
  const operationKey = crypto.randomUUID();
  let submitted = $state(false);

  const admitted = () =>
    alive &&
    account === workspace.user?.id &&
    generation === workspace.authorizationGeneration &&
    !workspace.authorizationUnverified;

  async function load() {
    loading = true;
    loadError = '';
    try {
      const result = await api<RequestPreview>(`/directs/${conversationId}/report-preview`);
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
    if (busy || !admitted() || !preview || !reason.trim()) return;
    busy = true;
    error = '';
    uncertain = false;
    submitted = true;
    try {
      await api<{ outcome: string; duplicate: boolean }>(
        `/directs/${conversationId}/report-and-block`,
        'POST',
        { reason: reason.trim(), version: preview.version, operation_key: operationKey }
      );
      if (!admitted()) {
        // The operation's own authority invalidation can arrive before its
        // response. That is expected, not failure: the receipt will say what
        // happened, so the person is sent to the check rather than told it failed.
        uncertain = true;
        return;
      }
      sent = true;
    } catch (failure) {
      if (!admitted()) {
        uncertain = true;
        return;
      }
      if (failure instanceof APIError && failure.status === 409) {
        // The request changed under the reporter. The refreshed introduction is
        // returned with the refusal; the explanation is kept, and a fresh
        // confirmation is required before anything is saved.
        const refreshed = (failure.detail ?? {}) as { request?: RequestPreview };
        if (refreshed.request) {
          preview = refreshed.request;
          error = 'This request changed since you read it. Check the text above before reporting.';
        } else {
          error = 'This request changed. Close and open it again to report it.';
        }
      } else {
        error = errorMessage(failure);
      }
    } finally {
      if (alive) busy = false;
    }
  }

  // Checking what happened reconciles *this* operation, and it is deliberately
  // the one action that does not require the authority the operation just
  // changed. Requiring it would make the flow unreachable at exactly the moment
  // it is needed: the block revokes access to the very request being reported, so
  // the check would refuse to run and the person would be left with a report they
  // cannot confirm (ADR 0014, "Own-outcome reconciliation").
  //
  // What it still requires is that this is the same account, because the receipt
  // belongs to whoever started the operation. It never reports that the person is
  // currently blocked: a later deliberate unblock must stay effective.
  async function check() {
    if (busy || !alive || account !== workspace.user?.id) return;
    busy = true;
    error = '';
    try {
      await api<{ outcome: string }>(
        `/me/report-receipt?operation_key=${encodeURIComponent(operationKey)}`
      );
      if (account !== workspace.user?.id) return;
      sent = true;
      uncertain = false;
    } catch (failure) {
      if (account === workspace.user?.id) error = errorMessage(failure);
    } finally {
      if (alive) busy = false;
    }
  }
</script>

<Modal title="Report this request" {close} initialFocus="textarea">
  <!-- A confirmed outcome is shown even though the block revoked the authority the
       operation itself needed. Reporting that it worked is not a privilege
       decision, and hiding the answer behind the very change the person just made
       would leave them unable to tell whether it succeeded. -->
  {#if sent}
    <p role="status">
      The request was declined, its introduction saved as evidence and the sender blocked. That
      report is kept until it expires; blocking them stops new messages and requests. If you later
      unblock them, that choice stands — checking this again will not block them a second time.
    </p>
    <button onclick={close}>Done</button>
  {:else if uncertain || !admitted()}
    <p role="status">
      {#if submitted}
        Your access changed while this was being sent, so the result is not shown here. Check what
        happened — this checks the same action and will not report it again.
      {:else}
        Your access changed. Nothing from this request has been sent.
      {/if}
    </p>
    <div class="button-row">
      <button class="secondary" onclick={() => void check()}>Check what happened</button><button
        class="secondary"
        onclick={close}>Close</button
      >
    </div>
  {:else if loading}
    <p role="status">Loading the exact request you would be reporting…</p>
  {:else if loadError}
    <p role="alert">{loadError}</p>
    <p class="muted">
      This request can no longer be reported: it may have been accepted, withdrawn or cleared.
    </p>
    <div class="button-row">
      <button class="secondary" onclick={close}>Close</button>
    </div>
  {:else if preview}
    <section aria-label="What will be saved">
      <h3>What will be saved</h3>
      <p>
        This request from <strong>{preview.requester_name || requesterName}</strong>, and your
        explanation for reporting it. Nobody else sees it: not the conversation it would have
        opened, and not any message in it.
      </p>
      <blockquote>
        {preview.has_introduction ? preview.introduction : 'This request has no introduction.'}
      </blockquote>
      <p class="muted">
        {preview.reviewer_count === 1 ? '1 account' : `${preview.reviewer_count} accounts`} holding the
        private-evidence review permission can read it. It is kept until {new Date(
          preview.expires_at
        ).toLocaleDateString()}, and reviewing it neither deletes anything nor blocks anyone a
        second time.
      </p>
      <p class="muted">
        Sending this also <strong>blocks {preview.requester_name || requesterName}</strong>,
        declines the request and clears its introduction. That is why it cannot be undone by
        clicking again. If you only want to block without reporting, close this and use Block.
      </p>
    </section>
    <form onsubmit={submit}>
      <label
        >Why are you reporting this request?<textarea
          bind:value={reason}
          maxlength="2000"
          required
          rows="3"></textarea></label
      >
      {#if error}<p role="alert">{error}</p>{/if}
      <div class="button-row">
        <button class="secondary" onclick={close}>Cancel</button><button
          disabled={busy || !reason.trim()}
          type="submit">{busy ? 'Reporting…' : 'Report and block'}</button
        >
      </div>
    </form>
  {/if}
</Modal>

<style>
  blockquote {
    border-inline-start: 3px solid var(--accent);
    margin: 0.5rem 0;
    padding-inline-start: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
