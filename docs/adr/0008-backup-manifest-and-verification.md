# ADR 0008 — Backup manifest, inventory and verification

- **Status:** accepted
- **Date:** 24 September 2026
- **Context:** v1 plan §6.3 (backup format versioning), WP-23 (trustworthy backups and recoverability), WP-04.

## Decision

A backup archive carries a versioned manifest that describes what it contains, and every reader checks the archive against it.

**Format 2** records, next to the archive layout itself:

- the application version and the database schema version that produced it,
- a `database` inventory entry, and one entry per archived attachment,
- for each entry, its stored name, its uncompressed size and the SHA-256 digest of its uncompressed bytes.

The digest is the part that matters. A ZIP already carries per-entry CRC-32 checksums, but those only show that the compressed bytes were not corrupted in transit; they say nothing about whether the archive a restore is about to install is the archive that was verified, and they cannot notice a re-compressed or re-written entry. A recorded digest can, and it is also the identity an operator compares between two copies.

The manifest is written **last**, because the inventory is only known once every entry has been streamed and hashed, so recording an entry costs a single pass over each file.

**Format versioning is separate from the database schema and the application version.** Format 1 archives carry no inventory and remain readable: structure, size limits, ZIP checksums and the database's own integrity and reference checks still apply, and `verify-backup` says plainly that per-file digests could not be compared. A format 2 binary therefore restores both, and nothing in the format promises that an older binary can read a newer archive.

## Consequences

**Restore changed shape.** Structure, manifest agreement and size budget are validated before anything is written, and each entry is hashed as it lands in the staging directory, so a touched archive cannot be installed and a rejected restore still creates no database. The staging-and-move commit is unchanged, including on Windows. An archive whose schema is **older** than the running build is a valid recovery point and is migrated by the restore; one whose schema is **newer** is refused as "cannot restore it", because the older binary cannot interpret it (ADR 0002).

**The recorded schema is the schema inside the archive.** A manifest describes the snapshot, so the version it records is read from the archived database itself (`db.OpenWithoutMigrating`), not from the binary that happens to be running. A candidate therefore reports an alpha.3 archive as schema 5 rather than stamping its own schema onto it, which is what makes a pre-upgrade archive a usable rollback point after an upgrade.

**An archive can be checked without being restored.** `brrpbrrp verify-backup --file ARCHIVE.zip` reads an archive anywhere on disk, needs no data directory and no lock, and reports the format, creation time, versions, entry counts and sizes, and the archive's own SHA-256. It exits non-zero if any entry disagrees with its recorded size or digest, if the manifest does not account for exactly the entries present, or if the database fails its integrity or reference checks. This is the check for a copy that has been sitting on separate storage, which is the copy that actually matters after a loss.

**A copy is only a copy when it matches.** `copyBackup` hashes while it streams to `BRRP_BACKUP_COPY_DIR` and compares the result with the archive that was just written; a mismatch removes the partial copy and is reported as a failed copy, separately from local backup success. The local digest and the copy digest are recorded in the backup status and shown in **Manage server → Maintenance**, so the identity of the newest archive and of its extra copy is visible rather than assumed.

**Monitoring depends on integrity, not existence.** `production-check` verifies the newest local archive and the newest extra copy, not just their age, so a truncated or altered archive fails the check instead of satisfying it as a fresh file. A monitor that only dates files cannot tell a readable backup from an unusable one. Verification walks the whole archive, so on a large installation the scheduled check costs one read of the newest archive and its copy; set `--verify-backup=false` only if that cost is genuinely unacceptable, and then do not claim integrity monitoring.

## What this does not cover

- **Confidentiality and encryption.** Archives contain account password hashes and push keys and are not encrypted; the operator's separate encrypted, off-host storage is the answer, and it stays out of scope here.
- **Host configuration and TLS material**, which are still exported separately.
- **An off-host restore into clean Windows and Docker destinations**, and RPO/RTO against a reference dataset. Those need the hosts WP-24 provides and remain unclaimed.
- **A dishonest filesystem or a rewritten archive with a rewritten manifest.** Digests prove an archive is the one that was written; they are not a signature and do not attest to who wrote it.

## Evidence

- `internal/backup/manifest_test.go`: the manifest records the application and schema versions and a correct size and digest for the database and the attachment; verification is stable; a rebuilt archive whose attachment content changed by one byte while keeping a correct ZIP checksum and an unchanged manifest fails verification and is refused by restore, which then creates no database; a manifest that lists an entry the archive does not contain, or omits one it does, is rejected; and a format 1 archive still verifies and restores.
- `internal/api/backup_schedule_test.go`: a recorded digest equals the archive on disk and the copy digest equals it; a copy whose digest disagrees with the archive is refused, leaves no published file, and a matching copy is byte-identical.
- `internal/app/production_test.go`: an archive or copy that fails verification fails `production-check` with `local backup integrity` or `backup copy integrity`, and verified archives pass.
- `make rehearsal` runs `verify-backup` on the archive it produces (format 2, schema 5, one attachment, printed SHA-256) and confirms that a single flipped byte makes the same command fail, before restoring that archive into an empty destination.
