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
`;
const { Voice, workspace, setCapture } = await import(
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
