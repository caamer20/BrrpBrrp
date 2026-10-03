//go:build !windows

package app

import "golang.org/x/sys/unix"

// FreeSpace reports the bytes available to unprivileged writers on the filesystem that
// holds path.
//
// Bavail rather than Bfree: the blocks reserved for root are not available to this
// process, and counting them is how a server reports room it cannot use and then fails
// the write it promised.
func FreeSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
