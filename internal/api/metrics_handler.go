package api

import (
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"brrpbrrp/internal/metrics"
)

// metricsReport is the machine-readable view of the server: request counts and latencies
// per route, the process's own memory and CPU, what the media path has forwarded, and the
// jobs and storage an operator watches.
//
// It is behind SettingsManage like the rest of the administrative surface, and it carries
// no message content, no member names and no conversation identifiers. What it does carry
// is route *patterns* — a per-id label is how a metrics map becomes a memory leak, so the
// labels are the routes the server declares and nothing else.
//
// Every figure names what it measures. The GC pause fields say which window they cover,
// the storage figures carry the moment they were measured, and a platform that cannot
// report process counters says so rather than reporting zero.
func (s *Server) metricsReport(w http.ResponseWriter, r *http.Request, user db.User) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	resident, cpu, processOK := metrics.ProcessUsage()
	forwardedPackets, forwardedBytes, droppedPackets := s.voice.MediaCounters()
	freeDisk, diskErr := app.FreeDiskBytes(s.config.DataDir)

	storage := map[string]any{
		"free_disk_bytes": diskErr == nil,
		"database_bytes":  fileSize(s.config.DBFile),
		// The two directory totals come from the last measurement, not from a walk on
		// this request: a scrape must not pay a cost that grows with the installation.
		// The age travels with them.
		"measured":    false,
		"age_seconds": int64(0),
	}
	s.storageMu.Lock()
	footprint := s.storageFootprint
	s.storageMu.Unlock()
	if !footprint.MeasuredAt.IsZero() {
		storage["measured"] = true
		storage["measured_at"] = footprint.MeasuredAt.UTC().Format(time.RFC3339)
		storage["age_seconds"] = int64(time.Since(footprint.MeasuredAt).Seconds())
		storage["uploads_bytes"] = footprint.UploadsBytes
		storage["backups_bytes"] = footprint.BackupsBytes
	}
	if footprint.Err != "" {
		storage["measurement_error"] = footprint.Err
	}
	if diskErr != nil {
		storage["free_disk_error"] = diskErr.Error()
	} else {
		storage["free_bytes"] = freeDisk
	}

	report := map[string]any{
		"version":        app.Version,
		"uptime_seconds": s.metrics.UptimeSeconds(),
		"pid":            metrics.PID(),
		"routes":         s.metrics.Routes(),
		"process": map[string]any{
			// ok=false means this platform reports no process usage, which is said rather
			// than reported as zero megabytes of memory in use.
			"reported":       processOK,
			"resident_bytes": resident,
			"cpu_seconds":    cpu,
			"goroutines":     runtime.NumGoroutine(),
			// The Go runtime's own figures. go_heap_bytes is the live heap, not the
			// process's resident set; resident_bytes above is the resident set where the
			// platform reports it.
			"go_heap_bytes": memory.HeapAlloc,
			"go_sys_bytes":  memory.Sys,
			"go_gc_cycles":  memory.NumGC,
			// The runtime keeps the most recent 256 pauses in a ring. Both fields name
			// their window: the latest pause, and the maximum inside that ring over the
			// pauses that have actually happened. The earlier field was called
			// "go_gc_pause_ns_max" and reported the latest pause.
			"go_gc_pause_ns_latest":       memory.PauseNs[(memory.NumGC+255)%256],
			"go_gc_pause_ns_max_last_256": gcPauseMax(&memory),
		},
		"media": map[string]any{
			// The SFU runs in this process (there is no separate media worker), so its
			// memory and CPU are the process figures above; these are what it has done.
			"voice_users":       len(s.voice.States()),
			"forwarded_packets": forwardedPackets,
			"forwarded_bytes":   forwardedBytes,
			"dropped_packets":   droppedPackets,
		},
		"connections": s.hub.Count(),
		"storage":     storage,
		"jobs": map[string]any{
			"backup": s.backupStatus(),
		},
	}
	respond(w, 200, report)
}

// gcPauseMax is the largest pause inside the runtime's 256-entry ring, over the pauses
// that have actually happened. It is a defined-window maximum: the window is the ring,
// and a process younger than 256 collections is measured over its whole life.
func gcPauseMax(memory *runtime.MemStats) uint64 {
	window := uint64(memory.NumGC)
	if window > uint64(len(memory.PauseNs)) {
		window = uint64(len(memory.PauseNs))
	}
	var largest uint64
	for index := range window {
		if memory.PauseNs[index] > largest {
			largest = memory.PauseNs[index]
		}
	}
	return largest
}

// storageMeasurement is the last measured on-disk size of the directories an operator
// watches, with the moment it was taken and any error the walk hit.
//
// The two directories are walked by a bounded-cadence worker rather than by the scrape.
// A recursive walk costs more as an installation grows — tens of thousands of small
// attachment files — and a metrics endpoint scraped every fifteen seconds must not pay
// that on every request. The measurement's own time travels with it, so a value that has
// gone stale is visibly stale rather than silently presented as current.
type storageMeasurement struct {
	UploadsBytes int64
	BackupsBytes int64
	MeasuredAt   time.Time
	Err          string
}

// storageMeasurementInterval is the cache's bound. It is short enough that the figure
// tracks an installation hour to hour and long enough that the walk is a rounding error
// against the work it describes.
const storageMeasurementInterval = 5 * time.Minute

// measureStorageFootprint walks the trees once and records what it found.
func (s *Server) measureStorageFootprint() {
	uploads, uploadsErr := directorySizeErr(s.config.UploadDir)
	backups, backupsErr := directorySizeErr(filepath.Join(s.config.DataDir, "backups"))
	measurement := storageMeasurement{
		UploadsBytes: uploads,
		BackupsBytes: backups,
		MeasuredAt:   time.Now(),
	}
	if uploadsErr != nil {
		measurement.Err = "uploads: " + uploadsErr.Error()
	} else if backupsErr != nil {
		measurement.Err = "backups: " + backupsErr.Error()
	}
	s.storageMu.Lock()
	s.storageFootprint = measurement
	s.storageMu.Unlock()
}

// storageSizesWorker keeps the measurement current on a bounded cadence, outside every
// request path.
func (s *Server) storageSizesWorker() {
	defer s.workers.Done()
	s.measureStorageFootprint()
	ticker := time.NewTicker(storageMeasurementInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.measureStorageFootprint()
		}
	}
}
