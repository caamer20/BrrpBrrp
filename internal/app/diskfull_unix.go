//go:build !windows

package app

import (
	"errors"
	"syscall"
)

// IsDiskFull reports whether err says the filesystem could not take the write because
// it has no room left.
//
// A full disk is a property of the volume, not of the request, so the caller maps it to
// a storage refusal rather than a server fault. The Unix error value differs by
// platform — ENOSPC on Linux and macOS, and both it and EDQUOT when a user quota is the
// reason — so the check names them here rather than at the call site.
func IsDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
