package postgres

// Requirements: REQ-EXCHANGE-001, REQ-LEDGER-001. Features: migration.packages, procurement.finance. GitHub: #9.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/ledger"
)

type ledgerExchangeReferences struct{}

func (ledgerExchangeReferences) ValidateAssets(context.Context, []string) error    { return nil }
func (ledgerExchangeReferences) ValidateDocuments(context.Context, []string) error { return nil }
func (ledgerExchangeReferences) ValidateDirectory(context.Context, string, string) error {
	return nil
}

func TestLedgerExchangeImporterPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("ledger-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Ledger Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewLedgerStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := ledger.NewServiceWithExchangeImporter(store, ledgerExchangeReferences{}, nil, auditor, ledger.ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	operation := ledger.ExchangeImportOperation{Token: "ledger-postgres-import", OccurredAt: time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC)}
	createdAt := time.Date(2021, time.January, 2, 3, 4, 5, 0, time.UTC)
	updatedAt := time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC)
	date := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2029, time.January, 1, 0, 0, 0, 0, time.UTC)
	vendor := ledger.Vendor{ID: "vendor-postgres", Name: "Postgres Vendor", Status: "active", Revision: 21, CreatedAt: createdAt, UpdatedAt: updatedAt}
	purchase := ledger.PurchaseOrder{ID: "purchase-postgres", Number: "PO-PG", VendorID: vendor.ID, Status: "ordered", Currency: "USD", TotalMinor: 50_000,
		OrderedOn: &date, AssetIDs: []string{"asset-pg"}, ReceiptDocumentIDs: []string{"document-pg"}, Revision: 20, CreatedAt: createdAt, UpdatedAt: updatedAt}
	contract := ledger.Contract{ID: "contract-postgres", Name: "Postgres Contract", VendorID: vendor.ID, OperationalStatus: "active", FinancialStatus: "committed",
		Currency: "USD", CeilingMinor: 500_000, StartsOn: date, EndsOn: end, DocumentIDs: []string{"document-pg"}, Revision: 19, CreatedAt: createdAt, UpdatedAt: updatedAt}
	commitment := ledger.Commitment{ID: "commitment-postgres", ContractID: contract.ID, Kind: "subscription", Description: "Postgres commitment",
		Currency: "USD", AmountMinor: 25_000, StartsOn: date, EndsOn: end, FiscalPeriod: "FY2028", Scenario: "baseline", Revision: 18, CreatedAt: createdAt, UpdatedAt: updatedAt}
	budget := ledger.Budget{ID: "budget-postgres", Name: "Postgres budget", FiscalPeriod: "FY2028", Scenario: "baseline", Currency: "USD",
		AllocatedMinor: 600_000, Revision: 17, CreatedAt: createdAt, UpdatedAt: updatedAt}
	cost := ledger.CostRecord{ID: "cost-postgres", Description: "Postgres invoice", Kind: "actual", Currency: "USD", AmountMinor: 49_000,
		FiscalPeriod: "FY2028", Scenario: "baseline", PurchaseOrderID: purchase.ID, ContractID: contract.ID, AssetID: "asset-pg",
		DocumentID: "document-pg", SourceSystemID: "postgres-erp", SourceRecordID: "invoice/pg", Revision: 16, CreatedAt: createdAt, UpdatedAt: updatedAt}
	imports := []func() (ledger.ExchangeImportResult, error){
		func() (ledger.ExchangeImportResult, error) { return importer.ImportVendor(ctx, operation, vendor) },
		func() (ledger.ExchangeImportResult, error) {
			return importer.ImportPurchaseOrder(ctx, operation, purchase)
		},
		func() (ledger.ExchangeImportResult, error) { return importer.ImportContract(ctx, operation, contract) },
		func() (ledger.ExchangeImportResult, error) {
			return importer.ImportCommitment(ctx, operation, commitment)
		},
		func() (ledger.ExchangeImportResult, error) { return importer.ImportBudget(ctx, operation, budget) },
		func() (ledger.ExchangeImportResult, error) { return importer.ImportCost(ctx, operation, cost) },
	}
	for _, importRecord := range imports {
		result, err := importRecord()
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import Ledger PostgreSQL record: %#v err=%v", result, err)
		}
	}
	snapshot, err := service.ExchangeSnapshot(ctx, 6)
	if err != nil || len(snapshot.Vendors) != 1 || len(snapshot.PurchaseOrders) != 1 || len(snapshot.Contracts) != 1 || len(snapshot.Commitments) != 1 || len(snapshot.Budgets) != 1 || len(snapshot.Costs) != 1 {
		t.Fatalf("unexpected repeatable-read Ledger snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Vendors[0].Revision != vendor.Revision || !snapshot.Vendors[0].CreatedAt.Equal(vendor.CreatedAt) ||
		snapshot.Costs[0].Revision != cost.Revision || snapshot.Costs[0].SourceSystemID != cost.SourceSystemID || snapshot.Costs[0].SourceRecordID != cost.SourceRecordID {
		t.Fatalf("PostgreSQL Ledger import was not lossless: %#v", snapshot)
	}
	if _, err := service.ExchangeSnapshot(ctx, 5); !errors.Is(err, ledger.ErrTooLarge) {
		t.Fatalf("expected bounded Ledger Exchange snapshot rejection, got %v", err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 {
		t.Fatalf("expected six deterministic Ledger import audits, got %d", auditCount)
	}
	replay, err := importer.ImportVendor(ctx, operation, vendor)
	if err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Ledger import: %#v err=%v", replay, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 {
		t.Fatalf("Ledger audit replay duplicated rows: %d", auditCount)
	}
}
