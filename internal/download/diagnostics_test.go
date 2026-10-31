package download

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"strings"
	"testing"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTransferDiagnosticsOmitRemoteErrorsAndMetadata(t *testing.T) {
	var logs bytes.Buffer
	secret := "https://user:password@example.invalid/?q=private-search&token=secret"
	m, _, _ := testManager(t, Options{Logger: log.New(&logs, "", 0), HTTPClient: &http.Client{Transport: diagnosticTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret + strings.Repeat("article body", 10000))
	})}})
	d := descriptor("https://example.invalid/?token=secret", nil)
	d.Title = "private title"
	d.ProviderPackageID = "private provider id"
	snapshot, e := m.Create(d)
	if e != nil {
		t.Fatal(e)
	}
	failed := waitStatus(t, m, snapshot.ID, "failed")
	if failed.Error == nil || failed.Error.Code != "source_unavailable" {
		t.Fatalf("state %+v", failed)
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	text := logs.String()
	if !strings.Contains(text, "download transfer:") || !strings.Contains(text, "code=source_unavailable") || !strings.Contains(text, snapshot.ID) {
		t.Fatalf("missing operation diagnostics %s", text)
	}
	for _, private := range []string{"password", "token=", "private-search", "article body", "private title", "private provider id"} {
		if strings.Contains(text, private) {
			t.Fatalf("diagnostic leaked %q", private)
		}
	}
	if len(text) > 1024 {
		t.Fatalf("unbounded error log %d", len(text))
	}
}
func TestPreflightAndStorageFailureDiagnostics(t *testing.T) {
	for _, disk := range []bool{true, false} {
		t.Run(map[bool]string{true: "disk", false: "database"}[disk], func(t *testing.T) {
			var logs bytes.Buffer
			opts := Options{Logger: log.New(&logs, "", 0)}
			if disk {
				opts.Available = func(string) (uint64, error) { return 0, nil }
			}
			m, db, _ := testManager(t, opts)
			expected := "insufficient_space"
			if !disk {
				db.Close()
				expected = "storage_error"
			}
			if _, e := m.Create(descriptor("https://example.invalid/source", nil)); e == nil {
				t.Fatal("failure passed")
			}
			if text := logs.String(); !strings.Contains(text, "download create:") || !strings.Contains(text, "code="+expected) {
				t.Fatalf("missing failure diagnostics %s", text)
			}
		})
	}
}
