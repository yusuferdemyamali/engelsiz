package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"engelsiz/internal/database"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"engelsiz/internal/providers"
	"engelsiz/internal/providers/kiwix"
	"engelsiz/internal/testsupport/kiwixfixture"
)

func appProfile(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux XDG and browser integration fixture")
	}
	base := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, base)
	}
	marker := filepath.Join(base, "browser-url")
	t.Setenv("ENGELSIZ_TEST_BROWSER_MARKER", marker)
	bin := filepath.Join(base, "browser bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" > \"$ENGELSIZ_TEST_BROWSER_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(base, "engelsiz", "logs", "engelsiz.log"), marker
}

func runApp(t *testing.T, opts Options) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- Run(ctx, opts)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(15 * time.Second):
			t.Error("application still running after test cleanup")
		}
	})
	return cancel, done
}

func appResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("application did not finish bounded cleanup")
		return nil
	}
}

func waitApp(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func readAppFile(path string) string {
	body, _ := os.ReadFile(path)
	return string(body)
}

func fixtureProvider(t *testing.T, cfg kiwixfixture.Config) (*kiwix.KiwixProvider, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "events.jsonl")
	cfg.MarkerPath = marker
	content := kiwixfixture.WriteConfig(t, cfg)
	// Seed the same durable registry used in production. Run must replace
	// provider option paths with accepted installed files, not fixture shortcuts.
	root := filepath.Join(os.Getenv("XDG_DATA_HOME"), "engelsiz")
	if err := os.MkdirAll(filepath.Join(root, "content", "kiwix"), 0700); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	body, err := os.ReadFile(content)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, "content", "kiwix", id+".zim")
	if err = os.WriteFile(local, body, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(root, "engelsiz.db"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT OR REPLACE INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value,artifact_owner,artifact_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "kiwix", "fixture", "Fixture", "nopic", "fixture.zim", "https://fixture.invalid/", filepath.Join(root, "downloads", id+".part"), len(body), len(body), "completed", stamp, stamp, "wikipedia", "tr", "v1", len(body), "sha256", hex.EncodeToString(sum[:]), id, "consumed")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO content_packages(id,download_id,provider,provider_package_id,content_type,language,variant,title,version,source_url,source_path,local_path,size_bytes,checksum_algorithm,checksum_value,status,error_code,error_message,created_at,updated_at,installed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, id, "kiwix", "fixture", "wikipedia", "tr", "nopic", "Fixture", "v1", "https://fixture.invalid/", filepath.Join(root, "downloads", id+".part"), local, len(body), "sha256", hex.EncodeToString(sum[:]), "installed", "", "", stamp, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := kiwix.New(kiwix.Options{
		BinaryPath:     kiwixfixture.Build(t),
		ZIMPaths:       []string{content},
		StartupTimeout: 8 * time.Second,
		StopGrace:      100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.Stop(ctx); err != nil {
			t.Errorf("fixture cleanup: %v", err)
		}
	})
	return p, marker
}

func assertFixtureReleased(t *testing.T, marker string) {
	t.Helper()
	events := kiwixfixture.ReadEvents(t, marker)
	if len(events) == 0 {
		t.Fatal("expected actual child-process evidence")
	}
	for _, event := range events {
		if event.Kind != "start" {
			continue
		}
		process, err := os.FindProcess(event.PID)
		if err != nil {
			t.Fatal(err)
		}
		if err := process.Signal(syscall.Signal(0)); err == nil {
			t.Errorf("owned process %d still exists after app return", event.PID)
		}
		_ = process.Release()
		if event.Address != "" {
			ln, err := net.Listen("tcp4", event.Address)
			if err != nil {
				t.Errorf("private address %s not released: %v", event.Address, err)
			} else {
				ln.Close()
			}
		}
	}
}

func TestEmptyProfileInvalidKiwixBinaryKeepsFoundation(t *testing.T) {
	logPath, browserMarker := appProfile(t)
	cancel, done := runApp(t, Options{KiwixBinary: filepath.Join(t.TempDir(), "absent kiwix-serve")})
	waitApp(t, "empty-profile browser", func() bool { return readAppFile(browserMarker) != "" })
	url := strings.TrimSpace(readAppFile(browserMarker))
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/") {
		t.Fatalf("browser URL = %q", url)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, path := range []string{"api/health", ""} {
		resp, err := client.Get(url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q err=%v", path, resp.StatusCode, body, err)
		}
		if path == "api/health" && strings.TrimSpace(string(body)) != `{"status":"ok"}` {
			t.Fatalf("foundation health changed: %s", body)
		}
	}
	cancel()
	if err := appResult(t, done); err != nil {
		t.Fatal(err)
	}
	logs := readAppFile(logPath)
	if !strings.Contains(logs, "health check ok") || strings.Contains(logs, "content provider startup failed") {
		t.Fatalf("empty-profile startup log: %s", logs)
	}
}

func TestRuntimeStartupFailureKeepsShellAndBrowser(t *testing.T) {
	logPath, browserMarker := appProfile(t)
	p, marker := fixtureProvider(t, kiwixfixture.Config{EarlyExit: true, ExitCode: 23})
	cancel, done := runApp(t, Options{ContentProvider: p})
	waitApp(t, "browser after content failure", func() bool { return readAppFile(browserMarker) != "" })
	if p.Status().State != providers.Failed {
		t.Fatalf("runtime=%+v", p.Status())
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	base := strings.TrimSpace(readAppFile(browserMarker))
	for _, path := range []string{"api/health", "api/packages", "api/runtime", ""} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	cancel()
	if err := appResult(t, done); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAppFile(logPath), "content provider startup failed") {
		t.Fatal("missing private diagnostics")
	}
	assertFixtureReleased(t, marker)
}

func TestRuntimeReadinessCancellationCleansChild(t *testing.T) {
	_, browserMarker := appProfile(t)
	p, marker := fixtureProvider(t, kiwixfixture.Config{ReadyDelay: time.Hour})
	cancel, done := runApp(t, Options{ContentProvider: p})
	waitApp(t, "runtime process startup", func() bool { return readAppFile(marker) != "" })
	cancel()
	if err := appResult(t, done); err != nil {
		t.Fatalf("readiness cancellation = %v", err)
	}
	if readAppFile(browserMarker) != "" {
		t.Fatal("browser launched before cancelled readiness completed")
	}
	if status := p.Status(); status.PID != 0 || status.State != providers.Stopped {
		t.Fatalf("cancelled runtime status = %+v", status)
	}
	assertFixtureReleased(t, marker)
}

func TestRuntimeReadyCancellationReleasesChildAndPrivatePort(t *testing.T) {
	_, browserMarker := appProfile(t)
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = originalStdout
		output.Close()
	})
	sentinel, sentinelMarker := fixtureProvider(t, kiwixfixture.Config{})
	if err := sentinel.EnsureStarted(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, marker := fixtureProvider(t, kiwixfixture.Config{})
	cancel, done := runApp(t, Options{ContentProvider: p})
	waitApp(t, "ready runtime and browser", func() bool { return readAppFile(browserMarker) != "" })
	if status := p.Status(); status.State != providers.Ready || status.PID == 0 {
		t.Fatalf("browser opened before content readiness: %+v", status)
	}
	url := strings.TrimSpace(readAppFile(browserMarker))
	for _, event := range kiwixfixture.ReadEvents(t, marker) {
		if event.Kind == "start" && strings.Contains(url, event.Address) {
			t.Fatalf("private runtime address exposed to browser: %s", url)
		}
	}
	cancel()
	if err := appResult(t, done); err != nil {
		t.Fatal(err)
	}
	if publicOutput := readAppFile(output.Name()); publicOutput != "Engelsiz running at "+strings.TrimSuffix(url, "/")+"\n" {
		t.Fatalf("unexpected public stdout: %q", publicOutput)
	}
	if status := p.Status(); status.PID != 0 || status.State != providers.Stopped {
		t.Fatalf("stopped runtime status = %+v", status)
	}
	assertFixtureReleased(t, marker)
	address := strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/")
	ln, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("application HTTP listener remains active: %v", err)
	}
	ln.Close()
	if status := sentinel.Status(); status.State != providers.Ready || status.PID == 0 {
		t.Fatalf("unrelated runtime was stopped: %+v", status)
	}
	for _, event := range kiwixfixture.ReadEvents(t, sentinelMarker) {
		if event.Kind == "start" {
			conn, err := net.DialTimeout("tcp4", event.Address, time.Second)
			if err != nil {
				t.Fatalf("unrelated runtime listener stopped: %v", err)
			}
			conn.Close()
		}
	}
}

type cleanupFailureProvider struct {
	providers.ContentProvider
	startupErr error
	stopErr    error
	stopped    bool
	fresh      bool
	cancel     context.CancelFunc
}

func (p *cleanupFailureProvider) EnsureStarted(context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	return p.startupErr
}
func (p *cleanupFailureProvider) Stop(ctx context.Context) error {
	p.stopped = true
	_, bounded := ctx.Deadline()
	p.fresh = ctx.Err() == nil && bounded
	return p.stopErr
}

func TestRuntimeCleanupErrorJoinedWithStartupError(t *testing.T) {
	logPath, browserMarker := appProfile(t)
	startupErr := errors.New("fixture startup failure")
	cleanupErr := errors.New("fixture cleanup failure")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &cleanupFailureProvider{startupErr: startupErr, stopErr: cleanupErr, cancel: cancel}
	err := Run(ctx, Options{ContentProvider: p})
	if !errors.Is(err, startupErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("startup/cleanup errors were not retained: %v", err)
	}
	if !p.stopped || !p.fresh {
		t.Fatalf("cleanup missing or context not fresh/bounded: %+v", p)
	}
	if readAppFile(browserMarker) != "" || !strings.Contains(readAppFile(logPath), "content provider stop failed") {
		t.Fatalf("cleanup diagnostics/browser behavior: %s", readAppFile(logPath))
	}
}

func TestRuntimeCleanupUsesFreshContextAfterCancellation(t *testing.T) {
	appProfile(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanupErr := errors.New("fixture cleanup failure")
	p := &cleanupFailureProvider{startupErr: context.Canceled, stopErr: cleanupErr, cancel: cancel}
	err := Run(ctx, Options{ContentProvider: p})
	if errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) || !p.fresh {
		t.Fatalf("cancelled startup cleanup: err=%v fresh=%v", err, p.fresh)
	}
}

func TestRuntimeStartupCancellationRetainsOtherFailures(t *testing.T) {
	for _, joined := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrelated startup failure", true: "joined startup cleanup failure"}[joined], func(t *testing.T) {
			_, browserMarker := appProfile(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("fixture startup cleanup failure")
			startupErr := failure
			if joined {
				startupErr = errors.Join(context.Canceled, failure)
			}
			p := &cleanupFailureProvider{startupErr: startupErr, cancel: cancel}
			if err := Run(ctx, Options{ContentProvider: p}); !errors.Is(err, failure) {
				t.Fatalf("failure lost during application cancellation: %v", err)
			}
			if !p.stopped || !p.fresh || readAppFile(browserMarker) != "" {
				t.Fatalf("cancelled failed startup cleanup/browser: %+v", p)
			}
		})
	}
}
