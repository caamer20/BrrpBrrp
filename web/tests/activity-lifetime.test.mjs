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
// The server acknowledges one explicit set — the unacknowledged rows at or below the
// newest row the person was shown — and answers with exactly the ids it persisted. The
// client reflects exactly those ids. The earlier design sent the *oldest* loaded row as
// a boundary and painted the resulting count over the top of the list, which
// acknowledged rows nobody had seen and displayed rows as seen that the server still
// held unacknowledged — the badge came back on reload. That is review 17, V17-04, and
// these tests pin the identity contract that replaced it.

const row = (id, acknowledged = false) => ({ id, acknowledged, acknowledged_at: '' });
const page = () => [row('n3'), row('n2'), row('n1')]; // newest first

test('exactly the ids the server named become seen', () => {
  const marked = markAcknowledged(page(), new Set(['n1', 'n3']), 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [true, false, true],
    'the client marked by position instead of by the ids the server named'
  );
});

test('a row the server did not acknowledge is never shown as seen', () => {
  // The defect this prevents, stated directly: the server named only the newest row,
  // and the middle and oldest must stay unseen no matter where they sit on the page.
  const marked = markAcknowledged(page(), new Set(['n3']), 'T');
  assert.equal(marked[0].acknowledged, true);
  assert.equal(marked[1].acknowledged, false, 'an unnamed row was painted as seen');
  assert.equal(marked[2].acknowledged, false, 'an unnamed row was painted as seen');
});

test('marking nothing marks nothing', () => {
  const marked = markAcknowledged(page(), new Set(), 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [false, false, false]
  );
});

test('ids not on screen mark nothing and invent nothing', () => {
  const marked = markAcknowledged(page(), new Set(['elsewhere']), 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [false, false, false]
  );
  assert.equal(marked.length, 3);
});

test('an already-acknowledged row keeps its own timestamp', () => {
  const entries = [row('n3', true), row('n2'), row('n1')];
  entries[0].acknowledged_at = 'earlier';
  const marked = markAcknowledged(entries, new Set(['n3', 'n1']), 'T');
  assert.deepEqual(
    marked.map((entry) => entry.acknowledged),
    [true, false, true]
  );
  // The timestamp of an already-seen row records when the person saw it, not when
  // they pressed the button again.
  assert.equal(marked[0].acknowledged_at, 'earlier');
  assert.equal(marked[2].acknowledged_at, 'T');
});

test('marking stamps the acknowledged rows once, at one time', () => {
  const marked = markAcknowledged(page(), new Set(['n1', 'n2', 'n3']), '2026-01-01T00:00:00Z');
  for (const entry of marked) assert.equal(entry.acknowledged_at, '2026-01-01T00:00:00Z');
});

test('the server response is read as ids, and anything else names nothing', () => {
  // A count is not a set: an older server that answers with a number cannot say
  // which rows it marked, and painting any of them would be a guess the panel may not
  // make. Reflecting nothing leaves the rows honestly unseen until reload confirms them.
  assert.deepEqual([...acknowledgedRows(undefined)], []);
  assert.deepEqual([...acknowledgedRows(null)], []);
  assert.deepEqual([...acknowledgedRows(3)], []);
  assert.deepEqual([...acknowledgedRows('n1')], []);
  assert.deepEqual([...acknowledgedRows(['n1', '', null, 7, 'n2'])], ['n1', 'n2']);
});

test('marking an empty page is empty, not an error', () => {
  assert.deepEqual(markAcknowledged([], new Set(['n1']), 'T'), []);
  assert.deepEqual(markAcknowledged([], new Set(), 'T'), []);
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
