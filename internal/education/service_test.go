package education_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"engelsiz/internal/education"
	"engelsiz/internal/packages"
)

var (
	pdfBytes  = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< >>\n%%EOF\n")
	htmlBytes = []byte("<!DOCTYPE html><html><head><title>E</title></head><body><p>merhaba</p></body></html>")
	zimBytes  = []byte("zim-fixture-bytes-0123456789abcdef")
)

func licensed() education.License {
	return education.License{Name: "CC BY-SA 4.0", URL: "https://creativecommons.org/licenses/by-sa/4.0/", Attribution: "Test", Redistribution: "allowed", Verified: true}
}

func fixtureResources(server string, data map[string][]byte) []education.Resource {
	mk := func(id, format, filename string) education.Resource {
		body := data[id]
		n := int64(len(body))
		sum := sha256.Sum256(body)
		return education.Resource{
			ID: id, Title: "T " + id, Description: "D " + id, Category: "disaster",
			Source: "S", SourceURL: "https://example.invalid/s", Language: "tr",
			Format: format, SizeBytes: &n, Version: "2026.01",
			License: licensed(), DownloadURL: server + "/" + id,
			Downloadable: true, SHA256: hex.EncodeToString(sum[:]), Filename: filename,
		}
	}
	return []education.Resource{
		mk("pdf-1", education.FormatPDF, "doc.pdf"),
		mk("html-1", education.FormatHTML, "app.html"),
		mk("zim-1", education.FormatZIM, "edu.zim"),
		{ID: "ref-1", Title: "Ref", Description: "reference only", Category: "science",
			Source: "S", SourceURL: "https://example.invalid/s", Language: "tr",
			Format: education.FormatPDF, Version: "2026.01", License: education.License{Name: "Unknown", Verified: false}},
	}
}

type fixture struct {
	dir      string
	db       *sql.DB
	mgr      *download.Manager
	pkgs     *packages.Service
	svc      *education.Service
	data     map[string][]byte
	server   *httptest.Server
	notifyCh chan struct{}
}

func setup(t *testing.T, mutate func(data map[string][]byte, res []education.Resource) []education.Resource) *fixture {
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
	data := map[string][]byte{"pdf-1": pdfBytes, "html-1": htmlBytes, "zim-1": zimBytes}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := data[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	res := fixtureResources(server.URL, data)
	if mutate != nil {
		res = mutate(data, res)
		// Recompute pins after mutation of served bytes.
		for i := range res {
			if body, ok := data[res[i].ID]; ok && res[i].Downloadable {
				n := int64(len(body))
				sum := sha256.Sum256(body)
				res[i].SizeBytes = &n
				res[i].SHA256 = hex.EncodeToString(sum[:])
			}
		}
	}
	notifyCh := make(chan struct{}, 1)
	pkgs, err := packages.New(context.Background(), db, dir, packages.Options{OnInstalled: func() {
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	}})
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
	fx := &fixture{dir: dir, db: db, mgr: mgr, pkgs: pkgs, svc: svc, data: data, server: server, notifyCh: notifyCh}
	t.Cleanup(func() { svc.Close(); pkgs.Close(); server.Close(); mgr.Close(); db.Close() })
	return fx
}

func isCode(err error, code string) bool {
	var problem *download.Error
	return errors.As(err, &problem) && problem.Code == code
}

func waitDownload(t *testing.T, m *download.Manager, id string) {
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
		if snap.Status == "failed" || snap.Status == "cancelled" {
			t.Fatalf("download ended %s %+v", snap.Status, snap.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not complete")
}

func waitResource(t *testing.T, fx *fixture, id, want string) education.View {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		res, err := fx.svc.List(fx.mgr, fx.pkgs)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range res.Resources {
			if v.ID == id {
				if v.Status == want {
					return v
				}
				if v.Status == "failed" && want != "failed" {
					t.Fatalf("resource %s failed: %+v", id, v.Error)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("resource %s did not become %s", id, want)
	return education.View{}
}

func downloadAndInstall(t *testing.T, fx *fixture, id string) education.View {
	t.Helper()
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, id)
	if err != nil {
		t.Fatal(err)
	}
	if started.Download == nil {
		t.Fatalf("download view missing download: %+v", started)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	view, installed, err := fx.svc.Install(fx.mgr, fx.pkgs, id, snapID)
	if err != nil || installed {
		t.Fatalf("install accept: %+v %v %v", view, installed, err)
	}
	return waitResource(t, fx, id, "installed")
}

// deleteResource removes a resource, retrying while an install worker that
// already reported its terminal status is still settling. A terminal status
// becomes visible before the worker releases its active guard, so a single
// immediate Delete can hit invalid_state on slow runners; the post-delete
// assertions below stay strict.
func deleteResource(t *testing.T, fx *fixture, id string) education.View {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := fx.svc.Delete(fx.mgr, fx.pkgs, id)
		if err == nil {
			return out
		}
		if !isCode(err, "invalid_state") {
			t.Fatalf("delete %s: %v", id, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("delete %s never settled", id)
	return education.View{}
}

func TestCatalogFailsClosed(t *testing.T) {
	base := setup(t, nil)
	_ = base
	dup := fixtureResources("https://example.invalid", map[string][]byte{"pdf-1": pdfBytes, "html-1": htmlBytes, "zim-1": zimBytes})
	dup = append(dup, dup[0])
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := education.New(db, dir, education.Options{Resources: dup}); err == nil {
		t.Fatal("duplicate ids accepted")
	}
	bad := fixtureResources("https://example.invalid", map[string][]byte{"pdf-1": pdfBytes, "html-1": htmlBytes, "zim-1": zimBytes})
	bad[0].Format = "exe"
	if _, err := education.New(db, t.TempDir(), education.Options{Resources: bad}); err == nil {
		t.Fatal("bad format accepted")
	}
	unverified := fixtureResources("https://example.invalid", map[string][]byte{"pdf-1": pdfBytes, "html-1": htmlBytes, "zim-1": zimBytes})
	unverified[0].License.Verified = false
	if _, err := education.New(db, t.TempDir(), education.Options{Resources: unverified}); err == nil {
		t.Fatal("unverified downloadable accepted")
	}
}

func TestReferenceOnly(t *testing.T) {
	fx := setup(t, nil)
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "ref-1" && (v.Status != "reference" || v.Download != nil || v.OpenHref != nil) {
			t.Fatalf("reference view %+v", v)
		}
	}
	if _, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "ref-1"); !isCode(err, "metadata_unavailable") {
		t.Fatalf("reference download: %v", err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "ref-1", "x"); !isCode(err, "metadata_unavailable") {
		t.Fatalf("reference install: %v", err)
	}
	if _, _, _, err := fx.svc.Open("ref-1"); !isCode(err, "not_found") {
		t.Fatalf("reference open: %v", err)
	}
}

func TestUnknownIDs(t *testing.T) {
	fx := setup(t, nil)
	if _, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "nope"); !isCode(err, "not_found") {
		t.Fatalf("download: %v", err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "nope", "x"); !isCode(err, "not_found") {
		t.Fatalf("install: %v", err)
	}
	if _, err := fx.svc.Delete(fx.mgr, fx.pkgs, "nope"); !isCode(err, "not_found") {
		t.Fatalf("delete: %v", err)
	}
	if _, _, _, err := fx.svc.Open("../x"); !isCode(err, "not_found") {
		t.Fatalf("open traversal: %v", err)
	}
}

func TestPDFRoundTripRestartOpenDelete(t *testing.T) {
	fx := setup(t, nil)
	keep := filepath.Join(fx.dir, "content", "education", "user-keep.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := downloadAndInstall(t, fx, "pdf-1")
	if v.Download == nil || v.Download.Status != "completed" {
		t.Fatalf("installed view %+v", v.Download)
	}
	var state string
	if err := fx.db.QueryRow(`SELECT artifact_state FROM downloads WHERE id=?`, v.Download.ID).Scan(&state); err != nil || state != "consumed" {
		t.Fatalf("artifact %q %v", state, err)
	}
	f, fi, mime, err := fx.svc.Open("pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(fx.dir, "content", "education", "pdf-1", "doc.pdf"))
	f.Close()
	if err != nil || string(body) != string(pdfBytes) || mime != "application/pdf" || fi.Size() != int64(len(pdfBytes)) {
		t.Fatalf("open %q %s %v", body, mime, err)
	}
	// Restart without networking keeps the installed file.
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fx.pkgs.Close(); err != nil {
		t.Fatal(err)
	}
	fx.server.Close()
	fx.mgr.Close()
	mgr2, err := download.New(fx.db, filepath.Join(fx.dir, "downloads"), download.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr2.Close()
	pkgs2, err := packages.New(context.Background(), fx.db, fx.dir, packages.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer pkgs2.Close()
	svc2, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources(fx.server.URL, fx.data), Packages: pkgs2})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()
	waitResource(t, &fixture{svc: svc2, mgr: mgr2, pkgs: pkgs2}, "pdf-1", "installed")
	fx.svc = svc2
	fx.mgr = mgr2
	fx.pkgs = pkgs2
	// Delete removes file+row, preserves unrelated files, stays idempotent.
	// Tombstoned transfers never resurrect, so the view reports missing.
	out, err := fx.svc.Delete(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil || out.Status != "missing" || out.Download != nil {
		t.Fatalf("delete %+v %v", out, err)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "education", "pdf-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final remains: %v", err)
	}
	if raw, err := os.ReadFile(keep); err != nil || string(raw) != "keep me" {
		t.Fatalf("user file disturbed: %q %v", raw, err)
	}
	out, err = fx.svc.Delete(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil || out.Status != "missing" {
		t.Fatalf("idempotent delete %+v %v", out, err)
	}
	if _, _, _, err := fx.svc.Open("pdf-1"); !isCode(err, "not_found") {
		t.Fatalf("open after delete: %v", err)
	}
}

func TestCorruptAndTruncatedRejected(t *testing.T) {
	fx := setup(t, nil)
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	if started.Status != "downloading" && started.Status != "downloaded" {
		t.Fatalf("download view %+v", started)
	}
	waitDownload(t, fx.mgr, snapID)
	part := filepath.Join(fx.dir, "downloads", snapID+".part")
	if err := os.WriteFile(part, []byte(strings.Repeat("X", len(pdfBytes))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "pdf-1", snapID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitResource(t, fx, "pdf-1", "failed")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("want verification_failed, got %+v", got.Error)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "education", "pdf-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt payload published: %v", err)
	}

	fx2 := setup(t, nil)
	started2, err := fx2.svc.StartDownload(fx2.mgr, fx2.pkgs, "pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID2 := started2.Download.ID
	waitDownload(t, fx2.mgr, snapID2)
	if err := os.Truncate(filepath.Join(fx2.dir, "downloads", snapID2+".part"), int64(len(pdfBytes)-10)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx2.svc.Install(fx2.mgr, fx2.pkgs, "pdf-1", snapID2); !isCode(err, "verification_failed") {
		t.Fatalf("truncated must fail fast, got %v", err)
	}
}

func TestHTMLSubstitutionRejected(t *testing.T) {
	fx := setup(t, func(data map[string][]byte, res []education.Resource) []education.Resource {
		data["pdf-1"] = htmlBytes // server swaps payload; pin recomputed over HTML
		return res
	})
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "pdf-1", snapID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitResource(t, fx, "pdf-1", "failed")
	if got.Error == nil || got.Error.Code != "verification_failed" {
		t.Fatalf("substitution must fail, got %+v", got.Error)
	}
}

// TestPhETExternalRefsTolerated serves a PhET-shaped document carrying the
// known optional external references (GTM script, tracking GIF, splash.svg).
// Install must accept it: the validator only enforces document magic, and the
// network is fail-closed at serve time via CSP, not by invalidating the sim.
func TestPhETExternalRefsTolerated(t *testing.T) {
	phet := []byte(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Sim</title>` +
		`<script async src="https://www.googletagmanager.com/gtm.js?id=GTM-XXXX"></script>` +
		`<script>window.phetOptionalTelemetry="https://www.google-analytics.com/collect?v=1";</script>` +
		`</head><body><img src="splash.svg">` +
		`<img src="https://www.google-analytics.com/collect?v=1&t=pageview">` +
		`<p>merhaba</p></body></html>`)
	fx := setup(t, func(data map[string][]byte, res []education.Resource) []education.Resource {
		data["html-1"] = phet // pin recomputed over the PhET-shaped payload
		return res
	})
	v := downloadAndInstall(t, fx, "html-1")
	if v.Download == nil {
		t.Fatal("installed view missing download")
	}
	raw, err := os.ReadFile(filepath.Join(fx.dir, "content", "education", "html-1", "app.html"))
	if err != nil || string(raw) != string(phet) {
		t.Fatalf("payload altered: %v", err)
	}
	f, _, mime, err := fx.svc.Open("html-1")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if mime != "text/html; charset=utf-8" {
		t.Fatalf("mime %q", mime)
	}
}

func TestSymlinkAndMissingSurfaceWithoutLeaks(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "html-1")
	final := filepath.Join(fx.dir, "content", "education", "html-1", "app.html")
	if err := os.Remove(final); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fx.dir, "downloads"), final); err != nil {
		t.Skip(err)
	}
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "html-1" && v.Status != "failed" {
			t.Fatalf("symlink must surface failed: %+v", v)
		}
	}
	if _, _, _, err := fx.svc.Open("html-1"); !isCode(err, "not_found") {
		t.Fatalf("symlink open: %v", err)
	}
	// Repair with explicit reinstall stages the foreign dir aside.
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "html-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "html-1", snapID); err != nil {
		t.Fatal(err)
	}
	waitResource(t, fx, "html-1", "installed")
	broken, _ := filepath.Glob(filepath.Join(fx.dir, "content", "education", "html-1.broken-*"))
	if len(broken) != 1 {
		t.Fatalf("foreign dir must be staged aside, got %v", broken)
	}
}

func TestZIMDelegatesAndRemoves(t *testing.T) {
	fx := setup(t, nil)
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "zim-1")
	if err != nil {
		t.Fatal(err)
	}
	if started.Download == nil || started.Download.Provider != "kiwix" || started.Download.Variant != "edu-zim-1" {
		t.Fatalf("zim descriptor %+v", started.Download)
	}
	snapID := started.Download.ID
	// Generic packages installer can claim it (namespaced education variant).
	waitDownload(t, fx.mgr, snapID)
	view, installed, err := fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", snapID)
	if err != nil || installed {
		t.Fatalf("install accept %+v %v %v", view, installed, err)
	}
	got := waitResource(t, fx, "zim-1", "installed")
	if got.Download == nil || got.Download.ID != snapID {
		t.Fatalf("installed association %+v", got.Download)
	}
	// Latest-associated install is idempotent.
	view, installed, err = fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", "")
	if err != nil || !installed {
		t.Fatalf("idempotent reinstall %+v %v %v", view, installed, err)
	}
	// No PDF/HTML file must exist for the ZIM.
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "education", "zim-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zim must not publish files: %v", err)
	}
	out := deleteResource(t, fx, "zim-1")
	if out.Status != "missing" || out.Download != nil {
		t.Fatalf("delete %+v", out)
	}
	items, err := fx.pkgs.List("")
	if err != nil || len(items) != 0 {
		t.Fatalf("package remains %+v %v", items, err)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "kiwix", snapID+".zim")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zim file remains: %v", err)
	}
}

func TestCrashRecovery(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Orphan final without a row: adopted on restart.
	if _, err := fx.db.Exec(`DELETE FROM education_resources WHERE id='pdf-1'`); err != nil {
		t.Fatal(err)
	}
	// Interrupted install plus staging leftover.
	if _, err := fx.db.Exec(`INSERT INTO education_resources(id,version,status) VALUES('html-1','2026.01','installing')`); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(fx.dir, "content", "education", ".staging-html-1-abcdef0123456789")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources(fx.server.URL, fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	res, err := svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]education.View{}
	for _, v := range res.Resources {
		byID[v.ID] = v
	}
	if byID["pdf-1"].Status != "installed" {
		t.Fatalf("orphan not adopted: %+v", byID["pdf-1"])
	}
	if byID["html-1"].Status != "failed" {
		t.Fatalf("interrupted not surfaced: %+v", byID["html-1"])
	}
	if _, err := os.Lstat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging leftover: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "education", "pdf-1")); err != nil {
		t.Fatalf("adopted payload missing: %v", err)
	}
}

func openHref(v education.View) string {
	if v.OpenHref == nil {
		return ""
	}
	return *v.OpenHref
}

// TestOpenHrefExact asserts installed PDF/HTML views carry the exact reader
// link through the service snapshot.
func TestOpenHrefExact(t *testing.T) {
	fx := setup(t, nil)
	v := downloadAndInstall(t, fx, "pdf-1")
	if v.Status != "installed" || openHref(v) != "/education/content/pdf-1" {
		t.Fatalf("href %+v", v)
	}
	h := downloadAndInstall(t, fx, "html-1")
	if openHref(h) != "/education/content/html-1" {
		t.Fatalf("href %+v", h)
	}
	v = waitResource(t, fx, "pdf-1", "installed")
	if openHref(v) != "/education/content/pdf-1" {
		t.Fatalf("refetch href %+v", v)
	}
}

// TestDownloadInstallDeleteCycle exercises the full lifecycle twice with a
// restart: no tombstoned transfer may resurrect a downloaded/installed state
// after Delete.
func TestDownloadInstallDeleteCycle(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	out := deleteResource(t, fx, "pdf-1")
	if out.Status != "missing" || out.Download != nil || openHref(out) != "" {
		t.Fatalf("delete %+v", out)
	}
	v := downloadAndInstall(t, fx, "pdf-1")
	if openHref(v) != "/education/content/pdf-1" {
		t.Fatalf("reinstall href %+v", v)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	fx.server.Close()
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	res, err := svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Resources {
		if m.ID == "pdf-1" && (m.Status != "installed" || openHref(m) != "/education/content/pdf-1") {
			t.Fatalf("restart lost install: %+v", m)
		}
	}
}

// TestFailedDownloadRetryFlow covers transfer failure, the explicit
// download_exists conflict on a second StartDownload, manager retry, and
// install after recovery. A new download after a stored install error must
// clear that error.
func TestFailedDownloadRetryFlow(t *testing.T) {
	fx := setup(t, nil)
	saved := fx.data["pdf-1"]
	delete(fx.data, "pdf-1")
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	deadline := time.Now().Add(20 * time.Second)
	for {
		snap, err := fx.mgr.Get(snapID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("transfer did not fail: %s", snap.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1"); !isCode(err, "download_exists") {
		t.Fatalf("second download must conflict, got %v", err)
	}
	fx.data["pdf-1"] = saved
	if _, err := fx.mgr.Control(snapID, "retry"); err != nil {
		t.Fatal(err)
	}
	waitDownload(t, fx.mgr, snapID)
	view, installed, err := fx.svc.Install(fx.mgr, fx.pkgs, "pdf-1", snapID)
	if err != nil || installed {
		t.Fatalf("install %+v %v %v", view, installed, err)
	}
	waitResource(t, fx, "pdf-1", "installed")
}

// TestErrorClearedByNewDownload installs corrupt bytes (stored error), then
// starts a fresh download and asserts the error is cleared immediately.
func TestErrorClearedByNewDownload(t *testing.T) {
	fx := setup(t, nil)
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	if err := os.WriteFile(filepath.Join(fx.dir, "downloads", snapID+".part"), []byte(strings.Repeat("X", len(pdfBytes))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "pdf-1", snapID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	waitResource(t, fx, "pdf-1", "failed")
	if _, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "pdf-1"); err != nil {
		t.Fatal(err)
	}
	var code string
	if err := fx.db.QueryRow(`SELECT error_code FROM education_resources WHERE id='pdf-1'`).Scan(&code); err != nil || code != "" {
		t.Fatalf("stored error not cleared: %q %v", code, err)
	}
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" && (v.Error != nil || (v.Status != "downloading" && v.Status != "downloaded")) {
			t.Fatalf("fresh download must supersede error: %+v", v)
		}
	}
}

// TestForgedReceiptRejectedOnRestart tampers the payload and forges the
// receipt to match: restart validation hashes the payload against the trusted
// pin (never the self-reported file hash) and must reject it.
func TestForgedReceiptRejectedOnRestart(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	final := filepath.Join(fx.dir, "content", "education", "pdf-1", "doc.pdf")
	tampered := append(append([]byte{}, pdfBytes...), []byte("evil")...)
	if err := os.WriteFile(final, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(tampered)
	forged, _ := json.Marshal(map[string]any{
		"id": "pdf-1", "version": "2026.01",
		"sha256": "b01fe9412bb8ff64d170d57b07a68e24d8effdde07e2c81b7f1afcb7a421aae8",
		"files":  []map[string]any{{"name": "doc.pdf", "size": int64(len(tampered)), "sha256": hex.EncodeToString(sum[:])}},
	})
	if err := os.WriteFile(filepath.Join(fx.dir, "content", "education", "pdf-1", "engelsiz-receipt.json"), forged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	res, err := svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" {
			if v.Status != "failed" {
				t.Fatalf("forged payload accepted: %+v", v)
			}
			if _, _, _, err := svc.Open("pdf-1"); !isCode(err, "not_found") {
				t.Fatalf("forged open: %v", err)
			}
		}
	}
}

// TestTruncatedFileCannotOpen truncates the installed payload: Open must
// refuse it and the listing must surface failure via the exact-size check.
func TestTruncatedFileCannotOpen(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	final := filepath.Join(fx.dir, "content", "education", "pdf-1", "doc.pdf")
	if err := os.Truncate(final, int64(len(pdfBytes)-5)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fx.svc.Open("pdf-1"); !isCode(err, "not_found") {
		t.Fatalf("truncated open: %v", err)
	}
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" && v.Status != "failed" {
			t.Fatalf("truncated must surface failed: %+v", v)
		}
	}
}

// TestSymlinkDirAndReceiptRejected replaces the install dir and (separately)
// the receipt with symlinks: both must surface failure without being
// followed, and Open must refuse.
func TestSymlinkDirAndReceiptRejected(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "html-1")
	final := filepath.Join(fx.dir, "content", "education", "html-1")
	if err := os.RemoveAll(final); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fx.dir, final); err != nil {
		t.Skip(err)
	}
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "html-1" && v.Status != "failed" {
			t.Fatalf("symlink dir must surface failed: %+v", v)
		}
	}
	if _, _, _, err := fx.svc.Open("html-1"); !isCode(err, "not_found") {
		t.Fatalf("symlink open: %v", err)
	}
	if err := os.Remove(final); err != nil {
		t.Fatal(err)
	}

	fx2 := setup(t, nil)
	downloadAndInstall(t, fx2, "pdf-1")
	receipt := filepath.Join(fx2.dir, "content", "education", "pdf-1", "engelsiz-receipt.json")
	raw, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(fx2.dir, "outside-receipt.json")
	if err := os.WriteFile(outside, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, receipt); err != nil {
		t.Skip(err)
	}
	if err := fx2.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx2.db, fx2.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx2.data), Packages: fx2.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	res, err = svc.List(fx2.mgr, fx2.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" && v.Status != "failed" {
			t.Fatalf("symlink receipt must surface failed: %+v", v)
		}
	}
}

// TestDeleteCrashReconcilesTrash simulates a crash between the trash rename
// and the row commit (payload must be restored), and a crash after the row
// commit (leftover trash must be dropped).
func TestDeleteCrashReconcilesTrash(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	final := filepath.Join(fx.dir, "content", "education", "pdf-1")
	trash := final + ".deleting"
	if err := os.Rename(final, trash); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" && v.Status != "installed" {
			t.Fatalf("crashed delete must restore payload: %+v", v)
		}
	}
	if _, err := os.Lstat(final); err != nil {
		t.Fatalf("restored payload missing: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	// Crash after the row commit: only the known trash name may be dropped.
	if err := os.Rename(final, trash); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`DELETE FROM education_resources WHERE id='pdf-1'`); err != nil {
		t.Fatal(err)
	}
	svc2, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()
	res, err = svc2.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "pdf-1" && v.Status != "missing" {
			t.Fatalf("committed delete must stay missing: %+v", v)
		}
	}
	if _, err := os.Lstat(trash); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("known trash not cleaned: %v", err)
	}
}

// TestStagingSweepNarrowsToOwnedIDs ensures restart removes only
// ".staging-<known-id>-*" names and preserves foreign files.
func TestStagingSweepNarrowsToOwnedIDs(t *testing.T) {
	fx := setup(t, nil)
	edu := filepath.Join(fx.dir, "content", "education")
	foreign := filepath.Join(edu, ".staging-evil")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(edu, "random.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(edu, ".staging-html-1-abcdef0123456789")
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	// Wrong-shaped staging-like names are preserved, not swept.
	odd := filepath.Join(edu, ".staging-html-1-xyz")
	if err := os.MkdirAll(odd, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`INSERT INTO education_resources(id,version,status) VALUES('html-1','2026.01','installing')`); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if _, err := os.Lstat(own); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned staging not cleaned: %v", err)
	}
	if _, err := os.Lstat(odd); err != nil {
		t.Fatalf("wrong-shaped staging must be preserved: %v", err)
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Fatalf("foreign staging disturbed: %v", err)
	}
	if raw, err := os.ReadFile(keep); err != nil || string(raw) != "keep" {
		t.Fatalf("foreign file disturbed: %q %v", raw, err)
	}
}

func seedTransfer(t *testing.T, fx *fixture, id, provider, packageID, contentType, variant, version string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	path := filepath.Join(fx.dir, "downloads", id+".part")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fx.db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, provider, packageID, "T "+packageID, variant, "f.zim", "https://upstream.invalid/private", path, len(body), len(body), "completed", stamp, stamp, contentType, "tr", version, len(body), "sha256", hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
}

func waitPackageInstalled(t *testing.T, fx *fixture, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		items, err := fx.pkgs.List("")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range items {
			if p.ID == id && p.Status == "installed" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("package %s not installed", id)
}

// TestZIMRejectsForeignInstalledID installs a real Wikipedia package, then
// requires the education ZIM install to reject that foreign download ID with
// metadata_unavailable instead of associating it.
func TestZIMRejectsForeignInstalledID(t *testing.T) {
	fx := setup(t, nil)
	wikiID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seedTransfer(t, fx, wikiID, "kiwix", "wiki-release", "wikipedia", "nopic", "2026-10-01", []byte("wikipedia zim bytes"))
	if _, _, err := fx.pkgs.Install(fx.mgr, wikiID); err != nil {
		t.Fatal(err)
	}
	waitPackageInstalled(t, fx, wikiID)
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", wikiID); !isCode(err, "metadata_unavailable") {
		t.Fatalf("foreign install must be rejected, got %v", err)
	}
	res, err := fx.svc.List(fx.mgr, fx.pkgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Resources {
		if v.ID == "zim-1" && (v.Status == "installed" || v.Status == "installing") {
			t.Fatalf("foreign association leaked: %+v", v)
		}
	}
}

// TestZIMFailureDerivedFromPackage tampers ZIM bytes so the delegated
// packages worker fails verification: the education view must surface failed
// with the package error, never installing forever.
func TestZIMFailureDerivedFromPackage(t *testing.T) {
	fx := setup(t, nil)
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "zim-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	if err := os.WriteFile(filepath.Join(fx.dir, "downloads", snapID+".part"), []byte(strings.Repeat("Z", len(zimBytes))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", snapID); err != nil {
		t.Fatalf("install must be accepted async, got %v", err)
	}
	got := waitResource(t, fx, "zim-1", "failed")
	if got.Error == nil {
		t.Fatalf("failed view missing package error: %+v", got)
	}
	out := deleteResource(t, fx, "zim-1")
	if out.Status != "missing" {
		t.Fatalf("delete after failure %+v", out)
	}
}

// TestZIMAdoptPersistsVerifiedID restarts with a stale row association: a
// newer superseding download exists while the verified install belongs to
// the older one. Recovery must persist the exact verified installed ID, not
// the stale row value.
func TestZIMAdoptPersistsVerifiedID(t *testing.T) {
	fx := setup(t, nil)
	view := downloadAndInstallZIM(t, fx)
	installedID := view.Download.ID
	// Simulate a stale row: a newer superseding download recorded while the
	// verified install still belongs to the older one.
	staleID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	seedTransfer(t, fx, staleID, "kiwix", "zim-1", "education", "edu-zim-1", "2026.01", zimBytes)
	if _, err := fx.db.Exec(`UPDATE education_resources SET download_id=?,status='installing' WHERE id='zim-1'`, staleID); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	var assoc string
	if err := fx.db.QueryRow(`SELECT download_id FROM education_resources WHERE id='zim-1'`).Scan(&assoc); err != nil || assoc != installedID {
		t.Fatalf("adopted association %q, want verified %q (%v)", assoc, installedID, err)
	}
}

// TestZIMInstallDifferentIDCollides installs a ZIM, then requests install of
// a different completed download of the same resource: collision, never a
// silent association with the unrelated job.
func TestZIMInstallDifferentIDCollides(t *testing.T) {
	fx := setup(t, nil)
	installed := downloadAndInstallZIM(t, fx)
	installedID := installed.Download.ID
	// A second completed transfer of the same pin, seeded directly (a fresh
	// StartDownload is correctly refused while installed).
	otherID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	seedTransfer(t, fx, otherID, "kiwix", "zim-1", "education", "edu-zim-1", "2026.01", zimBytes)
	if _, _, err := fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", otherID); !isCode(err, "target_collision") {
		t.Fatalf("different installed id must collide, got %v", err)
	}
	var assoc string
	if err := fx.db.QueryRow(`SELECT download_id FROM education_resources WHERE id='zim-1'`).Scan(&assoc); err != nil || assoc != installedID {
		t.Fatalf("association moved to %q, want %q", assoc, installedID)
	}
	waitResource(t, fx, "zim-1", "installed")
}

// TestZIMDeleteWithoutPackagesFails removes nothing when the packages
// service is unavailable: failing instead of clearing the row while the ZIM
// may still be served.
func TestZIMDeleteWithoutPackagesFails(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstallZIM(t, fx)
	if _, err := fx.svc.Delete(fx.mgr, nil, "zim-1"); !isCode(err, "storage_error") {
		t.Fatalf("nil-packages delete must fail, got %v", err)
	}
	waitResource(t, fx, "zim-1", "installed")
}

// TestPackagesStorageFailureBubbles passes a closed packages service: ZIM
// snapshots must surface the storage failure instead of misreporting.
func TestPackagesStorageFailureBubbles(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstallZIM(t, fx)
	dead, err := packages.New(context.Background(), fx.db, t.TempDir(), packages.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := dead.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.List(fx.mgr, dead); !isCode(err, "storage_error") {
		t.Fatalf("closed packages must bubble storage_error, got %v", err)
	}
}

func downloadAndInstallZIM(t *testing.T, fx *fixture) education.View {
	t.Helper()
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "zim-1")
	if err != nil {
		t.Fatal(err)
	}
	snapID := started.Download.ID
	waitDownload(t, fx.mgr, snapID)
	view, installed, err := fx.svc.Install(fx.mgr, fx.pkgs, "zim-1", snapID)
	if err != nil || installed {
		t.Fatalf("install accept %+v %v %v", view, installed, err)
	}
	return waitResource(t, fx, "zim-1", "installed")
}

// TestTombstoneStorageFaultFailsDelete drops the tombstone table to simulate
// a storage fault: Delete must fail instead of reporting success while stale
// transfers stay live, and the row and payload are preserved.
func TestTombstoneStorageFaultFailsDelete(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	if _, err := fx.db.Exec(`DROP TABLE education_tombstones`); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.Delete(fx.mgr, fx.pkgs, "pdf-1"); !isCode(err, "storage_error") {
		t.Fatalf("tombstone fault must fail delete, got %v", err)
	}
	var status string
	if err := fx.db.QueryRow(`SELECT status FROM education_resources WHERE id='pdf-1'`).Scan(&status); err != nil || status != "installed" {
		t.Fatalf("row not preserved: %q %v", status, err)
	}
	if _, err := os.Lstat(filepath.Join(fx.dir, "content", "education", "pdf-1", "doc.pdf")); err != nil {
		t.Fatalf("payload not preserved: %v", err)
	}
}

// TestTombstoneStorageFaultFailsRecover leaves delete trash behind with no
// row while the tombstone table is broken: restart must fail instead of
// silently dropping the trash or resurrecting state.
func TestTombstoneStorageFaultFailsRecover(t *testing.T) {
	fx := setup(t, nil)
	downloadAndInstall(t, fx, "pdf-1")
	final := filepath.Join(fx.dir, "content", "education", "pdf-1")
	if err := os.Rename(final, final+".deleting"); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`DELETE FROM education_resources WHERE id='pdf-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`DROP TABLE education_tombstones`); err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := education.New(fx.db, fx.dir, education.Options{Resources: fixtureResources("http://127.0.0.1:1", fx.data), Packages: fx.pkgs}); err == nil {
		t.Fatal("recover with broken tombstones must fail")
	}
}

// TestZIMStartDownloadDuringDelegatedInstall seeds a verifying packages
// record for the current association, then requires StartDownload to refuse
// with collision instead of overwriting the row (which would let Delete of
// the new id orphan the served install).
func TestZIMStartDownloadDuringDelegatedInstall(t *testing.T) {
	fx := setup(t, nil)
	started, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "zim-1")
	if err != nil {
		t.Fatal(err)
	}
	assoc := started.Download.ID
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := fx.db.Exec(`INSERT INTO content_packages(id,download_id,provider,provider_package_id,content_type,language,variant,title,version,source_url,source_path,local_path,size_bytes,checksum_algorithm,checksum_value,status,error_code,error_message,created_at,updated_at,installed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		assoc, assoc, "kiwix", "zim-1", "education", "tr", "edu-zim-1", "T zim-1", "2026.01", "https://upstream.invalid/private", filepath.Join(fx.dir, "downloads", assoc+".part"), filepath.Join(fx.dir, "content", "kiwix", assoc+".zim"), len(zimBytes), "sha256", "00", "verifying", "", "", stamp, stamp, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.StartDownload(fx.mgr, fx.pkgs, "zim-1"); !isCode(err, "target_collision") {
		t.Fatalf("download during delegated install must collide, got %v", err)
	}
	var kept string
	if err := fx.db.QueryRow(`SELECT download_id FROM education_resources WHERE id='zim-1'`).Scan(&kept); err != nil || kept != assoc {
		t.Fatalf("association moved to %q, want %q", kept, assoc)
	}
}
