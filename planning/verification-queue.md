# Verification queue

**Owner's checklist for the pass that turns "written, not run" into evidence.** From
6 October the owner suspended test runs: tranches 23 onward change product, packaging and
harness code without executing any test, browser suite, rehearsal or loop. The static gates
are run after every item (`make check`, which builds the frontend, vets, formats,
type-checks and runs the documentation, packaging and workflow audits) and every behavioural
change carries tests that have not been executed. This file says what to run, in what order,
against which commit, and what a reader should expect each run to prove.

Toolchain on this machine: Go 1.27.1 at `BrrpBrrp/.tools/go/bin/go` (not on `PATH`;
`.tools/` is untracked), module cache `/Users/cameronamer/go/pkg/mod`, Node 24, Playwright
Chromium 1243 / Firefox 1543 / WebKit 2359 in `/private/tmp/brrpbrrp-playwright-browsers`.
`rg` is not installed; use `grep` and check its exit status.

## 1. The order the pass should take

Cheapest and most informative first, so a failure is found before an hour is spent on
something downstream of it.

1. **Static gates.** `make check GO=$GO` — exit 0 and no `failed`/`did not run` line.
2. **Frontend unit tests.** `npm --prefix web test` — 366 passed before this work; any
   count change must be explained by a test added in an item below.
3. **Go, compile-only first.** `$GO vet ./...` and `$GO vet ./internal/api/` (the second
   compiles the API package's tests without running them, which catches a test that does
   not build).
4. **Go test suite.** `$GO test ./... -count=1` — about 4 minutes; the API package about
   230 s. Eleven test-bearing packages passed at `1faeb92`.
5. **Browser suites, with audio muted** (W-23). One at a time, each with its own `--output`
   directory and a `--global-timeout`; the config now carries a twenty-minute default.
   ```sh
   export BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-e2e"
   T="--global-timeout=540000"
   BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e -- $T --output /tmp/brrp-e2e/default
   BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e -- $T --output /tmp/brrp-e2e/private
   BRRP_E2E_FIREFOX=1 npm --prefix web run test:e2e -- $T --project=voice-firefox  --output /tmp/brrp-e2e/firefox
   BRRP_E2E_WEBKIT=1  npm --prefix web run test:e2e -- $T --project=release-webkit --output /tmp/brrp-e2e/webkit
   BRRP_E2E_SCALE=1   npm --prefix web run test:e2e -- $T --project=thread-scale   --output /tmp/brrp-e2e/scale
   ```
   Rebuild the frontend and the e2e binary first (`npm --prefix web run build` then
   `CGO_ENABLED=0 $GO build -trimpath -o dist/brrpbrrp-e2e ./cmd/brrpbrrp`); a stale binary
   is the easiest way to test the wrong thing.
6. **Rehearsals** — `scripts/upgrade-recovery.mjs`, `scripts/upgrade-recovery-private.mjs`,
   `scripts/backup-encrypted-rehearsal.mjs`, `scripts/full-disk-rehearsal.mjs`,
   `scripts/upgrade-rollback.mjs` (needs the alpha.7 archive; alpha.8 and alpha.9 cannot be
   the rollback baseline because they write schema 25), then `make schema13-rehearsal`.
7. **The owed acceptance runs** in section 4 below, which are verification by nature.

## 2. Written-not-run in this session

Each entry: the item, its commit, the tests written beside it, and the suites that must
confirm it. Nothing here has been executed.

### W-05 — Firefox `playsInline` assertion and the sign-in race (`496e90a`)

*Placed first because its figures were measured before the suspension; everything after it
was not.*

- **Tests written:** no new test files. `web/e2e/support.mjs` gains `waitForSurface`, used by
  `signIn` and by the phone-viewport journey; `web/e2e/voice.spec.mjs` asserts the
  `playsinline` attribute; `web/playwright.config.mjs` gains `globalTimeout`.
- **Must be confirmed by:** `BRRP_E2E_FIREFOX=1 … --project=voice-firefox`, repeated — the
  session that made the change reported twenty consecutive green invocations of
  **14 / 0 / 0 / 0**, and Chromium default **30 / 0 / 0 / 0** and private **45 / 0 / 0 / 0**
  afterwards. A single green run is not the claim.
- **Already run, and the run is the evidence:** a slowed boot (`/api/state` delayed 400 ms
  by the fixture's route interception) failed the pre-fix helper **0 of 3** times and passed
  the fixed helper **3 of 3**. The probe that measured the underlying race — the auth surface
  absent at the instant `goto` resolved in **3 of 30** fresh Firefox contexts — lives in
  `web/node_modules/.brrp-signin-probe.mjs`, which is inside `node_modules` and therefore not
  committed; re-write it from tranche 21's description if it is needed again.

### W-06 — the WebKit pass runs against its own installation (`e0c6280`)

- **Tests written:** none new. `web/playwright.config.mjs` drops `release-webkit`'s
  dependency on `release`; `web/e2e/run.mjs` refuses a WebKit invocation with no
  `--project`; `web/e2e/release.spec.mjs` gates the clipboard permission on the engine and
  asserts whether the send reached the intercepted route before it asserts the pending row.
- **Must be confirmed by:** `BRRP_E2E_WEBKIT=1 … --project=release-webkit`. Before this
  change it was **10 / 6 / 0 / 0** (1.2 min): nine Chromium journeys green, then six of the
  WebKit subset's seven failing on a fixture the Chromium pass had already spent. The
  expectation is **7 / 0 / 0 / 0** for the project itself, with the Chromium project no
  longer part of the invocation.
- **Open question the pass must answer:** the `phone viewport` journey failed under WebKit
  with `getByText('Sending…')` not found, and the cause is not explained. The new
  `expect.poll(() => held).toBeGreaterThan(0)` assertion says whether the send reached the
  intercepted route at all. Report which half failed.
- **Also confirm:** `BRRP_E2E_WEBKIT=1 npm --prefix web run test:e2e` with no `--project`
  now exits non-zero with the one-sentence message rather than running the Chromium
  projects first.

## 3. What the pass must not assume

- A passing rerun does not close a failure; keep the first failing trace.
- A project's result is its own journeys, not its dependency's. With W-06 the WebKit
  invocation no longer contains a dependency, so its summary line is now entirely its own.
- A run is green only when its exit status is zero **and** its summary has no `failed`,
  `did not run`, `interrupted` or `timed out` line. Record four numbers.
- Playwright's WebKit is not Safari on a device, and synthetic media on loopback is not a
  microphone or a network. Engine simulation is supplemental evidence.

## 4. Owed acceptance runs (verification by nature — unchanged, and not started)

These were owed before this session and remain owed exactly as specified. Do not treat any
of them as closed by the work above.

- **W-07** restricted channels exercised in a browser, with `BRRP_ENABLE_RESTRICTED_CHANNELS`
  on and off, and a refusal journey with it off.
- **W-10** an acceptance record that means "ran and passed" (F-07).
- **W-11** real mixed-direction text in the Arabic journey.
- **W-12** T-CHAT-01: 1,000 logical sends with duplicate submissions, delayed and reordered
  events, lost acknowledgements, reconnects and deletion before retry.
- **W-13** T-MEDIA-02: 100 screen-share start/stop cycles with late viewers, cancellation,
  a room move and a reconnect.
- **W-14** T-READ-01: two browser contexts with separate sessions plus a background tab.
- **W-15** T-PWA-01: two genuinely different builds and the update handoff.
- **W-16** the runtime half of the negative matrix for the `conversation`, `channel`,
  `audience`, `recipient` and `authorship` classes.
- **W-17** the capacity driver (F-05).
- **W-18** failure coverage (V1-10), including the encrypted rehearsal's statement of which
  backup tool was exercised (`openssl` only; the `age` and `gpg` paths were not).
- **W-19** two consecutive fresh-context review rounds with no new P1, now also covering the
  commits in this session.
- **W-20** one sweep on one commit with nothing skipped, then `0.4.0-alpha.10`.

## 5. Questions recorded for the owner

None yet. Add them here as they arise, and keep the section honest: an item whose answer is
not known does not become an item whose answer was assumed.
