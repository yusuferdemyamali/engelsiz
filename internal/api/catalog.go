package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"engelsiz/internal/providers"
)

// The handler accepts only the operation it needs, keeping lifecycle controls
// inaccessible through HTTP.
func CatalogHandler(provider interface {
	Catalog(context.Context) ([]providers.CatalogPackage, error)
}, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "method_not_allowed"})
			return
		}
		packages, err := provider.Catalog(r.Context())
		if err != nil {
			if logger != nil {
				logger.Printf("catalog unavailable: code=catalog_unavailable cause=%T", err)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "catalog_unavailable", "message": "Katalog şu anda yenilenemiyor. Lütfen tekrar deneyin."}})
			return
		}
		if packages == nil {
			packages = []providers.CatalogPackage{}
		}
		_ = json.NewEncoder(w).Encode(struct {
			Packages []providers.CatalogPackage `json:"packages"`
		}{packages})
	}
}
