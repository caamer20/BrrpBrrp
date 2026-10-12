# BrrpBrrp — project overview

This is the complete description of the project: what it is, what it does, how it is built, what
it promises and what it does not, and where each kind of document lives. It is written for four
readers: someone deciding whether to run a server, a member using one, an administrator running
one, and a contributor reading the code. Each section says where to go next.

For the short version, read [the README](../README.md). For how to use the app, read the
[user manual](manual/README.md). For the plan from here, read the [roadmap](../ROADMAP.md).

---

## 1. What BrrpBrrp is

BrrpBrrp is a self-hosted place to talk, for a small group. A person runs one server, on a computer
they control, and invites a community of up to 100 accounts to it. Members write in text channels,
hold private conversations with each other, talk in voice rooms and share their screens. The
server keeps everything on its own disk, in one SQLite database and one folder of uploaded files.

It is deliberately small in three ways:

- **One program.** A single native Go executable serves the whole app: the API, the realtime
  connection, the voice relay, the backup tooling and the web interface. The web interface is
  compiled and embedded into that executable. There is no separate application server, no database
  service and no hosted account system.
- **One data folder.** The database, uploads, certificates and settings live in one directory that
  an operator can back up, move and restore.
- **One community.** The product is designed for a single workspace run by one operator and a small
  set of members. It is not a multi-tenant service, and it does not federate with other servers.

### Who it is for

- **Members** of a small group — a club, a team, a class, a family, a project — who want a chat
  space they do not rent from a company.
- **Administrators** who want to run that space themselves, with backups they can restore and
  permissions they can explain.
- **Contributors** who want an open-source chat server with a small, readable code base.

### What it is not

It is not end-to-end encrypted, so the operator can read what is stored and hear voice that passes
through. It is not a public network, a federated system or a hosted service. It has no camera video,
no recording and no voice history. It is not yet a published, reviewed or measured product; see
[Status](#2-status-and-what-has-been-verified).

---

## 2. Status and what has been verified

**The current build is `1.0.0-rc.1`, a release candidate, schema 25.** It is the feature set intended
for version 1.0.0. It has not been published as an application release. The development repository is
private during qualification, and the project has no code-signing identity
and has had **no independent security, privacy or accessibility review**. No beta has happened.

The distinction between *built*, *tested*, *run on a platform* and *reviewed* matters, and the project
keeps it explicit:

| Question | Answer for this candidate |
| --- | --- |
| Is the feature set implemented? | Yes. Every feature family planned for 1.0 has an implementation. |
| Does the Go code build and type-check? | Yes, under the static gates the project runs. |
| Were the automated suites run on the candidate? | The local Go suites, 392 frontend tests, static/package gates and 30 off-mode/45 private-mode browser journeys passed on 8 October. Restricted-channel browser acceptance and the hosted race sweep remain open. See [deployment readiness](launch-readiness.md). |
| Has it run on Windows, in Docker or on a hosted runner? | Hosted cross-platform qualification has started. A Docker build defect was found and fixed; completed outcomes are tracked in [deployment readiness](launch-readiness.md). Native service and real-device qualification remain open. |
| Has it been run on a second machine or a real network? | No. Voice across two networks, a UDP-blocked network and a physical device are unqualified. |
| Has it been independently reviewed? | No. Security, privacy and accessibility reviews are owed. |
| Has it had a beta? | No. |

The authoritative account is the [1.0.0-rc.1 release record](releases/1.0.0-rc.1.md), the
[known limitations](known-limitations.md) and the [production readiness](production-readiness.md)
runbook, which records the gates a 1.0.0 release must pass before it is called one. The
execution ledger (`planning/v1-execution-ledger-2026-10-01.md`, in the development repository) is the evidence trail behind
each claim.

---

## 3. Principles

These come from the [roadmap](../ROADMAP.md) and the [architecture notes](architecture.md), and
they decide hard questions when they arise.

1. **Own your server and your data.** People can back up, move and restore their installation
   without a vendor account.
2. **Stay small to run.** A small installation is one executable and one data directory. Docker is
   an alternative, not a requirement.
3. **Authority is enforced on the server.** Visibility, posting, moderation and administration are
   checked on every request, every WebSocket action and every notification path. A hidden button is
   not a control.
4. **Consent before contact.** Nobody is added to a conversation without accepting it, and a block works
   in both directions.
5. **Accessibility, privacy, moderation and recovery belong to each feature's completion criteria.**
   They are not follow-up work.
6. **Honest claims.** A feature is described as implemented, tested, run or reviewed only at the level
   that has actually been reached. Written-but-unrun work is labelled as such.
7. **Bounds are product policy.** Limits such as 100 accounts are stated, enforced and documented, and
   raising them requires capacity evidence.

---

## 4. Features

### 4.1 Messaging

- **Text channels** with persistent history, paged 50 messages at a time, and a bounded display window.
- **Sending** with idempotency: a retried send returns the existing message instead of creating a copy.
  A send that cannot finish is kept under **Recovered messages**.
- **Editing and deleting** your own messages; administrators can delete any message after confirmation.
- **Formatting** limited to emphasis (bold, italic, strike), block quotes and literal code. No lists,
  headings or images from markup.
- **Mentions**, with a popup and, where enabled, device notifications. Mentioning everyone requires a
  capability.
- **Reactions**, from a picker of 60, with the reacting person's own removal.
- **Replies and focused threads**: reply threads from any message, with their own bounded paging,
  realtime updates and drafts kept separately from the channel.
- **Named threads** with follow state, unread replies and archiving, each with its own reading position.
- **Pins**, up to 100 per channel.
- **Attachments** up to 12 MB by default, with private server-made previews for images. Files are staged
  and claimed transactionally so a committed reference always has its bytes.
- **Link previews**, fetched by the server under strict network rules.
- **Full-text search** (SQLite FTS5) across channels, with scoped search inside a direct conversation.
- **Slow mode**, 0 to 21,600 seconds per channel, exempting channel managers.
- **Reports** of a message by a member, reviewed by moderators who can read that channel, retained 90 days.

### 4.2 Channels, categories and order

- Text and voice channels, grouped in ordered, durable **categories**.
- Drag-and-drop or keyboard ordering that saves for the whole server.
- Per-account **mute** of a channel, which suppresses popups and push but keeps counts and history.
- **Jump to a channel** (Ctrl/⌘ + K), with unread channels and mentions first.
- Up to 100 channels per workspace.

### 4.3 Roles, authority and restricted channels

- **Capabilities** are the unit of authority: channel reading, sending, reacting, uploading, pinning,
  deleting others' messages, mentioning everyone, voice connection, speaking, screen sharing, voice
  moderation, member and channel and category and role and invitation and settings management, audit
  reading, private-report review, and three operator capabilities (backup, credential recovery,
  ownership transfer).
- **Built-in roles** — Administrator, Moderator and Member — have fixed names and positions. Custom roles
  sit below their creator's position and carry only capabilities the creator holds. Roles only add.
- **Ownership** is a separate, explicit ceremony. The operator capabilities can only be granted by the owner.
- **Restricted channels** can be **private** (readable by listed roles and accounts) or **announcement**
  (posting limited to managers). Access changes take effect on the next request, and a person who loses
  access is removed from a restricted voice room in the same request.
- **The access inspector** ("Who can access this?") reports the rule the server applies, and states that
  community managers can read every community channel.
- Restricted channels and direct conversations are **on by default**. An operator withholds either with
  `BRRP_ENABLE_RESTRICTED_CHANNELS=false` or `BRRP_ENABLE_PRIVATE_CONVERSATIONS=false`. Neither has been
  independently reviewed.

### 4.4 Direct conversations and groups

- **Consent-based requests** with an optional note of up to 500 characters. Nothing else is shared until a
  request is accepted: no history, no files, no calls.
- Request limits per account — pending and new per day — and a rejection cooldown, all server defaults.
- **Blocking** in both directions makes a shared direct conversation read-only without removing either person
  from community channels. Unblocking never resends old operations.
- **Groups** named by an invitation, joined only by accepting it. The history rule is stated before acceptance.
- **Private evidence reporting**: a report of direct messages gives reviewers only the messages the reporter
  selected.
- The same message model serves channels, direct conversations and groups, so one set of authorization checks
  covers them all.

### 4.5 Voice and screen sharing

- **Voice rooms** with an audio-only selective forwarding unit (SFU) built on Pion WebRTC. The server forwards
  Opus, drops media for muted or deafened participants before sending it, and isolates rooms.
- **Caps**: 20 voice participants across the server and 10 per room.
- **Push to talk** (Space or a button), a microphone test, speaker selection where the browser allows it,
  a join chime, and a 30-second wait with **Retry voice** when the microphone does not open.
- **Screen sharing** of a display, with its own system audio, capped at 2.5 Mbit/s per publication, one
  per room and two per server. Capture is fenced to the people who can see the room.
- **Administrative voice moderation** — removing a participant and restricting microphone or audio —
  enforced in the forwarding path.
- **Embedded TURN relay** over UDP by default, with optional plain-TCP and TLS fallbacks for networks that
  block UDP. Credentials are temporary and tied to the voice session.
- No camera video, recording or voice history.

### 4.6 Notifications and realtime

- **WebSocket** realtime with `{t,d}` envelopes, the same cookie as HTTP, bounded outgoing queues, and
  disconnection and recovery for slow consumers.
- **Coalesced** message events with bounded concurrent lookups, plus short-lived journals that stop delayed
  responses from restoring deleted messages.
- **Unread counts** private to each account.
- **Web push** per device, with payloads that carry a notice rather than the message text.
- **Mention notifications** and **Messages and requests** notifications, configured per device.

### 4.7 Accounts and security

- Invitation-only accounts. Passwords (bcrypt, cost 12) and server-side session tokens stored only as hashes.
- **Two-factor sign-in** with TOTP and ten single-use recovery codes. Password reset does not remove a second factor.
- **Sign-in throttling** that counts failures only, per address, per address-and-username and per account,
  with an IPv6 /64 rule. The decision is recorded as D-17 in [security](security.md#application-protections).
- **Signed-in device list**, sign out of other devices, and recent-authentication checks for sensitive actions.
- **Member data export** of conversations the member can read, the member's own details and messages.
- **Account removal** choices: deactivate (reversible), remove keeping messages under *Deleted member*, or
  erase messages and files.
- **Owner recovery** on the host, recorded in the audit log under the host operator.
- Origin checks on mutations, a content security policy and framing protection, and HSTS for real HTTPS
  deployments that have their own name (not for local-CA or loopback modes).

### 4.8 Backups, recovery and operations

- **Scheduled backups** every 12 hours by default, seven kept, with uploads, written by `VACUUM INTO`
  snapshots streamed as a ZIP. New installations start with backups on.
- **Manifests** with a size and SHA-256 digest for the database and each attachment. Restore and
  `verify-backup` refuse an altered or truncated archive.
- **Offline restore** into a new data folder, never over an existing one. Sessions and push subscriptions are
  cleared, so devices sign in again.
- **Recovery checks** that report a backup kept only on the same host as not recovery, and a secondary copy
  directory (`BRRP_BACKUP_COPY_DIR`).
- **Operator alerts** delivered to a webhook configured on the host, signed with an optional secret.
- **Maintenance** shows connections, voice participants, message and upload counts, database size, free disk,
  backup status, memory and the slowest routes. A first-run checklist covers readiness.
- **Commands**: `serve`, `install`, `uninstall`, `backup`, `restore`, `verify-backup`, `create-admin`,
  `recover-owner`, `setup-token`, `generate-cert`, `repair-cert`, `preflight`, `migrate`, `verify`,
  `production-check` and `healthcheck`.

### 4.9 Installation and distribution

- A **browser-based installation wizard** that installs the executable and a background service, checks the port
  and storage, starts the server, and creates the first administrator. It listens only on a loopback address
  during setup.
- **Background services**: a systemd service on Linux, a Windows service on Windows, and a per-user launch agent
  on macOS.
- **Connection choices**: this computer only (`http-local`), a local network with a managed local certificate
  authority (`auto`), or an HTTPS proxy or certificate you manage (`https`, `http-behind-proxy`).
- **Docker**: a multi-stage Dockerfile and Compose files for Linux x64 and ARM64, with a read-only root filesystem,
  dropped capabilities and a health check. The image has not yet been built on a Docker host.
- **Release archives** for Linux, Windows and macOS on x64 and ARM64, with SHA-256 sums and dependency notices.
- **Installable web app** (PWA) with a service worker that keeps the static shell and does not cache messages or
  authenticated responses.

### 4.10 Interface and accessibility

- Light, dark and system themes, comfortable or compact spacing, a phone layout, and a preference to save
  drafts on the device.
- Keyboard support across the main surfaces: Ctrl/⌘ + K to jump, Ctrl/⌘ + Shift + K to search, arrow keys in
  tab lists and pickers, and Escape to close.
- Mixed-direction text is drawn in the order it reads.
- Automated accessibility checks run over the main surfaces. **That is not an accessibility audit**; keyboard,
  screen-reader and zoom passes are still owed.

---

## 5. Architecture

BrrpBrrp is one Go process with an embedded Svelte frontend. The responsibilities are split into packages:

| Package | Responsibility |
| --- | --- |
| `cmd/brrpbrrp` | Command-line entry point, service control, platform-specific install and uninstall |
| `internal/api` | Authentication, HTTP routes, WebSocket commands, moderation, settings, background jobs and the route policy |
| `internal/authz` | The capability model and the single rule every check calls |
| `internal/db` | SQLite schema, migrations, message hydration and offline account creation |
| `internal/ws` | Bounded socket fan-out and presence |
| `internal/rtc` | Opus SFU rooms, publications and the embedded TURN relay |
| `internal/backup` | Consistent snapshots, ZIP streaming, manifests and offline restore |
| `internal/installer` | Local browser setup, configuration checks, service registration and administrator creation |
| `internal/app` | Host configuration, filesystem containment, logging, TLS and graceful shutdown |
| `internal/limits` | The installation's hard bounds, in one place |
| `internal/metrics` | Truthful counters and measurements for the administrator panel |
| `internal/scraper` | Constrained HTTPS fetching and metadata extraction for link previews |
| `internal/desktop` | Platform-specific tray and desktop integration on Windows |
| `web/src` | The Svelte application: components, workspace state, voice, replies and the first-run checklist |

### Data

SQLite runs in WAL mode with foreign keys, a busy timeout and a single connection, which serializes writes
at the intended scale. Identifiers are UUIDv7. Timestamps are fixed-width UTC strings. Messages, attachments,
reactions and mentions are related by foreign keys, and FTS5 triggers keep search synchronized with edits and
deletions. Migrations are versioned; the current schema is **25**, the backup format is **2** and the
realtime protocol is **1**. A newer schema than the binary understands is refused, not guessed at.

Uploads are written to a temporary file, flushed, renamed and synced before any row refers to them, so an
interrupted upload leaves nothing behind and a committed reference always has its bytes.

### Realtime

Clients keep one WebSocket. Each connection has a bounded outgoing queue; a consumer that falls behind is
disconnected and resynchronizes from state and history. Mutations recheck the session and permissions. Read
state is sent only to its owner. Request revisions and a short-lived journal stop stale snapshots restoring
deleted or outdated content.

### Voice

The SFU forwards audio only. Each client has one microphone sender and nine receiving slots, with fixed
transceivers to avoid renegotiation as people join. Rooms are isolated, sequence and timestamp continuity is
maintained when slots are reused, and peer resources close on leave, socket loss, moderation or shutdown.
Screen publications have their own bitrate ceiling enforced at ingress. The TURN relay issues temporary
credentials that can reach only the server's own addresses.

### Resource bounds

Preview workers, a push worker and an hourly maintenance worker share cancellation with shutdown. Queues are
bounded. Previews have a three-second timeout, a 2 MB body cap and restricted redirects. Login hashing is capped
at two concurrent operations. Image decoding is limited in pixels and concurrency. Caches are disposable and
excluded from backups.

### Frontend

The Svelte application is compiled into `web/dist` and embedded in the binary, which serves precompressed gzip
and Brotli variants with ETags. Administrative and preference modules load on demand. Session and workspace
state live in `web/src/lib/workspace.svelte.ts`; replies in `replies.svelte.ts`; microphone, peer connection,
playback and speaking state in `voice.svelte.ts`.

---

## 6. Security model

BrrpBrrp is built for a small group whose operator is trusted. Its boundary is precise, and the
[security guide](security.md) is the full statement. In short:

- **Protected from members:** authority checks on every route, socket action and notification path; user text
  rendered as text; uploads served with safe content types; attachment paths generated by the server.
- **Protected in transit:** HTTPS by configuration; WebRTC transport encryption, which the SFU terminates to
  forward audio.
- **Not protected:** the operator and anyone with the data folder can read stored messages, attachments,
  backups and voice. Backups are not encrypted. There is no end-to-end encryption.
- **Outbound requests** from the server (previews, push) are constrained to public HTTPS, with DNS and redirect
  validation and pinned connections.
- **Credentials** are rate-limited by failures, not attempts, so one mistake or a shared address does not lock
  a member out.

The [vulnerability reporting policy](../SECURITY.md) names the enabled private GitHub intake and fallback contact.

---

## 7. Limits

These are product bounds, stated in [known limitations](known-limitations.md) and defined in
`internal/limits/limits.go`:

| Bound | Value |
| --- | --- |
| Live and recoverable accounts | 100 |
| Channels per workspace | 100 |
| Outstanding invitations | 100 |
| Pinned messages per channel | 100 |
| Voice participants, whole server | 20 |
| Voice participants, one room | 10 |
| Screen shares, one room / whole server | 1 / 2 |
| Screen bitrate | 2.5 Mbit/s per publication |
| Upload size | 12 MiB per file by default |
| Reports per account | 10 new per hour |

The limits are implementation boundaries, not measured capacities. No run has been made above them, and
no capacity or soak measurement exists.

---

## 8. Deployment

BrrpBrrp runs in four ways. Each is documented in full in the linked guide.

- **Native with the wizard** — the easiest path on Windows, Linux (systemd) and macOS. See
  [Installation](installation.md).
- **Native, configured by hand** — `brrpbrrp serve --config server.env`, with the platform scripts for
  operators who manage the service themselves. See [Deployment](deployment.md).
- **Docker and Compose** — for Linux hosts with a container runtime. See [Docker](docker.md).
- **Behind a reverse proxy** that terminates HTTPS and forwards WebSockets, with `BRRP_TRUSTED_PROXIES` set to
  the proxy's exact network. See [Hosting](hosting.md).

Ports are described in [Security](security.md#firewall-and-voice): the application port (8082 in the wizard),
UDP 50000–50100 for voice, and, if the relay is enabled, UDP 3478 with TCP fallbacks on the following two ports.
Updates and rollback are in [Upgrading](upgrading.md); a rollback loses writes made after the snapshot it restores.

---

## 9. Platforms and verification

| Platform | State |
| --- | --- |
| macOS, Intel (darwin/amd64) | Built and run on the development host, with the browser suites and recovery rehearsals. |
| macOS, Apple silicon (darwin/arm64) | Cross-compiled. Not run. |
| Linux | Cross-compiled. Not yet run on a Linux host. |
| Windows | Cross-compiled, including process counters, which are compiled but not executed. Install, service, ACLs and the Tailscale deployment are unqualified. |
| Docker | Hosted x64 build and container lifecycle/persistence checks passed on 8 October; the ARM64 image built. Real deployment, ARM64 runtime and off-host recovery remain unqualified; see [deployment readiness](launch-readiness.md). |
| Physical phones and tablets | None yet. Touch, the on-screen keyboard, background and resume, push and certificate trust are unqualified. |

The full, dated account is in [known limitations](known-limitations.md#what-has-been-executed-and-on-what).

---

## 10. Repository layout

| Path | What it holds |
| --- | --- |
| `cmd/brrpbrrp/` | The executable's entry point and platform service code |
| `internal/` | The Go packages listed in [§5](#5-architecture), with their tests beside them |
| `web/` | The Svelte application, its unit tests in `web/tests`, browser journeys in `web/e2e`, and its build |
| `docs/` | Operator, user and reference documents, decision records in `docs/adr/`, and release records in `docs/releases/` |
| `docs/manual/` | The end-user manual |
| `scripts/` | Release packaging, audits (documentation links, environment coverage, Docker inputs, workflow shells, schema documents), rehearsals and browser tooling |
| `planning/` | The execution ledger and verification queue that record how each change was made and what was run |
| `.github/` | Issue and pull request templates, and the CI workflows |
| `Dockerfile`, `compose*.yaml`, `.env*.example` | Container packaging and the configuration reference |
| `dist/` | Build outputs, including release archives; not committed |

---

## 11. Development

Install Go 1.27.1 or later and Node.js 22.12 or later (Node 24 is used in CI). Build the frontend before the
Go binary, because the binary embeds it:

```sh
npm --prefix web ci
npm --prefix web run build
go build -trimpath -o dist/brrpbrrp ./cmd/brrpbrrp
```

The `Makefile` wraps the common tasks:

| Target | Does |
| --- | --- |
| `make build` | Builds the frontend and the executable |
| `make test` | Runs the Go and frontend unit suites |
| `make check` | Runs the static gates: formatting, type-checking, vetting, the documentation, packaging and workflow audits |
| `make browser-check` | Runs the browser journeys after Playwright's Chromium is installed |
| `make rehearsal` | Migrates, backs up, restores into an empty folder and compares what survived |
| `make notices` | Regenerates the third-party notices from the modules that are linked |

`go run scripts/release.go` builds the release archives. Contribution rules are in
[CONTRIBUTING.md](../CONTRIBUTING.md): design expectations, verification, and what to report when a check
was written but not run.

The [architecture notes](architecture.md) say more about working on the code: the style, the testing
approach and the container packaging.

---

## 12. Governance and licensing

- **License:** MIT, in [LICENSE](../LICENSE). Every redistributed dependency is listed with its licence in
  [THIRD-PARTY-NOTICES.md](../THIRD-PARTY-NOTICES.md), and an SBOM is in `THIRD-PARTY-SBOM.json`.
- **Contributions** are covered by the [Contributor License Agreement](../CLA.md), which contributors accept
  when they open a pull request. The rationale is in [ADR 0006](adr/0006-licensing-and-contribution-terms.md).
- **Conduct:** [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md).
- **Support:** [SUPPORT.md](../SUPPORT.md) describes what a maintainer can and cannot do.
- **Security:** [SECURITY.md](../SECURITY.md).
- **Decisions** are recorded as architecture decision records in [docs/adr/](adr/), with dated notes when a
  decision is changed. Twenty records cover roles, schema migrations, restricted channels, private
  conversations, groups, threads, backups, attachments and media.

---

## 13. Documentation map

| If you want to… | Read |
| --- | --- |
| Understand the project quickly | [README](../README.md), then this document |
| Use the app as a member | [User manual](manual/README.md) |
| Run an administrator's day-to-day | [Administrators (manual)](manual/administrators.md) and the [administrator guide](admin.md) |
| Install a server | [Installation](installation.md), [Hosting](hosting.md), [Docker](docker.md) |
| Operate, update or roll back | [Deployment](deployment.md), [Upgrading](upgrading.md), [Production readiness](production-readiness.md) |
| Understand the security boundary | [Security](security.md) and [SECURITY.md](../SECURITY.md) |
| Know what does not work yet | [Known limitations](known-limitations.md) |
| See what changed in a release | [CHANGELOG](../CHANGELOG.md), [1.0.0-rc.1](releases/1.0.0-rc.1.md) |
| Plan the future | [Roadmap](../ROADMAP.md) |
| Read the design decisions | [ADRs](adr/) and [architecture](architecture.md) |
| Contribute code | [CONTRIBUTING.md](../CONTRIBUTING.md) and [architecture](architecture.md) |
| Check compatibility | [Compatibility](compatibility.md) |
| Look up a term | [Glossary](manual/glossary.md) |
