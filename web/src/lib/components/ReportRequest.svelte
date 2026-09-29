<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { APIError, api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import { admitted as isAdmitted, mayReportFailure } from '../request-report';
  import {
    canSubmitIn,
    keepsOperationKey,
    offersReconciliation,
    submitDisabled,
    submitLabel,
    type ReportState
  } from '../report-dialog-state';
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
  let loadError = $state('');
  let reason = $state('');
  let busy = $state(false);
  let error = $state('');
  // `uncertain` is its own state, not an error message: the operation may well
  // have committed, and the person is told to check rather than being told nothing
  // was sent (ADR 0014, "Own-outcome reconciliation").
  // The operation key identifies this attempt, so a retry is recognised as the
  // same action rather than new work. It is generated once per dialog and held
  // across every state that is still the same intent; a changed request rotates it,
  // because the frozen version no longer describes what would be written.
  let operationKey = crypto.randomUUID();
  // Explicit rather than a "submitted" flag. A flag set before the request and
  // never cleared left the button looking enabled after a refusal while its handler
  // returned immediately, so the person could press it forever (R12-07).
  let phase = $state<ReportState>('loading');

  // The admission rules live in request-report.ts so they can be tested directly:
  // they are a sequence of await boundaries, and the case that matters is what
  // happens when the account changes while a request is in flight.
  // Named for what it reads rather than `state`, which is Svelte's own rune.
  const admission = () => ({
    alive,
    account,
    currentAccountId: workspace.user?.id,
    generation,
    authorizationGeneration: workspace.authorizationGeneration,
    authorizationUnverified: workspace.authorizationUnverified
  });
  const admitted = () => isAdmitted(admission());

  async function load() {
    phase = 'loading';
    loadError = '';
    try {
      const result = await api<RequestPreview>(`/directs/${conversationId}/report-preview`);
      if (!isAdmitted(admission())) return;
      preview = result;
      phase = 'ready';
    } catch (failure) {
      if (!isAdmitted(admission())) return;
      loadError = errorMessage(failure);
      phase = 'load-failed';
    }
  }
  void load();

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!isAdmitted(admission()) || !canSubmitIn(phase, busy, reason)) return;
    // canSubmit proved the preview is present, but the guard cannot narrow it
    // across the call, so the value the request carries is captured explicitly
    // rather than read again from state that could have changed.
    const confirmed = preview;
    if (!confirmed) return;
    busy = true;
    error = '';
    phase = 'submitting';
    try {
      await api<{ outcome: string; duplicate: boolean }>(
        `/directs/${conversationId}/report-and-block`,
        'POST',
        { reason: reason.trim(), version: confirmed.version, operation_key: operationKey }
      );
      if (!admitted()) {
        // The operation's own authority invalidation can arrive before its
        // response. That is expected, not failure: the receipt will say what
        // happened, so the person is sent to the check rather than told it failed.
        phase = 'uncertain';
        return;
      }
      phase = 'committed';
    } catch (failure) {
      if (!isAdmitted(admission())) {
        phase = 'uncertain';
        return;
      }
      if (failure instanceof APIError && failure.status === 409) {
        // The request changed under the reporter. The refreshed introduction is
        // returned with the refusal; the explanation is kept, and a fresh
        // confirmation is required before anything is saved.
        const refreshed = (failure.detail ?? {}) as { request?: RequestPreview };
        if (refreshed.request) {
          preview = refreshed.request;
          phase = 'changed';
          error = 'This request changed since you read it. Check the text above before reporting.';
        } else {
          phase = 'refused';
          error = 'This request changed. Close and open it again to report it.';
        }
      } else {
        // A definite refusal wrote nothing, so the same action stays available and
        // the key is still the one this dialog started with.
        phase = 'refused';
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
    phase = 'checking';
    try {
      await api<{ outcome: string }>(
        `/me/report-receipt?operation_key=${encodeURIComponent(operationKey)}`
      );
      if (account !== workspace.user?.id) return;
      phase = 'committed';
    } catch (failure) {
      if (!mayReportFailure(admission())) return;
      // A 404 means the key was never spent, so nothing was written and the same
      // action is still available. Anything else — a transport failure, a disabled
      // feature, a 410 for an aged-out receipt — is an error the person has to be
      // able to read, which is why the uncertain view renders it (R12-07).
      const status = failure instanceof APIError ? failure.status : 0;
      if (status === 404) {
        phase = 'uncommitted';
        error = 'That report was never submitted. You can report it now.';
      } else {
        phase = 'uncertain';
        error = errorMessage(failure);
      }
    } finally {
      if (alive) busy = false;
    }
  }

  // A changed request is different work, so the dialog takes a new key: the frozen
  // one is bound to a version that no longer describes anything.
  $effect(() => {
    if (keepsOperationKey(phase)) return;
    operationKey = crypto.randomUUID();
  });
</script>

<Modal title="Report this request" {close} initialFocus="textarea">
  <!-- A confirmed outcome is shown even though the block revoked the authority the
       operation itself needed. Reporting that it worked is not a privilege
       decision, and hiding the answer behind the very change the person just made
       would leave them unable to tell whether it succeeded. -->
  {#if phase === 'committed'}
    <p role="status">
      The request was declined, its introduction saved as evidence and the sender blocked. That
      report is kept until it expires; blocking them stops new messages and requests. If you later
      unblock them, that choice stands — checking this again will not block them a second time.
    </p>
    <button onclick={close}>Done</button>
  {:else if offersReconciliation(phase) || !admitted()}
    <p role="status">
      {#if phase === 'uncommitted'}
        That report was never submitted, so nothing was saved and the sender was not blocked. You
        can report it now.
      {:else if phase === 'checking'}
        Checking what happened…
      {:else}
        Your access changed while this was being sent, so the result is not shown here. Check what
        happened — this checks the same action and will not report it again.
      {/if}
    </p>
    <!-- The check's own failure has to be visible here. It was stored and never
         rendered, so a transport error or a disabled feature left the person
         looking at a button that silently did nothing (R12-07). -->
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="button-row">
      <button class="secondary" disabled={phase === 'checking'} onclick={() => void check()}
        >Check what happened</button
      ><button class="secondary" onclick={close}>Close</button>
    </div>
    {#if phase === 'uncommitted'}
      <div class="button-row">
        <button onclick={close}>Back to the report</button>
      </div>
    {/if}
  {:else if phase === 'loading'}
    <p role="status">Loading the exact request you would be reporting…</p>
  {:else if phase === 'load-failed'}
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
        explanation for reporting it. The text above and your explanation are kept as evidence for
        {preview.reviewer_count === 1
          ? 'one reviewer'
          : `up to ${preview.reviewer_count} reviewers`}
        who hold the private-evidence role, and they see the introduction, who sent it, who reported it
        and your explanation. They are not given this conversation, and cannot open it or read any message
        in it.
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
          disabled={submitDisabled(phase, busy, reason)}
          type="submit">{submitLabel(phase, busy)}</button
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
