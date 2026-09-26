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
}

func New(options Options, changed func([]State)) (*SFU, error) {
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0;sprop-stereo=0"}, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
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
	return &SFU{api: api, options: options, peers: make(map[string]*Peer), changed: changed}, nil
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
		if signal.SDP.Type != webrtc.SDPTypeOffer || len(signal.SDP.SDP) > 50000 || peer.pc.RemoteDescription() != nil {
			return errors.New("invalid voice offer")
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
