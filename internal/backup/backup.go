package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

// snapshotClock dates a database snapshot boundary. It is a variable so a test can
// advance the clock deliberately and observe that the declared recovery point is the
// moment the snapshot was taken rather than a later stage of the job.
var snapshotClock = func() string { return db.Now() }

// archiveBudget enforces, during creation, the same limits the reader applies:
// the attachment count, the aggregate uncompressed size and the manifest. Without
// it a backup can succeed at creation and then fail verification, which is how a
// 600-attachment archive used to be produced and refused.
//
// The manifest counts against the aggregate budget as well. It used to be sized
// separately after the entry budget had already been spent, so a data set could be
// accepted at creation and then refused by a reader that counted every entry.
type archiveBudget struct {
	attachments int
	bytes       int64
}

func (b *archiveBudget) add(size int64) error {
	b.bytes += size
	if b.bytes > maxArchiveBytes {
		return fmt.Errorf("this data set is at least %d bytes; one archive supports up to %d bytes", b.bytes, int64(maxArchiveBytes))
	}
	return nil
}

// addAttachment counts one stored file as well as its bytes.
func (b *archiveBudget) addAttachment(size int64) error {
	b.attachments++
	if b.attachments > MaxAttachments {
		return fmt.Errorf("this installation holds more than %d stored files; one archive supports %d attachments", b.attachments, MaxAttachments)
	}
	return b.add(size)
}

// snapshotSchema reports the schema recorded inside a database file, so a
// snapshot is described by its own contents rather than by the binary that copied
// it. The file is opened without applying migrations to it.
func snapshotSchema(ctx context.Context, path string) (int, error) {
	store, err := db.OpenWithoutMigrating(path)
	if err != nil {
		return 0, err
	}
	defer store.Close()
	version, err := store.SchemaVersionIn(ctx, store)
	if err != nil {
		return 0, fmt.Errorf("reading the schema of the snapshot: %w", err)
	}
	return version, nil
}

// Create writes a complete archive and publishes nothing that claims to be one
// until it is complete. The ZIP is built under a temporary name in the
// destination directory, flushed, and only then given its final name, so an
// interrupted backup cannot be mistaken for a usable one.
//
// The manifest is written last because it carries the inventory: every entry's
// uncompressed size and SHA-256 digest. Creation refuses to produce an archive
// whose manifest or totals the reader would reject.
func Create(ctx context.Context, store *db.Store, config app.Config, out string, uploads bool) (err error) {
	out, err = app.Contained(config.DataDir, out)
	if err != nil {
		return err
	}
	snapshot := filepath.Join(config.DataDir, "backups", "snapshot-"+db.ID()+".db")
	defer os.Remove(snapshot)
	// The declared recovery point is the moment the database snapshot begins, before the
	// vacuum writes it and before the database entry is compressed. Dating it at the end
	// of the job would make the data look as fresh as the copy took: a database that takes
	// an hour to snapshot and compress would claim an hour of recoverability it does not
	// have, and health ages the recovery point by exactly this value.
	snapshotAt := snapshotClock()
	if _, err := store.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return err
	}
	// The manifest describes the database the archive contains, not the binary that
	// wrote it: a pre-upgrade snapshot has to record the schema an operator's data
	// actually has, so it can be read back by a binary that still serves that schema.
	schemaVersion, err := snapshotSchema(ctx, snapshot)
	if err != nil {
		return err
	}
	budget := &archiveBudget{}
	_, err = app.Publish(filepath.Dir(out), filepath.Base(out), func(file *os.File) error {
		archive := zip.NewWriter(file)
		database, err := addFile(ctx, archive, databaseEntry, snapshot)
		if err != nil {
			return err
		}
		if err := budget.add(database.Size); err != nil {
			return err
		}
		// The database is read regardless of the mode: it is what says how many
		// uploaded files the snapshot references, so a database-only archive can
		// record exactly what it leaves out instead of being refused later for
		// being incomplete in the way it declared.
		names, err := snapshotFiles(snapshot)
		if err != nil {
			return err
		}
		manifest := Manifest{
			Format:          currentFormat,
			CreatedAt:       snapshotAt,
			IncludesUploads: uploads,
			AppVersion:      app.Version,
			SchemaVersion:   schemaVersion,
			Database:        &database,
		}
		if uploads {
			manifest.Attachments = make([]Inventory, 0, len(names))
			for _, name := range names {
				path, err := app.Contained(config.DataDir, filepath.Join(config.UploadDir, name))
				if err != nil {
					return err
				}
				attachment, err := addFile(ctx, archive, uploadsPrefix+name, path)
				if err != nil {
					return fmt.Errorf("backup attachment: %w", err)
				}
				if err := budget.addAttachment(attachment.Size); err != nil {
					return err
				}
				manifest.Attachments = append(manifest.Attachments, attachment)
			}
		} else {
			// A database-only archive is explicitly incomplete recovery material: it
			// records the number of uploaded files it omits, so creation, verification
			// and restoration all describe the same incompleteness instead of
			// disagreeing about whether the archive is valid.
			manifest.ExcludedUploads = len(names)
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		if len(encoded) > maxManifestBytes {
			return fmt.Errorf("this installation's inventory needs a %d byte manifest; one archive supports %d bytes, which is about %d attachments", len(encoded), maxManifestBytes, MaxAttachments)
		}
		if err := budget.add(int64(len(encoded))); err != nil {
			return err
		}
		record, err := archive.Create(manifestEntry)
		if err != nil {
			return err
		}
		if _, err := record.Write(encoded); err != nil {
			return err
		}
		return archive.Close()
	})
	return err
}

func snapshotFiles(path string) ([]string, error) {
	snapshot, err := sql.Open("sqlite", db.DatabaseFileURI(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer snapshot.Close()
	rows, err := snapshot.Query(`SELECT stored_name FROM attachments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if _, err := uuid.Parse(name); err != nil || len(name) != 36 {
			return nil, errors.New("invalid stored attachment name")
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// addFile appends one entry and returns its inventory record, hashing the bytes
// as they are written so recording an entry costs one pass over the file.
//
// The entry's own limit comes from entryLimit, the same table the reader applies, so
// creation cannot produce an archive that a verifier or a restore then refuses for
// a single oversized entry. It is enforced while streaming, so an oversized file
// fails the backup instead of being written in full first.
func addFile(ctx context.Context, archive *zip.Writer, name, path string) (Inventory, error) {
	if ctx.Err() != nil {
		return Inventory{}, ctx.Err()
	}
	limit, err := entryLimit(name)
	if err != nil {
		return Inventory{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Inventory{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Inventory{}, errors.New("backup requires regular files")
	}
	if info.Size() > int64(limit) {
		return Inventory{}, fmt.Errorf("stored file %q is %d bytes; one archive supports %d bytes for it", name, info.Size(), limit)
	}
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0600)
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return Inventory{}, err
	}
	hasher := sha256.New()
	copied, err := io.Copy(io.MultiWriter(writer, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: file}, int64(limit)+1))
	if copied > int64(limit) {
		return Inventory{}, fmt.Errorf("stored file %q is larger than the %d bytes one archive supports for it", name, limit)
	}
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{Name: name, Size: copied, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	return r.reader.Read(buffer)
}

// Restore only accepts an empty destination; it never overwrites an installation.
func Restore(ctx context.Context, config app.Config, source string) error {
	source, err := app.Contained(config.DataDir, source)
	if err != nil {
		return err
	}
	if _, err := os.Stat(config.DBFile); !os.IsNotExist(err) {
		return errors.New("restore requires a new data directory with no database")
	}
	entries, err := os.ReadDir(config.UploadDir)
	if err != nil || len(entries) != 0 {
		return errors.New("restore requires an empty uploads directory")
	}
	// Structure, manifest and inventory are checked before any entry is written,
	// and every entry is hashed as it lands in the staging directory, so an
	// archive that was touched cannot be installed.
	archive, manifest, inventory, err := openArchive(source)
	if err != nil {
		return err
	}
	defer archive.Close()
	stage, err := os.MkdirTemp(config.DataDir, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, entry := range archive.File {
		limit, err := entryLimit(entry.Name)
		if err != nil {
			return err
		}
		path, err := app.Contained(stage, entry.Name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		copyErr := extractEntry(ctx, entry, output, inventory, limit)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := validateDatabase(filepath.Join(stage, databaseEntry)); err != nil {
		return err
	}
	// Restoration applies the same schema agreement and attachment coverage the
	// verifier applies, on the extracted database and before anything is committed.
	// Without this a restore accepted an archive whose manifest disagreed with the
	// database it carried, which is the disagreement review R2-07 reproduced.
	if err := verifyCoverage(ctx, filepath.Join(stage, databaseEntry), manifest); err != nil {
		return err
	}
	restored, err := db.Open(filepath.Join(stage, "database.sqlite"))
	if err != nil {
		return err
	}
	var check string
	if err = restored.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		restored.Close()
		return errors.New("backup database integrity check failed")
	}
	if _, err = restored.Exec(`DELETE FROM sessions; DELETE FROM push_subscriptions; DELETE FROM settings WHERE key='backup-status'`); err != nil {
		restored.Close()
		return err
	}
	if !manifest.IncludesUploads {
		if _, err = restored.Exec(`DELETE FROM attachments`); err != nil {
			restored.Close()
			return err
		}
	}
	if _, err = restored.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		restored.Close()
		return err
	}
	if err = restored.Close(); err != nil {
		return err
	}
	if manifest.IncludesUploads {
		names, err := snapshotFiles(filepath.Join(stage, "database.sqlite"))
		if err != nil {
			return err
		}
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(stage, "uploads", name)); err != nil {
				return errors.New("backup is missing an attachment")
			}
		}
	}
	var moved []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range moved {
				_ = os.Remove(path)
			}
		}
	}()
	if _, err := os.Stat(filepath.Join(stage, "uploads")); err == nil {
		// Destination was verified empty; moving individual files works on Windows.
		files, err := os.ReadDir(filepath.Join(stage, "uploads"))
		if err != nil {
			return err
		}
		for _, file := range files {
			if !manifest.IncludesUploads {
				return errors.New("unexpected uploads in database-only backup")
			}
			if err := os.Rename(filepath.Join(stage, "uploads", file.Name()), filepath.Join(config.UploadDir, file.Name())); err != nil {
				return err
			}
			moved = append(moved, filepath.Join(config.UploadDir, file.Name()))
		}
	}
	if err := os.Rename(filepath.Join(stage, "database.sqlite"), config.DBFile); err != nil {
		return err
	}
	committed = true
	_ = os.Chtimes(config.DBFile, time.Now(), time.Now())
	return nil
}

func validateDatabase(path string) error {
	source, err := sql.Open("sqlite", db.DatabaseFileURI(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer source.Close()
	var version int
	if err := source.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version < 1 || version > db.SchemaVersion {
		return errors.New("unsupported database schema")
	}
	var check string
	if err := source.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return errors.New("backup database integrity check failed")
	}
	rows, err := source.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup contains broken references")
	}
	return rows.Err()
}
