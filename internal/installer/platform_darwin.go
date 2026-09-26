package installer

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultPaths() Paths {
	home, _ := os.UserHomeDir()
	installDir := filepath.Join(home, "Library", "Application Support", "BrrpBrrp")
	dataDir := filepath.Join(installDir, "data")
	return Paths{InstallDir: installDir, Binary: filepath.Join(installDir, "brrpbrrp"), DataDir: dataDir, Config: filepath.Join(dataDir, "server.env")}
}
func nativeCheck() error {
	if os.Geteuid() == 0 {
		return errors.New("Run setup as your normal macOS user, without sudo. The server starts when that user signs in.")
	}
	return nil
}
func Elevate() (bool, error) { return false, nil }
func nativeSecure(_ context.Context, p Plan) error {
	if err := os.Chmod(p.Paths.InstallDir, 0700); err != nil {
		return err
	}
	return os.Chmod(p.Paths.DataDir, 0700)
}
func nativeOwnFiles(context.Context, Plan) error { return nil }
func agentFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "co.brrpbrrp.server.plist")
}
func nativeRegister(ctx context.Context, p Plan) error {
	file := agentFile()
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	if err := ensureServiceFile(file, launchAgent(p)); err != nil {
		return err
	}
	return command(ctx, "plutil", "-lint", file)
}
func launchAgent(p Plan) []byte {
	escape := func(value string) string {
		var out strings.Builder
		_ = xml.EscapeText(&out, []byte(value))
		return out.String()
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>Label</key><string>co.brrpbrrp.server</string>
<key>ProgramArguments</key><array><string>` + escape(p.Paths.Binary) + `</string><string>serve</string><string>--config</string><string>` + escape(p.Paths.Config) + `</string></array>
<key>WorkingDirectory</key><string>` + escape(p.Paths.DataDir) + `</string>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>5</integer>
<key>ExitTimeOut</key><integer>25</integer>
</dict></plist>
`)
}
func nativeStart(ctx context.Context, p Plan) error {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if exec.CommandContext(ctx, "launchctl", "print", domain+"/co.brrpbrrp.server").Run() == nil {
		return command(ctx, "launchctl", "kickstart", domain+"/co.brrpbrrp.server")
	}
	return command(ctx, "launchctl", "bootstrap", domain, agentFile())
}
func openBrowser(address string) error { return exec.Command("open", address).Run() }

func nativeStop(ctx context.Context, p Plan) error {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if exec.CommandContext(ctx, "launchctl", "print", domain+"/co.brrpbrrp.server").Run() != nil {
		return nil
	}
	return command(ctx, "launchctl", "bootout", domain+"/co.brrpbrrp.server")
}
