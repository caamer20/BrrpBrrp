<script lang="ts">
  import { voice } from '../voice.svelte';
  import { workspace } from '../workspace.svelte';
  import { SCREEN_PRESETS, type ScreenPreset } from '../screen-share';
  import Icon from './Icon.svelte';
</script>

{#if voice.channelId || voice.status === 'requesting' || (voice.error && !voice.panelOpen)}
  <div class="voice-controls">
    {#if voice.channelId || voice.status === 'requesting'}
      <button
        class="voice-status"
        title="Open call details"
        disabled={voice.status === 'requesting'}
        onclick={() => {
          voice.selectedId = voice.channelId;
          voice.panelOpen = true;
        }}
        ><Icon name="volume" size={18} /><span
          ><strong
            >{voice.status === 'requesting'
              ? 'Waiting for microphone…'
              : voice.status === 'connected'
                ? voice.roomFor(voice.channelId)?.private
                  ? 'Call connected'
                  : 'Voice connected'
                : 'Connecting…'}</strong
          ><small>{voice.roomFor(voice.channelId || voice.selectedId)?.name ?? ''}</small></span
        ></button
      >
      <div class="voice-buttons">
        {#if voice.channelId}
          <button
            class="icon-button"
            class:danger-text={voice.muted || voice.ownState?.admin_muted}
            aria-label={voice.muted ? 'Unmute microphone' : 'Mute microphone'}
            aria-pressed={voice.muted}
            disabled={voice.ownState?.admin_muted || voice.ownState?.admin_deafened}
            onclick={() => voice.toggleMute()}
            ><Icon
              name={voice.muted || voice.ownState?.admin_muted ? 'micOff' : 'mic'}
              size={18}
            /></button
          ><button
            class="icon-button"
            class:danger-text={voice.deafened || voice.ownState?.admin_deafened}
            aria-label={voice.deafened ? 'Undeafen' : 'Deafen'}
            aria-pressed={voice.deafened}
            disabled={voice.ownState?.admin_deafened}
            onclick={() => voice.toggleDeafen()}><Icon name="headphones" size={18} /></button
          >{#if workspace.can('voice.share_screen')}
            <button
              class="icon-button"
              class:danger-text={voice.screenState === 'sharing'}
              aria-label={voice.screenState === 'sharing' ? 'Stop sharing screen' : 'Share screen'}
              aria-pressed={voice.screenState === 'sharing'}
              disabled={voice.screenState === 'asking'}
              onclick={() =>
                voice.screenState === 'sharing'
                  ? voice.stopScreenShare()
                  : voice.startScreenShare()}><Icon name="hash" size={18} /></button
            >
            <button
              class="icon-button"
              class:selected={voice.wantSystemAudio}
              aria-label="Include the sound your computer is playing"
              aria-pressed={voice.wantSystemAudio}
              title="Include the sound your computer is playing, if your browser offers it"
              onclick={() => (voice.wantSystemAudio = !voice.wantSystemAudio)}
              ><Icon name="volume" size={18} /></button
            >{/if}<button
            class="icon-button danger-text"
            aria-label={voice.roomFor(voice.channelId)?.private ? 'Leave call' : 'Leave voice'}
            onclick={() => voice.leave()}><Icon name="leave" size={18} /></button
          >
        {:else}
          <button class="secondary" onclick={() => voice.leave()}>Cancel</button>
        {/if}
      </div>
      {#if voice.screenMessage}
        <small
          class="voice-quality"
          class:screen-error={voice.screenState === 'failed' || voice.screenState === 'unsupported'}
          role="status">{voice.screenMessage}</small
        >
      {/if}
      {#if workspace.can('voice.share_screen') && voice.channelId}
        <select
          class="screen-preset"
          aria-label="Screen share quality"
          title="What the share is for: movement, or readable text"
          disabled={voice.screenState === 'asking'}
          value={voice.screenPreset}
          onchange={(event) =>
            void voice.setScreenPreset(event.currentTarget.value as ScreenPreset)}
        >
          {#each Object.values(SCREEN_PRESETS) as preset (preset.id)}
            <option value={preset.id}>{preset.label}</option>
          {/each}
        </select>
      {/if}
      {#if voice.quality}<small class="voice-quality">{voice.quality}</small>{/if}
      {#if voice.connectionPath}<small class="voice-quality" title={voice.connectionPath}
          >{voice.connectionPath}</small
        >{/if}
      {#if voice.movingTo}<small class="voice-quality" role="status">Switching rooms…</small>{/if}
      {#if voice.pushToTalk && voice.channelId}<button
          class="secondary full"
          class:talking={voice.talkHeld}
          onpointerdown={(event) => {
            event.currentTarget.setPointerCapture(event.pointerId);
            voice.holdTalk();
          }}
          onpointerup={() => voice.releaseTalk()}
          onpointercancel={() => voice.releaseTalk()}
          onlostpointercapture={() => voice.releaseTalk()}
          onkeydown={(event) => {
            if (event.key === ' ' || event.key === 'Enter') {
              event.preventDefault();
              voice.holdTalk();
            }
          }}
          onkeyup={() => voice.releaseTalk()}
          onblur={() => voice.releaseTalk()}>{voice.talkHeld ? 'Talking…' : 'Hold to talk'}</button
        >{/if}
      {#if voice.audioBlocked}<button class="secondary full" onclick={() => voice.enableAudio()}
          >Enable audio playback</button
        >{/if}
    {/if}
    {#if voice.error && !voice.panelOpen}
      <div class="voice-error-notice">
        <p class="error" role="alert">{voice.error}</p>
        {#if voice.status === 'disconnected' && workspace.state?.channels.some((channel) => channel.id === voice.selectedId && channel.type === 'voice')}
          <button class="secondary" onclick={() => voice.join(voice.selectedId)}>Retry voice</button
          >
        {/if}
        <button
          class="icon-button"
          aria-label="Dismiss voice error"
          onclick={() => (voice.error = '')}><Icon name="close" size={16} /></button
        >
      </div>
    {/if}
  </div>
{/if}

<style>
  /* A share that did not happen needs to read as an explanation rather than as a
     status, because the person is looking for why nothing changed. */
  .screen-error {
    color: var(--danger);
  }

  /* The sidebar is narrow, so the choice is one line and truncates rather than
     widening the column. It is a native select: keyboard, screen reader and phone
     behaviour come with it, and a custom control would have to rebuild all three. */
  .screen-preset {
    margin-top: 0.25rem;
    max-width: 100%;
    font: inherit;
    font-size: 0.75rem;
    padding: 0.15rem 0.25rem;
    background: var(--surface-2);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 0.375rem;
  }
</style>
