//go:build windows

package metrics

import "os"

// ProcessUsage on Windows reports what the Go runtime knows and nothing more: the resident
// set needs GetProcessMemoryInfo through golang.org/x/sys/windows, and no journey or
// rehearsal on Windows has needed it yet. Reporting zero with ok=false is the honest
// answer; inventing a number is not.
func ProcessUsage() (residentBytes int64, cpuSeconds float64, ok bool) {
	return 0, 0, false
}

func PID() int { return os.Getpid() }
