package rtc

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// MaxServerParticipants bounds the whole process's voice peers, and MaxRoomParticipants
// the largest room it will admit. They are separate because they are separate facts:
// a room's size is a conversation's decision (the channel's own bound up to ten, or a
// private call's two), while the serverwide number is what the process will carry —
// twenty peers forwarding media at once (master plan, reference capacity). Treating one
// number as both made the serverwide ceiling ten, which the plan's twenty-participant
// profile never had room to reach (review 17).
const (
	MaxServerParticipants = 20
	MaxRoomParticipants   = 10
)

type Options struct {
	// Log records which network path carried each call. It is optional: media tests
	// and a bare embedder do not want a logger, and a call that connects is not a
	// failure that needs inventing a logger to report.
	Log *slog.Logger
	// ForceRelay forbids direct candidates on the *server* side too. Setting it on
	// clients alone is half an instruction, and ICE is free to pair a relayed client
	// with a direct server.
	ForceRelay bool

	ExternalIP       string
	PortMin, PortMax uint16
	ICEServers       []webrtc.ICEServer
}

type State struct {
	UserID        string `json:"user_id"`
	ChannelID     string `json:"channel_id"`
	Muted         bool   `json:"muted"`
	Deafened      bool   `json:"deafened"`
	Speaking      bool   `json:"speaking"`
	AdminMuted    bool   `json:"admin_muted"`
	AdminDeafened bool   `json:"admin_deafened"`
}

type Signal struct {
	ConnectionID string                     `json:"connection_id,omitempty"`
	SDP          *webrtc.SessionDescription `json:"sdp,omitempty"`
	ICE          *webrtc.ICECandidateInit   `json:"ice,omitempty"`
	// Publication names what this offer is adding. It is empty for the initial join
	// and for an ICE-only message. A later offer that names a publication is a
	// *renegotiation* — a client adding a track to a call that is already up — which
	// the previous code refused outright, so there was no way to share a screen into a
	// live call at all.
	Publication PublicationKind `json:"publication,omitempty"`
	// PublicationID is the id the new track is published under, chosen by the server
	// and returned to the client so a reconnecting client can tell a resume from a
	// new publication that reused a slot.
	PublicationID PublicationID `json:"publication_id,omitempty"`
	// PublicationStopped withdraws a publication. It is a separate field rather than an
	// empty `publication`, because "stop sharing" and "share something" are different
	// intents and reading one as the other would refuse a stop as an unnamed offer.
	PublicationStopped bool `json:"publication_stopped,omitempty"`
}

type JoinEvent struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

type Peer struct {
	owner   string
	state   State
	slot    int
	pc      *webrtc.PeerConnection
	outputs []*forwarder
	// screens are this peer's video outputs, created when a screen publication is
	// accepted rather than up front. The audio outputs are pre-created because a
	// participant's microphone is known at join; a screen is not, and pre-creating
	// nine video tracks for every participant would negotiate video nobody asked for.
	screens      []*screenForwarder
	send         func(string, any)
	receiving    atomic.Bool
	signalMu     sync.Mutex
	pendingICE   []webrtc.ICECandidateInit
	joinTimer    *time.Timer
	announced    bool
	resumeToken  string
	connectionID string
	detachTimer  *time.Timer
	sendMu       sync.RWMutex
	// speakDenied records that this account may listen but not transmit. It is
	// decided by the caller from the voice.speak capability and enforced here, so
	// a client's own state message cannot turn transmission back on.
	speakDenied bool
	// Keep client intent separate from effective state. Permission withdrawal
	// must not become a permanent self-mute, and regrant must not erase a mute
	// the member deliberately selected while permission was absent.
	requestedMuted    bool
	requestedSpeaking bool
}

// refreshState is called with SFU.mu held after client intent or a server-side
// restriction changes. Speaking is presentation state; forward still enforces
// every independent mute/deafen/permission restriction for each RTP packet.
func (p *Peer) refreshState() {
	p.state.Muted = p.requestedMuted || p.speakDenied
	p.state.Speaking = p.requestedSpeaking && !p.state.Muted && !p.state.Deafened && !p.state.AdminMuted && !p.state.AdminDeafened
}

func (p *Peer) emit(kind string, data any) {
	p.sendMu.RLock()
	send := p.send
	p.sendMu.RUnlock()
	if send != nil {
		send(kind, data)
	}
}

// attached reports whether this peer still has a signaling connection. A detached peer
// (Detach, when its websocket goes away and its session is still valid) keeps its media
// connection and its resume token but has no sink, so nothing can be delivered to it.
func (p *Peer) attached() bool {
	p.sendMu.RLock()
	defer p.sendMu.RUnlock()
	return p.send != nil
}

type SFU struct {
	mu             sync.Mutex
	api            *webrtc.API
	options        Options
	peers          map[string]*Peer
	changed        func([]State)
	routingVersion uint64
	// publications is keyed by room. A room is a channel for a community call and a
	// conversation for a private one (ADR 0019, §5), so the key is an opaque room
	// string and nothing here assumes a channel row exists.
	publications map[string]*publicationRegistry
	// limits is the room-and-server publication bound. It is read under mu and never
	// changed after construction, so a peer cannot negotiate against a bound that
	// moved underneath it.
	limits PublicationLimits
	// log records which network path actually carried each call. It may be nil.
	log *slog.Logger
}

// h264Codecs are the video profiles this build offers. Several are registered because
// browsers disagree about which one a screen capture produces, and registering only
// the preferred profile makes a share fail on a machine that would otherwise work.
func h264Codecs() []webrtc.RTPCodecParameters {
	profiles := []string{
		"42e01f", // baseline
		"4d001f", // main
		"640c1f", // high
	}
	parameters := make([]webrtc.RTPCodecParameters, 0, len(profiles))
	for index, profile := range profiles {
		parameters = append(parameters, webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
				SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + profile,
			},
			PayloadType: webrtc.PayloadType(102 + index),
		})
	}
	return parameters
}

func New(options Options, changed func([]State)) (*SFU, error) {
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0"}, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	// Video is registered, not merely accepted. A second offer for a screen track
	// fails at the media engine if the codec is unknown, so "the server tolerates a
	// renegotiation" and "the server can carry a screen" are different claims and only
	// the second one is useful.
	//
	// H.264 rather than VP8: it is what a browser's getDisplayMedia produces most
	// reliably, and what the hardware encoders on the supported platforms accept. The
	// profile-level-id is left to the default so a stream that does not match the
	// preferred profile falls back rather than failing the whole connection.
	for _, parameters := range h264Codecs() {
		if err := media.RegisterCodec(parameters, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(media, registry); err != nil {
		return nil, err
	}
	settings := webrtc.SettingEngine{}
	if options.PortMin != 0 {
		if err := settings.SetEphemeralUDPPortRange(options.PortMin, options.PortMax); err != nil {
			return nil, err
		}
	}
	if options.ExternalIP != "" {
		settings.SetNAT1To1IPs([]string{options.ExternalIP}, webrtc.ICECandidateTypeHost)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(settings))
	return &SFU{
		api: api, options: options, peers: make(map[string]*Peer), changed: changed,
		publications: make(map[string]*publicationRegistry),
		limits:       DefaultPublicationLimits(),
		log:          options.Log,
	}, nil
}

func (s *SFU) Join(userID, owner, channelID string, capacity int, send func(string, any)) error {
	s.mu.Lock()
	// A peer whose signaling connection has gone is *detached*: its sink is nil, its
	// client is gone, and its resume token is the only way back to it. It must not hold
	// the account's voice place. Refusing the new connection because of one told a
	// person who reloaded the page or closed the tab and came straight back "you are
	// already in voice, possibly in another tab" — false, unactionable, and lasting
	// exactly as long as the drain window (review 17 handoff; the browser journeys hit
	// it intermittently as a join that never became a call).
	//
	// The loop is written this way because releasing the peer takes the lock that this
	// decision needs, so it is released, the old peer is torn down, and the decision is
	// made again on the state that is left: another join racing this one lands here too,
	// and whichever arrives second sees a peer that now exists and is attached.
	for {
		existing := s.peers[userID]
		if existing == nil {
			break
		}
		if existing.attached() {
			s.mu.Unlock()
			return errors.New("you are already in voice, possibly in another tab")
		}
		s.mu.Unlock()
		s.leavePeerOwned(userID, existing, existing.owner)
		s.mu.Lock()
	}
	if len(s.peers) >= MaxServerParticipants {
		s.mu.Unlock()
		return errors.New("the server's voice places are all occupied")
	}
	used := make(map[int]bool)
	for _, peer := range s.peers {
		if peer.state.ChannelID == channelID {
			used[peer.slot] = true
		}
	}
	if len(used) >= capacity {
		s.mu.Unlock()
		return errors.New("this voice channel is full")
	}
	slot := 0
	for used[slot] {
		slot++
	}
	// The transport policy is the *server's* to set, not only the client's.
	//
	// Telling a browser to use only the relay is half an instruction: if the server
	// keeps offering direct candidates, ICE is free to pair a relayed client with a
	// direct server and the call runs peer-to-peer anyway. An operator who switched
	// forced relay on for a network where direct paths do not work would have calls
	// that appear to work locally and fail for everybody else — the exact failure the
	// setting exists to prevent, and one that no client-side test can see.
	configuration := webrtc.Configuration{ICEServers: s.options.ICEServers}
	if s.options.ForceRelay {
		configuration.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	pc, err := s.api.NewPeerConnection(configuration)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	peer := &Peer{owner: owner, resumeToken: uuid.NewString(), connectionID: uuid.NewString(), state: State{UserID: userID, ChannelID: channelID}, slot: slot, pc: pc, send: send}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		s.mu.Unlock()
		pc.Close()
		return err
	}
	for index := 0; index < MaxRoomParticipants-1; index++ {
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, fmt.Sprintf("audio-%d", index), fmt.Sprintf("voice-%d", index))
		if err != nil {
			s.mu.Unlock()
			pc.Close()
			return err
		}
		transceiver, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if err != nil {
			s.mu.Unlock()
			pc.Close()
			return err
		}
		peer.outputs = append(peer.outputs, &forwarder{track: track})
		go func(sender *webrtc.RTPSender) {
			for {
				if _, _, err := sender.ReadRTCP(); err != nil {
					return
				}
			}
		}(transceiver.Sender())
	}
	peer.joinTimer = time.AfterFunc(30*time.Second, func() {
		if peer.pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
			s.leavePeer(userID, peer)
		}
	})
	s.peers[userID] = peer
	s.mu.Unlock()
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			ice := candidate.ToJSON()
			peer.emit("rtc.signal", Signal{ICE: &ice, ConnectionID: peer.connectionID})
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			s.announceJoin(userID, peer)
			// A connected connection says *something* carried the call, and never says
			// what. Recording the selected pair's type is the difference between a call
			// that worked and a call that worked over TURN — which is the difference
			// between a working deployment and a working deployment that will fail the
			// day the direct path stops being available.
			//
			// It is read once, shortly after connecting, and logged. Reading it is
			// cheap; waiting for a later transition and reading again would be a
			// polling loop for a value that settles.
			go func() {
				time.Sleep(500 * time.Millisecond)
				local, remote, err := s.selectedCandidatePair(peer)
				if err != nil {
					return
				}
				if s.log == nil {
					return
				}
				s.log.Info("voice path established",
					"user_id", userID, "room", peer.state.ChannelID,
					"local", local, "remote", remote,
					"relayed", local == "relay" || remote == "relay")
			}()
		} else if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			s.leavePeer(userID, peer)
		}
	})
	// A peer may now carry more than one track, and the kind of the arriving track
	// decides what it is rather than the order it arrived in. The previous code
	// accepted the first audio track and silently dropped everything after it, which
	// is why a screen — negotiated successfully, announced, and then discarded at the
	// first packet — could never actually be seen.
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		// Which publication a track belongs to is a property of what the client
		// negotiated, not of its codec. The media kind alone cannot answer it: screen
		// audio is an audio track, exactly like the microphone, and telling them apart
		// here is what keeps sharing a screen's system audio from being mistaken for a
		// replacement microphone.
		kind := s.publicationForTrack(peer, track.Kind())
		if kind == "" {
			// A track this build does not carry, or one the peer published nothing for.
			// Dropped rather than forwarded into an audio forwarder, which would rewrite
			// its sequence numbers with audio's clock and produce noise rather than a
			// picture.
			return
		}
		if kind == PublicationMicrophone && !peer.receiving.CompareAndSwap(false, true) {
			// A second audio input on the microphone's own publication. This is the one
			// case that is a replacement rather than a silence: a member who plugs in a
			// headset mid-call has a track that connected, a level meter that moves, and
			// no audio for anybody else. The newcomer takes the connection's single
			// output; the previous input stops the moment it does.
			peer.takeMicrophone()
		}
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			s.forwardPublication(peer, kind, packet)
		}
	})
	s.publish()
	return nil
}

// takeMicrophone hands this connection's single audio output to a microphone track
// that arrived while another was still live.
//
// The reason this is a replacement rather than a refusal is that the alternative is
// silence. A member who plugs in a headset, switches to a USB microphone, or whose
// browser reopens the input after a device change gets no audio at all, and nothing
// in the interface tells them why: the track connected, the level meter moved, and
// no other member can hear them. Refusing the newcomer and continuing to forward the
// old input would be worse - it would forward a microphone nobody is using.
//
// One connection carries one audio input. The outgoing track stops being read the
// moment the newcomer claims the output, so the previous microphone's packets end
// rather than interleave with the new one's.
//
// This deliberately emits nothing. No client in this build can reach this path — a
// device change goes through `replaceTrack`, which reuses the connection's existing
// transceiver and so never produces a second track — and an event with no handler is
// a claim about the interface that nobody made. If a client is taught to add a
// second microphone transceiver, the signal belongs with that change.
func (p *Peer) takeMicrophone() {}

func (s *SFU) announceJoin(userID string, peer *Peer) {
	s.mu.Lock()
	if s.peers[userID] != peer || peer.announced {
		s.mu.Unlock()
		return
	}
	peer.announced = true
	event := JoinEvent{UserID: userID, ChannelID: peer.state.ChannelID}
	var recipients []func(string, any)
	for _, destination := range s.peers {
		if destination.state.ChannelID == event.ChannelID && destination.announced {
			recipients = append(recipients, destination.emit)
		}
	}
	s.mu.Unlock()
	// Notify the participating tabs only, after the media connection succeeds.
	for _, send := range recipients {
		send("voice.user_joined", event)
	}
}

func (s *SFU) forward(source *Peer, packet *rtp.Packet) {
	s.mu.Lock()
	// A listen-only member's audio is dropped here rather than trusted to the
	// client: forwarding is the boundary, not the state the client reported.
	if s.peers[source.state.UserID] != source || source.speakDenied || source.state.Muted || source.state.Deafened || source.state.AdminMuted || source.state.AdminDeafened {
		s.mu.Unlock()
		return
	}
	outputs := make([]*forwarder, 0, 9)
	for _, destination := range s.peers {
		if destination == source || destination.state.ChannelID != source.state.ChannelID || destination.state.Deafened || destination.state.AdminDeafened {
			continue
		}
		slot := source.slot
		if slot > destination.slot {
			slot--
		}
		outputs = append(outputs, destination.outputs[slot])
	}
	sourceID := source.state.UserID
	for _, output := range outputs {
		output.write(sourceID, packet)
	}
	s.mu.Unlock()
}

func (s *SFU) Signal(userID, owner string, signal Signal) error {
	// A withdrawal is answered before anything else, because it must work even after a
	// client has lost the ability to publish: somebody whose permission was withdrawn
	// still has to be able to stop sending.
	if signal.PublicationStopped {
		return s.stopPublication(userID, signal)
	}
	s.mu.Lock()
	peer := s.peers[userID]
	owned := peer != nil && peer.owner == owner
	s.mu.Unlock()
	if !owned {
		return errors.New("join voice in this tab first")
	}
	if signal.ConnectionID != "" && signal.ConnectionID != peer.connectionID {
		return nil
	}
	peer.signalMu.Lock()
	defer peer.signalMu.Unlock()
	// A publication can be named without a description when its media is already
	// negotiated: the screen's audio track travelled inside the screen's own offer, and
	// naming it is what makes the room's forwarder route it. Admission and registration
	// live in the renegotiation path, so the naming is routed there rather than
	// dropped here. Any other publication without an offer is refused: there would be
	// no m-line carrying it (review 17, two-browser screen journey).
	if signal.SDP == nil && signal.Publication != "" {
		if signal.Publication != PublicationScreenAudio {
			return errors.New("a renegotiation needs an offer")
		}
		return s.renegotiate(peer, signal)
	}
	if signal.SDP != nil {
		if signal.SDP.Type != webrtc.SDPTypeOffer || len(signal.SDP.SDP) > 50000 {
			return errors.New("invalid voice offer")
		}
		// A second offer is a renegotiation, and it is how a screen is added to a call
		// that is already up. The first offer is the join and is unconditional; later
		// ones carry a publication kind, are checked against the bounds and against
		// this account's own ability to transmit, and go through a serialized queue so
		// two clients renegotiating at once cannot interleave their SDP.
		if peer.pc.RemoteDescription() != nil {
			return s.renegotiate(peer, signal)
		}
		if err := peer.pc.SetRemoteDescription(*signal.SDP); err != nil {
			return err
		}
		for _, candidate := range peer.pendingICE {
			if err := peer.pc.AddICECandidate(candidate); err != nil {
				return err
			}
		}
		peer.pendingICE = nil
		answer, err := peer.pc.CreateAnswer(nil)
		if err != nil {
			return err
		}
		if err := peer.pc.SetLocalDescription(answer); err != nil {
			return err
		}
		peer.emit("rtc.signal", Signal{SDP: peer.pc.LocalDescription(), ConnectionID: peer.connectionID})
	}
	if signal.ICE != nil {
		if len(signal.ICE.Candidate) > 2048 {
			return errors.New("invalid ICE candidate")
		}
		if peer.pc.RemoteDescription() == nil {
			if len(peer.pendingICE) >= 32 {
				return errors.New("too many ICE candidates")
			}
			peer.pendingICE = append(peer.pendingICE, *signal.ICE)
			return nil
		}
		return peer.pc.AddICECandidate(*signal.ICE)
	}
	return nil
}

// SetForceRelay changes the transport policy for connections made from now on.
//
// It does not tear down existing calls. A policy is how a *new* connection is
// offered; changing it mid-call would drop a working call to move it onto a path that
// has not been shown to work, which is the opposite of what the operator asked for.
// The calls already up keep whatever path they negotiated, and new ones get the new
// policy — so a member's next call is the one that changes, and the log's
// candidate-pair line is what confirms it happened.
func (s *SFU) SetForceRelay(force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options.ForceRelay = force
}

// SetICEServers replaces the media engine's own ICE servers, and it is the only way
// forced relay can actually work.
//
// The engine needs its own relay credentials, not just the clients'. Removing the
// hardcoded public STUN server left the server with *no* ICE servers at all, so when
// the policy forbade direct candidates there was nothing left for the server to
// allocate from: a forced-relay deployment would have refused every call, or fallen
// back to a direct path the policy was meant to forbid. Both halves need the relay —
// the client's to reach it, the server's to be reachable through it.
func (s *SFU) SetICEServers(servers []webrtc.ICEServer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options.ICEServers = servers
}

// ForceRelayEnabled reports the policy the next connection will be offered. It is
// the read side of SetForceRelay, so a caller — a test, or an operator-facing
// surface — can confirm the setting reached the engine rather than inferring it from
// a configuration value the operator typed.
func (s *SFU) ForceRelayEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options.ForceRelay
}

func (s *SFU) SetState(userID, owner string, muted, deafened, speaking bool) {
	s.mu.Lock()
	if peer := s.peers[userID]; peer != nil && peer.owner == owner {
		peer.requestedMuted, peer.requestedSpeaking = muted, speaking
		peer.state.Deafened = deafened
		peer.refreshState()
	}
	s.mu.Unlock()
	s.publish()
}

// SetSpeakAllowed applies the voice.speak capability to a live peer. A member who
// may listen but not speak is marked muted and stops transmitting immediately,
// whatever state its own client reports next.
func (s *SFU) SetSpeakAllowed(userID, owner string, allowed bool) {
	s.mu.Lock()
	peer := s.peers[userID]
	withdrawn := false
	if peer != nil && (owner == "" || peer.owner == owner) {
		peer.speakDenied = !allowed
		peer.refreshState()
		// Derived from the registry rather than a flag: a flag that can disagree with
		// the room's own record is how this went wrong before. The registry is the
		// truth, and it is already under this lock.
		if !allowed {
			registry := s.existingRoom(peer.state.ChannelID)
			for _, publication := range registry.list() {
				if publication.Owner == userID {
					if _, ok := registry.remove(publication.ID); ok {
						withdrawn = true
					}
				}
			}
		}
		// The server stops forwarding the moment the permission goes, which is the half
		// it can enforce alone. The *capture* is the other half, and only the client can
		// release it — so the client is told. Without this the browser keeps its sharing
		// indicator lit, keeps a rectangle of somebody's desktop in memory, and keeps
		// publishing a screen the server is discarding: the person thinks they are
		// sharing, the room is not, and the browser's own capture prompt is the only
		// place the truth is visible. That is the "clear capture and indicators for
		// cooperative clients" half of ADR 0019 §8.
	} else {
		peer = nil
	}
	s.mu.Unlock()
	if peer != nil && withdrawn {
		peer.emit("voice.authority_changed", map[string]any{
			"may_share": false,
			"reason":    "A moderator withdrew your permission to share in this conversation.",
		})
	}
	s.publish()
}

// PendingChannel reports the channel of a peer that a resume token still names,
// so a caller can re-establish access before the connection is restored.
func (s *SFU) PendingChannel(userID, token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.peers[userID]
	if peer == nil || peer.resumeToken != token {
		return "", false
	}
	return peer.state.ChannelID, true
}

func (s *SFU) Moderate(userID string, muted, deafened *bool) {
	s.mu.Lock()
	if peer := s.peers[userID]; peer != nil {
		if muted != nil {
			peer.state.AdminMuted = *muted
		}
		if deafened != nil {
			peer.state.AdminDeafened = *deafened
		}
		peer.refreshState()
	}
	s.mu.Unlock()
	s.publish()
}

func (s *SFU) Leave(userID, owner string) {
	s.mu.Lock()
	peer := s.peers[userID]
	owned := peer != nil && (owner == "" || peer.owner == owner)
	s.mu.Unlock()
	if owned {
		s.leavePeerOwned(userID, peer, owner)
	}
}

func (s *SFU) leavePeer(userID string, peer *Peer) {
	s.leavePeerOwned(userID, peer, "")
}

func (s *SFU) leavePeerOwned(userID string, peer *Peer, owner string) {
	s.mu.Lock()
	if s.peers[userID] != peer || (owner != "" && peer.owner != owner) {
		s.mu.Unlock()
		return
	}
	delete(s.peers, userID)
	peer.joinTimer.Stop()
	if peer.detachTimer != nil {
		peer.detachTimer.Stop()
	}
	channelID := peer.state.ChannelID
	// Everything this peer was publishing stops now, not when its connection happens
	// to drain. The room's bound is counted from this registry, so a removed member's
	// screen left behind here keeps consuming the one-screen allowance and refuses
	// everybody after them — the room stays full for a share that ended the moment its
	// owner was removed. This is the revocation half of "stop forwarding immediately",
	// and it happens under the same lock that removed the peer, so nothing can observe
	// a member who is gone and still published.
	registry := s.room(channelID)
	withdrew := false
	for _, publication := range registry.list() {
		if publication.Owner == userID {
			if _, ok := registry.remove(publication.ID); ok {
				withdrew = true
			}
		}
	}
	var recipients []*Peer
	for _, other := range s.peers {
		if other.state.ChannelID == channelID {
			recipients = append(recipients, other)
		}
	}
	s.mu.Unlock()
	// Told only when something was actually withdrawn. A member who left with nothing
	// sharing does not make the room re-render a screen nobody was watching.
	if withdrew {
		for _, other := range recipients {
			other.emit("screen.stopped", map[string]any{"channel_id": channelID})
		}
	}
	peer.emit("voice.left", map[string]string{"channel_id": channelID})
	_ = peer.pc.Close()
	s.publish()
}

func (s *SFU) CloseChannel(channelID string) {
	for _, state := range s.States() {
		if state.ChannelID == channelID {
			s.Leave(state.UserID, "")
		}
	}
}

func (s *SFU) States() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]State, 0, len(s.peers))
	for _, peer := range s.peers {
		states = append(states, peer.state)
	}
	return states
}

func (s *SFU) publish() {
	if s.changed != nil {
		s.changed(s.States())
	}
	s.publishRouting()
}
func (s *SFU) Close() {
	for _, state := range s.States() {
		s.Leave(state.UserID, "")
	}
}

type forwarder struct {
	mu                sync.Mutex
	track             *webrtc.TrackLocalStaticRTP
	sequence          uint16
	timestamp         uint32
	previousTimestamp uint32
	source            string
	// written counts what this forwarder accepted, for the same reason the video
	// forwarder counts: whether a client negotiated to receive says nothing about what
	// the server did with the packet. Reading the server's count is what distinguishes
	// "a replacement microphone was forwarded" from "a track existed".
	written uint64
}

func (f *forwarder) write(source string, packet *rtp.Packet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delta := packet.Timestamp - f.previousTimestamp
	if f.source != source || delta == 0 || delta > 48000 {
		delta = 960
	}
	f.source = source
	f.previousTimestamp = packet.Timestamp
	f.sequence++
	f.timestamp += delta
	forwarded := rtp.Packet{Header: rtp.Header{Version: 2, Marker: packet.Marker, SequenceNumber: f.sequence, Timestamp: f.timestamp}, Payload: packet.Payload}
	// Keep sequence/timestamp continuity when a new participant reuses a slot.
	f.written++
	_ = f.track.WriteRTP(&forwarded)
}

// renegotiate handles an offer that arrives on a call which is already negotiated —
// in practice, a client adding a screen publication to a live conversation.
//
// Three things make this different from the join, and all three are what the previous
// single-offer gate was refusing to allow:
//
//   - The publication is named, so the server knows what is being added and can check
//     the room's and server's bounds before a single packet is forwarded.
//   - The account's ability to transmit is re-checked, and it is the *same*
//     `voice.speak` decision the microphone is gated on. A screen is a publication
//     like any other, and a member who may not transmit may not publish one either.
//   - Renegotiation is serialized per peer. Two clients adding a track at the same
//     moment would otherwise interleave their offers and leave the connection in a
//     state neither of them asked for.
func (s *SFU) renegotiate(peer *Peer, signal Signal) error {
	// The caller holds peer.signalMu, so this runs one at a time per peer already.
	// Holding it across the whole exchange is the point: an SDP exchange is a
	// multi-step conversation and interleaving two of them corrupts both.
	if signal.Publication == "" {
		// An offer that names no publication is either a client asking to *receive* — to
		// watch a screen somebody else is sharing — or a client that forgot to say what it
		// was adding. The two must not be confused: a client that has not asked to publish
		// must never be able to by omitting a field, and a viewer must never have to claim
		// a publication to watch one.
		//
		// The verdict is read from the tracks rather than from the offer's direction
		// attribute, for two reasons that are both specific rather than stylistic. A
		// transceiver created with `AddTransceiverFromKind` produces a section with no
		// direction at all, which the SDP default makes sendrecv — so a viewer and a
		// publisher can produce byte-identical offers. And Pion reuses a same-kind
		// transceiver rather than creating a second one, so a bare recvonly offer matches
		// the server's existing screen output. A viewer's own stream id is what puts its
		// track on a separate m-line, and it is what this reads.
		if signal.SDP == nil || signal.SDP.SDP == "" {
			return errors.New("a renegotiation needs an offer")
		}
		// A view line is how a client asks to receive, and it is answered below. What
		// is still refused is a *sending* video line the server was not told about:
		// the receive path must not become a way to publish by omitting a field,
		// whichever view lines the same offer carries. A section this connection has
		// already negotiated is not an addition — re-offering media that already has
		// a mid and a publication changes nothing.
		if unnamedOfferAddsVideo(peer, *signal.SDP) {
			return errors.New("a second offer must name the publication it is adding")
		}
		// A viewer's output transceiver is created *before* the offer is applied, and
		// this ordering is the whole fix. Pion associates an arriving m-line with an
		// existing unassociated transceiver of the same kind, so creating the screen
		// output first is what makes the answer's section the one that carries the
		// screen track. Creating it afterwards left the client's line answered by a
		// trackless transceiver and the real output as an m-line the client never
		// offered: the connection stayed `connected`, the view lines existed, and no
		// browser viewer ever decoded a frame (review 17, two-browser screen journey).
		wanted := viewRequestKinds(*signal.SDP)
		for _, kind := range wanted {
			if err := peer.addScreenOutputOfKind(kind); err != nil {
				return err
			}
		}
		if err := peer.pc.SetRemoteDescription(*signal.SDP); err != nil {
			return err
		}
		if len(wanted) > 0 || peerIsViewing(peer) {
			// A viewer that joins before anybody shares still needs an output, and a
			// marker-only offer (a client that says so with its stream id rather than
			// with a direction) reaches here through `peerIsViewing` rather than
			// through the offer's own text.
			if len(wanted) == 0 {
				if err := peer.addScreenOutput(); err != nil {
					return err
				}
			}
			return s.answerViewOnly(peer)
		}
		// Not a viewer. A second offer *replaces* the first, so the publication path
		// below re-applies this same offer and the connection is never left holding
		// one that was never answered.
		return errors.New("a second offer must name the publication it is adding")
	}
	if !signal.Publication.Valid() {
		return ErrWrongPublicationKind
	}
	if _, ok := signal.Publication.Codec(); !ok {
		// A kind this build does not carry is refused rather than negotiated, because
		// an offer for a codec with no forwarder would connect and then send into
		// nothing.
		return errors.New("this server does not carry that publication kind")
	}
	// A microphone is the initial offer's own track, not something renegotiated in.
	// A second microphone on one connection is a second audio input, which the
	// forwarder has nowhere to send — but it is a *replacement* rather than an error
	// when the client is swapping devices, so the claim is re-read and released under
	// the lock below rather than refused outright.
	if signal.Publication == PublicationMicrophone {
		peer.receiving.Store(false)
	}
	// One critical section for the whole decision. Reading the permission, the room's
	// publications and the server's totals separately would let another peer publish
	// between the check and the announcement, which is how a bound of one becomes a
	// bound of two.
	s.mu.Lock()
	if peer.speakDenied {
		s.mu.Unlock()
		// The same refusal the microphone gets, for the same reason: a member who may
		// listen may not transmit, and a screen is a transmission.
		return errors.New("you may listen but not share in this channel")
	}
	room := peer.state.ChannelID
	registry := s.room(room)
	limits := s.limits
	roomScreens := registry.countOfKind(PublicationScreenVideo)
	serverScreens := 0
	for _, each := range s.publications {
		serverScreens += each.countOfKind(PublicationScreenVideo)
	}
	s.mu.Unlock()

	// The bound is applied on the write, and *before* the description is examined. A
	// person refused because the room is full must be told that rather than told their
	// offer was malformed, and a refusal is available without an SDP at all.
	switch signal.Publication {
	case PublicationScreenVideo:
		if err := limits.admits(PublicationScreenVideo, roomScreens, serverScreens); err != nil {
			return err
		}
	case PublicationScreenAudio:
		// Screen audio is only meaningful alongside a screen. Publishing it without
		// one is a client that captured system audio but not the display, and
		// forwarding it would be sound from nowhere.
		if roomScreens == 0 {
			return errors.New("share a screen before sharing its audio")
		}
	}

	if signal.SDP == nil || signal.SDP.SDP == "" {
		// A publication can be named without a fresh description when its media is
		// already negotiated. The screen's audio track arrives inside the screen's own
		// offer — one m-line among several — and the client then names it so the
		// forwarder routes it. Re-sending the same offer produced a second answer for a
		// description that was already applied, and the browser's `setRemoteDescription`
		// refuses that: the whole call was torn down by publishing screen audio
		// (review 17, two-browser screen journey).
		//
		// Only a *missing* description counts as already-negotiated media. An
		// empty-string description is a malformed client, and screen audio is the one
		// kind that maps to an existing m-line; every other publication must bring its
		// offer.
		if signal.Publication != PublicationScreenAudio || signal.SDP != nil {
			return errors.New("a renegotiation needs an offer")
		}
		s.mu.Lock()
		if s.room(room).ownedBy(peer.state.UserID, PublicationScreenAudio) {
			// Already named — a re-announcement after a restart that did not stop.
			// There is nothing to add, and a second registry entry would leave the room
			// counting one track twice.
			s.mu.Unlock()
			return nil
		}
		if err := peer.addScreenOutputOfKind(PublicationScreenAudio); err != nil {
			s.mu.Unlock()
			return err
		}
		publicationID := signal.PublicationID
		if publicationID == "" {
			publicationID = PublicationID(uuid.NewString())
		}
		s.announcePublication(peer, Publication{
			ID: publicationID, Owner: peer.state.UserID, Kind: PublicationScreenAudio,
			Started: time.Now().UTC(),
		})
		s.mu.Unlock()
		return nil
	}

	publicationID := signal.PublicationID
	if publicationID == "" {
		publicationID = PublicationID(uuid.NewString())
	}
	// The output track is added before the description is created, so the answer
	// actually satisfies the offer. Creating it afterwards would produce an answer the
	// client's offer did not ask for, which a peer connection rejects.
	//
	// The microphone is excluded: it has been forwarded by slot since the beginning and
	// is not a registered publication, so an output for it here would be a second, empty
	// one. Screen *audio* is not excluded — it is a registered publication with its own
	// track, and skipping its output is why a shared screen's system audio was never
	// delivered to anybody.
	if !signal.Publication.Audio() || signal.Publication == PublicationScreenAudio {
		if err := peer.addScreenOutputOfKind(signal.Publication); err != nil {
			return err
		}
	}
	if err := peer.pc.SetRemoteDescription(*signal.SDP); err != nil {
		return err
	}
	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		return err
	}
	// The publication is announced only after the description that carries it has been
	// accepted locally. A viewer told about a publication the connection does not have
	// would wait forever for it.
	//
	// And the allowance is re-asked *here*, under this lock, rather than trusted from the
	// read that happened before the SDP exchange above. That read released the lock, so
	// a second peer negotiating in between saw the same count this one did and passed
	// too: the room ended up carrying two screens against a bound of one, the second
	// consuming bandwidth and a viewer's output for an offer the server should have
	// refused. The reservation is now taken by the transaction that registers the
	// publication, which is the only point close enough to the write to be about it.
	s.mu.Lock()
	roomNow, serverNow := 0, 0
	for _, each := range s.publications {
		serverNow += each.countOfKind(signal.Publication)
	}
	roomNow = s.existingRoom(room).countOfKind(signal.Publication)
	if err := limits.admits(signal.Publication, roomNow, serverNow); err != nil {
		s.mu.Unlock()
		// The description has already been accepted, so the *offer* is answered; it is
		// the publication that is refused, and the client is told so rather than left
		// with a negotiated track that carries nothing.
		return err
	}
	s.announcePublication(peer, Publication{
		ID: publicationID, Owner: peer.state.UserID, Kind: signal.Publication,
		Started: time.Now().UTC(),
	})
	s.mu.Unlock()
	peer.emit("rtc.signal", Signal{
		SDP: peer.pc.LocalDescription(), ConnectionID: peer.connectionID,
		Publication: signal.Publication, PublicationID: publicationID,
	})
	return nil
}

// room returns the publication registry for a room, creating it on first use. It is
// called with mu held, or from a path that has already established the room exists.
func (s *SFU) room(id string) *publicationRegistry {
	registry, ok := s.publications[id]
	if !ok {
		registry = newPublicationRegistry()
		s.publications[id] = registry
	}
	return registry
}

// existingRoom returns a room's registry without creating one.
//
// The two are separate because "what is in this room" and "make this room" are
// different questions, and a read that quietly makes one turns every lookup for a
// room that does not exist into a permanent map entry. Nothing ever removed them, so
// a server accumulated one per name it had been asked about — which for a room id an
// attacker chooses is unbounded, and for a deleted channel is just a slow leak.
func (s *SFU) existingRoom(id string) *publicationRegistry {
	if registry, ok := s.publications[id]; ok {
		return registry
	}
	return newPublicationRegistry()
}

func (s *SFU) publicationsFor(id string) []Publication {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.existingRoom(id).list()
}

// serverPublicationCount counts a kind across every room. It is a capacity fact rather
// than a room policy, which is why it is counted separately and reported separately
// (publications.go, PublicationLimits).
func (s *SFU) serverPublicationCount(kind PublicationKind) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, registry := range s.publications {
		total += registry.countOfKind(kind)
	}
	return total
}

// announcePublication records a new publication. The caller holds mu, because the
// registry and the peer map have to agree about who is in the room.
// announcePublication records a publication and tells the room.
//
// The announcement is what makes a client that joined *after* a share began able to
// receive one: a client that has no video transceiver cannot be sent a screen, and
// the only moment it learns a screen exists is when somebody says so. It is emitted to
// the room's participants rather than the whole server because who is in a call is
// the same boundary as who may hear it (ADR 0019 §5).
func (s *SFU) announcePublication(peer *Peer, publication Publication) {
	room := peer.state.ChannelID
	s.room(room).add(&publication)
	var recipients []*Peer
	for _, other := range s.peers {
		if other.state.ChannelID != room {
			continue
		}
		// The publisher is not told about its own publication. It already has the
		// publication id from the answer that registered it, and the announcement's
		// whole purpose is to tell *somebody else* that a screen exists so it can
		// negotiate a line for it. Delivered back to the publisher, it made the
		// sharing client ask to view its own share: the view request was answered
		// with a video line the client then attached as a picture, which stopped the
		// capture the share was publishing (review 17, two-browser screen journey).
		if other == peer {
			continue
		}
		recipients = append(recipients, other)
	}
	s.mu.Unlock()
	for _, other := range recipients {
		other.emit("screen.published", map[string]any{
			"publication_id": string(publication.ID),
			"owner":          publication.Owner,
			"channel_id":     room,
			// The kind is what lets a viewer renegotiate for the piece it does not
			// hold yet: the video arrives on the view offer it already made, and the
			// screen's audio is announced afterwards as its own publication, so a
			// client that kept them indistinguishable could never hear a share.
			"kind": string(publication.Kind),
		})
	}
	s.mu.Lock()
}

// Publications returns the live publications in a room, in announcement order. It is
// how a client that joins mid-share finds the screen without having to be told about
// it in the past.
func (s *SFU) Publications(room string) []Publication {
	return s.publicationsFor(room)
}

// StopPublication removes a publication and reports whether it was there.
//
// A stop removes rather than marks, because a viewer cannot tell a stopped publication
// from somebody who stopped talking. The caller is responsible for the connection-side
// work — closing the sender — because only it knows which transceiver carried it.
func (s *SFU) StopPublication(room string, id PublicationID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, removed := s.room(room).remove(id)
	return removed
}

// connectionIDFor reports the peer's current connection id, which a client must echo
// in every signal so a message meant for a call somebody has already left is dropped
// rather than applied to whatever replaced it.
func (s *SFU) connectionIDFor(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[userID]
	if !ok {
		return ""
	}
	return peer.connectionID
}

// connectionStateOf reports the peer's connection state, which is what a test (and
// the operator diagnostics) needs to know without reaching into the peer.
func (s *SFU) connectionStateOf(userID string) webrtc.PeerConnectionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[userID]
	if !ok {
		return webrtc.PeerConnectionStateClosed
	}
	return peer.pc.ConnectionState()
}

// setEmitter replaces a peer's event sink. The production sink is set at Join; this
// exists so a test can observe the answers a renegotiation produces without reaching
// into the peer's unexported fields.
func (s *SFU) setEmitter(userID string, send func(string, any)) {
	s.mu.Lock()
	peer, ok := s.peers[userID]
	s.mu.Unlock()
	if !ok {
		return
	}
	// Under the peer's own lock, which is the one `emit` reads it under. Writing it
	// under the SFU lock was a race: `Join`'s connection-state goroutine can emit the
	// moment a peer becomes connected, and a test that installs the emitter at that
	// moment wrote the field while that goroutine read it. Two locks guarding one field
	// is not two locks — it is no lock at all.
	peer.sendMu.Lock()
	peer.send = send
	peer.sendMu.Unlock()
}

// forwardPublication routes a packet from a peer's publication to the peers watching
// it. It is `forward` with the publication as an explicit parameter rather than
// inferred from a slot, which is the whole point of the model: a screen's packets must
// never be written through an audio forwarder, and a screen must never occupy a voice
// slot that a listener is counting on.
func (s *SFU) forwardPublication(source *Peer, kind PublicationKind, packet *rtp.Packet) {
	s.mu.Lock()
	// Forwarding is the boundary, not the state the client reported: a member whose
	// permission was withdrawn has their packets dropped here even if their client
	// still believes it is sharing.
	//
	// Which flags that means depends on what the packet *is*, and this used to ask
	// the microphone's question of every publication. The consequence was that a
	// member who pressed their own mute button also stopped broadcasting their
	// screen to everyone in the room — the most common reason to mute is somebody
	// talking over you, which is not a withdrawal of the right to be seen.
	//
	// A screen is gated on the *permissions*: the connection is still the peer's,
	// the capability is still granted, and no moderator has silenced the member.
	// Client-side voice state — requested mute, deafen, and the indicators that go
	// with them — describes the microphone and says nothing about a second,
	// independent publication on the same connection.
	if s.peers[source.state.UserID] != source || source.speakDenied || source.state.AdminMuted {
		s.mu.Unlock()
		return
	}
	if kind == PublicationMicrophone {
		// The microphone is the connection's own audio input and has always been
		// forwarded by slot; it is not a registered publication. Routing it through the
		// publication registry would count zero and drop every voice packet.
		//
		// Its own gate lives in `forward`, which is where the voice state has always
		// been applied — keeping it there means the check is not duplicated here and
		// cannot drift between the two copies.
		if source.state.Muted || source.state.Deafened || source.state.AdminDeafened {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.forward(source, packet)
		return
	}
	// Everything beyond the microphone is a registered publication, so a stopped one
	// has nothing to forward to and its packets stop here. That is what makes a stop
	// a stop rather than a silent track a viewer has to notice.
	//
	// The check is *whose* publication, not merely whether one exists. "Does this room
	// have a screen" is the wrong question: it lets a second member — one who was
	// refused a share, or never asked for one — advance viewer output under somebody
	// else's publication, because the room's allowance is theirs and the packet is not.
	// Forwarding is the boundary, so the boundary asks whether *this connection* owns a
	// live publication of this kind here.
	// A read, so it must not create the room. This runs on *every* screen packet, so
	// the read-and-create version left a registry behind for any room a stopped
	// screen's packets kept arriving for — once per room, which is bounded, but it is
	// the same mistake as the read path above and the fix belongs in one place.
	if !s.existingRoom(source.state.ChannelID).ownedBy(source.state.UserID, kind) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.forwardScreen(source, kind, packet)
}

// screenForwarder rewrites a video publication's sequence numbers and timestamps.
//
// It exists as a separate type rather than a branch inside `forwarder` because the
// audio forwarder's constants are *wrong* for video and reusing them would produce a
// stream that looks like it connects and plays as noise: it steps the timestamp by a
// fixed 960 against a 48 kHz clock, which is right for 20 ms of Opus and meaningless
// for a 90 kHz video clock. A clock mismatch here is not a small error — it is a
// broken picture, and a broken picture is worse than an absent one because a viewer
// cannot tell the difference between "the sharer's screen froze" and "the server
// mangled it".
type screenForwarder struct {
	mu                sync.Mutex
	track             *webrtc.TrackLocalStaticRTP
	sequence          uint16
	timestamp         uint32
	previousTimestamp uint32
	source            string
	// started says whether a previous timestamp has been seen. Without it, the first
	// packet of a publication computes its delta against zero and produces a huge
	// jump, which a decoder reads as a stream that has been running for minutes.
	started bool
	// written and bytes count what this forwarder accepted. They are the server's own
	// cost, which is what a capacity measurement reads: whether a client has negotiated
	// to receive says nothing about what the server did with the packet.
	written uint64
	bytes   uint64
	// kind is what this output is for, and it is the reason a packet is accepted or
	// refused rather than rewritten.
	//
	// A video track runs on a 90 kHz clock and carries H264. Screen *audio* is an Opus
	// stream on a 48 kHz clock. Writing audio into the video output does not produce
	// audio — it produces a track that decodes as noise, which is the failure ADR 0019
	// §5 warns about in the other direction. So the two are separate outputs and a
	// packet whose kind does not match is dropped rather than translated.
	kind PublicationKind
}

// videoClockRate is the RTP clock for the video this build carries. It is stated once,
// here, so the forwarder's sanity bound and its codec registration cannot disagree.
const videoClockRate = 90000

// maxVideoTimestampJump bounds how far a video timestamp may advance between packets
// before the stream is treated as discontinuous and restarted. A keyframe every few
// seconds plus a frame interval is generous; anything beyond it is a different
// publication or a reordered packet, and replaying the old delta would stall the
// picture for as long as the gap.
const maxVideoTimestampJump = videoClockRate * 5

// minVideoTimestampJump is the smallest plausible advance, used only to catch a
// backwards timestamp. A negative delta is a reorder, and applying it would move the
// stream backwards.
const minVideoTimestampJump = -(videoClockRate / 30)

func (f *screenForwarder) write(source string, packet *rtp.Packet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delta := int64(packet.Timestamp) - int64(f.previousTimestamp)
	// A first packet, a new source, or a gap too large to be a frame interval all
	// mean the previous timeline no longer applies, so it is restarted rather than
	// continued from a number that would be wrong.
	if !f.started || f.source != source || delta <= int64(minVideoTimestampJump) || delta > maxVideoTimestampJump {
		delta = videoClockRate / 30
	}
	f.source = source
	f.previousTimestamp = packet.Timestamp
	f.started = true
	f.sequence++
	f.timestamp = f.timestamp + uint32(delta)
	forwarded := rtp.Packet{
		Header: rtp.Header{
			Version: 2, Marker: packet.Marker,
			SequenceNumber: f.sequence, Timestamp: f.timestamp,
			PayloadType: packet.PayloadType,
		},
		Payload: packet.Payload,
	}
	// The payload type is carried through rather than assumed. A video stream whose
	// payload type does not match the negotiated codec is not decodable, and the
	// caller cannot be assumed to have rewritten it.
	if err := f.track.WriteRTP(&forwarded); err == nil {
		f.written++
		f.bytes += uint64(len(forwarded.Payload))
	}
}

// forwardScreen writes a video publication to every peer's screen output in the room.
func (s *SFU) forwardScreen(source *Peer, kind PublicationKind, packet *rtp.Packet) {
	s.mu.Lock()
	room := source.state.ChannelID
	var recipients []*screenForwarder
	for _, destination := range s.peers {
		if destination.state.ChannelID != room || destination == source {
			continue
		}
		recipients = append(recipients, destination.screens...)
	}
	s.mu.Unlock()
	// Writing happens outside the lock: a slow viewer must not hold up the room, and
	// holding the lock across a network write is exactly the pattern the authority
	// fence exists to avoid.
	for _, output := range recipients {
		// Bound to the publication, not just to the room. The two screen publications
		// are separate outputs with different clocks and codecs, so a packet written to
		// the wrong one is not delivered late — it is never delivered at all, and the
		// failure looks like a broken speaker rather than a routing mistake.
		if output.kind != kind {
			continue
		}
		output.write(source.state.UserID, packet)
	}
}

// screenTrackPrefix names the tracks this server sends. It is a prefix rather than an
// exact id because the user id is appended, and two places now depend on it — a
// literal in two of them is how they come to disagree.
const screenTrackPrefix = "screen-"

// addScreenOutput creates one video output on this peer. There is one, not several:
// the room's publication bound already decides how many screens a room carries, and a
// second output per peer would be a way around it.
func (p *Peer) addScreenOutput() error {
	return p.addScreenOutputOfKind(PublicationScreenVideo)
}

// addScreenOutputOfKind creates one output for one screen publication kind.
//
// The two are separate outputs rather than one track carrying both, and that is the
// whole point of the `kind` on the forwarder. A video track is an H264 stream on a 90 kHz
// clock; screen audio is Opus on 48 kHz. Writing the second into the first does not
// produce audio — it produces a track that decodes as noise, and a viewer who asks why
// the shared screen's sound is broken is being told about a routing mistake in terms of
// a speaker.
//
// So each kind gets its own track with its own clock, and `forwardScreen` writes only to
// the outputs of the kind it is carrying.
func (p *Peer) addScreenOutputOfKind(kind PublicationKind) error {
	for _, existing := range p.screens {
		if existing.kind == kind {
			return nil
		}
	}
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: videoClockRate}
	label := "screen"
	if kind == PublicationScreenAudio {
		codec = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}
		label = "screen-audio"
	}
	track, err := webrtc.NewTrackLocalStaticRTP(codec, label, label+"-"+p.state.UserID)
	if err != nil {
		return err
	}
	transceiver, err := p.pc.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		return err
	}
	p.screens = append(p.screens, &screenForwarder{track: track, kind: kind})
	// RTCP is drained rather than interpreted, as the audio outputs already do. The
	// reason is the same: this build enforces publication at the forwarding boundary,
	// not by answering feedback, and an unread RTCP channel blocks the sender.
	go func(sender *webrtc.RTPSender) {
		for {
			if _, _, err := sender.ReadRTCP(); err != nil {
				return
			}
		}
	}(transceiver.Sender())
	return nil
}

// screenOutputs returns a peer's video outputs. It copies the slice so a caller can
// read the forwarders' counters under their own locks without holding the SFU's.
func (p *Peer) screenOutputs() []*screenForwarder {
	return append([]*screenForwarder(nil), p.screens...)
}

// renegotiateForTest announces a publication for a peer that is already connected,
// without the SDP exchange.
//
// It exists because the load measurement needs the *forwarding* path and the
// negotiation is not what it is measuring: announcing through the real signal path
// would make the harness wait on offer/answer round trips that a benchmark loop
// cannot usefully do hundreds of times a second. Everything the measurement does care
// about — which forwarder a packet takes, how much is copied, what the server's
// timeline does — is the production code path.
//
// It is deliberately unexported and unavailable outside this package, so the
// production surface has no way to announce a publication without negotiating one.
func (s *SFU) renegotiateForTest(peer *Peer, publicationID string) error {
	s.mu.Lock()
	if s.peers[peer.state.UserID] != peer {
		s.mu.Unlock()
		return errors.New("peer is not in this server")
	}
	if err := peer.addScreenOutput(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.room(peer.state.ChannelID).add(&Publication{
		ID: PublicationID(publicationID), Owner: peer.state.UserID,
		Kind: PublicationScreenVideo, Started: time.Now().UTC(),
	})
	s.mu.Unlock()
	// The answer is produced and sent here, because a screen track that was added
	// without a negotiation never reaches the server at all: the connection has no
	// transceiver for it, its OnTrack never fires, and the publication is announced
	// with nothing behind it. That is what a measurement reports when it has not been
	// wired up, and it is why this is stated rather than assumed.
	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		return err
	}
	peer.emit("rtc.signal", Signal{
		SDP: peer.pc.LocalDescription(), ConnectionID: peer.connectionID,
		Publication: PublicationScreenVideo, PublicationID: PublicationID(publicationID),
	})
	return nil
}

// viewRequestKinds reads which publications a client is asking to *receive*, from the
// offer itself.
//
// Two things say it: the direction a client writes down (`a=recvonly`, which is
// unambiguous) and the view stream id, which a client puts on a line it never writes
// to. The direction attribute is otherwise no guide at all — a bare transceiver has no
// direction and the SDP default makes it sendrecv — which is why the marker exists,
// and why both are read here rather than only one. A publisher's screen line carries
// neither: it is `sendonly` (or sendrecv) with its own stream id.
func viewRequestKinds(description webrtc.SessionDescription) []PublicationKind {
	var kinds []PublicationKind
	for _, media := range remoteMedia(description.SDP) {
		if !mediaDeclaresView(media) && !mediaIsRecvOnly(media) {
			continue
		}
		switch {
		case isVideoMedia(media):
			kinds = append(kinds, PublicationScreenVideo)
		case isAudioMedia(media):
			kinds = append(kinds, PublicationScreenAudio)
		}
	}
	return kinds
}

// unnamedOfferAddsVideo reports whether an offer that names no publication carries a
// video section that would send media the server has not been told about.
//
// The question is asked of the offer's own text rather than of the transceivers it
// produced, because by then the two cases look identical: an unmarked sending line and
// the view line a client uses to receive both appear as a video transceiver on this
// connection. What separates them is the direction the client wrote down and whether
// the section is new — a section whose mid this connection has already negotiated is
// media the connection already has, not an addition, and a re-offer of it changes
// nothing about who publishes.
func unnamedOfferAddsVideo(peer *Peer, description webrtc.SessionDescription) bool {
	negotiated := map[string]bool{}
	for _, transceiver := range peer.pc.GetTransceivers() {
		if mid := transceiver.Mid(); mid != "" {
			negotiated[mid] = true
		}
	}
	for _, media := range remoteMedia(description.SDP) {
		if !isVideoMedia(media) || !mediaSends(media) || mediaDeclaresView(media) {
			continue
		}
		if negotiated[mediaMid(media)] {
			continue
		}
		return true
	}
	return false
}

// mediaMid reads a section's mid, which is how a re-offer refers to media this
// connection has already negotiated. An empty mid is a new section, because a section
// that has been negotiated always has one.
func mediaMid(media sdpMedia) string {
	for _, attribute := range media.attributes {
		if strings.HasPrefix(attribute, "a=mid:") {
			return strings.TrimPrefix(attribute, "a=mid:")
		}
	}
	return ""
}

// peerIsViewing reports whether the offer that was just applied is a client asking to
// receive rather than to publish.
//
// It looks for the view stream id, which is the one signal that survives both problems
// above: the direction attribute is absent from a bare transceiver, and a same-kind
// transceiver is reused rather than duplicated. A viewer that omits the id is refused,
// which is the right way round — the burden is on the client that wants something
// unusual to say so explicitly, and a client that publishes without saying so gets the
// refusal rather than a silent publication.
func peerIsViewing(peer *Peer) bool {
	receiving := false
	for _, transceiver := range peer.pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeVideo {
			continue
		}
		// The server's own screen output is a sendonly video track it added, and it is
		// not the client's. Counting it would make every viewer look like a publisher,
		// which is the exact confusion this check exists to prevent.
		if sender := transceiver.Sender(); sender != nil && sender.Track() != nil &&
			isScreenOutputTrack(sender.Track().ID()) {
			continue
		}
		// A video transceiver the client is only receiving on is a viewer. This is what a
		// marker-free offer looks like once the transceivers are negotiated, and it is
		// why the check reads them rather than the description.
		if transceiver.Direction() != webrtc.RTPTransceiverDirectionRecvonly {
			continue
		}
		if receiver := transceiver.Receiver(); receiver != nil && receiver.Track() != nil {
			if IsViewOnlyStream(receiver.Track().StreamID()) {
				return true
			}
		}
		receiving = true
	}
	return receiving
}

// publicationForTrack decides which publication an arriving track belongs to.
//
// The codec cannot answer this. Screen audio is an audio track, exactly like the
// microphone, so "is this audio" leaves two candidates and no way to choose — and
// choosing wrongly is not cosmetic: mistaking a screen's system audio for a
// replacement microphone hands the connection's single audio output to the wrong
// input, and the member's actual microphone stops being forwarded.
//
// What distinguishes them is what the peer has published. A live screen audio
// publication means an audio track on this connection is that publication; otherwise
// the connection's own audio input is the microphone. That is a question about the
// peer's negotiated state rather than about the packet, which is why it is asked once
// per track rather than per packet.
func (s *SFU) publicationForTrack(peer *Peer, codec webrtc.RTPCodecType) PublicationKind {
	switch codec {
	case webrtc.RTPCodecTypeVideo:
		return PublicationScreenVideo
	case webrtc.RTPCodecTypeAudio:
	default:
		return ""
	}
	s.mu.Lock()
	sharesScreenAudio := s.existingRoom(peer.state.ChannelID).ownedBy(peer.state.UserID, PublicationScreenAudio)
	s.mu.Unlock()
	if sharesScreenAudio {
		return PublicationScreenAudio
	}
	return PublicationMicrophone
}

// isScreenOutputTrack recognises the track the server added to send a screen, by the
// prefix it was created with.
func isScreenOutputTrack(trackID string) bool {
	return strings.HasPrefix(trackID, screenTrackPrefix)
}

// remoteMedia splits an SDP into its media sections, each with its own attributes.
func remoteMedia(offer string) []sdpMedia {
	sections := []sdpMedia{}
	for _, part := range strings.Split(offer, "m=")[1:] {
		lines := strings.Split(part, "\n")
		media := sdpMedia{header: strings.TrimSpace(strings.SplitN(lines[0], "\r", 2)[0])}
		for _, raw := range lines[1:] {
			line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
			if line == "" {
				break
			}
			media.attributes = append(media.attributes, line)
		}
		sections = append(sections, media)
	}
	return sections
}

type sdpMedia struct {
	header     string
	attributes []string
}

func isVideoMedia(media sdpMedia) bool {
	fields := strings.Fields(media.header)
	return len(fields) > 0 && (strings.Contains(fields[0], "video") || strings.Contains(fields[0], "image"))
}

func isAudioMedia(media sdpMedia) bool {
	fields := strings.Fields(media.header)
	return len(fields) > 0 && strings.HasPrefix(fields[0], "audio")
}

// mediaIsRecvOnly reports the direction a client wrote down for a section. It is the
// unambiguous half of "this line is a view": a client that means to receive says so,
// while a bare transceiver says nothing and defaults to sendrecv.
func mediaIsRecvOnly(media sdpMedia) bool {
	for _, attribute := range media.attributes {
		if attribute == "a=recvonly" {
			return true
		}
	}
	return false
}

func mediaSends(media sdpMedia) bool {
	for _, attribute := range media.attributes {
		switch attribute {
		case "a=sendonly", "a=sendrecv":
			return true
		case "a=recvonly", "a=inactive":
			return false
		}
	}
	// No direction attribute at all is sendrecv by the SDP default, so a section that
	// omits one is a publisher rather than a viewer.
	return true
}

func mediaDeclaresView(media sdpMedia) bool {
	for _, attribute := range media.attributes {
		if !strings.HasPrefix(attribute, "a=msid:") {
			continue
		}
		// Either position may carry the marker: a client that adds a track puts it on
		// the track (the second token), and one that adds a transceiver puts it on the
		// stream (the first). Both are the same statement — this line is not written to.
		for _, field := range strings.Fields(strings.TrimPrefix(attribute, "a=msid:")) {
			if IsViewOnlyStream(field) {
				return true
			}
		}
	}
	return false
}

// answerViewOnly answers an offer that adds only a receiving track. It deliberately
// announces nothing: the bounds, the participant list and the forwarding are all
// about publications, and a viewer that appeared in them would be counted as a
// sharer and would consume the room's one-screen budget.
func (s *SFU) answerViewOnly(peer *Peer) error {
	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		return err
	}
	peer.emit("rtc.signal", Signal{
		SDP: peer.pc.LocalDescription(), ConnectionID: peer.connectionID,
	})
	return nil
}

// stopPublication withdraws a peer's screen share and tells the room.
//
// A stop *removes* the publication rather than marking it inactive: the room's screen
// bound is one, and a publication that is still counted after the person stopped
// sharing means the next person to try is refused by a share that ended minutes ago.
//
// A screen and its audio are one share — `admits` refuses screen audio without a
// screen and bounds neither separately — so stopping withdraws both kinds. Removing
// only the video left the audio publication in the room's registry: the listing kept
// advertising a share that had ended, and the audio's forwarders stayed attached to a
// capture that was already released (review 17, media scout).
//
// It takes no locks beyond the SFU's and touches no forwarder state, which is
// deliberate — an earlier version reached into the viewer's forwarders here and
// deadlocked on a lock the caller already held.
func (s *SFU) stopPublication(userID string, signal Signal) error {
	s.mu.Lock()
	peer, ok := s.peers[userID]
	if !ok || (signal.ConnectionID != "" && peer.connectionID != signal.ConnectionID) {
		s.mu.Unlock()
		// Not an error: a client whose session has already gone has nothing to stop.
		return nil
	}
	room := peer.state.ChannelID
	registry := s.room(room)
	stopped := false
	for _, publication := range registry.list() {
		if publication.Owner != userID {
			continue
		}
		if publication.Kind != PublicationScreenVideo && publication.Kind != PublicationScreenAudio {
			continue
		}
		registry.remove(publication.ID)
		stopped = true
	}
	if !stopped {
		s.mu.Unlock()
		return nil
	}
	var recipients []*Peer
	for _, other := range s.peers {
		if other.state.ChannelID == room {
			recipients = append(recipients, other)
		}
	}
	s.mu.Unlock()
	for _, other := range recipients {
		other.emit("screen.stopped", map[string]any{"channel_id": room})
	}
	return nil
}

// selectedCandidatePair reports the type of the candidate pair ICE actually chose.
// It is the only place the question "which path carried this call" has an answer, and
// the answer is the difference between a direct path and a relayed one.
//
// It asks the ICE transport, not the connection's statistics report. Both answer the
// same question, but `GetStats` reads state that Pion's own teardown clears without a
// lock, so collecting a diagnostic while a client disconnects is a data race — one
// the race detector found the moment a test client closed its side while this read
// was in flight. The ICE transport answers from its agent's atomically stored
// selected pair, which a concurrent close cannot corrupt.
func (s *SFU) selectedCandidatePair(peer *Peer) (string, string, error) {
	// A peer can be present in the map before its connection is built. Reading from
	// no connection is not a measurement; it is a nil dereference, and the caller is
	// a logging goroutine whose panic would take the server down over a diagnostic.
	if peer == nil || peer.pc == nil {
		return "", "", errors.New("the peer has no connection")
	}
	iceTransport := peer.pc.SCTP().Transport().ICETransport()
	if iceTransport == nil {
		return "", "", errors.New("the peer has no ICE transport")
	}
	pair, err := iceTransport.GetSelectedCandidatePair()
	if err != nil {
		return "", "", err
	}
	if pair == nil || pair.Local == nil || pair.Remote == nil {
		return "", "", errors.New("no candidate pair has succeeded")
	}
	if pair.Local.Typ == webrtc.ICECandidateTypeUnknown || pair.Remote.Typ == webrtc.ICECandidateTypeUnknown {
		return "", "", errors.New("the selected pair's candidate type is unknown")
	}
	// String is the type's own name — "host", "srflx", "prflx", "relay" — which is the
	// token that appears in an SDP candidate line and the one an operator will search
	// a log for.
	return pair.Local.Typ.String(), pair.Remote.Typ.String(), nil
}

// ICEServers reports the servers the media engine will use for its own connections,
// so a caller can confirm a configuration reached the engine rather than inferring it
// from the settings an operator typed.
func (s *SFU) ICEServers() []webrtc.ICEServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]webrtc.ICEServer(nil), s.options.ICEServers...)
}
