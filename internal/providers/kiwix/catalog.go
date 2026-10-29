package kiwix

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	atomNS       = "http://www.w3.org/2005/Atom"
	dcNS         = "http://purl.org/dc/terms/"
	openSearchNS = "http://a9.com/-/spec/opensearch/1.1/"
	pageLimit    = 2 * 1024 * 1024
	maxPages     = 10
	maxEntries   = 1000
)

type catalogEntry struct {
	ID       string        `xml:"http://www.w3.org/2005/Atom id"`
	Title    string        `xml:"http://www.w3.org/2005/Atom title"`
	Summary  string        `xml:"http://www.w3.org/2005/Atom summary"`
	Language string        `xml:"http://www.w3.org/2005/Atom language"`
	Name     string        `xml:"http://www.w3.org/2005/Atom name"`
	Flavour  string        `xml:"http://www.w3.org/2005/Atom flavour"`
	Category string        `xml:"http://www.w3.org/2005/Atom category"`
	Updated  string        `xml:"http://www.w3.org/2005/Atom updated"`
	Issued   string        `xml:"http://purl.org/dc/terms/ issued"`
	Links    []catalogLink `xml:"http://www.w3.org/2005/Atom link"`
}

type catalogLink struct {
	Rel    string `xml:"rel,attr"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Length string `xml:"length,attr"`
}

type catalogPage struct {
	total, start, perPage int
	entries               []catalogEntry
}

// Catalog never starts, stops or mutates the content runtime. A traversal is
// all-or-nothing, including when an upstream page becomes inconsistent.
func (p *KiwixProvider) Catalog(ctx context.Context) ([]providers.CatalogPackage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	selected := make(map[string]providers.CatalogPackage)
	seenPages := make(map[[32]byte]bool)
	offset, total := 0, -1
	for pageNumber := 0; pageNumber < maxPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u := *p.catalogURL
		query := u.Query()
		query.Set("lang", "tur")
		query.Set("q", "wikipedia")
		query.Set("count", "100")
		query.Set("start", strconv.Itoa(offset))
		u.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("catalog request: %w", err)
		}
		req.Header.Set("Accept", "application/atom+xml")
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("catalog fetch: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, pageLimit+1))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("catalog HTTP status %d", resp.StatusCode)
		}
		if readErr != nil {
			return nil, fmt.Errorf("catalog body: %w", readErr)
		}
		if len(body) > pageLimit {
			return nil, fmt.Errorf("catalog page exceeds byte limit")
		}
		page, err := decodeCatalog(ctx, body)
		if err != nil {
			return nil, fmt.Errorf("catalog XML: %w", err)
		}
		if page.start != offset || page.total < 0 || page.total > maxEntries || page.perPage < 0 || page.perPage > 100 || len(page.entries) > page.perPage || page.start+len(page.entries) > page.total || (total >= 0 && page.total != total) {
			return nil, fmt.Errorf("catalog inconsistent pagination")
		}
		// Compare page content independently of pagination counters: a broken
		// server can repeat entries while echoing a new requested offset.
		content, err := xml.Marshal(page.entries)
		if err != nil {
			return nil, fmt.Errorf("catalog page fingerprint: %w", err)
		}
		digest := sha256.Sum256(content)
		if seenPages[digest] {
			return nil, fmt.Errorf("catalog repeated page")
		}
		seenPages[digest] = true
		total = page.total
		for _, entry := range page.entries {
			pkg, ok := normalizeEntry(entry, resp.Request.URL)
			if !ok {
				continue
			}
			previous, exists := selected[pkg.Variant]
			if !exists || newer(pkg, previous) {
				selected[pkg.Variant] = pkg
			}
		}
		offset += len(page.entries)
		if offset == total {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			packages := make([]providers.CatalogPackage, 0, 3)
			for _, variant := range []string{"mini", "nopic", "maxi"} {
				if pkg, ok := selected[variant]; ok {
					packages = append(packages, pkg)
				}
			}
			return packages, nil
		}
		if len(page.entries) == 0 {
			return nil, fmt.Errorf("catalog pagination made no progress")
		}
	}
	return nil, fmt.Errorf("catalog page limit exhausted")
}

// Decode the complete document, including its tail. encoding/xml otherwise
// accepts a valid first root followed by malformed or extra XML.
func decodeCatalog(ctx context.Context, body []byte) (catalogPage, error) {
	page := catalogPage{total: -1, start: -1, perPage: -1}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	opened, closed := false, false
	for {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			if !closed {
				return page, io.ErrUnexpectedEOF
			}
			return page, nil
		}
		if err != nil {
			return page, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if !opened {
				if token.Name.Space != atomNS || token.Name.Local != "feed" {
					return page, fmt.Errorf("expected Atom feed")
				}
				opened = true
				continue
			}
			if closed {
				return page, fmt.Errorf("multiple XML roots")
			}
			if token.Name.Space == atomNS && token.Name.Local == "entry" {
				var entry catalogEntry
				if err := decoder.DecodeElement(&entry, &token); err != nil {
					return page, err
				}
				page.entries = append(page.entries, entry)
				if len(page.entries) > maxEntries {
					return page, fmt.Errorf("catalog entry limit")
				}
			} else if (token.Name.Space == atomNS || token.Name.Space == openSearchNS) && (token.Name.Local == "totalResults" || token.Name.Local == "startIndex" || token.Name.Local == "itemsPerPage") {
				var value string
				if err := decoder.DecodeElement(&value, &token); err != nil {
					return page, err
				}
				count, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil || count < 0 {
					return page, fmt.Errorf("invalid pagination counter")
				}
				target := &page.total
				if token.Name.Local == "startIndex" {
					target = &page.start
				} else if token.Name.Local == "itemsPerPage" {
					target = &page.perPage
				}
				if *target != -1 {
					return page, fmt.Errorf("duplicate pagination counter")
				}
				*target = count
			} else if err := decoder.Skip(); err != nil {
				return page, err
			}
		case xml.EndElement:
			if token.Name.Space == atomNS && token.Name.Local == "feed" {
				closed = true
			}
		case xml.CharData:
			if (!opened || closed) && strings.TrimSpace(string(token)) != "" {
				return page, fmt.Errorf("text outside Atom feed")
			}
		}
	}
}

func normalizeEntry(e catalogEntry, base *url.URL) (providers.CatalogPackage, bool) {
	if e.Language != "tur" || e.Name != "wikipedia_tr_all" || e.Category != "wikipedia" || (e.Flavour != "mini" && e.Flavour != "nopic" && e.Flavour != "maxi") || strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Title) == "" {
		return providers.CatalogPackage{}, false
	}
	for _, link := range e.Links {
		if link.Rel != "http://opds-spec.org/acquisition/open-access" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(link.Href))
		if err != nil || link.Href == "" {
			continue
		}
		u = base.ResolveReference(u)
		if (u.Scheme != "https" && !(base.Scheme == "http" && u.Scheme == "http" && u.Host == base.Host)) || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			continue
		}
		kind := ""
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".meta4":
			kind = "metalink"
		case ".zim":
			kind = "zim"
		}
		if kind == "" {
			continue
		}
		var size *int64
		if n, err := strconv.ParseInt(link.Length, 10, 64); err == nil && n > 0 {
			size = &n
		}
		return providers.CatalogPackage{Provider: "kiwix", ProviderPackageID: strings.TrimSpace(e.ID), Title: strings.TrimSpace(e.Title), Language: "tr", Variant: e.Flavour, Description: strings.TrimSpace(e.Summary), PublishedAt: catalogDate(e.Issued), UpdatedAt: catalogDate(e.Updated), SizeBytes: size, Download: providers.Acquisition{Kind: kind, URL: u.String(), MediaType: link.Type}}, true
	}
	return providers.CatalogPackage{}, false
}

func catalogDate(value string) *time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return &parsed
		}
	}
	return nil
}

func newer(a, b providers.CatalogPackage) bool {
	date := func(p providers.CatalogPackage) *time.Time {
		if p.PublishedAt != nil {
			return p.PublishedAt
		}
		return p.UpdatedAt
	}
	ad, bd := date(a), date(b)
	if ad != nil && bd == nil {
		return true
	}
	if ad == nil && bd != nil {
		return false
	}
	if ad != nil && !ad.Equal(*bd) {
		return ad.After(*bd)
	}
	if a.ProviderPackageID != b.ProviderPackageID {
		return a.ProviderPackageID > b.ProviderPackageID
	}
	return a.Download.URL > b.Download.URL
}
