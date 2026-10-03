package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/app"
	"brrpbrrp/internal/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// The hello frame is the handshake. A tab can be open across a server update, and the
// bundle in it is then older than the server: it has to learn that before it sends text
// the server would store differently, and it has to keep the text it holds while it
// updates. The frame therefore carries the protocol this server speaks, the oldest one it
// still accepts, and the version a person can be told to update from.
func TestTheHelloFrameCarriesTheProtocolHandshake(t *testing.T) {
	s, handler := testServer(t)
	admin := setupAdmin(t, s, handler)

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Cookie": []string{admin.String()}, "Origin": []string{s.config.BaseURL}}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("a current session could not connect: %v", err)
	}
	defer conn.CloseNow()

	var hello ws.Event
	if err := wsjson.Read(ctx, conn, &hello); err != nil {
		t.Fatalf("no hello frame: %v", err)
	}
	if hello.Type != "hello" {
		t.Fatalf("the first frame was %q, want hello", hello.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(hello.Data, &payload); err != nil {
		t.Fatalf("the hello payload is not an object: %v", err)
	}
	number := func(key string) int {
		value, ok := payload[key].(float64)
		if !ok {
			t.Fatalf("hello carries no %s: %v", key, payload)
		}
		return int(value)
	}
	if got := number("protocol"); got != ws.Protocol {
		t.Fatalf("hello says protocol %d, this build speaks %d", got, ws.Protocol)
	}
	if got := number("protocol_min"); got != ws.ProtocolMin {
		t.Fatalf("hello accepts from %d, this build accepts from %d", got, ws.ProtocolMin)
	}
	if ws.ProtocolMin > ws.Protocol {
		t.Fatalf("the accepted range is empty: min %d, protocol %d", ws.ProtocolMin, ws.Protocol)
	}
	// The version is what a person is told when the two disagree: "update the app" is not
	// actionable without knowing which build the server is.
	if payload["version"] != app.Version {
		t.Fatalf("hello says version %v, this build is %s", payload["version"], app.Version)
	}
	// The permitted control: the fields this frame already had are still there, so the
	// handshake is an addition rather than a replacement.
	if id, ok := payload["user_id"].(string); !ok || id == "" {
		t.Fatalf("hello lost its user_id: %v", payload)
	}
	if payload["refetch"] != true {
		t.Fatalf("hello stopped asking for a refetch: %v", payload)
	}
}
