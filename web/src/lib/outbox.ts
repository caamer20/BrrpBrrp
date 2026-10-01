// Account-scoped, opt-in storage for unconfirmed sends and text awaiting recovery.
export type OutboxEntry = {
  key: OperationKey;
  conversation: string;
  // The thread the operation was filed into, as part of its destination, or '' for an
  // ordinary message in the conversation itself. An entry written before threads
  // were recorded has no field at all, and its destination is therefore unknown: it
  // is never retried automatically (see boundOutbox), because retrying it would guess
  // between a thread and the channel it lives in.
  thread_id?: string;
  content: string;
  attachments: string[];
  reply_to: string;
  created: string;
  // Recovery records cannot be retried. They hold only the author's text, never attachment
  // claims or quoted server content, for one day after recovery is first offered.
  recovery?: string;
  recovered_at?: string;
};

export const OutboxLimit = 50;
export const OutboxTextLimit = 64 * 1024;
export const OutboxAgeLimit = 24 * 60 * 60 * 1000;
// The largest clock disagreement an operation is still retried across. A stamp ahead of
// this clock usually means the device clock was corrected backwards after the send, so
// the operation is a little younger than it looks. The allowance is deliberately small
// and one-directional: without a bound, a timestamp arbitrarily in the future would keep
// an operation — and the author's text — retryable forever, past the server's receipt
// window and past the point where "may or may not have been sent" is still a useful
// thing to say.
export const OutboxClockSkew = 5 * 60 * 1000;
// OperationKey is the durable identity of one send: a UUID, validated wherever it is
// read. Adopting a stored key into a composer must not adopt a malformed one, because
// that key is what the server reconciles against its receipt.
export type OperationKey = `${string}-${string}-${string}-${string}-${string}`;
type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const keyFor = (account: string) => `brrp.outbox.${account}`;

export function isOperationKey(value: string): value is OperationKey {
  return idPattern.test(value);
}

function isEntry(value: unknown): value is OutboxEntry {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const entry = value as Record<string, unknown>;
  return (
    typeof entry.key === 'string' &&
    isOperationKey(entry.key) &&
    typeof entry.conversation === 'string' &&
    idPattern.test(entry.conversation) &&
    typeof entry.content === 'string' &&
    entry.content.length <= 4000 &&
    Array.isArray(entry.attachments) &&
    entry.attachments.length <= 5 &&
    entry.attachments.every((id) => typeof id === 'string' && idPattern.test(id)) &&
    (entry.content.trim() !== '' || entry.attachments.length > 0 || !!entry.recovery) &&
    typeof entry.reply_to === 'string' &&
    (entry.reply_to === '' || idPattern.test(entry.reply_to)) &&
    (entry.thread_id === undefined ||
      (typeof entry.thread_id === 'string' &&
        (entry.thread_id === '' || idPattern.test(entry.thread_id)))) &&
    typeof entry.created === 'string' &&
    Number.isFinite(Date.parse(entry.created)) &&
    (entry.recovery === undefined ||
      (typeof entry.recovery === 'string' &&
        entry.recovery.length <= 1000 &&
        typeof entry.recovered_at === 'string' &&
        Number.isFinite(Date.parse(entry.recovered_at))))
  );
}

export function operationExpired(entry: Pick<OutboxEntry, 'created'>, now = Date.now()) {
  const age = now - Date.parse(entry.created);
  // A stamp slightly in the future is an uncertain send, not an expired one: the clock
  // can run behind the created stamp an operation was submitted under, and converting
  // it to recovery would discard a retry the receipt can still answer. Past the skew
  // allowance, a future stamp is untrustworthy rather than younger, and an undecodable
  // stamp is too: neither can promise the receipt is still there, so neither retries.
  if (!Number.isFinite(age)) return true;
  return age >= OutboxAgeLimit || age < -OutboxClockSkew;
}

// The reason an expired attempt is handed back for recovery. A future stamp and an old
// one are different situations to the person reading them, and "too old" would be a
// false description of a clock problem.
function expiryReason(entry: Pick<OutboxEntry, 'created'>, now: number) {
  const age = now - Date.parse(entry.created);
  return age < 0
    ? 'This send was saved with a clock reading that cannot be checked, so it was not retried automatically. Your text is here; check the conversation and send it again.'
    : 'This send is too old to retry safely. Check the conversation before sending its text again.';
}

export function recoveryEntry(entry: OutboxEntry, reason: string, now = Date.now()): OutboxEntry {
  return {
    ...entry,
    attachments: [],
    reply_to: '',
    recovery: reason.slice(0, 1000),
    recovered_at: new Date(now).toISOString()
  };
}

// Apply one shared bound to reads and writes. Never truncate an operation's intent: the
// original key must always carry the exact original text. Oldest whole entries leave first.
export function boundOutbox(entries: unknown[], now = Date.now()): OutboxEntry[] {
  const encoder = new TextEncoder();
  const bounded: OutboxEntry[] = [];
  const seen = new Set<string>();
  let bytes = 0;
  for (const candidate of [...entries].reverse()) {
    if (!isEntry(candidate) || seen.has(candidate.key)) continue;
    seen.add(candidate.key);
    let entry = candidate;
    if (entry.recovery) {
      const age = now - Date.parse(entry.recovered_at!);
      // The same bounded skew as a submitted operation: a recovery offered a moment
      // ahead of this clock because the clock moved is still offered, and one stamped
      // arbitrarily ahead is not kept forever either.
      if (!Number.isFinite(age) || age >= OutboxAgeLimit || age < -OutboxClockSkew) continue;
      entry = recoveryEntry(entry, entry.recovery, Date.parse(entry.recovered_at!));
    } else if (entry.thread_id === undefined) {
      // The entry predates thread recording, and whether it was going to a thread or
      // to the conversation itself cannot be told from it. Retrying it would guess —
      // and a guess that lands in the channel files a thread reply where its author
      // never sent it, while a guess that lands in a thread is impossible to make
      // without knowing which one — so the text is held for the author to place
      // deliberately. Nothing is lost and nothing is rerouted.
      entry = recoveryEntry(
        entry,
        'This send was saved before threads were recorded, so where it was going cannot be told. Your text is here; check the conversation and send it again.'
      );
    } else if (operationExpired(entry, now)) {
      entry = recoveryEntry(entry, expiryReason(entry, now), now);
    }
    const size = encoder.encode(entry.content).byteLength;
    if (bounded.length >= OutboxLimit || bytes + size > OutboxTextLimit) break;
    bytes += size;
    // Whitelist fields, so unknown stored fields cannot smuggle tokens or attachment bytes.
    bounded.push({
      key: entry.key,
      conversation: entry.conversation,
      content: entry.content,
      attachments: [...entry.attachments],
      reply_to: entry.reply_to,
      // Kept as a whole: '' is an ordinary message, a value is a thread, and absent
      // stays absent so an unresolved destination is never invented.
      ...(entry.thread_id !== undefined ? { thread_id: entry.thread_id } : {}),
      created: entry.created,
      ...(entry.recovery ? { recovery: entry.recovery, recovered_at: entry.recovered_at } : {})
    });
  }
  return bounded.reverse();
}

export function readOutbox(storage: StorageLike, account: string, now = Date.now()): OutboxEntry[] {
  if (!idPattern.test(account)) return [];
  try {
    const saved: unknown = JSON.parse(storage.getItem(keyFor(account)) || '[]');
    return Array.isArray(saved) ? boundOutbox(saved, now) : [];
  } catch {
    return [];
  }
}

export function writeOutbox(
  storage: StorageLike,
  account: string,
  entries: OutboxEntry[],
  now = Date.now()
) {
  if (!idPattern.test(account)) return;
  try {
    const bounded = boundOutbox(entries, now);
    if (!bounded.length) storage.removeItem(keyFor(account));
    else storage.setItem(keyFor(account), JSON.stringify(bounded));
  } catch {
    /* A full or blocked browser store must not interrupt sending. */
  }
}

export function clearOutbox(storage: StorageLike, account: string) {
  if (!idPattern.test(account)) return;
  try {
    storage.removeItem(keyFor(account));
  } catch {
    /* Storage may be unavailable. */
  }
}

export function retryableStatus(status: number) {
  return status === 0 || status === 408 || status === 425 || status === 429 || status >= 500;
}

export function outcomeExplanation(status: number, detail = '') {
  return (
    detail.trim() ||
    (status === 403 || status === 404
      ? 'You no longer have access to that conversation.'
      : status === 413
        ? 'That message was too large to send.'
        : status === 422
          ? 'An attachment is no longer available.'
          : 'That message could not be sent.')
  );
}
