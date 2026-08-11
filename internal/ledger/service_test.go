package ledger_test

// Requirement: REQ-LEDGER-001. Feature: procurement.finance.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type ledgerReferences struct {
	missingAsset, missingDocument, missingDirectory bool
}

func (r *ledgerReferences) ValidateAssets(context.Context, []string) error {
	if r.missingAsset {
		return ledger.ErrReferenceMissing
	}
	return nil
}
func (r *ledgerReferences) ValidateDocuments(context.Context, []string) error {
	if r.missingDocument {
		return ledger.ErrReferenceMissing
	}
	return nil
}
func (r *ledgerReferences) ValidateDirectory(context.Context, string, string) error {
	if r.missingDirectory {
		return ledger.ErrReferenceMissing
	}
	return nil
}

func newLedgerService(t *testing.T, references *ledgerReferences) *ledger.Service {
	t.Helper()
	service, err := ledger.NewService(repository.NewMemoryLedgerStore(), references, foundation.NopAuditor{}, ledger.ServiceConfig{
		OrganizationID: "example-org", Now: func() time.Time { return time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestPurchaseOrdersPreserveAssetsDocumentsAndEnforceTransitions(t *testing.T) {
	refs := &ledgerReferences{}
	service := newLedgerService(t, refs)
	vendor, err := service.CreateVendor(context.Background(), ledger.CreateVendorInput{ID: "vendor-1", Name: "Example Vendor"})
	if err != nil {
		t.Fatal(err)
	}
	orderedOn := time.Date(2026, time.August, 10, 14, 35, 0, 0, time.FixedZone("offset", -5*60*60))
	created, err := service.CreatePurchaseOrder(context.Background(), ledger.CreatePurchaseOrderInput{
		ID: "po-1", Number: "PO-2026-001", VendorID: vendor.ID, Status: "ordered", Currency: "usd", TotalMinor: 12345,
		OrderedOn: &orderedOn, AssetIDs: []string{"asset-2", "asset-1"}, ReceiptDocumentIDs: []string{"doc-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Currency != "USD" || created.TotalMinor != 12345 || len(created.AssetIDs) != 2 || created.AssetIDs[0] != "asset-1" ||
		created.OrderedOn == nil || created.OrderedOn.Hour() != 0 || len(created.ReceiptDocumentIDs) != 1 {
		t.Fatalf("unexpected purchase order %#v", created)
	}
	if _, err := service.UpdatePurchaseOrderStatus(context.Background(), ledger.UpdatePurchaseOrderStatusInput{ID: created.ID, Status: "approved", Revision: 1}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Fatalf("expected backward transition rejection, got %v", err)
	}
	updated, err := service.UpdatePurchaseOrderStatus(context.Background(), ledger.UpdatePurchaseOrderStatusInput{ID: created.ID, Status: "received", Revision: 1})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("unexpected received purchase order %#v err=%v", updated, err)
	}
	if _, err := service.UpdatePurchaseOrderStatus(context.Background(), ledger.UpdatePurchaseOrderStatusInput{ID: created.ID, Status: "cancelled", Revision: 1}); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected stale revision conflict, got %v", err)
	}
	refs.missingDocument = true
	if _, err := service.CreatePurchaseOrder(context.Background(), ledger.CreatePurchaseOrderInput{Number: "PO-2", VendorID: vendor.ID, Currency: "USD", ReceiptDocumentIDs: []string{"missing"}}); !errors.Is(err, ledger.ErrReferenceMissing) {
		t.Fatalf("expected missing document rejection, got %v", err)
	}
}

func TestContractsCommitmentsAndDateValidation(t *testing.T) {
	service := newLedgerService(t, &ledgerReferences{})
	vendor, _ := service.CreateVendor(context.Background(), ledger.CreateVendorInput{ID: "vendor-1", Name: "Vendor"})
	startsOn := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	endsOn := startsOn.AddDate(3, 0, 0)
	contract, err := service.CreateContract(context.Background(), ledger.CreateContractInput{
		ID: "contract-1", Name: "Cloud commitment", VendorID: vendor.ID, Currency: "USD", CeilingMinor: 900000,
		StartsOn: startsOn, EndsOn: endsOn, DocumentIDs: []string{"contract-document"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contract.OperationalStatus != "planned" || contract.FinancialStatus != "planned" {
		t.Fatalf("unexpected defaults %#v", contract)
	}
	active, err := service.UpdateContractStatus(context.Background(), ledger.UpdateContractStatusInput{
		ID: contract.ID, OperationalStatus: "active", FinancialStatus: "committed", Revision: contract.Revision,
	})
	if err != nil || active.Revision != 2 {
		t.Fatalf("unexpected contract transition %#v err=%v", active, err)
	}
	commitment, err := service.CreateCommitment(context.Background(), ledger.CreateCommitmentInput{
		ContractID: contract.ID, Kind: "savings_plan", Description: "Three-year capacity plan", Currency: "USD",
		AmountMinor: 300000, StartsOn: startsOn, EndsOn: endsOn, FiscalPeriod: "FY2027", Scenario: "Baseline",
	})
	if err != nil || commitment.Scenario != "baseline" || commitment.EndsOn.Year()-commitment.StartsOn.Year() != 3 {
		t.Fatalf("unexpected commitment %#v err=%v", commitment, err)
	}
	if _, err := service.CreateContract(context.Background(), ledger.CreateContractInput{
		Name: "Invalid dates", VendorID: vendor.ID, Currency: "USD", StartsOn: endsOn, EndsOn: startsOn,
	}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Fatalf("expected invalid contract range, got %v", err)
	}
}

func TestCostReconciliationVarianceAndCSVExport(t *testing.T) {
	service := newLedgerService(t, &ledgerReferences{})
	_, err := service.CreateBudget(context.Background(), ledger.CreateBudgetInput{
		ID: "budget-1", Name: "Infrastructure", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AllocatedMinor: 200000,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := ledger.ReconcileCostInput{
		Description: "Cloud invoice", Kind: "billed", Currency: "USD", AmountMinor: 125000, FiscalPeriod: "FY2027",
		Scenario: "baseline", SourceSystemID: "ERP", SourceRecordID: "invoice-42", ExternalReference: "INV-42",
	}
	first, err := service.ReconcileCost(context.Background(), input)
	if err != nil || !first.Created || !first.Applied || first.Record.Revision != 1 {
		t.Fatalf("unexpected first reconciliation %#v err=%v", first, err)
	}
	second, err := service.ReconcileCost(context.Background(), input)
	if err != nil || second.Applied || second.Created || second.Record.ID != first.Record.ID {
		t.Fatalf("expected idempotent reconciliation %#v err=%v", second, err)
	}
	input.Kind, input.AmountMinor = "paid", 130000
	third, err := service.ReconcileCost(context.Background(), input)
	if err != nil || !third.Applied || third.Created || third.Record.Revision != 2 || third.Record.Kind != "paid" {
		t.Fatalf("unexpected updated reconciliation %#v err=%v", third, err)
	}
	report, err := service.BudgetVariance(context.Background(), "FY2027", "baseline")
	if err != nil || report.AllocatedMinor != 200000 || report.RecognizedMinor != 130000 || report.VarianceMinor != 70000 || report.OverBudget {
		t.Fatalf("unexpected variance %#v err=%v", report, err)
	}
	csvContent, err := service.ExportCSV(context.Background(), "FY2027", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	text := string(csvContent)
	if !strings.Contains(text, "record_type") || !strings.Contains(text, "Infrastructure") || !strings.Contains(text, "Cloud invoice") || !strings.Contains(text, "130000") {
		t.Fatalf("unexpected CSV %q", text)
	}
}

func TestBudgetVarianceRejectsMixedCurrencies(t *testing.T) {
	service := newLedgerService(t, &ledgerReferences{})
	for _, input := range []ledger.CreateBudgetInput{
		{Name: "US budget", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AllocatedMinor: 100},
		{Name: "EU budget", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "EUR", AllocatedMinor: 100},
	} {
		if _, err := service.CreateBudget(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.BudgetVariance(context.Background(), "FY2027", "baseline"); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected mixed-currency conflict, got %v", err)
	}
}

func TestCSVExportNeutralizesSpreadsheetFormulaCells(t *testing.T) {
	service := newLedgerService(t, &ledgerReferences{})
	if _, err := service.CreateBudget(context.Background(), ledger.CreateBudgetInput{
		Name: "=HYPERLINK(\"https://example.test\")", FiscalPeriod: "FY2027", Scenario: "baseline", Currency: "USD", AllocatedMinor: 100,
	}); err != nil {
		t.Fatal(err)
	}
	content, err := service.ExportCSV(context.Background(), "FY2027", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `'=HYPERLINK`) {
		t.Fatalf("expected formula-safe CSV cell, got %q", content)
	}
}

func TestConcurrentReconciliationReturnsOneCurrentSourceRecord(t *testing.T) {
	service := newLedgerService(t, &ledgerReferences{})
	input := ledger.ReconcileCostInput{
		Description: "Concurrent invoice", Kind: "billed", Currency: "USD", AmountMinor: 100,
		FiscalPeriod: "FY2027", Scenario: "baseline", SourceSystemID: "ERP", SourceRecordID: "invoice-concurrent",
	}
	const workers = 12
	results := make(chan ledger.ReconcileResult, workers)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.ReconcileCost(context.Background(), input)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("unexpected reconciliation error: %v", err)
	}
	ids := make(map[string]struct{})
	created := 0
	for result := range results {
		ids[result.Record.ID] = struct{}{}
		if result.Created {
			created++
		}
	}
	if len(ids) != 1 || created != 1 {
		t.Fatalf("expected one created source record, ids=%v created=%d", ids, created)
	}
}
