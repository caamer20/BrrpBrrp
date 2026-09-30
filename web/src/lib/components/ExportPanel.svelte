<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import Modal from './Modal.svelte';

  // What the server says each export holds, and what it could not include.
  //
  // The omissions are the reason this is a list rather than a button. A member whose
  // export stopped at the server's bound needs to see that here, not discover it by
  // comparing counts against what they remember posting — and a silently truncated
  // file is worse than a refusal (ADR 0019b).
  interface ExportRecord {
    id: string;
    state: 'building' | 'ready' | 'failed';
    created_at: string;
    completed_at: string;
    conversation_count: number;
    message_count: number;
    attachment_count: number;
    bytes_written: number;
    truncated: boolean;
    omissions: string;
    expires_at: string;
  }

  let records = $state<ExportRecord[]>([]);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');
  let busy = $state('');
  let alive = true;
  let generation = 0;

  onDestroy(() => {
    alive = false;
  });

  // The list is this account's own, so an account switch must clear it before the new
  // one loads. Holding another account's export history on screen is the same class of
  // leak the export itself is careful to avoid.
  $effect(() => {
    workspace.user?.id;
    void load();
  });

  async function load() {
    const version = ++generation;
    const account = workspace.user?.id;
    records = [];
    error = notice = '';
    loading = true;
    try {
      const result = await api<ExportRecord[]>('/me/export');
      if (!alive || version !== generation || account !== workspace.user?.id) return;
      records = result;
    } catch (failure) {
      if (!alive || version !== generation) return;
      error = errorMessage(failure);
    } finally {
      if (alive && version === generation) loading = false;
    }
  }

  async function request() {
    if (busy) return;
    busy = 'request';
    error = notice = '';
    try {
      // The key is this account's and names this request. A retry is the same export
      // rather than a second copy of somebody's own history.
      const record = await api<ExportRecord>('/me/export', 'POST', {
        operation_key: crypto.randomUUID()
      });
      notice =
        record.state === 'building'
          ? 'Preparing your export. It will be ready here in a moment.'
          : 'Your export is ready.';
      await load();
    } catch (failure) {
      error = errorMessage(failure);
    } finally {
      busy = '';
    }
  }

  async function remove(record: ExportRecord) {
    if (busy || !confirm('Delete this export from the server? Your downloaded copy is unaffected.'))
      return;
    busy = record.id;
    error = notice = '';
    try {
      await api(`/me/export/${record.id}`, 'DELETE');
      if (!alive) return;
      notice = 'Export deleted from the server.';
      await load();
    } catch (failure) {
      if (alive) error = errorMessage(failure);
    } finally {
      if (alive) busy = '';
    }
  }

  function size(bytes: number) {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  function when(value: string) {
    if (!value) return '';
    return new Date(value).toLocaleString();
  }
</script>

<Modal title="Your data" close={() => (workspace.exportOpen = false)} wide>
  <div class="export-panel">
    <p class="muted">
      Download a copy of the conversations you can read, your own account details and your messages.
      It does not include anyone else's credentials, other people’s private conversations, or
      anything the server has already deleted.
    </p>

    <div class="button-row">
      <button class="primary" disabled={!!busy} onclick={() => void request()}>
        {busy === 'request' ? 'Preparing…' : 'Request an export'}
      </button>
      <button class="secondary" disabled={loading} onclick={() => void load()}>Refresh</button>
    </div>

    {#if error}<p class="error-banner" role="alert">{error}</p>{/if}
    {#if notice}<p class="success-note" role="status">{notice}</p>{/if}

    {#if loading}
      <p role="status">Loading your exports…</p>
    {:else if !records.length}
      <p class="muted">You have not requested an export yet.</p>
    {:else}
      <ul class="export-list">
        {#each records as record (record.id)}
          <li class="export-row">
            <div class="export-row-body">
              <strong>{when(record.created_at)}</strong>
              <span class="muted">
                {record.state === 'building'
                  ? 'Being prepared…'
                  : record.state === 'failed'
                    ? 'Could not be completed'
                    : `${record.conversation_count} conversations · ${record.message_count} messages · ${size(record.bytes_written)}`}
              </span>
              {#if record.truncated}
                <p class="export-warning" role="status">
                  This export stopped at the server’s limit and is incomplete. The file itself says
                  so, and the counts above are only what it reached.
                </p>
              {/if}
              {#if record.omissions}
                <p class="export-omissions">{record.omissions}</p>
              {/if}
              {#if record.expires_at && record.state === 'ready'}
                <span class="muted">Removed from the server {when(record.expires_at)}.</span>
              {/if}
            </div>
            <div class="button-row">
              {#if record.state === 'ready'}
                <a class="secondary" href={`/api/me/export/${record.id}`} download>Download</a>
              {/if}
              {#if record.state !== 'building'}
                <button class="secondary" disabled={!!busy} onclick={() => void remove(record)}>
                  Delete
                </button>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</Modal>

<style>
  .export-list {
    list-style: none;
    margin: 0.5rem 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .export-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.6rem;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.5rem 0.6rem;
  }
  .export-row-body {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }
  /* A truncated export is a fact the member has to be able to find, so it is stated in
     words rather than left to be inferred from a count. */
  .export-warning {
    color: var(--danger);
    font-size: 0.85rem;
    margin: 0.15rem 0;
  }
  .export-omissions {
    color: var(--muted);
    font-size: 0.8rem;
    margin: 0.1rem 0;
  }
</style>
