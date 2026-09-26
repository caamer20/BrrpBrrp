import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const dataDir = await mkdtemp(join(tmpdir(), 'brrpbrrp-browser-'));
const binary = resolve(process.env.BRRP_E2E_BINARY || '../dist/brrpbrrp-e2e');
let server;
let serverOutput = '';
let serverError;

async function freePort() {
  const listener = createServer();
  await new Promise((done, reject) => {
    listener.once('error', reject);
    listener.listen(0, '127.0.0.1', done);
  });
  const port = listener.address().port;
  await new Promise((done) => listener.close(done));
  return port;
}

async function ready(url) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (serverError) throw serverError;
    if (server.exitCode !== null || server.signalCode !== null)
      throw new Error(`Server exited early:\n${serverOutput}`);
    try {
      const response = await fetch(`${url}/api/setup/status`);
      if (response.ok) return;
    } catch {
      // The process is still starting.
    }
    await delay(200);
  }
  throw new Error(`Server did not become ready:\n${serverOutput}`);
}

async function run() {
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  const cleanEnv = Object.fromEntries(
    Object.entries(process.env).filter(([key]) => !key.startsWith('BRRP_'))
  );
  server = spawn(binary, ['serve'], {
    env: {
      ...cleanEnv,
      BRRP_DATA_DIR: dataDir,
      BRRP_ADDR: `127.0.0.1:${port}`,
      BRRP_BASE_URL: baseURL,
      BRRP_TLS_MODE: 'http-local',
      // Every journey signs in from 127.0.0.1, so the shipped address-wide sign-in budget
      // is spent by the suite's own framing long before any journey that needs it runs.
      // It is raised here rather than in the product, because a journey must measure what
      // it is about: the default budget stays 20, and the limiter's behaviour stays a
      // server-side concern with its own tests.
      BRRP_LOGIN_ATTEMPTS: '400',
      // Private conversations stay disabled in the default suite. The focused DM journey
      // opts this temporary server in explicitly; it never changes shipped defaults.
      BRRP_ENABLE_PRIVATE_CONVERSATIONS:
        process.env.BRRP_E2E_PRIVATE_CONVERSATIONS === '1' ? 'true' : 'false'
    },
    stdio: ['ignore', 'ignore', 'pipe']
  });
  server.once('error', (error) => (serverError = error));
  server.stderr.on('data', (chunk) => {
    serverOutput = (serverOutput + chunk.toString()).slice(-12000);
  });
  await ready(baseURL);
  const test = spawn(
    process.execPath,
    // Extra arguments are forwarded to Playwright, so a single journey can be run with
    // `npm run test:e2e -- --grep <name>` while still using this harness's server.
    [
      'node_modules/@playwright/test/cli.js',
      'test',
      '--config',
      'playwright.config.mjs',
      ...process.argv.slice(2)
    ],
    {
      env: {
        ...cleanEnv,
        BRRP_E2E_BASE_URL: baseURL,
        BRRP_E2E_DATA_DIR: dataDir,
        BRRP_E2E_PRIVATE_CONVERSATIONS: process.env.BRRP_E2E_PRIVATE_CONVERSATIONS || '0'
      },
      stdio: 'inherit'
    }
  );
  return await new Promise((done, reject) => {
    test.once('error', reject);
    test.once('exit', (code, signal) => done(signal ? 1 : (code ?? 1)));
  });
}

let exitCode = 1;
try {
  exitCode = await run();
} catch (error) {
  console.error(error);
} finally {
  if (server && server.exitCode === null && server.signalCode === null) {
    server.kill('SIGTERM');
    await Promise.race([
      new Promise((done) => {
        if (server.exitCode !== null || server.signalCode !== null) done();
        else server.once('exit', done);
      }),
      delay(3000)
    ]);
    if (server.exitCode === null && server.signalCode === null) server.kill('SIGKILL');
  }
  await rm(dataDir, { recursive: true, force: true });
}
process.exitCode = exitCode;
