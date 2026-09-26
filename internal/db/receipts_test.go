package db

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestReceiptCompactionIsBoundedAndNeverReleasesKeys(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := Now()
	if _, err := s.Exec("INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES('account','reader','unused',?,?)", now, now); err != nil {
		t.Fatal(err)
	}
	tx, err := s.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	old := Timestamp(time.Now().Add(-40 * 24 * time.Hour))
	for i := 0; i < 1002; i++ {
		if err := s.BeginOperation(t.Context(), tx, Receipt{AccountID: "account", Key: fmt.Sprintf("operation-%d", i), ConversationID: "gone-room", IntentDigest: "fingerprint", CreatedAt: old, ExpiresAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.BeginOperation(t.Context(), tx, Receipt{AccountID: "account", Key: "surviving-operation", ConversationID: "live-room", IntentDigest: "keep", CreatedAt: old, ExpiresAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteOperation(t.Context(), tx, "account", "surviving-operation", "live-message"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for pass, want := range []int64{1000, 2, 0} {
		got, err := s.PruneSendReceipts(t.Context(), s, now)
		if err != nil || got != want {
			t.Fatalf("pass %d: compacted %d want%d, %v", pass, got, want, err)
		}
	}
	var total int
	if err := s.QueryRow("SELECT count(*) FROM message_operations").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1003 {
		t.Fatalf("keys released: %d", total)
	}
	spent, err := s.Receipt(t.Context(), s, "account", "operation-0")
	if err != nil || spent.ConversationID != "" || spent.IntentDigest != "" || spent.CreatedAt != "" || spent.State != "deleted" {
		t.Fatalf("spent metadata: %+v %v", spent, err)
	}
	live, err := s.Receipt(t.Context(), s, "account", "surviving-operation")
	if err != nil || live.ConversationID != "live-room" || live.IntentDigest != "keep" || live.ResultMessageID != "live-message" {
		t.Fatalf("live receipt damaged: %+v %v", live, err)
	}
	// The account lifecycle is the terminal bound on the minimal key registry.
	if _, err := s.Exec("DELETE FROM users WHERE id='account'"); err != nil {
		t.Fatal(err)
	}
	if err := s.QueryRow("SELECT count(*) FROM message_operations").Scan(&total); err != nil || total != 0 {
		t.Fatalf("account erasure kept receipts: %d %v", total, err)
	}
}
