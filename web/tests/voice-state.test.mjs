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
const prelude = `
const $state = value => value;
const storage = {getItem:()=>null,setItem:()=>{}};
const window = {addEventListener:()=>{}, isSecureContext:true};
let capture = () => Promise.reject(new Error('no capture fixture'));
export const setCapture = next => capture = next;
const navigator = {mediaDevices:{getUserMedia:(...args)=>capture(...args)}};
class AudioContext { resume(){return Promise.resolve();} close(){return Promise.resolve();} }
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
export const storedValues = {};
`;
const { Voice, workspace, setCapture, presetApplications } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + compiled).toString('base64')
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
  const stream = { getTracks: () => [] };
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
  const stream = { getTracks: () => [] };
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
  const stream = { getTracks: () => [] };
  voice.attachScreen(stream);
  voice.bindScreenElement(element);
  voice.bindScreenElement(element);
  assert.equal(element.played, 1, 'playback was restarted by a redundant bind');
  assert.equal(element.srcObject, stream);
});

test('a second share replaces the first on the same element', () => {
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  const first = { getTracks: () => [] };
  voice.attachScreen(first);
  const second = { getTracks: () => [] };
  voice.attachScreen(second);
  assert.equal(element.srcObject, second, 'the element still shows the previous share');
  assert.equal(element.played, 2, 'a new share was not started');
});

test('the element going away does not leave the model pointing at a detached node', () => {
  const voice = new Voice();
  const element = videoElement();
  voice.bindScreenElement(element);
  voice.attachScreen({ getTracks: () => [] });
  voice.bindScreenElement(null);
  assert.equal(voice.screenElement, null);
  // A stream arriving after that is held rather than written into nothing, so a share
  // that arrives while the workspace is between renders is not lost. The detached element
  // keeps whatever it had — it is no longer on screen, and touching it would be the only
  // way to leak the model into a node nobody owns.
  const stream = { getTracks: () => [] };
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
