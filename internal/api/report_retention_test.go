package api

import (
	"brrpbrrp/internal/db"
	"fmt"
	"testing"
	"time"
)

func TestExpiredReportEvidenceIsPurgedInBoundedBatches(t *testing.T) {
	s, h := testServer(t)
	setupAdmin(t, s, h)
	channel := textChannelID(t, s)
	now := time.Now()
	old := db.Timestamp(now.Add(-time.Hour))
	future := db.Timestamp(now.Add(time.Hour))
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 501; i++ {
		if _, err := tx.Exec(`INSERT INTO message_reports(id,message_id,channel_id,reason,snapshot_text,status,created_at,updated_at,expires_at) VALUES(?,?,?,'reason','selected evidence','resolved',?,?,?)`, fmt.Sprintf("expired-%03d", i), fmt.Sprintf("message-%d", i), channel, old, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO message_reports(id,message_id,channel_id,reason,snapshot_text,status,created_at,updated_at,expires_at) VALUES('live','live-message',?,'reason','still needed','open',?,?,?)`, channel, old, old, future); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for pass, want := range []int{2, 1} {
		if err := s.maintain(now); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := s.store.QueryRow(`SELECT count(*) FROM message_reports`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("pass%d count%d want%d", pass, count, want)
		}
	}
	var text string
	if err := s.store.QueryRow(`SELECT snapshot_text FROM message_reports WHERE id='live'`).Scan(&text); err != nil || text != "still needed" {
		t.Fatalf("unexpired evidence removed: %q %v", text, err)
	}
}
