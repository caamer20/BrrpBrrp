# BrrpBrrp — Eighth completion review and next implementation plan

**Reviewed:** 26 September 2026; documentation finalized 27 September, America/Los_Angeles  
**Source:** `1f762e9a3cee4e1871f434b4e2c7e52360a38511` (clean working tree at review start)  
**Candidate:** `0.4.0-alpha.6` · database schema **12** · backup format **2**  
**Packaged checkpoint:** `0.4.0-alpha.5`, six native ZIPs and one source ZIP  
**Required deployments:** Windows/Tailscale **and** Linux Docker  
**Scope:** source/evidence review, bounded fresh verification, documentation corrections and an implementation handoff. No application code, feature flags, deployment or releases changed.

## 1. Completion verdict

**Substantial implementation progress has occurred since review 7. The app is a working alpha, with opt-in direct messaging, but is neither v1 feature complete nor production qualified. No G0–G8 release gate has passed.**

Review 7 inspected alpha.4/schema 5 before this workspace had Git history. Its statements that no implementation had changed and no Git checkout existed are historical. The current repository contains alpha.5's community/recovery work and alpha.6's private conversation foundation. Read the current release records and this review before using an older handoff.

The next implementation should stabilize the new private-conversation integration before expanding it. This review reproduced an attachment capability bypass and identified two client defects directly in source. It also found gaps between the tests available locally and those required by CI, and between general recovery tests and private-data recovery evidence.

No completion percentage is assigned: implemented features, independent acceptance and elapsed pilot periods are different obligations. The master plan's original 158–261 engineer-day estimate is a historical whole-project estimate, not an estimate of work remaining today. Re-estimate remaining packages after this stabilization tranche and the media feasibility work.

### Document order

1. [This review's coder handoff](handoff-next-coder-review-8-2026-09-26.md) — immediate execution instructions.
2. [Current progress ledger](../docs/v1-progress.md) — implementation status.
3. [Alpha.6 release record](../docs/releases/0.4.0-alpha.6.md) and [evidence](alpha6-evidence/verification.md) — recorded local checkpoint.
4. [Master v1 plan](../docs/v1-implementation-plan.md) — complete scope, work packages, invariants and release gates.
5. [ADR 0012](../docs/adr/0012-private-conversations-consent-and-shared-storage.md) — current direct-message contract; ADRs 0007–0011 remain relevant to durability, authority, recovery and lifecycle.

## 2. What has actually been achieved

| Area | Current implementation / evidence | Remaining boundary |
| --- | --- | --- |
| Core community chat | Persistent messages, files, replies, pins, reactions, mentions, full-text search, responsive interface and PWA shell | Named threads, full notification/activity model, richer composer/member experience and broad accessibility acceptance |
| Identity and authority | MFA, session/recovery UI, ownership ceremony, roles/delegation, central capability checks and documented transaction/admission ordering; account/session and delivery regressions | New attachment-claim finding below; independent review; actual host recovery ceremonies |
| Restricted community spaces | Categories, per-role/account access, management UI, scoped state/events/files/search/voice and revocation | Default gate remains closed; independent privacy acceptance and platform evidence |
| Reliable sends | Shared operation receipts, deletion-safe spent-key guards, account-specific outbox and explicit Copy/Dismiss recovery; 50-record/64 KiB UTF-8 budget | Reconnect performance qualification and complete private/group lifecycle coverage |
| Community moderation | Selected-message reporting/review, durable slow mode, channel mention/push muting, account removal preserving history as Deleted member | Private evidence reports, moderation workflow/timeouts, invitation controls and policy qualification |
| Shared conversation storage | Schema 11 rowid-preserving message rebuild, required conversation FK, nullable channel projection and strict descriptors; schema 12 direct/contact/read state | Groups, later thread/lifecycle migrations, wider fault and performance qualification |
| Consented DMs | Canonical pairs; requests, accept/reject/withdraw, durable limits/cooldown, contact opt-out, block/unblock, hide preferences; gated Messages UI | Source-level client defects below; discoverable request activity; private reports and DM notification policy |
| Private content | Accepted members use shared text/files/replies/pins/reactions/search/read/outbox; outsider admins denied; blocked accepted history stays readable; private previews disabled; private metadata excluded from community audit | Independent surface review; recovery fixture; groups/calls. No end-to-end encryption claim |
| Account lifecycle | Reversible deactivation, permanent history-preserving removal, separate account/message erasure; pending DM introduction cleanup and retained accepted history | Account export, complete private/group history policy and recovery reconciliation |
| Voice | Community SFU/Opus, channel admission and revocation; browser journey negotiates real WebRTC/SFU/RTP using a synthetic capture track | Physical microphones/devices/networks, private calls, multi-publication screen sharing and supported browser matrix |
| Recovery | Versioned hashed archives, verification, durable copy/status/retention handling, fresh restore and actual archived-release upgrade/rollback scripts | DM-specific restored state, actual Windows/Docker/off-host recovery, encrypted-copy/key-loss procedure, measured RPO/RTO |
| Open source and distribution | MIT/CLA records, generated notices, local Git baseline, six alpha.5 native ZIPs and source ZIP, CI definitions | Canonical public repository, tested private security intake, hosted CI evidence, signed/provenance-bound candidate, maintainer/registry/recovery ownership |

The alpha.5 release record documents the fixes to the older authority-lock, credential delivery and backup-retention findings. The bounded current audit found no additional confirmed outsider-admin DM bypass or schema rebuild defect. That observation is not independent security acceptance or proof of absence of other defects.

## 3. Verification performed for this review

### Fresh results on the reviewed source

| Check | Result and scope |
| --- | --- |
| `git status --short`, source/version inspection | Clean at start; source identity above |
| `npm --prefix web test` | **101/101 passed**, zero failed/skipped |
| `npm --prefix web run check` | **0 errors, 0 warnings** |
| `go test ./...` | **Passed all packages**; API 82.083s. Several packages were cached. This was not an uncached race run |
| Attachment revocation probe | **Required denial failed in both community and direct conversations**; fresh upload 403, text send 201, staged-file message 201, recipient download 200 |
| Alpha.5 archive identities | Recomputed all seven archive hashes; all match stored manifests. Manifest copies identical |
| Bounded source audit | Reviewed private scope/admission, migration, contacts, uploads, notifications/inbox and release/recovery surfaces. Two client findings are source-confirmed, not browser-reproduced in this review |
| Local environment | No Git remote, Docker executable or `pwsh` available. No live-host or credential access attempted |
| Documentation validation after edits | `git diff --check` and source/native package documentation checker passed; all 11 relative links in the new planning documents resolve. The checker's known count-label defect remains R8-08 |

Fresh Go tests used `.tools/go/bin/go`, `GOCACHE=/private/tmp/brrpbrrp-go-build` and `GOMODCACHE=/private/tmp/brrpbrrp-go-mod`. Local socket binding required the execution permission already granted for the test run.

### Recorded alpha.6 evidence inspected, not rerun here

- Full Go non-race suite and vet passed in the implementation tranche.
- The whole race attempt **exited 1** because a pinning fixture lacked the permission it exercised. The corrected and strengthened fixture and later fixes passed focused race tests. There is still no recorded fully green final whole-race command for alpha.6.
- Production build/format passed; frontend identity `2e25bd1cf7a5e3f4`.
- Default-off browser check: **1/1**. Opted-in full browser suite: **14/14**. Synthetic capture with real negotiated RTP does not establish physical-media acceptance.
- Fresh restore: **23 checks**. Actual alpha.5/schema-10 upgrade/rollback: **57 checks**. Actual alpha.3/schema-2 upgrade/rollback: **55 checks**.
- Dependency notices and static Docker/source input checks passed. Static inspection does not execute Docker.

Evidence files: [verification](alpha6-evidence/verification.md), [fresh recovery](alpha6-evidence/recovery.json), [alpha.5 rollback](alpha6-evidence/rollback.json), [legacy rollback](alpha6-evidence/legacy-rollback.json).

## 4. Findings and priorities

Priority means execution order/impact, not an invented external severity score. R8-01 is a reproduced permission defect; R8-02–03 are direct source findings requiring rendered regressions. R8-04–06 are qualification gaps, not evidence that every covered feature is broken.

### R8-01 — P1: attachment publication survives upload-permission revocation

**Evidence:** `internal/api/messages.go:309–323`, `internal/api/conversation_access.go:65–76`, `web/src/lib/permissions.ts:30`.

The UI defines `message.upload` as permission to attach files to messages. Upload metadata commit rechecks it, but later message creation claims staged attachments using owner/destination constraints without checking the current attachment capability. A member can stage a file, lose `message.upload`, retain `message.send`, and publish that file to a community channel or an accepted DM.

**Fresh reproduction:** stage a destination-bound file; remove `message.upload` through the roles API; confirm a new upload returns 403 and a text-only send returns 201; submit the staged attachment using a new idempotency key. The message returns **201**, and the other participant downloads the file with **200**. Expected new attachment publication: **403** with no message, receipt or claim committed.

The temporary Go overlay adds `TestReviewStagedAttachmentClaimAfterPermissionRevocation` to the existing upload-authority test file without changing repository source. It is at `/private/tmp/brrp-alpha6-review-kpscdfv_/overlay.json`; it is diagnostic evidence, not a maintained regression. If the temporary directory is gone, reconstruct the five API steps above using `privatePipeline`, `directUpload`, `defaultRole` and `withoutCapability` from the existing tests.

**Acceptance:** check capability in the same transaction that authorizes a new attachment claim. Preserve already acknowledged receipt replay without creating another message or claim. Cover community and DM paths, role revocation and restoration, text-only permitted control, same-key retry after a denied transaction, and zero new side effects on denial. Do not retroactively revoke delivery of an already published authorized file merely because its author lost upload permission.

### R8-02 — P2: community mention preferences are not loaded while staying in a DM

**Evidence:** `web/src/lib/components/Workspace.svelte:275`, `ChannelNotifications.svelte:54–65`, `web/src/lib/workspace.svelte.ts:69–70`, `:569–570`, `:1203–1215`.

The channel header component owns the only preference fetch. A direct conversation mounts `DirectControls` instead. Fresh state and authority invalidation set `channelNotificationsReady=false`, and mention events are discarded until it becomes true. A direct-conversation deep link, or an authority refresh while staying there, can therefore suppress otherwise allowed community mention interruptions until the user visits a community channel.

This was established by source tracing; no rendered reproduction was run in this review. It concerns the open client's mention path, not a claim that server push delivery also fails.

**Acceptance:** load account notification policy independently of the selected conversation, with account/generation guards and bounded retry. Keep unverified policy fail-closed. Browser-test a fresh accepted-DM URL and same-account authority refresh, an unmuted channel mention, a muted control, delayed preference responses and account switching. Stale preferences must never be installed for another account.

### R8-03 — P2: unrelated contact events discard an unsent request introduction

**Evidence:** `web/src/lib/components/MessagesInbox.svelte:54–69`; `web/src/lib/workspace.svelte.ts:1244–1250`.

The inbox effect clears `target` and `introduction` on contact-version/authorization-generation changes. Any direct/contact/conversation event advances this state. An incoming request or contact update from another device can erase text the user is composing for a different person.

Source-confirmed; no rendered reproduction in this review.

**Acceptance:** preserve same-account unsent text across unrelated refreshes. Revalidate the intended recipient separately from draft ownership; if sending becomes forbidden, retain a safe copyable draft and explain why. Keep account-switch/sign-out clearing and stale-result protection. Cover an unrelated request, same-account authority refresh, a newly blocked target, a successful intentional send and an account switch. Do not automatically persist private introductions to shared browser storage.

### R8-04 — P1 qualification: required CI omits private browser and recovery coverage

**Evidence:** `.github/workflows/check.yml:74–93`, `web/playwright.config.mjs:39–42`, `web/e2e/run.mjs`.

The `directs` Playwright project is included only when `BRRP_E2E_PRIVATE_CONVERSATIONS=1`. CI invokes the default suite without that flag. Its required browser job can pass without executing the direct-message journey. CI also runs static source-package checks, but does not require the maintained fresh-recovery/archived-rollback scripts or actual extracted-package consumer execution.

**Acceptance:** default-off and opted-in private browser runs must both be required before packaging, alongside maintained recovery and relevant extracted-artifact checks. Record feature mode in test artifacts. Use isolated data and a pinned, checksum-verified prior archive for cross-version tests. Missing prior artifacts or unavailable dependencies must fail/report an incomplete check, never silently pass. Preserve the default production gate and the existing Docker/native prerequisites. Workflow edits are not a hosted CI pass.

### R8-05 — P1 qualification: private-state recovery is not demonstrated

**Evidence:** `scripts/upgrade-recovery.mjs`, `scripts/upgrade-rollback.mjs`, and the three alpha.6 recovery JSON records.

Existing recovery fixtures exercise community messages/files, replies, pins, invitation/MFA and schema compatibility. They do not explicitly seed and verify DM requests, blocks, direct read cursors, hide/contact preferences or private attachments. General database restoration cannot substitute for behavior-level assertions after restore.

**Acceptance:** add a maintained private-data fresh recovery scenario with accepted and pending/rejected requests, a blocked pair, scoped attachment bytes/search, receipts, cursors, hide/contact preferences and erased-peer history. Log in afresh after restore and assert participant/outsider authorization, consent and block behavior. Verify sessions/push registrations are invalidated. Seed post-snapshot block/role/password/removal changes and demonstrate the required operator reconciliation before cutover; restoring a snapshot cannot reconstruct later decisions by itself. Keep actual alpha.5 and alpha.3 rollback paths. This does not establish off-host recovery.

### R8-06 — P1 release evidence: no final alpha.6 race pass or exact release artifact

There are **no alpha.6 distribution ZIPs**. The preview binary is not the recorded rollback-tested binary:

| Artifact | SHA-256 / identity |
| --- | --- |
| Current `dist/brrpbrrp-alpha6-preview` | `ffd53a04be65dc3fa2a0a403249560fcb9fb6e40bb069f631f7203d4f05ddd63`; embedded revision `44ae4f0`, `vcs.modified=true` |
| Candidate recorded by both rollback reports | `e37309a6f2e7dba244bb48150d5abd153088be3a5221c588374a213baabed894` |
| Identical alpha.5 manifest copies | `562c7153306352568e24dfc9b74d754c26b9d2daa7d0fc0363ae21d809eb29fb` |

Different binary hashes do not prove different behavior or source; build options can change bytes. They do prevent treating those binaries as one tested artifact.

**Acceptance:** after fixes, obtain a green `go test -race -count=1 -timeout 20m ./...`; capture exact command, source identity, result and elapsed time. Build the checkpoint from clean committed source; record executable/frontend/archive identities and toolchain. Run consumer and recovery checks against those exact bytes. Keep prior archives immutable. Any signed/repacked artifact needs its own resulting digest and relevant verification. Do not relabel a source checkpoint as a published release.

### R8-07 — Documentation drift corrected in this review

The master-plan banner still named alpha.5; progress rows understated the implemented private schema/DM/blocking work; README's limits said DMs did not exist; deployment instructions still said schema 6 and universally rejected candidate-created pre-upgrade archives; the production runbook omitted the race timeout and put local macOS recovery under the Windows column.

Current summary/runbook text is corrected alongside this review. ADR 0011's no-DM statement is labeled as its alpha.5 boundary; the decision register now distinguishes the implemented report expiry (90 days after creation) from its older proposal (after resolution). This changes documentation, not policy. Historical release records and prior test results remain unchanged. Compatible format-2/schema-10 pre-upgrade archives were accepted by alpha.5; legacy alpha.3 rejects format 2. Supported rollback still requires a verified pre-upgrade snapshot and the matching retained executable. Compatibility must be measured for the actual pair.

### R8-08 — P3: source-package displayed count includes a blank entry

`scripts/check-source-package.mjs:58` normalizes before removing empty lines, converting a trailing blank into `.`. The known displayed count is inflated by one. This does not invalidate the freshly checked alpha.5 archive hashes.

**Acceptance:** filter blank lines before normalization; cover empty input and Windows/space-containing paths; display the actual number of entries. Keep this small tooling fix separate from privacy claims.

## 5. Immediate implementation tranche — stabilize the alpha.6 integration

This is the next coder's bounded assignment. Product expansion starts after its exit criteria are met or a precise external blocker is documented.

| Order | Deliverable | Dependencies | Required proof |
| --- | --- | --- | --- |
| P8-01 | Fix R8-01 attachment-claim authorization | Current shared message/receipt contract | Permanent regression fails before fix and passes after; permitted text/replay/restoration controls; focused race run |
| P8-02 | Fix R8-02 notification loading and R8-03 request-draft loss | Account/authority invalidation contract | Rendered regressions with real events/refresh, account switch and muted/blocked controls; frontend tests/type check |
| P8-03 | Require both browser modes and recovery/consumer checks in CI | P8-01–02 for a green baseline; pinned old release fixture | Opted-in direct test is visibly selected; default-off path remains tested; package dependencies include all required jobs; actual local execution where possible |
| P8-04 | Add private-data recovery and security-reconciliation fixture | P8-01 and existing restore semantics | Restored API assertions and file hashes; expected post-snapshot loss/reconciliation recorded; failure exits nonzero; preserved old-version paths |
| P8-05 | Establish a reproducible checkpoint; optionally fix R8-08 | P8-01–04 | Full final race pass, full two-mode browser runs, recovery reports, clean-source artifact identities and extracted-consumer evidence; truthful evidence ledger |

P8-01 and P8-02 can run independently with separate file ownership. Recovery fixture work can start alongside them. Integrate before the final baseline to avoid rerunning expensive tests against successive unfinished snapshots. Change schema/version only when justified; do not label this v1 or turn on restricted/private features globally.

**Exit:** all three defects have maintained regressions; CI definitions cover the private journey and recovery/artifacts; private recovery passes; the final local race/browser/recovery baseline is green on the recorded checkpoint. Hosted CI, real hosts and external review remain explicitly pending when unavailable. Record missing infrastructure and ready-to-run steps; continue local work instead of repeatedly requesting unavailable access.

## 6. Ordered roadmap after stabilization

The master plan remains the complete contract. These stages translate it into the next dependency order; they do not silently reduce v1 to DMs.

### A. Make private communication usable and supportable — WP-10, WP-12, WP-14–15

1. **Private evidence reporting.** Let a participant select reportable messages and explicitly preview the evidence disclosed. Give reviewers only that evidence through a separate capability; no general DM browsing. Bound/rate-limit submissions, audit reviewer actions, specify retention/deletion and protect selected file evidence. Cover report-after-block and source deletion.
2. **Request discovery and notification policy.** Add an accessible Messages request/activity count that updates while the modal is closed. Implement persistent activity, per-conversation/account/device precedence, mute/quiet controls, safe private previews and cross-tab/service-worker deduplication. Recheck current consent, membership, block and preferences at dispatch. No request spam, automatic unhide or replay after unblock.
3. **Lifecycle/export contract.** Complete account export and direct/group membership/history rules. Preserve the distinction between deactivation, history-preserving removal and erasure; disclose operator access and backup retention. Test erased peers, report evidence and post-snapshot reconciliation.

**Exit:** ordinary users can discover, accept, decline, block and report contact; notifications match preferences on active/background/closed app paths; outsider administrators still cannot browse private conversations. Minimum moderation/reporting exists before outside private-message pilots.

### B. Consented small groups — WP-11, dependent on A's trust controls

Build on conversation storage and centralized access rather than creating shadow channels or a second message pipeline. Implement invitation/acceptance, a ten-member limit, naming, owner transfer, leave/removal and explicit history admission. Define blocked-user behavior in shared groups before coding it. Cover concurrent invites/acceptance/removal/transfer, the last owner, calls/files/events/search/counts and restored membership. Removed members lose every server delivery path; new members see exactly the disclosed history. Update lifecycle and recovery fixtures with group state.

**Exit:** T-GROUP-01 and the shared private-surface regressions pass, including permitted controls and removal during queued delivery. G2 still requires its full ownership/review acceptance.

### C. Finish the everyday community experience — WP-13–17

- **Threads:** stable identity/title, follow/read state, archive/reopen/lock, correct root deletion/retention and migration of existing reply discussions. All 1,000 replies in the master fixture must be reachable with bounded memory; current 250-reply UI limit is not this acceptance.
- **Moderation:** timeouts, report assignment/actions, controlled multi-use invitations and abuse limits applied across all mutation paths.
- **Composer and member experience:** documented formatting/mention contract, safe rendering, useful profiles, upload progress/cancel/retry, IME/RTL, reliable errors and recoverable drafts.
- **Navigation/accessibility:** keyboard-only critical paths, predictable focus after dialogs/errors, responsive layouts, empty/loading/offline states, zoom/reduced motion and physical mobile keyboard behavior.

**Exit:** journey-level evidence for sending, finding, following, reporting and recovering work; automatic accessibility scans plus human checks; no inaccessible core action. Preserve current privacy and recovery invariants during polish.

### D. Qualify the media design, then build it — WP-18–22

Start the media feasibility work in parallel with A–C: it has the highest architectural uncertainty. Record an ADR and measured proof for separate microphone, screen video and optional screen-audio publications, renegotiation/generation handling, codec/browser support, SFU/resource limits and licensing. Resolve D-03 host support and D-12 media architecture, declare the browser/device support boundary and assign D-05 staffing before committing to device or screen-audio support.

Then implement consented private calls, direct/forced TURN routing (UDP and chosen TCP/TLS support), IPv6 policy, listen-only participation, explicit capture start/stop, moderator/owner controls and viewer admission. Moving room, blocking, revocation or leaving must stop the correct publications. Never silently restart capture.

**Exit:** cross-browser physical-device calls and shares on both target deployments, separate networks and restricted NAT; two-hour call and 100 share-cycle fixtures; late viewers, reconnect, cancellation, room movement and revocation; measured quality/resource results. Current 10-participant server limit stays until a higher limit is measured and accepted. Synthetic RTP regression remains a fast gate, not the physical qualification.

### E. Establish both operational environments and public project ownership — WP-01–03, WP-23–27

Start environment/governance work in parallel now; software work cannot create missing operational evidence.

| Track | Concrete acceptance |
| --- | --- |
| Public project | Canonical repository and maintainers with recovery owners; real private vulnerability intake tested with a harmless report; release/registry/signing identities; code-of-conduct enforcement contact. Preserve recorded MIT/CLA decisions |
| Windows/Tailscale | Extracted candidate: install/UAC cancel, service account/ACLs, start/stop, reboot/sign-in/tray, trusted exact HTTPS origin from another device, upgrade/failed-start rollback, uninstall/data preservation, owner recovery and diagnostics |
| Linux Docker | Exact image digest from clean source; non-root execution, persistent volume/backup permissions, health/start/stop/recreate/reboot, upgrade/rollback/fresh-volume restore and network/media paths |
| Independent recovery | Encrypted independent copy and separate configuration/TLS/key custody; restore on clean Windows and Docker destinations; private/group fixtures; measured RPO ≤24 h and RTO ≤60 min including response/acquisition/decryption/reconciliation; honest post-snapshot loss |
| Observability | Redacted logs and useful CPU/RSS/storage/network/queue signals; independent HTTPS/WebSocket probe; deliberately fail a check and prove the intended operator receives an alert |
| Capacity | Freeze reference dataset/load/hardware; two-hour peak and 24-hour soak with failures; correctness, latency/resource budgets and recovery measured on both hosts. Do not increase advertised limits from unit tests |
| Distribution | Required hosted native/browser/Docker/recovery/consumer jobs; notices/SBOM/provenance; signed artifacts where the support policy requires them; independent clean source build and actual downloaded-package install/upgrade/restore |

A second local directory is not independent storage. Cross-compiling Windows is not a Windows service test. Existing alpha.5 archives are usable baselines, not proof that alpha.6 distributed bytes passed any check.

### F. Independent acceptance, pilots and release — WP-28–33

1. Independent security/privacy review, including private evidence/reporting, new group/media surfaces, authority races, restore reconciliation and file/preview boundaries; resolve critical/high findings and track lesser ones with owners.
2. Chromium/Firefox/WebKit and selected screen-reader/keyboard/physical-phone acceptance against the declared support policy; a non-author installs/restores and another contributor builds from supplied instructions.
3. After required features, hosts and review gates pass, run **14 consecutive days** of beta with at least three communities spanning both deployment paths. Record incidents, lost-work reports, delivery/media failures, backup/alert results and user friction.
4. Freeze the actual distributed release candidate, verify provenance/signatures/digests and compatible upgrade/rollback, then complete **seven days** of final observation. Material changes require the affected checks/observation to be reassessed.
5. Obtain explicit release approval, publish verified artifacts, perform staged rollout and assign maintenance/security/support/rollback owners. A version-string change cannot satisfy this stage.

## 7. Gate ledger and external dependencies

| Gate | Current status | Evidence still required |
| --- | --- | --- |
| G0 baseline | Open | Remaining scope/support/media/identity/operations decisions and named owners/environments |
| G1 privacy foundation | Open | R8-01 repair, complete final evidence and independent interim review of WP-04–08 |
| G2 private integration | Open | Stabilized DMs, groups, lifecycle/export and complete private-surface acceptance |
| G3 community complete | Open | Threads, notification/activity model, moderation and UX/accessibility journeys |
| G4 media complete | Open | Private calls/screen sharing plus physical browser/network qualification |
| G5 operations | Open | Both actual hosts, independent recovery, monitoring/capacity and attributable artifacts |
| G6 beta accepted | Open | Independent reviews, newcomer docs and 14-day communities pilot |
| G7 stable candidate | Open | Exact final artifacts, compatibility and seven-day observation |
| G8 published | Open | Explicit approval, verified publication, rollout and maintenance handoff |

Owner inputs remain necessary for the public repository/identities, test hosts and testers, support/browser/media choices and outstanding master-plan decisions (including D-03, D-05, D-13, D-15 and D-17). Neither the current review nor a coder should invent approvals. They do not block fixing P8 locally. Both deployment targets and MIT/CLA are already established; do not ask the user to choose them again.

## 8. Handoff and evidence discipline

Use [the complete next-coder prompt](handoff-next-coder-review-8-2026-09-26.md). The first deliverable is P8-01–05, with small coherent changes and real regression evidence. If stopped by unavailable infrastructure, finish independent local tasks and leave an exact command/fixture/expected-result handoff for that environment.

For every checkpoint record: source commit and dirty status; feature mode; toolchain; artifact/frontend identity; command and exit status; test count/duration; reproduction before repair; permitted control after repair; and the scope of any skipped/external acceptance. Keep historical failures and hashes. Do not silently replace old archives or convert local results into host/independent approvals.
