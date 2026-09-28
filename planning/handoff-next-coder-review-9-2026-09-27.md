# Standalone coding-agent handoff — P9 private reporting and request discovery

Prepared 27 September 2026. Copy **everything below the separator** into the next coding session. This prompt assumes repository access, but no previous conversation or agent memory. It supersedes the review-8 execution prompt; that work has largely been implemented.

---

## 1. Mission and bounded assignment

You are continuing **BrrpBrrp**, an open-source, self-hosted community chat application intended to become a user-friendly Discord replacement. Product priorities are simple installation/operation, understandable consent and permissions, reliable conversations, recoverable user work, moderation, accessible desktop/mobile use, voice and screen sharing.

**Implement and verify the P9 tranche described here. Do not stop after another plan.** First repair the concrete CI, preference-lifecycle and evidence gaps found after P8; then build selected private-message text reporting and persistent incoming-request discovery. Finish with a clean, reproducible checkpoint. Full DM push/activity, groups, threads and media expansion are subsequent milestones, not unfinished extras to start in parallel with this assignment.

The current user is asking you to continue development, not to certify v1 from a version string. No release gate is accepted. Do not inherit the previous agent's unlimited goal, token usage, time allowance or publication authority. Work efficiently: reproduce focused defects, integrate coherent changes, then run the required expensive final verification on the frozen checkpoint.

## 2. Repository, current state and reading order

Known local workspace: `/Users/cameronamer/Documents/ChatGPT/BrrpBrrp`. If moved, use the actual repository root. If you only have this prompt, request the source checkout/archive once. No canonical public remote URL was configured at the review; do not invent one or assume credentials from another agent.

Reviewed HEAD: **`876260ab1c01313efd3358459b4178c010ee2f45`**, clean at the start of review 9. P8 implementation commit: **`606cdb120326507c280cf0c1fbc89c85b44598cd`**. Current version **`0.4.0-alpha.6`**, schema **12**, backup format **2**. Review-8 documentation was committed at `17893f9`; alpha.6 began at `1f762e9`. Check actual Git status/HEAD before editing and preserve later user/agent changes, including new review-9 planning documents.

Read in this order, then inspect relevant source:

1. `planning/completion-review-9-and-next-steps-2026-09-27.md` — latest assessment, findings R9-01–06 and P9 order.
2. `planning/p8-checkpoint/verification.md` and its recovery/rollback JSON files — existing results, with the corrections below.
3. `docs/v1-progress.md` and `docs/releases/0.4.0-alpha.6.md` — implementation status. Some older table rows remain stale; review 9 explains the current boundary.
4. ADRs 0007–0012 under `docs/adr/` — attachment durability, backup verification, authority/admission ordering, shared receipts/outbox/cursors, account lifecycle/report retention and direct consent/storage.
5. `docs/v1-implementation-plan.md` — complete v1 scope, WP-10–15 and release gates; `docs/architecture.md` for layout.

Do not reapply the review-8 fixes from scratch. Its original three defects are repaired. New R9 issues are narrower follow-ups, not proof those repairs made no progress.

## 3. What already exists

One Go executable serves a Svelte/TypeScript frontend and SQLite database; Node is a build tool, not a separate production application server. SQLite uses WAL and a single connection. WebSockets carry bounded event queues; Opus/Pion SFU voice is embedded. Native service installers and Docker sources exist.

Implemented community functionality includes messages, files, replies, search, pins, reactions, mentions, PWA shell/themes/mobile layouts, MFA/session recovery, owner ceremony, additive roles/delegation, restricted-channel controls, slow mode, selected community-message reports, per-channel mention muting and history-preserving account removal.

Alpha.6 provides opt-in DMs with canonical requests, explicit consent, durable request limits, contact opt-out, block/unblock, personal hiding, shared messages/files/replies/search/read state and recoverable sends. Private and restricted-channel features remain **default-off** behind `BRRP_ENABLE_PRIVATE_CONVERSATIONS` and `BRRP_ENABLE_RESTRICTED_CHANNELS`; test them on disposable opted-in servers. Keep shipped defaults unchanged.

Both **Windows native service/Tailscale and Linux Docker** are required qualification targets. MIT and CLA choices are already recorded. Do not ask the owner to choose these again. Existing limits include 100 live/reversibly deactivated accounts, 100 channels, 12 MB uploads and 10 voice participants serverwide; higher master-plan targets require measurement, not a constant change.

No groups, private calls, screen sharing, complete named threads, private evidence reporting or dedicated DM notification/activity system exist yet. The operator controls stored data/backups/media. There is no end-to-end encryption claim; unrelated administrators still cannot browse DMs through ordinary application APIs.

### Code and test map

| Area | Starting files |
| --- | --- |
| CLI/host/runtime | `cmd/brrpbrrp`, `internal/app`, `internal/installer` |
| Storage/migrations | `internal/db/migrations.go`, `migration9.sql`, `migration10.sql`, `migration11.sql`, `migration12.sql`, `receipts.go` |
| Current community reports | `internal/api/reports.go`, `reports_test.go`, `report_retention_test.go`, `maintenance.go` |
| Private scope/consent | `internal/api/conversation_access.go`, `direct.go`, `contacts.go`, `state.go`, `conversations.go` |
| Shared message/files | `internal/api/messages.go`, `uploads.go`, `staged_attachment_claim_test.go`, `message_receipts_test.go` |
| Permissions/routes | `internal/authz/authz.go`, `internal/api/router.go`, `web/src/lib/permissions.ts`, `types.ts` |
| Client state | `web/src/lib/workspace.svelte.ts`, `outbox.ts`, `conversations.ts` |
| Report interfaces | `web/src/lib/components/ReportMessage.svelte`, `MessageItem.svelte`, `admin/Reports.svelte`, `AdminPanel.svelte`, `admin/Roles.svelte` |
| Requests/preferences | `components/MessagesInbox.svelte`, `Workspace.svelte`, `DirectControls.svelte`, `ChannelNotifications.svelte`; `internal/api/channel_notifications.go` |
| Notifications | `internal/api/mentions.go`, `push.go`, `notification_lifecycle_test.go`; `web/public/service-worker.js` |
| Client tests | `web/tests/workspace-state.test.mjs`, `web/e2e/directs.spec.mjs`, `privacy.spec.mjs`, `account-switch.spec.mjs`, `support.mjs` |
| Recovery | `scripts/upgrade-recovery.mjs`, `upgrade-recovery-private.mjs`, `upgrade-rollback.mjs` |
| CI/artifacts | `.github/workflows/check.yml`, `docker.yml`, `scripts/release.go`, `check-package.mjs`, `check-source-package.mjs`, `fetch-baseline-release.mjs`, `baseline-releases.json` |

Use symbol searches; line numbers in reports can move. New capability/API/type names below are proposals to define, not symbols already implemented.

## 4. Contracts you must preserve

1. **Authority ordering:** acquire the store connection/transaction before the authority fence. Never acquire the single database connection again through `s.store` while holding a transaction; use `tx`. Re-establish account/session/capability in the transaction. Authority-changing commits and delivery invalidation follow ADR 0009. Network/stream/voice cleanup happens outside the fence. Preserve late-delivery admission checks.
2. **Shared conversation model:** schema 11 already preserves message rowids/IDs and relationships while requiring `conversation_id` and allowing nullable `channel_id`. Channel conversation ID equals channel ID; private messages have no channel projection. Do not redo this rebuild, create shadow channels or duplicate private message storage. Preserve FK/FTS/postcondition recovery.
3. **Consent:** only the recipient accepts a canonical request; crossed requests do not auto-accept. Accepted current membership grants reads. Owner/admin roles do not grant private history access. Current durable limits are 500 Unicode introduction characters, five outgoing pending, twenty incoming pending, ten new pairs/day and a 24-hour rejection cooldown.
4. **Blocking/hiding:** accepted history stays readable after block; new contact mutations stop. Own deletion and necessary deletion events remain. No retry replay after unblock, no automatic unhide, and no claim that block hides shared community content. Reporting must use read eligibility so blocked victims can report.
5. **Private surfaces:** scoped search only; global search remains community-only. Private event/file/count audiences use current authorization. No private external URL preview fetches, no private message text/membership in community audit, no unrelated-admin original-message access through report tools.
6. **Receipts and drafts:** send identity is `(account, key)` with destination inside the intent. Replay must not create side effects; deleted/expired keys never resurrect. Preserve opt-in account-scoped outbox bounds (50 records/64 KiB UTF-8 text), 24-hour retry limit, explicit copy/recovery and monotonic read cursors. Retain session/account/authority/version guards on successful and failed asynchronous work.
7. **Lifecycle:** deactivation, permanent Deleted member removal and account/message erasure are distinct. Backup restore rolls security state back to its snapshot; later bans/blocks/role changes/password changes/erasure need explicit reconciliation before traffic cutover. Account deletion does not rewrite historical backups.
8. **P8 attachment fix:** current upload capability is checked for a new attachment claim after replay handling. Keep text/typing/reaction paths independent of that capability. Retain zero-side-effect denial and same-key retry after permission restoration.
9. **Community reports:** selected text ≤4,000 Unicode characters, reason/review note ≤2,000, expiry 90 days after creation, receipt-only submit response, read-filtered pagination, current channel access plus `message.delete_any` for review. Resolve/reopen does not punish an account or extend expiry. Preserve these contracts while adding private evidence.

## 5. P9-01 — Close the verified follow-ups first

### R9-01: CI must fail when browser tests fail

Both browser runs in `.github/workflows/check.yml` pipe `npm run test:e2e` to `tee` without `pipefail`. GitHub's unspecified Linux shell uses `bash -e`; explicit `shell: bash` adds `-eo pipefail`. The later `[directs]` grep proves only that project text appeared. A failed test that prints its project can leave the step green.

Fix failure propagation for both modes without losing logs/traces/screenshots on early exit. Preserve package dependencies on native tests, browser, Docker, recovery and dispatch rollback. Add a meaningful runner/pipeline regression or self-test with a failing test command that prints `[directs]`: it must fail. Include a passing control. Preserve diagnostic collection even when strict shell handling stops before the existing `mv`; use failure/always handling or collect original paths appropriately. Do not globally force Bash onto Windows steps without checking their platform requirements.

Primary reference: https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idstepsshell . A local shell control established that `false | tee /dev/null` returns 0 under `bash -e` and 1 with pipefail. YAML parsing alone does not prove the repair.

### R9-02: refresh account notification policy across devices/reconnect/focus

P8 fixed fresh-DM loading. A new sequence remains: tab A caches a channel as muted and stays in a DM; device B unmutes; the PUT commits without an account invalidation; A's ready flag stays true, including through same-revision reconnect, and the only focus refresh is in the absent channel header. A continues dropping eligible mention events indefinitely.

First establish a rendered two-session reproduction. Implement account-owned preference invalidation/refresh covering other-device changes, reconnect and focus, with bounded retries. Retain stale GET/newer PUT fencing, account/session/generation isolation, server dispatch checks and fail-closed unknown policy. Prefer a scoped preference-change signal/refresh contract over broad unbounded polling or fetching on every mention. Test both mute and unmute, staying in the DM, same-revision reconnect, delayed reads, account switch and failures.

### R9-05/06: make two recovery controls and one client control meaningful

- `upgrade-recovery-private.mjs` checks zero restored push subscriptions without seeding one. Seed a fixture subscription, prove it exists before backup and disappears on restore; no real external push delivery. Preserve old-session 401 assertions.
- The fixture deliberately loses a post-snapshot block and deactivation, but only reapplies the block. Reconcile both on the isolated restored server and prove blocked writes and deactivated-account denial before declaring recovery complete.
- The existing request-draft browser test switches accounts only after a successful send cleared the text. Add a switch with a genuinely unsent introduction and a held asynchronous response. No text, target or result may appear for account B. Source clearing appears correct; this is a regression gap, not a confirmed leak.

### R9-03/04: correct evidence and freeze clean source

P8's packaged Darwin binary matches its recovery reports but embeds `606cdb1` with **`vcs.modified=true`**. Its source ZIP differs from that commit in five packaging/documentation files and from current HEAD only in two progress-header lines. Do not call it a clean-commit build or infer an untested application change from that alone.

Add dated corrections to the existing evidence ledger: `168e89422…` and `3e08497f…` are backup ZIP hashes, not old binary hashes. Correct `baseline.binary_sha256` values are alpha.5 `79d90400e39ce2b08bcbfd15af46b4c95436c2977cbe3117b5063423b2960278` and alpha.3 `918c4c873b4d848aaaeeaf30a187db985935472ab8b27d74c1472ab13e365099`. Final P8 source has 370 entries; 367 was before three packaging additions. Reconcile stale status rows without erasing historical results.

Preserve old archives and their digests. Produce new artifacts only after P9 code, schema, packaging inputs and shipped documentation are committed and clean. Record immutable source/frontend/binary/archive identities. Add evidence afterward without retrospectively relabeling which source produced the tested bytes.

## 6. P9-02 — Write the private-safety contract, then implement it

Record the following bounded design in a new ADR before adding schema/routes. These are conservative proposed defaults for a gated alpha; they do not open the production gate. Resolve ordinary implementation choices yourself. If an actual owner policy is missing and cannot be resolved conservatively, isolate that question and continue independent work.

### Evidence scope and reviewer audience

- Report exactly **one currently readable accepted-DM message's text**, its author/reporter identity and the reporter's explanation. No adjacent history, reply content, attachment bytes/URLs/thumbnails, external previews or automatic export. Clearly disclose this boundary, including attachment-only messages.
- Use a distinct capability, proposed `report.review_private`, for evidence review. Owners already receive every valid capability in `Principal.Allowed`; other reviewer roles should require explicit delegation. Do not automatically grant this to every community moderator or use `is_admin` shortcuts. Declare any recent-auth requirement and its consistent route/UI behavior.
- A reviewer gets the submitted snapshot only. They gain no private membership or rights to original message/context/search/files. A community-only moderator cannot see private queue records, counts, identities or cursor side channels.
- Show who can review the evidence and exactly what is disclosed before confirmation. Submission returns only the reporter's own receipt, not reviewer notes or another reporter's identity/reason.

### Preview integrity and lifecycle

Current community reporting previews cached text and snapshots current server text when submitting. Private disclosure needs a stricter contract: fetch an authorized server snapshot/version, show it, and confirm only that exact evidence. If edited between preview and confirmation, refuse or return a new preview requiring consent. Do not silently disclose unseen replacement text or accept fabricated client text as server evidence. Recheck identity/read access inside the report transaction.

Keep 90-day availability from creation, unchanged by resolution/reopen; separate expiry from bounded physical cleanup. Define source deletion, account removal/erasure, reviewer revocation, backup/restore and private-feature-off behavior explicitly. Suggested gated behavior: disabling private features refuses private report submission/review and clears private UI, while retaining stored evidence only until normal expiry. Do not pretend disabling a feature erases backups.

Define durable per-account submission bounds and a server storage bound for private evidence, plus retry/duplicate identity and concurrency outcomes. Reuse community size limits; a starting rate of ten new private reports/hour/account aligns with current community intent, but enforce the new private limit durably and document any chosen cap/cooldown. The old community limiter is in memory; do not claim it is restart-resistant. Return useful Retry-After/errors and do not silently evict unexpired evidence under pressure.

Private review changes must have atomic audit metadata with an explicit failure policy, without placing text/reasons/membership in the general community audit feed. Store/access any private audit details behind the appropriate evidence policy. Resolving a report records triage; it does not implicitly delete a message, ban someone or grant moderation power over a private conversation.

### Explicit scope exclusions

This P9 slice does **not** add selected file evidence, report assignment/timeouts, full activity history, groups or DM push/ringing. It also does not cover pending-request introductions. Rejection/block clears an introduction today; a later request-harassment report feature requires recipient-only server-verified evidence and an explicit snapshot-before-block workflow. Never require accepting a harasser merely to report them, covertly retain rejected introductions, or construct their supposed server evidence from stale client text. Record this limitation and schedule it before broad private pilots.

## 7. P9-03/04 — Implement private text reporting end to end

### Storage/API/authorization

Current `message_reports` in schema 9 requires `channel_id` and cascades on channel deletion. Simply removing the community-only SELECT filter or enabling the existing button is unsafe. Choose an explicit discriminated report model or separate private-evidence storage with shared well-factored mechanics. A separate evidence table is not a second message store; it contains only voluntarily submitted immutable snapshots. Explain the migration choice in the ADR.

Assign the next unused ordered migration after inspecting the actual registry (currently 12). Preserve existing community report rows, original IDs/expiry, readable-channel pagination and source/channel deletion semantics. Do not alter old migrations in place. Test upgrade from schema 12 and representative older fixtures, cancellation/failure, FK integrity, FTS and preserved messages/files/receipts.

Submission must use current accepted **read** access, so a blocked participant with retained history can report. A pending requester, outsider or nonparticipating owner/admin must not report by guessing message IDs. Validate identifiers/input size, gate state and session inside the transaction. Deduplicate concurrently retried submissions without replacing the original evidence or explanation. Include reliable preview-to-submit conflict behavior and source-delete races.

Review/list/update paths require the new capability at transaction time and current feature policy. Filter private/community records before pagination/limits. Loss of reviewer permission must fence later deliveries and mutations through the established authority contract. New routes must be in the route-policy inventory; add the capability to server/client catalogs, role-management labels, admin-panel visibility and relevant tests without granting unrelated management powers.

### UI and accessibility

Enable Report for eligible DM history, including blocked read-only history. The disclosure modal shows the exact server-confirmed text, author/reporter disclosure, purpose, expiry and exclusions. Keep explanation text across a retry or changed-preview challenge for the same account; prevent stale account/target responses from changing the next user's form. Escape text; no raw-HTML or URL-fetch shortcut.

Expose a clearly separated private-evidence queue to authorized reviewers. Do not filter private evidence by the reviewer's community channel list or offer a link that bypasses original DM authorization. Include loading, empty, error/retry, expired and permission-revoked states; safe bounded pagination; review notes; explicit nonpunitive resolve/reopen wording. Pending evidence reads must not repopulate the view after account/capability changes.

Test keyboard navigation, labels, focus return, narrow layout and zoom for the new flows. Reuse existing components/design language and keep permission/backend implementation terms out of ordinary member instructions unless needed to explain disclosure.

## 8. P9-05 — Make incoming requests discoverable

Add a persistent accessible **outstanding incoming request count** to the Messages entry and a direct way to open its Requests tab. This is not an unseen-event count and does not require a general activity table.

Derive it from durable current requests addressed to the signed-in account. Include only pending eligible incoming requests; exclude outgoing, rejected, withdrawn, blocked and unavailable identities. Opening the modal does not clear the count. Accept/decline/withdraw/block, including from another device, updates it; initial load and reconnect reconstruct it. Keep accepted hidden conversations hidden and message unread cursors unchanged.

Add an account-scoped summary to the appropriate snapshot/API contract. Do not fetch every request introduction into the closed workspace just to display a badge. Reuse `direct.changed` as an invalidation hint with admitted snapshot fetches, not as authority-bearing data to trust blindly. Clear counts while account/authority is unverified and reject stale responses. Preserve P8's independent introduction-draft ownership during summary/list refreshes.

For this tranche, the explicit notification contract is **request badge plus existing DM unread state, with no new DM OS/push/sound interruption**. Update help/settings copy so users understand the distinction. Do not pass DMs into community mention push with an empty channel ID; that path's authorization assumptions are channel-specific. A full DM activity/preference/deduplication system is the next separate implementation contract.

## 9. Acceptance matrix and recovery

Add behavior-level regressions, including positive controls. Do not merely assert that a new function calls another new function.

| Surface | Minimum proof |
| --- | --- |
| CI | Failing default/private test commands fail the steps even when logs contain `[directs]`; passing controls work; diagnostic artifacts retained; packages stay gated |
| Preference lifecycle | Two sessions mute/unmute while receiving tab stays in DM; reconnect/focus reverify; delayed GET cannot overwrite newer PUT; no stale account policy |
| Report authorization | Participant succeeds; blocked readable participant succeeds; pending/outsider/nonmember owner/admin denied without existence disclosure |
| Evidence disclosure | Saved snapshot equals confirmed preview; edit before confirmation requires new consent; fabricated body rejected; no files/replies/history included |
| Reviewer isolation | Evidence reviewer reads only submitted snapshot; original DM history/context/search/files still denied; community-only moderator sees no private queue metadata |
| Races/lifecycle | Submit/edit/delete/retry, concurrent duplicates, capability loss during queue/review, source/account deletion, expiry/cleanup, audit failure and feature disable behave as contracted |
| Request badge | Live closed-modal arrival; restart/reconnect; another-device accept/decline/withdraw/block; opening does not clear; hidden conversations unaffected |
| Client isolation | Held responses across account/authority changes; genuinely unsent introduction never crosses account switch; current form text survives unrelated refresh |
| Restore | Existing P8 fixtures remain; private report evidence/expiry/review access and request count reconstruct; seeded push registrations removed; post-snapshot block and deactivation explicitly reconciled |
| UI | Keyboard/narrow-layout critical paths, useful error/retry states, accurate accessible counts, no repeated noisy announcements |

Extend `reports_test.go`, `report_retention_test.go`, direct privacy/publication tests and the maintained private recovery script as appropriate. Existing Node workspace tests do not render Svelte component effects; use Playwright for mounted reporting/request/notification journeys. Add account/reviewer revocation and held-response controls, using `web/e2e/support.mjs` helpers and deterministic barriers rather than arbitrary sleeps. Keep default-off and opted-in browser runs required.

Use disposable data. In recovery, assert required state exists before backup so absence after restore is meaningful. Reauthenticate against the restored server; prove permitted and denied reads/writes, evidence retention and absence of old sessions/devices. Keep the restored instance isolated during reconciliation. Preserve actual alpha.5/schema-10 and alpha.3/schema-2 rollback tests. A candidate-created format-2/schema-10 pre-upgrade archive can be accepted by alpha.5, while alpha.3 rejects format 2; test actual pair compatibility rather than assuming a universal rule.

## 10. Toolchain and execution commands

Known tools: `.tools/go/bin/go` (Go 1.27.1), Node 24.2.0, dependencies in `web`, Playwright Chromium. A fresh checkout needs supported tools, `npm --prefix web ci` and Chromium installation from `web` with `npx playwright install chromium` if absent. The frontend must be built before Go because assets are embedded.

Inspect `go env GOMODCACHE GOCACHE`; do not blindly reuse temporary directories from another session. Review 9 successfully used the existing `/Users/cameronamer/go/pkg/mod` with a writable temporary build cache. Its first attempt with the older `/private/tmp` module cache needed unavailable network downloads. On another host, use the valid installed caches/toolchain; classify missing dependencies separately from test failures.

Example final baseline from the repository root, adapting paths to the actual environment:

```sh
export BRRP_GO="$PWD/.tools/go/bin/go"
export GOCACHE=/private/tmp/brrpbrrp-go-build
export PLAYWRIGHT_BROWSERS_PATH=/private/tmp/brrpbrrp-playwright-browsers
export BRRP_E2E_BINARY="$PWD/dist/brrpbrrp-p9-checkpoint"

npm --prefix web test
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
"$BRRP_GO" vet ./...
"$BRRP_GO" test -race -count=1 -timeout 20m ./...
mkdir -p dist
CGO_ENABLED=0 "$BRRP_GO" build -trimpath -ldflags='-s -w' -o "$BRRP_E2E_BINARY" ./cmd/brrpbrrp
BRRP_E2E_PRIVATE_CONVERSATIONS=0 npm --prefix web run test:e2e
BRRP_E2E_PRIVATE_CONVERSATIONS=1 npm --prefix web run test:e2e
node scripts/licenses.mjs --check
node scripts/check-docker-inputs.mjs --self-test
node scripts/check-docker-inputs.mjs
node scripts/check-source-package.mjs --self-test
node scripts/check-source-package.mjs
node scripts/fetch-baseline-release.mjs --self-test
git diff --check
```

Use focused tests during implementation. The full race API package previously took about 15 minutes; allow the intentional 20-minute package timeout and do not repeatedly restart it. The e2e wrapper creates/removes a temporary server and strips ambient `BRRP_` variables; use the dedicated feature-mode variable and verify the selected projects. Local socket binding may require the current environment's execution permission; follow that mechanism without weakening tests. Do not change production login limits to make browser fixtures pass.

Recovery/consumer entry points (replace uppercase placeholders with actual absolute paths):

```text
node scripts/upgrade-recovery.mjs --binary CANDIDATE --report NEW_REPORT
node scripts/upgrade-recovery-private.mjs --binary CANDIDATE --report NEW_PRIVATE_REPORT
node scripts/upgrade-rollback.mjs --binary CANDIDATE --old-release BASELINE_ZIP --report NEW_ROLLBACK_REPORT
node scripts/check-package.mjs EXTRACTED_CANDIDATE_BINARY
```

Use one identified candidate for browser/recovery checks, then verify the actual packaged/extracted bytes. A source build and a linker-stamped package may differ; do not borrow the source-build result as proof of untested package identity. Clean source consumption needs extraction, dependency installation, frontend build, Go build and executable smoke. Store new evidence in a new checkpoint directory, not over old P8 reports.

## 11. Artifact supply and clean checkpoint discipline

All alpha.6 P8 archive hashes and all twelve pinned alpha.3/alpha.5 archive hashes were verified in review 9. Existing candidate binary is `68c2458fbd9fcb248d691d6dcae24fe15d4a5ac0b527220a7634d3274f9d5bc8`; source ZIP is `623e4d303ea815bcec6bae7db6caf7f18d0315ec0e59b4992a2b7b19d6b87ee0`; frontend is `8263e263579a2b47`. Preserve their history with the dirty-build qualification above.

`dist/`, `.tools/`, dependency/browser caches and temporary probes are ignored local material, not contents of a fresh CI checkout. `scripts/baseline-releases.json` pins expected old archive digests. `fetch-baseline-release.mjs` accepts local approved archives or an approved HTTPS base URL. CI's `vars.BRRP_BASELINE_RELEASE_URL` still needs a real archive host. Missing or mismatched archives must fail explicitly; never generate a supposed old release from current source, fabricate a URL, silently skip, or replace it with a schema-only fixture.

Finish code/schema/shipped-doc/packager inputs, create coherent commits when permitted (default new branch prefix `codex/`), and verify a clean source tree before building. Choose the next consistent prerelease identity for new distribution artifacts—expected alpha.7 if still unused—rather than overwrite alpha.6 or label v1. Keep Go/frontend/release-helper version declarations and the version-identity test consistent; use the next schema only for an actual migration. Backup format need not change merely because schema does.

Produce outputs/evidence outside tracked build inputs. Record source commit, dirty flag, toolchain, frontend hash, executable hashes and every archive digest in a manifest; verify embedded VCS metadata. Run consumers and relevant restore/rollback on those exact extracted bytes. Add derived reports afterward, clearly identifying the implementation commit they tested. Do not keep rebuilding archives solely to include the evidence describing those same archives.

## 12. External work and remaining v1 path

At review time no Git remote, Docker runtime, PowerShell/Windows host or signing identity was available. Local code work and tests do not establish actual hosted CI, Windows/Tailscale or Docker lifecycle, physical media, encrypted off-host recovery, load/soak, independent security/accessibility review, beta or publication acceptance.

Prepare the corrected workflow and exact artifacts for external execution. With actual access, test Windows service/ACL/UAC/reboot/tray/update/rollback/recovery and Linux non-root Docker/volume/recreate/reboot/update/restore. Independently restore configuration/TLS/keys and data, measure RPO ≤24 hours and RTO ≤60 minutes through acquisition/decryption/security reconciliation, and prove alert delivery. These are master-plan targets, not achieved promises. Ask for missing external inputs only when needed; continue independent local tasks instead of waiting indefinitely.

After P9, recommend separate tranches for:

1. Pending-introduction reporting and DM activity/notifications with safe previews, preference precedence, deduplication and active/background/closed-PWA behavior.
2. Consented groups, membership/history/owner transfer and account export/lifecycle.
3. Named threads, moderation/timeouts/invitations, composer/upload/navigation and human accessibility.
4. A measured media architecture spike, then private calls/screen sharing and physical browser/network/relay qualification.
5. Public repository/security-intake/maintainer/signing ownership; both-host recovery/operations and reference load/soak.
6. Independent security/privacy and browser/accessibility reviews; newcomer install/restore; 14 consecutive beta days across at least three communities and both deployments; seven-day frozen-candidate observation; explicit approval and staged publication.

No gate G0–G8 is passed. This tranche is a private-text-report/request-discovery slice, not full WP-14/15 or v1. Keep incomplete evidence types and contact safety features visible in the roadmap rather than quietly deleting them from scope.

## 13. Working method and definition of done

Start with a concise execution outline, then implement. Parallelize CI/backend/client/recovery work only with clear file ownership and agreed API/schema contracts. Use small coherent changes, explicit error handling and comments for invariants. Avoid unrelated rewrites/dependency churn, arbitrary sleeps, blanket retries, fabricated test results or broad new infrastructure that the bounded feature does not need.

Preserve existing user changes. You may inspect/reproduce/fix local code and add maintained tests/ADRs/docs; no public publication, live production migration or feature-gate approval is implied. Ask only for genuinely missing authority/decisions, explaining the specific issue while completing unaffected work. The previous coder's “goal complete” means their reported tranche, not overall product completion.

**Stop at a coherent P9 checkpoint** with:

- R9 CI/preference defects fixed with regression evidence; recovery/account-switch controls strengthened; dated provenance/hash/count corrections.
- An explicit private-report ADR, implemented migration and server/UI permission boundaries, exact-preview disclosure and evidence-only reviewer access.
- Outstanding incoming-request discovery that updates durably across clients without draft loss or private-data leakage.
- New report/request state covered by maintained restore tests, with both existing rollback baselines preserved.
- Green relevant frontend/backend/browser/recovery checks and a final clean-source artifact/consumer record; unavailable external checks listed separately.

Final report must include P9-01–06 status, changed files and rationale, reproduced failures and permitted controls, exact commands/exit codes/counts/durations/feature modes, source/frontend/artifact identities, paths to reports, remaining blockers and the next bounded feature tranche. Distinguish inherited results from your own runs. If any required local item is incomplete, say so; do not mark the whole tranche complete because the turn or budget ended.
