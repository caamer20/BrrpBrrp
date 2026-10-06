# v1.0.0 execution ledger — 2026-10-01

Compact record of implementation tranches toward v1.0.0, per the review-17 handoff
(`planning/handoff-agentic-coder-v1-review-17-2026-10-01.md`). One section per tranche.

**Current status (6 October 2026).** Read this first; the per-tranche sections
below are evidence, some of them written when assignments were hazy, and two of them
(11e and 11d) are at the tail and historical. This index is refreshed at every tranche.

- **Branch head after tranche 22** (`e0c6280`); **W-01 through W-06 and W-22 are closed.**
  The two independent-review product P1s — the quota race (F-01) and the thread deep-link
  defect (F-02) — are fixed **and** carry regressions that fail without the fix (the quota
  one proven against a pre-fix overlay; the deep-link one a rendered journey). `make check`,
  `go test ./...`, both Chromium feature modes with nothing excluded, `thread-scale`, and
  the full-disk rehearsal were green at `1faeb92`. From tranche 23 onward the last verified
  state of the tree is that commit.
- **From tranche 23 the owner suspended test runs.** No browser suite, Go test, frontend
  unit test or rehearsal is run in these tranches. The static gates are run after every item
  — `make check`, which builds the frontend, vets, formats, type-checks and runs the
  documentation, packaging and workflow audits — and every behavioural change carries tests
  that have **not** been executed. `planning/verification-queue.md` is the owner's single-pass
  checklist for them, and it opens with the order the pass should take. **An entry that
  quotes a figure says whether it was measured before the suspension**, which is true of
  tranches 21 and 22 and of nothing later.
- **Candidate:** 0.4.0-alpha.9 at `f810f0c` (digests in `dist/releases/SHA256SUMS`,
  record at `docs/releases/0.4.0-alpha.9.md`). That archive predates several fixes; any
  further packaging is a new version (`0.4.0-alpha.10`). **Do not build on it as if it
  were current.**
- **What is closed, measured in the tree on this branch:** all fifteen Phase B rows
  (ENG-01...ENG-15); the 1000-reply thread journey, the Arabic journey, the refusal
  cleanup for a failed publication, the shared rehearsal kit, the acceptance and
  negative matrices with their CI gates; the Firefox voice project (13/14 green) and
  WebKit release subset (a red starting point over the Chromium fixture, see below);
  tranche 16's W-01 items 1 through 8; tranche 17's W-02 (F-10, member export limits
  enforced as the document is built); tranche 18's W-03 (F-11, one recovery-health model
  shared by `production-check` and alerts); tranche 19's W-04 (F-12, metric names that say
  what they measure); tranche 20's W-22 (the status index, a garbled table label and the
  alpha.9 record's list of open decisions).
- **What is open and the owner owns it:** a private remote (settled: none), the two
  feature gates (settled: only after the independent reviews and their P1s land), the
  host/device/reviewer/profile requirements of the D–I phases (settled), a Windows
  code-signing identity, and the long lead items listed in section 5 of the top-level
  kickoff. The rest of this ledger's tail is historical; the owed list that follows it
  accurately is the one the acceptance checker prints on any `make check`.
- **Red or owed on this branch, as last measured:** Firefox's voice project and WebKit's
  release subset were both red at `1faeb92` — the `playsInline` assertion (W-05) and the
  fixture the WebKit pass shared with the Chromium `release` project (W-06). Both are
  addressed in tranches 21 and 22, which the verification pass must confirm; nothing is
  known red meanwhile, and nothing after `1faeb92` has been run. **W-07 onward and W-23
  onward are open**, and the verification-only items listed at the end of
  `planning/verification-queue.md` remain owed exactly as previously specified.

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

### 11g. ENG-08 — emphasis and block quotes, and still no markup

- **What the row bounds.** The owner decided on 2 October that formatting is emphasis and
  block quotes only: lists are out of v1, and the tokenizer's contract is unchanged — no
  piece of a message ever becomes markup. The renderer chooses an element from a closed set
  of token kinds; the message contributes text.
- **Changed files and reason.** `web/src/lib/message-tokens.ts` gains two token kinds —
  `emphasis` (bold, italic, strike) and `quote`, both carrying their own tokens — and the
  parsing for them: markers are read *per line* so an unclosed `**` cannot restyle the rest
  of a message, `_` inside a word is left alone (`snake_case_name`) and `*` between digits
  is arithmetic, an unmatched marker stays literal, and consecutive `> ` lines become one
  quote whose content is still tokenized (a mention in a quote is a mention). Emphasis
  nests to a depth of three, which is a bound on the renderer's work rather than on the
  person. `web/src/lib/components/MessageTokens.svelte` is new: the recursive renderer,
  moved out of `MessageItem.svelte`'s inline template (which could only ever render one
  level) and now used by it. Code is untouched by all of this: a fenced block is still
  literal, and markers inside one are text.
- **One bug the tests found, and it is the kind this file's history is made of.** The first
  parser read the markers in their own order — `**`, `__`, `~~`, `*`, `_` — so in
  `a **bold** and *italic* and ~~struck~~` it found the strike pair first, recursed past it,
  and lost the italic entirely. The rule is that the *earliest* pair wins whatever its
  marker, and the test that caught it is the one that lists three styles in one line.
- **Tests.** Seven tokenizer tests, each with its control: the three styles become tokens
  with their content inside and the markers are consumed; `snake_case_name`, `2*3` and
  `file_name.py` stay text; an unmatched marker stays literal and emphasis never crosses a
  line; a fence keeps markers literal and emphasis around code leaves the code a token;
  mentions and links keep their own tokens inside emphasis; consecutive quoted lines are one
  quote, two lines separated by a blank line are two, and a mention inside a quote is still a
  mention; and malicious markup stays inert — `**<b>not a tag</b>**` produces a bold token
  whose only child is text, and the set of kinds in the result is exactly
  `emphasis, text`. The rendered proof is in `release.spec.mjs`: the browser journey posts
  `**bold text** and *italic text*`, a two-line quote, and `**<b>not a tag</b>**`, and
  asserts the `strong`/`em`/`blockquote` elements exist with the right text while no `b`
  element appears anywhere in that message's body.
- **Commands and results.** 328/328 frontend tests; `svelte-check` 0/0; formatting clean;
  the release project green (7 journeys); default mode **26 passed**, private mode
  **40 passed** against the rebuilt binary.
- **What it does not prove.** It does not render an Arabic or right-to-left emphasis (that
  is a Phase C proof owed), does not cover lists (out of v1 by decision), and says nothing
  about how a screen reader announces nested emphasis — the accessibility sweep checks
  violations, not comprehension.

### 11h. ENG-09 — a member's profile, from the list and from a message

- **Changed files.** `web/src/lib/components/MemberProfile.svelte` is new: a dialog showing
  the identity the workspace already publishes — avatar, display name, `@handle`, role
  standing (Owner / Administrator / Member, with "that's you" on the reader's own), live
  availability when presence is enabled, the account's creation date, and a note when the
  account is deactivated. Nothing private is on it: no conversation state, no unread count,
  no moderation history, no timeout — a profile is the member list's own information laid
  out legibly. `workspace.profileUser` is store state, because two different surfaces open
  the same dialog about the same person. `Workspace.svelte` renders it and its member rows
  are now `<button>`s (a list of people is not a mouse-only surface); `MessageItem.svelte`
  makes the author's name and picture open the same dialog; `app.css` carries the button
  resets so both still look like the text and avatars they replaced.
- **Tests.** The a11y journey in `release.spec.mjs` now focuses the first member row **by
  keyboard** (`focus()` then `Enter`) and asserts the dialog opens, carries the handle and
  "Member since", passes the accessibility sweep, closes on `Escape`, and **returns focus to
  the row it came from**; then opens the same dialog from a message author and asserts it
  identifies the reader's own profile. That focus-return assertion is the one this file's
  history keeps paying for: a dialog that closes into nowhere is a keyboard user stranded.
- **One self-inflicted detour, recorded because it cost a run.** The first execution of the
  new assertions failed waiting for a button that did not exist in the served page: the
  frontend had been rebuilt but the Go binary that embeds it had not, so the harness served
  the previous bundle. `make browser-check` runs `build` (which builds the web bundle) and
  then builds `brrpbrrp-e2e`, so the ordering is right there; the trap is only in ad-hoc
  runs. Both artifacts are now rebuilt together in the commands below.
- **Commands and results.** `svelte-check` 0/0; release project green (7 journeys) after the
  binary rebuild; default mode **26 passed**, private mode **40 passed**.
- **What it does not prove.** It does not prove a screen reader announces the dialog well
  (that is the human accessibility review, M6), and it says nothing about a member who is
  banned or deactivated beyond the note the record already carries.

### 11i. ENG-10 — Back and Forward, and a revoked conversation is not shown

- **The distinction the row needs.** Until now every selection wrote the address bar with
  `replaceState`, so Back left the app: the browser's entry list never held a conversation.
  The rule implemented is between a *deliberate* selection — a click in the channel list, a
  direct conversation, a result in the switcher, a notification entry — which pushes an
  entry Back should return through, and every other transition — the initial load, an
  authority re-read moving the account out of a channel, a reply that arrives late — which
  replaces the current entry. Back must not walk through screens nobody chose.
- **Changed files.** `workspace.svelte.ts`: `selectChannel(id, push)` (never pushing the URL
  already on screen, so re-opening the conversation you are in — an empty one, where the
  refetch is still wanted — cannot leave a Back step that appears to do nothing); a
  `routeReadable` test that replaces the same three-line check written out at three call
  sites; `openHistoryDestination`, which reads the destination out of the URL on `popstate`
  and opens it exactly the way any other route is opened; a `historyPush` wrapper that falls
  back to `replaceState` if the browser refuses; and bind/unbind of the `popstate` listener
  beside the connectivity watchers, because a listener left behind by a signed-out session
  would navigate as the wrong account. The call sites that push are the channel list, the
  direct and group lists, the switcher, the messages inbox and an activity entry's
  destination.
- **A route is a destination, not a grant.** `openHistoryDestination` re-runs the same rule
  the initial load does: an id the account may no longer open is not selected. When the
  account has nothing readable at all to fall back to, the address bar is reset to the
  workspace root rather than left naming a conversation the account cannot open.
- **Tests, unit.** Two in `workspace-state.test.mjs` against a history the shell can walk
  (`pushState`, `replaceState`, `historyBack`, `historyForward` firing a real `popstate`):
  Back returns to the conversation you came from, and a channel missing from the next
  snapshot — revoked while the member was away — is not opened by Forward, with the
  member's own message history never showing that channel's content; and a selection that
  changes nothing writes no second entry. The shell's entry list is shared by the file, so
  a `resetHistory` helper clears it between tests: a browser gives each page its own.
- **Tests, browser.** A new journey in `release.spec.mjs` (both modes) creates a room of its
  own with one line in it and walks it: general → room → Back (general's own line is there)
  → Forward (the room's line is there). Then the member's role loses `channel.read` and the
  line disappears from the open view; Back names general, also revoked, and Forward names
  the room — neither is shown, and the address bar does not end up naming the room. When the
  privilege is restored the room is listed again but the history step taken while it was
  refused does not reopen it: only a deliberate click does. The journey deletes its room at
  the end.
- **Commands and results.** 330/330 frontend unit tests; `svelte-check` 0/0; release project
  green with the new journey; default mode **27 passed**, private mode **41 passed**.
- **What it does not prove.** It does not prove Back/Forward across a *reload* (the entry
  list survives, the in-memory view does not — that is the ordinary cold-load path, covered
  by the permalink and initial-route journeys), and it says nothing about Back from a voice
  channel: joining voice is not a conversation selection and writes no entry.

### 11j. ENG-04 — viewer controls, and the phone layout a share is watched on

- **What a viewer gets.** `ScreenStage.svelte` replaces the bare `<video>` the workspace
  used to place: the picture, a placeholder that says the picture is not being decoded, and
  three controls — **Audio only**, **Enlarge**, **Fullscreen**. All three are real buttons
  with `aria-pressed`, so the state is announced, and the stage is a labelled region.
- **Audio-only is a data choice, not a display trick.** Hiding the element would keep the
  browser receiving and decoding a picture nobody asked to see, which is the cost the
  choice exists to avoid — so `applyScreenAudioOnly` disables the inbound *video tracks*
  and leaves the audio alone (the desktop's sound is what the viewer chose to keep). The
  element stays mounted in both states and the control that restores the picture stays
  reachable: an audio-only view must never strand the person in it. The store applies the
  preference on the attach path, so a share that starts while audio-only is on arrives
  already silenced rather than playing one frame first. Fullscreen is an explicit request to
  see the picture, so it clears audio-only rather than filling the screen with black.
- **A real layout bug the phone assertion found.** The stage was rendered inside the sidebar,
  which on a phone is a drawer — so a viewer with their channel list closed would have had
  no picture and no controls. It is placed at the workspace root now, outside every drawer;
  it is fixed-positioned, so where it sits in the tree is about who can see it, not layout.
- **Two contrast violations the phone sweep found, and fixed.** My own pressed-state chip
  measured 3.27:1 (`#0d1117` on the accent green) and now uses the theme's own
  `--accent-text`. The other two were pre-existing and only visible on a pale selected row
  in the light theme: `--muted`/`--faint` measured 4.37:1 against `#dfead9` — just under the
  threshold, on the two rows a person reads first. The light theme's muted tone is now
  `#566272` (4.9:1 there, 6.1:1 on white). The accessibility helper now reports the measured
  ratio and both colours with each violation, because a contrast fix depends on the numbers.
- **Tests.** `screen-receive.test.mjs`: audio-only disables video tracks and leaves audio
  tracks enabled, restores them, and tolerates a missing stream; the stage keeps the control
  that turns the picture back on. `voice-state.test.mjs`: a share arriving while audio-only
  is on is born silenced, and turning the picture back on re-enables it — the shell now
  compiles the real `screen-receive.ts` rather than standing in for it, and its stream
  fixtures grew the `getVideoTracks` a stream owes. The two-browser journey asserts, on the
  viewer's side, that the element is muted and inline, that Audio only disables the inbound
  track while the picture control remains reachable, that the picture resumes decoding
  afterwards, that Enlarge and Fullscreen take effect where they live (`document.fullscreenElement`,
  the element's own class), and that at 390×844 the stage and its controls are visible and
  axe-clean.
- **One source-text test replaced rather than re-pinned.** The old assertion that
  `Workspace.svelte` contains `class="shared-screen"` moved to the component and gained the
  behavioural assertions above; a test that greps a template for `muted` is not what proves
  a viewer's picture is muted.
- **Commands and results.** 332/332 frontend unit tests; `svelte-check` 0/0; formatting
  clean; voice project 9 passed; default mode **27 passed**, private mode **41 passed**.
- **What it does not prove.** Fullscreen was asserted in headless Chromium only, so the
  browser matrix (Firefox, Safari) for these controls is still owed; and the phone layout
  was measured at one viewport (390×844), not across the range of real devices.

### 11k. ENG-06 — the sound's destination, and the rejoin budget measured

- **Output selection, where the browser has it.** `devices.ts` gains
  `supportsOutputSelection` and `listOutputs` (mirroring the input listing, including the
  nameless `default` entry Chrome reports before permission, which is named "System
  default"); `voice.svelte.ts` gains `outputDevice` and `setOutputDevice`, applied to every
  audio element the call owns **and to each one as it arrives** — a participant who joins
  after the choice must not come out of a different speaker than the others. A device id
  stored on one machine and carried to another names nothing: the sink call fails, the
  choice is forgotten and said out loud. Firefox and Safari have no `setSinkId`, and the
  panel says so in a sentence rather than showing a list that appears to work.
- **The recovery path, stated.** Two transports fail independently. A lost *signaling*
  connection is not a lost call: media is a peer connection and keeps flowing, the server
  holds the session for 28 s, and the client reinstates it with the `voice.resume` token.
  A *failed media transport* is torn down and rejoined (4 s of tolerance, then
  `voice.leave` + `voice.join` under ordinary admission, with a 20 s guard and no second
  retry in the same call). **ICE restart is not used**, and §13 of ADR 0019 now says why: a
  rejoin reuses admission, capacity and the publication model unchanged.
- **The measurement, and one wrong turn worth recording.** The first journey I wrote
  injected the outage with Playwright's `setOffline` — and the client never noticed,
  because that emulation covers the HTTP stack and leaves WebRTC alone. The call kept
  playing, `Connected` stayed on screen, and the assertion failed after 30 s: the fault
  injection was wrong, not the product. Rewritten to sever the socket itself (the harness
  holds the listener's connection open and closes it), the journey asserts the correct
  shape: audio on the far side keeps growing across the severance (the call survives),
  `voice.resumed` arrives **4 027 ms** after severance, the app never says "Join again",
  the listener is still in the room and audio flows again afterwards. Asserted at 15 s in
  the journey, so a regression that waits out a backoff fails there; the number is printed
  by the run so the ledger can quote it.
- **What is owed, explicitly.** The *media-failure* rejoin is implemented and staged but
  **not measured** in a browser: the harness cannot fail a peer honestly (`setOffline`
  leaves WebRTC alone; `close()` reports `closed`, which the recovery does not listen for).
  Measuring it needs a fault-injection hook or a real network change on a physical host —
  a Phase D item, recorded rather than claimed.
- **Tests.** `devices.test.mjs`: the speakers are listed with the default named, a browser
  without output selection says so in words, and a listing failure is never described as a
  microphone problem. `voice-state.test.mjs`: the chosen speaker reaches the elements the
  call already owns and the one a later participant arrives on (driven through a real
  `createPeer` + `ontrack`, not by reaching into the store), and a speaker that is gone is
  forgotten and said out loud. The browser journey asserts the Speaker control exists in
  Chromium's preferences, that choosing from it produces no error, and is axe-clean.
- **Commands and results.** 336/336 frontend unit tests; `svelte-check` 0/0; voice project
  green; default mode **28 passed**, private mode **42 passed**.
- **What it does not prove.** Firefox and Safari are untested for output selection (they
  have no such API, and the panel's sentence is what runs there); the resume budget was
  measured on localhost, where the socket comes back in milliseconds — a hostile network is
  Phase D; and the media-failure path is unmeasured, as above.

### 11l. ENG-14 — what an operator can see, and a probe that asks from outside

- **`internal/metrics`** is a dependency-free recorder: per route *pattern*, the request
  count, status classes and a ten-bucket latency histogram with the p95 reported as the
  bucket it falls in (named `p95_bucket_ms`, because a percentile needs every sample and a
  bucket costs one counter), plus the process's resident memory and CPU time read from the
  operating system rather than inferred from the Go heap — the gap between the two is where
  a leak or a page-cache-heavy upload lives, and the media subsystem runs in this process,
  so there is no separate media worker to measure. Platform files follow the existing
  `space_unix.go`/`space_windows.go` convention; Windows reports `reported: false` rather
  than inventing zeroes, and Darwin's `Maxrss` is bytes where Linux's is kilobytes.
- **The label is the pattern, never the path.** Routes are registered in a table, so the
  wrapper is applied at registration and the label is `GET /api/channels/{id}/messages`. A
  per-id label is how one busy channel turns a metrics map into a memory leak, and the
  endpoint's own test walks every label looking for identifier-shaped segments.
- **`GET /api/admin/metrics`** (SettingsManage) reports routes, process, runtime, media,
  connections, storage and jobs. The media section carries the SFU's own counters — packets
  and bytes forwarded per destination, and packets dropped at the authorization boundary —
  which the forwarding path increments with one atomic add. Nothing in the report is
  message content, a member name or a conversation identifier, and it deliberately does not
  count messages: a scrape that runs a table count every fifteen seconds costs more than it
  reports.
- **The wrapper has to let a websocket through.** The measuring middleware records the
  status and hijacks for the upgrade; a `ResponseWriter` wrapper that also intercepts the
  body is a wrapper that breaks the one route which stops being an HTTP request at all.
- **`scripts/probe.mjs`** asks the deployment from outside: setup status, the shell, an
  asset the shell names, the security headers, the session and the metrics endpoint when a
  cookie is supplied, and the websocket handshake. It tells apart the three ways a
  websocket fails — refused cleanly (a proxy doing its job on an unauthenticated request),
  upgraded but silent (the application not speaking), and never answered (a proxy eating
  the upgrade) — because an operator fixes them in different places. `make probe BASE=…`
  runs it; `docs/deployment.md` says when to schedule it.
- **Tests.** `internal/metrics`: the label is the caller's (so the caller passes the
  pattern), the histogram holds every request exactly once and the p95 bucket is one of the
  reported buckets, an in-flight request is visible while it runs, two reads serialize
  identically, and 400 concurrent requests across two routes record exactly 400 — under
  `-race`. Process usage is asserted to be real or explicitly unavailable. `internal/api`:
  unauthenticated is 401, an ordinary member is 403, the operator's report has every
  section, the metrics route counts itself, and the counters move — including for a
  *refused* request, because an endpoint that answers 401 for everybody is a fact an
  operator needs. `web/tests/probe.test.mjs` runs the probe against real local servers:
  a healthy one passes every check and completes a websocket conversation, and four
  seeded faults (swallowed upgrade, silent upgrade, stale asset, no asset link) each fail
  the check that names them.
- **Commands and results.** 340/340 frontend unit tests; `svelte-check` 0/0; formatting
  clean; `go vet` and `gofmt` clean; `go test ./... -count=1` green across 13 packages;
  default mode **28 passed**, private mode **42 passed**.
- **What it does not prove.** The probe was exercised against local servers, not against a
  real reverse proxy on a hostname with a certificate (Phase D); the latency histogram is
  in-process, so it says nothing about a proxy's own latency; and there is no long-term
  storage of these numbers — the endpoint answers "is it healthy now", and trending needs a
  scraper the operator supplies.

### 11m. ENG-15 — the encrypted copy that is not on the server, and a rehearsal that runs the documented steps

- **What the row asks for.** An encrypted off-host backup procedure using established
  operator tooling, with key custody and key-loss handling written down, and a *second
  directory that restores from the encrypted copy using only the documented steps*.
- **The procedure.** `docs/deployment.md` now has a section that names three tools in
  order: **age** first, because the server only ever needs a public recipient key and the
  identity never leaves the operator's machine; **gpg** next, driven through a keyring of
  its own; and **openssl** last, because `enc -aes-256-cbc -pbkdf2 -iter 600000` is on
  every host but has **no authenticated mode** — the trade-off is stated, and the archive's
  own manifest (which `verify-backup` checks) is what catches a corrupted or wrong-key
  ciphertext afterwards. The document is explicit that the encrypted copy is the copy: the
  plaintext staging file is deleted in the same step that encrypts it, or the procedure is
  theatre.
- **Key custody and key loss.** The private key never lives on the server — the server holds
  only a public recipient. Two copies of the key, in two places that do not fail together,
  with the holder recorded in the recovery plan. If the key is lost the encrypted copies are
  unrecoverable, with no backdoor, by design: the page says what remains (the most recent
  on-host archive and anything still inside the retention window), and treats a lost key as
  an incident with a rotation step.
- **`scripts/backup-encrypted-rehearsal.mjs`** runs exactly those steps against a real
  installation: install, write a message and an attachment through the API, back up and
  verify, copy the archive to a second directory, encrypt it there with the documented
  command, **delete the plaintext**, then decrypt into a fresh destination and restore from
  that — proving the decrypted archive is byte-identical to the original, the restored
  database verifies, and the message and attachment bytes survived. It generates its key
  material rather than writing a made-up recipient, since a rehearsal that would fail on a
  machine that actually has `age` proves the opposite of what it claims, and it records in
  its report which tool it exercised. On this machine that is `openssl`; `make
  rehearsal-encrypted` runs it.
- **The scaffolding is now shared.** Three rehearsals had grown their own copies of the
  server lifecycle, the teardown backstop and the API client — including the teardown fix
  that stopped ten orphaned servers from a previous session. They now import
  `scripts/rehearsal-kit.mjs`; migrating them found three real drifts that the copies had
  hidden: the private rehearsal's server environment (the private gate and the raised
  sign-in throttle) would have been silently dropped by the kit's defaults, the rollback
  rehearsal's `api` returned the body as text where the kit returns bytes, and its
  expected-failure path needed the kit's `serverEnv`. Each is fixed where it belongs, and
  all three rehearsals were re-run to prove the migration rather than to assume it.
- **Commands and results.** Encrypted rehearsal **18 checks, 0 failed** (openssl path);
  community rehearsal **23 checks, 0 failed**; private rehearsal **172 checks, 0 failed**;
  rollback rehearsal **57 checks, 0 failed** (0.4.0-alpha.7 → alpha.8, schema 21 → 25);
  frontend unit tests and `svelte-check` clean. `planning/m5-checkpoint/encrypted-backup-rehearsal.json`
  is the report.
- **What it does not prove, and the row cannot yet claim.** Two directories and one machine
  are still not off-host recovery: a *separate host* (or object storage) holding the
  encrypted copy, with retrieval over the network, is Phase D and needs the owner's
  infrastructure. The `age` and `gpg` paths are implemented in the rehearsal but were
  exercised here only through `openssl`, because neither tool is installed on this machine —
  the report records which one ran, and a host with either will exercise that path.

### 11n. Phase C — the matrix that fails the build, and two rendered proofs it was owed

- **The acceptance matrix is now machine-checked.** `planning/acceptance-matrix.json` maps
  every one of the plan's twenty-nine §11.2 scenarios to the checks that prove it, and
  `scripts/check-acceptance-matrix.mjs` reads the catalog out of the plan and fails when a
  scenario is missing from the matrix, when a named check no longer exists (a Go test is
  looked up where it lives, a journey title in its spec, a unit test in `web/tests`, a script
  on disk), when a scenario names neither a check nor an owed item, or when a matrix id the
  plan does not declare appears. `make check` and the CI `test` job both run it, and
  `web/tests/acceptance-matrix.test.mjs` proves it can fail — eight tests over small trees of
  its own, including the two silent defects it exists to catch: an id pattern that skipped
  `T-A11Y-01` because it carries digits, and a reference reader that kept the second colon of
  `kind::name` and reported every check as missing.
  Today it reads **132 named checks across 29 scenarios, with 14 owed items in 14 scenarios**
  — every owed item naming what is missing and which phase owns it. That list is the honest
  answer to "what is left", and it is printed by every run rather than buried here.
- **The right-to-left journey, and the bug it found.** `release.spec.mjs` now posts an Arabic
  message that begins in Arabic and ends with an English URL, an Arabic display name, and
  searches for Arabic text. It failed on its first run, and the reason was not the test: the
  direction rule was written as `direction: auto` **which is not a CSS value at all**, so
  every declaration was dropped and every Arabic message had rendered left-to-right since it
  was added. The rule is `unicode-bidi: plaintext` plus `dir="auto"` on the message content,
  the composer and the author's name; the journey asserts the *resolved* direction
  (`getComputedStyle(node).direction === 'rtl'`), the rendered text with its line break, and
  that search finds the message by its own script.
- **The thousand-reply thread, walked in a browser.** `thread-scale.spec.mjs` (gated behind
  `BRRP_E2E_SCALE=1`, like the Firefox pass, because it seeds a thousand messages and takes
  about two minutes) creates its own channel, seeds 1,000 replies through the API from
  twenty-five accounts paced to the product's own flood control — sixty a minute and ten in
  five seconds, per account — and then opens the thread, walks back to its beginning, and
  asserts what the plan asks for: every reply appears **once inside a window**, the window
  never grows past its bound however many pages are loaded, and the **oldest** reply is
  reachable and is the words that were written. Three of its assertions were wrong when they
  were first written and each failure taught something the unit tests could not: the root
  message is not inside the panel (it is the channel's), the panel's dialog is titled
  "Thread" for every thread, and the reply list is deliberately bounded, so "content appears
  once" means once per window rather than once in a growing list.
- **Three harness defects found by the runs themselves, all in the release journeys and none
  of them in the product.** The phone-width journey held a POST with `page.route` and
  continued reads from the same handler; it failed twice in twenty runs with
  `route.continue: Route is already handled!` while the accessibility sweep and the send were
  racing. Reads now `fallback()` — passing the request on rather than claiming it — and the
  contended `continue()` is guarded. The data-export journey waited five seconds for the
  "your acknowledgement was lost" alert, but its own route handler fetches the real response
  before aborting it, so the alert cannot appear until that chain finishes; the wait is now
  twenty seconds and says in a comment that it is not a latency claim. And the Arabic
  journey read `getComputedStyle` from the element it had captured a moment earlier — the
  optimistic copy, which the server's reply then replaces. A detached node computes no CSS at
  all, which is `direction: ltr`, so the assertion failed three times in twenty runs for a
  reason that had nothing to do with Arabic. It waits for delivery to settle and polls the
  resolved direction now. Six consecutive runs of the two tightened journeys pass.
- **One flake found in my own new assertion, and fixed where it belonged.** The member-profile
  journey's `expect(memberRow).toBeFocused()` failed twice in twenty runs: the member list's
  presence groups were not keyed, so a presence update replaced the row a keyboard user was
  standing on (now keyed), and `toBeFocused` also asserts that the *page* has focus, which a
  headless page behind another context does not (now `bringToFront()` first). Twelve
  consecutive runs of that journey pass; the twenty-run loop is the proof that matters and is
  recorded below.
- **Commands and results.** Acceptance matrix green (132 checks, 14 owed). 348/348 frontend
  unit tests; `svelte-check` 0/0; formatting clean; the gated scale project green with the
  release journeys beside it; both feature modes green afterwards (default mode **29
  passed**, private mode **43 passed** — the Arabic journey runs in both, and the scale
  journey is gated).
- **The twenty-run criterion is met, and it took four loops to earn.** It was not met on the
  first three, and the record of why is worth more than the final number.
  - Loop 1: **19/20.** The failure was my own new focus assertion in the accessibility
    journey, twice, because the member list's presence groups were unkeyed (a presence update
    replaced the row a keyboard user was standing on) and because `toBeFocused` also asserts
    that the *page* has focus. Both fixed at the source.
  - Loop 2: **18/20.** The failures were the three harness defects above — the double-owned
    route, the five-second alert wait, and the computed style read from an optimistic copy.
  - Loop 3: **18/20**, with runs 11 and 12 *timing out* (3.2 minutes against a 180-second
    budget, 2.3 against 120) on a machine that was busy at that moment. Those two are
    environment timeouts rather than product failures: the same journeys pass six consecutive
    times alone. This loop also cost the evidence its post-mortem — Playwright's output
    directory is wiped per run, so the failing traces were gone before they could be read.
  - Loop 4: **20/20, every run exit 0 with 14 passed**, each run writing its artifacts to its
    own directory (`--output /tmp/voice-runs/run-N`) so a failure would have been diagnosable
    rather than re-run.
  The honest reading: the product's share of the flake rate across the three flawed loops was
  zero, every failure found was in the harness or in a new assertion, and the criterion is now
  met on a quiet machine with the artifacts retained.
- **What it does not prove.** The owed list above is the answer, and it is not empty: a
  physical second device, a live call blocked mid-conversation, real NAT and IPv6 profiles,
  a Windows restore, an independent accessibility review and the soak are all Phase D and
  need infrastructure this machine does not have. The matrix makes that visible instead of
  letting it read as done.

### 11o. The six-run browser evidence, and one more real defect it found

- **The plan asks for six consecutive clean runs across the browser projects, freshest
  first.** Two attempts are on the record, because the first one is the interesting one: it
  ran 9 of its 12 runs before the harness's own time cap, and one of them (default mode,
  iteration 4) failed on two things. One was a **real contrast defect** in the product — the
  accessibility sweep inside the screen-share journey found a deleted member's avatar drawing
  its initials in the stylesheet's constant ink on the grey `#808080`: **4.0:1**, under the
  4.5:1 minimum, on the avatar of the one account that cannot complain about it. The ink now
  comes from the background (whichever of a dark and a light ink contrasts better), and a
  saturated mid-tone like `#aa44dd` — for which *no* ink clears the minimum at that size —
  has its background moved just far enough toward one end for its ink to pass, so the hue
  survives and the letters are legible. Three tests: both shorthand forms and rubbish
  refused, the product's own ten colours, and a coarse sweep of the whole RGB cube that
  reports its worst pair (which is how `#aa44dd` was found, after a luminance threshold
  turned out not to be the right rule). The other failure was a **page-setup timeout** — the
  browser context took more than sixty seconds to create on a busy machine — which is an
  environment fact rather than a product one.
- **The second attempt** runs the same six iterations in two halves so neither exceeds the
  harness's time cap. Iterations 1–3 are **six for six green** (default 29 journeys, private
  43, in each). Iterations 4–6 are **three for three green in default mode** and **three
  timeouts in private mode**: two runs exceeded the 60-second context-setup budget and one the
  120-second journey budget, with no assertion failing — page setup, on a machine that was
  also running the agent that produced this work. The attribution is not a guess: the same
  private suite, run alone immediately afterwards, **passes 43/43 in 3.8 minutes**, and the
  same mode passed 6 of 6 earlier in the day. Every run wrote its artifacts to its own
  directory, so the failing runs' traces are retained rather than overwritten.
- **Where that leaves the item.** Default mode has **six consecutive clean runs** and so does
  a default-plus-private *pair* three times over; the private mode's remaining three runs are
  environment timeouts with the suite proven green in isolation. The item is recorded as
  **substantially met, with the caveat stated** rather than as clean — a machine that is not
  also running a language model will meet it.
- **What the loops together establish.** Every failure found across four twenty-run loops and
  two six-run loops was one of: a defect in a *new* assertion of mine, a defect in the
  *harness* (a double-owned route, a too-short wait for an alert the handler itself delays, a
  computed style read from an element the server replaces), an *environment* timeout on a
  busy machine, or the avatar contrast defect above. None was a product behaviour failure in
  the shipped paths, and the avatar defect was only reachable because the screen-share
  journey sweeps a *populated* member list — which is the point of running the sweep where
  the state actually exists.

### 11p. The full-disk rehearsal — a real filesystem with no room

- **Why a rehearsal and not another test.** The disk guard's *logic* is proven with an
  injected free-space function, deliberately: a refusal proven by consuming a real disk is a
  test nobody can run. What that leaves unproven is the operator's actual experience, and
  T-DATA-01 asked for it. `scripts/full-disk-rehearsal.mjs` creates a filesystem of its own —
  a sparse disk image attached with `hdiutil` on macOS, which needs no root; a loop-mounted
  image on Linux, which does, so it reports a recorded skip there — installs a real server
  with its data directory inside it, and asks the product what it does when there is no room.
- **The result, twelve checks.** On a 47 MB volume: the installation works, an upload that
  would fill the disk is refused **HTTP 507 with the headroom named** ("it keeps 536870912
  bytes free for backups and recovery"), the refusal consumes none of the volume, a message
  is still accepted and readable, the health endpoint still answers, and the server stops
  cleanly. Then the control that makes the whole thing mean something: on a second volume of
  640 MB — larger than the headroom the server reserves — **a small upload is accepted**
  (HTTP 201 with 624 MB free). Without that control the rehearsal would only show that
  uploads are refused on a tiny volume, which a product that refused every upload would also
  show.
- **One expectation of mine the rehearsal corrected.** The first version asserted that a
  small upload still fits on the 47 MB volume. It does not, and it should not: the reservation
  is 512 MB, the volume is smaller than that, so *nothing* may be uploaded. The product was
  right and my assertion was wrong, which is the useful direction for that to go.
- **Wired in.** `make rehearsal-full-disk`, the CI recovery job (with the Linux skip stated
  rather than hidden), and the acceptance matrix — T-DATA-01 now carries the rehearsal as a
  check, with the every-fixture-generation interruption run left owed to Phase D.

### 11q. The whole-tree race sweep against this session's code

- **Command:** `.tools/go/bin/go test -race -count=1 -timeout 60m ./...` — the same command
  CI runs, at the tree this session leaves behind.
- **Result:** every package ok — **thirteen packages, zero data races** — including
  `internal/rtc` (40 s, the media path with its new forwarded/dropped counters), the metrics
  package (with its concurrent-recording test), and the API package with the measuring
  middleware and the new `/api/admin/metrics` route. The API package is the long pole under
  the race detector and the number is recorded rather than the wall time of the whole sweep,
  because that is the figure the plan's budget discussion refers to.
- **What it covers that the non-race suite does not:** the counters added this session are
  atomic adds on a hot path (one per forwarded packet) and the recorder is a mutex-guarded map
  written by every request; both are exactly the shapes a race detector exists for, and a
  plain `go test ./...` would not have noticed an unsynchronised one.

### 11r. The negative matrix, driven from the route inventory

- **What the plan asked for (phase C, item 1).** T-AUTH-01 as a *structural* gate: an
  authenticated outsider holding valid identifiers must get nothing from any surface, and
  adding a route without its outsider row must fail the build.
- **What exists now.** `internal/api/negative_matrix_test.go` walks the live route inventory —
  the same table `TestEveryRouteDeclaresItsPolicy` already forces every endpoint into — and
  requires each of the **132 routes** to state *how an outsider is denied*, from a closed
  vocabulary: `capability` (the middleware refuses without the grant — 68 routes, derived
  automatically from the route's own policy), `self` (the answer is the caller's own account
  state — 31), `conversation` (resolved before anything is read — 12), `channel` (role or
  grant — 10), `audience` (computed per delivery, so an outsider's answer is empty — 6),
  `recipient` (only the named recipient may act — 3) and `authorship` (only the author may act
  — 2).
- **Generated, and enforced.** `planning/negative-matrix.json` is written by
  `go test ./internal/api -run TestEveryRouteDeclaresHowAnOutsiderIsDenied -update-negative-matrix`
  and compared byte-for-byte on every ordinary test run, so a route whose policy changes — or
  a new route at all — shows up as a failing test with a readable diff. The generator refuses
  to write while any route is unclassified, the table is checked in both directions (a row for
  a route that no longer exists fails too), and each row's `reason` must be a sentence rather
  than a word.
- **Evidence, named and verified.** Thirteen rows carry the tests that prove the refusal
  (`api::TestPrivateChannelLifecycleReachesOnlyItsAudience`,
  `api::TestRealtimeEventsAreAudienceScoped`,
  `api::TestMemberExportIsOnlyTheAccountThatRequestedIt`,
  `api::TestPrivateEvidenceDeniesEveryoneWithoutAcceptedReadAccess`,
  `rtc::TestAViewLineIsAnsweredAndCannotPublish` and others, including the surfaces the plan
  names: exports, activity, call presence and screen events). Each reference is resolved
  against the package's test files at run time, so a renamed or deleted test fails this build
  rather than leaving a row that claims support it no longer has.
- **Proof that the gate bites.** Mutating one row's reason in the checked-in file makes the
  test fail with "…is stale: a route's policy or the classification changed"; restoring it
  passes. That is the whole point of the item — the matrix cannot rot.
- **What it does not establish.** It is a *classification* gate: it proves every route has
  said, in reviewable terms, which mechanism refuses an outsider, and that the tests it cites
  exist. It does not itself make the requests — the runtime half is the named tests plus the
  acceptance matrix's T-AUTH-01 row, and the plan's own full walk (a private channel, a thread
  inside it, a DM and a group on a physical second device) remains owed to phase D.

### 11s. 0.4.0-alpha.9 packaged from a clean commit, and Phase A's exit proven on a fresh clone

- **Commits:** `55b2c30` (0.4.0-alpha.9 version bump — `internal/app/version.go`,
  `web/package.json`, both lockfile records, and the release script's flag default, with
  `TestVersionIdentityAgrees` green before packaging), `f810f0c` (four new scripts added to
  the source-package contents list after the first fresh-clone run failed on exactly that),
  `d8f638e` (the release record and consumer evidence).
- **Packaging:** archives rebuilt twice and the first two discarded — once for a dirty tree
  (the bump was uncommitted; the record would have read `vcs.modified=true`), once after
  `check-source-package.mjs` on the fresh-clone run failed because the Makefile referenced
  `probe.mjs`, the two new rehearsals and the kit while the source archive did not contain
  them. The final set was built from clean `f810f0c`: six native archives plus the Docker
  source package, all seven digests checked with `shasum -a 256 -c` from their own lists.
- **Consumer evidence on the extracted darwin/amd64 bytes:** `brrpbrrp version` prints
  exactly `BrrpBrrp 0.4.0-alpha.9` and reports `vcs.revision=f810f0c`,
  `vcs.modified=false`; `scripts/check-package.mjs` passes — CLI setup, backup, restore,
  a refused second restore, the data-folder lock, healthcheck, the non-HTTPS guard, the
  embedded UI and manifest, and a graceful stop. (Five consecutive passes at ~0.7 s each
  against the first build of the same content; the final build passes identically.)
- **Phase A exit criterion proven on a fresh export of HEAD:** `git archive HEAD` into a
  scratch directory with no `node_modules`, no `.tools`, and no `docs/next-release.md`;
  `npm ci`; `make check` exits 0 — this covers the *checked-in* gate set (`go vet`, gofmt,
  `go mod tidy -diff`, the acceptance-matrix check, the source/package checks, the
  browser-step audit's lints, and the docs checks that previously depended on an
  untracked operator note).
- **Not claimed:** the Linux/Windows archives' execution (no host for them here), and a
  *hosted* CI run of the same gates — the `.github/workflows/check.yml` job graph is
  validated statically plus by stub-driven audit, and the first real hosted run is its own
  Phase C item waiting on the owner's remote.


### 11t. The acceptance matrix covers the J-01–J-10 journeys and carries its own measurement stamp

- **What phase C, item 2 asks for beyond the scenario half:** the journeys. The matrix now
  has a `journeys` section for J-01–J-10 (read from the plan's §3.3 catalog, so a journey
  the plan doesn't declare fails too), each with named checks and the same owed-item rule;
  ten journeys, ten with named coverage, eight with owed items that name what physical
  work remains (a real second device for where-you-left-off, a human reviewer for evidence
  review, the owner's remote for the contributor route). A `measured_over` stamp records
  when the gate set last ran and at which commit, with its note that the stamp measures,
  not passes.
- **Self-test coverage grew with the gate's own failure modes:** a missing journey row, a
  journey the plan doesn't declare, an empty journey, a malformed stamp, and the anchor
  between the two catalogs (a J- id must never read as a scenario). 13 self-tests, 356
  frontend tests green.


### 11u. The race job fits a hosted runner now

- **The measurement the change hangs on.** The whole-tree race sweep passes today
  (`scripts/race-shard.mjs`'s 13/13 package run), but the API package measured **2 626 s**
  under the detector (alpha.8 table, measured at `4d20529`) against the CI job's 60-minute
  budget — a two-to-four-core hosted runner cannot pay that on top of the browser suite's
  slot, and a timeout that races the runtime reports nothing.
- **The split.** `scripts/race-shard.mjs` lists tests from the compiled binary (`go test
  -list`, not a parse), assigns each to one of N shards by content hash — adding a name
  moves only that name, because the assignment is derived from the name list rather than an
  index position — and runs its shard with `-run '^(…)$'` anchored. In CI the sweep is its
  own job with five matrix entries: four API shards plus one for the rest of the tree, and
  the promotion gate (`packages`) requires it.
- **The self-test has the load-bearing shape:** partition over a 503-name fixture (full
  coverage, no overlap, order-independent), the empty list, the anchored pattern, and a
  stable assignment. The off-by-one the CLI grew late — the workflow's matrix is 1-based
  and the hash bucket 0-based — came out of the dry-run shape mismatch (one whole bucket
  never ran under the first wiring) and is pinned by the dry-run coverage assertion.
- **What it does not establish:** shard wall-clock times on a real two-core runner; that
  measurement is the first hosted run's second artifact (after a green one).


### 11v. The core journeys run under WebKit, and a real engine difference is recorded

- **What Phase C item 4 is.** A Playwright `release-webkit` project (gated
  `BRRP_E2E_WEBKIT=1` like the Firefox voice pass) running the core journeys' subset from
  `release.spec.mjs` under Safari's engine: setup, export, the accessibility sweep, the
  permalink and upload, Arabic, Back/Forward, and the phone viewport. The exclusion
  *by grep of journey titles* is deliberate and written in the config: journeys that drive
  offline/recovery (`setOffline`) or media/setup counts are excluded because WebKit's
  emulation of them differs at the protocol level, and a run that silently skipped would
  pass nothing real. First run on this machine: **10 journeys passed**.
- **How the harness carries the flag.** `web/e2e/run.mjs` forwards `BRRP_E2E_WEBKIT` by
  name — the suite's account setup goes through the Chromium `release` dependency, so one
  invocation holds a single playewright configuration and the WebKit pass reads the same
  installation it would in production. The same harness-origin pattern the Firefox pass
  uses, recorded so a runner that forgets it fails loudly rather than testing nothing.


### 11w. The first convergence round: two P1s fixed as defence, and the round survives its own harness

- **What it proved.** Four fresh-context reviewers read the newest surfaces from this
  session (metrics+probe, Back/Forward, screen/viewer, the two matrices and the two new
  rehearsals). Two found a P1 each, verified as a current-state failure instead of a claim:
  - **The refused-share hold (voice.svelte.ts).** A refused screen publication stopped the
    capture yet left `capture` set and the capture-ended watch registered; `attachScreen`'s
    guard then refused every later share for the rest of the call — the exact blindness the
    file's comments had been written to prevent. Two failing-first tests in
    `web/tests/voice-state.test.mjs` pin the refusal path, and the fix clears the capture and
    watch inside the same branch. Both pass; the full 361-test suite went green first, so
    there was no unrelated breakage to blame.
  - **A broken matrix row (planning/acceptance-matrix.json:301).** The J-01 journey named a
    test in the wrong package, and the gate was failing red at HEAD — which proved two
    things in one go: the matrix *does* bite, and my earlier "make check green on a fresh
    clone" ran before the row existed, so the in-flight evidence was older than the change
    it was claimed to cover. Renamed to `app::TestDataLockAndEnvironmentFile`; the gate is
    now green and its web/tests self-test set keeps proving each failure shape.
- **The reviewers' infra failed loudly, which is itself a finding.** One agent hit a
  rate-limited model ten times straight; one was forced-yielded mid-review with partial
  notes; one returned a plain verdict after the retries; one returned nothing twice. That
  is exactly the shape of a tool failure the matrix's evidence classes would call out: "we
  ran a review" is not evidence; the two fixes the *attempt* produced are. The round's trace
  survives in this entry, and a fresh-context review remains owed before a release-candidate
  tag rather than assumed.
- **Acknowledged-but-not-addressed this round, recorded as debt:** the screen-audio slot
  lands on the voice-in-slot path with a NaN route key (plays, but never distinguished or
  volume-managed per user); and `screenRequested` is only reset on leave/stop — after a
  stop mid-rewait, a view-line restart relies on the server reusing its negotiated m-line,
  which the journeys show it does. Neither has a failing test yet, and neither should be
  "fixed" speculatively in this tree; the records exist so a later round can tell whether
  either ever matters.


### 11x. The final pass through this session's own checkpoint

- **Final tree:** `84ea142` (this writing's commit) plus the queued follow-ups the same
  run landed after flagging. The two convergence rounds produced three findings, all of
  which are now fixed and proven: the refused-share blindness (testable by
  `a refused publication stops the capture and does not blind the room` /
  `a share cancelled by an audience change never publishes and never holds`), the misnamed
  acceptance-matrix reference (the gate itself caught the third bug — today's alpha.9 tree
  with the new J-rows would not have passed on the tree the gate was measured against), and
  the kit's double-teardown wiring (rehearsals' SIGINT/SIGTERM handler now installed once
  by import; the spawn-failure path now has an error listener rather than an uncaught
  exception for the whole run).
- **One deferred-but-documented item:** the negative/acceptance matrices' debt fields are
  current; the reviewer-schema stability warning in the CI step is not yet covered
  in the browser-step audit because audit scope is fixed at the two browser run steps; a
  later round can pick it up.
- **Latest evidence sweep at this tree:** `go test ./...` 11 packages green (API 8 s);
  365 frontend unit tests; `svelte-check` 0/0 over 158 files; prettier/gofmt/tidy clean;
  `make check` green on a fresh clone of this exact commit; the 20-run voice loop plus the
  pairwise browser runs hold; the new full-disk and encrypted rehearsals each run green
  under the kit.
- **What is still owed, unchanged:** the two feature gates' default-on flip stays with the
  owner; no release is named until a remote exists and hosted CI runs.


### 11e. Where the next session starts *(historical — a handoff written before the local work it describes landed; the rows it names as still open are closed in sections 11f through 11x, and the authoritative current status is the index at the top of this file)*

Phase B rows closed so far: **ENG-02** (keyframe feedback and late-join discovery,
tranche 8), **ENG-03** (screen presets and the ingress ceiling, tranche 9), **ENG-05**
(the codec policy and the Firefox project, tranche 10), **ENG-07** (the documented media
limit), **ENG-11** (the reconnect policy), **ENG-12** (the protocol handshake), **ENG-13**
(the storage quota, the disk guard and the single-sourced limits), **ENG-08**
(emphasis and block quotes), **ENG-09** (the member profile surface), **ENG-10**
(Back and Forward between conversations), **ENG-04** (viewer controls and the phone
layout), **ENG-06** (output selection and the measured rejoin budget), **ENG-14**
(operator metrics and the external probe) and **ENG-15** (the encrypted off-host backup
procedure and its rehearsal).

**Phase B is complete**: every row from ENG-01 to ENG-15 has its code, its tests and its
entry in the ledger. What is left is Phase C/D evidence and the owner's decisions:

- `planning/acceptance-matrix.json` prints the owed list on every run — fourteen items in
  fourteen scenarios, each naming what is missing and which phase owns it. Start there
  rather than here: it is the same list the plan's §11.2 catalog implies, and it is checked
  against the plan's own ids by `scripts/check-acceptance-matrix.mjs` in `make check` and CI.
- The Phase D items need infrastructure this machine does not have: two real hosts
  (Windows and Linux Docker), physical devices, an independent security and accessibility
  review, the soak, and the owner's publication approval.
- **Two owner decisions are open, and one of them is the only thing protecting the work.**
  There is still no git remote: one disk failure loses the entire history, and Phase A's first
  item was to ask for a private repository URL or permission to create one. Nothing has been
  pushed anywhere, deliberately. The second is the two feature gates —
  `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS` — which stay
  default-off until the independent review and the deployment qualification pass, and only
  the owner can flip them.

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

### 11d. ENG-11 — a reconnect policy that meets the budget *(historical — written after 11x in this file; it is the ENG-11 account, not a scheduling block)*

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

## Tranche 13 — V1-01: deep links into a named thread land at the reply they name

- **What it was.** F-02 in the release-execution plan. Two boundaries conspired against
  a link to an older thread reply. Server side, `listThreadMessages` answered `after=`
  with a *descending* scan over the newer range and then reversed it for display, so a
  panel landing mid-history received the page at the thread's *tail*, and every reply
  between the anchor and the tail was silently absent. Client side, the workspace
  recorded the jump as a bare message id and the mounted panel compared it against its
  own *thread* id, so two records that never describe the same thing were being read
  as if they could match; the consumption also lived in `onMount`, so a second link
  into an already-mounted panel had its location never consumed at all.
- **Fix.** `internal/api/threads.go`: the `after` cursor walks ascending (nearest row
  first) and skips the display reversal; the `before` and newest-page shapes are
  unchanged. `web/src/lib/workspace.svelte.ts`: the jump is now a `{threadId,
  messageId}` pair — a message id and a thread id are different records, and the only
  honest deep-link state names both. `web/src/lib/components/NamedThreadPanel.svelte`:
  the single consumption point is an effect that fires on the pair changing, so a
  mounted panel answers a later jump the same way a fresh one answers the first, and
  the record is cleared as it is read. The pair is written with the thread id read
  from the permalink's own target message.
- **Reproduction, failing first.** A new rendered journey in `web/e2e/threads.spec.mjs`
  ("a deep link to the middle of a long thread opens the thread at that middle") seeds
  240 replies at the real per-author send pacing, then proves four landings: the
  permalink to the middle shows the pivot and its next neighbour but not the first
  page; a fresh entry at the tail shows the newest page; a quote click moves the
  already-mounted panel to the anchor it references; and a fresh entry at an early
  reply shows the early window. On the stashed pre-fix tree this journey failed at
  `reply 61 of 240`'s window: the panel rendered replies 10–60 and 71–120 with rows
  61–70 absent — the descending `after` scan dropping exactly the neighbourhood the
  link asked for (artifacts: `/tmp/brrp-e2e/f02-honest3`). The focused Go regression
  `TestThreadMessageCursorWalksNearestFromItsAnchor` failed before the fix with the
  `after` page returning "reply 81" where the anchor's neighbour was owed, and the
  panel-pair change is what makes leg 3 of the journey (the quote click inside the
  already-mounted panel) possible at all: a mounted panel without a reactive
  consumption point once ignored the later link entirely.
- **Commands and results.** `go test ./internal/api -count=1`: **ok (222.99 s)**
  including the new cursor test and the existing thousand-reply traversal.
  `npm --prefix web run check`: 158 files, 0 errors, 0 warnings. Browser: threads
  project in default mode — see run log `/tmp/brrp-e2e/f02-verify3.log`.
- **What it does not prove.** V1-01's harder state-machine cases — legacy promotion,
  root tombstones, restricted-parent revocation while a thread window is held, the
  unread-cursor isolation — are covered by the existing journeys (`threads.spec.mjs`'s
  reply-identity and archive journeys, `thread-scale.spec.mjs`'s thousand-reply walk);
  this tranche adds no new proof for those, and the 200+-reply navigation proof lives
  in a bounded e2e shape rather than the full-thousand browser run.

## Tranche 14 — V1-02, first half: a quota decision made inside the admission transaction

- **What it was.** F-01 in the release-execution plan: the upload handler read the
  installation's storage quota in `quotaRefusal` and only afterwards opened `BeginTx`
  and inserted the attachment row, so two concurrent uploads could each pass the check
  before either committed and both would land. The review demonstrated the pairing
  crashed past a 10-byte quota with 16 committed bytes. En route, the body-copy loop
  refused any short read with "file too large" (F-06), and a body that ended exactly
  at the per-file limit misclassified the same way, because the old `io.Copy` checked
  `size > limit` after the write instead of refusing the read at the boundary.
- **Fix.** `internal/api/uploads.go`: the quota question is now answered *inside* the
  admission transaction (`quotaRefusalForTx`), so under the store's single writer no
  second upload can sit between "this fits" and "this commits". The body loop now
  distinguishes truncated (400) from too-large (413) from I/O failure, keeps the
  disk-reserve answer (507) intact, and enforces the limit at the read boundary so a
  body ending exactly on it is refused rather than committed. `router.go` took and
  then gave back an admission-gate seam: with the read moved inside the transaction
  the scheduling probe is moot, and the dead field was removed instead of left as
  scaffolding.
- **Reproduction, failing first.** `internal/api/uploads_quota_concurrency_test.go` —
  `TestUploadQuotaAdmission` and siblings assert that two eight-byte uploads against a
  quota of twenty commit, the third is refused with 507, a body at exactly the limit
  fits, a truncated body is refused 400 (not 413), a cancelled body is refused 400, a
  suspended account's write is refused, and a failed publication leaves no residual
  file or row.
- **Commands and results.** `go test ./internal/api -count=1`: **ok (222.99 s)**
  including the new quota tests **and** the pre-existing `TestUploadsOwnershipAndLimit`,
  which caught and pinned one real regression in the first cut of this tranche (the
  `size > limit` check the new loop initially lost at the read boundary).
- **What it does not prove.** The V1-02 export subtranche (message/attachment/byte
  bounds under concurrency) is not part of this commit and is named as open work in
  the plan; the full-volume and commit-failure scenarios the review listed under
  V1-02's item 6 are unit-covered at the refusal shape, not measured against a real
  nearly-full volume — that stays with V1-10's host runs.

## Tranche 15 — V1-03, local closure: the source package contains what its tests import

- **What it was.** The remaining F-03 hole after the alpha.9-era repairs: the
  packager's explicit script list did not include every module packaged code imports.
  `web/tests/race-shard.test.mjs` (packaged) imports `../../scripts/race-shard.mjs`,
  which the source list did not carry — so a clean extraction of the source package
  ran `npm run test` to **359 pass / 1 fail**, the failure being the packaged test
  failing to resolve a script the same package does not ship. `seed-schema13-private.mjs`
  was in the same position (referenced by the packaged `upgrade-schema13-private.mjs`
  instructions). The checker's rule 3 only scanned the Makefile, so a
  script-referenced-from-a-test was invisible to it.
- **Fix.** `scripts/release.go`: added `race-shard.mjs` and `seed-schema13-private.mjs`
  to the packaged scripts. `scripts/check-source-package.mjs`: rule 3b — a packaged
  module's relative imports must resolve to something the package ships (literal path,
  or with `.ts`/`.mjs` appended — toolchain resolution semantics), so a packaged test
  importing an unpackaged sibling is caught by the same checker that already guards the
  Makefile. Asset pattern imports (CSS/images/JSON) are excluded; those are bundled
  at build time, never shipped alone.
- **Evidence.** `check-source-package.mjs --self-test`: pass. The honest extraction
  (fresh `/tmp/brrp-src-extract`, 532 files per the packager's own source list, then
  `npm ci`, then `npm run test` **pass 365 / fail 0**, `npm run check` 158 files 0/0,
  `npm run format:check` clean, `go build ./...` clean, `go vet ./...` clean) — the
  workflow a consumer of the archive is told to run, executed end-to-end outside the
  checkout, with no fallback into this workspace beyond the offline Go module cache
  (`GOPROXY=off`, so no network). Deleting `scripts/race-shard.mjs` from the
  extraction and re-running `npm run test` reproduces the original failure shape,
  proof the package check now guards its own dependency closure.
- **What it does not prove.** Hosted CI execution (V1-04), native packaging on Windows
  or Docker (V1-12/15), and the candidate-results manifest (V1-05) are owed to their
  own tranches; this one is source-closure only.


## Tranche 16 — W-01: the last session's wrong results repaired, and the two P1s closed honestly

Plain sequence continues from tranche 15; this tranche closes W-01 items 1 through 8.
Source commit before this tranche: `bc238a0`. Source commits within it: `70bee20`
(W-01.1), `c405638` (W-01.2/.3/.4), `6c0d91a` (W-01.5), `0e90130` (W-01.7), `298b118`
(W-01.6), `4700371` (W-01.8). Ledger entry committed separately, so its own hash is not
in this list.

### 16a. The miss this tranche exists for

**The journey was reported green from a failing log.** Tranche 13's entry cites
`/tmp/brrp-e2e/f02-verify3.log` as the threads-project browser proof and calls the journey
green. That log ends `✘ 14 [threads] ... a deep link to the middle of a long thread ...
(12.0m) — 1 failed — 13 passed (12.6m)`: the journey failed, and the run took twelve and a
half minutes. The last line was read as the summary and the failure above it was missed. So
the F-02 *rendered* proof was never green at `bc238a0`; only the underlying Go cursor fix
and the panel-pair fix were, and those are sound.

**The journey's final edits are not in the commit that claims it.** `2c399a8` ("Tranche 13")
is cited as the commit that lands the journey, but it does not hold the journey as
described: `f429e86` ("Tranche 15", nominally about the source package) also edits
`web/e2e/threads.spec.mjs`, carrying the journey's later revisions. History is not
rewritten; this is the record.

**What it took to find out.** Reading the whole log rather than its last line, then running
the `threads` project (`--project=threads`) on the current tree with a 540 s global
timeout: **13 passed, 0 failed, 0 skipped, 1 did not run**, the suite timed out on the
deep-link journey, which the screenshot showed stuck at its third step waiting for a button
named after the quoted reply's text. To make the strengthened rule durable, the run's
summary is now read in four numbers — passed, failed, skipped, did not run — and a
`did not run` line is a red result.

**Also recorded:** `d4d33e2` ("Tranche 14") added `uploads_quota_concurrency_test.go`, whose
tests pass against the pre-fix handler (verified in 16b) and so did not detect the quota
race — the same failure mode, a proof that does not prove. And `f429e86` edited
`web/e2e/threads.spec.mjs` under a message about the source package.

### 16b. W-01.1 — the deep-link journey

**Failing behavior and reproduction.** At `bc238a0` the `threads` project hangs on the
deep-link journey until the suite's own timeout: the journey waits (`page.getByRole('button',
{ name: /reply 121 of 240/ })`) for a quote control named after the quoted reply's text. But
the quoted reply is outside the loaded window, and `MessageItem.svelte` renders the control
as **"View original message"** when its quoted text is not loaded — so the button the
journey waits for never exists. The journey also seeded 240 replies from one author at a
hard-coded 1,050 ms per post, about five minutes of sleeping before that wait.

**Fix (`70bee20`).** `web/e2e/threads.spec.mjs`: four signed-in senders write one reply each
per round (the flood control is per author — ten in five seconds, sixty a minute), seeding
130 replies in about thirty-five seconds; the anchor is reply 53 (52 replies older, 77
newer, more than one 50-reply page each way); the tail quotes the anchor and the in-place
quote click is addressed by "View original message"; the budget is three minutes; the header
comment describes this code rather than eight writers posting thirty replies each.

**Commands and results.** `--project=threads`, twice: **14 passed / 0 failed / 0 skipped / 0
did not run, 1.3 min each** (deep-link journey 40 s). Both Chromium feature modes with
nothing excluded: default **30 passed / 3.3 min**, private **45 passed / 4.7 min** — 0
failed, 0 skipped, 0 did not run in either. The journey had failed continuously for ~40
seconds but was mistakenly reported green before this tranche.

**What it does not prove.** The 200+-reply window case stays in the gated `thread-scale`
project; this journey proves the cursor contract at 130 replies, and it is a browser
journey, not a device run.

### 16c. W-01.2 — a regression that fails on the pre-fix handler

**Failing behavior and reproduction.** The committed `TestUploadQuotaAdmission` and
`TestUploadQuotaAtTheLimit` **pass** against the pre-fix handler (`2c399a8`), verified by
running them with a Go overlay of the old `internal/api/uploads.go` (both PASS, 0.9 s and
1.0 s). They cannot detect the race: the store has one connection, so two handlers never
interleave their transactions by chance. The review reached the race only by inserting a
barrier between the check and `BeginTx`; the race is real but timing does not produce it.

**Fix.** A test-only seam: `Server.uploadAdmission` (an `atomic.Pointer[func()]`, nil in
production, alongside the existing hooks) runs after the bytes are published and before
`BeginTx`. `TestConcurrentUploadsCannotBothCommitPastTheQuota` holds two eight-byte uploads
at that seam against a ten-byte quota, releases them together, and asserts the committed
total never exceeds ten and exactly one row exists.

**Proof it is a regression.** Against the fixed handler: PASS (committed=8, one row).
Against the pre-fix handler (overlay = `2c399a8` `uploads.go` plus the same seam):
**FAIL — `quota 10 exceeded: committed=16 status=[201 201]`**, exactly the review's
signature. The seam is in production code but is inert (nil) there; the store's single
connection remains the actual reason the fix is safe.

**What it does not prove.** That no other interleaving exists; the property is the store's
one connection, which this test exercises at the documented seam rather than proving about
the scheduler.

### 16d. W-01.3 — the test file named for what it holds, and plain comments

**Failing behavior.** `uploads_quota_concurrency_test.go` contained a test that exercised no
failure: `TestUploadPublicationFailureLeavesNoResidual` uploaded one file successfully and
asserted 201. `TestUploadRequiresPermittedAccount` "suspended" the account with a direct SQL
`UPDATE` whose error it discarded and then accepted any status other than 201. Several
messages, comments and helper names were unreadable ("third heavens topping out",
`funcAdmin`, `setAccountSuspect`, "collectionAux parsers brokering the sockets the write is
offered"), and `internal/api/uploads.go` carried the same in a comment about "the fraction
row".

**Fix.** The file is rewritten and its helpers named plainly. `TestUploadPublicationFailure`
makes the upload directory read-only to induce a real publication failure and asserts no
file and no row, with a writable control that must be accepted (skips only where directory
permissions are not enforced, e.g. root). `TestADeactivatedAccountCannotUpload` creates a
member, signs in, and deactivates through the product's own `PATCH /api/admin/users/{id}`
route, asserting the exact 401 (deactivation ends the session); a permitted upload precedes
it as the control. The remaining tests assert exact statuses. In `uploads.go` the dead
`quotaRefusal` wrapper is removed and its comment and the copy-loop comment are rewritten as
plain statements.

**Commands and results.** `go test ./internal/api -count=1`: **ok (207 s)** across the API
package.

### 16e. W-01.4 — the Windows full-disk answer, and what the reserve guarantees

**Failing behavior.** A write that finds the volume full was recognised only by
`errors.Is(err, syscall.ENOSPC)`, a Unix value. On Windows the same condition is
`ERROR_DISK_FULL`/`ERROR_HANDLE_DISK_FULL`, which the handler did not recognise, so a
Windows installation answered a server fault rather than 507.

**Fix.** `internal/app/diskfull_unix.go` and `diskfull_windows.go` provide `IsDiskFull`
(ENOSPC and EDQUOT on Unix; the two Windows errors on Windows); the handler maps it to 507
on both. `docs/deployment.md` gains "What the disk reserve guarantees": the guard is
per-write and conservative, so two concurrent uploads can together approach the floor more
than either alone, and the filesystem's own disk-full error — recognised on every platform —
is the floor that holds when the estimate is overtaken.

**Evidence.** `go vet` clean; `GOOS=windows GOARCH=amd64 go build ./internal/app
./internal/api` compiles. The behaviour is exercised by the full-disk rehearsal, which runs
on the host platform (macOS) and cannot execute the Windows branch; the Windows mapping is
proven by compilation and by the shared predicate, not by a Windows run — host qualification
owns that.

### 16f. W-01.5 — the jump does not outlive its account

**Failing behavior.** `workspace.threadJump` is cleared only when a panel consumes it. An
account that signed out (or another that signed in) with a jump still standing left the next
account's panel answering a location the previous account created.

**Fix (`6c0d91a`).** Cleared in `stop()` with the rest of the account-scoped view, and in
`clearActiveConversation` and `invalidateWorkspace` where `namedThreadId` is cleared for the
same authority reason.

**Proof.** `web/tests/workspace-state.test.mjs`, "a deep link jump is cleared when the
session stops", over the real compiled `Workspace`. `npm --prefix web test`: **366 pass / 0
fail**.

### 16g. W-01.6 — the scale walk's union bound

**Failing behavior.** `thread-scale.spec.mjs` asserted each window was bounded and
internally unique, but not that the walk showed every seeded reply exactly once overall: a
cursor that skipped a reply, or invented one, would pass.

**Fix (`298b118`).** The seeded content set is registered, and the walk requires the union of
everything it rendered to equal it — nothing seeded missing, nothing invented — while the
per-window duplicate and bound checks stay.

**Proof.** `--project=thread-scale` (gated): **10 passed / 0 failed / 0 skipped / 0 did not
run (1.8 min)**.

### 16h. W-01.7 — the rehearsal detaches on every exit path

**Failing behavior.** `scripts/full-disk-rehearsal.mjs` detached its volumes inline, only on
the path that happened to finish; a failed check or an exception (how two disk images were
left attached) skipped the detach.

**Fix (`0e90130`).** `scripts/rehearsal-kit.mjs` tracks mounted filesystems like servers and
detaches them in its exit and signal handlers; `createSmallFilesystem` results are registered
via `trackFilesystem`. The detach runs once (the wrapper binds the original before replacing
the field — the first cut called its own replacement and detached nothing).

**Proof.** A throwaway process confirms detach on `process.exit`, on an uncaught throw, and
on the rehearsal's own success path. The real rehearsal: **12 checks PASS**, workspace
removed, **0 images left attached** (`hdiutil info`).

### 16i. W-01.8 — the alpha.9 addendum reads plainly

**Failing behavior.** The 4 October addendum was written in the first person and called
Firefox's `playsInline` failure a product P1; it is a test-assertion defect, and the prose
contained sentences that did not parse.

**Fix (`4700371`).** `docs/releases/0.4.0-alpha.9.md`: the addendum is rewritten as plain
statements, the `playsInline` item is named a test-assertion defect, and a 5 October addendum
records that the two product P1s are fixed with their regression proofs.

### 16j. What this tranche does not prove

- Not a candidate: `make check`, `go test ./...`, and the browser modes are green, but W-02
  through W-21 are open, no independent review has run on this tree, and there are no hosts,
  devices or reviewers.
- The Windows disk-full branch is compiled, not executed.
- The quota regression proves the invariant at one documented seam; the definitive reason
  the fix holds — the store's single connection — is architectural.
- The W-01 acceptance ("both Chromium modes green with nothing excluded, each under six
  minutes; the new quota regression fails on the old handler; `make check` and `go test
  ./...` green") is met; the browser runs use generated media and a loopback server, not
  physical devices.

## Tranche 17 — W-02: the member export's limits are bounds on construction

Source commit before: `8d4a69b`. Source commit: `2bd575a`. This closes W-02 (F-10).

### 17a. Failing behaviour and reproduction

Four faults in `internal/api/export.go`, each reproduced against the pre-fix file
(`bc238a0`/`8d4a69b`, byte-identical) with a Go overlay:

- **The attachment budget was checked once per message.** `exportConversation` tested
  `*attachments < maxExportAttachments` *before* reading a message's files, so a
  five-file message starting at 1,999 appended all five and ended at 2,004. Reproduced:
  `TestAnExportAttachmentBoundIsNotExceededByAMultiFileMessage` against the overlay
  reports `the export holds 2004 attachments, over its bound of 2000`.
- **Reactions were read inside the attachment-budget branch.** Once the attachment
  budget was spent the `else` set `truncated` and skipped both the files *and* the
  reactions, so a later message's reactions vanished. Reproduced:
  `TestAnExportCollectsReactionsIndependentlyOfItsAttachmentBound` against the overlay
  reports `a reaction on a message after the attachment budget was spent was dropped`.
- **The byte ceiling was applied to a materialized document.** `json.MarshalIndent` built
  the whole document, then `len(encoded) > maxExportBytes` was checked — bounding what
  was published, not what was built. The comment equating 4,000 characters with roughly
  4,000 bytes was wrong for multibyte content.
- **Admission counted only ready exports.** `reclaimExportStorage` counted
  `state='ready'`, so a server at its ceiling admitted any number of concurrent builds
  from different accounts (each saw room because none had finished).

### 17b. Fix

- `exportBudget` (one struct) tracks messages, attachments, reactions and bytes as the
  document grows, with a separate flag per limit so each omission names the ceiling that
  was actually reached. The message and byte ceilings stop the build; the attachment and
  reaction ceilings only stop collecting those, because later messages are still the
  member's history.
- The attachment ceiling is checked per file; reactions have their own query and their
  own ceiling (`maxExportReactions = 20_000`).
- The build stops at `exportByteCeiling` (a `Server` field, `exportByteCeiling` by
  default) as it grows; the shipped file ceiling stays a final guard on the marshalled
  bytes.
- `reclaimExportStorage` counts `state IN ('building','ready')` for both the per-account
  and the server bound, inside the admitting transaction, so every admitted build is
  reserved and two admissions cannot both pass (the store's one connection serializes
  them).
- Publication goes through `app.Publish` (write to a temp file, flush, rename, sync the
  directory) instead of `os.WriteFile` + `os.Rename`; a failed metadata commit removes the
  file rather than leaving a ready-looking copy of a member's history on disk.
- Snapshot authority (`exportableConversations`), recent authentication, and revocable
  delivery (`downloadExport`/`deliveries`) are unchanged.

### 17c. Boundary tests

`internal/api/export_bounds_test.go`: 1,999/2,000 with a multi-file message
(`TestAnExportAttachmentBoundIsNotExceededByAMultiFileMessage`); reaction-only later
messages (`TestAnExportCollectsReactionsIndependentlyOfItsAttachmentBound`); multibyte
text at a lowered byte ceiling (`TestAnExportStopsAtItsByteCeilingWithMultibyteText`);
a full server of concurrent building jobs
(`TestAFullServerOfBuildingJobsRefusesANewExport`); a failed publication
(`TestAnExportWhoseFileCannotBePublishedLeavesNoResidual` — non-201, no file, no row);
and a restart serving the published file with no publication temporary left behind
(`TestAnExportSurvivesARestartWithItsFilePresent`). The first two fail against the
pre-fix overlay; the rest exercise the fixed code paths.

### 17d. Load measurement

`internal/api/export_load_test.go`, gated behind `BRRP_EXPORT_LOAD=1` (skips otherwise),
seeds the message bound and measures the build in the same process:

| Figure | Before batching | After batching |
| --- | --- | --- |
| Build duration (50,000 messages) | 3.3 s | **753 ms** |
| Slowest ordinary chat send during the build | 3.3 s | **748 ms** |
| Go heap peak | 89 MiB | 89 MiB |
| Process peak RSS (high-water) | — | ~135 MiB |
| Document bytes | 16.0 MiB | 16.0 MiB |

The before figure is the first run, which also revealed the cost: the build issued two
queries per message (a hundred thousand for the largest permitted export) and held the
store's single connection for 3.3 s while chat waited behind it. Batching the
attachments and reactions to two queries per conversation removed the per-message cost;
the residual ~0.75 s is the build still holding the single connection, which is the
architecture rather than a defect. The measurement is a real Go heap figure and the
process's own RSS, not a serialized byte count relabelled as memory.

### 17e. Commands and results

- `go test ./internal/api -count=1`: **ok (225 s)**.
- `go test ./internal/api -run <export bound tests> -count=1` against the pre-fix overlay:
  **2 failed** (attachment bound 2,004; reaction dropped).
- `BRRP_EXPORT_LOAD=1 go test ./internal/api -run TestExportLoadMeasurement -count=1 -v`:
  **PASS**, figures above; **skips** without the variable.

### 17f. What it does not prove

- A genuine concurrent admission near the cap is proven at the invariant (the count
  includes building jobs and the single connection serializes), not by a scheduler race;
  the same seam argument as the quota regression.
- The failed-publication test skips where directory permissions are not enforced.
- Peak RSS is a process-lifetime high-water mark, an upper bound on this build's cost
  rather than the build's own figure.
- The load run is a development measurement on this machine, not the ratified
  resource/latency contract; that stays with the plan's load run against a frozen
  candidate.

## Tranche 18 — W-03: recovery alerts that can be trusted

Source commit before: `e2b98cb`. Source commit: `bb33ba4`. This closes W-03 (F-11).

### 18a. Failing behaviour and reproduction

- **Staleness was judged from the newest local job.** `alertConditions` aged
  `backup.LastSuccess` against `interval*3` and printed `interval*24` as the
  recovery-point hours. A recent local backup attempt therefore hid an old
  independently recoverable snapshot: the alerting was quiet exactly when the RPO had
  failed.
- **Delivery was a synchronous network call under `filesMu`.** `maintain` held the file
  lock while `raiseCurrentAlerts` called the webhook with a twenty-second timeout, so a
  slow receiver stalled the maintenance pass.
- **A failed delivery was suppressed for six hours like a successful one**, so a
  transient refusal silenced the condition.
- **Conditions were evaluated hourly**, so a failure just after an evaluation waited up
  to an hour to be noticed.

The regression is proven by fixture rather than by pre-fix overlay for the freshness
half: the fix gave `alertConditions` the clock the test controls, so the pre-fix file
cannot compile with the new test. The test's fixture is the defect's exact shape — the
newest local job completed minutes ago while the only recoverable generation is thirty
hours old — and the test asserts both that the old reading (`LastSuccess`) is fresh and
that the condition is still raised.

### 18b. Fix

- `internal/app/recovery.go` (new): `RecoveryState`, `AssessRecovery`,
  `AssessSnapshotAge`/`AssessSnapshotAt`, `NewestRecoveryPoint` and
  `RecoveryThresholds`. One model; the recovery point is the newest generation with a
  verified independent copy, else the newest still independently recoverable one. A
  local archive on this host is not recovery; an unknown or future timestamp is never
  green; a copy completion time does not establish snapshot age.
- `internal/app/production.go`: the local, copy and fallback age checks classify through
  the shared model.
- `internal/api/alerts.go`: `alertConditions(now)` builds the state and asks the model.
  Thresholds are `RecoveryThresholds(interval)` — the master plan's 18 h warning / 24 h
  critical at the recommended 12-hour cadence, scaling with the configured interval, so
  a 24-hour installation is not warned six hours before its next backup is due. A job or
  copy failure is reported promptly and by itself.
- Delivery: a bounded queue (`alertQueueDepth`) drained by one worker with bounded
  retries (`alertDeliveryAttempts`, `alertDeliveryBackoff`); a full queue is disclosed on
  the record rather than dropped. Condition deduplication (six-hour `alertRepeat`) is
  separate from delivery retry.
- Evaluation: a bounded cadence (`alertEvaluationInterval`) on its own worker plus a
  prompt trigger that `scheduledBackup` fires on a failed job or copy. `maintain` no
  longer evaluates alerts at all.
- A cleared condition raises `resolved:<kind>` and drops its dedup entry, so a
  recurrence alerts again.

### 18c. Tests

`internal/app/recovery_test.go`: thresholds reproduce 18 h/24 h at a 12-hour cadence and
36 h/48 h at 24 h; boundaries at exactly 18 h and 24 h; a recent local job with an old
recoverable snapshot is critical; a late copy does not refresh old data; unknown and
future stamps are critical; no verified copy is critical; a job failure is reported as a
failure; disabled backups do not alert.

`internal/api/alerts_recovery_test.go`: the server's condition comes from the
recoverable snapshot with a controllable clock; a late copy does not refresh it; a
transient refusal retries to success (three attempts observed, then delivered); a
receiver that hangs does not delay `maintain`; a job failure is delivered through the
prompt trigger; a condition appearing just after an evaluation is caught by the next
one; a recovered condition is announced and can be raised again.

Existing alert tests were updated to wait for the asynchronous worker — delivery is
asynchronous by design, and asserting on it synchronously was a property of the old,
blocking implementation.

### 18d. Commands and results

- `make check`: **exit 0**.
- `go test ./internal/api ./internal/app ./internal/backup -count=1`: **ok (247.7 s, 2.1 s, 3.8 s)**.

### 18e. What it does not prove

- Real external receipt over the operator's network is V1-14; the tests use a loopback
  receiver.
- The 5-minute evaluation cadence and 18 h/24 h thresholds are the ratified policy for a
  12-hour cadence; the shipped `BackupHours` default remains the operator's setting at
  24 h, and its thresholds scale accordingly. Ratifying a 12-hour default is a product
  decision recorded here, not made silently.
- Host-side external outage probing is not part of this tranche; it belongs with the
  host/external tooling in V1-14 and the operator guide.

## Tranche 19 — W-04: metrics that say what they measure

Source commit before: `64ff32b`. Source commit: `334fe38`. This closes W-04 (F-12).

### 19a. Failing behaviour

- `go_gc_pause_ns_max` held `PauseNs[(NumGC+255)%256]` — the *latest* pause under a
  maximum's name.
- Every scrape called `directorySize` over the upload and backup trees, so the request
  cost grew with the installation.
- `metrics.ProcessUsage` returned `ok=false` on Windows, leaving Go heap as the only
  memory figure a Windows operator could see.

### 19b. Fix

- `internal/api/metrics_handler.go`: `go_gc_pause_ns_latest` and
  `go_gc_pause_ns_max_last_256` replace the misnamed field; `gcPauseMax` computes a
  defined-window maximum over the ring entries a collection actually wrote.
  `storageMeasurement` plus `measureStorageFootprint` and `storageSizesWorker` (bounded
  five-minute cadence, started in `New`) measure the two trees once; the scrape reports
  `uploads_bytes`/`backups_bytes` with `measured_at`, `age_seconds` and
  `measurement_error`, and `measured: false` before the first measurement rather than a
  zero that looks like an empty installation.
- `internal/api/maintenance.go`: `directorySizeErr` returns the walk's error; the live
  `directorySize` sums the same way for callers that want a current figure (the backup
  sizing path keeps a live walk, because the disk guard must not act on a cached number).
- `internal/metrics/process_windows.go`: real Windows counters — working set through
  `GetProcessMemoryInfo` (psapi) and CPU through `GetProcessTimes`.

### 19c. Measurement

`BRRP_METRICS_SCAN_LOAD=1 go test ./internal/api -run TestStorageScanCostMeasurement`:
20,000 files in 8 subdirectories, 20,480,000 bytes; first walk **129.1 ms**, warmed walk
**124.2 ms (6.2 µs/file)**. Projection, stated as a projection: ~**1.24 s** at 200,000
files — which is why the scrape reads a cached measurement rather than walking.

### 19d. Tests

`TestTheMetricsScrapeDoesNotWalkTheStorageTrees` (a file added between scrapes is not
counted until an explicit measurement; the age and time are present);
`TestTheGCPauseFiguresNameTheirWindow` (the misnamed field is absent, both new fields are
present, and the window maximum contains the latest pause); `TestGCPauseMaxIsTheMaximum
OverThePausesThatHappened` (only written slots count).

### 19e. Commands and results

- `make check`: **exit 0**.
- `go test ./internal/api ./internal/app ./internal/metrics -count=1`: **ok (217.7 s,
  1.7 s, 0.4 s)**.
- `GOOS=windows GOARCH=amd64|arm64 go vet ./internal/metrics`: clean.

### 19f. What it does not prove

- The Windows counters are compiled, not executed: no Windows host here. Reading them is
  host qualification.
- The scan measurement is on this host and filesystem; the target population is a
  projection from 20,000 files, not a measurement at 200,000.
- `diagnostics.go` and the admin overview still take a live `directorySize`; they are
  explicit operator actions rather than a periodic scrape, and the metrics endpoint is
  the surface F-12 named.

## Tranche 20 — W-22: three corrections to the record

Source commit before: `1faeb92` (tree clean). Source: `65935f3` (W-22's own commit; it
edits only ledger and release prose).

### 20a. The three stale entries

- **The status index was stale.** The index at the top still declared W-02 onward open
  and pinned the head to a post-tranche-16 commit, although tranches 17–19 closed W-02
  (F-10), W-03 (F-11) and W-04 (F-12). It now names the head after tranche 19 (`1faeb92`),
  records W-01 through W-04 as closed with the tranche that closed each, marks W-05 and W-06
  as the two red engine items, states that W-07 onward are open, and says the index is
  refreshed at every tranche.
- **A garbled table row.** Tranche 17's export-load table labelled its Go heap row with a
  mangled prefix glued onto the front; it now reads plainly "Go heap peak". The figure
  (89 MiB) is unchanged and was never in question.
- **The alpha.9 record kept two settled questions open.** `docs/releases/0.4.0-alpha.9.md`,
  "The path from here", still offered the 250-account profile and the VP8 fallback as the
  owner's open decisions. Both were settled on 2 October (VP8 accepted; the 250-account
  profile deferred for the 100-account v1 bound), and the addendum above already says so.
  The section now lists only the remote and the gate flip as the owner's remaining
  decisions and cites the addendum.

### 20b. Verification

- The stale index phrase, the garbled row label and the two settled-decision phrases were
  each searched for across `planning/` and `docs/`: **0 matches each** (one each before this
  tranche). The search terms are deliberately not repeated in this entry, so that the entry
  does not match them itself.
- The replacement text is present: the index records **W-01 through W-04 as closed**, and
  the export-load table's row now reads **Go heap peak**.
- The two red engine projects were re-measured against a binary built from this tree
  (`dist/brrpbrrp-e2e`), so the index's red claims are current rather than inherited:
  Firefox `voice-firefox` **13 / 1 / 0 / 0** (2.3 min; the failure is the inline assertion
  at `web/e2e/voice.spec.mjs:732`, W-05) and WebKit `release-webkit` **10 / 6 / 0 / 0**
  (2.3 min; the six are the release journeys run over the Chromium fixture, W-06). No
  leftover server process remained after either run.
- `git diff --stat`: two prose files changed; no code, test, configuration or artifact
  touched.

### 20c. What it does not prove

- W-22 is a record correction: it changes no behaviour, so there is no failing-before fix
  to demonstrate and no test to add. The proof is that the stale strings are gone and the
  replacement text is present.
- It makes the index accurate as of tranche 19, not complete: the two red engine items
  (W-05, W-06) and W-07 onward remain open until their own tranches.
- Committed as `65935f3`; the checkpoint bundle at HEAD covers it.

## Tranche 21 — W-05: the Firefox assertion, and the sign-in that never happened

Source commit before: `cd77c5f` (tree clean). Source: `496e90a`.

**Run, not written-only — but by the session that preceded this one**, before the owner
suspended test runs. Every figure below is that session's report; this tranche re-measured
nothing, and the entry says so where it matters.

### 21a. The inline assertion, and why only Firefox failed it

- **What was wrong.** `web/e2e/voice.spec.mjs` read `node.playsInline` from the shared
  screen's `<video>` and asserted it was `true`. Firefox does not define that IDL property,
  so the value was `undefined` and the journey could only ever fail under that engine. The
  markup has carried `playsinline` all along (`web/src/lib/components/ScreenStage.svelte:78`,
  `web/src/lib/components/MessageItem.svelte:293`) and `web/tests/screen-receive.test.mjs:85`
  already asserts it, so this was a test-assertion defect and never a product one.
- **The change.** The assertion now checks `node.hasAttribute('playsinline')` — the markup's
  contract, and the same thing the unit test checks — and additionally asserts the reflected
  boolean wherever `typeof node.playsInline === 'boolean'`. That is strictly stronger than
  the old assertion on the engines that define the property, and correct on the one that
  does not.

### 21b. The intermittent was a skipped sign-in, not a lost session

- **What the record said.** Ledger §11c: roughly one Firefox invocation in three failed at
  `signIn` with `getByLabel('Your preferences')` never appearing, the page left on the
  sign-in form, and a probe from that page showing **`/api/state` answering 401** after a
  sign-in that had returned 200 and set the cookie. It read as a session the server had lost.
- **What it was.** `signIn` navigated and then sampled the sign-in heading's visibility
  *once*, at the instant `goto` resolved. The app's boot is asynchronous: it renders
  "Opening your space…" until `/setup/status` and then `/state` have answered
  (`web/src/lib/workspace.svelte.ts:71` — `loading = $state(true)` — with `start()` at 575
  and `enter()` at 596), which is later than the load event. A `false` from that one sample
  was read as "this account is already signed in", so the form was never filled, no sign-in
  was ever posted, and the twenty-second wait expired on a page that honestly answered 401
  because **nobody had signed in in that run**. The 401 in the dump was evidence of the
  skipped sign-in, not of a lost session; the earlier "the POST returned 200 and set the
  cookie" measurement came from a different run.
- **Measured, by the session that made the change.** In 30 fresh Firefox contexts the
  surface was absent at the instant `goto` resolved **3 times**, needing 36–153 ms more to
  appear (median 45 ms). A slowed boot makes the race deterministic: with `/api/state`
  delayed 400 ms through the fixture's own route interception, the pre-fix helper failed
  **0 of 3** attempts — each after about 20 s, at the same `toBeVisible` on `Your
  preferences` — and the fixed helper passed **3 of 3** in about 2 s.
- **The change.** `web/e2e/support.mjs` gains `waitForSurface(page, workspaceReady)`: it
  polls until the app has settled on the sign-in form or the workspace before the caller
  decides which it is, and takes the locator that proves the workspace up (the preferences
  control on a desktop, a channel heading at phone width where the header is collapsed).
  `signIn` uses it, and `web/e2e/release.spec.mjs`'s phone-viewport journey — which carried
  the same one-shot sample inline — now uses it too.
- **Global timeout.** `web/playwright.config.mjs` gains a top-level `globalTimeout` of twenty
  minutes, about four times the longest measured invocation, overridable by
  `--global-timeout` on the command line (which wins over the config) or by
  `BRRP_E2E_GLOBAL_TIMEOUT`. Playwright has no per-project `globalTimeout`, so this one
  number is what gives every engine project a ceiling; before it, a hung run held the
  harness until a person noticed.
- **The failure dump.** `voice.spec.mjs`'s `afterEach` already printed every page with a
  `/api/state` probe. It now also prints, per browser context, the cookie names the context
  holds, the session cookie's length/`Secure`/`SameSite`/`HttpOnly`, and the status the
  server returns when the harness sends that exact cookie back. That is the probe ledger
  §11c asked the next session to make: it distinguishes "no session was ever created" from
  "the browser is not sending the cookie it holds".

### 21c. What it reported, and what it does not prove

- **Reported by the session that made the change, not re-measured here.** Twenty consecutive
  `voice-firefox` invocations, every one **14 passed / 0 failed / 0 skipped / 0 did not
  run**, 81–105 s each; Chromium default **30 / 0 / 0 / 0** (3.3 min) and private **45 / 0 /
  0 / 0** (4.7 min) afterwards with nothing excluded; `npm --prefix web test` 366 / 0 / 0 / 0;
  Prettier clean. The twenty runs were made against the helper as first written; the only
  later edit to those files was comment re-wrapping, so the executable lines the runs
  exercised are the committed ones.
- **It does not prove** the race is gone on other hardware: the fix removes a single-sample
  read, and every figure above is this host's timing. It does not cover WebKit — that is
  tranche 22 — and a green Firefox run says nothing about the assertions the journey makes
  when the media itself misbehaves.

## Tranche 22 — W-06: the WebKit pass gets its own installation

Source commit before: `496e90a` (tree clean). Source: `e0c6280`.

**Written, not run.** The owner suspended test runs before this tranche, so the change is
judged by reading and by the static gates; `planning/verification-queue.md` carries what
must confirm it.

### 22a. Six failures with one cause

- **What was measured before this tranche**, by the session that preceded it, against
  `dist/brrpbrrp-e2e` built from this tree: `BRRP_E2E_WEBKIT=1 … --project=release-webkit` →
  **10 passed / 6 failed / 0 skipped / 0 did not run** in 1.2 min. The Chromium `release`
  project ran its nine journeys first and green; then the WebKit project ran its own seven,
  six of which failed:
  - `first setup` — `ENOENT …/setup-token`, because the Chromium pass had consumed it.
  - `a member requests their data export`, `Back and Forward`, `Arabic message` — `409` from
    `createAccount`; `export_member`, `walk_member` and `arabic_member` already existed.
  - `message permalink` — `browserContext.grantPermissions: Unknown permission:
    clipboard-write`.
  - `phone viewport` — `getByText('Sending…')` not found (see 22c).
- **The cause of the first five.** The project declared `dependencies: ['release']`, and a
  Playwright dependency runs that project first **against the one server this invocation
  starts** (`web/e2e/run.mjs` gives each invocation one data directory). One installation
  serves one setup, so an engine whose subset includes the setup journey cannot share an
  installation with another engine's pass. The dependency's mission is to produce a fixture;
  here it consumed the fixture.
- **The change.** The dependency is removed, so `--project=release-webkit` runs the WebKit
  subset and nothing else, against its own server and data directory; and `run.mjs` throws
  with the exact invocation to use when `BRRP_E2E_WEBKIT=1` is set without a `--project`,
  because that invocation would run the Chromium projects beside it and fail the same way.
  A runner that forgets now gets one sentence instead of six unrelated failures.

### 22b. The clipboard permission belongs to the engine

- `grantPermissions(['clipboard-read', 'clipboard-write'])` threw before the permalink
  journey could begin, because Playwright's WebKit has no permission by that name. The
  journey's subject is the permalink, not the clipboard, so the grant is now attempted and
  remembered, and each branch asserts the outcome that engine can produce: with the
  clipboard, the toast says the link was copied and `navigator.clipboard.readText()` must
  hold a URL containing `#message=`; without it, the product's own answer to a refused
  clipboard — `MessageItem.svelte`'s `copyLink` catch, which prints `Copy this link: <link>`
  rather than claiming a copy — is asserted, and that printed permalink is what the
  navigation below follows. It is deliberately one outcome per branch, not either outcome:
  the comment says which line reports an engine that writes with no permission to grant.

### 22c. The phone-viewport failure is still unexplained, and now it will name itself

- The WebKit pass failed at `await expect(page.getByText('Sending…')).toBeVisible()` with
  "element(s) not found", while the send was held 1.5 s by the journey's own route handler.
  Two causes fit and a screenshot cannot separate them: the composer's Enter never submitted
  (so no request existed at all), or the request was held and the optimistic pending row
  never rendered. The journey now counts the POSTs its route handler intercepts and asserts
  that at least one arrived *before* it asserts the row, so the first verification pass will
  say which half failed. No hypothesis is recorded here as a diagnosis.

### 22d. What this tranche does not prove

- Nothing in it has been run. The static gates are clean — `make check` exit 0 after the
  change, `node --check` and Prettier on the three files — which says the files parse, the
  frontend still builds and the audits still pass. It does not say the WebKit pass is green.
- The WebKit subset is still the *core* journeys only and its exclusions are unchanged: the
  journeys that drive offline/recovery and the media/setup counts are not in it. Playwright's
  WebKit is not Safari on a device, and this pass does not claim to be.
