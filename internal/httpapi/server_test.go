package httpapi

// Requirements: REQ-FOUNDATION-001, REQ-WORKSPACE-001, REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-ATLAS-CODES-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-DIRECTORY-EXPANSION-002, REQ-PATTERNS-001, REQ-THREADS-001, REQ-STORAGE-001, REQ-LEDGER-001, REQ-HORIZON-001, REQ-PLATFORM-VALKEY-001,
// SEC-GUARD-001, SEC-HTTP-001. Features include experience.workspace.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/identity"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
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

type unavailableHTTPAttemptLimiter struct{}

type allowHTTPAssetReferences struct{}

func (allowHTTPAssetReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}

type allowHTTPThreadTargets struct{}

func (allowHTTPThreadTargets) ValidateThreadTarget(context.Context, string, threads.TargetType, string) error {
	return nil
}

type allowHTTPLedgerReferences struct{}

func (allowHTTPLedgerReferences) ValidateAssets(context.Context, []string) error          { return nil }
func (allowHTTPLedgerReferences) ValidateDocuments(context.Context, []string) error       { return nil }
func (allowHTTPLedgerReferences) ValidateDirectory(context.Context, string, string) error { return nil }

type fakeHTTPOIDCAuthenticator struct {
	state         string
	nonce         string
	verifier      string
	authenticates int
}

type fakeHTTPSAMLAuthenticator struct {
	relayState        string
	expectedRequestID string
	authenticates     int
}

type httpDirectoryConnector struct {
	page  directoryexpansion.Page
	calls int
}

func (c *httpDirectoryConnector) SourceSystem() directoryexpansion.SourceSystem {
	return directoryexpansion.SourceSystem{ID: "hr-primary", Provider: "example", ConfigRevision: "v1"}
}

func (c *httpDirectoryConnector) PullPage(context.Context, string) (directoryexpansion.Page, error) {
	c.calls++
	return c.page, nil
}

func (f *fakeHTTPSAMLAuthenticator) AuthenticationURL(relayState string) (string, string, error) {
	f.relayState = relayState
	return "https://identity.example.test/sso?RelayState=" + url.QueryEscape(relayState), "id-saml-request", nil
}

func (f *fakeHTTPSAMLAuthenticator) Authenticate(request *http.Request, expectedRequestID string) (identity.SAMLPrincipal, error) {
	f.authenticates++
	f.expectedRequestID = expectedRequestID
	if expectedRequestID != "id-saml-request" || request.Form.Get("SAMLResponse") != "verified-assertion" {
		return identity.SAMLPrincipal{}, identity.ErrSAMLAuthentication
	}
	return identity.SAMLPrincipal{
		Issuer: "https://identity.example.test/saml", Subject: "persistent-name-id",
		Email: "saml@example.test", DisplayName: "SAML Administrator", Administrator: true,
	}, nil
}

func (*fakeHTTPSAMLAuthenticator) Metadata() ([]byte, error) {
	return []byte(`<?xml version="1.0"?><EntityDescriptor entityID="http://localhost:5173/api/v1/auth/saml/metadata"/>`), nil
}

func (f *fakeHTTPOIDCAuthenticator) AuthorizationURL(state, nonce, verifier string) (string, error) {
	f.state, f.nonce, f.verifier = state, nonce, verifier
	return "https://identity.example.test/authorize?state=" + url.QueryEscape(state), nil
}

func (f *fakeHTTPOIDCAuthenticator) Authenticate(_ context.Context, code, verifier, nonce string) (identity.OIDCPrincipal, error) {
	f.authenticates++
	if code != "authorization-code" || verifier != f.verifier || nonce != f.nonce {
		return identity.OIDCPrincipal{}, identity.ErrOIDCAuthentication
	}
	return identity.OIDCPrincipal{
		Issuer: "https://identity.example.test/tenant", Subject: "external-subject",
		Email: "external@example.test", EmailVerified: true, DisplayName: "External Administrator", Administrator: true,
	}, nil
}

func (unavailableHTTPAttemptLimiter) Allow(context.Context, string, time.Time) (bool, error) {
	return false, io.ErrUnexpectedEOF
}

func (unavailableHTTPAttemptLimiter) Failure(context.Context, string, time.Time) error {
	return io.ErrUnexpectedEOF
}

func (unavailableHTTPAttemptLimiter) Reset(context.Context, string) error {
	return io.ErrUnexpectedEOF
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

func TestPatternsTemplateManagementSchemaValidationAndCSV(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, authenticatedRequest(http.MethodGet, "/api/v1/templates?recordType=atlas.asset", nil, session))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"id":"builtin-atlas-asset"`) || !strings.Contains(list.Body.String(), `"accessibleLabel":"Asset name"`) {
		t.Fatalf("expected built-in form metadata, got %d: %s", list.Code, list.Body.String())
	}

	payload := map[string]any{
		"id": "http-intake", "recordType": "exchange.row", "name": "HTTP intake",
		"fields": []map[string]any{
			{"key": "name", "label": "Record name", "help": "Use the name shown by the source system.", "type": "text", "required": true},
			{"key": "ownerId", "label": "Owner", "type": "reference", "required": true, "allowHolding": true, "referenceType": "people.identity"},
		},
	}
	encoded, _ := json.Marshal(payload)
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, authenticatedRequest(http.MethodPost, "/api/v1/templates", bytes.NewReader(encoded), session))
	if create.Code != http.StatusCreated || !strings.Contains(create.Body.String(), `"version":1`) {
		t.Fatalf("expected custom template creation, got %d: %s", create.Code, create.Body.String())
	}

	schema := httptest.NewRecorder()
	handler.ServeHTTP(schema, authenticatedRequest(http.MethodGet, "/api/v1/templates/http-intake/schema?version=1", nil, session))
	if schema.Code != http.StatusOK || !strings.Contains(schema.Body.String(), `"csvHeader":"ownerId"`) {
		t.Fatalf("expected schema metadata, got %d: %s", schema.Code, schema.Body.String())
	}

	validationPayload, _ := json.Marshal(map[string]any{
		"values":            map[string]any{"name": "Imported row", "ownerId": "person-404"},
		"missingReferences": []string{"ownerId"}, "allowHoldingRecord": true,
	})
	validation := httptest.NewRecorder()
	handler.ServeHTTP(validation, authenticatedRequest(http.MethodPost, "/api/v1/templates/http-intake/validate?version=1", bytes.NewReader(validationPayload), session))
	if validation.Code != http.StatusOK || !strings.Contains(validation.Body.String(), `"status":"holding"`) || !strings.Contains(validation.Body.String(), `"field":"ownerId"`) {
		t.Fatalf("expected visible holding result, got %d: %s", validation.Code, validation.Body.String())
	}

	csvTemplate := httptest.NewRecorder()
	handler.ServeHTTP(csvTemplate, authenticatedRequest(http.MethodGet, "/api/v1/templates/http-intake/template.csv?version=1", nil, session))
	if csvTemplate.Code != http.StatusOK || csvTemplate.Header().Get("Content-Type") != "text/csv; charset=utf-8" || csvTemplate.Body.String() != "name,ownerId\n" {
		t.Fatalf("expected CSV template, got %d %q: %s", csvTemplate.Code, csvTemplate.Header().Get("Content-Type"), csvTemplate.Body.String())
	}
}

func TestVaultUploadMetadataAuthorizationAndDownload(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	file, err := writer.CreateFormFile("file", "evidence.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("verified evidence"))
	_ = writer.WriteField("sourceSystemId", "manual-import")
	_ = writer.WriteField("sourceRecordId", "row-42")
	_ = writer.WriteField("resourceType", "asset")
	_ = writer.WriteField("resourceId", "asset-42")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := authenticatedRequest(http.MethodPost, "/api/v1/blobs", &upload, session)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated || strings.Contains(res.Body.String(), "objectKey") || strings.Contains(res.Body.String(), "signed") {
		t.Fatalf("expected private Vault metadata, got %d: %s", res.Code, res.Body.String())
	}
	var blob storage.Blob
	if err := json.Unmarshal(res.Body.Bytes(), &blob); err != nil {
		t.Fatal(err)
	}
	if blob.ID == "" || blob.Name != "evidence.txt" || blob.SizeBytes != 17 || blob.SHA256 == "" || blob.SourceRecordID != "row-42" {
		t.Fatalf("unexpected Vault blob %#v", blob)
	}

	list := authenticatedRequest(http.MethodGet, "/api/v1/blobs", nil, session)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, list)
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), blob.ID) || !strings.Contains(listRes.Body.String(), "maximumUploadBytes") {
		t.Fatalf("unexpected Vault listing %d: %s", listRes.Code, listRes.Body.String())
	}
	authorize := authenticatedRequest(http.MethodPost, "/api/v1/blobs/"+blob.ID+"/download-authorization", nil, session)
	authorizeRes := httptest.NewRecorder()
	handler.ServeHTTP(authorizeRes, authorize)
	if authorizeRes.Code != http.StatusCreated || !strings.Contains(authorizeRes.Body.String(), "/content") || !strings.Contains(authorizeRes.Body.String(), "expiresAt") {
		t.Fatalf("unexpected Vault authorization %d: %s", authorizeRes.Code, authorizeRes.Body.String())
	}
	var authorization storage.DownloadAuthorization
	if err := json.Unmarshal(authorizeRes.Body.Bytes(), &authorization); err != nil {
		t.Fatal(err)
	}
	download := authenticatedRequest(http.MethodGet, authorization.URL, nil, session)
	downloadRes := httptest.NewRecorder()
	handler.ServeHTTP(downloadRes, download)
	if downloadRes.Code != http.StatusOK || downloadRes.Body.String() != "verified evidence" || downloadRes.Header().Get("ETag") == "" ||
		!strings.Contains(downloadRes.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("unexpected Vault download %d headers=%v body=%q", downloadRes.Code, downloadRes.Header(), downloadRes.Body.String())
	}
	unauthorizedDownload := authenticatedRequest(http.MethodGet, "/api/v1/blobs/"+blob.ID+"/content", nil, session)
	unauthorizedDownloadRes := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedDownloadRes, unauthorizedDownload)
	if unauthorizedDownloadRes.Code != http.StatusBadRequest {
		t.Fatalf("expected missing short-lived token to fail, got %d", unauthorizedDownloadRes.Code)
	}
}

func TestLedgerFinanceWorkflowVarianceReconciliationAndExport(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	vendor := createPeopleRecord[ledger.Vendor](t, handler, session, "/api/v1/ledger/vendors", map[string]any{
		"id": "vendor-1", "name": "Example Vendor", "externalId": "V-42",
	})
	if vendor.Status != "active" {
		t.Fatalf("unexpected vendor %#v", vendor)
	}
	orderedOn := "2026-08-01T00:00:00Z"
	purchaseOrder := createPeopleRecord[ledger.PurchaseOrder](t, handler, session, "/api/v1/ledger/purchase-orders", map[string]any{
		"id": "po-1", "number": "PO-2026-001", "vendorId": vendor.ID, "status": "ordered", "currency": "USD",
		"totalMinor": 180000, "orderedOn": orderedOn, "assetIds": []string{}, "receiptDocumentIds": []string{},
	})
	statusPayload, _ := json.Marshal(map[string]any{"status": "received", "revision": purchaseOrder.Revision})
	statusRequest := authenticatedRequest(http.MethodPut, "/api/v1/ledger/purchase-orders/po-1/status", bytes.NewReader(statusPayload), session)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"status":"received"`) {
		t.Fatalf("unexpected purchase status response %d: %s", statusResponse.Code, statusResponse.Body.String())
	}

	contract := createPeopleRecord[ledger.Contract](t, handler, session, "/api/v1/ledger/contracts", map[string]any{
		"id": "contract-1", "name": "Managed service", "vendorId": vendor.ID, "operationalStatus": "active",
		"financialStatus": "committed", "currency": "USD", "ceilingMinor": 600000,
		"startsOn": "2026-01-01T00:00:00Z", "endsOn": "2028-12-31T00:00:00Z",
	})
	createPeopleRecord[ledger.Commitment](t, handler, session, "/api/v1/ledger/commitments", map[string]any{
		"contractId": contract.ID, "kind": "subscription", "description": "Three-year managed service", "currency": "USD",
		"amountMinor": 200000, "startsOn": "2026-01-01T00:00:00Z", "endsOn": "2028-12-31T00:00:00Z",
		"fiscalPeriod": "FY2027", "scenario": "baseline",
	})
	createPeopleRecord[ledger.Budget](t, handler, session, "/api/v1/ledger/budgets", map[string]any{
		"name": "Infrastructure", "fiscalPeriod": "FY2027", "scenario": "baseline", "currency": "USD", "allocatedMinor": 200000,
	})
	costPayload, _ := json.Marshal(map[string]any{
		"description": "Vendor invoice", "kind": "billed", "currency": "USD", "amountMinor": 175000,
		"fiscalPeriod": "FY2027", "scenario": "baseline", "purchaseOrderId": purchaseOrder.ID,
		"sourceSystemId": "erp", "sourceRecordId": "invoice-42", "externalReference": "INV-42",
	})
	for index, expectedStatus := range []int{http.StatusCreated, http.StatusOK} {
		request := authenticatedRequest(http.MethodPost, "/api/v1/ledger/costs/reconcile", bytes.NewReader(costPayload), session)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != expectedStatus {
			t.Fatalf("reconciliation %d returned %d: %s", index, response.Code, response.Body.String())
		}
		if index == 1 && !strings.Contains(response.Body.String(), `"applied":false`) {
			t.Fatalf("expected idempotent reconciliation: %s", response.Body.String())
		}
	}
	varianceRequest := authenticatedRequest(http.MethodGet, "/api/v1/ledger/budget-variance?fiscalPeriod=FY2027&scenario=baseline", nil, session)
	varianceResponse := httptest.NewRecorder()
	handler.ServeHTTP(varianceResponse, varianceRequest)
	if varianceResponse.Code != http.StatusOK || !strings.Contains(varianceResponse.Body.String(), `"varianceMinor":25000`) || !strings.Contains(varianceResponse.Body.String(), `"overBudget":false`) {
		t.Fatalf("unexpected budget variance %d: %s", varianceResponse.Code, varianceResponse.Body.String())
	}
	exportRequest := authenticatedRequest(http.MethodGet, "/api/v1/ledger/export.csv?fiscalPeriod=FY2027&scenario=baseline", nil, session)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || !strings.HasPrefix(exportResponse.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(exportResponse.Body.String(), "Vendor invoice") || exportResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected Ledger export %d headers=%v body=%s", exportResponse.Code, exportResponse.Header(), exportResponse.Body.String())
	}
	snapshotRequest := authenticatedRequest(http.MethodGet, "/api/v1/ledger", nil, session)
	snapshotResponse := httptest.NewRecorder()
	handler.ServeHTTP(snapshotResponse, snapshotRequest)
	if snapshotResponse.Code != http.StatusOK || !strings.Contains(snapshotResponse.Body.String(), "Managed service") || !strings.Contains(snapshotResponse.Body.String(), "Three-year managed service") {
		t.Fatalf("unexpected Ledger snapshot %d: %s", snapshotResponse.Code, snapshotResponse.Body.String())
	}
}

func TestHorizonLifecyclePlanForecastHistoryAndExport(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	asset := createPeopleRecord[domain.Asset](t, handler, session, "/api/v1/assets", map[string]any{
		"id": "horizon-server", "name": "Forecast server", "kind": "server", "status": "active",
		"purchaseDate": "2024-01-31T00:00:00Z",
	})
	plan := createPeopleRecord[horizon.Plan](t, handler, session, "/api/v1/horizon/plans", map[string]any{
		"assetId": asset.ID, "scenario": "baseline", "expectedUsefulLifeMonths": 36,
		"lifecycleStage": "in_service", "replacementCostMinor": 240000, "currency": "USD",
		"effectiveFrom": "2026-01-01T00:00:00Z",
	})
	if plan.DerivedReplacementDate == nil || plan.Revision != 1 {
		t.Fatalf("unexpected Horizon plan %#v", plan)
	}
	updatePayload, _ := json.Marshal(map[string]any{
		"assetId": asset.ID, "scenario": "baseline", "expectedUsefulLifeMonths": 36,
		"replacementDate": "2027-06-30T00:00:00Z", "lifecycleStage": "approved",
		"replacementCostMinor": 260000, "currency": "USD", "effectiveFrom": "2026-08-01T00:00:00Z", "revision": plan.Revision,
	})
	updateRequest := authenticatedRequest(http.MethodPut, "/api/v1/horizon/plans/"+plan.ID, bytes.NewReader(updatePayload), session)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"revision":2`) || !strings.Contains(updateResponse.Body.String(), `"lifecycleStage":"approved"`) {
		t.Fatalf("unexpected Horizon plan update %d: %s", updateResponse.Code, updateResponse.Body.String())
	}
	historyRequest := authenticatedRequest(http.MethodGet, "/api/v1/horizon/plans/"+plan.ID+"/history", nil, session)
	historyResponse := httptest.NewRecorder()
	handler.ServeHTTP(historyResponse, historyRequest)
	if historyResponse.Code != http.StatusOK || !strings.Contains(historyResponse.Body.String(), `"revision":1`) ||
		!strings.Contains(historyResponse.Body.String(), `"revision":2`) ||
		!strings.Contains(historyResponse.Body.String(), `"derivedReplacementDate":"2027-01-31T00:00:00Z"`) {
		t.Fatalf("unexpected Horizon history %d: %s", historyResponse.Code, historyResponse.Body.String())
	}
	forecastPath := "/api/v1/horizon/forecast?scenarios=baseline&asOf=2026-08-11T00%3A00%3A00Z&fromYear=2027&toYear=2027&fiscalYearStartMonth=1&groupBy=fiscal_year"
	forecastRequest := authenticatedRequest(http.MethodGet, forecastPath, nil, session)
	forecastResponse := httptest.NewRecorder()
	handler.ServeHTTP(forecastResponse, forecastRequest)
	if forecastResponse.Code != http.StatusOK || !strings.Contains(forecastResponse.Body.String(), `"plannedReplacementMinor":260000`) || !strings.Contains(forecastResponse.Body.String(), `"label":"FY2027"`) || forecastResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected Horizon forecast %d headers=%v body=%s", forecastResponse.Code, forecastResponse.Header(), forecastResponse.Body.String())
	}
	exportRequest := authenticatedRequest(http.MethodGet, strings.Replace(forecastPath, "/forecast", "/export.csv", 1), nil, session)
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK || !strings.HasPrefix(exportResponse.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(exportResponse.Header().Get("Content-Disposition"), "horizon-forecast") || !strings.Contains(exportResponse.Body.String(), "FY2027") {
		t.Fatalf("unexpected Horizon export %d headers=%v body=%s", exportResponse.Code, exportResponse.Header(), exportResponse.Body.String())
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

func TestOIDCAuthorizationCodeFlowCreatesJITSession(t *testing.T) {
	authenticator := &fakeHTTPOIDCAuthenticator{}
	flow, err := identity.NewOIDCFlow(authenticator, strings.Repeat("t", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := newGuardServerWithDependencies(t, nil, flow)
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/bootstrap", nil)
	statusRes := httptest.NewRecorder()
	handler.ServeHTTP(statusRes, statusReq)
	var status struct {
		OIDCEnabled bool `json:"oidcEnabled"`
	}
	if err := json.Unmarshal(statusRes.Body.Bytes(), &status); err != nil || !status.OIDCEnabled {
		t.Fatalf("expected bootstrap status to advertise OpenID Connect, status=%#v err=%v", status, err)
	}
	startBeforeBootstrap := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	startBeforeBootstrap.Header.Set("Sec-Fetch-Site", "same-origin")
	startBeforeBootstrapRes := httptest.NewRecorder()
	handler.ServeHTTP(startBeforeBootstrapRes, startBeforeBootstrap)
	if startBeforeBootstrapRes.Code != http.StatusConflict {
		t.Fatalf("expected local administrator bootstrap before OpenID Connect, got %d", startBeforeBootstrapRes.Code)
	}
	bootstrapAdministrator(t, handler)

	start := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	start.Header.Set("Sec-Fetch-Site", "same-origin")
	startRes := httptest.NewRecorder()
	handler.ServeHTTP(startRes, start)
	if startRes.Code != http.StatusSeeOther || !strings.HasPrefix(startRes.Header().Get("Location"), "https://identity.example.test/authorize?") {
		t.Fatalf("unexpected OpenID Connect start response %d %#v", startRes.Code, startRes.Header())
	}
	transactionCookie := findCookie(t, startRes.Result().Cookies(), localOIDCTransactionName)
	if !transactionCookie.HttpOnly || transactionCookie.Secure || transactionCookie.SameSite != http.SameSiteLaxMode || transactionCookie.Value == "" {
		t.Fatalf("unexpected OpenID Connect transaction cookie %#v", transactionCookie)
	}
	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authorization-code&state="+url.QueryEscape(authenticator.state), nil)
	callback.AddCookie(transactionCookie)
	callbackRes := httptest.NewRecorder()
	handler.ServeHTTP(callbackRes, callback)
	if callbackRes.Code != http.StatusSeeOther || callbackRes.Header().Get("Location") != testOrigin || authenticator.authenticates != 1 {
		t.Fatalf("unexpected OpenID Connect callback %d location=%q authentications=%d body=%s",
			callbackRes.Code, callbackRes.Header().Get("Location"), authenticator.authenticates, callbackRes.Body.String())
	}
	sessionCookie := findCookie(t, callbackRes.Result().Cookies(), localSessionName)
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), "external@example.test") ||
		!strings.Contains(sessionResponse.Body.String(), "Administrator") {
		t.Fatalf("expected JIT administrator session, got %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestOIDCCallbackRejectsMismatchedStateAndClearsTransaction(t *testing.T) {
	authenticator := &fakeHTTPOIDCAuthenticator{}
	flow, err := identity.NewOIDCFlow(authenticator, strings.Repeat("t", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := newGuardServerWithDependencies(t, nil, flow)
	bootstrapAdministrator(t, handler)
	start := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	start.Header.Set("Sec-Fetch-Site", "same-origin")
	startRes := httptest.NewRecorder()
	handler.ServeHTTP(startRes, start)
	transactionCookie := findCookie(t, startRes.Result().Cookies(), localOIDCTransactionName)
	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authorization-code&state=wrong-state", nil)
	callback.AddCookie(transactionCookie)
	callbackRes := httptest.NewRecorder()
	handler.ServeHTTP(callbackRes, callback)
	if callbackRes.Code != http.StatusSeeOther || callbackRes.Header().Get("Location") != testOrigin+"?auth=oidc_error" ||
		authenticator.authenticates != 0 {
		t.Fatalf("expected state mismatch to fail before exchange, got %d location=%q authentications=%d",
			callbackRes.Code, callbackRes.Header().Get("Location"), authenticator.authenticates)
	}
	cleared := findCookie(t, callbackRes.Result().Cookies(), localOIDCTransactionName)
	if cleared.MaxAge != -1 || cleared.Value != "" {
		t.Fatalf("expected transaction cookie to be cleared, got %#v", cleared)
	}
}

func TestSAMLSPFlowPublishesMetadataCreatesJITSessionAndRejectsReplay(t *testing.T) {
	authenticator := &fakeHTTPSAMLAuthenticator{}
	flow, err := identity.NewSAMLFlow(authenticator, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := newGuardServerWithIdentity(t, nil, nil, flow)
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/bootstrap", nil)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	var status struct {
		SAMLEnabled bool `json:"samlEnabled"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil || !status.SAMLEnabled {
		t.Fatalf("expected bootstrap status to advertise SAML, status=%#v err=%v", status, err)
	}
	metadataRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/saml/metadata", nil)
	metadataResponse := httptest.NewRecorder()
	handler.ServeHTTP(metadataResponse, metadataRequest)
	if metadataResponse.Code != http.StatusOK || metadataResponse.Header().Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(metadataResponse.Header().Get("Content-Type"), "application/samlmetadata+xml") ||
		!strings.Contains(metadataResponse.Body.String(), "EntityDescriptor") {
		t.Fatalf("unexpected SAML metadata response %d %#v %s", metadataResponse.Code, metadataResponse.Header(), metadataResponse.Body.String())
	}
	beforeBootstrap := httptest.NewRequest(http.MethodGet, "/api/v1/auth/saml/start", nil)
	beforeBootstrap.Header.Set("Sec-Fetch-Site", "same-origin")
	beforeBootstrapResponse := httptest.NewRecorder()
	handler.ServeHTTP(beforeBootstrapResponse, beforeBootstrap)
	if beforeBootstrapResponse.Code != http.StatusConflict {
		t.Fatalf("expected bootstrap before SAML, got %d", beforeBootstrapResponse.Code)
	}
	bootstrapAdministrator(t, handler)
	startRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/saml/start", nil)
	startRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusSeeOther || !strings.HasPrefix(startResponse.Header().Get("Location"), "https://identity.example.test/sso?") ||
		authenticator.relayState == "" || len(startResponse.Result().Cookies()) != 0 {
		t.Fatalf("unexpected SAML start response %d %#v relay=%q", startResponse.Code, startResponse.Header(), authenticator.relayState)
	}
	form := url.Values{"RelayState": {authenticator.relayState}, "SAMLResponse": {"verified-assertion"}}
	acsRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/saml/acs", strings.NewReader(form.Encode()))
	acsRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	acsRequest.Header.Set("Origin", "https://identity.example.test")
	acsResponse := httptest.NewRecorder()
	handler.ServeHTTP(acsResponse, acsRequest)
	if acsResponse.Code != http.StatusSeeOther || acsResponse.Header().Get("Location") != testOrigin ||
		authenticator.authenticates != 1 || authenticator.expectedRequestID != "id-saml-request" {
		t.Fatalf("unexpected SAML ACS response %d location=%q authentications=%d request=%q body=%s",
			acsResponse.Code, acsResponse.Header().Get("Location"), authenticator.authenticates,
			authenticator.expectedRequestID, acsResponse.Body.String())
	}
	sessionCookie := findCookie(t, acsResponse.Result().Cookies(), localSessionName)
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), "saml@example.test") ||
		!strings.Contains(sessionResponse.Body.String(), "Administrator") {
		t.Fatalf("expected SAML JIT administrator session, got %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	replayRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/saml/acs", strings.NewReader(form.Encode()))
	replayRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusSeeOther || replayResponse.Header().Get("Location") != testOrigin+"?auth=saml_error" ||
		authenticator.authenticates != 1 {
		t.Fatalf("expected SAML replay to fail before assertion processing, got %d location=%q authentications=%d",
			replayResponse.Code, replayResponse.Header().Get("Location"), authenticator.authenticates)
	}
}

func TestExternalAuthStartTrustsConfiguredOriginAndLimitsLoopbackFallbackToDevelopment(t *testing.T) {
	production := &Server{allowedOrigin: "https://stewardmesh.example.test"}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	if production.trustedExternalAuthStart(request) {
		t.Fatal("a production reverse proxy must not make a headerless external-auth start request trusted")
	}
	request.Header.Set("Origin", "https://stewardmesh.example.test")
	if !production.trustedExternalAuthStart(request) {
		t.Fatal("the exact configured origin should be trusted")
	}
	request.Header.Del("Origin")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	development := &Server{allowedOrigin: testOrigin}
	if development.trustedExternalAuthStart(request) {
		t.Fatal("cross-site external-auth initiation must be rejected even on loopback")
	}
	request.Header.Del("Sec-Fetch-Site")
	if !development.trustedExternalAuthStart(request) {
		t.Fatal("headerless loopback development should remain available")
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
		OIDCEnabled               bool `json:"oidcEnabled"`
		SAMLEnabled               bool `json:"samlEnabled"`
	}
	if err := json.Unmarshal(statusRes.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Required || status.TokenRequired || status.OIDCEnabled || status.SAMLEnabled || status.MinimumPasswordCharacters != guard.MinimumPasswordCharacters {
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

func TestGuardAccessAPIManagesScopedAssignmentsAndProtectsLastAdministrator(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	listRequest := authenticatedRequest(http.MethodGet, "/api/v1/guard/access", nil, session)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected non-cacheable Guard access response, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var directory struct {
		Accounts             []guardAccountResponse        `json:"accounts"`
		Roles                []guardRoleResponse           `json:"roles"`
		PolicyBundles        []guardPolicyBundleResponse   `json:"policyBundles"`
		AvailablePermissions []guard.Permission            `json:"availablePermissions"`
		Assignments          []guardRoleAssignmentResponse `json:"assignments"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &directory); err != nil {
		t.Fatal(err)
	}
	if len(directory.Accounts) != 1 || len(directory.Roles) != 1 || len(directory.PolicyBundles) != 1 ||
		len(directory.AvailablePermissions) != len(guard.SupportedPermissions()) || len(directory.Assignments) != 1 ||
		!directory.Roles[0].Managed || directory.Roles[0].Source != guard.BuiltInRoleSource {
		t.Fatalf("unexpected Guard access directory %#v", directory)
	}
	rolePayload, err := json.Marshal(map[string]any{
		"name": "Asset steward", "description": "Maintains inventory records.",
		"permissions":     []string{"assets.read", "assets.write"},
		"policyBundleIds": []string{directory.PolicyBundles[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	createRoleRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/roles", bytes.NewReader(rolePayload), session)
	createRoleResponse := httptest.NewRecorder()
	handler.ServeHTTP(createRoleResponse, createRoleRequest)
	if createRoleResponse.Code != http.StatusCreated || createRoleResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected custom role creation, got %d: %s", createRoleResponse.Code, createRoleResponse.Body.String())
	}
	var createdRole guardRoleResponse
	if err := json.Unmarshal(createRoleResponse.Body.Bytes(), &createdRole); err != nil {
		t.Fatal(err)
	}
	if createdRole.Name != "Asset steward" || createdRole.Source != guard.LocalRoleSource || createdRole.Managed ||
		len(createdRole.Permissions) != 2 || len(createdRole.PolicyBundleIDs) != 1 {
		t.Fatalf("unexpected custom role %#v", createdRole)
	}
	duplicateRolePayload := bytes.Replace(rolePayload, []byte("Asset steward"), []byte("ASSET STEWARD"), 1)
	duplicateRoleRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/roles", bytes.NewReader(duplicateRolePayload), session)
	duplicateRoleResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRoleResponse, duplicateRoleRequest)
	if duplicateRoleResponse.Code != http.StatusConflict {
		t.Fatalf("expected duplicate role conflict, got %d: %s", duplicateRoleResponse.Code, duplicateRoleResponse.Body.String())
	}
	lastAdministratorRequest := authenticatedRequest(http.MethodDelete,
		"/api/v1/guard/role-assignments/"+directory.Assignments[0].ID, nil, session)
	lastAdministratorResponse := httptest.NewRecorder()
	handler.ServeHTTP(lastAdministratorResponse, lastAdministratorRequest)
	if lastAdministratorResponse.Code != http.StatusConflict || !strings.Contains(lastAdministratorResponse.Body.String(), "last_administrator") {
		t.Fatalf("expected last-administrator conflict, got %d: %s", lastAdministratorResponse.Code, lastAdministratorResponse.Body.String())
	}
	payload, err := json.Marshal(map[string]any{
		"accountId": directory.Accounts[0].ID,
		"roleId":    directory.Roles[0].ID,
		"scope": map[string]string{
			"kind": "site", "resourceId": "site-one",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	createRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/role-assignments", bytes.NewReader(payload), session)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected scoped role assignment creation, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created guardRoleAssignmentResponse
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Scope.Kind != guard.ScopeSite || created.Scope.ResourceID != "site-one" || created.Managed || created.Source != guard.LocalAssignmentSource {
		t.Fatalf("unexpected created assignment %#v", created)
	}
	duplicateRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/role-assignments", bytes.NewReader(payload), session)
	duplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("expected duplicate assignment conflict, got %d: %s", duplicateResponse.Code, duplicateResponse.Body.String())
	}
	deleteRequest := authenticatedRequest(http.MethodDelete, "/api/v1/guard/role-assignments/"+created.ID, nil, session)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected role assignment deletion, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestGuardResourceOwnershipAPIBlocksWritesUntilClaimed(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	assetPayload, _ := json.Marshal(map[string]string{"id": "imported-asset", "name": "Imported server", "kind": "server"})
	assetRequest := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(assetPayload), session)
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusCreated {
		t.Fatalf("expected asset creation, got %d: %s", assetResponse.Code, assetResponse.Body.String())
	}
	identity := createPeopleRecord[people.Identity](t, handler, session, "/api/v1/identities", map[string]any{
		"kind": "person", "displayName": "Imported Asset User", "email": "asset-user@example.test", "status": "active",
	})
	ownershipPayload, _ := json.Marshal(map[string]string{
		"resourceType": "asset", "resourceId": "imported-asset",
		"sourceSystemId": "inventory-source", "sourceRecordId": "upstream-record-one",
	})
	registerRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/resource-ownership", bytes.NewReader(ownershipPayload), session)
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, registerRequest)
	if registerResponse.Code != http.StatusCreated || !strings.Contains(registerResponse.Body.String(), `"writeLocked":true`) {
		t.Fatalf("expected ownership write lock creation, got %d: %s", registerResponse.Code, registerResponse.Body.String())
	}
	listRequest := authenticatedRequest(http.MethodGet, "/api/v1/guard/resource-ownership", nil, session)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "upstream-record-one") {
		t.Fatalf("expected ownership listing, got %d: %s", listResponse.Code, listResponse.Body.String())
	}
	assignmentPayload, _ := json.Marshal(map[string]string{
		"assigneeKind": "identity", "assigneeId": identity.ID, "role": "primary",
	})
	lockedRequest := authenticatedRequest(http.MethodPost, "/api/v1/assets/imported-asset/assignments", bytes.NewReader(assignmentPayload), session)
	lockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(lockedResponse, lockedRequest)
	if lockedResponse.Code != http.StatusLocked || !strings.Contains(lockedResponse.Body.String(), "ownership_locked") {
		t.Fatalf("expected imported resource write lock, got %d: %s", lockedResponse.Code, lockedResponse.Body.String())
	}
	claimRequest := authenticatedRequest(
		http.MethodPost, "/api/v1/guard/resource-ownership/asset/imported-asset/claim", nil, session,
	)
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claimRequest)
	if claimResponse.Code != http.StatusOK || !strings.Contains(claimResponse.Body.String(), `"writeLocked":false`) {
		t.Fatalf("expected ownership claim, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}
	allowedRequest := authenticatedRequest(http.MethodPost, "/api/v1/assets/imported-asset/assignments", bytes.NewReader(assignmentPayload), session)
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusCreated {
		t.Fatalf("expected claimed resource mutation, got %d: %s", allowedResponse.Code, allowedResponse.Body.String())
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
	modelPayload, _ := json.Marshal(map[string]any{
		"id": "model-1", "manufacturer": "Framework", "name": "Laptop 13", "modelNumber": "FW13",
		"kind": "laptop", "warrantyMonths": 36, "usefulLifeMonths": 48,
		"specifications": map[string]string{"CPU": "Ryzen", "Memory": "32 GB"},
		"sourceSystemId": "model-import", "sourceRecordId": "framework-fw13-v1",
	})
	modelReq := authenticatedRequest(http.MethodPost, "/api/v1/asset-models", bytes.NewReader(modelPayload), session)
	modelRes := httptest.NewRecorder()
	handler.ServeHTTP(modelRes, modelReq)
	if modelRes.Code != http.StatusCreated {
		t.Fatalf("expected model create 201, got %d: %s", modelRes.Code, modelRes.Body.String())
	}
	var model domain.AssetModel
	if err := json.Unmarshal(modelRes.Body.Bytes(), &model); err != nil {
		t.Fatal(err)
	}
	if model.OrganizationID != "example-org" || model.Revision != 1 || model.Status != "active" {
		t.Fatalf("unexpected created model %#v", model)
	}
	payload, _ := json.Marshal(map[string]any{
		"id": "asset-1", "modelId": model.ID, "name": "Lab server", "kind": "server", "assetTag": "LAB-001",
		"serialNumber": "SERIAL-001", "hostname": "lab-server.example.test", "status": "active",
	})
	createReq := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(payload), session)
	createRes := httptest.NewRecorder()
	handler.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}
	var created domain.Asset
	if err := json.Unmarshal(createRes.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.OrganizationID != "example-org" || created.ModelID != model.ID || created.Revision != 1 || created.Status != "active" {
		t.Fatalf("unexpected created asset %#v", created)
	}
	if created.ModelContext == nil || created.ModelContext.ModelRevision != 1 || created.ModelContext.Kind != "laptop" ||
		created.ModelContext.SourceSystemID != "model-import" || created.ModelContext.SourceRecordID != "framework-fw13-v1" ||
		len(created.ModelContext.Overrides) != 1 || created.ModelContext.Overrides[0] != "kind" {
		t.Fatalf("unexpected asset model context %#v", created.ModelContext)
	}
	modelListReq := authenticatedRequest(http.MethodGet, "/api/v1/asset-models?q=framework&kind=laptop", nil, session)
	modelListRes := httptest.NewRecorder()
	handler.ServeHTTP(modelListRes, modelListReq)
	if modelListRes.Code != http.StatusOK || !strings.Contains(modelListRes.Body.String(), `"instanceCount":1`) {
		t.Fatalf("expected model list with instance count, got %d: %s", modelListRes.Code, modelListRes.Body.String())
	}
	modelUpdatePayload, _ := json.Marshal(map[string]any{
		"manufacturer": model.Manufacturer, "name": "Laptop 13 v2", "modelNumber": model.ModelNumber,
		"kind": model.Kind, "warrantyMonths": 48, "usefulLifeMonths": model.UsefulLifeMonths,
		"specifications": map[string]string{"CPU": "Ryzen AI", "Memory": "32 GB"},
		"sourceSystemId": "model-import", "sourceRecordId": "framework-fw13-v2", "revision": model.Revision,
	})
	modelUpdateReq := authenticatedRequest(http.MethodPut, "/api/v1/asset-models/model-1", bytes.NewReader(modelUpdatePayload), session)
	modelUpdateRes := httptest.NewRecorder()
	handler.ServeHTTP(modelUpdateRes, modelUpdateReq)
	if modelUpdateRes.Code != http.StatusOK || !strings.Contains(modelUpdateRes.Body.String(), `"revision":2`) ||
		!strings.Contains(modelUpdateRes.Body.String(), `"sourceRecordId":"framework-fw13-v2"`) {
		t.Fatalf("expected complete model update, got %d: %s", modelUpdateRes.Code, modelUpdateRes.Body.String())
	}
	listReq := authenticatedRequest(http.MethodGet, "/api/v1/assets?q=lab-001&kind=server&status=active&modelId=model-1", nil, session)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), "asset-1") {
		t.Fatalf("expected filtered asset, got %d: %s", listRes.Code, listRes.Body.String())
	}
	inventoryReq := authenticatedRequest(http.MethodGet, "/api/v1/asset-models/model-1/inventory?status=active&deploymentContext=LAB-SERVER&groupBy=status&limit=10", nil, session)
	inventoryRes := httptest.NewRecorder()
	handler.ServeHTTP(inventoryRes, inventoryReq)
	if inventoryRes.Code != http.StatusOK || !strings.Contains(inventoryRes.Body.String(), `"totalCount":1`) ||
		!strings.Contains(inventoryRes.Body.String(), `"filteredCount":1`) || !strings.Contains(inventoryRes.Body.String(), `"key":"active","count":1`) ||
		!strings.Contains(inventoryRes.Body.String(), `"id":"asset-1"`) {
		t.Fatalf("expected filtered and grouped model inventory, got %d: %s", inventoryRes.Code, inventoryRes.Body.String())
	}
	invalidInventoryReq := authenticatedRequest(http.MethodGet, "/api/v1/asset-models/model-1/inventory?groupBy=building", nil, session)
	invalidInventoryRes := httptest.NewRecorder()
	handler.ServeHTTP(invalidInventoryRes, invalidInventoryReq)
	if invalidInventoryRes.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid model inventory grouping to fail, got %d: %s", invalidInventoryRes.Code, invalidInventoryRes.Body.String())
	}
	getReq := authenticatedRequest(http.MethodGet, "/api/v1/assets/asset-1", nil, session)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK || !strings.Contains(getRes.Body.String(), "lab-server.example.test") ||
		!strings.Contains(getRes.Body.String(), `"modelContext":{"manufacturer":"Framework"`) ||
		!strings.Contains(getRes.Body.String(), `"defaultsEffectiveAt"`) ||
		!strings.Contains(getRes.Body.String(), `"overrides":["kind"]`) {
		t.Fatalf("expected asset detail, got %d: %s", getRes.Code, getRes.Body.String())
	}
	updatePayload, _ := json.Marshal(map[string]any{
		"modelId": created.ModelID, "name": created.Name, "kind": created.Kind, "assetTag": created.AssetTag,
		"serialNumber": created.SerialNumber, "hostname": created.Hostname, "status": "retired",
		"revision": created.Revision, "lifecycleNote": "Replaced after lifecycle review",
	})
	updateReq := authenticatedRequest(http.MethodPut, "/api/v1/assets/asset-1", bytes.NewReader(updatePayload), session)
	updateRes := httptest.NewRecorder()
	handler.ServeHTTP(updateRes, updateReq)
	if updateRes.Code != http.StatusOK || !strings.Contains(updateRes.Body.String(), `"revision":2`) {
		t.Fatalf("expected asset update, got %d: %s", updateRes.Code, updateRes.Body.String())
	}
	lifecycleReq := authenticatedRequest(http.MethodGet, "/api/v1/assets/asset-1/lifecycle", nil, session)
	lifecycleRes := httptest.NewRecorder()
	handler.ServeHTTP(lifecycleRes, lifecycleReq)
	if lifecycleRes.Code != http.StatusOK || !strings.Contains(lifecycleRes.Body.String(), "Replaced after lifecycle review") {
		t.Fatalf("expected lifecycle history, got %d: %s", lifecycleRes.Code, lifecycleRes.Body.String())
	}
	staleReq := authenticatedRequest(http.MethodPut, "/api/v1/assets/asset-1", bytes.NewReader(updatePayload), session)
	staleRes := httptest.NewRecorder()
	handler.ServeHTTP(staleRes, staleReq)
	if staleRes.Code != http.StatusConflict {
		t.Fatalf("expected stale revision conflict, got %d: %s", staleRes.Code, staleRes.Body.String())
	}
	retireReq := authenticatedRequest(http.MethodPost, "/api/v1/asset-models/model-1/retire?revision=2", nil, session)
	retireRes := httptest.NewRecorder()
	handler.ServeHTTP(retireRes, retireReq)
	if retireRes.Code != http.StatusOK || !strings.Contains(retireRes.Body.String(), `"status":"retired"`) {
		t.Fatalf("expected model retirement, got %d: %s", retireRes.Code, retireRes.Body.String())
	}
	postRetirementPayload, _ := json.Marshal(map[string]any{
		"modelId": created.ModelID, "name": "Lab server maintained", "kind": created.Kind,
		"assetTag": created.AssetTag, "serialNumber": created.SerialNumber, "hostname": created.Hostname,
		"status": "retired", "revision": 2,
	})
	postRetirementReq := authenticatedRequest(http.MethodPut, "/api/v1/assets/asset-1", bytes.NewReader(postRetirementPayload), session)
	postRetirementRes := httptest.NewRecorder()
	handler.ServeHTTP(postRetirementRes, postRetirementReq)
	if postRetirementRes.Code != http.StatusOK || !strings.Contains(postRetirementRes.Body.String(), `"revision":3`) ||
		!strings.Contains(postRetirementRes.Body.String(), `"modelRevision":1`) {
		t.Fatalf("expected linked asset maintenance after model retirement, got %d: %s", postRetirementRes.Code, postRetirementRes.Body.String())
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

func TestResolveModelAndBulkCreateAssets(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	model := createPeopleRecord[domain.AssetModel](t, handler, session, "/api/v1/asset-models", map[string]any{
		"id": "bulk-model", "manufacturer": "Framework", "name": "Laptop 13", "modelNumber": "FW13", "kind": "laptop",
	})
	resolvePath := "/api/v1/asset-models/resolve?manufacturer=" + url.QueryEscape(" framework ") +
		"&name=" + url.QueryEscape("LAPTOP 13") + "&modelNumber=" + url.QueryEscape("fw13")
	resolveRequest := authenticatedRequest(http.MethodGet, resolvePath, nil, session)
	resolveResponse := httptest.NewRecorder()
	handler.ServeHTTP(resolveResponse, resolveRequest)
	if resolveResponse.Code != http.StatusOK || !strings.Contains(resolveResponse.Body.String(), `"id":"bulk-model"`) {
		t.Fatalf("expected exact model resolution, got %d: %s", resolveResponse.Code, resolveResponse.Body.String())
	}
	payload, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"id": "bulk-http-one", "name": "Bulk laptop one", "assetTag": "HTTP-BULK-001", "serialNumber": "HTTP-SERIAL-001", "status": "active"},
		{"id": "bulk-http-two", "name": "Bulk laptop two", "assetTag": "HTTP-BULK-002", "serialNumber": "HTTP-SERIAL-002"},
	}})
	bulkPath := "/api/v1/asset-models/" + model.ID + "/assets/bulk"
	bulkRequest := authenticatedRequest(http.MethodPost, bulkPath, bytes.NewReader(payload), session)
	bulkResponse := httptest.NewRecorder()
	handler.ServeHTTP(bulkResponse, bulkRequest)
	if bulkResponse.Code != http.StatusCreated {
		t.Fatalf("expected bulk create 201, got %d: %s", bulkResponse.Code, bulkResponse.Body.String())
	}
	var result atlas.BulkCreateAssetsResult
	if err := json.Unmarshal(bulkResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].ModelID != model.ID || result.Items[0].Kind != "laptop" || result.Items[1].Status != "draft" {
		t.Fatalf("unexpected bulk response %#v", result)
	}
	conflictPayload, _ := json.Marshal(map[string]any{"items": []map[string]any{
		{"id": "bulk-http-three", "name": "Should roll back", "assetTag": "HTTP-BULK-003"},
		{"id": "bulk-http-four", "name": "Existing identity", "assetTag": "http-bulk-001"},
	}})
	conflictRequest := authenticatedRequest(http.MethodPost, bulkPath, bytes.NewReader(conflictPayload), session)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("expected atomic bulk conflict, got %d: %s", conflictResponse.Code, conflictResponse.Body.String())
	}
	missingRequest := authenticatedRequest(http.MethodGet, "/api/v1/assets/bulk-http-three", nil, session)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected failed batch to leave no partial asset, got %d: %s", missingResponse.Code, missingResponse.Body.String())
	}
	missingCSRF := authenticatedRequest(http.MethodPost, bulkPath, bytes.NewReader(payload), session)
	missingCSRF.Header.Del(csrfHeader)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("expected bulk creation to require CSRF, got %d", missingCSRFResponse.Code)
	}
}

func TestAtlasCodesIdentifierLifecycleIsRetrySafeLockedAndConflictRedacted(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	for _, asset := range []map[string]any{
		{"id": "codes-asset-one", "name": "Codes asset one", "kind": "server", "status": "active"},
		{"id": "codes-asset-two", "name": "Codes asset two", "kind": "server", "status": "active"},
	} {
		createPeopleRecord[domain.Asset](t, handler, session, "/api/v1/assets", asset)
	}

	createPayload, _ := json.Marshal(map[string]any{
		"id": "identifier-one", "symbology": "code128", "value": "Case-CODE-001",
		"displayValue": "Rack code 001", "primary": true,
	})
	createPath := "/api/v1/assets/codes-asset-one/identifiers"
	createRequest := authenticatedRequest(http.MethodPost, createPath, bytes.NewReader(createPayload), session)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected identifier creation, got %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Identifier atlascodes.Identifier `json:"identifier"`
		Created    bool                  `json:"created"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.Identifier.ID != "identifier-one" || created.Identifier.NormalizedValue != "Case-CODE-001" ||
		created.Identifier.Source != atlascodes.SourceUserEntered || created.Identifier.Status != atlascodes.StatusActive ||
		created.Identifier.Revision != 1 {
		t.Fatalf("unexpected created identifier %#v", created)
	}

	retryRequest := authenticatedRequest(http.MethodPost, createPath, bytes.NewReader(createPayload), session)
	retryResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK || !strings.Contains(retryResponse.Body.String(), `"created":false`) {
		t.Fatalf("expected safe create retry, got %d: %s", retryResponse.Code, retryResponse.Body.String())
	}

	resolvePayload, _ := json.Marshal(map[string]string{"symbology": "code128", "value": "Case-CODE-001"})
	resolveRequest := authenticatedRequest(http.MethodPost, "/api/v1/asset-identifiers/resolve", bytes.NewReader(resolvePayload), session)
	resolveResponse := httptest.NewRecorder()
	handler.ServeHTTP(resolveResponse, resolveRequest)
	if resolveResponse.Code != http.StatusOK || !strings.Contains(resolveResponse.Body.String(), `"id":"identifier-one"`) {
		t.Fatalf("expected case-preserving identifier resolution, got %d: %s", resolveResponse.Code, resolveResponse.Body.String())
	}

	duplicatePayload, _ := json.Marshal(map[string]any{
		"id": "identifier-duplicate", "symbology": "code128", "value": "Case-CODE-001",
		"source": "user_entered",
	})
	duplicateRequest := authenticatedRequest(http.MethodPost, "/api/v1/assets/codes-asset-two/identifiers", bytes.NewReader(duplicatePayload), session)
	duplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != http.StatusConflict || strings.Contains(duplicateResponse.Body.String(), "Case-CODE-001") {
		t.Fatalf("expected redacted active-claim conflict, got %d: %s", duplicateResponse.Code, duplicateResponse.Body.String())
	}

	ownershipPayload, _ := json.Marshal(map[string]string{
		"resourceType": "asset", "resourceId": "codes-asset-one",
		"sourceSystemId": "external-inventory", "sourceRecordId": "private-source-record",
	})
	registerRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/resource-ownership", bytes.NewReader(ownershipPayload), session)
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, registerRequest)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("expected imported asset lock, got %d: %s", registerResponse.Code, registerResponse.Body.String())
	}
	lockedRequest := authenticatedRequest(
		http.MethodPost,
		"/api/v1/assets/codes-asset-one/identifiers/identifier-one/replace",
		strings.NewReader("not-json"),
		session,
	)
	lockedResponse := httptest.NewRecorder()
	handler.ServeHTTP(lockedResponse, lockedRequest)
	if lockedResponse.Code != http.StatusLocked || !strings.Contains(lockedResponse.Body.String(), "ownership_locked") {
		t.Fatalf("expected ownership lock before payload decoding, got %d: %s", lockedResponse.Code, lockedResponse.Body.String())
	}
	claimRequest := authenticatedRequest(http.MethodPost, "/api/v1/guard/resource-ownership/asset/codes-asset-one/claim", nil, session)
	claimResponse := httptest.NewRecorder()
	handler.ServeHTTP(claimResponse, claimRequest)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("expected local ownership claim, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}

	replacePayload, _ := json.Marshal(map[string]any{
		"replacementId": "identifier-two", "symbology": "qr", "value": "Case-QR-002",
		"displayValue": "Rack QR 002", "source": "generated", "revision": 1,
	})
	replacePath := "/api/v1/assets/codes-asset-one/identifiers/identifier-one/replace"
	replaceRequest := authenticatedRequest(http.MethodPost, replacePath, bytes.NewReader(replacePayload), session)
	replaceResponse := httptest.NewRecorder()
	handler.ServeHTTP(replaceResponse, replaceRequest)
	if replaceResponse.Code != http.StatusOK || !strings.Contains(replaceResponse.Body.String(), `"changed":true`) ||
		!strings.Contains(replaceResponse.Body.String(), `"id":"identifier-two"`) {
		t.Fatalf("expected identifier replacement, got %d: %s", replaceResponse.Code, replaceResponse.Body.String())
	}

	listRequest := authenticatedRequest(http.MethodGet, createPath, nil, session)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"status":"replaced"`) ||
		!strings.Contains(listResponse.Body.String(), `"supersedesId":"identifier-one"`) {
		t.Fatalf("expected preserved replacement history, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	deactivatePayload, _ := json.Marshal(map[string]int64{"revision": 1})
	deactivatePath := "/api/v1/assets/codes-asset-one/identifiers/identifier-two/deactivate"
	deactivateRequest := authenticatedRequest(http.MethodPost, deactivatePath, bytes.NewReader(deactivatePayload), session)
	deactivateResponse := httptest.NewRecorder()
	handler.ServeHTTP(deactivateResponse, deactivateRequest)
	if deactivateResponse.Code != http.StatusOK || !strings.Contains(deactivateResponse.Body.String(), `"status":"deactivated"`) ||
		!strings.Contains(deactivateResponse.Body.String(), `"revision":2`) {
		t.Fatalf("expected identifier deactivation, got %d: %s", deactivateResponse.Code, deactivateResponse.Body.String())
	}
	retryDeactivateRequest := authenticatedRequest(http.MethodPost, deactivatePath, bytes.NewReader(deactivatePayload), session)
	retryDeactivateResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryDeactivateResponse, retryDeactivateRequest)
	if retryDeactivateResponse.Code != http.StatusOK || !strings.Contains(retryDeactivateResponse.Body.String(), `"changed":false`) {
		t.Fatalf("expected safe deactivation retry, got %d: %s", retryDeactivateResponse.Code, retryDeactivateResponse.Body.String())
	}

	missingCSRF := authenticatedRequest(http.MethodPost, "/api/v1/assets/codes-asset-two/identifiers", bytes.NewReader(duplicatePayload), session)
	missingCSRF.Header.Del(csrfHeader)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("expected identifier writes to require CSRF, got %d", missingCSRFResponse.Code)
	}
}

func TestAtlasCodesResolveHonorsScopedAssetReadWithoutDisclosingDeniedMatches(t *testing.T) {
	handler, guardService := newGuardServerWithIdentityAndGuard(t, nil, nil, nil)
	administrator := bootstrapAdministrator(t, handler)
	for _, asset := range []map[string]any{
		{"id": "scoped-visible-asset", "name": "Scoped visible asset", "kind": "server", "status": "active"},
		{"id": "scoped-hidden-asset", "name": "Scoped hidden asset", "kind": "server", "status": "active"},
	} {
		createPeopleRecord[domain.Asset](t, handler, administrator, "/api/v1/assets", asset)
	}
	for _, association := range []struct {
		assetID string
		id      string
		value   string
	}{
		{assetID: "scoped-visible-asset", id: "scoped-visible-code", value: "SCOPED-VISIBLE-001"},
		{assetID: "scoped-hidden-asset", id: "scoped-hidden-code", value: "SCOPED-HIDDEN-001"},
	} {
		payload, _ := json.Marshal(map[string]any{
			"id": association.id, "symbology": "code128", "value": association.value, "source": "user_entered",
		})
		request := authenticatedRequest(http.MethodPost, "/api/v1/assets/"+association.assetID+"/identifiers", bytes.NewReader(payload), administrator)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("expected identifier setup for %s, got %d: %s", association.assetID, response.Code, response.Body.String())
		}
	}

	ctx := context.Background()
	administratorAuthentication, err := guardService.AuthenticateSession(ctx, administrator.cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	scopedCredentials, err := guardService.LoginOIDC(ctx, identity.OIDCPrincipal{
		Issuer: "https://identity.example.test/scoped", Subject: "asset-reader", Email: "asset-reader@example.test",
		EmailVerified: true, DisplayName: "Scoped Asset Reader",
	})
	if err != nil {
		t.Fatal(err)
	}
	role, err := guardService.CreateRole(ctx, administratorAuthentication, guard.CreateRoleInput{
		Name: "Scoped asset reader", Permissions: []guard.Permission{guard.PermissionAssetsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guardService.AssignRole(ctx, administratorAuthentication, guard.RoleAssignmentInput{
		AccountID: scopedCredentials.Authentication.Principal.Subject,
		RoleID:    role.ID, ScopeKind: guard.ScopeResource, ResourceID: "scoped-visible-asset",
	}); err != nil {
		t.Fatal(err)
	}
	scopedSession := testSession{
		cookie:    &http.Cookie{Name: localSessionName, Value: scopedCredentials.Token},
		csrfToken: scopedCredentials.CSRFToken,
	}
	sessionRequest := authenticatedRequest(http.MethodGet, "/api/v1/auth/session", nil, scopedSession)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), `"permission":"assets.read"`) ||
		!strings.Contains(sessionResponse.Body.String(), `"kind":"resource"`) ||
		!strings.Contains(sessionResponse.Body.String(), `"resourceId":"scoped-visible-asset"`) ||
		strings.Contains(sessionResponse.Body.String(), `"permissions":["assets.read"]`) {
		t.Fatalf("expected scoped session hints without an organization-wide permission hint, got %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}
	listRequest := authenticatedRequest(http.MethodGet, "/api/v1/assets", nil, scopedSession)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusForbidden {
		t.Fatalf("expected scoped UI hints not to widen the organization asset list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	resolve := func(value string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"symbology": "code128", "value": value})
		request := authenticatedRequest(http.MethodPost, "/api/v1/asset-identifiers/resolve", bytes.NewReader(payload), scopedSession)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	visible := resolve("SCOPED-VISIBLE-001")
	if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), `"assetId":"scoped-visible-asset"`) {
		t.Fatalf("expected resource-scoped resolution, got %d: %s", visible.Code, visible.Body.String())
	}
	visibleAssetRequest := authenticatedRequest(http.MethodGet, "/api/v1/assets/scoped-visible-asset", nil, scopedSession)
	visibleAssetResponse := httptest.NewRecorder()
	handler.ServeHTTP(visibleAssetResponse, visibleAssetRequest)
	if visibleAssetResponse.Code != http.StatusOK || !strings.Contains(visibleAssetResponse.Body.String(), `"id":"scoped-visible-asset"`) {
		t.Fatalf("expected resource-scoped asset read after resolution, got %d: %s", visibleAssetResponse.Code, visibleAssetResponse.Body.String())
	}
	hiddenAssetRequest := authenticatedRequest(http.MethodGet, "/api/v1/assets/scoped-hidden-asset", nil, scopedSession)
	hiddenAssetResponse := httptest.NewRecorder()
	handler.ServeHTTP(hiddenAssetResponse, hiddenAssetRequest)
	if hiddenAssetResponse.Code != http.StatusNotFound || !strings.Contains(hiddenAssetResponse.Body.String(), `"code":"not_found"`) ||
		strings.Contains(hiddenAssetResponse.Body.String(), "scoped-hidden-asset") {
		t.Fatalf("expected redacted not-found for unauthorized asset, got %d: %s", hiddenAssetResponse.Code, hiddenAssetResponse.Body.String())
	}
	for _, value := range []string{"SCOPED-HIDDEN-001", "SCOPED-UNKNOWN-001"} {
		response := resolve(value)
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"not_found"`) ||
			strings.Contains(response.Body.String(), "scoped-hidden-asset") || strings.Contains(response.Body.String(), value) {
			t.Fatalf("expected uniform redacted not-found for %q, got %d: %s", value, response.Code, response.Body.String())
		}
	}
}

func TestPeopleAndThreadsCollectionsRequireDirectoryGrants(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)
	for _, path := range []string{
		"/api/v1/buildings", "/api/v1/rooms", "/api/v1/departments", "/api/v1/users", "/api/v1/tags", "/api/v1/goals",
	} {
		req := authenticatedRequest(http.MethodGet, path, nil, session)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", path, res.Code, res.Body.String())
		}
	}
}

func TestThreadsHierarchyProvenanceAndGoalLinks(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)

	assetPayload, _ := json.Marshal(map[string]string{"id": "asset-threads-1", "name": "Primary server", "kind": "server"})
	assetRequest := authenticatedRequest(http.MethodPost, "/api/v1/assets", bytes.NewReader(assetPayload), session)
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusCreated {
		t.Fatalf("expected asset creation, got %d: %s", assetResponse.Code, assetResponse.Body.String())
	}

	parent := createPeopleRecord[threads.Tag](t, handler, session, "/api/v1/tags", map[string]any{
		"id": "governance", "name": "Governance", "inheritByDefault": true,
	})
	child := createPeopleRecord[threads.Tag](t, handler, session, "/api/v1/tags", map[string]any{
		"id": "security", "name": "Security", "parentId": parent.ID, "inheritByDefault": false,
	})
	if child.ParentID != parent.ID {
		t.Fatalf("unexpected tag hierarchy %#v", child)
	}

	includePayload, _ := json.Marshal(map[string]any{"mode": "include", "revision": 0})
	includeRequest := authenticatedRequest(http.MethodPut, "/api/v1/threads/asset/asset-threads-1/tags/security", bytes.NewReader(includePayload), session)
	includeResponse := httptest.NewRecorder()
	handler.ServeHTTP(includeResponse, includeRequest)
	if includeResponse.Code != http.StatusOK || !strings.Contains(includeResponse.Body.String(), `"revision":1`) {
		t.Fatalf("expected explicit tag rule, got %d: %s", includeResponse.Code, includeResponse.Body.String())
	}

	evaluateRequest := authenticatedRequest(http.MethodGet, "/api/v1/threads/asset/asset-threads-1/tags", nil, session)
	evaluateResponse := httptest.NewRecorder()
	handler.ServeHTTP(evaluateResponse, evaluateRequest)
	if evaluateResponse.Code != http.StatusOK || !strings.Contains(evaluateResponse.Body.String(), `"state":"explicit"`) ||
		!strings.Contains(evaluateResponse.Body.String(), `"state":"inherited"`) ||
		!strings.Contains(evaluateResponse.Body.String(), `"sourceTagId":"security"`) {
		t.Fatalf("expected explicit and inherited provenance, got %d: %s", evaluateResponse.Code, evaluateResponse.Body.String())
	}

	suppressPayload, _ := json.Marshal(map[string]any{"mode": "suppress", "revision": 0})
	suppressRequest := authenticatedRequest(http.MethodPut, "/api/v1/threads/asset/asset-threads-1/tags/governance", bytes.NewReader(suppressPayload), session)
	suppressResponse := httptest.NewRecorder()
	handler.ServeHTTP(suppressResponse, suppressRequest)
	if suppressResponse.Code != http.StatusOK {
		t.Fatalf("expected suppression rule, got %d: %s", suppressResponse.Code, suppressResponse.Body.String())
	}

	cyclePayload, _ := json.Marshal(map[string]any{
		"name": parent.Name, "parentId": child.ID, "inheritByDefault": true, "revision": parent.Revision,
	})
	cycleRequest := authenticatedRequest(http.MethodPut, "/api/v1/tags/governance", bytes.NewReader(cyclePayload), session)
	cycleResponse := httptest.NewRecorder()
	handler.ServeHTTP(cycleResponse, cycleRequest)
	if cycleResponse.Code != http.StatusConflict || !strings.Contains(cycleResponse.Body.String(), "hierarchy_cycle") {
		t.Fatalf("expected cycle prevention, got %d: %s", cycleResponse.Code, cycleResponse.Body.String())
	}

	goal := createPeopleRecord[threads.Goal](t, handler, session, "/api/v1/goals", map[string]any{
		"id": "reduce-risk", "name": "Reduce operational risk", "description": "Lower material service exposure.",
	})
	linkRequest := authenticatedRequest(http.MethodPut, "/api/v1/threads/asset/asset-threads-1/goals/"+goal.ID, nil, session)
	linkResponse := httptest.NewRecorder()
	handler.ServeHTTP(linkResponse, linkRequest)
	if linkResponse.Code != http.StatusOK || !strings.Contains(linkResponse.Body.String(), `"goalId":"reduce-risk"`) {
		t.Fatalf("expected goal link, got %d: %s", linkResponse.Code, linkResponse.Body.String())
	}
	listRequest := authenticatedRequest(http.MethodGet, "/api/v1/threads/asset/asset-threads-1/goals", nil, session)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"goalId":"reduce-risk"`) {
		t.Fatalf("expected goal relationship list, got %d: %s", listResponse.Code, listResponse.Body.String())
	}

	missingCSRF := authenticatedRequest(http.MethodPost, "/api/v1/tags", bytes.NewBufferString(`{"name":"Denied","inheritByDefault":false}`), session)
	missingCSRF.Header.Del(csrfHeader)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || !strings.Contains(missingCSRFResponse.Body.String(), "csrf_failed") {
		t.Fatalf("expected CSRF protection, got %d: %s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
	}
}

func TestPeopleDirectoryCreateSearchAndAssignmentHistory(t *testing.T) {
	handler := newGuardServer(t)
	session := bootstrapAdministrator(t, handler)

	site := createPeopleRecord[people.Site](t, handler, session, "/api/v1/sites", map[string]any{
		"name": "Main Campus",
		"address": map[string]string{
			"line1": "100 Steward Way", "city": "Madison", "region": "WI",
			"postalCode": "53703", "country": "US",
		},
	})
	if site.Address.Line1 != "100 Steward Way" || site.Address.Country != "US" {
		t.Fatalf("unexpected structured site address %#v", site.Address)
	}
	building := createPeopleRecord[people.Building](t, handler, session, "/api/v1/buildings", map[string]any{
		"siteId": site.ID, "name": "Steward Hall",
	})
	room := createPeopleRecord[people.Room](t, handler, session, "/api/v1/rooms", map[string]any{
		"siteId": site.ID, "buildingId": building.ID, "number": "101", "name": "Receiving",
	})
	if building.SiteID != site.ID || room.BuildingID != building.ID || room.SiteID != site.ID {
		t.Fatalf("unexpected location hierarchy %#v %#v", building, room)
	}
	for path, expectedID := range map[string]string{
		"/api/v1/buildings?siteId=" + site.ID:                            building.ID,
		"/api/v1/rooms?siteId=" + site.ID + "&buildingId=" + building.ID: room.ID,
	} {
		req := authenticatedRequest(http.MethodGet, path, nil, session)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(expectedID)) {
			t.Fatalf("expected scoped location %s from %s, got %d: %s", expectedID, path, res.Code, res.Body.String())
		}
	}
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

func TestLoginReturnsSafeServiceUnavailableWhenSharedProtectionFails(t *testing.T) {
	handler := newGuardServerWithLimiter(t, unavailableHTTPAttemptLimiter{})
	payload, _ := json.Marshal(map[string]string{
		"username": "administrator",
		"password": "correct horse battery staple",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "authentication_unavailable") ||
		strings.Contains(res.Body.String(), "unexpected EOF") {
		t.Fatalf("expected safe unavailable response, got %s", res.Body.String())
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

func TestDirectoryImportHTTPRequiresIntegrationPermissionsCSRFAndNoStore(t *testing.T) {
	connector := &httpDirectoryConnector{page: directoryexpansion.Page{CompleteSnapshot: true, Records: []directoryexpansion.Record{{
		SourceRecordID: "employee-1", Kind: directoryexpansion.RecordIdentity, IdentityKind: "person",
		DisplayName: "Ada Example", Email: "ada@example.test", Status: "active",
	}}}}
	handler, _, connector := newGuardServerWithDirectory(t, nil, nil, nil, connector)
	session := bootstrapAdministrator(t, handler)

	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/directory-imports", nil)
	unauthenticatedRes := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRes, unauthenticated)
	if unauthenticatedRes.Code != http.StatusUnauthorized || unauthenticatedRes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected protected no-store response, got %d %#v", unauthenticatedRes.Code, unauthenticatedRes.Header())
	}

	missingCSRF := authenticatedRequest(http.MethodPost, "/api/v1/directory-imports/preview", strings.NewReader(`{"sourceSystemId":"hr-primary"}`), session)
	missingCSRF.Header.Del(csrfHeader)
	missingCSRF.Header.Set(idempotencyHeader, "http-preview-key-1")
	missingCSRFRes := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFRes, missingCSRF)
	if missingCSRFRes.Code != http.StatusForbidden || connector.calls != 0 || missingCSRFRes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("CSRF failure reached connector: %d calls=%d headers=%#v", missingCSRFRes.Code, connector.calls, missingCSRFRes.Header())
	}

	previewRequest := authenticatedRequest(http.MethodPost, "/api/v1/directory-imports/preview", strings.NewReader(`{"sourceSystemId":"hr-primary"}`), session)
	previewRequest.Header.Set(idempotencyHeader, "http-preview-key-1")
	previewRes := httptest.NewRecorder()
	handler.ServeHTTP(previewRes, previewRequest)
	if previewRes.Code != http.StatusCreated || previewRes.Header().Get("Cache-Control") != "no-store" || connector.calls != 1 {
		t.Fatalf("unexpected preview %d headers=%#v body=%s calls=%d", previewRes.Code, previewRes.Header(), previewRes.Body.String(), connector.calls)
	}
	var preview directoryexpansion.OperationResult
	if err := json.Unmarshal(previewRes.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Batch.ID == "" || preview.Batch.Status != directoryexpansion.BatchPreviewed {
		t.Fatalf("unexpected preview %#v", preview)
	}

	applyRequest := authenticatedRequest(http.MethodPost, "/api/v1/directory-imports/"+preview.Batch.ID+"/apply", nil, session)
	applyRequest.Header.Set(idempotencyHeader, "http-apply-key-001")
	applyRes := httptest.NewRecorder()
	handler.ServeHTTP(applyRes, applyRequest)
	if applyRes.Code != http.StatusOK || applyRes.Header().Get("Cache-Control") != "no-store" || !strings.Contains(applyRes.Body.String(), `"status":"applied"`) {
		t.Fatalf("unexpected apply %d headers=%#v body=%s", applyRes.Code, applyRes.Header(), applyRes.Body.String())
	}

	detailRequest := authenticatedRequest(http.MethodGet, "/api/v1/directory-imports/"+preview.Batch.ID, nil, session)
	detailRes := httptest.NewRecorder()
	handler.ServeHTTP(detailRes, detailRequest)
	if detailRes.Code != http.StatusOK || detailRes.Header().Get("Cache-Control") != "no-store" || strings.Contains(detailRes.Body.String(), "http-preview-key") || strings.Contains(detailRes.Body.String(), "account:") {
		t.Fatalf("unexpected safe detail %d headers=%#v body=%s", detailRes.Code, detailRes.Header(), detailRes.Body.String())
	}
}

func TestDirectoryImportHTTPRejectsMissingIdempotencyAndUnknownSource(t *testing.T) {
	handler, _, _ := newGuardServerWithDirectory(t, nil, nil, nil, nil)
	session := bootstrapAdministrator(t, handler)
	missingKey := authenticatedRequest(http.MethodPost, "/api/v1/directory-imports/preview", strings.NewReader(`{"sourceSystemId":"hr-primary"}`), session)
	missingKeyRes := httptest.NewRecorder()
	handler.ServeHTTP(missingKeyRes, missingKey)
	if missingKeyRes.Code != http.StatusBadRequest || !strings.Contains(missingKeyRes.Body.String(), "validation_failed") {
		t.Fatalf("unexpected missing key response %d %s", missingKeyRes.Code, missingKeyRes.Body.String())
	}
	unknown := authenticatedRequest(http.MethodPost, "/api/v1/directory-imports/preview", strings.NewReader(`{"sourceSystemId":"hr-primary"}`), session)
	unknown.Header.Set(idempotencyHeader, "http-preview-key-2")
	unknownRes := httptest.NewRecorder()
	handler.ServeHTTP(unknownRes, unknown)
	if unknownRes.Code != http.StatusNotFound || !strings.Contains(unknownRes.Body.String(), "source_system_not_found") {
		t.Fatalf("unexpected unknown source response %d %s", unknownRes.Code, unknownRes.Body.String())
	}
}

func TestDirectoryImportHTTPRejectsSessionWithoutIntegrationPermission(t *testing.T) {
	connector := &httpDirectoryConnector{page: directoryexpansion.Page{CompleteSnapshot: true}}
	handler, guardService, _ := newGuardServerWithDirectory(t, nil, nil, nil, connector)
	administrator := bootstrapAdministrator(t, handler)

	request := authenticatedRequest(http.MethodGet, "/api/v1/directory-imports", nil, administrator)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("administrator integration permission missing: %d %s", response.Code, response.Body.String())
	}

	external, err := guardService.LoginOIDC(context.Background(), identity.OIDCPrincipal{
		Issuer: "https://issuer.example.test", Subject: "directory-reader", Email: "reader@example.test", DisplayName: "Reader",
	})
	if err != nil {
		t.Fatal(err)
	}
	deniedSession := testSession{cookie: &http.Cookie{Name: localSessionName, Value: external.Token}, csrfToken: external.CSRFToken}
	denied := authenticatedRequest(http.MethodGet, "/api/v1/directory-imports", nil, deniedSession)
	deniedRes := httptest.NewRecorder()
	handler.ServeHTTP(deniedRes, denied)
	if deniedRes.Code != http.StatusForbidden || !strings.Contains(deniedRes.Body.String(), "permission_denied") {
		t.Fatalf("expected session without integrations.read to fail, got %d %s", deniedRes.Code, deniedRes.Body.String())
	}
}

func newGuardServer(t *testing.T) http.Handler {
	return newGuardServerWithLimiter(t, nil)
}

func newGuardServerWithLimiter(t *testing.T, limiter guard.AttemptLimiter) http.Handler {
	return newGuardServerWithDependencies(t, limiter, nil)
}

func newGuardServerWithDependencies(t *testing.T, limiter guard.AttemptLimiter, oidcFlow *identity.OIDCFlow) http.Handler {
	return newGuardServerWithIdentity(t, limiter, oidcFlow, nil)
}

func newGuardServerWithIdentity(t *testing.T, limiter guard.AttemptLimiter, oidcFlow *identity.OIDCFlow, samlFlow *identity.SAMLFlow) http.Handler {
	handler, _ := newGuardServerWithIdentityAndGuard(t, limiter, oidcFlow, samlFlow)
	return handler
}

func newGuardServerWithIdentityAndGuard(t *testing.T, limiter guard.AttemptLimiter, oidcFlow *identity.OIDCFlow, samlFlow *identity.SAMLFlow) (http.Handler, *guard.Service) {
	handler, service, _ := newGuardServerWithDirectory(t, limiter, oidcFlow, samlFlow, nil)
	return handler, service
}

func newGuardServerWithDirectory(t *testing.T, limiter guard.AttemptLimiter, oidcFlow *identity.OIDCFlow, samlFlow *identity.SAMLFlow, connector directoryexpansion.Connector) (http.Handler, *guard.Service, *httpDirectoryConnector) {
	t.Helper()
	organization, err := bootstrap.NewOrganization("example-org", "Example Organization")
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewMemoryGuardStore()
	service, err := guard.NewService(store, httpTestHasher{}, foundation.NopAuditor{}, limiter, guard.ServiceConfig{
		OrganizationID: organization.ID,
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	assetStore := repository.NewMemoryAtlasStore()
	atlasService, err := atlas.NewService(assetStore, allowHTTPAssetReferences{}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: organization.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	atlasCodesService, err := atlascodes.NewService(repository.NewMemoryAtlasCodesStore(), atlasService, foundation.NopAuditor{}, atlascodes.ServiceConfig{
		OrganizationID: organization.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	peopleStore := repository.NewMemoryPeopleStore()
	peopleService, err := people.NewService(peopleStore, atlasService, foundation.NopAuditor{}, people.ServiceConfig{
		OrganizationID: organization.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := directoryexpansion.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var concreteConnector *httpDirectoryConnector
	if connector != nil {
		if err := registry.Register(connector); err != nil {
			t.Fatal(err)
		}
		concreteConnector, _ = connector.(*httpDirectoryConnector)
	}
	directoryTarget, err := directoryexpansion.NewPeopleTarget(peopleStore, service, nil)
	if err != nil {
		t.Fatal(err)
	}
	directoryService, err := directoryexpansion.NewService(repository.NewMemoryDirectoryImportStore(), directoryTarget, foundation.NopAuditor{}, registry, directoryexpansion.ServiceConfig{OrganizationID: organization.ID})
	if err != nil {
		t.Fatal(err)
	}
	threadsService, err := threads.NewService(repository.NewMemoryThreadsStore(), allowHTTPThreadTargets{}, foundation.NopAuditor{}, threads.ServiceConfig{
		OrganizationID: organization.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	objectStore, err := storage.NewLocalBlobStore(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	vaultService, err := storage.NewService(repository.NewMemoryStorageStore(), objectStore, foundation.NopAuditor{}, storage.ServiceConfig{OrganizationID: organization.ID})
	if err != nil {
		t.Fatal(err)
	}
	ledgerService, err := ledger.NewService(repository.NewMemoryLedgerStore(), allowHTTPLedgerReferences{}, foundation.NopAuditor{}, ledger.ServiceConfig{OrganizationID: organization.ID})
	if err != nil {
		t.Fatal(err)
	}
	horizonService, err := horizon.NewService(repository.NewMemoryHorizonStore(), atlasService, ledgerService, threadsService, foundation.NopAuditor{}, horizon.ServiceConfig{OrganizationID: organization.ID})
	if err != nil {
		t.Fatal(err)
	}
	patternsService, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{OrganizationID: organization.ID})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Dependencies{
		Atlas:            atlasService,
		AtlasCodes:       atlasCodesService,
		People:           peopleService,
		DirectoryImports: directoryService,
		Threads:          threadsService,
		Vault:            vaultService,
		Ledger:           ledgerService,
		Horizon:          horizonService,
		Patterns:         patternsService,
		Guard:            service,
		OIDC:             oidcFlow,
		SAML:             samlFlow,
	}, testOrigin, organization)
	return handler, service, concreteConnector
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %#v", name, cookies)
	return nil
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
