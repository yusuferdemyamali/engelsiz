package kiwix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"engelsiz/internal/providers"
	fixture "engelsiz/internal/testsupport/kiwixfixture"
)

func TestReplaceCompleteSetAndLoadedSnapshot(t *testing.T) {
	r, marker := setup(t, fixture.Config{})
	var lifecycle bytes.Buffer
	r.opts.Logger = log.New(&lifecycle, "", 0)
	initial := r.opts.ZIMPaths[0]
	if err := r.ReplaceZIMPaths(t.Context(), []string{initial, initial}, false); err != nil {
		t.Fatal(err)
	}
	status, loaded := r.Snapshot()
	if status.State != providers.Ready || !slices.Equal(loaded, []string{initial}) {
		t.Fatalf("snapshot=%+v %q", status, loaded)
	}
	loaded[0] = "mutated copy"
	extra := filepath.Join(filepath.Dir(initial), "z extra.zim")
	if err := os.WriteFile(extra, []byte("opaque content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceZIMPaths(t.Context(), []string{initial}, true); err != nil || starts(t, marker) != 1 {
		t.Fatalf("ready retry not reused: %v", err)
	}
	if err := r.ReplaceZIMPaths(t.Context(), []string{extra, initial, extra}, false); err != nil {
		t.Fatal(err)
	}
	status, loaded = r.Snapshot()
	if status.State != providers.Ready || !slices.Equal(loaded, []string{initial, extra}) || starts(t, marker) != 2 {
		t.Fatalf("replacement=%+v %q", status, loaded)
	}
	if err := r.ReplaceZIMPaths(t.Context(), []string{extra, initial}, false); err != nil || starts(t, marker) != 2 {
		t.Fatalf("equal sorted set not reused: %v", err)
	}
	r.opts.BinaryPath = "missing binary must not resolve for empty set"
	if err := r.ReplaceZIMPaths(t.Context(), nil, false); err != nil {
		t.Fatal(err)
	}
	if status, loaded := r.Snapshot(); status.State != providers.Stopped || len(loaded) != 0 {
		t.Fatalf("empty=%+v %q", status, loaded)
	}
	assertReapedGenerations(t, lifecycle.String())
}

func TestReplaceFailureRequiresExplicitRetryAndChangedSetRecovers(t *testing.T) {
	r, marker := setup(t, fixture.Config{EarlyExit: true})
	paths := append([]string(nil), r.opts.ZIMPaths...)
	if err := r.ReplaceZIMPaths(t.Context(), paths, false); err == nil {
		t.Fatal("bad content started")
	}
	data, _ := json.Marshal(fixture.Config{MarkerPath: marker})
	if err := os.WriteFile(paths[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceZIMPaths(t.Context(), paths, false); err == nil || starts(t, marker) != 1 {
		t.Fatal("failed equal set retried implicitly")
	}
	if _, loaded := r.Snapshot(); len(loaded) != 0 {
		t.Fatal("failed generation exposed content")
	}
	if err := r.ReplaceZIMPaths(t.Context(), paths, true); err != nil || starts(t, marker) != 2 {
		t.Fatalf("explicit retry=%v", err)
	}
	badSet := fixture.WriteConfig(t, fixture.Config{MarkerPath: marker, EarlyExit: true})
	if err := r.ReplaceZIMPaths(t.Context(), []string{badSet}, false); err == nil || starts(t, marker) != 3 {
		t.Fatalf("new failing set=%v", err)
	}
	changed := fixture.WriteConfig(t, fixture.Config{MarkerPath: marker})
	if err := r.ReplaceZIMPaths(t.Context(), []string{changed}, false); err != nil || starts(t, marker) != 4 {
		t.Fatalf("new set=%v", err)
	}
}

func TestStopInvalidatesDelayedAndQueuedRefresh(t *testing.T) {
	r, marker := setup(t, fixture.Config{})
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.opts.StartupTimeout = 4 * time.Second
	slow := fixture.WriteConfig(t, fixture.Config{MarkerPath: marker, ReadyDelay: 2 * time.Second})
	refresh := make(chan error, 1)
	go func() { refresh <- r.ReplaceZIMPaths(context.Background(), []string{slow}, false) }()
	eventually(t, func() bool { return starts(t, marker) == 2 && r.Status().State == providers.Starting })
	if _, paths := r.Snapshot(); len(paths) != 0 {
		t.Fatal("starting generation advertised content")
	}
	queued := make(chan error, 1)
	go func() { queued <- r.ReplaceZIMPaths(context.Background(), []string{slow}, true) }()
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("Stop waited for replacement readiness")
	}
	if err := <-refresh; !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh=%v", err)
	}
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued refresh=%v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if status := r.Status(); status.State != providers.Stopped || status.PID != 0 || starts(t, marker) != 2 {
		t.Fatalf("late child after stop: %+v", status)
	}
}

func TestConcurrentRefreshAndRetryReuseOneGeneration(t *testing.T) {
	r, marker := setup(t, fixture.Config{})
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	changed := fixture.WriteConfig(t, fixture.Config{MarkerPath: marker, ReadyDelay: 150 * time.Millisecond})
	var callers sync.WaitGroup
	errors := make(chan error, 20)
	for i := range 20 {
		callers.Go(func() {
			errors <- r.ReplaceZIMPaths(t.Context(), []string{changed, changed}, i%2 == 0)
		})
	}
	callers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if status, paths := r.Snapshot(); status.State != providers.Ready || !slices.Equal(paths, []string{changed}) || starts(t, marker) != 2 {
		t.Fatalf("concurrent refresh=%+v %q starts=%d", status, paths, starts(t, marker))
	}
}

func TestGenerationTransportRejectsLostProofAndOldGeneration(t *testing.T) {
	requests := filepath.Join(t.TempDir(), "requests")
	r, _ := setup(t, fixture.Config{RequestMarker: requests})
	if _, _, err := r.ContentTransport(); err == nil {
		t.Fatal("stopped transport available")
	}
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport, target, err := r.ContentTransport()
	if err != nil {
		t.Fatal(err)
	}
	if target.Scheme != "http" || target.Path != "" || target.RawQuery != "" || target.User != nil {
		t.Fatalf("unsafe target=%v", target)
	}
	before := len(fixture.ReadEvents(t, requests))
	owner := r.listenerOwner
	r.listenerOwner = func(int, int) (bool, error) { return false, errors.New("proof unavailable") }
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String()+"/content/article?q=safe", nil)
	if resp, err := transport.RoundTrip(req); err == nil {
		resp.Body.Close()
		t.Fatal("unproven connection sent HTTP")
	}
	if len(fixture.ReadEvents(t, requests)) != before {
		t.Fatal("HTTP written before positive post-dial ownership evidence")
	}
	r.listenerOwner = owner
	if err := r.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resp, err := transport.RoundTrip(req); err == nil {
		resp.Body.Close()
		t.Fatal("old transport adopted replacement")
	}
	current, target, err := r.ContentTransport()
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, target.String()+"/content/article", nil)
	resp, err := current.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if body, err := io.ReadAll(resp.Body); err != nil || string(body) != "fixture\n" {
		t.Fatalf("content=%q err=%v", body, err)
	}
}

func TestGenerationTransportChildExitPortReuseBeforeHTTP(t *testing.T) {
	r, _ := setup(t, fixture.Config{})
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport, target, _, err := r.ContentSnapshotTransport()
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	old := r.current.child
	r.mu.Unlock()
	var foreign *httptest.Server
	var contacted atomic.Int32
	r.dialContent = func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := old.cmd.Process.Kill(); err != nil {
			return nil, err
		}
		<-old.done
		listener, err := net.Listen("tcp4", address)
		if err != nil {
			return nil, err
		}
		foreign = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			contacted.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		foreign.Listener = listener
		foreign.Start()
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, address)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String()+"/content/search?content=old&pattern=wiki&format=xml&start=0&pageLength=25", nil)
	if resp, err := transport.RoundTrip(req); err == nil {
		resp.Body.Close()
		t.Fatal("adopted reused port")
	}
	if foreign == nil {
		t.Fatal("port reuse did not occur")
	}
	defer foreign.Close()
	if contacted.Load() != 0 {
		t.Fatal("HTTP reached foreign listener after owned child exited")
	}
	resp, err := http.Get(foreign.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if contacted.Load() != 1 {
		t.Fatal("foreign listener was changed")
	}
}

func TestContentStreamingDoesNotHoldLifecycleLock(t *testing.T) {
	r, _ := setup(t, fixture.Config{StreamDelay: 3 * time.Second})
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport, target, err := r.ContentTransport()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprint(target)+"/content/stream", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("streaming held lifecycle mutex")
	}
}

func TestContentSnapshotSearchGenerationAndMembershipStayTogether(t *testing.T) {
	requests := filepath.Join(t.TempDir(), "search requests")
	r, _ := setup(t, fixture.Config{RequestMarker: requests})
	if _, _, paths, err := r.ContentSnapshotTransport(); err == nil || len(paths) != 0 {
		t.Fatal("stopped generation returned loaded paths")
	}
	if err := r.EnsureStarted(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport, target, paths, err := r.ContentSnapshotTransport()
	if err != nil || !slices.Equal(paths, r.opts.ZIMPaths) {
		t.Fatalf("capture paths=%q err=%v", paths, err)
	}
	initial := paths[0]
	paths[0] = "caller mutation"
	_, _, copyPaths, err := r.ContentSnapshotTransport()
	if err != nil || len(copyPaths) != 1 || copyPaths[0] != initial {
		t.Fatal("capture exposed mutable generation paths")
	}

	// Keep an unissued search against the captured generation, then replace its
	// complete loaded set. A saved search cannot use the new child or its paths.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String()+"/content/search?content=old&pattern=wiki&format=xml&start=0&pageLength=25", nil)
	changed := fixture.WriteConfig(t, fixture.Config{RequestMarker: requests})
	if err := r.ReplaceZIMPaths(t.Context(), []string{changed}, false); err != nil {
		t.Fatal(err)
	}
	before := len(fixture.ReadEvents(t, requests))
	if resp, err := transport.RoundTrip(req); err == nil {
		resp.Body.Close()
		t.Fatal("pending search adopted replacement generation")
	}
	if len(fixture.ReadEvents(t, requests)) != before {
		t.Fatal("saved search wrote HTTP to replacement")
	}
	current, currentTarget, currentPaths, err := r.ContentSnapshotTransport()
	if err != nil || !slices.Equal(currentPaths, []string{changed}) {
		t.Fatalf("replacement capture=%q err=%v", currentPaths, err)
	}
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, currentTarget.String()+"/content/search?content=new&pattern=wiki&format=xml&start=0&pageLength=25", nil)
	// Search follows exactly the content transport's positive post-dial listener
	// ownership proof, before any request bytes reach even the owned child.
	owner := r.listenerOwner
	r.listenerOwner = func(int, int) (bool, error) { return false, errors.New("proof lost") }
	if resp, err := current.RoundTrip(req); err == nil {
		resp.Body.Close()
		t.Fatal("unproven search wrote HTTP")
	}
	r.listenerOwner = owner
	if len(fixture.ReadEvents(t, requests)) != before {
		t.Fatal("search bytes preceded ownership proof")
	}
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, _, loaded, err := r.ContentSnapshotTransport(); err == nil || len(loaded) != 0 {
		t.Fatal("stopped search capture returned loaded content")
	}
}
