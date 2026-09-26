<script lang="ts">
  import { onDestroy } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import Icon from './Icon.svelte';
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
  >
  <button
    class="secondary"
    disabled={busy || workspace.authorizationUnverified || workspace.direct.peer.is_deleted}
    onclick={block}
    title="Blocking stops new sends and requests; existing history stays readable."
    >{busy ? 'Saving…' : workspace.direct.blocked_by_me ? 'Unblock person' : 'Block person'}</button
  >
{/if}
