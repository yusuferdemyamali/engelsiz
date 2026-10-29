package kiwix

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func acquisitionEntry(href string) string {
	return strings.Replace(entry("selected", "mini", "2026-01-01"), "https://example.org/selected.zim.meta4", href, 1)
}

func metalink(files string) string {
	return `<metalink xmlns="` + metalinkNS + `">` + files + `</metalink>`
}

func metalinkPayload(name, size, urls string) string {
	return `<file name="` + name + `"><size>` + size + `</size>` + urls + `</file>`
}

func TestResolveDownloadMetalinkUsesTrustedCatalogAndFirstSafeMirror(t *testing.T) {
	catalogCalls, metadataCalls, payloadCalls := 0, 0, 0
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			catalogCalls++
			fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry("/selected.zim.meta4")))
		case "/selected.zim.meta4":
			metadataCalls++
			if r.Header.Get("Accept") != "application/metalink4+xml" {
				t.Error("missing Metalink accept header")
			}
			fmt.Fprint(w, metalink(metalinkPayload("selected.zim", "7", `<url>ftp://invalid/file.zim</url><url>https://user:pass@example.org/file.zim</url><url>http://mirror.example/payload.zim</url><url>https://second.example/payload.zim</url>`)))
		default:
			payloadCalls++
		}
	}, nil)
	for range 2 {
		source, err := p.ResolveDownload(context.Background(), "urn:uuid:selected")
		if err != nil {
			t.Fatal(err)
		}
		if source.Provider != "kiwix" || source.ProviderPackageID != "urn:uuid:selected" || source.Title != "Vikipedi" || source.Variant != "mini" || source.FileName != "selected.zim" || source.SourceURL != "http://mirror.example/payload.zim" || source.TotalBytes == nil || *source.TotalBytes != 7 {
			t.Fatalf("source: %+v", source)
		}
	}
	if catalogCalls != 2 || metadataCalls != 2 || payloadCalls != 0 {
		t.Fatalf("catalog=%d metadata=%d payload=%d", catalogCalls, metadataCalls, payloadCalls)
	}
}

func TestResolveDownloadDirectFileIgnoresApproximateCatalogSize(t *testing.T) {
	calls := 0
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry("/selected.zim")))
	}, nil)
	source, err := p.ResolveDownload(context.Background(), "urn:uuid:selected")
	if err != nil {
		t.Fatal(err)
	}
	if source.FileName != "selected.zim" || source.SourceURL != p.catalogURL.String()+"/selected.zim" || source.TotalBytes != nil || calls != 1 {
		t.Fatalf("source=%+v calls=%d", source, calls)
	}
}

func TestResolveDownloadRejectsBadMetadata(t *testing.T) {
	valid := metalinkPayload("selected.zim", "7", `<url>https://mirror.example/payload.zim</url>`)
	cases := map[string]string{
		"malformed":           `<metalink`,
		"truncated":           strings.TrimSuffix(metalink(valid), `</metalink>`),
		"trailing junk":       metalink(valid) + `junk`,
		"trailing broken XML": metalink(valid) + `<broken`,
		"multiple roots":      metalink(valid) + metalink(valid),
		"wrong namespace":     strings.Replace(metalink(valid), metalinkNS, "https://invalid.example", 1),
		"missing file":        metalink(""),
		"wrong filename":      metalink(strings.Replace(valid, "selected.zim", "other.zim", 1)),
		"ambiguous file":      metalink(valid + valid),
		"missing name":        metalink(`<file><size>7</size><url>https://mirror.example/file.zim</url></file>`),
		"duplicate name":      metalink(strings.Replace(valid, `name="selected.zim"`, `name="selected.zim" name="selected.zim"`, 1)),
		"missing size":        metalink(strings.Replace(valid, "<size>7</size>", "", 1)),
		"duplicate size":      metalink(strings.Replace(valid, "<size>7</size>", "<size>7</size><size>8</size>", 1)),
		"zero size":           metalink(strings.Replace(valid, "<size>7</size>", "<size>0</size>", 1)),
		"negative size":       metalink(strings.Replace(valid, "<size>7</size>", "<size>-7</size>", 1)),
		"invalid size":        metalink(strings.Replace(valid, "<size>7</size>", "<size>abc</size>", 1)),
		"overflow size":       metalink(strings.Replace(valid, "<size>7</size>", "<size>9223372036854775808</size>", 1)),
		"no safe mirror":      metalink(strings.Replace(valid, "https://mirror.example/payload.zim", "file:///tmp/file.zim", 1)),
		"relative mirror":     metalink(strings.Replace(valid, "https://mirror.example/payload.zim", "/file.zim", 1)),
		"fragment mirror":     metalink(strings.Replace(valid, "https://mirror.example/payload.zim", "https://mirror.example/file.zim#fragment", 1)),
		"oversized":           metalink(valid) + strings.Repeat(" ", metalinkLimit),
	}
	for _, name := range []string{"../selected.zim", `..\selected.zim`, "/selected.zim", `C:\selected.zim`, "CON.zim", "selected.zim.", "selected.zim ", "bad:name.zim"} {
		cases["unsafe filename "+name] = metalink(strings.Replace(valid, "selected.zim", name, 1))
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry("/selected.zim.meta4")))
					return
				}
				fmt.Fprint(w, body)
			}, nil)
			if source, err := p.ResolveDownload(context.Background(), "urn:uuid:selected"); err == nil || source.SourceURL != "" {
				t.Fatalf("accepted source=%+v err=%v", source, err)
			}
		})
	}
}

func TestResolveDownloadMissingSelectionAndUnsafeDirectName(t *testing.T) {
	for _, tc := range []struct{ name, href, id string }{
		{"missing selection", "/selected.zim", "urn:uuid:missing"},
		{"windows path", "/bad%5Cname.zim", "urn:uuid:selected"},
		{"reserved name", "/CON.zim", "urn:uuid:selected"},
		{"windows drive", "/C:selected.zim", "urn:uuid:selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry(tc.href)))
			}, nil)
			if _, err := p.ResolveDownload(context.Background(), tc.id); err == nil {
				t.Fatal("accepted invalid selection")
			}
		})
	}
}

func TestResolveDownloadMetadataStatusRedirectAndTimeout(t *testing.T) {
	for _, behavior := range []string{"status", "redirect", "timeout"} {
		t.Run(behavior, func(t *testing.T) {
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry("/selected.zim.meta4")))
					return
				}
				switch behavior {
				case "status":
					w.WriteHeader(http.StatusBadGateway)
				case "redirect":
					http.Redirect(w, r, "/loop", http.StatusFound)
				case "timeout":
					<-r.Context().Done()
				}
			}, &http.Client{Timeout: 50 * time.Millisecond})
			_, err := p.ResolveDownload(context.Background(), "urn:uuid:selected")
			if err == nil {
				t.Fatal("expected metadata error")
			}
			if behavior == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout: %v", err)
			}
		})
	}
}
