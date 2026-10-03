//go:build !windows

package metrics

import (
	"os"
	"syscall"
)

// ProcessUsage is what the operating system says this process has used: resident memory
// and CPU time.
//
// Read from the process's own accounting rather than inferred from Go's heap, because the
// two answer different questions. A heap figure says how much memory the runtime has
// allocated; the resident set says how much the machine has actually given the process,
// and the gap between them is where a leak or a page-cache-heavy upload lives. The media
// subsystem runs in this process — there is no separate media worker to measure — so this
// is also the media figure.
func ProcessUsage() (residentBytes int64, cpuSeconds float64, ok bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0, false
	}
	user := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6
	system := float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	return residentBytesOf(&usage), user + system, true
}

// PID is the process id, so an operator can match a metrics reading against the process on
// the machine. Reading it is how somebody finds the container that is holding the port.
func PID() int { return os.Getpid() }
