# Standalone implementation handoff: integrate the private product and finish v1.0.0

Prepared 29 September 2026 after independent completion review 13 of the activity/group changes. Copy **everything below the separator** into the next coding agent. This replaces the review-12 prompt for current implementation work; preserve older reviews as historical evidence.

---

You are taking over **BrrpBrrp**, an open-source, self-hosted community communication application. The owner wants a polished, approachable Discord replacement that ordinary people can install, operate and enjoy. Continue implementation through a feature-complete release candidate and then **v1.0.0** when its actual acceptance gates pass.

This is an implementation assignment. Read the current source and evidence, repair the integration gaps below, finish the remaining product features, test them, create attributable artifacts and continue toward release. Do not stop after another plan, management-only demo or locally green helper suite. Use coherent, reviewable milestones and report the evidence for the actual member/operator journey.

## 1. Mission, authorization and finishing points

The product promise is one community per installation, local identity, no required vendor account, useful community/private/group chat, organized threads, activity and notification controls, voice/private calls, desktop sharing and mobile viewing, accessible responsive web/PWA interaction, and dependable recovery.

The owner already requires **both Windows x64 native/Tailscale and Linux amd64 Docker**. Do not ask them to choose again. The project's MIT/CLA licensing decision is also recorded; finish public-project setup without reopening settled choices unnecessarily.

**First finish line:** every agreed v1 feature is implemented and integrated into an approachable product, meaningful local tests pass, and a feature-complete candidate has exact source/artifact identities. List any external acceptance still pending. A working backend or group membership interface alone is not this finish line.

**Final finish line:** G0–G7 are genuinely accepted, the final bytes pass qualification and observation, then explicit owner publication approval and verified rollout complete G8. Both-host tests, independent reviews, real beta use and elapsed observation time are required evidence.

Implement necessary application code, migrations, tests, scripts, UI and documentation. Preserve unrelated work, the existing architecture and working behavior. Resolve routine engineering choices autonomously. Ask only for unresolved product decisions, external access/resources or actions needing approval; prepare concrete artifacts and procedures before final approval requests. Do not publish, message others, expose unqualified private features to production or alter live data without authorization.

Respect your actual budget and environment; older quota or unlimited-time instructions do not carry forward automatically. If the whole assignment cannot finish in one run, stop after a coherent verified milestone with a continuation record. Do not mark the overall goal complete while required work remains.

## 2. Current baseline and evidence

Last independently inspected state:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
Source HEAD: 6e36c10c162bffa525fc0f676b5b7fb5092b4719
App: 0.4.0-alpha.7
Database schema: 17
Backup format: 2
Private feature: default-off via BRRP_ENABLE_PRIVATE_CONVERSATIONS
Latest packaged source: 976366148348cb7ea754bb5304c126503ae68639
Latest packaged schema: 14
Latest packaged frontend: b17ccfd6cd9325e2
```

Inspect status/history, local instructions and current versions first. Planning documents may have been committed after this source review. If application code advanced, inspect its delta and credit fixes supported by evidence. Never reset someone else's work to these identifiers.

Architecture: Go server, SQLite, embedded Svelte/TypeScript frontend, common conversations/messages, centralized authorization with transaction-time checks and authority generations, WebSocket invalidation, recoverable outbox and embedded Pion media. Extend these systems; do not add parallel group messaging, authentication or storage stacks.

### What to preserve and credit

- Community chat, roles/restricted channels, owner/operator distinction, MFA/session/recovery, attachment durability and existing permission coverage.
- Shared conversation/message identities, account-owned operation keys, deletion-safe send receipts, bounded outbox and monotonic read cursors.
- Consented direct messaging, blocking, selected private evidence and existing recovery/package consumers.
- **Verified R12 server repairs:** centralized restricted audit classification, combined evidence capacity, Unicode character validation, destination/preview-bound receipt replay, separate receipt/evidence expiry and explicit expired-key refusal. Eight selected regressions passed in review 13.
- New activity/acknowledgment/preference APIs and typed push helper foundation.
- New group membership model and create/invite/accept/list/transfer/remove/leave interface.
- Aligned 40-minute race timeouts and complete historical P10 archive inventory.

### What the latest report does not establish

The group browser journey covers membership management, with page reloads for changed authority. **The client cannot open group chat**, and common audience/reporting paths still reject groups. M1 has no activity inbox/private preference interface, no retained cross-tab deduplication and no wired normal DM/request push publication. Several promised bounds, lifecycle and privacy guarantees fail focused probes.

Fresh review-13 checks: **132 frontend tests passed**, Svelte **zero errors/warnings**, **eight existing server repair tests passed**, and **nine temporary probes reproduced defects**. No full race/browser/host suite was rerun by that review.

The coder reports vet, full Go tests, format/build and browser **13 default-off / 22 private-enabled** at current HEAD. M0's recorded race **1330.041s**, recovery **23/113** and rollback **57** are **schema14/source153202d** evidence. Do not reuse them as schema17 acceptance. The schema17 group/activity restore fixtures are still missing.

The six native archives plus source archive remain historical schema14 packages, with matching hashes. The current `dist/brrpbrrp` is a development build stamped `f4d3a7c…`, `vcs.modified=true`, SHA-256 `e457d2a30084a59e7d4ec5aa8732506ffe99cdf4dee92562189a08dae3cc6a3e`. There is no clean schema17 distribution. Do not mistake this mutable binary for a final artifact from HEAD or overwrite old alpha7 archives with new bytes.

No G0–G8 gate has passed. The last environment check had no Git remote, Docker or PowerShell. Discover current resources before assuming those constraints remain.

### Read first

1. Applicable `AGENTS.md`/repository instructions, Git status and recent changes.
2. `planning/completion-review-13-and-v1-finish-plan-2026-09-29.md` — current findings **V13-01–10** and evidence distinctions.
3. `docs/v1-implementation-plan.md` — R requirements, WP-01–33, D decisions, G0–G8, support/quality budgets and observation reset rules.
4. `docs/v1-progress.md`, the remediation ledger and current release documentation, distinguishing historical from current claims.
5. ADRs 0001–0014 for authority, durability, consent, lifecycle and receipts; **0015 activity** and **0016 groups** for the new contracts. Several claims in those latest ADRs are not yet implemented; verify them against real behavior.
6. `planning/m0-checkpoint`, `planning/p10-checkpoint`, recovery scripts, `scripts/release.go`, `Makefile`, `.github/workflows`, `web/package.json` and browser harnesses.

## 3. Execution strategy

Maintain one concise implementation/evidence ledger, mapping V13 findings and remaining WP requirements to status, dependency, owner, test and artifact. Distinguish implemented, locally verified, host/device verified, independently accepted and published.

Begin with the private group authority/lifecycle boundary, then integrate group chat and reliable activity. Finish the outstanding report UI/retention work and current-schema recovery alongside that integration. Continue to M3–M7. Begin the media architecture spike and real-host provisioning early in parallel where independent work is possible.

Use bounded assignments and clear file ownership if delegating. Review results against the user-visible flow. A test name, positive constant, SQL fixture or helper predicate is not evidence that the feature works through its supported route.

At every async/mutation boundary preserve account/session identity, current authority, immutable source identity and transaction order. Filter private reads/counts/errors/events/files/push/audit/media consistently. Do not broaden ordinary admin access to private content to make implementation easier.

Add migrations for changes to installed schemas; do not silently edit applied migrations. Schema17 is the current baseline; the next available migration is expected to be 18, subject to current-tree verification. Record new or revised contracts before implementation and update their acceptance evidence afterward.

## 4. First milestone: finish private group integration and safety

### A. Authority, live invalidation and stream cancellation — V13-03

`internal/api/groups.go` currently commits membership/ownership changes with plain `tx.Commit`. A reproduced removal returned 200 while authorization revision stayed **1→1** and a registered attachment-stream context stayed active. This is missing cancellation in the production stream registry, not a claim of observed network exfiltration.

Route group accept/remove/leave/transfer and relevant account changes through the established authority commit/fence protocol. Define which transitions advance authority, invalidate snapshots, cancel streams/queued work/media and publish targeted events. Keep the database mutation, revision and required audit consistent; publish only after a successful commit.

Fix the corresponding client admission contract. `MessagesInbox.svelte:123–145` clears direct data but not group/invitation/member arrays. Those arrays render raw after verification returns. `groupLifecycle:230–239` assigns an awaited member response without capturing the account, generation and group. Clear or withhold stale private metadata immediately; fence every result and mutation continuation.

**Acceptance:** two live pages remain open through accept/transfer/remove; permissions and visibility update without reload. Hold old history, membership and file responses across removal, fail the confirming reload, and prove old group names/members/content cannot reappear. Test stream cancellation, event/typing denial, queued notification denial and future media invalidation. Verify retained members continue working.

### B. Group lifecycle audit privacy — V13-02

Real `group.invited` and `group.joined` events currently appear in general audit to a nonmember holding only `audit.read`, including actor usernames and private group IDs. Evidence text was not exposed.

Define an explicit audience/retention policy for private group lifecycle events. Apply it to handler writes, all readers, historic rows and maintenance using a coherent classifier. Do not automatically treat all private group metadata as selected report evidence or give `report.review_private` arbitrary group access. Test actual handler-emitted actions with allowed and denied principals, and revocation after a previously allowed read.

### C. Invitation and terminal-group lifecycle — V13-04

Reproduced sequence: sole owner leaves with a pending invitation → group state becomes `removed` → invitee accepts with **200** → history returns **404**. Acceptance does not verify active group state. `discardGroupIfEmpty` merely updates state although its comment/ADR promise cascaded deletion.

Define and implement the exact terminal-group contract: active/removed state, pending invitations, remaining metadata, message/file/activity retention and physical cleanup. Close invitations atomically when their group ends; never allow an expired/revoked/old invitation to re-admit someone or claim success into a dead group. Validate current owner/account/group/invitation eligibility in the accepting transaction. Test idempotent acceptance, removed-member retry, renewed invitations, concurrent last-slot acceptance and group closure.

Apply block/contact eligibility to invitation creation and acceptance; current invite code omits both. Blocking must stop future unwanted contact/invitations without pretending to hide an existing shared group's history. Disclose that distinction clearly.

Enforce finite creation/invitation limits and cleanup, including account/server bounds, pending/expired records and refusal rollback. Do not equate a list's 200-row response limit with bounded storage. Last-member Leave must explain its actual consequence before the final action; do not claim erasure if the implementation only marks a group removed.

### D. Make groups usable through the shared conversation pipeline — V13-01

Current gaps include:

- `state.go:89–115` returns direct descriptors only.
- `workspace.svelte.ts:200–245` has direct/channel-only admission, route and URL helpers.
- Group cards show management actions but no Open conversation action.
- `conversationAudienceIDs` refuses groups; message/typing fanout stops there.
- `private_reports.go:164,334` rejects group selected-message reporting with 404.

Generalize the typed private-conversation descriptor and common controller deliberately. Add groups to snapshots, navigation/picker, active conversation state, deep links, unread/read cursors, drafts, outbox, history, composer, uploads, replies/reactions/pins, scoped search, user context and current-authority recovery. Use group-specific display identity without inventing a direct peer. Keep permissions explicit: “not direct” must not accidentally mean community moderator privileges.

Enable common message/reaction/typing delivery and selected-message report preview/confirmation for accepted group members. Keep nonmember and unrelated-admin denial, snapshot bounds, private-evidence audience and retention intact. Audit direct-only branches by policy; do not globally replace string checks without evaluating their meaning.

**Required rendered journey:** create → invite → disclosed history acceptance → open group → exchange messages live between two pages → reply/reaction/upload/download/search/read → lost-response same-key recovery → report a selected message → transfer live → remove while old responses/streams are held → verify denial and retained members' continued use. Include a nonmember and a former member. Page reloads cannot substitute for required live updates.

## 5. Complete activity and real private notification delivery

### A. Correct activity read/paging/source semantics — V13-05

The current list applies eligibility after SQL's limit. The review reproduced 51 ineligible request rows above one valid message producing **zero entries, no next cursor, count52**. Put current eligibility before page limits and counts; preserve complete bounded cursor traversal under filtering. Handle group source kinds and nullable actors: the schema's valid null actor currently returns **500**.

Bind request activity to its immutable attempt, not merely a pair that happens to have another pending request. Define behavior for accepted/withdrawn/expired/renewed requests, deleted messages, removed conversations, blocks, unavailable actors and regained membership. Old entries must not become false new activity or expose revoked metadata. Distinguish a durable historical record from an actionable current request.

Define acknowledgment precisely using activity identity/cursors, including inclusive/exclusive boundaries, account scoping and new events arriving between list and acknowledge. Do not use message IDs accidentally as activity cursors. Separate message unread, activity acknowledgment, outstanding requests and group invitations. Test stale/cross-account cursors, filter changes, newest/oldest boundaries and hidden-heavy pages.

### B. Enforce bounds and actual expiry — V13-06

The documented 2,000-row cap is not enforced on insertion: seeding 2,000 unacknowledged rows and posting a real message yields **201 and 2,001 rows**. An unacknowledged row survives **31 days** unless the account also exceeds the cap. The existing bounded test sends five messages and checks positive constants, so it does not establish either property.

Implement a coherent finite overflow/retention contract and bounded cleanup. Preserve message durability and explicitly decide what happens when catch-up activity is full; never silently lose an acknowledged message or allow an offline recipient's backlog to grow forever. If revising the ADR to a bounded summary/gap policy, document the user-visible behavior and its recovery semantics. Test exact cap, concurrent admission, acknowledged/unacknowledged expiry above and below cap, duplicate source events, restart and large maintenance backlogs.

### C. Build the activity and preference interface — V13-07

There are no frontend consumers of `/me/activity`, `/me/activity/acknowledge` or `/me/notification-preferences`. Build an account-owned controller and visible activity view, preferably using the existing Messages/navigation patterns. Add current-authority deep links, explicit acknowledgment, loading/offline/empty/expired-source/retry states, complete pagination and cross-device refresh.

Add direct/request/group preference controls and finish account/conversation/device precedence, mute/quiet behavior and private preview defaults. Keep interruption preferences separate from history and unread state. Integrate community mentions and later followed-thread activity into the same product instead of leaving private activity as an inaccessible API.

### D. Connect actual producers and device delivery

`queueActivityNotice` is not called by normal DM/request producers; the only call is the incomplete group-invitation path. Group activity is missing from dispatch eligibility. Implement real post-commit publication for each supported source, with stable row/event identity and no duplicate work after receipt replay. Failed/rolled-back transactions must produce no notices.

Typed destinations must validate exact source liveness, current recipient, consent/membership, block, account availability and preferences at each new device dispatch. Unknown destinations fail closed. A late queued event must not borrow the eligibility of a renewed request or later membership.

Align payload and service worker identity: server supplies `tag`, current worker reads `message_id`. Implement the declared account-scoped **24-hour retained deduplication** across tabs and worker, active-conversation suppression, burst behavior and safe private previews. Server row uniqueness alone does not prove one OS notice per event/device. Document browser-storage reset limitations.

Fix authenticated navigation: private payload `/messages` has no implemented activity route. Use a supported current-authority route that opens the intended inbox/source safely. Correct the UI copy and ADRs to match actual delivered behavior.

**Acceptance:** real producer → queued job → per-device checks → app/worker handler → correct authorized destination. Cover active/background/closed PWA, two tabs/devices, duplicate/delayed events, account switches, mute/block/removal/deletion during delay, offline/denied push and unblock without historical replay. Activity remains useful if push never arrives. Physical platform qualification remains an external acceptance item, not a local test claim.

## 6. Finish reporting, lifecycle/export and current recovery

### A. Remaining reporting work — V13-08

Preserve the eight passing server repairs. Finish the actual component and maintenance behavior:

- Expired `private_request_reports` text remains stored after maintenance; add bounded cleanup. Compact aged command-receipt metadata while retaining a minimal spent-key guard or equally enforceable protocol. An old key must never become new work after cleanup/restart/restore.
- In `ReportRequest.svelte`, `changed` has no route back to a confirmable preview. Transport errors enter `refused` instead of uncertain reconciliation. “Back to report” closes the dialog and loses explanation/key. Fix those transitions, including unrelated authority refresh and the operation's own invalidation ordering.
- Preserve the same frozen key/intent while outcome is unknown; reconcile before creating different work. Define account/session/reload recovery and fresh block state separately from historical receipts.
- Add the backup and physical-cleanup caveat to the disclosure; keep exact saved text/identities and ordinary Block clear.
- In `PrivateEvidence.svelte`, skip exhausted-source requests entirely; an empty query suffix still fetches page one. Preserve `(source_kind,id)` identity in both merge and keyed rendering.

Use rendered component/network-order tests, not only pure predicate tests. Establish held response order explicitly. Test refusal, 409 preview change, response loss before/after commit, receipt not-found/failure, retry, account switch, revoke, mixed source pagination and reload recovery. Failures before commit roll back; lost responses after commit reconcile without undoing or repeating effects.

### B. Account/content lifecycle and bounded export — V13-09

Real account anonymization currently leaves group ownership pointing to an unusable Deleted member account; remaining members cannot administer it. This was reproduced. Physical erasure also cascades the owner row: surviving membership still selects the group for `/api/groups`, then its required owner lookup fails and the endpoint returns 500. That physical-erasure consequence is source-confirmed, not a separately executed probe.

Define and implement deterministic non-escalating ownership disposition across self-removal, administrative removal, deactivation/reactivation, ban and physical erasure. Keep consent, retained-history/tombstone policy and privacy intact. Do not grant ordinary administrators private-history browsing as an owner-recovery shortcut. Make irreversible consequences clear before action.

Finish explicit deactivation versus retained-history removal versus content erasure, group membership cleanup, message/attachment/search/report consequences, orphan files and quota accounting. Restore reconciliation must account for erasure and post-snapshot membership/ownership decisions.

Implement the remaining bounded per-user export contract: explicit account ownership/current-access rules, finite work/storage/expiry, safe file handling, cancellation/failure recovery, reauthentication where appropriate and reviewable output. Exclude unrelated conversations, other people's credentials/sessions, reviewer-only evidence and operator secrets. Test allowed/denied export, access withdrawal, erased sources, large pagination and interrupted downloads. State backup limits without legal compliance claims.

### C. Current-schema migration and restore qualification — V13-10

The new schema13 tool assumes exactly one new migration (`upgrade-schema13-private.mjs:94`); it rejects a valid 13→17 result. Validate the complete expected registry and data invariants. Use preserved real prior executables, not freshly compiled source pretending to be an old release.

Add populated **15/16→17** fixtures for the activity table rebuild: IDs, actor nullability, acknowledgment, interruptibility, preferences, source references and foreign keys must survive. Test interrupted/repeated migration and unknown-future-schema refusal. If another schema is added, include current17→new with the same guarantees.

Finish the recovery fixture's ownership/MFA ceremony and revoke reviewer grants through the supported API. Remove SQL fallback plus unconditional success checks. SQL seeding can establish a declared fixture, but cannot qualify operator revocation, authority revision or queued-delivery fencing. This is routine rehearsal engineering; do not stop for an owner decision merely because the earlier checkpoint called it one.

Back up and restore mixed direct/group state: pending/accepted/rejected/expired invitations, owner/member changes, messages/files/search, read cursors, operation receipts/spent keys, private evidence/expiry, activity acknowledgments and notification preferences. Reconcile post-snapshot reviewer revocation, group removal/transfer, blocks, account state and erasure before cutover. Verify positive retained-member controls and negative old-link/event/stream controls.

Include schema13 tools in the explicit source-package inventory and appropriate CI gates, with approved baseline acquisition and hashes. Record exact commands, executable identities and machine-readable reports. Keep historical M0/P10 results intact and label new evidence by current source/schema.

**Integrated private-product exit:** a member can use group chat, activity and reporting through the interface; live authority changes are enforced; lifecycle/export work has explicit outcomes; the current schema survives qualified upgrade/restore. Produce a new clean prerelease checkpoint with fresh extracted consumers when verified. Then continue to the remaining milestones below.

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

Start the bounded WP-18 architecture spike early alongside private-product completion. Inspect the existing embedded Pion implementation and fixed audio receiver assumptions. Measure the ability to support the planned publication model before rewriting signaling or selecting a separate SFU.

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

The timeout entry points now agree on 40 minutes. Record the exact effective Go commands for the current candidate, for example:

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
2. Closed V13 findings, remaining R12 obligations and completed WP requirements, with direct behavioral evidence.
3. Features newly usable by owners/members, including screenshots or demonstrations where they make review easier.
4. Exact tests run and meaningful failures/skips/limitations; distinguish recorded old evidence from fresh results.
5. Migration, retention, privacy and recovery consequences with operator-facing actions.
6. G0–G8 status and the precise reason each remaining gate is open.
7. External inputs required, ready procedures, and the next executable task.
8. A continuation note sufficient for another context-free coder if the run must stop.

Do not say “nothing remains queued” while the milestones in this prompt remain open. The desired outcome is a polished, tested and operable product with honest release evidence. Start with the source audit and the private group integration milestone, then keep progressing through the remaining implementation toward that outcome.
