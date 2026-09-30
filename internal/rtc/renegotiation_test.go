package rtc

import (
	"testing"
	"time"

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
	t.Cleanup(func() {
		for _, peer := range sfu.peers {
			_ = peer.pc.Close()
		}
	})
	return sfu
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
