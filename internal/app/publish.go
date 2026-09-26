package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// publishTempPrefix is the prefix every temporary publication file carries. It
// is a constant because two things depend on it: the name Publish creates, and
// the reaper that removes files an abrupt process death left behind.
const publishTempPrefix = ".publish-"

// ErrDirectorySyncUnsupported reports that the destination filesystem cannot
// synchronize a directory entry. Publication still succeeds — the rename is
// journaled by the filesystem, or the platform cannot offer this guarantee at
// all — but the durability of the *name* is then the filesystem's claim rather
// than this package's, so the caller gets a labelled limitation instead of a
// silent success indistinguishable from a completed directory sync.
//
// It is exported because the condition is a property of the volume, not a
// per-call failure: Windows always reports it (a directory handle cannot be
// flushed there), and Unix filesystems that answer EINVAL/ENOTSUP report it too.
var ErrDirectorySyncUnsupported = errors.New("publish: directory synchronization is unsupported on this filesystem")

// Publish writes a file so that it either appears complete at dir/name or does
// not appear at all, and the bytes are on disk before any database row may
// reference them.
//
// The order is the whole point of this function:
//
//  1. the data is written to a temporary file in the same directory, so the
//     final rename never crosses a filesystem boundary and never copies
//  2. that file is flushed to the device before it is given its final name
//  3. the temporary file is renamed onto the final name, which is atomic on the
//     supported platforms: a reader sees the whole file or no file at all
//  4. the directory itself is synchronized so the new name survives a power loss
//
// Only after Publish returns successfully may the caller record a reference to
// the file. A commit that promises a file which is not yet durable — or a final
// name that visible-but-partial — is the inconsistency this ordering removes.
//
// A returned error leaves neither the final name nor a temporary file behind, so
// a write error costs at most the incomplete upload.
//
// What each failure mode means, stated rather than assumed:
//
//   - **An ordinary write error** (full disk, permission, a failing callback): the
//     temporary file and the final name are both removed in this call.
//   - **Process death** (a kill or a crash): the deferred cleanup does not run, so
//     a `.publish-*` file can survive in dir. Nothing references it — the row that
//     would is written by the caller only after Publish returns — and
//     SweepStaleTemporaries removes it after a grace period.
//   - **Power loss**: the intermediate order is stated above. On a filesystem that
//     can synchronize a directory, the temporary file is gone and the final name
//     is on the device. Where directory synchronization is unsupported
//     (ErrDirectorySyncUnsupported) the directory may hold either name after a
//     power loss, and a `.publish-*` leftover is cleaned up by the same reaper.
//
// The *final-name* durability claim therefore carries the platform evidence in
// ADR 0007; this function guarantees the ordering, not the storage.
func Publish(dir, name string, write func(*os.File) error) (string, error) {
	if name == "" || filepath.Base(name) != name {
		return "", fmt.Errorf("publish: %q is not a plain file name", name)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("publish: %s already exists", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	temp, err := os.CreateTemp(dir, publishTempPrefix+"*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	published := false
	defer func() {
		if !published {
			_ = temp.Close()
			_ = os.Remove(tempName)
		}
	}()
	if err := write(temp); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempName, target); err != nil {
		return "", err
	}
	published = true
	if err := SyncDirectory(dir); err != nil && !errors.Is(err, ErrDirectorySyncUnsupported) {
		// The name exists but its durability is genuinely unknown, so nothing may
		// refer to it. Remove it and let the caller report the failure. An
		// unsupported directory sync is not that case: the volume's own journaling
		// is the claim there, and it is reported as a documented limit.
		_ = os.Remove(target)
		return "", err
	}
	return target, nil
}

// SweepStaleTemporaries removes publications that were abandoned before they were
// renamed, which is the residue of a process killed mid-write rather than of a
// handled failure. Only files whose name carries publishTempPrefix are touched,
// and only those last modified before the cutoff, so a write that is still in
// flight is never removed. It returns the number of files removed.
func SweepStaleTemporaries(dir string, before time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), publishTempPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if !info.Mode().IsRegular() || !info.ModTime().Before(before) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
