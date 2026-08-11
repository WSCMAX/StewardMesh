// Package atlas implements the organization-scoped asset registry.
// Requirement: REQ-ATLAS-001. Feature: inventory.assets.
package atlas

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

const (
	RequirementID = "REQ-ATLAS-001"
	FeatureID     = "inventory.assets"
)

var (
	ErrInvalidInput     = errors.New("invalid Atlas input")
	ErrNotFound         = errors.New("Atlas record not found")
	ErrConflict         = errors.New("Atlas record conflicts with existing data")
	ErrReferenceMissing = errors.New("Atlas reference does not exist")
)

type Query struct {
	Search       string
	Kind         string
	Status       string
	SiteID       string
	DepartmentID string
	UserID       string
	Limit        int
}

type References struct {
	SiteID       string `json:"siteId,omitempty"`
	BuildingID   string `json:"buildingId,omitempty"`
	RoomID       string `json:"roomId,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
	UserID       string `json:"userId,omitempty"`
}

type CreateAssetInput struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AssetTag     string `json:"assetTag,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	References
	Status       string     `json:"status"`
	PurchaseDate *time.Time `json:"purchaseDate,omitempty"`
}

type UpdateAssetInput struct {
	ID           string `json:"-"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AssetTag     string `json:"assetTag,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	References
	Status        string     `json:"status"`
	PurchaseDate  *time.Time `json:"purchaseDate,omitempty"`
	Revision      int64      `json:"revision"`
	LifecycleNote string     `json:"lifecycleNote,omitempty"`
}

type Store interface {
	ListAssets(ctx context.Context, organizationID string, query Query) ([]domain.Asset, error)
	GetAsset(ctx context.Context, organizationID, id string) (domain.Asset, error)
	CreateAsset(ctx context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error)
	UpdateAsset(ctx context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error)
	ListAssetLifecycle(ctx context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error)
}

type ReferenceValidator interface {
	ValidateAssetReferences(ctx context.Context, organizationID string, references References) error
}
