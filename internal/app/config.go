package app

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"brrpbrrp/internal/db"
)

const MaxUpload = 12 * 1024 * 1024

// LoginAttemptsDefault is how many sign-in attempts one address may make in five
// minutes before the server answers 429. It is deliberately small: the limit is what
// makes online guessing expensive. It is also address-wide, which is why it is
// configurable: every account behind one office NAT, VPN or reverse proxy shares it, and
// a community that signs in from one of those can exhaust it with ordinary use rather
// than with an attack. Raising it trades that protection for reachability; lowering it
// is not supported below one attempt.
const LoginAttemptsDefault = 20

type Config struct {
	DataDir, Addr, BaseURL, TLSMode, Domain, ACMEEmail                      string
	CertFile, KeyFile, DBFile, UploadDir, SetupTokenFile, LogFile, LogLevel string
	BehindProxy, AllowInsecure                                              bool
	// RestrictedChannels exposes private community channels and their access
	// grants through the API. It stays off by default so an incomplete privacy
	// surface is never reachable by users before the privacy gate passes.
	RestrictedChannels   bool
	PrivateConversations bool
	TrustedProxies       []netip.Prefix
	MaxUploadBytes       int64
	LoginAttempts        int
	// SQLiteSynchronous is the durability level for committed transactions. The
	// production baseline is FULL: a commit survives an operating-system crash or
	// power loss on honest storage. NORMAL is available for disposable or
	// development installations and can lose recent commits in that scenario.
	SQLiteSynchronous          string
	VoiceExternalIP            string
	BackupCopyDir              string
	VoicePortMin, VoicePortMax uint16
}

func LoadConfig() (Config, error) {
	c := Config{DataDir: env("BRRP_DATA_DIR", "./data"), Addr: env("BRRP_ADDR", ":8080"), BaseURL: strings.TrimRight(env("BRRP_BASE_URL", "https://localhost:8080"), "/"), TLSMode: env("BRRP_TLS_MODE", "auto"), Domain: os.Getenv("BRRP_DOMAIN"), ACMEEmail: os.Getenv("BRRP_ACME_EMAIL"), LogLevel: env("BRRP_LOG_LEVEL", "info"), VoiceExternalIP: os.Getenv("BRRP_VOICE_EXTERNAL_IP")}
	var err error
	if raw := os.Getenv("BRRP_BACKUP_COPY_DIR"); raw != "" {
		if !filepath.IsAbs(raw) {
			return c, errors.New("BRRP_BACKUP_COPY_DIR must be an absolute path")
		}
		c.BackupCopyDir, err = resolveRoot(raw)
		if err != nil {
			return c, err
		}
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	// macOS exposes its temporary directory through /var, a system symlink.
	// Resolve the selected root once; paths beneath it still reject symlinks.
	c.DataDir, err = resolveRoot(c.DataDir)
	if err != nil {
		return c, err
	}
	if c.BehindProxy, err = boolean("BRRP_BEHIND_PROXY"); err != nil {
		return c, err
	}
	if c.AllowInsecure, err = boolean("BRRP_ALLOW_INSECURE_HTTP"); err != nil {
		return c, err
	}
	if c.RestrictedChannels, err = boolean("BRRP_ENABLE_RESTRICTED_CHANNELS"); err != nil {
		return c, err
	}
	if c.PrivateConversations, err = boolean("BRRP_ENABLE_PRIVATE_CONVERSATIONS"); err != nil {
		return c, err
	}
	c.SQLiteSynchronous = strings.ToUpper(env("BRRP_SQLITE_SYNCHRONOUS", db.DurabilityFull))
	if c.SQLiteSynchronous != db.DurabilityFull && c.SQLiteSynchronous != db.DurabilityNormal {
		return c, errors.New("BRRP_SQLITE_SYNCHRONOUS must be " + db.DurabilityFull + " or " + db.DurabilityNormal)
	}
	n, err := number("BRRP_MAX_UPLOAD_BYTES", MaxUpload, 1, MaxUpload)
	if err != nil {
		return c, err
	}
	c.MaxUploadBytes = int64(n)
	if c.LoginAttempts, err = number("BRRP_LOGIN_ATTEMPTS", LoginAttemptsDefault, 1, 100000); err != nil {
		return c, err
	}
	min, err := number("BRRP_VOICE_PORT_MIN", 50000, 1024, 65535)
	if err != nil {
		return c, err
	}
	max, err := number("BRRP_VOICE_PORT_MAX", 50100, min, 65535)
	if err != nil {
		return c, err
	}
	c.VoicePortMin, c.VoicePortMax = uint16(min), uint16(max)
	if c.VoiceExternalIP != "" && net.ParseIP(c.VoiceExternalIP) == nil {
		return c, errors.New("BRRP_VOICE_EXTERNAL_IP must be an IP address")
	}
	for _, raw := range strings.Split(os.Getenv("BRRP_TRUSTED_PROXIES"), ",") {
		if strings.TrimSpace(raw) != "" {
			p, e := netip.ParsePrefix(strings.TrimSpace(raw))
			if e != nil {
				return c, fmt.Errorf("invalid trusted proxy: %w", e)
			}
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
	}
	if c.BehindProxy && len(c.TrustedProxies) == 0 {
		return c, errors.New("BRRP_BEHIND_PROXY requires BRRP_TRUSTED_PROXIES")
	}
	for _, p := range []struct {
		dst           *string
		key, fallback string
	}{{&c.DBFile, "BRRP_DB_FILE", "brrp.db"}, {&c.UploadDir, "BRRP_UPLOAD_DIR", "uploads"}, {&c.SetupTokenFile, "BRRP_SETUP_TOKEN_FILE", "setup-token"}, {&c.CertFile, "BRRP_CERT_FILE", "certs/tls.crt"}, {&c.KeyFile, "BRRP_KEY_FILE", "certs/tls.key"}, {&c.LogFile, "BRRP_LOG_FILE", ""}} {
		raw := env(p.key, p.fallback)
		if raw == "" {
			continue
		}
		if *p.dst, err = Contained(c.DataDir, raw); err != nil {
			return c, fmt.Errorf("%s: %w", p.key, err)
		}
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return c, errors.New("BRRP_BASE_URL must be an http(s) origin without path, query or credentials")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, errors.New("BRRP_ADDR must be host:port")
	}
	switch c.TLSMode {
	case "http-local":
		host, port, _ := net.SplitHostPort(c.Addr)
		ip := net.ParseIP(host)
		urlIP := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() || !ip.Equal(urlIP) || u.Scheme != "http" || u.Port() != port || c.BehindProxy {
			return c, errors.New("http-local requires a loopback IP listener and matching http:// loopback URL and port, without a proxy")
		}
	case "auto", "https":
		if u.Scheme != "https" {
			return c, errors.New("TLS mode requires an https BRRP_BASE_URL")
		}
	case "http-behind-proxy":
		if u.Scheme == "http" && !c.AllowInsecure {
			return c, errors.New("HTTP requires explicit BRRP_ALLOW_INSECURE_HTTP=true; use only for localhost development")
		}
		if !c.BehindProxy && !c.AllowInsecure {
			return c, errors.New("proxy mode requires BRRP_BEHIND_PROXY and explicit trusted proxies")
		}
	default:
		return c, errors.New("invalid BRRP_TLS_MODE")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error", "off":
	default:
		return c, errors.New("invalid BRRP_LOG_LEVEL")
	}
	return c, nil
}

// Contained rejects traversal and symlink escapes, including existing ancestors.
func Contained(root, name string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path must be inside the data directory")
	}
	for q := p; ; q = filepath.Dir(q) {
		if s, e := os.Lstat(q); e == nil && s.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("data paths must not contain symlinks")
		} else if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if q == filepath.Dir(q) {
			break
		}
	}
	return p, nil
}

func (c Config) Prepare() error {
	for _, p := range []string{c.DataDir, c.UploadDir, filepath.Dir(c.DBFile), filepath.Dir(c.SetupTokenFile), filepath.Dir(c.CertFile), filepath.Dir(c.KeyFile), filepath.Join(c.DataDir, "logs"), filepath.Join(c.DataDir, "backups")} {
		if _, err := Contained(c.DataDir, p); err != nil {
			return err
		}
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	return nil
}
func (c Config) SecureCookie() bool { return strings.HasPrefix(c.BaseURL, "https://") }

func resolveRoot(path string) (string, error) {
	parent := path
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			relative, err := filepath.Rel(parent, path)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, relative), nil
		}
		if !os.IsNotExist(err) || parent == filepath.Dir(parent) {
			return "", err
		}
		parent = filepath.Dir(parent)
	}
}
func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func boolean(k string) (bool, error) {
	v := os.Getenv(k)
	if v == "" {
		return false, nil
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return false, fmt.Errorf("%s must be true or false", k)
	}
	return b, nil
}
func number(k string, fallback, min, max int) (int, error) {
	v := env(k, strconv.Itoa(fallback))
	n, e := strconv.Atoi(v)
	if e != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be %d–%d", k, min, max)
	}
	return n, nil
}
