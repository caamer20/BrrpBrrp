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
  // A message holding only spaces is *not* empty. The send boundary refuses one, so
  // this shape only reaches the renderer after an edit emptied the text, and rendering
  // the spaces is closer to what the author sees than rendering nothing at all.
  assert.deepEqual(tokenizeMessage('   '), [{ kind: 'text', value: '   ' }]);
});

// A message's shape is content. Two lines are not one word, and a line that holds
// nothing but spaces is a blank line, not an absence.
//
// The tokenizer dropped every whitespace-only text token and then joined the text runs
// either side of a line break with no separator, so `first\nsecond` tokenized to a
// single text token reading `firstsecond`. A pasted log, a list, a poem, or a table —
// every one of which is why somebody types more than one line — rendered as one run-on
// line. It was invisible in the tests because the tests only ever checked token *kinds*
// and mentions, and a concatenation has the same kinds as the text it came from.

test('two lines stay two lines rather than concatenating', () => {
  const tokens = tokenizeMessage('first\nsecond');
  assert.deepEqual(
    tokens.map((t) => t.value),
    ['first\nsecond'],
    'a line break between two runs of text was dropped'
  );
});

test('a blank line between two paragraphs survives', () => {
  const tokens = tokenizeMessage('above\n\nbelow');
  assert.equal(tokens.map((t) => t.value).join(''), 'above\n\nbelow');
});

test('leading indentation and internal runs of spaces are preserved', () => {
  assert.equal(tokenizeMessage('    indented')[0].value, '    indented');
  assert.equal(tokenizeMessage('a  b')[0].value, 'a  b');
  // A mention between two runs of text must not swallow the spacing around it.
  const tokens = tokenizeMessage('hi  @alice  there');
  assert.deepEqual(
    tokens.map((t) => [t.kind, t.value]),
    [
      ['text', 'hi  '],
      ['mention', '@alice'],
      ['text', '  there']
    ]
  );
});

test('a message holding only spaces is one space, not nothing', () => {
  // An all-blank message is refused at the send boundary, so this shape only arises
  // from a message that had content and lost it to tokenizing. Rendering nothing is the
  // better failure than rendering the text it was carrying.
  const tokens = tokenizeMessage('   ');
  assert.equal(tokens.map((t) => t.value).join(''), '   ');
});

test('code keeps its own whitespace without gaining the surrounding message line breaks', () => {
  const tokens = tokenizeMessage('before\n```\n  indented\n\n  blank above\n```\nafter');
  const code = tokens.find((t) => t.kind === 'code');
  assert.equal(code.value, '  indented\n\n  blank above');
  // The prose around the block keeps its own break. The fence line itself is consumed
  // as the fence, so it does not contribute a break of its own -- but the line after the
  // closing fence does, and the merge must not fold it into the block.
  assert.equal(
    tokens
      .filter((t) => t.kind === 'text')
      .map((t) => t.value)
      .join(''),
    'before\nafter'
  );
});
