package kiwix

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"engelsiz/internal/providers"
)

const (
	metalinkNS    = "urn:ietf:params:xml:ns:metalink"
	metalinkLimit = 1024 * 1024
)

var _ providers.AcquisitionResolver = (*KiwixProvider)(nil)

// ResolveDownload refetches the catalog so callers only select a package ID;
// URLs, filenames and exact sizes never come from the browser.
func (p *KiwixProvider) ResolveDownload(ctx context.Context, packageID string) (providers.DownloadSource, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	packages, err := p.Catalog(ctx)
	if err != nil {
		return providers.DownloadSource{}, err
	}
	var selected *providers.CatalogPackage
	for i := range packages {
		if packages[i].ProviderPackageID == packageID {
			if selected != nil {
				return providers.DownloadSource{}, fmt.Errorf("ambiguous catalog package")
			}
			selected = &packages[i]
		}
	}
	if selected == nil {
		return providers.DownloadSource{}, fmt.Errorf("catalog package not found")
	}
	u, err := url.Parse(selected.Download.URL)
	if err != nil || !safeDownloadURL(u) {
		return providers.DownloadSource{}, fmt.Errorf("unsafe acquisition URL")
	}
	name := path.Base(u.Path)
	if selected.Download.Kind == "metalink" {
		name = name[:len(name)-len(".meta4")]
	}
	if !safeDownloadName(name) || !strings.HasSuffix(strings.ToLower(name), ".zim") || strings.HasSuffix(u.Path, "/") {
		return providers.DownloadSource{}, fmt.Errorf("unsafe acquisition filename")
	}
	source := providers.DownloadSource{Provider: selected.Provider, ProviderPackageID: selected.ProviderPackageID, Title: selected.Title, Variant: selected.Variant, FileName: name}
	switch selected.Download.Kind {
	case "zim":
		// Catalog length is approximate. The streaming response establishes the
		// direct file's exact length, without requiring a successful HEAD.
		source.SourceURL = u.String()
		return source, nil
	case "metalink":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return providers.DownloadSource{}, fmt.Errorf("metalink request: %w", err)
		}
		req.Header.Set("Accept", "application/metalink4+xml")
		resp, err := p.client.Do(req)
		if err != nil {
			return providers.DownloadSource{}, fmt.Errorf("metalink fetch: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return providers.DownloadSource{}, fmt.Errorf("metalink HTTP status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, metalinkLimit+1))
		if err != nil {
			return providers.DownloadSource{}, fmt.Errorf("metalink body: %w", err)
		}
		if len(body) > metalinkLimit {
			return providers.DownloadSource{}, fmt.Errorf("metalink exceeds byte limit")
		}
		resolvedURL, size, err := decodeMetalink(ctx, body, name)
		if err != nil {
			return providers.DownloadSource{}, err
		}
		source.SourceURL, source.TotalBytes = resolvedURL, &size
		return source, nil
	default:
		return providers.DownloadSource{}, fmt.Errorf("unsupported acquisition kind")
	}
}

type metalinkFile struct {
	Sizes []string `xml:"urn:ietf:params:xml:ns:metalink size"`
	URLs  []string `xml:"urn:ietf:params:xml:ns:metalink url"`
}

func decodeMetalink(ctx context.Context, body []byte, expected string) (string, int64, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	opened, closed, matched := false, false, false
	var source string
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if !closed || !matched {
				return "", 0, fmt.Errorf("metalink missing expected file")
			}
			return source, size, nil
		}
		if err != nil {
			return "", 0, fmt.Errorf("metalink XML: %w", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			if !opened {
				if token.Name.Space != metalinkNS || token.Name.Local != "metalink" {
					return "", 0, fmt.Errorf("expected Metalink document")
				}
				opened = true
				continue
			}
			if closed {
				return "", 0, fmt.Errorf("multiple Metalink roots")
			}
			if token.Name.Space != metalinkNS || token.Name.Local != "file" {
				if err := decoder.Skip(); err != nil {
					return "", 0, fmt.Errorf("metalink XML: %w", err)
				}
				continue
			}
			name, count := "", 0
			for _, attr := range token.Attr {
				if attr.Name.Space == "" && attr.Name.Local == "name" {
					name, count = attr.Value, count+1
				}
			}
			if count != 1 || !safeDownloadName(name) {
				return "", 0, fmt.Errorf("unsafe Metalink filename")
			}
			var file metalinkFile
			if err := decoder.DecodeElement(&file, &token); err != nil {
				return "", 0, fmt.Errorf("metalink XML: %w", err)
			}
			if name != expected {
				continue
			}
			if matched {
				return "", 0, fmt.Errorf("ambiguous Metalink file")
			}
			matched = true
			if len(file.Sizes) != 1 {
				return "", 0, fmt.Errorf("metalink requires one exact size")
			}
			size, err = strconv.ParseInt(strings.TrimSpace(file.Sizes[0]), 10, 64)
			if err != nil || size <= 0 {
				return "", 0, fmt.Errorf("invalid Metalink size")
			}
			for _, raw := range file.URLs {
				u, err := url.Parse(strings.TrimSpace(raw))
				if err == nil && safeDownloadURL(u) {
					source = u.String()
					break
				}
			}
			if source == "" {
				return "", 0, fmt.Errorf("metalink has no safe transfer URL")
			}
		case xml.EndElement:
			if token.Name.Space == metalinkNS && token.Name.Local == "metalink" {
				closed = true
			}
		case xml.CharData:
			if (!opened || closed) && strings.TrimSpace(string(token)) != "" {
				return "", 0, fmt.Errorf("text outside Metalink document")
			}
		case xml.Directive:
			return "", 0, fmt.Errorf("unsupported Metalink directive")
		}
	}
}

func safeDownloadURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.Opaque == ""
}

// Apply Windows restrictions on every OS so metadata cannot become dangerous
// when the same profile is used on Windows later.
func safeDownloadName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `/\<>:"|?*`) {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" {
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}
