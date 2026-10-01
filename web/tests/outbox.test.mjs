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
const {
  readOutbox,
  writeOutbox,
  clearOutbox,
  retryableStatus,
  outcomeExplanation,
  isOperationKey,
  OutboxLimit,
  OutboxAgeLimit,
  boundOutbox
} = await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));

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
    thread_id: '',
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

test('malformed neighboring entries cannot hide a valid send', () => {
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
});

// A timestamp is a trust boundary: the retry an operation is still owed depends on it.
// A small skew means the clock moved, and the receipt can still answer; an arbitrarily
// future stamp cannot promise a receipt at all, so it is never retried — and it never
// extends how long the author's text is kept either.
test('a future stamp is retried only inside a bounded allowance', () => {
  const store = storage();
  const now = Date.now();
  writeOutbox(
    store,
    account,
    [entry({ created: new Date(now + 60 * 1000).toISOString(), content: 'clock-skew text' })],
    now
  );
  const held = readOutbox(store, account, now);
  assert.equal(held.length, 1, 'a slightly future stamp was discarded instead of retried');
  assert.equal(held[0].content, 'clock-skew text');
  assert.equal(held[0].recovery, undefined, 'a slightly future stamp was treated as expired');

  // Far enough ahead, the stamp cannot be checked against the receipt window, so the
  // operation is handed back as recovery rather than retried.
  writeOutbox(
    store,
    account,
    [entry({ created: new Date(now + 60 * 60 * 1000).toISOString(), content: 'far-future text' })],
    now
  );
  const recovered = readOutbox(store, account, now);
  assert.equal(recovered.length, 1, 'the far-future text was dropped instead of recovered');
  assert.equal(recovered[0].content, 'far-future text', 'recovery keeps the complete text');
  assert.deepEqual(recovered[0].attachments, []);
  assert.match(recovered[0].recovery, /clock/);
  // And recovery itself is bounded: offering the text does not retain it forever.
  assert.deepEqual(
    readOutbox(store, account, now + 25 * 60 * 60 * 1000),
    [],
    'a far-future stamp kept text past the recovery window'
  );
});

// The key is the identity the server reconciles against, so it is validated before it
// can be adopted into a composer — a malformed stored key must not become the key a
// future send is submitted under.
test('an operation key is validated before it can be adopted', () => {
  assert.equal(isOperationKey(crypto.randomUUID()), true);
  for (const bad of [
    '',
    'key-1',
    'me',
    '11111111-1111-4111-8111-11111111111',
    'zzzzzzzz-zzzz-4zzz-8zzz-zzzzzzzzzzzz'
  ])
    assert.equal(isOperationKey(bad), false, bad);
});

// A future stamp the clock has caught up with is still too old once it is old. The
// allowance changes only the meaning of a small negative age, not the expiry bound.
test('a restored send whose stamp is newer than the clock still reconciles', () => {
  const store = storage();
  const now = Date.now();
  const saved = boundOutbox(
    [
      entry({
        created: new Date(now + 60 * 1000).toISOString(),
        content: 'clock-skew text'
      })
    ],
    now
  );
  assert.equal(saved.length, 1, 'the uncertain send was discarded');
  assert.equal(saved[0].content, 'clock-skew text');
  assert.equal(saved[0].recovery, undefined);
});

// The destination an operation was submitted to is part of the operation. A thread is
// a destination a channel is not, so it is kept whole — and an entry from before
// threads were recorded has no trustworthy destination at all and is never retried
// into a guess (review 15, V15-01).
test('a thread destination is kept, and an unrecorded one is never guessed', () => {
  const store = storage();
  const thread = crypto.randomUUID();
  writeOutbox(store, account, [entry({ thread_id: thread })]);
  assert.equal(readOutbox(store, account)[0].thread_id, thread, 'the thread is kept whole');

  writeOutbox(store, account, [entry({ thread_id: '' })]);
  assert.equal(readOutbox(store, account)[0].thread_id, '', 'an ordinary destination is explicit');

  // An entry written before the field existed cannot say whether it was going to a
  // thread or to the conversation, so its text is held for the author instead of
  // being sent somewhere the client would be guessing about.
  writeOutbox(store, account, [{ ...entry(), thread_id: undefined }]);
  const legacy = readOutbox(store, account);
  assert.equal(legacy.length, 1, 'the text is not dropped');
  assert.match(legacy[0].recovery, /where it was going/);
  assert.equal(legacy[0].content, 'held text', 'and it is kept complete');
  assert.deepEqual(legacy[0].attachments, []);

  // A destination that is not an id is not a destination.
  writeOutbox(store, account, [entry({ thread_id: 'not-a-thread-id' })]);
  assert.deepEqual(readOutbox(store, account), []);
});
