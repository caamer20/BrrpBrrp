# BrrpBrrp implementation handoff to reach version 1.0.0

Prepared 30 September 2026 for an agentic coder with repository and terminal access. This is an implementation assignment covering the current unfinished repair batch, the remaining product work, and release qualification. Copy everything below the separator.

---

## 1. Your goal and expected outcome

You are the implementation agent for **BrrpBrrp**, an open-source, self-hosted Discord replacement. Your goal is to finish the agreed v1 product, produce a coherent and reproducible release candidate, complete its actual acceptance gates, and reach **v1.0.0** after explicit publication approval.

Work in the repository. Implement, verify, and preserve progress. Continue beyond the first passing batch while meaningful work remains. Do not turn this assignment into another broad review or a succession of replacement plans. If your environment provides a goal facility, create or continue this goal without inventing a token budget. Otherwise maintain a compact execution ledger.

There are two outcomes to distinguish:

1. **Engineering candidate:** the complete agreed product works through its interfaces; local correctness, privacy, data preservation, integration and package-consumer gates pass against an identifiable source and distribution.
2. **Stable v1.0.0:** that candidate also passes both required deployment qualifications, physical-device/network acceptance, independent reviews, beta and final observation, and receives release authorization.

An unavailable external resource may prevent the second outcome. It does not justify leaving independently implementable local failures unresolved. Never change only the version number, waive substantive tests, silently reduce scope, or represent a locally tested alpha as a qualified stable release.

## 2. Current repository state

Last inspected checkout:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
Committed HEAD: fbce8b9cfb67b3efdbcd9c66ea5bec7e6470307c
Application: 0.4.0-alpha.7
Database schema: 24
Backup format: 2
State: uncommitted implementation work is present
```

The committed HEAD has not advanced since review15. The important progress is in the **working tree**. Inspect HEAD, status, current diff and applicable repository instructions before editing. If source has advanced, reconcile these findings with the actual code and preserve the newer work.

Preexisting tracked modifications:

```text
internal/api/messages.go
internal/rtc/sfu.go
web/src/lib/components/NamedThreadPanel.svelte
web/src/lib/outbox.ts
web/src/lib/thread-state.ts
web/src/lib/threads.svelte.ts
web/src/lib/workspace.svelte.ts
web/tests/outbox.test.mjs
web/tests/workspace-state.test.mjs
```

Preexisting untracked implementation tests:

```text
internal/api/send_receipt_upgrade_test.go
web/tests/thread-send-outcome.test.mjs
```

Review15 and review16 documents may also be untracked. Do not discard, overwrite, stage indiscriminately or attribute existing changes to yourself. Finish the existing implementation coherently. Inspect what each change does before retaining or revising it. Keep shared schema, authorization and protocol edits serialized; independent tasks can run in parallel if ownership is clear.

The SFU patch now covers callback synchronization **and** candidate-pair diagnostics through the ICE transport, including corrected relay classification. It is larger than the one-line-purpose description in the previous handoff. Preserve the working behavior and inspect the actual diff.

### Read the right documents

1. `planning/completion-review-16-and-v1-next-steps-2026-09-30.md` — current verification, failures and limits.
2. `docs/v1-implementation-plan.md` — full scope, invariants, work packages, reference targets, acceptance matrix and G0–G8 gates.
3. Relevant ADRs and current implementation/tests for the batch you are changing.
4. Review15 only for details of carried-forward defects: `planning/completion-review-15-and-v1-goal-2026-09-30.md`.

This handoff supersedes both prior DeepSeek prompts for execution order. Historical “documentation only” statements describe their original planning tasks; this assignment is implementation. Some summaries in `docs/v1-progress.md` are stale. Source and attributable verification establish implementation facts; the approved plan and decisions establish intended behavior. A code comment or passing helper test is not acceptance of a user journey.

### What has already improved

Retain the existing Go/SQLite server, Svelte/TypeScript client, embedded frontend, shared conversation/send pipeline, account-owned receipts, bounded opt-in outbox, centralized authorization/revisions, WebSockets and embedded Pion media.

Earlier repairs already cover real timeout login/private HTTP scope, non-replaying timeout expiry, private-audit audience filtering, export bounds/feature checks/interrupted-job handling, thread locking at commit, root tombstones, message whitespace, Activity remount/destinations, private-call block/room checks, several publication-routing controls, capture audience fencing, mounted remote-stream binding and TLS/heartbeat support in the capacity client. Do not rebuild these systems or reopen old defects without current evidence.

The new working-tree changes additionally implement a fence before the thread lookup, explicit thread send outcomes, saved `thread_id`, recovery-only handling of ambiguous old outbox entries, pending-thread visibility, unresolved-key lookup and legacy ordinary-receipt fallback. These are partial repairs. Their callers, lifecycle and compatibility boundaries still need work below.

## 3. Current verification and immediate first checkpoint

Fresh verification at the inspected working tree found:

- Frontend suite: **271 tests, 268 passed, 3 failed** after granting local socket access. Initial additional `listen EPERM` failures were environmental.
- Svelte check: **1 error** in `NamedThreadPanel.svelte`; this batch is not ready to build or release.
- Nine repository receipt tests and the earlier ordinary-upgrade probe pass. Five server probe functions still reproduce receipt destination, role, export and thread-state failures; the bounded API batch took **4.829s**.
- Eight selected RTC tests passed under **`-race`**, including forced relay, candidate reporting, publication ownership, microphone/screen independence, screen audio, capacity reservation and failed-negotiation cleanup. This is focused evidence, not a full race-suite or physical-media result.
- Controlled execution with valid UUID fixtures confirms that the delayed account-switch thread lookup makes no POST and that a saved thread operation retries at its original thread endpoint with its original key/text.
- Further lifecycle probes still find cross-account draft restoration, mutable uncertain intent and stale drafts after successful sends. Server receipt compatibility also remains incomplete for older thread receipts.

**Your first checkpoint is to finish the current repair batch and make its meaningful checks green.** Do this before starting unrelated features or rebuilding packages.

### Fix the immediate verification failures correctly

1. `NamedThreadPanel.svelte` infers the initial `crypto.randomUUID()` value as a UUID template-literal type, then assigns a restored generic string key. Give operation identity an appropriate validated type through the flow; do not silence the error with unsafe casts or disable checking.
2. Two new reload/retry tests use account ID `me`, while durable outbox storage accepts UUID account IDs. Use valid identities and assert that data was actually persisted before restoring it. A test that persists nothing cannot establish recovery.
3. The existing future-timestamp test disagrees with the new `operationExpired` behavior. Resolve the intended clock-skew/retry policy against the receipt and finite-retention contract, then align code and tests. Do not simply change an assertion to match whichever implementation is present. An arbitrarily future timestamp must not make automatic retry or retained text effectively unbounded.

Run the relevant tests while correcting these issues, then the full frontend unit suite and Svelte check. Preserve exact failures until their cause is understood. Formatting/build checks follow once the batch is coherent.

Start with these focused frontend tests, using `web` as the working directory:

```sh
node --test tests/outbox.test.mjs tests/workspace-state.test.mjs tests/thread-send-outcome.test.mjs
npm run check
```

From the repository root, the three new receipt tests can be selected with:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test ./internal/api \
  -run '^(TestTheLegacyDigestEncodingIsTheRecordedOne|TestAnUpgradedReceiptStillAnswersItsUnchangedOrdinaryRetry|TestAThreadReceiptKeepsItsThread)$' \
  -count=1 -timeout 3m -v
```

Those three tests already pass; expand them to cover the missing historical destinations below rather than treating their existing success as completion.

## 4. Finish thread author intent across the actual UI lifecycle

### What is already fixed narrowly

The new Workspace captures identity before the preliminary thread GET and checks it again before pending-state creation and POST. With valid UUID fixtures, the outbox now preserves `thread_id`, restores the original thread endpoint, and clears after successful reconciliation. Credit these controls and keep their regressions.

### Remaining reproduced lifecycle failures

**Account cleanup can move a draft into another account.** A controlled lifecycle sequence calls `workspace.stop()`, then the old thread panel's cleanup. `captureDraft` writes A's text into the newly replaced `composerDrafts` map. Account B can subsequently restore that text. Account isolation must include component teardown, not only requests and storage keys.

**An uncertain operation can change its text while keeping its key.** After a lost-response outcome, the composer permits edits and submits with the unresolved operation's old key. The current path replaces the outbox text. The key is supposed to identify one immutable intent; overwriting it destroys the ability to reconcile what may already have committed.

**A successful send can leave an old saved draft behind.** Reopening after success can restore the previously saved text under a fresh key, making an accidental duplicate possible. Clearing the visible textarea alone is insufficient.

**A dropped operation needs an explicit component outcome.** The controller now distinguishes `sent`, `refused`, `uncertain` and `dropped`, but the panel still infers success from the absence of `sendError`. Complete the outcome contract all the way to the composer. Only an actual accepted result should perform the corresponding successful-send cleanup.

The first three lifecycle failures were reproduced with actual transpiled controller/workspace and extracted component functions under controlled ordering. The dropped-outcome mismatch is source-confirmed only. None establishes the result of a rendered browser journey. Add the missing browser acceptance once implementation is corrected.

### Required design properties

- Associate each mounted composer, draft snapshot and pending operation with the initiating account/session and destination. Validate that ownership before writing during send completion, effects, teardown or navigation.
- Stop stale components from mutating new account state. Preserve a recoverable old-account draft only in its legitimate account scope, according to the existing opt-in persistence preference.
- Keep immutable submitted intent separate from editable draft text. An unresolved operation's account, key, thread/conversation, content and attachment claims cannot be rewritten by a later edit.
- If the author edits after uncertainty, provide a clear interaction that preserves the original operation for reconciliation and treats new text as distinct work only when appropriate. Do not silently create a fresh key for possibly committed work.
- Clear the correct saved draft after success, without erasing newer text typed while the request was pending. Handle both in-memory and persisted draft representations.
- Keep failed/pending rows visible only in their proper named thread, with useful retry, copy/recovery and discard behavior.
- When a legacy saved entry has no recoverable destination, retain its author text for explicit recovery; never infer a thread or channel from the active view.
- Bound all retained operation and recovery data by the established count, bytes and age limits. Preserve account isolation when storage is unavailable or full.

### Acceptance matrix for this checkpoint

Use real UUID fixtures and assert stored effects, endpoint, key and message count, not just return status.

| Scenario | Required result |
| --- | --- |
| A starts lookup; account/session changes before completion | No B POST, B pending row, B outbox entry or B draft contamination |
| Lookup fails or the thread becomes unavailable | No fallback to the active channel; author receives an accurate outcome |
| Failure before commit, close/reopen and reload | Retry keeps exact original intent and creates one message in the original thread |
| Commit succeeds but acknowledgment is lost | Same-key retry returns the original message; no duplicate or parent-channel message |
| Author edits after an uncertain send | Original unresolved intent remains intact; edited draft does not overwrite it |
| Authority refresh while an operation is pending | Permitted recovery remains possible without treating stale UI completion as success |
| Definite refusal | Text remains recoverable; no fabricated sent row or hidden successful state |
| Successful send with an older saved draft | Reopening does not resurrect the sent draft |
| New text entered while earlier text sends | Successful completion does not erase the newer draft |
| Old panel cleanup runs after stop/sign-out/account switch | It cannot write into the new account's stores or browser storage |
| Pending named-thread operation | Visible in that thread, absent from ordinary channel history |
| Ambiguous legacy entry or expired operation | Explicit bounded recovery; no automatic destination guess or replacement send |

Test the Workspace and controller for deterministic ordering, then prove the critical outcomes through the rendered panel and supported API. A mock returning a correctly shaped result cannot prove that the production caller supplies or consumes it correctly.

## 5. Complete server receipt compatibility without changing destinations

The new `legacySendIntentDigest` correctly matches the old ordinary-message encoding, but its comments assume that pre-change thread receipts never existed. That assumption is false: named threads existed before thread identity was added to the digest.

The incomplete fallback can refuse an old thread operation at its original thread endpoint while accepting its legacy digest at the ordinary channel endpoint. Older backfilled receipts with an empty digest also need a destination check. Do not call the entire compatibility problem solved because the new ordinary-message fixture passes.

### Required work

1. Establish the actual historical encodings and migration/backfill states from source and populated fixtures. Separate current digests, legacy nonempty digests and blank backfilled digests.
2. Bind replay to the complete destination, including the original thread where one exists. Use legitimate retained result metadata or a reviewed forward migration/versioning approach where necessary.
   Preserve the distinction between original intent and present message placement: an ordinary root may have been promoted into a thread later. A check of the message's current `ThreadID` alone can reject a legitimate original channel operation or approve the wrong destination. Include promoted roots in the compatibility fixtures.
3. An unchanged surviving old thread send should resolve to its original message through its original thread. It must not replay through a sibling thread or the parent-channel send endpoint.
4. Retain the ordinary old-receipt repair, canonical attachment ordering and conversation/content/reply binding.
5. Preserve deleted/erased/expired outcomes. If evidence of old intent or destination has been deliberately erased, return the documented terminal/recovery result. Do not recreate content, clear fingerprints wholesale or interpret missing information as permission for different work.
6. Correct misleading compatibility comments and add behavioral tests covering the real historical possibilities.

### Required receipt tests

Cover old ordinary and old thread receipts, current ordinary and current thread receipts, empty-digest backfills, ordinary roots later promoted into threads, changed content, changed conversation, sibling thread, parent-channel replay, reordered attachments, deletion and expiry. For allowed replay, assert the original ID, destination and one stored message. For refusal, assert no new message, attachment claim, notification or replacement operation.

Use focused fixtures for fast development and a populated actual-prior-version upgrade for final acceptance. Label those evidence levels separately. Avoid tests whose “legacy” value is generated solely by the same new helper being tested; pin historical encoding or use preserved prior output.

**Checkpoint:** the compiler/tests and the full author-intent/receipt matrix pass; the uncommitted tranche has been reviewed and saved coherently. Then proceed to the remaining authority blockers.

## 6. Repair current authority and server-held delivery boundaries

### Effective role changes

A manager with `role.manage` but without `message.send` can remove the target's only read-only role using `role_ids: []`. Default-role fallback then makes the target's send succeed, although the manager could not explicitly grant that power.

Authorize the effective final authority change, including implicit default capabilities and intermediate revoke/grant transitions. Reconcile the chosen default-role semantics with the approved additive model and invariant that removing an ordinary grant must not increase authority. Do not fix only one empty-array route while leaving equivalent role deletion, replacement, default-role edits or account lifecycle transitions unsafe.

Prove permitted role management still works. Test explicit and implicit holders, actor hierarchy, final-assignment removal, multi-role replacement and undelegable capabilities. Preserve the designated owner's recovery authority. Keep the test at the real role endpoint and assert the resulting capabilities and actions.

### Export admission and revocation

Current export delivery validates scopes, commits, then registers with an empty channel scope. Role-permission changes skip that registry entry. A held export can deliver withdrawn channel content while a new history request is denied.

Represent the export's complete content scope and admit delivery through the existing authorization fence/registry protocol. Cover the interval between checking and registration as well as cancellation after registration. Do not hold database/authority locks across slow network writes.

Test access withdrawal before admission, after registration and during transfer. Cover relevant role/channel visibility, membership, account/session and feature-gate changes. Prove an unchanged permitted export still downloads. A registry entry existing is not proof that cancellation works. Keep current bounds, expiry, orphan cleanup, erasure and interrupted-job recovery. The contract controls server-held copies still being delivered; it does not recall a previously legitimate download.

### Timeout audit and live enforcement

The community/private HTTP scope and non-replaying expiry repairs already pass their selected tests. Apply/lift still need the promised audit and appropriate live enforcement; bulk principal construction used for media does not currently carry `Suspended`.

Use a coherent policy at admission, revalidation and forwarding. Apply the documented community restriction without turning it into a ban on permitted private contact. Prove live community participation stops, retained/private access and allowed private calls remain correct, bans/deactivation still deny, and expiry never resurrects revoked roles. Audit the committed change accurately and once; failed mutations must not produce misleading success records.

## 7. Complete threads and everyday member workflows

### Thread state and navigation

- Integrate `thread_reads` with actual unread/mention projections and client state. Reading a thread reply must not leave a phantom channel unread; reading a channel must not silently consume an independent thread cursor.
- Archive must stop new replies per ADR0017, including commit-time races. Preserve root tombstones and acknowledged replies. Offer Reopen and distinguish it from lock/unlock.
- Make the full 1,000-reply fixture navigable with a bounded rendered window and bounded cache. Current accumulation/re-reading of every loaded page does not meet this contract.
- Preserve scroll position, permalinks, active/archived lists, follow/activity behavior and access withdrawal during navigation.

### Activity acknowledgment

The current client and server acknowledge different rows: visible `05,04,03` leads the server to mark `02,01`, while the UI paints `04,03`. Define one explicit acknowledgment set/range contract. Persist exactly the intended rows and reflect the same result locally. Verify reload, older pages, concurrent arrivals, multiple devices and access loss. Preserve repaired remount and destination navigation.

### Private evidence queue

With 120 message reports and 70 request reports, all 190 arrive but the component never finishes because a skipped exhausted source retains a cursor that the merge helper interprets as active. Preserve source exhaustion independently of cursor values. Use `(source_kind,id)` consistently for list rows, notes, selection and busy state. Exercise the actual component call pattern, pagination boundaries, colliding IDs and changing access. End after the complete unique set without repeatedly fetching an exhausted source.

### Notifications and member export

Fix the `brrp.notified.*` versus `brrp.notify.*` retention mismatch and shared atomic notification admission across app/worker. Verify bounded cleanup, duplicate suppression, preferences, active-view suppression and current-access deep links.

Finish member export reauthentication, stable request retry identity, visible building/failure/expiry states and successful download. Its browser journey currently permits early success on 401/403 and looks for a button where the UI renders a link. Assert a ready export with the expected permissible data, exclusions and a real downloaded result. Do not substitute an error path for export acceptance.

### Remaining group and account journeys

Verify current connected clients through invitations, acceptance, membership removal, owner transfer/disposition, account deactivation/removal, block policy, terminal-group retention and reporting. Test outsider access and former-member caches/events/files alongside positive member controls. Preserve already-working group behavior and the agreed full-history disclosure policy. Correct user-facing deletion/retention language to match actual storage behavior.

Complete required focus/keyboard, mobile/reflow, loading/error, recoverability and accessibility states throughout these journeys. Follow the master's remaining WP-09–17 acceptance criteria rather than treating this defect list as the entire product specification.

## 8. Complete media acceptance while preserving current repairs

The current SFU patch and eight focused race tests are useful progress. Do not replace the media architecture or repeat old repairs without a current failure. Extend the embedded Pion publication model and operator-controlled STUN/TURN configuration already chosen by the project.

Prove the actual two-browser producer/consumer journey: decoded remote frames, independent microphone and optional screen audio, late join, listen-only reception, stop/restart, move/reconnect and permission withdrawal. Preserve full owner/room/connection/publication identity through admission, renegotiation, forwarding and replacement. Stale tracks or old capture chooser results must never publish into a new audience.

Verify stopping a share releases video/audio publications, captures, senders, receivers and reservations. Exercise non-cooperative senders at the forwarding boundary. Establish actual forced-relay behavior and correct diagnostics, not merely a “connected” state or a configured TURN URL.

Resolve current implementation limits against the approved target: twenty participants serverwide, at most ten per room/call including viewers, one screen per room and two serverwide. Current ten-serverwide and private-call bounds do not establish that target. Implement and measure the approved profile or obtain an explicit scope decision; do not silently lower it.

Keep evidence categories distinct:

- Registry/unit tests establish bookkeeping contracts.
- Synthetic RTP tests establish measured forwarding behavior.
- Browser-generated streams establish automated browser transport/rendering behavior.
- Real microphones, desktop chooser flows, sound, legibility, device/network changes and long calls require physical acceptance.

Qualify supported desktop browsers and mobile viewing, actual UDP-blocked/relay paths, the defined impairment profile, two-hour media sessions and 100 capture start/stop cycles. Record route, browser/device, frame/audio outcome, resource growth and limitations. No synthetic count substitutes for perceived quality or physical capture cleanup.

## 9. Make upgrades and recovery trustworthy

### Use the preserved real schema13 executable locally

```text
Path: dist/brrpbrrp-p9-checkpoint
SHA256: fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f
Embedded clean revision: 2869db55394b2b2a004424672517b38d6908586b
Verified supported schema: 13
```

The hash was checked again in review16. Preserve these bytes. This local baseline is available; archive hosting is not a prerequisite for local qualification. The `0.3.0` archive writes schema 2 and cannot seed the schema13 private-state rehearsal.

Hosted Linux CI needs a correctly identified runnable baseline for that environment. Distinguish its acquisition/hosting/provenance requirement from fixture work that can already run locally. Do not pass a different platform's executable or current source off as an archived schema13 release.

### Repair the fixture and workflow

1. Seed accepted private history and submitted evidence using the prior version's supported API. The current seed does not create evidence required by its verifier.
2. Fix wrong baseline selection in CI and make `--report` produce actual machine-readable results, including failure information. Retain exact baseline identity checks.
3. Complete ownership, privileged MFA and reviewer grants/revocation in the disposable fixture through supported APIs. Remove SQL fallback mutations followed by unconditional success. A disposable operator ceremony is locally implementable.
4. Fix export restore assertions: the API returns a raw array, not `body.exports`. Capture original IDs, metadata and ready-state preconditions; prove preserved history, account isolation and the documented missing-file 410 behavior.
5. Verify exact restored group ownership/membership/invitations, thread roots/tombstones/follows/read positions, activity/preferences/acknowledgments, moderation, evidence and export state.
6. Prove post-snapshot reconciliation of bans/deactivation, role/member removal, password/session changes, blocks, erasure and reviewer access before cutover. Disclose any unreconstructable change.
7. Include targeted negative controls: deleting/corrupting an asserted fixture item must cause its check to fail. HTTP 200, empty-array fallback and aggregate counts alone cannot prove preservation.

Run populated supported-prior-version upgrades, current-schema recovery, interruption, repeated attempts, newer-schema refusal and rollback through documented materials. If implementation advances schema 24, qualify populated 24→new and relevant older migrations. Preserve IDs, row relationships, search, attachments and acknowledged operation outcomes.

Recovery JSON must identify exact source/dirty state, executable hash, schema/backup versions, fixture identity, command, start/end, exit status and limitations. Keep baseline and candidate identities separate. A local fixture result is not proof of deployed RPO/RTO.

## 10. Integrated validation and reproducible distribution

### Test at the right scope

During implementation, run focused regressions and affected checks. After a coherent integrated source state, run required gates:

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
.tools/go/bin/go vet ./...
.tools/go/bin/go test -race -count=1 -timeout 40m ./...
```

Known local paths, subject to rediscovery:

```text
Go: .tools/go/bin/go
Go build cache: /private/tmp/brrpbrrp-go-build
Go module cache: /Users/cameronamer/go/pkg/mod
Browser cache: /private/tmp/brrpbrrp-playwright-browsers
```

Use repository scripts for both browser feature modes, fresh recovery, populated upgrades/rollback and extracted package consumers. Read current arguments instead of guessing them. Use a freshly built server with its matching embedded frontend and the intended private-feature configuration. Record skipped projects as gaps, not passes.

Local socket denial and missing dependencies are environmental failures. Diagnose them explicitly and use the permitted execution path. Do not hide genuine assertions behind environmental noise. Never launch the full 40-minute race suite after every small change; rerun broad gates when a coherent candidate or a concrete affected risk requires them.

### Produce one coherent new prerelease

Existing alpha.7 archives mix schema 21 and schema 14. Preserve historical artifacts and hashes. Choose a **new unique prerelease version** after integration, synchronize the version identities and build from reviewable, identified source. Do not overwrite alpha.7 again.

Record source commit, clean/dirty state, dependencies, toolchains, configuration, app/protocol/schema/backup versions, native executable/archive hashes, source archive identity and container digest. Verify extracted native packages and the extracted source consumer, not only files still in the developer checkout. Qualify the actual container image on Docker before claiming that path works.

Align Windows update documentation with supported stop/backup/replace/start/verify behavior, including failed replacement and rollback. Keep operator/member/contributor documentation current and remove stale claims from active summaries without rewriting historical evidence.

## 11. Complete deployment and release acceptance

### Settled scope and remaining inputs

Both **Windows x64 native/Tailscale** and **Linux amd64 Docker** are required. Desktop sharing, mobile viewing, group/private calls, threads, notifications, moderation and member recovery are in v1 scope. MIT and the project's established contributor terms remain in force. Do not keep asking the owner to choose between the two required hosts or reopen settled architecture/licensing.

Camera video, federation, E2EE, native mobile apps, bot marketplaces, multi-community hosting, enterprise SSO and clustering remain deferred unless the owner explicitly changes scope.

Request genuinely missing hosts, credentials, reviewers, physical devices, backup custody or publication identities precisely: name the gate, prepared procedure, expected output and input needed. Continue independent local work while waiting. Do not claim host runs, signatures, delivered alerts or human acceptance that did not happen.

### Reference capacity and operations

Freeze the approved workload and support matrix before measurement. The master targets include:

| Dimension | Qualification target |
| --- | --- |
| Accounts and activity | 250 registered, 50 concurrently active |
| WebSockets | 100 total, with per-account admission limits |
| Group membership | Ten accepted members |
| Media | Twenty serverwide, ten per room/call including viewers |
| Screens | One publisher per room, two serverwide |
| Retained data | One million messages and 20 GiB attachments |
| Reference allocation per host | Four dedicated cores/vCPUs, 8 GiB RAM, at least 80 GiB free SSD and measured 100 Mbit/s symmetric link |

These are qualification targets, not established capacity or a universal minimum specification. Use a separate load generator and record actual hardware, OS/runtime, topology, configuration and competing load.

On each required deployment, prove clean installation, trusted HTTPS, service/container permissions and lifecycle, persistence across restart, update and failed-update rollback, disk/network/process faults, target two-hour peak and 24-hour mixed soak, realistic private/media traffic, overload rejection/recovery and bounded resource growth.

Verify actual external monitoring and delivered alerts, including backup-age and outage scenarios. A local alert receiver test is not proof that an operator is notified.

Use independent encrypted off-host backups with documented key custody and an alternate operator. Measure **RPO ≤24 hours** and **RTO ≤60 minutes** from the declared recovery event, including operator response, retrieving/decrypting the copy, security reconciliation and validation. Report total outage separately. Restore to clean destinations on both required paths and verify identities, permissions, search and attachment hashes.

### Independent acceptance and observation

Complete independent security/privacy review, human accessibility/browser acceptance, non-author installation/recovery and clean-source contributor rehearsal. Close critical/severe findings and disposition lesser issues according to the master's rules.

After prerequisite gates, recruit at least **three communities** spanning the required deployments and relevant desktop/phone clients. Complete the planned **fourteen-day beta**, including real critical journeys, calls, moderation, backups, alerts and operator rehearsals. Then freeze the final source/dependencies/schema/protocol/configuration/artifacts and observe the final candidate for **seven consecutive days**.

Apply reset rules honestly: a material executable/dependency/schema/protocol change creates a new final artifact identity and restarts the final observation; severe beta defects restart affected clean observation as specified. Do not invent elapsed days or carry affected evidence forward silently.

### Gate ledger

| Gate | Required outcome |
| --- | --- |
| G0 | Explicit approved baseline, scope, trust, owners, environments and budgets |
| G1 | Complete privacy boundary and verified data/upgrade foundation |
| G2 | Accepted DM/group integration, membership, blocking and lifecycle |
| G3 | Complete threads, activity/notifications, moderation and core UX |
| G4 | Qualified voice/private calls, sharing and capture/network lifecycle |
| G5 | Qualified required hosts, recovery, monitoring, capacity and artifacts |
| G6 | Independent acceptance and successful real-community beta |
| G7 | Final distributed candidate and seven-day observation accepted |
| G8 | Explicit publication approval, verified release/rollout and maintenance handoff |

Current evidence does not close these gates. Every closure needs its actual source/artifact, scope, result and accountable acceptance. Present the concrete final release materials for publication approval; do not stop earlier merely to ask permission for routine local repairs. After authorization, verify published downloads, signatures/checksums, staged upgrades, release notes, security contact and maintenance ownership.

## 12. Debugging and continuation discipline

Keep one primary failure under investigation at a time. After two unsuccessful attempts:

1. Run the single relevant test with fresh artifacts and record exact source, assertion and actual output.
2. Inspect the current code/diff. Confirm a scripted edit matched and landed rather than assuming it did.
3. For browser failures, inspect that exact context's DOM, account, URL, response and permission state. Do not diagnose one account from another account's screenshot.
4. Distinguish fixture/setup error, implementation error and stale assertion. State one falsifiable explanation and gather discriminating evidence.
5. Make the smallest coherent correction, rerun the focused behavior, then expand only to the affected contract.
6. If blocked by a real external dependency, record it with a ready procedure and move to independent work.

Do not manufacture a pass by deleting assertions, accepting success-or-failure, returning early on 401/403, lengthening waits without a timing diagnosis, broadening permissions, bypassing ownership through SQL or checking only that something “is not 403.” Require exact intended outcomes and meaningful permitted/denied controls.

Keep a compact ledger for each tranche:

```text
Observed failure and reproduction
Source and working-tree identity
Changed files and reason
Exact commands, exit status and evidence paths
What the evidence proves and does not prove
Remaining issue or external input
Next executable action
```

Save coherent verified changes before large context switches, using the repository's normal commit/checkpoint practice. Do not mass-stage unrelated work. If context or runtime limits force a stop, save enough detail for the next turn to resume the current implementation without another broad planning cycle. A checkpoint is not goal completion.

Your first response should briefly state the actual source/dirty state and first failing check. Then **finish the existing thread/outbox/receipt batch**, beginning with the Svelte error, valid recovery fixtures, component teardown account isolation and immutable uncertain intent. Produce a verified first checkpoint, report its evidence briefly, and continue through the remaining product and release work.
