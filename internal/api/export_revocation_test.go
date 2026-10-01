package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// A produced member export is a protected response like any other, and the boundary
// under test here is the one review 17 reproduced: the file was admitted through a
// scope check and then registered as if it were an unclaimed upload, so a role
// capability withdrawn while the response was held after registration still received
// the whole snapshot while history answered 404. These are the permanent regressions
// for that boundary, with allowed and denied controls.

// exportIDFor finds one account's export by the operation key that built it, so a
// fixture with several accounts cannot pass by asserting on somebody else's file.
func exportIDFor(t *testing.T, s *Server, userID, operationKey string) string {
	t.Helper()
	var id string
	if err := s.store.QueryRow(`SELECT id FROM data_exports WHERE user_id=? AND operation_key=?`, userID, operationKey).Scan(&id); err != nil {
		t.Fatalf("export %q for %s: %v", operationKey, userID, err)
	}
	return id
}

// startExportDownload opens an export response and leaves it open, so a test can act
// while the server is still streaming.
func startExportDownload(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie, exportID string) (*http.Response, int64, int64) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	download, err := http.NewRequest(http.MethodGet, server.URL+"/api/me/export/"+exportID, nil)
	if err != nil {
		t.Fatal(err)
	}
	download.Header.Set("Origin", s.config.BaseURL)
	download.AddCookie(cookie)
	response, err := (&http.Client{}).Do(download)
	if err != nil {
		t.Fatalf("export download: %v", err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("export download status = %d", response.StatusCode)
	}
	stat, err := os.Stat(s.exportPath(exportID))
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 8<<10)
	if _, err := io.ReadFull(response.Body, head); err != nil {
		t.Fatalf("first read: %v", err)
	}
	return response, int64(len(head)), stat.Size()
}

// drainBytes reads a response to its end and reports how many bytes arrived.
func drainBytes(t *testing.T, response *http.Response, already int64) int64 {
	t.Helper()
	delivered := already
	buffer := make([]byte, 32<<10)
	for {
		n, err := response.Body.Read(buffer)
		delivered += int64(n)
		if err != nil {
			return delivered
		}
	}
}

// waitForNoStreams waits briefly for the registry to unregister a finished response.
// The client observes the end of the body when the server closes the connection, and
// the handler's deferred unregister runs around the same moment; the wait is for that
// ordering, not for a timeout to heal anything.
func waitForNoStreams(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.deliveries.snapshot()) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%d registered streams outlived their transfer", len(s.deliveries.snapshot()))
}

// seedExportContent writes enough messages that the export cannot fit in socket
// buffers before a revocation, so a mid-transfer boundary is actually exercised.
func seedExportContent(t *testing.T, s *Server, channelID, authorID string, count int) {
	t.Helper()
	line := strings.Repeat("export-line ", 300)
	base := time.Now().UTC()
	for index := range count {
		if _, err := s.store.Exec(`INSERT INTO messages(id,conversation_id,channel_id,user_id,content,created_at,idempotency_key)
			VALUES(?,?,?,?,?,?,?)`, db.ID(), channelID, channelID, authorID, line,
			db.Timestamp(base.Add(time.Duration(index)*time.Millisecond)), "seed-"+db.ID()); err != nil {
			t.Fatalf("seed row %d: %v", index, err)
		}
	}
}

// TestExportStopsWhenRoleCapabilityIsWithdrawnAfterRegistration is review 17, V17-02,
// as a permanent regression: the response is held after its registration, the
// reader's channel.read is withdrawn through the real role API, and the held response
// must not deliver the snapshot while a fresh history read already refuses.
func TestExportStopsWhenRoleCapabilityIsWithdrawnAfterRegistration(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	role, member := evidenceRole(t, s, handler, admin, "export_scope_member", "channel.read")
	secret := "the withdrawn snapshot's content"
	sent := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]string{"content": secret, "idempotency_key": "export-scope-secret"}, admin)
	if sent.Code != 201 {
		t.Fatalf("seed message: %d %s", sent.Code, sent.Body.String())
	}
	exportFor(t, s, handler, member, "export-scope-withdrawal")
	id := exportIDFor(t, s, memberIDOf(t, s, "export_scope_member_holder"), "export-scope-withdrawal")

	// The barrier stands in for the interval between registration and the first byte.
	var once sync.Once
	hook := func() {
		once.Do(func() { setRolePermissions(t, handler, admin, role, nil) })
	}
	s.streamAdmission.Store(&hook)
	defer s.streamAdmission.Store(nil)

	delivered := request(t, handler, "GET", "/api/me/export/"+id, nil, member)
	history := request(t, handler, "GET", "/api/channels/"+channelID+"/messages", nil, member)
	if history.Code == http.StatusOK {
		t.Fatalf("the withdrawal control failed: history still answers 200")
	}
	if delivered.Code == http.StatusOK {
		t.Fatalf("a withdrawn snapshot was still admitted: %d %s", delivered.Code, delivered.Body.String())
	}
	if strings.Contains(delivered.Body.String(), secret) {
		t.Fatal("the withdrawn snapshot's content was delivered")
	}
	if delivered.Code != http.StatusForbidden {
		t.Fatalf("refusal = %d, want 403: %s", delivered.Code, delivered.Body.String())
	}
	waitForNoStreams(t, s)
}

// TestExportDeliveryReauthorizesEveryRequestAndEveryByte is the allowed/denied pair
// for ordinary admission: with the capability intact the complete file downloads with
// the expected content, after a withdrawal the same request is refused without a
// single protected byte, and restoring the capability admits it again.
func TestExportDeliveryReauthorizesEveryRequestAndEveryByte(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	role, member := evidenceRole(t, s, handler, admin, "export_reauthorize", "channel.read")
	secret := "content the member may export"
	sent := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]string{"content": secret, "idempotency_key": "export-reauth-secret"}, admin)
	if sent.Code != 201 {
		t.Fatalf("seed message: %d %s", sent.Code, sent.Body.String())
	}
	exportFor(t, s, handler, member, "export-reauthorize")
	id := exportIDFor(t, s, memberIDOf(t, s, "export_reauthorize_holder"), "export-reauthorize")

	granted := request(t, handler, "GET", "/api/me/export/"+id, nil, member)
	if granted.Code != http.StatusOK {
		t.Fatalf("an intact grant could not download its export: %d %s", granted.Code, granted.Body.String())
	}
	if !strings.Contains(granted.Body.String(), secret) {
		t.Fatal("the permitted export contained none of the account's own content")
	}

	setRolePermissions(t, handler, admin, role, nil)
	withdrawn := request(t, handler, "GET", "/api/me/export/"+id, nil, member)
	if withdrawn.Code != http.StatusForbidden {
		t.Fatalf("a withdrawn export answered %d, want 403", withdrawn.Code)
	}
	if strings.Contains(withdrawn.Body.String(), secret) {
		t.Fatal("the refusal carried protected content")
	}

	setRolePermissions(t, handler, admin, role, []string{"channel.read"})
	restored := request(t, handler, "GET", "/api/me/export/"+id, nil, member)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), secret) {
		t.Fatalf("a restored grant could not download its export: %d %s", restored.Code, restored.Body.String())
	}
}

// TestExportRangeRequestsAreReauthorizedPerRequest: a resumed download is authorized
// like a first one — the bytes of an authorized range are exact, and a range request
// after a withdrawal is refused rather than served from a stale admission.
func TestExportRangeRequestsAreReauthorizedPerRequest(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	role, member := evidenceRole(t, s, handler, admin, "export_range", "channel.read")
	sent := request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		map[string]string{"content": "range content", "idempotency_key": "export-range-secret"}, admin)
	if sent.Code != 201 {
		t.Fatalf("seed message: %d %s", sent.Code, sent.Body.String())
	}
	exportFor(t, s, handler, member, "export-range")
	id := exportIDFor(t, s, memberIDOf(t, s, "export_range_holder"), "export-range")
	file, err := os.ReadFile(s.exportPath(id))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	ranged := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/me/export/"+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", s.config.BaseURL)
		req.Header.Set("Range", "bytes=0-99")
		req.AddCookie(member)
		response, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatalf("range request: %v", err)
		}
		return response
	}

	first := ranged()
	body, err := io.ReadAll(first.Body)
	first.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if first.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", first.StatusCode)
	}
	if string(body) != string(file[:100]) {
		t.Fatal("the range returned bytes from the wrong offset")
	}

	setRolePermissions(t, handler, admin, role, nil)
	denied := ranged()
	deniedBody, err := io.ReadAll(denied.Body)
	denied.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("a range request after withdrawal = %d, want 403", denied.StatusCode)
	}
	if strings.Contains(string(deniedBody), string(file[:100])) {
		t.Fatal("the refused range carried protected bytes")
	}
}

// TestOneWithdrawnConversationEndsTheWholeExportButNotOthers: an export is one
// document, so losing one recorded scope makes it ineligible in full — no half-file —
// while an unrelated account's export keeps working.
func TestOneWithdrawnConversationEndsTheWholeExportButNotOthers(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "export_multi_member")
	otherID, other := memberAccount(t, s, "export_multi_other")
	first := privateTextChannel(t, handler, admin, "export-scope-a", memberID)
	second := privateTextChannel(t, handler, admin, "export-scope-b", memberID)
	// The second channel is shared with the other account, so its export is an
	// unrelated authorized transfer that must keep working.
	if w := request(t, handler, "PUT", "/api/channels/"+second.ID+"/access",
		map[string]any{"visibility": "private", "user_ids": []string{memberID, otherID}}, admin); w.Code != 200 {
		t.Fatalf("share channel: %d %s", w.Code, w.Body.String())
	}
	firstSecret, secondSecret := "only in the first scope", "only in the second scope"
	if w := request(t, handler, "POST", "/api/channels/"+first.ID+"/messages",
		map[string]string{"content": firstSecret, "idempotency_key": "export-multi-a"}, admin); w.Code != 201 {
		t.Fatalf("first channel message: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/channels/"+second.ID+"/messages",
		map[string]string{"content": secondSecret, "idempotency_key": "export-multi-b"}, admin); w.Code != 201 {
		t.Fatalf("second channel message: %d %s", w.Code, w.Body.String())
	}
	exportFor(t, s, handler, member, "export-multi-member")
	exportFor(t, s, handler, other, "export-multi-other")
	memberExport := exportIDFor(t, s, memberID, "export-multi-member")
	otherExport := exportIDFor(t, s, otherID, "export-multi-other")

	// One scope is withdrawn through the real access API; the other stays readable.
	if w := request(t, handler, "PUT", "/api/channels/"+first.ID+"/access",
		map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("withdraw first scope: %d %s", w.Code, w.Body.String())
	}
	refused := request(t, handler, "GET", "/api/me/export/"+memberExport, nil, member)
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a partly withdrawn export answered %d, want 403", refused.Code)
	}
	if strings.Contains(refused.Body.String(), firstSecret) || strings.Contains(refused.Body.String(), secondSecret) {
		t.Fatal("the refused export leaked content from either scope")
	}

	// The unrelated account's export, whose only scope it still reads, is untouched.
	permitted := request(t, handler, "GET", "/api/me/export/"+otherExport, nil, other)
	if permitted.Code != http.StatusOK {
		t.Fatalf("an unrelated export answered %d: %s", permitted.Code, permitted.Body.String())
	}
	if !strings.Contains(permitted.Body.String(), secondSecret) {
		t.Fatal("the unrelated export lost its own content")
	}
	if strings.Contains(permitted.Body.String(), firstSecret) {
		t.Fatal("the unrelated export contained another scope's content")
	}
}

// TestAnExportStopsMidTransferWhenItsScopeIsWithdrawn: a response already streaming
// stops at the next read when the grant behind it is withdrawn, and it leaves no
// registered stream behind.
func TestAnExportStopsMidTransferWhenItsScopeIsWithdrawn(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "export_midtransfer")
	channel := privateTextChannel(t, handler, admin, "export-midtransfer", memberID)
	seedExportContent(t, s, channel.ID, memberIDOf(t, s, "admin"), 800)
	exportFor(t, s, handler, member, "export-midtransfer")
	id := exportIDFor(t, s, memberID, "export-midtransfer")

	response, head, size := startExportDownload(t, s, handler, member, id)
	if size <= head {
		t.Fatalf("fixture too small to be mid-transfer: %d bytes", size)
	}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access",
		map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("withdraw: %d %s", w.Code, w.Body.String())
	}
	if delivered := drainBytes(t, response, head); delivered >= size {
		t.Fatalf("the whole export was delivered after the withdrawal: %d of %d bytes", delivered, size)
	}
	waitForNoStreams(t, s)

	// And the account cannot start another export download either.
	again := request(t, handler, "GET", "/api/me/export/"+id, nil, member)
	if again.Code != http.StatusForbidden {
		t.Fatalf("a new export download after withdrawal = %d", again.Code)
	}
}

// TestAnExportStopsWhenItsSessionIsRevoked mid-transfer: the session identity is part
// of a registered response, so revoking one device ends the export that device
// admitted while the account's other session keeps working.
func TestAnExportStopsWhenItsSessionIsRevoked(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	channelID := textChannelID(t, s)
	memberID, member := memberAccount(t, s, "export_session_member")
	second := httptest.NewRecorder()
	if err := s.newSession(second, memberID); err != nil {
		t.Fatal(err)
	}
	other := second.Result().Cookies()[0]
	seedExportContent(t, s, channelID, memberIDOf(t, s, "admin"), 800)
	exportFor(t, s, handler, member, "export-session-review")
	id := exportIDFor(t, s, memberID, "export-session-review")

	revoked, revokedHead, size := startExportDownload(t, s, handler, member, id)
	surviving, survivingHead, _ := startExportDownload(t, s, handler, other, id)

	victim := currentSession(t, handler, member)
	if w := request(t, handler, "DELETE", "/api/me/sessions/"+victim.ID, nil, other); w.Code != 200 {
		t.Fatalf("revoke one session: %d %s", w.Code, w.Body.String())
	}
	if delivered := drainBytes(t, revoked, revokedHead); delivered >= size {
		t.Fatalf("the revoked session received the whole export: %d of %d bytes", delivered, size)
	}
	if delivered := drainBytes(t, surviving, survivingHead); delivered != size {
		t.Fatalf("the surviving session received %d of %d bytes", delivered, size)
	}
	waitForNoStreams(t, s)
}
