//go:build !windows

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// SyncDirectory flushes a directory entry so that a completed rename survives a
// power loss. POSIX systems support fsync on a directory handle; the filesystems
// that cannot synchronize one at all report EINVAL or ENOTSUP, and their rename
// is already journaled by the filesystem. That answer is not a failed upload, but
// it is not a completed synchronization either, so it is reported as
// ErrDirectorySyncUnsupported (wrapped, with the filesystem's own error) rather
// than silently collapsed into success. Every other error — EIO, a read-only
// mount, a vanished directory — is a real failure and is returned as one.
func SyncDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, errors.ErrUnsupported) {
			return fmt.Errorf("%w: %v", ErrDirectorySyncUnsupported, err)
		}
		return err
	}
	return nil
}
