//go:build !linux && !windows

package download

import (
	"errors"
	"syscall"
)

func diskAvailable(string) (uint64, error) { return 0, errors.New("unsupported capacity inspection") }

func diskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
