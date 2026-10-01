import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// A thread is read as a bounded window (review 17, V17-03). The claim under test is
// that a thousand-reply thread costs one page per "load earlier", that the window
// never grows past its bound, and that a deep link positions the window at the linked
// reply. The real controller is compiled against a scripted server implementing the
// same id-cursor semantics as `GET /api/threads/{id}/messages`.
const source = await readFile(new URL('../src/lib/threads.svelte.ts', import.meta.url), 'utf8');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '');
const prelude = `
const $state = value => value;
const errorMessage = error => (error && error.message) || '';
class APIError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}
export { APIError };
export const calls = [];
// A thousand replies, ids '0000'..'0999', so every cursor walk can be checked
// against a record that is bigger than any window may hold.
const total = 1000;
const pad = value => String(value).padStart(4, '0');
const reply = id => ({ id, content: 'reply ' + id, thread_id: 'thread-1', user_id: 'u1' });
export const keyFor = value => pad(value);
const api = async (path, method) => {
  calls.push({ path, method });
  if (path === '/threads/thread-1') return { id: 'thread-1', state: 'active', title: 'T', followers: 1, following: true };
  const match = /^\\/threads\\/thread-1\\/messages(?:\\?(.*))?$/.exec(path);
  if (match) {
    const params = new URLSearchParams(match[1] || '');
    const before = params.get('before');
    const after = params.get('after');
    let ids = Array.from({ length: total }, (_, index) => index);
    if (before !== null) ids = ids.filter(id => pad(id) < before);
    if (after !== null) ids = ids.filter(id => pad(id) > after);
    // The server takes the *nearest* fifty, newest first, then reverses.
    ids = ids.slice(-50).map(id => reply(pad(id)));
    return ids;
  }
  if (path.startsWith('/messages/')) {
    const id = path.slice('/messages/'.length);
    return reply(id);
  }
  throw new Error('unexpected request ' + path);
};
const workspace = { sendIntoThread: async () => ({ state: 'refused' }) };
`;
const { ThreadView, calls } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + compiled).toString('base64')
);
const pageSize = 50;
const windowLimit = 200;

function view() {
  const value = new ThreadView('thread-1');
  return value;
}

test('the initial window is one page and says whether older replies exist', async () => {
  calls.length = 0;
  const value = view();
  await value.load();
  assert.equal(value.messages.length, pageSize);
  assert.equal(value.messages[0].id, '0950');
  assert.equal(value.messages[pageSize - 1].id, '0999');
  assert.equal(value.hasOlder, true);
  assert.equal(calls.filter((call) => call.path.includes('/messages')).length, 1);
});

test('loading earlier fetches one page at a time and keeps the window bounded', async () => {
  calls.length = 0;
  const value = view();
  await value.load();
  assert.equal(value.atNewest, true, 'the newest page is the newest reply');
  for (let step = 0; step < 3; step++) await value.older();
  const messageCalls = calls.filter((call) => call.path.includes('/messages'));
  // One request per step, each naming the oldest row the window held — never a
  // re-walk of the accumulated pages.
  assert.equal(messageCalls.length, 4);
  assert.equal(messageCalls[1].path, '/threads/thread-1/messages?before=0950');
  assert.equal(messageCalls[2].path, '/threads/thread-1/messages?before=0900');
  assert.equal(messageCalls[3].path, '/threads/thread-1/messages?before=0850');
  assert.equal(value.messages.length, windowLimit);
  assert.equal(value.messages[0].id, '0800');
  assert.equal(value.messages[value.messages.length - 1].id, '0999');
  assert.equal(value.hasOlder, true);

  // One more page: the window stays at its bound and the overflow leaves the newest
  // end, which is where the reader is not looking — and the window is no longer at
  // the newest reply.
  await value.older();
  assert.equal(value.messages.length, windowLimit);
  assert.equal(value.messages[0].id, '0750');
  assert.equal(value.messages[value.messages.length - 1].id, '0949');
  assert.equal(value.atNewest, false);
});

test('the end of the thread stops the walk and says so', async () => {
  const value = view();
  await value.load();
  // Nineteen pages move the window's oldest row from 0950 to 0000.
  for (let step = 0; step < 19; step++) await value.older();
  assert.equal(value.messages[0].id, '0000');
  assert.equal(
    value.hasOlder,
    true,
    'the page that reached the first replies ended the walk early'
  );
  await value.older();
  assert.equal(value.hasOlder, false);
  assert.equal(value.messages[0].id, '0000', 'the exhausted walk moved the window');
  const requests = calls.filter((call) => call.path.includes('/messages')).length;
  await value.older();
  assert.equal(
    calls.filter((call) => call.path.includes('/messages')).length,
    requests,
    'an exhausted window fetched another page'
  );
});

test('a deep link positions the window at the linked reply', async () => {
  calls.length = 0;
  const value = view();
  await value.loadAround('0500');
  const paths = calls.map((call) => call.path);
  assert.ok(paths.includes('/threads/thread-1/messages?before=0500'));
  assert.ok(paths.includes('/threads/thread-1/messages?after=0500'));
  assert.ok(paths.includes('/messages/0500'));
  assert.equal(value.messages.length, 101);
  assert.ok(value.messages.some((message) => message.id === '0500'));
  assert.equal(value.hasOlder, true);
  assert.equal(value.atNewest, false, 'a link to an older reply claimed the newest end');
});

test('a deep link near the end still opens at its reply', async () => {
  const value = view();
  await value.loadAround('0999');
  assert.equal(value.messages.length, 51);
  assert.equal(value.messages[value.messages.length - 1].id, '0999');
  assert.equal(value.hasOlder, true);
  assert.equal(value.atNewest, true, 'a link to the newest reply is the newest end');
});
