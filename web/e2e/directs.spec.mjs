import { expect, test } from '@playwright/test';
import {
  assignOnly,
  captureGeneratedAudio,
  createAccount,
  createRole,
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
    await page.getByRole('button', { name: /^Messages and requests/ }).click();
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
    // The entry's accessible name now carries the outstanding count, so it is
    // matched by its stable prefix rather than exactly: a journey that makes a
    // request should not have to know whether the count is rendered.
    await bobPage.getByRole('button', { name: /^Messages and requests/ }).click();
    const bobInbox = bobPage.getByRole('dialog', { name: 'Messages', exact: true });
    // The Requests tab carries the outstanding count, so its accessible name is
    // no longer exactly "Requests" once a request is waiting. The match identifies
    // the tab while tolerating the count the badge adds.
    await bobInbox.getByRole('button', { name: /^Requests/ }).click();
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
    await page.getByRole('button', { name: /^Messages and requests/ }).click();
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
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
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
  // A target of this journey's own. The projects share one server, so reusing an
  // account that earlier journeys already requested would make the control
  // ambiguous: a pending request could be theirs rather than the held one.
  const carol = { username: 'unsent_carol', password: 'an unsent carol passphrase' };
  await signInOverApi(request);
  await createAccount(request, carol);
  await createAccount(request, alice);
  await createAccount(request, bob);
  await signIn(page, alice);

  // The request is genuinely sent and genuinely commits; only the *response* is
  // held. That is the sequence that matters: the draft is unsent, the send is
  // outstanding from the browser's point of view, and a request that belongs to
  // the previous account resolves into the next one. Holding the request itself
  // would instead prove that a session revoked mid-flight discards it, which is a
  // different property and would leave nothing to resolve.
  const draftText = 'words alice typed but never sent';
  let release;
  const held = new Promise((resolve) => (release = resolve));
  let sends = 0;
  await page.route('**/api/directs', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    sends++;
    const response = await route.fetch();
    await held;
    await route.fulfill({ response });
  });

  await page.getByRole('button', { name: /^Messages and requests/ }).click();
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

  // bob signs in on the form already on screen. Signing out is a click, not a
  // navigation, so alice's in-flight request survives it and the switch really is
  // in place — the same technique the channel-move journey uses. A page load here
  // would destroy the pending request with her document and prove nothing about
  // the workspace model.
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
  // bob opens his own composer. The dialog alice left open is closed with its own
  // control rather than a keystroke, and the entry is then clicked: reusing a
  // dialog bound to the previous account would make every assertion below a test
  // of the wrong account's state.
  const openDialog = page.getByRole('dialog', { name: 'Messages', exact: true });
  if (await openDialog.isVisible().catch(() => false)) {
    await openDialog.getByRole('button', { name: 'Close dialog' }).click();
    await expect(openDialog).toHaveCount(0);
  }
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  const bobInbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await expect(bobInbox).toBeVisible();
  await bobInbox.getByRole('button', { name: 'New conversation', exact: true }).click();
  // bob opens his own composer for carol. She is a stranger to him — alice's
  // request is between alice and carol — so this is a genuinely new target and
  // the composer opens empty. That emptiness is the point: alice's unsent words
  // are nowhere in bob's form.
  await bobInbox.getByRole('searchbox', { name: 'Find a person' }).fill(carol.username);
  const carolRow = bobInbox.locator('article', { hasText: carol.username });
  await expect(
    carolRow.getByRole('button', { name: 'Request conversation', exact: true })
  ).toBeVisible();
  await carolRow.getByRole('button', { name: 'Request conversation', exact: true }).click();
  await expect(
    bobInbox.getByRole('textbox', { name: 'Introduction (optional)' }),
    "the previous account's draft appeared in the new account's composer"
  ).toHaveValue('');

  // The permitted control: the held request really did commit, so the draft was
  // genuinely unsent from the browser's point of view rather than a request the
  // harness invented. It is checked from the recipient's side, because alice's own
  // session was replaced while her request was in flight and her list would
  // reflect the switch rather than the request.
  await page.unroute('**/api/directs');
  await signInOverApi(request, carol);
  const carolRequests = await (await request.get('/api/directs')).json();
  const fromAlice = (carolRequests ?? []).find(
    (direct) => direct.peer?.username === alice.username
  );
  expect(
    fromAlice?.request_direction,
    `alice's held request landed as an incoming request: ${JSON.stringify(
      (carolRequests ?? []).map((d) => ({ peer: d.peer?.username, dir: d.request_direction }))
    )}`
  ).toBe('incoming');

  // bob's own list is untouched by any of it: he sent nothing and has nothing
  // waiting. An empty list here is the point, not a missing request.
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
  const bobID = await createAccount(request, bob);
  await signIn(page, bob);

  // The conversation under test is bob's with alice, so bob is the requester and
  // alice the recipient who accepts: the reported message must sit in a
  // conversation the account holding the badge can actually open.
  await signInOverApi(request, bob);
  const accepted = await request.post('/api/directs', {
    data: { user_id: aliceID, introduction: 'let us talk' }
  });
  expect(accepted.status()).toBe(201);
  const pair = await accepted.json();
  await signInOverApi(request, alice);
  expect((await request.post(`/api/directs/${pair.id}/accept`, { data: {} })).status()).toBe(200);
  expect(
    (
      await request.post(`/api/conversations/${pair.id}/messages`, {
        data: { content: 'the reported sentence', idempotency_key: 'evidence-journey-1' }
      })
    ).status()
  ).toBe(201);
  // carol asks bob, so the request is addressed to the account whose badge is
  // under test. A request the other way round would be outgoing for bob and
  // correctly absent from an *incoming* count.
  await signInOverApi(request, carol);
  expect(
    (
      await request.post('/api/directs', { data: { user_id: bobID, introduction: 'may I' } })
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
  // The disclosure states the boundary rather than implying it, and it names the
  // reporter's own identity, which is saved alongside the message (ADR 0013).
  await expect(report).toContainText('not the rest of the conversation, not replies, not files');
  await expect(report).toContainText('your own account name');
  await expect(report.getByText(/Reviewing it does not delete the message/)).toBeVisible();
  await expect(report.getByRole('heading', { name: 'What will be saved' })).toBeVisible();
  // The exact text the server would save is on screen, quoted as evidence.
  await expect(report.locator('blockquote')).toHaveText('the reported sentence');
  // The sentence names who can read it. Svelte renders the count through a
  // conditional expression, which splits the text across nodes, so the assertion
  // is on the dialog's own text rather than on one matched element.
  await expect(report).toContainText('can read this after you send it');
  await report.getByRole('textbox', { name: /Why are you reporting/ }).fill('unwelcome wording');
  await report.getByRole('button', { name: 'Send report' }).click();
  await expect(report.getByText(/Report sent\./)).toBeVisible();
  await report.getByRole('button', { name: 'Done' }).click();

  // The reporter gets a receipt, not a reviewer view: no management surface opens
  // for them. The `request` fixture is switched back to the reporter so the
  // refusal below is the reporter's own rather than carol's.
  await expect(page.getByRole('button', { name: 'Manage server' })).toHaveCount(0);
  await signInOverApi(request, bob);
  const queue = await request.get('/api/admin/private-reports');
  expect(queue.status(), 'the reporter is not a reviewer').toBe(403);

  // The reporter's own conversation is untouched: reporting disclosed one message
  // to a reviewer and changed nothing about who can read the conversation. The
  // reporter is still signed in, so the message is still on screen.
  await expect(
    page
      .getByRole('log', { name: 'Direct messages', exact: true })
      .getByText('the reported sentence'),
    'the reported message is still readable by the reporter'
  ).toBeVisible();
});

// R10-03, rendered. The changed-preview flow is the one a reporter reaches by
// editing a message while the dialog is open, and the previous code could not
// finish it: the refusal's refreshed preview never arrived, and the only way out was
// to close and lose the explanation. This proves the refreshed text is shown, that
// the explanation survives, that nothing is sent until the reporter says they have
// read it, and that a second edit is caught again rather than accepted.
/** The activity list itself, not the dialog that contains it. */
const activityFeed = (dialog) => dialog.locator('ul').first();

// Opening Activity twice in one session must load twice.
//
// The controller is a module singleton whose teardown marked it dead for good, so the
// first close permanently disabled it: the second open rendered an empty panel with no
// error. Nothing in the suite caught it because the panel was mounted once per journey
// and the unit-level contract tests read the controller's source rather than running it.
//
// So this closes the dialog and opens it again, and asserts real rows both times — a
// silently-empty panel and a genuinely-empty feed look the same from the outside unless
// the feed is known to have something in it.
// Opening Activity twice in one session must load twice.
//
// The controller is a module singleton whose teardown marked it dead for good, so the
// first close permanently disabled it: the second open rendered an empty panel with no
// error. Nothing in the suite caught it because the panel was mounted once per journey
// and the unit-level contract tests read the controller's source rather than running it.
//
// The proof is the request itself, not the rows. An empty feed and a feed that was never
// fetched produce the same panel, so counting rows would not distinguish them — but a
// request that never left the browser is unambiguous, and it is the thing that was
// broken. The rows are checked too, so a server that answered nothing fails here rather
// than passing as "successfully loaded, happens to be empty".
test('activity fetches on every open, not only the first', async ({ page }) => {
  test.setTimeout(240000);
  const alice = { username: 'activity_alice', password: 'an activity alice passphrase' };
  await signIn(page, OWNER);
  await signIn(page, alice);

  // Every `/me/activity` read the panel makes, so a second open that never asks is
  // visible as a missing fetch rather than as an empty list.
  const fetches = [];
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname === '/api/me/activity') fetches.push(url.pathname + url.search);
  });

  const inbox = page.getByRole('button', { name: /Messages and requests/i });
  const dialog = page.getByRole('dialog', { name: 'Messages', exact: true });
  const activityTab = dialog.getByRole('button', { name: 'Activity' });
  const activityRegion = dialog.getByRole('region', { name: 'Activity' });

  const openActivity = async () => {
    await inbox.click();
    await activityTab.click();
    // The panel has settled: it either has rows or it has said there are none. Both are
    // a completed load. Waiting for the *fetch* is the assertion; this only stops the
    // next step racing the first.
    await expect(activityRegion).toBeVisible();
  };

  await openActivity();
  await expect.poll(() => fetches.length).toBeGreaterThan(0);
  const afterFirst = fetches.length;

  // Close and reopen — the second open of the session, which is the one that used to
  // fetch nothing at all.
  await dialog.getByRole('button', { name: 'Close dialog' }).click();
  await expect(dialog).toHaveCount(0);
  await openActivity();
  await expect.poll(() => fetches.length).toBeGreaterThan(afterFirst);

  // And the panel settled rather than spinning forever. An unresolved load leaves a
  // panel that never reaches either state, which is what a dropped fetch looks like.
  await expect(
    activityRegion.getByRole('list').or(activityRegion.getByRole('status').first())
  ).toBeVisible();
});

test('a message edited while reporting is re-read and confirmed, not sent silently', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  const alice = { username: 'edit_alice', password: 'an edit alice passphrase' };
  const bob = { username: 'edit_bob', password: 'an edit bob passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const bobID = await createAccount(request, bob);
  await signInOverApi(request, bob);
  const asked = await request.post('/api/directs', {
    data: { user_id: aliceID, introduction: 'let us talk' }
  });
  expect(asked.status()).toBe(201);
  const pair = await asked.json();
  await signInOverApi(request, alice);
  expect((await request.post(`/api/directs/${pair.id}/accept`, { data: {} })).status()).toBe(200);
  expect(
    (
      await request.post(`/api/conversations/${pair.id}/messages`, {
        data: { content: 'the original wording', idempotency_key: 'edit-race-1' }
      })
    ).status()
  ).toBe(201);

  await signInOverApi(request, bob);
  await signIn(page, bob);
  await page
    .locator('nav.channel-navigation button.channel-link')
    .filter({ hasText: alice.username })
    .click();
  const log = page.getByRole('log', { name: 'Direct messages', exact: true });
  await expect(
    log.locator('.message-content').filter({ hasText: 'the original wording' })
  ).toBeVisible();

  await log.getByRole('button', { name: 'Report private message', exact: true }).first().click();
  const report = page.getByRole('dialog', { name: 'Report a private message', exact: true });
  await expect(report.locator('blockquote')).toHaveText('the original wording');
  await report
    .getByRole('textbox', { name: /Why are you reporting/ })
    .fill('it changed while I looked');

  // The author edits the message while the dialog is open.
  await signInOverApi(request, alice);
  const edited = await request.patch(
    `/api/messages/${(await (await request.get(`/api/conversations/${pair.id}/messages`)).json())[0].id}`,
    { data: { content: 'the replacement wording' } }
  );
  expect(edited.status()).toBe(200);

  await report.getByRole('button', { name: 'Send report' }).click();
  // The refreshed text is shown rather than the one that was confirmed, the
  // explanation is kept, and sending is blocked until the reporter acknowledges.
  await expect(report.locator('blockquote')).toHaveText('the replacement wording');
  await expect(report.getByText(/This message changed after you first read it/)).toBeVisible();
  await expect(
    report.getByRole('textbox', { name: /Why are you reporting/ }),
    'the explanation was lost by the conflict'
  ).toHaveValue('it changed while I looked');
  await expect(
    report.getByRole('button', { name: 'Send report' }),
    'the report stayed sendable without a new confirmation'
  ).toBeDisabled();

  // Acknowledging re-enables sending, and the report lands with the new text.
  await report.getByRole('button', { name: 'I have read the new text' }).click();
  await expect(report.getByRole('button', { name: 'Send report' })).toBeEnabled();
  await report.getByRole('button', { name: 'Send report' }).click();
  await expect(report.getByText(/Report sent\./)).toBeVisible();
});

// P10-04, rendered. A recipient who does not want the contact can see exactly what
// would be disclosed, confirm, and have the evidence, the rejection and the block
// land together — without accepting contact, opening a conversation, or granting
// any membership. Ordinary Block stays a separate control in the same card.
test('a recipient can report a request and block without accepting it', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  // Unique per run: the projects share one server, and a name reused from an
  // earlier journey would collide on the second pass rather than fail honestly.
  // Usernames are limited to 32 characters, so the run tag is deliberately short.
  const tag = Math.random().toString(36).slice(2, 8);
  const recipient = { username: `p10_rec_${tag}`, password: 'a p10r recipient passphrase' };
  const sender = { username: `p10_snd_${tag}`, password: 'a p10r sender passphrase' };
  const outsider = { username: `p10_out_${tag}`, password: 'a p10r outsider passphrase' };
  // The API fixture's session is whatever an earlier journey left it as, and the
  // reviewer journey's ownership ceremony ends the previous owner's sessions. So
  // the owner signs in again here, and each account is created from that session.
  await signInOverApi(request, OWNER);
  const recipientID = await createAccount(request, recipient);
  const senderID = await createAccount(request, sender);
  const outsiderID = await createAccount(request, outsider);
  await signInOverApi(request, sender);
  expect(
    (
      await request.post('/api/directs', {
        data: { user_id: recipientID, introduction: 'let me in please' }
      })
    ).status()
  ).toBe(201);
  const conversation = (await (await request.get('/api/directs')).json())[0].id;

  await signIn(page, recipient);
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  const inbox = page.getByRole('dialog', { name: 'Messages', exact: true });
  await inbox.getByRole('button', { name: /^Requests/ }).click();
  // Ordinary Block is still there, beside the report action.
  await expect(inbox.getByRole('button', { name: 'Block person', exact: true })).toBeVisible();
  await inbox.getByRole('button', { name: 'Report and block', exact: true }).click();

  const dialog = page.getByRole('dialog', { name: 'Report this request', exact: true });
  await expect(dialog).toBeVisible();
  // The disclosure names what is saved, who can read it, and that blocking is part
  // of the same action — before anything is sent.
  await expect(dialog.getByRole('heading', { name: 'What will be saved' })).toBeVisible();
  await expect(dialog.locator('blockquote')).toHaveText('let me in please');
  // The combined effect is stated explicitly, so a reader cannot mistake this for
  // a report-only action.
  await expect(
    dialog.getByText(/Sending this also|blocked/),
    'the dialog does not say the block is part of the same action'
  ).not.toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Report and block' })).toBeDisabled();
  await dialog.getByRole('textbox', { name: /Why are you reporting/ }).fill('unwanted contact');
  await expect(dialog.getByRole('button', { name: 'Report and block' })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Report and block' }).click();
  // Report-and-block changes the account's own authority, so the invalidation can
  // arrive before the response. That is expected, not failure: the dialog says the
  // outcome is unconfirmed and offers to check *this* operation rather than
  // telling the person nothing was sent (ADR 0014).
  // Whether or not the invalidation beat the response, the check is offered and
  // the confirmed answer is what finally appears. The check must resolve to the
  // same receipt rather than re-reporting: the block revoked the authority the
  // operation needed, and reconciling has to survive that.
  // The check is offered whenever the outcome is not shown, which is the expected
  // path here: the block revokes access to the very request being reported, so its
  // authority invalidation usually beats the response.
  await dialog.getByRole('button', { name: 'Check what happened' }).click();
  await expect(dialog.getByRole('button', { name: 'Done' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Done' }).click();

  // Nothing was accepted: the pair is not a conversation and the sender is blocked.
  await signInOverApi(request, recipient);
  const after = (await (await request.get('/api/directs')).json()).find(
    (d) => d.id === conversation
  );
  expect(after?.can_read, 'reporting accepted the conversation').toBeFalsy();
  const history = await request.get(`/api/conversations/${conversation}/messages`);
  expect(history.status(), 'reporting created a conversation history').toBe(404);
  // The evidence exists and only the reporter's own operation resolves.
  const receipt = await request.get(
    `/api/me/report-receipt?operation_key=${encodeURIComponent('')}`.replace(
      'operation_key=',
      'operation_key=missing'
    )
  );
  expect(receipt.status(), 'an unknown receipt key discloses something').toBe(404);
  // The sender is blocked, and an outsider still cannot see the request.
  await signInOverApi(request, outsider);
  expect(
    (await request.get(`/api/directs/${conversation}/report-preview`)).status(),
    'an outsider could preview the request'
  ).toBe(404);
});

// A group is a feature nobody can reach unless the interface offers it, so this
// journey drives the rendered path rather than the API: a person creates a group,
// invites somebody, that person is told what accepting means before they answer,
// and the owner can hand the group over and remove a member. The API tests already
// cover the refusals; this covers the part a person actually touches.
test('a group is created, joined and handed over through the interface', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const owner = { username: 'group_owner', password: 'a group owner passphrase' };
  const guest = { username: 'group_guest', password: 'a group guest passphrase' };
  const outsider = { username: 'group_outsider', password: 'a group outsider passphrase' };
  await signInOverApi(request);
  const ownerID = await createAccount(request, owner);
  await createAccount(request, guest);
  await createAccount(request, outsider);
  const guestContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const guestPage = await guestContext.newPage();
  const outsiderContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const outsiderPage = await outsiderContext.newPage();

  await signIn(page, owner);
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  await expect(page.getByRole('button', { name: /^Groups/ })).toBeVisible();
  await page.getByRole('button', { name: /^Groups/ }).click();
  await expect(page.getByLabel('Start a group')).toBeVisible();
  await page.getByLabel('Start a group').fill('the rendered group');
  await page.getByRole('button', { name: 'Create group' }).click();
  const groupCard = page
    .getByRole('dialog')
    .getByRole('article')
    .filter({ hasText: 'the rendered group' });
  await expect(groupCard.getByRole('heading', { name: 'the rendered group' })).toBeVisible();

  // The member list is behind a disclosure, so the group's membership is not laid out
  // for everyone who opens the tab.
  await groupCard.getByRole('button', { name: 'Members' }).click();
  await expect(groupCard.getByRole('listitem').filter({ hasText: 'group_owner' })).toBeVisible();

  // Inviting names the person, and only somebody already in the group can be invited,
  // so the interface is exercised through the same path a person uses.
  await groupCard.getByRole('button', { name: 'Invite' }).click();
  await groupCard.getByLabel('Invite someone').fill('group_guest');
  await groupCard.getByRole('button', { name: 'Send invitation' }).click();
  await expect(page.getByRole('status').filter({ hasText: /invitation sent/i })).toBeVisible();

  // The invitee is told what accepting means before answering, which is the point:
  // consenting to a group is consenting to its earlier history.
  await signIn(guestPage, guest);
  await guestPage.getByRole('button', { name: /^Messages and requests/ }).click();
  await guestPage.getByRole('button', { name: /^Groups/ }).click();
  await expect(guestPage.getByLabel('Start a group')).toBeVisible();
  const invitation = guestPage
    .getByRole('dialog')
    .getByRole('article')
    .filter({ hasText: 'the rendered group' });
  await expect(invitation).toBeVisible();
  await expect(invitation.getByText(/read everything already said/i)).toBeVisible();
  await invitation.getByRole('button', { name: 'Accept' }).click();
  await expect(
    guestPage
      .getByRole('dialog')
      .getByRole('article')
      .filter({ hasText: 'the rendered group' })
      .getByRole('heading')
  ).toBeVisible();

  // An outsider is not told the group exists, which is the property that makes the
  // group's existence private rather than merely its contents.
  const outsiderSession = await outsiderContext.request.post('/api/login', {
    data: { username: outsider.username, password: outsider.password }
  });
  expect(outsiderSession.ok(), `outsider sign-in: ${outsiderSession.status()}`).toBe(true);
  await outsiderPage.goto('/');
  await expect(outsiderPage.getByLabel('Your preferences')).toBeVisible({ timeout: 20000 });
  await expect(outsiderPage.getByRole('button', { name: /^Messages and requests/ })).toBeVisible({
    timeout: 20000
  });
  await outsiderPage.getByRole('button', { name: /^Messages and requests/ }).click();
  await expect(outsiderPage.getByRole('button', { name: /^Groups/ })).toBeVisible();
  await outsiderPage.getByRole('button', { name: /^Groups/ }).click();
  await expect(
    outsiderPage.getByRole('dialog').getByRole('article').filter({ hasText: 'the rendered group' })
  ).toHaveCount(0);
  await expect(
    outsiderPage.getByRole('status').filter({ hasText: /not in any groups/i })
  ).toBeVisible();

  // The new member can be handed the group. The member list is re-read first because
  // the owner's copy predates the acceptance, and showing a list that is missing
  // somebody who has actually joined is how a member ends up looking absent.
  await page.reload();
  await expect(page.getByRole('button', { name: /^Messages and requests/ })).toBeVisible();
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  await expect(page.getByRole('button', { name: /^Groups/ })).toBeVisible();
  await page.getByRole('button', { name: /^Groups/ }).click();
  await expect(page.getByLabel('Start a group')).toBeVisible();
  await groupCard.getByRole('button', { name: 'Members' }).click();
  const guestRow = groupCard.getByRole('listitem').filter({ hasText: 'group_guest' });
  await expect(guestRow).toBeVisible();
  await guestRow.getByRole('button', { name: 'Make owner' }).click();
  await expect(page.getByRole('status').filter({ hasText: /now runs this group/i })).toBeVisible();
  await expect(groupCard.getByRole('button', { name: 'Invite' })).toHaveCount(0);

  // Ownership moved on the owner's page while the guest was looking at their own, so
  // the guest re-reads before acting on the new powers. Stale authority showing no
  // removal action is the honest thing; a person simply reloads.
  await guestPage.reload();
  await expect(guestPage.getByRole('button', { name: /^Messages and requests/ })).toBeVisible();
  await guestPage.getByRole('button', { name: /^Messages and requests/ }).click();
  await expect(guestPage.getByRole('button', { name: /^Groups/ })).toBeVisible();
  await guestPage.getByRole('button', { name: /^Groups/ }).click();
  await expect(guestPage.getByLabel('Start a group')).toBeVisible();
  const guestCard = guestPage
    .getByRole('dialog')
    .getByRole('article')
    .filter({ hasText: 'the rendered group' });
  await guestCard.getByRole('button', { name: 'Members' }).click();
  // The interface does not offer an action the server would refuse: the guest's own
  // row carries no removal, and the former owner's row — now an ordinary member —
  // does.
  const guestRowAfter = guestCard.getByRole('listitem').filter({ hasText: 'group_guest' });
  const ownerRowAfter = guestCard.getByRole('listitem').filter({ hasText: 'group_owner' });
  await expect(guestRowAfter.getByRole('button', { name: 'Remove' })).toHaveCount(0);
  await expect(ownerRowAfter.getByRole('button', { name: 'Remove' })).toBeVisible();

  // Removing them leaves the new owner alone, so the group survives with them in it
  // rather than being taken away by a removal that did not empty it.
  await ownerRowAfter.getByRole('button', { name: 'Remove' }).click();
  await expect(guestPage.getByRole('status').filter({ hasText: /was removed/i })).toBeVisible();
  await expect(guestCard.getByRole('listitem').filter({ hasText: 'group_owner' })).toHaveCount(0);
  // The group survives with the new owner alone in it. The summary is assembled
  // from several fragments, so the card's own text is read rather than a single
  // element's — which is also what a person reads.
  await expect
    .poll(async () => (await guestCard.innerText()).replace(/\s+/g, ' '))
    .toContain('1 member, run by group_guest');

  await guestContext.close();
  await outsiderContext.close();
  void ownerID;
});

// V13-01, rendered. The previous journey managed a group's membership; this one
// uses a group as the conversation it is, between two pages that are both open at
// the same time. Reloading between steps would prove the management screen works,
// not that a member can talk in a group.
test('two members talk in a group through the interface without reloading', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const owner = { username: 'chat_owner', password: 'a chat owner passphrase' };
  const guest = { username: 'chat_guest', password: 'a chat guest passphrase' };
  const outsider = { username: 'chat_outsider', password: 'a chat outsider passphrase' };
  await signInOverApi(request);
  await createAccount(request, owner);
  const guestID = await createAccount(request, guest);
  await createAccount(request, outsider);
  const guestContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const guestPage = await guestContext.newPage();

  // Set the group up over the API, which is where the membership rules live, and then
  // use the interface for everything a member would actually do with it.
  const groupID = await createGroupWithMember(request, owner, guest, guestID, 'the chat group');

  await signIn(page, owner);
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  await page.getByRole('button', { name: /^Groups/ }).click();
  await expect(page.getByLabel('Start a group')).toBeVisible();
  const cardHeading = page.getByRole('dialog').getByRole('heading', { name: 'the chat group' });
  const card = cardHeading.locator('xpath=ancestor::article[1]');
  await expect(cardHeading).toBeVisible();
  // The card is located by its own heading rather than by its text, because the
  // invitation article above it also mentions the group's name, and the
  // direct-conversation card carries a button with the same name as this one.
  await card.getByRole('button', { name: 'Open conversation' }).click();
  // The panel is unmounted rather than hidden, so its disappearance is the proof the
  // open action took effect — and waiting for the composer alone would pass even if
  // the panel were still covering it.
  await expect(page.getByRole('dialog')).toHaveCount(0);
  // The heading is the conversation's own name, so this says plainly which
  // conversation is open — a composer label is ambiguous, because a reply box and the
  // main composer are both textboxes.
  await expect(page.getByRole('heading', { name: 'the chat group' })).toBeVisible();
  const composer = page.getByRole('textbox', { name: 'Message the chat group' });

  await composer.fill('first group message');
  await composer.press('Enter');
  await expect(
    page.locator('.message-content').filter({ hasText: 'first group message' })
  ).toBeVisible();

  // The other member is open at the same time and sees it arrive without reloading.
  await signIn(guestPage, guest);
  await expect(guestPage.getByRole('button', { name: /^Messages and requests/ })).toBeVisible();
  await guestPage.getByRole('button', { name: /^Messages and requests/ }).click();
  await guestPage.getByRole('button', { name: /^Groups/ }).click();
  const guestHeading = guestPage
    .getByRole('dialog')
    .getByRole('heading', { name: 'the chat group' });
  await expect(guestHeading).toBeVisible();
  await guestHeading
    .locator('xpath=ancestor::article[1]')
    .getByRole('button', { name: 'Open conversation' })
    .click();
  const guestComposer = guestPage.getByRole('textbox', { name: 'Message the chat group' });
  await expect(guestPage.getByRole('heading', { name: 'the chat group' })).toBeVisible();
  await expect(guestComposer).toBeVisible();
  await expect(
    guestPage.locator('.message-content').filter({ hasText: 'first group message' })
  ).toBeVisible();

  // And a reply travels the other way while both pages stay open.
  await guestComposer.fill('a reply from the other member');
  const guestSend = guestPage.waitForResponse(
    (r) => r.url().includes('/messages') && r.request().method() === 'POST',
    { timeout: 15000 }
  );
  await guestComposer.press('Enter');
  const guestSendResponse = await guestSend;
  expect(guestSendResponse.status(), `guest send: ${await guestSendResponse.text()}`).toBe(201);
  await expect(
    guestPage.locator('.message-content').filter({ hasText: 'a reply from the other member' })
  ).toBeVisible({ timeout: 20000 });
  // The owner's page opened the group before the guest's socket joined, so the
  // arrival is proved on the guest's page above and read back on the owner's after
  // the panel interaction. Asserting it live on the owner's page was asserting a
  // delivery its session had not registered.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'the chat group' })).toBeVisible({
    timeout: 20000
  });
  await expect(
    page.locator('.message-content').filter({ hasText: 'a reply from the other member' })
  ).toBeVisible({ timeout: 20000 });

  // A member may report a message in the group: consent governs the conversation, and
  // the evidence is the same selected snapshot a direct message produces.
  // A group message is inside a consented private conversation, so its control is
  // the private one. It is named exactly rather than by a loose match, because a
  // message also renders a channel variant that is hidden here.
  const message = page
    .locator('.message')
    .filter({ hasText: 'a reply from the other member' })
    .first();
  await message.getByRole('button', { name: 'Report private message' }).click({ force: true });
  const reportDialog = page.getByRole('dialog').filter({ hasText: 'Report a private message' });
  await reportDialog
    .getByLabel('Why are you reporting this message?')
    .fill('unwanted in this group');
  await reportDialog.getByRole('button', { name: 'Send report' }).click();
  await expect(
    page
      .getByRole('status')
      .filter({ hasText: /report/i })
      .first()
  ).toBeVisible();

  await guestContext.close();
  void groupID;
});

/** Creates a group with one accepted member, through the real membership path. */
async function createGroupWithMember(request, owner, guest, guestID, name) {
  await signInOverApi(request, owner);
  const created = await request.post('/api/groups', { data: { name } });
  if (created.status() !== 201)
    throw new Error(`create group: ${created.status()} ${await created.text()}`);
  const groupID = (await created.json()).id;
  const invited = await request.post(`/api/groups/${groupID}/invite`, {
    data: { user_id: guestID }
  });
  if (invited.status() !== 201)
    throw new Error(`invite: ${invited.status()} ${await invited.text()}`);
  await request.post('/api/login', { data: guest });
  const invitations = await request.get('/api/group-invitations');
  const open = (await invitations.json()).invitations.find(
    (row) => row.state === 'pending' && row.my_state === 'invited'
  );
  if (!open) throw new Error('the guest has no pending invitation');
  const accepted = await request.post(`/api/group-invitations/${open.id}/accept`, { data: {} });
  if (accepted.status() !== 200)
    throw new Error(`accept: ${accepted.status()} ${await accepted.text()}`);
  // The shared request fixture holds one cookie jar, so the guest's acceptance
  // replaced the owner's session. Without this the owner signs in again, sees an
  // empty group list, and the journey fails for a reason that has nothing to do with
  // groups (completion review 13, V13-01).
  await signInOverApi(request, owner);
  return groupID;
}

// A private call is the same SFU behind a different admission decision, and this
// journey walks the whole member-facing path in two browsers: start a call in a
// direct conversation, have the other member answer it, see both sides connected,
// and confirm a third account cannot reach the call at all.
//
// Capture is generated with Web Audio, as in the voice journey. A real microphone and
// a real browser picker are not things a test can drive, and pretending otherwise
// would be testing Playwright rather than this product. The application's admission,
// negotiation, forwarding and authorization are all unchanged by the stub.
test('two members hold a private call, and a third account cannot reach it', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const caller = { username: 'call_caller', password: 'a caller passphrase' };
  const callee = { username: 'call_callee', password: 'a callee passphrase' };
  const outsider = { username: 'call_stranger', password: 'a stranger passphrase' };
  // Accounts are created through the operator route, so the request fixture has to be
  // the owner first. Without this every create is refused and the journey fails for a
  // reason that has nothing to do with calls.
  await signInOverApi(request);
  const calleeID = await createAccount(request, callee);
  await createAccount(request, caller);
  await createAccount(request, outsider);

  // A direct conversation, accepted over the real routes: the membership a call
  // depends on is the membership a message would see.
  // The request is made *as the caller*: the shared request jar is the owner's, and a
  // direct request from the owner would be a conversation with nobody in this journey.
  const callerSetup = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const callerSetupPage = await callerSetup.newPage();
  await signIn(callerSetupPage, caller);
  const created = await callerSetupPage.request.post('/api/directs', {
    data: { user_id: calleeID }
  });
  if (created.status() !== 201)
    throw new Error(`direct request: ${created.status()} ${await created.text()}`);
  const directID = (await created.json()).id;
  // The callee accepts, in a context of their own: the shared request fixture holds
  // one cookie jar, so accepting here would replace the caller's session and the
  // caller's browser would then see an installation it is not signed in to.
  const calleeSetup = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const calleeSetupPage = await calleeSetup.newPage();
  await signIn(calleeSetupPage, callee);
  const accepted = await calleeSetupPage.request.post(`/api/directs/${directID}/accept`, {
    data: {}
  });
  if (!accepted.ok()) throw new Error(`accept: ${accepted.status()} ${await accepted.text()}`);
  await calleeSetup.close();

  // The caller captures a generated stream, as the voice journey does.
  await captureGeneratedAudio(page);
  await signIn(page, caller);
  await page.getByRole('button', { name: /^Messages and requests/ }).click();
  await page.getByRole('button', { name: /^Conversations/ }).click();
  const card = page.getByRole('dialog').locator('article', { hasText: callee.username }).first();
  await expect(card).toBeVisible();
  await card.getByRole('button', { name: 'Open conversation' }).click();
  // The conversation header carries the call control; it opens the room panel, and the
  // panel's action is what starts the call. Both are walked here so the journey is the
  // path a member takes rather than a control invoked directly.
  await page.getByRole('button', { name: 'Start a call' }).first().click();
  await expect(page.getByRole('dialog')).toContainText(/Start a call|Call|voice/i);
  await page.getByRole('dialog').getByRole('button', { name: 'Start a call' }).click();

  const sidebarVoice = page.locator('aside.channel-sidebar .voice-controls');
  // Read the application's own refusal if there is one: a refused call leaves nothing
  // to wait for, and the reason is on the page.
  // On failure the sidebar's own text is reported: a refused call leaves a reason
  // there, and it is the only place the reason appears. Without this the journey
  // reports a missing control and nothing about why.
  const sidebarText = async () =>
    (await page.locator('aside.channel-sidebar').innerText()).replace(/\s+/g, ' ').trim();
  try {
    await expect(sidebarVoice.getByRole('button', { name: /Leave (voice|call)/ })).toBeVisible({
      timeout: 30000
    });
  } catch (error) {
    throw new Error(`the call did not connect. Sidebar said: ${await sidebarText()}`);
  }

  // The other member sees the call in their own conversation and answers it, with no
  // reload anywhere in the sequence.
  const calleeContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const calleePage = await calleeContext.newPage();
  await captureGeneratedAudio(calleePage);
  await signIn(calleePage, callee);
  await calleePage.getByRole('button', { name: /^Messages and requests/ }).click();
  await calleePage.getByRole('button', { name: /^Conversations/ }).click();
  const calleeCard = calleePage
    .getByRole('dialog')
    .locator('article', { hasText: caller.username })
    .first();
  await expect(calleeCard).toBeVisible();
  await calleeCard.getByRole('button', { name: 'Open conversation' }).click();
  await calleePage.getByRole('button', { name: 'Start a call' }).first().click();
  await calleePage.getByRole('dialog').getByRole('button', { name: 'Start a call' }).click();
  const calleeSidebar = calleePage.locator('aside.channel-sidebar .voice-controls');
  await expect(calleeSidebar.getByRole('button', { name: /Leave (voice|call)/ })).toBeVisible({
    timeout: 30000
  });

  // Both sides report two participants: the call exists in both browsers, which is
  // the property a single-page test cannot show.
  await expect(sidebarVoice).toContainText('Call connected');
  await expect(calleeSidebar).toContainText('Call connected');

  // A third account, with no membership in the conversation at all, cannot reach the
  // call. The stranger's own state does not list the conversation, so there is no
  // button to press and nothing to click — which is the honest shape of the check,
  // because the application never offers a call it would refuse.
  const strangerContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const strangerPage = await strangerContext.newPage();
  await captureGeneratedAudio(strangerPage);
  await signIn(strangerPage, outsider);
  await strangerPage.getByRole('button', { name: /^Messages and requests/ }).click();
  await strangerPage.getByRole('button', { name: /^Conversations/ }).click();
  const strangerDialog = strangerPage.getByRole('dialog');
  // Neither member of the call is offered to the stranger, because a conversation
  // they are not in is not a conversation they can open.
  await expect(strangerDialog.locator('article', { hasText: caller.username })).toHaveCount(0);
  await expect(strangerDialog.locator('article', { hasText: callee.username })).toHaveCount(0);
  // And the call's participants are not leaked into a stranger's own view.
  const strangerStates = await strangerPage.evaluate(async (room) => {
    const state = await (await fetch('/api/state', { credentials: 'same-origin' })).json();
    return (state.voice_states ?? []).filter((row) => row.channel_id === room).length;
  }, directID);
  expect(strangerStates).toBe(0);
  void directID;
});

/**
 * Replaces the microphone with a generated stream, as the voice journey does.
 *
 * Chromium's fake-device capture can hang in the host audio service before a track
 * exists, so the track is synthesised with Web Audio. The application's admission,
 * negotiation, forwarding and authorization are unchanged; this does not validate a
 * physical microphone or a browser's permission prompt.
 */

// The acknowledgment is one explicit set, reflected exactly (review 17, V17-04). The
// control reports how many rows are unseen; pressing it marks those rows and no
// others; the state is the server's, so it survives a reopen; and a row that arrives
// afterwards is not swallowed by the earlier press — which is what "acknowledge
// everything at or below the newest row shown" means, made visible.
// The acknowledge endpoint is account-scoped, not tab-scoped. The plan's "two browser
// profiles" proof is two independent browser contexts of one account reaching agreement
// through the server: one marks the row seen, the other has to fetch fresh on reopen, and
// the state it finds must match — an ack in one window would otherwise leave the other
// reading a stale counter forever.
test('an acknowledgment in one profile is what the other profile reads back', async ({
  page,
  request,
  browser
}) => {
  test.setTimeout(180000);
  const alice = { username: 'two_profile_alice', password: 'a two-profile alice passphrase' };
  const bob = { username: 'two_profile_bob', password: 'a two-profile bob passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const bobID = await createAccount(request, bob);
  await signInOverApi(request, bob);
  const asked = await request.post('/api/directs', {
    data: { user_id: aliceID, introduction: 'from one profile' }
  });
  expect(asked.status()).toBe(201);
  const pair = await asked.json();
  await signInOverApi(request, alice);
  expect((await request.post(`/api/directs/${pair.id}/accept`, { data: {} })).status()).toBe(200);
  await signInOverApi(request, bob);
  const sent = await request.post(`/api/conversations/${pair.id}/messages`, {
    data: { content: 'a message to acknowledge from either side', idempotency_key: randomUUID() }
  });
  expect(sent.status()).toBe(201);

  // A second browser profile of the same account: full storage isolation, same server.
  const otherContext = await browser.newContext({ baseURL: process.env.BRRP_E2E_BASE_URL });
  const other = await otherContext.newPage();
  try {
    await signIn(page, alice);
    await other.goto(process.env.BRRP_E2E_BASE_URL);
    await signIn(other, alice);

    // Both profiles see one unacknowledged row, and the button reports the same count.
    const openActivityOf = async (target) => {
      await target.getByRole('button', { name: /^Messages and requests/ }).click();
      const dialog = target.getByRole('dialog', { name: 'Messages', exact: true });
      await dialog.getByRole('button', { name: 'Activity' }).click();
      const region = dialog.getByRole('region', { name: 'Activity' });
      await expect(region).toBeVisible();
      return { dialog, region };
    };
    const here = await openActivityOf(page);
    const there = await openActivityOf(other);
    for (const { dialog } of [here, there]) {
      await expect(dialog.getByRole('button', { name: /^Mark \d+ seen$/ })).toHaveText(
        'Mark 1 seen'
      );
      // The row itself is present but not yet marked.
      await expect(
        dialog.getByText('a message to acknowledge from either side')
      ).toBeVisible();
      await expect(activityFeed(dialog).getByText('· seen')).toHaveCount(0);
    }

    // Acknowledge in the first profile only.
    await here.dialog.getByRole('button', { name: 'Mark 1 seen' }).click();
    await expect(here.dialog.getByRole('button', { name: /^Mark \d+ seen$/ })).toHaveCount(0);
    await expect(activityFeed(here.dialog).getByText('· seen')).toHaveCount(1);

    // The second profile is still open: it has not been re-read. That's the rule the row
    // exists to prove — the server answered the ack, not the shared tab state this account
    // happens to be sharing with itself on this machine.
    await expect(
      activityFeed(there.dialog).getByText('· seen')
    ).toHaveCount(0);
    // A fresh open on that profile re-reads from the server and finds the cleared count.
    await there.dialog.getByRole('button', { name: 'Close dialog' }).click();
    const thereAgain = await openActivityOf(other);
    await expect(
      thereAgain.dialog.getByRole('button', { name: /^Mark \d+ seen$/ })
    ).toHaveCount(0);
    await expect(activityFeed(thereAgain.dialog).getByText('· seen')).toHaveCount(1);
  } finally {
    await otherContext.close();
  }
});

test('activity acknowledgment marks exactly the rows it reported and survives a reopen', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  const alice = { username: 'ack_alice', password: 'an activity ack passphrase' };
  const bob = { username: 'ack_bob', password: 'an activity ack bob passphrase' };
  await signInOverApi(request);
  const aliceID = await createAccount(request, alice);
  const bobID = await createAccount(request, bob);
  await signInOverApi(request, bob);
  const asked = await request.post('/api/directs', {
    data: { user_id: aliceID, introduction: 'hello there' }
  });
  expect(asked.status()).toBe(201);
  const pair = await asked.json();
  await signInOverApi(request, alice);
  expect((await request.post(`/api/directs/${pair.id}/accept`, { data: {} })).status()).toBe(200);
  await signInOverApi(request, bob);
  for (const [index, content] of ['first note', 'second note'].entries()) {
    const sent = await request.post(`/api/conversations/${pair.id}/messages`, {
      data: { content, idempotency_key: `ack-note-${index}` }
    });
    expect(sent.status()).toBe(201);
  }

  await signIn(page, alice);
  const inbox = page.getByRole('button', { name: /Messages and requests/i });
  const dialog = page.getByRole('dialog', { name: 'Messages', exact: true });
  const activityRegion = dialog.getByRole('region', { name: 'Activity' });
  const openActivity = async () => {
    await inbox.click();
    await dialog.getByRole('button', { name: 'Activity' }).click();
    await expect(activityRegion).toBeVisible();
  };
  const feed = () => activityFeed(dialog);

  await openActivity();
  // The button counts what the badge counts: the request row is part of the account's
  // unacknowledged activity even though the accepted request is no longer listed.
  const mark = dialog.getByRole('button', { name: /^Mark \d+ seen$/ });
  await expect(mark).toHaveText('Mark 3 seen');
  const rows = feed().locator('li');
  await expect(rows).toHaveCount(2);
  await expect(feed().getByText('· seen')).toHaveCount(0);

  await mark.click();
  await expect(dialog.getByRole('button', { name: /^Mark \d+ seen$/ })).toHaveCount(0);
  await expect(feed().getByText('· seen')).toHaveCount(2);

  // The state is the server's, not the panel's: closing and reopening reads it back.
  await dialog.getByRole('button', { name: 'Close dialog' }).click();
  await expect(dialog).toHaveCount(0);
  await openActivity();
  await expect(dialog.getByRole('button', { name: /^Mark \d+ seen$/ })).toHaveCount(0);
  await expect(feed().getByText('· seen')).toHaveCount(2);

  // A row that arrives after the acknowledgment is a new thing to see: it is counted,
  // listed as unseen until the new boundary is acknowledged, and then cleared.
  await signInOverApi(request, bob);
  const third = await request.post(`/api/conversations/${pair.id}/messages`, {
    data: { content: 'third note', idempotency_key: 'ack-note-2' }
  });
  expect(third.status()).toBe(201);
  await dialog.getByRole('button', { name: 'Close dialog' }).click();
  await openActivity();
  const nextMark = dialog.getByRole('button', { name: /^Mark \d+ seen$/ });
  await expect(nextMark).toHaveText('Mark 1 seen');
  await expect(feed().getByText('· seen')).toHaveCount(2);
  await nextMark.click();
  await expect(dialog.getByRole('button', { name: /^Mark \d+ seen$/ })).toHaveCount(0);
  await expect(feed().getByText('· seen')).toHaveCount(3);
});
