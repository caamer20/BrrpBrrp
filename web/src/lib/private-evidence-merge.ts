/**
 * Merging the two private-evidence queues without refetching an exhausted one.
 *
 * Message evidence and request evidence are separate tables paginated on their own
 * ids, so each carries its own cursor. They were merged through a single combined
 * cursor, which meant that when one source ran out and the other still had rows,
 * "load older" asked the exhausted source for its first page again: its cursor was
 * empty, so the request carried no `before` and the server answered with the newest
 * page, duplicating rows the reviewer had already seen (completion review 12,
 * R12-08).
 *
 * Identity is `(source_kind, id)` rather than `id` alone, so a collision between
 * two sources cannot merge two different records into one.
 */

export interface EvidenceRow {
  id: string;
  source_kind?: string;
}

export interface SourcePage<T> {
  reports: T[];
  next_cursor: string;
}

/** The two independent cursors, and which sources still have more. */
export interface MergedPage {
  messageCursor: string;
  requestCursor: string;
  /** True when the server reported no further cursor for that source. */
  messageExhausted: boolean;
  requestExhausted: boolean;
}

export interface MergedResult<T extends EvidenceRow> extends MergedPage {
  rows: T[];
  /** True when neither source can produce anything further. */
  done: boolean;
}

/**
 * Whether a source is finished.
 *
 * An empty cursor is the server saying it has no more rows, which is a fact about
 * the data rather than a guess. Treating it as "no cursor yet" is what caused the
 * exhausted source to be refetched from the start.
 */
export function exhausted(nextCursor: string | undefined | null): boolean {
  return !nextCursor;
}

/**
 * Identity of a row within the merged list.
 *
 * A record is identified by its source as well as its id, so two sources that
 * happen to share an id stay two rows.
 */
export function rowKey(row: EvidenceRow): string {
  return `${row.source_kind ?? 'message'}:${row.id}`;
}

/**
 * Merge a page from each source, newest first, without duplicates.
 *
 * Ordering is by id descending because both sources paginate on descending ids and
 * the ids are monotonic per source; the comparison is on the id alone so a
 * cross-source comparison never depends on a field one source lacks.
 */
export function mergeEvidence<T extends EvidenceRow>(
  previous: readonly T[],
  messages: SourcePage<T>,
  requests: SourcePage<T>,
  cursors: { message: string; request: string } = { message: '', request: '' }
): MergedResult<T> {
  const previousMessageCursor = cursors.message;
  const previousRequestCursor = cursors.request;
  const seen = new Set<string>();
  const rows: T[] = [];
  for (const row of [...messages.reports, ...requests.reports, ...previous]) {
    const key = rowKey(row);
    if (seen.has(key)) continue;
    seen.add(key);
    rows.push(row);
  }
  rows.sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0));
  // The previous cursor is preserved when a source reports nothing. An exhausted
  // source is not asked again, so its last real cursor is the only record of how
  // far it had got; replacing it with the empty cursor of "nothing to give you"
  // would rewind that source to its first page on the next request, which is the
  // duplication this whole shape exists to prevent.
  const messageExhausted = exhausted(messages.next_cursor);
  const requestExhausted = exhausted(requests.next_cursor);
  return {
    rows,
    messageCursor: messages.next_cursor || previousMessageCursor,
    requestCursor: requests.next_cursor || previousRequestCursor,
    messageExhausted,
    requestExhausted,
    done: messageExhausted && requestExhausted
  };
}

/**
 * The page a source contributes when it is exhausted and therefore not asked again.
 *
 * It is empty, and its `next_cursor` is empty too: that is what marks the source
 * finished. Handing back its last real cursor would read as "this source still has
 * rows" on the next merge, so the queue would never finish and the source would be
 * fetched again from a cursor that points at the page it already delivered.
 */
export function skippedPage<T>(): SourcePage<T> {
  return { reports: [], next_cursor: '' };
}

/**
 * The `before` parameter to send for a source on the next "load older".
 *
 * An exhausted source is not asked again: the request is dropped rather than sent
 * with an empty cursor, which is what would return its first page a second time.
 */
export function beforeParam(cursor: string, isExhausted: boolean): string {
  if (isExhausted) return '';
  return cursor ? `&before=${encodeURIComponent(cursor)}` : '';
}
