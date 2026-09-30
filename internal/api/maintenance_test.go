package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// A reclaimed attachment row is only half of reclaiming an upload. Without this the
// database reports free space while the bytes stay on disk, and an operator has no
// way to tell why a server is full.
func TestMaintenanceReclaimsUploadFilesNotJustTheirRows(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channel := moderationChannel(t, s, handler, admin, "orphan-room")

	// A message with an attachment, whose file is live. The upload goes through the
	// real multipart route: a fixture that inserted a row would not have written a
	// file, and the whole point of this test is the file.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "kept.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("kept bytes")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	uploadRequest := httptest.NewRequest("POST", "/api/uploads", &body)
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRequest.Header.Set("Cookie", admin.String())
	// The origin guard applies to writes from a browser-shaped request, and a request
	// without one is refused; the header is set so this is the real route.
	uploadRequest.Header.Set("Origin", s.config.BaseURL)
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != 201 && uploadResponse.Code != 200 {
		t.Fatalf("upload: %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	var uploaded map[string]any
	if err := json.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	uploadID, _ := uploaded["id"].(string)
	keptPath := s.config.UploadDir + "/" + uploadID
	if err := os.WriteFile(keptPath, []byte("kept bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "POST", "/api/channels/"+channel+"/messages",
		map[string]any{"content": "with a file", "idempotency_key": "orphan-msg-1", "attachment_ids": []string{uploadID}},
		admin); w.Code != 201 {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}

	// A file whose row is reclaimed: an attachment that never got a message, aged out.
	orphanID := db.ID()
	orphanPath := s.config.UploadDir + "/" + orphanID
	if err := os.WriteFile(orphanPath, []byte("orphaned bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`INSERT INTO attachments(id,uploader_id,file_name,stored_name,file_size,mime_type,created_at)
		VALUES(?,?,?,?,?,?,?)`, orphanID, memberIDOf(t, s, "admin"), "orphan.txt", orphanID, 14, "text/plain",
		db.Timestamp(time.Now().Add(-48*time.Hour))); err != nil {
		t.Fatal(err)
	}
	// The file is aged too. The grace period protects a *young* unclaimed file — an
	// upload between the response and the message that claims it — so a fixture that
	// only aged the row would be protected by that same grace and prove nothing.
	aged := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphanPath, aged, aged); err != nil {
		t.Fatal(err)
	}

	if err := s.maintain(time.Now()); err != nil {
		t.Fatal(err)
	}
	// The row is gone — that part already worked.
	var rows int
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT count(*) FROM attachments WHERE id=?`, orphanID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("the orphan attachment row survived maintenance")
	}
	// And now the file is gone too, which is the part that did not.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("the reclaimed upload's file is still on disk: %v", err)
	}
	// And a file somebody is still using is untouched, however old its row.
	if _, err := os.Stat(keptPath); err != nil {
		t.Fatalf("a live attachment's file was removed: %v", err)
	}
}
