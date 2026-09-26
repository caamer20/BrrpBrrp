import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// The store the workspace writes through, compiled from its own source: the bounds and the
// account scoping are claims about that file, not about a copy of it.
const source = await readFile(new URL('../src/lib/outbox.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
}).outputText;
const { readOutbox, writeOutbox, clearOutbox, retryableStatus, outcomeExplanation, OutboxLimit } =
  await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));

const account = '11111111-1111-4111-8111-111111111111';
const other = '22222222-2222-4222-8222-222222222222';
const conversation = '33333333-3333-4333-8333-333333333333';

function storage() {
  const memory = new Map();
  return {
    getItem: (key) => (memory.has(key) ? memory.get(key) : null),
    setItem: (key, value) => memory.set(key, String(value)),
    removeItem: (key) => memory.delete(key),
    keys: () => [...memory.keys()]
  };
}

function entry(extra = {}) {
  return {
    key: crypto.randomUUID(),
    conversation,
    content: 'held text',
    attachments: [],
    reply_to: '',
    created: new Date().toISOString(),
    ...extra
  };
}

test('the outbox is scoped to the account that wrote it', () => {
  const store = storage();
  writeOutbox(store, account, [entry()]);
  assert.equal(readOutbox(store, account).length, 1);
  assert.deepEqual(readOutbox(store, other), [], 'another account reads nothing');
  assert.equal(store.keys().length, 1, 'one key per account, not one per entry');
  clearOutbox(store, account);
  assert.deepEqual(readOutbox(store, account), []);
  assert.deepEqual(store.keys(), [], 'sign-out clears the whole scope');
});

test('the outbox is bounded in entries, text and age', () => {
  const store = storage();
  const now = Date.now();
  const entries = Array.from({ length: OutboxLimit + 10 }, (_, index) =>
    entry({ created: new Date(now - (OutboxLimit + 10 - index) * 1000).toISOString() })
  );
  writeOutbox(store, account, entries, now);
  const saved = readOutbox(store, account, now);
  assert.equal(saved.length, OutboxLimit, 'the oldest entries beyond the bound are dropped');
  assert.equal(saved[saved.length - 1].key, entries[entries.length - 1].key, 'newest kept');

  writeOutbox(store, account, [entry({ content: 'x'.repeat(80 * 1024) })], now);
  assert.deepEqual(
    readOutbox(store, account, now),
    [],
    'oversized intent is never truncated and retried'
  );

  writeOutbox(
    store,
    account,
    Array.from({ length: 20 }, () => entry({ content: '😀'.repeat(2000) })),
    now
  );
  const bounded = readOutbox(store, account, now);
  assert.ok(bounded.length < 20);
  assert.ok(bounded.reduce((sum, item) => sum + Buffer.byteLength(item.content), 0) <= 64 * 1024);
  assert.ok(
    bounded.every((item) => item.content === '😀'.repeat(2000)),
    'whole intent remains exact'
  );

  writeOutbox(
    store,
    account,
    [entry({ created: new Date(now - 25 * 60 * 60 * 1000).toISOString() })],
    now
  );
  const expired = readOutbox(store, account, now)[0];
  assert.match(expired.recovery, /too old/);
  assert.deepEqual(expired.attachments, []);
  assert.equal(expired.content, 'held text');
  assert.deepEqual(
    readOutbox(store, account, now + 25 * 60 * 60 * 1000),
    [],
    'recovered text expires one day after it is first offered'
  );
});

test('an unusable entry is never restored', () => {
  const store = storage();
  store.setItem(
    `brrp.outbox.${account}`,
    JSON.stringify([
      entry({ content: '', attachments: [] }),
      entry({ attachments: ['not an id'] }),
      'not an entry',
      { key: crypto.randomUUID() }
    ])
  );
  assert.deepEqual(readOutbox(store, account), [], 'a shape the client cannot act on is dropped');
  store.setItem(`brrp.outbox.${account}`, '{ not json');
  assert.deepEqual(readOutbox(store, account), [], 'a corrupt scope reads as empty');
  // A storage that refuses writes must not break sending either.
  writeOutbox(
    {
      getItem: () => null,
      setItem: () => {
        throw new Error('quota');
      },
      removeItem: () => {}
    },
    account,
    [entry()]
  );
});

test('an attachment-only send is a valid entry and an answer decides whether it is retried', () => {
  const store = storage();
  writeOutbox(store, account, [entry({ content: '', attachments: [conversation] })]);
  assert.equal(readOutbox(store, account).length, 1);

  for (const status of [0, 408, 425, 429, 500, 503]) assert.ok(retryableStatus(status), status);
  for (const status of [400, 403, 404, 409, 413, 415, 422])
    assert.equal(retryableStatus(status), false, status);
  assert.match(outcomeExplanation(403), /no longer have access/);
  assert.match(outcomeExplanation(409, 'That send has expired.'), /^That send has expired\./);
  assert.equal(outcomeExplanation(409, 'That send has expired.'), 'That send has expired.');
});

test('malformed neighboring entries cannot hide a valid send, and future dates never retry', () => {
  const store = storage();
  const valid = entry();
  store.setItem(
    `brrp.outbox.${account}`,
    JSON.stringify([{ ...valid, content: '', attachments: undefined }, valid])
  );
  assert.deepEqual(
    readOutbox(store, account).map((item) => item.key),
    [valid.key]
  );
  writeOutbox(store, account, [entry({ created: new Date(Date.now() + 60000).toISOString() })]);
  assert.match(readOutbox(store, account)[0].recovery, /too old/);
});
