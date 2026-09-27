# Comprehensive next-coder prompt — BrrpBrrp, review 8

Updated 27 September 2026. Copy **everything below the separator** into the next coding session. This prompt supplies the context the previous conversation contained; the repository documents provide supporting evidence and exact contracts. No access to the previous conversation is required.

---

You are taking over development of **BrrpBrrp**, a self-hosted, open-source community chat application. The owner's goal is a usable Discord replacement: simple installation, understandable privacy and permissions, dependable messaging and recovery, accessible desktop/mobile use, moderation, voice and screen sharing. Reliability, recoverable user work and ease of operation are product requirements.

You have no prior conversation context. Use this prompt and the repository as your starting point. **Your assignment is implementation and verification, not another planning-only review.** Complete the bounded stabilization tranche below, then leave a clean handoff for subsequent feature development. Do not interpret the broad v1 goal as permission to skip acceptance requirements or start several unfinished rewrites.

## Context and source of truth

Workspace: `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp` (use the equivalent repository root if moved).

You need access to the repository/source files to execute this task. If you were given only this prompt and no checkout, request the repository or source archive once; there is no known configured public remote URL to assume. Do not depend on a previous agent's memory, terminal session, temporary probe or access credentials.

Read these before editing:

1. `planning/completion-review-8-and-next-steps-2026-09-26.md`
2. `docs/v1-progress.md`
3. `docs/releases/0.4.0-alpha.6.md` and `planning/alpha6-evidence/verification.md`
4. `docs/adr/0012-private-conversations-consent-and-shared-storage.md`, plus ADRs 0007–0011 for the contracts you touch
5. Relevant packages and invariants in `docs/v1-implementation-plan.md`

Reviewed source is `1f762e9a3cee4e1871f434b4e2c7e52360a38511`: app `0.4.0-alpha.6`, schema 12, backup format 2. Check actual Git status/HEAD first; preserve subsequent user changes and reconcile them with the review. Review 7's alpha.4/no-Git assessment is historical. The newer evidence takes precedence for current implementation status; older ADRs still govern invariants except where a later ADR explicitly extends them.

At handoff preparation, HEAD still matched that commit. There were intentional uncommitted documentation changes in `README.md`, `docs/adr/0011-account-removal-and-retained-evidence.md`, `docs/deployment.md`, `docs/production-readiness.md`, `docs/v1-implementation-plan.md` and `docs/v1-progress.md`, plus the new review-8 report and this prompt under `planning/`. Those are completed review work to preserve, not unrelated changes to discard. No application fixes from review 8 have been made yet. Recheck this rather than assuming it remains true when you start.

Alpha.5 remains the packaged checkpoint (six native ZIPs and one source ZIP). Alpha.6 implements gated consented DMs on shared storage, request limits, blocking, hide/contact preferences and shared messages/files/replies/search/read/recovery. Private reporting, DM notification policy, groups, named threads, private calls and screen sharing remain incomplete. No G0–G8 release gate is accepted. Required deployment targets are already **both Windows/Tailscale and Linux Docker**. MIT/CLA decisions are already recorded.

## What exists and how the application works

### Product and deployment baseline

- One community/workspace served by one native Go executable, with embedded Svelte/TypeScript assets and SQLite. Node is a build tool, not an application runtime service. There is no separate database server or hosted account dependency.
- Community text/voice channels, durable messaging, attachments, replies, pins, reactions, mentions, search, themes, responsive layout and an installable PWA shell already exist.
- MFA/session management, owner recovery, roles/delegation, restricted-channel administration, slow mode, selected community-message reports, channel notification muting and history-preserving account removal are implemented locally.
- The default-off `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS` flags are exposure gates. Keep them closed by default while testing explicitly opted-in disposable installations.
- Existing product limits include 100 live/reversibly deactivated accounts, 100 channels, 12 MB uploads and 10 simultaneous voice participants serverwide. Larger v1 targets in the master plan are proposed qualification targets; do not silently raise current limits.
- Windows native service/Tailscale and Linux Docker are both required qualification paths. Native archives also exist for other platform/architecture pairs, which does not prove native runtime acceptance on them.
- The operator controls stored data, backups and media infrastructure. The app does not provide end-to-end encryption. Application-level DM membership isolation must still hold, including against unrelated administrators.

### Code map

| Location | Responsibility / relevance |
| --- | --- |
| `cmd/brrpbrrp`, `internal/app`, `internal/installer` | CLI, configuration, TLS, host lifecycle, setup and native services |
| `internal/db` | SQLite, ordered migrations, message hydration, receipts and recovery invariants |
| `internal/api/messages.go`, `uploads.go` | Shared message/attachment mutations; R8-01 is in the new attachment-claim path |
| `internal/api/conversation_access.go` | Central channel/direct scope checks and private event audience/publication |
| `internal/api/direct.go`, `contacts.go`, `conversations.go` | Canonical direct requests, consent/blocking/preferences and shared conversation APIs |
| `internal/api/authz.go`, `internal/ws` | Current authority, commit/admission fencing, bounded delivery queues and revocation |
| `internal/backup` | Snapshot/archive verification and restore |
| `internal/rtc`, `web/src/lib/voice.svelte.ts` | Existing Opus SFU/voice and browser capture/connection lifecycle |
| `web/src/lib/workspace.svelte.ts` | Account/authority generations, workspace snapshots, realtime events, sends, notification state and reconnects |
| `web/src/lib/outbox.ts`, `conversations.ts` | Shared operation/recovery state and conversation helpers |
| `web/src/lib/components/Workspace.svelte`, `ChannelNotifications.svelte`, `MessagesInbox.svelte`, `DirectControls.svelte` | The client lifecycle boundaries relevant to R8-02/03 |
| `web/tests`, `web/e2e` | Node unit/state tests and real rendered Playwright journeys |
| `scripts/upgrade-recovery.mjs`, `upgrade-rollback.mjs` | Maintained fresh recovery and actual archived-release upgrade/rollback |
| `scripts/check-package.mjs`, `check-source-package.mjs`, `release.go` | Extracted executable smoke, static package input/link validation, archive construction |
| `.github/workflows/check.yml`, `docker.yml` | Native/browser/Docker checks and package promotion dependencies |

Use `rg` to find symbols and current lines; review line numbers are from the inspected checkpoint and can move.

### Invariants you must preserve

1. **Database and authority ordering:** SQLite uses one connection. Acquire the store connection/transaction before the authority fence. Never wait for that connection while holding the fence. Re-establish session and capability in the mutation transaction. Revision-changing commits and hub invalidation follow ADR 0009; slow network/stream/voice work occurs outside the fence. Do not reintroduce a commit-to-invalidation gap or an opposite lock order.
2. **Shared storage:** schema 11 already rebuilt messages with preserved rowids, required `conversation_id`, nullable `channel_id` and strict projection/descriptor triggers. A community conversation ID equals its channel ID; a direct/group message has no channel projection. Schema 12 adds direct pairs, requests, blocks, preferences/read state and destination-bound staged uploads. Do not redo the migration, introduce shadow channels, or create a second private-message stack.
3. **Migration safety:** foreign-key mode for the rebuild is changed on a pinned connection outside the transaction and restored even on cancellation/failure. Preserve FKs, rowids, FTS, attachment/reply/pin/reaction references and durable postcondition/restart recovery. No backward migration is promised; rollback uses the matching older binary and pre-upgrade snapshot.
4. **Consent and privacy:** canonical unordered pairs do not imply acceptance. Only the request recipient accepts; crossed requests cannot auto-consent. Current active accepted membership authorizes reads. Owner/admin status alone never permits DM browsing. A participating administrator cannot delete another participant's message merely because of the global role. Current durable limits are a 500-Unicode-character introduction, five pending outgoing requests, twenty pending incoming requests, ten newly requested pairs per rolling day and a 24-hour rejection cooldown; do not bypass them to make fixtures convenient.
5. **Blocking:** an accepted pair's history remains readable, but new contact writes/uploads/claims/edits/reactions stop. Own deletion and necessary deletion updates remain allowed. Unblock never automatically retries refused sends. Blocking does not remove shared community-channel access. Hidden conversations remain hidden until the account explicitly changes that preference.
6. **Private surfaces:** DM search is explicitly scoped; global search remains community-only. Private events/files/counts must use current eligible audiences. Private pin/delete activity does not enter community audit, private messages do not enter the community reporting queue, and private URLs do not trigger external preview fetches.
7. **Send identity:** operation uniqueness is `(account, idempotency key)`; destination/content/attachments/reply are part of its intent. An acknowledged identical send replays its result without new side effects. Reusing a key with changed intent conflicts. Deleted/expired sends cannot resurrect; minimal spent-key guards survive compaction until the account lifecycle permits removal.
8. **Client recovery:** outbox persistence is opt-in and account-scoped, with a shared 50-record/64 KiB UTF-8 text budget. Automatic retry ends after 24 hours and becomes explicit text recovery. Do not silently reclaim old attachments, overwrite a current draft, submit recovered text, restore another account's work or move read cursors backward. Captured account/generation/version guards must reject stale asynchronous results.
9. **Account/data lifecycle:** deactivation, permanent removal preserving a Deleted member identity, and account/message erasure are different operations. Pending introductions are cleaned on removal; surviving accepted peers retain the permitted history. Restoring a backup restores its historical security state and requires reconciliation of later decisions. Existing backups are not rewritten by account erasure.
10. **Disclosure and retention:** community report evidence currently expires 90 days after creation, with bounded physical cleanup afterward. Do not accidentally implement the older proposal of 90 days after resolution. Private report policy is future work. Respect the no-E2EE disclosure and current data-retention explanations.

## Evidence you inherit — do not present it as your own rerun

Review 8 freshly passed 101 frontend tests, Svelte checking with zero errors/warnings and `go test ./...` (API 82.083 seconds, some other packages cached). It rechecked all seven alpha.5 archive hashes. Documentation link/package-input validation also passed. A diagnostic Go overlay reproduced R8-01 in both scopes. R8-02/03 were established by source tracing, not by new browser executions.

The earlier alpha.6 implementation record reports vet/build/format success, 14 opted-in browser journeys, a separate one-test default-off check, and recovery reports with 23 fresh-restore, 57 alpha.5 upgrade/rollback and 55 legacy alpha.3 upgrade/rollback checks. Browser voice tests use synthetic capture feeding real WebRTC/SFU/RTP; they do not prove microphone capture, audible quality, independent networks or physical-device behavior.

The alpha.6 whole-race attempt **failed** on a pinning fixture that lacked `message.pin`. The fixture and later fixes passed focused race runs; no final fully green whole-race result was recorded. Do not label that historical command successful or remove the failure from evidence.

The local `dist/brrpbrrp-alpha6-preview` hashed to `ffd53a04be65dc3fa2a0a403249560fcb9fb6e40bb069f631f7203d4f05ddd63` and embedded revision `44ae4f0` with `vcs.modified=true`. Rollback reports instead tested `e37309a6f2e7dba244bb48150d5abd153088be3a5221c588374a213baabed894`. Different hashes may reflect build options; they are still different artifact identities. No alpha.6 distribution ZIPs or release manifest exist yet.

## Assignment: implement P8-01 through P8-05 in order

### 1. Repair the reproduced attachment capability bypass (R8-01)

`internal/api/messages.go` claims staged files without rechecking current `message.upload`. Reproduce with existing API helpers: stage a destination-bound file, remove that capability through roles, confirm a new upload returns 403 and a text-only send returns 201, then send the staged file with a fresh idempotency key. Reviewed code incorrectly returns 201 and the recipient can download it. This affects both community and accepted-DM paths.

Add permanent regressions, then enforce current capability in the same transaction as a **new attachment claim**. Denial must create no message, receipt or claim. Preserve acknowledged idempotent replay and previously published authorized files. Cover both scopes, permission restoration, same-key retry after denial and permitted text sends. The review's temporary overlay is optional; reconstruct the case if it no longer exists.

Start with `internal/api/upload_authority_commit_test.go`, `direct_message_privacy_test.go`, `message_receipts_test.go` and the helpers `privatePipeline`, `directUpload`, `defaultRole`, `withoutCapability`. The existing upload test protects authority changes during byte transfer/metadata commit; this finding occurs **after staging, when the message claims the file**. Fixing only the upload endpoint does not close it. Use fresh operation keys for the denied new sends and explicitly separate an already-acknowledged replay control. Prove rejected sends leave the staged file unclaimed and do not emit a new message/notification.

Both community and direct sends enter `createMessage`; reuse the current principal returned by `scopeInTx`. Put the attachment capability guard after acknowledged/legacy replay handling and before new claims. **Do not add `message.upload` to generic `conversationAccess(..., write=true)`**, which would also deny ordinary text/typing/other writes. Inside the transaction, query through `tx`, never acquire the store's single connection again through `s.store`.

The `messagesIn()` test helper counts `channel_id`, so it cannot establish absence of DM side effects. Count by `conversation_id`, inspect the exact account/key operation receipt and check the staged attachment's `message_id`. Add a permission-restoration retry with the same previously denied key (one creation), an authorized committed-file replay after permission removal (200 while required read access remains), and a deleted-message replay (existing non-recreating 409). Preserve cross-destination/unscoped-upload refusal.

### 2. Repair two client lifecycle defects (R8-02 and R8-03)

- Move account notification-preference loading out of the community-channel header lifecycle. A fresh DM deep link or authority refresh while in a DM currently leaves `channelNotificationsReady=false`, dropping allowed community mention events. Keep fail-closed unverified state and account/generation fencing. Add rendered tests for deep link, refresh, unmuted/muted controls, delayed responses and account switch.
- Preserve an unsent direct-request introduction across unrelated contact/authority refreshes. `MessagesInbox.svelte` currently clears it whenever the effect watching `contactsVersion`/`authorizationGeneration` runs. Revalidate target eligibility without discarding the same account's text. Keep explicit account-switch clearing and copyable recovery if contact becomes forbidden. Test unrelated incoming requests, authority refresh, target block, successful send and account switch. Do not silently persist private drafts to shared storage.

Both client findings were confirmed by source tracing, not browser reproduction; establish failing rendered regressions before claiming fixes.

For R8-02, the mechanism is specific: `Workspace.svelte` selects `DirectControls` in a DM; the replaced `ChannelNotifications` component owns the only preference fetch. Initial state and authority invalidation clear readiness; `notification.mention` drops events while readiness is false. Fix account policy ownership without restoring stale preferences or producing duplicate OS notifications. The defect concerns live client interruptions; do not claim an independently proven server WebPush failure.

For R8-03, preserve draft ownership separately from fetched private/contact data. Unverified authority may require hiding fetched data and disabling submission; it need not destroy text the same account just typed. A blocked/disappeared target must not become a new destination automatically. Cover reload/persistence only according to the existing opt-in policy; do not add silent durable storage as a shortcut.

Clear a request draft only after an admitted successful mutation for its captured account/target/draft, explicit discard or account exit. The existing `start()` uses `if (!error)` after `act()`, but `act()` can no-op or ignore stale work; absence of an error is not proof of a successful send. Preserve session-version/account/authority-generation guards on successful responses, failures, retries and cleanup. Also prevent a delayed preference GET from overwriting a newer mute PUT.

`web/tests/workspace-state.test.mjs` runs the store in an inert browser shell and does not render Svelte component effects. Use it for deferred-response/state controls and Playwright for actual mounting and draft preservation. Extend `web/e2e/directs.spec.mjs`, `privacy.spec.mjs` or `account-switch.spec.mjs` as appropriate; reuse `support.mjs` helpers. Keep the `directs` project's `release` setup dependency and isolate accounts/state between journeys.

### 3. Make CI protect the actual feature set (R8-04)

Require both default-off and opted-in (`BRRP_E2E_PRIVATE_CONVERSATIONS=1`) browser runs before package promotion. Current CI omits the conditional direct-message project. Add meaningful fresh-recovery, pinned archived-version rollback and extracted source/native consumer checks to the appropriate required jobs; static file-list validation is not consumer execution. Pin and verify the old archive checksum. Missing baseline artifacts must not silently skip a gate. Preserve existing native and Docker prerequisites. Label modes and retain useful failure artifacts. Workflow edits alone are not hosted CI evidence.

The existing native matrix is Ubuntu/Windows/macOS with Go 1.27.1 and Node 24. The browser job currently uses Chromium. Docker builds Linux amd64/arm64 and executes amd64 container smoke; the package job depends on native, browser and Docker jobs. Preserve these distinctions, sensible job timeouts and existing pinned action references. Both feature modes must visibly execute the intended tests; merely setting a variable where the harness discards it is insufficient.

**Artifact availability matters:** `dist/`, `.tools/` and dependency/browser caches are ignored local directories. The archived ZIPs present here will not appear in a fresh CI checkout. The alpha.5 manifest at `planning/alpha5-evidence/release-manifest.json` is evidence, not an artifact download service. Implement a reproducible approved artifact supply/cache mechanism with pinned checksums, or clearly report the unavailable input. Do not fabricate a public URL, regenerate a supposed old release from new source, or substitute a current-schema fixture for actual cross-version coverage. Missing infrastructure must remain a failing/incomplete gate rather than a false pass.

### 4. Demonstrate private-state recovery (R8-05)

Extend maintained recovery coverage with accepted/pending/rejected DM requests, blocks, private attachment bytes/search, receipts, cursors, hide/contact preferences and erased-peer history. Verify restored behavior through freshly authenticated APIs, including participant/outsider access and invalidated old sessions/push registrations. Demonstrate post-snapshot security-state loss and the required reconciliation before cutover; do not pretend a restored snapshot remembers later blocks/password changes/removals. Keep the actual alpha.5 and legacy alpha.3 rollback paths passing. These are local recovery tests, not off-host acceptance.

Use disposable data and deterministic fixtures, not a user's real installation. Take a backup, restore to an empty destination, start the restored server and assert behavior through its real APIs. Compare the exact private-file bytes and test both eligible participants and an outsider administrator. Demonstrate a lost-ack receipt replay without a duplicate message and preserved blocked-send refusal after restoring a snapshot that contained the block. Separately exercise a block/security change made only after the snapshot: show that it is absent from the restored state until the documented reconciliation applies it. Keep restored service isolated until that step is complete.

Archive compatibility is baseline-dependent. The recorded alpha.5 binary accepts a candidate-produced format-2/schema-10 pre-upgrade snapshot; legacy alpha.3 rejects format 2. Neither fact permits running an old executable on an already-upgraded schema-12 database. Report actual reader/schema/format results instead of hardcoding universal incompatibility.

### 5. Close a clean checkpoint (R8-06; small R8-08 cleanup if practical)

After integration, obtain a fully green uncached race suite, final two-mode browser coverage and current recovery reports. The prior alpha.6 full race attempt exited 1 on a fixture; later focused passes do not replace the final whole-suite result. Record exact source and artifact identities. Current preview and rollback-tested binaries have different hashes; use one identified candidate for final checks. Build clean-source checkpoint artifacts and verify the extracted consumers. Preserve prior archives and evidence. Fix the source-package blank-line/count bug with small path/empty-input controls if it fits this tranche.

For the small count defect, `check-source-package.mjs` currently normalizes split lines before filtering blanks, so a trailing blank becomes `.` and adds one displayed entry. Filter blanks before normalization and preserve Windows-path/space-containing-path behavior. This tooling issue is not evidence of damaged alpha.5 archives.

Freeze implementation before the final expensive run. Record the implementation commit, frontend build identity, executable hash and resulting archive hashes. Create coherent commits when the environment permits, preserving existing documentation changes; default to a `codex/` branch if a branch is needed. Build from the recorded clean source. If a commit/build environment is unavailable, disclose the dirty state rather than inventing clean provenance. Generating evidence afterward must not silently change the tested program. Test the actual extracted distribution binaries too; a build from the working tree is not automatically identical to a packaged build.

## Verification tools and commands

The local toolchain is `.tools/go/bin/go` (Go 1.27.1); recorded Node is 24.2.0. On another machine, provision the supported versions and adapt paths. Check `go version`/`node --version` and existing dependencies before starting. Run `npm --prefix web ci` on a clean checkout or when dependency installation is needed; do not change the lockfile just to reproduce the build. Install Playwright Chromium using `npx playwright install chromium` from `web` if the browser cache is absent.

`web/dist` must be built before compiling Go because it is embedded. The following shell commands run from the repository root; environment exports are local conveniences, not project requirements:

```sh
export GOCACHE=/private/tmp/brrpbrrp-go-build
export GOMODCACHE=/private/tmp/brrpbrrp-go-mod
export BRRP_GO="$PWD/.tools/go/bin/go"
export PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
export BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-checkpoint"

npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
"$BRRP_GO" vet ./...
"$BRRP_GO" test -race -count=1 -timeout 20m ./...
mkdir -p dist
CGO_ENABLED=0 "$BRRP_GO" build -trimpath -ldflags='-s -w' -o "$BRRP_E2E_BINARY" ./cmd/brrpbrrp
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e
BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e

node scripts/licenses.mjs --check
node scripts/check-docker-inputs.mjs --self-test
node scripts/check-docker-inputs.mjs
node scripts/check-source-package.mjs
node scripts/check-source-package.mjs --self-test
git diff --check
```

Use focused failing/passing regressions during development, then run the expensive integrated baseline once the tranche is ready. The API race package previously took about 14 minutes; the 20-minute package timeout is intentional. Do not repeatedly restart a running test. Local socket binding may require the environment's execution approval.

The e2e wrapper `web/e2e/run.mjs` creates its own temporary loopback server, strips inherited `BRRP_` settings and forwards a controlled feature mode. Use `BRRP_E2E_PRIVATE_CONVERSATIONS`, not a blanket production flag, to select the private test mode. It supports forwarded Playwright arguments for focused development runs. Verify test selection and do not add arbitrary sleeps or blanket retries to conceal races. The harness's elevated login budget is isolated to test accounts; production login limits stay unchanged.

Existing recovery entry points are:

```text
node scripts/upgrade-recovery.mjs --binary PATH --report FILE
node scripts/upgrade-rollback.mjs --binary PATH --old-release ZIP --report FILE
node scripts/check-package.mjs PATH_TO_EXTRACTED_BINARY
```

Replace `PATH`, `ZIP` and `FILE` with actual absolute paths; they are placeholders. Use the same identified candidate binary for browser and recovery runs. Choose the host-matching alpha.5 archive explicitly and run the legacy alpha.3 baseline separately. Reports must record old/new hashes. Extend the scripts or add a maintained companion for private fixtures. A clean source extraction also needs its own dependency install/build/check proof. Save reports under a new checkpoint directory rather than overwriting `planning/alpha6-evidence`.

`scripts/release.go` builds native/source archives and can replace output names/checksum files in `dist`; inspect its flags and output paths before running. Preserve the existing alpha.5 archives/manifests. Avoid optional repeated packaging or full-suite runs once the required checkpoint is sufficiently verified. When packaging changes artifact bytes, record the new identities and run relevant consumer/recovery checks on those bytes.

## Environment constraints and external acceptance

At the review, this was a local macOS workspace with no Git remote, Docker executable, PowerShell runtime or connected Windows host. Do not assume credentials, a public repository, a registry or a signing identity. Local socket tests needed the tool environment's execution approval; follow the current environment's approval mechanism rather than editing tests to evade it. Browser caches and `/private/tmp` evidence are optional local aids, never portable dependencies.

There is no executed hosted-CI, native Windows/Tailscale, Docker-runtime, physical-media, independent off-host restore, capacity/soak, independent security, human accessibility, beta or final release approval evidence. If an environment remains unavailable, complete the independent local implementation and leave the exact executable/artifact/checksum, command, fixture, expected result and responsible role for the external check. Do not repeatedly ask whether both hosts are required; that decision is already made.

## What comes after this checkpoint

Keep the broad product direction visible, but do not start these features at the expense of finishing P8:

1. **Private safety and discovery:** selected-message evidence reports with an explicit disclosure preview and evidence-only reviewer access; persistent pending-request/activity indicators; DM notification defaults/preferences, private previews, current-eligibility dispatch and deduplication. Complete lifecycle/export and retention decisions. A community report queue is not authorization to browse DMs.
2. **Consenting groups:** ten-member target, invite/accept, owner transfer, leave/remove, disclosed history admission and explicit shared-group block semantics. Use the shared storage/authorization pipeline. Race-test consent/removal and the last owner; revoked members lose all file/search/event/history access according to the contract.
3. **Everyday experience:** named threads with follow/read/archive/lock and full bounded traversal of the 1,000-reply fixture; richer moderation/timeouts/invitations; clear formatting/mentions, upload cancel/retry, keyboard/focus/IME/RTL/mobile quality. The existing bounded reply pane is not the complete planned thread feature.
4. **Media:** start a measured architecture spike early, then private calls and separate microphone/screen/video/audio publication lifecycles. Qualify explicit capture, cancellation, rejoin, move, revocation, direct and forced TURN routes, supported browsers/IPv6 and real physical devices. No silent recapture; do not raise voice limits without measurements.
5. **Operations/open source:** actual Windows service/Tailscale and Docker lifecycle/permissions/reboot/upgrade/rollback, independent encrypted restore with configuration/TLS/key custody, external alert delivery, frozen capacity profile and load/soak; canonical public repository, maintainers/recovery owners, tested private security intake, provenance/signing and support ownership. Retain MIT/CLA decisions.
6. **Release acceptance:** independent security/privacy and accessibility/browser reviews; non-author installation/recovery; at least three communities across both hosts for 14 consecutive beta days; a frozen distributed candidate with seven days of final observation; then explicit publication approval and staged rollout.

All gates remain open: G0 decisions/environments, G1 privacy foundation, G2 private integration, G3 community completion, G4 media, G5 operations/artifacts, G6 independent reviews/beta, G7 final candidate, G8 approved publication. The master plan contains exact work packages and targets, including proposed RPO ≤24 hours and RTO ≤60 minutes measured through operator response, acquisition/decryption, reconciliation and verification. Do not claim a completion percentage or reuse the original whole-project estimate as remaining effort.

## Working rules and stopping point

- Execute independently with small coherent changes. Parallelize backend, frontend and recovery work only with clear file ownership. Preserve centralized authority/admission ordering and account-scoped client state.
- Begin with a short execution outline, then code. Resolve routine reversible implementation choices yourself. Ask only for genuinely missing decisions or external authority, explaining the specific blocker while continuing independent work.
- Keep private and restricted features default-off. No automatic unhide, replay after unblock, admin DM browsing, private external previews, or new end-to-end encryption claims.
- No schema/version bump without a reason; no v1 label based on feature count. Avoid unrelated rewrites/dependency churn.
- No public publication, live deployment, production-data migration or unsupported release approval is implied by this prompt. Do not infer an old quota allowance for this new session.
- If host/CI/signing access is absent, finish local work and document the exact external check still needed. Do not claim Windows, Docker, off-host, independent review or physical-media acceptance from local substitutes.

Stop at a coherent, verified P8 checkpoint. Update the progress/evidence ledger with fixed findings, exact commands/results/source/artifact identities and remaining blockers. Report any incomplete item explicitly. Next planned feature tranche is selected-evidence private reporting and DM request/notification discoverability, then consented groups; the review covers the later threads/media/host/review/beta/release stages. Do not start that broad expansion before stabilizing these defects and evidence gaps.

### Required completion report

Provide a compact table for P8-01–05 with implementation status, evidence, and any remaining blocker. Include:

- The reviewed and resulting commit/dirty state, changed areas and why they changed.
- Each defect's failing reproduction, the fix and positive/negative controls proving it.
- Exact commands, exit statuses, test counts, feature modes and durations; distinguish new results from inherited evidence.
- Candidate/frontend/archive identities and the paths to new reports, including actual tested old-release identities.
- Any skipped/unavailable hosted or physical checks and precise steps to finish them.
- Remaining product/release gaps and a bounded recommended next tranche.

Do not mark P8 complete if one of its required local repairs/regressions remains undone. Do not mark v1, a release gate, a host or an independent review accepted merely because tests pass locally. Leave the next coder a reproducible checkpoint and an honest list of unfinished obligations.
