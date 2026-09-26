# BrrpBrrp — Fourth Completion Review and Next-Steps Plan

**Date:** 25 September 2026, America/Los_Angeles  
**Candidate:** 0.4.0-alpha.4 · schema 5 · backup format 2  
**Required deployments:** Windows/Tailscale and Linux Docker  
**Task boundary:** audit, verification, implementation plan and coder handoff. No application-source, configuration or deployment changes.

## 1. Assessment

**The latest implementation is stronger, and the existing automated baseline passes. The application remains an alpha and is not production-complete or ready for v1.0.0.**

The direct defects from review 3 received meaningful fixes. The checked-in browser suite now passes without a diagnostic patch, including a non-cooperative voice sender. Backup generations and effective default-role revocation are better modeled. Four additional required-behavior probes nevertheless reproduce remaining privacy and recovery failures:

1. An audience assembled across different database states can receive the new authorization revision and deliver private data to a revoked account.
2. A delayed channel-move response from an old account can replace the next account's channel list.
3. A correctly published backup copy cannot be reconciled after its completion status is lost.
4. An outstanding failed copy can prevent new local snapshots even after the regular backup interval expires.

These are the next implementation priorities. No G0–G8 gate is passed. Existing chat, restricted-channel and voice foundations should be preserved; DMs, groups, named threads, full notification controls and screen sharing remain substantial product work.

### Reading order and authority

- [This review's coder handoff](handoff-next-coder-review-4-2026-09-25.md).
- [Master v1 implementation plan](../docs/v1-implementation-plan.md): approved planning framework, 33 work packages, invariants, scope and gate definitions.
- [Third completion review](completion-review-3-and-next-steps-2026-09-25.md): historical R3 findings and P3 work.
- [Remediation ledger](../docs/completion-remediation-2026-09-24.md) and [progress ledger](../docs/v1-progress.md): implementation history, qualified by the current findings below.

This document supersedes review 3 for the current assessment and immediate execution order. It does not replace the master product requirements. Review documents remain in planning/ because they contain developer source links that do not belong in native distribution documents. Both deployment targets and the recorded MIT/CLA direction remain accepted inputs.

## 2. What has been achieved since review 3

Content comparison found **28 changed files and five additional files** against the previous audit inventory. The additions include three regression-test files and the two previous planning documents. The version remains alpha.4.

| Area | Current implementation and evidence | Remaining boundary |
| --- | --- | --- |
| Client authorization invalidation | Incremental message, unread and snapshot completions now check authorization generation; invalidation clears message revisions and withholds counts while authority is unverified. Frontend tests increased from 68 to 73. | Account/session identity is not captured by every caller; R4-02. Rendered failed-refresh and narrow-role cases still need closure. |
| Effective default-role holders | Role mutation includes accounts that implicitly hold the default role, and cancels streams according to their resulting access. | Preserve unaffected-account controls; complete delivery ordering remains open under R4-01. |
| Delivery revision and stream admission | Hub events carry an authorization revision; invalidation records committed revisions; registered streams recheck session/account authority. | Multiple audience queries do not share one snapshot. A revision label alone does not prove coherent authority or write admission. |
| Backup generation state | Local, copied and recoverable generation fields are separated. Failed copies have retry accounting and retain the previous recoverable identity and archive. | Restart reconciliation and snapshot scheduling during copy outages fail; R4-03/04. |
| Snapshot age | Snapshot time is taken before database vacuum/compression rather than at archive completion. | Health must report the snapshot age of the actual independently recoverable generation. |
| Browser harness | Voice locator now follows the real dialog lifecycle, with cleanup and independent project dependencies. | Physical devices, other browsers and real network routes remain unqualified. |
| Voice enforcement | Synthetic WebRTC test verifies speaker outbound RTP continues while listener inbound RTP stops after speak revocation; regrant restores delivery. | Stronger local server-enforcement evidence, not acoustic, forced-relay or host acceptance. |
| Earlier foundations | Owner/credential transaction fixes, archive coverage validation, migration recovery, scoped management and package tooling remain present. | Independent review and platform qualification are still required. Do not redo already fixed cases without a regression. |

Relevant changes include [effective-role regressions](../internal/api/effective_role_revocation_test.go), [hub regressions](../internal/ws/hub_test.go), [snapshot-time regression](../internal/backup/snapshot_time_test.go), [workspace state](../web/src/lib/workspace.svelte.ts), [authorization helpers](../internal/api/authz.go) and [backup scheduling](../internal/api/backup_schedule.go).

### Claims in older ledgers that need qualification

The progress ledger's claim that a restricted audience computed before revocation cannot be delivered afterward is too broad: R4-01 reproduces stale audience data stamped with the current revision. Its claim that production-check names the previous recoverable generation on copy failure also needs correction: the general error branch returns before that fallback branch in the ordinary failed-copy case.

Conversely, older text that says there is no negotiated RTP evidence is now stale. The fresh checked-in voice journey includes negotiated RTP and a non-cooperative sender control. Record these distinctions rather than calling the whole foundation either complete or absent.

## 3. Fresh verification

| Check | Result | Meaning |
| --- | --- | --- |
| Frontend unit suite | **73 passed** | Existing unmodified tests |
| Svelte/TypeScript | **0 errors, 0 warnings** | Current component/type checks |
| Frontend formatting and production build | **Passed** | Current source |
| Go vet | **Passed** | Static checks |
| Full uncached Go race suite | **Passed, exit 0** | go test -race -count=1 ./...; API package 497.284 seconds |
| Dependency-notice freshness | **Passed** | 39 components, 20 license texts |
| Docker build-input check and self-test | **Passed** | Static checks only; no Docker Engine available |
| Source-package checker | **Passed** | 313 source entries, 34 native documents |
| Checked-in browser suite | **5 passed, 51.1 seconds** | Release, authority, privacy, voice and ownership; no test patch |
| Maintained cross-version rollback | **53 passed, exit 0** | Actual archived alpha.3 executable and current build |
| Current executable package smoke | **Passed** | CLI setup, backup/restore, lock, health, production guard, embedded UI/manifest, graceful stop |
| Additional API contract probes | **Three failed as expected for the findings below** | R4-01, R4-03, R4-04 |
| Additional client contract probe | **One failed as expected for the finding below** | R4-02 |

The rollback rehearsal starts with old schema 2/backup format 1, upgrades to schema 5/format 2 and restores the old executable's compatible archive into a clean folder. Its fixture includes two users, three messages, a reply, pin, invitation, file and MFA. It explicitly verifies loss of post-snapshot message/ban changes after rollback. The damaged-index/incomplete-upgrade case is injected state, not a process killed during migration. The old release lacks a role API; that coverage limitation is recorded rather than counted as a failed candidate assertion.

The review-3 clean extracted-source rebuild/repackage remains historical evidence; that full archive-consumer exercise was not repeated here. This review freshly rebuilt and smoke-tested the current executable.

### Snapshot and reproduction

The workspace has no Git checkout, so no reviewed commit or hosted-CI provenance can be asserted. The starting manifest contains 323 files; canonical manifest SHA-256:

    3e39510719da949d847d5276e1e4213f01051df5ae9870b3f38fafd75a8256a1

Disposable audit directory:

    /private/tmp/brrpbrrp-review4-4vd89phc

The three API probes are in internal/api/review4_audit_test.go within that copy; run go test ./internal/api -run '^TestReview4' -count=1 -v there. The client probe is web/tests/review4-audit.test.mjs; run node --test tests/review4-audit.test.mjs from that copy's web directory. These files are diagnostic artifacts, not permanent project tests; temporary storage may be removed. The next coder must reconstruct maintained regressions from the scenarios below if necessary.

Build/test caches and the diagnostic application copy stayed in temporary directories. Final manifest comparison confirms all 323 pre-existing inventoried files are unchanged. The only intended workspace additions are this report and its handoff.

## 4. Remaining findings

### R4-01 — Audience data and its revision do not share a coherent snapshot

**Priority: privacy blocker. Evidence: deterministic API interleaving and actual socket delivery.**

[authz.go](../internal/api/authz.go), approximately lines 315–330 and 373–395, reads channel, grants, account roles and ownership/revision in separate queries. publishToChannel passes the store rather than a shared read transaction. AuthorizationSnapshot reads the revision after earlier audience inputs. The hub can discard an old revision, but cannot recognize old authority data stamped with the new revision.

**Reproduction:** create a restricted channel and an allowed member with a real WebSocket. Wrap the existing query interface to pause at the final revision query. Revoke that member through the real channel-access API after earlier audience reads, then resume. The next history request returns HTTP 404, but the resolved audience still includes the member and carries current revision 3. Broadcasting the resolved audience through BroadcastToRevision delivers the restricted test payload on the actual socket.

This is controlled scheduling through an existing query interface; no production test hook was added and no claim is made that an ordinary request happened to hit this timing during this audit.

**Required change:** obtain channel state, grants, effective roles, ownership, account state and revision from one consistent snapshot, or use a rigorously validated revision protocol. Audit every audience and eligibility helper. Define the send-admission boundary against committed authority, including a commit whose invalidation notification has not yet reached the hub.

The WebSocket writer's separate stale check and network write also require an explicit contract and targeted interleaving test. Do not silently claim that queue filtering proves that boundary. Do not hold a global database lock across an unbounded network write.

**Acceptance:** force revocation between each relevant query, between commit and invalidation, and between dequeue and write admission. Cover direct grants, default/explicit roles, account status, session revocation and owner transfer as applicable. Denied recipients receive no newly admitted private payload; unaffected recipients still receive permitted events. Regrant recovers without reconnect-only workarounds. Preserve the documented treatment of writes already admitted.

### R4-02 — A delayed mutation can cross an account switch

**Priority: client privacy/account-isolation blocker. Evidence: deterministic client-model failure.**

[workspace.svelte.ts](../web/src/lib/workspace.svelte.ts), approximately lines 141 and 296–310, defines admitted(generation, session = this.sessionVersion). moveChannel captures only authorization generation and invokes that default after awaiting its response. stop increments sessionVersion, but a new account can enter without changing authorization generation.

**Reproduction:** defer a channel move, stop the previous workspace, enter another account with the same generation, then resolve the old move with a private old-account channel. The new account's expected channel list [a, b] becomes [old-account-private-channel]. This review tested the state model, not a rendered two-account browser journey.

**Required change:** make request admission require identity captured before the await: account/session epoch, authorization generation and resource/request version where necessary. Remove defaults that substitute current identity for the original caller. Audit mutation success, error, finally, loading flags, toasts and follow-up refreshes, not only read responses.

**Acceptance:** sign out/in to a different account with identical capabilities/revision while each relevant request is pending. Old results cannot alter the new account's data or UI state. Permitted same-account operations still succeed; invalidation/regrant, navigation and failed-refresh recovery remain functional. Add a rendered account-switch regression in addition to focused store tests.

### R4-03 — Published copies cannot recover from lost completion status

**Priority: recovery blocker. Evidence: real archive/copy retry failure.**

[backup_schedule.go](../internal/api/backup_schedule.go), approximately lines 134–151 and 261–265, publishes the independent copy before saving completion status. copyBackup rejects any existing final name. A crash or status-write failure after publication therefore leaves a valid copy that retries will permanently reject.

**Reproduction:** create a locally verified generation with a failed copy. Publish the correct copy using the real copy helper but leave its completion ledger pending, simulating loss of the post-publication status. Retry. The call returns nil, CopyAttempts rises to 2, RecoverableID stays empty, and the warning says “backup copy already exists.”

**Required change:** make completion idempotent. Reconcile an existing regular final file only after verifying the expected generation, digest and archive validity. Preserve conflicting/corrupt files for explicit handling; do not overwrite them silently. Make status durability, publication and retention ordering explicit.

**Acceptance:** correct existing copy is recognized after restart; wrong digest, malformed archive, symlink/non-regular target and partial publication are not accepted. Inject failures before/after publication, status persistence and pruning. A verified fallback remains identifiable and protected; retries converge without duplicate generations or false recovery success.

### R4-04 — Copy failures can starve scheduled local snapshots

**Priority: recovery freshness blocker. Evidence: due scheduled backup reuses an old generation.**

[backup_schedule.go](../internal/api/backup_schedule.go), approximately lines 188–198, returns after retrying any outstanding copy, even when a fresh local backup is due. The shared LastAttempt scheduling clock compounds this behavior.

**Reproduction:** create a verified local snapshot with an unavailable copy destination. Add a new message. Set the last attempt to 25 hours ago for a 24-hour schedule and exhaust the short copy retries. backupDue correctly returns true. scheduledBackup nevertheless retries the original copy, retains the same generation ID, increments CopyAttempts to 5 and advances LastAttempt; the new message is absent from a fresh snapshot because no fresh snapshot was made.

This establishes a stale recovery-point risk during an outage, not observed permanent data loss.

**Required change:** separate local snapshot deadlines from copy retry deadlines. Continue the regular local snapshot cadence during a prolonged independent-storage outage. Specify a bounded pending-copy/coalescing policy and disk-reserve behavior. Retain the verified independently recoverable fallback. Copy repair must not reset the next local backup deadline for another full interval.

**Acceptance:** fake-clock tests span multiple backup intervals, restart, retry exhaustion, copy recovery, retention pressure and manual backup requests. New local generations include later writes while copy failures remain truthfully degraded. Pending work stays bounded and a recovered destination converges to an appropriate current generation.

### Related operational reporting gap

In [production.go](../internal/app/production.go), checkGeneration returns on record.Error before its missing-copy branch can identify the saved recoverable fallback. Its fallback status view also lacks enough snapshot/coverage data to establish that fallback's own recovery age.

Report separately: last attempt/error, newest verified local snapshot, newest verified independent recovery point, its actual snapshot age and coverage, and any failed validation. A failed newest copy may coexist with an older useful recovery point; neither fact should hide the other. No green status should be derived from file mtime or copy completion time alone. This is source-confirmed behavior related to R4-03/04, not a fifth independently executed probe.

## 5. Next implementation packages

Roles below identify responsibilities, not assigned people: BE backend, FE frontend, QA verification, OPS operations, TL technical lead, SEC independent security reviewer, REL release maintainer.

### P4-01 — Coherent authorization and committed delivery admission

**Owners:** BE + QA; TL reviews contract; SEC reviews evidence.  
**Depends on:** current foundation. **Closes:** R4-01.

1. Inventory all channel audience/eligibility and queue/writer admission paths.
2. Write the exact snapshot and committed-revision contract before changing locking.
3. Implement one reusable coherent resolution boundary and carry its revision through dispatch.
4. Add deterministic query/commit/dispatch interleavings and permitted-recipient controls.
5. Update the route/delivery evidence ledger and correct blanket ordering claims.

**Exit:** reproduced socket disclosure is prevented, broader commit/invalidation/write interleavings meet the stated contract, race suite passes and no deadlock or global network-write lock is introduced.

### P4-02 — Mandatory captured client identity

**Owners:** FE + QA. **Can proceed independently of P4-01. Closes:** R4-02.

1. Define a captured request token; disallow default-current-session admission.
2. Audit every async completion that writes protected state or operation status.
3. Add account-switch, navigation, revocation, failed-refresh and regrant tests.
4. Finish rendered category-only/role-only management and permission-loss journeys where they remain uncovered.

**Exit:** old account completions cannot affect the new workspace; permitted controls pass; current 73-test and five-browser baseline remains green. Tests should assert behavior, not merely token fields.

### P4-03 — Restart-safe independent-copy completion

**Owners:** BE + OPS + QA. **Closes:** R4-03.

1. Specify transitions from verified local archive through copy staging, final publication, verified completion, durable status and retention.
2. Reconcile correct existing final copies; fail safely on conflicts.
3. Persist recovery identity before pruning anything needed by the current durable ledger.
4. Add restart/status-failure tests at the meaningful transitions.

**Exit:** correct copy plus missing status converges to verified recovery success; invalid targets never do; failures preserve a usable, verifiable fallback.

### P4-04 — Independent backup clocks and truthful recovery health

**Owners:** BE + OPS + QA. **Depends on:** P4-03 state contract. **Closes:** R4-04 and the related health gap.

1. Separate snapshot cadence, copy retry cadence and user-requested backup semantics.
2. Define bounded pending generations, retry exhaustion, disk reserve and retention.
3. Carry fallback snapshot/coverage/identity into health validation and operator output.
4. Add multi-interval outage/restart tests and healthy recovery controls.

**Exit:** fresh local recovery points continue during an offsite outage; copied recovery remains accurately degraded; newest/fallback identities and ages are explicit; retry recovery cannot defer the next snapshot incorrectly.

### P4-05 — Qualify both supported deployment paths

**Owners:** OPS + QA + REL. **Preparation can proceed now; final candidate evidence depends on P4-01–04.**

Execute the matrix in section 7 on actual Windows/Tailscale and Linux Docker hosts. Bind evidence to exact binaries/images and configuration. Establish a canonical Git history and hosted CI/release identities before asserting provenance. Record signed artifacts and a clean consumer verification path when available.

**Exit:** both host records contain real lifecycle/recovery evidence, independent-copy/key recovery and alert delivery results. A missing host, key workflow, signing identity or reviewer stays explicitly unqualified with an owner and next action. Cross-compilation and static Docker checks do not close this package.

### P4-06 — Consolidate the foundation and resume product work

**Owners:** TL + QA + SEC; product scope decisions owned by project owner.

1. Update remediation/progress records with the four new regressions and fresh passing baseline.
2. Obtain independent foundation security/privacy review against the actual implementation and delivery contract.
3. Close remaining G0 decisions/reference-environment ownership and assess G1 evidence against the master plan.
4. Prepare WP-09's shared conversation schema, idempotent-send/outbox, read-cursor and migration ADR; then implement according to the foundation gate.
5. Keep private features gated until their full authorization contract is verified. Do not mark G1 passed merely because these four cases are fixed.

**Exit:** one consistent evidence ledger, explicit remaining external dependencies, justified G0/G1 decisions, and a reviewable WP-09 implementation starting point.

### Execution order

P4-01 and P4-02 are separate code areas. P4-03 precedes P4-04 because they share backup state. Host/reviewer provisioning and WP-09 design preparation can progress while those fixes are implemented. Final host qualification and foundation acceptance use the corrected candidate. This dependency description does not require multiple coding agents.

Do not assign a calendar completion date until host access, reviewers and the four fixes are estimated from their actual designs. The master plan's broad estimates remain provisional; repeated small fixes do not establish a percentage of v1 completion.

## 6. Remaining path to v1.0.0

| Master packages | Current assessment | Next completion evidence |
| --- | --- | --- |
| WP-01–03: project, decisions, environments | License/contribution/security documents and ADRs exist; repository identity, hosted CI, some decisions and reference hosts remain open. | G0 decision record, canonical history, maintainer recovery and environment inventory |
| WP-04: migrations/durability | Strong local implementation and maintained 53-check rollback evidence | Both-host upgrade, interruption and recovery fixtures |
| WP-05: identity/session/recovery | Backend/UI/offline paths and regressions implemented | Keep regressions; complete host and delivery-boundary qualification |
| WP-06–08: roles/privacy | Substantial implementation; foundation remains blocked by R4-01/02 and independent review | Complete surface/transition evidence, narrow-role UX and G1 review |
| WP-09: shared conversations/client state | Existing channel state is a foundation; v1 shared model, durable send/outbox and receipt contract remain | Schema/migration ADR, deduplication under lost acknowledgments, account-scoped drafts/outbox, monotonic reads |
| WP-10–11: DMs/groups/blocking | Not implemented to the planned v1 contract | Canonical pair, consent, group ownership/membership, blocking and private-call eligibility |
| WP-12: lifecycle/export | Not implemented to the planned contract | Retention, erasure/tombstones, exports and restore reconciliation |
| WP-13–15: threads/inbox/moderation | Replies/basic activity/admin controls exist; named threads, full inbox/preferences, reports/timeouts/slow mode remain | G3 journeys and abuse/notification matrices |
| WP-16–17: composer/navigation/accessibility | Existing chat/PWA UI provides a base; complete v1 journeys unqualified | Scoped draft/upload/retry UX, keyboard/focus/mobile and assistive-technology evidence |
| WP-18–19: media spike/voice reliability | Existing voice and synthetic revocation transport test | Architecture decision, private calls, real devices, routes and reconnect quality |
| WP-20–21: publications/screen sharing | Not implemented | Publication/signaling model, scoped screen capture, lifecycle/permissions and share UX |
| WP-22: media qualification | Local Chromium evidence only | Cross-browser/network/room-scale and long-call results |
| WP-23–24: backups/host lifecycle | Extensive local tooling; R4-03/04 block complete recovery claims | Crash-safe scheduling/copy state plus actual Windows/Docker restore and update |
| WP-25–27: monitoring/capacity/release | Health, scripts and packaging foundations exist | External alert receipt, load/soak, hosted CI, signed artifacts and provenance |
| WP-28–31: review/accessibility/beta/docs | Documents exist; independent acceptance and beta not complete | Security review, physical accessibility/browser acceptance, 14-day beta, newcomer rehearsals |
| WP-32–33: candidate/release | Not started | Frozen candidate, seven-day final observation, explicit publication approval and maintenance handoff |

Product sequence after foundation acceptance: shared conversation/state contract → DMs and consented groups → lifecycle/export → named threads, inbox and moderation → complete community UX. Media architecture can be investigated alongside those streams, followed by scoped publication/signaling and screen sharing. Operations and documentation continue throughout. Preserve the master plan's explicit post-v1 exclusions; “Discord competitor” is not permission to silently add unbounded feature scope.

**Gate status:** G0 baseline, G1 privacy, G2 private messaging, G3 community, G4 media, G5 operations, G6 beta, G7 candidate and G8 release are all **not passed**. This is an evidence assessment, not a claim that no useful implementation exists.

## 7. Both-host acceptance matrix

| Area | Windows/Tailscale evidence | Linux Docker evidence |
| --- | --- | --- |
| Fresh install | Actual selected OS build, service identity/ACLs, UAC/tray behavior and first setup | Actual distribution/runtime, image digest, fresh volume and service UID/permissions |
| Networking | TLS/Tailscale trust, intended exposure, TCP/WebSocket and media reachability | Reverse proxy, HTTPS/WebSocket, published ports, direct/relay media routes |
| Lifecycle | Start/stop, reboot, cancellation, interrupted update, rollback, uninstall/reinstall with intended data preservation | Restart/recreate, health/readiness, interrupted image update, rollback and volume preservation |
| Recovery | Independent archive onto a clean destination using documented key/recovery materials | Independent archive into a fresh volume/container using only documented materials |
| Failure behavior | Disk pressure, permissions, lost copy mount, conflicting copy and credential recovery | Read-only/unavailable volumes, disk pressure, container interruption and copy-destination faults |
| Observability | External monitor detects failure and an operator receives the alert | Container health plus external monitor/alert receipt; do not rely on the failed service to report itself |
| Capacity/media | Frozen reference dataset, load/soak, physical clients, forced relay and supported browser routes | Same declared workload and quality budgets, recorded host resources |
| Release consumer | Obtain exact candidate, verify checksum/signature, install and upgrade | Pull exact image/source, verify provenance/digest, install and upgrade |

For recovery, retain the master plan's proposed **RPO ≤24 hours and RTO ≤60 minutes** until ratified against reference data. They are targets, not measured results. The proposed 12-hour backup/copy cadence and 18-hour warning/24-hour critical alerts provide operating margin. Recovery age starts at the usable snapshot. Start the RTO clock at the declared recovery event, including operator response and the steps required to obtain/decrypt/restore/verify/reconcile before cutover; report total outage separately. Reconcile post-snapshot security changes and disclose changes that cannot be reconstructed. A second directory on one laptop is not independent recovery evidence.

Docker is unavailable in this audit environment. No actual Windows host, independent off-host restore, physical audio/screen-reader test, hosted signing pipeline, independent security acceptance, target-scale soak or beta observation was completed here.

## 8. Evidence and completion rules for the next coder

For each package, record the candidate/source identity, environment, exact command, exit status, behavioral assertion, permitted control, retained artifacts and limitations. Use these distinct statuses: implemented; locally verified; platform qualified; independently reviewed; release accepted.

Permanent regressions must reproduce the four scenarios above against the pre-fix behavior. Prefer explicit barriers/fake clocks and actual effect assertions over sleeps or tests that repeat implementation details. Preserve the existing regression coverage and real RTP sender control. Run focused tests during development, then the affected broader gates and final baseline; do not repeatedly rebuild unrelated artifacts without a concrete risk.

The next iteration is complete when:

- [ ] R4-01 coherent authorization and delivery-admission contract is implemented and tested.
- [ ] R4-02 captured account/session identity covers all protected async completions.
- [ ] R4-03 valid published copies reconcile safely after lost status.
- [ ] R4-04 regular snapshots continue during copy outages with bounded storage and accurate health.
- [ ] Existing frontend, race, browser, rollback and package gates pass on the corrected candidate.
- [ ] Progress/remediation claims match evidence, including the improved voice result.
- [ ] Both-host qualifications are executed where access exists; remaining access-dependent work has an explicit owner/action.
- [ ] Independent foundation review and G0/G1 decisions are recorded without self-certification.
- [ ] The next product package is WP-09 with a concrete design and acceptance sequence.

No application fix was performed by this review. The diagnostic failures and this implementation plan are the handoff, not a production-ready claim.
