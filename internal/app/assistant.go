// Assistant app wiring: optional initialization, routes and shutdown for
// the offline assistant. The backend is optional: a missing or failed
// backend disables only the assistant surface (typed 503s) and never
// prevents the app from opening, and nothing here starts downloads on its
// own. Config flows as: ENGELSIZ_LLAMA_BINARY -> assistant.Options.Binary
// and ENGELSIZ_LLAMA_GPU_LAYERS (explicit, valid positive integers only) ->
// engine.Options.GPULayers; anything else keeps the CPU baseline (0).
package app

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"engelsiz/internal/api"
	"engelsiz/internal/assistant/rag"
	"engelsiz/internal/download"
	"engelsiz/internal/education"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

// assistantConfig carries the operator-controlled launcher inputs.
type assistantConfig struct {
	// Binary is the ENGELSIZ_LLAMA_BINARY override for the llama server
	// binary. Empty selects the installed runtime binary.
	Binary string
	// GPULayers enables GPU offload in the engine. Zero is the CPU baseline.
	GPULayers int
}

func assistantConfigFromEnv() assistantConfig {
	cfg := assistantConfig{Binary: os.Getenv("ENGELSIZ_LLAMA_BINARY")}
	if raw := strings.TrimSpace(os.Getenv("ENGELSIZ_LLAMA_GPU_LAYERS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 999 {
			cfg.GPULayers = n
		}
	}
	return cfg
}

// Backend seams, set by assistant_live.go. A nil seam (tests only) disables that surface.
var (
	newAssistantBackend func(db *sql.DB, dataRoot string, cfg assistantConfig, logger *log.Logger) (api.AssistantService, error)
	newAssistantRunner  func(logger *log.Logger, gpuLayers int) api.ChatRunner
)

// assistantRuntime is the wired assistant backend. A nil service means the
// assistant surface fails closed with 503s while the app runs normally.
type assistantRuntime struct {
	service api.AssistantService
	deps    api.ChatDeps
}

// openAssistant wires the optional assistant backend. It never fails: a
// missing seam or a construction error disables the assistant with a coded
// log line and no downloads. Retrieval always uses owned local inputs: the
// activation search and article readers plus the verified snapshot and
// installed education packs. Nothing routes through the /content proxy.
func openAssistant(logger *log.Logger, db *sql.DB, dataRoot string, manager *download.Manager, pkgs *packages.Service, edu *education.Service, searcher providers.SearchProvider, articles providers.ArticleReader) *assistantRuntime {
	cfg := assistantConfigFromEnv()
	rt := &assistantRuntime{}
	if newAssistantBackend != nil {
		svc, err := newAssistantBackend(db, dataRoot, cfg, logger)
		if err != nil {
			if logger != nil {
				logger.Printf("assistant initialization failed: code=storage_error")
			}
		} else {
			rt.service = svc
		}
	}
	if newAssistantRunner != nil {
		rt.deps.Runner = newAssistantRunner(logger, cfg.GPULayers)
	}
	var installed rag.InstalledSnapshooter
	if pkgs != nil {
		installed = pkgs
	}
	texts := &rag.EducationTexts{Service: edu, Manager: manager, Packages: pkgs}
	rt.deps.Service = rt.service
	rt.deps.Manager = manager
	rt.deps.Logger = logger
	rt.deps.Retrieve = func(ctx context.Context, query string, contextSize, maxTokens int) rag.Outcome {
		return rag.Retrieve(ctx, rag.Deps{Search: searcher, Installed: installed, Articles: articles, Texts: texts}, query, contextSize, maxTokens)
	}
	return rt
}

// ReadArticle serves owned article bytes with the activation staging guard,
// delegating to the provider's ArticleReader. Retrieval reaches installed
// articles the same way Search does, without touching the /content proxy.
func (a *activation) ReadArticle(ctx context.Context, packageID, href string) (providers.Article, error) {
	a.gate.RLock()
	defer a.gate.RUnlock()
	if a.staging || a.ctx.Err() != nil {
		return providers.Article{}, providers.ErrRuntimeUnavailable
	}
	reader, ok := a.provider.(providers.ArticleReader)
	if !ok || a.serving == nil {
		return providers.Article{}, providers.ErrRuntimeUnavailable
	}
	return reader.ReadArticle(ctx, packageID, href)
}

func (rt *assistantRuntime) handler() http.HandlerFunc {
	if rt == nil {
		return api.AssistantHandler(api.ChatDeps{})
	}
	return api.AssistantHandler(rt.deps)
}

// close releases the assistant backend. It runs before the download manager
// closes (deferred after it in Run), matching translation/education order.
func (rt *assistantRuntime) close() error {
	if rt == nil || rt.service == nil {
		return nil
	}
	return rt.service.Close()
}
