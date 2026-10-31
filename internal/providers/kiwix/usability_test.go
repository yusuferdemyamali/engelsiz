package kiwix

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type usabilityTransport func(*http.Request) (*http.Response, error)

func (f usabilityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCandidateUsabilityRequiresSpecificBook(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name     string
		status   int
		location string
		loaded   bool
		wantOK   bool
	}{
		{"ready book", 200, "", true, true},
		{"ready homepage missing book", 404, "", true, false},
		{"unloaded", 200, "", false, false},
		{"redirect homepage", 302, "/content/", true, false},
		{"redirect external", 302, "https://example.com/", true, false},
		{"redirect article", 302, "/content/content/" + id + "/A/Main_Page", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			transport := usabilityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasPrefix(r.URL.Path, "/content/content/"+id+"/") {
					t.Fatalf("wrong book request %s", r.URL)
				}
				status := tc.status
				if calls > 1 {
					status = 200
				}
				header := http.Header{}
				if tc.location != "" {
					header.Set("Location", tc.location)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("page")), Request: r}, nil
			})
			capture := func() (http.RoundTripper, *url.URL, []string, error) {
				paths := []string{}
				if tc.loaded {
					paths = append(paths, id+".zim")
				}
				return transport, &url.URL{Scheme: "http", Host: "127.0.0.1:1"}, paths, nil
			}
			err := checkContent(context.Background(), id, capture)
			if (err == nil) != tc.wantOK {
				t.Fatalf("err=%v", err)
			}
			if !tc.loaded && calls != 0 {
				t.Fatal("unloaded book requested")
			}
		})
	}
}
