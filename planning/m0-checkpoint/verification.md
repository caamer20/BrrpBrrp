# M0 checkpoint — review 12 defects repaired

29 September 2026. Source `153202da9ae15118be8fff13bcf7c84e688db345`, clean. Application **0.4.0-alpha.7**,
schema **14**, backup format **2**, frontend `be50533485433eca`. No publication, no production exposure, no
feature-gate change.

This checkpoint repairs the ten R12 findings. The alpha.7 archives under `dist/releases` were built from
`9763661` and are **unchanged** — these source changes are not packaged, so no alpha.7 archive describes this
commit. A new prerelease label is required before any distribution.

## Findings closed

| Finding | Defect | Evidence |
| --- | --- | --- |
| R12-01 | A request review wrote `private_request.*` while both audit readers and the maintenance pass recognised only `private_report.*`. An audit-only account read the reviewer, evidence id, action and time; the event was reclaimed on the 90-day clock. | `TestRequestReviewAuditIsClassifiedAsRestricted`, `TestRequestReviewAuditKeepsTheRestrictedRetention` |
| R12-02 | The storage bound counted only message evidence, so 5,000 request reports were invisible to it and row 5,001 was admitted. | `TestRequestAndMessageEvidenceShareOneStorageBound`, `TestRequestEvidenceIgnoresExpiredRowsForTheStorageBound` |
| R12-03 | The report path measured the introduction in bytes while creation and preview measured characters, so 500 CJK characters created and previewed but could not be reported. | `TestRequestIntroductionLimitCountsCharactersNotBytes` |
| R12-04 | The operation digest bound the reason and the receipt's own attempt, not the destination; a spent key was accepted after a pair renewed. | `TestOperationKeyIsBoundToItsDestination`, `TestARenewedAttemptIsDifferentWorkForTheSameKey` |
| R12-05 | One `expires_at` carried the evidence's 90 days on the first answer and the receipt's 30 on a replay. An aged-out receipt hid its row while its unique key remained. | `TestReceiptDistinguishesEvidenceExpiryFromItsOwn`, `TestAnExpiredReceiptIsDefinedAndItsKeyStaysSpent` |
| R12-06 | Rollback was injected only at the evidence write, proving the first of five. | `TestReportAndBlockRollsBackAtEveryWrite` (5 subtests), `TestReportRefusalLeavesNoTraceOfAnAttempt` |
| R12-07 | A `submitted` flag set before the request and never cleared left the button enabled after a refusal while its handler ignored it; a check failure was stored and never rendered. | `report-dialog-state.test.mjs` (7 tests) |
| R12-08 | The disclosure claimed "Nobody else sees it", which is false. The queue merged two cursors, refetching an exhausted source from its start. | `private-evidence-merge.test.mjs` (7 tests) |
| R12-09 | The recovery rehearsal upgraded from schema 12, skipping migration 13, and had no reviewer grant to revoke. | `seed-schema13-private.mjs` + `upgrade-schema13-private.mjs`; 113 private-recovery checks |
| R12-10 | The race budget was 20m in the Makefile and runbook but 40m in CI; the ledger said schema 12 and ADR 0014 described a slice as alpha.8. | Aligned to 40m; manifest lists all six archives with extracted-executable hashes |

### A second defect found while proving R12-01

`restrictedAuditLike` emitted its `OR` chain without parentheses, so
`DELETE ... WHERE created_at<? AND a LIKE 'private_report.%' OR a LIKE …` parsed as
`age AND first OR second OR third` — `AND` binds tighter — and reclaimed **every** restricted audit row on the
first maintenance pass after it was written, regardless of age. The community reader had the same shape. The
parentheses are now emitted by the helper and documented as load-bearing, because a fix that only patched the
caller would have left the next caller to reintroduce it.

## Verification

| Check | Command | Result |
| --- | --- | --- |
| Go vet | `go vet ./...` | Clean. |
| Go race suite | `go test -race -count=1 -timeout 40m ./...` | Green, all ten packages. `internal/api` 1330.041s. |
| Frontend units | `npm --prefix web test` | 132 passed. |
| Svelte checking | `npm --prefix web run check` | 135 files, 0 errors, 0 warnings. |
| Prettier | `npm --prefix web run format:check` | Clean. |
| Frontend build | `npm --prefix web run build` | Clean; `be50533485433eca`. |
| Browser, default feature-off | `npm --prefix web run test:e2e` | **13 passed.** |
| Browser, private enabled | `BRRP_E2E_PRIVATE_CONVERSATIONS=1 …` | **21 passed.** |
| Community recovery | `upgrade-recovery.mjs` | **PASS: 23 checks.** Schema 14, durability FULL. |
| Private recovery | `upgrade-recovery-private.mjs` | **PASS: 113 checks.** |
| Schema 13 → current | `seed-` + `upgrade-schema13-private.mjs` | Every preserved value identical; distinct attempt identities; format unchanged. |
| Cross-version rollback | `upgrade-rollback.mjs --old-binary` | **PASS: 57 checks** from a real schema-13 executable. |
| Built binary's VCS stamp | `go version -m dist/brrpbrrp` | `vcs.modified=false` at `153202d`. |

Each Go fix was verified failing before its change and passing after, by reverting the fix and re-running the
named test. The failure-injection matrix was verified to catch a broken transaction by committing early
between the block and the evidence: four of its five subtests fail. The dialog-state regression was verified by
restoring the permissive predicate, which fails two tests.

## What was not done

No Windows/Tailscale host, no Linux Docker runtime, no PowerShell, no physical microphone, browser or device
testing, no off-host or encrypted-backup restore, no capacity or soak measurement, no independent security,
privacy or accessibility review, no hosted CI, no signing, no publication. Neither of the two required
qualification targets was executed, and no v1 release gate is claimed.

M1 (durable activity and private notifications) through M7 remain open, as does the WP-14 through WP-33
product scope in the v1 plan. Private conversations remain **default-off**.

## Two items needing an owner decision

1. **Reviewer grant seeding in the recovery rehearsal.** The rehearsal writes its `report.review_private` role
   and assignment directly rather than through the API, because this installation has never completed the
   ownership ceremony and so has no `role.manage`. The rehearsal therefore proves that a grant survives a
   backup and must be reconciled, but not that the API path creates one. Completion of the ceremony in the
   rehearsal would close that, at the cost of mandating a second factor for the rest of the run.
2. **`request_evidence.` retained in the restricted-audit prefix list.** Kept for rows written before the
   `private_` prefixes were adopted. If no such rows can exist in a real installation, the entry can be
   dropped; it is harmless while it is there, and removing it without a compatibility decision risks
   reclassifying existing rows.
