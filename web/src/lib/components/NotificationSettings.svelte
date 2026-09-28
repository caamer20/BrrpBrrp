<script lang="ts">
  import { onMount } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { errorMessage } from '../api';
  import {
    notificationStatus,
    enableNotifications,
    disableNotifications,
    type NotificationStatus
  } from '../notifications';
  let status = $state<NotificationStatus>();
  let busy = $state(false);
  let error = $state('');
  async function refresh() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      status = await notificationStatus(!!workspace.state?.settings.push_enabled);
    } catch (failure) {
      error = errorMessage(failure);
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    void refresh();
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  });
  async function toggle() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      if (status?.state === 'enabled') await disableNotifications();
      else {
        if (status?.state === 'unlinked') await disableNotifications();
        await enableNotifications(workspace.state!.push_public_key);
      }
      busy = false;
      await refresh();
    } catch (failure) {
      error = errorMessage(failure);
    } finally {
      busy = false;
    }
  }
</script>

<section class="settings-section">
  <h3>Mention notifications</h3>
  <p class="muted">These settings apply to this browser and device.</p>
  {#if workspace.privateConversationsEnabled}
    <p class="muted">
      These notifications are for mentions in channels. Direct messages do not use them: a new
      message request is shown as a count on <strong>Messages and requests</strong>, and an unread
      direct message is shown there and in the conversation itself. Nothing rings, pops up or
      reaches this device for a direct message. Turning these off does not change either.
    </p>
  {/if}
  {#if status}<div
      class="notification-status"
      class:enabled={status.state === 'enabled'}
      role="status"
    >
      <strong>{status.title}</strong>
      <p>{status.detail}</p>
    </div>
  {:else if !error}<p role="status" class="muted">Checking this device…</p>{/if}
  {#if error}<p role="alert" class="error">{error}</p>{/if}
  <div class="button-row">
    <button class="secondary" onclick={refresh} disabled={busy}>Check again</button>
    {#if status && ['enabled', 'off', 'unlinked'].includes(status.state)}<button
        class="primary"
        onclick={toggle}
        disabled={busy}
        >{busy
          ? 'One moment…'
          : status.state === 'enabled'
            ? 'Turn off on this device'
            : status.state === 'unlinked'
              ? 'Reconnect this device'
              : 'Enable on this device'}</button
      >{/if}
  </div>
</section>
