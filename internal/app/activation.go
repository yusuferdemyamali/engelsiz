package app

import (
	"context"
	"engelsiz/internal/api"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sync"
)

type servingProvider interface {
	ReplaceZIMPaths(context.Context, []string, bool) error
	Snapshot() (providers.RuntimeStatus, []string)
	ContentTransport() (http.RoundTripper, *url.URL, error)
}
type updateStore interface {
	PendingActivations() ([]packages.UpdateActivation, error)
	BeginActivation(string) ([]packages.InstalledFile, packages.InstalledFile, error)
	CommitActivation(string) error
	FailActivation(string, string, string) error
}

type activation struct {
	operations sync.Mutex
	gate       sync.RWMutex
	staging    bool
	updates    updateStore
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	notify     chan struct{}
	service    interface {
		InstalledSnapshot() ([]packages.InstalledFile, error)
	}
	provider     providers.ContentProvider
	serving      servingProvider
	logger       *log.Logger
	mu           sync.Mutex
	retry        bool
	retryPending bool
}

func newActivation(ctx context.Context, service *packages.Service, p providers.ContentProvider, logger *log.Logger, notify chan struct{}) *activation {
	work, cancel := context.WithCancel(ctx)
	serving, _ := p.(servingProvider)
	a := &activation{ctx: work, cancel: cancel, done: make(chan struct{}), notify: notify, service: service, provider: p, serving: serving, logger: logger}
	if service != nil {
		a.updates = service
	}
	return a
}
func (a *activation) refresh(retry bool) error {
	a.operations.Lock()
	defer a.operations.Unlock()
	files, err := a.service.InstalledSnapshot()
	if err != nil {
		return err
	}
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if a.serving == nil {
		return a.provider.EnsureStarted(a.ctx)
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	return a.serving.ReplaceZIMPaths(a.ctx, paths, retry)
}
func (a *activation) start() {
	go func() {
		defer close(a.done)
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-a.notify:
				if a.ctx.Err() != nil {
					return
				}
				a.mu.Lock()
				retry := a.retry
				a.retry = false
				a.mu.Unlock()
				if err := a.refresh(retry); err != nil && !errors.Is(err, context.Canceled) {
					a.logger.Printf("content refresh failed: %v", err)
				}
				if a.ctx.Err() == nil {
					if err := a.activatePending(); err != nil && !errors.Is(err, context.Canceled) {
						a.logger.Printf("update activation failed: %v", err)
					}
				}
				if retry {
					a.mu.Lock()
					a.retryPending = false
					a.mu.Unlock()
				}
			}
		}
	}()
}
func (a *activation) close() { a.cancel(); <-a.done }
func (a *activation) Retry() error {
	files, err := a.service.InstalledSnapshot()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return api.ErrNoInstalledContent
	}
	status := a.provider.Status()
	if status.State == providers.Starting || status.State == providers.Stopping {
		return nil
	}
	a.mu.Lock()
	if a.retryPending {
		a.mu.Unlock()
		return nil
	}
	a.retryPending = true
	a.retry = true
	a.mu.Unlock()
	select {
	case a.notify <- struct{}{}:
	default:
	}
	return nil
}

var installedStem = regexp.MustCompile(`^[0-9a-f]{32}\.zim$`)

func (a *activation) Snapshot() api.RuntimeResponse {
	a.gate.RLock()
	defer a.gate.RUnlock()
	if a.ctx.Err() != nil {
		return api.RuntimeResponse{Runtime: api.RuntimeView{State: string(providers.Stopping)}, Contents: []api.ContentLink{}}
	}
	if a.staging {
		return api.RuntimeResponse{Runtime: api.RuntimeView{State: string(providers.Starting)}, Contents: []api.ContentLink{}}
	}
	status := a.provider.Status()
	var paths []string
	if a.serving != nil {
		status, paths = a.serving.Snapshot()
	}
	result := api.RuntimeResponse{Runtime: api.RuntimeView{State: string(status.State)}, Contents: []api.ContentLink{}}
	if status.State == providers.Failed {
		result.Runtime.Error = &api.RuntimeError{Code: "runtime_unavailable", Message: "İçerik hizmeti başlatılamadı. Tekrar deneyin."}
	}
	if status.State == providers.Ready {
		for _, path := range paths {
			name := filepath.Base(path)
			if installedStem.MatchString(name) {
				id := name[:32]
				result.Contents = append(result.Contents, api.ContentLink{PackageID: id, Href: "/content/content/" + id + "/"})
			}
		}
	}
	return result
}

// Public transports retain their captured generation, and cannot begin requests
// while candidate usability or rollback is still being established.
func (a *activation) ContentTransport() (http.RoundTripper, *url.URL, error) {
	a.gate.RLock()
	defer a.gate.RUnlock()
	if a.staging || a.serving == nil || a.ctx.Err() != nil {
		return nil, nil, providers.ErrRuntimeUnavailable
	}
	transport, target, err := a.serving.ContentTransport()
	if err != nil {
		return nil, nil, err
	}
	return &publicContentTransport{owner: a, transport: transport}, target, nil
}

type publicContentTransport struct {
	owner     *activation
	transport http.RoundTripper
}

func (p *publicContentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.owner.gate.RLock()
	defer p.owner.gate.RUnlock()
	if p.owner.staging || p.owner.ctx.Err() != nil {
		return nil, providers.ErrRuntimeUnavailable
	}
	return p.transport.RoundTrip(req)
}
func (a *activation) Search(ctx context.Context, id, query string) (providers.SearchResponse, error) {
	a.gate.RLock()
	defer a.gate.RUnlock()
	p, ok := a.provider.(providers.SearchProvider)
	if a.staging || !ok || a.ctx.Err() != nil {
		return providers.SearchResponse{}, providers.ErrRuntimeUnavailable
	}
	return p.Search(ctx, id, query)
}
func (a *activation) setStaging(value bool) { a.gate.Lock(); a.staging = value; a.gate.Unlock() }

func (a *activation) activatePending() error {
	a.operations.Lock()
	defer a.operations.Unlock()
	if a.updates == nil || a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	pending, err := a.updates.PendingActivations()
	if err != nil {
		return err
	}
	for _, op := range pending {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		if err := a.activateOne(op); err != nil {
			a.logger.Printf("update %s: %v", op.ID, err)
		}
	}
	return nil
}
func (a *activation) activateOne(op packages.UpdateActivation) error {
	previous, candidate, err := a.updates.BeginActivation(op.ID)
	if err != nil {
		return err
	}
	a.setStaging(true)
	defer a.setStaging(false)
	priorPaths := make([]string, 0, len(previous))
	nextPaths := make([]string, 0, len(previous))
	found := false
	for _, file := range previous {
		priorPaths = append(priorPaths, file.Path)
		if file.PackageID == op.PackageID {
			nextPaths = append(nextPaths, candidate.Path)
			found = true
		} else {
			nextPaths = append(nextPaths, file.Path)
		}
	}
	if !found || candidate.PackageID != op.CandidatePackageID || candidate.Path == "" {
		err = fmt.Errorf("candidate selection changed")
	}
	checker, ok := a.provider.(interface {
		CheckContent(context.Context, string) error
	})
	if err == nil && (a.serving == nil || !ok) {
		err = fmt.Errorf("candidate usability check unavailable")
	}
	attempted := false
	if err == nil {
		err = a.ctx.Err()
	}
	if err == nil {
		attempted = true
		err = a.serving.ReplaceZIMPaths(a.ctx, nextPaths, false)
	}
	if err == nil {
		err = checker.CheckContent(a.ctx, candidate.PackageID)
	}
	if err == nil {
		err = a.ctx.Err()
	}
	if err == nil {
		err = a.updates.CommitActivation(op.ID)
	}
	if err == nil {
		return nil
	}
	// Shutdown joins the coordinator and subsequently stops the provider. Never
	// launch rollback children once shutdown has cancelled the operation context.
	if attempted && a.ctx.Err() == nil {
		if restoreErr := a.serving.ReplaceZIMPaths(a.ctx, priorPaths, true); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore committed content: %w", restoreErr))
		}
	}
	if failErr := a.updates.FailActivation(op.ID, "activation_failed", "Güncelleme etkinleştirilemedi. Önceki sürüm korundu; tekrar deneyin."); failErr != nil {
		err = errors.Join(err, failErr)
	}
	return err
}
