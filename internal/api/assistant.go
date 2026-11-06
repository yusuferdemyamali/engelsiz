// Assistant API: offline profiles, runtime acquisition and grounded chat.
//
// Chat answers only from validated local retrieval: no sources means a
// deterministic no-evidence response with no model call and no fabricated
// references. A question that alone exceeds the selected profile's prompt
// byte budget is rejected as invalid_request instead of being answered
// ungrounded. Prompts and source text are never logged.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"engelsiz/internal/assistant/rag"
	"engelsiz/internal/download"
)

const (
	// assistantChatBodyLimit bounds chat JSON bodies (2000-rune prompt plus envelope).
	assistantChatBodyLimit = 16 * 1024
	// assistantInstallBodyLimit bounds install JSON bodies.
	assistantInstallBodyLimit = 4096
	// assistantRetrievalTimeout bounds owned-local retrieval per chat turn.
	assistantRetrievalTimeout = 30 * time.Second
)

// AssistantProfile matches the manager Profile JSON.
type AssistantProfile struct {
	ID           string             `json:"id"`
	Title        string             `json:"title"`
	ModelName    string             `json:"model_name"`
	Quantization string             `json:"quantization"`
	SizeBytes    int64              `json:"size_bytes"`
	RAMGB        int                `json:"ram_gb"`
	ContextSize  int                `json:"context_size"`
	License      string             `json:"license"`
	Source       string             `json:"source"`
	Status       string             `json:"status"`
	Download     *download.Snapshot `json:"download"`
	Error        *download.Error    `json:"error"`
}

// AssistantHardware matches the manager hardware JSON.
type AssistantHardware struct {
	TotalMemoryBytes     int64 `json:"total_memory_bytes"`
	AvailableMemoryBytes int64 `json:"available_memory_bytes"`
	CPUCount             int   `json:"cpu_count"`
}

// AssistantRuntime matches the manager runtime JSON.
type AssistantRuntime struct {
	Available bool               `json:"available"`
	Status    string             `json:"status"`
	Download  *download.Snapshot `json:"download"`
	Error     *download.Error    `json:"error"`
	Version   string             `json:"version"`
}

// AssistantStatus matches the GET /api/assistant JSON.
type AssistantStatus struct {
	Profiles             []AssistantProfile `json:"profiles"`
	ActiveProfileID      string             `json:"active_profile_id"`
	RecommendedProfileID string             `json:"recommended_profile_id"`
	Hardware             AssistantHardware  `json:"hardware"`
	Runtime              AssistantRuntime   `json:"runtime"`
	Busy                 bool               `json:"busy"`
}

// AssistantModel matches the manager Model passed to UseModel.
type AssistantModel struct {
	ID          string
	Path        string
	Binary      string
	ContextSize int
	MaxTokens   int
}

// AssistantService matches the manager Go contract. The live adapter
// (internal/app/assistant_live.go) maps the real
// *assistant.Service onto this interface by field copy.
type AssistantService interface {
	List(m *download.Manager) (AssistantStatus, error)
	StartDownload(m *download.Manager, id string) (download.Snapshot, error)
	Install(m *download.Manager, id, downloadID string) (AssistantProfile, bool, error)
	Activate(id string) error
	Remove(id string) error
	StartRuntimeDownload(m *download.Manager) (download.Snapshot, error)
	InstallRuntime(m *download.Manager, downloadID string) (bool, error)
	UseModel(ctx context.Context, id string, fn func(context.Context, AssistantModel) error) error
	Close() error
}

// ChatMessage is one engine message. Roles are system/user.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatRequest matches the engine Request.
type ChatRequest struct {
	Binary      string
	ModelPath   string
	ContextSize int
	MaxTokens   int
	Messages    []ChatMessage
}

// ChatRunner matches the engine Generate surface. The live adapter
// builds one engine per call and always closes it before returning.
type ChatRunner interface {
	Generate(ctx context.Context, req ChatRequest, onDelta func(string) error) error
}

// ChatDeps carries the chat backend. A nil Service or Runner fails closed
// with 503; a nil Retrieve degrades to ungrounded answers.
type ChatDeps struct {
	Service  AssistantService
	Manager  *download.Manager
	Runner   ChatRunner
	Retrieve func(ctx context.Context, query string, contextSize, maxTokens int) rag.Outcome
	Logger   *log.Logger
}

func assistantFailure(w http.ResponseWriter, e error) {
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
	case "busy":
		status = 409
	case "insufficient_space":
		status = 507
	case "forbidden":
		status = 403
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": problem})
}

func assistantGuard(w http.ResponseWriter, r *http.Request) bool {
	host, hostErr := url.Parse("http://" + r.Host)
	origin := r.Header.Get("Origin")
	if hostErr != nil || host.Host != r.Host || host.User != nil ||
		(host.Hostname() != "127.0.0.1" && host.Hostname() != "localhost" && host.Hostname() != "::1") ||
		(origin != "" && origin != "http://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": &download.Error{Code: "forbidden", Message: "Asistan yalnızca uygulamanın yerel arayüzünden kullanılabilir."}})
		return false
	}
	return true
}

func jsonHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
}

func jsonInvalid(w http.ResponseWriter) {
	jsonHeaders(w)
	assistantFailure(w, &download.Error{Code: "invalid_request", Message: "Asistan isteği geçersiz."})
}

func jsonUnavailable(w http.ResponseWriter) {
	jsonHeaders(w)
	assistantFailure(w, &download.Error{Code: "engine_unavailable", Message: "Asistan motoru şu anda kullanılamıyor. Tekrar deneyin."})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	jsonHeaders(w)
	w.Header().Set("Allow", allow)
	w.WriteHeader(405)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "method_not_allowed", "message": "Bu işlem için HTTP yöntemi desteklenmiyor."}})
}

func (d ChatDeps) logf(format string, args ...any) {
	if d.Logger != nil {
		d.Logger.Printf("assistant "+format, args...)
	}
}

// AssistantHandler serves the assistant API. A nil Service fails closed with
// engine_unavailable; the app keeps booting regardless.
func AssistantHandler(deps ChatDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !assistantGuard(w, r) {
			return
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				jsonInvalid(w)
				return
			}
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/assistant")
		switch {
		case rest == "" || rest == "/":
			if r.Method != http.MethodGet {
				methodNotAllowed(w, "GET")
				return
			}
			assistantStatus(w, r, deps)
			return
		case rest == "/chat":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, "POST")
				return
			}
			assistantChat(w, r, deps)
			return
		case rest == "/runtime/download":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, "POST")
				return
			}
			if !emptyObject(w, r) {
				jsonInvalid(w)
				return
			}
			if deps.Service == nil {
				jsonUnavailable(w)
				return
			}
			snap, err := deps.Service.StartRuntimeDownload(deps.Manager)
			if err != nil {
				jsonHeaders(w)
				assistantFailure(w, err)
				return
			}
			jsonHeaders(w)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"download": snap})
			return
		case rest == "/runtime/install":
			if r.Method != http.MethodPost {
				methodNotAllowed(w, "POST")
				return
			}
			id, ok := assistantDownloadID(w, r)
			if !ok {
				jsonInvalid(w)
				return
			}
			if deps.Service == nil {
				jsonUnavailable(w)
				return
			}
			installed, err := deps.Service.InstallRuntime(deps.Manager, id)
			if err != nil {
				jsonHeaders(w)
				assistantFailure(w, err)
				return
			}
			jsonHeaders(w)
			if installed {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(202)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
		if len(parts) == 3 && parts[0] == "profiles" && parts[1] != "" && !strings.Contains(parts[1], "/") {
			id, action := parts[1], parts[2]
			switch action {
			case "download":
				if r.Method != http.MethodPost {
					methodNotAllowed(w, "POST")
					return
				}
				if !emptyObject(w, r) {
					jsonInvalid(w)
					return
				}
				if deps.Service == nil {
					jsonUnavailable(w)
					return
				}
				snap, err := deps.Service.StartDownload(deps.Manager, id)
				if err != nil {
					jsonHeaders(w)
					assistantFailure(w, err)
					return
				}
				jsonHeaders(w)
				w.WriteHeader(201)
				_ = json.NewEncoder(w).Encode(map[string]any{"download": snap})
				return
			case "install":
				if r.Method != http.MethodPost {
					methodNotAllowed(w, "POST")
					return
				}
				downloadID, ok := assistantDownloadID(w, r)
				if !ok {
					jsonInvalid(w)
					return
				}
				if deps.Service == nil {
					jsonUnavailable(w)
					return
				}
				profile, installed, err := deps.Service.Install(deps.Manager, id, downloadID)
				if err != nil {
					jsonHeaders(w)
					assistantFailure(w, err)
					return
				}
				jsonHeaders(w)
				if installed {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(202)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"profile": profile})
				return
			case "activate":
				if r.Method != http.MethodPost {
					methodNotAllowed(w, "POST")
					return
				}
				if !emptyObject(w, r) {
					jsonInvalid(w)
					return
				}
				if deps.Service == nil {
					jsonUnavailable(w)
					return
				}
				if err := deps.Service.Activate(id); err != nil {
					jsonHeaders(w)
					assistantFailure(w, err)
					return
				}
				jsonHeaders(w)
				w.WriteHeader(200)
				_ = json.NewEncoder(w).Encode(map[string]any{})
				return
			}
		}
		if len(parts) == 2 && parts[0] == "profiles" && parts[1] != "" && !strings.Contains(parts[1], "/") {
			if r.Method != http.MethodDelete {
				methodNotAllowed(w, "DELETE")
				return
			}
			if r.URL.RawQuery != "" || !assistantEmptyDelete(w, r) {
				jsonInvalid(w)
				return
			}
			if deps.Service == nil {
				jsonUnavailable(w)
				return
			}
			if err := deps.Service.Remove(parts[1]); err != nil {
				jsonHeaders(w)
				assistantFailure(w, err)
				return
			}
			jsonHeaders(w)
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		jsonHeaders(w)
		assistantFailure(w, &download.Error{Code: "not_found", Message: "Asistan profili bulunamadı."})
	}
}

// assistantDownloadID accepts exactly {"download_id": "<id>"}.
func assistantDownloadID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		DownloadID string `json:"download_id"`
	}
	if !decodeSingle(w, r, assistantInstallBodyLimit, &input) || strings.TrimSpace(input.DownloadID) == "" {
		return "", false
	}
	return strings.TrimSpace(input.DownloadID), true
}

// assistantEmptyDelete accepts an empty body or {} for DELETE.
func assistantEmptyDelete(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, assistantInstallBodyLimit))
	if err != nil {
		return false
	}
	if len(body) == 0 {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return false
	}
	return len(object) == 0
}

func assistantStatus(w http.ResponseWriter, r *http.Request, deps ChatDeps) {
	jsonHeaders(w)
	if r.URL.RawQuery != "" {
		assistantFailure(w, &download.Error{Code: "invalid_request", Message: "Asistan isteği geçersiz."})
		return
	}
	if deps.Service == nil {
		jsonUnavailable(w)
		return
	}
	status, err := deps.Service.List(deps.Manager)
	if err != nil {
		assistantFailure(w, err)
		return
	}
	if status.Profiles == nil {
		status.Profiles = []AssistantProfile{}
	}
	_ = json.NewEncoder(w).Encode(status)
}

// assistantPreMaxTokens is the output reservation assumed for the
// pre-stream fit check. Profiles carry no max-tokens field; the exact model
// values apply again inside UseModel, so a wrong guess only moves the
// rejection in-stream, never fabricates an answer.
const assistantPreMaxTokens = 512

// assistantWindow resolves the context window for profileID (or the active
// profile when empty) from a status snapshot. Unknown profiles report false
// so the turn proceeds to the exact in-stream handling.
func assistantWindow(st AssistantStatus, profileID string) (int, bool) {
	id := profileID
	if id == "" {
		id = st.ActiveProfileID
	}
	if id == "" {
		return 0, false
	}
	for _, p := range st.Profiles {
		if p.ID == id && p.ContextSize > 0 {
			return p.ContextSize, true
		}
	}
	return 0, false
}

// assistantTooLongMessage is the invalid_request text for an over-long
// question. Lite names its window; larger windows use the generic form.
func assistantTooLongMessage(contextSize int) string {
	if contextSize <= 2048 {
		return "Lite bağlamı için soruyu kısaltın."
	}
	return "Bağlam penceresi için soruyu kısaltın."
}

// assistantChat serves POST /api/assistant/chat as a server-sent event
// stream. Pre-stream failures use typed JSON; in-stream failures use error
// events. The prompt and source text never reach the logs: only typed codes
// are logged. The model runs inside UseModel and the runner closes its
// engine before returning; client disconnect cancels via the request context.
func assistantChat(w http.ResponseWriter, r *http.Request, deps ChatDeps) {
	var input struct {
		ProfileID string `json:"profile_id"`
		Prompt    string `json:"prompt"`
	}
	if !decodeSingle(w, r, assistantChatBodyLimit, &input) {
		jsonInvalid(w)
		return
	}
	prompt := strings.TrimSpace(input.Prompt)
	profileID := strings.TrimSpace(input.ProfileID)
	if !validChatInput(prompt, profileID) {
		jsonInvalid(w)
		return
	}
	if deps.Service == nil || deps.Runner == nil {
		jsonUnavailable(w)
		return
	}
	if st, err := deps.Service.List(deps.Manager); err == nil {
		if cs, ok := assistantWindow(st, profileID); ok && !rag.FitsBare(prompt, cs, assistantPreMaxTokens) {
			jsonHeaders(w)
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": &download.Error{Code: "invalid_request", Message: assistantTooLongMessage(cs)}})
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	emit := func(event string, payload any) bool {
		raw, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return r.Context().Err() == nil
	}
	emitStatus := func(state string) bool {
		return emit("status", map[string]any{"state": state})
	}
	emitError := func(err error) {
		code, message := "engine_unavailable", "Asistan motoru şu anda kullanılamıyor. Tekrar deneyin."
		var known *download.Error
		if errors.As(err, &known) && known.Code != "" {
			code = known.Code
			if known.Message != "" {
				message = known.Message
			}
		}
		deps.logf("chat failed: code=%s", code)
		emit("error", map[string]any{"error": map[string]string{"code": code, "message": message}})
	}
	if !emitStatus("searching") {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	answered := false
	if !emitStatus("loading") {
		return
	}
	err := deps.Service.UseModel(ctx, profileID, func(mctx context.Context, model AssistantModel) error {
		if mctx.Err() != nil {
			return mctx.Err()
		}
		contextSize, maxTokens := model.ContextSize, model.MaxTokens
		if contextSize <= 0 {
			contextSize = 2048
		}
		if maxTokens <= 0 {
			maxTokens = 512
		}
		outcome := rag.Outcome{}
		if deps.Retrieve != nil {
			retrieveCtx, stop := context.WithTimeout(mctx, assistantRetrievalTimeout)
			outcome = deps.Retrieve(retrieveCtx, prompt, contextSize, maxTokens)
			stop()
		}
		if mctx.Err() != nil {
			return mctx.Err()
		}
		if outcome.TooLong {
			emitError(&download.Error{Code: "invalid_request", Message: assistantTooLongMessage(outcome.ContextSize)})
			answered = true
			return nil
		}
		sources := outcome.Sources
		if sources == nil {
			sources = []rag.Source{}
		}
		if !emit("sources", map[string]any{"sources": sources}) {
			return context.Canceled
		}
		if !outcome.Grounded {
			emit("delta", map[string]any{"text": rag.NoEvidenceText})
			emit("done", map[string]any{"grounded": false})
			answered = true
			return nil
		}
		messages := outcome.Messages(prompt)
		req := ChatRequest{
			Binary:      model.Binary,
			ModelPath:   model.Path,
			ContextSize: contextSize,
			MaxTokens:   maxTokens,
			Messages:    []ChatMessage{{Role: messages[0].Role, Content: messages[0].Content}, {Role: messages[1].Role, Content: messages[1].Content}},
		}
		if !emitStatus("generating") {
			return context.Canceled
		}
		return deps.Runner.Generate(mctx, req, func(delta string) error {
			if delta == "" || mctx.Err() != nil {
				return mctx.Err()
			}
			if !emit("delta", map[string]any{"text": delta}) {
				return context.Canceled
			}
			return nil
		})
	})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		emitError(err)
		return
	}
	if ctx.Err() != nil || answered {
		return
	}
	emit("done", map[string]any{"grounded": true})
}

func validChatInput(prompt, profileID string) bool {
	runes := []rune(prompt)
	if len(runes) == 0 || len(runes) > rag.MaxPromptRunes {
		return false
	}
	if !utf8.ValidString(prompt) {
		return false
	}
	for _, r := range prompt {
		if (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || r == 0x7f {
			return false
		}
	}
	if len([]rune(profileID)) > 64 {
		return false
	}
	for _, r := range profileID {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
