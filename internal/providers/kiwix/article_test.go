package kiwix

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"engelsiz/internal/providers"
)

func articleCapture(trip func(*http.Request) (*http.Response, error), paths []string, err error) func() (http.RoundTripper, *url.URL, []string, error) {
	return func() (http.RoundTripper, *url.URL, []string, error) {
		if err != nil {
			return nil, nil, nil, err
		}
		return searchTripFunc(trip), &url.URL{Scheme: "http", Host: "127.0.0.1:12345"}, paths, nil
	}
}

func articleHTML(body string, contentType string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestReadArticleHappyPath(t *testing.T) {
	calls := 0
	t.Setenv("HTTP_PROXY", "http://foreign.invalid:8888")
	art, err := readArticleOwned(t.Context(), searchPackage, searchPrefix+"Su_G%C3%BCvenli%C4%9Fi", articleCapture(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Scheme != "http" || r.URL.Host != "127.0.0.1:12345" || r.URL.EscapedPath() != searchPrefix+"Su_G%C3%BCvenli%C4%9Fi" {
			t.Fatalf("request escaped bound target: %v", r)
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 15*time.Second {
			t.Fatal("missing bounded deadline")
		}
		return articleHTML(`<html><body><p>Suyu kaynatın.</p></body></html>`, "text/html; charset=utf-8"), nil
	}, []string{"/private/" + searchPackage + ".zim"}, nil))
	if err != nil || calls != 1 {
		t.Fatalf("read = %+v calls=%d err=%v", art, calls, err)
	}
	if art.PackageID != searchPackage || !strings.Contains(string(art.Body), "kaynat") {
		t.Fatalf("article = %+v", art)
	}
}

func TestReadArticleAvailabilityAndValidation(t *testing.T) {
	okTrip := func(*http.Request) (*http.Response, error) {
		return articleHTML("<html></html>", "text/html"), nil
	}
	for _, tc := range []struct {
		name            string
		packageID, href string
		paths           []string
		captureErr      error
		trip            func(*http.Request) (*http.Response, error)
		want            error
	}{
		{"runtime first", searchPackage, searchPrefix + "A", nil, errors.New("private path"), okTrip, providers.ErrRuntimeUnavailable},
		{"not loaded", searchPackage, searchPrefix + "A", []string{"/private/other.zim"}, nil, okTrip, providers.ErrContentUnavailable},
		{"empty package", "", searchPrefix + "A", []string{"/private/" + searchPackage + ".zim"}, nil, okTrip, providers.ErrArticleUnavailable},
		{"external href", searchPackage, "https://evil.example/x", []string{"/private/" + searchPackage + ".zim"}, nil, okTrip, providers.ErrArticleUnavailable},
		{"traversal href", searchPackage, "/content/content/" + searchPackage + "/../x", []string{"/private/" + searchPackage + ".zim"}, nil, okTrip, providers.ErrArticleUnavailable},
		{"wrong package href", searchPackage, "/content/content/other/A", []string{"/private/" + searchPackage + ".zim"}, nil, okTrip, providers.ErrArticleUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			trip := func(r *http.Request) (*http.Response, error) {
				calls++
				return tc.trip(r)
			}
			_, err := readArticleOwned(t.Context(), tc.packageID, tc.href, articleCapture(trip, tc.paths, tc.captureErr))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v want %v", err, tc.want)
			}
			if tc.want == providers.ErrArticleUnavailable && calls != 0 {
				t.Fatal("rejected href must not reach transport")
			}
		})
	}
}

func TestReadArticleRejectsNonHTMLAndRedirect(t *testing.T) {
	loaded := []string{"/private/" + searchPackage + ".zim"}
	for _, tc := range []struct {
		name string
		resp *http.Response
	}{
		{"redirect", &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1:12345/other"}}, Body: io.NopCloser(strings.NewReader(""))}},
		{"missing", &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("yok"))}},
		{"image", articleHTML("bytes", "image/png")},
		{"json", articleHTML("{}", "application/json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readArticleOwned(t.Context(), searchPackage, searchPrefix+"A", articleCapture(func(*http.Request) (*http.Response, error) {
				return tc.resp, nil
			}, loaded, nil))
			if !errors.Is(err, providers.ErrArticleUnavailable) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestReadArticleTruncatesOverLimit(t *testing.T) {
	big := "<html><body><p>" + strings.Repeat("veri ", 64*1024) + "</p></body></html>"
	art, err := readArticleOwned(t.Context(), searchPackage, searchPrefix+"A", articleCapture(func(*http.Request) (*http.Response, error) {
		return articleHTML(big, "text/html"), nil
	}, []string{"/private/" + searchPackage + ".zim"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(art.Body) != articleBodyLimit {
		t.Fatalf("body len = %d want %d", len(art.Body), articleBodyLimit)
	}
}
