package translation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	// defaultTranslateTimeout covers cold model load on modest hardware.
	defaultTranslateTimeout = 120 * time.Second
	// maxTranslateResponse bounds a single worker reply line in bytes.
	maxTranslateResponse = 1 << 20
	// maxTranslatedRunes bounds translated output in runes (translations may
	// legitimately exceed the input length).
	maxTranslatedRunes = 20000
)

// workerRequest is the only message the Go side ever sends. argv[1] already
// carries the absolute models root, so no paths travel inside requests.
type workerRequest struct {
	Cmd            string `json:"cmd"`
	SourceLanguage string `json:"source_language"`
	TargetLanguage string `json:"target_language"`
	Text           string `json:"text"`
}

// workerReply is the only accepted success shape. The worker error shape is
// accepted and mapped to a generic engine failure without surfacing details.
type workerReply struct {
	TranslatedText *string `json:"translated_text"`
	Error          *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type workerProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

// resolveBinary locates the frozen worker without a shell. An explicit
// override (tests) wins, then ENGELSIZ_TRANSLATION_BINARY (development only),
// then the application-relative runtime/translation directory.
func resolveBinary(override string) string {
	if override != "" {
		return override
	}
	if dev := os.Getenv("ENGELSIZ_TRANSLATION_BINARY"); dev != "" {
		return dev
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		exe = resolved
	}
	name := "engelsiz-translate"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), "runtime", "translation", name)
}

// binaryAvailable reports whether path is a usable executable file.
func binaryAvailable(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode().Perm()&0o111 != 0
}

// startWorker launches the persistent worker with explicit argv only:
// argv[1] is the absolute models root. Stderr is discarded so source text can
// never reach logs through the child. There is no startup greeting; the first
// read happens per request.
func startWorker(binary, modelsRoot string) (*workerProc, error) {
	if !binaryAvailable(binary) {
		return nil, ErrEngine
	}
	abs, err := filepath.Abs(modelsRoot)
	if err != nil {
		return nil, err
	}
	// No shell: explicit argv, direct exec.
	cmd := exec.Command(binary, abs)
	cmd.Dir = filepath.Dir(abs)
	cmd.Stderr = io.Discard
	cmd.Stdin = nil
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, ErrEngine
	}
	return &workerProc{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdoutPipe, 64*1024)}, nil
}

// killWorker terminates and reaps the child, then clears it so the next
// request starts fresh. It never blocks on the child longer than necessary.
func killWorker(p *workerProc) {
	if p == nil {
		return
	}
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
}

// readLineBounded reads one newline-terminated reply with a hard byte cap.
func readLineBounded(r *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		frag, err := r.ReadSlice('\n')
		out = append(out, frag...)
		if len(out) > max {
			return nil, errors.New("reply exceeds bound")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

// roundTrip performs one request/response exchange. The caller kills and
// clears the worker on any error.
func roundTrip(ctx context.Context, p *workerProc, payload []byte, timeout time.Duration) (string, error) {
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
			done <- result{err: err}
			return
		}
		line, err := readLineBounded(p.stdout, maxTranslateResponse)
		if err != nil {
			done <- result{err: err}
			return
		}
		var reply workerReply
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&reply); err != nil {
			done <- result{err: err}
			return
		}
		// Exactly one JSON object per line: trailing data is malformed.
		if dec.Decode(&struct{}{}) != io.EOF {
			done <- result{err: errors.New("trailing worker output")}
			return
		}
		if reply.Error != nil || reply.TranslatedText == nil {
			done <- result{err: errors.New("worker reported failure")}
			return
		}
		done <- result{text: *reply.TranslatedText}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", ErrEngine
	case r := <-done:
		if r.err != nil {
			return "", ErrEngine
		}
		return r.text, nil
	}
}

// gate serializes translation work: one worker, one in-flight request.
// Acquisition is context-aware so shutdown never wedges behind a queue.
type gate struct {
	ch chan struct{}
}

func newGate() gate { return gate{ch: make(chan struct{}, 1)} }

func (g gate) acquire(ctx context.Context) (release func(), err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case g.ch <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.ch }) }, nil
	}
}

// translateGate is a package-level single global gate shared by every Service
// in the process, so concurrent services cannot spawn unbounded workers.
var translateGate = newGate()
