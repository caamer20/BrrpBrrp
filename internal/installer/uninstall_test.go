package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"brrpbrrp/internal/app"
)

func oldInstallation(t *testing.T, paths Paths) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		paths.Binary:                            []byte("previous BrrpBrrp executable"),
		paths.Config:                            []byte("BRRP_DATA_DIR=" + strconv.Quote(paths.DataDir) + "\nBRRP_ADDR=127.0.0.1:8082\n"),
		filepath.Join(paths.DataDir, "brrp.db"): []byte("existing database contents"),
		filepath.Join(paths.DataDir, "uploads", "attachment.txt"): []byte("private attachment contents"),
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestUninstallPreservesDataAndAllowsFreshInstallation(t *testing.T) {
	paths := testPaths(t)
	files := oldInstallation(t, paths)
	// A failed wizard may have left an incomplete resume record.
	metadata := filepath.Join(paths.InstallDir, "installation.json")
	if err := os.WriteFile(metadata, []byte("incomplete record"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRRP_DATA_DIR", "/an/unrelated/environment/path")
	p, err := inspectInstallation(paths, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Found || p.DataDir != paths.DataDir || p.NextDataDir == paths.DataDir {
		t.Fatalf("wrong removal plan: %+v", p)
	}
	if err := removeApplicationFiles(*p); err != nil {
		t.Fatal(err)
	}
	for path, before := range files {
		if path == paths.Binary {
			continue
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("uninstall changed %s: %v", path, err)
		}
	}
	for _, path := range []string{paths.Binary, metadata} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("application file remains: %s", path)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	fresh, err := makePlan(Settings{Mode: "computer", Port: port, DataDir: p.NextDataDir}, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(&fresh); err != nil {
		t.Fatalf("fresh install still conflicts: %v", err)
	}
}

func TestUninstallRefusesLockedDataOrLinkedExecutable(t *testing.T) {
	paths := testPaths(t)
	oldInstallation(t, paths)
	p, err := inspectInstallation(paths, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := app.LockData(app.Config{DataDir: paths.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := removeApplicationFiles(*p); err == nil {
		t.Fatal("removed a manually running server")
	}
	if _, err := os.Stat(paths.Binary); err != nil {
		t.Fatal("removed executable while data was locked")
	}
	if err := os.Remove(paths.Binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(paths.Config, paths.Binary); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := inspectInstallation(paths, nil, nil); err == nil {
		t.Fatal("accepted a linked application")
	}
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatal("changed symlink target")
	}
}

func TestLegacyServiceLocatesItsActualConfiguration(t *testing.T) {
	paths := testPaths(t)
	legacy := Paths{InstallDir: filepath.Join(filepath.Dir(paths.InstallDir), "legacy-app"), Binary: filepath.Join(filepath.Dir(paths.InstallDir), "legacy-app", "brrpbrrp"), DataDir: filepath.Join(filepath.Dir(paths.DataDir), "custom data"), Config: filepath.Join(filepath.Dir(paths.DataDir), "old settings", "server.env")}
	oldInstallation(t, legacy)
	service := &installedService{Binary: legacy.Binary, Config: legacy.Config, Signature: "registered command"}
	p, err := inspectInstallation(paths, []Paths{legacy}, service)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Service || p.DataDir != legacy.DataDir || p.Config != legacy.Config || !slices.Equal(p.Binaries, []string{legacy.Binary}) {
		t.Fatalf("legacy installation: %+v", p)
	}
	service.Binary = filepath.Join(filepath.Dir(paths.InstallDir), "unrelated-service")
	if _, err := inspectInstallation(paths, []Paths{legacy}, service); err == nil {
		t.Fatal("accepted unrelated service executable")
	}
}

func TestUninstallRequiresReviewConfirmationAndExclusiveAccess(t *testing.T) {
	w := testWizard(t)
	oldInstallation(t, w.paths)
	w.detect = func(_ context.Context, paths Paths) (*RemovalPlan, error) {
		return inspectInstallation(paths, nil, nil)
	}
	w.remove = func(_ context.Context, p RemovalPlan, _ progress) error { return removeApplicationFiles(p) }
	post := func(body any) int {
		out := httptest.NewRecorder()
		w.handler().ServeHTTP(out, wizardRequest(w, "POST", "/api/install/uninstall", body))
		return out.Code
	}
	if got := post(map[string]any{"id": "unreviewed", "confirmed": true}); got != 409 {
		t.Fatalf("unreviewed request: %d", got)
	}
	out := httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "GET", "/api/install/uninstall-plan", nil))
	var p RemovalPlan
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &p) != nil {
		t.Fatal(out.Body.String())
	}
	if got := post(map[string]any{"id": p.ID, "confirmed": false}); got != 409 {
		t.Fatalf("unconfirmed request: %d", got)
	}
	w.state.Phase = "installing"
	if got := post(map[string]any{"id": p.ID, "confirmed": true}); got != 409 {
		t.Fatalf("concurrent request: %d", got)
	}
	w.state.Phase = "welcome"
	w.privilege = func() error { return errors.New("administrator permission required") }
	if got := post(map[string]any{"id": p.ID, "confirmed": true}); got != 403 {
		t.Fatalf("unelevated request: %d", got)
	}
	w.privilege = func() error { return nil }
	if err := os.WriteFile(w.paths.Config, []byte("BRRP_DATA_DIR="+strconv.Quote(w.paths.DataDir)+"\n# changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := post(map[string]any{"id": p.ID, "confirmed": true}); got != 409 {
		t.Fatalf("outdated request: %d", got)
	}
	out = httptest.NewRecorder()
	w.handler().ServeHTTP(out, wizardRequest(w, "GET", "/api/install/uninstall-plan", nil))
	if err := json.Unmarshal(out.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if got := post(map[string]any{"id": p.ID, "confirmed": true}); got != 202 {
		t.Fatalf("confirmed request: %d", got)
	}
	deadline := time.After(5 * time.Second)
	for {
		w.mu.Lock()
		state, next := w.state, w.paths.DataDir
		w.mu.Unlock()
		if state.Phase == "removed" {
			if next != p.NextDataDir || state.Registered {
				t.Fatalf("fresh setup not ready: %+v", state)
			}
			break
		}
		if state.Phase == "remove_error" {
			t.Fatal(state.Error)
		}
		select {
		case <-deadline:
			t.Fatal("uninstall did not finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestServiceCommandRecognizesPackagedUnits(t *testing.T) {
	for _, line := range []string{
		`/usr/local/bin/brrpbrrp serve --config /etc/brrpbrrp/server.env`,
		`"/usr/local/lib/brrpbrrp/brrpbrrp" serve --config "/data/Pat's space $$20 100%%/server.env"`,
	} {
		args, err := serviceCommand(line)
		if err != nil {
			t.Fatal(err)
		}
		if len(args) != 4 || !strings.HasSuffix(args[3], "/server.env") {
			t.Fatalf("command parsed incorrectly: %v", args)
		}
	}
	for _, line := range []string{`/bin/sh -c unrelated`, `"unterminated`, `/app serve --config file --extra`} {
		if _, err := serviceCommand(line); err == nil {
			t.Fatalf("accepted unsupported command: %s", line)
		}
	}
}

func TestUninstallCannotRemoveTheRunningSetupExecutable(t *testing.T) {
	paths := testPaths(t)
	if err := os.MkdirAll(paths.InstallDir, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(executable, paths.Binary); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	p, err := inspectInstallation(paths, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Blocked == "" {
		t.Fatal("allowed setup to uninstall the executable it is running from")
	}
}
