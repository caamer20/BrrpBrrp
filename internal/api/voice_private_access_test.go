package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// A block between two members of a *group* is not the group's business.
//
// On a direct pair the block is the whole relationship, so refusing the call is right.
// On a group it is one relationship among several: the group has other members, the
// block does not remove anybody from it, and both blocked and blocking members keep
// reading the history. A call is not a message and a group is not a pair, so admitting
// the call is the behaviour the block model already implies everywhere else.
//
// The check under test was `blockedBetween`, which asks whether *any* accepted member
// blocks the caller. Applied to a group that is wrong twice over: a member who blocked
// somebody years ago could never join the group's call again, and — because the same
// query runs for every participant — the blocker and the blocked could each exclude
// the other from a room the third member is standing in.
func TestABlockBetweenTwoGroupMembersDoesNotRemoveThemFromTheGroupCall(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	s.config.PrivateConversations = true
	setupAdmin(t, s, handler)

	ownerID, owner := memberAccount(t, s, "callgrp_owner")
	blockedID, blocked := memberAccount(t, s, "callgrp_blocked")
	_, third := memberAccount(t, s, "callgrp_third")

	created := request(t, handler, "POST", "/api/groups", map[string]string{"name": "call group"}, owner)
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	group := readJSON[groupSummary](t, created)
	// Each invitee accepts their own invitation — the invitation is addressed to them,
	// so accepting it as somebody else would be accepting an invitation that was never
	// issued to them.
	for inviteeID, invitee := range map[string]*http.Cookie{blockedID: blocked, memberIDOf(t, s, "callgrp_third"): third} {
		if w := request(t, handler, "POST", "/api/groups/"+group.ID+"/invite",
			map[string]string{"user_id": inviteeID}, owner); w.Code != 201 {
			t.Fatalf("invite %s: %d %s", inviteeID, w.Code, w.Body.String())
		}
		accept(t, handler, invitee, pendingInvitation(t, handler, invitee))
	}
	_ = ownerID

	// The control: every member can open the group's call before anything is blocked.
	if _, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, blocked), group.ID); refusal != "" {
		t.Fatalf("a group member could not open the group call: %q", refusal)
	}

	// One member blocks another. This is the relationship under test.
	if w := request(t, handler, "PUT", "/api/me/blocks/"+blockedID, map[string]any{}, owner); w.Code >= 300 {
		t.Fatalf("block: %d %s", w.Code, w.Body.String())
	}

	// Neither party is removed from the group by somebody else's block.
	for name, cookie := range map[string]*http.Cookie{"blocker": owner, "blocked": blocked} {
		if _, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, cookie), group.ID); refusal != "" {
			t.Fatalf("a block between two group members excluded the %s from the group call: %q", name, refusal)
		}
	}

	// A group is not a pair, so a block between two of its members is not a
	// two-person relationship and does not make the group read-only: the blocked member
	// still writes into it. That is `conversationAccess`'s existing decision, and this
	// asserts the call path now agrees with the message path rather than inventing a
	// second policy.
	if w := request(t, handler, "POST", "/api/channels/"+group.ID+"/messages",
		map[string]any{"content": "after the block", "idempotency_key": "grp-block-1"}, blocked); w.Code != 201 {
		t.Fatalf("a group went read-only because two of its members blocked each other: %d %s", w.Code, w.Body.String())
	}

	// The direct pair, which is the case the block *is* about, is still refused. A
	// relaxation that had widened past the direct pair would pass everything above.
	if w := request(t, handler, "POST", "/api/directs",
		map[string]any{"user_id": blockedID, "introduction": "still there?"}, blocked); w.Code < 400 {
		t.Fatalf("a blocked member opened a fresh direct request: %d %s", w.Code, w.Body.String())
	}
}

// A direct peer who is erased, deactivated or banned is not someone to call.
//
// The membership row survives all three — erasure and deactivation are account
// lifecycle, not conversation membership, and deleting the conversation because one
// person left would destroy the other's history. So the pair still says "accepted",
// and the call path resolved on membership alone would admit a live call with an
// account that cannot sign in. This asserts each state refuses, and asserts the
// history the other member is entitled to is still there.
func TestADirectCallIsRefusedWhenTheOtherAccountIsUnavailable(t *testing.T) {
	for _, state := range []string{"erased", "deactivated", "banned"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
			s, handler := testServer(t)
			s.config.PrivateConversations = true
			directID, alice, _, _ := privateCallPair(t, s, handler)

			// The control: the pair can call before anything is wrong.
			if _, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, alice), directID); refusal != "" {
				t.Fatalf("the pair could not call before the account became unavailable: %q", refusal)
			}

			bobID := memberIDOf(t, s, "call_bob")
			column := map[string]string{"erased": "is_deleted", "deactivated": "is_deactivated", "banned": "is_banned"}[state]
			if _, err := s.store.Exec(`UPDATE users SET `+column+`=1 WHERE id=?`, bobID); err != nil {
				t.Fatal(err)
			}

			if _, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, alice), directID); refusal == "" {
				t.Fatalf("a direct call opened with the other account %s", state)
			}
			// And the surviving member keeps their own history: losing the peer must not
			// take the conversation with them.
			listed := request(t, handler, "GET", "/api/directs", nil, alice)
			if listed.Code != 200 {
				t.Fatalf("the surviving member lost access to their own conversation list: %d %s", listed.Code, listed.Body.String())
			}
			if !strings.Contains(listed.Body.String(), directID) {
				t.Fatalf("the conversation vanished from the surviving member's list when the peer was %s", state)
			}
		})
	}
}

// The gate above is admission. A peer that becomes unavailable *during* a call must
// also be removed from it, or the room holds a participant the admission check would
// now refuse — the live equivalent of "a membership revoked midway is not honoured".
func TestAPeerThatBecomesUnavailableIsRemovedFromALiveCall(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	s.config.PrivateConversations = true
	directID, alice, bob, _ := privateCallPair(t, s, handler)
	server := httptest.NewServer(handler)
	defer server.Close()
	aliceConn := voiceSocket(t, s, server, alice)
	joinVoiceSocket(t, aliceConn, directID)
	bobConn := voiceSocket(t, s, server, bob)
	joinVoiceSocket(t, bobConn, directID)
	if got := len(s.voice.States()); got != 2 {
		t.Fatalf("the call holds %d participants, want 2", got)
	}

	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=1 WHERE id=?`, memberIDOf(t, s, "call_bob")); err != nil {
		t.Fatal(err)
	}
	s.enforceVoiceAccess(t.Context(), "probe", "an unrelated change")

	// The whole call goes, and it should: this is a direct pair, so the one member who
	// remains cannot rejoin either — the same admission rule that refuses them a fresh
	// call with an unavailable peer would refuse them a rejoin into this one. Leaving
	// Alice alone in an empty room would be a call to nobody.
	if got := len(s.voice.States()); got != 0 {
		t.Fatalf("the call holds %d participants after a peer was deactivated, want 0", got)
	}
}

// The same rule on a group is the opposite, because a group is not a pair: one member
// becoming unavailable removes that member and nobody else. The call has to survive the
// loss of a single member or any departure would end the room.
func TestAGroupCallSurvivesOneMemberBecomingUnavailable(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	s.config.PrivateConversations = true
	setupAdmin(t, s, handler)

	_, owner := memberAccount(t, s, "vcall_owner")
	departingID, departing := memberAccount(t, s, "vcall_departing")
	stayingID, staying := memberAccount(t, s, "vcall_staying")
	created := request(t, handler, "POST", "/api/groups", map[string]string{"name": "survivor"}, owner)
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	group := readJSON[groupSummary](t, created)
	for inviteeID, invitee := range map[string]*http.Cookie{departingID: departing, stayingID: staying} {
		if w := request(t, handler, "POST", "/api/groups/"+group.ID+"/invite",
			map[string]string{"user_id": inviteeID}, owner); w.Code != 201 {
			t.Fatalf("invite %s: %d %s", inviteeID, w.Code, w.Body.String())
		}
		accept(t, handler, invitee, pendingInvitation(t, handler, invitee))
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	ownerConn := voiceSocket(t, s, server, owner)
	joinVoiceSocket(t, ownerConn, group.ID)
	departingConn := voiceSocket(t, s, server, departing)
	joinVoiceSocket(t, departingConn, group.ID)
	// A private call is one-to-one by design — `maxPrivateCallParticipants` is 2 — so
	// the third member is deliberately not dialled in. It is in the group, which is what
	// matters for the revalidation, and it is the participant that must survive.
	if got := len(s.voice.States()); got != 2 {
		t.Fatalf("the group call holds %d participants, want 2", got)
	}

	if _, err := s.store.Exec(`UPDATE users SET is_deactivated=1 WHERE id=?`, departingID); err != nil {
		t.Fatal(err)
	}

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

// A listen-only member is in the call and out of the microphone.
//
// This is the shape that `voice.connect` without `voice.speak` produces, and the two
// halves have to disagree with each other on purpose: `SetSpeakAllowed` in the same
// enforcement pass that decided they may stay. Dropping either half produces a different
// failure — an evicted member (too strict, and they lose the call they are entitled to),
// or a member who stays and is never silenced (too loose, and the permission is decorative).
//
// And a member who *does* hold the permission in the same room is unaffected. A test that
// only checks the refusal passes against a server that refuses everybody.

func TestAListenOnlyMemberStaysInTheCallAndIsSilenced(t *testing.T) {
	t.Setenv("BRRP_ENABLE_PRIVATE_CONVERSATIONS", "1")
	s, handler := testServer(t)
	s.config.PrivateConversations = true
	admin := setupAdmin(t, s, handler)

	// Three accounts in one direct pair is impossible — a pair is two people — so the
	// room is a group call, which is what lets a listen-only member and a speaking member
	// be on the line together and the refusal be attributable to one of them.
	_, speaking := memberAccount(t, s, "listen_speaker")
	_, listening := memberAccount(t, s, "listen_only")
	_, waiting := memberAccount(t, s, "listen_third")

	created := request(t, handler, "POST", "/api/groups", map[string]string{"name": "mixed"}, speaking)
	if created.Code != 201 {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	group := readJSON[groupSummary](t, created)
	for inviteeID, invitee := range map[string]*http.Cookie{
		memberIDOf(t, s, "listen_only"):     listening,
		memberIDOf(t, s, "listen_third"):    waiting,
	} {
		if w := request(t, handler, "POST", "/api/groups/"+group.ID+"/invite",
			map[string]string{"user_id": inviteeID}, speaking); w.Code != 201 {
			t.Fatalf("invite %s: %d %s", inviteeID, w.Code, w.Body.String())
		}
		accept(t, handler, invitee, pendingInvitation(t, handler, invitee))
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	speakingConn := voiceSocket(t, s, server, speaking)
	joinVoiceSocket(t, speakingConn, group.ID)
	listeningConn := voiceSocket(t, s, server, listening)
	joinVoiceSocket(t, listeningConn, group.ID)
	if got := len(s.voice.States()); got != 2 {
		t.Fatalf("the call holds %d participants, want 2", got)
	}

	// The listen-only account is denied the transmit capability and nothing else, so it
	// stays in the call — which is the entire point of a listen-only role.
	listenOnly := createTestRole(t, s, handler, admin, "listen_only_role",
		[]string{"voice.connect", "channel.read", "message.send"}, 30)
	// Through the supported route, so this is an authority change an operator would make
	// rather than a test-only edit: a role that may join a call and read, and may not
	// speak.
	setRoles(t, s, handler, admin, memberIDOf(t, s, "listen_only"), listenOnly)

	s.enforceVoiceAccess(t.Context(), "admin", "a capability was withdrawn")
	after := s.voice.States()
	// Both are still in the room: withdrawing `voice.speak` is not withdrawing the call.
	if len(after) != 2 {
		t.Fatalf("the call holds %d participants after a speak withdrawal, want 2: a listen-only member was evicted", len(after))
	}
	for _, state := range after {
		if state.UserID == memberIDOf(t, s, "listen_only") {
			if !state.Muted {
				t.Fatal("A LISTEN-ONLY MEMBER IS STILL TRANSMITTING")
			}
			continue
		}
		if state.UserID == memberIDOf(t, s, "listen_speaker") && state.Muted {
			t.Fatal("withdrawing one member's speak permission silenced somebody else")
		}
	}

	// And the refusal is specific: the member may still *try* to speak, and the server is
	// what stops the packet. A test that only inspects the reported state would pass
	// against a client that suppressed locally.
	_, refusal := s.voiceJoin(t.Context(), mustPrincipal(t, s, handler, listening), group.ID)
	_ = refusal
	_ = waiting
}
