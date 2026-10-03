//go:build windows

package app

import "golang.org/x/sys/windows"

// FreeSpace reports the bytes available to the calling user on the volume that holds
// path.
//
// GetDiskFreeSpaceEx rather than the plain total/free pair: the per-user figure is the
// one a quota or a reserved-blocks policy can reduce, and it is the space this process
// can actually write into.
func FreeSpace(path string) (uint64, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &available, &total, &totalFree); err != nil {
		return 0, err
	}
	return available, nil
}
