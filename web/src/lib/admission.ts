// The lifetime gate shared by the activity controller and its tests.
//
// The activity controller is a module-level singleton, and every fetch and every
// response check goes through one admission question: is what I am about to do — or
// what just answered — still wanted? The question has four parts, and keeping it here
// rather than inline is what makes it testable.
//
// The fourth part is `alive`, and it is per-*mount* rather than permanent. That is the
// whole point of this module existing. The controller is a singleton, so closing the
// Activity panel and opening it again mounts a new panel over the same object. Teardown
// sets `alive` false to discard a response that arrives after the panel is gone — a
// statement about work in flight. When the controller was dead for good, the second open
// in a session admitted nothing, fetched nothing, and rendered an empty panel with no
// error and no way to retry, which is indistinguishable from a server that returned
// nothing.
//
// So: teardown invalidates what was in flight, mount says there is a listener again, and
// neither touches the account or generation fences. Those are what keep one account's
// private metadata out of the next account's panel, and they must survive any number of
// mounts.

/** What one request was started under, and what the controller was asked at the time. */
export interface AdmissionRequest {
  /** The signed-in account. */
  account: string;
  /** The account's authority generation, advanced by any authority change. */
  generation: number;
  /** The controller's own version, advanced by every reset and teardown. */
  version: number;
}

/** What the controller currently believes, read at the moment of the question. */
export interface AdmissionNow {
  account: string;
  generation: number;
  version: number;
  alive: boolean;
  authorizationUnverified: boolean;
}

/**
 * Whether work started under `request` is still wanted.
 *
 * `now` is passed in rather than read from a singleton so that a caller cannot forget
 * to refresh it, and so a test can state the situation rather than arrange it.
 */
export function admitted(request: AdmissionRequest, now: AdmissionNow): boolean {
  // Strictly boolean. `now.alive && ...` returns whatever the first falsy operand was,
  // so a caller writing `if (!admitted(...))` and a caller writing `admitted(...) ===
  // false` would disagree about a missing field — and a caller that logs the result
  // would print `undefined` for a state the type says cannot happen. The conditions are
  // written as comparisons so the answer is a decision, not a value that happened to
  // propagate.
  if (now.alive !== true) return false;
  if (request.account !== now.account) return false;
  if (request.generation !== now.generation) return false;
  if (request.version !== now.version) return false;
  if (now.authorizationUnverified === true) return false;
  return true;
}

/**
 * How many rows the server said it acknowledged.
 *
 * A server that reports a count is believed. One that does not is answered for
 * generously: marking the whole page is the behaviour that predates the count, and it is
 * the right guess when the server acknowledged anything inside this page. Guessing low
 * would leave rows the server did mark looking unseen, which is the mistake that shows a
 * person a "N unseen" badge for rows they just acknowledged.
 */
export function acknowledgedRows(acknowledged: unknown): number {
  return typeof acknowledged === 'number' && Number.isInteger(acknowledged) && acknowledged >= 0
    ? acknowledged
    : Number.POSITIVE_INFINITY;
}

/**
 * Marks at most `limit` unacknowledged rows as seen, oldest first.
 *
 * Oldest first because that is the direction the server acknowledged in: the cursor is
 * the oldest row the person has loaded, and everything strictly older than it was marked
 * in one statement. The count is therefore spent on the bottom of the page, not the top.
 *
 * Marking more than the server did is the defect this exists to prevent. The panel shows
 * each row's own "· seen" marker from this local state, so a row the server left
 * unacknowledged that the client painted would claim the person looked at something they
 * did not — and would revert on reload, which reads as a bug in the badge rather than a
 * bug in the marker.
 */
export function markAcknowledged<T extends { acknowledged: boolean; acknowledged_at: string }>(
  entries: readonly T[],
  limit: number,
  at: string
): T[] {
  let left = limit;
  // Walked from the bottom of the page, which is the oldest end of a newest-first list
  // and therefore the end the server's `id < cursor` marks. Spending the count from the
  // top would paint the rows the person is looking at while leaving the ones the server
  // actually acknowledged showing as unseen.
  const marked = [...entries];
  for (let index = marked.length - 1; index >= 0 && left > 0; index--) {
    if (marked[index].acknowledged) continue;
    marked[index] = { ...marked[index], acknowledged: true, acknowledged_at: at };
    left -= 1;
  }
  return marked;
}
