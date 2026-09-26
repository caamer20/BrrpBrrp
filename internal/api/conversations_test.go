package api

import (
	"fmt"
	"testing"

	"brrpbrrp/internal/db"
)

func TestReplyContextPinsAndDeletion(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	user, member := ordinaryUser(t, s)
	var channel string
	s.store.QueryRow(`SELECT id FROM channels WHERE type='text' LIMIT 1`).Scan(&channel)
	ids := make([]string, 61)
	for i := range ids {
		ids[i] = db.ID()
		if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?,?)`, ids[i], channel, channel, user, fmt.Sprintf("message %d", i), db.Now(), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/channels/" + channel + "/messages"
	w := request(t, handler, "POST", path, messageInput{Content: "reply", IdempotencyKey: "reply-key", ReplyToID: ids[30]}, member)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	reply := readJSON[db.Message](t, w)
	if reply.ReplyToID != ids[30] {
		t.Fatal("reply was not stored")
	}
	w = request(t, handler, "GET", "/api/messages/"+ids[30]+"/context", nil, member)
	context := readJSON[[]db.Message](t, w)
	if len(context) != 51 || context[25].ID != ids[30] || context[0].ID != ids[5] || context[50].ID != ids[55] {
		t.Fatal("context is incomplete or not chronological")
	}
	pinPath := "/api/messages/" + ids[30] + "/pin"
	if w := request(t, handler, "PUT", pinPath, map[string]bool{"pinned": true}, member); w.Code != 403 {
		t.Fatal("member pinned a message")
	}
	if w := request(t, handler, "PUT", pinPath, map[string]bool{"pinned": true}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	pins := readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channel+"/pins", nil, member))
	if len(pins) != 1 || !pins[0].Pinned {
		t.Fatal("pin not listed")
	}
	if w := request(t, handler, "POST", path, messageInput{Content: "invalid", IdempotencyKey: "missing-reply", ReplyToID: db.ID()}, member); w.Code != 400 {
		t.Fatal("missing reply target accepted")
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+ids[30], map[string]string{"confirm": ids[30]}, admin); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	loaded := readJSON[db.Message](t, request(t, handler, "GET", "/api/messages/"+reply.ID, nil, member))
	if loaded.ReplyToID != "" || loaded.Content != "reply" {
		t.Fatal("deleting original damaged reply")
	}
	pins = readJSON[[]db.Message](t, request(t, handler, "GET", "/api/channels/"+channel+"/pins", nil, member))
	if len(pins) != 0 {
		t.Fatal("deleted message remained pinned")
	}
}
