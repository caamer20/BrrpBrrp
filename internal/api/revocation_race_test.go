package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

// privateTextChannel creates a restricted text channel granted to one account, which
// is the shape both races below need: readable by exactly one member.
func privateTextChannel(t *testing.T, handler http.Handler, admin *http.Cookie, name, userID string) db.Channel {
	t.Helper()
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: name, Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin))
	grant := map[string]any{"visibility": "private", "user_ids": []string{userID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	return channel
}

// TestRevokedAccessEndsAnInFlightDownload is the HTTP half of T-AUTH-02. Resolving a
// file's access when the request arrives is not enough: a slow client must not keep
// receiving a restricted file after the grant that admitted it is withdrawn.
func TestRevokedAccessEndsAnInFlightDownload(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	channel := privateTextChannel(t, handler, admin, "vault", insiderID)

	// A file large enough that the server cannot have written it into the socket
	// buffers before the revocation.
	content := strings.Repeat("private-bytes", 512*1024)
	attachment := readJSON[db.Attachment](t, sendFile(t, handler, admin, "archive.bin", content))
	message := readJSON[db.Message](t, request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages",
		messageInput{Content: "the archive", IdempotencyKey: "race-file", AttachmentIDs: []string{attachment.ID}}, admin))
	if message.ID == "" {
		t.Fatal("message was not stored")
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	download, err := http.NewRequest(http.MethodGet, server.URL+"/api/uploads/"+attachment.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	download.Header.Set("Origin", s.config.BaseURL)
	download.AddCookie(insider)
	response, err := (&http.Client{}).Do(download)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", response.StatusCode)
	}
	if response.ContentLength != attachment.FileSize {
		t.Fatalf("content length = %d, want %d", response.ContentLength, attachment.FileSize)
	}

	// Read a little, then withdraw the grant while the response is still open.
	head := make([]byte, 8<<10)
	if _, err := io.ReadFull(response.Body, head); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}

	delivered := int64(len(head))
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		delivered += int64(n)
		if readErr != nil {
			break
		}
	}
	if delivered >= attachment.FileSize {
		t.Fatalf("the whole file was delivered after the revocation: %d of %d bytes", delivered, attachment.FileSize)
	}
	if delivered > attachment.FileSize {
		t.Fatalf("delivered more than the file: %d of %d bytes", delivered, attachment.FileSize)
	}
	// The revoked account cannot start another download either.
	again, err := http.NewRequest(http.MethodGet, server.URL+"/api/uploads/"+attachment.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	again.AddCookie(insider)
	second, err := (&http.Client{}).Do(again)
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusNotFound {
		t.Fatalf("a new download after revocation = %d", second.StatusCode)
	}
}

// pushRecorder is a push transport that records where the worker sent a notice and
// blocks the first request so a backlog can be built deliberately instead of hoping
// for a race.
type pushRecorder struct {
	mu        sync.Mutex
	byHost    map[string]int
	started   chan struct{}
	release   chan struct{}
	blockOnce sync.Once
}

func newPushRecorder() *pushRecorder {
	return &pushRecorder{byHost: map[string]int{}, started: make(chan struct{}), release: make(chan struct{})}
}

func (r *pushRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.byHost[request.URL.Host]++
	r.mu.Unlock()
	r.blockOnce.Do(func() {
		close(r.started)
		<-r.release
	})
	return &http.Response{
		StatusCode: http.StatusCreated,
		Status:     "201 Created",
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    request,
	}, nil
}

func (r *pushRecorder) sent(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byHost[host]
}

// subscribeDevice inserts a device subscription directly. The API validates the
// endpoint against public DNS, which a test cannot reach; the subscription's keys are
// real, so dispatch encrypts a payload exactly as it would for a browser.
func subscribeDevice(t *testing.T, s *Server, userID, endpoint string) {
	t.Helper()
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	_, err = s.store.Exec(`INSERT INTO push_subscriptions(id,user_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?)`,
		db.ID(), userID, endpoint, base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth), db.Now())
	if err != nil {
		t.Fatal(err)
	}
}

// TestQueuedNotificationCannotOutliveAccess is the queue half of T-AUTH-02: a notice
// that was already eligible when it was queued must not be sent if the recipient's
// access was withdrawn while it waited.
func TestQueuedNotificationCannotOutliveAccess(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	revokedID, _ := memberAccount(t, s, "revoked")
	waitingID, _ := memberAccount(t, s, "waiting")
	text := privateTextChannel(t, handler, admin, "backroom", revokedID)
	grant := map[string]any{"visibility": "private", "user_ids": []string{revokedID}}

	subscribeDevice(t, s, revokedID, "https://revoked.example.invalid/device")
	subscribeDevice(t, s, waitingID, "https://waiting.example.invalid/device")

	recorder := newPushRecorder()
	s.pushHTTP = &http.Client{Transport: recorder}

	// Hold the worker on the first notice, so the next two wait in the backlog. The
	// hold is a deliverable notice for `waiting` in a channel it can read, because
	// the recorder counts a request before it blocks: holding on a `revoked` notice
	// would leave that account's counter non-zero for the wrong reason, and the leak
	// assertion below could not tell the hold from a real leak. A destination-less
	// notice would not hold the worker at all, since dispatch refuses one by design.
	general := textChannelID(t, s)
	s.pushJobs <- channelJob(waitingID, general, map[string]string{"body": "first"})
	select {
	case <-recorder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the push worker never dispatched the first notice")
	}
	s.pushJobs <- channelJob(revokedID, text.ID, map[string]string{"body": "restricted"})
	// The trailing notice is a deliverable one in an unrestricted channel: it proves
	// the worker drained past the restricted notice rather than stalling on it.
	s.pushJobs <- channelJob(waitingID, general, map[string]string{"body": "last"})

	// The grant is withdrawn while the notice sits in the backlog.
	if w := request(t, handler, "PUT", "/api/channels/"+text.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	close(recorder.release)

	// The trailing notice proves the backlog was drained past the restricted one. It
	// is the only notice addressed to `waiting`, so one delivery is the whole point:
	// the worker got past the notice it had to refuse.
	deadline := time.Now().Add(5 * time.Second)
	for recorder.sent("waiting.example.invalid") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the worker did not reach the trailing notice: %d", recorder.sent("waiting.example.invalid"))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sent := recorder.sent("revoked.example.invalid"); sent != 0 {
		t.Fatalf("a queued notice reached an account that lost access (%d requests)", sent)
	}

	// With the grant restored, the same queue does deliver, so the check above is
	// about access and not about a broken push path.
	if w := request(t, handler, "PUT", "/api/channels/"+text.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	s.pushJobs <- channelJob(revokedID, text.ID, map[string]string{"body": "allowed"})
	deadline = time.Now().Add(5 * time.Second)
	for recorder.sent("revoked.example.invalid") < 1 {
		if time.Now().After(deadline) {
			t.Fatal("an authorized notice was never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
