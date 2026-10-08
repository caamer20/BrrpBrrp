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
	"strings"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
	"github.com/google/uuid"
)

const (
	// legacyFormat archives carry no inventory, so their entries can only be
	// checked for structure and the ZIP's own checksums.
	legacyFormat = 1
	// currentFormat archives carry an application/schema version and a size and
	// SHA-256 digest for every entry.
	currentFormat = 2

	manifestEntry = "manifest.json"
	databaseEntry = "database.sqlite"
	uploadsPrefix = "uploads/"
)

// CurrentFormat is the backup format this build writes. It is exported for the
// operator's own view of the server: a restore is only possible onto a build that reads
// the archive's format, so the number belongs beside the version and the schema.
func CurrentFormat() int { return currentFormat }

// The declared support profile of one archive.
//
// Creation, verification and restoration all read these, because a limit that
// only the reader applies is how an installation can produce a backup that it
// then refuses to restore. The previous manifest ceiling was 64 KiB, which about
// 500 attachments overflow: a 600-file backup was created successfully and then
// failed both verification and restoration.
const (
	// MaxAttachments is the greatest number of attachment files one archive may
	// carry. The manifest records about 110 bytes per attachment, so this stays
	// inside maxManifestBytes with room to spare.
	MaxAttachments = 60000
	// maxManifestBytes is the ceiling for the manifest itself. It is read into
	// memory in one bounded allocation, so it is small on purpose.
	maxManifestBytes = 8 << 20
	// maxDatabaseBytes bounds the database entry.
	maxDatabaseBytes = 8 << 30
	// maxArchiveBytes is the aggregate uncompressed ceiling. The reference target
	// is 20 GiB of attachments plus a database, so it is deliberately above that.
	maxArchiveBytes = 64 << 30
	// maxEntries is every entry an archive may contain: the manifest, the
	// database and the attachments.
	maxEntries = MaxAttachments + 2
)

// SupportProfile describes the declared limits in operator language, for
// documentation and diagnostics.
func SupportProfile() map[string]int64 {
	return map[string]int64{
		"max_attachments":    MaxAttachments,
		"max_manifest_bytes": maxManifestBytes,
		"max_database_bytes": maxDatabaseBytes,
		"max_archive_bytes":  maxArchiveBytes,
	}
}

// Manifest describes what an archive contains and what it should look like after
// it is read back. Format numbers are independent of the database schema and the
// application version, so an older archive keeps its meaning.
type Manifest struct {
	Format          int         `json:"format"`
	CreatedAt       string      `json:"created_at"`
	IncludesUploads bool        `json:"includes_uploads"`
	AppVersion      string      `json:"app_version,omitempty"`
	SchemaVersion   int         `json:"schema_version,omitempty"`
	Database        *Inventory  `json:"database,omitempty"`
	Attachments     []Inventory `json:"attachments,omitempty"`
	// ExcludedUploads is how many uploaded files a database-only archive leaves
	// out. It is recorded rather than inferred: the archive states its own
	// incompleteness, so creation, verification and restoration describe the same
	// material, and an operator reading the manifest knows what a recovery from it
	// cannot restore. It is zero whenever the archive carries its uploads.
	ExcludedUploads int `json:"excluded_uploads,omitempty"`
}

// databaseOnly reports whether the archive deliberately omits uploaded files, and
// how many its database references.
func (m Manifest) databaseOnly() (int, bool) {
	if m.IncludesUploads {
		return 0, false
	}
	return m.ExcludedUploads, true
}

// Inventory is one archive entry's identity: its stored name, its uncompressed
// size and the SHA-256 digest of its uncompressed bytes.
type Inventory struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (m Manifest) inventory() map[string]Inventory {
	byName := make(map[string]Inventory, len(m.Attachments)+1)
	if m.Database != nil {
		byName[m.Database.Name] = *m.Database
	}
	for _, attachment := range m.Attachments {
		byName[attachment.Name] = attachment
	}
	return byName
}

// Digest is the archive file's own identity. It is what a copied archive is
// compared against, so an operator can tell two copies apart and prove that a
// copy is the one that was verified.
func Digest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// openArchive validates an archive's structure, size budget and manifest before
// anything is read out of it, and rejects an archive whose manifest does not
// describe exactly the entries that are present. It returns the manifest and the
// inventory its entries will be checked against; a format 1 archive has an empty
// inventory because it records none.
func openArchive(path string) (*zip.ReadCloser, Manifest, map[string]Inventory, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, Manifest{}, nil, err
	}
	if len(archive.File) > maxEntries {
		archive.Close()
		return nil, Manifest{}, nil, fmt.Errorf("backup has more than %d entries", maxEntries)
	}
	seen := make(map[string]bool, len(archive.File))
	var total uint64
	for _, entry := range archive.File {
		if seen[entry.Name] || !entry.Mode().IsRegular() {
			archive.Close()
			return nil, Manifest{}, nil, errors.New("backup contains duplicate or non-file entries")
		}
		seen[entry.Name] = true
		limit, err := entryLimit(entry.Name)
		if err != nil {
			archive.Close()
			return nil, Manifest{}, nil, err
		}
		if entry.UncompressedSize64 > limit || entry.UncompressedSize64 > maxArchiveBytes-total {
			archive.Close()
			return nil, Manifest{}, nil, fmt.Errorf("backup entry %q exceeds its size limit", entry.Name)
		}
		total += entry.UncompressedSize64
	}
	if !seen[manifestEntry] || !seen[databaseEntry] {
		archive.Close()
		return nil, Manifest{}, nil, errors.New("backup is missing its manifest or database")
	}
	manifest, err := readManifest(archive, seen)
	if err != nil {
		archive.Close()
		return nil, Manifest{}, nil, err
	}
	inventory := map[string]Inventory(nil)
	if manifest.Format >= currentFormat {
		inventory = manifest.inventory()
	}
	return archive, manifest, inventory, nil
}

// entryLimit bounds one entry by its kind, and rejects unexpected paths. It uses
// the same declared limits creation enforces.
func entryLimit(name string) (uint64, error) {
	if !isLocalPath(name) {
		return 0, errors.New("backup contains an unexpected path")
	}
	switch {
	case name == manifestEntry:
		return maxManifestBytes, nil
	case name == databaseEntry:
		return maxDatabaseBytes, nil
	case strings.HasPrefix(name, uploadsPrefix) && strings.Count(name, "/") == 1 && len(strings.TrimPrefix(name, uploadsPrefix)) == 36:
		if _, err := uuid.Parse(strings.TrimPrefix(name, uploadsPrefix)); err != nil {
			return 0, errors.New("invalid attachment filename")
		}
		return app.MaxUpload, nil
	}
	return 0, errors.New("backup contains an unexpected path")
}

func isLocalPath(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func readManifest(archive *zip.ReadCloser, entries map[string]bool) (Manifest, error) {
	var manifest Manifest
	var raw []byte
	for _, entry := range archive.File {
		if entry.Name != manifestEntry {
			continue
		}
		input, err := entry.Open()
		if err != nil {
			return Manifest{}, err
		}
		raw, err = io.ReadAll(io.LimitReader(input, maxManifestBytes+1))
		input.Close()
		if err != nil {
			return Manifest{}, err
		}
		break
	}
	if len(raw) == 0 {
		return Manifest{}, errors.New("backup is missing its manifest")
	}
	if len(raw) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("backup manifest is larger than %d bytes", maxManifestBytes)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, errors.New("backup manifest is not readable")
	}
	switch manifest.Format {
	case legacyFormat:
		// No inventory to check: the database reference check downstream still
		// establishes what the archive contains.
	case currentFormat:
		if manifest.Database == nil || manifest.Database.Name != databaseEntry {
			return Manifest{}, errors.New("backup manifest does not describe its database")
		}
		if manifest.SchemaVersion < 1 || manifest.SchemaVersion > 1<<16 {
			return Manifest{}, errors.New("backup manifest has an implausible schema version")
		}
		if len(manifest.Attachments) > MaxAttachments {
			return Manifest{}, fmt.Errorf("backup manifest lists more than %d attachments", MaxAttachments)
		}
		// The two statements about upload coverage must agree: an archive either
		// carries its files or records how many it omits, never both and never
		// neither.
		if manifest.IncludesUploads && manifest.ExcludedUploads != 0 {
			return Manifest{}, errors.New("backup manifest excludes uploads while claiming to include them")
		}
		if !manifest.IncludesUploads && manifest.ExcludedUploads > MaxAttachments {
			return Manifest{}, fmt.Errorf("backup manifest exceeds the %d uploads one archive supports", MaxAttachments)
		}
		// An archive that carries no uploads cannot list any. The converse is not a
		// defect: an installation with no stored files produces a complete archive with
		// upload coverage enabled and an empty attachment list.
		if !manifest.IncludesUploads && len(manifest.Attachments) > 0 {
			return Manifest{}, errors.New("backup manifest lists uploaded files while recording that it excludes them")
		}
		for _, name := range manifest.inventoryList() {
			if !entries[name] {
				return Manifest{}, fmt.Errorf("backup manifest lists a missing entry %q", name)
			}
		}
		// The manifest describes every entry except itself.
		if len(manifest.inventoryList()) != len(entries)-1 {
			return Manifest{}, errors.New("backup manifest does not account for every entry")
		}
	default:
		return Manifest{}, errors.New("unsupported backup format")
	}
	return manifest, nil
}

func (m Manifest) inventoryList() []string {
	names := make([]string, 0, len(m.Attachments)+1)
	if m.Database != nil {
		names = append(names, m.Database.Name)
	}
	for _, attachment := range m.Attachments {
		names = append(names, attachment.Name)
	}
	return names
}

// checkInventory compares one entry's digest and size with the manifest's record
// of it, which is what makes a touched or truncated archive unusable.
func checkInventory(name string, size int64, summed []byte, inventory map[string]Inventory) error {
	// The manifest cannot carry its own digest, and every entry it does describe is
	// checked against it.
	if name == manifestEntry {
		return nil
	}
	recorded, ok := inventory[name]
	if !ok {
		return fmt.Errorf("backup manifest does not list %q", name)
	}
	if recorded.Size != size {
		return fmt.Errorf("backup entry %q is %d bytes, expected %d", name, size, recorded.Size)
	}
	if got := hex.EncodeToString(summed); got != recorded.SHA256 {
		return fmt.Errorf("backup entry %q does not match its recorded digest", name)
	}
	return nil
}

// VerifyArchive checks an archive without restoring it: structure, size budget,
// manifest agreement, the digest and size of every entry, the database's own
// integrity and references, and the same database-to-attachment coverage a
// restore applies. It is the check an operator runs against a copy that has been
// sitting on separate storage, and it needs only read access.
//
// Verification extracts the database to scratch storage, which defaults to the
// system temporary directory. A container with a small /tmp should set the scratch
// directory with VerifyArchiveIn, because a realistic database does not fit in a
// few megabytes of scratch.
func VerifyArchive(ctx context.Context, path string) (Manifest, string, error) {
	return VerifyArchiveIn(ctx, path, "")
}

// VerifyArchiveIn is VerifyArchive with an explicit scratch directory for the
// extracted database. The directory is created when missing, and the extracted
// file is bounded by the declared database limit.
func VerifyArchiveIn(ctx context.Context, path, scratchDir string) (Manifest, string, error) {
	archive, manifest, inventory, err := openArchive(path)
	if err != nil {
		return Manifest{}, "", err
	}
	defer archive.Close()
	var databasePath string
	for _, entry := range archive.File {
		limit, err := entryLimit(entry.Name)
		if err != nil {
			return Manifest{}, "", err
		}
		if entry.Name != databaseEntry {
			if err := extractEntry(ctx, entry, io.Discard, inventory, limit); err != nil {
				return Manifest{}, "", err
			}
			continue
		}
		temp, err := createScratch(scratchDir, entry.UncompressedSize64)
		if err != nil {
			return Manifest{}, "", err
		}
		databasePath = temp.Name()
		if err := extractEntry(ctx, entry, temp, inventory, limit); err != nil {
			temp.Close()
			os.Remove(databasePath)
			return Manifest{}, "", err
		}
		if err := temp.Close(); err != nil {
			os.Remove(databasePath)
			return Manifest{}, "", err
		}
		defer os.Remove(databasePath)
	}
	if databasePath == "" {
		return Manifest{}, "", errors.New("backup is missing its database")
	}
	if err := validateDatabase(databasePath); err != nil {
		return Manifest{}, "", err
	}
	// Verification now applies the coverage checks restoration applies, so an
	// archive cannot pass verification and then be refused by a restore.
	if err := verifyCoverage(ctx, databasePath, manifest); err != nil {
		return Manifest{}, "", err
	}
	digest, err := Digest(path)
	if err != nil {
		return Manifest{}, "", err
	}
	return manifest, digest, nil
}

// createScratch opens the temporary file a verification extracts its database
// into. The failure is reported with the size the archive claims, because the most
// likely cause is a scratch filesystem the database does not fit in.
func createScratch(dir string, size uint64) (*os.File, error) {
	if dir == "" {
		return os.CreateTemp("", "brrp-verify-*.db")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("verification scratch directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "brrp-verify-*.db")
	if err != nil {
		return nil, fmt.Errorf("verification needs %d bytes of scratch space in %s: %w", size, dir, err)
	}
	return temp, nil
}

// verifyCoverage applies the checks that make an archive usable: the schema its
// manifest records is the schema inside the database, and the archive carries
// exactly the attachment files that database references. A format 1 archive
// records no inventory, so it carries only the database checks (ADR 0008).
func verifyCoverage(ctx context.Context, databasePath string, manifest Manifest) error {
	if manifest.Format < currentFormat {
		return nil
	}
	version, referenced, err := readDatabaseInventory(ctx, databasePath)
	if err != nil {
		return err
	}
	if version != manifest.SchemaVersion {
		return fmt.Errorf("the manifest records schema %d but its database is schema %d", manifest.SchemaVersion, version)
	}
	if excluded, databaseOnly := manifest.databaseOnly(); databaseOnly {
		// A database-only archive is supported, and it is explicitly incomplete
		// recovery material for uploaded files. Its database is restored without the
		// files it references, so the manifest must record exactly how many that is:
		// the archive is accepted as what it declares itself to be, and refused when
		// its declaration and its database disagree.
		if len(referenced) != excluded {
			return fmt.Errorf("the archive records %d uploaded files as excluded but its database references %d", excluded, len(referenced))
		}
		return nil
	}
	carried := make(map[string]bool, len(manifest.Attachments))
	for _, attachment := range manifest.Attachments {
		name := strings.TrimPrefix(attachment.Name, uploadsPrefix)
		if name == attachment.Name || name == "" {
			return fmt.Errorf("the manifest lists an unexpected attachment name %q", attachment.Name)
		}
		carried[name] = true
	}
	for name := range referenced {
		if !carried[name] {
			return fmt.Errorf("the archive is missing an attachment the database references (%s)", name)
		}
	}
	for name := range carried {
		if !referenced[name] {
			return fmt.Errorf("the archive carries an attachment the database does not reference (%s)", name)
		}
	}
	return nil
}

// readDatabaseInventory reads the schema version and the attachment files a
// backup database references. It opens the file read-only, so it can run against
// an archive that was never restored.
func readDatabaseInventory(ctx context.Context, path string) (int, map[string]bool, error) {
	source, err := sql.Open("sqlite", db.DatabaseFileURI(path)+"?mode=ro")
	if err != nil {
		return 0, nil, err
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		return 0, nil, err
	}
	var version int
	if err := source.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, nil, err
	}
	rows, err := source.QueryContext(ctx, `SELECT stored_name FROM attachments WHERE stored_name <> ''`)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	referenced := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, nil, err
		}
		referenced[name] = true
	}
	return version, referenced, rows.Err()
}

// extractEntry streams one entry to writer, hashing as it goes, and refuses an
// entry that is larger than its kind allows or that disagrees with the inventory.
// The inventory is nil when the archive is too old to carry one.
func extractEntry(ctx context.Context, entry *zip.File, writer io.Writer, inventory map[string]Inventory, limit uint64) error {
	input, err := entry.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	hasher := sha256.New()
	copied, copyErr := io.Copy(io.MultiWriter(writer, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: input}, int64(limit)+1))
	if copied > int64(limit) {
		copyErr = errors.New("backup entry exceeds size limit")
	}
	if copyErr != nil {
		return copyErr
	}
	if inventory == nil {
		return nil
	}
	return checkInventory(entry.Name, copied, hasher.Sum(nil), inventory)
}
