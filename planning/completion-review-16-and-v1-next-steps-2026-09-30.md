# BrrpBrrp review 16 and next implementation steps

**Reviewed:** 30 September 2026, Pacific time.  
**Committed source:** `fbce8b9cfb67b3efdbcd9c66ea5bec7e6470307c`, branch `codex/p9-private-reporting`.  
**Version:** `0.4.0-alpha.7`, schema **24**, backup format **2**.  
**Scope:** read-only implementation review, bounded verification and a new detailed coding handoff. No application or test files were changed by this review.

## Assessment

There are **no new commits since review15**, but an unfinished repair batch now changes thread submission, outbox persistence, receipt compatibility and SFU diagnostics. It contains useful repairs and should be completed, not discarded or restarted from the previous prompt.

**The batch is not green:** frontend tests have three failures and Svelte has one error. Additional probes identify draft lifecycle and receipt destination defects. The next coder's first objective is to finish this batch, then proceed to the previously verified authority, workflow, recovery and release work.

The [detailed implementation prompt](handoff-agentic-coder-v1-review-16-2026-09-30.md) supersedes the prior prompts. It specifies current source state, exact first failures, behavioral acceptance, subsequent implementation tranches, integrated verification and the complete v1 qualification gates.

## Working tree reviewed

Nine tracked files were already modified: `internal/api/messages.go`, `internal/rtc/sfu.go`, the named-thread panel, outbox, thread state/controller, Workspace and two frontend test files. Two implementation tests were already untracked: `internal/api/send_receipt_upgrade_test.go` and `web/tests/thread-send-outcome.test.mjs`. Review15 documents were also untracked.

The tracked diff contained **473 additions and 82 deletions** across nine files. Its SHA256 from `git diff --binary` was `24ced8ab8e1c08e29a6846066ec5dc1400b5e00611c465f760a4ee55f8b739f5`. This excludes untracked files. Selected complete-file identities:

| File | SHA256 |
| --- | --- |
| `internal/api/messages.go` | `f7c47fe823c3d5f01ce47b85bd2b6a752c5a1d0df83f3facacb96ba00d26d8e6` |
| `internal/api/send_receipt_upgrade_test.go` | `fc0be65e0cdc141887b615bb5f0fabf2cb4a5e852c9eccc5ce70eb09fa1654ba` |
| `internal/rtc/sfu.go` | `d331bbea25d86ae754b50c68ff0227abcfe6e2823d02d6bd91e5e67362d048bc` |
| `web/src/lib/workspace.svelte.ts` | `c76c70cee6c92751fc24f580589a2b2157b226d3d972d7662e80aaa45bbf7b3b` |
| `web/src/lib/components/NamedThreadPanel.svelte` | `b939163437cab41bb2a40b01b21acb10165b2c2595376caacf6ef309881e145a` |

All fresh results describe that working tree, not a clean committed release. The SFU modification now includes ICE candidate-pair diagnostics and relay classification as well as callback synchronization.

## Fresh verification

| Check | Result | Scope |
| --- | --- | --- |
| Frontend unit suite | **271 total; 268 passed, 3 failed** | Same command rerun with loopback access after sandbox listener errors |
| Svelte check | **1 error, 0 warnings** | Type error in the named-thread panel |
| Repository receipt tests | **9 passed**, including all three new tests | Ordinary legacy replay, current identity and deletion/expiry controls |
| Previous ordinary-upgrade probe | **Passed** | Credits the new ordinary legacy-digest fallback |
| Five server probe functions | **Failed on intended assertions**, reproducing remaining defects | Receipt destinations, role fallback, export revocation, unread/archive behavior; API batch **4.829s** |
| Eight focused RTC tests under `-race` | **8 passed**, **19.514s** | Forced relay, candidate reporting, missing-peer handling, ownership, mute/screen independence, reservation, screen audio and failed-negotiation cleanup |
| Client lifecycle probes | Mixed: account fence/destination repairs confirmed; three lifecycle failures reproduced | Actual transpiled Workspace/controller plus extracted component functions with controlled inputs; not rendered browser tests |

No full Go race suite, full browser suite, release build, end-to-end old-binary upgrade/recovery, physical media or real deployment qualification was run. Earlier source/artifact results were not relabeled as current acceptance.

## Repairs confirmed in this batch

- **Thread lookup ownership:** holding the initial GET, switching from A to B and resolving it returns `dropped`, with no POST or durable operation under B.
- **Persistent thread destination:** with valid UUID fixtures, an uncertain operation retains `thread_id`; restoration posts the original key/text to the thread endpoint, creates no channel row and clears after acknowledgment.
- **Pending visibility:** the named thread now receives its pending row.
- **Explicit refusal:** the thread controller now represents a definite refusal as an error outcome, rather than assuming every normal return means success.
- **Legacy client records:** entries lacking thread identity become recovery-only text rather than guessing a destination. This deliberately requires manual recovery for old ordinary entries too.
- **Ordinary server receipts:** unchanged ordinary old-format operations replay correctly; changed text conflicts. The new frozen-hash and current-thread destination tests are useful.
- **SFU repairs:** the current patch passes the eight selected race tests. This does not qualify the full media subsystem or physical capture.

## Current first-checkpoint blockers

### V16-01 Verification is failing

`NamedThreadPanel.svelte:32` assigns `unresolved.key: string` to a value inferred from `crypto.randomUUID()` as a UUID template-literal type. Fix the operation-key type through its validated contract.

Three unit tests fail:

1. **“malformed neighboring entries cannot hide a valid send, and future dates never retry”**, `outbox.test.mjs:143`: its future-date expectation contradicts the new behavior and another new test. Resolve the intended clock-skew/expiry policy before changing assertions.
2. **“a lost thread-send response replays under the same key after a reload”**, `workspace-state.test.mjs:1968`.
3. **“a thread operation that failed before commit retries into its thread, never the channel”**, `workspace-state.test.mjs:2018`.

The last two use account ID `me`; durable storage correctly requires a UUID, so nothing is persisted. Use valid identities and explicitly prove setup durability before testing restore. Do not weaken the storage guard.

### V16-02 Old component cleanup can restore A's draft for B

**P1; controlled lifecycle reproduction.** `NamedThreadPanel.svelte:48,153` writes via the current `workspace.composerDrafts` during cleanup. `workspace.stop()` replaces that map at `workspace.svelte.ts:2234`. Running old-panel cleanup afterward writes A's text into the replacement store; B can restore it when opening the same thread.

Bind component cleanup and saved drafts to their initiating account/session/store. A request fence cannot protect a synchronous cleanup that writes into a new account's state. Verify the real sign-out/account-switch component lifecycle as well as the deterministic ordering.

### V16-03 An unresolved key does not yet preserve immutable intent

**P1/P2; component-function/controller reproduction.** Send “original immutable text,” receive an uncertain outcome, edit to “edited new intent,” then press Reply. Both POSTs use the same key, but the second contains edited text and overwrites the outbox's original intent.

Separate the immutable submitted operation from editable drafting. Preserve original account, destination, key and text until resolved; define explicit recovery/new-work behavior. Adopting an unresolved key alone is insufficient.

### V16-04 Successful or dropped outcomes do not complete the draft lifecycle

**P2; successful-send case reproduced.** Save a draft by closing, reopen and send successfully, then close/reopen. Previously sent text returns under a fresh key. The panel clears only its local textarea; the saved draft remains.

**Source-confirmed dropped case:** `ThreadView` returns with no error for `dropped`; the panel still clears/rotates whenever `sendError` is absent. Propagate and consume the explicit outcome instead of inferring success from no error. Clear only the acknowledged draft snapshot and preserve newer edits made while a request was pending.

### V16-05 Legacy thread and blank-digest receipts can answer the wrong destination

**P1; reproduced with the old canonical encoding.** The new comments at `messages.go:115` assume no pre-change thread receipt existed. Threads preceded the digest change.

Observed:

- An old thread receipt retried through its original thread returns **409**.
- The same receipt presented through the parent channel returns **200 with the thread message**.
- A blank-digest backfilled ordinary receipt presented as a new thread send returns **200 with the ordinary message**.

Legacy fallback is restricted to non-thread requests at `messages.go:317–319`, while the returned result lacks a complete original-destination check. Fix ordinary, thread and blank-digest compatibility together. Do not naively treat the message's current `ThreadID` as its original destination: an ordinary root may have been promoted into a named thread later.

Existing deletion controls remain effective: deleting the old threaded result and retrying did not recreate content. Preserve deletion/erasure/expiry safeguards while repairing replay.

## Earlier blockers still open

Fresh server probes again reproduced:

- **Effective-role escalation:** a manager lacking send removes the target's last read-only assignment; default fallback grants sending.
- **Export delivery after withdrawal:** history returns 404 after role read access is removed, but a registered export still returns 200 with withdrawn content.
- **Incomplete unread projection:** reading the sole thread reply leaves channel unread at 1.
- **Archive contract:** an archived thread still accepts a new reply with 201.

The following relevant code is unchanged from review15; it was source-checked or carried forward as attributed earlier evidence, not claimed as freshly reproduced here:

- Timeout apply/lift lack promised audit/live-media follow-through, and bulk media principals omit suspension.
- Activity acknowledgment updates a different set from the visible acknowledged rows; evidence pagination does not retain terminal source exhaustion.
- Thread history remains unbounded, Reopen is missing, notification cleanup prefixes disagree, and the member-export browser journey can pass without a successful export.
- Recovery expects `body.exports` although the endpoint returns an array, uses unconditional successful checks after SQL reviewer fallback, and lacks exact restoration checks for several new data types.
- CI still selects the wrong 0.3.0 baseline; schema13 seed evidence and actual report output remain incomplete.

The local schema13 baseline remains available. Its rechecked SHA256 is `fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f` at `dist/brrpbrrp-p9-checkpoint`. Review15 verified its clean embedded revision and schema13 preflight. Local upgrade qualification can proceed independently of hosted Linux baseline acquisition.

## Release position and execution order

Existing alpha.7 packages still do not represent the current working tree; the previously inspected set mixes schema 21 and schema 14. No new current-source distribution or host/independent acceptance was found. Preserve historical artifacts and create a uniquely versioned prerelease after integration.

Execute in this order:

1. Finish the existing batch: green frontend gates, teardown account isolation, immutable uncertain sends, correct saved-draft cleanup and full receipt compatibility.
2. Repair effective-role escalation, export admission/cancellation and timeout live/audit behavior.
3. Complete thread, activity/evidence, notification, export and group/account journeys.
4. Complete browser media and deterministic recovery/upgrade qualification with meaningful controls.
5. Freeze coherent source, run integrated checks and consume a new identifiable distribution.
6. Qualify Windows x64/Tailscale and Linux amd64 Docker, physical media/mobile behavior, independent review, target capacity, backups and actual alerts.
7. Complete three-community fourteen-day beta, seven-day frozen-candidate observation, owner publication approval and verified release/maintenance handoff.

No G0–G8 gate is established by this review. The detailed prompt retains the entire master-plan acceptance scope and distinguishes local implementation from external qualification.

## Reproduction material

Frontend output is retained at `/private/tmp/brrp-review16-web-tests.log`. Client lifecycle probes were stdin diagnostics, not saved tests; implementation must add durable regressions and rendered acceptance.

Server fixture and overlay:

```text
/private/tmp/brrp-review16-l01ysvgq/export_test.go
/private/tmp/brrp-review16-l01ysvgq/overlay.json
```

Run the diagnostic functions from the repository with the local Go/module caches and `-overlay`; inspect function names in the fixture before selecting them. They assert intended behavior, so the failing controls correctly return a nonzero result until repaired. Temporary files are diagnostic aids, not dependencies to ship.

The review added only this document and the detailed prompt. Existing application changes and test files were preserved.
