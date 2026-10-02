package rtc

import "testing"

func TestTransferKeepsConnectionAndEnforcesRoomIsolation(t *testing.T) {
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	alice := connectPeer(t, sfu, "alice", "lounge")
	bob := connectPeer(t, sfu, "bob", "lounge")
	carol := connectPeer(t, sfu, "carol", "games")
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 1}, true)
	sfu.mu.Lock()
	original := sfu.peers["alice"]
	sfu.mu.Unlock()
	if err := sfu.Move("alice", "other-tab", "games", 2); err == nil {
		t.Fatal("another tab moved a user")
	}
	if err := sfu.Move("alice", "alice", "games", 1); err == nil {
		t.Fatal("full room accepted a transfer")
	}
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 2}, true)
	if err := sfu.Move("alice", "alice", "games", 2); err != nil {
		t.Fatal(err)
	}
	sfu.mu.Lock()
	same := sfu.peers["alice"] == original
	sfu.mu.Unlock()
	if !same {
		t.Fatal("room move recreated the connection")
	}
	awaitPacket(t, alice, carol, []byte{0xf8, 0xff, 0xfe, 3}, true)
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 4}, false)
	awaitPacket(t, bob, alice, []byte{0xf8, 0xff, 0xfe, 5}, false)
}

// A person who closes or reloads the tab during a call and comes straight back can
// rejoin. The old connection's peer is detached — nobody can reach it without its
// resume token — but it held the account's voice place for the whole 30-second drain,
// and the new join was answered "you are already in voice, possibly in another tab":
// false, unactionable, and exactly the state a reload produces (found while chasing
// the browser journeys' intermittent join, review 17 handoff).
func TestADetachedPeerDoesNotHoldTheVoicePlace(t *testing.T) {
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	if err := sfu.Join("alice", "first-tab", "room", 2, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	detachedToken := sfu.ResumeToken("alice", "first-tab")
	if detachedToken == "" {
		t.Fatal("the peer has no resume token")
	}
	// The tab goes away without a leave, and the transport cleanup detaches the peer.
	sfu.Detach("alice", "first-tab")

	// The same account joins again: admitted, in the same room, once.
	if err := sfu.Join("alice", "second-tab", "room", 2, func(string, any) {}); err != nil {
		t.Fatalf("a detached peer refused the account's next join: %v", err)
	}
	states := sfu.States()
	if len(states) != 1 || states[0].UserID != "alice" || states[0].ChannelID != "room" {
		t.Fatalf("the rejoin left %+v", states)
	}
	// And the replaced peer is gone rather than parked: the token it held cannot bring
	// back a second voice place for the same account.
	if _, err := sfu.Resume("alice", detachedToken, "third-tab", func(string, any) {}); err == nil {
		t.Fatal("the replaced peer was resumed as a second place")
	}

	// The control: a peer that still has its signaling connection is still refused,
	// because two live tabs sharing one account's voice place is the confusion the
	// refusal exists for.
	if err := sfu.Join("alice", "fourth-tab", "room", 2, func(string, any) {}); err == nil {
		t.Fatal("a second live connection took a voice place that was already held")
	}
}

func TestResumeOwnershipTokenRotationAndStaleSignaling(t *testing.T) {
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	if err := sfu.Join("alice", "first-tab", "room", 2, func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	token := sfu.ResumeToken("alice", "first-tab")
	connection := sfu.ConnectionID("alice", "first-tab")
	sfu.Detach("alice", "first-tab")
	if _, err := sfu.Resume("bob", token, "second-tab", func(string, any) {}); err == nil {
		t.Fatal("another account resumed a peer")
	}
	if _, err := sfu.Resume("alice", "wrong-token", "second-tab", func(string, any) {}); err == nil {
		t.Fatal("wrong token resumed a peer")
	}
	if _, err := sfu.Resume("alice", token, "second-tab", func(string, any) {}); err != nil {
		t.Fatal(err)
	}
	if sfu.ResumeToken("alice", "second-tab") == token {
		t.Fatal("resume token not rotated")
	}
	if sfu.ConnectionID("alice", "second-tab") != connection {
		t.Fatal("resume changed the media connection")
	}
	sfu.Leave("alice", "first-tab")
	sfu.Detach("alice", "first-tab")
	if len(sfu.States()) != 1 {
		t.Fatal("stale tab removed resumed peer")
	}
	if _, err := sfu.Resume("alice", token, "third-tab", func(string, any) {}); err == nil {
		t.Fatal("reused resume token accepted")
	}
	if err := sfu.Signal("alice", "second-tab", Signal{ConnectionID: "previous-call"}); err != nil {
		t.Fatal("stale signal was not ignored")
	}
}
