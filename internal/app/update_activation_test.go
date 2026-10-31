package app

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

type updateMemory struct {
	previous          []packages.InstalledFile
	candidate         packages.InstalledFile
	op                packages.UpdateActivation
	committed, failed bool
	commitErr         error
}

func (s *updateMemory) PendingActivations() ([]packages.UpdateActivation, error) {
	return []packages.UpdateActivation{s.op}, nil
}
func (s *updateMemory) BeginActivation(string) ([]packages.InstalledFile, packages.InstalledFile, error) {
	return s.previous, s.candidate, nil
}
func (s *updateMemory) CommitActivation(string) error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.committed = true
	return nil
}
func (s *updateMemory) FailActivation(string, string, string) error { s.failed = true; return nil }

type updateRuntime struct {
	activationProvider
	paths        []string
	replacements [][]string
	checkErr     error
	restoreErr   error
	probe        func(context.Context) error
}

func (p *updateRuntime) ReplaceZIMPaths(ctx context.Context, paths []string, retry bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = append([]string{}, paths...)
	p.replacements = append(p.replacements, append([]string{}, paths...))
	if len(p.replacements) > 1 && p.restoreErr != nil {
		p.state = providers.Failed
		return p.restoreErr
	}
	p.state = providers.Ready
	return nil
}
func (p *updateRuntime) Snapshot() (providers.RuntimeStatus, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return providers.RuntimeStatus{State: p.state}, append([]string{}, p.paths...)
}
func (p *updateRuntime) Search(ctx context.Context, id, query string) (providers.SearchResponse, error) {
	return providers.SearchResponse{PackageID: id, Query: query}, nil
}
func (p *updateRuntime) CheckContent(ctx context.Context, id string) error {
	if p.probe != nil {
		return p.probe(ctx)
	}
	return p.checkErr
}
func (p *updateRuntime) ContentTransport() (http.RoundTripper, *url.URL, error) {
	return nil, &url.URL{Scheme: "http", Host: "127.0.0.1:1"}, nil
}

func updateSetup(t *testing.T) (*activation, *updateMemory, *updateRuntime) {
	t.Helper()
	store := &updateMemory{op: packages.UpdateActivation{ID: "op", PackageID: "old", CandidatePackageID: "new"}, previous: []packages.InstalledFile{{PackageID: "old", Path: "old.zim"}, {PackageID: "other", Path: "other.zim"}}, candidate: packages.InstalledFile{PackageID: "new", Path: "new.zim"}}
	provider := &updateRuntime{}
	a := newActivation(context.Background(), nil, provider, log.New(io.Discard, "", 0), make(chan struct{}, 1))
	a.updates = store
	t.Cleanup(a.cancel)
	return a, store, provider
}
func TestUpdateActivationSuccessReplacesOnlyMatchingRelease(t *testing.T) {
	a, store, p := updateSetup(t)
	if err := a.activatePending(); err != nil {
		t.Fatal(err)
	}
	if !store.committed || store.failed || !reflect.DeepEqual(p.replacements, [][]string{{"new.zim", "other.zim"}}) {
		t.Fatalf("commit=%v failed=%v replacements=%v", store.committed, store.failed, p.replacements)
	}
	if a.staging {
		t.Fatal("gate remained closed")
	}
}
func TestUpdateActivationFailureRestoresCompleteSelection(t *testing.T) {
	for _, commitFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "book unavailable", true: "commit failed"}[commitFailure], func(t *testing.T) {
			a, store, p := updateSetup(t)
			if commitFailure {
				store.commitErr = errors.New("storage failure")
			} else {
				p.checkErr = errors.New("book unavailable")
			}
			if err := a.activatePending(); err != nil {
				t.Fatal(err)
			}
			if store.committed || !store.failed || !reflect.DeepEqual(p.replacements, [][]string{{"new.zim", "other.zim"}, {"old.zim", "other.zim"}}) {
				t.Fatalf("commit=%v failed=%v replacements=%v", store.committed, store.failed, p.replacements)
			}
		})
	}
}
func TestUpdateGateHidesCandidateAndShutdownNeverRestartsRollback(t *testing.T) {
	a, store, p := updateSetup(t)
	if _, err := a.Search(context.Background(), "old", "query"); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	p.probe = func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = a.activatePending() }()
	<-entered
	if got := a.Snapshot(); got.Runtime.State != "starting" || len(got.Contents) != 0 {
		t.Fatal(got)
	}
	if _, _, err := a.ContentTransport(); err == nil {
		t.Fatal("candidate content exposed")
	}
	if _, err := a.Search(context.Background(), "new", "query"); !errors.Is(err, providers.ErrRuntimeUnavailable) {
		t.Fatal(err)
	}
	a.cancel()
	wg.Wait()
	if got := a.Snapshot(); got.Runtime.State != "stopping" || len(got.Contents) != 0 {
		t.Fatal(got)
	}
	if store.committed || !store.failed || len(p.replacements) != 1 {
		t.Fatalf("commit=%v failed=%v replacements=%v", store.committed, store.failed, p.replacements)
	}
}

type publicTestTransport struct{ calls int }

func (p *publicTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("page")), Header: http.Header{}, Request: r}, nil
}
func TestCapturedPublicTransportCannotEnterStagedGeneration(t *testing.T) {
	a, _, _ := updateSetup(t)
	underlying := &publicTestTransport{}
	transport := &publicContentTransport{owner: a, transport: underlying}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://127.0.0.1:1/content/", nil)
	a.setStaging(true)
	if _, err := transport.RoundTrip(req); !errors.Is(err, providers.ErrRuntimeUnavailable) {
		t.Fatal(err)
	}
	if underlying.calls != 0 {
		t.Fatal("request reached uncommitted runtime")
	}
	a.setStaging(false)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if underlying.calls != 1 {
		t.Fatal(underlying.calls)
	}
}

func TestUpdateRollbackFailurePreservesCommittedSelection(t *testing.T) {
	a, store, p := updateSetup(t)
	p.checkErr = errors.New("candidate unavailable")
	p.restoreErr = errors.New("old runtime failed")
	if err := a.activatePending(); err != nil {
		t.Fatal(err)
	}
	if store.committed || !store.failed || len(p.replacements) != 2 {
		t.Fatalf("commit=%v failed=%v replacements=%v", store.committed, store.failed, p.replacements)
	}
	result := a.Snapshot()
	if result.Runtime.State != "failed" || result.Runtime.Error == nil || len(result.Contents) != 0 {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(store.previous, []packages.InstalledFile{{PackageID: "old", Path: "old.zim"}, {PackageID: "other", Path: "other.zim"}}) {
		t.Fatal("previous selection changed")
	}
}
