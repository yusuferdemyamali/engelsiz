//go:build !linux && !windows

package packages

import "os"

// Only Linux and Windows are supported; do not silently weaken atomicity.
func renameNoReplace(*os.Root, string, *os.Root, string) error { return os.ErrInvalid }
