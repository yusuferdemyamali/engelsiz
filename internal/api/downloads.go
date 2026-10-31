package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

func DownloadsHandler(manager *download.Manager, resolver providers.AcquisitionResolver, installers ...*packages.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		write := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
		fail := func(e error) {
			var problem *download.Error
			if !errors.As(e, &problem) {
				problem = download.ErrStorage
			}
			status := 500
			switch problem.Code {
			case "invalid_request":
				status = 400
			case "not_found":
				status = 404
			case "invalid_state", "download_exists":
				status = 409
			case "source_unavailable":
				status = 502
			case "insufficient_space":
				status = 507
			}
			write(status, map[string]any{"error": problem})
		}
		method := func(allow string) bool {
			if r.Method == allow {
				return true
			}
			w.Header().Set("Allow", allow)
			write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
			return false
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/downloads")
		if path == "" {
			if r.Method == http.MethodGet {
				items, e := manager.List()
				if e != nil {
					fail(e)
					return
				}
				write(200, map[string]any{"downloads": items})
				return
			}
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "GET, POST")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			var input struct {
				Provider          string `json:"provider"`
				ProviderPackageID string `json:"providerPackageId"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if e := decoder.Decode(&input); e != nil {
				fail(download.ErrInvalid)
				return
			}
			if decoder.Decode(&struct{}{}) != io.EOF || input.Provider != "kiwix" || strings.TrimSpace(input.ProviderPackageID) == "" {
				fail(download.ErrInvalid)
				return
			}
			// Existing selections remain a conflict even while upstream
			// metadata is unavailable; also avoid a needless catalog fetch.
			existing, err := manager.List()
			if err != nil {
				fail(err)
				return
			}
			for _, item := range existing {
				if item.Provider == input.Provider && item.ProviderPackageID == input.ProviderPackageID && item.Status != "cancelled" && item.Status != "completed" {
					fail(download.ErrExists)
					return
				}
			}
			if resolver == nil {
				fail(download.ErrSource)
				return
			}
			source, e := resolver.ResolveDownload(r.Context(), input.ProviderPackageID)
			if e != nil {
				fail(download.ErrSource)
				return
			}
			if source.Provider != input.Provider || source.ProviderPackageID != input.ProviderPackageID {
				fail(download.ErrSource)
				return
			}
			item, e := manager.Create(download.Descriptor{Provider: source.Provider, ProviderPackageID: source.ProviderPackageID, Title: source.Title, Variant: source.Variant, FileName: source.FileName, SourceURL: source.SourceURL, TotalBytes: source.TotalBytes, ContentType: source.ContentType, Language: source.Language, Version: source.Version, ExpectedSize: source.ExpectedSize, ChecksumAlgorithm: source.ChecksumAlgorithm, ChecksumValue: source.ChecksumValue})
			if e != nil {
				fail(e)
				return
			}
			write(201, map[string]any{"download": item})
			return
		}
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) < 1 || parts[0] == "" || len(parts) > 2 {
			fail(download.ErrNotFound)
			return
		}
		if len(parts) == 1 {
			if !method(http.MethodGet) {
				return
			}
			item, e := manager.Get(parts[0])
			if e != nil {
				fail(e)
				return
			}
			write(200, map[string]any{"download": item})
			return
		}
		if parts[1] == "install" {
			if !method(http.MethodPost) {
				return
			}
			if len(installers) != 1 || installers[0] == nil {
				fail(download.ErrStorage)
				return
			}
			if !emptyObject(w, r) {
				write(400, map[string]any{"error": download.ErrInvalid})
				return
			}
			item, installed, e := installers[0].Install(manager, parts[0])
			if e != nil {
				packageFailure(w, e)
				return
			}
			status := 202
			if installed {
				status = 200
			}
			write(status, map[string]any{"package": item})
			return
		}
		switch parts[1] {
		case "pause", "resume", "cancel", "retry":
		default:
			fail(download.ErrNotFound)
			return
		}
		if !method(http.MethodPost) {
			return
		}
		item, e := manager.Control(parts[0], parts[1])
		if e != nil {
			fail(e)
			return
		}
		write(200, map[string]any{"download": item})
	}
}
