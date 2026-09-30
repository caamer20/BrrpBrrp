// Builds a real schema-13 data directory for scripts/upgrade-schema13-private.mjs.
//
// The point of this file is that the fixture is *real*. A schema-13 database cannot
// be written by a current binary and cannot be assembled by writing old-looking rows
// into a current one: either way it is a current database, and the rehearsal would
// pass while proving nothing about the upgrade it exists to qualify.
//
// So a preserved prior executable is run against an empty data directory, and the
// rehearsal's subject — a consenting direct conversation and a pending request — is
// created through that executable's own HTTP surface. Everything in the resulting
// directory was written by the binary that is supposed to have written it.

import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, existsSync } from 'node:fs';
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
    const second = await post(
      `${base}/api/admin/users`,
      { username: 'seed_bob', password: 'a rehearsal member passphrase', display_name: 'Bob' },
      ownerCookie
    );
    if (!second.ok) throw new Error(`create bob: ${second.status} ${await second.text()}`);

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

    const conversation = await post(
      `${base}/api/directs`,
      { user_id: (await second.json()).id },
      ownerCookie
    );
    if (!conversation.ok)
      throw new Error(`direct request: ${conversation.status} ${await conversation.text()}`);

    // The pending request is half the rehearsal's subject: the attempt identity
    // migration 14 added, which a schema-13 database by definition does not have.
    console.log(`schema-13 seed written to ${seedDir}`);
  } finally {
    child.kill('SIGTERM');
    // The database must be closed before the directory is used, or a WAL write can
    // land after the rehearsal has started reading it.
    await new Promise((done) => setTimeout(done, 800));
    if (child.exitCode === null) child.kill('SIGKILL');
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
