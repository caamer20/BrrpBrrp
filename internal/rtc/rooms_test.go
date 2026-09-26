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
