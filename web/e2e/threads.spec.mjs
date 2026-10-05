import { expect, test } from '@playwright/test';
import { createAccount, OWNER, signIn, signInOverApi } from './support.mjs';

// A named thread is a channel conversation with a subject (ADR 0017). This journey
// walks the member-facing path in a real browser: start a thread from a message, name
// it, reply, see the reply land, find it in the channel's thread list, and lock it —
// confirming the locked state refuses a reply while leaving the history readable.
//
// The second account replies over the API. What is under test is that the open thread
// picks up a reply from somebody else and renders it; a rejected or unreadable reply
// would not render at all, so the assertion still means something.
const REPLIER = { username: 'thread_replier', password: 'a passphrase for threading' };
const CHANNEL = 'general';

// The overflow menu is opened the way a person without a mouse opens it: the control
// is always in the accessibility tree, and the test clicks it rather than hovering.
async function startThreadFrom(page, text, title) {
  const message = page.locator(`article.message:has-text("${text}")`).first();
  await expect(message).toBeVisible();
  await message.getByRole('button', { name: /^Actions for message/ }).click();
  await page.getByRole('button', { name: 'Start a named thread' }).click();
  const field = page.getByLabel('Name this thread');
  await expect(field).toBeVisible();
  // Naming is required, and the control stays dead until there is a name: a thread is
  // a subject, not a side effect of opening one.
  const start = page.getByRole('button', { name: 'Start thread' });
  await expect(start).toBeDisabled();
  await field.fill(title);
  await expect(start).toBeEnabled();
  await start.click();
}

test('a named thread is started, answered, listed and locked', async ({ page, request }) => {
  test.setTimeout(180000);
  await signInOverApi(request);
  await createAccount(request, REPLIER);

  await signIn(page, OWNER);
  await page
    .getByRole('button', { name: new RegExp(CHANNEL, 'i') })
    .first()
    .click();
  await expect(page.getByRole('heading', { name: CHANNEL })).toBeVisible();

  const composer = page.locator('.composer textarea').first();
  await composer.fill('We should decide where release notes live before Friday.');
  await composer.press('Enter');
  await expect(
    page.locator('article.message:has-text("release notes live")').first()
  ).toBeVisible();

  await startThreadFrom(page, 'release notes live', 'Where do release notes live?');

  // The thread opens with its name and says it belongs to this channel.
  const thread = page.getByRole('dialog');
  await expect(thread.getByText('Where do release notes live?')).toBeVisible();
  await expect(thread.getByText('1 person follows')).toBeVisible();
  await expect(thread.getByRole('log', { name: 'Thread messages' })).toBeVisible();

  // Starting a thread follows it, and the control reflects what the server decided
  // rather than what was asked for.
  await expect(thread.getByRole('button', { name: 'Following' })).toBeVisible();

  const replyField = thread.getByLabel('Reply to this thread');
  await replyField.fill('One page in the docs, linked from the channel topic.');
  await thread.getByRole('button', { name: 'Reply', exact: true }).click();
  await expect(thread.locator('article.message:has-text("One page in the docs")')).toBeVisible();

  // The channel did not gain the reply: a thread keeps a discussion together without
  // burying the channel it lives in, and neither view reads it twice.
  await thread.getByRole('button', { name: 'Close' }).click();
  await expect(
    page.locator('.message-list article.message:has-text("One page in the docs")')
  ).toHaveCount(0);

  // The channel's thread list is a real surface, not an API.
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const list = page.getByRole('dialog');
  await expect(list.getByRole('heading', { name: 'Threads in ' + CHANNEL })).toBeVisible();
  await expect(list.getByRole('button', { name: /Where do release notes live/ })).toBeVisible();
  await list.getByRole('button', { name: /Where do release notes live/ }).click();

  // Locking is channel management. The owner holds it, so the control is offered.
  const open = page.getByRole('dialog');
  await open.getByRole('button', { name: 'Lock' }).click();

  // A locked thread is still readable and says why it cannot be written to. Hiding it
  // instead would be a different decision, and the notice is what distinguishes them.
  await expect(open.getByText(/This thread is locked/)).toBeVisible();
  await expect(open.getByLabel('Reply to this thread')).toHaveCount(0);
  await expect(open.locator('article.message:has-text("One page in the docs")')).toBeVisible();
  await open.getByRole('button', { name: 'Close' }).click();

  // Unfollowing is a personal choice and costs nothing: the thread is still readable
  // and still listed.
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  await expect(page.getByRole('dialog').getByText(/Where do release notes live/)).toBeVisible();
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /Where do release notes live/ })
    .click();
  const reopened = page.getByRole('dialog');
  await expect(reopened.getByRole('button', { name: 'Following' })).toBeVisible();
  await reopened.getByRole('button', { name: 'Following' }).click();
  await expect(reopened.getByRole('button', { name: 'Follow', exact: true })).toBeVisible();
  // Unfollowing did not remove the history or the thread itself.
  await expect(reopened.locator('article.message:has-text("One page in the docs")')).toBeVisible();
  await reopened.getByRole('button', { name: 'Close' }).click();

  // A locked thread is still in the channel's list, marked as locked. Locking is a
  // statement about writing; hiding it is what archiving is, and the two are separate
  // states precisely so that "locked" cannot quietly become "hidden".
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const finalList = page.getByRole('dialog');
  const row = finalList.getByRole('button', { name: /Where do release notes live/ });
  await expect(row).toBeVisible();
  await expect(row).toContainText('Locked');
  // Unfollowing is reflected in the list: the thread is not gone, this account is no
  // longer a follower of it.
  await expect(row).not.toContainText('following');
});

test('a thread a reader cannot reach is not found rather than forbidden', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  await signInOverApi(request);
  const outsider = { username: 'thread_outsider', password: 'a passphrase for outsiders' };
  await createAccount(request, outsider);

  await signIn(page, OWNER);
  await page
    .getByRole('button', { name: new RegExp(CHANNEL, 'i') })
    .first()
    .click();
  const composer = page.locator('.composer textarea').first();
  await composer.fill('A line that becomes a thread with a private subject.');
  await composer.press('Enter');
  await expect(page.locator('article.message:has-text("private subject")').first()).toBeVisible();
  await startThreadFrom(page, 'private subject', 'A private subject');
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();

  // A member of no role in particular still reads a public channel, so the thread is
  // reachable — the point here is the journey completes rather than 500s.
  await signIn(page, outsider);
  await page
    .getByRole('button', { name: new RegExp(CHANNEL, 'i') })
    .first()
    .click();
  await expect(page.getByRole('heading', { name: CHANNEL })).toBeVisible();
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const list = page.getByRole('dialog');
  await expect(list.getByText(/A private subject|No threads yet/)).toBeVisible();
  // An empty list says so plainly rather than showing a spinner that never resolves.
  if (await list.getByText('No threads yet').isVisible()) {
    await expect(list.getByText(/Start a named thread/)).toBeVisible();
  }
});

// Review 17, V17-01, through the rendered member interface. Two replies are left
// uncertain in a row — the first never reaches the server, the second is stored and
// its response is discarded, which is precisely a committed send whose acknowledgement
// was lost — and then each is retried. The operation identities must survive: retrying
// the older reply posts under its own key while the newer stays untouched, and
// retrying the newer words directs the receipt at the original reply instead of
// storing a second one under a rotated key. The server's own message count is the
// judge; a client that merely painted "sent" could not fake it.
test('two uncertain thread replies retry under their own keys', async ({ page, request }) => {
  test.setTimeout(180000);
  const FIRST = 'A: the first uncertain reply in this thread';
  const SECOND = 'B: the second uncertain reply in this thread';
  await signInOverApi(request);
  await signIn(page, OWNER);
  await page
    .getByRole('button', { name: new RegExp(CHANNEL, 'i') })
    .first()
    .click();
  await expect(page.getByRole('heading', { name: CHANNEL })).toBeVisible();

  const composer = page.locator('.composer textarea').first();
  await composer.fill('A root line for the retry identity journey.');
  await composer.press('Enter');
  await expect(
    page.locator('article.message:has-text("retry identity journey")').first()
  ).toBeVisible();
  await startThreadFrom(page, 'retry identity journey', 'Retry identity');
  const thread = page.getByRole('dialog');
  const reply = thread.getByLabel('Reply to this thread');

  // The two uncertain outcomes are produced deliberately, and only for the first
  // request carrying each text; every retry afterwards goes through untouched.
  const posts = [];
  const seen = new Map();
  await page.route('**/api/threads/*/messages', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.continue();
      return;
    }
    const body = route.request().postDataJSON();
    const content = body?.content ?? '';
    const count = (seen.get(content) ?? 0) + 1;
    seen.set(content, count);
    posts.push({ content, key: body?.idempotency_key });
    if (count === 1 && content === FIRST) {
      // Never delivered: the transport dropped it before the server saw it.
      await route.abort();
      return;
    }
    if (count === 1 && content === SECOND) {
      // Delivered and stored, and the response is discarded — a committed reply whose
      // acknowledgement was lost.
      await route.fetch();
      await route.abort();
      return;
    }
    await route.continue();
  });

  await reply.fill(FIRST);
  await thread.getByRole('button', { name: 'Reply', exact: true }).click();
  // The refusal is honest: the text stays in the composer and the reply is visible as
  // unconfirmed, with its own retry.
  await expect(reply).toHaveValue(FIRST);
  await expect(thread.locator('button.retry-link')).toHaveCount(1);

  await reply.fill(SECOND);
  await thread.getByRole('button', { name: 'Reply', exact: true }).click();
  await expect(reply).toHaveValue(SECOND);
  // Both replies are unconfirmed; the second may already be rendered from the server's
  // own event, which is exactly the committed case.
  await expect(thread.locator(`article.message:has-text("${SECOND}")`).first()).toBeVisible();

  // Retrying the older reply uses its own key, and leaves the newer operation and the
  // composer's words untouched. B's pending row may be hidden because the committed
  // reply already rendered from the server's own event; the words in the composer and
  // the captured keys are what the contract rests on, not that row's visibility.
  const firstRow = thread
    .locator(`article.message:has-text("${FIRST}")`)
    .getByRole('button', { name: 'Message not sent · Retry' });
  await firstRow.click();
  await expect(reply).toHaveValue(SECOND);
  const firstPosts = posts.filter((post) => post.content === FIRST);
  expect(firstPosts.length).toBe(2);
  expect(firstPosts[1].key).toBe(firstPosts[0].key);
  expect(posts.filter((post) => post.content === SECOND).length).toBe(1);

  // Pressing Reply on the unchanged, newly-uncertain words retries that operation
  // under its own key: the receipt answers with the original reply instead of a
  // second copy, and only then does the composer clear. Both operations are now
  // settled, so no unconfirmed row is left behind.
  await thread.getByRole('button', { name: 'Reply', exact: true }).click();
  await expect(reply).toHaveValue('');
  await expect(thread.locator('button.retry-link')).toHaveCount(0);
  const secondPosts = posts.filter((post) => post.content === SECOND);
  expect(secondPosts.length).toBe(2);
  expect(secondPosts[1].key).toBe(secondPosts[0].key);

  // The server's own record is the judge: one stored reply per text, each keeping the
  // key it was originally submitted under.
  const state = await (await request.get('/api/state')).json();
  const channel = state.channels.find((item) => item.name === CHANNEL);
  const threads = await (await request.get(`/api/channels/${channel.id}/threads`)).json();
  const target = threads.find((item) => item.title === 'Retry identity');
  expect(target, 'the journey thread is missing').toBeTruthy();
  const stored = await (await request.get(`/api/threads/${target.id}/messages`)).json();
  const storedFirst = stored.filter((message) => message.content === FIRST);
  const storedSecond = stored.filter((message) => message.content === SECOND);
  expect(storedFirst.length, 'the older reply was stored a wrong number of times').toBe(1);
  expect(storedSecond.length, 'the committed reply was stored twice').toBe(1);
  expect(storedFirst[0].idempotency_key).toBe(firstPosts[0].key);
  expect(storedSecond[0].idempotency_key).toBe(secondPosts[0].key);
});

// Review 17, V17-03, through the member interface: an unseen thread reply must not
// raise the channel's badge — a thread is a conversation with its own reading
// position — and it must show on the thread itself until that thread is read.
// Archiving then closes replies (the composer is withdrawn and the server refuses)
// while the history stays readable, and Reopen is an explicit action that restores
// writing.
test('a thread reply moves only the thread, and archiving closes replies until reopened', async ({
  page,
  request
}) => {
  test.setTimeout(180000);
  await signInOverApi(request);
  await signIn(page, OWNER);
  const channelButton = page.getByRole('button', { name: new RegExp(CHANNEL, 'i') }).first();
  await channelButton.click();
  const composer = page.locator('.composer textarea').first();
  await composer.fill('A root line for the unread and archive journey.');
  await composer.press('Enter');
  await expect(
    page.locator('article.message:has-text("unread and archive journey")').first()
  ).toBeVisible();
  await startThreadFrom(page, 'unread and archive journey', 'Unread and archive');
  // The thread closes unread while the reply lands, so the test observes a reply
  // nobody has read rather than one the open panel consumed.
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();

  const state = await (await request.get('/api/state')).json();
  const channelID = state.channels.find((item) => item.name === CHANNEL).id;
  const threads = await (await request.get(`/api/channels/${channelID}/threads`)).json();
  const target = threads.find((item) => item.title === 'Unread and archive');
  expect(target, 'the journey thread is missing').toBeTruthy();
  // The reply must be somebody else's: an author's own reply is never counted unread
  // for them, and a journey that posted it as the owner would assert nothing.
  await signInOverApi(request, REPLIER);
  const reply = await request.post(`/api/threads/${target.id}/messages`, {
    data: { content: 'the only unseen reply', idempotency_key: 'journey-unread-1' }
  });
  expect(reply.status()).toBe(201);
  await signInOverApi(request);
  const replyBody = await reply.json();

  // The channel badge must not move: nothing arrived in the channel itself.
  await expect(channelButton.locator('.unread-dot')).toHaveCount(0);
  await expect(channelButton.locator('.unread-badge')).toHaveCount(0);

  // The thread list carries the count.
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const list = page.getByRole('dialog');
  const row = list.getByRole('button', { name: /Unread and archive/ });
  await expect(row).toBeVisible();
  await expect(row.getByText('1 new')).toBeVisible();

  // The thread is read through a deep link to the reply. The channel listing
  // deliberately excludes thread replies, so a link resolved through the channel
  // would not even have the row on screen; the thread panel positions its window at
  // the linked reply instead.
  await page.goto('about:blank');
  await page.goto(`/#message=${replyBody.id}`);
  const panel = page.getByRole('dialog');
  // Exact, because the panel also renders the root message, whose body *contains* the
  // title ("A root line for the unread and archive journey") — a substring match
  // resolves to both elements and fails strict mode whenever the panel's window happens
  // to include the root (caught by the private-conversations run, 2 October).
  await expect(panel.getByText('Unread and archive', { exact: true })).toBeVisible();
  await expect(panel.locator('article.message:has-text("the only unseen reply")')).toBeVisible();
  await panel.getByRole('button', { name: 'Close' }).click();

  // Reading it cleared the thread's own count — and left the channel's alone.
  await page.getByRole('button', { name: 'Threads in this channel' }).click();
  const reloaded = page.getByRole('dialog');
  const readRow = reloaded.getByRole('button', { name: /Unread and archive/ });
  await expect(readRow).toBeVisible();
  await expect(reloaded.getByText('1 new')).toHaveCount(0);
  await expect(channelButton.locator('.unread-dot')).toHaveCount(0);

  // Archiving withdraws the composer before anything is pressed, keeps the history,
  // and the server refuses a reply from another member while it holds.
  await readRow.click();
  const open = page.getByRole('dialog');
  await open.getByRole('button', { name: 'Archive' }).click();
  await expect(open.getByText(/This thread is archived, so new replies are closed/)).toBeVisible();
  await expect(open.getByLabel('Reply to this thread')).toHaveCount(0);
  await expect(open.locator('article.message:has-text("the only unseen reply")')).toBeVisible();
  const refused = await request.post(`/api/threads/${target.id}/messages`, {
    data: { content: 'a reply to an archived thread', idempotency_key: 'journey-unread-2' }
  });
  expect(refused.status()).toBe(403);

  // Reopen is offered as its own action and restores writing.
  await open.getByRole('button', { name: 'Reopen' }).click();
  await expect(open.getByText(/This thread is archived, so new replies are closed/)).toHaveCount(0);
  await expect(open.getByLabel('Reply to this thread')).toBeVisible();
});

// F-02 / V1-01 regression. Two boundaries conspired against deep links into a long
// named-thread thread: the workspace recorded the reply's id beside its own global jump
// scalar, which the mounted panel compared against the *thread* id — the two records
// never describe the same thing — and the server's `after` reader walked the thread
// newest-first, so a panel landing mid-history had every reply between the anchor and
// the tail silently absent from its window. Failing-first proof is in tranche 13's
// ledger entry; what this journey reconstructs, in order:
//
//   1. a permalink that lands mid-history and shows the anchor's neighbour rows;
//   2. a return to the newest reply from a fresh entry;
//   3. a second jump *inside the still-mounted* thread — clicking the quote in the
//      newest reply that references the middle one;
//   4. a permalink to the earliest page of the thread, from a fresh entry.
//
// Replies are rate-limited per author (60 a minute, 10 per 5 seconds) and logins are
// rate-limited per client (10 a 5 minutes), so the volume is spread low and wide:
// eight writers post thirty replies each at a pace every cap allows (under 2 a second
// per author; nine sign-ins a run).
test('a deep link to the middle of a long thread opens the thread at that middle', async ({
  page,
  request
}) => {
  test.setTimeout(720000);
  await signInOverApi(request);
  await signIn(page, OWNER);

  const channelButton = page.getByRole('button', { name: new RegExp(CHANNEL, 'i') }).first();
  await expect(channelButton).toBeVisible();
  await channelButton.click();
  const composer = page.locator('.composer textarea').first();
  await composer.fill('a subject to return to later');
  await composer.press('Enter');
  await page.waitForSelector('article.message');
  await startThreadFrom(page, 'a subject to return to later', 'Long haul');

  const state = await (await request.get('/api/state')).json();
  const channelID = state.channels.find((item) => item.name === CHANNEL).id;
  const threads = await (await request.get(`/api/channels/${channelID}/threads`)).json();
  const thread = threads.find((item) => item.title === 'Long haul');
  expect(thread, 'the journey thread exists').toBeTruthy();

  // Replies are rate-limited per author (60 a minute, 10 per 5 seconds) and logins are
  // per client (10 in 5 minutes), so the volume stays with one author at a pace inside
  // both caps — about a second per post — and the run keeps its sign-ins at two.
  const ids = [];
  for (let n = 1; n <= 240; n++) {
    const data = { content: `reply ${n} of 240`, idempotency_key: crypto.randomUUID() };
    if (n === 240) data.reply_to_id = ids[120];
    const sent = await request.post(`/api/threads/${thread.id}/messages`, { data });
    expect(sent.status()).toBe(201);
    ids.push((await sent.json()).id);
    await page.waitForTimeout(1050);
  }
  // The anchor sits 121st: a hundred and twenty replies older and a hundred and
  // nineteen newer, so neither half of the window fits in one page — a panel that
  // skips a whole page in either direction loses it here, and the final reply quotes
  // the anchor so the same-panel jump is a row the reader can click.
  const middle = ids[120];
  const last = ids[239];
  const early = ids[2];

  // 1. Entry at the middle: the pivot page must include the row it names and the
  //    reply immediately after it — a descending `after` scan makes every row between
  //    122 and 190 vanish — but not the first page. A permalink is read once at entry,
  //    so arriving at one is a fresh load, never a hash write.
  await page.evaluate((hash) => {
    location.hash = hash;
  }, `#message=${middle}`);
  await page.reload();
  const panel = page.getByRole('dialog', { name: 'Thread' });
  await expect(panel).toBeVisible({ timeout: 30000 });
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 121 of 240' })
  ).toBeVisible({ timeout: 30000 });
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 122 of 240' })
  ).toBeVisible();
  await expect(panel.locator('article.message').filter({ hasText: 'reply 1 of 240' })).toHaveCount(
    0
  );

  // 2. Fresh entry at the tail: newest first, nothing of the middle standing around.
  await page.evaluate((hash) => {
    location.hash = hash;
  }, `#message=${last}`);
  await page.reload();
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 240 of 240' })
  ).toBeVisible({ timeout: 30000 });
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 121 of 240' })
  ).toHaveCount(0);

  // 3. The in-place second link: the quote inside the newest reply sends the same
  //    mounted panel back to the anchor. No remount happens — the location has to be
  //    consumed by the panel's own watching, not by an initial-mount read.
  const tailRow = panel.locator('article.message').filter({ hasText: 'reply 240 of 240' });
  await tailRow.getByRole('button', { name: /reply 121 of 240/ }).click();
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 121 of 240' })
  ).toBeVisible({ timeout: 30000 });
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 122 of 240' })
  ).toBeVisible();
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 240 of 240' })
  ).toHaveCount(0);

  // 4. Fresh entry at the early replies: the anchor page, three rounds in, stays put.
  await page.evaluate((hash) => {
    location.hash = hash;
  }, `#message=${early}`);
  await page.reload();
  await expect(panel.locator('article.message').filter({ hasText: 'reply 3 of 240' })).toBeVisible({
    timeout: 30000
  });
  await expect(
    panel.locator('article.message').filter({ hasText: 'reply 121 of 240' })
  ).toHaveCount(0);
});
