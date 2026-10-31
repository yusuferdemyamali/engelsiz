package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"engelsiz/internal/database"
	"engelsiz/internal/download"
	"engelsiz/internal/packages"
)

type packageFixture struct {
	db      *sql.DB
	root    string
	service *packages.Service
	manager *download.Manager
	install http.HandlerFunc
	list    http.HandlerFunc
}

func newPackageFixture(t *testing.T, options packages.Options) *packageFixture {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := packages.New(context.Background(), db, root, options)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	manager, err := download.New(db, filepath.Join(root, "downloads"), download.Options{})
	if err != nil {
		service.Close()
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(); manager.Close(); db.Close() })
	return &packageFixture{db: db, root: root, service: service, manager: manager, install: DownloadsHandler(manager, nil, service), list: PackagesHandler(service)}
}
func (f *packageFixture) seed(t *testing.T, n int, body []byte) string {
	t.Helper()
	id := fmt.Sprintf("%032x", n)
	if err := os.WriteFile(filepath.Join(f.root, "downloads", id+".part"), body, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	_, err := f.db.Exec(`INSERT INTO downloads(id,provider,provider_package_id,title,variant,file_name,source_url,temp_path,total_bytes,downloaded_bytes,status,created_at,updated_at,content_type,language,version,expected_size,checksum_algorithm,checksum_value) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "kiwix", "urn:uuid:"+id, "Vikipedi", "mini", "selected.zim", "https://private.example/source.zim", filepath.Join(f.root, "downloads", id+".part"), len(body), len(body), "completed", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "wikipedia", "tr", "2026-01-01", len(body), "sha256", hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func packageRequest(t *testing.T, h http.HandlerFunc, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRecorder()
	h(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	if r.Code != want {
		t.Fatalf("%s %s: status=%d body=%s want=%d", method, path, r.Code, r.Body, want)
	}
	if r.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("missing safe JSON headers: %v", r.Header())
	}
	return r
}
func decodePackage(t *testing.T, r *httptest.ResponseRecorder) packages.Package {
	t.Helper()
	var result struct {
		Package packages.Package `json:"package"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Package
}
func waitPackage(t *testing.T, f *packageFixture, id, status string) packages.Package {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := f.service.List("")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.DownloadID == id && item.Status == status {
				return item
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("package %s did not reach %s", id, status)
	return packages.Package{}
}
func assertPackageRedaction(t *testing.T, f *packageFixture, r *httptest.ResponseRecorder) {
	t.Helper()
	for _, secret := range []string{f.root, "private.example", "sourceURL", "source_url", "sourcePath", "source_path", "localPath", "local_path", "tempPath", "temp_path"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Fatalf("package response leaked %q: %s", secret, r.Body)
		}
	}
}
func TestPackageHTTPAcceptedDuplicateInstalledAndRequestCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var verifies atomic.Int32
	f := newPackageFixture(t, packages.Options{Hook: func(ctx context.Context, stage string) error {
		if stage != "verify" {
			return nil
		}
		if verifies.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	id := f.seed(t, 1, []byte("payload"))
	path := "/api/downloads/" + id + "/install"
	empty := packageRequest(t, f.list, "GET", "/api/packages", "", 200)
	if strings.TrimSpace(empty.Body.String()) != `{"packages":[]}` {
		t.Fatalf("empty list: %s", empty.Body)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRecorder()
	f.install(r, httptest.NewRequest("POST", path, strings.NewReader(`{}`)).WithContext(ctx))
	if r.Code != 202 {
		t.Fatalf("accepted: %d %s", r.Code, r.Body)
	}
	cancel()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not start")
	}
	first := decodePackage(t, r)
	if first.Status != "verifying" || first.ID == "" || first.DownloadID != id || first.InstalledAt != nil || first.Error != nil || first.ContentType != "wikipedia" || first.Language != "tr" || first.Version != "2026-01-01" || first.Variant != "mini" || first.SizeBytes != 7 || first.Checksum.Algorithm != "sha256" {
		t.Fatalf("accepted snapshot: %+v", first)
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(r.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for key := range raw["package"] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"active", "checksum", "contentType", "createdAt", "downloadId", "error", "id", "installedAt", "language", "provider", "providerPackageId", "sizeBytes", "status", "title", "updatedAt", "variant", "version"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("public package fields: %v", keys)
	}
	assertPackageRedaction(t, f, r)
	second := decodePackage(t, packageRequest(t, f.install, "POST", path, `{}`, 202))
	if second.ID != first.ID {
		t.Fatal("active duplicate changed package ID")
	}
	concurrent := make(chan *httptest.ResponseRecorder, 4)
	for range 4 {
		go func() {
			recorder := httptest.NewRecorder()
			f.install(recorder, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
			concurrent <- recorder
		}()
	}
	for range 4 {
		recorder := <-concurrent
		if recorder.Code != 202 || decodePackage(t, recorder).ID != first.ID {
			t.Fatalf("concurrent duplicate: %d %s", recorder.Code, recorder.Body)
		}
	}
	if got := verifies.Load(); got != 1 {
		t.Fatalf("duplicate verification workers: %d", got)
	}
	activeList := packageRequest(t, f.list, "GET", "/api/packages", "", 200)
	assertPackageRedaction(t, f, activeList)
	filtered := packageRequest(t, f.list, "GET", "/api/packages?status=installed", "", 200)
	if strings.TrimSpace(filtered.Body.String()) != `{"packages":[]}` {
		t.Fatalf("active package included as installed: %s", filtered.Body)
	}
	close(release)
	installed := waitPackage(t, f, id, "installed")
	if installed.ID != first.ID || installed.InstalledAt == nil || installed.Error != nil {
		t.Fatalf("installed: %+v", installed)
	}
	repeated := decodePackage(t, packageRequest(t, f.install, "POST", path, `{}`, 200))
	if repeated.ID != first.ID || repeated.InstalledAt == nil || *repeated.InstalledAt != *installed.InstalledAt {
		t.Fatalf("installed duplicate changed identity: %+v", repeated)
	}
	list := packageRequest(t, f.list, "GET", "/api/packages?status=installed", "", 200)
	assertPackageRedaction(t, f, list)
	var rows struct {
		Packages []packages.Package `json:"packages"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &rows); err != nil || len(rows.Packages) != 1 || rows.Packages[0].ID != first.ID {
		t.Fatalf("installed listing: %s %v", list.Body, err)
	}
	snapshot, err := f.manager.Get(id)
	if err != nil || snapshot.Status != "completed" {
		t.Fatalf("consumed transfer history: %+v %v", snapshot, err)
	}
}
func TestPackageHTTPValidationAndEligibilityErrors(t *testing.T) {
	f := newPackageFixture(t, packages.Options{})
	id := f.seed(t, 1, []byte("payload"))
	path := "/api/downloads/" + id + "/install"
	for _, body := range []string{"", `not-json`, `[]`, `null`, `{"path":"/private/file"}`, `{} {}`, `{"retry":true}`} {
		packageRequest(t, f.install, "POST", path, body, 400)
	}
	for _, filter := range []string{"failed", "verifying", "installing", "unknown"} {
		packageRequest(t, f.list, "GET", "/api/packages?status="+filter, "", 400)
	}
	for _, query := range []string{"status=", "unexpected=value", "status=installed&status=installed", "status=installed&unexpected=value"} {
		packageRequest(t, f.list, "GET", "/api/packages?"+query, "", 400)
	}
	packageRequest(t, f.install, "POST", "/api/downloads/missing/install", `{}`, 404)
	rec := packageRequest(t, f.install, "GET", path, "", 405)
	if rec.Header().Get("Allow") != "POST" {
		t.Fatal("install Allow header")
	}
	rec = packageRequest(t, f.list, "POST", "/api/packages", `{}`, 405)
	if rec.Header().Get("Allow") != "GET" {
		t.Fatal("package list Allow header")
	}
	for i, tc := range []struct {
		name, sql string
		status    int
		code      string
	}{
		{"incomplete", `UPDATE downloads SET status='paused' WHERE id=?`, 409, "invalid_state"},
		{"missing metadata", `UPDATE downloads SET version=NULL WHERE id=?`, 422, "metadata_unavailable"},
		{"missing exact size", `UPDATE downloads SET expected_size=NULL WHERE id=?`, 422, "metadata_unavailable"},
		{"missing checksum", `UPDATE downloads SET checksum_algorithm=NULL,checksum_value=NULL WHERE id=?`, 422, "checksum_unavailable"},
		{"unsupported checksum", `UPDATE downloads SET checksum_algorithm='sha1' WHERE id=?`, 422, "checksum_unavailable"},
		{"ownership", `UPDATE downloads SET artifact_owner='another-package',artifact_state='claimed' WHERE id=?`, 409, "ownership_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := f.seed(t, i+2, []byte("payload"))
			if _, err := f.db.Exec(tc.sql, id); err != nil {
				t.Fatal(err)
			}
			r := packageRequest(t, f.install, "POST", "/api/downloads/"+id+"/install", `{}`, tc.status)
			if !strings.Contains(r.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("error envelope: %s", r.Body)
			}
			assertPackageRedaction(t, f, r)
		})
	}
	collision := f.seed(t, 20, []byte("payload"))
	target := filepath.Join(f.root, "content", "kiwix", collision+".zim")
	if err := os.WriteFile(target, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	rec = packageRequest(t, f.install, "POST", "/api/downloads/"+collision+"/install", `{}`, 409)
	if !strings.Contains(rec.Body.String(), "target_collision") {
		t.Fatalf("collision: %s", rec.Body)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "foreign" {
		t.Fatalf("foreign destination overwritten: %q %v", body, err)
	}
}
func TestPackageHTTPExplicitRetryReusesIdentity(t *testing.T) {
	f := newPackageFixture(t, packages.Options{})
	id := f.seed(t, 1, []byte("payload"))
	source := filepath.Join(f.root, "downloads", id+".part")
	if err := os.WriteFile(source, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/downloads/" + id + "/install"
	first := decodePackage(t, packageRequest(t, f.install, "POST", path, `{}`, 202))
	failed := waitPackage(t, f, id, "failed")
	if failed.Error == nil || failed.Error.Code != "verification_failed" || failed.InstalledAt != nil {
		t.Fatalf("failed state: %+v", failed)
	}
	if err := os.WriteFile(source, []byte("payload"), 0600); err != nil {
		t.Fatal(err)
	}
	retried := decodePackage(t, packageRequest(t, f.install, "POST", path, `{}`, 202))
	if retried.ID != first.ID || retried.CreatedAt != first.CreatedAt {
		t.Fatalf("retry changed identity: %+v", retried)
	}
	installed := waitPackage(t, f, id, "installed")
	if installed.ID != first.ID {
		t.Fatal("retry duplicated package")
	}
}
func TestPackageHTTPStorageFailuresAreSafe(t *testing.T) {
	f := newPackageFixture(t, packages.Options{})
	id := f.seed(t, 1, []byte("payload"))
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		handler            http.HandlerFunc
		method, path, body string
	}{
		{f.list, "GET", "/api/packages", ""},
		{f.install, "POST", "/api/downloads/" + id + "/install", `{}`},
	} {
		r := packageRequest(t, request.handler, request.method, request.path, request.body, 500)
		if !strings.Contains(r.Body.String(), `"code":"storage_error"`) {
			t.Fatalf("storage envelope: %s", r.Body)
		}
		assertPackageRedaction(t, f, r)
		if strings.Contains(r.Body.String(), "sql:") || strings.Contains(r.Body.String(), "database is closed") {
			t.Fatalf("raw storage error leaked: %s", r.Body)
		}
	}
}
