//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
)

func runPlatform(ctx context.Context) error { return run(ctx, os.Args[1:]) }
func manageService(command, config string) error {
	return errors.New("use the supplied systemd or launchd installation script on this operating system")
}
