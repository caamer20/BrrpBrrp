package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/backup"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

// backupStatus records what the installation can actually recover, which is not the same
// question as whether the last job succeeded. Three facts are kept apart:
//
//   - the last attempt, and how many in a row have failed;
//   - the newest archive that was created and verified *here* (LastSuccess/Name/Digest);
//   - the newest generation that is also copied to independent storage, which is the one
//     that survives losing this host (the Recoverable* fields).
//
// A failed copy therefore does not erase the recovery point the installation already
// had: it is recorded as a failure of the copy, the previous independently recoverable
// generation keeps its identity, and local retention keeps that archive rather than
// pruning it for being older than the newest one.
type backupStatus struct {
	LastAttempt string `json:"last_attempt"`
	// LastSuccess is when a local archive was created and verified, and LastCopy when an
	// independent copy of the current generation was completed.
	LastSuccess string `json:"last_success"`
	LastCopy    string `json:"last_copy"`
	// LastSnapshot is when a local snapshot was last attempted, and LastCopyAttempt when an
	// independent copy was last attempted. They are separate clocks on purpose: a copy
	// destination that stays unavailable must not stop fresh local recovery points, and
	// repairing a copy must not defer the next snapshot for a whole interval.
	LastSnapshot    string `json:"last_snapshot,omitempty"`
	LastCopyAttempt string `json:"last_copy_attempt,omitempty"`
	// SnapshotAt is when the recoverable data inside the current archive was captured. It
	// is carried separately from the job's completion time because a long-running backup
	// reports a snapshot that is older than the moment it finished: host health measures
	// the recovery point, not the job.
	SnapshotAt string `json:"snapshot_at,omitempty"`
	// Attempts counts consecutive failed attempts at the job itself, which bounds how
	// often a failing destination is retried inside one interval.
	Attempts int `json:"attempts,omitempty"`
	// CopyAttempts counts consecutive failed independent copies. It is kept separately
	// because a failed copy is a different fact from a failed backup: the local archive
	// exists and verified, and what is missing is the second copy on separate storage.
	CopyAttempts int `json:"copy_attempts,omitempty"`
	// SourceMissing records that the recorded generation's local archive is gone, so its
	// outstanding copy cannot be repaired from it. It is a statement about the local
	// recovery point rather than about the attempt, so it accelerates a replacement
	// snapshot instead of being cleared by a no-op copy retry.
	SourceMissing bool   `json:"source_missing,omitempty"`
	ID            string `json:"id"`
	// Name is the local archive inside the backups directory, and CopyName the extra copy
	// on separate storage.
	Name     string `json:"name,omitempty"`
	CopyName string `json:"copy_name,omitempty"`
	// Digest is the identity of the archive that was written, and CopyDigest the identity
	// of the extra copy. Both are recorded so a copy can be proved to be the archive that
	// was verified rather than a same-named file.
	Digest     string `json:"digest,omitempty"`
	CopyDigest string `json:"copy_digest,omitempty"`
	// IncludesUploads records whether the generation carries the uploaded files, so health
	// can tell a complete recovery point from a database-only one.
	IncludesUploads bool `json:"includes_uploads"`
	// Error is the most recent failure of any kind, kept for the operator-facing warning.
	// SnapshotError and CopyError are the two facts it summarizes: a failed local snapshot
	// and a failed independent copy are different problems, and neither may hide the other.
	SnapshotError string `json:"snapshot_error,omitempty"`
	CopyError     string `json:"copy_error,omitempty"`
	Error         string `json:"error"`
	// RecoverableID/Name/Digest and RecoverableCopyName/Digest/At describe the newest
	// generation that is both verified locally and copied to independent storage: the
	// recovery point an operator can actually use if this host is lost. They survive a
	// failed copy, a failed retention pass and a restart, so monitoring states what is
	// recoverable rather than only that the newest attempt did not complete.
	RecoverableID         string `json:"recoverable_id,omitempty"`
	RecoverableName       string `json:"recoverable_name,omitempty"`
	RecoverableDigest     string `json:"recoverable_digest,omitempty"`
	RecoverableCopyName   string `json:"recoverable_copy_name,omitempty"`
	RecoverableCopyDigest string `json:"recoverable_copy_digest,omitempty"`
	RecoverableCopyAt     string `json:"recoverable_copy_at,omitempty"`
	// RecoverableSnapshotAt is when the independently recoverable generation captured its
	// own data, and RecoverableIncludesUploads what it carries. Health ages that recovery
	// point by its own snapshot rather than by the newest attempt, so an older but usable
	// fallback is described accurately instead of being reported as the latest failure.
	RecoverableSnapshotAt      string `json:"recoverable_snapshot_at,omitempty"`
	RecoverableIncludesUploads bool   `json:"recoverable_includes_uploads,omitempty"`
}

// recoverable reports whether a generation that survives losing this host is recorded.
func (s backupStatus) recoverable() bool {
	return s.RecoverableID != "" && s.RecoverableName != "" && s.RecoverableCopyName != "" && s.RecoverableCopyDigest != ""
}

func (s *Server) backupStatus() backupStatus {
	var saved string
	var status backupStatus
	if s.store.QueryRow(`SELECT value FROM settings WHERE key='backup-status'`).Scan(&saved) == nil {
		_ = json.Unmarshal([]byte(saved), &status)
	}
	return status
}

// saveBackupStatus persists the recorded recovery state. It is written on the way out of
// every attempt, successful or not, so a crash mid-backup still describes the generation
// that was complete when the process stopped.
//
// It returns an error rather than only logging one: the status is what names the
// generation an operator can recover, so a write that did not land must be able to stop
// retention from pruning the generation the durable record still points at.
func (s *Server) saveBackupStatus(status backupStatus) error {
	encoded, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode backup status: %w", err)
	}
	if _, err := s.store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('backup-status',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, string(encoded), db.Now()); err != nil {
		return fmt.Errorf("save backup status: %w", err)
	}
	return nil
}

// backupKeep is the configured retention count with the default applied.
func (s *Server) backupKeep() int {
	if keep := s.Settings().BackupKeep; keep >= 1 {
		return keep
	}
	return 7
}

// outstandingGeneration reports the recorded local archive whose independent copy never
// completed, if there is one. Retrying that copy is what a failed backup destination
// needs; writing another archive does not produce the missing copy and cannot make the
// older one less recoverable.
// outstandingCopy reports whether the recorded local generation still lacks a verified
// independent copy. It is the condition the copy cadence repairs and the reason the
// recoverable fallback keeps its identity.
func outstandingCopy(status backupStatus) bool {
	if status.ID == "" || status.Name == "" || status.Digest == "" {
		return false
	}
	return !(status.CopyName != "" && status.CopyDigest == status.Digest)
}

func (s *Server) outstandingGeneration(status backupStatus) (path, copyName, digest string) {
	if s.config.BackupCopyDir == "" || !outstandingCopy(status) {
		return "", "", ""
	}
	return filepath.Join(s.config.DataDir, "backups", status.Name), "brrpbrrp-" + status.ID + ".zip", status.Digest
}

// completeCopy writes the independent copy of one verified generation and records it as
// the installation's recoverable recovery point. A failure leaves every previously
// recorded recovery identity in place: what the installation can recover is a fact about
// the installation, not about the attempt that just failed.
//
// The order is publication, then the durable recovery identity, then retention. The record
// is written before retention so a crash cannot prune an archive the durable ledger still
// names as the recovery point; if the process stops after publication but before the
// status write, the next attempt reconciles the published copy instead of rejecting it.
func (s *Server) completeCopy(ctx context.Context, status *backupStatus, path, name, digest string) error {
	// The generation the durable ledger names before this attempt. It is protected from
	// retention alongside the new one until the new identity is known to be persisted.
	previousRecoverableCopy := status.RecoverableCopyName
	copied, err := copyBackup(ctx, path, s.config.BackupCopyDir, name, digest)
	if err != nil {
		return fmt.Errorf("local backup succeeded; extra copy failed: %w", err)
	}
	status.LastCopy, status.CopyName, status.CopyDigest = db.Now(), name, copied
	status.RecoverableID, status.RecoverableName, status.RecoverableDigest = status.ID, filepath.Base(path), digest
	status.RecoverableCopyName, status.RecoverableCopyDigest, status.RecoverableCopyAt = name, copied, status.LastCopy
	status.RecoverableSnapshotAt, status.RecoverableIncludesUploads = status.SnapshotAt, status.IncludesUploads
	status.SourceMissing = false
	// The recovery identity is written before retention, and retention does not run at all
	// unless that write landed. A status write that failed leaves the durable ledger
	// naming the previous generation, so pruning by the new generation's name would delete
	// the very archive the ledger still points at.
	if err := s.saveBackupStatus(*status); err != nil {
		return fmt.Errorf("%w: the extra copy was published but its recovery identity was not recorded: %v", errStatusNotRecorded, err)
	}
	if err := retainBackups(s.config.BackupCopyDir, "brrpbrrp-", s.backupKeep(), status.RecoverableCopyName, previousRecoverableCopy); err != nil {
		return fmt.Errorf("extra copy succeeded; retention failed: %w", err)
	}
	return nil
}

// errStatusNotRecorded marks a generation that was published but whose recovery identity
// could not be written. Retention is skipped when it is returned: the durable ledger still
// names the previous generation, so pruning under the new name would delete the archive
// the installation actually recovers from.
var errStatusNotRecorded = errors.New("backup recovery identity was not recorded")

// retainLocal prunes the local archives to the configured count while protecting the
// generation that is independently recoverable. Without the protection, a destination
// that has been failing long enough would cost the installation the very recovery point
// the failures are reported against.
// retainLocal prunes the local archives to the configured count, always keeping every
// local generation the durable ledger names. Four archives are protected, which bounds how
// much extra storage retention may hold:
//
//   - the archive this attempt produced (status.Name), so a retention pass cannot delete
//     the generation that was just persisted;
//   - the archive the pre-attempt durable record named (previous.Name), so a pass cannot
//     delete what the ledger named when this attempt began;
//   - the local archives of the current and previous independently recoverable
//     generations, because the recovery procedure follows those identities.
//
// Retention is called only after the caller has established that its own record is durable,
// so this protection is the second half of the same rule rather than a substitute for it.
func (s *Server) retainLocal(status *backupStatus, previous backupStatus) error {
	if err := retainBackups(filepath.Join(s.config.DataDir, "backups"), "", s.backupKeep(),
		status.Name, previous.Name, status.RecoverableName, previous.RecoverableName); err != nil {
		return fmt.Errorf("backup succeeded; local retention failed: %w", err)
	}
	return nil
}

// recordBackupStatus persists the current record and reports whether it landed. That report
// is what retention depends on: pruning runs only after the record that names the
// generations involved is durable, so an attempt that cannot persist its own record does not
// prune anything. The failure is recorded into the attempt's outcome (failure) unless a more
// specific failure is already there, and it is always logged.
func (s *Server) recordBackupStatus(status backupStatus, failure *error, message string) bool {
	if saveErr := s.saveBackupStatus(status); saveErr != nil {
		s.log.Error("save backup status", "error", saveErr)
		if *failure == nil {
			*failure = fmt.Errorf("%w: %s: %v", errStatusNotRecorded, message, saveErr)
		}
		return false
	}
	return true
}

// The caller holds backupMu. A failed attempt delays retries to avoid filling logs or
// repeatedly copying a large archive when the destination is offline.
//
// The returned error describes the attempt as a whole and is what an operator-initiated
// backup answers with. The retry policy deliberately does not read it: a copy that failed
// after a local archive verified returns success with the failure recorded, and the
// recorded attempt counts are what schedule the retry.
func (s *Server) runBackup(ctx context.Context, uploads bool) (string, error) {
	return s.runBackupWith(ctx, uploads, true)
}

// repairCopy retries the independent copy of the recorded local generation without
// creating another archive. It is the copy half of the schedule, kept deliberately
// separate from the snapshot half: a destination that stays unavailable must not stop
// fresh local recovery points, and repairing a copy must not defer the next snapshot for
// a whole interval.
func (s *Server) repairCopy(ctx context.Context) (string, error) {
	return s.runBackupWith(ctx, false, false)
}

// runBackupWith runs one backup job. fresh selects a new local snapshot (with a copy
// attempt); otherwise only the outstanding independent copy is retried. Both paths write
// one status record, because both describe the same installation.
func (s *Server) runBackupWith(ctx context.Context, uploads, fresh bool) (id string, err error) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	status := s.backupStatus()
	// The record that is actually persisted now. Retention keeps its recovery generation as
	// well as the one this attempt records, so an attempt that cannot advance the durable
	// ledger cannot cost the installation the archive the ledger still names.
	durable := status
	status.LastAttempt = db.Now()
	if fresh {
		status.LastSnapshot = status.LastAttempt
	} else {
		status.LastCopyAttempt = status.LastAttempt
	}
	var jobErr, recoveryErr error
	// The two failure counters belong to their own cadence: a copy repair does not touch
	// the snapshot counters, and a snapshot that never produced an archive does not reset
	// the copy counters of a generation it did not write.
	defer func() {
		if jobErr != nil {
			status.SnapshotError = jobErr.Error()
			status.Attempts++
		} else if fresh {
			status.SnapshotError = ""
			status.Attempts = 0
		}
		if jobErr == nil || !fresh {
			if recoveryErr != nil {
				status.CopyError = recoveryErr.Error()
				status.CopyAttempts++
			} else {
				status.CopyError = ""
				status.CopyAttempts = 0
			}
		}
		status.Error = status.SnapshotError
		if status.Error == "" {
			status.Error = status.CopyError
		}
		// The final record is written on the way out, so a crash mid-backup still
		// describes the generation that was complete. A write that does not land is
		// reported as the job's outcome rather than logged and forgotten: the returned
		// error is what an operator-initiated backup answers with, and the retry policy
		// reads the recorded counters instead. It is not the error retention depends on:
		// the recovery identities were already persisted before retention ran, so a failed
		// final update leaves the durable ledger naming generations whose files are intact.
		if saveErr := s.saveBackupStatus(status); saveErr != nil {
			s.log.Error("save backup status", "error", saveErr)
			if err == nil {
				err = fmt.Errorf("the backup finished but its status record was not updated: %v", saveErr)
			}
		}
	}()

	// The copy-only path repairs the recorded generation and never writes an archive, so a
	// due snapshot cannot be starved by an outage on the copy destination.
	if !fresh {
		path, name, digest := s.outstandingGeneration(status)
		if path == "" {
			// No copy is configured, or the recorded generation already has its verified
			// copy. This is a genuine no-op: the deferred save clears any stale copy error.
			return status.ID, nil
		}
		// A repair that cannot reach its source is a failed repair, not a successful one.
		// The three cases are kept apart: a missing archive means the recorded recovery
		// generation no longer exists locally and a replacement snapshot is owed; an archive
		// that exists but cannot be read (permissions, I/O, a replaced non-regular file) is an
		// operator problem; a readable regular file is the copy this path exists to complete.
		info, statErr := os.Stat(path)
		switch {
		case statErr != nil && os.IsNotExist(statErr):
			status.SourceMissing = true
			recoveryErr = fmt.Errorf("the local archive %q for the recorded recovery generation is missing; a new snapshot is required", status.Name)
			return status.ID, nil
		case statErr != nil:
			recoveryErr = fmt.Errorf("the local archive %q for the recorded recovery generation could not be read: %w", status.Name, statErr)
			return status.ID, nil
		case !info.Mode().IsRegular():
			recoveryErr = fmt.Errorf("the local archive %q for the recorded recovery generation is not a regular file", status.Name)
			return status.ID, nil
		}
		// The record this path can advance is the copy identity. A failed copy may not
		// have reached completeCopy's status write at all, so local retention needs its
		// own explicit durability check, just as a fresh snapshot does. Protecting the
		// named generations alone does not authorize pruning after a failed write.
		if copyFailure := s.completeCopy(ctx, &status, path, name, digest); copyFailure != nil {
			recoveryErr = copyFailure
		}
		if s.recordBackupStatus(status, &recoveryErr, "the copy repair identity was not recorded") {
			if retentionFailure := s.retainLocal(&status, durable); retentionFailure != nil && recoveryErr == nil {
				recoveryErr = retentionFailure
			}
		}
		return status.ID, nil
	}

	needed := uint64(fileSize(s.config.DBFile))*2 + 256<<20
	if uploads {
		needed += uint64(directorySize(s.config.UploadDir))
	}
	free, freeErr := app.FreeDiskBytes(s.config.DataDir)
	if freeErr != nil {
		jobErr = fmt.Errorf("check backup storage: %w", freeErr)
		return "", jobErr
	}
	if free < needed {
		jobErr = errors.New("not enough free disk space for a backup and temporary snapshot")
		return "", jobErr
	}
	id = db.ID()
	path := filepath.Join(s.config.DataDir, "backups", id+".zip")
	if jobErr = backup.Create(ctx, s.store, s.config, path, uploads); jobErr != nil {
		return "", jobErr
	}
	// The archive is verified before it is recorded as a recovery generation, so a backup
	// that cannot be read back is never advertised as one. Verification extracts the
	// database into scratch storage, which is placed on the data volume: a container's
	// /tmp is small on purpose, and a realistic database does not fit in it even when the
	// data volume has room to spare.
	manifest, digest, verifyErr := backup.VerifyArchiveIn(ctx, path, filepath.Join(s.config.DataDir, "tmp"))
	if verifyErr != nil {
		jobErr = fmt.Errorf("the backup was created but does not verify: %w", verifyErr)
		return "", jobErr
	}
	status.ID, status.LastSuccess = id, db.Now()
	status.SnapshotAt = manifest.CreatedAt
	status.Name, status.IncludesUploads = filepath.Base(path), uploads && manifest.IncludesUploads
	status.SourceMissing = false
	// This generation's own copy has not been written yet, so the copy fields describe
	// this generation and are not inherited from a previous one. The Recoverable* fields
	// are what survive a failure here.
	status.Digest, status.CopyName, status.CopyDigest, status.LastCopy = digest, "", "", ""
	// A verified local generation is persisted here, before anything destructive happens and
	// independently of the copy. Two failures are then covered by one rule: a copy failure
	// cannot leave a fresh archive that no durable record names, and a later status failure
	// (including the final save on the way out) cannot leave the durable ledger naming an
	// archive that retention has already pruned. Retention runs only when this attempt made
	// its record durable.
	recorded := s.recordBackupStatus(status, &recoveryErr, "the backup was published but its local identity was not recorded")
	if recorded && s.config.BackupCopyDir != "" {
		status.LastCopyAttempt = db.Now()
		if copyFailure := s.completeCopy(ctx, &status, path, "brrpbrrp-"+id+".zip", digest); copyFailure != nil {
			recoveryErr = copyFailure
		} else {
			// completeCopy returns nil only after the recovery identity it recorded is durable.
			recorded = true
		}
	}
	if recorded {
		if retentionFailure := s.retainLocal(&status, durable); retentionFailure != nil && recoveryErr == nil {
			recoveryErr = retentionFailure
		}
	}
	return id, nil
}

// copyBackup writes the extra copy and returns its digest. The digest is computed
// while the bytes stream out and compared with the archive that was just verified,
// so a truncated, replaced or partially written copy is reported as a failed copy
// instead of passing as a backup.
func copyBackup(ctx context.Context, source, directory, name, expected string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	final := filepath.Join(directory, name)
	// A copy that was published before its completion was recorded is reconciled instead
	// of rejected. Publication and the status write are two steps, so a crash, a lost
	// write or a status-persistence failure between them would otherwise leave a valid
	// copy that every later attempt refuses by name for the life of the installation.
	//
	// An existing file is accepted only when it is a regular file whose digest is the
	// archive that was verified locally; that digest can only match byte-identical
	// content, so a truncated, replaced or otherwise different file is preserved for
	// explicit handling rather than overwritten. A symlink, directory or other
	// non-regular target is never followed or replaced, and a copy with no expected digest
	// cannot be verified, so it is not adopted either.
	if info, err := os.Lstat(final); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("the copy target %q exists and is not a regular file; it was left in place", final)
		}
		if info.Size() == 0 {
			return "", fmt.Errorf("the copy target %q is empty; it was left in place", final)
		}
		if expected == "" {
			return "", fmt.Errorf("the copy target %q already exists and cannot be verified without the expected digest", final)
		}
		existing, err := backup.Digest(final)
		if err != nil {
			return "", fmt.Errorf("verify the existing copy at %q: %w", final, err)
		}
		if existing != expected {
			return "", fmt.Errorf("a different file already exists at %q; it was left in place", final)
		}
		return existing, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	// A staging file left behind by an interrupted attempt is removed before a new one is
	// created: it was never published, and its deterministic name would otherwise fail
	// every retry with O_EXCL.
	staging := final + ".partial"
	if err := os.Remove(staging); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	output, err := os.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	complete := false
	defer func() {
		output.Close()
		if !complete {
			os.Remove(output.Name())
		}
	}()
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := input.Read(buffer)
		if n > 0 {
			if _, err := output.Write(buffer[:n]); err != nil {
				return "", err
			}
			hasher.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if err = output.Sync(); err != nil {
		return "", err
	}
	if err = output.Close(); err != nil {
		return "", err
	}
	copied := hex.EncodeToString(hasher.Sum(nil))
	if expected != "" && copied != expected {
		return "", errors.New("the copy does not match the archive that was verified")
	}
	if err = os.Rename(output.Name(), final); err != nil {
		return "", err
	}
	// The copy directory is flushed for the same reason the archive's own
	// directory is: a published name must be durable, not merely visible. A
	// filesystem that cannot synchronize a directory entry is a documented
	// platform limit (ADR 0007), not a failed copy; every other error is one.
	if err = app.SyncDirectory(directory); err != nil && !errors.Is(err, app.ErrDirectorySyncUnsupported) {
		return "", err
	}
	complete = true
	return copied, nil
}

// retainBackups keeps the newest archives and always keeps protect, which is the base
// name of the generation the installation can still recover. Retention by count alone
// would delete that archive as soon as enough newer attempts accumulated.
func retainBackups(directory, prefix string, keep int, protect ...string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	kept := make(map[string]bool, len(protect))
	for _, name := range protect {
		if name != "" {
			kept[name] = true
		}
	}
	var names []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), prefix), ".zip")
		parsed, err := uuid.Parse(id)
		if err == nil && parsed.Version() == 7 && parsed.String() == id {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names[min(keep, len(names)):] {
		if kept[name] {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	return nil
}

// scheduledBackup runs the backup half that is due. The local snapshot cadence and the
// independent-copy cadence are separate: a copy destination that has been failing cannot
// stop fresh local recovery points, and repairing a copy cannot defer the next snapshot.
// A due snapshot therefore wins over a due copy retry, and the generation it writes
// becomes the one the copy cadence tries to place.
func (s *Server) scheduledBackup(now time.Time) {
	settings := s.Settings()
	if !settings.BackupEnabled || !s.backupMu.TryLock() {
		return
	}
	defer s.backupMu.Unlock()
	status := s.backupStatus()
	switch {
	case snapshotDue(settings, status, now):
		if _, err := s.runBackup(s.ctx, settings.BackupUploads); err != nil {
			s.log.Warn("scheduled backup failed", "error", err)
		}
	case copyRetryDue(settings, status, now):
		if _, err := s.repairCopy(s.ctx); err != nil {
			s.log.Warn("scheduled backup copy repair failed", "error", err)
		}
	}
}

// backupRetryDelay is how soon a failed attempt is retried, and maxBackupRetries
// bounds how many times that happens inside one interval. A destination that is
// offline is retried promptly a few times and then left until the next interval,
// so a failure is reported rather than hidden behind a full interval of silence.
const (
	backupRetryDelay = 15 * time.Minute
	maxBackupRetries = 3
)

// backupInterval is the configured regular interval with the floor applied.
func backupInterval(settings db.Settings) time.Duration {
	return time.Duration(max(1, settings.BackupHours)) * time.Hour
}

// lastStamp parses the first recorded time in preference order.
func lastStamp(stamps ...string) time.Time {
	for _, stamp := range stamps {
		if parsed, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// lastSnapshotAt is which clock the snapshot cadence runs on. Older records predate the
// separate fields, so the last attempt and then the last success are used as fallbacks.
func lastSnapshotAt(status backupStatus) time.Time {
	return lastStamp(status.LastSnapshot, status.LastAttempt, status.LastSuccess)
}

// lastCopyAttemptAt is which clock the copy cadence runs on.
func lastCopyAttemptAt(status backupStatus) time.Time {
	return lastStamp(status.LastCopyAttempt, status.LastCopy, status.LastAttempt)
}

// snapshotDue decides whether a regular local snapshot is owed now. A failed snapshot
// enters the bounded short-retry path instead of waiting a whole interval; a healthy
// schedule runs on the configured interval from its last attempt. It is a function of the
// schedule, the recorded status and the clock, so the policy is testable without waiting.
func snapshotDue(settings db.Settings, status backupStatus, now time.Time) bool {
	last := lastSnapshotAt(status)
	// The recorded generation's archive is gone, so the copy that depends on it can never be
	// repaired from it. A replacement snapshot is owed on the short retry clock rather than
	// after a full interval, because nothing else will produce one.
	if status.SourceMissing {
		return now.Sub(last) >= backupRetryDelay
	}
	if status.SnapshotError != "" && status.Attempts > 0 && status.Attempts <= maxBackupRetries {
		return now.Sub(last) >= backupRetryDelay
	}
	return now.Sub(last) >= backupInterval(settings)
}

// copyRetryDue decides whether the outstanding independent copy is owed a retry. It runs
// only while a local generation actually lacks a verified copy, so a healthy
// installation is not copied more often than it is snapshotted.
func copyRetryDue(settings db.Settings, status backupStatus, now time.Time) bool {
	if !outstandingCopy(status) {
		return false
	}
	last := lastCopyAttemptAt(status)
	if status.CopyError != "" && status.CopyAttempts > 0 && status.CopyAttempts <= maxBackupRetries {
		return now.Sub(last) >= backupRetryDelay
	}
	return now.Sub(last) >= backupInterval(settings)
}
