// Assistant HTTP integration checks against the real manager service
// contract. The stack is real (assistant service with test pins, download
// manager, packages service, openAssistant wiring, HTTP handler); only the
// model bytes, the runtime archive and inference itself are faked (loopback
// httptest server, fake executable binary, fake runner). No network beyond
// loopback, no UI.
package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"engelsiz/internal/api"
	"engelsiz/internal/assistant"
	"engelsiz/internal/assistant/rag"
	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

type assistantITSearch struct {
	mu       sync.Mutex
	hits     map[string]providers.SearchResponse
	articles map[string]string
}

func (f *assistantITSearch) Search(_ context.Context, id, q string) (providers.SearchResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if res, ok := f.hits[id]; ok {
		return res, nil
	}
	return providers.SearchResponse{Query: q, PackageID: id, Results: []providers.SearchResult{}}, nil
}

func (f *assistantITSearch) ReadArticle(_ context.Context, pkg, href string) (providers.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.articles[href]
	if !ok {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	return providers.Article{PackageID: pkg, Href: href, ContentType: "text/html", Body: []byte(body)}, nil
}

type assistantITRunner struct {
	mu     sync.Mutex
	req    api.ChatRequest
	deltas []string
	called bool
}

func (f *assistantITRunner) Generate(ctx context.Context, req api.ChatRequest, onDelta func(string) error) error {
	f.mu.Lock()
	f.req, f.called = req, true
	f.mu.Unlock()
	for _, d := range f.deltas {
		if err := onDelta(d); err != nil {
			return err
		}
	}
	return ctx.Err()
}

type assistantITFixture struct {
	t       *testing.T
	dir     string
	db      *sql.DB
	mgr     *download.Manager
	pkgs    *packages.Service
	rt      *assistantRuntime
	handler http.HandlerFunc
	search  *assistantITSearch
	runner  *assistantITRunner
	pins    map[string]assistant.Acquisition
	data    map[string][]byte
	binary  string
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func assistantITBinary(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "fake-llama-server")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func assistantITServerBinary() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func assistantITLib() string {
	if runtime.GOOS == "windows" {
		return "libggml.dll"
	}
	return "libggml.so"
}

func assistantITTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, body []byte, mode int64, link string) {
		hdr := &tar.Header{Name: name, Mode: mode}
		if link != "" {
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = link
		} else if body == nil {
			hdr.Typeflag = tar.TypeDir
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if body != nil {
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("pkg/", nil, 0o755, "")
	write("pkg/"+assistantITServerBinary(), []byte("MZ-fake-server"), 0o755, "")
	write("pkg/lib/", nil, 0o755, "")
	write("pkg/lib/"+assistantITLib(), []byte("fake-lib"), 0o644, "")
	if runtime.GOOS != "windows" {
		write("pkg/lib/link", nil, 0o777, "libggml.so")
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func setupAssistantIT(t *testing.T) *assistantITFixture {
	t.Helper()
	fx := &assistantITFixture{t: t, data: map[string][]byte{}, pins: map[string]assistant.Acquisition{}}
	fx.dir = t.TempDir()
	for id, body := range map[string][]byte{
		"lite":     []byte("fake-gguf-lite-" + strings.Repeat("A", 4096)),
		"balanced": []byte("fake-gguf-balanced-" + strings.Repeat("B", 8192)),
		"quality":  []byte("fake-gguf-quality-" + strings.Repeat("C", 8192)),
	} {
		fx.data[id] = body
	}
	fx.data["runtime"] = assistantITTar(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := fx.data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	contexts := map[string]int{"lite": 2048, "balanced": 4096, "quality": 4096}
	tokens := map[string]int{"lite": 512, "balanced": 1024, "quality": 1024}
	for id, zb := range fx.data {
		if id == "runtime" {
			continue
		}
		fx.pins[id] = assistant.Acquisition{
			Title: "Test " + id, ModelName: "Qwen3-Test", Quantization: "Q4_K_M",
			Version: "v-test", FileName: id + ".gguf",
			SourceURL:  server.URL + "/" + id,
			TotalBytes: int64(len(zb)), ExpectedSize: int64(len(zb)),
			ChecksumValue: shaHex(zb), ContextSize: contexts[id], MaxTokens: tokens[id],
			RAMGB: 2, License: "Apache-2.0", Source: "test",
		}
	}
	rtPin := assistant.RuntimeAcquisition{
		Version: "b-test", FileName: "rt.tar.gz", SourceURL: server.URL + "/runtime",
		TotalBytes: int64(len(fx.data["runtime"])), ExpectedSize: int64(len(fx.data["runtime"])),
		ChecksumValue: shaHex(fx.data["runtime"]), Format: "tar.gz",
	}
	fx.binary = assistantITBinary(t, fx.dir)
	db, err := database.Open(filepath.Join(fx.dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mgr, err := download.New(db, filepath.Join(fx.dir, "downloads"), download.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })
	pkgs, err := packages.New(context.Background(), db, fx.dir, packages.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkgs.Close() })
	oldBackend, oldRunner := newAssistantBackend, newAssistantRunner
	t.Cleanup(func() { newAssistantBackend, newAssistantRunner = oldBackend, oldRunner })
	newAssistantBackend = func(db *sql.DB, dataRoot string, cfg assistantConfig, logger *log.Logger) (api.AssistantService, error) {
		cfg.Binary = fx.binary
		svc, err := assistant.New(db, dataRoot, assistant.Options{Binary: cfg.Binary, Logger: logger, Pins: fx.pins, RuntimePin: &rtPin})
		if err != nil {
			return nil, err
		}
		return liveService{inner: svc}, nil
	}
	fx.search = &assistantITSearch{hits: map[string]providers.SearchResponse{}, articles: map[string]string{}}
	fx.runner = &assistantITRunner{deltas: []string{"yapay ", "yanıt"}}
	newAssistantRunner = func(*log.Logger, int) api.ChatRunner { return fx.runner }
	t.Setenv("ENGELSIZ_LLAMA_BINARY", "")
	t.Setenv("ENGELSIZ_LLAMA_GPU_LAYERS", "")
	fx.mgr, fx.pkgs, fx.db = mgr, pkgs, db
	fx.rt = openAssistant(log.New(io.Discard, "", 0), db, fx.dir, mgr, pkgs, nil, fx.search, fx.search)
	fx.handler = fx.rt.handler()
	t.Cleanup(func() { fx.rt.close() })
	return fx
}

func (fx *assistantITFixture) do(method, path, body string) *httptest.ResponseRecorder {
	fx.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:17321"
	if method == http.MethodPost || method == http.MethodDelete {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	fx.handler(rec, req)
	return rec
}

func (fx *assistantITFixture) status(t *testing.T) api.AssistantStatus {
	t.Helper()
	rec := fx.do("GET", "/api/assistant", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	var st api.AssistantStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func (fx *assistantITFixture) statusKeys(t *testing.T) map[string]any {
	t.Helper()
	rec := fx.do("GET", "/api/assistant", "")
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func (fx *assistantITFixture) waitDownload(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := fx.mgr.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		switch snap.Status {
		case "completed":
			return
		case "failed", "cancelled":
			t.Fatalf("download %s ended: %+v", snap.Status, snap.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("download %s did not complete", id)
}

func (fx *assistantITFixture) waitProfile(t *testing.T, id, want string) api.AssistantProfile {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range fx.status(t).Profiles {
			if p.ID == id && p.Status == want {
				return p
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("profile %s did not become %s", id, want)
	return api.AssistantProfile{}
}

func (fx *assistantITFixture) waitRuntime(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if fx.status(t).Runtime.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runtime did not become %s", want)
}

func downloadIDOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var v struct {
		Download download.Snapshot `json:"download"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Download.ID == "" {
		t.Fatalf("no download id: %s %v", rec.Body, err)
	}
	return v.Download.ID
}

type assistantITSSE struct {
	event string
	data  string
}

func parseITsse(t *testing.T, body string) []assistantITSSE {
	t.Helper()
	var out []assistantITSSE
	for _, chunk := range strings.Split(body, "\n\n") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("bad SSE chunk: %q", chunk)
		}
		out = append(out, assistantITSSE{strings.TrimPrefix(lines[0], "event: "), strings.TrimPrefix(lines[1], "data: ")})
	}
	return out
}

func sseField(t *testing.T, data, field string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatal(err)
	}
	s, _ := v[field].(string)
	if s == "" {
		if nested, ok := v[field].(map[string]any); ok {
			s, _ = nested["code"].(string)
		}
	}
	return s
}

func TestAssistantIntegration(t *testing.T) {
	fx := setupAssistantIT(t)

	// A. Status contract keys against the real service.
	raw := fx.statusKeys(t)
	for _, key := range []string{"profiles", "active_profile_id", "recommended_profile_id", "hardware", "runtime", "busy"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("status missing key %q: %v", key, raw)
		}
	}
	st := fx.status(t)
	if len(st.Profiles) != 3 {
		t.Fatalf("profiles = %+v", st.Profiles)
	}
	ids := map[string]bool{}
	for _, p := range st.Profiles {
		ids[p.ID] = true
		if p.ContextSize <= 0 || p.Title == "" || p.License == "" || p.Source == "" {
			t.Fatalf("profile = %+v", p)
		}
	}
	if !ids["lite"] || !ids["balanced"] || !ids["quality"] {
		t.Fatalf("profile ids = %v", ids)
	}
	if !ids[st.RecommendedProfileID] {
		t.Fatalf("recommended = %q", st.RecommendedProfileID)
	}
	if st.Hardware.CPUCount < 1 {
		t.Fatalf("hardware = %+v", st.Hardware)
	}

	// B. Unknown profile and malformed bodies fail typed, offline.
	rec := fx.do("POST", "/api/assistant/profiles/nope/download", "{}")
	if rec.Code != 404 {
		t.Fatalf("unknown download = %d %s", rec.Code, rec.Body)
	}
	if rec := fx.do("POST", "/api/assistant/profiles/lite/install", `{"download_id":""}`); rec.Code != 400 {
		t.Fatalf("empty id = %d", rec.Code)
	}
	if rec := fx.do("DELETE", "/api/assistant/profiles/nope", ""); rec.Code != 404 {
		t.Fatalf("unknown remove = %d %s", rec.Code, rec.Body)
	}

	// C. Model lifecycle: download -> install -> activate, all offline.
	rec = fx.do("POST", "/api/assistant/profiles/lite/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("download = %d %s", rec.Code, rec.Body)
	}
	dlID := downloadIDOf(t, rec)
	fx.waitDownload(t, dlID)
	rec = fx.do("POST", "/api/assistant/profiles/lite/install", `{"download_id":`+strconv.Quote(dlID)+`}`)
	if rec.Code != 202 && rec.Code != 200 {
		t.Fatalf("install = %d %s", rec.Code, rec.Body)
	}
	fx.waitProfile(t, "lite", "ready")
	if rec := fx.do("POST", "/api/assistant/profiles/lite/activate", "{}"); rec.Code != 200 {
		t.Fatalf("activate = %d %s", rec.Code, rec.Body)
	}
	if got := fx.status(t).ActiveProfileID; got != "lite" {
		t.Fatalf("active = %q", got)
	}

	// D. Install one ZIM so retrieval has a verified snapshot member.
	zimBody := []byte("fake zim for assistant retrieval")
	zimID := fmt.Sprintf("%032x", 77)
	if err := os.WriteFile(filepath.Join(fx.dir, "downloads", zimID+".part"), zimBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		zimID, "kiwix", "urn:uuid:"+zimID, "Vikipedi", "mini", "selected.zim", "https://private.example/source.zim",
		filepath.Join(fx.dir, "downloads", zimID+".part"), len(zimBody), len(zimBody), "completed",
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "wikipedia", "tr", "2026-01-01", len(zimBody), "sha256", shaHex(zimBody)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.pkgs.Install(fx.mgr, zimID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	installed := false
	for time.Now().Before(deadline) {
		files, err := fx.pkgs.InstalledSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if f.PackageID == zimID {
				installed = true
			}
		}
		if installed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !installed {
		t.Fatal("zim did not install")
	}

	// E. Chat without evidence is deterministic and never touches the model.
	rec = fx.do("POST", "/api/assistant/chat", `{"prompt":"kurulu içerikte olmayan bir konu?"}`)
	if rec.Code != 200 {
		t.Fatalf("chat = %d %s", rec.Code, rec.Body)
	}
	events := parseITsse(t, rec.Body.String())
	if len(events) != 5 || events[0].event != "status" || events[2].event != "sources" {
		t.Fatalf("events = %+v", events)
	}
	if sseField(t, events[3].data, "text") != rag.NoEvidenceText {
		t.Fatalf("delta = %s", events[3].data)
	}
	var done struct {
		Grounded bool `json:"grounded"`
	}
	if err := json.Unmarshal([]byte(events[4].data), &done); err != nil || done.Grounded {
		t.Fatalf("done = %s", events[4].data)
	}
	fx.runner.mu.Lock()
	runnerCalled := fx.runner.called
	fx.runner.mu.Unlock()
	if runnerCalled {
		t.Fatal("runner must not run without sources")
	}

	// F. Chat with evidence runs the real UseModel lease into the runner.
	href := "/content/content/" + zimID + "/Su"
	fx.search.mu.Lock()
	fx.search.hits[zimID] = providers.SearchResponse{Query: "su", PackageID: zimID, TotalResults: 1, Results: []providers.SearchResult{
		{Title: "Su güvenliği", Snippet: "kısa özet", Source: "Viki", ArticleID: "Su", Href: href},
	}}
	fx.search.articles[href] = `<html><body><h1>Su</h1><p>Depremde suyu kaynatın.</p></body></html>`
	fx.search.mu.Unlock()
	rec = fx.do("POST", "/api/assistant/chat", `{"prompt":"depremde su nasıl güvenli olur?"}`)
	events = parseITsse(t, rec.Body.String())
	names := make([]string, 0, len(events))
	for _, e := range events {
		names = append(names, e.event)
	}
	if strings.Join(names, ",") != "status,status,sources,status,delta,delta,done" {
		t.Fatalf("events = %v", names)
	}
	var src struct {
		Sources []rag.Source `json:"sources"`
	}
	if err := json.Unmarshal([]byte(events[2].data), &src); err != nil || len(src.Sources) != 1 || src.Sources[0].Href != href {
		t.Fatalf("sources = %s", events[2].data)
	}
	if err := json.Unmarshal([]byte(events[len(events)-1].data), &done); err != nil || !done.Grounded {
		t.Fatalf("done = %s", events[len(events)-1].data)
	}
	fx.runner.mu.Lock()
	req := fx.runner.req
	fx.runner.mu.Unlock()
	if req.Binary != fx.binary || !strings.HasSuffix(req.ModelPath, filepath.Join("lite", "model.gguf")) || req.ContextSize != 2048 || req.MaxTokens != 512 {
		t.Fatalf("runner req = %+v", req)
	}
	if len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, `"id":"K1"`) {
		t.Fatalf("messages = %+v", req.Messages)
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "/content/") {
			t.Fatalf("href leaked into prompt: %q", m.Content)
		}
	}

	// F2. Lite rejects an over-long question with invalid_request, not a
	// fabricated no-evidence answer.
	rec = fx.do("POST", "/api/assistant/chat", `{"profile_id":"lite","prompt":"`+strings.Repeat("ğ", 2000)+`"}`)
	if rec.Code != 400 {
		t.Fatalf("too-long chat = %d %s", rec.Code, rec.Body)
	}
	var tooLong struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tooLong); err != nil || tooLong.Error.Code != "invalid_request" || tooLong.Error.Message != "Lite bağlamı için soruyu kısaltın." {
		t.Fatalf("too-long body = %s", rec.Body)
	}

	// G. Unknown profile surfaces a typed in-stream error; runner stays idle.
	fx.runner.mu.Lock()
	fx.runner.called = false
	fx.runner.mu.Unlock()
	rec = fx.do("POST", "/api/assistant/chat", `{"profile_id":"nope","prompt":"su?"}`)
	events = parseITsse(t, rec.Body.String())
	last := events[len(events)-1]
	if last.event != "error" || sseField(t, last.data, "error") != "not_found" {
		t.Fatalf("last = %+v", last)
	}
	fx.runner.mu.Lock()
	runnerCalled = fx.runner.called
	fx.runner.mu.Unlock()
	if runnerCalled {
		t.Fatal("runner must not run for unknown profile")
	}

	// H. Runtime lifecycle: download -> install -> ready status.
	rec = fx.do("POST", "/api/assistant/runtime/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("runtime download = %d %s", rec.Code, rec.Body)
	}
	rtDlID := downloadIDOf(t, rec)
	fx.waitDownload(t, rtDlID)
	rec = fx.do("POST", "/api/assistant/runtime/install", `{"download_id":`+strconv.Quote(rtDlID)+`}`)
	if rec.Code != 202 && rec.Code != 200 {
		t.Fatalf("runtime install = %d %s", rec.Code, rec.Body)
	}
	fx.waitRuntime(t, "ready")

	// I. Remove disables the profile; chat degrades to a typed error.
	if rec := fx.do("DELETE", "/api/assistant/profiles/lite", ""); rec.Code != 200 {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	removed := true
	for _, p := range fx.status(t).Profiles {
		if p.ID == "lite" && p.Status == "ready" {
			removed = false
		}
	}
	if !removed {
		t.Fatal("lite still ready after remove")
	}
	rec = fx.do("POST", "/api/assistant/chat", `{"prompt":"depremde su nasıl güvenli olur?"}`)
	events = parseITsse(t, rec.Body.String())
	last = events[len(events)-1]
	if last.event != "error" {
		t.Fatalf("expected error after remove, got %+v", events)
	}
	if code := sseField(t, last.data, "error"); code != "invalid_state" && code != "not_found" {
		t.Fatalf("code = %q", code)
	}
}
