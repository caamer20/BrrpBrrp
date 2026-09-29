# Standalone implementation handoff: finish BrrpBrrp v1.0.0

Prepared 28 September 2026 after completion review 12; updated after rechecking the repeated P10 recap and unchanged source. Copy **everything below the separator** into the next coding agent. The prompt is self-contained; the repository documents supply deeper contracts and historical evidence.

---

You are taking over **BrrpBrrp**, an open-source, self-hosted community communication application. The owner wants a polished, approachable Discord replacement that ordinary people can install, operate and enjoy. Continue the existing implementation through a coherent, feature-complete release candidate and then to **v1.0.0** when its real acceptance gates pass.

This is an implementation assignment. Read and reconcile the current state, make the changes, test them, build the deliverables and continue through the remaining milestones. Do not stop after writing another plan, repairing only the reporting dialog, or making the current tests green. Work in reviewable milestones with honest evidence and durable checkpoints.

**Latest verification:** the owner's most recent attached report repeats the previous P10 recap. Source remains `6b7b30157f3bd9246c1dbba442c8f341f4c208af`; there have been no tracked implementation changes since review 12. Only its two planning documents are untracked. P10 remains the latest evidence, and its Darwin/source archive hashes still match. R12-01–05 were checked again in source and remain unfixed. No newer host, independent-review, beta or release qualification was found. Treat the report's “all eight items closed” as the previous coder's claim; review 12 identifies the remaining failures.

Your first implementation result should be permanent regressions and fixes for M0's verified defects. Reuse the current plans and evidence. Proceed into durable activity and the other product milestones after those fixes; if external resources block qualification, continue independent code work and report the precise blocked gate. Do not spend the next run producing another completion recap without implementing the outstanding work. If this checkout has advanced since the state above, verify the actual delta and credit fixes supported by evidence.

## 1. Mission, authority and finish lines

### Product promise

- One community per self-hosted installation, local accounts and no required vendor account.
- Easy owner setup, invitations, account recovery, permissions and member onboarding.
- Reliable community chat, consented one-to-one and group messaging, useful threads, activity and notification controls.
- Voice, bounded private calls, desktop screen sharing and mobile viewing.
- Accessible responsive web/PWA experience, understandable failure states, safe retries and data recovery.
- Real qualification on **both Windows x64 native/Tailscale and Linux amd64 Docker**. The owner already selected both; do not ask them to choose again.
- Public source, reproducible/verifiable distributions, maintainable code and usable operator/contributor documentation.

### Completion means

**First finish line — feature-complete candidate:** every agreed v1 feature is implemented and integrated, tests cover its actual behavior, artifacts identify the exact source and build, and every remaining external acceptance item is explicitly listed. A reporting-only alpha is not this finish line.

**Final finish line — v1.0.0:** all G0–G7 requirements are accepted, final distributed bytes are verified, and approved publication/support handoff completes G8. Real host tests, independent review, beta participation and elapsed observation time are required evidence. Do not simulate their completion or shorten them silently.

You may implement code, migrations, tests, scripts, UI and documentation needed for this mission. Use the existing project's architecture and conventions. Preserve unrelated work. Complete reversible engineering autonomously. Ask only for missing product decisions, credentials, external resources or actions that actually require approval. Do not publish, expose private features to production, send messages to other people or change live data without the appropriate authorization. Prepare the concrete artifacts and procedures before requesting final approval.

Old token allocations and claims of unlimited time in previous handoffs are not instructions for this run. Respect your actual environment and budget. If the whole task cannot finish in one run, checkpoint after a coherent verified milestone with precise remaining work. Do not mark the overall mission complete while required work remains.

## 2. Exact starting point

The last independently inspected application state was:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
Source HEAD: 6b7b30157f3bd9246c1dbba442c8f341f4c208af
Application version: 0.4.0-alpha.7
Database schema: 14
Backup format: 2
Private feature: default-off via BRRP_ENABLE_PRIVATE_CONVERSATIONS
P10 packaged commit: 976366148348cb7ea754bb5304c126503ae68639
Packaged frontend build: b17ccfd6cd9325e2
```

Planning documents may have been committed after that review. Inspect current HEAD, status, recent commits, versions and local instructions. If the source moved, establish the delta before acting. Do not reset or overwrite work to force the checkout back to these identifiers.

Architecture: Go server, embedded Svelte/TypeScript client, SQLite, common conversation/message services, centralized authorization with transaction-time checks and authority generations, WebSocket invalidation and embedded Pion media. Keep one coherent implementation; do not build a second chat/auth/storage system for groups or a broad frontend rewrite.

### Existing work to preserve

- Owner/operator distinction, additive roles, restricted channels, MFA/session/recovery controls and current-authority enforcement.
- Shared conversations, account-owned message operation identities, deletion-safe receipts, bounded recoverable outbox and monotonic read cursors.
- Default-off direct-message request/accept/reject flows, canonical pairs, blocking, attachments/search/read behavior and lifecycle cleanup.
- Separate selected-message private evidence and recipient-only pending-request reporting. `report.review_private` grants submitted-evidence review, never arbitrary private-history access.
- Migration/backup validation, package consumers, recovery/rollback scripts, cross-platform build machinery and existing regression coverage.
- Existing design language and straightforward installation model.

### Read first, in this order

1. Any current `AGENTS.md` or applicable repository instructions; then Git status/history.
2. `planning/completion-review-12-and-v1-finish-plan-2026-09-28.md`.
3. `docs/v1-implementation-plan.md`: full scope, D-01–18, WP-01–33, G0–G8, targets, matrices and reset rules.
4. `docs/v1-progress.md` and `docs/completion-remediation-2026-09-24.md`, recognizing their historical sections.
5. ADRs 0001–0009 for identity, authority, durability, licensing, attachments and backup; ADRs 0010–0014 for conversations, receipts, lifecycle, private consent and evidence.
6. `planning/p10-checkpoint/verification.md`, `manifest.json`, `recovery.json`, `recovery-private.json`, `rollback.json`; compare P9 evidence where needed.
7. `README.md`, `docs/deployment.md`, `docs/production-readiness.md`, current release documentation, `Makefile`, `.github/workflows`, `web/package.json` and the existing test harnesses.

Historical prose may describe functionality that has since landed. Current source and applicable later ADRs control implementation facts. The master plan controls release scope unless the owner explicitly changes it. Correct stale current-status claims without rewriting historical evidence as though old runs covered new code.

### Evidence already available

The latest review freshly passed **118/118 frontend unit tests** and **Svelte checking with zero errors/warnings**. It dynamically reproduced three defects listed below, using temporary Go overlay tests; the expected failing probe command took 2.225s. It did not rerun all Go or browser tests.

P10 recorded **102 private-recovery checks**, **23 community-recovery checks**, **57 alpha.6/schema12→14 rollback checks**, **13 default-off browser tests**, **21 private-enabled browser tests**, native package consumption and an independent extracted-source build/consumer. Those results belong to their recorded artifacts and scenarios.

All six native archives were hash-checked and embed clean commit `9763661…`; the source ZIP's 388 entries match it. The current durable manifest records only Darwin and source, so complete its inventory in new evidence. Native cross-compilation does not establish platform support.

Important identities:

```text
Darwin/amd64 ZIP:
0d4e87f81a3a13c77c94d0dc8c088d6bcf63b02c083e3c21804a7e5a800521bf
Extracted Darwin/amd64 executable:
4dc6e2494f847124f4945ee624e9ae5815fb5a0c8d311aad32364e7c48d12d1d
Source ZIP:
8a18084a613fc3c30e82d36f15206adb50ed2fe946de62b275a7f91273ded0df
```

No G0–G8 gate has passed. The last environment check found no Git remote, Docker executable or PowerShell. Discover current availability instead of assuming these constraints persist.

## 3. Execution discipline

Create a concise live implementation/evidence ledger mapping the milestones below to WP requirements, dependencies, status and acceptance. Use separate statuses for implementation, automated verification, real-host/device verification, independent acceptance and publication. Name each unresolved external dependency and the work it actually blocks.

After the initial audit, begin M0 immediately. Provisioning and the media architecture spike should start early in parallel where independent workers/resources exist. Use bounded independent assignments with clear file ownership and review their results. Group/thread work depends on stable shared contracts, and full beta depends on the completed product.

At every mutation boundary, preserve account identity, session validity, current authorization, source identity and transaction ordering. Private content must not leak through counts, errors, search, file URLs, ranges, events, push, metrics, audit or media. Keep queues, pagination, staged files, retention and retries bounded. UI hiding is not authorization.

Use ADR updates for genuinely new contracts before schema/API implementation. Resolve routine engineering choices yourself. Record any unresolved owner policy with a recommended concrete choice and continue unaffected work. Existing MIT/CLA licensing is decided; finish the public-project setup without reopening it unnecessarily.

## 4. M0 — Close the current private-reporting defects

Do this before new private feature expansion. The review found recurring policy drift between adjacent implementations; repair the shared policy boundary as well as each observed symptom.

### M0.1 Audit privacy and retention — R12-01

At the reviewed source, `internal/api/private_request_reports.go:590` writes `private_request.<status>`. General and restricted audit in `internal/api/admin.go:747,800` recognize only `private_report.*`. Maintenance recognizes `private_report.*` and `request_evidence.*`, not the actual request-review prefix.

**Reproduced:** an audit-only account receives 403 from private evidence, but 200 from general audit exposing the request reviewer, evidence ID, action and time. Evidence text was not leaked. The event is absent from restricted audit and qualifies for ordinary 90-day cleanup instead of declared 400-day restricted retention.

Implement one coherent restricted-event classification for writers, both readers and cleanup. Account for existing stored prefixes through explicit compatibility or migration. Preserve current reviewer authorization and non-disclosing results. Tests must invoke a real request review, then read both audit feeds as differently authorized accounts, revoke the grant, and run retention around the relevant boundaries. Do not prove this by seeding a prefix no handler emits.

### M0.2 Capacity and Unicode — R12-02–03

`privateEvidenceIsFull` in `internal/api/private_reports.go` counts only `private_message_reports`. With 5,000 unexpired request reports, a new report succeeded and produced row 5,001. Enforce the combined message/request evidence cap on both submission paths within the transaction. Cover request-only, message-only and mixed populations, expired rows, concurrent final-slot claims, and rollback without quota/block/request/receipt side effects.

Request creation permits 500 Unicode characters, but report submission uses `len(introduction) > 500` bytes. A valid 500-CJK-character introduction creates and previews successfully, then reporting returns 400. Align validation with the existing source contract; test empty, ASCII, CJK, emoji and limit boundaries through request creation, preview and confirmation.

### M0.3 Operation identity, expiry and maintenance — R12-04–05

The replay branch recomputes intent from submitted reason and the receipt's stored attempt without comparing the submitted destination or preview. Correct it so a key cannot acknowledge different work. Bind source kind, conversation, immutable attempt/preview identity, action and explanation according to a canonical documented intent. Resolve an existing receipt before requiring the source to remain pending, because a successful command rejects and clears that source.

Test lost responses, concurrent retries, another account, different reason, another conversation, renewed same-pair attempts, ordinary block/withdraw/accept races, resolution/reopening, and deliberate unblock followed by retry. Historical success must never re-block or imply the current block state.

The first response currently advertises the evidence's 90-day expiry; lookup/replay advertises the receipt's 30-day expiry. Distinguish these fields/meanings consistently. After receipt expiry, return a defined expired terminal result. Today the lookup hides the row but its unique key remains, so a later request can hit a late SQL failure.

Implement bounded reclamation of expired request evidence and bounded receipt compaction. **Do not simply delete operation keys:** expiry must never make an old operation executable again. Reuse the message-receipt spent-key pattern or justify an equally enforceable age protocol. Keep minimal guard records free of evidence text, document their growth/account-lifecycle boundary, and test after cleanup, restart and restore. Correct ADR0014's contradiction that 30-day receipts “outlive” 90-day evidence.

Inject failures after evidence insertion, block insertion, request rejection/clearing and receipt/audit writes, all before commit; also exercise authority refusal and a failed commit. Assert the entire uncommitted operation rolls back. A failure or lost response after a successful commit must reconcile the committed receipt without undoing or repeating its effects. Test later acceptance as the **recipient**, not only the sender who was unauthorized from the start. Keep ordinary Block usable when report storage/quota is unavailable.

### M0.4 Real report UI and reviewer pagination — R12-06–08

Relevant files: `web/src/lib/components/ReportRequest.svelte`, `web/src/lib/request-report.ts`, `web/src/lib/components/admin/PrivateEvidence.svelte`, `web/e2e/directs.spec.mjs`, `web/e2e/evidence-review.spec.mjs`.

Replace the stuck `submitted` flag behavior with explicit states for preview, submitting, definite refusal, changed preview, uncertain outcome, checking, uncommitted outcome and confirmed historical outcome. Preserve the same frozen operation during uncertainty. Allow safe same-operation retry and explicit confirmation of a genuinely new preview. Show actionable errors in every visible state.

Specific failures to reproduce and fix:

- 400/409/429 refusal leaves the current button apparently enabled but its handler inert.
- Transport failure with unchanged authority never offers receipt reconciliation.
- Receipt 404/transport/feature errors are stored but hidden in the uncertain view.
- The command's own invalidation may arrive before or after its response; both orderings must succeed without duplicate work.
- Account/session changes must prevent old responses, text or outcomes from entering the new account's UI.
- Closing/reloading loses the mounted dialog's key. Provide an account-owned minimal recovery design, or explicitly define and test a constrained recovery policy without persisting private introduction text unnecessarily.

Fix the reviewer list: when one source is exhausted and the other has more, do not refetch the exhausted source's first page. Use independent cursor/exhaustion state with `(source_kind, id)` identity and deterministic merging, or one authorized typed server cursor. Test uneven 0/1/51/101-row populations, all rows reachable once, refresh/review races, expiry and reviewer revocation.

Make the disclosure truthful: exact introduction, reporter/sender identity, explanation, reviewer audience, block/reject behavior, 90-day evidence availability, bounded physical cleanup and backups retaining their captured contents. Remove “Nobody else sees it.” Preserve privacy without obscuring consent in dense legal wording.

Add rendered behavior tests. Existing pure admission predicates do not prove async UI recovery. Existing browser tests must establish the race ordering they claim instead of depending on whichever response happens to win.

### M0.5 Recovery and evidence consistency — R12-09–10

- Preserve the real schema13 executables at `dist/brrpbrrp-p9-checkpoint` and `dist/brrpbrrp-p9`. Verify their identity before use. Seed private reports, reviewed state, expiry and relevant membership with that actual old binary, then upgrade and verify exact preservation. P10's schema12→14 rehearsal does not cover this.
- Qualify current schema14→the next candidate too. Add migrations only when required; do not edit applied migrations to retrofit a live database. The next available migration is expected to be 15, but check the tree first.
- Add post-snapshot reviewer-grant revocation: grant before backup, revoke afterward, demonstrate the restored old grant on an isolated host, reconcile before cutover, and verify every protected surface denies access. Keep block/deactivation reconciliation.
- Align Makefile, runbook and CI race budgets. Current CI says 40m; Makefile/runbook say 20m. The P10 API duration was 1201.909s, near 1200s, but its full-suite ledger omits the effective timeout. Recover the exact old command/log if available; otherwise qualify the next frozen candidate with one clearly recorded complete run. Do not infer a full 40m run from an API-only row.
- Record all six archives and their extracted executable hashes, source hash/build identity, schema/protocol/backup/frontend versions and actual consumer results in durable evidence.
- Correct current progress/ADR version drift: progress says schema12, ADR0014 calls the slice alpha8, actual P10 is alpha7/schema14. Preserve old immutable archives and their original names. Use a new prerelease version for changed distributions; alpha8 is a likely next label if unused.

**M0 exit:** all R12 defects have permanent behavioral evidence, retention/receipt policy is coherent, current and prior data survive the required paths, and the report/review UI is usable under failure. Then proceed to M1; M0 alone does not finish this assignment.

## 5. M1 — Durable activity and private notifications

Implement the missing activity foundation first for incoming requests and accepted DMs, then integrate groups/threads as they land. This begins WP-14; it does not close the entire work package until all required sources and platforms pass.

Define the contract before schema work:

- Stable event identity tied to immutable request attempt or message identity.
- Outstanding requests, unread messages, activity acknowledgment and delivered interruptions are distinct states.
- Account/conversation/device preference precedence, mute/quiet behavior, private preview defaults and request-interruption policy.
- Retention, deleted-source behavior, current-access filtering before counts/pagination and bounded storage.
- Active-view suppression, burst grouping and the master plan's 24-hour cross-tab/service-worker deduplication contract.
- Push-denied/unavailable/offline behavior and current-authority deep-link resolution.

Create durable activity with its originating transaction; avoid storing a second copy of private message history. Provide account-owned cursor pagination and monotonic acknowledgment. Default private notifications should not disclose introductions/message text. The inbox must remain useful without push.

Reuse and extend `internal/api/messages.go`, `direct.go`, `contacts.go`, `conversation_access.go`, `mentions.go`, `channel_notifications.go`, `push.go`, `workspace.svelte.ts`, `MessagesInbox.svelte`, `DirectControls.svelte`, `types.ts`, `notifications.ts` and `web/public/service-worker.js` as appropriate.

The current push job checks `channelID`. Do not bypass private authorization by supplying an empty channel ID. Use typed destinations and recheck current consent, membership, block, account and preferences at actual dispatch. Muting suppresses interruptions without erasing history or activity. Unblocking does not replay old unwanted contact.

**Acceptance:** active tab/background tab/closed PWA, two tabs/devices, duplicate/delayed events, account switch, block/removal/deletion during queued dispatch, preference changes, reconnect/offline recovery and denied push. Restore activity/preferences without resurrecting ended sessions or revoked access. Distinguish browser automation from physical platform notification qualification.

## 6. M2 — Consenting groups and complete lifecycle

Complete WP-11/12 using shared conversations and membership, not direct-pair tables or a second message pipeline.

- Explicit invitations and acceptance; ten accepted members initially; bounded invitation creation/expiry/rejection/revocation and abuse controls.
- Name, owner, transfer, leave, removal and deterministic last-owner behavior.
- Explain the D-07 history rule before acceptance: the planned policy lets accepted members see group history; record any still-required owner ratification before broad exposure.
- Define what blocking means in a shared group accurately. Never promise that a block hides all shared-group messages if it does not.
- Removal/leave must revoke history/search/file/range/reply/event/typing/unread/push/media access consistently. Concurrent accept/remove/transfer races must resolve safely.
- Integrate group activity and notifications through M1.
- Finish lifecycle distinctions for deactivation, retained-history removal and explicit erasure. Preserve tombstones and remaining members' agreed history.
- Implement the remaining bounded user-export contract with explicit ownership/access rules; never export unrelated conversations, other users' credentials, sessions, reviewer-only evidence or secrets. Keep operator-equivalent export powers distinct.
- Cover orphan files, quotas, retention, backups and post-snapshot erasure/security reconciliation.

**Acceptance:** real multi-account browser journeys plus outsider/ordinary-admin denials across every delivery surface; retry/race/lifecycle tests; upgrade/recovery fixtures with mixed direct/group state and exports. Finalize any conditional export scope explicitly rather than silently dropping the master-plan obligation.

## 7. M3 — Threads, moderation and everyday polish

### Organized discussion — WP-13 and completion of WP-14

Add named community threads with stable identity, inherited channel authorization, explicit promotion of legacy replies, title/activity summaries, follow/unfollow, independent read cursors, archive/reopen/lock and root tombstones. Preserve old IDs/links. Provide desktop pane/mobile route and complete bounded cursor navigation.

Integrate followed replies and group/thread activity with M1. Reading a thread must not clear unrelated channel unread state. **A 1,000-reply fixture must be fully reachable without duplicates or unbounded DOM/cache growth.** Lock/archive/access loss must affect send/search/notifications consistently.

### Moderation and invitations — WP-15

Finish delegated report assignment/resolution, expiring timeouts and invitation controls. Enforce moderator hierarchy and timeout/slow-mode decisions transactionally across alternate REST/socket/action paths. Expiry restores only powers the account otherwise has. Add bounded multi-use invites with expiry/revocation and safe entry roles. Keep private evidence review limited to deliberately submitted evidence, with audit and retention intact.

### Composer, navigation and onboarding — WP-16/17

Complete safe formatting without raw HTML, scoped stable-identity mentions, copy links, accessible actions, member context, edit/delete/reply states, image-description behavior, and upload progress/cancel/retry that preserves the originating draft.

Verify malicious markup/URLs, code blocks containing mention-like text, IME, emoji, RTL, long content and failed uploads. Provide consistent loading/empty/error/removed/reconnecting states, focus restoration, non-hover actions, keyboard alternatives, responsive layouts and mobile virtual-keyboard behavior.

Walk through owner install/setup/recovery/invite and member join/chat/voice journeys. Improve confusing copy and unnecessary steps while keeping security decisions explicit. Make PWA install/update and stale-client recovery understandable, preserve safe drafts, and keep authenticated API/private content out of inappropriate service-worker caches.

**Acceptance:** end-to-end journeys at narrow/wide layouts with keyboard and meaningful accessibility checks; rendered failure/recovery tests; consistent search/export/render behavior. Automated accessibility checks supplement the later human review.

## 8. M4 — Reliable voice, private calls and screen sharing

Start the bounded WP-18 architecture spike early after M0 while other work proceeds. Inspect the existing embedded Pion implementation and fixed audio receiver assumptions. Measure the ability to support the planned publication model before rewriting signaling or selecting a separate SFU.

Record an ADR comparing extension of embedded Pion with a separately self-hosted SFU only if measurements justify it. Include installation/resource implications, compatibility, authorization and migration. Preserve the simple deployment model where feasible.

Implement WP-18–22:

- Community and private calls using current conversation consent/membership, including listen-only participation and accurate device diagnostics.
- Distinct microphone, screen-video and optional screen-audio publications with stable identity/generation; a screen track must not masquerade as microphone state.
- Explicit user-started desktop capture, understandable browser denial/cancel, app/browser stop, ended tracks, mute/deafen, leave/move, reconnect and authority withdrawal.
- Server-enforced participant/publication limits across community/private rooms. Stop forwarding immediately on revocation; clear capture and indicators for cooperative clients.
- One screen publisher per room, two per server as qualification targets; mobile viewing; no promise of mobile publishing or universal system-audio capture.
- Configurable STUN/TURN, direct and forced relay paths, UDP-blocked TCP/TLS fallback and relevant IPv4/IPv6 routes. Record actual candidate/relay selection; Tailscale/HTTPS success alone does not prove media.

**Acceptance:** non-cooperative sender controls for server denial, browser lifecycle tests, real microphone/listening observations on the supported matrix, two-hour calls, 100 share start/stop cycles, disconnect/reconnect and restricted-network routes. Use the master plan's frozen speech/screen fixtures and quality rubric. Synthetic RTP and fake capture are useful regressions but cannot establish physical media quality or hidden-capture behavior.

## 9. M5 — Both-host operations, capacity and public releases

Provision these resources early. If unavailable, prepare runnable tools, exact prerequisites and an evidence template, request the missing access, and continue independent implementation. Mark unexecuted procedures as pending.

### Windows x64 native/Tailscale

Qualify setup, trusted HTTPS, UAC/service installation, LocalService/ACL behavior, reboot/service recovery, tray or operator controls where applicable, logs, update, failed-update rollback, uninstall/data preservation and offline owner recovery. Use the actual packaged executable on the chosen supported Windows build. Include a novice operator rehearsal; undocumented manual database repair is a failure.

### Linux amd64 Docker

Build and run the actual image with documented Compose/configuration. Verify non-root identity, read-only behavior where intended, writable volumes/permissions, health/shutdown/restart, backup/update/rollback, disk/mount failures, image identity and the reachable media/TURN topology. Static Docker-input checks and a source ZIP build are not Docker runtime qualification.

### Recovery and monitoring

Qualify encrypted independent/off-host copies, key custody, backup age, verification and restore into a clean destination. Treat TLS/configuration/recovery keys explicitly without putting secrets into test logs. Prove complete source data and post-snapshot security/erasure reconciliation before cutover.

Targets: **RPO ≤24h; RTO ≤60m** on the reference dataset, measured from the declared event and including human response, retrieval/decryption, reconciliation and verification. Record total outage separately. Test failed copy, missing destination, corrupt/truncated archive and interrupted update. Never run an old executable on a new-schema database; rollback uses its supported pre-upgrade restore path.

Finish redacted diagnostics, external HTTPS/WebSocket checks, backup-age/storage health and alerts that actually reach an approved recipient. A configured alert is not a delivered alert. Avoid private content/credentials in metrics or unbounded label cardinality.

### Capacity qualification

Freeze the support profile before measurement. Proposed targets from section 10 of the master plan:

- 250 accounts, 50 active people, 100 WebSockets/five per account, 100 community channels.
- Ten-member groups; 20 media participants serverwide/ten per room, including viewers.
- One share per room/two per server, up to nine remote viewers/share, initially 2.5 Mbit/s video and optional 64 kbit/s screen audio per publisher.
- One million messages and 20 GiB attachments; preserve current message/upload limits unless separately qualified.
- Reference allocation: four cores/vCPUs, 8 GiB RAM, SSD with at least 80 GiB free initially and measured 100 Mbit/s symmetric network. This is a test allocation, not a proven minimum requirement.

Use versioned F-S/F-T/F-X fixtures, a separate generator and valid synchronized/monotonic latency measurement. Run 30-minute baselines, ramp, two-hour peak and **24-hour mixed soak on each deployment** with actual backups, retention, private traffic, revocations and media. Exercise full disk, termination/reset, slow clients, overload and network/TURN faults on disposable data.

Apply the master-plan budgets: LAN delivery p95 ≤500ms/p99 ≤1s; target search p95 ≤1s/p99 ≤2s; healthy reconnect p95 ≤10s; proposed RSS ≤2GiB, mean CPU ≤65%/one-minute p95 ≤85%, warmed idle RSS within 15% after comparable cycles, egress below 70% of reference link. Separate expected fault/admission denials from healthy failures. Record zero unauthorized delivery, duplicate logical sends or lost acknowledged content under the approved durability contract. Ratify/revise unapproved budgets openly before qualification, never after a failed run to manufacture a pass.

### Public project and release engineering

Complete canonical repository/registry/release identities, maintainer access/recovery, tested private security intake, enforcement/support contacts and accepted maintenance commitments. Preserve MIT licensing and the recorded CLA/signatory decision. Check exported source/history for secrets and host-specific private operational material before publication.

Run hosted CI on actual configured runners. Host immutable approved prior-release baselines with checksums and provenance; local-only executables are useful transition fixtures, not a complete contributor/CI supply chain. Record dependency notices/SBOM, appropriate provenance, exact source and final package hashes. Sign required Windows downloads; only advertise supported Mac distribution if its runtime/signing/notarization requirements are met.

Produce and consume final native/source/container artifacts from their actual extracted/downloaded bytes. A binary built separately in the checkout does not qualify an archive. Include all manifests and exact compatibility/rollback directions. Do not overwrite alpha7 packages with changed bytes.

## 10. M6 — Independent acceptance and real beta

Complete WP-28–31, with G1–G5 and the required reviews accepted before the full beta:

- Independent security/privacy review of authorization, current delivery, receipts, source snapshots, uploads, recovery and release dependencies. Resolve critical/high findings and rerun affected evidence. An automated self-review or an assumed reviewer is not independent acceptance.
- Human keyboard/screen-reader, contrast, focus, zoom/reflow, reduced-motion and mobile assessment of scoped critical flows, alongside automated checks. Record the actual browser/OS/device/assistive-technology versions. Do not advertise blanket accessibility conformance from axe alone.
- Supported Chromium/Firefox/WebKit coverage plus physical Android/iOS for applicable journeys, push/background behavior, media and virtual keyboard.
- A non-author installs/updates/restores with the documents; a contributor builds/tests from a clean source distribution. Address usability failures in the product and docs.
- At least **three consenting communities**, spanning **both required deployments**, use the complete product for **14 consecutive days** with incident, build, journey, backup and alert records.

Follow master-plan beta criteria: at least 90% of prescribed journey attempts without author intervention and 100% eventual completion of every required journey; 30 cross-device call sessions including six longer than 30 minutes; no unresolved disclosure, acknowledged-data-loss, migration, forwarding/capture or core-flow blocker. These numbers supplement controlled tests.

Apply observation resets honestly. A severity 0/1 fix restarts the affected deployment's clean beta window after verification; retain unaffected evidence only with an explicit rationale. Obtain a named owner/rationale/workaround for any permitted material issue. Do not downgrade a security/accessibility/core-flow blocker into a cosmetic item.

Private features should remain default-off during unqualified development. Before claiming the complete v1 product is generally available, qualify and record the intended shipping defaults through the privacy/exposure gates; leaving required features inaccessible behind an unexplained development flag does not fulfill the product promise.

## 11. M7 — Final candidate and v1.0.0

Freeze the candidate's source, dependencies, defaults, schema/protocol/backup compatibility, supported matrix and actual distributed artifacts. Finish release notes, known issues, installation, upgrade, rollback, recovery and support material.

Run the complete required checks and host/artifact consumers against this identity. Observe the final candidate for **seven consecutive days on both deployments**. Perform clean independent recovery and rollback using its materials. No new feature work enters that window.

Executable/dependency/schema/protocol changes create a new identity and restart final observation. Documentation/repacking/signing changes need appropriate final-byte checks and a documented evidence-reuse decision.

Once G7 is actually accepted, present the concrete release and evidence for explicit owner publication approval. Then publish through authorized destinations, verify real downloads/signatures/checksums, stage rollout, confirm recovery access and hand off maintenance ownership. Define first-week/day-30 review work without claiming an unconfigured scheduler exists.

If external acceptance prevents publication, the correct result is a clearly identified feature-complete candidate or an accurately scoped earlier checkpoint, plus exact remaining gates. Do not tag/claim v1.0.0 merely to satisfy the version number in this assignment.

## 12. Verification tools and evidence rules

Use focused tests during development, then one integrated final baseline for the frozen candidate. Broaden testing when the change creates a concrete risk. Do not run the 20-minute API race suite repeatedly after documentation-only edits or hide failures with retries.

Known local tools from the last review, to rediscover if necessary:

```text
.tools/go/bin/go                 Go 1.27.1
Node                           24.2.0
GOCACHE                        /private/tmp/brrpbrrp-go-build
GOMODCACHE                     /Users/cameronamer/go/pkg/mod
PLAYWRIGHT_BROWSERS_PATH       /private/tmp/brrpbrrp-playwright-browsers
```

Use an available dependency cache/offline mode where suitable; do not confuse a missing dependency/network permission with an application test failure. Avoid the incomplete historical `/private/tmp/brrpbrrp-go-mod` cache.

Typical frontend gates from the repository root:

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
```

After the timeout entry points are aligned, record the effective Go commands explicitly, for example:

```sh
.tools/go/bin/go vet ./...
.tools/go/bin/go test -race -count=1 -timeout 40m ./...
```

Read harness arguments/environment before running package/browser/recovery commands. The browser harness is `web/e2e/run.mjs`; `BRRP_E2E_PRIVATE_CONVERSATIONS=1` enables private journeys, and `BRRP_E2E_BINARY` selects the executable. Run both default and opted-in suites against a newly built candidate with the matching embedded frontend. Do not let a skipped private project count as a pass.

Preserve and extend these consumers:

- `scripts/check-package.mjs`
- `scripts/check-source-package.mjs`
- `scripts/upgrade-recovery.mjs`
- `scripts/upgrade-recovery-private.mjs`
- `scripts/upgrade-rollback.mjs`
- Docker-input/runtime and browser-step checks in the existing Makefile/workflows.

For each consequential result record commit, dirty status, build/version IDs, artifact hash, OS/runtime/configuration, exact command, effective timeout, exit status, pass/fail/skip counts, duration and report/log path. Record what it does **not** establish. Preserve previous failures and explain the fix; a rerun alone is not a diagnosis.

Use new evidence directories for new candidates. Keep historical P8/P9/P10 records attributable. Tests must exercise real handlers, emitted events, async states and extracted artifacts. Avoid tests that merely mirror an implementation helper or seed the wrong event name.

## 13. Scope boundaries and external inputs

The current v1 scope defers camera video, federation, cross-server identity, multi-community hosting in one process, native mobile apps, bot/plugin marketplaces, discovery, recording/transcription, remote control, E2EE, enterprise SSO and clustering. Do not spend the release budget adding those while core v1 remains unfinished. Do not silently remove groups, threads, notifications, private calls or desktop sharing to make completion easier.

Request only unresolved resources with a precise reason and prepared procedure:

- Actual Windows/Tailscale and Linux Docker staging/recovery hosts and supported versions.
- Physical client devices and direct/relay/restricted-network access.
- Owner-controlled Git/registry/signing destinations and credentials through approved secret handling.
- Encrypted independent-backup destination, key custody and alert recipient authorization.
- Named security/accessibility reviewers, pilot communities and support/release owners.
- Specific outstanding history/deletion/support-policy decisions.

Keep independent coding moving while waiting. Do not substitute invented hostnames, fake hosted CI runs, assumed policy approval or simulated observation time.

## 14. Required checkpoint and final report

At each coherent checkpoint, provide:

1. Source commit/branch, clean-or-dirty state, application/schema/backup/frontend versions and artifact identities.
2. Closed R12 findings and completed WP requirements, with direct behavioral evidence.
3. Features newly usable by owners/members, including screenshots or demonstrations where they make review easier.
4. Exact tests run and meaningful failures/skips/limitations; distinguish recorded old evidence from fresh results.
5. Migration, retention, privacy and recovery consequences with operator-facing actions.
6. G0–G8 status and the precise reason each remaining gate is open.
7. External inputs required, ready procedures, and the next executable task.
8. A continuation note sufficient for another context-free coder if the run must stop.

Do not say “nothing remains queued” while the milestones in this prompt remain open. The desired outcome is a polished, tested and operable product with honest release evidence. Start with the source audit and M0, then keep progressing through the remaining implementation toward that outcome.
