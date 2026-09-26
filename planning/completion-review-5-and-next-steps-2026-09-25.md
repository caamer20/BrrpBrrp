# BrrpBrrp — Fifth Completion Review and Next-Steps Plan

**Date:** 25 September 2026, America/Los_Angeles  
**Candidate:** 0.4.0-alpha.4 · schema 5 · backup format 2  
**Deployment targets:** Windows/Tailscale and Linux Docker  
**Scope:** inspect current completion, verify progress, plan implementation and provide a coder handoff. Application source and configuration remain unchanged by this review.

## 1. Current assessment

**The four review-4 problems received implementation work. The original account-switch defect and ordinary backup-copy reconciliation/scheduling cases now have maintained regressions. The project remains an alpha with privacy and recovery blockers.**

This review reproduced four remaining failures:

1. A private event queued earlier is admitted to a real socket after revocation commits if hub invalidation is delayed.
2. Public voice presence is broadcast to an account whose channel.read capability was removed, even though its state response hides the channel.
3. Failure to persist backup completion status does not stop retention; the old recovery generation named by the durable ledger can be deleted locally and from the copy destination.
4. A copy repair with a missing local source returns success and clears the copy error despite producing no copy.

The browser account-switch journey passes. The full browser run is currently **five passed, one failed**: voice stalls before capture completes. A separate minimal page also leaves getUserMedia pending with the same synthetic-device configuration. This is evidence of a capture-environment problem; it does not prove a new application or RTP-enforcement defect. The review-4 RTP result remains historical evidence, not a fresh pass.

No G0–G8 gate has passed. DMs, groups, named threads, the complete notification/moderation experience and screen sharing remain major v1 work. Further foundation fixes should be bounded by the explicit contracts below; feature work should start from WP-09's shared conversation model once its prerequisites are met.

### Source of truth

- [Review-5 coder handoff](handoff-next-coder-review-5-2026-09-25.md).
- [Master v1 implementation plan](../docs/v1-implementation-plan.md): scope, invariants, all 33 work packages and gate definitions.
- [Review 4](completion-review-4-and-next-steps-2026-09-25.md): historical R4 findings and P4 plan.
- [Progress ledger](../docs/v1-progress.md) and [remediation ledger](../docs/completion-remediation-2026-09-24.md): implementation history, with closure claims qualified by this review.

Use this review for current priorities. Preserve earlier reports as history. Both deployment targets and the recorded MIT/CLA direction are accepted inputs. Developer audit documents remain in planning/ so source links do not enter the native-package document set.

## 2. What changed and what is achieved

Compared with the review-4 manifest, **18 existing project files changed and two application test files were added**:

- [coherent_audience_test.go](../internal/api/coherent_audience_test.go).
- [account-switch.spec.mjs](../web/e2e/account-switch.spec.mjs).

The two review-4 planning documents are additional inventory entries, not implementation. Incidental local tool metadata is excluded from the application delta. There is still no Git checkout in this workspace, so these are content comparisons rather than commit or authorship claims.

| Review-4 item | Current achievement | Closure assessment |
| --- | --- | --- |
| R4-01 coherent audience/revision | coherentAudience wraps channel, grants, account roles, ownership and revision resolution in one transaction. Ordinary channel events, typing and restricted voice use it. Queued events retain their revision and the writer checks it. Tests cover snapshot interleaving, direct grants, implicit default roles, visibility changes and a real revoked socket. | **Original mixed-snapshot mechanism addressed. Overall committed-send boundary remains incomplete:** R5-01. Public voice still bypasses audience resolution: R5-02. |
| R4-02 cross-account completion | RequestIdentity captures account, session and authorization generation. moveChannel no longer defaults to the current session after awaiting. Broader read/incremental completion guards were updated; stop clears move/drag state. | **Direct defect locally verified as addressed.** Four new store tests and a rendered account-switch journey with a permitted reorder control pass. Do not infer universal async/panel coverage. |
| R4-03 published copy with lost status | An existing regular final file with the expected digest is reconciled; conflicting/non-regular targets are refused. Interrupted staging files are handled. Completion attempts to save recovery identity before pruning. | **Matching-copy reconciliation implemented and tested. Durability failure handling remains incomplete:** R5-03. |
| R4-04 copy outage starving snapshots | Snapshot and copy-repair paths have separate clocks and counters. A due snapshot takes priority and the latest local generation becomes the pending copy target. Multi-interval and recovery tests exist. | **Original cadence starvation mechanism addressed.** Missing-source repair still misreports success: R5-04. Real storage faults and host qualification remain open. |
| Related backup health | Health aggregates errors, verifies a recorded fallback and reports its identity, age and upload coverage when the newest copy failed. | Improved, with legacy timestamp/custody limits still requiring explicit handling. |
| P4-05 both-host qualification | No new actual Windows, Docker or off-host evidence found. | **Open.** Static checks and local macOS rehearsals do not qualify either selected host. |
| P4-06 evidence/next product package | No post-review-4 section or refreshed counts in the progress/remediation ledgers; no WP-09 design or implementation added in this delta. | **Open.** Evidence reconciliation is a concrete deliverable of the next iteration. |

Earlier owner-transfer, credential/audit, stream revocation, effective-role, archive validation and migration fixes remain present. They should be preserved. This review found remaining boundary cases, not a reason to restart the application.

## 3. Fresh verification and its limits

| Check | Current result | Scope |
| --- | --- | --- |
| Frontend unit tests | **77 passed** | Up from 73; unchanged baseline tests plus four account/request tests |
| Svelte/TypeScript | **0 errors, 0 warnings** | Current source |
| Formatting and frontend build | **Passed** | Current source and fresh embedded assets |
| Go vet | **Passed** | Current source |
| Full uncached Go race suite | **Passed, exit 0; API package 497.416 seconds** | Web assets built first, then go test -race -count=1 ./...; all test-bearing packages executed uncached |
| Dependency notices | **Passed: 39 components, 20 texts** | Generator freshness |
| Docker input audit/self-test | **Passed: 24 COPY sources** | Static only; Docker executable unavailable |
| Source package checker | **Passed: 315 files, 34 native documents** | Contents and local links; run against original workspace |
| Current executable package smoke | **Passed** | CLI setup, backup, restore, lock, health, production guard, embedded UI/manifest and graceful stop |
| Maintained rollback rehearsal | **53 passed, 0 failed** | Actual archived alpha.3 executable → current candidate → old-compatible rollback |
| Full current browser suite | **5 passed, 1 failed** | Release, authority, privacy, account-switch and ownership pass; voice capture stalls |
| Voice plus setup only | **1 passed, 1 failed** | Same microphone-stage stall, independent of other journeys |
| Minimal browser capture diagnostic | **AudioContext resumed; getUserMedia remained pending during the four-second observation** | Same browser flags on a local page without app code; no fresh RTP result |
| Four additional API behavior probes | **Four required-contract failures reproduced** | R5-01–04; temporary test code only |

### Build and test qualifications

The first disposable-copy race invocation ran before embedded web assets existed; affected packages failed setup. It is not a product-source failure and is not counted as the completed race result. The corrected invocation runs after the frontend build. Likewise, the source checker rejects a symlinked node_modules in the disposable harness; the same checker passes against the actual workspace with its normal dependency directory. Those harness conditions are disclosed rather than presented as product defects.

The fresh rollback uses schema 2/format 1 from alpha.3 and schema 5/format 2 from alpha.4. It verifies account/message/reply/pin/invitation/file/MFA continuity, explicitly checks post-snapshot message and ban loss after old-version rollback, and repairs injected incomplete-upgrade/index state. The old release has no role API; the missing historical-role fixture is disclosed. The injected interruption is not a real process/power failure. No off-host RPO/RTO was measured.

The clean extracted-source rebuild from review 3 remains historical evidence; it was not repeated here. The current executable was freshly rebuilt and smoke-tested.

### Audit identity and artifacts

The starting inventory contains 327 files. Its canonical path/digest manifest SHA-256 is:

    a8bb5e74c5b961c7e6521da88026cadafdc2dc1cd1d1a07a2116fb74e0dd523b

Baseline/build copy:

    /private/tmp/brrpbrrp-review5-ow6nymki

Diagnostic copy:

    /private/tmp/brrpbrrp-review5-probes

The diagnostic copy contains internal/api/review5_audit_test.go. R5-01 adds a temporary socket-writer barrier immediately before the existing admission check; it changes scheduling only. The other probes use real status/copy/publisher paths without application-logic changes. These temporary artifacts may disappear; the reproduction steps below are the durable handoff.

Final content comparison confirmed that all 327 pre-existing inventoried files remain unchanged. This review's workspace additions are only this document and its coder handoff.

## 4. Current findings

### R5-01 — Writer admission still depends on observed invalidation, not committed authority

**Priority: privacy blocker. Related to the unresolved part of R4-01.**

Source: [hub.go](../internal/ws/hub.go), Client.Admitted around lines 85–104; [websocket.go](../internal/api/websocket.go), socket writer; [channel_access.go](../internal/api/channel_access.go), committed mutation followed by stream/voice enforcement and hub invalidation.

The coherent transaction prevents an audience from combining old role data with a new revision. The additional committed-revision check before enqueue also helps. Neither covers an event already queued when a later change commits. Client.Admitted compares the event only with the revision the connection has observed. Enforcement can delay publication of that revision to the hub.

**Reproduction:**

1. Open a member's real socket while it can read a private channel.
2. Publish a private event normally, pausing the socket writer immediately before Client.Admitted.
3. Hold the existing voiceGate and revoke access through the real channel-access API. The database commit succeeds; enforcement waits before publishing invalidation.
4. Verify committed revision 3 and HTTP 404 for the member's history request.
5. Release the writer while invalidation is still delayed.

**Observed:** the actual socket receives the queued private payload. The check itself happened after the committed revocation, so this is not merely already-admitted network bytes completing.

The temporary writer barrier is an audit scheduling tool, not a remotely accessible production feature. Holding the existing voice gate makes the real post-commit/pre-invalidation window deterministic.

**Required outcome:** implement a linearizable admission decision with respect to committed authority. Choose a shared commit/admission mechanism or another rigorously justified protocol. A last-second database read with an unchecked gap is not, by itself, a complete ordering design. Keep the decision short; do not serialize arbitrary socket I/O under a global database lock.

**Acceptance:** deterministic pauses before commit, after commit/before invalidation, after enqueue and before write admission. Assert no post-revocation admission; preserve admitted-write semantics, unaffected recipients and regrant. Include grant, effective-role, visibility, owner, account and session transitions where applicable. The master plan's section 5.4 remains the contract; narrowing it to “the connection has heard about the change” is not closure.

### R5-02 — Public voice presence bypasses channel.read

**Priority: privacy blocker. Existing shortcut newly exercised in this review.**

Source: [voice_access.go](../internal/api/voice_access.go), publishVoiceChannel around lines 54–78.

The public-channel branch calls hub.Broadcast and returns. It assumes every authenticated account can read a public channel. The authorization engine additionally requires channel.read, so that assumption is false after a default/assigned role is narrowed.

**Reproduction:** open a member socket; remove channel.read from its effective default role through the API; verify /state no longer includes the public voice room. Admit a permitted account to that room through the real SFU, triggering normal presence publication.

**Observed:** the denied member's actual socket receives voice.state_updated containing the room ID, participant ID and mute/deafen/speaking flags, even though /state hides the room. This proves metadata disclosure, not that the denied member received audio.

**Required outcome:** route public and private voice presence through the same coherent capability-aware audience and admission rules. Visibility is an input to authorization, not a substitute for it. Review the surrounding public lifecycle fast paths and visibility-transition handling for the same assumption.

**Acceptance:** denied account receives no participant IDs, counts, flags or empty-room updates; a still-authorized account receives updates. Test default and explicit role withdrawal, regrant, room becoming private, participant departure, and delayed queued presence using the R5-01 admission contract.

### R5-03 — Failed status persistence does not stop backup retention

**Priority: recovery blocker. Remaining part of R4-03's crash/durability acceptance.**

Source: [backup_schedule.go](../internal/api/backup_schedule.go), saveBackupStatus around lines 114–122, completeCopy around lines 163–175, deferred status save and retainLocal.

completeCopy calls saveBackupStatus before pruning, but saveBackupStatus logs and returns no error. The caller therefore prunes even when the status was never committed.

**Reproduction:** create verified/copied generation A with retention count one. Inject a database trigger that rejects updates to the backup-status setting. Create and copy generation B through runBackup.

**Observed:** runBackup returns nil; durable status still names A; retention deletes both A's local archive and its independent copy. B exists, but it is not the generation the durable recovery record names.

This demonstrates loss of the recorded fallback and an inconsistent recovery ledger. It does not establish that every recoverable archive was destroyed.

**Required outcome:** propagate persistence failure into job outcome and stop destructive retention unless the needed new recovery state is durably recorded. Protect the previous durable generation through all unsuccessful transitions. Handle the final save and local-only generation path as carefully as copy completion.

**Acceptance:** inject INSERT/UPDATE/commit failures and process stops before/after publication, durable status and pruning. Reopen state using a new store/server, then verify both recorded identity and actual archive bytes. Correct published copies must still reconcile; conflicting files must remain refused. A failed status save cannot return a successful durable-completion claim or delete the recorded fallback.

### R5-04 — Missing copy source clears the error without repairing the copy

**Priority: operational correctness. Remaining part of P4-04's fault matrix.**

Source: [backup_schedule.go](../internal/api/backup_schedule.go), copy-only path around lines 253–261 and deferred error/counter handling.

Any source os.Stat failure returns nil without setting recoveryErr. The deferred completion resets CopyError, Error and CopyAttempts even though the generation still lacks its copy. The branch also treats non-missing errors, such as inaccessible storage, as if the archive were simply gone.

**Reproduction:** create a verified local generation whose copy fails; remove its local archive; make the copy destination available; call repairCopy.

**Observed:** returned error nil; Error and CopyError empty; CopyAttempts zero; CopyName empty; outstandingCopy still true.

**Required outcome:** distinguish missing source, inaccessible source and no pending copy. Preserve/report the failed recovery attempt, retain any valid independent fallback, and define whether a missing source accelerates a replacement local snapshot or requires operator action. Do not let a no-op masquerade as successful repair.

**Acceptance:** missing file, permissions/I/O errors, source replacement/corruption, no-copy configuration and a genuine successful retry. Verify API status, scheduler deadlines, health output and recovery identity together. The deeper archive checker can still report a missing archive today; the reproduced defect is the repair result and saved job state, not a claim that every health surface goes green.

### Verification gap — Fresh negotiated voice evidence is unavailable

The full suite and isolated voice run each stop waiting for the initial “Leave voice” control while the UI says “Waiting for microphone.” This occurs before the RTP assertions. The separate minimal local page starts/resumes AudioContext but does not complete getUserMedia during observation.

Treat this as a current capture-environment blocker requiring diagnosis, not as proof that the previous server-side RTP enforcement regressed. Preserve the non-cooperative outbound-RTP control and permitted regrant assertion. Do not make the suite pass by skipping voice, weakening assertions or replacing real RTP counters with peer-state checks.

### Evidence debt

The progress/remediation ledgers still describe the P3 tranche and 73 tests/five green journeys. Some passages already contradict later paragraphs. Update them once with current package status and explicit historical/current labels. In particular:

- Do not claim that an old audience can never deliver after committed revocation.
- Record the 77-test and rendered account-switch progress.
- Record current voice capture failure separately from review-4 RTP success.
- Replace “remaining recovery work only needs other machines” with the local R5-03/04 work.
- Preserve the correct gate names and never equate local verification with independent acceptance.

## 5. Next implementation plan

### P5-01 — Finish committed delivery admission and remove voice bypass

**Responsibility:** backend + QA; technical lead reviews ordering; independent security reviewer assesses the resulting contract.  
**Closes:** R5-01 and R5-02. **Depends on:** current coherent-snapshot implementation.

1. Map authorization commits to enqueue/admission/invalidation for every channel event, typing and presence path.
2. Write a short ADR describing the linearization point, already-admitted bytes and lock ordering.
3. Implement admission synchronization without holding a global lock across network I/O.
4. Apply capability-aware resolution to public and private voice presence; inspect equivalent shortcuts.
5. Add real-socket negative cases and permitted controls with explicit barriers.
6. Run targeted API/hub/voice tests and the full uncached race suite.

**Exit:** both socket disclosures fail against the old behavior and pass against the fix; no deadlock, deny-all workaround or lost permitted recovery. Update the surface inventory with the exact tested transition.

### P5-02 — Make backup completion durable before retention

**Responsibility:** backend + operations + QA. **Closes:** R5-03.

1. Return/check errors from status persistence and separate publication from durable acknowledgement.
2. Base retention protection on successfully persisted recovery identity.
3. Handle deferred/final saves and local-only backups explicitly.
4. Add database failure injection and restart tests across the state transitions.
5. Re-run existing matching-copy reconciliation, retention, archive and rollback tests.

**Exit:** failed persistence preserves the previously recorded archives, returns/reports failure and can recover on restart. A valid newly published copy is safely adopted when status storage recovers.

### P5-03 — Complete copy-repair failure semantics and legacy health handling

**Responsibility:** backend + operations + QA. **Depends on:** P5-02's status contract. **Closes:** R5-04.

1. Distinguish a missing source from stat/read failures and a true no-op.
2. Preserve separate snapshot/copy errors and clocks; define replacement-snapshot behavior.
3. Validate newest local and independently recoverable fallback identity/coverage after restart.
4. Qualify old status records lacking the new fields. In particular, fallbackProblem currently substitutes copy time when snapshot time is absent; report an unknown/conservative age or recover the archive's actual timestamp instead of labeling copy time as proven snapshot freshness.
5. Test prolonged outages, retry exhaustion, recovery, disk reserve and bounded pending-generation retention with a controlled clock.

**Exit:** each failure is visible without erasing useful fallback information; successful repair converges; fresh snapshots continue and deadlines are not incorrectly reset.

### P5-04 — Restore reproducible browser/media verification and finish UI evidence

**Responsibility:** frontend/media + QA. **Can proceed independently of backup work.**

1. Diagnose synthetic microphone availability using the minimal capture probe, browser logs and declared host/browser versions.
2. Establish a reliable capture environment, then run the unmodified six-journey suite.
3. Preserve new account-switch negative/allowed controls.
4. Complete still-missing rendered category-only/role-only and failed-refresh cases with search, pins, threads and previews open.
5. Verify stale errors/finally callbacks and account/session cleanup beyond the directly fixed channel move.

**Exit:** six fresh journeys pass on the corrected candidate and required rendered scenarios have evidence. If capture remains unavailable, finish unblocked UI work and record the specific media environment dependency; do not call the suite green.

### P5-05 — Consolidate evidence, qualify hosts and seek foundation acceptance

**Responsibility:** technical lead + operations + release + QA; independent security reviewer and project owner for their decisions.

1. Update progress/remediation ledgers with P4 achievements and R5 closure evidence.
2. Establish canonical repository/history, maintainer access/recovery, hosted CI and artifact identities. This workspace currently has no Git checkout.
3. Execute actual Windows/Tailscale and Linux Docker install/restart/upgrade/rollback/uninstall-preservation checks using exact candidate artifacts.
4. Restore an independent encrypted backup to clean destinations using documented key custody; verify files, permissions, search and security reconciliation before cutover.
5. Deliver a real external alert for injected service/backup/copy failure; record receipt.
6. Obtain independent foundation security/privacy review, resolve findings and record G0/G1 decisions.

**Exit:** one consistent evidence ledger with named owners and actual host results. Missing hosts, credentials, reviewers or signing infrastructure remain explicit dependencies. Complete local work while these are arranged.

### P5-06 — Begin the next product milestone from WP-09

**Responsibility:** technical lead + backend/frontend + QA; product owner for unresolved semantics.

Design preparation can proceed alongside foundation remediation. Implement under the master plan's prerequisites and feature gates:

1. Shared conversation identity/membership and migration compatibility.
2. Idempotent send receipts surviving lost acknowledgements, deletion and retry.
3. Account-scoped bounded drafts/outbox and clear delivery states.
4. Monotonic read cursors and reconnect/event reconciliation.
5. Only then build canonical 1:1 DMs, consented groups and blocking on those primitives.

**Exit:** a reviewable ADR/migration plan and a bounded vertical slice with old-data migration, duplicate/lost-ACK, account-switch and authorization tests. Do not graft private messages onto channel assumptions or advertise private-call support before its own media admission contract exists.

### Dependency and stopping rules

P5-01 and P5-02 affect different areas. P5-03 builds on the backup status contract. P5-04 and host provisioning can proceed while fixes are developed. Final host and foundation acceptance must use the corrected candidate; independent review is a separate step. This plan does not require multiple coding agents.

The next coder should implement the local packages and run their meaningful gates before moving to wider product changes. Avoid another open-ended sequence of broad rewrites. Each package has a concrete contract and exit criteria; remaining external dependencies should have an owner and next action rather than a fabricated completion date.

## 6. Completion map toward v1.0.0

| Packages / gate | Current state | Next evidence |
| --- | --- | --- |
| WP-01–03 / G0 baseline | Rights/contribution documents and ADRs exist; canonical history, some decisions and reference environments remain open. | Recorded decisions, owners, real hosts and working hosted CI |
| WP-04–05 migrations/identity | Substantial local implementation and fresh rollback evidence. | Both-host lifecycle and recovery; preserve identity/session regressions |
| WP-06–08 / G1 privacy | Coherent snapshots and account isolation improved; R5-01/02 still block the delivery contract. | Fixed socket cases, full surface/transition evidence, independent review |
| WP-09–12 / G2 private messaging | Shared conversation/outbox/receipt, DMs, groups, blocking and lifecycle/export remain ahead. | WP-09 first; then consent, membership and lifecycle journeys |
| WP-13–17 / G3 community | Existing chat/replies/PWA provide foundations; named threads, inbox/preferences, reports/timeouts/slow mode and full UX remain. | Complete feature journeys and accessible interaction |
| WP-18–22 / G4 media | Existing voice; historical local non-cooperative RTP evidence; current capture verification blocked. Publications/screen sharing/private calls remain. | Architecture spike, signaling/publication work, physical/cross-browser/network qualification |
| WP-23–27 / G5 operations | Recovery/package tooling exists; R5-03/04 remain; selected hosts, capacity and provenance unqualified. | Durable status, fault matrices, independent restore, monitoring/load and artifact evidence |
| WP-28–31 / G6 beta | Documents exist; independent reviews, physical accessibility and beta acceptance incomplete. | Security/accessibility review, newcomer drills and 14-day beta |
| WP-32 / G7 candidate | Not started. | Frozen artifacts, resolved findings, seven-day observation and go/no-go |
| WP-33 / G8 release | Not started. | Explicit publication authorization, staged rollout and maintenance ownership |

**No gate is passed.** A feature-count percentage would be misleading because the remaining work includes privacy guarantees, production operations and major product capabilities. The goal is a credible self-hosted community platform within the master plan's v1 scope; preserve its stated post-v1 exclusions.

### Both-host qualification requirements

For each selected deployment, retain the exact OS/runtime/build and artifact digest, configuration, dataset, command, observed result and limitation:

- Installation, service/container identity and data permissions.
- TLS/Tailscale or reverse-proxy/WebSocket routing; direct and relay media routes.
- Reboot/restart, interruption, update, rollback and intended data preservation.
- Independent encrypted backup acquisition, key recovery and clean restore.
- Disk pressure, missing/inaccessible copy storage and external alert receipt.
- Frozen load/soak dataset, physical clients and supported browser/accessibility matrix.
- Clean consumer verification of source/package/image and available signatures/provenance.

Retain the master plan's provisional RPO ≤24 hours and RTO ≤60 minutes, with its proposed 12-hour backup/copy cadence and age alerts. These remain targets, not measured results. RTO begins at the declared recovery event and includes operator response, acquisition/decryption, restore, verification and security reconciliation; report total outage separately. A second directory on this Mac is not independent recovery.

## 7. Deliverable and evidence checklist

For every fix, record source/artifact identity, environment, exact command, outcome, negative assertion, permitted control and evidence limits. Distinguish implemented, locally verified, platform qualified, independently reviewed and release accepted.

- [ ] R5-01 committed write-admission boundary fixed and exercised on real sockets.
- [ ] R5-02 public voice capability bypass removed with allowed-recipient controls.
- [ ] R5-03 status failures prevent destructive retention and survive real reopen.
- [ ] R5-04 missing/inaccessible source remains a failed repair with correct scheduling.
- [ ] Existing account-switch, archive, rollback and other foundation regressions preserved.
- [ ] Current browser/media outcome reproducible and accurately labeled.
- [ ] Progress/remediation ledgers updated, with historical claims kept distinct.
- [ ] Both-host and independent-review work completed where accessible; remaining owners/actions recorded.
- [ ] G0/G1 decision evidence assessed without self-certification.
- [ ] WP-09 has a concrete design and executable next slice.

This review produced planning documents and disposable diagnostic evidence. It did not fix or deploy the app.
