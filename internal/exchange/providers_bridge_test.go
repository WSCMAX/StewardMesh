package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-API-001, SEC-MCP-001, REQ-PATTERNS-001, REQ-ATLAS-MODELS-001.
// Features: migration.packages, integrations.protocols, templates.schemas, inventory.models. GitHub: #9, #14, #68.

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type bridgeProviderReferences struct{}

func (bridgeProviderReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}

type bridgeProviderAssets struct{ service *atlas.Service }

func (r bridgeProviderAssets) Get(ctx context.Context, id string) (domain.Asset, error) {
	return r.service.GetAsset(ctx, id)
}

type bridgeProviderEvaluator struct{}

func (bridgeProviderEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return nil, nil
}

type bridgeProviderTargets struct{}

func (bridgeProviderTargets) ListSubscriptionTargets(context.Context, string) ([]signals.SubscriptionTarget, error) {
	return []signals.SubscriptionTarget{}, nil
}

type bridgeProviderAuditor struct {
	mu     sync.Mutex
	events map[string]foundation.AuditEvent
}

func newBridgeProviderAuditor() *bridgeProviderAuditor {
	return &bridgeProviderAuditor{events: make(map[string]foundation.AuditEvent)}
}

func (a *bridgeProviderAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	event.Metadata = cloneBridgeProviderMetadata(event.Metadata)
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("audit event id conflicts with different immutable content")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

func (a *bridgeProviderAuditor) importedEvent(organizationID string) (foundation.AuditEvent, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range a.events {
		if event.OrganizationID == organizationID && event.Action == "bridge.oauth.client.imported" {
			return event, true
		}
	}
	return foundation.AuditEvent{}, false
}

func cloneBridgeProviderMetadata(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

type bridgeProviderHarness struct {
	service     *bridge.Service
	importer    bridge.ExchangeImporter
	guard       *guard.Service
	credentials guard.SessionCredentials
	now         time.Time
}

func newBridgeProviderHarness(t *testing.T, organizationID string, auditor foundation.Auditor) bridgeProviderHarness {
	t.Helper()
	now := time.Date(2026, time.August, 13, 18, 0, 0, 123_456_789, time.UTC)
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), guard.NewArgon2idHasher(), auditor, nil, guard.ServiceConfig{
		OrganizationID: organizationID, SessionTTL: time.Hour, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := guardService.Bootstrap(t.Context(), guard.BootstrapInput{
		Username: "bridge-provider-admin", Email: organizationID + "@example.test", DisplayName: "Bridge Provider Administrator",
		Password: "correct horse battery staple",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	atlasService, err := atlas.NewService(repository.NewMemoryAtlasStore(), bridgeProviderReferences{}, auditor, atlas.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	peopleService, err := people.NewService(repository.NewMemoryPeopleStore(), bridgeProviderAssets{service: atlasService}, auditor, people.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	signalsService, err := signals.NewService(repository.NewMemorySignalsStore(), bridgeProviderEvaluator{}, auditor, signals.ServiceConfig{
		OrganizationID: organizationID, SubscriptionTargets: bridgeProviderTargets{}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := bridge.NewServiceWithExchangeImporter(
		repository.NewMemoryBridgeStore(), guardService, atlasService, peopleService, signalsService, auditor,
		domain.Organization{ID: organizationID, Name: organizationID}, bridge.ServiceConfig{
			OrganizationID: organizationID, Issuer: "https://bridge.example.test", ResourceURI: "https://bridge.example.test/mcp", Now: func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return bridgeProviderHarness{service: service, importer: importer, guard: guardService, credentials: credentials, now: now}
}

func TestBridgeProviderRoundTripPreservesOnlyPublicClientState(t *testing.T) {
	source := newBridgeProviderHarness(t, "bridge-provider-source", foundation.NopAuditor{})
	active, err := source.service.CreateClient(t.Context(), source.credentials.Authentication, bridge.CreateClientInput{
		Name: "Active public client", RedirectURIs: []string{"http://127.0.0.1:9191/callback", "https://client.example.test/callback"},
		AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources, bridge.ScopeAssetsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := source.service.CreateClient(t.Context(), source.credentials.Authentication, bridge.CreateClientInput{
		Name: "Revoked public client", RedirectURIs: []string{"https://revoked.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources},
	})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err = source.service.RevokeClient(t.Context(), source.credentials.Authentication, revoked.ID)
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoke source Bridge client: %#v err=%v", revoked, err)
	}
	if _, err := exchange.NewBridgeProvider(source.service, nil); err == nil {
		t.Fatal("expected Bridge provider to require its opaque importer")
	}
	provider, err := exchange.NewBridgeProvider(source.service, source.importer)
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.Types(); !slices.Equal(got, []string{"bridge.oauth-client"}) {
		t.Fatalf("unexpected Bridge provider types %#v", got)
	}
	records, err := provider.ListRecords(t.Context())
	if err != nil || len(records) != 2 {
		t.Fatalf("list Bridge records: %#v err=%v", records, err)
	}
	byID := map[string]exchange.Record{}
	for _, record := range records {
		byID[record.ID] = record
		if len(record.Dependencies) != 0 || record.File != nil {
			t.Fatalf("Bridge public client unexpectedly has dependencies or a file: %#v", record)
		}
		for _, forbidden := range [][]byte{
			[]byte("organizationId"), []byte("createdBy"), []byte("createdAt"), []byte("clientSecret"), []byte("token"),
			[]byte("Hash"), []byte("grant"), []byte("authorization"), []byte("credential"), []byte("private"),
		} {
			if bytes.Contains(record.Payload, forbidden) {
				t.Fatalf("Bridge payload leaked forbidden state %q: %s", forbidden, record.Payload)
			}
		}
	}
	if byID[active.ID].Revision != 1 || byID[revoked.ID].Revision != 2 || !bytes.Contains(byID[revoked.ID].Payload, []byte(`"revokedAt"`)) {
		t.Fatalf("Bridge active/revoked revision contract drift: %#v", byID)
	}

	target := newBridgeProviderHarness(t, "bridge-provider-target", foundation.NopAuditor{})
	targetProvider, err := exchange.NewBridgeProvider(target.service, target.importer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewBridgeProvider(source.service, target.importer); err == nil {
		t.Fatal("expected Bridge provider to reject an importer from another service")
	}
	for index, sourceRecord := range records {
		result, err := targetProvider.ImportRecord(t.Context(), exchange.ProviderImportOperation{
			Token: "bridge-provider-import-" + string(rune('a'+index)), OccurredAt: source.now.Add(time.Duration(index+1) * time.Minute), ExpectedCreated: true,
		}, "bridge-source", sourceRecord, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import Bridge record %s: %#v err=%v", sourceRecord.ID, result, err)
		}
		if exact, err := targetProvider.ImportRecordExists(t.Context(), sourceRecord, nil); err != nil || !exact {
			t.Fatalf("Bridge exact readback %s exact=%t err=%v", sourceRecord.ID, exact, err)
		}
		replay, err := targetProvider.ImportRecord(t.Context(), exchange.ProviderImportOperation{ExpectedCreated: false}, "bridge-source", sourceRecord, nil)
		if err != nil || !replay.Committed || replay.Created {
			t.Fatalf("Bridge replay %s: %#v err=%v", sourceRecord.ID, replay, err)
		}
	}
}

func TestBridgeProviderRejectsSecretsAndNoncanonicalRedirects(t *testing.T) {
	harness := newBridgeProviderHarness(t, "bridge-provider-validation", foundation.NopAuditor{})
	provider, err := exchange.NewBridgeProvider(harness.service, harness.importer)
	if err != nil {
		t.Fatal(err)
	}
	valid := exchange.Record{
		Type: "bridge.oauth-client", ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Revision: 1, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback"}`),
	}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"client secret": func(value exchange.Record) exchange.Record {
			value.Payload = []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback","clientSecret":"forbidden"}`)
			return value
		},
		"external http redirect": func(value exchange.Record) exchange.Record {
			value.Payload = []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"http://client.example.test/callback"}`)
			return value
		},
		"redirect fragment": func(value exchange.Record) exchange.Record {
			value.Payload = []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback#token"}`)
			return value
		},
		"noncanonical whitespace": func(value exchange.Record) exchange.Record {
			value.Payload = []byte(`{ "allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback"}`)
			return value
		},
		"grant dependency": func(value exchange.Record) exchange.Record {
			value.Dependencies = []exchange.Reference{{Type: "bridge.oauth-grant", ID: "grant-one"}}
			return value
		},
		"revoked revision without time": func(value exchange.Record) exchange.Record { value.Revision = 2; return value },
		"active revision with time": func(value exchange.Record) exchange.Record {
			value.Payload = []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback","revokedAt":"2026-08-13T18:00:00Z"}`)
			return value
		},
		"sub-microsecond revoked time": func(value exchange.Record) exchange.Record {
			value.Revision = 2
			value.Payload = []byte(`{"allowedScopes":"mcp:resources","name":"Public client","redirectUris":"https://client.example.test/callback","revokedAt":"2026-08-13T18:00:00.000000001Z"}`)
			return value
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecord(t.Context(), exchange.ProviderImportOperation{
				Token: "bridge-invalid", OccurredAt: harness.now, ExpectedCreated: true,
			}, "source", mutate(valid), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected strict Bridge rejection, got %v", err)
			}
		})
	}
}

func TestBridgeImporterRepairsOrganizationScopedAuditAndWriteFence(t *testing.T) {
	auditor := newBridgeProviderAuditor()
	first := newBridgeProviderHarness(t, "bridge-audit-first", auditor)
	second := newBridgeProviderHarness(t, "bridge-audit-second", auditor)
	revokedAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	candidate := bridge.Client{
		ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Imported public client",
		RedirectURIs: []string{"https://imported.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources}, RevokedAt: &revokedAt,
	}
	operation := bridge.ExchangeImportOperation{Token: "shared-import-operation", OccurredAt: first.now}
	for _, harness := range []bridgeProviderHarness{first, second} {
		created, err := harness.importer.ImportClient(t.Context(), operation, 2, candidate)
		if err != nil || !created.Committed || !created.Created {
			t.Fatalf("initial Bridge import: %#v err=%v", created, err)
		}
		replayed, err := harness.importer.ImportClient(t.Context(), operation, 2, candidate)
		if err != nil || !replayed.Committed || replayed.Created {
			t.Fatalf("Bridge audit repair replay: %#v err=%v", replayed, err)
		}
	}
	firstEvent, firstOK := auditor.importedEvent("bridge-audit-first")
	secondEvent, secondOK := auditor.importedEvent("bridge-audit-second")
	if !firstOK || !secondOK || firstEvent.ID == secondEvent.ID || firstEvent.CorrelationID != operation.Token || secondEvent.CorrelationID != operation.Token {
		t.Fatalf("Bridge deterministic audits were not organization-scoped: first=%#v second=%#v", firstEvent, secondEvent)
	}

	active := bridge.Client{
		ID: "cccccccccccccccccccccccccccccccc", Name: "Write locked client",
		RedirectURIs: []string{"https://locked.example.test/callback"}, AllowedScopes: []bridge.Scope{bridge.ScopeMCPResources},
	}
	if result, err := first.importer.ImportClient(t.Context(), bridge.ExchangeImportOperation{Token: "write-lock-import", OccurredAt: first.now}, 1, active); err != nil || !result.Committed {
		t.Fatalf("import active Bridge client: %#v err=%v", result, err)
	}
	if _, _, err := first.guard.RegisterImportedResourceOwnership(t.Context(), first.credentials.Authentication.Principal.Subject, guard.ResourceOwnershipInput{
		ResourceType: "bridge.oauth-client", ResourceID: active.ID, SourceSystemID: "foreign-system", SourceRecordID: "bridge.oauth-client:" + active.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.service.RevokeClient(t.Context(), first.credentials.Authentication, active.ID); !errors.Is(err, guard.ErrResourceWriteLocked) {
		t.Fatalf("expected Bridge imported-resource revoke fence, got %v", err)
	}
	stored, err := first.service.ExchangeClient(t.Context(), active.ID)
	if err != nil || stored.RevokedAt != nil {
		t.Fatalf("write-locked Bridge client changed: %#v err=%v", stored, err)
	}
}
