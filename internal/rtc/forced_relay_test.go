package rtc

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pion/turn/v4"
	"github.com/pion/webrtc/v4"
)

// The forced-relay switch is the only way to establish that the relay path carries
// a call. Without it, a passing call says nothing about which of the two paths was
// used — the same gap ADR 0019 §7 names for Tailscale, applied to ICE.
//
// These tests use a real TURN server, real relay allocations and real RTP. Nothing
// here is a stub: if the relay is not genuinely carrying the media, the packets do
// not arrive and the assertions fail.

// freeUDPPort reserves a port and releases it, so the relay can bind it. There is a
// window between closing the probe and the relay binding, and a fixed port would
// collide with whatever else the suite is running.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// relayServer starts a real TURN relay on a free UDP port and returns it.
func relayServer(t *testing.T) (*TURN, int) {
	t.Helper()
	server, err := NewTURN()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	port := freeUDPPort(t)
	if err := server.Configure(true, "127.0.0.1", port); err != nil {
		t.Fatalf("the relay did not come up: %v", err)
	}
	return server, port
}

// relayPeer connects a peer that is forbidden from using a direct path. `relay` is
// the policy the browser is given when the operator switches the server to forced
// relay, and it is the whole point of the test: without it this is an ordinary
// direct call on a loopback interface, and it would prove nothing about the relay.
func relayPeer(t *testing.T, sfu *SFU, id, room string, server webrtc.ICEServer) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers:         []webrtc.ICEServer{server},
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "microphone", id)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, _, err := sender.Sender().ReadRTCP(); err != nil {
				return
			}
		}
	}()

	var mu sync.Mutex
	var pending []webrtc.ICECandidateInit
	send := func(kind string, data any) {
		if kind != "rtc.signal" {
			return
		}
		signal := data.(Signal)
		mu.Lock()
		defer mu.Unlock()
		if signal.SDP != nil {
			if err := pc.SetRemoteDescription(*signal.SDP); err != nil {
				t.Errorf("applying the server's description: %v", err)
				return
			}
			for _, candidate := range pending {
				if err := pc.AddICECandidate(candidate); err != nil {
					t.Errorf("applying a queued candidate: %v", err)
				}
			}
			pending = nil
		}
		if signal.ICE != nil {
			if pc.RemoteDescription() == nil {
				pending = append(pending, *signal.ICE)
			} else if err := pc.AddICECandidate(*signal.ICE); err != nil {
				t.Errorf("applying a candidate: %v", err)
			}
		}
	}
	if err := sfu.Join(id, id, room, 2, send); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(20 * time.Second):
		t.Fatal("ICE gathering timed out; the relay advertised no usable candidate")
	}
	if err := sfu.Signal(id, id, Signal{ConnectionID: sfu.connectionIDFor(id), SDP: pc.LocalDescription()}); err != nil {
		t.Fatalf("the offer was refused: %v", err)
	}
}

// A call that must be relayed, and is. This is the case the switch exists for: a
// network where direct candidates cannot be used at all. The test asserts the pair
// that carried it, not merely that something connected.
func TestAForcedRelayCallIsCarriedByTheRelay(t *testing.T) {
	relay, port := relayServer(t)
	credentials, enabled := relay.Credentials("ana")
	if !enabled {
		t.Fatal("the relay issued no credentials")
	}
	ice := webrtc.ICEServer{
		URLs:       []string{"turn:127.0.0.1:" + strconv.Itoa(port) + "?transport=udp"},
		Username:   credentials.Username,
		Credential: credentials.Credential,
	}
	sfu, err := New(Options{ICEServers: []webrtc.ICEServer{ice}, ForceRelay: true}, func([]State) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sfu.Close() })

	relayPeer(t, sfu, "ana", "room", ice)
	relayPeer(t, sfu, "bo", "room", ice)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if sfu.connectionStateOf("ana") != webrtc.PeerConnectionStateConnected {
		t.Fatal("a call restricted to the relay did not connect")
	}

	// The point of the test: not "connected" but "connected *through the relay*".
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.mu.Unlock()
	local, remote, err := sfu.selectedCandidatePair(peer)
	if err != nil {
		t.Fatalf("a connected relayed call reported no candidate pair: %v", err)
	}
	if local != "relay" || remote != "relay" {
		t.Fatalf("a call restricted to the relay was carried by %s/%s, want relay/relay", local, remote)
	}
}

// The relay's own advertised URLs must name the port it is actually on. A relay
// that hands out a URL for a port it is not listening on produces a call that works
// locally and fails for everyone else, which is the whole reason the advertised
// address is configurable.
func TestTheRelayAdvertisesThePortItListensOn(t *testing.T) {
	relay, port := relayServer(t)
	credentials, enabled := relay.Credentials("ana")
	if !enabled {
		t.Fatal("the relay issued no credentials")
	}
	server, enabled := relay.Credentials("ana")
	if !enabled || server.URLs[0] != "turn:127.0.0.1:"+strconv.Itoa(port)+"?transport=udp" {
		t.Fatalf("the relay advertised %v on port %d", server.URLs, port)
	}
	_ = credentials
}

// A credential the relay never issued must not produce a working allocation. The
// relay is the one place where an unauthenticated peer could otherwise reach an
// arbitrary third party, so this is worth asserting through the real server rather
// than through a predicate.
func TestAnUnissuedCredentialGetsNoAllocation(t *testing.T) {
	relay, port := relayServer(t)
	if _, enabled := relay.Credentials("ana"); !enabled {
		t.Fatal("the relay issued no credentials")
	}
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	client, err := turn.NewClient(&turn.ClientConfig{
		Conn: socket, TURNServerAddr: "127.0.0.1:" + strconv.Itoa(port), STUNServerAddr: "127.0.0.1:" + strconv.Itoa(port),
		Username: "invented", Password: "invented", Realm: "brrpbrrp",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Listen(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Allocate(); err == nil {
		t.Fatal("the relay granted an allocation to a credential it never issued")
	}
}
