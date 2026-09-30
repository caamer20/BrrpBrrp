package rtc

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const MaxParticipants = 10

type Options struct {
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
	Publication PublicationKind   `json:"publication,omitempty"`
	// PublicationID is the id the new track is published under, chosen by the server
	// and returned to the client so a reconnecting client can tell a resume from a
	// new publication that reused a slot.
	PublicationID PublicationID `json:"publication_id,omitempty"`
}

type JoinEvent struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

type Peer struct {
	owner        string
	state        State
	slot         int
	pc           *webrtc.PeerConnection
	outputs      []*forwarder
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
	}, nil
}

func (s *SFU) Join(userID, owner, channelID string, capacity int, send func(string, any)) error {
	s.mu.Lock()
	if _, exists := s.peers[userID]; exists {
		s.mu.Unlock()
		return errors.New("you are already in voice, possibly in another tab")
	}
	if len(s.peers) >= MaxParticipants {
		s.mu.Unlock()
		return errors.New("the server's ten voice places are occupied")
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
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{ICEServers: s.options.ICEServers})
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
	for index := 0; index < MaxParticipants-1; index++ {
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
		} else if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			s.leavePeer(userID, peer)
		}
	})
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeAudio || !peer.receiving.CompareAndSwap(false, true) {
			return
		}
		for {
			packet, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			s.forward(peer, packet)
		}
	})
	s.publish()
	return nil
}

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
	if peer := s.peers[userID]; peer != nil && (owner == "" || peer.owner == owner) {
		peer.speakDenied = !allowed
		peer.refreshState()
	}
	s.mu.Unlock()
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
	s.mu.Unlock()
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
	// A second microphone would be a second audio input on one connection, which the
	// forwarder has nowhere to send.
	if signal.Publication == PublicationMicrophone {
		return errors.New("this connection already has a microphone")
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
		// A nil description would reach Pion and panic, taking the process with it. The
		// path is reachable from a malformed client, so it is guarded here rather than
		// assumed away.
		return errors.New("a renegotiation needs an offer")
	}

	publicationID := signal.PublicationID
	if publicationID == "" {
		publicationID = PublicationID(uuid.NewString())
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
	s.mu.Lock()
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

func (s *SFU) publicationsFor(id string) []Publication {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.room(id).list()
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
func (s *SFU) announcePublication(peer *Peer, publication Publication) {
	s.room(peer.state.ChannelID).add(&publication)
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
	defer s.mu.Unlock()
	if peer, ok := s.peers[userID]; ok {
		peer.send = send
	}
}
