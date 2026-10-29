package kiwixfixture

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestFixture(t *testing.T) {
	binary := Build(t)
	t.Run("early exit drains long output and records arguments", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "events.jsonl")
		cfg := WriteConfig(t, Config{EarlyExit: true, ExitCode: 7, StdoutBytes: 256 << 10, StderrBytes: 256 << 10, MarkerPath: marker})
		args := []string{"--address=127.0.0.1", "--port=12345", cfg, "/opaque path/-content.zim"}
		cmd := exec.Command(binary, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if err == nil || cmd.ProcessState.ExitCode() != 7 {
			t.Fatalf("exit = %v; state = %v", err, cmd.ProcessState)
		}
		if stdout.Len() != 256<<10 || stderr.Len() != 256<<10 {
			t.Fatalf("output sizes = %d, %d", stdout.Len(), stderr.Len())
		}
		events := ReadEvents(t, marker)
		if len(events) != 2 || events[0].Kind != "start" || events[1].Kind != "exit" || events[1].Code != 7 {
			t.Fatalf("events = %+v", events)
		}
		if !reflect.DeepEqual(events[0].Args, args) || events[0].PID != cmd.Process.Pid {
			t.Fatalf("start = %+v", events[0])
		}
	})
	t.Run("HTTP redirect and owned process termination", func(t *testing.T) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "events.jsonl")
		cfg := WriteConfig(t, Config{Redirect: "/elsewhere", MarkerPath: marker})
		cmd := exec.Command(binary, "--address", "127.0.0.1", "--port", fmt.Sprint(port), cfg)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		waited := false
		t.Cleanup(func() {
			if !waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		client := &http.Client{Timeout: 100 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		deadline := time.Now().Add(3 * time.Second)
		for {
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/elsewhere" {
					t.Fatalf("response = %+v", resp)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if runtime.GOOS == "windows" {
			err = cmd.Process.Kill()
		} else {
			err = cmd.Process.Signal(syscall.SIGTERM)
		}
		if err != nil {
			t.Fatal(err)
		}
		err = cmd.Wait()
		waited = true
		if runtime.GOOS != "windows" && err != nil {
			t.Fatal(err)
		}
		events := ReadEvents(t, marker)
		if len(events) == 0 || events[0].Address != fmt.Sprintf("127.0.0.1:%d", port) {
			t.Fatalf("events = %+v", events)
		}
		if runtime.GOOS != "windows" && (len(events) != 2 || events[1].Kind != "exit" || events[1].Code != 0) {
			t.Fatalf("graceful exit events = %+v", events)
		}
	})
}
