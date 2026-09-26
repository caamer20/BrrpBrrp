import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/replies.svelte.ts', import.meta.url), 'utf8');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '');
const prelude = `
const $state = value => value;
const errorMessage = error => error.message;
export class APIError extends Error { constructor(message, status) { super(message); this.status = status; } }
let handler;
export const setHandler = value => handler = value;
const api = (...args) => handler(...args);
`;
const { Replies, APIError, setHandler } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + compiled).toString('base64')
);
const message = (id, reply = 'root') => ({
  id,
  reply_to_id: reply,
  channel_id: 'channel',
  idempotency_key: id,
  content: id
});
const page = (replies, hasOlder = false) => ({
  parent: message('root', ''),
  replies: replies.map((id) => message(id)),
  has_older: hasOlder
});
function deferred() {
  let resolve;
  const promise = new Promise((yes) => (resolve = yes));
  return { promise, resolve };
}

test('reply pagination is chronological, isolated, and limited to five pages', async (t) => {
  const value = new Replies('root');
  t.after(() => value.dispose());
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return path.includes('before=3') ? page(['1', '2']) : page(['3', '4'], true);
  });
  await value.load();
  await value.older();
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['1', '2', '3', '4']
  );
  assert.equal(calls.at(-1), '/messages/root/replies?before=3');
  assert.equal(value.hasOlder, false);
  value.pages = 5;
  const previous = calls.length;
  await value.older();
  assert.equal(calls.length, previous);
});

test('deleting the parent while loading cannot resurrect it', async (t) => {
  const value = new Replies('root');
  t.after(() => value.dispose());
  const response = deferred();
  setHandler(() => response.promise);
  const loading = value.load();
  value.receive({ t: 'message.deleted', d: { id: 'root' } });
  response.resolve(page(['1']));
  await loading;
  assert.equal(value.parent, null);
  assert.equal(value.missing, true);
  assert.deepEqual(value.messages, []);
});

test('a reply deletion during refresh invalidates the stale response', async (t) => {
  const value = new Replies('root');
  t.after(() => value.dispose());
  setHandler(async () => page(['1', '2']));
  await value.load();
  const response = deferred();
  setHandler(() => response.promise);
  const loading = value.load();
  value.receive({ t: 'message.deleted', d: { id: '2', reply_to_id: 'root' } });
  response.resolve(page(['1', '2']));
  await loading;
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['1']
  );
  setHandler(async () => page(['1']));
  await value.load();
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['1']
  );
});

test('unrelated conversations do not reload a thread, reconnect does', async (t) => {
  const value = new Replies('root');
  t.after(() => value.dispose());
  setHandler(async () => page(['1']));
  await value.load();
  value.receive({ t: 'message.created', d: { id: 'other', reply_to_id: 'elsewhere' } });
  assert.equal(value.timer, undefined);
  value.receive({ t: 'hello', d: {} });
  assert.ok(value.timer);
});

test('disposed threads cannot restore data or acknowledge delivery', async () => {
  let acknowledged = false;
  const value = new Replies('root', () => (acknowledged = true));
  const response = deferred();
  setHandler(() => response.promise);
  const loading = value.load();
  value.dispose();
  response.resolve(page(['1']));
  await loading;
  assert.equal(value.parent, null);
  assert.equal(acknowledged, false);
});

test('network failure preserves the discussion and permits retry', async (t) => {
  const value = new Replies('root');
  t.after(() => value.dispose());
  setHandler(async () => page(['1']));
  await value.load();
  setHandler(async () => {
    throw new Error('offline');
  });
  await value.load();
  assert.equal(value.error, 'offline');
  assert.equal(value.messages.length, 1);
  setHandler(async () => {
    throw new APIError('Not found', 404);
  });
  await value.load();
  assert.equal(value.missing, true);
  assert.equal(value.parent, null);
});
