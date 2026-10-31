// Package download transfers generic HTTP files into durable, unverified partial files.
package download

import (
	"errors"
	"net/url"
	"strings"
)

type Descriptor struct {
	Provider, ProviderPackageID, Title, Variant, FileName, SourceURL string
	TotalBytes                                                       *int64
	ContentType, Language, Version, ChecksumAlgorithm, ChecksumValue string
	ExpectedSize                                                     *int64
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

var (
	ErrNotFound = &Error{"not_found", "İndirme bulunamadı."}
	ErrState    = &Error{"invalid_state", "Bu işlem indirme durumuna uygun değil."}
	ErrExists   = &Error{"download_exists", "Bu paket için devam eden bir indirme var."}
	ErrInvalid  = &Error{"invalid_request", "İndirme isteği geçersiz."}
	ErrSpace    = &Error{"insufficient_space", "İndirme için yeterli boş disk alanı yok veya disk alanı ölçülemiyor."}
	ErrStorage  = &Error{"storage_error", "İndirme bilgileri veya dosyası kaydedilemedi. Lütfen tekrar deneyin."}
	ErrSource   = &Error{"source_unavailable", "İndirme kaynağına güvenli biçimde erişilemiyor. Lütfen tekrar deneyin."}
)

type Snapshot struct {
	ID                string   `json:"id"`
	Provider          string   `json:"provider"`
	ProviderPackageID string   `json:"providerPackageId"`
	Title             string   `json:"title"`
	Variant           string   `json:"variant"`
	FileName          string   `json:"fileName"`
	Status            string   `json:"status"`
	DownloadedBytes   int64    `json:"downloadedBytes"`
	TotalBytes        *int64   `json:"totalBytes"`
	Percentage        *float64 `json:"percentage"`
	Error             *Error   `json:"error"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

type record struct {
	Snapshot
	SourceURL, TempPath, ETag, LastModified                          string
	ContentType, Language, Version, ChecksumAlgorithm, ChecksumValue string
	ExpectedSize                                                     *int64
	ArtifactOwner, ArtifactState                                     string
}

func (r record) snapshot() Snapshot {
	s := r.Snapshot
	if s.TotalBytes != nil && *s.TotalBytes > 0 {
		p := float64(s.DownloadedBytes) / float64(*s.TotalBytes) * 100
		s.Percentage = &p
	}
	return s
}

// SafeFilename applies both Windows and Unix basename restrictions on either host.
func SafeFilename(s string) bool {
	if s == "" || s == "." || s == ".." || strings.TrimSpace(s) != s || strings.ContainsAny(s, `/\:*?"<>|`) || strings.HasSuffix(s, ".") {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}
func validDescriptor(d Descriptor) bool {
	u, e := url.Parse(d.SourceURL)
	return e == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Fragment == "" && SafeFilename(d.FileName) && d.Provider != "" && d.ProviderPackageID != "" && (d.TotalBytes == nil || *d.TotalBytes > 0)
}
func publicError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrStorage
}
