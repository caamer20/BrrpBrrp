//go:build windows

package app

import (
	"errors"

	"golang.org/x/sys/windows"
)

// IsDiskFull reports whether err says the filesystem could not take the write because
// it has no room left.
//
// Windows does not return the Unix ENOSPC value: a write that finds the volume full
// fails with ERROR_DISK_FULL (or ERROR_HANDLE_DISK_FULL when the handle itself cannot
// extend the file). Both mean the same thing to the caller, so both are recognised here
// and the upload path answers them with the same 507 it answers on Unix.
func IsDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) ||
		errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
