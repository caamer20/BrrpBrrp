package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"brrpbrrp/internal/api"
	"brrpbrrp/internal/app"
	"brrpbrrp/internal/db"
)

func testWizard(t *testing.T) *wizard {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &wizard{ctx: ctx, cancel: cancel, token: "a-private-test-token", host: "127.0.0.1:45678", origin: "http://127.0.0.1:45678", paths: testPaths(t), state: State{Phase: "welcome"}, privilege: func() error { return nil }}
}

func wizardRequest(w *wizard, method, path string, body any) *http.Request {
	var buffer bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buffer).Encode(body)
	}
	r := httptest.NewRequest(method, w.origin+path, &buffer)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Authorization", "Bearer "+w.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", w.origin)
	return r
}

func TestWizardRejectsRemoteAndCrossSiteAccess(t *testing.T) {
	w := testWizard(t)
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
	}{
		{"missing token", func(r *http.Request) { r.Header.Del("Authorization") }},
		{"wrong token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }},
		{"foreign origin", func(r *http.Request) { r.Header.Set("Origin", "https://example.com") }},
		{"missing mutation origin", func(r *http.Request) { r.Header.Del("Origin") }},
		{"DNS rebinding", func(r *http.Request) { r.Host = "attacker.example:45678" }},
		{"remote address", func(r *http.Request) { r.RemoteAddr = "192.168.1.20:50000" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := wizardRequest(w, "POST", "/api/install/check", Settings{})
			tc.change(r)
			out := httptest.NewRecorder()
			w.handler().ServeHTTP(out, r)
			if out.Code != 403 {
				t.Fatalf("unauthorized request returned %d", out.Code)
			}
		})
	}
	out := httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "GET", "/api/install/info", nil))
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("installer response could cache or leak its origin")
	}
}

func TestPreviewCannotMutateOrExposeFiles(t *testing.T) {
	w := testWizard(t)
	w.preview = true
	for _, path := range []string{"apply", "retry", "account", "uninstall"} {
		out := httptest.NewRecorder()
		w.handler().ServeHTTP(out, wizardRequest(w, "POST", "/api/install/"+path, map[string]string{}))
		if out.Code != 403 {
			t.Fatalf("preview allowed %s: %d", path, out.Code)
		}
	}
	for _, path := range []string{"/server.env", "/service-worker.js", "/api/install/certificate"} {
		out := httptest.NewRecorder()
		w.handler().ServeHTTP(out, wizardRequest(w, "GET", path, nil))
		if out.Code != 404 {
			t.Fatalf("unexpected file access for %s: %d", path, out.Code)
		}
	}
}

func TestWizardCreatesAccountThroughTheRunningServer(t *testing.T) {
	w := testWizard(t)
	t.Setenv("BRRP_DATA_DIR", w.paths.DataDir)
	t.Setenv("BRRP_BASE_URL", "http://127.0.0.1:8082")
	t.Setenv("BRRP_ADDR", "127.0.0.1:8082")
	t.Setenv("BRRP_TLS_MODE", "http-local")
	t.Setenv("BRRP_BEHIND_PROXY", "false")
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Prepare(); err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(config.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, err := api.New(config, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	local := httptest.NewServer(server.Handler())
	defer local.Close()
	address, _ := url.Parse(local.URL)
	p := Plan{Paths: w.paths, URL: config.BaseURL, Address: address.Host, Settings: Settings{Mode: "computer"}}
	required, err := waitReady(w.ctx, p)
	if err != nil || !required {
		t.Fatalf("first start: required=%v, err=%v", required, err)
	}
	w.state = State{Phase: "account", Plan: &p, Registered: true}
	input := map[string]string{"server_name": "Our place", "username": "owner", "password": "a private setup passphrase"}
	out := httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "POST", "/api/install/account", input))
	if out.Code != 201 {
		t.Fatalf("account setup returned %d: %s", out.Code, out.Body.String())
	}
	var name string
	if err := store.QueryRow("SELECT username FROM users WHERE is_admin=1").Scan(&name); err != nil || name != "owner" {
		t.Fatalf("administrator not persisted: %q %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(w.paths.DataDir, "setup-token")); !os.IsNotExist(err) {
		t.Fatal("setup token was not retired")
	}
	if w.state.Phase != "complete" {
		t.Fatalf("phase = %s", w.state.Phase)
	}
	out = httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "POST", "/api/install/account", input))
	if out.Code != 409 {
		t.Fatal("duplicate account creation was accepted")
	}
}

func TestExecutableCopyNeverOverwritesAnExistingFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("existing executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyExecutable(source, target); err == nil {
		t.Fatal("overwrote existing file")
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "existing executable" {
		t.Fatal("existing executable changed")
	}
}

func TestServiceDefinitionCanBeRetriedWithoutReplacingAnotherService(t *testing.T) {
	file := filepath.Join(t.TempDir(), "brrpbrrp.service")
	definition := []byte("the installed service definition")
	for range 2 {
		if err := ensureServiceFile(file, definition); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureServiceFile(file, []byte("a different installation")); err == nil {
		t.Fatal("replaced an unrelated service")
	}
	data, _ := os.ReadFile(file)
	if !bytes.Equal(data, definition) {
		t.Fatal("existing service changed")
	}
}

func TestInstallerClientTrustsOnlyItsServerCertificate(t *testing.T) {
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"setup_required":true}`)
	}))
	defer local.Close()
	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Join(paths.DataDir, "certs"), 0700); err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: local.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(paths.DataDir, "certs", "tls.crt"), cert, 0600); err != nil {
		t.Fatal(err)
	}
	p := Plan{Paths: paths, URL: local.URL, Address: strings.TrimPrefix(local.URL, "https://"), Settings: Settings{Mode: "network"}}
	client, address, err := clientFor(p)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	// Even a locally trusted certificate cannot authenticate the wrong hostname.
	p.URL = "https://another-server.invalid"
	client, address, err = clientFor(p)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if response, err := client.Get(address); err == nil {
		response.Body.Close()
		t.Fatal("accepted a certificate for another hostname")
	}
}

func TestFailedStartResumesWithoutReinstalling(t *testing.T) {
	w := testWizard(t)
	local := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(out, `{"setup_required":true}`)
	}))
	defer local.Close()
	p := Plan{Paths: w.paths, Address: strings.TrimPrefix(local.URL, "http://"), Settings: Settings{Mode: "computer"}}
	w.state = State{Phase: "installing", Plan: &p}
	installs, starts := 0, 0
	w.install = func(context.Context, Plan, progress) error { installs++; return nil }
	w.start = func(context.Context, Plan) error {
		starts++
		if starts == 1 {
			return errors.New("temporary service startup failure")
		}
		return nil
	}
	w.perform(p, false)
	if w.state.Phase != "error" || !w.state.Registered {
		t.Fatalf("failed start cannot be resumed: %+v", w.state)
	}
	w.perform(p, true)
	if installs != 1 || starts != 2 || w.state.Phase != "account" {
		t.Fatalf("retry: installs=%d starts=%d state=%+v", installs, starts, w.state)
	}
}

func TestResumeDoesNotUseOutdatedHostSettings(t *testing.T) {
	paths := testPaths(t)
	p, err := makePlan(Settings{Mode: "computer", Port: 8082, DataDir: paths.DataDir}, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.InstallDir, paths.DataDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	metadata, _ := json.Marshal(p)
	for path, data := range map[string][]byte{paths.Binary: []byte("binary"), paths.Config: []byte(p.configText()), filepath.Join(paths.InstallDir, "installation.json"): metadata} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if existingInstallation(paths) == nil {
		t.Fatal("did not recognize installation")
	}
	if err := os.WriteFile(paths.Config, []byte("manually changed configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if existingInstallation(paths) != nil {
		t.Fatal("resumed outdated configuration")
	}
}
