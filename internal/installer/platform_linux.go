package installer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func defaultPaths() Paths {
	return Paths{InstallDir: "/usr/local/lib/brrpbrrp", Binary: "/usr/local/lib/brrpbrrp/brrpbrrp", DataDir: "/var/lib/brrpbrrp", Config: "/var/lib/brrpbrrp/server.env"}
}
func nativeCheck() error {
	if os.Geteuid() != 0 {
		return errors.New("Start setup with sudo ./brrpbrrp install to install the system service.")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("This installer needs systemd. Use the documented manual server command on other Linux systems.")
	}
	return nil
}
func Elevate() (bool, error) { return false, nil }
func nativeSecure(ctx context.Context, p Plan) error {
	if exec.CommandContext(ctx, "id", "brrpbrrp").Run() != nil {
		if err := command(ctx, "useradd", "--system", "--home-dir", p.Paths.DataDir, "--shell", "/usr/sbin/nologin", "brrpbrrp"); err != nil {
			return err
		}
	}
	return os.Chmod(p.Paths.DataDir, 0700)
}
func nativeOwnFiles(ctx context.Context, p Plan) error {
	return command(ctx, "chown", "-R", "brrpbrrp:brrpbrrp", p.Paths.DataDir)
}
func nativeRegister(ctx context.Context, p Plan) error {
	quote := func(value string) string {
		return strconv.Quote(strings.ReplaceAll(value, "%", "%%"))
	}
	execArg := func(value string) string { return quote(strings.ReplaceAll(value, "$", "$$")) }
	// Every configured path is absolute. WorkingDirectory uses different parsing
	// rules from ExecStart, so keep it independent of the selected data path.
	unit := "[Unit]\nDescription=BrrpBrrp community chat\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=brrpbrrp\nGroup=brrpbrrp\nWorkingDirectory=/\nExecStart=" + execArg(p.Paths.Binary) + " serve --config " + execArg(p.Paths.Config) + "\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=25\nUMask=0077\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\nReadWritePaths=" + quote(p.Paths.DataDir) + "\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\nLimitNOFILE=4096\n\n[Install]\nWantedBy=multi-user.target\n"
	if err := ensureServiceFile("/etc/systemd/system/brrpbrrp.service", []byte(unit)); err != nil {
		return err
	}
	if err := command(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	return command(ctx, "systemctl", "enable", "brrpbrrp")
}
func nativeStart(ctx context.Context, p Plan) error {
	return command(ctx, "systemctl", "start", "brrpbrrp")
}
func openBrowser(address string) error { return exec.Command("xdg-open", address).Run() }

func nativeStop(ctx context.Context, p Plan) error {
	return command(ctx, "systemctl", "stop", "brrpbrrp")
}
