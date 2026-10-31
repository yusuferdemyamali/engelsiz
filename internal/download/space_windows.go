package download

import (
	"errors"
	"golang.org/x/sys/windows"
	"syscall"
)

func diskAvailable(path string) (uint64, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return 0, e
	}
	var free, total, available uint64
	e = windows.GetDiskFreeSpaceEx(p, &available, &total, &free)
	return available, e
}

func diskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL) || errors.Is(err, syscall.ENOSPC)
}
