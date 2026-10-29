package download

import (
	"errors"
	"golang.org/x/sys/unix"
)

func diskAvailable(path string) (uint64, error) {
	var s unix.Statfs_t
	if e := unix.Statfs(path, &s); e != nil {
		return 0, e
	}
	return s.Bavail * uint64(s.Bsize), nil
}

func diskFull(err error) bool { return errors.Is(err, unix.ENOSPC) }
