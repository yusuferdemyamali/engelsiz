// Package kiwix provides the application's sole content provider.
package kiwix

import (
	"context"
	"engelsiz/internal/providers"
	runtime "engelsiz/internal/runtime/kiwix"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)

// Options are trusted application inputs. CatalogURL and HTTPClient allow
// deterministic local integration tests without changing production filters.
type Options struct {
	BinaryPath     string
	ZIMPaths       []string
	Logger         *log.Logger
	StartupTimeout time.Duration
	StopGrace      time.Duration
	CatalogURL     string
	HTTPClient     *http.Client
}

type KiwixProvider struct {
	logger     *log.Logger
	runtime    *runtime.Runtime
	catalogURL *url.URL
	client     *http.Client
}

var _ providers.ContentProvider = (*KiwixProvider)(nil)

func New(opts Options) (*KiwixProvider, error) {
	endpoint := opts.CatalogURL
	if endpoint == "" {
		endpoint = "https://library.kiwix.org/catalog/v2/entries"
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, fmt.Errorf("invalid catalog endpoint")
	}
	client := http.Client{Timeout: 15 * time.Second}
	if opts.HTTPClient != nil {
		client = *opts.HTTPClient
	}
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	originalRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("catalog redirect limit")
		}
		if req.URL.Scheme != "https" && !(u.Scheme == "http" && req.URL.Scheme == "http" && req.URL.Host == u.Host) {
			return fmt.Errorf("unsafe catalog redirect")
		}
		if originalRedirect != nil {
			return originalRedirect(req, via)
		}
		return nil
	}
	r, err := runtime.New(runtime.Options{BinaryPath: opts.BinaryPath, ZIMPaths: opts.ZIMPaths, Logger: opts.Logger, StartupTimeout: opts.StartupTimeout, StopGrace: opts.StopGrace})
	if err != nil {
		return nil, err
	}
	return &KiwixProvider{logger: opts.Logger, runtime: r, catalogURL: u, client: &client}, nil
}
func (p *KiwixProvider) EnsureStarted(ctx context.Context) error { return p.runtime.EnsureStarted(ctx) }
func (p *KiwixProvider) Stop(ctx context.Context) error          { return p.runtime.Stop(ctx) }
func (p *KiwixProvider) Restart(ctx context.Context) error       { return p.runtime.Restart(ctx) }
func (p *KiwixProvider) Status() providers.RuntimeStatus         { return p.runtime.Status() }

// Private application integration; endpoints never enter RuntimeStatus or JSON.
func (p *KiwixProvider) ReplaceZIMPaths(ctx context.Context, paths []string, retry bool) error {
	return p.runtime.ReplaceZIMPaths(ctx, paths, retry)
}
func (p *KiwixProvider) Snapshot() (providers.RuntimeStatus, []string) { return p.runtime.Snapshot() }
func (p *KiwixProvider) ContentTransport() (http.RoundTripper, *url.URL, error) {
	return p.runtime.ContentTransport()
}

// The diagnostic boundary never formats remote errors: net/url and XML errors
// can carry credentials, query terms or malformed response-body fragments.
func (p *KiwixProvider) logFailure(operation string, err error) {
	if err != nil && p.logger != nil {
		p.logger.Printf("kiwix provider %s failed: code=source_unavailable cause=%T", operation, err)
	}
}
