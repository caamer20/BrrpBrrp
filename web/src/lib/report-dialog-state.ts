/**
 * The states of the private request-report dialog.
 *
 * A report commits an immutable snapshot, a rejection and a block in one
 * transaction, so the dialog has to be honest about what it knows. A single
 * "submitted" flag could not be: it was set before the request went out and never
 * cleared, so a definite refusal — a 400, a 409, a 429 — left the button looking
 * enabled while the handler returned immediately. The person could press it
 * forever and nothing would happen (completion review 12, R12-07).
 *
 * These are explicit because each one decides three different things: whether the
 * button is live, what it says, and what the person is told happened. The
 * distinction that matters most is between a definite refusal, where nothing was
 * written and the same action may be retried once the cause is addressed, and an
 * uncertain outcome, where the operation may well have committed and the only safe
 * next step is to reconcile it.
 */
export type ReportState =
  /** The introduction is being fetched; nothing can be confirmed yet. */
  | 'loading'
  /** The preview could not be fetched at all. The operation is not available. */
  | 'load-failed'
  /** A confirmed preview and an editable explanation. The action is available. */
  | 'ready'
  /** The request is in flight. The action is unavailable but visibly so. */
  | 'submitting'
  /** The server refused outright: nothing was written. The action may be retried. */
  | 'refused'
  /** The request changed after it was previewed. A fresh confirmation is required. */
  | 'changed'
  /**
   * The outcome is not known: the response was lost, or the command's own
   * authority invalidation arrived before it. The operation may have committed, so
   * the same key must be reused and the only offered action is reconciliation.
   */
  | 'uncertain'
  /** A reconciliation is in flight. */
  | 'checking'
  /** Reconciliation found that the operation never committed. */
  | 'uncommitted'
  /** Reconciliation found that the operation committed, with a historical outcome. */
  | 'committed';

/**
 * Whether the submit control is live.
 *
 * A refusal keeps it live because nothing was written; a changed request does not,
 * because the person has to read the new text and confirm it before the same
 * action can be offered again.
 */
export function canSubmitIn(state: ReportState, busy: boolean, reason: string): boolean {
  if (busy) return false;
  if (!reason.trim()) return false;
  return state === 'ready' || state === 'refused' || state === 'uncommitted';
}

/**
 * What the submit control says.
 *
 * A refusal or an uncommitted outcome names the next step rather than repeating
 * the original verb, so the button never looks like it is doing nothing.
 */
export function submitLabel(state: ReportState, busy: boolean): string {
  if (busy || state === 'submitting') return 'Reporting…';
  if (state === 'refused') return 'Try again';
  if (state === 'uncommitted') return 'Report again';
  return 'Report and block';
}

/**
 * The control's disabled state, which is the single source of truth for the
 * rendered button.
 *
 * A control that looks live and does nothing is worse than one that looks dead,
 * so this must agree with {@link canSubmitIn} exactly. The test that matters is
 * that no visible state leaves them disagreeing.
 */
export function submitDisabled(state: ReportState, busy: boolean, reason: string): boolean {
  return !canSubmitIn(state, busy, reason);
}

/**
 * Whether the frozen operation key may be reused.
 *
 * The same key is what makes a retry the same action rather than new work, so it
 * is held across an uncertain outcome and across a refusal of the same intent. A
 * changed request is different work and needs a new key, because the frozen
 * version no longer describes what would be written.
 */
export function keepsOperationKey(state: ReportState): boolean {
  return state !== 'changed';
}

/**
 * Whether the reconciliation control is offered.
 *
 * Offered only where the outcome is genuinely unknown. Offering it after a definite
 * refusal would invite someone to check for something that was never written.
 */
export function offersReconciliation(state: ReportState): boolean {
  return state === 'uncertain' || state === 'checking' || state === 'uncommitted';
}
