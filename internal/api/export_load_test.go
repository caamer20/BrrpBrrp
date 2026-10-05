package api

import (
	"fmt"
	"os"
	"runtime"

	"brrpbrrp/internal/metrics"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// The export load measurement: what building a large permitted export costs the
// process, and whether ordinary chat keeps working while it runs.
//
// It is gated behind BRRP_EXPORT_LOAD=1 because it seeds tens of thousands of rows and
// is a measurement rather than a pass/fail check. Run it with:
//
//	BRRP_EXPORT_LOAD=1 go test ./internal/api -run TestExportLoadMeasurement -count=1 -v
//
// The numbers it reports are Go heap measurements from the same process that runs the
// build (the test server is in-process), not a serialized byte count relabelled as
// memory: the point of the finding was that a document's size is not a measure of what
// it cost to construct. It also times ordinary message sends while the build runs, so
// "the chat is still responsive" is a measured claim rather than an assumption.

const (
	exportLoadMessages = 50_000 // the message bound; the largest permitted document by row count
	exportLoadBody     = "a line of ordinary history for the export load measurement"
)

func TestExportLoadMeasurement(t *testing.T) {
	if os.Getenv("BRRP_EXPORT_LOAD") != "1" {
		t.Skip("gated measurement; run with BRRP_EXPORT_LOAD=1")
	}
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "export-load-room")
	authorID := memberIDOf(t, s, "admin")

	base := time.Now().UTC()
	seeded := time.Now()
	for index := range exportLoadMessages {
		if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
			VALUES(?,?,?,?,?,?,?)`, db.ID(), channel, channel, authorID, exportLoadBody,
			db.Timestamp(base.Add(time.Duration(index)*time.Millisecond)), fmt.Sprintf("load-%d", index)); err != nil {
			t.Fatalf("seed row %d: %v", index, err)
		}
	}
	seedDuration := time.Since(seeded)

	_, alice := namedUser(t, s, "export_load_alice")
	markPrivileged(t, s, alice)

	var baseline runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&baseline)

	// Sample the heap while the build runs, so the number reported is the peak during
	// construction rather than the state before or after it.
	var (
		mu       sync.Mutex
		peakHeap uint64
		stop     = make(chan struct{})
		done     = make(chan struct{})
	)
	go func() {
		defer close(done)
		var stats runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&stats)
			mu.Lock()
			if stats.HeapAlloc > peakHeap {
				peakHeap = stats.HeapAlloc
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Ordinary chat latency while the build runs: send messages and record the slowest.
	// The build runs in the same process, so a send that stalls behind it is visible.
	buildStart := time.Now()
	markPrivileged(t, s, alice)
	w := request(t, handler, "POST", "/api/me/export", map[string]any{"operation_key": "export-load-key"}, alice)
	if w.Code != 202 {
		t.Fatalf("request export: %d %s", w.Code, w.Body.String())
	}
	record := readJSON[exportRecord](t, w)

	var (
		slowestSend time.Duration
		sendCount   int
	)
	sendDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(sendDeadline) {
		start := time.Now()
		sent := request(t, handler, "POST", "/api/channels/"+channel+"/messages",
			map[string]any{"content": "chat during the build", "idempotency_key": fmt.Sprintf("during-%d", sendCount)}, admin)
		elapsed := time.Since(start)
		if sent.Code == 201 && elapsed > slowestSend {
			slowestSend = elapsed
		}
		sendCount++
		var state string
		if err := s.store.QueryRow(`SELECT state FROM data_exports WHERE id=?`, record.ID).Scan(&state); err == nil && state != "building" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	close(stop)
	<-done
	buildDuration := time.Since(buildStart)

	var buildState string
	var bytesWritten int64
	var messages int
	if err := s.store.QueryRow(`SELECT state,bytes_written,message_count FROM data_exports WHERE id=?`, record.ID).
		Scan(&buildState, &bytesWritten, &messages); err != nil {
		t.Fatal(err)
	}

	t.Logf("export load measurement")
	t.Logf("  seeded rows:        %d in %s", exportLoadMessages, seedDuration.Round(time.Millisecond))
	t.Logf("  build state:        %s", buildState)
	t.Logf("  build duration:     %s", buildDuration.Round(time.Millisecond))
	t.Logf("  messages exported:  %d", messages)
	t.Logf("  document bytes:     %d (%.1f MiB)", bytesWritten, float64(bytesWritten)/(1<<20))
	t.Logf("  go heap before:     %d MiB", baseline.HeapAlloc/(1<<20))
	t.Logf("  go heap peak:       %d MiB", peakHeap/(1<<20))
	// The process's own high-water resident set, measured the way the metrics endpoint
	// measures it — not a serialized byte count relabelled as memory. Maxrss is a
	// high-water mark for the process's lifetime, so it is an upper bound on this
	// build's cost rather than the build's own figure; it is still the honest number an
	// operator would see.
	if rss, _, ok := metrics.ProcessUsage(); ok {
		t.Logf("  process peak RSS:   %d MiB (process lifetime high-water)", rss/(1<<20))
	}
	t.Logf("  slowest chat send during the build: %s over %d sends", slowestSend.Round(time.Millisecond), sendCount)

	if buildState != "ready" && buildState != "failed" {
		t.Fatalf("the export never finished: %s", buildState)
	}
	if bytesWritten > maxExportBytes {
		t.Fatalf("the export exceeded its byte bound: %d", bytesWritten)
	}
}
