<script lang="ts">
  import { api, errorMessage } from '../../api';
  import { workspace } from '../../workspace.svelte';
  import type { Settings } from '../../types';
  let settings = $state<Settings>({ ...workspace.state!.settings });
  let confirm = $state('');
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let uploadMB = $state(workspace.state!.settings.max_upload_bytes / 1048576);
  const switches: { key: keyof Settings; title: string; description: string }[] = [
    {
      key: 'enable_link_previews',
      title: 'Link previews',
      description: 'Fetch titles and images for shared links.'
    },
    {
      key: 'enable_gif_embeds',
      title: 'GIF embeds',
      description: 'Allow animated previews from shared links.'
    },
    {
      key: 'enable_typing_indicators',
      title: 'Typing indicators',
      description: 'Show when someone is composing a message.'
    },
    {
      key: 'enable_presence',
      title: 'Online presence',
      description: 'Show online and idle status.'
    },
    {
      key: 'enable_voice',
      title: 'Voice channels',
      description: 'Allow live audio. Turning this off ends active calls.'
    },
    {
      key: 'push_enabled',
      title: 'Push notifications',
      description: 'Send mentions to people who have opted in.'
    },
    {
      key: 'privacy_mode_embeds_default',
      title: 'Private previews by default',
      description: 'New visitors click before loading external media.'
    },
    {
      key: 'low_resource_default',
      title: 'Low resource mode by default',
      description: 'New visitors load media on demand with less motion.'
    }
  ];
  let needsConfirm = $derived(
    settings.purge_enabled &&
      (!workspace.state?.settings.purge_enabled ||
        settings.message_retention_days < workspace.state.settings.message_retention_days)
  );
  function toggle(key: keyof Settings, checked: boolean) {
    settings = { ...settings, [key]: checked };
  }
  async function save(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    notice = '';
    try {
      settings.max_upload_bytes = Math.round(uploadMB * 1048576);
      const result = await api<Settings>('/admin/settings', 'PUT', { settings, confirm });
      if (workspace.state) workspace.state.settings = result;
      notice = 'Server settings saved.';
      confirm = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<form class="settings-form" onsubmit={save}>
  <div class="section-heading">
    <div>
      <h3>Make it yours</h3>
      <p>Balance features, privacy and the resources your server uses.</p>
    </div>
  </div>
  {#if error}<p class="error-banner" role="alert">{error}</p>{/if}{#if notice}<p
      class="success-note"
      role="status"
    >
      {notice}
    </p>{/if}
  <div class="form-grid">
    <label>Server name<input bind:value={settings.server_name} maxlength={80} required /></label
    ><label
      >Default appearance<select bind:value={settings.theme_default}
        ><option value="dark">Dark</option><option value="light">Light</option></select
      ></label
    ><label
      >Upload limit (MB)<input
        type="number"
        min={0.01}
        max={12}
        step={0.01}
        bind:value={uploadMB}
        required
      /></label
    ><label
      >Voice bitrate (kbps)<input
        type="number"
        min={8}
        max={64}
        bind:value={settings.voice_bitrate_kbps}
        required
      /></label
    >
  </div>
  {#each switches as item}<label class="setting-row"
      ><span><strong>{item.title}</strong><small>{item.description}</small></span><input
        type="checkbox"
        checked={Boolean(settings[item.key])}
        onchange={(event) => toggle(item.key, event.currentTarget.checked)}
      /></label
    >{/each}
  <!-- The audit log cannot be switched off: a community whose administrators can
       silence the record of their own authority and credential changes cannot be
       reviewed. The server refuses the change, so the interface states the rule
       rather than offering a control that would always fail (review R2-06). -->
  <div class="setting-row">
    <span
      ><strong>Audit log</strong><small
        >Always on. It records authority, credential and access changes for 90 days.</small
      ></span
    ><input type="checkbox" checked disabled aria-label="Audit log (always on)" />
  </div>
  <h3 class="section-divider">Message retention</h3>
  <label class="setting-row"
    ><span
      ><strong>Automatically delete old messages</strong><small
        >Deletion runs hourly and includes attached files. Back up first.</small
      ></span
    ><input type="checkbox" bind:checked={settings.purge_enabled} /></label
  >
  <label
    >Keep messages for (days)<input
      type="number"
      min={settings.purge_enabled ? 1 : 0}
      max={36500}
      bind:value={settings.message_retention_days}
      required
    /></label
  >
  {#if needsConfirm}<label
      >Type {workspace.state?.settings.server_name} to confirm automatic deletion<input
        bind:value={confirm}
        required
        autocomplete="off"
      /></label
    >{/if}
  <h3 class="section-divider">Voice relay</h3>
  <label class="setting-row"
    ><span
      ><strong>Built-in TURN server</strong><small
        >Helps voice through restrictive networks. Open the relay ports on your server first.</small
      ></span
    ><input type="checkbox" bind:checked={settings.enable_turn_server} /></label
  >
  <div class="form-grid">
    <label
      >Reachable IPv4 address<input
        bind:value={settings.turn_external_ip}
        placeholder="203.0.113.10"
      /></label
    ><label
      >TURN UDP port<input
        type="number"
        min={1024}
        max={65535}
        bind:value={settings.turn_port}
        required
      /></label
    >
  </div>
  <p class="muted">
    Allow UDP 49152–49999 for relay traffic and your configured media range. Disable TURN and save
    before changing its address or port.
  </p>
  <h3 class="section-divider">Network paths</h3>
  <div class="form-grid">
    <label
      >STUN server<input
        bind:value={settings.stun_server}
        placeholder="stun:stun.example.com:3478"
      /><small
        >Optional. Helps calls find a public address. Leave empty to use none — no third-party
        server is contacted unless you name one here.</small
      ></label
    >
  </div>
  <label class="setting-row"
    ><span
      ><strong>Send every call through the relay</strong><small
        >Forces relayed calls when direct connections do not work on your network. Uses more of your
        server's bandwidth and adds a little latency, so leave it off unless you need it.</small
      ></span
    ><input
      type="checkbox"
      bind:checked={settings.force_relay}
      disabled={!settings.enable_turn_server}
    /></label
  >
  {#if settings.force_relay && !settings.enable_turn_server}
    <p class="muted">
      Turn the built-in TURN server on first — there is nothing to relay through otherwise.
    </p>
  {/if}
  <div class="button-row">
    <button
      class="primary"
      disabled={busy || (needsConfirm && confirm !== workspace.state?.settings.server_name)}
      >{busy ? 'Saving…' : 'Save settings'}</button
    >
  </div>
</form>
