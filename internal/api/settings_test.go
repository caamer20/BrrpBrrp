package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

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
