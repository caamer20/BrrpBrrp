package api

import (
	"net/http"
	"os"
	"path/filepath"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request, user db.User) {
	var input struct {
		Uploads bool `json:"include_uploads"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !s.backupMu.TryLock() {
		problem(w, 409, "A backup is already running.")
		return
	}
	defer s.backupMu.Unlock()
	id, err := s.runBackup(r.Context(), input.Uploads)
	if err != nil {
		s.failed(w, err)
		return
	}
	s.audit(user.ID, "backup.created", id)
	path := filepath.Join(s.config.DataDir, "backups", id+".zip")
	// A verified local archive whose independent copy failed is a success with a recorded
	// failure, not a silent one: the warning says what is missing and which generation is
	// still recoverable, and the same text is what the next scheduled attempt retries.
	status := s.backupStatus()
	respond(w, 201, map[string]any{"id": id, "size": fileSize(path), "url": "/api/admin/backups/" + id, "warning": status.Error, "recoverable_id": status.RecoverableID})
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request, user db.User) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		problem(w, 404, "Backup not found.")
		return
	}
	path, err := app.Contained(s.config.DataDir, filepath.Join("backups", id+".zip"))
	if err != nil {
		s.failed(w, err)
		return
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		problem(w, 404, "Backup not found.")
		return
	}
	if err != nil {
		s.failed(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		s.failed(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="brrpbrrp-`+id+`.zip"`)
	w.Header().Set("Content-Type", "application/zip")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
