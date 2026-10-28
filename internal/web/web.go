package web

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

func distFS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return embedded
	}
	return sub
}

func Handler() http.Handler {
	if proxyTarget := os.Getenv("ENGELSIZ_DEV_PROXY"); proxyTarget != "" {
		if u, err := url.Parse(proxyTarget); err == nil {
			return httputil.NewSingleHostReverseProxy(u)
		}
	}
	fsys := distFS()
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "" {
			serveIndex(w, r, fsys)
			return
		}
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if _, err := fs.Stat(fsys, clean); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, fsys)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
