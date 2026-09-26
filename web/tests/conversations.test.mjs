import test from 'node:test';
import assert from 'node:assert/strict';
import { canGroup, firstUnread } from '../src/lib/conversations.ts';
import { readDrafts, writeDrafts } from '../src/lib/drafts.ts';

test('grouping respects replies, pins, author, time and date boundaries', () => {
  const previous = { id: 'a', user_id: 'alice', created_at: '2026-09-04T12:00:00Z' };
  const next = { id: 'b', user_id: 'alice', created_at: '2026-09-04T12:01:00Z' };
  assert.equal(canGroup(next, previous), true);
  for (const change of [
    { user_id: 'bob' },
    { reply_to_id: 'a' },
    { pinned: true },
    { created_at: '2026-09-04T12:05:00Z' },
    { created_at: '2026-09-05T12:01:00Z' }
  ])
    assert.equal(canGroup({ ...next, ...change }, previous), false);
});

test('unread divider ignores local pending and own messages', () => {
  const messages = [
    { id: 'b', user_id: 'me' },
    { id: 'c', user_id: 'other', pending: 'sending' },
    { id: 'd', user_id: 'other' }
  ];
  assert.equal(firstUnread(messages, 'a', 'me'), 'd');
  assert.equal(firstUnread(messages, null, 'me'), '');
  assert.equal(firstUnread(messages, 'z', 'me'), '');
});

test('drafts are account-scoped, bounded, and tolerate unavailable storage', () => {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key),
    setItem: (key, value) => values.set(key, value)
  };
  const channel = '00000000-0000-7000-8000-000000000001';
  writeDrafts(storage, 'alice', { [channel]: 'hello', [`thread:${channel}`]: 'reply draft' });
  assert.deepEqual(readDrafts(storage, 'alice'), {
    [channel]: 'hello',
    [`thread:${channel}`]: 'reply draft'
  });
  assert.deepEqual(readDrafts(storage, 'bob'), {});
  values.set('brrp.drafts.alice', JSON.stringify({ [channel]: 'x'.repeat(5000), bad: 3 }));
  assert.equal(readDrafts(storage, 'alice')[channel].length, 4000);
  values.set('brrp.drafts.alice', 'invalid');
  assert.deepEqual(readDrafts(storage, 'alice'), {});
  assert.doesNotThrow(() =>
    writeDrafts(
      {
        setItem() {
          throw new Error('full');
        }
      },
      'alice',
      { [channel]: 'hello' }
    )
  );
});
