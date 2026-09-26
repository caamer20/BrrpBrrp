package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func capacityAccounts(t *testing.T, s *Server, count int, deleted bool) []string {
	t.Helper()
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		id := db.ID()
		name := fmt.Sprintf("capacity_%03d", i)
		if deleted {
			name = "deleted-" + id
		}
		if _, err := tx.Exec(`INSERT INTO users(id,username,password_hash,is_deactivated,is_deleted,created_at,updated_at) VALUES(?,?,'',?,?,?,?)`, id, name, deleted, deleted, db.Now(), db.Now()); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestAccountCapacityCountsRecoverableAccountsButNotRemovedIdentities(t *testing.T) {
	for _, mode := range []string{"admin", "invitation"} {
		t.Run(mode, func(t *testing.T) {
			s, handler := testServer(t)
			admin := setupAdmin(t, s, handler)
			enrollMFA(t, handler, admin, "correct horse battery")
			ids := capacityAccounts(t, s, 99, false)
			capacityAccounts(t, s, 120, true)
			// Recoverable bans and deactivations still occupy their account slots.
			if _, err := s.store.Exec(`UPDATE users SET is_banned=1 WHERE id=?`, ids[1]); err != nil {
				t.Fatal(err)
			}
			if _, err := s.store.Exec(`UPDATE users SET is_deactivated=1 WHERE id=?`, ids[2]); err != nil {
				t.Fatal(err)
			}
			var token string
			if mode == "invitation" {
				invitation := request(t, handler, "POST", "/api/admin/invitations", map[string]int{"hours": 24}, admin)
				if invitation.Code != http.StatusCreated {
					t.Fatal(invitation.Body.String())
				}
				token = strings.Split(readJSON[map[string]any](t, invitation)["url"].(string), "#invite=")[1]
			}
			create := func(name string) int {
				body := map[string]string{"username": name, "password": "a replacement member passphrase"}
				if mode == "invitation" {
					body["token"] = token
					return request(t, handler, "POST", "/api/invitations/accept", body, nil).Code
				}
				return request(t, handler, "POST", "/api/admin/users", body, admin).Code
			}
			if code := create("replacement"); code != http.StatusConflict {
				t.Fatalf("100 live/recoverable accounts admitted another: %d", code)
			}
			removed := request(t, handler, "POST", "/api/admin/users/"+ids[0]+"/anonymize", map[string]string{"confirm": "capacity_000"}, admin)
			if removed.Code != http.StatusOK {
				t.Fatal(removed.Body.String())
			}
			if code := create("replacement"); code != http.StatusCreated {
				t.Fatalf("removed identity still occupied capacity: %d", code)
			}
			var live, total int
			if err := s.store.QueryRow(`SELECT (SELECT count(*) FROM users WHERE is_deleted=0),(SELECT count(*) FROM users)`).Scan(&live, &total); err != nil {
				t.Fatal(err)
			}
			if live != 100 || total != 221 {
				t.Fatalf("live=%d total=%d", live, total)
			}
			if required, err := s.setupRequired(); err != nil || required {
				t.Fatalf("setup reopened: required=%v err=%v", required, err)
			}
			if _, err := s.store.Exec(`UPDATE users SET is_deactivated=0 WHERE id=?`, ids[0]); err == nil {
				t.Fatal("capacity release made the removed identity recoverable")
			}
		})
	}
}

func TestRemovedIdentitiesAloneDoNotReopenSetup(t *testing.T) {
	s, _ := testServer(t)
	ids := capacityAccounts(t, s, 120, true)
	if required, err := s.setupRequired(); err != nil || required {
		t.Fatalf("tombstones reopened setup: required=%v err=%v", required, err)
	}
	if _, err := s.store.Exec(`UPDATE users SET is_deleted=0,is_deactivated=0 WHERE id=?`, ids[0]); err == nil {
		t.Fatal("tombstone could be reactivated")
	}
}

func TestConcurrentAccountCreationCannotExceedTheLastSlot(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	capacityAccounts(t, s, 98, false)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, name := range []string{"last_slot_one", "last_slot_two"} {
		go func() {
			<-start
			results <- performRequest(handler, "POST", "/api/admin/users", map[string]string{"username": name, "password": "a last-slot member passphrase"}, admin).Code
		}()
	}
	close(start)
	a, b := <-results, <-results
	if !((a == http.StatusCreated && b == http.StatusConflict) || (a == http.StatusConflict && b == http.StatusCreated)) {
		t.Fatalf("last slot outcomes=%d,%d", a, b)
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM users WHERE is_deleted=0`).Scan(&count); err != nil || count != 100 {
		t.Fatalf("live=%d err=%v", count, err)
	}
}
