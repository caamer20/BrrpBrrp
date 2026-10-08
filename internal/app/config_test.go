package app

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigAndCertificate(t *testing.T) {
	t.Setenv("BRRP_DATA_DIR", filepath.Join(t.TempDir(), "new-data"))
	t.Setenv("BRRP_BASE_URL", "https://localhost:8080")
	c, e := LoadConfig()
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Prepare(); e != nil {
		t.Fatal(e)
	}
	if e = GenerateCert(c); e != nil {
		t.Fatal(e)
	}
	pair, e := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		if e := cert.VerifyHostname(h); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = Contained(c.DataDir, "../escape"); e == nil {
		t.Fatal("allowed path escape")
	}
	outside := t.TempDir()
	if os.Symlink(outside, filepath.Join(c.DataDir, "link")) == nil {
		if _, e = Contained(c.DataDir, "link/file"); e == nil {
			t.Fatal("allowed symlink escape")
		}
	}
	if e = GenerateCert(c); e == nil {
		t.Fatal("overwrote key")
	}
}

func TestRejectUnsafeConfig(t *testing.T) {
	outsideDB := filepath.Join(t.TempDir(), "outside.db")
	for _, pair := range [][2]string{{"BRRP_MAX_UPLOAD_BYTES", "999999999"}, {"BRRP_TLS_MODE", "invalid"}, {"BRRP_BASE_URL", "https://example.com/path"}, {"BRRP_BEHIND_PROXY", "true"}, {"BRRP_DB_FILE", outsideDB}} {
		t.Run(pair[0], func(t *testing.T) {
			t.Setenv("BRRP_DATA_DIR", t.TempDir())
			t.Setenv(pair[0], pair[1])
			if _, e := LoadConfig(); e == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}

func TestLocalHTTPStaysOnLoopback(t *testing.T) {
	for _, tc := range []struct {
		address, url string
		valid        bool
	}{
		{"127.0.0.1:8082", "http://127.0.0.1:8082", true},
		{"[::1]:8082", "http://[::1]:8082", true},
		{":8082", "http://127.0.0.1:8082", false},
		{"0.0.0.0:8082", "http://127.0.0.1:8082", false},
		{"127.0.0.1:8082", "http://192.168.1.20:8082", false},
		{"127.0.0.1:8082", "http://127.0.0.1:8083", false},
		{"127.0.0.1:8082", "http://127.0.0.2:8082", false},
	} {
		t.Run(tc.address+tc.url, func(t *testing.T) {
			t.Setenv("BRRP_DATA_DIR", t.TempDir())
			t.Setenv("BRRP_TLS_MODE", "http-local")
			t.Setenv("BRRP_ADDR", tc.address)
			t.Setenv("BRRP_BASE_URL", tc.url)
			t.Setenv("BRRP_BEHIND_PROXY", "false")
			t.Setenv("BRRP_ALLOW_INSECURE_HTTP", "false")
			_, err := LoadConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %v, error = %v", tc.valid, err)
			}
		})
	}
}

// Restricted channels and private conversations are on unless an operator switches one
// off (owner decision of 6 October 2026). The distinction this pins is between a variable
// nobody set — which takes the shipped default — and one an operator set, which is theirs:
// an installation that wrote `false` while the features shipped off must still be off
// after an upgrade, and a typo must be an error rather than a silent default.
func TestBothConversationFeaturesAreOnUnlessAnOperatorSwitchesOneOff(t *testing.T) {
	gates := []string{"BRRP_ENABLE_RESTRICTED_CHANNELS", "BRRP_ENABLE_PRIVATE_CONVERSATIONS"}
	load := func(t *testing.T, value string) (Config, error) {
		t.Helper()
		t.Setenv("BRRP_DATA_DIR", filepath.Join(t.TempDir(), "data"))
		t.Setenv("BRRP_BASE_URL", "https://localhost:8080")
		for _, gate := range gates {
			t.Setenv(gate, value)
		}
		return LoadConfig()
	}
	for _, testCase := range []struct {
		name  string
		value string
		want  bool
	}{
		// An empty variable is what Compose passes for a key .env.docker leaves blank, and
		// is indistinguishable from an unset one.
		{"a variable nobody set", "", true},
		{"an operator's explicit off", "false", false},
		{"an operator's explicit off, as a digit", "0", false},
		{"an explicit on", "true", true},
		{"an explicit on, as a digit", "1", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c, err := load(t, testCase.value)
			if err != nil {
				t.Fatal(err)
			}
			if c.RestrictedChannels != testCase.want || c.PrivateConversations != testCase.want {
				t.Fatalf("restricted channels %v, private conversations %v; want both %v", c.RestrictedChannels, c.PrivateConversations, testCase.want)
			}
		})
	}
	if _, err := load(t, "yes please"); err == nil {
		t.Fatal("a value that is neither true nor false was accepted")
	}
	// The other boolean settings are unchanged: unset still means off for them.
	for _, gate := range gates {
		t.Setenv(gate, "")
	}
	t.Setenv("BRRP_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("BRRP_BEHIND_PROXY", "")
	t.Setenv("BRRP_ALLOW_INSECURE_HTTP", "")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.BehindProxy || c.AllowInsecure {
		t.Fatal("a boolean setting other than the two features took a default of on")
	}
}
