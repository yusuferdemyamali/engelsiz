package api

import (
	"bytes"
	"encoding/json"
	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"errors"
	"io"
	"net/http"
	"net/url"
)

func packageFailure(w http.ResponseWriter, e error) {
	problem := download.ErrStorage
	var known *download.Error
	if errors.As(e, &known) {
		problem = known
	}
	status := 500
	switch problem.Code {
	case "invalid_request":
		status = 400
	case "not_found":
		status = 404
	case "invalid_state", "download_exists", "ownership_conflict", "target_collision":
		status = 409
	case "source_unavailable":
		status = 502
	case "insufficient_space":
		status = 507
	case "metadata_unavailable", "checksum_unavailable":
		status = 422
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": problem})
}
func emptyObject(w http.ResponseWriter, r *http.Request) bool {
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil {
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) < 2 || body[0] != '{' {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return false
	}
	return len(object) == 0
}
func PackagesHandler(service *packages.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(405)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": &download.Error{Code: "method_not_allowed", Message: "Bu işlem için HTTP yöntemi desteklenmiyor."}})
			return
		}
		query, parseErr := url.ParseQuery(r.URL.RawQuery)
		if parseErr != nil {
			packageFailure(w, download.ErrInvalid)
			return
		}
		values, present := query["status"]
		status := ""
		if len(query) > 1 || (len(query) == 1 && !present) || (present && (len(values) != 1 || values[0] != "installed")) {
			packageFailure(w, download.ErrInvalid)
			return
		}
		if present {
			status = values[0]
		}
		items, err := service.List(status)
		if err != nil {
			packageFailure(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"packages": items})
	}
}
