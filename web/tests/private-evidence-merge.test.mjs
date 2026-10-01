import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  beforeParam,
  exhausted,
  mergeEvidence,
  rowKey,
  skippedPage
} from '../src/lib/private-evidence-merge.ts';

// The reviewer queue merges two separately paginated tables. They were merged
// through one combined cursor, so when one source ran out and the other still had
// rows, "load older" asked the exhausted source for its first page again — its
// cursor was empty, so the request carried no `before` and the server answered with
// the newest page, duplicating rows the reviewer had already read (completion
// review 12, R12-08).

const ids = (source, count, from = 0) =>
  Array.from({ length: count }, (_, i) => ({
    id: `${source}-${String(from + i).padStart(4, '0')}`,
    source_kind: source
  }));

const page = (rows, next = '') => ({ reports: rows, next_cursor: next });

/**
 * The server side: return the first page whose rows are all after the cursor, and
 * an empty cursor once the source has nothing further. The component cannot tell
 * these apart, which is exactly why exhaustion has to be carried per source.
 */
function serve(pages, cursor) {
  if (!cursor) return pages[0];
  const index = pages.findIndex((candidate) => candidate.reports.at(-1)?.id === cursor);
  if (index < 0) return page([]);
  const next = pages[index + 1];
  if (!next) return page([]);
  // `before` is exclusive of the cursor, so the row the cursor names is not
  // delivered a second time.
  return next.reports[0]?.id === cursor ? page([]) : next;
}

test('an empty cursor means the source is finished, not unfetched', () => {
  assert.equal(exhausted(''), true);
  assert.equal(exhausted(undefined), true);
  assert.equal(exhausted(null), true);
  assert.equal(exhausted('abc'), false);
});

test('a finished source is not asked again', () => {
  // This is the defect: an empty cursor used to be sent as no `before` at all,
  // which asks for the first page and duplicates what the reviewer already has.
  assert.equal(beforeParam('', true), '');
  assert.equal(beforeParam('abc', true), '');
  assert.equal(beforeParam('', false), '');
  assert.equal(beforeParam('abc', false), '&before=abc');
  // A cursor is url-encoded rather than pasted into a query raw.
  assert.equal(beforeParam('a&b=c', false), '&before=a%26b%3Dc');
});

test('rows are identified by source as well as id', () => {
  // Two tables can hold the same id; merging them would lose a record.
  assert.equal(rowKey({ id: 'x', source_kind: 'message' }), 'message:x');
  assert.equal(rowKey({ id: 'x', source_kind: 'request' }), 'request:x');
  assert.notEqual(
    rowKey({ id: 'x', source_kind: 'message' }),
    rowKey({ id: 'x', source_kind: 'request' })
  );
});

test('an exhausted source and a full one both stay reachable', () => {
  // Message evidence has run out; request evidence has 3 more. The list is not
  // finished, and the message source is not refetched.
  const first = mergeEvidence([], page(ids('message', 2), ''), page(ids('request', 2), 'req-2'));
  assert.equal(first.messageExhausted, true);
  assert.equal(first.requestExhausted, false);
  assert.equal(first.done, false, 'one finished source must not end the list');

  const second = mergeEvidence(first.rows, page([], ''), page(ids('request', 3, 2), 'req-5'));
  assert.equal(second.rows.length, 7);
  assert.equal(new Set(second.rows.map(rowKey)).size, 7, 'the refetched page duplicated rows');
  assert.equal(second.requestExhausted, false);
  // Nothing from the exhausted source reappeared.
  assert.equal(
    second.rows.filter((r) => r.source_kind === 'message').length,
    2,
    'an exhausted source was refetched from the start'
  );
});

test('the list is finished only when both sources are', () => {
  const both = mergeEvidence([], page(ids('message', 1), ''), page(ids('request', 1), ''));
  assert.equal(both.messageExhausted, true);
  assert.equal(both.requestExhausted, true);
  assert.equal(both.done, true);

  const one = mergeEvidence([], page(ids('message', 1), 'm-1'), page(ids('request', 1), ''));
  assert.equal(one.done, false);
});

test('rows from both sources are merged newest first without duplicates', () => {
  const merged = mergeEvidence(
    [],
    page(
      [
        { id: 'm-02', source_kind: 'message' },
        { id: 'm-01', source_kind: 'message' }
      ],
      'm'
    ),
    page(
      [
        { id: 'r-03', source_kind: 'request' },
        { id: 'm-02', source_kind: 'message' }
      ],
      'r'
    )
  );
  assert.deepEqual(
    merged.rows.map((r) => r.id),
    ['r-03', 'm-02', 'm-01']
  );
});

test('the component\u2019s own walk finishes a large uneven queue without refetching an exhausted source', () => {
  // The reproduced defect: 120 message reports and 70 request reports all arrived,
  // but the walk never finished. An exhausted source was skipped by handing back its
  // last cursor, which the merge reads as "this source still has rows" — so `done`
  // stayed false and the source was fetched again from the page it had already
  // delivered. This walks exactly the shape the component builds.
  const messagePages = [
    page(ids('message', 50, 0), 'message-0049'),
    page(ids('message', 50, 50), 'message-0099'),
    page(ids('message', 20, 100), '')
  ];
  const requestPages = [
    page(ids('request', 50, 0), 'request-0049'),
    page(ids('request', 20, 50), '')
  ];
  let previous = [];
  let messageCursor = '';
  let requestCursor = '';
  let messageExhausted = false;
  let requestExhausted = false;
  let done = false;
  let older = false;
  let rounds = 0;
  let messageFetches = 0;
  let requestFetches = 0;
  while (!done) {
    rounds += 1;
    assert.ok(rounds <= 10, 'the walk did not terminate');
    const messages =
      older && messageExhausted
        ? skippedPage()
        : ((messageFetches += 1), serve(messagePages, older ? messageCursor : ''));
    const requests =
      older && requestExhausted
        ? skippedPage()
        : ((requestFetches += 1), serve(requestPages, older ? requestCursor : ''));
    const result = mergeEvidence(older ? previous : [], messages, requests, {
      message: messageCursor,
      request: requestCursor
    });
    previous = result.rows;
    messageCursor = result.messageCursor;
    requestCursor = result.requestCursor;
    messageExhausted = result.messageExhausted;
    requestExhausted = result.requestExhausted;
    done = result.done;
    older = true;
  }
  assert.equal(previous.length, 120 + 70, 'not every row was reachable');
  assert.equal(new Set(previous.map(rowKey)).size, 120 + 70, 'a row was delivered twice');
  assert.equal(messageFetches, 3, 'the exhausted message source was fetched again');
  assert.equal(requestFetches, 2, 'the exhausted request source was fetched again');
});

test('a skipped exhaustion still reports the source finished', () => {
  // The shape the component contributes for a source it does not ask again: empty,
  // with an empty cursor. The preserved cursor stays in the merge, not in the page.
  const skipped = skippedPage();
  const result = mergeEvidence(
    [{ id: 'message-0001', source_kind: 'message' }],
    skipped,
    page(ids('request', 1, 0), ''),
    { message: 'message-0001', request: '' }
  );
  assert.equal(result.messageExhausted, true, 'a skipped source looked active again');
  assert.equal(result.messageCursor, 'message-0001', 'the last real cursor was lost');
  assert.equal(result.done, true, 'the queue never finished with both sources exhausted');
});

test('a large uneven population is fully reachable exactly once', () => {
  // 51 message rows over two pages and 101 request rows over three, walked to
  // exhaustion: every row appears once, with no repeats and none lost.
  const seen = new Set();
  let previous = [];
  // Explicit pages: 51 message rows in 26+25, 101 request rows in 34+34+33. Each
  // page's cursor is the last id of the page it ends, and the final page of each
  // source reports none, which is how a source signals it is finished.
  const messagePages = [
    page(ids('message', 26, 0), 'message-0025'),
    page(ids('message', 25, 26), '')
  ];
  const requestPages = [
    page(ids('request', 34, 0), 'request-0033'),
    page(ids('request', 34, 34), 'request-0067'),
    page(ids('request', 33, 68), '')
  ];
  // The walk mirrors what the component does: each "load older" asks both sources
  // for the page after that source's own cursor, and a finished source is asked for
  // nothing and contributes nothing.
  let messageCursor = '';
  let requestCursor = '';
  let messageExhausted = false;
  let requestExhausted = false;
  let pages = 0;
  while (!(messageExhausted && requestExhausted)) {
    pages += 1;
    assert.ok(pages <= 10, 'the walk did not terminate');
    const messages = messageExhausted ? page([]) : serve(messagePages, messageCursor);
    const requests = requestExhausted ? page([]) : serve(requestPages, requestCursor);
    const result = mergeEvidence(previous, messages, requests, {
      message: messageCursor,
      request: requestCursor
    });
    messageCursor = result.messageCursor;
    requestCursor = result.requestCursor;
    messageExhausted = result.messageExhausted;
    requestExhausted = result.requestExhausted;
    // The whole merged list must be free of duplicates: that is the property the
    // reviewer sees, and it is what the combined cursor broke.
    const withinPass = new Set();
    for (const row of result.rows) {
      const key = rowKey(row);
      assert.equal(withinPass.has(key), false, `${key} appeared twice in one merged list`);
      withinPass.add(key);
      seen.add(key);
    }
    assert.equal(result.rows.length, withinPass.size, 'the merged list shrank by deduplication');
    previous = result.rows;
  }
  assert.equal(seen.size, 51 + 101, 'not every row was reachable');
  assert.equal(messageExhausted, true);
  assert.equal(requestExhausted, true);
});

// The identity of a row is its source and its id, everywhere the component keeps
// per-row state. Two tables can hand out the same id; keying notes or busy state by
// id alone made one record's note and one record's review button belong to the other.
test('the component keys rows, notes and busy state by source and id together', () => {
  const source = readFileSync(
    new URL('../src/lib/components/admin/PrivateEvidence.svelte', import.meta.url),
    'utf8'
  );
  assert.match(source, /\{#each reports as report \(rowKey\(report\)\)\}/);
  assert.match(source, /notes\[rowKey\(report\)\] \?\?= report\.review_note/);
  assert.match(source, /bind:value=\{notes\[rowKey\(report\)\]\}/);
  assert.match(source, /const claimed = rowKey\(report\)/);
  assert.match(source, /busy === rowKey\(report\)/);
  // And an exhausted source contributes the finished page, never its preserved
  // cursor, which would read as "still has rows".
  assert.match(source, /Promise\.resolve\(skippedPage<Evidence>\(\)\)/);
  assert.doesNotMatch(source, /next_cursor: messageCursor/);
  assert.doesNotMatch(source, /next_cursor: requestCursor/);
});
