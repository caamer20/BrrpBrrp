# BrrpBrrp — Review 10: overall progress after P9

**Date:** 27 September 2026, America/Los_Angeles  
**Reviewed HEAD:** `89101e3e30d1984c495f7526f6c4283d5605387d`, clean at review start  
**Branch:** `codex/p9-private-reporting`  
**Tested implementation commit:** `2869db55394b2b2a004424672517b38d6908586b`  
**Current source:** **0.4.0-alpha.7**, database **13**, backup format **2**  
**Scope:** evaluate the other coder's report, source and evidence; bounded checks and diagnostic probes; next-step plan and standalone coder handoff. No application code or deployment changes.

## 1. Overall assessment

**P9 added real functionality: private text reporting, a separate evidence-review capability and queue, and incoming-request discovery. Its clean-source binary identity is now properly recorded. Several new reporting paths remain broken or under-tested, and no production release gate has passed.**

The coder's completed todo list is a record of their workflow, not independent product acceptance. The reported happy-path results are supported by source and evidence; this review found two reproduced backend defects, several client lifecycle defects and a likely Windows CI blocker. Repair those before extending reporting to pending requests.

The recommended next milestone is **P10: reliable private evidence and reporting abusive introductions without accepting contact**. Full DM activity/push, groups, organized threads and screen sharing remain separate milestones. Do not redo the implemented shared-storage/DM foundation or convert this alpha into v1 by renaming it.

### Cumulative progress toward the intended product

| Area | Achieved so far | Remaining before the planned v1 |
| --- | --- | --- |
| Community chat | Durable text/files/replies/pins/reactions/search, mentions, responsive UI, themes, PWA shell | Named/followed/archived/locked threads, richer composer/upload/navigation, full keyboard/mobile/accessibility acceptance |
| Identity/authority | MFA, sessions and recovery, owner ceremony, roles/delegation, scoped channels, admission/revocation contracts | Independent privacy/security acceptance, actual host ceremonies and continued regressions across new surfaces |
| Resilient messaging | Shared conversation storage, send receipts/spent-key protection, bounded opt-in outbox, explicit text recovery and monotonic read state | Broader performance/reconnect qualification and group/thread lifecycle integration |
| Direct conversations | Canonical requests/consent, durable limits, block/unblock, hide preferences, shared content/search/file/read paths | Introduction reporting, dedicated DM activity/notifications, full lifecycle/export and independent acceptance |
| Moderation/evidence | Community reports and slow mode; new private text preview/report/review capability, expiry and durable bounds | Findings below; request harassment, selected file evidence, report workflow/timeouts/invitation controls |
| Request discovery | Durable incoming-request count, badge and Requests navigation | Correct pending eligibility in R10-06 and complete cross-device/race coverage |
| Voice/media | Existing community SFU/Opus and negotiated synthetic RTP regressions | Private calls, typed publication lifecycle, screen sharing, physical device/browser/NAT/relay qualification |
| Data/recovery | Ordered migrations through 13, verified backups, community/private restore, actual alpha.5/alpha.3 rollback, explicit security reconciliation | Stronger equality assertions, alpha.7 package consumers, both-host encrypted independent restore, measured RPO/RTO |
| Distribution/operations | Local Git history, MIT/CLA/notices, CI definitions and local audits, old native/source archives, clean alpha.7 test binary | Windows audit portability, actual hosted CI, current distribution artifacts/provenance, services/containers, alerts and capacity/soak |
| Open-source release | License/contribution/security policy foundations | Canonical public repository, live tested security intake, maintainer/recovery/signing identities, independent reviews, pilots and publication approval |

All **G0–G8** gates remain open. No completion percentage or remaining calendar estimate is justified by counting features/tests. Both Windows/Tailscale and Linux Docker remain required; MIT/CLA decisions remain established.

## 2. Verification and artifact identity

### Fresh in this review

| Check | Result |
| --- | --- |
| Git/status | Clean HEAD and branch above |
| Frontend units/state | `npm --prefix web test`: **112/112 passed**, zero failed/skipped |
| Svelte/type checking | **0 errors, 0 warnings** |
| CI browser-step audit | Plain audit and `--self-test` passed on local macOS; these are stubbed Linux-shell checks, not hosted or Windows execution |
| Unicode private-report probe | Valid 4,000-character ASCII report succeeds; valid CJK/emoji reports fail with 400 after successful edit and preview (R10-01) |
| Audit-only disclosure probe | Private queue returns 403, but general audit returns 200 with private report review metadata (R10-02) |
| Candidate identity | SHA-256 `fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f`; embedded source `2869db5…`, **`vcs.modified=false`** |
| Evidence inspection | Candidate hash matches private recovery and both rollback reports. HEAD changes only five evidence files from the tested implementation |
| Archives | No alpha.7 distribution ZIPs. All seven existing alpha.6 archive hashes remain unchanged |

The two diagnostic cases ran through a temporary Go overlay at `/private/tmp/brrp-p9-review-nr89mkh5/overlay.json`, without changing repository source. The denial assertions failed as expected (exit 1, API 2.469s). The temporary fixture is not a maintained regression; the reproduction steps below are sufficient to recreate it. The existing module cache `/Users/cameronamer/go/pkg/mod` worked with `GOPROXY=off`; the old temporary module cache lacked needed dependencies.

### P9 recorded results inspected, not rerun here

- Full uncached race suite: 10 packages passed; API **966.515s** with a 25-minute package timeout.
- Browser: **13/13 default-off**, **18/18 opted-in**, against the recorded binary. The source has **five** directs journeys; the ledger's “six” is a typo.
- Community restore **23/23**, private restore **85/85**, alpha.5 upgrade/rollback **57/57**, alpha.3 **55/55**; no failed recorded checks.
- Frontend build identity **`6b00f391da81a4bc`**, matching the current build file; vet/format/static input/notices checks recorded as passing.

See [P9 evidence](p9-checkpoint/verification.md). These results establish substantial local coverage. They do not establish every user journey, a distribution consumer, cross-browser/device behavior or host acceptance.

## 3. Findings to repair before expanding private evidence

### R10-01 — P2, reproduced: valid Unicode messages cannot be reported

`privateEvidenceForMessage` in `internal/api/private_reports.go:216` uses full content plus a NUL and edit timestamp as `preview.version`. Submission rejects versions over **8,000 bytes** at `:267`, even though messages accept up to 4,000 Unicode characters.

Fresh controls: ASCII edit/preview/report = **200/200/201**, version 4,031 bytes; CJK = **200/200/400**, 12,031 bytes; emoji = **200/200/400**, 16,031 bytes. The error incorrectly tells the reporter the preview is stale.

**Acceptance:** use a bounded canonical digest/version tied to the authoritative source and its state; retain transaction-time equality and read checks. Test full-length ASCII/CJK/emoji, edit conflicts, source substitution, malformed tokens and zero report side effects on denial. Do not solve this by lowering supported message length or trusting client evidence text.

### R10-02 — P1, reproduced: private review metadata reaches the general audit audience

`reviewPrivateEvidence` calls `auditTx` with `private_report.*` (`private_reports.go:562`). `auditLogs` (`internal/api/admin.go:737`) selects these records without private-evidence policy filtering.

A fixture account holding only `audit.read` receives **403** from the private report queue and **200** from the general audit with the reviewer username, report ID, action and timestamp. **No message text or explanation was exposed in this probe.** The metadata disclosure still contradicts ADR 0013's restricted-audit promise.

**Acceptance:** restrict private review audit records to the private-evidence policy, including reads, pagination, revocation and retention. Preserve atomic audit failure rollback. Filter before limits/cursors if using a shared store, or migrate to a restricted store; do not merely hide rows in the client or stop recording reviews. Test general audit-only, community moderator, authorized reviewer and revoked reviewer with positive controls.

### R10-03 — P2, source-confirmed: changed-preview recovery cannot finish

`web/src/lib/api.ts:26–28` discards structured error fields and throws only message/status. `ReportPrivateMessage.svelte:87–97` expects `failure.preview`, so the 409's refreshed preview never arrives. Independently, the component sets `changed=true` when a preview is present, but has no explicit acknowledgement/reset control; `submit` and the button remain disabled.

The backend edit-conflict test does not exercise this UI. Current behavior requires closing the dialog and loses the explanation, contrary to the promised re-preview flow.

**Acceptance:** typed/validated structured error handling or a safely re-fetched current preview; keep same-account explanation; display the changed text and require an explicit new confirmation that can actually submit. Preserve account/authority fencing and prevent automatic consent. Add a real rendered edit-between-preview-and-submit journey plus missing/malformed payload and repeated-edit controls.

### R10-04 — P2, source-confirmed: reviewer queue cannot refresh after authority changes

`admin/PrivateEvidence.svelte:29–30` captures account/generation once. Every subsequent `load` uses those constants (`:59`), even when its effect responds to a newer same-account generation. Responses become permanently inadmissible and loading can remain stuck; Refresh cannot repair it. Review mutation success/failure paths also need consistent request identity checks.

**Acceptance:** capture fresh identity per request, clear/withhold evidence while unverified, and resume admitted reads when the account retains the capability. Test actual reviewer rendering, same-account unrelated role/contact changes, revocation/regrant, delayed list/mutation completion, account switch and pagination. Existing P9 browser reporting coverage does not render the reviewer panel.

### R10-05 — P1 CI readiness, source-confirmed: Linux shell audit runs in the Windows matrix

The original `tee` bug is repaired. The new audit runs at `.github/workflows/check.yml:53–54` in the Ubuntu/Windows/macOS matrix but `scripts/check-browser-steps.mjs:222–225` hardcodes `/bin/sh` and a colon-separated PATH. Those assumptions do not hold for native Windows Node.

**Acceptance:** run the Linux browser-step execution audit in a required Linux job, or implement and verify real Windows portability. Keep the Windows application build/test matrix intact. Do not turn an unavailable shell into a passing skip. Local macOS audit success does not prove Windows execution.

### R10-06 — P2, source-confirmed: request badge excludes an actionable request

`outstandingIncomingRequests` (`internal/api/direct.go:181`) filters on the **sender's** `allow_requests`. That preference controls whether the sender receives new incoming requests; disabling it does not withdraw their existing outgoing request, and the recipient can still accept it. The badge can show zero while the inbox contains an actionable request.

**Acceptance:** derive count/list/action eligibility from the same explicit contract. Test sender opt-out after sending, recipient policy changes, acceptance, withdrawal, blocking and unavailable accounts across sessions/reconnects. Opening the modal must not resolve a request.

### R10-07 — reporting contract and disclosure corrections

These are source/documentation mismatches to resolve deliberately:

- The dialog does not explicitly disclose the **reporter's identity** before submit, although it is retained. Explain it before confirmation.
- An authority change during a pending request displays “Nothing … is sent,” even if the request committed. Distinguish unsubmitted work from an outcome not yet confirmed; this is critical when report-and-block intentionally changes authority.
- “Kept until” must distinguish API expiry, delayed physical cleanup and retained backups; do not promise deletion of every copy on that date.
- `migration13.sql` says no conversation ID, while storage and reviewer JSON include it. Minimize the public evidence DTO and align the actual storage/disclosure contract; an ID is not permission to browse a DM.
- ADR 0013 conflates removal with physical erasure. Tombstoned removal retains the stable identity under Deleted member; physical erasure nulls FKs. Match ADR 0011 and actual behavior.
- ADR 0013's restore sentence is misleading: snapshot reviewer grants **are restored**; later revocations require reconciliation precisely because the backup cannot contain them.
- The private-evidence admin tab checks capability but not the private feature gate. Hide/clear it consistently when the feature is disabled; API refusal alone is not the advertised UI contract.
- Full private evidence capacity is routed through generic 500, rather than the promised actionable refusal. Return a defined bounded-capacity result with preserved reporter text and no partial quota/report state.

### R10-08 — evidence/qualification gaps

- Private recovery claims exact preserved expiry but checks only nonempty expiry (`upgrade-recovery-private.mjs:617`); it checks snapshot length rather than text equality (`:620`). Capture originals and compare exact values after restore.
- A passing revoked-session 401 assertion prints an earlier response's 200 in the diagnostic (`:720`). Correct the diagnostic and add a dated evidence note.
- Feature-off/on preservation is covered by an API test, not the always-enabled recovery fixture; fix the ledger attribution.
- Current README/master-plan/progress/deployment summaries still name alpha.6/schema12 or private reports as absent. Update them to alpha.7/schema13 and the actual qualification limits.
- Alpha.7 has a clean tested binary but **no distribution ZIPs or extracted alpha.7 package consumers**. That P9 checkpoint obligation remains unfinished.

## 4. Next execution plan — P10

| Package | Work | Required completion evidence |
| --- | --- | --- |
| P10-01 | Repair R10-01–07; strengthen exact recovery assertions and current documentation | Maintained backend/browser regressions for the specific failures, permitted controls, real reviewer rendering and CI placement |
| P10-02 | Record introduction-report contract and data model | Source-attempt identity, preview/confirmation, combined report-and-block semantics, separate block-only fallback, restricted audit, retry, deletion/expiry and resource limits explicit |
| P10-03 | Implement recipient-only introduction evidence and atomic report-and-block | No acceptance required; snapshot before clearing; current eligibility/generation; rollback of all effects on failure; safe receipt replay after source disappears |
| P10-04 | Add request report UI and typed reviewer evidence | Exact disclosure, retained explanation, separate source labels, safe block-only fallback, no stale previews or fabricated client evidence |
| P10-05 | Extend restoration and end-to-end coverage | Message and request reports coexist; exact text/expiry; blocks and pending state; post-snapshot reviewer revocation; cross-device races and account isolation |
| P10-06 | Build clean artifacts and close the checkpoint | Final source/race/browser/recovery results, immutable manifests, native/source extraction consumers and honest external remainder |

Do P10-01 before building on the flawed reporter/reviewer components. Introduction reporting is an extension of the existing private evidence boundary, not a second moderation system. Do not fold full DM push/activity, groups or screen sharing into this tranche.

### Introduction-reporting contract to resolve

Only the actual pending recipient may preview/report the still-current introduction. They must not have to accept contact first. Distinguish a request **attempt** from its reusable canonical pair/conversation ID: a later retry must not inherit a former request's confirmation or receipt.

Add an explicit **Report request and block** action alongside the existing block-only action. Accepted-message reporting remains unchanged; report-only for pending introductions is outside this bounded tranche. Confirm the exact server preview; do not recover already-erased introductions from client text. Save consented evidence and block/reject/clear the introduction in one authority-safe transaction, with restricted audit and correct invalidation. Any failure rolls back that combined mutation. Block-only remains available even when reporting is full/unavailable.

An acknowledged retry must return its own minimal receipt without repeating the report or unblocking/recreating a request, even though the successful combined action cleared the source. Do not assume the current message report's open-row deduplication solves this case. Share the private rate/storage limits across evidence kinds; do not double the allowance by adding an endpoint.

## 5. Subsequent milestones and gate status

After P10, implement a dedicated DM activity/notification milestone: persistent activity, account/conversation/device preferences, safe preview defaults, active/background/closed-PWA behavior, deduplication and dispatch-time current consent/block/access checks. Existing community push jobs require channel IDs and cannot safely become DM jobs by passing an empty ID.

Then continue consented groups and lifecycle/export; named threads and everyday UX/moderation; private calls/screen sharing after a measured architecture spike. Public project ownership, actual hosted CI, both deployment hosts, independent encrypted recovery, external alerting and load/soak should proceed in parallel when resources exist.

G0 decisions/environments, G1 privacy, G2 private integration, G3 community, G4 media, G5 operations/artifacts, G6 independent review/beta, G7 final candidate and G8 publication all remain open. The prescribed independent reviews, three-community/both-host 14-day beta and seven-day final-candidate observation remain required. No package count or local green suite substitutes for them.

## 6. Handoff

Use [the standalone P10 coding prompt](handoff-next-coder-review-10-2026-09-27.md). It contains enough context to work without earlier chats and distinguishes existing code from proposed interfaces. Preserve the P9 evidence and clean binary identity, add dated corrections, and stop at a verifiable P10 checkpoint with unfinished external obligations clearly listed.
