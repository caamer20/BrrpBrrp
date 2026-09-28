# Standalone handoff — complete the remaining P10 implementation

Prepared 28 September 2026. Copy **everything below the separator** into the next coding session. This prompt supplies the project and execution context for an agent with repository access and no previous chat. It supersedes the review-10 prompt as the current assignment while retaining P10's work-package numbering.

---

## 1. Your assignment and the stopping point

Continue **BrrpBrrp**, an open-source, self-hosted community chat app intended to become a user-friendly Discord replacement. Dependable messaging, consent, understandable permissions, recoverable work, accessible desktop/mobile use and simple installation/operation are core product goals.

**Complete the remaining P10 implementation.** The last coder repaired review-10 defects in P9, but did not implement P10's pending-introduction reporting or its distribution deliverables. Their completion report is background evidence, not this task's completion checklist.

Required outcome:

1. Close the remaining reviewer busy-state/receipt-recovery gaps while preserving the existing repairs.
2. Add recipient-only **Report request and block**, without accepting the request first.
3. Make evidence, request rejection/clearing, block, audit and command receipt atomic and safe to retry.
4. Add the real recipient/reviewer UI, migration and recovery regressions.
5. Produce clean, attributable native/source distribution artifacts and execute their consumers.

**A repair-only checkpoint is not completion of this assignment.** Do not stop after rechecking old fixes, writing another plan or rerunning existing suites. Do not start full DM push/activity, groups, named threads or media expansion before P10 is finished. No v1 declaration, public publication or production feature-gate approval is implied.

Use focused checks while developing, then one final integrated verification on frozen implementation/package inputs. Do not inherit the prior agent's goal, unlimited token/time allowance or repeated-run strategy. Evidence-only commits do not require repeating a full 17-minute race suite when the tested application and fixture inputs did not change.

## 2. Starting state and documents

Known workspace: `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp`; use the actual checkout if moved. If you only have this prompt, request the repository/source archive once. No configured public remote or credentials existed at review time; do not invent a URL or assume another agent's access.

- Reviewed HEAD: **`ba8921886c56bfb0b3c947f297fefd0051947e2f`**.
- Branch: **`codex/p9-private-reporting`**.
- Application: **`0.4.0-alpha.7`**, schema **13**, backup format **2**.
- The tree was clean at review start. The review-11 report and this prompt may now be uncommitted planning additions; preserve them and any subsequent user changes.
- Latest local executable: `dist/brrpbrrp-p9`, SHA-256 **`051b7f3d39e416c776f8b294104299fc8b4982e38606687f794706ba5cb23ae5`**.
- Embedded build revision: **`34bddbf4733969c58421dff985d74a876f8538b3`**, **`vcs.modified=false`**. Later changes through HEAD affect the browser journey and evidence ledger, not application inputs.
- Frontend identity: **`20d76bada950286f`**.
- No alpha.7 distribution archives exist. Alpha.6 native/source archives remain prior packaged artifacts; preserve them.

Inspect current Git status/history first. Read:

1. `planning/completion-review-11-and-next-steps-2026-09-28.md` — latest assessment and remaining findings.
2. `planning/p9-checkpoint/verification.md`, especially its appended review-10 repair/rerun sections; see the evidence qualifications below.
3. `docs/adr/0013-private-message-evidence-and-request-discovery.md` and ADRs 0007–0012 for durability, backup, authority, receipts/outbox, account lifecycle and consent.
4. `docs/releases/0.4.0-alpha.7.md`, `docs/architecture.md`, relevant WP-10–15 and release gates in `docs/v1-implementation-plan.md`.
5. `docs/v1-progress.md`, with caution: its current header/rows still contain stale alpha.6/schema12 statements that this task must reconcile.

The review-10 prompt remains useful history, but its already repaired items are not a new list to implement again. New endpoints/fields proposed below do not yet exist.

## 3. What already works

The production app is one Go executable with embedded Svelte/TypeScript assets and SQLite WAL storage. Node is used for builds/tests, not as a separate application server. The database has one connection. WebSockets use bounded delivery queues. Opus/Pion SFU voice is embedded. Native setup/service and Docker paths exist.

Community functionality includes persistent messages/files/replies/search/pins/reactions/mentions, responsive UI/themes/PWA, MFA and session recovery, ownership, additive roles/delegation, restricted channels, slow mode, community reports and per-channel mention muting.

Direct conversations implement canonical requests, explicit acceptance, durable request limits, contact preferences, blocking, personal hiding, shared messaging/files/search/read state and recoverable sends. P9 added selected accepted-message text reports, a dedicated reviewer capability and queue, private review audit, and outstanding incoming-request discovery.

The important R10 repairs are present:

- Bounded SHA-256 preview versions support full-length Unicode messages.
- General audit excludes private review events before pagination; a separate capability/feature-gated audit endpoint exists.
- Structured409 error details survive; changed text is shown and explicitly acknowledged before resubmission.
- Reviewer loads capture current request identity; feature-disabled private tabs are hidden.
- Requester incoming-contact opt-out no longer hides an actionable request they sent.
- Linux shell execution audit runs in the Linux browser job, preserving the Windows application matrix.
- Reporter identity/backups/uncertainty/capacity wording and exact recovery comparisons have improved.

Current notification behavior is **incoming request badge plus existing DM unread state**. There is no new DM OS notification, push, sound or ringing. Groups, private calls, screen sharing, complete named threads and general activity inbox remain absent.

Both **Windows native service/Tailscale and Linux Docker** are required deployment qualifications. MIT/CLA decisions are already recorded. The operator controls stored data, backups and media; the app does not promise end-to-end encryption. Ordinary application APIs still deny unrelated owner/admin DM browsing.

Keep `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS` **default-off**. Use opted-in disposable servers for tests. Preserve current supported limits rather than promoting proposed capacity targets without measurement.

## 4. Architecture map and rules

| Area | Existing files / entry points |
| --- | --- |
| Runtime/installation | `cmd/brrpbrrp`, `internal/app`, `internal/installer` |
| Conversation storage | `internal/db/migrations.go`, `migration11.sql`–`migration13.sql`, `receipts.go` |
| Requests/blocks | `internal/api/direct.go`: `createDirectConversation`, `rejectDirectRequest`, `commitDirectChange`, `signalDirectChange`; `contacts.go`: `changeContactBlock`, pending cleanup |
| Private evidence | `internal/api/private_reports.go`: bounded preview, report receipt, quota/capacity, list/review/expiry helpers; `private_evidence_test.go`, `private_evidence_helpers_test.go` |
| Scope/policy | `internal/api/conversation_access.go`, `router.go`, `internal/authz/authz.go` |
| Restricted audit | `internal/api/admin.go`: general audit filter and `privateEvidenceAuditLogs`; `/api/admin/private-reports/audit` |
| UI/request identity | `web/src/lib/workspace.svelte.ts`, `api.ts`, `outbox.ts`, `App.svelte` |
| Relevant components | `MessagesInbox.svelte`, `ReportPrivateMessage.svelte`, `admin/PrivateEvidence.svelte`, `AdminPanel.svelte`, `Workspace.svelte`, client `types.ts`/`permissions.ts` |
| Rendered/state tests | `web/e2e/directs.spec.mjs`, `privacy.spec.mjs`, `account-switch.spec.mjs`, `support.mjs`; `web/tests/workspace-state.test.mjs`, `api-error-detail.test.mjs` |
| Recovery/artifacts | `scripts/upgrade-recovery.mjs`, `upgrade-recovery-private.mjs`, `upgrade-rollback.mjs`, `release.go`, `check-package.mjs`, `check-source-package.mjs` |
| CI/baselines | `.github/workflows/check.yml`, `docker.yml`; `check-browser-steps.mjs`, `fetch-baseline-release.mjs`, `baseline-releases.json` |

Existing message-report routes are `GET /api/messages/{id}/private-report-preview`, `POST /api/messages/{id}/private-report`, and the private reviewer list/update/audit endpoints. There are no introduction-report routes, immutable request attempts or combined-action receipts yet.

Preserve these invariants:

1. **Connection before authority fence.** Inside a transaction use `tx`, never reacquire the single connection through `s.store`. Reauthorize account/session/capability transactionally. Commit authority changes and delivery invalidation through the existing protocol; network/stream/voice follow-ups occur outside the fence.
2. **One message pipeline.** Schema11 already preserves IDs/rowids/FKs/FTS while requiring conversation scope. Channel conversation IDs equal channel IDs; private messages have no channel projection. No shadow channels or second private message stack.
3. **Explicit consent.** Crossed requests do not imply acceptance; only the recipient accepts. Pending recipients lack accepted-history access. Owner/admin/reviewer status is never a substitute for participant or recipient authorization.
4. **Blocking/hiding.** Blocked accepted history remains readable and reportable; new contact writes stop, own deletion remains. Unblock never replays refused operations. Hide preferences change only by explicit personal action.
5. **Receipts/drafts.** `(account,key)` identifies a send and its intent binds the destination. Acknowledged replay has no new effects; deleted/expired keys cannot resurrect. Preserve opt-in account-scoped outbox bounds (50 records/64 KiB UTF-8), 24-hour automatic retry, explicit text recovery and monotonic read state.
6. **Evidence-only review.** `report.review_private` admits only submitted evidence and its restricted audit. No original conversation/history/context/search/files or membership. General community reports keep their separate read-plus-`message.delete_any` policy.
7. **Disclosure/lifecycle.** Evidence reveals the author, reporter and explanation as disclosed. Availability expires 90 days from creation; resolve/reopen cannot extend it. Physical cleanup/backups are separate. Tombstoned removal retains a Deleted-member identity; physical erasure clears references. Snapshot restoration restores snapshot grants, so later security changes need reconciliation.
8. **Resource bounds.** Introductions ≤500 Unicode characters; durable request limits5 outgoing/20 incoming pending, 10 new pairs/day and24-hour rejection cooldown. Private evidence currently shares ten submissions in a fixed one-hour account window and 5,000 unexpired installation rows; introducing a new endpoint must not multiply allowances.

## 5. Finish P10-01 inside this implementation

### Reviewer busy-state race (R11-01)

`PrivateEvidence.review()` increments a shared `version`, sets `busy`, then awaits PATCH. Refresh/Load older/filter or authority refresh calls `load()`, incrementing that same version. The old mutation's `finally` fails admission and never clears `busy`; future review actions immediately return.

This is source-confirmed, not yet browser-reproduced. Add a rendered deterministic regression: hold a real PATCH, refresh/change filter or trigger a same-account authority update, release it, then review another record without closing the panel. Separate mutation ownership/cleanup from list sequencing so an invalidated completion cannot patch the wrong view or permanently hold a busy flag. Include allowed refresh, permission loss/regrant, account switch and failure controls. Do not weaken stale-result guards to make cleanup work.

### Own-outcome recovery (R11-02)

Ordinary reporters cannot access the reviewer queue. Current uncertainty guidance suggests a report list they cannot open. Provide a narrow own-receipt Check outcome/Retry operation as part of the command receipt design below, and use accurate available guidance for message-report uncertainty. Never grant review capability or expose the evidence queue merely to reconcile a submission.

### Restricted audit and documentation

Define audit metadata retention separately from 90-day evidence, including expiry/account-erasure/backups; current access filtering is correct but retention is not explicit. Add focused audit pagination, feature-off, revocation and audit-write failure rollback controls while integrating request evidence.

Update stale `docs/v1-progress.md` headers, relevant rows and next actions to alpha.7/schema13 and actual unfinished P10. Preserve dated historical outcomes; do not silently relabel an old checkpoint. Finish these local items, then continue the feature work—closing them alone is not the assignment.

## 6. P10-02: introduction-report contract and migration

Write an ADR defining exact schema/API shapes before implementation. New member action: **Report request and block**, beside ordinary Block. Existing accepted-message reporting remains report-only. A second report-only action for pending introductions is outside this bounded scope.

Only the authenticated active **recipient of the exact current pending attempt** can preview/confirm. The sender, unrelated member, nonrecipient owner and reviewer are denied without an existence oracle. The action never accepts contact, creates a message, grants membership or enables files/history.

Evidence is the server-held introduction, sender/reporter identities, necessary source-kind/time context and explanation—nothing else. Show exact text, identities, reviewer audience, expiry/backups and the combined blocking effect before confirmation. Define empty-introduction behavior explicitly. Never fabricate missing text or import cached browser text after ordinary reject/block/withdraw/account cleanup erased the source.

### A canonical pair is not a request attempt

`direct_requests` reuses a row when a pair requests again. Add a durable immutable attempt identifier or rigorous generation scheme, rotating on each new attempt and backfilling existing pending rows. Bind preview and operation to source kind, exact attempt, sender, recipient and authoritative text/state. Reuse the bounded preview digest with proper source separation.

The next unused migration is currently 14; inspect actual registry first. Preserve every schema 13 evidence ID, original text/reason/review state/expiry and existing community report behavior. Add an explicit evidence source discriminator/request reference; do not use a fake message ID, create a synthetic message or build another moderation system. Choose a schema extension/rebuild or related source table deliberately, maintaining one reviewer experience and shared policy/limits/cleanup.

Document how active request identity, evidence provenance and command receipts survive account removal, restoration, evidence expiry and renewed contact. Define which internal identifiers are necessary to store and which must be omitted from outward reviewer DTOs.

## 7. P10-03: atomic backend operation and durable receipts

Use transaction-level helpers, not HTTP-handler-to-handler calls. In one transaction:

1. Re-establish authentication, private feature policy and account ownership of the operation.
2. Resolve any already committed receipt using the rule below.
3. For new work, resolve the exact pending attempt/recipient and compare the confirmed preview with current server state.
4. Apply shared private evidence rate/capacity/input bounds.
5. Save immutable introduction evidence and command receipt.
6. Insert the block, reject the request and clear its introduction.
7. Write required restricted audit and commit through `commitDirectChange`/authority fencing.
8. Publish contact/account invalidations only after commit; run slow work outside the fence.

If an evidence, block/rejection, audit or commit step fails, all combined side effects roll back. No report, quota consumption, block or rejection may be partially acknowledged. Ordinary Block remains available when reporting is full/rate-limited/unavailable, with clear UI explaining that the combined action did not complete.

If accept/withdraw/block/account removal wins first, refuse stale confirmation without copying client text. If report-and-block wins first, later acceptance must fail. Preserve unrelated conversations and users.

### Receipt contract — not “one currently open report”

Successful report-and-block deliberately clears the live source and changes authority. The current report deduplication by open status cannot supply its retry semantics.

Use a stable account-owned operation key with source attempt and intent (including explanation/action) bound to it. Check a known committed receipt after current authentication/feature policy but **before requiring a still-pending source**. A different intent under the same key conflicts; another account cannot retrieve the receipt.

Required outcomes:

- Lost-response/concurrent retry returns the original minimal receipt without a second evidence record, quota charge or repeated block.
- Resolve/reopen does not release the operation identity.
- A later deliberate unblock remains effective; replay must not block again.
- An old preview/operation never affects a later request in the same pair.
- First success retains original evidence/explanation; retries cannot overwrite them.
- Expiry/cleanup never makes an old action new work or exposes expired text through a receipt. Define minimal guard/receipt retention explicitly.
- Return the historical operation outcome without asserting that the person is **currently** blocked; current status comes from fresh state.

Expose only a minimal own-outcome reconciliation path. Neither receipt checks nor command replies return reviewer notes, other reporters, evidence bodies or unrestricted review data.

## 8. P10-04: recipient and reviewer user experience

Add the new action to a current incoming request in `MessagesInbox.svelte`. Reuse corrected disclosure/preview patterns from `ReportPrivateMessage.svelte`, but do not require accepted-conversation read access for a pending recipient.

Flow: open request → choose Report request and block → read authoritative introduction and disclosure → enter reason → explicitly confirm → show confirmed or accurately uncertain outcome. Ordinary Block remains separate. With source changes, refresh the preview and require fresh consent; keep same-account explanation. With source deletion, explain unavailability; do not reconstruct evidence.

Keep these state lifetimes separate:

- Restricted preview: current authority/request-attempt fenced.
- Explanation and operation identity: account/session owned.
- Receipt/outcome: reconciliation for the original account and operation.
- Current block/request state: freshly fetched, not inferred from a historical receipt.

The operation's own authority invalidation can arrive before its HTTP response. That is expected, not proof of failure. Drop stale private preview data, preserve appropriate same-account recovery text, and reconcile with the **same operation**. Never silently replace it with another report or repeat a block. Account switching clears authored text and refuses late results.

Extend the existing reviewer queue with clear Message/Request introduction source labels and no original-conversation navigation. Add a real reviewer browser journey: current P9 journeys mostly exercise report submission, not reviewer behavior. Check keyboard focus, narrow layouts, loading/empty/errors, capacity/rate refusal and accurate receipt status.

## 9. P10-05: acceptance and recovery matrix

Use permanent behavior-level tests and positive controls. Expected critical cases:

| Area | Required acceptance |
| --- | --- |
| Ownership | Recipient-only preview/submit; sender/outsider/nonrecipient owner/reviewer denied; no acceptance/history/files/membership |
| Preview | Exact server text and disclosed identities; Unicode/empty bounds; forged input, old attempt and changed text refused |
| Atomicity | Inject evidence/block/rejection/audit/commit failures; no partial report/block/quota/receipt effects |
| Races | Accept/withdraw/block/removal/renewed request; deterministic winning mutation and no stale evidence capture |
| Receipts | Lost response, concurrent retry, restart, resolved/reopened report, later unblock/new attempt, changed intent and other-account access |
| UI | Own authority change before HTTP completion, uncertain outcome reconciliation after source clearing, account switch, preserved explanation, no automatic re-consent |
| Reviewer | Real rendered typed evidence, original DM denial, overlapping refresh/PATCH busy cleanup, permission withdrawal/regrant, pagination and audit controls |
| Resources | One shared ten/hour quota and 5,000-unexpired-row cap; no allowance consumed on failed combined action; actionable capacity refusal |
| Gate/lifecycle | Default-off surfaces hidden/refused; unchanged 90-day expiry; tombstone versus erasure; restricted audit retention |
| Recovery | Exact message/request evidence and expiry, attempt identity, receipt, block/rejected state and request counts survive; old sessions/devices removed; post-snapshot security changes reconciled |

Extend `private_evidence_test.go`, direct/contact/authority tests and relevant workspace tests. Node state tests do not render Svelte effects; use Playwright for recipient and reviewer UI. Use held real requests and transaction barriers, not arbitrary sleeps or mocks that bypass actual mutation behavior.

Extend the maintained private restore fixture using real APIs. Prove pre-backup state exists, capture exact text/reason/expiry/source/receipt values, restore into an empty isolated destination and assert equality plus current permitted/denied access. Test report-and-block replay after restore and after later unblock. Include post-snapshot reviewer revocation/contact changes that require explicit reconciliation before traffic cutover.

Use the preserved real alpha.7 executable as a schema 13 upgrade baseline for the new schema, and retain the maintained alpha.5/schema 10 and alpha.3/schema 2 archive paths. Do not rebuild an old baseline from current source, or claim a schema-only fixture is an actual archived-version rollback. Preserve required FK/rowid/FTS/data invariants and failure/reopen recovery.

## 10. P10-06: actual distribution checkpoint and trustworthy evidence

The missing distribution work is part of this assignment. Finish application/schema/package inputs/shipped docs, create coherent commits when permitted, and build from one clean source revision. Preserve existing changes and default to a `codex/` branch prefix if creating a branch.

Choose the next unused coherent prerelease identity (expected alpha.8 if still unused) for new native/source artifacts. Do not overwrite alpha.6/alpha.7 historical bytes or label v1. Keep Go/frontend/lockfile/release helper identities and the version test aligned. Schema increments follow actual migrations; backup format need not change solely because schema does.

Build the native and source archives, record source commit/dirty flag, frontend identity, executable hashes and every archive digest. Cross-building Windows is not a Windows runtime check. Execute host-matching extracted-native smoke and clean source extraction → dependency install → frontend/Go build → consumer smoke. Exercise browser/recovery/upgrade against the identified packaged binary; package linker flags may change bytes relative to an ad-hoc build.

Store fresh machine-readable reports under a **new checkpoint directory**. Include command, exit code, build/source revision, fixture revision, binary hash, feature mode, test count/duration and artifact identities. Do not overwrite historical P9 JSON or imply they describe new bytes.

Important inherited evidence caveat: the appended ledger reports reruns of binary `051b7f3d…`, but all four committed P9 recovery/rollback JSON still identify original binary `fddd1e3f…` / commit `2869db5`. The latest executable is clean `34bddbf`; `f329993` is a test/evidence revision, not its embedded build revision. Correct attribution in a dated note; this is not proof the reruns did not happen.

Freeze application and test inputs, run the final baseline once, then add evidence without rebuilding solely to include it in the artifacts it describes. A documentation-only evidence commit does not invalidate an unchanged tested binary. If actual application/package inputs change after testing, rerun the affected gates on the new identities.

`dist/`, `.tools/`, caches and temporary probes are ignored local material, not part of a fresh CI checkout. Pinned baselines are specified in `scripts/baseline-releases.json`; the fetch helper accepts an approved local directory or HTTPS base. Hosted rollback still needs real `vars.BRRP_BASELINE_RELEASE_URL`; no remote was configured. Missing/wrong baselines must fail explicitly, never skip or become new-source stand-ins.

## 11. Toolchain and commands

Known local tools: `.tools/go/bin/go` (Go 1.27.1), Node 24.2.0 and Playwright Chromium. On a fresh checkout provision supported tools, run `npm --prefix web ci` as needed, and install Chromium from `web` with `npx playwright install chromium`. Build `web/dist` before Go because assets are embedded.

Inspect `go env GOMODCACHE GOCACHE`. This workspace's populated module cache is `/Users/cameronamer/go/pkg/mod`; old temporary caches were incomplete. `GOPROXY=off` is appropriate only when dependencies are present. Use writable build cache paths and distinguish dependency setup failure from a product test failure.

Example final integration from the repository root; adapt paths:

```sh
export BRRP_GO="$PWD/.tools/go/bin/go"
export GOCACHE=/private/tmp/brrpbrrp-go-build
export PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
export BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-p10-checkpoint"

npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
"$BRRP_GO" vet ./...
"$BRRP_GO" test -race -count=1 -timeout 25m ./...
mkdir -p dist
CGO_ENABLED=0 "$BRRP_GO" build -trimpath -ldflags='-s -w' -o "$BRRP_E2E_BINARY" ./cmd/brrpbrrp
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e
BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e
node scripts/check-browser-steps.mjs --self-test
node scripts/check-browser-steps.mjs
node scripts/check-docker-inputs.mjs --self-test
node scripts/check-docker-inputs.mjs
node scripts/check-source-package.mjs --self-test
node scripts/check-source-package.mjs
node scripts/licenses.mjs --check
node scripts/fetch-baseline-release.mjs --self-test
git diff --check
```

The shell audit belongs on Unix/Linux; preserve the Windows application matrix. The wrapper creates disposable server data and strips ambient `BRRP_` configuration; select modes with `BRRP_E2E_PRIVATE_CONVERSATIONS` and check selected projects. Do not change production defaults/login budgets for tests. Local socket binding may require the environment's execution approval.

Recovery/consumer entry points, replacing placeholders with actual absolute paths:

```text
node scripts/upgrade-recovery.mjs --binary CANDIDATE --report NEW_REPORT
node scripts/upgrade-recovery-private.mjs --binary CANDIDATE --report NEW_PRIVATE_REPORT
node scripts/upgrade-rollback.mjs --binary CANDIDATE --old-binary PRESERVED_ALPHA7 --report NEW_ALPHA7_REPORT
node scripts/upgrade-rollback.mjs --binary CANDIDATE --old-release PINNED_OLD_ZIP --report NEW_OLD_BASELINE_REPORT
node scripts/check-package.mjs EXTRACTED_NATIVE_BINARY
```

Fresh review 11 results: frontend 114/114, Svelte 0 errors/warnings, local browser-step audits and targeted Unicode/audit/badge Go regressions passed (API 2.763s). Inherited appended results: ten-package full race (API 1032.994s), browser 13/13 and 19/19, recovery 23/23 and 85/85, rollback 57/57 and 55/55. Do not report these as your own reruns.

One RTC connection test failed earlier and later runs passed; RTC code is unchanged since alpha.5. Preserve that record. “Under load” is a hypothesis, not established root cause. If it recurs, retain diagnostics and analyze the concrete failure; do not repeatedly run the entire suite until the failure disappears or weaken the assertion. Current full race runtime is about 17 minutes; allow the planned timeout and avoid restarting a still-running command.

## 12. External work and the remaining v1 path

No actual hosted CI, Windows/Tailscale lifecycle, Docker runtime, encrypted off-host restore, physical media, measured RPO/RTO, delivered alerts, capacity/soak, independent security/accessibility review, beta or release approval has been established. All G0–G8 gates remain open.

Unavailable credentials/hosts do not block the local P10 feature or packaging work. Prepare exact artifacts/commands/expected outcomes for external execution and report the missing input specifically. Do not repeatedly ask whether both deployments or MIT/CLA are wanted; those choices are already recorded.

After P10, proceed through separate bounded milestones:

1. **DM activity/notifications:** authoritative inbox without push, stable activity identity/retention/acknowledgement, account/conversation/device preferences, safe previews, active/background/closed-PWA behavior, deduplication and dispatch-time consent/block/access checks. Keep request count, message unread, activity acknowledgement and OS delivery distinct. Do not reuse channel push with an empty channel ID.
2. **Consenting groups and lifecycle/export:** invitations, limits, owner transfer/leave/removal, disclosed history, shared-group blocks and restore safety.
3. **Threads and community UX:** stable named/followed/read/archived/locked threads, full bounded 1,000-reply traversal, moderation/timeouts/invitations, composer/upload/navigation and human accessibility.
4. **Media:** measured architecture spike, then private calls and screen/audio publications with explicit capture/stop/admission/generation and real browser/device/NAT/relay qualification.
5. **Operations/open source in parallel:** repository/security intake/maintainers/signing identities, hosted CI, both-host install/reboot/update/rollback, encrypted independent recovery/config/TLS/key custody, received alerts and load/soak. Master recovery targets remain proposed RPO ≤24 h/RTO ≤60 min through acquisition/decryption/security reconciliation, not just database restore time.
6. **Release acceptance:** independent reviews, non-author install/restore, at least three communities across both deployments for 14 consecutive beta days, seven-day frozen distributed-candidate observation, explicit approval and staged publication/support handoff.

## 13. Working method and final acceptance checklist

Start with a short execution outline, then code. Resolve routine reversible choices independently. Parallelize backend, client and packaging work with explicit file ownership after agreeing schema/API contracts. Preserve existing user work and use small coherent changes. Do not initiate broad refactors or repeat general audits without a concrete need.

No public publication, live production migration, new unlimited budget or global feature enabling is authorized by this prompt. Ask only for genuinely missing decisions/authority and continue independent work while waiting.

Your completion table must explicitly account for **P10-01 through P10-06**:

- Remaining reviewer/own-outcome/audit-policy controls closed.
- New attempt identity and source-kind migration recorded and implemented.
- Recipient-only Report request and block backend and UI work end to end.
- Atomicity, renewal, unblock, concurrent/lost-response retry and account isolation proven.
- Exact new evidence/receipt/block restoration and old-version compatibility demonstrated.
- Actual clean native/source packages, extracted consumers and fresh attributable reports delivered.

Final response: changed areas and rationale, each package's status, regression before/after with positive controls, exact commands/exits/counts/durations/modes, source/build/fixture/frontend/artifact identities, report paths, unresolved external obligations and the next bounded feature milestone. Do not mark P10 complete if only repairs or tests are done, if introductions remain unreportable, or if distribution/consumer work is still missing. State any incomplete obligation plainly.
