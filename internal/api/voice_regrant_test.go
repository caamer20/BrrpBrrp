package api

import (
	"net/http"
	"testing"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/ws"
)

func TestVoiceStateWhileDeniedDoesNotBecomeAnIntentionalSelfMute(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)
	// The default role is additive even when another role is assigned. Make
	// listening the baseline so the initial request truly lacks voice.speak.
	if _, err := s.store.Exec(`UPDATE roles SET permissions='["channel.read","message.send","voice.connect"]' WHERE is_default=1`); err != nil {
		t.Fatal(err)
	}
	id, cookie := onlyCapability(t, s, handler, "listener", "voice.connect")
	roleID := customRole(t, s, "speaker", 30, `["channel.read","voice.connect","voice.speak"]`)
	channel := readJSON[db.Channel](t, request(t, handler, "POST", "/api/channels", db.Channel{Name: "regrant", Type: "voice", MaxVoiceUsers: 4}, admin))
	if err := s.voice.Join(id, "owning-tab", channel.ID, 4, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.voice.Leave(id, "") })
	s.voice.SetSpeakAllowed(id, "owning-tab", false)
	user, err := s.store.Account(t.Context(), s.store, id)
	if err != nil {
		t.Fatal(err)
	}
	client := &ws.Client{ID: "owning-tab", UserID: id, Out: make(chan ws.Queued, 8)}
	s.voiceEvent(withSession(t.Context(), hashToken(cookie.Value)), client, user, ws.Event{Type: "voice.state", Data: []byte(`{"muted":false,"deafened":false,"speaking":true}`)})
	for _, state := range s.voice.States() {
		if state.UserID == id && (!state.Muted || state.Speaking) {
			t.Fatalf("denied state transmitted: %+v", state)
		}
	}
	granted := request(t, handler, "PUT", "/api/admin/users/"+id+"/roles", map[string]any{"role_ids": []string{roleID}}, admin)
	if granted.Code != http.StatusOK {
		t.Fatalf("regrant: %d %s", granted.Code, granted.Body.String())
	}
	for _, state := range s.voice.States() {
		if state.UserID == id && (state.Muted || !state.Speaking) {
			t.Fatalf("permission regrant retained forced mute instead of client intent: %+v", state)
		}
	}
}
