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
const shellModules = [outboxSource, draftsSource]
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
