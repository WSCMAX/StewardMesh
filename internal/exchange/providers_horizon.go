package exchange

// Requirements: REQ-EXCHANGE-001, REQ-HORIZON-001, REQ-PATTERNS-001. Features: migration.packages, lifecycle.planning, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/horizon"
)

const horizonPlanRecordType = "horizon.plan"

type HorizonProvider struct {
	service  *horizon.Service
	importer horizon.ExchangeImporter
}

type horizonPlanPayload struct {
	AssetID                  string `json:"assetId"`
	Scenario                 string `json:"scenario"`
	ExpectedUsefulLifeMonths int    `json:"expectedUsefulLifeMonths"`
	ReplacementDate          string `json:"replacementDate,omitempty"`
	LifecycleStage           string `json:"lifecycleStage"`
	ReplacementCostMinor     int64  `json:"replacementCostMinor"`
	Currency                 string `json:"currency"`
	EffectiveFrom            string `json:"effectiveFrom"`
}

func NewHorizonProvider(service *horizon.Service, importer horizon.ExchangeImporter) (*HorizonProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Horizon service and its construction-time Exchange importer are required")
	}
	return &HorizonProvider{service: service, importer: importer}, nil
}

func (*HorizonProvider) Types() []string { return []string{horizonPlanRecordType} }

func (p *HorizonProvider) ListRecords(ctx context.Context) ([]Record, error) {
	plans, err := p.service.ListPlans(ctx, horizon.ListPlansQuery{})
	if err != nil {
		return nil, err
	}
	if len(plans) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(plans))
	for _, plan := range plans {
		payload, err := json.Marshal(horizonPlanPayload{
			AssetID: plan.AssetID, Scenario: plan.Scenario, ExpectedUsefulLifeMonths: plan.ExpectedUsefulLifeMonths,
			ReplacementDate: horizonOptionalDate(plan.ReplacementDate), LifecycleStage: plan.LifecycleStage,
			ReplacementCostMinor: plan.ReplacementCostMinor, Currency: plan.Currency, EffectiveFrom: horizonDate(plan.EffectiveFrom),
		})
		if err != nil || len(payload) > MaximumPayloadBytes {
			return nil, ErrInvalidInput
		}
		result = append(result, Record{
			Type: horizonPlanRecordType, ID: plan.ID, Revision: plan.Revision,
			Dependencies: horizonPlanDependencies(plan), Ownership: OwnershipMetadata{State: "local"}, Payload: payload,
		})
	}
	return result, nil
}

func (p *HorizonProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	if reference.Type != horizonPlanRecordType {
		return false, nil
	}
	_, err := p.service.GetPlan(ctx, reference.ID)
	if errors.Is(err, horizon.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (p *HorizonProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeHorizonPlanRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	current, err := p.service.GetPlan(ctx, record.ID)
	if errors.Is(err, horizon.ErrNotFound) {
		return false, nil
	}
	return err == nil && sameHorizonPlan(current, candidate), err
}

func (p *HorizonProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodeHorizonPlanRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	result, err := p.importer.ImportPlan(ctx, horizon.ExchangeImportOperation{
		Token: operation.Token, OccurredAt: operation.OccurredAt,
	}, candidate)
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, horizon.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, horizon.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, horizon.ErrReferenceMissing), errors.Is(err, horizon.ErrNotFound):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeHorizonPlanRecord(record Record) (horizon.Plan, []Reference, error) {
	if record.Type != horizonPlanRecordType || record.Revision < 1 || len(record.Payload) == 0 || len(record.Payload) > MaximumPayloadBytes {
		return horizon.Plan{}, nil, ErrInvalidInput
	}
	var payload horizonPlanPayload
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return horizon.Plan{}, nil, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return horizon.Plan{}, nil, ErrInvalidInput
	}
	replacementDate, err := parseHorizonOptionalDate(payload.ReplacementDate)
	if err != nil {
		return horizon.Plan{}, nil, err
	}
	effectiveFrom, err := parseHorizonDate(payload.EffectiveFrom)
	if err != nil || payload.AssetID != strings.TrimSpace(payload.AssetID) ||
		payload.Scenario != strings.ToLower(strings.TrimSpace(payload.Scenario)) ||
		payload.LifecycleStage != strings.ToLower(strings.TrimSpace(payload.LifecycleStage)) ||
		payload.Currency != strings.ToUpper(strings.TrimSpace(payload.Currency)) {
		return horizon.Plan{}, nil, ErrInvalidInput
	}
	plan := horizon.Plan{
		ID: record.ID, AssetID: payload.AssetID, Scenario: payload.Scenario,
		ExpectedUsefulLifeMonths: payload.ExpectedUsefulLifeMonths, ReplacementDate: replacementDate,
		LifecycleStage: payload.LifecycleStage, ReplacementCostMinor: payload.ReplacementCostMinor,
		Currency: payload.Currency, EffectiveFrom: effectiveFrom, Revision: record.Revision,
	}
	return plan, horizonPlanDependencies(plan), nil
}

func horizonPlanDependencies(plan horizon.Plan) []Reference {
	return []Reference{{Type: "atlas.asset", ID: plan.AssetID}}
}

func horizonDate(value time.Time) string { return value.UTC().Format("2006-01-02") }

func horizonOptionalDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return horizonDate(*value)
}

func parseHorizonDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, ErrInvalidInput
	}
	return parsed, nil
}

func parseHorizonOptionalDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseHorizonDate(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func sameHorizonPlan(left, right horizon.Plan) bool {
	return left.ID == right.ID && left.AssetID == right.AssetID && left.Scenario == right.Scenario &&
		left.ExpectedUsefulLifeMonths == right.ExpectedUsefulLifeMonths && sameHorizonOptionalDate(left.ReplacementDate, right.ReplacementDate) &&
		left.LifecycleStage == right.LifecycleStage && left.ReplacementCostMinor == right.ReplacementCostMinor &&
		left.Currency == right.Currency && left.EffectiveFrom.Equal(right.EffectiveFrom) && left.Revision == right.Revision
}

func sameHorizonOptionalDate(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}
