package api

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/backup"
	"brrpbrrp/internal/db"
)

func TestBackupCopyMustMatchTheArchiveItCopied(t *testing.T) {
	s, _ := testServer(t)
	source := filepath.Join(s.config.DataDir, "source.zip")
	if err := os.WriteFile(source, []byte("an archive's bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(s.config.DataDir, "copy")
	if _, err := copyBackup(context.Background(), source, directory, "copy.zip", strings.Repeat("0", 64)); err == nil {
		t.Fatal("a copy whose digest disagreed with the archive was accepted")
	}
	if _, err := os.Stat(filepath.Join(directory, "copy.zip")); !os.IsNotExist(err) {
		t.Fatal("rejected copy was published")
	}
	digest, err := backup.Digest(source)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := copyBackup(context.Background(), source, directory, "copy.zip", digest)
	if err != nil || copied != digest {
		t.Fatalf("copy digest %q, want %q (%v)", copied, digest, err)
	}
	stored, err := os.ReadFile(filepath.Join(directory, "copy.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "an archive's bytes" {
		t.Fatalf("copy holds %q", stored)
	}
}

func TestScheduledBackupRetentionAndCopy(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.config.BackupCopyDir = t.TempDir()
	s.settings.BackupEnabled = true
	s.settings.BackupKeep = 2
	managed := filepath.Join(s.config.DataDir, "backups")
	for _, dir := range []string{managed, s.config.BackupCopyDir} {
		if err := os.WriteFile(filepath.Join(dir, "keep-me.zip"), []byte("unrelated"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		s.backupMu.Lock()
		_, err := s.runBackup(s.ctx, true)
		s.backupMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	status := s.backupStatus()
	if status.LastSuccess == "" || status.LastCopy == "" || status.Error != "" {
		t.Fatalf("bad backup status: %+v", status)
	}
	// The recorded identity must be the archive on disk, and the copy must match
	// it, so an operator can prove the copy is the archive that was verified.
	onDisk, digestErr := backup.Digest(filepath.Join(managed, status.ID+".zip"))
	if digestErr != nil || status.Digest != onDisk {
		t.Fatalf("status digest %q, archive %q (%v)", status.Digest, onDisk, digestErr)
	}
	if status.CopyDigest != status.Digest {
		t.Fatalf("copy digest %q does not match %q", status.CopyDigest, status.Digest)
	}
	for _, dir := range []string{managed, s.config.BackupCopyDir} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 3 {
			t.Fatalf("retention removed unrelated files or kept too many: %v %v", entries, err)
		}
	}
	s.scheduledBackup(time.Now())
	if s.backupStatus().ID != status.ID {
		t.Fatal("schedule ran before its interval")
	}
	// A failed extra copy must leave the local backup available and record a warning.
	s.config.BackupCopyDir = filepath.Join(managed, "keep-me.zip")
	s.backupMu.Lock()
	_, err := s.runBackup(s.ctx, true)
	s.backupMu.Unlock()
	if err != nil || s.backupStatus().Error == "" {
		t.Fatal("copy failure not recorded separately")
	}
	if _, err := os.Stat(filepath.Join(managed, s.backupStatus().ID+".zip")); err != nil {
		t.Fatal("local backup lost after copy failure")
	}
}

// TestAFailedCopyKeepsTheRecoverableGenerationAndSchedulesItsRetry is the reproduced
// R3-03 probe. A copy destination that has stopped working used to reset the attempt
// count, discard the previously verified independent copy identity and let count-based
// retention prune the archive that was still recoverable.
func TestAFailedCopyKeepsTheRecoverableGenerationAndSchedulesItsRetry(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	copies := t.TempDir()
	s.config.BackupCopyDir = copies
	s.settings.BackupEnabled = true
	s.settings.BackupKeep = 1

	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	first := s.backupStatus()
	if !first.recoverable() || first.CopyAttempts != 0 || first.Error != "" {
		t.Fatalf("a completed copy was not recorded as a recoverable generation: %+v", first)
	}
	if first.RecoverableID != first.ID || first.RecoverableDigest != first.Digest {
		t.Fatalf("the recoverable generation is not the one that was copied: %+v", first)
	}

	// The destination becomes a regular file, which is exactly how an unavailable or
	// misconfigured copy target presents itself.
	blocked := filepath.Join(copies, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked

	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("a local backup whose extra copy failed reported the job as failed: %v", err)
	}
	failed := s.backupStatus()
	if failed.ID == first.ID {
		t.Fatal("the second attempt did not create and verify a new local generation")
	}
	if failed.Error == "" || failed.CopyAttempts != 1 {
		t.Fatalf("the failed copy was not counted: %+v", failed)
	}
	if failed.Attempts != 0 || failed.LastSuccess == "" {
		t.Fatalf("the verified local backup was not recorded as a success: %+v", failed)
	}
	// The recovery point that survives losing this host is unchanged, and still named.
	if !failed.recoverable() {
		t.Fatalf("the previous independent recovery identity was discarded: %+v", failed)
	}
	if failed.RecoverableID != first.RecoverableID || failed.RecoverableName != first.RecoverableName || failed.RecoverableCopyDigest != first.RecoverableCopyDigest {
		t.Fatalf("recoverable identity moved on a failed copy:\n first  %+v\n failed %+v", first, failed)
	}
	// Retention keeps the archive that can still be recovered even at a retention count
	// of one, and keeps its copy too.
	for _, path := range []string{
		filepath.Join(s.config.DataDir, "backups", failed.RecoverableName),
		filepath.Join(copies, failed.RecoverableCopyName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the recoverable generation was pruned: %v", err)
		}
	}
	// The failure enters the bounded short-retry path instead of waiting a full interval.
	last, err := time.Parse(time.RFC3339Nano, failed.LastAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if !copyRetryDue(db.Settings{BackupHours: 24}, failed, last.Add(backupRetryDelay+time.Minute)) {
		t.Fatal("a failed copy was not scheduled for its short retry")
	}
	if copyRetryDue(db.Settings{BackupHours: 24}, failed, last.Add(time.Minute)) {
		t.Fatal("the retry ran before the retry delay")
	}
	// The recorded state is the persisted one, so a restart keeps the recovery identity.
	reloaded := s.backupStatus()
	if reloaded.RecoverableID != first.RecoverableID || reloaded.CopyAttempts != 1 {
		t.Fatalf("the recording did not survive a reload: %+v", reloaded)
	}

	// The copy repair runs on its own clock and promotes the generation that now has a
	// copy, without writing another archive. Under R4-04 the scheduler chooses this path
	// instead of calling the snapshot path, which is what let a failing destination starve
	// fresh local snapshots.
	s.config.BackupCopyDir = t.TempDir()
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("copy retry: %v", err)
	}
	repaired := s.backupStatus()
	if repaired.ID != failed.ID {
		t.Fatalf("the retry wrote another archive instead of copying the outstanding one: %s -> %s", failed.ID, repaired.ID)
	}
	if !repaired.recoverable() || repaired.RecoverableID != repaired.ID || repaired.CopyDigest != repaired.Digest {
		t.Fatalf("the repaired generation was not recorded as recoverable: %+v", repaired)
	}
	if repaired.CopyAttempts != 0 || repaired.Error != "" {
		t.Fatalf("the repair left a failure recorded: %+v", repaired)
	}

	// A destination that fails from the start is recorded as a job failure, not as a
	// successful backup with a warning, because there is no verified local archive.
	s2, handler2 := testServer(t)
	setupAdmin(t, s2, handler2)
	s2.config.BackupCopyDir = blocked
	if _, err := s2.runBackup(s2.ctx, true); err != nil {
		t.Fatalf("second installation: %v", err)
	}
	starting := s2.backupStatus()
	if starting.CopyAttempts != 1 || starting.Attempts != 0 || starting.Error == "" {
		t.Fatalf("a first-generation copy failure was not counted: %+v", starting)
	}
	if starting.recoverable() {
		t.Fatalf("an installation with no completed copy claims a recoverable generation: %+v", starting)
	}
}

// TestAFailedStatusWriteBlocksRetentionOfTheDurableRecoveryPoint is the maintained R5-03
// regression. Review 5 reproduced it by rejecting the backup-status write with a database
// trigger and running a second backup with a retention count of one: the completion
// reported success, the durable ledger still named the first generation, and retention
// deleted both that generation's local archive and its independent copy, leaving the
// recorded recovery point unrecoverable.
func TestAFailedStatusWriteBlocksRetentionOfTheDurableRecoveryPoint(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.config.BackupCopyDir = t.TempDir()
	s.settings.BackupKeep = 1

	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	previous := s.backupStatus()
	if !previous.recoverable() {
		t.Fatalf("fixture: the first generation is not recoverable: %+v", previous)
	}

	// Every status write for the next attempt is rejected.
	if _, err := s.store.Exec(`CREATE TRIGGER r5_status_failure BEFORE UPDATE OF value ON settings WHEN NEW.key='backup-status' BEGIN SELECT RAISE(FAIL,'injected backup status persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	next, err := s.runBackup(s.ctx, true)
	durable := s.backupStatus()
	t.Logf("next=%s returned=%v durable=%s", next, err, durable.RecoverableID)

	if next == previous.ID {
		t.Fatal("the second attempt did not create a new local generation")
	}
	if durable.RecoverableID != previous.RecoverableID {
		t.Fatalf("the durable ledger moved while its write was failing: %+v", durable)
	}
	// A failed persistence is the job's outcome, not a silent success.
	if err == nil {
		t.Fatal("a failed status persistence was reported as a successful durable completion")
	}
	// Nothing may prune the generation the durable ledger still names, locally or on its
	// independent storage.
	for _, path := range []string{
		filepath.Join(s.config.DataDir, "backups", previous.RecoverableName),
		filepath.Join(s.config.BackupCopyDir, previous.RecoverableCopyName),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("retention removed the durable recovery generation %s: %v", path, statErr)
		}
	}
}

// TestAMissingCopySourceIsNotReportedAsARepairedCopy is the maintained R5-04 regression.
// Review 5 reproduced it by creating a verified local generation whose copy failed,
// removing that generation's local archive, making the destination available and calling
// the copy repair: it returned success with an empty error and reset the attempt counters,
// even though the generation still had no copy and its source was gone.
func TestAMissingCopySourceIsNotReportedAsARepairedCopy(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("local backup: %v", err)
	}
	pending := s.backupStatus()
	if pending.CopyError == "" || !outstandingCopy(pending) {
		t.Fatalf("fixture: no pending copy failure: %+v", pending)
	}
	if err := os.Remove(filepath.Join(s.config.DataDir, "backups", pending.Name)); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = t.TempDir()

	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("a failed copy repair stopped the job: %v", err)
	}
	after := s.backupStatus()
	if after.CopyError == "" {
		t.Fatal("a missing source was silently treated as a successful copy repair")
	}
	if !after.SourceMissing {
		t.Fatalf("the missing source was not recorded: %+v", after)
	}
	if after.CopyAttempts == 0 {
		t.Fatalf("the failed repair reset the copy attempt count: %+v", after)
	}
	if !outstandingCopy(after) {
		t.Fatalf("the copy stopped being outstanding: %+v", after)
	}
	// A missing source accelerates a replacement snapshot instead of waiting a full
	// interval, because nothing else will produce one.
	last, err := time.Parse(time.RFC3339Nano, after.LastAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotDue(s.settings, after, last.Add(backupRetryDelay+time.Minute)) {
		t.Fatal("a missing source did not schedule a replacement snapshot on the short clock")
	}

	// A replacement snapshot converges: the missing-source state clears and the new
	// generation is recorded with its independent copy.
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("replacement snapshot: %v", err)
	}
	recovered := s.backupStatus()
	if recovered.SourceMissing || recovered.CopyError != "" || !recovered.recoverable() {
		t.Fatalf("a replacement snapshot did not clear the missing-source state: %+v", recovered)
	}
}

// TestANonRegularCopySourceIsStillAFailedRepair covers the other half of R5-04: an archive
// that exists but is not the regular file that was recorded (a directory, a device, a
// replaced symlink) is reported as a failure rather than treated as a missing-or-absent
// no-op.
func TestANonRegularCopySourceIsStillAFailedRepair(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("local backup: %v", err)
	}
	pending := s.backupStatus()
	archive := filepath.Join(s.config.DataDir, "backups", pending.Name)
	if err := os.Remove(archive); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(archive, 0700); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = t.TempDir()
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("a failed copy repair stopped the job: %v", err)
	}
	after := s.backupStatus()
	if after.CopyError == "" || after.CopyAttempts == 0 {
		t.Fatalf("a non-regular source was reported as a repaired copy: %+v", after)
	}
	if !outstandingCopy(after) {
		t.Fatalf("the copy stopped being outstanding: %+v", after)
	}
}

// TestAPublishedCopyIsReconciledWhenItsCompletionWasLost is the maintained R4-03
// regression. Review 4 reproduced it by publishing the correct copy through the real copy
// helper while leaving its completion unrecorded: the retry then rejected the existing
// file by name forever, so a valid published copy could never become the recorded
// recovery point. Completion is now idempotent: an existing regular file that is the
// archive that was verified is adopted instead of rejected.
func TestAPublishedCopyIsReconciledWhenItsCompletionWasLost(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("local backup: %v", err)
	}
	pending := s.backupStatus()
	if pending.CopyAttempts == 0 || pending.recoverable() {
		t.Fatalf("the copy failure was not recorded: %+v", pending)
	}

	// The correct copy is published, but the process stops before its success is recorded:
	// the status still describes a generation without a copy, while the destination holds
	// the archive that was verified.
	destination := t.TempDir()
	if _, err := copyBackup(context.Background(), filepath.Join(s.config.DataDir, "backups", pending.Name),
		destination, "brrpbrrp-"+pending.ID+".zip", pending.Digest); err != nil {
		t.Fatal(err)
	}
	lost := s.backupStatus()
	if lost.CopyDigest != "" || lost.recoverable() {
		t.Fatalf("fixture: the completion record survived: %+v", lost)
	}

	s.config.BackupCopyDir = destination
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	after := s.backupStatus()
	if after.RecoverableID != pending.ID || after.CopyDigest != pending.Digest || after.CopyError != "" {
		t.Fatalf("a correct published copy was not reconciled after its completion was lost: %+v", after)
	}
	// The completion is idempotent, so a second attempt neither duplicates anything nor
	// reports a failure.
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("repeated reconcile: %v", err)
	}
	if again := s.backupStatus(); again.CopyAttempts != 0 || again.RecoverableID != pending.ID || again.Error != "" {
		t.Fatalf("a repeated reconcile changed the record: %+v", again)
	}
}

// TestAnExistingCopyIsAdoptedOnlyWhenItIsTheVerifiedGeneration covers the other half of
// R4-03: a conflicting, non-regular, unverifiable or partially published target is never
// adopted and never overwritten, so a wrong file cannot be promoted to the recovery point.
func TestAnExistingCopyIsAdoptedOnlyWhenItIsTheVerifiedGeneration(t *testing.T) {
	s, _ := testServer(t)
	source := filepath.Join(s.config.DataDir, "archive.zip")
	if err := os.WriteFile(source, []byte("the verified archive"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := backup.Digest(source)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a published copy is adopted", func(t *testing.T) {
		directory := t.TempDir()
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", digest); err != nil {
			t.Fatalf("first copy: %v", err)
		}
		adopted, err := copyBackup(context.Background(), source, directory, "copy.zip", digest)
		if err != nil || adopted != digest {
			t.Fatalf("an identical copy reconciled as %q (%v)", adopted, err)
		}
	})

	t.Run("a different file is preserved", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "copy.zip")
		if err := os.WriteFile(target, []byte("someone else's file"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", digest); err == nil {
			t.Fatal("a conflicting file was adopted")
		}
		kept, err := os.ReadFile(target)
		if err != nil || string(kept) != "someone else's file" {
			t.Fatalf("the conflicting file was overwritten: %q (%v)", kept, err)
		}
	})

	t.Run("a truncated copy is preserved", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "copy.zip")
		if err := os.WriteFile(target, []byte("the verified"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", digest); err == nil {
			t.Fatal("a truncated file was adopted")
		}
		if kept, err := os.ReadFile(target); err != nil || string(kept) != "the verified" {
			t.Fatalf("the truncated file was overwritten: %q (%v)", kept, err)
		}
	})

	t.Run("a symlink is not followed", func(t *testing.T) {
		directory := t.TempDir()
		elsewhere := filepath.Join(directory, "elsewhere")
		if err := os.WriteFile(elsewhere, []byte("the verified archive"), 0600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "copy.zip")
		if err := os.Symlink(elsewhere, link); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", digest); err == nil {
			t.Fatal("a symlink target was adopted")
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the symlink was replaced: %v %v", info, err)
		}
	})

	t.Run("an unverifiable copy is not adopted", func(t *testing.T) {
		directory := t.TempDir()
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", digest); err != nil {
			t.Fatal(err)
		}
		if _, err := copyBackup(context.Background(), source, directory, "copy.zip", ""); err == nil {
			t.Fatal("an existing copy was adopted without a digest to verify it against")
		}
	})

	t.Run("a staging file does not block the retry", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "copy.zip.partial"), []byte("half a copy"), 0600); err != nil {
			t.Fatal(err)
		}
		copied, err := copyBackup(context.Background(), source, directory, "copy.zip", digest)
		if err != nil || copied != digest {
			t.Fatalf("an interrupted attempt blocked the retry: %q (%v)", copied, err)
		}
	})
}

// TestACopyOutageDoesNotStarveFreshLocalSnapshots is the maintained R4-04 regression.
// Review 4 reproduced it by making a copy destination unavailable, committing a new
// message, and setting the schedule overdue: the old code returned after retrying the
// outstanding copy, so the new committed data had no local snapshot and the generation id
// never moved. The snapshot cadence is now independent of the copy cadence.
func TestACopyOutageDoesNotStarveFreshLocalSnapshots(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.settings.BackupEnabled = true
	s.settings.BackupHours = 24
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked

	first, err := s.runBackup(s.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
			break
		}
	}
	if w := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "new data since the failed copy", IdempotencyKey: "r4-04-new"}, admin); w.Code != 201 {
		t.Fatalf("message: %d %s", w.Code, w.Body.String())
	}

	// The regular interval has passed while the destination stays unavailable.
	status := s.backupStatus()
	if status.Error == "" || status.CopyAttempts == 0 {
		t.Fatalf("the copy failure was not recorded: %+v", status)
	}
	overdue := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	status.LastAttempt, status.LastSnapshot, status.LastCopyAttempt = overdue, overdue, overdue
	s.saveBackupStatus(status)
	if !snapshotDue(s.settings, s.backupStatus(), time.Now()) {
		t.Fatal("fixture: the snapshot deadline is not due")
	}

	s.scheduledBackup(time.Now())
	after := s.backupStatus()
	if after.ID == first {
		t.Fatal("the regular snapshot deadline retried only the old copy; the new committed data has no local snapshot")
	}
	if after.SnapshotError != "" || after.Attempts != 0 {
		t.Fatalf("the fresh local snapshot did not succeed: %+v", after)
	}
	if after.CopyError == "" || after.CopyAttempts == 0 {
		t.Fatalf("the copy outage was not reported truthfully: %+v", after)
	}
	if !after.recoverable() && after.Error == "" {
		t.Fatalf("the degraded state was reported as healthy: %+v", after)
	}
	// The new generation's own snapshot is the one health ages, and it is newer than the
	// first generation's.
	if after.SnapshotAt == "" || after.LastSuccess == "" {
		t.Fatalf("the new generation recorded no snapshot time: %+v", after)
	}
}

// TestTheSnapshotCadenceSurvivesACopyOutageAndRecovers covers the acceptance review 4
// named for R4-04: several intervals with an unavailable destination, the durable record
// read back between them as a restart does, retry exhaustion, and a recovered destination
// that converges on the current generation without deferring the next snapshot.
func TestTheSnapshotCadenceSurvivesACopyOutageAndRecovers(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.settings.BackupEnabled = true
	s.settings.BackupHours = 24
	s.settings.BackupKeep = 2
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked

	now := time.Now()
	seen := map[string]bool{}
	for interval := range 4 {
		at := now.Add(time.Duration(interval*25) * time.Hour)
		// The record is read back from the database on each pass, which is what a restart
		// between intervals is.
		status := s.backupStatus()
		stamp := at.Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
		status.LastAttempt, status.LastSnapshot, status.LastCopyAttempt = stamp, stamp, stamp
		s.saveBackupStatus(status)
		s.scheduledBackup(at)
		seen[s.backupStatus().ID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("a copy outage collapsed the snapshot generations: %d distinct ids", len(seen))
	}
	if status := s.backupStatus(); status.CopyError == "" || status.CopyAttempts == 0 {
		t.Fatalf("the copy outage stopped being reported: %+v", status)
	}

	// The destination recovers, and the next copy attempt converges on the current
	// generation rather than on an obsolete one.
	s.config.BackupCopyDir = t.TempDir()
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("copy repair: %v", err)
	}
	recovered := s.backupStatus()
	if !recovered.recoverable() || recovered.RecoverableID != recovered.ID || recovered.CopyError != "" || recovered.Error != "" {
		t.Fatalf("the recovered destination did not converge on the current generation: %+v", recovered)
	}
	// The repair did not defer the next snapshot for a whole interval: the snapshot clock
	// is its own, and the regular deadline is still due on schedule.
	if !snapshotDue(s.settings, recovered, now.Add(time.Duration(3*25)*time.Hour+time.Minute)) {
		t.Fatal("a copy repair deferred the next regular snapshot")
	}
}

// TestRetentionAlwaysKeepsTheRecoverableGeneration covers the pruning rule directly: the
// recorded recovery point survives even when newer archives exceed the retention count.
func TestRetentionAlwaysKeepsTheRecoverableGeneration(t *testing.T) {
	directory := t.TempDir()
	// Identifier order is creation order for UUIDv7, so these are three archived
	// generations in sequence; retention names are compared in that same order.
	prunable := "0198f000-0000-7000-8000-000000000001.zip"
	recoverable := "0198f000-0000-7000-8000-000000000002.zip"
	newest := "0198f000-0000-7000-8000-000000000003.zip"
	for _, name := range []string{recoverable, newest, prunable, "keep-me.zip"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := retainBackups(directory, "", 1, recoverable); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, prunable)); !os.IsNotExist(err) {
		t.Fatal("retention kept an archive beyond the count that is not the recovery point")
	}
	for _, name := range []string{recoverable, newest, "keep-me.zip"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("retention removed %s: %v", name, err)
		}
	}
}

// assertArchive proves a recovery identity is backed by the file it names, by digest rather
// than by name: a same-named file with different bytes is not the recorded archive.
func assertArchive(t *testing.T, path, digest string) {
	t.Helper()
	onDisk, err := backup.Digest(path)
	if err != nil {
		t.Fatalf("the recorded archive %s is missing or unreadable: %v", filepath.Base(path), err)
	}
	if onDisk != digest {
		t.Fatalf("the recorded archive %s has digest %s, want %s", filepath.Base(path), onDisk, digest)
	}
}

// TestACombinedCopyAndStatusFailureKeepsTheDurableLocalGeneration is the maintained R6-03
// regression. Review 6 reproduced it with a retention count of one, a copy destination that
// could not be written and an injected rejection of every backup-status update: the fresh
// local generation was not persisted before retention ran, so retention deleted the archive
// the durable ledger still named and the ledger then pointed at nothing.
//
// The attempt now persists its verified local generation before retention, and retention runs
// only when that write landed. Both generations are checked on disk by digest, including
// through an installation reopened as a new store and server, and recovery then converges
// on the generation the durable record names.
func TestACombinedCopyAndStatusFailureKeepsTheDurableLocalGeneration(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.settings.BackupKeep = 1
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	previous := s.backupStatus()
	if previous.Name == "" || previous.Digest == "" || previous.RecoverableID != "" {
		t.Fatalf("fixture: the first generation must be a verified local-only recovery point: %+v", previous)
	}
	if _, err := s.store.Exec(`CREATE TRIGGER r6_status_failure BEFORE UPDATE OF value ON settings WHEN NEW.key='backup-status' BEGIN SELECT RAISE(FAIL,'injected status persistence failure'); END`); err != nil {
		t.Fatal(err)
	}

	next, err := s.runBackup(s.ctx, true)
	durable := s.backupStatus()
	t.Logf("next=%s returned=%v durable=%s", next, err, durable.ID)
	if next == "" || next == previous.ID {
		t.Fatal("the second attempt did not create a new local generation")
	}
	if durable.ID != previous.ID || durable.Name != previous.Name {
		t.Fatalf("the durable ledger moved while its write was failing: %+v", durable)
	}
	// A combined copy and status failure is the attempt's outcome, not a silent success.
	if err == nil {
		t.Fatal("a combined copy and status failure was reported as a successful durable completion")
	}
	// The reviewed defect: the archive the durable record still names must have survived the
	// retention pass, and the archive this attempt created must exist too rather than being
	// left unrecorded and pruned.
	assertArchive(t, filepath.Join(s.config.DataDir, "backups", durable.Name), durable.Digest)
	created := filepath.Join(s.config.DataDir, "backups", next+".zip")
	if _, err := backup.Digest(created); err != nil {
		t.Fatalf("the verified generation this attempt created was pruned without being recorded: %v", err)
	}

	// A restart is a new store and a new server: the record it reads and the archives on
	// disk must still agree.
	reopened, err := db.Open(s.config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := New(s.config, reopened, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restored := restarted.backupStatus()
	if restored.ID != previous.ID || restored.Name != previous.Name || restored.Digest != previous.Digest {
		t.Fatalf("the reopened installation reads a different durable record:\n before %+v\n after  %+v", previous, restored)
	}
	assertArchive(t, filepath.Join(s.config.DataDir, "backups", restored.Name), restored.Digest)

	// Recovery converges: with the failure cleared and a destination available, the copy
	// repair completes the generation the durable record names.
	if _, err := s.store.Exec(`DROP TRIGGER r6_status_failure`); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	restarted.config.BackupCopyDir = destination
	if _, err := restarted.repairCopy(restarted.ctx); err != nil {
		t.Fatalf("copy repair after recovery: %v", err)
	}
	converged := restarted.backupStatus()
	if !converged.recoverable() || converged.RecoverableID != previous.ID || converged.CopyDigest != previous.Digest {
		t.Fatalf("the recovery did not converge on the durable generation: %+v", converged)
	}
	assertArchive(t, filepath.Join(destination, converged.RecoverableCopyName), converged.RecoverableCopyDigest)
}

// TestAStatusInsertFailureBlocksRetentionOfBothIdentities covers the INSERT half of the same
// rule, with both identities the durable ledger needed: a local generation that also had a
// working independent copy. The record is lost, every later insert is rejected and the copy
// destination is unavailable, so nothing this attempt produced can be persisted. Retention
// must not run at all, and both files of the recoverable generation must survive.
func TestAStatusInsertFailureBlocksRetentionOfBothIdentities(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	copies := t.TempDir()
	s.config.BackupCopyDir = copies
	s.settings.BackupKeep = 1
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	previous := s.backupStatus()
	if !previous.recoverable() {
		t.Fatalf("fixture: the first generation is not independently recoverable: %+v", previous)
	}

	// The status row is lost, as a partial restore leaves it, and no later insert lands.
	if _, err := s.store.Exec(`DELETE FROM settings WHERE key='backup-status'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`CREATE TRIGGER r6_insert_failure BEFORE INSERT ON settings WHEN NEW.key='backup-status' BEGIN SELECT RAISE(FAIL,'injected status insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked

	next, err := s.runBackup(s.ctx, true)
	if err == nil {
		t.Fatal("a backup whose status insert failed was reported as a successful completion")
	}
	if next == "" || next == previous.ID {
		t.Fatal("the attempt did not create a new local generation")
	}
	// Every identity the previous durable record needed is still backed by its file, and the
	// generation this attempt created exists as well.
	assertArchive(t, filepath.Join(s.config.DataDir, "backups", previous.Name), previous.Digest)
	assertArchive(t, filepath.Join(copies, previous.RecoverableCopyName), previous.RecoverableCopyDigest)
	if _, err := backup.Digest(filepath.Join(s.config.DataDir, "backups", next+".zip")); err != nil {
		t.Fatalf("the verified generation this attempt created was pruned without being recorded: %v", err)
	}
}

// TestAFinalStatusWriteFailureLeavesTheRecoveryIdentitiesIntact covers the last shape of
// R6-03: the record that names this attempt's generation lands and retention therefore runs,
// and then the final status update on the way out is rejected. The failure is reported, and
// the durable record it left behind still names an archive that is on disk with the recorded
// digest, together with the generation the previous record named. The outstanding-copy state
// the next attempt repairs is preserved as well.
func TestAFinalStatusWriteFailureLeavesTheRecoveryIdentitiesIntact(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	s.settings.BackupKeep = 1
	blocked := filepath.Join(s.config.DataDir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BackupCopyDir = blocked
	if _, err := s.runBackup(s.ctx, true); err != nil {
		t.Fatalf("first backup: %v", err)
	}
	previous := s.backupStatus()

	// The next attempt's first record write lands and the final one on the way out does not:
	// the marker table places the injected failure exactly there.
	if _, err := s.store.Exec(`CREATE TABLE r6_status_writes(n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`CREATE TRIGGER r6_status_record AFTER UPDATE OF value ON settings WHEN NEW.key='backup-status' BEGIN INSERT INTO r6_status_writes(n) VALUES(1); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`CREATE TRIGGER r6_status_later BEFORE UPDATE OF value ON settings WHEN NEW.key='backup-status' AND (SELECT count(*) FROM r6_status_writes)>=1 BEGIN SELECT RAISE(FAIL,'injected final status failure'); END`); err != nil {
		t.Fatal(err)
	}

	next, err := s.runBackup(s.ctx, true)
	if err == nil {
		t.Fatal("a failed final status write was reported as a successful completion")
	}
	durable := s.backupStatus()
	if durable.ID != next || durable.Digest == "" {
		t.Fatalf("the durable record does not name the generation this attempt verified: %+v", durable)
	}
	assertArchive(t, filepath.Join(s.config.DataDir, "backups", durable.Name), durable.Digest)
	// Retention ran, because the record that triggered it was durable; the generation the
	// previous record named was protected from it.
	assertArchive(t, filepath.Join(s.config.DataDir, "backups", previous.Name), previous.Digest)
	// The record the next attempt reads still knows that this generation owes a copy.
	if !outstandingCopy(durable) {
		t.Fatalf("the surviving record lost the outstanding-copy state: %+v", durable)
	}

	// With the failure cleared, the copy cadence converges on the recorded generation.
	for _, trigger := range []string{"r6_status_later", "r6_status_record"} {
		if _, err := s.store.Exec(`DROP TRIGGER ` + trigger); err != nil {
			t.Fatal(err)
		}
	}
	s.config.BackupCopyDir = t.TempDir()
	if _, err := s.repairCopy(s.ctx); err != nil {
		t.Fatalf("copy repair: %v", err)
	}
	repaired := s.backupStatus()
	if !repaired.recoverable() || repaired.RecoverableID != next || repaired.CopyError != "" {
		t.Fatalf("the recovered record did not converge on the current generation: %+v", repaired)
	}
}

// Copy repairs must satisfy the same durability prerequisite as fresh snapshots.
// Interrupted snapshots may have left additional verified archives outside the ledger;
// they may be pruned only after this repair can persist its outcome.
func TestCopyRepairDoesNotPruneBeforeItsStatusIsDurable(t *testing.T) {
	for _, copyAvailable := range []bool{false, true} {
		name := "copy-unavailable"
		if copyAvailable {
			name = "copy-published"
		}
		t.Run(name, func(t *testing.T) {
			s, handler := testServer(t)
			setupAdmin(t, s, handler)
			s.settings.BackupKeep = 1
			blocked := filepath.Join(s.config.DataDir, "blocked")
			if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			s.config.BackupCopyDir = blocked
			if _, err := s.runBackup(s.ctx, true); err != nil {
				t.Fatal(err)
			}
			previous := s.backupStatus()
			var unrecorded []string
			for range 2 {
				path := filepath.Join(s.config.DataDir, "backups", db.ID()+".zip")
				if err := backup.Create(s.ctx, s.store, s.config, path, true); err != nil {
					t.Fatal(err)
				}
				unrecorded = append(unrecorded, path)
			}
			if copyAvailable {
				s.config.BackupCopyDir = t.TempDir()
			}
			if _, err := s.store.Exec(`CREATE TRIGGER repair_status_failure BEFORE UPDATE OF value ON settings WHEN NEW.key='backup-status' BEGIN SELECT RAISE(FAIL,'injected repair status failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.repairCopy(s.ctx); err == nil {
				t.Fatal("an unrecorded repair must report the persistence failure")
			}
			assertArchive(t, filepath.Join(s.config.DataDir, "backups", previous.Name), previous.Digest)
			for _, path := range unrecorded {
				if _, _, err := backup.VerifyArchiveIn(s.ctx, path, filepath.Join(s.config.DataDir, "tmp")); err != nil {
					t.Fatalf("repair pruned a verified archive before persisting its outcome: %v", err)
				}
			}
			// A new process must read the same durable identity. Close both the old
			// server and its store rather than merely opening a second connection.
			s.Close()
			if err := s.store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := db.Open(s.config.DBFile)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { reopened.Close() })
			restarted, err := New(s.config, reopened, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			restarted.settings.BackupKeep = 1
			if restored := restarted.backupStatus(); restored.ID != previous.ID || restored.Digest != previous.Digest {
				t.Fatalf("reopen lost the recorded generation: %+v", restored)
			}
			if _, err := reopened.Exec(`DROP TRIGGER repair_status_failure`); err != nil {
				t.Fatal(err)
			}
			if !copyAvailable {
				restarted.config.BackupCopyDir = t.TempDir()
			}
			if _, err := restarted.repairCopy(restarted.ctx); err != nil {
				t.Fatalf("repair after status recovery: %v", err)
			}
			after := restarted.backupStatus()
			if after.RecoverableID != previous.ID || after.CopyError != "" {
				t.Fatalf("repair did not converge after status recovery: %+v", after)
			}
			assertArchive(t, filepath.Join(restarted.config.BackupCopyDir, after.CopyName), after.CopyDigest)
			if _, err := os.Stat(unrecorded[0]); !os.IsNotExist(err) {
				t.Fatalf("successful repair did not resume bounded retention: %v", err)
			}
		})
	}
}
