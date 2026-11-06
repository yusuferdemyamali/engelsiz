// Package assistant owns the offline LLM assistant: the model catalog,
// hardware recommendation, verified GGUF acquisition, confined installation
// and the CPU llama.cpp runtime lifecycle.
//
// New(db *sql.DB, dataRoot string, opts Options) (*Service,error).
// List(manager *download.Manager) (Status,error).
// StartDownload(manager *download.Manager,id string) (download.Snapshot,error).
// Install(manager *download.Manager,id,downloadID string) (Profile,bool,error).
// Activate(id string) error. Remove(id string) error.
// StartRuntimeDownload(manager *download.Manager) (download.Snapshot,error).
// InstallRuntime(manager *download.Manager,downloadID string) (bool,error).
// UseModel(ctx,id string,fn func(context.Context,Model) error) error.
// Model {ID,Path,Binary,ContextSize,MaxTokens}. Close() error.
//
// Pins below are the verified supply-chain values (2026-11-06). Lite/Balanced
// are Apache-2.0 Unsloth community Q4_K_M quantizations and are explicitly
// labelled as community, not official Qwen quantizations. Quality is the
// official Qwen Q4_K_M artifact. No weights or binaries are downloaded during
// development or tests.
package assistant

import (
	"strings"

	"engelsiz/internal/download"
)

const (
	// Provider marks assistant artifacts inside the generic download table.
	Provider = "assistant-llm"
	// RuntimePackageID is the provider package id for the llama.cpp runtime.
	RuntimePackageID = "runtime"

	// ProfileLite is the 0.6B community quantization for low-memory hardware.
	ProfileLite = "lite"
	// ProfileBalanced is the 1.7B community quantization.
	ProfileBalanced = "balanced"
	// ProfileQuality is the official 4B quantization.
	ProfileQuality = "quality"

	// ContentTypeModel marks GGUF downloads; ContentTypeRuntime marks runtime archives.
	ContentTypeModel   = "assistant-model"
	ContentTypeRuntime = "assistant-runtime"
)

// Model statuses. GET is read-only; recovery happens at startup, never as a
// side effect of listing. Runtime adds "unavailable" for unsupported platforms.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
	StatusInstalling  = "installing"
	StatusReady       = "ready"
	StatusError       = "error"
	StatusUnavailable = "unavailable"
)

var (
	ErrBusy      = &download.Error{Code: "busy", Message: "Asistan şu anda başka bir işlemle meşgul. İşlem bitince tekrar deneyin."}
	ErrInvalid   = &download.Error{Code: "invalid_request", Message: "Asistan isteği geçersiz."}
	ErrNotFound  = &download.Error{Code: "not_found", Message: "Asistan profili bulunamadı."}
	ErrState     = &download.Error{Code: "invalid_state", Message: "Bu işlem profilin mevcut durumunda yapılamaz."}
	ErrEngine    = &download.Error{Code: "engine_unavailable", Message: "Asistan motoru şu anda kullanılamıyor. Tekrar deneyin."}
	ErrMetadata  = &download.Error{Code: "metadata_unavailable", Message: "Bu profil için doğrulanmış indirme bilgisi yok."}
	ErrChecksum  = &download.Error{Code: "checksum_unavailable", Message: "Bu profil için desteklenen SHA-256 doğrulaması bulunmuyor."}
	ErrVerify    = &download.Error{Code: "verification_failed", Message: "Dosya doğrulanamadı. Kurulum yapılmadı."}
	ErrCollision = &download.Error{Code: "target_collision", Message: "Kurulum konumu başka bir dosya veya profil tarafından kullanılıyor."}
	ErrUnsafe    = &download.Error{Code: "ownership_conflict", Message: "Dosyanın güvenli sahipliği doğrulanamadı. Kurulum yapılmadı."}
	ErrFiles     = &download.Error{Code: "artifact_unavailable", Message: "Kurulum dosyaları eksik veya belirsiz. Dosyalar korunarak kurulum durduruldu."}
)

// Acquisition is the verified per-model artifact pin. Every field except
// Title participates in install validation; empty SourceURL/ChecksumValue
// means the model has no verified provenance and cannot be downloaded.
// SourceURL values come only from this table: no arbitrary URL is ever
// accepted. Repo and Revision name the exact upstream snapshot the file,
// size and checksum were verified against.
type Acquisition struct {
	Title         string
	ModelName     string
	Quantization  string
	Version       string
	Repo          string
	Revision      string
	FileName      string
	SourceURL     string
	TotalBytes    int64
	ExpectedSize  int64
	ChecksumValue string
	ContextSize   int
	MaxTokens     int
	RAMGB         int
	License       string
	Source        string
}

// Definition is the static identity of one profile. Acquisition pins are
// injected; the zero value means unverified.
type Definition struct {
	ID          string
	Title       string
	Acquisition Acquisition
}

// Definitions returns the three profiles in stable order.
func Definitions(pins map[string]Acquisition) []Definition {
	return []Definition{
		{ID: ProfileLite, Title: "Lite · 0.6B hafif", Acquisition: pins[ProfileLite]},
		{ID: ProfileBalanced, Title: "Balanced · 1.7B dengeli", Acquisition: pins[ProfileBalanced]},
		{ID: ProfileQuality, Title: "Quality · 4B nitelikli (12-16 GB RAM hedefi)", Acquisition: pins[ProfileQuality]},
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

// Verified reports whether the definition carries a complete verified pin.
func (d Definition) Verified() bool {
	a := d.Acquisition
	return a.SourceURL != "" && a.FileName != "" && a.Version != "" &&
		a.TotalBytes > 0 && a.ExpectedSize > 0 && a.ChecksumValue != "" &&
		a.ModelName != "" && a.ContextSize > 0
}

// RuntimeAcquisition is the verified per-platform llama.cpp CPU archive pin.
type RuntimeAcquisition struct {
	Version       string
	FileName      string
	SourceURL     string
	TotalBytes    int64
	ExpectedSize  int64
	ChecksumValue string
	Format        string // "tar.gz" or "zip"
}

// Verified reports whether the runtime pin carries complete provenance.
func (r RuntimeAcquisition) Verified() bool {
	return r.Version != "" && r.FileName != "" && r.SourceURL != "" &&
		r.TotalBytes > 0 && r.ExpectedSize > 0 && r.ChecksumValue != "" &&
		(r.Format == "tar.gz" || r.Format == "zip")
}

// DefaultPins carries the verified per-model acquisition pins. Do not invent
// or edit these values without repeating the verification against the exact
// upstream revision. Lite/Balanced are Unsloth community quantizations.
var DefaultPins = map[string]Acquisition{
	ProfileLite: {
		Title: "Lite · 0.6B hafif", ModelName: "Qwen3-0.6B", Quantization: "Q4_K_M",
		Version:       "qwen3-0.6b-q4_k_m-2026-11-06",
		Repo:          "unsloth/Qwen3-0.6B-GGUF",
		Revision:      "50968a4468ef4233ed78cd7c3de230dd1d61a56b",
		FileName:      "Qwen3-0.6B-Q4_K_M.gguf",
		SourceURL:     "https://huggingface.co/unsloth/Qwen3-0.6B-GGUF/resolve/50968a4468ef4233ed78cd7c3de230dd1d61a56b/Qwen3-0.6B-Q4_K_M.gguf",
		TotalBytes:    396705472,
		ExpectedSize:  396705472,
		ChecksumValue: "ac2d97712095a558e31573f62f466a3f9d93990898b0ec79d7c974c1780d524a",
		ContextSize:   2048, MaxTokens: 512, RAMGB: 4,
		License: "Apache-2.0", Source: "Unsloth topluluk nicemlemesi (community)",
	},
	ProfileBalanced: {
		Title: "Balanced · 1.7B dengeli", ModelName: "Qwen3-1.7B", Quantization: "Q4_K_M",
		Version:       "qwen3-1.7b-q4_k_m-2026-11-06",
		Repo:          "unsloth/Qwen3-1.7B-GGUF",
		Revision:      "d7f544eead698dbd1f15126ef60b45a1e1933222",
		FileName:      "Qwen3-1.7B-Q4_K_M.gguf",
		SourceURL:     "https://huggingface.co/unsloth/Qwen3-1.7B-GGUF/resolve/d7f544eead698dbd1f15126ef60b45a1e1933222/Qwen3-1.7B-Q4_K_M.gguf",
		TotalBytes:    1107409472,
		ExpectedSize:  1107409472,
		ChecksumValue: "b139949c5bd74937ad8ed8c8cf3d9ffb1e99c866c823204dc42c0d91fa181897",
		ContextSize:   4096, MaxTokens: 1024, RAMGB: 8,
		License: "Apache-2.0", Source: "Unsloth topluluk nicemlemesi (community)",
	},
	ProfileQuality: {
		Title: "Quality · 4B nitelikli (12-16 GB RAM hedefi)", ModelName: "Qwen3-4B", Quantization: "Q4_K_M",
		Version:       "qwen3-4b-q4_k_m-2026-11-06",
		Repo:          "Qwen/Qwen3-4B-GGUF",
		Revision:      "bc640142c66e1fdd12af0bd68f40445458f3869b",
		FileName:      "Qwen3-4B-Q4_K_M.gguf",
		SourceURL:     "https://huggingface.co/Qwen/Qwen3-4B-GGUF/resolve/bc640142c66e1fdd12af0bd68f40445458f3869b/Qwen3-4B-Q4_K_M.gguf",
		TotalBytes:    2497280256,
		ExpectedSize:  2497280256,
		ChecksumValue: "7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5",
		ContextSize:   4096, MaxTokens: 1024, RAMGB: 12,
		License: "Apache-2.0", Source: "Qwen resmi",
	},
}

// DefaultRuntimePins carries the verified llama.cpp b12174 CPU archive pins
// per platform (MIT, ggml-org/llama.cpp releases).
var DefaultRuntimePins = map[string]RuntimeAcquisition{
	"linux/amd64": {
		Version: "b12174", FileName: "llama-b12174-bin-ubuntu-x64.tar.gz",
		SourceURL:     "https://github.com/ggml-org/llama.cpp/releases/download/b12174/llama-b12174-bin-ubuntu-x64.tar.gz",
		TotalBytes:    23047184,
		ExpectedSize:  23047184,
		ChecksumValue: "75c077cf5481264c9701851df05013c39dfe16760167eca7932af84c255e3dc5",
		Format:        "tar.gz",
	},
	"windows/amd64": {
		Version: "b12174", FileName: "llama-b12174-bin-win-cpu-x64.zip",
		SourceURL:     "https://github.com/ggml-org/llama.cpp/releases/download/b12174/llama-b12174-bin-win-cpu-x64.zip",
		TotalBytes:    24746357,
		ExpectedSize:  24746357,
		ChecksumValue: "b2ec458994bacf7e837dfc54e98152ea4bea97779014c858d98840d050f349d6",
		Format:        "zip",
	},
}

func validChecksum(value string) bool {
	lowered := strings.ToLower(strings.TrimSpace(value))
	if len(lowered) != 64 {
		return false
	}
	for _, c := range lowered {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
