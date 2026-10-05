package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"brrpbrrp/internal/db"
)

// ArchiveSummary is what verifying one archive establishes about it: the file's
// own digest and what the archive says it contains. The caller supplies it
// because the backup package depends on this one.
type ArchiveSummary struct {
	Digest          string
	IncludesUploads bool
	SchemaVersion   int
	Attachments     int
}

type ProductionCheckOptions struct {
	BackupMaxAge    time.Duration
	CertMinValidity time.Duration
	MinFreeBytes    uint64
	RequireCopy     bool
	Now             time.Time
	// VerifyArchive checks one archive's structure, inventory digests and database
	// integrity and reports its identity. Backup health depends on this rather than
	// on an archive merely existing, so a touched or truncated ZIP fails the check.
	VerifyArchive func(ctx context.Context, path string) (ArchiveSummary, error)
}

// CheckProduction is a host-side check for scheduled monitoring. It complements
// physical-device voice and push tests; it cannot verify either of those paths.
func CheckProduction(ctx context.Context, c Config, options ProductionCheckOptions) error {
	var problems []error
	if err := CheckHealth(ctx, c); err != nil {
		problems = append(problems, fmt.Errorf("local listener: %w", err))
	}
	if err := checkPublicHTTPS(ctx, c, options.Now, options.CertMinValidity); err != nil {
		problems = append(problems, fmt.Errorf("public HTTPS: %w", err))
	}
	if free, err := FreeDiskBytes(c.DataDir); err != nil {
		problems = append(problems, fmt.Errorf("free disk: %w", err))
	} else if free < options.MinFreeBytes {
		problems = append(problems, fmt.Errorf("free disk: %d bytes available, need at least %d", free, options.MinFreeBytes))
	}
	record, err := readGeneration(c.DBFile)
	if err != nil {
		problems = append(problems, fmt.Errorf("backup schedule: %w", err))
	} else if err := checkGeneration(ctx, c, record, options); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// generation is the recorded identity of the last successful backup: which files
// were written, whether they carry the uploaded files, and when. Health is decided
// from this record, never from a file's modification time, so touching an old
// archive cannot make it look fresh and an unrelated archive cannot stand in for
// the recovery copy.
type generation struct {
	Enabled bool
	// ScheduleUploads is what the schedule is set to do now, and GenerationUploads
	// is what the last recorded generation actually carries. They are separate facts:
	// a full backup taken before the schedule changed must not make a schedule that
	// has stopped including uploaded files look complete.
	ScheduleUploads   bool
	GenerationUploads bool
	Name              string
	CopyName          string
	Digest            string
	CopyDigest        string
	// SnapshotAt is when the recoverable data was captured, CompletedAt when the
	// backup job finished and CopiedAt when the extra copy was written. Health ages
	// the recovery point by the snapshot, not by the job that produced it.
	SnapshotAt  time.Time
	CompletedAt time.Time
	CopiedAt    time.Time
	// Error is the last failure of any kind; CopyError is the copy half of it. They are
	// read separately so a failed copy does not stop the check before it names the
	// recovery point the installation still has.
	Error     string
	CopyError string
	// RecoverableID and RecoverableCopiedAt describe the newest generation that is both
	// verified locally and copied to independent storage. When the recorded generation
	// has no copy, this is what the installation can still recover, and the failure says
	// so instead of only reporting that the latest attempt did not complete.
	// RecoverableSnapshotAt and RecoverableUploads describe that fallback's own recovery
	// age and coverage, which are different facts from the newest attempt's.
	RecoverableID         string
	RecoverableName       string
	RecoverableCopyName   string
	RecoverableCopyDigest string
	RecoverableCopiedAt   time.Time
	RecoverableSnapshotAt time.Time
	RecoverableUploads    bool
}

func readGeneration(database string) (generation, error) {
	var record generation
	public, err := db.ReadSetting(database, "public")
	if err != nil {
		return record, err
	}
	var settings struct {
		Enabled bool `json:"backup_enabled"`
		Uploads bool `json:"backup_include_uploads"`
	}
	if err := json.Unmarshal([]byte(public), &settings); err != nil {
		return record, err
	}
	record.Enabled, record.ScheduleUploads = settings.Enabled, settings.Uploads
	saved, err := db.ReadSetting(database, "backup-status")
	if err != nil {
		return record, err
	}
	var status struct {
		Name                      string `json:"name"`
		CopyName                  string `json:"copy_name"`
		Digest                    string `json:"digest"`
		CopyDigest                string `json:"copy_digest"`
		LastSuccess               string `json:"last_success"`
		LastCopy                  string `json:"last_copy"`
		SnapshotAt                string `json:"snapshot_at"`
		IncludesUploads           bool   `json:"includes_uploads"`
		Error                     string `json:"error"`
		CopyError                 string `json:"copy_error"`
		RecoverableID             string `json:"recoverable_id"`
		RecoverableName           string `json:"recoverable_name"`
		RecoverableCopyAt         string `json:"recoverable_copy_at"`
		RecoverableCopyName       string `json:"recoverable_copy_name"`
		RecoverableCopyDigest     string `json:"recoverable_copy_digest"`
		RecoverableSnapshotAt     string `json:"recoverable_snapshot_at"`
		RecoverableIncludesUpload bool   `json:"recoverable_includes_uploads"`
	}
	if err := json.Unmarshal([]byte(saved), &status); err != nil {
		return record, err
	}
	record.Name, record.CopyName = status.Name, status.CopyName
	record.Digest, record.CopyDigest = status.Digest, status.CopyDigest
	record.Error, record.CopyError = status.Error, status.CopyError
	record.GenerationUploads = status.IncludesUploads
	record.CompletedAt, _ = time.Parse(time.RFC3339Nano, status.LastSuccess)
	record.CopiedAt, _ = time.Parse(time.RFC3339Nano, status.LastCopy)
	record.SnapshotAt, _ = time.Parse(time.RFC3339Nano, status.SnapshotAt)
	record.RecoverableID = status.RecoverableID
	record.RecoverableName = status.RecoverableName
	record.RecoverableCopyName = status.RecoverableCopyName
	record.RecoverableCopyDigest = status.RecoverableCopyDigest
	record.RecoverableCopiedAt, _ = time.Parse(time.RFC3339Nano, status.RecoverableCopyAt)
	record.RecoverableSnapshotAt, _ = time.Parse(time.RFC3339Nano, status.RecoverableSnapshotAt)
	record.RecoverableUploads = status.RecoverableIncludesUpload
	if status.RecoverableCopyName != "" && status.RecoverableCopyDigest == "" {
		// A recovery point without an identity cannot be verified, so it is not one.
		record.RecoverableID = ""
	}
	// Missing or malformed legacy snapshot times stay unknown. A completion or copy
	// timestamp cannot establish when the archived data was captured: using it could
	// make a long-running backup, or a delayed copy of an old archive, appear fresh.
	return record, nil
}

// checkGeneration decides backup health from what the installation recorded, and verifies
// that the archives it names are the generation it recorded. Every problem it finds is
// reported together: a failed newest copy and an older usable recovery point are two
// facts, and neither may hide the other.
func checkGeneration(ctx context.Context, c Config, record generation, options ProductionCheckOptions) error {
	if !record.Enabled {
		return errors.New("backup schedule: scheduled backups are not enabled")
	}
	var problems []error
	// The schedule and the recorded generation are reported separately. A schedule that
	// has stopped including uploaded files is a current-policy problem even when the last
	// generation is complete, and a database-only generation is incomplete material even
	// when the schedule is set to include them.
	if !record.ScheduleUploads {
		problems = append(problems, errors.New("backup schedule: scheduled backups no longer include uploaded files, so a recovery point would omit them"))
	}
	if !record.GenerationUploads {
		problems = append(problems, errors.New("backup schedule: the recorded backup is database-only and would not restore uploaded files"))
	}
	if record.Error != "" {
		problems = append(problems, fmt.Errorf("backup schedule: latest attempt failed: %s", record.Error))
	}
	if record.Name == "" || record.CompletedAt.IsZero() {
		problems = append(problems, errors.New("local backup: no successful backup has been recorded"))
		return errors.Join(problems...)
	}
	// The local recovery point is the data inside the archive. A backup that took two hours
	// is two hours older than it looks if it is dated by its completion time. The
	// classification is the shared model's, so the command-line check and the running
	// server's alerts cannot disagree about what "unknown" or "too old" means.
	local := AssessSnapshotAt(record.SnapshotAt, options.Now)
	switch {
	case !local.Known:
		problems = append(problems, errors.New("local backup: snapshot time is unknown; create a new backup to establish recovery freshness"))
	case local.Future || local.Age > options.BackupMaxAge:
		problems = append(problems, fmt.Errorf("local backup: the recoverable snapshot is %s old; maximum is %s", local.Age.Round(time.Minute), options.BackupMaxAge))
	}
	if err := checkArchive(ctx, filepath.Join(c.DataDir, "backups", record.Name), record.Digest, record.GenerationUploads, options, "local backup"); err != nil {
		problems = append(problems, err)
	}
	if c.BackupCopyDir == "" {
		if options.RequireCopy {
			problems = append(problems, errors.New("backup copy: BRRP_BACKUP_COPY_DIR is not configured"))
		}
		return errors.Join(problems...)
	}
	// The independent recovery point. A copy of a different, valid archive is not a
	// recovery of this generation, so the recorded copy must also be the recorded digest.
	recordedCopy := record.CopyName != "" && record.CopyDigest != "" && !record.CopiedAt.IsZero()
	if recordedCopy && record.CopyDigest == record.Digest {
		if copied := AssessSnapshotAt(record.SnapshotAt, options.Now); copied.Known && copied.Age > options.BackupMaxAge {
			problems = append(problems, fmt.Errorf("backup copy: the copied snapshot is %s old; maximum is %s", copied.Age.Round(time.Minute), options.BackupMaxAge))
		}
		if err := checkArchive(ctx, filepath.Join(c.BackupCopyDir, record.CopyName), record.CopyDigest, record.GenerationUploads, options, "backup copy"); err != nil {
			problems = append(problems, err)
		}
		return errors.Join(problems...)
	}
	if recordedCopy {
		problems = append(problems, errors.New("backup copy: the recorded copy is a different recovery generation from the local backup"))
	}
	// The newest generation has no verified independent copy. Naming and verifying the
	// generation that is still recoverable is the difference between an operator knowing
	// what they have and knowing only that the latest attempt failed. A fallback that does
	// not verify is not a recovery point, and is reported as unusable rather than usable.
	if record.RecoverableID == "" || record.RecoverableCopyName == "" || record.RecoverableCopyDigest == "" {
		problems = append(problems, errors.New("backup copy: no verified copy of the recorded backup"))
		return errors.Join(problems...)
	}
	problems = append(problems, fallbackProblem(ctx, c, record, options))
	return errors.Join(problems...)
}

// fallbackProblem describes the newest generation that is still independently
// recoverable, in terms of its own snapshot age and coverage, and verifies its archive.
// The age is the fallback's snapshot, not the failed attempt's, so an older but usable
// recovery point is not reported as fresh and a fresh but unusable one is not reported as
// usable.
func fallbackProblem(ctx context.Context, c Config, record generation, options ProductionCheckOptions) error {
	snapshotAt := record.RecoverableSnapshotAt
	aged := AssessSnapshotAt(snapshotAt, options.Now)
	age := aged.Age
	var reasons []string
	ageDescription := fmt.Sprintf("snapshot %s old", age.Round(time.Minute))
	switch {
	case !aged.Known:
		ageDescription = "snapshot age unknown"
		reasons = append(reasons, "its snapshot time is unknown; copy completion does not establish recovery freshness")
	case aged.Future:
		ageDescription = "snapshot age invalid"
		reasons = append(reasons, "its snapshot time is in the future; check the host clock")
	case age > options.BackupMaxAge:
		reasons = append(reasons, fmt.Sprintf("it is older than the %s maximum", options.BackupMaxAge))
	}
	coverage := "database-only"
	if record.RecoverableUploads {
		coverage = "database and uploads"
	}
	identity := fmt.Sprintf("the last independently recoverable generation is %s (%s, %s, %s)",
		record.RecoverableID, record.RecoverableCopyName, ageDescription, coverage)
	// Its age and its validity are separate facts, and both are stated: an old but readable
	// fallback is still a recovery point, and a fresh one that does not verify is not.
	if err := checkArchive(ctx, filepath.Join(c.BackupCopyDir, record.RecoverableCopyName), record.RecoverableCopyDigest, record.RecoverableUploads, options, "backup copy fallback"); err != nil {
		reasons = append(reasons, fmt.Sprintf("it does not verify: %v", err))
	}
	if len(reasons) == 0 {
		return fmt.Errorf("backup copy: no verified copy of the recorded backup; %s", identity)
	}
	return fmt.Errorf("backup copy: no verified copy of the recorded backup; %s, but %s", identity, strings.Join(reasons, ", and "))
}

// checkArchive establishes that one archive is readable, is the generation that
// was recorded, was written for the schema this build serves, and covers the same
// uploads. A missing verifier can only confirm that the file is present, which is
// reported as exactly that.
func checkArchive(ctx context.Context, path, expectedDigest string, includesUploads bool, options ProductionCheckOptions, label string) error {
	if options.VerifyArchive == nil {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	summary, err := options.VerifyArchive(ctx, path)
	if err != nil {
		return fmt.Errorf("%s integrity: %w", label, err)
	}
	if expectedDigest != "" && summary.Digest != expectedDigest {
		return fmt.Errorf("%s integrity: the archive is not the one the installation recorded", label)
	}
	// An archive written for a newer schema than this build serves is not a recovery
	// point: nothing here can read it, so it cannot be migrated forward either. An
	// archive written for an older schema is a recovery point: restoring it migrates
	// it forward, and the writes it does not contain are what the freshness check
	// above exists to bound. A pre-upgrade snapshot is exactly that, and it is the
	// baseline an operator keeps while the new schema settles.
	if summary.SchemaVersion > db.SchemaVersion {
		return fmt.Errorf("%s integrity: the archive was written for schema %d and this build serves schema %d, so this build cannot restore it", label, summary.SchemaVersion, db.SchemaVersion)
	}
	if includesUploads && !summary.IncludesUploads {
		return fmt.Errorf("%s integrity: the archive does not contain the uploaded files the recorded backup covers", label)
	}
	return nil
}

func checkPublicHTTPS(ctx context.Context, c Config, now time.Time, minValidity time.Duration) error {
	if !strings.HasPrefix(c.BaseURL, "https://") {
		return errors.New("the configured browser origin is not HTTPS")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if path := LocalCAFile(c); path != "" {
		pem, err := os.ReadFile(path)
		if err == nil {
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("invalid local CA certificate at %s", path)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/setup/status", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("setup status returned HTTP %d", response.StatusCode)
	}
	if response.TLS == nil || len(response.TLS.PeerCertificates) == 0 {
		return errors.New("no TLS certificate was presented")
	}
	if remaining := response.TLS.PeerCertificates[0].NotAfter.Sub(now); remaining < minValidity {
		return fmt.Errorf("certificate expires in %s", remaining.Round(time.Hour))
	}
	var status struct {
		SetupRequired *bool `json:"setup_required"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&status); err != nil || status.SetupRequired == nil {
		return errors.New("setup status response is invalid")
	}
	if *status.SetupRequired {
		return errors.New("first-time setup is incomplete")
	}
	return nil
}
