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
    ...(process.env.BRRP_E2E_PRIVATE_CONVERSATIONS === '1'
      ? [
          { name: 'directs', testMatch: 'directs.spec.mjs', dependencies: ['release'] },
          // A community timeout must not close the private half of the product, so
          // this journey needs consented DMs and therefore the private feature mode.
          // It runs after the release journey, which creates the installation and the
          // community channel whose absence the journey asserts.
          { name: 'timeout', testMatch: 'timeout.spec.mjs', dependencies: ['release'] }
        ]
      : []),
    // Threads are a community feature, not a private one, so this journey runs in
    // both modes. It depends on the release journey for the installation and its
    // channel, and it must precede the ownership ceremony, which cannot be undone.
    { name: 'threads', testMatch: 'threads.spec.mjs', dependencies: ['release'] },
    { name: 'voice', testMatch: 'voice.spec.mjs', dependencies: ['release'] },
    // The media journeys under a second engine, in their own invocation:
    //
    //   BRRP_E2E_FIREFOX=1 BRRP_E2E_BINARY=… npm run test:e2e -- --project=voice-firefox
    //
    // The codec policy (the client's own order decides, VP8 as well as H264) and the
    // screen path are only real if a browser other than Chromium exercises them, and
    // Firefox offers a different codec set and a different encoder. It is gated because
    // this spec *creates the accounts and roles it uses*: one installation serves one
    // pass of it, and running the Chromium and Firefox passes against the same server
    // makes the second collide with the first (measured: 403 on the role the first pass
    // created). Each invocation gets its own server and its own data directory, so the
    // engine pass is a separate run rather than a second project in the same one.
    ...(process.env.BRRP_E2E_FIREFOX === '1'
      ? [
          {
            name: 'voice-firefox',
            testMatch: 'voice.spec.mjs',
            dependencies: ['release'],
            use: { ...devices['Desktop Firefox'] }
          }
        ]
      : []),
    { name: 'account-switch', testMatch: 'account-switch.spec.mjs', dependencies: ['release'] },
    {
      name: 'ownership',
      testMatch: 'ownership.spec.mjs',
      dependencies: ['account-switch']
    },
    // This journey performs an ownership ceremony, which the ownership journey above
    // asserts has not happened, and it cannot be undone: the ceremony creates an
    // owner and there is no route back to none. It therefore runs last, after every
    // project that asserts an installation with no owner.
    ...(process.env.BRRP_E2E_PRIVATE_CONVERSATIONS === '1'
      ? [
          {
            name: 'evidence-review',
            testMatch: 'evidence-review.spec.mjs',
            dependencies: ['ownership']
          }
        ]
      : [])
  ]
});
