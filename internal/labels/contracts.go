// Package labels implements organization-scoped configurable tags that can be
// applied to any durable record with typed values. Requirement: REQ-LABELS-001.
// Feature: identity.labels.
package labels

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID = "REQ-LABELS-001"
	FeatureID     = "identity.labels"
)

var (
	ErrInvalidInput          = errors.New("invalid Labels input")
	ErrNotFound              = errors.New("Labels record not found")
	ErrConflict              = errors.New("Labels record conflicts with existing data")
	ErrCycle                 = errors.New("Labels hierarchy would contain a cycle")
	ErrHasChildren           = errors.New("label definition has child tags")
	ErrConfirmationRequired  = errors.New("label definition delete confirmation is required")
)

type ValueKind string

const (
	ValueFlag        ValueKind = "flag"
	ValueText        ValueKind = "text"
	ValueSelect      ValueKind = "select"
	ValueMultiSelect ValueKind = "multiselect"
)

type DefinitionStatus string

const (
	StatusActive  DefinitionStatus = "active"
	StatusRetired DefinitionStatus = "retired"
)

type Definition struct {
	ID                    string           `json:"id"`
	OrganizationID        string           `json:"organizationId"`
	Name                  string           `json:"name"`
	Description           string           `json:"description,omitempty"`
	ValueKind             ValueKind        `json:"valueKind"`
	ApplicableRecordTypes []string         `json:"applicableRecordTypes"`
	Options               []string         `json:"options,omitempty"`
	ParentID              string           `json:"parentId,omitempty"`
	GoalID                string           `json:"goalId,omitempty"`
	Status                DefinitionStatus `json:"status"`
	Revision              int64            `json:"revision"`
	CreatedAt             time.Time        `json:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt"`
}

type Assignment struct {
	OrganizationID string    `json:"organizationId"`
	DefinitionID   string    `json:"definitionId"`
	RecordType     string    `json:"recordType"`
	RecordID       string    `json:"recordId"`
	ValueText      string    `json:"valueText,omitempty"`
	Values         []string  `json:"values,omitempty"`
	Revision       int64     `json:"revision"`
	UpdatedBy      string    `json:"updatedBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Snapshot struct {
	Definitions []Definition `json:"definitions"`
	Assignments []Assignment `json:"assignments"`
}

type CreateDefinitionInput struct {
	ID                    string    `json:"id,omitempty"`
	Name                  string    `json:"name"`
	Description           string    `json:"description,omitempty"`
	ValueKind             ValueKind `json:"valueKind"`
	ApplicableRecordTypes []string  `json:"applicableRecordTypes"`
	Options               []string  `json:"options,omitempty"`
	ParentID              string    `json:"parentId,omitempty"`
	GoalID                string    `json:"goalId,omitempty"`
}

type UpdateDefinitionInput struct {
	ID                    string           `json:"-"`
	Name                  string           `json:"name"`
	Description           string           `json:"description,omitempty"`
	ValueKind             ValueKind        `json:"valueKind"`
	ApplicableRecordTypes []string         `json:"applicableRecordTypes"`
	Options               []string         `json:"options,omitempty"`
	ParentID              string           `json:"parentId,omitempty"`
	GoalID                string           `json:"goalId,omitempty"`
	Status                DefinitionStatus `json:"status"`
	Revision              int64            `json:"revision"`
}

type DeleteDefinitionMode string

const (
	DeleteModeStrict          DeleteDefinitionMode = "strict"
	DeleteModeOrphanChildren  DeleteDefinitionMode = "orphan_children"
	DeleteModeCascadeChildren DeleteDefinitionMode = "cascade_children"
)

type DeleteDefinitionInput struct {
	ID      string               `json:"-"`
	Revision int64               `json:"revision"`
	Mode    DeleteDefinitionMode `json:"mode"`
	Confirm bool                 `json:"confirm"`
}

type DefinitionRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AffectedAssignment struct {
	DefinitionID   string `json:"definitionId"`
	DefinitionName string `json:"definitionName"`
	RecordType     string `json:"recordType"`
	RecordID       string `json:"recordId"`
}

type DeleteDefinitionImpact struct {
	DefinitionsRemoved []DefinitionRef      `json:"definitionsRemoved"`
	AssignmentsRemoved []AffectedAssignment `json:"assignmentsRemoved"`
}

type DeleteDefinitionPreview struct {
	Definition            DefinitionRef          `json:"definition"`
	ChildDefinitions      []DefinitionRef        `json:"childDefinitions"`
	HasChildren           bool                   `json:"hasChildren"`
	OrphanChildrenOption  DeleteDefinitionImpact `json:"orphanChildrenOption"`
	CascadeChildrenOption DeleteDefinitionImpact `json:"cascadeChildrenOption"`
}

type SetAssignmentInput struct {
	DefinitionID string   `json:"definitionId"`
	RecordType   string   `json:"recordType"`
	RecordID     string   `json:"recordId"`
	ValueText    string   `json:"valueText,omitempty"`
	Values       []string `json:"values,omitempty"`
	Revision     int64    `json:"revision"`
}

type NormalizedValue struct {
	ValueText string
	Values    []string
}

type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

type ExchangeImporter interface {
	ImportDefinition(context.Context, ExchangeImportOperation, Definition) (ExchangeImportResult, error)
	ImportAssignment(context.Context, ExchangeImportOperation, Assignment) (ExchangeImportResult, error)
	labelsExchangeImporter()
}

type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

type RecordValidator interface {
	ValidateRecord(ctx context.Context, organizationID, recordType, recordID string) error
}

type GoalValidator interface {
	ValidateGoal(ctx context.Context, organizationID, goalID string) error
}

type Store interface {
	Snapshot(ctx context.Context, organizationID string) (Snapshot, error)

	ListDefinitions(ctx context.Context, organizationID string) ([]Definition, error)
	GetDefinition(ctx context.Context, organizationID, id string) (Definition, error)
	CreateDefinition(ctx context.Context, definition Definition) (Definition, error)
	UpdateDefinition(ctx context.Context, definition Definition, expectedRevision int64) (Definition, error)
	DeleteDefinitions(ctx context.Context, organizationID, rootID string, rootExpectedRevision int64, definitionIDs []string, orphanRemainingChildren bool) error

	ListAssignments(ctx context.Context, organizationID, recordType, recordID string) ([]Assignment, error)
	ListAssignmentsForDefinition(ctx context.Context, organizationID, definitionID string) ([]Assignment, error)
	GetAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string) (Assignment, error)
	PutAssignment(ctx context.Context, assignment Assignment, expectedRevision int64) (Assignment, error)
	DeleteAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string, expectedRevision int64) error
}
