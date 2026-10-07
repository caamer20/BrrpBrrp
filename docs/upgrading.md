# Upgrading

How to move an installation forward, and what to do when you need to go back. Both halves
matter: a rollback that nobody rehearsed is a plan rather than a recovery. The operator
procedures are in [deployment.md](deployment.md#updates-and-rollback) for a native service
and [docker.md](docker.md) for a container; this page is the rule they both follow, plus
the alpha-to-1.0 path.

## The rule

**Migrations are forward-only.** A binary upgrades an older database in the order the
migration registry lists, in transactional stages, and refuses a database from a *newer*
schema instead of downgrading it. There is no downgrade. Going back means restoring the
pre-upgrade snapshot with the executable that matches that snapshot.

**A rollback needs two things kept together, and both are made before the upgrade:**

1. a verified backup of the *pre-upgrade* database and attachments, and
2. the executable that wrote it.

An archive of the already-upgraded database is not a rollback source, and neither is the new
executable pointed at an old snapshot. Verify the archive with the executable that will
restore it, and rehearse the restore on the destination you actually intend to keep, because
a temporary directory on the server cannot prove an off-host copy is readable.

**A rollback loses later writes.** Everything written after the snapshot — messages, uploads,
role changes, moderation decisions, sessions — is outside the recovery point. Quiesce writes
before taking the snapshot if that matters, and reconcile anything decided afterwards before
serving people again.

## The upgrade, step by step

1. **Read the record** for the version you are moving to, under
   [docs/releases](releases/0.4.0-alpha.9.md). Its "Known issues" and upgrade sections state
   what changed and what the checkpoint does not prove.
2. **Stop writing.** Scheduled jobs, uploads and browser sessions should not be running.
3. **Take the snapshot** with the executable currently in use, including attachments, and
   verify it. Keep it and that executable together, somewhere off the host.
4. **Inspect before changing anything.** `brrpbrrp preflight` reports the source and target
   schema, integrity, journal mode and pending migrations without touching the database, so
   the decision to proceed is made on the actual state rather than an assumed one.
5. **Update.** The wizard's **Update this installation** path does the supported sequence
   from the extracted new release: export a backup, confirm, restart. The manual path is to
   replace the executable, keep the data and configuration directories, and start the
   service. Docker follows its own documented procedure, and its backup service is separate.
6. **Confirm.** Check the log for the applied migration, sign in, read a stored message,
   search, download an attachment, and join voice. `brrpbrrp verify` and
   `production-check` answer the rest.

If the upgrade is interrupted, the database records that it is incomplete and the binary
refuses to serve until `brrpbrrp migrate` completes and verifies it. That refusal is the
recovery path working, not a new fault.

## Alpha to 1.0

Every 0.4.0-alpha checkpoint is a development tree rather than a release, and each carries
its own database schema:

| Version | Database |
| --- | --- |
| 0.4.0-alpha.5 | 10 |
| 0.4.0-alpha.6 | 12 |
| 0.4.0-alpha.7 | 21 |
| 0.4.0-alpha.8 | 25 |
| 0.4.0-alpha.9 | 25 |
| 1.0.0 | not built |

The path from an alpha installation to 1.0.0 is a supported forward upgrade, one step at a
time if you want the rollback points, in the order above. Two practical notes:

- **alpha.7 is the reference rollback baseline.** The rehearsed rollback crosses alpha.7
  (database 21) to the current tree (database 25) in one run, restoring the old snapshot
  with the old executable afterwards. alpha.8 and alpha.9 write the same schema as the
  current tree, so they cannot be a rollback baseline on their own — a snapshot of a
  database already at schema 25 is not an older version's recovery point.
- **Keep the alpha executable with its snapshot.** A later alpha does not read an earlier
  snapshot's schema, and an earlier alpha does not read a later one's.

**1.0.0 is not released.** Neither the archives, the container image nor the release notes
exist as anything but a draft, and no upgrade *to* 1.0.0 has been performed. This section
describes the path this project supports; it is not a record of one that was walked.

## What an upgrade does not need

Nothing in a normal upgrade changes `server.env`, the data directory layout, the
certificates or the backup destination. An installation that set a feature gate explicitly
keeps that setting; a gate whose *default* changes is a release note, not a silent switch.
If a release introduces a key, it is documented in [deployment.md](deployment.md) and the
example files before it is read.

A changed *default* is the same kind of thing. The shipped backup interval is now **12
hours**, the cadence the recovery objective of losing at most 24 hours assumes. An
installation that already stores an interval keeps it across the upgrade; if yours is
longer than 12 hours and you want the stated objective, set it to 12 in **Manage server →
Maintenance**.

A new installation also starts with scheduled backups **on**. That, too, is a default for
installations created from this release: one created before it keeps scheduled backups as
they were. If yours had them off, they are still off after the upgrade — switch them on in
**Manage server → Maintenance**, and give them a second destination
(`BRRP_BACKUP_COPY_DIR`; in Docker, `compose.backup.yaml`), because the recovery check
treats a backup kept only on the server's own machine as no recovery at all.

## If something goes wrong

- **The service will not start.** Read the log first: a refusal to serve an unfinished
  upgrade, a newer schema, a permissions problem on the data directory and a port already
  in use are four different sentences. `brrpbrrp preflight` distinguishes the schema ones.
- **You need to go back.** Stop the service, restore the pre-upgrade snapshot into a clean
  destination with the matching older executable, and point the service there. Do not point
  an older binary at the migrated database.
- **The browser shows "An update is ready".** That is the client half of the same
  discipline: it preserves unsent text, staged attachments and the current reply across the
  refresh, and it waits until voice has ended and pending sends and uploads have finished.
