package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

// pausedDownloadWriter flushes one chunk and blocks before the next read. This
// prevents fast loopback sockets from buffering the whole file before revocation.
// It never checks authorization: the production reader must stop the revoked copy.
type pausedDownloadWriter struct {
	http.ResponseWriter
	paused, release chan struct{}
	once            sync.Once
}

func (w *pausedDownloadWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.once.Do(func() {
		w.ResponseWriter.(http.Flusher).Flush()
		close(w.paused)
		<-w.release
	})
	return n, err
}

func (w *pausedDownloadWriter) Flush() { w.ResponseWriter.(http.Flusher).Flush() }

// startDownload returns an admitted, paused download and an explicit resume
// function. Cleanup always releases the writer, including on failed assertions.
func startDownload(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie, attachmentID string) (*http.Response, int64, func()) {
	t.Helper()
	paused, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(&pausedDownloadWriter{ResponseWriter: w, paused: paused, release: release}, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(resume)
	download, err := http.NewRequest(http.MethodGet, server.URL+"/api/uploads/"+attachmentID, nil)
	if err != nil {
		t.Fatal(err)
	}
	download.Header.Set("Origin", s.config.BaseURL)
	download.AddCookie(cookie)
	response, err := (&http.Client{Timeout: time.Minute}).Do(download)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", response.StatusCode)
	}
	head := make([]byte, 8<<10)
	if _, err := io.ReadFull(response.Body, head); err != nil {
		t.Fatalf("first read: %v", err)
	}
	select {
	case <-paused:
	case <-time.After(time.Minute):
		t.Fatal("download did not reach its pause boundary")
	}
	return response, int64(len(head)), resume
}

// drain reads a download to its end and reports how many bytes arrived.
func drain(t *testing.T, response *http.Response, already int64) int64 {
	t.Helper()
	delivered := already
	buffer := make([]byte, 32<<10)
	for {
		n, err := response.Body.Read(buffer)
		delivered += int64(n)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("download failed without a clean or truncated end: %v", err)
			}
			return delivered
		}
	}
}

// sharedArchive installs a file both accounts can read, in a channel they both may
// open, and returns its attachment id and total size.
func sharedArchive(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie) (string, int64) {
	t.Helper()
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
		}
	}
	if channelID == "" {
		t.Fatal("no text channel exists")
	}
	content := strings.Repeat("shared-bytes", 128*1024)
	attachment := readJSON[db.Attachment](t, sendFile(t, handler, admin, "shared.bin", content))
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channelID+"/messages",
		messageInput{Content: "the archive", IdempotencyKey: "boundary-file", AttachmentIDs: []string{attachment.ID}}, admin))
	if message.ID == "" {
		t.Fatal("message was not stored")
	}
	return attachment.ID, attachment.FileSize
}

// TestARevokedSessionEndsItsOwnStreamsAndKeepsTheOthers is the identity half of
// T-AUTH-02: a stream admitted by one session must stop when that session is revoked,
// and the account's other session must keep receiving the file it was already
// admitted for.
func TestARevokedSessionEndsItsOwnStreamsAndKeepsTheOthers(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	second := httptest.NewRecorder()
	if err := s.newSession(second, insiderID); err != nil {
		t.Fatal(err)
	}
	other := second.Result().Cookies()[0]
	attachmentID, size := sharedArchive(t, s, handler, admin)

	revoked, revokedHead, resumeRevoked := startDownload(t, s, handler, insider, attachmentID)
	surviving, survivingHead, resumeSurviving := startDownload(t, s, handler, other, attachmentID)

	// The account revokes one of its own devices while both responses are open.
	victim := currentSession(t, handler, insider)
	if w := request(t, handler, "DELETE", "/api/me/sessions/"+victim.ID, nil, other); w.Code != 200 {
		t.Fatalf("revoke one session: %d %s", w.Code, w.Body.String())
	}
	resumeRevoked()
	resumeSurviving()
	if delivered := drain(t, revoked, revokedHead); delivered >= size {
		t.Fatalf("the revoked session received the whole file: %d of %d bytes", delivered, size)
	}
	if delivered := drain(t, surviving, survivingHead); delivered != size {
		t.Fatalf("the surviving session received %d of %d bytes", delivered, size)
	}
}

// TestAChangedPasswordEndsEveryStreamThatCredentialAdmitted is the account-wide half
// of T-AUTH-02, and its control: a password change ends the streams the old
// credential admitted while an unrelated account's download finishes untouched.
func TestAChangedPasswordEndsEveryStreamThatCredentialAdmitted(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := memberAccount(t, s, "member")
	attachmentID, size := sharedArchive(t, s, handler, admin)

	adminStream, adminHead, resumeAdminStream := startDownload(t, s, handler, admin, attachmentID)
	memberStream, memberHead, resumeMemberStream := startDownload(t, s, handler, member, attachmentID)

	changed := request(t, handler, "POST", "/api/me/password",
		map[string]string{"current_password": "correct horse battery", "new_password": "a different long passphrase"}, admin)
	if changed.Code != 200 {
		t.Fatalf("password change: %d %s", changed.Code, changed.Body.String())
	}
	resumeAdminStream()
	resumeMemberStream()
	if delivered := drain(t, adminStream, adminHead); delivered >= size {
		t.Fatalf("a stream admitted by the changed password received the whole file: %d of %d bytes", delivered, size)
	}
	if delivered := drain(t, memberStream, memberHead); delivered != size {
		t.Fatalf("an unrelated account's download stopped at %d of %d bytes", delivered, size)
	}
}

// TestAnOperatorPasswordResetEndsTheStreamsTheOldCredentialAdmitted is the
// operator half of T-AUTH-02, and the reproduced half of R2-03. A chosen-password
// reset deleted the sessions and disconnected the sockets, but a response that had
// already been admitted for the old credential kept streaming: the probe received
// all 7,471,104 bytes after the reset returned success.
func TestAnOperatorPasswordResetEndsTheStreamsTheOldCredentialAdmitted(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enrollMFA(t, handler, admin, "correct horse battery")
	if w := request(t, handler, "POST", "/api/owner", map[string]any{"username": "admin", "confirm": "admin"}, admin); w.Code != http.StatusOK {
		t.Fatalf("ownership ceremony: %d %s", w.Code, w.Body.String())
	}
	targetID, target := memberAccount(t, s, "target")
	_, bystander := memberAccount(t, s, "bystander")
	attachmentID, size := sharedArchive(t, s, handler, admin)

	targetStream, targetHead, resumeTargetStream := startDownload(t, s, handler, target, attachmentID)
	bystanderStream, bystanderHead, resumeBystanderStream := startDownload(t, s, handler, bystander, attachmentID)

	reset := request(t, handler, "POST", "/api/admin/users/"+targetID+"/reset-password",
		map[string]any{"new_password": "a replacement passphrase", "confirm": "target"}, admin)
	if reset.Code != http.StatusOK {
		t.Fatalf("operator password reset: %d %s", reset.Code, reset.Body.String())
	}
	resumeTargetStream()
	resumeBystanderStream()
	if delivered := drain(t, targetStream, targetHead); delivered >= size {
		t.Fatalf("the reset left its admitted download running: %d of %d bytes", delivered, size)
	}
	if delivered := drain(t, bystanderStream, bystanderHead); delivered != size {
		t.Fatalf("an unrelated account's download stopped at %d of %d bytes", delivered, size)
	}
	// The session the old credential admitted cannot resume it either.
	if w := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, target); w.Code != http.StatusUnauthorized {
		t.Fatalf("the reset account's old session still reads files: %d %s", w.Code, w.Body.String())
	}
	var audits int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='user.password_reset' AND target_id=?`, targetID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("operator password reset audit records = %d, want 1", audits)
	}
	// The chosen password is the new credential, and it signs in normally.
	signedIn := request(t, handler, "POST", "/api/login", map[string]string{"username": "target", "password": "a replacement passphrase"}, nil)
	if signedIn.Code != http.StatusOK {
		t.Fatalf("the reset password does not sign in: %d %s", signedIn.Code, signedIn.Body.String())
	}
}

// TestAnEventQueuedBeforeARefusalIsNotWritten is the queue half of the same
// boundary. The sockets carry raw events with no second authorization decision, so
// the generation an event was queued under is what the writer compares: after a
// revocation the restricted event is stale and the refetch signal is not.
func TestAnEventQueuedBeforeARefusalIsNotWritten(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, _ := memberAccount(t, s, "insider")
	outsiderID, _ := memberAccount(t, s, "outsider")
	channel := privateTextChannel(t, handler, admin, "queued", insiderID)

	client := &ws.Client{ID: db.ID(), UserID: insiderID, SessionHash: activeSessionHash(t, s, insiderID), Out: make(chan ws.Queued, 8)}
	if !s.hub.Add(client) {
		t.Fatal("the hub refused the connection")
	}
	defer s.hub.Remove(client)
	bystander := &ws.Client{ID: db.ID(), UserID: outsiderID, SessionHash: activeSessionHash(t, s, outsiderID), Out: make(chan ws.Queued, 8)}
	if !s.hub.Add(bystander) {
		t.Fatal("the hub refused the connection")
	}
	defer s.hub.Remove(bystander)

	// A restricted event is queued for both connections, and the writer has already
	// taken the insider's copy out of the queue: this is the event that must not be
	// written after the access behind it is withdrawn.
	client.Send("channel.created", map[string]string{"id": channel.ID, "name": channel.Name})
	bystander.Send("presence.update", map[string]string{"state": "online"})
	restricted := <-client.Out
	bystanderEvent := <-bystander.Out
	if client.Stale(restricted) {
		t.Fatal("an event was stale before any authority changed")
	}
	if bystander.Stale(bystanderEvent) {
		t.Fatal("an unrelated connection's event was stale before any authority changed")
	}

	s.publishAuthorizationChange("Channel access changed.", insiderID)
	if !client.Stale(restricted) {
		t.Fatal("an event queued before the revocation was still writable")
	}
	// The refetch signal itself must arrive, and the connection that lost nothing
	// must not be invalidated.
	signal := <-client.Out
	if client.Stale(signal) {
		t.Fatal("the invalidation itself was stale")
	}
	if kind := eventKind(t, signal.Event); kind != "authorization.changed" {
		t.Fatalf("the connection was told %q instead of refetching", kind)
	}
	select {
	case queued := <-bystander.Out:
		if bystander.Stale(queued) {
			t.Fatal("an unrelated connection was invalidated by another account's revocation")
		}
	default:
	}
}

// TestAnAudienceResolvedBeforeARevocationIsRefusedAtDelivery is the first named R3-05
// interleaving at the API layer. An audience is a database answer about who may read a
// channel; the change commits between resolving it and queueing the event. The event
// must not be delivered to the connection that has already been told its authority
// moved, and an event authorized at the revision the change committed must be.
func TestAnAudienceResolvedBeforeARevocationIsRefusedAtDelivery(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, _ := memberAccount(t, s, "insider")
	channel := privateTextChannel(t, handler, admin, "queued", insiderID)
	adminID := sessionUserID(t, s, admin)

	// The audience is resolved while the insider may still read the channel.
	audience, revision, err := s.channelAudienceIDs(t.Context(), s.store, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revision <= 0 {
		t.Fatalf("audience revision = %d", revision)
	}
	insider := &ws.Client{ID: db.ID(), UserID: insiderID, SessionHash: activeSessionHash(t, s, insiderID), Out: make(chan ws.Queued, 8)}
	insider.ObserveRevision(revision)
	if !s.hub.Add(insider) {
		t.Fatal("the hub refused the insider's connection")
	}
	defer s.hub.Remove(insider)
	manager := &ws.Client{ID: db.ID(), UserID: adminID, SessionHash: activeSessionHash(t, s, adminID), Out: make(chan ws.Queued, 8)}
	manager.ObserveRevision(revision)
	if !s.hub.Add(manager) {
		t.Fatal("the hub refused the manager's connection")
	}
	defer s.hub.Remove(manager)

	// The grant is withdrawn: the change commits and reaches both connections.
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if s.authorizationRevision(t.Context()) <= revision {
		t.Fatalf("the revocation did not advance the authorization revision past %d", revision)
	}

	// The event that was authorized before the revocation is queued after it. The
	// insider's connection has moved to the new revision, so it is refused; the manager
	// still reads the channel and receives the permitted event published at the revision
	// the change committed.
	s.hub.BroadcastToRevision(audience, revision, "message.created", map[string]string{"id": "stale"})
	current := s.authorizationRevision(t.Context())
	s.hub.BroadcastToRevision([]string{adminID}, current, "message.created", map[string]string{"id": "permitted"})

	if delivered := eventsOf(t, insider); delivered["message.created"] {
		t.Fatal("a restricted event authorized before the revocation reached the revoked connection")
	}
	if !eventsOf(t, manager)["message.created"] {
		t.Fatal("the permitted event never reached the account that still reads the channel")
	}
}

// eventsOf drains a connection's queue and reports which event kinds arrived, so a test
// asserts on delivery rather than on a queue position.
func eventsOf(t *testing.T, client *ws.Client) map[string]bool {
	t.Helper()
	kinds := map[string]bool{}
	for {
		select {
		case queued := <-client.Out:
			if !client.Admitted(queued) {
				continue
			}
			kinds[eventKind(t, queued.Event)] = true
		default:
			return kinds
		}
	}
}

// TestADownloadRegisteredBeforeASessionRevocationIsRefusedAtAdmission is the third named
// R3-05 interleaving: a download can register after the session that admitted it was
// revoked, while still carrying the principal the middleware resolved. The barrier stands
// in for exactly that ordering — the response is registered, the account's session ends,
// and the first byte is still to be sent — and the control below proves the same
// download succeeds for a session that was not revoked.
func TestADownloadRegisteredBeforeASessionRevocationIsRefusedAtAdmission(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "member")
	second := httptest.NewRecorder()
	if err := s.newSession(second, memberID); err != nil {
		t.Fatal(err)
	}
	other := second.Result().Cookies()[0]
	attachmentID, size := sharedArchive(t, s, handler, admin)

	var once sync.Once
	hook := func() {
		once.Do(func() {
			victim := currentSession(t, handler, member)
			if w := request(t, handler, "DELETE", "/api/me/sessions/"+victim.ID, nil, member); w.Code != 200 {
				t.Fatalf("revoke one session: %d %s", w.Code, w.Body.String())
			}
		})
	}
	s.streamAdmission.Store(&hook)
	defer s.streamAdmission.Store(nil)

	// The revoked session's download is refused before any content byte is written.
	refused := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, member)
	if refused.Code != http.StatusUnauthorized {
		t.Fatalf("a download admitted under a revoked session answered %d: %s", refused.Code, refused.Body.String())
	}
	// The account's other session is untouched and receives the whole file.
	surviving := request(t, handler, "GET", "/api/uploads/"+attachmentID, nil, other)
	if surviving.Code != http.StatusOK || int64(surviving.Body.Len()) != size {
		t.Fatalf("the surviving session received %d of %d bytes with status %d", surviving.Body.Len(), size, surviving.Code)
	}
}

// eventKind reads the type out of an encoded event.
func eventKind(t *testing.T, encoded []byte) string {
	t.Helper()
	var event ws.Event
	if err := json.Unmarshal(encoded, &event); err != nil {
		t.Fatalf("event %q: %v", encoded, err)
	}
	return event.Type
}
