# BrrpBrrp v1.0.0 implementation plan

**Plan date:** 23 September 2026  
**Planning baseline:** 0.4.0-alpha.3; database schema 2  
**Status:** Proposed implementation and release contract; owner decisions remain open  
**Scope of this task:** Documentation only. No application, schema, configuration, dependency, infrastructure or release changes are authorized by this document. *(This constraint governed producing the plan itself. The implementation assignment that followed it, and the tranches recorded in the ledger above, are authorized by their own handoffs: review the ledger and the release record for what exists.)*

> **Current implementation: 0.4.0-alpha.8, schema 25, backup format 2.**
> Roles, migration/recovery, send recovery, account removal, community reporting,
> slow mode, channel notification muting and opt-in consented DMs/blocking have
> local implementations. Alpha.5 remains the packaged checkpoint. See the
> [progress and evidence ledger](v1-progress.md) and the
> [architecture decision records](adr/). This plan remains the contract; the
> ledger records what exists and what does not.

This plan defines the work needed to turn the present small-group alpha into a dependable, self-hosted, open-source community platform. It is a delivery plan rather than a claim that the features, performance targets or production checks below already exist or pass.

The recommended v1 includes community roles, restricted channels, direct and small-group messages, organized threads, controlled notifications, reliable voice, desktop screen sharing, mobile participation, recoverable installations and a maintainable public project. Camera video is a proposed post-v1 feature. These scope choices and the staffing assumption are recommendations pending owner confirmation.

**Reading guide:** Use sections 1–4 and 8–9 to approve scope, decisions and resources; sections 5–7 to turn the plan into implementation issues; and sections 10–17 to verify that the result is ready to release. The base estimate is 158–261 focused engineer-days across 33 packages, before the visible reserve and external waits detailed in section 9.

## Contents

1. [Release definition and planning rules](#1-release-definition-and-planning-rules)
2. [Audited starting point](#2-audited-starting-point)
3. [Product scope and user journeys](#3-product-scope-and-user-journeys)
4. [Decisions and responsibilities](#4-decisions-and-responsibilities)
5. [Architecture and behavioral contracts](#5-architecture-and-behavioral-contracts)
6. [Data migrations and compatibility](#6-data-migrations-and-compatibility)
7. [Work breakdown](#7-work-breakdown)
8. [Milestones and dependency order](#8-milestones-and-dependency-order)
9. [Effort, staffing and resource plan](#9-effort-staffing-and-resource-plan)
10. [Capacity and performance qualification](#10-capacity-and-performance-qualification)
11. [Verification and acceptance matrix](#11-verification-and-acceptance-matrix)
12. [Deployment, recovery and operations](#12-deployment-recovery-and-operations)
13. [Open-source governance and release engineering](#13-open-source-governance-and-release-engineering)
14. [Pilot, release candidate and launch](#14-pilot-release-candidate-and-launch)
15. [Risk register and change control](#15-risk-register-and-change-control)
16. [First implementation iteration](#16-first-implementation-iteration)
17. [Evidence ledger and final release checklist](#17-evidence-ledger-and-final-release-checklist)
18. [Source map and reference standards](#18-source-map-and-reference-standards)

## 1. Release definition and planning rules

### 1.1 What v1.0.0 must mean

A community owner can install a documented release on either required deployment path, invite members, delegate moderation, hold public and restricted conversations, exchange private messages, use voice and screen sharing, update safely, and recover the server from independent backup. Members can understand who can access a conversation, control unwanted contact and notifications, and keep their work during ordinary network failures. Contributors can obtain the licensed source, reproduce the development workflow and report vulnerabilities privately.

The release has a declared support boundary and capacity profile. It makes an explicit compatibility commitment for documented configuration, installation, backup/restore and supported application interfaces. Version 1.0 establishes that commitment; feature count alone is not the release criterion. See [Semantic Versioning](https://semver.org/) for the versioning convention.

### 1.2 Release invariants

| ID | Required invariant |
| --- | --- |
| INV-01 | No unauthorized conversation content or sensitive metadata is returned through HTTP, WebSockets, files, search, notifications or media. |
| INV-02 | A retry of one logical send within the supported retry window creates at most one stored message, even after deletion; a successful acknowledgement has a documented durability meaning. |
| INV-03 | Existing schema-2 identities, message IDs, channel IDs, attachment relationships and deep links survive supported upgrades. |
| INV-04 | Permission removal closes future access consistently and clears affected application views; previously downloaded or delivered data cannot be recalled. |
| INV-05 | The core application works without a vendor account or mandatory vendor-hosted application/media service. |
| INV-06 | Updates and restoration preserve authorized data and expose a tested recovery path; a failed migration never masquerades as successful startup. |
| INV-07 | Critical flows are usable with keyboard, assistive technology and the supported physical mobile devices. |
| INV-08 | Published limits, platform support and release claims have evidence from the exact candidate being promoted. |
| INV-09 | Open-source publication includes an owner-approved license, buildable source, dependency notices and a real maintenance/security reporting process. |
| INV-10 | Private data is excluded from public logs, diagnostics, source archives and release artifacts. |

### 1.3 How to interpret this plan

- **Existing** means observable in the current source or recorded release evidence. This planning exercise did not rerun application tests.
- **Required** means required by the recommended v1 scope after scope approval. It does not authorize immediate implementation.
- **Proposed target** means a number to approve before qualification; it is not an achieved measurement.
- **Decision** means an owner or technical reviewer must resolve an identified choice before dependent work is committed.
- **Gate** means recorded pass/fail evidence is required to advance. A document, mockup, cross-build or feature flag does not substitute for the specified test.
- All work packages in section 7 are **planned**. No new feature is marked complete by this document.

## 2. Audited starting point

### 2.1 What should be retained

| Area | Current baseline | Evidence | v1 consequence |
| --- | --- | --- | --- |
| Core architecture | One Go process, embedded Svelte frontend, SQLite on local storage | [Architecture](architecture.md), [database initialization](../internal/db/db.go) | Keep the simple installation model unless measured evidence requires a specific change. |
| Release identity | 0.4.0-alpha.3, schema 2 | [App version](../internal/app/version.go), [package metadata](../web/package.json) | Establish an immutable source baseline before further releases. |
| Chat | Persistent messages, replies, reactions, pins, mentions, uploads, FTS search and idempotent sends | [Messages API](../internal/api/messages.go), [message store](../internal/db/messages.go) | Reuse one messaging pipeline across new conversation types. |
| Discussion view | Direct replies, 50 per page, latest 250 in the view; separate drafts; replies remain channel messages | [Replies state](../web/src/lib/replies.svelte.ts), [replies API](../internal/api/conversations.go) | Named threads, lifecycle, subscriptions and full-history navigation remain new work. |
| Identity | Local passwords, hashed sessions, invitations, admin/member distinction, bans/deactivation, administrator TOTP/recovery | [Authentication](../internal/api/auth.go), [MFA](../internal/api/mfa.go), [route map](../internal/api/router.go) | Extend identity and authority incrementally; do not require SSO for v1. |
| Voice | Pion Opus SFU, device controls, mute/deafen/PTT, room movement, moderation and reconnect handling | [RTC](../internal/rtc/sfu.go), [voice client](../web/src/lib/voice.svelte.ts) | Retain audio behavior while qualifying network reliability and redesigning track handling. |
| Client resilience | Bounded message windows, scroll anchors, stale-response guards, explicit PWA updates, optional text draft persistence | [Workspace state](../web/src/lib/workspace.svelte.ts), [update handoff](../web/src/lib/reload-drafts.ts) | Generalize established mechanisms to DMs and named threads. |
| Operations | Native installer/services, Docker source configuration, backups/restore, diagnostics and production-check | [Installer](../internal/installer/install.go), [backup](../internal/backup/backup.go), [production check](../internal/app/production.go) | Complete real-host evidence and strengthen recovery/monitoring semantics. |
| Local evidence | Alpha.3 records 55 frontend tests, API/database race tests, static/build checks and a Chromium/a11y journey | [Acceptance record](acceptance.md#040-alpha3-conversations--2026-09-23) | Useful regression baseline; it does not prove new v1 features or target deployments. |

### 2.2 Architectural gaps that control implementation order

1. **All channels are currently shared with all active members.** Channel/state/unread queries are global. The message store's `viewer` argument personalizes reactions; it is not an authorization boundary. Attached uploads are readable by authenticated members. Sensitive events use global broadcasts. These are coherent with today's public-workspace model and must change before restricted conversations ship.
2. **Media routing is audio-specific.** The client has one microphone sender and nine receiving slots; the server accepts one initial offer and uses a fixed audio-oriented model. Screen sharing needs track identity, negotiation, feedback and subscription work.
3. **The source is not yet a public open-source release.** A contributor guide and issue templates exist, but no project license or Git repository metadata is present in this workspace. Repository ownership, hosted CI, signing and private disclosure intake are unestablished here.
4. **Backups are implemented, but their monitoring is too weak for the intended release contract.** The current freshness check accepts a recent nonempty ZIP by filesystem modification time. Backup creation writes the final archive name while work is in progress. Completion, integrity, copy identity and recovery success need stronger evidence.
5. **Data lifecycle semantics need decisions.** Account deletion currently cascades through authored messages. Deleting a reply parent detaches replies. Named threads and DMs cannot inherit these consequences accidentally.
6. **Current capacity evidence is local and short.** Historical loopback tests used 20 chat clients and ten synthetic Pion peers; their combined client/server heap is not process RSS. They do not establish production capacity or perceived audio quality.

### 2.3 Current limits and unresolved evidence

Current limits include 100 accounts, 100 channels, 100 total WebSocket connections, five connections per account, ten simultaneous voice participants serverwide, 12 MiB per upload, five attachments and 4,000 characters per message. Actual limits are enforced in several layers; changing one constant will not establish a larger support envelope.

Native packages exist for six OS/architecture combinations. Both **Windows/Tailscale** and **Linux Docker** remain required real deployment gates. Cross-compilation, Compose parsing and local browser resizing do not prove installed service behavior, container networking, phone behavior, off-host recovery or external alert delivery. The [production runbook](production-readiness.md) correctly leaves those gates pending.

## 3. Product scope and user journeys

### 3.1 Recommended v1 scope

| Requirement | v1 boundary | Completion owner / package |
| --- | --- | --- |
| R-01 Self-hosting | One community per installation; local identity; Windows native and Linux Docker reference deployments; no required vendor account | OPS / WP-03, WP-24 |
| R-02 Accounts | Invitations, account recovery, session management, member MFA and protected privileged actions | BE / WP-05 |
| R-03 Community permissions | Owner/admin/moderator/member roles, custom role grants, restricted text/voice channels and effective-access preview | BE + FE / WP-06–08 |
| R-04 Direct conversations | Request-based 1:1 DMs and consenting group DMs, with blocking/reporting and shared chat features | BE + FE / WP-09–12 |
| R-05 Organized discussion | Named community threads, follow/unfollow, unread state, archive/lock, searchable full history and legacy reply compatibility | BE + FE / WP-13 |
| R-06 Message experience | Safe basic formatting, accessible actions, attachment handling, scoped mentions, stable links, drafts and retry recovery | FE + BE / WP-09, WP-16–17 |
| R-07 Notifications | Unified activity inbox, per-conversation preferences, private defaults, device management and duplicate suppression | BE + FE / WP-14 |
| R-08 Moderation | Delegated moderation, timeouts, slow mode, report queue, bounded invite controls and auditable actions | BE + FE / WP-15 |
| R-09 Voice | Reliable community voice and bounded private conversation calls; device/recovery diagnostics and direct/relay paths | MEDIA / WP-18–22 |
| R-10 Screen sharing | Explicit desktop capture, mobile viewing, one publisher per room, server-enforced limits and stop controls | MEDIA + FE / WP-20–22 |
| R-11 Mobile/accessibility | Responsive web/PWA flows, actual mobile testing and a scoped accessibility audit | FE + QA / WP-17, WP-29 |
| R-12 Durability/recovery | Safe migrations, verifiable backups, off-host recovery, update and rollback drills | BE + OPS / WP-04, WP-23–24 |
| R-13 Operability/capacity | Useful redacted diagnostics, external checks, delivered alerts and a measured support profile | OPS + QA / WP-25–26 |
| R-14 Public project | Chosen license, public repository, contribution/security process and verifiable releases | OWNER + REL / WP-01, WP-27, WP-31–33 |

### 3.2 Deliberate post-v1 work

Defer camera video, federation, multi-community hosting inside one process, cross-server identity/friends, native mobile applications, bot/plugin marketplaces, public server discovery, recording/transcription, remote desktop control, E2EE, enterprise SSO/SCIM, active-active clustering and large broadcast rooms. Defer arbitrary Discord-style inherited allow/deny permission overrides in favor of the bounded model in section 5.2.

Keep custom emoji/stickers, elaborate profile customization, voice messages and broad third-party integrations outside the critical path. Basic display name/avatar color and a useful member profile are in scope. No support claim for mobile screen publishing or universal shared system audio is implied.

These exclusions are scope proposals. If the owner requires one for v1, add its design, implementation, test and support costs before accepting a date; do not absorb it as an unestimated enhancement.

### 3.3 Required end-to-end journeys

| Journey | Required outcome |
| --- | --- |
| J-01 New owner | Install → establish trusted HTTPS → create owner → configure recovery → create public/restricted channels → invite a member. No undocumented shell repair. |
| J-02 Returning member | Accept invitation → optionally enable MFA → send/read/search → return on another device → see correct authorized unread state and preferences. |
| J-03 Private conversation | Request DM → recipient accepts → exchange attachment/reply → create consenting group → remove member → old links and queued events cease granting access. |
| J-04 Busy discussion | Start/follow thread → navigate older replies → read on two devices → archive/reopen/lock → preserve legacy links and unrelated channel unread state. |
| J-05 Unwanted behavior | Block contact/report message → authorized moderator reviews selected evidence → timeout/resolve → action is audited without opening unrelated DMs. |
| J-06 Live room | Join authorized room → test microphone → talk → share screen → stop via browser/app → reconnect after outage → leave with capture indicators off. |
| J-07 Interruption | Lose POST response or socket → keep draft/attachments → retry safely → reconcile exactly once → regain correct read position and access. |
| J-08 Operator recovery | Detect unhealthy server → receive alert → locate verified independent backup → restore into clean destination → validate data/permissions → resume service within the approved objective. |
| J-09 Upgrade | Obtain verifiable release → preflight/backup → upgrade → recover stale clients → validate → exercise documented rollback on a separate rehearsal installation. |
| J-10 Contributor | Clone licensed source → build/run/test from documentation → submit reviewed change → trace a published artifact to its source and checks. |

## 4. Decisions and responsibilities

### 4.1 Decision register

Recommendations below provide a planning baseline, not approvals. Resolve a decision by recording the choice, owner, date, rationale and affected packages in an architecture decision record (ADR). Exact migration numbers, supported OS builds and release dates are assigned only after their relevant decisions are settled.

| ID | Decision | Recommended baseline | Due / consequence |
| --- | --- | --- | --- |
| D-01 | Source license, copyright holder and contributor terms | Resolved in [ADR 0006](adr/0006-licensing-and-contribution-terms.md): MIT, named copyright holder and CLA/signatory record | Public repository, intake and maintainer identities still require WP-01 completion |
| D-02 | v1 product boundary | Scope in section 3; desktop sharing included, camera video deferred | G0; changes rebaseline estimates |
| D-03 | Supported hosts/architectures | Fully qualify Windows x64 native/Tailscale and Linux amd64 Docker; promote others only with native runtime evidence | G0; determines device, CI and signing costs |
| D-04 | Capacity promise | Qualification targets in section 10, retaining present limits until promotion gates pass | G0 provisional; freeze before load qualification |
| D-05 | Team and responsibility | One primary developer with AI assistance; named owner/operator plus independent review and physical testers | G0; no calendar commitment without accepted staffing |
| D-06 | Privilege model | One transferable owner; additive roles plus explicit channel access gates; no arbitrary inherited overrides | G0; blocks role schema and UI |
| D-07 | DM trust/history | No unrelated-admin DM access through ordinary application APIs; operator-equivalent backup/credential takeover powers are owner-only and disclosed; accepted group members see full group history after disclosure | Before WP-09; blocks membership queries and UI promises |
| D-08 | Account/content deletion | Deactivate by default; explicit content-erasure policy; preserve conversation continuity with tombstoned identity where approved | Before WP-12 schema approval |
| D-09 | Moderation evidence | Community implementation in [ADR 0011](adr/0011-account-removal-and-retained-evidence.md): selected text/explanation expires 90 days after report creation, with current-access review and bounded physical cleanup. The original proposal of 90 days after resolution is not implemented; private-report retention and any policy change require an explicit decision | Before private WP-15 reporting; preserve disclosed expiry until a policy change is approved |
| D-10 | Privileged MFA/recovery | Require owner/admin enrollment after setup; allow all members; offline break-glass recovery with audit | Before WP-05 completion |
| D-11 | Conversation data model | **Resolved by [ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md):** one shared descriptor (a channel conversation's id is its channel id), operation identity `(account, key)` with the destination inside the intent digest, server-enforced expiry with deletion-safe receipts, opt-in account-scoped outbox, monotonic read cursors and the staged M-C migration mechanics | Before WP-09 migration; no duplicated DM storage stack |
| D-12 | Media implementation | First evaluate embedded Pion extension; optional separate self-hosted SFU only if the spike justifies it | End WP-18; may change install model and estimate |
| D-13 | Network/privacy dependencies | Operator-configurable STUN/TURN; optional push/external previews; no mandatory telemetry | G0 contract; verified by WP-19, WP-31 |
| D-14 | Backup objectives and custody | RPO ≤24 h; RTO ≤60 min from the declared recovery event, including operator response, on the reference dataset; independent encrypted copy and documented key custody | G0; requires destination and recovery owner |
| D-15 | Signing, repository and registry identities | Owner-controlled release destinations; signed Windows downloads; signed/notarized Mac downloads if officially supported | Start WP-01; credentials must exist before G5 |
| D-16 | Durability and compatibility | WAL/FULL production durability proposal; separate app/protocol/schema/backup versions; stable documented v1 interfaces | Before WP-04 performance baseline |
| D-17 | Support/security response | Publish a commitment only after a maintainer accepts it; latest stable minor plus a defined transition period | Before beta; blocks unsupported SLA claims |
| D-18 | Thread migration/lifecycle | Keep old reply messages/links; explicitly promote a reply discussion into a named thread; root deletion tombstones the root | Before WP-13 migration |

### 4.2 Responsibility model

| Role | Owns |
| --- | --- |
| OWNER — product/project owner | Scope, licensing, privacy/deletion policy, expenditure, release approval and public identity |
| TL — technical lead | ADRs, dependencies, data/contracts, reviews and compatibility decisions |
| BE — backend engineer | Authentication, authorization, migrations, messaging, storage and job correctness |
| FE — frontend engineer | Conversation UX, drafts/reconciliation, accessibility and browser behavior |
| MEDIA — media engineer/reviewer | SFU/signaling, codecs, network behavior, capture lifecycle and measured quality |
| OPS — deployment operator | Hosts, HTTPS, firewall/TURN, backups, alerting, restore and incident ownership |
| QA — verification owner | Fixtures, automation, physical matrix, reproducible results and release ledger |
| SEC — independent security reviewer | Authorization/private-data design review, adversarial test review and finding disposition |
| REL — release maintainer | CI, source/artifact identity, signing, provenance, promotion and rollback assets |

One person can hold several roles, but each gate still needs explicit ownership. AI assistance can help implement and review work; it does not provide physical devices, operating credentials, independent human sign-off or staffed support. Name an alternate recovery/release operator where feasible before public launch.

## 5. Architecture and behavioral contracts

### 5.1 Preserve the core architecture and separate responsibilities

Retain Go, Svelte, SQLite and the embedded frontend. Keep a single application process and local persistent data as the reference installation. An independently self-hosted TURN relay is an optional network component. A separate SFU is a decision at WP-18, not a hidden new dependency. PostgreSQL, Redis and microservices are not prerequisites for the proposed scale.

Refactor only around new boundaries: identity/authorization; conversation resolution; message operations; recipient-scoped events; notification decisions; and media publications. Frontend session, conversation cache, draft/outbox, notification and media state should have distinct responsibilities. Preserve working stale-response guards and bounded caches while reducing the growth of the central workspace controller. Each extraction must preserve behavior and ship with its associated feature or regression evidence.

### 5.2 Permission model to approve

Use the following bounded model for v1:

1. Check authentication, active account, valid session and installation-wide restrictions first.
2. Classify the resource as a community channel/thread or a direct/group conversation. Never infer that an administrator may read every resource.
3. For community content, an owner/admin may manage and view community channels. Ordinary members need public visibility or an explicit private-channel grant through membership or an assigned role. A thread inherits its parent channel's access.
4. For DMs/groups, require current accepted conversation membership; owner/admin status does not bypass this check. Selected report evidence is a separate, audited moderation resource.
5. Derive ordinary capabilities from the assigned role grants, with the default role as the floor an account holds only when it has no assignment at all (ADR 0001, “The default role is a floor, not a grant”). The default role is not deletable. Custom role hierarchy controls delegation and moderation, not hidden access precedence.
6. Apply action-specific restrictions: account timeout, closed request, block relationship, archived/locked thread, announcement posting gate, file quota or call capacity. A general role grant does not bypass these restrictions unless that bypass is a documented capability.
7. Validate actor authority and membership again inside security-sensitive write transactions. Apply authorization before query pagination/counting and before outbound delivery.

Categories are durable organizational objects in v1. Moving a channel between categories does not change its visibility. A private channel uses explicit role/member access grants; an announcement channel uses an explicit posting-role gate. Permission inheritance and arbitrary conflicting allow/deny overrides are deferred. If full overwrite inheritance is required, specify its precedence algebra and tests before schema approval and re-estimate WP-06–08.

Proposed capability families: read/send/react/upload; create/manage threads; mention everyone; connect/speak/share screen/moderate voice; manage messages/timeouts/bans/reports; manage channels/roles/invitations; inspect audit; administer settings/backups. Final names are frozen with the API contract. Role managers cannot grant undelegable powers, edit peers/superiors, transfer ownership implicitly or remove the last recovery authority.

Separate community administration from operator-equivalent authority. Current administrators can download whole-server backups and set another account's password; those paths can defeat a superficial DM privacy claim. For v1, full backup/export, credential takeover, ownership transfer and security-key recovery are owner/operator-only powers, nondelegable through ordinary custom roles, guarded by recent strong authentication and audit where exercised through the application. Delegated administrators can moderate community accounts without silently inheriting those powers. The owner/host operator remains trusted and can access storage; the DM membership boundary is not E2EE or protection against that operator.

### 5.3 Resource authorization coverage

| Surface | Required rule |
| --- | --- |
| Bootstrap, navigation and picker | Return only visible channels/conversations and permitted capabilities; private titles and member lists are private data. |
| Messages, context and replies | Resolve the message's conversation before reading; parent/context paths use the same rule as ordinary history. |
| Search | Filter eligible conversations and history inside the query, before limit/count/cursor processing. No hidden hits, excerpts or misleading result counts. |
| Pins, thread summaries and unread | Scope every count and cursor to the viewer; hidden conversation activity must not affect visible totals. |
| Originals, thumbnails and range requests | Resolve current attachment → message → conversation access; unclaimed uploads remain uploader-only. |
| Mentions/autocomplete | Eligible recipients must have access; rendering a mention does not grant access. Broad mentions require a capability. |
| WebSocket events/typing/presence | Deliver only permitted resource metadata. Global presence can remain community-wide only as an explicit privacy setting; private room membership cannot. |
| Voice/share join, resume and move | Check current conversation access and action permission at every transition; removal stops media/subscriptions. |
| Push/in-app jobs | Re-evaluate active account, membership, blocks, preferences and device association at dispatch; recheck access on click. |
| Reports/audit/diagnostics | Restrict by purpose and role; no general administrator DM browser or private message bodies in routine diagnostics. |
| Exports/backups | Member export contains only permitted personal data under policy; full server backup is an explicit privileged operator capability. |

Hidden resources should normally return a non-disclosing unavailable/404 result; denial of an action on a visible resource can return 403. Define a consistent client experience without implying that a guessed ID exists. Do not promise resistance to every timing side channel without a corresponding threat analysis.

### 5.4 Revocation and concurrent operations

Maintain an authorization revision or equivalent invalidation mechanism. A role/membership/visibility change commits before the application acknowledges success. Subsequent authorized operations must use the new policy. Re-evaluate or discard queued sensitive events/jobs; disconnect or resynchronize impacted sockets; stop affected media; invalidate client cache, search, participant and notification state.

The committed authorization revision is the linearization point for admitting new reads, mutations, streams and subscriptions. Cancel active private file streams and media when that revision removes access; recheck before subsequent application-level streaming work and reject new range requests. Tests must distinguish already admitted in-flight bytes from work admitted after revocation. An implementation that merely hides the resource while allowing the original stream or call to continue fails this contract.

Specify the boundary realistically: bytes already sent, in-flight network packets and copies already downloaded cannot be recalled. The testable guarantee is that no newly authorized server send or resource read is permitted after the relevant revocation boundary. Queued work from an older revision must not bypass it. Race tests must include revocation during transaction commit, file streaming, queue backlog, notification dispatch and voice resume.

### 5.5 Conversation and message contract

Prefer one common conversation descriptor and messaging service. Compare an additive `conversations` model with a typed extension to the existing channel container in ADR D-11. The chosen implementation must preserve existing IDs and reuse message, attachment, reaction, search and notification paths; a second DM message database/API implementation is not acceptable.

Proposed conceptual records, not final SQL:

| Record | Essential meaning / invariants |
| --- | --- |
| Conversation | Stable ID, kind, lifecycle, community-channel mapping where applicable and authorization revision |
| Community channel | Existing stable channel ID, type, name/topic/order, category, visibility, access grants and posting policy |
| Direct pair | Canonical unordered account pair unique within the installation; concurrent creation returns one conversation |
| Group membership/invitation | Accepted/pending/left/removed state, inviter, timestamps and authority; no unsolicited membership |
| Thread | Stable identity, parent channel, root reference/tombstone, title, creator, activity, archive/lock state |
| Read/follow/preference | Per account and conversation/thread; no shared public read receipt is implied |
| Message | Stable UUIDv7 ID, author/tombstone, conversation scope, reply/thread references, immutable idempotency identity and edit version |
| Notification/report | Stable event/evidence identity, eligible recipient/reviewer, lifecycle and explicit expiry |

One acknowledged send maps to one stored logical message. Reusing an idempotency key with different conversation or content/attachment intent returns a conflict instead of silently treating different work as the same send. Attachment claiming and quota changes occur atomically with message creation. Retried references are revalidated against current access and attachment availability.

Preserve idempotency across deletion and retention. The current uniqueness constraint lives on the message row and disappears when that row is deleted. Add a receipt/tombstone with original intent fingerprint and a deleted/expired outcome, independent of surviving message content. Proposed automatic outbox retry window: 24 hours; receipt retention: at least 72 hours and never shorter than accepted retry eligibility. Specify server-enforced expiry/operation admission in D-11 before pruning receipts, so an expired operation cannot silently become a new retry. After expiry, offer text recovery and an explicit new send with a new key. Test committed send → lost response → delete/purge → original retry: no replacement message, attachment claim or notification. Receipts retain minimal metadata, not erased message bodies.

Use server-authoritative snapshots plus scoped invalidation events for v1 unless a durable event log is justified by evidence. Add protocol/capability and resource-version information sufficient to reject stale events and reconcile changed/deleted content. A durable global event log, background offline delivery and exactly-once network transport are not promises in this plan.

### 5.6 DM and group rules

- A 1:1 conversation starts as a request. The recipient accepts, rejects or blocks. Pending requests carry a bounded introduction, with no attachment delivery or live call before acceptance.
- Concurrent A→B and B→A creation resolves to one pair. Closing a DM hides it for the current user; it does not erase the other participant's history.
- Groups require invitation acceptance. Proposed initial cap: ten accepted members. New members are told they will see existing group history before accepting. Prior-history isolation is a different design requiring revised search/context/count rules.
- A removed or departed group member loses server access to that conversation. Ownership transfer/last-owner departure is explicit; no orphaned management authority.
- A block in either direction prevents subsequent 1:1 message sends/edits, attachment claims, reaction additions, contact requests, notifications and calls. Retained history remains available under retention policy, with own-content deletion and report controls; unblocking does not automatically resend suppressed work. Rejected requests have a bounded retry/cooldown policy. In an already joined shared group/community, a block must not pretend to provide confidentiality: offer local hiding and leave/report controls. It cannot remove another member's authorized access to shared history.
- Group membership changes produce minimal system events visible only to eligible members. A private call is authorized by the conversation membership, not a globally discoverable voice-channel ID.
- Reports disclose only selected evidence the reporter was authorized to see. A moderator receives no general browse/search access to unrelated DM history.

Community moderation timeouts restrict community messages/reactions/thread creation and community speaking/sharing according to the published action matrix. They do not grant control over private DM/call activity. Installation bans/deactivation stop all account access and require the corresponding higher-level authority; private group owners can remove members from their own group. UI wording and audit records must distinguish these scopes.

### 5.7 Named threads and legacy replies

Retain ordinary reply edges. Do not automatically convert every old reply chain into a new thread or move messages out of existing channel history. Offer an explicit promotion action that creates a thread identity and records eligible legacy messages without changing their IDs. Define whether nested reply branches are included during promotion, show the scope to the actor, and record the operation transactionally.

New thread messages use a thread scope under one parent channel. Search and moderation resolve through that channel; v1 has no independently private thread membership. Existing promoted messages may retain their legacy channel placement, but they count once in search and thread history. The migration cannot silently change old read cursors.

Archive is a lifecycle state that permitted actions can reopen; lock prevents ordinary replies until a moderator unlocks. Deleting the original content leaves a tombstone when a named thread survives. Deleting a whole thread is a separately confirmed moderation operation with defined retention/report consequences. Maintain a separate thread unread cursor and subscription; reading a thread does not clear unrelated channel messages.

Full thread history must be cursor-accessible beyond 250 messages. Keep a bounded rendered/window cache rather than loading the full history into memory. Follow/unfollow, active/archived listings and activity summaries must use the same access rules as the messages.

### 5.8 Drafts, outbox, navigation and notifications

Use stable conversation IDs for draft/read/cache keys. Provide channel/thread/DM links, Back/Forward behavior and access-aware deep-link recovery after login. Prefer an adjacent discussion pane on desktop and a full-page conversation on mobile, tested with assistive technology.

Preserve in-memory drafts by default. Extend the explicit device-persistence preference to a bounded recoverable text outbox and attachment references; document expiry, shared-device exposure and sign-out clearing. Do not persist passwords, session tokens or attachment bytes in that outbox. Revalidate destination and staged attachments on retry. If access or a parent disappears, retain recoverable unsent text with an actionable explanation rather than silently discarding it. Private draft restoration must verify account and destination before rendering.

Read cursors advance monotonically for the account/scope, only when the relevant messages were actually visible under the agreed focus rules. Opening an inbox, background tab, history window or different thread does not mark unseen content read.

Notification precedence is: no access/inactive account/blocked relationship → suppress; then conversation/account/device preferences; then active-view duplicate suppression. Default private notifications contain a generic activity notice, not message text. Stable IDs coordinate tabs and server push, with an account-scoped 24-hour retained deduplication window as the proposed app behavior. Within an intact supported browser store, the app issues at most one OS notice per event/device in that window. Browser data deletion, reinstall and provider behavior are outside that lifetime guarantee. Push remains best effort; the in-app inbox is the authoritative unread activity record.

### 5.9 Media contract

Keep microphone, optional screen video and optional screen audio as separately identified publications. Every publication has a stable owner, room/conversation, kind, generation and permitted subscribers. Stale signaling, duplicate offers, unauthorized tracks and cross-room subscriptions must be rejected. Specify negotiation/glare handling, replacement, ICE restart, RTCP feedback, congestion behavior and cleanup before building capture UI.

Every screen viewer joins the authorized room and consumes a participant seat, including listen-only viewers. Limit a room to ten accounts including the publisher and a shared screen to nine remote subscribers. Permit only one active room/call per account; transfer to another device is explicit. A user can join listen-only without granting microphone capture. Private calls are joinable from their conversation in v1, without outbound ringing; initial entry and new capture require a user action. Transport recovery may resume the same still-authorized room with still-live tracks, but ended screen capture cannot be silently reacquired. Adding ringing later requires its own invitation, acceptance, timeout and multi-device behavior.

Moving rooms ends screen and screen-audio publications before entering the new audience; starting another share requires another explicit action. Microphone mute affects microphone audio only; screen audio has its own explicit toggle. Deafen stops received audio and outgoing microphone audio, preserving the prior microphone choice for undeafen; it leaves outgoing screen audio under its separate explicit control and keeps any active sharing indicator visible. Moderator screen-stop stops forwarding both screen tracks. Private call owner/participant authority is membership-scoped; community moderators do not gain access to unrelated private calls through media controls. Installation bans still terminate all affected sessions/media.

In a private group, the group owner may stop a participant's publication; ordinary participants can stop their own publication and locally mute/leave. In a 1:1 call, neither participant gains remote capture control over the other's browser; block/leave ends the authorized connection. On any revoke/stop, the server must stop forwarding regardless of client cooperation. The normal client must stop its relevant capture tracks and indicators within a proposed two-second foreground deadline after receiving the stop. A modified client may keep local capture, and frames already transmitted or buffered cannot be recalled; the security guarantee is server forwarding control, not remote control of arbitrary devices.

Desktop sharing starts only from an explicit user action and browser source chooser. Browser capture permission cannot be silently reused; returned screen audio is optional and varies by browser/source. Cancellation, browser stop, permission loss and moderator stop must end associated publications. Reconnect must not silently start a new capture. These requirements follow the [getDisplayMedia contract](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getDisplayMedia).

Propose one screen publisher per room and two per server, with 720p/15 fps motion and 1080p/5 fps text presets, adaptive reduction, a viewer audio-only option and no transcoding/recording. These are qualification targets, not existing capabilities. Choose a codec interoperability policy during WP-18 and prove it across supported browsers; do not assume codec support from capture success alone. Screen congestion must not starve voice.

Make STUN/TURN operator-configurable. Keep embedded IPv4/UDP TURN as a simple option and add a tested external self-hosted relay path with authenticated short-lived credentials and TCP/TLS where UDP is blocked. Define IPv6 and relay credential revocation explicitly. Core private-network operation must not require public STUN, push or external preview services; optional dependencies and their data flows must be documented.

## 6. Data migrations and compatibility

### 6.1 Migration sequence

Assign numbered migrations after schema 2 only when the relevant ADR and schema review are complete. The logical order below prevents feature work from becoming one unreviewable migration.

| Stage | Change set | Required compatibility proof |
| --- | --- | --- |
| M-A | Ownership/role metadata, assignments and authorization revision | All existing admins retain intended authority; owner designation is explicit; ordinary users gain no new privileged capabilities. |
| M-B | Durable categories, channel visibility/access grants and posting policy | Existing channels remain public with identical IDs/history/order; no channel becomes accidentally invisible or restricted. |
| M-C | Shared conversation mapping, direct/group membership, blocks and prerequisite account-deletion/tombstone policy | Existing channel/message IDs and links remain valid; direct pair uniqueness and membership invariants hold; destructive legacy cascades do not contradict new continuity promises. |
| M-D | Named threads, message/root tombstones, subscriptions and independent read state | Old replies remain available and searchable once; promoted content preserves IDs and declared history placement; surviving threads have an explicit deleted-root representation. |
| M-E | Notifications, moderation/report records and retention metadata | Old mentions/read state are not broadened into unintended notifications; report visibility is restricted. |
| M-F | Remaining erasure/export/retention work, backup metadata and supporting indexes | Earlier tombstone prerequisites are already active; exports, retention and restore preserve the selected policy. |

Stages may be divided further. Never merge unrelated migrations solely to reach a preferred version number. Ship incomplete private-data features disabled at the API boundary as well as the UI until the privacy gate passes.

### 6.2 Required migration procedure

For each migration: inspect schema/version/free space without automatic migration; quiesce writes and acquire the exclusive data lock; produce and verify the final pre-upgrade recovery snapshot while writes remain stopped; execute transactional changes with an explicit transaction/query interface; backfill; verify invariant counts and foreign keys; validate search; commit; record old/new app/schema versions; and expose failure without starting incompatible services. The current `db.Open()` automatically migrates, so a preflight/backup path must inspect the old schema without invoking that behavior. A live backup taken before the write barrier is useful but is not the final no-gap upgrade snapshot. Test interrupted, repeated and already-applied attempts.

Preserve message UUIDv7 ordering and stored relationships. FTS5 references `messages.rowid`; a table rebuild must preserve rowids or rebuild the FTS index transactionally. Review cascading foreign keys before adding deletion/tombstone behavior. The current single-connection store can deadlock if a helper opens a separate store query while its transaction holds the connection; new abstractions must accept the current transaction.

Fixtures must include schema-2 alpha.3, each supported intermediate release, non-ASCII names/paths, retained uploads, replies/pins/reactions, revoked/expired sessions, multiple admins, bans/deactivation and deliberately inconsistent/corrupt input. Unsupported schema versions fail with a recovery instruction, not best-effort mutation.

### 6.3 Durability and backup compatibility

The existing SQLite connection uses WAL with `synchronous=NORMAL`. SQLite documents that a committed transaction can be lost after an OS crash or power loss under that setting, while an application crash has different guarantees. The proposed production baseline is WAL with `FULL`, benchmarked on the reference storage and accompanied by explicit filesystem assumptions. This is a design decision and future change, not a statement that the current code provides power-loss durability. [SQLite synchronous documentation](https://www.sqlite.org/pragma.html#pragma_synchronous)

Define the v1 acknowledgement contract against that approved durability policy. Disk destruction, dishonest storage hardware and loss of the entire host are covered by the backup/RPO contract rather than a claim that a local commit prevents all loss.

Include attachment durability: a database commit must not acknowledge a reference to a file that has not been durably published under the supported platform/filesystem contract. Define file flush, atomic publication, necessary directory/metadata synchronization and database-reference ordering with Windows/Linux semantics reviewed explicitly. Test crashes at file write, publication, metadata insertion and message-claim boundaries. SQLite durability by itself does not make uploaded files durable.

Backup format versioning is separate from database schema and application version. A new manifest should identify app/schema/format, completion time, database and attachment inventory, sizes and hashes. Old supported archives must be readable by the new restore path. Newly written archives must not be advertised as readable by older binaries unless that compatibility is tested.

### 6.4 Client and downgrade policy

Support an explicit client/protocol compatibility range. Compatible stale clients revalidate state; incompatible clients receive a useful update-required state that protects drafts. A security or authorization change may require immediate session/client invalidation; provide an authenticated draft-recovery flow rather than running an obsolete client indefinitely.

Do not promise backward database migration. After a schema-changing upgrade, rollback means stopping the candidate and restoring the matching pre-upgrade database/uploads plus prior executable/image into a clean destination. Explain the loss of writes made after the recovery point. Keep old binaries, verified archives and host configuration available for the declared rollback window.

A snapshot also rolls back security and erasure decisions made later: bans, removed memberships, role revocations, blocks, password changes and deleted content can return. Clearing sessions/push devices does not resolve that. Keep restored service restricted until known post-snapshot changes are reconciled from a separately retained record or explicit owner review; unknown lost changes cannot be reconstructed from the archive. If access/credential state cannot be trusted, require reset/review of affected accounts or private areas before reopening. Publish the recovery window's limitations instead of promising that old backups automatically preserve later erasure or revocation.

## 7. Work breakdown

### 7.1 Package conventions

Every package below needs a linked issue, named implementer, reviewer, selected ADRs, test/evidence links and documented residual risks before completion. Estimates are **focused engineer-days**, include implementation and package-level tests/review/docs, and exclude external procurement waits. Dedicated final qualification is estimated separately. The estimates are preliminary and must be rebaselined after the first iteration and media spike.

The phase grouping is an organizational view, not a requirement to delay operations or accessibility work. Dependencies below determine the actual order. In particular, backup verification, reference hosts, observability, signing procurement and the media spike start early.

### 7.2 P0 — Project and evidence foundation

#### WP-01 — Project identity, rights and public-project setup

**Owner:** OWNER + REL. **Estimate:** 2–3 days. **Dependencies:** D-01, D-15; public actions require explicit owner authorization.

**Deliver:** chosen license/copyright and contribution terms; canonical repository/history baseline; maintainer access and recovery; private security intake; branch/tag/release protection design; initial release ledger; source/history/artifact review for secrets and private operational notes. Configure actual destinations before claiming they exist. Reconcile contributor guidance with the selected license.

**Done when:** the owner controls the repository/release identities, a private test security report reaches the responsible person, the source has an approved license, and the alpha.3 baseline has a stable commit identity. No database, setup token or host key is published.

#### WP-02 — Scope, architecture decisions and acceptance baseline

**Owner:** OWNER + TL + QA. **Estimate:** 2–3 days. **Dependencies:** stakeholder availability.

**Deliver:** resolve D-02–08 and provisional D-12–17; freeze requirement IDs, trust boundaries, support tiers and target workload; approved permission matrix and core journey designs; artifact/evidence conventions; inventory of current tests and their limitations. Separate the Go language/module minimum from the documented release toolchain (`go.mod` and current build docs differ).

**Done when:** every required feature maps to an owner/package/test; each unresolved decision has a deadline and affected work; the team has a reproducible baseline and does not treat historical test results as current qualification.

#### WP-03 — Reference environments and working CI

**Owner:** OPS + REL + QA. **Estimate:** 2–4 days. **Dependencies:** WP-01, D-03–05; actual host access.

**Deliver:** disposable Windows/Tailscale and Linux Docker staging installations, clean recovery destinations, separate load generator, representative devices and network routes; run the existing hosted CI for real; retain logs/versions; establish test data isolation and secret handling. Reserve signing/registry identities, three consenting pilot communities, independent reviewers and physical test sessions early. Provision the relay's reachable listener, SFU route, certificate/ports, credentials and egress budget; an HTTP proxy or overlay alone does not prove the UDP-blocked relay path.

**Done when:** both staging paths are reachable by intended test devices, the baseline runs in hosted CI, test data is disposable, and no acceptance task depends on an unnamed machine, credential or tester. This package establishes environments, not final production support.

### 7.3 P1 — Identity, migrations and privacy foundation

#### WP-04 — Migration framework and durability contract

**Owner:** BE + OPS. **Estimate:** 4–6 days. **Dependencies:** WP-02–03, D-16.

**Deliver:** staged migration fixtures and invariants; transaction-aware query interfaces; preflight/version/disk checks; source/target schema reporting; durability decision and baseline measurement; supported backup compatibility and downgrade refusal tests. Establish a manually verified pre-upgrade restore procedure before shipping any new schema; WP-23 subsequently automates/strengthens completion evidence.

**Done when:** schema-2 fixtures survive an additive rehearsal, interrupted/failed migrations leave a recognized recoverable state, IDs/search/attachments match, no new-schema database opens with an old binary, and recovery is proved on a disposable destination. Application-crash and OS/power-loss guarantees are stated separately.

#### WP-05 — Account, session and recovery completion

**Owner:** BE + FE + SEC. **Estimate:** 4–6 days. **Dependencies:** WP-04, minimal owner/operator policy in WP-06; D-06, D-10.

**Deliver:** member MFA enrollment and recovery-code lifecycle; privileged enrollment policy; active-session listing/revoke-one/revoke-all; recent-authentication gates; owner-only audited emergency credential recovery; explicit deactivation/reactivation behavior. Eliminate delegated-admin credential takeover through existing reset paths unless it is an explicit operator role outside ordinary moderation.

**Done when:** concurrent recovery-code use succeeds once, revoked sessions stop REST/socket/media access, password/MFA changes cannot race stale credentials into a new session, demotion preserves the user's MFA, and offline owner recovery is documented without exposing secrets.

#### WP-06 — Central authorization and policy engine

**Owner:** BE + TL + SEC. **Estimate:** 5–8 days. **Dependencies:** WP-02, WP-04; D-06–07.

**Deliver:** resource-aware policy evaluation and allowed-recipient queries; string/typed capability catalog; minimal explicit owner designation and nondelegable operator powers in M-A; default roles and hierarchy/delegation rules; transaction-time authorization; hidden-resource/error contract; a full endpoint/event-to-policy inventory. Owner designation uses an authenticated operator ceremony, not an automatic assumption that the oldest administrator is the owner. WP-07 adds the management/transfer UI. Prefer narrow authorized resource methods over handlers freely calling an unscoped message lookup.

**Done when:** table/property tests cover the catalog, owner/last-owner, hierarchy, malformed principals and revocation; deleting an ordinary grant never increases authority; no route or event is left with an undefined resource policy. Independent design review precedes private-feature exposure.

#### WP-07 — Roles, categories and restricted channels

**Owner:** BE + FE. **Estimate:** 5–9 days. **Dependencies:** WP-05–06, migration stage M-A/M-B; release depends on WP-08.

**Deliver:** owner designation/transfer; administrator/moderator/member presets and custom role grants; durable category ordering; public/private text and voice channels; explicit role/member access lists; announcement posting roles; “who can access this?” inspector; safe public-to-private conversion. Category moves preserve visibility.

**Done when:** legacy channels remain public with unchanged IDs/order/history, effective-access UI matches policy tests, last-owner protection holds, managers cannot escalate privileges, and private UI/API enablement remains blocked until every delivery path in WP-08 passes.

#### WP-08 — Privacy across reads, files, events, queues and media

**Owner:** BE + FE + SEC. **Estimate:** 6–10 days. **Dependencies:** WP-06–07.

**Deliver:** filtered state/navigation/search/unread/pins/context/replies/counts; current-access file/thumbnail/range delivery; scoped mentions/typing/voice state/events; dispatch-time push checks; authorization revisions, queue invalidation and media revocation; client cache/search/draft recovery handling after access loss. Preserve bounded queues and non-disclosing errors.

**Done when:** an authenticated outsider with valid private IDs sees no content or sensitive metadata through any listed surface; concurrent revocation and slow-client cases pass; reused file URLs fail after access removal; queued notifications and resumed voice cannot bypass policy. This is the mandatory G1 privacy gate.

### 7.4 P2 — Private conversations and data lifecycle

#### WP-09 — Shared conversation model and resilient client state

**Owner:** BE + FE + TL. **Estimate:** 5–8 days. **Dependencies:** WP-04, WP-06, D-08 and ADR D-11; exposed operation requires WP-08.

**Deliver:** shared conversation descriptor and message pipeline; prerequisite account-deletion/tombstone schema/policy in M-C before private conversation enablement; stable legacy channel mapping; conversation-specific read/draft/cache keys; typed navigation/deep links; resource/message revisions; immutable idempotency-intent validation and deletion-safe receipts/expiry; bounded opt-in recoverable outbox; protocol compatibility handshake and snapshot reconciliation. WP-12 completes erasure/export/retention tools rather than supplying a late prerequisite for this migration.

**Done when:** existing chat retains behavior, navigation/history/drafts survive scope changes, lost POST responses reconcile once, stale results cannot resurrect deleted content or cross accounts, and legacy links resolve through the authorized path. New controllers reduce responsibility concentration without a wholesale frontend rewrite.

The current socket backoff can reach 30 seconds. Specify bounded foreground retry, half-open detection and fast network/page-resume recovery consistent with the proposed ten-second target; do not retain a timer policy that makes the release budget impossible. Qualify from measured link restoration for active pages and from foreground resume for OS-suspended pages, not merely from when a convenient browser event fires.

##### WP-09 implementation contract and current status

[ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md) is the controlling D-11 contract. The following summary replaces the earlier draft, whose nullable-column, operation-key and pruning statements were inconsistent with that ADR.

1. **Channel mapping:** schema 6 adds one conversation per channel using the existing channel ID. `messages.conversation_id` is backfilled, preserving message IDs and rowids; rows are updated, not recreated. `channel_id` stays `NOT NULL` and the new conversation column is nullable without a foreign key at this stage. Readers use `COALESCE(conversation_id, channel_id)`. A later private-message table rebuild must preserve rowids and dependent rows, with foreign-key mode managed outside its transaction.
2. **Send identity:** receipts are unique by `(account_id, idempotency_key)`. Destination, trimmed content, sorted attachment IDs and reply target form the intent digest. Reusing a key for different intent conflicts. Legacy empty-digest receipts still check the destination before replaying a result.
3. **No resurrection:** receipts survive message deletion. After 30 days, deleted receipts compact into minimal spent-key records instead of disappearing; delayed original requests always conflict. Compaction is limited to 1,000 rows per maintenance pass, but spent-key storage can grow until account lifecycle cleanup. This limitation must appear in capacity accounting.
4. **Outbox and recovery:** persistence is optional and account-specific. Pending and recovered entries share 50 records and 64 KiB of UTF-8 text. After the 24-hour retry window, text-only recovery remains for up to 24 hours from first display. Permanent refusal ends retries and attachment/reply claims while offering explicit Copy/Dismiss recovery. It never changes the active draft or sends to another destination automatically. Malformed intent is rejected as a whole.
5. **Read state:** the channel client advances only a server-confirmed cursor and rejects out-of-order acknowledgements that would move it backward. Reconnect and access changes invalidate stale snapshots before counts or private content are restored. Broader target-latency qualification remains unmeasured.
6. **Lifecycle:** schema 7 and [ADR 0011](adr/0011-account-removal-and-retained-evidence.md) provide permanent removal with retained community history, distinct from reversible deactivation and explicit erasure. Private membership/history policy still requires ratification and implementation.

Both server and client channel slices are implemented, with unit, transaction, migration and browser evidence recorded in the [alpha.5 release record](releases/0.4.0-alpha.5.md). WP-09 is not a claim that direct/group conversation services or the proposed reconnect performance target are complete. DMs, groups, blocking and private calls remain absent. Their consent, privacy and migration contracts must precede exposure.

#### WP-10 — One-to-one direct messaging

**Owner:** BE + FE. **Estimate:** 4–7 days. **Dependencies:** WP-05, WP-08–09; public availability also requires blocking/reporting in WP-11/WP-15.

**Deliver:** unique canonical direct pairs; request/accept/reject states; bounded introduction; conversation list/open/close; ordinary text/reply/reaction/upload/search/read features using common services; generic notification events; clear operator-trust explanation.

**Done when:** simultaneous requests yield one pair; closed/unaccepted requests cannot carry attachments/calls; unrelated members and delegated admins cannot read any direct-conversation surface; hiding a DM for one user does not delete shared history.

#### WP-11 — Group conversations, consent and blocking

**Owner:** BE + FE + SEC. **Estimate:** 6–9 days. **Dependencies:** WP-10, D-07.

**Deliver:** group invitation acceptance, ten-member initial cap, name/owner/member actions, history disclosure, leave/remove/ownership transfer; block/unblock and contact-request limits; shared-group behavior; minimal membership system events; conversation access for future private calls.

**Done when:** no user is added without consent; accept/remove races resolve deterministically; removal closes old URLs/search/files/events; last-owner departure has a defined result; blocked contact cannot notify/call/invite through an alternate path. Shared-group visibility is accurately explained rather than falsely described as hidden by a block.

#### WP-12 — Identity/content lifecycle, retention and export

**Owner:** OWNER + BE + SEC. **Estimate:** 4–7 days. **Dependencies:** WP-09; D-08–09, approved deletion schema.

**Deliver:** explicit deactivate/delete/anonymize distinctions; safe tombstone identities where chosen; content-erasure workflow separate from account disable; bounded per-user export if offered; retention policy for community/DM/report data; orphan-file cleanup and quota accounting; operator export boundaries. Review every existing cascading foreign key.

**Done when:** UI wording matches actual deletion, content/attachment/search/report outcomes are tested, member export never includes another private conversation or credentials, and routine retention/retry cannot resurrect purged content. Historical restore limitations and post-snapshot security/erasure reconciliation are explicit and tested under §6.4. Any legal policy claims require owner review; this is a product contract, not a compliance certification.

### 7.5 P3 — Community conversation experience

#### WP-13 — Named threads and discussion lifecycle

**Owner:** BE + FE. **Estimate:** 6–9 days. **Dependencies:** WP-08–09, lifecycle decisions in WP-12/D-18.

**Deliver:** named thread identity, channel inheritance, explicit legacy promotion, title/activity summaries, follow/unfollow, independent read cursor, active/archive lists, reopen/lock and root tombstones; bounded cursor navigation through all authorized history; desktop side pane and mobile route.

**Done when:** a 1,000-reply fixture is fully reachable without duplication or unbounded DOM growth; promotion preserves old IDs/links; archive/lock/access loss affects send/search/push consistently; reading the thread does not clear unrelated channel unread state.

#### WP-14 — Notification preferences and activity inbox

**Owner:** BE + FE. **Estimate:** 5–8 days. **Dependencies:** WP-08, WP-10–11, WP-13.

**Deliver:** stable inbox entries for mentions, followed replies and DMs; account/conversation/device preference precedence; mute/quiet controls; default private previews; active-view suppression, tab/device deduplication and burst grouping; notification test/diagnostic states; current-access dispatch and deep-link resolution.

**Done when:** the preference/access/block matrix passes for active/background/closed PWA states; the retained deduplication-window contract passes across tabs; revoked or deleted content does not produce delayed disclosure; inbox state remains correct when push permission is denied or delivery never occurs.

#### WP-15 — Moderation, report queue and invitation controls

**Owner:** BE + FE + OWNER. **Estimate:** 5–9 days. **Dependencies:** WP-05–08, WP-11–12.

**Deliver:** scoped moderator powers, expiring timeouts, transactionally enforced slow mode, report submission/queue/assignment/resolution, reporter-selected evidence, retention and audit policy; bounded multi-use/expiry/revocation invite controls with safe entry roles. Protect reports from arbitrary DM browsing and storage abuse.

**Done when:** alternate REST/socket/action paths cannot evade restrictions; moderators cannot act above their authority; timeout expiry restores only otherwise-allowed powers; evidence survives source deletion under the approved retention contract; required security audit records are committed with their mutations. Apply a documented audit failure policy.

#### WP-16 — Composer, readable messages and member context

**Owner:** FE + BE. **Estimate:** 4–7 days. **Dependencies:** WP-06, WP-09, WP-11; coordinate with WP-13–14.

**Deliver:** one safe formatting contract for paragraphs/emphasis/code/quotes/lists/links without raw HTML; scoped stable-identity mentions; accessible touch/keyboard message actions and copy link; useful member profile with current identity fields; upload cancel/retry/progress and originating-draft preservation; deleted/edited/reply context and image description behavior.

**Done when:** malicious markup/URLs remain inert, code blocks do not trigger unintended mentions, IME/emoji/RTL/long text behave correctly, upload failure preserves text, and unread/search/export semantics match rendered message content. Adding an HTML renderer requires a security review before enabling it.

#### WP-17 — Navigation, responsive interaction and accessibility foundations

**Owner:** FE + QA. **Estimate:** 3–5 days. **Dependencies:** begins with WP-02; integrate through WP-09/13/16.

**Deliver:** consistent loading/empty/error/removed/reconnecting states, focus restoration, non-hover actions, permission-aware conversation picker, keyboard shortcuts with visible alternatives, responsive side-pane/full-page transitions and virtual-keyboard behavior. Set reusable contrast, touch target, zoom, reduced-motion and live-region patterns.

**Done when:** critical journey prototypes work by keyboard at narrow/wide layouts, no known action requires a pointer, loading history does not announce hundreds of messages, and the physical-device/a11y test checklist is ready before feature completion. Final audit remains WP-29.

### 7.6 P4 — Reliable live rooms and screen sharing

#### WP-18 — Media architecture and interoperability spike

**Owner:** MEDIA + TL + OPS. **Estimate:** 4–7 days. **Dependencies:** WP-02–03; start early while privacy implementation proceeds.

**Deliver:** compare embedded Pion extension with an optional self-hosted SFU; choose track/signaling/codec/feedback design; prove multi-publication enable/disable and late subscriber behavior; quantify likely deployment and maintenance costs. Exercise representative supported browsers and a constrained network before capture UI buildout.

**Done when:** an ADR records the chosen route, license/operational consequences and measured spike; ten participants can identify/receive the intended test publication without track confusion; negotiation/feedback feasibility is demonstrated. If the spike fails or requires a separate service, stop dependent media work and rebaseline scope/architecture/effort explicitly.

#### WP-19 — Voice reliability, private calls and network reachability

**Owner:** MEDIA + OPS + FE. **Estimate:** 5–8 days. **Dependencies:** WP-05, WP-08, WP-18; private-call integration requires WP-10–11.

**Deliver:** media connection states and redacted diagnostics; microphone loss/change and supported output-device controls; safe PTT/blur/cancel behavior; defined resume/rejoin/ICE restart; listen-only admission and joinable membership-scoped private calls without ringing; operator-configured STUN and external TURN TCP/TLS/UDP credentials; IPv6 policy and deployment recipes.

**Done when:** direct and forced-relay audio work on both deployment paths, UDP-blocked clients use the supported relay route, unsupported conditions fail usefully, mute/deafen survive recovery, and membership removal stops resumed media. Private calls cannot appear in unrelated users' voice state.

#### WP-20 — Publication/subscription and signaling redesign

**Owner:** MEDIA + BE. **Estimate:** 8–14 days. **Dependencies:** WP-08, WP-18–19.

**Deliver:** typed microphone/screen/screen-audio publications; stable IDs/generations and subscriber authorization; negotiation collision handling; track replacement, feedback/congestion behavior, cancellation, room moves and resource cleanup; bounded queues and server-enforced publication/subscription caps. Separate serverwide participant admission from per-room receiver capacity: the current shared participant constant must not turn the nine receiving slots into nineteen merely because total server capacity rises. Establish old-client compatibility or an explicit upgrade requirement.

**Done when:** repeated publication changes and joining/leaving do not misattribute media, create ghost peers or cross room boundaries; stale signals and unknown tracks are rejected; voice remains usable under screen bandwidth pressure. Tests include stop/revoke during signaling and slow consumers.

#### WP-21 — Screen-sharing experience

**Owner:** MEDIA + FE. **Estimate:** 9–16 days. **Dependencies:** WP-17, WP-20.

**Deliver:** explicit source chooser, local preview, unmistakable sharing state, app/browser stop, moderator stop, text/motion presets, viewer pin/fullscreen, mobile viewing and audio-only preference. Show optional shared audio only when actually returned/supported. Enforce one publisher/room and two/server target limits in the server as well as UI.

**Done when:** capture cancellation is harmless; 100 start/stop cycles leave no capture/peer leaks; new viewers receive active shares; server forwarding ends on stop and the normal foreground client ends associated capture under §5.9; reconnect never silently recaptures ended tracks; text is legible at the agreed preset; all supported combinations in section 11 are qualified. Camera video remains excluded from this estimate.

#### WP-22 — Media and network qualification

**Owner:** MEDIA + QA + OPS. **Estimate:** 6–10 days. **Dependencies:** WP-19–21, observability from WP-25.

**Deliver:** real-browser audio and share tests across independent networks, direct/relay/UDP-blocked paths, sleep/resume, device changes and the declared impairment profiles; two-hour target calls; room-move/moderation/publication-cycle tests; measured bitrate/RTT/loss/jitter and participant observations. Run maximum allowed combinations, not only one quiet room.

**Done when:** target audio intelligibility, share quality, join/recovery and room-isolation criteria pass on both deployments with no hidden capture; browser/device exceptions are explicit; no unbounded process/client/media growth appears. Synthetic RTP forwarding alone cannot close this package.

### 7.7 P5 — Production installation, recovery and release machinery

#### WP-23 — Trustworthy backups and recoverability

**Owner:** BE + OPS. **Estimate:** 5–8 days. **Dependencies:** WP-04, D-14; begin before private-data milestones are deployed.

**Deliver:** staged/atomic archive completion; versioned manifest/inventory/hashes; successful backup/copy identity in status; integrity-aware monitoring; isolated verification/restore rehearsal; configuration/TLS recovery procedure; independent encrypted-copy workflow using established operator tooling; failure/retention/key-loss handling.

**Done when:** arbitrary touched/corrupt ZIPs cannot satisfy backup health, interruption cannot publish a completed archive, copies match verified identity, and an off-host archive restores into clean Windows/Docker destinations with accounts/permissions/search and sampled attachment hashes intact. Check RPO/RTO against the reference dataset.

#### WP-24 — Native and Docker install/update/rollback lifecycle

**Owner:** OPS + BE + QA. **Estimate:** 6–10 days. **Dependencies:** WP-03–04, WP-23; D-03 support boundary.

**Deliver:** installer preflight/resume/failure recovery; real Windows service/UAC/tray/reboot/ACL testing; Docker image runtime and persistent-volume tests; configuration and certificate survival; prior-release upgrade and matched-data rollback; graceful stop; non-ASCII/custom paths; uninstall preserving intended data; published proxy/media/firewall recipes.

**Done when:** the lifecycle matrix in section 12 passes from clean machines/volumes, including interrupted install, failed startup, unavailable backup mount and full disk. ARM64 or additional native platforms are fully supported only after their native runtime/recovery rows pass.

#### WP-25 — Metrics, diagnostics, external checks and alerting

**Owner:** OPS + BE. **Estimate:** 4–6 days. **Dependencies:** WP-03, D-14/D-17; instrument early.

**Deliver:** redacted correlated logs; bounded metrics for HTTP/DB latency/errors, event queues/reconnects, jobs/backups/disk and media quality; host RSS/CPU/network collection separate from Go heap; protected diagnostics export; scheduled production check and independent external HTTPS/authenticated-socket checks; alert ownership/runbooks and recovery notices.

**Done when:** every critical failure has a delivered alert and useful remediation path, metrics expose no tokens/messages/private labels, external reachability is distinct from host-local health, and induced certificate/disk/backup/process/network faults are detected within agreed windows.

#### WP-26 — Capacity and reliability qualification

**Owner:** QA + OPS + BE + MEDIA. **Estimate:** 5–8 days. **Dependencies:** WP-08–22 as applicable, WP-23–25.

**Deliver:** deterministic fixtures and a separate load generator; end-to-end receipt measurements; cold/warm runs, target ramp, overload rejection, two-hour peaks and 24-hour mixed soaks on each reference deployment; resource and long-run growth analysis; capacity knobs audited across APIs/client/queues/RTC.

**Done when:** thresholds frozen before qualification pass for the declared profile, acknowledged writes/authorization invariants hold during failure injection, overload recovers without corruption, and raw results/configuration are attached. Do not raise public limits from short local benchmark results.

#### WP-27 — Unified CI, signed artifacts and source provenance

**Owner:** REL + OPS + SEC. **Estimate:** 4–7 days. **Dependencies:** WP-01, WP-03; final integration with WP-24.

**Deliver:** one candidate pipeline linking native, browser, security, Docker runtime, migration/recovery and package gates; immutable toolchain/base-image records; complete source distribution, dependency notices/SBOM and provenance; checksums/signatures; signing/notarization where required; registry/release permissions and recovery; clean consumer verification.

**Done when:** every artifact traces to one source commit and tested configuration; package contents exclude private data; final distributed bytes are verified after signing/repacking; Docker runtime cannot be skipped by a native-only release job; a second operator verifies downloads/images without private build tools.

### 7.8 P6 — Independent review and real-community beta

#### WP-28 — Security and privacy release review

**Owner:** SEC + TL; implementers remediate. **Estimate:** 8–12 days. **Dependencies:** all exposed feature paths; interim reviews start with WP-06.

**Deliver:** updated threat model, route/event/file/job inventory, adversarial authority/revocation/DM/media review, session/recovery and operator-boundary review, XSS/SSRF/CSRF/restore/path tests, resource-abuse tests and dependency findings disposition. Map relevant controls to [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0), with an explicit applicability record rather than claiming certification.

**Done when:** independent review has no unresolved critical/high exposure, all reproducible findings have regression evidence, and residual medium issues have an owner, rationale and deadline. Zero private-channel leaks in a happy-path test is not sufficient review.

#### WP-29 — Accessibility and cross-browser acceptance

**Owner:** QA + FE + qualified human testers. **Estimate:** 4–7 days. **Dependencies:** WP-17, complete features and media UI.

**Deliver:** automated Chromium/Firefox/WebKit core flows plus manual real-browser/phone/screen-reader testing; keyboard, zoom/reflow, contrast, target size, live-region, reduced-motion and viewport review; capability-specific media/push behavior. Audit complete journeys, not isolated components.

**Done when:** no critical journey is inaccessible, support-matrix rows have actual results, and claims accurately distinguish automated checks from the scoped human audit. Track any live-media caption/transcript limitation explicitly; do not claim blanket WCAG conformance from axe results alone.

#### WP-30 — Beta pilots and operational rehearsals

**Owner:** OWNER + OPS + QA. **Estimate:** 8–12 engineering days plus required observation time. **Dependencies:** WP-22–29, initial operator documentation from WP-31.

**Deliver:** at least three consenting small communities, including both required deployment paths, over 14 consecutive observation days; opt-in/redacted defect reports; installation by a non-author; actual restore/alert/update drills; cross-device onboarding/media/push use; triage and measured fixes.

**Done when:** pilot success criteria in section 14 pass, no unresolved severe defect remains, participants can complete core journeys without author intervention, and failures have owners/regressions. A major security/data/media fix resets its affected observation gate.

#### WP-31 — User, operator, contributor and support documentation

**Owner:** OWNER + REL + OPS + FE. **Estimate:** 4–7 days. **Dependencies:** contracts stabilized; installation/release work available for rehearsal.

**Deliver:** newcomer quickstart, member/privacy/moderation guides, permissions model, support matrix, network/certificate/TURN recipes, backup/recovery/upgrade/rollback runbooks, diagnostics/incident guides, contributor setup, license/security/governance policy and known limitations. Reconcile all ports/toolchain/version statements.

**Done when:** a non-author completes fresh install and recovery from the docs, another contributor builds/tests from a clean checkout, links/configuration examples match the candidate, and every public operational promise has a named owner. Pilot feedback is incorporated before WP-32.

### 7.9 P7 — Stable release and maintenance handoff

#### WP-32 — Candidate freeze and final go/no-go

**Owner:** REL + QA + TL + OWNER. **Estimate:** 3–6 engineering days plus the final observation window. **Dependencies:** WP-28–31 and all applicable requirements/gates.

**Deliver:** freeze source/dependencies/schema/protocol/docs; create final signed distribution artifacts; run final artifact-specific checks and qualification resets; close release ledger; verify rollback materials, capacity/support statements and security contact; complete seven consecutive days of final-candidate observation.

**Done when:** section 17 is signed against immutable artifact identities, no open Severity 0/1 defect exists, final checks use distributed bytes, and any post-freeze change has rerun its affected gates. Release approval is a recorded decision, not an automatic tag-number change.

#### WP-33 — Publish, roll out and transfer maintenance ownership

**Owner:** OWNER + REL + OPS. **Estimate:** 3–5 engineering days; observation continues after launch. **Dependencies:** WP-32 and explicit publication approval.

**Deliver:** publish approved source/packages/images/docs and compatibility/known-issue notes; staged upgrades from pilot to early adopters; monitored launch; incident/rollback readiness; patch/release ownership and supported-version policy; 7-day and 30-day follow-up reviews.

**Done when:** clean downloads verify and install, early adopter upgrades meet the rollout criteria, alerts reach responsible operators, the release can be reproduced/traced, and maintenance responsibilities are accepted. Future scheduled reviews require explicit scheduling when implementation begins; this document creates no automation.

## 8. Milestones and dependency order

### 8.1 Gate map

| Gate | Outcome | Packages/evidence required | Who approves |
| --- | --- | --- | --- |
| G0 — Baseline approved | Scope, trust, ownership, environments and provisional budgets are explicit | WP-01–03; decision register with no unowned blocking decision | OWNER + TL + OPS |
| G1 — Privacy foundation | Restricted community content has one complete authorization boundary | WP-04–08; verified upgrade recovery; privacy tests across every delivery surface; interim SEC review | TL + SEC + QA |
| G2 — Private messaging integration | 1:1/group membership, blocking, lifecycle and state behavior work internally | WP-09–12 plus shared authorization regressions | TL + QA; no broad rollout yet |
| G3 — Community feature complete | Threads, notification control, moderation and core UX are complete | WP-13–17; R-02–08 journey evidence; minimum reporting/moderation before outside private-message pilots | OWNER + QA |
| G4 — Media feature complete | Reliable voice/private calls and scoped desktop screen sharing meet their contract | WP-18–22; cross-browser/network and capture-lifecycle evidence | MEDIA + QA + OPS |
| G5 — Operationally recoverable | Required hosts, recovery, metrics, capacity and artifacts are qualified | WP-23–27; section 12 matrix and target-load results | OPS + REL + QA |
| G6 — Beta accepted | Independent reviews and real communities validate the full product | WP-28–31; 14-day beta, resolved findings and newcomer rehearsals | OWNER + SEC + QA + OPS |
| G7 — Stable candidate accepted | Final distributed artifacts and compatibility are qualified | WP-32; seven-day final observation; complete release ledger | OWNER + TL + SEC + OPS + REL |
| G8 — v1.0.0 released | Approved publication, staged rollout and maintenance ownership | WP-33; explicit release authorization, verified downloads and support handoff | OWNER + REL |

G3, G4 and much of G5 can progress in parallel after their prerequisites. G6 waits for all three. Gates protect release claims, not just code merging: an unfinished feature may exist behind a closed boundary, but it is not available to users or advertised as supported.

### 8.2 Dependency streams

| Stream | Order / join points |
| --- | --- |
| Privacy/data | Decisions → migration discipline → policy engine → roles/access → all scoped delivery paths → private conversation model → DM/group/lifecycle → independent security review |
| Community UX | Shared conversation contract → named threads and message UX → inbox/preferences and moderation → full accessibility/browser acceptance |
| Media | Early spike → approved architecture/network contract → authorized private/community voice → track/signaling redesign → sharing UI → physical network qualification |
| Operations | Early reference hosts and instrumentation → backup integrity/upgrade contract → real lifecycle tests → target-load soaks → signed/verifiable candidate |
| Final release | Feature completion + operations → independent review → 14-day beta → final artifact qualification + seven-day observation → approved publication |

The likely critical path is authorization/data design, then private-conversation integration, then media qualification and operational recovery, followed by independent review and observation. It is not safe to assume that adding more frontend work shortens that path.

### 8.3 Work that can proceed safely in parallel

- Start media feasibility, signing/account procurement, host setup, diagnostics instrumentation and accessibility design during P0/P1.
- Design DM/thread navigation against reviewed contracts while backend access enforcement is built; do not expose fake/private placeholder endpoints publicly.
- Develop backup completion/integrity improvements independently of DM UI, then rerun recovery when each schema stage lands.
- Run security design reviews before implementation and a separate final review after features are complete.
- Draft operator/member documentation alongside working increments; perform the non-author rehearsal before beta.

Serialize migrations with overlapping tables, authorization semantic changes, protocol-breaking media changes and final release publication. Assign one owner to each migration and one owner to shared API/protocol contracts to avoid incompatible parallel work.

### 8.4 Release naming during development

Use development versions as checkpoints, not deadlines. A possible sequence is 0.5 for the privacy foundation, 0.6 for private conversations, 0.7 for community UX and 0.8 for the media/operations convergence, followed by beta/candidate channels. Actual version assignment follows coherent deliverables; skip or add versions rather than bundling incomplete work to match this illustrative numbering.

Stable publication requires a final artifact verification pass after all version stamping, signing, notarization or repacking. Do not promote an RC binary while pretending it reports a different version. Either qualify the final stable-version artifacts privately before publishing them, or rebuild from the frozen source and rerun every affected artifact gate. Source-identical code does not make byte-level signature/checksum evidence interchangeable.

## 9. Effort, staffing and resource plan

### 9.1 Estimation basis

These estimates cover the recommended scope, including screen sharing and excluding camera video. They are engineering judgment, not measured project velocity or a promised schedule. One engineer-day means roughly six focused hours of implementation, review, test or documentation work. Context switching, owner decisions, procurement and observation windows reduce calendar throughput.

| Phase | Packages | Base effort, engineer-days | Confidence |
| --- | --- | --- | --- |
| P0 Foundation | WP-01–03 | 6–10 | Medium; access/rights may delay calendar time |
| P1 Identity/privacy | WP-04–08 | 24–39 | Medium-low; authorization breadth and migrations |
| P2 Private conversations | WP-09–12 | 19–31 | Medium-low; lifecycle/history decisions |
| P3 Community experience | WP-13–17 | 23–38 | Medium; UX/a11y and state interactions |
| P4 Media | WP-18–22 | 32–55 | Low until spike and physical network tests |
| P5 Production machinery | WP-23–27 | 24–39 | Medium-low; host/signing availability |
| P6 Review/beta/docs | WP-28–31 | 24–38 | Medium-low; findings can add work |
| P7 Release/handoff | WP-32–33 | 6–11 | Medium after previous gates |
| **Total base effort** | **33 packages** | **158–261** | **Rebaseline after G0 and WP-18** |

Add a visible **25% planning reserve** for integration defects, review findings and unknowns: approximately **198–327 engineer-days** in total. Reserve is not permission to absorb new features. It is separate from external waiting time and does not excuse missing gates.

### 9.2 Calendar scenarios

| Team scenario | Capacity assumption | Preliminary calendar implication |
| --- | --- | --- |
| One primary developer with AI assistance | Four focused engineering days/week after coordination/support overhead | About 50–82 weeks of engineering capacity including reserve; external waits can extend it |
| Two consistently available developers | About seven combined focused days/week after review/coordination | About 29–47 weeks of capacity; critical-path and observation constraints still apply |
| Three complementary developers | About ten combined focused days/week, including backend, frontend and media/ops skills | About 20–33 weeks of capacity; a practical plan must still allow serialized integration and at least the defined beta/RC windows |

These are capacity calculations, not delivery dates. The 14-day beta and seven-day final observation cannot be compressed by generating code faster. Some waiting can overlap other work; blocked hosts/signing/owner decisions cannot be assumed to overlap harmlessly. After two completed iterations, replace these estimates with measured throughput, actual defect/review load and the accepted support matrix.

Independent security/accessibility/media expertise and physical testers are required even in the one-developer scenario. Their effort is represented in the review/qualification packages; obtaining their availability is an additional scheduling dependency.

### 9.3 Scope alternatives

| Change | Planning impact |
| --- | --- |
| Remove screen sharing from v1 | Potentially saves roughly 24–42 engineer-days across the media spike/redesign/sharing and screen-specific qualification; reliable voice/network work remains. Re-estimate dependencies rather than subtracting blindly. |
| Add camera video to v1 | Reserve another provisional 20–40 engineer-days for capture/subscription/layout/adaptation and added qualification; re-estimate after the media spike. |
| Require full inherited permission overrides | Add precedence/hierarchy UI, migration and matrix complexity; estimate only after an explicit policy ADR. |
| Add fully supported ARM64 or native Mac/Linux distributions | Add actual machines, lifecycle/media/recovery evidence and signing obligations; a cross-build is insufficient. |
| Require federation, E2EE, native mobile or active-active operation | Separate program-level scope/design estimate; not a small addition to this plan. |

### 9.4 People, infrastructure and budget inputs

Obtain a Windows x64 test host with service/UAC access; a Linux Docker host; separate clean recovery destinations; a separate load generator; storage for deterministic fixtures and independent backups; two independent real networks; an external TURN test route; representative desktop browsers and physical Android/iOS devices; and access to screen-reader testers. Add native ARM64/Mac test hardware only for platforms selected as fully supported.

Budget lines to quote before implementation: engineering/review time, signing/notarization identity and account costs, domain/certificate operations if applicable, registry/build-runner usage, staging and TURN egress, backup storage/retention, external security/accessibility review and device availability. This plan does not invent current prices or assume free egress. Use the work-package day totals with actual agreed rates and vendor quotes when producing a cash budget.

## 10. Capacity and performance qualification

### 10.1 Proposed supported v1 profile

The numbers below are targets to test, not promises of current capacity. Retain present enforced limits until the relevant implementation and qualification pass. Freeze final numbers before the qualification run; if they fail, fix the problem or openly approve a smaller support profile and rerun it.

| Dimension | Current boundary | Proposed v1 qualification target |
| --- | --- | --- |
| Registered accounts | 100 | 250 per installation |
| Concurrent active people | Historical local test: 20 clients | 50 people, with activity across community/private contexts |
| WebSocket connections | 100 total; five/account | Retain 100 total and five/account, with explicit admission errors; qualify 50 people using two connections each |
| Community channels | 100 | Retain 100; DMs and named threads use separately bounded/admitted records rather than consuming this community-channel quota |
| Group DM membership | Absent | Ten accepted members/group |
| Media participants | Ten voice participants serverwide | Twenty serverwide, no more than ten in one room/call including listen-only screen viewers; enforcement spans community and private rooms |
| Screen publishers | Absent | One/room, two/server, with per-subscriber admission/bandwidth limits |
| Screen viewers/bitrate | Absent | Viewers consume room seats; ≤9 remote viewers/share; initial video ceiling 2.5 Mbit/s/publisher; optional screen audio ≤64 kbit/s/publisher |
| Message/upload limits | 4,000 characters; five files; 12 MiB/file | Retain initially; no v1 size increase without separate storage/memory/path validation |
| History rendering | 250-message windows | Bounded windows/cache with complete cursor navigation; no full-history DOM rendering |
| Qualified retained dataset | No production-sized promise | One million messages and 20 GiB attachments in the target fixture; explicit storage quota and disk guard |

Specify finite creation/admission policies for DMs, groups, threads, notifications, invitations, reports and staged uploads before beta. Current outbox default: 50 pending/recovery records and 64 KiB of UTF-8 text per device/account. Other proposed defaults to ratify: at most five pending group invitations per target account from one inviter; bounded retry cooldown on rejected direct requests; account/server byte quotas and a configured disk-free reserve. Exact abuse/rate values must be tested against normal activity and tuned in WP-15/WP-26, not scattered into independent unexplained constants.

### 10.2 Reference machines and datasets

Use a proposed initial reference allocation of **four dedicated CPU cores/vCPUs, 8 GiB RAM, local SSD storage with at least 80 GiB free at test start, and a measured 100 Mbit/s symmetric link** for each server deployment. This is a qualification environment, not a declared minimum system requirement. Record actual CPU model, hypervisor, disk/filesystem, OS/build, Docker/Compose versions, network path and competing host load. Use a separate generator so its memory/CPU is not counted as server use.

The storage allocation must also satisfy the retention/staging formula in section 12; 80 GiB free is only the starting reference condition. Do not assume that a 20 GiB attachment corpus compresses materially. Size separate recovery and independent-backup destinations before testing.

Define and version three deterministic datasets:

- **F-S (small):** 20 accounts, 20 channels, 10,000 messages, 1 GiB attachments; includes representative roles, restricted channels, DMs and threads.
- **F-T (target):** 250 accounts, 100 community channels, one million messages, 20 GiB attachments, mixed direct/group conversations, reports, mentions and skewed thread sizes.
- **F-X (stress):** twice the target retained message/attachment volume on adequate test storage, plus deliberate attempts beyond connection/media/creation limits. This establishes rejection/recovery behavior, not a supported double-capacity claim.

Include realistic long/code/Unicode text, attachment size/type distribution, one 1,000-reply thread, deep pagination, inactive/banned/deactivated accounts, private-access changes and cold/warm search. Store fixture definitions and seeds without real user content.

### 10.3 Provisional release budgets

Approve and freeze these budgets after the baseline/spike and before final qualification. A test result must include its sample count, healthy/fault interval classification and measurement points.

| Measure | Proposed budget / definition |
| --- | --- |
| Healthy message delivery | p95 ≤500 ms and p99 ≤1 s from sender submit to authorized recipient render on the reference LAN; record API commit and UI arrival separately |
| Ordinary WAN delivery | p95 ≤observed RTT +500 ms on the defined non-impaired profile; do not hide relay/queue/render cost in a server-only number |
| Target search | p95 ≤1 s, p99 ≤2 s for the agreed query mix on F-T; cold/warm reported separately |
| Cached conversation switch | p95 ≤150 ms to usable cached content; fresh history p95 ≤1 s on reference LAN |
| API mutation failure | <0.1% unexpected failures in healthy workload; deliberate overload/auth denials and injected outages reported separately |
| Correctness | Zero lost acknowledged logical messages within the approved durability/fault contract; zero duplicate logical sends; zero unauthorized delivery |
| Reconnect | Healthy-network socket/state recovery p95 ≤10 s after connectivity returns; no stale authorization or duplicate participants |
| Media join/recovery | Proposed p95 join ≤8 s direct / ≤12 s relay after human permission prompts resolve; rejoin ≤15 s from actual link restoration for foreground runnable clients with available permission; suspended clients measured from foreground resume |
| Peak server CPU/RSS | Initial budget: mean ≤65% total allocated CPU during target steady load, one-minute p95 ≤85%; process RSS ≤2 GiB; freeze or explicitly revise before qualification |
| Resource growth | After warmup and repeated equivalent cycles, no monotonic peer/goroutine/handle/queue growth; end-of-soak idle RSS within 15% of comparable warmed idle baseline, with allocator behavior documented |
| Network headroom | Sustained egress below 70% of the reference link during supported maximum media combinations; measure direct SFU and relay traffic separately |
| Recovery | RPO ≤24 h and RTO ≤60 min for F-T from the declared recovery event, including operator response, fetching/decrypting independent backup, security reconciliation and verification; measure total outage separately |
| Monitoring | Host checks every 15 min; external outage checks every minute with bounded retries; alert delivery ≤5 min for sustained external unavailability and ≤20 min for periodic host-check failure |

These are engineering acceptance proposals, not an uptime SLA for arbitrary self-hosted networks. Measure browser usability separately on the selected low-end phone. Track stalls, interaction latency, memory after 100 conversation switches, battery/thermal observations and virtual-keyboard behavior. Define device-specific pass limits at G0 rather than claiming that a simulated viewport measures them.

With two 2.5 Mbit/s shares and nine remote viewers each, video forwarding alone is approximately 45 Mbit/s. Twenty simultaneous 24 kbit/s microphone publishers each forwarded to nine peers add about 4.32 Mbit/s; two 64 kbit/s screen-audio sources forwarded nine times add about 1.15 Mbit/s. These illustrative payload totals exclude protocol overhead, retransmission and relay topology. Measure actual NIC/SFU/TURN ingress and egress separately, especially when relay and app share a host; if the 70% link budget fails, reduce the qualified media profile or increase the documented link requirement before release. Do not label this arithmetic a load-test result.

Use verified clock synchronization with recorded error bounds for cross-machine latency, or a harness that measures round trips/receipt reports on one monotonic clock and discloses the added reporting overhead. Raw unsynchronized browser timestamps are not comparable measurements.

### 10.4 Load and fault program

1. Establish 30-minute small/target baselines on each deployment, including cold and warm storage/cache behavior.
2. Ramp 10 → 25 → 50 active people and 20 → 50 → 100 sockets. Use realistic idle/activity intervals, not every user sending continuously at a rate no community uses.
3. Run a two-hour peak with ten aggregate message creates/second, a 30-second burst to 30/second within per-account limits, two searches/second, five reaction/edit/delete operations/second, periodic near-limit uploads, and the declared media maximum. Final workload rates are approved before qualification.
4. Run a 24-hour mixed soak on **each** required deployment, with real backups, retention, preview jobs, private conversation traffic, membership revocations and reconnect waves. Sample chat delivery throughout; include long physical-media sessions rather than equating fake RTP with perceived audio.
5. Inject process termination, VM/OS reset where safe, full disk, lost backup mount, expired relay credentials, broken certificate route, ten-second network loss, slow clients and restore from corrupt/truncated archives. Use disposable data. Verify acknowledged-message/file invariants at each boundary.
6. Attempt admission above limits and then remove load. The server must reject predictably and recover without an operator database repair or unbounded queue growth.

Media network profiles must include clean LAN, normal WAN, 150 ms RTT/30 ms jitter/2% random loss, restricted bandwidth, and UDP completely blocked with the supported TCP/TLS relay. Verify actual impairment parameters. Under the impairment profile, require intelligible two-way conversation with no stuck mute/capture and documented share adaptation. Record listening observations as well as packet statistics; do not substitute a single synthetic score for usability.

Freeze repeatable media-quality fixtures in WP-18: a defined spoken-word/number corpus with a listener transcription rubric, and a screen chart with specified source resolution, fonts, fine text and frame counter. Proposed thresholds to ratify are ≥95% spoken-word recognition on clean routes and ≥90% on the impairment profile, with no unexplained silence longer than one second clean/two seconds impaired during continuous source speech. For the text-share preset, require ≥95% correct transcription of the frozen chart at the declared desktop viewer size; verify phone pan/zoom rather than promising that all desktop text is readable when shrunk to a whole phone screen. Record first-frame delay, longest frozen frame and adaptation behavior against limits approved at the spike. Use consenting test material and retain no real private conversation recordings.

## 11. Verification and acceptance matrix

### 11.1 Test layers

| Layer | Purpose | Evidence / frequency |
| --- | --- | --- |
| Policy/unit/property tests | Deterministic access, role, state, cursor, retry and lifecycle invariants | Every affected change; include negative and adversarial combinations |
| API/database integration | Real migrations, transactions, search, file access, sessions and idempotency | Relevant packages per change; full required suite in candidate CI |
| WebSocket/RTC integration | Real sockets/peers, scoped delivery, revocation, signaling and cleanup | Changes to policy/realtime/media plus candidate CI |
| Browser automation | Complete auth/chat/private/thread/moderation/update flows with accessibility scans | Chromium/Firefox/WebKit core suite; capability-specific media smoke |
| Physical acceptance | Actual browser/device capture/audio, push, PWA, keyboard and screen readers | At feature completion and exact candidate qualification |
| Host lifecycle/recovery | Real installed services/containers, storage, migration, restart, restore and rollback | Both required deployments for every affected candidate |
| Performance/fault tests | Capacity, long-run resource behavior and recovery under specified failures | Frozen reference workload before beta and after material changes |
| Independent review | Challenge design assumptions and inspect adversarial/privacy/recovery behavior | Early authorization/media reviews plus final security/a11y review |

Do not write tests that merely repeat implementation details. Test externally observable contracts and failure behavior. A changed authorization, schema, media or installer path requires its corresponding regression gate even if an unrelated local suite stays green.

### 11.2 Mandatory scenario catalog

| Test ID | Scenario and pass condition | Packages / requirements |
| --- | --- | --- |
| T-AUTH-01 | Authorized member A and outsider B know the same private IDs. Exercise state/list/history/context/replies/pins/counts/search/original/thumbnail/range/typing/events/push/media. B gets no content or sensitive metadata. Repeat for private channel, inherited thread, 1:1 DM and group. | WP-06–11; R-03–04 |
| T-AUTH-02 | Remove access during queued socket events, file streaming, preview/push work and media resume. New admissions after the committed revision fail; active delivery is canceled according to §5.4; client scopes clear without restoring stale snapshots. | WP-08, WP-14, WP-20; INV-01/04 |
| T-AUTH-03 | Role manager attempts higher-role assignment, undelegable grant, peer moderation, implicit ownership takeover and last-owner deletion. All denied; permitted lower-role actions succeed and audit. | WP-05–07, WP-15 |
| T-AUTH-04 | Delegated admin attempts full backup/export and chosen-password takeover of another account, then tries unrelated DM APIs. Denied. Explicit owner/operator workflow requires recent strong auth/audit and accurately discloses operator trust. | WP-05–08, WP-23; D-07 |
| T-IDENT-01 | Concurrent recovery-code use, reset/login races, MFA enable/disable, session revoke-one/all and demotion. Exactly one recovery use succeeds; stale sessions cannot persist over REST/socket/media. | WP-05 |
| T-DM-01 | Concurrent A↔B requests, rejection cooldown, accept/reject/block races and close/reopen. One canonical pair; no pre-acceptance attachment/call; no alternate path through message context/files. | WP-10–11 |
| T-DM-02 | Block an accepted pair during queued sends/calls. New sends/edits/attachment claims/reaction additions/calls are rejected; history and permitted deletion/report controls follow the explicit policy. No replay after unblock. | WP-11, WP-14 |
| T-GROUP-01 | Invite/accept/remove/leave/transfer races and ten-member boundary. No unconsenting member, no orphan owner; removed member loses all server access; history disclosure matches actual results. | WP-11–12 |
| T-CHAT-01 | 1,000 logical sends with duplicate requests, delayed/reordered events and dropped POST responses. One stored logical result/send; changed intent under a reused key conflicts. Delete/purge a committed lost-ACK message, then retry within the horizon: no recreation, attachment claim or new notification. Expired operations follow the explicit expiry policy. | WP-09–10; INV-02 |
| T-CHAT-02 | Navigate between scopes or accounts during upload/history fetch/retry; remove destination while typing; reload an opted-in outbox. Text is recoverable only for its account, no wrong-destination attachment, no restored deleted server content. | WP-09, WP-16–17 |
| T-READ-01 | Two devices, one background tab, a history window and a followed thread. Cursors never move backward; unseen or unrelated content stays unread; removed access does not leave private counts. | WP-09, WP-13–14 |
| T-THREAD-01 | Promote legacy discussion; navigate 1,000 replies; archive/reopen/lock/delete root; apply retention. IDs and links survive, content appears once, lock/access rules hold, and full authorized history is reachable with bounded memory. | WP-12–13 |
| T-NOTIFY-01 | Mention/reply/DM across active tab, background tab and closed PWA; mute/block/access removal before dispatch. Preference matrix and private previews hold; no notification for an ineligible recipient. | WP-14 |
| T-NOTIFY-02 | Duplicate events and simultaneous tab/service-worker delivery within the retained 24-hour store. App issues one OS notice/event/device; reset/reinstall behavior is disclosed; inbox remains authoritative. | WP-14 |
| T-MOD-01 | Timeout/slow mode/invite/report abuse over every mutation path. Scope and hierarchy hold; evidence is limited to reporter-authorized selections; audit/retention survives source deletion and failure. | WP-15 |
| T-CONTENT-01 | Malicious HTML/URLs/code/Unicode/filenames, DNS/redirect/preview attacks and large/malformed inputs. No active markup or private-network fetch bypass; bounded resources and useful errors. | WP-08, WP-16, WP-28 |
| T-MEDIA-01 | Ten room participants, including listen-only users, and twenty serverwide across rooms; two-hour real call. No room leakage, stuck capture, duplicate peer, unbounded resource growth or mute/deafen regression. | WP-19–22 |
| T-MEDIA-02 | 100 share start/stop cycles, late viewers, browser cancellation, device transfer and room movement. Publication ownership stays correct; move ends old share/audio; new capture is explicit; caps count viewers. | WP-20–22 |
| T-MEDIA-03 | Screen-audio/microphone/deafen controls and member/moderator revocation in community/private rooms. Each affects only its declared tracks/authority; unrelated moderators cannot control private calls. | WP-08, WP-19–22 |
| T-NET-01 | Direct UDP, forced relay, restrictive NAT, UDP blocked, selected IPv6 policy, ten-second loss and defined impairment profiles. Useful join/failure/recovery states; media and chat meet the approved profile. | WP-19, WP-22 |
| T-DATA-01 | Upgrade every supported fixture through M-A–M-F with interruption/full disk/retry. IDs/counts/FKs/FTS/access/file hashes match invariants; failures are recoverable; no automatic preflight migration. | WP-04, WP-09, WP-12–13 |
| T-DATA-02 | Process and controlled OS/VM failure at attachment publication, DB commit and claim boundaries. Approved durability contract holds; no acknowledged reference to a missing durable file. State the storage assumptions. | WP-04, WP-09, WP-23 |
| T-BACKUP-01 | Touched stale ZIP, corrupt/truncated archive, incomplete completion marker, wrong copy, missing attachment and unavailable key/mount. None is reported as a verified independent recovery point. | WP-23, WP-25 |
| T-RESTORE-01 | Restore F-T from independent backup onto clean Windows and fresh Docker volume using only documented materials. RPO/RTO, permissions, search and sampled hashes pass; sessions/push devices are invalidated. Fixture includes post-snapshot bans, role/member removals, password changes, blocks and erasure; reconcile before cutover and disclose unreconstructable changes. | WP-23–24, WP-30–31 |
| T-INSTALL-01 | Fresh install, cancellation, interrupted/resumed install, reboot, upgrade, failed-start rollback, uninstall/reinstall on each supported host. Exact intended data/configuration survive; privileges and service identity are correct. | WP-24 |
| T-PWA-01 | Two builds with active voice, pending send, upload and channel/thread/DM drafts. Update blocks correctly, then preserves authorized work; offline startup contains no private cached API/upload data. Physical push/background/resume checked separately. | WP-09, WP-14, WP-17, WP-29 |
| T-A11Y-01 | Critical journeys by keyboard and selected screen readers, narrow/zoomed/reduced-motion/forced-color layouts and physical virtual keyboards. No inaccessible action or lost/trapped focus. | WP-17, WP-29 |
| T-SOAK-01 | Frozen mixed load and failure program on each required host, then pilot/RC observation. Latency/resource/correctness budgets pass and overload recovers. | WP-22, WP-25–26, WP-30, WP-32 |
| T-RELEASE-01 | Obtain actual published-candidate source/package/image on a clean consumer machine, verify provenance/signature/digest, install, restore and exercise upgrade. Hosted CI evidence corresponds to those bytes. | WP-27, WP-31–33 |

### 11.3 Browser and physical-device matrix

Support policy: select the current and previous supported browser release at candidate freeze and record exact versions. Test real supported engines rather than assuming that Playwright's packaged WebKit is identical to shipping Safari. Additional platforms are promoted only after evidence, not by changing a README label.

| Client | Required core flows | Media and device qualification |
| --- | --- | --- |
| Windows Chrome and Edge | Complete journeys, keyboard, selected screen reader and update behavior | Publish/view shares, audio/listen-only, device transfer, direct and relay; output selection only where supported |
| Desktop Firefox on selected Windows/Linux reference client | Core private/thread/admin flows, keyboard | Voice and desktop sharing interoperability; useful capability fallbacks |
| macOS Safari and Chrome clients | Core flows, VoiceOver on Safari | Receive/voice; qualify screen publishing explicitly by OS/browser/source; no universal screen-audio promise |
| Physical Android Chrome: low-end and mainstream phone | Chat/DM/thread/search, uploads, keyboard, PWA, account switching and supported push | Voice/listen-only, shared-screen viewing, background/resume, network change and thermal observations |
| Physical iPhone/iPad Safari and Home Screen app | Core flows, VoiceOver, safe areas/rotation, login/update; installed-app push where supported | Voice and sharing reception, permission denial/recovery and background behavior; no promised mobile screen capture |
| Automated Chromium/Firefox/WebKit | Core journeys, negative authorization UI, accessibility scans and layout regressions | Fake-device/media smoke supplements physical evidence only |

Run at least one Windows-hosted and one Docker-hosted desktop-to-phone call across independent networks. Include denials, expired permissions, browser updates, locked screen, Bluetooth/headset changes and return from background. Record unavailable platform features as capability limits with a tested fallback.

### 11.4 Accessibility claim boundary

Use [WCAG 2.2](https://www.w3.org/TR/WCAG22/) as the audit target for application flows. Combine automated checks with keyboard/screen-reader review, contrast and focus inspection, text spacing/zoom/reflow, reduced motion and error recovery. Use a 44×44 CSS-pixel touch-target design goal where practical; distinguish that product target from the standard's actual criteria.

Live user media raises caption/alternative requirements that automated UI scans cannot settle. Record the audit scope, applicable criteria, exclusions and unmet needs with qualified review. If the product lacks required alternatives, publish that limitation and avoid a blanket conformance claim. Text chat remains available alongside live rooms, but its existence alone does not prove live-media conformance.

## 12. Deployment, recovery and operations

### 12.1 Support levels

| Level | Meaning | Proposed assignment |
| --- | --- | --- |
| Fully supported | Native runtime, lifecycle, recovery, media, device and security evidence; named maintenance owner | Windows x64 native with Tailscale access, Linux amd64 Docker |
| Candidate for promotion | Build plus scheduled native-runtime qualification; no production-support claim until passed | Linux ARM64 Docker and selected additional native Linux/macOS targets |
| Build/community supported | Source/cross-build available; users receive explicit runtime/support limits | Remaining generated architectures until evidence and ownership exist |

Owner selects actual OS edition/build and Linux distribution/runtime at G0; confirm vendor servicing status and pin versions for the release ledger. Client-browser support is separate from server-host support: a Mac browser can be a supported client even while the native Mac server package remains below the fully supported tier.

### 12.2 Both-deployment checklist

Every cell below requires a dated result for the candidate's hash/digest. No cell is passed by this plan.

| Area | Windows/Tailscale proof | Linux Docker proof |
| --- | --- | --- |
| Fresh setup | UAC accept/deny; LocalService identity; private ACLs; custom/non-ASCII paths; first-owner recovery | Non-root UID/GID, volume ownership, read-only root/tmpfs, first setup and proper secret handling |
| Service lifecycle | Stop/start/reboot and promised pre-login service availability; tray after sign-in and Explorer restart | Graceful stop, container recreate, host reboot/restart policy and signal handling |
| Origin/HTTPS | Exact canonical URL; trusted device certificate; Tailscale access rules; a second allowed device; denied unauthorized network access | Proxy trust, exact public origin, private backend binding, no unintended plaintext/public diagnostics |
| Media | Separate real networks; direct/relay and route changes; host firewall; published media IP | Same plus UDP port mapping/container network traversal and external relay route |
| Certificates | Renewal, local CA trust/repair where used, protected root/private keys and expired-cert behavior | Certificate persistence across recreation, renewal ownership and proxy/backend trust |
| Data/storage | Message/upload/search persistence, locks, disk exhaustion and ACL regression | Volume persistence, unavailable/full volume or copy mount, correct UID permissions |
| Backup/restore | Verified independent archive and host-key/config materials restored onto clean destination | Same into a fresh volume and clean container; old volume retained for rollback |
| Upgrade/downgrade | Prior supported executable/data → candidate; failed replacement/start; newer-schema refusal | Prior immutable image/volume → candidate; explicit schema migration and digest rollback |
| Rollback | Matching old executable plus verified pre-upgrade data; declared post-backup write loss | Matching image digest plus restored old-schema volume; avoid reusing the upgraded live volume |
| Devices | Physical desktop/phone auth, private flows, voice/share viewing, push and PWA | Repeat against Docker deployment, including independent network path |
| Monitoring | Delivered host and external alert drills; useful redacted diagnostics | Same with container/volume/network metrics and verified relay health |
| Reliability | Approved workload/soak budgets and process/host failure recovery | Same on actual Docker reference hardware |

### 12.3 Backup and retention policy proposal

Use a **12-hour backup-and-independent-copy cadence** to support an RPO target of at most 24 hours. Measure age from the latest independently recoverable completed snapshot, not from job start, an archive's touched modification time or a same-host copy. Proposed alerts: warning at 18 hours, critical at 24 hours, plus immediate job/copy failure notice. Use bounded retries outside the regular schedule so one transient failure does not defer recovery protection for another entire cycle.

Approve local/off-host retention and encryption/key custody in D-14. A reasonable starting policy to cost and test is two recent complete local archives plus independently retained recent/daily/weekly copies selected by the operator. Never delete the last verified recovery point before a successor is complete and independently copied. Unencrypted application archives remain sensitive even if their transport/storage destination encrypts them; access to plaintext staging must be restricted.

Calculate storage rather than assuming the reference free-space number covers retention:

**Required usable storage = live database/uploads + retained local full archives + next archive under construction + database snapshot/WAL allowance + restore/decryption staging if colocated + operating reserve.**

Size the independent destination separately from actual archive sizes and retention count. Seven full archives of 20 GiB incompressible uploads already consume roughly 140 GiB before databases or staging. The backup process must report that a schedule cannot fit, rather than failing repeatedly or reclaiming the only verified copy. Do not restore into the live directory to save space.

### 12.4 Recovery drill

1. Choose a completed independent recovery point and record its snapshot time, manifest/hash, format/schema/app compatibility and key location.
2. Start the RTO clock at the declared recovery event, before the operator begins work; include operator response delay. For a fault exercise, record the actual outage start and detection/declaration timestamps too. Total outage is reported separately and cannot be made shorter by delaying the declaration. State any prerequisite spare-host/storage availability.
3. Provision a clean destination with the declared storage/network prerequisites. Retrieve/decrypt/verify the archive and separately protected configuration/TLS material.
4. Restore using the candidate's supported path, with no overwrite of live data. Start in a restricted validation mode before serving normal users.
5. Compare users/roles/membership, message counts and selected content, FTS results, thread relationships, sampled attachment hashes, limits/settings and expected invalidation of sessions/push associations. Reconcile known post-snapshot bans, membership/role revocations, blocks, credential changes and erasure before admitting users; require owner review/reset where current authority cannot be trusted.
6. Test login/recovery, a private authorization negative case, text delivery, media and monitoring. Record elapsed time and the recovered snapshot's age.
7. Approve traffic cutover, retain prior materials and record any lost post-snapshot writes. Never assert RPO/RTO compliance if acquisition/decryption or validation was excluded without disclosure.

Repeat on both required paths, after backup-format/schema changes, and at least quarterly as a proposed operating practice. Scheduling that recurring task is future operator work, not an automation created by this plan.

### 12.5 Monitoring and incident response

Define one accountable operator and an alternate. Protect diagnostic/metric access and use bounded-cardinality labels; never label metrics with raw private message text, tokens, full URLs or arbitrary user-provided values. Keep process RSS and CPU distinct from Go allocated heap. Log safe request/event correlation, component, outcome, build/schema and timing with retention limits.

The minimum alert set is process/HTTPS unavailable, authenticated socket failure, certificate renewal/expiry, free-space/quota pressure, backup/copy failure or age, migration/startup failure, sustained request latency/error rate, event queue saturation and media/relay failure. Host-side checks and external checks must be independently exercised. The plan requires delivered test alerts, acknowledged ownership, remediation links and recovery notices.

Maintain incident playbooks for outage, account/session compromise, private-data exposure, failed migration, corrupt storage, missing recovery keys, certificate failure and media-only failure. Each specifies containment, evidence preservation without spreading private data, recovery authority, user communication, rollback/cutover criteria and post-incident actions. Application changes must not silently send incident messages to users; communication policy and recipients are owner-controlled.

## 13. Open-source governance and release engineering

### 13.1 Public-project requirements

Before public source/release publication, resolve the license grant, copyright/asset ownership, dependency/license compatibility and contribution terms. Select a canonical repository and issue/security destinations; establish maintainer permissions, recovery and branch/tag protection; test private vulnerability intake; review source history and artifacts for private data. A README saying “open source” does not replace the license decision.

Provide a contributor quickstart, architecture/ADRs, review expectations, code of conduct and moderation ownership, support policy, bug/feature templates and a public scope/roadmap. Do not require a CLA or DCO by assumption; choose and document contribution rights deliberately. Retain build-from-source support and release-matching source even when signed binaries/images are the easiest installation path.

### 13.2 Candidate pipeline

The release pipeline must join the currently separate native/browser and Docker verification paths:

1. Identify immutable source commit, dependencies, toolchains, build/container inputs, app/protocol/schema/backup versions and intended support matrix.
2. Complete required lint/static/frontend/unit/integration/race checks and current dependency/security scans. Record actual hosted-run URLs and finding disposition.
3. Produce frontend and native/container artifacts from that source; generate dependency notices, SBOM and provenance; verify source/archive contents.
4. Complete signing/notarization and final packaging. Compute final checksums/digests and verify identities on clean consumer machines.
5. Run artifact-specific package/container smoke, supported migration/recovery and host/media/device gates using the artifacts that will be distributed.
6. Freeze the candidate and record approvals/observation. Promote only the approved identities to public stable channels.

For deterministic unsigned build inputs, aim for repeatable hashes where feasible; signed/notarized artifacts can contain variable metadata. Document what is reproducible and verify provenance/contents rather than claiming every independently signed file will be byte-identical. Capture actual runner/toolchain details instead of treating mutable labels such as “latest” as complete provenance.

### 13.3 Required release contents

| Artifact/evidence | Required content |
| --- | --- |
| Source | Exact licensed source matching the release, complete build/test inputs, dependency locks, public guides and no private operational material |
| Native packages | Correct executable/installer helpers, version/commit identity, license/notices, operational docs and platform signature where promised |
| Docker images | Explicit supported architectures, immutable version/digest, non-root runtime, persistent-volume and network contract, provenance and source link |
| Verification metadata | SHA-256/digests, signature verification guidance, SBOM, build provenance, compatible app/schema/backup versions and hosted evidence |
| Release notes | User changes, supported platforms/capacity, upgrade path, breaking pre-v1 changes, known issues and rollback data-loss boundary |
| Recovery materials | Prior supported binaries/digests, verified upgrade snapshot procedure, configuration/key custody guidance and clean-destination rollback steps |

Do not distribute a cross-built architecture as fully supported without runtime evidence. Windows Authenticode is required for broadly recommended official Windows downloads under this plan; Mac signing/notarization is required if native Mac downloads are promoted to the fully supported tier. Define a verifiable Linux/OCI artifact trust mechanism and key rotation/revocation process. Procurement failure is a visible release blocker or an explicit support/distribution scope change, not a reason to claim unsigned artifacts are signed.

### 13.4 Compatibility and maintenance contract

At v1, document stable configuration keys, supported install/update/restore workflows and any supported external API. Internal frontend APIs can remain internal only if clearly labeled; do not casually market an integration API without versioning, credentials, rate limits and compatibility tests. Deprecations require notice and a migration path. Schema and backup versions continue independently of SemVer.

Proposed maintenance policy: actively support the latest stable v1 minor and give the immediately previous minor a 90-day security transition window. This is conditional on a named maintainer accepting the obligation before beta. Propose monthly dependency/patch review and an emergency security-release procedure. Response targets, contact hours and patch deadlines must be staffing-backed; no 24/7 response guarantee is implied.

At launch, at least the responsible maintainer and an alternate should know how to build/promote a release, revoke publication/signing credentials and restore a server. If the team has only one person, explicitly document that concentration risk and arrange external recovery coverage before advertising dependable support.

## 14. Pilot, release candidate and launch

### 14.1 Entry to beta

Beta is for validating a coherent product, not discovering that private channels were implemented on only one API. Before entry, complete G1–G5 plus WP-28 security/privacy review and WP-29 accessibility/browser acceptance, resolve critical/high findings, provide a verified independent backup and recovery path, make operator/member documentation usable, and have consenting participants and support coverage. Preserve pilot data ownership and disclose the pre-release status and recovery boundary.

Recruit at least three small communities with different operators or usage patterns. Include both required server deployments and desktop/phone clients on independent networks. At least one operator and several members must not be the primary developer. Prefer realistic use of restricted channels, DMs, threads and live rooms over an artificial checklist-only demonstration.

### 14.2 Fourteen-day beta acceptance

Record daily build/deployment identity, activity, incidents, delivered alerts and recovery status. Pilot targets:

- Each operator can install and complete one backup/restore/update rehearsal with documentation and scheduled support, without undocumented application repair.
- Each community exercises invitations, private-channel membership, DMs/groups, moderation/reporting, threads/inbox, voice and desktop sharing; mobile clients participate in applicable flows.
- At least 90% of attempted prescribed critical journeys are completed without author intervention; investigate every failure and require 100% eventual successful completion of each required journey on the supported matrix.
- No confirmed unauthorized disclosure, acknowledged-data-loss defect, unrecoverable migration, hidden continuing forwarding or core-flow outage remains open.
- At least 30 complete cross-device call sessions across the pilots, including six sessions longer than 30 minutes, have logged route/browser outcome; these supplement the controlled two-hour media tests.
- Backup-age, off-host recovery and alert-delivery objectives are met; a silent scheduled-backup failure is a failed gate even if chat remains usable.
- All reported issues are reproducible or explicitly dispositioned with severity, owner and evidence. Do not bury unresolved failures in anecdotal positive feedback.

The journey percentage is a usability signal, not permission to release broken features. Final required functional/security tests still all pass. Review pilot friction such as invite setup, voice permissions, finding unread replies and recovering failed sends as product work, not merely support documentation.

### 14.3 Severity and observation reset rules

| Severity | Examples | Release disposition |
| --- | --- | --- |
| Severity 0 — critical | Unauthorized private-data access/forwarding, credible remote code execution, unrecoverable data corruption, compromised release credentials | Stop affected distribution/pilot exposure, contain, investigate and remediate; no stable release |
| Severity 1 — severe | Lost acknowledged content, unsafe upgrade/restore, login/chat/voice/share unusable on supported configuration without a reasonable workaround, normal client hidden capture after stop | Blocks release; fix and rerun affected broad gates |
| Severity 2 — material | Degraded function with a safe documented workaround, important usability/performance issue outside hard release budgets | Must have owner, rationale, workaround and release-note entry; OWNER/TL approval required |
| Severity 3 — minor | Cosmetic or low-impact wording/layout defect with critical flows intact | Triage and schedule; never use this label to hide an accessibility blocker |

Any fix invalidates the evidence for its affected behavior. Security/data integrity/migration/media/installer changes require the corresponding complete regression and real-host/device checks. A Severity 0/1 found during the 14-day beta restarts the affected deployment's 14-day clean observation after a verified fix; unaffected evidence can be retained with an explicit rationale. The final release waits for every affected requirement to have its required clean window.

For the final candidate, any executable/dependency/schema/protocol change creates a new artifact identity and restarts the seven-day final observation. Documentation-only changes require doc/link/artifact checks if docs are packaged; a signing/repacking change requires final-byte verification and an explicit review of whether runtime evidence remains applicable. Record the decision rather than silently reusing old pass labels.

### 14.4 Final seven-day candidate

Freeze the candidate's source, dependencies, configuration defaults, schemas/protocols, supported matrix and final distribution artifacts. Run the full required CI and artifact/host qualification, keep the final candidate under seven consecutive days of observation on both required deployments, and perform one clean independent recovery and one rollback rehearsal against its materials.

No feature work enters this candidate window. A new request goes to the next release unless it fixes a release-blocking defect; such a fix resets the applicable gates. Final review checks actual release notes and support claims against the ledger, not memory of earlier demos.

### 14.5 Launch and first thirty days

Publication follows the completed reviewable release result and explicit owner approval. Stage rollout from existing pilots to a small early-adopter cohort, then broader availability after initial operational review. Define pause thresholds: any suspected private-data incident, unexplained acknowledged-write loss, failed recovery, repeatable upgrade damage or widespread core-flow failure stops expansion immediately.

During the first seven days, review reports and operational signals daily at the accepted support coverage; confirm independent backups and rollback access before each upgrade cohort. At day 7, review incident/upgrade/capacity outcomes before expanding. At day 30, review support volume, recurring usability issues, dependency changes, cost/resource measurements and the v1.0.x/v1.1 backlog. These reviews are planned work; no scheduler or public notification has been configured by this document.

## 15. Risk register and change control

### 15.1 Primary risks

| Risk | Probability / impact | Early signal | Mitigation / owner |
| --- | --- | --- | --- |
| RK-01 Partial private-data enforcement | High / critical | A route/event/file/job lacks an access rule; UI-only restriction | Full surface inventory and G1 negative/race tests before exposure; BE + SEC |
| RK-02 Operator powers undermine DM promise | Medium / critical | Delegated admin retains full backup or chosen-password reset | Nondelegable operator tier, recent auth/audit, accurate trust disclosure; TL + OWNER |
| RK-03 Destructive schema/lifecycle migration | Medium / critical | Cascades/FTS rowids or owner backfill are unclear | Staged fixtures, explicit deletion contract, quiesced verified snapshot, clean rollback; BE + OPS |
| RK-04 Media redesign exceeds estimate | High / high | Track/codec/feedback/reconnect spike fails across browsers | Early WP-18 decision; qualify bounded profile; rebaseline optional SFU or scope transparently; MEDIA |
| RK-05 Browser/network promise exceeds capability | High / high | Screen audio, mobile capture, relay or background behavior differs | Capability matrix, external TURN, real devices and honest fallbacks; MEDIA + QA |
| RK-06 Backup health gives false confidence | Medium / critical | Recent file is accepted without completion/integrity/copy proof | Manifest/digest completion, independent verification and restore drill; BE + OPS |
| RK-07 Capacity targets fail on reference host | Medium / high | DB wait, sender queues, CPU/egress or client memory grows | Instrument first, realistic separate-host load, enforce caps, approve and retest revised profile; QA + OPS |
| RK-08 Missing host/signing/reviewer access | High / high calendar risk | Gate depends on unknown credentials, hardware or availability | Procure in WP-03; named owner and due date; do not present cross-build as substitute; OWNER + REL |
| RK-09 Scope grows into full Discord parity | High / high | Federation/camera/native apps/overwrites enter without estimates | Requirement/change register and explicit v1 boundary; OWNER + TL |
| RK-10 One maintainer becomes operational bottleneck | High / high | Releases, review and incidents depend on one unavailable person | Alternate recovery/release operator, protected credentials and realistic support policy; OWNER |
| RK-11 Notification/reconnect races expose or lose state | Medium / high | Old job/snapshot bypasses policy or replays suppressed work | Revision checks, bounded queues, retained dedup contract and deterministic failure tests; BE + FE |
| RK-12 Misstated release evidence | Medium / high | Prior alpha tests copied as current; signed artifacts differ | Per-build ledger, immutable identities, rerun/reset rules and final approvers; QA + REL |
| RK-13 Storage retention exhausts the server | Medium / high | Archive growth measured only on compressible toy data | Full storage formula, quotas, completed-copy retention rule and alerts; OPS |
| RK-14 Optional external services become hidden requirements | Medium / high | Core setup silently requires public STUN, vendor login or hosted relay | Dependency inventory, configurable endpoints and isolated/private-network smoke; TL + OPS |

### 15.2 Change request rule

Every proposed scope change records: affected requirement; user problem; explicit in/out decision; data/privacy/media/operations impact; package/dependency changes; added/removed estimate; acceptance tests; support consequences; and approval. Recalculate the critical path and budget before accepting it into v1.

The owner may reduce scope, but cannot waive accurate labeling. A reduced feature set can still become v1 if its new release contract is coherent and tested; a failed privacy/recovery/signature gate cannot be reclassified as passed to preserve a date. Never lower a performance threshold after a failed final run without publishing the changed profile, obtaining approval and rerunning qualification.

### 15.3 Stop conditions

Pause dependent implementation when an ADR changes its contracts, migration safety cannot be demonstrated, media feasibility fails, or required owner/legal/platform inputs are unavailable. Continue independent planned work where useful. Freeze distribution immediately for suspected severe data/privacy or release-integrity failures. Record the blocked dependency and required action; elapsed time is not an approval.

## 16. First implementation iteration

This is the proposed first execution sequence after the owner authorizes implementation. It is not work performed by this planning task.

| Order | Action | Required output before moving on |
| --- | --- | --- |
| 1 | Confirm v1 scope/staffing and license/repository owner | D-01–05 dispositions; named responsibility map; no inferred license grant |
| 2 | Establish immutable alpha.3 source and evidence baseline | Canonical commit, clean setup procedure, inventory of what was actually tested |
| 3 | Secure both staging hosts, recovery storage, relay route and reviewers/pilot contacts | Environment matrix with access owners and real test availability |
| 4 | Run existing hosted CI and record baseline host/runtime gaps | Actual run links and failure backlog, with no production support claim yet |
| 5 | Approve permission/operator/DM/deletion contracts | ADR D-06–11, resource/event matrix and threat-model review |
| 6 | Define quiesced upgrade/backup/durability contract | Schema-2 fixture, recovery rehearsal plan, FTS/file/disk invariants |
| 7 | Begin the media spike and backup completion work in parallel with policy design | Feasibility measurements and operational gap evidence; no capture UI before track contract |
| 8 | Slice the first implementation issues | Minimal owner/operator authority → central policy → role/channel UI → complete delivery enforcement; each has negative tests |
| 9 | Rebaseline after initial throughput and spike results | Updated estimates, finalized support targets and remaining procurement blockers |

A reasonable first review point is after roughly ten focused engineering days, not a promised feature release. Use it to confirm that the selected privacy model, source/CI baseline, host availability and media direction are viable. If one developer is available, prioritize the highest-risk dependency and limit work in progress rather than starting all 33 packages simultaneously.

## 17. Evidence ledger and final release checklist

### 17.1 Evidence record format

Each required result must identify:

- Requirement/test/gate IDs and work-package owner.
- Source commit, application/protocol/schema/backup versions and build configuration.
- Final package checksum or image digest; signing/provenance identity where relevant.
- Server hardware, OS/build, runtime/filesystem/network/relay configuration and data fixture/seed.
- Client OS/browser/device/assistive-technology versions and relevant permissions.
- Steps, expected outcome, actual outcome, pass/fail, sample size, timestamps and measurement method.
- Logs/results/screenshots or recordings using consented synthetic data, with secrets/private content redacted.
- Defect references, known limitations, rerun reason and reviewer approval.

Store evidence with immutable links or checksums under an owner-controlled release record. Assign **Not run**, **Pass**, **Fail**, or **Not applicable with approved rationale**; never leave a blank row looking implicitly passed. A plan, generated screenshot or mock cannot be evidence for real network/audio/service behavior.

### 17.2 Requirement coverage index

| Requirements | Principal evidence |
| --- | --- |
| R-01 Self-hosting | T-INSTALL-01, T-RESTORE-01, both-deployment matrix, J-01/J-08/J-09 |
| R-02 Accounts | T-IDENT-01, T-AUTH-03/04, J-01/J-02 |
| R-03 Permissions | T-AUTH-01–04, T-GROUP-01 where shared policies apply, J-01/J-03 |
| R-04 DMs/groups | T-DM-01/02, T-GROUP-01, T-CHAT-01/02, J-03/J-05 |
| R-05 Threads | T-THREAD-01, T-READ-01, T-NOTIFY-01, J-04 |
| R-06 Message experience | T-CHAT-01/02, T-CONTENT-01, T-A11Y-01, J-02/J-07 |
| R-07 Notifications | T-NOTIFY-01/02, T-PWA-01, J-02/J-04 |
| R-08 Moderation | T-MOD-01, T-AUTH-03/04, J-05 |
| R-09–10 Voice/sharing | T-MEDIA-01–03, T-NET-01, physical-device matrix, J-06 |
| R-11 Mobile/accessibility | T-A11Y-01, T-PWA-01, actual browser/device matrix and scoped human audit |
| R-12 Durability/recovery | T-DATA-01/02, T-BACKUP-01, T-RESTORE-01, J-08/J-09 |
| R-13 Operations/capacity | T-SOAK-01, alert drills, reference profile and 24-hour runs on both hosts |
| R-14 Public project | T-RELEASE-01, license/security/governance records, newcomer build/install rehearsal, J-10 |

### 17.3 Final v1.0.0 go/no-go checklist

- [ ] Owner approves the exact scope, capacity profile, support tiers, trust/deletion policies and known limitations.
- [ ] Project license, source ownership, canonical repository and contributor/security reporting processes are real and tested.
- [ ] All required work packages are complete or removed through an approved coherent scope change; no hidden incomplete private-data path is exposed.
- [ ] Every required test/matrix row has a passing or approved non-applicable result against the correct final build; hosted CI evidence is linked.
- [ ] Independent security/privacy review is complete; no unresolved critical/high exposure or Severity 0/1 defect remains.
- [ ] Ordinary delegated administration cannot bypass DM isolation via files, search, events, backup/export or credential-reset powers.
- [ ] Supported schema upgrades, power/process fault contracts, backup-format compatibility and clean rollback/recovery pass.
- [ ] Real Windows/Tailscale and Linux Docker lifecycle/HTTPS/network/storage/media/alert rows pass.
- [ ] Physical browser/device voice, screen sharing, push/PWA, keyboard and accessibility acceptance passes within documented capability limits.
- [ ] Target-load, two-hour media/peak tests and 24-hour soaks pass with resource headroom and no correctness/authorization failure.
- [ ] Independent recovery meets declared RPO/RTO, with configuration/key custody and an alternate recovery owner recorded.
- [ ] Restored historical security/credential/erasure state is reconciled before cutover, with unknown recovery-window changes explicitly disclosed and affected access kept restricted when needed.
- [ ] Monitoring delivers induced-fault and recovery alerts to responsible people; incident playbooks are usable.
- [ ] Fourteen-day beta and seven-day final-candidate observation requirements are met, with reset decisions recorded.
- [ ] Final stamped/signed/notarized/packaged artifacts have verified signatures/digests, provenance, SBOM/notices, matching source and clean-consumer installation evidence.
- [ ] Release notes, support/maintenance policy, version compatibility and rollback data-loss boundaries match observed behavior.
- [ ] Previous release/recovery materials remain available; signing/publication credentials and revocation/recovery procedures have owners.
- [ ] OWNER, TL, SEC, OPS, QA and REL sign the release ledger in their assigned capacities; independent review is identified separately from the implementer.
- [ ] Publication and rollout are explicitly authorized only after the completed release result is reviewable.

If any required item fails, the build remains pre-release. The correct next step is to fix it, reduce the declared scope transparently where appropriate, or resolve the missing dependency. A version-number change does not supply the missing evidence.

## 18. Source map and reference standards

### 18.1 Current-project sources reviewed

| Planning area | Primary local sources |
| --- | --- |
| Baseline/scope/evidence | [README](../README.md), [roadmap](roadmap.md), [acceptance](acceptance.md), [architecture](architecture.md), [alpha guide](alpha-testing.md) |
| Schema/storage/durability | [schema 1](../internal/db/schema.sql), [migration 2](../internal/db/migration2.sql), [store/migration entry](../internal/db/db.go), [message hydration](../internal/db/messages.go) |
| Routes/identity/authority | [router](../internal/api/router.go), [middleware](../internal/api/middleware.go), [authentication](../internal/api/auth.go), [sessions](../internal/api/login_session.go), [MFA](../internal/api/mfa.go), [administration](../internal/api/admin.go) |
| Private-surface inventory | [state/unread](../internal/api/state.go), [channels](../internal/api/channels.go), [messages/search](../internal/api/messages.go), [context/replies/pins](../internal/api/conversations.go), [uploads](../internal/api/uploads.go), [thumbnails](../internal/api/thumbnails.go), [mentions](../internal/api/mentions.go), [push](../internal/api/push.go), [WebSockets](../internal/api/websocket.go), [hub](../internal/ws/hub.go) |
| Client behavior | [workspace](../web/src/lib/workspace.svelte.ts), [replies](../web/src/lib/replies.svelte.ts), [composer](../web/src/lib/components/Composer.svelte), [drafts](../web/src/lib/drafts.ts), [update handoff](../web/src/lib/reload-drafts.ts), [updates](../web/src/lib/updates.svelte.ts), [service worker](../web/public/service-worker.js) |
| Media/network | [SFU](../internal/rtc/sfu.go), [rooms](../internal/rtc/rooms.go), [TURN](../internal/rtc/turn.go), [voice API](../internal/api/voice.go), [voice client](../web/src/lib/voice.svelte.ts), [RTC workload](../internal/rtc/load_test.go) |
| Recovery/operations | [backup/restore](../internal/backup/backup.go), [schedule](../internal/api/backup_schedule.go), [production check](../internal/app/production.go), [installer](../internal/installer/install.go), [updater](../internal/installer/update.go), [runbook](production-readiness.md), [hosting](hosting.md), [security](security.md), [Docker guide](docker.md) |
| Packaging/CI/project | [native/browser workflow](../.github/workflows/check.yml), [Docker workflow](../.github/workflows/docker.yml), [release helper](../scripts/release.go), [package check](../scripts/check-package.mjs), [Docker check](../scripts/check-docker.mjs), [contributor guide](../CONTRIBUTING.md) |
| Existing measurements | [chat workload](../internal/api/load_test.go), [historical performance fixture](performance-2026-09-14.json), [browser journey](../web/e2e/release.spec.mjs), [browser configuration](../web/playwright.config.mjs) |

### 18.2 External references checked for this plan

- [Semantic Versioning 2.0.0](https://semver.org/): basis for defining versioned compatibility and distinguishing pre-release from stable commitments.
- [SQLite synchronous behavior](https://www.sqlite.org/pragma.html#pragma_synchronous): basis for the documented distinction between WAL/NORMAL and the proposed production durability policy.
- [MDN getDisplayMedia](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getDisplayMedia): source of the capture-permission/user-action and optional-audio constraints; recheck browser capability data when qualifying a release.
- [W3C WCAG 2.2](https://www.w3.org/TR/WCAG22/): target reference for the scoped accessibility review, not evidence of current conformance.
- [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0): reference for selecting applicable application-security verification requirements, not a certification claim.

External support/toolchain/provider details can change before v1. Revalidate them at the relevant milestone instead of treating this planning date as permanent compatibility evidence. Capacity, effort, staffing and quality budgets in this document are this project's proposed planning decisions, not facts borrowed from those standards.

### 18.3 Glossary

| Term | Meaning in this plan |
| --- | --- |
| ADR | Architecture decision record: the selected design, alternatives, rationale, owner and consequences. |
| RPO | Recovery point objective: maximum acceptable age of recoverable data; a 24-hour target permits at most that recovery-window loss under the declared scenario. |
| RTO | Recovery time objective: target time to restore accepted service from the declared recovery event, including the steps and response time specified here. Total outage is also reported. |
| SFU | Selective forwarding unit: the server component that forwards participants' media to permitted recipients. |
| STUN / TURN | WebRTC address discovery / relay services. A relay carries media when a direct route is unavailable; an HTTP proxy does not replace that route. |
| ICE / SDP | WebRTC connection-path establishment / session description and negotiation. These are distinct from application login and channel authorization. |
| RTCP | Media feedback/control messages used with RTP streams; needed for observable quality and appropriate video adaptation. |
| WAL / FTS | SQLite write-ahead log / full-text search. Migration and recovery must preserve both data and the relationships used by search. |
| RSS | Resident set size: process memory resident in RAM, distinct from the Go heap allocation metric. |
| PWA | Progressive web app: the installable browser application and its service-worker shell; it is not a native mobile app or a promise of offline private history. |
| p95 / p99 | The latency/resource value at or below which 95% / 99% of the measured samples fall; the workload and samples must be specified. |
| SBOM | Software bill of materials: a machine-readable inventory of distributed components/dependencies. |
| Idempotency | Retrying the same declared operation yields the same logical outcome within its supported eligibility window rather than creating duplicate work. |
| Tombstone | A minimal retained record of identity/deletion/state, used to preserve references or prevent resurrection without retaining erased message content. |

### 18.4 Planning-task change boundary

This document is the only intended repository change from the planning task. Existing implementation, dependencies, schema, test suites, configuration, release packages and infrastructure remain unchanged. Read-only source review and previously recorded test evidence informed the plan; no new application test run, deployment, public publication or recurring automation was performed for it.
