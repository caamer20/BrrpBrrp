<script lang="ts">
  import { onDestroy } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import Icon from './Icon.svelte';
  import { voice } from '../voice.svelte';
  let busy = $state(false);
  let alive = true;
  onDestroy(() => {
    alive = false;
  });
  async function block() {
    const direct = workspace.direct;
    const account = workspace.user?.id;
    if (!direct || busy || workspace.authorizationUnverified) return;
    busy = true;
    try {
      await api(`/me/blocks/${direct.peer.id}`, direct.blocked_by_me ? 'DELETE' : 'PUT', {});
      if (!alive || account !== workspace.user?.id) return;
      await workspace.refreshState();
      if (!alive || account !== workspace.user?.id) return;
      workspace.showToast(
        direct.blocked_by_me
          ? 'Person unblocked. Recovered text will not be sent automatically.'
          : 'Person blocked. History stays readable; new messages and requests are stopped.'
      );
    } catch (failure) {
      if (alive && account === workspace.user?.id) workspace.showToast(errorMessage(failure));
    } finally {
      if (alive && account === workspace.user?.id) busy = false;
    }
  }
</script>

{#if workspace.direct}
  <button
    class="icon-button"
    aria-label="Messages and requests"
    title="Messages and requests"
    onclick={() => (workspace.messagesOpen = true)}><Icon name="users" size={18} /></button
  >{#if workspace.direct && !workspace.direct.blocked_by_me && workspace.can('voice.connect')}
    <!-- A call is available in a direct conversation, and it is the same act as
         joining a voice channel: the same capability, the same two-person bound, and
         the same membership decision. Blocked means no contact, so no call either. -->
    <button
      class="icon-button"
      aria-label={voice.channelId === workspace.direct.id ? 'Leave call' : 'Start a call'}
      aria-pressed={voice.channelId === workspace.direct.id}
      title="Start a call"
      onclick={() =>
        voice.channelId === workspace.direct?.id
          ? voice.leave()
          : voice.selectChannel(workspace.direct!.id)}><Icon name="headphones" size={18} /></button
    >{/if}
  <button
    class="secondary"
    disabled={busy || workspace.authorizationUnverified || workspace.direct.peer.is_deleted}
    onclick={block}
    title="Blocking stops new sends and requests; existing history stays readable."
    >{busy ? 'Saving…' : workspace.direct.blocked_by_me ? 'Unblock person' : 'Block person'}</button
  >
{/if}
