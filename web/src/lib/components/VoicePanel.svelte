<script lang="ts">
  import { voice } from '../voice.svelte';
  import { workspace } from '../workspace.svelte';
  import type { VoiceState } from '../types';
  import Modal from './Modal.svelte';
  import Avatar from './Avatar.svelte';
  import Icon from './Icon.svelte';
  import AudioSettings from './AudioSettings.svelte';
  let showAudio = $state(false);
  let audioSettings = $state<{ stopTest: () => void }>();
  // One resolver for both kinds of room. A private call is a conversation, not a
  // channel, and reading the channel list here would have made it unreachable — the
  // panel would have shown "no channel by that name" for a call the server admits.
  let room = $derived(voice.selectedRoom);
  let channel = $derived(
    workspace.state?.channels.find((channel) => channel.id === voice.selectedId)
  );
  let participants = $derived(room?.participants ?? []);
  let roomName = $derived(room?.name ?? channel?.name ?? 'this call');

  function moderate(state: VoiceState, action: 'kick' | 'mute' | 'deafen') {
    const user = workspace.state?.users.find((user) => user.id === state.user_id);
    if (
      !confirm(
        `${action === 'kick' ? 'Remove' : action === 'mute' ? 'Change microphone restriction for' : 'Change audio restriction for'} ${user?.display_name || user?.username} in voice?`
      )
    )
      return;
    workspace.sendEvent(`admin.voice_${action}`, {
      user_id: state.user_id,
      confirm: state.user_id,
      ...(action === 'mute'
        ? { muted: !state.admin_muted }
        : action === 'deafen'
          ? { deafened: !state.admin_deafened }
          : {})
    });
  }
</script>

<Modal
  title={room?.name || (channel?.name ?? 'Voice channel')}
  close={() => (voice.panelOpen = false)}
>
  <div class="voice-panel-header">
    <div class="channel-symbol"><Icon name="volume" size={32} /></div>
    <p>{channel?.topic || 'Drop in and hang out.'}</p>
    <small>
      {#if room?.private}
        {participants.length === 2 ? 'Two people on this call' : 'Nobody else is on this call yet'}
      {:else}
        {participants.length} of {room?.capacity || 8} places occupied
      {/if}
    </small>
  </div>
  <div class="voice-participants">
    {#each participants as state (state.user_id)}{@const user = workspace.state?.users.find(
        (user) => user.id === state.user_id
      )}
      <div class="voice-person" class:speaking={state.speaking}>
        <Avatar {user} size={44} /><strong>{user?.display_name || user?.username}</strong><small
          >{state.admin_muted
            ? 'Muted by admin'
            : state.muted
              ? 'Muted'
              : state.deafened || state.admin_deafened
                ? 'Deafened'
                : state.speaking
                  ? 'Speaking'
                  : 'Listening'}</small
        >{#if workspace.can('voice.moderate') && state.user_id !== workspace.user?.id}<div
            class="voice-moderation"
          >
            <button
              class="icon-button"
              aria-label={`Toggle mute for ${user?.username}`}
              onclick={() => moderate(state, 'mute')}><Icon name="micOff" size={15} /></button
            ><button
              class="icon-button"
              aria-label={`Toggle deafen for ${user?.username}`}
              onclick={() => moderate(state, 'deafen')}><Icon name="headphones" size={15} /></button
            ><button
              class="icon-button danger-text"
              aria-label={`Remove ${user?.username} from voice`}
              onclick={() => moderate(state, 'kick')}><Icon name="leave" size={15} /></button
            >
          </div>{/if}
        {#if state.user_id !== workspace.user?.id}<label class="participant-volume"
            >Volume · {Math.round((voice.participantVolumes[state.user_id] ?? 1) * 100)}%<input
              aria-label={`Volume for ${user?.display_name || user?.username}`}
              type="range"
              min="0"
              max="1"
              step="0.05"
              value={voice.participantVolumes[state.user_id] ?? 1}
              oninput={(event) =>
                voice.setParticipantVolume(state.user_id, Number(event.currentTarget.value))}
            /></label
          >{/if}
      </div>{/each}
  </div>
  {#if voice.error}<p class="error voice-error" role="alert">{voice.error}</p>{/if}
  {#if voice.audioBlocked}<button class="secondary full" onclick={() => voice.enableAudio()}
      >Tap to enable audio playback</button
    >{/if}
  {#if voice.channelId !== channel?.id}<button
      class="secondary full"
      aria-expanded={showAudio}
      onclick={() => (showAudio = !showAudio)}
      ><Icon name="mic" size={18} />Microphone &amp; sound check</button
    >
    {#if showAudio}<AudioSettings bind:this={audioSettings} compact />{/if}
  {/if}
  <div class="voice-panel-actions">
    {#if voice.channelId === room?.id}<button
        class="secondary"
        onclick={() => voice.toggleMute()}
        disabled={voice.ownState?.admin_muted || voice.ownState?.admin_deafened}
        ><Icon name={voice.muted ? 'micOff' : 'mic'} size={18} />{voice.muted
          ? 'Unmute'
          : 'Mute'}</button
      ><button class="danger-button" onclick={() => voice.leave()}
        >{room?.private ? 'Leave call' : 'Leave voice'}</button
      >{:else}<button
        class="primary full"
        disabled={voice.status === 'requesting' ||
          voice.status === 'connecting' ||
          !room ||
          participants.length >= (room.capacity || 8)}
        onclick={() => {
          audioSettings?.stopTest();
          showAudio = false;
          if (room) void voice.join(room.id);
        }}
        ><Icon name="headphones" size={19} />{voice.status === 'requesting'
          ? 'Waiting for microphone…'
          : participants.length >= (room?.capacity || 8)
            ? room?.private
              ? 'Already on a call here'
              : 'Channel is full'
            : room?.private
              ? 'Start a call'
              : 'Join voice'}</button
      >{/if}
  </div>
</Modal>
