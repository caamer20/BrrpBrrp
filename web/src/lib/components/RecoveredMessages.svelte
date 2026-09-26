<script lang="ts">
  import { workspace } from '../workspace.svelte';
  import type { OutboxEntry } from '../outbox';
  let copied = $state('');
  let copyError = $state('');
  let container = $state<HTMLElement>();

  async function copy(entry: OutboxEntry) {
    const account = workspace.user?.id;
    try {
      await navigator.clipboard.writeText(entry.content);
      if (workspace.user?.id !== account) return;
      copied = entry.key;
      copyError = '';
    } catch {
      if (workspace.user?.id !== account) return;
      copied = '';
      copyError = 'Select the text below and copy it with your device’s copy command.';
      const field = container?.querySelector<HTMLTextAreaElement>(`[data-recovery="${entry.key}"]`);
      field?.focus();
      field?.select();
    }
  }
</script>

{#if workspace.recoveredMessages.length}
  <details class="recovered-messages" open bind:this={container}>
    <summary>Recovered messages ({workspace.recoveredMessages.length})</summary>
    <p class="muted">
      These messages will not retry. Copy your text and check the conversation before choosing to
      send it again.
    </p>
    {#if !workspace.saveDrafts}<p class="muted">
        Text is kept only until you leave this session. Copy anything you want to keep.
      </p>{:else}<p class="muted">
        Recovered text is saved on this device for up to one day. Signing out clears it.
      </p>{/if}
    {#each workspace.recoveredMessages as entry (entry.key)}
      <section aria-label="Recovered message">
        <p>{entry.recovery}</p>
        {#if entry.content}
          <textarea
            aria-label="Recovered message text"
            data-recovery={entry.key}
            readonly
            value={entry.content}
            rows="3"></textarea>
        {:else}<p class="muted">
            This message contained only attachments. Attach the files again if you choose to send a
            new message.
          </p>{/if}
        <div class="recovery-actions">
          {#if entry.content}<button class="secondary" onclick={() => copy(entry)}>Copy text</button
            >{/if}
          <button class="secondary" onclick={() => workspace.dismissRecovery(entry.key)}
            >Dismiss recovered message</button
          >
        </div>
        {#if copied === entry.key}<p role="status">Text copied.</p>{/if}
      </section>
    {/each}
    {#if copyError}<p role="status">{copyError}</p>{/if}
  </details>
{/if}

<style>
  .recovered-messages {
    flex-shrink: 0;
    max-height: 35vh;
    overflow: auto;
    margin: 8px 16px;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  summary {
    cursor: pointer;
    font-weight: 600;
  }
  p {
    margin: 8px 0;
  }
  section + section {
    border-top: 1px solid var(--border);
    margin-top: 12px;
    padding-top: 8px;
  }
  textarea {
    width: 100%;
    resize: vertical;
  }
  .recovery-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 8px;
  }
</style>
