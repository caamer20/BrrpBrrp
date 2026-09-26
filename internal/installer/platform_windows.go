package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func defaultPaths() Paths {
	installDir := filepath.Join(os.Getenv("ProgramFiles"), "BrrpBrrp")
	dataDir := filepath.Join(os.Getenv("ProgramData"), "BrrpBrrp")
	return Paths{InstallDir: installDir, Binary: filepath.Join(installDir, "brrpbrrp.exe"), DataDir: dataDir, Config: filepath.Join(dataDir, "server.env")}
}

func nativeCheck() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("Close setup and choose Run as administrator on BrrpBrrp.exe.")
	}
	return nil
}

// Elevate uses the normal Windows consent prompt. It does not alter script policy.
func Elevate() (bool, error) {
	if nativeCheck() == nil {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	arguments := append([]string(nil), os.Args[1:]...)
	if len(arguments) == 0 {
		arguments = []string{"install"}
	}
	for i := range arguments {
		arguments[i] = syscall.EscapeArg(arguments[i])
	}
	args, _ := windows.UTF16PtrFromString(strings.Join(arguments, " "))
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return true, windows.ShellExecute(0, verb, file, args, dir, windows.SW_SHOWNORMAL)
}

func nativeSecure(ctx context.Context, p Plan) error {
	return command(ctx, "icacls.exe", p.Paths.DataDir, "/inheritance:r", "/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-19:(OI)(CI)M")
}
func nativeOwnFiles(context.Context, Plan) error { return nil }
func nativeRegister(ctx context.Context, p Plan) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService("BrrpBrrp")
	if err == nil {
		defer s.Close()
		config, err := s.Config()
		if err != nil {
			return err
		}
		args, err := windows.DecomposeCommandLine(config.BinaryPathName)
		if err != nil || !slices.Equal(args, []string{p.Paths.Binary, "serve", "--config", p.Paths.Config}) {
			return errors.New("A different BrrpBrrp service is already registered. It has not been changed.")
		}
		return nil
	}
	if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return err
	}
	return command(ctx, p.Paths.Binary, "service-install", "--config", p.Paths.Config)
}
func nativeStart(ctx context.Context, p Plan) error {
	if p.Settings.AllowFirewall && p.Settings.Mode != "computer" {
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
		script := "$ErrorActionPreference='Stop'; "
		if p.Settings.Mode == "network" {
			script += "if (-not (Get-NetFirewallRule -Name 'BrrpBrrp-Local-Web' -ErrorAction SilentlyContinue)) { New-NetFirewallRule -Name 'BrrpBrrp-Local-Web' -DisplayName 'BrrpBrrp local web' -Direction Inbound -Action Allow -Protocol TCP -LocalPort " + strconv.Itoa(p.Settings.Port) + " -RemoteAddress LocalSubnet -Profile Private,Domain -Program " + quote(p.Paths.Binary) + " | Out-Null }; "
		}
		script += "if (-not (Get-NetFirewallRule -Name 'BrrpBrrp-Local-Voice' -ErrorAction SilentlyContinue)) { New-NetFirewallRule -Name 'BrrpBrrp-Local-Voice' -DisplayName 'BrrpBrrp local voice' -Direction Inbound -Action Allow -Protocol UDP -LocalPort '50000-50100' -RemoteAddress LocalSubnet -Profile Private,Domain -Program " + quote(p.Paths.Binary) + " | Out-Null }"
		if err := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
			return err
		}
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService("BrrpBrrp")
	if err != nil {
		return err
	}
	defer s.Close()
	err = s.Start()
	if errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return nil
	}
	return err
}
func openBrowser(address string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	url, _ := windows.UTF16PtrFromString(address)
	return windows.ShellExecute(0, verb, url, nil, nil, windows.SW_SHOWNORMAL)
}
