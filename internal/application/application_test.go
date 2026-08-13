package application

// Requirements: REQ-FOUNDATION-001, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-PATTERNS-001, REQ-STORAGE-001, REQ-HORIZON-001, REQ-PLATFORM-VALKEY-001, SEC-GUARD-001.
// Features: lifecycle.planning, templates.schemas.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/config"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/grouperfixture"
)

func TestNewBuildsReusableMemoryApplication(t *testing.T) {
	cfg := memoryConfiguration(t)
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID || app.Organization().Name != cfg.OrganizationName {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected reusable handler health response, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["organizationId"] != cfg.OrganizationID {
		t.Fatalf("unexpected health response %#v", body)
	}
	templatesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	templatesResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(templatesResponse, templatesRequest)
	if templatesResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected wired Patterns service to require authentication, got %d: %s", templatesResponse.Code, templatesResponse.Body.String())
	}
}

func TestNewRegistersOptionalMicrosoftEntraConnectorWithoutStartupNetworkWrites(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.EntraSourceSystemID = "entra-primary"
	cfg.EntraTenantID = "11111111-1111-4111-8111-111111111111"
	cfg.EntraClientID = "22222222-2222-4222-8222-222222222222"
	cfg.EntraClientSecret = "0123456789abcdef"
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewRegistersOptionalSailPointConnectorWithoutStartupNetworkWrites(t *testing.T) {
	cfg := memoryConfiguration(t)
	cfg.SailPointSourceSystemID = "sailpoint-primary"
	cfg.SailPointBaseURL = "https://example.api.identitynow.com"
	cfg.SailPointClientID = "0123456789abcdef"
	cfg.SailPointClientSecret = "abcdef0123456789"
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNewWiresConfiguredGrouperThroughPreviewApplyAndGraph(t *testing.T) {
	const token = "application-fixture-token"
	fixture, err := grouperfixture.New(token)
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest(http.MethodPost, "/fixture/groups", bytes.NewBufferString(`{"id":"researchers","name":"app:researchers","displayName":"Researchers","active":true}`))
	create.Header.Set("Authorization", "Bearer "+token)
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	fixture.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create fixture group: %d %s", created.Code, created.Body.String())
	}
	provider := httptest.NewServer(fixture.Handler())
	defer provider.Close()

	cfg := memoryConfiguration(t)
	cfg.GrouperURL = provider.URL + "/grouper-ws/scim/v2"
	cfg.GrouperSourceSystemID = "application-grouper"
	cfg.GrouperBearerToken = token
	cfg.GrouperConfigRevision = "test-v1"
	cfg.GrouperAllowPrivateNetwork = true
	app, err := New(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})

	bootstrapPayload := bytes.NewBufferString(`{"username":"administrator","email":"administrator@example.test","displayName":"Administrator","password":"correct horse battery staple"}`)
	bootstrapRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bootstrapPayload)
	bootstrapRequest.Header.Set("Content-Type", "application/json")
	bootstrapRequest.Header.Set("Origin", cfg.AllowedOrigin)
	bootstrapResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(bootstrapResponse, bootstrapRequest)
	var credentials struct {
		CSRFToken string `json:"csrfToken"`
	}
	if bootstrapResponse.Code != http.StatusCreated || json.Unmarshal(bootstrapResponse.Body.Bytes(), &credentials) != nil ||
		credentials.CSRFToken == "" || len(bootstrapResponse.Result().Cookies()) != 1 {
		t.Fatalf("bootstrap application administrator: %d %s", bootstrapResponse.Code, bootstrapResponse.Body.String())
	}
	cookie := bootstrapResponse.Result().Cookies()[0]
	authenticated := func(method, path string, body *bytes.Buffer, idempotencyKey string) *http.Request {
		var requestBody io.Reader
		if body != nil {
			requestBody = body
		}
		request := httptest.NewRequest(method, path, requestBody)
		request.AddCookie(cookie)
		if method != http.MethodGet {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", cfg.AllowedOrigin)
			request.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		}
		if idempotencyKey != "" {
			request.Header.Set("Idempotency-Key", idempotencyKey)
		}
		return request
	}

	previewResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewResponse, authenticated(http.MethodPost, "/api/v1/directory-imports/preview",
		bytes.NewBufferString(`{"sourceSystemId":"application-grouper"}`), "application-grouper-preview"))
	var preview directoryexpansion.OperationResult
	if previewResponse.Code != http.StatusCreated || json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil ||
		preview.Batch.Provider != directoryexpansion.GrouperProvider || preview.Batch.Counts.Created != 1 {
		t.Fatalf("preview configured Grouper: %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	applyResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(applyResponse, authenticated(http.MethodPost, "/api/v1/directory-imports/"+preview.Batch.ID+"/apply", nil, "application-grouper-apply"))
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("apply configured Grouper: %d %s", applyResponse.Code, applyResponse.Body.String())
	}
	graphResponse := httptest.NewRecorder()
	app.Handler().ServeHTTP(graphResponse, authenticated(http.MethodGet, "/api/v1/graph", nil, ""))
	var graph directoryexpansion.Graph
	if graphResponse.Code != http.StatusOK || json.Unmarshal(graphResponse.Body.Bytes(), &graph) != nil ||
		len(graph.Nodes) != 1 || graph.Nodes[0].Kind != "group" || graph.Nodes[0].Label != "Researchers" {
		t.Fatalf("read configured Grouper graph: %d %s", graphResponse.Code, graphResponse.Body.String())
	}
}

func TestNewSupportsMemoryCacheAndFailsClosedForUnavailableValkey(t *testing.T) {
	memory := memoryConfiguration(t)
	memory.CacheDriver = config.CacheDriverMemory
	app, err := New(context.Background(), memory, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	valkey := memoryConfiguration(t)
	valkey.CacheDriver = config.CacheDriverValkey
	valkey.CacheURL = "redis://127.0.0.1:6379/0"
	valkey.CacheKeySecret = "0123456789abcdef0123456789abcdef"
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if app, err := New(canceled, valkey, Options{}); err == nil || app != nil {
		t.Fatalf("expected configured Valkey startup to fail closed, app=%T err=%v", app, err)
	}
}

func TestInitializeAttemptLimiterPreservesDisabledModeAndSupportsMemory(t *testing.T) {
	disabled := memoryConfiguration(t)
	limiter, closeCache, err := initializeAttemptLimiter(context.Background(), disabled)
	if err != nil {
		t.Fatal(err)
	}
	if limiter != nil {
		t.Fatal("expected disabled cache mode to preserve Guard's local limiter")
	}
	if err := closeCache(); err != nil {
		t.Fatal(err)
	}

	memory := disabled
	memory.CacheDriver = config.CacheDriverMemory
	limiter, closeCache, err = initializeAttemptLimiter(context.Background(), memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeCache(); err != nil {
			t.Fatal(err)
		}
	})
	if limiter == nil {
		t.Fatal("expected memory cache mode to create a cache-backed limiter")
	}
	allowed, err := limiter.Allow(context.Background(), "account|administrator", time.Now())
	if err != nil || !allowed {
		t.Fatalf("expected initialized memory limiter to allow, allowed=%t err=%v", allowed, err)
	}
}

func TestCloseIsIdempotentAndReleasesResourcesInReverseOrder(t *testing.T) {
	var order []string
	cacheError := errors.New("cache close")
	foundationError := errors.New("foundation close")
	app := &Application{
		closeCache: func() error {
			order = append(order, "cache")
			return cacheError
		},
		closeFoundation: func() error {
			order = append(order, "foundation")
			return foundationError
		},
	}
	for range 2 {
		err := app.Close()
		if !errors.Is(err, cacheError) || !errors.Is(err, foundationError) {
			t.Fatalf("expected joined close errors, got %v", err)
		}
	}
	if !reflect.DeepEqual(order, []string{"cache", "foundation"}) {
		t.Fatalf("unexpected close order %v", order)
	}
}

func TestNewRejectsMissingContextAndInvalidConfiguration(t *testing.T) {
	cfg := memoryConfiguration(t)
	if app, err := New(nil, cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected nil context to fail, app=%T err=%v", app, err)
	}
	cfg.BlobDir = ""
	if app, err := New(context.Background(), cfg, Options{}); err == nil || app != nil {
		t.Fatalf("expected invalid blob configuration to fail, app=%T err=%v", app, err)
	}
}

func TestNewBuildsPostgresApplicationWithExplicitMigrations(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	cfg := memoryConfiguration(t)
	cfg.RepositoryDriver = config.RepositoryDriverPostgres
	cfg.DatabaseURL = databaseURL
	cfg.OrganizationID = fmt.Sprintf("application-construction-%d", time.Now().UnixNano())
	cfg.OrganizationName = "Application Construction"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app, err := New(ctx, cfg, Options{RunMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if app.Organization().ID != cfg.OrganizationID {
		t.Fatalf("unexpected organization %#v", app.Organization())
	}
}

func memoryConfiguration(t *testing.T) config.Config {
	t.Helper()
	cfg := config.FromEnv()
	cfg.Addr = "127.0.0.1:8080"
	cfg.RepositoryDriver = config.RepositoryDriverMemory
	cfg.DatabaseURL = ""
	cfg.CacheDriver = config.CacheDriverNone
	cfg.CacheURL = ""
	cfg.CacheKeySecret = ""
	cfg.OIDCIssuerURL = ""
	cfg.OIDCClientID = ""
	cfg.OIDCClientSecret = ""
	cfg.OIDCRedirectURL = ""
	cfg.OIDCTransactionSecret = ""
	cfg.OIDCAdministratorClaim = ""
	cfg.OIDCAdministratorValues = nil
	cfg.EntraSourceSystemID = "entra"
	cfg.EntraTenantID = ""
	cfg.EntraClientID = ""
	cfg.EntraClientSecret = ""
	cfg.SailPointSourceSystemID = "sailpoint"
	cfg.SailPointBaseURL = ""
	cfg.SailPointClientID = ""
	cfg.SailPointClientSecret = ""
	cfg.BlobDir = t.TempDir()
	cfg.AllowedOrigin = "http://localhost:5173"
	cfg.SessionCookieSecure = false
	cfg.BootstrapToken = ""
	cfg.SessionTTL = time.Hour
	cfg.OrganizationID = "application-test"
	cfg.OrganizationName = "Application Test"
	cfg.GrouperURL = ""
	cfg.GrouperSourceSystemID = ""
	cfg.GrouperUsername = ""
	cfg.GrouperPassword = ""
	cfg.GrouperBearerToken = ""
	cfg.GrouperConfigRevision = ""
	cfg.GrouperPageSize = directoryexpansion.DefaultGrouperPageSize
	cfg.GrouperMaximumResponseBytes = directoryexpansion.DefaultGrouperResponseBytes
	cfg.GrouperTimeout = directoryexpansion.DefaultGrouperTimeout
	cfg.GrouperAllowPrivateNetwork = false
	return cfg
}
