package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

type reportPage struct {
	Reports    []messageReport `json:"reports"`
	NextCursor string          `json:"next_cursor"`
}

func reportedMessage(t *testing.T, s *Server, channelID, authorID, content string) string {
	t.Helper()
	id := db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?,?)`, id, channelID, channelID, authorID, content, db.Now(), id); err != nil {
		t.Fatal(err)
	}
	return id
}

func publicReportChannel(t *testing.T, s *Server) string {
	t.Helper()
	var id string
	if err := s.store.QueryRow(`SELECT id FROM channels WHERE type='text' AND visibility='public' LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReportsRespectCommunityVisibilityAndModerationAuthority(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	adminID := sessionUserID(t, s, admin)
	reporterID, reporter := memberAccount(t, s, "reporter")
	_, outsider := memberAccount(t, s, "outsider")
	moderatorID, moderator := onlyCapability(t, s, handler, "reviewer", "message.delete_any")
	channel := privateTextChannel(t, handler, admin, "reported-private", reporterID)
	messageID := reportedMessage(t, s, channel.ID, adminID, "Selected message evidence")
	path := "/api/messages/" + messageID + "/report"
	created := request(t, handler, "POST", path, map[string]string{"reason": "  This needs review.  "}, reporter)
	if created.Code != http.StatusCreated {
		t.Fatalf("report: %d %s", created.Code, created.Body.String())
	}
	receipt := readJSON[map[string]any](t, created)
	reportID := receipt["id"].(string)
	if receipt["duplicate"] != false {
		t.Fatal("first submission was marked duplicate")
	}
	for key := range receipt {
		if key != "id" && key != "status" && key != "created_at" && key != "expires_at" && key != "duplicate" {
			t.Errorf("submission leaked moderator field %s", key)
		}
	}
	duplicate := request(t, handler, "POST", path, map[string]string{"reason": "Do not overwrite the evidence."}, reporter)
	if duplicate.Code != http.StatusOK {
		t.Fatal(duplicate.Body.String())
	}
	second := readJSON[map[string]any](t, duplicate)
	if second["id"] != reportID || second["duplicate"] != true {
		t.Fatal("retry created another active report")
	}
	if w := request(t, handler, "POST", path, map[string]string{"reason": "Guessed private message."}, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("outsider report: %d", w.Code)
	}
	if w := request(t, handler, "GET", "/api/admin/reports", nil, reporter); w.Code != http.StatusForbidden {
		t.Fatalf("reporter queue: %d", w.Code)
	}
	if w := request(t, handler, "PATCH", "/api/admin/reports/"+reportID, map[string]string{"status": "resolved"}, reporter); w.Code != http.StatusForbidden {
		t.Fatalf("reporter review: %d", w.Code)
	}
	page := readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, moderator))
	if len(page.Reports) != 0 || page.NextCursor != "" {
		t.Fatalf("unreadable report leaked to moderator: %+v", page)
	}
	if w := request(t, handler, "PATCH", "/api/admin/reports/"+reportID, map[string]string{"status": "resolved"}, moderator); w.Code != http.StatusNotFound {
		t.Fatalf("unreadable review: %d", w.Code)
	}
	grant := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private", "user_ids": []string{reporterID, moderatorID}}, admin)
	if grant.Code != http.StatusOK {
		t.Fatal(grant.Body.String())
	}
	page = readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, moderator))
	if len(page.Reports) != 1 || page.Reports[0].SnapshotText != "Selected message evidence" || page.Reports[0].Reason != "This needs review." {
		t.Fatalf("wrong evidence: %+v", page)
	}
	// Reviewing evidence does not punish the author or use role hierarchy to hide
	// a report about an administrator. Only current channel access is relevant.
	reviewed := request(t, handler, "PATCH", "/api/admin/reports/"+reportID, map[string]string{"status": "resolved", "note": "Reviewed with the community."}, moderator)
	if reviewed.Code != http.StatusOK {
		t.Fatalf("review: %d %s", reviewed.Code, reviewed.Body.String())
	}
	resolved := readJSON[messageReport](t, reviewed)
	if resolved.ReviewedBy != moderatorID || resolved.Status != "resolved" {
		t.Fatalf("review not attributed: %+v", resolved)
	}
	var audits int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE actor_id=? AND target_id=? AND action='report.resolved'`, moderatorID, reportID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("review audit count=%d err=%v", audits, err)
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+messageID, map[string]string{"confirm": messageID}, admin); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	page = readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports?status=resolved", nil, moderator))
	if len(page.Reports) != 1 || page.Reports[0].SnapshotText != "Selected message evidence" {
		t.Fatal("source deletion erased the disclosed evidence snapshot")
	}
	request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private", "user_ids": []string{reporterID}}, admin)
	page = readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports?status=resolved", nil, moderator))
	if len(page.Reports) != 0 {
		t.Fatal("revoked reviewer kept evidence access")
	}
}

func TestReportEvidenceBoundsExpiryAndPrivateConversationExclusion(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	authorID, _ := memberAccount(t, s, "author")
	_, reporter := memberAccount(t, s, "reporter")
	channelID := publicReportChannel(t, s)
	messageID := reportedMessage(t, s, channelID, authorID, strings.Repeat("界", 4500))
	path := "/api/messages/" + messageID + "/report"
	for _, reason := range []string{"", "  ", strings.Repeat("界", 2001)} {
		if w := request(t, handler, "POST", path, map[string]string{"reason": reason}, reporter); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid reason accepted: %d", w.Code)
		}
	}
	created := request(t, handler, "POST", path, map[string]string{"reason": strings.Repeat("界", 2000)}, reporter)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	id := readJSON[map[string]any](t, created)["id"].(string)
	page := readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, admin))
	if len(page.Reports) != 1 || len([]rune(page.Reports[0].SnapshotText)) != 4000 {
		t.Fatal("snapshot did not keep the bounded selected text")
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, page.Reports[0].CreatedAt)
	expiresAt, _ := time.Parse(time.RFC3339Nano, page.Reports[0].ExpiresAt)
	if expiresAt.Sub(createdAt) != reportEvidenceLifetime {
		t.Fatal("wrong evidence availability window")
	}
	if _, err := s.store.Exec(`DELETE FROM users WHERE id=?`, authorID); err != nil {
		t.Fatal(err)
	}
	page = readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, admin))
	if len(page.Reports) != 1 || page.Reports[0].AuthorID != "" {
		t.Fatal("author deletion broke retained evidence or left an identity reference")
	}
	if _, err := s.store.Exec(`UPDATE message_reports SET expires_at=? WHERE id=?`, db.Timestamp(time.Now().Add(-time.Second)), id); err != nil {
		t.Fatal(err)
	}
	page = readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, admin))
	if len(page.Reports) != 0 || page.NextCursor != "" {
		t.Fatal("expired evidence was listed")
	}
	if w := request(t, handler, "PATCH", "/api/admin/reports/"+id, map[string]string{"status": "resolved"}, admin); w.Code != http.StatusNotFound {
		t.Fatalf("expired evidence reviewed: %d", w.Code)
	}
	// A future private conversation cannot enter this community-only queue even
	// if a compatibility row still has a readable channel_id.
	messageID = reportedMessage(t, s, channelID, sessionUserID(t, s, admin), "Private conversation content")
	directID := db.ID()
	if _, err := s.store.Exec(`INSERT INTO conversations(id,kind,created_at,updated_at) VALUES(?,'direct',?,?)`, directID, db.Now(), db.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE messages SET conversation_id=? WHERE id=?`, directID, messageID); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "POST", "/api/messages/"+messageID+"/report", map[string]string{"reason": "Must be private."}, reporter); w.Code != http.StatusNotFound {
		t.Fatalf("private conversation report entered community flow: %d", w.Code)
	}
}

func TestReportQueuePaginatesOnlyReadableUnexpiredEvidence(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	authorID := sessionUserID(t, s, admin)
	_, moderator := onlyCapability(t, s, handler, "reviewer", "message.delete_any")
	publicID := publicReportChannel(t, s)
	private := privateTextChannel(t, handler, admin, "hidden-reports", authorID)
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 160; i++ {
		channelID, expiry := publicID, db.Timestamp(time.Now().Add(time.Hour))
		if i >= 51 && i < 110 {
			channelID = private.ID
		}
		if i >= 110 {
			expiry = db.Timestamp(time.Now().Add(-time.Hour))
		}
		if _, err := tx.Exec(`INSERT INTO message_reports(id,message_id,channel_id,reporter_id,author_id,reason,snapshot_text,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, db.ID(), db.ID(), channelID, authorID, authorID, "Needs review", "One chosen message", db.Now(), db.Now(), expiry); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	first := readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports", nil, moderator))
	if len(first.Reports) != 50 || first.NextCursor == "" {
		t.Fatalf("first page: %d next=%q", len(first.Reports), first.NextCursor)
	}
	seen := map[string]bool{}
	for _, report := range first.Reports {
		if report.ChannelID != publicID {
			t.Fatal("hidden report in page")
		}
		seen[report.ID] = true
	}
	second := readJSON[reportPage](t, request(t, handler, "GET", "/api/admin/reports?before="+first.NextCursor, nil, moderator))
	if len(second.Reports) != 1 || second.NextCursor != "" || seen[second.Reports[0].ID] {
		t.Fatalf("pagination skipped or repeated evidence: %+v", second)
	}
	if w := request(t, handler, "GET", "/api/admin/reports?status=all", nil, moderator); w.Code != http.StatusBadRequest {
		t.Fatal("invalid queue state accepted")
	}
}

func TestReportMutationsReauthorizeAndReviewAuditIsAtomic(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	reporterID, reporter := memberAccount(t, s, "reporter")
	moderatorID, moderator := onlyCapability(t, s, handler, "reviewer", "message.delete_any")
	channel := privateTextChannel(t, handler, admin, "revoked-reports", reporterID)
	messageID := reportedMessage(t, s, channel.ID, sessionUserID(t, s, admin), "Selected evidence")
	grant := func(ids []string) error {
		w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private", "user_ids": ids}, admin)
		if w.Code != http.StatusOK {
			return fmt.Errorf("grant: %d %s", w.Code, w.Body.String())
		}
		return nil
	}
	body := bodyThatRevokes(t, map[string]string{"reason": "Late report"}, func() error { return grant(nil) })
	if w := pausedRequest(t, handler, "POST", "/api/messages/"+messageID+"/report", reporter, body); w.Code != http.StatusNotFound {
		t.Fatalf("revoked report submission: %d", w.Code)
	}
	if err := grant([]string{reporterID, moderatorID}); err != nil {
		t.Fatal(err)
	}
	created := request(t, handler, "POST", "/api/messages/"+messageID+"/report", map[string]string{"reason": "Current report"}, reporter)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	id := readJSON[map[string]any](t, created)["id"].(string)
	body = bodyThatRevokes(t, map[string]string{"status": "resolved", "note": "Late review"}, func() error { return grant([]string{reporterID}) })
	if w := pausedRequest(t, handler, "PATCH", "/api/admin/reports/"+id, moderator, body); w.Code != http.StatusNotFound {
		t.Fatalf("revoked review: %d", w.Code)
	}
	if err := grant([]string{reporterID, moderatorID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`CREATE TRIGGER deny_report_audit BEFORE INSERT ON audit_logs WHEN NEW.action LIKE 'report.%' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "PATCH", "/api/admin/reports/"+id, map[string]string{"status": "resolved", "note": "Must roll back"}, moderator); w.Code != http.StatusInternalServerError {
		t.Fatalf("missing required audit succeeded: %d", w.Code)
	}
	var status, note string
	if err := s.store.QueryRow(`SELECT status,review_note FROM message_reports WHERE id=?`, id).Scan(&status, &note); err != nil {
		t.Fatal(err)
	}
	if status != "open" || note != "" {
		t.Fatal("review committed without its audit")
	}
}

func TestReportsBoundNewSubmissionsAndKeepRetriesPrivate(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	reporterID, reporter := memberAccount(t, s, "reporter")
	_, other := memberAccount(t, s, "other")
	channelID := publicReportChannel(t, s)
	var firstID, receiptID string
	for i := 0; i < 11; i++ {
		id := reportedMessage(t, s, channelID, sessionUserID(t, s, admin), "Message for review")
		w := request(t, handler, "POST", "/api/messages/"+id+"/report", map[string]string{"reason": "Needs a review"}, reporter)
		if i < 10 && w.Code != http.StatusCreated {
			t.Fatalf("submission %d: %d %s", i, w.Code, w.Body.String())
		}
		if i == 0 {
			firstID = id
			receiptID = readJSON[map[string]any](t, w)["id"].(string)
		}
		if i == 10 && (w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "") {
			t.Fatal("new submission budget not enforced")
		}
	}
	duplicate := request(t, handler, "POST", "/api/messages/"+firstID+"/report", map[string]string{"reason": "Retry"}, reporter)
	if duplicate.Code != http.StatusOK || readJSON[map[string]any](t, duplicate)["id"] != receiptID {
		t.Fatal("existing receipt could not be retried after limit")
	}
	independent := request(t, handler, "POST", "/api/messages/"+firstID+"/report", map[string]string{"reason": "Another reporter"}, other)
	if independent.Code != http.StatusCreated || readJSON[map[string]any](t, independent)["id"] == receiptID {
		t.Fatal("one reporter learned another reporter's receipt")
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM message_reports WHERE reporter_id=?`, reporterID).Scan(&count); err != nil || count != 10 {
		t.Fatalf("unbounded submissions: %d %v", count, err)
	}
}
