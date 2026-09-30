package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/rtc"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// voiceUpdate is one channel's participant list as the client receives it.
type voiceUpdate struct {
	ChannelID string      `json:"channel_id"`
	States    []rtc.State `json:"states"`
}

// voiceSocket dials the realtime endpoint for one session and consumes the hello
// frame, so every later read is an event the server actually delivered.
func voiceSocket(t *testing.T, s *Server, server *httptest.Server, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

// voiceUpdateWithin waits for the next voice update for one channel, or reports
// that none arrived inside the window. Proving silence needs a bounded read
// rather than an assumption.
func voiceUpdateWithin(t *testing.T, conn *websocket.Conn, channelID string, window time.Duration) (voiceUpdate, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return voiceUpdate{}, false
			}
			t.Fatalf("socket read failed: %v", err)
		}
		if event.Type != "voice.state_updated" {
			continue
		}
		var update voiceUpdate
		if err := json.Unmarshal(event.Data, &update); err != nil {
			t.Fatalf("voice update is unreadable: %v", err)
		}
		if update.ChannelID == channelID {
			return update, true
		}
	}
}

// expectVoiceUpdate fails when the channel's list never arrives.
func expectVoiceUpdate(t *testing.T, conn *websocket.Conn, channelID string) voiceUpdate {
	t.Helper()
	update, ok := voiceUpdateWithin(t, conn, channelID, 3*time.Second)
	if !ok {
		t.Fatalf("no voice update for channel %s arrived", channelID)
	}
	return update
}

// voiceError reads the refusal the server sent for a voice command. Joining and
// moving answer on their own error channels, so both are accepted here.
func voiceError(t *testing.T, ctx context.Context, conn *websocket.Conn) string {
	t.Helper()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("no voice refusal arrived: %v", err)
		}
		if event.Type != "voice.error" && event.Type != "voice.move.error" {
			continue
		}
		var payload map[string]string
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatal(err)
		}
		return payload["error"]
	}
}

// privateVoiceChannel creates a restricted voice channel and grants it to the
// accounts or roles named.
func privateVoiceChannel(t *testing.T, s *Server, handler http.Handler, admin *http.Cookie, name string, access map[string]any) db.Channel {
	t.Helper()
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: name, Type: "voice", MaxVoiceUsers: 4, Visibility: "private"}, admin))
	if channel.Type != "voice" || channel.Visibility != "private" {
		t.Fatalf("created channel = %+v", channel)
	}
	access["visibility"] = "private"
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", access, admin); w.Code != 200 {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
	return channel
}

// TestVoicePresenceFollowsChannelAccess is the voice half of the privacy
// boundary: an account that may not read a restricted channel sees neither a
// participant nor a mute flag, and cannot enter the channel by guessing its id.
func TestVoicePresenceFollowsChannelAccess(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	outsiderID, outsider := memberAccount(t, s, "outsider")
	channel := privateVoiceChannel(t, s, handler, admin, "briefing", map[string]any{"user_ids": []string{insiderID}})

	// The insider is in the channel; the outsider holds nothing.
	if err := s.voice.Join(insiderID, "insider-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.voice.Leave(insiderID, "") })

	inside := readJSON[struct {
		VoiceStates []rtc.State `json:"voice_states"`
	}](t, request(t, handler, "GET", "/api/state", nil, insider))
	if len(inside.VoiceStates) != 1 || inside.VoiceStates[0].UserID != insiderID || inside.VoiceStates[0].ChannelID != channel.ID {
		t.Fatalf("granted member's snapshot = %+v", inside.VoiceStates)
	}
	outside := readJSON[struct {
		VoiceStates []rtc.State `json:"voice_states"`
	}](t, request(t, handler, "GET", "/api/state", nil, outsider))
	if len(outside.VoiceStates) != 0 {
		t.Fatalf("an account without access received voice presence: %+v", outside.VoiceStates)
	}
	// Community managers can read every community channel, including restricted
	// ones, and the access inspector says so. Voice is no exception.
	manager := readJSON[struct {
		VoiceStates []rtc.State `json:"voice_states"`
	}](t, request(t, handler, "GET", "/api/state", nil, admin))
	if len(manager.VoiceStates) != 1 || manager.VoiceStates[0].UserID != insiderID {
		t.Fatalf("manager's snapshot = %+v", manager.VoiceStates)
	}

	// A guessed identifier is answered exactly like a missing channel.
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outsiderConn := voiceSocket(t, s, server, outsider)
	if err := wsjson.Write(ctx, outsiderConn, map[string]any{"t": "voice.join", "d": map[string]string{"channel_id": channel.ID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, outsiderConn); message != "Voice room not found." {
		t.Fatalf("join refusal = %q", message)
	}
	if err := wsjson.Write(ctx, outsiderConn, map[string]any{"t": "voice.move", "d": map[string]string{"channel_id": channel.ID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, outsiderConn); message != "Voice room not found." {
		t.Fatalf("move refusal = %q", message)
	}
	for _, state := range s.voice.States() {
		if state.UserID == outsiderID {
			t.Fatalf("an unauthorized account entered voice: %+v", state)
		}
	}
	// The granted member still joins the ordinary way, from a real socket.
	s.voice.Leave(insiderID, "")
	grantedConn := voiceSocket(t, s, server, insider)
	if err := wsjson.Write(ctx, grantedConn, map[string]any{"t": "voice.join", "d": map[string]string{"channel_id": channel.ID}}); err != nil {
		t.Fatal(err)
	}
	accepted := false
	for !accepted {
		var event ws.Event
		if err := wsjson.Read(ctx, grantedConn, &event); err != nil {
			t.Fatalf("the granted member could not join voice: %v", err)
		}
		switch event.Type {
		case "voice.error":
			t.Fatalf("the granted member was refused: %s", event.Data)
		case "voice.join.accept":
			accepted = true
		}
	}
}

// TestVoiceStateEventsStayInsideTheChannelAudience is the delivery half: a
// restricted channel's voice event reaches its readers and nobody else, and an
// emptied channel publishes its empty list to the same readers so no client keeps
// showing a participant who has left.
func TestVoiceStateEventsStayInsideTheChannelAudience(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	_, outsider := memberAccount(t, s, "outsider")
	channel := privateVoiceChannel(t, s, handler, admin, "briefing", map[string]any{"user_ids": []string{insiderID}})

	server := httptest.NewServer(handler)
	defer server.Close()
	insiderConn := voiceSocket(t, s, server, insider)
	outsiderConn := voiceSocket(t, s, server, outsider)
	managerConn := voiceSocket(t, s, server, admin)

	if err := s.voice.Join(insiderID, "insider-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	update := expectVoiceUpdate(t, insiderConn, channel.ID)
	if len(update.States) != 1 || update.States[0].UserID != insiderID {
		t.Fatalf("granted member's update = %+v", update)
	}
	if _, ok := voiceUpdateWithin(t, managerConn, channel.ID, time.Second); !ok {
		t.Fatal("a community manager did not receive the restricted channel's voice state")
	}
	if update, ok := voiceUpdateWithin(t, outsiderConn, channel.ID, 300*time.Millisecond); ok {
		t.Fatalf("an account without access received restricted voice presence: %+v", update)
	}

	s.voice.Leave(insiderID, "")
	emptied := expectVoiceUpdate(t, insiderConn, channel.ID)
	if len(emptied.States) != 0 {
		t.Fatalf("leaving published %+v instead of an empty list", emptied.States)
	}
}

// TestPublicVoicePresenceStillRequiresChannelRead is the maintained R5-02 regression.
// Review 5 reproduced it by narrowing the default role so an implicit holder lost
// channel.read, confirming its own /state no longer listed the public voice room, and then
// admitting a permitted account to that room through the real SFU: the denied member's
// actual socket still received voice.state_updated with the room id, the participant id
// and its mute, deafen and speaking flags. Visibility is an input to authorization, not a
// substitute for the capability, so public and private presence now take the same
// capability-aware, revision-checked path.
func TestPublicVoicePresenceStillRequiresChannelRead(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	_, denied := implicitMember(t, s, handler, admin, "r5-voice-no-read")
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels",
		db.Channel{Name: "r5-public-voice", Type: "voice", Visibility: "public", MaxVoiceUsers: 8}, admin))

	server := httptest.NewServer(handler)
	defer server.Close()
	deniedSocket := voiceSocket(t, s, server, denied)
	managerSocket := voiceSocket(t, s, server, admin)

	// Removing channel.read from the default role denies the implicit holder the public
	// channel, which its own state snapshot confirms.
	role := defaultRole(t, handler, admin)
	changed := request(t, handler, "PATCH", "/api/roles/"+role.ID,
		map[string]any{"name": role.Name, "position": role.Position, "permissions": withoutCapability(role.Permissions, "channel.read")}, admin)
	if changed.Code != http.StatusOK {
		t.Fatalf("narrow the default role: %d %s", changed.Code, changed.Body.String())
	}
	visible := readJSON[struct {
		Channels []db.Channel `json:"channels"`
	}](t, request(t, handler, "GET", "/api/state", nil, denied))
	for _, listed := range visible.Channels {
		if listed.ID == channel.ID {
			t.Fatal("fixture: the denied account still sees the channel")
		}
	}

	// An admitted account joins through the real SFU, which triggers the normal publisher.
	adminID := sessionUserID(t, s, admin)
	if err := s.voice.Join(adminID, "r5-02-voice", channel.ID, 8, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	defer s.voice.Leave(adminID, "r5-02-voice")

	// The denied account learns nothing about the room: no participant id, no flags, not
	// even that the room exists.
	if update, ok := voiceUpdateWithin(t, deniedSocket, channel.ID, 500*time.Millisecond); ok {
		t.Fatalf("a denied account received public voice presence: %+v", update)
	}
	// The permitted control: the manager keeps channel.read and receives the update.
	update, ok := voiceUpdateWithin(t, managerSocket, channel.ID, 2*time.Second)
	if !ok || len(update.States) != 1 || update.States[0].UserID != adminID {
		t.Fatalf("a still-authorized account did not receive the voice presence: %+v (%v)", update, ok)
	}
}

// TestADetachedVoicePeerCannotResumeAfterRevocation covers the third T-AUTH-02
// surface: media that survives a dropped socket. A tab that was disconnected keeps a
// resume token, so the revocation has to reach the detached connection too, or a
// withdrawn grant could be undone by reconnecting.
func TestADetachedVoicePeerCannotResumeAfterRevocation(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, _ := memberAccount(t, s, "insider")
	channel := privateVoiceChannel(t, s, handler, admin, "briefing", map[string]any{"user_ids": []string{insiderID}})

	send := func(string, any) {}
	if err := s.voice.Join(insiderID, "insider-tab", channel.ID, 4, send); err != nil {
		t.Fatal(err)
	}
	// A dropped socket detaches the connection instead of ending it, and the tab can
	// come back with its token.
	s.voice.Detach(insiderID, "insider-tab")
	token := s.voice.ResumeToken(insiderID, "insider-tab")
	if token == "" {
		t.Fatal("a detached connection should hold a resume token")
	}
	if _, err := s.voice.Resume(insiderID, token, "insider-tab", send); err != nil {
		t.Fatalf("resume before the revocation: %v", err)
	}

	// The grant is withdrawn while the tab is away.
	s.voice.Detach(insiderID, "insider-tab")
	token = s.voice.ResumeToken(insiderID, "insider-tab")
	if w := request(t, handler, "PUT", "/api/channels/"+channel.ID+"/access", map[string]any{"visibility": "private"}, admin); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	for _, state := range s.voice.States() {
		if state.UserID == insiderID {
			t.Fatalf("a detached media connection survived the revocation: %+v", state)
		}
	}
	if _, err := s.voice.Resume(insiderID, token, "insider-tab", send); err == nil {
		t.Fatal("a detached connection resumed into a channel it may no longer read")
	}
}

// TestRevocationReachesAnActiveVoiceStream is the T-AUTH-02 scenario for media:
// when the grant that admitted an account is withdrawn, the live stream ends with
// it. Waiting for the next request would leave the connection listening.
func TestRevocationReachesAnActiveVoiceStream(t *testing.T) {
	s, handler := restrictedServer(t)
	admin := setupAdmin(t, s, handler)
	insiderID, insider := memberAccount(t, s, "insider")
	var moderatorRole string
	if err := s.store.QueryRow(`SELECT id FROM roles WHERE role_key='moderator'`).Scan(&moderatorRole); err != nil {
		t.Fatal(err)
	}
	channel := privateVoiceChannel(t, s, handler, admin, "briefing", map[string]any{"role_ids": []string{moderatorRole}})
	roles := map[string]any{"role_ids": []string{db.MemberRoleID, moderatorRole}}
	if w := request(t, handler, "PUT", "/api/admin/users/"+insiderID+"/roles", roles, admin); w.Code != 200 {
		t.Fatalf("role grant: %d %s", w.Code, w.Body.String())
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insiderConn := voiceSocket(t, s, server, insider)
	if err := s.voice.Join(insiderID, "insider-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	if len(expectVoiceUpdate(t, insiderConn, channel.ID).States) != 1 {
		t.Fatal("the stream was not established")
	}

	// The role is withdrawn while the account is connected to the voice channel.
	if w := request(t, handler, "PUT", "/api/admin/users/"+insiderID+"/roles", map[string]any{"role_ids": []string{db.MemberRoleID}}, admin); w.Code != 200 {
		t.Fatalf("role revocation: %d %s", w.Code, w.Body.String())
	}
	for _, state := range s.voice.States() {
		if state.UserID == insiderID {
			t.Fatalf("the voice stream outlived the revocation: %+v", state)
		}
	}
	var revoked map[string]string
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, insiderConn, &event); err != nil {
			t.Fatalf("no explanation arrived for the ended stream: %v", err)
		}
		if event.Type != "voice.access_revoked" {
			continue
		}
		if err := json.Unmarshal(event.Data, &revoked); err != nil {
			t.Fatal(err)
		}
		break
	}
	if revoked["channel_id"] != channel.ID || revoked["reason"] == "" {
		t.Fatalf("revocation notice = %+v", revoked)
	}
	// The account cannot re-enter or read the channel afterwards.
	if err := wsjson.Write(ctx, insiderConn, map[string]any{"t": "voice.join", "d": map[string]string{"channel_id": channel.ID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, insiderConn); message != "Voice room not found." {
		t.Fatalf("join after revocation = %q", message)
	}
	if w := request(t, handler, "GET", "/api/channels/"+channel.ID+"/messages", nil, insider); w.Code != http.StatusNotFound {
		t.Fatalf("revoked member reached history: %d", w.Code)
	}
	// The revocation that ended a stream is evidence, so it is recorded.
	var recorded int
	if err := s.store.QueryRow(`SELECT count(*) FROM audit_logs WHERE action='voice.access_revoked' AND target_id=?`, insiderID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 1 {
		t.Fatalf("audit rows for the ended stream = %d", recorded)
	}
}
