# Handoff prompt — next BrrpBrrp implementation tranche

Copy the following prompt into the next coding task.

```text
Continue implementing BrrpBrrp toward a production-ready, self-hosted, open-source Discord competitor.

Workspace: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp

Read applicable AGENTS.md instructions, then:
1. planning/completion-review-3-and-next-steps-2026-09-25.md — latest assessment and P3 execution plan.
2. docs/completion-remediation-2026-09-24.md and docs/v1-progress.md.
3. docs/v1-implementation-plan.md and relevant docs/adr/ decisions.

Your task is implementation and verification. Inspect current source first because work may have landed after the review. Preserve completed fixes; do not repeat historical findings as current defects without reproducing them.

Current baseline: alpha.4, schema 5, backup format 2. Owner/credential transaction fixes, history/context invalidation, scoped management UI, archive validation and the maintained rollback command have advanced. Both Windows/Tailscale and Linux Docker are required. Existing licensing decisions stand.

Execute P3-01 through P3-06, starting with the remaining local correctness work:

1. CLIENT AUTHORIZATION: Late /messages/{id} and /unread responses still cross revocation. Apply one generation contract to every protected async completion, including snapshots, errors/finally, parent refresh and send acknowledgements. Add permanent model and rendered failed-refresh regressions.

2. SERVER REVOCATION: Default roles apply implicitly, but stream cancellation enumerates explicit assignments. Fix effective-holder targeting and test actual original/preview/range streams. Close the documented commit/audience/queue/write and download-admission ordering cases with deterministic barriers and permitted controls.

3. BACKUP FAILURES: A failed independent copy leaves Attempts=0, skips the short retry, clears prior verified copy identity and can delete the previous protected local generation. Separate attempt/local-success/independent-success state; protect recovery generations; count/retry copy failures. Capture snapshot time at the actual database snapshot boundary, before lengthy compression.

4. BROWSER/MEDIA: The checked-in voice test waits for Leave voice inside a room dialog that closes on join. Follow the actual sidebar state and reopen the dialog for participant assertions. A locator-only diagnostic adjustment made all five journeys pass; no app behavior change was necessary. Add failure cleanup and a malicious RTP sender control proving outbound traffic continues while server forwarding stops. Complete category-only/role-only and failed-refresh browser coverage.

5. QUALIFICATION: Preserve the now-maintained 53-check old-binary rollback path and local extracted-source consumer progress. Run actual Windows and Linux Docker lifecycle/recovery checks when hosts are available; keep missing access explicit while continuing local work. Finish traceable artifacts, signing/provenance and independent review before promotion.

6. EVIDENCE: Update the ledgers to distinguish implementation, local verification, platform acceptance, independent approval and release. Correct stale completion claims and gate labels. G3 is community completion; G4 is media completion.

Working rules:
- Keep changes focused and maintainable; preserve existing work and old release archives.
- Reproduce defects with permanent failing tests before fixes. Test valid behavior too.
- Keep restricted exposure disabled until its acceptance gate passes.
- Do not weaken assertions or deny every user merely to get green tests.
- Do not claim Docker/Windows/physical-device/independent-review evidence from local substitutes.
- Do not publish, deploy, alter licensing or overwrite historical releases without authorization.
- Avoid routine confirmation requests for reversible coding/testing. Report real external blockers and continue unaffected work.
- Complete the privacy/recovery foundation before implementing private conversations. After acceptance, proceed to WP-09 common conversation/state/lifecycle design, then DMs/groups and the remaining v1 roadmap.

Verification context: 68 frontend tests and the full uncached Go race suite passed, alongside frontend checks/build, static package checks, 53 maintained rollback checks and a local extracted-source build/repackage/package-smoke run. The unmodified browser suite had 3 passes, 1 voice-locator failure and 1 dependency skip. The diagnostic locator-only copy passed all 5, including synthetic voice transport. Four additional required-behavior probes failed (client incremental/unread, implicit-role stream, copy retry/retention). Read the latest review for precise limits.

Deliver implemented fixes, permanent regressions, updated evidence/docs, and a concise handoff listing checks run, remaining blockers and the next executable package. Proceed from inspection into implementation rather than stopping after another plan.
```
