# Roadmap

BrrpBrrp's direction is a self-hosted chat and voice server that a small community can run,
back up and trust without a vendor account. This roadmap says where the project stands, what
production qualification remains, what comes after version 1.0.0, and what is deliberately out of scope.

It makes **no delivery dates**. Each item states what it needs before it can be called done, so
that its completion can be checked rather than asserted.

For what is in the product today, read the [project overview](docs/project-overview.md). For the
known gaps in the current release, read [known limitations](docs/known-limitations.md).

## How to read this roadmap

Every item has one of these statuses:

| Status | Meaning |
| --- | --- |
| **Shipped** | In the current release, implemented, and covered by tests in the source tree. |
| **Shipped, not run** | Implemented in the release; the tests that prove it have been written but not yet executed on this exact tree. |
| **Open gate** | Required for production deployment sign-off. Usually needs a machine, a person or elapsed time. |
| **Planned** | Documented as the next slice of work after 1.0.0. Scope is written down; timing is not. |
| **Proposed** | An idea recorded here for discussion. It has no decision behind it yet and may change or be dropped. |
| **Not planned** | Deliberately outside the project's design. Listed so it is not mistaken for an oversight. |

Items marked **Proposed** are this roadmap's own suggestions, not commitments in any earlier record.

## Where we are

The project is at **1.0.0**, schema 25, for the stable release channel. The owner requested
stable publication while keeping the remaining production qualification visible.

- All planned feature families are implemented: roles, restricted channels, consented conversations,
  groups, moderation, exports, voice/screen sharing, verified backups and operator alerts.
- The preceding [RC qualification](https://github.com/caamer20/BrrpBrrp/actions/runs/38244004395)
  passed all 18 jobs, including 142 browser executions and 354 recovery/rollback checks. Its full-disk
  Linux rehearsal skipped because loop mounts were unavailable; that scenario was not qualified.
- Every version requires fresh exact-source automated qualification and actual asset verification.
  Version 1.0.0's source, run, checksums and publication status are recorded on
  [GitHub Releases](https://github.com/caamer20/BrrpBrrp/releases/tag/v1.0.0).
- Clean-host/device and ARM64 runtime acceptance, native signing/notarization, external networks,
  independent reviews, measured capacity and the community beta remain open.
- The public repository has private vulnerability reporting, a conduct/privacy contact, protected
  `main` and protected version tags. `badges` contains only the dated download-count snapshot.

Read the [1.0.0 release record](docs/releases/1.0.0.md),
[deployment readiness](docs/launch-readiness.md) and [known limitations](docs/known-limitations.md).



## Production qualification

### 1. Retain automated release qualification

**Required for every version.** The preceding RC completed hosted native suites, five race shards,
three Chromium modes, WebKit and Firefox media, Docker, recovery/rollback and package consumers.
Retain a new exact-source run and checksums for 1.0.0. Any failing automated gate blocks packaging.
A skipped scenario stays unqualified; automated success does not close the physical acceptance work below.

### 2. Exercise restricted channels in a browser

**Partly verified.** The shipped-default browser journey now covers a private channel, an individual
grant, the rendered access inspector, outsider denial and live revocation. Extend it to role grants,
announcement-channel posting and restricted voice/device behavior. The complete target remains: a
private channel, granting a role and an individual account, a
removed grant ending access immediately, an announcement channel's posting rule, a restricted voice room,
and the access inspector's statement.

### 3. Build and run the remaining acceptance scenarios

**Open gate.** These are specified in the project plan and not yet built or run:

- A thousand sends with injected faults, to show that retries never duplicate and recovery never loses text.
- A hundred screen-share start-and-stop cycles.
- Read state on two devices for the same account.
- An update between two different builds, with data preserved.
- The outsider matrix run against the live server, not just the route inventory.
- Failure coverage across the upload, backup and restore paths.
- A capacity run at the reference profile: 100 accounts, 20 voice participants, and a message volume
  agreed with the owner (see step 7).

### 4. Qualify on real machines

**Open gate.** The release must be run where people will run it:

- **Windows:** install, service lifecycle, file permissions, the firewall rules and the tray, on a real host.
- **Linux:** the systemd installation and the dedicated service account, on a real host.
- **Docker:** the image is built, its health check passes, and the smoke test runs on a Docker host. The
  first hosted build found a notices output-path defect; its corrected build and runtime qualification
  are tracked in [deployment readiness](docs/launch-readiness.md).
- **Two networks:** a voice call between two networks, including one where UDP is blocked and the relay's
  TCP or TLS fallback carries it.
- **Devices:** a phone and a tablet for touch, the on-screen keyboard, background and resume, push
  notifications, and certificate trust.
- **Off-host backup:** a backup copied to a second destination, restored on different hardware.

The per-host procedures are written in [host qualification](docs/host-qualification.md), and the ordered
sign-off is in [production readiness](docs/production-readiness.md).

### 5. Independent reviews

**Open gate.** Three independent reviews remain outstanding production acceptance:

- **Security**, of the authority model, the authentication flows, the upload and preview paths, and the voice relay.
- **Privacy**, of the restricted channels, the consented conversations, reports, exports and retention, which
  were switched on without a review.
- **Accessibility**, of the keyboard, screen-reader and zoom behaviour. Automated checks are not an audit.

Each review produces numbered findings. The highest-severity findings block production rollout; how the others
are handled is a decision for the owner.

### 6. A beta on real communities

**Open gate.** A fourteen-day beta with three communities, followed by a seven-day observation of a frozen
candidate. The beta's purpose is to find what the tests and reviews did not: confusing flows, real-world
network trouble, and the moderation situations a community actually produces. Its entry conditions are in the
beta entry checklist, `planning/beta-entry-checklist.md`, in the development repository.

### 7. Owner decisions

These are open questions that only the project's owner can answer. They are recorded in the verification queue,
section 5, and none of them is assumed.

1. **Community enforcement contact:** supplied and published in the code of conduct.
2. **Support response expectations:** community support is best-effort, with no response guarantee.
3. **Harness follows the gate default:** an explicit shipped-default mode is part of qualification.
4. **Community guide: living or dated.** Whether member-facing guidance is updated in place or versioned.
5. **Self-service departure in 1.0.** Whether a member can remove their own account in v1, or whether removal
   stays an administrator's action as it is now.

A sixth decision is the **capacity target**: the one-million-message and 20 GiB reference fixture is defined
by the owner before the 100-account reference run can be written.

### 8. Publication

**Stable publication requested.** Prepare 1.0.0 through the full automated workflow and verify
the actual downloadable assets before publishing it as latest. Remaining host/device, review,
capacity and beta work stays visible in its release notes. Publication status and remaining work:

- The public repository, MIT licence, contribution terms and private reporting intake are configured.
- Private vulnerability reporting is enabled. An external report delivery test remains pending
  before wider rollout; configuration alone does not establish delivery.
- Hosted release artifacts receive GitHub/Sigstore build-provenance attestations. Platform code signing
  and notarization identities remain unconfigured; provenance is separate from those signatures.
- A release policy and changelog process are in place, with [CHANGELOG.md](CHANGELOG.md) as the record.

## Planned after 1.0.0

These are documented as the next slices of work. Each names what it needs before it is done.

### Conversation and formatting

- **Lists in messages.** Deferred from the first formatting slice. Needs: rendering that never turns member text
  into markup, keyboard and screen-reader behaviour, and tests in the message renderer.

### Live rooms

- **Bandwidth presets for screen sharing**, and **caps a room can set**, so a small server controls its own
  media load. Needs: measured bitrate behaviour on real networks, not only the loopback tests.
- **Device selection beyond the basics.** The audio settings already choose an input and, where the browser allows, an
  output. The roadmap's next slice covers the device choices still missing. *Needs:* a written definition of those
  choices, since they are not yet specified.
- **Automated Firefox and WebKit coverage for screen sharing.** Today the screen path is exercised end to end in two
  browsers; the rest needs automation. Needs: the browser harness to drive those engines reliably.

### Operations

- **Encrypted backups.** The procedures for encrypted off-host copies exist in [deployment](docs/deployment.md) using
  `openssl`. The `age` and `gpg` paths are documented but have not been exercised. Needs: exercising each path and
  a restore from an encrypted copy.
- **Measured capacity profiles.** Documented numbers for 100 accounts, with the hardware that produced them, so an
  operator can size a host from evidence. Needs: the capacity run in step 3.

### Accessibility and language

- **A full keyboard, screen-reader and zoom audit**, with the findings fixed. Needs: an independent reviewer and
  recorded results per surface.

## Proposed, not yet decided

These are suggestions from this roadmap. Each is a candidate for discussion, not a commitment. Several would change
an existing design principle, and those are marked.

- **Scripted restore drills.** The rehearsal already restores into an empty folder. A scheduled, reported drill an
  operator can run against a real backup, without touching live data, would make recovery routine. *Needs:* a
  design for where its report is kept.
- **Interface translation**, starting with the sign-in, setup and member surfaces. Layout mirroring belongs to the
  same work, because the message renderer already handles mixed-direction text. *Needs:* a translation process and
  a decision about who maintains each language.
- **Notification quiet hours and digests.** A member sets hours in which push is held, and a daily summary of mentions
  instead of many small notifications. *Needs:* a decision about where the schedule is stored and how it interacts
  with a call in progress.
- **Message edit history visible to moderators.** Moderators can see what a message said before an edit, so an
  edit cannot hide abuse. *Needs:* a privacy decision. It changes what an edit means to the author, and must be
  disclosed in the guide before it ships.
- **A documented API and webhooks for channels.** Let an operator's own tools post to a channel or receive its
  events, under a bot account with its own capabilities. *Needs:* a token model, its own rate limits and a review.
- **Admin-side audit export.** Export the audit log for a period as a file, for an operator's records.
  *Needs:* the same retention rules the log already has.
- **Signed releases and a package manager entry** for Windows and macOS, once a signing identity exists.
- **Accessibility-first default for new installations**: reduce motion and compact layout chosen by the setup
  wizard, with the choice shown. *Needs:* a usability check on the setup flow.
- **Interactive restore in the browser**, with the archive verified and restored into a new folder by the server.
  *Needs:* a design that keeps restore offline, because a restore can replace state. This conflicts with the current
  rule that restore never runs against a live installation, so it changes a principle.

## Exploratory

These need a decision before anyone designs them. They are listed so the trade-off is visible.

- **A second storage backend** for installations that outgrow one SQLite database. *This changes the single-binary,
  one-folder principle.* It would need a migration path and an honest statement of which features lose their
  guarantees.
- **External TURN servers.** Excluded from v1; the built-in relay is the only supported route. Revisiting it needs
  a supported configuration, credential handling and a test on a network that blocks UDP.
- **IPv6 media.** Media is IPv4 today. *Needs:* a measured case for it and tests across dual-stack networks.
- **Camera video.** Not scheduled. It would change the bandwidth, capacity and privacy assumptions across the
  product, and it needs its own qualification.
- **Federation between servers.** Not planned for 1.0. *It changes the trust model entirely*, and it is listed only
  so its absence is deliberate.

## Not planned

These are outside the design. They are recorded so that their absence is not mistaken for an oversight.

- **End-to-end encryption of messages and voice.** The operator holds the keys by design, so the operator can read
  stored messages and relayed audio. This is stated in every guide.
- **Hosted, multi-tenant service.** The product runs one community per server.
- **Multi-server guilds**, friends lists and public read receipts.
- **Public profiles and discovery** across servers.
- **Recording or voice history.** A call leaves nothing behind.

## How this roadmap changes

Items move between statuses when there is evidence. An open gate closes when its check has been run and recorded
in the execution ledger or the verification queue (both in `planning/`, in the development repository).
A proposal becomes planned when someone writes down its scope, its completion criteria and the decision that
approved it. Changes to this file belong in the same change as the decision they record.
