package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"brrpbrrp/internal/db"
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

// V13-08. Private evidence expires for a reader long before its bytes are gone, and
// only the message-evidence table was ever reclaimed: a request's submitted
// introduction and its command receipts stayed in the database for the life of the
// installation.
//
// That is the retention promise failing quietly. A person was told their evidence
// would be available for 90 days, which is true, and not that it would be kept
// forever, which is what happened. Receipts are the sharper case: an operation key
// has to stay reserved after its receipt window closes, or a late retry becomes new
// work — so the row cannot simply be deleted either.
func TestExpiredRequestEvidenceAndItsReceiptsAreReclaimedWithoutResurrectingAKey(t *testing.T) {
	s, h, guest, _, conversation, _, _ := requestFixture(t)
	preview := previewRequest(t, h, guest, conversation)
	// A report, so there is real evidence and a real receipt to reclaim.
	_ = preview
	saved := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "retained evidence", "version": preview.Version, "operation_key": "reclaim-key-0001",
	}, guest)
	if saved.Code != 201 {
		t.Fatalf("report: %d %s", saved.Code, saved.Body.String())
	}

	// Age the evidence past its window and the receipt past its own.
	old := db.Timestamp(time.Now().AddDate(0, 0, -200))
	if _, err := s.store.Exec(`UPDATE private_request_reports SET expires_at=?`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE private_report_receipts SET expires_at=?`, old); err != nil {
		t.Fatal(err)
	}
	if err := s.maintain(time.Now()); err != nil {
		t.Fatal(err)
	}

	// The submitted text is gone: it is disclosed content with a stated lifetime.
	var evidence int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_request_reports`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 {
		t.Fatalf("%d expired evidence rows kept their submitted text", evidence)
	}
	// The receipt's payload is gone too: the outcome becomes the guard marker and the
	// evidence, source, attempt, reporter and subject identities are cleared.
	var receipts int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_report_receipts WHERE outcome<>'spent' OR evidence_id<>'' OR source_id<>'' OR subject_id<>''`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("%d aged receipts kept their payload", receipts)
	}
	// But the key stays spent: a guard row that records only that the key was used,
	// so a late retry is refused as expired rather than becoming new work.
	var guards int
	if err := s.store.QueryRow(`SELECT count(*) FROM private_report_receipts WHERE operation_key='reclaim-key-0001' AND outcome='spent' AND evidence_id=''`).Scan(&guards); err != nil {
		t.Fatal(err)
	}
	if guards != 1 {
		t.Fatalf("the spent key was not kept as a guard: %d guard rows", guards)
	}
	// And the guarantee holds across the cleanup, a restart and a restore: the key
	// still cannot run the operation again.
	retry := request(t, h, "POST", "/api/directs/"+conversation+"/report-and-block", map[string]string{
		"reason": "retained evidence", "version": preview.Version, "operation_key": "reclaim-key-0001",
	}, guest)
	if retry.Code != http.StatusGone && retry.Code != http.StatusConflict {
		t.Fatalf("an aged key ran the operation again: %d %s", retry.Code, retry.Body.String())
	}
}
