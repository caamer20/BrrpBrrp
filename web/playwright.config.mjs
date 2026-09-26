import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.mjs',
  workers: 1,
  retries: 0,
  timeout: 60000,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  outputDir: './test-results',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: process.env.BRRP_E2E_BASE_URL,
    channel: process.env.BRRP_E2E_BROWSER || undefined,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    // Media permissions never need a person in this harness. The voice RTP journey
    // supplies its own deterministic Web Audio track, because Chromium fake-device capture
    // can stall in the host audio service; its actual peer/SFU/RTP assertions remain real.
    launchOptions: {
      args: [
        '--use-fake-device-for-media-stream',
        '--use-fake-ui-for-media-stream',
        '--autoplay-policy=no-user-gesture-required'
      ]
    }
  },
  // Every journey signs in as an account or reads content the first one created, and a
  // server can only be set up once, so each depends on the setup journey and the declared
  // order is the run order. The dependencies are deliberately this flat: each journey
  // restores what it changes, and chaining them would make one harness failure suppress
  // every later journey's coverage (review R3-04). The ownership ceremony still runs last,
  // because it leaves the installation owned by another account and would change what the
  // earlier journeys see.
  projects: [
    { name: 'release', testMatch: 'release.spec.mjs' },
    { name: 'authority', testMatch: 'authority.spec.mjs', dependencies: ['release'] },
    { name: 'privacy', testMatch: 'privacy.spec.mjs', dependencies: ['release'] },
    { name: 'voice', testMatch: 'voice.spec.mjs', dependencies: ['release'] },
    { name: 'account-switch', testMatch: 'account-switch.spec.mjs', dependencies: ['release'] },
    { name: 'ownership', testMatch: 'ownership.spec.mjs', dependencies: ['release'] }
  ]
});
