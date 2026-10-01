import { isOperationKey, type OperationKey } from './outbox';

// The composer's own record of the operations it submitted.
//
// The outbox answers "what has this account not been told about yet". That is a
// different question from "are the words in this composer a retry of an operation
// that was already attempted": identical words can be two deliberate sends, and the
// operation a draft belongs to can be older than the newest one. An association is
// written when a composer submits words under a key, so re-submitting exactly those
// words retries that operation under its own key while anything else is new work.
// Matching text against the outbox alone cannot tell those apart, which is why this
// record exists (review 17, V17-01).
export type ComposerOperation = { key: OperationKey; content: string };

type OperationsStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
const operationsKeyFor = (account: string) => `brrp.operations.${account}`;
const accountPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
// A thread composer's slot. Associations are made for thread replies today; the
// pattern is the same shape the drafts store uses, so nothing else can be smuggled in.
const slotPattern = /^thread:[0-9a-f-]{36}$/;
// Bounds, shared by reads and writes: enough unresolved operations for one composer's
// history and enough composers for a busy session, while a corrupted or hostile store
// cannot materialise an unbounded record.
export const OperationsPerSlotLimit = 8;
export const OperationsSlotLimit = 50;

function isOperation(value: unknown): value is ComposerOperation {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const operation = value as Record<string, unknown>;
  return (
    typeof operation.key === 'string' &&
    isOperationKey(operation.key) &&
    typeof operation.content === 'string' &&
    operation.content.length <= 4000
  );
}

// readOperations returns the stored associations, validated and bounded. A record
// that fails validation is dropped rather than repaired: a composer deciding a key
// from a malformed record would retry an operation that never existed.
export function readOperations(
  storage: OperationsStorage,
  account: string
): Record<string, ComposerOperation[]> {
  if (!accountPattern.test(account)) return {};
  try {
    const saved: unknown = JSON.parse(storage.getItem(operationsKeyFor(account)) || '{}');
    if (!saved || typeof saved !== 'object' || Array.isArray(saved)) return {};
    const operations: Record<string, ComposerOperation[]> = {};
    let slots = 0;
    for (const [slot, list] of Object.entries(saved)) {
      if (slots >= OperationsSlotLimit) break;
      if (!slotPattern.test(slot) || !Array.isArray(list)) continue;
      const kept: ComposerOperation[] = [];
      const seen = new Set<string>();
      for (const candidate of list) {
        if (!isOperation(candidate) || seen.has(candidate.key)) continue;
        seen.add(candidate.key);
        kept.push({ key: candidate.key, content: candidate.content });
      }
      if (!kept.length) continue;
      operations[slot] = kept.slice(-OperationsPerSlotLimit);
      slots++;
    }
    return operations;
  } catch {
    return {};
  }
}

// writeOperations persists the composer's associations. The newest slots are kept when
// the bound is exceeded, because those are the ones a live composer is working with.
export function writeOperations(
  storage: OperationsStorage,
  account: string,
  operations: Map<string, ComposerOperation[]>
) {
  if (!accountPattern.test(account)) return;
  const bounded: Record<string, ComposerOperation[]> = {};
  for (const [slot, list] of [...operations].slice(-OperationsSlotLimit)) {
    if (!slotPattern.test(slot)) continue;
    const kept = list.filter(isOperation).slice(-OperationsPerSlotLimit);
    if (!kept.length) continue;
    bounded[slot] = kept.map((operation) => ({ key: operation.key, content: operation.content }));
  }
  try {
    storage.setItem(operationsKeyFor(account), JSON.stringify(bounded));
  } catch {
    /* A full or blocked browser store must not interrupt a send. */
  }
}

export function clearOperations(storage: OperationsStorage, account: string) {
  storage.removeItem(operationsKeyFor(account));
}
