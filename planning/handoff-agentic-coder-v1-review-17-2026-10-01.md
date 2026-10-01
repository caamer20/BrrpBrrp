# BrrpBrrp implementation assignment to reach v1.0.0

Prepared 1 October 2026 for an agentic coder with repository and terminal access. This detailed handoff replaces the review16 prompt. Copy everything below the separator.

---

## 1. The goal is the complete v1.0.0 release

You are the implementation agent for **BrrpBrrp**, an open-source, self-hosted Discord replacement. Finish the agreed product, produce a coherent and reproducible release candidate, complete the actual release gates, and reach **v1.0.0** after explicit publication approval.

A successful v1 installation lets its owner install on either required deployment path, invite members, delegate moderation, use public/restricted channels, consented DMs and groups, threads, recoverable messaging, activity/notifications, voice/private calls and desktop sharing, then update and recover safely. Members must retain their work through ordinary network failures and understand access and contact controls. Contributors and operators need usable source, documentation, support and security procedures.

Implement and verify in the repository. Continue through coherent checkpoints while meaningful work remains. Do not spend this assignment producing another broad plan, re-auditing every previously closed defect or stopping after one regression passes. If your environment has a goal facility, create or continue this goal without inventing a token budget; otherwise maintain a concise execution ledger.

Distinguish two milestones:

1. **Engineering candidate:** all agreed product flows and required local correctness/privacy/data/integration checks pass against identified source and artifacts.
2. **Stable v1.0.0:** that candidate also passes real host/device/network qualification, independent review, beta and final observation, then receives publication approval and a verified release/support handoff.

Unavailable external resources may block a particular qualification. They do not block unrelated local implementation. Do not claim the goal complete, rename an alpha to v1, silently lower scope or mark missing evidence as passed.

## 2. Establish and preserve the current source

Last independently inspected state:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
HEAD: cf2f6926a7aa640c25533cd5418af611e19d5601
Application: 0.4.0-alpha.7
Schema: 24
Backup format: 2
Preexisting dirty file: internal/rtc/sfu.go
Git remote: none configured at review time
```

Inspect actual HEAD/status/diff and applicable repository instructions before editing. If source advanced, reconcile it with this handoff and preserve newer work. Do not reset, force-push, overwrite concurrent edits or mass-stage unrelated files.

Four commits have landed since review16:

| Commit | Work to preserve |
| --- | --- |
| `950f4c6` | Thread draft/outbox lifecycle and historical receipt destinations |
| `462e42c` | Effective capability checks on role-set changes |
| `587b18b` | Evidence source exhaustion and row identity |
| `cf2f692` | Shared worker notification admission and bounded retention |

The remaining SFU patch covers callback synchronization, ICE candidate-pair diagnostics and relay classification. Its file SHA256 is `d331bbea25d86ae754b50c68ff0227abcfe6e2823d02d6bd91e5e67362d048bc`, unchanged from review16. Preserve it, review it and integrate it coherently before freezing an artifact. The earlier eight focused RTC race passes are attributable evidence for that patch, not a new full-suite result.

Read:

1. `planning/completion-review-17-and-v1-next-steps-2026-10-01.md` for current findings and verification limits.
2. `docs/v1-implementation-plan.md` for the complete product, invariants, work packages, acceptance matrix and G0–G8 gates.
3. Relevant current ADRs, implementation and tests for the batch being changed.
4. Older reviews only when historical details are needed.

This handoff supersedes previous prompts for execution order. Historical “documentation only” statements describe old planning tasks, not this implementation assignment. Some active summaries still name schema21 even though current source writes 24. Source and attributable evidence establish what exists; approved contracts establish what must work.

Keep the Go/SQLite server, Svelte/TypeScript client, embedded frontend, shared conversation/send pipeline, account-owned receipts, bounded opt-in outbox, centralized authority revisions, WebSockets and embedded Pion media. Extend those systems rather than introducing separate chat, permission, retry or media stacks.

## 3. Current verification and repairs that should stay closed

Fresh review17 checks:

- Frontend: **287/287 unit tests passed**, **Svelte 0 errors/0 warnings**, formatting passed.
- Bounded API verification: **30 top-level tests, 28 passed and 2 diagnostic functions failed**, reproducing the remaining export and thread-state failures; **15.016s**.
- All ten new receipt tests, four effective-role tests, nine established receipt/thread-identity tests and relevant independent closure probes passed.
- Actual controller/component-function probes confirmed earlier draft lifecycle repairs, but found a multiple-uncertain-operation retry defect.
- The evidence component reaches 190 unique rows in three pages and terminates, including overlapping source IDs.

Initial socket EPERM errors were environmental and were separated from meaningful assertions. Tests used HEAD plus the existing SFU patch. No fresh full race suite, browser suite, release build, actual upgrade/recovery, physical media, deployed host run or independent acceptance was performed by this review.

### Preserve these verified repairs

- Ordinary/thread historical digests, blank-digest destination controls, promoted roots, deleted results and expired receipts now pass the selected compatibility cases. Do not restart the old receipt rewrite.
- Delegated role removal cannot grant capabilities the actor lacks. Deleting an assigned restrictive role is refused for manager and owner; an unassigned-role deletion works.
- Old panel cleanup does not put A's draft into B's store. Successful sends clear saved drafts, dropped outcomes preserve text, and edited new work does not overwrite the first unresolved intent.
- Thread destinations survive outbox persistence and recovery; pending rows stay in their threads. Previous Svelte/UUID-fixture failures are resolved. Future clock skew is bounded to five minutes.
- Evidence pagination no longer loops on exhausted sources; rows, notes and busy state use `(source_kind,id)`.
- Page/push notices share worker admission, the store prefix matches cleanup, and retained deduplication is bounded to 24 hours/500 entries. Unit mocks do not prove actual browser concurrency or deployed push delivery.

Also preserve earlier private-audit separation, timeout login/private HTTP scope and non-replaying expiry, export bounds and interrupted-job recovery, thread lock-at-commit and tombstones, whitespace, Activity remount/destinations, private-call access controls, publication ownership/independence/reservation cleanup, capture audience fencing and mounted remote-stream binding.

## 4. First checkpoint: reliable retries and revocable exports

Start implementation here. These are current reproduced failures, not obsolete items from review16. Use focused failing regressions, make a coherent correction and verify the complete boundary. Report the checkpoint briefly, then continue.

### 4.1 Associate each draft with its own unresolved operation

Current relevant code:

```text
web/src/lib/components/NamedThreadPanel.svelte:33,94
web/src/lib/workspace.svelte.ts:1349
```

`unresolvedThreadOperation` returns only the first unresolved operation in a thread. The panel compares the current draft with that first entry and otherwise generates a fresh key.

**Reproduced sequence:**

1. Submit A; its outcome is uncertain, leaving A/key1.
2. Edit to B and submit; its outcome is uncertain, leaving B/key2.
3. Press Reply again with B unchanged; the panel submits B/key3.

The probe observed all three POSTs and retained entries. If B/key2 committed but its response was lost, B/key3 can create a duplicate. Keeping the original text immutable is repaired; retaining the identity of the current unresolved operation is not.

**Required behavior:**

- Maintain an explicit association between the author, account/session, destination, submitted draft snapshot and operation key.
- Retry of B must use B/key2 even while A/key1 remains unresolved. A still needs its independent retry/discard/recovery state.
- Do not choose an operation solely by the first outbox entry or by matching text. Identical text can represent intentionally separate sends; edited text can be new work while an earlier operation remains uncertain.
- Preserve association across close/reopen and opted-in reload recovery. Do not leak it across accounts, threads or channels.
- Keep immutable submitted intent separate from editable next-draft text. Clear only the acknowledged draft snapshot, preserving newer edits made while a request is pending.
- Keep pending/failed rows visible and their actions understandable. A terminal or expired operation becomes bounded explicit recovery; it must not silently turn into a new send.
- Preserve the already-fixed teardown ownership, pre-lookup account fence, full destination persistence and explicit sent/refused/uncertain/dropped outcomes.

**Acceptance matrix:**

| Scenario | Required result |
| --- | --- |
| A uncertain, B uncertain, retry B unchanged | B reuses key2; no key3 and no second stored B |
| Retry A while B is pending | A reuses key1; B's operation and draft remain unchanged |
| B committed but acknowledgment lost | Reconciliation returns the original B ID; one B exists server-side |
| B never committed | Same-key retry creates exactly one B in its original thread |
| Close/reopen or reload with A and B pending | Correct associations survive; no channel reroute or stale draft resurrection |
| Same text intentionally sent as distinct new work | Explicit new operation is possible without accidentally replaying an unrelated pending send |
| New draft edited while a prior send completes | Only its matching acknowledged snapshot clears |
| Account/session/thread changes mid-operation | No new-account write, cache insertion or teardown draft contamination |
| Refusal, deletion or expiry | Accurate terminal/recovery behavior; no replacement message or attachment claim |

Add a deterministic test exercising the real component/controller decision, then a rendered browser journey with two uncertain operations and server message counts. A mock returning an ideal key is insufficient; verify the production caller selects and persists it.

### 4.2 Close export authorization and cancellation as one protocol

Current relevant code:

```text
internal/api/export.go:622–671
internal/api/delivery.go:23–28
internal/api/authz.go:676–679
```

The export checks its recorded conversation scopes, commits, then registers with an empty channel scope. Selective role revocation skips that entry. A deterministic probe holds the response after registration, removes the reader's `channel.read` through the real role API, observes history 404, and still receives export 200 with the withdrawn snapshot.

**Required behavior:**

- Model an export's actual set of content scopes instead of pretending it is an unclaimed upload or one ordinary channel response.
- Register and perform the final coherent authorization through the existing authority/delivery protocol so withdrawal cannot fit between a stale check and admission. Review ordering with concurrent commit, registration, revalidation and cancellation.
- Cancel an affected transfer after relevant role/channel visibility, group membership, account/session or private-feature changes. Evaluate the recorded export scopes with the same current policy as ordinary content.
- Preserve unrelated authorized transfers, permitted exports, Range/resume behavior, no-store headers, bounded memory and useful error outcomes.
- Do not hold authority/database locks across slow network writes. Lookup failure must not accidentally authorize a transfer.
- Retain current storage bounds, expiry, erasure, orphan cleanup and interrupted-job handling. Do not recall or claim to erase a copy legitimately downloaded before withdrawal.

**Acceptance matrix:**

| Boundary | Required control |
| --- | --- |
| Access withdrawn before admission | No protected response body is delivered |
| Withdrawal races check/registration | Neither ordering leaves an unauthorized admitted stream |
| Role change after registration | Held/continuing export stops; history and delivery agree |
| One scope removed from a multi-conversation export | That export becomes ineligible; unrelated exports stay usable |
| Group/account/session/feature withdrawal | The appropriate active transfer and future requests are denied |
| Unchanged permissions | Successful complete download with expected data |
| Authorized Range/resume | Correct bytes/status; reauthorization applies to each request |
| Failure or cancellation during transfer | No abandoned registry/file handle or false success record |

Current diagnostic commands, if their temporary files remain available:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test \
  -overlay /private/tmp/brrp-review17-xk3zhw46/overlay.json \
  ./internal/api \
  -run '^TestReview15(ExportStopsAtRoleCapabilityWithdrawal|ThreadUnreadAndArchivedSend)$' \
  -count=1 -timeout 3m -v
```

These historical function names were rerun against current source. The tests deliberately fail until the contracts are fixed. Add permanent repository regressions; do not make production verification depend on temporary overlay files.

**Checkpoint complete when:** the two new boundaries have meaningful allowed/denied controls, affected frontend/server checks pass, the diff is reviewable, and the ledger records source, commands, outcomes and limitations. Then proceed without restarting a broad planning exercise.

## 5. Complete thread, Activity, moderation and export journeys

### Thread read state, archive and history

Fresh probes still show that reading the sole unread thread reply leaves channel unread at 1, and sending to an archived thread returns 201. Storage for `thread_reads` exists, but the current unread projection still counts thread replies through channel cursors. Both admission and committing send checks reject only `locked`.

Implement the approved contract across persistence, API state and UI:

- Independent channel/thread read and mention projections, with no phantom unread and no accidental consumption of the other cursor.
- Archive stops new replies, including concurrent archive/send races. Reopen is an explicit usable action. Lock/unlock remains distinct.
- Root deletion leaves tombstoned, reachable acknowledged history. Retain receipt compatibility for roots promoted after their original ordinary send.
- All 1,000 replies are cursor-accessible with bounded cache/DOM, preserved scroll position and deep links. Incrementing a page count and re-reading every accumulated page is not bounded history.
- Follow/activity, active/archived lists, live updates and access withdrawal work through the actual member interface.

Do not test only separate cursor writes; assert the visible counts and navigation after reload and from another device.

### Activity acknowledgment

The current client submits the oldest loaded row as `before`, while the server updates rows older than that ID. The client then paints a count of visible rows as seen. With visible 05,04,03, this can acknowledge 02,01 while displaying 04,03 as acknowledged.

Define one explicit acknowledged set or range, validate it against current access and persist exactly that set. Reflect the same result locally. Verify reload, pagination, concurrent new arrivals, multiple devices and loss of access. Preserve repaired Activity remount, controller lifetime and destination routing. Do not confuse the now-fixed evidence-pagination issue with this separate Activity defect.

### Timeout audit and live enforcement

Keep the passing community/private HTTP scope and non-replaying expiry behavior. Complete transactional audit and appropriate live community-media enforcement for apply/lift/expiry. Bulk principals used in media revalidation currently omit suspension; admission, revalidation and forwarding must use a coherent policy.

Prove a live community restriction takes effect while permitted private reads/messages/calls remain correct. Ban/deactivation must still deny access. Revoked roles must remain revoked after expiry. Audit an actual committed change once, with no misleading successful audit of a rolled-back mutation. Include a positive unchanged-private-authority control so a timeout fix does not become an account-wide ban.

### Member data export

Finish recent-authentication, stable request retry identity, useful building/failure/expiry state and a real successful download. The existing browser journey can return early on 401/403 and uses a locator that does not match the Download link. An error path is not export acceptance.

Through the real UI, request an export, prove the job becomes ready, download it, inspect permissible account/content fields and exclusions, and verify deletion/expiry. Keep snapshots and server-held access controls consistent with the corrected delivery boundary. Handle interrupted builds and retries without unexplained duplicate jobs.

### Reconcile authority wording

The delegated-role vulnerability is closed in current checks. Preserve those guards and permitted controls. Separately reconcile the older “default applies to everyone” wording with the later zero-assignment fallback in ADR0001, the master plan and role UI. Make the actual effect of clearing assignments understandable. Do not silently weaken an approved authority invariant or describe a closed exploit as still reproduced; record a real policy decision if the contracts conflict.

## 6. Finish integrated member and notification acceptance

The new evidence queue implementation is repaired for the 190-row, overlapping-ID case. Keep the source-qualified row/notes/busy identity and exhausted-source behavior. Extend acceptance only where still needed: actual rendered reviewing, refresh/status change, authorization loss and reporting recovery. Do not reimplement the merge helper because an old review listed it as broken.

Notification code now shares worker admission and one IndexedDB read/write claim transaction, with consistent 24-hour/500-entry cleanup. Qualify the real boundary the unit mocks cannot establish: concurrent page/push requests across tabs, worker restart, denied/blocked storage, expiry/count cleanup, account switch, permission withdrawal and actual destination handling. Keep the same event from producing duplicate notices, while distinct events remain eligible. Verify preferences and active-view suppression and record actual deployed push separately from local browser tests.

Complete required DM/group/account flows through joined clients:

- Requests, acceptance, invitations, membership removal and full-history disclosure.
- Group ownership transfer/disposition, deactivation/removal and block behavior according to the approved group policy.
- Retained history versus live contact/call eligibility, terminal retention/deletion wording and evidence reporting.
- Former-member/outsider refusal across HTTP, WebSockets, search, previews, files, notifications and media, with positive member controls.
- Current-account state after reconnect, authority refresh and device changes, including bounded drafts/outbox and non-disclosing refusal bodies.

Keep core composer/history/search/permalink/upload behavior complete and usable. Verify keyboard/focus, assistive technology, reduced motion, zoom/reflow and physical mobile behavior for critical flows. Automated accessibility and simulated viewports are partial evidence. Use the full master plan's WP-09–17 requirements; this defect list is not a reduced product specification.

## 7. Work in bounded tranches and close them with evidence

Use a compact execution ledger, not a new master plan. For each tranche, record:

```text
Current source and dirty state
Concrete failing behavior and reproduction
Changed files and reason
Exact command and result
What the result proves and its limits
Remaining dependency or next executable action
```

Suggested sequence:

1. Multiple-uncertain operation identity and export delivery revocation.
2. Thread unread/archive/history, Activity acknowledgment, timeout live/audit and member-export journeys.
3. Remaining browser member/media/notification integration.
4. Real-prior-version and current-schema recovery with exact state and security reconciliation.
5. Integrated suites, coherent new prerelease and extracted artifact consumers.
6. Real deployment/device/reviewer qualification, beta, final observation and approved release.

Independent streams may overlap when file/schema ownership is clear. Serialize shared migrations, authority semantics and protocol changes. Preserve working behavior, use the project's normal local commit/checkpoint practice and do not mass-stage unrelated work. A passing tranche is a checkpoint toward v1.0.0, not permission to stop the goal.

## 8. Complete media acceptance while preserving current repairs

The SFU patch passed eight focused race tests in review16 and has unchanged file bytes in review17. This is useful prior evidence, not a fresh full-suite result. Do not replace the media architecture or repeat old repairs without a current failure. Extend the embedded Pion publication model and operator-controlled STUN/TURN configuration already chosen by the project.

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

The hash was checked again in review17. Preserve these bytes. This local baseline is available; archive hosting is not a prerequisite for local qualification. The `0.3.0` archive writes schema 2 and cannot seed the schema13 private-state rehearsal.

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

Existing alpha.7 archives mix schema 21 and schema 14. Preserve historical artifacts and hashes. Choose a **new unique prerelease version** after integration, synchronize the version identities and establish a **clean committed source checkpoint before building**. Resolve the existing SFU patch coherently before that freeze. Record generated evidence in a separate subsequent commit so the source identity used to build remains unambiguous. Do not overwrite alpha.7 again.

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

Your first response should briefly state the actual source/dirty state and the current multiple-uncertain-reply or export-revocation failure. Then implement section 4, preserving the verified fixes. Produce a reviewable checkpoint with evidence, report it briefly, and continue through the full remaining product and release sequence until the actual v1.0.0 goal is achieved or a specific external gate requires input.
