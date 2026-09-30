import { storage } from './storage';
import { workspace } from './workspace.svelte';
import { errorMessage } from './api';
import type { RealtimeEvent } from './types';
import { VoiceChime } from './voice-chime';
import {
  canShareScreen,
  onCaptureEnded,
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
  movingTo = $state('');
  inputLevel = $state(0);
  changingMicrophone = $state(false);
  quality = $state('');
  // Screen sharing. The state is the *share's* state rather than the call's, because
  // somebody can be connected and not sharing, sharing and muted, or neither — and
  // collapsing them into one flag is how "I stopped sharing" ends up meaning "I left
  // the call".
  screenState = $state<ScreenShareState>('idle');
  screenLabel = $state('');
  screenMessage = $state('');
  private screenStream: MediaStream | null = null;
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
    const channel = workspace.state?.channels.find((channel) => channel.id === id);
    if (!channel || channel.type !== 'voice') return;
    const participants = workspace.state?.voice_states.filter((state) => state.channel_id === id);
    if ((participants?.length || 0) >= channel.max_voice_users) {
      this.error = `${channel.name} is full. Try another voice channel.`;
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
      this.error =
        error instanceof DOMException && error.name === 'NotAllowedError'
          ? 'Microphone access was denied. Allow it in your browser settings, then try again.'
          : errorMessage(error);
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

  private async createPeer(accepted: {
    channel_id: string;
    ice_servers: RTCIceServer[];
    receive_slots: number;
    bitrate: number;
    resume_token: string;
    connection_id: string;
  }) {
    if (!this.stream || accepted.channel_id !== this.channelId) return;
    this.rejoining = false;
    this.resumeToken = accepted.resume_token;
    this.bitrate = accepted.bitrate;
    const peer = new RTCPeerConnection({ iceServers: accepted.ice_servers });
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
        stats.forEach((report) => {
          if (
            report.type === 'candidate-pair' &&
            report.state === 'succeeded' &&
            report.nominated &&
            report.currentRoundTripTime != null
          ) {
            const latency = Math.round(report.currentRoundTripTime * 1000);
            this.quality = `${latency < 150 ? 'Good' : latency < 300 ? 'Fair' : 'Slow'} connection · ${latency} ms`;
          }
        });
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
    const result = await requestScreen();
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
  }

  /** Stops sharing and releases the browser's capture. Safe to call when not sharing. */
  async stopScreenShare(): Promise<void> {
    this.releaseScreenWatch?.();
    this.releaseScreenWatch = null;
    if (this.screenStream) {
      stopScreen(this.screenStream);
      this.screenStream = null;
    }
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
