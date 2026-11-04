package translation_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain re-executes the test binary as the fake translation worker when
// requested via environment. The worker child is the test binary itself, so
// no shebang script or external interpreter is needed and the fixture runs
// unmodified on Windows.
func TestMain(m *testing.M) {
	if os.Getenv("ENGELSIZ_TEST_TRANSLATION_WORKER") == "1" {
		runFakeWorker()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func writeWorkerLine(line string) {
	_, _ = fmt.Fprintln(os.Stdout, line)
}

func respondWorker(value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	writeWorkerLine(string(raw))
}

// runFakeWorker speaks the worker JSONL protocol. Behavior is driven by the
// request text markers used across the service tests.
func runFakeWorker() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 256*1024), 256*1024)
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req struct {
			Cmd  string `json:"cmd"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(line), &req) != nil || req.Cmd != "translate" {
			respondWorker(map[string]any{"error": map[string]string{"code": "bad_request", "message": "bad"}})
			continue
		}
		switch text := req.Text; {
		case text == "SLEEP" || text == "SLOW":
			time.Sleep(30 * time.Second)
			respondWorker(map[string]string{"translated_text": "late"})
		case text == "BADJSON":
			writeWorkerLine("this is not json")
		case text == "DOUBLE":
			first, _ := json.Marshal(map[string]string{"translated_text": "first"})
			second, _ := json.Marshal(map[string]string{"translated_text": "second"})
			writeWorkerLine(string(first) + " " + string(second))
		case text == "WORKERERROR":
			respondWorker(map[string]any{"error": map[string]string{"code": "engine_failed", "message": "boom"}})
		default:
			shown := text
			if rest, ok := strings.CutPrefix(text, "ECHO_STDERR:"); ok {
				_, _ = fmt.Fprintln(os.Stderr, text)
				shown = rest
			}
			time.Sleep(10 * time.Millisecond)
			respondWorker(map[string]string{"translated_text": "ÇEVİRİ:" + shown})
		}
	}
}

// fakeWorker returns the current test binary as a directly executable worker.
// The binary re-executes itself as the worker child via TestMain, which is
// portable (Windows has no shebang execution) and needs no toolchain at test
// time beyond the already-built test binary.
func fakeWorker(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path unavailable, cannot spawn fake worker: %v", err)
	}
	t.Setenv("ENGELSIZ_TEST_TRANSLATION_WORKER", "1")
	return exe
}
