// Builds a real schema-13 data directory for scripts/upgrade-schema13-private.mjs.
//
// The point of this file is that the fixture is *real*. A schema-13 database cannot
// be written by a current binary and cannot be assembled by writing old-looking rows
// into a current one: either way it is a current database, and the rehearsal would
// pass while proving nothing about the upgrade it exists to qualify.
//
// So a preserved prior executable is run against an empty data directory, and the
// rehearsal's subject — an accepted direct conversation with private history and
// submitted evidence, plus a pending request and a block — is created through that
// executable's own HTTP surface. Everything in the resulting directory was written by
// the binary that is supposed to have written it.
//
// The seed writes `seed-manifest.json` beside the database, recording the exact
// identities it created (accounts, conversation, message, evidence, block). The
// verifier compares the preserved rows by value *and* checks that these identities
// still resolve after the migration: value equality alone would accept a migration
// that renumbered rows, because the same values could be found under new ids.

import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, existsSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'node:net';

async function freePort() {
  const listener = createServer();
  await new Promise((done, reject) => {
    listener.once('error', reject);
    listener.listen(0, '127.0.0.1', done);
  });
  const { port } = listener.address();
  await new Promise((done) => listener.close(done));
  return port;
}

// The seed's server is tracked so an interrupted parent takes it down: the finally
// below covers the paths this function can see, and the exit handler covers the ones
// it cannot (a signal while a request is in flight, an uncaught throw elsewhere).
const liveChildren = new Set();

function killLiveChildren() {
  for (const child of liveChildren) {
    try {
      child.kill('SIGKILL');
    } catch {
      /* already exited */
    }
  }
  liveChildren.clear();
}

process.on('exit', killLiveChildren);
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, async () => {
    console.error(`Seed interrupted (${signal}); stopping the seed server.`);
    // SIGTERM first so the server closes its database cleanly on the way out.
    for (const child of liveChildren) child.kill('SIGTERM');
    await new Promise((done) => setTimeout(done, 1500));
    killLiveChildren();
    process.exit(1);
  });
}

async function waitReady(base, child, getOutput) {
  for (let attempt = 0; attempt < 150; attempt++) {
    if (child.exitCode !== null || child.signalCode !== null)
      throw new Error(`the prior executable exited early:\n${getOutput()}`);
    try {
      const response = await fetch(`${base}/healthz`, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return;
    } catch {
      // Not listening yet.
    }
    await new Promise((done) => setTimeout(done, 200));
  }
  throw new Error(`the prior executable never became ready:\n${getOutput()}`);
}

export async function makeSchema13Seed({ seedDir, oldBinary, cwd }) {
  if (existsSync(seedDir)) rmSync(seedDir, { recursive: true, force: true });
  mkdirSync(seedDir, { recursive: true });
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const env = {
    ...process.env,
    BRRP_DATA_DIR: seedDir,
    BRRP_BASE_URL: base,
    BRRP_ADDR: `127.0.0.1:${port}`,
    BRRP_TLS_MODE: 'http-local',
    BRRP_LOG_LEVEL: 'warn',
    // Private conversations are default-off, and the rehearsal's subject is private
    // state. A seed without them would exercise the community path twice and the
    // private path never.
    BRRP_ENABLE_PRIVATE_CONVERSATIONS: '1'
  };
  // The prior release needs its own frontend assets, so the script is resolved from the
  // archive root rather than from whatever happens to be in the working directory.
  const child = spawn(oldBinary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'], cwd });
  liveChildren.add(child);
  let output = '';
  child.stdout.on('data', (chunk) => (output += chunk));
  child.stderr.on('data', (chunk) => (output += chunk));

  try {
    await waitReady(base, child, () => output);

    // The first administrator exists only after the setup ceremony, and the rehearsal's
    // subject is private state, which needs an administrator to create it.
    const setupToken = await readSetupToken(seedDir);
    const setup = await post(`${base}/api/setup`, {
      token: setupToken,
      server_name: 'Schema 13 rehearsal',
      username: 'seed_owner',
      password: 'a rehearsal owner passphrase'
    });
    if (!setup.ok) throw new Error(`setup: ${setup.status} ${await setup.text()}`);
    const ownerCookie = (setup.headers.getSetCookie?.() ?? []).map((row) => row.split(';')[0]).join('; ');
    if (!ownerCookie) throw new Error('setup returned no session cookie');

    const first = await post(
      `${base}/api/admin/users`,
      { username: 'seed_alice', password: 'a rehearsal member passphrase', display_name: 'Alice' },
      ownerCookie
    );
    if (!first.ok) throw new Error(`create alice: ${first.status} ${await first.text()}`);
    const aliceID = (await first.json()).id;
    const second = await post(
      `${base}/api/admin/users`,
      { username: 'seed_bob', password: 'a rehearsal member passphrase', display_name: 'Bob' },
      ownerCookie
    );
    if (!second.ok) throw new Error(`create bob: ${second.status} ${await second.text()}`);
    const bobID = (await second.json()).id;
    const third = await post(
      `${base}/api/admin/users`,
      { username: 'seed_carol', password: 'a rehearsal member passphrase', display_name: 'Carol' },
      ownerCookie
    );
    if (!third.ok) throw new Error(`create carol: ${third.status} ${await third.text()}`);
    const carolID = (await third.json()).id;

    // A message in a community channel, so the upgrade has ordinary content to carry.
    const channels = await get(`${base}/api/channels`, ownerCookie);
    const channel = channels.find((row) => row.type === 'text');
    if (!channel) throw new Error('the seed has no text channel to post into');
    const sent = await post(
      `${base}/api/channels/${channel.id}/messages`,
      { content: 'a message written before the upgrade', idempotency_key: 'seed-message-one' },
      ownerCookie
    );
    if (!sent.ok) throw new Error(`post: ${sent.status} ${await sent.text()}`);

    // The accepted conversation with private history, and evidence submitted against
    // one of its messages — the two things the verifier requires and the previous
    // seed did not create, which made every `--make-seed` run fail its own checks
    // (review 17, V17-05).
    const signIn = async (username) => {
      const session = await post(`${base}/api/login`, {
        username,
        password: 'a rehearsal member passphrase'
      });
      if (!session.ok) throw new Error(`sign in ${username}: ${session.status}`);
      return (session.headers.getSetCookie?.() ?? [])
        .map((row) => row.split(';')[0])
        .join('; ');
    };
    const aliceCookie = await signIn('seed_alice');
    const bobCookie = await signIn('seed_bob');
    const carolCookie = await signIn('seed_carol');
    const conversation = await post(
      `${base}/api/directs`,
      { user_id: bobID, introduction: 'schema thirteen hello' },
      aliceCookie
    );
    if (!conversation.ok)
      throw new Error(`direct request: ${conversation.status} ${await conversation.text()}`);
    const conversationID = (await conversation.json()).id;
    const accepted = await post(`${base}/api/directs/${conversationID}/accept`, {}, bobCookie);
    if (!accepted.ok && accepted.status !== 409)
      throw new Error(`accept: ${accepted.status} ${await accepted.text()}`);
    const privateSend = await post(
      `${base}/api/conversations/${conversationID}/messages`,
      { content: 'a schema thirteen private message', idempotency_key: 'seed-s13-private-one' },
      aliceCookie
    );
    if (!privateSend.ok)
      throw new Error(`private message: ${privateSend.status} ${await privateSend.text()}`);
    const privateMessageID = (await privateSend.json()).id;
    const preview = await get(
      `${base}/api/messages/${privateMessageID}/private-report-preview`,
      aliceCookie
    );
    const reported = await post(
      `${base}/api/messages/${privateMessageID}/private-report`,
      { reason: 'schema thirteen rehearsal evidence', version: preview.version },
      aliceCookie
    );
    if (!reported.ok && reported.status !== 200)
      throw new Error(`private evidence: ${reported.status} ${await reported.text()}`);
    const evidenceID = (await reported.json())?.id ?? '';

    // A second, pending request from carol. The attempt identity migration 14 adds is
    // the rehearsal's subject, and it must be distinct from the accepted pair's.
    const pending = await post(
      `${base}/api/directs`,
      { user_id: aliceID, introduction: 'a pending schema thirteen introduction' },
      carolCookie
    );
    if (!pending.ok)
      throw new Error(`pending request: ${pending.status} ${await pending.text()}`);
    const pendingID = (await pending.json()).id;

    // A block, so the preserved-set comparison has a row to compare rather than an
    // empty table that any change would leave empty too. The block route may not exist
    // in every preserved build; its absence is recorded rather than guessed at.
    const blocked = await post(`${base}/api/blocks`, { user_id: carolID }, aliceCookie);
    const blockRecorded = blocked.ok;
    if (!blockRecorded) console.log(`  note: the preserved build has no block route (${blocked.status})`);

    writeFileSync(
      join(seedDir, 'seed-manifest.json'),
      JSON.stringify(
        {
          schema: 13,
          accounts: { alice: aliceID, bob: bobID, carol: carolID },
          channel_message: (await sent.json()).id,
          conversation: conversationID,
          private_message: privateMessageID,
          evidence: evidenceID,
          pending_request: pendingID,
          block_recorded: blockRecorded
        },
        null,
        2
      )
    );
    console.log(`schema-13 seed written to ${seedDir}`);
  } finally {
    child.kill('SIGTERM');
    // The database must be closed before the directory is used, or a WAL write can
    // land after the rehearsal has started reading it.
    await new Promise((done) => setTimeout(done, 800));
    if (child.exitCode === null) child.kill('SIGKILL');
    liveChildren.delete(child);
  }
}

async function readSetupToken(seedDir) {
  for (let attempt = 0; attempt < 50; attempt++) {
    const { readFileSync } = await import('node:fs');
    try {
      return readFileSync(join(seedDir, 'setup-token'), 'utf8').trim();
    } catch {
      await new Promise((done) => setTimeout(done, 100));
    }
  }
  throw new Error('the prior executable never wrote a setup token');
}

async function post(url, body, cookie) {
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(cookie ? { Cookie: cookie } : {})
    },
    body: JSON.stringify(body)
  });
  return response;
}

async function get(url, cookie) {
  const response = await fetch(url, { headers: cookie ? { Cookie: cookie } : {} });
  if (!response.ok) throw new Error(`GET ${url}: ${response.status}`);
  return response.json();
}

// json reads a response body without consuming the caller's copy of it.
async function json(response) {
  return response.json();
}
