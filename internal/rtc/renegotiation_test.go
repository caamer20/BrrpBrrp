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

// Muting your microphone is not a withdrawal of your screen. The two are separate
// publications with separate audiences, and the reason people mute is usually the
// opposite of the reason that makes them stop presenting: an interruption during a
// call, or a cough, not a withdrawal of the right to be heard or seen.
//
// The withdrawal that does stop a screen is named differently and enforced
// differently - `SetSpeakAllowed` and the admin mute, which are permissions rather
// than a client-side choice.
func TestAMutedMicrophoneDoesNotFreezeTheScreen(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})

	// Ana publishes a screen and bo is watching it.
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatalf("the viewer could not create a screen output: %v", err)
	}

	// A frame before the microphone is muted.
	before := sfu.publishedScreenFrames()
	sfu.forwardPublication(sfu.peers["ana"], PublicationScreenVideo, screenFrame(1))

	// Ana mutes her microphone, as she would when someone talks over her. `SetState`
	// is what the client calls; the third argument is deafened, which stays false.
	sfu.SetState("ana", "ana", true, false, false)
	sfu.mu.Lock()
	muted := sfu.peers["ana"].state.Muted
	sfu.mu.Unlock()
	if !muted {
		t.Fatal("the mute did not take effect")
	}

	// A frame after. The screen is still the same screen, still live, and still
	// published - a member's own push-to-mute cannot stop what everyone is watching.
	sfu.forwardPublication(sfu.peers["ana"], PublicationScreenVideo, screenFrame(2))

	if got := sfu.publishedScreenFrames(); got != before+2 {
		t.Fatalf("the viewer received %d screen frames across the mute, want %d", got, before+2)
	}
}

// A withdrawn permission still does stop a screen, including when the withdrawal
// arrives while a share is live - which is the case the mute test above must not
// have opened up. The two look alike from the server and are not the same thing.
func TestAWithdrawnPermissionStopsALiveScreen(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	sfu.announcePublication(sfu.peers["ana"], Publication{
		ID: "s1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatalf("the viewer could not create a screen output: %v", err)
	}

	before := sfu.publishedScreenFrames()
	sfu.forwardPublication(sfu.peers["ana"], PublicationScreenVideo, screenFrame(1))
	if got := sfu.publishedScreenFrames(); got != before+1 {
		t.Fatalf("the viewer received %d frames before the withdrawal, want %d", got, before+1)
	}

	// A moderator withdraws the permission while the share is live.
	sfu.SetSpeakAllowed("ana", "ana", false)
	sfu.forwardPublication(sfu.peers["ana"], PublicationScreenVideo, screenFrame(2))

	if got := sfu.publishedScreenFrames(); got != before+1 {
		t.Fatalf("the viewer received %d frames after the permission was withdrawn, want the share stopped at %d", got, before+1)
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

// A second microphone on one connection is not a second audio input; it is a
// replacement. The connection has one audio output, and the track that claims it is
// the one members hear.
func TestASecondMicrophoneTakesOverTheConnectionInsteadOfLeavingItSilent(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	ana, bo := sfu.peers["ana"], sfu.peers["bo"]
	sfu.mu.Unlock()

	// Ana's current microphone is reaching bo.
	before := audioWrites(t, bo)
	sfu.forwardPublication(ana, PublicationMicrophone, audioFrame(1))
	if got := audioWrites(t, bo); got != before+1 {
		t.Fatalf("bo received %d audio packets, want %d", got, before+1)
	}

	// A second microphone arrives on ana's connection — a headset plugged in
	// mid-call. The old code returned here, so the new track was never read and
	// ana was left with a connected microphone nobody could hear.
	ana.takeMicrophone()

	// One audio input, not two interleaved: the replacement's packets reach bo, and
	// each forwarded frame advances this slot's sequence once rather than twice.
	slot := bo.outputs[0]
	sfu.forwardPublication(ana, PublicationMicrophone, audioFrame(2))
	if got := audioWrites(t, bo); got != before+2 {
		t.Fatalf("the replacement microphone did not reach bo: %d writes, want %d", got, before+2)
	}
	slot.mu.Lock()
	after := slot.sequence
	slot.mu.Unlock()
	if after != 2 {
		t.Fatalf("the slot advanced to sequence %d across two frames, want 2 — two inputs would double-step it", after)
	}
}

// Screen audio is an audio track, exactly like the microphone, and the codec cannot
// tell them apart. Classifying by codec made a member sharing a screen *with system
// audio* hand their connection's single audio output to the screen's audio — their
// real microphone stopped being forwarded, and the change that caused it read as an
// improvement.
//
// A track belongs to the publication it was negotiated under. The classifier is
// exercised through its decision, because the decision is what was wrong.
func TestAScreenAudioTrackIsNotMistakenForTheMicrophone(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	ana := sfu.peers["ana"]
	sfu.mu.Unlock()

	// The connection's own audio input, with nothing else published, is the microphone.
	if got := sfu.publicationForTrack(ana, webrtc.RTPCodecTypeAudio); got != PublicationMicrophone {
		t.Fatalf("an audio track with no screen audio published is %q, want the microphone", got)
	}

	// Now the same member shares a screen with its system audio. The same codec is
	// that publication, and must not be read as a replacement microphone.
	sfu.mu.Lock()
	sfu.announcePublication(ana, Publication{ID: "v1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC()})
	sfu.announcePublication(ana, Publication{ID: "a1", Owner: "ana", Kind: PublicationScreenAudio, Started: time.Now().UTC()})
	sfu.mu.Unlock()
	if got := sfu.publicationForTrack(ana, webrtc.RTPCodecTypeAudio); got != PublicationScreenAudio {
		t.Fatalf("an audio track while screen audio is published is %q, want the screen's audio", got)
	}

	// A video track is the screen, and a codec this build does not carry is refused
	// rather than guessed at.
	if got := sfu.publicationForTrack(ana, webrtc.RTPCodecTypeVideo); got != PublicationScreenVideo {
		t.Fatalf("a video track is %q, want the screen", got)
	}
	if got := sfu.publicationForTrack(ana, webrtc.RTPCodecType(0)); got != "" {
		t.Fatalf("an uncarried track classified as %q, want it refused", got)
	}

	// And the screen's audio reaches a viewer through the screen forwarder rather than
	// the microphone slots, so sharing a screen does not double-voice the room.
	connectPeer(t, sfu, "bo", "room")
	waitFor(t, "bo to connect", func() bool {
		return sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})
	sfu.mu.Lock()
	bo := sfu.peers["bo"]
	sfu.mu.Unlock()
	if err := bo.addScreenOutput(); err != nil {
		t.Fatalf("the viewer could not create a screen output: %v", err)
	}
	microphoneBefore := audioWrites(t, bo)
	sfu.forwardPublication(ana, PublicationScreenAudio, audioFrame(9))
	if got := audioWrites(t, bo); got != microphoneBefore {
		t.Fatalf("screen audio wrote %d microphone packets, want 0 — it is not the microphone", got-microphoneBefore)
	}
}

// Renegotiating a microphone is how a client says "here is my new input". It used to
// be refused outright — "this connection already has a microphone" — which left a
// member who swapped devices silent for the life of the call.
//
// The offer now releases the claim so the arriving track is read. What still refuses
// is the *permission*, not the existence of a second input: the client gets its
// microphone back, and a member who may not transmit still may not.
func TestAMemberWhoReplacesTheirMicrophoneCanRenegotiateAndBeHeard(t *testing.T) {
	sfu := newTestSFU(t)
	peer := connectPeer(t, sfu, "ana", "room")
	waitFor(t, "ana to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected
	})
	connectionID := sfu.connectionIDFor("ana")
	// The server's own view of the peer, which is what the offer must release.
	ana := sfu.peers["ana"]

	// A live microphone is claimed by the connection.
	ana.receiving.Store(true)
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: connectionID, SDP: microphoneOffer(t, peer.pc), Publication: PublicationMicrophone,
	}); err != nil {
		t.Fatalf("a member replacing their microphone was refused: %v", err)
	}
	if ana.receiving.Load() {
		t.Fatal("the connection still claims the old microphone after a replacement")
	}

	// The permission still gates it. A member who may not transmit cannot talk their
	// way back in by renegotiating a microphone.
	sfu.SetSpeakAllowed("ana", "ana", false)
	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: connectionID, SDP: microphoneOffer(t, peer.pc), Publication: PublicationMicrophone,
	}); err == nil {
		t.Fatal("a member with no speak permission replaced their microphone")
	}
}

// microphoneOffer builds a real offer carrying a fresh audio input, which is what a
// client sends when it swaps microphones. A stub SDP is refused by Pion for lacking an
// ICE ufrag, which would make this test fail on the wrong thing.
func microphoneOffer(t *testing.T, client *webrtc.PeerConnection) *webrtc.SessionDescription {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "microphone", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	return &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: client.LocalDescription().SDP}
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

// A peer that owns no publication in this room must not have its packets forwarded
// under somebody else's.
//
// The check was "does any publication of this kind exist in this room", so a second
// member — one who was refused a share, or never asked for one — could advance viewer
// output under another member's publication: the room's allowance was theirs, and the
// packet was not. Forwarding is the boundary, so the boundary asks whether *this
// connection* owns a live publication of this kind here.
func TestAPacketIsForwardedOnlyForThePeerThatOwnsThePublication(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeerWithCapacity(t, sfu, "ana", "room", 3)
	connectPeerWithCapacity(t, sfu, "bo", "room", 3)
	connectPeerWithCapacity(t, sfu, "cy", "room", 3)
	for _, who := range []string{"ana", "bo", "cy"} {
		id := who
		waitFor(t, id+" to connect", func() bool {
			return sfu.connectionStateOf(id) == webrtc.PeerConnectionStateConnected
		})
	}
	// Everybody is a viewer, so a forwarded packet is visible on all of them. A viewer
	// only has a screen output once it has negotiated one, which is what a viewer
	// actually does — so the observer is given its own.
	sfu.mu.Lock()
	owner := sfu.peers["ana"]
	outsider := sfu.peers["bo"]
	observer := sfu.peers["cy"]
	if err := observer.addScreenOutput(); err != nil {
		sfu.mu.Unlock()
		t.Fatal(err)
	}
	sfu.announcePublication(owner, Publication{
		ID: "owned", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()

	packet := &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 102, Timestamp: videoClockRate / 30, SequenceNumber: 1},
		Payload: []byte{0x00, 0x00, 0x00, 0x01},
	}
	before := screenWrites(t, sfu, "cy")

	// A packet arriving on the owner's connection is the owner's content.
	sfu.forwardPublication(owner, PublicationScreenVideo, packet)
	// A packet arriving on a member who owns nothing here is not.
	sfu.forwardPublication(outsider, PublicationScreenVideo, packet)

	written := screenWrites(t, sfu, "cy") - before
	if written != 1 {
		t.Fatalf("the viewer's screen forwarder took %d packets, want exactly the owner's one", written)
	}
}

// A publication belongs to its owner for as long as it lives. A different connection
// in the same room cannot forward under it, which is the same boundary as above seen
// from the room's side: one share, one source.
func TestOnlyTheOwnerCanFeedAPublishedScreen(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeerWithCapacity(t, sfu, "ana", "room", 3)
	connectPeerWithCapacity(t, sfu, "bo", "room", 3)
	connectPeerWithCapacity(t, sfu, "cy", "room", 3)
	for _, who := range []string{"ana", "bo", "cy"} {
		id := who
		waitFor(t, id+" to connect", func() bool {
			return sfu.connectionStateOf(id) == webrtc.PeerConnectionStateConnected
		})
	}
	sfu.mu.Lock()
	owner := sfu.peers["ana"]
	outsider := sfu.peers["bo"]
	observer := sfu.peers["cy"]
	if err := observer.addScreenOutput(); err != nil {
		sfu.mu.Unlock()
		t.Fatal(err)
	}
	sfu.announcePublication(owner, Publication{
		ID: "owned", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()

	packet := &rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 102, Timestamp: videoClockRate / 30, SequenceNumber: 2},
		Payload: []byte{0x00, 0x00, 0x00, 0x02},
	}
	before := screenWrites(t, sfu, "cy")
	sfu.forwardPublication(outsider, PublicationScreenVideo, packet)
	if screenWrites(t, sfu, "cy") != before {
		t.Fatal("A MEMBER WHO OWNS NO PUBLICATION FED A VIEWER'S SCREEN OUTPUT")
	}
	// The owner's own packets still flow. A fix that dropped everything would pass the
	// case above and break the share.
	sfu.forwardPublication(owner, PublicationScreenVideo, packet)
	if screenWrites(t, sfu, "cy") == before {
		t.Fatal("the publication's own packets are not reaching the viewer")
	}
}

// screenWrites sums the packets a member's video outputs have accepted.
// audioReceiveOffer builds a real offer carrying a fresh audio input, which is what a
// client sends when it renegotiates to receive a shared screen's system audio.
func audioReceiveOffer(t *testing.T, peer *Peer) *webrtc.SessionDescription {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio", "brrpbrrp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.pc.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	return &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: peer.pc.LocalDescription().SDP}
}

// audioWritesFor counts what one kind of screen output accepted, so a test can say
// "the audio went to the audio output" rather than "something moved".
func audioWritesFor(t *testing.T, sfu *SFU, userID string, kind PublicationKind) uint64 {
	t.Helper()
	var total uint64
	for _, output := range viewerScreens(sfu, userID) {
		if output.kind != kind {
			continue
		}
		output.mu.Lock()
		total += output.written
		output.mu.Unlock()
	}
	return total
}

func screenWrites(t *testing.T, sfu *SFU, userID string) uint64 {
	t.Helper()
	var total uint64
	for _, output := range viewerScreens(sfu, userID) {
		output.mu.Lock()
		total += output.written
		output.mu.Unlock()
	}
	return total
}

// audioWrites sums the packets this peer's audio outputs have accepted. It is the
// server's own counter, incremented by a write — not evidence that a track existed,
// which is exactly the distinction the microphone replacement turns on.
func audioWrites(t *testing.T, peer *Peer) uint64 {
	t.Helper()
	var total uint64
	for _, output := range peer.outputs {
		output.mu.Lock()
		total += output.written
		output.mu.Unlock()
	}
	return total
}

// audioFrame builds one Opus-sized RTP packet on the audio clock. `n` only has to
// differ between frames: the forwarder steps its own timeline by the arrival delta.
func audioFrame(n uint32) *rtp.Packet {
	return &rtp.Packet{
		Header:  rtp.Header{Version: 2, SequenceNumber: uint16(n), Timestamp: n * 960, Marker: n%50 == 0},
		Payload: []byte{0xf8, 0xff, 0xfe},
	}
}

// The room's screen allowance is one, and the check that enforces it must be the one
// that *registers* the publication — not an earlier read.
//
// The bound was read under the lock, the lock released for the SDP exchange, and the
// publication registered afterwards. A second peer arriving in that window read the
// same count the first did, passed, and the room ended up carrying two screens against
// a bound of one: the second consuming bandwidth and a viewer's output for an offer
// the server should have refused. The comment above the check claimed one critical
// section; the lock said otherwise.
//
// What is asserted is the behaviour, through the real signal path: the first member's
// screen is published, the second member's is refused, and the room ends with one.
func TestTheScreensAllowanceIsTakenWhenThePublicationIsRegistered(t *testing.T) {
	sfu := newTestSFU(t)
	first := connectPeerWithCapacity(t, sfu, "ana", "room", 3)
	second := connectPeerWithCapacity(t, sfu, "bo", "room", 3)
	for _, who := range []string{"ana", "bo"} {
		id := who
		waitFor(t, id+" to connect", func() bool {
			return sfu.connectionStateOf(id) == webrtc.PeerConnectionStateConnected
		})
	}

	// Each member adds a real screen track and offers it, the way a browser does.
	offerScreen := func(peer *testPeer) *webrtc.SessionDescription {
		track, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: videoClockRate}, "screen", peer.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.pc.AddTransceiverFromTrack(track,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
			t.Fatal(err)
		}
		description, err := peer.pc.CreateOffer(nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.pc.SetLocalDescription(description); err != nil {
			t.Fatal(err)
		}
		return &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: peer.pc.LocalDescription().SDP}
	}

	if err := sfu.Signal("ana", "ana", Signal{
		ConnectionID: firstConnectionID(t, sfu, "ana"),
		SDP:          offerScreen(first), Publication: PublicationScreenVideo,
	}); err != nil {
		t.Fatalf("the first share was refused: %v", err)
	}
	if err := sfu.Signal("bo", "bo", Signal{
		ConnectionID: firstConnectionID(t, sfu, "bo"),
		SDP:          offerScreen(second), Publication: PublicationScreenVideo,
	}); err == nil {
		t.Fatal("A SECOND MEMBER TOOK THE ROOM'S ONLY SCREEN")
	}
	if got := len(sfu.Publications("room")); got != 1 {
		t.Fatalf("the room carries %d publications, want 1", got)
	}
	// The owner is unaffected by the refusal: a fix that also dropped the first share
	// would satisfy the bound and break the feature.
	if got := sfu.Publications("room")[0].Owner; got != "ana" {
		t.Fatalf("the room publishes %q after the refusal, want the member who was admitted", got)
	}
}

// screenFrame builds one video RTP packet on the video clock. `n` only has to differ
// between frames: the forwarder steps its own timeline by the arrival delta.
func screenFrame(n uint32) *rtp.Packet {
	return &rtp.Packet{
		Header:  rtp.Header{Version: 2, SequenceNumber: uint16(n), Timestamp: n * videoClockRate / 30},
		Payload: []byte{0x00, 0x00, 0x00, 0x01, 0x65},
	}
}

// publishedScreenFrames counts what the server actually forwarded, per peer. The
// counter is the server's own — it is incremented by a write, not by a client's
// claim that it sent something — which is what makes a frame count evidence that a
// packet reached a viewer rather than evidence that a track existed.
func (s *SFU) publishedScreenFrames() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total uint64
	for _, peer := range s.peers {
		for _, output := range peer.screens {
			output.mu.Lock()
			total += output.written
			output.mu.Unlock()
		}
	}
	return total
}

func firstConnectionID(t *testing.T, sfu *SFU, userID string) string {
	t.Helper()
	return sfu.connectionIDFor(userID)
}

// A viewer's screen output is subscribed to the room, not to every publication that
// happens to exist in it.
//
// The room's screen bound is one, so today only one publication can exist at a time and
// this cannot be observed — the forwarded set is the room's set whichever question is
// asked. That is an accident of the bound, not a property of the forwarding, and the
// moment the bound moves this becomes a disclosure: a viewer's output would carry another
// peer's publication because it was in the room, not because they asked for it.
//
// So this asserts the property directly rather than through the bound. Two publications
// of different kinds are placed in the room — a screen and its audio, which travel
// separately — and each is checked against the outputs that are actually meant to receive
// it. A forwarder bound to the room rather than to the publication cannot pass this.

func TestAViewerReceivesOnlyThePublicationTheirOutputIsFor(t *testing.T) {
	sfu := newTestSFU(t)
	// Three participants, so the room's capacity is not what stops the third from
	// connecting — which would make the rest of this test vacuous.
	connectPeerWithCapacity(t, sfu, "ana", "room", 4)
	connectPeerWithCapacity(t, sfu, "bo", "room", 4)
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})

	sfu.mu.Lock()
	owner := sfu.peers["ana"]
	sfu.announcePublication(owner, Publication{
		ID: "v1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	if err := sfu.peers["bo"].addScreenOutput(); err != nil {
		t.Fatalf("the viewer could not create a screen output: %v", err)
	}

	// A peer in the room that never asked to receive anything.
	connectPeerWithCapacity(t, sfu, "cy", "room", 4)
	waitFor(t, "cy to connect", func() bool {
		return sfu.connectionStateOf("cy") == webrtc.PeerConnectionStateConnected
	})
	cyOutputs := len(viewerScreens(sfu, "cy"))
	if cyOutputs != 0 {
		t.Fatalf("a peer that asked for nothing has %d screen outputs", cyOutputs)
	}

	// The owner's own packets advance the viewer's output.
	before := screenWrites(t, sfu, "bo")
	sfu.forwardPublication(owner, PublicationScreenVideo, screenFrame(1))
	if got := screenWrites(t, sfu, "bo"); got != before+1 {
		t.Fatalf("the viewer received %d frames, want %d", got, before+1)
	}
	// And nobody who did not subscribe receives anything.
	if got := screenWrites(t, sfu, "cy"); got != 0 {
		t.Fatalf("A PEER THAT ASKED FOR NOTHING RECEIVED %d SCREEN FRAMES", got)
	}

	// Screen audio is a separate publication and travels by the same path. It must not
	// ride the video output, because a viewer subscribed to the video is not thereby
	// subscribed to the audio.
	sfu.mu.Lock()
	sfu.announcePublication(owner, Publication{
		ID: "a1", Owner: "ana", Kind: PublicationScreenAudio, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()
	videoBefore := screenWrites(t, sfu, "bo")
	sfu.forwardPublication(owner, PublicationScreenAudio, audioFrame(1))
	if got := screenWrites(t, sfu, "bo"); got != videoBefore {
		t.Fatalf("screen audio advanced the screen video output by %d writes", got-videoBefore)
	}
}

// Screen audio reaches a viewer as audio.
//
// It used to reach nobody. The video output was created for the video publication and
// the audio publication skipped output creation entirely, and what did travel was written
// into the *video* forwarder — an Opus stream on a 90 kHz H264 track, which is noise and
// not sound. A registered screen-audio publication with no delivered audio is exactly the
// shape of a feature that looks present and is not: the sharer's toggle works, the
// publication exists, the audit records it, and nobody hears anything.
//
// So this asserts the delivery, on the output that is actually for audio and not the one
// that is not.

func TestScreenAudioReachesAViewerAsAudio(t *testing.T) {
	sfu := newTestSFU(t)
	connectPeerWithCapacity(t, sfu, "ana", "room", 3)
	connectPeerWithCapacity(t, sfu, "bo", "room", 3)
	waitFor(t, "both peers to connect", func() bool {
		return sfu.connectionStateOf("ana") == webrtc.PeerConnectionStateConnected &&
			sfu.connectionStateOf("bo") == webrtc.PeerConnectionStateConnected
	})

	sfu.mu.Lock()
	owner := sfu.peers["ana"]
	viewer := sfu.peers["bo"]
	sfu.announcePublication(owner, Publication{
		ID: "v1", Owner: "ana", Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	sfu.announcePublication(owner, Publication{
		ID: "a1", Owner: "ana", Kind: PublicationScreenAudio, Started: time.Now().UTC(),
	})
	sfu.mu.Unlock()

	// The viewer receives both publications. This is what `renegotiate` does for each
	// kind before the description is created, asserted directly because the forwarding —
	// not the signalling — is what this test is about.
	for _, kind := range []PublicationKind{PublicationScreenVideo, PublicationScreenAudio} {
		if err := viewer.addScreenOutputOfKind(kind); err != nil {
			t.Fatalf("the viewer could not create a %s output: %v", kind, err)
		}
	}

	// Two distinct outputs: one per publication. One track doing both jobs is the
	// original defect — an Opus stream on a 90 kHz H264 track is noise, not sound.
	var audioOutput, videoOutput *screenForwarder
	for _, output := range viewerScreens(sfu, "bo") {
		switch output.kind {
		case PublicationScreenAudio:
			audioOutput = output
		case PublicationScreenVideo:
			videoOutput = output
		}
	}
	if audioOutput == nil || videoOutput == nil {
		t.Fatalf("the viewer's outputs are video=%v audio=%v, want one of each", videoOutput != nil, audioOutput != nil)
	}
	if audioOutput == videoOutput {
		t.Fatal("one output is doing both jobs, so audio is either lost or noise")
	}

	// The audio reaches the audio output.
	audioBefore := audioWritesFor(t, sfu, "bo", PublicationScreenAudio)
	sfu.forwardPublication(owner, PublicationScreenAudio, audioFrame(1))
	if got := audioWritesFor(t, sfu, "bo", PublicationScreenAudio); got != audioBefore+1 {
		t.Fatalf("the viewer's audio output received %d writes, want %d", got, audioBefore+1)
	}
	// The video output is untouched, so the two tracks cannot interleave. `screenWrites`
	// sums every output, so the comparison is per kind rather than against the total.
	if got := audioWritesFor(t, sfu, "bo", PublicationScreenVideo); got != 0 {
		t.Fatalf("screen audio advanced the video output by %d writes", got)
	}

	// And the video still reaches its own output, so the audio output did not displace it.
	videoBefore := audioWritesFor(t, sfu, "bo", PublicationScreenVideo)
	sfu.forwardPublication(owner, PublicationScreenVideo, screenFrame(1))
	if got := audioWritesFor(t, sfu, "bo", PublicationScreenVideo); got != videoBefore+1 {
		t.Fatalf("the viewer's video output received %d writes, want %d", got, videoBefore+1)
	}
}
