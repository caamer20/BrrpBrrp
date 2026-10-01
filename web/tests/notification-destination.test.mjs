import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// A notification is only useful if pressing it goes somewhere. Both the payload the
// server sends and the route the client handles have to name the same thing, or a
// click lands on a path nothing recognises.

// The reader is rooted at the web directory, because that is where the tests run from.
// A repository-root file is named explicitly rather than by climbing out, so a wrong
// path fails loudly instead of reading a different file.
const webRoot = new URL('../', import.meta.url);
const rootAbove = new URL('../../', import.meta.url);
const read = (file) => readFileSync(new URL(file, webRoot), 'utf8');
const readRoot = (file) => readFileSync(new URL(file, rootAbove), 'utf8');

test('the server announces a destination the client acts on', () => {
  const server = readRoot('internal/api/activity.go');
  assert.match(server, /"url":\s*"#messages"/);
  // The old value was a bare path that no route matched, so a click navigated the app
  // to something it did not recognise and the activity was never opened.
  assert.doesNotMatch(server, /"url":\s*"\/messages"/);
});

test('the client handles that destination and clears it', () => {
  const client = read('src/lib/workspace.svelte.ts');
  assert.match(client, /location\.hash === '#messages'/);
  // The hash is cleared so a reload does not reopen the inbox out of nowhere.
  assert.match(client, /history\.replaceState/);
});

// The old version of this test read the controller's source and asserted it returned
// '#messages'. That is the behaviour this change removes: an activity entry sent the
// person to the inbox it was listed in rather than to the conversation it happened in,
// which is not a destination at all. The rule now lives in `activity-destination.ts` and
// is asserted by calling it — see `activity-lifetime.test.mjs`.
test('an activity entry is resolved by the shared rule, not by a constant', () => {
  const activity = read('src/lib/activity.svelte.ts');
  assert.match(
    activity,
    /activityDestination\(entry\)/,
    'the controller does not use the shared rule'
  );
  assert.doesNotMatch(activity, /return '#messages'/, 'an entry still opens the inbox');
});

test('the worker deduplicates on the tag the server actually sends', () => {
  const worker = read('public/service-worker.js');
  // The server supplies `tag`; the worker read `message_id`, a field the payload does
  // not have, so every notice arrived with an undefined tag and none were deduplicated.
  assert.match(worker, /notification\.tag/);
  assert.doesNotMatch(worker, /tag: notification\.message_id/);
  // And the retention is bounded, so a member returning after a week is not met with a
  // week of stale notices they have already seen.
  assert.match(worker, /24 \* 60 \* 60 \* 1000/);
});
