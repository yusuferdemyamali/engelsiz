package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"engelsiz/internal/assistant/rag"
	"engelsiz/internal/download"
)

type assistantFake struct {
	status      AssistantStatus
	listErr     error
	snap        download.Snapshot
	downloadErr error
	profile     AssistantProfile
	installed   bool
	installErr  error
	activateErr error
	removeErr   error
	rtInstalled bool
	rtErr       error
	model       AssistantModel
	useModelErr error
	useModelID  string
	useModelFn  func(ctx context.Context, id string, fn func(context.Context, AssistantModel) error) error
	closed      bool
}

func (f *assistantFake) List(*download.Manager) (AssistantStatus, error) {
	return f.status, f.listErr
}

func (f *assistantFake) StartDownload(*download.Manager, string) (download.Snapshot, error) {
	return f.snap, f.downloadErr
}

func (f *assistantFake) Install(_ *download.Manager, _, _ string) (AssistantProfile, bool, error) {
	return f.profile, f.installed, f.installErr
}

func (f *assistantFake) Activate(string) error { return f.activateErr }

func (f *assistantFake) Remove(string) error { return f.removeErr }

func (f *assistantFake) StartRuntimeDownload(*download.Manager) (download.Snapshot, error) {
	return f.snap, f.rtErr
}

func (f *assistantFake) InstallRuntime(_ *download.Manager, _ string) (bool, error) {
	return f.rtInstalled, f.rtErr
}

func (f *assistantFake) UseModel(ctx context.Context, id string, fn func(context.Context, AssistantModel) error) error {
	f.useModelID = id
	if f.useModelFn != nil {
		return f.useModelFn(ctx, id, fn)
	}
	if f.useModelErr != nil {
		return f.useModelErr
	}
	return fn(ctx, f.model)
}

func (f *assistantFake) Close() error { f.closed = true; return nil }

type runnerFake struct {
	req    ChatRequest
	deltas []string
	err    error
	called bool
}

func (f *runnerFake) Generate(ctx context.Context, req ChatRequest, onDelta func(string) error) error {
	f.called = true
	f.req = req
	if f.err != nil {
		return f.err
	}
	for _, d := range f.deltas {
		if err := onDelta(d); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func chatDeps() (ChatDeps, *assistantFake, *runnerFake) {
	svc := &assistantFake{model: AssistantModel{ID: "lite", Path: "/m/model.gguf", Binary: "/r/server", ContextSize: 2048, MaxTokens: 512}}
	runner := &runnerFake{deltas: []string{"mer", "haba"}}
	deps := ChatDeps{
		Service: svc,
		Runner:  runner,
		Retrieve: func(ctx context.Context, query string, contextSize, maxTokens int) rag.Outcome {
			return rag.Outcome{
				Sources:     []rag.Source{{ID: "K1", Title: "Su", Href: "/content/content/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/Su", Excerpt: "suyu kaynatın"}},
				Evidence:    `[{"id":"K1","title":"Su","excerpt":"suyu kaynatın"}]`,
				Grounded:    true,
				ContextSize: contextSize,
			}
		},
		Logger: log.New(io.Discard, "", 0),
	}
	return deps, svc, runner
}

type sseEvent struct {
	name string
	data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("bad SSE chunk: %q", chunk)
		}
		out = append(out, sseEvent{strings.TrimPrefix(lines[0], "event: "), strings.TrimPrefix(lines[1], "data: ")})
	}
	return out
}

func chatRequest(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(body))
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestAssistantStatusContract(t *testing.T) {
	deps, _, _ := chatDeps()
	deps.Service.(*assistantFake).status = AssistantStatus{
		Profiles:             []AssistantProfile{{ID: "lite", ContextSize: 2048}},
		ActiveProfileID:      "lite",
		RecommendedProfileID: "lite",
		Runtime:              AssistantRuntime{Available: false, Status: "unavailable"},
	}
	handler := AssistantHandler(deps)
	req := httptest.NewRequest(http.MethodGet, "/api/assistant", nil)
	req.Host = "127.0.0.1:17321"
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	var got AssistantStatus
	if json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Profiles) != 1 || got.ActiveProfileID != "lite" {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestAssistantMissingServiceFailsClosed(t *testing.T) {
	deps := ChatDeps{Logger: log.New(io.Discard, "", 0)}
	handler := AssistantHandler(deps)
	req := httptest.NewRequest(http.MethodGet, "/api/assistant", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 503 {
		t.Fatalf("status = %d", rec.Code)
	}
	chat := chatRequest(t, handler, `{"prompt":"su?"}`)
	if chat.Code != 503 {
		t.Fatalf("chat = %d", chat.Code)
	}
}

func TestAssistantGuards(t *testing.T) {
	deps, _, _ := chatDeps()
	handler := AssistantHandler(deps)
	req := httptest.NewRequest(http.MethodGet, "/api/assistant", nil)
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 403 {
		t.Fatalf("origin = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "http://evil.example/api/assistant", nil)
	req.Host = "evil.example"
	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 403 {
		t.Fatalf("host = %d", rec.Code)
	}
	for _, body := range []string{
		`{"prompt":""}`,
		`{"prompt":"` + strings.Repeat("a", 2001) + `"}`,
		`{"prompt":"ok","extra":1}`,
		`{"prompt":"ok"} trailing`,
		`{"prompt":"a\u0000b"}`,
		`{"prompt":"ok","profile_id":"Balık"}`,
		`not json`,
	} {
		rec := chatRequest(t, handler, body)
		if rec.Code != 400 {
			t.Fatalf("body %q -> %d", body[:min(len(body), 40)], rec.Code)
		}
	}
}

func TestAssistantChatHappyPath(t *testing.T) {
	deps, svc, runner := chatDeps()
	handler := AssistantHandler(deps)
	rec := chatRequest(t, handler, `{"profile_id":"lite","prompt":"su nasıl güvenli olur?"}`)
	if rec.Code != 200 {
		t.Fatalf("chat = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("content-type = %s", ct)
	}
	events := parseSSE(t, rec.Body.String())
	var names []string
	for _, e := range events {
		names = append(names, e.name)
	}
	want := []string{"status", "status", "sources", "status", "delta", "delta", "done"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", names)
	}
	var states []string
	for _, e := range events {
		if e.name != "status" {
			continue
		}
		var v struct {
			State string `json:"state"`
		}
		if json.Unmarshal([]byte(e.data), &v) != nil {
			t.Fatal(e.data)
		}
		states = append(states, v.State)
	}
	if strings.Join(states, ",") != "searching,loading,generating" {
		t.Fatalf("states = %v", states)
	}
	var src struct {
		Sources []rag.Source `json:"sources"`
	}
	if json.Unmarshal([]byte(events[2].data), &src) != nil || len(src.Sources) != 1 || src.Sources[0].ID != "K1" {
		t.Fatalf("sources = %s", events[2].data)
	}
	var done struct {
		Grounded bool `json:"grounded"`
	}
	if json.Unmarshal([]byte(events[len(events)-1].data), &done) != nil || !done.Grounded {
		t.Fatalf("done = %s", events[len(events)-1].data)
	}
	if svc.useModelID != "lite" {
		t.Fatalf("profile = %q", svc.useModelID)
	}
	if runner.req.Binary != "/r/server" || runner.req.ModelPath != "/m/model.gguf" || runner.req.ContextSize != 2048 || runner.req.MaxTokens != 512 {
		t.Fatalf("runner req = %+v", runner.req)
	}
	if len(runner.req.Messages) != 2 || !strings.Contains(runner.req.Messages[0].Content, "güvenilmez") || !strings.Contains(runner.req.Messages[1].Content, `"id":"K1"`) {
		t.Fatalf("messages = %+v", runner.req.Messages)
	}
	for _, m := range runner.req.Messages {
		if strings.Contains(m.Content, "/content/") || strings.Contains(m.Content, "http") {
			t.Fatalf("href leaked into prompt: %q", m.Content)
		}
	}
}

func TestAssistantChatNoSourceDeterministic(t *testing.T) {
	deps, svc, runner := chatDeps()
	deps.Retrieve = func(context.Context, string, int, int) rag.Outcome { return rag.Outcome{} }
	var logs bytes.Buffer
	deps.Logger = log.New(&logs, "", 0)
	handler := AssistantHandler(deps)
	rec := chatRequest(t, handler, `{"prompt":"gizli-sorgu-xyz bilinmeyen konu?"}`)
	events := parseSSE(t, rec.Body.String())
	if len(events) != 5 {
		t.Fatalf("events = %+v", events)
	}
	var text struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(events[3].data), &text) != nil || text.Text != rag.NoEvidenceText {
		t.Fatalf("delta = %s", events[3].data)
	}
	var done struct {
		Grounded bool `json:"grounded"`
	}
	if json.Unmarshal([]byte(events[4].data), &done) != nil || done.Grounded {
		t.Fatalf("done = %s", events[4].data)
	}
	if runner.called {
		t.Fatal("runner must not run without sources")
	}
	if svc.useModelID != "" {
		t.Fatal("UseModel must not run without sources")
	}
	if strings.Contains(logs.String(), "gizli-sorgu-xyz") {
		t.Fatalf("prompt leaked to logs: %s", logs.String())
	}
}

func TestAssistantChatModelErrorEvent(t *testing.T) {
	deps, _, _ := chatDeps()
	deps.Service.(*assistantFake).useModelErr = &download.Error{Code: "busy", Message: "Asistan şu anda başka bir işlemle meşgul. İşlem bitince tekrar deneyin."}
	handler := AssistantHandler(deps)
	rec := chatRequest(t, handler, `{"prompt":"su?"}`)
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "error" {
		t.Fatalf("last = %+v", last)
	}
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(last.data), &v) != nil || v.Error.Code != "busy" {
		t.Fatalf("error = %s", last.data)
	}
}

func TestAssistantChatClientCancel(t *testing.T) {
	deps, _, _ := chatDeps()
	deps.Runner = &runnerFake{err: context.Canceled}
	handler := AssistantHandler(deps)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(`{"prompt":"su?"}`)).WithContext(ctx)
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestAssistantProfileRoutes(t *testing.T) {
	deps, _, _ := chatDeps()
	handler := AssistantHandler(deps)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Host = "127.0.0.1"
		if method == http.MethodPost || method == http.MethodDelete {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	if rec := do("POST", "/api/assistant/profiles/lite/download", "{}"); rec.Code != 201 {
		t.Fatalf("download = %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/assistant/profiles/lite/install", `{"download_id":"d1"}`); rec.Code != 202 {
		t.Fatalf("install = %d %s", rec.Code, rec.Body)
	}
	deps.Service.(*assistantFake).installed = true
	if rec := do("POST", "/api/assistant/profiles/lite/install", `{"download_id":"d1"}`); rec.Code != 200 {
		t.Fatalf("install sync = %d", rec.Code)
	}
	if rec := do("POST", "/api/assistant/profiles/lite/activate", "{}"); rec.Code != 200 {
		t.Fatalf("activate = %d", rec.Code)
	}
	if rec := do("DELETE", "/api/assistant/profiles/lite", ""); rec.Code != 200 {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/assistant/runtime/download", "{}"); rec.Code != 201 {
		t.Fatalf("runtime download = %d", rec.Code)
	}
	if rec := do("POST", "/api/assistant/runtime/install", `{"download_id":"d1"}`); rec.Code != 202 {
		t.Fatalf("runtime install = %d", rec.Code)
	}
	if rec := do("POST", "/api/assistant/profiles/lite/install", `{"download_id":""}`); rec.Code != 400 {
		t.Fatalf("empty id = %d", rec.Code)
	}
	if rec := do("POST", "/api/assistant/profiles/lite/download", `{"x":1}`); rec.Code != 400 {
		t.Fatalf("non-empty = %d", rec.Code)
	}
	deps.Service.(*assistantFake).downloadErr = errors.New("boom private http://127.0.0.1:9")
	if rec := do("POST", "/api/assistant/profiles/lite/download", "{}"); rec.Code != 500 || strings.Contains(rec.Body.String(), "boom") || strings.Contains(rec.Body.String(), "127.0.0.1") {
		t.Fatalf("leak: %d %s", rec.Code, rec.Body)
	}
}

func TestAssistantWindowResolution(t *testing.T) {
	st := AssistantStatus{
		Profiles:        []AssistantProfile{{ID: "lite", ContextSize: 2048}, {ID: "balanced", ContextSize: 4096}},
		ActiveProfileID: "lite",
	}
	if cs, ok := assistantWindow(st, "balanced"); !ok || cs != 4096 {
		t.Fatalf("explicit = %d %v", cs, ok)
	}
	if cs, ok := assistantWindow(st, ""); !ok || cs != 2048 {
		t.Fatalf("active = %d %v", cs, ok)
	}
	for _, tc := range []struct {
		status AssistantStatus
		id     string
	}{
		{AssistantStatus{}, ""},
		{st, "nope"},
		{AssistantStatus{Profiles: []AssistantProfile{{ID: "lite"}}}, "lite"},
	} {
		if _, ok := assistantWindow(tc.status, tc.id); ok {
			t.Fatalf("must skip pre-check: %+v %q", tc.status, tc.id)
		}
	}
}

func TestAssistantChatLiteTooLongRejected(t *testing.T) {
	deps, _, runner := chatDeps()
	deps.Service.(*assistantFake).status = AssistantStatus{
		Profiles:        []AssistantProfile{{ID: "lite", ContextSize: 2048}},
		ActiveProfileID: "lite",
	}
	handler := AssistantHandler(deps)
	rec := chatRequest(t, handler, `{"prompt":"`+strings.Repeat("ğ", 2000)+`"}`)
	if rec.Code != 400 {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error.Code != "invalid_request" || body.Error.Message != "Lite bağlamı için soruyu kısaltın." {
		t.Fatalf("body = %s", rec.Body)
	}
	if runner.called {
		t.Fatal("runner must not run for an over-long question")
	}
}

func TestAssistantChatInStreamTooLong(t *testing.T) {
	deps, _, runner := chatDeps()
	deps.Retrieve = func(context.Context, string, int, int) rag.Outcome {
		return rag.Outcome{TooLong: true, ContextSize: 2048}
	}
	handler := AssistantHandler(deps)
	rec := chatRequest(t, handler, `{"prompt":"su?"}`)
	events := parseSSE(t, rec.Body.String())
	if len(events) != 3 || events[0].name != "status" || events[1].name != "status" || events[2].name != "error" {
		t.Fatalf("events = %+v", events)
	}
	var v struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(events[2].data), &v) != nil || v.Error.Code != "invalid_request" || v.Error.Message != "Lite bağlamı için soruyu kısaltın." {
		t.Fatalf("error = %s", events[2].data)
	}
	if runner.called {
		t.Fatal("runner must not run when the question cannot fit")
	}
}
