package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type contentStub struct {
	transport http.RoundTripper
	target    *url.URL
}

func (s contentStub) ContentTransport() (http.RoundTripper, *url.URL, error) {
	return s.transport, s.target, nil
}
func TestContentConfinement(t *testing.T) {
	for _, path := range []string{"/content/../api/health", "/content/%2e%2e/api", "/content/%252e%252e/api", "/content/a%2f..%2f..%2fapi", "/content/a%5cb", "/content//evil"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			WithContent(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("escaped to shell") }), nil, nil).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 400 {
				t.Fatalf("%d", w.Code)
			}
		})
	}
	target, _ := url.Parse("http://127.0.0.1:32123")
	for _, location := range []string{"", "article%20name?x=1#part", "http://127.0.0.1:32123/content/item", "https://evil.invalid/content/", "/api/health", "//evil.invalid/content/", "/%63ontent/../api"} {
		t.Run(location, func(t *testing.T) {
			called := false
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.URL.Host != target.Host || r.Host != target.Host || r.URL.EscapedPath() != "/content/book/a%20b" || r.URL.RawQuery != "url=https://evil.invalid" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-Host") != "" {
					t.Fatalf("unconfined request=%+v", r)
				}
				header := http.Header{"Location": []string{location}, "Refresh": []string{"0;url=" + target.String()}, "X-Upstream": []string{target.String()}}
				return &http.Response{StatusCode: 302, Header: header, Body: io.NopCloser(strings.NewReader("article"))}, nil
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/content/book/a%20b?url=https://evil.invalid", nil)
			r.Host = "evil.invalid"
			r.Header.Set("Forwarded", "host=evil.invalid")
			r.Header.Set("X-Forwarded-Host", "evil.invalid")
			ContentHandler(contentStub{transport, target}, nil).ServeHTTP(w, r)
			safe := location == "" || strings.HasPrefix(location, "article") || strings.HasPrefix(location, target.String())
			if !called || safe && w.Code != 302 || !safe && w.Code != 502 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "127.0.0.1") || strings.Contains(w.Header().Get("Location"), "127.0.0.1") || w.Header().Get("Refresh") != "" || w.Header().Get("X-Upstream") != "" {
				t.Fatalf("internal endpoint leaked: %v %s", w.Header(), w.Body)
			}
		})
	}
}

// A literal percent can become an invalid escape after one decode. It must
// remain a valid article name without ending validation of other escapes.
func TestContentLiteralPercentDoesNotHideUnsafeEscapes(t *testing.T) {
	target, _ := url.Parse("http://127.0.0.1:32123")
	for _, tc := range []struct {
		path string
		safe bool
	}{
		{"/content/%25zz/%252e%252e/api", false},
		{"/content/%25zz/%255csecret", false},
		{"/content/%25zz/%252f%252fevil", false},
		{"/content/%25zz%255csecret", false},
		{"/content/100%25done", true},
		{"/content/Caf%C3%A9%20du%20monde", true},
		{"/content/%25zz/Article%20name", true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			contacted := false
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				contacted = true
				if r.URL.EscapedPath() != tc.path {
					t.Fatalf("article path changed: %q", r.URL.EscapedPath())
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("article"))}, nil
			})
			w := httptest.NewRecorder()
			ContentHandler(contentStub{transport, target}, nil).ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			expected := http.StatusBadRequest
			if tc.safe {
				expected = http.StatusOK
			}
			if w.Code != expected || contacted != tc.safe {
				t.Fatalf("status=%d upstream contacted=%t; wanted status=%d contacted=%t", w.Code, contacted, expected, tc.safe)
			}
		})
	}
}
