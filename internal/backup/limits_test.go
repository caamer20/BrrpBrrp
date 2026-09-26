package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

// manyAttachmentInstallation builds an installation with the requested number of
// one-byte attachments, which is the shape that reproduced the defect.
func manyAttachmentInstallation(t *testing.T, count int) (app.Config, *db.Store) {
	t.Helper()
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(config.DataDir, "backups"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWithOptions(config.DBFile, db.Options{Synchronous: config.SQLiteSynchronous})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now, userID := db.Now(), db.ID()
	if _, err := store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, userID, "owner", "unused-test-hash", now, now); err != nil {
		t.Fatal(err)
	}
	for range count {
		id := db.ID()
		if err := os.WriteFile(filepath.Join(config.UploadDir, id), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES(?,?,?,?,?,?,?)`,
			id, userID, "tiny.bin", id, 1, "application/octet-stream", now); err != nil {
			t.Fatal(err)
		}
	}
	return config, store
}

// TestManySmallAttachmentsCreateVerifyAndRestore is the permanent regression for
// F-10. Six hundred one-byte attachments used to produce an 84 KB manifest inside
// a 164 KB archive that creation accepted and both VerifyArchive and Restore
// rejected, because the reader's manifest ceiling was 64 KiB.
func TestManySmallAttachmentsCreateVerifyAndRestore(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 600)
	archive := filepath.Join(config.DataDir, "backups", "many.zip")
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	manifest, digest, err := VerifyArchive(context.Background(), archive)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(manifest.Attachments) != 600 {
		t.Fatalf("manifest attachments = %d, want 600", len(manifest.Attachments))
	}
	if digest == "" {
		t.Fatal("verification returned no digest")
	}

	// Restoration into a second, empty data directory must reproduce every file.
	t.Setenv("BRRP_UPLOAD_DIR", "")
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	restored, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Prepare(); err != nil {
		t.Fatal(err)
	}
	// Restore only accepts an archive named inside its own data directory.
	staged := filepath.Join(restored.DataDir, "many.zip")
	copyFile(t, archive, staged)
	if err := Restore(context.Background(), restored, staged); err != nil {
		t.Fatalf("restore: %v", err)
	}
	entries, err := os.ReadDir(restored.UploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 600 {
		t.Fatalf("restored attachments = %d, want 600", len(entries))
	}
}

// TestCreateRefusesWhatTheReaderWouldReject proves the limits are shared rather
// than reader-only: a data set that cannot fit inside the declared support profile
// fails creation with an explanation, and leaves no archive behind.
func TestCreateRefusesWhatTheReaderWouldReject(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 0)
	// A manifest far beyond its ceiling: entries are added to exceed the limit
	// with a small number of large names is impossible, so the aggregate and the
	// attachment count are exercised directly instead.
	if err := (&archiveBudget{attachments: MaxAttachments}).addAttachment(1); err == nil {
		t.Fatal("the entry budget accepted more attachments than one archive supports")
	} else if !strings.Contains(err.Error(), "stored files") {
		t.Fatalf("entry budget error = %v", err)
	}
	if err := (&archiveBudget{}).add(maxArchiveBytes + 1); err == nil {
		t.Fatal("the size budget accepted a data set larger than one archive supports")
	} else if !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("size budget error = %v", err)
	}

	// The declared profile is what creation, verification and restoration all use.
	profile := SupportProfile()
	if profile["max_manifest_bytes"] != maxManifestBytes || profile["max_attachments"] != MaxAttachments {
		t.Fatalf("support profile = %v", profile)
	}

	archive := filepath.Join(config.DataDir, "backups", "empty.zip")
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var manifestSize int64
	for _, entry := range reader.File {
		if entry.Name == manifestEntry {
			manifestSize = int64(entry.UncompressedSize64)
		}
	}
	if manifestSize == 0 || manifestSize > maxManifestBytes {
		t.Fatalf("manifest size = %d, ceiling %d", manifestSize, int64(maxManifestBytes))
	}
}

// rebuildArchive rebuilds an archive from its own entries, applying a mutation to
// the manifest and to the entry set. It is how a hostile or damaged archive is
// produced without hand-writing ZIP bytes.
func rebuildArchive(t *testing.T, source, destination string, mutate func(manifest *Manifest, entries map[string][]byte)) {
	t.Helper()
	reader, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := map[string][]byte{}
	var manifest Manifest
	for _, entry := range reader.File {
		input, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(input)
		input.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == manifestEntry {
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatal(err)
			}
			continue
		}
		entries[entry.Name] = content
	}
	mutate(&manifest, entries)
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, content := range entries {
		record, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := record.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	record, err := writer.Create(manifestEntry)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(record).Encode(manifest); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestVerificationAppliesTheChecksRestoreApplies covers the F-11 verifier/restore
// alignment: a verification must reject what a restoration would reject, including
// a manifest that disagrees with the schema inside the database and an archive
// whose attachment coverage does not match its own database.
func TestVerificationAppliesTheChecksRestoreApplies(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 2)
	archive := filepath.Join(config.DataDir, "backups", "coverage.zip")
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := VerifyArchive(context.Background(), archive); err != nil {
		t.Fatalf("a complete archive failed verification: %v", err)
	}
	rewritten := filepath.Join(config.DataDir, "backups", "rewritten.zip")

	// The manifest claims a schema its own database is not.
	rebuildArchive(t, archive, rewritten, func(manifest *Manifest, _ map[string][]byte) {
		manifest.SchemaVersion = 1
	})
	if _, _, err := VerifyArchive(context.Background(), rewritten); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("a manifest/database schema disagreement verified: %v", err)
	}

	// An attachment the database references is missing from the archive.
	rebuildArchive(t, archive, rewritten, func(manifest *Manifest, entries map[string][]byte) {
		if len(manifest.Attachments) == 0 {
			t.Fatal("the fixture archive carries no attachments")
		}
		delete(entries, manifest.Attachments[0].Name)
		manifest.Attachments = manifest.Attachments[1:]
	})
	if _, _, err := VerifyArchive(context.Background(), rewritten); err == nil || !strings.Contains(err.Error(), "missing an attachment") {
		t.Fatalf("an archive missing a referenced attachment verified: %v", err)
	}

	// The archive carries a file the database does not reference. Its inventory
	// record is genuine, so only the coverage check can refuse it.
	rebuildArchive(t, archive, rewritten, func(manifest *Manifest, entries map[string][]byte) {
		name := uploadsPrefix + "00000000-0000-7000-8000-000000000000"
		content := []byte("an attachment no message references")
		entries[name] = content
		sum := sha256.Sum256(content)
		manifest.Attachments = append(manifest.Attachments, Inventory{Name: name, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])})
	})
	if _, _, err := VerifyArchive(context.Background(), rewritten); err == nil || !strings.Contains(err.Error(), "does not reference") {
		t.Fatalf("an archive carrying an unreferenced file verified: %v", err)
	}

	// The archive claims to hold no uploads while its database references them, and
	// claims to exclude none. A database-only archive is supported, but it has to
	// declare what it leaves out: this one disagrees with its own database.
	rebuildArchive(t, archive, rewritten, func(manifest *Manifest, entries map[string][]byte) {
		for _, attachment := range manifest.Attachments {
			delete(entries, attachment.Name)
		}
		manifest.Attachments, manifest.IncludesUploads, manifest.ExcludedUploads = nil, false, 0
	})
	if _, _, err := VerifyArchive(context.Background(), rewritten); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("an archive whose exclusion count disagrees with its database verified: %v", err)
	}
	// Restoration refuses the same archive, which is the disagreement review R2-07
	// reproduced: verification rejected it and a restore accepted it.
	restored := emptyInstallation(t)
	staged := filepath.Join(restored.DataDir, "disagreement.zip")
	copyFile(t, rewritten, staged)
	if err := Restore(context.Background(), restored, staged); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("restoration accepted an archive verification refuses: %v", err)
	}
	if _, err := os.Stat(restored.DBFile); !os.IsNotExist(err) {
		t.Fatal("a refused restore created a database")
	}

	// A manifest that records a schema its database is not is the other half: the
	// verifier and the restorer must answer the same way.
	rebuildArchive(t, archive, rewritten, func(manifest *Manifest, _ map[string][]byte) {
		manifest.SchemaVersion = 1
	})
	agreement := emptyInstallation(t)
	staged = filepath.Join(agreement.DataDir, "schema.zip")
	copyFile(t, rewritten, staged)
	if err := Restore(context.Background(), agreement, staged); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("restoration accepted a schema disagreement verification refuses: %v", err)
	}
}

// emptyInstallation prepares a second data directory that a restore can target: a
// restore only accepts a destination with no database and no uploads.
func emptyInstallation(t *testing.T) app.Config {
	t.Helper()
	t.Setenv("BRRP_UPLOAD_DIR", "")
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	return config
}

// TestDatabaseOnlyArchiveIsExplicitlyIncomplete is the permanent regression for the
// first case of R2-07. Creation accepted a database-only archive whose database
// referenced two uploaded files, verification rejected it as inconsistent, and a
// restore accepted it and silently removed the attachment rows.
//
// One contract replaces the three answers: the archive records exactly how many
// uploaded files it leaves out, so it is accepted as incomplete recovery material
// by every surface that reads it, and refused only when its declaration and its
// database disagree.
func TestDatabaseOnlyArchiveIsExplicitlyIncomplete(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 2)
	archive := filepath.Join(config.DataDir, "backups", "database-only.zip")
	if err := Create(context.Background(), store, config, archive, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	manifest, _, err := VerifyArchive(context.Background(), archive)
	if err != nil {
		t.Fatalf("a database-only archive failed verification: %v", err)
	}
	if manifest.IncludesUploads {
		t.Fatal("a database-only archive claims to include uploads")
	}
	if manifest.ExcludedUploads != 2 {
		t.Fatalf("the archive records %d excluded uploads, want 2", manifest.ExcludedUploads)
	}

	restored := emptyInstallation(t)
	staged := filepath.Join(restored.DataDir, "database-only.zip")
	copyFile(t, archive, staged)
	if err := Restore(context.Background(), restored, staged); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// The restored database carries no attachment rows, because the files they
	// reference are not in the archive. That is the declared incompleteness, not a
	// silent repair: the manifest said two files would be missing.
	opened, err := db.Open(restored.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	var rows int
	if err := opened.QueryRow(`SELECT count(*) FROM attachments`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("a database-only restore left %d attachment rows", rows)
	}
}

// TestCreateRefusesAnEntryTheReaderWouldRefuse covers the second case of R2-07: a
// 12,582,913-byte stored file was written into an archive at creation and then
// rejected by verification and restoration for exceeding the per-entry limit. The
// per-entry limit is one table both sides read, and creation applies it while it
// streams, so it fails instead of producing an archive it cannot read back.
func TestCreateRefusesAnEntryTheReaderWouldRefuse(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 0)
	name := db.ID()
	path := filepath.Join(config.UploadDir, name)
	if _, err := store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at)
        SELECT ?,id,?,?,?,?,? FROM users LIMIT 1`, name, "huge.bin", name, app.MaxUpload+1, "application/octet-stream", db.Now()); err != nil {
		t.Fatal(err)
	}
	// A sparse file has the declared size without writing 12 MiB of data.
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, int64(app.MaxUpload)+1); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(config.DataDir, "backups", "oversized.zip")
	if err := Create(context.Background(), store, config, archive, true); err == nil || !strings.Contains(err.Error(), "supports") {
		t.Fatalf("creation accepted an entry the reader refuses: %v", err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("a refused creation left an archive behind")
	}

	// One byte under the limit is accepted, so the boundary is the declared limit
	// rather than an approximation of it.
	if err := os.Truncate(path, int64(app.MaxUpload)); err != nil {
		t.Fatal(err)
	}
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("creation refused an entry exactly at the limit: %v", err)
	}
	if _, _, err := VerifyArchive(context.Background(), archive); err != nil {
		t.Fatalf("an archive at the per-entry limit failed verification: %v", err)
	}
}

// TestVerificationUsesBoundedScratchStorage covers the scratch half of F-11: a
// verification extracts its database into a chosen directory and leaves nothing
// behind, so an operator whose temporary filesystem is small can point it at the
// data volume instead.
func TestVerificationUsesBoundedScratchStorage(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 1)
	archive := filepath.Join(config.DataDir, "backups", "scratch.zip")
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	scratch := filepath.Join(config.DataDir, "scratch")
	if _, _, err := VerifyArchiveIn(context.Background(), archive, scratch); err != nil {
		t.Fatalf("verify with a scratch directory: %v", err)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("verification left %d files in the scratch directory", len(entries))
	}
}

// TestArchiveIsPublishedAtomically covers the completion half of F-11: the final
// name appears only when the archive is complete, so an interrupted creation is
// never advertised as a finished backup.
func TestArchiveIsPublishedAtomically(t *testing.T) {
	config, store := manyAttachmentInstallation(t, 2)
	archive := filepath.Join(config.DataDir, "backups", "atomic.zip")

	// A failure inside the write leaves neither the final name nor a stray file
	// that looks like an archive.
	if err := Create(context.Background(), store, config, filepath.Join(config.DataDir, "backups", "atomic.zip"), true); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("the archive was not published: %v", err)
	}
	// Publishing the same name twice is refused rather than silently replacing a
	// verified archive.
	if err := Create(context.Background(), store, config, archive, true); err == nil {
		t.Fatal("a second archive replaced an existing one")
	}
	// A failing publish leaves the directory exactly as it was.
	before, err := os.ReadDir(filepath.Join(config.DataDir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Publish(filepath.Join(config.DataDir, "backups"), "atomic.zip", func(*os.File) error { return os.ErrInvalid }); err == nil {
		t.Fatal("publishing over an existing name succeeded")
	}
	after, err := os.ReadDir(filepath.Join(config.DataDir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("a failed publication left %d entries, want %d", len(after), len(before))
	}
	for _, entry := range after {
		if entry.Name() != "atomic.zip" && strings.HasPrefix(entry.Name(), ".publish-") {
			t.Fatalf("a failed publication left a temporary file behind: %s", entry.Name())
		}
	}
}
