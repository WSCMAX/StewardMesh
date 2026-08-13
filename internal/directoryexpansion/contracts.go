// Package directoryexpansion implements provider-neutral directory preview,
// reconciliation, apply, and retry contracts.
// Requirement: REQ-DIRECTORY-EXPANSION-002. Feature: integrations.protocols.
package directoryexpansion

import (
	"context"
	"errors"
	"time"

	"github.com/maxlemke/stewardmesh/internal/guard"
)

const (
	RequirementID = "REQ-DIRECTORY-EXPANSION-002"
	FeatureID     = "integrations.protocols"

	DefaultListLimit = 50
	MaximumListLimit = 100
	MaximumPages     = 100
	MaximumRecords   = 5000
	MaximumAttempts  = 100
)

var (
	ErrInvalidInput     = errors.New("invalid directory import input")
	ErrNotFound         = errors.New("directory import not found")
	ErrConflict         = errors.New("directory import conflict")
	ErrBusy             = errors.New("directory import is already being processed")
	ErrNotRetryable     = errors.New("directory import has no retryable failures")
	ErrConnectorMissing = errors.New("directory source system is not configured")
	ErrLeaseLost        = errors.New("directory import lease was lost")
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

// SourceSystem identifies one server-configured, read-only connector. Client
// requests can select this stable identifier but cannot submit credentials or
// provider endpoints.
type SourceSystem struct {
	ID             string   `json:"id"`
	Provider       Provider `json:"provider"`
	ConfigRevision string   `json:"configRevision"`
}

type RecordKind string

const RecordIdentity RecordKind = "identity"

// Record is the bounded normalized payload shared by every provider adapter.
// Raw provider responses and credentials never cross this contract or persist.
type Record struct {
	SourceRecordID string     `json:"sourceRecordId"`
	Kind           RecordKind `json:"kind"`
	IdentityKind   string     `json:"identityKind,omitempty"`
	DisplayName    string     `json:"displayName"`
	Email          string     `json:"email,omitempty"`
	Status         string     `json:"status"`
}

type Page struct {
	Records          []Record
	NextCursor       string
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

type Node struct {
	ID, Kind, Label string
	Attributes      map[string]string `json:"attributes,omitempty"`
}
type Edge struct {
	ID, From, To, Kind string
	Attributes         map[string]string `json:"attributes,omitempty"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
type GraphQuery struct {
	Search, Kind, Relationship string
	Limit                      int
}
type GraphStore interface {
	Graph(context.Context, GraphQuery) (Graph, error)
}
