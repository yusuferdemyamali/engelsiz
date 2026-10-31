package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestResolveLinuxXDG(t *testing.T) {
	d, err := ResolveFor("linux", fakeEnv(map[string]string{
		"XDG_CONFIG_HOME": "/tmp/xdg/config",
		"XDG_DATA_HOME":   "/tmp/xdg/data",
		"XDG_CACHE_HOME":  "/tmp/xdg/cache",
	}), "/home/test")
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigDir != "/tmp/xdg/config/engelsiz/config" {
		t.Errorf("config dir = %q", d.ConfigDir)
	}
	if d.DBPath != "/tmp/xdg/data/engelsiz/engelsiz.db" {
		t.Errorf("db path = %q", d.DBPath)
	}
	if !strings.HasPrefix(d.CacheDir, "/tmp/xdg/cache/engelsiz") {
		t.Errorf("cache dir = %q", d.CacheDir)
	}
	for _, p := range []string{d.ConfigDir, d.LogDir, d.DownloadDir, d.ContentDir, d.RuntimeDir, d.DBPath, d.LogPath} {
		if strings.Contains(p, string(filepath.Separator)+"cmd"+string(filepath.Separator)) {
			t.Errorf("path escapes to source tree: %q", p)
		}
	}
}

func TestResolveLinuxFallback(t *testing.T) {
	d, err := ResolveFor("linux", fakeEnv(nil), "/home/test")
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigRoot != "/home/test/.config/engelsiz" {
		t.Errorf("config root = %q", d.ConfigRoot)
	}
	if d.DataRoot != "/home/test/.local/share/engelsiz" {
		t.Errorf("data root = %q", d.DataRoot)
	}
	if d.CacheRoot != "/home/test/.cache/engelsiz" {
		t.Errorf("cache root = %q", d.CacheRoot)
	}
}

func TestResolveWindows(t *testing.T) {
	d, err := ResolveFor("windows", fakeEnv(map[string]string{
		"APPDATA":      `C:\Users\T\AppData\Roaming`,
		"LOCALAPPDATA": `C:\Users\T\AppData\Local`,
	}), `C:\Users\T`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(d.ConfigDir, "Engelsiz"+string(filepath.Separator)+"config") {
		t.Errorf("config dir = %q", d.ConfigDir)
	}
	if !strings.HasSuffix(d.LogDir, "Engelsiz"+string(filepath.Separator)+"logs") {
		t.Errorf("log dir = %q", d.LogDir)
	}
	if filepath.Dir(d.DBPath) != d.DataRoot {
		t.Errorf("db should live under data root: %q vs %q", d.DBPath, d.DataRoot)
	}
}

func TestEnsureCreatesDirs(t *testing.T) {
	base := t.TempDir()
	d, err := ResolveFor("linux", fakeEnv(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(base, "config"),
		"XDG_DATA_HOME":   filepath.Join(base, "data"),
		"XDG_CACHE_HOME":  filepath.Join(base, "cache"),
	}), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{d.ConfigDir, d.LogDir, d.DownloadDir, d.ContentDir, d.RuntimeDir, d.CacheDir} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Errorf("missing dir %q: %v", dir, err)
		}
	}
}

func TestResolveWindowsFallback(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	d, err := ResolveFor("windows", fakeEnv(nil), home)
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigRoot != filepath.Join(home, "AppData", "Roaming", "Engelsiz") || d.DataRoot != filepath.Join(home, "AppData", "Local", "Engelsiz") {
		t.Fatalf("fallback: %+v", d)
	}
}

func TestRelativeRootsIgnoredAndMissingHomeFails(t *testing.T) {
	for _, osName := range []string{"linux", "windows"} {
		if _, err := ResolveFor(osName, fakeEnv(nil), ""); err == nil {
			t.Fatalf("%s missing home accepted", osName)
		}
		if _, err := ResolveFor(osName, fakeEnv(nil), "."); err == nil {
			t.Fatalf("%s relative home accepted", osName)
		}
		home := t.TempDir()
		d, err := ResolveFor(osName, fakeEnv(map[string]string{"APPDATA": "relative", "LOCALAPPDATA": "relative", "XDG_CONFIG_HOME": "relative", "XDG_DATA_HOME": "relative", "XDG_CACHE_HOME": "relative"}), home)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{d.ConfigDir, d.DBPath, d.CacheDir} {
			if !filepath.IsAbs(p) || !strings.HasPrefix(p, home+string(filepath.Separator)) {
				t.Fatalf("unsafe fallback %q", p)
			}
		}
	}
}
