package rtc

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// The codec policy (ENG-05): this build sends H264 by preference and VP8 as a fallback,
// because a browser without an H264 decoder — a Chromium built without the proprietary
// codec, or a distribution's Firefox — offers VP8 alone, and an answer carrying a codec
// the offer never listed fails the negotiation instead of degrading.
//
// The decision has to be made from the offer's own text and before the offer is applied:
// the output track is created first, because that is what makes the answer's section the
// one the screen travels on (ADR 0019).

// connectViewerOfferingOnly joins a participant whose browser has exactly one video
// codec, then negotiates a receiving line for a screen. It is the shape of a client
// without an H264 decoder, which no browser on this machine is.
func connectViewerOfferingOnly(t *testing.T, sfu *SFU, id, room, videoCodec string) (*webrtc.PeerConnection, chan struct{}) {
	t.Helper()
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: videoCodec, ClockRate: videoClockRate},
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		t.Fatal(err)
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(media, registry); err != nil {
		t.Fatal(err)
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry)).
		NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	var mu sync.Mutex
	if err := sfu.Join(id, id, room, 2, func(kind string, data any) {
		if kind != "rtc.signal" {
			return
		}
		signal, ok := data.(Signal)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if signal.SDP != nil {
			if err := pc.SetRemoteDescription(*signal.SDP); err != nil {
				t.Errorf("the viewer refused the server's description: %v", err)
			}
		}
		if signal.ICE != nil && pc.RemoteDescription() != nil {
			if err := pc.AddICECandidate(*signal.ICE); err != nil {
				t.Errorf("the viewer refused a candidate: %v", err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		ice := candidate.ToJSON()
		_ = sfu.Signal(id, id, Signal{ICE: &ice, ConnectionID: sfu.connectionIDFor(id)})
	})

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "microphone", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal(id, id, Signal{ConnectionID: sfu.connectionIDFor(id), SDP: pc.LocalDescription()}); err != nil {
		t.Fatalf("the viewer could not join: %v", err)
	}
	waitFor(t, id+" to connect", func() bool {
		return sfu.connectionStateOf(id) == webrtc.PeerConnectionStateConnected
	})

	receiver, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		t.Fatal(err)
	}
	viewOffer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(viewOffer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal(id, id, Signal{ConnectionID: sfu.connectionIDFor(id), SDP: pc.LocalDescription()}); err != nil {
		t.Fatalf("the viewer could not negotiate a receiving line: %v", err)
	}
	waitFor(t, "the view to be answered", func() bool {
		return pc.SignalingState() == webrtc.SignalingStateStable && pc.RemoteDescription() != nil
	})

	received := make(chan struct{}, 64)
	go func() {
		remote := receiver.Receiver().Track()
		if remote == nil {
			return
		}
		for {
			if _, _, err := remote.ReadRTP(); err != nil {
				return
			}
			select {
			case received <- struct{}{}:
			default:
			}
		}
	}()
	return pc, received
}

func TestAScreenViewerWithoutH264IsAnsweredVP8(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the publisher to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)

	pc, received := connectViewerOfferingOnly(t, sfu, "bo", "room", webrtc.MimeTypeVP8)
	answer := pc.RemoteDescription()
	if answer == nil {
		t.Fatal("the viewer has no answer at all")
	}
	if !strings.Contains(answer.SDP, "VP8/90000") {
		t.Fatalf("the answer to a VP8-only viewer carries no VP8:\n%s", answer.SDP)
	}
	if strings.Contains(answer.SDP, "H264") {
		t.Fatalf("the answer offered H264 to a client that did not list it:\n%s", answer.SDP)
	}
	if codec := screenOutputCodec(t, sfu, "bo"); codec != webrtc.MimeTypeVP8 {
		t.Fatalf("the output built for a VP8-only viewer is %s", codec)
	}

	for sequence := range 20 {
		sfu.forwardPublication(sfuPeer(t, sfu, "ana"), PublicationScreenVideo, screenPacket(600, uint16(sequence)))
	}
	select {
	case <-received:
	case <-time.After(handshakeBudget):
		t.Fatal("no screen frame reached a viewer that can only decode VP8")
	}
}

// The permitted control: a client that lists both gets the one it listed first. The
// order is the client's own statement about what it can encode, and answering with this
// build's preference instead produced a stream that arrived in full and decoded to
// nothing on Firefox (ENG-05).
func TestAScreenViewerGetsTheCodecItListedFirst(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	shareScreenFrom(t, sfu, publisher)
	viewScreen(t, sfu, viewer)

	// What the viewer's own offer asked for, read back from its own description: the
	// assertion is about the rule, not about which codec a particular build happens to
	// prefer today.
	offer := viewer.pc.RemoteDescription()
	if offer == nil {
		t.Fatal("the viewer has no answer at all")
	}
	first := videoCodecsInOffer(*viewer.pc.LocalDescription())
	if len(first) == 0 {
		t.Fatal("the viewer's offer lists no video codecs")
	}
	want, ok := videoCapability(first[0])
	if !ok {
		t.Fatalf("the viewer's first video codec %q is not one this build carries", first[0])
	}
	if codec := screenOutputCodec(t, sfu, "bo"); codec != want.MimeType {
		t.Fatalf("the output built for a client listing %s first is %s", want.MimeType, codec)
	}
	// And the publication itself was negotiated with the publisher's own first choice,
	// so the payloads and the viewers' outputs agree.
	if codec := screenOutputCodec(t, sfu, "ana"); codec != want.MimeType {
		t.Fatalf("the publication is carried as %s while the viewer expects %s", codec, want.MimeType)
	}
}

// screenOutputCodec reports the codec of the server's video output for a viewer, which
// is the decision the offer's text produced.
func screenOutputCodec(t *testing.T, sfu *SFU, userID string) string {
	t.Helper()
	for _, output := range viewerScreens(sfu, userID) {
		if output.kind == PublicationScreenVideo {
			return output.track.Codec().MimeType
		}
	}
	t.Fatalf("the viewer %s has no screen output", userID)
	return ""
}

// The parser's own contract, on descriptions a browser could really send: the codec that
// is chosen has to be one the offer contained, which is what makes the negotiation
// succeed rather than fail.
func TestScreenTrackCodecChoosesFromTheOffersOwnText(t *testing.T) {
	both := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\nm=video 9 UDP/TLS/RTP/SAVPF 96 102\r\na=rtpmap:96 VP8/90000\r\na=rtpmap:102 H264/90000\r\n"}
	if codec := screenTrackCodec(both); codec.MimeType != webrtc.MimeTypeVP8 {
		t.Fatalf("an offer listing VP8 first chose %s", codec.MimeType)
	}
	h264First := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 102 96\r\na=rtpmap:102 H264/90000\r\na=rtpmap:96 VP8/90000\r\n"}
	if codec := screenTrackCodec(h264First); codec.MimeType != webrtc.MimeTypeH264 {
		t.Fatalf("an offer listing H264 first chose %s", codec.MimeType)
	}
	vp8Only := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000\r\n"}
	if codec := screenTrackCodec(vp8Only); codec.MimeType != webrtc.MimeTypeVP8 {
		t.Fatalf("a VP8-only offer chose %s", codec.MimeType)
	}
	noVideo := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\n"}
	if codec := screenTrackCodec(noVideo); codec.MimeType != webrtc.MimeTypeH264 {
		t.Fatalf("an offer with no video section chose %s", codec.MimeType)
	}
}
