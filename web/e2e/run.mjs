import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { describeEngines, harnessEnvironment } from './harness-env.mjs';

const dataDir = await mkdtemp(join(tmpdir(), 'brrpbrrp-browser-'));
const binary = resolve(process.env.BRRP_E2E_BINARY || '../dist/brrpbrrp-e2e');
let server;
let serverOutput = '';
let serverError;

// The harness stops its server on the paths it controls (the finally below); this
// backstop covers the ones it does not — a Ctrl-C or a CI cancel — so an interrupted
// browser run cannot leave a server listening on a loopback port.
function stopServerNow() {
  if (server && server.exitCode === null && server.signalCode === null) {
    try {
      server.kill('SIGKILL');
    } catch {
      /* already exited */
    }
  }
}
process.on('exit', stopServerNow);
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    stopServerNow();
    process.exit(1);
  });
}

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
  // The WebKit project is the one engine pass that cannot share an invocation. Its subset
  // includes the setup journey, and an installation can only be set up once, so a run that
  // also contains the Chromium `release` project spends the setup token before WebKit
  // reaches it: that is exactly how the project was red the first time (W-06), and it read
  // as six unrelated failures rather than one cause. Refuse the invocation and name the one
  // that works, instead of leaving that again to be rediscovered from a screenshot.
  const args = process.argv.slice(2);
  const forwarded = harnessEnvironment(process.env);
  if (process.env.BRRP_E2E_WEBKIT === '1' && !args.some((arg) => arg.startsWith('--project')))
    throw new Error(
      'BRRP_E2E_WEBKIT=1 needs its own server and cannot run beside the Chromium projects: ' +
        'pass --project=release-webkit.'
    );
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
      // Every journey signs in from 127.0.0.1. The address-wide budget used to be spent by
      // each of those sign-ins, so it was raised here; since W-29 only *failed* attempts
      // count and the suite's own framing spends nothing. The value is kept because some
      // journeys fail on purpose and they share this one address: the default stays 20,
      // and the limiter's behaviour stays a server-side concern with its own tests.
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
  // The mode is printed so a CI log proves which suite configuration ran rather
  // than just which variable someone hoped the harness honored (review 8, R8-04).
  console.log(
    `browser suite feature mode: private conversations ${
      process.env.BRRP_E2E_PRIVATE_CONVERSATIONS === '1' ? 'ENABLED' : 'disabled (default)'
    }`
  );
  console.log(`browser suite engines: ${describeEngines(forwarded)}`);
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
        // The caller's switches, by name; see FORWARDED above for the list and why it is one.
        ...forwarded
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
  // A failed journey is the one moment the server's own log matters, and this harness
  // used to discard it: the tail was kept for a readiness failure and thrown away for
  // everything else, which is exactly backwards. An intermittent join that the browser
  // reports as a timeout is a *server* event — a refusal, a stalled handshake, a
  // detached peer — and none of those can be told apart from the screenshot alone.
  if (exitCode !== 0 && serverOutput.trim()) {
    console.error(
      '\n--- server log (tail) ---\n' + serverOutput.trim() + '\n--- end server log ---'
    );
  }
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
