package api

import (
	"fmt"
	"testing"

	"brrpbrrp/internal/db"
)

type replyPage struct {
	Parent   db.Message   `json:"parent"`
	Replies  []db.Message `json:"replies"`
	HasOlder bool         `json:"has_older"`
}

func TestReplyThreadsPaginationIsolationAndDeletion(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	user, member := ordinaryUser(t, s)
	var channel string
	if err := s.store.QueryRow(`SELECT id FROM channels WHERE type='text' LIMIT 1`).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	insert := func(replyTo string) string {
		t.Helper()
		id := db.ID()
		if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key,reply_to_id) VALUES(?,?,?,?,?,?,?,NULLIF(?,''))`, id, channel, channel, user, "thread content", db.Now(), id, replyTo); err != nil {
			t.Fatal(err)
		}
		return id
	}
	parent, unrelated := insert(""), insert("")
	ids := make([]string, 61)
	for i := range ids {
		ids[i] = insert(parent)
	}
	insert(unrelated)
	nested := insert(ids[0])
	path := "/api/messages/" + parent + "/replies"
	if w := request(t, handler, "GET", path, nil, nil); w.Code != 401 {
		t.Fatal("anonymous thread access allowed")
	}
	get := func(path string) replyPage {
		t.Helper()
		w := request(t, handler, "GET", path, nil, member)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		return readJSON[replyPage](t, w)
	}
	page := get(path + "?limit=999")
	if page.Parent.ID != parent || page.Parent.ReplyCount != 61 || len(page.Replies) != 50 || !page.HasOlder || page.Replies[0].ID != ids[11] || page.Replies[49].ID != ids[60] {
		t.Fatalf("invalid latest page: %+v", page)
	}
	page = get(path + "?before=" + page.Replies[0].ID)
	if len(page.Replies) != 11 || page.HasOlder || page.Replies[0].ID != ids[0] || page.Replies[10].ID != ids[10] {
		t.Fatal("earlier replies lost, duplicated or out of order")
	}
	if page.Replies[0].ReplyCount != 1 {
		t.Fatal("nested reply count missing")
	}
	child := get("/api/messages/" + ids[0] + "/replies")
	if len(child.Replies) != 1 || child.Replies[0].ID != nested {
		t.Fatal("nested discussion mixed with parent")
	}
	for _, cursor := range []string{"invalid", "%27%20OR%201=1"} {
		if w := request(t, handler, "GET", path+"?before="+cursor, nil, member); w.Code != 400 {
			t.Fatal("invalid reply cursor accepted")
		}
	}
	other := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "other", Type: "text", MaxVoiceUsers: 8}, admin))
	if w := request(t, handler, "POST", "/api/channels/"+other.ID+"/messages", messageInput{Content: "cross-channel", IdempotencyKey: "cross-channel-reply", ReplyToID: parent}, member); w.Code != 400 {
		t.Fatal("reply crossed channels")
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+ids[60], map[string]string{"confirm": ids[60]}, member); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got := get(path).Parent.ReplyCount; got != 60 {
		t.Fatalf("reply deletion count: %d", got)
	}
	if w := request(t, handler, "DELETE", "/api/messages/"+parent, map[string]string{"confirm": parent}, member); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, handler, "GET", path, nil, member); w.Code != 404 {
		t.Fatal("deleted parent thread remained accessible")
	}
	for i, id := range ids[:60] {
		message := readJSON[db.Message](t, request(t, handler, "GET", "/api/messages/"+id, nil, member))
		if message.ID != id || message.ReplyToID != "" {
			t.Fatal(fmt.Sprintf("reply %d damaged after deleting parent", i))
		}
	}
}
