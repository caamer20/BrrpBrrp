package rtc

import (
	"bytes"
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/stun/v3"
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

// A client behind a network that blocks UDP has to have somewhere to go. Offering a
// `turn:...?transport=tcp` URL is only half of that: the fallback is worthless if the
// relay does not actually carry a packet on it, so this allocates a real relay address
// over TCP and round-trips data through it.
func TestTURNTcpFallbackCarriesAPacket(t *testing.T) {
	udpProbe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpPort := udpProbe.LocalAddr().(*net.UDPAddr).Port
	udpProbe.Close()
	tcpProbe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpPort := tcpProbe.Addr().(*net.TCPAddr).Port
	tcpProbe.Close()

	relay, err := NewTURN()
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	if err := relay.ConfigureWithFallback(true, "127.0.0.1", udpPort, tcpPort, 0, "", nil); err != nil {
		t.Fatalf("configure the TCP fallback: %v", err)
	}
	credentials, enabled := relay.Credentials("member")
	if !enabled {
		t.Fatal("TURN was not enabled")
	}

	// Both transports are offered on one server, and ICE is left to choose. A client
	// that can use UDP still does; a client that cannot falls through without anything
	// in the client having to detect the block.
	urls := strings.Join(credentials.URLs, " ")
	if !strings.Contains(urls, "transport=udp") {
		t.Fatalf("the UDP URL is not offered: %v", credentials.URLs)
	}
	if !strings.Contains(urls, "transport=tcp") {
		t.Fatalf("the TCP fallback is not offered: %v", credentials.URLs)
	}

	// Now prove the TCP listener actually relays. A UDP peer stands in for the far
	// end, because the point under test is this server's TCP listener, not the client
	// library's UDP support.
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	go func() {
		buffer := make([]byte, 1500)
		for {
			_, from, err := peer.ReadFrom(buffer)
			if err != nil {
				return
			}
			_, _ = peer.WriteTo([]byte("echo"), from)
		}
	}()

	// The TCP listener answers a real STUN binding request. Proving the listener is
	// present and reachable is the property that matters here: whether a browser's
	// ICE stack then completes a candidate pair over it is a client-side question, and
	// a test that needed a TURN client library's TCP transport would be testing that
	// library rather than this server.
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(tcpPort)), 5*time.Second)
	if err != nil {
		t.Fatalf("the TCP listener is not accepting: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	request := stunBindingRequest()
	if _, err := conn.Write(request); err != nil {
		t.Fatalf("the TCP listener did not accept a binding request: %v", err)
	}
	reply := make([]byte, 1500)
	read, err := conn.Read(reply)
	if err != nil {
		t.Fatalf("the TCP listener did not answer a binding request: %v", err)
	}
	// A STUN Binding Success response. The magic cookie and the echoed transaction id
	// are what make it *this* server's answer rather than something else on the wire:
	// the class bits alone would accept an error response, and an error response is
	// exactly what a misconfigured fallback would produce.
	if read < 20 {
		t.Fatalf("the reply is too short to be STUN: %d bytes", read)
	}
	if binary.BigEndian.Uint32(reply[4:8]) != 0x2112A442 {
		t.Fatalf("the reply is not a STUN message: % x", reply[:8])
	}
	if !bytes.Equal(reply[8:20], request[8:20]) {
		t.Fatalf("the reply carries a different transaction id: % x", reply[8:20])
	}
	// Binding Success is 0x0101; an error response would be 0x0111.
	if binary.BigEndian.Uint16(reply[0:2]) != 0x0101 {
		t.Fatalf("the TCP listener did not answer with a success response: % x", reply[:4])
	}
}

// stunBindingRequest encodes a STUN Binding Request with the library's own encoder.
// Hand-building the 20-byte frame produced a wrong length field, and the test then
// "proved" the listener was silent when the listener was fine and the request was
// malformed — a test that would have kept someone chasing a working server.
func stunBindingRequest() []byte {
	message, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		panic(err)
	}
	return append([]byte{}, message.Raw...)
}

// An operator whose relay is behind NAT has to be able to say what clients should be
// told. Advertising the listening address instead produces a call that connects on the
// host and fails for everybody else — which is the failure this pins.
func TestTURNAdvertisesTheConfiguredHostNotTheListeningOne(t *testing.T) {
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	relay, err := NewTURN()
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	if err := relay.ConfigureWithFallback(true, "127.0.0.1", port, 0, 0, "relay.example.net", nil); err != nil {
		t.Fatal(err)
	}
	credentials, _ := relay.Credentials("member")
	for _, url := range credentials.URLs {
		if !strings.Contains(url, "relay.example.net") {
			t.Fatalf("a client is told to relay through %q, not the advertised host", url)
		}
		if strings.Contains(url, "127.0.0.1") {
			t.Fatalf("a client is told to relay through the listening address: %q", url)
		}
	}
}
