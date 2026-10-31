// Package kiwix owns one foreground kiwix-serve child. No process is recovered
// by name, saved PID or network address.
package kiwix

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"engelsiz/internal/providers"
)

const diagnosticLimit = 64 * 1024

// Options are trusted local integration inputs, not downloaded metadata.
type Options struct {
	BinaryPath     string
	ZIMPaths       []string
	Logger         *log.Logger
	StartupTimeout time.Duration
	StopGrace      time.Duration
}

type attempt struct {
	ctx     context.Context
	cancel  context.CancelFunc
	started chan struct{} // startup result and cleanup are complete
	err     error
	child   *child
	paths   []string // immutable complete launch inputs for this generation
	proxy   *generationTransport
}

type child struct {
	cmd            *exec.Cmd
	done           chan struct{} // closed only by the sole Wait owner, after output drains
	err            error
	expected       bool // guarded by Runtime.mu
	stdout, stderr *output
}

type Runtime struct {
	mu            sync.Mutex
	operations    sync.Mutex // serializes cleanup/replacement; Stop cancels before waiting
	opts          Options
	status        providers.RuntimeStatus
	current       *attempt
	stopping      bool
	stopRequests  int    // pending Stop callers keep the startup barrier closed
	stopEpoch     uint64 // invalidates already queued/in-flight restart requests
	selectPort    func() (int, error)
	executable    func() (string, error)
	listenerOwner func(int, int) (bool, error)
	dialContent   func(context.Context, string, string) (net.Conn, error)
}

func New(opts Options) (*Runtime, error) {
	var err error
	if opts.BinaryPath != "" {
		opts.BinaryPath, err = filepath.Abs(opts.BinaryPath)
		if err != nil {
			return nil, fmt.Errorf("absolute kiwix executable: %w", err)
		}
	}
	opts.ZIMPaths = append([]string(nil), opts.ZIMPaths...)
	for i, path := range opts.ZIMPaths {
		opts.ZIMPaths[i], err = filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("absolute local content path: %w", err)
		}
	}
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	if opts.StartupTimeout <= 0 {
		opts.StartupTimeout = 10 * time.Second
	}
	if opts.StopGrace <= 0 {
		opts.StopGrace = 2 * time.Second
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
	return &Runtime{opts: opts, status: providers.RuntimeStatus{State: providers.Stopped}, selectPort: availablePort, executable: os.Executable, listenerOwner: childOwnsListener, dialContent: dialer.DialContext}, nil
}

func (r *Runtime) Status() providers.RuntimeStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Runtime) EnsureStarted(ctx context.Context) error {
	return r.ensureStarted(ctx, nil)
}

func (r *Runtime) ensureStarted(ctx context.Context, restartEpoch *uint64) error {
	r.mu.Lock()
	epoch := r.stopEpoch
	if restartEpoch != nil && *restartEpoch != epoch {
		r.mu.Unlock()
		return fmt.Errorf("kiwix restart interrupted by stop: %w", context.Canceled)
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return err
	}
	if r.stopping {
		r.mu.Unlock()
		return fmt.Errorf("kiwix startup interrupted by stop: %w", context.Canceled)
	}
	if len(r.opts.ZIMPaths) == 0 {
		r.mu.Unlock()
		return nil
	}
	if r.status.State == providers.Ready {
		r.mu.Unlock()
		return nil
	}
	a := r.current
	initiating := false
	if a != nil && r.status.State == providers.Stopped {
		select {
		case <-a.started:
			r.current, a = nil, nil // a cancelled and fully cleaned attempt may be retried
		default:
		}
	}
	if a != nil && r.status.State == providers.Failed {
		select {
		case <-a.started:
			err := errors.New(r.status.LastError)
			r.mu.Unlock()
			return err
		default:
		}
	}
	if a == nil {
		if r.status.State == providers.Failed {
			err := errors.New(r.status.LastError)
			r.mu.Unlock()
			return err
		}
		startCtx, cancel := context.WithTimeout(ctx, r.opts.StartupTimeout)
		a = &attempt{ctx: startCtx, cancel: cancel, started: make(chan struct{}), paths: append([]string(nil), r.opts.ZIMPaths...)}
		r.current = a
		initiating = true
		r.status = providers.RuntimeStatus{State: providers.Starting}
		go r.start(a)
	}
	r.mu.Unlock()
	select {
	case <-a.started:
	case <-ctx.Done():
		// Initiating context cancels a.ctx. Its owner must finish cleanup before
		// reporting cancellation; other callers may stop waiting independently.
		if !initiating {
			return ctx.Err()
		}
		<-a.started
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if epoch != r.stopEpoch {
		return errors.Join(a.err, fmt.Errorf("kiwix startup interrupted by stop: %w", context.Canceled))
	}
	return a.err
}

func (r *Runtime) start(a *attempt) {
	defer a.cancel() // release the startup timer; it never owns child lifetime
	err := r.launch(a)
	if err != nil {
		r.mu.Lock()
		c := a.child
		if c != nil {
			c.expected = true
		}
		r.mu.Unlock()
		if c != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), r.opts.StopGrace+3*time.Second)
			err = errors.Join(err, r.stopChild(cleanupCtx, c))
			cancel()
			err = fmt.Errorf("%w%s", err, c.diagnostics())
		}
		r.mu.Lock()
		if r.current == a {
			r.status.PID = 0
			if a.ctx.Err() != nil && (errors.Is(a.ctx.Err(), context.Canceled) || r.stopping) {
				r.status.State = providers.Stopped
			} else {
				r.status.State = providers.Failed
				r.status.LastError = err.Error()
			}
		}
		r.mu.Unlock()
		r.opts.Logger.Printf("kiwix startup failed: %v", err)
	}
	r.mu.Lock()
	a.err = err
	close(a.started)
	r.mu.Unlock()
}

func (r *Runtime) launch(a *attempt) error {
	binary, err := resolveBinary(r.opts.BinaryPath, r.executable)
	if err != nil {
		return err
	}
	for _, path := range a.paths {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat local content %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("local content %q is not a regular file", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open local content %q: %w", path, err)
		}
		openedInfo, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return fmt.Errorf("stat local content %q: %w", path, statErr)
		}
		if !openedInfo.Mode().IsRegular() {
			return fmt.Errorf("local content %q is not a regular file", path)
		}
	}
	port, err := r.selectPort()
	if err != nil {
		return fmt.Errorf("select kiwix loopback port: %w", err)
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	// Detect a listener that won the handoff before launch. This check and the
	// later exact-child listener check close the ordinary collision gap; this is
	// not authentication against hostile local processes.
	reservation, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("kiwix port handoff %s: %w", address, err)
	}
	reservation.Close()
	args := append([]string{"--address=127.0.0.1", "--port=" + strconv.Itoa(port), "--urlRootLocation=/content"}, a.paths...)
	c := &child{cmd: exec.Command(binary, args...), done: make(chan struct{})}
	// Bound exec output draining even if an unexpected descendant inherits a pipe.
	c.cmd.WaitDelay = 500 * time.Millisecond
	c.stdout = &output{label: "stdout", logger: r.opts.Logger}
	c.stderr = &output{label: "stderr", logger: r.opts.Logger}
	c.cmd.Stdout, c.cmd.Stderr = c.stdout, c.stderr
	// Stop and Start synchronize on this lock: once Stop wins, this attempt
	// cannot spawn a late child after it has observed cancellation.
	r.mu.Lock()
	if err := a.ctx.Err(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("kiwix start cancelled: %w", err)
	}
	if r.stopping {
		r.mu.Unlock()
		return errors.New("kiwix start interrupted by stop")
	}
	err = c.cmd.Start()
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("start kiwix executable %q: %w", binary, err)
	}
	a.child = c
	r.status.PID = c.cmd.Process.Pid
	go r.reap(a, c)
	r.mu.Unlock()
	r.opts.Logger.Printf("kiwix start: executable=%q pid=%d address=%s", binary, c.cmd.Process.Pid, address)
	if err := r.waitReady(a, c, address, port); err != nil {
		return fmt.Errorf("kiwix readiness: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("kiwix exited before ready: %v", c.err)
	default:
	}
	if err := a.ctx.Err(); err != nil {
		return fmt.Errorf("kiwix readiness cancelled: %w", err)
	}
	if r.stopping || r.current != a {
		return fmt.Errorf("kiwix readiness interrupted by stop: %w", context.Canceled)
	}
	r.status.State = providers.Ready
	r.status.LastError = ""
	a.proxy = r.newGenerationTransport(a, c, address, port)
	r.opts.Logger.Printf("kiwix ready: pid=%d", c.cmd.Process.Pid)
	return nil
}

func (r *Runtime) reap(a *attempt, c *child) {
	err := c.cmd.Wait() // exactly one Wait, including exec's output copy completion
	r.opts.Logger.Printf("kiwix exit: pid=%d result=%v", c.cmd.Process.Pid, err)
	r.mu.Lock()
	c.err = err
	if a.proxy != nil {
		a.proxy.transport.CloseIdleConnections()
	}
	if r.current == a {
		r.status.PID = 0
		if !c.expected {
			r.status.State = providers.Failed
			r.status.LastError = fmt.Sprintf("kiwix unexpected exit: %v%s", err, c.diagnostics())
		}
	}
	close(c.done)
	r.mu.Unlock()
}

func (r *Runtime) waitReady(a *attempt, c *child, address string, port int) error {
	client := &http.Client{Timeout: 250 * time.Millisecond, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var ownershipErr error
	startupContextError := func() error {
		if errors.Is(a.ctx.Err(), context.Canceled) {
			// A pending/unreadable listener is normal during startup. A winning
			// cancellation is controlled shutdown, not an additional failure.
			return a.ctx.Err()
		}
		return errors.Join(a.ctx.Err(), ownershipErr)
	}
	for {
		select {
		case <-a.ctx.Done():
			return startupContextError()
		case <-c.done:
			if a.ctx.Err() != nil {
				return startupContextError()
			}
			return fmt.Errorf("child exited: %v", c.err)
		default:
		}
		owned, err := r.listenerOwner(c.cmd.Process.Pid, port)
		if err != nil {
			ownershipErr = fmt.Errorf("prove child PID %d listener ownership at %s: %w", c.cmd.Process.Pid, address, err)
		} else if !owned {
			ownershipErr = fmt.Errorf("child PID %d does not own listener at %s", c.cmd.Process.Pid, address)
		} else {
			ownershipErr = nil
			req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, "http://"+address+"/content/", nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			ok := false
			if err == nil {
				ok = resp.StatusCode == http.StatusOK
				resp.Body.Close()
			}
			if ok {
				// Re-observe ownership after HTTP, then launch checks cancellation,
				// current generation and the sole Wait owner's liveness signal.
				owned, err = r.listenerOwner(c.cmd.Process.Pid, port)
				if err == nil && owned {
					return nil
				}
			}
		}
		select {
		case <-a.ctx.Done():
			return startupContextError()
		case <-c.done:
			if a.ctx.Err() != nil {
				return startupContextError()
			}
			return fmt.Errorf("child exited: %v", c.err)
		case <-ticker.C:
		}
	}
}

func (r *Runtime) Stop(ctx context.Context) error {
	// Publish intent before operation serialization: Restart may be awaiting
	// replacement readiness while holding operations. Cancellation wakes it.
	r.mu.Lock()
	r.stopEpoch++
	r.stopRequests++
	r.stopping = true
	r.cancelCurrentLocked()
	r.mu.Unlock()
	r.operations.Lock()
	defer r.operations.Unlock()
	err := r.stop(ctx)
	r.mu.Lock()
	r.stopRequests--
	r.stopping = r.stopRequests > 0
	r.mu.Unlock()
	return err
}

// cancelCurrentLocked publishes the startup barrier before any blocking cleanup.
func (r *Runtime) cancelCurrentLocked() {
	if a := r.current; a != nil {
		r.status.State = providers.Stopping
		if a.child != nil {
			a.child.expected = true
		}
		if a.proxy != nil {
			a.proxy.transport.CloseIdleConnections()
		}
		a.cancel()
	}
}

// stop runs with operations held and the startup barrier already published.
func (r *Runtime) stop(ctx context.Context) error {
	r.mu.Lock()
	a := r.current
	if a == nil {
		r.status.State = providers.Stopped
		r.mu.Unlock()
		return nil
	}
	r.cancelCurrentLocked()
	r.mu.Unlock()
	// The starter always performs bounded cleanup on startup failure. Waiting
	// here prevents a replacement from overlapping it or its output drains.
	<-a.started
	r.mu.Lock()
	c := a.child
	r.mu.Unlock()
	var err error
	if c != nil {
		err = r.stopChild(ctx, c)
	}
	r.mu.Lock()
	if r.current == a {
		if err == nil {
			r.current = nil
			r.status.PID = 0
			r.status.State = providers.Stopped
		} else {
			r.status.State = providers.Failed
			r.status.LastError = err.Error()
		}
	}
	r.mu.Unlock()
	r.opts.Logger.Printf("kiwix stop: %v", err)
	return err
}

func (r *Runtime) stopChild(ctx context.Context, c *child) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	err := terminate(c.cmd.Process)
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		r.opts.Logger.Printf("kiwix terminate: %v", err)
	}
	timer := time.NewTimer(r.opts.StopGrace)
	defer timer.Stop()
	select {
	case <-c.done:
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	killErr := c.cmd.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	// Even a cancelled caller must allow the direct owned child to be reaped.
	reapTimer := time.NewTimer(2 * time.Second)
	defer reapTimer.Stop()
	select {
	case <-c.done:
		return killErr
	case <-reapTimer.C:
		return errors.Join(killErr, errors.New("kiwix forced termination did not finish reaping within 2s"))
	}
}

func (r *Runtime) Restart(ctx context.Context) error {
	r.mu.Lock()
	epoch := r.stopEpoch
	stopping := r.stopping
	r.mu.Unlock()
	if stopping {
		return fmt.Errorf("kiwix restart interrupted by stop: %w", context.Canceled)
	}
	r.operations.Lock()
	defer r.operations.Unlock()
	r.mu.Lock()
	if r.stopping || epoch != r.stopEpoch {
		r.mu.Unlock()
		return fmt.Errorf("kiwix queued restart interrupted by stop: %w", context.Canceled)
	}
	r.stopping = true // block ensure during old-child cleanup
	r.mu.Unlock()
	r.opts.Logger.Printf("kiwix restart")
	err := r.stop(ctx)
	r.mu.Lock()
	r.stopping = r.stopRequests > 0
	interrupted := epoch != r.stopEpoch || r.stopping
	r.mu.Unlock()
	if err != nil {
		return fmt.Errorf("kiwix restart stop: %w", err)
	}
	if interrupted {
		return fmt.Errorf("kiwix restart interrupted by stop: %w", context.Canceled)
	}
	return r.ensureStarted(ctx, &epoch)
}

// ReplaceZIMPaths applies the complete trusted installed set. A failed equal set
// is retried only by an explicit retry; changed committed sets activate normally.
// The paths are copied and normalized before becoming immutable attempt inputs.
func (r *Runtime) ReplaceZIMPaths(ctx context.Context, paths []string, retry bool) error {
	r.mu.Lock()
	epoch := r.stopEpoch
	// A replacement's temporary startup barrier must queue other refreshes;
	// only an actual Stop rejects a newly arriving refresh immediately.
	stopping := r.stopRequests > 0
	r.mu.Unlock()
	if stopping {
		return fmt.Errorf("kiwix refresh interrupted by stop: %w", context.Canceled)
	}
	next, err := normalizedPaths(paths)
	if err != nil {
		return err
	}
	r.operations.Lock()
	defer r.operations.Unlock()
	r.mu.Lock()
	if r.stopping || epoch != r.stopEpoch {
		r.mu.Unlock()
		return fmt.Errorf("kiwix queued refresh interrupted by stop: %w", context.Canceled)
	}
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return err
	}
	previous, err := normalizedPaths(r.opts.ZIMPaths)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if len(next) != 0 && slices.Equal(previous, next) {
		state := r.status.State
		if state == providers.Ready || state == providers.Starting || (state == providers.Failed && !retry) {
			r.mu.Unlock()
			return r.ensureStarted(ctx, &epoch)
		}
	}
	r.stopping = true
	r.cancelCurrentLocked()
	r.mu.Unlock()
	err = r.stop(ctx)
	r.mu.Lock()
	r.stopping = r.stopRequests > 0
	interrupted := epoch != r.stopEpoch || r.stopping
	if err == nil && !interrupted {
		r.opts.ZIMPaths = next
		r.status = providers.RuntimeStatus{State: providers.Stopped}
	}
	r.mu.Unlock()
	if err != nil {
		return fmt.Errorf("kiwix refresh stop: %w", err)
	}
	if interrupted {
		return fmt.Errorf("kiwix refresh interrupted by stop: %w", context.Canceled)
	}
	return r.ensureStarted(ctx, &epoch)
}

func normalizedPaths(paths []string) ([]string, error) {
	result := make([]string, len(paths))
	for i, path := range paths {
		if path == "" {
			return nil, errors.New("empty local content path")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("absolute local content path: %w", err)
		}
		result[i] = absolute
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

// Snapshot returns one atomic private status/loaded-input snapshot. Non-ready
// generations never advertise content, including during replacement or failure.
func (r *Runtime) Snapshot() (providers.RuntimeStatus, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != providers.Ready || r.current == nil || r.current.child == nil {
		return r.status, nil
	}
	select {
	case <-r.current.child.done:
		return r.status, nil
	default:
		return r.status, append([]string(nil), r.current.paths...)
	}
}

func availablePort() (int, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, ln.Close()
}

type output struct {
	mu     sync.Mutex
	tail   []byte
	label  string
	logger *log.Logger
}

func (w *output) Write(p []byte) (int, error) {
	n := len(p)
	w.mu.Lock()
	if len(p) >= diagnosticLimit {
		w.tail = append(w.tail[:0], p[len(p)-diagnosticLimit:]...)
	} else {
		if excess := len(w.tail) + len(p) - diagnosticLimit; excess > 0 {
			copy(w.tail, w.tail[excess:])
			w.tail = w.tail[:len(w.tail)-excess]
		}
		w.tail = append(w.tail, p...)
	}
	w.mu.Unlock()
	// Runtime access/error output can contain article paths, queries or remote
	// bodies. Record stream activity, never its untrusted text.
	if w.logger != nil {
		w.logger.Printf("kiwix %s: bytes=%d content=omitted", w.label, n)
	}
	return n, nil
}
func (w *output) snapshot() string { w.mu.Lock(); defer w.mu.Unlock(); return string(w.tail) }
func (c *child) diagnostics() string {
	return fmt.Sprintf("; stdout bytes captured: %d; stderr bytes captured: %d", len(c.stdout.snapshot()), len(c.stderr.snapshot()))
}
