package api

import (
	"net/http"
	"runtime"

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
func (s *Server) metricsReport(w http.ResponseWriter, r *http.Request, user db.User) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	resident, cpu, processOK := metrics.ProcessUsage()
	forwardedPackets, forwardedBytes, droppedPackets := s.voice.MediaCounters()
	freeDisk, diskErr := app.FreeDiskBytes(s.config.DataDir)
	report := map[string]any{
		"version":        app.Version,
		"uptime_seconds": s.metrics.UptimeSeconds(),
		"pid":            metrics.PID(),
		"routes":         s.metrics.Routes(),
		"process": map[string]any{
			// ok=false means this platform reports no process usage, which is said rather
			// than reported as zero megabytes of memory in use.
			"reported":           processOK,
			"resident_bytes":     resident,
			"cpu_seconds":        cpu,
			"goroutines":         runtime.NumGoroutine(),
			"go_heap_bytes":      memory.HeapAlloc,
			"go_sys_bytes":       memory.Sys,
			"go_gc_cycles":       memory.NumGC,
			"go_gc_pause_ns_max": memory.PauseNs[(memory.NumGC+255)%256],
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
		"storage": map[string]any{
			// A scrape reads filesystem sizes, which is cheap; it deliberately does not
			// count messages, because a metrics endpoint that runs a table count every
			// fifteen seconds is a metrics endpoint that costs more than it reports.
			"free_disk_bytes": diskErr == nil,
			"database_bytes":  fileSize(s.config.DBFile),
			"uploads_bytes":   directorySize(s.config.UploadDir),
			"backups_bytes":   directorySize(s.config.DataDir + "/backups"),
			"free_bytes":      freeDisk,
		},
		"jobs": map[string]any{
			"backup": s.backupStatus(),
		},
	}
	if diskErr != nil {
		report["storage"].(map[string]any)["free_disk_bytes"] = false
		report["storage"].(map[string]any)["free_disk_error"] = diskErr.Error()
	} else {
		report["storage"].(map[string]any)["free_disk_bytes"] = true
	}
	respond(w, 200, report)
}
