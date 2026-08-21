// Package directoryexpansion implements provider-neutral directory preview,
// reconciliation, apply, and retry contracts.
// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-004, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-006, REQ-DIRECTORY-EXPANSION-008.
// Features: integrations.protocols, identity.directory, threads.relationships.
package directoryexpansion

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/people"
)

const (
	RequirementID          = "REQ-DIRECTORY-EXPANSION-002"
	EntraRequirementID     = "REQ-DIRECTORY-EXPANSION-003"
	GrouperRequirementID   = "REQ-DIRECTORY-EXPANSION-005"
	SailPointRequirementID = "REQ-DIRECTORY-EXPANSION-004"
	GraphRequirementID     = "REQ-DIRECTORY-EXPANSION-008"
	FeatureID              = "integrations.protocols"
	GraphFeatureID         = "threads.relationships"

	DefaultListLimit  = 50
	MaximumListLimit  = 100
	MaximumPages      = 100
	MaximumRecords    = 5000
	MaximumAttempts   = 100
	MaximumSources    = 100
	MaximumAttributes = 16
	MaximumGroupLinks = 256
	DefaultGraphLimit = 100
	MaximumGraphLimit = 50000
	MaximumGraphEdges = 200000
)

var (
	ErrInvalidInput     = errors.New("invalid directory import input")
	ErrNotFound         = errors.New("directory import not found")
	ErrReferenceMissing = errors.New("directory import reference is missing")
	ErrConflict         = errors.New("directory import conflict")
	ErrTooLarge         = errors.New("directory import exceeds a configured limit")
	ErrBusy             = errors.New("directory import is already being processed")
	ErrNotRetryable     = errors.New("directory import has no retryable failures")
	ErrConnectorMissing = errors.New("directory source system is not configured")
	ErrLeaseLost        = errors.New("directory import lease was lost")
	ErrGraphScope       = errors.New("relationship graph visibility scope is required")
)

// Address, Building, and Room remain transport-neutral directory location
// contracts used by the relationship graph seam.
type Address struct {
	Line1      string `json:"line1,omitempty"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
}

type Building struct {
	ID, OrganizationID, SiteID, Name string
	Status                           string
	CreatedAt, UpdatedAt             time.Time
}

type Room struct {
	ID, OrganizationID, SiteID, BuildingID, Number, Name string
	Status                                               string
	CreatedAt, UpdatedAt                                 time.Time
}

// Provider is intentionally open-ended. Runtime connector registration is the
// allowlist; provider-specific slices do not require a schema enum migration.
type Provider string

const (
	EntraProvider     Provider = "entra"
	SailPointProvider Provider = "sailpoint"
)

// SourceSystem identifies one server-configured, read-only connector. Client
// requests can select this stable identifier but cannot submit credentials or
// provider endpoints.
type SourceSystem struct {
	ID             string   `json:"id"`
	Provider       Provider `json:"provider"`
	ConfigRevision string   `json:"configRevision"`
}

type RecordKind string

const (
	RecordIdentity   RecordKind = "identity"
	RecordGroup      RecordKind = "group"
	RecordMembership RecordKind = "membership"
)

type MemberKind string

const (
	MemberSubject MemberKind = "subject"
	MemberGroup   MemberKind = "group"
)

// Record is the bounded normalized payload shared by every provider adapter.
// Raw provider responses and credentials never cross this contract or persist.
type Record struct {
	SourceRecordID      string            `json:"sourceRecordId"`
	Kind                RecordKind        `json:"kind"`
	IdentityKind        string            `json:"identityKind,omitempty"`
	DisplayName         string            `json:"displayName"`
	Email               string            `json:"email,omitempty"`
	Status              string            `json:"status"`
	Department          string            `json:"department,omitempty"`
	DirectoryAttributes map[string]string `json:"directoryAttributes,omitempty"`
	GroupSourceIDs      []string          `json:"groupSourceIds,omitempty"`
	GroupName           string            `json:"groupName,omitempty"`
	Description         string            `json:"description,omitempty"`
	GroupSourceID       string            `json:"groupSourceId,omitempty"`
	MemberSourceID      string            `json:"memberSourceId,omitempty"`
	MemberKind          MemberKind        `json:"memberKind,omitempty"`
	NormalizedMetadata  map[string]string `json:"metadata,omitempty"`
}

// ManagedGroup and ManagedMembership are the provider-neutral authoritative
// targets for group-oriented directory sources. They retain only the bounded,
// normalized fields above; credentials and raw provider payloads never enter
// this store.
type ManagedGroup struct {
	ID, OrganizationID, SourceSystemID, SourceRecordID string
	Name, DisplayName, Description, Status             string
	Metadata                                           map[string]string
	Revision                                           uint64
	CreatedAt, UpdatedAt                               time.Time
}

type ManagedMembership struct {
	ID, OrganizationID, SourceSystemID, SourceRecordID string
	GroupID, GroupSourceID, MemberID, MemberSourceID   string
	MemberKind                                         MemberKind
	MemberDisplayName, Status                          string
	Metadata                                           map[string]string
	Revision                                           uint64
	CreatedAt, UpdatedAt                               time.Time
}

type ManagedGroupGraphQuery struct {
	LabelSearch string
	GroupIDs    []string
	Limit       int
}

type ManagedMembershipGraphQuery struct {
	LabelSearch string
	GroupIDs    []string
	MemberIDs   []string
	Limit       int
}

func (q ManagedGroupGraphQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= MaximumGraphLimit && validGraphContractText(q.LabelSearch, 200) &&
		validGraphContractIDs(q.GroupIDs) && len(q.GroupIDs) <= MaximumGraphLimit
}

func (q ManagedMembershipGraphQuery) Valid() bool {
	return q.Limit >= 1 && q.Limit <= MaximumGraphLimit && validGraphContractText(q.LabelSearch, 200) &&
		validGraphContractIDs(q.GroupIDs) && validGraphContractIDs(q.MemberIDs) &&
		len(q.GroupIDs)+len(q.MemberIDs) <= MaximumGraphLimit
}

func validGraphContractText(value string, maximum int) bool {
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

func validGraphContractIDs(values []string) bool {
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

type GroupTargetStore interface {
	GetManagedGroup(context.Context, string, string) (ManagedGroup, error)
	GetManagedGroupBySource(context.Context, string, string, string) (ManagedGroup, error)
	CreateManagedGroup(context.Context, ManagedGroup) (ManagedGroup, error)
	ReconcileManagedGroup(context.Context, ManagedGroup, uint64) (ManagedGroup, error)
	DeleteManagedGroup(context.Context, string, string, uint64) error
	GetManagedMembership(context.Context, string, string) (ManagedMembership, error)
	GetManagedMembershipBySource(context.Context, string, string, string) (ManagedMembership, error)
	CreateManagedMembership(context.Context, ManagedMembership) (ManagedMembership, error)
	ReconcileManagedMembership(context.Context, ManagedMembership, uint64) (ManagedMembership, error)
	DeleteManagedMembership(context.Context, string, string, uint64) error
	ListManagedGroups(context.Context, string) ([]ManagedGroup, error)
	ListManagedMemberships(context.Context, string) ([]ManagedMembership, error)
	ListGraphManagedGroups(context.Context, string, ManagedGroupGraphQuery) ([]ManagedGroup, error)
	ListGraphManagedMemberships(context.Context, string, ManagedMembershipGraphQuery) ([]ManagedMembership, error)
}

// ExchangeSnapshot is one bounded, organization-consistent view of every
// portable managed group and membership. Repository adapters own the
// consistency boundary so Exchange never stitches together independently
// changing list results.
type ExchangeSnapshot struct {
	Groups      []ManagedGroup
	Memberships []ManagedMembership
}

// ExchangeImportOperation is the deterministic mutation identity reserved by
// Exchange before it invokes Directory's private importer capability.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. Connector
// reconciliation keeps its existing source-owned semantics while ordinary
// callers cannot choose revisions, source timestamps, or managed provenance.
type ExchangeImporter interface {
	directoryExchangeImporter()
	ImportManagedGroup(context.Context, ExchangeImportOperation, ManagedGroup) (ExchangeImportResult, error)
	ImportManagedMembership(context.Context, ExchangeImportOperation, ManagedMembership) (ExchangeImportResult, error)
}

// GroupExchangeStore extends the normal connector target store with an atomic,
// exact-replay import seam and a bounded consistent snapshot.
type GroupExchangeStore interface {
	GroupTargetStore
	ExchangeSnapshot(context.Context, string, int) (ExchangeSnapshot, error)
	ImportManagedGroup(context.Context, ManagedGroup) (ManagedGroup, bool, error)
	ImportManagedMembership(context.Context, ManagedMembership) (ManagedMembership, bool, error)
}

type Page struct {
	Records    []Record
	NextCursor string
	// CompleteSnapshot is a connector assertion that one traversal reached
	// its end. The importer confirms unfenced assertions with a second
	// identical normalized traversal before allowing missing-source actions.
	CompleteSnapshot bool
}

// Connector implementations are provider-specific translators owned by the
// follow-on provider slices. The core only permits opaque bounded pagination.
type Connector interface {
	SourceSystem() SourceSystem
	PullPage(context.Context, string) (Page, error)
}

type Action string

const (
	ActionCreate     Action = "create"
	ActionUpdate     Action = "update"
	ActionDeactivate Action = "deactivate"
	ActionUnchanged  Action = "unchanged"
	ActionConflict   Action = "conflict"
)

type Outcome string

const (
	OutcomePending   Outcome = "pending"
	OutcomeApplied   Outcome = "applied"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeConflict  Outcome = "conflict"
	OutcomeFailed    Outcome = "failed"
)

type BatchStatus string

const (
	BatchPreviewed BatchStatus = "previewed"
	BatchApplying  BatchStatus = "applying"
	BatchApplied   BatchStatus = "applied"
	BatchPartial   BatchStatus = "partially_applied"
	BatchFailed    BatchStatus = "failed"
)

type Operation string

const (
	OperationPreview Operation = "preview"
	OperationApply   Operation = "apply"
	OperationRetry   Operation = "retry"
)

type FailureClass string

const (
	FailureTransient FailureClass = "transient"
	FailurePermanent FailureClass = "permanent"
	FailureConflict  FailureClass = "conflict"
)

// ClassifiedError exposes only a safe, bounded explanation. Provider bodies,
// credentials, emails, and source identifiers must stay in adapter-local logs.
type ClassifiedError struct {
	Class     FailureClass
	Retryable bool
	Message   string
	Cause     error
}

func (e *ClassifiedError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ClassifiedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type Counts struct {
	Created     int `json:"created"`
	Updated     int `json:"updated"`
	Unchanged   int `json:"unchanged"`
	Deactivated int `json:"deactivated"`
	Conflicts   int `json:"conflicts"`
	Failed      int `json:"failed"`
}

type Batch struct {
	ID               string      `json:"id"`
	OrganizationID   string      `json:"-"`
	SourceSystemID   string      `json:"sourceSystemId"`
	Provider         Provider    `json:"provider"`
	ConfigRevision   string      `json:"configRevision"`
	Status           BatchStatus `json:"status"`
	CompleteSnapshot bool        `json:"completeSnapshot"`
	Counts           Counts      `json:"counts"`
	LeaseToken       string      `json:"-"`
	LeaseExpiresAt   *time.Time  `json:"-"`
	CreatedAt        time.Time   `json:"createdAt"`
	UpdatedAt        time.Time   `json:"updatedAt"`
	CompletedAt      *time.Time  `json:"completedAt,omitempty"`
}

// Item is the exact persisted plan. Apply and retry never repull a connector.
type Item struct {
	ID                   string       `json:"id"`
	OrganizationID       string       `json:"-"`
	BatchID              string       `json:"-"`
	Ordinal              int          `json:"ordinal"`
	Record               Record       `json:"record"`
	TargetID             string       `json:"targetId,omitempty"`
	ExpectedRevision     uint64       `json:"expectedRevision,omitempty"`
	SourceDigest         string       `json:"-"`
	ObservedTargetDigest string       `json:"-"`
	PlannedTargetDigest  string       `json:"-"`
	Action               Action       `json:"action"`
	Outcome              Outcome      `json:"outcome"`
	FailureClass         FailureClass `json:"failureClass,omitempty"`
	Retryable            bool         `json:"retryable,omitempty"`
	ErrorMessage         string       `json:"error,omitempty"`
	UpdatedAt            time.Time    `json:"updatedAt"`
}

type Attempt struct {
	ID                 string           `json:"id"`
	OrganizationID     string           `json:"-"`
	BatchID            string           `json:"-"`
	Operation          Operation        `json:"operation"`
	IdempotencyHash    string           `json:"-"`
	RequestFingerprint string           `json:"-"`
	Number             int              `json:"number"`
	Status             BatchStatus      `json:"status"`
	FailureClass       FailureClass     `json:"failureClass,omitempty"`
	Retryable          bool             `json:"retryable,omitempty"`
	ErrorMessage       string           `json:"error,omitempty"`
	ActorID            string           `json:"-"`
	CorrelationID      string           `json:"correlationId"`
	StartedAt          time.Time        `json:"startedAt"`
	CompletedAt        *time.Time       `json:"completedAt,omitempty"`
	Result             *OperationResult `json:"-"`
}

func (a Attempt) Public() Attempt {
	a.ActorID = ""
	return a
}

type Mapping struct {
	OrganizationID      string
	SourceSystemID      string
	Provider            Provider
	SourceRecordID      string
	Kind                RecordKind
	TargetID            string
	SourceDigest        string
	AppliedTargetDigest string
	LastRecord          Record
	Active              bool
	LastSeenBatchID     string
	LastAppliedBatchID  string
	UpdatedAt           time.Time
}

type BatchDetail struct {
	Batch    Batch     `json:"batch"`
	Items    []Item    `json:"items"`
	Attempts []Attempt `json:"attempts"`
}

type OperationResult struct {
	Batch  Batch `json:"batch"`
	Replay bool  `json:"replay"`
}

type ListQuery struct {
	Limit  int
	Cursor string
}

type BatchPage struct {
	Batches    []Batch `json:"batches"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type PreviewRequest struct {
	SourceSystemID string `json:"sourceSystemId"`
}

// TargetPlan is a read-only target observation used to construct the exact
// preview. Conflict is explicit and never silently overwritten.
type TargetPlan struct {
	TargetID       string
	Revision       uint64
	CurrentDigest  string
	DesiredDigest  string
	Found          bool
	SourceMatched  bool
	Conflict       bool
	ConflictReason string
}

type TargetResult struct {
	TargetID string
	Revision uint64
	Digest   string
	Changed  bool
}

type Target interface {
	Preview(context.Context, string, SourceSystem, Record, *Mapping) (TargetPlan, error)
	Apply(context.Context, guard.Authentication, SourceSystem, Item) (TargetResult, error)
	Compensate(context.Context, guard.Authentication, SourceSystem, Item, TargetResult) error
}

// Store owns all authoritative batch, item, attempt, mapping, idempotency, and
// lease state. Valkey is deliberately absent from this contract.
type Store interface {
	FindAttempt(context.Context, string, Operation, string) (Attempt, error)
	CreatePreview(context.Context, Batch, []Item, Attempt) (OperationResult, bool, error)
	GetBatch(context.Context, string, string) (BatchDetail, error)
	ListBatches(context.Context, string, ListQuery) (BatchPage, error)
	ListMappings(context.Context, string, string) ([]Mapping, error)
	BeginOperation(context.Context, string, string, Attempt, string, time.Time, time.Time) (BatchDetail, *OperationResult, error)
	SavePlan(context.Context, string, string, string, bool, []Item) error
	SaveItem(context.Context, string, string, string, Item, *Mapping) error
	FinishOperation(context.Context, string, string, string, Attempt, OperationResult) error
}

type NodeKind string

const (
	NodeOrganization NodeKind = "organization"
	NodeSite         NodeKind = "site"
	NodeBuilding     NodeKind = "building"
	NodeRoom         NodeKind = "room"
	NodeDepartment   NodeKind = "department"
	NodePerson       NodeKind = "person"
	NodeShared       NodeKind = "shared"
	NodePublic       NodeKind = "public"
	NodeLab          NodeKind = "lab"
	NodeGroup        NodeKind = "group"
	NodeSubject      NodeKind = "subject"
	NodeAsset        NodeKind = "asset"
)

type RelationshipKind string

const (
	RelationshipContains     RelationshipKind = "contains"
	RelationshipBelongsTo    RelationshipKind = "belongs_to"
	RelationshipLocatedAt    RelationshipKind = "located_at"
	RelationshipMemberOf     RelationshipKind = "member_of"
	RelationshipAssignedTo   RelationshipKind = "assigned_to"
	RelationshipUsesOffice   RelationshipKind = "uses_office"
	RelationshipTeachesIn    RelationshipKind = "teaches_in"
	RelationshipAttendsClass RelationshipKind = "attends_class"
	RelationshipResidesIn    RelationshipKind = "resides_in"
	RelationshipUsesLab      RelationshipKind = "uses_lab"
)

type Node struct {
	ID         string            `json:"id"`
	Kind       NodeKind          `json:"kind"`
	Label      string            `json:"label"`
	Attributes map[string]string `json:"attributes,omitempty"`
}
type Edge struct {
	ID         string            `json:"id"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	Kind       RelationshipKind  `json:"kind"`
	Attributes map[string]string `json:"attributes,omitempty"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// AssetVisibility is derived exclusively from authenticated Guard grants.
// It is deliberately absent from every transport request schema.
type AssetVisibility struct {
	All           bool
	ResourceIDs   []string
	SiteIDs       []string
	DepartmentIDs []string
}

func (v AssetVisibility) Empty() bool {
	return !v.All && len(v.ResourceIDs) == 0 && len(v.SiteIDs) == 0 && len(v.DepartmentIDs) == 0
}

// GraphScope is server-owned authorization context. Directory visibility is
// mandatory; asset visibility is optional and is intersected with it.
type GraphScope struct {
	Directory people.Visibility
	Assets    AssetVisibility
}

type GraphQuery struct {
	Search       string
	Kind         NodeKind
	Relationship RelationshipKind
	Limit        int
	Scope        GraphScope
}
type GraphStore interface {
	Graph(context.Context, GraphQuery) (Graph, error)
}
