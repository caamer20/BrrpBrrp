# BrrpBrrp — Sixth Completion Review and Next-Steps Plan

**Date:** 25 September 2026, America/Los_Angeles  
**Candidate:** 0.4.0-alpha.4 · schema 5 · backup format 2  
**Required hosts:** Windows/Tailscale and Linux Docker  
**Scope:** completion audit, verification, implementation plan and coder handoff. No application-source, configuration or deployment changes.

## 1. Assessment

**The review-5 fixes are implemented for their directly tested scenarios, and all six browser journeys now pass. The application remains an alpha with three reproduced implementation failures and a WP-09 design conflict to resolve before the next product milestone.**

The new authorization commit/admission lock closes the prior channel-revocation timing case. Public voice presence now uses capability-aware audiences. Backup status errors are propagated, the earlier independently recoverable generation is protected on the tested failure path, and missing copy sources keep an error and accelerate replacement snapshots. The ledgers were updated and a shared-conversation design note was added.

Remaining priorities:

1. Remove a database/authorization lock-order cycle.
2. Extend commit/admission ordering to credential and session revocations.
3. Protect the durable local backup record when copying and status persistence both fail.
4. Reconcile WP-09's new note with the established expiry, cross-conversation idempotency, draft-preservation and migration contracts.

These findings are narrower than restarting the foundation. Preserve the successful changes, fix the remaining shared contracts, obtain the required external acceptance, then advance the shared conversation model. No G0–G8 gate has passed; DMs, groups, named threads, the complete notification/moderation experience and screen sharing remain substantial work.

### Reading order

- [Review-6 coder handoff](handoff-next-coder-review-6-2026-09-25.md).
- [Master v1 plan](../docs/v1-implementation-plan.md): requirements, invariants, scope and all 33 packages.
- [Review 5](completion-review-5-and-next-steps-2026-09-25.md): historical findings and P5 plan.
- [Progress ledger](../docs/v1-progress.md) and [remediation ledger](../docs/completion-remediation-2026-09-24.md): current implementation history, with broad closure claims qualified below.

Use this review for immediate priorities. Preserve previous reports as history. Both deployment targets and the recorded MIT/CLA direction remain accepted inputs. Audit documents stay in planning/ because their source links do not belong in native distribution documents.

## 2. Progress since review 5

Content comparison found **17 changed project files: 14 Go source/test files and three documents**. There are no new application files in this delta. The two review-5 documents were added to the audit inventory; incidental local tool metadata is excluded from the application delta.

| Previous item | Achievement | Current assessment |
| --- | --- | --- |
| R5-01 channel commit versus delayed invalidation | Server.authority orders socket admission against commitAuthorityChange, which commits and invalidates before slow stream/voice enforcement. The permanent regression pauses a real socket writer and delays voice enforcement. Test hooks now use atomic pointers. | **Direct channel case addressed. Shared protocol incomplete:** lock ordering and unfenced credential/session paths, R6-01/02. |
| R5-02 public voice presence | Public and private room presence now resolve through the same coherent capability-aware audience; the blanket public broadcast was removed. | **Direct disclosure addressed**, with denied/allowed recipient tests. Preserve these tests while changing admission. |
| R5-03 failed status write and retention | saveBackupStatus returns errors. Successful-copy and no-copy-configured paths attempt persistence before retention; the earlier independently recoverable identity is also protected. | **Original copied-generation case addressed. Combined copy/status failure still prunes a durable local generation:** R6-03. |
| R5-04 missing copy source | Missing, non-regular and inaccessible sources are separated. Failed repair preserves CopyError/counters; SourceMissing requests a replacement snapshot on the short clock. | **Direct missing/non-regular cases addressed.** Wider storage/restart and legacy-health qualification remains open. |
| Voice verification blocker | The unchanged synthetic capture harness now completes, including outbound-RTP growth while denied inbound RTP stops and regrant restores delivery. | **Fresh local evidence restored: six browser journeys pass.** No physical-audio, forced-relay or cross-browser acceptance implied. |
| Evidence consolidation | A post-review-4/5 section was added to the remediation ledger; progress and test counts were refreshed. | Improved; “every authorization-changing transaction” and “retention only after durable recording” still overstate actual coverage. |
| WP-09 preparation | A six-part prerequisite design note now covers conversations, migration, receipts, outbox, read cursors and a first slice. | **Design started, no implementation.** Internal contradictions and missing transition details must be corrected under R6-04. |
| Actual production qualification | No new Windows, Docker, independent encrypted restore, external alert receipt, hosted provenance, independent review or beta evidence found. | Still open. |

Relevant sources: [authorization helpers](../internal/api/authz.go), [writer](../internal/api/websocket.go), [voice presence](../internal/api/voice_access.go), [backup state](../internal/api/backup_schedule.go), [coherent audience tests](../internal/api/coherent_audience_test.go), [voice privacy tests](../internal/api/voice_privacy_test.go), and [backup tests](../internal/api/backup_schedule_test.go).

## 3. Fresh verification

| Check | Result | Scope |
| --- | --- | --- |
| Frontend unit suite | **77 passed** | Existing source and tests; no frontend delta |
| Svelte/TypeScript | **0 errors, 0 warnings** | Current source |
| Formatting and frontend production build | **Passed** | Build b42b3fcb453f5078 |
| Current executable build and Go vet | **Passed** | Current Go source and fresh embedded assets |
| Full uncached Go race suite | **Passed, exit 0; API package 531.358 seconds** | Root-level go test -race -count=1 ./...; all test-bearing packages executed uncached |
| Six checked-in browser journeys | **6 passed, 53.7 seconds** | Release, authority, privacy, voice, account-switch and ownership; no test patch |
| Dependency notices | **Passed: 39 components, 20 texts** | Freshness check |
| Docker input audit and self-test | **Passed: 24 COPY sources** | Static inputs only |
| Source package checker | **Passed: 315 files, 34 native documents** | Contents and links; not an extracted build |
| Maintained cross-version rollback | **53 passed, 0 failed** | Real archived alpha.3 → candidate alpha.4 → old-compatible restore |
| Three additional API contract probes | **Three required-behavior failures reproduced** | R6-01–03, described below |
| WP-09 contract review | **Conflicting specifications found** | R6-04; document/source comparison, not an implemented product failure |

The race suite is run from the root after building web assets; an earlier web-directory invocation tested only the web package and is not counted as the complete suite.

The rollback rehearsal uses old schema 2/format 1 and current schema 5/format 2. It verifies users, messages, reply, pin, invitation, attachment bytes and MFA, explicitly identifies post-snapshot message/ban loss, and repairs injected incomplete-upgrade/index state. The old release has no role API. This remains a small local macOS fixture and injected interruption, not a process/power-loss test, actual Windows/Docker lifecycle or measured off-host recovery.

Review-5 standalone package smoke and review-3 clean extracted-source rebuild remain historical evidence; those separate exercises were not repeated here. The current executable was freshly built and used for browser and rollback checks.

### Snapshot and reproduction

There is no Git checkout, so this review cannot identify an accepted commit, authorship or hosted CI run. The starting inventory contains **329 files**. Canonical path/digest manifest SHA-256:

    867396bd87d1b43444bd9660e0a5d56bec7edc216aed56e4ca5b25747c2d0b00

Baseline/build copy:

    /private/tmp/brrpbrrp-review6-o7bwfcg6

Diagnostic copy:

    /private/tmp/brrpbrrp-review6-probes-73ai8fwv

The diagnostic tests are in internal/api/review6_audit_test.go inside that copy:

    go test ./internal/api -run '^TestReview6' -count=1 -v

They use existing test hooks, helpers and locks; no diagnostic changes to application logic were needed. The temporary files may disappear, so the scenarios below are the durable reproduction record. Builds and diagnostic mutations remained outside the workspace. Final content comparison confirmed that all 329 pre-existing inventoried files are unchanged; only this review and its handoff were added to the workspace.

## 4. Remaining findings

### R6-01 — Authorization and database locks are acquired in opposite orders

**Priority: availability and foundation blocker. Evidence: deterministic lock cycle.**

Source: [authz.go](../internal/api/authz.go), commitAuthorityChange around lines 401–413, publishAuthorizationChange around lines 503–506 and invalidateWorkspace around lines 623–626.

commitAuthorityChange is called with a live transaction holding the store's only database connection, then acquires authority.Lock. The standalone invalidation helpers acquire authority.Lock first, then query the database for its revision. Concurrent operations can therefore wait on each other:

| Operation | Holds | Waits for |
| --- | --- | --- |
| Authorization transaction committing | Database connection | Authorization lock |
| Workspace/standalone authorization invalidation | Authorization lock | Database connection |

**Reproduction:** begin a transaction and bump its revision; start invalidateWorkspace and wait until it holds the authorization lock; invoke commitAuthorityChange on the transaction. Neither completes. Canceling the transaction releases the database connection and allows cleanup; the commit returns context canceled.

The probe invokes real helpers with controlled scheduling. It demonstrates a lock-order cycle, not merely a slow query. Request cancellation can break the cycle; ordinary successful completion must not depend on cancellation. Other socket writers needing the shared lock can stall behind it.

**Required change:** define one lock/connection acquisition order for commits, standalone invalidation, connection admission and delivery. Eliminate database waits under an oppositely ordered lock. Preserve the linearization guarantee; moving a revision read casually outside the lock can reintroduce stale admission. Keep network writes outside the critical section.

**Acceptance:** deterministic concurrent authorization mutation and structural/credential invalidation complete without cancellation. Include rollback/error paths and representative real API operations. Test both the pre-commit admitted-write control and the post-commit refusal. A timeout is a failure bound, not proof of correct ordering.

### R6-02 — Credential/session commits still bypass the admission protocol

**Priority: privacy/session-revocation blocker. Evidence: committed password reset followed by actual socket delivery.**

Source: [admin.go](../internal/api/admin.go), resetPassword around lines 488–518; [auth.go](../internal/api/auth.go) and [mfa.go](../internal/api/mfa.go) contain other session mutations needing the same inventory. The new helper in [authz.go](../internal/api/authz.go) protects selected revision-changing commits; publishing a later invalidation under its lock does not retroactively protect an earlier credential commit.

**Reproduction:**

1. Enroll/designate an operator and open the target member's real socket.
2. Queue a private event and use the existing writerAdmission hook to pause before admission.
3. Hold the delivery registry lock and reset the target's password through the real API.
4. Wait until the transaction has deleted the target's sessions; confirm the old cookie gets HTTP 401.
5. Release the writer while the reset handler still waits in post-commit stream cleanup.

**Observed:** the old socket receives the private payload. The password reset then completes normally when cleanup is released.

This is a newly admitted send after session revocation committed, not bytes already admitted before the change. The existing channel-revocation fix does not cover it because resetPassword uses tx.Commit directly and invalidates/disconnects later.

**Required change:** inventory every authority/session transition and bring its commit plus delivery eligibility update under the same correctly ordered protocol. Include password change/reset, MFA enrollment/removal/recovery, logout, revoke-one/all, ban/deactivation, ownership and offline recovery as applicable. Support session-specific invalidation without disrupting permitted sibling sessions. Cover new socket registration racing a revocation as well as existing writers.

**Acceptance:** a committed reset or session removal prevents new delivery on affected sockets even if stream/voice cleanup is delayed. Unaffected users and permitted sessions retain access; fresh sign-in/regrant works. The route inventory must name its commit helper and invalidation audience. Do not close this item with only the password-reset regression.

### R6-03 — Combined copy and status failure still prunes the durable local generation

**Priority: recovery consistency blocker. Evidence: actual archive removed while durable status still names it.**

Source: [backup_schedule.go](../internal/api/backup_schedule.go), fresh-generation flow around lines 365–392, retainLocal and deferred status persistence.

The updated code prevents pruning when successful copy publication is followed by failed status persistence. However, if the copy itself fails normally, the fresh local generation is not persisted before retainLocal runs. Protection names include independently recoverable generations, but not necessarily the previous verified local generation's Name. Final status saving happens afterward.

**Reproduction:** configure retention one and an unavailable copy destination. Create verified local generation A with no successful independent copy. Inject a database trigger rejecting backup-status updates. Run another backup B while the copy destination still fails.

**Observed:** the command correctly returns a status-persistence error, and durable status still names A, but A's archive has already been deleted by retention. B exists but its identity was not committed to the ledger.

The previous false-success defect is improved; the remaining defect is destructive retention before the required durable state transition. This does not mean every recovery archive was lost.

**Required change:** express retention eligibility as an explicit successful persistence condition rather than absence of one particular error sentinel. Persist the verified local generation even when its copy fails, and preserve all identities still needed by the durable ledger until the transition succeeds. Cover installations with no previously successful independent copy and installations with both a newer local point and an older independent fallback.

**Acceptance:** combine copy unavailable/corrupt/partial with status INSERT/UPDATE failure, final-save failure and restart. The ledger's local and independent identities must remain backed by verified files. A new generation may be adopted after recovery; failed status storage cannot erase the only archive the record identifies. Reopen through a new store/server and verify archive contents, not only in-memory fields.

### R6-04 — WP-09's new design note conflicts with existing invariants

**Priority: prerequisite design blocker. Evidence: contradictory documents and current schema. No new DM/outbox code exists yet.**

Source: [v1-implementation-plan.md](../docs/v1-implementation-plan.md), section 5.5, section 5.8 and the new note around lines 446–460; [schema.sql](../internal/db/schema.sql), messages definition.

| Topic | Conflict or missing detail | Required resolution |
| --- | --- | --- |
| Expired operations | Section 5.5 requires server-enforced expiry and an explicit new send with a new key. The new note says an expired key may be reused, allowing a delayed original request to be mistaken for new work unless a separate admission identity is defined. | Specify enforceable operation expiry and distinguish explicit new intent. Retried expired operations cannot silently recreate a deleted message. |
| Idempotency scope | Current uniqueness is (user_id, idempotency_key), not per-channel. Section 5.5 requires a changed destination under a reused key to conflict. The new conversation-scoped tuple and listed digest fields do not preserve that rule by themselves. | Choose an immutable operation identity and include destination in intent validation. State exact uniqueness constraints and cross-conversation outcomes. |
| Draft/outbox recovery | Section 5.8 requires opt-in persistence and recoverable unsent text when access or a parent disappears. The note says revocation clears the outbox and does not restate the opt-in boundary. | Separate automatic retry cancellation, server-content removal and recovery of user-authored unsent text. Preserve account/destination isolation and explicit device-persistence choice. |
| Migration mechanics | The note makes messages.channel_id nullable and introduces conversation_id, but calls the migration purely additive with no message rows rewritten. The existing column is NOT NULL with an ON DELETE CASCADE foreign key; the compatibility/backfill strategy is unspecified. | Define each schema stage, any table rebuild/backfill, dual reads/writes, constraints, indices/triggers/FTS, verification and rollback boundary. Preserving IDs is different from promising no row rewrite. |
| Identity lifecycle prerequisite | WP-09 already requires the account-deletion/tombstone schema/policy before private conversations. The new first slice does not specify it. | Include the prerequisite explicitly and align migration naming with the existing sequence; WP-12's later tools cannot supply a missing privacy prerequisite retroactively. |

**Required outcome:** one D-11 ADR with exact invariants, transitions, schema and API outcomes. Resolve the contradictions against the established requirements before building receipts/outbox or enabling DMs. Do not interpret a short design note as a completed implementation-ready specification.

## 5. Next implementation packages

Responsibilities below identify work roles, not newly assigned people.

### P6-01 — Complete the authorization/session admission protocol

**Owners:** backend + QA; technical lead reviews design; independent security reviewer accepts evidence.  
**Closes:** R6-01 and R6-02. **First code priority.**

1. Enumerate database connection, authorization, hub, delivery and voice lock ordering.
2. Write the commit/admission ADR, including already-admitted writes, errors and cancellation.
3. Remove the lock-order cycle while preserving coherent audience snapshots.
4. Apply the protocol to all relevant authorization and session commits and connection registration.
5. Add deterministic lock-cycle and real-socket credential regressions, then complete the transition inventory with allowed controls.
6. Run affected tests and the complete uncached race suite.

**Exit:** no cycle requires cancellation to resolve; committed revocations prevent affected new admission; unrelated recipients/sessions continue normally; no global network-write lock. Correct the ledger's “every transaction” claim to match the audited inventory.

### P6-02 — Complete backup persistence and retention across combined faults

**Owners:** backend + operations + QA. **Closes:** R6-03.

1. Write a transition table separating verified local generation, verified independent fallback, pending copy, durable status and retention eligibility.
2. Persist local state independently of copy success; propagate errors and protect the previous durable local/fallback files.
3. Replace sentinel-only retention decisions with explicit persisted-state outcomes.
4. Test paired faults and real store/server reopen, including no previous independent copy.
5. Preserve matching-copy reconciliation, separate clocks, missing-source replacement and non-regular target refusal.

**Exit:** every durable identity resolves to a verified surviving archive after each failed transition. Successful recovery converges with bounded storage. Existing archive, cadence and maintained rollback checks pass.

### P6-03 — Make WP-09 implementation-ready

**Owners:** technical lead + backend/frontend + QA; owner resolves genuinely open product semantics.  
**Closes:** R6-04. **Can proceed alongside foundation fixes.**

Produce a D-11 ADR containing:

- Exact conversation mapping and membership boundary.
- Actual current constraints and staged migration SQL/design, including lifecycle/tombstone prerequisites.
- Operation uniqueness, intent fields, deletion/expiry outcomes, retry eligibility and receipt pruning.
- Opt-in bounded outbox storage, account clearing, destination revalidation and recoverable-text UX.
- Read-cursor visibility rules, resource versions and reconnect reconciliation.
- Compatibility/rollback rules and a scenario-to-test map.

Use existing approved requirements to resolve drafting errors; do not reopen the selected deployment targets or license. Ask for owner input only where the source requirements leave a consequential decision genuinely unresolved.

**Exit:** no conflict between the ADR, master invariants and proposed schema; the next slice has exact API outcomes and migration acceptance.

### P6-04 — Finish UI evidence and reconcile completion records

**Owners:** frontend + QA + technical lead.

1. Preserve the six fresh passing browser journeys and the non-cooperative RTP control.
2. Finish outstanding rendered category-only/role-only management and failed-refresh cases with search, pins, replies/threads and previews open.
3. Verify stale success/error/finally behavior and account/session cleanup on the remaining relevant paths.
4. Update remediation/progress entries with R6 evidence and distinguish direct fixes, complete contracts, platform qualification and independent acceptance.

**Exit:** current evidence is internally consistent and remaining UI obligations have explicit results. A green six-journey suite alone is not proof of every privacy surface.

### P6-05 — Qualify the selected hosts and obtain foundation acceptance

**Owners:** operations + release + QA; independent security reviewer; project owner for remaining decisions.

1. Establish canonical source history, maintainer recovery, reference hosts and hosted CI/artifact identities.
2. Run real Windows/Tailscale and Linux Docker installation, restart/reboot, upgrade, interruption, rollback and data-preserving lifecycle checks.
3. Restore independent encrypted backups into clean destinations using documented keys and operator materials.
4. Verify external alert receipt, storage-failure behavior and agreed reference-load objectives.
5. Obtain independent authorization/session/recovery review against the corrected implementation.
6. Record G0/G1 decisions from their complete evidence rather than marking individual test fixes as gate acceptance.

**Exit:** actual host and reviewer records tied to exact candidate bytes. Missing access, reviewers, keys or signing identities remain explicit dependencies with owner and next action. Continue all unblocked local work.

### P6-06 — Implement a bounded WP-09 vertical slice

**Owners:** backend/frontend + QA. **Depends on:** P6-03; exposed functionality follows the master privacy gate.

Implement in reviewable steps:

1. Conversation mapping and required lifecycle schema on legacy data.
2. Compatibility reads/writes and invariant verification.
3. Immutable, deletion-safe send receipts with explicit expiry and conflict behavior.
4. Opt-in account-scoped outbox and safe text recovery.
5. Read-cursor/reconnect integration.

Exercise legacy upgrade, lost acknowledgement → deletion → retry, changed destination/intent, expired retry, account switch, revoked destination, staged attachments and reconnect. Only then build canonical DMs, consented groups and blocking on the shared primitives.

**Exit:** a working channel-based slice proves the shared model while retaining old chat behavior. No premature DM/group/private-call claim.

### Dependency and estimation rules

P6-01 and P6-02 can be implemented independently. P6-03 and host/reviewer preparation can progress while they are fixed. Final host/privacy acceptance must use the corrected candidate. P6-06 follows the reconciled design and applicable gates.

Do not promise a calendar v1 date from test counts. Estimate the bounded packages after their designs and required access are known. The master plan's broader effort ranges remain provisional. This plan does not require multiple agents, a rewrite or a new technology stack.

## 6. Remaining v1 completion map

| Work / gate | Current state | Completion still required |
| --- | --- | --- |
| WP-01–03 / G0 | Rights/contribution documents and ADRs exist; source traceability, some decisions and reference environments remain open. | Canonical history, owners, actual hosts, working CI and ratified baseline |
| WP-04–05 | Strong local migration/identity work; fresh maintained rollback. | Both-host lifecycle and complete credential/session ordering |
| WP-06–08 / G1 | Coherent audiences, capability UI and scoped voice implemented; R6-01/02 remain. | Complete delivery/session contract, UI evidence and independent review |
| WP-09–12 / G2 | New design note; shared conversation/outbox/receipts, DMs, groups, blocking and lifecycle implementation still ahead. | Correct ADR, migration and private-conversation acceptance |
| WP-13–17 / G3 | Existing chat, replies and PWA are foundations. | Named threads, inbox/preferences, reports/timeouts/slow mode and complete accessible UX |
| WP-18–22 / G4 | Voice works locally with fresh RTP revocation evidence. | Media spike, private-call contract, publication/signaling, screen sharing and real network/device qualification |
| WP-23–27 / G5 | Extensive local recovery/packaging tools; R6-03 remains. | Combined-fault correctness, real hosts/independent restore, monitoring, capacity and provenance |
| WP-28–31 / G6 | Documents exist; independent acceptance and beta incomplete. | Security/accessibility review, newcomer drills and 14-day beta |
| WP-32 / G7 | Not started. | Frozen artifacts, resolved findings, seven-day final observation and go/no-go |
| WP-33 / G8 | Not started. | Explicit publication approval, staged rollout and maintenance ownership |

**No gate has passed.** The next feature milestone is the shared conversation foundation, not a collection of independent Discord-like features. Preserve the master plan's deliberate post-v1 exclusions.

### Both-host evidence matrix

| Area | Windows/Tailscale | Linux Docker |
| --- | --- | --- |
| Install/security | Selected OS build, service identity/ACLs, setup, TLS/Tailscale exposure | Selected distribution/runtime, image digest, UID/volume permissions, reverse proxy |
| Lifecycle | Reboot, service restart, interrupted update, rollback and intended data preservation | Restart/recreate, readiness, failed image update, rollback and volume preservation |
| Recovery | Independent encrypted archive to clean host with documented keys | Independent archive to fresh volume/container with documented keys |
| Faults/alerts | Disk pressure, permissions, copy outage and external alert receipt | Missing/read-only volumes, container interruption, copy faults and external alert receipt |
| Media/capacity | Physical clients, forced relay, declared browsers and reference workload | Same supported workload/network cases with recorded host resources |
| Release consumer | Exact candidate download, signature/checksum, install/upgrade | Exact image/source digest/provenance, install/upgrade |

Retain the master plan's provisional RPO ≤24 hours and RTO ≤60 minutes, proposed 12-hour backup/copy cadence and age alerts. These are targets, not measured results. RTO includes operator response, acquisition/decryption, restore, verification and post-snapshot security reconciliation from the declared recovery event; report total outage separately.

Docker is unavailable in this environment. No actual selected-host, off-host encrypted restore, physical accessibility/audio, forced relay, hosted signing/provenance, independent review, target-scale soak or beta acceptance was completed in this audit.

## 7. Next-iteration completion checklist

- [ ] R6-01 lock-order cycle eliminated with deterministic concurrency evidence.
- [ ] R6-02 credential/session commits and socket registration covered by the admission protocol.
- [ ] R6-03 combined copy/status failures preserve the durable local and independent recovery points.
- [ ] R6-04 WP-09 contradictions resolved in an implementation-ready ADR.
- [ ] Existing owner, role, client, voice, archive and rollback fixes preserved.
- [ ] Applicable unit/race/browser/package checks pass on the corrected candidate.
- [ ] Remaining rendered privacy/management scenarios have explicit evidence.
- [ ] Ledgers state current versus historical results and actual gate status accurately.
- [ ] Both-host/reviewer dependencies have actual results or named owners/actions.
- [ ] WP-09's next vertical slice is concrete and respects its prerequisites.

For each item retain source/artifact identity, environment, exact command, result, negative assertion, permitted control and limitations. Use separate labels for implemented, locally verified, platform qualified, independently reviewed and release accepted.

This review produced documents and disposable diagnostic evidence. It did not modify or deploy the application.
