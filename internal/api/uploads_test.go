package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// These are the crash and failure boundaries §6.3 asks about for attachments: the
// file write, publication, the metadata insert and the reference from a message.
// A process cannot be killed halfway through an HTTP request from a test, so each
// test drives the same state a crash would leave behind and asserts what the
// server promises about it.

// failingBody is an HTTP request body that dies partway through, the way a client
// that loses its connection mid-upload does.
type failingBody struct {
	prefix []byte
	offset int
	err    error
}

func (b *failingBody) Read(p []byte) (int, error) {
	if b.offset >= len(b.prefix) {
		return 0, b.err
	}
	n := copy(p, b.prefix[b.offset:])
	b.offset += n
	return n, nil
}

func uploadRaw(t *testing.T, handler http.Handler, cookie *http.Cookie, contentType string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/uploads", body)
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func uploadEntries(t *testing.T, s *Server) []string {
	t.Helper()
	entries, err := os.ReadDir(s.config.UploadDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func attachmentRows(t *testing.T, s *Server) int {
	t.Helper()
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM attachments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// The file write and publication boundary: an upload that dies while the bytes
// are arriving must leave no final name, no partial file and no database row.
func TestAbortedUploadPublishesNothing(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	boundary := "brrp-test-boundary"
	prefix := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"note.txt\"\r\n" +
		"Content-Type: text/plain\r\n\r\nhalf an upload that will never finish"
	body := &failingBody{prefix: []byte(prefix), err: errors.New("connection reset by peer")}
	response := uploadRaw(t, handler, admin, "multipart/form-data; boundary="+boundary, body)
	if response.Code < 400 {
		t.Fatalf("aborted upload answered %d %s", response.Code, response.Body.String())
	}
	if rows := attachmentRows(t, s); rows != 0 {
		t.Fatalf("aborted upload stored %d attachment rows", rows)
	}
	if names := uploadEntries(t, s); len(names) != 0 {
		t.Fatalf("aborted upload left %v in the upload directory", names)
	}
}

// The metadata boundary: if the bytes cannot be published at all, no row may be
// created that would promise them.
func TestUploadThatCannotPublishStoresNoRow(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	if err := os.Chmod(s.config.UploadDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.config.UploadDir, 0700) })

	response := sendFile(t, handler, admin, "note.txt", "content")
	if response.Code == 201 {
		// Running as a user that ignores directory permissions (typically root),
		// so this boundary cannot be reproduced here.
		t.Skip("directory permissions are not enforced for this user")
	}
	if rows := attachmentRows(t, s); rows != 0 {
		t.Fatalf("failed upload stored %d attachment rows", rows)
	}
	if names := uploadEntries(t, s); len(names) != 0 {
		t.Fatalf("failed upload left %v in the upload directory", names)
	}
}

// The reference boundary: a downloaded reference whose file is missing is refused
// rather than answered with something else. The restore path validates every
// archived attachment for the same reason.
func TestReferenceWithoutItsFileIsNotFound(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	uploaded := readJSON[db.Attachment](t, sendFile(t, handler, admin, "note.txt", "content"))
	if err := os.Remove(filepath.Join(s.config.UploadDir, uploaded.ID)); err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, "GET", "/api/uploads/"+uploaded.ID, nil, admin)
	if response.Code != 404 {
		t.Fatalf("download without its file answered %d %s", response.Code, response.Body.String())
	}
	if rows := attachmentRows(t, s); rows != 1 {
		t.Fatalf("attachment rows = %d, want the row kept for inspection", rows)
	}
}

// A file published without a row is an orphan from an interrupted upload. It is
// unreadable immediately and swept after the documented grace period, while a
// claimed attachment and its file survive the same sweep.
func TestOrphanFileIsUnreadableUntilTheGracePeriodEnds(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	claimed := readJSON[db.Attachment](t, sendFile(t, handler, admin, "kept.txt", "claimed content"))
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "general", Type: "text", MaxVoiceUsers: 8}, admin))
	if w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages",
		messageInput{Content: "holds the attachment", IdempotencyKey: "orphan-sweep", AttachmentIDs: []string{claimed.ID}}, admin); w.Code != 201 {
		t.Fatalf("claim attachment: %d %s", w.Code, w.Body.String())
	}
	orphan := readJSON[db.Attachment](t, sendFile(t, handler, admin, "orphan.txt", "never claimed"))

	if response := request(t, handler, "GET", "/api/uploads/"+orphan.ID, nil, admin); response.Code != 200 {
		t.Fatalf("uploader cannot read their own unclaimed upload: %d", response.Code)
	}

	// A kill mid-publication leaves no row and, unlike a handled error, keeps the
	// temporary file, because the deferred cleanup never ran. It is the same kind
	// of residue as an orphan and the same sweep must take it.
	interrupted := filepath.Join(s.config.UploadDir, ".publish-interrupted")
	if err := os.WriteFile(interrupted, []byte("half a file"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := s.maintain(time.Now().Add(25 * time.Hour)); err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.config.UploadDir, orphan.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan file survived the sweep: %v", err)
	}
	if _, err := os.Stat(interrupted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file from an interrupted publication survived the sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.config.UploadDir, claimed.ID)); err != nil {
		t.Fatalf("claimed attachment file was swept: %v", err)
	}
	if response := request(t, handler, "GET", "/api/uploads/"+claimed.ID, nil, admin); response.Code != 200 {
		t.Fatalf("claimed attachment unreadable after the sweep: %d", response.Code)
	}
}
