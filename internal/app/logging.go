package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const logMaxBytes int64 = 1024 * 1024
const logBackups = 2
const logRecordBytes = 8 * 1024

// rotatingLog owns only the fixed log names below its pinned directory. Errors
// are retained for Close because log.Logger cannot report writer failures.
type rotatingLog struct {
	mu          sync.Mutex
	root        *os.Root
	name        string
	file        *os.File
	size, limit int64
	backups     int
	err         error
	closed      bool
}

func initLogger(path string) (*log.Logger, io.Closer, error) {
	writer, err := openRotatingLog(path, logMaxBytes, logBackups)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	return log.New(io.MultiWriter(writer, os.Stderr), "", log.LstdFlags), writer, nil
}

func openRotatingLog(path string, limit int64, backups int) (*rotatingLog, error) {
	if limit <= 0 || backups < 1 {
		return nil, errors.New("invalid log retention")
	}
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe log directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	w := &rotatingLog{root: root, name: filepath.Base(path), limit: limit, backups: backups}
	ok := false
	defer func() {
		if !ok {
			_ = w.Close()
		}
	}()
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		return nil, errors.New("log directory changed")
	}
	if err = root.Chmod(".", 0700); err != nil {
		return nil, err
	}
	// Bound an existing pre-rotation log while retaining its most recent bytes.
	for i := 0; i <= backups; i++ {
		name := w.slot(i)
		info, e := w.regular(name)
		if e != nil {
			return nil, e
		}
		if info == nil {
			continue
		}
		file, e := root.OpenFile(name, os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		actual, e := file.Stat()
		if e == nil && !os.SameFile(info, actual) {
			e = errors.New("log file changed")
		}
		if e == nil {
			e = file.Chmod(0600)
		}
		if e == nil && actual.Size() > limit {
			tail := make([]byte, int(limit))
			_, e = file.ReadAt(tail, actual.Size()-limit)
			if e == nil {
				e = file.Truncate(0)
			}
			if e == nil {
				_, e = file.WriteAt(tail, 0)
			}
		}
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return nil, errors.Join(e, closeErr)
		}
	}
	if err = w.open(); err != nil {
		return nil, err
	}
	if w.size >= limit {
		if err = w.rotate(); err != nil {
			return nil, err
		}
	}
	ok = true
	return w, nil
}
func (w *rotatingLog) slot(i int) string {
	if i == 0 {
		return w.name
	}
	return w.name + "." + strconv.Itoa(i)
}
func (w *rotatingLog) regular(name string) (os.FileInfo, error) {
	info, err := w.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("unsafe log destination")
	}
	return info, nil
}
func (w *rotatingLog) open() error {
	before, err := w.regular(w.name)
	if err != nil {
		return err
	}
	f, err := w.root.OpenFile(w.name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	actual, err := f.Stat()
	if err == nil && (!actual.Mode().IsRegular() || (before != nil && !os.SameFile(before, actual))) {
		err = errors.New("log destination changed")
	}
	if err == nil {
		published, checkErr := w.regular(w.name)
		err = checkErr
		if err == nil && (published == nil || !os.SameFile(published, actual)) {
			err = errors.New("log destination changed during open")
		}
	}
	if err == nil {
		err = f.Chmod(0600)
	}
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = actual.Size()
	return nil
}
func (w *rotatingLog) rotate() error {
	// Validate every destination before the first mutation. A hostile backup must
	// not be overwritten or followed by the rotation mechanism.
	for i := 0; i <= w.backups; i++ {
		if _, err := w.regular(w.slot(i)); err != nil {
			return err
		}
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	if err := w.root.Remove(w.slot(w.backups)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := w.backups - 1; i >= 0; i-- {
		if err := w.root.Rename(w.slot(i), w.slot(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return w.open()
}
func (w *rotatingLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.err != nil {
		return 0, w.err
	}
	original := len(p)
	maximum := int64(logRecordBytes)
	if maximum > w.limit {
		maximum = w.limit
	}
	if int64(len(p)) > maximum {
		marker := []byte(" [truncated]\n")
		if int64(len(marker)) >= maximum {
			p = p[:int(maximum)]
		} else {
			bounded := make([]byte, 0, int(maximum))
			bounded = append(bounded, p[:int(maximum)-len(marker)]...)
			p = append(bounded, marker...)
		}
	}
	before, err := w.regular(w.name)
	if err == nil {
		actual, e := w.file.Stat()
		err = e
		if err == nil && (before == nil || !os.SameFile(before, actual)) {
			err = errors.New("log destination changed")
		}
	}
	if err == nil && w.size+int64(len(p)) > w.limit {
		err = w.rotate()
	}
	if err != nil {
		w.err = err
		return 0, err
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
		return n, err
	}
	return original, nil
}
func (w *rotatingLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.err
	}
	w.closed = true
	if w.file != nil {
		w.err = errors.Join(w.err, w.file.Close())
	}
	if w.root != nil {
		w.err = errors.Join(w.err, w.root.Close())
	}
	return w.err
}
