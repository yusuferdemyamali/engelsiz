package kiwix

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"engelsiz/internal/providers"
)

// generationTransport never supplies an endpoint through public runtime status.
// Every connection is checked after dialing, before net/http can write HTTP.
type generationTransport struct {
	runtime   *Runtime
	attempt   *attempt
	child     *child
	address   string
	port      int
	transport *http.Transport
}

var errContentUnavailable = errors.New("owned content generation unavailable")

// ContentTransport captures one ready generation, along with its fixed private
// upstream URL. The caller owns HTTP routing, response sanitization and streaming.
// Saved transports cannot become transports for later replacement generations.
func (r *Runtime) ContentTransport() (http.RoundTripper, *url.URL, error) {
	transport, target, _, err := r.ContentSnapshotTransport()
	return transport, target, err
}

// ContentSnapshotTransport atomically captures the ready generation and its loaded
// paths. The returned paths are independent of the immutable launch inputs.
func (r *Runtime) ContentSnapshotTransport() (http.RoundTripper, *url.URL, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.current
	if r.stopping || r.status.State != providers.Ready || a == nil || a.proxy == nil || !a.proxy.liveLocked() {
		return nil, nil, nil, errContentUnavailable
	}
	return a.proxy, &url.URL{Scheme: "http", Host: a.proxy.address}, append([]string(nil), a.paths...), nil
}

func (r *Runtime) newGenerationTransport(a *attempt, c *child, address string, port int) *generationTransport {
	g := &generationTransport{runtime: r, attempt: a, child: c, address: address, port: port}
	g.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           g.dialContext,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
	}
	return g
}

func (g *generationTransport) liveLocked() bool {
	r := g.runtime
	if r.stopping || r.current != g.attempt || r.status.State != providers.Ready || g.attempt.child != g.child {
		return false
	}
	select {
	case <-g.child.done:
		return false
	default:
		return true
	}
}

func (g *generationTransport) live() bool {
	g.runtime.mu.Lock()
	defer g.runtime.mu.Unlock()
	return g.liveLocked()
}

func (g *generationTransport) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if address != g.address || network != "tcp" || !g.live() {
		return nil, errContentUnavailable
	}
	// The destination is constructed from the owned IPv4-loopback listener;
	// callers and proxy environment variables never participate in its choice.
	conn, err := g.runtime.dialContent(ctx, "tcp4", g.address)
	if err != nil {
		return nil, err
	}
	owned, proofErr := g.runtime.listenerOwner(g.child.cmd.Process.Pid, g.port)
	if proofErr != nil || !owned || !g.live() || ctx.Err() != nil {
		_ = conn.Close()
		return nil, errContentUnavailable
	}
	return conn, nil
}

func (g *generationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "http" || req.URL.Host != g.address || req.URL.User != nil || req.URL.Opaque != "" || !g.live() {
		return nil, errContentUnavailable
	}
	if req.URL.Path != "/content" && !strings.HasPrefix(req.URL.Path, "/content/") {
		return nil, fmt.Errorf("content route outside owned URL root")
	}
	copy := req.Clone(req.Context())
	copy.Host = g.address
	return g.transport.RoundTrip(copy)
}
