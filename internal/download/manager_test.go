package download

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"engelsiz/internal/database"
)

func testManager(t *testing.T, opts Options) (*Manager, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := database.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	m, e := New(db, filepath.Join(dir, "downloads"), opts)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close(); db.Close() })
	return m, db, dir
}
func descriptor(url string, total *int64) Descriptor {
	return Descriptor{Provider: "example", ProviderPackageID: "package", Title: "Generic file", Variant: "v1", FileName: "payload.bin", SourceURL: url, TotalBytes: total}
}
func waitStatus(t *testing.T, m *Manager, id string, statuses ...string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, e := m.Get(id)
		if e != nil {
			t.Fatal(e)
		}
		for _, want := range statuses {
			if s.Status == want {
				return s
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, _ := m.Get(id)
	t.Fatalf("waiting for %v: %+v", statuses, s)
	return s
}
func seed(t *testing.T, m *Manager, url string, total *int64, etag string, payload []byte) record {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r := record{Snapshot: Snapshot{ID: "0123456789abcdef0123456789abcdef", Provider: "example", ProviderPackageID: "package", Title: "Generic file", Variant: "v1", FileName: "payload.bin", Status: "paused", TotalBytes: total, DownloadedBytes: int64(len(payload)), CreatedAt: now, UpdatedAt: now}, SourceURL: url, ETag: etag}
	r.TempPath = m.path(r.ID)
	if e := m.insert(&r); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(r.TempPath, payload, 0600); e != nil {
		t.Fatal(e)
	}
	return r
}
func waitPartial(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, e := os.Stat(path); e == nil && info.Size() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("partial bytes were not written")
}
func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(b, want) {
		t.Fatalf("file mismatch len=%d want=%d err=%v", len(b), len(want), e)
	}
}

func TestRangeContinuationAndSafeFallbacks(t *testing.T) {
	payload := []byte(strings.Repeat("a generic payload\n", 2048))
	total := int64(len(payload))
	prefix := payload[:1234]
	for _, mode := range []string{"valid", "ignored", "changed", "changed-total", "no-validator", "416", "bad-range", "unsolicited", "encoding", "short"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			replacement := payload
			if mode == "changed" || mode == "changed-total" {
				replacement = bytes.Repeat([]byte("z"), len(payload))
				if mode == "changed-total" {
					replacement = append(replacement, []byte("extra")...)
				}
			}
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				isRange := r.Header.Get("Range") != ""
				if r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("missing identity encoding")
				}
				if mode == "no-validator" && isRange {
					t.Error("Range without validator")
				}
				if isRange && r.Header.Get("If-Range") != `"old"` {
					t.Error("missing If-Range")
				}
				if isRange && mode == "416" {
					w.WriteHeader(416)
					return
				}
				if mode == "short" {
					w.Header().Set("Content-Length", fmt.Sprint(total))
					w.Write(payload[:len(payload)/2])
					return
				}
				if mode == "encoding" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.Header().Set("ETag", `"old"`)
				if mode == "changed" || mode == "changed-total" {
					w.Header().Set("ETag", `"new"`)
				}
				if (isRange && mode != "ignored") || mode == "unsolicited" {
					start := len(prefix)
					if mode == "bad-range" {
						start++
					}
					if mode == "unsolicited" && n == 1 {
						start = 0
					}
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(replacement)-1, len(replacement)))
					w.WriteHeader(206)
					w.Write(replacement[start:])
					return
				}
				w.Write(replacement)
			}))
			defer source.Close()
			m, _, _ := testManager(t, Options{})
			etag := `"old"`
			if mode == "no-validator" || mode == "unsolicited" {
				etag = ""
			}
			rec := seed(t, m, source.URL, &total, etag, prefix)
			if _, e := m.Control(rec.ID, "resume"); e != nil {
				t.Fatal(e)
			}
			s := waitStatus(t, m, rec.ID, "completed", "failed")
			failed := mode == "bad-range" || mode == "unsolicited" || mode == "encoding" || mode == "short"
			if failed {
				if s.Status != "failed" {
					t.Fatalf("unsafe completion: %+v", s)
				}
				if mode != "short" {
					assertFile(t, rec.TempPath, prefix)
				}
			} else {
				if s.Status != "completed" {
					t.Fatalf("transfer: %+v", s)
				}
				assertFile(t, rec.TempPath, replacement)
				if s.DownloadedBytes != int64(len(replacement)) || s.TotalBytes == nil || *s.TotalBytes != int64(len(replacement)) || s.Percentage == nil || *s.Percentage != 100 {
					t.Fatalf("progress: %+v", s)
				}
			}
			if calls.Load() > 2 {
				t.Fatal("unbounded restart")
			}
		})
	}
}
func TestUnknownTotalAndShortBody(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			payload := bytes.Repeat([]byte("x"), 4096)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if short {
					w.Header().Set("Content-Length", "8192")
				}
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				w.Write(payload)
			}))
			defer source.Close()
			m, _, _ := testManager(t, Options{})
			s, e := m.Create(descriptor(source.URL, nil))
			if e != nil {
				t.Fatal(e)
			}
			s = waitStatus(t, m, s.ID, "failed", "completed")
			want := "completed"
			if short {
				want = "failed"
			}
			if s.Status != want || s.DownloadedBytes != 4096 {
				t.Fatalf("snapshot: %+v", s)
			}
			if !short && (s.TotalBytes == nil || *s.TotalBytes != 4096) {
				t.Fatal("unknown EOF total not established")
			}
		})
	}
}
func TestSpacePreflightAndRestartPreservesPartial(t *testing.T) {
	total := int64(100)
	var free atomic.Uint64
	free.Store(10)
	m, _, _ := testManager(t, Options{Available: func(string) (uint64, error) { return free.Load(), nil }})
	if _, e := m.Create(descriptor("http://example.test/file", &total)); !errors.Is(e, ErrSpace) {
		t.Fatalf("preflight: %v", e)
	}
	entries, _ := os.ReadDir(m.dir)
	if len(entries) != 0 {
		t.Fatal("preflight wrote file")
	}
	var rangeSeen atomic.Bool
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeSeen.Store(r.Header.Get("Range") == "bytes=80-")
		w.Header().Set("Content-Length", "100")
		w.Write(bytes.Repeat([]byte("n"), 100))
	}))
	defer source.Close()
	free.Store(25)
	prefix := bytes.Repeat([]byte("o"), 80)
	rec := seed(t, m, source.URL, &total, `"v"`, prefix)
	if _, e := m.Control(rec.ID, "resume"); e != nil {
		t.Fatal(e)
	}
	s := waitStatus(t, m, rec.ID, "failed")
	if s.Error == nil || s.Error.Code != "insufficient_space" || !rangeSeen.Load() {
		t.Fatalf("restart space: %+v", s)
	}
	assertFile(t, rec.TempPath, prefix)
}
func TestRemainingCapacityAndInjectedDiskFullRetry(t *testing.T) {
	payload := bytes.Repeat([]byte("data"), 10000)
	total := int64(len(payload))
	var first atomic.Bool
	first.Store(true)
	var neededOffset atomic.Int64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"stable"`)
		start := 0
		if r.Header.Get("Range") != "" {
			fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start)
			neededOffset.Store(int64(start))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(206)
		}
		w.Write(payload[start:])
	}))
	defer source.Close()
	var free atomic.Uint64
	free.Store(uint64(total))
	m, _, _ := testManager(t, Options{Available: func(string) (uint64, error) { return free.Load(), nil }, Write: func(f *os.File, b []byte) (int, error) {
		if first.CompareAndSwap(true, false) {
			n, e := f.Write(b[:1234])
			if e != nil {
				return n, e
			}
			return n, syscall.ENOSPC
		}
		return f.Write(b)
	}})
	s, e := m.Create(descriptor(source.URL, &total))
	if e != nil {
		t.Fatal(e)
	}
	s = waitStatus(t, m, s.ID, "failed")
	if s.DownloadedBytes != 1234 || s.Error == nil || s.Error.Code != "insufficient_space" {
		t.Fatalf("disk full: %+v", s)
	}
	assertFile(t, m.path(s.ID), payload[:1234])
	free.Store(uint64(total - 1234))
	if _, e = m.Control(s.ID, "retry"); e != nil {
		t.Fatal(e)
	}
	s = waitStatus(t, m, s.ID, "completed", "failed")
	if s.Status != "completed" || neededOffset.Load() != 1234 {
		t.Fatalf("retry: %+v offset=%d", s, neededOffset.Load())
	}
	assertFile(t, m.path(s.ID), payload)
}
func TestUnavailableCapacityFailsClosed(t *testing.T) {
	m, _, _ := testManager(t, Options{Available: func(string) (uint64, error) { return 0, errors.New("capacity unavailable") }})
	if _, e := m.Create(descriptor("http://example.test/x", nil)); !errors.Is(e, ErrSpace) {
		t.Fatalf("capacity: %v", e)
	}
}
func TestReopenReconcilesAndResumesWithoutAutomaticTransfer(t *testing.T) {
	payload := bytes.Repeat([]byte("restart"), 1000)
	total := int64(len(payload))
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 500-%d/%d", len(payload)-1, len(payload)))
		if r.Header.Get("Range") != "bytes=500-" {
			t.Error("did not reconcile actual length")
		}
		w.WriteHeader(206)
		w.Write(payload[500:])
	}))
	defer source.Close()
	m, db, dir := testManager(t, Options{})
	rec := seed(t, m, source.URL, &total, `"v"`, payload[:500])
	rec.DownloadedBytes = 1
	rec.Status = "downloading"
	if e := m.save(&rec); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO settings(key,value) VALUES('preserved','yes')`); e != nil {
		t.Fatal(e)
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	if e := db.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := database.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	m2, e := New(reopened, filepath.Join(dir, "downloads"), Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer m2.Close()
	s, e := m2.Get(rec.ID)
	if e != nil || s.Status != "paused" || s.DownloadedBytes != 500 || calls.Load() != 0 {
		t.Fatalf("recovery %+v %v calls=%d", s, e, calls.Load())
	}
	var value string
	if e = reopened.QueryRow(`SELECT value FROM settings WHERE key='preserved'`).Scan(&value); e != nil || value != "yes" {
		t.Fatal("settings lost")
	}
	if _, e = m2.Control(rec.ID, "resume"); e != nil {
		t.Fatal(e)
	}
	waitStatus(t, m2, rec.ID, "completed")
	assertFile(t, rec.TempPath, payload)
}
func TestPauseConcurrentControlsCancelAndShutdown(t *testing.T) {
	started := make(chan struct{}, 10)
	payload := bytes.Repeat([]byte("a"), 4096)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v"`)
		w.Header().Set("Content-Length", "1000000")
		w.Write(payload)
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer source.Close()
	m, _, _ := testManager(t, Options{})
	s, e := m.Create(descriptor(source.URL, nil))
	if e != nil {
		t.Fatal(e)
	}
	<-started
	waitPartial(t, m.path(s.ID))
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			if _, e := m.Control(s.ID, "pause"); e != nil {
				t.Errorf("pause %v", e)
			}
		})
	}
	group.Wait()
	s, e = m.Get(s.ID)
	if e != nil || s.Status != "paused" || s.DownloadedBytes == 0 {
		t.Fatalf("paused %+v %v", s, e)
	}
	if _, e = m.Create(descriptor(source.URL, nil)); !errors.Is(e, ErrExists) {
		t.Fatalf("duplicate %v", e)
	}
	for i := 0; i < 8; i++ {
		group.Go(func() {
			if _, e := m.Control(s.ID, "resume"); e != nil {
				t.Errorf("resume %v", e)
			}
		})
	}
	group.Wait()
	<-started
	if _, e = m.Control(s.ID, "cancel"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(m.path(s.ID)); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("cancel file %v", e)
	}
	if _, e = m.Control(s.ID, "cancel"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Control(s.ID, "resume"); !errors.Is(e, ErrState) {
		t.Fatalf("resume cancelled %v", e)
	}
	d := descriptor(source.URL, nil)
	d.ProviderPackageID = "second"
	s, e = m.Create(d)
	if e != nil {
		t.Fatal(e)
	}
	<-started
	waitPartial(t, m.path(s.ID))
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	r, e := m.get(s.ID)
	if e != nil || r.Status != "paused" {
		t.Fatalf("shutdown %+v %v", r, e)
	}
	info, e := os.Stat(m.path(s.ID))
	if e != nil || info.Size() == 0 {
		t.Fatalf("shutdown partial %v", e)
	}
}
func TestUnsafePathsAndMissingPartialRecovery(t *testing.T) {
	for _, name := range []string{"../bad", "a\\b", "C:bad", "CON.zim", "a/b", "x.", "\x00"} {
		if SafeFilename(name) {
			t.Fatalf("unsafe filename %q", name)
		}
	}
	m, _, _ := testManager(t, Options{})
	rec := seed(t, m, "http://example.test/x", nil, "", []byte("partial"))
	if e := os.Remove(rec.TempPath); e != nil {
		t.Fatal(e)
	}
	if e := m.recover(); e != nil {
		t.Fatal(e)
	}
	s, _ := m.Get(rec.ID)
	if s.DownloadedBytes != 0 {
		t.Fatal("missing partial did not reconcile")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("keep"), 0600)
	if e := os.Symlink(outside, rec.TempPath); e != nil {
		t.Skip(e)
	}
	if _, e := m.Control(rec.ID, "cancel"); !errors.Is(e, ErrStorage) {
		t.Fatalf("symlink cancel %v", e)
	}
	assertFile(t, outside, []byte("keep"))
	if e := m.recover(); e != nil {
		t.Fatal(e)
	}
	s, _ = m.Get(rec.ID)
	if s.Status != "failed" || s.Error == nil {
		t.Fatalf("unsafe recovery %+v", s)
	}
}
func TestNullableStateAndPersistenceErrors(t *testing.T) {
	m, db, _ := testManager(t, Options{})
	rec := seed(t, m, "http://example.test/x", nil, "", nil)
	rec.Status = "failed"
	rec.Error = ErrSpace
	if e := m.save(&rec); e != nil {
		t.Fatal(e)
	}
	got, e := m.get(rec.ID)
	if e != nil || got.TotalBytes != nil || got.Error == nil || got.Error.Code != "insufficient_space" {
		t.Fatalf("nullable reload %+v %v", got, e)
	}
	if _, e := db.Exec(`CREATE TRIGGER fail_download_update BEFORE UPDATE ON downloads BEGIN SELECT RAISE(FAIL,'injected storage failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Control(rec.ID, "retry"); !errors.Is(e, ErrStorage) {
		t.Fatalf("persistence failure reported success: %v", e)
	}
}
func TestStreamingClientAndRealCapacity(t *testing.T) {
	m, _, _ := testManager(t, Options{})
	if m.client.Timeout != 0 {
		t.Fatal("streaming has overall timeout")
	}
	if free, e := diskAvailable(m.dir); e != nil || free == 0 {
		t.Fatalf("capacity %d %v", free, e)
	}
}

func TestChangedChunkedRestartChecksNewFullSize(t *testing.T) {
	oldSize := int64(100)
	newSize := int64(150)
	prefix := bytes.Repeat([]byte("o"), 80)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"new"`)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 80-149/150")
			w.WriteHeader(206)
			w.Write(bytes.Repeat([]byte("n"), 70))
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		w.Write(bytes.Repeat([]byte("n"), int(newSize)))
	}))
	defer source.Close()
	m, _, _ := testManager(t, Options{Available: func(string) (uint64, error) { return 100, nil }})
	rec := seed(t, m, source.URL, &oldSize, `"old"`, prefix)
	if _, e := m.Control(rec.ID, "resume"); e != nil {
		t.Fatal(e)
	}
	s := waitStatus(t, m, rec.ID, "failed")
	if s.Error == nil || s.Error.Code != "insufficient_space" {
		t.Fatalf("new full-size capacity not checked: %+v", s)
	}
	assertFile(t, rec.TempPath, prefix)
}
func TestLastModifiedAndWeakValidator(t *testing.T) {
	for _, strongDate := range []bool{false, true} {
		t.Run(fmt.Sprint(strongDate), func(t *testing.T) {
			const date = "Wed, 21 Oct 2015 07:28:00 GMT"
			payload := bytes.Repeat([]byte("x"), 1024)
			total := int64(len(payload))
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strongDate {
					if r.Header.Get("If-Range") != date || r.Header.Get("Range") != "bytes=100-" {
						t.Error("usable date not sent")
					}
					w.Header().Set("Last-Modified", date)
					w.Header().Set("Content-Range", "bytes 100-1023/1024")
					w.WriteHeader(206)
					w.Write(payload[100:])
				} else {
					if r.Header.Get("Range") != "" {
						t.Error("weak ETag resumed")
					}
					w.Write(payload)
				}
			}))
			defer source.Close()
			m, _, _ := testManager(t, Options{})
			rec := seed(t, m, source.URL, &total, `W/"weak"`, payload[:100])
			if strongDate {
				rec.LastModified = date
				if e := m.save(&rec); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := m.Control(rec.ID, "resume"); e != nil {
				t.Fatal(e)
			}
			waitStatus(t, m, rec.ID, "completed")
			assertFile(t, rec.TempPath, payload)
		})
	}
	for _, bad := range []string{`W/"a"`, `"a b"`, `"a"b"`, "\"a\nb\"", "missing-quotes"} {
		if usableETag(bad) {
			t.Fatalf("unusable ETag accepted: %q", bad)
		}
	}
}
