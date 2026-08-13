package httpapi

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/stack"
)

func TestExchangeHTTPRoundTripReplayHistoryAndOwnershipClaim(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	product := createPeopleRecord[stack.Product](t, handler, session, "/api/v1/stack/products", map[string]any{
		"id": "exchange-product", "name": "Exchange Product", "publisher": "Example Publisher", "category": "operations",
	})
	version := createPeopleRecord[stack.Version](t, handler, session, "/api/v1/stack/versions", map[string]any{
		"id": "exchange-version", "productId": product.ID, "name": "1.0",
	})

	records := httptest.NewRecorder()
	handler.ServeHTTP(records, authenticatedRequest(http.MethodGet, "/api/v1/exchange/records", nil, session))
	if records.Code != http.StatusOK || !strings.Contains(records.Body.String(), `"id":"exchange-version"`) ||
		!strings.Contains(records.Body.String(), `"maximumArchiveBytes":33554432`) ||
		!strings.Contains(records.Body.String(), `"dependencies":[]`) ||
		strings.Contains(records.Body.String(), `"dependencies":null`) {
		t.Fatalf("unexpected Exchange catalog %d: %s", records.Code, records.Body.String())
	}

	exportPayload, _ := json.Marshal(map[string]any{
		"selection":           []map[string]string{{"type": "stack.version", "id": version.ID}},
		"includeDependencies": true,
		"fileMode":            "metadata",
	})
	exported := httptest.NewRecorder()
	handler.ServeHTTP(exported, authenticatedRequest(http.MethodPost, "/api/v1/exchange/export", bytes.NewReader(exportPayload), session))
	if exported.Code != http.StatusOK || exported.Header().Get("Content-Type") != exchange.MediaType ||
		exported.Header().Get("X-Exchange-Package-ID") == "" || len(exported.Header().Get("X-Content-SHA256")) != 64 ||
		!strings.HasSuffix(exported.Header().Get("Content-Disposition"), `.openinventory"`) || exported.Body.Len() == 0 {
		t.Fatalf("unexpected Exchange export %d headers=%v body-bytes=%d", exported.Code, exported.Header(), exported.Body.Len())
	}
	packageBytes := append([]byte(nil), exported.Body.Bytes()...)

	importPackage := func() *httptest.ResponseRecorder {
		request := authenticatedRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewReader(packageBytes), session)
		request.Header.Set("Content-Type", exchange.MediaType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	imported := importPackage()
	if imported.Code != http.StatusCreated || imported.Header().Get("X-Idempotent-Replay") != "false" ||
		!strings.Contains(imported.Body.String(), `"status":"completed"`) ||
		!strings.Contains(imported.Body.String(), `"unchangedCount":2`) ||
		!strings.Contains(imported.Body.String(), `"writeLocked":true`) {
		t.Fatalf("unexpected Exchange import %d headers=%v body=%s", imported.Code, imported.Header(), imported.Body.String())
	}
	replayed := importPackage()
	if replayed.Code != http.StatusOK || replayed.Header().Get("X-Idempotent-Replay") != "true" ||
		!strings.Contains(replayed.Body.String(), `"replay":true`) {
		t.Fatalf("unexpected Exchange replay %d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	history := httptest.NewRecorder()
	handler.ServeHTTP(history, authenticatedRequest(http.MethodGet, "/api/v1/exchange/packages?limit=25", nil, session))
	if history.Code != http.StatusOK || strings.Count(history.Body.String(), `"direction":`) != 2 ||
		!strings.Contains(history.Body.String(), `"direction":"export"`) || !strings.Contains(history.Body.String(), `"direction":"import"`) ||
		strings.Contains(history.Body.String(), `"missingDependencies":null`) {
		t.Fatalf("unexpected Exchange history %d: %s", history.Code, history.Body.String())
	}

	statusPayload := bytes.NewBufferString(`{"status":"retired","revision":1}`)
	locked := httptest.NewRecorder()
	handler.ServeHTTP(locked, authenticatedRequest(http.MethodPut, "/api/v1/stack/products/"+product.ID+"/status", statusPayload, session))
	if locked.Code != http.StatusLocked || !strings.Contains(locked.Body.String(), "ownership_locked") {
		t.Fatalf("expected imported Stack product lock, got %d: %s", locked.Code, locked.Body.String())
	}
	claim := httptest.NewRecorder()
	handler.ServeHTTP(claim, authenticatedRequest(http.MethodPost, "/api/v1/guard/resource-ownership/stack.product/"+product.ID+"/claim", nil, session))
	if claim.Code != http.StatusOK || !strings.Contains(claim.Body.String(), `"writeLocked":false`) {
		t.Fatalf("expected Exchange ownership claim, got %d: %s", claim.Code, claim.Body.String())
	}
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, authenticatedRequest(http.MethodPut, "/api/v1/stack/products/"+product.ID+"/status", bytes.NewBufferString(`{"status":"retired","revision":1}`), session))
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"status":"retired"`) {
		t.Fatalf("expected claimed Stack product mutation, got %d: %s", allowed.Code, allowed.Body.String())
	}
}

func TestExchangeHTTPRejectsUnsafeTransportAndCorruption(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)

	wrongType := authenticatedRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewBufferString("not a package"), session)
	wrongTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongTypeResponse, wrongType)
	if wrongTypeResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected media type rejection, got %d: %s", wrongTypeResponse.Code, wrongTypeResponse.Body.String())
	}

	corrupt := authenticatedRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewBufferString("not a zip"), session)
	corrupt.Header.Set("Content-Type", exchange.MediaType)
	corruptResponse := httptest.NewRecorder()
	handler.ServeHTTP(corruptResponse, corrupt)
	if corruptResponse.Code != http.StatusUnprocessableEntity || !strings.Contains(corruptResponse.Body.String(), "integrity_failed") {
		t.Fatalf("expected corruption rejection, got %d: %s", corruptResponse.Code, corruptResponse.Body.String())
	}

	encoded := authenticatedRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewBufferString("zip"), session)
	encoded.Header.Set("Content-Type", exchange.MediaType)
	encoded.Header.Set("Content-Encoding", "gzip")
	encodedResponse := httptest.NewRecorder()
	handler.ServeHTTP(encodedResponse, encoded)
	if encodedResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected content encoding rejection, got %d", encodedResponse.Code)
	}

	tooLarge := authenticatedRequest(http.MethodPost, "/api/v1/exchange/import", bytes.NewBufferString("x"), session)
	tooLarge.Header.Set("Content-Type", exchange.MediaType)
	tooLarge.ContentLength = exchange.MaximumArchiveBytes + 1
	tooLargeResponse := httptest.NewRecorder()
	handler.ServeHTTP(tooLargeResponse, tooLarge)
	if tooLargeResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected size rejection, got %d: %s", tooLargeResponse.Code, tooLargeResponse.Body.String())
	}

	missingCSRF := authenticatedRequest(http.MethodPost, "/api/v1/exchange/export", bytes.NewBufferString(`{}`), session)
	missingCSRF.Header.Del(csrfHeader)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || !strings.Contains(missingCSRFResponse.Body.String(), "csrf_failed") {
		t.Fatalf("expected CSRF rejection, got %d: %s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
	}
}
