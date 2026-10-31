// Package packages verifies completed artifacts and owns their durable installation.
package packages

import "engelsiz/internal/download"

type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}
type Package struct {
	Active            bool            `json:"active"`
	ID                string          `json:"id"`
	DownloadID        string          `json:"downloadId"`
	Provider          string          `json:"provider"`
	ProviderPackageID string          `json:"providerPackageId"`
	ContentType       string          `json:"contentType"`
	Language          string          `json:"language"`
	Variant           string          `json:"variant"`
	Title             string          `json:"title"`
	Version           string          `json:"version"`
	SizeBytes         int64           `json:"sizeBytes"`
	Checksum          Checksum        `json:"checksum"`
	Status            string          `json:"status"`
	Error             *download.Error `json:"error"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
	InstalledAt       *string         `json:"installedAt"`
}
type record struct {
	Package
	SourceURL, SourcePath, LocalPath string
}

var (
	ErrMetadata     = &download.Error{Code: "metadata_unavailable", Message: "Bu indirmede kurulum için gereken sürüm bilgileri yok. Lütfen paketi yeniden indirin."}
	ErrChecksum     = &download.Error{Code: "checksum_unavailable", Message: "Bu paket için desteklenen SHA-256 doğrulaması bulunmuyor. Lütfen paketi yeniden indirin."}
	ErrVerification = &download.Error{Code: "verification_failed", Message: "Dosya boyutu veya SHA-256 doğrulaması eşleşmedi. Kurulum yapılmadı."}
	ErrCollision    = &download.Error{Code: "target_collision", Message: "Kurulum konumu başka bir dosya veya paket tarafından kullanılıyor."}
	ErrUnsafe       = &download.Error{Code: "ownership_conflict", Message: "Paket dosyasının güvenli sahipliği doğrulanamadı. Kurulum yapılmadı."}
	ErrFiles        = &download.Error{Code: "artifact_unavailable", Message: "Kurulum dosyaları eksik veya belirsiz. Dosyalar korunarak kurulum durduruldu."}
)
