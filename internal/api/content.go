package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type ContentBackend interface {
	ContentTransport() (http.RoundTripper, *url.URL, error)
}

// WithContent runs before ServeMux's canonical redirects so ambiguous content
// paths cannot be normalized into another application route.
func WithContent(shell http.Handler, backend ContentBackend, logger *log.Logger) http.Handler {
	content := ContentHandler(backend, logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/content" || strings.HasPrefix(r.URL.Path, "/content/") {
			content.ServeHTTP(w, r)
			return
		}
		shell.ServeHTTP(w, r)
	})
}
func safeContentPath(path string) bool {
	for i := 0; i < 8; i++ {
		if path != "/content" && !strings.HasPrefix(path, "/content/") {
			return false
		}
		if strings.ContainsAny(path, "\\\x00\r\n") || strings.Contains(path, "//") {
			return false
		}
		for _, part := range strings.Split(path, "/") {
			if part == "." || part == ".." {
				return false
			}
		}
		// Decode every valid escape independently. A literal percent in an
		// article name must not hide encoded traversal in another component.
		var next strings.Builder
		for offset := 0; offset < len(path); offset++ {
			if path[offset] == '%' && offset+2 < len(path) {
				if value, err := url.PathUnescape(path[offset : offset+3]); err == nil {
					next.WriteString(value)
					offset += 2
					continue
				}
			}
			next.WriteByte(path[offset])
		}
		decoded := next.String()
		if decoded == path {
			return true
		}
		path = decoded
	}
	return false
}
func contentFailure(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, `<!doctype html><html lang="tr"><meta charset="utf-8"><title>İçerik açılamıyor</title><main><h1>İçerik şu anda açılamıyor.</h1><p><a href="/">Engelsiz'e dön</a></p></main></html>`)
}
func ContentHandler(backend ContentBackend, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		if r.URL.IsAbs() || r.URL.Host != "" || r.URL.User != nil || r.URL.Opaque != "" || strings.HasPrefix(r.RequestURI, "//") || !strings.HasPrefix(r.RequestURI, "/") || !safeContentPath(r.URL.EscapedPath()) {
			contentFailure(w, 400)
			return
		}
		if r.URL.Path == "/content" {
			location := "/content/"
			if r.URL.RawQuery != "" {
				location += "?" + r.URL.RawQuery
			}
			w.Header().Set("Location", location)
			w.WriteHeader(302)
			return
		}
		if backend == nil {
			contentFailure(w, 503)
			return
		}
		transport, target, err := backend.ContentTransport()
		if err != nil || target == nil {
			contentFailure(w, 503)
			return
		}
		// The runtime owns the endpoint. Still fail closed at the HTTP boundary.
		if target.Scheme != "http" || target.Hostname() != "127.0.0.1" || target.Port() == "" || target.User != nil || target.RawQuery != "" || (target.Path != "" && target.Path != "/") {
			contentFailure(w, 503)
			return
		}
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.Out.URL.Scheme = target.Scheme
				pr.Out.URL.Host = target.Host
				pr.Out.Host = target.Host
				for key := range pr.Out.Header {
					lower := strings.ToLower(key)
					if lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") {
						pr.Out.Header.Del(key)
					}
				}
				pr.Out.Header.Del("Proxy-Authorization")
				pr.Out.Header.Del("Proxy-Connection")
			},
			ModifyResponse: func(resp *http.Response) error {
				for _, name := range []string{"Location", "Content-Location"} {
					if value := resp.Header.Get(name); value != "" {
						ref, e := url.Parse(value)
						if e != nil || ref.User != nil || ref.Opaque != "" {
							return errors.New("unsafe content redirect")
						}
						base := *target
						base.Path = r.URL.Path
						base.RawPath = r.URL.RawPath
						base.RawQuery = r.URL.RawQuery
						resolved := base.ResolveReference(ref)
						if resolved.Scheme != target.Scheme || resolved.Host != target.Host || !safeContentPath(resolved.EscapedPath()) || !strings.HasPrefix(resolved.Path, "/content/") {
							return errors.New("unsafe content redirect")
						}
						resolved.Scheme = ""
						resolved.Host = ""
						resp.Header.Set(name, resolved.String())
					}
				}
				for name, values := range resp.Header {
					lower := strings.ToLower(name)
					if lower == "refresh" || lower == "link" || lower == "alt-svc" || lower == "report-to" || lower == "nel" || lower == "server" || lower == "via" || lower == "forwarded" || strings.HasPrefix(lower, "x-forwarded-") {
						resp.Header.Del(name)
						continue
					}
					for _, value := range values {
						if strings.Contains(strings.ToLower(value), strings.ToLower(target.Host)) {
							resp.Header.Del(name)
							break
						}
					}
				}
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				if logger != nil {
					logger.Printf("content request failed: code=content_proxy_failed cause=%T", err)
				}
				contentFailure(w, 502)
			},
		}
		proxy.ServeHTTP(w, r)
	})
}
