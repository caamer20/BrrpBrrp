<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../../api';
  import type { Settings } from '../../types';
  let settings = $state<Settings | null>(null);
  let busy = $state(false);
  let error = $state('');
  let saved = $state(false);
  onMount(() => {
    api<Settings>('/admin/settings')
      .then((value) => (settings = value))
      .catch((e) => (error = errorMessage(e)));
  });
  async function save(event: SubmitEvent) {
    event.preventDefault();
    if (!settings) return;
    busy = true;
    saved = false;
    error = '';
    try {
      const current = await api<Settings>('/admin/settings');
      await api('/admin/settings', 'PUT', {
        settings: {
          ...current,
          backup_enabled: settings.backup_enabled,
          backup_interval_hours: settings.backup_interval_hours,
          backup_keep: settings.backup_keep,
          backup_include_uploads: settings.backup_include_uploads
        },
        confirm: ''
      });
      saved = true;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if settings}<form class="admin-form" id="backup-schedule" onsubmit={save}>
    <h3>Automatic backups</h3>
    <label class="check-label"
      ><input type="checkbox" bind:checked={settings.backup_enabled} />Back up on a schedule</label
    >
    <div class="form-grid">
      <label
        >Every (hours)<input
          type="number"
          min="1"
          max="168"
          required
          bind:value={settings.backup_interval_hours}
        /></label
      ><label
        >Backups to keep<input
          type="number"
          min="1"
          max="30"
          required
          bind:value={settings.backup_keep}
        /></label
      >
    </div>
    <label class="check-label"
      ><input type="checkbox" bind:checked={settings.backup_include_uploads} />Include uploaded
      files in scheduled backups</label
    >
    <p class="muted">
      The first scheduled backup runs within a minute when none has been attempted. After each
      successful backup, older app-created archives are removed to keep this many. Export copies you
      want to keep longer.
    </p>
    <button class="secondary" disabled={busy}>{busy ? 'Saving…' : 'Save backup schedule'}</button>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if saved}<p class="success-note" role="status">Backup schedule saved.</p>{/if}
  </form>{/if}
