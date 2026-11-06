// Package education owns offline training content (PDF/HTML documents and
// namespaced educational ZIM files).
//
// PDF/HTML artifacts are verified (SHA-256, exact size, magic) and installed
// atomically under content/education by this service. ZIM artifacts reuse the
// generic download.Manager transfer and delegate installation to the packages
// service with a namespaced Variant ("edu-<resourceID>") so educational ZIMs
// never replace the Wikipedia active group. Catalog metadata is injected via
// Options.Resources for deterministic tests; the production catalog is
// embedded from the parent-owned catalog.json and startup never fetches the
// network.
package education

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"engelsiz/internal/download"
)

//go:embed catalog.json
var catalogJSON []byte

// loadCatalog parses the parent-owned catalog. Parse or validation failures
// are returned so tests fail loudly while the app can degrade the education
// surface instead of crashing the whole process.
func loadCatalog() ([]Resource, error) {
	var out []Resource
	dec := json.NewDecoder(bytes.NewReader(catalogJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, errCatalogTrailing
	}
	if err := validateResources(out); err != nil {
		return nil, err
	}
	return out, nil
}

const (
	// ProviderFiles marks PDF/HTML transfers owned by this service. The
	// generic packages installer only claims kiwix rows, so it can never
	// claim these.
	ProviderFiles = "education"
	// ProviderZIM is the transfer provider for educational ZIM files.
	ProviderZIM = "kiwix"
	// ContentTypeZIM namespaces educational ZIMs inside content_packages.
	ContentTypeZIM = "education"
	// ContentTypeFiles marks PDF/HTML transfers inside the downloads table.
	ContentTypeFiles = "education"

	FormatPDF  = "pdf"
	FormatHTML = "html"
	FormatZIM  = "zim"

	// MaxRequestBody bounds install JSON bodies.
	MaxRequestBody = 4096
)

// Runtime statuses exposed by the API. The durable registry only stores
// installing/installed/error; the rest is derived from the download manager
// (downloading/downloaded), the packages service (zim installed) and the
// catalog (reference/missing).
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
	StatusInstalling  = "installing"
	StatusInstalled   = "installed"
	StatusFailed      = "failed"
	StatusReference   = "reference"
)

var (
	ErrInvalid   = &download.Error{Code: "invalid_request", Message: "Eğitim isteği geçersiz."}
	ErrNotFound  = &download.Error{Code: "not_found", Message: "Eğitim içeriği bulunamadı."}
	ErrState     = &download.Error{Code: "invalid_state", Message: "Bu işlem içeriğin mevcut durumunda yapılamaz."}
	ErrMetadata  = &download.Error{Code: "metadata_unavailable", Message: "Bu içerik için doğrulanmış indirme bilgisi yok."}
	ErrChecksum  = &download.Error{Code: "checksum_unavailable", Message: "Bu içerik için desteklenen SHA-256 doğrulaması bulunmuyor."}
	ErrVerify    = &download.Error{Code: "verification_failed", Message: "İçerik dosyası doğrulanamadı. Kurulum yapılmadı."}
	ErrCollision = &download.Error{Code: "target_collision", Message: "Kurulum konumu başka bir dosya veya içerik tarafından kullanılıyor."}
	ErrUnsafe    = &download.Error{Code: "ownership_conflict", Message: "İçerik dosyasının güvenli sahipliği doğrulanamadı. Kurulum yapılmadı."}
	ErrFiles     = &download.Error{Code: "artifact_unavailable", Message: "Kurulum dosyaları eksik veya belirsiz. Dosyalar korunarak kurulum durduruldu."}
)

var errCatalogTrailing = &download.Error{Code: "metadata_unavailable", Message: "Eğitim kataloğu okunamadı."}

// License carries the agreed frontend license shape. Verified must be true
// for a resource to be downloadable; unverified entries stay reference-only.
type License struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Attribution    string `json:"attribution"`
	Redistribution string `json:"redistribution"`
	Verified       bool   `json:"verified"`
}

// Resource is one static catalog entry. Pin fields (DownloadURL, SHA256,
// Filename, SizeBytes) are only honored when Downloadable is true and License
// is verified; otherwise the resource is reference-only and can never start
// a download.
type Resource struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	Category     string  `json:"category"`
	Source       string  `json:"source"`
	SourceURL    string  `json:"sourceUrl"`
	Language     string  `json:"language"`
	Format       string  `json:"format"`
	SizeBytes    *int64  `json:"sizeBytes"`
	Version      string  `json:"version"`
	PublishedAt  string  `json:"publishedAt,omitempty"`
	License      License `json:"license"`
	DownloadURL  string  `json:"downloadUrl,omitempty"`
	Downloadable bool    `json:"downloadable"`
	Restriction  string  `json:"restriction,omitempty"`
	SHA256       string  `json:"sha256,omitempty"`
	Filename     string  `json:"filename,omitempty"`
}

// DefaultResources is the production catalog embedded from the parent-owned
// catalog.json. An explicitly empty Options.Resources slice disables
// acquisition; nil selects this catalog. defaultCatalogErr carries a
// code-time catalog defect: tests fail on it, the app degrades the surface.
var DefaultResources, defaultCatalogErr = loadCatalog()

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validCategory(c string) bool {
	switch c {
	case "disaster", "survival", "science", "interactive":
		return true
	}
	return false
}

func validFormat(f string) bool {
	switch f {
	case FormatPDF, FormatHTML, FormatZIM:
		return true
	}
	return false
}

func validChecksum(pin string) bool {
	b, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(pin)))
	return pin != "" && err == nil && len(b) == sha256.Size
}

// validPinURL requires HTTPS, except loopback http URLs which exist only for
// deterministic httptest fixtures (the httptest server speaks plain http on
// 127.0.0.1). Production pins stay HTTPS-only.
func validPinURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Hostname() == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return true
	}
	return false
}

// VariantOf returns the namespaced download variant for a resource. The edu-
// prefix keeps educational ZIMs distinct from the Wikipedia active group.
func VariantOf(id string) string { return "edu-" + id }

// VerifiedPin reports whether the resource carries a complete verified
// acquisition pin and may be downloaded.
func (r Resource) VerifiedPin() bool {
	if !r.Downloadable || !r.License.Verified {
		return false
	}
	if r.DownloadURL == "" || r.Filename == "" || r.Version == "" {
		return false
	}
	if r.SizeBytes == nil || *r.SizeBytes <= 0 {
		return false
	}
	if !validChecksum(r.SHA256) || !validPinURL(r.DownloadURL) {
		return false
	}
	if !download.SafeFilename(r.Filename) {
		return false
	}
	if strings.ToLower(r.SHA256) != r.SHA256 {
		return false
	}
	return true
}

// validateResources fails closed on duplicates, bad ids, unknown enums and
// downloadable entries without complete verified pins.
func validateResources(in []Resource) error {
	seen := make(map[string]struct{}, len(in))
	for _, r := range in {
		if r.ID == "" || !validID.MatchString(r.ID) {
			return ErrInvalid
		}
		if _, dup := seen[r.ID]; dup {
			return ErrInvalid
		}
		seen[r.ID] = struct{}{}
		if r.Title == "" || !validFormat(r.Format) || !validCategory(r.Category) {
			return ErrInvalid
		}
		if r.Language == "" {
			return ErrInvalid
		}
		if r.Format == FormatZIM && !strings.HasSuffix(strings.ToLower(r.Filename), ".zim") && r.Downloadable {
			return ErrInvalid
		}
		if r.Downloadable && !r.VerifiedPin() {
			return ErrInvalid
		}
	}
	return nil
}
