package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brrpbrrp/internal/app"
)

// The transport-security header is the one header that can make a deployment a browser
// cannot reach: a pin for a name whose certificate is later replaced with a locally
// issued one is not something a mode change takes back. The decision is therefore made
// per deployment mode rather than whenever the base URL happens to be https, and this
// test is the whole matrix of what a browser may be told.
func TestTransportSecurityHeaderFollowsTheDeploymentMode(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cases := []struct {
		name    string
		mode    string
		baseURL string
		want    bool
	}{
		{"an operator-supplied or ACME certificate", "https", "https://community.example", true},
		{"a trusted proxy terminating TLS", "http-behind-proxy", "https://community.example", true},
		// The mode a device is taught to trust by hand while a name and a certificate are
		// still being sorted out. A pin here can outlive the reason for it.
		{"the per-installation local authority", "auto", "https://localhost:8080", false},
		// A proxy during local development, where the browser-facing origin is plain HTTP.
		{"a plain-HTTP proxy", "http-behind-proxy", "http://127.0.0.1:8080", false},
		{"plain HTTP on the server itself", "http-local", "http://127.0.0.1:8080", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			s := &Server{config: app.Config{TLSMode: testCase.mode, BaseURL: testCase.baseURL}}
			recorder := httptest.NewRecorder()
			s.middleware(handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.baseURL+"/", nil))
			value := recorder.Header().Get("Strict-Transport-Security")
			if testCase.want && value == "" {
				t.Fatal("a real HTTPS deployment sent no Strict-Transport-Security")
			}
			if !testCase.want && value != "" {
				t.Fatalf("a deployment that must not pin its host sent %q", value)
			}
			// The rest of the headers travel in every mode: this change adds one and takes
			// nothing away.
			for header, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"Referrer-Policy":        "no-referrer",
				"X-Frame-Options":        "DENY",
			} {
				if got := recorder.Header().Get(header); got != want {
					t.Fatalf("%s = %q, want %q", header, got, want)
				}
			}
			if recorder.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("no content security policy on a response")
			}
		})
	}
	// The value itself: six months, and nothing that widens the scope or commits the name
	// to a browser vendor's list.
	s := &Server{config: app.Config{TLSMode: "https", BaseURL: "https://community.example"}}
	recorder := httptest.NewRecorder()
	s.middleware(handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://community.example/", nil))
	value := recorder.Header().Get("Strict-Transport-Security")
	if value != "max-age=15552000" {
		t.Fatalf("Strict-Transport-Security = %q, want max-age=15552000", value)
	}
	if strings.Contains(value, "includeSubDomains") || strings.Contains(value, "preload") {
		t.Fatalf("the pin claims a scope this server does not own: %q", value)
	}
}
