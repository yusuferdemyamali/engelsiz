// Live wiring: plugs the real manager Service and engine into
// the assistant seams. Field names follow contracts.md, verified against
// internal/assistant/service.go and internal/assistant/engine/engine.go.
package app

import (
	"context"
	"database/sql"
	"log"

	"engelsiz/internal/api"
	"engelsiz/internal/assistant"
	"engelsiz/internal/assistant/engine"
	"engelsiz/internal/download"
)

func init() {
	newAssistantBackend = liveAssistantBackend
	newAssistantRunner = liveAssistantRunner
}

func liveAssistantBackend(db *sql.DB, dataRoot string, cfg assistantConfig, logger *log.Logger) (api.AssistantService, error) {
	svc, err := assistant.New(db, dataRoot, assistant.Options{Binary: cfg.Binary, Logger: logger})
	if err != nil {
		return nil, err
	}
	return liveService{inner: svc}, nil
}

type liveService struct{ inner *assistant.Service }

func liveStatus(s assistant.Status) api.AssistantStatus {
	profiles := make([]api.AssistantProfile, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		profiles = append(profiles, api.AssistantProfile{
			ID: p.ID, Title: p.Title, ModelName: p.ModelName, Quantization: p.Quantization,
			SizeBytes: p.SizeBytes, RAMGB: p.RAMGB, ContextSize: p.ContextSize,
			License: p.License, Source: p.Source, Status: p.Status,
			Download: p.Download, Error: p.Error,
		})
	}
	return api.AssistantStatus{
		Profiles: profiles, ActiveProfileID: s.ActiveProfileID, RecommendedProfileID: s.RecommendedProfileID,
		Hardware: api.AssistantHardware{
			TotalMemoryBytes: s.Hardware.TotalMemoryBytes, AvailableMemoryBytes: s.Hardware.AvailableMemoryBytes, CPUCount: s.Hardware.CPUCount,
		},
		Runtime: api.AssistantRuntime{
			Available: s.Runtime.Available, Status: s.Runtime.Status, Download: s.Runtime.Download, Error: s.Runtime.Error, Version: s.Runtime.Version,
		},
		Busy: s.Busy,
	}
}

func (s liveService) List(m *download.Manager) (api.AssistantStatus, error) {
	status, err := s.inner.List(m)
	if err != nil {
		return api.AssistantStatus{}, err
	}
	return liveStatus(status), nil
}

func (s liveService) StartDownload(m *download.Manager, id string) (download.Snapshot, error) {
	return s.inner.StartDownload(m, id)
}

func (s liveService) Install(m *download.Manager, id, downloadID string) (api.AssistantProfile, bool, error) {
	profile, installed, err := s.inner.Install(m, id, downloadID)
	if err != nil {
		return api.AssistantProfile{}, false, err
	}
	out := liveStatus(assistant.Status{Profiles: []assistant.Profile{profile}}).Profiles
	return out[0], installed, nil
}

func (s liveService) Activate(id string) error { return s.inner.Activate(id) }

func (s liveService) Remove(id string) error { return s.inner.Remove(id) }

func (s liveService) StartRuntimeDownload(m *download.Manager) (download.Snapshot, error) {
	return s.inner.StartRuntimeDownload(m)
}

func (s liveService) InstallRuntime(m *download.Manager, downloadID string) (bool, error) {
	return s.inner.InstallRuntime(m, downloadID)
}

func (s liveService) UseModel(ctx context.Context, id string, fn func(context.Context, api.AssistantModel) error) error {
	return s.inner.UseModel(ctx, id, func(ctx context.Context, model assistant.Model) error {
		return fn(ctx, api.AssistantModel{ID: model.ID, Path: model.Path, Binary: model.Binary, ContextSize: model.ContextSize, MaxTokens: model.MaxTokens})
	})
}

func (s liveService) Close() error { return s.inner.Close() }

func liveAssistantRunner(logger *log.Logger, gpuLayers int) api.ChatRunner {
	return liveRunner{logger: logger, gpuLayers: gpuLayers}
}

type liveRunner struct {
	logger    *log.Logger
	gpuLayers int
}

func (r liveRunner) Generate(ctx context.Context, req api.ChatRequest, onDelta func(string) error) error {
	eng := engine.New(engine.Options{Logger: r.logger, GPULayers: r.gpuLayers})
	defer eng.Close()
	messages := make([]engine.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, engine.Message{Role: m.Role, Content: m.Content})
	}
	return eng.Generate(ctx, engine.Request{
		Binary: req.Binary, ModelPath: req.ModelPath, ContextSize: req.ContextSize, MaxTokens: req.MaxTokens, Messages: messages,
	}, onDelta)
}
