# v1.0.0 execution ledger — 2026-10-01

Compact record of implementation tranches toward v1.0.0, per the review-17 handoff
(`planning/handoff-agentic-coder-v1-review-17-2026-10-01.md`). One section per tranche.

---

## Tranche 1 — Multiple-uncertain reply identity and revocable export delivery

**Source and dirty state before the tranche.** Branch `codex/p9-private-reporting`, HEAD
`cf2f692` (application 0.4.0-alpha.7, schema 24, backup format 2). One preexisting dirty
file, `internal/rtc/sfu.go` (SHA256 `d331bbea25d86ae754b50c68ff0227abcfe6e2823d02d6bd91e5e67362d048bc`,
unchanged from review 16/17), reviewed and integrated as its own commit in this tranche.
Two untracked review-17 planning documents were preserved.

### 1a. Multiple uncertain thread replies (review 17, V17-01)

**Failing behavior and reproduction.** Reproduced live in the browser journey added here
(`web/e2e/threads.spec.mjs`, "two uncertain thread replies retry under their own keys"):
submit A whose request is dropped before delivery, then B whose request is stored while
its response is discarded, then press Reply again on unchanged B. Before the fix the
second B submission carried a *fresh* idempotency key (verified in the journey: the two
observed keys differed), so a committed-but-unacknowledged reply could be stored twice.

**Changed files and reason.**

- `web/src/lib/operations.ts` (new) — the composer's explicit association record
  (account slot → operation key + immutable snapshot), validated and bounded, persisted
  beside drafts and the outbox under the same opt-in.
- `web/src/lib/workspace.svelte.ts` — records the association where a thread send is
  claimed (`sendIntoThread`), keeps it live only while the named outbox entry is
  unresolved at the same destination with the same words, ends it on every release
  (acknowledged, refused, expired, discarded, dropped), clears only the matching draft
  snapshot on acknowledgement, restores/persists it per account, and exposes
  `threadComposerOperations` for the composer's decision. The map is `$state` and the
  lookup reads the live outbox *before* any early return: the rendered journey caught
  the first version caching its initial empty answer forever (conditional dependency).
- `web/src/lib/thread-state.ts` — `retryOperationKey`: the shared, testable selection
  the panel calls; exact words select *their own* unresolved operation.
- `web/src/lib/components/NamedThreadPanel.svelte` — key selection through the
  association (never the first outbox entry, never a text search over the queue);
  the "new reply" notice now also covers unconfirmed pending rows.
- `web/tests/thread-operation-identity.test.mjs` (new) — ten deterministic tests over
  the real compiled Workspace: newest/older retry identity, receipt reconciliation,
  never-committed same-key retry, reload survival, refusal/expiry terminal behavior,
  matching-snapshot clearing, account isolation, store validation/bounds.
- `web/tests/workspace-state.test.mjs`, `web/tests/thread-view.test.mjs` — harness
  inlines the new module; replaced the two assertions that pinned the old first-entry
  mechanism with assertions of the association contract and its reactivity.
- `web/e2e/threads.spec.mjs` — rendered journey: both uncertain operations, key reuse
  observed on the wire, server-side message counts (one stored reply per text).

**Commands and results.**

```text
cd web && npm test                      → 297/297 pass (287 before + 10 new)
npm run check                           → 0 errors, 0 warnings
npm run format:check                    → pass
npm run build                           → pass (build f8cdd5847b0d3a64)
PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers \
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm run test:e2e -- --project=threads
                                        → 7 passed (release + threads projects), incl. the new journey
```

**What this proves and limits.** The production caller selects and persists the
association; retries reconcile under the original key; the server stores one reply per
text. The browser journey is the regression for the Svelte reactivity trap the unit
harness cannot see (its `$state` is identity). Not covered here: cross-tab concurrent
submissions and mobile-physical behavior of the same flow.

### 1b. Export delivery authorization (review 17, V17-02)

**Failing behavior and reproduction.** The review-17 diagnostic
(`TestReview15ExportStopsAtRoleCapabilityWithdrawal`, temporary overlay in
`/private/tmp/brrp-review17-xk3zhw46/`) reproduced: a held export response kept
delivering the snapshot after the reader's `channel.read` was withdrawn through the real
role API, while history answered 404. The export registered with an empty channel scope,
which every selective revocation path skipped.

**Changed files and reason.**

- `internal/api/delivery.go` — a registered response now carries its full set of
  conversation scopes (attachment: its channel; export: every recorded conversation;
  unclaimed upload: none). `revoke(channelID)` ends any response carrying that channel.
- `internal/api/export.go` — `downloadExport` registers the response with the export's
  complete scope set *before* the authorization decision, re-establishes authority and
  session through `admitStream` (the same protocol file downloads use), re-checks every
  scope with the same policy a history read applies, refuses a transfer already ended by
  a concurrent revocation, and streams through the same revocable reader. No database
  lock is held across the transfer; no path serves on a lookup failure.
- `internal/api/authz.go` — `endUnreadableStreams` evaluates every scope of every
  affected response with `exportScopeStillReadable` (channel visibility/access lists,
  private accepted membership, feature gate, existence/active state), cached per
  (account, scope); one unreadable scope ends the whole file, unrelated responses keep
  running.
- `internal/api/uploads.go`, `internal/api/groups_test.go` — call sites updated to the
  scoped registration.
- `internal/api/export_revocation_test.go` (new) — six regressions: withdrawal after
  registration while the response is held; allowed/denied/restored delivery;
  per-request range reauthorization with exact bytes; one withdrawn scope of a
  two-conversation export (no partial file, unrelated export untouched); mid-transfer
  withdrawal of a >3 MB export with registry cleanup; session revocation mid-export
  with a surviving session.

**Commands and results.**

```text
GOCACHE=/private/tmp/brrpbrrp-go-build GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test -overlay /private/tmp/brrp-review17-xk3zhw46/overlay.json ./internal/api \
  -run '^TestReview15(ExportStopsAtRoleCapabilityWithdrawal|ThreadUnreadAndArchivedSend)$' -count=1 -timeout 3m -v
→ TestReview15ExportStopsAtRoleCapabilityWithdrawal PASS
  (TestReview15ThreadUnreadAndArchivedSend still fails — tranche 2, section 5)

.tools/go/bin/go test ./internal/api -run '<six new tests>' -count=1     → 6/6 pass
.tools/go/bin/go test ./internal/api -count=1 -timeout 15m              → ok, 176.7s (full package)
.tools/go/bin/go build ./...                                            → pass
```

**What this proves and limits.** Withdrawal cannot fit between the scope check and
admission: registration precedes the decision, so either the check sees the committed
change or the registry ends the already-registered response; a held response delivers no
protected bytes and answers 403. Mid-transfer withdrawal stops the copy and leaves no
registered stream. Not covered here: real-network slow clients across a WAN; files
legitimately downloaded before withdrawal are out of scope by contract.

### 1c. SFU patch integration

**Change.** The preexisting `internal/rtc/sfu.go` patch (74 changed lines, hash above)
was reviewed and committed as its own change: `setEmitter` now writes `peer.send` under
`peer.sendMu` (the lock `emit` reads it under, instead of the SFU lock — two locks
guarding one field is no lock), `selectedCandidatePair` asks the ICE transport for its
selected pair instead of `GetStats` (whose teardown state Pion clears racelessly), and
the relayed-path log compares type tokens (`"relay"`) reported by the new pair source.

**Commands and results.**

```text
.tools/go/bin/go test -race -count=1 ./internal/rtc -timeout 12m          → ok, 94.5s
```

**What this proves and limits.** The focused and full-package race passes confirm the
lock and ICE-transport changes against the existing RTC suites. Media behavior itself
(two-browser journeys, forced-relay paths, physical capture) remains tranche 4 work.

### Remaining dependency / next executable action

- Next: tranche 2 (handoff §5) — thread unread/archive/history, Activity
  acknowledgment, timeout live/audit, member-export journey, authority wording. The
  deliberately-failing `TestReview15ThreadUnreadAndArchivedSend` names the first two
  thread contracts to close.
- External inputs still owed by the owner: hosted Linux schema-13 baseline for CI,
  publication destinations/credentials, physical devices/hosts for later gates. None
  blocks the local implementation above.
