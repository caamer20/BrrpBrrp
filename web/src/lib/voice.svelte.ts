import { diagnoseMicrophone, microphoneMessage, type MicrophoneDiagnosis } from './devices';
import { storage } from './storage';
import { workspace } from './workspace.svelte';
import { errorMessage } from './api';
import type { RealtimeEvent } from './types';
import { VoiceChime } from './voice-chime';
import { describePath } from './connection-path';
import { VIEW_STREAM_ID, isViewOnlyTrack } from './screen-receive';
import {
  canShareScreen,
  onCaptureEnded,
  screenAudioMessage,
  screenAudioOutcome,
  requestScreen,
  screenShareMessage,
  stopScreen,
  type ScreenShareState
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
  private releaseScreenWatch: (() => void) | null = null;
  microphone = $state(storage.getItem('brrp.microphone') || '');
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
      // after the share began has no video transceiver otherwise.
      if (event.t === 'screen.published') this.requestScreenReceive(event.d?.connection_id);
      else {
        this.screenStream?.getTracks().forEach((track) => track.stop());
        this.screenStream = null;
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
  private requestScreenReceive(connectionID?: string) {
    const peer = this.peer;
    if (!peer || !this.stream) return;
    if (this.screenRequested) return;
    this.screenRequested = true;
    addScreenReceive(peer, connectionID || this.connectionId);
    void (async () => {
      try {
        const offer = await peer.createOffer();
        await peer.setLocalDescription(offer);
        if (peer !== this.peer) return;
        workspace.sendEvent('rtc.signal', {
          sdp: peer.localDescription?.toJSON(),
          connection_id: this.connectionId
        });
      } catch {
        // The voice call continues. The share simply will not arrive, and the panel
        // says so rather than the call appearing broken.
        this.screenRequested = false;
      }
    })();
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
    this.screenStream?.getTracks().forEach((track) => track.stop());
    this.screenStream = stream;
    const element = this.screenElement;
    if (element) {
      element.srcObject = stream;
      element.play().catch(() => {
        // A browser that will not autoplay leaves the element paused with a picture on
        // it, which is better than nothing, and the person can press play.
      });
    }
    this.screenShared = true;
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
      const slot = Number((event.streams[0]?.id || '').replace('voice-', ''));
      this.audioSlots.set(audio, slot);
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
      const user = this.routes[this.audioSlots.get(audio) ?? -1];
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

  private closePeer() {
    const peer = this.peer;
    this.peer = null;
    peer?.close();
    this.audios.forEach((audio) => {
      audio.pause();
      audio.srcObject = null;
    });
    this.audios = [];
    this.audioSlots.clear();
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
    // System audio is asked for, never required. A browser or platform that cannot
    // provide it returns video-only rather than failing the whole share, and the
    // user is told what they got rather than what they asked for.
    const result = await requestScreen(this.wantSystemAudio);
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

    this.screenStream = result.stream;
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
    const peer = this.peer;
    if (!peer) return false;
    const track = stream.getVideoTracks()[0];
    if (!track) return false;
    try {
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
      workspace.sendEvent('rtc.signal', {
        sdp: peer.localDescription?.toJSON(),
        connection_id: this.connectionId,
        publication: 'screen_video'
      });
      // The audio publication is announced separately, because it is a separate
      // publication with its own bound. Announcing it inside the video's offer would
      // make the server count one thing twice.
      if (this.publishedScreenAudio) {
        workspace.sendEvent('rtc.signal', {
          sdp: peer.localDescription?.toJSON(),
          connection_id: this.connectionId,
          publication: 'screen_audio'
        });
      }
      return true;
    } catch {
      this.screenSender = null;
      return false;
    }
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
      this.screenAudioSender = peer.addTransceiver(track, { direction: 'sendonly' }).sender;
      this.publishedScreenAudio = true;
    } catch {
      // The video is already publishing by this point. Losing the audio is a
      // degraded share, not a failed one, and saying so beats taking the screen away.
      this.screenAudioSender = null;
      this.publishedScreenAudio = false;
    }
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
}

export const voice = new Voice();

/**
 * Adds a video transceiver this client only receives on, marked so the server can tell
 * a viewer from a publisher.
 *
 * The marker is the stream id and nothing else. A receiving transceiver produces a
 * media section with no direction attribute, and the SDP default makes that
 * `sendrecv` — so without an explicit marker the server would conclude this client is
 * publishing, charge it against the room's one-screen bound, and refuse the share
 * because that client is not one of its authors.
 *
 * A no-op when the client cannot add one, because a call that cannot receive a screen
 * is still a working voice call; failing the whole negotiation over it would be worse
 * than the missing picture.
 */
function addScreenReceive(peer: RTCPeerConnection, connectionID: string) {
  try {
    peer.addTransceiver('video', { direction: 'sendonly' });
    // The stream id has to be on the track, and a browser builds the transceiver's
    // media description from the track, so the id is applied through the receiver the
    // transceiver creates rather than by editing the SDP — which is validated against
    // the offer that produced it.
    for (const transceiver of peer.getTransceivers()) {
      if (
        transceiver.receiver?.track &&
        !transceiver.receiver.track.id.startsWith(VIEW_STREAM_ID)
      ) {
        Object.defineProperty(transceiver.receiver.track, 'id', {
          value: VIEW_STREAM_ID,
          configurable: true
        });
        break;
      }
    }
  } catch (error) {
    // A browser that will not take it is a browser that will not show a screen. The
    // call continues; the screen control says the share could not be started.
    void error;
  }
}
