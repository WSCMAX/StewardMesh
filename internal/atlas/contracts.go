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
	RequirementID      = "REQ-ATLAS-001"
	ModelRequirementID = "REQ-ATLAS-MODELS-001"
	FeatureID          = "inventory.assets"
	ModelFeatureID     = "inventory.models"
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
	ModelID      string
	SiteID       string
	DepartmentID string
	UserID       string
	Limit        int
}

type ModelQuery struct {
	Search string
	Kind   string
	Status string
	Limit  int
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
	ModelID      string `json:"modelId,omitempty"`
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
	ModelID      string `json:"modelId,omitempty"`
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

type CreateModelInput struct {
	ID               string            `json:"id,omitempty"`
	Manufacturer     string            `json:"manufacturer"`
	Name             string            `json:"name"`
	ModelNumber      string            `json:"modelNumber,omitempty"`
	Kind             string            `json:"kind"`
	VendorIdentifier string            `json:"vendorIdentifier,omitempty"`
	Specifications   map[string]string `json:"specifications,omitempty"`
	SupportURL       string            `json:"supportUrl,omitempty"`
	WarrantyMonths   int               `json:"warrantyMonths,omitempty"`
	UsefulLifeMonths int               `json:"usefulLifeMonths,omitempty"`
	SourceSystemID   string            `json:"sourceSystemId,omitempty"`
	SourceRecordID   string            `json:"sourceRecordId,omitempty"`
}

type UpdateModelInput struct {
	ID               string            `json:"-"`
	Manufacturer     string            `json:"manufacturer"`
	Name             string            `json:"name"`
	ModelNumber      string            `json:"modelNumber,omitempty"`
	Kind             string            `json:"kind"`
	VendorIdentifier string            `json:"vendorIdentifier,omitempty"`
	Specifications   map[string]string `json:"specifications,omitempty"`
	SupportURL       string            `json:"supportUrl,omitempty"`
	WarrantyMonths   int               `json:"warrantyMonths,omitempty"`
	UsefulLifeMonths int               `json:"usefulLifeMonths,omitempty"`
	SourceSystemID   string            `json:"sourceSystemId,omitempty"`
	SourceRecordID   string            `json:"sourceRecordId,omitempty"`
	Revision         int64             `json:"revision"`
}

type Store interface {
	ListModels(ctx context.Context, organizationID string, query ModelQuery) ([]domain.AssetModel, error)
	GetModel(ctx context.Context, organizationID, id string) (domain.AssetModel, error)
	CreateModel(ctx context.Context, model domain.AssetModel) (domain.AssetModel, error)
	UpdateModel(ctx context.Context, model domain.AssetModel, expectedRevision int64) (domain.AssetModel, error)
	RetireModel(ctx context.Context, organizationID, id string, expectedRevision int64, retiredAt time.Time) (domain.AssetModel, error)
	ListAssets(ctx context.Context, organizationID string, query Query) ([]domain.Asset, error)
	GetAsset(ctx context.Context, organizationID, id string) (domain.Asset, error)
	CreateAsset(ctx context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error)
	UpdateAsset(ctx context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error)
	ListAssetLifecycle(ctx context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error)
}

type ReferenceValidator interface {
	ValidateAssetReferences(ctx context.Context, organizationID string, references References) error
}
