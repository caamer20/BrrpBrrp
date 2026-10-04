import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// Exercise the controller with inert media devices. No microphone is opened.
const source = await readFile(new URL('../src/lib/voice.svelte.ts', import.meta.url), 'utf8');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '')
  .replace('export const voice = new Voice();', 'export { Voice };');
// The screen-receive helpers are pure and are used on the attach path, so the real module
// is compiled into the same bundle rather than stood in for: a stub here would let the
// preference stop silencing the inbound picture without any test noticing.
const screenReceive = ts
  .transpileModule(
    await readFile(new URL('../src/lib/screen-receive.ts', import.meta.url), 'utf8'),
    { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }
  )
  .outputText.replace(/^export /gm, '');
const prelude = `
const $state = value => value;
// An in-memory device store, so a preference a test sets is a preference the store reads
// back rather than a call that went nowhere.
export const storedValues = {};
const storage = {
  getItem: key => (key in storedValues ? storedValues[key] : null),
  setItem: (key, value) => { storedValues[key] = String(value); },
  removeItem: key => { delete storedValues[key]; }
};
const window = {addEventListener:()=>{}, isSecureContext:true};
let capture = () => Promise.reject(new Error('no capture fixture'));
export const setCapture = next => capture = next;
const navigator = {mediaDevices:{getUserMedia:(...args)=>capture(...args)}};
class AudioContext { resume(){return Promise.resolve();} close(){return Promise.resolve();} }
// The call's audio elements, and the speaker they are sent to. A real one plays through
// the system; these record what they were asked to do.
export const sinkCalls = [];
class Audio {
  constructor(){ this.autoplay = false; this.srcObject = null; }
  play(){ return Promise.resolve(); }
  setSinkId(id){ sinkCalls.push(id); return Promise.resolve(); }
}
export let failSink = false;
export const setFailSink = next => failSink = next;
// One peer connection the test can drive, so the arrival of a participant is an event the
// test fires rather than a path it hopes works.
export const peers = [];
class RTCPeerConnection {
  constructor(){ this.connectionState = 'new'; peers.push(this); }
  addTransceiver(){}
  addTrack(){}
  async createOffer(){ return { type: 'offer', sdp: 'v=0' }; }
  async setLocalDescription(){ this.localDescription = { toJSON: () => ({ type: 'offer', sdp: 'v=0' }) }; }
  close(){ this.connectionState = 'closed'; }
  getSenders(){ return []; }
}
class MediaStream { constructor(tracks){ this.tracks = tracks || []; } getTracks(){ return this.tracks; } getVideoTracks(){ return this.tracks.filter(t => t.kind === 'video'); } getAudioTracks(){ return this.tracks.filter(t => t.kind === 'audio'); } }
// The test body runs in another module, so the two fixtures it needs to build are handed
// over rather than guessed at.
export const newAudio = () => new Audio();
export const newStream = (tracks) => new MediaStream(tracks);
const document = {addEventListener:()=>{}};
const errorMessage = error => String(error);
class VoiceChime {stop(){}}
export const workspace = {state:{channels:[{id:'old',type:'voice',max_voice_users:10},{id:'new',type:'voice',max_voice_users:10}],voice_states:[]},connection:'connected',user:{id:'me'},events:[],sendEvent(type,data){this.events.push({type,data});}};
// The screen-share module is stubbed at the seam the store uses at construction time.
// A class field initializer runs before any test, so an undefined name here fails the
// whole file rather than the one test that would have reached it.
export const SCREEN_PRESETS = {
  motion: {id:'motion', label:'Motion — 720p at 15 fps', constraints:{}, parameters:{}},
  text: {id:'text', label:'Text — 1080p at 5 fps', constraints:{}, parameters:{}}
};
export const screenPresetFromValue = value => (value === 'text' ? 'text' : 'motion');
export const presetApplications = [];
export const applyScreenPreset = async (sender, preset) => { presetApplications.push(preset); return true; };
// The capture seam, the way real tests drive a share without a browser picker: what the
// browser would have delivered, in one answer, from a stub rather than a hardware dialog.
export let requestedScreen = { state: 'cancelled' };
export const setRequestedScreen = next => { requestedScreen = next; };
export const requestScreen = async () => requestedScreen;
export const screenShareMessage = () => '';
export const screenAudioMessage = () => '';
export const screenAudioOutcome = () => '';
export const canShareScreen = () => true;
export const captureStillWanted = () => ({ ok: true });
export const stopScreen = stream => { for (const track of stream.getTracks()) track.stop?.(); };
export const stoppedWatchers = [];
export let releasedWatches = 0;
export const bumpReleased = () => { releasedWatches += 1; };
export const onCaptureEnded = (stream, end) => { return () => bumpReleased(); };
`;
const {
  Voice,
  workspace,
  setCapture,
  presetApplications,
  sinkCalls,
  setFailSink,
  newAudio,
  newStream,
  storedValues,
  peers,
  setRequestedScreen
} = await import(
  'data:text/javascript;base64,' +
    Buffer.from(prelude + screenReceive + compiled).toString('base64')
);

function connected() {
  const voice = new Voice();
  const track = {
    enabled: true,
    stopped: false,
    stop() {
      this.stopped = true;
    }
  };
  const stream = { getTracks: () => [track], getAudioTracks: () => [track] };
  const peer = {
    connectionState: 'connected',
    closed: false,
    close() {
      this.closed = true;
    }
  };
  voice.channelId = 'old';
  voice.status = 'connected';
  voice.hasConnected = true;
  voice.stream = stream;
  voice.peer = peer;
  voice.resumeToken = 'resume-secret';
  workspace.events = [];
  return { voice, track, stream, peer };
}

test('moving rooms keeps the microphone and media connection', async () => {
  const { voice, track, peer } = connected();
  await voice.join('new');
  assert.equal(track.stopped, false);
  assert.equal(peer.closed, false);
  assert.equal(voice.channelId, 'old');
  assert.deepEqual(workspace.events, [{ type: 'voice.move', data: { channel_id: 'new' } }]);
  voice.receive({ t: 'voice.move.error', d: { error: 'full' } });
  assert.equal(voice.channelId, 'old');
  assert.equal(voice.movingTo, '');
  voice.leave();
});

test('a brief signaling break resumes without stopping capture', () => {
  const { voice, track, peer } = connected();
  workspace.voiceDisconnected();
  assert.equal(track.stopped, false);
  assert.equal(peer.closed, false);
  workspace.voiceReconnected();
  assert.equal(workspace.events[0].type, 'voice.resume');
  voice.receive({ t: 'voice.resumed', d: { channel_id: 'old', resume_token: 'rotated' } });
  assert.equal(voice.resumeToken, 'rotated');
  assert.equal(voice.status, 'connected');
  voice.leave();
});

test('queued signaling from an old call cannot reach a replacement peer', async () => {
  const { voice } = connected();
  voice.connectionId = 'old-connection';
  let release;
  voice.signalChain = new Promise((resolve) => (release = resolve));
  voice.receive({
    t: 'rtc.signal',
    d: { connection_id: 'old-connection', sdp: { type: 'answer' } }
  });
  const queued = voice.signalChain;
  voice.leave(false);
  let remoteCalls = 0;
  voice.peer = {
    setRemoteDescription() {
      remoteCalls++;
    },
    close() {}
  };
  release();
  await queued;
  assert.equal(remoteCalls, 0);
  voice.leave(false);
});

test('losing access to a voice channel ends the stream with an explanation', () => {
  const { voice, track } = connected();
  voice.receive({
    t: 'voice.access_revoked',
    d: { channel_id: 'old', reason: 'Channel access changed.' }
  });
  assert.equal(voice.channelId, '');
  assert.equal(voice.status, 'disconnected');
  assert.equal(track.stopped, true);
  // The reason stays on screen: a silent disconnect looks like a network failure.
  assert.equal(voice.error, 'Channel access changed.');
});

test('push-to-talk and deafen keep outgoing capture disabled', () => {
  const { voice, track } = connected();
  voice.pushToTalk = true;
  voice.releaseTalk();
  assert.equal(track.enabled, false);
  voice.holdTalk();
  assert.equal(track.enabled, true);
  voice.toggleDeafen();
  assert.equal(track.enabled, false);
  voice.releaseTalk();
  voice.toggleDeafen();
  assert.equal(track.enabled, false);
  voice.leave();
});

test('an unanswered microphone request times out and late capture is stopped', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const voice = new Voice();
  t.after(() => voice.leave(false));
  workspace.events = [];
  let answer;
  setCapture(
    () =>
      new Promise((resolve) => {
        answer = resolve;
      })
  );
  const joining = voice.join('old');
  await Promise.resolve();
  assert.equal(voice.status, 'requesting');
  t.mock.timers.tick(30000);
  assert.equal(voice.status, 'disconnected');
  assert.match(voice.error, /microphone did not become ready/);
  let stopped = false;
  answer({
    getTracks: () => [
      {
        stop: () => {
          stopped = true;
        }
      }
    ]
  });
  await joining;
  assert.equal(stopped, true);
  assert.equal(
    workspace.events.some((event) => event.type === 'voice.join'),
    false
  );
});

test('cancelling microphone capture ignores its late answer and a fresh retry can join', async (t) => {
  const voice = new Voice();
  t.after(() => voice.leave(false));
  workspace.events = [];
  let answer;
  setCapture(
    () =>
      new Promise((resolve) => {
        answer = resolve;
      })
  );
  const first = voice.join('old');
  await Promise.resolve();
  voice.leave(false);
  let stopped = false;
  answer({
    getTracks: () => [
      {
        stop: () => {
          stopped = true;
        }
      }
    ]
  });
  await first;
  assert.equal(stopped, true);
  assert.equal(voice.status, 'disconnected');
  const track = { stop() {} };
  setCapture(async () => ({ getTracks: () => [track] }));
  voice.startMonitor = () => {};
  await voice.join('old');
  assert.equal(voice.status, 'connecting');
  assert.equal(workspace.events.filter((event) => event.type === 'voice.join').length, 1);
});

// A screen and the element that shows it arrive independently, and either can be first.
//
// The model attached the stream only when the stream arrived, looking for an element that
// the workspace creates when `screenShared` becomes true — so the very first share had no
// element yet. The stream was stored, the screen was announced as shared, and a blank
// rectangle appeared: no error, no retry, and nothing a viewer could distinguish from a
// share that failed to send.
//
// Both orders have to render. Asserting the first is the one that matters, because that
// is the case that was broken and it is the ordinary one on a slow join.

/** A video element stand-in that records what was attached and whether it was told to play. */
function videoElement() {
  const element = {
    srcObject: null,
    played: 0,
    play() {
      element.played++;
      return Promise.resolve();
    }
  };
  return element;
}

test('a screen that arrives before the element is rendered once the element exists', () => {
  const voice = new Voice();
  const stream = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(stream);
  // The element has not mounted yet: the share is announced and the stream held, but
  // nothing has been rendered, because there was nothing to render into.
  assert.equal(voice.screenShared, true, 'the share was not announced');
  assert.equal(voice.screenStream, stream, 'the stream was not kept');
  assert.equal(voice.screenElement, null);

  // Now the workspace mounts the element, which is what `bindScreenElement` is for.
  const element = videoElement();
  voice.bindScreenElement(element);
  assert.equal(voice.screenElement, element);
  assert.equal(element.srcObject, stream, 'the waiting stream was never attached');
  assert.equal(element.played, 1, 'the element was attached but never told to play');
});

test('a screen that arrives after the element is rendered immediately', () => {
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  const stream = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(stream);
  assert.equal(element.srcObject, stream);
  assert.equal(element.played, 1);
});

test('binding the same element twice does not restart playback', () => {
  // Svelte re-runs an action's update when its argument changes, and a re-render of the
  // workspace can bind again. Re-assigning the same `srcObject` restarts the stream, so a
  // share would visibly jump whenever something else on the page re-rendered.
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  const stream = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(stream);
  voice.bindScreenElement(element);
  voice.bindScreenElement(element);
  assert.equal(element.played, 1, 'playback was restarted by a redundant bind');
  assert.equal(element.srcObject, stream);
});

test('a share that arrives while audio-only is on is born silenced', () => {
  // The preference outlives one share: somebody watching on a phone with a data limit who
  // chose audio-only must not get one frame of the next share decoded before they can
  // choose again. The store applies the preference on the attach path.
  const voice = new Voice();
  const video = { enabled: true };
  voice.setScreenAudioOnly(true);
  voice.attachScreen({ getTracks: () => [video], getVideoTracks: () => [video] });
  assert.equal(video.enabled, false, 'the arriving picture was not silenced');
  voice.setScreenAudioOnly(false);
  assert.equal(video.enabled, true, 'turning the picture back on did not re-enable it');
});

test('the chosen speaker is applied to every audio element, present and arriving', async () => {
  // Where the browser has speakers at all. The choice has to reach the elements a call
  // already owns and the one a later participant arrives on: a person who joins after the
  // choice was made must not come out of a different speaker than the others.
  const voice = new Voice();
  const existing = newAudio();
  voice.audios.push(existing);
  await voice.setOutputDevice('speaker-2');
  assert.equal(voice.outputDevice, 'speaker-2');
  assert.equal(storedValues['brrp.output-device'], 'speaker-2');
  assert.deepEqual(sinkCalls, ['speaker-2'], 'the element already in the call was not moved');

  // The peer is built the way a real accept builds it: the store only negotiates for the
  // room it believes it is in, and only once it holds a microphone.
  voice.channelId = 'lounge';
  voice.stream = newStream([{ kind: 'audio' }]);
  await voice.createPeer({
    channel_id: 'lounge',
    resume_token: 'resume-secret',
    connection_id: 'peer-1',
    ice_servers: [],
    receive_slots: 1,
    force_relay: false,
    relay_available: false,
    bitrate: 24000,
    may_speak: true
  });
  const peer = peers[peers.length - 1];
  peer.ontrack({ track: { kind: 'audio' }, streams: [newStream([{ kind: 'audio' }])] });
  assert.deepEqual(
    sinkCalls,
    ['speaker-2', 'speaker-2'],
    'the participant who arrived after the choice came out of the default speaker'
  );
});

test('a speaker that is gone is forgotten, and said out loud', async () => {
  // A device id stored on one machine and carried to another names nothing. Silence with
  // no explanation is the one outcome a person cannot act on.
  const voice = new Voice();
  setFailSink(true);
  try {
    const element = newAudio();
    element.setSinkId = async () => {
      throw new Error('NotFoundError');
    };
    voice.audios.push(element);
    await voice.setOutputDevice('gone');
    assert.equal(voice.outputDevice, '');
    assert.equal(storedValues['brrp.output-device'], '');
    assert.match(voice.error, /speaker/i);
  } finally {
    setFailSink(false);
  }
});

test('a second share replaces the first on the same element', () => {
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  const first = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(first);
  const second = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(second);
  assert.equal(element.srcObject, second, 'the element still shows the previous share');
  assert.equal(element.played, 2, 'a new share was not started');
});

test('the element going away does not leave the model pointing at a detached node', () => {
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  voice.attachScreen({ getTracks: () => [], getVideoTracks: () => [] });
  voice.bindScreenElement(null);
  assert.equal(voice.screenElement, null);
  // A stream arriving after that is held rather than written into nothing, so a share
  // that arrives while the workspace is between renders is not lost. The detached element
  // keeps whatever it had — it is no longer on screen, and touching it would be the only
  // way to leak the model into a node nobody owns.
  const stream = { getTracks: () => [], getVideoTracks: () => [] };
  voice.attachScreen(stream);
  assert.equal(voice.screenStream, stream);
  assert.notEqual(element.srcObject, stream, 'a stream was written into a detached element');
  // And it renders as soon as an element is available again.
  const replacement = videoElement();
  voice.bindScreenElement(replacement);
  assert.equal(replacement.srcObject, stream);
});

// Choosing a capture profile applies it where it matters. A preference that is only
// remembered would change the *next* share, which is the wrong answer for the person
// who switches because they cannot read the text in front of them now (ENG-03).
test('choosing a preset applies it to the share that is running', async () => {
  const { voice } = connected();
  presetApplications.length = 0;
  voice.screenState = 'sharing';
  voice.screenSender = {
    getParameters: () => ({ encodings: [{}] }),
    setParameters: async () => {}
  };
  voice.screenStream = { getVideoTracks: () => [{ applyConstraints: async () => {} }] };
  await voice.setScreenPreset('text');
  assert.equal(voice.screenPreset, 'text');
  assert.deepEqual(presetApplications, ['text'], 'the live sender was not re-parameterized');

  // The control: with no share in progress the choice is remembered for the next one
  // and nothing is applied to a sender that is not there.
  voice.screenState = 'idle';
  await voice.setScreenPreset('motion');
  assert.equal(voice.screenPreset, 'motion');
  assert.deepEqual(presetApplications, ['text']);
});

// A *failed* publication. The room can refuse, the connection can drop mid-prompt, or the
// negotiation can report no peer. Whatever the cause, the client must let go of the share:
// it holds the browser's capture indicator as stop evidence only while it might deliver
// media, and dropping that hold on itself was how a refused publication left the client
// believing it shared - and permanently blind to everyone else's shares until the call
// ended, because `attachScreen` takes `if (this.capture) return` seriously.
test('a refused publication stops the capture and does not blind the room', async () => {
  const voice = new Voice();
  voice.channelId = 'old';
  voice.peer = null; // negotiation cannot send without one — the transport-level refusal
  const track = {
    kind: 'video',
    enabled: true,
    stopped: false,
    stop() {
      this.stopped = true;
    }
  };
  setRequestedScreen({ state: 'sharing', stream: newStream([track]) });
  await voice.startScreenShare();
  assert.equal(voice.screenState, 'failed');
  assert.match(voice.screenMessage, /not shared/i);
  // The browser indicator is released with the stream, and *nothing about the attempted
  // share tolerates later screen work*: not the capture guard, and not the capture-ended
  // watcher that would still react to a share that no longer exists.
  assert.equal(voice.screenStream, null, 'the refused stream stayed recorded');
  assert.equal(voice.capture, null, 'the stopped stream still claims to be captured');
  // A share somebody else starts later must still land. The original defect made this
  // assertion fail: `attachScreen` bows out whenever `capture` is set, so once refused,
  // every later publication rendered as nothing at all.
  const later = newStream([{ kind: 'video', enabled: true }]);
  voice.attachScreen(later);
  assert.equal(voice.screenStream, later, 'a later share was never attached');
  assert.equal(voice.screenShared, true);
});

test('a refused publication detaches the capture watch', async () => {
  const voice = new Voice();
  voice.channelId = 'old';
  voice.peer = null;
  const track = {
    kind: 'video',
    enabled: true,
    stopped: false,
    stop() {
      this.stopped = true;
    }
  };
  setRequestedScreen({ state: 'sharing', stream: newStream([track]) });
  await voice.startScreenShare();
  assert.equal(voice.screenState, 'failed');
  // The capture-ended watcher is registered before publication so the browser's own Z
  // button cleaning up can be heard. It must be released when the share is refused: an
  // unreleased watch answering a share that is not running never frees the indicator.
  assert.equal(voice.releaseScreenWatch, null);
});
