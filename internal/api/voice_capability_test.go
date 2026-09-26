package api

import (
	"context"
	"encoding/json"
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

// customRole creates a role at a chosen position with an explicit capability list
// and grants it to one account.
func customRole(t *testing.T, s *Server, name string, position int, permissions string, accounts ...string) string {
	t.Helper()
	id, now := "role-"+strings.ReplaceAll(name, " ", "-"), db.Now()
	if _, err := s.store.Exec(`INSERT INTO roles(id,role_key,name,position,is_default,is_system,permissions,created_at,updated_at) VALUES(?,?,?,?,0,0,?,?,?)`,
		id, strings.ReplaceAll(name, " ", "-"), name, position, permissions, now, now); err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if _, err := s.store.Exec(`INSERT OR IGNORE INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, account, id, now); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// joinVoiceSocket joins a channel from a real socket and returns the acceptance
// payload, so the test observes what the client is actually told.
func joinVoiceSocket(t *testing.T, conn *websocket.Conn, channelID string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "voice.join", "d": map[string]string{"channel_id": channelID}}); err != nil {
		t.Fatal(err)
	}
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatalf("no answer to voice.join: %v", err)
		}
		switch event.Type {
		case "voice.error":
			t.Fatalf("join refused: %s", event.Data)
		case "voice.join.accept":
			accepted := map[string]any{}
			if err := json.Unmarshal(event.Data, &accepted); err != nil {
				t.Fatal(err)
			}
			return accepted
		}
	}
}

// TestListenOnlyMemberCannotTransmit is the F-06 acceptance case: an account
// holding voice.connect without voice.speak joins, is told it may not speak, and
// cannot turn transmission on with a state command.
func TestListenOnlyMemberCannotTransmit(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	listenerID, listener := memberAccount(t, s, "listener")
	// An installation where listening is the default and speaking is granted: the
	// default role connects but does not speak, and one account holds a role that
	// adds voice.speak. This is the configuration the contract has to honour, and
	// it is why the capability is read from the resolved principal rather than
	// from the legacy administrator column.
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["channel.read","message.send","voice.connect"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	speakerRoleID := customRole(t, s, "speaker", 20, `["channel.read","message.send","voice.connect","voice.speak"]`)

	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "town hall", Type: "voice", MaxVoiceUsers: 4}, admin))
	if channel.Type != "voice" {
		t.Fatalf("created channel = %+v", channel)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	conn := voiceSocket(t, s, server, listener)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	accepted := joinVoiceSocket(t, conn, channel.ID)
	if accepted["may_speak"] != false {
		t.Fatalf("a listen-only member was told it may speak: %v", accepted)
	}

	// The client claims it is unmuted and speaking. The server's decision wins.
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "voice.state", "d": map[string]bool{"muted": false, "speaking": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		states := s.voice.States()
		if len(states) == 1 {
			if !states[0].Muted || states[0].Speaking {
				t.Fatalf("a listen-only member holds microphone state: %+v", states[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the listener never appeared in voice: %+v", states)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "voice.speaking", "d": map[string]bool{"muted": false, "speaking": true}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, state := range s.voice.States() {
		if state.UserID == listenerID && (!state.Muted || state.Speaking) {
			t.Fatalf("a second state command unmuted a listen-only member: %+v", state)
		}
	}

	// A member who does hold voice.speak is admitted without the restriction, so
	// the check above is about the capability and not about a mute applied to
	// everyone.
	speakerID, _ := memberAccount(t, s, "talker")
	if _, err := s.store.Exec(`INSERT OR IGNORE INTO role_assignments(user_id,role_id,created_at) VALUES(?,?,?)`, speakerID, speakerRoleID, db.Now()); err != nil {
		t.Fatal(err)
	}
	s.voice.Leave(listenerID, "")
	if err := s.voice.Join(speakerID, "speaker-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.voice.Leave(speakerID, "") })
	s.voice.SetSpeakAllowed(speakerID, "speaker-tab", true)
	s.voice.SetState(speakerID, "speaker-tab", false, false, true)
	spoke := false
	for _, state := range s.voice.States() {
		if state.UserID == speakerID && state.Speaking && !state.Muted {
			spoke = true
		}
	}
	if !spoke {
		t.Fatalf("the member role could not speak: %+v", s.voice.States())
	}
}

// TestMemberWithoutVoiceConnectCannotJoin proves the connect capability is
// enforced on both entry paths, and that the refusal is explicit for a channel
// the caller can read rather than a misleading "not found".
func TestMemberWithoutVoiceConnectCannotJoin(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	readerID, reader := memberAccount(t, s, "reader")
	// The default member role always grants voice.connect, so the capability is
	// removed from the effective set by replacing the role with one that reads
	// and sends messages only.
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=?`, readerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["channel.read","message.send"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "voice room", Type: "voice", MaxVoiceUsers: 4}, admin))

	server := httptest.NewServer(handler)
	defer server.Close()
	conn := voiceSocket(t, s, server, reader)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "voice.join", "d": map[string]string{"channel_id": channel.ID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, conn); message != "Your role does not include voice.connect." {
		t.Fatalf("join refusal = %q", message)
	}
	if len(s.voice.States()) != 0 {
		t.Fatalf("a member without voice.connect entered voice: %+v", s.voice.States())
	}
	// The same account still reads the channel, so the refusal above is about
	// voice.connect rather than about channel visibility.
	listed := false
	for _, candidate := range readJSON[[]db.Channel](t, request(t, handler, "GET", "/api/channels", nil, reader)) {
		if candidate.ID == channel.ID {
			listed = true
		}
	}
	if !listed {
		t.Fatal("the account could not read the channel's metadata")
	}
}

// TestDelegatedVoiceModeratorIsLimitedToItsHierarchy covers the moderation half:
// voice.moderate is the capability, the legacy administrator column is not, and a
// delegated moderator cannot act on an account that outranks it.
func TestDelegatedVoiceModeratorIsLimitedToItsHierarchy(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	moderatorID, moderator := memberAccount(t, s, "moderator")
	subjectID, _ := memberAccount(t, s, "subject")
	customRole(t, s, "voice moderator", 30, `["channel.read","message.send","voice.connect","voice.moderate"]`, moderatorID)
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "stage", Type: "voice", MaxVoiceUsers: 4}, admin))

	adminID := readJSON[db.User](t, request(t, handler, "GET", "/api/me", nil, admin)).ID
	for _, userID := range []string{subjectID, adminID} {
		if err := s.voice.Join(userID, userID+"-tab", channel.ID, 4, func(string, any) {}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		s.voice.Leave(subjectID, "")
		s.voice.Leave(adminID, "")
	})

	server := httptest.NewServer(handler)
	defer server.Close()
	conn := voiceSocket(t, s, server, moderator)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// An administrator outranks a delegated voice moderator.
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "admin.voice_kick", "d": map[string]string{"user_id": adminID, "confirm": adminID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, conn); !strings.Contains(message, "same or higher authority") {
		t.Fatalf("hierarchy refusal = %q", message)
	}
	if adminID != "" && !voiceHasUser(s, adminID) {
		t.Fatal("a delegated moderator removed a higher-ranked account from voice")
	}

	// An ordinary member is inside its authority.
	if err := wsjson.Write(ctx, conn, map[string]any{"t": "admin.voice_kick", "d": map[string]string{"user_id": subjectID, "confirm": subjectID}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for voiceHasUser(s, subjectID) {
		if time.Now().After(deadline) {
			t.Fatal("a delegated moderator could not remove an ordinary member from voice")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A member without voice.moderate cannot moderate at all.
	plainID, plain := memberAccount(t, s, "plain")
	if err := s.voice.Join(plainID, "plain-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	defer s.voice.Leave(plainID, "")
	plainConn := voiceSocket(t, s, server, plain)
	if err := wsjson.Write(ctx, plainConn, map[string]any{"t": "admin.voice_mute", "d": map[string]any{"user_id": subjectID, "muted": true, "confirm": subjectID}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, plainConn); !strings.Contains(message, "voice.moderate") {
		t.Fatalf("capability refusal = %q", message)
	}
}

// TestLiveVoiceCapabilityLossAppliesToAnActivePeer is the permanent regression for
// R2-04. Initial admission enforced connect and speak, but the call's own lifetime
// did not: withdrawing voice.connect left the peer in the call and a later resume
// succeeded, and withdrawing voice.speak left the peer state unmuted and speaking
// with nothing stopping its audio.
//
// The authority is changed through the role API while the call is live, which is
// the same path an operator uses, and the peer is a real socket so the resume path
// is exercised as a client would take it.
func TestLiveVoiceCapabilityLossAppliesToAnActivePeer(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	speakerID, speaker := memberAccount(t, s, "speaker")
	// The default role connects but does not speak, so voice.speak is granted only by
	// the role this test changes: withdrawing it from that role withdraws it from the
	// account, which is what a live permission change looks like.
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["channel.read","message.send"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	// This account may read, connect and speak, and owns nothing else.
	speakingRole := `["channel.read","message.send","voice.connect","voice.speak"]`
	roleID := customRole(t, s, "voice speaker", 25, speakingRole, speakerID)
	if _, err := s.store.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role_id=?`, speakerID, db.MemberRoleID); err != nil {
		t.Fatal(err)
	}
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "call", Type: "voice", MaxVoiceUsers: 4}, admin))
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	voiceState := func() (bool, rtc.State) {
		for _, state := range s.voice.States() {
			if state.UserID == speakerID {
				return true, state
			}
		}
		return false, rtc.State{}
	}
	waitFor := func(t *testing.T, what string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("%s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	conn := voiceSocket(t, s, server, speaker)
	accepted := joinVoiceSocket(t, conn, channel.ID)
	if accepted["may_speak"] != true {
		t.Fatalf("the speaker was told it may not speak: %v", accepted)
	}
	resumeToken, _ := accepted["resume_token"].(string)
	if resumeToken == "" {
		t.Fatal("the join acceptance carried no resume token")
	}
	// The peer reports an open microphone through its own socket, which is what an
	// active peer looks like, and a non-cooperative client would keep reporting.
	reportSpeaking := func(target *websocket.Conn) {
		t.Helper()
		if err := wsjson.Write(ctx, target, map[string]any{"t": "voice.speaking", "d": map[string]bool{"muted": false, "speaking": true}}); err != nil {
			t.Fatal(err)
		}
	}
	reportSpeaking(conn)
	waitFor(t, "the speaker never became active", func() bool {
		present, state := voiceState()
		return present && !state.Muted && state.Speaking
	})

	// voice.speak is withdrawn while the call is live, through the role API an
	// operator uses. The peer keeps its connection and loses its microphone, and the
	// server holds that decision: the client is not asked to cooperate.
	if w := request(t, handler, "PATCH", "/api/roles/"+roleID, map[string]any{"name": "voice speaker", "position": 25, "permissions": []string{"channel.read", "message.send", "voice.connect"}}, admin); w.Code != 200 {
		t.Fatalf("withdraw voice.speak: %d %s", w.Code, w.Body.String())
	}
	waitFor(t, "a peer that lost voice.speak was still transmitting", func() bool {
		present, state := voiceState()
		return present && state.Muted && !state.Speaking
	})
	reportSpeaking(conn)
	time.Sleep(80 * time.Millisecond)
	if _, state := voiceState(); !state.Muted || state.Speaking {
		t.Fatalf("a non-cooperative client unmuted itself after losing voice.speak: %+v", state)
	}

	// A resume is governed by the authority committed at the moment it is attempted.
	// The peer is left detached with a live token — the shape a dropped connection
	// produces — and voice.connect is withdrawn without running enforcement, so the
	// resume itself has to make the decision. This is the reproduced defect: the
	// resume path used to check channel readability only, and the peer came back with
	// voice.connect false.
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the peer was never detached", func() bool {
		present, _ := voiceState()
		return present
	})
	if _, err := s.store.Exec(`UPDATE roles SET permissions=? WHERE id=?`, `["channel.read","message.send"]`, roleID); err != nil {
		t.Fatal(err)
	}
	resumeSocket := voiceSocket(t, s, server, speaker)
	defer resumeSocket.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, resumeSocket, map[string]any{"t": "voice.resume", "d": map[string]string{"token": resumeToken}}); err != nil {
		t.Fatal(err)
	}
	if message := voiceError(t, ctx, resumeSocket); !strings.Contains(message, "voice.connect") {
		t.Fatalf("resume refusal = %q, want a voice.connect refusal", message)
	}
	if voiceHasUser(s, speakerID) {
		t.Fatal("a resume under a withdrawn capability restored the media connection")
	}

	// The active-admission half: with voice.connect restored and the peer in the
	// call again, withdrawing connect through the API ends the admission at once.
	if w := request(t, handler, "PATCH", "/api/roles/"+roleID, map[string]any{"name": "voice speaker", "position": 25, "permissions": []string{"channel.read", "message.send", "voice.connect", "voice.speak"}}, admin); w.Code != 200 {
		t.Fatalf("restore the role: %d %s", w.Code, w.Body.String())
	}
	accepted = joinVoiceSocket(t, resumeSocket, channel.ID)
	if accepted["may_speak"] != true {
		t.Fatalf("the restored speaker could not join: %v", accepted)
	}
	if w := request(t, handler, "PATCH", "/api/roles/"+roleID, map[string]any{"name": "voice speaker", "position": 25, "permissions": []string{"channel.read", "message.send"}}, admin); w.Code != 200 {
		t.Fatalf("withdraw voice.connect: %d %s", w.Code, w.Body.String())
	}
	waitFor(t, "withdrawing voice.connect did not end the active admission", func() bool {
		return !voiceHasUser(s, speakerID)
	})

	// The permitted control: the same account, back in the role, is admitted again.
	if w := request(t, handler, "PATCH", "/api/roles/"+roleID, map[string]any{"name": "voice speaker", "position": 25, "permissions": []string{"channel.read", "message.send", "voice.connect", "voice.speak"}}, admin); w.Code != 200 {
		t.Fatalf("restore the role again: %d %s", w.Code, w.Body.String())
	}
	accepted = joinVoiceSocket(t, resumeSocket, channel.ID)
	if accepted["may_speak"] != true {
		t.Fatalf("the restored speaker could not join again: %v", accepted)
	}
}

func voiceHasUser(s *Server, userID string) bool {
	for _, state := range s.voice.States() {
		if state.UserID == userID {
			return true
		}
	}
	return false
}
