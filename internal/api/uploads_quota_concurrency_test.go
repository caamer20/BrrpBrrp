package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// The storage-quota boundary (F-01) and the failure shapes around it.
//
// The race this file is named for: the upload handler used to read the committed
// attachment total *before* the transaction that admitted the new row. Two uploads
// arriving together could both read the same total, both decide they fitted, and both
// commit — the review reproduced `quota 10 exceeded: committed=16 status=[201 201]` on a
// ten-byte quota. The fix reads the total inside the transaction that also inserts the
// row. The store runs on a single connection, which is what makes that read meaningful,
// and it is also why the race cannot be produced here by chance: two handlers cannot
// interleave their transactions. TestConcurrentUploadsCannotBothCommitPastTheQuota holds
// both uploads at the seam between publication and admission and releases them together,
// so the interleaving the store otherwise serializes away is reached deliberately.

// TestConcurrentUploadsCannotBothCommitPastTheQuota is the F-01 regression. Two uploads
// of eight bytes each against a ten-byte quota: exactly one commits and the committed
// total never exceeds the quota. The two requests are held at the admission seam until
// both have published their bytes, then released at once.
func TestConcurrentUploadsCannotBothCommitPastTheQuota(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 10

	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	hook := func() {
		arrived <- struct{}{}
		<-release
	}
	s.uploadAdmission.Store(&hook)
	t.Cleanup(func() { s.uploadAdmission.Store(nil) })

	var (
		mu    sync.Mutex
		codes []int
	)
	var writers sync.WaitGroup
	for _, name := range []string{"first.bin", "second.bin"} {
		writers.Add(1)
		go func(name string) {
			defer writers.Done()
			response := uploadFile(t, handler, admin, name, 8)
			mu.Lock()
			codes = append(codes, response.Code)
			mu.Unlock()
		}(name)
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(15 * time.Second):
			t.Fatal("both uploads did not reach the admission seam")
		}
	}
	close(release)
	writers.Wait()

	if used := storageUsed(t, s); used > 10 {
		t.Fatalf("quota 10 exceeded: committed=%d status=%v", used, codes)
	}
	if used := storageUsed(t, s); used != 8 {
		t.Fatalf("exactly one eight-byte upload should have committed: committed=%d status=%v", used, codes)
	}
	if rows := attachmentRows(t, s); rows != 1 {
		t.Fatalf("one attachment row should exist: %d status=%v", rows, codes)
	}
	created := 0
	for _, code := range codes {
		if code == http.StatusCreated {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("exactly one upload should be created: status=%v", codes)
	}
}

// TestAnUploadPastTheQuotaIsRefusedWithAReason is the boundary with the control beside
// it: a file that exactly fills the quota is accepted, and one that does not is refused
// 507 while the committed total is unchanged.
func TestAnUploadPastTheQuotaIsRefusedWithAReason(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 8

	fits := uploadFile(t, handler, admin, "fits.bin", 8)
	if fits.Code != http.StatusCreated {
		t.Fatalf("a file that exactly fills the quota is admitted: %d %s", fits.Code, fits.Body.String())
	}
	over := uploadFile(t, handler, admin, "over.bin", 4)
	if over.Code != http.StatusInsufficientStorage {
		t.Fatalf("an upload past the quota is refused 507: %d %s", over.Code, over.Body.String())
	}
	if used := storageUsed(t, s); used != 8 {
		t.Fatalf("the refused upload changed the committed total: %d", used)
	}
	if rows := attachmentRows(t, s); rows != 1 {
		t.Fatalf("the refused upload left a row: %d", rows)
	}
}

// TestUploadTruncatedBodyIs400Not413 pins F-06: a body that ends before it is complete
// is a truncated request (400), never the too-large answer (413). The two are different
// faults — the client picked a too-large file versus the file never all arrived — and the
// old handler answered both with 413.
func TestUploadTruncatedBodyIs400Not413(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	boundary := "brrp-truncated-test"
	prefix := append(
		[]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\"half.bin\"\r\nContent-Type: text/plain\r\n\r\n"),
		bytes.Repeat([]byte("x"), 32)...)
	body := &failingBody{prefix: prefix, err: io.ErrUnexpectedEOF}
	r := httptest.NewRequest("POST", "/api/uploads", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("a body ending early is refused as a truncated request, not a size complaint: %d body=%s", w.Code, w.Body.String())
	}
	if got := uploadEntries(t, s); len(got) != 0 {
		t.Fatalf("the stopped upload wrote a file: %v", got)
	}
	if got := attachmentRows(t, s); got != 0 {
		t.Fatalf("the stopped upload wrote a row: %d", got)
	}
}

// TestUploadCancelledBodyIs400Not413 is the cancelled half of the same boundary: a
// request the client abandoned is a truncated request, and it writes nothing.
func TestUploadCancelledBodyIs400Not413(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 1024

	boundary := "brrp-cancelled-test"
	prefix := append(
		[]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\"gone.bin\"\r\nContent-Type: text/plain\r\n\r\n"),
		bytes.Repeat([]byte("x"), 32)...)
	body := &failingBody{prefix: prefix, err: context.Canceled}
	r := httptest.NewRequest("POST", "/api/uploads", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("a cancelled upload is refused as truncated: %d body=%s", w.Code, w.Body.String())
	}
	if got := uploadEntries(t, s); len(got) != 0 {
		t.Fatalf("the cancelled upload wrote a file: %v", got)
	}
	if got := attachmentRows(t, s); got != 0 {
		t.Fatalf("the cancelled upload wrote a row: %d", got)
	}
}

// TestADeactivatedAccountCannotUpload drives the product's own deactivation route: an
// administrator deactivates an uploader through PATCH /api/admin/users/{id}, and the
// next upload from that account is refused 401 — deactivation ends the session, so the
// answer is a sign-in rather than a permission error. The permitted control beside it is
// that the same upload succeeds before the deactivation.
func TestADeactivatedAccountCannotUpload(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 32

	const password = "a member passphrase for uploads"
	created := request(t, handler, "POST", "/api/admin/users", map[string]any{
		"username":     "uploader",
		"password":     password,
		"display_name": "Uploader",
	}, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("creating the member: %d %s", created.Code, created.Body.String())
	}
	memberID := readJSON[map[string]any](t, created)["id"].(string)
	login := request(t, handler, "POST", "/api/login", map[string]string{
		"username": "uploader",
		"password": password,
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("the member signs in: %d %s", login.Code, login.Body.String())
	}
	member := login.Result().Cookies()[0]

	control := uploadFile(t, handler, member, "before.bin", 8)
	if control.Code != http.StatusCreated {
		t.Fatalf("the permitted control upload should be accepted: %d %s", control.Code, control.Body.String())
	}

	deactivate := request(t, handler, "PATCH", "/api/admin/users/"+memberID, map[string]any{
		"is_deactivated": true,
		"confirm":        "uploader",
	}, admin)
	if deactivate.Code != http.StatusOK {
		t.Fatalf("deactivating through the product's route: %d %s", deactivate.Code, deactivate.Body.String())
	}

	refused := uploadFile(t, handler, member, "blocked.bin", 8)
	if refused.Code != http.StatusUnauthorized {
		t.Fatalf("a deactivated account's upload is refused 401: %d %s", refused.Code, refused.Body.String())
	}
	if used := storageUsed(t, s); used != 8 {
		t.Fatalf("only the permitted upload should be stored: %d", used)
	}
}

// TestUploadPublicationFailureLeavesNoResidual induces a publication failure — the upload
// directory is made read-only so the write publication needs cannot happen — and asserts
// the product's promise: a non-201 answer, no file in the upload directory, and no row.
// The control beside it is that the same upload is accepted once the directory is
// writable again; a product that refused every upload would pass the assertions above and
// fail here.
func TestUploadPublicationFailureLeavesNoResidual(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	if err := os.Chmod(s.config.UploadDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.config.UploadDir, 0o700) })

	failed := uploadFile(t, handler, admin, "final.bin", 8)
	if failed.Code == http.StatusCreated {
		// Directory permissions are not enforced for this user (typically root), so the
		// publication cannot be made to fail here.
		t.Skip("directory permissions are not enforced for this user")
	}
	if names := uploadEntries(t, s); len(names) != 0 {
		t.Fatalf("a failed publication left files behind: %v", names)
	}
	if rows := attachmentRows(t, s); rows != 0 {
		t.Fatalf("a failed publication left a row: %d", rows)
	}

	if err := os.Chmod(s.config.UploadDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writable := uploadFile(t, handler, admin, "final.bin", 8)
	if writable.Code != http.StatusCreated {
		t.Fatalf("the control upload should be accepted once the directory is writable: %d %s", writable.Code, writable.Body.String())
	}
	if rows := attachmentRows(t, s); rows != 1 {
		t.Fatalf("the control upload should leave one row: %d", rows)
	}
}

// storageUsed is the committed attachment total the quota is measured against.
func storageUsed(t *testing.T, s *Server) int64 {
	t.Helper()
	var used int64
	if err := s.store.QueryRow(`SELECT COALESCE(SUM(file_size),0) FROM attachments`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

// uploadFile sends one real multipart file through the upload route and reports what the
// server answered. The bytes are real: a fixture that inserted a row would not have gone
// through the guard these tests are about.
func uploadFile(t *testing.T, handler http.Handler, cookie *http.Cookie, name string, size int) *httptest.ResponseRecorder {
	t.Helper()
	boundary := "brrp-quota-exact"
	payload := append(
		[]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\""+name+"\"\r\nContent-Type: application/octet-stream\r\n\r\n"),
		bytes.Repeat([]byte("x"), size)...)
	payload = append(payload, []byte("\r\n--"+boundary+"--\r\n")...)
	r := httptest.NewRequest("POST", "/api/uploads", bytes.NewReader(payload))
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
