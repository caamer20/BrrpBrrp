package metrics

import (
	"os"
	"testing"
	"time"
)

// The process figures are read from the operating system, so what a test can establish is
// that they are real: a running process has resident memory and a pid, or the platform has
// said it cannot answer and said so.
func TestProcessUsageIsReportedOrDeclaredUnavailable(t *testing.T) {
	resident, cpu, ok := ProcessUsage()
	if PID() != os.Getpid() {
		t.Fatalf("PID() = %d, want %d", PID(), os.Getpid())
	}
	if !ok {
		t.Skip("this platform reports no process usage")
	}
	if resident <= 0 {
		t.Fatalf("resident bytes = %d, want a positive figure", resident)
	}
	if cpu < 0 {
		t.Fatalf("cpu seconds = %v, want a non-negative figure", cpu)
	}
	// CPU time is spent by the goroutines a test starts; nothing else needs to be true
	// about it, and asserting a number would be asserting the scheduler.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, again, _ := ProcessUsage()
		if again >= cpu {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cpu seconds went backwards: %v then %v", cpu, func() float64 { _, c, _ := ProcessUsage(); return c }())
}
