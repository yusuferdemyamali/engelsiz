package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type AppDirs struct {
	ConfigRoot  string
	DataRoot    string
	CacheRoot   string
	ConfigDir   string
	LogDir      string
	DownloadDir string
	ContentDir  string
	RuntimeDir  string
	CacheDir    string
	DBPath      string
	LogPath     string
}

func Resolve() (AppDirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return AppDirs{}, fmt.Errorf("resolve user home: %w", err)
	}
	return ResolveFor(runtime.GOOS, os.Getenv, home)
}

func ResolveFor(osName string, getenv func(string) string, home string) (AppDirs, error) {
	var d AppDirs
	if !absoluteFor(osName, home) {
		return d, fmt.Errorf("user home must be absolute: %q", home)
	}
	if osName == "windows" {
		appData := getenv("APPDATA")
		if !absoluteFor(osName, appData) {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		localAppData := getenv("LOCALAPPDATA")
		if !absoluteFor(osName, localAppData) {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		configRoot := filepath.Join(appData, "Engelsiz")
		dataRoot := filepath.Join(localAppData, "Engelsiz")
		d.ConfigRoot = configRoot
		d.DataRoot = dataRoot
		d.CacheRoot = dataRoot
		d.ConfigDir = filepath.Join(configRoot, "config")
		d.LogDir = filepath.Join(configRoot, "logs")
		d.DownloadDir = filepath.Join(dataRoot, "downloads")
		d.ContentDir = filepath.Join(dataRoot, "content")
		d.RuntimeDir = filepath.Join(dataRoot, "runtime")
		d.CacheDir = filepath.Join(dataRoot, "cache")
		d.DBPath = filepath.Join(dataRoot, "engelsiz.db")
		d.LogPath = filepath.Join(d.LogDir, "engelsiz.log")
		return d, nil
	}
	configHome := getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	dataHome := getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		dataHome = filepath.Join(home, ".local", "share")
	}
	cacheHome := getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(cacheHome) {
		cacheHome = filepath.Join(home, ".cache")
	}
	configRoot := filepath.Join(configHome, "engelsiz")
	dataRoot := filepath.Join(dataHome, "engelsiz")
	cacheRoot := filepath.Join(cacheHome, "engelsiz")
	d.ConfigRoot = configRoot
	d.DataRoot = dataRoot
	d.CacheRoot = cacheRoot
	d.ConfigDir = filepath.Join(configRoot, "config")
	d.LogDir = filepath.Join(dataRoot, "logs")
	d.DownloadDir = filepath.Join(dataRoot, "downloads")
	d.ContentDir = filepath.Join(dataRoot, "content")
	d.RuntimeDir = filepath.Join(dataRoot, "runtime")
	d.CacheDir = filepath.Join(cacheRoot, "cache")
	d.DBPath = filepath.Join(dataRoot, "engelsiz.db")
	d.LogPath = filepath.Join(d.LogDir, "engelsiz.log")
	return d, nil
}

func (d AppDirs) Ensure() error {
	for _, dir := range []string{
		d.ConfigDir,
		d.LogDir,
		d.DownloadDir,
		d.ContentDir,
		d.RuntimeDir,
		d.CacheDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(d.DBPath), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Windows paths are also accepted by fake-environment tests running on Linux.
func absoluteFor(osName, path string) bool {
	if osName == "windows" {
		return filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) ||
			(len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/'))
	}
	return filepath.IsAbs(path)
}
