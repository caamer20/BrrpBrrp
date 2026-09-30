package api

import (
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/authz"
	"brrpbrrp/internal/db"
)

// Restricted review audit metadata outlives the evidence it describes. An audit
// record is administrative accountability: if it were reclaimed with the 90-day
// evidence, the only record that a review happened would vanish with the thing
// reviewed, and no later question about that review could be answered. Ordinary
// community audit entries keep the shorter default (ADR 0014).
const (
	auditLogRetention           = 90 * 24 * time.Hour
	restrictedAuditLogRetention = 400 * 24 * time.Hour
)

// Which audit events are restricted, and therefore which clock reclaims them, is
// decided in audit_classification.go. Keeping a second list here is exactly how the
// writer, the two readers and this pass came to disagree about `private_request.*`.

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
	// The operator's alerts come from this pass rather than from a timer of their own,
	// so the readings and the notifications describe the same moment. Without a
	// destination this raises nothing; the conditions are still readable on the status
	// surface, which is the point — an operator can see a problem without depending on a
	// webhook that may itself be what is broken.
	s.raiseCurrentAlerts()
	// The bytes behind a reclaimed attachment row are removed too, on the same pass and
	// with the same bound. The row is what records which file belonged to it.
	if _, err := s.reclaimUploadFiles(now); err != nil {
		return err
	}
	// A produced export's server copy has a lifetime (ADR 0019b). Keeping one account's
	// history on the server after the member has taken their copy is a liability that
	// grows with every export ever run, and the member can delete theirs sooner — but
	// an undeleted one must not survive its own stated lifetime either.
	if _, err := s.expireExports(s.ctx, s.store); err != nil {
		return err
	}
	// An export file with no row behind it: a crash between reclaiming the row and
	// unlinking the file, or an account erased by a path that predates the cleanup.
	// Every other sweep works from the table, so without this one a file with no row
	// is a file nothing ever reaches again.
	if _, err := s.sweepOrphanExports(24 * time.Hour); err != nil {
		return err
	}
	orphanCutoff := db.Timestamp(now.Add(-24 * time.Hour))
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM invitations WHERE expires_at<?`, orphanCutoff); err != nil {
		return err
	}
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM attachments WHERE message_id IS NULL AND created_at<?`, orphanCutoff); err != nil {
		return err
	}
	// Two ages, not one. Restricted review metadata is administrative
	// accountability and outlives the evidence it describes; ordinary community
	// audit entries do not. Reclaiming both on the same clock would delete the only
	// record that a private review ever happened (ADR 0014).
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM audit_logs WHERE created_at<? AND `+restrictedAuditNotLike("action"), db.Timestamp(now.Add(-auditLogRetention))); err != nil {
		return err
	}
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM audit_logs WHERE created_at<? AND `+restrictedAuditLike("action"), db.Timestamp(now.Add(-restrictedAuditLogRetention))); err != nil {
		return err
	}
	// Request evidence is reclaimed on the same 90-day window as message evidence: it
	// is disclosed content, and a reader loses access to it long before the bytes go,
	// so leaving the submitted introduction in the database forever is a retention
	// promise quietly not being kept (completion review 13, V13-08).
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM private_request_reports WHERE rowid IN (SELECT rowid FROM private_request_reports WHERE expires_at<=? ORDER BY expires_at LIMIT 500)`, db.Timestamp(now)); err != nil {
		return err
	}
	// A command receipt is compacted rather than deleted. The key has to stay spent for
	// the account's lifetime — deleting the row would let a late retry become new
	// work — so what goes is the payload: the outcome, the reason, the evidence
	// identity, the actor and the conversation. What is left is a guard that says only
	// "this key was used", which is what the replay path needs in order to refuse.
	if _, err := s.store.ExecContext(s.ctx, `UPDATE private_report_receipts
		SET outcome='spent', evidence_id='', source_id='', attempt_id='', reporter_id='', subject_id=''
		WHERE rowid IN (SELECT rowid FROM private_report_receipts WHERE expires_at<=? AND outcome<>'spent' ORDER BY expires_at LIMIT 500)`,
		db.Timestamp(now)); err != nil {
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
	// Private evidence expires on the same rule but is reclaimed separately, so
	// turning the private feature off never accelerates its deletion: the records
	// stay until this sweep reaches their own expiry, which is what "disabling a
	// feature is not erasure" has to mean in practice. Bounded batch, same as above.
	if _, err := s.store.ExecContext(s.ctx, `DELETE FROM private_message_reports WHERE rowid IN (SELECT rowid FROM private_message_reports WHERE expires_at<=? ORDER BY expires_at LIMIT 500)`, db.Timestamp(now)); err != nil {
		return err
	}
	// Account activity is reclaimed on its own 30-day clock, acknowledged rows
	// first, in bounded batches. It is deliberately not on the evidence clock: an
	// activity row is a catch-up affordance and evidence is a submitted record
	// someone may appeal (ADR 0015).
	if err := s.pruneActivity(now); err != nil {
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

// reclaimUploadFiles removes the files behind attachments that no longer have a row.
//
// The row sweep deletes an attachment whose message is gone, and the row is what
// records which file belonged to it — so without this the bytes stay on disk forever.
// That is not a cosmetic leak: a server whose uploads are reclaimed in the database
// and never on the filesystem fills its disk while reporting free space, and the
// operator has no way to tell why.
//
// The sweep is bounded and it is *file-first*: a file is only removed once no row
// claims it, so a slow read cannot lose a file somebody is being served. And it
// tolerates a missing file, because the interesting case is the reverse — a row gone
// and a file left — not a file gone and a row remaining.
func (s *Server) reclaimUploadFiles(now time.Time) (int, error) {
	entries, err := os.ReadDir(s.config.UploadDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	// Anything a row still claims is somebody's, whatever its age.
	claimed := map[string]bool{}
	rows, err := s.store.QueryContext(s.ctx, `SELECT stored_name FROM attachments`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, err
		}
		claimed[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// A young unclaimed file is a staged upload somebody is about to attach. Removing
	// it immediately would delete a file between the upload response and the message
	// that claims it.
	grace := now.Add(-24 * time.Hour)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || claimed[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(grace) {
			continue
		}
		if err := os.Remove(filepath.Join(s.config.UploadDir, entry.Name())); err == nil {
			removed++
		} else if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("could not remove an unclaimed upload", "error", err, "file", entry.Name())
		}
	}
	return removed, nil
}

// sweepOrphanExports removes export files with no row behind them.
//
// A crash between reclaiming a row and unlinking its file leaves a file the account
// table can no longer name and no other sweep can reach, because every other sweep
// works from the table. It is a copy of somebody's history with no owner, so it is
// removed on the same grace period as an unclaimed upload.
//
// The grace matters in one direction only: a file younger than it may belong to an
// export whose row committed moments ago and whose file is being written, so a file
// that recent is left alone rather than unlinked under a live build.
func (s *Server) sweepOrphanExports(grace time.Duration) (int, error) {
	directory := filepath.Join(s.config.DataDir, "exports")
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-grace)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// A partial file is a build in progress or a build that failed; the build
		// cleans its own, and one left by a crash is removed the same way.
		id := strings.TrimSuffix(strings.TrimSuffix(name, ".json"), ".partial")
		if id == "" {
			continue
		}
		var known bool
		if err := s.store.QueryRowContext(s.ctx,
			`SELECT EXISTS(SELECT 1 FROM data_exports WHERE id=?)`, id).Scan(&known); err != nil {
			return removed, err
		}
		if known {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err == nil {
			removed++
		} else if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("could not remove an orphaned export", "error", err, "file", name)
		}
	}
	return removed, nil
}
