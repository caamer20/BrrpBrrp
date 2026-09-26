package installer

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func nativeLegacyPaths(Paths) []Paths { return nil }

func nativeInstalledService(context.Context) (*installedService, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	s, err := m.OpenService("BrrpBrrp")
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer s.Close()
	config, err := s.Config()
	if err != nil {
		return nil, err
	}
	args, err := windows.DecomposeCommandLine(config.BinaryPathName)
	if err != nil || len(args) != 4 || args[1] != "serve" || args[2] != "--config" {
		return nil, errors.New("The BrrpBrrp service command is not recognized. Inspect it in Windows Services before uninstalling.")
	}
	return &installedService{Binary: args[0], Config: args[3], Signature: config.BinaryPathName}, nil
}

func nativeUninstall(ctx context.Context, p RemovalPlan) error {
	if p.service != nil {
		current, err := nativeInstalledService(ctx)
		if err != nil {
			return err
		}
		if current != nil {
			if current.Signature != p.service.Signature {
				return errors.New("The service changed. Review its new settings before uninstalling.")
			}
			if err := removeWindowsService(ctx); err != nil {
				return err
			}
		}
	}
	// Remove only rules created by this installer and still pointing at its binary.
	for _, binary := range p.Binaries {
		if err := removeDesktop(ctx, binary); err != nil {
			return err
		}
		quoted := "'" + strings.ReplaceAll(binary, "'", "''") + "'"
		script := "$ErrorActionPreference='Stop'; foreach ($name in @('BrrpBrrp-Local-Web','BrrpBrrp-Local-Voice')) { $rule = Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue; if ($rule -and (($rule | Get-NetFirewallApplicationFilter).Program -eq " + quoted + ")) { $rule | Remove-NetFirewallRule } }"
		if err := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
			return err
		}
	}
	return nil
}

func removeWindowsService(ctx context.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService("BrrpBrrp")
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			s.Close()
		}
	}()
	status, err := s.Query()
	if err != nil {
		return err
	}
	if status.State != svc.Stopped && status.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
	}
	stop, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for status.State != svc.Stopped {
		select {
		case <-stop.Done():
			return errors.New("The service did not stop within 30 seconds. Check Windows Services, then retry. Application files and data have been kept.")
		case <-time.After(200 * time.Millisecond):
		}
		status, err = s.Query()
		if err != nil {
			return err
		}
	}
	if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	closed = true
	deleted, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	for {
		probe, err := m.OpenService("BrrpBrrp")
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		if err == nil {
			probe.Close()
		} else if !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			return err
		}
		select {
		case <-deleted.Done():
			return errors.New("Windows is still releasing the service. Close Windows Services and other BrrpBrrp windows, then retry.")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
