import { expect } from '@playwright/test';
import { createAccount, OWNER, signIn, signOut, test, totpCode } from './support.mjs';

// Ownership is the one authority no role can delegate, so its journey is about the
// boundary rather than about a form: the ceremony completes the installation's first
// ownership, it refuses an account that cannot satisfy the strong-authentication policy
// and says how to fix that, ownership then moves with the operator surface attached to
// it, and the account that just lost ownership cannot delete the account that gained it
// (review R2-01, first reproduced as a transaction race).
//
// The journey starts where a fresh installation actually is: an administrator holds the
// operator bridge because nobody owns the installation yet. Both ceremonies are the
// same endpoint, so the first designation and the later transfer are the same check
// seen from both sides.
//
// This journey runs last and intentionally leaves the installation owned by the account
// it transfers to: ownership is one-way for the actor, and the server it runs against is
// thrown away at the end of the run. Transferring back would need the new owner to
// enrol a factor of its own, which no other journey needs.
const STEWARD = { username: 'steward', password: 'a steward passphrase' };

test('ownership designation and transfer move the operator surface', async ({ page }) => {
  test.setTimeout(240000);
  await signIn(page, OWNER);
  const stewardID = await createAccount(page.request, STEWARD);

  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  const tabs = tools.getByRole('navigation', { name: 'Server administration' });
  await tabs.getByRole('button', { name: 'People' }).click();
  // The ceremony is an operator surface: it is offered here because this account holds
  // the operator tier, and it disappears below when it no longer does.
  await expect(tools.getByText('Installation owner')).toBeVisible();
  await tools.getByText('Installation owner').click();
  await expect(tools.getByText('No owner is designated for this installation.')).toBeVisible();

  const ceremony = async (account, button) => {
    await tools.getByLabel('New owner username').fill(account.username);
    await tools.getByLabel('Type the username to confirm').fill(account.username);
    await tools.getByRole('button', { name: button }).click();
  };

  // The ceremony can reach every private boundary, so the default policy requires an
  // enrolled second factor as well as a recent sign-in. The refusal is actionable and
  // offers the place to fix it rather than failing a submission.
  await ceremony(OWNER, 'Designate owner');
  await expect(
    tools.getByText(
      'This ceremony needs an account that has enrolled a second factor and signed in recently.'
    )
  ).toBeVisible();
  await tools.getByRole('button', { name: 'Open account security' }).click();
  const preferences = page.getByRole('dialog', { name: 'Your preferences' });
  await preferences.getByRole('tab', { name: 'Account' }).click();
  const twoFactor = preferences
    .locator('form.settings-section')
    .filter({ hasText: 'Two-factor authentication' });
  await twoFactor.getByLabel('Current password').fill(OWNER.password);
  await twoFactor.getByRole('button', { name: 'Set up authenticator' }).click();
  const setupKey = (await preferences.locator('code.setup-key').innerText()).trim();
  await twoFactor.getByLabel('Authenticator or recovery code').fill(totpCode(setupKey));
  await twoFactor.getByRole('button', { name: 'Verify and enable' }).click();
  await expect(preferences.getByText('Save these recovery codes now.')).toBeVisible();
  await preferences.getByRole('button', { name: 'I have saved my codes' }).click();
  await expect(preferences.getByText('Enabled · 10 recovery codes left.')).toBeVisible();
  await preferences.getByRole('button', { name: 'Close dialog' }).click();

  // The same submission now completes, and the ceremony stops being the bootstrap form
  // and becomes the transfer form: this account owns the installation.
  await ceremony(OWNER, 'Designate owner');
  await expect(tools.getByText('@release_admin is now the installation owner.')).toBeVisible();
  await expect(tools.getByText('The owner is @release_admin.')).toBeVisible();

  await ceremony(STEWARD, 'Transfer ownership');
  // The answer has to outlive the ceremony: moving ownership away from this account
  // removes the operator tier, so the form it was typed into is unmounted by the same
  // change.
  // The toast specifically, not any status region: the ceremony's own success note
  // carries the same sentence, and while the refresh is in flight both can exist.
  await expect(
    page.locator('div.toast').filter({ hasText: '@steward is now the installation owner.' })
  ).toBeVisible();

  // The operator surface follows ownership without a reload: this account keeps the
  // administration panel through its administrator role, but the ceremony that belongs
  // to the operator tier is gone.
  await expect(tools.getByText('Installation owner')).toHaveCount(0);

  // The account that just lost ownership cannot delete the new owner, and the refusal
  // leaves the account in place. This is the invariant the race regression proves at
  // the transaction boundary, asserted here through the API a client would call.
  const refused = await page.request.delete(`/api/admin/users/${stewardID}`, {
    data: { confirm: STEWARD.username }
  });
  expect(refused.status()).toBe(403);
  expect(await refused.text()).toContain('ownership changes only through the ownership ceremony');

  // The rest of the operator tier went with it — no backup control — while the
  // administrator role still reads the audit log, and the transfer is recorded there.
  await tabs.getByRole('button', { name: 'Maintenance' }).click();
  await expect(tools.getByText('Recent activity')).toBeVisible();
  await expect(tools.getByRole('button', { name: 'Create backup' })).toHaveCount(0);
  await expect(tools.getByText('owner designated').first()).toBeVisible();
  await tools.getByRole('button', { name: 'Close dialog' }).click();

  // The new owner holds the operator surface even though no role grants it: ownership
  // is not a role and cannot be delegated by one.
  // Signing out is awaited through the page it leaves behind: clicking the control only
  // starts the request, and signing in again before it lands would re-enter the session
  // that just ended.
  await signOut(page);
  await signIn(page, STEWARD);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const stewardTools = page.getByRole('dialog', { name: 'Manage your server' });
  await stewardTools
    .getByRole('navigation', { name: 'Server administration' })
    .getByRole('button', { name: 'People' })
    .click();
  await stewardTools.getByText('Installation owner').click();
  await expect(stewardTools.getByText('The owner is @steward.')).toBeVisible();
  await stewardTools.getByRole('button', { name: 'Close dialog' }).click();
});
