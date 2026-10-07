import { diagnoseMicrophone, microphoneMessage, type MicrophoneDiagnosis } from './devices';
import { storage } from './storage';
import { workspace } from './workspace.svelte';
import { errorMessage } from './api';
import type { RealtimeEvent } from './types';
import { VoiceChime } from './voice-chime';
import { describePath } from './connection-path';
import {
  applyScreenAudioOnly,
  VIEW_STREAM_ID,
  isViewOnlyTrack,
  screenAudioOwner
} from './screen-receive';
import {
  applyScreenPreset,
  canShareScreen,
  onCaptureEnded,
  SCREEN_PRESETS,
  screenAudioMessage,
  screenAudioOutcome,
  requestScreen,
  screenPresetFromValue,
  screenShareMessage,
  stopScreen,
  type ScreenPreset,
  type ScreenShareState,
  captureStillWanted,
  type CaptureAudience
} from './screen-share';

class Voice {
  channelId = $state('');
  selectedId = $state('');
  panelOpen = $state(false);
  status = $state<'disconnected' | 'requesting' | 'connecting' | 'connected'>('disconnected');
  muted = $state(false);
  deafened = $state(false);
  speaking = $state(false);
  audioBlocked = $state(false);
  error = $state('');
  /** Why the microphone did not start, when it did not. Empty means it did. */
  diagnosis = $state<MicrophoneDiagnosis>('ok');
  movingTo = $state('');
  inputLevel = $state(0);
  changingMicrophone = $state(false);
  quality = $state('');
  // A shared screen arrives as a stream, not as an audio slot, and is rendered by a
  // video element the workspace places once. `screenElement` is bound by that
  // component; the model holds the stream so a re-negotiation can replace it.
  screenShared = $state(false);
  // One request per call: a second renegotiation for the same share would be noise,
  // and a client that failed to add the track should be able to try again later.
  private screenRequested = false;
  // The sender for a published screen, so a restart replaces the track rather than
  // negotiating a second one into a room whose bound is one screen.
  private screenSender: RTCRtpSender | null = null;
  // The local capture, kept apart from `screenStream` — which is what the panel
  // renders and is either the capture or a share arriving from the server. The two
  // were one field, and the consequence was that a stream arriving from the server
  // stopped the capture the client was publishing: the sharer's own share died the
  // moment the answer to its own view line was applied (review 17, two-browser screen
  // journey).
  private capture: MediaStream | null = null;
  // System audio is a *separate* publication from the screen's video. The server
  // bounds them separately — a screen's audio is only meaningful alongside its
  // video — and a client that sent them as one would be refused by the screen's own
  // bound the moment audio counted against the video's allowance.
  private screenAudioSender: RTCRtpSender | null = null;
  // Whether the server has accepted this screen as a publication. Only a *published*
  // share is withdrawn, and only the track is stopped for one that was not.
  private publishedScreen = false;
  private publishedScreenAudio = false;
  /** Whether the operator asked for system audio with the screen. */
  wantSystemAudio = $state(false);
  /**
   * Which capture profile the next share uses, remembered across calls because it is a
   * property of what somebody shares (a document, or a video) rather than of the call.
   */
  screenPreset = $state<ScreenPreset>(screenPresetFromValue(storage.getItem('brrp.screen-preset')));
  // Bound by the workspace. Reading it is a deliberate function rather than a plain
  // `bind:this`, because binding an element is also the moment the element becomes
  // available to render into — and a share that arrived first has nothing to attach to
  // until then.
  screenElement = $state<HTMLVideoElement | null>(null);
  // The selected candidate pair's kind: 'host' | 'srflx' | 'prflx' | 'relay'. It is
  // read by qualification and by support, and it is the only way to tell a direct
  // connection from a relayed one after the fact.
  connectionPath = $state('');
  // Screen sharing. The state is the *share's* state rather than the call's, because
  // somebody can be connected and not sharing, sharing and muted, or neither — and
  // collapsing them into one flag is how "I stopped sharing" ends up meaning "I left
  // the call".
  screenState = $state<ScreenShareState>('idle');
  screenLabel = $state('');
  screenMessage = $state('');
  screenStream = $state<MediaStream | null>(null);
  // How the picture is presented, and whether it is decoded at all. Both are the
  // viewer's, both outlive one share, and neither is the server's business: the room
  // does not know who is watching on a phone with a data limit.
  screenStage = $state<'corner' | 'large'>(
    storage.getItem('brrp.screen-stage') === 'large' ? 'large' : 'corner'
  );
  screenAudioOnly = $state(storage.getItem('brrp.screen-audio-only') === 'true');
  private releaseScreenWatch: (() => void) | null = null;
  microphone = $state(storage.getItem('brrp.microphone') || '');
  // The chosen speaker, where the browser has the notion at all. Applied to every audio
  // element the call owns, and to each one as it arrives: a participant who joins after
  // the choice was made must not come out of a different speaker than the others.
  outputDevice = $state(storage.getItem('brrp.output-device') || '');
  pushToTalk = $state(storage.getItem('brrp.push-to-talk') === 'true');
  talkHeld = $state(false);
  joinSounds = $state(storage.getItem('brrp.join-sounds') !== 'false');
  soundVolume = $state(Number(storage.getItem('brrp.sound-volume') ?? 0.7));
  participantVolumes = $state<Record<string, number>>({});
  private routes: string[] = [];
  private routingVersion = -1;
  private connectionId = '';
  private microphoneChange = 0;
  private monitorSource: MediaStreamAudioSourceNode | null = null;
  private audioSlots = new Map<HTMLAudioElement, number>();
  // A shared screen's sound is not in a voice slot; it is keyed to the account sharing it.
  private audioOwners = new Map<HTMLAudioElement, string>();
  private resumeToken = '';
  private resuming = false;
  private resumeTimer?: ReturnType<typeof setTimeout>;
  private moveTimer?: ReturnType<typeof setTimeout>;
  private statsTimer?: ReturnType<typeof setInterval>;
  private stream: MediaStream | null = null;
  private peer: RTCPeerConnection | null = null;
  private audioContext: AudioContext | null = null;
  private chime: VoiceChime | null = null;
  private hasConnected = false;
  private audios: HTMLAudioElement[] = [];
  private monitor?: ReturnType<typeof setInterval>;
  private disconnectTimer?: ReturnType<typeof setTimeout>;
  private joinTimer?: ReturnType<typeof setTimeout>;
  private captureTimer?: ReturnType<typeof setTimeout>;
  private candidates: RTCIceCandidateInit[] = [];
  private signalChain = Promise.resolve();
  // Offers are paired with their answers one at a time. `signalChain` orders the
  // *messages* this client processes, but a message can only be processed when the
  // offer it answers is the one outstanding: creating a second offer while the first
  // waits for its answer leaves the client in `have-local-offer`, and the arriving
  // answer is then refused — which surfaced as "Voice negotiation failed" the moment a
  // viewer request and a screen publish overlapped (review 17, two-browser screen
  // journey). This queue makes each offer wait for its answer (or a bounded timeout)
  // before the next one is created.
  private negotiation = Promise.resolve();
  private pendingAnswer: (() => void) | null = null;
  private generation = 0;
  private rejoining = false;
  private retries = 0;
  private bitrate = 24000;

  constructor() {
    workspace.onVoiceEvent = (event) => this.receive(event);
    workspace.leaveVoice = () => this.leave();
    workspace.voiceDisconnected = () => {
      if (!this.channelId) return;
      if (!this.hasConnected) {
        this.leave(false);
        return;
      }
      this.quality = 'Reconnecting to chat…';
      clearTimeout(this.resumeTimer);
      this.resumeTimer = setTimeout(() => {
        this.error = 'Voice disconnected. Join again to reconnect.';
        this.leave(false);
      }, 28000);
    };
    workspace.voiceReconnected = () => {
      if (this.channelId && this.resumeToken) {
        this.resuming = true;
        workspace.sendEvent('voice.resume', { token: this.resumeToken });
      }
    };
    window.addEventListener('keydown', (event) => {
      const target = event.target;
      if (
        event.code !== 'Space' ||
        !this.pushToTalk ||
        !this.channelId ||
        !(target instanceof Element) ||
        target.closest('input, textarea, select, button, [contenteditable="true"]') ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey
      )
        return;
      event.preventDefault();
      if (!this.talkHeld) {
        this.talkHeld = true;
        this.applyAudioState();
      }
    });
    window.addEventListener('keyup', (event) => {
      if (event.code === 'Space') this.releaseTalk();
    });
    window.addEventListener('blur', () => this.releaseTalk());
    document.addEventListener('visibilitychange', () => {
      if (document.hidden) this.releaseTalk();
    });
  }

  get ownState() {
    return workspace.state?.voice_states.find((state) => state.user_id === workspace.user?.id);
  }

  /**
   * Opens a call's room panel, or moves to it when one is already joined.
   *
   * A room is a community voice channel or a private conversation. Both go through
   * this one entry point, because a person deciding to call somebody should not have
   * to find a different control for a direct conversation than for a channel — and
   * because a second entry point is a second set of gates that can disagree about
   * whether a call may be started.
   */
  selectChannel(id: string) {
    if (
      id === this.channelId ||
      this.movingTo ||
      this.status === 'requesting' ||
      this.status === 'connecting'
    )
      return;
    this.selectedId = id;
    this.error = '';
    if (this.channelId) void this.join(id);
    else this.panelOpen = true;
  }

  /** The room the selected id names, whether it is a channel or a conversation. */
  roomFor(id: string) {
    if (!id) return null;
    return this.selectedRoomFor(id);
  }

  private selectedRoomFor(id: string) {
    const channel = workspace.state?.channels.find((item) => item.id === id);
    if (channel) {
      if (channel.type !== 'voice') return null;
      return {
        id,
        private: false,
        name: channel.name,
        capacity: channel.max_voice_users,
        participants: workspace.state?.voice_states.filter((state) => state.channel_id === id) ?? []
      };
    }
    const direct = workspace.directConversations.find((item) => item.id === id);
    if (direct) {
      return {
        id,
        private: true,
        name: direct.peer?.display_name || direct.peer?.username || 'this conversation',
        capacity: 2,
        participants: workspace.state?.voice_states.filter((state) => state.channel_id === id) ?? []
      };
    }
    const group = workspace.groups.find((item) => item.id === id);
    if (group) {
      return {
        id,
        private: true,
        name: group.name || 'this group',
        capacity: 2,
        participants: workspace.state?.voice_states.filter((state) => state.channel_id === id) ?? []
      };
    }
    return null;
  }

  get selectedRoom() {
    return this.roomFor(this.selectedId);
  }

  async join(id: string) {
    if (
      id === this.channelId ||
      this.movingTo ||
      this.status === 'requesting' ||
      this.status === 'connecting'
    )
      return;
    if (workspace.connection !== 'connected') {
      this.error = 'Reconnect to the server before joining voice.';
      return;
    }
    // A room is a voice channel or a private conversation. The gate is a room being
    // a *call* at all, and a private conversation is one; the capacity and fullness
    // are then the room's own.
    const room = this.roomFor(id);
    if (!room) return;
    if ((room.participants.length || 0) >= room.capacity) {
      this.error = `${room.name} is full.`;
      return;
    }
    if (this.channelId && this.peer) {
      this.movingTo = id;
      this.error = '';
      workspace.sendEvent('voice.move', { channel_id: id });
      this.moveTimer = setTimeout(() => {
        this.movingTo = '';
        this.error = 'Room switch timed out. Please reconnect to voice.';
        this.leave();
      }, 10000);
      return;
    }
    this.leave();
    this.error = '';
    this.selectedId = id;
    this.status = 'requesting';
    this.panelOpen = false;
    const generation = ++this.generation;
    this.captureTimer = setTimeout(() => {
      if (generation !== this.generation || this.status !== 'requesting') return;
      this.error =
        'The microphone did not become ready. Check browser and device microphone permissions, then retry voice.';
      this.leave(false);
    }, 30000);
    try {
      if (!window.isSecureContext || !navigator.mediaDevices)
        throw new Error('Voice needs HTTPS. Open the secure server address or use localhost.');
      this.audioContext = new AudioContext();
      this.chime = new VoiceChime(this.audioContext);
      await this.audioContext.resume();
      if (generation !== this.generation) return;
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
          channelCount: 1,
          ...(this.microphone ? { deviceId: { exact: this.microphone } } : {})
        },
        video: false
      });
      if (generation !== this.generation) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      clearTimeout(this.captureTimer);
      this.stream = stream;
      this.channelId = id;
      this.status = 'connecting';
      this.startMonitor();
      workspace.sendEvent('voice.join', { channel_id: id });
      this.joinTimer = setTimeout(() => {
        if (this.status !== 'connected') {
          this.error = 'Voice timed out. Check the server UDP ports or enable TURN.';
          this.leave();
        }
      }, 25000);
    } catch (error) {
      if (generation !== this.generation) return;
      // A call that did not start has a handful of ordinary causes, and they are not
      // the same for everybody. "Check permissions" is the beginning of a search for
      // most people who read it, because they have already checked the permission.
      this.diagnosis = diagnoseMicrophone(error, window.isSecureContext);
      this.error = microphoneMessage(this.diagnosis) || errorMessage(error);
      this.leave();
    }
  }

  private receive(event: RealtimeEvent) {
    if (event.t === 'voice.authority_changed') {
      // The server has withdrawn this account's permission to share. The server has
      // already stopped forwarding; only the browser can stop the *capture*. Leaving
      // it running is the worst outcome available: the person sees their own sharing
      // indicator still lit, a rectangle of their desktop still in memory, and no
      // picture in the room — with no way to tell those apart.
      //
      // The capture is released before anything is shown, so the browser's own
      // indicator goes off even if this handler throws.
      void this.releaseCaptureOnAuthorityChange(event.d?.reason);
      return;
    }
    if (event.t === 'voice.routing') {
      if (!this.channelId || event.d.version <= this.routingVersion) return;
      this.routingVersion = event.d.version;
      this.routes = event.d.slots;
      this.applyAudioState();
      return;
    }
    if (event.t === 'voice.resumed') {
      if (!this.channelId) return;
      clearTimeout(this.resumeTimer);
      this.resuming = false;
      this.resumeToken = event.d.resume_token;
      this.channelId = event.d.channel_id;
      this.sendState();
      if (this.peer?.connectionState !== 'connected') this.reconnect();
      else this.quality = '';
      return;
    }
    if (event.t === 'voice.moved' || event.t === 'voice.move.error') {
      if (!this.channelId || !this.movingTo) return;
      clearTimeout(this.moveTimer);
      this.movingTo = '';
      if (event.t === 'voice.move.error') this.error = event.d.error;
      else {
        this.channelId = this.selectedId = event.d.channel_id;
        this.panelOpen = false;
        this.playJoinChime();
        this.sendState();
      }
      return;
    }
    if (event.t === 'voice.user_joined') {
      if (
        event.d.channel_id === this.channelId &&
        event.d.user_id !== workspace.user?.id &&
        this.status === 'connected'
      )
        this.playJoinChime();
      return;
    }
    if (event.t === 'voice.state_updated') {
      this.applyAudioState();
      return;
    }
    if (event.t === 'voice.error') {
      if (this.resuming && this.stream && this.channelId) {
        this.resuming = false;
        clearTimeout(this.resumeTimer);
        this.reconnect();
        return;
      }
      this.error = event.d.error;
      this.leave();
      return;
    }
    if (event.t === 'voice.left') {
      if (!this.rejoining && event.d.channel_id === this.channelId) this.leave(false);
      return;
    }
    if (event.t === 'voice.access_revoked') {
      // The server ended the stream because this account may no longer read the
      // channel. Say so; silence would look like a connection failure.
      this.error = event.d.reason || 'You no longer have access to that voice channel.';
      this.leave(false);
      return;
    }
    if (
      event.t === 'thread.created' ||
      event.t === 'thread.archived' ||
      event.t === 'thread.locked'
    ) {
      // Not a media event; the screen path does not depend on threads.
    }
    if (event.t === 'screen.published' || event.t === 'screen.stopped') {
      // A share starting or stopping is the point at which this client needs a video
      // track, so the request is made here rather than at join: a client that joined
      // after the share began has no video transceiver otherwise. The kind is carried
      // on the announcement because a share is two publications: the picture arrives on
      // the view offer this client already made, while the screen's audio is announced
      // afterwards and needs its own m-line.
      if (event.t === 'screen.published') {
        // A publisher never asks to view its own publication: the capture is already
        // here, and the media the server would send back is the same picture relayed
        // at best. The server withholds the announcement from the owner; this keeps a
        // replayed or cross-tab event from restarting the cycle.
        if (event.d?.owner && event.d.owner === workspace.user?.id) return;
        this.requestScreenReceive(event.d?.kind, event.d?.connection_id);
      } else {
        this.screenStream?.getTracks().forEach((track) => track.stop());
        this.screenStream = null;
        this.capture = null;
        this.screenShared = false;
      }
    }
    if (event.t === 'voice.join.accept') {
      const generation = this.generation;
      if (!this.stream || event.d.channel_id !== this.channelId) return;
      this.connectionId = event.d.connection_id;
      this.signalChain = this.signalChain
        .then(() => (generation === this.generation ? this.createPeer(event.d) : undefined))
        .catch((error) => {
          if (generation === this.generation) this.fail(error);
        });
    }
    if (event.t === 'rtc.signal') {
      if (event.d.connection_id !== this.connectionId) return;
      const generation = this.generation;
      this.signalChain = this.signalChain
        .then(async () => {
          if (generation !== this.generation) return;
          const peer = this.peer;
          if (!peer) return;
          if (event.d.sdp) {
            await peer.setRemoteDescription(event.d.sdp);
            // The offer this answers is no longer outstanding, so the next offer may
            // be created (see `negotiation`).
            this.settleAnswer();
            if (peer !== this.peer) return;
            for (const candidate of this.candidates) await peer.addIceCandidate(candidate);
            this.candidates = [];
            const sender = peer.getSenders().find((sender) => sender.track?.kind === 'audio');
            if (sender) {
              const parameters = sender.getParameters();
              if (parameters.encodings.length) {
                parameters.encodings[0].maxBitrate = this.bitrate;
                await sender.setParameters(parameters);
              }
            }
          }
          if (event.d.ice) {
            if (peer.remoteDescription) await peer.addIceCandidate(event.d.ice);
            else this.candidates.push(event.d.ice);
          }
        })
        .catch((error) => {
          if (generation === this.generation) this.fail(error);
        });
    }
  }

  /**
   * Asks for a screen to be sent to this client.
   *
   * Adding a transceiver changes the connection, so it is a renegotiation and goes
   * through the same path a share does. A client that cannot add one — a browser with
   * no video support, or a device that will not negotiate a second m-line — keeps its
   * voice call, which is the point of a no-op here rather than a failure.
   */
  private requestScreenReceive(_kind?: string, _connectionID?: string) {
    const peer = this.peer;
    if (!peer || !this.stream) return;
    if (this.screenRequested) return;
    this.screenRequested = true;
    // The view request is one offer carrying both lines, and it waits its turn in the
    // offer/answer queue: it arrives while the *publisher's* offer is still being
    // answered, and creating it out of order left the connection in
    // `have-local-offer` and refused that answer — "Voice negotiation failed" whenever
    // somebody was watching (review 17, two-browser screen journey). The screen's audio
    // is announced separately by the server, but the client can hold an m-line for it
    // from the start: an idle line costs nothing, and a second renegotiation per share
    // is exactly the sort of interleaving this file has already been burned by.
    void this.negotiate(async () => {
      const current = this.peer;
      if (!current || !this.stream) return false;
      addScreenReceive(current, 'video');
      addScreenReceive(current, 'audio');
      const offer = await current.createOffer();
      await current.setLocalDescription(offer);
      if (current !== this.peer) return false;
      workspace.sendEvent('rtc.signal', {
        sdp: current.localDescription?.toJSON(),
        connection_id: this.connectionId
      });
      return true;
    }).then((sent) => {
      if (sent) return;
      // The voice call continues. The share simply will not arrive, and the panel says
      // so rather than the call appearing broken.
      this.screenRequested = false;
    });
  }

  /**
   * Renders a shared screen.
   *
   * One element, one at a time: the product's bound is one screen per room, so a second
   * one has nowhere to go. It is `playsinline` and muted because a video element that
   * tried to unmute would play the sharer's system audio at whoever is watching.
   */
  private attachScreen(stream: MediaStream | undefined | null) {
    if (!stream) return;
    // While this client is publishing a screen, its own capture is what the panel
    // shows. A stream arriving from the server at that moment is the same picture
    // relayed back — the answer to a view line the client should not have asked for —
    // and attaching it here stopped the capture's tracks, ending the very share the
    // announcement was about. The server no longer announces a publication to its
    // owner and the client no longer requests one, so this is the third layer: the
    // capture cannot be stopped by anything arriving on the connection.
    if (this.capture) return;
    this.screenStream?.getTracks().forEach((track) => track.stop());
    this.screenStream = stream;
    // The flag comes first, so the element is in the DOM before it is asked to render.
    // It used to come last, and the element only exists while the flag is true — so the
    // very first share never had an element to attach to. The stream was stored, the
    // screen was announced as shared, and nothing arrived: a blank rectangle with no
    // error anywhere, which a viewer cannot tell from a share that failed to send.
    this.screenShared = true;
    // A share that starts while the viewer prefers audio-only arrives already silenced,
    // rather than playing one frame's worth of a picture they asked not to decode.
    applyScreenAudioOnly(stream, this.screenAudioOnly);
    this.renderScreen();
  }

  /**
   * Puts the stored screen on the video element, whenever that element exists.
   *
   * Called both when a stream arrives and when the element binds, because the two can
   * happen in either order and neither order is wrong on its own. A share that arrives
   * before the workspace has rendered the element is the common case on a slow join.
   */
  /**
   * Receives the workspace's screen element and renders whatever is waiting for it.
   *
   * Used as the `bind:this` target rather than assigning the field directly. A stream and
   * an element arrive independently and in either order, and the old code only ever
   * looked once: when the stream arrived. A share that landed first was stored, the
   * screen was announced, and nothing was ever rendered — a blank rectangle with no
   * error, indistinguishable from a share that failed to send.
   */
  /**
   * Sends one audio element to the chosen speaker.
   *
   * The failure is reported rather than swallowed: a device id stored on one machine and
   * carried to another names nothing, and silence with no explanation is the one outcome
   * a person cannot act on.
   */
  private applyOutputDevice(audio: HTMLAudioElement) {
    if (!this.outputDevice || typeof audio.setSinkId !== 'function') return;
    audio.setSinkId(this.outputDevice).catch(() => {
      this.outputDevice = '';
      storage.setItem('brrp.output-device', '');
      this.error =
        'That speaker is not available on this device. Choose another in voice settings.';
    });
  }

  async setOutputDevice(deviceId: string) {
    this.outputDevice = deviceId;
    storage.setItem('brrp.output-device', deviceId);
    for (const audio of this.audios) {
      if (typeof audio.setSinkId !== 'function') continue;
      try {
        await audio.setSinkId(deviceId);
      } catch {
        this.outputDevice = '';
        storage.setItem('brrp.output-device', '');
        this.error =
          'That speaker could not be used. Choose another in voice settings; the system default is playing meanwhile.';
        return;
      }
    }
  }

  setScreenAudioOnly(audioOnly: boolean) {
    this.screenAudioOnly = audioOnly;
    storage.setItem('brrp.screen-audio-only', String(audioOnly));
    applyScreenAudioOnly(this.screenStream, audioOnly);
  }

  setScreenStage(stage: 'corner' | 'large') {
    this.screenStage = stage;
    storage.setItem('brrp.screen-stage', stage);
  }

  bindScreenElement(element: HTMLVideoElement | null) {
    this.screenElement = element;
    this.renderScreen();
    return element;
  }

  private renderScreen() {
    const element = this.screenElement;
    const stream = this.screenStream;
    if (!element || !stream) return;
    // Re-assigning the same object would restart playback, so this only writes when the
    // element is not already showing this stream.
    if (element.srcObject === stream) return;
    element.srcObject = stream;
    element.play().catch(() => {
      // A browser that will not autoplay leaves the element paused with a picture on
      // it, which is better than nothing, and the person can press play.
    });
  }

  private async createPeer(accepted: {
    channel_id: string;
    ice_servers: RTCIceServer[];
    receive_slots: number;
    bitrate: number;
    resume_token: string;
    connection_id: string;
    force_relay?: boolean;
    relay_available?: boolean;
  }) {
    if (!this.stream || accepted.channel_id !== this.channelId) return;
    this.rejoining = false;
    this.resumeToken = accepted.resume_token;
    this.bitrate = accepted.bitrate;
    // `relay` forbids direct candidates, so every packet goes through the server's
    // TURN relay. The server only asks for it when the relay is actually configured —
    // asking for a relay that does not exist would leave the client with no path at
    // all, which is worse than the direct path it was trying to give up.
    const forced = accepted.force_relay === true && accepted.relay_available === true;
    const peer = new RTCPeerConnection({
      iceServers: accepted.ice_servers,
      iceTransportPolicy: forced ? 'relay' : 'all'
    });
    this.peer = peer;
    peer.onicecandidate = (event) => {
      if (peer === this.peer && event.candidate)
        workspace.sendEvent('rtc.signal', {
          ice: event.candidate.toJSON(),
          connection_id: accepted.connection_id
        });
    };
    peer.ontrack = (event) => {
      if (peer !== this.peer) return;
      // A screen and a voice arrive on the same connection, and a client that played a
      // desktop through an <audio> element would hear nothing and show nothing.
      if (event.track.kind === 'video' || isViewOnlyTrack(event.streams[0])) {
        this.attachScreen(event.streams[0]);
        return;
      }
      const audio = new Audio();
      audio.autoplay = true;
      audio.srcObject = new MediaStream([event.track]);
      this.audios.push(audio);
      this.applyOutputDevice(audio);
      // A voice arrives in a numbered slot whose speaker the server routes and re-routes;
      // a screen's sound arrives on a stream named for the account sharing it. Each is
      // remembered as what it is, so the listener's volume for a person reaches both.
      const streamId = event.streams[0]?.id || '';
      const sharer = screenAudioOwner(streamId);
      if (sharer) this.audioOwners.set(audio, sharer);
      else this.audioSlots.set(audio, Number(streamId.replace('voice-', '')));
      this.applyAudioState();
      audio.play().catch(() => (this.audioBlocked = true));
    };
    peer.onconnectionstatechange = () => {
      if (peer !== this.peer) return;
      if (peer.connectionState === 'connected') {
        this.status = 'connected';
        this.retries = 0;
        clearTimeout(this.disconnectTimer);
        clearTimeout(this.joinTimer);
        this.sendState();
        this.startStats();
        if (!this.hasConnected) {
          this.hasConnected = true;
          this.playJoinChime();
        }
      } else if (['disconnected', 'failed'].includes(peer.connectionState)) {
        this.status = 'connecting';
        clearTimeout(this.disconnectTimer);
        this.disconnectTimer = setTimeout(() => this.reconnect(), 4000);
      }
    };
    peer.addTransceiver(this.stream.getAudioTracks()[0], { direction: 'sendonly' });
    for (let index = 0; index < accepted.receive_slots; index++)
      peer.addTransceiver('audio', { direction: 'recvonly' });
    this.applyAudioState();
    const offer = await peer.createOffer();
    await peer.setLocalDescription(offer);
    if (peer === this.peer)
      workspace.sendEvent('rtc.signal', {
        sdp: peer.localDescription?.toJSON(),
        connection_id: accepted.connection_id
      });
  }

  private reconnect() {
    if (!this.channelId || !this.stream) return;
    if (workspace.connection !== 'connected') {
      this.quality = 'Waiting for connection…';
      return;
    }
    if (this.retries++ >= 1) {
      this.error = 'Voice disconnected. Join again when your connection is stable.';
      this.leave();
      return;
    }
    this.rejoining = true;
    this.generation++;
    this.status = 'connecting';
    this.closePeer();
    workspace.sendEvent('voice.leave', {});
    workspace.sendEvent('voice.join', { channel_id: this.channelId });
    this.joinTimer = setTimeout(() => {
      if (this.status !== 'connected') {
        this.error = 'Voice could not reconnect.';
        this.leave();
      }
    }, 20000);
  }

  private startMonitor() {
    if (!this.audioContext || !this.stream) return;
    clearInterval(this.monitor);
    this.monitorSource?.disconnect();
    const analyser = this.audioContext.createAnalyser();
    analyser.fftSize = 256;
    this.monitorSource = this.audioContext.createMediaStreamSource(this.stream);
    this.monitorSource.connect(analyser);
    const samples = new Uint8Array(analyser.fftSize);
    let lastEvent = 0;
    this.monitor = setInterval(() => {
      analyser.getByteTimeDomainData(samples);
      const power =
        samples.reduce((sum, sample) => sum + ((sample - 128) / 128) ** 2, 0) / samples.length;
      const speaking =
        Math.sqrt(power) > 0.035 &&
        !this.muted &&
        !this.deafened &&
        !this.ownState?.admin_muted &&
        !this.ownState?.admin_deafened &&
        (!this.pushToTalk || this.talkHeld);
      this.inputLevel = Math.min(1, Math.sqrt(power) * 6);
      if (speaking !== this.speaking && Date.now() - lastEvent > 300) {
        this.speaking = speaking;
        lastEvent = Date.now();
        this.sendState();
      }
    }, 100);
  }

  toggleMute() {
    this.muted = !this.muted;
    this.applyAudioState();
    this.sendState();
  }
  toggleDeafen() {
    this.deafened = !this.deafened;
    this.applyAudioState();
    this.sendState();
  }
  private sendState() {
    workspace.sendEvent('voice.state', {
      muted: this.muted,
      deafened: this.deafened,
      speaking: this.speaking
    });
  }

  private applyAudioState() {
    const deafened = this.deafened || !!this.ownState?.admin_deafened;
    if (deafened) this.chime?.stop();
    const muted =
      this.muted || deafened || !!this.ownState?.admin_muted || (this.pushToTalk && !this.talkHeld);
    this.stream?.getAudioTracks().forEach((track) => (track.enabled = !muted));
    this.audios.forEach((audio) => {
      audio.muted = deafened;
      const user = this.audioOwners.get(audio) ?? this.routes[this.audioSlots.get(audio) ?? -1];
      audio.volume = this.participantVolumes[user] ?? 1;
    });
  }

  private playJoinChime() {
    if (!this.joinSounds || this.deafened || this.ownState?.admin_deafened) return;
    if (this.chime && !this.chime.play(this.soundVolume)) this.audioBlocked = true;
  }

  async enableAudio() {
    try {
      await this.audioContext?.resume();
      await Promise.all(this.audios.map((audio) => audio.play()));
      this.audioBlocked = false;
    } catch {
      this.error = 'Your browser is still blocking audio playback.';
    }
  }

  private fail(error: unknown) {
    this.error = errorMessage(error);
    this.leave();
  }

  // settleAnswer releases the offer waiting for this connection's next answer. It is
  // called when a description is applied (the answer arrived) and when the connection
  // is closed (the answer never will).
  private settleAnswer() {
    const settle = this.pendingAnswer;
    this.pendingAnswer = null;
    settle?.();
  }

  /**
   * Runs one offer/answer exchange with exclusive hold of the connection's negotiation
   * state: the previous exchange finishes (or times out) before this offer is created,
   * and this exchange receives its answer before the next one starts.
   *
   * The message chain (`signalChain`) is not enough on its own: it orders processing,
   * not the *pairing*. Creating a second offer while the first waits for its answer
   * leaves the connection in `have-local-offer`, and the first answer is then refused —
   * which is what "Voice negotiation failed" was, the moment a viewer request and a
   * screen publish overlapped (review 17, two-browser screen journey).
   */
  private async negotiate(describe: () => Promise<boolean>): Promise<boolean> {
    const generation = this.generation;
    const previous = this.negotiation;
    let release: () => void = () => {};
    this.negotiation = new Promise<void>((resolve) => (release = resolve));
    try {
      await previous;
      if (generation !== this.generation || !this.peer) return false;
      // A bounded wait for an answer that never came: the exchange times out on its
      // own, and the next one proceeds rather than being refused forever.
      const sent = await describe();
      if (!sent) return false;
      await new Promise<void>((resolve) => {
        this.pendingAnswer = resolve;
        setTimeout(() => {
          if (this.pendingAnswer === resolve) {
            this.pendingAnswer = null;
            resolve();
          }
        }, 4000);
      });
      return true;
    } catch {
      return false;
    } finally {
      release();
    }
  }

  private closePeer() {
    this.settleAnswer();
    const peer = this.peer;
    this.peer = null;
    peer?.close();
    this.audios.forEach((audio) => {
      audio.pause();
      audio.srcObject = null;
    });
    this.audios = [];
    this.audioSlots.clear();
    this.audioOwners.clear();
    clearInterval(this.statsTimer);
    this.candidates = [];
  }

  leave(notify = true) {
    this.generation++;
    if (notify && this.channelId) workspace.sendEvent('voice.leave', {});
    this.channelId = '';
    this.resumeToken = '';
    this.resuming = false;
    this.connectionId = '';
    this.routingVersion = -1;
    this.routes = [];
    this.signalChain = Promise.resolve();
    this.monitorSource?.disconnect();
    this.monitorSource = null;
    this.movingTo = '';
    this.quality = '';
    this.inputLevel = 0;
    this.talkHeld = false;
    clearTimeout(this.resumeTimer);
    clearTimeout(this.moveTimer);
    this.status = 'disconnected';
    this.hasConnected = false;
    this.rejoining = false;
    this.retries = 0;
    this.speaking = false;
    this.audioBlocked = false;
    clearInterval(this.monitor);
    clearTimeout(this.disconnectTimer);
    clearTimeout(this.joinTimer);
    clearTimeout(this.captureTimer);
    this.closePeer();
    this.stream?.getTracks().forEach((track) => track.stop());
    this.stream = null;
    // A share does not outlive the call it was published to. The capture is released
    // here rather than left running: the browser's sharing indicator would stay on
    // with nothing receiving it, and the next call would render a stale picture.
    this.capture?.getTracks().forEach((track) => track.stop());
    this.capture = null;
    this.screenStream = null;
    this.screenShared = false;
    this.screenState = 'idle';
    this.screenMessage = '';
    this.publishedScreen = false;
    this.publishedScreenAudio = false;
    this.screenRequested = false;
    this.chime?.stop();
    this.chime = null;
    this.audioContext?.close().catch(() => {});
    this.audioContext = null;
  }

  saveAudioPreferences() {
    storage.setItem('brrp.microphone', this.microphone);
    storage.setItem('brrp.push-to-talk', String(this.pushToTalk));
    storage.setItem('brrp.join-sounds', String(this.joinSounds));
    this.soundVolume = Number.isFinite(this.soundVolume)
      ? Math.max(0, Math.min(1, this.soundVolume))
      : 0.7;
    storage.setItem('brrp.sound-volume', String(this.soundVolume));
    this.releaseTalk();
    this.applyAudioState();
  }

  releaseTalk() {
    this.talkHeld = false;
    this.applyAudioState();
  }
  holdTalk() {
    this.talkHeld = true;
    this.applyAudioState();
  }

  setParticipantVolume(id: string, volume: number) {
    this.participantVolumes[id] = Math.max(0, Math.min(1, volume));
    this.applyAudioState();
  }

  async changeMicrophone(id: string) {
    if (this.changingMicrophone) return;
    if (!this.channelId) {
      this.microphone = id;
      this.saveAudioPreferences();
      return;
    }
    const generation = this.generation;
    const change = ++this.microphoneChange;
    let stream: MediaStream | null = null;
    this.changingMicrophone = true;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          deviceId: id ? { exact: id } : undefined,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true
        },
        video: false
      });
      if (generation !== this.generation || change !== this.microphoneChange || !this.peer) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      const peer = this.peer;
      const sender = peer.getSenders().find((sender) => sender.track?.kind === 'audio');
      if (!sender) throw new Error('The microphone connection is not ready. Try again shortly.');
      const track = stream.getAudioTracks()[0];
      track.enabled = this.stream?.getAudioTracks()[0]?.enabled ?? false;
      await sender.replaceTrack(track);
      if (generation !== this.generation || peer !== this.peer) {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      this.stream?.getTracks().forEach((track) => track.stop());
      this.stream = stream;
      this.microphone = id;
      clearInterval(this.monitor);
      this.startMonitor();
      this.saveAudioPreferences();
    } catch (error) {
      stream?.getTracks().forEach((track) => track.stop());
      if (generation === this.generation) this.error = errorMessage(error);
    } finally {
      this.changingMicrophone = false;
    }
  }

  private startStats() {
    clearInterval(this.statsTimer);
    this.statsTimer = setInterval(async () => {
      const peer = this.peer;
      if (!peer || workspace.connection !== 'connected') return;
      try {
        const stats = await peer.getStats();
        if (peer !== this.peer) return;
        // Which candidate pair won is recorded as well as how fast it is. Latency
        // alone cannot distinguish a direct path from a relay, so a call that works on
        // the host and fails for everybody else — the signature of a relay advertising
        // a private address — looks identical to a good connection without this.
        // The candidate reports carry the type of each candidate; a pair names the two
        // it settled on. Both are read from the same stats snapshot, so the pair and
        // its candidates cannot come from different moments.
        const candidates = new Map<string, { candidateType?: string }>();
        stats.forEach((report) => {
          if (
            (report.type === 'local-candidate' || report.type === 'remote-candidate') &&
            report.id
          )
            candidates.set(report.id, report as { candidateType?: string });
        });
        let chosen:
          { local?: { candidateType?: string }; remote?: { candidateType?: string } } | undefined;
        stats.forEach((report) => {
          if (
            report.type === 'candidate-pair' &&
            report.state === 'succeeded' &&
            report.nominated &&
            report.currentRoundTripTime != null
          ) {
            const latency = Math.round(report.currentRoundTripTime * 1000);
            const local = report.localCandidateId
              ? candidates.get(report.localCandidateId)
              : undefined;
            const remote = report.remoteCandidateId
              ? candidates.get(report.remoteCandidateId)
              : undefined;
            chosen = { local, remote };
            this.quality = `${latency < 150 ? 'Good' : latency < 300 ? 'Fair' : 'Slow'} connection · ${latency} ms`;
          }
        });
        if (chosen?.local || chosen?.remote) {
          // `relay` means the pair went through a TURN server. It is named explicitly
          // because a support conversation about a slow call almost always needs to
          // know whether the answer is "it is relayed" before it needs the round trip.
          this.connectionPath = describePath(chosen.local, chosen.remote);
        }
      } catch {
        /* A closing peer no longer has statistics. */
      }
    }, 3000);
  }

  /**
   * Starts sharing a screen into the call already joined.
   *
   * It never throws and never blocks: a refusal, a dismissal, an insecure page and a
   * genuine failure are all states, not exceptions, because all four are ordinary and a
   * caller that had to catch them would eventually catch one it did not handle.
   */
  async startScreenShare(): Promise<void> {
    if (this.screenState === 'asking' || this.screenState === 'sharing') return;
    if (!this.channelId) {
      // Sharing into no call is not a thing, and saying so is more useful than a
      // capture prompt that leads nowhere.
      this.screenState = 'failed';
      this.screenMessage = 'Join a voice conversation before sharing your screen.';
      return;
    }
    this.screenState = 'asking';
    this.screenMessage = screenShareMessage('asking');
    // Everything that identifies who the share would go to, read *before* the browser's
    // own modal opens. The modal blocks nothing in the app: the person can switch
    // account, leave, move to another call, or lose the permission while it is open, and
    // the stream that comes back belongs to the moment they opened it.
    const started: CaptureAudience = {
      channelId: this.channelId,
      userId: workspace.user?.id ?? '',
      connectionId: this.connectionId ?? '',
      generation: this.generation
    };
    // System audio is asked for, never required. A browser or platform that cannot
    // provide it returns video-only rather than failing the whole share, and the
    // user is told what they got rather than what they asked for.
    const result = await requestScreen(this.wantSystemAudio, this.screenPreset);
    if (result.state === 'sharing' && result.stream) {
      // The words describe what was delivered, not what was asked for. Asking for
      // audio and not getting it is an ordinary outcome, and the person sharing is the
      // only one who can act on it.
      const audio = screenAudioMessage(
        screenAudioOutcome(this.wantSystemAudio, result.stream.getAudioTracks().length)
      );
      if (audio) this.screenMessage = audio;
    }
    this.screenState = result.state;
    this.screenLabel = result.label ?? '';
    this.screenMessage = result.error || screenShareMessage(result.state);
    if (result.state !== 'sharing' || !result.stream) return;

    // The audience check, before anything is held. Publishing a stale capture would
    // broadcast a screen into a room the person had already left, or as an account that
    // no longer holds the permission — so the returned tracks are stopped and the
    // browser's indicator is released rather than renegotiated.
    const wanted = captureStillWanted(started, {
      channelId: this.channelId,
      userId: workspace.user?.id ?? '',
      connectionId: this.connectionId ?? '',
      generation: this.generation
    });
    if (!wanted.ok) {
      stopScreen(result.stream);
      this.screenState = 'failed';
      this.screenMessage = wanted.reason;
      return;
    }

    this.screenStream = result.stream;
    this.capture = result.stream;
    // The browser can end a share without telling us through any path we control:
    // the user stops it in their own UI, the tab closes, or the browser revokes the
    // capture. This is the only signal for all three, and without it a stopped share
    // keeps the indicator off and the stream running.
    this.releaseScreenWatch?.();
    this.releaseScreenWatch = onCaptureEnded(result.stream, () => {
      void this.stopScreenShare();
    });
    // The capture has to be *published*, not merely held. The server decides whether
    // this account may share — the room's screen bound and this account's own
    // permission are both the server's call — and it answers with the publication id
    // the rest of the negotiation is built on.
    const published = await this.publishScreen(result.stream);
    if (!published) {
      // The server refused. The browser's own indicator is still on, and leaving it on
      // while telling nobody is the worst outcome: the person believes they are sharing
      // to a room that is not receiving them.
      stopScreen(result.stream);
      this.screenStream = null;
      // The refusal also has to let go of the moment's hold on the client state: the
      // capture watch would still react to a share that is not running, and `capture`
      // would still tell `attachScreen` to bow out of every later share until the call
      // ended — which is the blindness this comment had been written to warn about.
      this.releaseScreenWatch?.();
      this.releaseScreenWatch = null;
      this.capture = null;
      this.screenState = 'failed';
      this.screenMessage =
        'Your screen was not shared. Join a conversation where you can share, and try again.';
    }
  }

  /**
   * Adds the captured screen as a publishing video track and renegotiates.
   *
   * The track's stream id is the *publishing* one, deliberately not the view marker:
   * that marker is how the server tells a viewer from a publisher, and using it here
   * would make this client's own share read as a view of somebody else's.
   */
  private async publishScreen(stream: MediaStream): Promise<boolean> {
    const track = stream.getVideoTracks()[0];
    if (!track) return false;
    // The negotiation goes through the offer/answer queue, like the receive path: an
    // offer created while another one waits for its answer is refused (review 17,
    // two-browser screen journey).
    return await this.negotiate(async () => {
      const peer = this.peer;
      if (!peer) return false;
      this.publishScreenAudio(stream, peer);
      if (this.screenSender) {
        // Already publishing: replace the track rather than adding a second
        // transceiver, so a restart is a swap and not a second screen.
        await this.screenSender.replaceTrack(track);
      } else {
        this.screenSender = peer.addTransceiver(track, { direction: 'sendonly' }).sender;
        this.publishedScreen = true;
      }
      const offer = await peer.createOffer();
      await peer.setLocalDescription(offer);
      if (peer !== this.peer) return false;
      // The preset is applied to the sender that now exists: the ceiling and the
      // degradation preference are properties of the negotiated encoding, so they can
      // only be set once there is one. Best-effort — a browser that refuses still
      // shares, and the capture constraints already asked for the resolution and rate.
      await applyScreenPreset(this.screenSender, this.screenPreset);
      workspace.sendEvent('rtc.signal', {
        sdp: peer.localDescription?.toJSON(),
        connection_id: this.connectionId,
        publication: 'screen_video'
      });
      // The audio publication is named *without* the description. Its track travelled
      // inside the screen's own offer — one m-line among several — and naming it is
      // what makes the room's forwarder route it. Re-sending the same offer produced a
      // second answer for a description that was already applied, which the browser
      // refuses and which tore the call down.
      if (this.publishedScreenAudio) {
        workspace.sendEvent('rtc.signal', {
          connection_id: this.connectionId,
          publication: 'screen_audio'
        });
      }
      return true;
    });
  }

  /**
   * Adds the capture's system audio as its own publication.
   *
   * It is best-effort by construction: a capture with no audio track is the ordinary
   * case on Windows and on Linux, and a share of the screen without its sound is far
   * more useful than no share at all. A failure here therefore leaves the video
   * alone rather than failing the whole operation.
   */
  private publishScreenAudio(stream: MediaStream, peer: RTCPeerConnection): void {
    const track = stream.getAudioTracks()[0];
    if (!track) {
      this.screenAudioSender = null;
      this.publishedScreenAudio = false;
      return;
    }
    if (this.screenAudioSender) {
      void this.screenAudioSender.replaceTrack(track);
      return;
    }
    try {
      const sender = peer.addTransceiver(track, { direction: 'sendonly' }).sender;
      // The screen-audio cap, applied here rather than left to the codec's default: the
      // server refuses anything past 64 kbit/s (ScreenAudioBitrateCeiling) and a client
      // that never approaches its own ceiling is a client that never meets the refusal.
      // Best-effort, like the video preset: a browser that refuses still shares the
      // sound, and the server's ceiling bounds it either way.
      try {
        const parameters = sender.getParameters();
        if (parameters.encodings.length > 0) {
          parameters.encodings[0] = { ...parameters.encodings[0], maxBitrate: 64_000 };
          void sender.setParameters(parameters).catch(() => {});
        }
      } catch {
        /* Same reasoning: the ceiling is the server's job, and it holds. */
      }
      this.screenAudioSender = sender;
      this.publishedScreenAudio = true;
    } catch {
      // The video is already publishing by this point. Losing the audio is a
      // degraded share, not a failed one, and saying so beats taking the screen away.
      this.screenAudioSender = null;
      this.publishedScreenAudio = false;
    }
  }

  /**
   * Releases the capture after the server withdrew permission, and says why.
   *
   * Separate from `stopScreenShare` because that one is a *choice* and this one is not:
   * the person did not decide to stop, and a message that reads like they did is worse
   * than no message. The existing screen state is left alone — it belongs to a share
   * that is over — and the reason is reported where the person is already looking.
   */
  private async releaseCaptureOnAuthorityChange(reason: string | undefined): Promise<void> {
    await this.stopScreenShare();
    this.error =
      reason ||
      'A moderator withdrew your permission to share in this conversation, so your screen is no longer being shared.';
  }

  /** Stops sharing and releases the browser's capture. Safe to call when not sharing. */
  async stopScreenShare(): Promise<void> {
    this.releaseScreenWatch?.();
    this.releaseScreenWatch = null;
    // The server is told, because the publication is the server's record: a room whose
    // screen publication is never removed stays "full" for the rest of the call, and
    // the next person to try to share is refused by a share that ended minutes ago.
    if (this.publishedScreen && this.connectionId) {
      workspace.sendEvent('rtc.signal', {
        connection_id: this.connectionId,
        publication: 'screen_video',
        publication_stopped: true
      });
      this.publishedScreen = false;
    }
    // The audio is withdrawn separately, for the same reason it was published
    // separately: it is its own publication with its own entry in the room's registry,
    // and stopping the video does not remove it.
    if (this.publishedScreenAudio && this.connectionId) {
      workspace.sendEvent('rtc.signal', {
        connection_id: this.connectionId,
        publication: 'screen_audio',
        publication_stopped: true
      });
      this.publishedScreenAudio = false;
    }
    this.screenAudioSender = null;
    if (this.screenStream) {
      stopScreen(this.screenStream);
      this.screenStream = null;
      this.capture = null;
    }
    // The track is stopped, but the transceiver stays: removing it would renegotiate
    // the connection again, and a share that restarts a moment later would then need a
    // third. A stopped track sends nothing, which is the property that matters.
    this.screenRequested = false;
    this.screenState = 'idle';
    this.screenLabel = '';
    this.screenMessage = '';
  }

  /** Whether this browser can share at all — decides whether the control is offered. */
  screenShareAvailable(): boolean {
    return canShareScreen();
  }

  /**
   * Chooses the capture profile, and applies it to a share that is already running.
   *
   * Waiting for the next share would be the wrong answer for the person who switches
   * because they cannot read the text in front of them now: the live sender's ceiling
   * and frame rate change immediately, and the capture is re-asked for the resolution
   * and rate the new preset wants. Both are best-effort, and the capture keeps running
   * if a browser refuses either.
   */
  async setScreenPreset(preset: ScreenPreset): Promise<void> {
    this.screenPreset = preset;
    storage.setItem('brrp.screen-preset', preset);
    if (this.screenState !== 'sharing' || !this.screenSender) return;
    await applyScreenPreset(this.screenSender, preset);
    const track = this.screenStream?.getVideoTracks()[0];
    if (track) {
      try {
        await track.applyConstraints(SCREEN_PRESETS[preset].constraints);
      } catch {
        // A display capture that will not take new constraints keeps the old size; the
        // sender's ceiling still changed, which is the part that bounds the cost.
      }
    }
  }
}

export const voice = new Voice();

/**
 * Adds a transceiver this client only receives on, marked so the server can tell a
 * viewer from a publisher.
 *
 * The marker is the stream id and nothing else. A receiving transceiver produces a
 * media section with no direction attribute, and the SDP default makes that
 * `sendrecv` — so without an explicit marker the server would conclude this client is
 * publishing, charge it against the room's one-screen bound, and refuse the share
 * because that client is not one of its authors.
 *
 * The marker goes on the receiver track of the transceiver this call just added, not
 * on "the first unmarked audio track": the server's own audio outputs also arrive as
 * receiver tracks, and marking one of those would label a participant's voice as a
 * screen. A kind that already has a marked transceiver is left alone, so a restart
 * does not stack a second m-line.
 *
 * A no-op when the client cannot add one, because a call that cannot receive a screen
 * is still a working voice call; failing the whole negotiation over it would be worse
 * than the missing picture.
 */
function addScreenReceive(peer: RTCPeerConnection, kind: 'video' | 'audio') {
  try {
    for (const transceiver of peer.getTransceivers()) {
      if (
        transceiver.receiver?.track?.kind === kind &&
        transceiver.receiver.track.id.startsWith(VIEW_STREAM_ID)
      )
        return;
    }
    // `recvonly` is the direction a viewer says it with. A client line marked
    // `sendonly` (which this used to send, carrying a track it never wrote to) told the
    // server the client meant to *send*, so the answer could never deliver a picture —
    // and a bare `recvonly` line used to be refused outright because the server decided
    // what an offer was from transceivers it had already created. The server now reads
    // the offer's own direction before applying it, so the client asks for exactly what
    // it wants (review 17, two-browser screen journey).
    const added = peer.addTransceiver(kind, { direction: 'recvonly' });
    // The stream id has to be on the track, and a browser builds the transceiver's
    // media description from the track, so the id is applied through the receiver the
    // transceiver creates rather than by editing the SDP — which is validated against
    // the offer that produced it.
    if (added.receiver?.track) {
      Object.defineProperty(added.receiver.track, 'id', {
        value: VIEW_STREAM_ID,
        configurable: true
      });
    }
  } catch (error) {
    // A browser that will not take it is a browser that will not show a screen. The
    // call continues; the screen control says the share could not be started.
    void error;
  }
}
