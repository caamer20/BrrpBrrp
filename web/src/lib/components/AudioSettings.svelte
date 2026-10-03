<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { voice } from '../voice.svelte';
  import { VoiceChime } from '../voice-chime';
  import { errorMessage } from '../api';
  import { listInputs, listOutputs, supportsOutputSelection } from '../devices';
  let { compact = false }: { compact?: boolean } = $props();
  let devices = $state<MediaDeviceInfo[]>([]);
  // Where the browser has speakers to choose between, they are chosen here. Where it
  // does not, the panel says so instead of showing an empty list.
  let supportsOutputs = supportsOutputSelection();
  let outputs = $state<{ id: string; label: string }[]>([]);
  let outputsError = $state('');
  let testing = $state(false);
  let busy = $state(false);
  let level = $state(0);
  let error = $state('');
  let stream: MediaStream | null = null;
  let context: AudioContext | null = null;
  let timer: ReturnType<typeof setInterval>;
  let captureTimer: ReturnType<typeof setTimeout>;
  let generation = 0;
  let namesPending = $state(false);
  async function refresh() {
    // The shared helper, so the list and the call's failure speak the same language.
    // Two enumerations in one product means one of them eventually says "your browser
    // could not list microphones" for a failure the other one can name exactly.
    const speakers = await listOutputs();
    outputs = speakers.devices;
    outputsError = speakers.error;
    const result = await listInputs();
    // The template reads `deviceId` and `label`, which is the shape the select has
    // always been written against. Adapting here keeps one enumeration in the product
    // and one spelling of a device at the boundary that renders it.
    devices = result.devices.map((device) => ({
      deviceId: device.id,
      kind: 'audioinput',
      label: device.label,
      groupId: ''
    })) as MediaDeviceInfo[];
    namesPending = result.namesPending;
    error = result.error;
  }
  onMount(() => {
    refresh();
    navigator.mediaDevices?.addEventListener('devicechange', refresh);
  });
  function stop() {
    generation++;
    clearInterval(timer);
    clearTimeout(captureTimer);
    stream?.getTracks().forEach((track) => track.stop());
    stream = null;
    context?.close().catch(() => {});
    context = null;
    testing = false;
    busy = false;
    level = 0;
  }
  export function stopTest() {
    stop();
  }
  onDestroy(() => {
    stop();
    navigator.mediaDevices?.removeEventListener('devicechange', refresh);
  });
  async function testMicrophone() {
    if (testing || busy) {
      stop();
      return;
    }
    error = '';
    busy = true;
    const current = ++generation;
    captureTimer = setTimeout(() => {
      if (current !== generation) return;
      stop();
      error =
        'The microphone did not become ready. Check browser and device microphone permissions, then try the microphone test again.';
    }, 30000);
    try {
      if (!navigator.mediaDevices?.getUserMedia)
        throw new Error('Microphone access needs trusted HTTPS. Open your secure server address.');
      const opened = await navigator.mediaDevices.getUserMedia({
        audio: { deviceId: voice.microphone ? { exact: voice.microphone } : undefined },
        video: false
      });
      if (current !== generation) {
        opened.getTracks().forEach((track) => track.stop());
        return;
      }
      stream = opened;
      context = new AudioContext();
      await context.resume();
      if (current !== generation || !context) return;
      clearTimeout(captureTimer);
      const analyser = context.createAnalyser();
      analyser.fftSize = 512;
      context.createMediaStreamSource(stream).connect(analyser);
      const samples = new Float32Array(analyser.fftSize);
      timer = setInterval(() => {
        analyser.getFloatTimeDomainData(samples);
        level = Math.min(
          1,
          Math.sqrt(samples.reduce((sum, sample) => sum + sample * sample, 0) / samples.length) * 6
        );
      }, 80);
      testing = true;
      busy = false;
      refresh();
    } catch (failure) {
      if (current === generation) {
        stop();
        error =
          failure instanceof DOMException && failure.name === 'NotAllowedError'
            ? 'Microphone access was denied. Allow it in this site’s browser settings, then try again.'
            : errorMessage(failure);
      }
    }
  }
  async function testSound() {
    const audio = new AudioContext();
    try {
      await audio.resume();
      new VoiceChime(audio).play(voice.soundVolume);
      setTimeout(() => audio.close().catch(() => {}), 1000);
    } catch (failure) {
      audio.close().catch(() => {});
      error = errorMessage(failure);
    }
  }
</script>

<section class="settings-section">
  <h3>{compact ? 'Check your audio' : 'Voice & sound'}</h3>
  <label
    >Microphone<select
      value={voice.microphone}
      disabled={testing || busy || voice.changingMicrophone}
      onchange={(event) => voice.changeMicrophone(event.currentTarget.value)}
    >
      <option value="">System default</option>
      {#each devices.filter((device) => device.deviceId && device.deviceId !== 'default') as device}<option
          value={device.deviceId}>{device.label || 'Microphone'}</option
        >{/each}
    </select><small
      >{#if namesPending}
        Your browser hides device names until you allow microphone access. Join a call once, or use
        the test below, and they appear here.
      {:else}
        Allow microphone access to see device names.
      {/if}</small
    ></label
  >
  {#if supportsOutputs}
    <label
      >Speaker<select
        value={voice.outputDevice}
        onchange={(event) => voice.setOutputDevice(event.currentTarget.value)}
      >
        <option value="">System default</option>
        {#each outputs.filter((device) => device.id && device.id !== 'default') as device}<option
            value={device.id}>{device.label}</option
          >{/each}
      </select><small
        >{#if outputsError}{outputsError}{:else}Played through this speaker for every voice
          conversation on this device.{/if}</small
      ></label
    >
  {:else}
    <p class="muted">
      This browser always plays voice through the system default speaker, so there is nothing to
      choose here.
    </p>
  {/if}
  <div class="button-row">
    <button class="secondary" onclick={testMicrophone}
      >{testing || busy ? 'Stop microphone test' : 'Test microphone'}</button
    ><button class="secondary" onclick={testSound}>Play test sound</button>
  </div>
  <label
    >Input level<meter min="0" max="1" value={testing ? level : voice.inputLevel}></meter></label
  >
  {#if compact}<p class="muted">
      The microphone test stays on this device. Joining voice stops the test.
    </p>{:else}
    <label class="toggle-row"
      ><span
        ><strong>Push to talk</strong><small
          >Hold Space while the app is focused, or use the Hold to talk button. Typing never opens
          the microphone.</small
        ></span
      ><input
        type="checkbox"
        bind:checked={voice.pushToTalk}
        onchange={() => voice.saveAudioPreferences()}
      /></label
    >
    <label class="toggle-row"
      ><span
        ><strong>Voice join sounds</strong><small
          >A gentle chime when someone joins your room.</small
        ></span
      ><input
        type="checkbox"
        bind:checked={voice.joinSounds}
        onchange={() => voice.saveAudioPreferences()}
      /></label
    >
    <label
      >Notification volume · {Math.round(voice.soundVolume * 100)}%<input
        type="range"
        min="0"
        max="1"
        step="0.05"
        bind:value={voice.soundVolume}
        oninput={() => voice.saveAudioPreferences()}
      /></label
    >
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if voice.error}<p class="error" role="alert">{voice.error}</p>{/if}
</section>
