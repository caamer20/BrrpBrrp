# BrrpBrrp — Coder Handoff After Review 5

Copy this prompt into the coding agent with the workspace attached.

~~~text
Continue implementing BrrpBrrp toward a production-ready, self-hosted, open-source v1.0.0.

Workspace: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp

Read applicable AGENTS.md instructions, then:
- planning/completion-review-5-and-next-steps-2026-09-25.md
- docs/completion-remediation-2026-09-24.md
- docs/v1-progress.md
- docs/v1-implementation-plan.md and relevant ADRs.

Use review 5 as the current assessment. Implement P5-01–P5-04, then complete unblocked P5-05 work and prepare WP-09 under P5-06. Proceed into implementation; do not spend the task writing another plan. Preserve existing work. Windows/Tailscale and Linux Docker are both required; MIT/CLA is already the recorded direction.

First address these reproduced failures:

R5-01: coherentAudience fixes mixed snapshots, but Client.Admitted only checks the revision the connection observed. A private event queued before revocation can be admitted to a real socket after the database commit if invalidation is delayed. Design and implement a linearizable commit/admission boundary. Do not hold a global database lock across network writes. Test delayed invalidation, queue/dequeue, grants/roles/visibility/account/session changes and unaffected/regrant controls.

R5-02: publishVoiceChannel broadcasts public voice presence to every socket, including accounts without channel.read whose /state hides the channel. Apply coherent capability-aware authorization to public and private presence and its queued delivery.

R5-03: saveBackupStatus logs persistence failures without returning them. completeCopy and runBackup can still prune the old generation named by the durable recovery ledger and return success. Propagate/check persistence errors; prevent retention until required state is durable; test failure and restart around publication, status and pruning.

R5-04: copy repair silently succeeds and clears CopyError/counters when os.Stat of its local source fails. Distinguish missing/inaccessible sources and genuine no-op/success, preserve failure reporting and fallback identity, and define replacement-snapshot scheduling.

Use review 5's reproduction steps to add permanent regressions that fail before the fix and include permitted controls. Temporary probes may exist at /private/tmp/brrpbrrp-review5-probes; reconstruct them if absent. The socket scheduling barrier was diagnostic instrumentation, not production logic.

Preserve the verified progress: 77 frontend tests, zero Svelte errors/warnings, formatting/build/vet, full uncached Go race suite, source/Docker-input/notice checks, current executable smoke and 53 rollback checks.

The current browser result is five passed and one voice failure; the new rendered account-switch journey passes. Voice stalls at initial capture, and a minimal page also leaves getUserMedia pending. Diagnose the capture environment, then rerun all six journeys. Preserve actual inbound/outbound RTP assertions, non-cooperative sender and regrant; do not skip or weaken voice to claim green.

Complete remaining rendered narrow-role/failed-refresh/privacy cases. Update progress and remediation ledgers with accurate current and historical results. Keep developer audit documents in planning/ so native package validation remains intact.

Qualify actual Windows/Tailscale and Linux Docker hosts where accessible. Record missing hosts, signing infrastructure and independent reviewers explicitly while finishing unblocked local work. Do not equate local directories or cross-builds with off-host/platform acceptance. No release gate passes without its evidence.

Prepare WP-09's shared conversation, migration, idempotency-receipt, outbox and read-cursor design before DMs/groups. Keep restricted/private features gated until their contracts pass. Do not publish releases, deploy live services or overwrite user data without authorization.

Finish with implemented changes, test results, unresolved dependencies, gate status and the next executable package.
~~~
