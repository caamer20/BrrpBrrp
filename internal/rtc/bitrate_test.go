package rtc

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// The per-publication bitrate ceiling (ENG-03). The server's egress for one screen
// packet is that packet written to every viewer, so a publisher that ignores its own cap
// costs the room a multiple of what it sends, and the first casualty is the voice the
// call exists for. These tests assert the bound and — as importantly — its absence when
// a publisher is inside it.

// The meter itself, on a clock the test controls. Its behaviour under real time is a
// refill of one second's allowance, which is too slow to assert directly.
func TestABitrateMeterRefillsAtItsRateAndDropsBeyondItsBurst(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	meter := newBitrateMeter(800, start) // 100 bytes per second, 100 bytes of burst

	if !meter.allow(100, start) {
		t.Fatal("the first burst was refused")
	}
	if meter.allow(1, start) {
		t.Fatal("a byte beyond the burst was allowed")
	}
	if drops := meter.dropCount(); drops != 1 {
		t.Fatalf("drops = %d, want 1", drops)
	}
	// Half a second of credit is 50 bytes: not enough for 60.
	half := start.Add(500 * time.Millisecond)
	if meter.allow(60, half) {
		t.Fatal("more was allowed than the elapsed time refilled")
	}
	if !meter.allow(40, half) {
		t.Fatal("credit for 40 bytes was refused after half a second")
	}
	// And the burst cap holds: ten seconds of idleness is still one second's credit.
	idle := start.Add(10 * time.Second)
	if !meter.allow(100, idle) {
		t.Fatal("a full second of credit was refused")
	}
	if meter.allow(1, idle) {
		t.Fatal("credit accumulated past the burst cap")
	}
}

// screenPacket builds a video packet of a deliberate size, so a test spends a
// publication's allowance on purpose rather than by luck.
func screenPacket(size int, sequence uint16) *rtp.Packet {
	payload := make([]byte, size)
	payload[0] = 0x00
	return &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 102, Timestamp: videoClockRate / 30, SequenceNumber: sequence},
		Payload: payload,
	}
}

// screenBytes counts the payload bytes a viewer's screen outputs accepted.
func screenBytes(t *testing.T, sfu *SFU, userID string) uint64 {
	t.Helper()
	var total uint64
	for _, output := range viewerScreens(sfu, userID) {
		output.mu.Lock()
		total += output.bytes
		output.mu.Unlock()
	}
	return total
}

// A publisher that blows through the ceiling has its packets dropped rather than
// forwarded: the bound is on what the server sends, not on what the client claims.
func TestAScreenThatExceedsItsCeilingIsBoundedAtTheServer(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	viewScreen(t, sfu, viewer)
	sfuPeer := sfuPeer(t, sfu, "ana")

	// 1200-byte frames, all in one instant: 400 of them are 480 kB, well past the
	// 2.5 Mbit/s ceiling's one-second burst (312.5 kB). The allowance is the burst plus
	// whatever the elapsed time refilled, and nothing waits between packets here, so that
	// second term is the clock the loop itself took.
	started := time.Now()
	for sequence := range 400 {
		sfu.forwardPublication(sfuPeer, PublicationScreenVideo, screenPacket(1200, uint16(sequence)))
	}
	forwarded, dropped := screenBytes(t, sfu, "bo"), sfu.bitrateDrops("ana", PublicationScreenVideo)
	if dropped == 0 {
		t.Fatal("a publisher 480 kB into a 312 kB burst had nothing dropped")
	}
	allowance := float64(ScreenBitrateCeiling/8) * (1 + time.Since(started).Seconds())
	if float64(forwarded) > allowance {
		t.Fatalf("forwarded %d bytes, more than the %.0f the ceiling allows", forwarded, allowance)
	}
	if forwarded == 0 {
		t.Fatal("nothing was forwarded, so the ceiling describes a stream that never started")
	}
}

// The permitted control: a publisher inside its ceiling is untouched. Without this, a
// refusal that dropped everything would pass the test above.
func TestAScreenInsideItsCeilingIsNotPoliced(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	viewScreen(t, sfu, viewer)
	sfuPeer := sfuPeer(t, sfu, "ana")

	// 60 kB in one burst: a keyframe, comfortably inside the one-second allowance.
	for sequence := range 50 {
		sfu.forwardPublication(sfuPeer, PublicationScreenVideo, screenPacket(1200, uint16(sequence)))
	}
	if drops := sfu.bitrateDrops("ana", PublicationScreenVideo); drops != 0 {
		t.Fatalf("a publisher inside its ceiling had %d packets dropped", drops)
	}
	if forwarded := screenBytes(t, sfu, "bo"); forwarded != 50*1200 {
		t.Fatalf("forwarded %d bytes, want all %d", forwarded, 50*1200)
	}
}

// Screen audio has its own, much smaller ceiling: 64 kbit/s is what the plan budgets
// for it, and a system-audio capture that sends more must not spend the picture's
// allowance or the call's.
func TestScreenAudioHasItsOwnSmallerCeiling(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	// A screen first: screen audio is only meaningful alongside one.
	shareScreenFrom(t, sfu, publisher)
	viewScreen(t, sfu, viewer)
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: sfu.connectionIDFor("ana"), Publication: PublicationScreenAudio,
	}); err != nil {
		t.Fatalf("the publisher could not name screen audio: %v", err)
	}
	sfuPeer := sfuPeer(t, sfu, "ana")

	// 500 packets of 200 bytes is 100 kB against an 8 kB burst.
	for sequence := range 500 {
		sfu.forwardPublication(sfuPeer, PublicationScreenAudio, screenPacket(200, uint16(sequence)))
	}
	dropped := sfu.bitrateDrops("ana", PublicationScreenAudio)
	if dropped == 0 {
		t.Fatal("screen audio 100 kB into an 8 kB burst had nothing dropped")
	}
	if drops := sfu.bitrateDrops("ana", PublicationScreenVideo); drops != 0 {
		t.Fatalf("screen audio's ceiling dropped %d video packets", drops)
	}
}

// The microphone is not metered at all: it is the call, it is bounded by the slot
// forwarder and by voice.speak, and a bucket here would silently drop speech under the
// same rule that exists for a screen somebody can simply re-share.
func TestTheMicrophoneIsNotPoliced(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	sfuPeer := sfuPeer(t, sfu, "ana")
	// Far more than any ceiling would allow in a burst, on the microphone's kind.
	for sequence := range 2000 {
		sfu.forwardPublication(sfuPeer, PublicationMicrophone, screenPacket(400, uint16(sequence)))
	}
	sfu.mu.Lock()
	metered := sfuPeer.police[PublicationMicrophone] != nil
	sfu.mu.Unlock()
	if metered {
		t.Fatal("the microphone was given a bitrate meter")
	}
}
