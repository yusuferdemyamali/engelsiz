package maps_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engelsiz/internal/api"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/providers/maps"
)

func apiServer(t *testing.T, s *maps.Service) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(api.MapsHandler(s))
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

func getPackages(t *testing.T, client *http.Client, url string) []maps.Region {
	t.Helper()
	resp, err := client.Get(url + "/api/maps")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	var body struct {
		Packages []maps.Region `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Packages
}

func eskisehirAPI(t *testing.T, packages []maps.Region) maps.Region {
	t.Helper()
	for _, item := range packages {
		if item.ID == "eskisehir" {
			return item
		}
	}
	t.Fatal("eskisehir missing from catalog")
	return maps.Region{}
}

func postStatus(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestAPILifecycleHTTP(t *testing.T) {
	s, _, cleanup := setup(t, fixtureRun)
	defer cleanup()
	srv, client := apiServer(t, s)

	if got := getPackages(t, client, srv.URL); len(got) != 81 || eskisehirAPI(t, got).Status != "available" {
		t.Fatalf("initial packages: %d entries", len(got))
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/nope/download"); code != http.StatusNotFound {
		t.Fatalf("unknown download status = %d", code)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/nope/cancel"); code != http.StatusNotFound {
		t.Fatalf("unknown cancel status = %d", code)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/cancel"); code != http.StatusConflict {
		t.Fatalf("idle cancel status = %d", code)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/download"); code != http.StatusAccepted {
		t.Fatalf("download status = %d", code)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/download"); code != http.StatusConflict {
		t.Fatalf("duplicate download status = %d", code)
	}
	waitFor(t, s, "ready")

	full, err := client.Get(srv.URL + "/api/maps/eskisehir/file")
	if err != nil {
		t.Fatal(err)
	}
	full.Body.Close()
	if full.StatusCode != http.StatusOK || full.Header.Get("Content-Type") != "application/vnd.pmtiles" {
		t.Fatalf("file: %d %v", full.StatusCode, full.Header)
	}

	head, err := client.Head(srv.URL + "/api/maps/eskisehir/file")
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Fatalf("head status = %d", head.StatusCode)
	}

	rng, err := http.NewRequest(http.MethodGet, srv.URL+"/api/maps/eskisehir/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	rng.Header.Set("Range", "bytes=0-9")
	resp, err := client.Do(rng)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != "bytes 0-9/1024" {
		t.Fatalf("range: %d %v", resp.StatusCode, resp.Header)
	}

	bad, err := http.NewRequest(http.MethodGet, srv.URL+"/api/maps/eskisehir/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("Range", "bytes=9999-")
	badResp, err := client.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfiable status = %d", badResp.StatusCode)
	}

	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/download"); code != http.StatusConflict {
		t.Fatalf("download when ready = %d", code)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/maps/eskisehir", nil)
	if err != nil {
		t.Fatal(err)
	}
	del, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", del.StatusCode)
	}
	missing, err := client.Get(srv.URL + "/api/maps/eskisehir/file")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("file after delete = %d", missing.StatusCode)
	}
	if got := getPackages(t, client, srv.URL); len(got) != 81 || eskisehirAPI(t, got).Status != "available" {
		t.Fatalf("after delete packages: %d entries", len(got))
	}
}

func TestAPICancelDeleteWhileActiveAndRetry(t *testing.T) {
	var calls atomic.Int32
	run := func(ctx context.Context, _ string, args ...string) error {
		if calls.Add(1) == 1 {
			if len(args) > 2 && args[0] == "extract" {
				_ = os.WriteFile(args[2], []byte("partial"), 0600)
			}
			<-ctx.Done()
			return ctx.Err()
		}
		return fixtureRun(ctx, "", args...)
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()
	srv, client := apiServer(t, s)

	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/download"); code != http.StatusAccepted {
		t.Fatalf("download status = %d", code)
	}
	waitFor(t, s, "downloading")

	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/maps/eskisehir", nil)
	del, err := client.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	del.Body.Close()
	if del.StatusCode != http.StatusConflict {
		t.Fatalf("delete while active = %d", del.StatusCode)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/cancel"); code != http.StatusAccepted {
		t.Fatalf("cancel status = %d", code)
	}
	waitFor(t, s, "error")
	if _, err := os.Stat(filepath.Join(dir, "content/maps/eskisehir.part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial remains: %v", err)
	}
	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/cancel"); code != http.StatusConflict {
		t.Fatalf("cancel when idle = %d", code)
	}

	if code := postStatus(t, client, srv.URL+"/api/maps/eskisehir/download"); code != http.StatusAccepted {
		t.Fatalf("retry status = %d", code)
	}
	waitFor(t, s, "ready")
	f, _, err := s.Open("eskisehir")
	if err != nil {
		t.Fatalf("retry did not install: %v", err)
	}
	f.Close()
}

func TestConcurrentStartRace(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	run := func(ctx context.Context, _ string, args ...string) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}
	s, _, cleanup := setup(t, run)
	defer cleanup()

	const n = 16
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.Start("eskisehir")
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		} else if !errors.Is(err, maps.ErrState) {
			t.Fatalf("unexpected start error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("accepted starts = %d, want 1", ok)
	}
	<-started
	if err := s.Cancel("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "error")
}

func TestManagerRetryAfterFailure(t *testing.T) {
	var calls atomic.Int32
	run := func(ctx context.Context, _ string, args ...string) error {
		if calls.Add(1) == 1 {
			return errors.New("boom")
		}
		return fixtureRun(ctx, "", args...)
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()

	db2, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	manager2, err := download.New(db2, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager2.Close()
	manager2.RegisterExternalProvider(s)

	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "error")
	if _, err := os.Stat(filepath.Join(dir, "content/maps/eskisehir.part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial remains after failure: %v", err)
	}
	got, err := manager2.Get("maps:eskisehir")
	if err != nil || got.Status != "failed" {
		t.Fatalf("manager failed view: %+v %v", got, err)
	}
	if _, err := manager2.Control("maps:eskisehir", "retry"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "ready")
}

func TestManagerConcurrentGenericTransfer(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	run := func(ctx context.Context, _ string, args ...string) error {
		if len(args) > 0 && args[0] == "verify" {
			return nil
		}
		once.Do(func() { close(started) })
		select {
		case <-release:
			h := make([]byte, 1024)
			copy(h, "PMTiles")
			h[7] = 3
			h[99] = 1
			h[101] = 15
			return os.WriteFile(args[2], h, 0600)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s, dir, cleanup := setup(t, run)
	defer cleanup()

	db2, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	manager2, err := download.New(db2, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager2.Close()
	manager2.RegisterExternalProvider(s)

	if err := s.Start("eskisehir"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("map extract did not start")
	}
	waitFor(t, s, "downloading")

	payload := []byte("kiwix-generic-payload")
	content := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "21")
		_, _ = w.Write(payload)
	}))
	defer content.Close()
	snap, err := manager2.Create(download.Descriptor{Provider: "kiwix", ProviderPackageID: "generic-live", Title: "Generic", FileName: "generic-live.zim", SourceURL: content.URL})
	if err != nil {
		t.Fatalf("generic create during active map op: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := manager2.Get(snap.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "completed" {
			if got.DownloadedBytes != int64(len(payload)) {
				t.Fatalf("generic bytes = %d want %d", got.DownloadedBytes, len(payload))
			}
			break
		}
		if got.Status == "failed" || got.Status == "cancelled" {
			t.Fatalf("generic did not complete: %+v", got)
		}
		if time.Now().After(deadline) {
			t.Fatalf("generic still %+v while map active", got)
		}
		time.Sleep(10 * time.Millisecond)
	}

	items, err := manager2.List()
	if err != nil {
		t.Fatal(err)
	}
	var seenMap, seenGeneric bool
	for _, item := range items {
		switch item.ID {
		case "maps:eskisehir":
			seenMap = true
			if item.Status != "downloading" {
				t.Fatalf("map status during held extract: %+v", item)
			}
		case snap.ID:
			seenGeneric = true
			if item.Status != "completed" {
				t.Fatalf("generic status: %+v", item)
			}
		}
	}
	if !seenMap || !seenGeneric {
		t.Fatalf("concurrent list missing entries: %+v", items)
	}
	regions, err := s.List()
	if err != nil || len(regions) != 81 {
		t.Fatalf("map must still be active, got %d regions %v", len(regions), err)
	}
	if eskisehirAPI(t, regions).Status == "ready" || eskisehirAPI(t, regions).Status == "error" {
		t.Fatalf("map must still be active, got %+v", eskisehirAPI(t, regions))
	}

	close(release)
	waitFor(t, s, "ready")
	f, _, err := s.Open("eskisehir")
	if err != nil {
		t.Fatalf("map did not install after release: %v", err)
	}
	f.Close()
	got, err := manager2.Get(snap.ID)
	if err != nil || got.Status != "completed" {
		t.Fatalf("generic after map ready: %+v %v", got, err)
	}
}
