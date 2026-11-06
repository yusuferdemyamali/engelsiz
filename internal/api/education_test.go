package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/education"
	"engelsiz/internal/packages"
)

var (
	eduPDF  = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< >>\n%%EOF\n")
	eduHTML = []byte("<!DOCTYPE html><html><head><title>E</title></head><body><p>selam</p></body></html>")
	eduZIM  = []byte("education-zim-bytes-abcdef0123456789")
)

func eduLicense() education.License {
	return education.License{Name: "CC BY-SA 4.0", URL: "https://creativecommons.org/licenses/by-sa/4.0/", Attribution: "Test", Redistribution: "allowed", Verified: true}
}

type eduFixture struct {
	handler http.HandlerFunc
	content http.HandlerFunc
	svc     *education.Service
	mgr     *download.Manager
	pkgs    *packages.Service
	hrefs   map[string]string
}

func setupEducation(t *testing.T) *eduFixture {
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
	data := map[string][]byte{"pdf-1": eduPDF, "html-1": eduHTML, "zim-1": eduZIM}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	mk := func(id, format, filename string) education.Resource {
		body := data[id]
		n := int64(len(body))
		sum := sha256.Sum256(body)
		return education.Resource{
			ID: id, Title: "T " + id, Description: "D " + id, Category: "disaster",
			Source: "S", SourceURL: "https://example.invalid/s", Language: "tr",
			Format: format, SizeBytes: &n, Version: "2026.01",
			License: eduLicense(), DownloadURL: server.URL + "/" + id,
			Downloadable: true, SHA256: hex.EncodeToString(sum[:]), Filename: filename,
		}
	}
	res := []education.Resource{
		mk("pdf-1", education.FormatPDF, "doc.pdf"),
		mk("html-1", education.FormatHTML, "app.html"),
		mk("zim-1", education.FormatZIM, "edu.zim"),
		{ID: "ref-1", Title: "Ref", Description: "reference", Category: "science",
			Source: "S", SourceURL: "https://example.invalid/s", Language: "tr",
			Format: education.FormatPDF, Version: "2026.01", License: education.License{Name: "U", Verified: false}},
	}
	pkgs, err := packages.New(context.Background(), db, dir, packages.Options{})
	if err != nil {
		server.Close()
		mgr.Close()
		db.Close()
		t.Fatal(err)
	}
	svc, err := education.New(db, dir, education.Options{Resources: res, Packages: pkgs})
	if err != nil {
		pkgs.Close()
		server.Close()
		mgr.Close()
		db.Close()
		t.Fatal(err)
	}
	fx := &eduFixture{svc: svc, mgr: mgr, pkgs: pkgs, hrefs: map[string]string{}}
	fx.handler = EducationHandler(svc, mgr, pkgs, func(id string) (string, bool) {
		href, ok := fx.hrefs[id]
		return href, ok
	})
	fx.content = EducationContentHandler(svc)
	t.Cleanup(func() { svc.Close(); pkgs.Close(); server.Close(); mgr.Close(); db.Close() })
	return fx
}

func callEdu(t *testing.T, h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:17321"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func waitEduDownload(t *testing.T, m *download.Manager, id string) {
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

func waitEduResource(t *testing.T, fx *eduFixture, id, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		res, err := fx.svc.List(fx.mgr, fx.pkgs)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range res.Resources {
			if v.ID == id && v.Status == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("resource %s did not become %s", id, want)
}

func installEdu(t *testing.T, fx *eduFixture, id string) string {
	t.Helper()
	rec := callEdu(t, fx.handler, "POST", "/api/education/"+id+"/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("download %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Resource struct {
			ID       string             `json:"id"`
			Status   string             `json:"status"`
			Download *download.Snapshot `json:"download"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Resource.ID != id || created.Resource.Download == nil {
		t.Fatalf("download resource envelope %s", rec.Body)
	}
	if created.Resource.Status != "downloading" && created.Resource.Status != "downloaded" {
		t.Fatalf("download status %q", created.Resource.Status)
	}
	downloadID := created.Resource.Download.ID
	waitEduDownload(t, fx.mgr, downloadID)
	rec = callEdu(t, fx.handler, "POST", "/api/education/"+id+"/install", `{"download_id":"`+downloadID+`"}`)
	if rec.Code != 202 {
		t.Fatalf("install %d %s", rec.Code, rec.Body)
	}
	waitEduResource(t, fx, id, "installed")
	return downloadID
}

func TestEducationHTTPStatus(t *testing.T) {
	fx := setupEducation(t)
	rec := callEdu(t, fx.handler, "GET", "/api/education", "")
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
		Resources []struct {
			ID           string             `json:"id"`
			Format       string             `json:"format"`
			Status       string             `json:"status"`
			Downloadable bool               `json:"downloadable"`
			License      map[string]any     `json:"license"`
			Download     *download.Snapshot `json:"download"`
			OpenHref     *string            `json:"openHref"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Resources) != 4 {
		t.Fatalf("resources %+v", status.Resources)
	}
	byID := map[string]string{}
	for _, r := range status.Resources {
		byID[r.ID] = r.Status
		if r.License == nil {
			t.Fatalf("license object missing for %s", r.ID)
		}
		if r.Download != nil || r.OpenHref != nil {
			t.Fatalf("fresh resource must carry null download/openHref: %+v", r)
		}
	}
	if byID["pdf-1"] != "missing" || byID["ref-1"] != "reference" {
		t.Fatalf("initial %+v", byID)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"download":null`) || !strings.Contains(body, `"openHref":null`) {
		t.Fatalf("download/openHref must be explicit null: %.200s", body)
	}
	for _, leak := range []string{"temp_path", "local_path", ".part", "tempdir", "TempDir"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Fatalf("internal path leaked: %q", leak)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/education"}, {"PUT", "/api/education"},
		{"GET", "/api/education/pdf-1/download"}, {"GET", "/api/education/pdf-1/install"},
		{"POST", "/api/education/pdf-1/unknown"}, {"POST", "/api/education//download"},
		{"DELETE", "/api/education/pdf-1/download"},
	} {
		rec = callEdu(t, fx.handler, tc.method, tc.path, "")
		if rec.Code != 405 && rec.Code != 404 {
			t.Fatalf("%s %s: want 404/405 got %d", tc.method, tc.path, rec.Code)
		}
	}
	rec = callEdu(t, fx.handler, "GET", "/api/education?local=1", "")
	if rec.Code != 400 {
		t.Fatalf("query must be rejected: %d", rec.Code)
	}
}

func TestEducationRejectsForeignOrigins(t *testing.T) {
	handler := EducationHandler(nil, nil, nil, nil)
	for _, tc := range []struct{ host, origin, site string }{
		{"attacker.example:17321", "", ""},
		{"127.0.0.1:17321", "https://attacker.example", ""},
		{"127.0.0.1:17321", "null", ""},
		{"127.0.0.1:17321", "", "cross-site"},
	} {
		req := httptest.NewRequest("POST", "/api/education/pdf-1/download", strings.NewReader("{}"))
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("foreign origin accepted: %+v -> %d", tc, rec.Code)
		}
	}
}

func TestEducationDownloadValidation(t *testing.T) {
	fx := setupEducation(t)
	for _, body := range []string{`{"unexpected":1}`, `not-json`, `{} {}`} {
		rec := callEdu(t, fx.handler, "POST", "/api/education/pdf-1/download", body)
		if rec.Code != 400 {
			t.Fatalf("body %q: %d %s", body, rec.Code, rec.Body)
		}
	}
	rec := callEdu(t, fx.handler, "POST", "/api/education/nope/download", "{}")
	if rec.Code != 404 {
		t.Fatalf("unknown %d", rec.Code)
	}
	rec = callEdu(t, fx.handler, "POST", "/api/education/ref-1/download", "{}")
	if rec.Code != 422 {
		t.Fatalf("reference download %d %s", rec.Code, rec.Body)
	}
	// Duplicate unfinished download conflicts.
	rec = callEdu(t, fx.handler, "POST", "/api/education/pdf-1/download", "{}")
	if rec.Code != 201 {
		t.Fatalf("first download %d", rec.Code)
	}
	rec = callEdu(t, fx.handler, "POST", "/api/education/pdf-1/download", "{}")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "download_exists") {
		t.Fatalf("duplicate %d %s", rec.Code, rec.Body)
	}
}

func TestEducationInstallValidation(t *testing.T) {
	fx := setupEducation(t)
	for _, body := range []string{`null`, `{"download_id":"","extra":1}`, `{"downloadId":123}`, `not-json`, `{"download_id":"a"} {"x":1}`} {
		rec := callEdu(t, fx.handler, "POST", "/api/education/pdf-1/install", body)
		if rec.Code != 400 {
			t.Fatalf("body %q: %d %s", body, rec.Code, rec.Body)
		}
	}
	rec := callEdu(t, fx.handler, "POST", "/api/education/nope/install", "{}")
	if rec.Code != 404 {
		t.Fatalf("unknown %d", rec.Code)
	}
	// Unknown download id without association.
	rec = callEdu(t, fx.handler, "POST", "/api/education/pdf-1/install", `{"download_id":"ffffffffffffffffffffffffffffffff"}`)
	if rec.Code != 404 && rec.Code != 409 {
		t.Fatalf("unknown download %d %s", rec.Code, rec.Body)
	}
}

func TestEducationInstallIDSpellings(t *testing.T) {
	for _, body := range []string{`{"download_id":"%s"}`, `{"downloadId":"%s"}`, `{}`} {
		fx := setupEducation(t)
		rec := callEdu(t, fx.handler, "POST", "/api/education/pdf-1/download", "{}")
		var created struct {
			Resource struct {
				Download *download.Snapshot `json:"download"`
			} `json:"resource"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if created.Resource.Download == nil {
			t.Fatalf("download envelope %s", rec.Body)
		}
		downloadID := created.Resource.Download.ID
		waitEduDownload(t, fx.mgr, downloadID)
		rec = callEdu(t, fx.handler, "POST", "/api/education/pdf-1/install", sprintf(body, downloadID))
		if rec.Code != 202 {
			t.Fatalf("body %q: %d %s", body, rec.Code, rec.Body)
		}
		waitEduResource(t, fx, "pdf-1", "installed")
	}
}

func sprintf(format, arg string) string {
	if strings.Contains(format, "%s") {
		return strings.Replace(format, "%s", arg, 1)
	}
	return format
}

func TestEducationEndToEndDelete(t *testing.T) {
	fx := setupEducation(t)
	downloadID := installEdu(t, fx, "pdf-1")
	_ = downloadID
	rec := callEdu(t, fx.handler, "GET", "/api/education", "")
	var status struct {
		Resources []struct {
			ID       string             `json:"id"`
			Status   string             `json:"status"`
			Download *download.Snapshot `json:"download"`
			OpenHref *string            `json:"openHref"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	for _, r := range status.Resources {
		if r.ID == "pdf-1" && (r.Status != "installed" || r.Download == nil || r.OpenHref == nil || *r.OpenHref != "/education/content/pdf-1") {
			t.Fatalf("installed view %+v", r)
		}
	}
	// DELETE with a body is rejected; the artifact expects no payload.
	rec = callEdu(t, fx.handler, "DELETE", "/api/education/pdf-1", `{"download_id":"x"}`)
	if rec.Code != 400 {
		t.Fatalf("delete with body %d %s", rec.Code, rec.Body)
	}
	// The install worker may still hold its active guard just after the
	// installed status became visible; retry the delete while it settles.
	// Any non-409 failure, or a settled non-200, fails below.
	deadline := time.Now().Add(15 * time.Second)
	for {
		rec = callEdu(t, fx.handler, "DELETE", "/api/education/pdf-1", "")
		if rec.Code == 200 {
			break
		}
		if rec.Code != 409 || !time.Now().Before(deadline) {
			t.Fatalf("delete %d %s", rec.Code, rec.Body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var deleted struct {
		Resource struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil || deleted.Resource.ID != "pdf-1" || deleted.Resource.Status != "missing" {
		t.Fatalf("delete body %s %v", rec.Body, err)
	}
}

func TestEducationContentServing(t *testing.T) {
	fx := setupEducation(t)
	installEdu(t, fx, "pdf-1")
	installEdu(t, fx, "html-1")

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:17321"
		rec := httptest.NewRecorder()
		fx.content(rec, req)
		return rec
	}
	rec := get("/education/content/pdf-1")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/pdf") {
		t.Fatalf("pdf %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "%PDF-") {
		t.Fatal("pdf body wrong")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("pdf headers %+v", rec.Header())
	}
	rec = get("/education/content/html-1")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("html %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "script-src 'unsafe-inline' 'unsafe-eval'", "connect-src 'none'", "frame-ancestors 'self'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("html csp missing %q in %q", want, csp)
		}
	}
	// Isolation stays mandatory per probe: opaque origin (no allow-same-origin)
	// and no remote hosts, so the optional gtm.js/telemetry GIFs inside
	// audited PhET documents remain blocked while inline scripts + eval run.
	if strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("csp must not allow same-origin: %q", csp)
	}
	for _, leak := range []string{"https://", "http://", "gtm", "google-analytics"} {
		if strings.Contains(csp, leak) {
			t.Fatalf("csp must not whitelist remote hosts: %q in %q", leak, csp)
		}
	}
	// HEAD, Range, method, query, traversal, zim and missing handling.
	req := httptest.NewRequest("HEAD", "/education/content/pdf-1", nil)
	req.Host = "127.0.0.1:17321"
	head := httptest.NewRecorder()
	fx.content(head, req)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("head %d body %d", head.Code, head.Body.Len())
	}
	req = httptest.NewRequest("GET", "/education/content/pdf-1", nil)
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Range", "bytes=0-4")
	ranged := httptest.NewRecorder()
	fx.content(ranged, req)
	if ranged.Code != 206 || ranged.Body.String() != "%PDF-" {
		t.Fatalf("range %d %q", ranged.Code, ranged.Body.String())
	}
	req = httptest.NewRequest("POST", "/education/content/pdf-1", nil)
	req.Host = "127.0.0.1:17321"
	rec = httptest.NewRecorder()
	fx.content(rec, req)
	if rec.Code != 405 {
		t.Fatalf("method %d", rec.Code)
	}
	for _, path := range []string{"/education/content/pdf-1?x=1", "/education/content/pdf-1/extra", "/education/content/zim-1", "/education/content/nope", "/education/content/../pdf-1"} {
		if rec := get(path); rec.Code != 404 {
			t.Fatalf("path %s: %d", path, rec.Code)
		}
	}
	// Direct handler calls with a non-matching path must 404, never panic on
	// the prefix slice.
	for _, path := range []string{"/nope", "/education/content", "/"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:17321"
		rec := httptest.NewRecorder()
		fx.content(rec, req)
		if rec.Code != 404 {
			t.Fatalf("direct path %s: %d", path, rec.Code)
		}
	}
}

func TestEducationZIMOpenHrefHonest(t *testing.T) {
	fx := setupEducation(t)
	downloadID := installEdu(t, fx, "zim-1")
	// Runtime not ready: no openHref invented.
	rec := callEdu(t, fx.handler, "GET", "/api/education", "")
	if !strings.Contains(rec.Body.String(), `"openHref":null`) {
		t.Fatalf("openHref invented: %s", rec.Body)
	}
	// Runtime ready: href exposed.
	fx.hrefs[downloadID] = "/content/content/" + downloadID + "/"
	rec = callEdu(t, fx.handler, "GET", "/api/education", "")
	var status struct {
		Resources []struct {
			ID       string  `json:"id"`
			OpenHref *string `json:"openHref"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	for _, r := range status.Resources {
		if r.ID == "zim-1" && (r.OpenHref == nil || *r.OpenHref != "/content/content/"+downloadID+"/") {
			t.Fatalf("zim href %+v", r)
		}
		if r.ID == "pdf-1" && r.OpenHref != nil {
			t.Fatalf("pdf href must stay null: %+v", r)
		}
	}
}

func TestEducationZIMDeleteKeepsWikipedia(t *testing.T) {
	fx := setupEducation(t)
	// Scoped refusal of non-education rows is covered in the packages
	// service tests; here assert the education delete only removes its own
	// ZIM and leaves no package behind.
	downloadID := installEdu(t, fx, "zim-1")
	rec := callEdu(t, fx.handler, "DELETE", "/api/education/zim-1", "")
	if rec.Code != 200 {
		t.Fatalf("delete %d %s", rec.Code, rec.Body)
	}
	items, err := fx.pkgs.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range items {
		if p.ID == downloadID {
			t.Fatalf("zim package remains %+v", p)
		}
	}
}
