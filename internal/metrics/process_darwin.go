//go:build darwin

package metrics

import "syscall"

// Maxrss is reported in bytes on Darwin, unlike Linux's kilobytes. The two are not the
// same unit and reading one as the other is a factor of 1024 in an operator's head.
func residentBytesOf(usage *syscall.Rusage) int64 { return usage.Maxrss }
