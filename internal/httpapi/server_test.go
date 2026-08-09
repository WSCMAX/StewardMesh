package httpapi

// Requirements: REQ-FOUNDATION-001, REQ-PEOPLE-001, SEC-GUARD-001, SEC-HTTP-001.

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

const testOrigin = "http://localhost:5173"

type httpTestHasher struct{}

func (httpTestHasher) Hash(password string) (string, error) {
	digest := sha256.Sum256([]byte(password))
	return "test$" + base64.RawStdEncoding.EncodeToString(digest[:]), nil
}

func (httpTestHasher) Verify(password, encodedHash string) (bool, bool, error) {
	expected, _ := (httpTestHasher{}).Hash(password)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(encodedHash)) == 1, false, nil
}

type testSession struct {
	cookie    *http.Cookie
	csrfToken string
}

func TestHealthIsPublicAndHardened(t *testing.T) {
	handler := newGuardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("Content-Security-Policy") == "" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("expected centralized browser security headers")
	}
}

func TestProtectedRoutesRequireAuthentication(t *testing.T) {
	handler := newGuardServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected non-cacheable 401, got %d: %s", res.Code, res.Body.String())
	}
}

func TestBootstrapSessionAndOrganizationCorrelation(t *testing.T) {
	handler := newGuardServer(t)
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/bootstrap", nil)
	statusRes := httptest.NewRecorder()
	handler.ServeHTTP(statusRes, statusReq)
	if statusRes.Code != http.StatusOK {
		t.Fatalf("expected bootstrap status 200, got %d", statusRes.Code)
	}
	var status struct {
		Required                  bool `json:"required"`
		TokenRequired             bool `json:"tokenRequired"`
		MinimumPasswordCharacters int  `json:"minimumPasswordCharacters"`
	}
	if err := json.Unmarshal(statusRes.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Required || status.TokenRequired || status.MinimumPasswordCharacters != guard.MinimumPasswordCharacters {
		t.Fatalf("unexpected bootstrap status %#v", status)
	}
	session := bootstrapAdministrator(t, handler)
	if !session.cookie.HttpOnly || session.cookie.Secure || session.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected local session cookie %#v", session.cookie)
	}
	req := authenticatedRequest(http.MethodGet, "/api/v1/organization", nil, session)
	req.Header.Set("X-Correlation-ID", "request-123")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected non-cacheable 200, got %d: %s", res.Code, res.Body.String())
	}
	if actual := res.Header().Get("X-Correlation-ID"); actual != "request-123" {
		t.Fatalf("expected correlation header to round trip, got %q", actual)
	}
	var body bootstrap.Organization
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "example-org" || body.Name != "Example Organization" {
		t.Fatalf("unexpected organization %#v", body)
	}
	sessionReq := authenticatedRequest(http.MethodGet, "/api/v1/auth/session", nil, session)
	sessionRes := httptest.NewRecorder()
	handler.ServeHTTP(sessionRes, sessionReq)
	if sessionRes.Code != http.StatusOK || sessionRes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected non-cacheable session response, got %d", sessionRes.Code)
	}
}

func TestInvalidCorrelationIDIsReplacedAndReturnedWithErrors(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	req := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewBufferString("{}"), session)
	req.Header.Set("X-Correlation-ID", "contains unsafe spaces")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
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

func TestCreateAndListAssetRequiresPermissionAndCSRF(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	payload, _ := json.Marshal(map[string]string{"id": "asset-1", "name": "Lab server", "kind": "server"})
	createReq := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(payload), session)
	createRes := httptest.NewRecorder()
	handler.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}
	listReq := authenticatedRequest(http.MethodGet, "/api/v1/assets", nil, session)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRes.Code)
	}
	missingCSRF := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(payload), session)
	missingCSRF.Header.Del(csrfHeader)
	missingRes := httptest.NewRecorder()
	handler.ServeHTTP(missingRes, missingCSRF)
	if missingRes.Code != http.StatusForbidden {
		t.Fatalf("expected missing csrf token to fail, got %d", missingRes.Code)
	}
	crossSite := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(payload), session)
	crossSite.Header.Set("Origin", "https://attacker.example")
	crossSiteRes := httptest.NewRecorder()
	handler.ServeHTTP(crossSiteRes, crossSite)
	if crossSiteRes.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site mutation to fail, got %d", crossSiteRes.Code)
	}
}

func TestPeopleAndThreadsCollectionsRequireDirectoryGrants(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	for _, path := range []string{"/api/v1/departments", "/api/v1/users", "/api/v1/tags", "/api/v1/goals"} {
		req := authenticatedRequest(http.MethodGet, path, nil, session)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", path, res.Code, res.Body.String())
		}
	}
}

func TestPeopleDirectoryCreateSearchAndAssignmentHistory(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)

	site := createPeopleRecord[people.Site](t, handler, session, "/api/v1/sites", map[string]any{
		"name": "Main Campus",
	})
	department := createPeopleRecord[people.Department](t, handler, session, "/api/v1/departments", map[string]any{
		"name": "Technology", "siteId": site.ID,
	})
	person := createPeopleRecord[people.Identity](t, handler, session, "/api/v1/identities", map[string]any{
		"kind": "person", "displayName": "Alex Rivera", "email": "alex@example.test", "departmentId": department.ID,
	})
	shared := createPeopleRecord[people.Identity](t, handler, session, "/api/v1/identities", map[string]any{
		"kind": "shared", "displayName": "Public workstation users", "departmentId": department.ID,
	})
	if person.SiteID != site.ID || shared.Email != "" {
		t.Fatalf("unexpected typed identities %#v %#v", person, shared)
	}

	searchReq := authenticatedRequest(http.MethodGet, "/api/v1/identities?q=public&kind=shared&limit=10", nil, session)
	searchRes := httptest.NewRecorder()
	handler.ServeHTTP(searchRes, searchReq)
	if searchRes.Code != http.StatusOK {
		t.Fatalf("expected directory search 200, got %d: %s", searchRes.Code, searchRes.Body.String())
	}
	var searchBody struct {
		Items []people.Identity `json:"items"`
	}
	if err := json.Unmarshal(searchRes.Body.Bytes(), &searchBody); err != nil {
		t.Fatal(err)
	}
	if len(searchBody.Items) != 1 || searchBody.Items[0].ID != shared.ID {
		t.Fatalf("unexpected directory search %#v", searchBody.Items)
	}

	assetPayload, _ := json.Marshal(map[string]string{"id": "asset-people-1", "name": "Lab computer", "kind": "computer"})
	assetReq := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(assetPayload), session)
	assetRes := httptest.NewRecorder()
	handler.ServeHTTP(assetRes, assetReq)
	if assetRes.Code != http.StatusCreated {
		t.Fatalf("expected asset 201, got %d: %s", assetRes.Code, assetRes.Body.String())
	}
	first := createPeopleRecord[people.AssetAssignment](t, handler, session, "/api/v1/assets/asset-people-1/assignments", map[string]any{
		"assigneeKind": "identity", "assigneeId": person.ID, "role": "primary", "effectiveFrom": "2030-01-01T00:00:00Z",
	})
	second := createPeopleRecord[people.AssetAssignment](t, handler, session, "/api/v1/assets/asset-people-1/assignments", map[string]any{
		"assigneeKind": "identity", "assigneeId": shared.ID, "role": "primary", "effectiveFrom": "2030-02-01T00:00:00Z",
	})
	historyReq := authenticatedRequest(http.MethodGet, "/api/v1/assets/asset-people-1/assignments", nil, session)
	historyRes := httptest.NewRecorder()
	handler.ServeHTTP(historyRes, historyReq)
	if historyRes.Code != http.StatusOK {
		t.Fatalf("expected assignment history 200, got %d: %s", historyRes.Code, historyRes.Body.String())
	}
	var historyBody struct {
		Items []people.AssetAssignment `json:"items"`
	}
	if err := json.Unmarshal(historyRes.Body.Bytes(), &historyBody); err != nil {
		t.Fatal(err)
	}
	if len(historyBody.Items) != 2 {
		t.Fatalf("unexpected assignment history %#v", historyBody.Items)
	}
	var previousFound bool
	for _, assignment := range historyBody.Items {
		if assignment.ID == first.ID && assignment.EffectiveTo != nil && assignment.EffectiveTo.Equal(second.EffectiveFrom) {
			previousFound = true
		}
	}
	if !previousFound {
		t.Fatalf("replacement did not preserve previous primary %#v", historyBody.Items)
	}

	endPayload, _ := json.Marshal(map[string]string{"effectiveTo": "2030-03-01T00:00:00Z"})
	endReq := authenticatedRequest(http.MethodPatch, "/api/v1/assets/asset-people-1/assignments/"+second.ID, bytes.NewReader(endPayload), session)
	endRes := httptest.NewRecorder()
	handler.ServeHTTP(endRes, endReq)
	if endRes.Code != http.StatusOK {
		t.Fatalf("expected assignment end 200, got %d: %s", endRes.Code, endRes.Body.String())
	}

	invalidPayload, _ := json.Marshal(map[string]string{"kind": "person", "displayName": "Missing email"})
	invalidReq := authenticatedRequest(http.MethodPost, "/api/v1/identities", bytes.NewReader(invalidPayload), session)
	invalidRes := httptest.NewRecorder()
	handler.ServeHTTP(invalidRes, invalidReq)
	if invalidRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid person to fail, got %d: %s", invalidRes.Code, invalidRes.Body.String())
	}
}

func TestLoginUsesUniformErrorsAndStrictJSON(t *testing.T) {
	handler := newGuardServer(t)
	bootstrapAdministrator(t, handler)
	invalidBody := bytes.NewBufferString(`{"username":"administrator","password":"wrong"} {}`)
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", invalidBody)
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidReq.Header.Set("Origin", testOrigin)
	invalidRes := httptest.NewRecorder()
	handler.ServeHTTP(invalidRes, invalidReq)
	if invalidRes.Code != http.StatusBadRequest {
		t.Fatalf("expected trailing JSON to fail, got %d", invalidRes.Code)
	}
	payload, _ := json.Marshal(map[string]string{"username": "unknown-user", "password": "incorrect password value"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(payload))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", testOrigin)
	loginRes := httptest.NewRecorder()
	handler.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusUnauthorized || !bytes.Contains(loginRes.Body.Bytes(), []byte("invalid username or password")) {
		t.Fatalf("expected uniform credential error, got %d: %s", loginRes.Code, loginRes.Body.String())
	}
}

func TestCORSAllowsOnlyConfiguredCredentialedOrigin(t *testing.T) {
	handler := newGuardServer(t)
	allowed := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	allowed.Header.Set("Origin", testOrigin)
	allowedRes := httptest.NewRecorder()
	handler.ServeHTTP(allowedRes, allowed)
	if allowedRes.Code != http.StatusNoContent ||
		allowedRes.Header().Get("Access-Control-Allow-Origin") != testOrigin ||
		allowedRes.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("unexpected allowed preflight %d %#v", allowedRes.Code, allowedRes.Header())
	}
	denied := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	denied.Header.Set("Origin", "https://attacker.example")
	deniedRes := httptest.NewRecorder()
	handler.ServeHTTP(deniedRes, denied)
	if deniedRes.Code != http.StatusForbidden ||
		deniedRes.Header().Get("Access-Control-Allow-Origin") != "" ||
		deniedRes.Header().Get("Vary") != "Origin" {
		t.Fatalf("expected untrusted origin to fail, got %d", deniedRes.Code)
	}
}

func newGuardServer(t *testing.T) http.Handler {
	t.Helper()
	organization, err := bootstrap.NewOrganization("example-org", "Example Organization")
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewMemoryGuardStore()
	service, err := guard.NewService(store, httpTestHasher{}, foundation.NopAuditor{}, nil, guard.ServiceConfig{
		OrganizationID: organization.ID,
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	assets := repository.NewMemoryAssetRepository()
	peopleService, err := people.NewService(repository.NewMemoryPeopleStore(), assets, foundation.NopAuditor{}, people.ServiceConfig{
		OrganizationID: organization.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := repository.NewMemoryCatalog()
	return NewServer(Dependencies{
		Assets: assets,
		People: peopleService,
		Tags:   catalog,
		Goals:  catalog,
		Guard:  service,
	}, testOrigin, organization)
}

func createPeopleRecord[T any](t *testing.T, handler http.Handler, session testSession, path string, payload any) T {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := authenticatedRequest(http.MethodPost, path, bytes.NewReader(encoded), session)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201 for %s, got %d: %s", path, res.Code, res.Body.String())
	}
	var record T
	if err := json.Unmarshal(res.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func bootstrapAdministrator(t *testing.T, handler http.Handler) testSession {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"username":    "administrator",
		"email":       "administrator@example.test",
		"displayName": "Example Administrator",
		"password":    "correct horse battery staple",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected bootstrap 201, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || body.CSRFToken == "" {
		t.Fatalf("expected session cookie and csrf token, cookies=%#v body=%s", cookies, res.Body.String())
	}
	return testSession{cookie: cookies[0], csrfToken: body.CSRFToken}
}

func authenticatedRequest(method, path string, body io.Reader, session testSession) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.AddCookie(session.cookie)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", testOrigin)
		req.Header.Set(csrfHeader, session.csrfToken)
	}
	return req
}
