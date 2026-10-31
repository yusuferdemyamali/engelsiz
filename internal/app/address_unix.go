//go:build !windows

package app

import (
	"errors"
	"syscall"
)

func addressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
