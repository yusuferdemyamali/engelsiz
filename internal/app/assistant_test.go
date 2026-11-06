package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"engelsiz/internal/api"
	"engelsiz/internal/assistant/rag"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/providers"
)

var errTestAssistant = errors.New("assistant test backend failure")

type assistantTestService struct{ closed bool }

func (s *assistantTestService) List(*download.Manager) (api.AssistantStatus, error) {
	return api.AssistantStatus{}, nil
}

func (s *assistantTestService) StartDownload(*download.Manager, string) (download.Snapshot, error) {
	return download.Snapshot{}, nil
}

func (s *assistantTestService) Install(_ *download.Manager, _, _ string) (api.AssistantProfile, bool, error) {
	return api.AssistantProfile{}, false, nil
}

func (s *assistantTestService) Activate(string) error { return nil }

func (s *assistantTestService) Remove(string) error { return nil }

func (s *assistantTestService) StartRuntimeDownload(*download.Manager) (download.Snapshot, error) {
	return download.Snapshot{}, nil
}

func (s *assistantTestService) InstallRuntime(_ *download.Manager, _ string) (bool, error) {
	return false, nil
}

func (s *assistantTestService) UseModel(ctx context.Context, _ string, fn func(context.Context, api.AssistantModel) error) error {
	return fn(ctx, api.AssistantModel{})
}

func (s *assistantTestService) Close() error { s.closed = true; return nil }

type assistantTestRunner struct{}

func (assistantTestRunner) Generate(context.Context, api.ChatRequest, func(string) error) error {
	return nil
}

type assistantTestProvider struct {
	article providers.Article
	err     error
	pkg     string
	href    string
}

func (f *assistantTestProvider) Catalog(context.Context) ([]providers.CatalogPackage, error) {
	return nil, nil
}

func (f *assistantTestProvider) EnsureStarted(context.Context) error { return nil }

func (f *assistantTestProvider) Stop(context.Context) error { return nil }

func (f *assistantTestProvider) Restart(context.Context) error { return nil }

func (f *assistantTestProvider) Status() providers.RuntimeStatus {
	return providers.RuntimeStatus{State: providers.Ready}
}

func (f *assistantTestProvider) ReadArticle(_ context.Context, pkg, href string) (providers.Article, error) {
	f.pkg, f.href = pkg, href
	if f.err != nil {
		return providers.Article{}, f.err
	}
	return f.article, nil
}

type assistantTestPlainProvider struct{}

func (assistantTestPlainProvider) Catalog(context.Context) ([]providers.CatalogPackage, error) {
	return nil, nil
}

func (assistantTestPlainProvider) EnsureStarted(context.Context) error { return nil }

func (assistantTestPlainProvider) Stop(context.Context) error { return nil }

func (assistantTestPlainProvider) Restart(context.Context) error { return nil }

func (assistantTestPlainProvider) Status() providers.RuntimeStatus { return providers.RuntimeStatus{} }

type assistantTestServing struct{}

func (assistantTestServing) ReplaceZIMPaths(context.Context, []string, bool) error { return nil }

func (assistantTestServing) Snapshot() (providers.RuntimeStatus, []string) {
	return providers.RuntimeStatus{}, nil
}

func (assistantTestServing) ContentTransport() (http.RoundTripper, *url.URL, error) {
	return nil, nil, nil
}

func TestActivationReadArticleGuard(t *testing.T) {
	article := providers.Article{PackageID: "p", Href: "/content/content/p/A", Body: []byte("<p>x</p>")}
	newActive := func() (*activation, *assistantTestProvider) {
		prov := &assistantTestProvider{article: article}
		return &activation{ctx: context.Background(), provider: prov, serving: assistantTestServing{}}, prov
	}
	a, prov := newActive()
	got, err := a.ReadArticle(context.Background(), "p", "/content/content/p/A")
	if err != nil || string(got.Body) != "<p>x</p>" || prov.pkg != "p" || prov.href != "/content/content/p/A" {
		t.Fatalf("delegate = %+v %v", got, err)
	}
	a, prov = newActive()
	a.staging = true
	if _, err := a.ReadArticle(context.Background(), "p", "/content/content/p/A"); !errors.Is(err, providers.ErrRuntimeUnavailable) || prov.pkg != "" {
		t.Fatalf("staging = %v pkg=%q", err, prov.pkg)
	}
	a, _ = newActive()
	a.serving = nil
	if _, err := a.ReadArticle(context.Background(), "p", "/content/content/p/A"); !errors.Is(err, providers.ErrRuntimeUnavailable) {
		t.Fatalf("no serving = %v", err)
	}
	a, _ = newActive()
	a.provider = &assistantTestPlainProvider{}
	if _, err := a.ReadArticle(context.Background(), "p", "/content/content/p/A"); !errors.Is(err, providers.ErrRuntimeUnavailable) {
		t.Fatalf("plain provider = %v", err)
	}
	a, _ = newActive()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.ctx = ctx
	if _, err := a.ReadArticle(context.Background(), "p", "/content/content/p/A"); !errors.Is(err, providers.ErrRuntimeUnavailable) {
		t.Fatalf("canceled = %v", err)
	}
}

func TestAssistantConfigFromEnv(t *testing.T) {
	t.Setenv("ENGELSIZ_LLAMA_BINARY", "/opt/llama/server")
	t.Setenv("ENGELSIZ_LLAMA_GPU_LAYERS", "12")
	cfg := assistantConfigFromEnv()
	if cfg.Binary != "/opt/llama/server" || cfg.GPULayers != 12 {
		t.Fatalf("config = %+v", cfg)
	}
	for _, raw := range []string{"", "  ", "abc", "-1", "0", "1000", "12.5"} {
		t.Setenv("ENGELSIZ_LLAMA_GPU_LAYERS", raw)
		if got := assistantConfigFromEnv().GPULayers; got != 0 {
			t.Fatalf("layers %q -> %d, want CPU baseline 0", raw, got)
		}
	}
	t.Setenv("ENGELSIZ_LLAMA_BINARY", "")
	if assistantConfigFromEnv().Binary != "" {
		t.Fatal("empty binary must stay empty")
	}
}

func TestAssistantDisabledWhenSeamsUnset(t *testing.T) {
	oldBackend, oldRunner := newAssistantBackend, newAssistantRunner
	defer func() { newAssistantBackend, newAssistantRunner = oldBackend, oldRunner }()
	newAssistantBackend, newAssistantRunner = nil, nil
	t.Setenv("ENGELSIZ_LLAMA_BINARY", "")
	t.Setenv("ENGELSIZ_LLAMA_GPU_LAYERS", "")
	rt := openAssistant(log.New(io.Discard, "", 0), nil, t.TempDir(), nil, nil, nil, nil, nil)
	if rt == nil || rt.service != nil {
		t.Fatal("disabled runtime must have nil service")
	}
	if err := rt.close(); err != nil {
		t.Fatalf("close disabled = %v", err)
	}
	if rt.deps.Retrieve == nil {
		t.Fatal("retrieve closure must always be set")
	}
	if out := rt.deps.Retrieve(context.Background(), "su?", 2048, 512); out.Grounded {
		t.Fatal("nil inputs must degrade to ungrounded")
	}
	handler := rt.handler()
	req := httptest.NewRequest(http.MethodGet, "/api/assistant", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 503 {
		t.Fatalf("disabled status = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(`{"prompt":"su?"}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 503 {
		t.Fatalf("disabled chat = %d", rec.Code)
	}
}

func TestAssistantBackendFailureKeepsAppBooting(t *testing.T) {
	oldBackend := newAssistantBackend
	oldRunner := newAssistantRunner
	defer func() { newAssistantBackend, newAssistantRunner = oldBackend, oldRunner }()
	newAssistantBackend = func(*sql.DB, string, assistantConfig, *log.Logger) (api.AssistantService, error) {
		return nil, errTestAssistant
	}
	newAssistantRunner = func(*log.Logger, int) api.ChatRunner { return nil }
	rt := openAssistant(log.New(io.Discard, "", 0), nil, t.TempDir(), nil, nil, nil, nil, nil)
	if rt.service != nil {
		t.Fatal("failed backend must disable service")
	}
	if err := rt.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantSeamWiresServiceAndRunner(t *testing.T) {
	oldBackend := newAssistantBackend
	oldRunner := newAssistantRunner
	defer func() { newAssistantBackend, newAssistantRunner = oldBackend, oldRunner }()
	svc := &assistantTestService{}
	var gotLayers int
	newAssistantBackend = func(_ *sql.DB, _ string, cfg assistantConfig, _ *log.Logger) (api.AssistantService, error) {
		if cfg.Binary != "/b/server" {
			t.Fatalf("binary = %q", cfg.Binary)
		}
		return svc, nil
	}
	newAssistantRunner = func(_ *log.Logger, layers int) api.ChatRunner {
		gotLayers = layers
		return &assistantTestRunner{}
	}
	t.Setenv("ENGELSIZ_LLAMA_BINARY", "/b/server")
	t.Setenv("ENGELSIZ_LLAMA_GPU_LAYERS", "7")
	rt := openAssistant(log.New(io.Discard, "", 0), nil, t.TempDir(), nil, nil, nil, nil, nil)
	if rt.service == nil || gotLayers != 7 {
		t.Fatalf("seam wiring failed: %+v layers=%d", rt.service, gotLayers)
	}
	if err := rt.close(); err != nil || !svc.closed {
		t.Fatalf("close = %v closed=%v", err, svc.closed)
	}
	_ = rag.MaxHits
}

func TestAssistantLiveSeams(t *testing.T) {
	if newAssistantBackend == nil || newAssistantRunner == nil {
		t.Fatal("live seams must be set by assistant_live.go")
	}
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc, err := newAssistantBackend(db, dataRoot, assistantConfig{}, log.New(io.Discard, "", 0))
	if err != nil || svc == nil {
		t.Fatalf("backend = %v %v", svc, err)
	}
	status, err := svc.List(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Profiles) != 3 || status.Profiles[0].ID != "lite" || status.Profiles[0].ContextSize != 2048 {
		t.Fatalf("status = %+v", status)
	}
	if newAssistantRunner(log.New(io.Discard, "", 0), 0) == nil {
		t.Fatal("runner must be set")
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	_ = rag.MaxHits
}
