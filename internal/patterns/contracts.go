// Package patterns implements versioned record templates and typed validation.
// Requirement: REQ-PATTERNS-001. Feature: templates.schemas. GitHub: #8.
package patterns

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID               = "REQ-PATTERNS-001"
	FeatureID                   = "templates.schemas"
	MaximumFields               = 64
	MaximumExchangeVersions     = 128
	MaximumExchangeHistoryBytes = 100_000
)

var (
	ErrInvalidInput = errors.New("invalid Patterns input")
	ErrNotFound     = errors.New("Patterns template not found")
	ErrConflict     = errors.New("Patterns template conflicts with existing data")
)

type FieldType string

const (
	FieldText       FieldType = "text"
	FieldNumber     FieldType = "number"
	FieldDate       FieldType = "date"
	FieldMoney      FieldType = "money"
	FieldEnum       FieldType = "enum"
	FieldAttachment FieldType = "attachment"
	FieldReference  FieldType = "reference"
)

type TemplateStatus string

const (
	StatusActive  TemplateStatus = "active"
	StatusRetired TemplateStatus = "retired"
)

type ValidationStatus string

const (
	ValidationValid   ValidationStatus = "valid"
	ValidationHolding ValidationStatus = "holding"
	ValidationInvalid ValidationStatus = "invalid"
)

type Field struct {
	Key             string    `json:"key"`
	Label           string    `json:"label"`
	Help            string    `json:"help,omitempty"`
	Type            FieldType `json:"type"`
	Required        bool      `json:"required"`
	AllowHolding    bool      `json:"allowHolding,omitempty"`
	ReferenceType   string    `json:"referenceType,omitempty"`
	Options         []string  `json:"options,omitempty"`
	Minimum         *float64  `json:"minimum,omitempty"`
	Maximum         *float64  `json:"maximum,omitempty"`
	MaximumLength   int       `json:"maximumLength,omitempty"`
	CurrencyField   string    `json:"currencyField,omitempty"`
	AccessibleLabel string    `json:"accessibleLabel"`
	CSVHeader       string    `json:"csvHeader"`
}

type Template struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organizationId,omitempty"`
	RecordType     string         `json:"recordType"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Version        int64          `json:"version"`
	BuiltIn        bool           `json:"builtIn"`
	Status         TemplateStatus `json:"status"`
	Fields         []Field        `json:"fields"`
	CreatedBy      string         `json:"createdBy"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type ListQuery struct {
	RecordType      string
	IncludeRetired  bool
	IncludeVersions bool
}

type CreateTemplateInput struct {
	ID          string  `json:"id,omitempty"`
	RecordType  string  `json:"recordType"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Fields      []Field `json:"fields"`
}

type CopyTemplateInput struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type NewVersionInput struct {
	Description string  `json:"description,omitempty"`
	Fields      []Field `json:"fields"`
}

// ExchangeTemplate is the lossless, organization-neutral representation of
// one custom template and its immutable version history. Built-in templates
// are code-owned at both installations and are therefore never emitted here.
type ExchangeTemplate struct {
	ID         string                    `json:"id"`
	RecordType string                    `json:"recordType"`
	Name       string                    `json:"name"`
	Versions   []ExchangeTemplateVersion `json:"versions"`
}

type ExchangeTemplateVersion struct {
	Description string         `json:"description,omitempty"`
	Version     int64          `json:"version"`
	Status      TemplateStatus `json:"status"`
	Fields      []Field        `json:"fields"`
}

// ExchangeImportOperation is the deterministic mutation identity reserved by
// Exchange before it grants a provider permission to write.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. It is the only
// surface allowed to install an arbitrary immutable template history.
type ExchangeImporter interface {
	ImportTemplate(context.Context, ExchangeImportOperation, ExchangeTemplate) (ExchangeImportResult, error)
	patternsExchangeImporter()
}

// WriteGate fences ordinary edits after Exchange establishes imported
// ownership. The opaque importer bypasses this gate only for its reserved
// deterministic operation.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

// ValidationInput and ValidationResult are intentionally provider-neutral so
// Exchange can validate package rows without depending on REST or PostgreSQL.
type ValidationInput struct {
	Values             map[string]any `json:"values"`
	MissingReferences  []string       `json:"missingReferences,omitempty"`
	AllowHoldingRecord bool           `json:"allowHoldingRecord,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type HoldingReference struct {
	Field         string `json:"field"`
	ReferenceType string `json:"referenceType"`
	Value         string `json:"value,omitempty"`
}

type ValidationResult struct {
	Status            ValidationStatus   `json:"status"`
	NormalizedValues  map[string]any     `json:"normalizedValues"`
	Errors            []FieldError       `json:"errors"`
	HoldingReferences []HoldingReference `json:"holdingReferences"`
}

type Store interface {
	ListTemplates(context.Context, string, ListQuery) ([]Template, error)
	GetTemplate(context.Context, string, string, int64) (Template, error)
	CreateTemplate(context.Context, Template) (Template, error)
	CreateVersion(context.Context, Template) (Template, error)
	ImportTemplateHistory(context.Context, string, []Template) error
}
