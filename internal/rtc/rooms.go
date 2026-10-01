package rtc

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Move checks capacity and changes room membership under the same lock. The
// microphone and negotiated tracks stay connected throughout the transfer.
func (s *SFU) Move(userID, owner, channelID string, capacity int) error {
	s.mu.Lock()
	peer := s.peers[userID]
	if peer == nil || peer.owner != owner {
		s.mu.Unlock()
		return errors.New("join voice in this tab first")
	}
	if peer.state.ChannelID == channelID {
		s.mu.Unlock()
		return nil
	}
	used := make(map[int]bool)
	for _, other := range s.peers {
		if other.state.ChannelID == channelID {
			used[other.slot] = true
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
	peer.state.ChannelID, peer.slot, peer.state.Speaking = channelID, slot, false
	peer.requestedSpeaking = false
	peer.announced = false
	s.mu.Unlock()
	s.publish()
	if peer.pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
		s.announceJoin(userID, peer)
	}
	return nil
}

func (s *SFU) ResumeToken(userID, owner string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if peer := s.peers[userID]; peer != nil && peer.owner == owner {
		return peer.resumeToken
	}
	return ""
}

func (s *SFU) ConnectionID(userID, owner string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if peer := s.peers[userID]; peer != nil && peer.owner == owner {
		return peer.connectionID
	}
	return ""
}

// A short signaling interruption may leave the media connection healthy.
func (s *SFU) Detach(userID, owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.peers[userID]
	if peer == nil || peer.owner != owner {
		return
	}
	peer.sendMu.Lock()
	peer.send = nil
	peer.sendMu.Unlock()
	if peer.detachTimer != nil {
		peer.detachTimer.Stop()
	}
	peer.detachTimer = time.AfterFunc(30*time.Second, func() {
		s.mu.Lock()
		current := s.peers[userID] == peer && peer.owner == owner && peer.detachTimer != nil
		s.mu.Unlock()
		if current {
			s.leavePeerOwned(userID, peer, owner)
		}
	})
}

func (s *SFU) Resume(userID, token, owner string, send func(string, any)) (State, error) {
	s.mu.Lock()
	peer := s.peers[userID]
	if token == "" || peer == nil || peer.resumeToken != token {
		s.mu.Unlock()
		return State{}, errors.New("your previous voice connection has ended")
	}
	if peer.detachTimer != nil {
		peer.detachTimer.Stop()
		peer.detachTimer = nil
	}
	peer.owner, peer.resumeToken = owner, uuid.NewString()
	peer.sendMu.Lock()
	peer.send = send
	peer.sendMu.Unlock()
	state := peer.state
	s.mu.Unlock()
	s.publish()
	return state, nil
}

func (s *SFU) publishRouting() {
	type delivery struct {
		peer  *Peer
		slots []string
	}
	s.mu.Lock()
	list := make([]delivery, 0, len(s.peers))
	s.routingVersion++
	version := s.routingVersion
	for _, destination := range s.peers {
		slots := make([]string, MaxRoomParticipants-1)
		for _, source := range s.peers {
			if source == destination || source.state.ChannelID != destination.state.ChannelID {
				continue
			}
			slot := source.slot
			if slot > destination.slot {
				slot--
			}
			slots[slot] = source.state.UserID
		}
		list = append(list, delivery{destination, slots})
	}
	s.mu.Unlock()
	for _, item := range list {
		item.peer.emit("voice.routing", map[string]any{"slots": item.slots, "version": version})
	}
}
