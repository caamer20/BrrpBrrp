package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

// TestAPreUpgradeSnapshotRecordsItsOwnSchema is the F-12 case for the snapshot an
// operator takes before changing anything. The snapshot is created the same way
// every other backup is, it describes the data folder as it is, and it has to be
// usable by the release that owns that schema.
//
// The fixture records an older schema the way a real installation does, by
// applying only the migrations it has applied, because the historical alpha.3
// executable and its schema-2 data folder are not available on this workstation.
// Rehearsing a real old executable against a real old data folder is operator work
// on a host that has both (N-06); this test covers the snapshot and restore side.
func TestAPreUpgradeSnapshotRecordsItsOwnSchema(t *testing.T) {
	ctx := context.Background()
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
	// The pre-upgrade installation has applied only the first migration, which is
	// what makes its snapshot worth taking.
	if err := db.CreateSchemaFixture(config.DBFile, 1); err != nil {
		t.Fatal(err)
	}
	store, err := db.OpenWithoutMigrating(config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	now, userID := db.Now(), db.ID()
	if _, err := store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, userID, "owner", "unused-test-hash", now, now); err != nil {
		t.Fatal(err)
	}
	attachmentID := db.ID()
	if err := os.WriteFile(filepath.Join(config.UploadDir, attachmentID), []byte("pre-upgrade payload"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES(?,?,?,?,?,?,?)`,
		attachmentID, userID, "before.txt", attachmentID, 19, "text/plain", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// The backup command opens the data folder without migrating it, so the archive
	// carries schema 1 rather than the 5 this binary serves.
	snapshot, err := db.OpenWithoutMigrating(config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(config.DataDir, "backups", "pre-upgrade.zip")
	if err := Create(ctx, snapshot, config, archive, true); err != nil {
		snapshot.Close()
		t.Fatalf("snapshotting an older data folder: %v", err)
	}
	snapshot.Close()
	manifest, _, err := VerifyArchiveIn(ctx, archive, filepath.Join(config.DataDir, "scratch"))
	if err != nil {
		t.Fatalf("the snapshot the installation just wrote does not verify: %v", err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("manifest records schema %d, want the 1 the data folder has, not the %d this binary serves", manifest.SchemaVersion, db.SchemaVersion)
	}
	if len(manifest.Attachments) != 1 {
		t.Fatalf("manifest lists %d attachments, want 1", len(manifest.Attachments))
	}
	if recorded, err := snapshotSchema(ctx, config.DBFile); err != nil || recorded != 1 {
		t.Fatalf("taking the snapshot left the data folder at schema %d, error = %v, want it untouched at 1", recorded, err)
	}

	// The same archive restores into a clean folder, and the release that owns it can
	// still read the data it holds.
	t.Setenv("BRRP_UPLOAD_DIR", "")
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	restored, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Prepare(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(restored.DataDir, "pre-upgrade.zip")
	if err := os.WriteFile(original, contents, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, restored, original); err != nil {
		t.Fatalf("restoring a pre-upgrade snapshot into this binary: %v", err)
	}
	target, err := db.Open(restored.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	var users, attachments int
	if err := target.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=?`, userID).Scan(&users); err != nil || users != 1 {
		t.Fatalf("restored users = %d, error = %v, want the snapshot's account", users, err)
	}
	if err := target.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE id=?`, attachmentID).Scan(&attachments); err != nil || attachments != 1 {
		t.Fatalf("restored attachments = %d, error = %v, want the snapshot's attachment", attachments, err)
	}
	if payload, err := os.ReadFile(filepath.Join(restored.UploadDir, attachmentID)); err != nil || string(payload) != "pre-upgrade payload" {
		t.Fatalf("restored attachment bytes = %q, error = %v", payload, err)
	}
	// The restored copy is migrated forward by the binary that reads it, which is the
	// documented direction; a downgrade is refused instead of attempted.
	var version int
	if err := target.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != db.SchemaVersion {
		t.Fatalf("restored schema = %d, error = %v, want %d", version, err, db.SchemaVersion)
	}
}
