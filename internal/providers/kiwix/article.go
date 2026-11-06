package kiwix

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"engelsiz/internal/providers"
)

// articleBodyLimit caps one owned article read before it reaches the
// plaintext stripper. Over-limit payloads truncate; they never grow the
// prompt budget because retrieval caps excerpts and evidence downstream.
const articleBodyLimit = 128 * 1024

var _ providers.ArticleReader = (*KiwixProvider)(nil)

// ReadArticle serves one validated article through the owned runtime
// transport. It mirrors searchOwned: capture the generation once, require
// the package ZIM loaded, and validate the href with searchArticle so only
// activated local routes are ever fetched. Redirects are disabled and only
// HTML payloads are returned.
func (p *KiwixProvider) ReadArticle(ctx context.Context, packageID, href string) (providers.Article, error) {
	return readArticleOwned(ctx, packageID, href, p.runtime.ContentSnapshotTransport)
}

func readArticleOwned(ctx context.Context, packageID, href string, capture func() (http.RoundTripper, *url.URL, []string, error)) (providers.Article, error) {
	if strings.TrimSpace(packageID) == "" {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	transport, target, paths, err := capture()
	if err != nil || transport == nil || target == nil {
		return providers.Article{}, providers.ErrRuntimeUnavailable
	}
	loaded := false
	for _, path := range paths {
		if filepath.Base(path) == packageID+".zim" {
			loaded = true
			break
		}
	}
	if !loaded {
		return providers.Article{}, providers.ErrContentUnavailable
	}
	articleID, err := searchArticle(href, packageID)
	if err != nil {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	endpoint := *target
	endpoint.Path = "/content/content/" + packageID + "/" + articleID
	endpoint.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ctype, "text/html") && !strings.HasPrefix(ctype, "application/xhtml+xml") {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, articleBodyLimit+1))
	if err != nil {
		return providers.Article{}, providers.ErrArticleUnavailable
	}
	if len(body) > articleBodyLimit {
		body = body[:articleBodyLimit]
	}
	return providers.Article{PackageID: packageID, Href: href, ContentType: resp.Header.Get("Content-Type"), Body: body}, nil
}
