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
