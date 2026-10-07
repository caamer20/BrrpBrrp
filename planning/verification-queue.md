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

## 0. The pass was run on 6 October

Everything in section 2 has now been executed, in the order section 1 gives, and passed.
Ledger tranche 44 holds the table: 389 unit tests; eleven Go packages; Chromium default 30
and private 45; Firefox 14; WebKit 7 (after two harness corrections, which that tranche
explains); `thread-scale` 10; and six rehearsals. Nothing failed, was skipped or did not
run. The entries in section 2 are kept as the record of what each change needed; read each
"must be confirmed by" as done, except where it names a hosted runner, a real deployment or
a device — W-09's Windows job, W-30's `curl -I` against a real `https` host, and the browser
reads of the Maintenance screen by a person have not happened.

**Still not run:** the `-race` sweep, everything in section 4, and anything on Windows, in
Docker or on a hosted runner.

## 1. The order the pass should take

Cheapest and most informative first, so a failure is found before an hour is spent on
something downstream of it.

1. **Static gates.** `make check GO=$GO` — exit 0 and no `failed`/`did not run` line.
2. **Frontend unit tests.** `npm --prefix web test` — 366 passed before this work; any
   count change must be explained by a test added in an item below. Written since and not
   run: 373 after W-28, then six for W-08 and ten for W-32 — **389** expected.
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

### W-23 — the browser suites make no sound (`18308ca`)

- **Changed:** `web/playwright.config.mjs` (`--mute-audio`, and `media.volume_scale` on the
  Firefox project), `web/e2e/support.mjs` (the suite now exports its own `test`, whose `page`
  fixture silences media elements), and the `test` import in all eleven spec files.
- **Tests written:** none new; this changes no assertion by design. **Read before trusting
  it:** every audio assertion in `voice.spec.mjs` reads peer-connection counters or an
  element's `muted` attribute, and nothing anywhere reads an output volume or an audio level.
- **Must be confirmed by:** one invocation per engine — default and private Chromium,
  `voice-firefox`, `release-webkit`. The result to check is not only green: the machine must
  be silent while the voice journeys run. **A load failure in any spec would be immediate and
  obvious** (all eleven now import `test` from `./support.mjs`), which is the risk this change
  carries.

### W-33 — wording that is no longer true (`3e46e27`)

- **Changed:** `README.md`, `docs/roadmap.md`, `docs/production-readiness.md`,
  `docs/acceptance.md`, `docs/architecture.md`, `docs/v1-implementation-plan.md` §2.3,
  `internal/api/admin.go` (one refusal string), `web/src/lib/components/admin/Channels.svelte`.
- **Must be confirmed by:** `make check` (already exit 0 here, including `svelte-check`), and
  then the journeys that render the administrator's channel editor — `privacy` and the private
  suite — with the gate **off**, which is how it ships. The two notices are the only rendered
  behaviour that changed.
- **Left alone deliberately:** `docs/implementation.md` and the completed review records still
  carry the 10–20-user profile as dated accounts of what was measured then.

### W-34 — the documents a public 1.0 needs (`84de26f`)

- **Created:** `CODE_OF_CONDUCT.md`, `CHANGELOG.md`, `SUPPORT.md`, `docs/compatibility.md`,
  `docs/known-limitations.md`, `docs/upgrading.md`, `docs/releases/1.0.0-notes-draft.md`,
  `docs/releases/readme-1.0.0-draft.md`, `.github/pull_request_template.md`. **Changed:**
  `scripts/release.go`, so both packages ship the three new top-level documents.
- **Must be confirmed by:** `make check` only (548 files / 60 native documents, all links
  resolved). Two documents need a human read rather than a run: the Windows and Docker gaps in
  `docs/known-limitations.md`, and the 1.0 notes draft, which must not be mistaken for a
  release record.

### W-35 — the gate flip as one reviewed commit (`4155b12`)

- **Created:** `planning/gate-flip-checklist.md`. Nothing was flipped.
- **Must be confirmed by:** nothing yet. When the owner flips the gates, the pass is the whole
  suite in both gate states — which is W-07's exercise of restricted channels, plus the private
  suite — and the four questions in §5 of the checklist need answering first.

### W-21 (part 1) — the review and beta bundles (`ab79e57`)

- **Created:** `planning/security-review-bundle.md`, `planning/accessibility-review-bundle.md`,
  `planning/beta-entry-checklist.md`, `planning/daily-ledger-template.md`.
- **Still owed from W-21:** the 100-account reference fixture definition (`F-T`). It waits on
  the owner ratifying the retained-data targets the plan proposes (one million messages,
  20 GiB); the Makefile's fixture default is still 250 accounts against the accepted
  100-account bound. The two corrections to `docs/host-qualification.md` are done — see
  W-21 (part 2) below.
- **Must be confirmed by:** a human read of the security bundle against the review plan's own
  prerequisites, and a run of every command the bundle lists as runnable — none of which has
  been executed to write it.

### W-21 (part 2) — the host procedures match the build (`cda1b1d`)

- **Changed:** `docs/host-qualification.md` only. §1.1 now hashes the **archive** against its
  line in `SHA256SUMS` before extracting, and hashes the executable only against a separately
  recorded executable digest, because the release publishes archive checksums and the two are
  not interchangeable. §1.4 no longer claims LocalService can write "nothing else"; it states
  what the installer actually grants (`scripts/windows/install.ps1:11`,
  `internal/installer/platform_windows.go:54`: Administrators full, SYSTEM full, LocalService
  modify on `%ProgramData%\BrrpBrrp`, with inheritance removed and no ACE outside it) and
  tests the rights the application needs against the real operating-system boundary. The
  version references become `<version>` placeholders, since the bytes to qualify are whatever
  the release publishes.
- **Tests written:** none — documents only; no Go file, no route, no behaviour.
- **Must be confirmed by:** `make check GO=$GO` (its documentation-link audit walks the file)
  and a human read beside the review's own wording
  (`planning/v1-release-execution-plan-2026-10-03.md:179`). The procedures themselves are
  QUAL-01, which needs the Windows and Linux hosts and stays blocked.

### W-36 — the backup cadence is twelve hours (`9bc8237`)

*Owner decision of 6 October 2026. No schema change.*

- **Changed:** `internal/db/models.go` (`DefaultSettings().BackupHours` 24 → 12, with a
  comment), `internal/api/settings.go` (the route's zero fallback now comes from
  `DefaultSettings()` for the interval *and* the copy count), and seven documents.
- **Tests written, not run:** `internal/api/settings_test.go` —
  `TestFreshSetupStoresTheTwelveHourBackupInterval` and
  `TestAStoredBackupIntervalSurvivesTheDefault`. Read, and deliberately untouched because
  they name their own interval: `internal/api/backup_schedule_test.go:169,172,521,589`,
  `internal/api/alerts_test.go:258`. No frontend test or journey asserts an interval default.
- **Must be confirmed by:** `$GO test ./internal/api/ ./internal/app/ ./internal/db/ -count=1`.
  The one number worth reading is the default in the API package's settings tests; then the
  recovery-threshold tests, which must still show 18 h warning and 24 h critical at a 12-hour
  interval (`app.RecoveryThresholds` returns 1.5× and 2×).
- **What a wrong answer would look like, and its blast radius:** if a stored 24 came back as
  12, an existing installation's cadence would silently change and the alert thresholds with
  it; if a fresh setup stored 24, the recovery objective would not match the plan. Both are
  asserted directly. **A migration was deliberately not written**, so nothing here rewrites an
  operator's stored interval.
- **Documents changed, for a human read rather than a run:** `docs/admin.md` (line 79),
  `docs/deployment.md` (an alert sentence that said "three times the configured interval"
  where the thresholds are 1.5× and 2×), `docs/docker.md`, `docs/production-readiness.md`,
  `docs/upgrading.md`, `docs/known-limitations.md`, `docs/releases/1.0.0-notes-draft.md`.

### W-24 — the settings an operator sets in `.env.docker` reach the container (`437082e`)

- **Changed:** `compose.yaml` (eight variables passed as `${NAME:-}`), `.env.docker.example`
  (the eight, blank, with a comment), `docs/docker.md` (a table of what an operator may set),
  `Makefile` (the audit wired into `check`, plus a `compose-env` target),
  `scripts/release.go` (the new script added to the source package). **No Go file changed.**
- **Tests written:** none — the assertion is the audit, and an audit's own self-test is its
  test. `scripts/check-compose-env.mjs --self-test` runs it against four fixtures (unpassed
  key, repaired tree, test-only key, deliberate list).
- **Already run, and the run is the evidence:** the audit, against the *pre-fix* Compose file,
  reported exactly the eight keys with one complaint each. `make check` exit 0 with the audit
  wired in, the source package at 550 files.
- **Must be confirmed by:** nothing on this machine. On a Docker host, the container run
  `scripts/check-docker.mjs` (owed anyway) is what proves `docker compose` accepts the file
  with `${NAME:-}` values — the audit parses Compose, it does not interpret it. Add one manual
  check there: set `BRRP_MAX_UPLOAD_BYTES` and `BRRP_STORAGE_QUOTA_BYTES` in `.env.docker`,
  start the container, and confirm both take effect in **Manage server → Settings**.

### W-25 — the relay's TCP and TLS fallbacks are reachable in Docker (`6f481ea`)

- **Changed:** `compose.turn.yaml` (two TCP mappings with named variables), `.env.docker.example`
  (the two variables), `docs/docker.md`, `docs/admin.md`, `docs/security.md`, `docs/hosting.md`,
  `docs/acceptance.md`, and one guidance sentence in
  `web/src/lib/components/admin/Settings.svelte`. **No Go file, no route, no behaviour.**
- **Tests written:** none, and none is the right shape: this needs a container and a second
  network. `make check` is the whole static gate, and it is already exit 0.
- **Must be confirmed by:** on a Docker host, `make docker-check`, then a manual run with
  `docker compose --env-file .env.docker -f compose.yaml -f compose.turn.yaml up -d --build`,
  **Plain TCP fallback** switched on in Manage server → Settings, one client whose network
  blocks UDP, and the log line `voice path established … relayed: true`. Repeat with **TLS
  fallback** and the certificate readable in the container.
- **Read, not run — the one thing to check in the docs:** the two fallback *ports* are not
  interface settings (the fields are in the schema but no control exists), so the app always
  takes the UDP port + 1 and + 2. If a future change adds controls for them, `docs/docker.md`
  and `docs/admin.md` say the ports "follow the UDP port" and would need updating with it.

### W-26 — the example configuration documents every key the server reads (`af5f137`, ledger `44e4022`)

- **Changed:** `scripts/env-keys.mjs` (new shared module — the one walk that finds a server's
  `BRRP_*` reads, excluding `*_test.go`), `scripts/check-env-coverage.mjs` (new, with its own
  `--self-test`), `.env.example` (rewritten to name every key), `.env.docker.example` (the
  direct-message line), `docs/deployment.md` (three keys and both gates added to the settings
  table, signature header named accurately), `docs/installation.md` (**Changing a setting
  after installation**, with `server.env`'s path per platform), `Makefile` (the audit wired
  into `check`, plus an `env-coverage` target), `scripts/release.go` (both new scripts into the
  source package). **No Go file, no route, no behaviour.**
- **Tests written:** none — the assertion is the audit, and an audit's own self-test is its
  test. `scripts/check-env-coverage.mjs --self-test` runs it against four fixtures: an
  undocumented key reported once per destination, a commented example that satisfies it, a
  harness-only key ignored, and a missing destination reported as itself.
- **Already run, and the run is the evidence:** the audit, against the *pre-fix* tree (a
  temporary root symlinking the real source trees), reported **12 problems: nine for
  `.env.example` and three for `docs/deployment.md`** — the exact list of five planned keys
  plus `BRRP_DB_FILE`, `BRRP_UPLOAD_DIR`, `BRRP_SETUP_TOKEN_FILE`, `BRRP_LOG_FILE`,
  `BRRP_SQLITE_SYNCHRONOUS` and the two gates. Afterwards it reports all 29 keys covered.
  `make check` exit 0 with both audits wired in; the source package is 552 files.
- **Must be confirmed by:** `make check GO=$GO` — static, and the only mechanical gate this
  item has. Then **a person must read `.env.example` and `.env.docker.example` once**: they
  are written for humans and an audit can only say that a key is named somewhere in them, not
  that its sentence is true. On a Docker host, confirm `docker compose --env-file
  .env.docker` still accepts the file after the comment edits.
- **A consequence for future work, not a defect:** `make check` now fails when a new key read
  outside a `*_test.go` file is absent from `.env.example` or `docs/deployment.md`. That is
  the drift this item exists to stop. A deliberately undocumented development switch must
  live in a test file (the load harness's knobs already do) or be documented.
- **A figure the plan got wrong, recorded so nobody looks for it:** the assignment said the
  installer writes thirteen keys. `Plan.configText()` (`internal/installer/settings.go:148`)
  writes **nine** in `computer`/`network` mode and **eleven** in `proxy`. Nothing was changed
  in the wizard; the ledger (tranche 31, §31d) carries the correction.

### Tranche 32 — how a member leaves today, and the owner's questions (`3d45c29`)

*Not a W-item: documents and this queue. **No behaviour changed.***

- **Changed:** `docs/community-guide.md` and `docs/known-limitations.md` — a member can take
  their own data (**Your data export**) but cannot remove their own account; only an
  administrator can, so a member who wants to leave asks an operator. Plus this queue's §5
  (the five owner questions) and the ledger's tranche 32. The ledger commit for W-26,
  `44e4022`, is bookkeeping and is named in W-26's entry above.
- **Tests written:** none — documents only; no Go file, no route, no behaviour.
- **Must be confirmed by:** `make check GO=$GO` (its documentation-link audit walks both
  files) and a human read. Nothing here needs a browser or a test binary.

### W-27 — a member can change their own name and colour (`f66ad04`)

- **Changed:** `internal/api/auth.go` (`updateMe` — the handler), `internal/api/router.go`
  (the route), `internal/api/negative_matrix_test.go` (its hand classification),
  `planning/negative-matrix.json` (regenerated), `internal/api/profile_test.go` (new — the
  tests), `web/src/lib/components/Preferences.svelte` (a **Profile** section),
  `web/src/lib/components/MemberProfile.svelte` (**Edit profile** on your own profile),
  `web/src/lib/avatar-ink.ts` (`AVATAR_PALETTE`), `web/src/app.css` (the swatch styles),
  `web/tests/avatar-ink.test.mjs` (the palette case), and one route figure in
  `planning/security-review-bundle.md`. **No schema change.**
- **Tests written, not run:** `internal/api/profile_test.go` —
  `TestAMemberChangesTheirOwnDisplayNameAndAvatarColor`,
  `TestARenameRefusesExactlyWhatTheAdministratorRouteRefuses`,
  `TestARenameLeavesEveryOtherAccountAlone`,
  `TestADeactivatedAccountCannotRenameItself` (the second half calls the handler directly,
  with a comment saying why a deactivated account cannot reach it over HTTP),
  `TestARenameAnnouncesItselfOnTheAccountSocket`. `web/tests/avatar-ink.test.mjs` — "the
  colours a member chooses from all clear the minimum".
- **The one test that was run:** the route-inventory generator
  (`$GO test ./internal/api -run
  'TestEveryRouteDeclaresHowAnOutsiderIsDenied|TestEveryRouteDeclaresItsPolicy' -count=1
  -update-negative-matrix`), exit 0 in 0.7 s, as section 6 allows for a new route. It took
  the matrix from 132 to **133** rows; the diff is the one new `PATCH /api/me` entry.
- **Must be confirmed by:** `$GO test ./internal/api/ -count=1` (the five new tests), and
  `npm --prefix web test`, where the only expected change is **366 → 367** passed — one new
  case in `avatar-ink.test.mjs`. The socket test is the one to watch: it is the only place
  the delivered `user.updated` event is checked. The browser suites do not exercise this
  route today.
- **What a wrong answer would look like, and its blast radius:** if the handler wrote a
  field the request did not name, an administrator's colour change could be reverted by a
  later rename; if the deactivation check were missing, a rename could land on an account
  that was deactivated while the request was in flight. Both are asserted directly. The
  handler is the only new writer of `display_name`/`avatar_color`, and it writes only the
  caller's own row.

### W-28 — a new owner is walked to a recoverable installation (`388920e`)

- **Changed:** `web/src/lib/first-run.ts` (new — the checklist as a value function),
  `web/tests/first-run.test.mjs` (new — six cases), `web/src/lib/components/admin/Maintenance.svelte`
  (**Setup checks** becomes **Is this server ready?**), `web/src/lib/components/admin/BackupSettings.svelte`
  (`id="backup-schedule"` so the schedule line can reach it), `web/src/app.css` (the list's
  styles). **No Go file, no route, no schema change, no server behaviour.**
- **Tests written, not run:** the six cases in `web/tests/first-run.test.mjs` — the fresh
  installation's line order and wording, the copy line saying "not recovery" and naming
  `BRRP_BACKUP_COPY_DIR` while offering no button, 24 hours called out and 12 not (with
  7 × 12 h written as 3.5 days), an account without `invite.manage` not being told to invite,
  a settled installation reading as done throughout, and a configured destination that has
  never delivered not counting as working. **Frontend test count 367 → 373.**
- **Must be confirmed by:** `npm --prefix web test` (367 → 373) and a browser read of
  **Manage server → Maintenance** on a fresh installation, because the lines are drawn from
  four fetched reports — `/api/admin/health`, `/api/admin/diagnostics`, `/api/admin/settings`,
  `/api/admin/alerts` — plus `/api/me/mfa` and the workspace snapshot, and only a browser
  shows them together. W-37's entry below names the journeys that read this panel.
- **What a wrong answer would look like:** a line claiming a check nobody ran (the voice line
  is the one this tranche rewrote to stop doing that); a button opening a tab the account
  cannot use (the invite line's button is withheld without `invite.manage`); or an interval
  longer than twelve hours passing without a sentence, which would let the recovery objective
  and the schedule disagree in silence.

### W-30 — a real HTTPS deployment pins its own host (`98ffec2`)

- **Changed:** `internal/api/middleware.go`, `internal/app/config.go` (`Config.HSTS`),
  `internal/api/middleware_test.go` (new), `docs/security.md`.
- **Tests written, not run:** `TestTransportSecurityHeaderFollowsTheDeploymentMode`.
- **Must be confirmed by:** that test, and `curl -I` against one real `https` deployment and
  one `auto` deployment (which must not carry the header).
- **What a wrong answer would look like:** the header on an `auto` or `http-local` server.
  That is the one outcome that can make an installation unreachable by browser.

### W-08 — the browser harness forwards the switches the config reads (`996bbfc`)

- **Changed:** `web/e2e/run.mjs`, `web/e2e/harness-env.mjs` (new),
  `web/tests/harness-env.test.mjs` (new, six cases).
- **Must be confirmed by:** `npm --prefix web test`, then the default browser invocation —
  its output now carries a `browser suite engines:` line — and one with
  `BRRP_E2E_BROWSER=chrome` where Chrome is installed.
- **What a wrong answer would look like:** a known variable reported as unknown, which would
  stop every run before it starts. The last unit test reads the config and the journeys for
  exactly that.

### W-09 — the cross-platform job runs its Bash under Bash, on LF files (`0fd9b56`)

- **Changed:** `.github/workflows/check.yml`, `.gitattributes` (new),
  `scripts/check-workflow-shells.mjs` (new), `Makefile`, `scripts/release.go`.
- **Must be confirmed by:** a hosted run of the `test` job on all three runners. Nothing on
  this machine can stand in for a Windows runner, and there is no remote yet.
- **What a wrong answer would look like:** a step that behaves differently under Git Bash on
  Windows than under PowerShell — `working-directory: web` steps and the single-quoted
  `node -e` body are the ones to read first.

### W-32 — an operator can read what the server reports about itself (`9a0b9fd`)

- **Changed:** `internal/api/metrics_handler.go` (the `about` block),
  `internal/backup/manifest.go` (`CurrentFormat`), `internal/api/metrics_test.go`,
  `web/src/lib/server-details.ts` (new), `web/tests/server-details.test.mjs` (new, ten
  cases), `web/src/lib/components/admin/Maintenance.svelte`, `web/src/app.css`,
  `docs/admin.md`, `docs/deployment.md`.
- **Must be confirmed by:** `go test ./internal/api -run TestMetrics`, `npm --prefix web
  test`, and a browser read of **Manage server → Maintenance → Server details** after a
  voice call, because the section is drawn from a live report.
- **What a wrong answer would look like:** a figure shown as zero that was never measured;
  the section still polling after it is closed or the tab is hidden.

### W-37 — a new installation starts with scheduled backups on (`9654b5e`)

**Confirm by a run before this branch is merged.**

- **Changed:** `internal/db/models.go` (`NewInstallationSettings`), `internal/api/bootstrap.go`,
  `internal/db/accounts.go`, `internal/api/settings_test.go` (three tests),
  `web/src/lib/components/InstallWizard.svelte`, and eight guides.
- **Must be confirmed by:** `go test ./internal/api -run 'Backup|Installation|OfflineSetup'`,
  then the whole package, then a fresh installation watched for two minutes: one backup
  appears, the checklist's copy line reads as tranche 35 wrote it, and a configured alert
  destination receives the critical condition once.
- **What changed for every other test:** each Go test server that goes through `setupAdmin`,
  and each browser run, now has scheduled backups on. A server alive for more than a minute
  takes a backup into its temporary data directory. Journeys that open the Maintenance tab:
  `authority.spec.mjs` and `ownership.spec.mjs` (both only count controls).
- **What a wrong answer would look like:** a backup attempted before setup; an existing
  installation switched on by the upgrade; an alert delivered on every evaluation.

### W-29 — the credential throttle counts failures (`5ffb6c1`)

**Confirm by a run before this branch is merged. This is the sign-in path.**

- **Changed:** `internal/api/middleware.go` (the limiter), `internal/api/auth.go`,
  `internal/api/mfa.go`, `internal/api/bootstrap.go`, `internal/api/invitations.go`,
  `internal/api/router.go`, `internal/api/throttle_test.go` (new, ten tests),
  `internal/api/auth_test.go` (the old budget test removed), `internal/app/config.go`
  (comment), `web/e2e/run.mjs` (comment), `.env.example`, and three guides.
  `docs/security.md` now records decision D-17.
- **Must be confirmed by:** `go test ./internal/api -run
  'SignIn|Throttle|Invitation|Password|Table|Flood|IPv6|Rated|Name' -count=1`, then the whole
  package (`mfa_test.go` and `sessions_test.go` exercise the re-check paths), then both
  Chromium feature modes, in which every journey signs in.
- **What a wrong answer would look like:** a correct password answered 429 for somebody who
  has not been failing; an unknown and a known username answering differently; any
  signed-in request refused while sign-in is being flooded.
- **The harness still sets `BRRP_LOGIN_ATTEMPTS=400`.** It no longer needs to for successful
  sign-ins. Whether to drop it is a harness decision for the pass, once the suites are green
  with it.

### W-31 — the two media debts (no commit; deliberately not changed)

- **Write first:** a case in `web/tests/voice-state.test.mjs` — a screen's audio follows its
  sharer's volume.
- **Then change:** carry the owner on the screen-audio stream id from the server, parse it
  beside `voice-N` in `voice.svelte.ts`, and apply `participantVolumes[owner]`.
- **Then run:** `voice.spec.mjs`, the screen-audio case, in Chromium and Firefox.
- Not done here because its only evidence is a voice journey.

### W-38 — the reading review (`ae98418`)

- One read-through of every commit from `e0c6280` to `5ffb6c1`; findings are in ledger
  tranche 43. **Still owed:** a second read by a context that has seen none of it.

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

None of these blocks anything in section 2 or 4: each is a decision only the owner can make,
and the code and documents around them already say so rather than guessing. They are gathered
here from wherever they first arose, so one reading covers all of them.

1. **Who receives a code-of-conduct report?** The Contributor Covenant requires an
   enforcement contact and the project does not have one. A report reaches the person who
   published the release you run, as a fallback; a guessed address reaches nobody.
   Where it arose: `CODE_OF_CONDUCT.md`, “Reporting and enforcement”; also recorded as
   owner item PUB-05 in `planning/v1-roadmap-2026-10-01.md`, beside the CLA decision.
2. **What are the support response expectations?** There is no hosted CI, no released 1.0.0
   and no maintainer rota, so any response target written today would be invented. The
   owner decides, when there is something to support, which channels exist and how a
   data-safety report is escalated. Where it arose: `SUPPORT.md`, “Response expectations”.
3. **Does the browser harness follow the shipped gate default, or keep forcing `false`?**
   Following the default is the only way the default job tests what a new installation gets;
   forcing it is the only way the opted-in/opted-out pair stays symmetrical in CI. The answer
   decides two rows in the checklist and the `check.yml` assertion. Where it arose:
   `planning/gate-flip-checklist.md` §5 item 1.
4. **Is `docs/community-guide.md` a living guide or a dated record?** Its first line pins it
   to `0.4.0-alpha.5` and says direct messages “are still planned”, which is no longer true.
   If it is living it needs an edit beyond the gate flip; if it is a record it should be left
   alone. Where it arose: `planning/gate-flip-checklist.md` §5 item 2.
5. **Should v1 let a member remove their own account?** Today a member can export their own
   data — **Your data export**, gated on a recent strong sign-in — but cannot leave on their
   own: only an administrator with the member-management capability can deactivate, remove or
   erase an account (`DELETE /api/admin/users/{id}`, `POST /api/admin/users/{id}/anonymize` in
   `internal/api/router.go`; `PATCH /api/admin/users/{id}` is likewise the only route that
   changes a member's `display_name` or `avatar_color` until W-27 adds `PATCH /api/me`).
   Nothing here builds a self-service departure; `docs/community-guide.md` and
   `docs/known-limitations.md` now state plainly how a member leaves today. Where it arose:
   the account-removal review in this session.
6. **Seven backup copies or fourteen?** At the 12-hour cadence, seven copies is three and a
   half days of history; it was a week at 24 hours. The default is unchanged and
   `docs/admin.md` states the arithmetic. Where it arose: W-37.
7. **Should an installation with scheduled backups switched off report a problem?** It
   reports recovery as `ok`, "Scheduled backups are disabled." Now that a new installation
   starts with them on, "off" is always somebody's choice — but the health check still says
   nothing is wrong. Where it arose: W-37, `internal/app/recovery.go:136`.
8. **Should the browser harness stop raising the sign-in budget?** `web/e2e/run.mjs` sets
   `BRRP_LOGIN_ATTEMPTS=400`. Since W-29 a successful sign-in spends nothing, so the default
   of 20 would do; dropping the override makes the suite test what ships. Where it arose:
   W-29.
