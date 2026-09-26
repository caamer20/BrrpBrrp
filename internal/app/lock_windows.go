package app

import (
	"fmt"
	"golang.org/x/sys/windows"
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
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		file.Close()
		return nil, fmt.Errorf("data folder is in use; stop the server before running this command: %w", err)
	}
	return file, nil
}
