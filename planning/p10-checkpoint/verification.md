# P10 checkpoint verification

28 September 2026. Candidate **0.4.0-alpha.7**, schema **14**, backup format **2**, frontend `b17ccfd6cd9325e2`.
Source commit `976366148348cb7ea754bb5304c126503ae68639`, clean at build time. No publication, no production exposure, no feature-gate change.

## What this tranche added

ADR 0013 (private-message evidence) and ADR 0014 (pending-request evidence and command receipts) implement
selected private-message text reporting and recipient-only request-introduction reporting, with
`report.review_private` as a distinct delegated capability. Migration 14 adds the request-attempt identity,
the request-evidence table and durable command receipts. The recipient flow and the reviewer queue are in
the interface; an outstanding incoming-request count is derived from current state rather than stored.

## Artifact identity

The native archives were built from a clean tree, which is the point the P8 checkpoint could not claim: the
binary **inside the extracted archive** carries `vcs.revision=9763661…` and `vcs.modified=false`. Every
consumer below ran against those extracted bytes, not against the build tree, and a source build is never
borrowed as proof of an untested package's identity. Digests are in `manifest.json` and `dist/releases/SHA256SUMS`.

| Consumer | Ran against | Result |
| --- | --- | --- |
| `check-package.mjs` | extracted `darwin/amd64` binary | Passed: CLI setup, backup, restore, lock, health, production guard, embedded UI, manifest, graceful stop. |
| `upgrade-recovery-private.mjs` | extracted binary | **PASS: 102 checks, 0 failed.** |
| `upgrade-recovery.mjs` | extracted binary | **PASS: 23 checks, 0 failed.** Schema 14, durability FULL. |
| `upgrade-rollback.mjs` | extracted binary + real alpha.6 archive | **PASS: 57 checks, 0 failed.** alpha.6 (schema 12) → alpha.7 (schema 14), format 2 unchanged. |
| `check-source-package.mjs` | working tree | Passed: 388 files, 43 native package documents, every documented local link resolved in both sets. |
| `check-package.mjs` on the source archive's own build | freshly extracted source | Passed. The extracted source installs, builds the frontend and links a working executable independently. |

The source archive's binary carries no VCS stamp. That is a property of a tarball rather than a git
checkout, not a dirty build, and it is recorded as such in the manifest rather than glossed.

## Test evidence

| Check | Result |
| --- | --- |
| `go vet ./...` | Clean. |
| `go test -race -count=1 -timeout 20m ./...` | Green across all ten packages; `internal/api` 1110.594s. |
| Restricted review audit retention regression | Fails against the previous single statement ("restricted review audit was reclaimed with the evidence it describes"), passes after. |
| Frontend units | 118/118 passed. |
| `svelte-check` | 133 files, 0 errors, 0 warnings. |
| Prettier | Clean. |
| Production frontend build | Clean; build `b17ccfd6cd9325e2`. |
| Browser suite, default feature-off | 13/13 passed. |
| Browser suite, private conversations enabled | 21/21 passed. |
| Node consumer syntax | Clean on all seven recovery and packaging scripts. |

## Defects found and repaired in this tranche

1. **Reviewer journey could not run at all.** Five separate causes, each masking the next: generated
   account names exceeded the 32-character username bound and were refused at creation while reporting as
   "Incorrect username or password"; the journey authenticated as an account that did not exist;
   `report.review_private` is deliberately withheld from the pre-ceremony administrator bridge
   (`internal/authz/authz.go:195`), so it must be delegated from an owner; arranging the fixture
   conversations signed the shared Playwright request fixture in as a participant, so role creation ran as
   that participant; and an intermediate project ordering contained a dependency cycle that Playwright
   silently dropped. The journey now runs after the ownership journey, signs in as the owner that journey
   leaves in place, and re-establishes that session immediately before delegating.
2. **The private evidence queue returned bare records**, so a merged queue had to infer the source from an
   id. Records now carry `source_kind`, and still omit the originating conversation.
3. **Restricted review audit was reclaimed with the evidence.** ADR 0014 promises review metadata survives
   400 days precisely so the record that a review happened outlives the thing reviewed, and maintenance
   deleted every audit row at 90 days. Now two statements, matched by action prefix, so neither rule
   reaches the other's rows.
4. **The recovery rehearsal never exercised the operation this tranche added.** It proved private-message
   evidence and post-snapshot reconciliation, but created no pending introduction and reported nothing.
   Request evidence, the block, the command receipt, the retry and the spent-key conflict are now asserted
   on both sides of the backup.
5. **The report dialog's admission rules were untested.** They gated every await inline, so an account
   switch or capability revocation landing mid-flight was only covered on the happy path. The rules moved to
   `web/src/lib/request-report.ts` and are now tested directly.

## Not done, and not claimed

No Windows or Tailscale host, Linux Docker runtime, PowerShell or load/soak qualification, off-host
recovery, physical browser/network/relay testing, independent security or privacy review, accessibility
review, hosted CI, signing, or publication. No beta or v1 release gate is declared passed. The Windows
native service and Linux Docker remain the two required qualification targets and neither was executed here.

Private conversations remain **default-off** behind `BRRP_ENABLE_PRIVATE_CONVERSATIONS`. Pending named
threads, moderation timers and invitations, composer/upload/navigation polish, human accessibility review,
a separate media architecture, private calls and screen sharing, and repository/security-intake ownership
remain the next bounded tranches, in that order.

Disabling the private feature is not erasure: it refuses private report submission and review, clears an
open queue and cancels private UI, while stored evidence, access grants and backups stay exactly as they
are. No gate in this tranche was changed from its default.
