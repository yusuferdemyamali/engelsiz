package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/translation"
)

func translationZip(t *testing.T, from, to string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	entries := []struct {
		name, body string
		dir        bool
	}{
		{name: "pkg/", dir: true},
		{name: "pkg/metadata.json", body: `{"from_code":"` + from + `","to_code":"` + to + `","package_version":"9.9.9-test"}`},
		{name: "pkg/sentencepiece.model", body: "fake-sentencepiece"},
		{name: "pkg/model/", dir: true},
		{name: "pkg/model/model.bin", body: "fake-model-payload"},
		{name: "pkg/model/shared_vocabulary.txt", body: "fake-vocab"},
	}
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store}
		if e.dir {
			h.SetMode(os.ModeDir | 0o755)
			if _, err := w.CreateHeader(h); err != nil {
				t.Fatal(err)
			}
			continue
		}
		h.SetMode(0o644)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type translationFixture struct {
	handler http.HandlerFunc
	svc     *translation.Service
	mgr     *download.Manager
	data    map[string][]byte
}

func setupTranslation(t *testing.T) *translationFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := download.New(db, filepath.Join(dir, "downloads"), download.Options{})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	data := map[string][]byte{
		translation.ModelTREN: translationZip(t, "tr", "en"),
		translation.ModelENTR: translationZip(t, "en", "tr"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	pins := map[string]translation.Acquisition{}
	for id, zb := range data {
		sum := sha256.Sum256(zb)
		pins[id] = translation.Acquisition{
			Title: "Test", Version: "9.9.9-test", FileName: id + ".zip",
			SourceURL:  server.URL + "/" + id,
			TotalBytes: int64(len(zb)), ExpectedSize: int64(len(zb)),
			ChecksumValue: hex.EncodeToString(sum[:]),
		}
	}
	svc, err := translation.New(db, dir, translation.Options{Binary: translationWorker(t), Pins: pins})
	if err != nil {
		server.Close()
		mgr.Close()
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close(); server.Close(); mgr.Close(); db.Close() })
	return &translationFixture{handler: TranslationHandler(svc, mgr), svc: svc, mgr: mgr, data: data}
}

func callTranslation(t *testing.T, h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func waitTranslationDownload(t *testing.T, m *download.Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not complete")
}

func waitTranslationModel(t *testing.T, fx *translationFixture, id, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := fx.svc.List(fx.mgr)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range st.Models {
			if m.ID == id && m.Status == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("model %s did not become %s", id, want)
}

func installTranslationModel(t *testing.T, fx *translationFixture, id string) {
	t.Helper()
	rec := callTranslation(t, fx.handler, "POST", "/api/translation/models/"+id+"/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("download %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Download download.Snapshot `json:"download"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Download.Provider != "argos" {
		t.Fatalf("provider %+v", created.Download)
	}
	waitTranslationDownload(t, fx.mgr, created.Download.ID)
	rec = callTranslation(t, fx.handler, "POST", "/api/translation/models/"+id+"/install", `{"download_id":"`+created.Download.ID+`"}`)
	if rec.Code != 202 {
		t.Fatalf("install %d %s", rec.Code, rec.Body)
	}
	waitTranslationModel(t, fx, id, "ready")
}

func TestTranslationHTTPStatus(t *testing.T) {
	fx := setupTranslation(t)
	rec := callTranslation(t, fx.handler, "GET", "/api/translation", "")
	if rec.Code != 200 {
		t.Fatalf("GET %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache header missing")
	}
	var status struct {
		Models []struct {
			ID             string             `json:"id"`
			SourceLanguage string             `json:"source_language"`
			TargetLanguage string             `json:"target_language"`
			Version        string             `json:"version"`
			SizeBytes      int64              `json:"size_bytes"`
			Status         string             `json:"status"`
			Download       *download.Snapshot `json:"download"`
			Error          *download.Error    `json:"error"`
		} `json:"models"`
		RuntimeAvailable bool `json:"runtime_available"`
		MaxCharacters    int  `json:"max_characters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Models) != 2 || status.Models[0].ID != "argos-tr-en" || status.Models[1].ID != "argos-en-tr" {
		t.Fatalf("models %+v", status.Models)
	}
	if status.Models[0].SourceLanguage != "tr" || status.Models[0].TargetLanguage != "en" {
		t.Fatalf("langs %+v", status.Models[0])
	}
	if status.MaxCharacters != 5000 || !status.RuntimeAvailable {
		t.Fatalf("meta %+v", status)
	}
	for _, m := range status.Models {
		if m.Status != "missing" || m.Download != nil || m.Error != nil || m.Version == "" || m.SizeBytes <= 0 {
			t.Fatalf("initial model %+v", m)
		}
	}
	// Methods, query strings and unknown routes.
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/translation"}, {"PUT", "/api/translation"},
		{"GET", "/api/translation/models/x/download"}, {"GET", "/api/translation/translate"},
		{"POST", "/api/translation/models/argos-tr-en/unknown"},
		{"POST", "/api/translation/models//download"},
	} {
		rec = callTranslation(t, fx.handler, tc.method, tc.path, "")
		if rec.Code != 405 && rec.Code != 404 {
			t.Fatalf("%s %s: want 404/405 got %d", tc.method, tc.path, rec.Code)
		}
	}
	rec = callTranslation(t, fx.handler, "GET", "/api/translation?local=1", "")
	if rec.Code != 400 {
		t.Fatalf("query must be rejected: %d", rec.Code)
	}
}

func TestTranslationRejectsForeignOriginsAndRebinding(t *testing.T) {
	handler := TranslationHandler(nil, nil)
	for _, tc := range []struct{ host, origin, site string }{
		{"attacker.example:17321", "", ""},
		{"127.0.0.1:17321", "https://attacker.example", ""},
		{"127.0.0.1:17321", "null", ""},
		{"127.0.0.1:17321", "", "cross-site"},
	} {
		req := httptest.NewRequest("POST", "/api/translation/models/argos-tr-en/download", strings.NewReader("{}"))
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("foreign origin/rebinding accepted: %+v, status %d", tc, rec.Code)
		}
	}
}

func TestTranslationDownloadValidation(t *testing.T) {
	fx := setupTranslation(t)
	marker := "MARKER-NOLEAK-" + strings.Repeat("z", 16)
	for _, body := range []string{`{"unexpected":1}`, `not-json`, `{} {}`} {
		rec := callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-tr-en/download", body)
		if body == "{}" {
			continue
		}
		if rec.Code != 400 {
			t.Fatalf("body %q: %d %s", body, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), marker) {
			t.Fatal("marker leaked")
		}
	}
	rec := callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-tr-en/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("download %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Download download.Snapshot `json:"download"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, leak := range []string{"source_url", "temp_path", "http://", "https://"} {
		if strings.Contains(body, leak) {
			t.Fatalf("snapshot leaked %q: %s", leak, body)
		}
	}
	// Duplicate while unfinished is a conflict, not a second download.
	rec = callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-tr-en/download", "{}")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "download_exists") {
		t.Fatalf("duplicate %d %s", rec.Code, rec.Body)
	}
	rec = callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-xx/download", "{}")
	if rec.Code != 404 {
		t.Fatalf("unknown model %d", rec.Code)
	}
}

func TestTranslationInstallValidation(t *testing.T) {
	fx := setupTranslation(t)
	for _, body := range []string{`{}`, `{"download_id":""}`, `{"download_id":"x","extra":1}`, `not-json`, `{"download_id":"a"} {"download_id":"b"}`} {
		rec := callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-tr-en/install", body)
		if rec.Code != 400 {
			t.Fatalf("body %q: %d %s", body, rec.Code, rec.Body)
		}
		var envelope struct {
			Error *download.Error `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil {
			t.Fatalf("envelope %q: %v", rec.Body, err)
		}
	}
	rec := callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-xx/install", `{"download_id":"x"}`)
	if rec.Code != 404 {
		t.Fatalf("unknown model %d", rec.Code)
	}
	marker := "MARKER-INSTALL-" + strings.Repeat("q", 32)
	rec = callTranslation(t, fx.handler, "POST", "/api/translation/models/argos-tr-en/install", `{"download_id":"`+marker+`"}`)
	if rec.Code != 404 && rec.Code != 409 {
		t.Fatalf("unknown download %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), marker) {
		t.Fatalf("download id leaked to error: %s", rec.Body)
	}
}

func TestTranslationTranslateValidation(t *testing.T) {
	fx := setupTranslation(t)
	marker := "MARKER-TEXT-" + strings.Repeat("k", 24)
	bodies := map[string]int{
		`{"source_language":"tr","target_language":"de","text":"merhaba"}`:                            400,
		`{"source_language":"tr","target_language":"tr","text":"merhaba"}`:                            400,
		`{"source_language":"tr","target_language":"en","text":""}`:                                   400,
		`{"source_language":"tr","target_language":"en","text":"   "}`:                                400,
		`{"source_language":"tr","target_language":"en","text":"` + strings.Repeat("a", 5001) + `"}`:  400,
		`{"source_language":"tr","target_language":"en","text":"x","extra":1}`:                        400,
		`{"source_language":"tr","target_language":"en"}`:                                             400,
		`{"source_language":"tr","target_language":"en","text":"` + marker + `"}`:                     409,
		`{"source_language":"tr","target_language":"en","text":"a"} {"a":1}`:                          400,
		`{"source_language":"tr","target_language":"en","text":"` + strings.Repeat("b", 70000) + `"}`: 400,
	}
	for body, want := range bodies {
		rec := callTranslation(t, fx.handler, "POST", "/api/translation/translate", body)
		if rec.Code != want {
			t.Fatalf("want %d got %d for %.60q (%s)", want, rec.Code, body, rec.Body)
		}
		if strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("input text leaked to error: %.120q", rec.Body.String())
		}
		var envelope struct {
			Error *download.Error `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil || envelope.Error.Code == "" {
			t.Fatalf("structured error missing: %.120q", rec.Body.String())
		}
	}
}

func TestTranslationEndToEnd(t *testing.T) {
	fx := setupTranslation(t)
	installTranslationModel(t, fx, translation.ModelTREN)
	rec := callTranslation(t, fx.handler, "POST", "/api/translation/translate", `{"source_language":"tr","target_language":"en","text":"günaydın"}`)
	if rec.Code != 200 {
		t.Fatalf("translate %d %s", rec.Code, rec.Body)
	}
	var out struct {
		TranslatedText string `json:"translated_text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TranslatedText != "X:günaydın" {
		t.Fatalf("result %+v %v", out, err)
	}
	// GET now exposes the ready model with its completed download attached.
	rec = callTranslation(t, fx.handler, "GET", "/api/translation", "")
	var status struct {
		Models []translation.Model `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	for _, m := range status.Models {
		if m.ID == translation.ModelTREN {
			if m.Status != "ready" || m.Download == nil || m.Download.Status != "completed" {
				t.Fatalf("ready view %+v", m)
			}
		}
		if m.ID == translation.ModelENTR && m.Status != "missing" {
			t.Fatalf("independent model %+v", m)
		}
	}
}

func TestTranslationNilService(t *testing.T) {
	h := TranslationHandler(nil, nil)
	rec := callTranslation(t, h, "GET", "/api/translation", "")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "engine_unavailable") {
		t.Fatalf("nil service %d %s", rec.Code, rec.Body)
	}
}
