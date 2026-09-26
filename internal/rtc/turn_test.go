package rtc

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/turn/v4"
)

func TestTURNRelayAndRevocation(t *testing.T) {
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	server, err := NewTURN()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Configure(true, "127.0.0.1", port); err != nil {
		t.Fatal(err)
	}
	credentials, enabled := server.Credentials("member")
	if !enabled {
		t.Fatal("TURN was not enabled")
	}
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	client, err := turn.NewClient(&turn.ClientConfig{Conn: socket, TURNServerAddr: address, STUNServerAddr: address, Username: credentials.Username, Password: credentials.Credential.(string), Realm: "brrpbrrp"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Listen(); err != nil {
		t.Fatal(err)
	}
	relay, err := client.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	destination, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	destination.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := relay.WriteTo([]byte("voice relay check"), destination.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 100)
	n, _, err := destination.ReadFrom(buffer)
	if err != nil || string(buffer[:n]) != "voice relay check" {
		t.Fatal("TURN did not relay traffic", err)
	}
	server.RevokeUser("member")
	server.credentialsMu.Lock()
	remaining := len(server.issued)
	server.credentialsMu.Unlock()
	if remaining != 0 {
		t.Fatal("relay credentials survived revocation")
	}
	if err := server.Configure(false, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, enabled := server.Credentials("member"); enabled {
		t.Fatal("disabled TURN issued credentials")
	}
}
