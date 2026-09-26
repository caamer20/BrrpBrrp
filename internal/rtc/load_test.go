package rtc

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestLocalWorkload(t *testing.T) {
	seconds, _ := strconv.Atoi(os.Getenv("BRRP_LOAD_SECONDS"))
	if seconds < 30 || seconds > 1800 {
		t.Skip("set BRRP_LOAD_SECONDS=30..1800 for a synthetic ten-peer transport workload")
	}
	sfu, err := New(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sfu.Close)
	peers := make([]*testPeer, 10)
	for i := range peers {
		peers[i] = connectPeerWithCapacity(t, sfu, fmt.Sprint(i), "room", 10)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Duration(seconds) * time.Second)
	defer deadline.Stop()
	started := time.Now()
	sent := 0
	var received [10]int
	var peak uint64
	for {
		select {
		case <-deadline.C:
			for i, count := range received {
				if count < sent/10*7 {
					t.Fatalf("peer %d received too few forwarded packets: %d, sent per source %d", i, count, sent/10)
				}
			}
			t.Logf("duration=%s peers=10 payload=60B interval=20ms sent=%d received=%v combined_server_and_clients_peak_heap_bytes=%d", time.Since(started).Round(time.Millisecond), sent, received, peak)
			return
		case <-ticker.C:
			for i, peer := range peers {
				payload := make([]byte, 60)
				payload[0] = byte(i)
				if err := peer.send(payload); err != nil {
					t.Fatal(err)
				}
				sent++
			}
			for i, peer := range peers {
				for len(peer.packets) > 0 {
					<-peer.packets
					received[i]++
				}
			}
			if sent%500 == 0 {
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				peak = max(peak, stats.Alloc)
			}
		}
	}
}
