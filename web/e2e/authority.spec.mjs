import { expect, test } from '@playwright/test';
import {
  assignOnly,
  createAccount,
  createRole,
  OWNER,
  setRolePermissions,
  signIn,
  signInOverApi,
  signOut
} from './support.mjs';

// These journeys need the accounts and channels the first journey creates, and the
// server they run against is the same one. They sign in as the account that journey
// left behind, and every assertion reads the rendered page rather than the model, so
// a capability gap or a stale view is visible here rather than only in a unit test.
//
// The owner's requests come from Playwright's `request` fixture, which has its own
// cookie jar, so a delegated account can stay signed in in the page while the owner
// changes the role that admits it. That is how "live demotion" is exercised: authority
// changes while somebody is using the surface it granted.
const INVITED = { username: 'invite_only', password: 'a delegated passphrase' };
const AUDITOR = { username: 'audit_only', password: 'a delegated passphrase' };
const INVITER_PERMISSIONS = ['channel.read', 'message.send', 'invite.manage'];
const INVITER_WITHOUT_MANAGEMENT = ['channel.read', 'message.send'];

test('single-capability managers see only the tools they were granted', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  await signIn(page, OWNER);
  await signInOverApi(request);

  // A role manager would normally do this through the interface; setting it up over
  // the API keeps this journey about what the delegated account can see.
  const inviterRole = await createRole(request, 'inviters', INVITER_PERMISSIONS);
  const auditorRole = await createRole(request, 'auditors', [
    'channel.read',
    'message.send',
    'audit.read'
  ]);
  const inviterID = await createAccount(request, INVITED);
  const auditorID = await createAccount(request, AUDITOR);
  await assignOnly(request, inviterID, [inviterRole]);
  await assignOnly(request, auditorID, [auditorRole]);

  // The invitation-only manager reaches invitations and nothing else.
  await signOut(page);
  await signIn(page, INVITED);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  await expect(tools).toBeVisible();
  const inviterTabs = tools.getByRole('navigation', { name: 'Server administration' });
  await expect(inviterTabs.getByRole('button', { name: 'Invitations' })).toBeVisible();
  for (const absent of ['People', 'Channels', 'Roles', 'Settings', 'Maintenance']) {
    await expect(inviterTabs.getByRole('button', { name: absent })).toHaveCount(0);
  }
  await tools.getByRole('button', { name: 'Create invitation' }).click();
  const invitationLink = tools.locator('label:has-text("Copy this invitation link") input');
  await expect(invitationLink).toBeVisible();
  expect(await invitationLink.inputValue()).toContain('invite=');

  // Live demotion: the role that admitted this account to the tab loses the capability
  // while the panel is open. The tab must go without a reload, and the account must be
  // told why it now has nothing here rather than left with a tab that fails every call.
  await setRolePermissions(request, inviterRole, INVITER_WITHOUT_MANAGEMENT);
  await expect(inviterTabs.getByRole('button', { name: 'Invitations' })).toHaveCount(0);
  await expect(
    tools.getByText('Your account does not hold any server management capability.')
  ).toBeVisible();

  // The grant is reversible, and the manager recovers its surface without signing in
  // again: a demotion that needed a reload would be a broken recovery path.
  await setRolePermissions(request, inviterRole, INVITER_PERMISSIONS);
  await expect(inviterTabs.getByRole('button', { name: 'Invitations' })).toBeVisible();
  await tools.getByRole('button', { name: 'Close dialog' }).click();

  // The audit-only manager reads the log inside Maintenance, without the server
  // health, setup checks or backup controls that belong to other capabilities.
  await signOut(page);
  await signIn(page, AUDITOR);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const auditorTools = page.getByRole('dialog', { name: 'Manage your server' });
  await expect(auditorTools).toBeVisible();
  const auditorTabs = auditorTools.getByRole('navigation', { name: 'Server administration' });
  await expect(auditorTabs.getByRole('button', { name: 'Maintenance' })).toBeVisible();
  for (const absent of ['People', 'Channels', 'Roles', 'Settings', 'Invitations']) {
    await expect(auditorTabs.getByRole('button', { name: absent })).toHaveCount(0);
  }
  // The recorded activity is rendered, and the surfaces the account may not use are
  // absent rather than present-and-failing. Actions are displayed with their dots
  // replaced by spaces.
  await expect(auditorTools.getByText('Recent activity')).toBeVisible();
  await expect(auditorTools.getByText('user created').first()).toBeVisible();
  await expect(auditorTools.getByRole('button', { name: 'Create backup' })).toHaveCount(0);
  await expect(auditorTools.getByText('Setup checks')).toHaveCount(0);
  // The health grid's own labels, not the sentence that happens to mention
  // connections in the heading every account sees.
  await expect(auditorTools.getByText('In voice')).toHaveCount(0);
  await expect(auditorTools.getByText('Go heap')).toHaveCount(0);
  await auditorTools.getByRole('button', { name: 'Close dialog' }).click();

  // The owner still sees the whole panel, so the narrowing above is about the
  // delegated accounts rather than a broken administration surface.
  await signOut(page);
  await signIn(page, OWNER);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const ownerTools = page.getByRole('dialog', { name: 'Manage your server' });
  const ownerTabs = ownerTools.getByRole('navigation', { name: 'Server administration' });
  for (const present of ['People', 'Invitations', 'Channels', 'Roles', 'Settings', 'Maintenance']) {
    await expect(ownerTabs.getByRole('button', { name: present })).toBeVisible();
  }
  // The audit log cannot be switched off, so the settings form states the rule
  // instead of offering a control the server would refuse.
  await ownerTabs.getByRole('button', { name: 'Settings' }).click();
  await expect(ownerTools.getByLabel('Audit log (always on)')).toBeDisabled();
  await ownerTools.getByRole('button', { name: 'Close dialog' }).click();
});

test('removing an account keeps its messages and leaves no reactivation controls', async ({
  page,
  request
}) => {
  const manager = { username: 'lifecycle_manager', password: 'a lifecycle manager passphrase' };
  const member = { username: 'lifecycle_member', password: 'a lifecycle member passphrase' };
  await signInOverApi(request);
  const managerID = await createAccount(request, manager);
  const memberID = await createAccount(request, member);
  const role = await createRole(request, 'lifecycle managers', [
    'channel.read',
    'message.send',
    'member.manage'
  ]);
  await assignOnly(request, managerID, [role]);
  await signInOverApi(request, member);
  const state = await (await request.get('/api/state')).json();
  const channel = state.channels.find((item) => item.type === 'text');
  const note = 'This message stays when its author leaves.';
  expect(
    (
      await request.post(`/api/channels/${channel.id}/messages`, {
        data: { content: note, idempotency_key: crypto.randomUUID() }
      })
    ).status()
  ).toBe(201);

  await signIn(page, manager);
  const setup = await page.request.post('/api/me/mfa/setup', {
    data: { password: manager.password }
  });
  expect(setup.ok()).toBe(true);
  const { secret } = await setup.json();
  const { totpCode } = await import('./support.mjs');
  expect(
    (await page.request.post('/api/me/mfa/enable', { data: { code: totpCode(secret) } })).ok()
  ).toBe(true);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  await tools
    .getByRole('navigation', { name: 'Server administration' })
    .getByRole('button', { name: 'People', exact: true })
    .click();
  const memberRow = tools.locator('.admin-row').filter({ hasText: `@${member.username}` });
  await memberRow.locator('summary').click();
  await expect(memberRow.getByRole('button', { name: 'Deactivate', exact: true })).toBeVisible();
  await memberRow
    .getByRole('button', { name: 'Remove account, keep messages', exact: true })
    .click();
  await expect(tools.getByText(/their content may still identify the author/)).toBeVisible();
  const form = tools
    .locator('form.admin-form')
    .filter({ hasText: 'Remove account, keep messages:' });
  await expect(
    form.getByRole('button', { name: 'Remove account, keep messages', exact: true })
  ).toBeDisabled();
  await form
    .getByRole('textbox', { name: `Type ${member.username} to confirm` })
    .fill(member.username);
  await form.getByRole('button', { name: 'Remove account, keep messages', exact: true }).click();
  await expect(tools.getByText('Account updated.', { exact: true })).toBeVisible();
  const deletedRow = tools
    .locator('.admin-row')
    .filter({ hasText: 'Account removed · Messages kept' });
  await expect(deletedRow.getByText('Deleted member', { exact: true })).toBeVisible();
  if ((await deletedRow.locator('details').getAttribute('open')) === null)
    await deletedRow.locator('summary').click();
  for (const name of ['Reactivate', 'Reset password', 'Roles and authority', 'Sign out everywhere'])
    await expect(deletedRow.getByRole('button', { name, exact: true })).toHaveCount(0);
  await expect(
    deletedRow.getByRole('button', { name: 'Erase account and messages' })
  ).toBeVisible();
  await tools.getByRole('button', { name: 'Close dialog' }).click();
  await expect(
    page.getByRole('log', { name: 'Channel messages' }).getByText(note, { exact: true })
  ).toBeVisible();
  const messages = await (await page.request.get(`/api/channels/${channel.id}/messages`)).json();
  expect(messages.some((message) => message.user_id === memberID && message.content === note)).toBe(
    true
  );
  expect((await request.post('/api/login', { data: member })).status()).toBe(401);
});

test('channel slow mode is configurable and a refused send keeps its text for retry', async ({
  page,
  request
}) => {
  const member = { username: 'paced_member', password: 'a paced member passphrase' };
  await signInOverApi(request);
  await createAccount(request, member);
  const created = await request.post('/api/channels', { data: { name: 'paced', type: 'text' } });
  expect(created.ok()).toBe(true);
  const channel = await created.json();
  await signIn(page, OWNER);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  await tools
    .getByRole('navigation', { name: 'Server administration' })
    .getByRole('button', { name: 'Channels', exact: true })
    .click();
  const row = tools.locator('.admin-row').filter({ has: page.getByText('paced', { exact: true }) });
  await row.locator('summary').click();
  await row.getByRole('button', { name: 'Edit channel', exact: true }).click();
  await tools.getByRole('spinbutton', { name: /Time between messages/ }).fill('30');
  await tools.getByRole('button', { name: 'Save channel', exact: true }).click();
  await expect(tools.getByText('Channel updated.', { exact: true })).toBeVisible();
  await tools.getByRole('button', { name: 'Close dialog' }).click();
  await signOut(page);
  await signIn(page, member);
  await page.goto(`/?channel=${channel.id}`);
  const composer = page.getByRole('textbox', { name: 'Message paced', exact: true });
  await expect(page.getByText('Slow mode: 30s between messages')).toBeVisible();
  await composer.fill('First paced message');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  const log = page.getByRole('log', { name: 'Channel messages' });
  await expect(log.getByText('First paced message', { exact: true })).toBeVisible();
  await expect(log.getByText('Sending…', { exact: true })).toHaveCount(0);
  await composer.fill('Keep this second paced message');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  await expect(log.getByRole('button', { name: /Message not sent.*Retry/ })).toBeVisible();
  await expect(page.getByText(/Slow mode is on. Wait/)).toBeVisible();
  expect(
    (await request.patch(`/api/channels/${channel.id}`, { data: { slow_mode_seconds: 0 } })).ok()
  ).toBe(true);
  await log.getByRole('button', { name: /Message not sent.*Retry/ }).click();
  await expect(log.getByRole('button', { name: /Message not sent.*Retry/ })).toHaveCount(0);
  await expect(log.getByText('Keep this second paced message', { exact: true })).toHaveCount(1);
});
