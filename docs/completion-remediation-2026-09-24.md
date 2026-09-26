# Remediation ledger — completion review of 24 September 2026

> **Current checkpoint:** [0.4.0-alpha.5](releases/0.4.0-alpha.5.md), schema 10 / backup format 2. Earlier sections are dated findings and observations; current fixes and limitations are recorded in §10 below.

**Ledger date:** 24 September 2026
**Review being remediated:** [completion-review-and-next-steps-2026-09-24.md](completion-review-and-next-steps-2026-09-24.md) (findings F-01…F-16, work items N-00…N-08)
**Plan of record:** [v1-implementation-plan.md](v1-implementation-plan.md)
**Delivery ledger:** [v1-progress.md](v1-progress.md)

This is the traceability record N-00 asked for: for every finding, what was changed, what
evidence exists, which test pins it, and what is still open. Its purpose is that another
developer can identify what was reviewed, reproduce each demonstrated defect, and see the
real gate status without trusting a completion percentage.

## 0. Baseline and how to reproduce this record

| Item | Value |
| --- | --- |
| Application identity | **0.4.0-alpha.4** (a distinct candidate; see F-15) |
| Database schema | **6** as of §9 (schema 5 was current for §0–§8; the schema-6 migration is additive and is recorded in ADR 0010) |
| Backup format | **2** |
| Reviewed predecessor | `0.4.0-alpha.3`, schema 2, backup format 2 |
| Reviewed snapshot manifest | SHA-256 `54f33c40837764a0f67d4bf3f2b2eca802f457ed1f87ecb09d75d98a2f70330c` (see §2.1 of the review) |
| Repository | **Not a Git checkout.** There is no branch, no commit and no history to point at; the artifacts above are identified by content. N-00 item 1 remains open for this reason. |
| Go toolchain | go1.27.1 (the commands in this ledger were run with the repository's bundled `.tools/go/bin/go`) |
| Node | v24 |

Every result recorded here was produced on this machine, in this working copy, with
`go test` and `npm test`. Nothing in this ledger is a host, container, power-loss,
off-host or multi-machine result; those require WP-24 and are marked **not qualified**.

### How to reproduce the three demonstrated defects

The review's probes were ad hoc (`go test -run '^TestCompletionAudit'`) and are gone.
Each is now a permanent negative test, and the same command that passes today fails when
the guard is removed:

| Defect | Permanent test | How to watch it fail |
| --- | --- | --- |
| F-01 delegated manager escalates to operator authority | `go test ./internal/api -run TestDelegatedMemberManagerCannotEscalateAuthority` | drop the owner/hierarchy check from the account update path (`internal/api/admin.go`) and the test's self-promotion PATCH returns 200 instead of 403 |
| F-02 private channel metadata reaches an unrelated socket | `go test ./internal/api -run TestPrivateChannelLifecycleReachesOnlyItsAudience` | replace the audience-scoped publish in `internal/api/channels.go` with `hub.Broadcast` and the outsider's socket receives `channel.created` with the private name and topic |
| F-10 a created backup cannot be restored | `go test ./internal/backup -run TestManySmallAttachmentsCreateVerifyAndRestore` | restore the manifest ceiling to 64 KiB; creation still succeeds and both verification and restore refuse the archive it produced |

## 1. Status vocabulary

A finding is not one status. Every row below is stated in these terms, and the weakest
link decides the row:

- **implemented** — the code enforces the contract.
- **locally tested** — an automated test in this repository pins the behavior, on this
  machine, for the paths the test exercises.
- **platform-accepted** — exercised on the required real deployment (Windows service or
  Linux container) with recorded evidence.
- **approved** — reviewed by someone other than the author where the plan requires it.

**No G0–G8 gate is passed by this ledger.** The rows here are implementation and local
test evidence; §5 records what each gate still needs.

### Scope of the claims in this ledger

| Claim | Scope | Not covered |
| --- | --- | --- |
| Privacy and authorization matrices | In-process HTTP and real WebSocket/SFU connections on one machine | Second host, proxy/VPN, browser rendering |
| Revocation boundary | Queued events, in-flight downloads, sessions, roles, accounts | Power loss, network partitions, already-received bytes |
| Backup/restore agreement | Local filesystem, real archives, disposable destinations | Off-host target, RPO/RTO, encryption, real dataset size |
| Durability | Publication ordering and crash-orphan cleanup | Power loss, native Windows volume, network filesystems |
| Packaging | Static input audit and archive contents | Docker image build, extracted clean build on a second host |
| Voice | Peer eviction and refusal at the SFU boundary | Flowing audio, SDP/RTP, real microphone, relay |

## 2. Findings

### F-01 · P0 · Delegated account management can become operator authority — **fixed**

Account create/update/delete/deactivate/ban and role assignment now go through one policy
path: the caller's current authority, the target's rank and the built-in role rules are
re-checked inside the committing transaction, ownership is not editable through account
management, and the ownerless state cannot be re-entered to recreate installation
privileges.

**Local tests** (`internal/api/account_authority_test.go`):
`TestDelegatedMemberManagerCannotEscalateAuthority` (self-promotion, creating a
higher-ranked administrator, granting and removing a role above the caller, deleting an
owner; each 403 with the authority unchanged), `TestOwnershipIsNotEditableThroughAccountManagement`,
`TestOwnerlessBootstrapCannotBeReEntered`. Legitimate delegated member management is
asserted alongside, so the tests cannot be satisfied by refusing everything.

**Open:** independent review (SEC) — N-01 exit.

### F-02 · P0 · Private lifecycle metadata reaches unrelated sockets — **fixed**

Channel lifecycle events are addressed to the audience that may see them, including the
previous audience for a deletion or visibility change, and a lookup that cannot be
resolved now fails closed instead of broadcasting. The broad refresh signal carries no
restricted identifiers.

**Local tests** (`internal/api/lifecycle_audience_test.go`, real sockets):
`TestPrivateChannelLifecycleReachesOnlyItsAudience` (create/rename/move/delete, outsider
connected beforehand receives no private name, topic or identifier; permitted members
receive the correct events), `TestChannelReorderDoesNotNamePrivateChannels`,
`TestLifecycleEventWithheldWhenAudienceCannotBeResolved`.

**Open:** the one remaining global broadcast in `internal/api/channels.go`
(`publishChannelChange`) is a content-free `workspace.changed` refresh; it is reviewed as
in-scope for N-02 item 2 but is not itself restricted metadata.

### F-03 · P1 · Current authority is not consistently checked at commit — **fixed**

Every mutating account, role and channel-access path re-resolves the caller's authority
inside the transaction that commits the change, so a request that was authenticated before
a revocation cannot commit with obsolete authority.

**Local test:** `TestAuthorityRevokedDuringARequestCannotCommit`
(`internal/api/account_authority_test.go`) pauses a privileged request after
authentication, revokes the session/role/account, resumes it, and asserts the mutation did
not commit and the database is unchanged.

**Open:** the plan's stronger ask — an explicit lock/revision/transaction order rather than
another read — is documented in ADR 0001 and remains a design item for N-02.

### F-04 · P1 · Search/state visibility differs from direct authorization — **fixed**

One SQL visibility predicate now backs single-resource reads, state, unread counts,
scoped and global search, the inspector and files, and it includes `channel.read` and
implicit default-role grants.

**Local tests** (`internal/api/visibility_matrix_test.go`):
`TestReadSurfacesAgreeOnTheAccessMatrix` (the same account is allowed and denied
identically across all listed surfaces), `TestAccessGrantedThroughTheDefaultRoleIsHonouredByEveryRead`,
`TestRemovingChannelReadRemovesEveryChannel`. Pagination and counts are scoped before
limits are applied.

**Open — needs an owner decision, not code:** category names are returned to every member
and category counts count channels a member cannot read (§F-04 of the review). The current
behavior is unchanged and disclosed in the access-inspector wording; the disclosure and the
count policy await owner approval.

### F-05 · P1 · Revocation misses queued and newly admitted delivery — **fixed**

There is now one revocation boundary. Socket queues carry a generation and a stale event is
dropped instead of written, with a refetch signal queued at the new generation. Streams are
registered against the account and session that admitted them, registration happens before
the access decision so a download cannot be admitted outside the registry, and session,
password, factor, role and account changes end exactly the streams they should.

**Local tests** (`internal/api/revocation_boundary_test.go`):
`TestARevokedSessionEndsItsOwnStreamsAndKeepsTheOthers`,
`TestAChangedPasswordEndsEveryStreamThatCredentialAdmitted` (with an unrelated-account
control so silence is meaningful), `TestAnEventQueuedBeforeARefusalIsNotWritten` (control
byte backlog, revoked mid-backlog; a bystander's stream is untouched).

**Open:** the already-received-bytes limitation stands and is documented (ADR 0003); range
and preview downloads share the registered stream, which the tests cover.

### F-06 · P1 · Voice roles and admission do not enforce the advertised contract — **fixed**

`voice.connect` gates joining and resuming, `voice.speak` gates transmission and
forwarding, and moderation acts only within the caller's hierarchy. A resume re-resolves
channel access, and the media connection is ended when the access that admitted it is
withdrawn.

**Local tests** (`internal/api/voice_capability_test.go`):
`TestListenOnlyMemberCannotTransmit`, `TestMemberWithoutVoiceConnectCannotJoin`,
`TestDelegatedVoiceModeratorIsLimitedToItsHierarchy`; plus
`TestADetachedVoicePeerCannotResumeAfterRevocation` and
`TestRevocationReachesAnActiveVoiceStream` (`voice_privacy_test.go`).

**Open — deliberately not claimed:** these tests use an SFU peer with a no-op signaling
callback and a bounded read. They prove **peer eviction and refusal**, not that flowing
audio stopped. Negotiated-peer RTP evidence belongs to N-03/WP-19 and WP-24. This is the
correction F-16 required to the ledger's older wording.

### F-07 · P1 · Security policy and required audit records are ordinary settings — **fixed**

Security policy (privileged MFA, mandatory audit) is separated from routine settings and
requires current operator authority with recent authentication. A required audit record is
written in the same transaction as the authority change it describes, so the two commit or
fail together, including on the offline recovery path.

**Local tests:** `TestAuthorityChangesCarryTheirAuditRecord`, `TestPolicyReportsOwnershipState`
(`internal/api/account_authority_test.go`), plus the existing
`TestPrivilegedActionsRequireAnEnrolledSecondFactor` and the audit-list wording test in
`internal/api/privacy_test.go`/`admin` tests.

**Open:** the emergency-recovery audit behavior is documented; the "recent authentication"
window is 15 minutes and remains an owner-ratifiable default (D-10).

### F-08 · P1 · Revoked private content remains in application views — **fixed on the client model; browser journeys outstanding**

The server-side boundary is closed (F-05). On the client there is now one authorization
generation: `workspace.invalidateAuthorization()` advances it, drops cached conversations,
closes an open reply thread and refetches the snapshot, and it runs for an
`authorization.changed` event **and** for a snapshot whose revision or capability set
differs from the one on screen (so a refresh that succeeds later, or a reconnect after
missed events, invalidates the same views). Search results and pins capture the generation
before they ask for data, clear when it moves, and discard an answer that arrives after it
moved; the thread panel is closed. The generation advances even when the snapshot request
fails, so the safe outcome is an empty view rather than a stale one.

**Local tests** (`web/tests/workspace-state.test.mjs`):
`an authorization change clears cached conversations and refetches the snapshot`,
`an authorization change advances the generation as well as clearing caches`,
`a changed snapshot advances the authorization generation and a repeat does not`.

**Open:** rendered-content browser assertions — open history/search/pins/thread/preview,
revoke, and prove the browser shows nothing restricted — are N-04 item 5 and are not
written. The `/state`-fails case is covered for the store; the rendered version is not.

### F-09 · P2 · Role and category UI is unfinished — **fixed**

- **Effective capabilities.** `/state` returns `authorization.capabilities`,
  `is_owner`, `owner_designated` and `ownership_established`; the client types and stores
them (`web/src/lib/types.ts`, `workspace.svelte.ts`) and exposes `can`, `canAny` and
`canManageAnything`.
- **Every management surface reads them.** The server-tools entry, the admin tabs
  (People, Channels, Roles, Settings, Maintenance each name the capabilities that make them
  useful, with a fallback when the requested tab is not permitted), the account row menu
  (password/second-factor by `operator.credential_recovery`, roles and admin status by
  `role.manage`, ban/deactivate/delete/sign-out by `member.manage`), the account-creation
  form and its administrator checkbox, invitations, the channel reorder controls, the
  channel-creation empty state, message pinning, deletion and voice moderation. A refused
  privileged action still explains itself and links to account security.
- **Ownership has an interface.** `admin/OwnerCeremony.svelte` designates or transfers the
  owner from the People tab: it states the current owner (or that none is designated),
  requires the target username typed twice, calls `POST /owner`, explains the
  recent-authentication refusal with a link to account security, and refreshes the snapshot
  so the new authority is visible.
- **Durable category order.** Grouping and ordering come from the store's `textGroups` and
  `voiceGroups`, which order groups by the durable category position the server returns
  (unordered categories last, channels by their own position), so a reordering performed
  anywhere survives a reload.

**Local tests:** `the interface offers what the snapshot says this account may do`,
`channel groups follow the durable category order, not channel order`
(`web/tests/workspace-state.test.mjs`), plus `npm run check` type/diagnostic cleanliness.

**Open:** browser journeys for delegated non-admin managers, role changes during use, the
owner ceremony and revocation with panels open (N-04 item 5), and the
announcement-posting reconciliation the review asks for.

### F-10 · P1 · The application can create an unrestorable backup — **fixed**

Creation, verification and restoration share one set of explicit bounded limits for entry
count, manifest size, database size and aggregate size, and the manifest ceiling reflects
what a real inventory needs. A 20 GiB aggregate target is no longer the restore ceiling.

**Local tests** (`internal/backup/limits_test.go`):
`TestManySmallAttachmentsCreateVerifyAndRestore` (the 600-file reproduction, now a permanent
test), `TestCreateRefusesWhatTheReaderWouldReject` (creation refuses what verification would
reject, so the two cannot disagree), `TestVerificationAppliesTheChecksRestoreApplies`, and
`TestArchiveIsPublishedAtomically`.

**Open:** hostile-ZIP fixtures beyond the ones covered, and a target-scale dataset, which
needs storage this machine does not have.

### F-11 · P1 · Archive completion, health and retention need stronger identity — **fixed (local)**

The archive is staged, closed, hashed, validated and published atomically, and completion is
recorded only after publication, so an interruption cannot advertise a completed backup.
Health and retention use the recorded snapshot time and generation rather than filesystem
mtime, an extra copy is recorded only when its digest matches, and verification applies the
same checks restore applies, using bounded scratch storage.

**Local tests:** `TestArchiveIsPublishedAtomically`, `TestVerificationUsesBoundedScratchStorage`
(`internal/backup/limits_test.go`); `TestProductionCheckUsesTheRecordedGeneration`
(`internal/app/production_test.go`) covers a touched old archive failing freshness and a
mismatched valid copy failing identity.

**Open:** an off-host destination, an RPO/RTO measurement and the operator's encrypted-copy
and key-loss procedure (WP-24). Not qualified here.

### F-12 · P1 · Upgrade safety is incomplete — **fixed (local)**

`backup` and `verify` open the database without applying migrations, so they are a real
no-mutation snapshot and inspection path; `verify` states its semantics and refuses a
future schema, an unfinished upgrade, or a schema behind the binary; the schema recorded in
a manifest is the schema inside the snapshot, not the running binary's; and an
incomplete-upgrade state is recorded durably and blocks normal startup until it is repaired
and verified.

**Local tests:** `TestInspectionAndSnapshotNeverMigrate` and `TestAnIncompleteUpgradeCannotBeSkipped`
(`internal/db/db_test.go`), `TestAPreUpgradeSnapshotRecordsItsOwnSchema`
(`internal/backup/preupgrade_test.go`), and the older-schema-accepted case in
`internal/app/production_test.go`.

**Open:** a genuine old-executable upgrade and rollback rehearsal with a matching alpha.3
binary and a real schema-2 data folder, plus a real host. The local rehearsal is a
fresh-install recovery check and is labelled as such (§4).

### F-13 · P1 · File durability claims exceed platform evidence — **fixed as documentation and explicit behavior; not qualified**

The publication ordering was already implemented; what was wrong was the claim. The
unsupported directory-sync case is now reported (`app.ErrDirectorySyncUnsupported`) instead
of being silently collapsed into success, the durable contract and its per-platform limits
are stated in ADR 0007, and the residue of an abrupt kill is handled explicitly: abandoned
temporary publications are reaped in the upload directory by the hourly sweep and in the
disposable preview directory by `app.SweepStaleTemporaries`.

**Local tests** (`internal/app/publish_test.go`): `TestDirectorySynchronizationIsExplicit`
(anything other than success or the stated sentinel fails the test, so a swallowed
filesystem error cannot pass), `TestAbandonedTemporaryPublicationsAreReaped` (old leftover
removed; in-flight write and published file untouched); `internal/api/thumbnails_test.go`
`TestInterruptedPreviewPublicationIsReaped`; the upload-directory sweep assertion added to
`TestOrphanFileIsUnreadableUntilTheGracePeriodEnds` (`internal/api/uploads_test.go`).

**Open — requires native evidence:** power-loss behavior on a real Windows volume and a real
Linux filesystem. The Windows reasoning (NTFS journals the rename) is recorded as reasoning,
not as a measurement.

### F-14 · P1 · Current Docker build inputs are incomplete — **fixed statically; image build not run**

Notices are now generated in a dedicated `notices` stage that has both runtimes and all
three inputs (the Go module set, `web/package.json` with `web/node_modules`, and the served
`web/dist` embed target), the `.dockerignore` admits `scripts/licenses.mjs` and `LICENSE`,
the Go builder stage no longer runs Node at all, and the runtime image ships both `LICENSE`
and `THIRD_PARTY_NOTICES.txt` under `/usr/share/licenses/brrpbrrp`.

**Regression test that runs without Docker:** `scripts/check-docker-inputs.mjs` audits the
build context statically — every `COPY` source exists and survives `.dockerignore`, every
stage that generates notices has both runtimes and all inputs, and the final stage ships
the license and notices. Its `--self-test` runs the audit against a fixture with the
*pre-fix* Dockerfile and asserts it produces each of the five expected complaints, so the
check cannot silently stop detecting the defect.

**Open:** an actual Linux image build and container lifecycle run
(`docker buildx build … && node scripts/check-docker.mjs`) — **not qualified**, no Docker
Engine on this machine. A static audit is not an image build, and is not counted as one.

### F-15 · P1 before distribution · Release identity and consumer completeness — **fixed as identity and package contents; promotion gate merged in workflow**

- **One identity:** the candidate is `0.4.0-alpha.4` in `internal/app/version.go`,
  `web/package.json`, `web/package-lock.json` and the release helper's default.
  `TestVersionIdentityAgrees` (`internal/app/version_test.go`) fails when any of them drifts,
  and `TestTheCandidateIsNotTheReviewedBaseline` fails if the candidate still identifies as
  the reviewed baseline.
- **Complete source package:** `scripts/release.go` now derives the package contents from one
  function used by both the archive and a `-source-list` mode; it walks `docs/` (including
  `docs/adr/`) instead of listing documents by hand, so a new document ships by default, and
  the operator-specific notes are an explicit exclusion list. The rehearsal script the
  Makefile invokes, the supporting documents, the workflows and the security/CLA documents
  are all included.
- **Verifiable promise:** `scripts/check-source-package.mjs` reads that same list and fails
  when a listed file is missing, when a packaged document links to a file the package does
  not contain, when a Makefile-invoked script is not packaged, when a `.dockerignore` input
  is not packaged (so the image remains buildable from the extraction), or when an
  operator-specific document is included.
- **One promotion gate:** `.github/workflows/docker.yml` became a reusable workflow and
  `.github/workflows/check.yml`'s manual `packages` job now requires `test`, `browser`
  *and* `docker`, so no distribution is built when a required job has not passed.

**Open:** checksums and the accepted SBOM/provenance/signing process, cross-built versus
runtime-qualified architecture distinctions, and a clean extracted-package build on a second
host — all N-07/REL work. The old alpha.3 artifacts remain identifiable and intact.

### F-16 · P1 for gate accuracy · Evidence labels overstate coverage — **fixed**

Corrected in this remediation:

- The ledger no longer says "nothing in WP-10–WP-33 can start" before WP-09; the
  independent paths (backup/packaging repair, host preparation, media feasibility, security
  design review, documentation) are named.
- The recovery script is described as a **fresh-install recovery** check — same build, one
  machine, second directory — and its "touched archive" step is stated to corrupt archive
  bytes rather than change only an old valid archive's mtime.
- The voice revocation test is described as proving **peer eviction and refusal**, not
  interruption of flowing audio (no SDP/RTP).
- ADR 0007's guarantees are restated with per-platform limits and evidence level; ADR 0002
  records the non-migrating snapshot/verify contract and the incomplete-upgrade state.
- Every row in this ledger names its scenario, evidence level and residual limits, and
  §5 states the gate status in the same terms.

**Open:** keeping the wording honest is a standing obligation, not a one-time edit.

## 3. Work items N-00…N-08

| Item | Status | Evidence / what remains |
| --- | --- | --- |
| N-00 traceable baseline | **Partially done** | This ledger, the corrected ledger wording, the permanent negative tests for the three reproduced defects and the distinct candidate identity exist. **Open:** the owner-controlled repository and history baseline, preserved old artifacts with hashes, and tracked items — none of which this workspace can create. |
| N-01 authority and audit | **Done locally** | F-01/F-03/F-07. Independent review outstanding. |
| N-02 reads and realtime delivery | **Done locally** | F-02/F-04/F-05, with real-socket and real-SFU tests. The category-name/count disclosure awaits an owner decision. |
| N-03 voice enforcement | **Done locally, media evidence outstanding** | F-06. Negotiated-peer RTP tests and a real relay run are WP-19/WP-24. |
| N-04 capability and privacy UI | **Done except browser journeys** | F-08/F-09 are closed in the client model: capabilities from `/state`, capability-gated management surfaces, the ownership ceremony UI, durable category order, and one authorization generation covering caches, threads, search, pins and in-flight responses. **Open:** item 5's browser journeys (delegated manager, role change during use, owner transfer, revocation with panels open, failed refresh, keyboard/focus recovery), which the existing Playwright journey does not cover. |
| N-05 backup correctness | **Done locally** | F-10/F-11. Off-host restore, RPO/RTO and encryption outstanding. |
| N-06 upgrade and durability | **Done locally** | F-12/F-13. A genuine old-to-new/rollback rehearsal and native power-loss evidence outstanding. |
| N-07 packaging and gates | **Partly done** | F-14/F-15. Docker image build/runtime, clean extracted build, checksums, SBOM/provenance and signing outstanding. |
| N-08 both-host foundation acceptance | **Not started** | Needs hosts, on-host recovery evidence, an independent encrypted copy and a second reviewer. |

## 4. The local rehearsal, stated precisely

`node scripts/upgrade-recovery.mjs` (also `make rehearsal`) performs: build a candidate →
preflight an empty folder (schema 0, five pending, creates nothing) → fresh install → write
a message and an attachment through the HTTP API → back the installation up → verify the
archive → restore it into a second empty directory → serve the restored copy and compare the
account, channel, message and attachment bytes → confirm `verify-backup` accepts the archive
and refuses it after one flipped byte.

It is **not** an upgrade rehearsal: it never runs an older executable and never rolls back.
`make rollback-rehearsal` (`node scripts/upgrade-rollback.mjs`, §6) is the cross-version
rehearsal, and it is maintained as a project command rather than as audit evidence. Neither
is an off-host or cross-platform test: the second destination is another directory on the
same machine. The "touched archive" step flips archive bytes, which tests tamper detection,
not freshness. Those distinctions are the correction F-16 required, and they are also stated
in `docs/v1-progress.md` and `docs/production-readiness.md`.

## 6. Progress after the second review (P2-01 … P2-08)

`docs/completion-review-2-and-next-steps-2026-09-24.md` added R2-01 … R2-10 and the P2 plan.
This section records what has since been implemented and what was actually verified, using the
vocabulary of §1: **implemented**, **locally verified**, **platform-qualified**, **independently
reviewed** and **released** are separate claims, and only the first two are made here. No gate
has passed, and the owner decisions in §5 are still open.

| P2 item | Status | Implementation and local evidence | What remains |
| --- | --- | --- | --- |
| P2-01 current authority and credential transactions (R2-01–03) | **Locally verified** | Account mutations re-read the target inside the committing transaction (delete/update/reset/remove-factor/kick/roles), the owner guard is in the statement itself, and fresh audits commit with their change. An owner's security-policy change resolves the freshness and enrollment policy through the transaction it already holds, so it no longer waits on the connection its own request is holding. An operator reset ends deliveries, voice and streams and records the change. Regressions: `internal/api/authority_transaction_test.go` (owner-transfer-versus-delete, out-of-reach target, no self-wait, audit-and-mutation commit together) and `revocation_boundary_test.go`. | Independent review; the queued-event and stream-ordering cases in the review's §5.1, including implicit default-role revocation. |
| P2-02 client authorization invalidation (R2-05) | **Locally verified, including rendering** | One authorization generation covers the active conversation, its quoted reply, threads, pins and search, and every history/jump/refresh completion; invalidation clears the content before an uncertain refresh and advances the load version so an older response cannot repopulate it. Evidence: 68 store tests and `web/e2e/privacy.spec.mjs`, which removes a reading privilege from a signed-in account and inspects the rendered page: the message, the quoted reply and the channel entry go, a history response that was already in flight is dropped when it lands, and restoring the grant lets the same account read the conversation again by navigating. | Independent review; a rendered case for a *failed* refresh specifically (the store test covers it). |
| P2-03 live media permissions (R2-04) | **Implemented; signaling and peer state verified, media evidence outstanding** | Authority is resolved inside the voice gate for join, move and resume; withdrawal of `voice.connect` evicts an admitted peer and refuses a resume; withdrawal of `voice.speak` sets the forwarding flag and mutes server-side without asking the client to volunteer. Evidence: `internal/api/voice_capability_test.go`. | **Negotiated RTP with a non-cooperative client** on a host with audio tooling, and a real relay run. Peer state alone does not establish that forwarding stopped. |
| P2-04 delegated-role journeys (R2-06) | **Locally verified** | Invitation-only, role-assignment-only, audit-only and settings surfaces have scoped APIs, tabs and controls; `/api/admin/accounts/summary` exposes identity and role assignments only. Evidence: `internal/api/delegated_role_test.go` and three rendered journeys in `web/e2e/`: `authority.spec.mjs` (single-capability tabs, a live demotion and its reversal, audit-only Maintenance content), `privacy.spec.mjs`, `ownership.spec.mjs` (first ownership designation, refusal without an enrolled factor with an actionable path, transfer, the former owner's refusal to delete the new owner, and the new owner's operator surface). Two defects the journeys found are fixed: the administration panel used to vanish silently when the last management capability was withdrawn, and transferring ownership destroyed the ceremony's own confirmation. | A category-only journey; independent review; the ownership journey intentionally leaves the installation owned by the account it designates (a run-scoped server). |
| P2-05 unified archive validation and recovery (R2-07–08) | **Locally verified** | Creation, verification and restore share one coverage check; database-only archives record their exclusion count; per-entry, manifest and aggregate budgets apply while staging and in both readers; schedules record snapshot, completion and copy time separately from the generation and retry boundedly; health ages by snapshot time and reports schedule and generation coverage separately. Evidence: `internal/backup/limits_test.go`, `internal/api/backup_schedule*`, `internal/app/production_test.go`. | A >64 MiB database in the real container, long-copy freshness, disk-pressure and scratch-cleanup evidence on a Docker host, and the protected-generation behaviour under repeated copy failure measured on that host. |
| P2-06 maintained upgrade and rollback path (R2-09) | **Locally verified for one platform pair; maintained command** | `make rollback-rehearsal` (`node scripts/upgrade-rollback.mjs`) uses the archived `0.4.0-alpha.3` executable for this platform: a representative dataset (owner, member, three messages with a reply, a pin, an invitation, an upload and an enrolled second factor), the old executable's own archive as the rollback source, proof that the candidate's snapshot of an un-migrated installation is format 2 and refused by the old executable, the upgrade with byte-identical attachment and a working factor, a recorded post-snapshot divergence, the rollback into a clean folder with the old executable, and an injected interruption before the upgrade's postconditions. **53 checks, 0 failed**; its JSON is written with `--report FILE`. | Windows service and container lifecycles, a representative-scale dataset, a second platform pair, and measured RPO/RTO. The interruption is injected as the durable state a crash leaves, not raced against a live migration. |
| P2-07 consumer packages and Docker (R2-10) | **Partly verified** | The source package ships the platform release-script directories and the rollback rehearsal, normalizes Windows path handling, and the checker validates links for both package sets with a path-handling self-test. Evidence: `scripts/check-source-package.mjs` (309 files), `node scripts/check-docker-inputs.mjs`. | An actual extracted clean build and consumer run, a Docker image built and run on a Linux host (`scripts/check-docker.mjs`), and the hosted CI gate on a canonical source identity. |
| P2-08 integration and independent acceptance | **Not started** | — | Hosts, an off-host restore, a second reviewer for the authorization and delivery ordering, and the named owners from §5. Restricted channels stay disabled until their acceptance requirements pass. |

### Verification performed with this section

Go: `gofmt -l` clean; `go build ./...`; `go vet ./...`; `go test -race -count=1 ./...` all packages ok
(`internal/api` 499 s). Frontend: `npm run check` 0 errors and 0 warnings; `npm run format:check` clean;
`npm test` 68 pass / 0 fail; `npm run build`. Browser: `npm run test:e2e` 4 journeys pass (release,
authority, privacy, ownership) in the declared project order. Scripts: `node scripts/licenses.mjs --check`,
`node scripts/check-docker-inputs.mjs` and `--self-test`, `node scripts/check-source-package.mjs` and
`--self-test`, `node scripts/upgrade-recovery.mjs` (23 checks), `node scripts/upgrade-rollback.mjs`
(53 checks). Nothing in this list establishes a Windows host, a Docker host, off-host recovery,
audible media or an independent review.

## 5. Gate status and remaining decisions

No gate has passed. In the review's terms:

| Gate | What it needs that this machine cannot provide |
| --- | --- |
| G0 source/traceability | Owner-controlled repository, preserved artifacts with hashes, tracked items |
| G1 foundation acceptance | Both hosts, an off-host restore, an independent privacy review (WP-28), the owner ratifications below |
| G1/G5 upgrade and recovery | A matching prior executable for each host, a real schema-2 data folder in a Windows service and a Docker container, and an interruption raced against a live migration rather than injected as its durable state |
| G5 packaging/release | Docker image build and runtime, clean extracted build, checksums, SBOM/provenance/signing |
| G6–G8 beta/stable | Hosts, pilot communities, independent security approval, staged rollout |

### Decisions and inputs still needed

1. **Ratify three owner defaults:** `BRRP_SQLITE_SYNCHRONOUS=FULL` (D-16),
   `BRRP_ENABLE_RESTRICTED_CHANNELS=false` (D-06/WP-08), `RequirePrivilegedMFA=true` (D-10).
2. **Approve the manager-visibility disclosure**, and decide whether category names are
   shared workspace structure and whether category counts may include inaccessible channels.
3. **ADR 0006 confirmations:** copyright holder spelling, manual CLA signing versus a
   signing service, `SECURITY.md` response targets, and one review of the agreement text.
4. **Named roles and hosts:** TL, BE, FE, OPS, REL, QA, SEC, OWNER; a Windows host and a
   Docker host; an alert destination; a second reviewer for authorization behavior.
5. **Deployment targets** are already chosen: Windows/Tailscale service and Linux Docker.
6. **Sign-in throttling (proposed D-17, added with §9.6):** the shipped budget is 20 successful-or-failed
   attempts per address per five minutes, shared by every account behind one NAT, VPN or non-forwarding
   reverse proxy. Decide whether that default should rise for a community server and whether the throttle
   should also key on the account rather than counting successful sign-ins against one address budget.
   The setting `BRRP_LOGIN_ATTEMPTS` exists either way; only its default and the keying are open.

Nothing in this ledger authorizes exposing restricted channels, publishing a release,
or claiming v1 readiness.

## 7. Progress after the third review (P3-01 … P3-04)

`planning/completion-review-3-and-next-steps-2026-09-25.md` added R3-01 … R3-06 and the P3
plan. This section records what has since been implemented and what was actually verified, using
the vocabulary of §1; §6 stays as the record of the P2 tranche. Two of §6's "what remains" items
are closed here (the message/unread half of P2-02 and the client half of P2-03's media evidence),
and its gate table above is superseded by the review's gate map: **G3 is community feature
completion and G4 is media feature completion**, so upgrade/recovery evidence belongs to G1/G5.

| P3 item | Status | Implementation and local evidence | What remains |
| --- | --- | --- | --- |
| P3-01 finish the client authorization boundary (R3-01) | **Locally verified, including rendering** | One authorization generation now covers every completion that can rewrite protected state: the snapshot, its error path, incremental single-message lookups, unread counts, read acknowledgements, the first-unread jump, the channel move and the restored reply. A completion is admitted only under the session *and* generation it started in (`admitted`), the per-message revision map is cleared with the cached conversations, and a stale failure can no longer overwrite the state the clearing path owns. Failed-refresh metadata has an explicit contract: while a snapshot under the new authority has not been applied, unread counts and mention badges are withheld (`unreadFor`) and the structure is kept for navigation and recovery. Evidence: five new model regressions in `web/tests/workspace-state.test.mjs` (late incremental lookup, late unread list, stale snapshot restoring the old capabilities, stale failure, withheld counts) — each fails when its guard is removed — and the rendered privacy journey still passes. | Rendered failed-refresh coverage for search/pins/thread/preview left open (see P3-04 below); a parent-refresh completion is guarded by the same rule but has no dedicated regression of its own. |
| P3-02 effective-role revocation and delivery ordering (R3-02, R3-05) | **Locally verified** | Role changes act on the *effective* holders of the role, computed by the same rule the policy engine resolves (`EffectiveRoleHolders` adds every account for the default role), and only the streams the new authority no longer admits are ended, decided per registered response outside the registry lock. Restricted delivery now carries the authorization revision the audience was resolved at: the hub refuses an event whose revision is older than the revision the connection has observed, and an invalidated connection records the committed revision. A streamed download re-establishes the account's authority and the session that made the request at admission, so a revocation between the middleware's decision and the registration is observed. Evidence: `internal/api/effective_role_revocation_test.go` (an implicit default-role holder's held file stops and a new request is refused while an account that keeps `channel.read` through its own role finishes, plus the event control), `TestAnAudienceResolvedBeforeARevocationIsRefusedAtDelivery` and `TestADownloadRegisteredBeforeASessionRevocationIsRefusedAtAdmission` in `revocation_boundary_test.go`, and `internal/ws/hub_test.go`. Each fails without its fix; the implicit-holder case delivers the whole file with the old code. | Independent review of the ordering contract; the contract itself is recorded in `docs/v1-implementation-plan.md` (T-AUTH-02) rather than as an ADR. Voice and push remain the queue paths with their own regressions. |
| P3-03 backup failure state and recovery-point semantics (R3-03) | **Locally verified** | The recorded status separates the last attempt, the newest verified local generation, and the newest generation that is *also* copied to independent storage; a failed copy counts in its own bounded retry and no longer resets the local attempt count, the previous independently recoverable identity survives, retention protects that generation's archive, and the next attempt repairs the outstanding copy instead of writing another archive. The archive's declared recovery point is taken at the database snapshot boundary, before `VACUUM INTO` and before compression, so a slow snapshot cannot declare itself fresher than it is; `production-check` names the last independently recoverable generation when the newest one has no copy. Evidence: `internal/api/backup_schedule_test.go` (failure, protection, retry, reload and repair) and `internal/backup/snapshot_time_test.go`, both of which fail against the previous behavior. | A >64 MiB scratch run in Docker, real disk-pressure and copy-destination faults, and an operator alert destination. |
| P3-04 repair and strengthen browser/media acceptance (R3-04) | **Locally verified; checked-in suite green** | The voice journey follows the product's real transitions — joining closes the room dialog, so the joined state is read from the sidebar control and the dialog is reopened for participant assertions — restores the shared default role and closes its browser in a `finally`, and makes the sender non-cooperative: capture is forced back on and the sender's `outbound-rtp` bytes are asserted to keep growing while the listener's `inbound-rtp` stops, which is what makes the denial server-side. Project dependencies are flat so one journey's harness failure cannot suppress another's coverage; the ownership journey now awaits the page sign-out leaves behind instead of racing the logout request. Evidence: `npm run test:e2e` — **5 journeys pass, 0 failed** (release, authority, privacy, voice, ownership) in the declared order, with the voice journey's withdrawal/regrant and the non-cooperative sender control. | Role-only and category-only rendered journeys and rendered failed-refresh coverage with search/pins/thread/preview open; real microphones, a forced relay, physical devices and multiple engines (P3-05). Synthetic Chromium/loopback transport is not audible quality. |
| P3-05 qualify the supported deployment artifacts | **Not started** | Nothing implemented; the maintained local paths (`make rehearsal`, `make rollback-rehearsal`, the browser suite, the static package checks) still run and were re-run for this section. | Windows/Tailscale and Linux Docker hosts, immutable candidate hashes, an independent off-host restore, capacity/disk-pressure acceptance, signing/provenance and a private security intake. |
| P3-06 consolidate evidence and propose foundation acceptance | **Partly done** | This section and `docs/v1-progress.md` were brought in line with the actual results: the frontend test count, the browser outcome, the rollback rehearsal, the corrected gate names, and the separation of implementation, local verification, platform qualification, independent review and release. | The independent foundation review (WP-28) still has to be performed by someone other than the implementing agent, and the owner ratifications in §5 are still open. |

### Verification performed with this section

Go: `gofmt -l` clean; `go build ./...`; `go vet ./...`; `go test -race -count=1 ./...` all packages ok
(`internal/api` 442.9 s). Frontend: `npm run check` 0 errors and 0 warnings; `npm run format:check`
clean; `npm test` **73 pass / 0 fail**; `npm run build`. Browser: `npm run test:e2e` — **5 journeys
pass, 0 failed**, including the negotiated-transport voice journey with a non-cooperative sender.
Scripts: `node scripts/licenses.mjs --check` (39 components, 20 texts),
`node scripts/check-docker-inputs.mjs` and `--self-test`,
`node scripts/check-source-package.mjs` (313 files, 34 native documents),
`node scripts/upgrade-recovery.mjs` (23 checks), `node scripts/upgrade-rollback.mjs` (53 checks).
The regression evidence for each item is the test that fails when its fix is removed, not only the
test that passes with it. Nothing in this list establishes a Windows host, a Docker host, off-host
recovery, audible media, target-scale capacity or an independent review, and no gate has passed.

## 8. Progress after the fourth and fifth reviews (P4-01…P4-06, R5-01…R5-04)

`planning/completion-review-4-and-next-steps-2026-09-25.md` and
`planning/completion-review-5-and-next-steps-2026-09-25.md` added the R4 and R5 findings and the
P4/P5 plans. This section records what has since been implemented and what was actually verified,
in the vocabulary of §1; §6 and §7 remain the P2 and P3 records. No gate has passed. Restricted
channels stay disabled and §5's owner decisions are still open.

### 8.1 P4 tranche (review 4)

| P4 item | Status | Implementation and local evidence | What remains |
| --- | --- | --- | --- |
| P4-01 coherent authorization snapshot and captured completion identity (R4-01, R4-02) | **Locally verified, including rendering** | `channelAudience` resolves the channel, its grants, effective roles, ownership and the committed revision in one read transaction (`coherentAudience`), and the delivery paths re-check the committed revision and carry the resolved revision with the event. `RequestIdentity` captures account, session and authorization generation before each await, and `moveChannel` no longer defaults to the current session after one. Evidence: `internal/api/coherent_audience_test.go`, `internal/ws/hub_test.go`, four store regressions in `web/tests/workspace-state.test.mjs`, and `web/e2e/account-switch.spec.mjs` (a held channel move across an in-place sign-in). The committed-write admission half review 5 named as still open is R5-01 below. | Independent review of the ordering contract (T-AUTH-02 in the plan, not yet an ADR). |
| P4-02 published copy with lost status; independent snapshot and copy cadences (R4-03, R4-04) | **Locally verified** | Copy completion is reconciling: a regular final file whose digest is the verified archive is adopted instead of rejected, and conflicting, non-regular or unverifiable targets are refused and preserved. Snapshot and copy-repair run on separate clocks and counters, so a failing destination cannot starve fresh local snapshots. Evidence: `TestAPublishedCopyIsReconciledWhenItsCompletionWasLost`, `TestAnExistingCopyIsAdoptedOnlyWhenItIsTheVerifiedGeneration`, `TestACopyOutageDoesNotStarveFreshLocalSnapshots` and `TestTheSnapshotCadenceSurvivesACopyOutageAndRecovers`. The durability failure handling is R5-03 and the missing-source repair semantics are R5-04 below. | Real storage faults on a host; a Docker scratch run larger than the test fixture. |
| P4-05 both-host qualification | **Not started** | No new Windows, Docker or off-host evidence. Static checks and local macOS rehearsals do not qualify either selected host. | The host matrix in §5 and P5-05. |
| P4-06 evidence/next product package | **Done for this and the §8.2 section** | These ledgers were brought to the current results, and the WP-09 design note is recorded with the implementation plan. | The independent review and the owner inputs. |

### 8.2 P5 tranche (review 5)

Each row names the permanent regression, the behavior that makes it fail before the fix, and the
permitted control that keeps the change from turning into a deny-all rule. The pre-fix failures
were observed by temporarily restoring the old behavior and re-running the test.

| R5 finding | Status | Implementation | Regression and pre-fix failure | Permitted control |
| --- | --- | --- | --- | --- |
| R5-01 committed write admission despite delayed invalidation (socket delivery) | **Locally verified** | The write-admission decision runs under an authorization fence (`Server.authority`), and every authorization-changing transaction commits and publishes its invalidation as one step under that fence (`commitAuthorityChange`); the slow follow-up work (stream revocation, voice enforcement) runs after the fence is released. Admission is therefore mutually exclusive with the commit of the authority it depends on: a writer either decides before the change commits or observes the invalidation and refuses. No database lock is held across a network write; the fence is released before the socket write. | `TestARevocationCommittingDuringAPendingWriteIsRefused` pauses the real socket writer immediately before its admission decision, commits a revocation through the API while the existing voice gate delays enforcement, and releases the writer: the queued private payload must not arrive. With the invalidation delayed again (simulating the pre-fix ordering) the test fails with *the revoked member's socket received the payload queued before the revocation*. | The manager, which keeps access, receives the event that was queued for it before the change; and a regrant delivers a new event on the same connection without a reconnect. |
| R5-02 public voice presence bypasses `channel.read` | **Locally verified** | `publishVoiceChannel` no longer broadcasts public channels to every socket. Public and restricted presence both resolve through the same coherent, capability-aware audience and the same revision-checked delivery, so an account whose role lost `channel.read` learns nothing about a room its own `/state` hides. | `TestPublicVoicePresenceStillRequiresChannelRead` narrows the default role, joins a permitted account through the real SFU, and asserts the denied socket receives no `voice.state_updated`. Against the public broadcast it receives the room id, the participant id and the flags. | A still-authorized account (the manager) receives the participant list. |
| R5-03 failed status persistence does not stop retention | **Locally verified** | `saveBackupStatus` returns its error. Copy completion and the local-only path record the recovery identity before any pruning, retention is skipped when that record did not land, the pre-attempt durable generation is protected as well as the new one, and the job returns the persistence failure instead of a durable-completion claim. | `TestAFailedStatusWriteBlocksRetentionOfTheDurableRecoveryPoint` rejects the status write with a database trigger, runs a second backup at retention one, and requires a reported failure with the durable generation's local archive and copy both still present. With the error swallowed the test fails with *a failed status persistence was reported as a successful durable completion*. | The existing matching-copy reconciliation, adoption-refusal, retention and cadence tests all still pass, so a correct copy is still adopted when status storage recovers. |
| R5-04 missing copy source clears the error without repairing | **Locally verified** | The copy-only path distinguishes a missing source (a failed repair that accelerates a replacement snapshot on the short clock), an inaccessible or non-regular source (an operator problem), and a genuine no-op (no pending copy). A failed repair keeps the copy error, the attempt counter and the outstanding-copy state instead of clearing them. | `TestAMissingCopySourceIsNotReportedAsARepairedCopy` and `TestANonRegularCopySourceIsStillAFailedRepair` require a non-empty `CopyError`, a raised `CopyAttempts`, a still-outstanding copy and, for the missing case, `SourceMissing` with a due replacement snapshot. With the old no-op branch the missing-source test fails with *a missing source was silently treated as a successful copy repair*. | A genuine successful retry still converges: the replacement snapshot clears the missing-source state and records its independent copy. |
| P5-05 host qualification and evidence consolidation | **Ledgers done; hosts not started** | These two ledgers were updated to the current results. | Windows/Tailscale and Linux Docker lifecycles, an off-host encrypted restore, an external alert receipt, hosted CI and an independent review all remain external dependencies. |
| P5-06 WP-09 shared conversation design | **Design note recorded; no private features built** | The shared conversation, migration, idempotency-receipt, outbox and read-cursor design is recorded in the implementation plan and the progress ledger, before any DM/group code. | Product-owner decisions on membership/consent semantics, then a bounded vertical slice with old-data migration. |

### Verification performed with this section

Go: `gofmt -l` clean; `go build ./...`; `go vet ./...`; `go test -count=1 ./...` all packages ok;
`go test -race -count=1 ./internal/api ./internal/ws ./internal/db ./internal/authz ./internal/app ./internal/backup`
all packages ok with **no data race** (`internal/api` 511 s). The review-4 `coherent_audience_test.go`
race — the audience-interleave and stream-admission hooks were ordinary fields read from handler
goroutines — is fixed by making both hooks atomic; the previously racy test passes under `-race`.
Frontend: `npm run check` 0 errors and 0 warnings; `npm run format:check` clean; `npm test`
**77 pass / 0 fail**; `npm run build` (build id `b42b3fcb453f5078`). Browser: `npm run test:e2e` —
**6 journeys pass, 0 failed** (release, authority, privacy, voice, account-switch, ownership). The
voice capture stall review 5 recorded as an environment blocker no longer reproduces on this host
with the harness's synthetic capture flags, and the journey still asserts real `outbound-rtp` growth
and inbound-stop with a non-cooperative sender; a synthetic Chromium/loopback transport is still not
audible-quality or multi-engine qualification. Scripts: `node scripts/licenses.mjs --check`
(39 components, 20 texts), `node scripts/check-docker-inputs.mjs` and `--self-test` (24 COPY sources),
`node scripts/check-source-package.mjs` (315 files, 34 native documents), and
`node scripts/upgrade-rollback.mjs` (**53 checks, 0 failed**). `make rehearsal` was not re-run for this
section and remains the P3 result. Nothing in this list establishes a Windows host, a Docker host,
off-host recovery, audible media, target-scale capacity or an independent review, and no gate has
passed.

## 9. Progress after the sixth and seventh reviews (R6-01…R6-04, P6-01…P6-06)

`planning/completion-review-6-and-next-steps-2026-09-25.md` and
`planning/completion-review-7-and-next-steps-2026-09-25.md` carried the R6 findings forward; review 7
confirmed the implementation was unchanged and reproduced all three code failures again. This section
records what was implemented against them, what is now locally verified, and what remains external.
No gate has passed. Restricted channels stay disabled, and §5's owner decisions are still open.

### 9.1 R6-01 — the database/authorization lock-order cycle

**Status: locally verified.** The cycle is removed by one documented acquisition order rather than by a
retry or a shorter critical section: **the store's connection first, the authority fence second, and
nothing waits for the connection while holding the fence** ([ADR 0009](adr/0009-authority-commit-and-admission-ordering.md)).
A committing change reads its revision through the transaction it already holds and only then takes the
fence (`commitAuthorityChange`); a standalone invalidation reads the committed revision *before* taking
the fence (`authorityRevisionBeforeFence`) and the refetch signal is queued at the revision the
connection actually ends up on, so it can never be refused by the admission rule it exists to satisfy.
The slow follow-ups (stream revocation, voice enforcement, socket disconnection) still run after the
fence is released, and no network write ever happens under it.

**Regression:** `TestAStandaloneInvalidationNeverWaitsForTheDatabaseUnderTheFence` holds a transaction
(and therefore the single connection), starts a standalone invalidation, and requires the fence to be
free where the invalidation is about to read the revision, the concurrent commit to complete without
cancellation, and the invalidation to finish afterwards. Observed against the pre-fix ordering
(revision read under the fence): *"a standalone invalidation holds the authority fence while waiting
for the store's connection"*.

### 9.2 R6-02 — credential/session commits bypassing admission, including registration

**Status: locally verified, with the transition inventory recorded.** Every authority, credential and
session transition commits through the same protocol, with the invalidation audience stated per route
(ADR 0009's inventory): the account's connections for a reset, factor removal, kick, ban, deactivation,
account deletion, password change, factor enrollment/removal or sign-out-everywhere; one session's
connections for sign-out and revoke-one, so an account's other devices are not disturbed. Socket
registration is now an admission: `registerConnection` re-establishes the account and its session from
the store inside a read transaction, takes the fence, commits that transaction and inserts the
connection with the revision it was admitted under, so a change that commits before the check refuses
the connection and one that commits after it invalidates it.

**Regressions:** `TestACredentialCommitRefusesAQueuedWriteOnTheRevokedSession` (real socket, real
operator reset, delivery registry held so the post-commit cleanup cannot run, writer paused immediately
before admission) and `TestASocketRegisteredAfterItsSessionEndedIsRefused` (the credential change
commits between the route guard and registration). Against the pre-fix ordering they fail with *"a
revoked session was handed a newly admitted private payload"* and *"a connection whose session had
ended was admitted and received hello"*.

**Permitted controls in the same tests:** another member of the restricted channel receives its event
during the window, the reset still answers success, the replacement credential signs in and reads the
channel, and a session that did not move registers normally and receives the ordinary `hello` frame.
The existing channel, role, session-stream, session-socket and voice-presence regressions all still
pass, so the change did not become a deny-all rule.

### 9.3 R6-03 — combined copy and status failure pruning a durable local generation

**Status: locally verified.** Retention is now gated on an explicit successful persistence result rather
than on the absence of one error sentinel. The fresh path persists its verified local generation
*before* anything destructive happens and independently of the copy; the copy attempt and retention run
only when that write landed, and retention protects four identities: the archive this attempt produced,
the archive the pre-attempt durable record named, and the local archives of the current and previous
independently recoverable generations. A final status write that fails on the way out can therefore no
longer leave the ledger naming an archive retention has already deleted.

**Regressions:** `TestACombinedCopyAndStatusFailureKeepsTheDurableLocalGeneration` (the review-6 probe:
retention one, unavailable destination, injected rejection of every status update, then a restart
through a new store and server and a converging recovery), `TestAStatusInsertFailureBlocksRetentionOfBothIdentities`
(a lost status row, a rejected INSERT and an unavailable destination, with the recoverable generation's
local archive *and* independent copy both checked by digest) and
`TestAFinalStatusWriteFailureLeavesTheRecoveryIdentitiesIntact` (the first record write lands and the
final update is rejected by a marker-table trigger). Observed against the pre-fix ordering:
*"copy failure path pruned the local generation still named by durable status before status persistence
succeeded"*.

**Evidence detail:** every assertion is by SHA-256 of the archive the record names, not by file name,
and the combined case is re-checked after reopening the installation as a new `db.Store` and `api.Server`.
Existing copy-reconciliation, adoption-refusal, cadence, missing-source and non-regular-source tests all
still pass.

### 9.4 R6-04 and the bounded WP-09 slice

**Status: D-11 recorded as [ADR 0010](adr/0010-conversation-model-receipts-outbox-and-cursors.md); the
server half of the first slice is implemented and locally verified; the client outbox and read-cursor
reconciliation remain.** The ADR resolves each contradiction review 6 listed: expiry is server-enforced
and distinguishes committed-and-deleted from expired; the operation identity stays `(account, key)` with
the destination inside the intent digest, so a key reused for another conversation conflicts; the
outbox keeps automatic-retry cancellation, server-content removal and recovery of user-authored unsent
text apart, opt-in and account-scoped; and the migration stages are stated exactly, including why the
`NOT NULL`/foreign-key tightening is a later rowid-preserving rebuild with foreign keys disabled outside
the transaction, and the D-08 lifecycle prerequisite.

**Implemented (schema 6 / M-C):** `conversations` (one per channel, id = channel id, created by the
migration and by a channel trigger so no request repairs the mapping), `conversation_members`,
`messages.conversation_id` (nullable, backfilled, resolved through `COALESCE(conversation_id,
channel_id)`), deletion-safe `message_operations` receipts written in the same transaction as the message
they acknowledge, and the `AFTER DELETE` trigger that marks a receipt's outcome in the same transaction
as the erasure. The send path claims the operation before creating the message, answers every replay
from the receipt, and never recreates deleted or purged content.

**Regressions:** `internal/api/message_receipts_test.go` (`TestASendReceiptIsIdempotentAndNeverRecreatesDeletedContent`,
`TestAnOperationKeyIsBoundToItsIntent`, `TestAnExpiredReceiptIsNeverNewWork`, `TestConcurrentRetriesOfOneOperationCreateOneMessage`,
`TestALegacyBackfilledReceiptNeverRecreatesContent`, `TestAChannelConversationIsCreatedAndReadable`) and
the extended `TestUpgradeFromAlphaPreservesWorkspace`, which now also proves that the alpha fixture
upgrades with one conversation per channel, every message scoped and every message receipted while ids,
rowids, replies, pins, reactions, attachments and FTS results stay unchanged.

**The client half of the same slice is now implemented too.** `web/src/lib/outbox.ts` is the
opt-in, account-scoped device scope for sends whose acknowledgement never arrived: at most 50
entries, 64 KiB of text each, 24 hours, bounded again on read and on write, with identifiers held
to their shape so a corrupt or foreign scope is dropped rather than submitted. The workspace
restores each entry once the snapshot says the account may still write to that destination,
reconciles it through the operation key it was submitted under (so the server's receipt answers
for it: the committed message, or the outcome that ends it), releases the entry on the answer or
on an explicit discard, and — when an outcome the server would repeat ends the operation —
keeps the author's own text in the composer with the server's own explanation instead of
retrying a doomed send or discarding it. Sign-out clears the scope; a destination the snapshot no
longer offers is not a retry. Read cursors advance only where the server confirmed them, an older
window sends nothing, and a refused mark is offered again.

**Regressions:** `web/tests/outbox.test.mjs` (account scoping and clearing; the entry, text and
age bounds; unusable shapes and corrupt or blocked storage; the attachment-only entry and the
outcome classification) and three tests in `web/tests/workspace-state.test.mjs` (a held send is
restored and reconciled under its own key with its attachment claim, and the acknowledgement
clears it; a refusal the server would repeat releases the entry and keeps the text in the
composer; a cursor only advances once the server confirmed it, an older window sends nothing and
a refused mark is offered again). Each was observed failing against the pre-fix behaviour:
dropping the monotonic guard fails the cursor test, treating every outcome as retryable fails the
refusal test, and keying the scope by a constant fails the scoping test. The rendered journey
`an unconfirmed send survives a reload and is reconciled by its own key` fails when the restore
is removed.

**Not claimed:** no direct or group conversation can be created, no route reads `conversation_members`,
and no private icon, DM or call appears anywhere. D-08 still gates private conversations.

### 9.5 P6-04/P6-05 evidence and remaining dependencies

| Item | Status | Evidence / dependency |
| --- | --- | --- |
| Rendered privacy/management coverage | **Covered for the listed surfaces** | `web/e2e/privacy.spec.mjs` gained *held panel answers cannot repopulate pins, a thread, search or a preview*. It arranges a rendered image preview, a pin and a threaded reply as the account itself sends them, opens each surface, holds the answer **after** the server admitted it (the request is made first, only its delivery is delayed, so the answer really carries restricted content), then takes `channel.read` away while the refresh that would confirm the change fails. It asserts, for pins, the open thread, search and the inline preview, that nothing renders at the moment of the revocation and that nothing renders when the held answer lands, and that the same account recovers by navigating. Three pre-fix mutations were observed failing this journey: removing `PinsPanel`'s answer guard (the pin reappears), removing `SearchPanel`'s authorization dependency (results survive), and removing `clearActiveConversation`'s thread clearing (the thread stays open). Remaining: category-only/role-only management is covered by `authority.spec.mjs` (single-capability managers); the failed-refresh path is now covered for the four surfaces above and not for the voice and admin panels. |
| Both-host qualification | **Access dependency, unchanged** | No Windows/Tailscale host, no Docker Engine on this machine (static Docker-input checks only), no off-host restore destination, no external alert receipt, no power-loss test, no hosted CI or signing identity, no independent reviewer. These are recorded as external dependencies with the owner, not as code work. |
| Gates | **No gate has passed** | G1 still needs R6-01/02's independent review (the rendered evidence it was waiting on is now in place); G2 needs the rest of WP-09–12, the client outbox and D-08; G5 needs real hosts and an off-host restore. |

### 9.6 The address-wide sign-in budget

**Found while completing the rendered coverage, and fixed as an operator setting rather than a test
hack.** The sign-in throttle is keyed on the client address alone: every account behind one office NAT,
VPN or reverse proxy that does not forward the client address shares a budget of 20 attempts per five
minutes, and a *successful* sign-in spends it. The journey suite hits this: every journey signs in from
`127.0.0.1`, so with this tranche's extra journey the ownership journey's two-factor verification was
answered 429 ("Too many sign-in attempts") while five of seven tests passed — the journey failed on the
limiter rather than on the behaviour it measures.

**What changed:** `BRRP_LOGIN_ATTEMPTS` (default `20`, range 1–100000) now supplies the ceiling, wired
through `app.Config.LoginAttempts` into `limitLogin`, with a zero value meaning the default rather than
"no limit". Documented in `docs/deployment.md` as the setting for installations where everyone signs in
from one address. The e2e harness sets it high for its own run (`web/e2e/run.mjs`), so a journey measures
its own subject; the shipped default is unchanged. `TestTheSignInBudgetIsTheConfiguredOne` proves the
configured value is the one enforced (three attempts with `BRRP_LOGIN_ATTEMPTS=3`, the fourth answered 429
with `Retry-After`) and that a zero value falls back to the default; the test fails against the pre-fix
hardcoded 20 with *sign-in over the budget: 200*.

**Open owner decision (proposed D-17):** whether the *shipped* default should rise for a self-hosted
community server, and whether the throttle should also key on the account (a failure budget per account
plus a wider address-wide abuse ceiling) instead of counting successful sign-ins against a shared budget.
Neither is changed here: this tranche only makes the ceiling an operator setting.

### Verification performed with this section

Go: `gofmt -l internal cmd scripts` clean; `go build ./...`; `go vet ./...` clean;
`go test -count=1 ./...` all packages ok (uncached; `internal/api` 53.8 s on the final run);
`go test -race -count=1 ./internal/api ./internal/ws ./internal/db ./internal/authz ./internal/app ./internal/backup`
all packages ok with **no data race** (`internal/api` 589.75 s), and after the sign-in-budget change the
same race detector was re-run on `internal/ws ./internal/db ./internal/authz ./internal/app ./internal/backup`
(no race) and over the `internal/api` auth/session/factor/receipt/ordering regressions (89.6 s, no race),
which is where that change lives; a full `internal/api` race run was not repeated for it. The three R6 regressions were observed
failing against the pre-fix ordering before the fix was restored, and the R6-03 regressions were observed
failing against the pre-fix retention order; both observations are recorded in §9.1–§9.3.
Frontend: `npm test` **84 pass / 0 fail** (the 77 of the previous tranche plus four outbox tests and
three client-state tests); `npm run check` 0 errors and 0 warnings; `npm run format:check` clean;
`npm run build`. Browser: `npm run test:e2e` — **eight journey cases across the six projects**
(release, authority, privacy ×3, voice, account-switch, ownership). The three privacy cases pass
(15.0 s, 29.0 s and the reconciled-send case), as do release, authority, account-switch and ownership. The voice journey failed in this host's
runs at *microphone acquisition* ("Waiting for microphone…", 31.9 s), which is the capture-environment
stall recorded in review 5: it reproduces in isolation with only `release` and `voice` selected, twice in
a row, so it is not caused by the journeys added here; it had passed 15.6 s earlier in the same session
with the same code. No RTP assertion was reached, so this run does **not** renew the transport evidence.
Scripts and rehearsals:
`node scripts/licenses.mjs --check` (39 components, 20 texts), `node scripts/check-docker-inputs.mjs`
and `--self-test` (24 COPY sources, static audit only), `node scripts/check-source-package.mjs`
(323 files, 36 native documents), `make rehearsal` (**23 checks, 0 failed**, schema 6) and
`make rollback-rehearsal` (**53 checks, 0 failed**, real archived `0.4.0-alpha.3` schema 2/format 1 →
candidate schema 6/format 2 → old-binary restore). Nothing in this list establishes a Windows host, a
Docker host, off-host recovery, audible media, target-scale capacity, hosted CI, signing/provenance or
an independent review, and no gate has passed.

## 10. Alpha.5 implementation checkpoint — 25 September 2026

This implementation tranche follows the later source audit rather than treating review 7 as the latest source state. Its authoritative verification and release limits are in the [alpha.5 record](releases/0.4.0-alpha.5.md).

### Reliability and lifecycle

- Compacted deleted receipts retain a spent account/key guard; the original payload cannot recreate content after 30 days. Legacy empty-digest replays must match the destination. Attachment IDs are canonically sorted for intent comparison. Maintenance strips obsolete destination/digest/times in batches; total lifetime tombstone growth is explicitly recorded.
- Failed-send recovery is a separate Copy/Dismiss surface. Permanent refusal stops retries with persistence on or off, expiry becomes text-only recovery, account scopes are isolated, and complete intent is validated. Count and UTF-8 byte budgets are shared by pending and recovered entries. Cursor acknowledgements cannot regress.
- Password, factor, reauthentication and device-management mutations reauthorize their session in the write transaction. Delivery admission checks the validity of connected sessions at credential commits, closing the late new-event interval.
- Copy-only recovery repair now requires a successful status write before retention, and unknown snapshot dates do not borrow later completion/copy times. Reopen tests prove that decisions survive store restart.
- Schema 7 introduces immutable removed identities with retained community history and transactional credential/personal-state cleanup. Explicit erasure remains separate; selected report evidence and backups are disclosed. Socket disconnect cannot restore removed activity data. Push registration/deletion also rechecks current session authority transactionally.

### Community features

Schema 8 adds durable slow-mode intervals with current-authority manager bypass and safe receipt replay. Schema 9 adds selected-message reports, visible-channel moderation, active deduplication, 10/hour submission limits, bounded 50-item pages, transaction-time review and audit, 90-day availability and hourly bounded purge. Schema 10 adds account-specific channel notification preferences, coherent mention audiences, dispatch-time mute checks and corresponding UI. Mention/history/unread records remain intact.

The [community guide](community-guide.md) documents the member flows. [ADR 0011](adr/0011-account-removal-and-retained-evidence.md) records the community lifecycle policy and leaves private-history ratification explicit. The master plan's obsolete WP-09 draft was replaced with the current contract; this corrects the earlier nullable-column, operation-key and pruning contradictions instead of appending a second conflicting statement.

### Final integration fixes

The deterministic browser media fixture uses a real WebAudio audio track through real browser/SFU connections. It exposed a persistent-muted state after speaking permission was restored: the SFU had overwritten requested self-mute with the denial. The SFU now separates those states, the API preserves raw client intent under the voice gate, and RTP resumes on regrant without another client update while deliberate/moderator restrictions remain. Browser capture still needs actual device qualification; stalled browser capture now gives a 30-second timeout and retry, with late-track cleanup.

A read-cursor request paused across sign-out could also recreate personal read state. It now resolves session, channel-read permission, update and delivery revision in one transaction. Its regression was observed returning 200 and recreating a row before the fix, then refuses the ended session. These last deltas have focused race coverage in addition to the whole-package integration run.

### Acceptance boundary

Focused regressions include pre-fix failures for revoked self-service, late socket delivery, receipt resurrection, push mutation after revocation and restored removed-account activity metadata. The release record reports final integrated suites and packaging. This tranche does not claim DMs, groups, named threads, screen sharing, real-host qualification, off-host recovery, signed distribution, independent review, target capacity or beta completion. Restricted-channel exposure stays gated.
