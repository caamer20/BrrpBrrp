package api

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

func privatePipeline(t *testing.T) (*Server, http.Handler, *http.Cookie, string, *http.Cookie, string, *http.Cookie, string) {
	t.Helper()
	s, h := testServer(t)
	s.config.PrivateConversations = true
	admin := setupAdmin(t, s, h)
	aid, a := memberAccount(t, s, "alice_dm")
	bid, b := memberAccount(t, s, "bob_dm")
	w := request(t, h, "POST", "/api/directs", map[string]string{"user_id": bid, "introduction": "Hello privately"}, a)
	if w.Code != 201 {
		t.Fatalf("request: %d %s", w.Code, w.Body.String())
	}
	d := readJSON[directConversation](t, w)
	w = request(t, h, "POST", "/api/directs/"+d.ID+"/accept", map[string]any{}, b)
	if w.Code != 200 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	return s, h, admin, aid, a, bid, b, d.ID
}

func directUpload(t *testing.T, h http.Handler, cookie *http.Cookie, conversation string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "private.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	path := "/api/uploads"
	if conversation != "" {
		path += "?conversation_id=" + conversation
	}
	r := httptest.NewRequest("POST", path, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Origin", "https://localhost:8080")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDirectMessagePipelinePreservesScopeAndBlocksOperatorBypass(t *testing.T) {
	s, h, admin, aid, a, bid, b, id := privatePipeline(t)
	upload := directUpload(t, h, a, id)
	if upload.Code != 201 {
		t.Fatal(upload.Body.String())
	}
	file := readJSON[db.Attachment](t, upload)
	send := messageInput{Content: "privateuniquewording", IdempotencyKey: "direct-original-key", AttachmentIDs: []string{file.ID}}
	path := "/api/conversations/" + id + "/messages"
	w := request(t, h, "POST", path, send, a)
	if w.Code != 201 {
		t.Fatalf("send: %d %s", w.Code, w.Body.String())
	}
	m := readJSON[db.Message](t, w)
	if m.ChannelID != "" || m.ConversationID != id {
		t.Fatalf("private message projected into channel: %+v", m)
	}
	if w := request(t, h, "POST", path, send, a); w.Code != 200 || readJSON[db.Message](t, w).ID != m.ID {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	reply := request(t, h, "POST", path, messageInput{Content: "reply privately", IdempotencyKey: "direct-reply-key", ReplyToID: m.ID}, b)
	if reply.Code != 201 {
		t.Fatal(reply.Body.String())
	}
	for _, route := range []string{path, "/api/messages/" + m.ID, "/api/messages/" + m.ID + "/replies", "/api/messages/" + m.ID + "/context", "/api/search?q=privateuniquewording&conversation_id=" + id, "/api/conversations/" + id + "/pins", "/api/uploads/" + file.ID, "/api/uploads/" + file.ID + "?preview=1"} {
		if w := request(t, h, "GET", route, nil, b); w.Code != 200 {
			t.Fatalf("member %s: %d %s", route, w.Code, w.Body.String())
		}
		if w := request(t, h, "GET", route, nil, admin); w.Code != 404 || strings.Contains(w.Body.String(), "privateuniquewording") {
			t.Fatalf("operator bypass %s: %d %s", route, w.Code, w.Body.String())
		}
	}
	// Global community search/state never includes private content or identifiers.
	for _, route := range []string{"/api/search?q=privateuniquewording", "/api/state", "/api/unread", "/api/directs"} {
		w := request(t, h, "GET", route, nil, admin)
		if w.Code != 200 || strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), m.ID) {
			t.Fatalf("private metadata in %s: %d %s", route, w.Code, w.Body.String())
		}
	}
	if w := request(t, h, "POST", "/api/messages/"+m.ID+"/report", map[string]string{"reason": "private evidence must not enter community queue"}, b); w.Code != 404 {
		t.Fatalf("DM leaked to reports: %d", w.Code)
	}
	// Private pin/deletion activity must not appear in the community audit feed.
	tx, err := s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAccountRole(t.Context(), tx, aid, "role-moderator"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "PUT", "/api/messages/"+m.ID+"/pin", map[string]bool{"pinned": true}, a); w.Code != 200 {
		t.Fatalf("private pin: %d %s", w.Code, w.Body.String())
	}
	var auditLeaks int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE target_id=?`, m.ID).Scan(&auditLeaks); err != nil || auditLeaks != 0 {
		t.Fatalf("private pin disclosed activity: %d %v", auditLeaks, err)
	}
	// A recipient cannot erase the other participant's content, even after acquiring admin authority.
	tx, err = s.store.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAccountRole(t.Context(), tx, bid, db.AdministratorRoleID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE users SET is_admin=1 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "DELETE", "/api/messages/"+m.ID, map[string]string{"confirm": m.ID}, b); w.Code != 403 {
		t.Fatalf("peer admin deleted other's DM: %d", w.Code)
	}
	// Channel history cannot claim private-scoped staged bytes or quote a private reply.
	staged := readJSON[db.Attachment](t, directUpload(t, h, a, id))
	channel := textChannelID(t, s)
	if w := request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "attempt", IdempotencyKey: "scope-attachment-key", AttachmentIDs: []string{staged.ID}}, a); w.Code != 400 {
		t.Fatalf("cross-scope attachment: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "attempt", IdempotencyKey: "scope-reply-key", ReplyToID: m.ID}, a); w.Code != 400 {
		t.Fatalf("cross-scope reply: %d", w.Code)
	}
	// Preview URLs inside DMs are never sent to an external scraping service.
	s.settingsMu.Lock()
	s.settings.LinkPreviews = true
	s.settingsMu.Unlock()
	s.queuePreview(db.Message{ID: m.ID, ConversationID: id, Content: "https://private.example/sensitive"})
	if len(s.previewJobs) != 0 {
		t.Fatal("private URL queued for external preview")
	}
	_ = aid
}

func TestDirectBlockCancelsWritesButRetainsHistoryAndOwnDeletion(t *testing.T) {
	s, h, _, aid, a, bid, b, id := privatePipeline(t)
	path := "/api/conversations/" + id + "/messages"
	m := readJSON[db.Message](t, request(t, h, "POST", path, messageInput{Content: "retained history", IdempotencyKey: "block-original-key"}, a))
	staged := readJSON[db.Attachment](t, directUpload(t, h, a, id))
	body := bodyThatRevokes(t, messageInput{Content: "late message", IdempotencyKey: "late-block-key", AttachmentIDs: []string{staged.ID}}, func() error {
		w := request(t, h, "PUT", "/api/me/blocks/"+aid, map[string]any{}, b)
		if w.Code != 200 {
			return fmt.Errorf("block: %d %s", w.Code, w.Body.String())
		}
		return nil
	})
	if w := pausedRequest(t, h, "POST", path, a, body); w.Code != 403 {
		t.Fatalf("write crossed block: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", path, nil, a); w.Code != 200 {
		t.Fatal("block removed accepted history")
	}
	if w := request(t, h, "PATCH", "/api/messages/"+m.ID, map[string]string{"content": "blocked edit"}, a); w.Code != 403 {
		t.Fatalf("edit while blocked: %d", w.Code)
	}
	if w := request(t, h, "POST", "/api/messages/"+m.ID+"/reactions", map[string]string{"emoji": "👍"}, a); w.Code != 403 {
		t.Fatalf("reaction while blocked: %d", w.Code)
	}
	if w := directUpload(t, h, a, id); w.Code != 403 {
		t.Fatalf("upload while blocked: %d", w.Code)
	}
	if w := request(t, h, "DELETE", "/api/messages/"+m.ID, map[string]string{"confirm": m.ID}, a); w.Code != 200 {
		t.Fatalf("own deletion under block: %d %s", w.Code, w.Body.String())
	}
	var auditLeaks int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE target_id=?`, m.ID).Scan(&auditLeaks); err != nil || auditLeaks != 0 {
		t.Fatalf("private deletion disclosed activity: %d %v", auditLeaks, err)
	}
	if w := request(t, h, "DELETE", "/api/me/blocks/"+aid, nil, b); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "POST", path, messageInput{Content: "retained history", IdempotencyKey: "block-original-key"}, a); w.Code != 409 {
		t.Fatalf("unblock resurrected erased send: %d", w.Code)
	}
	if w := request(t, h, "POST", path, messageInput{Content: "explicit new message", IdempotencyKey: "after-block-new-key"}, a); w.Code != 201 {
		t.Fatalf("new send after unblock: %d %s", w.Code, w.Body.String())
	}
	var claims int
	if err := s.store.QueryRow(`SELECT count(*) FROM attachments WHERE id=? AND message_id IS NOT NULL`, staged.ID).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("blocked send claimed file: %d %v", claims, err)
	}
	_ = bid
}

func TestDirectMessageEventsAndFeatureGateDoNotLeak(t *testing.T) {
	s, h, admin, aid, a, bid, b, id := privatePipeline(t)
	clients := []*ws.Client{}
	for _, entry := range []struct {
		id     string
		cookie *http.Cookie
	}{{aid, a}, {bid, b}, {sessionUserID(t, s, admin), admin}} {
		c := &ws.Client{ID: db.ID(), UserID: entry.id, SessionHash: hashToken(entry.cookie.Value), Out: make(chan ws.Queued, 16)}
		if _, err := s.registerConnection(withSession(t.Context(), c.SessionHash), db.User{ID: c.UserID}, c); err != nil {
			t.Fatal(err)
		}
		defer s.hub.Remove(c)
		clients = append(clients, c)
	}
	w := request(t, h, "POST", "/api/conversations/"+id+"/messages", messageInput{Content: "scoped event", IdempotencyKey: "direct-event-key"}, a)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	if len(clients[0].Out) != 1 || len(clients[1].Out) != 1 || len(clients[2].Out) != 0 {
		t.Fatalf("wrong event audience: %d %d %d", len(clients[0].Out), len(clients[1].Out), len(clients[2].Out))
	}
	s.config.PrivateConversations = false
	if w := request(t, h, "GET", "/api/conversations/"+id+"/messages", nil, a); w.Code != 404 {
		t.Fatalf("disabled gate disclosed history: %d", w.Code)
	}
	if w := request(t, h, "GET", "/api/state", nil, a); strings.Contains(w.Body.String(), id) {
		t.Fatal("disabled gate exposed descriptors")
	}
}
