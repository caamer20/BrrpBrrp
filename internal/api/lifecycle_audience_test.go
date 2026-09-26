package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// socketEvents collects every event that arrives within the window. A read that
// times out ends the collection, which is how "nothing was delivered" is
// observed on a real socket instead of from a helper's return value.
func socketEvents(t *testing.T, conn *websocket.Conn, window time.Duration) []ws.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	events := []ws.Event{}
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			return events
		}
		events = append(events, event)
	}
}

func dialSocket(t *testing.T, s *Server, server *httptest.Server, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	headers := http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{s.config.BaseURL}}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	readEvent(t, ctx, conn, "hello")
	return conn
}

// eventMentions reports whether an event's payload carries one of the given
// strings. It inspects the delivered bytes rather than a server-side decision,
// because the finding this test closes was about what a socket actually received.
func eventMentions(t *testing.T, event ws.Event, needles ...string) bool {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// TestPrivateChannelLifecycleReachesOnlyItsAudience is the F-02 regression. An
// outsider returned 404 for private channel history used to still receive the
// channel's identifier, name and topic over its own WebSocket in
// channel.created/updated/deleted. Event ordering also matters: a socket that was
// connected before the channel existed was the reproduced case.
func TestPrivateChannelLifecycleReachesOnlyItsAudience(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insiderCookie := memberAccount(t, s, "insider")
	_, outsiderCookie := memberAccount(t, s, "outsider")

	server := httptest.NewServer(handler)
	defer server.Close()
	insiderSocket := dialSocket(t, s, server, insiderCookie)
	outsiderSocket := dialSocket(t, s, server, outsiderCookie)

	// The outsider cannot see private channels at all.
	if w := request(t, handler, "GET", "/api/channels", nil, outsiderCookie); w.Code != 200 {
		t.Fatalf("channel list: %d", w.Code)
	}
	created := request(t, handler, "POST", "/api/channels", db.Channel{Name: "vault", Type: "text", MaxVoiceUsers: 8, Visibility: "private", Topic: "the hiding place"}, admin)
	if created.Code != 201 {
		t.Fatalf("create private channel: %d %s", created.Code, created.Body.String())
	}
	channel := readJSON[db.Channel](t, created)
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, outsiderCookie); w.Code != http.StatusNotFound {
		t.Fatalf("outsider read private history: %d", w.Code)
	}
	// The granted insider is the audience the event must reach.
	grant := map[string]any{"visibility": "private", "user_ids": []string{insiderID}}
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", grant, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "PATCH", "/api/channels/"+channel.ID, db.Channel{Name: "vault", Type: "text", MaxVoiceUsers: 8, Visibility: "private", Topic: "the hiding place, renamed"}, admin); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/admin/channels/"+channel.ID+"/purge", map[string]any{"older_than_days": 0, "confirm": "vault"}, admin); w.Code != 200 {
		t.Fatalf("purge: %d %s", w.Code, w.Body.String())
	}
	if w := request(t, handler, "DELETE", "/api/channels/"+channel.ID, map[string]any{"confirm": "vault"}, admin); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	// The outsider's socket must carry none of it: not the identifier, not the
	// name, not the topic.
	for _, event := range socketEvents(t, outsiderSocket, 1500*time.Millisecond) {
		if eventMentions(t, event, channel.ID, "vault", "hiding place") {
			t.Fatalf("an outsider socket received private channel metadata: %s %s", event.Type, event.Data)
		}
	}

	// The granted member, by contrast, is told what changed, so the fix is a
	// restriction rather than a removed delivery path.
	insider := socketEvents(t, insiderSocket, 1500*time.Millisecond)
	named := 0
	for _, event := range insider {
		if eventMentions(t, event, channel.ID) {
			named++
		}
	}
	if named == 0 {
		t.Fatal("the channel's own audience received no event naming it")
	}
}

// TestChannelReorderDoesNotNamePrivateChannels covers the same finding on the
// reordering path, which used to broadcast the complete ordered channel list.
func TestChannelReorderDoesNotNamePrivateChannels(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, outsiderCookie := memberAccount(t, s, "outsider")

	server := httptest.NewServer(handler)
	defer server.Close()
	outsiderSocket := dialSocket(t, s, server, outsiderCookie)

	created := request(t, handler, "POST", "/api/channels", db.Channel{Name: "secret-room", Type: "text", MaxVoiceUsers: 8, Visibility: "private"}, admin)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	hidden := readJSON[db.Channel](t, created)
	text := readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, admin))
	var targetID string
	for _, channel := range text {
		if channel.Type == "text" && channel.ID != hidden.ID {
			targetID = channel.ID
		}
	}
	if targetID == "" {
		t.Fatal("no other text channel to reorder against")
	}
	if w := request(t, handler, "POST", "/api/channels/"+hidden.ID+"/move", map[string]any{"target_id": targetID, "after": true}, admin); w.Code != 200 {
		t.Fatalf("move: %d %s", w.Code, w.Body.String())
	}
	for _, event := range socketEvents(t, outsiderSocket, 1200*time.Millisecond) {
		if eventMentions(t, event, hidden.ID, "secret-room") {
			t.Fatalf("reordering named a private channel to an outsider: %s %s", event.Type, event.Data)
		}
	}
}

// TestLifecycleEventWithheldWhenAudienceCannotBeResolved covers the fail-closed
// half of F-02: a delivery that cannot be scoped is withheld, never broadcast.
func TestLifecycleEventWithheldWhenAudienceCannotBeResolved(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, outsiderCookie := memberAccount(t, s, "outsider")
	server := httptest.NewServer(handler)
	defer server.Close()
	outsiderSocket := dialSocket(t, s, server, outsiderCookie)

	// A channel identifier that cannot be resolved must produce nothing at all.
	s.publishToChannel(t.Context(), "no-such-channel", "channel.updated", db.Channel{ID: "no-such-channel", Name: "ghost"})
	for _, event := range socketEvents(t, outsiderSocket, 500*time.Millisecond) {
		if eventMentions(t, event, "no-such-channel", "ghost") {
			t.Fatalf("an unresolvable channel event was delivered: %s %s", event.Type, event.Data)
		}
	}
	_ = admin
}
