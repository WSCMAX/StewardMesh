package bridge_test

// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols. GitHub: #14.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	removeSignalsWrite atomic.Bool
	assetScopeSite     atomic.Value
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
	site, _ := s.assetScopeSite.Load().(string)
	if !s.removeIntegrations.Load() && !s.removeAssets.Load() && !s.removeSignalsWrite.Load() && site == "" {
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
		if s.removeSignalsWrite.Load() && grant.Permission == guard.PermissionSignalsWrite {
			continue
		}
		if site != "" && grant.Permission == guard.PermissionAssetsRead {
			grant.Scope = guard.Scope{Kind: guard.ScopeSite, OrganizationID: grant.Scope.OrganizationID, ResourceID: site}
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

func (a *testAuditor) allEvents() []foundation.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]foundation.AuditEvent, len(a.events))
	for index, event := range a.events {
		event.Metadata = cloneMetadata(event.Metadata)
		result[index] = event
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
func (allowAssetReferences) ValidateIdentities(context.Context, string, []string) error { return nil }

type atlasAssetReader struct{ service *atlas.Service }

func (r atlasAssetReader) Get(ctx context.Context, id string) (domain.Asset, error) {
	return r.service.GetAsset(ctx, id)
}

type testSignalsEvaluator struct {
	mu         sync.Mutex
	candidates []signals.Candidate
}

func (e *testSignalsEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]signals.Candidate(nil), e.candidates...), nil
}

func (e *testSignalsEvaluator) set(candidates ...signals.Candidate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.candidates = append([]signals.Candidate(nil), candidates...)
}

type emptySignalTargets struct{}

func (emptySignalTargets) ListSubscriptionTargets(context.Context, string) ([]signals.SubscriptionTarget, error) {
	return []signals.SubscriptionTarget{}, nil
}

type bridgeHarness struct {
	service     *bridge.Service
	atlas       *atlas.Service
	signals     *signals.Service
	evaluator   *testSignalsEvaluator
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
	guardStore.assetScopeSite.Store("")
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
	evaluator := &testSignalsEvaluator{}
	signalsService, err := signals.NewService(repository.NewMemorySignalsStore(), evaluator, auditor, signals.ServiceConfig{OrganizationID: organizationID, SubscriptionTargets: emptySignalTargets{}, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	service, err := bridge.NewService(repository.NewMemoryBridgeStore(), guardService, atlasService, peopleService, signalsService, auditor,
		domain.Organization{ID: organizationID, Name: "Bridge Security Test"}, bridge.ServiceConfig{OrganizationID: organizationID, Issuer: "https://bridge.example.test", ResourceURI: "https://bridge.example.test/mcp", Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &bridgeHarness{service: service, atlas: atlasService, signals: signalsService, evaluator: evaluator, guard: guardService, store: guardStore, auditor: auditor, clock: clock, credentials: credentials}
}

func (h *bridgeHarness) createSignalAlert(t *testing.T) signals.Alert {
	t.Helper()
	h.evaluator.set(signals.Candidate{TargetType: "contract", TargetID: "mcp-contract-1", Title: "MCP renewal", Summary: "Contract renewal needs review."})
	if _, err := h.signals.CreateRule(t.Context(), signals.CreateRuleInput{ID: "mcp-rule-1", Name: "MCP renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning}); err != nil {
		t.Fatal(err)
	}
	if result, err := h.signals.Evaluate(t.Context(), h.clock.Now()); err != nil || result.Created != 1 {
		t.Fatalf("create MCP alert result=%#v err=%v", result, err)
	}
	alerts, err := h.signals.ListAlerts(t.Context(), signals.AlertQuery{Status: signals.StatusActive, Limit: 10})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("list MCP alert alerts=%#v err=%v", alerts, err)
	}
	return alerts[0]
}

func signalMCP(t *testing.T, harness *bridgeHarness) (*mcp.ClientSession, func()) {
	t.Helper()
	access, err := harness.service.AuthenticateLocalSession(t.Context(), harness.credentials.Token, "mcp:resources signals:read signals:acknowledge")
	if err != nil {
		t.Fatal(err)
	}
	return connectStdio(t, harness.service, access)
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("call MCP tool %s result=%#v err=%v", name, result, err)
	}
	return result
}

func requireMCPToolFailure(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err == nil && result != nil && !result.IsError {
		t.Fatalf("MCP tool %s unexpectedly succeeded: %#v", name, result)
	}
}

func decodeStructured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded T
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode structured MCP result: %v (%s)", err, encoded)
	}
	return decoded
}

type confirmationResult struct {
	ConfirmationToken string    `json:"confirmationToken"`
	Action            string    `json:"action"`
	Summary           string    `json:"summary"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

func prepareMCPAcknowledgement(t *testing.T, session *mcp.ClientSession, alert signals.Alert) confirmationResult {
	t.Helper()
	return decodeStructured[confirmationResult](t, callMCPTool(t, session, "prepare_acknowledge_alert", map[string]any{"alertId": alert.ID, "revision": alert.Revision}))
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

func TestMCPAcknowledgementRequiresExactFreshAuthorizedConfirmation(t *testing.T) {
	t.Run("discovery exact confirmation replay and audit redaction", func(t *testing.T) {
		harness := newBridgeHarness(t)
		alert := harness.createSignalAlert(t)
		session, stop := signalMCP(t, harness)
		defer stop()

		listed, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		tools := make(map[string]*mcp.Tool, len(listed.Tools))
		for _, tool := range listed.Tools {
			tools[tool.Name] = tool
		}
		if len(tools) != 3 || tools["list_alerts"] == nil || tools["prepare_acknowledge_alert"] == nil || tools["confirm_acknowledge_alert"] == nil {
			t.Fatalf("scope-reduced MCP tools=%v", tools)
		}
		prepareTool, confirmTool := tools["prepare_acknowledge_alert"], tools["confirm_acknowledge_alert"]
		if prepareTool.Annotations == nil || !prepareTool.Annotations.ReadOnlyHint || confirmTool.Annotations == nil || confirmTool.Annotations.ReadOnlyHint || confirmTool.Annotations.DestructiveHint == nil || *confirmTool.Annotations.DestructiveHint {
			t.Fatalf("unsafe acknowledgement annotations prepare=%#v confirm=%#v", prepareTool.Annotations, confirmTool.Annotations)
		}

		challenge := prepareMCPAcknowledgement(t, session, alert)
		if challenge.ConfirmationToken == "" || challenge.Action != "signals.alert.acknowledge" || challenge.Summary == "" || !challenge.ExpiresAt.After(harness.clock.Now()) {
			t.Fatalf("invalid MCP confirmation challenge %#v", challenge)
		}
		unchanged, err := harness.signals.GetAlert(t.Context(), alert.ID)
		if err != nil || unchanged.Status != signals.StatusActive || unchanged.Revision != alert.Revision {
			t.Fatalf("prepare mutated alert=%#v err=%v", unchanged, err)
		}

		requireMCPToolFailure(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision + 1, "confirmationToken": challenge.ConfirmationToken,
		})
		unchanged, err = harness.signals.GetAlert(t.Context(), alert.ID)
		if err != nil || unchanged.Status != signals.StatusActive || unchanged.Revision != alert.Revision {
			t.Fatalf("changed arguments mutated alert=%#v err=%v", unchanged, err)
		}

		confirmed := decodeStructured[struct {
			Alert struct {
				ID       string              `json:"id"`
				Status   signals.AlertStatus `json:"status"`
				Revision int64               `json:"revision"`
			} `json:"alert"`
		}](t, callMCPTool(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		}))
		if confirmed.Alert.ID != alert.ID || confirmed.Alert.Status != signals.StatusAcknowledged || confirmed.Alert.Revision != alert.Revision+1 {
			t.Fatalf("unexpected acknowledged alert %#v", confirmed.Alert)
		}
		requireMCPToolFailure(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})

		for _, event := range harness.auditor.allEvents() {
			if strings.Contains(event.Action, challenge.ConfirmationToken) || strings.Contains(event.ResourceID, challenge.ConfirmationToken) {
				t.Fatalf("audit identity leaked confirmation token: %#v", event)
			}
			for key, value := range event.Metadata {
				if strings.Contains(key, challenge.ConfirmationToken) || strings.Contains(value, challenge.ConfirmationToken) {
					t.Fatalf("audit metadata leaked confirmation token in %q", key)
				}
			}
		}
	})

	t.Run("expired confirmation", func(t *testing.T) {
		harness := newBridgeHarness(t)
		alert := harness.createSignalAlert(t)
		session, stop := signalMCP(t, harness)
		defer stop()
		challenge := prepareMCPAcknowledgement(t, session, alert)
		harness.clock.Add(3 * time.Minute)
		requireMCPToolFailure(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})
		stored, err := harness.signals.GetAlert(t.Context(), alert.ID)
		if err != nil || stored.Status != signals.StatusActive || stored.Revision != alert.Revision {
			t.Fatalf("expired confirmation mutated alert=%#v err=%v", stored, err)
		}
	})

	t.Run("permission revoked after prepare", func(t *testing.T) {
		harness := newBridgeHarness(t)
		alert := harness.createSignalAlert(t)
		session, stop := signalMCP(t, harness)
		defer stop()
		challenge := prepareMCPAcknowledgement(t, session, alert)
		harness.store.removeSignalsWrite.Store(true)
		requireMCPToolFailure(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})
		harness.store.removeSignalsWrite.Store(false)
		callMCPTool(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})
	})

	t.Run("external ownership lock after prepare", func(t *testing.T) {
		harness := newBridgeHarness(t)
		alert := harness.createSignalAlert(t)
		session, stop := signalMCP(t, harness)
		defer stop()
		challenge := prepareMCPAcknowledgement(t, session, alert)
		if _, created, err := harness.guard.RegisterImportedResourceOwnership(t.Context(), harness.credentials.Authentication.Principal.Subject, guard.ResourceOwnershipInput{
			ResourceType: "signal_alert", ResourceID: alert.ID, SourceSystemID: "mcp-external-source", SourceRecordID: "alert-1",
		}); err != nil || !created {
			t.Fatalf("register external ownership created=%t err=%v", created, err)
		}
		requireMCPToolFailure(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})
		if _, err := harness.guard.ClaimResourceOwnership(t.Context(), harness.credentials.Authentication, "signal_alert", alert.ID); err != nil {
			t.Fatal(err)
		}
		callMCPTool(t, session, "confirm_acknowledge_alert", map[string]any{
			"alertId": alert.ID, "revision": alert.Revision, "confirmationToken": challenge.ConfirmationToken,
		})
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

func TestMCPAssetSearchAuthorizesBeforeLimitAndPaginatesEveryVisibleAsset(t *testing.T) {
	harness := newBridgeHarness(t)
	const visibleSite = "11111111111111111111111111111111"
	const hiddenSite = "22222222222222222222222222222222"
	for index := 0; index < 206; index++ {
		visible := index >= 105
		name, site := fmt.Sprintf("Alpha Hidden Asset %03d", index), hiddenSite
		if visible {
			name, site = fmt.Sprintf("Zeta Visible Asset %03d", index), visibleSite
		}
		if _, err := harness.atlas.CreateAsset(t.Context(), atlas.CreateAssetInput{
			ID: fmt.Sprintf("mcp-asset-%03d", index), Name: name, Kind: "computer",
			References: atlas.References{SiteID: site}, Status: "active",
		}); err != nil {
			t.Fatalf("create MCP asset %d: %v", index, err)
		}
	}
	harness.store.assetScopeSite.Store(visibleSite)
	access, err := harness.service.AuthenticateLocalSession(t.Context(), harness.credentials.Token, "mcp:resources assets:read")
	if err != nil {
		t.Fatal(err)
	}
	session, stop := connectStdio(t, harness.service, access)
	defer stop()

	seen := make(map[string]struct{}, 101)
	cursor := ""
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		result, callErr := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search_assets", Arguments: map[string]any{"limit": 25, "cursor": cursor}})
		if callErr != nil || result == nil || result.IsError {
			t.Fatalf("search authorized MCP assets page %d: result=%#v err=%v", pageNumber+1, result, callErr)
		}
		encoded, marshalErr := json.Marshal(result.StructuredContent)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var page struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(encoded, &page); err != nil {
			t.Fatalf("decode MCP asset page: %v (%s)", err, encoded)
		}
		for _, item := range page.Items {
			if !strings.HasPrefix(item.Name, "Zeta Visible") {
				t.Fatalf("unauthorized asset escaped pre-limit visibility: %#v", item)
			}
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("duplicate MCP keyset item %q", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 101 {
		t.Fatalf("MCP keyset pagination returned %d of 101 authorized assets", len(seen))
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
