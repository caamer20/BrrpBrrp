import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

async function expectAccessible(page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze();
  expect(
    result.violations.map(({ id, impact, nodes }) => ({
      id,
      impact,
      targets: nodes.map(({ target }) => target).slice(0, 12)
    }))
  ).toEqual([]);
}

test('first setup, persistent chat, search and sign-out', async ({ page, context }) => {
  test.setTimeout(120000);
  const token = readFileSync(join(process.env.BRRP_E2E_DATA_DIR, 'setup-token'), 'utf8');
  await page.goto('/setup');
  await expect(page.getByRole('heading', { name: 'Your space starts here.' })).toBeVisible();
  await expectAccessible(page);

  await page.getByLabel('Server name').fill('Release check');
  await page.getByLabel('Setup token').fill(token);
  await page.getByLabel('Username').fill('release_admin');
  await page.locator('input[autocomplete="new-password"]').fill('release check passphrase');
  await page.getByRole('button', { name: 'Create your space' }).click();
  const administration = page.getByRole('dialog', { name: 'Manage your server' });
  await expect(administration).toBeVisible();
  await administration.getByRole('button', { name: 'Close dialog' }).click();

  const composer = page.getByRole('textbox', { name: 'Message general' });
  await expect(composer).toBeVisible();
  await expectAccessible(page);
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.theme)).toBe('dark');
  await expectAccessible(page);
  await page.emulateMedia({ colorScheme: 'light' });
  const message = 'Release check searchable message';
  await composer.fill(message);
  await page.getByRole('button', { name: 'Send message' }).click();
  const log = page.getByRole('log', { name: 'Channel messages' });
  await expect(log.locator('.message-content').filter({ hasText: message })).toBeVisible();
  await page.reload();
  await expect(log.locator('.message-content').filter({ hasText: message })).toBeVisible();

  // A focused discussion keeps its own draft, uploads, and live replies.
  await composer.fill('Keep my channel draft');
  const original = log
    .locator('article')
    .filter({ has: page.locator('.message-content').filter({ hasText: message }) });
  await original.hover();
  await original.getByRole('button', { name: 'Open reply thread', exact: true }).click();
  const thread = page.getByRole('dialog', { name: 'Reply thread', exact: true });
  const replyBox = thread.getByRole('textbox', { name: 'Reply in thread' });
  await replyBox.fill('Keep my thread draft');
  await thread.getByRole('button', { name: 'Close dialog' }).click();
  await expect(composer).toHaveValue('Keep my channel draft');
  await original.hover();
  await original.getByRole('button', { name: 'Open reply thread', exact: true }).click();
  await expect(replyBox).toHaveValue('Keep my thread draft');
  await replyBox.fill('A focused reply with a file');
  await thread.getByLabel('Choose attachments').setInputFiles({
    name: 'discussion.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Thread attachment')
  });
  await expect(thread.getByRole('button', { name: 'Remove discussion.txt' })).toBeVisible();
  await page.route('**/api/channels/*/messages', (route) =>
    route.request().method() === 'POST' ? route.abort() : route.continue()
  );
  await thread.getByRole('button', { name: 'Send reply', exact: true }).click();
  const replies = thread.getByRole('log', { name: 'Thread replies' });
  await expect(replies.getByRole('button', { name: 'Message not sent · Retry' })).toBeVisible();
  await page.unroute('**/api/channels/*/messages');
  await replies.getByRole('button', { name: 'Message not sent · Retry' }).click();
  await expect(replies.getByText('A focused reply with a file', { exact: true })).toBeVisible();
  await expect(replies.getByRole('link', { name: /discussion.txt/ })).toBeVisible();
  await expect(thread.getByText('1 reply', { exact: true })).toBeVisible();
  await expectAccessible(page);
  await page.screenshot({ path: test.info().outputPath('thread-desktop.png') });
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.theme)).toBe('dark');
  await expectAccessible(page);
  await page.emulateMedia({ colorScheme: 'light' });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(replyBox).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expectAccessible(page);
  await page.screenshot({ path: test.info().outputPath('thread-phone.png') });
  await page.setViewportSize({ width: 1280, height: 720 });

  const other = await context.newPage();
  await other.goto('/');
  const otherOriginal = other
    .getByRole('log', { name: 'Channel messages' })
    .locator('article')
    .filter({ has: other.locator('.message-content').filter({ hasText: message }) });
  await otherOriginal.hover();
  await otherOriginal.getByRole('button', { name: 'Open reply thread', exact: true }).click();
  const otherThread = other.getByRole('dialog', { name: 'Reply thread', exact: true });
  await otherThread
    .getByRole('textbox', { name: 'Reply in thread' })
    .fill('A reply from another tab');
  await otherThread.getByRole('button', { name: 'Send reply', exact: true }).click();
  await expect(replies.getByText('A reply from another tab', { exact: true })).toBeVisible();
  await expect(thread.getByText('2 replies', { exact: true })).toBeVisible();
  const otherReply = otherThread.locator('article').filter({
    has: other.locator('.message-content').filter({ hasText: 'A reply from another tab' })
  });
  await otherReply.hover();
  await otherReply.getByRole('button', { name: 'Edit message', exact: true }).click();
  await otherThread.getByRole('textbox', { name: 'Edit message' }).fill('Edited from another tab');
  await otherThread.getByRole('button', { name: 'Save changes' }).click();
  await expect(replies.getByText('Edited from another tab', { exact: true })).toBeVisible();
  const editedReply = otherThread.locator('article').filter({
    has: other.locator('.message-content').filter({ hasText: 'Edited from another tab' })
  });
  await editedReply.hover();
  other.once('dialog', (dialog) => dialog.accept());
  await editedReply.getByRole('button', { name: 'Delete message', exact: true }).click();
  await expect(replies.getByText('Edited from another tab', { exact: true })).not.toBeVisible();
  await expect(thread.getByText('1 reply', { exact: true })).toBeVisible();
  await other.close();
  await thread.getByRole('button', { name: 'Close dialog' }).click();
  await expect(composer).toHaveValue('Keep my channel draft');

  await page.keyboard.press('ControlOrMeta+k');
  const switcher = page.getByRole('dialog', { name: 'Jump to a channel' });
  await expect(switcher.getByRole('combobox', { name: 'Find a channel' })).toBeFocused();
  await switcher.getByRole('combobox').fill('no-channel-with-this-name');
  await expect(switcher.getByText('0 channels found')).toBeVisible();
  await switcher.getByRole('combobox').fill('general');
  await expectAccessible(page);
  await switcher.getByRole('combobox').press('ArrowDown');
  await switcher.getByRole('combobox').press('Enter');
  await expect(switcher).not.toBeVisible();
  await expect(composer).toBeFocused();
  await expect(composer).toHaveValue('Keep my channel draft');
  await composer.fill('Temporary thread');
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  const temporary = log
    .locator('article')
    .filter({ has: page.locator('.message-content').filter({ hasText: 'Temporary thread' }) });
  await temporary.hover();
  await temporary.getByRole('button', { name: 'Open reply thread', exact: true }).click();
  await replyBox.fill('Keep this unsent draft after deletion');
  const parent = thread.locator('.thread-original article');
  await parent.hover();
  page.once('dialog', (dialog) => dialog.accept());
  await parent.getByRole('button', { name: 'Delete message', exact: true }).click();
  await expect(
    thread.getByText('This discussion is no longer available. Return to the channel to continue.')
  ).toBeVisible();
  await expect(replyBox).toHaveValue('Keep this unsent draft after deletion');
  await expect(thread.getByRole('button', { name: 'Send reply', exact: true })).toBeDisabled();
  await thread.getByRole('button', { name: 'Close dialog' }).click();

  await page.getByRole('button', { name: 'Search messages' }).click();
  const search = page.getByRole('dialog', { name: 'Search messages' });
  await search.getByRole('textbox', { name: 'Search' }).fill('searchable');
  await search.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(search.getByText('1 message found')).toBeVisible();
  await expect(search.getByText(message)).toBeVisible();
  await search.getByRole('button', { name: 'Close dialog' }).click();

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(composer).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 1280, height: 720 });

  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back.' })).toBeVisible();
  await page.getByLabel('Username').fill('release_admin');
  await page.locator('input[autocomplete="current-password"]').fill('release check passphrase');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(log.locator('.message-content').filter({ hasText: message })).toBeVisible();

  await page.evaluate(() => navigator.serviceWorker.ready);
  await page.reload();
  await expect(log.locator('.message-content').filter({ hasText: message })).toBeVisible();
  await context.setOffline(true);
  await page.reload();
  await expect(page.getByRole('heading', { name: "We couldn't reach your server." })).toBeVisible();
});
