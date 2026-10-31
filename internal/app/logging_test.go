package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestRotatingLogThresholdReopenAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engelsiz.log")
	w, err := openRotatingLog(path, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)} {
		if _, err = w.Write([]byte(entry)); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{path: strings.Repeat("d", 40), path + ".1": strings.Repeat("c", 40), path + ".2": strings.Repeat("b", 40)} {
		body, e := os.ReadFile(name)
		if e != nil || string(body) != want {
			t.Fatalf("%s: %q %v", name, body, e)
		}
		info, e := os.Stat(name)
		if e != nil {
			t.Fatal(e)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %v", info.Mode())
		}
	}
	if _, e := os.Stat(path + ".3"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("unexpected unbounded backup", e)
	}
	w, err = openRotatingLog(path, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal("close must be idempotent", err)
	}
	if _, err = w.Write([]byte("closed")); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != strings.Repeat("d", 40)+"tail" {
		t.Fatalf("append reopen %q %v", body, err)
	}
}
func TestRotatingLogBoundsPreexistingHistoryAndOversizedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engelsiz.log")
	for _, name := range []string{path, path + ".1", path + ".2"} {
		if err := os.WriteFile(name, []byte(strings.Repeat("old-", 100)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	w, err := openRotatingLog(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	original := strings.Repeat("large", 100)
	n, err := w.Write([]byte(original))
	if err != nil || n != len(original) {
		t.Fatalf("oversized write: %d %v", n, err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, e := os.Stat(name)
		if e != nil || info.Size() > 100 {
			t.Fatalf("unbounded %s: %v %v", name, info, e)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(string(body), " [truncated]\n") {
		t.Fatalf("truncation %q %v", body, err)
	}
}
func TestRotatingLogRejectsNonregularAndSymlinkSlots(t *testing.T) {
	for _, slot := range []string{"engelsiz.log", "engelsiz.log.1", "engelsiz.log.2"} {
		t.Run(slot, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, slot), 0700); err != nil {
				t.Fatal(err)
			}
			if w, e := openRotatingLog(filepath.Join(root, "engelsiz.log"), 100, 2); e == nil {
				w.Close()
				t.Fatal("directory accepted")
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("preserved"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "engelsiz.log")); err != nil {
		t.Fatal(err)
	}
	if w, e := openRotatingLog(filepath.Join(root, "engelsiz.log"), 100, 2); e == nil {
		w.Close()
		t.Fatal("symlink accepted")
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "preserved" {
		t.Fatal("outside modified")
	}
	parent := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, parent); err != nil {
		t.Fatal(err)
	}
	if w, e := openRotatingLog(filepath.Join(parent, "other.log"), 100, 2); e == nil {
		w.Close()
		t.Fatal("symlink directory accepted")
	}
}
func TestRotatingLogConcurrentWritesRemainBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engelsiz.log")
	w, err := openRotatingLog(path, 256, 2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				if _, e := fmt.Fprintf(w, "worker=%02d item=%02d\n", i, j); e != nil {
					t.Error(e)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, e := os.Stat(name)
		if e != nil || info.Size() > 256 {
			t.Fatalf("unbounded concurrent log: %v %v", info, e)
		}
		body, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
			if !strings.HasPrefix(line, "worker=") || len(line) != 17 {
				t.Fatalf("interleaved record: %q", line)
			}
		}
	}
}
func TestRotatingLogLatchesRotationFailureAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engelsiz.log")
	w, err := openRotatingLog(path, 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("first entry")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path+".1", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("next entry")); err == nil {
		t.Fatal("rotation failure ignored")
	}
	if _, err = w.Write([]byte("more")); err == nil {
		t.Fatal("latched failure ignored")
	}
	if err = w.Close(); err == nil {
		t.Fatal("close lost logging failure")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "first entry" {
		t.Fatalf("original log corrupted: %q %v", body, err)
	}
}
func TestRotatingLogDetectsReplacedCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engelsiz.log")
	w, err := openRotatingLog(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".moved"); err != nil {
		w.Close()
		t.Skip("open-file rename unavailable")
	}
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("unsafe")); err == nil {
		t.Fatal("replaced file accepted")
	}
	if err = w.Close(); err == nil {
		t.Fatal("replacement error lost")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "replacement" {
		t.Fatal("replacement mutated")
	}
}
