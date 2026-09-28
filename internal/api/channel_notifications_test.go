package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

func setChannelMute(t *testing.T, handler http.Handler, cookie *http.Cookie, channelID string, muted bool) {
	t.Helper()
	w := request(t, handler, "PUT", "/api/channels/"+channelID+"/notifications", map[string]bool{"muted": muted}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("set channel mute: %d %s", w.Code, w.Body.String())
	}
	got := readJSON[channelNotificationPreference](t, w)
	if got.ChannelID != channelID || got.Muted != muted {
		t.Fatalf("wrong preference response: %+v", got)
	}
}

func TestChannelNotificationsArePersonalAndAccessScoped(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	_, outsider := memberAccount(t, s, "outsider")
	channel := privateTextChannel(t, handler, admin, "private mute", memberID)
	path := "/api/channels/" + channel.ID + "/notifications"
	setChannelMute(t, handler, member, channel.ID, true)
	setChannelMute(t, handler, member, channel.ID, true)
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM channel_notification_preferences WHERE user_id=? AND channel_id=?`, memberID, channel.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate preference created extra rows: %d %v", count, err)
	}
	for _, cookie := range []*http.Cookie{admin, outsider} {
		w := request(t, handler, "GET", "/api/me/channel-notifications", nil, cookie)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), channel.ID) {
			t.Fatalf("another account learned the member's preference: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(t, handler, "PUT", path, map[string]any{"muted": false, "user_id": memberID}, admin); w.Code != http.StatusBadRequest {
		t.Fatalf("an administrator selected somebody else's preference: %d %s", w.Code, w.Body.String())
	}
	setChannelMute(t, handler, admin, channel.ID, false)
	if preferences := readJSON[[]channelNotificationPreference](t, request(t, handler, "GET", "/api/me/channel-notifications", nil, member)); len(preferences) != 1 || !preferences[0].Muted {
		t.Fatalf("another account changed the member's mute: %+v", preferences)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"muted": nil}} {
		if w := request(t, handler, "PUT", path, body, member); w.Code != http.StatusBadRequest {
			t.Fatalf("missing explicit preference was accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if w := request(t, handler, "PUT", path, map[string]bool{"muted": true}, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("unreadable room accepted a preference: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PUT", "/api/channels/missing/notifications", map[string]bool{"muted": true}, outsider); w.Code != http.StatusNotFound {
		t.Fatalf("missing room response differs: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	w := request(t, handler, "GET", "/api/me/channel-notifications", nil, member)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), channel.ID) {
		t.Fatalf("revoked room identity leaked through preferences: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PUT", path, map[string]bool{"muted": false}, member); w.Code != http.StatusNotFound {
		t.Fatalf("revoked room preference could still change: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private", "user_ids": []string{memberID}}, admin); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	preferences := readJSON[[]channelNotificationPreference](t, request(t, handler, "GET", "/api/me/channel-notifications", nil, member))
	if len(preferences) != 1 || !preferences[0].Muted {
		t.Fatalf("regaining access lost the personal preference: %+v", preferences)
	}
	if w := request(t, handler, "DELETE", "/api/channels/"+channel.ID, map[string]string{"confirm": channel.Name}, admin); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if err := s.store.QueryRow(`SELECT count(*) FROM channel_notification_preferences WHERE channel_id=?`, channel.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted channel retained preference metadata: %d %v", count, err)
	}
}

func TestChannelNotificationMutationRechecksEndedSession(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	if _, err := s.store.Exec(`DELETE FROM sessions WHERE user_id=?`, memberID); err != nil {
		t.Fatal(err)
	}
	// Enter the handler as an already-authenticated request whose session ended
	// after its guard. The transaction must reject the now-stale identity.
	r := httptest.NewRequest(http.MethodPut, "/api/channels/"+channel.ID+"/notifications", bytes.NewBufferString(`{"muted":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", channel.ID)
	r = r.WithContext(withSession(r.Context(), hashToken(member.Value)))
	w := httptest.NewRecorder()
	s.setChannelNotifications(w, r, db.User{ID: memberID})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("ended session changed preference: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := s.store.QueryRow(`SELECT count(*) FROM channel_notification_preferences`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ended session committed a preference: %d %v", count, err)
	}
}

func TestChannelMuteSuppressesLiveMentionsWithoutHidingHistory(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	mutedID, muted := memberAccount(t, s, "muted")
	allowedID, _ := memberAccount(t, s, "allowed")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	clients := make(map[string]*ws.Client)
	for _, id := range []string{mutedID, allowedID} {
		client := &ws.Client{ID: db.ID(), UserID: id, Out: make(chan ws.Queued, 32)}
		if !s.hub.Add(client) {
			t.Fatal("register notice recipient")
		}
		t.Cleanup(func() { s.hub.Remove(client) })
		clients[id] = client
	}
	setChannelMute(t, handler, muted, channel.ID, true)
	path := "/api/channels/" + channel.ID + "/messages"
	w := request(t, handler, "POST", path, messageInput{Content: "hello @muted @allowed", IdempotencyKey: "muted-mention-1"}, admin)
	if w.Code != http.StatusCreated {
		t.Fatal(w.Body.String())
	}
	mutedEvents := drainNotificationEvents(t, clients[mutedID])
	allowedEvents := drainNotificationEvents(t, clients[allowedID])
	if mutedEvents["notification.mention"] != 0 || allowedEvents["notification.mention"] != 1 {
		t.Fatalf("mention interruptions: muted=%v allowed=%v", mutedEvents, allowedEvents)
	}
	if mutedEvents["message.created"] != 1 {
		t.Fatal("muting suppressed live message history")
	}
	unread, err := s.unread(mutedID)
	preserved := false
	for _, entry := range unread {
		if entry.ChannelID == channel.ID && entry.Count == 1 && entry.Mentions == 1 {
			preserved = true
		}
	}
	if err != nil || !preserved {
		t.Fatalf("muting lost unread/mention records: %+v %v", unread, err)
	}
	if history := readJSON[[]db.Message](t, request(t, handler, "GET", path, nil, muted)); len(history) != 1 {
		t.Fatal("muting hid readable history")
	}
	setChannelMute(t, handler, muted, channel.ID, false)
	if w := request(t, handler, "POST", path, messageInput{Content: "hello @muted", IdempotencyKey: "muted-mention-2"}, admin); w.Code != http.StatusCreated {
		t.Fatal(w.Body.String())
	}
	if events := drainNotificationEvents(t, clients[mutedID]); events["notification.mention"] != 1 {
		t.Fatalf("unmuting did not restore new mention notices: %v", events)
	}
}

// R9-02: a mute is account-owned policy written by one device, so the account's
// other connections have to be told to re-read it. A session that cached the old
// answer keeps filtering mentions on it until it does — and the two ways it used
// to learn were both conditional on a mounted channel header or a published
// authority change, neither of which a mute produces.
func TestChannelMuteTellsTheAccountsOtherDevicesToReReadIt(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	// Two connections for one account stand in for two signed-in devices.
	first := &ws.Client{ID: db.ID(), UserID: memberID, Out: make(chan ws.Queued, 32)}
	second := &ws.Client{ID: db.ID(), UserID: memberID, Out: make(chan ws.Queued, 32)}
	for _, client := range []*ws.Client{first, second} {
		if !s.hub.Add(client) {
			t.Fatal("register device")
		}
		t.Cleanup(func() { s.hub.Remove(client) })
	}
	otherID, _ := memberAccount(t, s, "bystander")
	bystander := &ws.Client{ID: db.ID(), UserID: otherID, Out: make(chan ws.Queued, 32)}
	if !s.hub.Add(bystander) {
		t.Fatal("register bystander")
	}
	t.Cleanup(func() { s.hub.Remove(bystander) })

	setChannelMute(t, handler, member, channel.ID, true)
	if events := drainNotificationEvents(t, first); events["preferences.changed"] != 1 {
		t.Fatalf("the writing device was not told to re-read the policy it wrote: %v", events)
	}
	if events := drainNotificationEvents(t, second); events["preferences.changed"] != 1 {
		t.Fatalf("another device of the same account was not told: %v", events)
	}
	if events := drainNotificationEvents(t, bystander); len(events) != 0 {
		t.Fatalf("an unrelated account was told about another account's preference: %v", events)
	}

	// Unmuting is the same signal: the defect was silence in both directions.
	drainNotificationEvents(t, first)
	drainNotificationEvents(t, second)
	setChannelMute(t, handler, member, channel.ID, false)
	if events := drainNotificationEvents(t, second); events["preferences.changed"] != 1 {
		t.Fatalf("an unmute did not reach the other device: %v", events)
	}

	// The policy is account state, not authority: it must not advance the
	// authorization revision, or every mute would invalidate every connection's
	// queued events and force a whole-workspace refetch.
	before := s.authorizationRevision(t.Context())
	setChannelMute(t, handler, member, channel.ID, true)
	if after := s.authorizationRevision(t.Context()); after != before {
		t.Fatalf("a mute advanced the authorization revision %d -> %d", before, after)
	}
}

// R9-02: the signal is a hint with no authority in it. A client that trusts the
// payload instead of re-reading its own policy must be wrong, so the event
// carries only what changed and never the resulting mute state.
func TestThePreferenceSignalCarriesNoPolicyValue(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	client := &ws.Client{ID: db.ID(), UserID: memberID, Out: make(chan ws.Queued, 32)}
	if !s.hub.Add(client) {
		t.Fatal("register device")
	}
	t.Cleanup(func() { s.hub.Remove(client) })
	setChannelMute(t, handler, member, channel.ID, true)
	select {
	case queued := <-client.Out:
		var event ws.Event
		if err := json.Unmarshal(queued.Event, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "preferences.changed" {
			t.Fatalf("unexpected event %s", event.Type)
		}
		// The flag says which policy changed, not what it now is: a payload
		// carrying `muted` would let a client adopt another device's answer
		// without reading its own.
		if strings.Contains(string(queued.Event), "muted") {
			t.Fatalf("the signal carries a mute value a client could trust: %s", queued.Event)
		}
		if !strings.Contains(string(queued.Event), "channel_notifications") {
			t.Fatalf("the signal does not say which policy changed: %s", queued.Event)
		}
	default:
		t.Fatal("no preference signal was delivered")
	}
}

func drainNotificationEvents(t *testing.T, client *ws.Client) map[string]int {
	t.Helper()
	events := map[string]int{}
	for {
		select {
		case queued := <-client.Out:
			var event ws.Event
			if err := json.Unmarshal(queued.Event, &event); err != nil {
				t.Fatal(err)
			}
			events[event.Type]++
		default:
			return events
		}
	}
}

func TestChannelMuteSuppressesAlreadyQueuedPushAndUnmuteRestoresIt(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	waitingID, _ := memberAccount(t, s, "waiting")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	subscribeDevice(t, s, memberID, "https://muted.example.invalid/device")
	subscribeDevice(t, s, waitingID, "https://waiting.example.invalid/device")
	recorder := newPushRecorder()
	t.Cleanup(func() {
		select {
		case <-recorder.release:
		default:
			close(recorder.release)
		}
	})
	s.pushHTTP = &http.Client{Transport: recorder}
	s.pushJobs <- pushJob{userID: waitingID, payload: map[string]string{"body": "block"}}
	select {
	case <-recorder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("push worker did not start")
	}
	s.pushJobs <- pushJob{userID: memberID, channelID: channel.ID, payload: map[string]string{"body": "already queued"}}
	s.pushJobs <- pushJob{userID: waitingID, payload: map[string]string{"body": "barrier"}}
	setChannelMute(t, handler, member, channel.ID, true)
	close(recorder.release)
	waitPushCount(t, recorder, "waiting.example.invalid", 2)
	if count := recorder.sent("muted.example.invalid"); count != 0 {
		t.Fatalf("muting did not suppress the queued push: %d", count)
	}
	setChannelMute(t, handler, member, channel.ID, false)
	s.pushJobs <- pushJob{userID: memberID, channelID: channel.ID, payload: map[string]string{"body": "unmuted"}}
	waitPushCount(t, recorder, "muted.example.invalid", 1)
}

func TestChannelMuteIsRecheckedBetweenPushDevices(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	memberID, member := memberAccount(t, s, "reader")
	waitingID, _ := memberAccount(t, s, "waiting")
	channel := slowModeChannel(t, handler, admin, "chat", 0)
	subscribeDevice(t, s, memberID, "https://first.example.invalid/device")
	subscribeDevice(t, s, memberID, "https://second.example.invalid/device")
	subscribeDevice(t, s, waitingID, "https://waiting.example.invalid/device")
	recorder := newPushRecorder()
	t.Cleanup(func() {
		select {
		case <-recorder.release:
		default:
			close(recorder.release)
		}
	})
	s.pushHTTP = &http.Client{Transport: recorder}
	s.pushJobs <- pushJob{userID: memberID, channelID: channel.ID, payload: map[string]string{"body": "two devices"}}
	select {
	case <-recorder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("push worker did not start")
	}
	setChannelMute(t, handler, member, channel.ID, true)
	s.pushJobs <- pushJob{userID: waitingID, payload: map[string]string{"body": "barrier"}}
	close(recorder.release)
	waitPushCount(t, recorder, "waiting.example.invalid", 1)
	if sent := recorder.sent("first.example.invalid") + recorder.sent("second.example.invalid"); sent != 1 {
		t.Fatalf("later device reused a pre-mute decision: sent %d requests, want only the already-admitted request", sent)
	}
}

func waitPushCount(t *testing.T, recorder *pushRecorder, host string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for recorder.sent(host) < want {
		if time.Now().After(deadline) {
			t.Fatalf("push worker did not send %d requests to %s: %d", want, host, recorder.sent(host))
		}
		time.Sleep(5 * time.Millisecond)
	}
}
