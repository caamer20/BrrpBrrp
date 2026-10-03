package metrics

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// A route label must be a pattern, never a path. A per-id label is how one busy channel
// turns the metrics map into a memory leak, and this is the assertion that keeps the
// recorder's own documentation from being the only thing saying so.
func TestTheRouteLabelIsWhateverTheCallerPassesSoTheCallerMustPassThePattern(t *testing.T) {
	recorder := NewRecorder()
	done := recorder.Begin("GET /api/channels/{id}/messages")
	done(200)
	done = recorder.Begin("GET /api/channels/{id}/messages")
	done(404)
	snapshot := recorder.Routes()
	if len(snapshot) != 1 {
		t.Fatalf("routes = %d, want 1: %+v", len(snapshot), snapshot)
	}
	route := snapshot[0]
	if route.Route != "GET /api/channels/{id}/messages" {
		t.Fatalf("route label = %q", route.Route)
	}
	if route.Total != 2 || route.Errors != 1 {
		t.Fatalf("total/errors = %d/%d, want 2/1", route.Total, route.Errors)
	}
	if route.StatusClass["2xx"] != 1 || route.StatusClass["4xx"] != 1 {
		t.Fatalf("status classes = %+v, want one of each", route.StatusClass)
	}
	if route.InFlight != 0 {
		t.Fatalf("in flight = %d, want 0 once both are answered", route.InFlight)
	}
}

// The histogram is buckets, and the reported p95 is the bucket the quantile falls in. The
// test pins that reading rather than an interpolated percentile, because the label the
// snapshot uses says which one it is and the two must not drift apart.
func TestTheLatencyHistogramCountsEveryRequestOnceAndNamesItsBucket(t *testing.T) {
	recorder := NewRecorder()
	for index := 0; index < 20; index++ {
		done := recorder.Begin("POST /api/channels")
		time.Sleep(time.Millisecond)
		done(201)
	}
	route := recorder.Routes()[0]
	if route.Total != 20 {
		t.Fatalf("total = %d, want 20", route.Total)
	}
	var counted int64
	for _, count := range route.LatencyMs {
		counted += count
	}
	if counted != 20 {
		t.Fatalf("the buckets hold %d requests, want 20", counted)
	}
	if route.P95BucketMs <= 0 {
		t.Fatalf("p95 bucket = %v, want a bound", route.P95BucketMs)
	}
	if _, ok := route.LatencyMs["le_"+format(route.P95BucketMs)]; !ok {
		t.Fatalf("the p95 bucket %v is not one of the reported buckets: %+v", route.P95BucketMs, route.LatencyMs)
	}
	if route.MaxMs < 0 {
		t.Fatalf("max ms = %d", route.MaxMs)
	}
}

func TestAnInFlightRequestIsVisibleWhileItRuns(t *testing.T) {
	recorder := NewRecorder()
	done := recorder.Begin("GET /api/me")
	if route := recorder.Routes()[0]; route.InFlight != 1 {
		t.Fatalf("in flight = %d while the request is open, want 1", route.InFlight)
	}
	done(200)
	if route := recorder.Routes()[0]; route.InFlight != 0 {
		t.Fatalf("in flight = %d after the request, want 0", route.InFlight)
	}
}

// The snapshot is read by a probe and diffed by a person, so an unchanged server must
// serialize identically twice.
func TestTheSnapshotIsStableAndSerializes(t *testing.T) {
	recorder := NewRecorder()
	for _, route := range []string{"GET /api/me", "GET /api/state", "GET /api/me"} {
		recorder.Begin(route)(200)
	}
	first, err := json.Marshal(recorder.Routes())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(recorder.Routes())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("two reads differ:\n%s\n%s", first, second)
	}
	if recorder.UptimeSeconds() < 0 {
		t.Fatalf("uptime = %d", recorder.UptimeSeconds())
	}
}

// Concurrent requests to the same route and to different routes, because the recorder is
// on the request path of every endpoint the server has.
func TestTheRecorderIsSafeUnderConcurrentUse(t *testing.T) {
	recorder := NewRecorder()
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for index := 0; index < 50; index++ {
				route := "GET /api/me"
				if index%2 == 0 {
					route = "GET /api/state"
				}
				recorder.Begin(route)(200)
				_ = recorder.Routes()
			}
		}(worker)
	}
	group.Wait()
	var total int64
	for _, route := range recorder.Routes() {
		total += route.Total
	}
	if total != 400 {
		t.Fatalf("total recorded = %d, want 400", total)
	}
}

func format(bound float64) string {
	return bucketLabel(bound)[len("le_"):]
}
