# Continue toward v1 after alpha.5

The latest implemented checkpoint is **0.4.0-alpha.5 / schema 10 / backup format 2**. Read [the release record](../docs/releases/0.4.0-alpha.5.md), [progress](../docs/v1-progress.md) and the relevant ADRs before starting. Earlier numbered reviews and the product report describe earlier source snapshots; use their regression scenarios, not their old inventory, as current evidence.

## Preserve this checkpoint

- Do not reopen spent operation keys, auto-send recovered text, truncate malformed intent into valid work or reuse account A's device preference for account B.
- Keep transaction-time session/permission checks and the database-before-authority-fence acquisition order. Revocation includes newly committed socket events and push registrations already admitted by middleware.
- Keep removed identity rows immutable. They preserve history but do not count as live account slots. Capacity check and creation must share a transaction.
- Keep report evidence selected, bounded, expiring and readable only by current authorized reviewers. Reports are not an admin bypass into private conversations.
- Keep notification preferences separate from unread/mention history, and recheck late asynchronous client notifications and each push dispatch.
- Preserve verified recovery identities when a copy or status write fails. Copy completion time is not snapshot time.
- Do not enable the restricted-channel gate, publish packages, rename this checkpoint v1.0.0 or claim host/media/security acceptance without its evidence.

## Next implementation sequence

### 1. Private conversation storage and authority

Start from `internal/db/migration6.sql`, `internal/db/migrations.go`, `internal/db/messages.go`, `internal/api/conversations.go` and ADR 0010. Schema 6 intentionally kept `messages.channel_id NOT NULL`; simply adding DM endpoints will not work.

Specify and implement a rowid-preserving message table migration with foreign-key mode controlled outside the transaction, preserving replies, reactions, pins, mentions, attachment claims, FTS and send receipts. Pin interruption/restart/rollback behavior with a legacy schema-10 dataset. Unaccepted members and operators without membership must fail every content/metadata path. Keep exposure disabled until these contracts and the privacy gate are satisfied.

**Acceptance:** preflight, migration and verify report the new schema accurately; exact message IDs/rowids and attachment hashes survive; injected interrupted migration does not open a partially upgraded store; full search/read/download/event/retry checks deny unrelated accounts.

### 2. Direct requests, consent and blocking together

Ratify contact defaults and private-history lifecycle first. Use a unique canonical account pair and explicit pending/accepted/rejected states. Limit request creation, invitation counts and rejected-contact retries. No attachments or calls before consent. Add block/unblock before exposing direct messages broadly; scope notification payloads and ensure cross-account UI responses are discarded.

**Acceptance:** racing requests create one pair; requests cannot leak accepted history; administrators have no membership shortcut; read/search/reply/file/notification/socket paths agree after block, removal and account deletion. Hiding a conversation does not erase another participant's history. Offline retry never resurrects erased text.

### 3. Small groups and organized discussions

Build groups on the same services with acceptance, ownership transfer, ten-member initial cap, leave/removal rules and explicit history disclosure. Extend named/archived threads only after common message/cursor services are stable. Keep public reply discussions working through upgrades.

**Acceptance:** pending or departed accounts receive no newly admitted content, files or notifications; stale UI responses cannot restore it; history policy is explained before acceptance; account removal has a defined owner succession path.

### 4. Media and user experience

Complete the media admission design before private calls or screen sharing. Preserve microphone/device/room-transfer behavior and server-side forwarding enforcement. Add desktop screen selection/stop/reconnect and physical mobile viewing with visible capability failures. Expand keyboard, screen-reader and phone journeys using the exact supported browsers.

**Acceptance:** actual physical two-device audio, revocation during flowing RTP, separate-network relay, 30-minute calls and screen-share stop/rejoin; bounded failure UI when capture is unavailable. Synthetic fake audio is only transport evidence.

## Parallel release work

- Connect the Windows/Tailscale and Linux Docker reference hosts. No such host credentials or addresses were available during alpha.5 work.
- Run the existing Docker runtime checker and both install/update/reboot/lockout/backup/rollback runbooks. Capture exact executable/image identity, host versions and result evidence.
- Restore an independent backup on a genuinely separate destination; measure RPO/RTO on a reference dataset.
- Establish the canonical remote repository, maintainer access/recovery, private security intake, hosted CI and signing/provenance. The local Git baseline and local ZIPs are not public release evidence.
- Complete independent foundation review, target-capacity/load/soak measurements, the planned beta and final-candidate observation. Do not turn time-based gates into a checklist marked complete by code generation.

## Verification discipline

Run focused behavior regressions while developing. Before a checkpoint, run frontend checks/build, complete Go vet/race tests, browser journeys, both recovery rehearsals and package-consumer checks. Use `GOCACHE`/`GOMODCACHE` outside protected tool directories when needed. Tests need local loopback/UDP permission; that is different from deployment authorization. Record any capture-environment failure separately from executed media assertions.

The source archive excludes `planning/`; packaged end-user documentation must link only to packaged files. Keep machine paths, temporary accounts, browser state, data folders and private keys out of release artifacts. Build every platform archive from the same frozen frontend/source/docs. Preserve an honest prerelease version until every required v1 gate is met.
