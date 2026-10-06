# BrrpBrrp — README for 1.0.0 (draft)

**Draft.** This is the front page the project intends to publish with 1.0.0. It is not the
current README — [`README.md`](../../README.md) is, and it stays in place until 1.0.0 is
released. Nothing here has been published, and the version it describes does not exist yet.

---

# BrrpBrrp

A small, self-hosted place for your people to talk: text channels, direct conversations,
group conversations and voice, with screen sharing, running as one binary on a machine you
own.

BrrpBrrp is for a community that wants its own space rather than a rented one — a club, a
team, a class, a group of friends — and is willing to run one process and keep one file
safe. It is one Go binary with the web interface embedded and SQLite underneath, so there
is no Node.js to install, no separate database, and no second service to keep alive.

## What you get

- **Text, in one place.** Channels with categories, threads, mentions, search, pins,
  attachments, reactions, per-account drafts and an opt-in outbox that survives a reload.
- **Conversations that belong to the people in them.** Direct and group conversations,
  requested and accepted rather than assumed, with blocking, an explicit consent record and
  no community role that can read them.
- **Voice that stays on your machine.** An embedded Opus SFU and an embedded TURN relay, so
  a small call needs no external media service. Mute, deafen, push-to-talk, per-person
  volume, output-device choice, admin moderation and reconnection after a lost connection.
- **Screen sharing**, with the share's own audio, a choice of quality preset, and a
  server-side ceiling so a share cannot starve the call.
- **Restricted channels**, so some rooms are for some people — granted by role or to an
  individual, and refused without disclosing that the room exists.
- **Moderation that leaves a record.** Roles and delegation, timeouts, private evidence
  reporting, an audit trail of authority and credential changes, and invitations you can
  bound and withdraw.
- **Recovery you can rehearse.** Verified backups with a manifest and checksums, a copy that
  can go somewhere other than this host, a restore path, and an upgrade you can roll back.
- **An interface you can use with a keyboard**, at phone width or a desktop's, with themes,
  reduced motion and low-resource modes.

## What it is not

- Not end-to-end encrypted. A conversation is private from other members, not from the
  machine the server runs on. Read [security.md](../security.md) before trusting it with
  something that must not be read by whoever holds the disk.
- Not Discord. There are no bots, no camera video, no lists in messages, no recording, no
  voice message history, and no mobile app — the interface is a web page you can add to
  your home screen.
- Not built for a large community. 100 accounts, 100 channels, 20 people in voice across
  the server and 10 in one room. See
  [known-limitations.md](../known-limitations.md) for the full boundary and
  [production-readiness.md](../production-readiness.md) for what has and has not been
  measured.

## Run it

Docker is the shortest path:

```sh
docker compose --env-file .env.docker up -d
```

Windows, and the native service on either platform, are in
[installation.md](../installation.md) and [windows-setup.md](../windows-setup.md). The
operator's guides are [deployment.md](../deployment.md), [docker.md](../docker.md),
[admin.md](../admin.md) and the [production runbook](../production-readiness.md).

Back up before you invite anybody, and put the copy somewhere other than this machine: a
backup that exists only on the host it came from is not recovery. That is what the server
tells you, and it is the one operational thing this project is insistent about.

## Updating and recovering

Updates are forward-only migrations, with the pre-upgrade snapshot kept beside the previous
executable so a release that goes wrong can be rolled back.
[upgrading.md](../upgrading.md) is the procedure and
[compatibility.md](../compatibility.md) is what may change between versions.

## Licence and contributing

MIT; see [LICENSE](../../LICENSE). Contributions are welcome under the CLA — read
[CONTRIBUTING.md](../../CONTRIBUTING.md), [CODE_OF_CONDUCT.md](../../CODE_OF_CONDUCT.md) and
[SUPPORT.md](../../SUPPORT.md) first. Security problems go to [SECURITY.md](../../SECURITY.md)
and nowhere else.
