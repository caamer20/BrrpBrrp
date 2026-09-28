import test from 'node:test';
import assert from 'node:assert/strict';
import {
  admitted,
  mayReportFailure,
  maySubmit,
  describesAnIntroduction
} from '../src/lib/request-report.ts';

// A report commits an immutable snapshot, a rejection and a block in one
// transaction. The dialog that starts it can be overtaken by an account switch, a
// capability revocation or a teardown while its request is in flight, and each of
// those has to stop the commit or stop the result being shown. These are the
// predicates the component gates every await behind.

const base = {
  alive: true,
  account: 'account-1',
  currentAccountId: 'account-1',
  generation: 3,
  authorizationGeneration: 3,
  authorizationUnverified: false
};

test('admission holds only while the account and authority are unchanged', () => {
  assert.equal(admitted(base), true);

  // The account changed mid-flight: this is the case that would otherwise submit
  // one account's report under another account's authority.
  assert.equal(admitted({ ...base, currentAccountId: 'account-2' }), false);
  assert.equal(admitted({ ...base, currentAccountId: undefined }), false);

  // The capability was revoked and re-granted, so the generation moved. The
  // generation is the fence: a matching account with a stale grant is still stale.
  assert.equal(admitted({ ...base, authorizationGeneration: 4 }), false);
  assert.equal(admitted({ ...base, generation: 2 }), false);

  // An unverified authorization is refused rather than assumed. The server has not
  // said this account may report, so the client does not act on its behalf.
  assert.equal(admitted({ ...base, authorizationUnverified: true }), false);

  // Teardown: the dialog is gone, so nothing may commit after it.
  assert.equal(admitted({ ...base, alive: false }), false);
});

test('an unverified authority still reports a failure to the same account', () => {
  // Reading a receipt is not a mutation, so the stricter admission does not apply.
  // What must never happen is the failure being attributed to another account.
  assert.equal(mayReportFailure({ ...base, authorizationUnverified: true }), true);
  assert.equal(mayReportFailure({ ...base, currentAccountId: 'account-2' }), false);
  assert.equal(mayReportFailure({ ...base, currentAccountId: undefined }), false);
  assert.equal(mayReportFailure({ ...base, alive: false }), false);
});

test('submission needs a confirmed preview, a reason and a live authority', () => {
  const ready = {
    ...base,
    busy: false,
    preview: { version: 'v1' },
    reason: 'unwanted',
    submitted: false
  };
  assert.equal(maySubmit(ready), true);

  // A reason is part of what the server saves, so an empty one is not a submission
  // the reporter ever confirmed.
  assert.equal(maySubmit({ ...ready, reason: '   ' }), false);
  assert.equal(maySubmit({ ...ready, reason: '' }), false);

  // No preview means no version to submit, and submitting without one would save
  // text nobody saw.
  assert.equal(maySubmit({ ...ready, preview: null }), false);

  // Already submitted: a second submit under a new key would file a second report.
  assert.equal(maySubmit({ ...ready, submitted: true }), false);

  // And the authority rules still apply through the submit path.
  assert.equal(maySubmit({ ...ready, currentAccountId: 'account-2' }), false);
  assert.equal(maySubmit({ ...ready, authorizationUnverified: true }), false);
  assert.equal(maySubmit({ ...ready, busy: true }), false);
});

test('an empty introduction is a real attempt, not an absent one', () => {
  // A request introduction is optional, so a pending attempt with no text exists.
  // It is still reportable, but the dialog must not present an empty snapshot as
  // something the reporter chose to disclose.
  assert.equal(describesAnIntroduction({ has_introduction: false }), false);
  assert.equal(describesAnIntroduction({ has_introduction: true }), true);
  assert.equal(describesAnIntroduction(null), false);
});
