package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

func updateHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
}
func UpdatesHandler(service *packages.Service, manager *download.Manager, provider providers.ContentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		updateHeaders(w)
		if r.URL.Path != "/api/updates" {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/updates/"), "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] != "activate" {
				packageFailure(w, download.ErrNotFound)
				return
			}
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				w.WriteHeader(405)
				return
			}
			if r.URL.RawQuery != "" || !emptyObject(w, r) {
				packageFailure(w, download.ErrInvalid)
				return
			}
			u, e := service.RetryActivation(parts[0])
			if e != nil {
				packageFailure(w, e)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"update": u})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(405)
			return
		}
		local := r.URL.RawQuery == "local=1"
		if r.URL.RawQuery != "" && !local {
			packageFailure(w, download.ErrInvalid)
			return
		}
		ops, e := service.Updates()
		if e != nil {
			packageFailure(w, e)
			return
		}
		installed, e := service.List("")
		if e != nil {
			packageFailure(w, e)
			return
		}
		// Transfer/verification failures remain recoverable through their existing
		// controls; mirror their errors without destroying the durable association.
		for i := range ops {
			op := &ops[i]
			if op.State == "downloading" && op.DownloadID != "" {
				if d, e := manager.Get(op.DownloadID); e == nil {
					if d.Status == "cancelled" {
						op.State = "cancelled"
					}
					if d.Error != nil {
						op.Error = d.Error
					}
				}
				for _, p := range installed {
					if p.ID == op.DownloadID && p.Error != nil {
						op.Error = p.Error
					}
				}
			}
		}
		response := map[string]any{"comparisonAvailable": false, "updates": ops}
		if !local {
			catalog, e := provider.Catalog(r.Context())
			if e != nil {
				response["error"] = &download.Error{Code: "catalog_unavailable", Message: "Güncelleme kontrolü şu anda yapılamıyor. Kurulu içerikler ve yerel işlemler kullanılabilir."}
			} else {
				response["comparisonAvailable"] = true
				for _, offer := range packages.CompareUpdates(installed, catalog) {
					conflict := false
					for _, op := range ops {
						superseded := (op.State == "failed" || op.State == "interrupted") && op.CandidateProviderPackageID != offer.CandidateProviderPackageID && offer.CandidateVersion > op.CandidateVersion
						if op.PackageID == offer.PackageID && op.State != "succeeded" && op.State != "cancelled" && !superseded {
							conflict = true
						}
					}
					if !conflict {
						ops = append(ops, offer)
					}
				}
				response["updates"] = ops
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}
}
func UpdatePackageHandler(service *packages.Service, manager *download.Manager, provider providers.ContentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		updateHeaders(w)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/packages/"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "update" {
			packageFailure(w, download.ErrNotFound)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			w.WriteHeader(405)
			return
		}
		if r.URL.RawQuery != "" {
			packageFailure(w, download.ErrInvalid)
			return
		}
		var input struct {
			ProviderPackageID string `json:"providerPackageId"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(input.ProviderPackageID) == "" {
			packageFailure(w, download.ErrInvalid)
			return
		}
		resolver, _ := provider.(providers.AcquisitionResolver)
		u, e := service.StartUpdate(r.Context(), manager, resolver, parts[0], input.ProviderPackageID)
		if e != nil {
			packageFailure(w, e)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"update": u})
	}
}
