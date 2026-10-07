# Compatibility

What BrrpBrrp promises to keep working, what it reserves the right to change, and which
versions of its data it accepts. Version 1.0 is where this becomes a commitment; until
then every statement here is the current state of a development tree, and the plan's own
compatibility section — "Data migrations and compatibility" in the v1 implementation plan,
which ships in the source package but not beside a binary — is the contract this document
reports on.

## The four numbers

They move independently, and confusing one for another is how somebody decides a rollback
will work and finds out afterwards that it will not.

| Number | Value in this tree | Where it is defined | What it means |
| --- | --- | --- | --- |
| Application version | `1.0.0-rc.1` | `internal/app/version.go` | Which build this is. Not a compatibility statement by itself. |
| Database schema | 25 | `internal/db/migrations.go`, derived from the migration registry | What the database contains. Forward-only, and a binary refuses a database from a newer schema. |
| Backup format | 2 | `internal/backup/manifest.go` | What a backup archive looks like. Read by the restore, verify and production-check paths. |
| Websocket protocol | 1, minimum 1 | `internal/ws/protocol.go` | Whether an open client and this server can understand each other's frames. |

The schema is derived from the migration registry rather than written down, and
`make check` compares the number stated in the documentation with the code, so a stale
number fails the build instead of misleading an operator. The websocket protocol moves only
when a frame's *meaning* changes — adding a field is not a protocol change, because an old
client ignores what it does not know.

## Stable configuration keys

The keys below are the supported configuration surface: each is documented in
[deployment.md](deployment.md) or named in a guide, the example files list the ones an
installation normally sets, and the installation wizard writes the ones it sets into
`server.env`. A key in this list keeps its name and its meaning, and a change to one is a
change to the release's contract. Where a guide and an example file disagree with the code,
the code is right and the disagreement is a documentation defect — `internal/app/config.go`
and `internal/api/alerts.go` are what actually read these.

- `BRRP_DATA_DIR`, `BRRP_ADDR`, `BRRP_BASE_URL`, `BRRP_TLS_MODE`
- `BRRP_DOMAIN`, `BRRP_ACME_EMAIL`, `BRRP_CERT_FILE`, `BRRP_KEY_FILE`
- `BRRP_BEHIND_PROXY`, `BRRP_TRUSTED_PROXIES`, `BRRP_ALLOW_INSECURE_HTTP`
- `BRRP_LOG_LEVEL`, `BRRP_LOG_FILE`
- `BRRP_DB_FILE`, `BRRP_UPLOAD_DIR`, `BRRP_SETUP_TOKEN_FILE`
- `BRRP_MAX_UPLOAD_BYTES`, `BRRP_STORAGE_QUOTA_BYTES`, `BRRP_STORAGE_RESERVE_BYTES`
- `BRRP_LOGIN_ATTEMPTS`, `BRRP_SQLITE_SYNCHRONOUS`
- `BRRP_VOICE_EXTERNAL_IP`, `BRRP_VOICE_PORT_MIN`, `BRRP_VOICE_PORT_MAX`
- `BRRP_BACKUP_COPY_DIR`, `BRRP_ALERT_WEBHOOK`, `BRRP_ALERT_SECRET`
- `BRRP_ENABLE_RESTRICTED_CHANNELS`, `BRRP_ENABLE_PRIVATE_CONVERSATIONS`

Everything else the binary reads from the environment is not part of that surface: the
`BRRP_E2E_*` harness variables, the load-measurement switches and the internal overrides
used by tests exist to run this project's own checks and may change or disappear without
notice. `BRRP_ENABLE_RESTRICTED_CHANNELS` and `BRRP_ENABLE_PRIVATE_CONVERSATIONS` are
supported keys whose *defaults* changed from off to on in 1.0.0-rc.1; an installation that
sets either one explicitly keeps that setting across an upgrade, an unset or empty value
takes the default, and the change is stated in the release record and in
[upgrading.md](upgrading.md) rather than made silently.

Limits that are product policy rather than configuration — 100 accounts, 100 channels, 100
outstanding invitations, 100 pins per channel, 20 voice participants serverwide, 10 per
room, one screen per room and two serverwide — are not keys at all. They live in
`internal/limits/limits.go` and `internal/rtc/`, and raising one means shipping a different
build, because the capacity evidence behind the number belongs to the release that states
it. See [known limitations](known-limitations.md).

## Supported installation and update paths

- **Install.** Native archives (Linux, macOS, Windows) and the Docker image. Both are
  described in [installation.md](installation.md) and [docker.md](docker.md).
- **Update within a schema.** Replace the executable, keep the data directory, restart.
- **Update across schemas.** Forward-only, transactional migrations, applied by
  `brrpbrrp migrate` or on start. The binary refuses a database from a newer schema rather
  than downgrading it, and refuses to serve a database whose upgrade did not finish.
  [upgrading.md](upgrading.md) is the procedure.
- **Restore.** A verified backup archive, into a clean destination, with an executable
  whose schema matches the archive. Restoring an *older* archive with an *older* executable
  is the supported rollback; the reference rollback rehearsal crosses alpha.7 (database 21)
  to this tree (database 25).

## Backup format

Format 2 archives carry an application and schema version and a size and SHA-256 digest for
the database and for every attachment; format 1 archives carry no inventory, so their
entries can only be checked for structure and the ZIP's own checksums. The restore, verify
and production-check paths all read both, so an old supported archive stays readable.

What that does **not** mean: an archive written by a *newer* binary is not advertised as
readable by an older one unless that direction has been tested, and an archive of an
already-upgraded database is not an old-version rollback source. Verify an archive with the
executable that will restore it, and rehearse the restore, before relying on it. The archive
carries digests, which detect damage; it does not carry a signature, so it does not
establish who produced it.

## The application interface

The HTTP routes and websocket frames under `/api` and `/ws` are **internal**: they are what
this project's own bundled frontend speaks, they are versioned by the websocket protocol
number rather than by SemVer, and they may change in any release without a deprecation
period. Nothing here promises them as an integration API. A supported external interface
would need versioning, credentials, rate limits and its own compatibility tests, and there
is none today.

## What is not promised yet

- **No support window and no deprecation policy.** `SECURITY.md` states that the newest
  release is the supported one because the upgrade path is forward-only; that is the whole
  of the policy today.
- **No published repository, so no release cadence.** There is no remote, no hosted CI run
  and no signing identity, and the project's plan records those as the owner's.
- **No platform qualification.** Which platforms have been executed, and which have only
  been cross-compiled, is stated in [known limitations](known-limitations.md) and
  [acceptance.md](acceptance.md) rather than assumed from an archive existing.
