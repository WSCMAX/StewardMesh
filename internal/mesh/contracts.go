// Package mesh projects a permission-scoped, cross-product relationship graph.
// Requirement: REQ-DIRECTORY-EXPANSION-008. Feature: threads.relationships.
package mesh

import (
	"context"
	"errors"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/horizon"
	"github.com/maxlemke/stewardmesh/internal/labels"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

const (
	RequirementID = directoryexpansion.GraphRequirementID
	FeatureID     = directoryexpansion.GraphFeatureID

	DefaultLimit = directoryexpansion.DefaultGraphLimit
	MaximumLimit = directoryexpansion.MaximumGraphLimit
	MaximumEdges = directoryexpansion.MaximumGraphEdges
)

var (
	ErrInvalidInput = errors.New("invalid mesh graph input")
	ErrGraphScope   = errors.New("mesh graph visibility scope is required")
)

type NodeKind = directoryexpansion.NodeKind
type RelationshipKind = directoryexpansion.RelationshipKind
type Node = directoryexpansion.Node
type Edge = directoryexpansion.Edge

const (
	NodeOrganization  NodeKind = directoryexpansion.NodeOrganization
	NodeSite          NodeKind = directoryexpansion.NodeSite
	NodeBuilding      NodeKind = directoryexpansion.NodeBuilding
	NodeRoom          NodeKind = directoryexpansion.NodeRoom
	NodeDepartment    NodeKind = directoryexpansion.NodeDepartment
	NodePerson        NodeKind = directoryexpansion.NodePerson
	NodeShared        NodeKind = directoryexpansion.NodeShared
	NodePublic        NodeKind = directoryexpansion.NodePublic
	NodeLab           NodeKind = directoryexpansion.NodeLab
	NodeGroup         NodeKind = directoryexpansion.NodeGroup
	NodeSubject       NodeKind = directoryexpansion.NodeSubject
	NodeAsset         NodeKind = directoryexpansion.NodeAsset
	NodeModel         NodeKind = "model"
	NodeVendor        NodeKind = "vendor"
	NodePurchaseOrder NodeKind = "purchase_order"
	NodeContract      NodeKind = "contract"
	NodeBudget        NodeKind = "budget"
	NodeCommitment    NodeKind = "commitment"
	NodeProduct       NodeKind = "product"
	NodeVersion       NodeKind = "version"
	NodeLicense       NodeKind = "license"
	NodeLabel         NodeKind = "label"
	NodeGoal          NodeKind = "goal"
	NodeDocument      NodeKind = "document"
	NodePlan          NodeKind = "plan"
)

const (
	RelationshipContains     RelationshipKind = directoryexpansion.RelationshipContains
	RelationshipBelongsTo    RelationshipKind = directoryexpansion.RelationshipBelongsTo
	RelationshipLocatedAt    RelationshipKind = directoryexpansion.RelationshipLocatedAt
	RelationshipMemberOf     RelationshipKind = directoryexpansion.RelationshipMemberOf
	RelationshipAssignedTo   RelationshipKind = directoryexpansion.RelationshipAssignedTo
	RelationshipTaggedWith   RelationshipKind = "tagged_with"
	RelationshipAdvances     RelationshipKind = "advances"
	RelationshipPurchasedVia RelationshipKind = "purchased_via"
	RelationshipSuppliedBy   RelationshipKind = "supplied_by"
	RelationshipCoveredBy    RelationshipKind = "covered_by"
	RelationshipDocumentedBy RelationshipKind = "documented_by"
	RelationshipModeledAs    RelationshipKind = "modeled_as"
	RelationshipInstalledOn  RelationshipKind = "installed_on"
	RelationshipPlannedFor   RelationshipKind = "planned_for"
	RelationshipUsesOffice   RelationshipKind = directoryexpansion.RelationshipUsesOffice
	RelationshipTeachesIn    RelationshipKind = directoryexpansion.RelationshipTeachesIn
	RelationshipAttendsClass RelationshipKind = directoryexpansion.RelationshipAttendsClass
	RelationshipResidesIn    RelationshipKind = directoryexpansion.RelationshipResidesIn
	RelationshipUsesLab      RelationshipKind = directoryexpansion.RelationshipUsesLab
)

const (
	SourcePeople  = "people"
	SourceAtlas   = "atlas"
	SourceLedger  = "ledger"
	SourceStack   = "stack"
	SourceLabels  = "labels"
	SourceGoals   = "goals"
	SourceVault   = "vault"
	SourceHorizon = "horizon"
)

type Graph struct {
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
	Sources []string `json:"sources"`
}

type Scope struct {
	Directory people.Visibility
	Assets    directoryexpansion.AssetVisibility
	Finance   bool
	Software  bool
	Labels    bool
	Goals     bool
	Storage   bool
	Planning  bool
}

func (s Scope) Empty() bool {
	return s.Directory.Empty() && s.Assets.Empty() && !s.Finance && !s.Software && !s.Labels && !s.Goals && !s.Storage && !s.Planning
}

type Query struct {
	Search        string
	Kinds         []NodeKind
	Relationships []RelationshipKind
	Limit         int
	Scope         Scope
}

type DirectoryGraph interface {
	Graph(context.Context, directoryexpansion.GraphQuery) (directoryexpansion.Graph, error)
}

type AtlasReader interface {
	ListModels(context.Context, atlas.ModelQuery) ([]domain.AssetModel, error)
	ListGraphAssets(context.Context, atlas.GraphAssetQuery) ([]domain.Asset, error)
}

type LedgerReader interface {
	Snapshot(context.Context) (ledger.Snapshot, error)
}

type StackReader interface {
	Snapshot(context.Context) (stack.Snapshot, error)
}

type LabelsReader interface {
	Snapshot(context.Context) (labels.Snapshot, error)
}

type GoalsReader interface {
	Snapshot(context.Context) (threads.Snapshot, error)
}

type VaultReader interface {
	ListBlobs(context.Context) ([]storage.Blob, error)
}

type HorizonReader interface {
	ListPlans(context.Context, horizon.ListPlansQuery) ([]horizon.Plan, error)
}

type Dependencies struct {
	Directory      DirectoryGraph
	Atlas          AtlasReader
	Ledger         LedgerReader
	Stack          StackReader
	Labels         LabelsReader
	Goals          GoalsReader
	Vault          VaultReader
	Horizon        HorizonReader
	OrganizationID string
	Organization   string
}

type Store interface {
	Graph(context.Context, Query) (Graph, error)
}
