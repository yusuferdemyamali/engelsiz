// This executable intentionally depends only on the standard library so it can
// be compiled separately from the test binary on Linux and Windows.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	ReadyDelay    time.Duration `json:"ready_delay"`
	StatusCode    int           `json:"status_code"`
	Redirect      string        `json:"redirect"`
	EarlyExit     bool          `json:"early_exit"`
	ExitAfter     time.Duration `json:"exit_after"`
	ExitCode      int           `json:"exit_code"`
	StdoutBytes   int           `json:"stdout_bytes"`
	StderrBytes   int           `json:"stderr_bytes"`
	IgnoreSIGTERM bool          `json:"ignore_sigterm"`
	MarkerPath    string        `json:"marker_path"`
}

type event struct {
	Kind    string    `json:"kind"`
	PID     int       `json:"pid"`
	Args    []string  `json:"args,omitempty"`
	Code    int       `json:"code"`
	Address string    `json:"address,omitempty"`
	At      time.Time `json:"at"`
}

func main() { os.Exit(run()) }

func run() (code int) {
	address := flag.String("address", "127.0.0.1", "listen address")
	port := flag.Int("port", 0, "listen port")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "fixture requires a JSON config as its first ZIM path")
		return 2
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	endpoint := net.JoinHostPort(*address, strconv.Itoa(*port))
	if err := record(cfg.MarkerPath, event{Kind: "start", PID: os.Getpid(), Args: os.Args[1:], Address: endpoint, At: time.Now()}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer func() {
		if err := record(cfg.MarkerPath, event{Kind: "exit", PID: os.Getpid(), Code: code, At: time.Now()}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	var output sync.WaitGroup
	output.Add(2)
	go func() { defer output.Done(); flood(os.Stdout, cfg.StdoutBytes, "stdout") }()
	go func() { defer output.Done(); flood(os.Stderr, cfg.StderrBytes, "stderr") }()
	defer output.Wait()
	if cfg.EarlyExit {
		return cfg.ExitCode
	}
	ready := time.NewTimer(cfg.ReadyDelay)
	defer ready.Stop()
	var exit <-chan time.Time
	if cfg.ExitAfter > 0 {
		timer := time.NewTimer(cfg.ExitAfter)
		defer timer.Stop()
		exit = timer.C
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := cfg.StatusCode
		if cfg.Redirect != "" {
			w.Header().Set("Location", cfg.Redirect)
			if status == 0 {
				status = http.StatusFound
			}
		}
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "fixture\n")
	})}
	defer server.Close()
	serveError := make(chan error, 1)
	for {
		select {
		case <-signals:
			if !cfg.IgnoreSIGTERM {
				return 0
			}
		case <-exit:
			return cfg.ExitCode
		case <-ready.C:
			listener, err := net.Listen("tcp4", endpoint)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			go func() { serveError <- server.Serve(listener) }()
		case err := <-serveError:
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
}

func record(path string, e event) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func flood(w io.Writer, size int, label string) {
	chunk := strings.Repeat(label+" ", 4096)
	for size > 0 {
		n := min(size, len(chunk))
		if _, err := io.WriteString(w, chunk[:n]); err != nil {
			return
		}
		size -= n
	}
}
