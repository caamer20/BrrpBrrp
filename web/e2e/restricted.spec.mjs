import { expect } from '@playwright/test';
import { createAccount, OWNER, signIn, signInOverApi, test } from './support.mjs';

test('restricted channel grants, inspector and live revocation match the shipped defaults', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  await signInOverApi(request);
  const reader = { username: 'restricted_reader', password: 'restricted reader test passphrase' };
  const outsider = {
    username: 'restricted_outsider',
    password: 'restricted outsider test passphrase'
  };
  const readerID = await createAccount(request, reader);
  await createAccount(request, outsider);
  const created = await request.post('/api/channels', {
    data: { name: 'restricted-room', type: 'text', visibility: 'private' }
  });
  expect(created.status()).toBe(201);
  const channel = await created.json();
  const state = await (await request.get('/api/state')).json();
  if (process.env.BRRP_E2E_SHIPPED_DEFAULTS === '1')
    expect(state.features.private_conversations).toBe(true);

  const readerContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const outsiderContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  try {
    const readerPage = await readerContext.newPage();
    const outsiderPage = await outsiderContext.newPage();
    await signIn(readerPage, reader);
    await signIn(outsiderPage, outsider);
    const link = (target) =>
      target
        .locator('nav.channel-navigation button.channel-link')
        .filter({ hasText: 'restricted-room' });
    await expect(link(readerPage)).toHaveCount(0);
    await expect(link(outsiderPage)).toHaveCount(0);

    await signIn(page, OWNER);
    await page.getByRole('button', { name: 'Manage server', exact: true }).click();
    const tools = page.getByRole('dialog', { name: 'Manage your server' });
    await tools.getByRole('button', { name: 'Channels', exact: true }).click();
    const row = tools
      .locator('article.admin-row')
      .filter({ has: page.locator('strong', { hasText: 'restricted-room' }) });
    await row.locator('summary').click();
    await row.getByRole('button', { name: 'Who can access this?', exact: true }).click();
    const access = tools.locator('form').filter({
      has: page.getByRole('heading', { name: 'Who can access #restricted-room?', exact: true })
    });
    await expect(access.getByRole('combobox', { name: 'Visibility', exact: true })).toHaveValue(
      'private'
    );
    await expect(access.getByRole('button', { name: 'Save access', exact: true })).toBeEnabled();
    await expect(
      access.getByText('Community managers can read every community channel', { exact: false })
    ).toBeVisible();
    await access.getByRole('checkbox', { name: reader.username, exact: true }).check();
    await access.getByRole('button', { name: 'Save access', exact: true }).click();
    await expect(access.getByText('Access updated.', { exact: true })).toBeVisible();
    const inspected = await (await request.get(`/api/channels/${channel.id}/access`)).json();
    expect(inspected.restricted_enabled).toBe(true);
    expect(inspected.users).toContain(readerID);
    expect(inspected.effective_members).toContain(reader.username);
    expect(inspected.effective_members).not.toContain(outsider.username);
    await tools.getByRole('button', { name: 'Close dialog', exact: true }).click();

    await expect(link(readerPage)).toBeVisible();
    await link(readerPage).click();
    const secret = 'Visible only to the granted restricted-channel reader';
    await readerPage
      .getByRole('textbox', { name: 'Message restricted-room', exact: true })
      .fill(secret);
    await readerPage.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(
      readerPage.getByRole('log', { name: 'Channel messages' }).getByText(secret)
    ).toBeVisible();
    await expect(link(outsiderPage)).toHaveCount(0);
    const hidden = await outsiderContext.request.get(`/api/channels/${channel.id}/messages`);
    expect(hidden.status()).toBe(404);

    const revoked = await request.put(`/api/channels/${channel.id}/access`, {
      data: { visibility: 'private', user_ids: [], role_ids: [] }
    });
    expect(revoked.ok()).toBe(true);
    await expect(link(readerPage)).toHaveCount(0);
    await expect(readerPage.getByText(secret)).toHaveCount(0);
    expect((await readerContext.request.get(`/api/channels/${channel.id}/messages`)).status()).toBe(
      404
    );
  } finally {
    await readerContext.close();
    await outsiderContext.close();
    const removed = await request.delete(`/api/channels/${channel.id}`, {
      data: { confirm: channel.name }
    });
    expect(removed.ok()).toBe(true);
  }
});
