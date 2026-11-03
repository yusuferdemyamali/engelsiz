package maps_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"engelsiz/internal/api"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/providers/maps"
)

func setup(t *testing.T, run func(context.Context, string, ...string) error) (*maps.Service, string, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 127)
	copy(header, "PMTiles")
	header[7] = 3
	header[99] = 1
	header[101] = 15
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-126/1000")
		w.WriteHeader(206)
		_, _ = w.Write(header)
	}))
	s, err := maps.New(db, dir, maps.Options{Source: server.URL, Binary: "fake", HTTPClient: server.Client(), Run: run})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, func() { s.Close(); server.Close(); db.Close() }
}
func fixtureRun(_ context.Context, _ string, args ...string) error {
	if args[0] == "verify" {
		return nil
	}
	h := make([]byte, 1024)
	copy(h, "PMTiles")
	h[7] = 3
	h[99] = 1
	h[101] = 15
	return os.WriteFile(args[2], h, 0600)
}
func eskisehir(t *testing.T, s *maps.Service) maps.Region {
	t.Helper()
	items, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "eskisehir" {
			return item
		}
	}
	t.Fatal("eskisehir missing from catalog")
	return maps.Region{}
}
func waitFor(t *testing.T, s *maps.Service, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if eskisehir(t, s).Status == status {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatalf("want %s got %+v", status, eskisehir(t, s))
}
func TestInstallRestartRangeAndDelete(t *testing.T) {
	s, dir, cleanup := setup(t, fixtureRun)
	defer cleanup()
	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "ready")
	handler := api.MapsHandler(s)
	for _, tc := range []struct {
		rangeValue   string
		status       int
		contentRange string
		length       string
	}{{"bytes=0-9", 206, "bytes 0-9/1024", "10"}, {"bytes=9999-", 416, "bytes */1024", ""}} {
		r := httptest.NewRequest("GET", "/api/maps/eskisehir/file", nil)
		r.Header.Set("Range", tc.rangeValue)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != tc.status || w.Header().Get("Content-Range") != tc.contentRange || (tc.length != "" && w.Header().Get("Content-Length") != tc.length) {
			t.Fatalf("range: %d %v", w.Code, w.Header())
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	restarted, err := maps.New(db, dir, maps.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	items, err := restarted.List()
	if err != nil || eskisehir(t, restarted).Status != "ready" {
		t.Fatalf("restart: %+v %v", items, err)
	}
	if err := restarted.Delete("eskisehir"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "content/maps/eskisehir.pmtiles")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file remains: %v", err)
	}
	if eskisehir(t, restarted).Status != "available" {
		t.Fatalf("metadata remains: %+v", items)
	}
}
func TestCancelCleansPartialAndDuplicateRejected(t *testing.T) {
	started := make(chan struct{})
	run := func(ctx context.Context, _ string, args ...string) error {
		if err := os.WriteFile(args[2], []byte("partial"), 0600); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()
	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Start("eskisehir"); !errors.Is(err, maps.ErrState) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.Cancel("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "error")
	if _, err := os.Stat(filepath.Join(dir, "content/maps/eskisehir.part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial remains: %v", err)
	}
}
func TestFailedExtractionNotInstalled(t *testing.T) {
	run := func(_ context.Context, _ string, args ...string) error {
		if args[0] == "extract" {
			_ = os.WriteFile(args[2], []byte("bad"), 0600)
		}
		return fmt.Errorf("failure")
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()
	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "error")
	if _, err := os.Stat(filepath.Join(dir, "content/maps/eskisehir.pmtiles")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected installation: %v", err)
	}
	if _, _, err := s.Open("eskisehir"); err == nil {
		t.Fatal("failed package served")
	}
}

func TestDeleteDatabaseFailureRestoresFile(t *testing.T) {
	s, dir, cleanup := setup(t, fixtureRun)
	defer cleanup()
	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "ready")
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER map_delete_failure BEFORE DELETE ON map_packages BEGIN SELECT RAISE(FAIL,'failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("eskisehir"); err == nil {
		t.Fatal("expected failed delete")
	}
	f, _, err := s.Open("eskisehir")
	if err != nil {
		t.Fatalf("file not restored: %v", err)
	}
	f.Close()
}
func TestInterruptedWorkReconciledOnRestart(t *testing.T) {
	s, dir, cleanup := setup(t, fixtureRun)
	defer cleanup()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO map_packages(id,title,version,status) VALUES('eskisehir','Eskişehir','20261102','downloading')`); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, "content/maps/eskisehir.part")
	if err := os.WriteFile(part, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	restart, err := maps.New(db, dir, maps.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	items, err := restart.List()
	if err != nil || eskisehir(t, restart).Status != "error" {
		t.Fatalf("recovery: %+v %v", items, err)
	}
	if _, err := os.Stat(part); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale partial: %v", err)
	}
}

func TestExternalDownloadManagerAdapter(t *testing.T) {
	started := make(chan struct{})
	run := func(ctx context.Context, _ string, args ...string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager, err := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.RegisterExternalProvider(s)
	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	<-started
	items, err := manager.List()
	if err != nil || len(items) != 1 || items[0].ID != "maps:eskisehir" || !items[0].Indeterminate || items[0].Percentage != nil {
		t.Fatalf("adapter list: %+v %v", items, err)
	}
	if _, err := manager.Control("maps:eskisehir", "pause"); !errors.Is(err, download.ErrState) {
		t.Fatalf("pause: %v", err)
	}
	if _, err := manager.Control("maps:eskisehir", "cancel"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "error")
	item, err := manager.Get("maps:eskisehir")
	if err != nil || item.Status != "failed" {
		t.Fatalf("adapter status: %+v %v", item, err)
	}
}
