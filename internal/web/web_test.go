package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerServesPlaceholder(t *testing.T) {
	if _, err := fs.Stat(distFS(), "index.html"); err != nil {
		t.Skip("index.html not embedded yet (dev placeholder only), skipping")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Result().StatusCode)
	}
}

func TestHandlerDoesNotShadowAPI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (api handled elsewhere)", rec.Result().StatusCode)
	}
}

func TestDistGitkeepExists(t *testing.T) {
	if _, err := os.Stat(filepath.Join("dist", ".gitkeep")); err != nil {
		t.Fatalf("internal/web/dist/.gitkeep missing: %v", err)
	}
}
