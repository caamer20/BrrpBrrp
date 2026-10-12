import { expect } from '@playwright/test';
import { randomBytes } from 'node:crypto';
import { OWNER, createAccount, signIn, signInOverApi, test } from './support.mjs';

// The reviewer journey lives in its own file and its own Playwright project, which
// runs after the ownership journey. It needs an account holding
// report.review_private, which the pre-ceremony administrator bridge deliberately
// does not hand out, so it delegates the capability from the owner the ownership
// journey leaves in place. Relying on that account rather than performing a second
// ceremony matters: a ceremony is irreversible, mandates a second factor for later
// sign-ins, and would leave a different installation state behind.

// R11-01, rendered. A review was in flight when the list was re-read. The old code
// shared one version counter between reads and mutations, so the held PATCH's
// cleanup failed admission and left the panel permanently busy: every later review
// returned immediately and nothing in the panel could recover it. This holds a real
// PATCH, refreshes underneath it, releases it, and then reviews another record
// without closing the panel.
test('a review survives a refresh that lands while it is in flight', async ({ page, request }) => {
  test.setTimeout(180000);
  // Usernames are limited to 32 characters and the projects share one server, so
  // the run tag is short and every account here is distinct: a fixed name would
  // collide on a second pass rather than fail honestly.
  const tag = randomBytes(4).toString('hex');
  // Two pairs across three accounts: the queue needs two records with different
  // authors, and the suite already creates many accounts against the server's
  // 100-account cap.
  const reporter = { username: `p10_rep_${tag}`, password: 'a p10 reporter passphrase' };
  const peer = { username: `p10_peer_${tag}`, password: 'a p10 peer passphrase' };
  const reviewer = { username: `p10_rev_${tag}`, password: 'a p10 reviewer passphrase' };
  // The ownership journey runs before this one and leaves an account holding the
  // owner position, which holds every capability including report.review_private. So
  // this journey signs in as that account, creates what it needs and delegates the
  // capability from it — no ceremony, no second factor, and no change to the
  // installation's authentication state that a later project would inherit.
  const STEWARD = { username: 'steward', password: 'a steward passphrase' };
  const session = await request.post('/api/login', { data: STEWARD });
  expect(
    session.ok(),
    `sign in as the installation owner: HTTP ${session.status()} ${await session.text()}`
  ).toBe(true);
  const reporterID = await createAccount(request, reporter);
  const peerID = await createAccount(request, peer);
  const reviewerID = await createAccount(request, reviewer);

  // Two genuinely separate accepted conversations, each with one message, so the
  // reviewer queue holds two records with different authors and neither pair is
  // reused (a pair asked twice reopens rather than creating a second one).
  const pairs = [];
  for (const [from, fromID, to, toID] of [
    [reporter, reporterID, peer, peerID],
    [peer, peerID, reviewer, reviewerID]
  ]) {
    await signInOverApi(request, from);
    const asked = await request.post('/api/directs', {
      data: { user_id: toID, introduction: 'may I' }
    });
    expect(
      [200, 201],
      `request from ${from.username}: HTTP ${asked.status()} ${await asked.text()}`
    ).toContain(asked.status());
    await signInOverApi(request, to);
    const conversation = (await (await request.get('/api/directs')).json()).find(
      (d) => d.peer.username === from.username
    ).id;
    expect((await request.post(`/api/directs/${conversation}/accept`, { data: {} })).status()).toBe(
      200
    );
    expect(
      (
        await request.post(`/api/conversations/${conversation}/messages`, {
          data: { content: 'the wording under review', idempotency_key: `p10-msg-${pairs.length}` }
        })
      ).status()
    ).toBe(201);
    pairs.push(conversation);
  }
  // Each record is reported by the participant who can read that conversation.
  for (const [account, conversation] of [
    [reporter, pairs[0]],
    [peer, pairs[1]]
  ]) {
    await signInOverApi(request, account);
    const messageID = (
      await (await request.get(`/api/conversations/${conversation}/messages`)).json()
    )[0].id;
    const preview = await (
      await request.get(`/api/messages/${messageID}/private-report-preview`)
    ).json();
    expect(
      (
        await request.post(`/api/messages/${messageID}/private-report`, {
          data: { reason: 'because it was unwelcome', version: preview.version }
        })
      ).status()
    ).toBe(201);
  }

  // The reviewer needs the capability, which only an account already holding it can
  // delegate, so the ownership ceremony is the real operator path.
  await signInOverApi(request);
  // Recorded deliberately: the previous journey transfers ownership, and a session
  // established before that transfer must not be trusted to carry the operator tier.
  // Arranging the two conversations above signed the shared API session in as a
  // participant, and role creation needs the owner rather than a participant. The
  // session is re-established here rather than assumed, so the delegation below
  // cannot silently run as whoever was signed in last.
  await signInOverApi(request, STEWARD);
  const created = await request.post('/api/roles', {
    data: { name: `p10 reviewers ${tag}`, position: 20, permissions: ['report.review_private'] }
  });
  expect(created.status(), `create the reviewer role: ${await created.text()}`).toBe(201);
  const roleId = (await created.json()).id;
  expect(
    (
      await request.put(`/api/admin/users/${reviewerID}/roles`, { data: { role_ids: [roleId] } })
    ).status(),
    'the reviewer was granted the capability'
  ).toBe(200);
  const reviewerState = await (
    await signInOverApi(request, reviewer).then(() => request.get('/api/state'))
  ).json();
  expect(
    reviewerState.authorization.capabilities,
    'the reviewer holds report.review_private'
  ).toContain('report.review_private');

  await signIn(page, reviewer);
  // The page session starts before the grant existed in some orderings, so the
  // workspace is reloaded rather than assumed fresh.
  await page.reload();
  await expect(page.getByRole('button', { name: 'Manage server' })).toBeVisible();
  await page.getByRole('button', { name: 'Manage server' }).click();
  const tools = page.getByRole('dialog', { name: 'Manage your server' });
  await tools
    .getByRole('navigation', { name: 'Server administration' })
    .getByRole('button', { name: 'Private evidence' })
    .click();
  const cards = tools.getByRole('article', { name: /^Message reported by/ });
  await expect(cards.first()).toBeVisible();
  expect(await cards.count(), 'two submitted records are queued').toBeGreaterThanOrEqual(2);

  // Hold a real PATCH, refresh the list underneath it, then release it.
  let release;
  const held = new Promise((resolve) => (release = resolve));
  let intercepted = 0;
  await page.route('**/api/admin/private-reports/*', async (route) => {
    if (route.request().method() !== 'PATCH') return route.continue();
    intercepted++;
    await held;
    await route.continue();
  });
  await cards.first().getByRole('button', { name: 'Mark reviewed' }).click();
  await expect.poll(() => intercepted).toBe(1);
  await tools.getByRole('button', { name: 'Refresh', exact: true }).click();
  release();
  await expect(tools.getByText('Marked as reviewed.')).toBeVisible({ timeout: 15000 });
  await page.unroute('**/api/admin/private-reports/*');

  // The control that matters: another record is still reviewable in the same panel.
  const remaining = tools.getByRole('article', { name: /^Message reported by/ });
  await expect(remaining.first(), 'the panel did not recover after the overlap').toBeVisible();
  await remaining.first().getByRole('textbox', { name: 'Review note (optional)' }).fill('after');
  await remaining.first().getByRole('button', { name: 'Mark reviewed' }).click();
  await expect(tools.getByText('Marked as reviewed.')).toBeVisible();
});
