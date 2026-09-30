# BrrpBrrp implementation handoff for version 1.0.0

Prepared 30 September 2026 from completion review 14. Copy **everything below the separator** into the next coding agent. This is a standalone assignment for the current repository; older handoffs remain historical evidence.

---

You are taking over **BrrpBrrp**, an open-source, self-hosted community communication application. The owner wants an approachable Discord replacement with dependable chat, private conversations, groups, threads, notifications, voice and desktop sharing, easy installation and genuine data ownership.

Continue the existing implementation to a **feature-complete, stable release candidate**, then to **v1.0.0** when its operational and independent acceptance gates pass. This is an implementation assignment: inspect, repair, finish, verify and package the product. Keep progressing through the feasible work instead of stopping after another plan or a small helper-test checkpoint.

## 1 Mission and completion boundary

The owner already selected **both Windows x64 native/Tailscale and Linux amd64 Docker** as required qualification targets. Do not ask them to choose again. Existing MIT licensing and CLA/signatory decisions are recorded; complete public-project setup without reopening settled choices unnecessarily.

The app must provide one community per installation, local accounts and no required vendor account; understandable owner/member onboarding; current permissions; consented direct/group conversations; organized threads; recoverable sending; useful activity and preferences; community/private calls; desktop screen sharing and mobile viewing; accessible responsive web/PWA flows; and reliable update, backup and restore.

There are two honest finishing points:

1. **Feature-complete candidate:** every agreed feature works through its supported member/operator interface; known privacy, authority, durability and core-flow defects are resolved; meaningful tests and recovery pass; exact candidate artifacts exist. External acceptance still pending must be listed explicitly.
2. **Released v1.0.0:** G0–G7 are actually accepted, both required deployments and independent reviews qualify the final product, observation periods finish, then explicit owner publication approval and verified rollout complete G8.

You may implement the necessary application code, migrations, tests, scripts, interface and documents. Preserve unrelated changes and the existing architecture. Resolve routine engineering choices autonomously. Ask for unresolved product policy, external resources or actions requiring authorization only when needed, and continue unaffected work while waiting. Prepare concrete artifacts/procedures before requesting publication approval. Do not message other people, expose unqualified private features to production, publish or change live data without authorization.

Respect your actual environment and budget; old quota allocations or unlimited-time instructions do not automatically carry forward. If the full assignment cannot finish in one run, checkpoint after a coherent verified milestone with precise continuation instructions. Do not call the overall task complete while required work remains.

## 2 Exact starting point

The last independently inspected source was clean:

```text
Repository: /Users/cameronamer/Documents/ChatGPT/BrrpBrrp
Branch: codex/p9-private-reporting
HEAD: cc2a8a140fe00292236b2c723a3d69b0fb30afce
Application version: 0.4.0-alpha.7
Database schema: 21
Backup format: 2
Private conversations: default-off via BRRP_ENABLE_PRIVATE_CONVERSATIONS
```

Planning documents may have been committed afterward. Check current status/history and applicable instructions first. If code advanced, establish the delta and credit fixes supported by evidence; do not reset to the identifiers above.

There are 83 commits since the previous reviewed `6e36c10`. Group chat, activity UI, named threads, moderation, member export, private calls, screen capture/receive machinery, optional screen audio, configurable relay paths, alerts, SBOM and capacity tools now exist. **Do not rebuild these features from scratch.** Integrate and correct them.

Architecture remains Go, SQLite, embedded Svelte/TypeScript, common conversation/message services, account-owned operation keys, a bounded recoverable outbox, authority revisions and transaction/stream admission, WebSockets and an embedded Pion SFU. Keep one coherent implementation. Avoid parallel thread/group sending, permission, recovery or notification systems.

### Evidence to credit

- Fresh review-14 frontend run: **198 tests passed**, zero skipped/failed; Svelte **zero errors/warnings**.
- Nine focused V13 regressions passed: activity filtering/bounds/expiry, group revision and stream cancellation, ended/blocked invitation refusal, common group conversation/report/audience paths, owner-disposition helper, and request-evidence/spent-key cleanup.
- A two-member group chat journey now exists; previous management-only coverage was improved.
- Local relay, publication, alert receiver and package-consumer tests establish useful specific behaviors.

### Evidence not to overstate

- Review 14 reproduced new privacy, timeout, export, thread, activity and media defects. They are detailed below.
- The fresh review did not rerun a full race/browser suite or real hosts/devices. An older M5 race record reports an API duration of 1959.611 seconds with a 40-minute timeout; it predates the newest media changes.
- Recovery counts such as 23/113/57 do not establish coverage of groups, activity, threads, timeouts or export jobs unless those data and transitions are actually seeded and checked.
- A registry-level hundred-cycle media test does not establish a real browser chooser, remote decoded frames or physical two-hour call quality.
- All G0–G8 gates remain open. The latest attachment is a partial progress transcript, not an authoritative current-state acceptance ledger.

### Current distribution is inconsistent

The clean package-consumer record refers to source **`b51ffc26956fbc6a80cb974c3d5975b367a59b46`**, schema 21, before the latest media commits. Its source ZIP has 452 matching files; fifteen shipped files differ from current HEAD.

Darwin/amd64 and Linux/amd64 alpha.7 archives were rebuilt at that source; the other four native alpha.7 archives remain P10/schema14. Historical paths were overwritten and `SHA256SUMS` lists only the two rebuilt platforms. Do not describe the directory as a coherent six-platform release.

Current identities for reference:

```text
Darwin/amd64 ZIP:
e9fb0fb3cc4455ec62b50fa2d3a87a92d72cc50582f584d24a46813c3cbb9091
Extracted Darwin executable:
0367f3fd02b858e400fc216e70e7953c9c6cf821d220ac9678eecf6028f020ac
Linux/amd64 ZIP:
2dc7c74534df1eedc6bcf00eabf3d6a54ff94b74f2f1372e12a77112a076a271
Source ZIP:
18f606b923119486db7770e499f2362c7198bf011666f41bdccba73f6b1f8e6a
```

Use a new unique prerelease identity for changed distributions. Preserve original historical bytes where available; record overwritten/unavailable evidence honestly. Never rebuild different bytes and attribute them to an old digest.

## 3 Read and establish a working ledger

Read, in order:

1. Applicable repository instructions, Git status and recent history.
2. `planning/completion-review-14-and-v1-finish-plan-2026-09-30.md`, especially **V14-01–12** and the distinction between reproduced and source-confirmed findings.
3. `docs/v1-implementation-plan.md`: requirements, WP-01–33, D decisions, G0–G8, capacity/quality targets and observation reset rules.
4. Current progress/release records and `planning/m1-checkpoint`, `m3-checkpoint`, `m4-checkpoint`, `m5-checkpoint`. Separate the source identity of each result from current HEAD.
5. ADRs 0001–0016 for authority, durability, private consent, lifecycle, reporting, groups and activity; 0017 threads, 0018 moderation, 0019 media and 0019b member export.
6. Routes and central authorization/commit helpers; new services/controllers; browser tests; `Makefile`, workflows, package/recovery/host/capacity scripts.

Some new ADRs/tests contradict the master contract. In particular, community timeouts must not silently become account bans or restore revoked roles; private audit must not silently broaden ordinary auditor access. Correct code, tests and documents together. A test that approves an unsafe policy is not closure evidence.

Maintain one concise implementation/evidence ledger. For each requirement distinguish implemented, locally verified, host/device verified, independently accepted and published. Assign precise blocking dependencies. Use existing finding IDs for traceability, without relabelling an old pass as a new one.

Start local privacy/authority repairs immediately. Parallelize independent interface, media and release-tool work with clear file ownership. Provision host/reviewer resources early. Missing infrastructure does not block the code defects below.

## 4 Repair privacy and authority boundaries first

### Private audit access V14-01

`router.go:372` and `admin.go:829` now permit ordinary `audit.read` at `/api/admin/private-reports/audit`. A reproduced audit-only account gets 403 from private evidence but 200 with reporter/reviewer identities, evidence IDs and times from the audit feed; group lifecycle actions are also selected. No evidence body was exposed.

Restore the approved private metadata boundary. A second endpoint needs a genuinely appropriate policy. Distinguish selected-evidence audit from private group lifecycle metadata; do not automatically give ordinary auditors or evidence reviewers arbitrary group access. Handle existing rows, retention and revoked grants consistently. If a broader policy is desired, present it explicitly for owner approval before exposure rather than encoding it as a “fix.”

**Acceptance:** invoke real report/review/group mutations, then test ordinary auditor, participant, authorized private reviewer, unrelated administrator and owner/operator cases through every audit surface. Include pagination/counts, direct IDs and revocation after an earlier allowed read. Preserve the correct retention boundary and do not expose evidence text unnecessarily.

### Export authorization and lifecycle V14-02

Current export download checks the job owner/state/expiry then calls `ServeFile`. A real group removal makes history return 404 while the stored export still downloads with 200 with private group text. A new export also includes private data with private conversations disabled because its selection bypasses the shared scope policy.

Use the common current-access and feature rules for source selection and delivery. Store sufficient scope/provenance to invalidate or refuse a server-held snapshot when access changes; define partial-versus-whole-export behavior clearly. Register downloads with the existing session/authority stream lifecycle, including ranges and interrupted transfers. Previously downloaded legitimate copies cannot be recalled; the repair concerns new server delivery.

Enforce the declared message/attachment/byte limits before writing beyond them. The current oversized-output branch records an omission but still writes the oversized document. Add finite per-account/server ready-export storage, bounded job admission, expiration/deletion, cancellation, crash/restart reconciliation and orphan-file cleanup after account erasure. Preserve operation identity during retries.

**Acceptance:** build/download as an authorized account; then remove membership, revoke a role, disable the private feature, end the session or erase the account before and during delivery. Denied controls receive no newly unauthorized bytes. Test large output, concurrent jobs, interrupted workers, restore and expiry. Complete real member reauthentication/download in the browser; 401/403 is a refusal test, not an export-success pass.

### Timeout policy and implementation V14-03

Fix the supported route contract first: the UI uses `/admin/users/{id}/timeout`, the API registers `/admin/accounts/{id}/timeout`, and the handler ignores `{id}` in favor of body `user_id`. Route-only POST returns 404; including that field commits and then returns 500 because `commitAuthorityChange` already committed and the handler commits again. Lift has the same double commit.

Use one authoritative target, one commit and the established revision, audit and live enforcement protocol. The current probe observed revision 1 → 1 and an uncancelled registered stream. Confirm mutation success/failure accurately and make rollback atomic before commit; do not undo committed work merely because a response was lost.

Apply the master plan's **community-scoped** timeout contract. A community moderator's timeout must not silently deny login, private history, DMs or private calls as if it were an installation ban. Explicit installation bans remain their own authority and terminate affected sessions/media.

Remove role-snapshot replay. Expiry must remove the temporary restriction and use **current** grants. The review granted `channel.manage`, applied timeout, revoked the role through the API, then expired/logged in: the old grant returned. Replace the existing test that expects this unsafe behavior. Cover concurrent assignment/demotion/ban/deactivation and manual lift.

Add a usable duration control and accurate scope/recovery copy. Test the actual browser apply/lift flow, REST/socket alternate actions, current hierarchy and live media/streams within the approved timeout scope.

### Private call access V14-07

`resolveVoiceRoom` uses retained-history read admission, so a blocked direct pair can still join a call while its new message POST correctly returns 403. `enforceVoiceAccess` uses community-channel lookup for private rooms, so revalidating a still-authorized private participant evicts them.

Define one current live-call policy separate from historical readability. Apply it at join, move, resume, signaling and revalidation. Block/unavailable peer/ended consent must prevent new direct contact; valid unchanged private membership must survive revalidation. Preserve listen-only capability without requiring message-send permission merely as a shortcut.

Apply membership-scoped private moderation: group owners may stop permitted group publications; ordinary community moderators gain no unrelated private-call control. In a direct call, block/leave ends contact without granting control over the other browser. Keep installation-wide ban authority separate.

**Acceptance:** allowed direct/group/listen-only controls plus block, unavailable partner, removed member, stale resume, capability change, unrelated authority refresh and concurrent admission/revocation. Verify both media and participant-state audiences.

## 5 Repair thread integrity and member work recovery

### Commit rules receipts and root lifecycle V14-04

Thread sends check state in one transaction and write through another. A deterministic lock after the first check still permits 201. Move thread channel/state/posting validation into the shared committing send transaction.

Bind operation intent to the full thread destination. Currently the same key/content sent to another thread in the same channel replays the first thread's message with 200. Include immutable thread identity and handle existing receipts safely: an old receipt must never become permission to send different work. Retain deletion-safe/spent-key behavior.

Implement the promised root tombstone. Deleting the root currently cascades the thread row while acknowledged replies remain stored but are stranded from normal thread/channel history. Preserve legacy promotion, IDs/links, searchable history and appropriate erasure rules through a forward migration. Do not edit applied migration 19 or delete stranded replies merely to make a test pass.

Finish archive/lock/reopen semantics, thread-specific follow/activity and **independent thread read state**. Reading a thread must not mark unrelated channel content read. Keep all authorized history reachable with stable bounded pagination; the client's DOM/cache must stay bounded even for the 1,000-reply fixture.

### Draft and uncertain send recovery V14-05

`NamedThreadPanel.send` clears the draft before awaiting and rotates the operation key unconditionally. `ThreadView.send` catches errors, so both 403 and a lost response leave empty text, a replacement key and no pending item. The shared outbox is bypassed.

Reuse the existing account/conversation draft, pending-send and receipt reconciliation machinery with thread identity included. Preserve text, attachment claims and key through uncertainty; an explicit new send is distinct from retrying old work. Closing/reopening a panel and unrelated authority refresh must not erase ordinary author work. Account changes must isolate it.

**Acceptance:** refusal, rate limit, lock during send, lost acknowledgment before/after commit, retry after deletion, browser/panel restart, attachments and stale-account responses. Prove exactly one logical message and a clear user-visible recovery path. Replace source-string tests that currently mistake unconditional key rotation for preserved identity.

### Message presentation

Fix tokenization before expanding formatting: actual `tokenizeMessage` concatenates ordinary lines and drops separators between mentions/URLs. Preserve paragraph breaks, blank lines and meaningful spaces while keeping code literal and raw HTML inert.

Finish the planned safe formatting contract, accessible member/message actions, edit/delete/reply context and upload error/cancel/retry behavior. Test malicious markup/URLs, IME, emoji, RTL, long text, keyboard operation and a rendered multiline control. Stored content, search, export and displayed meaning must agree.

## 6 Complete activity reporting and export workflows

### Activity and acknowledgment V14-06

`ActivityPanel` destroys an exported singleton on unmount; `alive=false` never resets, so returning to the tab stops all loaders. Give account-scoped state a correct lifetime and fence old requests without permanently disabling the next admitted view.

Define exact acknowledgment semantics. The client sends the oldest displayed ID; the server marks older items with `id < before`; then the client marks the displayed page seen only locally. Separate visible-page acknowledgment from an explicit “all through this cursor” operation and persist what the UI claims. Test multiple pages, incoming events between read/ack, a second device and cross-account cursors.

Make activity entries navigate to their actual currently readable DM, group, request or thread. Preserve meaningful expired/removed-source states. Switching a global tab variable alone does not change a panel that keeps its own local tab. Distinguish message unread, activity acknowledgment and outstanding requests/invitations.

### Evidence pagination and report reconciliation

The attempted exhausted-source skip still revives the source by returning its preserved nonempty cursor. A local probe with 120 message and 70 request reports reaches all 190 entries but never finishes. Keep exhaustion and cursor position separate; skip exhausted fetches without synthesizing a live continuation. Use `(source_kind, id)` consistently in merging and rendering.

Finish same-operation report recovery after authority changes, keeping explanation/key and historical receipt distinct from current block state. Preserve the fixed request evidence expiry/compaction and ordinary Block fallback. Test changed preview, uncertain transport, receipt error, current account changes and terminal pagination through the rendered component, not only helper predicates.

### Notifications

Correct the worker prefix mismatch: stored `brrp.notified.…` entries are not matched by cleanup's `brrp.notify.` prefix. Use a bounded retained deduplication store and atomic admission coordinated by app and worker. A separate read/show/write sequence and an uncoordinated foreground mention path do not establish one OS notice per event/device.

Complete account/conversation/device preference precedence, mute/quiet controls, active-view suppression, burst handling, private preview defaults, current-access deep links and group/thread sources. Test concurrent duplicates, delayed dispatch, block/removal/deletion, preference changes, account switches, offline/denied push and storage failure. Unblock must not replay old unwanted contact. The activity inbox remains useful without successful push.

### Export interface and test completion

Guide the member through recent authentication, queue/build status, retry with the same operation identity, progress/refresh, download, omissions and deletion. A building result should not promise automatic completion while requiring an unexplained manual refresh.

Fix the browser test's early success on401/403 and its button locator for an actual download link. Require an ordinary member to authenticate, create, download and inspect a real export with permitted content and no credentials. Keep denied controls separate.

### Remaining group and account lifecycle

Preserve shared group chat and the real two-member journey. Finish live invitation/ownership/removal without reload, held history/file/member responses during access withdrawal, and group read/search/attachment/recovery integration.

Make terminal-group retention accurate. Current code closes invitations/removes ownership and marks the group removed but does not perform all deletion promised in comments. Define the intended retained/erased data and bounded cleanup, then align UI wording and tests. Explicitly handle owner deactivation/ban/reactivation as well as removal/erasure without leaving an unadministrable group or granting unrelated private-history access.

## 7 Finish the media implementation before physical qualification

### Publication authority and concurrency V14-08

Current forwarding checks whether any screen publication exists in the room. It does not bind the packet source to that publication: a synthetic probe lets nonpublisher B advance viewer output under A's room grant.

Bind each accepted track to owner, room, connection, publication ID/generation, kind and permitted subscribers. Reject unsolicited or stale tracks even while another person legitimately shares. Validate any client-supplied identity; a same-room publication is not a reusable authorization token for everyone else.

Reserve per-room/server publication capacity atomically through negotiation, with rollback on failed/cancelled offers. The current check releases the lock before final registration, so independently negotiating peers can race the allowance. Revalidate permission/room generation at commit. Apply consistent lock order and avoid holding global locks across network writes.

### Separate microphone screen video and screen audio

Microphone mute currently drops screen video, contrary to the master contract. Stopping video and screen audio leaves an audio registry entry. `OnTrack` still classifies all audio as microphone and drops the second input; the registered screen-audio kind does not prove that sound is carried independently.

Implement distinct transport identity, forwarding, receiving/playback, mute/deafen and stop behavior for the three publications. Microphone mute affects microphone only. Deafen follows the master contract without silently stopping unrelated screen controls. Screen stop/permission withdrawal ends both associated screen tracks and their registry/resources, leaving an otherwise permitted call intact.

Test two audio sources with actual delivered packets, independent controls and repeated replacement. Verify a denied/withdrawn publisher cannot keep forwarding through another person's active share. Do not count a manually inserted registry object as end-to-end media evidence.

### Capture lifetime and received screen V14-09

Fence the browser chooser with initiating account/session/room/connection generation. After `getDisplayMedia` resolves, recheck all of them before retaining or publishing the stream. Leave, room move, account change, permission loss or newer capture intent must stop every stale returned track. Never publish an earlier gesture into a different audience or silently reacquire ended capture on reconnect.

Separate local capture from received streams. The current first remote stream is attached before its conditional video element exists, then the element mounts without `srcObject`. Bind streams when elements actually mount and clean up on source change. Handle first frame, autoplay refusal, late subscribers, ended tracks, restart and mobile viewing.

Use two browser peers to verify decoded remote video and optional screen audio, not just a publisher's capture/status label. Cover listen-only viewing, explicit stop/browser stop/moderator stop, move/reconnect, non-cooperative sender controls and meaningful permission diagnostics. Keep frame/packet/registry counts distinct.

### Network and support profile

Preserve the new operator-controlled STUN/TURN UDP/TCP/TLS and forced-relay behavior. Verify actual selected paths at both ends and useful failure when the requested route is unavailable. Add required external self-hosted relay/IPv6 policy, short-lived credentials and revocation checks where incomplete.

Current code has ten serverwide participants and a two-person private-call bound. The master target is twenty serverwide/ten per room, including group calls/viewers. Implement and qualify the intended profile, or explicitly obtain a revised support decision; do not silently treat current smaller constants as measured target capacity. Keep existing enforced limits until safe promotion.

Physical microphone/capture, sustained listening/decoded screen quality, real UDP-blocked routes and device lifecycle remain necessary after these local paths work. A hundred registry cycles and a synthetic forwarding measurement are valuable regressions, but they do not close those physical requirements.

## 8 Make CI recovery and packaging trustworthy

### Clean checkout schema 13 qualification V14-10

CI currently unzips ignored `dist/releases/brrpbrrp-0.3.0-linux-amd64.zip` without acquiring/verifying it; that version is also not a proven schema 13 executable. The pinned baseline manifest contains other releases. The new seed creates no accepted private history or submitted evidence, although the verifier requires evidence. CI supplies `--report`, which is ignored.

Pin a real preserved schema 13 executable by version/source/schema/hash, acquire it explicitly through an approved stable source and fail clearly if unavailable. Seed the required accepted messages, reports/review state, requests and identities through that old version's supported API. Validate the complete registry and exact data preservation after upgrade. Emit machine-readable results with both executable identities. Test missing/wrong baseline, incomplete seed and failed assertion so the workflow cannot pass without doing the rehearsal.

### Full current-product recovery

Extend backup/restore and upgrade fixtures to include:

- Direct/group consent, owners, invitations, retained/removed identities and private attachment bytes.
- Activity acknowledgments/preferences and no stale interruption replay.
- Named threads, root tombstones, follows, read state, messages and operation receipts.
- Moderation assignment/timeouts/current grants and invitation allowance consumption.
- Export jobs/files, interrupted work, expiry, invalidation and cleanup.
- Selected private evidence, receipts/spent keys, exact retention and relevant audit records.

Complete fixture ownership/MFA setup and require real API reviewer revocation. Remove SQL fallback plus unconditional success checks. Direct SQL can seed an explicitly declared database fixture, but cannot qualify the operator API or live authority boundary.

Take snapshots before group/reviewer/role/account changes, show what the isolated restored copy contains, reconcile those security/erasure decisions before cutover, then prove current denial with positive unaffected-member controls. Test old sessions, delayed delivery and reused URLs/streams. Keep TLS/configuration/keys and independent backup custody explicit.

Preserve prior migrations and test current schema 21 to the next schema, populated intermediate thread/export states, migration interruption, restart and future-schema refusal. Never open a new-schema database with an old executable. Rollback uses a verified supported pre-upgrade snapshot and old reader.

### Consistent release artifacts V14-11

Select a new unused prerelease version, likely alpha.8 if still available, and align server/frontend/package/installer/docs identities. Freeze clean source and dependencies, then build every platform being distributed under that version. Do not overwrite alpha.7 again or mix schemas under one manifest.

Record exact archive and extracted executable hashes, source commit, dirty status, frontend build, schema/protocol/backup versions, toolchain, dependencies/notices/SBOM and consumer results. Clearly separate cross-compiled from runtime-qualified platforms. Preserve historical files where available and annotate lost/overwritten history without inventing replacement provenance.

Run fresh native extracted-package and independently installed/built extracted-source consumers. Qualify the actual container image too when Docker is available. A separately built checkout binary and static source inventory do not prove that an archive installs and works. Run current integrated tests against the intended candidate, not the earlier b51ffc2 package.

## 9 Complete operations and capacity on both deployments

### Repair the tools first V14-12

The raw capacity client uses plain TCP with default port 80 even for HTTPS and ignores WebSocket Ping/Pong. Fix TLS/SNI/port handling, protocol framing/heartbeat and shutdown/reconnect. Add a controlled run longer than the server heartbeat interval, denied/broken transport controls and missing-sample failure criteria before a two-hour or 24-hour host run.

Correct the fixture/manifest mismatch: the recorded population is four accounts, three channels, 24 messages and no private conversations. Preserve that historical run as a small smoke result. Generate and verify intended populations independently; fail if required clients/messages/private/media/sample counts are absent. Use valid monotonic/synchronized timing, and distinguish expected fault/admission failures from healthy failures.

Correct Windows upgrade instructions: the installer refuses an existing service. Qualify an actual stop/backup/replace/start/verify procedure with rollback and data preservation.

### Required host lifecycle

Provision disposable Windows x64/Tailscale and Linux amd64 Docker installations, independent recovery destinations, representative clients and a separate load generator. If access is missing, prepare exact runnable procedures and evidence forms, request only the needed resources, and continue local work.

Windows: trusted HTTPS, setup, owner/MFA recovery, UAC/service identity and ACLs, reboot/recovery, operator controls, logs, update/failure rollback, uninstall/data preservation. Linux Docker: actual image/Compose, non-root permissions, intended read-only paths, volumes, health/shutdown/restart, update/rollback, disk/mount faults and media/TURN topology. Native cross-compilation or static Docker checks are not lifecycle acceptance.

### Recovery monitoring and capacity

Qualify encrypted off-host copies and keys, verified backup age, clean-destination restoration and post-snapshot reconciliation. Proposed objectives remain **RPO ≤24 hours and RTO ≤60 minutes**, including operator response, fetching/decrypting backup, reconciliation and verification on the approved reference dataset. Record total outage separately. Test corrupt/truncated archives, failed copy, lost destination and disk pressure.

Complete external HTTPS/WebSocket/backup-age checks and alert delivery to an approved real recipient. Local HTTP receiver tests establish the mechanism, not an on-call service or deployed outage detection. Verify delivered alerts and failure/retry/deduplication behavior; keep private content and secrets out of logs/metrics.

Freeze the master-plan support profile and budgets before qualification. Proposed targets include 250 accounts, 50 active people, 100 sockets with five per account, 100 community channels, ten-member groups, twenty media participants with ten per room, one share per room and two per server, one million messages and 20 GiB of attachments. Keep current message/upload limits unless separately qualified.

Reference allocation: four dedicated cores/vCPUs, 8 GiB RAM, SSD with at least 80 GiB free initially and a measured 100 Mbit/s symmetric link. This is a qualification environment, not a proven minimum. Record actual hardware, OS/build, runtime, filesystem, network and competing load.

Use F-S/F-T/F-X fixtures, baseline/ramp, a two-hour peak and **24-hour mixed soak on each deployment**, including backups, retention, private traffic, revoked membership, reconnects and supported media combinations. Apply the master v1 plan's section 10 delivery, search, reconnect, resource, egress and quality budgets. Measure zero unauthorized delivery, duplicate logical sends or lost acknowledged content under the approved durability contract. Do not revise a failed budget afterward to manufacture a pass.

Physical media qualification includes direct/forced relay and UDP-blocked TCP/TLS, specified impairment, real microphones/listeners, two-hour calls, 100 browser capture start/stop cycles and mobile viewing. Record first-frame/freeze/audio quality against the frozen fixtures. Synthetic loopback writes are not client receipt, codec quality or reference-host capacity.

## 10 Independent acceptance beta and final release

Finish remaining WP13–17 member journeys, full notification integration, media and operational requirements before full beta. Verify newcomer owner/member flows, installation, recovery, permissions, useful defaults and consistent error/loading/offline/removed states.

Obtain independent security/privacy review of the actual exposed routes, audit, export, threads, jobs, live media and release chain. Resolve critical/high findings and rerun affected evidence. Obtain human keyboard/screen-reader, focus/contrast, zoom/reflow, reduced-motion and physical mobile acceptance, alongside Chromium/Firefox/WebKit automation. Automated audits do not establish blanket accessibility conformance.

A non-author must install/update/restore from the documents; a contributor must build/test from clean source. Correct product friction and documents when those journeys fail. Define actual canonical repository/registry/security intake, maintainer access/recovery, signing and support ownership. Test that a private security report reaches its responsible recipient before claiming the intake works.

After prerequisites pass, run **three consenting communities spanning both deployments for fourteen consecutive clean beta days**. Record exact builds, journeys, incidents, alerts, backups and fixes. Follow the master criteria, including prescribed journey completion and cross-device call sessions. Apply severity-based reset rules honestly.

Freeze final source/dependencies/defaults/schema/protocol/docs and distribution artifacts. Run all required final-byte/host checks and clean recovery/rollback. Observe the candidate for **seven consecutive days on both deployments**. Executable/dependency/schema/protocol changes create a new identity and restart the final window; document evidence reuse for packaging/docs changes.

Present the complete reviewable release for explicit publication approval. Once authorized, publish through approved destinations, verify actual downloads/signatures/checksums/install/upgrade, stage rollout and hand off incident/patch/maintenance responsibilities. Plan first-week and day 30 reviews without claiming an unconfigured schedule exists.

Keep camera video, federation, E2EE, native mobile applications, bot marketplaces, multi-community hosting, enterprise SSO and clustering outside this v1 critical path unless the owner explicitly changes scope. Do not silently drop groups, threads, private calls or desktop sharing to reach a version label.

## 11 Verification and working rules

Known local tools, to rediscover if needed:

```text
.tools/go/bin/go
GOCACHE=/private/tmp/brrpbrrp-go-build
GOMODCACHE=/Users/cameronamer/go/pkg/mod
PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
```

Use focused behavior tests during implementation, then one complete integrated baseline on the frozen candidate. Current race entry points use 40 minutes; record the exact effective command and result. Do not repeatedly run the long API race suite after documentation-only changes or treat a rerun as diagnosis.

Typical gates from the repository root:

```sh
npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
.tools/go/bin/go vet ./...
.tools/go/bin/go test -race -count=1 -timeout 40m ./...
```

Read current harness arguments before use. The browser harness is `web/e2e/run.mjs`; `BRRP_E2E_PRIVATE_CONVERSATIONS=1` enables private journeys and `BRRP_E2E_BINARY` selects the executable. Build the matching embedded frontend/server and run both modes. A skipped project or early successful return on refusal cannot count as the feature's acceptance.

For each consequential result record source/dirty state, version/schema/frontend, artifact hash, environment/configuration, exact command/timeout, exit status, counts, duration, report path and limitations. Keep raw machine-readable reports. Preserve failures and their corrections. Do not reuse old totals without inspecting fixture coverage.

Use tests that cross the real boundary: emitted audit events and denied readers; supported timeout routes and actual role expiry; export download after withdrawal; thread lock at commit; lost response preserving draft/key; closed/reopened Activity; remote rendered video and distinct audio; complete CI baseline acquisition; extracted artifact consumers. Retain useful helper tests, but correct tests that assert the defect or fail to exercise their named promise.

Do not bypass broken verification with SQL fallback, weakened assertions, arbitrary waits or approval requests for routine fixture setup. Complete the owner/MFA ceremony in test environments and use the supported API. Keep secrets and real private content out of fixtures and reports.

## 12 Required checkpoints and final report

At each coherent checkpoint provide:

1. Source/branch/status, application/schema/protocol/backup/frontend identities and exact artifacts.
2. Closed V14 findings and completed WP requirements with behavioral evidence; preserve credit for earlier repairs.
3. Features actually usable by owners/members, with meaningful demonstrations where useful.
4. Tests newly run, failures/skips/limitations and any historical evidence reused with its scope.
5. Migration, retention, privacy, export and recovery consequences with operator actions.
6. G0–G8 status, remaining blockers, named external inputs and prepared procedures.
7. The next executable task and enough continuation context for a coder with no prior conversation.

The immediate next milestone is a repaired, integrated current product with reliable local evidence, followed by a coherent new prerelease. Continue through host qualification and independent acceptance whenever resources permit. If they block final release, report the precise candidate/checkpoint and remaining gates; do not claim v1.0.0 is complete because a checklist or version field says so.
