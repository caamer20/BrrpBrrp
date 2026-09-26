# ADR 0002 — Schema migration, preflight verification and durability

**Status:** Implemented (schema 5). Durability default needs owner ratification
**Date:** 24 September 2026
**Plan reference:** D-16, WP-04, §6.1–6.3, INV-06
**Deciders needed:** OWNER + OPS (ratify the FULL default after measuring the
reference storage; confirm the stated filesystem assumptions)

## Context

The previous upgrade path was one function that opened the database, applied all
schema text and returned. Three properties the plan requires were missing:

- There was no way to inspect an old schema **without migrating it**, so an
  operator could not take a verified pre-upgrade snapshot with the old binary
  after the new binary had already rewritten the file. (Closed by decision 8;
  before that fix, `backup` and `verify` opened the database through `db.Open`,
  which applies pending migrations, so a "pre-upgrade snapshot" taken with the new
  binary had already rewritten the file it was meant to preserve.)
- Interrupted migrations were not obviously recoverable, because each migration
  recorded its own version inside its own statement list.
- A failed migration could leave the schema advanced while the postconditions
  (search-index rebuild and invariant verification) never ran, and a later open
  with no pending migrations would skip them entirely. (Closed by decision 9.)
- `synchronous=NORMAL` was the only durability setting, and the plan proposes FULL
  with the difference documented rather than assumed.

FTS5 added a fourth problem: the search index is an external-content table, so a
missing index entry is indistinguishable from an absent message. FTS5's own
`integrity-check` cannot see content drift.

## Decision

1. **One ordered registry.** `db.Migrations` is append-only; shipped entries are
   never edited. `SchemaVersion` is derived from the registry rather than being a
   second constant that can disagree with it.
2. **All pending migrations run in one transaction**, each recording its own
   version in that transaction. A failure rolls the whole upgrade back, so "half
   applied" is not a state an operator has to reason about.
3. **`preflight` never migrates.** `db.Inspect` reads the schema version, recorded
   history, integrity, journal mode, effective durability, row counts and file
   size over a connection that performs no schema or pragma change, and never
   creates a missing file. The `brrpbrrp preflight` command adds free space and
   refuses a database whose schema is newer than the binary.
4. **Downgrade refusal is explicit.** A newer schema returns `UpgradeError`, which
   names both versions and tells the operator to reinstall the newer version or
   restore a pre-upgrade backup. Restores and backups accept schema 1..current, so
   an alpha.3 archive still opens.
5. **The derived search index is rebuilt, not trusted.** Every migration that
   applies ends with `INSERT INTO message_fts(message_fts) VALUES('rebuild')`,
   which regenerates the index from surviving messages without touching message
   rows, IDs or ordering. `RebuildSearchIndex` is exported so a restore or an
   operator can do the same deliberately.
6. **Verification is a separate, stated act.** `db.Verify` checks the recorded
   history is complete and contiguous, that no foreign key is broken, and that the
   index structure is sound; `brrpbrrp verify` runs it explicitly.
7. **Durability defaults to WAL + `synchronous=FULL`**, configurable with
   `BRRP_SQLITE_SYNCHRONOUS=NORMAL` for disposable installations. `preflight`
   reports the effective level by name so a report is a durability statement
   rather than a number.
8. **Only `Open`/`OpenWithOptions` apply schema changes.** Inspection and snapshot
   paths use `db.OpenWithoutMigrating`, which opens the existing file without
   applying migrations and without creating a missing one. This is the helper
   behind `brrpbrrp preflight`, `brrpbrrp verify` and `brrpbrrp backup`, and behind
   the schema the backup manifest records, which is therefore the schema *inside
   the snapshot* rather than the schema of the running binary.
   - `brrpbrrp verify` states its mutation semantics explicitly: it refuses to run
     when the schema is **newer** than the binary, when an upgrade is outstanding,
     and when the schema is **behind** — telling the operator to run
     `brrpbrrp migrate`, or to take a snapshot with `brrpbrrp backup` first, rather
     than migrating as a side effect of a read-only-seeming command.
   - It is deliberately *not* opened with SQLite `query_only`/`mode=ro`: `VACUUM
     INTO` (used for the snapshot) and the FTS `integrity-check` insert are
     classified as writes by SQLite, and a read-only connection failed with
     `attempt to write a readonly database (8)`. The read-only helper
     `db.OpenReadOnly` still exists for callers that only read.
   - The consequence is a stated boundary, not a hidden one: `backup` and `verify`
     do not mutate the schema, but they do open the file read-write and can write
     WAL/shm side files, so a snapshot must still be taken from a quiesced
     installation.
9. **An unfinished upgrade is a durable state, not an inference.** A migration
   records an `upgrade-state` row in the same transaction that advances the schema
   and clears it only after the search-index rebuild and invariant verification
   have run. `Migrate` reads that row *after* applying migrations, and any later
   open with the row still present fails with an actionable message until the
   upgrade is repaired and verified — including a run where there is nothing left
   to migrate. `db.Migrations` remains append-only (1..5).

## Consequences

- The acknowledgement contract is now statable: with FULL, an acknowledged commit
  survives an application crash, an operating-system crash and power loss on
  honest storage; with NORMAL, recent commits may be lost in the last two cases.
  Disk destruction and host loss remain covered by backup/RPO, not by a local
  commit.
- An upgrade costs a search-index rebuild proportional to stored messages. That is
  deliberate: it is the only way to establish consistency for an external-content
  index, and it happens once per schema change rather than on every start.
- Attachment durability is **not** solved here. A database commit still does not
  imply that a referenced upload was durably published. WP-04's file-boundary
  crash tests and WP-23's archive work remain open, and the acknowledgement
  contract must not claim otherwise until they pass.

## Outstanding

- No benchmark of FULL on the reference storage yet (WP-04/WP-26 evidence).
- No genuine old-to-new upgrade or rollback rehearsal. The local rehearsal
  (`scripts/upgrade-recovery.mjs`, `make rehearsal`) is a **fresh-install recovery**
  check — same build, one machine, second directory — and its "touched archive"
  step flips archive bytes rather than only the mtime of an old valid archive.
  A matching prior executable, a real schema-2 data folder and an off-host target
  are still required (WP-24), as is an automated pre-upgrade snapshot step in the
  installer (WP-23).
- The incomplete-upgrade state is exercised at the database level
  (`TestAnIncompleteUpgradeCannotBeSkipped`) and with an injected failure, not by
  killing a process mid-upgrade on a real host.
- Migration fixtures cover schemas 1..5 (`db.CreateSchemaFixture`); they do not yet
  include a real archive of each supported intermediate release beyond the alpha.3
  fixture.
