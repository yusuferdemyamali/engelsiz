// Package translation owns offline Argos TR<->EN text translation.
//
// Scope is exactly two directions (tr->en, en->tr). Model acquisition reuses
// the generic download.Manager; verification, atomic installation and the
// persistent worker lifecycle live here. Production acquisition metadata is
// pinned in pins.go after real artifact verification. Options.Pins permits
// isolated test catalogs; an explicitly empty map disables acquisition.
// Pins must never be invented or accepted from API request parameters.
package translation

import "engelsiz/internal/download"

const (
	Provider = "argos"

	// ModelTREN is Turkish -> English.
	ModelTREN = "argos-tr-en"
	// ModelENTR is English -> Turkish.
	ModelENTR = "argos-en-tr"

	// MaxCharacters bounds translate input in runes.
	MaxCharacters = 5000
	// MaxRequestBody bounds the translate/install JSON body (~64KB + object overhead).
	MaxRequestBody = 66 * 1024

	// ContentType marks translation artifacts inside the generic download table.
	ContentType = "translation-model"
)

// Model statuses exposed by the API. GET is read-only; recovery happens at
// startup, never as a side effect of listing.
const (
	StatusMissing    = "missing"
	StatusReady      = "ready"
	StatusInstalling = "installing"
	StatusError      = "error"
)

var (
	ErrInvalid   = &download.Error{Code: "invalid_request", Message: "Çeviri isteği geçersiz."}
	ErrNotFound  = &download.Error{Code: "not_found", Message: "Çeviri modeli bulunamadı."}
	ErrState     = &download.Error{Code: "invalid_state", Message: "Bu işlem modelin mevcut durumunda yapılamaz."}
	ErrMetadata  = &download.Error{Code: "metadata_unavailable", Message: "Bu model için doğrulanmış indirme bilgisi yok."}
	ErrChecksum  = &download.Error{Code: "checksum_unavailable", Message: "Bu model için desteklenen SHA-256 doğrulaması bulunmuyor."}
	ErrVerify    = &download.Error{Code: "verification_failed", Message: "Model dosyası doğrulanamadı. Kurulum yapılmadı."}
	ErrCollision = &download.Error{Code: "target_collision", Message: "Kurulum konumu başka bir dosya veya model tarafından kullanılıyor."}
	ErrUnsafe    = &download.Error{Code: "ownership_conflict", Message: "Model dosyasının güvenli sahipliği doğrulanamadı. Kurulum yapılmadı."}
	ErrFiles     = &download.Error{Code: "artifact_unavailable", Message: "Kurulum dosyaları eksik veya belirsiz. Dosyalar korunarak kurulum durduruldu."}
	ErrEngine    = &download.Error{Code: "engine_unavailable", Message: "Çeviri motoru şu anda kullanılamıyor. Tekrar deneyin."}
)

// Acquisition is the verified per-model artifact pin. Every field except Title
// participates in install validation; empty SourceURL/ChecksumValue means the
// model has no verified provenance and cannot be downloaded.
type Acquisition struct {
	Title         string
	Version       string
	FileName      string
	SourceURL     string
	TotalBytes    int64
	ExpectedSize  int64
	ChecksumValue string
}

// Definition is the static identity of one direction. Acquisition pins are
// injected; the zero value means unverified.
type Definition struct {
	ID          string
	SourceLang  string
	TargetLang  string
	Title       string
	Acquisition Acquisition
}

// Definitions returns the two supported directions in stable order.
func Definitions(pins map[string]Acquisition) []Definition {
	return []Definition{
		{ID: ModelTREN, SourceLang: "tr", TargetLang: "en", Title: "Türkçe → İngilizce", Acquisition: pins[ModelTREN]},
		{ID: ModelENTR, SourceLang: "en", TargetLang: "tr", Title: "İngilizce → Türkçe", Acquisition: pins[ModelENTR]},
	}
}

// Find returns the definition for id, or false when the id is unknown.
func Find(pins map[string]Acquisition, id string) (Definition, bool) {
	for _, d := range Definitions(pins) {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// Pair resolves a source/target language pair to its model id.
func Pair(pins map[string]Acquisition, source, target string) (Definition, bool) {
	for _, d := range Definitions(pins) {
		if d.SourceLang == source && d.TargetLang == target {
			return d, true
		}
	}
	return Definition{}, false
}

// Verified reports whether the definition carries a complete verified pin.
func (d Definition) Verified() bool {
	a := d.Acquisition
	return a.SourceURL != "" && a.FileName != "" && a.Version != "" &&
		a.TotalBytes > 0 && a.ExpectedSize > 0 && a.ChecksumValue != ""
}
