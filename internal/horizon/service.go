package horizon

// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

var (
	stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	scenarioPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	validStages     = stringSet("planned", "in_service", "refresh_due", "approved", "retired")
	validGroups     = stringSet("fiscal_year", "department", "site", "tag", "goal", "asset_class")
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	assets         AssetReader
	finance        FinanceReader
	relationships  RelationshipReader
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct {
	operation ExchangeImportOperation
	revision  int64
}

func NewService(store Store, assets AssetReader, finance FinanceReader, relationships RelationshipReader, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, assets, finance, relationships, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, assets AssetReader, finance FinanceReader, relationships RelationshipReader, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || assets == nil || finance == nil || relationships == nil || auditor == nil {
		return nil, nil, errors.New("Horizon store, Atlas, Ledger, Threads, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, errors.New("Horizon organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	service := &Service{
		store: store, assets: assets, finance: finance, relationships: relationships, writes: writes, auditor: auditor,
		organizationID: configuration.OrganizationID, now: configuration.Now,
	}
	return service, &exchangeImporter{service: service}, nil
}

func (*exchangeImporter) horizonExchangeImporter() {}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (i *exchangeImporter) ImportPlan(ctx context.Context, operation ExchangeImportOperation, candidate Plan) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.Revision < 1 || candidate.OrganizationID != "" ||
		!candidate.CreatedAt.IsZero() || !candidate.UpdatedAt.IsZero() || candidate.DerivedReplacementDate != nil {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	normalized, err := normalizePlanInput(CreatePlanInput{
		ID: candidate.ID, AssetID: candidate.AssetID, Scenario: candidate.Scenario,
		ExpectedUsefulLifeMonths: candidate.ExpectedUsefulLifeMonths, ReplacementDate: candidate.ReplacementDate,
		LifecycleStage: candidate.LifecycleStage, ReplacementCostMinor: candidate.ReplacementCostMinor,
		Currency: candidate.Currency, EffectiveFrom: candidate.EffectiveFrom,
	})
	if err != nil || !stableIDPattern.MatchString(strings.TrimSpace(candidate.ID)) || !samePlanInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation, revision: candidate.Revision})
	existing, err := i.service.store.GetPlan(ctx, i.service.organizationID, candidate.ID)
	if err == nil {
		if !sameExchangePlan(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "horizon.plan.created", existing.ID, horizonAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.CreatePlan(ctx, normalized)
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.store.GetPlan(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangePlan(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = operation.OccurredAt.UTC()
	if !stableIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func (s *Service) ListPlans(ctx context.Context, query ListPlansQuery) ([]Plan, error) {
	query.AssetID = strings.TrimSpace(query.AssetID)
	query.Scenario = strings.ToLower(strings.TrimSpace(query.Scenario))
	if query.AssetID != "" && !stableIDPattern.MatchString(query.AssetID) || query.Scenario != "" && !scenarioPattern.MatchString(query.Scenario) {
		return nil, ErrInvalidInput
	}
	plans, err := s.store.ListPlans(ctx, s.organizationID, query)
	if err != nil {
		return nil, err
	}
	for index := range plans {
		plans[index], err = s.enrichPlan(ctx, plans[index])
		if err != nil {
			return nil, err
		}
	}
	return plans, nil
}

func (s *Service) GetPlan(ctx context.Context, id string) (Plan, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Plan{}, ErrInvalidInput
	}
	plan, err := s.store.GetPlan(ctx, s.organizationID, id)
	if err != nil {
		return Plan{}, err
	}
	return s.enrichPlan(ctx, plan)
}

func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (Plan, error) {
	normalized, err := normalizePlanInput(input)
	if err != nil {
		return Plan{}, err
	}
	asset, err := s.assets.GetAsset(ctx, normalized.AssetID)
	if err != nil {
		return Plan{}, mapAssetError(err)
	}
	id := strings.TrimSpace(normalized.ID)
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Plan{}, fmt.Errorf("create Horizon plan id: %w", err)
		}
	}
	now, revision := s.creationState(ctx)
	plan := Plan{
		ID: id, OrganizationID: s.organizationID, AssetID: normalized.AssetID, Scenario: normalized.Scenario,
		ExpectedUsefulLifeMonths: normalized.ExpectedUsefulLifeMonths, ReplacementDate: cloneDate(normalized.ReplacementDate),
		LifecycleStage: normalized.LifecycleStage, ReplacementCostMinor: normalized.ReplacementCostMinor,
		Currency: normalized.Currency, EffectiveFrom: normalized.EffectiveFrom, Revision: revision, CreatedAt: now, UpdatedAt: now,
	}
	version := versionFromPlan(plan, actorFromContext(ctx), now)
	created, err := s.store.CreatePlan(ctx, plan, version)
	if err != nil {
		return Plan{}, err
	}
	if err := s.audit(ctx, "horizon.plan.created", created.ID, horizonAuditMetadata(created)); err != nil {
		return Plan{}, fmt.Errorf("audit Horizon plan creation: %w", err)
	}
	return deriveReplacementDate(created, asset), nil
}

func (s *Service) UpdatePlan(ctx context.Context, input UpdatePlanInput) (Plan, error) {
	input.ID = strings.TrimSpace(input.ID)
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 || strings.TrimSpace(input.Scenario) == "" || strings.TrimSpace(input.LifecycleStage) == "" {
		return Plan{}, ErrInvalidInput
	}
	normalized, err := normalizePlanInput(CreatePlanInput{
		AssetID: input.AssetID, Scenario: input.Scenario, ExpectedUsefulLifeMonths: input.ExpectedUsefulLifeMonths,
		ReplacementDate: input.ReplacementDate, LifecycleStage: input.LifecycleStage,
		ReplacementCostMinor: input.ReplacementCostMinor, Currency: input.Currency, EffectiveFrom: input.EffectiveFrom,
	})
	if err != nil {
		return Plan{}, err
	}
	if err := s.checkWrite(ctx, input.ID); err != nil {
		return Plan{}, err
	}
	existing, err := s.store.GetPlan(ctx, s.organizationID, input.ID)
	if err != nil {
		return Plan{}, err
	}
	if existing.Revision != input.Revision {
		return Plan{}, ErrConflict
	}
	if normalized.AssetID != existing.AssetID || normalized.Scenario != existing.Scenario || normalized.EffectiveFrom.Before(existing.EffectiveFrom) {
		return Plan{}, ErrInvalidInput
	}
	asset, err := s.assets.GetAsset(ctx, normalized.AssetID)
	if err != nil {
		return Plan{}, mapAssetError(err)
	}
	now := s.now().UTC()
	updated := existing
	updated.ExpectedUsefulLifeMonths = normalized.ExpectedUsefulLifeMonths
	updated.ReplacementDate = cloneDate(normalized.ReplacementDate)
	updated.LifecycleStage = normalized.LifecycleStage
	updated.ReplacementCostMinor = normalized.ReplacementCostMinor
	updated.Currency = normalized.Currency
	updated.EffectiveFrom = normalized.EffectiveFrom
	updated.Revision++
	updated.UpdatedAt = now
	version := versionFromPlan(updated, actorFromContext(ctx), now)
	updated, err = s.store.UpdatePlan(ctx, updated, existing.Revision, version)
	if err != nil {
		return Plan{}, err
	}
	if err := s.audit(ctx, "horizon.plan.updated", updated.ID, map[string]string{
		"assetId": updated.AssetID, "scenario": updated.Scenario, "lifecycleStage": updated.LifecycleStage,
		"effectiveFrom": updated.EffectiveFrom.Format(time.RFC3339), "currency": updated.Currency,
		"revision": strconv.FormatInt(updated.Revision, 10),
	}); err != nil {
		return Plan{}, fmt.Errorf("audit Horizon plan update: %w", err)
	}
	return deriveReplacementDate(updated, asset), nil
}

func (s *Service) ListPlanHistory(ctx context.Context, planID string) ([]PlanVersion, error) {
	planID = strings.TrimSpace(planID)
	if !stableIDPattern.MatchString(planID) {
		return nil, ErrInvalidInput
	}
	plan, err := s.store.GetPlan(ctx, s.organizationID, planID)
	if err != nil {
		return nil, err
	}
	asset, err := s.assets.GetAsset(ctx, plan.AssetID)
	if err != nil {
		return nil, mapAssetError(err)
	}
	versions, err := s.store.ListPlanVersions(ctx, s.organizationID, planID)
	if err != nil {
		return nil, err
	}
	for index := range versions {
		if versions[index].ReplacementDate != nil {
			versions[index].DerivedReplacementDate = cloneDate(versions[index].ReplacementDate)
			continue
		}
		if asset.PurchaseDate != nil {
			date := addCalendarMonths(*asset.PurchaseDate, versions[index].ExpectedUsefulLifeMonths)
			versions[index].DerivedReplacementDate = &date
		}
	}
	return versions, nil
}

func (s *Service) Forecast(ctx context.Context, query ForecastQuery) (Forecast, error) {
	query, err := normalizeForecastQuery(query, s.now())
	if err != nil {
		return Forecast{}, err
	}
	plans, err := s.store.ListPlans(ctx, s.organizationID, ListPlansQuery{})
	if err != nil {
		return Forecast{}, err
	}
	assetList, err := s.assets.ListAssets(ctx, atlas.Query{Limit: 100})
	if err != nil {
		return Forecast{}, mapAssetError(err)
	}
	assetsByID := make(map[string]domain.Asset, len(assetList))
	for _, asset := range assetList {
		assetsByID[asset.ID] = asset
	}
	finance, err := s.finance.Snapshot(ctx)
	if err != nil {
		return Forecast{}, fmt.Errorf("read Ledger forecast inputs: %w", err)
	}
	goals, err := s.relationships.ListGoals(ctx)
	if err != nil {
		return Forecast{}, fmt.Errorf("read Threads goals: %w", err)
	}
	goalNames := make(map[string]string, len(goals))
	for _, goal := range goals {
		goalNames[goal.ID] = goal.Name
	}
	report := Forecast{
		AsOf: query.AsOf, GroupBy: query.GroupBy, Scenarios: append([]string(nil), query.Scenarios...),
		TotalsByKindMinor: make(map[string]int64), Groups: []ForecastGroup{},
	}
	type groupAccumulator struct {
		ForecastGroup
		assets map[string]struct{}
	}
	groups := make(map[string]*groupAccumulator)
	currencies := make(map[string]struct{})
	seenAssets := make(map[string]struct{})
	for _, current := range plans {
		if !contains(query.Scenarios, current.Scenario) {
			continue
		}
		versions, err := s.store.ListPlanVersions(ctx, s.organizationID, current.ID)
		if err != nil {
			return Forecast{}, err
		}
		plan, ok := effectivePlan(current, versions, query.AsOf)
		if !ok || plan.LifecycleStage == "retired" {
			continue
		}
		asset, ok := assetsByID[plan.AssetID]
		if !ok {
			asset, err = s.assets.GetAsset(ctx, plan.AssetID)
			if err != nil {
				return Forecast{}, mapAssetError(err)
			}
			assetsByID[asset.ID] = asset
		}
		plan = deriveReplacementDate(plan, asset)
		if plan.DerivedReplacementDate == nil {
			continue
		}
		fiscalYear := fiscalYearFor(*plan.DerivedReplacementDate, query.FiscalYearStartMonth)
		if fiscalYear < query.FromYear || fiscalYear > query.ToYear {
			continue
		}
		currencies[plan.Currency] = struct{}{}
		if report.PlannedReplacementMinor, ok = addMinor(report.PlannedReplacementMinor, plan.ReplacementCostMinor); !ok {
			return Forecast{}, ErrConflict
		}
		seenAssets[asset.ID] = struct{}{}
		amounts, err := matchingCostAmounts(finance.Costs, plan, fiscalYear)
		if err != nil {
			return Forecast{}, err
		}
		for _, cost := range finance.Costs {
			if cost.AssetID == plan.AssetID && cost.Scenario == plan.Scenario && cost.FiscalPeriod == fmt.Sprintf("FY%d", fiscalYear) {
				currencies[cost.Currency] = struct{}{}
			}
		}
		for kind, amount := range amounts {
			var added bool
			report.TotalsByKindMinor[kind], added = addMinor(report.TotalsByKindMinor[kind], amount)
			if !added {
				return Forecast{}, ErrConflict
			}
		}
		dimensions, err := s.dimensions(ctx, query.GroupBy, asset, fiscalYear, goalNames)
		if err != nil {
			return Forecast{}, err
		}
		for _, dimension := range dimensions {
			mapKey := plan.Scenario + "\x00" + dimension.key
			group := groups[mapKey]
			if group == nil {
				group = &groupAccumulator{ForecastGroup: ForecastGroup{
					Key: dimension.key, Label: dimension.label, Scenario: plan.Scenario, AmountsByKindMinor: make(map[string]int64),
				}, assets: make(map[string]struct{})}
				groups[mapKey] = group
			}
			group.PlannedReplacementMinor, ok = addMinor(group.PlannedReplacementMinor, plan.ReplacementCostMinor)
			if !ok {
				return Forecast{}, ErrConflict
			}
			group.assets[asset.ID] = struct{}{}
			for kind, amount := range amounts {
				group.AmountsByKindMinor[kind], ok = addMinor(group.AmountsByKindMinor[kind], amount)
				if !ok {
					return Forecast{}, ErrConflict
				}
			}
		}
	}
	if len(currencies) > 1 {
		return Forecast{}, ErrMixedCurrency
	}
	for currency := range currencies {
		report.Currency = currency
	}
	report.AssetCount = len(seenAssets)
	for _, group := range groups {
		group.AssetCount = len(group.assets)
		report.Groups = append(report.Groups, group.ForecastGroup)
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].Scenario == report.Groups[j].Scenario {
			if report.Groups[i].Label == report.Groups[j].Label {
				return report.Groups[i].Key < report.Groups[j].Key
			}
			return report.Groups[i].Label < report.Groups[j].Label
		}
		return report.Groups[i].Scenario < report.Groups[j].Scenario
	})
	return report, nil
}

func (s *Service) ExportCSV(ctx context.Context, query ForecastQuery) ([]byte, error) {
	report, err := s.Forecast(ctx, query)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"as_of", "group_by", "key", "label", "scenario", "planned_replacement_minor", "asset_count", "currency", "actual_minor", "estimated_minor", "committed_minor", "normalized_real_minor", "tco_minor"})
	for _, group := range report.Groups {
		_ = writer.Write([]string{
			report.AsOf.Format(time.RFC3339), report.GroupBy, safeCSVCell(group.Key), safeCSVCell(group.Label), safeCSVCell(group.Scenario),
			strconv.FormatInt(group.PlannedReplacementMinor, 10), strconv.Itoa(group.AssetCount), report.Currency,
			strconv.FormatInt(group.AmountsByKindMinor["actual"], 10), strconv.FormatInt(group.AmountsByKindMinor["estimated"], 10),
			strconv.FormatInt(group.AmountsByKindMinor["committed"], 10), strconv.FormatInt(group.AmountsByKindMinor["normalized_real"], 10),
			strconv.FormatInt(group.AmountsByKindMinor["tco"], 10),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("encode Horizon CSV: %w", err)
	}
	return output.Bytes(), nil
}

type dimension struct{ key, label string }

func (s *Service) dimensions(ctx context.Context, groupBy string, asset domain.Asset, fiscalYear int, goalNames map[string]string) ([]dimension, error) {
	switch groupBy {
	case "fiscal_year":
		label := fmt.Sprintf("FY%d", fiscalYear)
		return []dimension{{key: label, label: label}}, nil
	case "department":
		return []dimension{namedDimension(asset.DepartmentID, "No department")}, nil
	case "site":
		return []dimension{namedDimension(asset.SiteID, "No site")}, nil
	case "asset_class":
		return []dimension{namedDimension(asset.Kind, "No asset class")}, nil
	case "tag":
		tags, err := s.relationships.EvaluateTags(ctx, threads.TargetAsset, asset.ID)
		if err != nil {
			return nil, fmt.Errorf("read Threads tags: %w", err)
		}
		result := make([]dimension, 0, len(tags))
		for _, item := range tags {
			if item.State != "suppressed" {
				result = append(result, dimension{key: item.Tag.ID, label: item.Tag.Name})
			}
		}
		if len(result) == 0 {
			result = append(result, dimension{key: "unassigned", label: "No effective tag"})
		}
		return result, nil
	case "goal":
		links, err := s.relationships.ListGoalLinks(ctx, threads.TargetAsset, asset.ID)
		if err != nil {
			return nil, fmt.Errorf("read Threads goals: %w", err)
		}
		result := make([]dimension, 0, len(links))
		for _, link := range links {
			result = append(result, dimension{key: link.GoalID, label: firstNonEmpty(goalNames[link.GoalID], link.GoalID)})
		}
		if len(result) == 0 {
			result = append(result, dimension{key: "unassigned", label: "No linked goal"})
		}
		return result, nil
	default:
		return nil, ErrInvalidInput
	}
}

func normalizePlanInput(input CreatePlanInput) (CreatePlanInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	input.LifecycleStage = strings.ToLower(strings.TrimSpace(input.LifecycleStage))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.LifecycleStage == "" {
		input.LifecycleStage = "planned"
	}
	if input.Scenario == "" {
		input.Scenario = "baseline"
	}
	if input.EffectiveFrom.IsZero() {
		return CreatePlanInput{}, ErrInvalidInput
	}
	input.EffectiveFrom = normalizeDate(input.EffectiveFrom)
	if input.ReplacementDate != nil {
		replacement := normalizeDate(*input.ReplacementDate)
		input.ReplacementDate = &replacement
	}
	if input.ID != "" && !stableIDPattern.MatchString(input.ID) || !stableIDPattern.MatchString(input.AssetID) ||
		!scenarioPattern.MatchString(input.Scenario) || input.ExpectedUsefulLifeMonths < 1 || input.ExpectedUsefulLifeMonths > 1200 ||
		!validStages[input.LifecycleStage] || input.ReplacementCostMinor < 0 || input.ReplacementCostMinor > MaximumExactMinorUnits ||
		!currencyPattern.MatchString(input.Currency) ||
		input.EffectiveFrom.Year() < 1970 || input.EffectiveFrom.Year() > 9999 ||
		input.ReplacementDate != nil && (input.ReplacementDate.Year() < 1970 || input.ReplacementDate.Year() > 9999) {
		return CreatePlanInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeForecastQuery(query ForecastQuery, now time.Time) (ForecastQuery, error) {
	if query.AsOf.IsZero() {
		query.AsOf = now.UTC()
	} else {
		query.AsOf = query.AsOf.UTC()
	}
	if query.FiscalYearStartMonth == 0 {
		query.FiscalYearStartMonth = 1
	}
	if query.GroupBy == "" {
		query.GroupBy = "fiscal_year"
	}
	query.GroupBy = strings.ToLower(strings.TrimSpace(query.GroupBy))
	if query.FromYear == 0 {
		query.FromYear = query.AsOf.Year()
	}
	if query.ToYear == 0 {
		query.ToYear = query.FromYear + 10
	}
	if len(query.Scenarios) == 0 {
		query.Scenarios = []string{"baseline"}
	}
	seen := make(map[string]struct{}, len(query.Scenarios))
	scenarios := make([]string, 0, len(query.Scenarios))
	for _, scenario := range query.Scenarios {
		scenario = strings.ToLower(strings.TrimSpace(scenario))
		if !scenarioPattern.MatchString(scenario) {
			return ForecastQuery{}, ErrInvalidInput
		}
		if _, exists := seen[scenario]; !exists {
			seen[scenario] = struct{}{}
			scenarios = append(scenarios, scenario)
		}
	}
	sort.Strings(scenarios)
	query.Scenarios = scenarios
	if len(scenarios) > 5 || query.FiscalYearStartMonth < 1 || query.FiscalYearStartMonth > 12 ||
		!validGroups[query.GroupBy] || query.FromYear < 1970 || query.ToYear > 9999 ||
		query.ToYear < query.FromYear || query.ToYear-query.FromYear > 50 {
		return ForecastQuery{}, ErrInvalidInput
	}
	return query, nil
}

func effectivePlan(current Plan, versions []PlanVersion, asOf time.Time) (Plan, bool) {
	var selected PlanVersion
	found := false
	for _, version := range versions {
		if version.EffectiveFrom.After(asOf) {
			continue
		}
		if !found || version.EffectiveFrom.After(selected.EffectiveFrom) || version.EffectiveFrom.Equal(selected.EffectiveFrom) && version.Revision > selected.Revision {
			selected, found = version, true
		}
	}
	if !found {
		return Plan{}, false
	}
	return Plan{
		ID: selected.PlanID, OrganizationID: selected.OrganizationID, AssetID: selected.AssetID, Scenario: selected.Scenario,
		ExpectedUsefulLifeMonths: selected.ExpectedUsefulLifeMonths, ReplacementDate: cloneDate(selected.ReplacementDate),
		LifecycleStage: selected.LifecycleStage, ReplacementCostMinor: selected.ReplacementCostMinor, Currency: selected.Currency,
		EffectiveFrom: selected.EffectiveFrom, Revision: selected.Revision, CreatedAt: current.CreatedAt, UpdatedAt: selected.RecordedAt,
	}, true
}

func matchingCostAmounts(costs []ledger.CostRecord, plan Plan, fiscalYear int) (map[string]int64, error) {
	result := make(map[string]int64)
	period := fmt.Sprintf("FY%d", fiscalYear)
	for _, cost := range costs {
		if cost.AssetID == plan.AssetID && cost.Scenario == plan.Scenario && cost.FiscalPeriod == period {
			added, ok := addMinor(result[cost.Kind], cost.AmountMinor)
			if !ok {
				return nil, ErrConflict
			}
			result[cost.Kind] = added
		}
	}
	return result, nil
}

func fiscalYearFor(date time.Time, startMonth int) int {
	if startMonth == 1 || int(date.Month()) < startMonth {
		return date.Year()
	}
	return date.Year() + 1
}

func deriveReplacementDate(plan Plan, asset domain.Asset) Plan {
	if plan.ReplacementDate != nil {
		plan.DerivedReplacementDate = cloneDate(plan.ReplacementDate)
		return plan
	}
	if asset.PurchaseDate != nil {
		date := addCalendarMonths(*asset.PurchaseDate, plan.ExpectedUsefulLifeMonths)
		plan.DerivedReplacementDate = &date
	}
	return plan
}

// addCalendarMonths deliberately documents the useful-life rule used by
// Horizon: Go calendar AddDate semantics, normalized to a UTC calendar date.
func addCalendarMonths(value time.Time, months int) time.Time {
	return normalizeDate(value.AddDate(0, months, 0))
}

func (s *Service) enrichPlan(ctx context.Context, plan Plan) (Plan, error) {
	asset, err := s.assets.GetAsset(ctx, plan.AssetID)
	if err != nil {
		return Plan{}, mapAssetError(err)
	}
	return deriveReplacementDate(plan, asset), nil
}

func versionFromPlan(plan Plan, actorID string, recordedAt time.Time) PlanVersion {
	return PlanVersion{
		PlanID: plan.ID, OrganizationID: plan.OrganizationID, AssetID: plan.AssetID, Scenario: plan.Scenario,
		ExpectedUsefulLifeMonths: plan.ExpectedUsefulLifeMonths, ReplacementDate: cloneDate(plan.ReplacementDate),
		LifecycleStage: plan.LifecycleStage, ReplacementCostMinor: plan.ReplacementCostMinor, Currency: plan.Currency,
		EffectiveFrom: plan.EffectiveFrom, Revision: plan.Revision, ActorID: actorID, RecordedAt: recordedAt,
	}
}

func namedDimension(value, emptyLabel string) dimension {
	if value == "" {
		return dimension{key: "unassigned", label: emptyLabel}
	}
	return dimension{key: value, label: value}
}

func normalizeDate(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func cloneDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func addMinor(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > MaximumExactMinorUnits || right > MaximumExactMinorUnits || left > MaximumExactMinorUnits-right {
		return 0, false
	}
	return left + right, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}

func mapAssetError(err error) error {
	if errors.Is(err, atlas.ErrNotFound) {
		return ErrReferenceMissing
	}
	if errors.Is(err, atlas.ErrInvalidInput) {
		return ErrInvalidInput
	}
	return fmt.Errorf("read Atlas asset: %w", err)
}

func actorFromContext(ctx context.Context) string {
	if _, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); ok {
		return "system:exchange"
	}
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:horizon"
}

func (s *Service) audit(ctx context.Context, action, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	state, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext)
	if importing {
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: state.operation.Token}
		ok = true
	}
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	metadata["requirementId"] = RequirementID
	eventID := ""
	occurredAt := s.now().UTC()
	if importing {
		digest := sha256.Sum256([]byte(strings.Join([]string{s.organizationID, state.operation.Token, action, "lifecycle_plan", resourceID}, "\x00")))
		eventID = fmt.Sprintf("%x", digest[:])
		occurredAt = state.operation.OccurredAt.UTC()
	} else {
		var err error
		eventID, err = foundation.NewCorrelationID()
		if err != nil {
			return err
		}
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: "lifecycle_plan", ResourceID: resourceID, OccurredAt: occurredAt, Metadata: metadata,
	})
}

func (s *Service) creationState(ctx context.Context) (time.Time, int64) {
	if state, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); ok && state.revision > 0 && !state.operation.OccurredAt.IsZero() {
		return state.operation.OccurredAt.UTC(), state.revision
	}
	return s.now().UTC(), 1
}

func (s *Service) checkWrite(ctx context.Context, id string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, "horizon.plan", id)
}

func samePlanInput(candidate Plan, normalized CreatePlanInput) bool {
	return candidate.ID == normalized.ID && candidate.AssetID == normalized.AssetID && candidate.Scenario == normalized.Scenario &&
		candidate.ExpectedUsefulLifeMonths == normalized.ExpectedUsefulLifeMonths && equalOptionalDate(candidate.ReplacementDate, normalized.ReplacementDate) &&
		candidate.LifecycleStage == normalized.LifecycleStage && candidate.ReplacementCostMinor == normalized.ReplacementCostMinor &&
		candidate.Currency == normalized.Currency && candidate.EffectiveFrom.Equal(normalized.EffectiveFrom)
}

func sameExchangePlan(left, right Plan) bool {
	return left.ID == right.ID && left.AssetID == right.AssetID && left.Scenario == right.Scenario &&
		left.ExpectedUsefulLifeMonths == right.ExpectedUsefulLifeMonths && equalOptionalDate(left.ReplacementDate, right.ReplacementDate) &&
		left.LifecycleStage == right.LifecycleStage && left.ReplacementCostMinor == right.ReplacementCostMinor &&
		left.Currency == right.Currency && left.EffectiveFrom.Equal(right.EffectiveFrom) && left.Revision == right.Revision
}

func equalOptionalDate(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func horizonAuditMetadata(plan Plan) map[string]string {
	return map[string]string{
		"assetId": plan.AssetID, "scenario": plan.Scenario, "lifecycleStage": plan.LifecycleStage,
		"effectiveFrom": plan.EffectiveFrom.Format(time.RFC3339), "currency": plan.Currency,
		"revision": strconv.FormatInt(plan.Revision, 10),
	}
}
