package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-HORIZON-001, REQ-PATTERNS-001. Features: migration.packages, lifecycle.planning, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type horizonProviderAssets struct{ items map[string]domain.Asset }

func (r horizonProviderAssets) ListAssets(_ context.Context, query atlas.Query) ([]domain.Asset, error) {
	items := make([]domain.Asset, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if query.Limit > 0 && len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (r horizonProviderAssets) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	item, ok := r.items[id]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return item, nil
}

func (r horizonProviderAssets) GetModel(_ context.Context, id string) (domain.AssetModel, error) {
	return domain.AssetModel{}, atlas.ErrNotFound
}

type horizonProviderFinance struct{}

func (horizonProviderFinance) Snapshot(context.Context) (ledger.Snapshot, error) {
	return ledger.Snapshot{}, nil
}

type horizonProviderRelationships struct{}

func (horizonProviderRelationships) ListGoals(context.Context) ([]threads.Goal, error) {
	return nil, nil
}
func (horizonProviderRelationships) EvaluateTags(context.Context, threads.TargetType, string) ([]threads.EffectiveTag, error) {
	return nil, nil
}
func (horizonProviderRelationships) ListGoalLinks(context.Context, threads.TargetType, string) ([]threads.GoalLink, error) {
	return nil, nil
}

func newHorizonProviderService(t *testing.T, organizationID string, assets map[string]domain.Asset) (*horizon.Service, horizon.ExchangeImporter) {
	t.Helper()
	service, importer, err := horizon.NewServiceWithExchangeImporter(
		repository.NewMemoryHorizonStore(), horizonProviderAssets{items: assets}, horizonProviderFinance{}, horizonProviderRelationships{}, nil,
		foundation.NopAuditor{}, horizon.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time {
			return time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC)
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return service, importer
}

func horizonProviderAsset(organizationID string) domain.Asset {
	return domain.Asset{
		ID: "asset-portable", OrganizationID: organizationID, Name: "Portable asset", Kind: "server", Status: "active",
		Revision: 1, CreatedAt: time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC),
	}
}

func TestHorizonProviderRoundTripPreservesPlanAndDependency(t *testing.T) {
	sourceAssets := map[string]domain.Asset{"asset-portable": horizonProviderAsset("source-org")}
	sourceService, sourceImporter := newHorizonProviderService(t, "source-org", sourceAssets)
	if _, err := exchange.NewHorizonProvider(sourceService, nil); err == nil {
		t.Fatal("expected Horizon provider to require an importer capability")
	}
	sourceProvider, err := exchange.NewHorizonProvider(sourceService, sourceImporter)
	if err != nil {
		t.Fatal(err)
	}
	replacement := time.Date(2031, time.July, 20, 0, 0, 0, 0, time.UTC)
	created, err := sourceService.CreatePlan(context.Background(), horizon.CreatePlanInput{
		ID: "plan-portable", AssetID: "asset-portable", Scenario: "baseline", ExpectedUsefulLifeMonths: 60,
		ReplacementDate: &replacement, LifecycleStage: "planned", ReplacementCostMinor: 350_000,
		Currency: "USD", EffectiveFrom: time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := sourceService.UpdatePlan(context.Background(), horizon.UpdatePlanInput{
		ID: created.ID, AssetID: created.AssetID, Scenario: created.Scenario, ExpectedUsefulLifeMonths: 72,
		ReplacementDate: created.ReplacementDate, LifecycleStage: "approved", ReplacementCostMinor: 375_000,
		Currency: created.Currency, EffectiveFrom: created.EffectiveFrom.AddDate(0, 1, 0), Revision: created.Revision,
	})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("prepare portable Horizon plan: %#v err=%v", updated, err)
	}
	records, err := sourceProvider.ListRecords(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("list Horizon records: %#v err=%v", records, err)
	}
	record := records[0]
	if record.Type != "horizon.plan" || record.ID != updated.ID || record.Revision != updated.Revision ||
		!slices.Equal(record.Dependencies, []exchange.Reference{{Type: "atlas.asset", ID: updated.AssetID}}) {
		t.Fatalf("unexpected Horizon record %#v", record)
	}
	for _, forbidden := range [][]byte{[]byte("organizationId"), []byte("createdAt"), []byte("updatedAt"), []byte("derivedReplacementDate")} {
		if bytes.Contains(record.Payload, forbidden) {
			t.Fatalf("Horizon payload leaked deployment state %q: %s", forbidden, record.Payload)
		}
	}
	targetAssets := map[string]domain.Asset{"asset-portable": horizonProviderAsset("target-org")}
	targetService, targetImporter := newHorizonProviderService(t, "target-org", targetAssets)
	targetProvider, err := exchange.NewHorizonProvider(targetService, targetImporter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewHorizonProvider(sourceService, targetImporter); err == nil {
		t.Fatal("expected Horizon provider to reject an importer capability owned by another service")
	}
	result, err := targetProvider.ImportRecord(context.Background(), exchange.ProviderImportOperation{
		Token: "horizon-provider-import", OccurredAt: time.Date(2026, time.August, 13, 23, 0, 0, 0, time.UTC), ExpectedCreated: true,
	}, "source-system", record, nil)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Horizon record: %#v err=%v", result, err)
	}
	if exact, err := targetProvider.ImportRecordExists(context.Background(), record, nil); err != nil || !exact {
		t.Fatalf("Horizon exact readback exact=%t err=%v", exact, err)
	}
	imported, err := targetService.GetPlan(context.Background(), record.ID)
	if err != nil || imported.Revision != updated.Revision || imported.AssetID != updated.AssetID ||
		imported.Scenario != updated.Scenario || imported.ExpectedUsefulLifeMonths != updated.ExpectedUsefulLifeMonths ||
		imported.ReplacementDate == nil || !imported.ReplacementDate.Equal(*updated.ReplacementDate) ||
		imported.LifecycleStage != updated.LifecycleStage || imported.ReplacementCostMinor != updated.ReplacementCostMinor ||
		imported.Currency != updated.Currency || !imported.EffectiveFrom.Equal(updated.EffectiveFrom) {
		t.Fatalf("Horizon record did not round trip losslessly: %#v err=%v", imported, err)
	}
	replay, err := targetProvider.ImportRecord(context.Background(), exchange.ProviderImportOperation{ExpectedCreated: false}, "source-system", record, nil)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("Horizon provider replay did not converge: %#v err=%v", replay, err)
	}
}

func TestHorizonProviderRejectsNoncanonicalPayloadAndMissingAsset(t *testing.T) {
	service, importer := newHorizonProviderService(t, "target-org", map[string]domain.Asset{})
	provider, err := exchange.NewHorizonProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	record := exchange.Record{
		Type: "horizon.plan", ID: "plan-portable", Revision: 3,
		Dependencies: []exchange.Reference{{Type: "atlas.asset", ID: "asset-portable"}},
		Payload:      []byte(`{"assetId":"asset-portable","scenario":"baseline","expectedUsefulLifeMonths":60,"replacementDate":"2031-07-20","lifecycleStage":"approved","replacementCostMinor":375000,"currency":"USD","effectiveFrom":"2027-02-01"}`),
	}
	result, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{
		Token: "horizon-missing-dependency", OccurredAt: time.Date(2026, time.August, 13, 23, 30, 0, 0, time.UTC), ExpectedCreated: true,
	}, "source-system", record, nil)
	if !errors.Is(err, exchange.ErrDependencyMissing) || result.Committed {
		t.Fatalf("expected missing Horizon dependency, result=%#v err=%v", result, err)
	}
	for name, payload := range map[string][]byte{
		"unknown field":         []byte(`{"assetId":"asset-portable","scenario":"baseline","expectedUsefulLifeMonths":60,"lifecycleStage":"approved","replacementCostMinor":375000,"currency":"USD","effectiveFrom":"2027-02-01","organizationId":"source-org"}`),
		"noncanonical scenario": []byte(`{"assetId":"asset-portable","scenario":" Baseline ","expectedUsefulLifeMonths":60,"lifecycleStage":"approved","replacementCostMinor":375000,"currency":"USD","effectiveFrom":"2027-02-01"}`),
		"timestamp date":        []byte(`{"assetId":"asset-portable","scenario":"baseline","expectedUsefulLifeMonths":60,"lifecycleStage":"approved","replacementCostMinor":375000,"currency":"USD","effectiveFrom":"2027-02-01T00:00:00Z"}`),
	} {
		t.Run(name, func(t *testing.T) {
			invalid := record
			invalid.Payload = payload
			if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{ExpectedCreated: true}, "source-system", invalid, nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected strict Horizon payload rejection, got %v", err)
			}
		})
	}
}
