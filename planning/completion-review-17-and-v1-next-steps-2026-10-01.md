# BrrpBrrp review 17 and the remaining path to v1.0.0

**Reviewed:** 1 October 2026, Pacific time.  
**Committed source:** `cf2f6926a7aa640c25533cd5418af611e19d5601`, branch `codex/p9-private-reporting`.  
**Version:** `0.4.0-alpha.7`, schema **24**, backup format **2**.  
**Scope:** source review, bounded verification and a new implementation handoff. No application or test files were changed.

## Assessment

Four commits have landed since review16. The failing frontend batch is now green, receipt compatibility has materially improved, the delegated-role escalation is closed, and the private-evidence queue now terminates correctly. These repairs should be preserved.

**The next objective remains the complete v1.0.0 product and its release acceptance.** It is still an alpha, with reproducible message-retry and export-delivery defects, unfinished thread/activity behavior, incomplete recovery verification and no current coherent distribution or real deployment acceptance. No G0–G8 release gate is established by this review.

The [new detailed coding prompt](handoff-agentic-coder-v1-review-17-2026-10-01.md) starts with the two current high-priority failures, then specifies the remaining product, recovery, packaging and release work. It replaces review16's obsolete instruction to fix the already-resolved compiler and fixture failures.

## Changes since review16

| Commit | Change | Current assessment |
| --- | --- | --- |
| `950f4c6` | Thread draft/send lifecycle and historical receipt destinations | Earlier failures repaired; one multiple-operation retry defect remains |
| `462e42c` | Authorization of effective role-set gains | Delegated escalation closed; permitted controls pass |
| `587b18b` | Evidence pagination exhaustion and source-qualified identities | Actual component-function probe now reaches all 190 unique rows and finishes |
| `cf2f692` | Worker notification admission and retention | Implementation and bounded unit checks improved; real browser concurrency/delivery still unqualified |

Only `internal/rtc/sfu.go` was dirty at review start. Its complete-file SHA256 remains `d331bbea25d86ae754b50c68ff0227abcfe6e2823d02d6bd91e5e67362d048bc`, identical to review16. The current diff SHA256 is `59a533e71bf7364fe17cdb58b4a6e8be4109e6318ac8072fa3667cb7f9df8a3e`. Preserve that patch. Fresh test results describe HEAD plus this existing patch, not a clean release artifact.

## Fresh verification

| Verification | Result and boundary |
| --- | --- |
| Frontend unit suite | **287/287 passed** with local loopback access; initial listener EPERM failures were environmental |
| Svelte check | **0 errors, 0 warnings** |
| Frontend formatting | **Passed** |
| Bounded API run including diagnostic probes | **30 top-level tests: 28 passed, 2 failed**, **15.016s**; failures deliberately assert remaining export and thread contracts |
| Receipt and role regressions | All ten new receipt tests, four effective-role tests, nine existing receipt/thread-identity tests and relevant independent closure probes passed |
| Client lifecycle probes | Earlier teardown, saved-draft, dropped-outcome and immutable-original-text repairs confirmed; a multiple-uncertain-send failure reproduced |
| Actual evidence component load function | **100 → 170 → 190 rows**, done on page 3, five API calls, no exhausted-source refetch; overlapping IDs retain separate notes |
| Operations/artifact inspection | No new recovery, CI, package or qualification evidence; preserved schema13 executable hashes unchanged |

Client probes execute actual transpiled Workspace/controller and extracted component functions with controlled inputs. They are not rendered-browser acceptance. Notification unit tests use an IndexedDB mock and do not establish concurrent real-browser transactions or deployed push delivery.

The eight selected RTC race tests passed in review16 against the same SFU file bytes; they were **not rerun** here. No fresh full Go race suite, browser suite, release build, full recovery/upgrade, physical media, real Windows/Tailscale or Docker qualification, independent review or observation period was performed.

## Repairs now credited

- Old ordinary/thread digests replay only through the intended destination. Blank-digest receipts reject wrong thread destinations. Promoted roots retain their original channel destination. Deleted and expired results remain terminal.
- A delegated manager cannot remove a role assignment to grant capabilities they lack. Deleting an assigned restrictive role is refused for both the manager and owner; the assignment remains. Deleting an unassigned role succeeds.
- Old component teardown no longer writes A's draft into B's store. Successful sending clears the saved draft. An account switch during the preliminary lookup drops that operation while preserving its text and making no POST; a later dropped outcome can occur after a POST and does not generally prove that nothing was sent.
- Edited new work no longer overwrites an older uncertain operation. Thread destination persistence and pending visibility remain intact. The previous type error, invalid-UUID fixtures and conflicting time-policy tests are resolved; allowed future skew is now bounded to five minutes.
- Evidence pagination uses an exhausted skipped page and `(source_kind,id)` identity for rows, notes and busy state. The previously nonterminating case now finishes.
- Page and push notifications share a worker admission path and an IndexedDB transaction for claiming a key. The same `brrp.notified.` prefix is used for cleanup and writes; retention is bounded to 24 hours and 500 entries.

These are closures of specific defects and improvements in implementation. Full feature, browser and deployment acceptance remains separate.

## Remaining findings

### V17-01 Multiple uncertain thread replies can lose retry identity

**P1; reproduced through actual component/controller functions.** `unresolvedThreadOperation` at `workspace.svelte.ts:1349` returns the first unresolved operation in a thread. The panel's key selection at `NamedThreadPanel.svelte:94` compares the current draft only with that operation.

Sequence:

1. Submit A; uncertain outcome leaves A/key1.
2. Edit to B and submit; uncertain outcome leaves B/key2.
3. Press Reply again with B unchanged; the panel submits B/key3.

The probe observed three POSTs and outbox entries. If B/key2 committed but its response was lost, B/key3 can create a duplicate. The older immutable-intent overwrite is repaired; association between the current draft and its own unresolved operation is still incomplete.

**Required:** explicit account/destination/operation association across multiple pending replies, edits, retry, close/reopen and reload. Do not infer operation identity solely from the first entry or matching text. Add deterministic coverage and a rendered two-uncertain-operation journey with server message counts.

### V17-02 Export delivery still survives permission withdrawal

**P1; freshly reproduced.** A permitted export is held after registry admission. The role API removes the reader's `channel.read`. New history becomes 404, but the export returns 200 containing the protected snapshot.

`export.go:622–665` checks scopes and commits; `:671` then registers an empty channel scope. Selective revocation at `authz.go:676–679` skips that scope, while the current registry models only one channel per delivery. The separate check/registration interval also needs coherent admission.

**Required:** register and revalidate export delivery through the existing authority protocol, carrying the complete set of conversations in the snapshot. Cancel applicable streams after role/channel/group/account/session/feature changes, preserving unaffected streams and permitted range delivery. Do not hold database locks across slow transfer. These rules concern server-held delivery, not copies legitimately downloaded earlier.

### V17-03 Thread read and archive behavior remains incomplete

**P2; freshly reproduced.** Reading the sole unread thread reply still leaves channel unread at 1; the unread query counts thread messages using only the channel cursor. Archived threads still accept new replies with 201 because both admission and commit checks reject only `locked`.

Source inspection also confirms unbounded accumulated page reads and a missing direct Reopen action. Finish independent channel/thread unread and mention projection, archive/reopen behavior, commit-time state controls and bounded full-history navigation.

### V17-04 Activity, moderation and export journeys still need integration

**Unchanged source and attributed earlier findings.** Activity acknowledgment still sends the oldest visible ID while the server updates older rows; a returned count does not identify the same acknowledged set. Fix persistence and local rendering together, including concurrent arrivals and pagination.

Timeout apply/lift still lack the promised audit and live community-media follow-through. Bulk media principal construction omits suspension. Preserve the passing private HTTP scope and non-replaying expiry behavior while completing those controls.

The member-export browser journey still accepts error paths instead of proving a successful export, and its request/building/download UX remains unfinished. Complete real recent-authentication, stable retry identity and downloaded-data acceptance.

Default-role semantics remain inconsistent between older ADR/master wording and later fallback implementation. Reconcile the documented contract and UI meaning separately from the now-closed delegated escalation; do not relabel the closed vulnerability as still reproduced.

### V17-05 Recovery and CI still cannot establish the current product's preservation

**No changes in this area across the four commits.** The known issues remain:

- CI requests 0.3.0, which is not the required schema13 private-state baseline.
- The schema13 seed omits submitted evidence required by the verifier.
- The upgrade script ignores `--report`.
- Export restore checks read `body.exports` although the API returns a raw array, allowing vacuous assertions.
- Reviewer setup/revocation can fall back to SQL and record unconditional success.
- Exact restored thread follow/read, activity acknowledgment/preferences, group authority and post-snapshot reconciliation remain insufficiently asserted.

Two preserved local schema13 binaries still match their recorded hashes. In particular, `dist/brrpbrrp-p9-checkpoint` is SHA256 `fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f`. Its embedded clean revision and schema13 preflight were verified in review15. Local qualification is not blocked on archive hosting.

Finish real API fixture ceremonies, precise restored-state checks and failing controls. Include populated current/historical acknowledged and deleted send receipts so the new compatibility repair is covered by an actual upgrade. Retain source/binary identities and machine-readable results.

### V17-06 Distribution and real release acceptance remain open

No new current-source distribution, recovery report or qualification manifest was found. Alpha.7 packages remain the previously inspected mixed schema21/schema14 set. No Git remote is configured; public repository/archive/registry destinations must be established before hosted CI/publication, while local implementation and packaging can proceed.

Produce a new unique prerelease after integration, consume extracted native/source artifacts and qualify the actual container image. Complete both required host paths, physical media/mobile acceptance, independent review, target-load/soak, encrypted independent recovery and delivered alerts. Then complete the master's three-community fourteen-day beta and seven-day frozen-candidate observation before publication approval.

## Next implementation order

1. Fix multiple-uncertain thread retry identity and export admission/revocation, preserving the verified closures.
2. Complete thread read/archive/history, Activity acknowledgment, timeout live/audit behavior and the member-export journey.
3. Finish real browser media and remaining group/account/notification acceptance.
4. Produce dependable current/prior-schema recovery evidence with exact assertions and security reconciliation.
5. Freeze coherent source, run integrated gates and package a uniquely versioned candidate with extracted consumers.
6. Complete both-host, physical-device, independent-review, beta and final-release acceptance.

The detailed prompt supplies executable first steps, acceptance matrices, evidence requirements and the full goal. The coder should continue through that sequence; passing the next two regressions alone is not v1 completion.

## Reproduction material

```text
Frontend log: /private/tmp/brrp-review17-web-tests.log
Formatting log: /private/tmp/brrpbrrp-review17-format.log
API log: /private/tmp/brrp-review17-xk3zhw46/go-api.log
API overlay: /private/tmp/brrp-review17-xk3zhw46/overlay.json
API fixture: /private/tmp/brrp-review17-xk3zhw46/export_test.go
```

The remaining server probes are `TestReview15ExportStopsAtRoleCapabilityWithdrawal` and `TestReview15ThreadUnreadAndArchivedSend`. Their historical function names do not change the fact that they were rerun against this review's source. The client probes were stdin diagnostics, not saved browser tests. Add permanent implementation regressions instead of depending on temporary files.
