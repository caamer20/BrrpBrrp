import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';
import { retryOperationKey } from '../src/lib/thread-state.ts';

// Exercise the real workspace and its operation associations, not a stand-in: the
// claim under test is which key the production caller selects for a reply, and that
// the association it selects from is recorded, validated, bounded and persisted by
// the shipped store (review 17, V17-01).
const source = await readFile(new URL('../src/lib/workspace.svelte.ts', import.meta.url), 'utf8');
// The workspace imports these at runtime and the shell strips imports, so the real
// sources are compiled into the same module.
const shell = async (file) =>
  ts
    .transpileModule(await readFile(new URL(`../src/lib/${file}`, import.meta.url), 'utf8'), {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
    })
    .outputText.replace(/^export /gm, '')
    .replace(/import .*? from .*?;\n/g, '');
const outboxSource = await shell('outbox.ts');
const operationsSource = await shell('operations.ts');
const draftsSource = await shell('drafts.ts');
const activeViewSource = await shell('active-view.ts');
const shellModules = [outboxSource, operationsSource, draftsSource, activeViewSource].join('\n');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/import .*? from .*?;\n/g, '')
  .replace('export const workspace = new Workspace();', 'export { Workspace };');
const prelude = `
const $state = value => value;
const memory = new Map();
export const store = {
  getItem: key => (memory.has(key) ? memory.get(key) : null),
  setItem: (key, value) => memory.set(key, String(value)),
  removeItem: key => memory.delete(key),
  clear: () => memory.clear(),
  keys: () => [...memory.keys()]
};
const storage = store;
const sessionStorage = store;
const localStorage = storage;
const window = { innerWidth: 1200 };
const Notification = { permission: 'default' };
const navigator = {};
const disableNotifications = async () => {};
const consumeUpdateDrafts = () => null;
const clearUpdateDrafts = () => {};
const location = { hash: '', search: '', protocol: 'http:', host: 'localhost' };
const history = { replaceState() {} };
const document = { hidden: true, hasFocus: () => false };
const WebSocket = { OPEN: 1 };
const errorMessage = error => error.message;
class APIError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}
export const apiError = (message, status) => new APIError(message, status);
let handler;
export const setHandler = value => handler = value;
const api = (...args) => handler(...args);
`;
const { Workspace, setHandler, store, apiError } = await import(
  'data:text/javascript;base64,' + Buffer.from(prelude + shellModules + compiled).toString('base64')
);
// The operations store itself, compiled the same way, for its validation and bounds.
const operationsModule = await import(
  'data:text/javascript;base64,' +
    Buffer.from(
      shellModules +
        '\nexport { isOperationKey, readOperations, writeOperations, clearOperations, OperationsPerSlotLimit, OperationsSlotLimit };'
    ).toString('base64')
);
const {
  readOperations,
  writeOperations,
  clearOperations,
  OperationsPerSlotLimit,
  OperationsSlotLimit
} = operationsModule;

const account = '11111111-1111-4111-8111-111111111111';
const otherAccount = '22222222-2222-4222-8222-222222222222';
const conversation = 'a0000000-0000-4000-8000-00000000000a';
const thread = 'b0000000-0000-4000-8000-00000000000b';
const keys = {
  a: '30000000-0000-4000-8000-0000000000a1',
  b: '30000000-0000-4000-8000-0000000000b2',
  c: '30000000-0000-4000-8000-0000000000c3'
};
const state = () => ({
  user: { id: account },
  channels: [{ id: conversation, type: 'text' }],
  settings: {},
  unread: [],
  voice_states: [],
  categories: [],
  authorization: {
    revision: 1,
    is_owner: false,
    capabilities: ['channel.read', 'message.send'],
    owner_designated: true,
    ownership_established: true
  }
});
const message = (id, extra = {}) => ({
  id,
  idempotency_key: id,
  channel_id: conversation,
  conversation_id: conversation,
  user_id: account,
  content: 'hello',
  created_at: new Date().toISOString(),
  attachments: [],
  reactions: [],
  embeds: [],
  ...extra
});
function workspace(user = account) {
  const value = new Workspace();
  value.state = state();
  value.state.user.id = user;
  value.activeId = conversation;
  value.saveDrafts = true;
  value.showToast = (text) => {
    value.toast = text;
  };
  return value;
}
const replyToThread = (path, method, body, replies) => {
  if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
  replies.push({ path, method, body });
  return Promise.reject(apiError('server busy', 503));
};

// The reproduced defect: with A and B both uncertain, pressing Reply again on B
// submitted B under a fresh key, so a committed-but-unacknowledged B could be
// created twice. The decision now selects from the composer's own associations.
test('retrying the newest of two uncertain replies reuses its own key', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));

  assert.equal((await value.sendIntoThread(thread, 'A', keys.a)).state, 'uncertain');
  assert.equal((await value.sendIntoThread(thread, 'B', keys.b)).state, 'uncertain');
  const operations = value.threadComposerOperations(thread);
  assert.deepEqual(
    operations.map((operation) => operation.content),
    ['A', 'B'],
    'the composer does not hold both unresolved replies'
  );
  assert.equal(retryOperationKey(operations, 'B'), keys.b, 'the newest reply rotated its key');

  // Retrying B unchanged posts under B's own key and creates no third operation.
  const retryKey = retryOperationKey(operations, 'B');
  assert.equal((await value.sendIntoThread(thread, 'B', retryKey)).state, 'uncertain');
  const bPosts = replies.filter((reply) => reply.body.content === 'B');
  assert.deepEqual(
    bPosts.map((reply) => reply.body.idempotency_key),
    [keys.b, keys.b],
    'B was submitted under a rotated key'
  );
  assert.equal(
    value.outbox.filter((entry) => !entry.recovery && entry.thread_id === thread).length,
    2,
    'a third stored operation was created'
  );
});

// The older reply keeps its independent retry identity while the newest is pending.
test('retrying an older uncertain reply reuses its key while the newest is pending', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await value.sendIntoThread(thread, 'A', keys.a);
  await value.sendIntoThread(thread, 'B', keys.b);

  const operations = value.threadComposerOperations(thread);
  assert.equal(
    retryOperationKey(operations, 'A'),
    keys.a,
    'the older reply lost its retry identity'
  );
  await value.sendIntoThread(thread, 'A', retryOperationKey(operations, 'A'));
  const aPosts = replies.filter((reply) => reply.body.content === 'A');
  assert.deepEqual(
    aPosts.map((reply) => reply.body.idempotency_key),
    [keys.a, keys.a],
    'A was submitted under a rotated key'
  );
  // B's operation and association are untouched by A's retry.
  const after = value.threadComposerOperations(thread);
  assert.deepEqual(
    after.map((operation) => [operation.content, operation.key]),
    [
      ['A', keys.a],
      ['B', keys.b]
    ]
  );
  assert.equal(
    value.outbox.filter((entry) => !entry.recovery && entry.content === 'B').length,
    1,
    "B's operation was replaced or duplicated"
  );
});

// B committed but its acknowledgement was lost: the retry under the same key returns
// the original message, and the operation is over rather than a second B.
test('a receipt answers a same-key retry with the original message', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  assert.equal((await value.sendIntoThread(thread, 'B', keys.b)).state, 'uncertain');

  const original = '40000000-0000-4000-8000-0000000000b1';
  const retries = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    retries.push({ path, method, body });
    return Promise.resolve(
      message(original, {
        idempotency_key: keys.b,
        thread_id: thread,
        user_id: account,
        content: 'B'
      })
    );
  });
  const key = retryOperationKey(value.threadComposerOperations(thread), 'B');
  assert.equal(key, keys.b);
  const outcome = await value.sendIntoThread(thread, 'B', key);
  assert.equal(outcome.state, 'sent');
  assert.equal(outcome.message.id, original, 'the retry did not reconcile to the original message');
  assert.equal(retries.length, 1);
  assert.equal(retries[0].path, `/threads/${thread}/messages`);
  assert.equal(retries[0].body.idempotency_key, keys.b);
  assert.equal(
    value.outbox.filter((entry) => !entry.recovery && entry.thread_id === thread).length,
    0,
    'the acknowledged operation was left retryable'
  );
  assert.deepEqual(value.threadComposerOperations(thread), []);
});

// B never committed: the same-key retry creates exactly one reply, in its original
// thread, and nothing lands in the channel the thread lives in.
test('a same-key retry that never committed creates exactly one reply in its thread', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await value.sendIntoThread(thread, 'A', keys.a);
  assert.equal((await value.sendIntoThread(thread, 'B', keys.b)).state, 'uncertain');

  const stored = '40000000-0000-4000-8000-0000000000b2';
  const retries = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    retries.push({ path, method, body });
    return Promise.resolve(
      message(stored, {
        idempotency_key: keys.b,
        thread_id: thread,
        user_id: account,
        content: 'B'
      })
    );
  });
  const outcome = await value.sendIntoThread(
    thread,
    'B',
    retryOperationKey(value.threadComposerOperations(thread), 'B')
  );
  assert.equal(outcome.state, 'sent');
  assert.equal(retries.length, 1, 'one retry produced more than one request');
  assert.equal(retries[0].path, `/threads/${thread}/messages`, 'the retry left its thread');
  assert.equal(retries[0].body.idempotency_key, keys.b, 'the immutable key was not reused');
  assert.equal(
    retries.some((reply) => reply.path === `/channels/${conversation}/messages`),
    false,
    'a thread reply was rerouted into its channel'
  );
  // A's still-pending operation survived B's completion untouched.
  assert.deepEqual(
    value.threadComposerOperations(thread).map((operation) => [operation.content, operation.key]),
    [['A', keys.a]]
  );
});

// Close/reopen and reload: the associations are persisted beside the drafts and the
// queue, so a restored draft retries under its original key rather than as new work.
test('operation associations survive a reload with two pending replies', async (t) => {
  const first = workspace();
  t.after(() => first.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await first.sendIntoThread(thread, 'A', keys.a);
  await first.sendIntoThread(thread, 'B', keys.b);
  const durable = JSON.parse(store.getItem(`brrp.operations.${account}`) || '{}');
  assert.deepEqual(
    durable[`thread:${thread}`].map((operation) => [operation.content, operation.key]),
    [
      ['A', keys.a],
      ['B', keys.b]
    ],
    'the associations were not persisted'
  );
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.restoreComposerOperations();
  const offline = [];
  setHandler((path, method, body) => replyToThread(path, method, body, offline));
  await second.restoreOutbox();
  const operations = second.threadComposerOperations(thread);
  assert.equal(retryOperationKey(operations, 'B'), keys.b, 'the restored reply lost its key');
  assert.equal(retryOperationKey(operations, 'A'), keys.a, 'the restored older reply lost its key');

  const retries = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    retries.push({ path, method, body });
    return Promise.resolve(
      message('40000000-0000-4000-8000-0000000000b3', {
        idempotency_key: keys.b,
        thread_id: thread,
        user_id: account,
        content: 'B'
      })
    );
  });
  const outcome = await second.sendIntoThread(thread, 'B', retryOperationKey(operations, 'B'));
  assert.equal(outcome.state, 'sent');
  assert.equal(retries.length, 1);
  assert.equal(retries[0].path, `/threads/${thread}/messages`, 'the restored retry was rerouted');
  assert.equal(retries[0].body.idempotency_key, keys.b);
});

// A terminal outcome — here a definite refusal — becomes bounded recovery: the
// association ends with the operation, and the same words are deliberately new work.
// They must not quietly replay the refused operation's key, and must not be blocked
// from being sent as a new reply.
test('a definite refusal ends the operation and the same words become new work', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    replies.push({ path, method, body });
    return Promise.reject(apiError('this reply was rejected', 400));
  });
  const refused = await value.sendIntoThread(thread, 'A', keys.a);
  assert.equal(refused.state, 'refused');
  const entries = value.outbox.filter((entry) => entry.thread_id === thread);
  assert.equal(entries.length, 1, 'the refused operation was not kept for recovery');
  assert.ok(entries[0].recovery, 'a definite refusal did not become bounded recovery');
  assert.deepEqual(
    value.threadComposerOperations(thread),
    [],
    'a refused operation is still offered as retryable'
  );
  assert.equal(
    retryOperationKey(value.threadComposerOperations(thread), 'A'),
    undefined,
    'the refused key would be replayed'
  );

  const fresh = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    fresh.push({ path, method, body });
    return Promise.resolve(
      message('40000000-0000-4000-8000-0000000000c1', {
        idempotency_key: keys.c,
        thread_id: thread,
        user_id: account,
        content: 'A'
      })
    );
  });
  const sent = await value.sendIntoThread(thread, 'A', keys.c);
  assert.equal(sent.state, 'sent', 'the same words could not be sent as new work');
  assert.equal(fresh.length, 1);
  assert.equal(fresh[0].body.idempotency_key, keys.c, 'the refused key was replayed');
});

// An expired operation is bounded recovery too: no silent retry turns it into a send.
test('an expired operation becomes recovery rather than a silent retry', async (t) => {
  const first = workspace();
  t.after(() => first.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await first.sendIntoThread(thread, 'B', keys.b);
  // Age the durable entry past the retry window, as a device left open overnight
  // would find it.
  const durable = JSON.parse(store.getItem(`brrp.outbox.${account}`));
  durable[0].created = new Date(Date.now() - 26 * 60 * 60 * 1000).toISOString();
  store.setItem(`brrp.outbox.${account}`, JSON.stringify(durable));
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.restoreComposerOperations();
  const attempts = [];
  setHandler((path, method, body) => {
    attempts.push({ path, method });
    return Promise.reject(apiError('server busy', 503));
  });
  await second.restoreOutbox();
  assert.deepEqual(attempts, [], 'an expired operation was retried automatically');
  const entries = second.outbox.filter((entry) => entry.thread_id === thread);
  assert.equal(entries.length, 1);
  assert.ok(entries[0].recovery, 'the expired operation did not become recovery');
  assert.deepEqual(second.threadComposerOperations(thread), []);
});

// The acknowledgement clears exactly the snapshot it acknowledged: a draft still
// holding those words is cleared, and a newer edit is left alone.
test('an acknowledgement clears only the matching draft snapshot', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  store.clear();
  const owner = value.composerOwner();
  const slot = `thread:${thread}`;
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await value.sendIntoThread(thread, 'A', keys.a);
  await value.sendIntoThread(thread, 'B', keys.b);

  // The composer slot still holds the unedited B, and then a newer edit.
  value.saveComposerDraft(slot, 'B', [], owner);
  value.acknowledgeMessages([
    message('50000000-0000-4000-8000-0000000000b4', {
      idempotency_key: keys.b,
      thread_id: thread,
      user_id: account,
      content: 'B'
    })
  ]);
  assert.equal(value.composerDrafts.get(slot), undefined, 'the acknowledged snapshot stayed');
  assert.equal(value.drafts[slot], undefined);
  assert.deepEqual(
    value.threadComposerOperations(thread).map((operation) => operation.content),
    ['A'],
    "acknowledging B disturbed A's operation"
  );

  // Now the newer edit: acknowledging A must not clear it.
  value.saveComposerDraft(slot, 'A with a newer thought', [], owner);
  value.acknowledgeMessages([
    message('50000000-0000-4000-8000-0000000000a5', {
      idempotency_key: keys.a,
      thread_id: thread,
      user_id: account,
      content: 'A'
    })
  ]);
  assert.equal(
    value.composerDrafts.get(slot)?.text,
    'A with a newer thought',
    'a newer edit was erased by an acknowledgement'
  );
  assert.deepEqual(value.threadComposerOperations(thread), []);
});

// No association leaks across accounts: a signed-in account reads only its own, and
// nothing is written into the other account's device scope.
test("an account switch neither reads nor writes another account's associations", async (t) => {
  const first = workspace();
  t.after(() => first.stop());
  store.clear();
  const replies = [];
  setHandler((path, method, body) => replyToThread(path, method, body, replies));
  await first.sendIntoThread(thread, 'B', keys.b);
  assert.ok(store.getItem(`brrp.operations.${account}`), 'the first account persisted nothing');
  first.stop();

  const second = workspace(otherAccount);
  t.after(() => second.stop());
  assert.deepEqual(
    second.threadComposerOperations(thread),
    [],
    "another account's session sees the first account's associations"
  );
  assert.equal(retryOperationKey(second.threadComposerOperations(thread), 'B'), undefined);
  assert.equal(store.getItem(`brrp.operations.${otherAccount}`), null);
  assert.ok(store.getItem(`brrp.operations.${account}`), "the first account's record was lost");
});

// The association store validates what it restores: a malformed record cannot decide
// a send key, and the record stays bounded.
test('the association store validates and bounds what it restores', () => {
  const memory = new Map();
  const storage = {
    getItem: (key) => (memory.has(key) ? memory.get(key) : null),
    setItem: (key, value) => memory.set(key, String(value)),
    removeItem: (key) => memory.delete(key)
  };
  assert.deepEqual(readOperations(storage, 'not-an-account'), {});
  memory.set(
    `brrp.operations.${account}`,
    JSON.stringify({
      [`thread:${thread}`]: [
        { key: keys.a, content: 'A' },
        { key: 'not-a-key', content: 'nope' },
        { key: keys.b }, // missing content
        { key: keys.b, content: 'B' },
        { key: keys.c, content: 'C' },
        'garbage'
      ],
      'channel:somewhere': [{ key: keys.a, content: 'A' }],
      'thread:not-a-uuid': [{ key: keys.a, content: 'A' }]
    })
  );
  const read = readOperations(storage, account);
  assert.deepEqual(Object.keys(read), [`thread:${thread}`]);
  assert.deepEqual(
    read[`thread:${thread}`].map((operation) => operation.key),
    [keys.a, keys.b, keys.c],
    'invalid or duplicate entries decided a key'
  );

  // Writes keep the newest slots and the newest entries per slot, and reject malformed
  // keys and content.
  const operations = new Map();
  for (let index = 0; index < OperationsSlotLimit + 10; index++) {
    operations.set(`thread:${crypto.randomUUID()}`, [
      { key: crypto.randomUUID(), content: `t${index}` }
    ]);
  }
  const newest = `thread:${thread}`;
  operations.set(newest, [
    { key: 'not-a-key', content: 'nope' },
    ...Array.from({ length: OperationsPerSlotLimit + 4 }, () => ({
      key: crypto.randomUUID(),
      content: 'x'
    })),
    { key: keys.c, content: 'the newest' }
  ]);
  writeOperations(storage, account, operations);
  const written = readOperations(storage, account);
  const slots = Object.keys(written);
  assert.equal(slots.length, OperationsSlotLimit);
  assert.equal(slots.at(-1), newest, 'the newest slot was trimmed away');
  assert.equal(written[newest].length, OperationsPerSlotLimit);
  assert.deepEqual(written[newest].at(-1), { key: keys.c, content: 'the newest' });

  clearOperations(storage, account);
  assert.equal(memory.has(`brrp.operations.${account}`), false);
});
