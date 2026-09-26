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
	for _, pair := range [][2]string{{"BRRP_MAX_UPLOAD_BYTES", "999999999"}, {"BRRP_TLS_MODE", "invalid"}, {"BRRP_BASE_URL", "https://example.com/path"}, {"BRRP_BEHIND_PROXY", "true"}, {"BRRP_DB_FILE", "/outside.db"}} {
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
