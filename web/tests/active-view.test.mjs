import test from 'node:test';
import assert from 'node:assert/strict';
import { watchingNow } from '../src/lib/active-view.ts';

// "Is this person watching this conversation?" was answered twice and the answers
// disagreed. The read cursor asked about four conditions — tab visible, tab focused,
// scrolled to the newest message, not reading back through history — and the desktop
// notification asked about one: `document.hidden`.
//
// That difference is a notification banner over the conversation being read. A member
// with the tab open on another channel, or reading back through history in this one, is
// visible to the browser and was still interrupted. And it teaches people to dismiss
// banners without reading them, which is the opposite of what a notification is for.
//
// So the rule lives in one place and both callers read it.

const live = { visible: true, focused: true, atLatest: true, viewingHistory: false };

test('a member watching the live tail is watching', () => {
  assert.equal(watchingNow(live), true);
});

test('a backgrounded tab is not watching', () => {
  assert.equal(watchingNow({ ...live, visible: false }), false);
});

test('a visible tab in another window is not watching', () => {
  // The distinction the old check could not make: `document.hidden` is false for a
  // visible tab that has lost focus, so the notification fired for it.
  assert.equal(watchingNow({ ...live, focused: false }), false);
});

test('a member scrolled back through history is not watching', () => {
  assert.equal(watchingNow({ ...live, atLatest: false }), false);
});

test('a member deliberately reading history is not watching', () => {
  // Distinct from being scrolled away: this one is a mode the reader chose, and the
  // notification firing over it interrupts an intentional act.
  assert.equal(watchingNow({ ...live, viewingHistory: true }), false);
});

test('each condition is load-bearing on its own', () => {
  // A rule that ignored one of these would pass a suite that only ever removed one at a
  // time from a single scenario. Every single removal must flip the answer.
  for (const key of ['visible', 'focused', 'atLatest']) {
    assert.equal(watchingNow({ ...live, [key]: false }), false, `ignoring ${key}`);
  }
  assert.equal(watchingNow({ ...live, viewingHistory: true }), false, 'ignoring viewingHistory');
});

test('the answer is a strict boolean', () => {
  // A caller writing `!watchingNow(...)` and one writing `=== false` must agree. A
  // `&&` chain returns its first falsy operand, which is `undefined` for a missing field
  // and would make those two callers disagree about a state the type forbids.
  for (const field of ['visible', 'focused', 'atLatest']) {
    assert.equal(watchingNow({ ...live, [field]: undefined }), false, `missing ${field}`);
    assert.equal(typeof watchingNow({ ...live, [field]: undefined }), 'boolean');
  }
  // `viewingHistory` is the one condition phrased as its absence, so a missing value is
  // genuinely ambiguous: undefined means "we do not know". It is resolved to not-watching,
  // because the safe answer to not knowing is to interrupt rather than to stay silent.
  assert.equal(typeof watchingNow({ ...live, viewingHistory: undefined }), 'boolean');
  assert.equal(watchingNow({ ...live, viewingHistory: undefined }), true);
});
