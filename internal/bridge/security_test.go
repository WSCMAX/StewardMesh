package bridge_test

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testClock struct{ nanos atomic.Int64 }

func newTestClock(now time.Time) *testClock {
	clock := &testClock{}
	clock.nanos.Store(now.UnixNano())
	return clock
}

func (c *testClock) Now() time.Time             { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *testClock) Add(duration time.Duration) { c.nanos.Add(int64(duration)) }

type mutableGuardStore struct {
	guard.Store
	removeIntegrations atomic.Bool
	removeAssets       atomic.Bool
}

func (s *mutableGuardStore) AccessForAccount(ctx context.Context, organizationID, accountID string) (guard.Access, error) {
	access, err := s.Store.AccessForAccount(ctx, organizationID, accountID)
	return s.current(access), err
}

func (s *mutableGuardStore) FindSessionByTokenHash(ctx context.Context, organizationID string, tokenHash []byte, now time.Time) (guard.Session, guard.Access, error) {
	session, access, err := s.Store.FindSessionByTokenHash(ctx, organizationID, tokenHash, now)
	return session, s.current(access), err
}

func (s *mutableGuardStore) current(access guard.Access) guard.Access {
	if !s.removeIntegrations.Load() && !s.removeAssets.Load() {
		return access
	}
	grants := make([]guard.Grant, 0, len(access.Grants))
	for _, grant := range access.Grants {
		if s.removeIntegrations.Load() && grant.Permission == guard.PermissionIntegrationsRead {
			continue
		}
		if s.removeAssets.Load() && grant.Permission == guard.PermissionAssetsRead {
			continue
		}
		grants = append(grants, grant)
	}
	access.Grants = grants
	return access
}

type testAuditor struct {
	mu             sync.Mutex
	events         []foundation.AuditEvent
	failOperations atomic.Bool
}

func (a *testAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.failOperations.Load() && event.Action == "bridge.mcp.operation" {
		return errors.New("audit unavailable")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	event.Metadata = cloneMetadata(event.Metadata)
	a.events = append(a.events, event)
	return nil
}

func (a *testAuditor) operationEvents() []foundation.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]foundation.AuditEvent, 0)
	for _, event := range a.events {
		if event.Action == "bridge.mcp.operation" {
			result = append(result, event)
		}
	}
	return result
}

func cloneMetadata(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

type allowAssetReferences struct{}

func (allowAssetReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}

type atlasAssetReader struct{ service *atlas.Service }

func (r atlasAssetReader) Get(ctx context.Context, id string) (domain.Asset, error) {
	return r.service.GetAsset(ctx, id)
}

type emptySignalsEvaluator struct{}

func (emptySignalsEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return nil, nil
}

type bridgeHarness struct {
	service     *bridge.Service
	guard       *guard.Service
	store       *mutableGuardStore
	auditor     *testAuditor
	clock       *testClock
	credentials guard.SessionCredentials
}

func newBridgeHarness(t *testing.T) *bridgeHarness {
	t.Helper()
	const organizationID = "bridge-security-test"
	clock := newTestClock(time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC))
	auditor := &testAuditor{}
	guardStore := &mutableGuardStore{Store: repository.NewMemoryGuardStore()}
	guardService, err := guard.NewService(guardStore, guard.NewArgon2idHasher(), auditor, nil, guard.ServiceConfig{OrganizationID: organizationID, SessionTTL: time.Hour, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := guardService.Bootstrap(t.Context(), guard.BootstrapInput{Username: "bridge-admin", Email: "bridge-admin@example.test", DisplayName: "Bridge Administrator", Password: "correct horse battery staple"}, true)
	if err != nil {
		t.Fatal(err)
	}
	atlasService, err := atlas.NewService(repository.NewMemoryAtlasStore(), allowAssetReferences{}, auditor, atlas.ServiceConfig{OrganizationID: organizationID, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	peopleService, err := people.NewService(repository.NewMemoryPeopleStore(), atlasAssetReader{service: atlasService}, auditor, people.ServiceConfig{OrganizationID: organizationID, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	signalsService, err := signals.NewService(repository.NewMemorySignalsStore(), emptySignalsEvaluator{}, auditor, signals.ServiceConfig{OrganizationID: organizationID, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	service, err := bridge.NewService(repository.NewMemoryBridgeStore(), guardService, atlasService, peopleService, signalsService, auditor,
		domain.Organization{ID: organizationID, Name: "Bridge Security Test"}, bridge.ServiceConfig{OrganizationID: organizationID, Issuer: "https://bridge.example.test", ResourceURI: "https://bridge.example.test/mcp", Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &bridgeHarness{service: service, guard: guardService, store: guardStore, auditor: auditor, clock: clock, credentials: credentials}
}

func (h *bridgeHarness) oauthAccess(t *testing.T) (bridge.Access, string) {
	t.Helper()
	client, err := h.service.CreateClient(t.Context(), h.credentials.Authentication, bridge.CreateClientInput{Name: "Security client", RedirectURIs: []string{"https://client.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources, bridge.ScopeAssetsRead}})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("A", 43)
	digest := sha256.Sum256([]byte(verifier))
	authorization, err := h.service.BeginAuthorization(t.Context(), h.credentials.Authentication, bridge.AuthorizationInput{
		ResponseType: "code", ClientID: client.ID, RedirectURI: client.RedirectURIs[0], ResourceURI: h.service.ResourceURI(), Scopes: "mcp:resources assets:read",
		State: "security-state", CodeChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := h.service.DecideConsent(t.Context(), h.credentials.Authentication, authorization.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse(redirect)
	tokens, err := h.service.ExchangeToken(t.Context(), bridge.TokenInput{GrantType: "authorization_code", Code: location.Query().Get("code"), ClientID: client.ID, RedirectURI: client.RedirectURIs[0], ResourceURI: h.service.ResourceURI(), CodeVerifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	access, err := h.service.AuthenticateAccessToken(t.Context(), tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return access, tokens.AccessToken
}

func TestRemoteBridgeOperationsRecheckGatewayAndDomainPermissions(t *testing.T) {
	harness := newBridgeHarness(t)
	access, rawToken := harness.oauthAccess(t)
	harness.store.removeAssets.Store(true)
	scoped, refreshed, err := harness.service.ContextForAccess(t.Context(), access)
	if err != nil {
		t.Fatalf("remote access refresh failed before domain authorization: %v", err)
	}
	if err := harness.service.RequireScopePermission(scoped, refreshed, bridge.ScopeAssetsRead, guard.PermissionAssetsRead); !errors.Is(err, bridge.ErrPermissionDenied) {
		t.Fatalf("existing remote access survived assets.read revocation: %v", err)
	}
	harness.store.removeIntegrations.Store(true)
	if _, err := harness.service.AuthenticateAccessToken(t.Context(), rawToken); !errors.Is(err, bridge.ErrPermissionDenied) {
		t.Fatalf("remote bearer survived integrations.read revocation: %v", err)
	}
	if _, _, err := harness.service.ContextForAccess(t.Context(), access); !errors.Is(err, bridge.ErrPermissionDenied) {
		t.Fatalf("existing remote access survived integrations.read revocation: %v", err)
	}
}

func TestAdministrationSessionDefersAuthorizationToSharedServiceMethods(t *testing.T) {
	harness := newBridgeHarness(t)
	harness.store.removeIntegrations.Store(true)
	authentication, err := harness.service.AuthenticateAdministrationSession(t.Context(), harness.credentials.Token)
	if err != nil {
		t.Fatalf("active Guard session was not authenticated: %v", err)
	}
	if _, err := harness.service.ListClients(t.Context(), authentication, bridge.PageRequest{}); !errors.Is(err, bridge.ErrPermissionDenied) {
		t.Fatalf("read-only administration method skipped its shared permission check: %v", err)
	}
	if _, err := harness.service.CreateClient(t.Context(), authentication, bridge.CreateClientInput{
		Name: "Write-only administration client", RedirectURIs: []string{"https://write-only.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources},
	}); err != nil {
		t.Fatalf("gRPC authentication added a permission not required by the shared write method: %v", err)
	}
}

func TestStdioRevalidatesSessionRevocationAndExpiryPerOperation(t *testing.T) {
	t.Run("revocation", func(t *testing.T) {
		harness := newBridgeHarness(t)
		access, err := harness.service.AuthenticateLocalSession(t.Context(), harness.credentials.Token, "mcp:resources assets:read")
		if err != nil {
			t.Fatal(err)
		}
		session, stop := connectStdio(t, harness.service, access)
		defer stop()
		if err := harness.guard.Logout(t.Context(), harness.credentials.Authentication); err != nil {
			t.Fatal(err)
		}
		result, callErr := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"limit": 1}})
		if callErr == nil && result != nil && !result.IsError {
			t.Fatalf("stdio operation survived originating session revocation: %#v", result)
		}
		if _, err := session.ListTools(t.Context(), nil); err == nil {
			t.Fatal("stdio SDK list operation survived originating session revocation")
		}
		events := harness.auditor.operationEvents()
		if len(events) != 1 || events[0].Metadata["method"] != "tools/call:search_assets" || events[0].Metadata["outcome"] != "denied" {
			t.Fatalf("revoked stdio operation audit=%#v", events)
		}
	})

	t.Run("expiry", func(t *testing.T) {
		harness := newBridgeHarness(t)
		access, err := harness.service.AuthenticateLocalSession(t.Context(), harness.credentials.Token, "mcp:resources assets:read")
		if err != nil {
			t.Fatal(err)
		}
		session, stop := connectStdio(t, harness.service, access)
		defer stop()
		harness.clock.Add(2 * time.Hour)
		if _, err := session.ListTools(t.Context(), nil); err == nil {
			t.Fatal("stdio operation survived session/access expiry")
		}
	})
}

func TestMCPOperationsAuditBoundedRedactedOutcomesAndFailClosed(t *testing.T) {
	harness := newBridgeHarness(t)
	access, err := harness.service.AuthenticateLocalSession(t.Context(), harness.credentials.Token, "mcp:resources assets:read")
	if err != nil {
		t.Fatal(err)
	}
	session, stop := connectStdio(t, harness.service, access)
	defer stop()
	privateQuery := "private-person@example.test"
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"query": privateQuery, "limit": 1}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("audited MCP read failed result=%#v err=%v", result, err)
	}
	resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "stewardmesh://reports/inventory"})
	if err != nil || resource == nil || len(resource.Contents) != 1 {
		t.Fatalf("audited MCP resource read failed result=%#v err=%v", resource, err)
	}
	failed, failedErr := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"limit": 1000}})
	if failedErr == nil && failed != nil && !failed.IsError {
		t.Fatalf("invalid MCP read unexpectedly succeeded: %#v", failed)
	}
	malformed, malformedErr := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"limit": "not-a-number"}})
	if malformedErr == nil && malformed != nil && !malformed.IsError {
		t.Fatalf("malformed MCP read unexpectedly succeeded: %#v", malformed)
	}
	events := harness.auditor.operationEvents()
	if len(events) != 4 {
		t.Fatalf("MCP operation audits=%d", len(events))
	}
	for _, event := range events {
		for _, key := range []string{"actorId", "clientId", "grantId", "method", "resource", "count", "outcome", "requirementId"} {
			if event.Metadata[key] == "" || len(event.Metadata[key]) > 128 {
				t.Fatalf("invalid bounded MCP audit %q=%q", key, event.Metadata[key])
			}
		}
		for key, value := range event.Metadata {
			if strings.Contains(key, privateQuery) || strings.Contains(value, privateQuery) {
				t.Fatalf("MCP audit leaked input query in %q=%q", key, value)
			}
		}
	}
	if events[0].Metadata["outcome"] != "succeeded" || events[1].Metadata["method"] != "resources/read" || events[2].Metadata["outcome"] != "failed" || events[3].Metadata["method"] != "tools/call:search_assets" || events[3].Metadata["outcome"] != "failed" {
		t.Fatalf("unexpected MCP audit outcomes %#v", events)
	}
	harness.auditor.failOperations.Store(true)
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"limit": 1}})
	if err == nil && result != nil && !result.IsError {
		t.Fatalf("MCP read succeeded when its required audit failed: %#v", result)
	}
	resource, err = session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "stewardmesh://reports/inventory"})
	if err == nil && resource != nil {
		t.Fatalf("MCP resource succeeded when its required audit failed: %#v", resource)
	}
}

func connectStdio(t *testing.T, service *bridge.Service, access bridge.Access) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	serverConnection, clientConnection := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- service.RunStdio(ctx, access, serverConnection, serverConnection) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-security-test", Version: "1"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientConnection, Writer: clientConnection}, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return session, func() {
		_ = session.Close()
		_ = clientConnection.Close()
		_ = serverConnection.Close()
		cancel()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("stdio server did not stop")
		}
	}
}
