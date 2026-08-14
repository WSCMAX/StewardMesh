// Package horizon implements lifecycle planning and replacement forecasting.
// Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.
package horizon

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

const (
	RequirementID          = "REQ-HORIZON-001"
	FeatureID              = "lifecycle.planning"
	MaximumExactMinorUnits = int64(9_007_199_254_740_991)
)

var (
	ErrInvalidInput     = errors.New("invalid Horizon input")
	ErrNotFound         = errors.New("Horizon record not found")
	ErrConflict         = errors.New("Horizon record conflicts with existing data")
	ErrReferenceMissing = errors.New("Horizon reference does not exist")
	ErrMixedCurrency    = errors.New("Horizon cannot aggregate mixed currencies")
)

type Plan struct {
	ID                       string     `json:"id"`
	OrganizationID           string     `json:"organizationId"`
	AssetID                  string     `json:"assetId"`
	Scenario                 string     `json:"scenario"`
	ExpectedUsefulLifeMonths int        `json:"expectedUsefulLifeMonths"`
	ReplacementDate          *time.Time `json:"replacementDate,omitempty"`
	DerivedReplacementDate   *time.Time `json:"derivedReplacementDate,omitempty"`
	LifecycleStage           string     `json:"lifecycleStage"`
	ReplacementCostMinor     int64      `json:"replacementCostMinor"`
	Currency                 string     `json:"currency"`
	EffectiveFrom            time.Time  `json:"effectiveFrom"`
	Revision                 int64      `json:"revision"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

// PlanVersion is an immutable snapshot written with every plan mutation.
type PlanVersion struct {
	PlanID                   string     `json:"planId"`
	OrganizationID           string     `json:"organizationId"`
	AssetID                  string     `json:"assetId"`
	Scenario                 string     `json:"scenario"`
	ExpectedUsefulLifeMonths int        `json:"expectedUsefulLifeMonths"`
	ReplacementDate          *time.Time `json:"replacementDate,omitempty"`
	DerivedReplacementDate   *time.Time `json:"derivedReplacementDate,omitempty"`
	LifecycleStage           string     `json:"lifecycleStage"`
	ReplacementCostMinor     int64      `json:"replacementCostMinor"`
	Currency                 string     `json:"currency"`
	EffectiveFrom            time.Time  `json:"effectiveFrom"`
	Revision                 int64      `json:"revision"`
	ActorID                  string     `json:"actorId"`
	RecordedAt               time.Time  `json:"recordedAt"`
}

type ListPlansQuery struct {
	AssetID  string
	Scenario string
}

type CreatePlanInput struct {
	ID                       string     `json:"id,omitempty"`
	AssetID                  string     `json:"assetId"`
	Scenario                 string     `json:"scenario"`
	ExpectedUsefulLifeMonths int        `json:"expectedUsefulLifeMonths"`
	ReplacementDate          *time.Time `json:"replacementDate,omitempty"`
	LifecycleStage           string     `json:"lifecycleStage"`
	ReplacementCostMinor     int64      `json:"replacementCostMinor"`
	Currency                 string     `json:"currency"`
	EffectiveFrom            time.Time  `json:"effectiveFrom"`
}

type UpdatePlanInput struct {
	ID                       string     `json:"-"`
	AssetID                  string     `json:"assetId"`
	Scenario                 string     `json:"scenario"`
	ExpectedUsefulLifeMonths int        `json:"expectedUsefulLifeMonths"`
	ReplacementDate          *time.Time `json:"replacementDate,omitempty"`
	LifecycleStage           string     `json:"lifecycleStage"`
	ReplacementCostMinor     int64      `json:"replacementCostMinor"`
	Currency                 string     `json:"currency"`
	EffectiveFrom            time.Time  `json:"effectiveFrom"`
	Revision                 int64      `json:"revision"`
}

// ExchangeImportOperation is the deterministic mutation identity issued by
// Exchange after it has reserved durable intent and external ownership.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. Ordinary
// Horizon callers cannot select an imported revision or deterministic audit
// identity, and implementations outside this package cannot forge the seam.
type ExchangeImporter interface {
	ImportPlan(context.Context, ExchangeImportOperation, Plan) (ExchangeImportResult, error)
	horizonExchangeImporter()
}

type ForecastQuery struct {
	Scenarios            []string
	AsOf                 time.Time
	FromYear             int
	ToYear               int
	FiscalYearStartMonth int
	GroupBy              string
}

type ForecastGroup struct {
	Key                     string           `json:"key"`
	Label                   string           `json:"label"`
	Scenario                string           `json:"scenario"`
	PlannedReplacementMinor int64            `json:"plannedReplacementMinor"`
	AssetCount              int              `json:"assetCount"`
	AmountsByKindMinor      map[string]int64 `json:"amountsByKindMinor"`
}

type Forecast struct {
	AsOf                    time.Time        `json:"asOf"`
	GroupBy                 string           `json:"groupBy"`
	Currency                string           `json:"currency"`
	Scenarios               []string         `json:"scenarios"`
	PlannedReplacementMinor int64            `json:"plannedReplacementMinor"`
	AssetCount              int              `json:"assetCount"`
	TotalsByKindMinor       map[string]int64 `json:"totalsByKindMinor"`
	Groups                  []ForecastGroup  `json:"groups"`
}

type Store interface {
	ListPlans(ctx context.Context, organizationID string, query ListPlansQuery) ([]Plan, error)
	GetPlan(ctx context.Context, organizationID, id string) (Plan, error)
	CreatePlan(ctx context.Context, plan Plan, version PlanVersion) (Plan, error)
	UpdatePlan(ctx context.Context, plan Plan, expectedRevision int64, version PlanVersion) (Plan, error)
	ListPlanVersions(ctx context.Context, organizationID, planID string) ([]PlanVersion, error)
}

type AssetReader interface {
	ListAssets(ctx context.Context, query atlas.Query) ([]domain.Asset, error)
	GetAsset(ctx context.Context, id string) (domain.Asset, error)
}

type FinanceReader interface {
	Snapshot(ctx context.Context) (ledger.Snapshot, error)
}

type RelationshipReader interface {
	ListGoals(ctx context.Context) ([]threads.Goal, error)
	EvaluateTags(ctx context.Context, targetType threads.TargetType, targetID string) ([]threads.EffectiveTag, error)
	ListGoalLinks(ctx context.Context, targetType threads.TargetType, targetID string) ([]threads.GoalLink, error)
}

// WriteGate is the service-layer imported-ownership fence. Transports and
// background jobs pass an authenticated operation context; Exchange alone
// receives the opaque importer capability that bypasses this gate.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}
