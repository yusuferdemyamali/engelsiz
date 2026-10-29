package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"engelsiz/internal/providers"
	"engelsiz/internal/web"
)

type catalogStub func(context.Context) ([]providers.CatalogPackage, error)

func (stub catalogStub) Catalog(ctx context.Context) ([]providers.CatalogPackage, error) {
	return stub(ctx)
}

func TestCatalogSuccessAndEmpty(t *testing.T) {
	for _, packages := range [][]providers.CatalogPackage{nil, {}, {{Provider: "kiwix", ProviderPackageID: "urn:uuid:id", Title: "Vikipedi", Language: "tr", Variant: "mini", Download: providers.Acquisition{Kind: "metalink", URL: "https://example.org/file.zim.meta4", MediaType: "application/x-zim"}}}} {
		handler := CatalogHandler(catalogStub(func(context.Context) ([]providers.CatalogPackage, error) { return packages, nil }), nil)
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/api/catalog", nil))
		if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%d %v", rec.Code, rec.Header())
		}
		var body struct {
			Packages []providers.CatalogPackage `json:"packages"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Packages == nil || len(body.Packages) != len(packages) {
			t.Fatalf("body: %s", rec.Body)
		}
		if len(packages) > 0 && body.Packages[0].Download.Kind != "metalink" {
			t.Fatal("acquisition lost")
		}
	}
}

func TestCatalogMethodAndFailureIsolation(t *testing.T) {
	var diagnostics bytes.Buffer
	calls := 0
	mux := NewMux()
	mux.HandleFunc("/api/catalog", CatalogHandler(catalogStub(func(context.Context) ([]providers.CatalogPackage, error) {
		calls++
		return nil, errors.New("upstream secret /private/profile 127.0.0.1:9911")
	}), log.New(&diagnostics, "", 0)))
	mux.Handle("/", web.Handler())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/catalog", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET" || calls != 0 {
		t.Fatalf("method: %d %v calls=%d", rec.Code, rec.Header(), calls)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/catalog", nil))
	want := "{\"error\":{\"code\":\"catalog_unavailable\",\"message\":\"Katalog şu anda yenilenemiyor. Lütfen tekrar deneyin.\"}}\n"
	if rec.Code != 503 || rec.Body.String() != want {
		t.Fatalf("error: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(diagnostics.String(), "upstream secret") {
		t.Fatal("missing technical diagnostics")
	}
	for _, path := range []string{"/api/health", "/"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s after catalog failure: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestCatalogRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reached := false
	handler := CatalogHandler(catalogStub(func(received context.Context) ([]providers.CatalogPackage, error) {
		reached = received == ctx && errors.Is(received.Err(), context.Canceled)
		return nil, received.Err()
	}), nil)
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/catalog", nil).WithContext(ctx))
	if !reached || rec.Code != 503 {
		t.Fatalf("context propagated=%v status=%d", reached, rec.Code)
	}
}
