<script lang="ts">
  import { voice } from '../voice.svelte';
  import { workspace } from '../workspace.svelte';
  import Icon from './Icon.svelte';
</script>

{#if voice.channelId || voice.status === 'requesting' || (voice.error && !voice.panelOpen)}
  <div class="voice-controls">
    {#if voice.channelId || voice.status === 'requesting'}
      <button
        class="voice-status"
        title="Open voice channel details"
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
                ? 'Voice connected'
                : 'Connecting voice…'}</strong
          ><small
            >{workspace.state?.channels.find(
              (channel) => channel.id === (voice.channelId || voice.selectedId)
            )?.name}</small
          ></span
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
          ><button
            class="icon-button danger-text"
            aria-label="Leave voice"
            onclick={() => voice.leave()}><Icon name="leave" size={18} /></button
          >
        {:else}
          <button class="secondary" onclick={() => voice.leave()}>Cancel</button>
        {/if}
      </div>
      {#if voice.quality}<small class="voice-quality">{voice.quality}</small>{/if}
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
