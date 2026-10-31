package kiwix

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var contentPackageID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// CheckContent proves this particular book is usable through one owned runtime
// generation. General homepage readiness is insufficient: Kiwix may ignore an
// invalid archive while still serving its other books successfully.
func (p *KiwixProvider) CheckContent(ctx context.Context, packageID string) error {
	return checkContent(ctx, packageID, p.runtime.ContentSnapshotTransport)
}

func checkContent(ctx context.Context, packageID string, capture func() (http.RoundTripper, *url.URL, []string, error)) error {
	if !contentPackageID.MatchString(packageID) {
		return fmt.Errorf("invalid candidate identity")
	}
	transport, target, paths, err := capture()
	if err != nil || transport == nil || target == nil {
		return fmt.Errorf("candidate runtime unavailable")
	}
	found := false
	for _, path := range paths {
		found = found || filepath.Base(path) == packageID+".zim"
	}
	if !found {
		return fmt.Errorf("candidate not loaded")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	prefix := "/content/content/" + packageID + "/"
	endpoint := *target
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = prefix, "", ""
	client := http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != target.Scheme || req.URL.Host != target.Host || req.URL.User != nil || req.URL.RawQuery != "" || !strings.HasPrefix(req.URL.Path, prefix) {
			return fmt.Errorf("candidate redirected outside its book")
		}
		// A redirect must remain a real candidate article, including after URL
		// decoding, rather than escaping into another book or the homepage.
		if _, err := searchArticle(req.URL.EscapedPath(), packageID); err != nil {
			return fmt.Errorf("unsafe candidate redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("candidate content request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("candidate content status %d", resp.StatusCode)
	}
	// Reading catches early body failures while bounding work for large pages.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return err
}
