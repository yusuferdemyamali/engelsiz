package app

import (
	"context"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
	"io"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"
)

type snapshotStore struct {
	mu    sync.Mutex
	files []packages.InstalledFile
}

func (s *snapshotStore) InstalledSnapshot() ([]packages.InstalledFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]packages.InstalledFile{}, s.files...), nil
}
func (s *snapshotStore) set(paths ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = nil
	for _, p := range paths {
		s.files = append(s.files, packages.InstalledFile{Path: p})
	}
}

type activationProvider struct {
	mu      sync.Mutex
	state   providers.RuntimeState
	calls   chan []string
	release chan struct{}
}

func (p *activationProvider) Catalog(context.Context) ([]providers.CatalogPackage, error) {
	return nil, nil
}
func (p *activationProvider) EnsureStarted(context.Context) error { return nil }
func (p *activationProvider) Stop(context.Context) error          { return nil }
func (p *activationProvider) Restart(context.Context) error       { return nil }
func (p *activationProvider) Status() providers.RuntimeStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return providers.RuntimeStatus{State: p.state}
}
func (p *activationProvider) Snapshot() (providers.RuntimeStatus, []string) { return p.Status(), nil }
func (p *activationProvider) ContentTransport() (http.RoundTripper, *url.URL, error) {
	return nil, nil, nil
}
func (p *activationProvider) ReplaceZIMPaths(ctx context.Context, paths []string, retry bool) error {
	p.mu.Lock()
	p.state = providers.Starting
	p.mu.Unlock()
	select {
	case p.calls <- append([]string{}, paths...):
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.mu.Lock()
	p.state = providers.Ready
	p.mu.Unlock()
	return nil
}
func callPaths(t *testing.T, ch <-chan []string) []string {
	t.Helper()
	select {
	case paths := <-ch:
		return paths
	case <-time.After(time.Second):
		t.Fatal("no activation")
		return nil
	}
}
func TestActivationCoalescesLatestCompleteSetAndJoinsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &snapshotStore{}
	store.set("one")
	provider := &activationProvider{state: providers.Stopped, calls: make(chan []string, 4), release: make(chan struct{})}
	a := newActivation(ctx, nil, provider, log.New(io.Discard, "", 0), make(chan struct{}, 1))
	a.service = store
	a.start()
	a.notify <- struct{}{}
	if got := callPaths(t, provider.calls); !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatal(got)
	}
	store.set("one", "two")
	a.notify <- struct{}{}
	store.set("one", "two", "three")
	select {
	case a.notify <- struct{}{}:
	default:
	}
	// Duplicate retries during activation are accepted without queuing attempts.
	for i := 0; i < 10; i++ {
		if err := a.Retry(); err != nil {
			t.Fatal(err)
		}
	}
	provider.release <- struct{}{}
	if got := callPaths(t, provider.calls); !reflect.DeepEqual(got, []string{"one", "two", "three"}) {
		t.Fatal(got)
	}
	// Closing cancels the blocked attempt and joins it; queued work cannot run.
	select {
	case a.notify <- struct{}{}:
	default:
	}
	closed := make(chan struct{})
	go func() { a.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not join")
	}
	select {
	case got := <-provider.calls:
		t.Fatalf("late activation=%v", got)
	default:
	}
}
