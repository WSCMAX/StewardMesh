package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestHealth(t *testing.T) {
	handler := NewServer(Dependencies{Assets: repository.NewMemoryAssetRepository(), Departments: repository.NewMemoryCatalog()}, "http://localhost:5173")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
}

func TestCreateAndListAsset(t *testing.T) {
	handler := NewServer(Dependencies{Assets: repository.NewMemoryAssetRepository()}, "http://localhost:5173")
	payload, _ := json.Marshal(map[string]string{"id": "asset-1", "name": "Lab server", "kind": "server"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(payload))
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	handler.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRes.Code)
	}
}

func TestPeopleAndThreadsCollections(t *testing.T) {
	catalog := repository.NewMemoryCatalog()
	handler := NewServer(Dependencies{
		Assets:      repository.NewMemoryAssetRepository(),
		Departments: catalog,
		Users:       catalog,
		Tags:        catalog,
		Goals:       catalog,
	}, "http://localhost:5173")
	for _, path := range []string{"/api/v1/departments", "/api/v1/users", "/api/v1/tags", "/api/v1/goals"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", path, res.Code)
		}
	}
}
