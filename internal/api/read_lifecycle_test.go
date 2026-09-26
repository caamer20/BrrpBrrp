package api

import (
	"fmt"
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
)

func TestReadCursorRejectsRevokedSessionAndKeepsMonotonicControl(t *testing.T) {
	s, h := testServer(t)
	admin := setupAdmin(t, s, h)
	id, member := memberAccount(t, s, "reader")
	channel := textChannelID(t, s)
	first := readJSON[db.Message](t, request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "first", IdempotencyKey: "read-first"}, admin))
	last := readJSON[db.Message](t, request(t, h, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: "last", IdempotencyKey: "read-last"}, admin))
	path := "/api/channels/" + channel + "/read"
	for _, messageID := range []string{last.ID, first.ID} {
		if w := request(t, h, "POST", path, map[string]string{"last_read_message_id": messageID}, member); w.Code != 200 {
			t.Fatalf("permitted mark: %d %s", w.Code, w.Body.String())
		}
	}
	var cursor string
	if err := s.store.QueryRow(`SELECT last_read_message_id FROM channel_reads WHERE user_id=? AND channel_id=?`, id, channel).Scan(&cursor); err != nil || cursor != last.ID {
		t.Fatalf("cursor regressed: %q %v", cursor, err)
	}
	if _, err := s.store.Exec(`DELETE FROM channel_reads WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	body := bodyThatRevokes(t, map[string]string{"last_read_message_id": last.ID}, func() error {
		w := request(t, h, "POST", "/api/logout", nil, member)
		if w.Code != 200 {
			return fmt.Errorf("logout: %d", w.Code)
		}
		return nil
	})
	if w := pausedRequest(t, h, "POST", path, member, body); w.Code != http.StatusUnauthorized {
		t.Errorf("ended session restored read state: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM channel_reads WHERE user_id=?`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ended session wrote %d cursors: %v", count, err)
	}
}
