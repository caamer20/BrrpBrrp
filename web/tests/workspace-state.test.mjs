import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// Exercise request ordering with deferred responses and an inert browser shell.
const source = await readFile(new URL('../src/lib/workspace.svelte.ts', import.meta.url), 'utf8');
// The workspace imports the outbox store at runtime, and the shell below strips imports, so the
// store's real source is compiled into the same module: the tests exercise the shipped code
// rather than a stand-in for it.
const outboxSource = await readFile(new URL('../src/lib/outbox.ts', import.meta.url), 'utf8');
const draftsSource = await readFile(new URL('../src/lib/drafts.ts', import.meta.url), 'utf8');
// The active-view rule is a dependency of both markRead and the mention notification,
// and the shell below strips imports, so it is inlined too — otherwise the methods would
// reference a name that does not exist in the module under test.
const activeViewSource = await readFile(
  new URL('../src/lib/active-view.ts', import.meta.url),
  'utf8'
);
const shellModules = [outboxSource, draftsSource, activeViewSource]
  .map((file) =>
    ts
      .transpileModule(file, {
        compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
      })
      .outputText.replace(/^export /gm, '')
  )
  .join('\n');
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
const localStorage = storage;
const window = { innerWidth: 1200 };
const Notification = { permission: 'default' };
const navigator = {};
// The workspace's sign-out path asks the notification worker to stop first. The real
// module is stripped with the other imports, so the shell stands in for it.
const disableNotifications = async () => {};
export const setNotificationWorker = ready => {
  if (ready) {
    window.Notification = Notification;
    Notification.permission = 'granted';
    navigator.serviceWorker = { ready };
  } else {
    delete window.Notification;
    Notification.permission = 'default';
    delete navigator.serviceWorker;
  }
};
const location = { hash: '', search: '', protocol: 'http:', host: 'localhost' };
const history = { replaceState() {} };
const document = { hidden: true, hasFocus: () => false };
export const setDocumentFocus = (hidden, focused) => {
  document.hidden = hidden;
  document.hasFocus = () => focused;
};
const WebSocket = { OPEN: 1 };
const errorMessage = error => error.message;
class APIError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}
export const apiError = (message, status) => new APIError(message, status);
let handler;
export const setHandler = value => handler = value;
const api = (...args) => handler(...args);
`;
const { Workspace, setHandler, store, setDocumentFocus, apiError, setNotificationWorker } =
  await import(
    'data:text/javascript;base64,' +
      Buffer.from(prelude + shellModules + compiled).toString('base64')
  );
const state = () => ({
  user: { id: 'me' },
  channels: [
    { id: 'a', type: 'text' },
    { id: 'b', type: 'text' }
  ],
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
  channel_id: 'a',
  user_id: 'me',
  content: 'hello',
  created_at: new Date().toISOString(),
  attachments: [],
  reactions: [],
  ...extra
});
function workspace() {
  const value = new Workspace();
  value.state = state();
  value.activeId = 'a';
  value.showToast = (text) => {
    value.toast = text;
  };
  return value;
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

test('a failed message load has a persistent retry state and keeps existing messages', async () => {
  const value = workspace();
  value.messages = [message('001')];
  setHandler(async () => {
    throw new Error('offline');
  });
  assert.equal(await value.loadMessages(true), false);
  assert.equal(value.messagesError, 'offline');
  assert.equal(value.messagesErrorOlder, true);
  assert.equal(value.messages[0].id, '001');
  assert.equal(value.olderLoading, false);
  setHandler(async () => []);
  await value.loadMessages(true);
  assert.equal(value.messagesError, '');
});

test('channel switches clear old pagination and ignore stale responses', async () => {
  const value = workspace();
  const first = deferred(),
    second = deferred();
  setHandler((path) => (path.includes('/a/') ? first.promise : second.promise));
  value.hasOlder = true;
  const oldLoad = value.loadMessages();
  const nextLoad = value.selectChannel('b');
  assert.equal(value.hasOlder, false);
  second.resolve([message('new', { channel_id: 'b' })]);
  await nextLoad;
  first.resolve([message('old')]);
  await oldLoad;
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['new']
  );
  assert.equal(value.messagesLoading, false);
});

test('repeated load-earlier clicks start only one request', async () => {
  const value = workspace();
  value.messages = [message('050')];
  const response = deferred();
  let calls = 0;
  setHandler(() => {
    calls++;
    return response.promise;
  });
  const first = value.loadMessages(true);
  const second = value.loadMessages(true);
  assert.equal(calls, 1);
  response.resolve([message('049')]);
  await Promise.all([first, second]);
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['049', '050']
  );
});

test('in-flight requests cannot restore private state after the session stops', async () => {
  const value = workspace();
  const response = deferred();
  setHandler(() => response.promise);
  const refresh = value.refreshState();
  value.stop();
  value.state = null;
  response.resolve(state());
  await refresh;
  assert.equal(value.state, null);

  value.state = state();
  const messages = deferred();
  setHandler(() => messages.promise);
  const load = value.loadMessages();
  value.stop();
  messages.resolve([message('private')]);
  await load;
  assert.deepEqual(value.messages, []);
});

test('a late send response is discarded even if the same user signs back in', async () => {
  const value = workspace();
  const response = deferred();
  setHandler(() => response.promise);
  const send = value.send('test', []);
  const pending = value.messages[0];
  value.stop();
  value.state = state();
  response.resolve(message('saved', { idempotency_key: pending.idempotency_key }));
  await send;
  assert.deepEqual(value.messages, []);
});

test('confirmed delivery wins over a failed POST connection', async () => {
  const value = workspace();
  const response = deferred();
  let saved;
  setHandler((_path, method) => (method === 'POST' ? response.promise : Promise.resolve([saved])));
  const send = value.send('test', []);
  saved = message('saved', { idempotency_key: value.messages[0].idempotency_key });
  await value.loadMessages();
  response.reject(new Error('connection lost after delivery'));
  await send;
  assert.deepEqual(value.messages, [saved]);
  assert.equal(value.toast, '');
});

test('failed jump to latest preserves the history position', async () => {
  const value = workspace();
  value.viewingHistory = true;
  value.atLatest = false;
  value.messages = [message('old')];
  setHandler(async () => {
    throw new Error('offline');
  });
  assert.equal(await value.latest(), false);
  assert.equal(value.viewingHistory, true);
  assert.equal(value.atLatest, false);
  assert.equal(value.messages[0].id, 'old');
});

test('sending from history returns to the live conversation without losing the pending message', async () => {
  const value = workspace();
  value.viewingHistory = true;
  value.atLatest = false;
  const response = deferred();
  setHandler((_path, method) =>
    method === 'POST' ? response.promise : Promise.resolve([message('050')])
  );
  const send = value.send('reply', [], 'old');
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(value.viewingHistory, false);
  assert.equal(value.atLatest, true);
  assert.equal(value.messages.length, 2);
  const pending = value.messages[1];
  assert.equal(pending.pending, 'sending');
  response.resolve(message('051', { idempotency_key: pending.idempotency_key }));
  await send;
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['050', '051']
  );
});

test('sending and retrying in a thread preserve channel history and the idempotency key', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.viewingHistory = true;
  value.atLatest = false;
  value.messages = [message('old')];
  setHandler(async () => {
    throw new Error('offline');
  });
  await value.send('thread reply', [], 'parent', true);
  const pending = value.pendingReplies('parent')[0];
  assert.equal(pending.pending, 'failed');
  assert.equal(value.messages[0].id, 'old');
  assert.equal(value.messages.length, 1);
  let body;
  setHandler(async (_path, _method, input) => {
    body = input;
    return message('saved', { ...input, reply_to_id: 'parent' });
  });
  await value.retryMessage(pending);
  assert.equal(body.idempotency_key, pending.idempotency_key);
  assert.equal(value.viewingHistory, true);
  assert.equal(value.atLatest, false);
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['old']
  );
  assert.equal(value.pendingReplies('parent').length, 0);
});

test('a thread refresh can confirm delivery before a send connection fails', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const response = deferred();
  setHandler(() => response.promise);
  const sending = value.send('thread reply', [], 'parent', true);
  const pending = value.pendingReplies('parent')[0];
  value.acknowledgeMessages([
    message('saved', { idempotency_key: pending.idempotency_key, reply_to_id: 'parent' })
  ]);
  response.reject(new Error('connection lost'));
  await sending;
  assert.equal(value.pendingReplies('parent').length, 0);
  assert.equal(value.toast, '');
});

async function flush(value) {
  clearTimeout(value.flushTimer);
  value.flushTimer = undefined;
  await value.flushChanges();
}

// A send whose acknowledgement was lost is restored from the account's own scope of the device
// store and reconciled against the operation record the server kept for it.
test('an opted-in outbox reconciles a held send under its own key and clears it on the answer', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const key = '11111111-1111-4111-8111-111111111111';
  const account = '55555555-5555-4555-8555-555555555555';
  const conversation = '77777777-7777-4777-8777-777777777777';
  value.state.user = { id: account };
  value.state.channels = [...value.state.channels, { id: conversation, type: 'text' }];
  value.saveDrafts = true;
  store.clear();
  store.setItem(
    `brrp.outbox.${account}`,
    JSON.stringify([
      {
        key,
        conversation,
        thread_id: '',
        content: 'held text',
        attachments: ['aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'],
        reply_to: '',
        created: new Date().toISOString()
      }
    ])
  );
  const posts = [];
  setHandler(async (path, method, body) => {
    posts.push({ path, method, body });
    return message('88888888-8888-4888-8888-888888888888', {
      idempotency_key: key,
      content: 'held text'
    });
  });
  await value.restoreOutbox();
  assert.equal(posts.length, 1, 'the held send is submitted once');
  assert.equal(posts[0].body.idempotency_key, key, 'reconciled under its original key');
  assert.deepEqual(
    posts[0].body.attachment_ids,
    ['aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'],
    'the retry claims the attachments the send was submitted with'
  );
  assert.equal(value.pending[key], undefined, 'the acknowledgement clears the pending send');
  assert.equal(store.getItem(`brrp.outbox.${account}`), null, 'and releases the durable entry');
});

// A refusal the server would repeat is the one case where retrying is wrong: the entry ends, and
// the text the person typed is kept where they can do something with it.
test('a refusal the server will repeat releases the entry and keeps the text', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const key = '22222222-2222-4222-8222-222222222222';
  const account = '66666666-6666-4666-8666-666666666666';
  const conversation = '99999999-9999-4999-8999-999999999999';
  value.state.user = { id: account };
  value.state.channels = [...value.state.channels, { id: conversation, type: 'text' }];
  value.saveDrafts = true;
  store.clear();
  store.setItem(
    `brrp.outbox.${account}`,
    JSON.stringify([
      {
        key,
        conversation,
        thread_id: '',
        content: 'unsendable text',
        attachments: [],
        reply_to: '',
        created: new Date().toISOString()
      }
    ])
  );
  let calls = 0;
  setHandler(async () => {
    calls++;
    throw apiError('You no longer have access to that conversation.', 403);
  });
  await value.restoreOutbox();
  assert.equal(calls, 1, 'attempted once, not retried');
  assert.equal(value.recoveredMessages[0].content, 'unsendable text');
  assert.match(value.recoveredMessages[0].recovery, /no longer have access/);
  assert.equal(value.drafts.a, undefined, 'another conversation is never filled with this text');
  assert.equal(value.pending[key], undefined, 'a terminal refusal has no Retry action');
  assert.equal(
    JSON.parse(store.getItem(`brrp.outbox.${account}`))[0].recovery,
    value.recoveredMessages[0].recovery
  );
});

// The client half of ADR 0010's cursor rule: a cursor advances only where the server confirmed
// it, and a stale window or a stale snapshot can never move it backward.
test('a read cursor only advances, and only once the server confirmed it', async (t) => {
  const value = workspace();
  // The shell is shared by every test in this file, so the focused document this test needs is
  // handed back before the next one runs.
  t.after(() => {
    setDocumentFocus(true, false);
    value.stop();
  });
  setDocumentFocus(false, true);
  const reads = [];
  let refuse = false;
  setHandler(async (path, method, body) => {
    reads.push({ path, method, body });
    if (refuse) throw apiError('busy', 429);
    return {};
  });
  const first = '00000000-0000-4000-8000-000000000001';
  const second = '00000000-0000-4000-8000-000000000002';
  value.messages = [message(first), message(second)];
  await value.markRead();
  assert.deepEqual(
    reads.map((read) => read.body.last_read_message_id),
    [second]
  );
  // A conversation reloaded from an older cached window has nothing to advance.
  value.messages = [message(first)];
  await value.markRead();
  assert.equal(reads.length, 1, 'an older window sends nothing');
  // A refused mark is not progress: the same message is offered again rather than assumed sent.
  refuse = true;
  const third = '00000000-0000-4000-8000-000000000003';
  value.messages = [message(third)];
  await value.markRead();
  assert.equal(reads.length, 2);
  value.messages = [message(third)];
  await value.markRead();
  assert.equal(reads.length, 3, 'the unconfirmed cursor is offered again');
  refuse = false;
  await value.markRead();
  assert.equal(reads.length, 4);
  // A confirmed snapshot carries the server's cursor, and it is a floor rather than a reset.
  value.messages = [message(third)];
  await value.markRead();
  assert.equal(reads.length, 4, 'a confirmed cursor is not sent again');
});

test('discarding a failed reply releases the update guard without discarding an active send', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const response = deferred();
  setHandler(() => response.promise);
  const sending = value.send('unsent', [], 'parent', true);
  const pending = value.pendingReplies('parent')[0];
  value.discardPending(pending);
  assert.equal(value.hasPendingMessages, true);
  response.reject(new Error('offline'));
  await sending;
  value.discardPending(value.pendingReplies('parent')[0]);
  assert.equal(value.hasPendingMessages, false);
  assert.equal(value.canUpdate, true);
});

test('reactions update only the affected message with no workspace or history reload', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return message('001', { reactions: [{ emoji: '👍', count: 1, mine: false }] });
  });
  value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  await flush(value);
  assert.deepEqual(calls, ['/messages/001']);
  assert.equal(value.messages[0].reactions[0].count, 1);
});

test('activity in an unvisited channel fetches only unread counts', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return [{ channel_id: 'b', count: 1, mentions: 0, last_read_message_id: '' }];
  });
  value.receive({ t: 'message.created', d: { id: '002', channel_id: 'b' } });
  await flush(value);
  assert.deepEqual(calls, ['/unread']);
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['001']
  );
  assert.equal(value.state.unread[0].count, 1);
});

test('a burst of changes to one message is coalesced into one lookup', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  let calls = 0;
  setHandler(async () => {
    calls++;
    return message('001', { content: 'edited' });
  });
  for (let i = 0; i < 40; i++)
    value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  await flush(value);
  assert.equal(calls, 1);
  assert.equal(value.messages[0].content, 'edited');
});

test('a delayed lookup cannot resurrect a deleted message', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001'), message('002', { reply_to_id: '001' })];
  const response = deferred();
  setHandler((path) => (path === '/unread' ? Promise.resolve([]) : response.promise));
  value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  const loading = flush(value);
  value.receive({ t: 'message.deleted', d: { id: '001', channel_id: 'a' } });
  response.resolve(message('001'));
  await loading;
  await flush(value);
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['002']
  );
  assert.equal(value.messages[0].reply_to_id, undefined);
});

test('cached channels appear before revalidation and restore a reading anchor', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  value.atLatest = false;
  value.viewingHistory = true;
  value.recordScroll('a', { id: '001', offset: -12, top: 150 });
  setHandler(async () => []);
  await value.selectChannel('b');
  const response = deferred();
  const calls = [];
  setHandler((path) => {
    calls.push(path);
    return response.promise;
  });
  const loading = value.selectChannel('a');
  assert.equal(value.messages[0].id, '001');
  assert.equal(value.scrollRestore.offset, -12);
  assert.equal(value.messagesLoading, false);
  assert.match(calls[0], /from=001&to=001/);
  response.resolve([message('001', { content: 'fresh' })]);
  await loading;
  assert.equal(value.messages[0].content, 'fresh');
  assert.equal(value.viewingHistory, true);
  assert.equal(value.atLatest, false);
});

test('channel cache is bounded and cleared on session end', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler(async () => [message('001')]);
  for (let i = 0; i < 20; i++) {
    value.messages = [message(String(i), { channel_id: value.activeId })];
    await value.selectChannel('channel-' + i);
  }
  assert.ok(value.views.size <= 5);
  assert.equal(value.views.has('a'), false);
  value.stop();
  assert.equal(value.views.size, 0);
});

test('an authorization change clears cached conversations and refetches the snapshot', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return path === '/state' ? state() : [message('001')];
  });
  value.messages = [message('001', { channel_id: 'a' })];
  await value.selectChannel('b');
  value.messages = [message('002', { channel_id: 'b' })];
  await value.selectChannel('a');
  assert.ok(value.views.size >= 1);
  // The server says who may read what has changed, so locally held history is no
  // longer evidence of access and is dropped before the snapshot is refetched.
  value.receive({ t: 'authorization.changed', d: { reason: 'Channel access changed.' } });
  assert.equal(value.views.size, 0);
  await flush(value);
  assert.equal(value.views.size, 0);
  assert.ok(calls.includes('/state'));
});

test('the interface offers what the snapshot says this account may do', () => {
  const value = workspace();
  // Capabilities come from the server, so a member sees no management entry even
  // though the account exists, and the member list's admin flag is not consulted.
  assert.equal(value.can('member.manage'), false);
  assert.equal(value.can('channel.read'), true);
  assert.equal(value.canManageAnything, false);
  assert.equal(value.isOwner, false);
  value.state.authorization = {
    revision: 2,
    is_owner: false,
    capabilities: ['channel.read', 'member.manage', 'role.manage'],
    owner_designated: true,
    ownership_established: true
  };
  assert.equal(value.canManageAnything, true);
  assert.equal(value.canAny('channel.manage', 'member.manage'), true);
  assert.equal(value.can('operator.backup'), false);
  assert.equal(value.can('settings.manage'), false);
});

test('channel groups follow the durable category order, not channel order', () => {
  const value = workspace();
  value.state.channels = [
    { id: 'general', type: 'text', category: 'Lounge', position: 0 },
    { id: 'briefing', type: 'text', category: 'Operations', position: 0 },
    { id: 'watercooler', type: 'text', category: '', position: 1 },
    { id: 'quiet', type: 'text', category: 'Operations', position: 1 }
  ];
  value.state.categories = [
    { id: 'two', name: 'Lounge', position: 2, channels: 1 },
    { id: 'one', name: 'Operations', position: 1, channels: 2 }
  ];
  // The durable order is Operations then Lounge, even though the channels were
  // created in the other order; a category with no channels in view is absent, and
  // an uncategorized channel appears after the durable groups rather than vanishing.
  assert.deepEqual(
    value.textGroups.map((group) => group.name),
    ['Operations', 'Lounge', '']
  );
  assert.deepEqual(
    value.textGroups[0].channels.map((channel) => channel.id),
    ['briefing', 'quiet']
  );
});

test('a changed snapshot advances the authorization generation and a repeat does not', async () => {
  const value = workspace();
  const generation = value.authorizationGeneration;
  setHandler(async () => state());
  await value.refreshState();
  // The same authority is not a change, so panels holding their own results keep them.
  assert.equal(value.authorizationGeneration, generation);
  setHandler(async () => ({
    ...state(),
    authorization: {
      revision: 5,
      is_owner: false,
      capabilities: ['channel.read'],
      owner_designated: true,
      ownership_established: true
    }
  }));
  // A refresh that removes a capability is a change of authority even with no event,
  // because a rejected refresh can otherwise hide the revocation until a reload.
  await value.refreshState();
  assert.equal(value.authorizationGeneration, generation + 1);
});

test('an authorization change advances the generation as well as clearing caches', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler(async (path) => (path === '/state' ? state() : []));
  const generation = value.authorizationGeneration;
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  // Panels that captured the previous generation discard their results and any answer
  // still in flight, which is what keeps a revoked search or pin list from returning.
  assert.equal(value.authorizationGeneration, generation + 1);
  await flush(value);
  assert.equal(value.authorizationGeneration, generation + 1);
});

test('a revocation clears the conversation before the refresh is attempted, even if it fails', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001'), message('002', { reply_to_id: '001' })];
  value.replyTo = message('001');
  value.threadId = '001';
  value.unreadAfter = '001';
  value.viewingHistory = true;
  setHandler(async () => {
    throw new Error('offline');
  });
  value.receive({ t: 'authorization.changed', d: { reason: 'Channel access changed.' } });
  // Restricted content goes before the refetch is attempted: the refetch is not
  // guaranteed to succeed, and waiting for it would leave the old content on screen.
  assert.deepEqual(value.messages, []);
  assert.equal(value.replyTo, null);
  assert.equal(value.threadId, '');
  assert.equal(value.unreadAfter, null);
  assert.equal(value.viewingHistory, false);
  await flush(value);
  assert.deepEqual(value.messages, []);
  assert.equal(value.replyTo, null);
  // The failure is still recoverable: the error is shown and the loading state is
  // cleared, so the view is empty rather than stuck on a spinner.
  assert.equal(value.messagesError, 'offline');
  assert.equal(value.messagesLoading, false);
  assert.equal(value.olderLoading, false);
});

test('a history response admitted before a revocation cannot repopulate the view', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const inFlight = deferred();
  let revoked = false;
  setHandler((path) => {
    if (path === '/state') return Promise.resolve(state());
    return revoked ? Promise.resolve([]) : inFlight.promise;
  });
  const loading = value.loadMessages();
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  revoked = true;
  inFlight.resolve([message('hidden', { content: 'restricted' })]);
  await loading;
  assert.deepEqual(value.messages, []);
  await flush(value);
  assert.deepEqual(value.messages, []);
  assert.equal(
    value.messages.some((item) => item.content === 'restricted'),
    false
  );
});

test('a context jump admitted before a revocation cannot restore private content', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const inFlight = deferred();
  setHandler((path) => (path === '/state' ? Promise.resolve(state()) : inFlight.promise));
  const jumping = value.jumpToMessage('secret');
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  inFlight.resolve([message('secret', { channel_id: 'b', content: 'restricted' })]);
  await jumping;
  // The jump neither moved the view nor restored the message it was admitted for.
  assert.equal(value.activeId, 'a');
  assert.deepEqual(value.messages, []);
  assert.equal(value.highlightId, '');
});

test('a late single-message lookup cannot restore content after a revocation', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const lookup = deferred();
  setHandler((path) => {
    if (path === '/state') return Promise.resolve(state());
    if (path === '/unread') return Promise.resolve([]);
    // The incremental lookup is the request the revocation overtakes; the history
    // refetch that follows it is a request this account is still allowed to make.
    if (path === '/messages/001') return lookup.promise;
    return Promise.resolve([]);
  });
  value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  const loading = flush(value);
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  lookup.resolve(message('001', { content: 'late private incremental content' }));
  await loading;
  // The answer was admitted before the revocation, so it is dropped instead of being
  // written into the conversation the revocation just cleared.
  assert.deepEqual(value.messages, []);
  await flush(value);
  assert.deepEqual(value.messages, []);
  assert.equal(
    value.messages.some((item) => item.content === 'late private incremental content'),
    false
  );
  assert.equal(value.views.size, 0);
});

test('a late unread response cannot restore counts after a revocation', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const counts = deferred();
  setHandler((path) => {
    if (path === '/state') return Promise.resolve(state());
    if (path === '/unread') return counts.promise;
    return Promise.resolve([]);
  });
  value.receive({ t: 'unread.updated', d: {} });
  const loading = flush(value);
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  counts.resolve([{ channel_id: 'hidden', count: 42, mentions: 3, last_read_message_id: '' }]);
  await loading;
  clearTimeout(value.flushTimer);
  value.flushTimer = undefined;
  // Unread state names channels and counts their activity, so a list admitted under
  // the previous authority is not adopted: the count is metadata about a channel this
  // account may no longer be allowed to read.
  assert.deepEqual(value.state.unread, []);
  assert.equal(value.unreadFor('hidden'), undefined);
});

test('a snapshot admitted before a revocation cannot restore the old authority', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const snapshot = deferred();
  let snapshots = 0;
  setHandler((path) => {
    if (path !== '/state') return Promise.resolve([]);
    // The first snapshot is the one the revocation overtakes. The recovery request
    // that follows it returns the authority this account actually still has.
    return snapshots++ === 0 ? snapshot.promise : Promise.resolve(state());
  });
  const refresh = value.refreshState();
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  // The revoked snapshot grants a capability the account has already lost. Adopting
  // it would put management controls back on screen under authority that no longer
  // exists, and would also hide the revocation from the next capability comparison.
  snapshot.resolve({
    ...state(),
    authorization: {
      revision: 1,
      is_owner: false,
      capabilities: ['channel.read', 'message.send', 'role.manage'],
      owner_designated: true,
      ownership_established: true
    }
  });
  await refresh;
  assert.equal(value.can('role.manage'), false);
  assert.equal(value.authorizationUnverified, true);
  await flush(value);
  // The permitted refetch is the snapshot that is applied, and it clears the
  // unverified state that withholds the counts.
  assert.equal(value.authorizationUnverified, false);
});

test('an unverified authorization change withholds counts until a snapshot lands', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.unread = [{ channel_id: 'a', count: 7, mentions: 1, last_read_message_id: '' }];
  assert.equal(value.unreadFor('a').count, 7);
  setHandler(async () => {
    throw new Error('offline');
  });
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  await flush(value);
  // The refresh failed, so no snapshot under the new authority was admitted: the
  // structure stays for navigation and recovery, while the counts derived from the
  // previous authority are withheld rather than shown as current.
  assert.equal(value.authorizationUnverified, true);
  assert.equal(value.unreadFor('a'), undefined);
  assert.equal(value.messagesError, 'offline');
});

test('a failure admitted before a revocation cannot overwrite the cleared view', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const lookup = deferred();
  let revoked = false;
  setHandler((path) => {
    if (path === '/state') return Promise.reject(new Error('recovery failure'));
    if (path === '/unread') return Promise.resolve([]);
    if (path === '/messages/001') return lookup.promise;
    if (revoked) return Promise.resolve([]);
    return Promise.resolve([]);
  });
  value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  const loading = flush(value);
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  revoked = true;
  lookup.reject(new Error('stale failure'));
  await loading;
  clearTimeout(value.flushTimer);
  value.flushTimer = undefined;
  // The old request's failure belongs to the request, not to the view the revocation
  // left behind: the refetch that follows owns the loading and error state now.
  assert.equal(value.messagesError, '');
  await flush(value);
  assert.equal(value.messagesError, 'recovery failure');
});

test('a permitted refresh reloads the conversation cleared by a revocation', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  let calls = 0;
  setHandler(async (path) => {
    if (path === '/state') return state();
    calls++;
    return [message('kept')];
  });
  value.messages = [message('001')];
  value.receive({ t: 'authorization.changed', d: { reason: 'Role changed.' } });
  assert.deepEqual(value.messages, []);
  await flush(value);
  // The account still reads the channel, so it recovers without navigating away.
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['kept']
  );
  assert.equal(value.messagesLoading, false);
  assert.equal(value.messagesError, '');
  assert.ok(calls >= 1);
});

test('a structural change keeps the open conversation but advances the generation', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler(async (path) => (path === '/state' ? state() : [message('kept')]));
  value.messages = [message('001')];
  value.threadId = '001';
  const generation = value.authorizationGeneration;
  value.receive({ t: 'workspace.changed', d: { reason: 'Channels were reordered.' } });
  // A category or channel move is not a change of this account's authority, so the
  // conversation on screen is kept while panels still drop what they captured.
  assert.equal(value.authorizationGeneration, generation + 1);
  assert.deepEqual(
    value.messages.map((item) => item.id),
    ['001']
  );
  assert.equal(value.threadId, '');
  await flush(value);
  assert.equal(value.authorizationGeneration, generation + 1);
});

test('a voice update carries one channel and replaces only its entries', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.voice_states = [
    { user_id: 'me', channel_id: 'a', muted: false },
    { user_id: 'other', channel_id: 'b', muted: false }
  ];
  // Voice presence arrives one channel at a time, so an update for a channel the
  // account may read cannot disturb what is shown for another one.
  value.receive({ t: 'voice.state_updated', d: { channel_id: 'a', states: [] } });
  assert.deepEqual(value.state.voice_states, [{ user_id: 'other', channel_id: 'b', muted: false }]);
  value.receive({
    t: 'voice.state_updated',
    d: { channel_id: 'b', states: [{ user_id: 'guest', channel_id: 'b', muted: true }] }
  });
  assert.deepEqual(value.state.voice_states, [{ user_id: 'guest', channel_id: 'b', muted: true }]);
});

test('a channel the snapshot no longer returns is not left cached', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler(async (path) => {
    if (path === '/state') return { ...state(), channels: [{ id: 'b', type: 'text' }] };
    return [message('001')];
  });
  value.messages = [message('001', { channel_id: 'a' })];
  assert.equal(value.activeId, 'a');
  await value.refreshState();
  // 'a' disappeared from the snapshot, so both the cache and the active channel
  // move on rather than rendering a conversation this account may no longer read.
  assert.equal(value.views.has('a'), false);
  assert.notEqual(value.activeId, 'a');
});

test('in-flight incremental requests cannot restore messages after sign-out', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const response = deferred();
  setHandler(() => response.promise);
  value.receive({ t: 'reaction.updated', d: { id: '001', channel_id: 'a' } });
  const loading = flush(value);
  value.stop();
  response.resolve(message('001'));
  await loading;
  assert.deepEqual(value.messages, []);
});

test('first unread uses the saved boundary, including after the server read marker advanced', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.unreadAfter = '010';
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return path.includes('first-unread')
      ? { message_id: '011' }
      : [message('011', { user_id: 'other' })];
  });
  await value.jumpToFirstUnread();
  assert.equal(calls[0], '/channels/a/first-unread?after=010');
  assert.equal(value.highlightId, '011');
  assert.equal(value.unreadAfter, '010');
  assert.equal(value.viewingHistory, true);
});

test('a history response captured before deletion cannot resurrect the message', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const response = deferred();
  setHandler((path) => (path === '/unread' ? Promise.resolve([]) : response.promise));
  const loading = value.loadMessages();
  value.receive({ t: 'message.deleted', d: { id: '001', channel_id: 'a' } });
  await flush(value);
  response.resolve([message('001')]);
  await loading;
  assert.deepEqual(value.messages, []);
});

test('edits received during a snapshot survive an older snapshot response', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messages = [message('001')];
  const response = deferred();
  setHandler((path) =>
    path.includes('/channels/')
      ? response.promise
      : path === '/unread'
        ? Promise.resolve([])
        : Promise.resolve(message('001', { content: 'new edit' }))
  );
  const loading = value.loadMessages();
  value.receive({ t: 'message.updated', d: { id: '001', channel_id: 'a' } });
  await flush(value);
  response.resolve([message('001', { content: 'old text' })]);
  await loading;
  assert.equal(value.messages[0].content, 'new edit');
});

// R4-02. A channel move is a mutation whose answer is a channel list. The account
// and session that issued it are captured before the request, so a response that
// arrives after another account signed in can never replace that account's channels.
// The pre-fix behaviour admitted the completion against the *current* session because
// moveChannel called admitted(generation) with the session defaulted.
test('a deferred channel move cannot cross into another account', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.authorization.capabilities = ['channel.manage'];
  const pending = deferred();
  setHandler(() => pending.promise);
  const moving = value.moveChannel('a', 'b', true);
  // The first account signs out and a different account enters. The authorization
  // generation is unchanged, which is exactly why the previous default-session
  // admission let the old answer through.
  value.stop();
  value.state = state();
  value.state.user = { id: 'another-account' };
  value.state.channels = [
    { id: 'a', type: 'text' },
    { id: 'b', type: 'text' }
  ];
  const expected = value.state.channels.map((channel) => channel.id);
  pending.resolve([{ id: 'old-account-private-channel', type: 'text', name: 'old private' }]);
  await moving;
  assert.deepEqual(
    value.state.channels.map((channel) => channel.id),
    expected,
    "a previous account's move response replaced the new account's channel list"
  );
});

// The permitted control for the case above: the same account, session and authority
// still applies the returned order, so the fix does not disable moves.
test('a channel move by the same account still applies the returned order', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.authorization.capabilities = ['channel.manage'];
  setHandler(async () => [
    { id: 'b', type: 'text' },
    { id: 'a', type: 'text' }
  ]);
  await value.moveChannel('a', 'b', true);
  assert.deepEqual(
    value.state.channels.map((channel) => channel.id),
    ['b', 'a']
  );
});

// A move admitted before an authorization change is dropped with the rest of the
// previous authority, even though the account and session are unchanged.
test('a channel move admitted before a revocation is dropped', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.authorization.capabilities = ['channel.manage'];
  const pending = deferred();
  setHandler(() => pending.promise);
  const moving = value.moveChannel('a', 'b', true);
  value.invalidateAuthorization();
  pending.resolve([{ id: 'x', type: 'text' }]);
  await moving;
  assert.deepEqual(
    value.state.channels.map((channel) => channel.id),
    ['a', 'b']
  );
});

// A history response belongs to the account that requested it, so a switch with the
// same channel identifiers and authorization revision cannot be filled by it.
test('a history response from a previous account cannot fill the next account', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const pending = deferred();
  setHandler(() => pending.promise);
  const loading = value.loadMessages();
  value.stop();
  value.state = state();
  value.state.user = { id: 'another-account' };
  pending.resolve([message('leak', { user_id: 'me' })]);
  await loading;
  assert.deepEqual(value.messages, []);
});

const recoveryAccount = '11111111-1111-4111-8111-111111111111';
const recoveryChannel = '22222222-2222-4222-8222-222222222222';
function recoveryWorkspace() {
  const value = workspace();
  value.state.user.id = recoveryAccount;
  value.state.channels = [{ id: recoveryChannel, type: 'text' }];
  value.activeId = recoveryChannel;
  store.clear();
  return value;
}
function savedSend(extra = {}) {
  return {
    key: crypto.randomUUID(),
    conversation: recoveryChannel,
    thread_id: '',
    content: 'text to keep',
    attachments: [],
    reply_to: '',
    created: new Date().toISOString(),
    ...extra
  };
}

test('terminal outcomes preserve complete text without opt-in, attachment claims or retry', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  const text = 'untrimmed user text '.repeat(180);
  value.drafts[recoveryChannel] = 'an unrelated draft';
  let calls = 0;
  setHandler(async () => {
    calls++;
    throw apiError('This send was already committed and its content is gone.', 409);
  });
  await value.send(text, [{ id: crypto.randomUUID() }]);
  const recovered = value.recoveredMessages[0];
  assert.equal(recovered.content, text);
  assert.deepEqual(recovered.attachments, []);
  assert.equal(recovered.reply_to, '');
  assert.equal(value.drafts[recoveryChannel], 'an unrelated draft');
  assert.equal(value.hasPendingMessages, false);
  assert.deepEqual(store.keys(), [], 'opt-out never writes author text');
  await value.retryMessage(
    message(recovered.key, {
      user_id: recoveryAccount,
      channel_id: recoveryChannel,
      idempotency_key: recovered.key
    })
  );
  assert.equal(calls, 1, 'a terminal send cannot reuse its old operation');
  value.dismissRecovery(recovered.key);
  assert.equal(value.recoveredMessages.length, 0);
});

test('expired and revoked destinations restore for recovery without sending any request', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  const entries = [
    savedSend({
      created: new Date(Date.now() - 25 * 3600000).toISOString(),
      attachments: [crypto.randomUUID()]
    }),
    savedSend({ conversation: crypto.randomUUID(), content: 'destination removed' })
  ];
  store.setItem(`brrp.outbox.${recoveryAccount}`, JSON.stringify(entries));
  setHandler(() => assert.fail('a non-retryable operation must not reach the server'));
  await value.restoreOutbox();
  assert.equal(value.recoveredMessages.length, 2);
  assert.deepEqual(
    value.recoveredMessages.map((entry) => entry.content),
    entries.map((entry) => entry.content)
  );
  assert.ok(value.recoveredMessages.every((entry) => !entry.attachments.length));
  assert.equal(value.hasPendingMessages, false);
  await value.restoreOutbox();
  assert.equal(value.recoveredMessages.length, 2, 'a reload restores recovery without sending');
});

test('outbox restoration keeps later entries durable and cannot continue into another account', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  const entries = [savedSend(), savedSend({ content: 'second private text' })];
  store.setItem(`brrp.outbox.${recoveryAccount}`, JSON.stringify(entries));
  const first = deferred();
  let calls = 0;
  setHandler(() => {
    calls++;
    return first.promise;
  });
  const restoring = value.restoreOutbox();
  assert.equal(JSON.parse(store.getItem(`brrp.outbox.${recoveryAccount}`)).length, 2);
  value.stop();
  value.state.user.id = crypto.randomUUID();
  value.saveDrafts = true;
  first.reject(apiError('Forbidden', 403));
  await restoring;
  assert.equal(calls, 1, 'second entry is never sent using the new session');
  assert.equal(value.recoveredMessages.length, 0);
  assert.equal(value.hasPendingMessages, false);
  assert.equal(store.getItem(`brrp.outbox.${value.state.user.id}`), null);
});

test('acknowledging one restored send never drops later unconfirmed sends', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  const entries = [savedSend(), savedSend({ content: 'next unconfirmed' })];
  store.setItem(`brrp.outbox.${recoveryAccount}`, JSON.stringify(entries));
  const second = deferred();
  let calls = 0;
  setHandler(async () => {
    if (++calls === 1)
      return message('saved', {
        idempotency_key: entries[0].key,
        user_id: recoveryAccount,
        channel_id: recoveryChannel
      });
    assert.deepEqual(
      JSON.parse(store.getItem(`brrp.outbox.${recoveryAccount}`)).map((entry) => entry.key),
      [entries[1].key]
    );
    return second.promise;
  });
  const restoring = value.restoreOutbox();
  await new Promise((resolve) => setImmediate(resolve));
  second.reject(apiError('server busy', 503));
  await restoring;
  assert.equal(calls, 2);
  assert.equal(JSON.parse(store.getItem(`brrp.outbox.${recoveryAccount}`))[0].key, entries[1].key);
});

test('disabling persistence clears saved sends but preserves the current session recovery', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  setHandler(async () => {
    throw apiError('This send has expired.', 409);
  });
  await value.send('keep me here', []);
  assert.ok(store.getItem(`brrp.outbox.${recoveryAccount}`));
  value.saveDrafts = false;
  value.savePreferences();
  assert.equal(store.getItem(`brrp.outbox.${recoveryAccount}`), null);
  assert.equal(value.recoveredMessages[0].content, 'keep me here');
  assert.equal(store.getItem(`brrp.save-drafts.${recoveryAccount}`), 'false');
  value.stop();
  assert.equal(value.recoveredMessages.length, 0);
  assert.equal(value.saveDrafts, false);
});

test('out-of-order successful read acknowledgements never move the cursor backward', async (t) => {
  const value = workspace();
  t.after(() => {
    value.stop();
    setDocumentFocus(true, false);
  });
  setDocumentFocus(false, true);
  const first = deferred(),
    second = deferred();
  const calls = [];
  setHandler((path, method, body) => {
    calls.push(body.last_read_message_id);
    return calls.length === 1 ? first.promise : second.promise;
  });
  value.messages = [message('001')];
  const old = value.markRead();
  value.messages = [message('002')];
  const recent = value.markRead();
  second.resolve({});
  await recent;
  first.resolve({});
  await old;
  assert.equal(value.readCursors.a, '002');
  value.messages = [message('001')];
  await value.markRead();
  assert.deepEqual(calls, ['001', '002']);
});

test('a message moderator can reach report tools without receiving unrelated management authority', () => {
  const value = workspace();
  value.state.authorization.capabilities = ['channel.read', 'message.delete_any'];
  assert.equal(value.canManageAnything, true);
  assert.equal(value.can('member.manage'), false);
  assert.equal(value.can('role.manage'), false);
  value.state.authorization.capabilities = ['channel.read'];
  assert.equal(value.canManageAnything, false);
});

test('confirmed channel mutes suppress queued mention interruptions without changing unread state', () => {
  const value = workspace();
  value.state.unread = [{ channel_id: 'a', count: 4, mentions: 2 }];
  const mention = { t: 'notification.mention', d: { channel_id: 'a', body: 'mentioned you' } };
  value.receive(mention);
  assert.equal(value.toast, '', 'unknown preferences withhold interruptions');
  value.channelNotificationsReady = true;
  value.channelNotificationMutes = { a: true };
  value.receive(mention);
  assert.equal(value.toast, '');
  assert.deepEqual(value.state.unread, [{ channel_id: 'a', count: 4, mentions: 2 }]);
  value.channelNotificationMutes.a = false;
  value.receive(mention);
  assert.equal(value.toast, 'mentioned you', 'unmuting restores notices');
  value.stop();
  assert.equal(value.channelNotificationsReady, false);
  assert.deepEqual(value.channelNotificationMutes, {});
});

test('muting while service-worker readiness is pending prevents the delayed notification', async (t) => {
  const value = workspace();
  const ready = deferred();
  setNotificationWorker(ready.promise);
  setDocumentFocus(true, false);
  t.after(() => {
    value.stop();
    setNotificationWorker(null);
  });
  value.channelNotificationsReady = true;
  let notifications = 0;
  const mention = {
    t: 'notification.mention',
    d: { channel_id: 'a', body: 'mentioned you', title: 'server' }
  };
  value.receive(mention);
  value.channelNotificationMutes.a = true;
  ready.resolve({
    showNotification: () => {
      notifications++;
    }
  });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 0);
  value.channelNotificationMutes.a = false;
  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 1, 'a confirmed unmute permits later notifications');
});

// A desktop notification is for a conversation the member is not looking at.
//
// The check used to be `document.hidden` alone, which is true only when the whole tab
// is in the background. A member with the tab open on another conversation — or reading
// back through history in this one — is visible to the browser and was still interrupted
// by an operating system banner for a message they could watch arrive. On a laptop that
// is a banner over the conversation being read, and it teaches people to dismiss banners
// without reading them.
//
// The rule is the same one that decides whether the read cursor moves, so both now
// answer one question. The conversation matters as much as the view state: a mention
// arriving in another channel is still an interruption for a member reading this one,
// and the `channel_id` is what tells the two apart.
test('a member watching the conversation a mention names is not interrupted', async (t) => {
  const value = workspace();
  t.after(() => {
    setDocumentFocus(true, false);
    value.stop();
  });
  // `setDocumentFocus(hidden, focused)`: a visible, focused window is (false, true). This
  // is the state the old `document.hidden` check treated as *not* backgrounded, and the
  // one it then interrupted anyway when the mention named another conversation.
  setDocumentFocus(false, true);
  value.channelNotificationsReady = true;
  let notifications = 0;
  const ready = { promise: Promise.resolve({ showNotification: () => notifications++ }) };
  setNotificationWorker(ready.promise);
  const mention = {
    t: 'notification.mention',
    d: { channel_id: 'a', body: 'hi', title: 'server' }
  };

  // The member is in conversation 'a' (the harness sets activeId) at the live tail.
  assert.equal(value.activeId, 'a');

  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 0, 'a member watching the conversation was interrupted');

  // Scrolled back through history in the same conversation: not watching, so notified.
  value.atLatest = false;
  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 1, 'a member reading history was not notified');

  // Deliberately in history mode, still in the same conversation: still notified.
  value.atLatest = true;
  value.viewingHistory = true;
  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 2, 'a member in history mode was not notified');

  // Back at the live tail: quiet again.
  value.viewingHistory = false;
  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 2, 'a member back at the live tail was interrupted again');

  // A visible, focused tab that has lost focus is not being read either.
  setDocumentFocus(false, false);
  value.receive(mention);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(notifications, 3, 'a visible but unfocused window was not notified');
});

// R8-02: mention policy is account state the workspace verifies from a confirmed
// snapshot — not a fetch owned by the channel header, which never mounts while the
// account stays in a direct conversation.
test('a confirmed snapshot verifies mention notification policy without a channel header', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  assert.equal(value.channelNotificationsReady, false, 'policy starts unverified');
  setHandler((path) => {
    if (path === '/state') return Promise.resolve(state());
    if (path === '/me/channel-notifications')
      return Promise.resolve([{ channel_id: 'a', muted: false }]);
    return Promise.resolve([]);
  });
  await value.refreshState();
  // The policy load follows the admitted snapshot on its own microtask chain.
  for (let i = 0; i < 100 && !value.channelNotificationsReady; i++)
    await new Promise((resolve) => setImmediate(resolve));
  assert.equal(value.channelNotificationsReady, true, 'snapshot admits the policy load');
  assert.deepEqual(value.channelNotificationMutes, { a: false });
  value.receive({ t: 'notification.mention', d: { channel_id: 'a', body: 'mentioned you' } });
  assert.equal(value.toast, 'mentioned you');
});

// R8-02: a preference read that started before a mute write must not apply its older
// answer over the write's own confirmed result.
test('a delayed preference read cannot overwrite a newer mute write', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.channelNotificationsReady = true;
  value.channelNotificationMutes = { a: false };
  const read = deferred();
  setHandler((path) => (path === '/me/channel-notifications' ? read.promise : Promise.resolve([])));
  const loading = value.loadChannelNotifications();
  setHandler((path) =>
    path === '/channels/a/notifications'
      ? Promise.resolve({ channel_id: 'a', muted: true })
      : read.promise
  );
  const muted = value.setChannelNotificationMuted('a', true);
  assert.equal(await muted, true, 'the mute write applied');
  assert.equal(value.channelNotificationMutes.a, true);
  read.resolve([]);
  assert.equal(await loading, false, 'the stale read was dropped');
  assert.equal(value.channelNotificationMutes.a, true, 'the newer mute survives');
});

// R8-02: a policy read answered after the account changed is dropped entirely; the
// next account starts unverified rather than inheriting another account's mutes.
test('a preference answer for a previous account is never installed', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const read = deferred();
  setHandler((path) => (path === '/me/channel-notifications' ? read.promise : Promise.resolve()));
  const loading = value.loadChannelNotifications();
  value.state = { ...state(), user: { id: 'other' } };
  read.resolve([{ channel_id: 'a', muted: true }]);
  assert.equal(await loading, false, "the previous account's answer was dropped");
  assert.equal(value.channelNotificationsReady, false);
  assert.deepEqual(value.channelNotificationMutes, {});
});

// R8-02: a failed load keeps the policy unverified, so allowed mentions stay
// withheld (fail-closed) instead of falling back to unfiltered delivery.
test('a failed preference load keeps mention policy fail-closed', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler((path) =>
    path === '/me/channel-notifications'
      ? Promise.reject(apiError('preference service unavailable', 500))
      : Promise.resolve(undefined)
  );
  assert.equal(await value.loadChannelNotifications(), false);
  assert.equal(value.channelNotificationsReady, false, 'unverified policy stays withheld');
  assert.match(value.channelNotificationError, /preference service unavailable/);
  value.receive({ t: 'notification.mention', d: { channel_id: 'a', body: 'mentioned you' } });
  assert.equal(value.toast, '', 'an unverified failure never permits interruptions');
});

// R9-02: a mute is account-owned policy, so a change written on one device has to
// reach the others. These cases cover the three ways a session can miss it that
// survive the R8-02 repair: a session sitting in a direct conversation (no channel
// header is mounted), a same-revision reconnect (no authority change is published),
// and a window that was hidden while the change was made.
test('another device changing a mute re-reads policy for a session in a direct conversation', async (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  let muted = true;
  setHandler((path) =>
    path === '/me/channel-notifications'
      ? Promise.resolve([{ channel_id: 'a', muted }])
      : Promise.resolve([])
  );
  assert.equal(await value.loadChannelNotifications(), true);
  assert.equal(value.channelNotificationMutes.a, true);
  const mention = { t: 'notification.mention', d: { channel_id: 'a', body: 'mentioned you' } };
  value.receive(mention);
  assert.equal(value.toast, '', 'the stale mute withholds the mention before the signal');

  // The other device unmutes. The signal carries no policy, only the reason to
  // re-read, so the answer still comes from the server.
  muted = false;
  value.receive({ t: 'preferences.changed', d: { channel_notifications: true } });
  assert.equal(value.channelNotificationsReady, false, 'the cached answer is dropped first');
  value.receive(mention);
  assert.equal(
    value.toast,
    '',
    'a mention in the gap is withheld, not answered from a known-stale answer'
  );
  for (let i = 0; i < 100 && !value.channelNotificationsReady; i++)
    await new Promise((resolve) => setImmediate(resolve));
  assert.equal(value.channelNotificationMutes.a, false, "the other device's unmute is installed");
  value.receive(mention);
  assert.equal(value.toast, 'mentioned you', 'the unmuted mention is delivered');
});

// R9-02: the reconnect path must re-verify too. A same-revision reconnect
// publishes no authority change, so without this the ready flag survives and the
// account keeps filtering on the answer it loaded before the socket dropped.
test('reconnecting re-verifies mention policy even when authority did not move', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  setHandler((path) =>
    path === '/me/channel-notifications'
      ? Promise.resolve([{ channel_id: 'a', muted: true }])
      : Promise.resolve(state())
  );
  assert.equal(await value.loadChannelNotifications(), true);
  assert.equal(value.channelNotificationsReady, true);
  setHandler((path) =>
    path === '/me/channel-notifications'
      ? Promise.resolve([{ channel_id: 'a', muted: false }])
      : Promise.resolve(state())
  );
  value.receive({ t: 'hello', d: { refetch: true } });
  assert.equal(value.channelNotificationsReady, false, 'the cached policy is dropped first');
  for (let i = 0; i < 100 && !value.channelNotificationsReady; i++)
    await new Promise((resolve) => setImmediate(resolve));
  assert.equal(value.channelNotificationMutes.a, false, 'the reconnect re-read the policy');
});

// R9-02: focus is the last chance for a change made while this window was hidden,
// and it must be rate-limited so it cannot become a poll.
test('focus re-verifies policy once per window focus', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  let reads = 0;
  setHandler((path) => {
    if (path !== '/me/channel-notifications') return Promise.resolve(state());
    reads++;
    return Promise.resolve([{ channel_id: 'a', muted: false }]);
  });
  value.state = state();
  value.reverifyPreferences();
  value.reverifyPreferences();
  value.reverifyPreferences();
  for (let i = 0; i < 100 && !value.channelNotificationsReady; i++)
    await new Promise((resolve) => setImmediate(resolve));
  assert.equal(reads, 1, 'repeated focus events collapse into one re-read');
});

// R9-02: a policy read belongs to the account that asked for it. A refresh the
// previous account requested is not an outstanding request for the next one, and
// an answer that arrives after the switch is refused rather than installed.
test('a preference refresh does not install across an account switch', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const read = deferred();
  let reads = 0;
  setHandler((path) => {
    if (path !== '/me/channel-notifications') return Promise.resolve(state());
    reads++;
    return read.promise;
  });
  value.state = state();
  value.receive({ t: 'preferences.changed', d: { channel_notifications: true } });
  assert.equal(reads, 1, 'the signal issues the re-read it asked for');
  value.state = { ...state(), user: { id: 'other' } };
  read.resolve([{ channel_id: 'a', muted: true }]);
  for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve));
  assert.equal(
    value.channelNotificationsReady,
    false,
    "the previous account's policy was not installed"
  );
  assert.deepEqual(value.channelNotificationMutes, {});
  value.stop();
  reads = 0;
  value.state = { ...state(), user: { id: 'other' } };
  for (let i = 0; i < 20; i++) await new Promise((resolve) => setImmediate(resolve));
  assert.equal(reads, 0, 'the next account did not inherit the previous refresh request');
});

const directID = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
function directWorkspace() {
  const value = recoveryWorkspace();
  value.state.features = { private_conversations: true };
  value.state.conversations = [
    {
      id: directID,
      kind: 'direct',
      state: 'active',
      peer: { id: 'peer', username: 'peer', display_name: 'Peer' },
      can_read: true,
      can_write: true,
      blocked: false,
      blocked_by_me: false,
      request_state: 'accepted',
      request_direction: 'none',
      introduction: '',
      hidden: false,
      count: 2,
      mentions: 0,
      last_read_message_id: '',
      updated_at: new Date().toISOString()
    }
  ];
  value.activeId = directID;
  return value;
}

test('an accepted direct conversation uses the shared send, history and receipt path without a shadow channel', async (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  const calls = [];
  let sent;
  setHandler(async (path, method, body) => {
    calls.push({ path, method, body });
    if (method === 'POST') {
      sent = message('direct-saved', {
        user_id: recoveryAccount,
        channel_id: '',
        conversation_id: directID,
        idempotency_key: body.idempotency_key,
        content: body.content
      });
      return sent;
    }
    return [sent];
  });
  assert.equal(value.channel, undefined, 'there is no fake community channel');
  await value.send('a private message', []);
  assert.equal(calls[0].path, `/conversations/${directID}/messages`);
  assert.equal(value.messages[0].conversation_id, directID);
  assert.equal(value.messages[0].channel_id, '');
  assert.equal(value.hasPendingMessages, false);
  assert.equal(store.getItem(`brrp.outbox.${recoveryAccount}`), null);
  await value.loadMessages();
  assert.equal(calls[1].path, `/conversations/${directID}/messages`);
  assert.equal(value.messages[0].content, 'a private message');
});

// state from the snapshot, not an accumulation. These cases pin the properties
// that make it safe to draw: it is hidden rather than zero while the account or
// its authority is unverified, it does not move on its own when the dialog opens,
// and it survives an account switch without carrying the previous account's count.
test('the request badge is hidden, not zero, while the account is unverified', (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  value.state.requests = { outstanding_incoming: 3 };
  value.channelNotificationsReady = true;
  assert.equal(value.authorizationUnverified, false);
  assert.equal(value.outstandingIncomingRequests, 3, 'a confirmed count is shown');
  value.invalidateAuthorization();
  assert.equal(value.authorizationUnverified, true);
  assert.equal(
    value.outstandingIncomingRequests,
    0,
    'an unverified account shows no count rather than a wrong zero'
  );
});

test('the request count is per account and never carried across a switch', (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  value.state.requests = { outstanding_incoming: 2 };
  assert.equal(value.outstandingIncomingRequests, 2);
  // Opening the messages dialog is a view, not an acknowledgement: the count is
  // the server's and only the server clears it by accepting or declining.
  value.messagesOpen = true;
  value.messagesTab = 'requests';
  assert.equal(value.outstandingIncomingRequests, 2, 'opening the dialog does not clear it');
  // A different account starts from its own snapshot, not the previous one.
  value.state = { ...directWorkspace().state, user: { id: 'other' } };
  value.state.requests = { outstanding_incoming: 1 };
  assert.equal(value.outstandingIncomingRequests, 1, "the previous account's count is gone");
});

test('a private-disabled server never shows a request badge', (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.features = { private_conversations: false };
  value.state.requests = { outstanding_incoming: 4 };
  assert.equal(value.outstandingIncomingRequests, 0, 'no badge without the feature');
  value.messagesTab = 'requests';
  assert.equal(value.messagesTab, 'requests', 'the tab choice is still remembered');
});

test('blocking a direct preserves readable history and recovers pending text without replay on unblock', async (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  const held = deferred();
  let posts = 0;
  const history = [
    message('history', { channel_id: '', conversation_id: directID, content: 'accepted history' })
  ];
  setHandler((path, method) => {
    if (method === 'POST') {
      posts++;
      return held.promise;
    }
    if (path === '/state') return structuredClone(value.state);
    return history;
  });
  const sending = value.send('preserve my unconfirmed text', []);
  value.state.conversations[0].can_write = false;
  value.state.conversations[0].blocked = true;
  value.invalidateAuthorization();
  await value.refreshState();
  held.reject(apiError('Send not allowed', 403));
  await sending;
  assert.equal(value.canReadConversation(directID), true);
  assert.equal(value.canWriteConversation(directID), false);
  assert.equal(value.recoveredMessages[0].content, 'preserve my unconfirmed text');
  assert.deepEqual(value.recoveredMessages[0].attachments, []);
  assert.equal(value.hasPendingMessages, false);
  value.state.conversations[0].can_write = true;
  value.state.conversations[0].blocked = false;
  await value.restoreOutbox();
  assert.equal(posts, 1, 'unblocking never automatically resends recovered intent');
  assert.equal(value.recoveredMessages.length, 1);
});

test('direct read cursors and unread rows use conversation IDs and stay separate from community channels', async (t) => {
  const value = directWorkspace();
  t.after(() => {
    value.stop();
    setDocumentFocus(true, false);
  });
  setDocumentFocus(false, true);
  value.state.unread = [
    { channel_id: '', conversation_id: directID, count: 3, mentions: 0, last_read_message_id: '' }
  ];
  value.messages = [message('003', { channel_id: '', conversation_id: directID })];
  const calls = [];
  setHandler(async (path) => {
    calls.push(path);
    return {};
  });
  await value.markRead();
  assert.deepEqual(calls, [`/conversations/${directID}/read`]);
  assert.equal(value.unreadFor(directID).count, 0);
  assert.equal(value.readCursors[directID], '003');
  assert.equal(value.readCursors[''], undefined);
});

test('disabling private conversations clears their scope even when the authorization revision is unchanged', async (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  const old = deferred();
  const oldLoad = (() => {
    setHandler(() => old.promise);
    return value.loadMessages();
  })();
  const next = structuredClone(value.state);
  next.features.private_conversations = false;
  next.conversations = [];
  setHandler(async (path) => (path === '/state' ? next : []));
  await value.refreshState();
  old.resolve([
    message('restricted', {
      channel_id: '',
      conversation_id: directID,
      content: 'old private answer'
    })
  ]);
  await oldLoad;
  assert.equal(value.directConversations.length, 0);
  assert.equal(
    value.messages.some((item) => item.id === 'restricted'),
    false
  );
  assert.notEqual(value.activeId, directID);
});

test('an uncertain send overtaken by an allowed authority refresh is retryable rather than stuck sending', async (t) => {
  const value = directWorkspace();
  t.after(() => value.stop());
  const held = deferred();
  setHandler(() => held.promise);
  const sending = value.send('same operation, uncertain answer', []);
  const key = Object.keys(value.pending)[0];
  value.invalidateAuthorization();
  held.reject(apiError('old server answer', 503));
  await sending;
  assert.equal(value.pending[key].pending, 'failed');
  assert.equal(value.pending[key].idempotency_key, key);
  assert.equal(value.messages.length, 0, 'old authority cannot refill the cleared view');
  assert.notEqual(value.toast, 'old server answer');
});

// Following an activity entry opens the conversation it happened in.
//
// The workspace is asked directly rather than by writing `location.hash`: the hash is
// read once, when the workspace enters, so writing it while the app is running changes
// the address bar and nothing else — a link that moves the URL and not the page is worse
// than a button that plainly does nothing.
//
// Authority is re-read when the conversation opens, so a stale entry cannot become a
// grant: it opens whatever that account may now read, and nothing more.

test('following an activity entry opens its conversation', async (t) => {
  const value = workspace();
  t.after(() => {
    setDocumentFocus(true, false);
    value.stop();
  });
  setDocumentFocus(true, true);
  const opened = [];
  value.selectChannel = async (id) => {
    opened.push(id);
  };

  await value.followActivityDestination({ kind: 'message', conversation_id: 'a' });
  assert.deepEqual(opened, ['a'], 'the conversation was not opened');
});

test('following a request entry opens the requests tab', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const opened = [];
  value.selectChannel = async (id) => {
    opened.push(id);
  };

  await value.followActivityDestination({ kind: 'request', conversation_id: 'r1' });
  assert.equal(value.messagesTab, 'requests');
  assert.equal(value.messagesOpen, true);
  assert.deepEqual(opened, [], 'a request entry opened a conversation instead');
});

test('following an entry closes the dialog it was listed in', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.messagesOpen = true;
  value.selectChannel = async () => {};
  await value.followActivityDestination({ kind: 'message', conversation_id: 'a' });
  assert.equal(value.messagesOpen, false, 'the messages dialog stayed open over the conversation');
});

test('an entry whose conversation is gone opens nothing rather than the wrong thing', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const opened = [];
  value.selectChannel = async (id) => {
    opened.push(id);
  };
  value.messagesOpen = true;

  // No conversation at all.
  await value.followActivityDestination({ kind: 'message' });
  assert.deepEqual(opened, []);
  assert.equal(value.messagesOpen, true, 'the dialog closed for an entry with nowhere to go');

  // A conversation this account cannot open, which is what a stale entry names.
  await value.followActivityDestination({ kind: 'message', conversation_id: 'somebody-elses' });
  assert.deepEqual(opened, [], 'an unopenable conversation was opened anyway');
  assert.equal(value.messagesOpen, true);
});

// Review 15, V15-01: the initiating account, session and authority are captured
// before the first asynchronous step, and the destination is never inferred from
// whatever conversation happens to be open.
test('a thread lookup that resolves after an account switch sends nothing as the new account', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const key = '30000000-0000-4000-8000-000000000001';
  const lookup = deferred();
  const calls = [];
  setHandler((path, method) => {
    calls.push({ path, method });
    if (method === 'GET') return lookup.promise;
    return Promise.resolve(message('saved', { idempotency_key: key }));
  });
  const sending = value.sendIntoThread('thread-1', 'A wrote this', key);
  // A opened the send; B takes the view while the thread lookup is still in flight.
  value.state.user = { id: crypto.randomUUID() };
  lookup.resolve({ conversation_id: 'a' });
  const outcome = await sending;
  assert.equal(outcome.state, 'dropped', 'a moved view completed the operation anyway');
  assert.deepEqual(
    calls.filter((call) => call.method === 'POST'),
    [],
    'the first account\u2019s text was posted as the second'
  );
  assert.equal(value.pending[key], undefined);
  assert.equal(value.outbox.length, 0);
  assert.equal(store.getItem(`brrp.outbox.${value.state.user.id}`), null);
});

test('a failed thread lookup never falls back to the active conversation', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const key = '30000000-0000-4000-8000-000000000002';
  const calls = [];
  setHandler((path, method) => {
    calls.push({ path, method });
    if (method === 'GET') return Promise.reject(new Error('offline'));
    return Promise.resolve(message('saved', { idempotency_key: key }));
  });
  const outcome = await value.sendIntoThread('thread-1', 'held text', key);
  assert.equal(outcome.state, 'refused');
  assert.deepEqual(
    calls.filter((call) => call.method === 'POST'),
    [],
    'a failed lookup sent the text into the open channel instead'
  );
  assert.equal(value.outbox.length, 0, 'nothing was persisted for an operation that never started');
  assert.equal(value.toast, 'offline');
});

test('a pending named-thread send is visible in its thread and not in the channel', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  const key = '30000000-0000-4000-8000-000000000003';
  setHandler((path, method) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: 'a' });
    return Promise.reject(apiError('server busy', 503));
  });
  const outcome = await value.sendIntoThread('thread-1', 'a thread reply', key);
  assert.equal(outcome.state, 'uncertain');
  const pending = value.pendingReplies('thread-1');
  assert.equal(pending.length, 1, 'the pending reply is not shown in its thread');
  assert.equal(pending[0].pending, 'failed');
  assert.equal(pending[0].thread_id, 'thread-1');
  assert.deepEqual(value.messages, [], 'a thread reply became a channel-history row');
});

// The destination a thread operation was submitted to survives a reload: a lost
// response is reconciled under the same key against the same thread, and a failure
// before commit retries into the thread rather than into its parent channel.
test('a lost thread-send response replays under the same key after a reload', async (t) => {
  const first = workspace();
  t.after(() => first.stop());
  // Durable outbox storage is account-scoped by UUID. A fixture that is not a valid
  // account id persists nothing, and a test that persists nothing cannot establish
  // recovery.
  first.state.user.id = recoveryAccount;
  first.saveDrafts = true;
  store.clear();
  const key = '30000000-0000-4000-8000-000000000004';
  const thread = crypto.randomUUID();
  const conversation = crypto.randomUUID();
  first.state.channels = [...first.state.channels, { id: conversation, type: 'text' }];
  first.activeId = conversation;
  setHandler((path, method) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    // The server stored the message and the response was lost on the way back.
    return Promise.reject(new Error('connection lost after delivery'));
  });
  const outcome = await first.sendIntoThread(thread, 'lost acknowledgement', key);
  assert.equal(outcome.state, 'uncertain');
  const account = first.state.user.id;
  const durable = store.getItem(`brrp.outbox.${account}`);
  assert.ok(durable, 'the uncertain operation was never persisted');
  const held = JSON.parse(durable);
  assert.equal(held[0].thread_id, thread, 'the destination is durable');
  assert.equal(held[0].content, 'lost acknowledgement', 'the intent is durable');
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.state.user.id = account;
  second.state.channels = [...second.state.channels, { id: conversation, type: 'text' }];
  second.activeId = conversation;
  second.saveDrafts = true;
  const calls = [];
  setHandler((path, method, body) => {
    calls.push({ path, method, body });
    return Promise.resolve(
      message('88888888-8888-4888-8888-888888888888', {
        idempotency_key: key,
        thread_id: thread,
        user_id: account,
        content: 'lost acknowledgement'
      })
    );
  });
  await second.restoreOutbox();
  const posts = calls.filter((call) => call.method === 'POST');
  assert.equal(posts.length, 1, 'the operation was submitted a different number of times');
  assert.equal(posts[0].path, `/threads/${thread}/messages`, 'the replay left its thread');
  assert.equal(posts[0].body.idempotency_key, key, 'the immutable key was not reused');
  assert.equal(posts[0].body.content, 'lost acknowledgement');
  assert.equal(store.getItem(`brrp.outbox.${account}`), null, 'the answer releases the entry');
  assert.equal(second.pending[key], undefined);
});

test('a thread operation that failed before commit retries into its thread, never the channel', async (t) => {
  const first = workspace();
  t.after(() => first.stop());
  first.state.user.id = recoveryAccount;
  first.saveDrafts = true;
  store.clear();
  const key = '30000000-0000-4000-8000-000000000005';
  const thread = crypto.randomUUID();
  const conversation = crypto.randomUUID();
  first.state.channels = [...first.state.channels, { id: conversation, type: 'text' }];
  first.activeId = conversation;
  setHandler((path, method) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    return Promise.reject(apiError('server busy', 503));
  });
  const outcome = await first.sendIntoThread(thread, 'retry me into the thread', key);
  assert.equal(outcome.state, 'uncertain');
  const account = first.state.user.id;
  assert.ok(store.getItem(`brrp.outbox.${account}`), 'the operation is durable');
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.state.user.id = account;
  second.state.channels = [...second.state.channels, { id: conversation, type: 'text' }];
  second.activeId = conversation;
  second.saveDrafts = true;
  const calls = [];
  setHandler((path, method, body) => {
    calls.push({ path, method, body });
    return Promise.resolve(
      message('99999999-9999-4999-8999-999999999999', {
        idempotency_key: key,
        thread_id: thread,
        user_id: account
      })
    );
  });
  await second.restoreOutbox();
  const posts = calls.filter((call) => call.method === 'POST');
  assert.equal(posts.length, 1, 'exactly one message is created');
  assert.equal(posts[0].path, `/threads/${thread}/messages`);
  assert.ok(
    !calls.some((call) => call.path === `/channels/${conversation}/messages`),
    'a thread reply was routed into the channel it lives in'
  );
  assert.deepEqual(
    second.messages.filter((item) => item.id === key),
    [],
    'a channel-history row was created for the thread reply'
  );
  assert.equal(store.getItem(`brrp.outbox.${account}`), null);
});

// Review 15, V15-01: account isolation includes component teardown, not only requests
// and storage keys. A composer that unmounts after the session has moved presents the
// identity it was mounted under; its text goes into that account's own device scope at
// most, and never into the store or storage of the account that replaced it.
test('a composer cleanup that runs after a switch cannot write into the new account', (t) => {
  const first = workspace();
  t.after(() => first.stop());
  first.state.user.id = recoveryAccount;
  first.saveDrafts = true;
  store.clear();
  // The old account had draft saving on, so its text is still recoverable in its own
  // scope rather than being silently dropped.
  store.setItem(`brrp.save-drafts.${recoveryAccount}`, 'true');
  const owner = first.composerOwner();
  const slot = `thread:${crypto.randomUUID()}`;
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.state.user.id = crypto.randomUUID();
  second.composerDrafts.set(slot, { text: 'B owns this slot now', attachments: [] });

  first.saveComposerDraft(slot, 'A wrote this', [], owner);
  assert.equal(
    second.composerDrafts.get(slot).text,
    'B owns this slot now',
    "A's late cleanup overwrote B's draft"
  );
  assert.equal(second.drafts[slot], undefined, "A's text leaked into B's drafts");
  assert.equal(
    store.getItem(`brrp.drafts.${second.state.user.id}`),
    null,
    "A's text was persisted into B's storage scope"
  );
  const kept = JSON.parse(store.getItem(`brrp.drafts.${recoveryAccount}`));
  assert.equal(kept[slot], 'A wrote this', "A's own draft was not recoverable in its own scope");
});

// The other half of teardown isolation: a cleanup must not delete the new account's
// draft either. An empty composer closing late used to clear a slot the new account
// had already filled.
test('a composer cleanup cannot clear the new account\u2019s draft', (t) => {
  const first = workspace();
  t.after(() => first.stop());
  first.state.user.id = recoveryAccount;
  const owner = first.composerOwner();
  const slot = `thread:${crypto.randomUUID()}`;
  first.stop();

  const second = workspace();
  t.after(() => second.stop());
  second.state.user.id = crypto.randomUUID();
  second.composerDrafts.set(slot, { text: 'B is still typing', attachments: [] });
  second.drafts[slot] = 'B is still typing';

  first.clearComposerDraft(slot, owner);
  assert.equal(second.composerDrafts.get(slot).text, 'B is still typing');
  assert.equal(second.drafts[slot], 'B is still typing');
});

// A sign-out deliberately clears the account's drafts and unconfirmed sends from the
// device. A composer unmounting afterwards must not write them back.
test('a sign-out clears the device scope and a late cleanup cannot put it back', async (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  store.setItem(`brrp.save-drafts.${recoveryAccount}`, 'true');
  const owner = value.composerOwner();
  const slot = `thread:${crypto.randomUUID()}`;
  setHandler(async () => ({}));
  await value.logout();
  assert.equal(store.getItem(`brrp.drafts.${recoveryAccount}`), null);
  value.saveComposerDraft(slot, 'signed-out text', [], owner);
  assert.equal(
    store.getItem(`brrp.drafts.${recoveryAccount}`),
    null,
    'a late cleanup resurrected text the sign-out cleared'
  );
});

// The same rule for the preference: turning draft saving off clears the scope, and a
// pending composer must not write the text back after the person asked for it to go.
test('turning draft saving off blocks a late cleanup from rewriting the cleared scope', (t) => {
  const value = recoveryWorkspace();
  t.after(() => value.stop());
  value.saveDrafts = true;
  store.setItem(`brrp.save-drafts.${recoveryAccount}`, 'true');
  const owner = value.composerOwner();
  const slot = `thread:${crypto.randomUUID()}`;
  value.saveDrafts = false;
  value.savePreferences();
  assert.equal(store.getItem(`brrp.drafts.${recoveryAccount}`), null);
  value.saveComposerDraft(slot, 'late text', [], owner);
  assert.equal(store.getItem(`brrp.drafts.${recoveryAccount}`), null);
});

// An unresolved operation's key is its immutable intent. Other words submitted under
// that key are refused before they reach the server, and the durable record keeps the
// original words so the original retry still reconciles.
test('an unresolved operation keeps its original intent when other text reuses its key', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.user.id = recoveryAccount;
  value.saveDrafts = true;
  store.clear();
  const key = '30000000-0000-4000-8000-000000000006';
  const thread = crypto.randomUUID();
  const conversation = crypto.randomUUID();
  value.state.channels = [{ id: conversation, type: 'text' }];
  value.activeId = conversation;
  const posts = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    posts.push(body);
    return Promise.reject(apiError('server busy', 503));
  });
  assert.equal((await value.sendIntoThread(thread, 'the original reply', key)).state, 'uncertain');
  assert.equal(value.unresolvedThreadOperation(thread).content, 'the original reply');
  const durable = JSON.parse(store.getItem(`brrp.outbox.${recoveryAccount}`));
  assert.equal(durable[0].content, 'the original reply', 'the durable intent changed');

  const reused = await value.sendIntoThread(thread, 'an edited reply', key);
  assert.equal(reused.state, 'refused');
  assert.equal(
    value.unresolvedThreadOperation(thread).content,
    'the original reply',
    'the edit rewrote the operation it reused'
  );
  assert.equal(posts.length, 1, 'the edited words were submitted under the original key');
  assert.equal(
    JSON.parse(store.getItem(`brrp.outbox.${recoveryAccount}`))[0].content,
    'the original reply',
    'the stored intent changed'
  );

  // The original operation still retries with its own words and key.
  const retried = [];
  setHandler((path, method, body) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    retried.push({ path, body });
    return Promise.resolve(
      message('77777777-7777-4777-8777-777777777777', {
        idempotency_key: key,
        thread_id: thread,
        user_id: recoveryAccount,
        content: 'the original reply'
      })
    );
  });
  await value.retryMessage(value.pending[key]);
  assert.equal(retried.length, 1);
  assert.equal(retried[0].path, `/threads/${thread}/messages`);
  assert.equal(retried[0].body.idempotency_key, key);
  assert.equal(retried[0].body.content, 'the original reply');
});

// A successful send clears the saved draft it came from — and only that text. Newer
// words typed while the request was in flight are the author's current work.
test('a committed send clears its saved draft without erasing newer text', (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.user.id = recoveryAccount;
  value.saveDrafts = true;
  store.clear();
  store.setItem(`brrp.save-drafts.${recoveryAccount}`, 'true');
  const owner = value.composerOwner();
  const slot = `thread:${crypto.randomUUID()}`;
  value.composerDrafts.set(slot, { text: 'sent text', attachments: [] });
  value.saveDraft(slot, 'sent text');
  assert.ok(store.getItem(`brrp.drafts.${recoveryAccount}`));

  value.clearSentDraft(slot, 'sent text', owner);
  assert.equal(value.composerDrafts.get(slot), undefined, 'the in-memory draft stayed');
  assert.equal(value.drafts[slot], undefined);
  assert.deepEqual(
    JSON.parse(store.getItem(`brrp.drafts.${recoveryAccount}`) || '{}'),
    {},
    'the sent draft stayed saved and would be restored on reopen'
  );

  value.composerDrafts.set(slot, { text: 'newer text', attachments: [] });
  value.saveDraft(slot, 'newer text');
  value.clearSentDraft(slot, 'sent text', owner);
  assert.equal(value.drafts[slot], 'newer text', 'newer text typed during the send was erased');
  assert.equal(
    JSON.parse(store.getItem(`brrp.drafts.${recoveryAccount}`))[slot],
    'newer text',
    'newer text was erased from the saved drafts'
  );
});

// A success that completes after the account moved belongs to a view that no longer
// exists: it is reported as dropped so the composer never runs its success cleanup for
// a session it no longer owns. The durable entry is released rather than left retryable,
// because the operation did complete.
test('a success that lands after a switch is dropped and releases its record', async (t) => {
  const value = workspace();
  t.after(() => value.stop());
  value.state.user.id = recoveryAccount;
  value.saveDrafts = true;
  store.clear();
  const key = '30000000-0000-4000-8000-000000000007';
  const thread = crypto.randomUUID();
  const conversation = crypto.randomUUID();
  value.state.channels = [{ id: conversation, type: 'text' }];
  value.activeId = conversation;
  const post = deferred();
  setHandler((path, method) => {
    if (method === 'GET') return Promise.resolve({ conversation_id: conversation });
    return post.promise;
  });
  const sending = value.sendIntoThread(thread, 'completed late', key);
  await new Promise((resolve) => setImmediate(resolve));
  value.state.user.id = crypto.randomUUID();
  post.resolve(
    message('66666666-6666-4666-8666-666666666666', {
      idempotency_key: key,
      thread_id: thread,
      user_id: recoveryAccount,
      content: 'completed late'
    })
  );
  const outcome = await sending;
  assert.equal(outcome.state, 'dropped');
  assert.equal(
    value.outbox.length,
    0,
    'a completed operation was left retryable in the moved view'
  );
  assert.equal(
    store.getItem(`brrp.outbox.${value.state.user.id}`),
    null,
    "the first account's queue was written into the second account's scope"
  );
  // The operation's own device scope keeps the record until that account signs back in
  // and reconciles it — it completed, so the answer it then gets releases it.
  assert.ok(
    store.getItem(`brrp.outbox.${recoveryAccount}`),
    "the first account's own scope lost its unconfirmed record"
  );
});
