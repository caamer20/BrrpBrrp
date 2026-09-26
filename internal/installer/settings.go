package installer

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"brrpbrrp/internal/app"
)

type Settings struct {
	Mode           string `json:"mode"`
	Port           int    `json:"port"`
	DataDir        string `json:"data_dir"`
	NetworkHost    string `json:"network_host"`
	PublicURL      string `json:"public_url"`
	TrustedProxies string `json:"trusted_proxies"`
	VoiceIP        string `json:"voice_ip"`
	AllowFirewall  bool   `json:"allow_firewall"`
}

type Paths struct {
	InstallDir string `json:"install_dir"`
	Binary     string `json:"binary"`
	DataDir    string `json:"data_dir"`
	Config     string `json:"config"`
}

type Plan struct {
	Settings  Settings `json:"settings"`
	Paths     Paths    `json:"paths"`
	URL       string   `json:"url"`
	Address   string   `json:"address"`
	FreeBytes uint64   `json:"free_bytes"`
	Warnings  []string `json:"warnings"`
}

func makePlan(input Settings, defaults Paths, addresses []string) (Plan, error) {
	p := Plan{Settings: input, Paths: defaults, Warnings: []string{}}
	if input.Port < 1024 || input.Port > 65535 {
		return p, errors.New("Choose a port between 1024 and 65535.")
	}
	if !filepath.IsAbs(input.DataDir) || strings.ContainsAny(input.DataDir, "\x00\r\n") {
		return p, errors.New("Choose an absolute path for the data folder.")
	}
	p.Paths.DataDir = filepath.Clean(input.DataDir)
	p.Paths.Config = filepath.Join(p.Paths.DataDir, "server.env")
	if p.Paths.DataDir == filepath.Dir(p.Paths.DataDir) || p.Paths.DataDir == p.Paths.InstallDir {
		return p, errors.New("Choose a dedicated data folder, separate from the application folder.")
	}
	if _, err := app.Contained(p.Paths.DataDir, p.Paths.Config); err != nil {
		return p, fmt.Errorf("Data folder: %w", err)
	}
	port := strconv.Itoa(input.Port)
	p.Address = net.JoinHostPort("127.0.0.1", port)
	switch input.Mode {
	case "computer":
		p.URL = "http://" + p.Address
		p.Settings.AllowFirewall = false
		p.Settings.VoiceIP = ""
	case "network":
		ip, err := netip.ParseAddr(input.NetworkHost)
		if err != nil || !ip.Is4() || ip.IsLoopback() || !ip.IsPrivate() {
			return p, errors.New("Choose this server's private IPv4 address for local-network access.")
		}
		if !slices.Contains(addresses, ip.String()) {
			return p, errors.New("That address does not belong to this computer. Select an address from the list.")
		}
		p.Address = net.JoinHostPort(input.NetworkHost, port)
		p.URL = "https://" + p.Address
		p.Settings.VoiceIP = input.NetworkHost
		p.Warnings = append(p.Warnings, "Trust this server's certificate on each device before using voice. Reserve this IP address in your router so the server address stays the same.")
	case "proxy":
		u, err := url.Parse(input.PublicURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return p, errors.New("Enter your HTTPS address without a path, for example https://chat.example.com.")
		}
		p.URL = u.String()
		if strings.TrimSpace(input.TrustedProxies) == "" {
			return p, errors.New("Enter the IP ranges of the proxy you trust.")
		}
		for _, raw := range strings.Split(input.TrustedProxies, ",") {
			if _, err := netip.ParsePrefix(strings.TrimSpace(raw)); err != nil {
				return p, errors.New("Trusted proxies must be comma-separated CIDRs, such as 127.0.0.1/32,::1/128.")
			}
		}
		p.Warnings = append(p.Warnings, "Configure your HTTPS proxy to forward to this computer's loopback port, including WebSockets. Voice also needs a reachable UDP route; an HTTP proxy alone does not carry audio.")
	default:
		return p, errors.New("Choose how people will connect to this server.")
	}
	if p.Settings.VoiceIP != "" && net.ParseIP(p.Settings.VoiceIP) == nil {
		return p, errors.New("The advertised voice address must be an IP address.")
	}
	return p, nil
}

func checkDestination(p *Plan) error {
	for _, file := range []string{p.Paths.Binary, p.Paths.Config, filepath.Join(p.Paths.DataDir, "brrp.db"), filepath.Join(p.Paths.InstallDir, "installation.json")} {
		if _, err := os.Lstat(file); err == nil {
			if file == p.Paths.Binary || file == filepath.Join(p.Paths.InstallDir, "installation.json") {
				return fmt.Errorf("BrrpBrrp is already installed at %s. Choose Uninstall an existing version below before starting a fresh installation.", p.Paths.InstallDir)
			}
			return errors.New("This folder contains an existing server's data or settings. Choose an empty folder, or use Uninstall an existing version to remove the application and keep the old data.")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if entries, err := os.ReadDir(p.Paths.DataDir); err == nil && len(entries) > 0 {
		return errors.New("The data folder must be empty for a new installation.")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("tcp", p.Address)
	if err != nil {
		return fmt.Errorf("Port %d is unavailable. Choose another port. (%v)", p.Settings.Port, err)
	}
	listener.Close()
	parent := p.Paths.DataDir
	for {
		if _, err := os.Stat(parent); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return errors.New("Could not find the data folder's disk.")
		}
		parent = next
	}
	p.FreeBytes, err = app.FreeDiskBytes(parent)
	if err != nil {
		return fmt.Errorf("Could not check available storage: %w", err)
	}
	if p.FreeBytes < 256*1024*1024 {
		return errors.New("At least 256 MB of free storage is needed to finish installation. Allow more space for attachments and backups.")
	}
	return nil
}

func (p Plan) configText() string {
	settings := [][2]string{
		{"BRRP_DATA_DIR", p.Paths.DataDir}, {"BRRP_ADDR", p.Address}, {"BRRP_BASE_URL", p.URL},
		{"BRRP_LOG_LEVEL", "info"}, {"BRRP_LOG_FILE", "logs/server.log"},
		{"BRRP_VOICE_PORT_MIN", "50000"}, {"BRRP_VOICE_PORT_MAX", "50100"}, {"BRRP_VOICE_EXTERNAL_IP", p.Settings.VoiceIP},
	}
	switch p.Settings.Mode {
	case "computer":
		settings = append(settings, [2]string{"BRRP_TLS_MODE", "http-local"})
	case "network":
		settings = append(settings, [2]string{"BRRP_TLS_MODE", "auto"})
	case "proxy":
		settings = append(settings, [2]string{"BRRP_TLS_MODE", "http-behind-proxy"}, [2]string{"BRRP_BEHIND_PROXY", "true"}, [2]string{"BRRP_TRUSTED_PROXIES", p.Settings.TrustedProxies})
	}
	var out strings.Builder
	out.WriteString("# BrrpBrrp server settings. Restart the service after editing.\n")
	for _, entry := range settings {
		fmt.Fprintf(&out, "%s=%s\n", entry[0], strconv.Quote(entry[1]))
	}
	return out.String()
}

func localIPs() []string {
	ips := []string{}
	addresses, _ := net.InterfaceAddrs()
	for _, addr := range addresses {
		prefix, err := netip.ParsePrefix(addr.String())
		if err == nil && prefix.Addr().Is4() && prefix.Addr().IsPrivate() {
			ips = append(ips, prefix.Addr().String())
		}
	}
	return ips
}
