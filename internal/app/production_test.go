package app

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// generationFixture is one installation with a recorded backup generation and
// matching archives, which is what the check now reads instead of file times.
type generationFixture struct {
	config    Config
	store     *db.Store
	localPath string
	copyPath  string
}

func newGenerationFixture(t *testing.T) generationFixture {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"setup_required":false}`))
	}))
	t.Cleanup(server.Close)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	copyDir := filepath.Join(directory, "copy")
	for _, path := range []string{filepath.Join(directory, "certs"), filepath.Join(directory, "backups"), copyDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	root := filepath.Join(directory, "certs", "root.crt")
	if err := os.WriteFile(root, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(directory, "brrp.db")
	store, err := db.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	localPath := filepath.Join(directory, "backups", "generation.zip")
	copyPath := filepath.Join(copyDir, "brrpbrrp-generation.zip")
	for _, path := range []string{localPath, copyPath} {
		if err := os.WriteFile(path, []byte("backup bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fixture := generationFixture{
		config:    Config{DataDir: directory, DBFile: database, BackupCopyDir: copyDir, BaseURL: server.URL, Addr: strings.TrimPrefix(server.URL, "https://"), TLSMode: "https", CertFile: root},
		store:     store,
		localPath: localPath,
		copyPath:  copyPath,
	}
	lock, err := LockData(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	fixture.record(t, db.Now(), db.Now())
	return fixture
}

// record writes the settings rows the check reads: the schedule and the recorded
// generation. The copy is a byte copy of the archive, so it carries the same
// digest unless a test deliberately records a different generation.
func (f generationFixture) record(t *testing.T, lastSuccess, lastCopy string) {
	t.Helper()
	f.recordDigests(t, lastSuccess, lastCopy, "digest-local", "digest-local")
}

func (f generationFixture) recordDigests(t *testing.T, lastSuccess, lastCopy, digest, copyDigest string) {
	t.Helper()
	status := `{"name":"generation.zip","copy_name":"brrpbrrp-generation.zip","digest":"` + digest + `","copy_digest":"` + copyDigest + `","last_success":"` + lastSuccess + `","snapshot_at":"` + lastSuccess + `","last_copy":"` + lastCopy + `","includes_uploads":true,"error":""}`
	if _, err := f.store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('public',?,?),('backup-status',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`,
		`{"backup_enabled":true,"backup_include_uploads":true}`, db.Now(), status, db.Now()); err != nil {
		t.Fatal(err)
	}
}

func (f generationFixture) options() ProductionCheckOptions {
	return ProductionCheckOptions{
		BackupMaxAge: time.Hour, CertMinValidity: time.Hour, RequireCopy: true, Now: time.Now(),
		VerifyArchive: func(_ context.Context, path string) (ArchiveSummary, error) {
			digest := "digest-local"
			if strings.Contains(path, "brrpbrrp-") {
				digest = "digest-copy"
			}
			return ArchiveSummary{Digest: digest, IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
		},
	}
}

// TestProductionCheckUsesTheRecordedGeneration covers F-11: health is decided from
// the recorded recovery generation, not from a file's modification time.
func TestProductionCheckUsesTheRecordedGeneration(t *testing.T) {
	fixture := newGenerationFixture(t)
	options := fixture.options()
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err != nil {
		t.Fatalf("a matching generation failed: %v", err)
	}

	// Touching an old archive does not make it fresh. The recorded time is the
	// snapshot time, so an archive whose contents are a week old stays old no
	// matter what its modification time says.
	fixture.record(t, db.Timestamp(time.Now().Add(-72*time.Hour)), db.Timestamp(time.Now().Add(-72*time.Hour)))
	if err := os.Chtimes(fixture.localPath, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.copyPath, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	err := CheckProduction(t.Context(), fixture.config, options)
	if err == nil || !strings.Contains(err.Error(), "recoverable snapshot is") {
		t.Fatalf("a touched old archive passed freshness: %v", err)
	}
	fixture.record(t, db.Now(), db.Now())

	// An unrelated archive that verifies is not the recorded generation.
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "some-other-archive", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "not the one the installation recorded") {
		t.Fatalf("a mismatched archive passed identity: %v", err)
	}

	// A copy from a different generation fails even though both archives verify.
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		digest := "digest-local"
		if strings.Contains(path, "brrpbrrp-") {
			digest = "digest-copy"
		}
		return ArchiveSummary{Digest: digest, IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	fixture.recordDigests(t, db.Now(), db.Now(), "digest-local", "digest-copy")
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "different recovery generation") {
		t.Fatalf("a copy from another generation passed: %v", err)
	}
	fixture.record(t, db.Now(), db.Now())

	// A copy that does not carry the uploaded files the generation covers fails.
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: false, SchemaVersion: db.SchemaVersion}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "uploaded files") {
		t.Fatalf("a database-only archive passed as the recovery generation: %v", err)
	}

	// An archive written for a newer schema than this build serves cannot be
	// restored by it, so it is not a recovery point.
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion + 1}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "cannot restore it") {
		t.Fatalf("an archive this build cannot restore passed: %v", err)
	}

	// A pre-upgrade snapshot is a recovery point: restoring it migrates it forward.
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion - 1}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err != nil {
		t.Fatalf("a pre-upgrade snapshot was refused as a recovery point: %v", err)
	}

	// A failed attempt is reported even when the archived generation is intact.
	fixture.record(t, db.Now(), db.Now())
	if _, err := fixture.store.Exec(`UPDATE settings SET value=json_set(value,'$.error','Local backup succeeded; extra copy failed') WHERE key='backup-status'`); err != nil {
		t.Fatal(err)
	}
	if err := CheckProduction(t.Context(), fixture.config, fixture.options()); err == nil || !strings.Contains(err.Error(), "latest attempt failed") {
		t.Fatalf("a failed copy attempt passed: %v", err)
	}
	fixture.record(t, db.Now(), db.Now())

	// A missing copy is a missing recovery point, not a stale file.
	if err := os.Remove(fixture.copyPath); err != nil {
		t.Fatal(err)
	}
	if err := CheckProduction(t.Context(), fixture.config, fixture.options()); err == nil || !strings.Contains(err.Error(), "backup copy") {
		t.Fatalf("a missing copy passed: %v", err)
	}
}

// writeGeneration records a backup-status row exactly as a test describes it, so a
// check can be judged against one fact at a time.
func (f generationFixture) writeGeneration(t *testing.T, status map[string]any, scheduleUploads bool) {
	t.Helper()
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	public := `{"backup_enabled":true,"backup_include_uploads":` + map[bool]string{true: "true", false: "false"}[scheduleUploads] + `}`
	if _, err := f.store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('public',?,?),('backup-status',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`,
		public, db.Now(), string(encoded), db.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestProductionCheckSeparatesScheduleFromGeneration covers the two facts review
// R2-08 found merged into one: the schedule's current coverage and the recorded
// generation's actual coverage. The old check took their logical OR, so a full
// backup taken before the schedule changed hid a schedule that had stopped
// including uploaded files.
func TestProductionCheckSeparatesScheduleFromGeneration(t *testing.T) {
	fixture := newGenerationFixture(t)
	now := time.Now()
	base := map[string]any{
		"name": "generation.zip", "copy_name": "brrpbrrp-generation.zip",
		"digest": "digest-local", "copy_digest": "digest-local",
		"last_success": db.Timestamp(now), "last_copy": db.Timestamp(now),
		"snapshot_at": db.Timestamp(now), "includes_uploads": true, "error": "",
	}
	options := fixture.options()
	// Both archives are byte copies of one generation, which is what the fixture
	// writes: they carry the same digest.
	options.VerifyArchive = func(_ context.Context, _ string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}

	// The schedule stopped including uploads, but the last generation is complete.
	fixture.writeGeneration(t, base, false)
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "no longer include uploaded files") {
		t.Fatalf("a schedule without upload coverage passed: %v", err)
	}

	// The schedule includes them and the last generation does not: the recovery
	// point itself is incomplete, which is a different message.
	incomplete := map[string]any{}
	for key, value := range base {
		incomplete[key] = value
	}
	incomplete["includes_uploads"] = false
	fixture.writeGeneration(t, incomplete, true)
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "database-only") {
		t.Fatalf("a database-only generation passed: %v", err)
	}

	// A long backup is dated by its snapshot, not by the job that produced it: an
	// archive that finished now but captured its data three hours ago is three hours
	// old as a recovery point.
	slow := map[string]any{}
	for key, value := range base {
		slow[key] = value
	}
	slow["snapshot_at"] = db.Timestamp(now.Add(-3 * time.Hour))
	fixture.writeGeneration(t, slow, true)
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "recoverable snapshot is") {
		t.Fatalf("a slow backup reported a fresh recovery point: %v", err)
	}

	// The same generation with a current snapshot passes, so the failures above are
	// about the facts rather than about the fixture.
	fixture.writeGeneration(t, base, true)
	if err := CheckProduction(t.Context(), fixture.config, options); err != nil {
		t.Fatalf("a complete current generation failed: %v", err)
	}
}

// TestProductionCheckNamesTheFallbackRecoveryPointWhenTheNewestCopyFailed covers the
// reporting gap review 4 recorded for R4-03/R4-04: the general failure branch returned
// before the missing-copy branch could identify the generation that is still recoverable,
// and its fallback view carried no snapshot age or coverage. A failed newest copy and an
// older usable recovery point are now reported together, and the fallback is described by
// its own snapshot age and coverage.
func TestProductionCheckNamesTheFallbackRecoveryPointWhenTheNewestCopyFailed(t *testing.T) {
	fixture := newGenerationFixture(t)
	now := time.Now()
	const fallbackID = "0198f000-0000-7000-8000-000000000009"
	fixture.writeGeneration(t, map[string]any{
		"name": "generation.zip", "digest": "digest-local",
		"last_success": db.Timestamp(now), "snapshot_at": db.Timestamp(now),
		"includes_uploads":             true,
		"error":                        "local backup succeeded; extra copy failed",
		"copy_error":                   "local backup succeeded; extra copy failed",
		"recoverable_id":               fallbackID,
		"recoverable_name":             fallbackID + ".zip",
		"recoverable_copy_name":        "brrpbrrp-" + fallbackID + ".zip",
		"recoverable_copy_digest":      "digest-fallback",
		"recoverable_copy_at":          db.Timestamp(now.Add(-30 * time.Minute)),
		"recoverable_snapshot_at":      db.Timestamp(now.Add(-30 * time.Minute)),
		"recoverable_includes_uploads": true,
	}, true)

	options := fixture.options()
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		digest := "digest-local"
		if strings.Contains(path, fallbackID) {
			digest = "digest-fallback"
		}
		return ArchiveSummary{Digest: digest, IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	err := CheckProduction(t.Context(), fixture.config, options)
	if err == nil {
		t.Fatal("a failed newest copy was reported as healthy")
	}
	message := err.Error()
	if !strings.Contains(message, "latest attempt failed") {
		t.Fatalf("the failed attempt was not reported: %v", err)
	}
	if !strings.Contains(message, fallbackID) {
		t.Fatalf("the last independently recoverable generation was not named: %v", err)
	}
	if !strings.Contains(message, "snapshot 30m0s old") {
		t.Fatalf("the fallback's own snapshot age was not reported: %v", err)
	}
	if !strings.Contains(message, "database and uploads") {
		t.Fatalf("the fallback's coverage was not reported: %v", err)
	}
	if strings.Contains(message, "but ") {
		t.Fatalf("a fresh, verifiable fallback was reported as unusable: %v", err)
	}

	// A fallback whose own snapshot is older than the maximum is reported with that
	// reason, not hidden behind the newest attempt's failure.
	fixture.writeGeneration(t, map[string]any{
		"name": "generation.zip", "digest": "digest-local",
		"last_success": db.Timestamp(now), "snapshot_at": db.Timestamp(now),
		"includes_uploads":             true,
		"recoverable_id":               fallbackID,
		"recoverable_copy_name":        "brrpbrrp-" + fallbackID + ".zip",
		"recoverable_copy_digest":      "digest-fallback",
		"recoverable_copy_at":          db.Timestamp(now.Add(-3 * time.Hour)),
		"recoverable_snapshot_at":      db.Timestamp(now.Add(-3 * time.Hour)),
		"recoverable_includes_uploads": true,
	}, true)
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "older than the 1h0m0s maximum") {
		t.Fatalf("an aged fallback was not reported as aged: %v", err)
	}

	// A fallback that does not verify is reported as unusable rather than as usable.
	fixture.writeGeneration(t, map[string]any{
		"name": "generation.zip", "digest": "digest-local",
		"last_success": db.Timestamp(now), "snapshot_at": db.Timestamp(now),
		"includes_uploads":             true,
		"recoverable_id":               fallbackID,
		"recoverable_copy_name":        "brrpbrrp-" + fallbackID + ".zip",
		"recoverable_copy_digest":      "digest-fallback",
		"recoverable_copy_at":          db.Timestamp(now.Add(-30 * time.Minute)),
		"recoverable_snapshot_at":      db.Timestamp(now.Add(-30 * time.Minute)),
		"recoverable_includes_uploads": true,
	}, true)
	options.VerifyArchive = func(_ context.Context, path string) (ArchiveSummary, error) {
		if strings.Contains(path, fallbackID) {
			return ArchiveSummary{}, errors.New("archive does not match its recorded digest")
		}
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("an unusable fallback was reported as a recovery point: %v", err)
	}
}

// TestProductionCheckReportsCertificateFailures keeps the certificate half of the
// host check honest alongside the backup half.
func TestProductionCheckReportsCertificateFailures(t *testing.T) {
	fixture := newGenerationFixture(t)
	options := fixture.options()
	options.CertMinValidity = 100 * 365 * 24 * time.Hour
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "certificate expires") {
		t.Fatalf("certificate threshold was not checked: %v", err)
	}
	options.CertMinValidity = time.Hour
	options.VerifyArchive = func(context.Context, string) (ArchiveSummary, error) {
		return ArchiveSummary{}, errors.New("archive does not match its recorded digest")
	}
	if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "local backup integrity") {
		t.Fatalf("unreadable local archive passed: %v", err)
	}
}

func TestProductionCheckDoesNotDateUnknownSnapshotsByCompletion(t *testing.T) {
	fixture := newGenerationFixture(t)
	options := fixture.options()
	options.VerifyArchive = func(context.Context, string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	for _, stamp := range []string{"", "not-a-timestamp"} {
		fixture.writeGeneration(t, map[string]any{
			"name": "generation.zip", "copy_name": "brrpbrrp-generation.zip",
			"digest": "digest-local", "copy_digest": "digest-local",
			"last_success": db.Now(), "last_copy": db.Now(), "snapshot_at": stamp,
			"includes_uploads": true,
		}, true)
		if err := CheckProduction(t.Context(), fixture.config, options); err == nil || !strings.Contains(err.Error(), "snapshot time is unknown") {
			t.Fatalf("completion time made unknown snapshot %q look fresh: %v", stamp, err)
		}
	}
	fixture.writeGeneration(t, map[string]any{
		"name": "generation.zip", "copy_name": "brrpbrrp-generation.zip",
		"digest": "digest-local", "copy_digest": "digest-local",
		"last_success": db.Now(), "last_copy": db.Now(), "snapshot_at": db.Now(),
		"includes_uploads": true,
	}, true)
	if err := CheckProduction(t.Context(), fixture.config, options); err != nil {
		t.Fatalf("a verified generation with a known recent snapshot should pass: %v", err)
	}
}

func TestProductionCheckDoesNotInventFallbackSnapshotAge(t *testing.T) {
	fixture := newGenerationFixture(t)
	options := fixture.options()
	options.VerifyArchive = func(context.Context, string) (ArchiveSummary, error) {
		return ArchiveSummary{Digest: "digest-local", IncludesUploads: true, SchemaVersion: db.SchemaVersion}, nil
	}
	for _, test := range []struct{ stamp, want string }{
		{"", "snapshot time is unknown"},
		{"not-a-timestamp", "snapshot time is unknown"},
		{db.Timestamp(options.Now.Add(time.Hour)), "snapshot time is in the future"},
	} {
		fixture.writeGeneration(t, map[string]any{
			"name": "generation.zip", "digest": "digest-local",
			"last_success": db.Now(), "snapshot_at": db.Now(), "includes_uploads": true,
			"recoverable_id": "fallback", "recoverable_copy_name": "brrpbrrp-fallback.zip",
			"recoverable_copy_digest": "digest-local", "recoverable_copy_at": db.Now(),
			"recoverable_snapshot_at": test.stamp, "recoverable_includes_uploads": true,
		}, true)
		err := CheckProduction(t.Context(), fixture.config, options)
		if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "fallback") {
			t.Fatalf("fallback snapshot %q had misleading age: %v", test.stamp, err)
		}
	}
}
