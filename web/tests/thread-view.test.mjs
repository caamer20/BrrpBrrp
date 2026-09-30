import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The send outcome decides what the composer offers next, and getting it wrong
// invites a person to send the same thing twice. A definite refusal wrote nothing, so
// the key is still free. Anything else may have committed, and a lost response must
// never look like a failure to save (ADR 0014).
import { threadSubmitState, canSendThreadReply } from '../src/lib/thread-state.ts';

const readSource = (file) => readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');
const readRepo = (file) => readFileSync(new URL(`../../${file}`, import.meta.url), 'utf8');

test('a refusal keeps the action available; an unknown outcome does not', () => {
  assert.equal(threadSubmitState(false, null), 'ready');
  assert.equal(threadSubmitState(false, { status: 400 }), 'refused');
  assert.equal(threadSubmitState(false, { status: 403 }), 'refused');
  assert.equal(threadSubmitState(false, { status: 429 }), 'refused');
  // No status at all is a transport failure: the request may have committed and the
  // response been lost, which is not the same as being told no.
  assert.equal(threadSubmitState(false, { message: 'network' }), 'uncertain');
  assert.equal(threadSubmitState(false, { status: 500 }), 'uncertain');
  assert.equal(threadSubmitState(true, null), 'ready');
});

test('a page is walked by its first row, not its last', () => {
  // The server returns each page oldest-first, so the next older page begins before
  // the first row. Taking the last re-reads the page just fetched, which is a cursor
  // walk that never terminates.
  const source = readSource('src/lib/threads.svelte.ts');
  assert.match(source, /before = page\[0\]\.id;/);
  assert.doesNotMatch(source, /before = page\[page\.length - 1\]\.id;/);
});

test('the send key is released only after the outcome is known', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  // The key is captured before the send and replaced after it, so a retry within the
  // same failure is the same operation rather than new work.
  assert.match(source, /const key = operationKey;/);
  assert.match(source, /await view\.send\(text, key\);/);
  assert.match(source, /operationKey = crypto\.randomUUID\(\);/);
});

test('a thread that is not found is not re-requested', () => {
  const source = readSource('src/lib/threads.svelte.ts');
  // Re-requesting a missing thread on every render would be a way to probe for the
  // existence of a channel the reader can no longer see.
  assert.match(source, /this\.missing = true;/);
  assert.match(source, /if \(this\.disposed \|\| this\.missing\) return;/);
});

test('the follow control reflects what the server decided', () => {
  const source = readSource('src/lib/threads.svelte.ts');
  // Reflecting the request rather than the answer would leave a refused follow
  // looking like it worked.
  assert.match(source, /following: result\.following/);
});

// The reply control is live only when there is somewhere to reply, the thread is
// writable, the person has typed something and no send is in flight. A locked or
// vanished thread must not offer a control whose handler cannot succeed.
test('the reply control follows the thread, not just the draft', () => {
  const ready = { open: true, locked: false, missing: false, sending: false, draft: 'hello' };
  assert.equal(canSendThreadReply(ready), true);
  assert.equal(canSendThreadReply({ ...ready, draft: '   ' }), false);
  assert.equal(canSendThreadReply({ ...ready, sending: true }), false);
  // A locked thread is still readable and followable, but nothing can be added to it.
  assert.equal(canSendThreadReply({ ...ready, locked: true }), false);
  // A thread that is gone offers nothing, rather than a control that fails on press.
  assert.equal(canSendThreadReply({ ...ready, missing: true }), false);
  assert.equal(canSendThreadReply({ ...ready, open: false }), false);
});

// A thread reply belongs to the thread, not to the channel it was branched off from.
// The server's channel page already excludes it; the client's own live-send path has to
// make the same decision, or a thread reply appears in the channel even though the
// stored message is correctly filed.
test('a thread reply is not added to the channel it was branched off from', () => {
  const source = readSource('src/lib/workspace.svelte.ts');
  // Filtered once, after the in-flight events are folded in and before the page is
  // split into its merge/replace branches. Before the reconciliation, a thread reply
  // that arrived while the page was loading slipped straight back in; in only one of
  // the branches, the other two disagree about the same message.
  assert.match(source, /loaded = loaded\.filter\(\(message\) => !message\.thread_id\)/);
  const filter = source.indexOf('loaded = loaded.filter((message) => !message.thread_id)');
  const reconcile = source.indexOf('change.message)');
  assert.ok(filter > reconcile, 'the filter runs before the in-flight events are folded in');
  // And at the one place every *live* event ends up. The page filter above cannot
  // cover this route: it fetches a single message by id and never passes through the
  // page. Two filters in the wrong two places left this third one to put a thread
  // reply back into its channel.
  assert.match(source, /if \(message\?\.thread_id && this\.channel\?\.id === channel\) return;/);
});

test('the server sends the thread id with a live message event', () => {
  // Without it the client cannot tell a thread reply from a channel message, and the
  // channel view would put it back. This is the defect the browser journey found.
  const source = readRepo('internal/api/websocket.go');
  assert.match(source, /"thread_id": message\.ThreadID/);
});

test('a stored message carries the thread it belongs to', () => {
  const source = readRepo('internal/db/messages.go');
  assert.match(source, /COALESCE\(thread_id,''\)/);
  assert.match(source, /&message\.ThreadID/);
});
