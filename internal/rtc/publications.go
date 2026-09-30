package rtc

import (
	"errors"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// A publication is one thing a participant is sending: a microphone, a screen's video,
// a screen's audio. It is a first-class object with its own identity, its own bounds
// and its own lifecycle, rather than a slot index (ADR 0019, §4).
//
// The distinction is not tidiness. The existing model has one `recvonly` audio input
// and nine pre-created `voice-0..8` output tracks per peer (`sfu.go:151,156-157`),
// forwarded positionally. That encoding says three things at once — a participant has
// exactly one track, it never changes, and its position is its identity. A screen share
// breaks all three, and a screen track stored against an audio constant is how a screen
// ends up reported as a microphone.

// PublicationKind is what a publication carries. The three are separate values rather
// than a boolean, because a screen's audio is a different thing from its video: it is
// optional, it is not tied to a microphone, and a listener who cannot hear the speaker
// may still be able to read the screen.
type PublicationKind string

const (
	// PublicationMicrophone is a participant's audio input.
	PublicationMicrophone PublicationKind = "microphone"
	// PublicationScreenVideo is a captured screen or window.
	PublicationScreenVideo PublicationKind = "screen_video"
	// PublicationScreenAudio is a screen's system audio, when the browser and the
	// operating system provide it. Not every platform does, and the product does not
	// promise it — a client that cannot capture it simply does not publish it.
	PublicationScreenAudio PublicationKind = "screen_audio"
)

// Valid reports whether a kind is one this build understands. An unknown kind is
// refused rather than ignored, because a client publishing something the server cannot
// classify has no way to know it is not being forwarded.
func (k PublicationKind) Valid() bool {
	switch k {
	case PublicationMicrophone, PublicationScreenVideo, PublicationScreenAudio:
		return true
	}
	return false
}

// Codec returns the RTP codec a publication of this kind is forwarded as, and whether
// this build carries it at all.
func (k PublicationKind) Codec() (webrtc.RTPCodecParameters, bool) {
	switch k {
	case PublicationMicrophone, PublicationScreenAudio:
		// Opus, and the same parameters the existing audio path uses. Screen audio is
		// audio: the same codec, the same clock, the same forwarding.
		return webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
			PayloadType:        111,
		}, true
	case PublicationScreenVideo:
		// H.264 rather than VP8, because it is what a browser's `getDisplayMedia`
		// produces most reliably and what the hardware encoders on the supported
		// platforms accept.
		return webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
			PayloadType:        102,
		}, true
	}
	return webrtc.RTPCodecParameters{}, false
}

// Audio reports whether a publication's packets are audio. The forwarder needs this to
// know whether the audio constants apply, and it is the reason a screen video track can
// never be written through the audio path.
func (k PublicationKind) Audio() bool {
	return k == PublicationMicrophone || k == PublicationScreenAudio
}

// PublicationID identifies one publication. It is stable for the life of the
// publication and is what a client subscribes to, so a viewer following a screen does
// not have to re-negotiate when somebody joins or leaves.
type PublicationID string

// ErrPublicationNotFound is returned for an unknown publication. It is deliberately
// the same answer a removed publication gives: a client must not be able to probe for
// what else a room is carrying.
var ErrPublicationNotFound = errors.New("publication not found")

// ErrNoPublicationSlot is returned when a room's or server's publication bound is
// reached. It is applied on the write, so a bound cannot be exceeded by a client that
// simply does not ask first.
var ErrNoPublicationSlot = errors.New("no publication slot available")

// ErrWrongPublicationKind is returned when a client acts on a publication as though it
// were another kind — muting a screen as a microphone, or reordering a video track with
// audio's clock.
var ErrWrongPublicationKind = errors.New("publication is not of that kind")

// Publication is one participant's live track, as the server understands it.
type Publication struct {
	ID    PublicationID  `json:"id"`
	Owner string         `json:"owner"`
	Kind  PublicationKind `json:"kind"`
	// Muted is the *effective* state, which is the account's own choice AND the
	// server's permission decision combined. Keeping them apart is what lets a
	// permission withdrawal mute a participant without turning into a permanent
	// self-mute they have to undo themselves (sfu.go:66-70).
	Muted bool `json:"muted"`
	// Stopped is set when the track ended — by the client, by the browser ending a
	// capture, or by a server-enforced removal. A stopped publication is removed, not
	// left as a silent track, because a viewer cannot tell the difference and would
	// read it as a person who stopped talking rather than one who left.
	Stopped bool `json:"stopped"`
	// Started is when the publication began, for the duration a UI shows.
	Started time.Time `json:"started"`
	// Generation increments on every stop and restart of the same publication id. A
	// client that reconnects mid-publication uses it to tell a resume from a new
	// publication that happens to have the same slot.
	Generation int `json:"generation"`
}

// publicationRegistry holds the live publications in one room. It is separate from the
// peer map because publications and peers have different lifetimes: a peer leaves and
// its microphone goes with it, but a screen publication is announced separately and a
// viewer may still be subscribed to it for a moment afterwards.
type publicationRegistry struct {
	mu    sync.RWMutex
	items map[PublicationID]*Publication
	// order preserves announcement order so a client that joins later receives the
	// list in the order a room filled, rather than in Go's map order, which changes
	// between calls and would make a participant list flicker on every refresh.
	order []PublicationID
}

func newPublicationRegistry() *publicationRegistry {
	return &publicationRegistry{items: map[PublicationID]*Publication{}}
}

func (r *publicationRegistry) add(publication *Publication) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[publication.ID]; !exists {
		r.order = append(r.order, publication.ID)
	}
	r.items[publication.ID] = publication
}

func (r *publicationRegistry) remove(id PublicationID) (*Publication, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	publication, ok := r.items[id]
	if !ok {
		return nil, false
	}
	delete(r.items, id)
	for i, existing := range r.order {
		if existing == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return publication, true
}

func (r *publicationRegistry) get(id PublicationID) (*Publication, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	publication, ok := r.items[id]
	if !ok {
		return nil, false
	}
	copied := *publication
	return &copied, true
}

// list returns the live publications in announcement order.
func (r *publicationRegistry) list() []Publication {
	r.mu.RLock()
	defer r.mu.RUnlock()
	publications := make([]Publication, 0, len(r.order))
	for _, id := range r.order {
		if publication, ok := r.items[id]; ok {
			publications = append(publications, *publication)
		}
	}
	return publications
}

func (r *publicationRegistry) countOfKind(kind PublicationKind) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := 0
	for _, publication := range r.items {
		if publication.Kind == kind {
			total++
		}
	}
	return total
}

func (r *publicationRegistry) size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}

// PublicationLimits bounds what a room and a server will carry.
//
// The two bounds are separate on purpose. A per-room screen bound is a room policy —
// two people sharing screens in one conversation is chaos and the people in it can
// see that. A serverwide bound is a capacity fact — the process forwards every
// publication it accepts, and there is a limit to how much that is. Conflating them is
// how "one share per room" turns into "one share on the server", which makes a busy
// community unusable for everybody who is not in the room that holds it.
type PublicationLimits struct {
	// PerRoomScreenVideo is how many screen video publications one room carries.
	PerRoomScreenVideo int
	// ServerScreenVideo is how many the whole server carries.
	ServerScreenVideo int
	// PerRoomMicrophones and ServerMicrophones bound audio inputs. They are separate
	// because a server with twenty rooms of ten has two hundred participants long
	// before any one room is full.
	PerRoomMicrophones int
	ServerMicrophones  int
}

// DefaultPublicationLimits are the v1 targets from the master plan: one screen per
// room, two per server, and the participant limits the existing voice path already
// enforces.
//
// The microphone numbers are deliberately not tighter than the participant limits.
// Voice is a hard bound for a reason — ten people talking is a conversation — but
// microphones and *publications* are different things, and a cap that looks generous
// here is the one that has to be measured rather than assumed (ADR 0019, §6).
func DefaultPublicationLimits() PublicationLimits {
	return PublicationLimits{
		PerRoomScreenVideo: 1,
		ServerScreenVideo:  2,
		PerRoomMicrophones: MaxParticipants,
		ServerMicrophones:  20,
	}
}

// admits reports whether a new publication of this kind can be accepted, and why not if
// it cannot. The reason is returned rather than logged because the client has to be
// able to tell a person "only one screen can be shared in this conversation" instead of
// failing silently.
func (l PublicationLimits) admits(kind PublicationKind, roomCount, serverCount int) error {
	switch kind {
	case PublicationScreenVideo:
		if l.PerRoomScreenVideo > 0 && roomCount >= l.PerRoomScreenVideo {
			return errors.New("only one screen can be shared in this conversation at a time")
		}
		if l.ServerScreenVideo > 0 && serverCount >= l.ServerScreenVideo {
			return errors.New("this server is already sharing as many screens as it can carry")
		}
	case PublicationScreenAudio:
		// Screen audio travels with its video, so it is bounded by the video bound and
		// not separately. A separate bound would let somebody publish audio for a
		// screen they cannot publish, which is the shape of a bug rather than a
		// feature.
	case PublicationMicrophone:
		if l.PerRoomMicrophones > 0 && roomCount >= l.PerRoomMicrophones {
			return errors.New("this conversation already has as many microphones as it allows")
		}
		if l.ServerMicrophones > 0 && serverCount >= l.ServerMicrophones {
			return errors.New("this server has as many active microphones as it allows")
		}
	default:
		return ErrWrongPublicationKind
	}
	return nil
}
