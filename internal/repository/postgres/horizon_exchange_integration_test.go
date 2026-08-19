package postgres

// Requirements: REQ-EXCHANGE-001, REQ-HORIZON-001. Features: migration.packages, lifecycle.planning. GitHub: #9.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type horizonExchangeAssets struct{ items map[string]domain.Asset }

func (r horizonExchangeAssets) ListAssets(_ context.Context, query atlas.Query) ([]domain.Asset, error) {
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

func (r horizonExchangeAssets) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	item, ok := r.items[id]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return item, nil
}

func (r horizonExchangeAssets) GetModel(_ context.Context, id string) (domain.AssetModel, error) {
	return domain.AssetModel{}, atlas.ErrNotFound
}

type horizonExchangeFinance struct{}

func (horizonExchangeFinance) Snapshot(context.Context) (ledger.Snapshot, error) {
	return ledger.Snapshot{}, nil
}

type horizonExchangeRelationships struct{}

func (horizonExchangeRelationships) ListGoals(context.Context) ([]threads.Goal, error) {
	return nil, nil
}
func (horizonExchangeRelationships) EvaluateTags(context.Context, threads.TargetType, string) ([]threads.EffectiveTag, error) {
	return nil, nil
}
func (horizonExchangeRelationships) ListGoalLinks(context.Context, threads.TargetType, string) ([]threads.GoalLink, error) {
	return nil, nil
}

func TestHorizonExchangeImporterPostgresIntegration(t *testing.T) {
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
	unique := fmt.Sprintf("horizon-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, unique, "Horizon Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	assetID := "asset-" + unique
	asset := domain.Asset{
		ID: assetID, OrganizationID: unique, Name: "Horizon Exchange Asset", Kind: "server", Status: "active", Revision: 1,
		CreatedAt: time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC),
	}
	assetStore, err := NewAtlasStore(database)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := domain.AssetLifecycleEvent{
		ID: fmt.Sprintf("%032x", time.Now().UnixNano()), OrganizationID: unique, AssetID: assetID,
		ToStatus: "active", Revision: 1, ActorID: "integration", OccurredAt: asset.CreatedAt,
	}
	if _, err := assetStore.CreateAsset(ctx, asset, lifecycle); err != nil {
		t.Fatal(err)
	}
	store, err := NewHorizonStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := horizon.NewServiceWithExchangeImporter(
		store, horizonExchangeAssets{items: map[string]domain.Asset{assetID: asset}}, horizonExchangeFinance{}, horizonExchangeRelationships{}, nil,
		auditor, horizon.ServiceConfig{OrganizationID: unique},
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement := time.Date(2032, time.September, 30, 0, 0, 0, 0, time.UTC)
	candidate := horizon.Plan{
		ID: "portable-plan", AssetID: assetID, Scenario: "baseline", ExpectedUsefulLifeMonths: 84,
		ReplacementDate: &replacement, LifecycleStage: "approved", ReplacementCostMinor: 525_000,
		Currency: "USD", EffectiveFrom: time.Date(2027, time.April, 1, 0, 0, 0, 0, time.UTC), Revision: 7,
	}
	operation := horizon.ExchangeImportOperation{
		Token: "horizon-postgres-import", OccurredAt: time.Date(2026, time.August, 13, 23, 0, 0, 0, time.UTC),
	}
	result, err := importer.ImportPlan(ctx, operation, candidate)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Horizon plan through PostgreSQL: %#v err=%v", result, err)
	}
	stored, err := service.GetPlan(ctx, candidate.ID)
	if err != nil || stored.OrganizationID != unique || stored.Revision != candidate.Revision ||
		!stored.CreatedAt.Equal(operation.OccurredAt) || !stored.UpdatedAt.Equal(operation.OccurredAt) ||
		stored.ReplacementDate == nil || !stored.ReplacementDate.Equal(*candidate.ReplacementDate) ||
		stored.LifecycleStage != candidate.LifecycleStage || stored.ReplacementCostMinor != candidate.ReplacementCostMinor {
		t.Fatalf("PostgreSQL Horizon import was not lossless: %#v err=%v", stored, err)
	}
	history, err := service.ListPlanHistory(ctx, candidate.ID)
	if err != nil || len(history) != 1 || history[0].Revision != candidate.Revision ||
		history[0].ActorID != "system:exchange" || !history[0].RecordedAt.Equal(operation.OccurredAt) {
		t.Fatalf("unexpected PostgreSQL Horizon import history %#v err=%v", history, err)
	}
	var auditCount int
	var action, actorID string
	if err := database.QueryRowContext(ctx, `SELECT count(*), min(action), min(actor_id) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, unique, operation.Token).Scan(&auditCount, &action, &actorID); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || action != "horizon.plan.created" || actorID != "system:exchange" {
		t.Fatalf("unexpected PostgreSQL Horizon import audit count=%d action=%q actor=%q", auditCount, action, actorID)
	}
	replay, err := importer.ImportPlan(ctx, operation, candidate)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Horizon import: %#v err=%v", replay, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, unique, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("Horizon audit replay duplicated rows: %d", auditCount)
	}
}
