//go:build windows

package app

// SyncDirectory reports that Windows cannot synchronize a directory entry. A
// directory handle cannot be flushed there, so there is nothing to do, but the
// answer is returned as ErrDirectorySyncUnsupported rather than nil: the caller
// must not mistake "this volume cannot offer the guarantee" for "the name is
// durable", and the difference is what ADR 0007 records as a platform limit.
//
// The consequence for this contract: after a power loss the directory holds
// either the temporary name or the final name — NTFS journals the rename, so a
// partial file never appears under the final name — and the temporary name may
// remain. Publication does not fail for that; the residue is removed by
// SweepStaleTemporaries, and the existing name check keeps the rename from
// replacing another file, because os.Rename on Windows would otherwise overwrite
// the destination.
func SyncDirectory(string) error {
	return ErrDirectorySyncUnsupported
}
