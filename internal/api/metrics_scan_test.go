package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The directory-scan cost measurement (F-12, the plan's item 5): "Measure recursive
// directory-scan cost at the target file population". It is gated because it writes
// thousands of files, and it reports a measurement rather than a pass/fail.
//
//	BRRP_METRICS_SCAN_LOAD=1 go test ./internal/api -run TestStorageScanCostMeasurement -count=1 -v
//
// The cost is what the cache exists to keep off the scrape path. The population here is a
// stand-in for the reference profile's attachment corpus; the projection is stated as a
// projection, not as a measurement at that population.
func TestStorageScanCostMeasurement(t *testing.T) {
	if os.Getenv("BRRP_METRICS_SCAN_LOAD") != "1" {
		t.Skip("gated measurement; run with BRRP_METRICS_SCAN_LOAD=1")
	}
	directory := t.TempDir()
	// A few subdirectories, the way an upload directory is not one flat list forever.
	for bucket := range 8 {
		sub := filepath.Join(directory, fmt.Sprintf("b%d", bucket))
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const files = 20_000
	written := time.Now()
	for index := range files {
		path := filepath.Join(directory, fmt.Sprintf("b%d", index%8), fmt.Sprintf("file-%05d", index))
		if err := os.WriteFile(path, make([]byte, 1024), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeDuration := time.Since(written)

	// Measured twice: the first walk warms the directory entries, and an operator's
	// steady-state scrape cost is the second.
	var size int64
	start := time.Now()
	if _, err := directorySizeErr(directory); err != nil {
		t.Fatal(err)
	}
	firstWalk := time.Since(start)
	start = time.Now()
	size, err := directorySizeErr(directory)
	if err != nil {
		t.Fatal(err)
	}
	warmWalk := time.Since(start)

	t.Logf("storage scan measurement")
	t.Logf("  files:            %d (write took %s)", files, writeDuration.Round(time.Millisecond))
	t.Logf("  total bytes:      %d", size)
	t.Logf("  first walk:       %s", firstWalk.Round(time.Microsecond))
	t.Logf("  warmed walk:      %s (%.1f µs/file)", warmWalk.Round(time.Microsecond), float64(warmWalk.Microseconds())/files)
	t.Logf("  projection:       at 200,000 files a warmed walk would be about %s, which is why the scrape reads a cached measurement instead of walking",
		time.Duration(int64(warmWalk)/files*200_000).Round(time.Millisecond))
}
