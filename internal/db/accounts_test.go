package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOfflineAdministrator(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateAdministrator(context.Background(), "first_admin", "a long private passphrase", 12582912); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAdministrator(context.Background(), "first_admin", "a different private passphrase", 12582912); err == nil {
		t.Fatal("existing account overwritten")
	}
	if err := store.CreateAdministrator(context.Background(), "recovery_admin", "a long recovery passphrase", 12582912); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.QueryRow(`SELECT count(*) FROM channels`).Scan(&count); err != nil || count != 2 {
		t.Fatal("recovery reseeded channels", err)
	}
}
