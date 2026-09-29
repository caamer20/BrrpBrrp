import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  canSubmitIn,
  keepsOperationKey,
  offersReconciliation,
  submitDisabled,
  submitLabel
} from '../src/lib/report-dialog-state.ts';

// A report commits an immutable snapshot, a rejection and a block in one
// transaction. The dialog previously used a `submitted` flag set before the request
// and never cleared, so a definite refusal — 400, 409, 429 — left the button
// rendered enabled while its handler returned immediately. A person could press it
// forever and nothing would happen (completion review 12, R12-07).

/**
 * @typedef {'loading'|'load-failed'|'ready'|'submitting'|'refused'|'changed'|'uncertain'|'checking'|'uncommitted'|'committed'} ReportState
 */

/** @type {ReportState[]} */
const every = [
  'loading',
  'load-failed',
  'ready',
  'submitting',
  'refused',
  'changed',
  'uncertain',
  'checking',
  'uncommitted',
  'committed'
];

test('the rendered control and its handler never disagree', () => {
  // This is the defect itself, stated as an invariant over every visible state.
  for (const phase of every) {
    for (const reason of ['', '   ', 'unwelcome request']) {
      for (const busy of [false, true]) {
        const live = canSubmitIn(phase, busy, reason);
        const rendered = !submitDisabled(phase, busy, reason);
        assert.equal(
          live,
          rendered,
          `${phase} with reason=${JSON.stringify(reason)} busy=${busy}: handler ${live ? 'accepts' : 'ignores'} but the button renders ${rendered ? 'enabled' : 'disabled'}`
        );
      }
    }
  }
});

test('a definite refusal leaves the action available', () => {
  // Nothing was written, so the person must be able to try again with the same
  // intent and the same key. This is the state the sticky flag broke.
  assert.equal(canSubmitIn('refused', false, 'unwelcome'), true);
  assert.equal(submitLabel('refused', false), 'Try again');
  // And the key is held: it is still the same operation, not a new one.
  assert.equal(keepsOperationKey('refused'), true);

  // A 429 is the same kind of refusal — a bound was reached, nothing was written.
  assert.equal(canSubmitIn('refused', false, 'unwelcome'), true);
});

test('a changed request is not retryable until it is read again', () => {
  // The frozen version no longer describes what would be written, so the same
  // action cannot be offered — and the key rotates, because this is different work.
  assert.equal(canSubmitIn('changed', false, 'unwelcome'), false);
  assert.equal(keepsOperationKey('changed'), false);
});

test('an uncommitted outcome is retryable, an uncertain one is not', () => {
  // Reconciliation proved nothing was written, so the action is available again.
  assert.equal(canSubmitIn('uncommitted', false, 'unwelcome'), true);
  assert.equal(submitLabel('uncommitted', false), 'Report again');
  assert.equal(keepsOperationKey('uncommitted'), true);

  // An uncertain outcome may well have committed, so the only offered action is
  // reconciliation. Pressing report again would be a second operation.
  assert.equal(canSubmitIn('uncertain', false, 'unwelcome'), false);
  assert.equal(canSubmitIn('checking', false, 'unwelcome'), false);
  assert.equal(keepsOperationKey('uncertain'), true);
});

test('reconciliation is offered only where the outcome is unknown', () => {
  assert.equal(offersReconciliation('uncertain'), true);
  assert.equal(offersReconciliation('checking'), true);
  assert.equal(offersReconciliation('uncommitted'), true);
  // Offering it after a definite refusal would invite someone to check for
  // something that was never written.
  for (const phase of /** @type {ReportState[]} */ ([
    'ready',
    'refused',
    'changed',
    'loading',
    'committed',
    'submitting'
  ])) {
    assert.equal(offersReconciliation(phase), false, `${phase} offered reconciliation`);
  }
});

test('in-flight and terminal states are not actionable', () => {
  // Loading has nothing to confirm, a failed load has nothing to report, a
  // committed operation is finished, and an in-flight one must not be re-sent.
  for (const phase of /** @type {ReportState[]} */ (['loading', 'load-failed', 'committed'])) {
    assert.equal(canSubmitIn(phase, false, 'unwelcome'), false, `${phase} was actionable`);
  }
  assert.equal(
    canSubmitIn('ready', true, 'unwelcome'),
    false,
    'a busy ready dialog was actionable'
  );
  assert.equal(submitLabel('submitting', false), 'Reporting…');
  assert.equal(submitLabel('ready', true), 'Reporting…');
});

test('an empty explanation blocks submission in every actionable state', () => {
  for (const phase of /** @type {ReportState[]} */ (['ready', 'refused', 'uncommitted'])) {
    assert.equal(canSubmitIn(phase, false, ''), false, `${phase} accepted an empty reason`);
    assert.equal(canSubmitIn(phase, false, '   '), false, `${phase} accepted a blank reason`);
    assert.equal(canSubmitIn(phase, false, 'why'), true, `${phase} refused a real reason`);
  }
});

// V13-08. A changed request and an unknown outcome are the two states where
// pressing the wrong control does real harm: one files a report about text the
// person never read, the other invites them to report the same request twice.
test('a changed request is only reportable once the new text is acknowledged', () => {
  assert.equal(canSubmitIn('changed', false, 'unwanted', false), false);
  assert.equal(canSubmitIn('changed', false, 'unwanted', true), true);
  // And the rendered control agrees, or it would look live and do nothing.
  assert.equal(submitDisabled('changed', false, 'unwanted', false), true);
  assert.equal(submitDisabled('changed', false, 'unwanted', true), false);
  // The acknowledgement is still not a blank cheque: it needs a reason and it does
  // not survive an in-flight submit.
  assert.equal(canSubmitIn('changed', true, 'unwanted', true), false);
  assert.equal(canSubmitIn('changed', false, '   ', true), false);
});

test('a refused request stays reportable, an unknown outcome does not', () => {
  // A definite refusal wrote nothing, so the same action and the same key are still
  // the right ones to try.
  for (const reason of ['unwanted', 'x']) {
    assert.equal(canSubmitIn('refused', false, reason), true);
    assert.equal(canSubmitIn('uncommitted', false, reason), true);
  }
  // An uncertain outcome must not offer the button: the operation may already have
  // committed, and a second press is a second report of the same request.
  assert.equal(canSubmitIn('uncertain', false, 'unwanted'), false);
  assert.equal(canSubmitIn('checking', false, 'unwanted'), false);
  assert.equal(offersReconciliation('uncertain'), true);
  assert.equal(offersReconciliation('checking'), true);
  assert.equal(offersReconciliation('uncommitted'), true);
});

// The transport-failure transition is the one that is easy to get wrong and hard to
// see: a network failure looks exactly like a refusal unless it is routed
// deliberately, and the two demand opposite responses.
test('a transport failure is an unknown outcome, not a refusal', () => {
  const source = readFileSync(
    new URL('../src/lib/components/ReportRequest.svelte', import.meta.url),
    'utf8'
  );
  const handler = source.slice(
    source.indexOf('} catch (failure) {'),
    source.indexOf('} finally {')
  );

  // Only the two that provably did not commit are refusals.
  assert.match(
    handler,
    /failure\.status === 400[\s\S]{0,80}429[\s\S]{0,400}phase = 'refused'/,
    'a definite refusal is not recognised'
  );
  // Everything else, including a transport failure, is uncertain: the request may
  // have committed and the response been lost.
  assert.match(
    handler,
    /else \{[\s\S]{0,900}phase = 'uncertain'/,
    'a transport failure is not routed to uncertain'
  );
  // And the uncertain branch says so in the terms the person reads.
  assert.match(handler, /could not confirm whether that report went through/i);
  // No branch may leave a transport failure sitting in a state that offers the
  // report button again.
  assert.doesNotMatch(handler, /status === 0[\s\S]{0,120}phase = 'refused'/);
});
