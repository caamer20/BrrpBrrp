# Completion review 13: groups, activity and the path to v1.0.0

**Date:** 29 September 2026.  
**Inspected source:** `6e36c10c162bffa525fc0f676b5b7fb5092b4719`, branch `codex/p9-private-reporting`; clean at review start.  
**Current application:** `0.4.0-alpha.7`, schema **17**, backup format **2**.  
**Scope:** review the supplied group-completion report, inspect changes since review 12, run bounded meaningful checks, and prepare a standalone implementation handoff. Application code is unchanged by this review.

## 1. Overall assessment

There is substantial new implementation since `6b7b301`: **13 commits and 48 changed files** introduce several review-12 repairs, durable activity and notification preference APIs, typed push destinations, consenting group membership, and a group management interface.

The report accurately acknowledges unfinished lifecycle/export/recovery work and external release gates. Its description of groups as usable end to end is broader than the implementation supports. The rendered journey covers **group membership management**. It does not open a group conversation or exercise its composer, live message delivery, attachments, search, read state or send recovery. The current client cannot open a group chat; several shared server paths still reject groups.

Similarly, M1 is an activity/API foundation. It has no user-facing activity inbox or private preference controls, and ordinary DM/request mutations do not enqueue the new private push path. New storage and pagination defects also undermine its stated guarantees.

**The next checkpoint should finish the integrated M1/M2 product, including privacy and recovery, before declaring either milestone complete.** Threads, remaining moderation/UX, private calls, screen sharing and operational acceptance still follow. No G0–G8 gate has passed, and no credible whole-project completion percentage can be assigned from test counts.

### Progress by area

| Area | What has actually landed | Remaining boundary |
| --- | --- | --- |
| Review-12 server fixes | Central audit classification, combined evidence cap, Unicode character limit, destination/preview-bound receipt replay, distinct expiry fields and spent-key refusal | Request evidence cleanup/receipt compaction still absent; group audit introduces a new metadata exposure |
| Reporting client | Explicit state helper, refusal retry improvements, visible receipt errors, evidence-row deduplication | Real component transitions and exhausted-source requests remain incomplete; disclosure still omits backups/physical cleanup |
| Durable activity | Transactional activity rows, list/acknowledgment APIs, private preference APIs, typed push helpers | Correct bounds/retention/filtering, usable inbox/preferences, real producers/dispatch, deduplication and device qualification |
| Groups | Shared membership model; create, invite, accept/decline, list members, transfer, remove and leave interface | Open/read/send through common client, events, reporting, revocation, invitation safety and lifecycle |
| Recovery | New schema13 preservation tools and reviewer-grant restore checks | Current schema17 qualification, honest API-based revocation, mixed groups/activity fixtures and packaged tools |
| Distribution | P10 archive inventory completed; race timeouts aligned to 40m | No clean schema17 distribution; current source remains labelled alpha.7 |
| Full v1 | Strong existing community/chat/authorization foundation | Named threads, remaining moderation/UX, private media/sharing, both-host operations, independent acceptance, beta and release |

## 2. Evidence and its limits

### Fresh checks in this review

- `npm --prefix web test`: **132 passed, zero failed**.
- `npm --prefix web run check`: **zero errors, zero warnings**.
- **Eight selected review-12 repair regressions passed** in the server audit.
- **Nine temporary defect probes failed as intended**, reproducing current bugs: four activity probes and five group/retention probes. Their expected exit status was 1, not a failed pre-existing suite.
- Current source, migrations, UI/controller paths, packaging/recovery scripts, ADRs and release records were inspected.

The activity probes ran through an external Go overlay:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test \
  -overlay /private/tmp/brrpbrrp-review13-activity-au7l7ohh/overlay.json \
  ./internal/api -run '^TestReview13' -count=1 -timeout 3m -v
```

API duration: **11.296s**. The group/retention audit used a second external overlay under `/private/tmp/brrp-review13-kndrs142/`, completing its selected batch in **12.025s**. These are temporary local diagnostics, not portable project dependencies. The next implementation must add permanent regressions for the described scenarios.

No new full race suite, browser suite, native packaging, real-host or physical-device qualification was run in this review. The fresh frontend results do not imply those checks passed.

### Historical and reported checks

The latest coder reports vet, full Go tests, 132 frontend tests, clean Svelte/formatting, and browser counts **13 default-off / 22 private-enabled** at `6e36c10`. The group browser journey is present in source. Its use of page reloads means it does not establish live ownership/membership reconciliation.

The [M0 checkpoint](m0-checkpoint/verification.md) records an explicit race command, `go test -race -count=1 -timeout 40m ./...`, with API **1330.041s**, browser **13/21**, recovery **23/113** and schema13 rollback **57**. Those results describe **source `153202d`, schema14**, not current schema17. The checkpoint has a manifest and Markdown record but no new raw recovery JSON reports or immutable executable digest.

### Artifact identities

| Artifact | Identity / conclusion |
| --- | --- |
| Latest native/source archives | Still **`976366148348cb7ea754bb5304c126503ae68639`**, schema14, frontend `b17ccfd6cd9325e2`; all seven historical archive hashes still match |
| M0 source checkpoint | `153202da9ae15118be8fff13bcf7c84e688db345`, schema14, frontend `be50533485433eca`; deliberately not repackaged |
| Current `dist/brrpbrrp` | SHA-256 `e457d2a30084a59e7d4ec5aa8732506ffe99cdf4dee92562189a08dae3cc6a3e`; stamped `f4d3a7c…`, **`vcs.modified=true`** |
| Current clean schema17 distribution | **Absent**; the workspace binary is not a clean final artifact from HEAD |

Preserve existing archive identities. Changed distributions need a new prerelease label and fresh consumers using extracted bytes.

## 3. Findings requiring implementation

The identifiers **V13-01–10** below belong to this review. They avoid confusing these findings with “review 13” labels already used inside the coder's comments. P1 indicates a privacy/authority or essential integration blocker; P2 indicates material functional, lifecycle or evidence work. This is a bounded engineering review, not independent release acceptance.

### V13-01 — Group messaging is not integrated (P1)

**Source confirmed:** `internal/api/state.go:89–115` supplies direct descriptors; `web/src/lib/workspace.svelte.ts:200–245` recognizes only channel/direct admission, paths and URLs. Group cards in `MessagesInbox.svelte:510–610` have management actions but no Open conversation action.

**Reproduced server gaps:** `conversationAudienceIDs` rejects groups, so the common event publisher cannot produce their live audience; a member's group-message report preview returns **404** because `private_reports.go` still requires a direct scope.

**Closure:** admit group descriptors into the existing conversation pipeline, navigation, composer, history, uploads, search, reads, receipts/outbox, events and selected-message reporting. Prove two members can actually converse and use shared features. Audit every direct-only branch deliberately; a mechanical replacement of all `direct` checks would break consent and blocking distinctions.

### V13-02 — Group membership metadata leaks through ordinary audit (P1)

**Reproduced:** a nonmember with ordinary audit-read permission can see `group.invited` and `group.joined`, actor usernames and the private group ID. No message text was exposed.

The new group handlers write unqualified `group.*` events into ordinary audit. Fixing request-review prefixes did not establish a policy for private group lifecycle metadata.

**Closure:** define the authorized audience and retention for group audit events, classify historical rows appropriately, and test actual handler-written events with nonmember/member/reviewer/operator controls. A private-evidence review grant must not silently become permission to browse all group metadata.

### V13-03 — Membership changes do not enforce the full revocation protocol (P1)

**Reproduced:** removing an accepted member leaves authorization revision **1 → 1**, and an already-registered attachment stream remains uncancelled. Group mutations use plain commits instead of the established authority mutation/revocation protocol.

**Source confirmed client issue:** `MessagesInbox.svelte:123–145` clears direct state on invalidation but leaves group/invitation/member arrays. Those arrays render without equivalent admitted projections. The member refresh in `groupLifecycle:230–239` admits its late response without account/generation/group checks. Stale group metadata can return after revocation or a failed confirming fetch.

**Closure:** integrate group membership/ownership with transaction ordering, revision advancement, stream/job/media cancellation and committed invalidations. Fence all client responses. Prove live updates across open tabs without reload, with held file/history/member responses and failed refreshes. An uncancelled test stream is direct evidence of missing cancellation; it is not a claim that real private bytes were exfiltrated during this review.

### V13-04 — Invitation and empty-group lifecycle are incomplete (P1)

**Reproduced:** the sole owner leaves while an invitation is pending; accepting that invitation returns **200** into a group whose conversation state is already `removed`; reading messages then returns **404**.

**Source confirmed:** invitation creation does not apply the existing block/contact policy. `discardGroupIfEmpty` changes the conversation state only, despite comments/ADR language promising cascaded removal of messages, files, activity and invitations. Expiry changes invitation state but leaves other lifecycle obligations incomplete.

**Closure:** check active group/owner/current invitation identity and eligibility at acceptance, close pending invitations atomically on group removal, apply block/contact rules at invite and accept, and define exact terminal-group retention/cleanup. Make the last-member UI state its actual consequence. Add finite group/invitation admission and cleanup rather than relying on a 200-row display limit.

### V13-05 — Activity paging and source validity are incorrect (P2)

**Reproduced:** 51 currently ineligible request rows above one valid message row produce an **empty activity page, empty cursor and unacknowledged count 52**. Eligibility is computed after SQL's 51-row limit, so valid older activity is unreachable. Counts include inaccessible/ineligible rows.

**Reproduced:** a schema-permitted null actor, simulating the declared erased-actor state, makes the list return **500** because a nullable username is scanned into a string.

**Source confirmed:** request eligibility checks the pair's current pending state without binding the activity's request attempt; group-kind filtering and dispatch are incomplete. Message/source deletion and acknowledgment boundaries need explicit coverage.

**Closure:** filter the full source/current-access predicate before pagination/counting, bind request activity to its immutable attempt, handle nullable/tombstoned actors, support intended kinds, and define precise acknowledgment semantics using activity IDs. Test hidden-heavy pages, deletion, renewal, revocation and stale cursors.

### V13-06 — Activity bounds and expiry are not enforced (P2)

**Reproduced:** with 2,000 unacknowledged rows, a real message POST returns **201** and creates row **2,001**. `recordActivity` does not enforce the declared bound. An unacknowledged row remains after **31 days** because pruning applies only when the account is also above the cap.

The existing “bounded” test sends five messages and checks that constants are positive. It does not exercise admission at the bound or expiry below it.

**Closure:** implement a documented finite storage/overflow policy and actual 30-day expiry in bounded batches. Keep message durability and activity-overflow semantics explicit; a catch-up buffer must not silently make acknowledged messages disappear or indefinitely prevent communication. Test exact boundary, concurrent writers, large backlog, restart and time advance.

### V13-07 — Private notification delivery and activity UX are unfinished (P2)

**Source confirmed:** no frontend calls consume activity, acknowledgment or private preference APIs. Normal DM/request producers do not call `queueActivityNotice`; the only call is the incomplete group-invite path. Group notification kinds are missing from dispatch eligibility. The payload supplies `tag` while the service worker reads `message_id`; no retained 24-hour deduplication or active-view suppression exists. `/messages` is not an implemented activity route. Existing UI copy still says private notifications are unavailable.

**Closure:** build the account-owned activity/preference experience, connect real post-commit producers, use stable identities across app/worker, implement current-access links, preference reconciliation and active/background/closed-PWA behavior. ADR0015's deduplication claims describe intended work, not a delivered guarantee.

### V13-08 — M0 client and retention obligations remain partial (P2)

The server audit/capacity/Unicode/receipt-binding fixes are real. Remaining issues:

- **Reproduced:** expired `private_request_reports` evidence survives maintenance. Command receipts also lack bounded metadata compaction; preserve spent-key protection when adding it.
- `ReportRequest.svelte` enters `changed` without a path back to confirmation, treats transport uncertainty as refusal, and closes the dialog from “Back to report,” losing its key/explanation. Pure-state helper tests miss these component transitions.
- Disclosure still omits backup/physical-cleanup limits.
- Evidence merging now deduplicates rows, but the component still fetches both sources after one is exhausted. `beforeParam` returns an empty string, which requests that source's first page again. Rendering still keys by `id` instead of `(source_kind,id)`.

**Closure:** permanent real-maintenance and rendered UI tests, proper same-operation reconciliation, explicit changed-preview consent, skipped exhausted-source requests and consistent source-qualified keys. Do not reopen the server repairs that already pass.

### V13-09 — Account lifecycle can orphan group administration (P1)

**Reproduced:** the real account anonymization endpoint succeeds for a group owner, leaving ownership attached to the unusable Deleted member identity. A surviving member cannot transfer or administer the group. **Source-confirmed separate consequence:** physical deletion cascades the owner row; surviving membership still selects the group for `/api/groups`, but its required owner lookup fails and the endpoint returns 500. Physical erasure was not exercised by the probe.

**Closure:** define and implement a deterministic ownership disposition for deactivation, ban, retained-history removal and physical erasure, without granting unrelated administrators ordinary private-history access. Finish bounded user export, safe identity/content distinctions, attachment/search/report consequences and mixed direct/group recovery.

### V13-10 — Current recovery and release evidence are incomplete (P2)

- `upgrade-schema13-private.mjs:94` assumes exactly one new migration; valid schema13→17 fails that assertion.
- Reviewer-grant rehearsal falls back to direct SQL when API revocation fails, then records unconditional success. This proves a database-state restore/reconciliation property, not the supported role-management and revocation path.
- New schema13 scripts are absent from the explicit source-package inventory and CI.
- No migration15/16→17 populated-activity preservation, mixed group/activity restore report, clean schema17 distribution, or equivalent current race/recovery evidence is recorded.
- Progress prose still contradicts itself about groups/activity, dates and 21 versus 22 opted-in browser checks.

**Closure:** complete ownership/MFA setup in fixtures, require actual API success, remove success fallbacks, validate the complete expected migration registry, package/register the new tools, and produce fresh machine-readable reports on the next clean candidate. Routine fixture setup and retaining compatible audit prefixes do not inherently require another owner decision.

## 4. Updated execution plan

### First: finish the existing private product

1. **Close the authority and lifecycle blockers:** V13-02–04 and V13-09, including server revocation and stale UI admission. Define the terminal-group and account-owner policies before implementing their migrations.
2. **Make groups usable for conversation:** V13-01 through the existing shared pipeline, with a full rendered two-member chat journey and outsider controls.
3. **Make activity reliable and visible:** V13-05–07, including real producers, preference UI, deduplication and working deep links. Keep durable activity usable when push is unavailable.
4. **Finish remaining reporting repairs:** V13-08, with actual component and maintenance evidence.
5. **Close lifecycle/export/current recovery:** V13-09–10, including seeded migration15/16→17 preservation, account export and post-snapshot group/reviewer reconciliation. Package a new attributable prerelease when this integrated milestone is verified.

A meaningful next checkpoint includes working group chat, live membership changes, a usable activity inbox, correct reporting recovery and current-schema recovery evidence. Do not claim that a management-only browser journey closes private messaging.

### Then: complete the agreed v1 scope

| Milestone | Remaining deliverable | Acceptance boundary |
| --- | --- | --- |
| M3 | Named threads, independent read/follow/archive/lock, moderation timeouts/assignment/invites, safe composer/upload/member context, onboarding and responsive accessibility | Full 1,000-reply traversal; consistent permissions; keyboard/mobile failure and recovery journeys; thread activity integrated |
| M4 | Media spike, private calls/listen-only, distinct microphone/screen publications, desktop sharing/mobile viewing and relay diagnostics | Server revocation, real direct/relay/UDP-blocked paths, two-hour calls, 100 capture cycles and physical quality observations |
| M5 | Windows/Tailscale and Linux Docker lifecycle, encrypted independent recovery, alerts, capacity/soak, hosted CI, public-project identities and signed/verifiable packages | Both real hosts, target dataset, measured RPO/RTO and load, delivered alerts, extracted final-byte consumers |
| M6 | Independent privacy/security and human accessibility review, non-author rehearsals and full-product beta | Required prior gates and reviews; three consenting communities spanning both deployments; 14 consecutive clean beta days |
| M7 | Candidate freeze, compatibility, final recovery/rollback, observation and authorized release | Seven consecutive candidate days on both hosts, complete evidence ledger, explicit approval, publication verification and support ownership |

Start media architecture and host provisioning in parallel with independent feature work; those are long dependencies. Keep full beta behind the completed product and required reviews. Current missing Git remote, Docker, PowerShell, physical devices, independent reviewers and pilot hosts are external work items, not proof of failed application behavior.

## 5. v1 finish criteria

The [master plan](../docs/v1-implementation-plan.md) remains the scope and acceptance reference. G0–G8 are still open. A **feature-complete release candidate** requires all agreed product features; a **released v1.0.0** also requires operational qualification, independent acceptance, beta/candidate observation and approved publication.

Keep the proposed limits and performance numbers as qualification targets until measured. Both required deployments need the planned two-hour peak and 24-hour mixed soak. RPO ≤24h and RTO ≤60m include independent backup retrieval/decryption, operator response, security reconciliation and verification. Local recovery check counts do not establish those objectives.

Camera video, federation, E2EE, native mobile apps, multiple communities per process, bot marketplaces, enterprise SSO and clustering remain outside this v1 critical path unless the owner explicitly changes scope. Required groups, activity, threads, private calls and desktop sharing must not be dropped to reach a version label.

## 6. Handoff

Use the [new standalone implementation prompt](handoff-v1-completion-review-13-2026-09-29.md). It preserves the completed server fixes, starts with the newly verified integration failures, and carries the coder through the remaining product and release milestones.

The next completion report should identify features actually usable by a member, source/artifact identities, direct evidence for each repaired finding, tests newly run, pending external gates, and the next executable task. A helper test or a checked task list does not substitute for the relevant end-to-end journey.
