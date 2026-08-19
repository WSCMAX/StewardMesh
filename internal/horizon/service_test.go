package horizon_test

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

var horizonNow = time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
var horizonAssetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type horizonAssets struct {
	items map[string]domain.Asset
}

func (r *horizonAssets) ListAssets(_ context.Context, query atlas.Query) ([]domain.Asset, error) {
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

func (r *horizonAssets) GetAsset(_ context.Context, id string) (domain.Asset, error) {
	if !horizonAssetIDPattern.MatchString(id) {
		return domain.Asset{}, atlas.ErrInvalidInput
	}
	item, exists := r.items[id]
	if !exists {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return item, nil
}

func (r *horizonAssets) GetModel(_ context.Context, id string) (domain.AssetModel, error) {
	return domain.AssetModel{}, atlas.ErrNotFound
}

type horizonFinance struct {
	snapshot ledger.Snapshot
}

func (r *horizonFinance) Snapshot(context.Context) (ledger.Snapshot, error) {
	return r.snapshot, nil
}

type horizonRelationships struct {
	goals []threads.Goal
	tags  map[string][]threads.EffectiveTag
	links map[string][]threads.GoalLink
}

func (r *horizonRelationships) ListGoals(context.Context) ([]threads.Goal, error) {
	return append([]threads.Goal(nil), r.goals...), nil
}

func (r *horizonRelationships) EvaluateTags(_ context.Context, targetType threads.TargetType, targetID string) ([]threads.EffectiveTag, error) {
	if targetType != threads.TargetAsset {
		return nil, threads.ErrInvalidInput
	}
	return append([]threads.EffectiveTag(nil), r.tags[targetID]...), nil
}

func (r *horizonRelationships) ListGoalLinks(_ context.Context, targetType threads.TargetType, targetID string) ([]threads.GoalLink, error) {
	if targetType != threads.TargetAsset {
		return nil, threads.ErrInvalidInput
	}
	return append([]threads.GoalLink(nil), r.links[targetID]...), nil
}

type horizonAuditor struct {
	events []foundation.AuditEvent
}

func (a *horizonAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

type horizonFixture struct {
	service       *horizon.Service
	store         *repository.MemoryHorizonStore
	assets        *horizonAssets
	finance       *horizonFinance
	relationships *horizonRelationships
	auditor       *horizonAuditor
}

func newHorizonFixture(t *testing.T, assets ...domain.Asset) *horizonFixture {
	t.Helper()
	assetMap := make(map[string]domain.Asset, len(assets))
	for _, asset := range assets {
		assetMap[asset.ID] = asset
	}
	fixture := &horizonFixture{
		store:         repository.NewMemoryHorizonStore(),
		assets:        &horizonAssets{items: assetMap},
		finance:       &horizonFinance{snapshot: ledger.Snapshot{}},
		relationships: &horizonRelationships{tags: make(map[string][]threads.EffectiveTag), links: make(map[string][]threads.GoalLink)},
		auditor:       &horizonAuditor{},
	}
	service, err := horizon.NewService(fixture.store, fixture.assets, fixture.finance, fixture.relationships, fixture.auditor, horizon.ServiceConfig{
		OrganizationID: "example-org",
		Now:            func() time.Time { return horizonNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = service
	return fixture
}

func horizonAsset(id string, purchaseDate *time.Time) domain.Asset {
	return domain.Asset{
		ID: id, OrganizationID: "example-org", Name: "Asset " + id, Kind: "server",
		SiteID: "site-1", DepartmentID: "department-1", Status: "active", PurchaseDate: purchaseDate,
		Revision: 1, CreatedAt: horizonNow, UpdatedAt: horizonNow,
	}
}

func createHorizonPlan(t *testing.T, fixture *horizonFixture, input horizon.CreatePlanInput) horizon.Plan {
	t.Helper()
	created, err := fixture.service.CreatePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func baseHorizonInput(id, assetID, scenario string, replacementDate *time.Time, replacementCost int64) horizon.CreatePlanInput {
	return horizon.CreatePlanInput{
		ID: id, AssetID: assetID, Scenario: scenario, ExpectedUsefulLifeMonths: 36,
		ReplacementDate: replacementDate, LifecycleStage: "planned", ReplacementCostMinor: replacementCost,
		Currency: "USD", EffectiveFrom: time.Date(2026, time.January, 1, 9, 30, 0, 0, time.UTC),
	}
}

func horizonForecastQuery(groupBy string) horizon.ForecastQuery {
	return horizon.ForecastQuery{
		Scenarios: []string{"baseline"}, AsOf: time.Date(2026, time.December, 31, 12, 0, 0, 0, time.UTC),
		FromYear: 2026, ToYear: 2026, FiscalYearStartMonth: 1, GroupBy: groupBy,
	}
}

func TestCreatePlanDerivesReplacementDateAndRecordsSafeAudit(t *testing.T) {
	purchaseDate := time.Date(2024, time.January, 31, 18, 45, 0, 0, time.UTC)
	fixture := newHorizonFixture(t, horizonAsset("asset-1", &purchaseDate))
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-1", CorrelationID: "correlation-1",
	})
	created, err := fixture.service.CreatePlan(ctx, horizon.CreatePlanInput{
		ID: "plan-1", AssetID: "asset-1", Scenario: " Baseline ", ExpectedUsefulLifeMonths: 1,
		LifecycleStage: "", ReplacementCostMinor: 125_000, Currency: "usd",
		EffectiveFrom: time.Date(2026, time.January, 2, 17, 30, 0, 0, time.FixedZone("central", -6*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	expectedReplacement := purchaseDate.AddDate(0, 1, 0)
	expectedReplacement = time.Date(expectedReplacement.Year(), expectedReplacement.Month(), expectedReplacement.Day(), 0, 0, 0, 0, time.UTC)
	if created.DerivedReplacementDate == nil || !created.DerivedReplacementDate.Equal(expectedReplacement) {
		t.Fatalf("expected Go AddDate-derived replacement %s, got %#v", expectedReplacement, created.DerivedReplacementDate)
	}
	if created.Scenario != "baseline" || created.LifecycleStage != "planned" || created.Currency != "USD" || created.Revision != 1 || created.EffectiveFrom.Hour() != 0 {
		t.Fatalf("unexpected normalized plan %#v", created)
	}
	history, err := fixture.service.ListPlanHistory(context.Background(), created.ID)
	if err != nil || len(history) != 1 || history[0].ActorID != "account-1" || history[0].Revision != 1 ||
		history[0].DerivedReplacementDate == nil || !history[0].DerivedReplacementDate.Equal(expectedReplacement) {
		t.Fatalf("unexpected initial immutable version %#v err=%v", history, err)
	}
	if len(fixture.auditor.events) != 1 {
		t.Fatalf("expected one audit event, got %#v", fixture.auditor.events)
	}
	event := fixture.auditor.events[0]
	if event.Action != "horizon.plan.created" || event.ResourceType != "lifecycle_plan" || event.ResourceID != created.ID ||
		event.ActorID != "account-1" || event.CorrelationID != "correlation-1" || event.Metadata["requirementId"] != horizon.RequirementID ||
		event.Metadata["scenario"] != "baseline" || event.Metadata["revision"] != "1" {
		t.Fatalf("unexpected Horizon audit event %#v", event)
	}
	if _, exists := event.Metadata["replacementCostMinor"]; exists {
		t.Fatalf("audit metadata must not contain monetary values: %#v", event.Metadata)
	}
}

func TestUpdatePreservesImmutableHistoryRejectsStaleRevisionAndSelectsEffectiveVersion(t *testing.T) {
	fixture := newHorizonFixture(t, horizonAsset("asset-1", nil))
	firstDate := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	created := createHorizonPlan(t, fixture, baseHorizonInput("plan-1", "asset-1", "baseline", &firstDate, 100))
	secondDate := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	update := horizon.UpdatePlanInput{
		ID: created.ID, AssetID: created.AssetID, Scenario: created.Scenario, ExpectedUsefulLifeMonths: 48,
		ReplacementDate: &secondDate, LifecycleStage: "approved", ReplacementCostMinor: 250, Currency: "USD",
		EffectiveFrom: time.Date(2026, time.July, 1, 15, 0, 0, 0, time.UTC), Revision: created.Revision,
	}
	updated, err := fixture.service.UpdatePlan(context.Background(), update)
	if err != nil || updated.Revision != 2 || updated.ReplacementCostMinor != 250 {
		t.Fatalf("unexpected update %#v err=%v", updated, err)
	}
	if _, err := fixture.service.UpdatePlan(context.Background(), update); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected stale revision conflict, got %v", err)
	}
	history, err := fixture.service.ListPlanHistory(context.Background(), created.ID)
	if err != nil || len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 1 ||
		history[1].ReplacementCostMinor != 100 || history[1].LifecycleStage != "planned" || history[0].ReplacementCostMinor != 250 {
		t.Fatalf("unexpected immutable history %#v err=%v", history, err)
	}
	before, err := fixture.service.Forecast(context.Background(), horizon.ForecastQuery{
		Scenarios: []string{"baseline"}, AsOf: time.Date(2026, time.June, 30, 23, 59, 0, 0, time.UTC),
		FromYear: 2026, ToYear: 2026, FiscalYearStartMonth: 1, GroupBy: "fiscal_year",
	})
	if err != nil || before.PlannedReplacementMinor != 100 {
		t.Fatalf("expected the earlier effective version, report=%#v err=%v", before, err)
	}
	after, err := fixture.service.Forecast(context.Background(), horizon.ForecastQuery{
		Scenarios: []string{"baseline"}, AsOf: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		FromYear: 2026, ToYear: 2026, FiscalYearStartMonth: 1, GroupBy: "fiscal_year",
	})
	if err != nil || after.PlannedReplacementMinor != 250 {
		t.Fatalf("expected the future version at its effective boundary, report=%#v err=%v", after, err)
	}
}

func TestForecastKeepsScenariosSeparateAcrossFiscalBoundary(t *testing.T) {
	fixture := newHorizonFixture(t, horizonAsset("asset-1", nil))
	september := time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC)
	october := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-baseline", "asset-1", "baseline", &september, 100))
	createHorizonPlan(t, fixture, baseHorizonInput("plan-optimistic", "asset-1", "optimistic", &october, 250))
	report, err := fixture.service.Forecast(context.Background(), horizon.ForecastQuery{
		Scenarios: []string{"optimistic", "baseline", "baseline"}, AsOf: horizonNow,
		FromYear: 2026, ToYear: 2027, FiscalYearStartMonth: 10, GroupBy: "fiscal_year",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Scenarios, ",") != "baseline,optimistic" || report.PlannedReplacementMinor != 350 || report.AssetCount != 1 || len(report.Groups) != 2 {
		t.Fatalf("unexpected scenario comparison %#v", report)
	}
	groups := make(map[string]horizon.ForecastGroup)
	for _, group := range report.Groups {
		groups[group.Scenario+":"+group.Key] = group
	}
	if groups["baseline:FY2026"].PlannedReplacementMinor != 100 || groups["optimistic:FY2027"].PlannedReplacementMinor != 250 {
		t.Fatalf("unexpected fiscal boundary groups %#v", report.Groups)
	}
}

func TestForecastSupportsEveryDimensionAndMakesMultiTagAndGoalRowsNonAdditive(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	replacement := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", &replacement, 100))
	fixture.relationships.tags[asset.ID] = []threads.EffectiveTag{
		{Tag: threads.Tag{ID: "tag-a", Name: "Reliability"}, State: "explicit"},
		{Tag: threads.Tag{ID: "tag-b", Name: "Sustainability"}, State: "inherited"},
		{Tag: threads.Tag{ID: "tag-hidden", Name: "Hidden"}, State: "suppressed"},
	}
	fixture.relationships.goals = []threads.Goal{{ID: "goal-a", Name: "Availability"}, {ID: "goal-b", Name: "Efficiency"}}
	fixture.relationships.links[asset.ID] = []threads.GoalLink{{GoalID: "goal-a"}, {GoalID: "goal-b"}}

	for _, test := range []struct {
		groupBy string
		keys    []string
	}{
		{groupBy: "fiscal_year", keys: []string{"FY2026"}},
		{groupBy: "department", keys: []string{"department-1"}},
		{groupBy: "site", keys: []string{"site-1"}},
		{groupBy: "asset_class", keys: []string{"server"}},
		{groupBy: "tag", keys: []string{"tag-a", "tag-b"}},
		{groupBy: "goal", keys: []string{"goal-a", "goal-b"}},
	} {
		t.Run(test.groupBy, func(t *testing.T) {
			report, err := fixture.service.Forecast(context.Background(), horizonForecastQuery(test.groupBy))
			if err != nil {
				t.Fatal(err)
			}
			actualKeys := make([]string, 0, len(report.Groups))
			var groupedTotal int64
			for _, group := range report.Groups {
				actualKeys = append(actualKeys, group.Key)
				groupedTotal += group.PlannedReplacementMinor
				if group.AssetCount != 1 {
					t.Fatalf("expected a distinct asset count in %#v", group)
				}
			}
			sort.Strings(actualKeys)
			if strings.Join(actualKeys, ",") != strings.Join(test.keys, ",") || report.PlannedReplacementMinor != 100 || report.AssetCount != 1 {
				t.Fatalf("unexpected %s grouping %#v", test.groupBy, report)
			}
			if test.groupBy == "tag" || test.groupBy == "goal" {
				if groupedTotal != 200 {
					t.Fatalf("multi-valued rows should repeat the asset and remain non-additive, got %d", groupedTotal)
				}
			} else if groupedTotal != report.PlannedReplacementMinor {
				t.Fatalf("additive grouping mismatch: groups=%d total=%d", groupedTotal, report.PlannedReplacementMinor)
			}
		})
	}
}

func TestForecastKeepsLedgerCostKindsSeparate(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	replacement := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", &replacement, 500))
	expected := map[string]int64{"actual": 10, "estimated": 20, "committed": 30, "normalized_real": 40, "tco": 50}
	for kind, amount := range expected {
		fixture.finance.snapshot.Costs = append(fixture.finance.snapshot.Costs, ledger.CostRecord{
			ID: "cost-" + kind, AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2026",
			Kind: kind, Currency: "USD", AmountMinor: amount,
		})
	}
	fixture.finance.snapshot.Costs = append(fixture.finance.snapshot.Costs,
		ledger.CostRecord{ID: "wrong-scenario", AssetID: asset.ID, Scenario: "optimistic", FiscalPeriod: "FY2026", Kind: "actual", Currency: "USD", AmountMinor: 999},
		ledger.CostRecord{ID: "wrong-period", AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2027", Kind: "actual", Currency: "USD", AmountMinor: 999},
		ledger.CostRecord{ID: "wrong-asset", AssetID: "asset-2", Scenario: "baseline", FiscalPeriod: "FY2026", Kind: "actual", Currency: "USD", AmountMinor: 999},
	)
	report, err := fixture.service.Forecast(context.Background(), horizonForecastQuery("fiscal_year"))
	if err != nil || len(report.Groups) != 1 {
		t.Fatalf("unexpected cost forecast %#v err=%v", report, err)
	}
	for kind, amount := range expected {
		if report.TotalsByKindMinor[kind] != amount || report.Groups[0].AmountsByKindMinor[kind] != amount {
			t.Fatalf("cost kind %s was not kept separate: %#v", kind, report)
		}
	}
}

func TestForecastRejectsMinorUnitOverflow(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	replacement := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", &replacement, 100))
	fixture.finance.snapshot.Costs = []ledger.CostRecord{
		{ID: "cost-1", AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2026", Kind: "actual", Currency: "USD", AmountMinor: horizon.MaximumExactMinorUnits},
		{ID: "cost-2", AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2026", Kind: "actual", Currency: "USD", AmountMinor: 1},
	}
	if _, err := fixture.service.Forecast(context.Background(), horizonForecastQuery("fiscal_year")); !errors.Is(err, horizon.ErrConflict) {
		t.Fatalf("expected minor-unit overflow conflict, got %v", err)
	}
}

func TestForecastRejectsMixedPlanAndLedgerCurrencies(t *testing.T) {
	t.Run("plans", func(t *testing.T) {
		first, second := horizonAsset("asset-1", nil), horizonAsset("asset-2", nil)
		fixture := newHorizonFixture(t, first, second)
		replacement := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
		createHorizonPlan(t, fixture, baseHorizonInput("plan-1", first.ID, "baseline", &replacement, 100))
		euro := baseHorizonInput("plan-2", second.ID, "baseline", &replacement, 200)
		euro.Currency = "EUR"
		createHorizonPlan(t, fixture, euro)
		if _, err := fixture.service.Forecast(context.Background(), horizonForecastQuery("fiscal_year")); !errors.Is(err, horizon.ErrMixedCurrency) {
			t.Fatalf("expected mixed plan currency rejection, got %v", err)
		}
	})

	t.Run("ledger facts", func(t *testing.T) {
		asset := horizonAsset("asset-1", nil)
		fixture := newHorizonFixture(t, asset)
		replacement := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
		createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", &replacement, 100))
		fixture.finance.snapshot.Costs = []ledger.CostRecord{
			{ID: "usd", AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2026", Kind: "actual", Currency: "USD", AmountMinor: 10},
			{ID: "eur", AssetID: asset.ID, Scenario: "baseline", FiscalPeriod: "FY2026", Kind: "estimated", Currency: "EUR", AmountMinor: 20},
		}
		if _, err := fixture.service.Forecast(context.Background(), horizonForecastQuery("fiscal_year")); !errors.Is(err, horizon.ErrMixedCurrency) {
			t.Fatalf("expected mixed Ledger currency rejection, got %v", err)
		}
	})
}

func TestCSVExportNeutralizesFormulaCapableDimensionLabels(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	replacement := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", &replacement, 100))
	fixture.relationships.goals = []threads.Goal{{ID: "goal-1", Name: `=HYPERLINK("https://example.test")`}}
	fixture.relationships.links[asset.ID] = []threads.GoalLink{{GoalID: "goal-1"}}
	content, err := fixture.service.ExportCSV(context.Background(), horizonForecastQuery("goal"))
	if err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(strings.NewReader(string(content)))
	rows, err := reader.ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("unexpected CSV %q err=%v", content, err)
	}
	if rows[1][3] != `'=HYPERLINK("https://example.test")` {
		t.Fatalf("formula-capable label was not neutralized: %#v", rows[1])
	}
	if _, err := reader.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected complete CSV consumption, got %v", err)
	}
}

func TestHorizonRejectsInvalidInputsAndMissingReferences(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	validDate := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	base := baseHorizonInput("plan-valid", asset.ID, "baseline", &validDate, 100)
	for _, test := range []struct {
		name   string
		mutate func(*horizon.CreatePlanInput)
	}{
		{name: "invalid id", mutate: func(input *horizon.CreatePlanInput) { input.ID = "bad id" }},
		{name: "invalid asset", mutate: func(input *horizon.CreatePlanInput) { input.AssetID = "bad!" }},
		{name: "invalid scenario", mutate: func(input *horizon.CreatePlanInput) { input.Scenario = "bad scenario" }},
		{name: "zero useful life without model default", mutate: func(input *horizon.CreatePlanInput) {
			input.ExpectedUsefulLifeMonths = 0
			input.AssetID = "missing-model-default"
		}},
		{name: "excessive useful life", mutate: func(input *horizon.CreatePlanInput) { input.ExpectedUsefulLifeMonths = 1201 }},
		{name: "invalid stage", mutate: func(input *horizon.CreatePlanInput) { input.LifecycleStage = "disposed" }},
		{name: "negative cost", mutate: func(input *horizon.CreatePlanInput) { input.ReplacementCostMinor = -1 }},
		{name: "cost exceeds exact browser boundary", mutate: func(input *horizon.CreatePlanInput) { input.ReplacementCostMinor = horizon.MaximumExactMinorUnits + 1 }},
		{name: "invalid currency", mutate: func(input *horizon.CreatePlanInput) { input.Currency = "US" }},
		{name: "missing effective date", mutate: func(input *horizon.CreatePlanInput) { input.EffectiveFrom = time.Time{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			err := error(nil)
			if test.name == "zero useful life without model default" {
				fixture.assets.items["missing-model-default"] = domain.Asset{ID: "missing-model-default", Kind: "server"}
				_, err = fixture.service.CreatePlan(context.Background(), input)
				delete(fixture.assets.items, "missing-model-default")
			} else {
				_, err = fixture.service.CreatePlan(context.Background(), input)
			}
			if !errors.Is(err, horizon.ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", err)
			}
		})
	}
	missing := base
	missing.ID, missing.AssetID = "plan-missing", "missing-asset"
	if _, err := fixture.service.CreatePlan(context.Background(), missing); !errors.Is(err, horizon.ErrReferenceMissing) {
		t.Fatalf("expected missing Atlas reference, got %v", err)
	}
	if _, err := fixture.service.ListPlans(context.Background(), horizon.ListPlansQuery{Scenario: "bad scenario"}); !errors.Is(err, horizon.ErrInvalidInput) {
		t.Fatalf("expected invalid list filter, got %v", err)
	}
	for _, query := range []horizon.ForecastQuery{
		{GroupBy: "unknown"},
		{GroupBy: "site", FiscalYearStartMonth: 13},
		{GroupBy: "site", FromYear: 2027, ToYear: 2026},
		{GroupBy: "site", FromYear: 2026, ToYear: 2077},
		{GroupBy: "site", Scenarios: []string{"one", "two", "three", "four", "five", "six"}},
	} {
		if _, err := fixture.service.Forecast(context.Background(), query); !errors.Is(err, horizon.ErrInvalidInput) {
			t.Fatalf("expected invalid forecast query %#v, got %v", query, err)
		}
	}
}

func TestPlanWithoutPurchaseOrReplacementDateIsExcludedFromDatedForecast(t *testing.T) {
	asset := horizonAsset("asset-1", nil)
	fixture := newHorizonFixture(t, asset)
	created := createHorizonPlan(t, fixture, baseHorizonInput("plan-1", asset.ID, "baseline", nil, 100))
	if created.DerivedReplacementDate != nil {
		t.Fatalf("expected an undated valid plan, got %#v", created)
	}
	report, err := fixture.service.Forecast(context.Background(), horizonForecastQuery("fiscal_year"))
	if err != nil || report.PlannedReplacementMinor != 0 || report.AssetCount != 0 || len(report.Groups) != 0 {
		t.Fatalf("undated plan must not enter a dated forecast: %#v err=%v", report, err)
	}
}

func TestForecastGroupAssetsListsMatchingPlans(t *testing.T) {
	assetOne := horizonAsset("asset-1", nil)
	assetTwo := horizonAsset("asset-2", nil)
	fixture := newHorizonFixture(t, assetOne, assetTwo)
	replacement := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	createHorizonPlan(t, fixture, baseHorizonInput("plan-1", assetOne.ID, "baseline", &replacement, 100))
	createHorizonPlan(t, fixture, baseHorizonInput("plan-2", assetTwo.ID, "baseline", &replacement, 200))
	createHorizonPlan(t, fixture, baseHorizonInput("plan-3", assetOne.ID, "optimistic", &replacement, 300))
	report, err := fixture.service.ForecastGroupAssets(context.Background(), horizon.ForecastGroupAssetsQuery{
		ForecastQuery: horizonForecastQuery("fiscal_year"), Scenario: "baseline", GroupKey: "FY2026",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Label != "FY2026" || report.Currency != "USD" || len(report.Items) != 2 {
		t.Fatalf("unexpected group assets %#v", report)
	}
	if report.Items[0].AssetID != "asset-1" || report.Items[1].AssetID != "asset-2" {
		t.Fatalf("expected asset name ordering, got %#v", report.Items)
	}
}
