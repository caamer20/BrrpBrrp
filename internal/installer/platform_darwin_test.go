package installer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestLaunchAgentPreservesPaths(t *testing.T) {
	p := Plan{Paths: Paths{Binary: "/Users/Pat & Jo/BrrpBrrp", Config: "/Users/Pat & Jo's/data/server.env", DataDir: "/Users/Pat & Jo's/data"}}
	file := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(file, launchAgent(p), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command("plutil", "-convert", "json", "-o", "-", file).Output()
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		ProgramArguments []string
		WorkingDirectory string
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(value.ProgramArguments, []string{p.Paths.Binary, "serve", "--config", p.Paths.Config}) || value.WorkingDirectory != p.Paths.DataDir {
		t.Fatalf("launchd paths changed: %+v", value)
	}
}
