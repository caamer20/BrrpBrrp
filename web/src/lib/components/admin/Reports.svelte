<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import type { MessageReport } from '../../types';

  type Scope = { account: string; generation: number; status: string; version: number };
  let reports = $state<MessageReport[]>([]);
  let status = $state<'open' | 'resolved'>('open');
  let loading = $state(false);
  let error = $state('');
  let notice = $state('');
  let cursor = $state('');
  let busy = $state('');
  let notes = $state<Record<string, string>>({});
  let loadedFor = $state<Scope | null>(null);
  let version = 0;
  let alive = true;
  onDestroy(() => {
    alive = false;
    version++;
  });
  const admitted = (request: Scope) =>
    alive &&
    request.version === version &&
    request.account === workspace.user?.id &&
    request.generation === workspace.authorizationGeneration &&
    request.status === status &&
    workspace.can('message.delete_any') &&
    !workspace.authorizationUnverified;
  const visible = $derived(
    loadedFor && admitted(loadedFor)
      ? reports.filter((report) =>
          workspace.state?.channels.some((channel) => channel.id === report.channel_id)
        )
      : []
  );
  const memberName = (id: string) => {
    const user = workspace.state?.users.find((user) => user.id === id);
    return user?.display_name || user?.username || 'Deleted member';
  };
  const channelName = (id: string) =>
    workspace.state?.channels.find((channel) => channel.id === id)?.name || 'channel';
  $effect(() => {
    workspace.user?.id;
    workspace.authorizationGeneration;
    const permitted = workspace.can('message.delete_any') && !workspace.authorizationUnverified;
    status;
    untrack(() => {
      version++;
      reports = [];
      loadedFor = null;
      notes = {};
      error = notice = cursor = busy = '';
      loading = false;
      if (permitted) void load();
    });
  });
  async function load(older = false) {
    if (
      loading ||
      !workspace.user ||
      !workspace.can('message.delete_any') ||
      workspace.authorizationUnverified
    )
      return;
    const request: Scope = {
      account: workspace.user.id,
      generation: workspace.authorizationGeneration,
      status,
      version
    };
    loading = true;
    error = '';
    if (!older) {
      reports = [];
      cursor = '';
    }
    try {
      const query = new URLSearchParams({ status: request.status });
      if (older && cursor) query.set('before', cursor);
      const result = await api<{ reports: MessageReport[]; next_cursor: string }>(
        `/admin/reports?${query}`
      );
      if (!admitted(request)) return;
      reports = older
        ? [
            ...reports,
            ...result.reports.filter((report) => !reports.some((item) => item.id === report.id))
          ]
        : result.reports;
      cursor = result.next_cursor;
      loadedFor = request;
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) loading = false;
    }
  }
  const reviewers = $derived(
    (workspace.state?.users ?? []).filter((member) => workspace.can('message.delete_any'))
  );

  async function assign(report: MessageReport, assignee: string) {
    if (busy || !loadedFor || !admitted(loadedFor)) return;
    const request = loadedFor;
    busy = report.id;
    error = notice = '';
    try {
      const updated = await api<MessageReport>(`/admin/reports/${report.id}/assign`, 'POST', {
        assignee_id: assignee
      });
      if (!admitted(request)) return;
      reports = reports.map((item) => (item.id === updated.id ? updated : item));
      notice = updated.assigned_to
        ? `Assigned to ${memberName(updated.assigned_to)}. They still need permission to review.`
        : 'Assignment cleared.';
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) busy = '';
    }
  }

  async function review(report: MessageReport) {
    if (busy || !loadedFor || !admitted(loadedFor)) return;
    const request = loadedFor;
    busy = report.id;
    error = notice = '';
    try {
      await api(`/admin/reports/${report.id}`, 'PATCH', {
        status: status === 'open' ? 'resolved' : 'open',
        note: (notes[report.id] ?? report.review_note).trim()
      });
      if (!admitted(request)) return;
      reports = reports.filter((item) => item.id !== report.id);
      delete notes[report.id];
      notice =
        status === 'open'
          ? 'Report resolved. No action was taken against the account or message.'
          : 'Report reopened.';
    } catch (failure) {
      if (admitted(request)) error = errorMessage(failure);
    } finally {
      if (admitted(request)) busy = '';
    }
  }
</script>

<div class="section-heading">
  <div>
    <h3>Community reports</h3>
    <p>
      Review messages reported by your community. You can only see reports from channels you can
      read.
    </p>
  </div>
</div>
<label
  >Report status<select bind:value={status}
    ><option value="open">Open</option><option value="resolved">Resolved</option></select
  ></label
>
<p class="muted">
  Evidence remains available for 90 days. Resolving a report records your review; it does not remove
  messages or restrict accounts.
</p>
{#if workspace.authorizationUnverified}<p role="status">
    Checking your access before showing reports…
  </p>
{:else if !workspace.can('message.delete_any')}<p role="status">
    You no longer have permission to review reports.
  </p>
{:else}
  {#if error}<p class="error-banner" role="alert">{error}</p>
    <button class="secondary" disabled={loading} onclick={() => load()}>Retry report loading</button
    >{/if}
  {#if notice}<p class="success-note" role="status">{notice}</p>{/if}
  {#if loading}<p role="status">Loading reports…</p>{:else if !visible.length && !error}<p
      role="status"
    >
      {status === 'open'
        ? 'No open reports in the channels you can read.'
        : 'No resolved reports in the channels you can read.'}
    </p>{/if}
  {#each visible as report (report.id)}
    <article class="report-card" aria-label={`Report in ${channelName(report.channel_id)}`}>
      <h4>#{channelName(report.channel_id)} · {memberName(report.author_id)}</h4>
      <p class="muted">
        Reported by {memberName(report.reporter_id)} ·
        <time datetime={report.created_at}>{new Date(report.created_at).toLocaleString()}</time>
      </p>
      <h5>Reported message text</h5>
      <blockquote>
        {report.snapshot_text || 'No message text was included. Files are not stored in reports.'}
      </blockquote>
      <h5>Reporter’s explanation</h5>
      <p class="report-text">{report.reason}</p>
      <p class="muted">
        Evidence available until {new Date(report.expires_at).toLocaleDateString()}. The original
        message may have changed or been deleted.
      </p>
      <div class="report-assignment">
        <label
          >Assigned to<select
            value={report.assigned_to ?? ''}
            disabled={!!busy}
            onchange={(event) => void assign(report, event.currentTarget.value)}
          >
            <option value="">Nobody yet</option>
            {#each reviewers as member (member.id)}
              <option value={member.id}>{member.display_name || member.username}</option>
            {/each}
          </select></label
        >
        <small
          >Assigning records who is expected to look at this report. It does not give them
          permission, and it does not stop anyone else who can already review it.</small
        >
      </div>
      <form
        onsubmit={(event) => {
          event.preventDefault();
          void review(report);
        }}
      >
        <label
          >Moderator note (optional)<textarea
            rows="3"
            maxlength="2000"
            value={notes[report.id] ?? report.review_note}
            oninput={(event) => (notes[report.id] = event.currentTarget.value)}></textarea><small
            >Visible to moderators who can read this channel.</small
          ></label
        >
        <button class="primary" disabled={!!busy}
          >{busy === report.id
            ? 'Saving review…'
            : status === 'open'
              ? 'Resolve report'
              : 'Reopen report'}</button
        >
      </form>
    </article>
  {/each}
  <div class="button-row">
    <button class="secondary" disabled={loading || !!busy} onclick={() => load()}
      >Refresh reports</button
    >{#if cursor}<button class="secondary" disabled={loading || !!busy} onclick={() => load(true)}
        >Load older reports</button
      >{/if}
  </div>
{/if}

<style>
  .report-card {
    margin: 16px 0;
    padding: 16px;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  h4,
  h5 {
    margin: 12px 0 6px;
  }
  blockquote {
    margin: 8px 0;
    padding: 12px;
    border-left: 3px solid var(--border);
  }
  blockquote,
  .report-text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .report-assignment {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    margin: 0.5rem 0;
  }
  .report-assignment small {
    color: var(--muted);
  }
</style>
