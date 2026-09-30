package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Migration is one ordered schema change. Migrations are append-only: never
// renumber, edit or delete a shipped migration, because a database that already
// recorded its version will never run it again.
type Migration struct {
	Version int
	Name    string
	SQL     string
	// RebuildsReferences requires foreign_keys=OFF before BEGIN on the exact
	// connection that runs this migration, plus integrity checks before commit.
	RebuildsReferences bool
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
	{Version: 11, Name: "required conversation scope and nullable channel message projection", SQL: migration11, RebuildsReferences: true},
	{Version: 12, Name: "direct consent, blocking, preferences and read cursors", SQL: migration12},
	{Version: 13, Name: "private message evidence and durable submission limits", SQL: migration13},
	{Version: 14, Name: "request attempt identity, request evidence and command receipts", SQL: migration14},
	{Version: 15, Name: "durable account activity and private notification preferences", SQL: migration15},
	{Version: 16, Name: "consenting groups on the shared conversation model", SQL: migration16},
	{Version: 17, Name: "group activity kind", SQL: migration17, RebuildsReferences: true},
	{Version: 18, Name: "spent command-receipt guard", SQL: migration18, RebuildsReferences: true},
	{Version: 19, Name: "named community threads", SQL: migration19, RebuildsReferences: true},
	{Version: 20, Name: "moderation, timeouts and invitations", SQL: migration20},
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
	_, err = store.applySchemaMigrations(context.Background(), version)
	return err
}

// applySchemaMigrations owns one pinned connection through the schema commit and
// restoration of its original foreign-key setting. A fixtureVersion greater
// than zero creates only that historical schema and omits the runtime upgrade
// marker. Production uses zero and retains the existing all-or-nothing batch.
func (s *Store) applySchemaMigrations(ctx context.Context, fixtureVersion int) (outstanding bool, resultErr error) {
	conn, err := s.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var exists, version int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return false, err
	}
	if exists != 0 {
		if err := conn.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version); err != nil {
			return false, err
		}
	}
	if version > SchemaVersion {
		return false, &UpgradeError{Version: version, Supported: SchemaVersion}
	}
	pending := PendingMigrations(version)
	if fixtureVersion > 0 {
		for i, migration := range pending {
			if migration.Version > fixtureVersion {
				pending = pending[:i]
				break
			}
		}
	}
	rebuild := false
	for _, migration := range pending {
		rebuild = rebuild || migration.RebuildsReferences
	}
	if rebuild {
		var original int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&original); err != nil {
			return false, err
		}
		// Install cleanup before changing the setting: even a cancelled Exec may
		// have executed its pragma. Cleanup uses an independent bounded context.
		defer func() {
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, err := conn.ExecContext(restoreCtx, fmt.Sprintf("PRAGMA foreign_keys=%d", original))
			if err == nil {
				var restored int
				err = conn.QueryRowContext(restoreCtx, `PRAGMA foreign_keys`).Scan(&restored)
				if err == nil && restored != original {
					err = errors.New("foreign-key setting was not restored")
				}
			}
			if err != nil {
				// Never return a connection with weakened enforcement to the pool.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				resultErr = errors.Join(resultErr, fmt.Errorf("restoring migration connection: %w", err))
			}
		}()
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return false, err
		}
		var enabled int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
			return false, err
		}
		if enabled != 0 {
			return false, errors.New("foreign-key enforcement could not be suspended before table rebuild")
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := s.ensureVersionTable(ctx, tx); err != nil {
		return false, err
	}
	for _, migration := range pending {
		if migration.SQL != "" {
			if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
				return false, fmt.Errorf("migration %d (%s): %w", migration.Version, migration.Name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, migration.Version); err != nil {
			return false, fmt.Errorf("migration %d (%s): recording version: %w", migration.Version, migration.Name, err)
		}
	}
	if rebuild {
		var missing int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&missing); err != nil {
			return false, fmt.Errorf("rebuild foreign-key check: %w", err)
		}
		if missing != 0 {
			return false, fmt.Errorf("table rebuild has %d broken references; migration rolled back", missing)
		}
		if err := verifyMessageScopes(ctx, tx); err != nil {
			return false, err
		}
	}
	if fixtureVersion == 0 {
		outstanding, err = upgradeOutstanding(ctx, tx)
		if err != nil {
			return false, err
		}
		if len(pending) > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, UpgradeStateKey, fmt.Sprintf(`{"from":%d,"to":%d}`, version, SchemaVersion), Now()); err != nil {
				return false, fmt.Errorf("recording the upgrade state: %w", err)
			}
			outstanding = true
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return outstanding, nil
}

func verifyMessageScopes(ctx context.Context, q Queryer) error {
	var invalid int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM messages m LEFT JOIN conversations c ON c.id=m.conversation_id WHERE COALESCE((c.kind='channel' AND c.channel_id=m.channel_id AND c.id=m.channel_id) OR (c.kind IN ('direct','group') AND c.channel_id IS NULL AND m.channel_id IS NULL),0)=0`).Scan(&invalid)
	if err != nil {
		return fmt.Errorf("message conversation scope verification: %w", err)
	}
	if invalid != 0 {
		return fmt.Errorf("database has %d invalid message conversation scopes", invalid)
	}
	return nil
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
