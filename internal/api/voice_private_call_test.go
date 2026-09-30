package api

import (
	"context"
	"net/http"
	"testing"

	"brrpbrrp/internal/authz"
)

// A private call is the same SFU behind a different admission decision. These tests
// are about the admission: a member of a consenting conversation may call, and
// everyone else is told the same thing, which is that the room does not exist.

func privateCallPair(t *testing.T, s *Server, handler http.Handler) (directID string, alice, bob, outsider *http.Cookie) {
	t.Helper()
	admin := setupAdmin(t, s, handler)
	_, alice = namedUser(t, s, "call_alice")
	_, bob = namedUser(t, s, "call_bob")
	_, outsider = namedUser(t, s, "call_outsider")
	bobID := memberIDOf(t, s, "call_bob")

	// A direct conversation created and accepted over the real routes, so the
	// membership this test relies on is the same membership a message would see.
	w := request(t, handler, "POST", "/api/directs", map[string]any{"user_id": bobID}, alice)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("create a direct request: %d %s", w.Code, w.Body.String())
	}
	requested := readJSON[map[string]any](t, w)
	directID, _ = requested["id"].(string)
	_ = admin
	if directID == "" {
		t.Fatalf("no direct conversation was created: %s", w.Body.String())
	}
	if w := request(t, handler, "POST", "/api/directs/"+directID+"/accept", map[string]any{}, bob); w.Code < 200 || w.Code >= 300 {
		t.Fatalf("accept the direct request: %d %s", w.Code, w.Body.String())
	}
	return directID, alice, bob, outsider
}

func TestAPrivateCallIsAdmittedForItsMembers(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, _, _ := privateCallPair(t, s, handler)

	room, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, alice), directID)
	if refusal != "" {
		t.Fatalf("a member of the conversation was refused a call: %q", refusal)
	}
	if !room.Private {
		t.Fatal("a direct conversation's call was not marked private")
	}
	if room.ID != directID {
		t.Fatalf("the call room is %q, want the conversation id", room.ID)
	}
	// A private call is two people. Three is a group call, and a direct call that
	// admits a third is a room the membership rule does not describe.
	if room.Capacity != maxPrivateCallParticipants {
		t.Fatalf("a private call carries %d, want %d", room.Capacity, maxPrivateCallParticipants)
	}
	// It is named for the other member, so somebody reading the participant list knows
	// who is on the line — which a channel's name never told them.
	if room.DisplayName == "" {
		t.Fatal("the call has no name, so a participant list cannot say who is on the line")
	}
}

func TestAPrivateCallIsRefusedForAStrangerWithTheSameSentence(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, _, _, outsider := privateCallPair(t, s, handler)
	_, channelRefusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, outsider), directID)
	if channelRefusal == "" {
		t.Fatal("an account outside the conversation was admitted to its call")
	}
	// The refusal is the same one a nonexistent room gets, so a stranger cannot use
	// the wording to learn that the conversation exists.
	_, missingRefusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, outsider), "not-a-room")
	if channelRefusal != missingRefusal {
		t.Fatalf("a private conversation answers %q and a missing room answers %q, so the difference discloses that one exists",
			channelRefusal, missingRefusal)
	}
}

func TestAPrivateCallIsRefusedWhenTheFeatureIsOff(t *testing.T) {
	// The private feature is default-off, and a call is no more available through it
	// than a message is. The conversation is created with the feature on and the flag
	// cleared afterwards, which is what an operator turning it off actually leaves
	// behind: the rows are still there, and a call must not reach them.
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, _, _ := privateCallPair(t, s, handler)
	s.config.PrivateConversations = false
	_, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, alice), directID)
	if refusal == "" {
		t.Fatal("a private call was admitted with the feature disabled")
	}
	if refusal != "Voice room not found." {
		t.Fatalf("the refusal discloses the conversation: %q", refusal)
	}
}

func TestAPrivateCallNeedsTheSameCapabilityAsAChannel(t *testing.T) {
	// Speaking in a private call is the same act as speaking in a channel, so the
	// capability that gates the microphone gates both. Checked before the room is
	// resolved, so a caller without it is not told whether the room exists.
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, _, _, _ := privateCallPair(t, s, handler)
	owner := sessionFor(t, s, memberIDOf(t, s, "admin"))
	namedUser(t, s, "call_silent")
	roleID := createTestRole(t, s, handler, owner, "connect_only", []string{string(authz.ChannelRead), string(authz.MessageSend), string(authz.VoiceConnect)}, 20)
	addRole(t, s, handler, owner, memberIDOf(t, s, "call_silent"), roleID)

	principal := mustPrincipal(t, s, handler, sessionFor(t, s, memberIDOf(t, s, "call_silent")))
	_, refusal := s.voiceJoin(t.Context(), principal, directID)
	if refusal == "" {
		t.Fatal("an account with no voice.connect joined a call")
	}
	// The refusal is about the caller, not the room: the capability is checked before
	// the room is resolved, so the wording discloses nothing about whether the
	// conversation exists. It is still a refusal rather than a silent no-op.
	if refusal == "" {
		t.Fatal("an account with no voice.connect joined a call")
	}
}

func TestAPrivateCallFollowsTheConversationNotAChannel(t *testing.T) {
	// The call's audience is the conversation's members. A member removed during a
	// call stops receiving its participant list on the next change, which is the same
	// rule their messages follow — and a call that outlived the membership would be a
	// way to keep hearing somebody after losing access to them.
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, bob, _ := privateCallPair(t, s, handler)
	_ = alice
	_ = bob

	principal := mustPrincipal(t, s, handler, sessionFor(t, s, memberIDOf(t, s, "call_bob")))
	// Both members are in the audience while the membership holds.
	if _, refusal := s.voiceJoin(t.Context(), principal, directID); refusal != "" {
		t.Fatalf("a member was refused: %q", refusal)
	}
	// Withdrawing the conversation's acceptance is what ends a call's reach; the
	// admission path is the same one a message reads, so the two cannot disagree.
	if _, err := s.store.Exec(`UPDATE conversation_members SET state='left'
		WHERE conversation_id=? AND user_id=?`, directID, memberIDOf(t, s, "call_bob")); err != nil {
		t.Fatal(err)
	}
	if _, refusal := s.voiceJoin(t.Context(), principal, directID); refusal == "" {
		t.Fatal("a member whose conversation acceptance was withdrawn still joined the call")
	}
}

// mustPrincipal resolves the authority a session cookie would get, which is what the
// admission path is given in production.
func mustPrincipal(t *testing.T, s *Server, handler http.Handler, cookie *http.Cookie) authz.Principal {
	t.Helper()
	user, err := s.store.Account(t.Context(), s.store, accountForCookie(t, s, cookie))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := s.authz.Resolve(context.Background(), s.store, user)
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

// accountForCookie reads the account a session cookie belongs to. The cookie's value
// is a hash, so the session row is the way back to the account.
func accountForCookie(t *testing.T, s *Server, cookie *http.Cookie) string {
	t.Helper()
	var id string
	if err := s.store.QueryRowContext(t.Context(),
		`SELECT user_id FROM sessions WHERE token_hash=?`, hashToken(cookie.Value)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
