//go:build linux

package packages

import (
	"golang.org/x/sys/unix"
	"os"
)

// Directory handles are opened through os.Root; the kernel checks destination
// absence atomically, so a late collision cannot overwrite somebody else's file.
func renameNoReplace(source *os.Root, old string, target *os.Root, new string) error {
	from, e := source.Open(".")
	if e != nil {
		return e
	}
	defer from.Close()
	to, e := target.Open(".")
	if e != nil {
		return e
	}
	defer to.Close()
	return unix.Renameat2(int(from.Fd()), old, int(to.Fd()), new, unix.RENAME_NOREPLACE)
}
