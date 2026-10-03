// Package metrics records what an operator needs to see from outside the process: how
// long requests take, which of them fail, and what the media subsystem is doing.
//
// Nothing here is a dependency. A metrics endpoint that pulls in a collection framework
// is a metrics endpoint that changes the build for the sake of a scrape, and this product
// is one binary that a small community runs; the recorder is a map of counters and ten
// buckets per route, and the numbers are read by an operator or a probe script rather
// than by a time-series database.
package metrics

import (
	"sort"
	"strconv"
	"sync"
	"time"
)

// latencyBuckets are the upper bounds, in milliseconds, of the latency histogram. They are
// buckets rather than percentiles because a percentile needs every sample stored or a
// sketch to estimate one, and a bucket costs one counter. The reported p95 is the bucket
// it falls in, named so the caller cannot mistake it for an interpolated figure.
var latencyBuckets = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 10000}

type routeStats struct {
	// byClass is the count of responses in each of 1xx..5xx, by first digit.
	byClass   [6]int64
	inFlight  int64
	total     int64
	latencyMs [11]int64 // one per bucket, plus the bucket above the last bound
	sumMs     int64
	maxMs     int64
}

// Recorder accumulates per-route request statistics. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	routes  map[string]*routeStats
	started time.Time
}

func NewRecorder() *Recorder {
	return &Recorder{routes: map[string]*routeStats{}, started: time.Now()}
}

// Begin marks a request as in flight and returns the function that records its outcome.
//
// `pattern` must be the route's *pattern* ("GET /api/channels/{id}/messages") and never the
// request path: a per-id label is how one busy channel turns a metrics map into a memory
// leak, and a scrape into an unbounded read.
func (r *Recorder) Begin(pattern string) func(status int) {
	stats := r.stats(pattern)
	r.mu.Lock()
	stats.inFlight++
	stats.total++
	r.mu.Unlock()
	started := time.Now()
	return func(status int) {
		elapsed := time.Since(started)
		ms := float64(elapsed.Microseconds()) / 1000
		bucket := len(latencyBuckets)
		for index, bound := range latencyBuckets {
			if ms <= bound {
				bucket = index
				break
			}
		}
		class := status / 100
		if class < 0 || class > 5 {
			class = 0
		}
		r.mu.Lock()
		stats.inFlight--
		stats.byClass[class]++
		stats.latencyMs[bucket]++
		stats.sumMs += elapsed.Milliseconds()
		if elapsed.Milliseconds() > stats.maxMs {
			stats.maxMs = elapsed.Milliseconds()
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) stats(pattern string) *routeStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	stats, ok := r.routes[pattern]
	if !ok {
		stats = &routeStats{}
		r.routes[pattern] = stats
	}
	return stats
}

// Routes returns a snapshot, ordered by name so two reads of an unchanged server are
// byte-identical: a metrics page that reorders itself between scrapes is a diff nobody
// can read.
func (r *Recorder) Routes() []RouteSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RouteSnapshot, 0, len(r.routes))
	for pattern, stats := range r.routes {
		snapshot := RouteSnapshot{
			Route:       pattern,
			Total:       stats.total,
			InFlight:    stats.inFlight,
			SumMs:       stats.sumMs,
			MaxMs:       stats.maxMs,
			LatencyMs:   map[string]int64{},
			StatusClass: map[string]int64{},
		}
		for index, count := range stats.latencyMs {
			if count == 0 {
				continue
			}
			label := "over_10000"
			if index < len(latencyBuckets) {
				label = bucketLabel(latencyBuckets[index])
			}
			snapshot.LatencyMs[label] = count
		}
		for class := 1; class <= 5; class++ {
			if stats.byClass[class] > 0 {
				snapshot.StatusClass[string(rune('0'+class))+"xx"] = stats.byClass[class]
			}
		}
		snapshot.P95BucketMs = percentileBucket(stats.latencyMs, stats.total, 0.95)
		snapshot.Errors = stats.byClass[4] + stats.byClass[5]
		out = append(out, snapshot)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Route < out[b].Route })
	return out
}

// UptimeSeconds is how long the recorder has existed, which is the process's own uptime
// for the only recorder the server makes.
func (r *Recorder) UptimeSeconds() int64 {
	return int64(time.Since(r.started).Seconds())
}

// RouteSnapshot is one route's counters as they are read out.
type RouteSnapshot struct {
	Route       string           `json:"route"`
	Total       int64            `json:"total"`
	InFlight    int64            `json:"in_flight"`
	Errors      int64            `json:"errors"`
	SumMs       int64            `json:"sum_ms"`
	MaxMs       int64            `json:"max_ms"`
	P95BucketMs float64          `json:"p95_bucket_ms"`
	LatencyMs   map[string]int64 `json:"latency_ms"`
	StatusClass map[string]int64 `json:"status_class"`
}

func bucketLabel(bound float64) string {
	return "le_" + strconv.FormatFloat(bound, 'f', -1, 64)
}

// percentileBucket reports the upper bound of the bucket that the given quantile falls in,
// or the last bound when the quantile lands above it.
func percentileBucket(counts [11]int64, total int64, quantile float64) float64 {
	if total == 0 {
		return 0
	}
	target := int64(float64(total) * quantile)
	if target < 1 {
		target = 1
	}
	var seen int64
	for index, count := range counts {
		seen += count
		if seen >= target {
			if index < len(latencyBuckets) {
				return latencyBuckets[index]
			}
			return latencyBuckets[len(latencyBuckets)-1]
		}
	}
	return latencyBuckets[len(latencyBuckets)-1]
}
