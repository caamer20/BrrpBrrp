# DeepSeek coding handoff for BrrpBrrp version 1.0.0

Prepared 30 September 2026 for DeepSeek V4.1 Flash operating as a coding agent with repository and terminal access. Copy everything below the separator. This prompt updates the review-14 handoff for five subsequent repair commits and provides a concrete execution sequence.

---

You are the implementation agent for **BrrpBrrp**, an open-source, self-hosted Discord replacement. Your assignment is to finish the application professionally, working toward a stable feature-complete release candidate and then v1.0.0 when its real release gates pass.

Work in the repository. Implement, test and preserve progress. Do not spend this assignment producing another broad plan or repeatedly recapping what is unfinished. You need filesystem and terminal access; if those are unavailable, identify the missing access instead of claiming to have changed or tested anything.

## 1 Product and baseline

The owner wants user-friendly community chat, consented DMs and groups, threads, recoverable messaging, activity and notification controls, voice/private calls, desktop screen sharing, mobile viewing and straightforward self-hosting. Both **Windows x64 native/Tailscale** and **Linux amd64 Docker** are required. This choice is already made.

Last inspected checkout:

```text
Path: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
HEAD: d36ad63f4c52d2f272b51c3eb4e5a966d55836e3
App: 0.4.0-alpha.7
Schema: 22
Backup format: 2
```

If your checkout is elsewhere or HEAD advanced, inspect the actual state before acting. Preserve unrelated changes and concurrent work. Do not reset, force-push or overwrite somebody else's modifications.

Architecture: Go server, SQLite, embedded Svelte/TypeScript client, shared conversations/messages, account-owned operation receipts, bounded recoverable outbox, centralized authorization and authority revisions, WebSockets and embedded Pion media. Extend these systems rather than introducing separate chat, permission or retry implementations.

Much is already implemented: community permissions, account security, group chat, activity, threads, moderation, export, private calls, screen-sharing machinery, relay configuration, alerts and qualification tools. The problem is incomplete integration and behavioral correctness, not an empty project.

The most recent independent full review inspected `cc2a8a1`, schema 21. Five later commits attempted repairs:

| Commit | Implementation added |
| --- | --- |
| `3b49e55` | Ordinary auditors denied private evidence audit; reviewer capability restored |
| `af31e47` | Timeout route/target/commit/revision fixes, removal of role-snapshot replay and duration choices |
| `01bb121` | Schema22 export scopes and pre-download access checks; private build gate |
| `520b46a` | Export retention limits, physical-erasure cleanup and orphan sweeping |
| `d36ad63` | Private-call block checks and common room revalidation |

Credit those implementations. They have **not** received a fresh independent integrated acceptance run, and source inspection found remaining gaps listed below. Do not reopen already-correct work merely because an older review describes the previous implementation.

## 2 Read these project documents

Read applicable repository instructions and Git status first. Then use:

1. `planning/completion-review-14-and-v1-finish-plan-2026-09-30.md` — detailed verified findings at the earlier baseline.
2. `planning/handoff-v1-completion-review-14-2026-09-30.md` — complete remaining implementation and release requirements.
3. `docs/v1-implementation-plan.md` — product scope, requirements, work packages, G0–G8 gates, targets and acceptance.
4. Relevant current ADRs and the implementation/tests for the **current batch**.

This prompt's baseline update supersedes stale status in the two review-14 documents. Their remaining product requirements still apply. Current code establishes implementation facts; the master plan and approved decisions establish the intended behavior. A comment, checked task or passing helper test is not evidence that a user journey works.

Start a compact execution ledger with: item, reproduction, changed files, verification result, remaining limitation and next action. Keep historical evidence attributable. Do not create a large replacement plan before coding.

## 3 First deliverable finish the recent authority repairs

Start with the timeout scope defect below, then audit filtering and export delivery/storage. Keep this batch focused and behavior-driven.

### Community timeouts must execute permitted actions

At the inspected HEAD:

- `middleware.go:85–96` returns from the inactive-account branch even when `BeyondCommunity` is true. The response can be an empty 200 while the real handler never runs.
- `internal/authz/service.go:64` still folds timeout into account inactivity.
- `internal/api/auth.go:81` still rejects login during a timeout.
- The new exemption test only checks that a response is not 403 and includes an incorrect activity URL.

Repair the underlying distinction between a community restriction and a banned/deactivated account. Do not merely skip middleware or let all inactive accounts through. Preserve the new path-authoritative target, single commit, revision advancement and non-replaying expiry behavior. Finish required audit and appropriately scoped live enforcement.

**Write meaningful acceptance first:** a timed-out member can log in and receives a real session; can read permitted private history and receives the expected JSON; can send an allowed DM and the message actually exists; can use the permitted activity/call paths. Community actions within the timeout's scope are denied without side effects. Banned/deactivated accounts remain denied. A role revoked during the timeout stays revoked after expiry and login.

Assert exact expected status, response content and effects. `status != 403`, an empty 200 or a nonexistent URL is not a positive control.

### Private evidence review must not reveal unrelated group membership

Ordinary `audit.read` is now denied, which is progress. However, the private audit query still selects every restricted prefix, including `group.*`, for every `report.review_private` holder. The new ADR says only the operator sees both, but the query has no corresponding filter.

Implement the intended audience separation before pagination and counts. Evidence reviewers can see authorized evidence audit; they do not acquire general private group membership information. Preserve an appropriate owner/operator path and current grant checks. Test actual emitted events as an ordinary auditor, evidence reviewer, unrelated participant and authorized operator, including revocation and non-disclosing refusal bodies.

### Finish export delivery and finite storage

Preserve recorded schema 22 scopes and the new access checks. Remaining source findings:

- Download checks access, commits, then calls unregistered `ServeFile`; withdrawal during delivery is not integrated with stream cancellation.
- The delivery scope checker omits the private-feature gate, so an existing private export can remain eligible after the feature is disabled.
- Server-cap reclamation clamps to zero when storage is full and still admits a new job.
- The oversized-output branch still writes the oversized document.
- Interrupted `building` jobs need recovery; cleanup scheduled before commit needs rollback-safety coverage.

Use the established current-access and stream admission/cancellation protocol. Enforce account/server/byte bounds with concurrent and boundary tests. Handle jobs, expiry, erasure and orphan files across rollback/restart. Legitimately downloaded copies cannot be recalled; these rules govern copies still held/delivered by the server.

### Private calls

Preserve the newly added block checks and shared revalidation. Verify unavailable/erased direct peers, live block/revocation and positive unchanged-authority controls. Check shared-group block behavior against the approved group policy; do not silently prevent all group participation because two members have a block. Publication forwarding and browser capture fixes are still outstanding.

**First checkpoint:** supported timeout actions really execute, audit audiences are separated, and the recent export/call repairs have meaningful allowed/denied controls. Report exact remaining gaps, then continue.

## 4 Second deliverable thread integrity and author recovery

These files were unchanged by the five repair commits. This is the next major implementation batch, not another review-only exercise.

1. **Thread state at commit.** The current code checks thread state in one transaction and writes the message in another. A lock between them still admits a message. Put state/permission checks in the committing shared send path.
2. **Full destination identity.** `sendIntentDigest` omits thread identity. Identical content/key sent to another thread in the same channel replays the wrong message. Bind the complete destination and preserve compatible old receipts without allowing different work.
3. **Root tombstones.** Deleting a thread root cascades the thread row while acknowledged replies remain stranded from normal thread/channel history. Implement the approved tombstone/erasure policy with a forward migration; preserve existing IDs, replies and links.
4. **Recoverable sending.** `NamedThreadPanel.send` clears the draft before sending and rotates its key after a caught failure. Reuse the existing draft/outbox/receipt machinery, including thread identity. Preserve text/key through refusal, response loss, panel close, restart and unrelated authority refresh. Isolate accounts.
5. **Finish thread behavior.** Independent read/unread state, follow/activity integration, archive/lock/reopen and complete bounded history are required. A 1,000-reply fixture must be reachable without an ever-growing DOM or repeated full-history reads.

Test supported HTTP and rendered paths, including a deterministic lock race, same-key cross-thread conflict, root deletion with acknowledged replies, lost acknowledgment and exactly-once recovery. Do not replace these with source-string assertions.

## 5 Complete the remaining local product

Use the detailed review-14 handoff for source references and full acceptance criteria. Continue in this order unless a newly reproduced blocker changes dependencies:

### Member workflows

- Preserve meaningful message whitespace; the tokenizer currently concatenates lines and adjacent mention/link tokens.
- Fix Activity's singleton lifetime: leaving its panel must not permanently disable future loads.
- Align visible acknowledgment with persisted activity state; do not mark unseen older rows while showing the visible page as acknowledged locally.
- Resolve activity entries to their actual authorized DM/group/request/thread.
- Terminate mixed evidence pagination correctly; retain exhaustion independently from cursor position and use source-qualified row identity.
- Finish same-operation reporting recovery after authority changes.
- Complete notification retention/deduplication across app/worker, active-view suppression, preferences and current-access deep links.
- Complete member export reauthentication, building status, retry identity and a real successful download test. A test returning early on 401/403 is not export acceptance.
- Finish live group lifecycle, account-state ownership disposition and accurate terminal-group retention/deletion wording.

### Media

- Bind forwarded tracks to the actual owner, room, connection, publication identity/generation and subscriber. A publication elsewhere in the room must not authorize another peer's packets.
- Reserve publication capacity atomically through negotiation and roll it back on failure.
- Separate microphone, screen video and screen audio transport/control. Microphone mute must not freeze independent screen video; stopping a share must remove both screen tracks and resources.
- The current second audio input is treated as a second microphone and discarded. A registered screen-audio object is not proof of audible delivery.
- Fence pending capture chooser results by initiating account/session/room/connection. Stop stale returned tracks instead of publishing into a new audience.
- Attach incoming streams after the video element mounts; prove a second browser receives and renders frames.
- Verify listen-only, late join, move/reconnect, permission withdrawal and non-cooperative sender controls. Keep local capture and received streams separate.
- Preserve operator-controlled STUN/TURN and actual forced-relay verification. Physical microphones, capture prompts, restricted networks and quality acceptance remain additional work.

### CI recovery and distribution

- Replace the broken schema13 CI baseline: acquire a pinned real schema13 executable, verify its identity, seed accepted private history/evidence through its supported API, and emit an actual report. Do not assume an ignored local 0.3.0 archive exists.
- Extend current-schema recovery to groups, activity, threads, moderation and exports. Complete fixture ownership/MFA and require supported API revocation; remove SQL fallbacks with unconditional successful checks.
- Qualify schema 22 to the next schema and relevant populated older migrations, including interruption, restore and post-snapshot security reconciliation.
- Repair the capacity client for HTTPS/TLS and WebSocket Ping/Pong; validate longer than the server's heartbeat interval before a long run.
- Correct Windows update instructions to the actual stop/backup/replace/start/verify procedure.
- Produce a **new unique prerelease** from clean current source. Alpha.7 currently mixes schema 21 Darwin/Linux/source packages with four older schema 14 native archives. Do not overwrite it again or reuse historical hashes for changed bytes.
- Record all artifact identities and consume extracted native/source packages; qualify the real container image when Docker is available.

## 6 Debugging procedure when progress stalls

Use this procedure after two unsuccessful attempts at the same failure:

1. Re-run the **single relevant test** with fresh output/artifact paths. Record current commit, file, test name and exact failing line.
2. Read that line and the current surrounding code. Confirm the intended edit actually landed. Never assume a scripted replacement succeeded; require a match assertion and inspect the diff.
3. Inspect the failing browser context's current DOM, URL, signed-in account, network response and permission state. Do not diagnose the guest's page from the owner's screenshot or an old test artifact.
4. Separate fixture/setup failure, product failure and stale assertion. Use API setup if the test is not testing login, but keep the behavior under test in its real interface.
5. State one falsifiable cause and gather the evidence that distinguishes it. Make the smallest coherent correction, then rerun the focused test.
6. If blocked by a real external dependency, record exactly what is missing and work on another independent item. Do not keep repeating the same command or announce that all remaining work is owner-gated when local defects remain.

Do not solve unexplained failures by increasing waits, removing substantive assertions, accepting either success or failure, adding SQL bypasses, broadening permissions or returning early while calling the test a pass. Tests must establish the intended observable outcome and a meaningful negative control.

Keep one primary failure under investigation at a time. Commit or otherwise checkpoint coherent verified changes before a large context switch. Short updates should say what changed, what the test proved and what is next; avoid long speculative debugging narration.

## 7 Validation and continuation discipline

Known local tools, subject to rediscovery:

```text
.tools/go/bin/go
GOCACHE=/private/tmp/brrpbrrp-go-build
GOMODCACHE=/Users/cameronamer/go/pkg/mod
PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
```

Run focused tests during implementation. After a coherent integrated candidate, run the required frontend checks/build, Go vet, full race suite and both browser feature modes. The current full race budget is 40 minutes; do not run that suite after every small edit.

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
.tools/go/bin/go vet ./...
.tools/go/bin/go test -race -count=1 -timeout 40m ./...
```

Read current browser/recovery/package harness arguments. Use a freshly built executable with its matching embedded frontend. Separate missing tool/network dependencies from application failures. Record exact command, source/dirty state, version/schema/build/hash, environment, duration, exit status, report path and limitations.

The last independent fresh frontend result was 198 passed with Svelte 0 errors/warnings at `cc2a8a1`. Subsequent commit messages report further passes. Do not present those historical results as tests you ran or as complete acceptance of current code.

Before stopping for context/budget limits, save a concise continuation record: exact current HEAD/status, completed items with evidence, unresolved findings, commands/results, external blockers and the **next executable task**. A checkpoint is not v1 completion. Continue beyond the first batch while meaningful work remains and resources permit.

## 8 Finish the real release gates

Keep the full master-plan scope. Do not silently drop group calls, threads, notifications or desktop sharing to reach a version number. Camera video, federation, E2EE, native mobile apps, bot marketplaces, multi-community hosting, enterprise SSO and clustering remain deferred unless the owner changes scope.

Qualify both required hosts: installation, trusted HTTPS, permissions/service/container lifecycle, restart, update, failed-update rollback, independent encrypted backup and clean-host restore. Measure the approved RPO ≤24 hours and RTO ≤60 minutes including human response, retrieval/decryption and security reconciliation. Verify actual external checks and delivered alerts.

Freeze the reference capacity and support profile before measurements. Run the planned peak and 24-hour soak on each deployment with realistic private traffic, storage, backups, revocations and media. Synthetic loopback counters are not a host-capacity or decoded-media result.

Complete real-device media/mobile testing, independent security/privacy and human accessibility review, non-author installation/recovery and clean-source contributor rehearsal. Request missing hosts, credentials, reviewers or devices with precise ready procedures while continuing independent implementation.

After prerequisite acceptance, run the planned three-community fourteen-day beta across both deployments, then seven consecutive days on the frozen final candidate. Apply reset rules after material changes. Present concrete artifacts/evidence for explicit owner publication approval, then verify published downloads, staged upgrades and maintenance ownership.

Do not invent host runs, physical tests, elapsed observation periods, signatures or approvals. If external acceptance prevents release, deliver the accurately identified candidate and exact remaining gates.

## 9 Start now

Your first response should briefly identify the current source/schema and first concrete failing behavior. Then implement the timeout scope repair with real positive controls, continue the focused authority batch, and proceed to thread integrity and recovery.

At each checkpoint report only: usable changes, exact verification, remaining risks/gates and the next action. The outcome sought is working, maintainable software. Mark milestones complete when their required member journeys and release evidence actually pass.
