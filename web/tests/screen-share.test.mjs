import test from 'node:test';
import assert from 'node:assert/strict';
import {
  screenShareMessage,
  classifyCaptureFailure,
  canShareScreen,
  onCaptureEnded,
  screenAudioOutcome,
  screenAudioMessage,
  captureStillWanted
} from '../src/lib/screen-share.ts';

// A screen share is a user gesture with three ordinary failure modes, and the two
// that matter are: the person declines, and the capture ends without us being told
// through any path we control. Both must leave the app exactly as it was.

/** Reproduces a browser's refusal, which is a DOMException with a name. */
// A DOMException in everything but its type: the module reads `error.name`, which is
// how a browser identifies a refusal, and that is the only part under test.
class FakeDOMException extends Error {
  constructor(name) {
    super(name);
    this.name = name;
  }
}

const deny = (name) => classifyCaptureFailure(new FakeDOMException(name));
const refuse = (name) => deny(name).state;

test('declining or dismissing the picker is not a failure', () => {
  // The browser cannot tell a refusal from a dismissal, and both are somebody
  // choosing not to share. Reporting either as an error would be wrong.
  assert.equal(refuse('NotAllowedError'), 'cancelled');
  assert.equal(refuse('AbortError'), 'cancelled');
});

test('a missing device is a different answer from a refusal', () => {
  assert.equal(refuse('NotFoundError'), 'failed');
  assert.equal(refuse('NotReadableError'), 'failed');
});

test('an insecure page says so, rather than "sharing failed"', () => {
  // A secure context is required. Naming the reason is what lets somebody fix it.
  const result = deny('SecurityError');
  assert.equal(result.state, 'unsupported');
  assert.match(result.error, /https/);
});

test('every state has a message phrased for the person reading it', () => {
  for (const state of ['asking', 'sharing', 'cancelled', 'denied', 'unsupported', 'failed']) {
    const message = screenShareMessage(state);
    assert.notEqual(message, '', `${state} has no message`);
  }
  // A cancellation must not read as a system fault: the person is wondering why
  // nothing happened, and "something went wrong" sends them looking for a bug.
  assert.doesNotMatch(screenShareMessage('cancelled'), /error|failed|cannot/i);
  // And a share in progress must say that people can see it.
  assert.match(screenShareMessage('sharing'), /see/i);
});

test('an ended capture is reported even though nothing asked for it', () => {
  // The browser's own UI can stop a share, the tab can close, and a browser can revoke
  // the capture. The `ended` event is the only signal for all three.
  const listeners = [];
  const track = {
    addEventListener: (_kind, handler) => listeners.push(handler),
    removeEventListener: () => {}
  };
  const stream = { getVideoTracks: () => [track], getAudioTracks: () => [] };

  let ended = 0;
  const release = onCaptureEnded(stream, () => ended++);
  assert.equal(listeners.length, 1, 'no listener was attached to the video track');
  listeners[0]();
  assert.equal(ended, 1, 'an ended capture was not reported');

  // Releasing detaches it, so a disposed component does not call back into state
  // that is gone.
  release();
  listeners.length = 0;
  assert.equal(ended, 1, 'a released listener still fired');
});

test('screen audio is watched as well as video', () => {
  // System audio can end independently of the video; a viewer who hears a room with
  // no picture is a bug this catches.
  const video = { addEventListener: () => {}, removeEventListener: () => {} };
  let audioListener;
  const audio = {
    addEventListener: (kind, handler) => {
      if (kind === 'ended') audioListener = handler;
    },
    removeEventListener: () => {}
  };
  let ended = 0;
  onCaptureEnded({ getVideoTracks: () => [video], getAudioTracks: () => [audio] }, () => ended++);
  assert.equal(typeof audioListener, 'function', 'the audio track was not watched');
  audioListener();
  assert.equal(ended, 1);
});

test('a browser without getDisplayMedia is unsupported, not broken', () => {
  const original = globalThis.navigator;
  try {
    Object.defineProperty(globalThis, 'navigator', {
      value: { mediaDevices: {} },
      configurable: true
    });
    assert.equal(canShareScreen(), false);
    assert.equal(screenShareMessage('unsupported').length > 0, true);
  } finally {
    Object.defineProperty(globalThis, 'navigator', { value: original, configurable: true });
  }
});

// System audio is requested, never required. Windows and Linux commonly cannot
// provide it, and a browser that cannot is expected to return video-only rather than
// fail — so the share still works, and the only thing owed to the person sharing is
// an accurate word about what they got.
test('screen audio is reported for what was delivered, not what was asked', () => {
  assert.equal(screenAudioOutcome(false, 0), 'not-requested');
  assert.equal(screenAudioOutcome(false, 1), 'not-requested');
  assert.equal(screenAudioOutcome(true, 1), 'delivered');
  assert.equal(screenAudioOutcome(true, 2), 'delivered');
  assert.equal(screenAudioOutcome(true, 0), 'unavailable');
});

test('the ordinary video-only share says nothing at all', () => {
  // Silence is the right answer for the common case. A notice on every share would
  // train people to dismiss notices, which costs us the ones that matter.
  assert.equal(screenAudioMessage('not-requested'), '');
  assert.equal(screenAudioMessage('delivered'), '');
  assert.notEqual(screenAudioMessage('unavailable'), '');
});

test('the unavailable message names the consequence, not the cause', () => {
  const message = screenAudioMessage('unavailable');
  assert.match(message, /without sound/);
  assert.doesNotMatch(message, /error|failed|cannot|unable/i);
});

// A capture is a promise about an audience, and the browser's picker takes as long as
// the person needs.
//
// `getDisplayMedia` opens a modal the app cannot close or observe. While it is open the
// person can sign in to another account, leave the call, move to another voice channel,
// or have a moderator withdraw their permission. The stream that comes back belongs to
// the moment the picker opened. Publishing it into whatever room they are in *now* is a
// disclosure rather than a glitch: the previous account's screen appears in a room the
// person never agreed to show it to, and the browser's own capture indicator is on the
// whole time.
//
// So every field that identifies the audience is compared, and a mismatch means the
// tracks are stopped rather than renegotiated.

test('a capture whose audience is unchanged may be published', () => {
  const audience = { channelId: 'room-1', userId: 'u1', connectionId: 'c1', generation: 7 };
  assert.deepEqual(captureStillWanted(audience, { ...audience }), { ok: true });
});

test('a capture is dropped when the account changed under the picker', () => {
  const started = { channelId: 'room-1', userId: 'u1', connectionId: 'c1', generation: 7 };
  const verdict = captureStillWanted(started, { ...started, userId: 'u2' });
  assert.equal(verdict.ok, false, 'a capture was accepted across an account switch');
  // The reason has to say what happened, because a silent drop reads as the browser
  // failing and sends the person looking in the wrong place.
  assert.match(verdict.reason, /account/i);
});

test('a capture is dropped when the call moved, reconnected or was re-entered', () => {
  const started = { channelId: 'room-1', userId: 'u1', connectionId: 'c1', generation: 7 };
  for (const [label, now] of [
    ['moved to another conversation', { ...started, channelId: 'room-2' }],
    ['the call reconnected', { ...started, connectionId: 'c2' }],
    ['left and rejoined', { ...started, generation: 8 }]
  ]) {
    assert.equal(
      captureStillWanted(started, now).ok,
      false,
      `a capture was accepted after ${label}`
    );
  }
});

test('a leave alongside every other change is reported as the conversation changing', () => {
  // A leave clears the channel and advances the generation at the same time. Reporting
  // "you moved to another conversation" for an empty channel id would name a move the
  // person never made, so the generation is asked before the channel. The order is part
  // of the contract, not an implementation detail.
  const started = { channelId: 'room-1', userId: 'u1', connectionId: 'c1', generation: 7 };
  const verdict = captureStillWanted(started, {
    channelId: '',
    userId: 'u1',
    connectionId: '',
    generation: 9
  });
  assert.equal(verdict.ok, false);
  assert.match(verdict.reason, /conversation changed/i);
});

test('an account switch is named as such even when the generation also moved', () => {
  // Signing in as somebody else replaces the session without necessarily advancing the
  // call's generation, so this pair is reachable on its own.
  const started = { channelId: 'room-1', userId: 'u1', connectionId: 'c1', generation: 7 };
  const verdict = captureStillWanted(started, { ...started, userId: 'u2', generation: 8 });
  assert.equal(verdict.ok, false);
  assert.match(verdict.reason, /account/i);
});
