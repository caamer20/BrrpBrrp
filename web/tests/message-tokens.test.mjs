import test from 'node:test';
import assert from 'node:assert/strict';
import { tokenizeMessage, linkHost } from '../src/lib/message-tokens.ts';

// A message is written by anybody who can post, and every piece of it is rendered as
// text — nothing becomes markup. What the tokenizer decides is which pieces are *live*:
// a mention notifies somebody, and a link is somewhere a reader will click. Getting
// either wrong inside a code block is how a pasted snippet pings the wrong person.

const kinds = (text) => tokenizeMessage(text).map((token) => token.kind);
const value = (text, kind) =>
  tokenizeMessage(text)
    .filter((token) => token.kind === kind)
    .map((token) => token.value);

test('a mention in ordinary text is a mention', () => {
  assert.deepEqual(kinds('hey @alice look'), ['text', 'mention', 'text']);
  assert.deepEqual(value('hey @alice look', 'mention'), ['@alice']);
});

test('a mention inside a code block is code, not a mention', () => {
  // The whole point: pasting a snippet that mentions somebody must not notify them.
  const tokens = tokenizeMessage('```\npip install @acme/thing\n```');
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ['code']
  );
  assert.match(tokens[0].value, /@acme\/thing/);
});

test('a URL inside a code block is code, not a link', () => {
  const tokens = tokenizeMessage('```\nhttps://example.com/hooks\n```');
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ['code']
  );
});

test('a fenced block with a language tag is still code', () => {
  const tokens = tokenizeMessage('```ts\nconst url = "https://example.com";\n```');
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ['code']
  );
  assert.match(tokens[0].value, /const url/);
});

test('an unterminated fence runs to the end of the message', () => {
  // A half-pasted snippet should still read as code rather than rendering its
  // mentions and links live.
  const tokens = tokenizeMessage('```\nsee @alice and https://example.com');
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ['code']
  );
});

test('text after a code block is ordinary text again', () => {
  const tokens = tokenizeMessage('```\ncode\n```\nthen @alice');
  assert.deepEqual(
    tokens.map((t) => t.kind),
    ['code', 'text', 'mention']
  );
});

test('only http and https become links', () => {
  assert.equal(kinds('go to https://example.com').includes('link'), true);
  assert.equal(kinds('go to http://example.com').includes('link'), true);
  // A scheme nobody should be clicking from a message body.
  for (const hostile of [
    'javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'file:///etc/passwd',
    'vbscript:msgbox'
  ]) {
    assert.equal(kinds(`see ${hostile}`).includes('link'), false, hostile);
  }
});

test('trailing punctuation is not part of the address', () => {
  // A link to a host with a full stop on the end goes somewhere unintended.
  assert.deepEqual(value('see https://example.com/page.', 'link'), ['https://example.com/page']);
  assert.equal(kinds('see https://example.com/page.').filter((k) => k === 'text').length, 2);
});

test('a mention must be a whole token', () => {
  // Otherwise an email address would notify the person before the @.
  assert.equal(kinds('write to alice@example.com').includes('mention'), false);
  assert.equal(kinds('write to @alice here').includes('mention'), true);
});

test('a one-character name is not a mention', () => {
  // Single letters are far more often a stray @ than a person.
  assert.equal(kinds('cost is 5@6').includes('mention'), false);
  assert.equal(kinds('hi @al').includes('mention'), true);
});

test('markup in a message is text, never interpreted', () => {
  const tokens = tokenizeMessage('<img src=x onerror=alert(1)> and <b>bold</b>');
  assert.ok(!tokens.some((token) => token.kind !== 'text'));
  assert.match(tokens.map((t) => t.value).join(''), /<img src=x onerror=alert\(1\)>/);
});

test('a link host is readable and never throws on a malformed address', () => {
  assert.equal(linkHost('https://example.com/a/b'), 'example.com');
  assert.equal(linkHost('not a url'), 'not a url');
});

test('empty input produces no tokens rather than one empty piece', () => {
  assert.deepEqual(tokenizeMessage(''), []);
  assert.deepEqual(tokenizeMessage('   '), []);
});
