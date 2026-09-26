<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, requiresFreshAuthentication } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import BackupSettings from './BackupSettings.svelte';
  interface Diagnostics {
    https_configured: boolean;
    local_only: boolean;
    base_url: string;
    voice_ip: string;
    voice_port_min: number;
    voice_port_max: number;
    turn_enabled: boolean;
    free_disk_bytes: number;
    storage_warning: string;
    backups_bytes: number;
    copy_configured: boolean;
    backup: {
      last_attempt: string;
      last_success: string;
      last_copy: string;
      snapshot_at?: string;
      id: string;
      digest?: string;
      copy_digest?: string;
      copy_attempts?: number;
      recoverable_id?: string;
      recoverable_copy_at?: string;
      error: string;
    };
  }
  let diagnostics = $state<Diagnostics | null>(null);
  interface Health {
    uptime_seconds: number;
    connections: number;
    voice_users: number;
    memory_bytes: number;
    database_bytes: number;
    uploads_bytes: number;
    messages: number;
    version: string;
  }
  interface Audit {
    id: string;
    actor: string;
    action: string;
    target_id: string;
    created_at: string;
  }
  let health = $state<Health | null>(null);
  let entries = $state<Audit[]>([]);
  let error = $state('');
  let busy = $state(false);
  let loading = $state(false);
  let includeUploads = $state(true);
  let backup = $state<{ url: string; size: number } | null>(null);
  let more = $state(false);
  // Archiving reaches every private conversation, so the server may refuse until
  // the operator confirms a recent sign-in and has enrolled a second factor.
  let needsConfirmation = $state(false);
  // Each surface inside this tab has its own capability, so an account that may
  // only read the audit log does not produce a run of unrelated 403s by opening it
  // (review R2-06). The server stays authoritative; these only decide what is asked
  // for and what is offered.
  const canViewServer = $derived(workspace.can('settings.manage'));
  const canReadAudit = $derived(workspace.can('audit.read'));
  const canBackUp = $derived(workspace.can('operator.backup'));
  function bytes(value: number) {
    if (value >= 1073741824) return `${(value / 1073741824).toFixed(1)} GB`;
    if (value < 1048576) return `${(value / 1024).toFixed(1)} KB`;
    return `${(value / 1048576).toFixed(1)} MB`;
  }
  async function refresh() {
    loading = true;
    error = '';
    try {
      if (canViewServer) {
        health = await api<Health>('/admin/health');
        diagnostics = await api('/admin/diagnostics');
      }
      if (canReadAudit) {
        const page = await api<Audit[]>('/admin/audit-logs');
        entries = page;
        more = page.length === 50;
      }
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(() => {
    void refresh();
  });
  async function createBackup() {
    busy = true;
    error = '';
    backup = null;
    needsConfirmation = false;
    try {
      // Large upload archives can take longer than an ordinary chat request.
      backup = await api(
        '/admin/backup',
        'POST',
        { include_uploads: includeUploads },
        { timeoutMs: 0 }
      );
      // A verified archive whose independent copy failed answers success with the
      // failure recorded, so the operator is told here rather than only in the next
      // diagnostics refresh.
      const warning = (backup as { warning?: string })?.warning;
      if (warning) workspace.showToast(warning);
      diagnostics = await api('/admin/diagnostics');
    } catch (e) {
      if (requiresFreshAuthentication(e)) needsConfirmation = true;
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
  async function older() {
    loading = true;
    try {
      const page = await api<Audit[]>(`/admin/audit-logs?before=${entries.at(-1)?.id}`);
      entries = [...entries, ...page];
      more = page.length === 50;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="section-heading">
  <div>
    <h3>A healthy little server</h3>
    <p>Storage, connections and a record of administrative actions.</p>
  </div>
  <button class="secondary" disabled={loading} onclick={refresh}
    >{loading ? 'Refreshing…' : 'Refresh'}</button
  >
</div>
{#if error}<p class="error-banner" role="alert">{error}</p>{/if}
{#if canViewServer && health}<div class="health-grid">
    <div><strong>{health.connections}</strong><span>Connections</span></div>
    <div><strong>{health.voice_users}</strong><span>In voice</span></div>
    <div><strong>{health.messages.toLocaleString()}</strong><span>Messages</span></div>
    <div><strong>{bytes(health.memory_bytes)}</strong><span>Go heap</span></div>
    <div><strong>{bytes(health.database_bytes)}</strong><span>Database</span></div>
    <div><strong>{bytes(health.uploads_bytes)}</strong><span>Uploads</span></div>
  </div>
  <p class="muted">
    Version {health.version} · Running for {Math.floor(health.uptime_seconds / 3600)}h {Math.floor(
      (health.uptime_seconds % 3600) / 60
    )}m
  </p>{/if}
{#if canViewServer}<section class="admin-form">
    <h3>Setup checks</h3>
    {#if diagnostics}<div class="diagnostic-list">
        <div>
          <strong
            >{diagnostics.local_only
              ? 'Local access configured'
              : diagnostics.https_configured
                ? 'HTTPS address configured'
                : 'HTTPS needs attention'}</strong
          ><small
            >{diagnostics.base_url} · {diagnostics.local_only
              ? 'This address is available on the server itself. Configure HTTPS before inviting people on other devices.'
              : 'Open this address on a second device and confirm there is no certificate warning.'}</small
          >
        </div>
        <div>
          <strong>Voice connections</strong><small
            >Media address: {diagnostics.voice_ip || 'Automatic'} · UDP {diagnostics.voice_port_min}–{diagnostics.voice_port_max}
            · Relay {diagnostics.turn_enabled ? 'enabled' : 'off'}. Confirm two-way audio from
            another device.</small
          >
        </div>
        <div>
          <strong>{bytes(diagnostics.free_disk_bytes)} available</strong><small
            >Backup archives: {bytes(diagnostics.backups_bytes)}. {diagnostics.storage_warning ||
              'Storage has room to grow.'}</small
          >
        </div>
        <div>
          <strong
            >Last successful backup: {diagnostics.backup.last_success
              ? new Date(diagnostics.backup.last_success).toLocaleString()
              : 'None yet'}</strong
          ><small
            >{diagnostics.copy_configured
              ? `Extra copy: ${diagnostics.backup.last_copy ? new Date(diagnostics.backup.last_copy).toLocaleString() : 'Not completed yet'}`
              : 'Extra copy destination is not configured. Export backups to another disk.'}</small
          >
          {#if diagnostics.backup.recoverable_id && diagnostics.backup.recoverable_id !== diagnostics.backup.id}<small
              >Latest independently recoverable generation: {new Date(
                diagnostics.backup.recoverable_copy_at ||
                  diagnostics.backup.last_copy ||
                  diagnostics.backup.last_success
              ).toLocaleString()}. The newest archive is not copied to independent storage yet, so
              it is not the recovery point for this host's loss.</small
            >{/if}
          {#if diagnostics.backup.digest}<small
              >Archive identity sha256 {diagnostics.backup.digest.slice(0, 16)}… — an extra copy is
              only recorded when it matches this archive. Check any archive with
              <code>brrpbrrp verify-backup --file ARCHIVE.zip</code>.</small
            >{/if}
          {#if diagnostics.backup.id}<a
              href={`/api/admin/backups/${diagnostics.backup.id}`}
              download>Download latest successful backup</a
            >{/if}
          {#if diagnostics.backup.error}<p class="error" role="alert">
              {diagnostics.backup.error}
            </p>{/if}
        </div>
      </div>{/if}
  </section>{/if}
{#if canViewServer}<BackupSettings />{/if}
{#if canBackUp}<section class="admin-form">
    <h3>Keep a copy</h3>
    <p>
      Backups contain accounts, password hashes and server keys. Store them privately. Restore with
      the server stopped into a new data folder.
    </p>
    <label class="check-label"
      ><input type="checkbox" bind:checked={includeUploads} />Include uploaded files</label
    >
    <button class="primary" disabled={busy} onclick={createBackup}
      >{busy ? 'Creating backup…' : 'Create backup'}</button
    >{#if needsConfirmation}<p class="notice" role="status">
        This needs an account that has enrolled a second factor and signed in recently.
        <button
          class="secondary"
          onclick={() => {
            workspace.preferencesSection = 'Account';
            workspace.settingsOpen = true;
          }}>Open account security</button
        >
      </p>{/if}{#if backup}<p class="success-note" role="status">
        Your backup is ready. <a href={backup.url} download>Download ZIP ({bytes(backup.size)})</a>
      </p>{/if}
  </section>{/if}
{#if canReadAudit}<h3 class="section-divider">Recent activity</h3>
  {#if !entries.length}<p class="muted">No recorded administrative actions.</p>{/if}
  <div class="audit-list">
    {#each entries as entry (entry.id)}<article>
        <div>
          <strong>{entry.action.replaceAll('.', ' ')}</strong><span
            >{entry.actor || 'Deleted account'}{entry.target_id
              ? ` · ${entry.target_id.slice(0, 8)}`
              : ''}</span
          >
        </div>
        <time datetime={entry.created_at}>{new Date(entry.created_at).toLocaleString()}</time>
      </article>{/each}
  </div>
  {#if more}<button class="secondary" disabled={loading} onclick={older}>Load older activity</button
    >{/if}{/if}
