package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"engelsiz/internal/download"
	"engelsiz/internal/education"
	"engelsiz/internal/packages"
)

func educationFailure(w http.ResponseWriter, e error) {
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
	case "insufficient_space":
		status = 507
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": problem})
}

func educationGuard(w http.ResponseWriter, r *http.Request) bool {
	host, hostErr := url.Parse("http://" + r.Host)
	origin := r.Header.Get("Origin")
	if hostErr != nil || host.Host != r.Host || host.User != nil ||
		(host.Hostname() != "127.0.0.1" && host.Hostname() != "localhost" && host.Hostname() != "::1") ||
		(origin != "" && origin != "http://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": &download.Error{Code: "forbidden", Message: "Eğitim yalnızca uygulamanın yerel arayüzünden kullanılabilir."}})
		return false
	}
	return true
}

// EducationHandler serves the offline education API. ZIM open links resolve
// through zimHref (runtime active snapshot); a nil zimHref leaves ZIM
// openHref null instead of inventing a path. Runtime failures stay honest:
// no openHref is exposed until the runtime actually serves the content.
func EducationHandler(service *education.Service, manager *download.Manager, pkgs *packages.Service, zimHref func(string) (string, bool)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		write := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
		if !educationGuard(w, r) {
			return
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				educationFailure(w, education.ErrInvalid)
				return
			}
		}
		if service == nil {
			write(503, map[string]any{"error": &download.Error{Code: "engine_unavailable", Message: "Eğitim hizmeti şu anda kullanılamıyor. Tekrar deneyin."}})
			return
		}
		enrich := func(v education.View) education.View {
			if v.Format == education.FormatZIM && v.Status == education.StatusInstalled && v.Download != nil && zimHref != nil {
				if href, ok := zimHref(v.Download.ID); ok && href != "" {
					dup := href
					v.OpenHref = &dup
				}
			}
			return v
		}
		enrichAll := func(res education.ListResponse) education.ListResponse {
			for i := range res.Resources {
				res.Resources[i] = enrich(res.Resources[i])
			}
			return res
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/education")
		if rest == "" {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			if r.URL.RawQuery != "" {
				educationFailure(w, education.ErrInvalid)
				return
			}
			res, err := service.List(manager, pkgs)
			if err != nil {
				educationFailure(w, err)
				return
			}
			write(200, enrichAll(res))
			return
		}
		trimmed := strings.TrimPrefix(rest, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) == 2 && parts[0] != "" && !strings.Contains(parts[0], "/") && (parts[1] == "download" || parts[1] == "install") {
			id, action := parts[0], parts[1]
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			if r.URL.RawQuery != "" {
				educationFailure(w, education.ErrInvalid)
				return
			}
			if action == "download" {
				if !emptyObject(w, r) {
					write(400, map[string]any{"error": education.ErrInvalid})
					return
				}
				view, err := service.StartDownload(manager, pkgs, id)
				if err != nil {
					educationFailure(w, err)
					return
				}
				write(201, map[string]any{"resource": enrich(view)})
				return
			}
			downloadID, ok := educationInstallID(w, r)
			if !ok {
				write(400, map[string]any{"error": education.ErrInvalid})
				return
			}
			view, installed, err := service.Install(manager, pkgs, id, downloadID)
			if err != nil {
				educationFailure(w, err)
				return
			}
			status := 202
			if installed {
				status = 200
			}
			write(status, map[string]any{"resource": enrich(view)})
			return
		}
		if len(parts) == 1 && parts[0] != "" && !strings.Contains(parts[0], "/") {
			if r.Method != http.MethodDelete {
				w.Header().Set("Allow", "DELETE")
				write(405, map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
				return
			}
			if r.URL.RawQuery != "" {
				educationFailure(w, education.ErrInvalid)
				return
			}
			// DELETE carries no body; any payload is rejected.
			if body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, education.MaxRequestBody)); err != nil || len(bytes.TrimSpace(body)) != 0 {
				educationFailure(w, education.ErrInvalid)
				return
			}
			view, err := service.Delete(manager, pkgs, parts[0])
			if err != nil {
				educationFailure(w, err)
				return
			}
			write(200, map[string]any{"resource": enrich(view)})
			return
		}
		educationFailure(w, education.ErrNotFound)
	}
}

// educationInstallID accepts {} (use latest associated download) or exactly
// one of download_id/downloadId. Unknown fields and extra data are rejected.
func educationInstallID(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, education.MaxRequestBody))
	if err != nil {
		return "", false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return "", false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return "", false
	}
	if object == nil {
		// Explicit JSON null is not an object: require {} or one id field.
		return "", false
	}
	if len(object) == 0 {
		return "", true
	}
	if len(object) != 1 {
		return "", false
	}
	raw, ok := object["download_id"]
	if !ok {
		raw, ok = object["downloadId"]
	}
	if !ok {
		return "", false
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", false
	}
	return strings.TrimSpace(id), true
}

const educationHTMLCSP = "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; font-src data:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

const educationContentPrefix = "/education/content/"

// EducationContentHandler serves installed PDF/HTML files with Range support.
// ZIM files are never served here; they go through the existing /content
// proxy. The CSP below is the network fail-closed boundary (not just the
// iframe sandbox): no network, no objects, no forms, self framing only. The
// real PhET runtime requires inline scripts plus eval, so script-src keeps
// 'unsafe-inline' 'unsafe-eval'; remote hosts stay unreachable (default-src
// 'none', connect-src 'none', images limited to data:/blob:), which actively
// blocks the known optional references inside audited PhET documents
// (Google Tag Manager script, tracking GIFs) without invalidating them.
// Inline http references are not rejected at install time because audited
// PhET documents carry license links; the network is fail-closed here.
func EducationContentHandler(service *education.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		host, hostErr := url.Parse("http://" + r.Host)
		if hostErr != nil || host.Host != r.Host || host.User != nil ||
			(host.Hostname() != "127.0.0.1" && host.Hostname() != "localhost" && host.Hostname() != "::1") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.RawQuery != "" || service == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if !strings.HasPrefix(r.URL.Path, educationContentPrefix) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, educationContentPrefix)
		if rest == "" || strings.Contains(rest, "/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		id := rest
		if strings.ContainsAny(id, "\\\x00\r\n") || id == "." || id == ".." {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		f, fi, contentType, err := service.Open(id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(contentType, "text/html") {
			w.Header().Set("Content-Security-Policy", educationHTMLCSP)
		} else {
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'self'")
		}
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	}
}
