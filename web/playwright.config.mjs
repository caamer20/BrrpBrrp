import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.mjs',
  workers: 1,
  retries: 0,
  timeout: 60000,
  // The whole run needs a ceiling, and Playwright takes this one only on the config:
  // there is no per-project `globalTimeout`, so this single number covers every engine
  // and every mode, which is what "give every engine project a global timeout" means
  // here. Without it a journey that hangs — or a server that never finishes starting —
  // holds the run until a person notices; one Firefox invocation produced no output for
  // ten minutes on 4 October. The longest measured invocation is the private-
  // conversations suite at 4.8 minutes and the longest single journey asks for seven
  // minutes for itself, so twenty minutes is about four times the largest real run: long
  // enough not to fail a slow host, short enough that a hang ends in a report rather
  // than in silence. `--global-timeout` on the command line still wins over this, which
  // is how the verification commands pin their own budget; BRRP_E2E_GLOBAL_TIMEOUT is
  // for a caller that wants a different default without repeating the flag.
  globalTimeout: Number(process.env.BRRP_E2E_GLOBAL_TIMEOUT || 0) || 20 * 60 * 1000,
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
        '--autoplay-policy=no-user-gesture-required',
        // The suite must make no sound. The voice journeys push a real 440 Hz tone and a
        // real 660 Hz screen tone through the server, and the receiving browser plays them
        // — and the join/leave chime — through whatever speakers this machine has. Every
        // assertion about audio reads the peer connection's own counters (`bytesReceived`,
        // `bytesSent`, `framesDecoded`, and whether an inbound track is enabled), never an
        // output device or an audio level, so muting the output cannot change a result.
        // `--mute-audio` mutes the device, not the pipeline: RTP is still received, still
        // decoded and still counted. Read every audio assertion in voice.spec.mjs before
        // relying on that; it was done once, when this was added (W-23).
        '--mute-audio'
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
    // The core journeys under a third engine. Firefox proves the media journeys against a
    // different codec set; WebKit proves the *core* journeys (setup, messages, search,
    // permalink, formatting, Back/Forward, phone viewport) against a different engine for
    // everything else - and its exclusions are stated, because a project that silently
    // skips would pass nothing real. Requires `npx playwright install webkit`.
    //
    // Safari's engine runs that subset in its own invocation, against its own server and
    // data directory:
    //
    //   BRRP_E2E_WEBKIT=1 BRRP_E2E_BINARY=… npm run test:e2e -- --project=release-webkit
    //
    // It must not depend on the Chromium `release` project, which is how it was first
    // wired and why it was red: a dependency runs that project first against the one
    // server this invocation starts, so the setup token was already spent (the setup
    // journey read a file that no longer existed), the three accounts this subset creates
    // already existed (409), and nothing the WebKit pass measured was its own. One
    // installation serves one setup, so an engine that exercises setup needs an
    // installation of its own. `run.mjs` refuses an invocation that would put both
    // projects in one run rather than leaving that to be rediscovered.
    ...(process.env.BRRP_E2E_WEBKIT === '1'
      ? [
          {
            name: 'release-webkit',
            testMatch: 'release.spec.mjs',
            // Service workers are blocked for this pass. Playwright's WebKit cannot intercept
            // the requests of a page a service worker controls, and three steps of these
            // journeys are interceptions: the aborted thread reply, the lost export
            // response and the held phone-width send. All three failed here with the worker
            // allowed — the reply was simply sent, and "no send reached the intercepted
            // route" was the phone journey's unexplained failure — and all three pass with
            // it blocked (6 October). The cost is stated where it is paid: the offline-shell
            // segment of the setup journey has no worker to wait for and records itself as
            // not exercised under this project.
            use: { ...devices['Desktop Safari'], serviceWorkers: 'block' },
            grep: /first setup|member requests their data export|detectable violations|message permalink|Arabic message|Back and Forward|phone width/
          }
        ]
      : []),
    { name: 'authority', testMatch: 'authority.spec.mjs', dependencies: ['release'] },
    { name: 'privacy', testMatch: 'privacy.spec.mjs', dependencies: ['release'] },
    ...(process.env.BRRP_E2E_RESTRICTED_CHANNELS === '1'
      ? [{ name: 'restricted', testMatch: 'restricted.spec.mjs', dependencies: ['release'] }]
      : []),
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
    // The long-thread scale proof (T-THREAD-01): a thousand replies seeded through the API
    // from twenty-five accounts, walked in a real browser. It is gated like the Firefox
    // pass — it seeds a thousand messages and takes about ten minutes — and it creates its
    // own channel and accounts, so it depends only on the installation the release journey
    // created. Run it with:
    //
    //   BRRP_E2E_SCALE=1 BRRP_E2E_BINARY=… npm run test:e2e -- --project=thread-scale
    //
    ...(process.env.BRRP_E2E_SCALE === '1'
      ? [{ name: 'thread-scale', testMatch: 'thread-scale.spec.mjs', dependencies: ['release'] }]
      : []),
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
            use: {
              ...devices['Desktop Firefox'],
              // Firefox has no equivalent of `--mute-audio`; this is its own switch for the
              // same thing, and it is a *string* preference deliberately —
              // `media.volume_scale` is declared as a string in Gecko and read back as a
              // float, so a number here would be stored as the wrong type and ignored.
              firefoxUserPrefs: { 'media.volume_scale': '0.0' }
            }
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
