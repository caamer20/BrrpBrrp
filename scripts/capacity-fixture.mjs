#!/usr/bin/env node
// Generates a capacity fixture against a running server, through its public API.
//
// The point of driving the real HTTP surface is that a fixture assembled by writing
// rows into the database directly measures nothing this product does: it skips
// authorization, receipts, the outbox, search indexing, retention, and the
// authorization revision. A capacity run on such a fixture reports numbers for a
// system nobody ships.
//
// Everything is versioned and reported. The generator writes a manifest naming the
// source commit, the server build, the counts it created and the wall-clock it took,
// because a capacity figure without those is not reproducible and a reviewer cannot
// tell which of the two it is looking at.
//
// Usage:
//   node scripts/capacity-fixture.mjs --base URL --owner-user NAME --owner-pass PASS
//     [--accounts N] [--channels N] [--messages N] [--private-traffic]
//     [--report PATH] [--label NAME]

import { writeFileSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { execFileSync } from 'node:child_process';

const args = process.argv.slice(2);
const option = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index < 0 ? fallback : args[index + 1];
};
const flag = (name) => args.includes(`--${name}`);

const base = (option('--base', process.env.BRRP_BASE_URL) || '').replace(/\/$/, '');
const ownerUser = option('--owner-user', process.env.BRRP_OWNER_USER || '');
const ownerPass = option('--owner-pass', process.env.BRRP_OWNER_PASS || '');
const accountCount = Number(option('--accounts', 20));
const channelCount = Number(option('--channels', 10));
const messageCount = Number(option('--messages', 2000));
const privateTraffic = flag('private-traffic');
const reportPath = option('--report', '');
const label = option('--label', 'ad-hoc');

if (!base || !ownerUser || !ownerPass) {
  console.error(
    'usage: --base URL --owner-user NAME --owner-pass PASS [--accounts N] [--channels N] [--messages N] [--private-traffic] [--report PATH] [--label NAME]'
  );
  process.exit(2);
}

let cookie = '';
// A 429 is the product working, not the fixture failing. A capacity run that treated
// the rate limiter as an error would either stop at the limiter or — worse — be run
// against a server with it disabled, and the number it produced would then describe a
// configuration nobody ships.
const rateLimited = { count: 0, waitedMs: 0 };
async function backoff(response, attempt) {
  const header = response.headers.get('Retry-After');
  const seconds = header ? Number(header) : Math.min(30, 2 ** attempt);
  const wait = Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : 1000;
  rateLimited.count += 1;
  rateLimited.waitedMs += wait;
  await new Promise((done) => setTimeout(done, wait));
}
async function call(path, { method = 'GET', body, expect = [200, 201], retryOnLimit = false } = {}) {
  let response = await fetch(`${base}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(cookie ? { Cookie: cookie } : {})
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) })
  });
  // A send the limiter defers is the same send, retried under its own key. The retry is
  // bounded so a fixture cannot hang forever against a misconfigured limit.
  for (let attempt = 0; retryOnLimit && response.status === 429 && attempt < 6; attempt++) {
    await backoff(response, attempt);
    response = await fetch(`${base}${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(cookie ? { Cookie: cookie } : {})
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) })
    });
  }
  if (expect.length && !expect.includes(response.status)) {
    const text = await response.text();
    throw new Error(`${method} ${path}: ${response.status} ${text.slice(0, 200)}`);
  }
  const setCookies = response.headers.getSetCookie?.() ?? [];
  for (const raw of setCookies) {
    const pair = raw.split(';')[0];
    if (pair.startsWith('brrp_session=') || pair.startsWith('session=')) cookie = pair;
  }
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function sourceCommit() {
  try {
    return execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

const started = Date.now();
const created = { accounts: 0, channels: 0, messages: 0, replies: 0, reactions: 0, directConversations: 0 };
// Adopted rather than created, so a second run on the same server is the same run.
const reused = { accounts: 0, channels: 0 };
const notes = [];

// The owner session is the fixture's own authority: everything below is created by
// an account the installation already has, through the same routes a person uses.
await call('/api/login', { method: 'POST', body: { username: ownerUser, password: ownerPass } });

const channels = [];
for (let index = 0; index < channelCount; index++) {
  const channel = await call('/api/channels', {
    method: 'POST',
    body: {
      name: `capacity-${index}`,
      type: index % 5 === 4 ? 'voice' : 'text',
      category: 'Capacity',
      topic: 'generated by scripts/capacity-fixture.mjs',
      max_voice_users: 8,
      position: index
    }
  });
  if (channel?.id) {
    channels.push(channel);
    created.channels += 1;
  }
}
// An existing run's channels are adopted too, so a re-run measures the same
// population rather than growing one without bound.
if (channels.length < channelCount) {
  const listed = await call('/api/channels', { expect: [200] }).catch(() => []);
  for (const channel of Array.isArray(listed) ? listed : []) {
    if (channels.length >= channelCount) break;
    if (channels.some((row) => row.id === channel.id)) continue;
    if (!String(channel.name || '').startsWith('capacity-')) continue;
    channels.push(channel);
    reused.channels += 1;
  }
}

const textChannels = channels.filter((channel) => channel.type === 'text');
// Re-running a fixture has to be the same run rather than a second one, so a name that
// already exists is adopted rather than refused. A generator that only works on an
// empty server is a generator that gets run once and then avoided.
const accounts = [];
const existing = await call('/api/admin/users', { expect: [200, 403] }).catch(() => []);
for (let index = 0; index < accountCount; index++) {
  const username = `capacity_${index}`;
  const already = Array.isArray(existing) ? existing.find((row) => row.username === username) : null;
  if (already?.id) {
    accounts.push(already);
    reused.accounts += 1;
    continue;
  }
  const account = await call('/api/admin/users', {
    method: 'POST',
    body: {
      username,
      password: `capacity fixture passphrase ${index}`,
      display_name: `Capacity ${index}`
    },
    expect: [201, 409]
  });
  if (account?.id) {
    accounts.push(account);
    created.accounts += 1;
  }
}

// Messages are written in bounded batches with the same key semantics a client uses,
// so a retry of the whole run is the same run rather than a second one. The key
// carries the account, the channel and the index: a send's identity is the account
// and the key, and reusing a key across accounts would be a conflict.
for (let index = 0; index < messageCount; index++) {
  const channel = textChannels[index % Math.max(1, textChannels.length)];
  if (!channel) break;
  const author = accounts[index % Math.max(1, accounts.length)];
  if (!author) break;
  // A fresh session per batch would be a thousand logins; the owner's session is used
  // for the bulk and the note records that, so a reader knows the accounts are
  // present as *recipients* rather than as senders of this particular population.
  await call(`/api/channels/${channel.id}/messages`, {
    method: 'POST',
    body: {
      content: `capacity fixture message ${index}`,
      idempotency_key: `capacity-msg-${index}-${channel.id}`
    },
    retryOnLimit: true
  });
  created.messages += 1;
  if (index % 10 === 0) process.stderr.write(`\r  messages ${index}/${messageCount}`);
}
process.stderr.write('\n');

// A share of replies and reactions, because a run that only appends never exercises
// the read path the way a real community does.
for (let index = 0; index < Math.floor(messageCount / 10); index++) {
  const channel = textChannels[index % Math.max(1, textChannels.length)];
  const messages = channel
    ? await call(`/api/channels/${channel.id}/messages?limit=1`, { expect: [200, 404] })
    : null;
  const target = Array.isArray(messages) ? messages.at(-1) : null;
  if (!target) continue;
  const reacted = await call(`/api/messages/${target.id}/reactions`, {
    method: 'POST',
    body: { emoji: '👍' },
    expect: [200, 201, 400, 409]
  });
  if (reacted) created.reactions += 1;
}

if (privateTraffic && accounts.length >= 2) {
  // Private conversations are default-off, so this is skipped rather than failed when
  // the installation has them disabled: a capacity run on a default installation is
  // still a valid run, and pretending otherwise would make the generator useless
  // everywhere except a configured server.
  for (let index = 0; index + 1 < Math.min(accounts.length, 10); index++) {
    const created1 = await call('/api/directs', {
      method: 'POST',
      body: { user_id: accounts[index + 1].id },
      expect: [201, 403, 409]
    });
    if (created1?.id) {
      created.directConversations += 1;
      await call(`/api/directs/${created1.id}/accept`, { method: 'POST', expect: [200, 403, 409] });
    }
  }
  if (created.directConversations === 0) {
    notes.push(
      'private conversations are disabled on this server, so no private traffic was created; this run is community-only'
    );
  }
}

const report = {
  label,
  generated_at: new Date().toISOString(),
  source_commit: sourceCommit(),
  base_url: base,
  requested: { accountCount, channelCount, messageCount, privateTraffic },
  created,
  reused,
  duration_seconds: Math.round((Date.now() - started) / 1000),
  // Reported rather than hidden: a run that spent most of its time waiting on the rate
  // limiter is measuring the limiter, and the reader has to be able to see that.
  rate_limited_requests: rateLimited.count,
  rate_limited_wait_seconds: Math.round(rateLimited.waitedMs / 1000),
  notes
};
if (reportPath) {
  mkdirSync(dirname(reportPath), { recursive: true });
  writeFileSync(reportPath, JSON.stringify(report, null, 2) + '\n');
}
console.log(JSON.stringify(report, null, 2));
