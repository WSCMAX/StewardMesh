package postgres

// Requirements: REQ-EXCHANGE-001, REQ-API-001, SEC-MCP-001.
// Features: migration.packages, integrations.protocols. GitHub: #9, #14.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/bridge"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type bridgeExchangeReferences struct{}

func (bridgeExchangeReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}

type bridgeExchangeAssets struct{ service *atlas.Service }

func (r bridgeExchangeAssets) Get(ctx context.Context, id string) (domain.Asset, error) {
	return r.service.GetAsset(ctx, id)
}

type bridgeExchangeEvaluator struct{}

func (bridgeExchangeEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return nil, nil
}

type bridgeExchangeTargets struct{}

func (bridgeExchangeTargets) ListSubscriptionTargets(context.Context, string) ([]signals.SubscriptionTarget, error) {
	return []signals.SubscriptionTarget{}, nil
}

func TestBridgeExchangeImporterPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("bridge-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Bridge Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	bridgeStore, err := NewBridgeStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), guard.NewArgon2idHasher(), auditor, nil, guard.ServiceConfig{OrganizationID: organizationID, SessionTTL: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	atlasService, err := atlas.NewService(repository.NewMemoryAtlasStore(), bridgeExchangeReferences{}, auditor, atlas.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	peopleService, err := people.NewService(repository.NewMemoryPeopleStore(), bridgeExchangeAssets{service: atlasService}, auditor, people.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	signalsService, err := signals.NewService(repository.NewMemorySignalsStore(), bridgeExchangeEvaluator{}, auditor, signals.ServiceConfig{OrganizationID: organizationID, SubscriptionTargets: bridgeExchangeTargets{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := bridge.NewServiceWithExchangeImporter(
		bridgeStore, guardService, atlasService, peopleService, signalsService, auditor,
		domain.Organization{ID: organizationID, Name: "Bridge Exchange Integration"},
		bridge.ServiceConfig{OrganizationID: organizationID, Issuer: "https://bridge.example.test", ResourceURI: "https://bridge.example.test/mcp", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := now.Add(-24 * time.Hour)
	candidate := bridge.Client{
		ID: fmt.Sprintf("%032x", time.Now().UnixNano()), Name: "Portable PostgreSQL client",
		RedirectURIs:  []string{"http://127.0.0.1:8484/callback", "https://client.example.test/callback"},
		AllowedScopes: []bridge.Scope{bridge.ScopeAssetsRead, bridge.ScopeMCPResources}, RevokedAt: &revokedAt,
	}
	operation := bridge.ExchangeImportOperation{Token: "bridge-postgres-import", OccurredAt: now}
	result, err := importer.ImportClient(ctx, operation, 2, candidate)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Bridge client: %#v err=%v", result, err)
	}
	replay, err := importer.ImportClient(ctx, operation, 2, candidate)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Bridge client: %#v err=%v", replay, err)
	}
	stored, err := service.ExchangeClient(ctx, candidate.ID)
	if err != nil || stored.OrganizationID != organizationID || stored.CreatedBy != "system:exchange" || stored.RevokedAt == nil || !stored.RevokedAt.Equal(revokedAt) ||
		stored.Name != candidate.Name || len(stored.RedirectURIs) != 2 || len(stored.AllowedScopes) != 2 {
		t.Fatalf("PostgreSQL Bridge import was not lossless: %#v err=%v", stored, err)
	}
	snapshot, err := service.ExchangeClients(ctx, 1)
	if err != nil || len(snapshot) != 1 || snapshot[0].ID != candidate.ID {
		t.Fatalf("PostgreSQL repeatable-read Bridge snapshot: %#v err=%v", snapshot, err)
	}
	conflict := candidate
	conflict.Name = "Changed portable client"
	if _, err := importer.ImportClient(ctx, operation, 2, conflict); err == nil {
		t.Fatal("PostgreSQL Bridge import accepted a same-ID conflict")
	}
}
