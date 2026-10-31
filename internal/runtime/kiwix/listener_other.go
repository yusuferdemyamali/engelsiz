//go:build !linux && !windows

package kiwix

import "errors"

func childOwnsListener(pid, port int) (bool, error) {
	return false, errors.New("child listener ownership is unsupported on this platform")
}
