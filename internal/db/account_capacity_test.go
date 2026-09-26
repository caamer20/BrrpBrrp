package db

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestOfflineAdministratorCapacityExcludesOnlyRemovedIdentities(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "capacity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateAdministrator(t.Context(), "operator", "an offline recovery passphrase", 12582912); err != nil {
		t.Fatal(err)
	}
	tx, err := s.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var removable string
	for i := 0; i < 220; i++ {
		id, name, deleted := ID(), fmt.Sprintf("member_%03d", i), i >= 99
		if deleted {
			name = "deleted-" + id
		}
		if i == 0 {
			removable = id
		}
		if _, err := tx.Exec(`INSERT INTO users(id,username,password_hash,is_deactivated,is_deleted,created_at,updated_at) VALUES(?,?,'',?,?,?,?)`, id, name, deleted, deleted, Now(), Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAdministrator(t.Context(), "replacement", "an offline recovery passphrase", 12582912); err == nil {
		t.Fatal("offline recovery exceeded100 live accounts")
	}
	if _, err := s.Exec(`UPDATE users SET is_deleted=1,is_deactivated=1 WHERE id=?`, removable); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAdministrator(t.Context(), "replacement", "an offline recovery passphrase", 12582912); err != nil {
		t.Fatalf("removed identity blocked recovery: %v", err)
	}
	var live, channels int
	if err := s.QueryRow(`SELECT (SELECT count(*) FROM users WHERE is_deleted=0),(SELECT count(*) FROM channels)`).Scan(&live, &channels); err != nil {
		t.Fatal(err)
	}
	if live != 100 || channels != 2 {
		t.Fatalf("recovery changed capacity or re-seeded channels: live=%d channels=%d", live, channels)
	}
}

func TestOfflineRecoveryDoesNotReseedACommunityWithOnlyTombstones(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tombstones.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Even without a setup marker, a retained identity means this is an existing
	// community. Offline recovery may add an operator but must not seed it anew.
	if _, err := s.Exec(`INSERT INTO users(id,username,password_hash,is_deactivated,is_deleted,created_at,updated_at) VALUES(?,'deleted-member','',1,1,?,?)`, ID(), Now(), Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAdministrator(t.Context(), "replacement", "an offline recovery passphrase", 12582912); err != nil {
		t.Fatal(err)
	}
	var channels int
	if err := s.QueryRow(`SELECT count(*) FROM channels`).Scan(&channels); err != nil || channels != 0 {
		t.Fatalf("tombstones reopened initialization: channels=%d err=%v", channels, err)
	}
}
