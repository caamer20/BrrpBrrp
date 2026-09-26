package installer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func nativeLegacyPaths(paths Paths) []Paths {
	return []Paths{{InstallDir: paths.InstallDir, Binary: paths.Binary, DataDir: paths.DataDir, Config: filepath.Join(paths.InstallDir, "server.env")}}
}

func nativeInstalledService(ctx context.Context) (*installedService, error) {
	file := agentFile()
	info, err := regularFile(file)
	if err != nil || info == nil {
		return nil, err
	}
	data, err := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", file).Output()
	if err != nil {
		return nil, err
	}
	var agent struct {
		Label            string
		ProgramArguments []string
	}
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, err
	}
	args := agent.ProgramArguments
	if agent.Label != "co.brrpbrrp.server" || len(args) != 4 || args[1] != "serve" || args[2] != "--config" {
		return nil, errors.New("The launch agent does not use the expected BrrpBrrp command. Inspect it before uninstalling.")
	}
	return &installedService{Binary: args[0], Config: args[3], Definition: file, Signature: string(data)}, nil
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
		return errors.New("The launch agent changed. Review it before uninstalling.")
	}
	label := "gui/" + strconv.Itoa(os.Getuid()) + "/co.brrpbrrp.server"
	if exec.CommandContext(ctx, "launchctl", "print", label).Run() == nil {
		if err := command(ctx, "launchctl", "bootout", label); err != nil {
			return err
		}
	}
	return os.Remove(agentFile())
}
