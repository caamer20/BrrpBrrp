package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"context"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

// The shipped interval is the ratified cadence of 12 hours, and a fresh setup
// writes it into the row an existing installation would later reopen over.
func TestFreshSetupStoresTheTwelveHourBackupInterval(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	if got := s.Settings().BackupHours; got != 12 {
		t.Fatalf("in-memory interval after setup = %d, want 12", got)
	}
	var stored string
	if err := s.store.QueryRow(`SELECT value FROM settings WHERE key='public'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var saved db.Settings
	if err := json.Unmarshal([]byte(stored), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.BackupHours != 12 {
		t.Fatalf("stored interval = %d, want 12", saved.BackupHours)
	}
	reported := readJSON[db.Settings](t, request(t, handler, "GET", "/api/admin/settings", nil, admin))
	if reported.BackupHours != 12 {
		t.Fatalf("reported interval = %d, want 12", reported.BackupHours)
	}
}

// Changing the default must not change an installation that already chose. The row
// below is what an installation set up before this release holds: it names its
// interval explicitly, and the server decodes it over DefaultSettings() rather than
// letting the default win. A zero-value field would be indistinguishable from an
// absent one, which is why the interval carries no omitempty.
func TestAStoredBackupIntervalSurvivesTheDefault(t *testing.T) {
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	c, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Prepare(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(c.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	// Offline setup inserts its own settings with INSERT OR IGNORE, so seeding the
	// row first is what an installation that already chose an interval looks like.
	stored := `{"server_name":"An older installation","backup_enabled":true,"backup_interval_hours":24,"backup_keep":7}`
	if _, err := store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('public',?,?)`, stored, db.Now()); err != nil {
		t.Fatal(err)
	}
	const password = "a stored interval passphrase"
	if err := store.CreateAdministrator(context.Background(), "admin", password, 12<<20); err != nil {
		t.Fatal(err)
	}
	s, err := New(c, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	handler := s.Handler()

	if got := s.Settings().BackupHours; got != 24 {
		t.Fatalf("a stored 24 reports %d; the new default overwrote an operator's choice", got)
	}
	signedIn := request(t, handler, "POST", "/api/login", map[string]string{"username": "admin", "password": password}, nil)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("login: %d %s", signedIn.Code, signedIn.Body.String())
	}
	reported := readJSON[db.Settings](t, request(t, handler, "GET", "/api/admin/settings", nil, signedIn.Result().Cookies()[0]))
	if reported.BackupHours != 24 {
		t.Fatalf("the settings route reported %d, want the stored 24", reported.BackupHours)
	}
}

// A new installation starts with scheduled backups switched on (owner decision of 6
// October 2026), and a server that has not been set up yet does not: with nothing
// recorded a snapshot is due at once, so an un-set-up server that carried the flag would
// archive an empty database. The flag therefore arrives with setup — in the stored row
// and in the running server together, without a restart.
func TestANewInstallationStartsWithScheduledBackupsOn(t *testing.T) {
	s, handler := testServer(t)

	if s.Settings().BackupEnabled {
		t.Fatal("a server that has not been set up carries scheduled backups")
	}
	s.scheduledBackup(time.Now())
	if status := s.backupStatus(); status.LastAttempt != "" || status.ID != "" {
		t.Fatalf("a server that has not been set up attempted a backup: %+v", status)
	}

	admin := setupAdmin(t, s, handler)
	if !s.Settings().BackupEnabled {
		t.Fatal("setup finished and the running server has scheduled backups off")
	}
	var stored string
	if err := s.store.QueryRow(`SELECT value FROM settings WHERE key='public'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var saved db.Settings
	if err := json.Unmarshal([]byte(stored), &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.BackupEnabled {
		t.Fatalf("the stored row has scheduled backups off: %s", stored)
	}
	reported := readJSON[db.Settings](t, request(t, handler, "GET", "/api/admin/settings", nil, admin))
	if !reported.BackupEnabled {
		t.Fatal("the settings route reports scheduled backups off after setup")
	}
	// Everything else a new installation gets is still the default: the function that
	// switches the flag on changes nothing beside it.
	want := db.DefaultSettings()
	want.BackupEnabled = true
	if got := db.NewInstallationSettings(); got != want {
		t.Fatalf("a new installation differs from the defaults in more than the flag:\n got %+v\nwant %+v", got, want)
	}
}

// Offline setup — `create-admin` against a database that has never been set up — is the
// other place a first settings row is written, and it makes the same installation.
func TestOfflineSetupStartsWithScheduledBackupsOn(t *testing.T) {
	c, store := untouchedStore(t)
	if err := store.CreateAdministrator(context.Background(), "admin", "an offline setup passphrase", 12<<20); err != nil {
		t.Fatal(err)
	}
	s, err := New(c, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if !s.Settings().BackupEnabled {
		t.Fatal("an installation set up offline starts with scheduled backups off")
	}
	if got := s.Settings().BackupHours; got != 12 {
		t.Fatalf("an installation set up offline has a %d-hour interval, want 12", got)
	}
}

// The decision is about new installations. One that already exists keeps what it holds:
// an explicit "off" stays off, and so does a row written before the setting existed,
// which has no key at all and is decoded over DefaultSettings — where the flag is false
// for exactly this reason.
func TestAnExistingInstallationKeepsItsBackupChoice(t *testing.T) {
	for name, stored := range map[string]string{
		"an explicit off":             `{"server_name":"An older installation","backup_enabled":false,"backup_interval_hours":24,"backup_keep":7}`,
		"a row that predates the key": `{"server_name":"An older installation"}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, store := untouchedStore(t)
			if _, err := store.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('public',?,?)`, stored, db.Now()); err != nil {
				t.Fatal(err)
			}
			// Adding an administrator offline must not rewrite the row either.
			if err := store.CreateAdministrator(context.Background(), "admin", "an existing installation passphrase", 12<<20); err != nil {
				t.Fatal(err)
			}
			s, err := New(c, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			if s.Settings().BackupEnabled {
				t.Fatal("an existing installation was switched to scheduled backups without choosing them")
			}
			s.scheduledBackup(time.Now())
			if status := s.backupStatus(); status.LastAttempt != "" {
				t.Fatalf("an installation with scheduled backups off attempted one: %+v", status)
			}
		})
	}
}

// untouchedStore opens a database that has never been set up, with its configuration.
func untouchedStore(t *testing.T) (app.Config, *db.Store) {
	t.Helper()
	t.Setenv("BRRP_DATA_DIR", t.TempDir())
	c, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Prepare(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(c.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return c, store
}
