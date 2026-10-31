package kiwix

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func adjacentBinary(executable, platform string) string {
	name := "kiwix-serve"
	if platform == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(executable), "runtime", name)
}

func resolveBinary(override string, executable func() (string, error)) (string, error) {
	path := override
	if path == "" {
		exe, err := executable()
		if err != nil {
			return "", fmt.Errorf("locate application executable: %w", err)
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		path = adjacentBinary(exe, runtime.GOOS)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("kiwix executable %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("kiwix executable %q is not a regular file", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("kiwix executable %q is not executable", path)
	}
	return path, nil
}
