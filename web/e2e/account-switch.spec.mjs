import { expect } from '@playwright/test';
import { createAccount, OWNER, signIn, signInOverApi, signOut, test } from './support.mjs';

// R4-02, rendered. The store regression proves the captured account/session identity; this
// journey proves the same rule through the real interface, where the consequence is
// visible rather than internal. A channel move issued by one account is held in flight
// until a different account has signed in, and then released. The held response is the
// first account's own list in a different order plus one channel only it would be shown,
// so applying it to the account that is signed in afterwards is both a changed order and
// a channel that account must never see.
//
// The switch is deliberately done in place: a full page load would destroy the pending
// request with the old document, which is exactly why the defect is about the workspace
// model rather than about navigating between accounts.
const MEMBER = { username: 'switch_member', password: 'a delegated passphrase' };

// signInHere fills the sign-in form that is already on screen, without navigating, so the
// previous account's in-flight work is still alive when the next account signs in.
async function signInHere(page, account) {
  await page.getByLabel('Username').fill(account.username);
  await page.locator('input[autocomplete="current-password"]').fill(account.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByLabel('Your preferences')).toBeVisible();
}

test('a channel move held across a sign-in cannot reorder the next account', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  await signIn(page, OWNER);
  await signInOverApi(request);
  await createAccount(request, MEMBER);

  // Two channels of this journey's own, so the held response's order is visibly different
  // from the order the next account receives from its own snapshot.
  for (const name of ['switch-alpha', 'switch-beta']) {
    const response = await request.post('/api/channels', {
      data: { name, type: 'text', visibility: 'public', max_voice_users: 8 }
    });
    expect(response.ok(), `create ${name}: ${response.status()}`).toBe(true);
  }
  await page.reload();
  await expect(page.getByRole('button', { name: 'Reorder switch-beta' })).toBeVisible();

  const ownerChannels = await (await request.get('/api/channels')).json();
  const heldChannel = 'held-response-only';
  const heldBody = [
    {
      id: heldChannel,
      name: heldChannel,
      type: 'text',
      category: '',
      position: 0,
      visibility: 'private'
    },
    ...[...ownerChannels].reverse()
  ];
  expect(heldBody.length).toBeGreaterThan(2);

  const order = () =>
    page.$$eval('.channel-navigation [data-channel-order]', (rows) =>
      rows.map((row) => row.dataset.channelOrder)
    );

  // The move is issued by the first account and left in flight: the route holds the
  // request and answers it only when the journey releases it.
  let release;
  const held = new Promise((resolve) => (release = resolve));
  let intercepted = false;
  await page.route('**/api/channels/*/move', async (route) => {
    intercepted = true;
    await held;
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(heldBody)
    });
  });
  await page.getByRole('button', { name: 'Reorder switch-beta' }).focus();
  await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(() => intercepted).toBe(true);

  // A different account signs in while the move is still in flight, without a reload.
  await signOut(page);
  await signInHere(page, MEMBER);
  // The new account cannot reorder channels, so this is not its own move.
  await expect(page.getByRole('button', { name: 'Reorder switch-beta' })).toHaveCount(0);
  const beforeRelease = await order();
  expect(beforeRelease.length).toBeGreaterThan(2);

  // The previous account's response now arrives. It must change nothing.
  release();
  await page.waitForTimeout(750);
  await expect(
    page.getByText(heldChannel),
    "the previous account's move response added a channel to the new account's view"
  ).toHaveCount(0);
  expect(
    await order(),
    "the previous account's move response replaced the new account's channel list"
  ).toEqual(beforeRelease);
  await expect(
    page.getByText('Channel position saved.'),
    "the previous account's move reported success in the new account's view"
  ).toHaveCount(0);

  // The permitted control: the same account doing the same move still reorders the list,
  // so the captured identity is not simply refusing every move. The assertions are on the
  // order itself rather than on the confirmation message: a reorder is published to the
  // account as a structural change, and that notification can legitimately supersede the
  // response the move is carrying, so a dropped confirmation is not a refused move.
  await page.unroute('**/api/channels/*/move');
  await signOut(page);
  await signInHere(page, OWNER);
  await expect(page.getByRole('button', { name: 'Reorder switch-beta' })).toBeVisible();
  const beforeControl = await order();
  await page.getByRole('button', { name: 'Reorder switch-beta' }).focus();
  await page.keyboard.press('Alt+ArrowUp');
  await expect
    .poll(async () => JSON.stringify(await order()), {
      message: 'the same account reordering its own channel no longer takes effect'
    })
    .not.toBe(JSON.stringify(beforeControl));
  // `held-response-only` is the previous account's private channel; the permitted move
  // must reorder the real list without ever introducing it.
  await expect(page.getByText(heldChannel)).toHaveCount(0);
});
