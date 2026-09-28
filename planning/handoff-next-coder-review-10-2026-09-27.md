# Standalone next-coder prompt — P10 reliable private evidence and request reporting

Prepared 27 September 2026. Copy **everything below the separator** into the next coding session. This is a complete handoff for a coder with repository access and no previous conversation. It supersedes the P9 execution prompt while preserving its history and evidence.

---

## 1. Mission and assignment

Continue **BrrpBrrp**, a self-hosted, open-source community chat application intended to become a user-friendly Discord replacement. Reliability, consent, understandable permissions, recoverable user work, easy installation/operation, accessible desktop/mobile use and dependable media are product requirements.

**Implement and verify P10; do not stop after another planning-only review.** First repair the concrete defects in the new private evidence flows and CI. Then add **Report request and block**, allowing the recipient to report a pending introduction without accepting contact. Finish with a clean, attributable binary/archive/consumer checkpoint.

This is a bounded milestone. Do not start full DM push/activity, groups, named threads, private calls or screen sharing in this tranche. Do not interpret a previous agent's completed todo list as v1 approval or inherit their token/time allowance. Use focused reproductions during development and one integrated final verification after changes are ready.

## 2. Repository and authoritative starting point

Known workspace: `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp`; adapt to the actual checkout if moved. If you were given only this prompt, request the source checkout/archive once. No public remote URL or credentials were available at review time; do not invent them.

- Reviewed HEAD: **`89101e3e30d1984c495f7526f6c4283d5605387d`**.
- Branch: **`codex/p9-private-reporting`**.
- Tested clean implementation commit: **`2869db55394b2b2a004424672517b38d6908586b`**. HEAD adds five evidence files/updates relative to it.
- Current application: **`0.4.0-alpha.7`**; database **13**; backup format **2**.
- Working tree was clean at review start. The new review-10 report and this handoff may be uncommitted additions; preserve them and any subsequent user changes.

Check current Git status/HEAD first and reconcile changes rather than resetting to these hashes. Read:

1. `planning/completion-review-10-and-next-steps-2026-09-27.md` — latest overall assessment and R10 findings.
2. `planning/p9-checkpoint/verification.md` plus its recovery/rollback JSON reports — inherited results, qualified below.
3. `docs/adr/0013-private-message-evidence-and-request-discovery.md`, then ADRs 0007–0012 for durability, backups, authority, receipts/outbox, lifecycle and direct consent.
4. `docs/releases/0.4.0-alpha.7.md`, `docs/v1-implementation-plan.md`, `docs/architecture.md` and `docs/v1-progress.md`.

Some current summaries still say alpha.6/schema12 or that private reporting is absent. Those are stale, not instructions to remove the new feature. Fix the summaries as part of P10, with dated corrections to historical evidence rather than silently rewriting past results.

## 3. Product and architecture context

The app is one Go process with embedded Svelte/TypeScript assets, SQLite WAL storage and one database connection. Node is a build/test tool, not a separate production app service. WebSockets carry bounded queues; Opus/Pion SFU voice is embedded. Native service/setup and Linux Docker paths exist.

Community features already include messages, files, replies, pins, reactions, search, mentions, themes, PWA shell, responsive UI, MFA/session recovery, ownership, additive roles/delegation, restricted channels, slow mode, community selected-message reports and per-channel mention muting.

Direct conversations already provide canonical requests, explicit acceptance, durable request limits, contact opt-out, blocking, personal hiding, shared messages/files/search/replies/read state and recoverable sends. P9 added selected private-message text evidence, a dedicated reviewer capability/queue, a durable incoming-request count and account-owned cross-device notification-preference refresh.

**Keep `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS` default-off.** Test opted-in features only on disposable installations. The operator controls stored data, backups and media; no end-to-end encryption is promised. Application APIs still deny unrelated owner/admin access to private conversations.

Both **Windows native service/Tailscale and Linux Docker** are required qualification targets. MIT/CLA decisions are already recorded. Current limits include 100 live/reversibly deactivated accounts, 100 channels, 12 MB uploads and ten simultaneous voice participants serverwide. Do not raise these based on local unit tests.

### Code map

| Area | Files / symbols |
| --- | --- |
| Runtime/host | `cmd/brrpbrrp`, `internal/app`, `internal/installer` |
| Migration/data | `internal/db/migrations.go`, `migration11.sql`–`migration13.sql`, `receipts.go` |
| Private reports | `internal/api/private_reports.go`: preview/submission/review, `privateEvidenceForMessage`, `consumePrivateReportAllowance`, `checkPrivateEvidenceCapacity`, `reportReceiptFor` |
| Existing report tests | `internal/api/private_evidence_test.go`, `private_evidence_helpers_test.go`, community `reports_test.go`, `report_retention_test.go` |
| Direct state/block | `internal/api/direct.go`, `contacts.go`, `conversation_access.go`, `state.go`; `rejectDirectRequest`, `commitDirectChange`, `signalDirectChange` |
| Policy/audit | `internal/authz/authz.go`, `internal/api/router.go`, `admin.go`, `maintenance.go`; client `permissions.ts`, `types.ts` |
| Client request identity | `web/src/lib/workspace.svelte.ts`, `api.ts`, `outbox.ts`, `App.svelte` |
| Evidence/request UI | `components/ReportPrivateMessage.svelte`, `admin/PrivateEvidence.svelte`, `AdminPanel.svelte`, `MessagesInbox.svelte`, `Workspace.svelte`, `MessageItem.svelte` |
| Rendered/state tests | `web/e2e/directs.spec.mjs`, `privacy.spec.mjs`, `account-switch.spec.mjs`, `support.mjs`; `web/tests/workspace-state.test.mjs` |
| CI/packaging | `.github/workflows/check.yml`, `docker.yml`; `scripts/check-browser-steps.mjs`, `release.go`, `check-source-package.mjs`, `check-package.mjs` |
| Recovery/baselines | `scripts/upgrade-recovery.mjs`, `upgrade-recovery-private.mjs`, `upgrade-rollback.mjs`, `fetch-baseline-release.mjs`, `baseline-releases.json` |

Use source searches rather than relying on old line numbers. Proposed route/type/column names below are design tasks, not existing symbols.

## 4. Invariants that govern every change

1. **Authority and transaction order:** acquire the store connection/transaction before the authority fence. Inside a transaction use `tx`, not a second connection through `s.store`. Re-establish session/account/capability in the transaction. Authority-changing commits and hub invalidation use ADR 0009's protocol; slow network/stream/voice cleanup stays outside the fence.
2. **Shared conversation storage:** schema 11 already rebuilt messages with preserved rowids/IDs/relationships, required `conversation_id` and nullable `channel_id`. Channel conversation ID equals channel ID; private messages have no channel projection. Preserve FKs/FTS and restart postconditions. Do not introduce shadow channels or a second private message stack.
3. **Consent/blocking:** crossed requests do not auto-consent; only the recipient accepts. Active accepted membership, not global role, authorizes history. Blocking retains accepted readable history but stops new contact mutations; own deletion remains. Unblock never replays refused operations. Hiding is personal and never automatically reversed.
4. **Request bounds:** current introductions are at most 500 Unicode characters; durable limits are five pending outgoing, twenty incoming, ten newly requested pairs/day and a 24-hour rejection cooldown. A canonical pair survives multiple request attempts, so pair ID alone cannot identify consent to a particular attempt.
5. **Send recovery:** operation identity is `(account, key)` with destination in the intent. Acknowledged replay has no new effects; deleted/expired keys never resurrect. Outbox persistence remains opt-in/account-scoped with 50 records/64 KiB UTF-8 text and a 24-hour automatic retry window. Preserve draft recovery, attachment destination binding and monotonic read cursors.
6. **Private evidence boundary:** `report.review_private` authorizes voluntarily submitted evidence only. It never grants history/context/search/files or membership. Owners hold valid capabilities by the existing owner rule; other reviewers need explicit delegation. Community report reviewers still require `message.delete_any` plus current community channel read access.
7. **Retention and identities:** evidence availability ends 90 days after creation; resolution/reopen does not extend it. Physical cleanup is bounded and backups keep their snapshots. Tombstoned removal retains a Deleted-member identity; physical erasure nulls report FKs. Do not conflate either with reversible deactivation or promise deletion of every copy on expiry.
8. **Private safety:** no private external link previews, no implicit surrounding evidence, no general audit disclosure of private review activity, no new E2EE promise, no global admin DM bypass. API/client responses require current admitted account/session/authority and safe error behavior.

## 5. P10-01 — Repair the new evidence paths before extending them

The two backend cases below were reproduced through an external Go overlay. Client cases were established by source inspection and must gain rendered failing-first regressions.

### A. Valid Unicode reporting fails (R10-01)

`privateEvidenceForMessage` builds `version` from full message text + NUL + `edited_at`; submit rejects `len(version)>8000` **bytes**. Valid 4,000-character ASCII edit/preview/report returns 200/200/201, but CJK and emoji return 200/200/400 with a misleading stale-preview error (12,031/16,031-byte versions).

Replace that unbounded representation with a bounded canonical fingerprint/version tied to the source identity and authoritative state. Keep transaction-time read authorization and preview equality; never accept client text as evidence. Test maximum-length ASCII/CJK/emoji, repeated edits, wrong source/actor, malformed tokens and denial with no side effects. Reuse the corrected preview mechanics for request introductions later; do not lower supported message length to hide the defect.

### B. Private review metadata leaks to general auditors (R10-02)

`reviewPrivateEvidence` writes `private_report.*` through general `auditTx`; `auditLogs` returns it without private-evidence filtering. An account with only `audit.read` gets private queue403 but general audit200 containing reviewer username, report ID, action and timestamp. No message text/reason or DM access was exposed in that probe.

Implement the restricted audit boundary promised by ADR 0013. Choose a restricted store or correct server filtering/admission before pagination/limits; handle existing records deliberately. Keep atomic review+audit rollback if audit fails. Test ordinary auditor, community moderator, private reviewer, revoked reviewer, feature-off state and pagination with allowed controls. Do not fix it by removing audit records or hiding rows only in the UI. Define audit retention consistently with evidence retention and document the chosen contract.

### C. Changed preview has no working reconfirmation (R10-03)

`api.ts` throws `APIError` with only message/status, discarding the server's structured409 preview. `ReportPrivateMessage.svelte` expects `failure.preview`. Even if the preview were retained, `changed=true` has no acknowledgement/reset control and keeps Send disabled.

Preserve a typed, validated error payload or safely fetch a fresh preview. Keep the same account's explanation, show exactly what changed, and provide an explicit new confirmation that can submit. Never auto-consent or blindly trust arbitrary error data. Render a journey where another participant edits between preview and submit, then the reporter re-confirms successfully; verify saved text is the new confirmed text and only one report exists. Include repeated edits, malformed/missing conflict payload, account switch and permission loss.

### D. Reviewer panel becomes permanently stale (R10-04)

`admin/PrivateEvidence.svelte` captures account and generation once using `untrack`, then reuses them on every `load`. An unrelated same-account authority change makes all later loads inadmissible; even Refresh cannot recover. Mutation completions are not fenced as consistently as list responses.

Capture current request identity per operation; separate loaded-data scope from current authorization. Immediately withhold/clear unverified or revoked evidence, but reload successfully if the same account retains the capability. Fence success, failure and cleanup for list/review/pagination operations. Add actual reviewer browser journeys for same-account refresh, revoke/regrant, held GET/PATCH, account switch and pagination. The P9 report happy-path browser journey did not render this panel.

### E. Fix Linux-audit placement in Windows CI (R10-05)

The previous `tee` masking fix is correct. However, the new `check-browser-steps.mjs` executes `/bin/sh` and builds a colon-separated PATH while `.github/workflows/check.yml` invokes it on Ubuntu, Windows and macOS.

Move execution of this Linux-shell audit into a required Linux job, or implement and verify native Windows portability. Preserve the actual Windows Go/frontend/application matrix and required browser gate. A missing shell is an infrastructure failure, not a passing skip. Retain the audit's failing-test-with-`[directs]` control, passing control and diagnostic collection. Local macOS success alone does not demonstrate hosted Windows behavior.

### F. Align request count with actionable requests (R10-06)

`outstandingIncomingRequests` filters on the requester's `contact_preferences.allow_requests`. That setting governs their incoming requests; turning it off does not withdraw their existing outgoing request. The recipient can accept the request even when the badge hides it.

Define count/list/action eligibility consistently and remove the unrelated dependency. Test both accounts' preference changes, existing pending requests, new-request policy, blocked/deactivated/removed senders, accept/withdraw/decline and cross-device/reconnect updates. Opening Requests must not resolve or clear an outstanding request.

### G. Finish disclosure, capacity and documentation accuracy (R10-07/08)

- Explicitly disclose before submit that reviewer evidence contains the reporter's identity, author identity, selected text and explanation; describe API expiry, bounded physical cleanup and historical backups accurately.
- Replace “Nothing … is sent” on an authority change with accurate submitted/unsent/uncertain outcome states. A request may have committed before its response; a combined report-and-block will itself advance authority.
- Honor the private feature flag in admin-tab visibility and loaded-data clearing as well as routes.
- Full 5,000-unexpired-record capacity currently becomes generic500. Define an actionable refusal, preserve explanation and roll back allowance/report side effects. The durable ten/hour quota uses a fixed one-hour window; do not call it rolling. The cap bounds unexpired rows, not every physical byte including backlog/backups.
- `migration13.sql` claims no conversation ID, but the table and reviewer JSON include one. Minimize outward DTOs and align storage/disclosure documentation. An internal provenance key does not need to become reviewer navigation or client metadata. Do not lose existing reports while changing representation.
- Correct ADR0013 removal/erasure wording and its restore sentence: snapshot grants **are restored**, while later revocations need reconciliation.
- Capture exact original report text/expiry in `upgrade-recovery-private.mjs` and compare them after restore instead of only length/nonempty checks. Correct the revoked-session diagnostic that prints an earlier200 while asserting401. Attribute feature-off/on coverage to the API test unless the rehearsal actually adds that branch.
- Update stale README, progress, master-plan and deployment summaries to the current candidate and genuine limits. Preserve earlier release/evidence history with dated corrections. The P9 browser ledger has five directs journeys, not six.

## 6. P10-02 — Contract for reporting a pending introduction

Record the extension in an ADR before schema/routes. The new member action is **Report request and block**, alongside the existing separate Block action. Existing accepted-message reports remain report-only and unchanged. Do not add a second pending-request report-only mode in this bounded tranche.

Eligibility: the authenticated active account must be the actual recipient of the exact still-pending incoming request. A requester, unrelated member, owner or evidence reviewer cannot preview/report it by guessing its identifier. No acceptance, message history, file access or membership is created by this action.

Evidence: only the server's current introduction, sender identity, reporter identity, source-kind/time information necessary to interpret it, and reporter explanation. Show the exact preview, reviewer audience, identity disclosure, expiry/backup limitations and combined blocking effect before confirmation. No files, surrounding messages or other pending requests. Empty-introduction behavior must be defined explicitly rather than silently manufacturing text.

After ordinary reject/block/withdraw/account cleanup has erased an introduction, it is unavailable for new reporting. Explain that honestly. Never accept client-cached text as authentic historical evidence, covertly preserve every rejected introduction, or require accepting a harasser to report them.

### Request-instance identity and migration

`direct_requests` is updated when the same canonical pair requests again. Add a durable immutable identity for each request attempt, or an equally rigorous generation scheme; backfill existing pending rows in the next ordered migration. Preview/confirmation must bind source kind, attempt identity, sender, recipient and exact authoritative introduction/state.

A direct request is not a message. Add an explicit evidence source discriminator and request-instance reference; do not put a request ID into `message_id`, create a fake message or duplicate the entire moderation subsystem. Extend/refactor existing evidence/list/review/expiry/limit mechanics while preserving all schema-13 reports, IDs, text, review state and original expiry. Identify the actual next migration number after inspecting the tree (currently newest13).

Use the same `report.review_private` boundary and corrected restricted audit. Reviewer UI labels evidence clearly as a reported message or request introduction; neither gives access to the underlying conversation. Share the ten/hour account quota and 5,000-unexpired installation capacity across source kinds so adding an endpoint does not multiply the allowance.

### Atomic combined operation

Within one transaction:

1. Re-establish account/session/feature eligibility and operation ownership.
2. Resolve the exact pending request instance and confirmed preview.
3. Enforce shared durable quota/capacity and input bounds.
4. Store the immutable consented evidence and operation receipt.
5. Insert the block and reject/clear that request introduction.
6. Record required restricted audit and commit through the existing authority fence.
7. Publish account/contact invalidations and perform slow follow-ups only after commit.

Share a transaction-level helper if necessary; do not call one HTTP handler from another or nest a connection acquisition. Inject failures at evidence, block/rejection, audit and commit boundaries. A failed combined action must leave neither a new report nor a new block/rejection. The UI must accurately offer separate Block when evidence submission is refused/full/unavailable; blocking without reporting must not depend on report capacity.

If accept/withdraw/block/removal wins the race first, reject the stale submission without saving client text. If report-and-block commits first, subsequent acceptance must fail. Keep retained history of other accepted conversations and unrelated users untouched.

## 7. P10-03/04 — Retry semantics and user experience

The existing message-report deduplication index is only “one open report per reporter/message.” It is insufficient for report-and-block, whose successful operation intentionally destroys its live source and changes authority.

Give the operation a stable account-owned idempotency identity bound to the exact request attempt and intent. Evaluate an existing acknowledged receipt before requiring a still-pending source, while checking the current authenticated account and permitted feature policy. Define changed-intent and expired receipts explicitly.

Required behavior:

- A lost-response retry returns the original reporter's minimal receipt without a second report, renewed quota consumption or repeated block.
- Resolve/reopen does not turn an old operation into new work.
- A later deliberate unblock is respected: replaying an old acknowledged operation must not block again.
- A new request attempt in the same canonical pair cannot be reported or blocked by an old preview/operation.
- Another account cannot retrieve the receipt. First success preserves its original explanation/snapshot; a retry cannot overwrite them.
- Evidence expiry/cleanup cannot resurrect a prior action or expose expired text through the receipt. Keep receipt metadata minimal and define its lifecycle separately from evidence bodies.

The new request action should reuse corrected preview/disclosure mechanics from message reports and appear beside existing pending-request controls. Keep independent ownership for (a) account/session-authored explanation and operation key, (b) authority-sensitive preview, (c) confirmed or uncertain outcome, and (d) current contact status.

A successful combined action can emit authority invalidation before its HTTP response arrives. Clear stale private preview data without incorrectly treating that expected transition as a failed send. Reconcile uncertain outcomes with the **same** operation identity; never silently submit another report or reapply a block. Clear user text on account exit and reject late responses for another account.

Provide explicit changed/unavailable/rate-limited/full/uncertain/success states, preserved same-account explanation, keyboard focus and narrow-layout usability. Display report/block success only from confirmed outcomes. A requester receives no reporter explanation, report status or reviewer activity through invalidation payloads.

## 8. P10-05 — Required regression and restore matrix

| Area | Required proof |
| --- | --- |
| Existing message reporting | Maximum Unicode; exact preview/re-confirmation; blocked readable participant; outsider/admin denial; restricted audit; capacity and expiry |
| Reviewer lifecycle | Actual rendered review queue, unrelated same-account authority refresh, permission loss/regrant, held list/mutation responses, account switch and paging |
| Request ownership | Only actual recipient can preview/confirm; sender/outsider/owner/reviewer denied; no implicit acceptance, history or file access |
| Source generations | Renewed requests in the same pair reject old preview/operation; request instance survives restart and restore |
| Atomicity | Evidence/block/rejection/audit failures leave all combined side effects absent; successful combined action clears intro, updates badge and blocks contact |
| Races and replay | Accept/withdraw/block/removal races; concurrent submissions; lost acknowledgement; restart; resolved report; deliberate unblock; no repeated side effects |
| Resources/privacy | Message and request reports share quotas/capacity; source text/explanation bounds; no arbitrary client evidence; no unauthorized audit/queue metadata |
| Client state | Exact preview, preserved explanation, expected authority transition during successful combined action, account switching, truthful uncertain outcomes |
| Feature gates | Default-off hides/refuses new surfaces; re-enable follows recorded retention policy; no production gate opened |
| Recovery | Exact submitted message/request text, original expiry, source kind/instance, receipt and block state survive; old sessions/devices absent; post-snapshot reviewer revocation/block/deactivation reconciled before cutover |

Extend `private_evidence_test.go`, `private_evidence_helpers_test.go`, direct/contact/authority tests and the opted-in Playwright journeys. Add permanent regressions from the temporary diagnostic reproductions; never depend on `/private/tmp` probe files in CI. Use deferred requests/transaction barriers rather than timing sleeps. Node workspace tests do not render Svelte effects; use actual Playwright for reporter and reviewer components.

Preserve both default-off and opted-in browser coverage and old regressions. Extend `upgrade-recovery-private.mjs` with real API-created request evidence and operation retries after restore. Assert preconditions before taking the snapshot, then compare exact restored values and both allowed/denied access. Test the actual alpha.7/schema13 → new-schema transition using the preserved P9 executable as an old baseline, plus maintained alpha.5 and alpha.3 paths as applicable. Do not rebuild an “old” release from current source or substitute a schema fixture for all cross-version tests.

## 9. Evidence inherited from P9 and review 10

Fresh review-10 checks: frontend112/112, Svelte0 errors/warnings, local browser-step audit/self-test. Two external overlay probes reproduced Unicode rejection and general-audit metadata disclosure (expected assertion failures, API2.469s); source was unchanged. Other UI findings await rendered reproduction.

P9 recorded, not freshly rerun in review10: full race all10 packages, API966.515s with25-minute timeout; browser13/13 default-off and18/18 opted-in; community restore23/23, private85/85, alpha.5 rollback57/57 and alpha.3 rollback55/55. Some exact-value/diagnostic claims need the corrections above. Do not present inherited results as your own executions.

Candidate `dist/brrpbrrp-p9-checkpoint` hashes to **`fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f`**, embeds **`2869db55394b2b2a004424672517b38d6908586b`** with **`vcs.modified=false`**, and matches private/rollback JSON candidate identities. Frontend build is **`6b00f391da81a4bc`**.

This closes the dirty-binary provenance problem for that executable. **It does not supply alpha.7 distribution archives or extracted-package consumers: none were built.** Alpha.6's seven archives remain unchanged as prior packaged history. Finish distribution evidence at P10 rather than relabeling P9's binary as an extracted package.

## 10. Toolchain and verification commands

Known local toolchain: `.tools/go/bin/go` (Go1.27.1), Node24.2.0 and Playwright Chromium. On a fresh checkout install supported tools/dependencies with `npm --prefix web ci`; install Chromium from `web` with `npx playwright install chromium` if needed. Build `web/dist` before Go, since it is embedded.

Inspect `go env GOMODCACHE GOCACHE`. The populated module cache in this workspace was `/Users/cameronamer/go/pkg/mod`; the old `/private/tmp/brrpbrrp-go-mod` lacked current modules. Use real available caches and a writable build cache. `GOPROXY=off` is useful only with a complete cache; classify missing dependencies separately from product failures. Do not assume private paths exist on another machine.

Final integration example from the actual repository root, adapting paths:

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

The whole race API package took16–19 minutes in P9. Use focused tests while developing, then run the expensive integrated baseline once ready. Keep CI timeout requirements consistent with measured workload; do not repeatedly increase a timeout to hide a hang or restart a still-running suite. Socket tests may need the environment's execution approval; follow that mechanism without weakening them.

The e2e wrapper strips ambient `BRRP_` settings and creates disposable data. Use `BRRP_E2E_PRIVATE_CONVERSATIONS`, check actual selected projects, and keep production login/feature defaults unchanged. The audit command above requires the platform fix or an appropriate Unix host; it is not a substitute for the real browser suite.

Recovery/consumer entry points; replace placeholders with real absolute paths and new report destinations:

```text
node scripts/upgrade-recovery.mjs --binary CANDIDATE --report NEW_REPORT
node scripts/upgrade-recovery-private.mjs --binary CANDIDATE --report NEW_PRIVATE_REPORT
node scripts/upgrade-rollback.mjs --binary CANDIDATE --old-binary P9_BINARY --report NEW_P9_ROLLBACK_REPORT
node scripts/upgrade-rollback.mjs --binary CANDIDATE --old-release OLD_ZIP --report NEW_BASELINE_REPORT
node scripts/check-package.mjs EXTRACTED_CANDIDATE_BINARY
```

Keep the prior alpha.5/schema10 and alpha.3/schema2 archive paths/digests. Backup-format compatibility depends on the actual old reader and snapshot schema: alpha.5 can read format2/schema10 pre-upgrade snapshots; alpha.3 rejects format2. Old readers must never run directly on an upgraded database.

## 11. P10-06 — Build an actual clean distribution checkpoint

Freeze code, migrations, package inputs and shipped docs in a clean commit. Create coherent commits when permitted; preserve existing work and default to a `codex/` prefix if a new branch is needed. Select the next unused consistent prerelease identity (expected alpha.8 if still free) for new archives. Do not overwrite alpha.6/P9 artifacts or change to v1. Keep Go/frontend/lockfile/release-helper identity declarations and their test consistent. Schema changes require the next registered migration; backup format need not change solely because schema does.

Build native/source distribution artifacts from that clean source, then verify embedded source revision/dirty flag, frontend identity, binary hashes and every ZIP digest in a manifest. Outputs/evidence should be outside tracked build inputs; add an evidence commit afterward identifying the exact implementation commit. Do not repeatedly rebuild archives just to insert the evidence describing those same archives.

Run actual extracted native smoke and clean source extraction → dependency install → frontend/Go build → consumer smoke. Exercise browser/recovery/upgrade against the identified packaged candidate where relevant; packager linker flags can produce different bytes from the ad-hoc binary. Record the actual tested identities instead of borrowing results from another build. Cross-building six native targets is not runtime qualification on six platforms.

`dist/`, tools, module/browser caches and temporary probes are ignored local artifacts, not contents of a fresh CI checkout. `scripts/baseline-releases.json` pins old archives; `fetch-baseline-release.mjs` accepts an approved local directory or HTTPS host. Hosted rollback still needs real `vars.BRRP_BASELINE_RELEASE_URL`. Missing/mismatched old bytes must fail, not skip or be replaced with rebuilt current code. Preserve historical reports and digests; put P10 evidence in a new directory with dated corrections where necessary.

## 12. External dependencies and remaining v1 roadmap

No hosted CI, configured repository remote, Windows/Tailscale host, Docker runtime, physical media, encrypted independent restore, measured RPO/RTO, delivered alerts, capacity/soak, independent security/accessibility review, beta or release approval was established. Prepare exact commands/artifacts/expected outcomes for unavailable environments and continue independent local tasks. Ask only for missing inputs you actually need; both host targets and MIT/CLA are already decided.

After P10, prioritize:

1. **DM activity/notification contract and implementation:** persistent activity that works without push; stable event identity/retention/acknowledgement; account/conversation/device preferences; private preview defaults; active/background/closed-PWA behavior; tab/service-worker deduplication and current consent/block/access checks at dispatch. Request count, message unread, activity acknowledgement and OS delivery are distinct concepts. Do not pass DMs through channel push with an empty channel ID.
2. **Consenting groups/lifecycle/export:** invitations, bounded membership, owner transfer/leave/remove, disclosed history, shared-group block semantics and restored membership safety.
3. **Named threads and daily UX:** follow/read/archive/lock, bounded full1,000-reply navigation, composer/upload reliability, moderation/timeouts/invitations, keyboard/mobile/accessibility.
4. **Media:** early measured architecture spike, then private calls/screen sharing with explicit capture/stop/generation/admission, physical browsers/devices, direct/forced relay/NAT/IPv6 and quality/capacity qualification.
5. **Open source/operations:** public repository and tested security intake, maintainer/recovery/signing identities, actual Windows service/Docker lifecycle, encrypted independent recovery with config/TLS/key custody, received alerts and reference-load/soak. Master targets include RPO≤24h and RTO≤60min including response/acquisition/decryption/reconciliation, not just database restore time.
6. **Release acceptance:** independent security/privacy and accessibility/browser reviews; non-author installation/recovery;14 consecutive beta days across at least three communities and both deployments; a frozen distributed candidate with seven-day observation; explicit publication approval and staged rollout/support ownership.

All G0–G8 gates remain open. This assignment does not authorize public publication, production migration, enabling privacy gates globally or pretending local coverage supplies independent/physical acceptance.

## 13. Working method and completion report

Start with a concise execution outline, then implement. Resolve routine reversible choices yourself. Delegate independent backend/client/CI/recovery work with clear file ownership and shared schema/API contracts. Avoid unrelated rewrites/dependency churn and tests that merely mirror implementation. Reproduce defects with permitted controls; do not use blanket retries, arbitrary sleeps or stubs to claim behavior those tools cannot prove.

A coherent stop is **P10-01–06 delivered locally**, with:

- The reporting/queue/audit/Unicode/count/CI defects repaired and permanent regressions.
- A recorded source-attempt/preview/atomicity/idempotency contract and implemented recipient-only Report request and block.
- Exact disclosure, useful failures, safe account/authority transitions and a restricted reviewer/audit boundary.
- Migration/recovery preserving old message reports and new request evidence/receipts/blocks with exact-value checks.
- Green integrated local verification and an attributable clean distribution/consumer record; missing external acceptance listed separately.

Final response: a P10 package status table, changed areas and rationale, failing-before/passing-after evidence with positive controls, exact commands/exits/counts/durations/feature modes, source/frontend/binary/archive identities, paths to new reports, unresolved blockers and the next bounded tranche. Distinguish inherited results from your runs. If any required local work remains, report it explicitly instead of marking the goal complete because the turn ended.
