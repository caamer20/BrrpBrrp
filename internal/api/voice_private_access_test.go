package api

import (
	"net/http/httptest"
	"testing"
)

// V14-07, private call access.
//
// Three facts, each reproduced before it was fixed. They share a cause: the call path
// and the message path asked different questions about the same relationship, and a
// revalidation pass asked a third.
//
// A blocked direct pair must not be able to open a call. The message is correctly
// refused; the call was admitted.: a blocked direct pair must not be able to start a call.
// A message POST is correctly refused; the call is not.
func TestABlockedDirectPairCannotOpenACall(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, _, bob, _ := privateCallPair(t, s, handler)

	// Block through the real route.
	if w := request(t, handler, "PUT", "/api/me/blocks/"+memberIDOf(t, s, "call_alice"), map[string]any{}, bob); w.Code >= 300 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	// The message is refused. This is the control that says the block works at all.
	posted := request(t, handler, "POST", "/api/channels/"+directID+"/messages",
		map[string]any{"content": "still here?", "idempotency_key": "blocked-1"}, bob)
	t.Logf("message after block -> %d", posted.Code)
	if posted.Code != 403 {
		t.Fatalf("a blocked pair could still message: %d", posted.Code)
	}
	// The call?
	_, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, bob), directID)
	t.Logf("call after block -> %q", refusal)
	if refusal == "" {
		t.Errorf("A BLOCKED DIRECT PAIR JOINED A CALL")
	}
}

// Revalidation must not evict a still-authorized private participant.: revalidating a still-authorized private participant must
// not evict them. The enforcement pass looks the room up as a community channel, so a
// direct conversation has no channel row and every member is dropped.
func TestRevalidationKeepsAnAuthorizedPrivateParticipant(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, bob, _ := privateCallPair(t, s, handler)

	server := httptest.NewServer(handler)
	defer server.Close()
	aliceConn := voiceSocket(t, s, server, alice)
	joinVoiceSocket(t, aliceConn, directID)
	bobConn := voiceSocket(t, s, server, bob)
	joinVoiceSocket(t, bobConn, directID)
	t.Logf("before revalidation: %d participants", len(s.voice.States()))

	// An unrelated authority change somewhere else, which triggers a revalidation pass.
	s.enforceVoiceAccess(t.Context(), "probe", "an unrelated change")

	after := s.voice.States()
	t.Logf("after revalidation: %d participants", len(after))
	for _, state := range after {
		t.Logf("  remaining: %s in %s", state.UserID, state.ChannelID)
	}
	if len(after) == 0 {
		t.Errorf("AN UNAUTHORIZED AUTHORITY CHANGE EVICTED BOTH AUTHORIZED PRIVATE PARTICIPANTS")
	}
}

// A block that lands *during* a call ends it. Admission and revalidation are the same
// question asked at two moments; checking only the first means a block placed while two
// people are already talking does nothing, and the "the block works" claim holds only
// for people who have not already joined.
func TestABlockAppliedDuringACallEndsIt(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	directID, alice, bob, _ := privateCallPair(t, s, handler)
	server := httptest.NewServer(handler)
	defer server.Close()
	aliceConn := voiceSocket(t, s, server, alice)
	joinVoiceSocket(t, aliceConn, directID)
	bobConn := voiceSocket(t, s, server, bob)
	joinVoiceSocket(t, bobConn, directID)
	if len(s.voice.States()) != 2 {
		t.Fatalf("expected two participants, got %d", len(s.voice.States()))
	}

	// Bob blocks Alice, then the ordinary revalidation pass runs — the same pass every
	// authority change runs.
	if w := request(t, handler, "PUT", "/api/me/blocks/"+memberIDOf(t, s, "call_alice"), map[string]any{}, bob); w.Code >= 300 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}
	s.enforceVoiceAccess(t.Context(), "probe", "a block was applied")
	after := s.voice.States()
	t.Logf("after the block: %d participants", len(after))
	if len(after) != 0 {
		t.Errorf("A BLOCK APPLIED DURING A CALL DID NOT END IT: %d still connected", len(after))
	}
	_ = directID
}
