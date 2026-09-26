# BrrpBrrp — Coder Handoff After Review 4

Copy the following prompt into the coding agent with this workspace attached.

~~~text
Continue implementing BrrpBrrp toward a self-hosted, open-source v1.0.0.

Workspace: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp

Read applicable AGENTS.md instructions, then:
1. planning/completion-review-4-and-next-steps-2026-09-25.md
2. docs/completion-remediation-2026-09-24.md
3. docs/v1-progress.md
4. docs/v1-implementation-plan.md and relevant ADRs.

Use review 4 as the latest assessment. Implement its P4 packages; do not spend the whole task writing another plan. Preserve existing work and use focused, maintainable changes. Both Windows/Tailscale and Linux Docker are required. MIT/CLA is already the recorded direction.

First close these four reproduced failures:

P4-01 / R4-01: channelAudienceIDs/channelAudience read channel, access, roles and ownership separately, then read the revision. Revocation between those reads can stamp a stale audience with the current revision and deliver restricted content on a real WebSocket. Establish one coherent authorization snapshot or a validated equivalent. Cover committed changes whose hub invalidation is delayed, and explicitly test dequeue/write admission. Do not hold a global database lock across network writes.

P4-02 / R4-02: admitted(generation, session = this.sessionVersion) allows callers such as moveChannel to substitute the current session after an await. A delayed old-account response can replace a new account's channel list. Require identity captured before the request, audit success/error/finally and follow-up callbacks, and test account switching with unchanged capabilities/generation, navigation, revocation and permitted recovery.

P4-03 / R4-03: after a valid independent backup copy is published but completion status is lost, retries reject the existing filename forever. Reconcile only an independently verified matching regular archive. Handle conflicting/corrupt/non-regular/partial targets safely. Test publication, status persistence, restart and retention boundaries.

P4-04 / R4-04: retrying an outstanding copy returns before making a new local snapshot, even after the regular backup interval expires. Separate snapshot and copy-retry clocks, keep fresh local snapshots during copy outages, bound pending generations/storage and preserve the verified fallback. Health must report latest failure, newest local snapshot and verified independent fallback with its own snapshot age/coverage.

Convert the review's scenarios into permanent regressions that fail before the fix and include permitted controls. Temporary diagnostic probes may remain under /private/tmp/brrpbrrp-review4-4vd89phc; reconstruct them from the report if absent.

Preserve the passing baseline: 73 frontend tests; zero Svelte errors/warnings; formatting/build/vet; full uncached Go race suite; five unmodified browser journeys including a non-cooperative RTP sender; 53 maintained rollback checks; notices, static Docker/source-package checks and current executable package smoke. Choose checks based on affected risks, then run the relevant final baseline.

Next execute P4-05 host qualification where actual access exists, and P4-06 evidence reconciliation and foundation review preparation. Record inaccessible hosts, signing identities or independent reviewers explicitly while completing all unblocked local work. Do not substitute cross-builds or local directories for real platform/off-host acceptance. Do not mark G0–G8 passed without their evidence.

Keep restricted/private features gated until their complete contract is verified. Prepare WP-09's shared conversation/migration/idempotency/outbox design as the next product package; do not bolt DMs onto the old model prematurely.

Update remediation/progress records accurately. Keep developer audit documents in planning/ to preserve native-package document validation. Do not publish releases, deploy to live services or overwrite user data as part of verification without authorization.

Finish with implemented changes, test results, unresolved external dependencies, gate status and the next executable package. Proceed from inspection into implementation.
~~~
