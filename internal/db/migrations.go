package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Migration is one ordered schema change. Migrations are append-only: never
// renumber, edit or delete a shipped migration, because a database that already
// recorded its version will never run it again.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations is the complete ordered list of schema changes this binary can
// apply. SchemaVersion is derived from the newest entry.
var Migrations = []Migration{
	{Version: 1, Name: "baseline workspace, chat, files, moderation", SQL: schema},
	{Version: 2, Name: "replies, pins, invitations, account mfa", SQL: migration2},
	{Version: 3, Name: "ownership, roles, categories and restricted channels", SQL: migration3},
	{Version: 4, Name: "session identity and recent authentication", SQL: migration4},
	{Version: 5, Name: "invite-time role selection", SQL: migration5},
	{Version: 6, Name: "shared conversation mapping and send receipts", SQL: migration6},
	{Version: 7, Name: "removed account identities preserve conversation history", SQL: migration7},
	{Version: 8, Name: "channel slow mode and durable send clocks", SQL: migration8},
	{Version: 9, Name: "community message reports and review evidence", SQL: migration9},
	{Version: 10, Name: "account-scoped channel notification preferences", SQL: migration10},
}

// SchemaVersion is the newest schema this binary writes and understands.
var SchemaVersion = NewestVersion()

// NewestVersion reports the highest migration version in the registry.
func NewestVersion() int {
	newest := 0
	for _, migration := range Migrations {
		if migration.Version > newest {
			newest = migration.Version
		}
	}
	return newest
}

// PendingMigrations lists, in order, the migrations a database at the given
// version still needs. It is used for reporting and for the explicit upgrade
// path; it never inspects or changes a database itself.
func PendingMigrations(version int) []Migration {
	pending := make([]Migration, 0, len(Migrations))
	for _, migration := range Migrations {
		if migration.Version > version {
			pending = append(pending, migration)
		}
	}
	return pending
}

// CreateSchemaFixture writes a database that records exactly the requested schema
// version and nothing later, the way an installation that stopped upgrading at
// that version records itself. It exists because an upgrade or rollback rehearsal
// needs an older data folder even when the older executable is not at hand; the
// server never calls it, and it refuses to overwrite an existing file.
func CreateSchemaFixture(path string, version int) error {
	if version < 1 || version > SchemaVersion {
		return fmt.Errorf("no schema %d fixture: this build carries schemas 1 to %d", version, SchemaVersion)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite the existing %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	database, err := sql.Open("sqlite", databaseURI(path))
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	store := &Store{database}
	ctx := context.Background()
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := store.ensureVersionTable(ctx, tx); err != nil {
		return err
	}
	for _, migration := range PendingMigrations(0) {
		if migration.Version > version {
			break
		}
		if migration.SQL != "" {
			if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
				return fmt.Errorf("migration %d (%s): %w", migration.Version, migration.Name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, migration.Version); err != nil {
			return fmt.Errorf("migration %d (%s): recording version: %w", migration.Version, migration.Name, err)
		}
	}
	return tx.Commit()
}

// Inspection is a read-only description of a database file. It answers the
// pre-upgrade questions an operator needs before authorizing a migration:
// which schema is on disk, whether this binary would rewrite it, and whether
// the workspace still holds the expected data.
type Inspection struct {
	Path              string           `json:"path"`
	Exists            bool             `json:"exists"`
	SchemaVersion     int              `json:"schema_version"`
	SupportedVersion  int              `json:"supported_schema_version"`
	UpgradeRequired   bool             `json:"upgrade_required"`
	FutureSchema      bool             `json:"future_schema"`
	RecordedVersions  []int            `json:"recorded_versions"`
	PendingMigrations []string         `json:"pending_migrations"`
	JournalMode       string           `json:"journal_mode"`
	Synchronous       string           `json:"synchronous"`
	Integrity         string           `json:"integrity"`
	Bytes             int64            `json:"database_bytes"`
	Counts            map[string]int64 `json:"counts"`
}

// Inspect reads schema state without creating, migrating or modifying a
// database. It opens an ordinary read connection that still performs no writes:
// WAL bookkeeping files may be created by SQLite, but no schema, setting or
// row changes. Callers add host facts such as free disk space.
func Inspect(path string) (Inspection, error) {
	report := Inspection{Path: path, SupportedVersion: SchemaVersion, Counts: map[string]int64{}}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return report, err
	}
	report.Path = absolute
	info, err := os.Stat(absolute)
	if os.IsNotExist(err) {
		report.Exists = false
		report.PendingMigrations = migrationNames(PendingMigrations(0))
		report.UpgradeRequired = len(report.PendingMigrations) > 0
		return report, nil
	}
	if err != nil {
		return report, err
	}
	if !info.Mode().IsRegular() {
		return report, fmt.Errorf("%s is not a regular file", absolute)
	}
	report.Exists = true
	report.Bytes = info.Size()
	database, err := sql.Open("sqlite", inspectionURI(absolute))
	if err != nil {
		return report, err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	var integrity string
	if err := database.QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil {
		return report, fmt.Errorf("database integrity check failed: %w", err)
	}
	report.Integrity = integrity
	if integrity != "ok" {
		return report, fmt.Errorf("database integrity check reported %q", integrity)
	}
	if err := database.QueryRow(`PRAGMA journal_mode`).Scan(&report.JournalMode); err != nil {
		return report, err
	}
	var synchronous int
	if err := database.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		return report, err
	}
	report.Synchronous = synchronousName(synchronous)
	var migrationsTable int
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&migrationsTable); err != nil {
		return report, err
	}
	if migrationsTable != 0 {
		versions, err := recordedVersions(database)
		if err != nil {
			return report, err
		}
		report.RecordedVersions = versions
		for _, version := range versions {
			if version > report.SchemaVersion {
				report.SchemaVersion = version
			}
		}
	}
	report.UpgradeRequired = report.SchemaVersion < SchemaVersion
	report.FutureSchema = report.SchemaVersion > SchemaVersion
	report.PendingMigrations = migrationNames(PendingMigrations(report.SchemaVersion))
	for _, table := range []string{"users", "channels", "categories", "roles", "messages", "attachments", "invitations", "audit_logs"} {
		count, err := countTable(database, table)
		if err != nil {
			return report, err
		}
		report.Counts[table] = count
	}
	return report, nil
}

// synchronousName turns the pragma's integer into the name an operator reads in a
// report, because "2" is not a durability statement.
func synchronousName(level int) string {
	switch level {
	case 0:
		return "OFF"
	case 1:
		return DurabilityNormal
	case 2:
		return DurabilityFull
	case 3:
		return "EXTRA"
	default:
		return fmt.Sprintf("unknown(%d)", level)
	}
}

func inspectionURI(path string) string {
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	// Deliberately no journal_mode or synchronous pragma: preflight must not
	// change how an existing database persists.
	return uri.String() + "?mode=rw&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

func recordedVersions(database *sql.DB) ([]int, error) {
	rows, err := database.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := make([]int, 0)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func countTable(database *sql.DB, table string) (int64, error) {
	var exists int
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var count int64
	if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func migrationNames(pending []Migration) []string {
	if len(pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(pending))
	for _, migration := range pending {
		names = append(names, fmt.Sprintf("%d %s", migration.Version, migration.Name))
	}
	return names
}

// UpgradeError explains a refused automatic migration. It is deliberately
// actionable: an operator must be able to tell a downgrade apart from a
// damaged database.
type UpgradeError struct {
	Version   int
	Supported int
}

func (e *UpgradeError) Error() string {
	return fmt.Sprintf("database schema %d is newer than this binary supports (schema %d); "+
		"this binary refuses to downgrade or rewrite it. Stop the server, install the newer version again, "+
		"or restore a backup taken before that upgrade.", e.Version, e.Supported)
}

// EnsureVersionTable creates the migration bookkeeping table when it is absent.
// It is the only schema change the preflight path may perform implicitly, and it
// happens inside the migration transaction, never during inspection.
func (s *Store) ensureVersionTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`)
	return err
}

// schemaVersion reports the highest recorded migration, or 0 for a database
// that has never been migrated.
func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version)
	return version, err
}
