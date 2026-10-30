package kiwix

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"engelsiz/internal/providers"
)

const searchBodyLimit = 1 << 20

var _ providers.SearchProvider = (*KiwixProvider)(nil)

func (p *KiwixProvider) Search(ctx context.Context, packageID, query string) (providers.SearchResponse, error) {
	return searchOwned(ctx, packageID, query, p.runtime.ContentSnapshotTransport)
}

// Capture once: neither membership nor a request may migrate to a replacement
// generation after the runtime's mutex has been released.
func searchOwned(ctx context.Context, packageID, query string, capture func() (http.RoundTripper, *url.URL, []string, error)) (providers.SearchResponse, error) {
	result := providers.SearchResponse{Query: query, PackageID: packageID, Results: []providers.SearchResult{}}
	transport, target, paths, err := capture()
	if err != nil || transport == nil || target == nil {
		return result, providers.ErrRuntimeUnavailable
	}
	loaded := false
	for _, path := range paths {
		if filepath.Base(path) == packageID+".zim" {
			loaded = true
			break
		}
	}
	if !loaded {
		return result, providers.ErrContentUnavailable
	}
	if strings.TrimSpace(query) == "" {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	endpoint := *target
	endpoint.Path = "/content/search"
	endpoint.RawPath = ""
	endpoint.RawQuery = url.Values{"content": {packageID}, "pattern": {query}, "format": {"xml"}, "start": {"0"}, "pageLength": {"25"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return result, providers.ErrSearchUnavailable
	}
	req.Header.Set("Accept", "application/rss+xml")
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return result, searchFailure(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, providers.ErrSearchUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, searchBodyLimit+1))
	if err != nil {
		return result, searchFailure(ctx, err)
	}
	if len(body) > searchBodyLimit {
		return result, providers.ErrSearchUnavailable
	}
	total, rows, err := decodeSearch(body, packageID)
	if err != nil {
		return result, providers.ErrSearchUnavailable
	}
	result.TotalResults, result.Results = total, rows
	return result, nil
}

func searchFailure(ctx context.Context, err error) error {
	var timeout net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return context.DeadlineExceeded
	}
	return providers.ErrSearchUnavailable
}

// XML character data is recursively collected in document order. Highlighting
// elements never become HTML, and their text is not lost as with flat chardata.
type searchText string

func (s *searchText) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var b strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			b.Write(tok)
		}
	}
	*s = searchText(b.String())
	return nil
}

type searchRSS struct {
	XMLName  xml.Name `xml:"rss"`
	Version  string   `xml:"version,attr"`
	Channels []struct {
		XMLName xml.Name
		Counts  []string `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
		Items   []struct {
			Title       searchText `xml:"title"`
			Link        string     `xml:"link"`
			Description searchText `xml:"description"`
			Book        struct {
				Title searchText `xml:"title"`
			} `xml:"book"`
		} `xml:"item"`
	} `xml:"channel"`
}

func decodeSearch(body []byte, packageID string) (int, []providers.SearchResult, error) {
	var rss searchRSS
	d := xml.NewDecoder(bytes.NewReader(body))
	if err := d.Decode(&rss); err != nil {
		return 0, nil, err
	}
	// Decoder.Decode permits trailing XML; a response must contain one document.
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, nil, err
		}
		if data, ok := tok.(xml.CharData); !ok || strings.TrimSpace(string(data)) != "" {
			return 0, nil, providers.ErrSearchUnavailable
		}
	}
	if rss.XMLName.Space != "" || rss.Version != "2.0" || len(rss.Channels) != 1 || len(rss.Channels[0].Counts) != 1 || rss.Channels[0].XMLName.Space != "" {
		return 0, nil, providers.ErrSearchUnavailable
	}
	channel := rss.Channels[0]
	count := strings.TrimSpace(channel.Counts[0])
	if count == "" || strings.IndexFunc(count, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, nil, providers.ErrSearchUnavailable
	}
	total, err := strconv.Atoi(count)
	if err != nil || total < len(channel.Items) {
		return 0, nil, providers.ErrSearchUnavailable
	}
	rows := make([]providers.SearchResult, 0, min(25, len(channel.Items)))
	for _, item := range channel.Items {
		articleID, err := searchArticle(item.Link, packageID)
		if err != nil {
			return 0, nil, err
		}
		if len(rows) == 25 {
			continue
		}
		title := boundedSearchText(string(item.Title), 1000)
		if title == "" {
			title = boundedSearchText(articleID, 1000)
		}
		rows = append(rows, providers.SearchResult{Title: title, Snippet: boundedSearchText(string(item.Description), 1000), Source: boundedSearchText(string(item.Book.Title), 1000), ArticleID: articleID, Href: item.Link})
	}
	return total, rows, nil
}

func boundedSearchText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func searchArticle(href, packageID string) (string, error) {
	u, err := url.Parse(href)
	prefix := "/content/content/" + packageID + "/"
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(href, "?#") || !strings.HasPrefix(href, prefix) || !strings.HasPrefix(u.Path, prefix) || len(u.Path) == len(prefix) || !utf8.ValidString(u.Path) {
		return "", providers.ErrSearchUnavailable
	}
	path := href
	for i := 0; i < 8; i++ {
		if !strings.HasPrefix(path, prefix) || strings.Contains(path, "//") || strings.ContainsRune(path, '\\') || strings.IndexFunc(path, unicode.IsControl) >= 0 || !utf8.ValidString(path) {
			return "", providers.ErrSearchUnavailable
		}
		for _, part := range strings.Split(path, "/") {
			if part == "." || part == ".." {
				return "", providers.ErrSearchUnavailable
			}
		}
		// Decode valid escapes individually: literal percent names cannot hide an
		// unsafe escape in another part of the path at a later decoding layer.
		var next strings.Builder
		for offset := 0; offset < len(path); offset++ {
			if path[offset] == '%' && offset+2 < len(path) {
				if value, e := url.PathUnescape(path[offset : offset+3]); e == nil {
					next.WriteString(value)
					offset += 2
					continue
				}
			}
			next.WriteByte(path[offset])
		}
		decoded := next.String()
		if decoded == path {
			return strings.TrimPrefix(u.Path, prefix), nil
		}
		path = decoded
	}
	return "", providers.ErrSearchUnavailable
}
