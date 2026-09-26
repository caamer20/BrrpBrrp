// Account-scoped, opt-in storage for unconfirmed sends and text awaiting recovery.
export type OutboxEntry = {
  key: string;
  conversation: string;
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
type StorageLike = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const keyFor = (account: string) => `brrp.outbox.${account}`;

function isEntry(value: unknown): value is OutboxEntry {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const entry = value as Record<string, unknown>;
  return (
    typeof entry.key === 'string' &&
    idPattern.test(entry.key) &&
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
  return !Number.isFinite(age) || age < 0 || age >= OutboxAgeLimit;
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
      if (age < 0 || age >= OutboxAgeLimit) continue;
      entry = recoveryEntry(entry, entry.recovery, Date.parse(entry.recovered_at!));
    } else if (operationExpired(entry, now)) {
      entry = recoveryEntry(
        entry,
        'This send is too old to retry safely. Check the conversation before sending its text again.',
        now
      );
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
