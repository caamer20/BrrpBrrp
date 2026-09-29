package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed migration2.sql
var migration2 string

//go:embed migration3.sql
var migration3 string

//go:embed migration4.sql
var migration4 string

//go:embed migration5.sql
var migration5 string

//go:embed migration6.sql
var migration6 string

//go:embed migration7.sql
var migration7 string

//go:embed migration8.sql
var migration8 string

//go:embed migration9.sql
var migration9 string

//go:embed migration10.sql
var migration10 string

//go:embed migration11.sql
var migration11 string

//go:embed migration12.sql
var migration12 string

//go:embed migration13.sql
var migration13 string

//go:embed migration14.sql
var migration14 string

//go:embed migration15.sql
var migration15 string

// Durability levels accepted for the SQLite synchronous pragma. The production
// baseline is FULL so that a committed transaction survives an operating-system
// crash or power loss on honest storage; NORMAL is available for disposable or
// development installations and is documented as losing recent commits in that
// scenario.
const (
	DurabilityFull   = "FULL"
	DurabilityNormal = "NORMAL"
)

type Store struct{ *sql.DB }

// Options controls how a database file is opened. The zero value is the
// production configuration: newest schema, cheapest safe durability.
type Options struct {
	// Synchronous selects the SQLite synchronous pragma. Empty means FULL.
	Synchronous string
}

func (o Options) synchronous() (string, error) {
	switch strings.ToUpper(strings.TrimSpace(o.Synchronous)) {
	case "", DurabilityFull:
		return DurabilityFull, nil
	case DurabilityNormal:
		return DurabilityNormal, nil
	default:
		return "", fmt.Errorf("unsupported synchronous mode %q; use %s or %s", o.Synchronous, DurabilityFull, DurabilityNormal)
	}
}

// Open creates a missing database and migrates an existing one to SchemaVersion.
// Use Inspect for a preflight that never migrates anything.
func Open(path string) (*Store, error) {
	return OpenWithOptions(path, Options{})
}

func OpenWithOptions(path string, options Options) (*Store, error) {
	synchronous, err := options.synchronous()
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", databaseURIWithDurability(path, synchronous))
	if err != nil {
		return nil, err
	}
	// At this scale one connection serializes writes without SQLite lock churn.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := &Store{database}
	if err := store.Migrate(context.Background()); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func databaseURI(path string) string {
	synchronous, _ := Options{}.synchronous()
	return databaseURIWithDurability(path, synchronous)
}

func databaseURIWithDurability(path, synchronous string) string {
	return databaseFileURI(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(" + synchronous + ")"
}

func databaseFileURI(path string) string {
	// SQLite expects /C:/... for an absolute Windows drive path. Without the
	// leading slash, net/url treats the drive letter as a URI authority.
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path}
	return uri.String()
}

// OpenWithoutMigrating opens an existing database that must not have its schema
// applied, but that SQLite itself still needs to write through: the snapshot copy
// of a database is taken with VACUUM INTO, and the search-index consistency check
// is issued as a statement SQLite classifies as a write. Neither changes the
// schema or the data. Callers hold the data-folder lock, so nothing else is
// writing, and a command that opens this way applies no migrations and repairs
// nothing: a pre-upgrade snapshot or check has to describe the installation as it
// is, and only Open and OpenWithOptions are allowed to apply schema changes.
func OpenWithoutMigrating(path string) (*Store, error) {
	return openExisting(path, "?_pragma=busy_timeout(5000)")
}

func openExisting(path, options string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", databaseFileURI(path)+options)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	return &Store{database}, nil
}

// SchemaVersionIn reports the schema an open database actually records, which is
// not necessarily the schema this binary serves: a snapshot taken before an
// upgrade describes the database it copied, not the tool that copied it.
func (s *Store) SchemaVersionIn(ctx context.Context, q Queryer) (int, error) {
	var version int
	err := q.QueryRowContext(ctx, `SELECT COALESCE(max(version),0) FROM schema_migrations`).Scan(&version)
	return version, err
}

// ReadSetting reads live server state without taking the exclusive data-folder
// lock or running migrations. SQLite WAL permits this concurrent read.
func ReadSetting(path, key string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	database, err := sql.Open("sqlite", databaseFileURI(path)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer database.Close()
	var value string
	err = database.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	return value, err
}

// UpgradeStateKey is the durable record that schema changes were applied whose
// postconditions — the search index rebuild and invariant verification — have
// not completed yet. It is written inside the migrating transaction and cleared
// only after the postconditions pass, so an interrupted upgrade cannot be skipped
// by a later start that finds no pending migrations.
const UpgradeStateKey = "upgrade-state"

// Migrate applies every pending migration in one transaction and records each
// version. A failed or interrupted migration rolls back completely, so an
// operator never has to reason about a half-applied schema.
//
// Migrations and their postconditions are two steps on purpose: rebuilding the
// search index and verifying referential integrity are reads and repairs of data,
// not schema changes, and SQLite cannot hold them inside the schema transaction
// without making every migration far more expensive. The durable upgrade record is
// what keeps the pair honest: while it exists, every open re-runs the
// postconditions and refuses to serve until they succeed.
func (s *Store) Migrate(ctx context.Context) error {
	outstanding, err := s.applySchemaMigrations(ctx, 0)
	if err != nil {
		return err
	}
	if !outstanding {
		return nil
	}
	// A migration can rewrite or recreate the message rows an external-content
	// search index points at. The index is derived data, and a missing entry in
	// it is undetectable (FTS5 stores no copy of the text), so it is rebuilt from
	// the messages that survived instead of being trusted. Reference integrity
	// and the recorded history are then verified rather than assumed.
	if err := s.RebuildSearchIndex(ctx); err != nil {
		return fmt.Errorf("search index rebuild after migration: %w", err)
	}
	if err := s.Verify(ctx); err != nil {
		return fmt.Errorf("verification after migration: %w", err)
	}
	if _, err := s.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, UpgradeStateKey); err != nil {
		return fmt.Errorf("clearing the upgrade state: %w", err)
	}
	return nil
}

// upgradeOutstanding reports whether a previous upgrade still owes its
// postconditions.
func upgradeOutstanding(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM settings WHERE key=?)`, UpgradeStateKey).Scan(&exists)
	return exists == 1, err
}

// UpgradePending reports whether this database owes upgrade postconditions, so an
// operator can see why a server refuses to start.
func (s *Store) UpgradePending(ctx context.Context) (bool, error) {
	return upgradeOutstanding(ctx, s)
}

// Verify checks the invariants that migrations and restores must preserve: a
// complete recorded history, no broken references and a search index that still
// matches the message table.
func (s *Store) Verify(ctx context.Context) error {
	versions, err := s.recordedVersions(ctx)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return errors.New("database records no schema version")
	}
	if newest := versions[len(versions)-1]; newest > SchemaVersion {
		return &UpgradeError{Version: newest, Supported: SchemaVersion}
	}
	for i, recorded := range versions {
		if recorded != i+1 {
			return fmt.Errorf("schema history is incomplete: expected version %d, found %d. Restore a verified backup instead of continuing", i+1, recorded)
		}
	}
	if versions[len(versions)-1] != SchemaVersion {
		return fmt.Errorf("schema history stops at version %d but this binary requires %d", versions[len(versions)-1], SchemaVersion)
	}
	var missing int
	if err := s.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&missing); err != nil {
		return fmt.Errorf("foreign key verification failed: %w", err)
	}
	if missing != 0 {
		return fmt.Errorf("database has %d broken references", missing)
	}
	if err := verifyMessageScopes(ctx, s); err != nil {
		return err
	}
	return s.VerifySearchIndex(ctx)
}

// VerifySearchIndex runs FTS5's structural consistency check. It cannot prove
// that every message is indexed: an external-content table stores no copy of the
// text, so a missing entry is indistinguishable from an absent message.
// RebuildSearchIndex is the supported way to make the index trustworthy, and
// migrations always call it.
func (s *Store) VerifySearchIndex(ctx context.Context) error {
	if _, err := s.ExecContext(ctx, `INSERT INTO message_fts(message_fts) VALUES('integrity-check')`); err != nil {
		return fmt.Errorf("search index verification failed: %w", err)
	}
	return nil
}

// RebuildSearchIndex regenerates the derived search index from the message
// table. It never changes message rows, IDs or ordering, so it is the safe
// repair for an index left behind by an interrupted write or an older release.
func (s *Store) RebuildSearchIndex(ctx context.Context) error {
	_, err := s.ExecContext(ctx, `INSERT INTO message_fts(message_fts) VALUES('rebuild')`)
	return err
}

func (s *Store) recordedVersions(ctx context.Context) ([]int, error) {
	rows, err := s.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
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

// Compact folds the write-ahead log back into the database file. Migration and
// restore paths use it so a snapshot taken afterwards needs no side files.
func (s *Store) Compact(ctx context.Context) error {
	_, err := s.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func ID() string                   { return uuid.Must(uuid.NewV7()).String() }
func Now() string                  { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }
func Timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
