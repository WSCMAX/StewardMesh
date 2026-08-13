// Package people implements the StewardMesh directory and asset assignment domain.
// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-002. Features: identity.directory, integrations.protocols.
package people

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

const (
	RequirementID                   = "REQ-PEOPLE-001"
	DirectoryExpansionRequirementID = "REQ-DIRECTORY-EXPANSION-001"
	FeatureID                       = "identity.directory"
)

var (
	ErrNotFound         = errors.New("people record not found")
	ErrReferenceMissing = errors.New("people reference not found")
	ErrConflict         = errors.New("people record conflicts with existing data")
	ErrInvalidInput     = errors.New("invalid people input")
	ErrScopeRequired    = errors.New("directory visibility scope is required")
)

type RecordStatus string

const (
	StatusActive   RecordStatus = "active"
	StatusInactive RecordStatus = "inactive"
)

type IdentityKind string

const (
	IdentityPerson IdentityKind = "person"
	IdentityShared IdentityKind = "shared"
	IdentityPublic IdentityKind = "public"
	IdentityLab    IdentityKind = "lab"
)

type AssigneeKind string

const (
	AssigneeIdentity   AssigneeKind = "identity"
	AssigneeDepartment AssigneeKind = "department"
)

type AssignmentRole string

const (
	AssignmentPrimary    AssignmentRole = "primary"
	AssignmentUser       AssignmentRole = "user"
	AssignmentDepartment AssignmentRole = "department"
)

type Site struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organizationId"`
	Name           string       `json:"name"`
	NormalizedName string       `json:"-"`
	Address        Address      `json:"address"`
	Status         RecordStatus `json:"status"`
	Revision       uint64       `json:"revision"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// Address is an optional structured postal address for a site. A zero-value
// address preserves the original site contract for callers that only provide
// a name and status.
type Address struct {
	Line1      string `json:"line1,omitempty"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
}

func (a Address) Empty() bool {
	return a.Line1 == "" && a.Line2 == "" && a.City == "" && a.Region == "" && a.PostalCode == "" && a.Country == ""
}

type Building struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organizationId"`
	SiteID         string       `json:"siteId"`
	Name           string       `json:"name"`
	NormalizedName string       `json:"-"`
	Status         RecordStatus `json:"status"`
	Revision       uint64       `json:"revision"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type Room struct {
	ID               string       `json:"id"`
	OrganizationID   string       `json:"organizationId"`
	SiteID           string       `json:"siteId"`
	BuildingID       string       `json:"buildingId"`
	Number           string       `json:"number"`
	NormalizedNumber string       `json:"-"`
	Name             string       `json:"name,omitempty"`
	Status           RecordStatus `json:"status"`
	Revision         uint64       `json:"revision"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type Department struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organizationId"`
	Name           string       `json:"name"`
	NormalizedName string       `json:"-"`
	SiteID         string       `json:"siteId,omitempty"`
	Status         RecordStatus `json:"status"`
	Revision       uint64       `json:"revision"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// Identity represents a person or a deliberately non-person directory entry.
// Provider and ProviderSubject reserve a safe mapping seam for future OIDC,
// OAuth, and SAML provisioning without coupling People to an auth provider.
type Identity struct {
	ID              string       `json:"id"`
	OrganizationID  string       `json:"organizationId"`
	Kind            IdentityKind `json:"kind"`
	DisplayName     string       `json:"displayName"`
	NormalizedName  string       `json:"-"`
	Email           string       `json:"email,omitempty"`
	NormalizedEmail string       `json:"-"`
	DepartmentID    string       `json:"departmentId,omitempty"`
	SiteID          string       `json:"siteId,omitempty"`
	Status          RecordStatus `json:"status"`
	Provider        string       `json:"provider,omitempty"`
	ProviderSubject string       `json:"providerSubject,omitempty"`
	Revision        uint64       `json:"revision"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type AssetAssignment struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId"`
	AssetID        string         `json:"assetId"`
	AssigneeKind   AssigneeKind   `json:"assigneeKind"`
	AssigneeID     string         `json:"assigneeId"`
	Role           AssignmentRole `json:"role"`
	EffectiveFrom  time.Time      `json:"effectiveFrom"`
	EffectiveTo    *time.Time     `json:"effectiveTo,omitempty"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// Visibility is mandatory for every read. All means organization-wide access;
// otherwise records are restricted to the listed department or site scopes.
type Visibility struct {
	All           bool
	DepartmentIDs []string
	SiteIDs       []string
}

func (v Visibility) Empty() bool {
	return !v.All && len(v.DepartmentIDs) == 0 && len(v.SiteIDs) == 0
}

type IdentityQuery struct {
	Search       string
	Kind         IdentityKind
	Status       RecordStatus
	DepartmentID string
	SiteID       string
	Limit        int
}

type CreateSiteInput struct {
	Name    string
	Address Address
	Status  RecordStatus
}

type CreateBuildingInput struct {
	SiteID string
	Name   string
	Status RecordStatus
}

type CreateRoomInput struct {
	SiteID     string
	BuildingID string
	Number     string
	Name       string
	Status     RecordStatus
}

type CreateDepartmentInput struct {
	Name   string
	SiteID string
	Status RecordStatus
}

type CreateIdentityInput struct {
	Kind            IdentityKind
	DisplayName     string
	Email           string
	DepartmentID    string
	SiteID          string
	Status          RecordStatus
	Provider        string
	ProviderSubject string
}

type CreateAssetAssignmentInput struct {
	AssetID       string
	AssigneeKind  AssigneeKind
	AssigneeID    string
	Role          AssignmentRole
	EffectiveFrom time.Time
}

type EndAssetAssignmentInput struct {
	AssetID      string
	AssignmentID string
	EffectiveTo  time.Time
}

// AssetReader is satisfied by the Atlas repository contract. Keeping this
// interface local lets Atlas become durable without changing People.
type AssetReader interface {
	Get(ctx context.Context, id string) (domain.Asset, error)
}

// Store is provider-neutral. PostgreSQL is the first durable adapter and a
// future DynamoDB adapter must pass the same conformance tests.
type Store interface {
	CreateSite(ctx context.Context, site Site) (Site, error)
	GetSite(ctx context.Context, organizationID, id string) (Site, error)
	ListSites(ctx context.Context, organizationID string, visibility Visibility) ([]Site, error)

	CreateBuilding(ctx context.Context, building Building) (Building, error)
	GetBuilding(ctx context.Context, organizationID, id string) (Building, error)
	ListBuildings(ctx context.Context, organizationID, siteID string, visibility Visibility) ([]Building, error)

	CreateRoom(ctx context.Context, room Room) (Room, error)
	GetRoom(ctx context.Context, organizationID, id string) (Room, error)
	ListRooms(ctx context.Context, organizationID, siteID, buildingID string, visibility Visibility) ([]Room, error)

	CreateDepartment(ctx context.Context, department Department) (Department, error)
	GetDepartment(ctx context.Context, organizationID, id string) (Department, error)
	ListDepartments(ctx context.Context, organizationID string, visibility Visibility) ([]Department, error)

	CreateIdentity(ctx context.Context, identity Identity) (Identity, error)
	GetIdentity(ctx context.Context, organizationID, id string) (Identity, error)
	GetIdentityByProvider(ctx context.Context, organizationID, provider, providerSubject string) (Identity, error)
	GetIdentityByEmail(ctx context.Context, organizationID, normalizedEmail string) (Identity, error)
	ReconcileIdentity(ctx context.Context, identity Identity, expectedRevision uint64) (Identity, error)
	DeleteIdentity(ctx context.Context, organizationID, id string, expectedRevision uint64) error
	SearchIdentities(ctx context.Context, organizationID string, query IdentityQuery, visibility Visibility) ([]Identity, error)

	CreateAssetAssignment(ctx context.Context, assignment AssetAssignment, replaceActiveRole bool) (AssetAssignment, error)
	EndAssetAssignment(ctx context.Context, organizationID, assetID, assignmentID string, effectiveTo time.Time) (AssetAssignment, error)
	ListAssetAssignments(ctx context.Context, organizationID, assetID string) ([]AssetAssignment, error)
}
