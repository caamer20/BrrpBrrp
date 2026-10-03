package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/limits"
)

// uploadBytes sends one file through the real multipart route and reports what the server
// said. The file is real: a fixture that inserted a row would not have gone through the
// guard this file is about.
func uploadBytes(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie, name string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/uploads", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Cookie", cookie.String())
	r.Header.Set("Origin", s.config.BaseURL)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// uploadFiles counts the files a data directory holds, so a refusal can be shown to have
// written nothing rather than merely to have answered with an error.
func uploadFiles(t *testing.T, s *Server) int {
	t.Helper()
	entries, err := os.ReadDir(s.config.UploadDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		t.Fatal(err)
	}
	return len(entries)
}

// The quota is the installation's own policy: how much attachment storage the operator is
// willing to hold. The control beside it is that a file which fits is accepted — a guard
// that refuses everything would pass a test that only asserted the refusal.
func TestAnUploadPastTheStorageQuotaIsRefusedWithAReason(t *testing.T) {
	t.Setenv("BRRP_STORAGE_QUOTA_BYTES", "10")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	first := uploadBytes(t, s, handler, admin, "fits.txt", []byte("12345678"))
	if first.Code != 201 && first.Code != 200 {
		t.Fatalf("a file inside the quota was refused: %d %s", first.Code, first.Body.String())
	}

	refused := uploadBytes(t, s, handler, admin, "over.txt", []byte("12345678"))
	if refused.Code != http.StatusInsufficientStorage {
		t.Fatalf("an upload past the quota answered %d, want 507: %s", refused.Code, refused.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(refused.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	message := payload["error"]
	if message == "" {
		message = refused.Body.String()
	}
	if !bytes.Contains([]byte(message), []byte("storage is full")) {
		t.Fatalf("the refusal does not say the storage is full: %q", message)
	}
	// And it wrote nothing: the stored bytes are the first file's eight.
	if files := uploadFiles(t, s); files != 1 {
		t.Fatalf("a refused upload wrote a file: %d files in the upload directory", files)
	}
}

// The reserve is a floor on free space rather than a policy about content: an installation
// that fills its volume cannot write the backup that would recover it, so the write is
// refused while there is still room to recover with.
func TestAnUploadThatWouldBreachTheDiskReserveIsRefused(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.freeSpace = func(string) (uint64, error) { return 1024, nil }
	s.config.StorageReserveBytes = 4096

	refused := uploadBytes(t, s, handler, admin, "reserve.txt", bytes.Repeat([]byte("x"), 64))
	if refused.Code != http.StatusInsufficientStorage {
		t.Fatalf("an upload inside the reserve answered %d, want 507: %s", refused.Code, refused.Body.String())
	}
	if !bytes.Contains(refused.Body.Bytes(), []byte("low on disk space")) {
		t.Fatalf("the refusal does not name the disk guard: %s", refused.Body.String())
	}
	if files := uploadFiles(t, s); files != 0 {
		t.Fatalf("a refused upload wrote a file: %d files", files)
	}

	// The permitted control: the same *size* of upload is accepted once the volume has
	// room, so the refusal above was about space rather than about the file. The bytes
	// differ from the refused attempt's because identical content is stored once: a
	// control that reused them would be answered from the digest table and would write no
	// file at all, which is what this test asserts.
	s.freeSpace = func(string) (uint64, error) { return 1 << 30, nil }
	accepted := uploadBytes(t, s, handler, admin, "reserve.txt", bytes.Repeat([]byte("y"), 64))
	if accepted.Code != 201 && accepted.Code != 200 {
		t.Fatalf("an upload with room was refused: %d %s", accepted.Code, accepted.Body.String())
	}
	if files := uploadFiles(t, s); files != 1 {
		t.Fatalf("the accepted upload did not write a file: %d files", files)
	}
}

// A guard that stops guarding is worse than one that refuses: if the storage figures
// cannot be read, the upload is refused and the fault is reported rather than the bound
// being silently skipped.
func TestAQuotaThatCannotBeReadRefusesRatherThanGuessing(t *testing.T) {
	t.Setenv("BRRP_STORAGE_QUOTA_BYTES", "10")
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	// A store that cannot answer is simulated by closing it, which is what a real failure
	// looks like from the handler's side.
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	refused := uploadBytes(t, s, handler, admin, "unreadable.txt", []byte("1234"))
	if refused.Code == 201 || refused.Code == 200 {
		t.Fatalf("an upload was accepted while the quota could not be read: %d", refused.Code)
	}
	if files := uploadFiles(t, s); files != 0 {
		t.Fatalf("a refused upload wrote a file: %d files", files)
	}
}

// The limits are single-sourced, so the refusal names the same number the check applied.
func TestTheAdvertisedLimitsAreTheEnforcedOnes(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	// The channel ceiling, reached through the route a person uses rather than by writing
	// rows: what is under test is the refusal the route produces. The installation arrives
	// with the channels setup created, so the loop fills to one below the ceiling and the
	// pair of assertions below is the boundary itself.
	create := func(name string) *httptest.ResponseRecorder {
		return request(t, handler, "POST", "/api/channels", db.Channel{
			Name: name, Type: "text", Topic: "a room", MaxVoiceUsers: 8,
		}, admin)
	}
	var existing int
	if err := s.store.QueryRow(`SELECT count(*) FROM channels`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing >= limits.Channels {
		t.Fatalf("the fixture already holds %d channels", existing)
	}
	for index := range limits.Channels - existing - 1 {
		if w := create(fmt.Sprintf("room-%d", index)); w.Code != 201 && w.Code != 200 {
			t.Fatalf("a channel inside the ceiling was refused: %d %s", w.Code, w.Body.String())
		}
	}
	// The permitted control: the last channel the ceiling allows is created.
	if w := create("the-last-allowed"); w.Code != 201 && w.Code != 200 {
		t.Fatalf("the last channel inside the ceiling was refused: %d %s", w.Code, w.Body.String())
	}
	refused := create("one-too-many")
	if refused.Code != 409 {
		t.Fatalf("a channel past the ceiling answered %d, want 409: %s", refused.Code, refused.Body.String())
	}
	if !bytes.Contains(refused.Body.Bytes(), []byte(fmt.Sprintf("%d-channel limit", limits.Channels))) {
		t.Fatalf("the refusal does not name the enforced limit: %s", refused.Body.String())
	}
}
