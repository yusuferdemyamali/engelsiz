package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain re-executes the test binary as the fake translation worker when
// requested via environment. The worker child is the test binary itself, so
// no shebang script or external interpreter is needed and the fixture runs
// unmodified on Windows.
func TestMain(m *testing.M) {
	if os.Getenv("ENGELSIZ_TEST_API_TRANSLATION_WORKER") == "1" {
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
				_, _ = fmt.Fprintln(os.Stdout, `{"error":{"code":"bad_request","message":"bad"}}`)
				continue
			}
			raw, _ := json.Marshal(map[string]string{"translated_text": "X:" + req.Text})
			_, _ = fmt.Fprintln(os.Stdout, string(raw))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// translationWorker returns the current test binary as a directly executable
// worker. The binary re-executes itself as the worker child via TestMain,
// which is portable (Windows has no shebang execution) and needs no toolchain
// at test time beyond the already-built test binary.
func translationWorker(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path unavailable, cannot spawn fake worker: %v", err)
	}
	t.Setenv("ENGELSIZ_TEST_API_TRANSLATION_WORKER", "1")
	return exe
}
