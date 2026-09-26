# BrrpBrrp — Seventh Completion Review and Execution Plan

**Date:** 25 September 2026, America/Los_Angeles  
**Candidate:** 0.4.0-alpha.4 · schema 5 · backup format 2  
**Required deployments:** Windows/Tailscale and Linux Docker  
**Scope:** completion review, targeted verification, next-steps plan and coder handoff. No application changes or deployment.

## 1. Assessment: implementation is unchanged

**No implementation progress has occurred since review 6 in this workspace.** All 329 files inventoried at that review match their recorded content hashes. The only additional planning files are the review-6 report and handoff; incidental local tool metadata does not represent application work.

The three review-6 diagnostic cases were rerun against a disposable copy whose inventoried files also match the current workspace. **All three failures still reproduce.** The WP-09 design contradictions remain in the master plan. The next coding task should implement the existing P6 packages, beginning with authorization/session ordering and backup durability.

The project remains an alpha. **No G0–G8 release gate has passed.** Existing local functionality and verification remain valuable, but neither a production-ready claim nor a v1 completion percentage is supported.

### Authoritative documents

- [Review-7 coder handoff](handoff-next-coder-review-7-2026-09-25.md): current execution prompt.
- [Review 6](completion-review-6-and-next-steps-2026-09-25.md): detailed reproductions, P6 packages and acceptance criteria.
- [Master v1 implementation plan](../docs/v1-implementation-plan.md): scope, invariants, all 33 work packages and release gates.
- [Progress ledger](../docs/v1-progress.md) and [remediation ledger](../docs/completion-remediation-2026-09-24.md): historical implementation records, qualified by review 6's remaining findings.

This review updates the evidence and execution order. It deliberately retains **R6-01–04 and P6-01–06** so unchanged work is not mistaken for new findings. Both deployment targets and the recorded MIT/CLA direction remain accepted inputs.

## 2. What has been achieved so far

These are cumulative achievements, not new changes in this review:

| Area | Implemented or locally demonstrated | Remaining completion boundary |
| --- | --- | --- |
| Core community app | Existing channel messaging, uploads, replies, search, reactions, PWA and voice foundations | Full planned v1 community and private-conversation experience |
| Identity and administration | MFA/recovery, sessions, ownership ceremony, scoped management and audit transactions | Complete credential/session delivery ordering and actual host recovery |
| Authorization | Central capabilities, roles, restricted channels, coherent audience snapshots, effective default-role handling and revocable delivery | R6-01 lock ordering, R6-02 session commits/registration, remaining UI evidence and independent review |
| Client privacy | Captured request identity, authorization invalidation, stale-response guards and rendered account-switch regression | Remaining rendered narrow-role/failed-refresh cases and complete callback coverage |
| Voice privacy | Public and private presence use capability-aware audiences; synthetic RTP test includes a non-cooperative sender and regrant | Physical devices, forced relay, supported browsers/networks, private-call/publication design and screen sharing |
| Recovery | Versioned verified archives, copy reconciliation, separate snapshot/copy clocks, failure status and maintained rollback rehearsal | R6-03 combined failure, real storage/restart matrix and independent encrypted restore |
| Packaging/evidence | Local build/static packaging checks, dependency notices and updated implementation ledgers | Canonical Git history, hosted CI, signing/provenance, actual target hosts and release-consumer acceptance |
| WP-09 preparation | A conversation/receipt/outbox/read-cursor design note exists | R6-04 contradictions, exact ADR/migration contract and implementation |

There is still no Git checkout in this workspace and no Docker executable available here. No new host, off-host recovery, external alert, independent review, load/soak, beta or stable-release evidence was supplied or found in the unchanged tracked documents.

## 3. Evidence: fresh checks versus prior results

### Fresh in review 7

| Check | Result |
| --- | --- |
| Review-6 content comparison | **329 of 329 inventoried files unchanged** |
| Diagnostic-copy source comparison | **No differences from current inventoried files** |
| R6-01 lock-order probe | **Failed required behavior again:** commit and invalidation wait on opposite resources; transaction cancellation releases the cycle |
| R6-02 credential/socket probe | **Failed required behavior again:** session deletion committed, old-cookie HTTP returns 401, actual socket still receives the private payload |
| R6-03 combined backup-failure probe | **Failed required behavior again:** durable status retains the old generation but its local archive is missing after retention |
| R6-04 specification inspection | **Contradictions remain:** expired-key reuse, idempotency scope, outbox clearing and migration mechanics |

The diagnostic command completed in 2.173 seconds with exit 1 because all three required-contract assertions failed:

    go test ./internal/api -run '^TestReview6' -count=1 -v

It ran in:

    /private/tmp/brrpbrrp-review6-probes-73ai8fwv

The file is internal/api/review6_audit_test.go in that disposable copy. It uses existing hooks, helpers and locks without changing application logic. These temporary tests are not yet maintained project regressions; the implementing coder must add permanent tests and permitted controls.

### Most recent complete baseline: review 6

The following results are **carried forward from review 6 on identical source**, not freshly rerun in review 7:

- 77 frontend tests passed.
- Svelte/TypeScript: zero errors and warnings; formatting/build passed.
- Go build/vet and the full uncached race suite passed; API package 531.358 seconds.
- Six unmodified browser journeys passed in 53.7 seconds, including RTP withdrawal/regrant and account switching.
- Maintained cross-version rollback: 53 checks passed.
- Notices: 39 components/20 texts; static Docker input and source-package checks passed.

The unchanged-source comparison and fresh blocker probes were sufficient for this update. Repeating the whole baseline would not demonstrate implementation progress. After fixes, the coder must run the affected tests and the appropriate final baseline.

Review-6 evidence remains limited to local macOS and synthetic browser/media conditions. Its rollback fixture used a real old executable but injected the interruption state; it did not measure power-loss recovery, actual Windows/Docker lifecycle or off-host RPO/RTO.

### Audit identity

The review-7 starting inventory contains 331 files, including the two review-6 planning documents. Canonical path/digest manifest SHA-256:

    80670d25ac3f9c71f99eaf40f42b668a677c19039c19d112ff3531113abbb607

Snapshot:

    /private/tmp/brrpbrrp-review7-w2nbwofn

Only this review and its handoff are intended workspace additions. Application source, configuration and existing documents remain unchanged.

## 4. Open findings and exact closure requirements

### R6-01 — Lock-order cycle

commitAuthorityChange holds the database connection and then waits for the authorization lock. invalidateWorkspace/publishAuthorizationChange hold that lock and then wait for the database connection.

**Close when:** one documented acquisition order covers commits, standalone invalidation and admission; deterministic concurrent operations complete without cancellation; rollback/error cases work; network I/O remains outside the critical section; post-commit refusal and pre-commit admitted-write controls pass.

Primary source: [authz.go](../internal/api/authz.go). The full controlled scheduling procedure is in review 6.

### R6-02 — Credential/session commits bypass admission ordering

The password-reset path commits removal of sessions before delayed stream cleanup, socket disconnection and invalidation. A queued private event can be admitted during that gap.

**Close when:** every relevant credential/session/account transition and socket registration uses the correctly ordered protocol, with a route-to-commit/invalidation inventory. Test reset/change, MFA changes/recovery, logout, revoke-one/all, ban/deactivation and ownership as applicable. Affected sockets must refuse new admission after commit; permitted sibling sessions/users must continue.

Primary sources: [admin.go](../internal/api/admin.go), [auth.go](../internal/api/auth.go), [mfa.go](../internal/api/mfa.go), [websocket.go](../internal/api/websocket.go). Existing successful channel-revocation tests must remain intact.

### R6-03 — Combined copy/status failure prunes durable local identity

With retention one and no prior successful independent copy, ordinary copy failure can reach local retention before the new local generation is durably recorded. A failing final status write leaves the ledger naming the archive already pruned.

**Close when:** explicit persistence success controls retention, and both local and independent identities needed by the durable record remain backed by verified files through paired failures. Reopen with a new store/server. Cover INSERT/UPDATE/final-save failures, unavailable/corrupt/partial copy destinations, no prior independent copy and recovery convergence. Preserve bounded storage and separate scheduling clocks.

Primary source: [backup_schedule.go](../internal/api/backup_schedule.go). The current code reports a persistence error; the remaining failure is destructive retention ordering, not a false-success claim.

### R6-04 — WP-09 design conflicts

Resolve the new prerequisite note against sections 5.5/5.8 and the actual schema:

1. Expired original operations must not silently become new sends; explicit new intent needs a new operation identity.
2. Reusing an operation key with a different destination must conflict. Current message uniqueness is per account/key, not per channel.
3. Preserve opt-in persistence and recoverable user-authored unsent text while canceling unsafe automatic retries and removing revoked server content.
4. Specify actual migration stages for the existing NOT NULL channel foreign key, conversation mapping, dual readers/writers, indices/FTS, backfill and rollback.
5. Include account-deletion/tombstone prerequisites before private conversation enablement.

**Close when:** one D-11 ADR states exact constraints, API outcomes, expiry/admission and migration behavior without contradicting the master invariants. This is a design blocker; no new DM/outbox implementation exists to test yet.

## 5. Updated next-steps execution plan

### Step 1 — P6-01: finish authorization/session ordering

**Responsibility:** backend + QA; technical lead reviews the protocol.

Deliver:

- A lock/transaction/admission ADR and a complete authority/session transition inventory.
- The R6-01/02 fixes.
- Permanent deterministic regressions, real-socket assertions and unaffected-session controls.
- Focused API/hub/authentication tests followed by the full uncached race suite.

**Dependency:** none beyond the current code. **Exit:** both findings satisfy section 4 and the existing channel/role/voice privacy regressions still pass.

### Step 2 — P6-02: complete backup durability under paired faults

**Responsibility:** backend + operations + QA. Can proceed independently of Step 1.

Deliver:

- Explicit state transitions for local generation, independent fallback, pending copy, persisted status and retention eligibility.
- R6-03 fix and a combined-failure/restart matrix.
- Verified surviving archives for every recorded identity.
- Passing existing archive/cadence/copy reconciliation and rollback checks.

**Exit:** paired copy/status faults cannot remove the recovery point the durable ledger names.

### Step 3 — P6-03: reconcile WP-09 before implementation

**Responsibility:** technical lead + backend/frontend + QA.

Deliver D-11 with exact schema, operation uniqueness/intent, expiry and pruning, lifecycle prerequisites, compatibility stages, opt-in outbox recovery, visible-message read-cursor rules and a scenario-to-test mapping. Resolve drafting contradictions from existing requirements; request owner input only for genuinely open product decisions.

**Exit:** R6-04 is closed and the next vertical slice is reviewable. Design preparation can proceed alongside Steps 1–2.

### Step 4 — P6-04: finish UI evidence and update ledgers

**Responsibility:** frontend + QA.

Complete rendered category-only/role-only management and failed-refresh scenarios with search, pins, thread/reply and preview surfaces open. Exercise remaining stale success/error/finally and account/session cleanup paths.

Preserve the six browser journeys and real RTP sender control. Update progress/remediation records after implementation; distinguish scenario-level fixes from full contracts, independent review and release acceptance.

**Exit:** missing UI obligations have explicit results and no blanket completion claim exceeds the evidence.

### Step 5 — P6-05: qualify both hosts and request independent foundation acceptance

**Responsibility:** operations + release + QA; independent security reviewer and owner for their decisions.

Prepare host/reviewer access while local fixes proceed. Final qualification must use corrected, identifiable artifacts:

| Required evidence | Windows/Tailscale | Linux Docker |
| --- | --- | --- |
| Installation and exposure | Actual OS/service/ACLs, setup and TLS/Tailscale | Actual runtime/image/UID/volumes, proxy/TLS |
| Lifecycle | Reboot, interrupted update, rollback, data preservation | Restart/recreate, failed image update, rollback, volume preservation |
| Recovery | Independent encrypted archive onto clean host | Independent encrypted archive into fresh volume |
| Faults and alerts | Disk/permissions/copy failure and external alert receipt | Volume/container/copy failure and external alert receipt |
| Consumer identity | Exact source/package, checksum/signature and install/upgrade | Exact image/source digest/provenance and install/upgrade |

Establish canonical Git history, maintainer recovery and hosted CI. Retain physical-device/media/accessibility, reference-load and independent review requirements.

**Exit:** actual results or explicitly owned access dependencies, with evidence-based G0/G1 decisions. Local directories and cross-builds do not satisfy host/off-host acceptance.

### Step 6 — P6-06: implement the bounded WP-09 slice

After Step 3 and the applicable foundation prerequisites, implement conversation mapping/lifecycle schema → compatibility readers/writers → deletion-safe receipts → opt-in account-scoped outbox → read-cursor/reconnect integration.

Accept using legacy upgrade, lost acknowledgement → deletion → retry, expiry, changed destination/intent, account switch, revoked destination and staged-attachment scenarios. Preserve existing channel behavior. Build DMs, consented groups and blocking on those primitives afterward.

**Exit:** a working channel-based shared-conversation slice, with no premature private messaging or private-call claim.

### Task boundaries and handoff rules

The next coder's first delivery should contain fixes and maintained tests for Steps 1–2, plus the reconciled design from Step 3. Continue other authorized local work where practical; list actual external dependencies separately. No rewrite, stack migration, new deployment decision or relicensing is required.

Do not create another completion report as a substitute for those changes. Update evidence once meaningful work has landed. Keep source-linked audit documents in planning/ to preserve native-package validation.

## 6. Remaining path to v1

| Gate | Status | Main work ahead |
| --- | --- | --- |
| G0 baseline | Not passed | Source identity, environments, owners and remaining decisions |
| G1 privacy foundation | Not passed | R6-01/02, complete surface/UI evidence and independent acceptance |
| G2 private messaging | Not passed | WP-09–12 shared model, DMs/groups/blocking and lifecycle |
| G3 community | Not passed | Named threads, inbox/preferences, moderation and complete UX |
| G4 media | Not passed | Private-call/publication architecture, screen sharing and device/network qualification |
| G5 operations | Not passed | R6-03, both hosts, independent recovery, monitoring, load and provenance |
| G6 beta | Not passed | Independent security/accessibility review, newcomer drills and 14-day beta |
| G7 candidate | Not passed | Frozen artifacts, resolved findings and seven-day final observation |
| G8 release | Not passed | Explicit publication approval, rollout and maintenance handoff |

Retain the master plan's scoped v1 and post-v1 exclusions. Its provisional RPO ≤24 hours and RTO ≤60 minutes remain targets, not measurements. Recovery timing includes operator response, archive/key acquisition, decryption, restore, verification and security reconciliation; total outage is reported separately.

**Conclusion:** review 6's implementation work remains outstanding. The next useful milestone is closing its three reproduced code failures and correcting D-11, followed by the bounded WP-09 slice and genuine platform/independent acceptance.
