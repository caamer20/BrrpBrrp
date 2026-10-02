package rtc

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// The video feedback path (ENG-02). A viewer's picture cannot start without a keyframe,
// and a browser that loses one waits for the sender's next natural keyframe — seconds,
// or until the encoder decides. These tests drive the real signalling and RTP path:
// a viewer's keyframe request is written as RTCP on its own connection, the server must
// forward it to the publication's owner, rate-limit it, and ignore one that has nothing
// behind it.

// shareScreenFrom publishes a screen through the production signal path and waits until
// the room is told about it.
func shareScreenFrom(t *testing.T, sfu *SFU, publisher *testPeer) {
	t.Helper()
	publisher.setScreenTrack(t)
	offer, err := publisher.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal(publisher.name, publisher.name, Signal{
		ConnectionID: sfu.connectionIDFor(publisher.name),
		SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: publisher.pc.LocalDescription().SDP},
		Publication:  PublicationScreenVideo,
	}); err != nil {
		t.Fatalf("the publisher could not share: %v", err)
	}
	waitFor(t, "the publication to be announced", func() bool {
		for _, publication := range sfu.Publications("room") {
			if publication.Kind == PublicationScreenVideo {
				return true
			}
		}
		return false
	})
}

// viewScreen negotiates a receiving line for the viewer and returns the transceiver, so
// a test can address RTCP at the stream the server will send on it.
func viewScreen(t *testing.T, sfu *SFU, viewer *testPeer) *webrtc.RTPTransceiver {
	t.Helper()
	receiver, err := viewer.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		t.Fatal(err)
	}
	offer, err := viewer.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := viewer.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal(viewer.name, viewer.name, Signal{
		ConnectionID: sfu.connectionIDFor(viewer.name),
		SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: viewer.pc.LocalDescription().SDP},
	}); err != nil {
		t.Fatalf("a viewer could not negotiate to receive: %v", err)
	}
	return receiver
}

// requestKeyframeFrom asks for a keyframe the way a browser does: RTCP addressed at the
// stream the server sends this viewer.
//
// Both SSRC fields carry that stream's value. A browser's RTCP for a receive-only
// transceiver identifies itself with the same media source it is reporting on — that is
// how the packet is demultiplexed to the sender it concerns on the other side — and a
// request that names a different originator arrives as "unhandled RTCP" and is dropped
// before any reader sees it.
func requestKeyframeFrom(t *testing.T, viewer *testPeer, receiver *webrtc.RTPTransceiver) {
	t.Helper()
	track := receiver.Receiver().Track()
	if track == nil {
		t.Fatal("the viewer has no remote video track to reference")
	}
	ssrc := uint32(track.SSRC())
	if err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: ssrc, MediaSSRC: ssrc}}); err != nil {
		t.Fatal(err)
	}
}

// awaitKeyframeRequest reports the next keyframe request the publisher's client sees,
// or fails.
func awaitKeyframeRequest(t *testing.T, publisher *testPeer, what string) rtcp.Packet {
	t.Helper()
	select {
	case packet := <-publisher.keyframeRequests:
		return packet
	case <-time.After(handshakeBudget):
		t.Fatalf("%s: the publisher saw no keyframe request", what)
		return nil
	}
}

// The feedback a keyframe request travels on has to be negotiated in the answer, or the
// browser will not send one: a receiver reports only on feedback that was offered. The
// registration is the deliverable, so the absence of these lines is the defect.
func TestTheAnswerAdvertisesKeyframeFeedback(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	viewScreen(t, sfu, viewer)

	videoFeedback := func(t *testing.T, description *webrtc.SessionDescription, who string) map[string]bool {
		t.Helper()
		if description == nil {
			t.Fatalf("%s has no answer", who)
		}
		video := ""
		for _, section := range strings.Split(description.SDP, "m=") {
			if strings.HasPrefix(section, "video") {
				video = section
				break
			}
		}
		if video == "" {
			t.Fatalf("the answer to %s carries no video section", who)
		}
		feedback := map[string]bool{}
		for _, raw := range strings.Split(video, "\n") {
			line := strings.TrimRight(raw, "\r")
			if strings.HasPrefix(line, "a=rtcp-fb:") {
				feedback[line] = true
			}
		}
		return feedback
	}
	has := func(feedback map[string]bool, suffix string) bool {
		for line := range feedback {
			if strings.HasSuffix(line, " "+suffix) {
				return true
			}
		}
		return false
	}

	// The publisher's section: the line its stream arrives on, and where a keyframe
	// request about it would be answered.
	publisherFeedback := videoFeedback(t, publisher.pc.RemoteDescription(), "the publisher")
	// The permitted control beside "the feedback is registered": a type this build does
	// not offer must *not* appear, so the assertion is about what was negotiated rather
	// than about the line format.
	if !has(publisherFeedback, "nack pli") {
		t.Fatalf("the publisher's video answer does not offer nack pli: %v", publisherFeedback)
	}
	if !has(publisherFeedback, "ccm fir") {
		t.Fatalf("the publisher's video answer does not offer ccm fir: %v", publisherFeedback)
	}
	if has(publisherFeedback, "goog-remb") {
		t.Fatalf("the answer offers goog-remb, which this build does not implement: %v", publisherFeedback)
	}
	// And the viewer's own line offers the same feedback: that is the negotiation its
	// browser reads before deciding whether a keyframe request is worth sending.
	viewerFeedback := videoFeedback(t, viewer.pc.RemoteDescription(), "the viewer")
	if !has(viewerFeedback, "nack pli") || !has(viewerFeedback, "ccm fir") {
		t.Fatalf("the viewer's video answer does not offer keyframe feedback: %v", viewerFeedback)
	}
}

// A room's registries do not outlive the room. One entry per room name that ever held a
// publication is a leak in the direction nobody looks — and the keyframe windows are
// keyed the same way, so they were doubled by this change rather than introduced by it.
func TestAnEmptyRoomLeavesNoRegistriesBehind(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the publisher to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	window := "room\x00" + string(PublicationScreenVideo)
	sfu.mu.Lock()
	sfu.keyframes[window] = time.Now()
	sfu.mu.Unlock()

	sfu.Leave("ana", "ana")
	waitFor(t, "the room to empty", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		return len(sfu.peers) == 0
	})
	sfu.mu.Lock()
	_, hasRegistry := sfu.publications["room"]
	_, hasWindow := sfu.keyframes[window]
	sfu.mu.Unlock()
	if hasRegistry || hasWindow {
		t.Fatalf("an empty room kept its registries: publications=%v keyframes=%v", hasRegistry, hasWindow)
	}

	// The permitted control: the room is still usable, and a new share in it is
	// announced exactly as before.
	again := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the rejoin to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, again)
}

// A viewer that joins after a share started is told about it. The announcement was
// emitted only when a publication was registered, so a client that arrived later
// negotiated nothing at all: no video line, no output for the server to write to, and a
// share that was invisible to exactly the person who joined to watch it (ENG-02).
func TestAViewerJoiningAfterAShareStartedIsToldAboutIt(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the publisher to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)

	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "the viewer to connect", func() bool {
		return sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	deadline := time.After(handshakeBudget)
	for {
		select {
		case event := <-viewer.events:
			if event.kind != "screen.published" {
				continue
			}
			announcement, ok := event.data.(map[string]any)
			if !ok {
				t.Fatalf("screen.published carried %T", event.data)
			}
			if announcement["kind"] != string(PublicationScreenVideo) {
				t.Fatalf("the announcement is about %v, want the screen", announcement["kind"])
			}
			if announcement["owner"] != "ana" {
				t.Fatalf("the announcement names owner %v, want ana", announcement["owner"])
			}
			return
		case <-deadline:
			t.Fatal("a viewer that joined after the share began was never told about it")
		}
	}
}

// The control: a stopped share is not announced to a joiner. A client told about a
// publication nobody is sending would negotiate a line for a picture that never comes,
// which is the same black rectangle reached from the other direction.
func TestAStoppedShareIsNotAnnouncedToAJoiner(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the publisher to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: sfu.connectionIDFor("ana"), Publication: PublicationScreenVideo, PublicationStopped: true,
	}); err != nil {
		t.Fatal(err)
	}

	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "the viewer to connect", func() bool {
		return sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case event := <-viewer.events:
			if event.kind == "screen.published" {
				t.Fatalf("a stopped share was announced to a joiner: %+v", event.data)
			}
		case <-deadline:
			return
		}
	}
}

func TestAKeyframeRequestWithNoPublicationIsIgnored(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	receiver := viewScreen(t, sfu, viewer)
	requestKeyframeFrom(t, viewer, receiver)

	select {
	case packet := <-publisher.keyframeRequests:
		t.Fatalf("a keyframe request with no publication reached a peer: %+v", packet)
	case <-time.After(500 * time.Millisecond):
	}
}

// A viewer that asks for a keyframe is asking the *publisher*, through the server: the
// request has to arrive on the connection that is sending the stream, addressed at the
// SSRC that publisher is sending.
func TestAViewersKeyframeRequestReachesThePublisher(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	receiver := viewScreen(t, sfu, viewer)
	// The publisher's screen track is on the wire as soon as frames are written; the
	// server learns the SSRC from the track it receives, which is what a keyframe
	// request has to reference.
	if err := publisher.sendVideo([]byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server to learn the publisher's video SSRC", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		peer := sfu.peers["ana"]
		return peer != nil && peer.incomingSSRC[PublicationScreenVideo] != 0
	})

	requestKeyframeFrom(t, viewer, receiver)
	packet := awaitKeyframeRequest(t, publisher, "a viewer asked for a keyframe")
	request, ok := packet.(*rtcp.PictureLossIndication)
	if !ok {
		t.Fatalf("the publisher received %T, want a picture loss indication", packet)
	}
	if request.MediaSSRC != publisher.screenSSRC(t) {
		t.Fatalf("the request names SSRC %d, but the publisher sends %d", request.MediaSSRC, publisher.screenSSRC(t))
	}
}

// screenSSRC reports the media source this peer's screen sender negotiated. It is what
// a keyframe request about that stream has to name.
func (peer *testPeer) screenSSRC(t *testing.T) uint32 {
	t.Helper()
	if peer.screenSender == nil {
		t.Fatal("the peer has no screen sender")
	}
	parameters := peer.screenSender.GetParameters()
	if len(parameters.Encodings) == 0 {
		t.Fatal("the screen sender has no encoding")
	}
	return uint32(parameters.Encodings[0].SSRC)
}

// Two requests in the same window are one: a browser that is waiting for a keyframe
// sends them repeatedly, and forwarding each one turns one viewer's impatience into a
// keyframe storm that hurts every viewer in the room.
func TestKeyframeRequestsAreRateLimited(t *testing.T) {
	sfu := newTestSFU(t)
	sfu.keyframeInterval = 300 * time.Millisecond
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	receiver := viewScreen(t, sfu, viewer)
	if err := publisher.sendVideo([]byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server to learn the publisher's video SSRC", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		peer := sfu.peers["ana"]
		return peer != nil && peer.incomingSSRC[PublicationScreenVideo] != 0
	})

	requestKeyframeFrom(t, viewer, receiver)
	awaitKeyframeRequest(t, publisher, "the first request")
	requestKeyframeFrom(t, viewer, receiver)
	select {
	case packet := <-publisher.keyframeRequests:
		t.Fatalf("a second request inside the window was forwarded: %+v", packet)
	case <-time.After(100 * time.Millisecond):
	}
	// And the window is a delay, not a one-shot: after it passes, a request is new
	// information again.
	time.Sleep(sfu.keyframeInterval)
	requestKeyframeFrom(t, viewer, receiver)
	awaitKeyframeRequest(t, publisher, "a request after the window")
}

// The permitted control beside the refusal: RTCP that is not a keyframe request — a
// receiver report, which every connection sends continuously — must not be forwarded as
// one, or the feedback path becomes a way to make the publisher encode keyframes as
// fast as a client can send reports.
func TestOrdinaryRTCPIsNotForwardedAsAKeyframeRequest(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	receiver := viewScreen(t, sfu, viewer)
	if err := publisher.sendVideo([]byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server to learn the publisher's video SSRC", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		peer := sfu.peers["ana"]
		return peer != nil && peer.incomingSSRC[PublicationScreenVideo] != 0
	})

	if err := viewer.pc.WriteRTCP([]rtcp.Packet{&rtcp.ReceiverReport{}}); err != nil {
		t.Fatal(err)
	}
	select {
	case packet := <-publisher.keyframeRequests:
		t.Fatalf("an ordinary RTCP packet became a keyframe request: %+v", packet)
	case <-time.After(500 * time.Millisecond):
	}
	// The control: the same connection's *actual* keyframe request does get through, so
	// the silence above is about the packet's kind rather than a dead path.
	requestKeyframeFrom(t, viewer, receiver)
	awaitKeyframeRequest(t, publisher, "a real request after an ordinary one")
}

// A viewer whose receiving line is bound while a screen is already being published asks
// for a keyframe without its browser having to notice first: that is what makes a
// late-joining viewer decode within a round trip instead of at the encoder's next
// natural keyframe.
func TestBindingAViewersOutputRequestsAKeyframe(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	if err := publisher.sendVideo([]byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server to learn the publisher's video SSRC", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		peer := sfu.peers["ana"]
		return peer != nil && peer.incomingSSRC[PublicationScreenVideo] != 0
	})
	// Nobody is watching yet, so nothing has been asked for.
	select {
	case packet := <-publisher.keyframeRequests:
		t.Fatalf("a share with no viewers asked for a keyframe: %+v", packet)
	case <-time.After(300 * time.Millisecond):
	}

	// The viewer never sends RTCP: binding the output is the request.
	viewScreen(t, sfu, viewer)
	_ = awaitKeyframeRequest(t, publisher, "a viewer bound to a live share")
}

// After the share stops there is nobody to ask: a stale request must be dropped rather
// than sent to a peer that no longer publishes anything.
func TestAKeyframeRequestAfterTheShareStopsIsIgnored(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	receiver := viewScreen(t, sfu, viewer)
	if err := publisher.sendVideo([]byte{0x00, 0x00, 0x00, 0x01, 0x65}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server to learn the publisher's video SSRC", func() bool {
		sfu.mu.Lock()
		defer sfu.mu.Unlock()
		peer := sfu.peers["ana"]
		return peer != nil && peer.incomingSSRC[PublicationScreenVideo] != 0
	})

	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: sfu.connectionIDFor("ana"), Publication: PublicationScreenVideo, PublicationStopped: true,
	}); err != nil {
		t.Fatal(err)
	}
	// The rate-limit window is shorter than the wait, so a forwarded request here would
	// be the publication's absence failing to stop it.
	time.Sleep(600 * time.Millisecond)
	requestKeyframeFrom(t, viewer, receiver)
	select {
	case packet := <-publisher.keyframeRequests:
		t.Fatalf("a request for a stopped share was forwarded: %+v", packet)
	case <-time.After(500 * time.Millisecond):
	}
}
