# Support policy

**Draft.** The policy below is what the project intends to offer at 1.0.0. It is written
before 1.0.0 exists and before any support channel has been opened, so it describes an
intention rather than a commitment that has been tested against real traffic. Anything
still a decision of the project owner's is marked as such rather than guessed at.

## What this is

BrrpBrrp is a self-hosted server. Whoever runs an installation is the operator of that
installation: they hold the data, choose the people who may join, and decide what happens
to the machine it runs on. The project publishes source, binaries and documentation. It
does not operate anybody's server and cannot reach into one.

That has three consequences worth stating plainly.

- **A community's data is the community operator's.** Nobody on the project can read a
  message, a private conversation, an export or a backup, and nobody can reset a password
  on an installation they do not hold access to. The recovery path for a lost administrator
  credential is the one in [deployment.md](docs/deployment.md), and it needs access to the
  host.
- **Operational questions are answered with the documentation, not with access.**
  [deployment.md](docs/deployment.md), [administration](docs/admin.md),
  [docker.md](docs/docker.md), [windows-setup.md](docs/windows-setup.md),
  [host-qualification.md](docs/host-qualification.md) and the
  [production runbook](docs/production-readiness.md) are the support surface. Where a
  question has no answer there, that is a documentation defect to report.
- **A security problem is not a support request.** [SECURITY.md](SECURITY.md) is the path,
  and it is the only path for anything that could be exploited.

## What is supported

| | |
| --- | --- |
| The released source and the binaries the project builds from it | Supported at the version released; the previous minor version is supported for the upgrade path described in [upgrading.md](docs/upgrading.md) |
| The two documented deployments | Windows as a native service, and Linux under Docker, as specified in [host-qualification.md](docs/host-qualification.md) |
| The documented configuration keys | Those listed in [deployment.md](docs/deployment.md) and [compatibility.md](docs/compatibility.md) |
| Restoring a backup the server itself wrote | Supported, format 2, per [deployment.md](docs/deployment.md) |
| The HTTP and WebSocket interfaces the bundled frontend uses | **Not** a supported API. They are internal and version with the server; see [compatibility.md](docs/compatibility.md) |

## What is not supported

- Installations edited at the database, at the schema, or by hand in the data directory.
- A fork, a patched build, or a reverse-proxy arrangement outside the documented one.
- Platforms other than macOS, Linux and Windows. Other Unixes may work; nothing is
  measured on them.
- Third-party clients, bots and integrations. No compatibility promise is made for them.
- Anything in [known-limitations.md](docs/known-limitations.md): IPv4 and the embedded
  relay only, 100 accounts, no end-to-end encryption, no lists, no camera video, and the
  platform gaps recorded there.

## How a problem is reported

Open an issue on the repository with the version (`brrpbrrp version`), the deployment
(Windows service or Docker), the exact command or step, what happened, what was expected,
and whether any data was at risk. Include the operator's log excerpt where it is safe to:
logs contain usernames and conversation identifiers, so read them before attaching them.

Do not put message contents, export files, backups, credentials or an installation's data
directory in an issue. If a problem cannot be described without them, say so and describe
the shape of the data instead — the project will ask for something narrower.

## Response expectations

**Not yet set.** There is no hosted CI, no released 1.0.0, no support channel and no
maintainer rota, so any number written here today would be invented. The project owner
decides, when there is something to support: which channels exist, whether there is a
security-only response target, and how a report that affects data safety is escalated.
Until that decision is recorded, treat every report as best-effort by whoever is carrying
the project at the time.

## Related documents

- [SECURITY.md](SECURITY.md) — reporting a vulnerability.
- [CONTRIBUTING.md](CONTRIBUTING.md) — contributing a change.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) — how people treat each other here.
- [known-limitations.md](docs/known-limitations.md) — what the product deliberately does
  not do.
