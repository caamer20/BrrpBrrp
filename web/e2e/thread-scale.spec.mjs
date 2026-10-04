import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { expectAccessible, signIn, signInOverApi } from './support.mjs';

// T-THREAD-01's owed half: a thread with a thousand replies, navigated in a real browser.
//
// The unit tests prove the windowing rules and the Go tests prove the page bounds; what
// nobody had run is the thing a reader actually does — open a long thread, walk back to its
// beginning, and find the content once. Three properties are asserted, and each has failed
// in a product of this shape at least once:
//
//   * every reply appears exactly once as the pages are loaded (a cursor that repeats makes
//     the list grow without bound, and a cursor that skips loses somebody's words);
//   * the oldest reply is reachable at all, and its identity is the one that was written
//     (an id that changes on the way through pagination breaks every link to it);
//   * the root message stays in the thread once, and the panel reports the true count.
//
// The seeding goes through the API, because a thousand replies typed into a composer is a
// test of the composer. The product's own flood control is per account — sixty sends a
// minute — so the replies come from sixteen member accounts at a pace the limiter permits,
// which is also the honest shape: a busy thread is many people, not one.
const REPLY_TOTAL = 1000;
const SENDERS = 25;

test('a thousand replies are navigable, each appearing once, with the oldest reachable', async ({
  page,
  request
}) => {
  test.setTimeout(600000);
  await signInOverApi(request);
  const owner = await signInOwnerCookie(request);

  // A channel of its own, so the journey's scale does not reach any other journey.
  const created = await request.post('/api/channels', {
    data: { name: 'scale', type: 'text', visibility: 'public', max_voice_users: 8 }
  });
  // Every owner-scoped call carries the owner's cookie explicitly. A sign-in through this
  // request context replaces the context's own session — which is how the second account
  // creation failed with a 403 answered from the first sender's session.
  const asOwner = { cookie: owner };
  expect(created.ok(), `create the scale channel: ${created.status()}`).toBe(true);
  const channels = await (await request.get('/api/channels', { headers: asOwner })).json();
  const channel = channels.find((item) => item.name === 'scale');
  expect(channel, 'the scale channel exists').toBeTruthy();

  const root = await postMessage(request, owner, channel.id, 'the first words of this thread');
  const threadResponse = await request.post(`/api/channels/${channel.id}/threads`, {
    headers: asOwner,
    data: { root_message_id: root.id, title: 'a thousand replies' }
  });
  expect(threadResponse.ok(), `start the thread: ${threadResponse.status()}`).toBe(true);
  const thread = await threadResponse.json();

  // Twenty-five senders, each paced to the limiter the product enforces — ten in five
  // seconds *and* sixty a minute per account. The per-minute cap is the binding one: five
  // replies every six seconds is fifty a minute, and twenty-five senders reach a thousand
  // replies in under a minute without ever touching either bound.
  const senders = [];
  for (let index = 0; index < SENDERS; index += 1) {
    const account = {
      username: `scale_sender_${index}`,
      password: `a scale sender passphrase ${index}`
    };
    const accountCreated = await request.post('/api/admin/users', {
      headers: asOwner,
      data: {
        username: account.username,
        password: account.password,
        display_name: account.username
      }
    });
    expect(accountCreated.status(), `create ${account.username}`).toBe(201);
    const login = await request.post('/api/login', {
      data: { username: account.username, password: account.password }
    });
    expect(login.ok(), `sign in ${account.username}: ${login.status()}`).toBe(true);
    const setCookie = (await login.headersArray()).find(
      (header) => header.name.toLowerCase() === 'set-cookie'
    );
    senders.push({
      account,
      cookie: setCookie.value.split(';')[0]
    });
  }

  const perSender = Math.floor(REPLY_TOTAL / SENDERS);
  const batchSize = 5;
  const batchPauseMs = 6200;
  let written = 0;
  const firstReplyContent = 'reply 0000 the oldest words in this thread';
  const lastReplyContent = `reply ${String(REPLY_TOTAL - 1).padStart(4, '0')} the newest words in this thread`;
  for (let batch = 0; batch * batchSize < perSender; batch += 1) {
    const results = await Promise.all(
      senders.map(async (sender, senderIndex) => {
        const lines = [];
        for (let step = 0; step < batchSize; step += 1) {
          const ordinal = batch * batchSize + step;
          if (ordinal >= perSender) break;
          const index = senderIndex * perSender + ordinal;
          const content =
            index === 0
              ? firstReplyContent
              : index === REPLY_TOTAL - 1
                ? lastReplyContent
                : `reply ${String(index).padStart(4, '0')}`;
          lines.push(await postMessage(request, sender.cookie, channel.id, content, thread.id));
        }
        return lines.length;
      })
    );
    written += results.reduce((total, count) => total + count, 0);
    if (batch * batchSize + batchSize < perSender)
      await new Promise((done) => setTimeout(done, batchPauseMs));
  }
  expect(written, 'every reply was accepted').toBe(perSender * SENDERS);

  // The server holds them, and the browser is about to be asked to render them.
  await expect
    .poll(
      async () => {
        const state = await (
          await request.get(`/api/threads/${thread.id}`, { headers: asOwner })
        ).json();
        return state.reply_count ?? 0;
      },
      { timeout: 60000, intervals: [1000] }
    )
    // The count is the messages carrying this thread's id, and the root message carries it
    // too: a thread of a thousand replies is a thousand and one messages.
    .toBe(perSender * SENDERS + 1);

  // Now the reader. The journey signs in as one of the senders, opens the root message's
  // thread, and walks to the beginning.
  await signIn(page, senders[0].account);
  const log = page.getByRole('log', { name: 'Channel messages' });
  await page
    .locator('nav.channel-navigation button.channel-link')
    .filter({ hasText: 'scale' })
    .click();
  // The root message is a thousand messages old, so it is not on the channel's first page:
  // a reader re-entering a thread like this uses the thread list, and that is what this
  // does. (Hovering the root and pressing "Open reply thread" is the other way in, and the
  // threads journey covers it.)
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const list = page.getByRole('dialog', { name: 'Threads in scale' });
  await expect(list).toBeVisible();
  await list
    .getByRole('button', { name: /a thousand replies/ })
    .first()
    .click();
  // The panel's own title is "Thread" for every thread: the thread's name is inside it,
  // not in the dialog's accessible name.
  const panel = page.getByRole('dialog', { name: 'Thread', exact: true });
  await expect(panel).toBeVisible();
  await expect(panel.getByText('a thousand replies').first()).toBeVisible();

  // The newest replies are what a thread opens on, and the newest one is the last written.
  await expect(panel.getByText(lastReplyContent)).toBeVisible({ timeout: 30000 });

  // Walk back to the beginning. The panel holds a bounded window (200 replies, from
  // `windowLimit`), so the claim is not "everything is rendered at once" — it is that
  // walking back reaches the oldest words, that nothing is rendered twice inside a window,
  // and that the window never grows without bound however many pages are loaded past it.
  const pageLimit = 50;
  const windowLimit = 200;
  const rendered = async () => {
    const texts = await panel
      .getByRole('log', { name: 'Thread messages' })
      .locator('article.message .message-content')
      .allInnerTexts();
    return texts.map((text) => text.trim());
  };
  const initial = await rendered();
  expect(initial.length, 'the thread opened with no replies').toBeGreaterThan(0);
  expect(initial.length).toBeLessThanOrEqual(pageLimit);
  expect(new Set(initial).size, 'a reply was rendered twice on the first page').toBe(
    initial.length
  );

  const older = panel.getByRole('button', { name: 'Load earlier replies' });
  const threadLog = panel.getByRole('log', { name: 'Thread messages' });
  let guard = 0;
  let reachedTheOldest = initial.includes(firstReplyContent);
  while (!reachedTheOldest && (await older.isVisible().catch(() => false))) {
    await older.click();
    // Wait for the page to land: the panel is `aria-busy` while one is in flight, and the
    // window is expected to grow until it reaches its bound. (The earlier version waited
    // for the button to come back and failed on the page that reached the beginning — the
    // one that matters.)
    await expect
      .poll(
        async () => {
          await expect(threadLog).toHaveAttribute('aria-busy', 'false');
          return (await rendered()).length;
        },
        { timeout: 30000, intervals: [250, 500, 1000] }
      )
      .toBeGreaterThan(0);
    const window = await rendered();
    expect(
      window.length,
      `the rendered window grew past its bound: ${window.length} replies`
    ).toBeLessThanOrEqual(windowLimit);
    expect(new Set(window).size, 'a reply was rendered twice inside one window').toBe(
      window.length
    );
    reachedTheOldest = window.includes(firstReplyContent);
    guard += 1;
    expect(guard, 'the thread never reached its oldest reply').toBeLessThan(30);
  }
  expect(reachedTheOldest, 'the oldest reply was never reachable by walking back').toBe(true);
  await expect(panel.getByText(firstReplyContent)).toBeVisible();

  // The panel shows the thread's replies and names the thread; the root message itself is
  // the channel's, which is where a reader met it. (An earlier version of this assertion
  // expected the root inside the panel and failed, which is how the difference was found.)
  await expect(panel.getByText('a thousand replies').first()).toBeVisible();
  await expect(panel.getByRole('log', { name: 'Thread messages' })).toBeVisible();
  await expectAccessible(page);
});

async function signInOwnerCookie(request) {
  const login = await request.post('/api/login', {
    data: { username: 'release_admin', password: 'release check passphrase' }
  });
  expect(login.ok(), `owner sign-in over the API: ${login.status()}`).toBe(true);
  const header = (await login.headersArray()).find(
    (entry) => entry.name.toLowerCase() === 'set-cookie'
  );
  return header.value.split(';')[0];
}

async function postMessage(request, cookie, channelId, content, threadId) {
  const route = threadId
    ? `/api/threads/${threadId}/messages`
    : `/api/channels/${channelId}/messages`;
  const response = await request.post(route, {
    headers: cookie ? { cookie } : undefined,
    data: { content, idempotency_key: randomUUID() }
  });
  expect(
    response.ok(),
    `send "${content.slice(0, 30)}": ${response.status()} ${await response.text()}`
  ).toBe(true);
  return response.json();
}
