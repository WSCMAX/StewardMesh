package httpapi

// Requirement: REQ-WORKSPACE-001. Feature: experience.workspace.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceFileServerFallsBackToIndexAndKeepsAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><title>StewardMesh</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(root, "assets")
	if err := os.Mkdir(assets, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "app.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/v1/organization", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"id":"test"}`))
	})
	served := (&Server{webDir: root}).withWorkspace(mux)

	index := httptest.NewRecorder()
	served.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "StewardMesh") {
		t.Fatalf("expected workspace index, got %d %s", index.Code, index.Body.String())
	}

	asset := httptest.NewRecorder()
	served.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "console.log") {
		t.Fatalf("expected hashed workspace asset, got %d %s", asset.Code, asset.Body.String())
	}

	deepLink := httptest.NewRecorder()
	served.ServeHTTP(deepLink, httptest.NewRequest(http.MethodGet, "/atlas/assets/example", nil))
	if deepLink.Code != http.StatusOK || !strings.Contains(deepLink.Body.String(), "StewardMesh") {
		t.Fatalf("expected SPA fallback, got %d %s", deepLink.Code, deepLink.Body.String())
	}

	health := httptest.NewRecorder()
	served.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok" {
		t.Fatalf("workspace must not intercept /healthz, got %d %s", health.Code, health.Body.String())
	}

	api := httptest.NewRecorder()
	served.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil))
	if api.Code != http.StatusOK || api.Body.String() != `{"id":"test"}` {
		t.Fatalf("workspace must not intercept API routes, got %d %s", api.Code, api.Body.String())
	}

	escaped := httptest.NewRecorder()
	served.ServeHTTP(escaped, httptest.NewRequest(http.MethodGet, "/..%2f..%2fetc/passwd", nil))
	if escaped.Code == http.StatusOK && strings.Contains(escaped.Body.String(), "root:") {
		t.Fatal("workspace must not serve files outside the web directory")
	}
	if escaped.Code == http.StatusOK && !strings.Contains(escaped.Body.String(), "StewardMesh") {
		t.Fatalf("traversal must not leak non-workspace content, got %d %s", escaped.Code, escaped.Body.String())
	}

	dotDot := httptest.NewRecorder()
	served.ServeHTTP(dotDot, httptest.NewRequest(http.MethodGet, "/assets/../../etc/passwd", nil))
	if dotDot.Code == http.StatusOK && strings.Contains(dotDot.Body.String(), "root:") {
		t.Fatal("cleaned traversal must not serve files outside the web directory")
	}
}
