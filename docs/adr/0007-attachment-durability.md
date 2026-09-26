# ADR 0007 — Attachment durability and publication order

- **Status:** accepted, with platform limits stated rather than assumed
- **Date:** 24 September 2026
- **Context:** v1 plan §6.3 (durability policy), §5.5 (conversation and message contract), WP-04 tail, WP-23.

## Decision

An uploaded file is written so that it is either complete at its final name or absent, and it is durable before any database row references it. `app.Publish` owns that order and both the upload and preview paths use it:

1. **Write to a temporary file in the destination directory.** The rename that follows can never cross a filesystem boundary, so publication is a rename and not a copy, and a reader looking for the final name finds nothing while the bytes are still arriving.
2. **Flush the file** with `Sync` before it is named. The bytes are on the device before the name exists, so a power loss cannot leave a short file under a name the application has already promised.
3. **Rename onto the final name.** Atomic on the supported platforms: an observer sees the whole file or no file.
4. **Synchronize the directory** so the new name itself survives a power loss. This is `fsync` on the directory handle on Linux and macOS. Where the volume cannot synchronize a directory entry, that is reported as `app.ErrDirectorySyncUnsupported`, not as a silent success (see "Platform limits" below).
5. **Only then insert the attachment row.** A commit never acknowledges a reference to a file that is not yet durable, and a handled failure removes both the temporary file and the final name, so an interrupted upload costs at most itself.

An attachment is bound to a message inside the message's own transaction (`UPDATE attachments SET message_id=? WHERE id=? AND uploader_id=? AND message_id IS NULL` inside the insert transaction), so a claim and the message that makes it are one commit. An unclaimed upload stays readable by its uploader alone.

The existing name check keeps publication from replacing another file, because `os.Rename` on Windows would otherwise overwrite the destination.

## What this guarantees

Each claim below names the failure mode it covers, because they are not interchangeable.

- **An ordinary write error** (full disk, a permission refusal, a failing callback): `Publish` returns an error and leaves neither the final name nor a temporary file. Nothing references the file; there is no database change to undo.
- **Application crash or kill, at any point** during the write: the final name never appears, so nothing can serve or reference a partial file. The temporary file is *not* removed, because the cleanup that removes it is deferred and a killed process does not run deferred functions. It is invisible to every read path, since authorization follows the `attachments` row, and it is removed by sweep after a grace period (`app.SweepStaleTemporaries`; the upload directory is covered by the hourly maintenance sweep, which removes any file older than 24 hours with no matching row, and the disposable preview directory reaps `.publish-*` entries itself).
- **Application crash between publication and the row insert**: the file exists with no owner. It is unreadable to everyone and the hourly sweep deletes it after a 24-hour grace period.
- **Power loss after a successful commit, on a filesystem that can synchronize a directory**: the database commit and the file it references are both on the device, in that order. Restoring a backup therefore never finds a referenced attachment missing, and `restore` validates every archived attachment for exactly this reason.
- **A missing file behind an existing row** (hostile filesystem, manual deletion): the download answers `404 File not found.` rather than serving something else.

## Platform limits

These are the parts of the claim that the code does not, and cannot, establish by itself. They are recorded here so that no reader infers more than was measured.

- **Windows directory-entry durability is unverified.** `SyncDirectory` is a no-op there — a directory handle cannot be flushed — and it now *says so* by returning `ErrDirectorySyncUnsupported` instead of `nil`. The reasoning it rests on is NTFS journaling the rename, which gives "either the temporary name or the final name after a power loss, never a partial file under the final name", and it is reasoning, **not** a measured result. No Windows power-loss test has been run.
- **Unix directory synchronization has tolerated cases.** `EINVAL`, `ENOTSUP` and `errors.ErrUnsupported` mean the volume cannot synchronize a directory entry at all; the rename is then journaled by the filesystem, or the guarantee is simply unavailable. That answer is reported as `ErrDirectorySyncUnsupported` (wrapped with the filesystem's own error) rather than being collapsed into success, and publication continues. Every other error — `EIO`, a read-only mount, a vanished directory — fails the upload. Network filesystems are in the tolerated group and are not a supported upload destination.
- **Non-NTFS Windows volumes** do not inherit the journaling argument.
- **Torn writes inside the filesystem's own cache**, and the durability of previews beyond their own rebuild, which is why the preview directory is disposable, capped and excluded from backups.
- **An abrupt process termination leaves a temporary file until the sweep runs.** Cleanup at the moment of death is not something a process can promise, so the contract is stated as "invisible immediately, removed within the grace period" rather than "leaves nothing".
- **Disk destruction, dishonest storage hardware, or the loss of the host.** Those are covered by the backup and RPO contract (WP-23, ADR 0008), not by a local commit. `BRRP_SQLITE_SYNCHRONOUS=FULL` (ADR 0002) makes the database side explicit; it does not make the filesystem honest.

Native host evidence (Windows service, Linux container, actual power loss) is required before the corresponding claim is made outside this document, and belongs to WP-24 qualification, which remains open.

## Evidence

- `internal/app/publish_test.go` covers the file-write, publication and directory-entry boundaries: the final name is absent while the writer runs, the temporary file is in the destination directory, an existing file is never replaced, a failed write leaves the directory exactly as it was, and a path-like name is refused. It also pins the two properties this ADR depends on and that were previously implicit: `TestDirectorySynchronizationIsExplicit` fails if `SyncDirectory` returns anything other than success or the stated `ErrDirectorySyncUnsupported`, so a swallowed filesystem error cannot pass unnoticed; and `TestAbandonedTemporaryPublicationsAreReaped` proves an old temporary file is removed while an in-flight write and a published file are left alone.
- `internal/api/uploads_test.go` covers the HTTP surfaces of the same boundaries: an aborted upload leaves no row and no file, an upload that cannot publish stores nothing, a row whose file is missing answers `404`, and the sweep removes both the unreferenced orphan and the temporary file an interrupted publication left behind, while keeping the claimed attachment and its bytes.
- `internal/api/thumbnails_test.go` (`TestInterruptedPreviewPublicationIsReaped`) covers the disposable preview directory, whose size budget counts only `.jpg` entries and would otherwise let one leftover temporary file accumulate per crash.
- `node scripts/upgrade-recovery.mjs` proves the end-to-end consequence on a disposable destination: message and attachment bytes written before a backup are byte-identical after a restore into an empty directory. It is a fresh-install recovery check on one machine and does not exercise power loss, a native Windows volume or an off-host destination.
