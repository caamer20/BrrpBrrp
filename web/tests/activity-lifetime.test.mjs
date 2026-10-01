import test from 'node:test';
import assert from 'node:assert/strict';
import { admitted } from '../src/lib/admission.ts';

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
