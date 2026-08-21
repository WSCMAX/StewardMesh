// Package ledger implements organization-scoped procurement, contracts,
// commitments, budgets, costs, and reconciliation.
// Requirement: REQ-LEDGER-001. Feature: procurement.finance.
package ledger

import (
	"context"
	"errors"
	"time"
)

const (
	RequirementID = "REQ-LEDGER-001"
	FeatureID     = "procurement.finance"
)

var (
	ErrInvalidInput      = errors.New("invalid Ledger input")
	ErrNotFound          = errors.New("Ledger record not found")
	ErrConflict          = errors.New("Ledger record conflicts with existing data")
	ErrReferenceMissing  = errors.New("Ledger reference does not exist")
	ErrInvalidTransition = errors.New("invalid Ledger status transition")
	ErrTooLarge          = errors.New("Ledger snapshot exceeds a configured limit")
)

type Vendor struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	ExternalID     string    `json:"externalId,omitempty"`
	Status         string    `json:"status"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PurchaseOrderLine struct {
	ID            string `json:"id,omitempty"`
	Description   string `json:"description"`
	Kind          string `json:"kind"`
	AssetID       string `json:"assetId,omitempty"`
	ModelID       string `json:"modelId,omitempty"`
	LicenseID     string `json:"licenseId,omitempty"`
	Quantity      int64  `json:"quantity"`
	UnitCostMinor int64  `json:"unitCostMinor"`
	AmountMinor   int64  `json:"amountMinor"`
}

type PurchaseOrder struct {
	ID                 string             `json:"id"`
	OrganizationID     string             `json:"organizationId"`
	Number             string             `json:"number"`
	VendorID           string             `json:"vendorId"`
	Status             string             `json:"status"`
	Currency           string             `json:"currency"`
	TotalMinor         int64              `json:"totalMinor"`
	OrderedOn          *time.Time         `json:"orderedOn,omitempty"`
	AssetIDs           []string           `json:"assetIds"`
	ReceiptDocumentIDs []string           `json:"receiptDocumentIds"`
	Lines              []PurchaseOrderLine `json:"lines,omitempty"`
	Revision           int64              `json:"revision"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

type Contract struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	Name              string     `json:"name"`
	VendorID          string     `json:"vendorId"`
	OperationalStatus string     `json:"operationalStatus"`
	FinancialStatus   string     `json:"financialStatus"`
	Currency          string     `json:"currency"`
	CeilingMinor      int64      `json:"ceilingMinor"`
	StartsOn          time.Time  `json:"startsOn"`
	EndsOn            time.Time  `json:"endsOn"`
	RenewsOn          *time.Time `json:"renewsOn,omitempty"`
	DocumentIDs       []string   `json:"documentIds"`
	Revision          int64      `json:"revision"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type Commitment struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	ContractID     string    `json:"contractId"`
	Kind           string    `json:"kind"`
	Description    string    `json:"description"`
	Currency       string    `json:"currency"`
	AmountMinor    int64     `json:"amountMinor"`
	StartsOn       time.Time `json:"startsOn"`
	EndsOn         time.Time `json:"endsOn"`
	FiscalPeriod   string    `json:"fiscalPeriod"`
	Scenario       string    `json:"scenario"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Budget struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	FiscalPeriod   string    `json:"fiscalPeriod"`
	Scenario       string    `json:"scenario"`
	DepartmentID   string    `json:"departmentId,omitempty"`
	SiteID         string    `json:"siteId,omitempty"`
	Currency       string    `json:"currency"`
	AllocatedMinor int64     `json:"allocatedMinor"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CostRecord is the current financial state of one cost. SourceSystemID and
// SourceRecordID form an idempotency key for reconciled records.
type CostRecord struct {
	ID                string    `json:"id"`
	OrganizationID    string    `json:"organizationId"`
	Description       string    `json:"description"`
	Kind              string    `json:"kind"`
	Currency          string    `json:"currency"`
	AmountMinor       int64     `json:"amountMinor"`
	FiscalPeriod      string    `json:"fiscalPeriod"`
	Scenario          string    `json:"scenario"`
	PurchaseOrderID   string    `json:"purchaseOrderId,omitempty"`
	ContractID        string    `json:"contractId,omitempty"`
	AssetID           string    `json:"assetId,omitempty"`
	DepartmentID      string    `json:"departmentId,omitempty"`
	SiteID            string    `json:"siteId,omitempty"`
	DocumentID        string    `json:"documentId,omitempty"`
	ExternalReference string    `json:"externalReference,omitempty"`
	SourceSystemID    string    `json:"sourceSystemId,omitempty"`
	SourceRecordID    string    `json:"sourceRecordId,omitempty"`
	Revision          int64     `json:"revision"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Snapshot struct {
	Vendors        []Vendor        `json:"vendors"`
	PurchaseOrders []PurchaseOrder `json:"purchaseOrders"`
	Contracts      []Contract      `json:"contracts"`
	Commitments    []Commitment    `json:"commitments"`
	Budgets        []Budget        `json:"budgets"`
	Costs          []CostRecord    `json:"costs"`
}

type BudgetVariance struct {
	FiscalPeriod       string           `json:"fiscalPeriod"`
	Scenario           string           `json:"scenario"`
	Currency           string           `json:"currency"`
	AllocatedMinor     int64            `json:"allocatedMinor"`
	RecognizedMinor    int64            `json:"recognizedMinor"`
	VarianceMinor      int64            `json:"varianceMinor"`
	OverBudget         bool             `json:"overBudget"`
	AmountsByKindMinor map[string]int64 `json:"amountsByKindMinor"`
}

type CreateVendorInput struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	ExternalID string `json:"externalId,omitempty"`
	Status     string `json:"status,omitempty"`
}

type CreatePurchaseOrderInput struct {
	ID                 string     `json:"id,omitempty"`
	Number             string     `json:"number"`
	VendorID           string     `json:"vendorId"`
	Status             string     `json:"status,omitempty"`
	Currency           string     `json:"currency"`
	TotalMinor         int64      `json:"totalMinor"`
	OrderedOn          *time.Time `json:"orderedOn,omitempty"`
	AssetIDs           []string            `json:"assetIds,omitempty"`
	ReceiptDocumentIDs []string            `json:"receiptDocumentIds,omitempty"`
	Lines              []PurchaseOrderLine `json:"lines,omitempty"`
}

type UpdatePurchaseOrderStatusInput struct {
	ID       string `json:"-"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

type CreateContractInput struct {
	ID                string     `json:"id,omitempty"`
	Name              string     `json:"name"`
	VendorID          string     `json:"vendorId"`
	OperationalStatus string     `json:"operationalStatus,omitempty"`
	FinancialStatus   string     `json:"financialStatus,omitempty"`
	Currency          string     `json:"currency"`
	CeilingMinor      int64      `json:"ceilingMinor"`
	StartsOn          time.Time  `json:"startsOn"`
	EndsOn            time.Time  `json:"endsOn"`
	RenewsOn          *time.Time `json:"renewsOn,omitempty"`
	DocumentIDs       []string   `json:"documentIds,omitempty"`
}

type UpdateContractStatusInput struct {
	ID                string `json:"-"`
	OperationalStatus string `json:"operationalStatus"`
	FinancialStatus   string `json:"financialStatus"`
	Revision          int64  `json:"revision"`
}

type CreateCommitmentInput struct {
	ID           string    `json:"id,omitempty"`
	ContractID   string    `json:"contractId"`
	Kind         string    `json:"kind"`
	Description  string    `json:"description"`
	Currency     string    `json:"currency"`
	AmountMinor  int64     `json:"amountMinor"`
	StartsOn     time.Time `json:"startsOn"`
	EndsOn       time.Time `json:"endsOn"`
	FiscalPeriod string    `json:"fiscalPeriod"`
	Scenario     string    `json:"scenario"`
}

type CreateBudgetInput struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	FiscalPeriod   string `json:"fiscalPeriod"`
	Scenario       string `json:"scenario"`
	DepartmentID   string `json:"departmentId,omitempty"`
	SiteID         string `json:"siteId,omitempty"`
	Currency       string `json:"currency"`
	AllocatedMinor int64  `json:"allocatedMinor"`
}

type ReconcileCostInput struct {
	ID                string `json:"id,omitempty"`
	Description       string `json:"description"`
	Kind              string `json:"kind"`
	Currency          string `json:"currency"`
	AmountMinor       int64  `json:"amountMinor"`
	FiscalPeriod      string `json:"fiscalPeriod"`
	Scenario          string `json:"scenario"`
	PurchaseOrderID   string `json:"purchaseOrderId,omitempty"`
	ContractID        string `json:"contractId,omitempty"`
	AssetID           string `json:"assetId,omitempty"`
	DepartmentID      string `json:"departmentId,omitempty"`
	SiteID            string `json:"siteId,omitempty"`
	DocumentID        string `json:"documentId,omitempty"`
	ExternalReference string `json:"externalReference,omitempty"`
	SourceSystemID    string `json:"sourceSystemId,omitempty"`
	SourceRecordID    string `json:"sourceRecordId,omitempty"`
}

type ReconcileResult struct {
	Record  CostRecord `json:"record"`
	Applied bool       `json:"applied"`
	Created bool       `json:"created"`
}

// ExchangeImportOperation is the deterministic mutation identity reserved by
// Exchange before it invokes Ledger's private importer capability.
type ExchangeImportOperation struct {
	Token      string
	OccurredAt time.Time
}

type ExchangeImportResult struct {
	Committed bool
	Created   bool
}

// ExchangeImporter is an opaque construction-time capability. It is the only
// Ledger surface that can preserve source revisions and timestamps while
// repairing the exact deterministic audit event after an ambiguous commit.
type ExchangeImporter interface {
	ImportVendor(context.Context, ExchangeImportOperation, Vendor) (ExchangeImportResult, error)
	ImportPurchaseOrder(context.Context, ExchangeImportOperation, PurchaseOrder) (ExchangeImportResult, error)
	ImportContract(context.Context, ExchangeImportOperation, Contract) (ExchangeImportResult, error)
	ImportCommitment(context.Context, ExchangeImportOperation, Commitment) (ExchangeImportResult, error)
	ImportBudget(context.Context, ExchangeImportOperation, Budget) (ExchangeImportResult, error)
	ImportCost(context.Context, ExchangeImportOperation, CostRecord) (ExchangeImportResult, error)
	ledgerExchangeImporter()
}

// WriteGate fences ordinary service mutations of imported Ledger resources.
// Exchange alone receives the opaque importer capability that bypasses it.
type WriteGate interface {
	CheckResourceWrite(context.Context, string, string) error
}

type ReferenceValidator interface {
	ValidateAssets(ctx context.Context, assetIDs []string) error
	ValidateDocuments(ctx context.Context, documentIDs []string) error
	ValidateDirectory(ctx context.Context, siteID, departmentID string) error
}

type Store interface {
	Snapshot(ctx context.Context, organizationID string) (Snapshot, error)
	ExchangeSnapshot(ctx context.Context, organizationID string, maximum int) (Snapshot, error)
	GetVendor(ctx context.Context, organizationID, id string) (Vendor, error)
	CreateVendor(ctx context.Context, vendor Vendor) (Vendor, error)
	GetPurchaseOrder(ctx context.Context, organizationID, id string) (PurchaseOrder, error)
	CreatePurchaseOrder(ctx context.Context, purchaseOrder PurchaseOrder) (PurchaseOrder, error)
	UpdatePurchaseOrder(ctx context.Context, purchaseOrder PurchaseOrder, expectedRevision int64) (PurchaseOrder, error)
	GetContract(ctx context.Context, organizationID, id string) (Contract, error)
	CreateContract(ctx context.Context, contract Contract) (Contract, error)
	UpdateContract(ctx context.Context, contract Contract, expectedRevision int64) (Contract, error)
	GetCommitment(ctx context.Context, organizationID, id string) (Commitment, error)
	CreateCommitment(ctx context.Context, commitment Commitment) (Commitment, error)
	GetBudget(ctx context.Context, organizationID, id string) (Budget, error)
	CreateBudget(ctx context.Context, budget Budget) (Budget, error)
	GetCost(ctx context.Context, organizationID, id string) (CostRecord, error)
	GetCostBySource(ctx context.Context, organizationID, sourceSystemID, sourceRecordID string) (CostRecord, error)
	CreateCost(ctx context.Context, cost CostRecord) (CostRecord, error)
	UpdateCost(ctx context.Context, cost CostRecord, expectedRevision int64) (CostRecord, error)
}
