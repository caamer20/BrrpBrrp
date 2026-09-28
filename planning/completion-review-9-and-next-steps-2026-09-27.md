# BrrpBrrp — Review 9: P8 verification and the next implementation tranche

**Date:** 27 September 2026, America/Los_Angeles  
**Reviewed HEAD:** `876260ab1c01313efd3358459b4178c010ee2f45` — clean at review start  
**Implementation:** `606cdb120326507c280cf0c1fbc89c85b44598cd`  
**Current app:** `0.4.0-alpha.6` · schema **12** · backup format **2**  
**Scope:** evaluate the other coder's completion report against source, artifacts and evidence; establish next steps and a standalone handoff. No application/workflow changes or deployments in this review.

## 1. Assessment

**P8 delivered substantial, verified local progress. The original three review-8 defects are repaired. The statement that everything is complete needs qualification: browser CI can conceal test failures, artifact provenance is not a clean-commit build, and some lifecycle/recovery controls remain missing.**

There is no reason to redo the shared conversation migration or the original attachment/draft fixes. The next coder should close the concrete follow-ups below, then deliver a bounded private-reporting and request-discovery milestone. Actual hosted CI and both host qualifications remain parallel external work; their absence should not halt independent local development.

No G0–G8 release gate has passed. Alpha.6 packages now exist locally; alpha.5 is the prior packaged baseline. Neither is a published or production-approved release.

### What P8 achieved

| Area | Verified assessment |
| --- | --- |
| Attachment claim authority | `createMessage` checks current `message.upload` after receipt replay and before new receipt/message/file claims. Permanent regressions cover community and direct paths, denied side effects, permission restoration, same-key retries and deletion-safe replay |
| Initial DM-view mention policy | The original header-dependent loading defect is fixed. The store loads policy independently of a mounted community header; deferred responses are fenced |
| Request introduction preservation | Same-account drafts survive unrelated contact/authority refreshes, unavailable targets and refused sends; explicit admitted success controls clearing |
| Browser coverage definitions | Both default-off and opted-in modes are defined, with labeled logs and a directs-project presence check. Failure propagation is still defective (R9-01) |
| Recovery | A maintained private recovery fixture covers meaningful consent, block, file, cursor, preference, receipt and removed-member state. Two controls need strengthening (R9-05) |
| Baseline supply | Checked-in archive digests and a fetch/stage helper fail when a baseline is absent or wrong; no silent stand-in rebuild |
| Local checkpoint | Full race and two-mode browser success are recorded; native/source packages and consumer evidence exist. All checked artifact hashes match recorded bytes, with provenance qualifications below |
| Package count bug | The prior blank-line normalization defect is repaired; the ledger's remaining count discrepancy is from later added files, not that old defect |

## 2. Fresh verification versus inherited results

### Executed or inspected in this review

| Check | Result |
| --- | --- |
| Git/source identity | Clean starting tree at `876260a`; implementation and evidence commits exist |
| `npm --prefix web test` | **105/105 passed**, no failed/skipped tests |
| `npm --prefix web run check` | **0 errors, 0 warnings** |
| Targeted P8 attachment regressions | `go test ./internal/api -run 'Test(StagedAttachmentClaim\|AcknowledgedAttachmentSend\|DeletedAttachmentSend\|AttachmentClaimKeeps)' -count=1` — **passed**, API 54.963s. This is a focused non-race run |
| P8 recovery report inspection | Community **23/23**, private **68/68**, alpha.5 rollback **57/57**, alpha.3 rollback **55/55**; no failed recorded assertions |
| Artifact/hash inspection | All seven alpha.6 archive hashes and all twelve pinned alpha.3/alpha.5 native archive hashes match the files inspected |
| Source and embedded build metadata | Packaged executable is the tested `68c2458f…` binary, but it records a dirty build of `606cdb1`; source ZIP also has the differences listed in R9-03 |
| CI failure propagation | Source review plus local shell controls confirm the `tee` failure-masking issue |
| Client lifecycle | Original repairs verified in source/tests; new cross-device preference finding is source-confirmed, not newly browser-reproduced |

The focused Go command initially could not set up dependencies using the old `/private/tmp/brrpbrrp-go-mod` path because required downloads were unavailable. Reusing the existing `/Users/cameronamer/go/pkg/mod` cache with `GOPROXY=off` and a writable temporary build cache succeeded. This was an environment/cache issue, not a failing application test. Future agents should inspect actual caches instead of assuming old temporary paths are complete.

### Recorded by P8, not rerun here

- Full uncached race suite: all packages passed; API **887.109s**.
- Packaged-binary browser runs: **13/13 default-off**, **16/16 opted-in**.
- Vet, frontend production build/format, notices and static Docker/source checks passed.
- Extracted native and clean source consumers passed; frontend identity **`8263e263579a2b47`**.

The source and artifact corroboration supports carrying these forward as historical evidence. This review does not relabel them as fresh executions or independent acceptance. See [P8 verification](p8-checkpoint/verification.md) and its JSON reports.

## 3. New findings and evidence corrections

### R9-01 — P1: failed browser tests can leave CI green

Both browser steps in `.github/workflows/check.yml:82,90` pipe `npm run test:e2e` into `tee` without an explicit shell or `pipefail`. The subsequent `[directs]` grep only establishes that project text appeared; a failed direct test can print the same text.

GitHub documents an unspecified Linux shell as `bash -e`, while explicit `shell: bash` includes `-eo pipefail`. A local control returns 0 for `bash -e -c 'false | tee /dev/null'` and 1 with pipefail. Therefore a successful log writer can mask the test process failure and allow the required browser job to pass. [GitHub shell documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idstepsshell).

**Required repair:** preserve the test process failure for both modes, retain useful artifacts even on early failure, and prove that a failing test which prints `[directs]` still fails the step. Test pass/fail controls with the workflow's actual shell/runner mechanism; YAML parsing and grep alone are insufficient. Packaging must remain dependent on the browser result.

### R9-02 — P2: another device's unmute is not refreshed while remaining in a DM

P8 fixed initial loading. A separate stale-cache sequence remains:

1. Tab A loads a channel preference as muted, then stays in a DM.
2. Device B unmutes that channel.
3. `internal/api/channel_notifications.go:118–127` commits the change without an account preference invalidation.
4. `workspace.svelte.ts:533` fetches only when `channelNotificationsReady` is false. Same-revision reconnect leaves it true; focus refresh remains in `ChannelNotifications.svelte:19–24`, which is absent in a DM.
5. Tab A discards new otherwise eligible mention events using its stale muted entry (`workspace.svelte.ts:1285–1287`).

**Source-confirmed; not browser-reproduced in this review.** Move refresh/invalidation ownership fully to account state. Cover another session's mute/unmute, same-revision reconnect and focus while staying in a DM, stale GET versus newer PUT, account switch and denied preference loading. Keep server dispatch checks and fail-closed uncertainty. Avoid per-message preference fetches or using a global authority revision for every personal setting without justification.

### R9-03 — P2 release evidence: tested artifacts lack clean-commit provenance

- Packaged Darwin executable: `68c2458fbd9fcb248d691d6dcae24fe15d4a5ac0b527220a7634d3274f9d5bc8`; matches the private-recovery and rollback candidate hashes.
- Embedded source: `606cdb120326507c280cf0c1fbc89c85b44598cd`, **`vcs.modified=true`**.
- Source archive: `623e4d303ea815bcec6bae7db6caf7f18d0315ec0e59b4992a2b7b19d6b87ee0`.
- Against `606cdb1`, the source ZIP differs in `Makefile`, `scripts/release.go`, production-readiness documentation, the alpha.6 release document and progress ledger. Against `876260a`, only two progress-ledger header lines differ. Application inputs align; this is not evidence of an untested application rewrite.
- The so-called evidence commit also changes packaging inputs and the Makefile; it is not literally documentation/evidence only.

**Required correction:** describe the P8 artifacts as tested local bytes from a modified checkout. Preserve them. After the next implementation is frozen, build from one clean committed source and bind source/frontend/executable/archive identities to the actual consumer/browser/recovery results. Build evidence outside the source checkout or in ignored output, then add an evidence commit afterward. Do not rebuild historical alpha.5/P8 outputs in place to make metadata look cleaner.

### R9-04 — P2 evidence accuracy: mislabeled hashes and stale file count

`planning/p8-checkpoint/verification.md` labels `168e89422…` and `3e08497f…` as old executable hashes. They are **rollback backup ZIP hashes**, as the JSON field `archives.rollback.sha256` shows. Correct old executable hashes are:

| Baseline | `baseline.binary_sha256` |
| --- | --- |
| Alpha.5 Darwin/amd64 | `79d90400e39ce2b08bcbfd15af46b4c95436c2977cbe3117b5063423b2960278` |
| Alpha.3 Darwin/amd64 | `918c4c873b4d848aaaeeaf30a187db985935472ab8b27d74c1472ab13e365099` |

The final source archive contains **370 entries**; the recorded 367 describes the list before three later release-input additions. Add a dated correction rather than silently changing original execution records. Reconcile current progress rows too: some still describe R8-01/02 as remaining after the top-level section marks them fixed.

### R9-05 — P2 coverage: private recovery has two weak controls

The fixture checks that restored push-subscription count is zero without first seeding a subscription. It cannot prove removal of a nonempty set. It demonstrates restoring an older state and reapplying a post-snapshot block, but does not reapply the deactivation it also deliberately lost.

**Acceptance:** seed a real fixture subscription and assert its pre-backup presence, then its absence after restore alongside old-session rejection; use a disposable local/stub endpoint without sending real push. Reapply both selected post-snapshot block and deactivation, verify blocked writes and deactivated account denial, and record reconciliation before hypothetical traffic cutover. These are fixture gaps, not proof the restore implementation retains subscriptions or restores access incorrectly.

### R9-06 — P3 coverage: switch accounts while a request draft is still unsent

The P8 request-draft journey switches accounts after successful submission has already cleared its text. Source ownership clearing looks correct, but that sequence cannot prove isolation of an unsent draft. Add a rendered case with unsent text and a delayed response crossing account exit; account B must see neither the text nor a stale mutation result. This is a regression gap, not a reproduced disclosure.

## 4. Next tranche: P9 private text reporting and request discovery

The following scope is a proposed implementation contract for an opt-in alpha slice. It does not ratify broad privacy exposure, change published support, or complete all of WP-14/15. Use [the standalone P9 coder prompt](handoff-next-coder-review-9-2026-09-27.md) for execution detail.

| Package | Deliverable | Exit criteria |
| --- | --- | --- |
| P9-01 | Close R9-01/02/04/05/06 and prepare provenance discipline | CI failure controls, rendered cross-device preferences and unsent-account-switch regressions, meaningful recovery controls, dated evidence corrections |
| P9-02 | Record private-report/request/notification contract in an ADR | Exact evidence, grants, preview, expiry, limits, lifecycle and feature-gate behavior defined before schema/UI work |
| P9-03 | Private text report storage and authorization | One selected accepted-DM message; read-authorized reporting including blocked history; distinct private-evidence capability; durable limits/idempotent receipts; existing community behavior preserved |
| P9-04 | Reporter disclosure and evidence-only review UI | Server-confirmed preview; edit-before-confirm refusal/repreview; evidence-only review without DM access; revocation-safe accessible queue and nonpunitive resolve/reopen |
| P9-05 | Persistent incoming-request discovery | Server-derived outstanding incoming count; correct events/reconnect/other-device updates, account isolation and keyboard/mobile navigation; no introductions fetched into a closed badge |
| P9-06 | Recovery, final verification and clean checkpoint | New schema/report/request behavior restored; current regression matrix green; clean-source artifacts and exact-byte consumer/recovery evidence with limitations |

P9-01 backend/CI and client work can run independently with explicit file ownership. P9-02 precedes new schema and permission work. P9-03/04 share a contract; P9-05 can run separately after account-summary/event ownership is agreed. Freeze integrated changes before final expensive verification.

### Private text reporting: precise boundary

- One selected message's bounded text, author/reporter identity and explanation; no attachment bytes/URLs/thumbnails, surrounding messages, replies, previews or automatic conversation export.
- Server-fetched preview must match the eventual saved evidence. A changed source requires explicit re-preview; client-supplied arbitrary text cannot become authentic server evidence.
- Submission uses current accepted **read** access, including blocked read-only history; global owner/admin status does not substitute for membership.
- A distinct capability (proposed name `report.review_private`) permits access only to voluntarily submitted evidence. Owners already hold valid capabilities; use explicit delegation for other reviewers rather than automatically granting every existing moderator. Document role defaults and the recipient disclosure. Ordinary community reports retain their current read-plus-`message.delete_any` policy.
- Preserve 90-day expiry from report creation; resolve/reopen does not extend it. API expiry, bounded disk cleanup, source/account deletion and backup retention need explicit tests.
- Use durable private submission/storage bounds and account-scoped retry deduplication. Record chosen limits and retry behavior; do not describe the current in-memory community limiter as durable.
- Keep private text/reasons/membership out of the general audit feed. Review mutations require atomic restricted audit metadata with an explicit failure policy.

This slice deliberately **does not report pending introductions**. Blocking/rejecting a request erases its introduction; future request-harassment reporting needs a second server-verified evidence source and snapshot-before-block semantics. Do not require acceptance merely to report a harasser, preserve rejected text covertly, or reconstruct it from untrusted client text. Track this as a required follow-up before broad private-message pilots.

### Request and interruption policy for this slice

The badge is the number of **outstanding incoming requests**, not an unseen-message/activity counter. It survives opening the modal and disappears only when the underlying request is accepted, rejected, withdrawn, blocked or becomes ineligible. Counts must be filtered by account/current authority before return; updates are invalidation hints followed by an admitted fetch.

Expose an accessible count and a direct way to open Requests on desktop/mobile. Keep hidden accepted conversations hidden and keep unread message semantics unchanged. The implemented policy remains **request badge plus existing DM unread state; no new DM OS notification, push, sound or ringing**. Explain this honestly. Do not route DMs into the community push job with an empty channel ID.

## 5. Following milestones and external track

1. **Private contact safety/notifications:** server-verified introduction reporting, then a dedicated DM activity/notification milestone with stable inbox, preference precedence, active/background/closed-app behavior, deduplication and current-access/block/preview protection. Full WP-14 also depends on groups/threads; label partial delivery honestly.
2. **Consenting groups and lifecycle/export:** invitations, ten-member bound, owner/leave/remove/history policy, shared-group blocks, consistent delivery revocation and recoverable membership state.
3. **Named threads and everyday UX:** durable thread identity/follow/read/archive/lock, full bounded 1,000-reply traversal, composer/uploads, moderation/timeouts/invitations and keyboard/mobile/accessibility journeys.
4. **Media:** measured architecture spike in parallel, then private calls/screen sharing and physical browser/network/relay qualification. Synthetic RTP remains transport evidence.
5. **Operations:** establish canonical repository/maintainer/security/signing identities and approved immutable archive supply; execute corrected hosted CI. Qualify both Windows/Tailscale and Docker lifecycle, encrypted independent recovery, configuration/key custody, delivered alerts, reference-load/soak and recovery objectives.
6. **Release:** independent security/accessibility review, newcomer installation/recovery, 14 consecutive beta days across at least three communities and both hosts, frozen distributed candidate with seven-day final observation, explicit publication approval and maintenance handoff.

Both host targets and MIT/CLA decisions are already established. No publication, live migration or new quota allowance is implied by this handoff. Missing external credentials should be reported specifically while local work continues.

## 6. Deliverable for the next coder

Use [the extensive standalone prompt](handoff-next-coder-review-9-2026-09-27.md). It replaces the review-8 prompt as the next execution assignment; review 8 remains historical context. Require a per-package completion table, regression evidence, exact identities and external limitations. Stop at a coherent P9 checkpoint, not another plan and not an unsupported v1 declaration.
