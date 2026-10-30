package api

import (
	"encoding/json"
	"engelsiz/internal/download"
	"net/http"
)

type RuntimeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type RuntimeView struct {
	State string        `json:"state"`
	Error *RuntimeError `json:"error"`
}
type ContentLink struct {
	PackageID string `json:"packageId"`
	Href      string `json:"href"`
}
type RuntimeResponse struct {
	Runtime  RuntimeView   `json:"runtime"`
	Contents []ContentLink `json:"contents"`
}
type RuntimeBackend interface {
	Snapshot() RuntimeResponse
	Retry() error
}

var ErrNoInstalledContent = &download.Error{Code: "no_installed_content", Message: "Henüz kurulu içerik yok."}

func RuntimeHandler(backend RuntimeBackend, retry bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		method := http.MethodGet
		if retry {
			method = http.MethodPost
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			w.WriteHeader(405)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": &download.Error{Code: "method_not_allowed", Message: "Bu işlem için HTTP yöntemi desteklenmiyor."}})
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery || (retry && !emptyObject(w, r)) {
			packageFailure(w, download.ErrInvalid)
			return
		}
		if retry {
			if err := backend.Retry(); err != nil {
				if err == ErrNoInstalledContent {
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": ErrNoInstalledContent})
				} else {
					packageFailure(w, download.ErrStorage)
				}
				return
			}
			w.WriteHeader(202)
		}
		_ = json.NewEncoder(w).Encode(backend.Snapshot())
	}
}
