// Package catalog implements reusable Atlas product, configuration, pricing,
// and upgrade-path records.
// Requirement: REQ-ATLAS-CATALOG-001. Feature: inventory.products.
package catalog

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID          = "REQ-ATLAS-CATALOG-001"
	FeatureID              = "inventory.products"
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

type Product struct {
	ID                      string            `json:"id"`
	OrganizationID          string            `json:"organizationId"`
	Manufacturer            string            `json:"manufacturer"`
	Model                   string            `json:"model"`
	AssetKind               string            `json:"assetKind"`
	Status                  Status            `json:"status"`
	Specifications          map[string]string `json:"specifications"`
	DefaultUsefulLifeMonths int               `json:"defaultUsefulLifeMonths,omitempty"`
	Revision                int64             `json:"revision"`
	CreatedAt               time.Time         `json:"createdAt"`
	UpdatedAt               time.Time         `json:"updatedAt"`
}

type Configuration struct {
	ID             string            `json:"id"`
	OrganizationID string            `json:"organizationId"`
	ProductID      string            `json:"productId"`
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
	ProductID       string     `json:"productId"`
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
	FromProductID       string      `json:"fromProductId"`
	FromConfigurationID string      `json:"fromConfigurationId,omitempty"`
	ToProductID         string      `json:"toProductId"`
	ToConfigurationID   string      `json:"toConfigurationId,omitempty"`
	Kind                UpgradeKind `json:"kind"`
	EffectiveFrom       time.Time   `json:"effectiveFrom"`
	Revision            int64       `json:"revision"`
	CreatedAt           time.Time   `json:"createdAt"`
}

type ProductQuery struct {
	Search    string
	AssetKind string
	Status    Status
	Limit     int
}

type CreateProductInput struct {
	ID                      string            `json:"id,omitempty"`
	Manufacturer            string            `json:"manufacturer"`
	Model                   string            `json:"model"`
	AssetKind               string            `json:"assetKind"`
	Status                  Status            `json:"status,omitempty"`
	Specifications          map[string]string `json:"specifications,omitempty"`
	DefaultUsefulLifeMonths int               `json:"defaultUsefulLifeMonths,omitempty"`
}

type CreateConfigurationInput struct {
	ID             string            `json:"id,omitempty"`
	ProductID      string            `json:"productId"`
	Name           string            `json:"name"`
	SKU            string            `json:"sku,omitempty"`
	Status         Status            `json:"status,omitempty"`
	Specifications map[string]string `json:"specifications,omitempty"`
}

type RecordPriceInput struct {
	ID              string     `json:"id,omitempty"`
	ProductID       string     `json:"productId"`
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
	FromProductID       string      `json:"fromProductId"`
	FromConfigurationID string      `json:"fromConfigurationId,omitempty"`
	ToProductID         string      `json:"toProductId"`
	ToConfigurationID   string      `json:"toConfigurationId,omitempty"`
	Kind                UpgradeKind `json:"kind"`
	EffectiveFrom       time.Time   `json:"effectiveFrom"`
}

type ResolvePriceInput struct {
	ProductID       string
	ConfigurationID string
	AsOf            time.Time
	Currency        string
	Kind            PriceKind
}

// Store is the provider-neutral catalog persistence boundary. Price records
// and upgrade paths are append-only in the foundation slice.
type Store interface {
	ListProducts(ctx context.Context, organizationID string, query ProductQuery) ([]Product, error)
	GetProduct(ctx context.Context, organizationID, productID string) (Product, error)
	CreateProduct(ctx context.Context, product Product) (Product, error)

	ListConfigurations(ctx context.Context, organizationID, productID string) ([]Configuration, error)
	GetConfiguration(ctx context.Context, organizationID, configurationID string) (Configuration, error)
	CreateConfiguration(ctx context.Context, configuration Configuration) (Configuration, error)

	ListPrices(ctx context.Context, organizationID, productID, configurationID string) ([]Price, error)
	CreatePrice(ctx context.Context, price Price) (Price, error)

	ListUpgradePaths(ctx context.Context, organizationID, fromProductID, fromConfigurationID string) ([]UpgradePath, error)
	CreateUpgradePath(ctx context.Context, path UpgradePath) (UpgradePath, error)
}
