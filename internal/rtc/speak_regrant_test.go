package rtc

import "testing"

// The sender keeps transmitting RTP while permission is withdrawn. Regrant must
// restore forwarding without waiting for another voice.state message, while all
// independently requested or moderator-imposed restrictions remain effective.
func TestSpeakRegrantRestoresRTPWithoutClearingOtherRestrictions(t *testing.T) {
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	source := connectPeer(t, sfu, "speaker", "room")
	destination := connectPeer(t, sfu, "listener", "room")
	sfu.SetState("speaker", "speaker", false, false, true)
	awaitPacket(t, source, destination, []byte{0xf8, 0xff, 0xfe, 1}, true)
	sfu.SetSpeakAllowed("speaker", "speaker", false)
	// A non-cooperative client continues to request transmission while denied;
	// its request is remembered but cannot override the server's permission.
	sfu.SetState("speaker", "speaker", false, false, true)
	sfu.SetSpeakAllowed("speaker", "wrong-tab", true)
	awaitPacket(t, source, destination, []byte{0xf8, 0xff, 0xfe, 2}, false)
	sfu.SetSpeakAllowed("speaker", "speaker", true)
	awaitPacket(t, source, destination, []byte{0xf8, 0xff, 0xfe, 3}, true)

	for i, tc := range []struct {
		name                                       string
		muted, deafened, adminMuted, adminDeafened bool
	}{
		{"self muted", true, false, false, false},
		{"self deafened", false, true, false, false},
		{"moderator muted", false, false, true, false},
		{"moderator deafened", false, false, false, true},
		{"allowed again", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sfu.SetState("speaker", "speaker", tc.muted, tc.deafened, true)
			sfu.Moderate("speaker", &tc.adminMuted, &tc.adminDeafened)
			sfu.SetSpeakAllowed("speaker", "speaker", false)
			// Preserve the latest deliberate setting even when it changes during
			// permission denial. Regrant itself receives no client state.
			sfu.SetState("speaker", "speaker", tc.muted, tc.deafened, true)
			sfu.SetSpeakAllowed("speaker", "speaker", true)
			allowed := !tc.muted && !tc.deafened && !tc.adminMuted && !tc.adminDeafened
			awaitPacket(t, source, destination, []byte{0xf8, 0xff, 0xfe, byte(4 + i)}, allowed)
			for _, state := range sfu.States() {
				if state.UserID == "speaker" && (state.Muted != tc.muted || state.Deafened != tc.deafened || state.AdminMuted != tc.adminMuted || state.AdminDeafened != tc.adminDeafened || state.Speaking != allowed) {
					t.Fatalf("regrant lost intent or moderation: %+v", state)
				}
			}
		})
	}
}
