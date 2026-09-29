<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import Icon from '../Icon.svelte';
  import { beforeParam, mergeEvidence } from '../../private-evidence-merge';

  // The private evidence queue (ADR 0013).
  //
  // It shows submitted snapshots and nothing else. There is deliberately no link
  // to the conversation a message came from, no author conversation, and no route
  // out of here into the reporter's or the author's private messages: this is a
  // record of what someone chose to disclose, not a moderation console over
  // private conversations. A reviewer with this capability who is not a participant
  // has no way to become one from this screen.
  // Both kinds of private evidence appear here: a reported message and a reported
  // request introduction. They share a capability, a policy, an expiry and a
  // cleanup, so a reviewer should not have to learn two interfaces — but the source
  // is labelled, because the two are different facts about different people, and
  // neither carries a conversation the reviewer could open.
  interface Evidence {
    id: string;
    source_kind: 'message' | 'request';
    author_id: string;
    requester_id: string;
    reporter_id: string;
    reason: string;
    snapshot_text: string;
    status: string;
    review_note: string;
    reviewed_by: string;
    created_at: string;
    expires_at: string;
  }
  // The account and generation are read per request, below. Capturing them once
  // here is what made every read after an authority change permanently
  // inadmissible (completion review 10, R10-04).
  let alive = true;
  onDestroy(() => {
    alive = false;
  });

  let status = $state<'open' | 'resolved'>('open');
  let reports = $state<Evidence[]>([]);
  // Each source carries its own cursor and its own exhaustion, so one running
  // out does not truncate or rewind the other (R12-08).
  let messageExhausted = $state(false);
  let requestExhausted = $state(false);
  let done = $state(false);
  // Each source paginates on its own ids, so each carries its own cursor.
  let messageCursor = $state('');
  let requestCursor = $state('');
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let notes = $state<Record<string, string>>({});
  // busy names the record whose review is in flight, and it is released by the
  // operation that claimed it — not by whatever the list happens to be showing.
  let busy = $state('');
  // listVersion sequences *reads only*. A mutation must not advance it: it did not
  // issue a read, and if it did, any read issued meanwhile would fail admission
  // against a version it had moved, and a held review's own cleanup would fail
  // against a version the refresh had moved — leaving the panel permanently busy
  // and every later action silently refused (completion review 11, R11-01).
  let listVersion = 0;
  let loadedFor = $state('');

  // Identity without list sequencing. A read is admitted only if it is still the
  // newest read; a mutation is admitted while the account, its generation and its
  // capability still hold, because it will re-read rather than patch a view in
  // place — so a concurrent refresh can never be overwritten by a stale result.
  const current = () => ({
    account: workspace.user?.id,
    generation: workspace.authorizationGeneration
  });
  const admittedAccount = (request: { account?: string; generation: number }) =>
    alive &&
    request.account === workspace.user?.id &&
    request.generation === workspace.authorizationGeneration &&
    workspace.can('report.review_private') &&
    !workspace.authorizationUnverified;
  const admittedRead = (request: { account?: string; generation: number; version: number }) =>
    admittedAccount(request) && request.version === listVersion;

  // The identity is captured per request, not once when the panel mounted. A
  // capture at construction meant every read issued after an unrelated authority
  // change compared against a stale generation, was refused, and could never be
  // admitted again — so the queue could not refresh at all once the account's
  // generation moved, and Refresh could not repair it (completion review 10,
  // R10-04).
  async function load(older = false) {
    const request = { ...current(), version: ++listVersion };
    loading = !older;
    error = '';
    if (!older) reports = [];
    try {
      // Both queues are read and merged so the reviewer sees one list. Each source
      // paginates on its own ids, so each keeps its own cursor.
      // An exhausted source is not asked again. Asking it with an empty cursor
      // returned its first page a second time, so "load older" duplicated rows the
      // reviewer had already read whenever one source ran out before the other.
      const [messages, requests] = await Promise.all([
        api<{ reports: Evidence[]; next_cursor: string }>(
          `/admin/private-reports?status=${status}${older ? beforeParam(messageCursor, messageExhausted) : ''}`
        ),
        api<{ reports: Evidence[]; next_cursor: string }>(
          `/admin/request-reports?status=${status}${older ? beforeParam(requestCursor, requestExhausted) : ''}`
        )
      ]);
      if (!admittedRead(request)) return;
      const merged = mergeEvidence(older ? reports : [], messages, requests, {
        message: messageCursor,
        request: requestCursor
      });
      reports = merged.rows;
      messageCursor = merged.messageCursor;
      requestCursor = merged.requestCursor;
      messageExhausted = merged.messageExhausted;
      requestExhausted = merged.requestExhausted;
      // The list is finished only when both sources are, not when one is.
      done = merged.done;
      loadedFor = `${request.account}:${request.generation}:${status}`;
      for (const report of merged.rows) notes[report.id] ??= report.review_note;
    } catch (failure) {
      if (admittedRead(request)) error = errorMessage(failure);
    } finally {
      if (admittedRead(request)) loading = false;
    }
  }
  // The gate is checked here, not only in the route. An API refusal alone is not
  // the interface contract: with the private feature off there is no evidence to
  // review, so the queue must not offer itself, and whatever it was showing must
  // be cleared rather than left on screen (completion review 10, R10-07).
  $effect(() => {
    void status;
    void workspace.authorizationGeneration;
    void workspace.privateConversationsEnabled;
    if (
      workspace.privateConversationsEnabled &&
      workspace.can('report.review_private') &&
      !workspace.authorizationUnverified
    )
      void load();
    else {
      reports = [];
      loadedFor = '';
    }
  });

  async function review(report: Evidence, next: 'open' | 'resolved') {
    if (busy) return;
    const request = current();
    // The two kinds are separate stores with separate endpoints, so the review goes
    // to the one this record actually lives in. Guessing by id would send a
    // request review into the message queue.
    const path =
      report.source_kind === 'request'
        ? `/admin/request-reports/${report.id}`
        : `/admin/private-reports/${report.id}`;
    const claimed = report.id;
    busy = claimed;
    error = '';
    notice = '';
    try {
      await api<Evidence>(path, 'PATCH', {
        status: next,
        note: (notes[report.id] ?? '').trim()
      });
      if (!admittedAccount(request)) return;
      // A resolve moves the record out of this view; a reopen brings it back. The
      // queue is re-read rather than patched in place, so a refresh that landed
      // while this was in flight is not overwritten by a stale removal, and a
      // filter that no longer admits the record cannot leave it stranded.
      notice = next === 'resolved' ? 'Marked as reviewed.' : 'Reopened for review.';
      void load();
    } catch (failure) {
      if (admittedAccount(request)) error = errorMessage(failure);
    } finally {
      // Released by the operation that claimed it, and only then. Tying this to the
      // list's admission — as it used to be — is what let a concurrent refresh
      // strand the flag and refuse every later review (completion review 11,
      // R11-01).
      if (busy === claimed) busy = '';
    }
  }

  const who = (id: string) =>
    workspace.userFor(id)?.display_name ||
    workspace.userFor(id)?.username ||
    (id ? 'Former member' : 'Unknown');
  const day = (value: string) => new Date(value).toLocaleDateString();
</script>

<section aria-label="Private message evidence">
  <h3>Private message evidence</h3>
  <p class="muted">
    These are messages people chose to report from their own direct conversations. You see only the
    text below, who reported it and their explanation. You do not get access to the conversation,
    the rest of it, its files or either person's messages. Reviewing records what you concluded; it
    does not delete anything, block anyone, or give you any power over that conversation.
  </p>
  <label
    >Evidence status<select bind:value={status}>
      <option value="open">Open</option>
      <option value="resolved">Reviewed</option>
    </select></label
  >
  {#if loading}<p role="status">Loading submitted evidence…</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
  {#if notice}<p role="status">{notice}</p>{/if}
  {#if !loading && !error && !reports.length}
    <p class="muted" role="status">
      No {status === 'open' ? 'open' : 'reviewed'} private evidence.
    </p>
  {/if}
  {#if loadedFor && `${workspace.user?.id}:${workspace.authorizationGeneration}:${status}` !== loadedFor && !error}
    <p role="alert">Your access to this queue changed. Reload to see what you may still read.</p>
  {/if}
  {#each reports as report (report.id)}
    <article
      class="report-card"
      aria-label={`${report.source_kind === 'request' ? 'Request introduction' : 'Message'} reported by ${who(report.reporter_id)}`}
    >
      <h4>
        {#if report.source_kind === 'request'}
          Request introduction from {who(report.requester_id)} · reported by {who(
            report.reporter_id
          )}
        {:else}
          Message from {who(report.author_id)} · reported by {who(report.reporter_id)}
        {/if}
      </h4>
      <p class="muted">
        {#if report.source_kind === 'request'}
          The sender asked to start a direct conversation. The request was declined and the sender
          blocked at the time this was reported.
        {/if}
        Reported {day(report.created_at)}. Evidence is available until {day(report.expires_at)}.
      </p>
      <h5>
        {report.source_kind === 'request' ? 'Requested introduction' : 'Reported message text'}
      </h5>
      <blockquote>
        {report.snapshot_text ||
          (report.source_kind === 'request'
            ? 'This request had no introduction.'
            : 'This message had no text.')}
      </blockquote>
      <h5>The reporter's explanation</h5>
      <p class="report-text">{report.reason}</p>
      <label
        >Review note (optional)<textarea bind:value={notes[report.id]} maxlength="2000" rows="2"
        ></textarea></label
      >
      <div class="button-row">
        {#if report.status === 'open'}<button
            class="secondary"
            disabled={busy === report.id}
            onclick={() => void review(report, 'resolved')}
            ><Icon name="check" />Mark reviewed</button
          >{:else}<button
            class="secondary"
            disabled={busy === report.id}
            onclick={() => void review(report, 'open')}><Icon name="undo" />Reopen</button
          >{/if}
      </div>
      {#if report.reviewed_by}
        <p class="muted">Last reviewed by {who(report.reviewed_by)}.</p>
      {/if}
    </article>
  {/each}
  <div class="button-row">
    <button class="secondary" onclick={() => void load()}>Refresh</button>{#if !done}<button
        class="secondary"
        onclick={() => void load(true)}>Load older</button
      >{/if}
  </div>
</section>

<style>
  blockquote {
    border-inline-start: 3px solid var(--accent);
    margin: 0.5rem 0;
    padding-inline-start: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .report-card {
    border: 1px solid var(--border);
    border-radius: 8px;
    margin-block: 0.75rem;
    padding: 0.75rem;
  }
</style>
