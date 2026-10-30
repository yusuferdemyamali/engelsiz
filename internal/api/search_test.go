package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"engelsiz/internal/packages"
	"engelsiz/internal/providers"
)

const selectedSearchID = "0123456789abcdef0123456789abcdef"

type searchStub func(context.Context, string, string) (providers.SearchResponse, error)

func (s searchStub) Search(ctx context.Context, id, q string) (providers.SearchResponse, error) {
	return s(ctx, id, q)
}
func (s searchStub) Catalog(context.Context) ([]providers.CatalogPackage, error) {
	panic("search must never call catalog")
}

type installedSearchStub func() ([]packages.InstalledFile, error)

func (s installedSearchStub) InstalledSnapshot() ([]packages.InstalledFile, error) { return s() }

func searchInstalled() ([]packages.InstalledFile, error) {
	return []packages.InstalledFile{{PackageID: selectedSearchID, Path: "/private/content/" + selectedSearchID + ".zim"}}, nil
}

func searchTarget(query string) string {
	return "/api/search?" + url.Values{"q": {query}, "packageId": {selectedSearchID}}.Encode()
}

func assertSearchError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct {
		Error RuntimeError `json:"error"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || rec.Code != status || body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("response = %d %s; want %d %s", rec.Code, rec.Body, status, code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("headers: %v", rec.Header())
	}
	for _, secret := range []string{"private", "127.0.0.1", "zim", "upstream"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("leaked %q: %s", secret, rec.Body)
		}
	}
}

func TestSearchHTTPContractAndUnicodeTrim(t *testing.T) {
	for _, q := range []string{"\u2003 bilgi ışık \u00a0", "\u2003  ", strings.Repeat("ğ", 256)} {
		t.Run(fmt.Sprintf("query-%d", len(q)), func(t *testing.T) {
			calls := 0
			handler := SearchHandler(searchStub(func(ctx context.Context, id, query string) (providers.SearchResponse, error) {
				calls++
				if id != selectedSearchID || query != strings.TrimSpace(q) {
					t.Fatalf("id=%q query=%q", id, query)
				}
				return providers.SearchResponse{Query: query, PackageID: id}, nil
			}), installedSearchStub(searchInstalled))
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest("GET", searchTarget(q), nil))
			var result providers.SearchResponse
			if rec.Code != 200 || calls != 1 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Query != strings.TrimSpace(q) || result.PackageID != selectedSearchID || result.Results == nil || result.TotalResults != 0 {
				t.Fatalf("success=%d %s calls=%d", rec.Code, rec.Body, calls)
			}
		})
	}
}

func TestSearchRejectsInvalidRequestsBeforeLocalOrProviderWork(t *testing.T) {
	target := searchTarget("wiki")
	for _, suffix := range []string{
		"/api/search", "/api/search?q=wiki", "/api/search?packageId=" + selectedSearchID,
		target + "&q=other", target + "&packageId=" + selectedSearchID, target + "&target=http://127.0.0.1:7",
		target + "&broken=%GG", strings.Replace(target, selectedSearchID, strings.ToUpper(selectedSearchID), 1),
		searchTarget("a\x00b"), searchTarget("a\nb"), searchTarget("\tquery"), searchTarget("\u0085query"),
		searchTarget(string([]byte{0xff})), searchTarget(strings.Repeat("ğ", 257)), searchTarget(strings.Repeat("a", 9000)),
		"/api/search?q=a;b&packageId=" + selectedSearchID,
	} {
		t.Run(suffix[:min(len(suffix), 100)], func(t *testing.T) {
			handler := SearchHandler(searchStub(func(context.Context, string, string) (providers.SearchResponse, error) {
				t.Fatal("provider called")
				return providers.SearchResponse{}, nil
			}), installedSearchStub(func() ([]packages.InstalledFile, error) { t.Fatal("snapshot called"); return nil, nil }))
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest("GET", suffix, nil))
			assertSearchError(t, rec, 400, "invalid_request")
		})
	}
	handler := SearchHandler(nil, nil)
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("POST", target, nil))
	assertSearchError(t, rec, 405, "method_not_allowed")
	if rec.Header().Get("Allow") != "GET" {
		t.Fatal(rec.Header())
	}
}

func TestSearchErrorsPrecedenceAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{providers.ErrContentUnavailable, 409, "content_unavailable"},
		{providers.ErrRuntimeUnavailable, 503, "runtime_unavailable"},
		{context.DeadlineExceeded, 504, "search_timeout"},
		{context.Canceled, 502, "search_unavailable"},
		{errors.New("upstream private /tmp/secret.zim http://127.0.0.1:9000"), 502, "search_unavailable"},
	} {
		t.Run(tc.code+tc.err.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			handler := SearchHandler(searchStub(func(received context.Context, _, _ string) (providers.SearchResponse, error) {
				if received != ctx || received.Err() != context.Canceled {
					t.Fatal("request context not propagated")
				}
				return providers.SearchResponse{}, fmt.Errorf("provider failed: %w", tc.err)
			}), installedSearchStub(searchInstalled))
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest("GET", searchTarget("wiki"), nil).WithContext(ctx))
			assertSearchError(t, rec, tc.status, tc.code)
		})
	}
	for _, q := range []string{"wiki", ""} {
		rec := httptest.NewRecorder()
		SearchHandler(nil, installedSearchStub(func() ([]packages.InstalledFile, error) { return nil, nil }))(rec, httptest.NewRequest("GET", searchTarget(q), nil))
		assertSearchError(t, rec, 409, "content_unavailable")
	}
	rec := httptest.NewRecorder()
	SearchHandler(nil, installedSearchStub(searchInstalled))(rec, httptest.NewRequest("GET", searchTarget(""), nil))
	assertSearchError(t, rec, 503, "runtime_unavailable")
}

func TestSearchUsesVerifiedSnapshotAfterInstalledFileChanges(t *testing.T) {
	f := newPackageFixture(t, packages.Options{})
	downloadID := f.seed(t, 77, []byte("verified search package"))
	packageRequest(t, f.install, "POST", "/api/downloads/"+downloadID+"/install", `{}`, 202)
	installed := waitPackage(t, f, downloadID, "installed")
	calls := 0
	handler := SearchHandler(searchStub(func(_ context.Context, id, q string) (providers.SearchResponse, error) {
		calls++
		return providers.SearchResponse{Query: q, PackageID: id}, nil
	}), f.service)
	target := "/api/search?" + url.Values{"q": {"wiki"}, "packageId": {installed.ID}}.Encode()
	packageRequest(t, handler, "GET", target, "", 200)
	files, err := f.service.InstalledSnapshot()
	if err != nil || len(files) != 1 {
		t.Fatalf("snapshot=%v %v", files, err)
	}
	if err := os.WriteFile(filepath.Clean(files[0].Path), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest("GET", target, nil))
	assertSearchError(t, rec, 409, "content_unavailable")
	if calls != 1 {
		t.Fatalf("unverified file reached provider: calls=%d", calls)
	}
}
