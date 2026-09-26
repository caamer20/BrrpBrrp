package api

import (
	"net/http"
	"strings"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request, user db.User) {
	free, err := app.FreeDiskBytes(s.config.DataDir)
	storage := ""
	if err != nil {
		storage = "The server could not read available disk space."
	} else if free < 1<<30 {
		storage = "Less than 1 GB remains. Export backups and free storage before uploading more files."
	}
	respond(w, 200, map[string]any{
		"https_configured": strings.HasPrefix(s.config.BaseURL, "https://"),
		"local_only":       s.config.TLSMode == "http-local",
		"base_url":         s.config.BaseURL, "behind_proxy": s.config.BehindProxy,
		"voice_ip": s.config.VoiceExternalIP, "voice_port_min": s.config.VoicePortMin, "voice_port_max": s.config.VoicePortMax,
		"turn_enabled":    s.Settings().TURN,
		"free_disk_bytes": free, "storage_warning": storage,
		"backups_bytes": directorySize(s.config.DataDir + "/backups"),
		"backup":        s.backupStatus(), "copy_configured": s.config.BackupCopyDir != "",
	})
}
