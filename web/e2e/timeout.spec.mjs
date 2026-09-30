import { expect, test } from '@playwright/test';
import { OWNER, createAccount, signIn, signInOverApi, signOut } from './support.mjs';

// A community timeout is a *community* restriction, and the member is the only person
// entitled to be told about it. This journey asserts the rendered interface for that
// member, because the defect it guards against is invisible to a status code: the
// server refuses every community request correctly, and the member sees an empty
// workspace with nothing saying why.
//
// It also asserts the other half — that the notice is not a ban notice. Direct
// messages keep working through a timeout, and a member who has just lost their
// channels cannot guess that from a screen of refusals.

const TIMED_OUT = { username: 'timed_out_member', password: 'a timeout passphrase here' };
const PEER = { username: 'timeout_peer', password: 'a timeout peer passphrase' };

test('a timed-out member is told why the community is closed and keeps their private conversations', async ({
  page,
  request
}) => {
  test.setTimeout(240000);
  await signIn(page, OWNER);
  await signInOverApi(request);

  const memberID = await createAccount(request, TIMED_OUT);
  const peerID = await createAccount(request, PEER);

  // A consented direct conversation, established *before* the timeout. Consent is what
  // a timeout does not touch, so this is the thing that must survive. Only the intended
  // recipient may accept, so the member makes the request and the peer accepts it.
  await signInOverApi(request, TIMED_OUT);
  const requested = await request.post('/api/directs', {
    data: { user_id: peerID, introduction: 'may we talk?' }
  });
  expect(requested.status(), `create direct: ${await requested.text()}`).toBe(201);
  const conversation = (await requested.json()).id;

  await signInOverApi(request, PEER);
  const accepted = await request.post(`/api/directs/${conversation}/accept`, { data: {} });
  expect(accepted.status()).toBe(200);

  // The seed is the member's own message, so the history the journey reads afterwards
  // is history the timed-out member is entitled to see.
  await signInOverApi(request, TIMED_OUT);
  const before = await request.post(`/api/conversations/${conversation}/messages`, {
    data: { content: 'written before the timeout', idempotency_key: 'timeout-seed-1' }
  });
  expect(before.status()).toBe(201);

  // Apply the restriction as the owner, through the route the interface calls.
  await signInOverApi(request);
  const applied = await request.post(`/api/admin/users/${memberID}/timeout`, {
    data: { minutes: 30, reason: 'cooling off' }
  });
  expect(applied.status()).toBe(200);

  // Now sign in as the timed-out member, in the browser.
  await signOut(page);
  await signIn(page, TIMED_OUT);

  // The community is genuinely closed, and the member is told so. Both halves matter:
  // a notice without the refusal would mean the timeout is not enforced, and a refusal
  // without the notice is the defect.
  const notice = page.getByText('You are in a voice-only timeout.');
  await expect(notice).toBeVisible();
  // The notice names what still works, because the restriction is narrower than a ban
  // and the member cannot infer that from the failures around it.
  await expect(
    page.getByText('direct messages, groups and activity still work', { exact: false })
  ).toBeVisible();
  // No community channels are rendered: the restriction is real, not decorative.
  await expect(page.getByRole('button', { name: 'general', exact: true })).toHaveCount(0);

  // The private conversation is still reachable, still listed in the sidebar beside the
  // community channels that are not, and still readable. It is opened from the sidebar
  // because that is where a member navigates to it.
  const inbox = page.getByRole('button', { name: /Messages and requests/i });
  await expect(inbox).toBeVisible();
  const peer = page.getByRole('button', { name: 'timeout_peer', exact: true });
  await expect(peer).toBeVisible();
  await peer.click();
  await expect(page.getByText('written before the timeout')).toBeVisible({ timeout: 20000 });

  // And they can still send — a timeout that silently closed their private messages
  // too would be a different, much larger restriction than the one described.
  const composer = page.getByRole('textbox', { name: /message/i }).last();
  await composer.fill('sent during the timeout');
  await composer.press('Enter');
  await expect(page.getByText('sent during the timeout')).toBeVisible({ timeout: 20000 });

  // The server really stored it, read back as the member rather than as the owner —
  // the owner is not a participant in this conversation and must not be able to see
  // it. A rendered optimistic message would satisfy every assertion above, and a
  // timeout that silently dropped private sends would be invisible.
  await signInOverApi(request, TIMED_OUT);
  const persisted = await request.get(`/api/conversations/${conversation}/messages`);
  expect(persisted.status()).toBe(200);
  expect(await persisted.text()).toContain('sent during the timeout');

  // Lifting the timeout removes the notice and reopens the community, which is what
  // makes it a restriction with an end rather than a silent ban.
  await signInOverApi(request);
  // A body-bearing request, which is what the origin check expects on a DELETE — the
  // bodiless form is refused by the CSRF guard before it reaches the handler.
  const lifted = await request.delete(`/api/admin/users/${memberID}/timeout`, { data: {} });
  expect(lifted.status()).toBe(200);
  await page.reload();
  await expect(page.getByText('You are in a voice-only timeout.')).toHaveCount(0);
});
