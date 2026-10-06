//go:build windows

package metrics

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessUsage on Windows reports the process's own resident set and CPU time, so an
// operator's dashboard reads the same facts on every supported host. The earlier version
// returned unavailable here, which left Go heap as the only figure a Windows operator
// could see — and the Go heap is not the process's memory.
//
// The resident set comes from GetProcessMemoryInfo (psapi), which x/sys/windows does not
// wrap; the CPU time comes from GetProcessTimes, which it does.
var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS. The first two fields are DWORD
// and the rest are SIZE_T, which places every pointer-sized field on its natural
// alignment on both 32- and 64-bit Windows.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
}

// ProcessUsage reports the resident set in bytes and the CPU time in seconds consumed by
// this process. ok is false when either counter cannot be read: a partial figure would be
// a claim about a number that was never measured.
func ProcessUsage() (residentBytes int64, cpuSeconds float64, ok bool) {
	handle, err := windows.GetCurrentProcess()
	if err != nil {
		return 0, 0, false
	}
	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))
	if result, _, _ := getProcessMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb)); result == 0 {
		return 0, 0, false
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, 0, false
	}
	cpu := float64(kernel.Nanoseconds()+user.Nanoseconds()) / 1e9
	return int64(counters.workingSetSize), cpu, true
}

func PID() int { return os.Getpid() }
