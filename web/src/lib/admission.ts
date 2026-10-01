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
 * The ids the server said it acknowledged, as a set the local list can be marked
 * from. The server returns exactly the rows it persisted, so the client never has to
 * guess which of the rows on screen a count refers to; a count painted over whichever
 * entries happen to be loaded marked the wrong ones (review 17, V17-04).
 */
export function acknowledgedRows(acknowledged: unknown): ReadonlySet<string> {
  if (!Array.isArray(acknowledged)) return new Set();
  const ids = new Set<string>();
  for (const id of acknowledged) if (typeof id === 'string' && id) ids.add(id);
  return ids;
}

/**
 * Marks the rows whose ids are in `acknowledged` as seen, and leaves every other row
 * exactly as it was.
 *
 * Marking by identity rather than by position is the contract: the list is
 * newest-first, the server marks an id range, and between the two a row can be
 * acknowledged on another device or arrive late. Painting a count over the top of the
 * list claimed rows were seen that were not, and a reload then showed the badge
 * returning — the visible form of the two sides disagreeing about the same set.
 */
export function markAcknowledged<
  T extends { acknowledged: boolean; acknowledged_at: string; id: string }
>(entries: readonly T[], acknowledged: ReadonlySet<string>, at: string): T[] {
  if (!acknowledged.size) return [...entries];
  return entries.map((entry) =>
    !entry.acknowledged && acknowledged.has(entry.id)
      ? { ...entry, acknowledged: true, acknowledged_at: at }
      : entry
  );
}
