<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import Icon from '../Icon.svelte';

  // The private evidence queue (ADR 0013).
  //
  // It shows submitted snapshots and nothing else. There is deliberately no link
  // to the conversation a message came from, no author conversation, and no route
  // out of here into the reporter's or the author's private messages: this is a
  // record of what someone chose to disclose, not a moderation console over
  // private conversations. A reviewer with this capability who is not a participant
  // has no way to become one from this screen.
  interface Evidence {
    id: string;
    message_id: string;
    conversation_id: string;
    reporter_id: string;
    author_id: string;
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
  let cursor = $state('');
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let notes = $state<Record<string, string>>({});
  let busy = $state('');
  let version = 0;
  let loadedFor = $state('');

  // The grant is re-checked on every read. Losing it must empty this view rather
  // than leave evidence on screen that the account may no longer read, which is
  // the same rule the community queue follows.
  const admitted = (request: { account?: string; generation: number; version: number }) =>
    alive &&
    request.account === workspace.user?.id &&
    request.generation === workspace.authorizationGeneration &&
    request.version === version &&
    workspace.can('report.review_private') &&
    !workspace.authorizationUnverified;

  // The identity is captured per request, not once when the panel mounted. A
  // capture at construction meant every read issued after an unrelated authority
  // change compared against a stale generation, was refused, and could never be
  // admitted again — so the queue could not refresh at all once the account's
  // generation moved, and Refresh could not repair it (completion review 10,
  // R10-04).
  async function load(older = false) {
    const request = {
      account: workspace.user?.id,
      generation: workspace.authorizationGeneration,
      version: ++version
    };
    loading = !older;
    error = '';
    if (!older) reports = [];
    try {
      const result = await api<{ reports: Evidence[]; next_cursor: string }>(
        `/admin/private-reports?status=${status}${older && cursor ? `&before=${cursor}` : ''}`
      );
      if (!admitted(request)) return;
      reports = older ? [...reports, ...result.reports] : result.reports;
      cursor = result.next_cursor;
      loadedFor = `${request.account}:${request.generation}:${status}`;
      for (const report of result.reports) notes[report.id] ??= report.review_note;
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) loading = false;
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
    // The mutation is admitted the same way the read is, against the identity
    // captured when it started: a response arriving after the account or its
    // authority changed belongs to a view that no longer exists, and applying it
    // would edit a queue the account may no longer be allowed to see.
    const request = {
      account: workspace.user?.id,
      generation: workspace.authorizationGeneration,
      version: ++version
    };
    busy = report.id;
    error = '';
    notice = '';
    try {
      await api<Evidence>(`/admin/private-reports/${report.id}`, 'PATCH', {
        status: next,
        note: (notes[report.id] ?? '').trim()
      });
      if (!admitted(request)) return;
      // A resolve moves the record out of this view; a reopen brings it back. The
      // queue is re-read rather than patched in place, so a filter that no longer
      // admits the record cannot leave it stranded.
      reports = reports.filter((entry) => entry.id !== report.id);
      notice = next === 'resolved' ? 'Marked as reviewed.' : 'Reopened for review.';
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) busy = '';
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
      aria-label={`Private evidence reported by ${who(report.reporter_id)}`}
    >
      <h4>From {who(report.author_id)} · reported by {who(report.reporter_id)}</h4>
      <p class="muted">
        Reported {day(report.created_at)}. Evidence is available until {day(report.expires_at)}.
      </p>
      <h5>Reported message text</h5>
      <blockquote>{report.snapshot_text || 'This message had no text.'}</blockquote>
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
    <button class="secondary" onclick={() => void load()}>Refresh</button>{#if cursor}<button
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
