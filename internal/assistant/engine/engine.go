// Package engine owns one lazy llama.cpp server child per Generate call.
//
// The engine is a standalone subprocess library: each Generate spawns the
// configured server binary with an explicit loopback address, an explicit
// randomly selected port and a random per-request API key, proves the
// listener's identity, streams one bounded /v1/chat/completions SSE response,
// then always terminates and reaps the child before returning, so no idle
// model RAM is held. There is no queue: concurrent Generate calls fail fast
// with a busy error. Close cancels any in-flight Generate and joins it,
// including while startup is still in progress.
//
// Identity evidence: llama.cpp /health is PUBLIC even with --api-key, so an
// authenticated /health round trip proves nothing about which process owns
// the port. The engine therefore treats /health as a mere liveness signal
// and proves identity against the protected /props endpoint instead: a
// negative probe with a key that cannot be valid must be rejected (401/403),
// which proves the endpoint actually enforces authentication, and then the
// per-request key must succeed, which proves the peer holds this request's
// 256-bit secret. Only our freshly spawned child ever received that secret,
// and the child is re-observed alive after every probe. No prompt bytes are
// transmitted until all of this holds; a permissive stray server that
// answers any key aborts before the first inference byte.
//
// Network use is loopback only with proxy environment disabled and
// redirects rejected. Child stdout/stderr bytes are counted and discarded;
// prompts, completions and absolute paths never reach logs or public errors.
// Public failures are *download.Error values with safe Turkish messages, so
// the model manager can surface them without leaking request details.
package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"engelsiz/internal/download"
)

// Message is one OpenAI-style chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request carries trusted local integration inputs for one generation.
type Request struct {
	Binary      string
	ModelPath   string
	ContextSize int
	MaxTokens   int
	Messages    []Message
}

// Options configures the engine. GPULayers is an explicit acceleration
// opt-in: when greater than zero the engine first tries -ngl <n> and falls
// back to CPU only before any token is emitted. The default (zero or
// negative) is safe CPU only.
type Options struct {
	Logger         *log.Logger
	StartupTimeout time.Duration
	GPULayers      int
}

// Engine serializes generations through single-flight.
type Engine struct {
	mu            sync.Mutex
	logger        *log.Logger
	startupTimout time.Duration
	gpuLayers     int
	busy          bool
	closed        bool
	genDone       chan struct{}
	activeCancel  context.CancelFunc
}

var (
	// ErrBusy rejects concurrent generations; there is no queue.
	ErrBusy = &download.Error{Code: "busy", Message: "Başka bir istek sürüyor. Lütfen tekrar deneyin."}
	// ErrInvalid marks malformed generation requests.
	ErrInvalid = &download.Error{Code: "invalid_request", Message: "İstek geçersiz."}
	// ErrUnavailable marks engine and model runtime failures.
	ErrUnavailable = &download.Error{Code: "engine_unavailable", Message: "Model motoru şu anda kullanılamıyor. Tekrar deneyin."}
)

const (
	// maxStartup caps any readiness wait, regardless of Options.
	maxStartup = 120 * time.Second
	// defaultStartup applies when Options.StartupTimeout is unset.
	defaultStartup = 120 * time.Second
	// stopGrace bounds SIGTERM-to-KILL escalation per child.
	stopGrace = 2 * time.Second
	// reapGrace bounds the final reaping wait after KILL.
	reapGrace = 2 * time.Second
	// completionTimeout bounds one streaming inference call.
	completionTimeout = 5 * time.Minute
	// maxSSEBytes bounds the whole SSE response body.
	maxSSEBytes = 4 << 20
	// maxSSELine bounds one SSE line.
	maxSSELine = 256 * 1024
	// maxDeltaRunes bounds total emitted completion text.
	maxDeltaRunes = 131072
	// maxPromptRunes bounds total input text.
	maxPromptRunes = 32768
	// maxMessages bounds the message count.
	maxMessages = 64
	// maxMessageRunes bounds one message content.
	maxMessageRunes = 16384
)

// New builds an engine. A nil logger discards output.
func New(opts Options) *Engine {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Engine{logger: logger, startupTimout: opts.StartupTimeout, gpuLayers: opts.GPULayers}
}

// Close cancels any in-flight Generate and joins it, including while the
// child is still starting. Later Generate calls fail with engine_unavailable.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	cancel := e.activeCancel
	done := e.genDone
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// Generate runs one lazy child lifecycle and streams delta text to onDelta.
// Deltas only reach onDelta; status and prompt text never do.
func (e *Engine) Generate(ctx context.Context, req Request, onDelta func(string) error) error {
	if err := validateRequest(req); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrUnavailable
	}
	if e.busy {
		e.mu.Unlock()
		return ErrBusy
	}
	e.busy = true
	done := make(chan struct{})
	e.genDone = done
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.busy = false
		e.genDone = nil
		e.activeCancel = nil
		e.mu.Unlock()
		close(done)
	}()
	if err := checkModelFile(req.ModelPath); err != nil {
		return err
	}
	if !binaryUsable(req.Binary) {
		e.log("engine spawn: binary unavailable")
		return ErrUnavailable
	}
	genCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.activeCancel = cancel
	closed := e.closed
	e.mu.Unlock()
	defer cancel()
	if closed || e.isClosed() {
		return ErrUnavailable
	}
	if err := genCtx.Err(); err != nil {
		return err
	}
	contextSize := boundContextSize(req.ContextSize)
	maxTokens := boundMaxTokens(req.MaxTokens)
	attempts := []bool{false}
	if e.gpuLayers > 0 {
		attempts = []bool{true, false}
	}
	var lastErr error
	emitted := false
	for i, gpu := range attempts {
		if err := genCtx.Err(); err != nil {
			return contextCause(genCtx, ctx)
		}
		child, spawnErr := e.spawn(genCtx, req.Binary, req.ModelPath, contextSize, gpu)
		if spawnErr != nil {
			lastErr = spawnErr
			if ctxErr := genCtx.Err(); ctxErr != nil {
				return contextCause(genCtx, ctx)
			}
			if i+1 < len(attempts) {
				continue
			}
			return sanitize(spawnErr)
		}
		readyErr := e.waitReady(genCtx, child)
		if readyErr != nil {
			e.stopChild(child)
			if ctxErr := genCtx.Err(); ctxErr != nil {
				return contextCause(genCtx, ctx)
			}
			lastErr = readyErr
			if i+1 < len(attempts) {
				e.log("engine gpu attempt failed before token: retry cpu")
				continue
			}
			return sanitize(readyErr)
		}
		streamErr, cbErr, wrote := e.stream(genCtx, child, req.Messages, maxTokens, onDelta, emitted)
		if wrote {
			emitted = true
		}
		e.stopChild(child)
		if cbErr != nil {
			return cbErr
		}
		if streamErr == nil {
			return nil
		}
		if ctxErr := genCtx.Err(); ctxErr != nil {
			// A cancellation races with real inference failures; report
			// cancellation so callers do not retry a torn-down request.
			// If tokens were already emitted the caller already owns a
			// prefix, so cancellation still wins over a retry.
			_ = emitted
			return contextCause(genCtx, ctx)
		}
		lastErr = streamErr
		if emitted || i+1 >= len(attempts) {
			return sanitize(streamErr)
		}
		e.log("engine gpu attempt failed before token: retry cpu")
	}
	if lastErr != nil {
		return sanitize(lastErr)
	}
	return ErrUnavailable
}

func (e *Engine) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func (e *Engine) log(format string, args ...any) {
	e.logger.Printf(format, args...)
}

func contextCause(genCtx, caller context.Context) error {
	if err := caller.Err(); err != nil {
		return err
	}
	return genCtx.Err()
}

func sanitize(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var pub *download.Error
	if errors.As(err, &pub) {
		return pub
	}
	return ErrUnavailable
}

func validateRequest(req Request) error {
	if req.Binary == "" || req.ModelPath == "" || len(req.Messages) == 0 || len(req.Messages) > maxMessages {
		return ErrInvalid
	}
	total := 0
	for _, m := range req.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return ErrInvalid
		}
		n := len([]rune(m.Content))
		if n == 0 || n > maxMessageRunes {
			return ErrInvalid
		}
		total += n
		if total > maxPromptRunes {
			return ErrInvalid
		}
	}
	return nil
}

func checkModelFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrInvalid
	}
	return nil
}

func binaryUsable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

func boundContextSize(n int) int {
	if n <= 0 {
		return 2048
	}
	if n < 512 {
		return 512
	}
	if n > 8192 {
		return 8192
	}
	return n
}

func boundMaxTokens(n int) int {
	if n <= 0 {
		return 512
	}
	if n > 2048 {
		return 2048
	}
	return n
}

func (e *Engine) startupTimeout() time.Duration {
	t := e.startupTimout
	if t <= 0 {
		return defaultStartup
	}
	if t > maxStartup {
		return maxStartup
	}
	return t
}

// running tracks one spawned child and its sole Wait owner. Byte counters
// are atomic because the exec output copiers mutate them while stopChild
// reads them for logging.
type running struct {
	cmd        *exec.Cmd
	address    string
	port       int
	apiKey     string
	done       chan struct{}
	waitErr    error
	stdoutN    atomic.Int64
	stderrN    atomic.Int64
	gpuAttempt bool
}

type countWriter struct{ n *atomic.Int64 }

func (w countWriter) Write(p []byte) (int, error) {
	w.n.Add(int64(len(p)))
	return len(p), nil
}

func randomAPIKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func selectPort() (int, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return port, ln.Close()
}

// buildArgs assembles the explicit CPU baseline. The GPU attempt keeps the
// same loopback, port, key, parallelism, web UI and reasoning pins and only
// swaps the device offload flags.
func buildArgs(modelPath string, port int, apiKey string, contextSize, gpuLayers int, gpuAttempt bool) []string {
	ctxStr := strconv.Itoa(contextSize)
	args := []string{
		"-m", modelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--api-key", apiKey,
		"--parallel", "1",
		"--no-webui",
		"-c", ctxStr,
		"--reasoning", "off",
		"--reasoning-budget", "0",
		"--no-context-shift",
	}
	if gpuAttempt {
		args = append(args, "-ngl", strconv.Itoa(gpuLayers))
	} else {
		args = append(args, "--device", "none", "-ngl", "0")
	}
	return args
}

// cleanEnv drops proxy variables so neither the child nor loopback HTTP can
// be steered off host.
func cleanEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		upper := strings.ToUpper(key)
		switch upper {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (e *Engine) spawn(ctx context.Context, binary, modelPath string, contextSize int, gpuAttempt bool) (*running, error) {
	absBinary, err := filepath.Abs(binary)
	if err != nil {
		return nil, ErrUnavailable
	}
	absModel, err := filepath.Abs(modelPath)
	if err != nil {
		return nil, ErrInvalid
	}
	port, err := selectPort()
	if err != nil {
		e.log("engine spawn: loopback port unavailable")
		return nil, ErrUnavailable
	}
	apiKey, err := randomAPIKey()
	if err != nil {
		e.log("engine spawn: random key unavailable")
		return nil, ErrUnavailable
	}
	gpuLayers := e.gpuLayers
	if gpuLayers <= 0 {
		gpuLayers = 0
	}
	args := buildArgs(absModel, port, apiKey, contextSize, gpuLayers, gpuAttempt)
	cmd := exec.Command(absBinary, args...)
	cmd.Env = cleanEnv()
	cmd.Dir = filepath.Dir(absBinary)
	r := &running{
		cmd:        cmd,
		address:    net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		port:       port,
		apiKey:     apiKey,
		done:       make(chan struct{}),
		gpuAttempt: gpuAttempt,
	}
	cmd.Stdout = countWriter{n: &r.stdoutN}
	cmd.Stderr = countWriter{n: &r.stderrN}
	cmd.WaitDelay = 500 * time.Millisecond
	setupChild(cmd)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	startAllowed := !e.closed
	e.mu.Unlock()
	if !startAllowed {
		return nil, ErrUnavailable
	}
	if err := cmd.Start(); err != nil {
		e.log("engine spawn: child start failed")
		return nil, fmt.Errorf("engine spawn: %w", err)
	}
	go func() {
		r.waitErr = cmd.Wait()
		close(r.done)
	}()
	e.log("engine start: address=127.0.0.1 gpu=%v", gpuAttempt)
	return r, nil
}

// stopChild always terminates and reaps the child. It never logs child
// output content, only atomically read byte counts. The reap-timeout path
// reports the child as still alive; it never claims a reaping that did not
// happen.
func (e *Engine) stopChild(r *running) {
	if r == nil || r.cmd.Process == nil {
		return
	}
	out, errOut := r.stdoutN.Load(), r.stderrN.Load()
	select {
	case <-r.done:
		e.log("engine stop: already exited stdout_bytes=%d stderr_bytes=%d content=omitted", out, errOut)
		return
	default:
	}
	_ = terminate(r.cmd.Process)
	timer := time.NewTimer(stopGrace)
	defer timer.Stop()
	select {
	case <-r.done:
		out, errOut = r.stdoutN.Load(), r.stderrN.Load()
		e.log("engine stop: terminated stdout_bytes=%d stderr_bytes=%d content=omitted", out, errOut)
		return
	case <-timer.C:
	}
	_ = kill(r.cmd.Process)
	reap := time.NewTimer(reapGrace)
	defer reap.Stop()
	select {
	case <-r.done:
		out, errOut = r.stdoutN.Load(), r.stderrN.Load()
		e.log("engine stop: killed stdout_bytes=%d stderr_bytes=%d content=omitted", out, errOut)
	case <-reap.C:
		out, errOut = r.stdoutN.Load(), r.stderrN.Load()
		e.log("engine stop: reap timeout, child may still be alive stdout_bytes=%d stderr_bytes=%d content=omitted", out, errOut)
	}
}

func loopbackClient(address string, port int, apiKey string, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr != address || network != "tcp" {
				return nil, errors.New("engine: non-loopback dial refused")
			}
			return dialer.DialContext(ctx, "tcp4", address)
		},
		ResponseHeaderTimeout: timeout,
	}
	_ = port
	_ = apiKey
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// bogusAPIKey fails authentication by construction: real per-request keys
// are 64 lowercase hex characters, and the '-' below is not hex, so this
// probe key can never equal a genuine secret.
const bogusAPIKey = "engine-identity-probe-not-a-key"

// getStatus performs one bounded loopback GET and reports its status code.
// A stale child exit observed afterwards is reported as an error so callers
// never mistake a recycled listener for proof.
func getStatus(ctx context.Context, client *http.Client, r *running, path, key string) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://"+r.address+path, nil)
	if err != nil {
		return 0, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	status := resp.StatusCode
	resp.Body.Close()
	select {
	case <-r.done:
		return 0, fmt.Errorf("engine child exited during readiness probe: %v", r.waitErr)
	default:
	}
	return status, nil
}

// waitReady polls public /health for liveness until the deadline, then
// proves the listener belongs to our fresh child against the protected
// /props endpoint: the bogus key must be rejected (proving the endpoint
// enforces authentication) and the per-request key must succeed (proving the
// peer holds this request's secret, which only the spawned child received).
// The child is re-observed alive after every probe. No prompt is sent until
// this returns nil; stream() is the only prompt sender and runs after.
func (e *Engine) waitReady(ctx context.Context, r *running) error {
	deadline := time.Now().Add(e.startupTimeout())
	client := loopbackClient(r.address, r.port, r.apiKey, 2*time.Second)
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.done:
			return fmt.Errorf("engine child exited before ready: %v", r.waitErr)
		default:
		}
		status, err := getStatus(ctx, client, r, "/health", "")
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case <-r.done:
				return fmt.Errorf("engine child exited before ready: %v", r.waitErr)
			default:
			}
		} else {
			switch status {
			case http.StatusOK:
				if err := e.proveIdentity(ctx, client, r); err != nil {
					if errors.Is(err, errIdentity) || errors.Is(err, errPropFailure) {
						return err
					}
					// Anything else is still-starting noise; keep polling.
				} else {
					return nil
				}
			case http.StatusInternalServerError:
				// llama.cpp reports 503 while loading and 500 on error;
				// only the error state fails fast.
				return errors.New("engine readiness reported failure")
			}
		}
		if !time.Now().Before(deadline) {
			return errors.New("engine readiness timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.done:
			return fmt.Errorf("engine child exited before ready: %v", r.waitErr)
		case <-ticker.C:
		}
	}
}

var (
	// errIdentity marks a listener that cannot be our child: it rejects the
	// genuine key or accepts any key. Never retried against the same child
	// beyond the GPU/CPU attempt boundary, and never followed by a prompt.
	errIdentity = errors.New("engine listener identity rejected")
	// errPropFailure marks a protected-endpoint error state. Fails fast like
	// a 500 on /health.
	errPropFailure = errors.New("engine protected endpoint reported failure")
)

// proveIdentity runs the negative/positive /props pair. A nil return means
// the listener enforced authentication against the bogus key and accepted
// the per-request secret while our child stayed alive throughout.
func (e *Engine) proveIdentity(ctx context.Context, client *http.Client, r *running) error {
	negStatus, err := getStatus(ctx, client, r, "/props", bogusAPIKey)
	if err != nil {
		return err
	}
	switch negStatus {
	case http.StatusUnauthorized, http.StatusForbidden:
		// The endpoint enforces authentication; the positive probe decides.
	case http.StatusOK:
		// A permissive listener answers any key: this cannot be bound to
		// our secret, so no prompt may ever be sent to it.
		return errIdentity
	case http.StatusInternalServerError:
		return errPropFailure
	default:
		return fmt.Errorf("engine identity probe inconclusive: %d", negStatus)
	}
	posStatus, err := getStatus(ctx, client, r, "/props", r.apiKey)
	if err != nil {
		return err
	}
	switch posStatus {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return errIdentity
	case http.StatusInternalServerError:
		return errPropFailure
	default:
		return fmt.Errorf("engine identity confirmation inconclusive: %d", posStatus)
	}
}

type chatRequest struct {
	Messages        []Message `json:"messages"`
	Stream          bool      `json:"stream"`
	ReasoningEffort string    `json:"reasoning_effort"`
	NPredict        int       `json:"n_predict"`
}

type chatChunk struct {
	Choices []struct {
		Delta *struct {
			Content *string `json:"content"`
		} `json:"delta"`
		Text *string `json:"text"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// stream posts one bounded SSE inference call. It returns the stream error,
// a callback error if onDelta failed (which is returned unwrapped), and
// whether any token was emitted before the outcome.
func (e *Engine) stream(ctx context.Context, r *running, messages []Message, maxTokens int, onDelta func(string) error, alreadyEmitted bool) (streamErr error, callbackErr error, emitted bool) {
	emitted = alreadyEmitted
	payload, err := json.Marshal(chatRequest{
		Messages:        messages,
		Stream:          true,
		ReasoningEffort: "none",
		NPredict:        maxTokens,
	})
	if err != nil {
		return err, nil, emitted
	}
	streamCtx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()
	client := &http.Client{
		Transport:     loopbackClient(r.address, r.port, r.apiKey, 10*time.Second).Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, "http://"+r.address+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err, nil, emitted
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		select {
		case <-r.done:
			return fmt.Errorf("engine child exited during inference: %v", r.waitErr), nil, emitted
		default:
		}
		return err, nil, emitted
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("engine inference status %d", resp.StatusCode), nil, emitted
	}
	reader := bufio.NewReaderSize(io.LimitReader(resp.Body, maxSSEBytes), 64*1024)
	totalRunes := 0
	sawDone := false
	for {
		if err := streamCtx.Err(); err != nil {
			return err, nil, emitted
		}
		select {
		case <-r.done:
			// The child died mid-stream. If it already closed the body with
			// [DONE] the loop below exits first; otherwise this is a
			// truncation, not a usable prefix.
			if !sawDone {
				return fmt.Errorf("engine child exited during inference: %v", r.waitErr), nil, emitted
			}
		default:
		}
		line, err := readSSELine(reader, maxSSELine)
		if err != nil {
			return err, nil, emitted
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			return errors.New("engine malformed SSE envelope"), nil, emitted
		}
		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		if data == "" {
			continue
		}
		var chunk chatChunk
		dec := json.NewDecoder(strings.NewReader(data))
		if err := dec.Decode(&chunk); err != nil {
			return fmt.Errorf("engine malformed SSE payload: %w", err), nil, emitted
		}
		if chunk.Error != nil {
			return errors.New("engine inference reported failure"), nil, emitted
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		var text string
		for _, ch := range chunk.Choices {
			if ch.Delta != nil && ch.Delta.Content != nil {
				text += *ch.Delta.Content
			} else if ch.Text != nil {
				text += *ch.Text
			}
		}
		if text == "" {
			continue
		}
		totalRunes += len([]rune(text))
		if totalRunes > maxDeltaRunes {
			return errors.New("engine completion exceeded bound"), nil, emitted
		}
		emitted = true
		if onDelta != nil {
			if err := onDelta(text); err != nil {
				return nil, err, emitted
			}
		}
	}
	if !sawDone {
		return errors.New("engine stream ended without completion"), nil, emitted
	}
	return nil, nil, emitted
}

func readSSELine(reader *bufio.Reader, max int) (string, error) {
	var out []byte
	for {
		frag, err := reader.ReadSlice('\n')
		out = append(out, frag...)
		if len(out) > max {
			return "", errors.New("engine SSE line exceeded bound")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(out) > 0 {
				// A final line without newline is still parseable; the
				// missing [DONE] is reported by the caller as truncation.
				return string(out), nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return "", errors.New("engine stream truncated")
			}
			return "", err
		}
		return string(out), nil
	}
}
