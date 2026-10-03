<script lang="ts" module>
  // The workspace places one screen element for the whole app, and the voice model
  // needs it: the action hands the element over and tells the model a stream may now be
  // waiting for it. A share can arrive before this element exists — on a slow join it
  // usually does — and a plain binding attached the stream only when the stream
  // arrived, so the first share of a call rendered a blank rectangle with no error.
  export function screenTarget(
    node: HTMLVideoElement,
    voiceModel: { bindScreenElement: (element: HTMLVideoElement | null) => void }
  ) {
    voiceModel.bindScreenElement(node);
    return {
      update(next: typeof voiceModel) {
        next.bindScreenElement(node);
      },
      destroy() {
        voiceModel.bindScreenElement(null);
      }
    };
  }
</script>

<script lang="ts">
  import { voice } from '../voice.svelte';
  import Icon from './Icon.svelte';

  // The shared screen and the controls a viewer gets over it. The picture itself is
  // muted and inline: a video element that tried to unmute would play the sharer's
  // system audio at everybody watching, and inline keeps a phone from going fullscreen
  // on its own.
  //
  // Audio-only disables the inbound video tracks rather than hiding the element (see
  // screen-receive.ts): hiding it would keep decoding a picture nobody asked to see.
  // The element stays mounted in both states, because the control that brings the
  // picture back has to remain reachable — that is the one thing an audio-only view
  // must not take away.
  let stage: HTMLElement;
  let fullscreen = $state(false);

  async function toggleFullscreen() {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else {
        // Asking for the whole picture is an explicit request to see it, so a viewer who
        // was saving data gets the picture back rather than a black screen in fullscreen.
        if (voice.screenAudioOnly) voice.setScreenAudioOnly(false);
        await stage.requestFullscreen();
      }
    } catch {
      /* A browser that refuses fullscreen leaves the corner view in place. */
    }
  }

  function trackFullscreen(node: HTMLElement) {
    const update = () => (fullscreen = document.fullscreenElement === node);
    document.addEventListener('fullscreenchange', update);
    update();
    return {
      destroy() {
        document.removeEventListener('fullscreenchange', update);
      }
    };
  }
</script>

<section
  class="screen-stage"
  class:large={voice.screenStage === 'large'}
  class:audio-only={voice.screenAudioOnly}
  aria-label="Shared screen"
  bind:this={stage}
  use:trackFullscreen
>
  <video
    use:screenTarget={voice}
    class="shared-screen"
    autoplay
    playsinline
    muted
    aria-label="A screen being shared in this conversation"
  ></video>
  {#if voice.screenAudioOnly}
    <p class="screen-placeholder">
      <Icon name="volumeOff" size={18} /> Audio only — the picture is not being decoded.
    </p>
  {/if}
  <div class="screen-controls">
    <button
      type="button"
      aria-pressed={voice.screenAudioOnly}
      onclick={() => voice.setScreenAudioOnly(!voice.screenAudioOnly)}
      ><Icon
        name={voice.screenAudioOnly ? 'volume' : 'volumeOff'}
        size={15}
      />{voice.screenAudioOnly ? 'Show picture' : 'Audio only'}</button
    >
    <button
      type="button"
      aria-pressed={voice.screenStage === 'large'}
      onclick={() => voice.setScreenStage(voice.screenStage === 'large' ? 'corner' : 'large')}
      ><Icon name="expand" size={15} />{voice.screenStage === 'large'
        ? 'Smaller'
        : 'Enlarge'}</button
    >
    <button type="button" aria-pressed={fullscreen} onclick={toggleFullscreen}
      ><Icon name="expand" size={15} />{fullscreen ? 'Exit fullscreen' : 'Fullscreen'}</button
    >
  </div>
</section>

<style>
  /*
   * The stage is a floating pane over the conversation rather than a layout region: the
   * conversation keeps its own scroll position while somebody shares, and a share is
   * interrupted by a modal for the same reason a call is.
   */
  .screen-stage {
    position: fixed;
    inset-block-end: 1rem;
    inset-inline-end: 1rem;
    width: min(28rem, 45vw);
    z-index: 30;
    display: flex;
    flex-direction: column;
    background: #000;
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 8px 32px #0006;
    overflow: hidden;
  }
  .screen-stage.large {
    width: min(72rem, 92vw);
  }
  .shared-screen {
    width: 100%;
    aspect-ratio: 16 / 10;
    object-fit: contain;
    background: #000;
    display: block;
  }
  .screen-stage.audio-only .shared-screen {
    display: none;
  }
  .screen-placeholder {
    margin: 0;
    padding: 1.25rem 0.9rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: #d6d9de;
    font-size: 0.85rem;
    background: #11141a;
  }
  .screen-controls {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    padding: 0.4rem;
    background: #11141a;
    border-top: 1px solid var(--border);
  }
  .screen-controls button {
    flex: 1 1 auto;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.35rem;
    min-height: 2rem;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: #1b202a;
    color: #e6e9ee;
    font-size: 0.8rem;
  }
  .screen-controls button:hover {
    background: #242b37;
  }
  .screen-controls button[aria-pressed='true'] {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-text);
  }

  /* A narrow screen has no room for a picture beside a conversation, so the share takes
     the width and the conversation is read above it. The controls keep full-size targets:
     this is the layout a phone gets, and a phone is where they are hardest to hit. */
  @media (max-width: 720px) {
    .screen-stage {
      inset-inline: 0.5rem;
      width: auto;
    }
    .screen-stage.large {
      max-height: 70vh;
    }
    .screen-controls button {
      min-height: 2.75rem;
      flex-basis: 30%;
    }
  }
</style>
