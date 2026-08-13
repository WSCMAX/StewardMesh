// Package atlas implements the organization-scoped asset registry.
// Requirements: REQ-ATLAS-001, REQ-DIRECTORY-EXPANSION-008. Features: inventory.assets, threads.relationships.
package atlas

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	Search            string
	Kind              string
	Status            string
	ModelID           string
	SiteID            string
	DepartmentID      string
	UserID            string
	DeploymentContext string
	Limit             int
}

// AuthorizedAssetQuery is the bounded keyset query used by non-browser
// integration transports. Visibility is an authenticated server-derived
// predicate and is applied by the repository before ordering and limiting, so
// records outside the caller's Guard grants cannot crowd visible assets out of
// a page. Cursor is the last asset ID returned by the preceding ID-ordered
// page; callers never supply authorization selectors.
// Requirements: REQ-API-001, SEC-MCP-001. Feature: integrations.protocols.
type AuthorizedAssetQuery struct {
	Search     string
	Cursor     string
	Limit      int
	Visibility GraphAssetVisibility
}

// GraphAssetVisibility and GraphAssetReferences are separate predicates: a
// graph asset must satisfy one authenticated visibility selector and, when
// supplied, one relationship-context selector. Keeping the predicates separate
// prevents a contextual site or user ID from widening an assets.read grant.
// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.
type GraphAssetVisibility struct {
	All           bool
	ResourceIDs   []string
	SiteIDs       []string
	DepartmentIDs []string
}

func (v GraphAssetVisibility) Empty() bool {
	return !v.All && len(v.ResourceIDs) == 0 && len(v.SiteIDs) == 0 && len(v.DepartmentIDs) == 0
}

func (v GraphAssetVisibility) Valid() bool {
	return !v.Empty() && len(v.ResourceIDs)+len(v.SiteIDs)+len(v.DepartmentIDs) <= MaximumGraphAssetLimit &&
		validGraphAssetIDs(v.ResourceIDs) && validGraphAssetIDs(v.SiteIDs) && validGraphAssetIDs(v.DepartmentIDs)
}

type GraphAssetReferences struct {
	ResourceIDs   []string
	SiteIDs       []string
	BuildingIDs   []string
	RoomIDs       []string
	DepartmentIDs []string
	UserIDs       []string
}

// GraphAssetDirectoryVisibility is a second, independent authorization
// predicate applied before the graph source limit. MatchUserDirectory permits
// an adapter to match an asset's user identity against the same site and
// department selectors; it never widens the authenticated Atlas visibility.
type GraphAssetDirectoryVisibility struct {
	All                bool
	SiteIDs            []string
	DepartmentIDs      []string
	UserIDs            []string
	MatchUserDirectory bool
}

func (v GraphAssetDirectoryVisibility) Empty() bool {
	return !v.All && len(v.SiteIDs)+len(v.DepartmentIDs)+len(v.UserIDs) == 0
}

func (v GraphAssetDirectoryVisibility) Valid() bool {
	return !v.Empty() && len(v.SiteIDs)+len(v.DepartmentIDs)+len(v.UserIDs) <= MaximumGraphAssetLimit &&
		(!v.MatchUserDirectory || len(v.SiteIDs)+len(v.DepartmentIDs) > 0) &&
		validGraphAssetIDs(v.SiteIDs) && validGraphAssetIDs(v.DepartmentIDs) && validGraphAssetIDs(v.UserIDs)
}

func (r GraphAssetReferences) Empty() bool {
	return len(r.ResourceIDs)+len(r.SiteIDs)+len(r.BuildingIDs)+len(r.RoomIDs)+len(r.DepartmentIDs)+len(r.UserIDs) == 0
}

func (r GraphAssetReferences) Valid() bool {
	return len(r.ResourceIDs)+len(r.SiteIDs)+len(r.BuildingIDs)+len(r.RoomIDs)+len(r.DepartmentIDs)+len(r.UserIDs) <= MaximumGraphAssetLimit &&
		validGraphAssetIDs(r.ResourceIDs) && validGraphAssetIDs(r.SiteIDs) && validGraphAssetIDs(r.BuildingIDs) &&
		validGraphAssetIDs(r.RoomIDs) && validGraphAssetIDs(r.DepartmentIDs) && validGraphAssetIDs(r.UserIDs)
}

type GraphAssetQuery struct {
	LabelSearch                string
	Visibility                 GraphAssetVisibility
	Directory                  GraphAssetDirectoryVisibility
	References                 GraphAssetReferences
	DirectOrganizationChildren bool
	Limit                      int
}

const MaximumGraphAssetLimit = 500

func (q GraphAssetQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= MaximumGraphAssetLimit && q.Visibility.Valid() && q.Directory.Valid() && q.References.Valid() &&
		validGraphAssetText(q.LabelSearch, 200)
}

func validGraphAssetText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validGraphAssetIDs(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
			return false
		}
		for _, character := range value {
			if unicode.IsControl(character) {
				return false
			}
		}
	}
	return true
}

type ModelQuery struct {
	Search string
	Kind   string
	Status string
	Limit  int
}

type ModelIdentity struct {
	Manufacturer string `json:"manufacturer"`
	Name         string `json:"name"`
	ModelNumber  string `json:"modelNumber,omitempty"`
}

const (
	ModelInventoryGroupStatus     = "status"
	ModelInventoryGroupSite       = "site"
	ModelInventoryGroupDepartment = "department"
	ModelInventoryGroupUser       = "user"
	ModelInventoryGroupDeployment = "deployment"
)

// ModelInventoryQuery applies the same instance filters as Atlas asset
// listing while allowing model detail to request one explicit grouping.
type ModelInventoryQuery struct {
	Status            string
	SiteID            string
	DepartmentID      string
	UserID            string
	DeploymentContext string
	GroupBy           string
	Limit             int
}

type ModelInventoryGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type ModelInventory struct {
	ModelID       string                `json:"modelId"`
	TotalCount    int                   `json:"totalCount"`
	FilteredCount int                   `json:"filteredCount"`
	GroupBy       string                `json:"groupBy,omitempty"`
	Groups        []ModelInventoryGroup `json:"groups"`
	Items         []domain.Asset        `json:"items"`
}

type References struct {
	SiteID       string `json:"siteId,omitempty"`
	BuildingID   string `json:"buildingId,omitempty"`
	RoomID       string `json:"roomId,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
	UserID       string `json:"userId,omitempty"`
}

type CreateAssetInput struct {
	ID              string `json:"id,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	AssetTag        string `json:"assetTag,omitempty"`
	SerialNumber    string `json:"serialNumber,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	DeploymentNotes string `json:"deploymentNotes,omitempty"`
	References
	Status       string     `json:"status"`
	PurchaseDate *time.Time `json:"purchaseDate,omitempty"`
}

type BulkCreateAssetsInput struct {
	ModelID string             `json:"modelId"`
	Items   []CreateAssetInput `json:"items"`
}

type BulkCreateAssetsResult struct {
	Items []domain.Asset `json:"items"`
}

type UpdateAssetInput struct {
	ID              string `json:"-"`
	ModelID         string `json:"modelId,omitempty"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	AssetTag        string `json:"assetTag,omitempty"`
	SerialNumber    string `json:"serialNumber,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	DeploymentNotes string `json:"deploymentNotes,omitempty"`
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
	ResolveModel(ctx context.Context, organizationID string, identity ModelIdentity) (domain.AssetModel, error)
	CreateModel(ctx context.Context, model domain.AssetModel) (domain.AssetModel, error)
	UpdateModel(ctx context.Context, model domain.AssetModel, expectedRevision int64) (domain.AssetModel, error)
	RetireModel(ctx context.Context, organizationID, id string, expectedRevision int64, retiredAt time.Time) (domain.AssetModel, error)
	GetModelInventory(ctx context.Context, organizationID, modelID string, query ModelInventoryQuery) (ModelInventory, error)
	ListAssets(ctx context.Context, organizationID string, query Query) ([]domain.Asset, error)
	ListAuthorizedAssets(ctx context.Context, organizationID string, query AuthorizedAssetQuery) ([]domain.Asset, error)
	ListGraphAssets(ctx context.Context, organizationID string, query GraphAssetQuery) ([]domain.Asset, error)
	GetAsset(ctx context.Context, organizationID, id string) (domain.Asset, error)
	CreateAsset(ctx context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error)
	CreateAssets(ctx context.Context, assets []domain.Asset, initialEvents []domain.AssetLifecycleEvent) ([]domain.Asset, error)
	UpdateAsset(ctx context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error)
	ListAssetLifecycle(ctx context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error)
}

type ReferenceValidator interface {
	ValidateAssetReferences(ctx context.Context, organizationID string, references References) error
}
