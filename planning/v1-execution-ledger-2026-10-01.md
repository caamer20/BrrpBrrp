# v1.0.0 execution ledger — 2026-10-01

Compact record of implementation tranches toward v1.0.0, per the review-17 handoff
(`planning/handoff-agentic-coder-v1-review-17-2026-10-01.md`). One section per tranche.

---

## Tranche 1 — Multiple-uncertain reply identity and revocable export delivery

**Source and dirty state before the tranche.** Branch `codex/p9-private-reporting`, HEAD
`cf2f692` (application 0.4.0-alpha.7, schema 24, backup format 2). One preexisting dirty
file, `internal/rtc/sfu.go` (SHA256 `d331bbea25d86ae754b50c68ff0227abcfe6e2823d02d6bd91e5e67362d048bc`,
unchanged from review 16/17), reviewed and integrated as its own commit in this tranche.
Two untracked review-17 planning documents were preserved.

### 1a. Multiple uncertain thread replies (review 17, V17-01)

**Failing behavior and reproduction.** Reproduced live in the browser journey added here
(`web/e2e/threads.spec.mjs`, "two uncertain thread replies retry under their own keys"):
submit A whose request is dropped before delivery, then B whose request is stored while
its response is discarded, then press Reply again on unchanged B. Before the fix the
second B submission carried a *fresh* idempotency key (verified in the journey: the two
observed keys differed), so a committed-but-unacknowledged reply could be stored twice.

**Changed files and reason.**

- `web/src/lib/operations.ts` (new) — the composer's explicit association record
  (account slot → operation key + immutable snapshot), validated and bounded, persisted
  beside drafts and the outbox under the same opt-in.
- `web/src/lib/workspace.svelte.ts` — records the association where a thread send is
  claimed (`sendIntoThread`), keeps it live only while the named outbox entry is
  unresolved at the same destination with the same words, ends it on every release
  (acknowledged, refused, expired, discarded, dropped), clears only the matching draft
  snapshot on acknowledgement, restores/persists it per account, and exposes
  `threadComposerOperations` for the composer's decision. The map is `$state` and the
  lookup reads the live outbox *before* any early return: the rendered journey caught
  the first version caching its initial empty answer forever (conditional dependency).
- `web/src/lib/thread-state.ts` — `retryOperationKey`: the shared, testable selection
  the panel calls; exact words select *their own* unresolved operation.
- `web/src/lib/components/NamedThreadPanel.svelte` — key selection through the
  association (never the first outbox entry, never a text search over the queue);
  the "new reply" notice now also covers unconfirmed pending rows.
- `web/tests/thread-operation-identity.test.mjs` (new) — ten deterministic tests over
  the real compiled Workspace: newest/older retry identity, receipt reconciliation,
  never-committed same-key retry, reload survival, refusal/expiry terminal behavior,
  matching-snapshot clearing, account isolation, store validation/bounds.
- `web/tests/workspace-state.test.mjs`, `web/tests/thread-view.test.mjs` — harness
  inlines the new module; replaced the two assertions that pinned the old first-entry
  mechanism with assertions of the association contract and its reactivity.
- `web/e2e/threads.spec.mjs` — rendered journey: both uncertain operations, key reuse
  observed on the wire, server-side message counts (one stored reply per text).

**Commands and results.**

```text
cd web && npm test                      → 297/297 pass (287 before + 10 new)
npm run check                           → 0 errors, 0 warnings
npm run format:check                    → pass
npm run build                           → pass (build f8cdd5847b0d3a64)
PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers \
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm run test:e2e -- --project=threads
                                        → 7 passed (release + threads projects), incl. the new journey
```

**What this proves and limits.** The production caller selects and persists the
association; retries reconcile under the original key; the server stores one reply per
text. The browser journey is the regression for the Svelte reactivity trap the unit
harness cannot see (its `$state` is identity). Not covered here: cross-tab concurrent
submissions and mobile-physical behavior of the same flow.

### 1b. Export delivery authorization (review 17, V17-02)

**Failing behavior and reproduction.** The review-17 diagnostic
(`TestReview15ExportStopsAtRoleCapabilityWithdrawal`, temporary overlay in
`/private/tmp/brrp-review17-xk3zhw46/`) reproduced: a held export response kept
delivering the snapshot after the reader's `channel.read` was withdrawn through the real
role API, while history answered 404. The export registered with an empty channel scope,
which every selective revocation path skipped.

**Changed files and reason.**

- `internal/api/delivery.go` — a registered response now carries its full set of
  conversation scopes (attachment: its channel; export: every recorded conversation;
  unclaimed upload: none). `revoke(channelID)` ends any response carrying that channel.
- `internal/api/export.go` — `downloadExport` registers the response with the export's
  complete scope set *before* the authorization decision, re-establishes authority and
  session through `admitStream` (the same protocol file downloads use), re-checks every
  scope with the same policy a history read applies, refuses a transfer already ended by
  a concurrent revocation, and streams through the same revocable reader. No database
  lock is held across the transfer; no path serves on a lookup failure.
- `internal/api/authz.go` — `endUnreadableStreams` evaluates every scope of every
  affected response with `exportScopeStillReadable` (channel visibility/access lists,
  private accepted membership, feature gate, existence/active state), cached per
  (account, scope); one unreadable scope ends the whole file, unrelated responses keep
  running.
- `internal/api/uploads.go`, `internal/api/groups_test.go` — call sites updated to the
  scoped registration.
- `internal/api/export_revocation_test.go` (new) — six regressions: withdrawal after
  registration while the response is held; allowed/denied/restored delivery;
  per-request range reauthorization with exact bytes; one withdrawn scope of a
  two-conversation export (no partial file, unrelated export untouched); mid-transfer
  withdrawal of a >3 MB export with registry cleanup; session revocation mid-export
  with a surviving session.

**Commands and results.**

```text
GOCACHE=/private/tmp/brrpbrrp-go-build GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test -overlay /private/tmp/brrp-review17-xk3zhw46/overlay.json ./internal/api \
  -run '^TestReview15(ExportStopsAtRoleCapabilityWithdrawal|ThreadUnreadAndArchivedSend)$' -count=1 -timeout 3m -v
→ TestReview15ExportStopsAtRoleCapabilityWithdrawal PASS
  (TestReview15ThreadUnreadAndArchivedSend still fails — tranche 2, section 5)

.tools/go/bin/go test ./internal/api -run '<six new tests>' -count=1     → 6/6 pass
.tools/go/bin/go test ./internal/api -count=1 -timeout 15m              → ok, 176.7s (full package)
.tools/go/bin/go build ./...                                            → pass
```

**What this proves and limits.** Withdrawal cannot fit between the scope check and
admission: registration precedes the decision, so either the check sees the committed
change or the registry ends the already-registered response; a held response delivers no
protected bytes and answers 403. Mid-transfer withdrawal stops the copy and leaves no
registered stream. Not covered here: real-network slow clients across a WAN; files
legitimately downloaded before withdrawal are out of scope by contract.

### 1c. SFU patch integration

**Change.** The preexisting `internal/rtc/sfu.go` patch (74 changed lines, hash above)
was reviewed and committed as its own change: `setEmitter` now writes `peer.send` under
`peer.sendMu` (the lock `emit` reads it under, instead of the SFU lock — two locks
guarding one field is no lock), `selectedCandidatePair` asks the ICE transport for its
selected pair instead of `GetStats` (whose teardown state Pion clears racelessly), and
the relayed-path log compares type tokens (`"relay"`) reported by the new pair source.

**Commands and results.**

```text
.tools/go/bin/go test -race -count=1 ./internal/rtc -timeout 12m          → ok, 94.5s
```

**What this proves and limits.** The focused and full-package race passes confirm the
lock and ICE-transport changes against the existing RTC suites. Media behavior itself
(two-browser journeys, forced-relay paths, physical capture) remains tranche 4 work.

### Remaining dependency / next executable action

- Next: tranche 2 (handoff §5) — thread unread/archive/history, Activity
  acknowledgment, timeout live/audit, member-export journey, authority wording. The
  deliberately-failing `TestReview15ThreadUnreadAndArchivedSend` names the first two
  thread contracts to close.
- External inputs still owed by the owner: hosted Linux schema-13 baseline for CI,
  publication destinations/credentials, physical devices/hosts for later gates. None
  blocks the local implementation above.

---

## Tranche 2 — Threads, Activity, timeouts, member export, authority wording

**Source.** Based on the tranche-1 commits; both review-17 diagnostics
(`TestReview15ExportStopsAtRoleCapabilityWithdrawal`, `TestReview15ThreadUnreadAndArchivedSend`)
now pass against this source.

### 2a. Thread read state, archive and bounded history (review 17, V17-03)

**Failing behavior.** Reading the sole unread thread reply left the channel badge at 1
(counted via the channel cursor), an archived thread accepted replies with 201, the
Reopen action did not exist, and the client re-read every accumulated page on each "load
earlier" with no window bound (also no thread deep link: the channel listing filters
thread replies out).

**Changed.** `internal/db/unread.go` (channel counts and mentions exclude thread replies;
the thread projection measures `thread_reads`); `internal/api/threads.go` (archived write
refusal in `threadAccess`; `unread`/`unread_mentions` on the thread summary, computed in
`listThreads` and `threadWithCounts`); `internal/api/messages.go` (commit-time re-check
refuses archived too); client: `threads.svelte.ts` (bounded 200-row window, one page per
`older()`, `loadAround` for deep links, read-marker method), `NamedThreadPanel.svelte`
(archived branch + Reopen control, mark-read while at latest, "new replies" affordance
instead of yanking history readers, deep-link positioning), `ThreadListPanel.svelte`
(unread badge), `workspace.svelte.ts` (`threadJumpId`, thread branch in `jumpToMessage`),
`thread-state.ts` (`archived` in the send gate).

**Evidence.** Four Go regressions (`thread_unread_projection_test.go`: independent
projections incl. mentions and no phantom unread; archive refuses until reopened; a
mid-send archive race refused at the commit-time check; the review-17 probe). Five
browser journeys (release + threads projects): the two token-fixed, the retry-identity
journey, and the unread/archive deep-link journey; plus five compiled-controller window
tests (`thread-window.test.mjs`).

### 2b. Activity acknowledgment (review 17, V17-04)

**Failing behavior.** The client sent the oldest loaded row as the boundary; the server
acknowledged rows *older* than it (unseen), and the client painted the resulting count
over the newest visible rows (unacknowledged on the server). Visible 05,04,03 could
acknowledge 02,01 while displaying 04,03 as seen.

**Changed.** `internal/api/activity.go` acknowledges everything unacknowledged at or
below the **newest row shown**, reads the exact rows, updates by those ids and returns
them with counts; `activity.svelte.ts` sends that boundary and reflects exactly the
returned ids; `admission.ts` marks by identity rather than count.

**Evidence.** `TestAcknowledgmentIsDurableAndMonotonic` rewritten for the explicit set
(three rows named, zero after, a later arrival counted and not swallowed, a stale
boundary names none and un-acknowledges nothing); nine identity-based client tests;
a rendered journey (`directs.spec.mjs`) covering count, marking exactly, reopen
persistence and a new arrival.

### 2c. Timeout audit and live enforcement

**Failing behavior.** Apply and lift wrote no audit; the uncalled expiry sweep replayed
role snapshots (removed earlier) and audited nothing; `AccountAuthorities`/`principalFor`
built bulk principals without `Suspended`, so a timed-out account kept its community
voice seat and kept streaming community content; state change from a timeout was not
audited.

**Changed.** `moderation.go`: audits inside the committing transaction for apply and
lift; the expiry sweep is wired into maintenance, audited once per lapsed row (`NULL`
actor — "the clock"), bumps the revision and invalidates the accounts' connections;
`timeoutState`'s point-of-use lapse audits too (whichever path runs first records once);
apply now runs `enforceVoiceAccess` and `endUnreadableStreams` for the target after the
commit. `db/roles.go`: `AccountAuthority.Suspended` read with the same expiry rule as
`authz.Resolve`. `authz.go`: `principalForAuthority` (suspension-aware) used by voice
enforcement and stream revocation; `MayReadChannel` refuses a suspended principal for
community content (one policy for requests, voice admission/revalidation and streams;
private conversations never reach it, and the descriptive middleware refusal is
unchanged). `db/roles.go` viewer comment updated; `admin.go` audit view names the clock.

**Evidence.** Four Go regressions (`timeout_enforcement_test.go`): audit once and only on
commit (refused apply/lift record nothing), lapse audited once by either path, a timed-out
member evicted from a community voice room while remaining in a consented private call
and still reading/sending privately, and a held community download stopped while a held
private download completed. Full `./internal/api` suite green afterwards (197 s).

### 2d. Member export journey and request identity

**Failing behavior.** The browser journey returned early on 401/403 (the owner is refused
by privileged-MFA policy, so it always did) and its `Download` locator was a button while
the control is a link; a lost response followed by a second press created a second job.

**Changed.** `ExportPanel.svelte` keeps the operation key until the server answers (a
retry reuses it), and no longer clears its success notice on reload; the journey is
rewritten to exercise the member path end to end: request (lost response, retry under the
same key, one job), poll to ready, download through the real link, inspect the JSON
(format, account fields, own content, no credential strings), delete, and confirm the
download disappears.

**Evidence.** Release project green in both feature modes (see the run recorded with the
tranche commit); the server-side identity/dedupe is pinned by
`TestMemberExportKeyIsTheOperationIdentity` (review-15 fixture, permanent).

### 2e. Authority wording reconciliation

ADR 0001's decision items now state the implemented rule — assigned roles are the
authority, the default role is the floor an account holds only with no assignment row —
and carry an amendment note pointing at the section that records the original defect; the
master plan's policy paragraph and the role screens match ("Held by accounts with no
roles"; the assignment form states the fallback when nothing is selected). No invariant
was weakened and the closed delegated-escalation defect is described as closed.

### Known limits of this tranche

- Cross-device Activity acknowledgment is proven by the server test (stale boundary), not
  by two real browser profiles.
- The 1,000-reply window is proven by the compiled controller against a scripted
  1,000-row cursor; the rendered journey covers the deep link and the archive path on a
  small thread.
- Deployed push delivery, media journeys and recovery remain tranche 3–5 work.

### Remaining dependency / next executable action

- Tranche 3: browser member/media/notification integration (two-browser media, DM/group
  journeys through joined clients, notification boundary), then tranche 4 recovery/packaging.

---

## Tranche 3 — Media profile, notification boundary, recovery and upgrades

### 3a. Approved media profile, measured

`MaxParticipants` was one constant doing two jobs; the process refused its eleventh
voice peer, so the plan's profile was unreachable and the load harness measured a
reduced combination. Now `MaxServerParticipants = 20` / `MaxRoomParticipants = 10`
(with per-peer output slots and routing arrays following the room bound), and the
forwarding harness runs the plan's own arrangement — two publishers, nine viewers
each, twenty peers in two rooms of ten — writing
`planning/m-review-17-checkpoint/video-forwarding-approved-profile.json`: **46.6
Mbit/s mean egress, 47.2 peak, 78 MiB peak heap, 2226 mean goroutines over 20 s**
against the plan's 50 Mbit/s arithmetic. Limits are 1 screen per room / 2 per
server / 1 micro per room member / 20 server microphones, unchanged and matching
the approved numbers. Also fixed: stopping a share released only the video
publication, leaving its screen-audio publication and forwarders behind — one stop
now releases the pair, with a regression.

Commands: `BRRP_VIDEO_LOAD_SECONDS=20 BRRP_VIDEO_LOAD_REPORT=… go test -run
'^TestVideoForwardingLoad$' -v ./internal/rtc` (PASS); `go test -race ./internal/rtc`
(PASS, 125 s).

### 3b. Notification admission boundary, in a real browser

The worker's retention sweep ran only after a successful `showNotification`, so a
profile that declines to display notices grew the dedup store past every bound.
Bookkeeping now runs first and display is best-effort. A rendered journey drives
the worker's own admission path (the page's permission gate cannot be granted in
this harness — Playwright leaves notifications denied, and the workspace correctly
posts nothing then, so the journey says that instead of pretending): one event
posted by two tabs of one account claims exactly once, distinct events claim
separately, stale and over-count keys are swept while keys outside the prefix
survive. Release project: **5 passed**.

### 3c. Recovery, upgrades and schema 25

- **Migration 25** admits the `group_message` interruption preference. Migration 17
  widened `account_activity` but not `private_notification_preferences`, so the route
  accepted the kind and the write failed its CHECK — a member could not mute group
  notifications at all, and the failure was a 500. Found by the private rehearsal;
  schema statements in current docs updated (deployment, plan); `check-schema-docs`
  green.
- **schema-13 seed** now creates an accepted private conversation with submitted
  evidence, a pending request (and a block when the preserved build has the route),
  and writes `seed-manifest.json` with the exact identities it created.
- **Baseline identity**: `scripts/schema13-baselines.json` records the preserved
  executable per platform (sha256 + embedded revision); `--make-seed` refuses a
  mismatch or a platform with no recorded identity; the seed manifest carries the
  baseline identity and the verifying run refuses a fixture of unknown provenance.
- **`--report`** on the schema-13 verifier writes machine-readable results (status,
  checks with failures, identities) for both seed and verify runs; the pass/fail exit
  status and the report come from the same list.
- **Exactness**: the verifier asserts preserved values *and* that the recorded
  identities still resolve, then runs a negative control (deleting asserted rows in a
  damaged copy must be reported as a mismatch).
- **Private rehearsal repairs** (`upgrade-recovery-private.mjs`): exports are read as
  the raw array the API returns, with id/metadata comparison and the missing-file 410
  asserted; the reviewer grant is seeded through the real ownership ceremony, MFA
  enrollment, role route and assignment route (additively, because assignments
  replace the set), both revocations are API-only with strict checks; new assertions
  cover thread follow/read positions, a still-pending group invitation, group owner
  and membership, activity acknowledgment and interruption preference; the
  post-snapshot decisions now include a ban, a password rotation and an erasure, each
  re-applied on the restored copy before cutover; sessions are disclosed as the one
  thing the artifact deliberately does not carry; a negative control damages thread
  and preference state in a copy and requires those tables to be reported as
  mismatched.
- **CI** no longer requests a version: the schema-13 baseline is acquired from an
  operator-provided URL and digest (`BRRP_SCHEMA13_BASELINE_URL/_SHA256`) and the
  rehearsal itself refuses a platform with no recorded identity; `make
  schema13-rehearsal` runs the whole local path with reports.
- **Results (local, `dist/brrpbrrp` built from this tree, schema 25)**:
  `upgrade-schema13-private --make-seed --old-binary dist/brrpbrrp-p9-checkpoint`
  (baseline identity verified) then `--seed-dir … --binary dist/brrpbrrp` →
  **PASS** (13→25, values and identities preserved, negative control detected);
  `upgrade-recovery.mjs` → **PASS 23/0**; `upgrade-recovery-private.mjs` →
  **PASS 172/0**; future-schema refusal smoke: `migrate` exits 1 with "database
  schema 9999 is newer than this binary supports (schema 25)…", `preflight` reports
  `future_schema: true`.

### Known limits of this tranche

- The schema-13 fixture for hosted Linux CI still requires the operator to acquire
  the archived schema-13 executable and record its identity; local qualification
  uses the preserved darwin/x64 binary.
- The notification journey drives the worker boundary directly; the operating-system
  banner path (permission → `showNotification`) is not exercisable in a headless
  harness and remains a physical-acceptance item.
- Media browser journeys still need decoded-frame/two-browser coverage, relay paths,
  late join, stop/restart and move/reconnect (audited by MediaScout; next tranche).
- Rollback rehearsal with the archived alpha.5 release is run in the same tranche;
  see the commit results.

---

## Tranche 4 — The screen path, end to end as far as this host goes

**Source and dirty state before the tranche.** HEAD `0b6108d`, working tree carrying
only `internal/rtc/sfu.go`, `internal/rtc/renegotiation_test.go` and the two e2e
specs from the previous tranche's browser work. Application 0.4.0-alpha.7, schema 25,
backup format 2.

### 4a. Screen audio as its own publication, on its own offer

- **Failing behavior and reproduction.** A two-browser journey produced the server
  answer to the screen's audio announcement as a *second* answer for a description
  that was already applied; the browser refuses that, and the sharer's call ended
  ("Voice negotiation failed") the moment screen audio was included. The server-side
  tests (`TestScreenAudioIsNamedWithoutResendingTheOffer`,
  `TestAScreenAudioTrackIsNotMistakenForTheMicrophone`) pinned both the fix and the
  refusal of an offerless announcement that is not screen audio.
- **Changed files and reason.** `internal/rtc/sfu.go` — the announcement carries the
  publication kind and no description; the audio publication is registered to the same
  peer, bounded separately, and released with the picture; an empty-string description
  that is not screen audio stays a malformed client. The preserved patch's callback
  synchronization, ICE candidate-pair diagnostics and relay classification land in the
  same commit (`ae9b5bd`) with its tests.
- **Commands and results.** `go test ./internal/rtc -count=1` → ok; focused race reruns
  of the three screen tests ×6 → 6/6 pass.

### 4b. Screen publications reach the voice store, and offers pair with answers

- **Failing behavior and reproduction.** `screen.published` never reached the voice
  store: the workspace router forwarded `voice.*` only, so the client's receive path
  was dead code and no viewer ever negotiated a receive line (journey
  `voice › a shared screen reaches another browser…`). With routing restored, the
  sharer's call ended whenever a viewer was present: a self-initiated view offer was
  created while the publisher's own offer still waited for its answer, leaving the
  connection in `have-local-offer` and refusing that answer.
- **Changed files and reason.** `web/src/lib/workspace.svelte.ts` (route `screen.*`
  events into the voice store); `web/src/lib/voice.svelte.ts` (explicit offer/answer
  queue `negotiate`/`settleAnswer`; publish goes through it; one receive negotiation
  adds both receive lines; audio named without a description); `web/e2e/voice.spec.mjs`
  (journey asserts both clients stay connected through share, stop and restart, the
  viewer learns the publication, the publisher encodes frames, and stopping releases
  it).
- **Recorded gap, not a narrowed claim.** A browser viewer still decodes no frames:
  the server's view exchange leaves the view line `inactive`. This is a skipped test
  with its measurement and the two-sided fix it needs, and it stays in the "Known
  issues" of the release record.
- **Commands and results.** `npm run check` → 0/0; `npm run build` → ok; voice project
  → 10 passed, 1 skipped (the gap); full default suite → 25 passed, 1 skipped; full
  opted-in suite → 39 passed, 1 skipped.

### 4c. Gate budgets corrected by measurement

- The whole-tree `-race` sweep hit the API package's 40-minute budget at **2402 s**
  while reporting no failure of its own. Budgets raised to 60 m in `Makefile` and
  `.github/workflows/check.yml`, with the measurement recorded in both.
- One RTC handshake poll missed its 5-second window under that parallel load while the
  same tests pass 6/6 in isolation; the poll's budget is 20 s. Both changes are in
  commit `4cf45f2` with their reasons.

### 4d. A data race in the harness teardown, found by the sweep

- **The finding.** The whole-tree `-race` sweep reported one data race during
  `TestScreenPacketsReachAnotherParticipant`: `SFU.leavePeerOwned` deleting from the
  peers map on Pion's connection-state goroutine while a **test** goroutine read the
  same map. The production path was correct — `leavePeerOwned` holds the SFU lock —
  and the reader was test code reaching into `sfu.peers[...]` without it, including
  reads that ran while a *previous* test's teardown callbacks were still finishing.
- **The fix.** One locked accessor (`sfuPeer`) replaces 26 unlocked reads in
  `renegotiation_test.go`; the remaining direct reads in the other test files were
  already inside `sfu.mu`; and `newTestSFU`'s teardown now waits (bounded by
  `handshakeBudget`) until the peers map is empty, so a callback from a finished test
  cannot straddle into the next one. Two harness waits that missed a 5-second window
  under parallel `-race` load are `handshakeBudget` (20 s, polled).
- **Reproduction and verification.** Before the fix: 1 race in a `-count=4` race run
  (405 s). After: `-count=4` race runs, **0 data races**, exit 0 (425 s). The focused
  screen tests also pass 6/6 in isolation. The API package, which is the package the
  old 40-minute budget could not contain, then passed alone under race:
  **2573.2 s, exit 0, 0 data races**.
- **Not the product.** No production code changed for this; the record keeps it in the
  tranche because the sweep is what exposed it and because a flaky harness is how a
  real race gets ignored later.

### 4e. Dependency and vulnerability gates

- `npm audit --audit-level=moderate` reported **1 high** (six `devalue` advisories,
  reached through `svelte`); `npm audit fix` moved devalue 5.9.2 → 5.9.4 and the audit
  is clean. The built bundle does not contain it (`devalue` is absent from `dist/`), so
  the exposure was tooling-side, but a high advisory in the lockfile of a release
  candidate is not something to carry in the record.
- `govulncheck` (v1.8.0, CI's version): **0 vulnerabilities** in code and imported
  packages; 1 in required modules that the code does not call.


---

## Tranche 5 — The 0.4.0-alpha.8 candidate

- **Version identity.** Four places, not three: `internal/app/version.go`,
  `web/package.json`, `web/package-lock.json`, `scripts/release.go`. The identity test
  (`TestVersionIdentityAgrees`) caught the lockfile, which is why it exists.
- **Artifacts.** `make release GO=.tools/go/bin/go` built six native packages plus the
  Docker source archive from a clean tree (`vcs.modified=false`,
  `vcs.revision=4cf45f2…`), wrote `SHA256SUMS`, and every digest was checked with
  `shasum -a 256 -c`.
- **Consumer execution.** The `darwin/amd64` archive was extracted and its binary run
  by `scripts/check-package.mjs`: CLI setup, backup, restore, refused double restore,
  folder lock, healthcheck, production guard, embedded UI and manifest, graceful stop
  — all pass. The first execution hit the script's per-command budget because macOS
  scans a new Mach-O on first run; the rerun is the evidence.
- **Rehearsals.** Community 23/0 (schema 25, FULL→FULL), private 172/0, rollback 57/0
  (alpha.7 archive at schema 21 → this build at schema 25), schema-13 upgrade 28/0
  (13→25, baseline `2869db55…` verified by digest, negative control detected).
- **Release-engineering checks.** Dependency notices/SBOM current (39 components, 20
  license texts); source package 487 files/50 native documents with every local link
  resolved; Docker input audit 24 COPY sources; browser-step audit 2 steps.
- **Documents.** `docs/releases/0.4.0-alpha.8.md` (this candidate's record), README,
  the plan and ledger headers, and the production-readiness commands.
- **Known limits.** No hosted CI execution (no remote); no Docker image build; the
  Linux and Windows packages are built and digest-verified but not executed; the
  screen-decode gap and the host/physical/beta gates in the release record remain
  open.

## Tranche 6 — The interrupted screen-view change, finished

**Source and dirty state before the tranche.** HEAD `f8bbc74` on
`codex/p9-private-reporting`, application 0.4.0-alpha.8, schema 25, backup format 2,
carrying the interrupted four-file change (+281/−35, `git diff` SHA-256
`e4fd0dcf1d23712343ebb17ef028ae969d001aefeb9b6da3956d2fa2010f8a24`) from the session
that was ended mid-change. Until it was finished, the change made the tree worse than
HEAD: `TestAnUnmarkedSecondOfferIsStillRefused` failed, the two-browser journey failed
at `voice.spec.mjs:632` (the publisher encoded 0 frames in 30 s — an assertion that
passes at HEAD), and `decodedVideo(viewer)` was called six times and defined nowhere.

### 6a. Rehearsal teardown on every exit path

- **Failing behavior and reproduction.** Ten `brrpbrrp serve --data-dir …` processes
  from aborted rehearsal runs (3 started 30 September, 7 on 1 October) were still
  listening when this session began — `ps aux | grep 'brrpbrrp serve' | grep -v grep`
  counted 10. The rehearsal scripts stop a server only on the path that reaches
  `stopServer`: `check()` throws on the first failed check, the top-level `catch`
  printed a message and called `process.exit(1)`, and the live children were never
  signalled. A failed check is the ordinary case — the alpha.6 binary below fails the
  same way.
- **Changed files and reason.** `scripts/upgrade-recovery.mjs`,
  `scripts/upgrade-recovery-private.mjs`, `scripts/upgrade-rollback.mjs`,
  `scripts/schema13-seed.mjs`, `web/e2e/run.mjs` — every spawned rehearsal server is
  tracked in a set; a `process.on('exit')` handler kills whatever is still live, and
  SIGINT/SIGTERM handlers stop children gracefully (SIGTERM, 1.5 s, SIGKILL) before
  exiting. `upgrade-rollback.mjs` also tracks its one-shot CLI child and its
  expected-to-refuse probe.
- **Commands and results.** The 10 orphans were stopped by PID (`kill`, then
  `ps … | wc -l` → 0, no rehearsal or `make` was running). Interrupt test: the private
  rehearsal was started against `dist/brrpbrrp-e2e`, SIGTERM was sent to the parent
  once its child server was observed (pid 39741), the parent exited rc 1, and
  `pgrep -f teardown-test4` found nothing. Failed-check test: the same rehearsal with
  the stale `dist/brrpbrrp-alpha6-preview` fails "a group created through its own
  route" (HTTP 404), the run aborts, rc 1, and `pgrep -f teardown-test5` found
  nothing.
- **What the evidence proves.** Three exit paths — normal stop, interrupt, failed
  check — leave zero processes. It does not prove a `SIGKILL`ed parent is covered
  (nothing can), and the e2e harness's signal backstop is verified by inspection
  rather than by an interrupted browser run.

### 6b. Why the sharer's own share died — mechanism from the trace, not from the theory

The interrupted change reasoned about the server but died on the client. Evidence, in
the order it was collected:

- The journey's own diagnostic, taken after the share started, on the **sharer's**
  page: `share: {grabbed: 1, stopped: 1}`, both screen senders
  `readyState: "ended"`, `framesEncoded: 0`, and a transceiver list containing
  `recvonly/recvonly:video` — the publisher had asked to view *its own* screen.
- A stop-trace instrumented into the spec (`MediaStreamTrack.prototype.stop` recording
  stacks) identified the caller exactly: `at sh.attachScreen (index-….js) at
  s.ontrack (index-….js)` for the video track labelled `Shared two-browser screen` and
  its audio. `attachScreen` stops the previous stream; the previous stream was the
  capture.
- The mechanism that made `ontrack` fire: the server announced the new publication to
  the whole room, the publisher included. The client's `screen.published` handler
  asked for a view (`requestScreenReceive`), the server answered the new
  `recvonly` line, and the browser reported the negotiated receive direction —
  at which point the arriving video track was treated as "a screen to display".
- A Go reproduction of the same sequence (`publisher` publishes through the production
  signal path, then sends an unnamed offer with `recvonly` video+audio lines) showed
  the server accepting it, which is why the client half — not the server half — was the
  one that ended the share.
- Related, and kept: `internal/api/voice.go` leaves the call when an **unnamed**
  `rtc.signal` fails ("Voice negotiation failed"), so any refusal of a view offer would
  also have dropped the sharer from the call. That is the path the old
  `TestAnUnmarkedSecondOfferIsStillRefused` protected, and it is why the contract had
  to be re-stated rather than deleted.

### 6c. The fix, and the contract it states

- **`internal/rtc/sfu.go`.** `announcePublication` no longer sends the announcement to
  the publication's owner. An unnamed offer is now refused only when it adds a
  *sending* video section this connection has not negotiated (`unnamedOfferAddsVideo`,
  decided from the offer's own `m=`/`a=recvonly`/`a=msid`/`a=mid` text);
  `viewRequestKinds` is rebuilt on the same section parser and accepts `a=recvonly` or
  the view stream id (in either msid position); the screen output is still created
  before `SetRemoteDescription`, which is what makes the answer's section the one the
  screen travels on.
- **`web/src/lib/voice.svelte.ts`.** The client asks with `recvonly`; it ignores
  `screen.published` for a publication it owns; and the local capture is a field of its
  own (`capture`) so nothing arriving on the connection — `ontrack` included — can stop
  it. `leave()` releases the capture, which also stops a stale one from blocking the
  next call.
- **`internal/rtc/renegotiation_test.go`.** `TestAnUnmarkedSecondOfferIsStillRefused`
  is replaced by two tests: the denial
  (`TestAnUnmarkedSendingVideoLineIsStillRefused` — a `sendonly` video line naming no
  publication) and the permitted control
  (`TestAViewLineIsAnsweredAndCannotPublish` — a `recvonly` line is answered, registers
  no publication for the viewer, and media arriving on that connection is never
  forwarded). `TestAViewerReceivesScreenFramesOnTheOfferItMade` (from the interrupted
  change) counts one video section in the answer, requires `sendonly`, and reads frames
  off the receiver.
- **`web/e2e/voice.spec.mjs`.** `decodedVideo()` is written (frames decoded + widest
  decoded frame width from inbound video stats); the temporary `DIAG-PUB` block and the
  stop-tracing instrumentation are removed; the journey asserts the viewer decodes the
  shared 320-pixel picture, stops decoding within 4 s of the share stopping, decodes
  again after a restart, and that the sharer stays connected throughout.
- **`docs/adr/0019-media-architecture.md`.** The section "A viewer's line carries the
  screen, and a publisher never views its own share" records the decision: an offer
  that names no publication may ask to receive and may not ask to send, and a view line
  can never become a publication.

### 6d. Commands, results and counts

- `$GO test ./internal/rtc -count=1` → **ok** (all tests; the failing test from the
  interrupted change now passes, and the rewritten pair passes).
- `$GO test ./internal/rtc -count=1 -race` → **ok**, 22.9 s.
- `npm --prefix web test` → 304/304 pass; `npm --prefix web run check` → 0 errors,
  0 warnings; `npm --prefix web run format:check` → clean.
- Voice browser project (`BRRP_E2E_PRIVATE_CONVERSATIONS=0 … --project=voice`) →
  **10 passed, 0 failed, 0 skipped** (the `fixme` is gone). The single journey
  `a shared screen reaches another browser and stops cleanly` passes in ~11–12 s,
  against ~31 s of failure before the fix.
- Twenty consecutive voice-project runs: see the tranche-6 addendum below.

### 6e. What this evidence proves, and what it does not

- A synthetic RTP run (the Go tests) proves the server binds the viewer's own line to
  the transceiver carrying the screen, answers it `sendonly`, and forwards the owner's
  frames; it does not prove a browser decodes them.
- A generated browser stream (the journey, `canvas.captureStream`) proves the browser
  half — decode counts, frame width, stop and restart — on this host, in headless
  Chromium, with a canvas capture. It says nothing about a physical display capture,
  screen audio from a real system, Firefox/WebKit, or a network path other than
  loopback.
- The refusal contract is proven for the offers the tests construct, not for every
  malformed client an attacker could write; the property that bounds that ("media
  arriving on a view line is never forwarded") is enforced at the forwarding boundary
  by publication ownership, which is tested separately
  (`TestAPacketIsForwardedOnlyForThePeerThatOwnsThePublication`,
  `TestOnlyTheOwnerCanFeedAPublishedScreen`).

### 6f. Twenty consecutive voice-project runs

Run after the change was implemented, built and formatted (the artifact is the
`dist/brrpbrrp-e2e` built from this commit's sources immediately before the twenty
runs, SHA-256 `ff9738e92a7f34732e9f90fbe53d7e7bae08377b2a40a545efe5462ba3c239ee`):

- `BRRP_E2E_BINARY=…/dist/brrpbrrp-e2e BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e -- --project=voice`
  twenty times in sequence, one worker, a fresh server and data directory per run.
- **20/20 runs passed**, each 10 tests, 0 failed, 0 skipped (the release project's six
  journeys run first as the voice project's dependency, which is why each run reports
  ten). Wall time for the sequence: 1038 s.
- What this establishes: the voice journeys, including the two-browser screen journey
  and its decode/stop/restart assertions, are stable across twenty fresh servers on
  this host with a generated capture. It does not establish anything about a physical
  microphone or display, Firefox/WebKit, or a non-loopback network.
- The intermittent `Join voice → Leave voice` timeout seen once at HEAD
  (`voice.spec.mjs`, the "every refusal is explained" journey, in a full-suite run
  immediately after another voice journey) did **not** reproduce in these twenty runs
  nor in the two full-suite runs of this tranche. The evidence available so far — the
  client's 25-second join timer reports "Voice timed out. Check the server UDP ports or
  enable TURN.", and the server leaves an unnamed failed `rtc.signal` with "Voice
  negotiation failed" — does not yet distinguish a stalled ICE handshake from a
  `createPeer` that returned early, because the harness discards the server's log tail
  when a run fails. That is the next step: keep the server's tail on failure, then
  reproduce and read it.

### 6g. A detached peer no longer holds the account's voice place

- **Failing behavior and reproduction.** `TestADetachedPeerDoesNotHoldTheVoicePlace`
  (new in `internal/rtc/rooms_test.go`): join, `Detach` — what the transport cleanup
  does when a page's socket goes away and the session is still valid — then join again
  from a new connection. Before the fix it failed with `a detached peer refused the
  account's next join: you are already in voice, possibly in another tab`: the place was
  held for the full 30-second drain, which is exactly what reloading the page during a
  call produces.
- **Changed files and reason.** `internal/rtc/sfu.go` — `Join` releases a peer whose
  signaling connection is gone (`attached()` reports whether a delivery sink exists) and
  admits the new connection; an attached peer is still refused, and the replaced peer's
  resume token cannot resurrect a second place for the same account.
- **Diagnostics added for the next occurrence of the intermittent.** `web/e2e/run.mjs`
  prints the server's log tail when a suite fails — it kept that tail for readiness
  failures only, which is backwards, because a join that times out is a server event (a
  refusal, a stalled handshake, a detached peer) and none of those is visible in a
  screenshot. `web/e2e/voice.spec.mjs` prints the voice panel's own text for a failed
  voice journey: the store's error, the screen share's message and the control state.
- **Commands and results.** `go test ./internal/rtc -count=1` → ok with the fix, and the
  new test fails without it (message above). After the fix and the diagnostics:
  `--project=voice` × 20 → **20/20**; the full default-mode suite × 5 → **5/5, 25
  journeys each, 125 journeys, 0 failed, 0 skipped**; no failure dump was produced in
  any run, so the intermittent join did **not** reproduce in these 29 suite executions.
- **What the evidence proves, and what it does not.** It proves a real defect in the
  join path — a page reloaded during a call cannot rejoin for 30 seconds, and is told a
  tab that does not exist is holding the call — and that the suites are green with the
  fix. It does **not** prove this defect caused the single intermittent join seen at
  HEAD: that mechanism was never reproduced, and the fix is not claimed as its cause.
  What is different now is that the next occurrence leaves the server's own reason in
  the log instead of a screenshot of a wait.

### 6h. Both browser modes, and the second intermittent the private mode caught

- Full default-mode suite (`BRRP_E2E_PRIVATE_CONVERSATIONS=0`) × 5: **5/5, 25 journeys
  each, 0 failed, 0 skipped** (125 journeys).
- Full private-conversations suite (`BRRP_E2E_PRIVATE_CONVERSATIONS=1`): the first run
  failed one journey — `threads.spec.mjs`, "a thread reply moves only the thread…", a
  strict-mode violation where `getByText('Unread and archive')` matched both the thread
  title and the root message body ("A root line for the unread and archive journey").
  It passed in five default-mode suites because the panel's loaded window decides whether
  the root is rendered; two more projects run before it in private mode. The assertion
  now names the title exactly; the rerun is **39 passed, 0 failed, 0 skipped**, and the
  `threads` project alone was green before the whole suite was repeated.
- What this establishes: both feature modes are green with the screen-view change, the
  voice-place change and the diagnostics, on this host, in headless Chromium, with
  generated media. It establishes nothing about Firefox, WebKit, physical devices or a
  non-loopback network.

## Owner decisions — 2 October 2026

Recorded from the kickoff prompt's single ask, so each Phase A–C item is closed or
descoped by a decision rather than by silence.

- **Off-machine copy: not now.** No remote is configured and none is to be created.
  Phase A.1 stays open, blocked on the owner, and nothing else in A–C depends on it.
  The consequence is stated plainly: the repository exists on one disk.
- **External TURN and IPv6: documented limit.** ENG-07 is descoped to "embedded relay
  only; IPv4 media", to be recorded in the compatibility statement and the roadmap's
  boundaries.
- **Message formatting: emphasis and block quotes only.** ENG-08 keeps lists out of v1;
  the tokenizer change is bounded to what it already parses.
- **VP8 fallback: acceptable.** ENG-05 registers VP8 as a fallback behind the H.264
  preference and amends ADR 0019.
- **Feature gates default-on: before the beta, after review entry.** Both
  `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS` stay off
  until the independent reviews are entered and their P1s are fixed; the beta then runs
  the configuration that will ship (Phase C, item 8).
- **Account limit: 100 for v1.** The 250-account profile is not a v1 requirement; the
  capacity tooling stays and the larger profile is documented as future work.
- **Long-lead items: none available yet.** No code-signing certificate, reviewer,
  accessibility tester, pilot community, Windows or Docker host, phone/UDP-blocked
  network, off-host storage or second releaser is available today. The gate documents
  and evidence templates stay ready for whoever can run them (Phases D–I).

## Tranche 7 — Phase A: a clone that passes its own checks

**Source and dirty state before the tranche.** HEAD `f8bbc74` (application
0.4.0-alpha.8, schema 25, backup format 2) plus the completed screen-view change.
The owner answered the kickoff list on 2 October: no remote for now, external TURN and
IPv6 documented as a limit, emphasis and quotes only for message formatting, a VP8
fallback is acceptable, the feature gates open before the beta after review entry, the
100-account limit stays for v1, and no long-lead item is available yet (decisions are
recorded above).

### 7a. The documentation checks could not pass on a fresh clone

- **Failing behavior and reproduction.** In a tree containing only tracked files
  (`git ls-files -z | tar -cf - --null -T -` into an empty directory),
  `node scripts/check-doc-links.mjs` failed with "docs/next-release.md is listed here
  but does not exist" and `node scripts/check-schema-docs.mjs` died reading that file
  (ENOENT). `.gitignore` excludes it on purpose as an operator note, and CI runs both
  scripts on a clone, so both steps were red for every consumer.
- **Changed files and reason.** `scripts/check-doc-links.mjs` — the note is an
  *optional* document: skipped when absent, checked when present, and a link *to* it is
  not a broken route in a clone that does not have it. `scripts/check-schema-docs.mjs` —
  the same, plus a required document that has gone missing is now a failure rather than
  a crash. Both gained `--self-test` (five cases each: a tree without the note passes, a
  broken link in a tracked document fails, a missing required document fails, a present
  note's links and numbers are checked, and the ignore rules still ignore).
- **Commands and results.** Both scripts and both self-tests pass in this checkout and
  in a tracked-files-only copy. Wired into `make check` and into CI beside the two
  existing steps.

### 7b. Formatting and module drift are gates now

- **Failing behavior.** `gofmt -l internal cmd scripts` listed 16 files, and
  `go mod tidy -diff` wanted `github.com/pion/stun/v3` as a direct requirement rather
  than an indirect one. Both were found by a manual sweep, which is not a gate.
- **Changed files and reason.** The 16 files were formatted and the module file was
  tidied; `make check` and CI now run `gofmt -l internal cmd scripts` (empty or red) and
  `go mod tidy -diff` (silent or red). `GOFMT` is derived from `GO`'s directory when
  `GO` is a path, so the gate cannot silently check nothing because the binary was not
  found.
- **Results.** `gofmt -l` empty; `go mod tidy -diff` clean; both run inside the clone
  check below.

### 7c. The packaging audit was already red at HEAD

- **Failing behavior and reproduction.** In a detached worktree at the *pre-session*
  revision (`git worktree add --detach /tmp/pristine f8bbc74`) and with this tree's Go
  toolchain, `node scripts/check-source-package.mjs` fails: `docs/v1-progress.md links
  to ../planning/v1-execution-ledger-2026-10-01.md, which the source package does not
  contain`. So `make check` was red before this session began, and the roadmap's new
  state column repeated the same mistake.
- **Changed files and reason.** `docs/v1-progress.md` and `docs/roadmap.md` name the
  ledger the way the rest of that file already does, in backticks: the planning
  directory is the development record and is deliberately not packaged, so a shipped
  document must not link into it. `scripts/release.go` packages
  `scripts/check-schema-docs.mjs` — the audit caught that omission the moment the gate
  was added to `make check`, which is the audit working.
- **Results.** The audit passes: 489 files, 51 native package documents, every
  documented local link resolved in both sets, every Makefile script and `.dockerignore`
  input packaged, the operator-specific notes excluded.

### 7d. One truthful status page

- `docs/v1-progress.md`'s status table no longer says groups are unbuilt, threads are
  outstanding, DM activity and notification preferences do not exist, or export and
  group lifecycle cleanup remain; it carries one WP-14 row instead of two, and the
  WP-13/WP-16–22 row names the phase-B media and platform gaps instead of listing built
  features as future work.
- `docs/roadmap.md` gains a dated state column and no longer claims there are no direct
  messages or screen sharing; README's scope paragraph describes threads, roles and
  delegation, consented DMs and groups, screen sharing with its own audio, and the two
  gates; CONTRIBUTING's verification block asks for the race budget `make test` actually
  uses (60 minutes) and says why.
- Dated records — the release records, this ledger's earlier tranches, the
  implementation sections — are untouched.

### 7e. Phase A status

- **A.1 off-machine copy: blocked on the owner**, who answered "not now". The repository
  remains on one disk; this is the only Phase A item that is not done.
- **A.2, A.3, A.4: done**, with the evidence above.
- **Exit criterion.** `make check` passes in a fresh clone of this commit: the
  repository was cloned into an empty directory (`git clone --no-hardlinks`), which
  therefore has no `node_modules`, no `.tools` and no untracked operator note; with
  `GO`/`GOMODCACHE` pointed at this machine's toolchain, `make check` ran `npm ci` (102
  packages), the production build, `go vet`, `go mod tidy -diff`, `svelte-check`,
  formatting, the licence check, both documentation checks with their self-tests, the
  Docker input audit, the source-package audit and the browser-step audit, and exited 0.
- **What this does not prove.** It is one machine and one operating system: "on any
  machine" needs the hosted CI run, which is Phase C's first hosted run and is not
  available yet. The clone check also does not run the Go tests or the browser suites —
  those are separate gates with their own evidence above.

## Tranche 8 — ENG-02: the video feedback path

**Source and dirty state before the tranche.** HEAD `cbf3a54` (tranche 7 committed),
working tree clean, application 0.4.0-alpha.8, schema 25, backup format 2. Phase B's
media rows come first because media carries the most risk and the code was already open;
ENG-02 is the row this tranche closes.

### 8a. A viewer's keyframe request died one hop short of the publisher

- **Failing behavior and reproduction.** `TestAViewersKeyframeRequestReachesThePublisher`
  (new): a viewer's PLI, written the way a browser writes one, reached the server and was
  discarded — the RTCP drain on every screen output read the packet and dropped it, so a
  viewer that joined mid-stream had no way to make the publisher start a picture. The
  first instrumented run showed the request arriving and the forward being suppressed
  (`[rtcp-drain] peer=bo … PictureLossIndication bbf0fa03 bbf0fa03`), which produced two
  further fixes the theory had missed: a request that cannot be *sent* — the server has
  not yet seen the publisher's first frame, so it has no media source to name — must not
  consume the rate-limit window, and a forwarded PLI must carry the publisher's SSRC in
  **both** SSRC fields or the publisher's own demultiplexer drops it as unhandled.
- **Changed files and reason.** `internal/rtc/sfu.go`: `ccm fir` is registered
  (`RegisterFeedback`; Pion's NACK configuration already offers `nack` and `nack pli`);
  each screen output's RTCP drain forwards PLI/FIR through `requestKeyframe`; the
  publisher's sending SSRC is learned from the track it sends (`Peer.incomingSSRC`); the
  forward is rate-limited per room and kind (`keyframeInterval`, 500 ms) and a request
  with nothing behind it is dropped *without* consuming the window. `internal/rtc/keyframe_test.go`
  is new; `internal/rtc/sfu_test.go` and `video_load_test.go` gain the harness to observe
  it (a client-side RTCP channel per peer, and an event channel that records what a client
  was told).
- **Commands and results.** `go test ./internal/rtc -count=1` → ok (17.0 s);
  `-race` → ok (34.2 s). Eight new tests, each with a permitted control beside it: a
  request reaches the publisher addressed at its own SSRC; a second request inside the
  window does not; an ordinary receiver report never becomes one (and a real request
  after it still does); a request for a stopped share is dropped; binding a line to a
  live share produces a request with no client RTCP at all; the answers offer `nack pli`
  and `ccm fir` and do not offer feedback this build does not implement.

### 8b. A viewer that joined after the share started was never told

- **Failing behavior.** The announcement existed only at registration. A client that
  joined afterwards negotiated nothing, so the server had no output to write to for it:
  the criterion "a viewer who joins after a share started decodes within a few seconds"
  was unreachable regardless of the feedback path, because there was nothing to bind and
  therefore nothing to request a keyframe for.
- **Changed files and reason.** `announceJoin` now emits `screen.published` for the
  room's *live* publications to the peer that just joined (not to their owner), through
  the same event the registration path uses, so the client keeps one code path.
- **Tests.** `TestAViewerJoiningAfterAShareStartedIsToldAboutIt` (the announcement names
  the screen and its owner) and its control `TestAStoppedShareIsNotAnnouncedToAJoiner`.

### 8c. The browser proof, and what it is not

- The two-browser journey gains a **third viewer** (a new account, since one account
  holds one voice place) that joins *while the share is running* and must decode the
  320-pixel canvas within ten seconds. Journey time went from ~12 s to ~16–21 s.
- What this does not establish: a browser cannot distinguish a keyframe produced because
  the request was forwarded from the encoder's next natural keyframe, so the forwarding
  itself is proven at the RTCP level in Go and the browser test proves the late-join path
  end to end. Neither layer says anything about a physical display, a lossy network, or a
  browser other than Chromium.

### 8d. One leak doubled, and closed

- The keyframe windows are keyed by room and kind, like the publication registry — which
  is itself never removed when a room empties. `leavePeerOwned` now deletes both when the
  last peer of a room leaves, so the new map does not double an existing leak and the
  existing one is closed. `TestAnEmptyRoomLeavesNoRegistriesBehind` pins it, with the
  control that the room is usable again afterwards.
- Kept deliberately: the rate-limit window survives a stopped-and-restarted share for at
  most 500 ms. A restart inside that window may miss one request; the publisher's first
  frames are a keyframe, so the cost is bounded and the alternative is an extra
  bookkeeping key per publication.

### 8e. Both browser modes, and the whole rtc package

- `go test ./internal/rtc -count=1` → ok; `-race` → ok.
- Full default-mode suite (`BRRP_E2E_PRIVATE_CONVERSATIONS=0`) → **25 passed, 0 failed,
  0 skipped**. Full private-conversations suite (`=1`) → **39 passed, 0 failed, 0
  skipped**. Both re-run against the final artifact
  (`dist/brrpbrrp-e2e` sha256 `9ee80c9c5ebc0762d1afd946cd081a64d4a7ff1a3a7a686b6979654edce40da4`)
  after the last code change.
- One intermediate voice-project run failed in a release-project journey
  (`getByLabel('Your preferences')` not found) and did not reproduce in the release
  project alone or in two subsequent full runs; the harness's new failure diagnostics
  printed the server log, which showed nothing but startup. It is recorded here as an
  unexplained single occurrence rather than as a fixed defect.

## Tranche 9 — ENG-03: screen presets and the server-side ceiling

**Source and dirty state before the tranche.** HEAD `2331fe4` (ENG-02 committed),
working tree clean, application 0.4.0-alpha.8, schema 25, backup format 2. The owner's
decisions from 2 October apply: emphasis-only formatting (lists deferred), a VP8
fallback is acceptable, the gates open before the beta after review entry, 100 accounts
for v1, external TURN/IPv6 documented as a limit.

### 9a. Two capture profiles, as data

- **What and why.** A plan-required profile: 720p at 15 fps for movement, 1080p at 5 fps
  for text, a sender bitrate cap, and a server-side per-publisher ceiling so a share
  cannot starve voice. One profile cannot serve both pictures: a film at 5 fps is a
  slideshow, a document at 15 fps spends four times the bandwidth on a still image, and
  the text preset therefore keeps resolution and drops frames while the motion preset
  does the opposite.
- **Changed files and reason.** `web/src/lib/screen-share.ts` — `SCREEN_PRESETS` (capture
  constraints, encoding cap and rate, degradation preference, content hint),
  `requestScreen(…, preset)`, `applyScreenPreset(sender, preset)`,
  `screenPresetFromValue`. `web/src/lib/voice.svelte.ts` — the choice is state, is
  passed to the capture, is applied to the sender once it exists, and can be changed
  *during* a share (the live sender is re-parameterized and the capture re-asked).
  `web/src/lib/components/VoiceControls.svelte` — a native select beside the share
  control, so keyboard, screen-reader and phone behaviour come with it.
  `web/tests/voice-state.test.mjs` — its prelude stubs the module seam the store touches
  in a class-field initializer, because that runs before any test.
- **Commands and results.** `npm --prefix web test` → **310/310** (was 304; six preset
  tests added); `npm --prefix web run check` → 0 errors, 0 warnings (it caught
  `degradationPreference`, which belongs to the send parameters rather than to an
  encoding, before the browser did); `format:check` clean.
- **What the unit tests assert, and their controls.** The table's numbers are the
  plan's; every sender ceiling sits below the server's 2.5 Mbit/s, so a client that
  obeys its cap never meets the enforcement; `requestScreen` asks for the preset's
  constraints and *not* for audio unless it was requested; applying a preset writes the
  ceiling and the degradation preference and leaves a field it does not speak about
  (`rid`) alone; a sender that refuses the parameters, or has no encodings, is reported
  as `false` rather than thrown; an unknown stored preference falls back to motion
  (including `constructor`, because storage is editable).

### 9b. The ceiling is enforced where every packet passes once

- **Changed files and reason.** `internal/rtc/bitrate.go` — `bitrateMeter` (a token
  bucket over payload bytes, one second of credit for the burst a keyframe arrives as,
  policing rather than shaping so an over-budget stream cannot queue without bound),
  `ScreenBitrateCeiling` (2.5 Mbit/s) and `ScreenAudioBitrateCeiling` (64 kbit/s) from
  the plan's initial profile, `Peer.admit`, and `SFU.bitrateDrops` for the count an
  operator surface will read (ENG-14). `internal/rtc/sfu.go` — `forwardPublication`
  polices screen video and screen audio **before** the packet is copied to any viewer,
  which is the only point every packet passes exactly once; the microphone is not
  metered at all, because it is the call and is bounded by the slot forwarder and by
  `voice.speak`.
- **Commands and results.** `go test ./internal/rtc -count=1` → ok (12.6 s); `-race` →
  ok (29.8 s); `go vet ./...` clean; `go test ./... -count=1` → all packages ok.
- **Tests and controls.** The meter's refill, burst cap and drop arithmetic on an
  injected clock; a publisher 480 kB into a 312 kB burst is bounded with drops > 0; the
  permitted control — 60 kB in one burst is forwarded in full with zero drops, so
  "refuse everything" does not pass; screen audio's ceiling is separate and does not
  drop video packets; the microphone has no meter.
- **What it does not prove.** The packets are synthetic RTP in one process: nothing here
  pushed a real encoder past 2.5 Mbit/s, and no browser was observed reacting to a
  dropped frame. The plan's qualification for the viewer/bitrate profile still needs the
  reference hardware and a lossy network.

### 9c. The choice reaches the wire, in a browser

- The screen-offer journey now reads the live sender's parameters after sharing
  (2 000 000 bps) and after switching to the text preset (5 fps), and gains
  `capturePeers(page)` so it can read the connection it is asserting about. Journey time
  ~2.7 s.
- The screen-audio sender is capped at 64 kbit/s from the client as well, so a browser
  that can carry the sound is also told not to exceed what the server will forward.
- Both browser modes against the final artifact (`dist/brrpbrrp-e2e` sha256
  `340befd7b16ec0c427dc2b88202ccb4fc43f9a4254afcc3d988466799285b454`): default mode
  **25 passed**, private mode **39 passed**, 0 failed and 0 skipped in either.
- What this does not prove: the capture is a canvas stub, so the *resolution* the preset
  asks for is visible only in the constraints the stub received (asserted in the unit
  test), not in an encoded frame; and the ceiling is proven at the RTP boundary, not
  against a real encoder's output.

## Tranche 10 — ENG-05: the codec policy, and a second engine

**Source and dirty state before the tranche.** HEAD `2b1be12` (the contrast fix, committed
after ENG-03), working tree clean, application 0.4.0-alpha.8, schema 25, backup format 2.
The owner approved a VP8 fallback on 2 October, which is what made this row's second half
possible instead of a browser-matrix proof that would have needed codecs no engine here
ships.

### 10a. A screen that arrived in full and decoded to nothing

- **Failing behavior and reproduction.** With a Firefox project added (10b), the
  two-browser journey failed at the viewer's decode assertion: `framesDecoded: 0` for
  thirty seconds. The failure-time counters — instrumented around the assertion, then
  removed — were decisive: the publisher had encoded **86 frames / 162 252 bytes**, and
  the viewer had received **176 packets / exactly the same 162 252 bytes**. The stream was
  complete and undecodable: the server answered **H264** while both Firefox peers had
  listed **VP8 first**, so the publisher encoded H264 and the viewer's decoder produced no
  frame from any of it.
- **Root cause.** `screenTrackCodec` returned H264 whenever the offer mentioned it,
  ignoring the order the client wrote down. A screen is carried end to end without
  transcoding — the server rewrites sequence numbers, timestamps and payload types, never
  payloads — so the answer decides what the publisher encodes and what every viewer must
  decode. Overruling a client's own preference is a decision about somebody else's
  decoder.
- **Changed files and reason.** `internal/rtc/sfu.go` — `videoCodecsInOffer` (the offer's
  video codecs in the order it listed them), `screenTrackCodec` (the first of those this
  build carries; H264 only as a default when the offer names neither), and
  `screenPublicationCodec` (the codec a live publication is carried in), used for a
  viewer's output when that viewer offers it, so payloads and outputs cannot disagree.
  `internal/rtc/codec_test.go` — the VP8-only test still passes unchanged; the control is
  rewritten around the rule (a viewer that lists both gets the one it listed first, and the
  publication is carried the same way), and the parser test pins the order in both
  directions. `docs/adr/0019-media-architecture.md` gains "The codec a screen is carried
  in is the client's own choice", as the ENG-05 row requires.
- **Commands and results.** `go test ./internal/rtc -count=1` → ok (14.6 s); `-race` → ok
  (30.0 s, and three further runs clean); `go vet` clean; 311/311 frontend unit tests;
  `svelte-check` 0/0; `format:check` clean.
- **What it does not prove.** Nothing about a codec neither engine offers, or about
  quality: the presets bound the bitrate, and legibility at the text preset is WP-18's
  qualification on the reference hardware.

### 10b. A second engine runs the media journeys

- `web/playwright.config.mjs` gains `voice-firefox`: the whole voice spec under Firefox,
  with the release journeys as its dependency. **10/10 passed** — six release journeys and
  all four voice journeys, including the screen-offer journey's preset assertions and the
  two-browser journey's decode, stop, restart and late-join assertions.
- The engine was installed with the pinned Playwright (`npx playwright install firefox`,
  revision 1543). That command pruned the Chromium installs in the shared browser
  directory; they were reinstalled with the same pinned binary
  (`./node_modules/.bin/playwright install chromium`, revision 1243). Both engines are now
  required by the harness.
- **What it does not establish.** WebKit (not installed), a physical display, a lossy
  network, or behaviour under a slow link — the Firefox project runs on this host, in a
  headless engine, with the same generated capture the Chromium project uses.

### 10c. Diagnostics the harness keeps

- `capturePeers` prints a page's console errors and warnings and any uncaught page error.
  The trace did not carry Firefox's page console, and a browser's own complaint is the one
  diagnostic a screenshot cannot show. It is what turned "the picture never appeared" into
  a named cause in this tranche.

### 10d. One occurrence that is not yet explained

- A single `-race` run of `internal/rtc` failed at 60.5 s without a data race being
  reported; four subsequent runs (including three captured) passed, and the failure's own
  output was not kept. It is recorded here as an unexplained single occurrence rather than
  as fixed: the next failure keeps its log, and the tranche's tests all use polling
  budgets rather than fixed sleeps.

### 10e. The sign-in helper's budget, and one wrong fix kept in the record

- **What failed.** The Firefox project's journeys failed intermittently at `signIn`, on a
  form with empty fields and no error anywhere.
- **What the evidence said.** Instrumented, the sign-in POST returned **200 and set the
  session cookie**, and the page had already routed to a channel — the app was signed in.
  The helper's five-second budget was for the *workspace render* after that, and five
  seconds was enough on an idle machine but not for Firefox's third context in a run under
  load.
- **The first fix was wrong, and it is recorded because it is instructive.** A retry loop
  around fill/click/wait turned a 5-second failure into a 30-second one: once the sign-in
  succeeded the form was gone, so every retry re-filled an element that no longer existed
  and the loop could never succeed. It was replaced by what the evidence supports — submit
  once, then wait for the workspace with a 20-second budget — and the helper now says why
  a retry is not the answer.
- **A second cause was environmental.** Killing the shell that drove a batch of runs left
  its Playwright children alive, so several suites ran against their own servers at once
  and the failures looked like product regressions. Killing the test browsers by their own
  path (`brrpbrrp-playwright-browsers`, never the operator's own Firefox) is what clear
  looks like; a batch runner has to take the whole process group down.
- **Final verification against the artifact** (`dist/brrpbrrp-e2e` sha256
  `507c547904f98808e1a2dde33b1add85d1bfa6cd5da486d042dd6eef5d9ee2ea`), each in its own
  invocation with its own server: default mode **25 passed**, private mode **39 passed**,
  Firefox **10 passed** — 0 failed, 0 skipped anywhere. The browser-step audit now covers
  three steps and checks each one's positive control.

## Tranche 11 — Phase B: the interior rows

**Source and dirty state before the tranche.** HEAD `310b32f` (ENG-05 committed), working
tree clean, application 0.4.0-alpha.8, schema 25, backup format 2. Media first was
completed in tranches 8–10 (ENG-02, ENG-03, ENG-05); this tranche takes the rows the
owner's decisions have already fixed and the ones with no external dependency.

### 11a. ENG-07 — external TURN and IPv6, as a recorded limit

- **Decision.** The owner chose "document as a limit" on 2 October: no external TURN
  server in v1, and media is IPv4. The row's alternative — an operator-supplied relay and
  an IPv6-reachable host — would have needed media qualification on hardware this
  workspace does not have.
- **Changed files and reason.** `docs/deployment.md` gains "What this release's media does
  and does not do": the embedded relay (UDP/TCP/TLS) is the only one supported, IPv6 is
  not a media transport, the TURN and forced-relay admin settings are what an operator can
  use, and `voice path established … relayed: true` is the evidence that a call was
  relayed. `docs/production-readiness.md` records the same limit beside the host-
  qualification row it belongs to, so the row now says **which** networks must be measured
  rather than whether the feature exists. `docs/roadmap.md`'s boundaries and the README's
  scope paragraph state it for a reader who never opens an operator guide.
- **What it proves, and what it does not.** It records a decision and makes the limit
  discoverable from the surface a member or operator actually reads. It does not measure
  any network: which restrictive networks the embedded relay traverses is a G5 item and
  still needs hosts and devices.

### 11b. ENG-12 — a protocol handshake that protects drafts

- **What it is for.** A tab can be open across a server update, and the bundle in it is
  then older than the server. Most changes are additive and harmless, but a change in what
  a frame *means* is not something an old bundle survives quietly: it would send text the
  server stores differently, or hold text while every send is refused and nothing says why.
- **Changed files and reason.**
  - `internal/ws/protocol.go` — `Protocol` and `ProtocolMin`, and why the number moves: a
    change in meaning, not an added field.
  - `internal/api/websocket.go` — `hello` carries `protocol`, `protocol_min` and
    `version`, so a client learns the server's range on the frame that opens every
    connection and a person can be told which side is stale.
  - `web/src/lib/protocol.ts` — the bundle's `PROTOCOL` and `protocolVerdict()`: inside
    the range is compatible; below the minimum means update the app; above the server's
    number means update the server. An absent field is an older server — the field arrives
    with the first protocol-aware release — and is treated as compatible rather than
    turning a missing field into a broken installation.
  - `web/src/lib/workspace.svelte.ts` — `protocolNotice`, set from the hello frame, and a
    refusal in **both** send paths (`send` and `sendIntoThread`).
  - `web/src/lib/components/Composer.svelte` — the send control is disabled and the send
    handler returns *before* clearing anything, so the typed text, the attachments and the
    draft stay exactly where they were; the reload an update performs restores the draft
    through the machinery that already exists.
  - `web/src/lib/components/UpdateNotice.svelte` — an incompatible protocol shows a notice
    that names which side is stale and cannot be dismissed, with the same "Update now"
    action whose draft capture already exists.
- **Tests.** `internal/api/protocol_test.go` dials a real websocket and asserts the hello
  frame carries the protocol, the minimum, the version, and — as the control — the fields
  it already had. `web/tests/protocol.test.mjs` pins the four verdicts.
  `web/tests/workspace-state.test.mjs` drives the hello frame and asserts the refusal: no
  pending operation, the draft untouched, and the *same* call queues a message once a
  compatible hello arrives.
- **What the first attempt got wrong, and how the test found it.** The store had no guard
  — only the composer did — and the test *hung* rather than failed: `await value.send(…)`
  never resolved because the send was accepted and the harness's response never came. That
  is as close to a proof as the harness can produce that a component guard is not the
  contract; the store now refuses in the same state, so a reply panel, a retry button and
  the thread view are covered too.
- **What it does not prove.** No browser was run with an incompatible pair, because both
  halves come from one tree: the rendered notice is proven by `svelte-check`, by the
  store-level test and by the component's shape rather than by a journey. The protocol
  number is 1 on both sides, so the handshake is exercised end to end in its compatible
  direction and by unit tests in its incompatible one.

### 11c. An open intermittent: Firefox occasionally loses a session right after sign-in

- **What happens.** Roughly one Firefox invocation in three, a journey fails at `signIn`
  with `getByLabel('Your preferences')` never appearing. The failure dump added in this
  tranche names the state precisely: the page is on the sign-in form, and a probe from
  that page shows **`/api/state` returning 401** — the session it believes it has is not
  accepted. The sign-in POST itself returns **200 and sets the session cookie** (measured
  earlier in the same investigation).
- **What is disproved, with the measurements.** Sequential Firefox contexts render the
  workspace in 0.6–1.2 s; four *concurrent* contexts in 0.9–1.4 s; a second context signing
  in while the first is in a voice call in ~0.65 s. The login throttle is 400 per five
  minutes per address (the harness raises it), so it is not that; no account in those
  projects has a second factor; page console and page errors are silent on failure.
- **Two wrong turns that are worth keeping.** The first "fix" was a retry loop around the
  form, which could never succeed once the sign-in had worked (the form is gone) and turned
  a five-second failure into a thirty-second one; and an earlier batch runner that killed
  its own shell left Playwright children running concurrently, which made a real regression
  look like a suite-wide flake.
- **Where the next session should start.** The probe distinguishes "the server has no such
  session" from "the browser is not sending the cookie" if it is made to send the cookie
  explicitly (the cookie is `Secure` when the deployment is, `SameSite=Strict` always, and
  Firefox and Chromium treat `http://127.0.0.1` differently for `Secure` cookies — the
  first hypothesis to test). Until then the Firefox project is engine coverage that is
  intermittently red on this host, not a claim that it is stable; the Chromium modes have
  not shown it, and the media assertions themselves have never been the failing step.

### 11f. ENG-13 — a storage quota, a disk guard, and limits stated once

- **What it is for.** Two failures this closes. A full volume took the *server* down: the
  upload path wrote until the filesystem said no, and the refusal was a 500 that named
  nothing an operator could act on — while an installation that fills its disk can no
  longer write the backup that would recover it. And the product's own bounds were
  literals: the account ceiling `100` appeared in `internal/api/admin.go`,
  `internal/api/invitations.go` and `internal/db/accounts.go`, the channel ceiling twice in
  one handler, and the pin ceiling inside two SQL strings. A check and a message that read
  different numbers is how an installation comes to advertise one bound and apply another.
- **Changed files and reason.** `internal/limits/limits.go` is new: `Accounts`,
  `Channels`, `OutstandingInvitations`, `PinsPerChannel`, each with why the number belongs
  to the release rather than to configuration. Every check and every message now reads the
  constant — the two channel refusals, the account refusal in two packages, the invitation
  ceiling, and the two pin statements (interpolated, because they are SQL). `internal/app`
  gains `FreeSpace` with a `Statfs`/`Bavail` implementation on unix and
  `GetDiskFreeSpaceEx` on Windows (both cross-compiled in this tranche), and the config
  gains `BRRP_STORAGE_QUOTA_BYTES` and `BRRP_STORAGE_RESERVE_BYTES` (512 MiB default).
  `internal/api/uploads.go` gains `diskGuard` — asked *before* the write, sized for the
  largest upload the installation accepts, so the reserve is a floor for recovery work
  rather than a budget spent to the byte — and `quotaRefusal`, asked *after* the bytes are
  counted and before any row promises them, so a refusal is one removal away from leaving
  nothing behind. A write that finds the volume full anyway is answered 507 with its own
  message instead of a server fault. `docs/deployment.md` documents both settings.
- **Tests.** Four: an upload past the quota is refused with the bytes used and the limit
  in the message, and the *control* is the file that fits — with a check that nothing was
  written either time; an upload inside the reserve is refused and the same upload is
  accepted once there is room (with the free-space call injected, because a refusal proven
  by filling a real volume is a test nobody can run); an unreadable quota refuses rather
  than guessing; and the channel ceiling is walked through the route to the boundary, with
  the refusal naming the constant and the last allowed channel created beside it.
- **Commands and results.** `go test ./internal/api -count=1` → ok (192 s);
  `./internal/db` → ok; `./internal/app` → ok; `go vet ./...` clean; the app package
  cross-compiles for darwin, linux and windows; default browser mode **26 passed** against
  the rebuilt binary.
- **What it does not prove.** No real full disk was induced, so the ENOSPC mapping is code
  read rather than exercised; the reserve's arithmetic is asserted with an injected
  free-space call; and the quota's `SUM(file_size)` is a scan per upload, which is fine at
  this product's upload rate (ten per minute per account) and is recorded here as the
  reason it has not been turned into a counter.

### 11e. Where the next session starts

Phase B rows closed so far: **ENG-02** (keyframe feedback and late-join discovery,
tranche 8), **ENG-03** (screen presets and the ingress ceiling, tranche 9), **ENG-05**
(the codec policy and the Firefox project, tranche 10), **ENG-07** (the documented media
limit), **ENG-11** (the reconnect policy), **ENG-12** (the protocol handshake) and **ENG-13**
(the storage quota, the disk guard and the single-sourced limits).

Still open, in the order they are worth taking:

1. **ENG-08** — emphasis and block quotes in `message-tokens.ts`, lists deliberately out
   by the owner's decision; the malicious-markup tests must stay inert.
2. **ENG-09** and **ENG-10** — a member profile surface reachable by keyboard, and
   Back/Forward between conversations (today only `replaceState`).
3. **ENG-14** — bounded, redacted metrics behind operator authority, plus the external
   HTTPS/WebSocket probe script; nothing user-supplied may reach a label.
4. **ENG-15** — the encrypted off-host backup procedure, with key custody and key-loss
   handling rehearsed locally into a second directory.
5. **ENG-04** and **ENG-06** — viewer controls (fullscreen/pin, audio-only, phone layout)
   and output-device selection plus the resume-token rejoin measurement.

Then Phase C: the generated outsider matrix, the acceptance matrix file, the rendered
proofs still owed, Firefox/WebKit projects for the core journeys, the sharded race job,
convergence passes, the first hosted CI run, flipping the feature gates with the owner's
approval, and cutting `1.0.0-beta.1`.

### 11d. ENG-11 — a reconnect policy that meets the budget

- **What it was.** The socket's backoff doubled to a thirty-second ceiling, and nothing
  else was watching: no `online` trigger, no foreground resume, and no way to notice a
  socket that had gone *half-open* — a path that died without a close event, which is the
  common case on a flaky link. The plan's budget is ten seconds at p95 after connectivity
  returns, and a schedule that has already climbed cannot meet it.
- **Changed files and reason.** `web/src/lib/reconnect.ts` — the policy as data:
  `reconnectDelayMs` (one second doubling to thirty, jitter added by the caller so two
  tabs do not arrive together) and `SILENT_CONNECTION_MS` (75 s — two and a half of the
  server's thirty-second `presence.update` frames, so one lost frame is not a
  reconnection). `web/src/lib/workspace.svelte.ts` — one shared `attemptReconnect`, a
  `retryNow` that cancels the pending backoff and resets the attempt count when the
  browser reports connectivity returning (`online`, visibility, focus), and a half-open
  check every ten seconds that *closes* a silent socket so the ordinary close path runs
  rather than a second reconnection mechanism existing. The listeners are attached once
  per session and removed in `stop()`, because a listener left behind by a signed-out
  session would reconnect as the wrong account. A hidden page is skipped: browsers
  throttle timers and suspend sockets in the background, and reconnecting there is a guess
  about a connection nobody is using.
- **Tests.** `web/tests/reconnect.test.mjs` pins the schedule and the silence window.
  `web/tests/workspace-state.test.mjs` gained a drivable `WebSocket` and window listeners
  in its harness and three tests: connectivity returning opens a socket *immediately*
  (asserted by a socket appearing inside a quarter of a second, well inside the one-second
  backoff the close scheduled), a healthy connection is left alone, and a socket silent
  past the window is closed while one inside it is not. `release.spec.mjs` gained "a
  severed connection reconnects on its own within the budget": the test severs the page's
  first socket with `routeWebSocket` and proxies every later one to the real server,
  asserts the workspace is connected within **10 s** (the plan's budget, not a comfortable
  round number), that at least two connection attempts happened, and that a message sent
  afterwards is stored rather than merely rendered.
- **Commands and results.** 321/321 frontend tests; `svelte-check` 0/0; the new journey
  passes in 2.2 s in the release project. Against the rebuilt artifact
  (`dist/brrpbrrp-e2e`): default mode **26 passed** (25 + the new journey), private mode
  **40 passed**, Firefox **11 passed** — the Firefox invocation failed once first with the
  open sign-in intermittent recorded in 11c, and passed on the rerun.
- **What it does not prove.** The half-open detector is proven with an injected clock, not
  by a real dead path; the `online` trigger is proven in the store harness and by the
  browser's own event, not by unplugging a cable; and no measurement over a real network
  exists — the p95 the plan states needs the reference hardware and a lossy link.



