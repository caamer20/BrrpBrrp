package api

import (
	"strings"
	"testing"
)

func TestMentionNames(t *testing.T) {
	names := mentionNames("Hi @Alice @ALICE, x@ignored @everyone", false)
	if len(names) != 1 || !names["alice"] {
		t.Fatalf("mentions: %v", names)
	}
	if !mentionNames("@everyone", true)["everyone"] {
		t.Fatal("admin everyone mention missing")
	}
}

func TestMentionUnreadAndPrivateKeys(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	id, member := ordinaryUser(t, s)
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range channels {
		if channel.Type == "text" {
			w := request(t, handler, "POST", "/api/channels/"+channel.ID+"/messages", messageInput{Content: "hello @member", IdempotencyKey: "mention-test-1"}, admin)
			if w.Code != 201 {
				t.Fatal(w.Body.String())
			}
			unread, err := s.unread(id)
			if err != nil || len(unread) != 1 || unread[0].Mentions != 1 {
				t.Fatalf("unread: %v %v", unread, err)
			}
		}
	}
	state := request(t, handler, "GET", "/api/state", nil, member)
	if s.vapidPrivate == "" || s.vapidPublic == "" {
		t.Fatal("missing VAPID keys")
	}
	if strings.Contains(state.Body.String(), s.vapidPrivate) || !strings.Contains(state.Body.String(), s.vapidPublic) {
		t.Fatal("public state exposed the wrong VAPID key")
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM settings WHERE key='vapid'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("VAPID keys not persisted")
	}
}
