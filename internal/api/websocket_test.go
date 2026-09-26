package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func readEvent(t *testing.T, ctx context.Context, conn *websocket.Conn, kind string) ws.Event {
	t.Helper()
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, conn, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == kind {
			return event
		}
	}
}

func TestWebSocketDeliveryAndSessionRevocation(t *testing.T) {
	s, handler := testServer(t)
	cookie := setupAdmin(t, s, handler)
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{s.config.BaseURL}}
	connect := func() *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		readEvent(t, ctx, conn, "hello")
		return conn
	}
	first, second := connect(), connect()
	channels, err := s.channels()
	if err != nil {
		t.Fatal(err)
	}
	var channelID string
	for _, channel := range channels {
		if channel.Type == "text" {
			channelID = channel.ID
		}
	}
	event := map[string]any{"t": "message.send", "d": map[string]string{"channel_id": channelID, "content": "Live delivery", "idempotency_key": "socket-send-1"}}
	if err := wsjson.Write(ctx, first, event); err != nil {
		t.Fatal(err)
	}
	readEvent(t, ctx, second, "message.created")
	readEvent(t, ctx, first, "ack")
	response := request(t, handler, "POST", "/api/logout", map[string]any{}, cookie)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	for {
		var event ws.Event
		if err := wsjson.Read(ctx, second, &event); err != nil {
			if ctx.Err() != nil {
				t.Fatal("logout did not close the connection")
			}
			break
		}
	}
}
