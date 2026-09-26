package app

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func probeConfig(t *testing.T, server *httptest.Server) Config {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return Config{Addr: parsed.Host, BaseURL: server.URL, TLSMode: "http-local", DataDir: t.TempDir()}
}

func TestHealthCheckHTTP(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"setup", `{"setup_required":true}`, 200, true},
		{"configured", `{"setup_required":false}`, 200, true},
		{"failed", `{"error":"unavailable"}`, 503, false},
		{"not-json", `<html>proxy error</html>`, 200, false},
		{"missing-status", `{}`, 200, false},
		{"redirect", ``, 302, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/setup/status" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if test.status == 302 {
					w.Header().Set("Location", "/should-not-follow")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			c := probeConfig(t, server)
			// Proxy mode still probes the internal HTTP listener, not the public hostname.
			c.TLSMode = "http-behind-proxy"
			c.BaseURL = "https://public.example.invalid"
			if err := CheckHealth(context.Background(), c); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
}

func TestHealthCheckTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"setup_required":false}`)) }))
	defer server.Close()
	c := probeConfig(t, server)
	c.TLSMode = "https"
	if err := CheckHealth(context.Background(), c); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	c.CertFile = filepath.Join(c.DataDir, "server.crt")
	if err := os.WriteFile(c.CertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckHealth(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.BaseURL = "https://wrong.example.invalid"
	if err := CheckHealth(context.Background(), c); err == nil {
		t.Fatal("accepted wrong certificate hostname")
	}
}

func TestHealthCheckCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := CheckHealth(ctx, probeConfig(t, server)); err == nil {
		t.Fatal("ignored cancellation")
	}
}
