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

test('an activity entry leads to the same place', () => {
  const activity = read('src/lib/activity.svelte.ts');
  assert.match(activity, /return '#messages'/);
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
