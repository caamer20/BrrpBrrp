import test from 'node:test';
import assert from 'node:assert/strict';
import { saveUpdateDrafts, consumeUpdateDrafts } from '../src/lib/reload-drafts.ts';
const channel = '00000000-0000-7000-8000-000000000001';
const attachment = {
  id: '00000000-0000-7000-8000-000000000002',
  file_name: 'photo.jpg',
  file_size: 3200,
  mime_type: 'image/jpeg'
};
const store = () => {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key)
  };
};

test('explicit app refresh preserves text, attachments, and reply once for the same user', () => {
  const memory = store();
  saveUpdateDrafts(
    memory,
    'me',
    { [channel]: { text: 'Draft with a photo', attachments: [attachment] } },
    attachment.id,
    100
  );
  const saved = consumeUpdateDrafts(memory, 'me', 101);
  assert.equal(saved.drafts[channel].text, 'Draft with a photo');
  assert.deepEqual(saved.drafts[channel].attachments, [attachment]);
  assert.equal(saved.reply, attachment.id);
  assert.equal(consumeUpdateDrafts(memory, 'me', 102), null);
});

test('update drafts never cross accounts or restore after expiration', () => {
  const memory = store();
  saveUpdateDrafts(
    memory,
    'me',
    { [channel]: { text: 'private', attachments: [] } },
    undefined,
    100
  );
  assert.equal(consumeUpdateDrafts(memory, 'someone-else', 101), null);
  saveUpdateDrafts(
    memory,
    'me',
    { [channel]: { text: 'private', attachments: [] } },
    undefined,
    100
  );
  assert.equal(consumeUpdateDrafts(memory, 'me', 86400200), null);
});

test('channel and thread drafts survive an update without overwriting one another', () => {
  const memory = store();
  const drafts = {
    [channel]: { text: 'channel draft', attachments: [] },
    [`thread:${channel}`]: { text: 'thread draft', attachments: [attachment] }
  };
  saveUpdateDrafts(memory, 'me', drafts, undefined, 100);
  assert.deepEqual(consumeUpdateDrafts(memory, 'me', 101).drafts, drafts);
});

test('corrupt or oversized update drafts are rejected or bounded', () => {
  const memory = store();
  memory.setItem('brrp.update-drafts', 'broken');
  assert.equal(consumeUpdateDrafts(memory, 'me', 100), null);
  memory.setItem(
    'brrp.update-drafts',
    JSON.stringify({
      user: 'me',
      expires: 200,
      drafts: {
        [channel]: {
          text: 'a'.repeat(5000),
          attachments: [{ ...attachment, id: '../../private' }, attachment]
        }
      }
    })
  );
  const saved = consumeUpdateDrafts(memory, 'me', 100);
  assert.equal(saved.drafts[channel].text.length, 4000);
  assert.deepEqual(saved.drafts[channel].attachments, [attachment]);
});

test('storage failure prevents a claimed successful handoff', () => {
  assert.throws(
    () =>
      saveUpdateDrafts(
        {
          setItem() {
            throw new Error('quota');
          },
          removeItem() {}
        },
        'me',
        { [channel]: { text: 'keep me', attachments: [] } },
        undefined
      ),
    /quota/
  );
});
