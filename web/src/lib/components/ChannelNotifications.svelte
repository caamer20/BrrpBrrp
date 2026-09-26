<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';
  import { api, errorMessage } from '../api';
  import { workspace } from '../workspace.svelte';
  import Icon from './Icon.svelte';

  type Preference = { channel_id: string; muted: boolean };
  let muted = $state<boolean | undefined>();
  let busy = $state(false);
  let error = $state('');
  let version = 0;
  let alive = true;
  const identity = () => ({
    account: workspace.user?.id,
    generation: workspace.authorizationGeneration,
    channel: workspace.activeId,
    version
  });
  const admitted = (request: ReturnType<typeof identity>) =>
    alive &&
    request.account === workspace.user?.id &&
    request.generation === workspace.authorizationGeneration &&
    request.channel === workspace.activeId &&
    request.version === version &&
    !workspace.authorizationUnverified;
  $effect(() => {
    workspace.user?.id;
    workspace.activeId;
    workspace.authorizationGeneration;
    workspace.connection;
    const ready = !workspace.authorizationUnverified;
    untrack(() => {
      version++;
      muted = undefined;
      workspace.channelNotificationMutes = {};
      workspace.channelNotificationsReady = false;
      error = '';
      busy = false;
      if (ready) void load();
    });
  });
  onMount(() => {
    const refresh = () => {
      if (!busy) void load();
    };
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  });
  onDestroy(() => {
    alive = false;
    version++;
  });
  async function load() {
    if (!workspace.user || !workspace.activeId || workspace.authorizationUnverified) return;
    const request = identity();
    busy = true;
    error = '';
    try {
      const preferences = await api<Preference[]>('/me/channel-notifications');
      if (admitted(request)) {
        workspace.channelNotificationMutes = Object.fromEntries(
          preferences.map((entry) => [entry.channel_id, entry.muted])
        );
        workspace.channelNotificationsReady = true;
        muted = preferences.find((entry) => entry.channel_id === request.channel)?.muted ?? false;
      }
    } catch (failure) {
      if (admitted(request)) {
        muted = undefined;
        error = errorMessage(failure);
      }
    } finally {
      if (admitted(request)) busy = false;
    }
  }
  async function toggle() {
    if (busy || workspace.authorizationUnverified) return;
    if (muted === undefined) {
      await load();
      return;
    }
    const request = identity();
    busy = true;
    error = '';
    try {
      const result = await api<Preference>(`/channels/${request.channel}/notifications`, 'PUT', {
        muted: !muted
      });
      if (!admitted(request)) return;
      muted = result.muted;
      workspace.channelNotificationMutes[result.channel_id] = result.muted;
      workspace.channelNotificationsReady = true;
      workspace.showToast(
        muted
          ? 'Mention notifications muted for this channel on all your devices. Unread counts are unchanged.'
          : 'Mention notifications restored for this channel on all your devices.'
      );
    } catch (failure) {
      if (admitted(request)) {
        muted = undefined;
        error = errorMessage(failure);
      }
    } finally {
      if (admitted(request)) busy = false;
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
