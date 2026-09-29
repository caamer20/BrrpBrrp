<script lang="ts">
  // The account's own activity list, and the private interruption preferences beside
  // it (ADR 0015).
  //
  // The list is here rather than in a notification centre because a notification is a
  // claim about the future and activity is a record of the past: it is what an
  // account reads after being away, and it is bounded and expiring rather than
  // permanent. A conversation remains the authoritative record — a gap in this list
  // is not a gap in the messages.
  import { activity } from '../activity.svelte';
  import { workspace } from '../workspace.svelte';
  import { onDestroy } from 'svelte';

  const admitted = $derived(
    !!workspace.user && workspace.privateConversationsEnabled && !workspace.authorizationUnverified
  );

  $effect(() => {
    // Refetch when the account, the authority or the private feature changes, and
    // clear the list for an account that is no longer the one it belongs to.
    void workspace.user?.id;
    void workspace.authorizationGeneration;
    if (!admitted) {
      activity.reset();
      return;
    }
    void activity.load();
    void activity.loadPreferences();
  });
  onDestroy(() => activity.destroy());

  // What a row says. It never says what was written: a private notification is the
  // one place private text most easily leaks, and a lock screen is not a place to
  // read somebody's message (ADR 0015).
  function describe(kind: string, actor: string): string {
    const who = actor || 'Someone';
    if (kind === 'request') return `${who} sent you a message request`;
    if (kind === 'group_message') return `${who} wrote in a group you are in`;
    return `${who} sent you a message`;
  }
</script>

<section aria-label="Activity">
  <h2>Activity</h2>
  <p class="muted">
    What happened while you were away, newest first. This list covers the last 30 days and keeps
    your most recent 2,000 unacknowledged items; the conversation itself is always the full record.
  </p>

  {#if !admitted}
    <p role="status">Your activity is not available right now.</p>
  {:else}
    {#if activity.error}
      <p role="alert">{activity.error}</p>
      <div class="button-row">
        <button class="secondary" onclick={() => void activity.load()}>Try again</button>
      </div>
    {/if}

    {#if activity.unacknowledged > 0}
      <div class="button-row">
        <button disabled={activity.loading} onclick={() => void activity.acknowledge()}>
          Mark {activity.unacknowledged} seen
        </button>
      </div>
    {/if}

    {#if activity.loading && activity.entries.length === 0}
      <p role="status">Loading your activity…</p>
    {:else if activity.loaded && activity.entries.length === 0}
      <p role="status">Nothing to catch up on.</p>
    {:else}
      <ul>
        {#each activity.entries as entry (entry.id)}
          <li data-acknowledged={entry.acknowledged}>
            <button
              class="channel-link"
              onclick={() => (
                (workspace.messagesTab = 'conversations'),
                (workspace.messagesOpen = true)
              )}
            >
              {describe(entry.kind, entry.actor)}
            </button>
            <small>
              {new Date(entry.created_at).toLocaleString()}
              {entry.acknowledged ? '· seen' : ''}
            </small>
          </li>
        {/each}
      </ul>
      {#if activity.cursor}
        <div class="button-row">
          <button
            class="secondary"
            disabled={activity.loading}
            onclick={() => void activity.load(true)}>Load older</button
          >
        </div>
      {/if}
    {/if}

    <h3>Notifications</h3>
    <p class="muted">
      These control interruptions only. Nothing here changes what you can read, what is unread, or
      what is kept.
    </p>
    {#each activity.preferences as preference (preference.kind)}
      <label class="toggle-row">
        <span>
          <strong
            >{preference.kind === 'incoming_request'
              ? 'Message requests'
              : preference.kind === 'group_message'
                ? 'Group messages'
                : 'Direct messages'}</strong
          >
          <small>
            {preference.muted ? 'No interruptions' : 'Interruption allowed'}
            {preference.kind === 'direct_message' || preference.kind === 'group_message'
              ? preference.show_sender
                ? '· names the sender'
                : '· does not name the sender'
              : ''}
          </small>
        </span>
        <input
          type="checkbox"
          checked={!preference.muted}
          onchange={(event) =>
            void activity.setPreference(preference.kind, { muted: !event.currentTarget.checked })}
        />
      </label>
      {#if preference.kind !== 'incoming_request'}
        <label class="toggle-row">
          <span>
            <strong>Name the sender</strong>
            <small
              >Off is the quieter default, and a lock screen is not a place to read a message.</small
            >
          </span>
          <input
            type="checkbox"
            checked={preference.show_sender}
            onchange={(event) =>
              void activity.setPreference(preference.kind, {
                show_sender: event.currentTarget.checked
              })}
          />
        </label>
      {/if}
    {/each}
  {/if}
</section>
