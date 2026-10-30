package kiwix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/providers"
	fixture "engelsiz/internal/testsupport/kiwixfixture"
)

func setup(t *testing.T, cfg fixture.Config) (*Runtime, string) {
	t.Helper()
	binary := fixture.Build(t)
	marker := filepath.Join(t.TempDir(), "events")
	cfg.MarkerPath = marker
	r, err := New(Options{BinaryPath: binary, ZIMPaths: []string{fixture.WriteConfig(t, cfg)}, StartupTimeout: time.Second, StopGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := r.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return r, marker
}
func eventually(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
func starts(t *testing.T, marker string) int {
	n := 0
	for _, e := range fixture.ReadEvents(t, marker) {
		if e.Kind == "start" {
			n++
		}
	}
	return n
}

func TestEmptyAndResolution(t *testing.T) {
	r, err := New(Options{BinaryPath: "missing override"})
	if err != nil {
		t.Fatal(err)
	}
	r.executable = func() (string, error) { panic("empty runtime must not resolve executable") }
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := r.Status(); s.State != providers.Stopped || s.PID != 0 {
		t.Fatal(s)
	}
	exe := filepath.Join(t.TempDir(), "engelsiz")
	for _, platform := range []string{"linux", "windows"} {
		want := "kiwix-serve"
		if platform == "windows" {
			want += ".exe"
		}
		if got := adjacentBinary(exe, platform); got != filepath.Join(filepath.Dir(exe), "runtime", want) {
			t.Fatal(got)
		}
	}
	dir := t.TempDir()
	valid := filepath.Join(dir, "runtime", "kiwix-serve")
	if runtime.GOOS == "windows" {
		valid += ".exe"
	}
	os.MkdirAll(filepath.Dir(valid), 0755)
	os.WriteFile(valid, []byte("fixture"), 0755)
	resolved, err := resolveBinary("", func() (string, error) { return filepath.Join(dir, "engelsiz"), nil })
	if err != nil || resolved != valid {
		t.Fatalf("adjacent=%s %v", resolved, err)
	}
	if _, err := resolveBinary(filepath.Join(dir, "missing"), func() (string, error) { panic("invalid override fallback") }); err == nil {
		t.Fatal("missing override accepted")
	}
	for _, path := range []string{dir, filepath.Join(dir, "missing")} {
		if _, err := resolveBinary(path, os.Executable); err == nil {
			t.Fatal(path)
		}
	}
	if runtime.GOOS != "windows" {
		nonexec := filepath.Join(dir, "nonexec")
		os.WriteFile(nonexec, []byte("no"), 0600)
		if _, err := resolveBinary(nonexec, os.Executable); err == nil {
			t.Fatal("nonexec accepted")
		}
	}
}

func TestRelativeOverrideFrozenAndPathsSeparate(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 50 * time.Millisecond})
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, r.opts.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(t.TempDir(), "- literal ; $(touch should-not-exist).zim")
	os.WriteFile(extra, []byte("opaque"), 0600)
	r2, err := New(Options{BinaryPath: rel, ZIMPaths: []string{r.opts.ZIMPaths[0], extra}, StartupTimeout: time.Second, StopGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if !filepath.IsAbs(r2.opts.BinaryPath) {
		t.Fatal("override not normalized")
	}
	if err := r2.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer r2.Stop(context.Background())
	events := fixture.ReadEvents(t, marker)
	if len(events) != 1 {
		t.Fatalf("events=%v", events)
	}
	args := events[0].Args
	if len(args) != 5 || args[0] != "--address=127.0.0.1" || !strings.HasPrefix(args[1], "--port=") || args[2] != "--urlRootLocation=/content" || args[3] != r.opts.ZIMPaths[0] || args[4] != extra {
		t.Fatalf("argv=%q", args)
	}
	if s := r2.Status(); s.State != providers.Ready || s.PID != events[0].PID {
		t.Fatal(s)
	}
}

func TestStartupFailuresAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    fixture.Config
		cancel bool
	}{
		{"early zero", fixture.Config{EarlyExit: true}, false},
		{"early nonzero", fixture.Config{EarlyExit: true, ExitCode: 7, StdoutBytes: 200, StderrBytes: 300}, false},
		{"non200", fixture.Config{StatusCode: 503}, false},
		{"redirect", fixture.Config{Redirect: "http://example.invalid/"}, false},
		{"timeout", fixture.Config{ReadyDelay: 3 * time.Second}, false},
		{"cancel", fixture.Config{ReadyDelay: 3 * time.Second}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, marker := setup(t, tc.cfg)
			r.opts.StartupTimeout = 200 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() {
					for starts(t, marker) == 0 {
						time.Sleep(time.Millisecond)
					}
					cancel()
				}()
			}
			err := r.EnsureStarted(ctx)
			if err == nil {
				t.Fatal("startup passed")
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			s := r.Status()
			want := providers.Failed
			if tc.cancel {
				want = providers.Stopped
			}
			if s.State != want || s.PID != 0 {
				t.Fatalf("status=%+v err=%v", s, err)
			}
			if starts(t, marker) != 1 {
				t.Fatal("process not launched")
			}
			if !tc.cancel && s.LastError == "" {
				t.Fatal("missing diagnostic")
			}
			r.mu.Lock()
			c := r.current.child
			r.mu.Unlock()
			select {
			case <-c.done:
			default:
				t.Fatal("startup returned before reap")
			}
		})
	}
}

func TestInvalidBinaryAndContent(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt", "directory", "content missing", "content directory"} {
		t.Run(kind, func(t *testing.T) {
			r, marker := setup(t, fixture.Config{})
			switch kind {
			case "missing":
				r.opts.BinaryPath = filepath.Join(t.TempDir(), "missing")
			case "directory":
				r.opts.BinaryPath = t.TempDir()
			case "corrupt":
				r.opts.BinaryPath = filepath.Join(t.TempDir(), "corrupt")
				os.WriteFile(r.opts.BinaryPath, []byte("broken executable"), 0755)
			case "content missing":
				r.opts.ZIMPaths = []string{filepath.Join(t.TempDir(), "missing")}
			case "content directory":
				r.opts.ZIMPaths = []string{t.TempDir()}
			}
			if err := r.EnsureStarted(context.Background()); err == nil {
				t.Fatal("invalid startup passed")
			}
			if s := r.Status(); s.State != providers.Failed || s.PID != 0 {
				t.Fatal(s)
			}
			if starts(t, marker) != 0 {
				t.Fatal("fixture launched with invalid inputs")
			}
		})
	}
}

func TestPortCollisionPreservesListener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	r, marker := setup(t, fixture.Config{})
	r.selectPort = func() (int, error) { return port, nil }
	err := r.EnsureStarted(context.Background())
	if err == nil || !strings.Contains(err.Error(), "port handoff") {
		t.Fatalf("collision=%v", err)
	}
	if starts(t, marker) != 0 {
		t.Fatal("occupied selected port spawned")
	}
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
}

func TestConcurrentEnsureSuccessAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			cfg := fixture.Config{ReadyDelay: 150 * time.Millisecond}
			if fail {
				cfg.StatusCode = 503
			}
			r, marker := setup(t, cfg)
			r.opts.StartupTimeout = 400 * time.Millisecond
			var wg sync.WaitGroup
			results := make(chan error, 40)
			for i := 0; i < 40; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); results <- r.EnsureStarted(context.Background()) }()
			}
			wg.Wait()
			close(results)
			for err := range results {
				if (err != nil) != fail {
					t.Fatalf("outcome=%v", err)
				}
			}
			if n := starts(t, marker); n != 1 {
				t.Fatalf("starts=%d", n)
			}
			if err := r.EnsureStarted(context.Background()); (err != nil) != fail {
				t.Fatal(err)
			}
			if starts(t, marker) != 1 {
				t.Fatal("repeat started child")
			}
		})
	}
}

func TestUnexpectedExitAndExplicitRecovery(t *testing.T) {
	for _, code := range []int{0, 9} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r, marker := setup(t, fixture.Config{ExitAfter: 250 * time.Millisecond, ExitCode: code})
			if err := r.EnsureStarted(context.Background()); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool { return r.Status().State == providers.Failed })
			if s := r.Status(); s.PID != 0 || !strings.Contains(s.LastError, "unexpected exit") {
				t.Fatal(s)
			}
			if err := r.EnsureStarted(context.Background()); err == nil {
				t.Fatal("failed runtime silently accepted")
			}
			time.Sleep(50 * time.Millisecond)
			if starts(t, marker) != 1 {
				t.Fatal("automatic restart")
			}
			if err := r.Restart(context.Background()); err != nil {
				t.Fatal(err)
			}
			if starts(t, marker) != 2 {
				t.Fatal("explicit restart did not recover")
			}
		})
	}
}

func TestRestartReapsBeforeReplacementAndSentinelSurvives(t *testing.T) {
	sentinel, _ := setup(t, fixture.Config{})
	if err := sentinel.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	sentinelPID := sentinel.Status().PID
	r, marker := setup(t, fixture.Config{})
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	old := r.current.child
	r.mu.Unlock()
	if err := r.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
	default:
		t.Fatal("old child not reaped")
	}
	events := fixture.ReadEvents(t, marker)
	if len(events) != 3 || events[0].Kind != "start" || events[1].Kind != "exit" || events[2].Kind != "start" || events[1].PID != events[0].PID || events[2].PID == events[0].PID {
		t.Fatalf("restart order=%v", events)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := sentinel.Status(); s.PID != sentinelPID || s.State != providers.Ready {
		t.Fatal("unrelated sentinel changed", s)
	}
}

func TestStopWhileStartingAndNextGeneration(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 300 * time.Millisecond})
	result := make(chan error, 1)
	go func() { result <- r.EnsureStarted(context.Background()) }()
	eventually(t, func() bool { return starts(t, marker) == 1 })
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("stopped startup succeeded")
	}
	if s := r.Status(); s.State != providers.Stopped || s.PID != 0 {
		t.Fatal(s)
	}
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if s := r.Status(); s.State != providers.Ready || s.PID == 0 {
		t.Fatal("old generation overwrote ready", s)
	}
	if starts(t, marker) != 2 {
		t.Fatal("wrong generations")
	}
}

func TestForcedStopAndBoundedOutput(t *testing.T) {
	r, marker := setup(t, fixture.Config{IgnoreSIGTERM: true, StdoutBytes: 300000, StderrBytes: 400000})
	var logs lockedBuffer
	r.opts.Logger = log.New(&logs, "", 0)
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	c := r.current.child
	r.mu.Unlock()
	eventually(t, func() bool {
		return len(c.stdout.snapshot()) == diagnosticLimit && len(c.stderr.snapshot()) == diagnosticLimit
	})
	start := time.Now()
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("forced stop was not bounded")
	}
	select {
	case <-c.done:
	default:
		t.Fatal("forced stop did not reap")
	}
	if starts(t, marker) != 1 {
		t.Fatal("unexpected start count")
	}
	text := logs.String()
	if !strings.Contains(text, "kiwix stdout:") || !strings.Contains(text, "kiwix stderr:") {
		t.Fatal("output labels missing")
	}
	if len(c.stdout.tail) > diagnosticLimit || len(c.stderr.tail) > diagnosticLimit {
		t.Fatal("unbounded retention")
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

var _ io.Writer = (*lockedBuffer)(nil)

func TestOneCallerCancellationDoesNotCancelSharedAttempt(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 200 * time.Millisecond})
	owner := make(chan error, 1)
	go func() { owner <- r.EnsureStarted(context.Background()) }()
	eventually(t, func() bool { return starts(t, marker) == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := r.EnsureStarted(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
	if starts(t, marker) != 1 || r.Status().State != providers.Ready {
		t.Fatal("shared attempt cancelled")
	}
}

func TestStartupFailureRetainsBothDiagnosticStreams(t *testing.T) {
	r, _ := setup(t, fixture.Config{EarlyExit: true, ExitCode: 4, StdoutBytes: 100000, StderrBytes: 200000})
	var logs lockedBuffer
	r.opts.Logger = log.New(&logs, "", 0)
	err := r.EnsureStarted(context.Background())
	if err == nil {
		t.Fatal("early exit passed")
	}
	if text := r.Status().LastError; !strings.Contains(text, "stdout tail: ") || !strings.Contains(text, "stderr tail: ") || !strings.Contains(text, "stdout stdout") || !strings.Contains(text, "stderr stderr") {
		t.Fatalf("diagnostic tails missing: %.100s", text)
	}
	if text := logs.String(); !strings.Contains(text, "kiwix stdout:") || !strings.Contains(text, "kiwix stderr:") || !strings.Contains(text, "startup failed:") {
		t.Fatal("failure output absent from logs")
	}
}

func TestStopAndRestartSerialization(t *testing.T) {
	r, marker := setup(t, fixture.Config{})
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- r.Stop(context.Background()) }()
	go func() { defer wg.Done(); errs <- r.Restart(context.Background()) }()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, e := range fixture.ReadEvents(t, marker) {
		if e.Kind == "start" {
			active++
			if active > 1 {
				t.Fatal("overlapping process generations")
			}
		} else if e.Kind == "exit" {
			active--
		}
	}
	if active != 0 {
		t.Fatalf("unreaped markers: %d", active)
	}
}

func TestCollisionAfterSpawnDoesNotAdoptListener(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 500 * time.Millisecond})
	port, err := availablePort()
	if err != nil {
		t.Fatal(err)
	}
	r.selectPort = func() (int, error) { return port, nil }
	result := make(chan error, 1)
	go func() { result <- r.EnsureStarted(context.Background()) }()
	eventually(t, func() bool { return starts(t, marker) == 1 })
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	server.Listener = listener
	server.Start()
	defer server.Close()
	if err := <-result; err == nil {
		t.Fatal("adopted competing HTTP listener")
	}
	if s := r.Status(); s.State != providers.Failed || s.PID != 0 {
		t.Fatal(s)
	}
	r.mu.Lock()
	owned := r.current.child
	r.mu.Unlock()
	select {
	case <-owned.done:
	default:
		t.Fatal("failed owned child not reaped")
	}
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
}

func TestCancelledStartupCanStartNextGeneration(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- r.EnsureStarted(ctx) }()
	eventually(t, func() bool { return starts(t, marker) == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := r.EnsureStarted(context.Background()); err != nil {
		t.Fatalf("new attempt after controlled cancellation: %v", err)
	}
	if starts(t, marker) != 2 || r.Status().State != providers.Ready {
		t.Fatal("cancelled generation blocked replacement")
	}
}

// Stop must interrupt a replacement, even when its caller has no remaining
// deadline. Both cases exercise actual subprocesses and the sole Wait owner.
func TestStopCancelsRestartReadiness(t *testing.T) {
	for _, neverReady := range []bool{false, true} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("neverReady=%v/cancelledStop=%v", neverReady, cancelled), func(t *testing.T) {
				sentinel, _ := setup(t, fixture.Config{})
				if err := sentinel.EnsureStarted(t.Context()); err != nil {
					t.Fatal(err)
				}
				sentinelPID := sentinel.Status().PID
				r, marker := setup(t, fixture.Config{})
				r.opts.StartupTimeout = 4 * time.Second
				if err := r.EnsureStarted(t.Context()); err != nil {
					t.Fatal(err)
				}
				cfg := fixture.Config{MarkerPath: marker, ReadyDelay: 2 * time.Second}
				if neverReady {
					cfg.ReadyDelay = 0
					cfg.StatusCode = 503
				}
				data, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(r.opts.ZIMPaths[0], data, 0600); err != nil {
					t.Fatal(err)
				}
				restarted := make(chan error, 1)
				go func() { restarted <- r.Restart(context.Background()) }()
				eventually(t, func() bool { return starts(t, marker) == 2 && r.Status().State == providers.Starting })
				r.mu.Lock()
				owned := r.current.child
				r.mu.Unlock()
				// A restart already queued behind replacement readiness belongs to
				// the old stop epoch and must not create a third generation.
				queued := make(chan error, 1)
				go func() { queued <- r.Restart(context.Background()) }()
				time.Sleep(20 * time.Millisecond)
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()
				if cancelled {
					cancel()
				}
				began := time.Now()
				if err := r.Stop(ctx); err != nil && !errors.Is(err, ctx.Err()) {
					t.Fatal(err)
				}
				t.Logf("Stop elapsed=%s", time.Since(began))
				if elapsed := time.Since(began); elapsed > 500*time.Millisecond {
					t.Fatalf("Stop blocked behind readiness: %s", elapsed)
				}
				if err := <-restarted; !errors.Is(err, context.Canceled) {
					t.Fatalf("restart outcome=%v", err)
				}
				if err := <-queued; !errors.Is(err, context.Canceled) {
					t.Fatalf("queued restart outcome=%v", err)
				}
				select {
				case <-owned.done:
				default:
					t.Fatal("Stop returned before replacement reap")
				}
				time.Sleep(75 * time.Millisecond)
				if s := r.Status(); s.State != providers.Stopped || s.PID != 0 || starts(t, marker) != 2 {
					t.Fatalf("late generation or stale status: %+v", s)
				}
				if s := sentinel.Status(); s.PID != sentinelPID || s.State != providers.Ready {
					t.Fatal("sentinel changed", s)
				}
				// A later explicit request after the winning Stop is a new epoch.
				data, _ = json.Marshal(fixture.Config{MarkerPath: marker})
				if err := os.WriteFile(r.opts.ZIMPaths[0], data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := r.EnsureStarted(t.Context()); err != nil {
					t.Fatal(err)
				}
				if starts(t, marker) != 3 || r.Status().State != providers.Ready {
					t.Fatal("new explicit generation failed")
				}
			})
		}
	}
}

func TestCollisionWithSameExecutableForeignProcess(t *testing.T) {
	r, marker := setup(t, fixture.Config{ReadyDelay: 500 * time.Millisecond})
	port, err := availablePort()
	if err != nil {
		t.Fatal(err)
	}
	r.selectPort = func() (int, error) { return port, nil }
	result := make(chan error, 1)
	go func() { result <- r.EnsureStarted(t.Context()) }()
	eventually(t, func() bool { return starts(t, marker) == 1 })
	foreignMarker := filepath.Join(t.TempDir(), "foreign-events")
	foreign, err := New(Options{BinaryPath: r.opts.BinaryPath, ZIMPaths: []string{fixture.WriteConfig(t, fixture.Config{MarkerPath: foreignMarker})}, StopGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	foreign.selectPort = func() (int, error) { return port, nil }
	defer foreign.Stop(context.Background())
	if err := foreign.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	foreignPID := foreign.Status().PID
	if err := <-result; err == nil {
		t.Fatal("adopted same-executable foreign listener")
	}
	if s := r.Status(); s.PID != 0 || s.State != providers.Failed {
		t.Fatal(s)
	}
	r.mu.Lock()
	owned := r.current.child
	r.mu.Unlock()
	select {
	case <-owned.done:
	default:
		t.Fatal("collision child not reaped")
	}
	if s := foreign.Status(); s.PID != foreignPID || s.State != providers.Ready {
		t.Fatal("foreign process changed", s)
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/content/", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
}

func TestUnavailableListenerProofCannotBecomeReady(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(fmt.Sprint(unreadable), func(t *testing.T) {
			r, marker := setup(t, fixture.Config{})
			r.opts.StartupTimeout = 300 * time.Millisecond
			r.listenerOwner = func(pid, port int) (bool, error) {
				if unreadable {
					return false, errors.New("ownership evidence unavailable")
				}
				return false, nil
			}
			result := make(chan error, 1)
			go func() { result <- r.EnsureStarted(t.Context()) }()
			eventually(t, func() bool {
				events := fixture.ReadEvents(t, marker)
				if len(events) == 0 {
					return false
				}
				resp, err := http.Get("http://" + events[0].Address + "/content/")
				if err != nil {
					return false
				}
				resp.Body.Close()
				return resp.StatusCode == http.StatusOK
			})
			if err := <-result; !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "ownership") && !strings.Contains(err.Error(), "does not own") {
				t.Fatalf("fail-closed outcome=%v", err)
			}
			if s := r.Status(); s.State != providers.Failed || s.PID != 0 {
				t.Fatal(s)
			}
			r.mu.Lock()
			owned := r.current.child
			r.mu.Unlock()
			select {
			case <-owned.done:
			default:
				t.Fatal("unproved child was not reaped")
			}
		})
	}
}
