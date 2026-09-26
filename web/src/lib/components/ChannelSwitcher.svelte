<script lang="ts">
  import { tick } from 'svelte';
  import { workspace } from '../workspace.svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let query = $state('');
  let selected = $state(0);
  const listId = $props.id();
  const shortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ ⇧ K' : 'Ctrl Shift K';
  let results = $derived.by(() => {
    const words = query.trim().toLocaleLowerCase().replace(/^#/, '').split(/\s+/).filter(Boolean);
    return (workspace.state?.channels || [])
      .filter(
        (channel) =>
          channel.type === 'text' &&
          words.every((word) =>
            `${channel.name} ${channel.category} ${channel.topic}`
              .toLocaleLowerCase()
              .includes(word)
          )
      )
      .map((channel) => ({
        channel,
        unread: workspace.unreadFor(channel.id)
      }))
      .sort((a, b) => {
        const score = (item: typeof a) =>
          (words.length && item.channel.name.toLocaleLowerCase().startsWith(words.join(' '))
            ? 100
            : 0) +
          (item.unread?.mentions ? 20 : 0) +
          (item.unread?.count ? 10 : 0);
        return (
          score(b) - score(a) ||
          a.channel.position - b.channel.position ||
          a.channel.id.localeCompare(b.channel.id)
        );
      });
  });
  async function choose(id: string) {
    workspace.switcherOpen = false;
    await workspace.selectChannel(id);
    await tick();
    document.querySelector<HTMLTextAreaElement>('.composer textarea')?.focus();
  }
  function move(direction: number) {
    if (!results.length) return;
    selected = (selected + direction + results.length) % results.length;
    void tick().then(() =>
      document.getElementById(`${listId}-${selected}`)?.scrollIntoView({ block: 'nearest' })
    );
  }
</script>

<Modal
  title="Jump to a channel"
  close={() => (workspace.switcherOpen = false)}
  initialFocus="input"
>
  <div class="channel-switcher">
    <label for={`${listId}-query`} class="visually-hidden">Find a channel</label>
    <input
      id={`${listId}-query`}
      bind:value={query}
      placeholder="Find a channel by name, topic or category…"
      autocomplete="off"
      role="combobox"
      aria-autocomplete="list"
      aria-expanded="true"
      aria-controls={listId}
      aria-activedescendant={results[selected] ? `${listId}-${selected}` : undefined}
      oninput={() => (selected = 0)}
      onkeydown={(event) => {
        if (event.isComposing) return;
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault();
          move(event.key === 'ArrowDown' ? 1 : -1);
        }
        if (event.key === 'Enter' && results[selected]) {
          event.preventDefault();
          void choose(results[selected].channel.id);
        }
      }}
    />
    <p class="switcher-summary" role="status">
      {query
        ? `${results.length} ${results.length === 1 ? 'channel' : 'channels'} found`
        : 'Unread conversations appear first'}
    </p>
    <div id={listId} class="switcher-results" role="listbox" aria-label="Text channels">
      {#each results as { channel, unread }, index (channel.id)}<button
          id={`${listId}-${index}`}
          role="option"
          aria-selected={index === selected}
          tabindex="-1"
          class:selected={index === selected}
          onclick={() => choose(channel.id)}
          onpointermove={() => (selected = index)}
        >
          <Icon name="hash" /><span class="switcher-channel"
            ><strong>{channel.name}</strong><small
              >{channel.category || channel.topic || 'Text channel'}</small
            ></span
          >
          {#if unread?.mentions}<span class="switcher-badge"
              >{unread.mentions} {unread.mentions === 1 ? 'mention' : 'mentions'}</span
            >{:else if unread?.count}<span class="switcher-badge">{unread.count} unread</span
            >{:else if channel.id === workspace.activeId}<small>Current</small>{/if}
        </button>{/each}
    </div>
    {#if !results.length}<p class="panel-empty">
        No channels match “{query}”. Try another name or search your messages.
      </p>{/if}
    <div class="switcher-footer">
      <span><kbd>↑</kbd> <kbd>↓</kbd> to move · <kbd>Enter</kbd> to open</span><button
        class="text-button"
        onclick={() => {
          workspace.switcherOpen = false;
          workspace.searchOpen = true;
        }}>Search messages <kbd>{shortcut}</kbd></button
      >
    </div>
  </div>
</Modal>
