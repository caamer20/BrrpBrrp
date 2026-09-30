package rtc

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type testPeer struct {
	pc *webrtc.PeerConnection
	// name is the account this peer connected as, so a test can find the SFU's own
	// peer for it. The two are different types, and a test that needs both has to be
	// able to cross between them by something the SFU stores rather than by reaching
	// into its fields.
	name      string
	track     *webrtc.TrackLocalStaticRTP
	packets   chan []byte
	// The screen publication a load harness gives this peer, and the frames that come
	// back on it. Kept here rather than in the harness so the peer owns its own tracks,
	// which is what the production peer does too.
	videoTrack     *webrtc.TrackLocalStaticRTP
	videoPackets   chan []byte
	videoSequence  uint16
	videoTimestamp uint32
	joins     chan JoinEvent
	sequence  uint16
	timestamp uint32
}

func connectPeer(t *testing.T, sfu *SFU, id, room string) *testPeer {
	return connectPeerWithCapacity(t, sfu, id, room, 2)
}

func connectPeerWithCapacity(t *testing.T, sfu *SFU, id, room string, capacity int) *testPeer {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
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
	for range 9 {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
	}
	peer := &testPeer{
		pc: pc, name: id, track: track,
		packets: make(chan []byte, 64), joins: make(chan JoinEvent, 16),
		videoPackets: make(chan []byte, 4096),
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			select {
			case peer.packets <- append([]byte{}, packet.Payload...):
			default:
			}
		}
	})
	var mu sync.Mutex
	var pending []webrtc.ICECandidateInit
	signalingErrors := make(chan error, 10)
	send := func(kind string, data any) {
		if kind == "voice.user_joined" {
			peer.joins <- data.(JoinEvent)
			return
		}
		if kind != "rtc.signal" {
			return
		}
		signal := data.(Signal)
		mu.Lock()
		defer mu.Unlock()
		if signal.SDP != nil {
			if err := pc.SetRemoteDescription(*signal.SDP); err != nil {
				signalingErrors <- err
				return
			}
			for _, candidate := range pending {
				if err := pc.AddICECandidate(candidate); err != nil {
					signalingErrors <- err
				}
			}
			pending = nil
		}
		if signal.ICE != nil {
			if pc.RemoteDescription() == nil {
				pending = append(pending, *signal.ICE)
			} else if err := pc.AddICECandidate(*signal.ICE); err != nil {
				signalingErrors <- err
			}
		}
	}
	if err := sfu.Join(id, id, room, capacity, send); err != nil {
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
	case <-time.After(5 * time.Second):
		t.Fatal("ICE gathering timed out")
	}
	if err := sfu.Signal(id, id, Signal{SDP: pc.LocalDescription()}); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
		select {
		case err := <-signalingErrors:
			t.Fatal(err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("peer failed to connect")
		}
	}
	return peer
}

func awaitJoin(t *testing.T, peer *testPeer, userID, channelID string) {
	t.Helper()
	select {
	case event := <-peer.joins:
		if event.UserID != userID || event.ChannelID != channelID {
			t.Fatalf("unexpected join: %+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("join notification was not delivered")
	}
}

func (peer *testPeer) send(payload []byte) error {
	peer.sequence++
	peer.timestamp += 960
	return peer.track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: peer.sequence, Timestamp: peer.timestamp}, Payload: payload})
}

func awaitPacket(t *testing.T, source, destination *testPeer, payload []byte, expect bool) {
	t.Helper()
	duration := 2 * time.Second
	if !expect {
		duration = 200 * time.Millisecond
	}
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := source.send(payload); err != nil {
				t.Fatal(err)
			}
		case packet := <-destination.packets:
			if bytes.Equal(packet, payload) {
				if !expect {
					t.Fatal("audio crossed a mute or room boundary")
				}
				return
			}
		case <-deadline.C:
			if expect {
				t.Fatal("audio was not forwarded")
			}
			return
		}
	}
}

func TestAudioForwardingCapacityIsolationAndModeration(t *testing.T) {
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	alice := connectPeer(t, sfu, "alice", "lounge")
	awaitJoin(t, alice, "alice", "lounge")
	bob := connectPeer(t, sfu, "bob", "lounge")
	awaitJoin(t, alice, "bob", "lounge")
	awaitJoin(t, bob, "bob", "lounge")
	elsewhere := connectPeer(t, sfu, "carol", "other-room")
	awaitJoin(t, elsewhere, "carol", "other-room")
	// A reserved place that never connects must remain silent.
	if err := sfu.Join("erin", "erin", "other-room", 2, func(kind string, _ any) {
		if kind == "voice.user_joined" {
			t.Error("unconnected peer received a join notification")
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Repeated connection callbacks and stale callbacks after leaving are silent.
	sfu.mu.Lock()
	bobPeer, departedPeer := sfu.peers["bob"], sfu.peers["erin"]
	sfu.mu.Unlock()
	sfu.Leave("erin", "erin")
	sfu.announceJoin("bob", bobPeer)
	sfu.announceJoin("erin", departedPeer)
	if err := sfu.Join("dave", "dave", "lounge", 2, func(string, any) {}); err == nil {
		t.Fatal("capacity was not enforced")
	}
	if err := sfu.Signal("alice", "wrong-tab", Signal{}); err == nil {
		t.Fatal("another tab took over signaling")
	}
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 1}, true)
	awaitPacket(t, alice, elsewhere, []byte{0xf8, 0xff, 0xfe, 2}, false)
	forced := true
	sfu.Moderate("alice", &forced, nil)
	sfu.SetState("alice", "alice", false, false, true)
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 3}, false)
	forced = false
	sfu.Moderate("alice", &forced, nil)
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 4}, true)
	sfu.SetState("bob", "bob", false, true, false)
	awaitPacket(t, alice, bob, []byte{0xf8, 0xff, 0xfe, 5}, false)
	for _, peer := range []*testPeer{alice, bob, elsewhere} {
		select {
		case event := <-peer.joins:
			t.Fatalf("duplicate or cross-channel join notification: %+v", event)
		default:
		}
	}
	sfu.CloseChannel("lounge")
	if states := sfu.States(); len(states) != 1 || states[0].UserID != "carol" {
		t.Fatalf("channel cleanup: %v", states)
	}
}
