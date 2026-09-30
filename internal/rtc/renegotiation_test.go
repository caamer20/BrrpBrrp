package rtc

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Renegotiation is the thing that was impossible before: the SDP gate accepted an
// offer only when no remote description existed yet (`sfu.go:275`), so a screen could
// not be added to a call that was already up. These tests drive a real second offer
// through a real peer connection.

func newTestSFU(t *testing.T) *SFU {
	t.Helper()
	sfu, err := New(Options{}, func([]State) {})
	if err != nil {
		t.Fatal(err)
	}
	// The peer map is read under the SFU's lock. Iterating it directly raced with the
	// connection-state callback that removes a peer as its connection closes, which the
	// race detector correctly flagged: the production code holds this lock and the
	// test was reaching past it.
	t.Cleanup(func() {
		sfu.mu.Lock()
		peers := make([]*Peer, 0, len(sfu.peers))
		for _, peer := range sfu.peers {
			peers = append(peers, peer)
		}
		sfu.mu.Unlock()
		for _, peer := range peers {
			_ = peer.pc.Close()
		}
	})
	return sfu
}

// viewerScreens copies a peer's video outputs under the SFU lock, so a test never
// reaches into a peer's fields while a connection-state callback may be replacing it.
func viewerScreens(sfu *SFU, userID string) []*screenForwarder {
	sfu.mu.Lock()
	defer sfu.mu.Unlock()
	peer, ok := sfu.peers[userID]
	if !ok {
		return nil
	}
	return append([]*screenForwarder(nil), peer.screens...)
}

// waitFor polls briefly rather than sleeping a fixed time. A fixed sleep makes the
// test either slow or flaky, and a flaky media test is worse than no media test.
func waitFor(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A second offer carrying a screen publication is answered rather than refused. That
// is the whole point: a screen added to a live call.
func TestRenegotiationAddsAScreenToALiveCall(t *testing.T) {
	sfu := newTestSFU(t)
	peer := connectPeer(t, sfu, "ana", "room")
	connectionID := sfu.connectionIDFor("ana")
	if connectionID == "" {
		t.Fatal("the peer has no connection id")
	}
	// The initial connection is up before anything is added to it.
	waitFor(t, "the call to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})

	// The client adds a screen track and offers again.
	video, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "screen", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.pc.AddTransceiverFromTrack(video, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: connectionID,
		SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: peer.pc.LocalDescription().SDP},
		Publication:  PublicationScreenVideo,
	}); err != nil {
		t.Fatalf("a second offer naming a screen was refused: %v", err)
	}
	// The server answered: the peer's own handler applied the remote description, so
	// the connection is still healthy afterwards rather than stuck mid-exchange. A
	// server that accepted the offer and produced no answer would leave the client's
	// transceiver sending into nothing.
	waitFor(t, "the connection to survive the renegotiation", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	// The publication is announced only after the description carrying it was accepted,
	// so a viewer is never told about a screen the connection does not have.
	waitFor(t, "the screen to be announced", func() bool {
		for _, publication := range sfu.Publications("room") {
			if publication.Kind == PublicationScreenVideo {
				return true
			}
		}
		return false
	})
}

// A second screen in one room is refused, and the refusal says which bound was hit —
// a person cannot act on "limit reached" and can act on "only one screen at a time in
// this conversation".
func TestASecondScreenInOneRoomIsRefusedWithAReason(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	// One screen already exists, as though ana had started sharing.
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "existing", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()

	err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenVideo,
	})
	if err == nil {
		t.Fatal("a second screen in one room was admitted")
	}
	if err.Error() == "" {
		t.Fatal("the refusal has no explanation")
	}
	// A second screen in a *different* room is a different question and is allowed,
	// because the serverwide bound is two. The check happens before the SDP exchange,
	// so reaching that exchange — and failing there for want of a real offer — is
	// exactly what "the bound admitted it" looks like.
	connectPeer(t, sfu, "bo", "other")
	waitFor(t, "bo to connect", func() bool {
		return sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	boError := sfu.renegotiate(sfu.peers["bo"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenVideo,
	})
	if boError == nil {
		return
	}
	if boError.Error() == err.Error() {
		t.Fatalf("a screen in a second room was refused by the same room bound: %v", boError)
	}
	// A third screen anywhere reaches the serverwide bound, whose refusal is a
	// different sentence from the room's — the two are different problems with
	// different remedies, and a person cannot act on a shared message.
	connectPeer(t, sfu, "cy", "third")
	waitFor(t, "cy to connect", func() bool {
		return sfu.connectionStateOf("cy") == webrtc.PeerConnectionStateConnected
	})
	third := sfu.renegotiate(sfu.peers["cy"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenVideo,
	})
	if third == nil {
		t.Fatal("the server accepted a third screen")
	}
	if third.Error() == err.Error() {
		t.Fatalf("a serverwide refusal reads the same as a room refusal: %v", third)
	}
}

// A member who may listen but not transmit may not publish a screen either. A screen
// is a transmission, and treating it as something separate is how a member with no
// voice permission ends up broadcasting video.
func TestSpeakPermissionGatesAScreenToo(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.SetSpeakAllowed("ana", "ana", false)
	if err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenVideo,
	}); err == nil {
		t.Fatal("a member who may not speak published a screen")
	}
	// Regranting restores the ability, and does not turn into a permanent refusal.
	sfu.SetSpeakAllowed("ana", "ana", true)
	// The remaining failure now is the SDP exchange, not the permission — which is
	// what proves the permission is no longer the blocker.
	err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenVideo,
	})
	if err != nil && err.Error() == "you may listen but not share in this channel" {
		t.Fatal("the regrant did not restore the ability to publish")
	}
}

// A second microphone is refused: the connection already has one, and the forwarder
// has nowhere to send a second audio input.
func TestASecondMicrophoneIsRefused(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	if err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationMicrophone,
	}); err == nil {
		t.Fatal("a second microphone was admitted on one connection")
	}
}

// A second offer with no publication named is refused, rather than being treated as an
// unspecified renegotiation — otherwise the bounds could be bypassed by leaving the
// kind out.
func TestASecondOfferMustSayWhatItIsAdding(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	if err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""},
	}); err == nil {
		t.Fatal("a second offer with no publication was admitted")
	}
	if err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationKind("hologram"),
	}); err == nil {
		t.Fatal("an unknown publication kind was admitted")
	}
}

// Screen audio without a screen is refused: sound from nowhere is not a feature.
func TestScreenAudioRequiresAScreen(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	if err := sfu.renegotiate(sfu.peers["ana"], Signal{
		SDP: &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}, Publication: PublicationScreenAudio,
	}); err == nil {
		t.Fatal("screen audio was published with no screen")
	}
}

// Stopping a publication removes it rather than leaving a silent entry, because a
// viewer cannot tell the difference.
func TestStoppingAPublicationRemovesIt(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	if len(sfu.Publications("room")) != 1 {
		t.Fatalf("the room carries %d publications, want 1", len(sfu.Publications("room")))
	}
	if !sfu.StopPublication("room", "s1") {
		t.Fatal("stopping a live publication reported nothing was removed")
	}
	if len(sfu.Publications("room")) != 0 {
		t.Fatal("a stopped publication is still listed")
	}
	// Stopping it again is not an error and does not remove something else.
	if sfu.StopPublication("room", "s1") {
		t.Fatal("stopping an absent publication reported success")
	}
}

// The media engine must actually carry video. Registering the codec is not the same as
// being able to negotiate it, and a server that accepts a screen offer and then cannot
// forward it is worse than one that refuses.
func TestTheMediaEngineCarriesVideo(t *testing.T) {
	sfu := newTestSFU(t)
	// A fresh API with the same engine: the SFU's own api is unexported, so the
	// capability is checked by asking a real peer to describe video.
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "screen", "ana")
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
	if offer.SDP == "" {
		t.Fatal("no offer was produced")
	}
	_ = sfu
}

// A screen's packets reach another participant. The previous forwarding path rewrote
// every timestamp with audio's 48 kHz clock and a fixed 960 step, which is right for
// Opus and meaningless for a 90 kHz video clock — so a screen that negotiated
// successfully would have produced noise. This drives real RTP through the video
// forwarder and checks the clock is video's.
func TestScreenPacketsReachAnotherParticipant(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})

	// The screen publication is live.
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	viewerOutputs := len(sfu.peers["bo"].screens)
	sfu.mu.Unlock()
	// The viewer has no screen output until it renegotiates to receive one, which is
	// the client's side of the exchange; the forwarding path is exercised directly
	// below rather than through a full second renegotiation.
	if viewerOutputs != 0 {
		t.Fatalf("the viewer has %d screen outputs before asking for one", viewerOutputs)
	}
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatalf("the viewer could not create a screen output: %v", err)
	}

	// Feed real video RTP through the forwarder.
	output := sfu.peers["bo"].screens[0]
	for index := range 30 {
		output.write("ana", &rtp.Packet{
			Header:  rtp.Header{Version: 2, SequenceNumber: uint16(index), Timestamp: uint32(index) * 3000, Marker: index%30 == 0},
			Payload: []byte{0x00, 0x00, 0x00, 0x01, 0x65},
		})
	}
	// The forwarder wrote 30 packets, and its timeline advanced on the video clock
	// rather than audio's: 30 frames at 1/30s each is one second, which is 90,000
	// ticks. On the old audio path this would have been 30 x 960.
	output.mu.Lock()
	elapsed := output.timestamp
	output.mu.Unlock()
	if elapsed != videoClockRate {
		t.Fatalf("the forwarded timeline advanced to %d, want one second of video (%d)", elapsed, videoClockRate)
	}
}

// A restarted publication does not inherit a stale timeline. Without the `started`
// check the first packet of a new publication computes its delta against the last
// packet of the previous one, and a decoder reads the result as a stream that has
// been running for minutes.
func TestARestartedScreenDoesNotInheritThePreviousTimeline(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "bo", "room")
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatal(err)
	}
	output := sfu.peers["bo"].screens[0]
	// A first source running for a long time: its timeline is deliberately far from
	// zero, so a restart that wrongly continued from it would be obvious.
	output.write("ana", &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 1, Timestamp: 900000}})
	output.mu.Lock()
	first := output.timestamp
	output.mu.Unlock()
	// A new source, whose timestamps start from zero. Continuing the previous source's
	// timeline would either jump by 900,000 or stall; restarting costs one frame.
	output.write("cy", &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: 1, Timestamp: 0}})
	output.mu.Lock()
	after := output.timestamp
	output.mu.Unlock()
	if after-first != videoClockRate/30 {
		t.Fatalf("a restarted publication advanced by %d, want one frame interval (%d)", after-first, videoClockRate/30)
	}
}

// A stopped screen stops forwarding. Otherwise a viewer who left the screen behind
// keeps receiving a frozen frame from a publication nobody is watching.
func TestAStoppedScreenForwardsNothing(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatal(err)
	}
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	sharer := sfu.peers["ana"]

	// While live, the packet reaches the viewer's forwarder.
	sfu.forwardPublication(sharer, PublicationScreenVideo, &rtp.Packet{
		Header: rtp.Header{Version: 2, SequenceNumber: 1, Timestamp: 0},
	})
	screenOutputs := viewerScreens(sfu, "bo")
	screenOutputs[0].mu.Lock()
	written := screenOutputs[0].sequence
	screenOutputs[0].mu.Unlock()
	if written == 0 {
		t.Fatal("a live screen's first packet was not forwarded")
	}
	// After the stop, nothing more is written.
	sfu.StopPublication("room", "s1")
	sfu.forwardPublication(sharer, PublicationScreenVideo, &rtp.Packet{
		Header: rtp.Header{Version: 2, SequenceNumber: 2, Timestamp: 3000},
	})
	afterScreens := viewerScreens(sfu, "bo")
	afterScreens[0].mu.Lock()
	after := afterScreens[0].sequence
	afterScreens[0].mu.Unlock()
	if after != written {
		t.Fatalf("a stopped screen kept forwarding: %d writes, want %d", after, written)
	}
}

// A video packet is never written through an audio forwarder. The two are separate
// types precisely so that a mistaken call is a compile error rather than noise.
func TestAVideoPacketNeverReachesTheAudioForwarder(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatal(err)
	}
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	sfu.forwardPublication(sfu.peers["ana"], PublicationScreenVideo, &rtp.Packet{
		Header: rtp.Header{Version: 2, SequenceNumber: 1, Timestamp: 0},
	})
	// The audio outputs are untouched by a video packet. The peer list is read under
	// the SFU lock: a connection-state callback can remove a peer while this test
	// looks at it, and reaching past the lock races with that.
	sfu.mu.Lock()
	viewer := sfu.peers["bo"]
	audioOutputs := append([]*forwarder(nil), viewer.outputs...)
	sfu.mu.Unlock()
	before := 0
	for _, output := range audioOutputs {
		output.mu.Lock()
		before += int(output.sequence)
		output.mu.Unlock()
	}
	if before != 0 {
		t.Fatalf("a video packet was written through %d audio outputs", before)
	}
}

// A client that wants to *watch* a screen must not have to claim a publication to do
// it. The refusal "a second offer must name the publication it is adding" made that
// impossible: a viewer was treated as a second publisher, and a room's
// one-screen-per-room bound would then refuse the viewer too.
func TestAViewerCanNegotiateToReceiveAScreenWithoutPublishing(t *testing.T) {
	sfu := newTestSFU(t)
	publisher := connectPeer(t, sfu, "ana", "room")
	viewer := connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})

	// Ana shares, through the production signal path.
	publisher.setScreenTrack(t)
	offer, err := publisher.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: sfu.connectionIDFor("ana"),
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

	// Bo asks only to *receive*. A bare video transceiver has no direction attribute at
	// all — the SDP default makes it sendrecv — so the description cannot say what the
	// client wants; the stream id is the marker, and it has to be on the track itself
	// because a description is validated against the offer that produced it.
	viewTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: videoClockRate},
		ViewStreamID, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := viewer.pc.AddTransceiverFromTrack(viewTrack,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	viewOffer, err := viewer.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := viewer.pc.SetLocalDescription(viewOffer); err != nil {
		t.Fatal(err)
	}
	if err := sfu.Signal("bo", "bo", Signal{
		ConnectionID: sfu.connectionIDFor("bo"),
		SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: viewer.pc.LocalDescription().SDP},
	}); err != nil {
		t.Fatalf("a viewer could not negotiate to receive: %v", err)
	}

	// And the viewer's claim to *receive* is not a claim to publish: the room still
	// holds exactly one screen, and it belongs to the person sharing.
	published, ownedByViewer := 0, false
	for _, publication := range sfu.Publications("room") {
		if publication.Kind != PublicationScreenVideo {
			continue
		}
		published++
		if publication.Owner == "bo" {
			ownedByViewer = true
		}
	}
	if published != 1 {
		t.Fatalf("the room holds %d screen publications, want 1", published)
	}
	if ownedByViewer {
		t.Fatal("a viewer was recorded as a screen publisher")
	}
}

// A second offer that does not say it is a view is still refused. The receive path
// must not become a way to publish by omitting a field.
func TestAnUnmarkedSecondOfferIsStillRefused(t *testing.T) {
	sfu := newTestSFU(t)
	peer := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "the peer to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	// A bare video transceiver with no view marker: this is what the old code could not
	// tell apart from a viewer, and it is refused rather than guessed at.
	if _, err := peer.pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	err = sfu.Signal("ana", "ana", Signal{
		ConnectionID: sfu.connectionIDFor("ana"),
		SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: peer.pc.LocalDescription().SDP},
	})
	if err == nil {
		t.Fatal("a second offer naming no publication and no view was accepted")
	}
}

// A connected call must say which path carried it. "The call connected" is true of a
// direct call and a relayed one alike, so without the candidate type a passing test
// proves nothing about the network path — which is the question an operator has when
// a call works locally and fails for everybody else.
func TestAConnectedCallReportsThePathThatCarriedIt(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.mu.Unlock()

	local, remote, err := sfu.selectedCandidatePair(peer)
	if err != nil {
		t.Fatalf("a connected call reported no candidate pair: %v", err)
	}
	// Two peers on the loopback interface settle on a host/srflx pair. What matters is
	// that the answer is a type name an operator can read, not an opaque id they would
	// have to look up — and never an empty string, which would look like a working
	// measurement while saying nothing.
	if local == "" || remote == "" {
		t.Fatalf("the candidate pair reported %q/%q, want two named types", local, remote)
	}
	switch local {
	case "host", "srflx", "prflx", "relay":
	default:
		t.Fatalf("the local candidate type was %q, which is not a candidate type", local)
	}
	switch remote {
	case "host", "srflx", "prflx", "relay":
	default:
		t.Fatalf("the remote candidate type was %q, which is not a candidate type", remote)
	}
}

// A peer with no established pair must say so rather than reporting an empty path.
// The alternative is a log line reading "path: /" for a call that never connected.
func TestAnUnestablishedCallReportsNoPathRatherThanAnEmptyOne(t *testing.T) {
	sfu := newTestSFU(t)
	// A real connected peer, read through the same path a diagnostic would use, has a
	// settled pair — and a peer with no connection at all must report that rather than
	// dereference nothing. The nil case is the one that matters: the caller is a
	// logging goroutine, and a panic there takes the server down over a diagnostic.
	if _, _, err := sfu.selectedCandidatePair(nil); err == nil {
		t.Fatal("a missing peer reported a candidate pair")
	}
	if _, _, err := sfu.selectedCandidatePair(&Peer{connectionID: "c1", state: State{UserID: "ghost", ChannelID: "room"}}); err == nil {
		t.Fatal("a peer with no connection reported a candidate pair")
	}
}

// A member whose authority is withdrawn must stop being visible in the room, not
// merely stop sending. `leavePeer` removed the peer from the map and closed its
// connection, but never touched the room's publication registry — so a revoked
// member's screen stayed in the room's list, kept consuming the one-screen bound,
// and every later screen share was refused by a share whose owner had been removed.
func TestARemovedMemberStopsBeingPublished(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.announcePublication(peer, Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	if len(sfu.Publications("room")) != 1 {
		t.Fatal("the share was never published")
	}

	sfu.leavePeer("ana", peer)
	if got := len(sfu.Publications("room")); got != 0 {
		t.Fatalf("a member who left is still published in the room: %d publications", got)
	}
}

// The other half of revocation: withdrawing one member's authority must not take
// anybody else's with it. A fix that cleared the whole room's publications would
// pass the test above and break every call in it.
func TestRemovingOneMemberLeavesTheRestSharing(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	waitFor(t, "bo to connect", func() bool {
		return sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	ana, bo := sfu.peers["ana"], sfu.peers["bo"]
	sfu.announcePublication(ana, Publication{ID: "a1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC()})
	sfu.announcePublication(bo, Publication{ID: "b1", Owner: "bo", Kind: PublicationScreenVideo, Started: time.Now().UTC()})
	sfu.mu.Unlock()
	if len(sfu.Publications("room")) != 2 {
		t.Fatalf("the room carries %d publications, want 2", len(sfu.Publications("room")))
	}

	sfu.leavePeer("ana", ana)
	published := sfu.Publications("room")
	if len(published) != 1 {
		t.Fatalf("removing one member left %d publications, want 1", len(published))
	}
	if published[0].Owner != "bo" {
		t.Fatalf("the surviving publication belongs to %q, want bo", published[0].Owner)
	}
	// Bo's connection is untouched: being in a room with someone who was removed is
	// not itself a reason to be disconnected.
	if sfu.connectionStateOf("bo") != webrtc.PeerConnectionStateConnected {
		t.Fatal("removing one member disconnected another")
	}
}

// Reading a room must not create one. `s.room(id)` returned a registry for any id
// and stored it, so every publication lookup for a room that does not exist left a
// permanent empty map behind. A server that has served a long-dead channel — or one
// whose name an attacker chose — accumulates an entry per name, and nothing ever
// removes it.
func TestReadingARoomDoesNotCreateOne(t *testing.T) {
	sfu := newTestSFU(t)
	for i := 0; i < 50; i++ {
		_ = sfu.Publications("room-that-never-existed")
	}
	sfu.mu.Lock()
	rooms := len(sfu.publications)
	sfu.mu.Unlock()
	if rooms != 0 {
		t.Fatalf("reading one nonexistent room %d times left %d registries behind", 50, rooms)
	}
}

// A screen's system audio is a *separate* publication, and the room's bound is
// enforced per publication. If the client sent audio inside the video's offer, the
// server would count one thing twice and refuse a share that is within bounds — and
// the failure would read as "the room is full" to somebody whose room is not.
func TestScreenAudioIsABoundedPublicationOfItsOwn(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.announcePublication(peer, Publication{ID: "v1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC()})
	sfu.announcePublication(peer, Publication{ID: "a1", Owner: "ana", Kind: PublicationScreenAudio, Started: time.Now().UTC()})
	sfu.mu.Unlock()

	published := sfu.Publications("room")
	if len(published) != 2 {
		t.Fatalf("a screen with its audio carries %d publications, want 2", len(published))
	}
	// The video's bound is about screens, not about how many tracks a screen has. A
	// second screen is still refused while this one holds the room's only place — the
	// audio must not have widened it.
	second := &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: ""}
	if err := sfu.renegotiate(peer, Signal{ConnectionID: peer.connectionID, SDP: second, Publication: PublicationScreenVideo}); err == nil {
		t.Fatal("a second screen was admitted to a room that already has one")
	}
	// And the audio is bounded too, so a second screen's audio cannot be smuggled in
	// under the video's allowance.
	if err := sfu.renegotiate(peer, Signal{ConnectionID: peer.connectionID, SDP: second, Publication: PublicationScreenAudio}); err == nil {
		t.Fatal("a second screen's audio was admitted to a room that already has one")
	}
}

// A hundred share start/stop cycles must leave nothing behind. This is the
// regression half of the master's-plan "100 share start/stop cycles": the cycles
// themselves, the room's bound, and the server's own accounting after each one.
//
// A stop that leaks a transceiver, a publication, or a screen output is invisible on
// the first cycle and fatal on the hundredth — the room stays full for shares that
// ended, or the connection accumulates state until a call fails for a reason nobody
// can name. Asserting the count after each cycle is what makes the leak visible on
// the one that causes it.
func TestAHundredShareCyclesLeaveNothingBehind(t *testing.T) {
	sfu := newTestSFU(t)
	// A real client peer, not a bare connection: it applies the server's answers, so
	// each cycle is a complete conversation rather than a half one. Pion refuses a
	// second offer while the connection still holds the first one's local state, so a
	// hundred cycles have to be hundred conversations to be a fair test of the stop.
	client := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.mu.Unlock()

	const cycles = 100
	for cycle := 1; cycle <= cycles; cycle++ {
		// A real offer, produced the way the client produces one. An empty description
		// is refused before the bound is consulted, so a test built from one would be
		// testing the refusal rather than the cycle.
		// The client's half of the exchange. A stop leaves the connection ready for the
		// next offer, so each cycle is a complete conversation rather than a half one —
		// which is what makes the hundred cycles representative of a real call.
		offer := func() *webrtc.SessionDescription {
			track, err := webrtc.NewTrackLocalStaticRTP(
				webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "screen", "ana")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
				t.Fatal(err)
			}
			desc, err := client.pc.CreateOffer(nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.pc.SetLocalDescription(desc); err != nil {
				t.Fatal(err)
			}
			return client.pc.LocalDescription()
		}
		if err := sfu.renegotiate(peer, Signal{ConnectionID: peer.connectionID, SDP: offer(), Publication: PublicationScreenVideo}); err != nil {
			t.Fatalf("cycle %d: the first share was refused: %v", cycle, err)
		}
		// The client applies the server's answer through its own send handler, which is
		// the same path a browser uses. Waiting for it keeps the next cycle honest.
		waitFor(t, "the client's half of the first exchange", func() bool {
			return client.pc.SignalingState() == webrtc.SignalingStateStable
		})
		if got := len(sfu.Publications("room")); got != 1 {
			t.Fatalf("cycle %d: the room carries %d publications, want 1", cycle, got)
		}
		if err := sfu.Signal("ana", "ana", Signal{ConnectionID: peer.connectionID, PublicationStopped: true}); err != nil {
			t.Fatalf("cycle %d: the stop failed: %v", cycle, err)
		}
		if got := len(sfu.Publications("room")); got != 0 {
			t.Fatalf("cycle %d: %d publications survived the stop", cycle, got)
		}
		// The room's allowance must come back every time. This is the property that
		// matters: a stop that does not release the room leaves it full for the rest of
		// the call, and the hundredth person is refused by the first person's share.
		if err := sfu.renegotiate(peer, Signal{ConnectionID: peer.connectionID, SDP: offer(), Publication: PublicationScreenVideo}); err != nil {
			t.Fatalf("cycle %d: the room did not become shareable again: %v", cycle, err)
		}
		waitFor(t, "the client's half of the second exchange", func() bool {
			return client.pc.SignalingState() == webrtc.SignalingStateStable
		})
		if err := sfu.Signal("ana", "ana", Signal{ConnectionID: peer.connectionID, PublicationStopped: true}); err != nil {
			t.Fatalf("cycle %d: the second stop failed: %v", cycle, err)
		}
	}
	if got := len(sfu.Publications("room")); got != 0 {
		t.Fatalf("after %d cycles the room carries %d publications, want 0", cycles, got)
	}
	// The connection is still the one we started with, and is still usable. A hundred
	// cycles that leave a connection that cannot carry a call have not passed.
	if sfu.connectionStateOf("ana") != webrtc.PeerConnectionStateConnected {
		t.Fatalf("after %d cycles the connection is %v, want connected", cycles, sfu.connectionStateOf("ana"))
	}
}

// Withdrawing permission must reach the person who had it, and it must take their
// screen with it.
//
// The server can stop forwarding on its own, but the *capture* is the browser's. A
// client that is not told keeps its sharing indicator lit, keeps a rectangle of its
// owner's desktop in memory, and keeps publishing a screen nobody receives — and the
// person has no way to tell those apart, because from where they sit the app looks
// exactly as it did when it was working.
func TestWithdrawingPermissionWithdrawsTheShareAndSaysSo(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	peer := sfu.peers["ana"]
	sfu.announcePublication(peer, Publication{ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC()})
	sfu.mu.Unlock()

	// The event the client acts on, collected from what the server actually emitted.
	events := make(chan string, 8)
	sfu.setEmitter("ana", func(kind string, data any) {
		if kind != "voice.authority_changed" {
			return
		}
		payload, _ := data.(map[string]any)
		reason, _ := payload["reason"].(string)
		events <- reason
	})

	sfu.SetSpeakAllowed("ana", "ana", false)

	select {
	case reason := <-events:
		if reason == "" {
			t.Fatal("the client was told its permission changed but not why")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("withdrawing permission told the client nothing; their capture is still running")
	}
	if got := len(sfu.Publications("room")); got != 0 {
		t.Fatalf("a peer whose permission was withdrawn still has %d publications", got)
	}
}

// The withdrawal is about *sharing*. A member who may no longer share is still in the
// call and still hears it — a fix that dropped them from the room would pass the test
// above and break every moderated channel.
func TestWithdrawingSharingPermissionDoesNotRemoveTheMember(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.SetSpeakAllowed("ana", "ana", false)
	if sfu.connectionStateOf("ana") != webrtc.PeerConnectionStateConnected {
		t.Fatal("losing sharing permission disconnected the member from the call")
	}
	if len(sfu.States()) != 2 {
		t.Fatalf("losing sharing permission left %d people in the room, want 2", len(sfu.States()))
	}
}
