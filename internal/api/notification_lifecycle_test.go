package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testPushSubscription(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"auth":   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}}
}

func TestPushDeviceMutationsRejectARevokedSession(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			s, handler := testServer(t)
			setupAdmin(t, s, handler)
			s.settingsMu.Lock()
			s.settings.Push = true
			s.settingsMu.Unlock()
			id, ended := memberAccount(t, s, "device_owner")
			fresh := httptest.NewRecorder()
			if err := s.newSession(fresh, id); err != nil {
				t.Fatal(err)
			}
			survivor := fresh.Result().Cookies()[0]
			// A literal public IP exercises endpoint validation without DNS or any
			// network request. No notification is dispatched by this test.
			endpoint := "https://1.1.1.1/notification-device"
			body := testPushSubscription(t, endpoint)
			if method == http.MethodDelete {
				if w := request(t, handler, http.MethodPost, "/api/push/subscribe", body, survivor); w.Code != http.StatusCreated {
					t.Fatalf("control subscribe: %d %s", w.Code, w.Body.String())
				}
				body = map[string]any{"endpoint": endpoint}
			}
			paused := bodyThatRevokes(t, body, func() error {
				w := request(t, handler, http.MethodPost, "/api/logout", nil, ended)
				if w.Code != http.StatusOK {
					return fmt.Errorf("logout: %d %s", w.Code, w.Body.String())
				}
				return nil
			})
			w := pausedRequest(t, handler, method, "/api/push/subscribe", ended, paused)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("ended device mutated push registration: %d %s", w.Code, w.Body.String())
			}
			var count int
			if err := s.store.QueryRow(`SELECT count(*) FROM push_subscriptions WHERE user_id=?`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if method == http.MethodDelete {
				want = 1
			}
			if count != want {
				t.Errorf("ended request changed registrations: got%d want%d", count, want)
			}
			if method == http.MethodPost {
				body = testPushSubscription(t, endpoint)
			}
			control := request(t, handler, method, "/api/push/subscribe", body, survivor)
			if control.Code != http.StatusOK && control.Code != http.StatusCreated {
				t.Fatalf("surviving device refused: %d %s", control.Code, control.Body.String())
			}
		})
	}
}

func TestRemovedAccountSocketCleanupDoesNotRestoreActivityMetadata(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	enrollMFA(t, handler, admin, "correct horse battery")
	id, member := memberAccount(t, s, "departing")
	server := httptest.NewServer(handler)
	defer server.Close()
	conn := voiceSocket(t, s, server, member)
	defer conn.CloseNow()
	w := request(t, handler, "POST", "/api/admin/users/"+id+"/anonymize", map[string]string{"confirm": "departing"}, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	done := make(chan struct{})
	go func() { s.connections.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("removed account socket did not close")
	}
	var lastSeen sql.NullString
	if err := s.store.QueryRow(`SELECT last_seen_at FROM users WHERE id=?`, id).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if lastSeen.Valid {
		t.Errorf("socket cleanup restored the removed identity's activity: %s", lastSeen.String)
	}
	// Ordinary disconnects still record useful activity for an active member.
	activeID, active := memberAccount(t, s, "remaining")
	activeConn := voiceSocket(t, s, server, active)
	activeConn.CloseNow()
	done = make(chan struct{})
	go func() { s.connections.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("active account socket did not close")
	}
	if err := s.store.QueryRow(`SELECT last_seen_at FROM users WHERE id=?`, activeID).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if !lastSeen.Valid {
		t.Fatal("ordinary disconnect lost its activity update")
	}
}

func TestInactiveAccountsHaveNoPushDispatchSubscriptions(t *testing.T) {
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	id, _ := memberAccount(t, s, "inactive")
	subscribeDevice(t, s, id, "https://device.example.invalid/push")
	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	devices, err := s.pushSubscriptions(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatal("inactive account remains eligible for unscoped push dispatch")
	}
	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	devices, err = s.pushSubscriptions(id)
	if err != nil || len(devices) != 1 {
		t.Fatalf("active device was refused: %v %v", devices, err)
	}
}
