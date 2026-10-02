package rtc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/turn/v4"
	"github.com/pion/webrtc/v4"
)

type TURN struct {
	mu     sync.Mutex
	server *turn.Server
	secret []byte
	host   string
	port   int
	// tcpPort and tlsPort are the fallback listeners. UDP alone leaves a client behind
	// a network that blocks it with no path at all, which is the ordinary case on
	// corporate and some mobile networks (ADR 0019, §7). A zero port means the
	// fallback is not offered for that transport.
	tcpPort int
	tlsPort int
	tlsCert tls.Certificate
	// advertised is the host clients are told to relay through. It is configurable
	// because a host or container behind NAT that advertises its private address
	// produces a call that connects locally and fails for everybody else.
	advertised    string
	credentialsMu sync.Mutex
	issued        map[string]string
}

func NewTURN() (*TURN, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &TURN{secret: secret, issued: make(map[string]string)}, nil
}

// Configure brings the relay up on UDP and, where configured, on TCP and TLS.
func (t *TURN) Configure(enabled bool, host string, port int) error {
	return t.ConfigureWithFallback(enabled, host, port, 0, 0, "", nil)
}

// ConfigureWithFallback is Configure with the extra transports and the advertised
// address. The plain Configure keeps its signature for the callers that only need UDP,
// and the extra arguments are read from settings by the one caller that has them.
func (t *TURN) ConfigureWithFallback(enabled bool, host string, port, tcpPort, tlsPort int, advertised string, certificate *tls.Certificate) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !enabled {
		if t.server != nil {
			_ = t.server.Close()
			t.server = nil
		}
		return nil
	}
	if t.server != nil && t.host == host && t.port == port && t.tcpPort == tcpPort && t.tlsPort == tlsPort && t.advertised == advertised {
		return nil
	}
	if t.server != nil {
		return errors.New("disable TURN before changing its address or port")
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || port < 1024 || port > 65535 {
		return errors.New("TURN needs an IPv4 address reachable by clients and a port from 1024 to 65535")
	}
	listener, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	allowed := []net.IP{ip}
	if interfaces, err := net.InterfaceAddrs(); err == nil {
		for _, address := range interfaces {
			if network, ok := address.(*net.IPNet); ok {
				allowed = append(allowed, network.IP)
			}
		}
	}
	config := turn.ServerConfig{
		Realm: "brrpbrrp",
		AuthHandler: func(username, realm string, _ net.Addr) ([]byte, bool) {
			parts := strings.SplitN(username, ":", 2)
			expiry, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil || len(parts) != 2 || expiry < time.Now().Unix() || expiry > time.Now().Add(24*time.Hour+time.Minute).Unix() {
				return nil, false
			}
			t.credentialsMu.Lock()
			_, issued := t.issued[username]
			t.credentialsMu.Unlock()
			if !issued {
				return nil, false
			}
			return turn.GenerateAuthKey(username, realm, t.password(username)), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn:            listener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{RelayAddress: ip, Address: "0.0.0.0", MinPort: 49152, MaxPort: 49999},
			PermissionHandler:     permissionHandler(allowed),
		}},
	}
	// The TCP listener is the fallback that makes a call possible at all on a network
	// which blocks UDP. It is separate rather than a replacement: ICE is offered both
	// and picks, so a client that can use UDP still does.
	if tcpPort > 0 {
		tcpListener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", tcpPort))
		if err != nil {
			listener.Close()
			return fmt.Errorf("TURN TCP fallback: %w", err)
		}
		config.ListenerConfigs = []turn.ListenerConfig{{
			Listener:              tcpListener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{RelayAddress: ip, Address: "0.0.0.0", MinPort: 49152, MaxPort: 49999},
			PermissionHandler:     permissionHandler(allowed),
		}}
	}
	if tlsPort > 0 {
		if certificate == nil {
			listener.Close()
			return errors.New("TURN TLS fallback needs a certificate")
		}
		// A certificate is required rather than optional: offering `turns:` without one
		// would give a client a URL that cannot connect, which is worse than not
		// offering it, because ICE reports the failure as a network problem.
		tlsListener, err := tls.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", tlsPort), &tls.Config{Certificates: []tls.Certificate{*certificate}, MinVersion: tls.VersionTLS12})
		if err != nil {
			listener.Close()
			return fmt.Errorf("TURN TLS fallback: %w", err)
		}
		config.ListenerConfigs = append(config.ListenerConfigs, turn.ListenerConfig{
			Listener:              tlsListener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{RelayAddress: ip, Address: "0.0.0.0", MinPort: 49152, MaxPort: 49999},
			PermissionHandler:     permissionHandler(allowed),
		})
	}
	server, err := turn.NewServer(config)
	if err != nil {
		listener.Close()
		return err
	}
	t.server, t.host, t.port, t.tcpPort, t.tlsPort, t.advertised = server, host, port, tcpPort, tlsPort, advertised
	if advertised != "" {
		t.tlsCert = tls.Certificate{}
		if certificate != nil {
			t.tlsCert = *certificate
		}
	}
	return nil
}

// permissionHandler admits a peer only when it is an address this host can actually
// reach. The relay must not become a way to reach an arbitrary third party.
func permissionHandler(allowed []net.IP) func(net.Addr, net.IP) bool {
	return func(_ net.Addr, peer net.IP) bool {
		for _, address := range allowed {
			if peer.Equal(address) {
				return true
			}
		}
		return false
	}
}

func (t *TURN) password(username string) string {
	hash := hmac.New(sha1.New, t.secret)
	_, _ = hash.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

func (t *TURN) Credentials(userID string) (webrtc.ICEServer, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.server == nil {
		return webrtc.ICEServer{}, false
	}
	username := fmt.Sprintf("%d:%s", time.Now().Add(24*time.Hour).Unix(), userID)
	t.credentialsMu.Lock()
	for name := range t.issued {
		expiry, _ := strconv.ParseInt(strings.SplitN(name, ":", 2)[0], 10, 64)
		if expiry < time.Now().Unix() {
			delete(t.issued, name)
		}
	}
	t.issued[username] = userID
	t.credentialsMu.Unlock()
	// One server, every transport it is listening on. ICE picks; the client is not
	// told which to use, and a network which blocks UDP falls through to TCP or TLS
	// without anything in the client having to detect it (ADR 0019, §7).
	host := t.advertised
	if host == "" {
		host = t.host
	}
	urls := []string{fmt.Sprintf("turn:%s:%d?transport=udp", host, t.port)}
	if t.tcpPort > 0 {
		urls = append(urls, fmt.Sprintf("turn:%s:%d?transport=tcp", host, t.tcpPort))
	}
	if t.tlsPort > 0 {
		urls = append(urls, fmt.Sprintf("turns:%s:%d?transport=tcp", host, t.tlsPort))
	}
	return webrtc.ICEServer{URLs: urls, Username: username, Credential: t.password(username)}, true
}

func (t *TURN) Close() { _ = t.Configure(false, "", 0) }

// RevokeUser prevents new allocations and renewals after a voice session ends.
func (t *TURN) RevokeUser(userID string) {
	t.credentialsMu.Lock()
	defer t.credentialsMu.Unlock()
	for credential, user := range t.issued {
		if user == userID {
			delete(t.issued, credential)
		}
	}
}
