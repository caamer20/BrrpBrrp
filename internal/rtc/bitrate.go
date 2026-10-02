package rtc

import (
	"sync"
	"time"
)

// The per-publication bitrate ceilings. They are the plan's initial profile: one
// screen picture and its optional audio per publisher, bounded server-side so that a
// publisher cannot starve the voice the call exists for.
//
// The enforcement is at ingress rather than at egress. The server's cost for one screen
// packet is that packet written to every viewer, so a publisher that ignores its own
// cap costs the room a multiple of what it sends; the ceiling is applied where every
// packet passes exactly once, before any copy is made. A client that respects the cap
// its preset applies never reaches this code with a packet to drop
// (`SCREEN_PRESETS[…].parameters.maxBitrate` in the web client is 2 Mbit/s).
const (
	ScreenBitrateCeiling      = 2_500_000 // bits per second, per screen publication
	ScreenAudioBitrateCeiling = 64_000    // bits per second, per screen-audio publication
)

// bitrateMeter is a token bucket over a publication's payload bytes.
//
// It polices rather than shapes: over-budget packets are dropped, not queued. A queue
// would add latency to a stream that is already over budget and would let the memory a
// publisher can consume grow without bound, and neither is a bound on anything.
//
// One second of credit is available to a burst, which is what a keyframe needs: it
// arrives as a burst by nature, and a meter with no burst would drop the frame the
// picture starts with — turning "this share is over budget" into "this share shows
// nothing".
type bitrateMeter struct {
	mu      sync.Mutex
	rate    float64 // bytes per second
	burst   float64 // the most credit that can accumulate, in bytes
	credit  float64
	updated time.Time
	dropped uint64
}

func newBitrateMeter(bitsPerSecond int, now time.Time) *bitrateMeter {
	rate := float64(bitsPerSecond) / 8
	return &bitrateMeter{rate: rate, burst: rate, credit: rate, updated: now}
}

// allow spends this packet's payload from the bucket and reports whether it fit. Time
// is a parameter rather than read inside, so the behaviour a test asserts is the
// behaviour production runs.
func (m *bitrateMeter) allow(bytes int, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if elapsed := now.Sub(m.updated).Seconds(); elapsed > 0 {
		m.credit += elapsed * m.rate
		if m.credit > m.burst {
			m.credit = m.burst
		}
		m.updated = now
	}
	if float64(bytes) > m.credit {
		m.dropped++
		return false
	}
	m.credit -= float64(bytes)
	return true
}

// dropCount reports how many packets this publication has had refused. It is the
// server's own count: a client's statistics say nothing about what the server dropped,
// which is the same reason the forwarders count what they wrote.
func (m *bitrateMeter) dropCount() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dropped
}

// admit polices one packet of a publication against its kind's ceiling.
//
// It is called with the SFU's lock held, which is what makes the meter map safe to
// touch without a second lock of its own; the meter's own lock then covers the credit
// arithmetic, so a diagnostic can read a drop count while packets are being policed.
func (p *Peer) admit(kind PublicationKind, bytes int, now time.Time) bool {
	if p.police == nil {
		p.police = make(map[PublicationKind]*bitrateMeter)
	}
	meter := p.police[kind]
	if meter == nil {
		ceiling := ScreenBitrateCeiling
		if kind == PublicationScreenAudio {
			ceiling = ScreenAudioBitrateCeiling
		}
		meter = newBitrateMeter(ceiling, now)
		p.police[kind] = meter
	}
	return meter.allow(bytes, now)
}

// bitrateDrops reports how many of a publisher's packets the ceiling has refused.
//
// It exists so the operator-facing view of an over-budget publisher is the server's own
// count rather than an inference from a broken picture; the metrics surface reads it
// when that lands (ENG-14).
func (s *SFU) bitrateDrops(userID string, kind PublicationKind) uint64 {
	s.mu.Lock()
	peer := s.peers[userID]
	var meter *bitrateMeter
	if peer != nil {
		meter = peer.police[kind]
	}
	s.mu.Unlock()
	if meter == nil {
		return 0
	}
	return meter.dropCount()
}
