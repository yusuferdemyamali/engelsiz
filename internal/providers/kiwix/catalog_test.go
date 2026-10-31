package kiwix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/providers"
)

func testProvider(t *testing.T, handler http.HandlerFunc, client *http.Client) *KiwixProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p, err := New(Options{CatalogURL: server.URL, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func feed(total, start, perPage int, entries ...string) string {
	return fmt.Sprintf(`<feed xmlns="%s" xmlns:dc="%s"><totalResults>%d</totalResults><startIndex>%d</startIndex><itemsPerPage>%d</itemsPerPage>%s</feed>`, atomNS, dcNS, total, start, perPage, strings.Join(entries, ""))
}

func entry(id, variant, issued string) string {
	return fmt.Sprintf(`<entry><id>urn:uuid:%s</id><title>Vikipedi</title><summary>Gerçek &amp; sade</summary><language>tur</language><name>wikipedia_tr_all</name><category>wikipedia</category><flavour>%s</flavour><dc:issued>%s</dc:issued><updated>2026-01-01T00:00:00Z</updated><link rel="http://opds-spec.org/acquisition/open-access" type="application/x-zim" href="https://example.org/%s.zim.meta4" length="1234"/></entry>`, id, variant, issued, id)
}

func TestCatalogLiveFormatFixture(t *testing.T) {
	// Captured public OPDS page, 2026-10-29. The fixture's reported total is
	// adjusted to the ten captured entries, leaving entry metadata untouched.
	body, err := os.ReadFile("testdata/entries.xml")
	if err != nil {
		t.Fatal(err)
	}
	xml := strings.Replace(string(body), "<totalResults>29</totalResults>", "<totalResults>10</totalResults>", 1)
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lang") != "tur" || r.URL.Query().Get("q") != "wikipedia" || r.URL.Query().Get("count") != "100" || r.URL.Query().Get("start") != "0" {
			t.Errorf("filters: %v", r.URL.Query())
		}
		fmt.Fprint(w, xml)
	}, nil)
	before := p.Status()
	packages, err := p.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 3 {
		t.Fatalf("packages: %+v", packages)
	}
	for i, variant := range []string{"mini", "nopic", "maxi"} {
		pkg := packages[i]
		if pkg.Variant != variant || pkg.Provider != "kiwix" || pkg.Language != "tr" || !strings.HasPrefix(pkg.ProviderPackageID, "urn:uuid:") || pkg.Download.Kind != "metalink" || !strings.HasSuffix(pkg.Download.URL, ".zim.meta4") || pkg.Download.MediaType != "application/x-zim" || pkg.PublishedAt == nil || pkg.SizeBytes == nil || pkg.Description == "" {
			t.Errorf("package: %+v", pkg)
		}
	}
	if after := p.Status(); before != after {
		t.Fatalf("runtime mutated: %+v -> %+v", before, after)
	}
}

func TestCatalogPaginationSelectionAndFiltering(t *testing.T) {
	pages := [][]string{
		{entry("old", "mini", "2025-01-01"), entry("maxi", "maxi", "2026-01-01")},
		{entry("new", "mini", "2026-02-01"), entry("nopic", "nopic", "2026-01-01")},
		{strings.Replace(entry("topic", "mini", "2027-01-01"), "wikipedia_tr_all", "wikipedia_tr_basketball", 1), strings.Replace(entry("foreign", "maxi", "2027-01-01"), "<language>tur</language>", "<language>eng</language>", 1)},
		{entry("unknown", "custom", "2027-01-01"), strings.Replace(entry("other", "maxi", "2027-01-01"), "<category>wikipedia</category>", "<category>books</category>", 1)},
	}
	calls := 0
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("start"))
		calls++
		if offset < 0 || offset/2 >= len(pages) {
			t.Errorf("offset %d", offset)
			return
		}
		fmt.Fprint(w, feed(8, offset, 2, pages[offset/2]...))
	}, nil)
	packages, err := p.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || len(packages) != 3 || packages[0].ProviderPackageID != "urn:uuid:new" || packages[1].Variant != "nopic" || packages[2].Variant != "maxi" {
		t.Fatalf("calls=%d packages=%+v", calls, packages)
	}
}

func TestCatalogOptionalMetadataAndAcquisition(t *testing.T) {
	raw := entry("unknown", "mini", "bad")
	raw = strings.ReplaceAll(raw, `length="1234"`, `length="-1"`)
	raw = strings.ReplaceAll(raw, "2026-01-01T00:00:00Z", "bad")
	raw = strings.ReplaceAll(raw, "<summary>Gerçek &amp; sade</summary>", "")
	raw = strings.ReplaceAll(raw, "https://example.org/unknown.zim.meta4", "https://example.org/unknown.zim")
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(1, 0, 100, raw)) }, nil)
	packages, err := p.Catalog(context.Background())
	if err != nil || len(packages) != 1 {
		t.Fatalf("%+v %v", packages, err)
	}
	pkg := packages[0]
	if pkg.PublishedAt != nil || pkg.UpdatedAt != nil || pkg.SizeBytes != nil || pkg.Description != "" || pkg.Download.Kind != "zim" {
		t.Fatalf("fabricated metadata: %+v", pkg)
	}
}

func TestNormalizeRejectsUnsafeOrIncompleteEntries(t *testing.T) {
	base, _ := http.NewRequest(http.MethodGet, "https://library.example/catalog/entries", nil)
	e := catalogEntry{ID: "urn:uuid:id", Title: "Title", Language: "tur", Name: "wikipedia_tr_all", Category: "wikipedia", Flavour: "mini"}
	for _, href := range []string{"http://example.org/file.zim", "javascript:evil.zim", "https://user:pass@example.org/file.zim", "https://example.org/file.zip", "", "https://example.org/file.zim#fragment"} {
		e.Links = []catalogLink{{Rel: "http://opds-spec.org/acquisition/open-access", Href: href}}
		if _, ok := normalizeEntry(e, base.URL); ok {
			t.Errorf("accepted %q", href)
		}
	}
	e.Links = []catalogLink{{Rel: "http://opds-spec.org/acquisition/open-access", Href: "../file.zim.meta4"}}
	pkg, ok := normalizeEntry(e, base.URL)
	if !ok || pkg.Download.URL != "https://library.example/file.zim.meta4" || pkg.Download.Kind != "metalink" {
		t.Fatalf("relative acquisition: %+v %v", pkg, ok)
	}
	e.ID = " "
	if _, ok := normalizeEntry(e, base.URL); ok {
		t.Fatal("accepted missing identity")
	}
}

func TestCatalogReleaseOrdering(t *testing.T) {
	p := func(id, published, updated, href string) providers.CatalogPackage {
		return providers.CatalogPackage{ProviderPackageID: id, PublishedAt: catalogDate(published), UpdatedAt: catalogDate(updated), Download: providers.Acquisition{URL: href}}
	}
	for _, tc := range []struct {
		a, b providers.CatalogPackage
		want bool
	}{
		{p("a", "2026-01-01", "", ""), p("z", "2025-01-01", "", ""), true},
		{p("a", "", "2026-01-01", ""), p("z", "2025-01-01", "", ""), true},
		{p("a", "", "", ""), p("z", "2025-01-01", "", ""), false},
		{p("b", "2026-01-01", "", ""), p("a", "2026-01-01", "", ""), true},
		{p("a", "2026-01-01", "", "z"), p("a", "2026-01-01", "", "a"), true},
	} {
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("newer(%+v,%+v)=%v", tc.a, tc.b, got)
		}
	}
}

func TestCatalogFailuresAreAtomicAndRuntimeIsolated(t *testing.T) {
	good := entry("mini", "mini", "2026-01-01")
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502); fmt.Fprint(w, "private diagnostics") }},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `<feed>broken`) }},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.TrimSuffix(feed(1, 0, 1, good), "</feed>"))
		}},
		{"tail", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(1, 0, 1, good)+`<broken`) }},
		{"extra-root", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(1, 0, 1, good)+`<extra/>`) }},
		{"oversized", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, feed(0, 0, 100)+strings.Repeat(" ", pageLimit))
		}},
		{"negative-counter", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(-1, 0, 100)) }},
		{"missing-counter", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.Replace(feed(0, 0, 100), "<startIndex>0</startIndex>", "", 1))
		}},
		{"empty-intermediate", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(1, 0, 100)) }},
		{"repeated-page", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(2, 0, 1, good)) }},
		{"repeated-content-with-new-offset", func(w http.ResponseWriter, r *http.Request) {
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			fmt.Fprint(w, feed(2, start, 1, good))
		}},
		{"changed-total", func(w http.ResponseWriter, r *http.Request) {
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			fmt.Fprint(w, feed(2+start, start, 1, good))
		}},
		{"bound-exhausted", func(w http.ResponseWriter, r *http.Request) {
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			fmt.Fprint(w, feed(11, start, 1, entry(strconv.Itoa(start), "mini", "2026-01-01")))
		}},
		{"entry-bound", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(1001, 0, 100, good)) }},
		{"wrong-namespace", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.Replace(feed(0, 0, 100), atomNS, "https://invalid.example", 1))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(t, tc.handler, nil)
			before := p.Status()
			packages, err := p.Catalog(context.Background())
			if err == nil || packages != nil {
				t.Fatalf("partial success: %+v %v", packages, err)
			}
			if before != p.Status() {
				t.Fatal("catalog failure mutated runtime")
			}
			if err := p.EnsureStarted(context.Background()); err != nil {
				t.Fatalf("empty-profile lifecycle after catalog failure: %v", err)
			}
			if err := p.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCatalogCancellationAndClientTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
	}{{"cancel", true}, {"timeout", false}} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }, &http.Client{Timeout: 50 * time.Millisecond})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() { <-started; cancel() }()
			}
			packages, err := p.Catalog(ctx)
			if packages != nil || err == nil {
				t.Fatalf("%+v %v", packages, err)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if !tc.cancel && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout: %v", err)
			}
		})
	}
}

func TestCatalogEmpty(t *testing.T) {
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, feed(0, 0, 100)) }, nil)
	packages, err := p.Catalog(context.Background())
	if err != nil || packages == nil || len(packages) != 0 {
		t.Fatalf("%+v %v", packages, err)
	}
}

func TestProviderFailureDiagnosticsOmitRemoteURLsAndBody(t *testing.T) {
	var logs bytes.Buffer
	secret := "https://user:password@example.invalid/?q=private-query&token=secret"
	p, e := New(Options{Logger: log.New(&logs, "", 0), HTTPClient: &http.Client{Transport: searchTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret + strings.Repeat("article-body", 10000))
	})}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Catalog(context.Background()); e == nil {
		t.Fatal("catalog failure passed")
	}
	if _, e = p.ResolveDownload(context.Background(), "private-provider-id"); e == nil {
		t.Fatal("acquisition failure passed")
	}
	text := logs.String()
	if !strings.Contains(text, "provider catalog failed:") || !strings.Contains(text, "provider acquisition failed:") {
		t.Fatalf("missing provider diagnostics %s", text)
	}
	for _, private := range []string{"password", "private-query", "token=", "article-body", "private-provider-id"} {
		if strings.Contains(text, private) {
			t.Fatalf("diagnostic leaked %q", private)
		}
	}
	if len(text) > 1024 {
		t.Fatalf("unbounded diagnostic %d", len(text))
	}
}
