package installer

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"brrpbrrp/internal/app"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Paths{InstallDir: filepath.Join(root, "app"), Binary: filepath.Join(root, "app", "brrpbrrp"), DataDir: filepath.Join(root, "community's data #1"), Config: filepath.Join(root, "community's data #1", "server.env")}
}

func TestConnectionProfilesProduceWorkingConfiguration(t *testing.T) {
	for _, mode := range []string{"computer", "network", "proxy"} {
		t.Run(mode, func(t *testing.T) {
			paths := testPaths(t)
			p, err := makePlan(Settings{Mode: mode, Port: 8082, DataDir: paths.DataDir, NetworkHost: "192.168.50.20", PublicURL: "https://chat.example.com", TrustedProxies: "127.0.0.1/32,::1/128"}, paths, []string{"192.168.50.20"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(p.configText(), "\n") {
				key, _, found := strings.Cut(line, "=")
				if found {
					t.Setenv(key, "")
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Setenv("BRRP_ALLOW_INSECURE_HTTP", "false")
			if mode != "proxy" {
				t.Setenv("BRRP_BEHIND_PROXY", "false")
			}
			if err := os.WriteFile(paths.Config, []byte(p.configText()), 0600); err != nil {
				t.Fatal(err)
			}
			if err := app.LoadEnvFile(paths.Config); err != nil {
				t.Fatal(err)
			}
			config, err := app.LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if config.DataDir != paths.DataDir || config.Addr != p.Address || config.BaseURL != p.URL {
				t.Fatalf("configuration did not preserve the chosen paths and address: %+v", config)
			}
			if (mode == "computer") == config.SecureCookie() {
				t.Fatal("cookie transport does not match the connection profile")
			}
		})
	}
}

func TestRejectUnsafeInstallSettings(t *testing.T) {
	paths := testPaths(t)
	for _, mutate := range []func(*Settings){
		func(s *Settings) { s.Port = 80 },
		func(s *Settings) { s.DataDir = "relative" },
		func(s *Settings) { s.DataDir += "\nBRRP_ADDR=:80" },
		func(s *Settings) { s.Mode = "network"; s.NetworkHost = "8.8.8.8" },
		func(s *Settings) { s.Mode = "network"; s.NetworkHost = "192.168.50.21" },
		func(s *Settings) { s.Mode = "proxy"; s.PublicURL = "http://chat.example.com" },
		func(s *Settings) { s.Mode = "proxy"; s.PublicURL = "https://chat.example.com/path" },
		func(s *Settings) { s.Mode = "proxy"; s.TrustedProxies = "everyone" },
		func(s *Settings) { s.Mode = "proxy"; s.VoiceIP = "not-an-ip" },
	} {
		input := Settings{Mode: "computer", Port: 8082, DataDir: paths.DataDir, PublicURL: "https://chat.example.com", TrustedProxies: "127.0.0.1/32"}
		mutate(&input)
		if _, err := makePlan(input, paths, []string{"192.168.50.20"}); err == nil {
			t.Fatalf("accepted unsafe settings: %+v", input)
		}
	}
}

func TestDestinationChecksPreserveExistingFilesAndRejectBusyPort(t *testing.T) {
	paths := testPaths(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	p, err := makePlan(Settings{Mode: "computer", Port: listener.Addr().(*net.TCPAddr).Port, DataDir: paths.DataDir}, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(&p); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("busy port: %v", err)
	}
	listener.Close()
	if err := checkDestination(&p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(paths.DataDir, "brrp.db")
	if err := os.WriteFile(file, []byte("existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkDestination(&p); err == nil {
		t.Fatal("accepted existing database")
	}
	contents, _ := os.ReadFile(file)
	if string(contents) != "existing data" {
		t.Fatal("preflight changed the existing database")
	}
}
