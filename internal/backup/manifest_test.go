package backup

import (
	"archive/zip"
	"bytes"
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

// createArchive writes one real archive with a real attachment and returns its
// path, the attachment's stored name and its bytes.
func createArchive(t *testing.T) (app.Config, string, string, []byte) {
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

	content := []byte("attachment payload that a restore must reproduce")
	userID, attachmentID, now := db.ID(), db.ID(), db.Now()
	if _, err := store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, userID, "owner", "unused-test-hash", now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.UploadDir, attachmentID), content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at) VALUES(?,?,?,?,?,?,?)`,
		attachmentID, userID, "note.txt", attachmentID, len(content), "text/plain", now); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(config.DataDir, "backups", "archive.zip")
	if err := Create(context.Background(), store, config, archive, true); err != nil {
		t.Fatalf("create: %v", err)
	}
	return config, archive, attachmentID, content
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func readManifestEntry(t *testing.T, path string) Manifest {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.Name != manifestEntry {
			continue
		}
		input, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		var manifest Manifest
		if err := json.NewDecoder(input).Decode(&manifest); err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	t.Fatal("archive has no manifest")
	return Manifest{}
}

// rewriteArchive copies an archive entry by entry, applying change to the bytes of
// one entry and rewrite to the manifest, so a test can build an archive that a
// naive reader would accept.
func rewriteArchive(t *testing.T, source, destination string, change func(name string, content []byte) []byte) {
	t.Helper()
	reader, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
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
		if change != nil {
			content = change(entry.Name, content)
		}
		target, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRecordsAndVerifiesWhatItContains(t *testing.T) {
	_, archive, attachmentID, content := createArchive(t)

	manifest := readManifestEntry(t, archive)
	if manifest.Format != currentFormat {
		t.Fatalf("format = %d, want %d", manifest.Format, currentFormat)
	}
	if manifest.AppVersion != app.Version || manifest.SchemaVersion != db.SchemaVersion {
		t.Fatalf("manifest versions = %q/%d", manifest.AppVersion, manifest.SchemaVersion)
	}
	if manifest.Database == nil || manifest.Database.Name != databaseEntry || manifest.Database.Size == 0 {
		t.Fatalf("database inventory = %+v", manifest.Database)
	}
	if len(manifest.Attachments) != 1 || manifest.Attachments[0].Name != uploadsPrefix+attachmentID {
		t.Fatalf("attachment inventory = %+v", manifest.Attachments)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if manifest.Attachments[0].SHA256 != want || manifest.Attachments[0].Size != int64(len(content)) {
		t.Fatalf("attachment inventory = %+v, want sha256 %s", manifest.Attachments[0], want)
	}

	verified, digest, err := VerifyArchive(context.Background(), archive)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Format != manifest.Format || len(verified.Attachments) != 1 {
		t.Fatalf("verified manifest = %+v", verified)
	}
	// The archive's identity must be stable, because a copy is measured against it.
	again, err := Digest(archive)
	if err != nil || again != digest {
		t.Fatalf("digest %q then %q (%v)", digest, again, err)
	}
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		t.Fatalf("digest %q is not a SHA-256 hex string", digest)
	}
}

func TestTouchedArchiveCannotSatisfyVerificationOrRestore(t *testing.T) {
	config, archive, _, _ := createArchive(t)
	tampered := filepath.Join(config.DataDir, "backups", "tampered.zip")
	rewriteArchive(t, archive, tampered, func(name string, content []byte) []byte {
		if !strings.HasPrefix(name, uploadsPrefix) {
			return content
		}
		// One byte of attachment content, with a correct ZIP checksum and an
		// unchanged manifest: only the recorded digest can notice this.
		content[len(content)-1] ^= 0x01
		return content
	})
	if _, _, err := VerifyArchive(context.Background(), tampered); err == nil {
		t.Fatal("verification accepted an archive whose content no longer matches its digest")
	} else if !strings.Contains(err.Error(), "digest") {
		t.Fatalf("verification failed for the wrong reason: %v", err)
	}

	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	destination, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Prepare(); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(destination.DataDir, "tampered.zip")
	copyFile(t, tampered, inside)
	if err := Restore(context.Background(), destination, inside); err == nil {
		t.Fatal("restore accepted an archive whose content no longer matches its digest")
	}
	if _, err := os.Stat(destination.DBFile); !os.IsNotExist(err) {
		t.Fatal("rejected restore created a database")
	}
}

func TestManifestThatDoesNotDescribeTheArchiveIsRejected(t *testing.T) {
	config, archive, attachmentID, _ := createArchive(t)
	for name, change := range map[string]func(string, []byte) []byte{
		"a missing entry": func(entry string, content []byte) []byte {
			if entry != manifestEntry {
				return content
			}
			var manifest Manifest
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Attachments = nil
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			return encoded
		},
		"an invented entry": func(entry string, content []byte) []byte {
			if entry != manifestEntry {
				return content
			}
			var manifest Manifest
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Attachments = append(manifest.Attachments, Inventory{Name: uploadsPrefix + attachmentID + "0", Size: 1, SHA256: strings.Repeat("0", 64)})
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			return encoded
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(config.DataDir, "backups", strings.ReplaceAll(name, " ", "-")+".zip")
			rewriteArchive(t, archive, path, change)
			if _, _, err := VerifyArchive(context.Background(), path); err == nil {
				t.Fatalf("verification accepted an archive with %s", name)
			}
		})
	}
}

// A format 1 archive records no inventory. It must still restore, because the
// plan requires old supported archives to stay readable.
func TestLegacyArchiveWithoutInventoryStillRestores(t *testing.T) {
	config, archive, attachmentID, content := createArchive(t)
	legacy := filepath.Join(config.DataDir, "backups", "legacy.zip")
	rewriteArchive(t, archive, legacy, func(name string, content []byte) []byte {
		if name != manifestEntry {
			return content
		}
		var manifest Manifest
		if err := json.Unmarshal(content, &manifest); err != nil {
			t.Fatal(err)
		}
		legacyManifest := Manifest{Format: legacyFormat, CreatedAt: manifest.CreatedAt, IncludesUploads: manifest.IncludesUploads}
		encoded, err := json.Marshal(legacyManifest)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	})
	if _, _, err := VerifyArchive(context.Background(), legacy); err != nil {
		t.Fatalf("verify legacy archive: %v", err)
	}

	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	destination, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Prepare(); err != nil {
		t.Fatal(err)
	}
	// Restore only accepts an archive inside the destination, as the operator
	// procedure documents.
	inside := filepath.Join(destination.DataDir, "legacy.zip")
	copyFile(t, legacy, inside)
	if err := Restore(context.Background(), destination, inside); err != nil {
		t.Fatalf("restore legacy archive: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(destination.UploadDir, attachmentID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, content) {
		t.Fatalf("restored attachment = %q", restored)
	}
}
