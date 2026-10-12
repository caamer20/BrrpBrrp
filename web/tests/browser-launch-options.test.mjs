import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

test('resolved engine projects isolate Chromium flags and channel from WebKit and Firefox', () => {
  const directory = mkdtempSync(join(tmpdir(), 'brrp-browser-launch-'));
  try {
    const web = fileURLToPath(new URL('../', import.meta.url));
    const config = join(directory, 'config.mjs');
    const reporter = join(directory, 'reporter.mjs');
    const resultPath = join(directory, 'resolved.json');
    writeFileSync(
      config,
      `import config from ${JSON.stringify(new URL('../playwright.config.mjs', import.meta.url).href)};
export default { ...config, testDir: ${JSON.stringify(join(web, 'e2e'))} };
`
    );
    writeFileSync(
      reporter,
      `import { writeFileSync } from 'node:fs';
export default class {
  onBegin(config) {
    writeFileSync(${JSON.stringify(resultPath)}, JSON.stringify(config.projects.map(({ name, use }) => ({ name, use }))));
  }
  printsToStdio() { return false; }
}
`
    );
    const env = Object.fromEntries(
      Object.entries(process.env).filter(([key]) => !key.startsWith('BRRP_'))
    );
    const result = spawnSync(
      process.execPath,
      [
        join(web, 'node_modules/@playwright/test/cli.js'),
        'test',
        '--config',
        config,
        '--list',
        '--reporter',
        reporter
      ],
      {
        cwd: web,
        env: {
          ...env,
          BRRP_E2E_BROWSER: 'chrome',
          BRRP_E2E_WEBKIT: '1',
          BRRP_E2E_FIREFOX: '1',
          BRRP_E2E_BASE_URL: 'http://127.0.0.1:8082',
          BRRP_E2E_DATA_DIR: directory
        },
        encoding: 'utf8',
        timeout: 60000
      }
    );
    assert.equal(result.status, 0, result.stderr || result.error?.message);
    const projects = new Map(
      JSON.parse(readFileSync(resultPath, 'utf8')).map((p) => [p.name, p.use])
    );
    const chromium = projects.get('release');
    assert.equal(chromium.channel ?? chromium.launchOptions.channel, 'chrome');
    assert.ok(chromium.launchOptions.args.includes('--use-fake-device-for-media-stream'));
    assert.ok(chromium.launchOptions.args.includes('--mute-audio'));
    for (const name of ['release-webkit', 'voice-firefox']) {
      const use = projects.get(name);
      assert.ok(use, `${name} was omitted`);
      assert.deepEqual(use.launchOptions.args, [], `${name} inherited Chromium flags`);
      assert.equal(use.channel, undefined, `${name} inherited the Chromium channel fixture`);
      assert.equal(
        use.launchOptions.channel,
        undefined,
        `${name} inherited the Chromium launch channel`
      );
    }
    assert.equal(projects.get('release-webkit').defaultBrowserType, 'webkit');
    const firefox = projects.get('voice-firefox');
    assert.equal(firefox.defaultBrowserType, 'firefox');
    assert.equal(firefox.launchOptions.firefoxUserPrefs['media.volume_scale'], '0.0');
    assert.equal(firefox.firefoxUserPrefs, undefined, 'Firefox preferences bypassed launchOptions');
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
