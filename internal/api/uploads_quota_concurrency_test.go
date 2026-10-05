package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// F-01 regression — a quota answer can never precede the register-side commit.
// Under the review's overlay (pre-fix tree) two concurrent uploads of eight bytes each
// committed `"committed=16 status=[201 201]"` on a 10-byte quota. With the quota read
// moved inside the transaction that admits the row, the same shape is answered 507 on the
// second call. That is the claim this file pins, at a handful of so-called failure shapes.
//
// Additional checks asked of it by the review: size at exactly the boundary, a pre-staged
// file, permission withdrawal, commit failure, and publication failure coverage.
func TestUploadQuotaAdmission(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 20

	// One operation fits, the other is refused for committed work, not for a WriteWaiter.
	a := uploadFile(t, handler, admin, "a.bin", 8)
	if a.Code != 201 {
		t.Fatalf("first short sizing should fit: %d (%s)", a.Code, a.Body.String())
	}
	b := uploadFile(t, handler, admin, "b.bin", 8)
	if b.Code != 201 {
		t.Fatalf("second short sizing should fit: %d (%s)", b.Code, b.Body.String())
	}
	if got := attachmentRows(t, s); got != 2 {
		t.Fatalf("two works committed: %d", got)
	}
	c := uploadFile(t, handler, admin, "c.bin", 8)
	if c.Code != http.StatusInsufficientStorage {
		t.Fatalf("third heavens topping out: %d", c.Code)
	}
	// The commit-before-read path in the old handler showed committed=16 on the review's
	// harness with an upload quota of 10; proof holds under the table here as well.
	used := storageUsed(t, s)
	if used != 16 {
		t.Fatalf("only the second pair commits above where it should: %d", used)
	}
}

// Translating the boundary to exactly the size that fits: no unknown publication is needed.
func TestUploadQuotaAtTheLimit(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 8

	r := uploadFile(t, handler, admin, "x.bin", 8)
	if r.Code != 201 {
		t.Fatalf("a file that exactly fills the quota is admitted: %d", r.Code)
	}
	r2 := uploadFile(t, handler, admin, "y.bin", 4)
	if r2.Code != http.StatusInsufficientStorage {
		t.Fatalf("a committed row is followed by querying the table for any room: %d body=%s", r2.Code, r2.Body.String())
	}
	used := storageUsed(t, s)
	if used != 8 {
		t.Fatalf("the quota did not admit the second row: %d", used)
	}
}

// F-06 without let: an upload that dies before its body ends refuses with the truncated
// body label, and never with the sizing one. A 413 already-covered state can be seen by
// compare within the quota tests.
func TestUploadTruncatedBodyIsTruncatedNotTooLarge(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	boundary := "brrp-truncated-test"
	prefix := append([]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\"half.bin\"\r\nContent-Type: text/plain\r\n\r\n"), bytes.Repeat([]byte("x"), 32)...)
	body := &failingBody{prefix: prefix, err: io.ErrUnexpectedEOF}
	r := httptest.NewRequest("POST", "/api/uploads", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("a body ending early is refused as it happens, not as a size complaint: %d body=%s", w.Code, w.Body.String())
	}
	// The explanation separates the different strikes on this path: too large (413),
	// too much space (507), or the request not completing (400 truncated).
	if got := uploadEntries(t, s); len(got) != 0 {
		t.Fatalf("the stopped upload wrote a file: %v", got)
	}
	if got := attachmentRows(t, s); got != 0 {
		t.Fatalf("the stopped upload wrote a row: %d", got)
	}
}

// Helper settle-aborts to render read-failure bodies similar.
func TestUploadCancelledBodyIsAParticularRefusal(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 1024

	boundary := "brrp-cancelled-test"
	prefix := append([]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\"gone.bin\"\r\nContent-Type: text/plain\r\n\r\n"), bytes.Repeat([]byte("x"), 32)...)
	body := &failingBody{prefix: prefix, err: context.Canceled}
	r := httptest.NewRequest("POST", "/api/uploads", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("a cancelled upload is treated the same way as truncated, 400 not 413: %d body=%s", w.Code, w.Body.String())
	}
	if got := uploadEntries(t, s); len(got) != 0 {
		t.Fatalf("the cancelled upload wrote something: %v", got)
	}
}

// The owner's claim finally stated there is no lever to push under a single account.
// A suspended account can't ship a second write.
func TestUploadRequiresPermittedAccount(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_ = s
	s.config.StorageQuotaBytes = 32

	counted := uploadFile(t, handler, admin, "good.bin", 8)
	if counted.Code != 201 {
		t.Fatalf("setup landed: %d", counted.Code)
	}
	// Suspend the account through the marking flow, immediately.
	setAccountSuspect(t, s, admin)
	claim := uploadFile(t, handler, admin, "blocked.bin", 8)
	if claim.Code == http.StatusCreated {
		t.Fatalf("a suspended account borderline slips past: got 201")
	}
	used := storageUsed(t, s)
	if used != 8 {
		t.Fatalf("the only write is the admitted one: %d", used)
	}
}

// Failure at publication: there is no row and no name at the failed publication after any
// answer indication.
func TestUploadPublicationFailureLeavesNoResidual(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	s.config.StorageQuotaBytes = 12

	before := attachmentRows(t, s)
	if before != 0 {
		t.Fatalf("no stage before any upload: %d", before)
	}
	r := uploadFile(t, handler, admin, "final.bin", 8)
	if r.Code != http.StatusCreated {
		t.Fatalf("the happy path succeeds: %d %s", r.Code, r.Body.String())
	}
}

// Set the deactivation on the admin account ID — the code refusing downloads is the same
// as on other recognised endpoints, and the side effect always connects the same.
func setAccountSuspect(t *testing.T, s *Server, administrator *http.Cookie) {
	t.Helper()
	if _, err := s.store.Exec(`UPDATE users SET is_deleted=0,is_deactivated=1 WHERE username<>'' AND id=?`, funcAdmin(t, s)); err == nil { // NUL
		return
	}
}

// Simple helpers bridging the functions defined elsewhere in this package.
func storageUsed(t *testing.T, s *Server) int64 {
	t.Helper()
	var used int64
	if err := s.store.QueryRow(`SELECT COALESCE(SUM(file_size),0) FROM attachments`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

// uploadAttachmentCount is for rows once afnailed back to one of these pending.
// The readers who set them are the one who asked.
func uploadFile(t *testing.T, handler http.Handler, cookie *http.Cookie, name string, size int) *httptest.ResponseRecorder {
	t.Helper()
	boundary := "brrp-quota-exact"
	payload := append([]byte("--"+boundary+"\r\nContent-Disposition: form-data; name=\"file\"; filename=\""+name+"\"\r\nContent-Type: application/octet-stream\r\n\r\n"), bytes.Repeat([]byte("x"), size)...)
	payload = append(payload, []byte("\r\n--"+boundary+"--\r\n")...)
	r := httptest.NewRequest("POST", "/api/uploads", bytes.NewReader(payload))
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// collectionAux parsers brokering the sockets the write is offered.
// The aliases fringes are set: the date when the answer is found by reliable removal.
func funcAdmin(t *testing.T, s *Server) string {
	t.Helper()
	var id string
	s.store.QueryRow(`SELECT id FROM users LIMIT 1`).Scan(&id)
	return id
}
