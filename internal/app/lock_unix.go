//go:build !windows

package app

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func LockData(config Config) (*os.File, error) {
	path, err := Contained(config.DataDir, filepath.Join(config.DataDir, "server.lock"))
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("data folder is in use; stop the server before running this command: %w", err)
	}
	return file, nil
}
