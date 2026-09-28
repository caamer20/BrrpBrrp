/**
 * Admission and account isolation for the private request-report dialog.
 *
 * A report commits an immutable snapshot, a rejection and a block in one
 * transaction, so a dialog opened by one account must never submit, check or
 * display anything on behalf of another. These predicates are the whole of that
 * rule, kept out of the component so they can be tested directly: the component's
 * own state is a sequence of `await`s, and the interesting case is what the next
 * `await` returns when the account changed underneath it.
 */

export interface AdmissionState {
  alive: boolean;
  account: string | undefined;
  currentAccountId: string | undefined;
  generation: number;
  authorizationGeneration: number;
  authorizationUnverified: boolean;
}

export interface SubmitState extends AdmissionState {
  busy: boolean;
  preview: { version: string } | null;
  reason: string;
  submitted: boolean;
}

/**
 * Whether work started by this dialog may still commit.
 *
 * `alive` is the component's own teardown, `account` the account the dialog was
 * opened for, and the two workspace fields are the server's authority decision.
 * An unverified authorization is treated as a refusal: a capability that has not
 * been confirmed must not be acted on.
 */
export function admitted(state: AdmissionState) {
  return (
    state.alive &&
    state.account !== undefined &&
    state.account !== null &&
    state.account === state.currentAccountId &&
    state.generation === state.authorizationGeneration &&
    !state.authorizationUnverified
  );
}

/**
 * Whether a response that has just arrived may still be shown.
 *
 * This is deliberately narrower than {@link admitted}: a failure to read is worth
 * reporting even once the authority is unverified, because the person still needs
 * to know nothing was sent. What it must never do is apply to another account.
 */
export function mayReportFailure(state: AdmissionState) {
  return state.alive && state.account !== null && state.account === state.currentAccountId;
}

/**
 * Whether the dialog may submit a report of the given preview.
 *
 * The preview has to have loaded and the reason has to be non-empty, because the
 * server saves the text the reporter confirmed — a reason invented at submit time
 * is not what was shown to them.
 */
export function maySubmit(state: SubmitState) {
  return (
    !state.busy &&
    admitted(state) &&
    state.preview !== null &&
    typeof state.reason === 'string' &&
    state.reason.trim() !== '' &&
    !state.submitted
  );
}

/**
 * Whether an empty introduction can be reported at all.
 *
 * An introduction is optional in a request, so a pending attempt with no text is
 * a real state. It is still reportable — the attempt itself is the evidence — but
 * the dialog has to say so rather than presenting an empty snapshot as if it were
 * something the reporter chose to disclose.
 */
export function describesAnIntroduction(preview: { has_introduction: boolean } | null) {
  return preview !== null && preview.has_introduction === true;
}
