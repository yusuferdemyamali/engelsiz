package app

import (
	"context"
	"engelsiz/internal/api"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
	"errors"
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
type activation struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	notify  chan struct{}
	service interface {
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
	return &activation{ctx: work, cancel: cancel, done: make(chan struct{}), notify: notify, service: service, provider: p, serving: serving, logger: logger}
}
func (a *activation) refresh(retry bool) error {
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
