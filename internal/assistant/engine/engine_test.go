package engine_test

// Fixture tests run the engine against a fake llama.cpp server that is the
// test binary re-executed as a child process. This establishes a real child
// process lifecycle (spawn, readiness, streaming, termination, reaping)
// without downloading any model or runtime.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/assistant/engine"
	"engelsiz/internal/download"
)

const (
	fakeEnv  = "ENGELSIZ_ENGINE_FAKE_SERVER"
	modeEnv  = "ENGELSIZ_ENGINE_MODE"
	recordEv = "ENGELSIZ_ENGINE_RECORD"
	portEv   = "ENGELSIZ_ENGINE_PORT_FILE"
	promptEv = "ENGELSIZ_ENGINE_PROMPT_FILE"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		runFakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeEngine points the engine at the test binary itself as the server and
// programs its behavior through environment inherited by the child.
func fakeEngine(t *testing.T, mode string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("test binary path unavailable: %v", err)
	}
	t.Setenv(fakeEnv, "1")
	t.Setenv(modeEnv, mode)
	return exe
}

func modelFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte("fake-gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

func engineError(t *testing.T, err error) *download.Error {
	t.Helper()
	var pub *download.Error
	if !errors.As(err, &pub) {
		t.Fatalf("expected *download.Error, got %T (%v)", err, err)
	}
	return pub
}

func readArgvLines(t *testing.T, path string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var out [][]string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var argv []string
		if err := json.Unmarshal([]byte(line), &argv); err != nil {
			t.Fatalf("decode record line: %v", err)
		}
		out = append(out, argv)
	}
	return out
}

func argvValue(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

func hasConsecutive(argv []string, a, b string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == a && argv[i+1] == b {
			return true
		}
	}
	return false
}

func hasFlag(argv []string, flag string) bool {
	for _, a := range argv {
		if a == flag {
			return true
		}
	}
	return false
}

func collect(t *testing.T, pieces *[]string) func(string) error {
	t.Helper()
	var mu sync.Mutex
	return func(s string) error {
		mu.Lock()
		defer mu.Unlock()
		*pieces = append(*pieces, s)
		return nil
	}
}

func request(binary, model string) engine.Request {
	return engine.Request{
		Binary:      binary,
		ModelPath:   model,
		ContextSize: 2048,
		MaxTokens:   64,
		Messages:    []engine.Message{{Role: "user", Content: "selam"}},
	}
}

func TestGenerate_StartStreamSuccess(t *testing.T) {
	binary := fakeEngine(t, "")
	model := modelFile(t)
	record := filepath.Join(t.TempDir(), "record.jsonl")
	portFile := filepath.Join(t.TempDir(), "port")
	t.Setenv(recordEv, record)
	t.Setenv(portEv, portFile)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second})
	defer eng.Close()

	var pieces []string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := eng.Generate(ctx, request(binary, model), collect(t, &pieces)); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := strings.Join(pieces, ""); got != "merhaba" {
		t.Fatalf("deltas joined = %q, want %q (pieces=%q)", got, "merhaba", pieces)
	}

	runs := readArgvLines(t, record)
	if len(runs) != 1 {
		t.Fatalf("spawns = %d, want 1", len(runs))
	}
	argv := runs[0]
	for _, want := range []struct{ a, b string }{
		{"--host", "127.0.0.1"}, {"--parallel", "1"}, {"--reasoning", "off"}, {"-c", "2048"},
	} {
		if !hasConsecutive(argv, want.a, want.b) {
			t.Fatalf("argv missing %s %s: %q", want.a, want.b, argv)
		}
	}
	for _, flag := range []string{"--no-webui", "--device", "-ngl", "--port", "--api-key", "-m"} {
		if !hasFlag(argv, flag) {
			t.Fatalf("argv missing %s: %q", flag, argv)
		}
	}
	if v, _ := argvValue(argv, "--device"); v != "none" {
		t.Fatalf("--device = %q, want none", v)
	}
	if v, _ := argvValue(argv, "-ngl"); v != "0" {
		t.Fatalf("-ngl = %q, want 0", v)
	}
	port, ok := argvValue(argv, "--port")
	if !ok || port == "" || port == "0" {
		t.Fatalf("explicit random port missing: %q", argv)
	}
	key, ok := argvValue(argv, "--api-key")
	if !ok || len(key) < 32 {
		t.Fatalf("random api key missing: %q", argv)
	}

	// The child must be terminated and reaped before Generate returns: its
	// loopback listener is gone.
	rawPort, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("read port file: %v", err)
	}
	if strings.TrimSpace(string(rawPort)) != port {
		t.Fatalf("port file = %q, argv port = %q", strings.TrimSpace(string(rawPort)), port)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatalf("child listener on %s still accepts after Generate", port)
	}

	// A second request uses a fresh random port and key.
	var pieces2 []string
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	if err := eng.Generate(ctx2, request(binary, model), collect(t, &pieces2)); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	runs = readArgvLines(t, record)
	if len(runs) != 2 {
		t.Fatalf("spawns = %d, want 2", len(runs))
	}
	key2, _ := argvValue(runs[1], "--api-key")
	port2, _ := argvValue(runs[1], "--port")
	if key2 == key {
		t.Fatal("api key reused across requests")
	}
	if port2 == port {
		t.Fatal("port reused across requests; expected fresh random port")
	}
}

func TestGenerate_Busy(t *testing.T) {
	binary := fakeEngine(t, "")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second})
	defer eng.Close()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	blocking := func(s string) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- eng.Generate(context.Background(), request(binary, model), blocking)
	}()
	select {
	case <-entered:
	case <-time.After(20 * time.Second):
		close(release)
		t.Fatal("first Generate never reached streaming")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := eng.Generate(ctx, request(binary, model), nil)
	pub := engineError(t, err)
	if pub.Code != "busy" {
		t.Fatalf("second Generate code = %q, want busy", pub.Code)
	}
	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first Generate: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("first Generate did not finish after release")
	}
}

func TestGenerate_CancelDuringStart(t *testing.T) {
	binary := fakeEngine(t, "hang_health")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 30 * time.Second})
	defer eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Generate(ctx, request(binary, model), nil) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancelled Generate = %v, want context cancellation", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cancelled Generate did not return")
	}
	// The slot must be released: a new call fails with the caller's own
	// cancellation, not busy.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if err := eng.Generate(ctx2, request(binary, model), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("after cancel Generate = %v, want context.Canceled (slot leak?)", err)
	}
}

func TestGenerate_CancelStreamMidway(t *testing.T) {
	binary := fakeEngine(t, "slow_stream")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second})
	defer eng.Close()

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- eng.Generate(ctx, request(binary, model), func(s string) error {
			once.Do(func() { close(first) })
			return nil
		})
	}()
	select {
	case <-first:
	case <-time.After(20 * time.Second):
		cancel()
		t.Fatal("no delta before cancel point")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancelled stream = %v, want context cancellation", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cancelled stream did not return")
	}
}

func TestGenerate_CloseDuringStart(t *testing.T) {
	binary := fakeEngine(t, "hang_health")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 30 * time.Second})

	done := make(chan error, 1)
	go func() { done <- eng.Generate(context.Background(), request(binary, model), nil) }()
	time.Sleep(300 * time.Millisecond)
	closeStart := time.Now()
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(closeStart); took > 20*time.Second {
		t.Fatalf("Close took %v", took)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Generate after Close returned nil")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Generate did not return after Close")
	}
	if err := eng.Generate(context.Background(), request(binary, model), nil); engineError(t, err).Code != "engine_unavailable" {
		t.Fatalf("Generate after Close = %v, want engine_unavailable", err)
	}
}

func TestGenerate_UnexpectedExit(t *testing.T) {
	binary := fakeEngine(t, "exit_early")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 10 * time.Second})
	defer eng.Close()

	req := request(binary, model)
	req.Messages = []engine.Message{{Role: "user", Content: "GIZLI-PROMPT-XYZ"}}
	start := time.Now()
	err := eng.Generate(context.Background(), req, nil)
	if took := time.Since(start); took > 30*time.Second {
		t.Fatalf("unexpected exit took %v", took)
	}
	pub := engineError(t, err)
	if pub.Code != "engine_unavailable" {
		t.Fatalf("code = %q, want engine_unavailable", pub.Code)
	}
	if strings.Contains(err.Error(), model) || strings.Contains(err.Error(), "GIZLI-PROMPT-XYZ") {
		t.Fatalf("error leaks request details: %v", err)
	}
}

func TestGenerate_AuthFailure(t *testing.T) {
	binary := fakeEngine(t, "bad_auth")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 3 * time.Second})
	defer eng.Close()

	start := time.Now()
	err := eng.Generate(context.Background(), request(binary, model), nil)
	took := time.Since(start)
	pub := engineError(t, err)
	if pub.Code != "engine_unavailable" {
		t.Fatalf("code = %q, want engine_unavailable", pub.Code)
	}
	if took > 25*time.Second {
		t.Fatalf("auth failure took %v", took)
	}
}

func TestGenerate_PermissivePropsNeverReceivesPrompt(t *testing.T) {
	// A stray server with public /health and a permissive /props answers
	// every key, so neither liveness nor a lone positive probe binds the
	// port. The engine must abort before the first inference byte.
	binary := fakeEngine(t, "open_props")
	model := modelFile(t)
	promptFile := filepath.Join(t.TempDir(), "prompt.bin")
	t.Setenv(promptEv, promptFile)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 10 * time.Second})
	defer eng.Close()

	req := request(binary, model)
	req.Messages = []engine.Message{{Role: "user", Content: "GIZLI-YABANCI-SUNUCU-DENEME"}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := eng.Generate(ctx, req, nil)
	pub := engineError(t, err)
	if pub.Code != "engine_unavailable" {
		t.Fatalf("code = %q, want engine_unavailable", pub.Code)
	}
	if _, statErr := os.Stat(promptFile); !os.IsNotExist(statErr) {
		t.Fatalf("stray server received the prompt: %v", statErr)
	}
	if strings.Contains(err.Error(), "GIZLI-YABANCI-SUNUCU-DENEME") || strings.Contains(err.Error(), model) {
		t.Fatalf("error leaks request details: %v", err)
	}
}

func TestGenerate_MalformedSSE(t *testing.T) {
	binary := fakeEngine(t, "malformed")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 10 * time.Second})
	defer eng.Close()

	req := request(binary, model)
	req.Messages = []engine.Message{{Role: "user", Content: "GIZLI-PROMPT-ABC"}}
	err := eng.Generate(context.Background(), req, nil)
	pub := engineError(t, err)
	if pub.Code != "engine_unavailable" {
		t.Fatalf("code = %q, want engine_unavailable", pub.Code)
	}
	if strings.Contains(err.Error(), "GIZLI-PROMPT-ABC") || strings.Contains(err.Error(), model) {
		t.Fatalf("error leaks request details: %v", err)
	}
}

func TestGenerate_EOFTruncate(t *testing.T) {
	binary := fakeEngine(t, "truncate")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 10 * time.Second})
	defer eng.Close()

	var pieces []string
	err := eng.Generate(context.Background(), request(binary, model), collect(t, &pieces))
	pub := engineError(t, err)
	if pub.Code != "engine_unavailable" {
		t.Fatalf("code = %q, want engine_unavailable (pieces=%q)", pub.Code, pieces)
	}
}

func TestGenerate_GPUFallbackStartup(t *testing.T) {
	binary := fakeEngine(t, "fail_gpu_health")
	model := modelFile(t)
	record := filepath.Join(t.TempDir(), "record.jsonl")
	t.Setenv(recordEv, record)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second, GPULayers: 8})
	defer eng.Close()

	var pieces []string
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := eng.Generate(ctx, request(binary, model), collect(t, &pieces)); err != nil {
		t.Fatalf("Generate with GPU fallback: %v", err)
	}
	if got := strings.Join(pieces, ""); got != "merhaba" {
		t.Fatalf("deltas joined = %q, want merhaba", got)
	}
	if len(pieces) != 2 {
		t.Fatalf("callbacks = %d, want 2 (answer must not repeat after fallback)", len(pieces))
	}
	runs := readArgvLines(t, record)
	if len(runs) != 2 {
		t.Fatalf("spawns = %d, want 2 (gpu then cpu)", len(runs))
	}
	if v, _ := argvValue(runs[0], "-ngl"); v != "8" {
		t.Fatalf("first attempt -ngl = %q, want 8", v)
	}
	if hasConsecutive(runs[0], "--device", "none") {
		t.Fatalf("first attempt must be GPU (no --device none): %q", runs[0])
	}
	if !hasConsecutive(runs[1], "--device", "none") {
		t.Fatalf("fallback must be CPU --device none: %q", runs[1])
	}
	if v, _ := argvValue(runs[1], "-ngl"); v != "0" {
		t.Fatalf("fallback -ngl = %q, want 0", v)
	}
}

func TestGenerate_GPUFallbackInference(t *testing.T) {
	binary := fakeEngine(t, "fail_gpu_stream")
	model := modelFile(t)
	record := filepath.Join(t.TempDir(), "record.jsonl")
	t.Setenv(recordEv, record)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second, GPULayers: 4})
	defer eng.Close()

	var pieces []string
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := eng.Generate(ctx, request(binary, model), collect(t, &pieces)); err != nil {
		t.Fatalf("Generate with GPU inference fallback: %v", err)
	}
	if got := strings.Join(pieces, ""); got != "merhaba" {
		t.Fatalf("deltas joined = %q, want merhaba", got)
	}
	if len(pieces) != 2 {
		t.Fatalf("callbacks = %d, want 2 (no repeated prefix)", len(pieces))
	}
	runs := readArgvLines(t, record)
	if len(runs) != 2 {
		t.Fatalf("spawns = %d, want 2", len(runs))
	}
}

func TestGenerate_OnDeltaError(t *testing.T) {
	binary := fakeEngine(t, "")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 15 * time.Second})
	defer eng.Close()

	sentinel := errors.New("delta consumer stopped")
	err := eng.Generate(context.Background(), request(binary, model), func(string) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("Generate = %v, want sentinel", err)
	}
}

func TestGenerate_Invalid(t *testing.T) {
	binary := fakeEngine(t, "")
	model := modelFile(t)
	eng := engine.New(engine.Options{Logger: discardLogger(), StartupTimeout: 5 * time.Second})
	defer eng.Close()

	cases := map[string]engine.Request{
		"empty binary":  {ModelPath: model, Messages: []engine.Message{{Role: "user", Content: "x"}}},
		"missing model": {Binary: binary, ModelPath: filepath.Join(t.TempDir(), "yok.gguf"), Messages: []engine.Message{{Role: "user", Content: "x"}}},
		"no messages":   {Binary: binary, ModelPath: model},
		"bad role":      {Binary: binary, ModelPath: model, Messages: []engine.Message{{Role: "hacker", Content: "x"}}},
		"empty content": {Binary: binary, ModelPath: model, Messages: []engine.Message{{Role: "user", Content: ""}}},
	}
	for name, req := range cases {
		if err := eng.Generate(context.Background(), req, nil); engineError(t, err).Code != "invalid_request" {
			t.Fatalf("%s: Generate = %v, want invalid_request", name, err)
		}
	}
}

// runFakeServer speaks just enough of the llama.cpp OpenAI-compatible API:
// PUBLIC GET /health like the real runtime (no key even with --api-key),
// protected GET /props (the endpoint the engine uses for identity proof),
// and POST /v1/chat/completions SSE. Any completions hit records its raw
// body to ENGELSIZ_ENGINE_PROMPT_FILE so tests can prove a prompt did or
// did not arrive.
func runFakeServer() {
	argv := os.Args[1:]
	port, _ := argvValue(argv, "--port")
	key, _ := argvValue(argv, "--api-key")
	if record := os.Getenv(recordEv); record != "" {
		raw, _ := json.Marshal(argv)
		f, err := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintln(f, string(raw))
			f.Close()
		}
	}
	if pf := os.Getenv(portEv); pf != "" && port != "" {
		_ = os.WriteFile(pf, []byte(port), 0o644)
	}
	mode := os.Getenv(modeEnv)
	if mode == "exit_early" {
		os.Exit(3)
	}
	gpu := !hasConsecutive(argv, "--device", "none")
	authed := func(r *http.Request) bool {
		if mode == "bad_auth" {
			return false
		}
		return r.Header.Get("Authorization") == "Bearer "+key
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if mode == "hang_health" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Second):
				http.Error(w, "hanging", http.StatusInternalServerError)
				return
			}
		}
		if mode == "fail_health" {
			http.Error(w, "unready", http.StatusInternalServerError)
			return
		}
		// Public like the real runtime: no Authorization check here.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		// open_props simulates a stray permissive server that answers any
		// key: the engine must detect this and never send it a prompt.
		if mode == "open_props" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"permissive":true}`)
			return
		}
		if mode == "fail_gpu_health" && gpu {
			http.Error(w, "gpu props failed", http.StatusInternalServerError)
			return
		}
		if !authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if pf := os.Getenv(promptEv); pf != "" {
			_ = os.WriteFile(pf, body, 0o644)
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if !authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if mode == "fail_gpu_stream" && gpu {
			http.Error(w, "gpu inference failed", http.StatusInternalServerError)
			return
		}
		var parsed struct {
			Stream          bool             `json:"stream"`
			ReasoningEffort string           `json:"reasoning_effort"`
			Messages        []map[string]any `json:"messages"`
		}
		if json.Unmarshal(body, &parsed) != nil || !parsed.Stream || parsed.ReasoningEffort != "none" || len(parsed.Messages) == 0 {
			http.Error(w, "bad inference request", http.StatusBadRequest)
			return
		}
		switch mode {
		case "malformed":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "this is not sse\nanother bad line\n")
			return
		case "truncate":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"yar\"}}]}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		case "slow_stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"mer\"}}]}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Second):
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				return
			}
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"mer\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"haba\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
	})
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	_ = srv.ListenAndServe()
	os.Exit(1)
}
