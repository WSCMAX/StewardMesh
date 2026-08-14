// Package catalog extends reusable Atlas Models with configurations, pricing,
// and upgrade-path records.
// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.catalog.
package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

const (
	RequirementID          = "REQ-ATLAS-CATALOG-001"
	FeatureID              = "inventory.catalog"
	MaximumExactMinorUnits = int64(9_007_199_254_740_991)
)

var (
	ErrInvalidInput  = errors.New("invalid Atlas Catalog input")
	ErrNotFound      = errors.New("Atlas Catalog record not found")
	ErrConflict      = errors.New("Atlas Catalog record conflicts with existing data")
	ErrMixedCurrency = errors.New("Atlas Catalog cannot select across currencies")
)

type Status string

const (
	StatusActive  Status = "active"
	StatusRetired Status = "retired"
)

type PriceKind string

const (
	PriceKindList     PriceKind = "list"
	PriceKindQuote    PriceKind = "quote"
	PriceKindContract PriceKind = "contract"
	PriceKindEstimate PriceKind = "estimate"
)

type UpgradeKind string

const (
	UpgradeKindSuccessor   UpgradeKind = "successor"
	UpgradeKindReplacement UpgradeKind = "replacement"
	UpgradeKindUpgrade     UpgradeKind = "upgrade"
)

type Configuration struct {
	ID             string            `json:"id"`
	OrganizationID string            `json:"organizationId"`
	ModelID        string            `json:"modelId"`
	Name           string            `json:"name"`
	SKU            string            `json:"sku,omitempty"`
	Status         Status            `json:"status"`
	Specifications map[string]string `json:"specifications"`
	Revision       int64             `json:"revision"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// Price is an immutable effective-dated catalog observation. AmountMinor is
// exact integer minor units; monetary values must not be copied into audits.
type Price struct {
	ID              string     `json:"id"`
	OrganizationID  string     `json:"organizationId"`
	ModelID         string     `json:"modelId"`
	ConfigurationID string     `json:"configurationId,omitempty"`
	Kind            PriceKind  `json:"kind"`
	AmountMinor     int64      `json:"amountMinor"`
	Currency        string     `json:"currency"`
	EffectiveFrom   time.Time  `json:"effectiveFrom"`
	EffectiveTo     *time.Time `json:"effectiveTo,omitempty"`
	SourceReference string     `json:"sourceReference,omitempty"`
	Revision        int64      `json:"revision"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type UpgradePath struct {
	ID                  string      `json:"id"`
	OrganizationID      string      `json:"organizationId"`
	FromModelID         string      `json:"fromModelId"`
	FromConfigurationID string      `json:"fromConfigurationId,omitempty"`
	ToModelID           string      `json:"toModelId"`
	ToConfigurationID   string      `json:"toConfigurationId,omitempty"`
	Kind                UpgradeKind `json:"kind"`
	EffectiveFrom       time.Time   `json:"effectiveFrom"`
	Revision            int64       `json:"revision"`
	CreatedAt           time.Time   `json:"createdAt"`
}

// Snapshot is the bounded, organization-scoped durable Catalog state used by
// Exchange and repository conformance tests. Callers must still enforce the
// Exchange package-wide record limit.
type Snapshot struct {
	Configurations []Configuration `json:"configurations"`
	Prices         []Price         `json:"prices"`
	UpgradePaths   []UpgradePath   `json:"upgradePaths"`
}

type CreateConfigurationInput struct {
	ID             string            `json:"id,omitempty"`
	ModelID        string            `json:"modelId"`
	Name           string            `json:"name"`
	SKU            string            `json:"sku,omitempty"`
	Status         Status            `json:"status,omitempty"`
	Specifications map[string]string `json:"specifications,omitempty"`
}

type RecordPriceInput struct {
	ID              string     `json:"id,omitempty"`
	ModelID         string     `json:"modelId"`
	ConfigurationID string     `json:"configurationId,omitempty"`
	Kind            PriceKind  `json:"kind"`
	AmountMinor     int64      `json:"amountMinor"`
	Currency        string     `json:"currency"`
	EffectiveFrom   time.Time  `json:"effectiveFrom"`
	EffectiveTo     *time.Time `json:"effectiveTo,omitempty"`
	SourceReference string     `json:"sourceReference,omitempty"`
}

type CreateUpgradePathInput struct {
	ID                  string      `json:"id,omitempty"`
	FromModelID         string      `json:"fromModelId"`
	FromConfigurationID string      `json:"fromConfigurationId,omitempty"`
	ToModelID           string      `json:"toModelId"`
	ToConfigurationID   string      `json:"toConfigurationId,omitempty"`
	Kind                UpgradeKind `json:"kind"`
	EffectiveFrom       time.Time   `json:"effectiveFrom"`
}

type ResolvePriceInput struct {
	ModelID         string
	ConfigurationID string
	AsOf            time.Time
	Currency        string
	Kind            PriceKind
}

// ExchangeImportOperation is the deterministic, bounded mutation identity
// issued by Exchange after it has reserved durable intent and ownership.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. Ordinary
// Catalog callers cannot select source revisions or deterministic audit IDs.
type ExchangeImporter interface {
	ImportConfiguration(context.Context, ExchangeImportOperation, Configuration) (ExchangeImportResult, error)
	ImportPrice(context.Context, ExchangeImportOperation, Price) (ExchangeImportResult, error)
	ImportUpgradePath(context.Context, ExchangeImportOperation, UpgradePath) (ExchangeImportResult, error)
}

// ModelReader keeps Atlas Models authoritative while Catalog validates stable
// model references without reading provider tables directly.
type ModelReader interface {
	GetModel(ctx context.Context, id string) (domain.AssetModel, error)
}

// WriteGate is the service-layer imported-ownership fence. Transports and
// background jobs pass the authenticated operation context; Exchange alone
// receives a construction-time importer capability that bypasses this gate.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

// Store is the provider-neutral catalog extension persistence boundary. Price
// records and upgrade paths are append-only in the foundation slice.
type Store interface {
	Snapshot(ctx context.Context, organizationID string) (Snapshot, error)

	ListConfigurations(ctx context.Context, organizationID, modelID string) ([]Configuration, error)
	GetConfiguration(ctx context.Context, organizationID, configurationID string) (Configuration, error)
	CreateConfiguration(ctx context.Context, configuration Configuration) (Configuration, error)

	ListPrices(ctx context.Context, organizationID, modelID, configurationID string) ([]Price, error)
	GetPrice(ctx context.Context, organizationID, priceID string) (Price, error)
	CreatePrice(ctx context.Context, price Price) (Price, error)

	ListUpgradePaths(ctx context.Context, organizationID, fromModelID, fromConfigurationID string) ([]UpgradePath, error)
	GetUpgradePath(ctx context.Context, organizationID, pathID string) (UpgradePath, error)
	CreateUpgradePath(ctx context.Context, path UpgradePath) (UpgradePath, error)
}
