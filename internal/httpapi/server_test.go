package httpapi

// Requirements: REQ-FOUNDATION-001, REQ-ATLAS-001, REQ-PEOPLE-001,
// REQ-DIRECTORY-EXPANSION-001, REQ-THREADS-001, REQ-STORAGE-001, REQ-PLATFORM-VALKEY-001,
// SEC-GUARD-001, SEC-HTTP-001.

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
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/identity"
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
	payload, _ := json.Marshal(map[string]any{
		"id": "asset-1", "name": "Lab server", "kind": "server", "assetTag": "LAB-001",
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
	if created.OrganizationID != "example-org" || created.Revision != 1 || created.Status != "active" {
		t.Fatalf("unexpected created asset %#v", created)
	}
	listReq := authenticatedRequest(http.MethodGet, "/api/v1/assets?q=lab-001&kind=server&status=active", nil, session)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), "asset-1") {
		t.Fatalf("expected filtered asset, got %d: %s", listRes.Code, listRes.Body.String())
	}
	getReq := authenticatedRequest(http.MethodGet, "/api/v1/assets/asset-1", nil, session)
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK || !strings.Contains(getRes.Body.String(), "lab-server.example.test") {
		t.Fatalf("expected asset detail, got %d: %s", getRes.Code, getRes.Body.String())
	}
	updatePayload, _ := json.Marshal(map[string]any{
		"name": created.Name, "kind": created.Kind, "assetTag": created.AssetTag,
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
	peopleService, err := people.NewService(repository.NewMemoryPeopleStore(), atlasService, foundation.NopAuditor{}, people.ServiceConfig{
		OrganizationID: organization.ID,
	})
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
	return NewServer(Dependencies{
		Atlas:   atlasService,
		People:  peopleService,
		Threads: threadsService,
		Vault:   vaultService,
		Guard:   service,
		OIDC:    oidcFlow,
		SAML:    samlFlow,
	}, testOrigin, organization)
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
