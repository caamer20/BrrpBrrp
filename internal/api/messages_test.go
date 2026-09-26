package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func readJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatalf("invalid JSON: %s", w.Body.String())
	}
	return value
}

func ordinaryUser(t *testing.T, s *Server) (string, *http.Cookie) {
	t.Helper()
	id, now := db.ID(), db.Now()
	_, err := s.store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, id, "member", "unused-test-hash", now, now)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := s.newSession(w, id); err != nil {
		t.Fatal(err)
	}
	return id, w.Result().Cookies()[0]
}

func TestChatPermissionsIdempotencyAndSearch(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := ordinaryUser(t, s)
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
	path := "/api/channels/" + channelID + "/messages"
	input := messageInput{Content: "hello searchable world", IdempotencyKey: "test-message-1"}
	if w := request(t, handler, "POST", path, input, nil); w.Code != 401 {
		t.Fatal("anonymous send allowed")
	}
	messageResponse := request(t, handler, "POST", path, input, member)
	if messageResponse.Code != 201 {
		t.Fatal(messageResponse.Body.String())
	}
	message := readJSON[db.Message](t, messageResponse)
	duplicate := request(t, handler, "POST", path, input, member)
	if duplicate.Code != 200 || readJSON[db.Message](t, duplicate).ID != message.ID {
		t.Fatal("idempotency failed")
	}
	if w := request(t, handler, "POST", path, input, admin); w.Code != 201 {
		t.Fatal("message keys should be scoped to user")
	}
	if w := request(t, handler, "PATCH", "/api/messages/"+message.ID, map[string]string{"content": "edited"}, admin); w.Code != 403 {
		t.Fatal("admin edited another user's message")
	}
	changed := request(t, handler, "PATCH", "/api/messages/"+message.ID, map[string]string{"content": "revised moon"}, member)
	if changed.Code != 200 {
		t.Fatal(changed.Body.String())
	}
	for _, term := range []string{"moon", `" OR *`} {
		w := request(t, handler, "GET", "/api/search?q="+url.QueryEscape(term), nil, member)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if term == "moon" && len(readJSON[[]db.Message](t, w)) != 1 {
			t.Fatal("search did not reflect edit")
		}
	}
	for _, want := range []int{1, 0} {
		w := request(t, handler, "POST", "/api/messages/"+message.ID+"/reactions", map[string]string{"emoji": "👍"}, member)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if len(readJSON[[]db.Reaction](t, w)) != want {
			t.Fatal("reaction toggle failed")
		}
	}
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "nope", Type: "text", MaxVoiceUsers: 8}, member); w.Code != 403 {
		t.Fatal("normal user created channel")
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+message.ID, map[string]string{"confirm": "wrong"}, admin); w.Code != 400 {
		t.Fatal("deletion missing confirmation")
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+message.ID, map[string]string{"confirm": message.ID}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", "/api/search?q=moon", nil, member); len(readJSON[[]db.Message](t, w)) != 0 {
		t.Fatal("deleted message remained in search")
	}
	if w := request(t, handler, "POST", path, messageInput{Content: strings.Repeat("x", 4001), IdempotencyKey: "too-long-message"}, member); w.Code != 400 {
		t.Fatal("long message accepted")
	}
}

func sendFile(t *testing.T, handler http.Handler, cookie *http.Cookie, name, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "https://localhost:8080")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestUploadsOwnershipAndLimit(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := ordinaryUser(t, s)
	s.settings.MaxUploadBytes = 1024
	uploaded := sendFile(t, handler, member, "unsafe.html", "<html><script>alert(1)</script></html>")
	if uploaded.Code != 201 {
		t.Fatal(uploaded.Body.String())
	}
	attachment := readJSON[db.Attachment](t, uploaded)
	if w := request(t, handler, "GET", "/api/uploads/"+attachment.ID, nil, admin); w.Code != 404 {
		t.Fatal("another user's draft upload was readable")
	}
	if w := request(t, handler, "GET", "/api/uploads/"+attachment.ID, nil, member); !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("HTML served inline")
	}
	if w := sendFile(t, handler, member, "large.bin", strings.Repeat("x", 1025)); w.Code != 413 {
		t.Fatal("oversize upload accepted")
	}
	files, err := os.ReadDir(s.config.UploadDir)
	if err != nil || len(files) != 1 {
		t.Fatal("failed upload left a file behind")
	}
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		if channel.Type == "text" {
			input := messageInput{IdempotencyKey: "stolen-attachment", AttachmentIDs: []string{attachment.ID}}
			if w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages", input, admin); w.Code != 400 {
				t.Fatal("stolen attachment accepted")
			}
			if w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages", input, member); w.Code != 201 {
				t.Fatal(w.Body.String())
			}
			if w := request(t, handler, "GET", "/api/uploads/"+attachment.ID, nil, admin); w.Code != 200 {
				t.Fatal("attached upload is inaccessible")
			}
		}
	}
}
