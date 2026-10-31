package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/download"
	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

type updateProvider struct {
	providers.ContentProvider
	catalog []providers.CatalogPackage
	source  providers.DownloadSource
	err     error
	calls   int
}

func (p *updateProvider) Catalog(context.Context) ([]providers.CatalogPackage, error) {
	p.calls++
	return p.catalog, p.err
}
func (p *updateProvider) ResolveDownload(context.Context, string) (providers.DownloadSource, error) {
	return p.source, p.err
}

type updateRoundTripper func(*http.Request) (*http.Response, error)

func (f updateRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func updateFixture(t *testing.T) (*packageFixture, string, *updateProvider) {
	t.Helper()
	f := newPackageFixture(t, packages.Options{})
	id := f.seed(t, 1, []byte("old ZIM"))
	if _, _, e := f.service.Install(f.manager, id); e != nil {
		t.Fatal(e)
	}
	waitPackage(t, f, id, "installed")
	body := "candidate ZIM"
	size := int64(len(body))
	sum := sha256.Sum256([]byte(body))
	date := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	p := &updateProvider{catalog: []providers.CatalogPackage{{Provider: "kiwix", ProviderPackageID: "candidate", Language: "tr", Variant: "mini", PublishedAt: &date, SizeBytes: &size}}, source: providers.DownloadSource{Provider: "kiwix", ProviderPackageID: "candidate", Title: "Wikipedia", Variant: "mini", ContentType: "wikipedia", Language: "tr", Version: "2026-02-01", FileName: "candidate.zim", SourceURL: "https://example.invalid/candidate.zim", ExpectedSize: &size, TotalBytes: &size, ChecksumAlgorithm: "sha256", ChecksumValue: hex.EncodeToString(sum[:])}}
	f.manager.Close()
	manager, e := download.New(f.db, filepath.Join(f.root, "downloads"), download.Options{HTTPClient: &http.Client{Transport: updateRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: size, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}})
	if e != nil {
		t.Fatal(e)
	}
	f.manager = manager
	t.Cleanup(func() { manager.Close() })
	return f, id, p
}
func TestUpdatesHTTPComparisonCreationAndOfflineLocalState(t *testing.T) {
	f, id, p := updateFixture(t)
	list := UpdatesHandler(f.service, f.manager, p)
	create := UpdatePackageHandler(f.service, f.manager, p)
	offer := packageRequest(t, list, "GET", "/api/updates", "", 200)
	if !strings.Contains(offer.Body.String(), `"comparisonAvailable":true`) || !strings.Contains(offer.Body.String(), `"state":"update_available"`) {
		t.Fatalf("offer %s", offer.Body)
	}
	if downloads, e := f.manager.List(); e != nil || len(downloads) != 1 {
		t.Fatalf("GET started transfer %+v %v", downloads, e)
	}
	rec := packageRequest(t, create, "POST", "/api/packages/"+id+"/update", `{"providerPackageId":"candidate"}`, 201)
	var response struct {
		Update packages.Update `json:"update"`
	}
	if e := json.Unmarshal(rec.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	u := response.Update
	if u.ID == "" || u.DownloadID == "" || u.PackageID != id {
		t.Fatalf("operation %+v", u)
	}
	packageRequest(t, create, "POST", "/api/packages/"+id+"/update", `{"providerPackageId":"candidate"}`, 409)
	p.err = errors.New("private diagnostic 127.0.0.1:9999 /private/data")
	offline := packageRequest(t, list, "GET", "/api/updates", "", 200)
	if !strings.Contains(offline.Body.String(), u.ID) || !strings.Contains(offline.Body.String(), `"comparisonAvailable":false`) || !strings.Contains(offline.Body.String(), `"catalog_unavailable"`) {
		t.Fatalf("offline %s", offline.Body)
	}
	assertPackageRedaction(t, f, offline)
	if strings.Contains(offline.Body.String(), "private diagnostic") {
		t.Fatal("private error leaked")
	}
	before := p.calls
	local := packageRequest(t, list, "GET", "/api/updates?local=1", "", 200)
	if p.calls != before || !strings.Contains(local.Body.String(), u.ID) {
		t.Fatalf("local fetched catalog: %s", local.Body)
	}
	packageRequest(t, list, "POST", "/api/updates/"+u.ID+"/activate", `{}`, 409)
}
func TestUpdateHTTPRejectsUntrustedAndStaleInput(t *testing.T) {
	f, id, p := updateFixture(t)
	create := UpdatePackageHandler(f.service, f.manager, p)
	list := UpdatesHandler(f.service, f.manager, p)
	for _, body := range []string{`{}`, `{"providerPackageId":"candidate","url":"https://evil.invalid"}`, `{"providerPackageId":"candidate"} {}`, `null`, `{"providerPackageId":42}`} {
		packageRequest(t, create, "POST", "/api/packages/"+id+"/update", body, 400)
	}
	p.source.Variant = "maxi"
	packageRequest(t, create, "POST", "/api/packages/"+id+"/update", `{"providerPackageId":"candidate"}`, 409)
	p.source.Variant = "mini"
	p.source.Version = "2026-01-01"
	packageRequest(t, create, "POST", "/api/packages/"+id+"/update", `{"providerPackageId":"candidate"}`, 409)
	p.source.Version = "2026-02-01"
	p.source.ChecksumValue = ""
	packageRequest(t, create, "POST", "/api/packages/"+id+"/update", `{"providerPackageId":"candidate"}`, 422)
	packageRequest(t, list, "GET", "/api/updates?source=evil", "", 400)
	packageRequest(t, list, "POST", "/api/updates/missing/activate", `{}`, 404)
	packageRequest(t, list, "POST", "/api/updates/missing/activate", `{"path":"/tmp/a"}`, 400)
}

func TestLaterOfferVisibleAfterCandidateActivationFailed(t *testing.T) {
	f, id, p := updateFixture(t)
	_, e := f.db.Exec(`INSERT INTO package_updates(id,package_id,candidate_provider_package_id,installed_version,candidate_version,state,error_code,error_message,created_at,updated_at) VALUES('failed-old',?,'prior-candidate','2026-01-01','2026-01-15','failed','activation_failed','Önceki aday açılamadı.','2026-01-20T00:00:00Z','2026-01-20T00:00:00Z')`, id)
	if e != nil {
		t.Fatal(e)
	}
	rec := packageRequest(t, UpdatesHandler(f.service, f.manager, p), "GET", "/api/updates", "", 200)
	if !strings.Contains(rec.Body.String(), `"state":"update_available"`) || !strings.Contains(rec.Body.String(), `"id":"failed-old"`) {
		t.Fatalf("later release hidden or history lost %s", rec.Body)
	}
}
