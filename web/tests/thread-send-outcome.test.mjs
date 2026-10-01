import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// Exercise the actual thread controller against a scripted workspace. The claim under
// test is what the controller does with each outcome the workspace can name — and the
// defect it guards against is a controller that treats everything that is not an
// exception as a send that happened (review 15, V15-01).
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
let sendResult = { state: 'dropped' };
export const setSendResult = value => (sendResult = value);
let sendThrows = false;
export const setSendThrows = value => (sendThrows = value);
export const sendCalls = [];
export const workspace = {
  sendIntoThread: async (...args) => {
    sendCalls.push(args);
    if (sendThrows) throw new Error('the connection broke mid-request');
    return sendResult;
  }
};
const api = async () => { throw new Error('unexpected request'); };
`;
const { ThreadView, setSendResult, setSendThrows, sendCalls, APIError } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + compiled).toString('base64')
);

function view() {
  const value = new ThreadView('thread-1');
  value.thread = { id: 'thread-1', state: 'active' };
  return value;
}

test('a definite refusal is named as one, and never as a send that happened', async () => {
  const value = view();
  setSendResult({ state: 'refused', error: new APIError('You cannot reply here.', 403) });
  assert.equal(await value.send('the text', 'key-1'), 'refused');
  assert.equal(value.sendError?.outcome, 'refused');
  assert.equal(value.sendError?.status, 403);
  assert.equal(value.sendError?.message, 'You cannot reply here.');
  assert.deepEqual(value.messages, [], 'a refused reply was added to the thread');
  assert.equal(value.sending, false);
});

test('an unknown outcome is uncertain, and the text and key stay with the author', async () => {
  const value = view();
  setSendResult({ state: 'uncertain', error: new Error('connection lost') });
  assert.equal(await value.send('the text', 'key-1'), 'uncertain');
  assert.equal(value.sendError?.outcome, 'uncertain');
  assert.equal(value.sendError?.message, 'connection lost');
  assert.deepEqual(value.messages, [], 'an unconfirmed reply was added to the thread');
  // The caller's key was used, so a retry with it reconciles rather than duplicates.
  assert.deepEqual(sendCalls.at(-1), ['thread-1', 'the text', 'key-1']);
});

test('only a stored message is added to the thread, and only it is named sent', async () => {
  const value = view();
  const saved = { id: 'm1', idempotency_key: 'key-1' };
  setSendResult({ state: 'sent', message: saved });
  assert.equal(await value.send('the text', 'key-1'), 'sent');
  assert.equal(value.sendError, null);
  assert.deepEqual(value.messages, [saved]);
});

test('an operation dropped for a moved view is named dropped', async () => {
  const value = view();
  setSendResult({ state: 'dropped' });
  assert.equal(await value.send('the text', 'key-1'), 'dropped');
  assert.equal(value.sendError, null);
  assert.deepEqual(value.messages, []);
});

test('a refusal with no error of its own still says the reply was not sent', async () => {
  const value = view();
  setSendResult({ state: 'refused' });
  assert.equal(await value.send('the text', 'key-1'), 'refused');
  assert.equal(value.sendError?.outcome, 'refused');
  assert.equal(value.sendError?.message, 'That reply was not sent.');
});

// A throw is an unknown outcome, never a silent success: the request may have committed
// before the throw, and a caller that treats it as a failure would invite a duplicate.
test('an unexpected throw is an uncertain outcome', async () => {
  const value = view();
  setSendThrows(true);
  assert.equal(await value.send('the text', 'key-1'), 'uncertain');
  assert.equal(value.sendError?.outcome, 'uncertain');
});
