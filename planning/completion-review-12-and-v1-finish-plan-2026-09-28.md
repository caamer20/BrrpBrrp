# Completion review 12 and the path to v1.0.0

**Reviewed:** 28 September 2026.  
**Application source:** `6b7b30157f3bd9246c1dbba442c8f341f4c208af`, branch `codex/p9-private-reporting`.  
**Scope:** read the supplied P10 completion report, inspect the current implementation and distribution evidence, verify selected behavior, and prepare a complete implementation handoff. This review changes planning documents only.

## Follow-up verification of the repeated P10 report

The subsequent attachment `502330b6-614d-4a81-ab73-b8ab0c2d414c/Pasted text.txt` contains the same P10 recap as the earlier attachment, after whitespace normalization. It describes the same `6b7b301` checkpoint and supplies no newer implementation or acceptance result.

The follow-up inspection found:

- HEAD remains `6b7b30157f3bd9246c1dbba442c8f341f4c208af`, with no tracked changes. The only untracked files are this review and its companion handoff.
- App/schema/backup versions remain alpha.7 / 14 / 2. P10 is still the latest recorded checkpoint; no new G0–G8 acceptance evidence appeared.
- R12-01–05 remain unchanged in source: audit namespace mismatch, incomplete combined capacity, byte-based introduction limit, incomplete receipt intent binding, and missing request-evidence/receipt expiry handling.
- The Darwin and source archives were hashed again and still match the P10 manifest. This confirms their identity; it does not repair or qualify newer behavior.
- Windows/Tailscale, Docker runtime, independent review, beta and final-candidate observation remain unqualified. The environment still has no Git remote, Docker executable or PowerShell.

No tests were rerun in this follow-up because the tracked application and tests are unchanged. The original review's fresh test results and reproduced failures below remain explicitly attributed to that review. **No additional implementation progress is established by the repeated report.**

The existing full handoff is updated with this status. Its immediate instruction remains to implement M0, then continue through the remaining product and release milestones. A new documentation-only checkpoint would not close those implementation obligations.

## 1. Assessment

P10 delivered real functionality: reporting an incoming private request without accepting it, blocking its sender in the same transaction, account-owned operation receipts, request evidence in the reviewer interface, and clean native/source distribution archives. Its recovery evidence also covers more private state than P9.

The statement that every P10 obligation is finished is too strong. Three focused probes reproduced defects in audit privacy, shared evidence capacity and Unicode handling. Additional source inspection found unfinished receipt expiry, physical retention, retry UI and pagination behavior. These should be repaired before expanding private messaging.

The broader product is still an alpha. Its existing authorization, community chat, consented one-to-one messaging, recovery and packaging foundations are substantial. Consenting groups, named threads, unified activity, remaining moderation and everyday UX, private calls and screen sharing are still unfinished. Production qualification on **both Windows x64/Tailscale and Linux amd64 Docker** remains required. No G0–G8 release gate has passed.

There is no defensible overall completion percentage. Local test counts measure particular behaviors; they do not measure the fraction of the full v1 product that is complete. The original 158–261 engineering-day estimate is a historical whole-project estimate, not an estimate of remaining work.

### Two distinct finishing points

1. **Feature-complete release candidate:** all agreed v1 features implemented, integrated and tested; candidate artifacts reproducible and attributable; remaining external acceptance explicitly listed. Calling this a candidate requires feature completeness, not merely finishing the next repair tranche.
2. **Released v1.0.0:** the feature-complete product also passes both-host operations, independent review, real-community beta, final-candidate observation and approved publication. This is the final goal. Missing hosts, reviewers or elapsed observation time cannot be replaced by local green tests.

The accompanying [standalone handoff](handoff-v1-completion-review-12-2026-09-28.md) instructs the next coder to continue through the feasible milestones toward these finishing points, instead of stopping after another small reporting patch.

## 2. What is verified now

### Source and artifact identity

| Item | Current evidence |
| --- | --- |
| Working tree at audit start | Clean at `6b7b30157f3bd9246c1dbba442c8f341f4c208af` |
| Application version | `0.4.0-alpha.7` |
| Database / backup format | Schema **14**, backup format **2** |
| P10 packaged source | `976366148348cb7ea754bb5304c126503ae68639` |
| Packaged frontend | `b17ccfd6cd9325e2` |
| Native archives | All six exist and match the local `dist/releases/SHA256SUMS`; their executables embed the packaged source revision and `vcs.modified=false` |
| Source archive | All 388 entries match the packaged commit byte for byte |
| Darwin archive SHA-256 | `0d4e87f81a3a13c77c94d0dc8c088d6bcf63b02c083e3c21804a7e5a800521bf` |
| Extracted Darwin executable SHA-256 | `4dc6e2494f847124f4945ee624e9ae5815fb5a0c8d311aad32364e7c48d12d1d` |
| Source archive SHA-256 | `8a18084a613fc3c30e82d36f15206adb50ed2fe946de62b275a7f91273ded0df` |

The packaged commit predates the final CI timeout change. HEAD has the same application inputs, but its workflow uses a 40-minute race timeout; the source ZIP still contains the earlier 20-minute workflow. This is an attribution difference, not evidence that the packaged application is dirty. Preserve those historical artifacts.

### Checks performed in the original review 12

| Check | Result and scope |
| --- | --- |
| Frontend unit suite | **118/118 passed**, zero failed or skipped |
| Svelte check | **Zero errors, zero warnings** |
| Three temporary Go API probes | All three reproduced their intended defect, so the probe command correctly exited **1**; API duration **2.225s** |
| Archive/source inspection | Six native archive hashes checked; embedded revisions checked; source ZIP compared with its recorded commit |
| Source review | Request-report server/client, mixed reviewer queue, audit/maintenance, recovery fixtures, release evidence and master v1 scope |

The temporary probes were added through a Go overlay outside the repository. Their exact local reproduction command was:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test \
  -overlay /private/tmp/brrp-review12-96lj27ja/overlay.json \
  ./internal/api -run '^TestReview12' -count=1 -v
```

That temporary directory is a local diagnostic convenience, not a portable project dependency. The permanent regressions should be written in the existing test suites using the reproduction scenarios below.

### Recorded P10 results inspected, not rerun here

- Extracted-native community recovery: **23/23** checks.
- Extracted-native private recovery: **102/102** checks.
- Actual prior-release rollback: **57/57** checks, specifically **alpha.6/schema12 → schema14**.
- Extracted-native and separately built extracted-source package consumers: recorded passes.
- Browser suites: **13/13 default-off** and **21/21 private-enabled**, recorded passes.
- Vet, formatting and production build: recorded passes.
- Full race suite: recorded green across ten packages, API **1201.909s**, with an evidence-command ambiguity described below.

Sources: [P10 verification](p10-checkpoint/verification.md), [manifest](p10-checkpoint/manifest.json), and its three JSON recovery reports. No full race run, new browser run, physical-device test or real-host deployment was performed by this review.

## 3. Findings to close first

Priority P1 means repair before expanding/exposing the affected private feature. P2 means material correctness, retention or usability work required before the candidate. These labels are triage priorities, not a completed independent security assessment.

| ID / priority | Finding and evidence | Required closure |
| --- | --- | --- |
| **R12-01 / P1** | **Request-review metadata reaches ordinary auditors.** `private_request_reports.go:590` emits `private_request.*`; `admin.go:747,800` filters only `private_report.*`. A reproduced audit-only account received **403** for private evidence but **200** with reviewer identity, report ID, action and time in general audit. Evidence text was not exposed. The event was missing from restricted audit. Maintenance recognizes `request_evidence.*`, so actual request reviews also receive ordinary **90-day** retention instead of **400 days**. | One coherent classification across real event writers, both audit feeds and retention, including already-stored prefixes. Test a real reviewer action through both feeds and maintenance; do not seed an action that production never writes. |
| **R12-02 / P1** | **Shared evidence capacity excludes requests.** `private_reports.go:473` counts only `private_message_reports`. With 5,000 unexpired request reports, the next request report returned **201**, creating row **5,001**. | Enforce the combined unexpired message/request cap transactionally on both paths, including concurrent last-slot races. Refusal must leave evidence, quota, block, request and receipt unchanged. |
| **R12-03 / P2** | **Valid Unicode requests cannot always be reported.** `private_request_reports.go:281` compares byte length against 500. A valid 500-CJK-character introduction created with **201**, previewed with **200**, then failed reporting with **400**. | Use the same character contract as request creation; test empty, ASCII, multibyte and boundary inputs through real handlers. |
| **R12-04 / P2** | **Receipt replay does not bind the submitted destination/attempt completely.** At `private_request_reports.go:245–254`, intent is recomputed using the stored attempt and submitted reason, without comparing the URL conversation or submitted preview version. A reused key can acknowledge an old report while the newly selected request remains untouched. | Bind the caller's immutable full intent, reject different destinations or attempts, and retain receipt resolution before requiring a still-pending source. Cover different-pair and renewed-same-pair reuse. |
| **R12-05 / P2** | **Receipt and evidence retention are unfinished.** Initial success advertises evidence expiry at 90 days; replay advertises receipt expiry at 30 days. Expired receipts are treated as missing, while their unique keys remain, allowing a later fresh attempt to reach a SQL conflict instead of a defined expired outcome. Maintenance does not reclaim request evidence or compact command receipts. | Distinguish evidence expiry from receipt validity; return a stable terminal outcome after expiry; reclaim expired request text in bounded batches. Preserve a minimal spent-key guard or equivalent enforceable protocol so cleanup can never re-execute old work. Include account lifecycle and backup reconciliation. |
| **R12-06 / P2** | **The request-report dialog can become inert.** `ReportRequest.svelte:94–169` sets `submitted` permanently; definite 400/409/429 refusals cannot be retried, transport uncertainty without authority change does not offer reconciliation, and errors from receipt checks are hidden in the uncertain branch. The key exists only for the mounted dialog. | Implement an explicit async state machine with visible failures, same-operation retry/reconciliation, refreshed-preview consent, account/session isolation and a stated reload recovery contract. Test the rendered component, not just admission predicates. |
| **R12-07 / P2** | **Mixed evidence pagination repeats exhausted sources.** `PrivateEvidence.svelte:94–109` refetches a source's first page when its cursor is exhausted but the other source still has data. Appending duplicate IDs can break keyed rendering. | Independent exhaustion tracking and source-qualified deduplication, or a unified typed server cursor. Prove complete, duplicate-free traversal of uneven mixed datasets. |
| **R12-08 / P2** | **Disclosure contradicts its audience and retention.** `ReportRequest.svelte:213–231` says “Nobody else sees it,” then describes reviewers, and omits explicit reporter identity and the backup caveat. | State exactly which text/identities/explanation are saved, who can read them, the combined block/reject effect, expiry versus cleanup, and what backups retain. Keep ordinary Block available. |
| **R12-09 / P2** | **Recovery qualification skips important current history.** P10 rollback covers schema12→14, not a real schema13 installation with private evidence. Post-snapshot security reconciliation covers block and deactivation, not reviewer-grant revocation. | Upgrade from a preserved real schema13 executable with existing reports/review state, then qualify current schema14→next candidate. Revoke a reviewer after a snapshot, demonstrate the old grant on isolated restore, reconcile before cutover, and prove denial. |
| **R12-10 / P2** | **Evidence and current status drift.** The full-race ledger omits its effective timeout; Makefile/runbook still use 20m while CI uses 40m. The durable manifest lists only Darwin and source, although six native archives exist. Progress header says schema12; ADR0014 says alpha8 while the app is alpha7/schema14. | Align current entry points and status, preserve historical results, record every distributed artifact hash, and capture the exact final command/environment/exit result. Correct ADR wording that receipts “outlive” evidence when the declared windows are 30 and 90 days. |

R12-01–03 are dynamically reproduced. R12-04–08 are source-confirmed defects; their permanent behavioral reproductions are work for the next tranche. R12-09–10 are evidence/qualification gaps.

### Tests that currently overstate their coverage

- The later-acceptance test uses the **sender**, who was already unauthorized. Test the recipient after report-and-block wins.
- Atomic rollback injection currently fails at the first evidence insert. Exercise later block, request clearing, receipt, audit and commit boundaries.
- Renewal testing uses an old preview with a new key; also test an old key with a fresh attempt.
- The retention fixture seeds `request_evidence.reported`, while the review handler emits `private_request.resolved`/`private_request.open`.
- The browser request-report journey always expects receipt reconciliation even though a valid POST response can win the race. Deliberately establish the ordering, or accept and verify both valid outcomes.
- The held-review-PATCH journey is useful, but must wait for the refresh response if claiming that refresh completed before the mutation was released.

## 4. Full implementation sequence

Use the [master v1 plan](../docs/v1-implementation-plan.md) for requirement IDs, approved contracts and G0–G8 acceptance. Current ADRs and implementation supersede its historical descriptions of what used to be absent. The sequence below updates execution order; it does not discard requirements.

| Milestone | Deliverable | Exit evidence / dependency |
| --- | --- | --- |
| **M0 — Close private-reporting defects** | R12-01–10, coherent policy helpers, permanent meaningful regressions, current recovery fixtures and accurate release evidence | Reproductions fail before and pass after repair; current and prior data preserved; focused rendered tests pass. Start here. |
| **M1 — Durable activity and DM notifications** | Account-owned activity, stable event identity, read/acknowledgment distinctions, preference precedence, private defaults, mute/quiet behavior, dispatch authorization, tab/device deduplication and actionable diagnostics | Active/background/closed-PWA matrix; offline and push-denied activity works; no delayed disclosure after block, deletion or access loss. Integrate later group/thread sources through this service. |
| **M2 — Consenting groups and lifecycle** | Explicit invitation acceptance; ten-member contract; owner transfer, leave/remove and history disclosure; all-surface revocation; remaining export/retention/account lifecycle | Group races, retained history policy, private files/search/events/media/push denial and bounded authorized export. Reuse the shared conversation pipeline. |
| **M3 — Complete discussion and daily UX** | Named threads and legacy promotion; follow/read/archive/lock; moderation assignment/timeouts/invites; safe formatting, member context, upload recovery, accessible responsive navigation | Complete 1,000-reply navigation; authoritative moderation on every path; keyboard, error/recovery and mobile journeys; group/thread activity integrated. |
| **M4 — Media completion** | Measured architecture spike; private calls and listen-only; distinct microphone/screen-video/screen-audio publications; desktop sharing/mobile viewing; lifecycle, limits and diagnostics | Server-enforced revocation, actual direct/relay qualification, sustained physical calls and capture cycles. Begin the spike early in parallel after M0. |
| **M5 — Operations and public-project readiness** | Both-host installation/update/rollback; encrypted independent backup and clean-host restore; alerts, redacted metrics, capacity; hosted CI; release identities, signing, complete manifests and public-project ownership | Real Windows/Tailscale and Linux Docker evidence; measured recovery/load/soak; extracted final-artifact consumers. Start provisioning early alongside feature work. |
| **M6 — Independent acceptance and beta** | Independent security/privacy and human accessibility review, newcomer/contributor rehearsals, full-product beta | G1–G5 and reviews before beta; 14 clean consecutive days in three consenting communities spanning both deployments, with logged incidents and required journeys. |
| **M7 — Candidate and v1 release** | Freeze final artifacts, finish compatibility and release notes, observe candidate, obtain publication approval, publish and verify | Seven consecutive candidate days on both deployments, clean recovery/rollback, exact final bytes verified, G7 accepted, then explicit approval and G8 publication/support handoff. |

The proposed capacity remains a **qualification target**, not current support: 250 accounts, 50 active people, 100 sockets/five per account, ten-member groups, 20 media participants/ten per room, one share per room/two per server, one million messages and 20 GiB attachments. Do not raise current enforced limits merely to match this table. Freeze and measure the full budgets in master-plan section 10.

The proposed recovery objective remains RPO ≤24 hours and RTO ≤60 minutes on the reference dataset, including operator response, fetching/decrypting independent backup, security reconciliation and verification. Both required deployments need a two-hour peak and a 24-hour mixed soak. These measurements cannot be inferred from the current local recovery counts.

## 5. Gate and resource status

| Gate | What remains necessary |
| --- | --- |
| G0 | Accepted scope/support profile, named owners, actual environments, release/security identities and unresolved decisions |
| G1 | Complete cross-surface privacy evidence, recovery coverage and independent foundation review; fix R12 privacy recurrence |
| G2 | Complete direct/group integration, lifecycle and blocking/reporting behavior |
| G3 | Threads, full activity/preferences, moderation and member UX |
| G4 | Private calls and screen sharing with browser/network/device qualification |
| G5 | Both host lifecycles, independent recovery, monitoring, measured capacity and verifiable artifacts |
| G6 | Independent review, newcomer acceptance and full-product beta |
| G7 | Final-byte qualification and seven-day candidate observation |
| G8 | Explicitly approved publication, verified downloads and maintenance ownership |

This workspace currently has no configured Git remote, Docker executable or PowerShell. Host access, physical devices, independent reviewers, signing/release credentials, pilot communities and observation time remain external inputs. They should be requested with a precise ready-to-run procedure while independent implementation continues. Local cross-compilation is useful, but does not qualify the six platforms it can build.

Keep camera video, federation, native mobile apps, E2EE, multiple communities per process, bot marketplaces, enterprise SSO and clustering outside the current v1 critical path unless the owner explicitly changes scope. Preserve the existing MIT/CLA decision; do not repeatedly reopen settled licensing questions.

## 6. Recommended next action

Give the next coder the complete [handoff prompt](handoff-v1-completion-review-12-2026-09-28.md). It authorizes implementation through M0–M7 wherever the environment and existing approvals permit, requires useful feature progress after M0, and defines an honest checkpoint if external acceptance prevents release.

The next report should contain: closed finding IDs with behavioral evidence, newly completed requirements, exact candidate identities, tests actually run, remaining release gates, external inputs with ready procedures, and the next executable task. A checked task list alone is insufficient to declare either P10 or v1 complete.
