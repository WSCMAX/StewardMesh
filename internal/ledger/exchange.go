package ledger

// Requirements: REQ-LEDGER-001, REQ-EXCHANGE-001. Features: procurement.finance, migration.packages. GitHub: #9.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

// exchangeImporter is deliberately unexported; only the capability returned
// beside its owning service can invoke the source-state preservation seam.
type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct{ operation ExchangeImportOperation }

func (*exchangeImporter) ledgerExchangeImporter() {}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) checkWrite(ctx context.Context, recordType, recordID string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, recordID)
}

func (i *exchangeImporter) ImportVendor(ctx context.Context, operation ExchangeImportOperation, candidate Vendor) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	input := CreateVendorInput{ID: candidate.ID, Name: candidate.Name, ExternalID: candidate.ExternalID, Status: candidate.Status}
	if err != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		candidate.Name != strings.TrimSpace(candidate.Name) || candidate.ExternalID != strings.TrimSpace(candidate.ExternalID) ||
		candidate.Status != strings.ToLower(strings.TrimSpace(candidate.Status)) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if !optionalID(input.ID) || !validTextRange(input.Name, 1, 200) || !validText(input.ExternalID, 200) || !contains(validVendorStatuses, input.Status) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.vendor.created", "vendor", candidate.ID, candidate,
		func(ctx context.Context) (Vendor, error) {
			return i.service.store.GetVendor(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Vendor, error) { return i.service.store.CreateVendor(ctx, candidate) },
		sameExchangeVendor, map[string]string{"status": candidate.Status, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportPurchaseOrder(ctx context.Context, operation ExchangeImportOperation, candidate PurchaseOrder) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	normalized, normalizeErr := normalizePurchaseOrder(CreatePurchaseOrderInput{
		ID: candidate.ID, Number: candidate.Number, VendorID: candidate.VendorID, Status: candidate.Status,
		Currency: candidate.Currency, TotalMinor: candidate.TotalMinor, OrderedOn: candidate.OrderedOn,
		AssetIDs: candidate.AssetIDs, ReceiptDocumentIDs: candidate.ReceiptDocumentIDs,
	})
	if err != nil || normalizeErr != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		!sameExchangePurchaseOrderInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if _, err := i.service.store.GetVendor(ctx, i.service.organizationID, candidate.VendorID); err != nil {
		return ExchangeImportResult{}, mapReferenceError(err)
	}
	if err := i.service.references.ValidateAssets(ctx, candidate.AssetIDs); err != nil {
		return ExchangeImportResult{}, err
	}
	if err := i.service.references.ValidateDocuments(ctx, candidate.ReceiptDocumentIDs); err != nil {
		return ExchangeImportResult{}, err
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.purchase_order.created", "purchase", candidate.ID, candidate,
		func(ctx context.Context) (PurchaseOrder, error) {
			return i.service.store.GetPurchaseOrder(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (PurchaseOrder, error) {
			return i.service.store.CreatePurchaseOrder(ctx, candidate)
		},
		sameExchangePurchaseOrder, map[string]string{"status": candidate.Status, "currency": candidate.Currency, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportContract(ctx context.Context, operation ExchangeImportOperation, candidate Contract) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	normalized, normalizeErr := normalizeContract(CreateContractInput{
		ID: candidate.ID, Name: candidate.Name, VendorID: candidate.VendorID, OperationalStatus: candidate.OperationalStatus,
		FinancialStatus: candidate.FinancialStatus, Currency: candidate.Currency, CeilingMinor: candidate.CeilingMinor,
		StartsOn: candidate.StartsOn, EndsOn: candidate.EndsOn, RenewsOn: candidate.RenewsOn, DocumentIDs: candidate.DocumentIDs,
	})
	if err != nil || normalizeErr != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		!sameExchangeContractInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if _, err := i.service.store.GetVendor(ctx, i.service.organizationID, candidate.VendorID); err != nil {
		return ExchangeImportResult{}, mapReferenceError(err)
	}
	if err := i.service.references.ValidateDocuments(ctx, candidate.DocumentIDs); err != nil {
		return ExchangeImportResult{}, err
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.contract.created", "contract", candidate.ID, candidate,
		func(ctx context.Context) (Contract, error) {
			return i.service.store.GetContract(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Contract, error) { return i.service.store.CreateContract(ctx, candidate) },
		sameExchangeContract, map[string]string{"operationalStatus": candidate.OperationalStatus, "financialStatus": candidate.FinancialStatus, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportCommitment(ctx context.Context, operation ExchangeImportOperation, candidate Commitment) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	normalized, normalizeErr := normalizeCommitment(CreateCommitmentInput{
		ID: candidate.ID, ContractID: candidate.ContractID, Kind: candidate.Kind, Description: candidate.Description,
		Currency: candidate.Currency, AmountMinor: candidate.AmountMinor, StartsOn: candidate.StartsOn, EndsOn: candidate.EndsOn,
		FiscalPeriod: candidate.FiscalPeriod, Scenario: candidate.Scenario,
	})
	if err != nil || normalizeErr != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		!sameExchangeCommitmentInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if _, err := i.service.store.GetContract(ctx, i.service.organizationID, candidate.ContractID); err != nil {
		return ExchangeImportResult{}, mapReferenceError(err)
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.commitment.created", "commitment", candidate.ID, candidate,
		func(ctx context.Context) (Commitment, error) {
			return i.service.store.GetCommitment(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Commitment, error) { return i.service.store.CreateCommitment(ctx, candidate) },
		sameExchangeCommitment, map[string]string{"kind": candidate.Kind, "fiscalPeriod": candidate.FiscalPeriod, "scenario": candidate.Scenario, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportBudget(ctx context.Context, operation ExchangeImportOperation, candidate Budget) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	normalized, normalizeErr := normalizeBudget(CreateBudgetInput{
		ID: candidate.ID, Name: candidate.Name, FiscalPeriod: candidate.FiscalPeriod, Scenario: candidate.Scenario,
		DepartmentID: candidate.DepartmentID, SiteID: candidate.SiteID, Currency: candidate.Currency, AllocatedMinor: candidate.AllocatedMinor,
	})
	if err != nil || normalizeErr != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		!sameExchangeBudgetInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if err := i.service.references.ValidateDirectory(ctx, candidate.SiteID, candidate.DepartmentID); err != nil {
		return ExchangeImportResult{}, err
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.budget.created", "budget", candidate.ID, candidate,
		func(ctx context.Context) (Budget, error) {
			return i.service.store.GetBudget(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Budget, error) { return i.service.store.CreateBudget(ctx, candidate) },
		sameExchangeBudget, map[string]string{"fiscalPeriod": candidate.FiscalPeriod, "scenario": candidate.Scenario, "currency": candidate.Currency, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportCost(ctx context.Context, operation ExchangeImportOperation, candidate CostRecord) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	normalized, normalizeErr := normalizeCost(ReconcileCostInput{
		ID: candidate.ID, Description: candidate.Description, Kind: candidate.Kind, Currency: candidate.Currency,
		AmountMinor: candidate.AmountMinor, FiscalPeriod: candidate.FiscalPeriod, Scenario: candidate.Scenario,
		PurchaseOrderID: candidate.PurchaseOrderID, ContractID: candidate.ContractID, AssetID: candidate.AssetID,
		DepartmentID: candidate.DepartmentID, SiteID: candidate.SiteID, DocumentID: candidate.DocumentID,
		ExternalReference: candidate.ExternalReference, SourceSystemID: candidate.SourceSystemID, SourceRecordID: candidate.SourceRecordID,
	})
	if err != nil || normalizeErr != nil || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) || candidate.OrganizationID != "" ||
		!sameExchangeCostInput(candidate, normalized) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if err := i.service.validateCostReferences(ctx, normalized); err != nil {
		return ExchangeImportResult{}, err
	}
	candidate.OrganizationID = i.service.organizationID
	return importLedgerRecord(ctx, i.service, operation, "ledger.cost.created", "cost", candidate.ID, candidate,
		func(ctx context.Context) (CostRecord, error) {
			return i.service.store.GetCost(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (CostRecord, error) { return i.service.store.CreateCost(ctx, candidate) },
		sameExchangeCost, map[string]string{"kind": candidate.Kind, "fiscalPeriod": candidate.FiscalPeriod, "scenario": candidate.Scenario, "revision": strconv.FormatInt(candidate.Revision, 10)})
}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !idPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func validExchangeState(id string, revision int64, createdAt, updatedAt time.Time) bool {
	return idPattern.MatchString(id) && revision >= 1 && !createdAt.IsZero() && !updatedAt.Before(createdAt) &&
		(revision != 1 || createdAt.Equal(updatedAt)) && portabletime.IsCanonical(createdAt) && portabletime.IsCanonical(updatedAt) &&
		createdAt.Year() >= 2000 && updatedAt.Year() <= 9999
}

func importLedgerRecord[T any](ctx context.Context, service *Service, operation ExchangeImportOperation, action, resourceType, resourceID string, candidate T,
	get func(context.Context) (T, error), create func(context.Context) (T, error), same func(T, T) bool, metadata map[string]string,
) (ExchangeImportResult, error) {
	existing, err := get(ctx)
	if err == nil {
		if !same(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata)
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	_, err = create(ctx)
	if err != nil {
		observed, readErr := get(ctx)
		if readErr == nil && same(observed, candidate) {
			auditErr := service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata)
			return ExchangeImportResult{Committed: true, Created: true}, errors.Join(err, auditErr)
		}
		return ExchangeImportResult{}, errors.Join(err, readErr)
	}
	err = service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata)
	return ExchangeImportResult{Committed: true, Created: true}, err
}

func (s *Service) auditExchange(ctx context.Context, operation ExchangeImportOperation, action, resourceType, resourceID string, metadata map[string]string) error {
	digest := sha256.Sum256([]byte(strings.Join([]string{s.organizationID, operation.Token, action, resourceType, resourceID}, "\x00")))
	copyMetadata := make(map[string]string, len(metadata)+2)
	for key, value := range metadata {
		copyMetadata[key] = value
	}
	copyMetadata["requirementId"] = RequirementID
	copyMetadata["exchangeRequirementId"] = "REQ-EXCHANGE-001"
	return s.auditor.Record(foundation.WithScope(ctx, foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: operation.Token}), foundation.AuditEvent{
		ID: fmt.Sprintf("%x", digest[:]), OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: operation.Token,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: operation.OccurredAt, Metadata: copyMetadata,
	})
}

func sameExchangeVendor(left, right Vendor) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.ExternalID == right.ExternalID &&
		left.Status == right.Status && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangePurchaseOrder(left, right PurchaseOrder) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Number == right.Number && left.VendorID == right.VendorID &&
		left.Status == right.Status && left.Currency == right.Currency && left.TotalMinor == right.TotalMinor && equalExchangeOptionalTime(left.OrderedOn, right.OrderedOn) &&
		equalStrings(left.AssetIDs, right.AssetIDs) && equalStrings(left.ReceiptDocumentIDs, right.ReceiptDocumentIDs) && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeContract(left, right Contract) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.VendorID == right.VendorID &&
		left.OperationalStatus == right.OperationalStatus && left.FinancialStatus == right.FinancialStatus && left.Currency == right.Currency &&
		left.CeilingMinor == right.CeilingMinor && left.StartsOn.Equal(right.StartsOn) && left.EndsOn.Equal(right.EndsOn) &&
		equalExchangeOptionalTime(left.RenewsOn, right.RenewsOn) && equalStrings(left.DocumentIDs, right.DocumentIDs) && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeCommitment(left, right Commitment) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.ContractID == right.ContractID && left.Kind == right.Kind &&
		left.Description == right.Description && left.Currency == right.Currency && left.AmountMinor == right.AmountMinor && left.StartsOn.Equal(right.StartsOn) &&
		left.EndsOn.Equal(right.EndsOn) && left.FiscalPeriod == right.FiscalPeriod && left.Scenario == right.Scenario && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeBudget(left, right Budget) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.FiscalPeriod == right.FiscalPeriod &&
		left.Scenario == right.Scenario && left.DepartmentID == right.DepartmentID && left.SiteID == right.SiteID && left.Currency == right.Currency &&
		left.AllocatedMinor == right.AllocatedMinor && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeCost(left, right CostRecord) bool {
	right.OrganizationID = left.OrganizationID
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && sameCost(left, right) && left.Revision == right.Revision &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangePurchaseOrderInput(value PurchaseOrder, input CreatePurchaseOrderInput) bool {
	return value.ID == input.ID && value.Number == input.Number && value.VendorID == input.VendorID && value.Status == input.Status &&
		value.Currency == input.Currency && value.TotalMinor == input.TotalMinor && equalExchangeOptionalTime(value.OrderedOn, input.OrderedOn) &&
		equalStrings(value.AssetIDs, input.AssetIDs) && equalStrings(value.ReceiptDocumentIDs, input.ReceiptDocumentIDs)
}

func sameExchangeContractInput(value Contract, input CreateContractInput) bool {
	return value.ID == input.ID && value.Name == input.Name && value.VendorID == input.VendorID && value.OperationalStatus == input.OperationalStatus &&
		value.FinancialStatus == input.FinancialStatus && value.Currency == input.Currency && value.CeilingMinor == input.CeilingMinor &&
		value.StartsOn.Equal(input.StartsOn) && value.EndsOn.Equal(input.EndsOn) && equalExchangeOptionalTime(value.RenewsOn, input.RenewsOn) &&
		equalStrings(value.DocumentIDs, input.DocumentIDs)
}

func sameExchangeCommitmentInput(value Commitment, input CreateCommitmentInput) bool {
	return value.ID == input.ID && value.ContractID == input.ContractID && value.Kind == input.Kind && value.Description == input.Description &&
		value.Currency == input.Currency && value.AmountMinor == input.AmountMinor && value.StartsOn.Equal(input.StartsOn) && value.EndsOn.Equal(input.EndsOn) &&
		value.FiscalPeriod == input.FiscalPeriod && value.Scenario == input.Scenario
}

func sameExchangeBudgetInput(value Budget, input CreateBudgetInput) bool {
	return value.ID == input.ID && value.Name == input.Name && value.FiscalPeriod == input.FiscalPeriod && value.Scenario == input.Scenario &&
		value.DepartmentID == input.DepartmentID && value.SiteID == input.SiteID && value.Currency == input.Currency && value.AllocatedMinor == input.AllocatedMinor
}

func sameExchangeCostInput(value CostRecord, input ReconcileCostInput) bool {
	return sameCost(value, costFromInput(input, value.ID, value.OrganizationID, value.CreatedAt, value.UpdatedAt, value.Revision))
}

func equalExchangeOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
