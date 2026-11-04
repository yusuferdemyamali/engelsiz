package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"engelsiz/internal/download"
	"engelsiz/internal/translation"
)

func translationFailure(w http.ResponseWriter, e error) {
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
	case "metadata_unavailable", "checksum_unavailable":
		status = 422
	case "source_unavailable":
		status = 502
	case "engine_unavailable":
		status = 503
	case "insufficient_space":
		status = 507
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": problem})
}

// decodeSingle reads exactly one JSON object with a byte cap and no unknown
// fields. Extra trailing data, oversize bodies and unknown fields are all
// invalid requests.
func decodeSingle(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

// TranslationHandler serves the offline translation API. A nil service (failed
// startup dependency) fails closed with engine_unavailable; it never invents
// model state.
func TranslationHandler(service *translation.Service, manager *download.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		write := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
		// Loopback binding alone does not prevent another website from
		// initiating large local downloads or DNS-rebinding to this API.
		host, hostErr := url.Parse("http://" + r.Host)
		origin := r.Header.Get("Origin")
		if hostErr != nil || host.Host != r.Host || host.User != nil ||
			(host.Hostname() != "127.0.0.1" && host.Hostname() != "localhost" && host.Hostname() != "::1") ||
			(origin != "" && origin != "http://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			write(403, map[string]any{"error": &download.Error{Code: "forbidden", Message: "Çeviri yalnızca uygulamanın yerel arayüzünden kullanılabilir."}})
			return
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				translationFailure(w, translation.ErrInvalid)
				return
			}
		}
		unavailable := func() { write(503, map[string]any{"error": translation.ErrEngine}) }
		if service == nil {
			unavailable()
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/translation")
		if rest == "" {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			if r.URL.RawQuery != "" {
				translationFailure(w, translation.ErrInvalid)
				return
			}
			status, err := service.List(manager)
			if err != nil {
				translationFailure(w, err)
				return
			}
			write(200, status)
			return
		}
		if rest == "/translate" {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			var input struct {
				SourceLanguage string `json:"source_language"`
				TargetLanguage string `json:"target_language"`
				Text           string `json:"text"`
			}
			if !decodeSingle(w, r, translation.MaxRequestBody, &input) {
				translationFailure(w, translation.ErrInvalid)
				return
			}
			translated, err := service.Translate(r.Context(), input.SourceLanguage, input.TargetLanguage, input.Text)
			if err != nil {
				// Context gone with the client: nothing left to answer.
				if errors.Is(err, r.Context().Err()) && r.Context().Err() != nil {
					return
				}
				translationFailure(w, err)
				return
			}
			write(200, map[string]any{"translated_text": translated})
			return
		}
		parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
		if len(parts) == 3 && parts[0] == "models" && parts[1] != "" && (parts[2] == "download" || parts[2] == "install") {
			id, action := parts[1], parts[2]
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			if action == "download" {
				if !emptyObject(w, r) {
					write(400, map[string]any{"error": translation.ErrInvalid})
					return
				}
				snap, err := service.StartDownload(manager, id)
				if err != nil {
					translationFailure(w, err)
					return
				}
				write(201, map[string]any{"download": snap})
				return
			}
			var input struct {
				DownloadID string `json:"download_id"`
			}
			if !decodeSingle(w, r, 4096, &input) || strings.TrimSpace(input.DownloadID) == "" {
				translationFailure(w, translation.ErrInvalid)
				return
			}
			model, installed, err := service.Install(manager, id, input.DownloadID)
			if err != nil {
				translationFailure(w, err)
				return
			}
			status := 202
			if installed {
				status = 200
			}
			write(status, map[string]any{"model": model})
			return
		}
		translationFailure(w, translation.ErrNotFound)
	}
}
