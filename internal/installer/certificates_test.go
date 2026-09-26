package installer

import (
	"bytes"
	"context"
	"crypto/x509"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"brrpbrrp/internal/app"
)

func certWizard(t *testing.T) *wizard {
	t.Helper()
	w := testWizard(t)
	p := Plan{Paths: w.paths, Settings: Settings{Mode: "network"}, URL: "https://192.168.1.5:8082"}
	c := certificateConfig(p)
	c.BaseURL = p.URL
	if err := os.MkdirAll(filepath.Dir(c.CertFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := app.GenerateCert(c); err != nil {
		t.Fatal(err)
	}
	w.state = State{Plan: &p, Registered: true, Phase: "complete"}
	return w
}
func TestCertificateDownloadContainsOnlyPublicRoot(t *testing.T) {
	w := certWizard(t)
	out := httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "GET", "/api/install/certificate", nil))
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	cert, err := x509.ParseCertificate(out.Body.Bytes())
	if err != nil || !cert.IsCA {
		t.Fatal("download is not a DER root certificate", err)
	}
	if bytes.Contains(out.Body.Bytes(), []byte("PRIVATE KEY")) {
		t.Fatal("download leaked private key")
	}
	if out.Header().Get("Content-Disposition") != `attachment; filename="BrrpBrrp-local-ca.cer"` {
		t.Fatal(out.Header())
	}
}
func TestCertificateTrustRequiresConfirmationFingerprintAndIdleState(t *testing.T) {
	w := certWizard(t)
	_, cert, _ := app.TrustCertificate(certificateConfig(*w.state.Plan))
	fingerprint := app.CertificateFingerprint(cert)
	calls := 0
	w.trust = func(_ context.Context, der []byte) error {
		calls++
		if !bytes.Equal(der, cert.Raw) {
			t.Fatal("trusted different certificate")
		}
		return nil
	}
	for _, tc := range []struct {
		name, phase, fingerprint string
		confirm, preview         bool
		status                   int
	}{
		{"missing confirmation", "complete", fingerprint, false, false, 400},
		{"stale certificate", "complete", "different", true, false, 409},
		{"busy", "installing", fingerprint, true, false, 409},
		{"preview", "complete", fingerprint, true, true, 403},
		{"valid", "complete", fingerprint, true, false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.state.Phase = tc.phase
			w.preview = tc.preview
			out := httptest.NewRecorder()
			w.handler().ServeHTTP(out, wizardRequest(w, "POST", "/api/install/trust-certificate", map[string]any{"confirm": tc.confirm, "fingerprint": tc.fingerprint}))
			if out.Code != tc.status {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatal("unexpected trust mutations", calls)
	}
}
func TestCertificateMutationRejectsRemoteAndCrossOrigin(t *testing.T) {
	w := certWizard(t)
	w.trust = func(context.Context, []byte) error { t.Fatal("unauthorized trust"); return nil }
	for _, path := range []string{"trust-certificate", "repair-certificate", "update", "open-tray"} {
		for _, kind := range []string{"remote", "origin", "token"} {
			r := wizardRequest(w, "POST", "/api/install/"+path, map[string]bool{"confirm": true})
			switch kind {
			case "remote":
				r.RemoteAddr = "192.168.1.5:1234"
			case "origin":
				r.Header.Set("Origin", "https://other.invalid")
			case "token":
				r.Header.Del("Authorization")
			}
			out := httptest.NewRecorder()
			w.handler().ServeHTTP(out, r)
			if out.Code != 403 {
				t.Fatal(path, kind, out.Code)
			}
		}
	}
}
func TestUpdatePreservesPreviousExecutable(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "release")
	target := filepath.Join(directory, "installed")
	os.WriteFile(source, []byte("new executable"), 0755)
	os.WriteFile(target, []byte("old executable"), 0755)
	if err := replaceExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(target)
	if string(current) != "new executable" {
		t.Fatal(string(current))
	}
	previous, _ := filepath.Glob(filepath.Join(directory, "previous-build-*", "installed"))
	if len(previous) != 1 {
		t.Fatal(previous)
	}
	old, _ := os.ReadFile(previous[0])
	if string(old) != "old executable" {
		t.Fatal("old executable lost")
	}
}
