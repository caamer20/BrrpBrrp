import { crc32, deflateSync } from 'node:zlib';
import { expect, test } from '@playwright/test';
import { createAccount, setRolePermissions, signIn, signInOverApi } from './support.mjs';

// The rendered half of R2-05. The store tests prove the model, but the claim under
// test is about what a person can still see, so this journey takes a real reading
// privilege away from a signed-in account and inspects the DOM: the messages, the
// quoted reply and the channel entry must all go, a response that was already in
// flight must not bring them back, and restoring the grant must let the same account
// read the conversation again without signing in or reloading.
const PEEKER = { username: 'peeker', password: 'a peeker passphrase' };
const SECRET = 'A private note the demoted reader must lose';
const PANEL_READER = { username: 'panel_reader', password: 'a panel reader passphrase' };
const OUTBOX_READER = { username: 'outbox_reader', password: 'an outbox reader passphrase' };
const OUTBOX_NOTE = 'A send whose delivery was never confirmed';
const PANEL_SECRET = 'Only members may read this pinned panel note';
// A real image, built here rather than pasted as base64: the server decodes previews with
// the standard library, which checks the checksums, so a plausible-looking blob is not
// enough for the conversation to render a preview a person can see.
function png(width = 64, height = 48) {
  const chunk = (type, data) => {
    const length = Buffer.alloc(4);
    length.writeUInt32BE(data.length, 0);
    const body = Buffer.concat([Buffer.from(type, 'latin1'), data]);
    const checksum = Buffer.alloc(4);
    checksum.writeUInt32BE(crc32(body), 0);
    return Buffer.concat([length, body, checksum]);
  };
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = 6; // RGBA
  const stride = 1 + width * 4;
  const pixels = Buffer.alloc(height * stride);
  for (let y = 0; y < height; y++)
    for (let x = 0; x < width; x++) {
      const at = y * stride + 1 + x * 4;
      pixels[at] = 0x2b;
      pixels[at + 1] = 0x6f;
      pixels[at + 2] = 0xd6;
      pixels[at + 3] = 0xff;
    }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(pixels)),
    chunk('IEND', Buffer.alloc(0))
  ]);
}
const PNG = png();

test('losing read access clears the page and no older response restores it', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  await signInOverApi(request);

  const state = await (await request.get('/api/state')).json();
  const general = state.channels.find((channel) => channel.type === 'text');
  expect(general?.name).toBe('general');
  const posted = await request.post(`/api/channels/${general.id}/messages`, {
    data: { content: SECRET, idempotency_key: 'privacy-journey-note' }
  });
  expect(posted.ok(), `posting the note: ${posted.status()}`).toBe(true);
  await createAccount(request, PEEKER);

  // An ordinary member reads the channel through the default role, which is the role
  // this journey narrows. Narrowing it rather than the account's own assignment keeps
  // the change to one fact: what every member may read.
  const roles = await (await request.get('/api/roles')).json();
  const memberRole = roles.find((role) => role.is_default);
  const granted = memberRole.permissions;
  expect(granted).toContain('channel.read');

  await signIn(page, PEEKER);
  const log = page.getByRole('log', { name: 'Channel messages' });
  await expect(log.getByText(SECRET)).toBeVisible();

  // A quoted reply renders the content it quotes, so it is restricted content too.
  const note = log
    .locator('article')
    .filter({ has: page.locator('.message-content').filter({ hasText: SECRET }) });
  await note.hover();
  await note.getByRole('button', { name: 'Reply to message' }).click();
  await expect(page.locator('.reply-composer')).toContainText(SECRET);

  // From here every history read is held back, so the response that arrives after the
  // revocation is one the server admitted while the account could still read.
  await page.route('**/api/channels/*/messages*', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await new Promise((resolve) => setTimeout(resolve, 3000));
    return route.continue();
  });
  await page.reload();
  await expect(page.getByRole('textbox', { name: 'Message general' })).toBeVisible();

  await setRolePermissions(
    request,
    memberRole.id,
    granted.filter((name) => name !== 'channel.read')
  );

  // The content on screen and the quoted reply are gone, and the channel itself is no
  // longer offered, before the held-back response can land.
  await expect(page.getByText(SECRET)).toHaveCount(0);
  await expect(page.locator('.reply-composer')).toHaveCount(0);
  const channelLink = page
    .locator('nav.channel-navigation button.channel-link')
    .filter({ hasText: 'general' });
  await expect(channelLink).toHaveCount(0);

  // That response has now landed. It carries content this account may no longer read,
  // so it must be dropped rather than rendered.
  await page.waitForTimeout(3500);
  await expect(page.getByText(SECRET)).toHaveCount(0);
  expect(await page.locator('.message-content').count()).toBe(0);

  // Restoring the grant is not a fresh sign-in and not a reload: the account recovers
  // by navigating, and the conversation it lost is readable again.
  await setRolePermissions(request, memberRole.id, granted);
  await expect(channelLink).toBeVisible({ timeout: 15000 });
  await channelLink.click();
  await expect(log.getByText(SECRET)).toBeVisible({ timeout: 15000 });
});

// The remaining rendered surfaces of the same claim. Search, pins and a reply thread keep
// their own answers, and a message can render an image preview of restricted content. Each
// of them is opened, is admitted an answer while the account may still read, and then the
// reading privilege is taken away while the refresh that would confirm the change fails.
// Nothing any of them answered may appear, in the moment of the revocation or when the
// held-back answer lands, and the same account must still recover by navigating.
//
// Two sessions again, and deliberately: the page is the account whose reading privilege is
// taken away, and the request context is the administrator who takes it — the only account
// that may pin. The journey above pays for the same pair.
test('held panel answers cannot repopulate pins, a thread, search or a preview', async ({
  page,
  request
}) => {
  test.setTimeout(300000);
  await signInOverApi(request);
  const state = await (await request.get('/api/state')).json();
  const general = state.channels.find((channel) => channel.type === 'text');
  expect(general?.name).toBe('general');

  await createAccount(request, PANEL_READER);
  const roles = await (await request.get('/api/roles')).json();
  const memberRole = roles.find((role) => role.is_default);
  const granted = memberRole.permissions;
  expect(granted).toContain('channel.read');

  // The account attaches and sends through the product itself, so the conversation holds a
  // rendered image preview of restricted content rather than an attachment the API merely
  // mentioned.
  await signIn(page, PANEL_READER);
  const log = page.getByRole('log', { name: 'Channel messages' });
  const composer = page.getByRole('textbox', { name: 'Message general' });
  await page.locator('input[type=file][aria-label="Choose attachments"]').setInputFiles({
    name: 'panel-note.png',
    mimeType: 'image/png',
    buffer: PNG
  });
  await expect(page.getByRole('button', { name: 'Remove panel-note.png' })).toBeVisible();
  await composer.fill(PANEL_SECRET);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(log.getByText(PANEL_SECRET).first()).toBeVisible();
  // The installation renders image previews behind a data-saver placeholder, so the surface
  // is revealed exactly the way a person reveals it.
  const preview = log.locator('.image-preview img');
  const revealPreview = log.getByRole('button', { name: 'Show image preview' });
  if (await revealPreview.count()) await revealPreview.click();
  await expect(preview).toBeVisible({ timeout: 20000 });
  await expect(log.getByText('Preview couldn’t load')).toHaveCount(0);
  const channelLink = page
    .locator('nav.channel-navigation button.channel-link')
    .filter({ hasText: 'general' });

  // Pinning is a moderation capability, so the administrator pins the note and the account
  // replies in its thread through the panel. The pin reaches the member's own view without a
  // reload, which is what gives the pins panel something to lose later.
  const messages = await (await request.get(`/api/channels/${general.id}/messages`)).json();
  const note = messages.find((message) => message.content === PANEL_SECRET);
  expect(note, 'the note the account sent is readable').toBeTruthy();
  const pinned = await request.put(`/api/messages/${note.id}/pin`, { data: { pinned: true } });
  expect(pinned.ok(), `pinning the note: ${pinned.status()}`).toBe(true);
  const article = log
    .locator('article')
    .filter({ has: page.locator('.message-content').filter({ hasText: PANEL_SECRET }) })
    .first();
  await expect(article.locator('.pin-label')).toBeVisible({ timeout: 15000 });
  await article.hover();
  await article.getByRole('button', { name: 'Open reply thread' }).click();
  const setupThread = page.getByRole('dialog', { name: 'Reply thread' });
  await expect(setupThread).toBeVisible();
  await setupThread
    .getByRole('textbox', { name: 'Reply in thread' })
    .fill(`${PANEL_SECRET} in the thread`);
  await setupThread.getByRole('button', { name: 'Send reply' }).click();
  await expect(setupThread.getByText(`${PANEL_SECRET} in the thread`)).toBeVisible({
    timeout: 15000
  });
  await setupThread.getByRole('button', { name: 'Close dialog' }).click();
  await expect(page.getByRole('dialog', { name: 'Reply thread' })).toHaveCount(0);

  // Every answer the three panels ask for is admitted by the server while this account may
  // still read the conversation and is then held back for longer than one assertion cycle, so
  // each of them is a response carrying restricted content that arrives after the privilege
  // is gone. That is the answer a revocation has to outlive, so it cannot be answered by the
  // server refusing it: the request is made first and only its delivery is delayed.
  for (const pattern of ['**/api/channels/*/pins*', '**/api/messages/*/replies*', '**/api/search*'])
    await page.route(pattern, async (route) => {
      const response = await route.fetch();
      const body = await response.text();
      await new Promise((resolve) => setTimeout(resolve, 4000));
      return route.fulfill({ response, body });
    });
  // The refresh that follows the access change fails too. It is the state where keeping a
  // stale view would be least visible, so it is the state the clearing has to survive.
  let brokenRefreshes = 0;
  const breakNextRefresh = (count = 2) => (brokenRefreshes = count);
  await page.route('**/api/state', async (route) => {
    if (brokenRefreshes <= 0) return route.continue();
    brokenRefreshes--;
    await new Promise((resolve) => setTimeout(resolve, 4000));
    return route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: '{"detail":"The workspace refresh could not be verified."}'
    });
  });

  const revoke = () =>
    setRolePermissions(
      request,
      memberRole.id,
      granted.filter((name) => name !== 'channel.read')
    );
  const restore = () => setRolePermissions(request, memberRole.id, granted);
  // Recovery is by navigating, which needs the panel that is still open to be closed first:
  // a modal dialog makes the rest of the page inert.
  const recover = async (panel) => {
    await restore();
    if (panel) await panel.getByRole('button', { name: 'Close dialog' }).click();
    await channelLink.click();
    await expect(log.getByText(PANEL_SECRET).first()).toBeVisible({ timeout: 30000 });
  };

  // Pins: the panel asks as it opens, so the pin renders and there is something on screen to
  // lose. Reopening asks again, and that answer is admitted while this account may still read
  // and is still in flight when the privilege goes.
  await page.getByRole('button', { name: 'Pinned messages' }).click();
  const pins = page.getByRole('dialog', { name: /^Pinned in/ });
  await expect(pins.getByText(PANEL_SECRET).first()).toBeVisible({ timeout: 20000 });
  await pins.getByRole('button', { name: 'Close dialog' }).click();
  await expect(page.getByRole('dialog', { name: /^Pinned in/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Pinned messages' }).click();
  await expect(page.getByRole('dialog', { name: /^Pinned in/ })).toBeVisible();
  breakNextRefresh();
  await revoke();
  await expect(page.getByText(PANEL_SECRET)).toHaveCount(0);
  await expect(preview).toHaveCount(0);
  // The held-back answer has landed by now and may not bring the pin back.
  await page.waitForTimeout(4500);
  await expect(page.getByText(PANEL_SECRET)).toHaveCount(0);
  expect(await page.locator('.search-results button').count()).toBe(0);
  await recover(pins);

  // A reply thread renders channel content of its own, so it is closed rather than trusted
  // when the authority moves, and the answer it was waiting on never lands.
  await article.hover();
  await article.getByRole('button', { name: 'Open reply thread' }).click();
  const thread = page.getByRole('dialog', { name: 'Reply thread' });
  await expect(thread.getByText(PANEL_SECRET).first()).toBeVisible({ timeout: 20000 });
  await thread.getByRole('button', { name: 'Close dialog' }).click();
  await article.getByRole('button', { name: 'Open reply thread' }).click();
  await expect(page.getByRole('dialog', { name: 'Reply thread' })).toBeVisible();
  breakNextRefresh();
  await revoke();
  await expect(page.getByRole('dialog', { name: 'Reply thread' })).toHaveCount(0);
  await page.waitForTimeout(4500);
  await expect(page.getByRole('dialog', { name: 'Reply thread' })).toHaveCount(0);
  await expect(page.getByText(PANEL_SECRET)).toHaveCount(0);
  await recover(null);

  // Search holds results of its own. The first query is answered, so there is something on
  // screen to lose; the second is asked while the privilege is still held and is in flight
  // when it goes.
  await page.getByRole('button', { name: 'Search messages' }).click();
  const search = page.getByRole('dialog', { name: 'Search messages' });
  await search.getByLabel('Search', { exact: true }).fill(PANEL_SECRET);
  await search.getByRole('button', { name: 'Search' }).click();
  await expect(search.getByText('2 messages found')).toBeVisible({ timeout: 20000 });
  await expect(search.getByText(PANEL_SECRET).first()).toBeVisible();
  await search.getByLabel('Search', { exact: true }).fill(`${PANEL_SECRET} thread`);
  await search.getByRole('button', { name: 'Search' }).click();
  await expect(search.getByText('Searching conversations…')).toBeVisible();
  breakNextRefresh();
  await revoke();
  // The panel is back at its empty state rather than showing the answer it was waiting on.
  await expect(search.getByText('Find a moment in the conversation')).toBeVisible();
  await expect(search.getByText('message found')).toHaveCount(0);
  await expect(page.getByText(PANEL_SECRET)).toHaveCount(0);
  await page.waitForTimeout(4500);
  await expect(page.getByText(PANEL_SECRET)).toHaveCount(0);
  expect(await page.locator('.search-results button').count()).toBe(0);
  await recover(search);

  // The fixture is unpinned so the installation is left as this journey found it.
  await request.put(`/api/messages/${note.id}/pin`, { data: { pinned: false } });
});

// The durable half of a send. Unsent text is the user's own, so a delivery that was never
// confirmed is not a lost message: with device persistence on it is rebuilt from the device
// scope and reconciled against the operation's own record, and a retry delivers once.
test('an unconfirmed send survives a reload and is reconciled by its own key', async ({
  page,
  request
}) => {
  test.setTimeout(300000);
  await signInOverApi(request);
  const state = await (await request.get('/api/state')).json();
  const general = state.channels.find((channel) => channel.type === 'text');
  expect(general?.name).toBe('general');
  await createAccount(request, OUTBOX_READER);

  await signIn(page, OUTBOX_READER);
  // Persistence is opt-in: it is the preference that makes unsent text durable.
  await page.getByRole('button', { name: 'Your preferences' }).click();
  const preferences = page.getByRole('dialog', { name: 'Your preferences' });
  await preferences.getByRole('checkbox', { name: /Remember drafts and unsent messages/ }).check();
  await preferences.getByRole('button', { name: 'Close dialog' }).click();

  // Delivery is refused in a way the server would accept later, so the send stays owed.
  const refuse = (route) =>
    route.request().method() !== 'POST'
      ? route.continue()
      : route.fulfill({
          status: 503,
          contentType: 'application/json',
          body: '{"detail":"The server is busy. Try again shortly."}'
        });
  await page.route('**/api/channels/*/messages*', refuse);
  const log = page.getByRole('log', { name: 'Channel messages' });
  await page.getByRole('textbox', { name: 'Message general' }).fill(OUTBOX_NOTE);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(log.getByText('Message not sent')).toBeVisible();

  // A reload rebuilds the send from the account's own scope and asks the operation record
  // again, so the person sees the message they wrote rather than an empty composer.
  await page.reload();
  await expect(log.getByText(OUTBOX_NOTE)).toBeVisible({ timeout: 20000 });
  await expect(log.getByText('Message not sent')).toBeVisible();

  // With delivery working again the retry submits the same operation key, so exactly one
  // message exists afterwards — not one per attempt.
  await page.unroute('**/api/channels/*/messages*', refuse);
  await log.getByRole('button', { name: /Retry/ }).click();
  await expect(log.getByText('Message not sent')).toHaveCount(0, { timeout: 20000 });
  await expect(log.locator('.message-content').filter({ hasText: OUTBOX_NOTE })).toHaveCount(1);
  await page.reload();
  await expect(log.locator('.message-content').filter({ hasText: OUTBOX_NOTE })).toHaveCount(1);
  const messages = await (await request.get(`/api/channels/${general.id}/messages`)).json();
  expect(messages.filter((message) => message.content === OUTBOX_NOTE)).toHaveLength(1);
});

// Terminal sends are recovery, not retry. A private destination's text is never silently
// moved into the currently open composer, and the feature also works without device storage.
test('refused sends offer complete recoverable text without retrying or replacing drafts', async ({
  page,
  request
}) => {
  const account = { username: 'recovery_reader', password: 'a recovery reader passphrase' };
  await signInOverApi(request);
  await createAccount(request, account);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async () => {
          throw new Error('Clipboard unavailable');
        }
      }
    });
  });
  await signIn(page, account);
  const composer = page.getByRole('textbox', { name: 'Message general' });
  const text = 'This complete text belongs to the author. '.repeat(80);
  let posts = 0;
  let release;
  const held = new Promise((resolve) => {
    release = resolve;
  });
  await page.route('**/api/channels/*/messages*', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    posts++;
    await held;
    return route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ detail: 'This send was already committed and its content is gone.' })
    });
  });
  await composer.fill(text);
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  await composer.fill('Keep this separate draft intact.');
  release();
  const recovered = page.getByRole('region', { name: 'Recovered message', exact: true });
  await expect(recovered.getByRole('textbox', { name: 'Recovered message text' })).toHaveValue(
    text.trim()
  );
  await expect(composer).toHaveValue('Keep this separate draft intact.');
  await expect(page.getByRole('button', { name: /Message not sent.*Retry/ })).toHaveCount(0);
  await expect(page.getByText(/Text is kept only until you leave/)).toBeVisible();
  await recovered.getByRole('button', { name: 'Copy text' }).click();
  await expect(page.getByText(/Select the text below and copy it/)).toBeVisible();
  const field = recovered.getByRole('textbox', { name: 'Recovered message text' });
  await expect(field).toBeFocused();
  expect(await field.evaluate((element) => element.selectionEnd - element.selectionStart)).toBe(
    text.trim().length
  );
  expect(posts).toBe(1);
  await recovered.getByRole('button', { name: 'Dismiss recovered message' }).click();
  await expect(recovered).toHaveCount(0);
  await expect(composer).toHaveValue('Keep this separate draft intact.');
  expect(posts).toBe(1);
});

test('member reports reach permitted moderators and revoked report evidence cannot return', async ({
  page,
  request
}) => {
  const { assignOnly, createRole, signOut } = await import('./support.mjs');
  const reporter = { username: 'report_member', password: 'a report member passphrase' };
  const moderator = { username: 'report_moderator', password: 'a report moderator passphrase' };
  await signInOverApi(request);
  const reporterID = await createAccount(request, reporter);
  const moderatorID = await createAccount(request, moderator);
  const roleID = await createRole(request, 'report reviewers', [
    'channel.read',
    'message.delete_any'
  ]);
  await assignOnly(request, moderatorID, [roleID]);
  const created = await request.post('/api/channels', {
    data: { name: 'report-room', type: 'text' }
  });
  expect(created.ok()).toBe(true);
  const channel = await created.json();
  const roles = await (await request.get('/api/roles')).json();
  const defaultRole = roles.find((role) => role.is_default);
  const originalPermissions = [...defaultRole.permissions];
  const access = async (allowed) => {
    await setRolePermissions(
      request,
      defaultRole.id,
      allowed
        ? originalPermissions
        : originalPermissions.filter((permission) => permission !== 'channel.read')
    );
    await setRolePermissions(
      request,
      roleID,
      allowed ? ['channel.read', 'message.delete_any'] : ['message.delete_any']
    );
  };
  const selectedText = 'A selected private message for moderation';
  const contextText = 'Surrounding private context must not be included';
  for (const content of [contextText, selectedText])
    expect(
      (
        await request.post(`/api/channels/${channel.id}/messages`, {
          data: { content, idempotency_key: crypto.randomUUID() }
        })
      ).status()
    ).toBe(201);

  await signIn(page, reporter);
  await page.goto(`/?channel=${channel.id}`);
  const selected = page
    .getByRole('log', { name: 'Channel messages' })
    .locator('article.message')
    .filter({ hasText: selectedText });
  await selected.getByRole('button', { name: 'Report message', exact: true }).click();
  const reportDialog = page.getByRole('dialog', { name: 'Report message', exact: true });
  await expect(reportDialog.getByText(selectedText, { exact: true })).toBeVisible();
  await expect(reportDialog.getByText(contextText, { exact: true })).toHaveCount(0);
  await expect(reportDialog.getByText(/moderators\s+who can read this channel/)).toBeVisible();
  await reportDialog
    .getByRole('textbox', { name: 'Why are you reporting this message?' })
    .fill('Please review this selected message.');
  await reportDialog.getByRole('button', { name: 'Send report', exact: true }).click();
  await expect(reportDialog.getByText(/Report sent/)).toBeVisible();
  await reportDialog.getByRole('button', { name: 'Done', exact: true }).click();
  expect((await page.request.get('/api/admin/reports')).status()).toBe(403);
  await expect(page.getByRole('button', { name: 'Manage server', exact: true })).toHaveCount(0);

  await signOut(page);
  await signIn(page, moderator);
  await page.getByRole('button', { name: 'Manage server', exact: true }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  const tabs = tools.getByRole('navigation', { name: 'Server administration' });
  await expect(tabs.getByRole('button', { name: 'Reports', exact: true })).toBeVisible();
  await expect(tabs.getByRole('button', { name: 'People', exact: true })).toHaveCount(0);
  const report = tools.getByRole('article', { name: 'Report in report-room', exact: true });
  await expect(report.getByText(selectedText, { exact: true })).toBeVisible();
  await expect(report.getByText(contextText, { exact: true })).toHaveCount(0);
  await report
    .getByRole('textbox', { name: 'Moderator note (optional)' })
    .fill('Reviewed with no account restriction.');
  await report.getByRole('button', { name: 'Resolve report', exact: true }).click();
  await expect(tools.getByText('No open reports in the channels you can read.')).toBeVisible();
  await tools
    .getByRole('combobox', { name: 'Report status', exact: true })
    .selectOption('resolved');
  await expect(report.getByRole('textbox', { name: 'Moderator note (optional)' })).toHaveValue(
    'Reviewed with no account restriction.'
  );

  let release, captured;
  const held = new Promise((resolve) => {
    release = resolve;
  });
  const ready = new Promise((resolve) => {
    captured = resolve;
  });
  let intercepted = false;
  await page.route('**/api/admin/reports?status=resolved*', async (route) => {
    if (intercepted) return route.continue();
    intercepted = true;
    const response = await route.fetch();
    captured();
    await held;
    await route.fulfill({ response });
  });
  await tools.getByRole('button', { name: 'Refresh reports', exact: true }).click();
  await ready;
  try {
    await access(false);
    await expect(tools.getByText(selectedText, { exact: true })).toHaveCount(0);
    release();
    await expect(
      tools.getByText('No resolved reports in the channels you can read.')
    ).toBeVisible();
    await expect(tools.getByText(selectedText, { exact: true })).toHaveCount(0);
    const denied = await page.request.get('/api/admin/reports?status=resolved');
    expect((await denied.json()).reports).toEqual([]);

    // Regrant is a positive control: the same panel recovers without a reload/sign-in.
  } finally {
    release();
    await access(true);
  }
  await expect(report.getByText(selectedText, { exact: true })).toBeVisible();
  await tools.getByRole('button', { name: 'Close dialog' }).click();
});

test('channel mention mute is stored per account and survives reload without changing unread state', async ({
  page,
  request
}) => {
  const { signOut } = await import('./support.mjs');
  const member = { username: 'mute_member', password: 'a mute member passphrase' };
  const other = { username: 'unmuted_member', password: 'an unmuted member passphrase' };
  await signInOverApi(request);
  await createAccount(request, member);
  await createAccount(request, other);
  const state = await (await request.get('/api/state')).json();
  const general = state.channels.find((channel) => channel.name === 'general');
  await signIn(page, member);
  const before = await (await page.request.get('/api/unread')).json();
  const mute = page.getByRole('button', {
    name: 'Mute mention notifications for #general',
    exact: true
  });
  await expect(mute).toHaveAttribute('aria-pressed', 'false');
  await mute.click();
  const unmute = page.getByRole('button', {
    name: 'Unmute mention notifications for #general',
    exact: true
  });
  await expect(unmute).toHaveAttribute('aria-pressed', 'true');
  const preferences = await (await page.request.get('/api/me/channel-notifications')).json();
  expect(preferences.find((entry) => entry.channel_id === general.id)?.muted).toBe(true);
  expect(await (await page.request.get('/api/unread')).json()).toEqual(before);
  await page.reload();
  await expect(unmute).toHaveAttribute('aria-pressed', 'true');
  await signOut(page);
  await signIn(page, other);
  await expect(mute).toHaveAttribute('aria-pressed', 'false');
  await signOut(page);
  await signIn(page, member);
  await expect(unmute).toHaveAttribute('aria-pressed', 'true');
  await unmute.click();
  await expect(mute).toHaveAttribute('aria-pressed', 'false');
});
