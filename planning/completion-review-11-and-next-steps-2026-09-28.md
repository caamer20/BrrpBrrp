# BrrpBrrp — Review 11: finish the remaining P10 work

**Date:** 28 September 2026, America/Los_Angeles  
**Reviewed HEAD:** `ba8921886c56bfb0b3c947f297fefd0051947e2f` — clean at review start  
**Branch:** `codex/p9-private-reporting`  
**Current application:** **0.4.0-alpha.7** · schema **13** · backup format **2**  
**Scope:** assess the attached coder report, inspect changed source/evidence, run bounded checks and prepare the next execution handoff. No application changes or deployment.

## 1. Verdict

**The latest work is mainly a repair of P9. The important review-10 backend and reporter-flow defects are repaired, with useful regressions. The previously requested P10 feature and distribution deliverables remain unfinished.**

There is still no pending-introduction reporting, immutable request-attempt identity, atomic report-and-block operation or its receipt/recovery/UI. No alpha.7 distribution archives or extracted alpha.7 package consumers exist. The next coder should complete **the remaining P10**, keeping its original package numbering, rather than start another repair-only milestone or jump to full DM notifications.

The attached report accurately acknowledges the missing feature/archives and external qualification. Its broad “every deliverable” completion wording should be read as the repair work it executed, not completion of the earlier P10 assignment or v1. The latest source is stronger, but one reviewer busy-state defect remains and the appended reruns need clearer durable evidence.

## 2. Overall progress toward v1

| Product area | Working implementation / local evidence | Remaining boundary |
| --- | --- | --- |
| Community conversations | Messages, attachments, replies, search, reactions, pins, mentions, responsive UI, themes and PWA shell | Named/followed/archived/locked threads, richer daily UX and broad accessibility/browser/mobile qualification |
| Identity and permissions | MFA/session recovery, ownership, additive roles/delegation, scoped channels, centralized mutation/delivery authority | Independent review and actual Windows/Docker identity/recovery ceremonies |
| Resilient shared messaging | Required conversation scope, preserved legacy IDs/rowids, deletion-safe receipts, bounded opt-in outbox, explicit text recovery and monotonic read state | Group/thread integration and performance/reconnect qualification |
| Direct conversations | Canonical consent, durable contact limits, block/unblock, hide preferences, shared content/file/search/read paths | Pending-request safety, DM activity/notification system, groups, broader lifecycle/export |
| Private evidence | Accepted-message text previews/reports, own reviewer capability, review queue, expiry, durable limits and restricted audit feed; R10 Unicode/preview/audit/count repairs | Remaining reviewer race; pending-introduction reporting, command receipts and corresponding recovery; selected file evidence later |
| Notifications/discovery | Channel mention muting, cross-device preference refresh, incoming request badge and DM unread state | No DM OS/push/sound/ringing; no complete activity inbox, preference precedence or deduplication contract |
| Voice/media | Community Opus/SFU with real negotiated synthetic RTP regressions | Physical devices/networks/relay qualification, private calls, screen sharing and multi-publication lifecycle |
| Data/operations | Ordered migrations, verified archives, local fresh/private restore and old-release rollback | Current packages/consumers, both-host lifecycle, encrypted independent restore, measured recovery objectives, received alerts and capacity/soak |
| Open-source release | Local Git, MIT/CLA/notices, CI definitions and local audits | Public repository/security intake/maintainer and signing identities; hosted CI; independent reviews; beta and publication |

No **G0–G8** gate has passed. Feature counts and local test counts do not support a defensible completion percentage. Both **Windows/Tailscale and Linux Docker** remain required; the recorded MIT/CLA direction remains settled.

## 3. What was freshly checked

| Check | Review-11 result |
| --- | --- |
| Git/source identity | Clean initial HEAD and branch above |
| Frontend unit/state suite | **114/114 passed**, zero failed/skipped |
| Svelte/type checking | **0 errors, 0 warnings** |
| Focused Go regressions | Unicode reporting, restricted audit and requester opt-out count regressions: **passed**, API **2.763s**, uncached non-race run |
| Browser-step audit | Plain and self-test passed locally; POSIX audit is now in the required Linux browser job, with Windows app tests retained |
| Binary identity | Current `dist/brrpbrrp-p9` hash **`051b7f3d39e416c776f8b294104299fc8b4982e38606687f794706ba5cb23ae5`**; clean embedded revision **`34bddbf4733969c58421dff985d74a876f8538b3`** |
| Frontend identity | **`20d76bada950286f`**, matching the built asset record |
| Artifact inspection | No alpha.7 ZIPs; all seven alpha.6 archive hashes remain unchanged |

Focused Go command:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test ./internal/api \
  -run '^(TestEveryScriptIsReportableAtTheSupportedLength|TestPrivateReviewAuditIsRestrictedToTheEvidencePolicy|TestRequesterOptingOutDoesNotWithdrawAnOutstandingRequest)$' \
  -count=1
```

### Inherited results, not rerun in this review

The appended [P9 verification record](p9-checkpoint/verification.md) reports 114 frontend passes, a full ten-package race run (API **1032.994s**), browser **13/13 default-off and 19/19 opted-in**, recovery **23/23 and 85/85**, and alpha.5/alpha.3 rollback **57/57 and 55/55**. These are the other coder's recorded executions, not fresh full-suite results from this review.

An earlier RTC connection failure is disclosed. The RTC implementation is unchanged since alpha.5 and later runs passed; that supports an intermittent failure observation. “Timing under load” remains a hypothesis, not an established root cause or physical-media acceptance. Keep the failure history and investigate if it recurs rather than repeatedly running suites until a green result appears.

## 4. Review-10 repair assessment

| Prior finding | Current state |
| --- | --- |
| R10-01 Unicode preview version | Fixed-size SHA-256 digest replaces raw content-bearing version; fresh maximum-length script regression passes |
| R10-02 general audit disclosure | General audit excludes private review rows before its limit; separate feature/capability-gated audit feed exists; fresh regression passes |
| R10-03 edit-conflict dead end | Structured API error detail survives; refreshed text and explicit re-read acknowledgement can complete; a real rendered journey exists |
| R10-04 stale reviewer generation | Mount-time request identity replaced with per-request identity; original refresh problem repaired. Separate busy-state race remains below |
| R10-05 Windows audit placement | Fixed: Linux-only shell audit moved out of the cross-platform application matrix |
| R10-06 badge eligibility | Requester's incoming-contact opt-out no longer hides their actionable outgoing request; fresh regression passes |
| R10-07 disclosure/capacity/gates | Reporter identity and backup boundary disclosed; feature-aware tabs; actionable capacity refusal and corrected policy text implemented |
| R10-08 exact recovery/current docs | Script now compares exact expiry/text/reason/status and corrects its diagnostic. Current progress ledger and durable rerun records remain incomplete |

The added rendered edit-conflict journey proves one edit/reconfirmation and successful submission. It does not prove a second edit or inspect the final reviewer record. There is still no rendered reviewer refresh/revocation journey. Those missing controls should be added while extending the evidence UI, not described as already covered.

## 5. Remaining concrete findings and evidence gaps

### R11-01 — P2, source-confirmed: reviewer mutation can remain busy forever

`admin/PrivateEvidence.svelte:109–120` sets `busy` and captures the shared `version`. Refresh, Load older, filter changes or an authority-triggered load increment that version (`:65–69`). When the held PATCH finishes, its `finally` fails `admitted(request)` and does not clear `busy` (`:137`). Later review actions return immediately while `busy` stays nonempty.

**Not browser-reproduced in this review.** Required regression: hold a real review PATCH; refresh/change filter or trigger an unrelated authority update; release the PATCH; then review another record without closing the panel. Keep stale completions from changing the wrong view while ensuring invalidated work cannot permanently hold the busy flag. Include allowed refresh, permission revocation and account-switch controls. Separate mutation ownership from list request sequencing.

### R11-02 — ordinary reporter uncertainty needs an available recovery action

The updated dialog no longer falsely asserts nothing was sent, but it directs the ordinary reporter to reopen a report list that is reviewer-only. Add a narrow own-receipt check/retry flow alongside P10's required command receipts. It must reveal only that account's minimal outcome and must not grant reviewer queue access. A replay returns an original outcome; it must not assert that the sender is still blocked after a later deliberate unblock.

### R11-03 — latest executable and rerun evidence need separate identities

The latest executable embeds clean **`34bddbf`**, not `4e149ad` or `f329993`. The later difference is only `web/e2e/directs.spec.mjs` and the evidence ledger, so current application inputs match. This is not evidence of untested application changes; record binary build revision and test-fixture revision separately.

The four committed P9 recovery/rollback JSON files still identify original binary **`fddd1e3f…`** / source **`2869db5`**. They do not substantiate the appended reruns of **`051b7f3d…`**. Preserve them as historical records and save new machine-readable reports in a new checkpoint directory when performing the next final verification. This gap is not proof that the reported reruns did not occur.

Do not repeat a full 17-minute suite solely because an evidence-only commit changed HEAD. Freeze runtime/package inputs, test the identified bytes, record separate fixture/evidence revisions and keep the results attributable.

### R11-04 — remaining documentation, audit policy and acceptance

- `docs/v1-progress.md:5,7,432` still identifies alpha.6/schema12 as current, despite corrected README/deployment/master-plan references. Update its summary and relevant rows to the real alpha.7 state and unfinished P10.
- Restricted private audit now has the proper access boundary, but its retention is not specified separately from 90-day evidence. Define metadata retention, account-erasure handling and backup treatment; do not silently promise all audit metadata expires with evidence. Add direct pagination, feature-off, revocation and audit-failure rollback controls.
- Packaging/consumer acceptance remains missing: no current alpha.7 archives or extracted native/source consumers. This was already an explicit P10 deliverable, not a newly invented gate.

## 6. Remaining P10 execution order

| Package | What remains | Completion criterion |
| --- | --- | --- |
| P10-01 finish-up | R11 reviewer busy-state regression/repair, own-outcome UX, accurate current docs and restricted audit policy/controls | Useful reviewer actions recover after overlap; private data remains fenced; ordinary reporter can reconcile without reviewer access |
| P10-02 | ADR for pending-introduction reporting, attempt identity, source kinds, atomic mutation and receipt lifecycle | Exact schema/API/consent/retention/failure contract recorded before implementation |
| P10-03 | Recipient-only preview and atomic Report request and block | Exact current introduction saved before clearing, no acceptance/membership, all effects commit together or none do |
| P10-04 | Request action, explicit disclosure, uncertain-outcome reconciliation and typed reviewer source UI | Real recipient/reviewer journeys, expected authority invalidation handled, separate Block remains available |
| P10-05 | New migration/recovery/race coverage | Preserve all schema13 reports and restore exact request evidence, receipts and security state; renewed-attempt/retry/unblock races pass |
| P10-06 | Clean native/source distribution and consumer evidence | Identified packaged bytes, manifests, extracted consumers and fresh reports; preserve historical artifacts and distinguish host acceptance |

The central next user journey is: **receive an unwanted request → preview exact evidence → Report request and block → see confirmed outcome → reviewer sees only submitted evidence**. Acceptance must not require opening a DM or consenting to contact first.

A canonical pair can request again later. Add an immutable attempt identity and bind the preview/operation to that attempt. A committed retry must return its receipt after source clearing; resolving evidence, unblocking or receiving a renewed request must never make it new work. Evidence, receipt, block, rejection, introduction clearing and required audit commit in one transaction through existing authority ordering. Keep the same private rate/capacity limits across evidence kinds. Ordinary Block must remain usable when reporting fails or is full.

## 7. What follows P10

1. Dedicated DM activity and notifications: stable activity identity/retention, useful inbox without push, account/conversation/device preference rules, safe previews, active/background/closed-PWA behavior, deduplication and current-access/block dispatch checks.
2. Consenting groups and lifecycle/export: invitations, owner/leave/remove/history semantics, shared-group blocks and restored membership safety.
3. Named threads and daily community UX/moderation: complete bounded history, follow/read/archive/lock, composer/upload/navigation and human accessibility.
4. Media: measured architecture spike, private calls, screen/audio publications and physical browser/device/network/relay qualification.
5. Operational/open-source track in parallel: canonical repository/security intake/maintainer/signing identities; corrected hosted CI; both-host lifecycle; encrypted off-host restoration/configuration/key custody; measured recovery objectives, alerts and load/soak.
6. Independent reviews, non-author install/restore, three-community/both-host 14-day beta, seven-day final-candidate observation, explicit publication approval and support ownership.

Do not add full DM push, groups or media to the current P10 implementation. Missing host/CI credentials do not prevent the local feature work. Keep gates open until the required evidence actually exists.

## 8. Next-coder prompt

[Complete the remaining P10 — standalone implementation handoff](handoff-next-coder-review-11-2026-09-28.md). Its completion checklist explicitly separates already repaired R10 issues from the feature and artifact work still required. A repair-only report must not mark the whole P10 assignment complete.
