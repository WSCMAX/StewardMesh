// Package atlascodes implements organization-scoped asset identifier
// associations. Requirement: REQ-ATLAS-CODES-001. Feature: inventory.identifiers.
package atlascodes

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

const (
	RequirementID = "REQ-ATLAS-CODES-001"
	FeatureID     = "inventory.identifiers"
)

var (
	ErrInvalidInput     = errors.New("invalid Atlas Codes input")
	ErrNotFound         = errors.New("Atlas Codes identifier not found")
	ErrConflict         = errors.New("Atlas Codes identifier conflicts with existing data")
	ErrReferenceMissing = errors.New("Atlas Codes asset reference does not exist")
)

type Symbology string

const (
	SymbologyCode128 Symbology = "code128"
	SymbologyQR      Symbology = "qr"
)

type Source string

const (
	SourceImported    Source = "imported"
	SourceUserEntered Source = "user_entered"
	SourceGenerated   Source = "generated"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusReplaced    Status = "replaced"
	StatusDeactivated Status = "deactivated"
)

// Identifier is one current or historical association between a physical or
// digital code and an Atlas asset. NormalizedValue remains case-sensitive and
// must never be copied into audit metadata.
type Identifier struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	AssetID         string    `json:"assetId"`
	Symbology       Symbology `json:"symbology"`
	NormalizedValue string    `json:"normalizedValue"`
	DisplayValue    string    `json:"displayValue"`
	Source          Source    `json:"source"`
	Primary         bool      `json:"primary"`
	Status          Status    `json:"status"`
	SupersedesID    string    `json:"supersedesId,omitempty"`
	ReplacedByID    string    `json:"replacedById,omitempty"`
	Revision        int64     `json:"revision"`
	CreatedBy       string    `json:"createdBy"`
	// Audit provenance is durable state, but correlation IDs and the latest
	// mutation actor are internal implementation details rather than API data.
	CreatedCorrelationID string     `json:"-"`
	UpdatedBy            string     `json:"-"`
	UpdatedCorrelationID string     `json:"-"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	DeactivatedAt        *time.Time `json:"deactivatedAt,omitempty"`
}

type CreateIdentifierInput struct {
	ID           string    `json:"id,omitempty"`
	AssetID      string    `json:"-"`
	Symbology    Symbology `json:"symbology"`
	Value        string    `json:"value"`
	DisplayValue string    `json:"displayValue,omitempty"`
	Source       Source    `json:"source,omitempty"`
	Primary      bool      `json:"primary,omitempty"`
}

type ReplaceIdentifierInput struct {
	AssetID              string    `json:"-"`
	IdentifierID         string    `json:"-"`
	Revision             int64     `json:"revision"`
	ReplacementID        string    `json:"replacementId,omitempty"`
	ReplacementSymbology Symbology `json:"symbology"`
	ReplacementValue     string    `json:"value"`
	DisplayValue         string    `json:"displayValue,omitempty"`
	Source               Source    `json:"source,omitempty"`
}

type DeactivateIdentifierInput struct {
	AssetID      string `json:"-"`
	IdentifierID string `json:"-"`
	Revision     int64  `json:"revision"`
}

type AssetReader interface {
	GetAsset(ctx context.Context, id string) (domain.Asset, error)
}

// Store is the provider-neutral identifier association boundary. Replace and
// deactivate must apply their history transition and active-state mutation
// atomically. The booleans distinguish an applied mutation from a safe retry.
type Store interface {
	ListIdentifiers(ctx context.Context, organizationID, assetID string) ([]Identifier, error)
	GetIdentifier(ctx context.Context, organizationID, assetID, identifierID string) (Identifier, error)
	ResolveIdentifier(ctx context.Context, organizationID string, symbology Symbology, normalizedValue string) (Identifier, error)
	CreateIdentifier(ctx context.Context, identifier Identifier) (Identifier, bool, error)
	ReplaceIdentifier(
		ctx context.Context,
		organizationID, assetID, identifierID string,
		expectedRevision int64,
		replacement Identifier,
		changedAt time.Time,
	) (Identifier, bool, error)
	DeactivateIdentifier(
		ctx context.Context,
		organizationID, assetID, identifierID string,
		expectedRevision int64,
		deactivatedAt time.Time,
		actorID, correlationID string,
	) (Identifier, bool, error)
}
