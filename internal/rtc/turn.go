package rtc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
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
	mu            sync.Mutex
	server        *turn.Server
	secret        []byte
	host          string
	port          int
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

func (t *TURN) Configure(enabled bool, host string, port int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !enabled {
		if t.server != nil {
			_ = t.server.Close()
			t.server = nil
		}
		return nil
	}
	if t.server != nil && t.host == host && t.port == port {
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
	server, err := turn.NewServer(turn.ServerConfig{
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
			PermissionHandler: func(_ net.Addr, peer net.IP) bool {
				for _, address := range allowed {
					if peer.Equal(address) {
						return true
					}
				}
				return false
			},
		}},
	})
	if err != nil {
		listener.Close()
		return err
	}
	t.server, t.host, t.port = server, host, port
	return nil
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
	return webrtc.ICEServer{URLs: []string{fmt.Sprintf("turn:%s:%d?transport=udp", t.host, t.port)}, Username: username, Credential: t.password(username)}, true
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
