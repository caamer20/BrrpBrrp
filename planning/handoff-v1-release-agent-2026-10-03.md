# Copy-ready prompt for the next implementing agent

Paste the block below into a new implementation session opened on the existing project. It points to the detailed review rather than requiring the user to paste the entire plan.

```text
Continue BrrpBrrp toward a fully qualified v1.0.0. Implement and verify the remaining work; do not stop at another plan or a version-number change.

Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Primary execution plan: planning/v1-release-execution-plan-2026-10-03.md
Review evidence: planning/v1-review-evidence-2026-10-03/README.md
Current implementation ledger: planning/v1-execution-ledger-2026-10-01.md
Normative release contract: docs/v1-implementation-plan.md, amended by the ledger's “Owner decisions — 2 October 2026.”

Read those files first, then inspect current source and git status. The reviewed baseline was branch codex/p9-private-reporting at 1e5a98f2689f6bcdebf37e8d572dddd20cfddd78, application 0.4.0-alpha.8, schema 25, backup format 2, INCLUDING the existing uncommitted changes listed in the execution plan. Preserve every unrelated edit. If HEAD has advanced, review the delta rather than reverting it. main was 212 commits behind; do not start from main or assume a clean worktree includes the pending candidate. No remote exists and the recorded owner decision is not to create one now.

Use the architect agent before a large plan, when an error repeats, and before declaring a long tranche complete. Use it only to review plans, repeated errors and finished work. Parallelize independent implementation with disjoint file ownership and one integrator; serialize shared authorization, database migration, protocol and central frontend-state changes.

Start with V1-00 through V1-04 in the plan:

1. Preserve and identify the actual working candidate; review/integrate the pending acceptance matrix/checker, thread-scale journey, RTL/focus and harness/CI changes. Reconcile stale current-status prose without falsifying historical artifact records.
2. Repair old named-thread links and pagination. NamedThreadPanel currently compares a message ID to a thread ID; the server's after cursor selects the last newer page rather than the nearest one. Add rendered early/mid-history and repeated-link regressions, contiguous-ID assertions, and a complete 1,000-reply traversal proof with bounded windows.
3. Fix upload storage admission. The real quota checks can both pass before either transaction inserts, allowing 16 committed bytes under a 10-byte quota; review evidence includes a deterministic scheduling-overlay reproduction. Enforce the cap at the serialized admission boundary, preserve durable file-before-row ordering and authorization rechecks, clean up every failure path, and distinguish actual disk exhaustion from file-too-large errors.
4. Repair source-package dependency closure and CI/harness portability. The source package currently omits required entry scripts, rehearsal-kit.mjs and the acceptance matrix. Verify an actual clean extraction. Fix Bash bodies using Windows's default shell, forward the intended browser channel, and add explicit restricted-channel enabled/disabled browser coverage.

Include the plan's export and operations subtranches. Export attachment accounting can overshoot 2,000, reaction inclusion is incorrectly tied to the attachment budget, and the byte cap applies only after full in-memory encoding; qualify pending-job bounds, durable publication, cleanup and chat responsiveness without weakening snapshot or revocable-download authority. Backup alerts currently age local job completion instead of the independently recoverable snapshot; synchronous webhook delivery can hold the maintenance path, failed delivery is deduplicated for six hours, and metrics have semantic/cost/Windows-counter gaps. Correct these before treating capacity/recovery monitoring as qualified.

Then execute V1-05 through V1-11: semantic requirement/journey evidence; route/event-driven outsider and revocation coverage; 1,000 logical-send fault acceptance; two-device read state; an actual two-build PWA update; 100 media share cycles; diagnose the recorded Firefox session intermittent; repair the capacity driver; finish controlled migration/disk/crash/recovery coverage; complete convergence review and a source-bound candidate sweep.

The capacity driver must be corrected before it supplies qualification evidence: it currently shares one cookie across all clients despite five sockets/account, counts initial handshakes as reconnect, may miss events arriving before HTTP responses, times failed searches, and reports a channel count it does not exercise. Test real admitted populations and outcomes, both event orders, actual outage/recovery, and sender/recipient/browser timings separately.

Preserve settled scope: 100 live/recoverable accounts; IPv4 media with embedded TURN only; VP8 fallback; emphasis/block quotes without lists; no camera video/federation/E2EE/service rewrite. Current code permits 20 media participants serverwide and 10 per room, which still needs qualification. Preserve consent, operator trust boundaries, commit/delivery authorization, retry identity, account-scoped drafts and migration/backup safety.

Keep private conversations and restricted channels default-off until the recorded independent-review entry/P1-resolution condition is actually met. Formal beta entry additionally requires all master-plan G1–G5 plus security/accessibility acceptance, verified recovery, documentation and consenting participants. Test the configuration that will ship before beta. Do not silently rescope required features or mark default-off testing as full-product acceptance.

The acceptance checker is a reference-integrity check, not proof of execution or semantic coverage. A named/skipped test does not pass a release obligation. Maintain separate implemented, locally tested, platform-qualified and approved statuses. Bind results to source/dirty identity, artifact hashes, toolchain/platform/browser, feature modes, exact command, result/skips, raw evidence and limitations. Never discard a failed run because a rerun passed.

Run focused tests for each fix, then a coherent required local sweep: frontend unit/check/format/build; Go vet/format/module checks, uncached tests and race coverage; declared browser/feature modes and scale/fault/cycle projects; recovery/rollback; clean source/native package consumption; current dependency security scans when available. Rebuild embedded frontend and the browser-test binary before browser verification. Environment-denied loopback binds are infrastructure failures: obtain appropriate permission for the same tests rather than weakening assertions. Do not change production code merely to accommodate the test sandbox.

Continue into V1-12 through V1-17 as prerequisites become available: actual Windows x64 and Linux amd64 Docker qualification, physical media/push/phone/network acceptance, independent security/accessibility reviews, measured capacity and encrypted off-host loss/recovery, genuine hosted CI/artifact provenance/signing, three-community fourteen-day beta, and seven-day final observation of the exact final bytes. Preserve the required thirty-minute baselines, two-hour peak and 24-hour mixed soak on EACH host. Cross-host latency requires verified clock synchronization/error bounds or a disclosed single-monotonic-clock receipt method. Correct alert evaluation cadence as well as delivery: the current hourly maintenance trigger cannot meet short detection windows.

No Windows/Docker hosts, physical devices/networks, independent reviewers, signing identity, remote/registry, off-host storage, pilot communities or alternate releaser were available in the recorded owner decisions. Prepare concrete runnable bundles now, continue independent local work, and request only a specific prerequisite when dependent work is ready. Do not repeat already answered scope/remote questions. Do not fabricate host tests, independent approvals, elapsed days, vulnerability scans or publication evidence. Do not contact others, create a remote, purchase services, schedule automations or publish artifacts without the required explicit authorization.

Honor the master plan's release reset rules: Severity 0/1 findings reset the affected beta clean window after verified remediation; executable/dependency/schema/protocol changes reset the seven-day final-candidate window. Qualify final stable-version bytes privately or repeat affected gates after rebuilding; an RC rename is not final-artifact qualification.

For each implementation tranche, update the execution ledger with the gap, change, exact checks/results, artifact/source identity, evidence limits, unresolved findings and next action. Report progress concisely while working. Ask the architect to review the completed tranche and address its findings. Avoid broad refactors or new features unrelated to the remaining v1 contract.

Do all authorized local work before stopping for unavailable external inputs. If those inputs ultimately prevent further progress, provide a precise checkpoint with completed packages, remaining gates, required owner/resource action and the exact continuation procedure. Do not declare v1.0.0 complete until G0–G8 are truly satisfied, publication is explicitly approved and verified, and maintenance ownership is accepted.
```
