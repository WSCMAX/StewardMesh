// Package stack implements organization-scoped software inventory and license
// management. Requirement: REQ-STACK-001. Feature: software.licenses.
package stack

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	RequirementID = "REQ-STACK-001"
	FeatureID     = "software.licenses"
)

var (
	ErrInvalidInput          = errors.New("invalid Stack input")
	ErrNotFound              = errors.New("Stack record not found")
	ErrConflict              = errors.New("Stack record conflicts with existing data")
	ErrReferenceMissing      = errors.New("Stack reference does not exist")
	ErrDurableImportRequired = errors.New("Stack batch imports require the durable Exchange workflow")
)

type Product struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	Publisher      string    `json:"publisher"`
	Category       string    `json:"category,omitempty"`
	Status         string    `json:"status"`
	SourceSystemID string    `json:"sourceSystemId,omitempty"`
	SourceRecordID string    `json:"sourceRecordId,omitempty"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Version struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	ProductID      string     `json:"productId"`
	Name           string     `json:"name"`
	ReleasedOn     *time.Time `json:"releasedOn,omitempty"`
	Status         string     `json:"status"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Installation struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	VersionID      string     `json:"versionId"`
	AssetID        string     `json:"assetId"`
	Status         string     `json:"status"`
	UsageState     string     `json:"usageState"`
	InstalledAt    time.Time  `json:"installedAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	RemovedAt      *time.Time `json:"removedAt,omitempty"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type License struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	ProductID         string     `json:"productId"`
	VersionID         string     `json:"versionId,omitempty"`
	Name              string     `json:"name"`
	EntitlementMetric string     `json:"entitlementMetric"`
	Quantity          int64      `json:"quantity"`
	Status            string     `json:"status"`
	StartsOn          *time.Time `json:"startsOn,omitempty"`
	ExpiresOn         *time.Time `json:"expiresOn,omitempty"`
	VendorID          string     `json:"vendorId,omitempty"`
	PurchaseOrderID   string     `json:"purchaseOrderId,omitempty"`
	ContractID        string     `json:"contractId,omitempty"`
	CostRecordID      string     `json:"costRecordId,omitempty"`
	DocumentIDs       []string   `json:"documentIds"`
	SourceSystemID    string     `json:"sourceSystemId,omitempty"`
	SourceRecordID    string     `json:"sourceRecordId,omitempty"`
	Revision          int64      `json:"revision"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Assignment struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	LicenseID      string     `json:"licenseId"`
	AssigneeKind   string     `json:"assigneeKind"`
	AssigneeID     string     `json:"assigneeId"`
	Seats          int64      `json:"seats"`
	UsageState     string     `json:"usageState"`
	AssignedAt     time.Time  `json:"assignedAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Snapshot struct {
	Products      []Product      `json:"products"`
	Versions      []Version      `json:"versions"`
	Installations []Installation `json:"installations"`
	Licenses      []License      `json:"licenses"`
	Assignments   []Assignment   `json:"assignments"`
}

type Condition struct {
	Code               string `json:"code"`
	Severity           string `json:"severity"`
	ProductID          string `json:"productId"`
	VersionID          string `json:"versionId,omitempty"`
	LicenseID          string `json:"licenseId,omitempty"`
	AssetID            string `json:"assetId,omitempty"`
	EntitledQuantity   int64  `json:"entitledQuantity,omitempty"`
	AssignedQuantity   int64  `json:"assignedQuantity,omitempty"`
	UnderusedQuantity  int64  `json:"underusedQuantity,omitempty"`
	DaysUntilExpiry    int64  `json:"daysUntilExpiry,omitempty"`
	HumanReadableState string `json:"humanReadableState"`
}

type Analytics struct {
	AsOf                 time.Time   `json:"asOf"`
	ExpiringWithinDays   int64       `json:"expiringWithinDays"`
	Products             int         `json:"products"`
	ActiveInstallations  int         `json:"activeInstallations"`
	ActiveLicenses       int         `json:"activeLicenses"`
	EntitledQuantity     int64       `json:"entitledQuantity"`
	AssignedQuantity     int64       `json:"assignedQuantity"`
	UnderusedAssignments int         `json:"underusedAssignments"`
	ComplianceConditions []Condition `json:"complianceConditions"`
}

type CreateProductInput struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	Publisher      string `json:"publisher"`
	Category       string `json:"category,omitempty"`
	Status         string `json:"status,omitempty"`
	SourceSystemID string `json:"sourceSystemId,omitempty"`
	SourceRecordID string `json:"sourceRecordId,omitempty"`
}

type CreateVersionInput struct {
	ID             string     `json:"id,omitempty"`
	ProductID      string     `json:"productId"`
	Name           string     `json:"name"`
	ReleasedOn     *time.Time `json:"releasedOn,omitempty"`
	Status         string     `json:"status,omitempty"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
}

type RecordInstallationInput struct {
	ID             string     `json:"id,omitempty"`
	VersionID      string     `json:"versionId"`
	AssetID        string     `json:"assetId"`
	Status         string     `json:"status,omitempty"`
	UsageState     string     `json:"usageState,omitempty"`
	InstalledAt    time.Time  `json:"installedAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	RemovedAt      *time.Time `json:"removedAt,omitempty"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
}

type CreateLicenseInput struct {
	ID                string     `json:"id,omitempty"`
	ProductID         string     `json:"productId"`
	VersionID         string     `json:"versionId,omitempty"`
	Name              string     `json:"name"`
	EntitlementMetric string     `json:"entitlementMetric"`
	Quantity          int64      `json:"quantity"`
	Status            string     `json:"status,omitempty"`
	StartsOn          *time.Time `json:"startsOn,omitempty"`
	ExpiresOn         *time.Time `json:"expiresOn,omitempty"`
	VendorID          string     `json:"vendorId,omitempty"`
	PurchaseOrderID   string     `json:"purchaseOrderId,omitempty"`
	ContractID        string     `json:"contractId,omitempty"`
	CostRecordID      string     `json:"costRecordId,omitempty"`
	DocumentIDs       []string   `json:"documentIds,omitempty"`
	SourceSystemID    string     `json:"sourceSystemId,omitempty"`
	SourceRecordID    string     `json:"sourceRecordId,omitempty"`
}

type CreateAssignmentInput struct {
	ID             string     `json:"id,omitempty"`
	LicenseID      string     `json:"licenseId"`
	AssigneeKind   string     `json:"assigneeKind"`
	AssigneeID     string     `json:"assigneeId"`
	Seats          int64      `json:"seats"`
	UsageState     string     `json:"usageState,omitempty"`
	AssignedAt     time.Time  `json:"assignedAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	SourceSystemID string     `json:"sourceSystemId,omitempty"`
	SourceRecordID string     `json:"sourceRecordId,omitempty"`
}

type UpdateAssignmentUsageInput struct {
	ID         string     `json:"-"`
	UsageState string     `json:"usageState"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	Revision   int64      `json:"revision"`
}

type UpdateProductStatusInput struct {
	ID       string `json:"-"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

type UpdateVersionStatusInput struct {
	ID       string `json:"-"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

type UpdateInstallationStateInput struct {
	ID         string     `json:"-"`
	Status     string     `json:"status"`
	UsageState string     `json:"usageState"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RemovedAt  *time.Time `json:"removedAt,omitempty"`
	Revision   int64      `json:"revision"`
}

type UpdateLicenseEntitlementInput struct {
	ID        string     `json:"-"`
	Quantity  int64      `json:"quantity"`
	Status    string     `json:"status"`
	StartsOn  *time.Time `json:"startsOn,omitempty"`
	ExpiresOn *time.Time `json:"expiresOn,omitempty"`
	Revision  int64      `json:"revision"`
}

type EndAssignmentInput struct {
	ID       string    `json:"-"`
	EndedAt  time.Time `json:"endedAt"`
	Revision int64     `json:"revision"`
}

type AssetContext struct {
	ID           string
	SiteID       string
	DepartmentID string
	IdentityID   string
}

// ExchangeRecord is Stack's provider-neutral seam for the future Exchange
// package. Dependencies contain stable "type:id" references and Payload is
// bounded canonical JSON produced from a typed Stack record.
type ExchangeRecord struct {
	Type           string          `json:"type"`
	ID             string          `json:"id"`
	Revision       int64           `json:"revision"`
	Dependencies   []string        `json:"dependencies"`
	SourceSystemID string          `json:"sourceSystemId,omitempty"`
	SourceRecordID string          `json:"sourceRecordId,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type ImportResult struct {
	Created   int `json:"created"`
	Unchanged int `json:"unchanged"`
}

type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque, single-owner capability used by the Exchange
// provider. Service itself deliberately does not expose the record-import
// operation, so ordinary in-process callers cannot forge the private context
// that unlocks the low-level mutation seam.
type ExchangeImporter interface {
	ImportExchangeRecord(context.Context, ExchangeImportOperation, string, ExchangeRecord) (ExchangeImportResult, error)
	stackExchangeImporter()
}

type ReferenceValidator interface {
	ResolveAsset(ctx context.Context, assetID string) (AssetContext, error)
	ValidateAssignee(ctx context.Context, kind, id string) error
	ValidateFinancialReferences(ctx context.Context, vendorID, purchaseOrderID, contractID, costRecordID string) error
	ValidateDocuments(ctx context.Context, documentIDs []string) error
}

type Store interface {
	Snapshot(ctx context.Context, organizationID string) (Snapshot, error)
	GetProduct(ctx context.Context, organizationID, id string) (Product, error)
	GetVersion(ctx context.Context, organizationID, id string) (Version, error)
	GetInstallation(ctx context.Context, organizationID, id string) (Installation, error)
	GetLicense(ctx context.Context, organizationID, id string) (License, error)
	GetAssignment(ctx context.Context, organizationID, id string) (Assignment, error)
	CreateProduct(ctx context.Context, product Product) (Product, bool, error)
	CreateVersion(ctx context.Context, version Version) (Version, bool, error)
	CreateInstallation(ctx context.Context, installation Installation) (Installation, bool, error)
	CreateLicense(ctx context.Context, license License) (License, bool, error)
	CreateAssignment(ctx context.Context, assignment Assignment) (Assignment, bool, error)
	UpdateProduct(ctx context.Context, product Product, expectedRevision int64) (Product, error)
	UpdateVersion(ctx context.Context, version Version, expectedRevision int64) (Version, error)
	UpdateInstallation(ctx context.Context, installation Installation, expectedRevision int64) (Installation, error)
	UpdateLicense(ctx context.Context, license License, expectedRevision int64) (License, error)
	UpdateAssignment(ctx context.Context, assignment Assignment, expectedRevision int64) (Assignment, error)
}
