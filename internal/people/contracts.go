// Package people implements the StewardMesh directory and asset assignment domain.
// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-008. Features: identity.directory, integrations.protocols, threads.relationships.
package people

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
	ErrTooLarge         = errors.New("people snapshot exceeds a configured limit")
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

// GraphIdentityQuery is the private, label-only People projection used by the
// relationship graph. Reference selectors are ORed together and applied before
// the result limit; when no selector is present the query scans the authorized
// active identity set. It deliberately excludes email and provider metadata
// from search so hidden fields cannot crowd a matching graph label out of a
// bounded result.
// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.
type GraphIdentityQuery struct {
	LabelSearch                string
	Kind                       IdentityKind
	IdentityIDs                []string
	DepartmentIDs              []string
	SiteIDs                    []string
	DirectOrganizationChildren bool
	Limit                      int
}

const MaximumGraphIdentityLimit = 500

func (q GraphIdentityQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= MaximumGraphIdentityLimit &&
		(q.Kind == "" || q.Kind == IdentityPerson || q.Kind == IdentityShared || q.Kind == IdentityPublic || q.Kind == IdentityLab) &&
		validGraphQueryText(q.LabelSearch, 200) && validGraphQueryIDs(q.IdentityIDs) &&
		validGraphQueryIDs(q.DepartmentIDs) && validGraphQueryIDs(q.SiteIDs) &&
		len(q.IdentityIDs)+len(q.DepartmentIDs)+len(q.SiteIDs) <= MaximumGraphIdentityLimit
}

type GraphLocationKind string

const (
	GraphLocationSite       GraphLocationKind = "site"
	GraphLocationBuilding   GraphLocationKind = "building"
	GraphLocationRoom       GraphLocationKind = "room"
	GraphLocationDepartment GraphLocationKind = "department"
)

// GraphLocationQuery is the bounded People location projection used by the
// relationship graph. Exact IDs select endpoints, parent IDs select hierarchy
// children, and DirectOrganizationChildren selects sites plus departments
// without a site. Selectors are combined with OR semantics.
type GraphLocationQuery struct {
	LabelSearch                string
	Kind                       GraphLocationKind
	SiteIDs                    []string
	BuildingIDs                []string
	RoomIDs                    []string
	DepartmentIDs              []string
	ParentSiteIDs              []string
	ParentBuildingIDs          []string
	DirectOrganizationChildren bool
	Limit                      int
}

type GraphLocations struct {
	Sites       []Site
	Buildings   []Building
	Rooms       []Room
	Departments []Department
}

func (q GraphLocationQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= MaximumGraphIdentityLimit &&
		(q.Kind == "" || q.Kind == GraphLocationSite || q.Kind == GraphLocationBuilding ||
			q.Kind == GraphLocationRoom || q.Kind == GraphLocationDepartment) &&
		validGraphQueryText(q.LabelSearch, 200) && validGraphQueryIDs(q.SiteIDs) &&
		validGraphQueryIDs(q.BuildingIDs) && validGraphQueryIDs(q.RoomIDs) && validGraphQueryIDs(q.DepartmentIDs) &&
		validGraphQueryIDs(q.ParentSiteIDs) && validGraphQueryIDs(q.ParentBuildingIDs) &&
		len(q.SiteIDs)+len(q.BuildingIDs)+len(q.RoomIDs)+len(q.DepartmentIDs)+
			len(q.ParentSiteIDs)+len(q.ParentBuildingIDs) <= MaximumGraphIdentityLimit
}

func validGraphQueryText(value string, maximum int) bool {
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

func validGraphQueryIDs(values []string) bool {
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

// ExchangeSnapshot is one bounded, organization-consistent view of every
// portable People record. Repository adapters own the consistency boundary so
// Exchange never stitches together independently changing list results.
type ExchangeSnapshot struct {
	Sites       []Site
	Buildings   []Building
	Rooms       []Room
	Departments []Department
	Identities  []Identity
	Assignments []AssetAssignment
}

// ExchangeImportOperation is the durable mutation identity reserved by
// Exchange before it invokes a domain provider.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. Ordinary
// People callers cannot choose source IDs, revisions, timestamps, or history.
type ExchangeImporter interface {
	peopleExchangeImporter()
	ImportSite(context.Context, ExchangeImportOperation, Site) (ExchangeImportResult, error)
	ImportBuilding(context.Context, ExchangeImportOperation, Building) (ExchangeImportResult, error)
	ImportRoom(context.Context, ExchangeImportOperation, Room) (ExchangeImportResult, error)
	ImportDepartment(context.Context, ExchangeImportOperation, Department) (ExchangeImportResult, error)
	ImportIdentity(context.Context, ExchangeImportOperation, Identity) (ExchangeImportResult, error)
	ImportAssetAssignment(context.Context, ExchangeImportOperation, AssetAssignment) (ExchangeImportResult, error)
}

// WriteGate is the service-layer imported-ownership fence. Exchange receives
// the opaque importer capability that alone bypasses this gate.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

// AssetReader is satisfied by the Atlas repository contract. Keeping this
// interface local lets Atlas become durable without changing People.
type AssetReader interface {
	Get(ctx context.Context, id string) (domain.Asset, error)
}

// Store is provider-neutral. PostgreSQL is the first durable adapter and a
// future DynamoDB adapter must pass the same conformance tests.
type Store interface {
	ExchangeSnapshot(ctx context.Context, organizationID string, maximum int) (ExchangeSnapshot, error)

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
	ListGraphLocations(ctx context.Context, organizationID string, query GraphLocationQuery, visibility Visibility) (GraphLocations, error)

	CreateIdentity(ctx context.Context, identity Identity) (Identity, error)
	GetIdentity(ctx context.Context, organizationID, id string) (Identity, error)
	GetIdentityByProvider(ctx context.Context, organizationID, provider, providerSubject string) (Identity, error)
	GetIdentityByEmail(ctx context.Context, organizationID, normalizedEmail string) (Identity, error)
	ReconcileIdentity(ctx context.Context, identity Identity, expectedRevision uint64) (Identity, error)
	DeleteIdentity(ctx context.Context, organizationID, id string, expectedRevision uint64) error
	SearchIdentities(ctx context.Context, organizationID string, query IdentityQuery, visibility Visibility) ([]Identity, error)
	ListGraphIdentities(ctx context.Context, organizationID string, query GraphIdentityQuery, visibility Visibility) ([]Identity, error)

	CreateAssetAssignment(ctx context.Context, assignment AssetAssignment, replaceActiveRole bool) (AssetAssignment, error)
	ImportAssetAssignment(ctx context.Context, assignment AssetAssignment) (AssetAssignment, error)
	GetAssetAssignment(ctx context.Context, organizationID, assignmentID string) (AssetAssignment, error)
	EndAssetAssignment(ctx context.Context, organizationID, assetID, assignmentID string, effectiveTo time.Time) (AssetAssignment, error)
	ListAssetAssignments(ctx context.Context, organizationID, assetID string) ([]AssetAssignment, error)
}
