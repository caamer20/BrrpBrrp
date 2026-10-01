import test from 'node:test';
import assert from 'node:assert/strict';
import { admitted, acknowledgedRows, markAcknowledged } from '../src/lib/admission.ts';

// One admission question, asked by every fetch and every response check in the
// activity controller: is what I am about to do — or what just answered — still wanted?
//
// The four parts are not equal in weight. The account, the generation and the version
// are privacy fences: they keep one account's private activity metadata out of the next
// account's panel, and they must hold regardless of anything else. `alive` is
// different in kind. It exists to discard a response that arrives after a panel is gone,
// which is a statement about work *in flight*.
//
// The activity controller is a module-level singleton, so that distinction is the whole
// bug. Closing the Activity panel and opening it again mounts a new panel over the same
// object; when teardown set `alive` false permanently, the second open in a session
// admitted nothing, fetched nothing, and rendered an empty panel — no error, no retry,
// and from the outside indistinguishable from a server that returned nothing.
//
// So `alive` is per-mount, and these tests say what must and must not survive one.

const request = { account: 'u1', generation: 3, version: 5 };
const now = (over = {}) => ({
  account: 'u1',
  generation: 3,
  version: 5,
  alive: true,
  authorizationUnverified: false,
  ...over
});

test('work started under the current account, generation and version is admitted', () => {
  assert.equal(admitted(request, now()), true);
});

test('a panel that is not mounted admits nothing, which is what discards a late response', () => {
  assert.equal(admitted(request, now({ alive: false })), false);
});

test('a remount revives admission, so a second open in a session loads again', () => {
  // The defect: teardown was terminal, so after the first close the panel never loaded
  // again for the rest of the session.
  assert.equal(admitted(request, now({ alive: false })), false);
  assert.equal(admitted(request, now({ alive: true })), true);
});

test('a remount does not resurrect the response of a request made before it', () => {
  // Mounting again says there is a listener for *new* work. The version advanced during
  // teardown, so the old request no longer matches — which is the property that stops a
  // pre-unmount fetch repainting the panel.
  const beforeTeardown = request;
  const afterTeardown = { ...beforeTeardown, version: 6 };
  // The teardown advanced the controller's version, so the *new* current version is 6.
  // A request made before the teardown carries 5 and is refused by the version fence
  // rather than by `alive` — which is the point: remounting restored `alive`, and only
  // the version still separates this response from work started now.
  assert.equal(admitted(afterTeardown, now({ alive: false, version: 6 })), false);
  assert.equal(admitted(afterTeardown, now({ alive: true, version: 6 })), true);
  assert.equal(
    admitted(beforeTeardown, now({ alive: true, version: 6 })),
    false,
    'a pre-teardown request was admitted after the remount'
  );
});

test('the account fence holds across any number of mounts', () => {
  for (const alive of [true, false, true]) {
    assert.equal(
      admitted(request, now({ alive, account: 'u2' })),
      false,
      `another account was admitted with alive=${alive}`
    );
  }
});

test('the authority generation fence holds across a remount', () => {
  assert.equal(admitted(request, now({ generation: 4 })), false);
});

test('the version fence holds, so a reset invalidates work in flight', () => {
  assert.equal(admitted(request, now({ version: 6 })), false);
});

test('unverified authority admits nothing even when everything else matches', () => {
  // The state before the first snapshot arrives. Admitting here would show the previous
  // account's activity while the new account's authority is still unknown.
  assert.equal(admitted(request, now({ authorizationUnverified: true })), false);
});

test('every false must come from a stated mismatch, never from a missing field', () => {
  // A guard on the guard: `undefined === undefined` would make several of these pass
  // for the wrong reason if a field were renamed.
  assert.equal(admitted(request, now({ account: undefined })), false);
  assert.equal(admitted(request, now({ generation: undefined })), false);
  assert.equal(admitted(request, now({ version: undefined })), false);
  assert.equal(admitted(request, now({ alive: undefined })), false);
});

// Acknowledgment is a claim about what the person has seen, and the panel states it.
//
// The server acknowledges every row strictly older than the cursor, and reports how many
// it actually marked. The client used to mark its whole loaded page regardless — so a row
// the server did *not* acknowledge was shown with a "· seen" marker, and reverts to
// unseen on reload. That is the visible tell: a badge that comes back.
//
// The count is spent from the bottom of the page, because that is the direction the
// server acknowledged in.

const row = (id, acknowledged = false) => ({ id, acknowledged, acknowledged_at: '' });
const page = () => [row('n3'), row('n2'), row('n1')]; // newest first

test('only as many rows as the server marked become seen', () => {
  const marked = markAcknowledged(page(), 2, 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    // The bottom two, because the cursor is the oldest row loaded.
    [false, true, true]
  );
});

test('a row the server did not acknowledge is never shown as seen', () => {
  // The defect this prevents, stated directly.
  const marked = markAcknowledged(page(), 1, 'T');
  assert.equal(marked[0].acknowledged, false, 'the newest row was painted as seen');
  assert.equal(marked[1].acknowledged, false, 'the middle row was painted as seen');
  assert.equal(marked[2].acknowledged, true);
});

test('marking nothing marks nothing', () => {
  const marked = markAcknowledged(page(), 0, 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [false, false, false]
  );
});

test('marking more than are on screen marks the page and no further', () => {
  const marked = markAcknowledged(page(), 99, 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [true, true, true]
  );
  // Nothing beyond the page is invented, so a reload cannot reveal a row that was
  // counted but never existed in this list.
  assert.equal(marked.length, 3);
});

test('an already-acknowledged row does not consume the budget', () => {
  // The server reports rows it *changed*, so re-running the acknowledgment must not
  // spend the count twice on a row that was already marked.
  const entries = [row('n3', true), row('n2'), row('n1')];
  const marked = markAcknowledged(entries, 1, 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [true, false, true],
    'an already-seen row consumed the count'
  );
  // And the timestamp of an already-seen row is not rewritten: it records when the
  // person saw it, not when they pressed the button again.
  assert.equal(marked[0].acknowledged_at, '');
});

test('marking stamps the time once, not per row', () => {
  const marked = markAcknowledged(page(), 3, '2026-01-01T00:00:00Z');
  for (const entry of marked) assert.equal(entry.acknowledged_at, '2026-01-01T00:00:00Z');
});

test('a server that reports no count marks the page rather than under-claiming', () => {
  // Guessing low would leave rows the server did mark looking unseen and put the badge
  // back, which is the mistake this guess avoids.
  assert.equal(acknowledgedRows(undefined), Number.POSITIVE_INFINITY);
  assert.equal(acknowledgedRows(null), Number.POSITIVE_INFINITY);
  assert.equal(acknowledgedRows(-1), Number.POSITIVE_INFINITY);
  assert.equal(acknowledgedRows(1.5), Number.POSITIVE_INFINITY);
  assert.equal(acknowledgedRows('3'), Number.POSITIVE_INFINITY);
  assert.equal(acknowledgedRows(0), 0);
  assert.equal(acknowledgedRows(7), 7);
});

test('marking an empty page is empty, not an error', () => {
  assert.deepEqual(markAcknowledged([], 5, 'T'), []);
  assert.deepEqual(markAcknowledged([], 0, 'T'), []);
});

// An activity entry must be followable.
//
// `destination` used to return `#messages` for every entry, which is the inbox the
// entry is listed in. Pressing one sent the person back where they came from and left
// them to look — the same experience as a link that does nothing. A group message, an
// incoming request and a thread reply each have somewhere specific to be.

import { activityDestination } from '../src/lib/activity-destination.ts';

const entry = (over) => ({ id: 'a', kind: 'message', conversation_id: 'c1', ...over });

test('a direct or group message opens its conversation', () => {
  assert.equal(activityDestination(entry({})), '#conversation=c1');
});

test('a request opens the requests tab', () => {
  assert.equal(activityDestination(entry({ kind: 'request', conversation_id: 'r1' })), '#requests');
});

test('a conversation id is escaped, so a crafted id cannot produce a second destination', () => {
  const crafted = activityDestination(entry({ conversation_id: 'c1#requests' }));
  assert.ok(!crafted.startsWith('#conversation=c1#'), `the id was not escaped: ${crafted}`);
  assert.equal(crafted, '#conversation=' + encodeURIComponent('c1#requests'));
});

test('an entry with no conversation has nothing to open', () => {
  // The fact still happened, so the row stays — but it is not a link to a place that
  // no longer exists, and it must not fall back to the inbox it was listed in.
  assert.equal(activityDestination(entry({ conversation_id: '' })), '');
  assert.equal(activityDestination(entry({ conversation_id: undefined })), '');
});

test('every kind resolves to something rather than the inbox', () => {
  for (const kind of ['message', 'group_message', 'request']) {
    const target = activityDestination(entry({ kind, conversation_id: 'c1' }));
    assert.notEqual(target, '#messages', `${kind} still opens the inbox`);
    assert.ok(target.length > 0, `${kind} has no destination`);
  }
});
