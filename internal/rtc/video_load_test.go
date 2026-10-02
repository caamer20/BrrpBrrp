package rtc

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// The video-forwarding measurement ADR 0019 is conditional on.
//
// The decision was to extend the embedded SFU rather than introduce a separate one,
// on the grounds that the control plane is already built and that the gap is in the
// media core. That reasoning is only sound while the *cost* of the media core is
// known. The master plan's own arithmetic puts two 2.5 Mbit/s screen shares
// forwarded to nine viewers at roughly 45 Mbit/s of egress from one process that is
// also serving HTTP, SQLite and WebSocket, and the plan says plainly: "Do not label
// this arithmetic a load-test result."
//
// So this is a load test, and it reports what it measured rather than what the
// arithmetic said.
//
// What it can and cannot establish is stated in the report it writes. It runs in one
// process with synthetic RTP, so it measures the *forwarding path's* cost — packets
// copied, headers rewritten, tracks written — and not the media pipeline around it.
// No claim is made about a decoder, a network, a browser or a codec.

// VideoLoadSpec is one measured combination. The defaults are the master plan's
// v1 targets: two screen shares, nine remote viewers each, at 2.5 Mbit/s.
type VideoLoadSpec struct {
	// ScreenPublishers is how many participants share a screen.
	ScreenPublishers int
	// ViewersPerShare is how many remote participants watch each share.
	ViewersPerShare int
	// BitrateKbps is the encoded video rate per publisher.
	BitrateKbps int
	// Seconds is the steady-state duration.
	Seconds int
}

// VideoLoadResult is what was measured. The fields are deliberately the ones a
// capacity decision needs and the ones this harness can honestly produce.
type VideoLoadResult struct {
	Spec             VideoLoadSpec `json:"spec"`
	Publishers       int           `json:"publishers"`
	Viewers          int           `json:"viewers"`
	DurationSeconds  float64       `json:"duration_seconds"`
	PacketsForwarded uint64        `json:"packets_forwarded"`
	BytesForwarded   uint64        `json:"bytes_forwarded"`
	MeanEgressMbit   float64       `json:"mean_egress_mbit_per_second"`
	PeakEgressMbit   float64       `json:"peak_egress_mbit_per_second"`
	// ExpectedEgressMbit is the master plan's arithmetic for the same combination.
	// It is recorded beside the measurement so a reader can see how far the real
	// figure is from the estimate — and it is never presented as the measurement.
	ExpectedEgressMbit float64  `json:"plan_arithmetic_mbit_per_second"`
	PeakHeapBytes      uint64   `json:"peak_server_heap_bytes"`
	MeanGoroutines     int      `json:"mean_goroutines"`
	NotEstablished     []string `json:"not_established"`
}

func defaultVideoLoadSpec() VideoLoadSpec {
	return VideoLoadSpec{ScreenPublishers: 2, ViewersPerShare: 9, BitrateKbps: 2500, Seconds: 30}
}

// videoFrameBytes is the payload of one encoded video frame. A 2.5 Mbit/s stream at
// thirty frames a second is about ten kilobytes per frame including overhead, and a
// frame of that size is what the forwarder actually copies.
const (
	// videoPacketBytes is one RTP payload. A video frame is several of these, so the
	// harness is measured in packets and the bitrate is computed from what it sent —
	// which is what a real encoder does, rather than what a payload size suggests.
	videoPacketBytes = 1200
	videoFrameRate   = 30
	// packetsPerFrame is how many packets carry one frame at 2.5 Mbit/s and thirty
	// frames a second: 2,500,000 bits / 8 / 30 / 1,200 bytes is about 8.7, rounded up
	// so the measured rate is at or slightly above the target rather than below it.
	packetsPerFrame = 9
)

// TestVideoForwardingLoad measures the cost of the screen-forwarding path at the
// master plan's combination.
//
// This is opt-in for the same reason the audio workload is: a measurement that runs
// in every ordinary test run is a measurement nobody reads, and a 30-second video
// workload adds real time to every suite.
func TestVideoForwardingLoad(t *testing.T) {
	seconds, _ := strconv.Atoi(os.Getenv("BRRP_VIDEO_LOAD_SECONDS"))
	if seconds < 5 || seconds > 1800 {
		t.Skip("set BRRP_VIDEO_LOAD_SECONDS=5..1800 to measure the screen-forwarding path")
	}
	spec := defaultVideoLoadSpec()
	if raw := os.Getenv("BRRP_VIDEO_BITRATE_KBPS"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			spec.BitrateKbps = value
		}
	}
	spec.Seconds = seconds
	result := runVideoForwardingLoad(t, spec)
	if t.Failed() {
		return
	}
	report := fmt.Sprintf(
		"\nvideo forwarding load: %d publishers x %d viewers at %d kbit/s for %.1fs\n"+
			"  packets forwarded: %d\n"+
			"  mean egress: %.2f Mbit/s (the plan's arithmetic for this combination: %.2f)\n"+
			"  peak egress: %.2f Mbit/s\n"+
			"  peak server heap: %.1f MiB, mean goroutines: %d",
		result.Publishers, result.Viewers, spec.BitrateKbps, result.DurationSeconds,
		result.PacketsForwarded, result.MeanEgressMbit, result.ExpectedEgressMbit,
		result.PeakEgressMbit, float64(result.PeakHeapBytes)/(1<<20), result.MeanGoroutines)
	for _, note := range result.NotEstablished {
		report += "\n  not established: " + note
	}
	t.Log(report)
	if path := os.Getenv("BRRP_VIDEO_LOAD_REPORT"); path != "" {
		writeVideoLoadReport(t, path, result)
	}
}

// runVideoForwardingLoad builds the combination and measures it.
//
// The publishers are real peer connections publishing real RTP through the real
// forwarder. What is synthetic is the payload's *content* — a frame of zeros rather
// than an encoded picture — because the forwarder's cost is per packet and per byte
// copied, neither of which depends on what is inside.
func runVideoForwardingLoad(t *testing.T, spec VideoLoadSpec) VideoLoadResult {
	t.Helper()
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sfu.Close()

	// The combination is capped at what the server admits. Serverwide that is twenty
	// peers (MaxServerParticipants); one room carries at most ten
	// (MaxRoomParticipants). The plan's profile — two publishers with nine viewers
	// each, twenty in total, in two rooms of ten — is exactly the serverwide bound,
	// so it is reachable as stated and measured as stated rather than at a reduced
	// combination (review 17).
	if spec.ScreenPublishers+spec.ScreenPublishers*spec.ViewersPerShare > MaxServerParticipants {
		spec.ScreenPublishers = 1
		spec.ViewersPerShare = MaxServerParticipants - 1
	}
	if spec.ViewersPerShare+1 > MaxRoomParticipants {
		spec.ViewersPerShare = MaxRoomParticipants - 1
	}
	total := spec.ScreenPublishers + spec.ScreenPublishers*spec.ViewersPerShare
	groupSize := spec.ViewersPerShare + 1
	peers := make([]*testPeer, 0, total)
	for index := range total {
		// Each publisher leads one room, and its viewers follow it: the arrangement the
		// plan's profile describes, with no room above the per-room bound. The
		// publishers come first in this slice, so a viewer's room is derived from how
		// far it sits past them.
		room := fmt.Sprintf("load-%d", index)
		if index >= spec.ScreenPublishers {
			room = fmt.Sprintf("load-%d", (index-spec.ScreenPublishers)/spec.ViewersPerShare)
		}
		peers = append(peers, connectPeerWithCapacity(t, sfu, fmt.Sprint(index), room, groupSize))
	}

	// Each publisher announces a screen publication and is given a screen output, so
	// the packets take the video forwarder rather than the audio one.
	//
	// The test peer and the SFU's peer are different types, so the SFU peer is looked
	// up by account rather than carried across: the harness sets up the tracks and the
	// server resolves the publication, which is the order the real path uses.
	publishers := peers[:spec.ScreenPublishers]
	for index, publisher := range publishers {
		publisher.setScreenTrack(t)
		// The offer first: the server can only answer an offer it has been sent, and
		// a screen track that was added without one never reaches the forwarder.
		offer, err := publisher.pc.CreateOffer(nil)
		if err != nil {
			t.Fatalf("screen %d could not be offered: %v", index, err)
		}
		if err := publisher.pc.SetLocalDescription(offer); err != nil {
			t.Fatalf("screen %d could not be described: %v", index, err)
		}
		if err := sfu.Signal(publisher.name, publisher.name, Signal{
			ConnectionID: sfu.connectionIDFor(publisher.name),
			SDP:          &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: publisher.pc.LocalDescription().SDP},
			Publication:  PublicationScreenVideo,
		}); err != nil {
			t.Fatalf("screen %d could not be announced: %v", index, err)
		}
	}

	viewers := peers[spec.ScreenPublishers:]
	// The forwarder state each viewer's output was last seen at, so a pass measures the
	// delta rather than re-counting what earlier passes already counted.
	sampled := map[*screenForwarder]uint64{}
	sampledBytes := map[*screenForwarder]uint64{}
	// Every viewer is given a screen output. The harness does not renegotiate to
	// *receive* on it, because a receive-only renegotiation is not currently something
	// the signal path accepts — a client asking to watch a screen is treated as a
	// second publisher, which is a real gap recorded below rather than one this
	// measurement papers over. What is measured is what the server writes to each
	// viewer's output, which is the cost ADR 0019 asks about.
	for _, viewer := range viewers {
		sfu.mu.Lock()
		if server := sfu.peers[viewer.name]; server != nil {
			if err := server.addScreenOutput(); err != nil {
				sfu.mu.Unlock()
				t.Fatalf("viewer %s could not be given a screen output: %v", viewer.name, err)
			}
		}
		sfu.mu.Unlock()
	}
	interval := time.Second / videoFrameRate
	framePayload := make([]byte, videoPacketBytes)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(time.Duration(spec.Seconds) * time.Second)
	started := time.Now()
	samples := 0
	var forwarded, bytes uint64
	goroutineTotal, goroutineSamples := 0, 0
	var peakHeap uint64
	// The egress figure is sampled rather than computed from the nominal bitrate, so a
	// forwarder that drops packets shows up as a lower measured rate rather than being
	// reported at the rate the arithmetic predicted.
	previous := started
	windowBytes := uint64(0)
	peakWindow := 0.0

	for {
		now := time.Now()
		if now.After(deadline) {
			break
		}
		select {
		case <-ticker.C:
			for _, publisher := range publishers {
				// One frame is several packets. A single oversized payload is refused
				// by the transport as a short buffer, which is the packet path working
				// rather than a failure.
				for packet := range packetsPerFrame {
					framePayload[0] = byte(packet)
					if err := publisher.sendVideo(framePayload); err != nil {
						t.Fatalf("publish: %v", err)
					}
					samples++
				}
			}
		default:
		}

		// What the server actually wrote, read from its own forwarders under their
		// lock. Sampling the difference since the last pass means the figure is a rate
		// over the window rather than a total divided by an elapsed time, so a forwarder
		// that dropped a packet shows up as a lower measured rate.
		for _, viewer := range viewers {
			sfu.mu.Lock()
			server := sfu.peers[viewer.name]
			sfu.mu.Unlock()
			if server == nil {
				continue
			}
			for _, output := range server.screenOutputs() {
				output.mu.Lock()
				forwarded += output.written - sampled[output]
				bytes += output.bytes - sampledBytes[output]
				windowBytes += output.bytes - sampledBytes[output]
				sampled[output] = output.written
				sampledBytes[output] = output.bytes
				output.mu.Unlock()
			}
		}
		if now.Sub(previous) >= time.Second {
			rate := float64(windowBytes*8) / float64(now.Sub(previous).Seconds()) / 1e6
			if rate > peakWindow {
				peakWindow = rate
			}
			windowBytes = 0
			previous = now
		}
		if samples%300 == 0 {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			if stats.Alloc > peakHeap {
				peakHeap = stats.Alloc
			}
			goroutineTotal += runtime.NumGoroutine()
			goroutineSamples++
		}
	}
	elapsed := time.Since(started).Seconds()
	meanGoroutines := 0
	if goroutineSamples > 0 {
		meanGoroutines = goroutineTotal / goroutineSamples
	}
	var finalHeap uint64
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	finalHeap = stats.Alloc
	if finalHeap > peakHeap {
		peakHeap = finalHeap
	}

	// The plan's arithmetic: every publisher's stream forwarded to every viewer in the
	// room, which is what the SFU does — it forwards to the room, not to one share's
	// viewers. That is the honest comparison and it is larger than the "two shares to
	// nine viewers each" figure, because the product forwards a screen to everybody in
	// the room.
	viewersPerPublisher := spec.ViewersPerShare + spec.ScreenPublishers - 1
	expected := float64(spec.ScreenPublishers) * float64(viewersPerPublisher) * float64(spec.BitrateKbps) / 1000

	result := VideoLoadResult{
		Spec:               spec,
		Publishers:         spec.ScreenPublishers,
		Viewers:            spec.ViewersPerShare,
		DurationSeconds:    elapsed,
		PacketsForwarded:   forwarded,
		BytesForwarded:     bytes,
		MeanEgressMbit:     float64(bytes*8) / elapsed / 1e6,
		PeakEgressMbit:     peakWindow,
		ExpectedEgressMbit: expected,
		PeakHeapBytes:      peakHeap,
		MeanGoroutines:     meanGoroutines,
		NotEstablished: []string{
			"synthetic RTP in one process: the forwarding path's cost, not an encoded video pipeline, a network, a browser or a codec",
			"the server's writes to each viewer's output are counted, not a client's receipt of them: a receive-only renegotiation is not currently accepted by the signal path, so no client receives a screen through this harness",
			"the combination is the plan's profile — two publishers with nine viewers each, twenty peers in two rooms of ten — measured at the serverwide bound; the forwarding figures are the server's writes to each viewer's output, not a client's receipt",
			"loopback: no NIC, no TURN relay, and no measure of the network the plan's budgets are stated for",
			"no decoder and no client, so nothing here says a picture arrives intact",
			"not run on the reference allocation (four cores, 8 GiB) and not run on either required deployment",
		},
	}
	if forwarded == 0 {
		t.Fatal("no packets were forwarded, so the measurement describes nothing")
	}
	return result
}

// setScreenTrack gives a test peer a video track and an OnTrack sink for the frames
// that come back, which is what makes the forwarded bytes observable at all.
func (peer *testPeer) setScreenTrack(t *testing.T) {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: videoClockRate}, "screen", peer.name+"-screen")
	if err != nil {
		t.Fatal(err)
	}
	peer.videoTrack = track
	if _, err := peer.pc.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}); err != nil {
		t.Fatal(err)
	}
	peer.pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			select {
			case peer.videoPackets <- packet.Payload:
			default:
				// A full queue means the harness is not draining fast enough, which is
				// itself the finding; dropping here keeps the measurement from changing
				// the thing being measured.
			}
		}
	})
}

// sendVideo writes one frame on a peer's screen track, on the video clock.
func (peer *testPeer) sendVideo(payload []byte) error {
	if peer.videoTrack == nil {
		return fmt.Errorf("peer has no screen track")
	}
	peer.videoSequence++
	peer.videoTimestamp += videoClockRate / videoFrameRate
	return peer.videoTrack.WriteRTP(&rtp.Packet{
		Header: rtp.Header{
			Version: 2, SequenceNumber: peer.videoSequence,
			Timestamp: peer.videoTimestamp, PayloadType: 102,
		},
		Payload: payload,
	})
}

// writeVideoLoadReport records the measurement as JSON, so the figure that a decision
// rests on is attributable rather than something somebody read off a log and typed
// into a document. The `not_established` list travels inside the file for the same
// reason: a number separated from its limits is a number that gets quoted without them.
func writeVideoLoadReport(t *testing.T, path string, result VideoLoadResult) {
	t.Helper()
	encoded, err := json.MarshalIndent(struct {
		MeasuredAt  string          `json:"measured_at"`
		GoVersion   string          `json:"go_version"`
		NumCPU      int             `json:"host_num_cpu"`
		Concurrency int             `json:"host_gomaxprocs"`
		Result      VideoLoadResult `json:"result"`
	}{time.Now().UTC().Format(time.RFC3339), runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0), result}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
