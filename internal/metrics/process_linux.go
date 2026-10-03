//go:build linux

package metrics

import "syscall"

// Maxrss is reported in kilobytes on Linux.
func residentBytesOf(usage *syscall.Rusage) int64 { return usage.Maxrss * 1024 }
