// The decisions a thread view makes about a send, kept out of the component so they
// can be tested directly. They are about what the composer should offer next, and
// getting them wrong invites a person to send the same thing twice.

/** What a send attempt left the composer in. */
export type ThreadSendState = 'ready' | 'refused' | 'uncertain';

/**
 * The outcome of a send, as a control state.
 *
 * A definite refusal — 400, 403, 429 — provably wrote nothing, so the same
 * operation key is still free and the action stays available. Anything else may have
 * committed with the response lost, so it is *uncertain* rather than refused: telling
 * a person a send failed when it succeeded invites them to send it again as new work,
 * and the receipt would then answer a different intent (ADR 0014).
 *
 * A caller that already knows which of the two it is — the workspace names its
 * outcome, because only it can tell a definite answer from a lost one — passes
 * `outcome` and the status is not consulted.
 */
export function threadSubmitState(
  busy: boolean,
  error: { status?: number; outcome?: 'refused' | 'uncertain' } | null
): ThreadSendState {
  if (busy) return 'ready';
  if (!error) return 'ready';
  if (error.outcome) return error.outcome;
  if (error.status === 400 || error.status === 403 || error.status === 429) return 'refused';
  return 'uncertain';
}

/** Whether the reply control is live. */
export function canSendThreadReply(options: {
  open: boolean;
  locked: boolean;
  missing: boolean;
  sending: boolean;
  draft: string;
}): boolean {
  return (
    options.open &&
    !options.locked &&
    !options.missing &&
    !options.sending &&
    options.draft.trim().length > 0
  );
}

/** One still-unresolved operation a thread composer recorded: its key and its words. */
export type ThreadOperation = { key: string; content: string };

/**
 * The operation a composer's submitted words are a retry of, if any.
 *
 * The candidates are the composer's own association record for the thread — the
 * submissions it actually made, each with the immutable snapshot it was submitted
 * under. The exact words being submitted select *that* operation among every
 * unresolved one: retrying the newest reply reuses its own key even while an older
 * reply is still unresolved, instead of rotating the key because the older entry
 * came first. `undefined` means these words are new work, which is what edited words
 * and words whose operation has settled are (review 17, V17-01).
 */
export function retryOperationKey(
  operations: readonly ThreadOperation[],
  submitted: string
): string | undefined {
  return operations.find((operation) => operation.content === submitted)?.key;
}
