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
		if source.Provider != "kiwix" || source.ProviderPackageID != "urn:uuid:selected" || source.Title != "Vikipedi" || source.Variant != "mini" || source.FileName != "selected.zim" || source.SourceURL != "http://mirror.example/payload.zim" || source.TotalBytes == nil || *source.TotalBytes != 7 || source.ExpectedSize == nil || *source.ExpectedSize != 7 || source.ContentType != "wikipedia" || source.Language != "tr" || source.Version != "2026-01-01" {
			t.Fatalf("source: %+v", source)
		}
		*source.TotalBytes = 8
		if *source.ExpectedSize != 7 {
			t.Fatal("transfer length aliases immutable expected size")
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
	if source.FileName != "selected.zim" || source.SourceURL != p.catalogURL.String()+"/selected.zim" || source.TotalBytes != nil || source.ExpectedSize != nil || source.ChecksumAlgorithm != "" || source.ChecksumValue != "" || calls != 1 {
		t.Fatalf("source=%+v calls=%d", source, calls)
	}
}

func TestResolveDownloadCapturesOnlySelectedFileChecksum(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		name, hashes, want string
		wantError          bool
	}{
		{name: "uppercase", hashes: `<hash type="sha-256">` + strings.ToUpper(digest) + `</hash>`, want: digest},
		{name: "algorithm alias", hashes: `<hash type="SHA256">` + digest + `</hash>`, want: digest},
		{name: "same supported digest", hashes: `<hash type="sha-256">` + digest + `</hash><hash type="sha256">` + digest + `</hash>`, want: digest},
		{name: "missing"},
		{name: "unsupported", hashes: `<hash type="sha-1">irrelevant</hash>`},
		{name: "short", hashes: `<hash type="sha-256">ab</hash>`, wantError: true},
		{name: "non hex", hashes: `<hash type="sha-256">` + strings.Repeat("g", 64) + `</hash>`, wantError: true},
		{name: "empty", hashes: `<hash type="sha-256"></hash>`, wantError: true},
		{name: "duplicate algorithm", hashes: `<hash type="sha-256" type="sha-1">` + digest + `</hash>`, wantError: true},
		{name: "conflicting", hashes: `<hash type="sha-256">` + digest + `</hash><hash type="sha256">` + strings.Repeat("cd", 32) + `</hash>`, wantError: true},
		{name: "malformed additional supported", hashes: `<hash type="sha-256">` + digest + `</hash><hash type="sha256">invalid</hash>`, wantError: true},
		{name: "foreign namespace", hashes: `<hash xmlns="urn:foreign" type="sha-256">` + digest + `</hash>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					fmt.Fprint(w, feed(1, 0, 100, acquisitionEntry("/selected.zim.meta4")))
					return
				}
				other := metalinkPayload("other.zim", "9", `<hash type="sha-256">malformed unrelated hash</hash><url>https://mirror.example/other.zim</url>`)
				fmt.Fprint(w, metalink(other+metalinkPayload("selected.zim", "7", tc.hashes+`<url>https://mirror.example/selected.zim</url>`)))
			}, nil)
			source, err := p.ResolveDownload(context.Background(), "urn:uuid:selected")
			if tc.wantError {
				if err == nil || source.SourceURL != "" {
					t.Fatalf("accepted source=%+v err=%v", source, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			algorithm := ""
			if tc.want != "" {
				algorithm = "sha256"
			}
			if source.ChecksumAlgorithm != algorithm || source.ChecksumValue != tc.want {
				t.Fatalf("checksum=%q:%q", source.ChecksumAlgorithm, source.ChecksumValue)
			}
		})
	}
}

func TestResolveDownloadDoesNotInventReleaseFromUpdatedDate(t *testing.T) {
	p := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		raw := strings.Replace(acquisitionEntry("/selected.zim"), "<dc:issued>2026-01-01</dc:issued>", "", 1)
		fmt.Fprint(w, feed(1, 0, 100, raw))
	}, nil)
	source, err := p.ResolveDownload(context.Background(), "urn:uuid:selected")
	if err != nil {
		t.Fatal(err)
	}
	if source.Version != "" {
		t.Fatalf("invented version: %q", source.Version)
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
