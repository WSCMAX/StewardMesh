package ledger

// Requirement: REQ-LEDGER-001. Feature: procurement.finance.

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

var (
	idPattern                  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	currencyPattern            = regexp.MustCompile(`^[A-Z]{3}$`)
	fiscalPeriodPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$`)
	scenarioPattern            = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	validVendorStatuses        = values("active", "inactive")
	validPurchaseOrderStatuses = values("draft", "approved", "ordered", "partially_received", "received", "cancelled")
	validOperationalStatuses   = values("planned", "active", "suspended", "expired", "terminated", "cancelled")
	validFinancialStatuses     = values("planned", "committed", "billed", "paid", "closed", "cancelled")
	validCommitmentKinds       = values("savings_plan", "subscription", "reserved_capacity", "lease", "maintenance", "license", "financing", "other")
	validCostKinds             = values("planned", "estimated", "actual", "billed", "paid", "committed", "normalized_real", "tco")
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	references     ReferenceValidator
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, references ReferenceValidator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, references, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, references ReferenceValidator, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || references == nil || auditor == nil {
		return nil, nil, errors.New("Ledger store, reference validator, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, errors.New("Ledger organization id is required")
	}
	clock := configuration.Now
	if clock == nil {
		clock = time.Now
	}
	service := &Service{store: store, references: references, writes: writes, auditor: auditor, organizationID: configuration.OrganizationID,
		now: func() time.Time { return portabletime.Normalize(clock()) }}
	return service, &exchangeImporter{service: service}, nil
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.store.Snapshot(ctx, s.organizationID)
}

func (s *Service) ExchangeSnapshot(ctx context.Context, maximum int) (Snapshot, error) {
	if maximum < 1 {
		return Snapshot{}, ErrInvalidInput
	}
	return s.store.ExchangeSnapshot(ctx, s.organizationID, maximum)
}

func (s *Service) GetVendor(ctx context.Context, id string) (Vendor, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return Vendor{}, ErrInvalidInput
	}
	return s.store.GetVendor(ctx, s.organizationID, id)
}

func (s *Service) GetPurchaseOrder(ctx context.Context, id string) (PurchaseOrder, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return PurchaseOrder{}, ErrInvalidInput
	}
	return s.store.GetPurchaseOrder(ctx, s.organizationID, id)
}

func (s *Service) GetContract(ctx context.Context, id string) (Contract, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return Contract{}, ErrInvalidInput
	}
	return s.store.GetContract(ctx, s.organizationID, id)
}

func (s *Service) GetCommitment(ctx context.Context, id string) (Commitment, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return Commitment{}, ErrInvalidInput
	}
	return s.store.GetCommitment(ctx, s.organizationID, id)
}

func (s *Service) GetBudget(ctx context.Context, id string) (Budget, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return Budget{}, ErrInvalidInput
	}
	return s.store.GetBudget(ctx, s.organizationID, id)
}

func (s *Service) GetCost(ctx context.Context, id string) (CostRecord, error) {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return CostRecord{}, ErrInvalidInput
	}
	return s.store.GetCost(ctx, s.organizationID, id)
}

func (s *Service) CreateVendor(ctx context.Context, input CreateVendorInput) (Vendor, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "active"
	}
	if !optionalID(input.ID) || !validTextRange(input.Name, 1, 200) || !validText(input.ExternalID, 200) || !contains(validVendorStatuses, input.Status) {
		return Vendor{}, ErrInvalidInput
	}
	id, err := s.id(input.ID)
	if err != nil {
		return Vendor{}, err
	}
	if err := s.checkWrite(ctx, "ledger.vendor", id); err != nil {
		return Vendor{}, err
	}
	now := s.now().UTC()
	vendor, err := s.store.CreateVendor(ctx, Vendor{
		ID: id, OrganizationID: s.organizationID, Name: input.Name, ExternalID: input.ExternalID,
		Status: input.Status, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Vendor{}, err
	}
	if err := s.audit(ctx, "ledger.vendor.created", "vendor", vendor.ID, map[string]string{"status": vendor.Status}); err != nil {
		return Vendor{}, fmt.Errorf("audit Ledger vendor creation: %w", err)
	}
	return vendor, nil
}

func (s *Service) CreatePurchaseOrder(ctx context.Context, input CreatePurchaseOrderInput) (PurchaseOrder, error) {
	normalized, err := normalizePurchaseOrder(input)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if _, err := s.store.GetVendor(ctx, s.organizationID, normalized.VendorID); err != nil {
		return PurchaseOrder{}, mapReferenceError(err)
	}
	if err := s.references.ValidateAssets(ctx, normalized.AssetIDs); err != nil {
		return PurchaseOrder{}, err
	}
	if err := s.references.ValidateDocuments(ctx, normalized.ReceiptDocumentIDs); err != nil {
		return PurchaseOrder{}, err
	}
	id, err := s.id(normalized.ID)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := s.checkWrite(ctx, "ledger.purchase-order", id); err != nil {
		return PurchaseOrder{}, err
	}
	now := s.now().UTC()
	purchaseOrder, err := s.store.CreatePurchaseOrder(ctx, PurchaseOrder{
		ID: id, OrganizationID: s.organizationID, Number: normalized.Number, VendorID: normalized.VendorID,
		Status: normalized.Status, Currency: normalized.Currency, TotalMinor: normalized.TotalMinor,
		OrderedOn: cloneDate(normalized.OrderedOn), AssetIDs: normalized.AssetIDs,
		ReceiptDocumentIDs: normalized.ReceiptDocumentIDs, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := s.audit(ctx, "ledger.purchase_order.created", "purchase", purchaseOrder.ID, map[string]string{
		"status": purchaseOrder.Status, "currency": purchaseOrder.Currency, "revision": "1",
	}); err != nil {
		return PurchaseOrder{}, fmt.Errorf("audit Ledger purchase order creation: %w", err)
	}
	return purchaseOrder, nil
}

func (s *Service) UpdatePurchaseOrderStatus(ctx context.Context, input UpdatePurchaseOrderStatusInput) (PurchaseOrder, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if !idPattern.MatchString(input.ID) || input.Revision < 1 || !contains(validPurchaseOrderStatuses, input.Status) {
		return PurchaseOrder{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "ledger.purchase-order", input.ID); err != nil {
		return PurchaseOrder{}, err
	}
	existing, err := s.store.GetPurchaseOrder(ctx, s.organizationID, input.ID)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if existing.Revision != input.Revision {
		return PurchaseOrder{}, ErrConflict
	}
	if !purchaseOrderTransition(existing.Status, input.Status) {
		return PurchaseOrder{}, ErrInvalidTransition
	}
	updated := existing
	updated.Status = input.Status
	updated.Revision++
	updated.UpdatedAt = portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err = s.store.UpdatePurchaseOrder(ctx, updated, existing.Revision)
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := s.audit(ctx, "ledger.purchase_order.status_changed", "purchase", updated.ID, map[string]string{
		"from": existing.Status, "to": updated.Status, "revision": strconv.FormatInt(updated.Revision, 10),
	}); err != nil {
		return PurchaseOrder{}, fmt.Errorf("audit Ledger purchase order status: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateContract(ctx context.Context, input CreateContractInput) (Contract, error) {
	normalized, err := normalizeContract(input)
	if err != nil {
		return Contract{}, err
	}
	if _, err := s.store.GetVendor(ctx, s.organizationID, normalized.VendorID); err != nil {
		return Contract{}, mapReferenceError(err)
	}
	if err := s.references.ValidateDocuments(ctx, normalized.DocumentIDs); err != nil {
		return Contract{}, err
	}
	id, err := s.id(normalized.ID)
	if err != nil {
		return Contract{}, err
	}
	if err := s.checkWrite(ctx, "ledger.contract", id); err != nil {
		return Contract{}, err
	}
	now := s.now().UTC()
	contract, err := s.store.CreateContract(ctx, Contract{
		ID: id, OrganizationID: s.organizationID, Name: normalized.Name, VendorID: normalized.VendorID,
		OperationalStatus: normalized.OperationalStatus, FinancialStatus: normalized.FinancialStatus,
		Currency: normalized.Currency, CeilingMinor: normalized.CeilingMinor,
		StartsOn: normalized.StartsOn, EndsOn: normalized.EndsOn, RenewsOn: cloneDate(normalized.RenewsOn),
		DocumentIDs: normalized.DocumentIDs, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Contract{}, err
	}
	if err := s.audit(ctx, "ledger.contract.created", "contract", contract.ID, map[string]string{
		"operationalStatus": contract.OperationalStatus, "financialStatus": contract.FinancialStatus, "revision": "1",
	}); err != nil {
		return Contract{}, fmt.Errorf("audit Ledger contract creation: %w", err)
	}
	return contract, nil
}

func (s *Service) UpdateContractStatus(ctx context.Context, input UpdateContractStatusInput) (Contract, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.OperationalStatus = strings.ToLower(strings.TrimSpace(input.OperationalStatus))
	input.FinancialStatus = strings.ToLower(strings.TrimSpace(input.FinancialStatus))
	if !idPattern.MatchString(input.ID) || input.Revision < 1 || !contains(validOperationalStatuses, input.OperationalStatus) || !contains(validFinancialStatuses, input.FinancialStatus) {
		return Contract{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "ledger.contract", input.ID); err != nil {
		return Contract{}, err
	}
	existing, err := s.store.GetContract(ctx, s.organizationID, input.ID)
	if err != nil {
		return Contract{}, err
	}
	if existing.Revision != input.Revision {
		return Contract{}, ErrConflict
	}
	if !contractOperationalTransition(existing.OperationalStatus, input.OperationalStatus) || !contractFinancialTransition(existing.FinancialStatus, input.FinancialStatus) {
		return Contract{}, ErrInvalidTransition
	}
	updated := existing
	updated.OperationalStatus = input.OperationalStatus
	updated.FinancialStatus = input.FinancialStatus
	updated.Revision++
	updated.UpdatedAt = portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err = s.store.UpdateContract(ctx, updated, existing.Revision)
	if err != nil {
		return Contract{}, err
	}
	if err := s.audit(ctx, "ledger.contract.status_changed", "contract", updated.ID, map[string]string{
		"operationalStatus": updated.OperationalStatus, "financialStatus": updated.FinancialStatus,
		"revision": strconv.FormatInt(updated.Revision, 10),
	}); err != nil {
		return Contract{}, fmt.Errorf("audit Ledger contract status: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateCommitment(ctx context.Context, input CreateCommitmentInput) (Commitment, error) {
	normalized, err := normalizeCommitment(input)
	if err != nil {
		return Commitment{}, err
	}
	if _, err := s.store.GetContract(ctx, s.organizationID, normalized.ContractID); err != nil {
		return Commitment{}, mapReferenceError(err)
	}
	id, err := s.id(normalized.ID)
	if err != nil {
		return Commitment{}, err
	}
	if err := s.checkWrite(ctx, "ledger.commitment", id); err != nil {
		return Commitment{}, err
	}
	now := s.now().UTC()
	commitment, err := s.store.CreateCommitment(ctx, Commitment{
		ID: id, OrganizationID: s.organizationID, ContractID: normalized.ContractID, Kind: normalized.Kind,
		Description: normalized.Description, Currency: normalized.Currency, AmountMinor: normalized.AmountMinor,
		StartsOn: normalized.StartsOn, EndsOn: normalized.EndsOn, FiscalPeriod: normalized.FiscalPeriod,
		Scenario: normalized.Scenario, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Commitment{}, err
	}
	if err := s.audit(ctx, "ledger.commitment.created", "commitment", commitment.ID, map[string]string{
		"kind": commitment.Kind, "fiscalPeriod": commitment.FiscalPeriod, "scenario": commitment.Scenario,
	}); err != nil {
		return Commitment{}, fmt.Errorf("audit Ledger commitment creation: %w", err)
	}
	return commitment, nil
}

func (s *Service) CreateBudget(ctx context.Context, input CreateBudgetInput) (Budget, error) {
	normalized, err := normalizeBudget(input)
	if err != nil {
		return Budget{}, err
	}
	if err := s.references.ValidateDirectory(ctx, normalized.SiteID, normalized.DepartmentID); err != nil {
		return Budget{}, err
	}
	id, err := s.id(normalized.ID)
	if err != nil {
		return Budget{}, err
	}
	if err := s.checkWrite(ctx, "ledger.budget", id); err != nil {
		return Budget{}, err
	}
	now := s.now().UTC()
	budget, err := s.store.CreateBudget(ctx, Budget{
		ID: id, OrganizationID: s.organizationID, Name: normalized.Name, FiscalPeriod: normalized.FiscalPeriod,
		Scenario: normalized.Scenario, DepartmentID: normalized.DepartmentID, SiteID: normalized.SiteID,
		Currency: normalized.Currency, AllocatedMinor: normalized.AllocatedMinor,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Budget{}, err
	}
	if err := s.audit(ctx, "ledger.budget.created", "budget", budget.ID, map[string]string{
		"fiscalPeriod": budget.FiscalPeriod, "scenario": budget.Scenario, "currency": budget.Currency,
	}); err != nil {
		return Budget{}, fmt.Errorf("audit Ledger budget creation: %w", err)
	}
	return budget, nil
}

func (s *Service) ReconcileCost(ctx context.Context, input ReconcileCostInput) (ReconcileResult, error) {
	normalized, err := normalizeCost(input)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := s.validateCostReferences(ctx, normalized); err != nil {
		return ReconcileResult{}, err
	}
	if normalized.SourceSystemID != "" {
		existing, err := s.store.GetCostBySource(ctx, s.organizationID, normalized.SourceSystemID, normalized.SourceRecordID)
		switch {
		case err == nil:
			if err := s.checkWrite(ctx, "ledger.cost", existing.ID); err != nil {
				return ReconcileResult{}, err
			}
			candidate := costFromInput(normalized, existing.ID, s.organizationID, existing.CreatedAt, portabletime.Max(s.now(), existing.UpdatedAt), existing.Revision+1)
			if sameCost(existing, candidate) {
				return ReconcileResult{Record: existing}, nil
			}
			updated, err := s.store.UpdateCost(ctx, candidate, existing.Revision)
			if err != nil {
				return ReconcileResult{}, err
			}
			if err := s.audit(ctx, "ledger.cost.reconciled", "cost", updated.ID, map[string]string{
				"kind": updated.Kind, "sourceSystemId": updated.SourceSystemID, "revision": strconv.FormatInt(updated.Revision, 10),
			}); err != nil {
				return ReconcileResult{}, fmt.Errorf("audit Ledger cost reconciliation: %w", err)
			}
			return ReconcileResult{Record: updated, Applied: true}, nil
		case !errors.Is(err, ErrNotFound):
			return ReconcileResult{}, err
		}
	}
	id, err := s.id(normalized.ID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if err := s.checkWrite(ctx, "ledger.cost", id); err != nil {
		return ReconcileResult{}, err
	}
	now := s.now().UTC()
	created, err := s.store.CreateCost(ctx, costFromInput(normalized, id, s.organizationID, now, now, 1))
	if err != nil {
		if errors.Is(err, ErrConflict) && normalized.SourceSystemID != "" {
			existing, getErr := s.store.GetCostBySource(ctx, s.organizationID, normalized.SourceSystemID, normalized.SourceRecordID)
			if getErr == nil && sameCost(existing, costFromInput(normalized, existing.ID, s.organizationID, existing.CreatedAt, existing.UpdatedAt, existing.Revision)) {
				return ReconcileResult{Record: existing}, nil
			}
			if getErr != nil && !errors.Is(getErr, ErrNotFound) {
				return ReconcileResult{}, getErr
			}
		}
		return ReconcileResult{}, err
	}
	if err := s.audit(ctx, "ledger.cost.created", "cost", created.ID, map[string]string{
		"kind": created.Kind, "fiscalPeriod": created.FiscalPeriod, "scenario": created.Scenario,
	}); err != nil {
		return ReconcileResult{}, fmt.Errorf("audit Ledger cost creation: %w", err)
	}
	return ReconcileResult{Record: created, Applied: true, Created: true}, nil
}

func (s *Service) BudgetVariance(ctx context.Context, fiscalPeriod, scenario string) (BudgetVariance, error) {
	fiscalPeriod = strings.TrimSpace(fiscalPeriod)
	scenario = strings.ToLower(strings.TrimSpace(scenario))
	if !fiscalPeriodPattern.MatchString(fiscalPeriod) || !scenarioPattern.MatchString(scenario) {
		return BudgetVariance{}, ErrInvalidInput
	}
	snapshot, err := s.store.Snapshot(ctx, s.organizationID)
	if err != nil {
		return BudgetVariance{}, err
	}
	report := BudgetVariance{FiscalPeriod: fiscalPeriod, Scenario: scenario, AmountsByKindMinor: make(map[string]int64)}
	currencies := make(map[string]struct{})
	for _, budget := range snapshot.Budgets {
		if budget.FiscalPeriod == fiscalPeriod && budget.Scenario == scenario {
			var ok bool
			report.AllocatedMinor, ok = addMinor(report.AllocatedMinor, budget.AllocatedMinor)
			if !ok {
				return BudgetVariance{}, ErrConflict
			}
			currencies[budget.Currency] = struct{}{}
		}
	}
	for _, cost := range snapshot.Costs {
		if cost.FiscalPeriod != fiscalPeriod || cost.Scenario != scenario {
			continue
		}
		amount, ok := addMinor(report.AmountsByKindMinor[cost.Kind], cost.AmountMinor)
		if !ok {
			return BudgetVariance{}, ErrConflict
		}
		report.AmountsByKindMinor[cost.Kind] = amount
		currencies[cost.Currency] = struct{}{}
		if cost.Kind == "actual" || cost.Kind == "billed" || cost.Kind == "paid" || cost.Kind == "committed" {
			report.RecognizedMinor, ok = addMinor(report.RecognizedMinor, cost.AmountMinor)
			if !ok {
				return BudgetVariance{}, ErrConflict
			}
		}
	}
	if len(currencies) > 1 {
		return BudgetVariance{}, ErrConflict
	}
	for currency := range currencies {
		report.Currency = currency
	}
	report.VarianceMinor = report.AllocatedMinor - report.RecognizedMinor
	report.OverBudget = report.VarianceMinor < 0
	return report, nil
}

func (s *Service) ExportCSV(ctx context.Context, fiscalPeriod, scenario string) ([]byte, error) {
	fiscalPeriod = strings.TrimSpace(fiscalPeriod)
	scenario = strings.ToLower(strings.TrimSpace(scenario))
	if !fiscalPeriodPattern.MatchString(fiscalPeriod) || !scenarioPattern.MatchString(scenario) {
		return nil, ErrInvalidInput
	}
	snapshot, err := s.store.Snapshot(ctx, s.organizationID)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"record_type", "id", "description", "kind", "amount_minor", "currency", "fiscal_period", "scenario", "reference"})
	for _, budget := range snapshot.Budgets {
		if budget.FiscalPeriod == fiscalPeriod && budget.Scenario == scenario {
			_ = writer.Write([]string{"budget", budget.ID, safeCSVCell(budget.Name), "allocated", strconv.FormatInt(budget.AllocatedMinor, 10), budget.Currency, budget.FiscalPeriod, budget.Scenario, safeCSVCell(budget.DepartmentID)})
		}
	}
	for _, commitment := range snapshot.Commitments {
		if commitment.FiscalPeriod == fiscalPeriod && commitment.Scenario == scenario {
			_ = writer.Write([]string{"commitment", commitment.ID, safeCSVCell(commitment.Description), commitment.Kind, strconv.FormatInt(commitment.AmountMinor, 10), commitment.Currency, commitment.FiscalPeriod, commitment.Scenario, safeCSVCell(commitment.ContractID)})
		}
	}
	for _, cost := range snapshot.Costs {
		if cost.FiscalPeriod == fiscalPeriod && cost.Scenario == scenario {
			reference := firstNonEmpty(cost.PurchaseOrderID, cost.ContractID, cost.AssetID, cost.ExternalReference)
			_ = writer.Write([]string{"cost", cost.ID, safeCSVCell(cost.Description), cost.Kind, strconv.FormatInt(cost.AmountMinor, 10), cost.Currency, cost.FiscalPeriod, cost.Scenario, safeCSVCell(reference)})
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("write Ledger CSV: %w", err)
	}
	return output.Bytes(), nil
}

func (s *Service) validateCostReferences(ctx context.Context, input ReconcileCostInput) error {
	if input.PurchaseOrderID != "" {
		if _, err := s.store.GetPurchaseOrder(ctx, s.organizationID, input.PurchaseOrderID); err != nil {
			return mapReferenceError(err)
		}
	}
	if input.ContractID != "" {
		if _, err := s.store.GetContract(ctx, s.organizationID, input.ContractID); err != nil {
			return mapReferenceError(err)
		}
	}
	if input.AssetID != "" {
		if err := s.references.ValidateAssets(ctx, []string{input.AssetID}); err != nil {
			return err
		}
	}
	if input.DocumentID != "" {
		if err := s.references.ValidateDocuments(ctx, []string{input.DocumentID}); err != nil {
			return err
		}
	}
	return s.references.ValidateDirectory(ctx, input.SiteID, input.DepartmentID)
}

func normalizePurchaseOrder(input CreatePurchaseOrderInput) (CreatePurchaseOrderInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Number = strings.TrimSpace(input.Number)
	input.VendorID = strings.TrimSpace(input.VendorID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "draft"
	}
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.AssetIDs = normalizeIDs(input.AssetIDs)
	input.ReceiptDocumentIDs = normalizeIDs(input.ReceiptDocumentIDs)
	if !optionalID(input.ID) || !validTextRange(input.Number, 1, 100) || !idPattern.MatchString(input.VendorID) || !contains(validPurchaseOrderStatuses, input.Status) || !currencyPattern.MatchString(input.Currency) || input.TotalMinor < 0 {
		return CreatePurchaseOrderInput{}, ErrInvalidInput
	}
	if !allIDs(input.AssetIDs) || !allIDs(input.ReceiptDocumentIDs) || hasDuplicates(input.AssetIDs) || hasDuplicates(input.ReceiptDocumentIDs) {
		return CreatePurchaseOrderInput{}, ErrInvalidInput
	}
	if input.OrderedOn != nil {
		date, ok := normalizeDate(*input.OrderedOn)
		if !ok {
			return CreatePurchaseOrderInput{}, ErrInvalidInput
		}
		input.OrderedOn = &date
	}
	if (input.Status == "ordered" || input.Status == "partially_received" || input.Status == "received") && input.OrderedOn == nil {
		return CreatePurchaseOrderInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeContract(input CreateContractInput) (CreateContractInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.VendorID = strings.TrimSpace(input.VendorID)
	input.OperationalStatus = strings.ToLower(strings.TrimSpace(input.OperationalStatus))
	input.FinancialStatus = strings.ToLower(strings.TrimSpace(input.FinancialStatus))
	if input.OperationalStatus == "" {
		input.OperationalStatus = "planned"
	}
	if input.FinancialStatus == "" {
		input.FinancialStatus = "planned"
	}
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.DocumentIDs = normalizeIDs(input.DocumentIDs)
	startsOn, startsOK := normalizeDate(input.StartsOn)
	endsOn, endsOK := normalizeDate(input.EndsOn)
	if !optionalID(input.ID) || !validTextRange(input.Name, 1, 200) || !idPattern.MatchString(input.VendorID) ||
		!contains(validOperationalStatuses, input.OperationalStatus) || !contains(validFinancialStatuses, input.FinancialStatus) ||
		!currencyPattern.MatchString(input.Currency) || input.CeilingMinor < 0 || !startsOK || !endsOK || endsOn.Before(startsOn) ||
		!allIDs(input.DocumentIDs) || hasDuplicates(input.DocumentIDs) {
		return CreateContractInput{}, ErrInvalidInput
	}
	input.StartsOn, input.EndsOn = startsOn, endsOn
	if input.RenewsOn != nil {
		renewsOn, ok := normalizeDate(*input.RenewsOn)
		if !ok || renewsOn.Before(startsOn) {
			return CreateContractInput{}, ErrInvalidInput
		}
		input.RenewsOn = &renewsOn
	}
	return input, nil
}

func normalizeCommitment(input CreateCommitmentInput) (CreateCommitmentInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ContractID = strings.TrimSpace(input.ContractID)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Description = strings.TrimSpace(input.Description)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.FiscalPeriod = strings.TrimSpace(input.FiscalPeriod)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	startsOn, startsOK := normalizeDate(input.StartsOn)
	endsOn, endsOK := normalizeDate(input.EndsOn)
	if !optionalID(input.ID) || !idPattern.MatchString(input.ContractID) || !contains(validCommitmentKinds, input.Kind) ||
		!validTextRange(input.Description, 1, 500) || !currencyPattern.MatchString(input.Currency) || input.AmountMinor < 0 ||
		!startsOK || !endsOK || endsOn.Before(startsOn) || !fiscalPeriodPattern.MatchString(input.FiscalPeriod) || !scenarioPattern.MatchString(input.Scenario) {
		return CreateCommitmentInput{}, ErrInvalidInput
	}
	input.StartsOn, input.EndsOn = startsOn, endsOn
	return input, nil
}

func normalizeBudget(input CreateBudgetInput) (CreateBudgetInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.FiscalPeriod = strings.TrimSpace(input.FiscalPeriod)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if !optionalID(input.ID) || !validTextRange(input.Name, 1, 200) || !fiscalPeriodPattern.MatchString(input.FiscalPeriod) ||
		!scenarioPattern.MatchString(input.Scenario) || !optionalID(input.DepartmentID) || !optionalID(input.SiteID) ||
		!currencyPattern.MatchString(input.Currency) || input.AllocatedMinor < 0 {
		return CreateBudgetInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeCost(input ReconcileCostInput) (ReconcileCostInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Description = strings.TrimSpace(input.Description)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.FiscalPeriod = strings.TrimSpace(input.FiscalPeriod)
	input.Scenario = strings.ToLower(strings.TrimSpace(input.Scenario))
	input.PurchaseOrderID = strings.TrimSpace(input.PurchaseOrderID)
	input.ContractID = strings.TrimSpace(input.ContractID)
	input.AssetID = strings.TrimSpace(input.AssetID)
	input.DepartmentID = strings.TrimSpace(input.DepartmentID)
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.DocumentID = strings.TrimSpace(input.DocumentID)
	input.ExternalReference = strings.TrimSpace(input.ExternalReference)
	input.SourceSystemID = strings.ToLower(strings.TrimSpace(input.SourceSystemID))
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if !optionalID(input.ID) || !validTextRange(input.Description, 1, 500) || !contains(validCostKinds, input.Kind) ||
		!currencyPattern.MatchString(input.Currency) || input.AmountMinor < 0 || !fiscalPeriodPattern.MatchString(input.FiscalPeriod) ||
		!scenarioPattern.MatchString(input.Scenario) || !allOptionalIDs(input.PurchaseOrderID, input.ContractID, input.AssetID, input.DepartmentID, input.SiteID, input.DocumentID) ||
		!validText(input.ExternalReference, 255) || !validText(input.SourceSystemID, 128) || !validText(input.SourceRecordID, 255) ||
		((input.SourceSystemID == "") != (input.SourceRecordID == "")) {
		return ReconcileCostInput{}, ErrInvalidInput
	}
	return input, nil
}

func costFromInput(input ReconcileCostInput, id, organizationID string, createdAt, updatedAt time.Time, revision int64) CostRecord {
	return CostRecord{
		ID: id, OrganizationID: organizationID, Description: input.Description, Kind: input.Kind,
		Currency: input.Currency, AmountMinor: input.AmountMinor, FiscalPeriod: input.FiscalPeriod, Scenario: input.Scenario,
		PurchaseOrderID: input.PurchaseOrderID, ContractID: input.ContractID, AssetID: input.AssetID,
		DepartmentID: input.DepartmentID, SiteID: input.SiteID, DocumentID: input.DocumentID,
		ExternalReference: input.ExternalReference, SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
		Revision: revision, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func sameCost(left, right CostRecord) bool {
	return left.Description == right.Description && left.Kind == right.Kind && left.Currency == right.Currency &&
		left.AmountMinor == right.AmountMinor && left.FiscalPeriod == right.FiscalPeriod && left.Scenario == right.Scenario &&
		left.PurchaseOrderID == right.PurchaseOrderID && left.ContractID == right.ContractID && left.AssetID == right.AssetID &&
		left.DepartmentID == right.DepartmentID && left.SiteID == right.SiteID && left.DocumentID == right.DocumentID &&
		left.ExternalReference == right.ExternalReference && left.SourceSystemID == right.SourceSystemID && left.SourceRecordID == right.SourceRecordID
}

func purchaseOrderTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{
		"draft":              {"approved": true, "cancelled": true},
		"approved":           {"ordered": true, "cancelled": true},
		"ordered":            {"partially_received": true, "received": true, "cancelled": true},
		"partially_received": {"received": true, "cancelled": true},
	}
	return allowed[from][to]
}

func contractOperationalTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{
		"planned":   {"active": true, "cancelled": true},
		"active":    {"suspended": true, "expired": true, "terminated": true},
		"suspended": {"active": true, "expired": true, "terminated": true},
		"expired":   {"active": true},
	}
	return allowed[from][to]
}

func contractFinancialTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{
		"planned":   {"committed": true, "cancelled": true},
		"committed": {"billed": true, "paid": true, "cancelled": true},
		"billed":    {"paid": true, "closed": true},
		"paid":      {"closed": true},
	}
	return allowed[from][to]
}

func values(items ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result
}

func contains(set map[string]struct{}, value string) bool { _, ok := set[value]; return ok }

func (s *Service) id(value string) (string, error) {
	if value != "" {
		return value, nil
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return "", fmt.Errorf("create Ledger id: %w", err)
	}
	return id, nil
}

func optionalID(value string) bool { return value == "" || idPattern.MatchString(value) }
func allOptionalIDs(values ...string) bool {
	for _, value := range values {
		if !optionalID(value) {
			return false
		}
	}
	return true
}
func allIDs(values []string) bool {
	for _, value := range values {
		if !idPattern.MatchString(value) {
			return false
		}
	}
	return true
}
func normalizeIDs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
func hasDuplicates(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
func validText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}
func validTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}
func normalizeDate(value time.Time) (time.Time, bool) {
	value = value.UTC()
	if value.IsZero() || value.Year() < 1970 || value.Year() > 9999 {
		return time.Time{}, false
	}
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC), true
}
func cloneDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	default:
		return value
	}
}
func addMinor(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}
func mapReferenceError(err error) error {
	if errors.Is(err, ErrNotFound) {
		return ErrReferenceMissing
	}
	return err
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:ledger"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["requirementId"] = RequirementID
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata,
	})
}
