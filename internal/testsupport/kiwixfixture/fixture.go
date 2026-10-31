// Package kiwixfixture builds a real foreground HTTP subprocess for lifecycle tests.
package kiwixfixture

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Config is stored in the first positional ZIM file passed to the fixture.
// Durations use encoding/json's numeric time.Duration representation.
type Config struct {
	ReadyDelay    time.Duration `json:"ready_delay,omitempty"`
	StatusCode    int           `json:"status_code,omitempty"`
	Redirect      string        `json:"redirect,omitempty"`
	EarlyExit     bool          `json:"early_exit,omitempty"`
	ExitAfter     time.Duration `json:"exit_after,omitempty"`
	ExitCode      int           `json:"exit_code,omitempty"`
	StdoutBytes   int           `json:"stdout_bytes,omitempty"`
	StderrBytes   int           `json:"stderr_bytes,omitempty"`
	IgnoreSIGTERM bool          `json:"ignore_sigterm,omitempty"`
	MarkerPath    string        `json:"marker_path,omitempty"`
}

// Event records a fixture start or a voluntary exit. A killed process cannot
// record an exit; callers should also observe the actual process completion.
type Event struct {
	Kind    string    `json:"kind"`
	PID     int       `json:"pid"`
	Args    []string  `json:"args,omitempty"`
	Code    int       `json:"code"`
	Address string    `json:"address,omitempty"`
	At      time.Time `json:"at"`
}

//go:embed testdata/main.go
var source []byte

// Build compiles an independent helper executable in t's temporary directory.
func Build(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainFile, source, 0o600); err != nil {
		t.Fatal(err)
	}
	name := "kiwix fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goName := "go"
		if runtime.GOOS == "windows" {
			goName += ".exe"
		}
		goBinary = filepath.Join(runtime.GOROOT(), "bin", goName)
	}
	cmd := exec.Command(goBinary, "build", "-o", binary, mainFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build kiwix fixture: %v\n%s", err, output)
	}
	return binary
}

// WriteConfig writes a regular local file suitable as the first ZIM input.
func WriteConfig(t testing.TB, cfg Config) string {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture config.zim")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ReadEvents reads complete JSONL events, tolerating a not-yet-created file or
// the last write being in progress so callers can safely poll during startup.
func ReadEvents(t testing.TB, markerPath string) []Event {
	t.Helper()
	data, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	events := make([]Event, 0, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("read kiwix fixture marker: %v", err)
		}
		events = append(events, event)
	}
	return events
}
