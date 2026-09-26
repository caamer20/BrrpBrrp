package api

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"brrpbrrp/internal/db"
)

func TestLightweightUnreadAndHistoryWindow(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	other, member := ordinaryUser(t, s)
	var channel, adminID string
	s.store.QueryRow(`SELECT id FROM channels WHERE type='text' LIMIT 1`).Scan(&channel)
	s.store.QueryRow(`SELECT id FROM users WHERE is_admin=1 LIMIT 1`).Scan(&adminID)
	ids := make([]string, 280)
	for i := range ids {
		ids[i] = db.ID()
		author := adminID
		if i == 101 {
			author = other
		}
		if _, err := s.store.Exec(`INSERT INTO messages(id,channel_id,conversation_id,user_id,content,created_at,idempotency_key) VALUES(?,?,?,?,?,?,?)`, ids[i], channel, channel, author, fmt.Sprint(i), db.Now(), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/channels/" + channel
	if w := request(t, handler, "GET", path+"/messages?from="+ids[0]+"&to="+ids[279], nil, nil); w.Code != 401 {
		t.Fatal("anonymous history access")
	}
	w := request(t, handler, "GET", path+"/messages?from="+ids[0]+"&to="+ids[279], nil, member)
	items := readJSON[[]db.Message](t, w)
	if len(items) != 250 || items[0].ID != ids[30] || items[249].ID != ids[279] {
		t.Fatal("range is not bounded/chronological")
	}
	if w := request(t, handler, "GET", path+"/messages?from=invalid&to="+ids[5], nil, member); w.Code != 400 {
		t.Fatal("invalid range accepted")
	}
	if w := request(t, handler, "GET", path+"/messages?from="+ids[5]+"&to="+ids[0], nil, member); w.Code != 400 {
		t.Fatal("reversed range accepted")
	}
	// The user can return to an earlier unread boundary after a latest-view read update.
	request(t, handler, "POST", path+"/read", map[string]string{"last_read_message_id": ids[279]}, member)
	w = request(t, handler, "GET", path+"/first-unread?after="+ids[100], nil, member)
	first := readJSON[map[string]string](t, w)
	if first["message_id"] != ids[102] {
		t.Fatal("first unread did not skip the user's own message")
	}
	snapshot := readJSON[struct {
		Unread []unreadCount `json:"unread"`
	}](t, request(t, handler, "GET", "/api/state", nil, admin))
	w = request(t, handler, "GET", "/api/unread", nil, admin)
	counts := readJSON[[]unreadCount](t, w)
	if !reflect.DeepEqual(snapshot.Unread, counts) {
		t.Fatal("lightweight unread differs from workspace state")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private counts are cacheable")
	}
	if w := request(t, handler, "GET", "/api/unread", nil, nil); w.Code != 401 {
		t.Fatal("anonymous unread access")
	}
}

func TestPushDeviceStatusIsAccountScoped(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := ordinaryUser(t, s)
	endpoint := "https://push.example.test/private-endpoint"
	if _, err := s.store.Exec(`INSERT INTO push_subscriptions(id,user_id,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?)`, db.ID(), memberID, endpoint, "key", "auth", db.Now()); err != nil {
		t.Fatal(err)
	}
	w := request(t, handler, "GET", "/api/push/devices", nil, member)
	if !strings.Contains(w.Body.String(), hashToken(endpoint)) || strings.Contains(w.Body.String(), endpoint) {
		t.Fatal("status should contain only endpoint hashes")
	}
	w = request(t, handler, "GET", "/api/push/devices", nil, admin)
	if strings.Contains(w.Body.String(), hashToken(endpoint)) {
		t.Fatal("another account's subscription leaked")
	}
	if w := request(t, handler, "GET", "/api/push/devices", nil, nil); w.Code != 401 {
		t.Fatal("anonymous device status access")
	}
}
