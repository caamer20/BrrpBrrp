import { expect, test } from '@playwright/test';
import {
  createAccount,
  OWNER,
  permissionsOf,
  setRolePermissions,
  signIn,
  signInOverApi,
  signOut
} from './support.mjs';

// Reactions to an invalidation are observed, never guessed: watchResponse yields a
// promise for the page's next GET of a URL fragment, and settled waits through the
// two render flushes Svelte needs to apply the response. Assertions then run only
// after the reload they belong to has finished.
function watchResponse(page, fragment) {
  return page.waitForResponse(
    (response) => response.url().includes(fragment) && response.request().method() === 'GET'
  );
}

async function flushRenders(page) {
  await page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  );
}

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
    // A direct message is reported through the private-evidence dialog, which has
    // its own disclosure contract (ADR 0013), so the community report control is
    // still absent here and the private one is what the person is offered.
    await expect(aliceLog.getByRole('button', { name: 'Report message', exact: true })).toHaveCount(
      0
    );
    await expect(
      aliceLog.getByRole('button', { name: 'Report private message', exact: true })
    ).toBeVisible();

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

// R8-02: mention privileges/notification preferences belong to the account, not to
// a mounted community-channel header. Before the fix, an account that stayed in a
// direct conversation — a fresh deep link, or an authority refresh while there —
// never reloaded its preferences, and allowed community mentions were silently
// withheld. Each positive assertion below failed before the repair.
test('mention policy loads while staying in a direct conversation', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const alice = { username: 'notify_alice', password: 'a notify alice passphrase' };
  const bob = { username: 'notify_bob', password: 'a notify bob passphrase' };
  const carol = { username: 'notify_carol', password: 'a notify carol passphrase' };
  await signInOverApi(request);
  const bobID = await createAccount(request, bob);
  await createAccount(request, carol);
  await createAccount(request, alice);
  const bobContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const bobPage = await bobContext.newPage();
  try {
    // An accepted direct conversation between alice and bob, arranged by API.
    await signIn(page, alice);
    const created = await page.request.post('/api/directs', {
      data: { user_id: bobID, introduction: 'share a private word' }
    });
    expect(created.status()).toBe(201);
    const direct = await created.json();
    await signInOverApi(request, bob);
    expect((await request.post(`/api/directs/${direct.id}/accept`, { data: {} })).status()).toBe(
      200
    );
    await signInOverApi(request, OWNER);
    const channels = await (await page.request.get('/api/channels')).json();
    const channel = channels.find((entry) => entry.type === 'text');
    const roles = await (await request.get('/api/roles')).json();
    const member = roles.find((entry) => entry.key === 'member');

    // Phase 1: a fresh deep link straight into the direct conversation. The policy
    // fetch belongs to the account and fires with no channel header on screen.
    const deepLinkPolicy = watchResponse(page, '/api/me/channel-notifications');
    await page.goto(`/?conversation=${direct.id}`);
    const aliceLog = page.getByRole('log', { name: 'Direct messages', exact: true });
    await expect(aliceLog).toBeVisible();
    await deepLinkPolicy;
    await flushRenders(page);

    await signIn(bobPage, bob);
    const bobComposer = bobPage.getByRole('textbox', {
      name: `Message ${channel.name}`,
      exact: true
    });
    await expect(bobComposer).toBeVisible();
    await bobComposer.fill('@notify_alice deep link ping');
    await bobPage.getByRole('button', { name: 'Send message', exact: true }).click();
    const mentionToast = page.getByText('notify_bob mentioned you', { exact: true });
    await expect(mentionToast).toBeVisible();
    // The toast expires after six seconds; wait for it to pass so later phases are
    // unambiguous.
    await expect(mentionToast).toHaveCount(0, { timeout: 15000 });

    // Phase 2: an authority refresh while staying in the DM. The account's policy is
    // verified again without visiting a community channel, and the live mention that
    // follows reaches the client instead of being dropped by the old unready state.
    const refreshedPolicy = watchResponse(page, '/api/me/channel-notifications');
    await setRolePermissions(request, member.id, await permissionsOf(request, member.id));
    await refreshedPolicy;
    await flushRenders(page);
    await bobComposer.fill('@notify_alice authority ping');
    await bobPage.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(mentionToast).toBeVisible();
    await expect(mentionToast).toHaveCount(0, { timeout: 15000 });

    // Phase 3: a muted control stays quiet. The server withholds the mention event
    // for a muted channel entirely; the DM beacon afterwards orders the client's
    // event processing so that "no toast" is asserted after the mention would have
    // arrived, not before.
    await page
      .locator('nav.channel-navigation button.channel-link')
      .filter({ hasText: channel.name })
      .first()
      .click();
    await page
      .getByRole('button', {
        name: `Mute mention notifications for #${channel.name}`,
        exact: true
      })
      .click();
    await expect(page.getByText('Mention notifications muted', { exact: false })).toBeVisible();
    await page
      .locator('nav.channel-navigation button.channel-link')
      .filter({ hasText: bob.username })
      .click();
    await expect(aliceLog).toBeVisible();
    await bobComposer.fill('@notify_alice muted ping');
    await bobPage.getByRole('button', { name: 'Send message', exact: true }).click();
    await bobPage
      .locator('nav.channel-navigation button.channel-link')
      .filter({ hasText: alice.username })
      .click();
    await bobPage
      .getByRole('textbox', { name: `Message ${alice.username}`, exact: true })
      .fill('muted-phase beacon');
    await bobPage.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(
      aliceLog.locator('.message-content').filter({ hasText: 'muted-phase beacon' })
    ).toBeVisible();
    await expect(mentionToast).toHaveCount(0);

    // Phase 4: switching accounts never inherits the previous account's policy. Carol
    // never muted the channel, and her mention arrives.
    await signOut(page);
    await signIn(page, carol);
    await expect(page.getByText('notify_bob mentioned you', { exact: true })).toHaveCount(0);
    await bobPage
      .locator('nav.channel-navigation button.channel-link')
      .filter({ hasText: channel.name })
      .first()
      .click();
    await bobComposer.fill('@notify_carol switch ping');
    await bobPage.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(page.getByText('notify_bob mentioned you', { exact: true })).toBeVisible();
  } finally {
    await bobContext.close();
  }
});

// R8-03: an unsent request introduction is the user's own text, not fetched contact
// data. Before the fix, any contact or authority event cleared the form. The journey
// keeps the draft across an unrelated incoming request, an authority refresh, a
// deactivation that forbids the target, a refused send after the target blocks, and
// clears it only after an admitted send or a real account switch.
test('an unsent request introduction survives unrelated refreshes and a refused send', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  const alice = { username: 'draft_alice', password: 'a draft alice passphrase' };
  const bob = { username: 'draft_bob', password: 'a draft bob passphrase' };
  const carol = { username: 'draft_carol', password: 'a draft carol passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const carolID = await createAccount(request, carol);
  await createAccount(request, bob);
  await signIn(page, alice);
  const introText = 'Please keep my words across refreshes';

  const openComposer = async () => {
    await page.getByRole('button', { name: 'Messages and requests', exact: true }).click();
    const inbox = page.getByRole('dialog', { name: 'Messages', exact: true });
    await inbox.getByRole('button', { name: 'New conversation', exact: true }).click();
    await inbox.getByRole('searchbox', { name: 'Find a person' }).fill(carol.username);
    await inbox.getByRole('button', { name: 'Request conversation', exact: true }).click();
    return inbox;
  };
  let inboxState = await openComposer();
  let intro = inboxState.getByRole('textbox', { name: 'Introduction (optional)' });
  await intro.fill(introText);

  // The draft is never written to device storage: a reload has nothing to restore.
  await page.reload();
  inboxState = await openComposer();
  intro = inboxState.getByRole('textbox', { name: 'Introduction (optional)' });
  await expect(intro).toHaveValue('');
  await intro.fill(introText);
  const form = inboxState.getByRole('heading', {
    name: `Request a conversation with ${carol.username}`,
    exact: true
  });

  // An unrelated incoming request refreshes contacts; the draft for carol stays.
  const contactReload = watchResponse(page, '/api/me/blocks');
  await signInOverApi(request, bob);
  expect(
    (
      await request.post('/api/directs', {
        data: { user_id: aliceID, introduction: 'an unrelated request' }
      })
    ).status()
  ).toBe(201);
  await contactReload;
  await flushRenders(page);
  await expect(form).toBeVisible();
  await expect(intro).toHaveValue(introText);

  // An authority refresh revalidates without discarding the same account's text.
  const authorityReload = watchResponse(page, '/api/me/blocks');
  await signInOverApi(request, OWNER);
  const roles = await (await request.get('/api/roles')).json();
  const memberRole = roles.find((entry) => entry.key === 'member');
  await setRolePermissions(request, memberRole.id, await permissionsOf(request, memberRole.id));
  await authorityReload;
  await flushRenders(page);
  await expect(form).toBeVisible();
  await expect(intro).toHaveValue(introText);

  // The target's deactivation forbids the destination: the send control disables,
  // the reason is shown, and the copyable text is kept rather than redirected.
  await request.patch(`/api/admin/users/${carolID}`, {
    data: { is_deactivated: true, confirm: carol.username }
  });
  const disabledNotice = inboxState.getByRole('alert');
  await expect(disabledNotice).toContainText('no longer available');
  await expect(intro).toHaveValue(introText);
  await expect(
    inboxState.getByRole('button', { name: 'Send request', exact: true })
  ).toBeDisabled();

  // Reactivation removes the explanation and restores the same destination.
  await request.patch(`/api/admin/users/${carolID}`, {
    data: { is_deactivated: false, confirm: carol.username }
  });
  await expect(disabledNotice).toHaveCount(0);
  await expect(inboxState.getByRole('button', { name: 'Send request', exact: true })).toBeEnabled();

  // The peer's own block is not disclosed ahead of time: alice's client only learns
  // it through the refreshed contacts. Her next send is refused by the server, and
  // the refused attempt must keep both the draft and its destination.
  await signInOverApi(request, carol);
  const blockReload = watchResponse(page, '/api/me/blocks');
  expect((await request.put(`/api/me/blocks/${aliceID}`, { data: {} })).status()).toBe(200);
  await blockReload;
  await flushRenders(page);
  await expect(inboxState.getByRole('button', { name: 'Send request', exact: true })).toBeEnabled();
  await inboxState.getByRole('button', { name: 'Send request', exact: true }).click();
  await expect(inboxState.getByRole('alert')).toBeVisible();
  await expect(form).toBeVisible();
  await expect(intro).toHaveValue(introText);

  // Once the block lifts, the same draft sends: clearing happens only after the
  // admitted success, and the request then waits for acceptance.
  const unblockReload = watchResponse(page, '/api/me/blocks');
  expect((await request.delete(`/api/me/blocks/${aliceID}`, { data: {} })).status()).toBe(200);
  await unblockReload;
  await flushRenders(page);
  await inboxState.getByRole('button', { name: 'Send request', exact: true }).click();
  await expect(inboxState.getByText('Waiting for them to accept')).toBeVisible();
  await expect(form).toHaveCount(0);

  // A new account never inherits another account's unsent words: bob's fresh draft
  // for the same person starts empty. The inbox overlay must close first.
  await inboxState.getByRole('button', { name: 'Close dialog' }).click();
  await signOut(page);
  await signIn(page, bob);
  await page.getByRole('button', { name: 'Messages and requests', exact: true }).click();
  const bobInbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await bobInbox.getByRole('button', { name: 'New conversation', exact: true }).click();
  await bobInbox.getByRole('searchbox', { name: 'Find a person' }).fill(carol.username);
  await bobInbox.getByRole('button', { name: 'Request conversation', exact: true }).click();
  await expect(bobInbox.getByRole('textbox', { name: 'Introduction (optional)' })).toHaveValue('');
});

// R9-06, rendered. The journey above switches accounts only after a successful send
// has already cleared the draft, so it cannot show what happens to words that were
// never sent. This one holds the send itself in flight across the switch: the first
// account's introduction is still in the composer when the response is released, and
// the second account must see neither the text nor a result belonging to it.
test('an unsent request introduction and its held response never cross an account switch', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  const alice = { username: 'unsent_alice', password: 'an unsent alice passphrase' };
  const bob = { username: 'unsent_bob', password: 'an unsent bob passphrase' };
  const carol = { username: 'unsent_carol', password: 'an unsent carol passphrase' };
  await signInOverApi(request);
  const carolID = await createAccount(request, carol);
  await createAccount(request, alice);
  await createAccount(request, bob);
  await signIn(page, alice);

  // alice's send is held at the network layer rather than replaced: the route waits
  // and then forwards the real request, so the request genuinely commits once
  // released and the positive control below can prove the draft really was unsent
  // rather than a response the harness invented.
  const draftText = 'words alice typed but never sent';
  let release;
  const held = new Promise((resolve) => (release = resolve));
  let sends = 0;
  await page.route('**/api/directs', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    sends++;
    await held;
    await route.continue();
  });

  await page.getByRole('button', { name: 'Messages and requests', exact: true }).click();
  const inbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await inbox.getByRole('button', { name: 'New conversation', exact: true }).click();
  await inbox.getByRole('searchbox', { name: 'Find a person' }).fill(carol.username);
  await inbox.getByRole('button', { name: 'Request conversation', exact: true }).click();
  const intro = inbox.getByRole('textbox', { name: 'Introduction (optional)' });
  await intro.fill(draftText);
  await inbox.getByRole('button', { name: 'Send request', exact: true }).click();
  await expect.poll(() => sends).toBe(1);
  // The draft is still on screen, unsent, while the response is outstanding.
  await expect(intro).toHaveValue(draftText);

  // bob signs in in place, without a reload, so alice's in-flight send stays alive.
  await inbox.getByRole('button', { name: 'Close dialog' }).click();
  await signOut(page);
  await page.getByLabel('Username').fill(bob.username);
  await page.locator('input[autocomplete="current-password"]').fill(bob.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByLabel('Your preferences')).toBeVisible();

  // alice's response now arrives. It belongs to the previous account: it must not
  // clear bob's composer, add a conversation, or report a result in his view.
  const bobBefore = await page.getByText('Waiting for them to accept').count();
  release();
  await page.waitForTimeout(750);
  await flushRenders(page);
  await expect(page.getByText(draftText)).toHaveCount(0);
  await expect(
    page.getByText('Waiting for them to accept'),
    "the previous account's held request response reported a result to the new account"
  ).toHaveCount(bobBefore);

  // bob opens his own composer. The dialog is explicitly closed and reopened
  // rather than reused: alice left it open, and clicking the entry while the
  // inbox is showing does not re-open it, so the search below would run against
  // a dialog bound to the previous account.
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: 'Messages', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Messages and requests', exact: true }).click();
  const bobInbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await expect(bobInbox).toBeVisible();
  await bobInbox.getByRole('button', { name: 'New conversation', exact: true }).click();
  await bobInbox.getByRole('searchbox', { name: 'Find a person' }).fill(carol.username);
  // carol has no relationship with bob, so this is genuinely a new request target.
  await bobInbox.getByRole('button', { name: 'Request conversation', exact: true }).click();
  await expect(bobInbox.getByRole('textbox', { name: 'Introduction (optional)' })).toHaveValue('');

  // The permitted control: the held request really did commit, so the draft was
  // genuinely unsent from the browser's point of view rather than a request the
  // harness faked. carol now holds a pending request from alice.
  await page.unroute('**/api/directs');
  await signInOverApi(request, carol);
  const carolRequests = await (await request.get('/api/directs')).json();
  const fromAlice = (carolRequests ?? []).find(
    (direct) => direct.peer?.username === alice.username && direct.request_state === 'pending'
  );
  expect(fromAlice, 'the held request committed for carol').toBeTruthy();

  // And the same account's own draft still works afterwards, so the empty composer
  // above is isolation rather than a form that stopped accepting input.
  await signInOverApi(request, bob);
  const bobRequests = await (await request.get('/api/directs')).json();
  expect(
    (bobRequests ?? []).filter((direct) => direct.request_state === 'pending').length,
    "the previous account's request appeared in the new account's conversations"
  ).toBe(0);
});

// P9-04/P9-05, rendered. The badge and the private report are the two new member
// surfaces, and both are disclosure decisions rather than controls: what a person is
// shown, what they agree to, and what a reviewer can reach afterwards. The store
// tests cover the counting rules; this proves the disclosure is actually in front
// of the person, and that a reviewer gets the snapshot and nothing more.
test('a request badge appears for a waiting request and a private report discloses exactly one message', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  const alice = { username: 'evidence_alice', password: 'an evidence alice passphrase' };
  const bob = { username: 'evidence_bob', password: 'an evidence bob passphrase' };
  const carol = { username: 'evidence_carol', password: 'an evidence carol passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const carolID = await createAccount(request, carol);
  await createAccount(request, bob);
  await signIn(page, bob);

  // One accepted conversation with a message alice wrote, plus a pending request
  // from carol so the badge has something real to count.
  const accepted = await request.post('/api/directs', {
    data: { user_id: aliceID, introduction: 'let us talk' }
  });
  expect(accepted.status()).toBe(201);
  const pair = await accepted.json();
  expect((await request.post(`/api/directs/${pair.id}/accept`, { data: {} })).status()).toBe(200);
  expect(
    (
      await request.post(`/api/conversations/${pair.id}/messages`, {
        data: { content: 'the reported sentence', idempotency_key: 'evidence-journey-1' }
      })
    ).status()
  ).toBe(201);
  expect(
    (
      await request.post('/api/directs', { data: { user_id: carolID, introduction: 'may I' } })
    ).status()
  ).toBe(201);
  await page.reload();
  await expect(page.getByLabel('Your preferences')).toBeVisible();

  // The badge counts the pending request and names itself for a screen reader, so
  // the number is not only visual.
  const entry = page.getByRole('button', { name: /Messages and requests/ });
  await expect(entry).toHaveAttribute('aria-label', /1 waiting for you/);
  // Opening the dialog does not consume it: the request is still actionable.
  await entry.click();
  const inbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await expect(inbox).toBeVisible();
  await expect(inbox.getByText(/request is waiting for you/)).toBeVisible();
  await expect(page.getByRole('button', { name: /Messages and requests/ })).toHaveAttribute(
    'aria-label',
    /1 waiting for you/
  );
  await inbox.getByRole('button', { name: 'Close dialog' }).click();

  // Reporting the message shows the exact server text and what is excluded. The
  // reporter must be able to see the whole disclosure before agreeing to it.
  const log = page.getByRole('log', { name: 'Direct messages', exact: true });
  await page
    .locator('nav.channel-navigation button.channel-link')
    .filter({ hasText: alice.username })
    .click();
  await expect(
    log.locator('.message-content').filter({ hasText: 'the reported sentence' })
  ).toBeVisible();
  await log.getByRole('button', { name: 'Report private message', exact: true }).first().click();

  const report = page.getByRole('dialog', { name: 'Report a private message', exact: true });
  await expect(report).toBeVisible();
  // The disclosure states the boundary rather than implying it.
  await expect(
    report.getByText(/not the rest of the conversation, not replies, not\s+files/)
  ).toBeVisible();
  await expect(report.getByText(/Reviewing it does not delete the message/)).toBeVisible();
  await expect(report.getByRole('heading', { name: 'What will be saved' })).toBeVisible();
  // The exact text the server would save is on screen, quoted as evidence.
  await expect(report.locator('blockquote')).toHaveText('the reported sentence');
  // Reviewer count is computed, not asserted as a constant.
  await expect(report.getByText(/account\(s\) can read this after you send it/)).toBeVisible();
  await report.getByRole('textbox', { name: /Why are you reporting/ }).fill('unwelcome wording');
  await report.getByRole('button', { name: 'Send report' }).click();
  await expect(report.getByText(/Report sent\./)).toBeVisible();
  await report.getByRole('button', { name: 'Done' }).click();

  // The reporter gets a receipt, not a reviewer view: no management surface opens
  // for them, and their own count is unaffected by reporting.
  await expect(page.getByRole('button', { name: 'Manage server' })).toHaveCount(0);
  const queue = await request.get('/api/admin/private-reports');
  expect(queue.status(), 'the reporter is not a reviewer').toBe(403);

  // The owner holds the review capability, so the queue opens and shows the
  // snapshot — and offers no way into the conversation it came from.
  await signOut(page);
  await signIn(page, OWNER);
  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  await tools
    .getByRole('navigation', { name: 'Server administration' })
    .getByRole('button', { name: 'Private evidence' })
    .click();
  const evidence = tools.getByRole('article', { name: /Private evidence reported by/ });
  await expect(evidence).toBeVisible();
  await expect(evidence.locator('blockquote')).toHaveText('the reported sentence');
  await expect(evidence.getByText('unwelcome wording')).toBeVisible();
  // Resolving is triage: the wording is present, and the conversation is not
  // reachable from anywhere in this queue.
  await expect(tools.getByRole('link', { name: /conversation/ })).toHaveCount(0);
  await evidence.getByRole('textbox', { name: 'Review note (optional)' }).fill('reviewed');
  await evidence.getByRole('button', { name: 'Mark reviewed' }).click();
  await expect(tools.getByText('Marked as reviewed.')).toBeVisible();
  // The permitted control: the conversation itself is still completely private.
  const history = await request.get(`/api/conversations/${pair.id}/messages`);
  expect(history.status(), 'the owner still cannot read the conversation').toBe(404);
});
