package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

var searchPackageID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type installedSearchSnapshot interface {
	InstalledSnapshot() ([]packages.InstalledFile, error)
}

func searchFailure(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": RuntimeError{Code: code, Message: message}})
}

// SearchHandler checks the verified local registry before asking the optional
// provider for a ready generation. Online metadata is never part of this path.
func SearchHandler(provider providers.SearchProvider, installed installedSearchSnapshot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			searchFailure(w, 405, "method_not_allowed", "Bu işlem için HTTP yöntemi desteklenmiyor.")
			return
		}
		if len(r.URL.RawQuery) > 8192 {
			searchFailure(w, 400, "invalid_request", "Arama isteği geçersiz. Sorguyu ve seçili paketi kontrol edin.")
			return
		}
		params, parseErr := url.ParseQuery(r.URL.RawQuery)
		query, queryOK := params["q"]
		ids, idOK := params["packageId"]
		if parseErr != nil || len(params) != 2 || !queryOK || !idOK || len(query) != 1 || len(ids) != 1 || !searchPackageID.MatchString(ids[0]) || !utf8.ValidString(query[0]) || strings.IndexFunc(query[0], unicode.IsControl) >= 0 || utf8.RuneCountInString(strings.TrimSpace(query[0])) > 256 {
			searchFailure(w, 400, "invalid_request", "Arama isteği geçersiz. Sorguyu ve seçili paketi kontrol edin.")
			return
		}
		q, id := strings.TrimSpace(query[0]), ids[0]
		if installed == nil {
			searchFailure(w, 503, "runtime_unavailable", "İçerik hizmeti şu anda kullanılamıyor. Tekrar deneyin.")
			return
		}
		files, err := installed.InstalledSnapshot()
		if err != nil {
			searchFailure(w, 503, "runtime_unavailable", "İçerik hizmeti şu anda kullanılamıyor. Tekrar deneyin.")
			return
		}
		found := false
		for _, file := range files {
			if file.PackageID == id {
				found = true
				break
			}
		}
		if !found {
			searchFailure(w, 409, "content_unavailable", "Seçili paket kullanılamıyor. Kurulu paketleri kontrol edin.")
			return
		}
		if provider == nil {
			searchFailure(w, 503, "runtime_unavailable", "İçerik hizmeti şu anda kullanılamıyor. Tekrar deneyin.")
			return
		}
		result, err := provider.Search(r.Context(), id, q)
		if err != nil {
			var timeout net.Error
			switch {
			case errors.Is(err, providers.ErrContentUnavailable):
				searchFailure(w, 409, "content_unavailable", "Seçili paket kullanılamıyor. Kurulu paketleri kontrol edin.")
			case errors.Is(err, providers.ErrRuntimeUnavailable):
				searchFailure(w, 503, "runtime_unavailable", "İçerik hizmeti şu anda kullanılamıyor. Tekrar deneyin.")
			case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()):
				searchFailure(w, 504, "search_timeout", "Arama zaman aşımına uğradı. Tekrar deneyin.")
			default:
				searchFailure(w, 502, "search_unavailable", "Arama şu anda tamamlanamıyor. Tekrar deneyin.")
			}
			return
		}
		if result.Results == nil {
			result.Results = []providers.SearchResult{}
		}
		_ = json.NewEncoder(w).Encode(result)
	}
}
