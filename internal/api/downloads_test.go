package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/providers"
)

type resolverStub func(context.Context, string) (providers.DownloadSource, error)

func (f resolverStub) ResolveDownload(ctx context.Context, id string) (providers.DownloadSource, error) {
	return f(ctx, id)
}
func TestDownloadHTTPContract(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v"`)
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer source.Close()
	dir := t.TempDir()
	db, e := database.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m, e := download.New(db, filepath.Join(dir, "files"), download.Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	var resolutionCalls int
	resolver := resolverStub(func(ctx context.Context, id string) (providers.DownloadSource, error) {
		resolutionCalls++
		if id == "bad" {
			return providers.DownloadSource{}, errors.New("secret upstream http://private/path")
		}
		if id == "mismatch" {
			return providers.DownloadSource{Provider: "other", ProviderPackageID: id}, nil
		}
		return providers.DownloadSource{Provider: "kiwix", ProviderPackageID: id, Title: "Paket", Variant: "mini", FileName: "tiny.zim", SourceURL: source.URL}, nil
	})
	handler := DownloadsHandler(m, resolver)
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		handler(r, httptest.NewRequest(method, path, strings.NewReader(body)))
		if r.Code != status {
			t.Fatalf("%s %s: %d %s want%d", method, path, r.Code, r.Body, status)
		}
		if r.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") {
			t.Fatal("JSON headers missing")
		}
		return r
	}
	rec := request("GET", "/api/downloads", "", 200)
	if strings.TrimSpace(rec.Body.String()) != `{"downloads":[]}` {
		t.Fatal("empty array contract")
	}
	for _, body := range []string{`{}`, `{"provider":"evil","providerPackageId":"x"}`, `{"provider":"kiwix","providerPackageId":"x","sourceURL":"http://evil"}`, `{"provider":"kiwix","providerPackageId":"x","tempPath":"/tmp/x"}`, `{"provider":"kiwix","providerPackageId":"x"} {}`, `no-json`} {
		request("POST", "/api/downloads", body, 400)
	}
	if resolutionCalls != 0 {
		t.Fatal("invalid request resolved source")
	}
	rec = request("PUT", "/api/downloads", "", 405)
	if rec.Header().Get("Allow") != "GET, POST" {
		t.Fatal("allow header")
	}
	for _, id := range []string{"bad", "mismatch"} {
		rec = request("POST", "/api/downloads", fmt.Sprintf(`{"provider":"kiwix","providerPackageId":%q}`, id), 502)
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "private") {
			t.Fatal("upstream detail leaked")
		}
	}
	body := `{"provider":"kiwix","providerPackageId":"selected"}`
	rec = request("POST", "/api/downloads", body, 201)
	var result struct {
		Download download.Snapshot `json:"download"`
	}
	if e = json.Unmarshal(rec.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	id := result.Download.ID
	for _, secret := range []string{"sourceURL", "source_url", "tempPath", "temp_path", "etag", "last_modified", source.URL, dir} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("public snapshot leaked %q", secret)
		}
	}
	beforeDuplicate := resolutionCalls
	request("POST", "/api/downloads", body, 409)
	if resolutionCalls != beforeDuplicate {
		t.Fatal("duplicate refetched upstream instead of returning conflict")
	}
	request("POST", "/api/downloads/"+id+"/pause", "", 200)
	request("POST", "/api/downloads/"+id+"/pause", "", 200)
	request("POST", "/api/downloads/"+id+"/retry", "", 409)
	request("GET", "/api/downloads/"+id, "", 200)
	request("GET", "/api/downloads", "", 200)
	rec = request("GET", "/api/downloads/"+id+"/resume", "", 405)
	if rec.Header().Get("Allow") != "POST" {
		t.Fatal("control Allow")
	}
	request("POST", "/api/downloads/"+id+"/resume", "", 200)
	request("POST", "/api/downloads/"+id+"/cancel", "", 200)
	request("POST", "/api/downloads/"+id+"/cancel", "", 200)
	request("POST", "/api/downloads/"+id+"/resume", "", 409)
	request("GET", "/api/downloads/missing", "", 404)
	request("POST", "/api/downloads/missing/pause", "", 404)
	request("POST", "/api/downloads/"+id+"/unknown", "", 404)
	request("POST", "/api/downloads", body, 201)
}
func TestDownloadHTTPPreflightAndStorageErrors(t *testing.T) {
	dir := t.TempDir()
	db, e := database.Open(filepath.Join(dir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m, e := download.New(db, filepath.Join(dir, "files"), download.Options{Available: func(string) (uint64, error) { return 1, nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	total := int64(100)
	resolver := resolverStub(func(context.Context, string) (providers.DownloadSource, error) {
		return providers.DownloadSource{Provider: "kiwix", ProviderPackageID: "x", FileName: "x.zim", SourceURL: "http://example.test/file", TotalBytes: &total}, nil
	})
	h := DownloadsHandler(m, resolver)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/api/downloads", strings.NewReader(`{"provider":"kiwix","providerPackageId":"x"}`)))
	if rec.Code != 507 || !strings.Contains(rec.Body.String(), "insufficient_space") {
		t.Fatalf("preflight%d %s", rec.Code, rec.Body)
	}
	// No worker exists after preflight failure; closed storage must return a
	// safe error rather than reporting a successful empty list.
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/api/downloads", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "storage_error") {
		t.Fatalf("storage%d %s", rec.Code, rec.Body)
	}
}
