package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"brrpbrrp/internal/db"
)

func slowModeChannel(t *testing.T, handler http.Handler, manager *http.Cookie, name string, seconds int) db.Channel {
	t.Helper()
	w := request(t, handler, "POST", "/api/channels", db.Channel{Name: name, Type: "text", MaxVoiceUsers: 8, SlowModeSeconds: seconds}, manager)
	if w.Code != http.StatusCreated {
		t.Fatalf("create channel: %d %s", w.Code, w.Body.String())
	}
	return readJSON[db.Channel](t, w)
}

func TestSlowModeSettingsRespectChannelManagementAndLimits(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	_, member := memberAccount(t, s, "slow-member")
	managerID, manager := memberAccount(t, s, "slow-manager")
	customRole(t, s, "channel manager", 20, `["channel.manage"]`, managerID)
	channel := slowModeChannel(t, handler, admin, "slow", 0)
	path := "/api/channels/" + channel.ID
	if channel.SlowModeSeconds != 0 {
		t.Fatal("slow mode must be off by default")
	}
	if w := request(t, handler, "PATCH", path, map[string]int{"slow_mode_seconds": 30}, member); w.Code != http.StatusForbidden {
		t.Fatalf("ordinary member changed slow mode: %d %s", w.Code, w.Body.String())
	}
	for _, seconds := range []int{-1, 21601} {
		if w := request(t, handler, "PATCH", path, map[string]int{"slow_mode_seconds": seconds}, manager); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid slow mode %d accepted: %d %s", seconds, w.Code, w.Body.String())
		}
	}
	for _, seconds := range []int{21600, 30, 0} {
		w := request(t, handler, "PATCH", path, map[string]int{"slow_mode_seconds": seconds}, manager)
		if w.Code != http.StatusOK {
			t.Fatalf("channel manager setting %d: %d %s", seconds, w.Code, w.Body.String())
		}
		got := readJSON[db.Channel](t, w)
		if got.SlowModeSeconds != seconds || got.Name != channel.Name || got.Type != "text" {
			t.Fatalf("patch changed unrelated channel data: %+v", got)
		}
		channels := readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, member))
		found := false
		for _, visible := range channels {
			if visible.ID == channel.ID {
				found = visible.SlowModeSeconds == seconds
			}
		}
		if !found {
			t.Fatal("members did not receive the current slow mode setting")
		}
	}
	if w := request(t, handler, "POST", "/api/channels", db.Channel{Name: "voice", Type: "voice", MaxVoiceUsers: 8, SlowModeSeconds: 1}, admin); w.Code != http.StatusBadRequest {
		t.Fatalf("voice channel accepted a message-only setting: %d", w.Code)
	}
}

// The HTTP requests start together and use the actual transactional send path.
// A durable interval admits one new operation, while replaying that operation
// remains available without changing its cooldown or consuming another receipt.
func TestSlowModeConcurrentSendsAndReceiptReplay(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-key-%t", sameKey), func(t *testing.T) {
			s, handler := testServer(t)
			admin := setupAdmin(t, s, handler)
			memberID, member := memberAccount(t, s, "sender")
			channel := slowModeChannel(t, handler, admin, "slow", 60)
			path := "/api/channels/" + channel.ID + "/messages"
			inputs := []messageInput{{Content: "first", IdempotencyKey: "slow-operation-1"}, {Content: "second", IdempotencyKey: "slow-operation-2"}}
			if sameKey {
				inputs[1] = inputs[0]
			}
			responses := make([]*httptest.ResponseRecorder, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for index, input := range inputs {
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("Origin", "https://localhost:8080")
					r.AddCookie(member)
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					responses[index] = w
				}()
			}
			close(start)
			workers.Wait()
			winner, created := -1, 0
			for index, response := range responses {
				if response.Code == http.StatusCreated {
					winner, created = index, created+1
					continue
				}
				if sameKey {
					if response.Code != http.StatusOK {
						t.Fatalf("concurrent receipt replay: %d %s", response.Code, response.Body.String())
					}
				} else {
					assertSlowModeWait(t, response)
				}
			}
			if created != 1 {
				t.Fatalf("created %d messages, want one", created)
			}
			var count int
			if err := s.store.QueryRow(`SELECT count(*) FROM message_operations WHERE account_id=?`, memberID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("a rejected send consumed a receipt: %d %v", count, err)
			}
			var before, after string
			if err := s.store.QueryRow(`SELECT last_sent_at FROM channel_slow_mode_sends WHERE channel_id=? AND user_id=?`, channel.ID, memberID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if replay := request(t, handler, "POST", path, inputs[winner], member); replay.Code != http.StatusOK {
				t.Fatalf("known receipt was blocked by slow mode: %d %s", replay.Code, replay.Body.String())
			}
			if err := s.store.QueryRow(`SELECT last_sent_at FROM channel_slow_mode_sends WHERE channel_id=? AND user_id=?`, channel.ID, memberID).Scan(&after); err != nil || before != after {
				t.Fatalf("replay advanced the send clock: %q -> %q (%v)", before, after, err)
			}
			message := readJSON[db.Message](t, responses[winner])
			if removed := request(t, handler, "DELETE", "/api/messages/"+message.ID, map[string]string{"confirm": message.ID}, admin); removed.Code != http.StatusOK {
				t.Fatalf("delete sent message: %d %s", removed.Code, removed.Body.String())
			}
			if replay := request(t, handler, "POST", path, inputs[winner], member); replay.Code != http.StatusConflict {
				t.Fatalf("deleted receipt should retain its distinct outcome: %d %s", replay.Code, replay.Body.String())
			}
			assertSlowModeWait(t, request(t, handler, "POST", path, messageInput{Content: "after delete", IdempotencyKey: "slow-after-delete"}, member))
			// A second account and another channel retain their own independent clocks.
			_, other := memberAccount(t, s, "other")
			if w := request(t, handler, "POST", path, messageInput{Content: "another account", IdempotencyKey: "slow-other-account"}, other); w.Code != http.StatusCreated {
				t.Fatalf("slow mode leaked across accounts: %d %s", w.Code, w.Body.String())
			}
			elsewhere := slowModeChannel(t, handler, admin, "elsewhere", 60)
			if w := request(t, handler, "POST", "/api/channels/"+elsewhere.ID+"/messages", messageInput{Content: "elsewhere", IdempotencyKey: "slow-other-channel"}, member); w.Code != http.StatusCreated {
				t.Fatalf("slow mode leaked across channels: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func assertSlowModeWait(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	seconds, err := strconv.Atoi(response.Header().Get("Retry-After"))
	if response.Code != http.StatusTooManyRequests || err != nil || seconds < 1 || seconds > 60 || !strings.Contains(response.Body.String(), "Slow mode") {
		t.Fatalf("expected an actionable cooldown: %d %s retry=%q", response.Code, response.Body.String(), response.Header().Get("Retry-After"))
	}
}

func TestSlowModeRollbackBypassAndRestart(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "sender")
	managerID, manager := memberAccount(t, s, "manager")
	role := customRole(t, s, "slow mode bypass", 20, `["channel.manage"]`, managerID)
	channel := slowModeChannel(t, handler, admin, "slow", 60)
	path := "/api/channels/" + channel.ID + "/messages"
	bad := messageInput{Content: "invalid attachment", IdempotencyKey: "slow-invalid-send", AttachmentIDs: []string{"missing"}}
	if w := request(t, handler, "POST", path, bad, member); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid attachment: %d %s", w.Code, w.Body.String())
	}
	bad.AttachmentIDs = nil
	if w := request(t, handler, "POST", path, bad, member); w.Code != http.StatusCreated {
		t.Fatalf("rolled-back send consumed its key or cooldown: %d %s", w.Code, w.Body.String())
	}
	for i := range 2 {
		if w := request(t, handler, "POST", path, messageInput{Content: "manager", IdempotencyKey: fmt.Sprintf("slow-manager-%d", i)}, manager); w.Code != http.StatusCreated {
			t.Fatalf("channel manager could not bypass slow mode: %d %s", w.Code, w.Body.String())
		}
	}
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, managerID, role); err != nil {
		t.Fatal(err)
	}
	assertSlowModeWait(t, request(t, handler, "POST", path, messageInput{Content: "demoted", IdempotencyKey: "slow-demoted-send"}, manager))
	s.Close()
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(s.config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	restarted, err := New(s.config, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	handler = restarted.Handler()
	assertSlowModeWait(t, request(t, handler, "POST", path, messageInput{Content: "after restart", IdempotencyKey: "slow-restart-send"}, member))
	// Advance the persisted fixture's clock instead of sleeping for an interval.
	if _, err := store.Exec(`UPDATE channel_slow_mode_sends SET last_sent_at=? WHERE channel_id=? AND user_id=?`, db.Timestamp(time.Now().Add(-61*time.Second)), channel.ID, memberID); err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "POST", path, messageInput{Content: "after interval", IdempotencyKey: "slow-interval-send"}, member); w.Code != http.StatusCreated {
		t.Fatalf("elapsed interval did not allow a new send: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/channels/"+channel.ID, map[string]int{"slow_mode_seconds": 0}, admin); w.Code != http.StatusOK {
		t.Fatalf("disable slow mode: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", path, messageInput{Content: "disabled", IdempotencyKey: "slow-disabled-send"}, member); w.Code != http.StatusCreated {
		t.Fatalf("disabled slow mode still blocked messages: %d %s", w.Code, w.Body.String())
	}
}
