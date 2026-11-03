package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"engelsiz/internal/providers/maps"
)

func MapsHandler(service *maps.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fail := func(err error) {
			status := http.StatusInternalServerError
			message := "Harita işlemi tamamlanamadı. Tekrar deneyin."
			if errors.Is(err, maps.ErrNotFound) {
				status = http.StatusNotFound
				message = "Harita bulunamadı."
			} else if errors.Is(err, maps.ErrState) {
				status = http.StatusConflict
				message = "Bu işlem haritanın mevcut durumunda yapılamaz."
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message}})
		}
		if r.URL.Path == "/api/maps" {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			items, err := service.List()
			if err != nil {
				fail(err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"packages": items})
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/maps/"), "/")
		if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
			fail(maps.ErrNotFound)
			return
		}
		id := parts[0]
		if len(parts) == 1 && r.Method == http.MethodDelete {
			if err := service.Delete(id); err != nil {
				fail(err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if len(parts) != 2 {
			fail(maps.ErrNotFound)
			return
		}
		switch parts[1] {
		case "file":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			f, fi, err := service.Open(id)
			if err != nil {
				fail(err)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "application/vnd.pmtiles")
			http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
		case "download", "cancel":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			var err error
			if parts[1] == "download" {
				err = service.Start(id)
			} else {
				err = service.Cancel(id)
			}
			if err != nil {
				fail(err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
		default:
			fail(maps.ErrNotFound)
		}
	}
}
