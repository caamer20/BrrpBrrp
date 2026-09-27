<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import Icon from './Icon.svelte';

  // The account's mention policy is loaded by the workspace store, independently of
  // the conversation on screen (R8-02): this header only renders the current
  // channel's entry and edits it. While the policy is unverified the button offers
  // the account-owned load and mention interruptions stay withheld (fail-closed).
  let busy = $state(false);
  let error = $state('');
  let alive = true;
  const muted = $derived<boolean | undefined>(
    workspace.channelNotificationsReady
      ? (workspace.channelNotificationMutes[workspace.activeId] ?? false)
      : undefined
  );
  onMount(() => {
    const refresh = () => {
      if (!busy) void workspace.loadChannelNotifications();
    };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  });
  onDestroy(() => {
    alive = false;
  });
  async function toggle() {
    if (busy || workspace.authorizationUnverified) return;
    const channel = workspace.activeId;
    if (!channel) return;
    busy = true;
    error = '';
    try {
      if (muted === undefined) {
        if (!(await workspace.loadChannelNotifications()) && alive)
          error = workspace.channelNotificationError;
        return;
      }
      const next = !muted;
      if (await workspace.setChannelNotificationMuted(channel, next)) {
        if (!alive) return;
        workspace.showToast(
          next
            ? 'Mention notifications muted for this channel on all your devices. Unread counts are unchanged.'
            : 'Mention notifications restored for this channel on all your devices.'
        );
      }
      // An unapplied (stale) answer belongs to another account or authority: the
      // store already refused it, and this view shows nothing for it.
    } catch (failure) {
      if (alive) error = errorMessage(failure);
    } finally {
      if (alive) busy = false;
    }
  }
</script>

{#if workspace.activeId}
  <button
    class="icon-button"
    class:selected={muted === true}
    aria-label={muted === undefined
      ? `Load mention notification settings for #${workspace.channel?.name || 'channel'}`
      : `${muted ? 'Unmute' : 'Mute'} mention notifications for #${workspace.channel?.name || 'channel'}`}
    aria-pressed={muted}
    disabled={busy || workspace.authorizationUnverified}
    title={error
      ? `${error} Click to check again.`
      : muted
        ? 'Mentions muted on all your devices. Unread counts are unchanged.'
        : 'Mute mention notifications for this channel on all your devices.'}
    onclick={toggle}><Icon name="bell" size={18} /></button
  >
  {#if error}<span class="visually-hidden" role="status"
      >Notification setting could not be confirmed. Use the notification settings button to try
      again.</span
    >{/if}
{/if}
