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

test('a page is walked by its first row, and the window stays bounded', () => {
  const source = readSource('src/lib/threads.svelte.ts');
  // The server returns each page oldest-first, so the next older page begins before
  // the first row of the one just fetched — and the request names the oldest row the
  // window holds, because the window is what is on screen.
  assert.match(source, /const oldest = this\.messages\[0\]\.id;/);
  assert.match(source, /\?before=\$\{encodeURIComponent\(oldest\)\}/);
  assert.doesNotMatch(source, /this\.pages \+= 1/);
  assert.doesNotMatch(source, /for \(let i = 0; i < this\.pages; i\+\+\)/);
  // One page per "load earlier", and the overflow leaves from the newest end, so a
  // thousand-reply thread costs the same as a fifty-reply one.
  assert.match(source, /windowLimit = 200/);
  assert.match(source, /combined\.slice\(0, ThreadView\.windowLimit\)/);
  assert.match(source, /combined\.slice\(-ThreadView\.windowLimit\)/);
});

test('the composer keeps one immutable intent per unresolved operation', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  // The words being submitted select their own operation among every association the
  // composer recorded for this thread — not the first outbox entry, and not a text
  // search over the queue: retrying the newest reply must not rotate its key because
  // an older unresolved reply happened to come first (review 17, V17-01).
  assert.match(source, /workspace\.threadComposerOperations\(id\)/);
  // The decision itself is the shared helper — the same function the deterministic
  // identity tests call — so the panel cannot drift from the tested behavior.
  assert.match(source, /retryOperationKey\(unresolvedOperations, submitted\)/);
  assert.match(source, /import \{ threadSubmitState, canSendThreadReply, retryOperationKey \}/);
  // The old design asked the store for the first unresolved entry in the thread; that
  // lookup cannot tell one retry from another when a thread holds several.
  assert.doesNotMatch(source, /unresolvedThreadOperation\(/);
  // The association is written where the send is claimed, so a later submission of the
  // same words can find the key it was submitted under.
  const workspace = readSource('src/lib/workspace.svelte.ts');
  assert.match(workspace, /recordComposerOperation\(threadID, key, message\.content\)/);
  // And the durable record is immutable on the store's side too, so an edit can never
  // rewrite what the key means.
  assert.match(workspace, /recorded\.content !== content\.trim\(\)/);
  assert.match(workspace, /item\.key === message\.idempotency_key && !item\.recovery/);
  // Reusing a stored key must select only a still-unresolved operation at this
  // destination; a released key is not a retry.
  assert.match(
    workspace,
    /entry\.key === operation\.key &&\s*!entry\.recovery &&\s*entry\.thread_id === threadID/
  );
  // Released work is not retryable: every release ends the association with it.
  assert.match(workspace, /this\.forgetComposerOperations\(key\);/);
  // The live outbox must be read before any early return in the lookup: a derivation
  // over the answer depends on the entries, and returning early first would cache the
  // initial empty answer forever — the rendered journey caught exactly that.
  assert.match(
    workspace,
    /threadComposerOperations\(threadID: string\): ComposerOperation\[\] \{\s*const live = this\.outbox;/
  );
  assert.match(
    workspace,
    /private composerOperations = \$state\(new Map<string, ComposerOperation\[\]>\(\)\)/
  );
});

test('edited text after an uncertain send is announced as a new reply', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  // The unresolved operation stays in the thread for its own retry or discard, and the
  // edited words are not silently submitted under its key or presented as a retry.
  assert.match(source, /const newReplyNotice = \$derived\(/);
  assert.match(
    source,
    /!unresolvedOperations\.some\(\(operation\) => operation\.content === draft\.trim\(\)\)/
  );
  assert.match(source, /will be sent as a new reply/);
  // The warning also covers unconfirmed replies shown as pending rows, so it does not
  // depend on which session submitted them.
  assert.match(source, /unresolvedOperations\.length > 0 \|\| pending\.length > 0/);
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
// writable, the person has typed something and no send is in flight. A locked,
// archived or vanished thread must not offer a control whose handler cannot succeed.
test('the reply control follows the thread, not just the draft', () => {
  const ready = {
    open: true,
    locked: false,
    archived: false,
    missing: false,
    sending: false,
    draft: 'hello'
  };
  assert.equal(canSendThreadReply(ready), true);
  assert.equal(canSendThreadReply({ ...ready, draft: '   ' }), false);
  assert.equal(canSendThreadReply({ ...ready, sending: true }), false);
  // A locked thread is still readable and followable, but nothing can be added to it.
  assert.equal(canSendThreadReply({ ...ready, locked: true }), false);
  // Archived is the same refusal with a different resolution: reopen it first.
  assert.equal(canSendThreadReply({ ...ready, archived: true }), false);
  // A thread that is gone offers nothing, rather than a control that fails on press.
  assert.equal(canSendThreadReply({ ...ready, missing: true }), false);
  assert.equal(canSendThreadReply({ ...ready, open: false }), false);
});

// Archiving must withdraw the control and offer Reopen as its own action; the server
// refuses the same way, and both sides say which state blocks the reply.
test('an archived thread withdraws the composer and offers reopen', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  assert.match(source, /const archived = \$derived\(view\.thread\?\.state === 'archived'\)/);
  assert.match(source, /locked,\s*archived,/);
  assert.match(source, /onclick=\{\(\) => setState\('active'\)\}>Reopen</);
  assert.match(source, /\{:else if archived\}/);
  assert.match(source, /This thread is archived, so new replies are closed/);
});

// Reading a thread moves the thread's own position — the server keeps it — and never
// the channel's. The panel marks read only what is on screen at the live end.
test('reading a thread moves only that thread\u2019s position', () => {
  const view = readSource('src/lib/threads.svelte.ts');
  assert.match(view, /\/threads\/\$\{encodeURIComponent\(this\.id\)\}\/read/);
  assert.match(view, /last_read_message_id: messageID/);
  // No channel read call exists on this path at all.
  assert.doesNotMatch(view, /channels\/\$\{[^}]+\}\/read/);
  const panel = readSource('src/lib/components/NamedThreadPanel.svelte');
  assert.match(
    panel,
    /if \(\(!atLatest && !view\.atNewest\) \|\| !view\.messages\.length\) return;/
  );
  assert.match(panel, /void view\.markRead\(view\.messages\[view\.messages\.length - 1\]\.id\)/);
  // And the list shows the thread's own count.
  const list = readSource('src/lib/components/ThreadListPanel.svelte');
  assert.match(list, /thread\.unread > 0/);
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

// A thread reply must go through the same machinery as a channel message.
//
// It used to have its own HTTP call, which meant its own outbox (none), its own draft
// handling (none) and its own account fence (none). So a lost response, a refused
// send and an account switch all behaved differently inside a thread than in the
// channel it lives in — three cases that ought to be one case, and which a member
// experiences as "threads are the place my messages get lost".
//
// These are assertions about the shape of the code path, not about rendered output:
// the behaviour under test is *which* machinery a thread reply is handed to, and that
// is only observable in the source. A browser test here would assert that a request
// was made, not that it was made through the recoverable path.
test('a thread reply is sent through the shared, recoverable send', () => {
  const source = readSource('src/lib/threads.svelte.ts');
  // It no longer posts a reply itself — the messages path is only ever *read* here;
  // writes go through the workspace's shared send below.
  assert.doesNotMatch(
    source,
    /\/threads\/\$\{[^}]+\}\/messages`,\s*'POST'/,
    'the thread view still posts replies directly instead of using the shared send'
  );
  // ...it hands the caller's key to the workspace, which owns the receipt and outbox.
  assert.match(
    source,
    /sendIntoThread\(/,
    'the thread view does not route through the shared send'
  );
});

test('the shared send routes a thread reply to the thread, not the channel', () => {
  const source = readSource('src/lib/workspace.svelte.ts');
  // One place decides where a message operation goes, and a thread is a destination
  // like any other. Two places building a message URL is how they drift.
  assert.match(
    source,
    /messagePath\(message\)/,
    'the send no longer goes through one path builder'
  );
  assert.match(
    source,
    /message\.thread_id[\s\S]{0,120}\/threads\//,
    'a thread reply is not routed to its thread'
  );
});

test("the caller's key is used, so a retry reconciles rather than duplicating", () => {
  const source = readSource('src/lib/workspace.svelte.ts');
  // The workspace's own send generates a key; the thread path takes one. A thread
  // composer freezes its key for the life of an unresolved send, and rotating it
  // after a caught failure turns a send that may have committed into a second message.
  const body = source.slice(source.indexOf('async sendIntoThread'));
  const bounded = body.slice(0, body.indexOf('\n  async retryMessage'));
  assert.doesNotMatch(
    bounded,
    /crypto\.randomUUID\(\)/,
    "the shared thread send generates its own key instead of using the caller's"
  );
  assert.match(bounded, /idempotency_key: message\.idempotency_key/);
});

test('a thread composer acts on the named outcome, not on the absence of an error', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  const body = source.slice(source.indexOf('async function send()'));
  const bounded = body.slice(0, body.indexOf('\n  async function older'));
  // The resolved outcome decides. Inferring success from `sendError` being null made a
  // dropped send — one whose view no longer exists — run the success cleanup.
  assert.match(bounded, /const outcome = await view\.send\(text, key\);/);
  assert.match(bounded, /if \(outcome === 'sent'\) \{/);
  assert.match(bounded, /if \(outcome === 'dropped'\) return;/);
  // Clearing before the await meant a refusal and a lost response both left an empty
  // composer, and the next attempt then made the lost response a second message. Only
  // the text that was actually sent is cleared.
  const clearAt = bounded.indexOf("if (draft === text) draft = '';");
  const sentAt = bounded.indexOf("if (outcome === 'sent')");
  assert.ok(clearAt > sentAt, 'the composer clears its draft outside the sent outcome');
  assert.match(
    bounded,
    /workspace\.clearSentDraft\(draftSlot, submitted, owner\)/,
    'the committed send does not clear its saved draft'
  );
  // And a refused or uncertain outcome keeps the author's words, restoring them if the
  // composer was emptied while the request was in flight.
  assert.match(bounded, /if \(!draft\.trim\(\)\) draft = text;/);
});

test('a thread draft is captured through the account-owning store, not component state', () => {
  const source = readSource('src/lib/components/NamedThreadPanel.svelte');
  // Local panel state dies with the panel, so closing a thread to look at a channel
  // and coming back lost an unsent reply. The capture goes through the store, which is
  // account-scoped by the identity the panel mounted under — so a teardown after a
  // switch cannot write the text into the account that replaced it.
  assert.match(source, /workspace\.saveComposerDraft\(draftSlot, draft, \[\], owner\)/);
  assert.match(source, /workspace\.clearComposerDraft\(draftSlot, owner\)/);
  assert.match(source, /const owner = untrack\(\(\) => workspace\.composerOwner\(\)\)/);
  assert.match(source, /captureDraft\(\)/, 'the draft is never captured on close');
});
