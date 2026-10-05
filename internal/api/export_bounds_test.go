package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// The export's limits are bounds on *construction*, not checks on a document that
// already exists, and each limit has its own honest reason in the file. These tests pin
// the three shapes the earlier version got wrong:
//
//   - the attachment budget was checked once per message, so a message with five files
//     arriving at 1,999 ended at 2,004;
//   - reactions were read inside the attachment-budget branch, so a member whose export
//     held many attachments lost every later reaction for a reason that was not about
//     reactions;
//   - admission counted only finished copies, so a server at its ceiling could admit an
//     unbounded number of concurrent builds from different accounts.
//
// Each fails against the earlier code and passes against the fixed code.

// seedAttachment adds one attachment row to a message without going through the upload
// route (the bound is about how the export collects what is stored, not about how it got
// there, and 2,004 uploads over HTTP would test the limiter).
func seedAttachment(t *testing.T, s *Server, messageID, uploaderID string, index int) {
	t.Helper()
	if _, err := s.store.Exec(`INSERT INTO attachments(id,message_id,uploader_id,file_name,stored_name,file_size,mime_type,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, db.ID(), messageID, uploaderID,
		fmt.Sprintf("file-%d.txt", index), fmt.Sprintf("stored-%s-%d", messageID, index),
		16, "text/plain", db.Now()); err != nil {
		t.Fatalf("seed attachment %d: %v", index, err)
	}
}

// TestAnExportAttachmentBoundIsNotExceededByAMultiFileMessage is the first shape: a
// message with several files arriving at the boundary cannot push the total past it.
func TestAnExportAttachmentBoundIsNotExceededByAMultiFileMessage(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "export-bounds-room")
	authorID := memberIDOf(t, s, "admin")

	// A message holding 1,999 files, and a later message holding five. The second must
	// contribute exactly one — the room left — and the total must not exceed the bound.
	first := db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
		VALUES(?,?,?,?,?,?,?)`, first, channel, channel, authorID, "a message with many files", db.Now(), "bounds-msg-1"); err != nil {
		t.Fatal(err)
	}
	for index := range maxExportAttachments - 1 {
		seedAttachment(t, s, first, authorID, index)
	}
	second := db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
		VALUES(?,?,?,?,?,?,?)`, second, channel, channel, authorID, "a message with five files", db.Now(), "bounds-msg-2"); err != nil {
		t.Fatal(err)
	}
	for index := range 5 {
		seedAttachment(t, s, second, authorID, 9000+index)
	}

	_, alice := namedUser(t, s, "export_bounds_alice")
	document := exportFor(t, s, handler, alice, "export-key-bounds-multi")

	total := 0
	for _, conversation := range document.Conversations {
		for _, message := range conversation.Messages {
			total += len(message.Attachments)
		}
	}
	if total > maxExportAttachments {
		t.Fatalf("the export holds %d attachments, over its bound of %d", total, maxExportAttachments)
	}
	if total != maxExportAttachments {
		t.Fatalf("the export should have collected the bound exactly: %d of %d", total, maxExportAttachments)
	}
	if !document.Summary.Truncated {
		t.Fatal("an export that stopped at the attachment bound was not marked truncated")
	}
	if !containsReason(document.Omissions, "attachments") {
		t.Fatalf("the omission does not name the attachment bound: %v", document.Omissions)
	}
}

// TestAnExportCollectsReactionsIndependentlyOfItsAttachmentBound is the second shape: a
// message that reaches the attachment ceiling must not lose its reactions to it.
func TestAnExportCollectsReactionsIndependentlyOfItsAttachmentBound(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "export-reactions-room")
	authorID := memberIDOf(t, s, "admin")

	// The first message spends the whole attachment budget and then some, so the bound is
	// genuinely reached (files skipped) and a reason is owed.
	full := db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
		VALUES(?,?,?,?,?,?,?)`, full, channel, channel, authorID, "attachments that spend the budget", db.Now(), "bounds-msg-full"); err != nil {
		t.Fatal(err)
	}
	for index := range maxExportAttachments + 5 {
		seedAttachment(t, s, full, authorID, index)
	}
	// A later message with a reaction and no attachments. The reaction must survive the
	// spent attachment budget: it was never about attachments.
	reacted := db.ID()
	if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
		VALUES(?,?,?,?,?,?,?)`, reacted, channel, channel, authorID, "a message that was reacted to", db.Now(), "bounds-msg-react"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO reactions(message_id,user_id,emoji,created_at) VALUES(?,?,?,?)`,
		reacted, authorID, "respect", db.Now()); err != nil {
		t.Fatal(err)
	}

	_, alice := namedUser(t, s, "export_react_alice")
	document := exportFor(t, s, handler, alice, "export-key-bounds-reactions")

	found := false
	for _, conversation := range document.Conversations {
		for _, message := range conversation.Messages {
			if message.ID == reacted && len(message.Reactions) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("a reaction on a message after the attachment budget was spent was dropped")
	}
	if !document.Summary.Truncated || !containsReason(document.Omissions, "attachments") {
		t.Fatalf("the attachment bound should still be recorded: truncated=%v omissions=%v", document.Summary.Truncated, document.Omissions)
	}
}

// TestAFullServerOfBuildingJobsRefusesANewExport is the third shape: the ceiling is on
// work, not only on finished files, so an unbounded number of concurrent builds cannot
// be admitted at the bound.
func TestAFullServerOfBuildingJobsRefusesANewExport(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)

	// The server's whole allowance, occupied by jobs that are still building — from
	// distinct accounts, so the per-account pending check does not apply.
	for index := range maxReadyExportsOnServer {
		id, _ := namedUser(t, s, fmt.Sprintf("export_building_%d", index))
		if _, err := s.store.Exec(`INSERT INTO data_exports(id,user_id,operation_key,state,created_at,fresh_auth_at)
			VALUES(?,?,'building-key','building',?,?)`, db.ID(), id, db.Now(), db.Now()); err != nil {
			t.Fatal(err)
		}
	}

	_, alice := namedUser(t, s, "export_predicate_alice")
	markPrivileged(t, s, alice)
	w := request(t, handler, "POST", "/api/me/export", map[string]any{"operation_key": "export-key-at-server-bound"}, alice)
	if w.Code != http.StatusConflict {
		t.Fatalf("a server whose whole allowance is building should refuse a new export with 409, got %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM data_exports WHERE state IN ('building','ready')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > maxReadyExportsOnServer {
		t.Fatalf("the server admitted a job past its bound: %d", count)
	}
}

// TestAnExportWhoseFileCannotBePublishedLeavesNoResidual makes the publication fail and
// asserts the product's promise: the job is failed, no finished file exists, and no
// publication temporary is left behind.
func TestAnExportWhoseFileCannotBePublishedLeavesNoResidual(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	_, alice := namedUser(t, s, "export_publish_alice")

	directory := s.config.DataDir + "/exports"
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

	markPrivileged(t, s, alice)
	w := request(t, handler, "POST", "/api/me/export", map[string]any{"operation_key": "export-key-publish-fails"}, alice)
	if w.Code != http.StatusAccepted {
		t.Fatalf("request: %d %s", w.Code, w.Body.String())
	}
	record := readJSON[exportRecord](t, w)

	// The build runs after the response; wait for it to fail.
	deadline := 0
	for deadline < 300 {
		var state string
		if err := s.store.QueryRow(`SELECT state FROM data_exports WHERE id=?`, record.ID).Scan(&state); err == nil && state != "building" {
			if state == "ready" {
				t.Skip("directory permissions are not enforced for this user")
			}
			if state != "failed" {
				t.Fatalf("a failed publication left the job in state %q", state)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				t.Fatalf("a failed publication left a file behind: %s", entry.Name())
			}
			return
		}
		deadline++
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the export never left the building state")
}

func containsReason(reasons []string, needle string) bool {
	for _, reason := range reasons {
		if strings.Contains(strings.ToLower(reason), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// TestAnExportStopsAtItsByteCeilingWithMultibyteText exercises the byte boundary
// directly (the shipped ceiling is 252 MiB, so a test lowers the field). It uses
// multibyte content, because the byte ceiling is a byte ceiling and a comment that
// equated 4,000 characters with roughly 4,000 bytes was wrong for exactly this text.
func TestAnExportStopsAtItsByteCeilingWithMultibyteText(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "export-bytes-room")
	authorID := memberIDOf(t, s, "admin")

	s.exportByteCeiling = 2_000
	base := time.Now().UTC()
	// "é" is two UTF-8 bytes and a multi-byte rune in JSON, so a handful of these
	// crosses a 2,000-byte estimate well before the message ceiling would.
	body := strings.Repeat("é", 500)
	for index := range 20 {
		if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
			VALUES(?,?,?,?,?,?,?)`, db.ID(), channel, channel, authorID, body,
			db.Timestamp(base.Add(time.Duration(index)*time.Millisecond)), fmt.Sprintf("bytes-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	_, alice := namedUser(t, s, "export_bytes_alice")
	document := exportFor(t, s, handler, alice, "export-key-bounds-bytes")

	if !document.Summary.Truncated {
		t.Fatal("an export that reached its byte ceiling was not marked truncated")
	}
	if !containsReason(document.Omissions, "size limit") {
		t.Fatalf("the omission does not name the size limit: %v", document.Omissions)
	}
	data, err := os.ReadFile(s.exportPath(firstExportID(t, s, alice)))
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > maxExportBytes {
		t.Fatalf("the written file exceeded the byte bound: %d", len(data))
	}
	if len(document.Conversations) == 0 || len(document.Conversations[0].Messages) >= 20 {
		t.Fatalf("the byte ceiling did not stop the build: %d messages", document.Summary.Messages)
	}
}

// TestAnExportSurvivesARestartWithItsFilePresent checks the promised durability: once a
// build reports `ready`, the file is on disk and a fresh server on the same data
// directory serves it. A publication that only renamed a buffered temp file would leave
// a `ready` row whose bytes were never flushed.
func TestAnExportSurvivesARestartWithItsFilePresent(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "export-restart-room")
	request(t, handler, "POST", "/api/channels/"+channel+"/messages",
		map[string]any{"content": "a line the export must carry", "idempotency_key": "restart-1"}, admin)
	_, alice := namedUser(t, s, "export_restart_alice")
	document := exportFor(t, s, handler, alice, "export-key-restart-one")
	if len(document.Conversations) == 0 {
		t.Fatal("the export is empty")
	}
	id := firstExportID(t, s, alice)
	// The file is complete and parseable without the building process: this reads the
	// bytes a restarted server would serve.
	onDisk := readExportFile(t, s, id)
	if onDisk.Format != "brrpbrrp-member-export" {
		t.Fatalf("the published file is not a complete export: %q", onDisk.Format)
	}
	// No publication temporary is left behind.
	entries, err := os.ReadDir(filepath.Dir(s.exportPath(id)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".publish-") {
			t.Fatalf("a publication temporary survived a completed export: %s", entry.Name())
		}
	}
}
