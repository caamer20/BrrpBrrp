package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"brrpbrrp/internal/db"
	"github.com/coder/websocket"
)

func TestLocalWorkload(t *testing.T) {
	seconds, _ := strconv.Atoi(os.Getenv("BRRP_LOAD_SECONDS"))
	if seconds < 30 || seconds > 1800 {
		t.Skip("set BRRP_LOAD_SECONDS=30..1800 for twenty connected chat users")
	}
	s, handler := testServer(t)
	setupAdmin(t, s, handler)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var channel string
	s.store.QueryRow(`SELECT id FROM channels WHERE type='text' LIMIT 1`).Scan(&channel)
	cookies := make([]*http.Cookie, 20)
	for i := range cookies {
		id := db.ID()
		if _, err := s.store.Exec(`INSERT INTO users(id,username,password_hash,created_at,updated_at) VALUES(?,?,?,?,?)`, id, fmt.Sprintf("load_%d", i), "synthetic-load-user", db.Now(), db.Now()); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := s.newSession(w, id); err != nil {
			t.Fatal(err)
		}
		cookies[i] = w.Result().Cookies()[0]
		headers := http.Header{"Origin": {"https://localhost:8080"}, "Cookie": {cookies[i].String()}}
		connection, _, err := websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{HTTPHeader: headers})
		if err != nil {
			t.Fatal(err)
		}
		defer connection.CloseNow()
		go func() {
			for {
				if _, _, err := connection.Read(ctx); err != nil {
					return
				}
			}
		}()
	}
	latencies := make([]time.Duration, 0, seconds*4)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Duration(seconds) * time.Second)
	defer deadline.Stop()
	started := time.Now()
	var peak uint64
	for n := 0; ; n++ {
		select {
		case <-deadline.C:
			if s.hub.Count() != 20 {
				t.Fatalf("lost chat connections: %d", s.hub.Count())
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			t.Logf("duration=%s sockets=20 messages=%d p50=%s p95=%s combined_server_and_clients_peak_heap_bytes=%d", time.Since(started).Round(time.Millisecond), len(latencies), latencies[len(latencies)/2], latencies[len(latencies)*95/100], peak)
			return
		case <-ticker.C:
			begin := time.Now()
			w := request(t, handler, "POST", "/api/channels/"+channel+"/messages", messageInput{Content: fmt.Sprintf("local workload message %d", n), IdempotencyKey: fmt.Sprintf("workload-message-%08d", n)}, cookies[n%20])
			if w.Code != 201 {
				t.Fatalf("message failed: %d %s", w.Code, w.Body.String())
			}
			latencies = append(latencies, time.Since(begin))
			if n%20 == 0 {
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				peak = max(peak, stats.Alloc)
			}
		}
	}
}
