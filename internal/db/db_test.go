package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDatabaseURI(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
	}{
		{"windows drive", "C:/ProgramData/BrrpBrrp/brrp.db", "file:///C:/ProgramData/BrrpBrrp/brrp.db"},
		{"windows special characters", "D:/Chat room/100% # café.db", "file:///D:/Chat%20room/100%25%20%23%20caf%C3%A9.db"},
		{"unix", "/var/lib/brrpbrrp/brrp.db", "file:///var/lib/brrpbrrp/brrp.db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// FromSlash supplies native backslashes when this test runs on Windows.
			uri, err := url.Parse(databaseURI(filepath.FromSlash(tc.path)))
			if err != nil {
				t.Fatal(err)
			}
			if uri.Host != "" || uri.User != nil || uri.Fragment != "" {
				t.Fatalf("filename was interpreted as URI metadata: %s", uri)
			}
			uri.RawQuery = ""
			if got := uri.String(); got != tc.want {
				t.Fatalf("URI = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenFilePaths(t *testing.T) {
	names := []string{"brrp.db", "chat café #100% & _pragma=foreign_keys(0).db"}
	if runtime.GOOS != "windows" {
		names = append(names, `literal\backslash.db`, "question?.db")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var foreignKeys int
			if err := store.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
				t.Fatalf("foreign keys = %d, error = %v", foreignKeys, err)
			}
			if _, err := store.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(path)
			if err != nil || len(contents) < 16 || string(contents[:16]) != "SQLite format 3\x00" {
				t.Fatalf("database was not written to the requested file %q: %v", path, err)
			}
		})
	}
	t.Run("relative", func(t *testing.T) {
		t.Chdir(t.TempDir())
		store, err := Open("relative.db")
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
	})
}

func TestReadSettingWhileServerDatabaseIsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat café #100%.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('backup-status','{"error":""}',?)`, Now()); err != nil {
		t.Fatal(err)
	}
	if value, err := ReadSetting(path, "backup-status"); err != nil || value != `{"error":""}` {
		t.Fatalf("read-only setting = %q, error = %v", value, err)
	}
	missing := filepath.Join(filepath.Dir(path), "missing.db")
	if _, err := ReadSetting(missing, "backup-status"); !os.IsNotExist(err) {
		t.Fatalf("missing database unexpectedly opened: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read-only check created a missing database")
	}
}

func TestUpgradeVersionOneAndRejectFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('kept','kept','hash','now','now')`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version, count int
	store.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version)
	store.QueryRow(`SELECT count(*) FROM users WHERE id='kept' AND is_deactivated=0`).Scan(&count)
	if version != SchemaVersion || count != 1 {
		t.Fatal("upgrade lost data or skipped migration")
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal("migration is not idempotent:", err)
	}
	if _, err := store.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, SchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err == nil {
		t.Fatal("future schema accepted")
	}
}

func TestMigrationsAndSearchTriggers(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('u','alex','hash','now','now')`,
		`INSERT INTO channels(id,name,type,created_at,updated_at) VALUES('c','general','text','now','now')`,
		`INSERT INTO messages(id,channel_id,user_id,content,created_at,idempotency_key) VALUES('m','c','u','hello world','now','key')`,
	} {
		if _, err := store.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	assertMatches := func(term string, want int) {
		t.Helper()
		var count int
		if err := store.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH ?`, term).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("search %q = %d, want %d", term, count, want)
		}
	}
	assertMatches("hello", 1)
	if _, err := store.Exec(`UPDATE messages SET content='goodbye moon' WHERE id='m'`); err != nil {
		t.Fatal(err)
	}
	assertMatches("hello", 0)
	assertMatches("moon", 1)
	if _, err := store.Exec(`DELETE FROM channels WHERE id='c'`); err != nil {
		t.Fatal(err)
	}
	assertMatches("moon", 0)
	var mode string
	if err := store.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("WAL not enabled: %s %v", mode, err)
	}
	if _, err := store.Exec(`INSERT INTO sessions VALUES('s','missing','hash','now','later')`); err == nil {
		t.Fatal("foreign keys not enforced")
	}
}

// TestInspectionAndSnapshotNeverMigrate covers the pre-upgrade path: a snapshot or
// inspection command must see the schema an installation actually records, and
// copy it, without the executable applying migrations to it.
func TestInspectionAndSnapshotNeverMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "older.db")
	// A real installation that predates the newest migrations records the schema it
	// actually has, which is what an inspection has to report and a snapshot has to
	// preserve.
	if err := CreateSchemaFixture(path, 1); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('kept','kept','hash','now','now')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	store, err := OpenWithoutMigrating(path)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.SchemaVersionIn(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("recorded schema = %d, want 1: opening for inspection migrated the database", version)
	}
	// The checks verification runs must work on that connection too: the
	// search-index consistency check reads the index it inspects.
	if err := store.VerifySearchIndex(ctx); err != nil {
		t.Fatalf("search index check on an inspecting connection: %v", err)
	}
	// A snapshot copies the database with SQLite itself rather than copying the file
	// around its write-ahead log.
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if _, err := store.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		t.Fatalf("copying an older database without migrating it: %v", err)
	}
	store.Close()
	copied, err := OpenWithoutMigrating(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if version, err = copied.SchemaVersionIn(ctx, copied); err != nil || version != 1 {
		t.Fatalf("copied database records schema %d, error = %v, want 1", version, err)
	}
	copied.Close()

	// Nothing about the inspection or the snapshot may have changed the file it read.
	after, err := OpenWithoutMigrating(path)
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if version, err = after.SchemaVersionIn(ctx, after); err != nil || version != 1 {
		t.Fatalf("recorded schema after inspection and snapshot = %d, error = %v, want 1", version, err)
	}
	var rows int
	if err := after.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE key=?`, UpgradeStateKey).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("inspection recorded %d upgrade states, error = %v, want none", rows, err)
	}
	// The writing open still migrates, which is the explicit upgrade path an
	// operator has to take deliberately.
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var current, users int
	if err := upgraded.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&current); err != nil || current != SchemaVersion {
		t.Fatalf("writing open left schema %d, error = %v, want %d", current, err, SchemaVersion)
	}
	if err := upgraded.QueryRow(`SELECT count(*) FROM users WHERE id='kept'`).Scan(&users); err != nil || users != 1 {
		t.Fatalf("upgrade lost existing rows: %d, error = %v", users, err)
	}
	// The fixture builder is the same tool the upgrade rehearsal uses, so it has to
	// refuse a version this build cannot describe.
	if err := CreateSchemaFixture(filepath.Join(t.TempDir(), "future.db"), SchemaVersion+1); err == nil {
		t.Fatal("a fixture for a schema this build does not carry was created")
	}
	if err := CreateSchemaFixture(path, 1); err == nil {
		t.Fatal("a fixture builder overwrote an existing database")
	}
}

// TestAnIncompleteUpgradeCannotBeSkipped covers the durable upgrade record: a
// crash between the schema transaction and the postconditions must leave a state
// every later start repairs before it serves, not one that a start with nothing
// left to migrate silently skips.
func TestAnIncompleteUpgradeCannotBeSkipped(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "interrupted.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// This is the row Migrate writes before it commits, standing in for a process
	// that died between the commit and the rebuild. It is written from outside so
	// the test does not depend on where an injected failure is caught.
	if _, err := store.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)`,
		UpgradeStateKey, `{"from":4,"to":5}`, Now()); err != nil {
		t.Fatal(err)
	}
	// Break an invariant the postconditions are responsible for proving.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO messages(id,channel_id,user_id,content,created_at,idempotency_key) VALUES('orphan','missing','missing','text','now','key')`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	raw.Close()
	if err := store.Migrate(ctx); err == nil {
		t.Fatal("an upgrade whose postconditions failed was treated as complete")
	}
	if pending, err := store.UpgradePending(ctx); err != nil || !pending {
		t.Fatalf("upgrade record = %v, error = %v, want it retained after a failed rebuild or verification", pending, err)
	}
	// A later start finds nothing pending, which used to be enough to skip the
	// postconditions entirely.
	if err := store.Migrate(ctx); err == nil {
		t.Fatal("a start with no pending migrations skipped the outstanding postconditions")
	}
	// Repairing the data lets the same record finish and then clear.
	if _, err := store.ExecContext(ctx, `DELETE FROM messages WHERE id='orphan'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repair did not complete the upgrade: %v", err)
	}
	if pending, err := store.UpgradePending(ctx); err != nil || pending {
		t.Fatalf("upgrade record = %v, error = %v, want it cleared after verification", pending, err)
	}
}
