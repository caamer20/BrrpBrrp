# BrrpBrrp progress review and version 1.0.0 completion plan

**Review date:** 30 September 2026.  
**Source:** `cc2a8a140fe00292236b2c723a3d69b0fb30afce`, branch `codex/p9-private-reporting`, clean at review start.  
**Application:** `0.4.0-alpha.7`, schema **21**, backup format **2**.  
**Change boundary:** review and planning documents only. No application implementation was changed.

## Overall assessment

The application has advanced significantly since review 13. There are **83 new commits, 142 changed files and approximately 19,000 added lines** since `6e36c10`. Group chat, activity controls, named threads, moderation, member export, private calls, screen sharing, relay configuration, alerts and qualification tools now have implementations.

The attachment is a partial work transcript: much of it describes the earlier group journey, followed by a checklist marking M3 complete and later milestones open. The current repository contains substantial work beyond that transcript. This assessment therefore uses the current source and attributable evidence as its baseline.

**The project has much more of its intended feature set, but it is not yet a feature-complete release candidate.** This review reproduced privacy, authorization, thread durability and member-experience defects. The remaining work includes significant local repairs and integration; it cannot all be classified as waiting for hosts or the owner.

The correct next objective is a stable integrated candidate: finish the existing features, repair the verified boundaries, qualify current data and artifacts, then run both-host and independent acceptance. No G0–G8 release gate has passed. A percentage-complete estimate would hide the difference between a feature existing, working through its interface, and being safe to release.

## Progress that should be preserved

| Area | Achieved since the prior review | Current qualification limit |
| --- | --- | --- |
| Private conversations | Groups participate in shared navigation, composer, URLs, messages and reporting; activity UI and private preferences exist | Remaining live lifecycle, activity interaction and private-audit issues below |
| Prior V13 repairs | Activity eligibility before pagination, insertion bound/expiry, group revision/cancellation, ended/blocked invitation refusal, shared audience, ownership helper and request-evidence cleanup | Nine selected regressions passed; helper/audience checks are not a full real-browser or account-removal ceremony |
| Reporting | Changed-preview/transport handling and receipt cleanup improved; exhausted-source skipping added | Reconciliation and mixed-source termination still need completion |
| Discussion | Named-thread model, list/reply/follow/lock/archive interfaces, paginated API and large fixture | Commit-time thread rules, receipts, root lifecycle, draft recovery and independent read state remain incomplete |
| Moderation and member data | Report assignment, timeouts, bounded invitations and personal export are implemented | Supported timeout routes, expiry authority and export download authorization fail current probes |
| Everyday interface | Safe tokenizer, upload cancel/retry, permalinks, RTL changes and automated accessibility checks | Whitespace regression, incomplete formatting/member context and missing human acceptance |
| Media | Private-call admission, publication model, screen capture/receive machinery, optional audio, configurable STUN/TURN UDP/TCP/TLS, forced-relay tests and diagnostics | Current packet ownership, mute/stop semantics, private-call policy and browser rendering/capture issues; physical qualification absent |
| Operations | Alert delivery mechanism with local receiver tests, SBOM, host procedures, capacity drivers, additional CI and package-consumer work | CI seed path, current recovery coverage, long-running load driver and distribution identity need local repair before real-host runs |

## Verification performed for this review

### Fresh results

- Frontend: **198 tests passed**, zero failed or skipped.
- Svelte: **zero errors and zero warnings**.
- Server: **nine selected existing V13 regressions passed**.
- Focused server probes reproduced private-audit, timeout, export and thread failures. The main batch took **6.077s**; an initial privilege-expiry fixture hit a disabled-capability setup condition, then its corrected isolated case reproduced the defect in **1.217s**.
- Five additional media probes reproduced blocked-call admission, incorrect private-call revalidation, missing publication ownership, microphone/screen coupling and incomplete audio stop. API package **1.473s**; RTC package **0.408s**.
- Isolated execution of actual TypeScript/store/component logic reproduced activity lifetime, thread-send recovery, message whitespace and evidence pagination defects. These were Node probes, not rendered browser acceptance.
- Source, schemas, workflow, recovery scripts, manifests, package contents and hashes were inspected.

The defect probes correctly exited nonzero because they asserted intended behavior that the implementation does not meet. They were added through temporary overlays outside the repository. No application tests or code were edited.

Local reproduction materials:

```text
Server probes: /private/tmp/brrp-review14-qtlpo0oe/overlay.json
Media probes:  /private/tmp/brrpbrrp-review14-media-u_d8wte1/overlay.json
```

Example media command:

```sh
GOCACHE=/private/tmp/brrpbrrp-go-build \
GOMODCACHE=/Users/cameronamer/go/pkg/mod GOPROXY=off \
.tools/go/bin/go test \
  -overlay /private/tmp/brrpbrrp-review14-media-u_d8wte1/overlay.json \
  ./internal/api ./internal/rtc -run '^TestReview14Media' \
  -count=1 -timeout 3m -v
```

Temporary files are diagnostic conveniences. Permanent implementation regressions must reproduce the scenarios in the repository without depending on those paths.

### What was not rerun

No fresh full Go race suite, full browser suite, release build, deployed alert, physical capture/microphone test, real Windows/Tailscale or Docker lifecycle, independent review, beta or final-candidate observation was performed here.

The M5 ledger records a full race command with API **1959.611s**, but that run predates the newest media changes. Existing browser, recovery and synthetic media results remain evidence for their specific source, fixture and transport. They must not be carried forward as complete current-product acceptance.

## Current blocking findings

The following **V14** identifiers are for this review. P1 indicates a release-blocking privacy, authority or core durability defect. P2 indicates material functional, integration or evidence work. This is a bounded engineering review; independent release security acceptance remains necessary.

### V14-01 Private audit separation was weakened

**P1, reproduced.** `/api/admin/private-reports/audit` is now guarded by ordinary `audit.read` in `router.go:372` and `admin.go:829`. An audit-only account receives **403** from private evidence but **200** from its audit endpoint, with reporter/reviewer identities, evidence IDs and times. The classifier also includes private `group.*` events.

No evidence text was exposed. This is an explicit widening of the current policy, not a bypass of the newly written route guard. Moving private metadata to another URL while granting the same ordinary capability does not satisfy the earlier privacy boundary. The new tests currently approve that widening.

**Required:** restore the approved separation, or obtain an explicit revised policy before exposure. Distinguish evidence audit and group lifecycle audiences; do not silently grant either ordinary auditors or evidence reviewers unrelated private metadata.

### V14-02 Server exports remain downloadable after access withdrawal

**P1, reproduced.** A member's group export was built, then membership removed through the real endpoint. Group history returned **404**, but the existing export download returned **200 with the private group text**. A newly built export also included the group with the private feature disabled.

`export.go:542–573` checks export ownership/state/expiry, then serves the file without reauthorizing its scopes or registering a revocable stream. The conversation selection at `:435` bypasses the common feature/access decision.

These are server-held copies still being delivered, not data legitimately downloaded before removal. Source inspection also finds an oversized document still written after the size check, no finite bound on accumulated ready exports, and incomplete interrupted-job/orphan-file recovery.

**Required:** common scope admission at build and download, revocable delivery, actual output/storage limits and a complete job/erasure/restore lifecycle. Finish reauthentication and successful download through the member interface; the current browser test can pass early on 401/403 without exporting anything.

### V14-03 Timeouts have unsafe scope, commit and expiry behavior

**P1, reproduced.** The UI calls `/admin/users/{id}/timeout`, while the server exposes `/admin/accounts/{id}/timeout`. The server then ignores the route target in favor of body `user_id`: a route-only request returns **404**. Supplying that field returns **500 after committing** the timeout. Lifting returns **500 after deleting** it. Both handlers commit twice.

The probe also observed unchanged revision **1→1** and an active registered stream. HTTP authorization does correctly refuse actions while the timeout is active, so this is not a claim that all timeout enforcement is absent.

More seriously, expiry deletes current role assignments and restores the old snapshot. A role granted before timeout, revoked through the role API during it, and absent afterward was **restored by a later real login**. The existing expiry test asserts this behavior.

The master plan scopes community timeouts to community actions and requires expiry to restore only otherwise-allowed powers. Current account-wide inactivity/login refusal and snapshot replay conflict with that contract.

**Required:** one route/target/commit contract, bounded duration UI, audit/revision/live enforcement, community scope, and expiry that never resurrects revoked authority. Update the ADR and unsafe tests together.

### V14-04 Thread storage and receipts can strand or misdirect work

**P1, reproduced.** Thread state is checked in one transaction, then sending occurs in another. A deterministic lock between those stages still allowed **201**. Sending the same key/content to another thread in the same channel returned **200 with the first thread's message**, because the intent digest omits thread identity.

Deleting a thread root through the real message route removed its thread row and made thread history return **404**, while an acknowledged reply remained stored and excluded from ordinary channel history. The root foreign key cascades instead of implementing the promised tombstone.

Source inspection also finds no independent thread read cursor, and archive does not share the lock's write refusal.

**Required:** thread state and destination identity inside the committing shared send path; safe migration/receipt compatibility; root tombstones and reachable historical replies; thread-specific read/follow/archive/reopen behavior.

### V14-05 Thread recovery and message rendering lose user intent

**P1/P2, locally reproduced in actual client logic.** Named-thread sending clears the draft before awaiting the request and always rotates the operation key afterward. The store catches errors. Both refusal and lost-response cases leave an empty draft, replacement key and no pending message. The shared outbox is not used.

The tokenizer also removes meaningful whitespace: two lines become one concatenated line, and separated mentions/URLs run together. This is display corruption, not stored text loss.

**Required:** reuse the normal draft/outbox/receipt recovery contract for threads, preserve text and key through uncertainty, and bound client history windows. Preserve paragraph breaks and inline separators while keeping raw HTML inert. Validate actual rendered behavior, not source strings that merely assert key rotation exists.

### V14-06 Activity and evidence panels fail ordinary navigation

**P2.** Locally reproduced: leaving Activity destroys its exported singleton permanently; reopening makes no new API call. Source inspection finds “Mark seen” sends the oldest visible ID, while the server marks older records instead of the displayed page; the client then marks visible entries seen only locally. Row clicks do not resolve their specific request/conversation destination.

Mixed evidence pagination is still defective: with 120 message and 70 request reports, a probe reaches all 190 rows but never finishes through seven pages. A synthetic skipped-source response restores a nonempty cursor and makes the exhausted source appear active again.

The worker writes `brrp.notified.…` keys but cleanup scans `brrp.notify.…`; retained deduplication entries are not reclaimed. App and worker notification paths also do not share atomic admission.

**Required:** account-safe controller lifetime, precise persisted acknowledgment, useful destinations, terminal exhaustion independent of cursors, source-qualified render keys and one bounded notification deduplication contract across app/worker.

### V14-07 Private calls use the wrong access rules

**P1/P2, reproduced in current admission/enforcement helpers.** A blocked direct pair correctly gets **403** for a new message, but can still be admitted to a private call: room resolution uses retained-history read access. Revalidating a still-authorized private participant evicts them because enforcement looks for a community channel row.

**Required:** separate historical readability from live contact/call eligibility, and use the private room resolver for ongoing admission/revalidation/resume. Test block/unavailable peer/removal and positive unchanged-authority controls. Apply the master plan's membership-scoped private call moderation; community moderation must not gain unrelated private-call powers.

### V14-08 Media forwarding does not bind the full publication

**P1/P2, reproduced at the runtime forwarding/registry boundary.** While A owns the room's screen publication, unregistered publisher B can advance the viewer's screen output: the forwarding guard checks whether any publication of that kind exists in the room, not whether this peer owns the permitted publication.

Separate probes show microphone mute suppressing screen video, and stopping video plus screen audio leaving an audio publication registered. The master plan requires separate microphone/video/screen-audio behavior.

These probes use registered peers and synthetic RTP/current forwarders; they do not claim a decoded browser image or physical capture was observed. Source inspection additionally finds that all arriving audio tracks are classified as microphone and a second audio input is dropped. A screen-audio registry record is therefore not proof of independent audio transport. Publication capacity checks are separated from final registration, leaving a concurrency boundary to repair.

**Required:** bind owner, room, connection, publication identity/generation, track and subscriber at admission and forwarding; reserve capacity atomically; reject stale/unknown tracks; independently route and stop both screen tracks. Test actual producer-to-consumer transport as well as registry bookkeeping.

### V14-09 Browser capture and screen reception need end-to-end completion

**P1/P2, source-confirmed; no browser reproduction in this review.** `attachScreen` assigns `srcObject` before setting the flag that mounts the video element, with no later binding effect. The first remote share can consequently show a blank element. The browser journey checks capture/status/stop on a publisher, not decoded frames in a second browser.

`startScreenShare` checks the room before awaiting the browser chooser but captures no room/session generation. It subsequently publishes through the current peer. A chooser completed after a room/account change can therefore apply an old gesture to a new audience unless fenced.

**Required:** separate local capture from received streams, bind mounted media elements to current streams, and cancel stale capture results by stopping all returned tracks. Test delayed chooser results, leave/move/account change, late subscribers, first frame, stop/withdraw/restart and sound on two real browser peers before physical qualification.

### V14-10 CI and recovery still do not qualify the current product

**P1/P2, source/evidence findings.** The schema13 CI step unzips an ignored local `0.3.0` archive without fetching/checking it; that release is also not a proven schema13 baseline. The seed script creates no accepted private history or submitted evidence, while the upgrade script requires evidence. CI passes `--report`, but the script does not write one.

The 113-check private recovery fixture still lacks explicit groups, activity, threads, moderation and export scenarios. Reviewer revocation can still fall back to SQL followed by unconditional successful checks.

**Required:** a pinned real schema13 binary, API-created complete seed, registry/data assertions, actual report output and failure controls. Complete fixture ownership/MFA and require supported API revocation. Extend current-schema restore and post-snapshot reconciliation across every new data type. This work is feasible locally.

### V14-11 Alpha7 packages now represent different source and schemas

**P1 for release preparation, independently verified.** The latest consumer record describes clean packaged source `b51ffc26956fbc6a80cb974c3d5975b367a59b46`, schema21. The source ZIP's 452 files match that commit. Fifteen packaged files differ from current HEAD, including media code and UI.

Current Darwin/amd64 and Linux/amd64 packages are schema21. The other four native alpha.7 packages are still P10/schema14. Three historical artifact paths have been overwritten; `SHA256SUMS` now lists only the two rebuilt platforms.

| Current file | SHA-256 |
| --- | --- |
| Darwin/amd64 ZIP | `e9fb0fb3cc4455ec62b50fa2d3a87a92d72cc50582f584d24a46813c3cbb9091` |
| Extracted Darwin executable | `0367f3fd02b858e400fc216e70e7953c9c6cf821d220ac9678eecf6028f020ac` |
| Linux/amd64 ZIP | `2dc7c74534df1eedc6bcf00eabf3d6a54ff94b74f2f1372e12a77112a076a271` |
| Source ZIP | `18f606b923119486db7770e499f2362c7198bf011666f41bdccba73f6b1f8e6a` |

**Required:** a new unique prerelease version, coherent platform inventory, clean source and exact final-byte identities. Preserve any original historical bytes still available; label unavailable/overwritten historical artifacts honestly. Do not rebuild different bytes and claim the old digest or silently overwrite alpha7 again.

### V14-12 Qualification tools need repair before host measurements

**P2, source/evidence findings.** The capacity client always uses plain TCP/default port80, including HTTPS URLs, and ignores WebSocket Ping frames. The server requires Pong within ten seconds. Those defects prevent the intended HTTPS and long-running qualification.

The saved loopback fixture contains four accounts, three channels, 24 messages and **no private conversations**, despite a conflicting manifest statement. The load run lasted 15 seconds. The saved video measurement covers one synthetic publisher/nine viewers on a developer host, not the two-share reference deployment or decoded browser quality.

Windows instructions invoke installation over an existing service, which the installer refuses. Alerts have local receiver/signing tests but no qualified real operator destination or external outage monitor.

**Required:** TLS/heartbeat/protocol-correct load generation, meaningful completeness and failure criteria, corrected population records, an actual update procedure and real-host evidence templates. Then run the planned measurements on supplied infrastructure.

## Implementation sequence to reach a stable candidate

1. **Repair privacy and authority first:** private audit, export delivery, timeout scope/expiry/transactions, private call admission and publication ownership. Preserve working group/activity/reporting fixes.
2. **Repair durability and author recovery:** thread commit/receipt/root lifecycle, shared draft/outbox, export job lifecycle, group terminal retention and account-state policy.
3. **Finish member workflows:** activity navigation/acknowledgment, evidence pagination, whitespace/formatting, timeout controls, export reauthentication/download and independent thread read/follow/reopen with bounded history.
4. **Complete media behavior locally:** separate publication transport/control, capture fencing, actual two-browser screen reception, safe renegotiation and deterministic cleanup. Keep synthetic and physical evidence distinct.
5. **Make verification and distribution dependable:** real baseline CI, full-product recovery, corrected capacity drivers, current-source integrated tests, a new clean prerelease and extracted native/source/container consumers.
6. **Qualify both real deployments:** Windows x64/Tailscale and Linux amd64 Docker, update/rollback, encrypted independent restore, alerts and the approved reference capacity. Provision these resources while independent coding proceeds.
7. **Independent acceptance and release:** privacy/security and human accessibility review, non-author installation/contributor rehearsals, full-product beta, final candidate observation, explicit approval and verified publication.

The [standalone handoff](handoff-v1-completion-review-14-2026-09-30.md) gives the next coder concrete implementation and acceptance instructions for this sequence.

## Remaining release gates and external inputs

Both required hosts, physical devices/networks, independent reviewers, canonical repository/registry/signing identities, independent backup custody, an actual alert recipient and pilot communities are still required. Missing infrastructure does not block the local fixes above.

Keep the master plan's limits as **qualification targets**, not established capacity: 250 accounts, 50 active people, 100 sockets, ten-member groups, twenty media participants/ten per room, two serverwide shares and the target retained dataset. Current media still has ten serverwide places and a two-participant private-call bound; approve or implement/qualify the final profile explicitly.

Each required deployment needs the approved peak and 24-hour soak. RPO ≤24 hours and RTO ≤60 minutes include operator response, fetching/decrypting independent backups, reconciliation and verification. Synthetic tests and local recovery counts do not establish these objectives.

After the product and prerequisite reviews pass, run the planned **three-community, fourteen-day beta across both deployments**, followed by **seven consecutive days on the frozen final candidate**. Apply the documented reset rules after material fixes. Publish only after the concrete final result is approved.

Camera video, federation, E2EE, native mobile apps, bot marketplaces, multi-community hosting, enterprise SSO and clustering remain outside this v1 critical path unless the owner changes scope. The release goal is the complete agreed product with honest evidence, not a premature version-number change.
