package api

import (
	"database/sql"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

func (s *Server) purgeChannel(w http.ResponseWriter, r *http.Request, user db.User) {
	channel, err := s.channel(r.PathValue("id"))
	if !s.found(w, err) {
		return
	}
	var input struct {
		Days    int    `json:"older_than_days"`
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Days < 0 || input.Days > 36500 || input.Confirm != channel.Name {
		problem(w, 400, "Confirm the channel name and choose a valid age.")
		return
	}
	cutoff := db.Timestamp(time.Now().Add(-time.Duration(input.Days) * 24 * time.Hour))
	var count int64
	if !s.mutateReply(w, r, user, authz.ChannelManage, func(tx *sql.Tx, principal authz.Principal) error {
		result, err := tx.ExecContext(r.Context(), `DELETE FROM messages WHERE channel_id=? AND created_at<?`, channel.ID, cutoff)
		if err != nil {
			return err
		}
		count, _ = result.RowsAffected()
		return s.auditTx(r.Context(), tx, user.ID, "channel.purged", channel.ID)
	}) {
		return
	}
	// The refresh signal is addressed like every other channel event.
	s.publishChannelChange(r.Context(), channel, "channel.updated", "A channel's history was purged.")
	respond(w, 200, map[string]int64{"deleted": count})
}

func (s *Server) maintenanceWorker() {
	defer s.workers.Done()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	backupTicker := time.NewTicker(time.Minute)
	defer backupTicker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-backupTicker.C:
			s.scheduledBackup(now)
		case <-ticker.C:
			if err := s.maintain(time.Now()); err != nil {
				s.log.Warn("maintenance did not finish", "error", err)
			}
		}
	}
}

func (s *Server) maintain(now time.Time) error {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	settings := s.Settings()
	if settings.Purge && settings.RetentionDays > 0 {
		cutoff := db.Timestamp(now.Add(-time.Duration(settings.RetentionDays) * 24 * time.Hour))
		if _, err := s.store.ExecContext(s.ctx, `DELETE FROM messages WHERE created_at<?`, cutoff); err != nil {
			return err
		}
		s.hub.Broadcast("reconnect.required", map[string]bool{"refetch": true})
	}
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM sessions WHERE expires_at<?`, db.Timestamp(now)); err != nil {
		return err
	}
	orphanCutoff := db.Timestamp(now.Add(-24 * time.Hour))
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM invitations WHERE expires_at<?`, orphanCutoff); err != nil {
		return err
	}
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM attachments WHERE message_id IS NULL AND created_at<?`, orphanCutoff); err != nil {
		return err
	}
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM audit_logs WHERE created_at<?`, db.Timestamp(now.AddDate(0, 0, -90))); err != nil {
		return err
	}
	// Compact old deleted receipts in bounded batches, retaining a minimal spent key
	// for the account's lifetime. A late client retry must never resurrect erased content.
	if _, err := s.store.PruneSendReceipts(s.ctx, s.store, db.Timestamp(now.Add(-receiptPruneAfter))); err != nil {
		return err
	}
	// Expired report evidence is unavailable immediately in the API and removed
	// here in a bounded batch. Backups follow their separate retention policy.
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM message_reports WHERE rowid IN (SELECT rowid FROM message_reports WHERE expires_at<=? ORDER BY expires_at LIMIT 500)`, db.Timestamp(now)); err != nil {
		return err
	}
	files, err := os.ReadDir(s.config.UploadDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		if file.IsDir() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) < 24*time.Hour {
			continue
		}
		var count int
		if err := s.store.QueryRowContext(s.ctx, `SELECT count(*) FROM attachments WHERE stored_name=?`, file.Name()).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			path, err := app.Contained(s.config.DataDir, filepath.Join(s.config.UploadDir, file.Name()))
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func fileSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}
func directorySize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			size += fileSize(path)
		}
		return nil
	})
	return size
}
