package httpapi

// Requirement: REQ-FOUNDATION-001.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
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

func TestOrganizationAndCorrelationBoundary(t *testing.T) {
	organization, err := bootstrap.NewOrganization("example-org", "Example Organization")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(
		Dependencies{Assets: repository.NewMemoryAssetRepository()},
		"http://localhost:5173",
		organization,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil)
	req.Header.Set("X-Correlation-ID", "request-123")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if actual := res.Header().Get("X-Correlation-ID"); actual != "request-123" {
		t.Fatalf("expected correlation header to round trip, got %q", actual)
	}
	var body bootstrap.Organization
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != organization.ID || body.Name != organization.Name {
		t.Fatalf("unexpected organization %#v", body)
	}
}

func TestInvalidCorrelationIDIsReplacedAndReturnedWithErrors(t *testing.T) {
	handler := NewServer(Dependencies{Assets: repository.NewMemoryAssetRepository()}, "http://localhost:5173")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets", bytes.NewBufferString("{}"))
	req.Header.Set("X-Correlation-ID", "contains unsafe spaces")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
	correlationID := res.Header().Get("X-Correlation-ID")
	if len(correlationID) != 32 {
		t.Fatalf("expected generated correlation ID, got %q", correlationID)
	}
	var body struct {
		Error struct {
			CorrelationID string `json:"correlationId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.CorrelationID != correlationID {
		t.Fatalf("expected error correlation ID %q, got %q", correlationID, body.Error.CorrelationID)
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
