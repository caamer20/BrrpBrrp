<script lang="ts">
  import { tick, type Snippet } from 'svelte';
  import type { Channel } from '../types';
  import { workspace } from '../workspace.svelte';
  let { channel, children }: { channel: Channel; children: Snippet } = $props();
  let controls = $state(false);
  let handle = $state<HTMLButtonElement>();
  let start: { x: number; y: number; pointer: number } | null = null;
  let suppressClick = false;
  let siblings = $derived(
    workspace.state?.channels.filter(
      (c) => c.type === channel.type && c.category === channel.category
    ) || []
  );
  let index = $derived(siblings.findIndex((c) => c.id === channel.id));
  async function step(direction: number) {
    const target = siblings[index + direction];
    if (!target) return;
    await workspace.moveChannel(channel.id, target.id, direction > 0);
    await tick();
    handle?.focus();
  }
  function reset() {
    start = null;
    workspace.draggedChannel = '';
    workspace.channelDropTarget = '';
  }
  function pointerMove(event: PointerEvent) {
    if (!start || event.pointerId !== start.pointer) return;
    if (
      !workspace.draggedChannel &&
      Math.hypot(event.clientX - start.x, event.clientY - start.y) < 6
    )
      return;
    workspace.draggedChannel = channel.id;
    suppressClick = true;
    const target = document
      .elementFromPoint(event.clientX, event.clientY)
      ?.closest<HTMLElement>('[data-channel-order]');
    const id = target?.dataset.channelOrder;
    const candidate = workspace.state?.channels.find((c) => c.id === id);
    workspace.channelDropTarget =
      candidate && candidate.id !== channel.id && candidate.type === channel.type
        ? candidate.id
        : '';
    if (target)
      workspace.channelDropAfter =
        event.clientY > target.getBoundingClientRect().top + target.offsetHeight / 2;
    const nav = handle?.closest('.channel-navigation');
    if (nav) {
      const bounds = nav.getBoundingClientRect();
      if (event.clientY < bounds.top + 32) nav.scrollTop -= 12;
      else if (event.clientY > bounds.bottom - 32) nav.scrollTop += 12;
    }
  }
  function pointerUp(event: PointerEvent) {
    if (!start || event.pointerId !== start.pointer) return;
    const target = workspace.channelDropTarget;
    const after = workspace.channelDropAfter;
    if (workspace.draggedChannel && target) void workspace.moveChannel(channel.id, target, after);
    reset();
  }
</script>

<div
  class="channel-order"
  data-channel-order={channel.id}
  class:drop-before={workspace.channelDropTarget === channel.id && !workspace.channelDropAfter}
  class:drop-after={workspace.channelDropTarget === channel.id && workspace.channelDropAfter}
  class:dragging={workspace.draggedChannel === channel.id}
>
  <div class="channel-order-row">
    {@render children()}
    {#if workspace.can('channel.manage')}
      <button
        bind:this={handle}
        class="channel-grip"
        disabled={workspace.channelMoving}
        aria-label={`Reorder ${channel.name}`}
        aria-expanded={controls}
        title="Drag to reorder or click for move controls. Alt + ↑ / ↓ to move."
        onclick={() => {
          if (suppressClick) {
            suppressClick = false;
            return;
          }
          controls = !controls;
        }}
        onkeydown={(event) => {
          if (event.key === 'Escape') {
            reset();
            controls = false;
          }
          if (event.altKey && ['ArrowUp', 'ArrowDown'].includes(event.key)) {
            event.preventDefault();
            void step(event.key === 'ArrowUp' ? -1 : 1);
          }
        }}
        onpointerdown={(event) => {
          if (event.button !== 0 || workspace.channelMoving) return;
          suppressClick = false;
          start = { x: event.clientX, y: event.clientY, pointer: event.pointerId };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onpointermove={pointerMove}
        onpointerup={pointerUp}
        onpointercancel={reset}
        onlostpointercapture={reset}>⠿</button
      >
    {/if}
  </div>
  {#if controls && workspace.can('channel.manage')}
    <div class="channel-move-controls">
      <button
        disabled={index <= 0 || workspace.channelMoving}
        onclick={() => step(-1)}
        aria-label={`Move ${channel.name} up`}>↑ Move up</button
      >
      <button
        disabled={index >= siblings.length - 1 || workspace.channelMoving}
        onclick={() => step(1)}
        aria-label={`Move ${channel.name} down`}>↓ Move down</button
      >
    </div>
  {/if}
</div>
