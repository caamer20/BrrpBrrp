# BrrpBrrp — Coder Handoff After Review 6

Copy this prompt into a coding agent with the workspace attached.

~~~text
Continue implementing BrrpBrrp toward a production-ready, self-hosted, open-source v1.0.0.

Workspace: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp

Read applicable AGENTS.md instructions, then:
- planning/completion-review-6-and-next-steps-2026-09-25.md
- docs/completion-remediation-2026-09-24.md
- docs/v1-progress.md
- docs/v1-implementation-plan.md and relevant ADRs.

Use review 6 as the latest assessment. Implement its P6 packages rather than spending the task writing another plan. Preserve existing work and keep changes focused. Windows/Tailscale and Linux Docker are both required; MIT/CLA is already the recorded direction.

First fix the three reproduced implementation failures:

R6-01: commitAuthorityChange holds the sole database connection before taking authority.Lock, while invalidateWorkspace/publishAuthorizationChange hold that lock before querying the database. This creates a lock-order cycle. Establish one consistent ordering while preserving linearizable admission and keeping network writes outside the critical section.

R6-02: password reset and other credential/session mutations still commit outside the admission protocol. A real old-session socket received private data after reset committed and HTTP already returned 401, while delivery cleanup was delayed. Cover every relevant credential/session/account transition and new socket registration, including unaffected-session controls.

R6-03: when copying fails before status persistence, retainLocal can run before the new local generation is durable. With retention one and no prior independent copy, a failed final status write leaves the ledger naming an archive already pruned. Make retention depend on explicit successful persistence and protect every identity still needed by the durable ledger.

Convert review 6's scenarios into permanent regressions with permitted controls. Temporary tests may remain at /private/tmp/brrpbrrp-review6-probes-73ai8fwv; reconstruct from the report if absent. Test combined faults and real store/server reopen, not only single failures or in-memory fields.

Resolve R6-04 before WP-09 implementation: the new design note permits expired-key reuse, changes idempotency scope, clears outbox text on revocation and promises an additive migration while making a NOT NULL foreign-key column nullable. Reconcile these with the established no-recreation, cross-conversation conflict, opt-in text recovery and lifecycle/tombstone requirements. Produce an exact D-11 ADR and staged migration contract.

Preserve the passing baseline: 77 frontend tests, zero Svelte errors/warnings, full uncached Go race suite, build/format/vet and static package/notices/Docker checks, six unmodified browser journeys including non-cooperative RTP withdrawal/regrant, and 53 rollback checks.

Complete outstanding rendered narrow-role/failed-refresh privacy scenarios and update the evidence ledgers accurately. Qualify actual Windows/Tailscale and Linux Docker hosts where accessible; record external dependencies while finishing unblocked work. Do not claim gates passed without their evidence or substitute cross-builds/local directories for platform/off-host acceptance.

Then implement the bounded WP-09 channel-based conversation/receipt/outbox/read-cursor slice under its prerequisites before DMs/groups. Keep private features gated. Do not publish, deploy live services or overwrite user data without authorization.

Finish with implemented changes, exact verification results, unresolved dependencies, gate status and the next executable package.
~~~
