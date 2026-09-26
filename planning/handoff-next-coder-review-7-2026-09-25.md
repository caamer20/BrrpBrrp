# BrrpBrrp — Coder Handoff After Review 7

Copy this prompt into the coding agent with the workspace attached.

~~~text
Continue implementing BrrpBrrp toward a production-ready, self-hosted, open-source v1.0.0.

Workspace: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp

Read applicable AGENTS.md instructions, then:
- planning/completion-review-7-and-next-steps-2026-09-25.md
- planning/completion-review-6-and-next-steps-2026-09-25.md
- docs/completion-remediation-2026-09-24.md
- docs/v1-progress.md
- docs/v1-implementation-plan.md and relevant ADRs.

Review 7 confirmed no implementation changes since review 6. All three R6 diagnostic failures still reproduce. Implement P6-01–06 using review 7's execution order; do not replace implementation with another audit or plan.

First delivery:

1. R6-01/02: establish a consistent database/authorization lock order and a complete commit/admission protocol. The current helpers can deadlock, and credential/session commits still permit private delivery after revocation. Cover every applicable transition and new socket registration, preserve unaffected users/sessions and keep network I/O outside the critical section.

2. R6-03: make backup retention depend on explicit successful persistence. Combined copy/status failures must preserve both local and independent identities needed by durable status, including when no independent copy ever succeeded. Test paired failures and actual store/server reopen.

3. R6-04: reconcile WP-09 in an exact D-11 ADR. Resolve expired retries, cross-conversation intent conflicts, opt-in unsent-text recovery, lifecycle/tombstone prerequisites and the actual NOT NULL foreign-key migration mechanics against the established requirements.

Add permanent regressions that fail before each fix, with permitted-operation controls. Existing temporary probes may be at /private/tmp/brrpbrrp-review6-probes-73ai8fwv/internal/api/review6_audit_test.go; reconstruct them from review 6 if absent.

Preserve prior verified progress: 77 frontend tests, full uncached Go race suite, six browser journeys including non-cooperative RTP withdrawal/regrant, 53 rollback checks and relevant build/type/format/package gates. These are review-6 results on identical source, not fresh review-7 full-suite runs. Run focused checks while implementing and the applicable final baseline afterward.

Then finish outstanding rendered privacy/narrow-role scenarios and update the evidence ledgers. Both Windows/Tailscale and Linux Docker are required; MIT/CLA remains the recorded direction. Qualify real hosts where accessible, record external dependencies and continue unblocked local work. No gate passes without its evidence.

Implement the bounded WP-09 shared-conversation/receipt/outbox/read-cursor slice after its design and foundation prerequisites. Build DMs/groups on those primitives later. Preserve existing work, avoid broad rewrites, keep private features gated and keep developer audit documents in planning/.

Do not publish releases, deploy live services or overwrite user data without authorization. Finish with implemented changes, exact tests/results, remaining dependencies, gate status and the next executable step.
~~~
