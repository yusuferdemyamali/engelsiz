package kiwix

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"engelsiz/internal/providers"
)

const searchPackage = "0123456789abcdef0123456789abcdef"
const searchPrefix = "/content/content/" + searchPackage + "/"

type searchTripFunc func(*http.Request) (*http.Response, error)

func (f searchTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func searchCapture(f searchTripFunc) func() (http.RoundTripper, *url.URL, []string, error) {
	return func() (http.RoundTripper, *url.URL, []string, error) {
		return f, &url.URL{Scheme: "http", Host: "127.0.0.1:12345"}, []string{"/private/" + searchPackage + ".zim"}, nil
	}
}
func searchResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func searchFeed(count string, items ...string) string {
	return `<rss version="2.0" xmlns:o="http://a9.com/-/spec/opensearch/1.1/"><channel><o:totalResults>` + count + `</o:totalResults>` + strings.Join(items, "") + `</channel></rss>`
}
func searchItem(href string) string {
	return `<item><title>Result</title><link>` + href + `</link></item>`
}

func TestSearchCapturedRealRSSAndFixedRequest(t *testing.T) {
	// Verbatim RSS from official kiwix-tools 3.8.2, renamed indexed example.zim,
	// captured 2026-10-30. The production-shaped basename is deliberately retained.
	body, err := os.ReadFile("testdata/search-kiwix-3.8.2.xml")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	t.Setenv("HTTP_PROXY", "http://foreign.invalid:8888")
	result, err := searchOwned(t.Context(), searchPackage, "wiki", searchCapture(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "http" || r.URL.Host != "127.0.0.1:12345" || r.URL.Path != "/content/search" || r.Method != "GET" || r.URL.RawQuery != "content="+searchPackage+"&format=xml&pageLength=25&pattern=wiki&start=0" {
			t.Fatalf("request escaped bound target: %v", r)
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("missing bounded deadline")
		}
		return searchResponse(string(body)), nil
	}))
	if err != nil || calls != 1 || result.Query != "wiki" || result.PackageID != searchPackage || result.TotalResults != 2 || len(result.Results) != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	first := result.Results[0]
	if !strings.HasPrefix(first.Snippet, "...wiki! See Wikibooks:") || strings.Contains(first.Snippet, "<b>") || first.Source != "Wikibooks" || !strings.Contains(first.ArticleID, "FreedomBox for Communities") || !strings.Contains(first.Href, "%20") {
		t.Fatalf("lost mixed text/escaped link: %+v", first)
	}
}

func TestSearchAvailabilityPrecedesBlankAndNoUpstream(t *testing.T) {
	for _, tc := range []struct {
		name             string
		paths            []string
		captureErr, want error
	}{
		{"runtime first", nil, errors.New("private path"), providers.ErrRuntimeUnavailable},
		{"not loaded", []string{"/private/not-" + searchPackage + ".zim"}, nil, providers.ErrContentUnavailable},
		{"exact basename", []string{"/private/" + searchPackage + ".zim.bak"}, nil, providers.ErrContentUnavailable},
		{"ready blank", []string{"/private/" + searchPackage + ".zim"}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captures := 0
			result, err := searchOwned(t.Context(), searchPackage, "", func() (http.RoundTripper, *url.URL, []string, error) {
				captures++
				return searchTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("blank queried upstream"); return nil, nil }), &url.URL{Scheme: "http", Host: "127.0.0.1:12345"}, tc.paths, tc.captureErr
			})
			if !errors.Is(err, tc.want) || captures != 1 || result.Results == nil || len(result.Results) != 0 {
				t.Fatalf("result=%+v err=%v captures=%d", result, err, captures)
			}
		})
	}
}

func TestSearchAdversarialArticleLinks(t *testing.T) {
	for _, suffix := range []string{"../escape", "%2e%2e/escape", "%252e%252e/escape", "a%2f..%2fescape", "a//b", "a%5cb", "a%255cb", "a%00b", "a%09b", "%25zz/%252e%252e/escape", "%25zz/%255csecret", "%25zz/%252f%252fevil", "a?x=1", "a#fragment", "a?", "a#", "%FF", "%25FF", "%zz", ""} {
		t.Run(suffix, func(t *testing.T) {
			if _, _, err := decodeSearch([]byte(searchFeed("1", searchItem(searchPrefix+suffix))), searchPackage); err == nil {
				t.Fatal("unsafe link accepted")
			}
		})
	}
	for _, href := range []string{"https://evil.invalid" + searchPrefix + "a", "//evil.invalid" + searchPrefix + "a", "/content/content/other/a", "/content/content/" + searchPackage + "-other/a", "/content/skin/a", "content/content/" + searchPackage + "/a"} {
		if _, _, err := decodeSearch([]byte(searchFeed("1", searchItem(href))), searchPackage); err == nil {
			t.Fatalf("unsafe target accepted: %s", href)
		}
	}
	for _, suffix := range []string{"Caf%C3%A9%20du%20monde", "100%25done", "%25zz/Article%20name", "A/Article.html", "Türkçe.html"} {
		href := searchPrefix + suffix
		_, rows, err := decodeSearch([]byte(searchFeed("1", searchItem(href))), searchPackage)
		if err != nil || len(rows) != 1 || rows[0].Href != href {
			t.Fatalf("valid href changed: %s %+v %v", href, rows, err)
		}
	}
}

func TestSearchRejectsMalformedPayloads(t *testing.T) {
	valid := searchFeed("0")
	for _, body := range []string{"", `<html><body>not RSS</body></html>`, `<rss version="1.0"><channel/></rss>`, strings.Replace(valid, `<channel>`, `<channel xmlns="foreign">`, 1), strings.ReplaceAll(valid, "o:totalResults", "totalResults"), searchFeed("-1"), searchFeed("1.0"), searchFeed("+1"), searchFeed("999999999999999999999999999999"), searchFeed(""), searchFeed("0", searchItem(searchPrefix+"a")), strings.Replace(valid, "</channel>", "<o:totalResults>0</o:totalResults></channel>", 1), valid + `<rss/>`, valid + "junk", strings.Replace(valid, "</rss>", "<channel/></rss>", 1), strings.Replace(valid, "</channel>", "<item></channel>", 1)} {
		if _, _, err := decodeSearch([]byte(body), searchPackage); err == nil {
			t.Fatalf("malformed payload accepted: %s", body)
		}
	}
}

func TestSearchBoundsAndMissingOptionalText(t *testing.T) {
	title := strings.Repeat("İ", 1100)
	item := `<item><title>` + title + `</title><link>` + searchPrefix + `a</link><description> start <b>highlight <i>nested</i></b> end ` + title + `</description><book><title>` + title + `</title></book></item>`
	total, rows, err := decodeSearch([]byte(searchFeed("100", strings.Repeat(item, 30))), searchPackage)
	if err != nil || total != 100 || len(rows) != 25 {
		t.Fatalf("rows=%d total=%d err=%v", len(rows), total, err)
	}
	for _, s := range []string{rows[0].Title, rows[0].Snippet, rows[0].Source} {
		if utf8.RuneCountInString(s) != 1000 || !utf8.ValidString(s) {
			t.Fatal("unbounded or broken unicode text")
		}
	}
	if !strings.HasPrefix(rows[0].Snippet, "start highlight nested end ") {
		t.Fatalf("mixed text=%q", rows[0].Snippet)
	}
	_, rows, err = decodeSearch([]byte(searchFeed("1", `<item><link>`+searchPrefix+`Caf%C3%A9</link></item>`)), searchPackage)
	if err != nil || rows[0].Title != "Café" || rows[0].Snippet != "" || rows[0].Source != "" {
		t.Fatalf("missing fields=%+v err=%v", rows, err)
	}
}

func TestSearchHTTPFailuresAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name string
		trip searchTripFunc
		want error
	}{
		{"redirect", func(*http.Request) (*http.Response, error) {
			r := searchResponse("secret")
			r.StatusCode = 302
			r.Header.Set("Location", "http://foreign.invalid/")
			return r, nil
		}, providers.ErrSearchUnavailable},
		{"status", func(*http.Request) (*http.Response, error) {
			r := searchResponse("secret")
			r.StatusCode = 503
			return r, nil
		}, providers.ErrSearchUnavailable},
		{"size", func(*http.Request) (*http.Response, error) {
			return searchResponse(strings.Repeat("x", searchBodyLimit+1)), nil
		}, providers.ErrSearchUnavailable},
		{"format", func(*http.Request) (*http.Response, error) { return searchResponse("<html/>"), nil }, providers.ErrSearchUnavailable},
		{"transport", func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("private URL failed") }, providers.ErrSearchUnavailable},
		{"header timeout", func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{Err: "header deadline", IsTimeout: true}
		}, context.DeadlineExceeded},
		{"deadline", func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			_, err := searchOwned(ctx, searchPackage, "a & Türkçe?", searchCapture(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Query().Get("pattern") != "a & Türkçe?" {
					t.Fatal("query altered")
				}
				return tc.trip(r)
			}))
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestSearchCancellationReachesCapturedTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := searchOwned(ctx, searchPackage, "wiki", searchCapture(func(r *http.Request) (*http.Response, error) {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}))
		finished <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, providers.ErrSearchUnavailable) {
			t.Fatalf("cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel the captured upstream transport")
	}
}
