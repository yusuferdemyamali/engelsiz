package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListenLoopbackOnly(t *testing.T) {
	ln, port, err := ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if port <= 0 {
		t.Fatalf("invalid port %d", port)
	}
	addr := ln.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listener not loopback-only: %s", addr)
	}
	if strings.Contains(addr, "0.0.0.0") || strings.Contains(addr, "[::]") {
		t.Fatalf("wildcard bind forbidden: %s", addr)
	}
}

func TestListenFallbackWhenPreferredOccupied(t *testing.T) {
	occupant, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve preferred port: %v", err)
	}
	defer occupant.Close()

	preferred := occupant.Addr().(*net.TCPAddr).Port
	ln, port, err := ListenLoopback(preferred)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if port == preferred {
		t.Fatal("expected fallback to a different loopback port")
	}
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Fatalf("fallback not loopback-only: %s", ln.Addr())
	}
}

func TestPreferredPort(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	preferred := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()
	ln, port, err := ListenLoopback(preferred)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if port != preferred {
		t.Fatalf("preferred port = %d", port)
	}
}

func TestInvalidPreferredPortDoesNotFallback(t *testing.T) {
	ln, _, err := ListenLoopback(-1)
	if ln != nil {
		ln.Close()
	}
	if err == nil {
		t.Fatal("invalid port must fail")
	}
}

func TestHealthCancellationAndPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not health</html>"))
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForHealth(ctx, server.URL, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	if err := waitForHealth(context.Background(), server.URL, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("false health accepted: %v", err)
	}
}

func TestAlreadyCancelledRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestLoggerFailureIsExplicit(t *testing.T) {
	_, f, err := initLogger(t.TempDir())
	if f != nil {
		f.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "open log file") {
		t.Fatalf("logger failure = %v", err)
	}
}

func TestCorruptDatabaseStartupLogsWithoutBrowser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux XDG startup fixture; Windows directories covered by config tests")
	}
	base := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, base)
	}
	root := filepath.Join(base, "engelsiz")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "engelsiz.db")
	body := []byte("corrupt database fixture")
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), Options{}); err == nil {
		t.Fatal("corrupt DB started")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(body) {
		t.Fatalf("corrupt DB changed: %q %v", actual, err)
	}
	logBody, err := os.ReadFile(filepath.Join(root, "logs", "engelsiz.log"))
	if err != nil || !strings.Contains(string(logBody), "database open failed") || strings.Contains(string(logBody), "browser helper started") {
		t.Fatalf("startup log: %s %v", logBody, err)
	}
}
