# Known limitations

What this release does not do, and what has not been run on it. It is the honest half of
the release note: a list of what to expect rather than what to hope for. Read it beside
[acceptance.md](acceptance.md), which records what has been measured on this tree, and
[compatibility.md](compatibility.md), which states what may change.

This describes version `1.0.0`; its record is [releases/1.0.0.md](releases/1.0.0.md).
The owner requested stable publication with the remaining qualification work disclosed.
A stable release label does not supply the missing host, device, signing or independent-review evidence.

## Bounds built into the product

These are product policy, not settings. Raising one means shipping a different build,
because the capacity evidence behind each number belongs to the release that states it
(`internal/limits/limits.go`, `internal/rtc/sfu.go`, `internal/rtc/publications.go`).

| Bound | Value |
| --- | --- |
| Live and recoverable accounts | 100 (erased identities are tombstones and do not count) |
| Channels per workspace | 100 |
| Outstanding invitations | 100 |
| Pinned messages per channel | 100 |
| Voice participants serverwide | 20 |
| Voice participants per room | 10 |
| Screen shares per room | 1 |
| Screen shares serverwide | 2 |
| Screen bitrate | 2.5 Mbit/s per publication, enforced at ingress |
| Upload size | 12 MiB per file by default (`BRRP_MAX_UPLOAD_BYTES`) |

The account and channel ceilings are the profile the release is designed around. This is a
server for one small community, not a multi-tenant service: 100 accounts is a capacity
statement, and nothing here has been measured above it.

## Media

- **IPv4 only, and the embedded relay only.** There is no external TURN service in v1 and
  no IPv6 media transport. A network that blocks UDP entirely needs a different route:
  the built-in relay carries UDP, and its TCP and TLS listeners exist for a network that
  blocks UDP but allows TCP.
- **No cameras.** Voice is a microphone, and screen sharing is a screen. There is no video
  call.
- **No recording and no voice history.** A call leaves nothing behind.
- **Not end-to-end encrypted.** WebRTC encrypts transport with DTLS-SRTP and this server
  terminates that encryption in order to forward audio. The operator can read stored
  messages and relay audio, and a backup contains them. What the application defends and
  what it does not is in [security.md](security.md).
- **Quality and capacity are not measured on hardware.** SFU behaviour is exercised with
  synthetic tracks on loopback. A real microphone, a real display, a real network and real
  devices remain qualification work.

## Text and interface

- **Formatting is emphasis and block quotes only.** There are no lists, and code stays
  literal: no piece of a message ever becomes markup.
- **Automated accessibility checks are not an accessibility audit.** Journeys run axe over
  the main surfaces, which finds what a tool can find. A keyboard pass, a screen-reader
  pass and a zoom pass are owed, and no blanket conformance is claimed.
- **Both conversation features are on by default, and neither has been independently
  reviewed.** Restricted channels and consented private conversations are implemented and
  enforced. They shipped switched off until an independent security and privacy review; on
  6 October 2026 the owner turned them on without one. An operator who wants to wait sets
  `BRRP_ENABLE_RESTRICTED_CHANNELS=false` or `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false`.
- **A restricted voice channel has no ring or admission step of its own.** It is as closed
  as a restricted text channel for presence, joining and revocation, and community managers
  can read and join every community channel, restricted ones included; the access inspector
  says so on each channel.
- **The interface is English and left to right.** Messages in any script are rendered in
  their own direction, including mixed-direction text; the product's own controls and
  labels are not translated or mirrored.
- **A shared screen's sound follows its sharer's volume.** The volume a listener sets for a
  person applies to that person's voice and to the sound of their shared screen together;
  the two cannot be set apart.

## Leaving a community

- **A member can take their data but cannot remove their own account.** **Your data export**
  (the download control beside the member's own name) produces an archive of the
  conversations they can read, their own account details and their own messages, gated on a
  recent strong sign-in. Removing an account is an administrator's action only — **Manage
  server → People** offers deactivation, remove-account-keep-messages and
  erase-account-and-messages — so a member who wants to leave asks an operator. Whether v1
  adds a self-service departure is an open question for the owner
  (`planning/verification-queue.md` §5).

## Backups and recovery

- **Backups are not encrypted.** The archive includes private push keys. Storing one
  off-host means storing it somewhere it is protected; the encrypted procedure in
  [deployment.md](deployment.md) covers that, and on this machine it has been rehearsed
  with `openssl` only — the `age` and `gpg` paths are documented and were not exercised.
- **A backup on the same machine is not recovery.** The recovery check reports a critical
  condition until an independently recoverable, verified copy exists, which means setting
  `BRRP_BACKUP_COPY_DIR` (or the Docker backup service) and letting a copy succeed.
- **Rollback loses later writes.** It restores a pre-upgrade snapshot, so everything
  written after that snapshot, including later moderation and security decisions, is
  outside the recovery point. See [upgrading.md](upgrading.md).

## Two owner decisions of 6 October 2026

Both are decided and are recorded in `planning/v1-execution-ledger-2026-10-01.md`:

- the default backup interval is **12 hours**, matching the recovery objective the plan
  states (lose at most 24 hours). An installation that already stores an interval keeps
  it — the default changed, the stored row did not; and
- a **new installation starts with scheduled backups on** (W-37): the setup route and
  offline setup write the flag into a first settings row, and nothing else changes it.
  Existing installations keep the setting they already have, so an installation that
  deliberately switched backups off stays off. Because a fresh installation has no
  off-host destination yet, its recovery check reports the critical condition above from
  its first minute — that is the ratified policy and it is true, not a defect.

## What has been executed, and on what

Nothing below is a claim about a platform this project has not run on. An archive existing
is not evidence that it works.

| Platform | State |
| --- | --- |
| macOS, native (darwin/amd64) | Built and run here: the Go suites, the browser suites and the recovery rehearsals were all executed on this host. |
| Linux, native | Hosted native Go suites passed for the preceding RC; exact 1.0.0 results are recorded on GitHub Releases. Native systemd installation, physical networks and off-host recovery remain unqualified. |
| Windows, native | Hosted native Go suites passed for the preceding RC; exact 1.0.0 results are recorded on GitHub Releases. Installation, service lifecycle, ACLs, tray and the Tailscale deployment remain unqualified. |
| Docker / any container runtime | Hosted x64 image build and container lifecycle/persistence checks passed on 8 October, and the ARM64 image built. Real host operation, ARM64 runtime, external media networks and off-host recovery remain unqualified. See [deployment readiness](launch-readiness.md). |
| Physical devices | None. No phone, tablet or second machine has been used: touch, on-screen keyboards, background/resume, push notifications and certificate trust are all unqualified. |
| Second host / off-host storage | None. Both-host recovery, an off-host backup destination and a restore on different hardware remain qualification work. |
| Independent reviews | Not entered. Security, privacy and accessibility reviews remain outstanding production acceptance; the conversation features are already enabled. |
