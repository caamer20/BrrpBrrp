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

// Restricted review audit metadata has to outlive the evidence it describes, and
// the community audit log must still be reclaimed on the shorter clock. ADR 0014
// promises both, and they fail independently: reclaiming restricted entries with
// the ordinary ones destroys the only record that a private review happened, while
// keeping them forever is not what was promised either.
func TestRestrictedReviewAuditOutlivesCommunityAudit(t *testing.T) {
	s, h := testServer(t)
	setupAdmin(t, s, h)
	now := time.Now()
	// Past the community age, short of the restricted one.
	stale := db.Timestamp(now.Add(-200 * 24 * time.Hour))
	// Past both, so it must actually be reclaimed.
	ancient := db.Timestamp(now.Add(-500 * 24 * time.Hour))
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, row := range []struct{ id, action, at string }{
		{"community", "channel.purged", stale},
		{"review", "private_report.resolved", stale},
		{"request", "request_evidence.reported", stale},
		{"ancient-review", "private_report.resolved", ancient},
		{"ancient-community", "channel.purged", ancient},
	} {
		if _, err := tx.Exec(`INSERT INTO audit_logs(id,action,target_id,created_at) VALUES(?,?,'',?)`, row.id, row.action, row.at); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.maintain(now); err != nil {
		t.Fatal(err)
	}
	remaining := map[string]bool{}
	rows, err := s.store.Query(`SELECT id FROM audit_logs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		remaining[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !remaining["review"] || !remaining["request"] {
		t.Fatal("restricted review audit was reclaimed with the evidence it describes")
	}
	if remaining["community"] {
		t.Fatal("community audit outlived its retention")
	}
	if remaining["ancient-review"] || remaining["ancient-community"] {
		t.Fatal("audit past both retentions survived")
	}
}
