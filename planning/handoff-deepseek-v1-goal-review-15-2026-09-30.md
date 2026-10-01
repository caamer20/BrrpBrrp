# DeepSeek V4.1 Flash: implementation goal for BrrpBrrp v1.0.0

Prepared 30 September 2026 from review15. Give this prompt to the coding agent with repository and terminal access. Copy everything below the separator. It supersedes `handoff-deepseek-v4-1-flash-2026-09-30.md`; that older prompt starts from schema22 and includes defects that are now repaired.

---

You are the implementation agent for **BrrpBrrp**, an open-source, self-hosted Discord replacement.

## Goal and working agreement

**Goal: finish the agreed product, produce a reproducible and qualified release candidate, and reach v1.0.0 after its actual release gates and owner publication approval pass.**

Implement and verify the work in this repository. Keep going through coherent checkpoints while useful work remains. Do not spend the assignment making another broad plan, repeatedly reviewing already-tested repairs, or declaring completion from a version change. If your environment provides a persistent goal facility, create or continue this goal without inventing a token budget. Otherwise maintain a short execution ledger. Never claim to have created a goal or used tools you do not have.

The first useful outcome is a working repair of the thread author/destination failures below. Start there after checking the checkout. A genuinely unavailable host or credential can block a particular qualification, but it does not block independent local implementation.

## 1. Establish the actual starting point

Last independently inspected state:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
HEAD: fbce8b9cfb67b3efdbcd9c66ea5bec7e6470307c
Application: 0.4.0-alpha.7
Schema: 24
Backup format: 2
Existing dirty file: internal/rtc/sfu.go
```

There are 34 commits after the previous handoff's `d36ad63` baseline. Discover the actual path, HEAD/status and applicable repository instructions before editing. Preserve unrelated and concurrent changes. Do not reset, force-push or overwrite another agent's work.

The existing SFU edit changes `setEmitter` to synchronize its callback through the peer's `sendMu`. Review and finish that small change with an appropriate focused race check if it remains pending; do not discard it or start the full race suite repeatedly to investigate it. The review's tests included this patch, so they do not establish a clean committed artifact.

Read:

1. `planning/completion-review-15-and-v1-goal-2026-09-30.md` for current findings and evidence.
2. The scope, invariants, G0–G8 gates and acceptance sections of `docs/v1-implementation-plan.md`.
3. Relevant ADRs, current implementation and tests for the batch you are implementing.

Consult earlier reviews for historical detail only. Do not re-read every historical plan at each checkpoint. Current code establishes implementation facts; approved contracts establish required behavior. The progress document contains stale summaries, including an incorrect claim that no local schema13 baseline exists.

The product already has community chat, consented DMs/groups, recoverable messaging, activity, threads, moderation, export, private calls and desktop sharing. Extend the Go/SQLite server, shared authorization and conversation/send pipeline, bounded outbox, Svelte client and embedded Pion media. Avoid parallel implementations of those contracts.

**Preserve the repaired behavior:** timeout login/private scope and non-replaying expiry; SQL audit audience separation; export bounds/feature checks/job recovery; thread lock-at-commit and root tombstones; whitespace and Activity remount/destinations; private-call block/revalidation; publication ownership, independent screen/audio behavior and reservation rollback; capture audience fencing and mounted video binding; TLS/heartbeat capacity transport.

Fresh review evidence was 259 frontend tests, Svelte 0 errors/warnings, 15 selected API regressions and five focused RTC regressions passing. Separate probes still failed on the intended contracts below. Those results are limited to the inspected working tree, not global release acceptance.

## 2. First deliverable: correct thread operation identity and recovery

Repair this as one bounded tranche and checkpoint it before moving on.

### Observed failures

- `workspace.svelte.ts:1380`: `sendIntoThread` awaits `GET /threads/{id}` before capturing identity. If A initiates a send and B becomes active during the lookup, the POST proceeds as B with A's drafted text.
- `outbox.ts`, `trackOutbox` and `reconcileOutboxEntry` omit `thread_id`. After restart, a failed thread operation retries at `/channels/{id}/messages` with the original text/key.
- `pendingReplies` filters `reply_to_id`, so a pending named-thread send is not shown in its thread.
- The component can treat definite refusal as success and loses an unresolved key on close. Thread-lookup failure also falls back to the current active conversation.
- `internal/api/messages.go`: adding thread identity changed even ordinary channel intent digests. An unchanged retry against a durable pre-change receipt returns 409. This was reproduced using the exact canonical digest from `d36ad63`.

### Required behavior and verification

Capture initiating account/session/authority and complete destination before any asynchronous step. Check admission again before pending-state creation, persistence and POST. A stale lookup must never send or persist A's text under B. A failed lookup must not choose an unrelated active conversation.

Carry the destination through the outbox schema, validation/whitelisting, persistence, restore, retry, recovery UI and receipt interpretation. Preserve existing ordinary outbox entries safely. If a legacy operation's destination cannot be established, retain author text for explicit recovery; never silently reroute it.

Keep the same immutable key and text while an outcome is unknown. Make pending/failed state visible in the actual named-thread panel; retain it across close/reopen, reload, refusal, lost response and unrelated authority refresh. Only resolve or rotate the key when the operation's outcome warrants it. Keep account scopes isolated.

Version or compatibly interpret old server receipts. Do not clear digests or let old receipts authorize different work. Preserve deleted-message safeguards.

Write meaningful failing controls and prove:

1. Delayed lookup plus account/session change makes no new-account POST or persistence.
2. Lookup failure makes no fallback-channel send.
3. Response loss after commit, reload and retry return the original thread message exactly once.
4. Failure before commit, restart and retry create exactly one message in the original thread, with none in parent-channel history.
5. Definite refusal and panel close/reopen retain recoverable text and correctly represented operation state.
6. An unchanged pre-change ordinary receipt replays successfully; changed text, conversation or thread conflicts.

Use actual controller/component behavior and supported API paths. A helper with hand-constructed correct input does not verify that the caller supplies it. Record the observed result, not merely a non-403 status.

**Checkpoint:** tests for these outcomes pass, the tranche is reviewable, and the ledger names source state, changed files, exact commands/results and the next task. Then continue.

## 3. Second deliverable: finish current authority boundaries

### Effective role changes

A delegated manager lacking `message.send` can remove a target's only read-only role using `role_ids: []`; implicit default fallback changes that target from 403 to 201 on send. Authorize the **effective final capabilities**, including implicit defaults and intermediate revocations/grants. Removing an ordinary grant must not increase authority or let an actor grant a power they cannot delegate. Preserve permitted assignments, hierarchy and owner recovery. Test the real role endpoint with allowed and denied controls.

### Export admission and live revocation

Exports check their scopes, commit, then register under an empty channel scope. Role changes skip that registration. A held export returns 200 with withdrawn content even while ordinary history returns 404.

Integrate complete export scope and atomic admission with the existing authority/delivery protocol. Test withdrawal before registration, after registration and during transfer; cover role/channel permission, membership, account/session and private-feature changes as applicable. An unchanged authorized export must still download successfully. Do not call registry membership alone a successful revocation test. Retain job/storage/expiry/erasure/restart controls already implemented.

### Timeout follow-through

Preserve the passing community/private HTTP scope repair. Add missing transactional audit and appropriate live community-media enforcement. Bulk principal construction currently lacks `Suspended`; use a coherent policy for admission, revalidation and forwarding. Prove that timed-out community activity stops while permitted private reads, messages and calls remain usable, and bans/deactivation still deny access. Expiry must never replay revoked roles.

## 4. Third deliverable: finish the member journeys

Implement these current integration failures in small batches:

- **Threads:** connect `thread_reads` to actual channel/thread unread and mention projections. A read thread reply must not leave a phantom channel unread. Archive must stop new replies per ADR0017; provide Reopen. Preserve commit-time locking and tombstones. Reach a 1,000-reply history with bounded DOM/cache and cursor navigation; do not repeatedly reload every accumulated page.
- **Activity:** align server acknowledgment and the visible acknowledged set. Current visible `05,04,03` leads the server to update `02,01`, while the client paints `04,03`. Verify reload, pagination and concurrent arrivals. Preserve the repaired controller remount and destinations.
- **Evidence:** preserve independent source exhaustion when a source is skipped. With 120 message/70 request reports, the actual component must stop at all 190 unique rows. Use `(source_kind,id)` for merge, rendering, note and busy state, including colliding IDs.
- **Notifications:** align stored/cleanup prefixes (`brrp.notified.*` versus `brrp.notify.*`), bound retention and use shared atomic admission across app/worker. Verify duplicate suppression, preferences, active-view behavior and current-access navigation.
- **Member export:** finish recent-authentication, stable request retry identity, useful building/failure state and a successful download. The browser test must not pass early on 401/403 and must interact with the actual Download link.
- **Group/account lifecycle:** verify real joined clients, membership/ownership changes, invitations, block policy, account removal and terminal retention wording against approved contracts. Close missing acceptance without rewriting already-working group behavior.

Complete required keyboard, focus, mobile/reflow, error and loading states for these flows. Keep private information inaccessible after authority changes across DOM, caches, events, files and notifications. Retain the master plan's remaining required UX; do not reduce the scope merely to clear a checklist.

## 5. Complete media acceptance and dependable recovery

### Media

The five reviewed RTC tests pass. Preserve them and finish the actual two-browser journey: remote decoded frames, independent audio where supported, late join, stop/restart, move/reconnect, permission withdrawal and delayed capture results. Track room/peer/publication identity through negotiation and forwarding; stale or unauthorized tracks must not survive replacement. Verify cleanup and non-cooperative sender enforcement.

Maintain the distinction between generated RTP, browser-generated streams, real capture/microphones and physical quality. Qualify listen-only behavior, optional screen audio, mobile viewing and forced relay. Physical prompts, sound, text legibility, long calls and restricted networks need their own evidence. Resolve the current ten-serverwide/two-private-participant limits against the agreed target, with implementation and measurements; do not silently lower the v1 promise.

### A real schema13 baseline already exists locally

```text
Executable: dist/brrpbrrp-p9-checkpoint
SHA256: fddd1e3f3ef3c9873e822dd96f625c3bcdfd036b76fdff2ba45c5835a4c3b57f
Embedded clean revision: 2869db55394b2b2a004424672517b38d6908586b
Read-only preflight supported schema: 13
```

Reverify those bytes and use the preserved executable for local prior-version qualification. Keep it intact. Do not substitute current source or the known-wrong `0.3.0` archive. Hosted Linux CI needs its own correctly identified runnable baseline; distinguish acquiring/publishing that artifact from fixture work that can already run locally.

### Repair recovery before trusting larger pass counts

- Seed accepted private history and submitted evidence through the old version's supported interface; the verifier currently expects evidence the seed never creates.
- Make `--report` actually write a machine-readable result on success and failure. Fix the workflow's wrong baseline selection and retain its identity/provenance checks.
- Complete ownership/MFA/reviewer setup in the disposable installation through supported APIs. Remove fallback SQL mutations followed by unconditional success.
- Fix export restoration assertions: the list endpoint returns a raw array, not `{exports: ...}`. Capture original IDs/metadata and a ready precondition; prove the intended preserved-history and missing-file 410 boundary and account isolation.
- Verify exact group ownership/membership/invitations, thread roots/tombstones/follows/read positions, activity acknowledgments/preferences, moderation, export state and retained evidence after restore. Check post-snapshot bans, revoked roles/membership, password/session changes, blocks and erasure before cutover.
- Add targeted failing controls demonstrating that lost or corrupted state fails the relevant check. A 200, empty fallback or count-only result cannot establish preservation.

Run current-schema recovery and populated supported-prior-schema upgrades, interruption and rollback against a frozen candidate. If repairs advance schema 24, include the appropriate populated 24→new migration. Save source/binary identities, command, timestamps, exit status and JSON results. Do not turn a local recovery pass into an RPO/RTO claim for deployment.

## 6. Build a coherent candidate

Use focused verification while coding. Once integrated, run required frontend checks/build, Go vet, the full race suite, both browser feature modes, recovery and package consumers against matching server/client bytes.

Known local tools, subject to rediscovery:

```text
.tools/go/bin/go
GOCACHE=/private/tmp/brrpbrrp-go-build
GOMODCACHE=/Users/cameronamer/go/pkg/mod
PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
```

Typical gates:

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
.tools/go/bin/go vet ./...
.tools/go/bin/go test -race -count=1 -timeout 40m ./...
```

Use the repository's actual current harness arguments. Socket denial, unavailable browser binaries or missing dependencies must be diagnosed separately from product failures. Never rerun the 40-minute suite after every small edit.

Existing alpha.7 archives mix schema 21 and schema 14. Preserve them. Once source is coherent, create **one new uniquely versioned prerelease**, record clean source/toolchain/configuration/app/protocol/schema/backup identities and final checksums, and test the extracted native and source packages. Qualify the actual container image on Docker. Reconcile the Windows update guide with the supported stop/backup/replace/start/verify procedure.

A local candidate is ready for external qualification when required local journeys, authority/upgrade/recovery controls and integrated gates pass against identifiable artifacts. Label it accurately; this is not yet stable v1.0.0.

## 7. Complete the actual v1 release gates

The following decisions are already made: **Windows x64 native/Tailscale and Linux amd64 Docker are both required**; desktop sharing and group/private calls are in scope; MIT and the project's contribution policy are established. Do not repeatedly ask which host to support or reopen settled licensing.

Use the full master plan, not just this summary:

- Reference targets: 250 accounts, 50 active people, 100 sockets, ten-member groups, twenty media participants serverwide/ten per room, two serverwide shares, one million messages and 20 GiB attachments; reference allocation four cores, 8 GiB RAM, 80 GiB free disk and 100 Mbit/s. Freeze exact configuration and budgets before measurements; these are targets, not established capacity.
- On **each required deployment**, qualify clean installation, trusted HTTPS, process/service/container lifecycle, update/failed-update rollback, target-load two-hour peak and 24-hour mixed soak, faults, resource growth and delivered external alerts.
- Qualify real desktop/mobile browsers, two-hour media sessions, 100 capture lifecycle cycles and actual restricted-network/relay paths per the acceptance matrix.
- Restore an independently encrypted off-host backup to clean hosts. Prove RPO ≤24h and RTO ≤60min including operator response, retrieval/decryption, reconciliation and validation, with key custody and an alternate operator. Report total outage separately.
- Complete independent security/privacy review, human accessibility/browser review, non-author installation/recovery and clean-source contributor rehearsal. Supply precise ready procedures when asking for hosts, reviewers, devices, credentials or signing access; continue independent tasks while waiting.
- After prerequisite gates, run at least **three communities for fourteen days**, spanning both hosts, followed by **seven consecutive days on the frozen final candidate**. Apply the master plan's reset rules after material changes. Do not invent elapsed time or evidence.
- Present final artifacts, signatures/provenance, support/security ownership, release notes and the evidence ledger for explicit owner publication approval. Publish only when authorized; then verify public downloads and staged upgrades.

Camera video, federation, E2EE, native mobile apps, bot marketplaces, multi-community hosting, enterprise SSO and clustering remain deferred. Do not drop agreed v1 features or expand into these deferred ones.

If external gates prevent release, deliver the exact verified candidate, prepared procedures and specific outstanding inputs. Keep the goal honestly incomplete; do not rename an alpha to v1 or claim all remaining work is external while local failures remain.

## 8. Keep progress moving when a test fails

After two unsuccessful attempts at the same failure:

1. Run only the relevant test with fresh output paths; record current source, exact assertion and actual response.
2. Inspect the current code/diff and verify the edit landed. Assert matches in scripted replacements.
3. Inspect the failing browser context's own DOM, account, URL and network state; do not use another account's page or an old screenshot.
4. Distinguish setup, product and assertion failure. State one falsifiable cause and gather the evidence that separates it from alternatives.
5. Make the smallest coherent correction and rerun the focused behavior. If truly blocked externally, record the missing dependency and switch to independent work.

Do not remove assertions, accept success-or-failure, extend waits without a diagnosed timing cause, use SQL as a privilege workaround, skip on 401/403, broaden access, or count an empty 200 as success. A test must prove its named behavior and fail when that behavior is broken.

Keep a compact ledger: issue → observed failure → changed files → exact verification → limitation → next executable task. Checkpoint coherent verified work before a context switch. If context or runtime limits require stopping, save that record with HEAD/status and reproduction commands so the next turn resumes implementation. Do not spend each turn creating a new handoff or repeating the same review.

**Start now:** check HEAD/status and the existing SFU edit, then reproduce and repair the account-switch and restored-thread-destination failures in section 2. Finish that tranche, report its evidence briefly, and continue toward the complete goal.
