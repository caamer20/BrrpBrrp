package api

import (
	"reflect"
	"testing"

	"brrpbrrp/internal/db"
)

func TestMoveChannelPermissionsAndAtomicOrder(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := ordinaryUser(t, s)
	create := func(name, kind, category string) db.Channel {
		t.Helper()
		w := request(t, handler, "POST", "/api/channels", db.Channel{Name: name, Type: kind, Category: category, Topic: "preserve topic", MaxVoiceUsers: 8, Position: 99}, admin)
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
		return readJSON[db.Channel](t, w)
	}
	a := create("alpha", "text", "First")
	b := create("beta", "text", "Second")
	v := create("voice", "voice", "Voice")
	path := "/api/channels/" + a.ID + "/move"
	input := map[string]any{"target_id": b.ID, "after": true}
	for _, tc := range []struct {
		target string
		status int
	}{{a.ID, 400}, {v.ID, 400}, {"missing", 404}} {
		before, _ := s.channels()
		w := request(t, handler, "POST", path, map[string]any{"target_id": tc.target}, admin)
		if w.Code != tc.status {
			t.Fatalf("target %s: %d %s", tc.target, w.Code, w.Body.String())
		}
		after, _ := s.channels()
		if !reflect.DeepEqual(before, after) {
			t.Fatal("invalid move changed order")
		}
	}
	if w := request(t, handler, "POST", path, input, nil); w.Code != 401 {
		t.Fatal("anonymous reorder allowed")
	}
	if w := request(t, handler, "POST", path, input, member); w.Code != 403 {
		t.Fatal("member reorder allowed")
	}
	for _, after := range []bool{true, false} {
		input["after"] = after
		w := request(t, handler, "POST", path, input, admin)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		got := readJSON[[]db.Channel](t, w)
		persisted, err := s.channels()
		if err != nil || !reflect.DeepEqual(got, persisted) {
			t.Fatal("order not persisted", err)
		}
		ai, bi := -1, -1
		for i, c := range got {
			if c.Position != i {
				t.Fatal("noncontiguous positions")
			}
			if c.ID == a.ID {
				ai = i
				if c.Category != b.Category || c.Topic != a.Topic || c.Name != a.Name || c.Type != a.Type {
					t.Fatal("move changed channel settings")
				}
			}
			if c.ID == b.ID {
				bi = i
			}
		}
		if (after && ai != bi+1) || (!after && ai != bi-1) {
			t.Fatal("incorrect relative order")
		}
	}
}

func TestExpandedReactionPalette(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	channels, _ := s.channels()
	var id string
	for _, c := range channels {
		if c.Type == "text" {
			id = c.ID
			break
		}
	}
	w := request(t, handler, "POST", "/api/channels/"+id+"/messages", messageInput{Content: "Reactions", IdempotencyKey: "palette-test"}, admin)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	message := readJSON[db.Message](t, w)
	seen := map[string]bool{}
	for _, emoji := range reactionPalette {
		if seen[emoji] {
			t.Fatal("duplicate emoji", emoji)
		}
		seen[emoji] = true
		for _, want := range []int{1, 0} {
			w := request(t, handler, "POST", "/api/messages/"+message.ID+"/reactions", map[string]string{"emoji": emoji}, admin)
			if w.Code != 200 {
				t.Fatal(emoji, w.Body.String())
			}
			if len(readJSON[[]db.Reaction](t, w)) != want {
				t.Fatal("toggle failed", emoji)
			}
		}
	}
	if len(seen) < 50 {
		t.Fatal("expected expanded palette")
	}
	if w := request(t, handler, "POST", "/api/messages/"+message.ID+"/reactions", map[string]string{"emoji": "unsupported"}, admin); w.Code != 400 {
		t.Fatal("invalid reaction accepted")
	}
}
