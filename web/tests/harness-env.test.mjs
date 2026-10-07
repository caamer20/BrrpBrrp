// The browser harness strips every BRRP_ variable from the Playwright child and hands back
// only the ones it knows. A switch that is read by the config but missing from that list is
// silently ignored — which is how a run "under Chrome stable" ran the bundled Chromium and
// reported nothing (F-08). These tests pin the list against the config that reads it.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { describeEngines, harnessEnvironment } from '../e2e/harness-env.mjs';

test('a default run forwards the four gates switched off and nothing else', () => {
  assert.deepEqual(harnessEnvironment({ PATH: '/usr/bin', BRRP_DATA_DIR: '/srv/data' }), {
    BRRP_E2E_PRIVATE_CONVERSATIONS: '0',
    BRRP_E2E_FIREFOX: '0',
    BRRP_E2E_WEBKIT: '0',
    BRRP_E2E_SCALE: '0'
  });
});

test('the browser channel and the run ceiling reach the Playwright child', () => {
  const forwarded = harnessEnvironment({
    BRRP_E2E_BROWSER: 'chrome',
    BRRP_E2E_GLOBAL_TIMEOUT: '540000',
    BRRP_E2E_PRIVATE_CONVERSATIONS: '1'
  });
  assert.equal(forwarded.BRRP_E2E_BROWSER, 'chrome');
  assert.equal(forwarded.BRRP_E2E_GLOBAL_TIMEOUT, '540000');
  assert.equal(forwarded.BRRP_E2E_PRIVATE_CONVERSATIONS, '1');
});

test('the harness keeps its own variables and the server keeps its configuration', () => {
  const forwarded = harnessEnvironment({
    BRRP_E2E_BINARY: '/tmp/brrpbrrp-e2e',
    BRRP_E2E_BASE_URL: 'http://127.0.0.1:1',
    BRRP_E2E_DATA_DIR: '/tmp/data',
    BRRP_LOGIN_ATTEMPTS: '5'
  });
  for (const key of [
    'BRRP_E2E_BINARY',
    'BRRP_E2E_BASE_URL',
    'BRRP_E2E_DATA_DIR',
    'BRRP_LOGIN_ATTEMPTS'
  ])
    assert.equal(key in forwarded, false, `${key} was forwarded`);
});

test('a misspelt switch stops the run instead of reading as the mode it names', () => {
  assert.throws(
    () => harnessEnvironment({ BRRP_E2E_FIREFOX: '1', BRRP_E2E_WEBKITT: '1' }),
    /Unknown browser-harness variable\(s\): BRRP_E2E_WEBKITT/
  );
});

test('the engine line names the channel a run actually used', () => {
  assert.equal(describeEngines(harnessEnvironment({})), 'chromium (bundled)');
  assert.equal(
    describeEngines(harnessEnvironment({ BRRP_E2E_BROWSER: 'msedge', BRRP_E2E_FIREFOX: '1' })),
    'chromium (channel msedge); firefox (voice project available)'
  );
});

// The drift this item exists to stop: a variable the config or a journey reads, which the
// harness does not know about, is one a caller can set and never see honoured.
test('every harness variable the config and the journeys read is one the harness knows', async () => {
  const sources = await Promise.all(
    [
      '../playwright.config.mjs',
      '../e2e/release.spec.mjs',
      '../e2e/voice.spec.mjs',
      '../e2e/directs.spec.mjs',
      '../e2e/threads.spec.mjs',
      '../e2e/support.mjs'
    ].map((path) => readFile(new URL(path, import.meta.url), 'utf8'))
  );
  const read = new Set(sources.flatMap((source) => source.match(/BRRP_E2E_[A-Z_]+/g) ?? []));
  for (const key of read)
    assert.doesNotThrow(
      () => harnessEnvironment({ [key]: '1' }),
      `${key} is read by the suite but unknown to the harness`
    );
});
