import { expect, test } from '@playwright/test';
import { createAccount, signIn, signInOverApi, signOut } from './support.mjs';

// This suite runs only when the harness explicitly opts its disposable server into
// private conversations. It does not enable the feature in shipped configuration.
test('direct consent isolates messages and blocking recovers pending text without replay', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const alice = { username: 'direct_alice', password: 'a private alice passphrase' };
  const bob = { username: 'direct_bob', password: 'a private bob passphrase' };
  const outsider = { username: 'direct_outsider', password: 'a private outsider passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const bobID = await createAccount(request, bob);
  await createAccount(request, outsider);
  const bobContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const bobPage = await bobContext.newPage();
  const relative = process.env.BRRP_E2E_BASE_URL;
  // Explicit base URL for a manually created browser context.
  await bobPage.goto(relative);
  let releaseHeld;
  try {
    await signIn(page, alice);
    await page.getByRole('button', { name: 'Messages and requests', exact: true }).click();
    const inbox = page.getByRole('dialog', { name: 'Messages', exact: true });
    await inbox.getByRole('button', { name: 'New conversation', exact: true }).click();
    await inbox.getByRole('searchbox', { name: 'Find a person' }).fill(bob.username);
    await inbox.getByRole('button', { name: 'Request conversation', exact: true }).click();
    await inbox
      .getByRole('textbox', { name: 'Introduction (optional)' })
      .fill('Would you like to chat privately?');
    await inbox.getByRole('button', { name: 'Send request', exact: true }).click();
    await expect(inbox.getByText('Waiting for them to accept')).toBeVisible();
    const waiting = await (await page.request.get('/api/directs')).json();
    const direct = waiting.find((item) => item.peer.id === bobID);
    expect(direct.request_state).toBe('pending');
    expect(direct.can_read).toBe(false);
    expect((await page.request.get(`/api/conversations/${direct.id}/messages`)).status()).toBe(404);
    expect(
      (
        await page.request.post(`/api/conversations/${direct.id}/messages`, {
          data: { content: 'must not send before consent', idempotency_key: crypto.randomUUID() }
        })
      ).status()
    ).toBe(404);
    await inbox.getByRole('button', { name: 'Close dialog' }).click();

    await signIn(bobPage, bob);
    await bobPage.getByRole('button', { name: 'Messages and requests', exact: true }).click();
    const bobInbox = bobPage.getByRole('dialog', { name: 'Messages', exact: true });
    await bobInbox.getByRole('button', { name: 'Requests', exact: true }).click();
    await expect(
      bobInbox.getByText('Would you like to chat privately?', { exact: true })
    ).toBeVisible();
    await bobInbox.getByRole('button', { name: 'Accept request', exact: true }).click();
    await bobInbox.getByRole('button', { name: 'Conversations', exact: true }).click();
    await bobInbox.getByRole('button', { name: 'Open conversation', exact: true }).click();
    const bobLog = bobPage.getByRole('log', { name: 'Direct messages', exact: true });
    await expect(bobLog).toBeVisible();
    await page
      .locator('nav.channel-navigation button.channel-link')
      .filter({ hasText: bob.username })
      .click();
    const aliceLog = page.getByRole('log', { name: 'Direct messages', exact: true });
    const secret = 'A direct message only Alice and Bob may read';
    await page.getByRole('textbox', { name: `Message ${bob.username}`, exact: true }).fill(secret);
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(aliceLog.locator('.message-content').filter({ hasText: secret })).toBeVisible();
    await expect(bobLog.locator('.message-content').filter({ hasText: secret })).toBeVisible();
    const shared = await (
      await page.request.get(`/api/conversations/${direct.id}/messages`)
    ).json();
    const saved = shared.find((message) => message.content === secret);
    expect(saved.conversation_id).toBe(direct.id);
    expect(saved.channel_id || '').toBe('');
    await expect(aliceLog.getByRole('button', { name: 'Report message', exact: true })).toHaveCount(
      0
    );

    // Replies and scoped search reuse the existing conversation components/endpoints.
    await aliceLog
      .locator('article.message')
      .filter({ hasText: secret })
      .getByRole('button', { name: 'Reply to message', exact: true })
      .click();
    await page
      .getByRole('textbox', { name: `Message ${bob.username}`, exact: true })
      .fill('A private reply through the shared composer');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(
      bobLog.getByText('A private reply through the shared composer', { exact: true })
    ).toBeVisible();
    await aliceLog
      .locator('article.message')
      .filter({ has: page.getByText(secret, { exact: true }) })
      .first()
      .getByRole('button', { name: 'Open reply thread', exact: true })
      .click();
    const thread = page.getByRole('dialog', { name: 'Reply thread', exact: true });
    await expect(
      thread.getByText('A private reply through the shared composer', { exact: true })
    ).toBeVisible();
    await thread.getByRole('button', { name: 'Close dialog' }).click();
    await page.getByRole('button', { name: 'Search messages', exact: true }).click();
    const search = page.getByRole('dialog', { name: 'Search messages', exact: true });
    await search.getByRole('textbox', { name: 'Search', exact: true }).fill('Alice');
    await search.getByRole('button', { name: 'Search', exact: true }).click();
    await expect(search.getByText('1 message found')).toBeVisible();
    await expect(search.getByText(secret, { exact: true })).toBeVisible();
    await search.getByRole('button', { name: 'Close dialog' }).click();

    // The installation owner is not an accepted participant and has no bypass.
    for (const path of [
      `/api/conversations/${direct.id}/messages`,
      `/api/messages/${saved.id}`,
      `/api/messages/${saved.id}/context`,
      `/api/messages/${saved.id}/replies`
    ])
      expect((await request.get(path)).status(), path).toBe(404);
    const ownerSearch = await (
      await request.get(`/api/search?q=${encodeURIComponent(secret)}`)
    ).json();
    expect(ownerSearch.some((message) => message.id === saved.id)).toBe(false);
    await signInOverApi(request, outsider);
    expect((await request.get(`/api/conversations/${direct.id}/messages`)).status()).toBe(404);
    expect((await request.get('/api/directs')).status()).toBe(200);
    expect(await (await request.get('/api/directs')).json()).toEqual([]);

    // Use the same upload/send pipeline, including binding claims to this conversation.
    await page.locator('input[type=file][aria-label="Choose attachments"]').setInputFiles({
      name: 'private-note.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('private attached text')
    });
    await expect(page.getByRole('button', { name: 'Remove private-note.txt' })).toBeVisible();
    await page
      .getByRole('textbox', { name: `Message ${bob.username}`, exact: true })
      .fill('A private attachment');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(bobLog.getByRole('link', { name: /private-note.txt/ })).toBeVisible();
    const fileLink = await bobLog
      .getByRole('link', { name: /private-note.txt/ })
      .getAttribute('href');
    expect((await request.get(fileLink)).status()).toBe(404);

    // A held request belongs to the old writing authority. Blocking keeps accepted
    // history readable, ends retries, and offers the author's own text for recovery.
    await page.getByRole('button', { name: 'Your preferences' }).click();
    const preferences = page.getByRole('dialog', { name: 'Your preferences' });
    await preferences
      .getByRole('checkbox', { name: /Remember drafts and unsent messages/ })
      .check();
    await preferences.getByRole('button', { name: 'Close dialog' }).click();
    let captured;
    const ready = new Promise((resolve) => {
      captured = resolve;
    });
    const held = new Promise((resolve) => {
      releaseHeld = resolve;
    });
    let posts = 0;
    await page.route(`**/api/conversations/${direct.id}/messages`, async (route) => {
      if (route.request().method() !== 'POST') return route.continue();
      posts++;
      captured();
      await held;
      await route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'This conversation is read-only.' })
      });
    });
    const unsent = 'Keep my unsent direct text without automatically resending it';
    await page.getByRole('textbox', { name: `Message ${bob.username}`, exact: true }).fill(unsent);
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await ready;
    await bobPage.getByRole('button', { name: 'Block person', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Send message', exact: true })).toBeDisabled();
    await expect(aliceLog.locator('.message-content').filter({ hasText: secret })).toBeVisible();
    releaseHeld();
    await expect(
      page.getByRole('textbox', { name: 'Recovered message text', exact: true })
    ).toHaveValue(unsent);
    await expect(aliceLog.getByRole('button', { name: /Retry/ })).toHaveCount(0);
    expect(posts).toBe(1);
    await bobPage.getByRole('button', { name: 'Unblock person', exact: true }).click();
    await expect(
      page.getByRole('textbox', { name: 'Recovered message text', exact: true })
    ).toHaveValue(unsent);
    await page.reload();
    await expect(
      page.getByRole('textbox', { name: 'Recovered message text', exact: true })
    ).toHaveValue(unsent);
    expect(posts).toBe(1);
    const after = await (
      await bobPage.request.get(`/api/conversations/${direct.id}/messages`)
    ).json();
    expect(after.some((message) => message.content === unsent)).toBe(false);
    await page
      .getByRole('textbox', { name: `Message ${bob.username}`, exact: true })
      .fill('A deliberately new send after unblocking');
    await page.unroute(`**/api/conversations/${direct.id}/messages`);
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(
      bobLog.getByText('A deliberately new send after unblocking', { exact: true })
    ).toBeVisible();

    // Sign-out removes the old account's private local recovery scope.
    await signOut(page);
    await signIn(page, outsider);
    await expect(
      page.getByRole('textbox', { name: 'Recovered message text', exact: true })
    ).toHaveCount(0);
    await expect(page.getByText(secret, { exact: true })).toHaveCount(0);
    expect(aliceID).not.toBe(bobID);
  } finally {
    releaseHeld?.();
    await bobContext.close();
  }
});
