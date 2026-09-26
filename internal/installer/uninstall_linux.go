package installer

import (
	"context"
	"errors"
	"os"
	"strings"
)

const systemServiceFile = "/etc/systemd/system/brrpbrrp.service"

func nativeLegacyPaths(paths Paths) []Paths {
	return []Paths{{InstallDir: "/usr/local/bin", Binary: "/usr/local/bin/brrpbrrp", DataDir: paths.DataDir, Config: "/etc/brrpbrrp/server.env"}}
}

func nativeInstalledService(context.Context) (*installedService, error) {
	info, err := regularFile(systemServiceFile)
	if err != nil || info == nil {
		return nil, err
	}
	data, err := os.ReadFile(systemServiceFile)
	if err != nil {
		return nil, err
	}
	var command string
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart="); ok {
			if command != "" {
				return nil, errors.New("The BrrpBrrp unit has multiple commands. Inspect it before uninstalling.")
			}
			command = value
		}
	}
	args, err := serviceCommand(command)
	if err != nil {
		return nil, err
	}
	return &installedService{Binary: args[0], Config: args[3], Definition: systemServiceFile, Signature: string(data)}, nil
}

func nativeUninstall(ctx context.Context, p RemovalPlan) error {
	if p.service == nil {
		return nil
	}
	current, err := nativeInstalledService(ctx)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.Signature != p.service.Signature {
		return errors.New("The service changed. Review it again before uninstalling.")
	}
	if err := command(ctx, "systemctl", "stop", "brrpbrrp"); err != nil {
		return err
	}
	if err := command(ctx, "systemctl", "disable", "brrpbrrp"); err != nil {
		return err
	}
	if err := os.Remove(systemServiceFile); err != nil {
		return err
	}
	return command(ctx, "systemctl", "daemon-reload")
}
