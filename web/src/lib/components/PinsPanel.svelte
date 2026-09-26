<script lang="ts">
  import { workspace } from '../workspace.svelte';
  import { api, errorMessage } from '../api';
  import type { Message } from '../types';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  let messages = $state<Message[]>([]);
  let loading = $state(true);
  let error = $state('');
  // Pins are channel content, so they follow the same authorization generation as the
  // conversation: a change clears what is shown, an answer admitted under the old
  // authority is dropped rather than displayed, and the panel asks again instead of
  // waiting on a response that will never be applied. Without that last part the panel
  // sat on its loading state forever after a generation change.
  let generation = $derived(workspace.authorizationGeneration);
  // The pins of a channel are fetched for the authorization generation they were
  // admitted under, and again whenever the panel opens, the channel changes or the
  // generation moves.
  $effect(() => {
    generation;
    load();
  });
  function load() {
    messages = [];
    error = '';
    loading = true;
    const admitted = workspace.authorizationGeneration;
    api<Message[]>(`/channels/${workspace.activeId}/pins`)
      .then((result) => {
        if (admitted !== workspace.authorizationGeneration) return;
        messages = result;
      })
      .catch((e) => {
        if (admitted !== workspace.authorizationGeneration) return;
        error = errorMessage(e);
      })
      .finally(() => {
        if (admitted === workspace.authorizationGeneration) loading = false;
      });
  }
</script>

<Modal
  title={`Pinned in #${workspace.channel?.name || 'channel'}`}
  close={() => (workspace.pinsOpen = false)}
>
  {#if loading}<p class="muted" role="status">Loading pins…</p>
  {:else if error}<div class="panel-empty">
      <p class="error" role="alert">{error}</p>
      <button class="secondary" onclick={load}>Try again</button>
    </div>
  {:else if !messages.length}<div class="panel-empty">
      <Icon name="pin" size={28} />
      <h3>Keep the good stuff close</h3>
      <p>Administrators can pin a message from its actions. It will appear here for everyone.</p>
    </div>{/if}
  <div class="search-results">
    {#each messages as message (message.id)}<button
        onclick={() => workspace.jumpToMessage(message.id)}
        ><small>{new Date(message.created_at).toLocaleString()}</small><strong
          >{workspace.state?.users.find((user) => user.id === message.user_id)?.display_name ||
            'Member'}</strong
        ><span>{message.content || 'Attachment'}</span></button
      >{/each}
  </div>
</Modal>
