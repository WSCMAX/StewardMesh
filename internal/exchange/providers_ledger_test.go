package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-LEDGER-001, REQ-PATTERNS-001. Features: migration.packages, procurement.finance, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type allowLedgerReferences struct{}

func (allowLedgerReferences) ValidateAssets(context.Context, []string) error          { return nil }
func (allowLedgerReferences) ValidateDocuments(context.Context, []string) error       { return nil }
func (allowLedgerReferences) ValidateDirectory(context.Context, string, string) error { return nil }

func newLedgerProvider(t *testing.T, organizationID string) (*exchange.LedgerProvider, ledger.ExchangeImporter) {
	t.Helper()
	service, importer, err := ledger.NewServiceWithExchangeImporter(
		repository.NewMemoryLedgerStore(), allowLedgerReferences{}, nil, foundation.NopAuditor{}, ledger.ServiceConfig{OrganizationID: organizationID},
	)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := exchange.NewLedgerProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	return provider, importer
}

func TestLedgerProviderRoundTripIsStrictLosslessAndDependencyAware(t *testing.T) {
	ctx := context.Background()
	source, importer := newLedgerProvider(t, "ledger-provider-source")
	createdAt := time.Date(2022, time.January, 2, 3, 4, 5, 600_000, time.UTC)
	updatedAt := time.Date(2025, time.June, 7, 8, 9, 10, 700_000, time.UTC)
	date := time.Date(2024, time.March, 4, 0, 0, 0, 0, time.UTC)
	end := time.Date(2029, time.March, 4, 0, 0, 0, 0, time.UTC)
	operation := ledger.ExchangeImportOperation{Token: "ledger-provider-source-import", OccurredAt: time.Date(2026, time.August, 13, 19, 0, 0, 0, time.UTC)}
	vendor := ledger.Vendor{ID: "vendor-roundtrip", Name: "Roundtrip Vendor", ExternalID: "vendor/7", Status: "active", Revision: 16, CreatedAt: createdAt, UpdatedAt: updatedAt}
	purchase := ledger.PurchaseOrder{ID: "purchase-roundtrip", Number: "PO-7", VendorID: vendor.ID, Status: "ordered", Currency: "USD", TotalMinor: 20_000,
		OrderedOn: &date, AssetIDs: []string{"asset-one"}, ReceiptDocumentIDs: []string{"document-one"}, Revision: 15, CreatedAt: createdAt, UpdatedAt: updatedAt}
	contract := ledger.Contract{ID: "contract-roundtrip", Name: "Roundtrip Contract", VendorID: vendor.ID, OperationalStatus: "active", FinancialStatus: "committed",
		Currency: "USD", CeilingMinor: 200_000, StartsOn: date, EndsOn: end, DocumentIDs: []string{"document-two"}, Revision: 14, CreatedAt: createdAt, UpdatedAt: updatedAt}
	commitment := ledger.Commitment{ID: "commitment-roundtrip", ContractID: contract.ID, Kind: "subscription", Description: "Roundtrip commitment", Currency: "USD",
		AmountMinor: 10_000, StartsOn: date, EndsOn: end, FiscalPeriod: "FY2028", Scenario: "baseline", Revision: 13, CreatedAt: createdAt, UpdatedAt: updatedAt}
	budget := ledger.Budget{ID: "budget-roundtrip", Name: "Roundtrip budget", FiscalPeriod: "FY2028", Scenario: "baseline", DepartmentID: "department-one",
		SiteID: "site-one", Currency: "USD", AllocatedMinor: 250_000, Revision: 12, CreatedAt: createdAt, UpdatedAt: updatedAt}
	cost := ledger.CostRecord{ID: "cost-roundtrip", Description: "Roundtrip invoice", Kind: "actual", Currency: "USD", AmountMinor: 19_500,
		FiscalPeriod: "FY2028", Scenario: "baseline", PurchaseOrderID: purchase.ID, ContractID: contract.ID, AssetID: "asset-one",
		DepartmentID: "department-one", SiteID: "site-one", DocumentID: "document-three", ExternalReference: "INV-7",
		SourceSystemID: "legacy-erp", SourceRecordID: "invoice/7", Revision: 11, CreatedAt: createdAt, UpdatedAt: updatedAt}
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
			t.Fatalf("seed Ledger provider: %#v err=%v", result, err)
		}
	}
	records, err := source.ListRecords(ctx)
	if err != nil || len(records) != 6 {
		t.Fatalf("list source Ledger records: count=%d err=%v", len(records), err)
	}
	target, _ := newLedgerProvider(t, "ledger-provider-target")
	providerOperation := exchange.ProviderImportOperation{Token: "ledger-provider-target-import", OccurredAt: operation.OccurredAt.Add(time.Hour), ExpectedCreated: true}
	for _, recordType := range []string{"ledger.vendor", "ledger.purchase-order", "ledger.contract", "ledger.commitment", "ledger.budget", "ledger.cost"} {
		index := slices.IndexFunc(records, func(record exchange.Record) bool { return record.Type == recordType })
		if index < 0 {
			t.Fatalf("source provider omitted %s", recordType)
		}
		result, err := target.ImportRecord(ctx, providerOperation, "source-system", records[index], nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s: %#v err=%v", recordType, result, err)
		}
	}
	targetRecords, err := target.ListRecords(ctx)
	if err != nil || len(targetRecords) != len(records) {
		t.Fatalf("list target Ledger records: count=%d err=%v", len(targetRecords), err)
	}
	for index := range records {
		left, right := records[index], targetRecords[index]
		if left.Type != right.Type || left.ID != right.ID || left.Revision != right.Revision || !slices.Equal(left.Dependencies, right.Dependencies) || !bytes.Equal(left.Payload, right.Payload) {
			t.Fatalf("Ledger provider roundtrip changed record\nsource=%#v\ntarget=%#v", left, right)
		}
		exact, err := target.ImportRecordExists(ctx, right, nil)
		if err != nil || !exact {
			t.Fatalf("exact target record was not idempotent: %s:%s exact=%t err=%v", right.Type, right.ID, exact, err)
		}
	}
	unknown := records[0]
	unknown.Payload = bytes.Replace(unknown.Payload, []byte(`{"`), []byte(`{"unexpected":true,"`), 1)
	if _, err := target.ImportRecordExists(ctx, unknown, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("strict Ledger DTO accepted unknown field: %v", err)
	}
	noncanonical := records[0]
	noncanonical.Payload = append([]byte(" "), noncanonical.Payload...)
	if _, err := target.ImportRecordExists(ctx, noncanonical, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("strict Ledger DTO accepted noncanonical top-level JSON: %v", err)
	}
	subMicrosecond := records[0]
	subMicrosecond.Payload = bytes.Replace(subMicrosecond.Payload, []byte(`"createdAt":"2022-01-02T03:04:05.0006Z"`), []byte(`"createdAt":"2022-01-02T03:04:05.000600001Z"`), 1)
	if _, err := target.ImportRecordExists(ctx, subMicrosecond, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Ledger provider accepted a sub-microsecond timestamp: %v", err)
	}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"invalid id":    func(record exchange.Record) exchange.Record { record.ID = "invalid id"; return record },
		"zero revision": func(record exchange.Record) exchange.Record { record.Revision = 0; return record },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := target.ImportRecordExists(ctx, mutate(records[0]), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("strict Ledger DTO accepted invalid identity/revision: %v", err)
			}
		})
	}
	revisionDrift := records[0]
	revisionDrift.Revision = 1
	if _, err := target.ImportRecord(ctx, providerOperation, "source-system", revisionDrift, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Ledger provider accepted revision-one timestamp drift: %v", err)
	}
	purchaseIndex := slices.IndexFunc(records, func(record exchange.Record) bool { return record.Type == "ledger.purchase-order" })
	badDependencies := records[purchaseIndex]
	badDependencies.Dependencies = []exchange.Reference{{Type: "ledger.vendor", ID: vendor.ID}}
	if _, err := target.ImportRecord(ctx, providerOperation, "source-system", badDependencies, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("Ledger provider accepted incomplete typed dependencies: %v", err)
	}
}

func TestLedgerProviderRejectsForeignOrMismatchedImporter(t *testing.T) {
	provider, importer := newLedgerProvider(t, "ledger-provider-owner")
	if provider == nil || importer == nil {
		t.Fatal("expected constructed Ledger provider")
	}
	otherService, _, err := ledger.NewServiceWithExchangeImporter(repository.NewMemoryLedgerStore(), allowLedgerReferences{}, nil, foundation.NopAuditor{}, ledger.ServiceConfig{OrganizationID: "ledger-provider-other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewLedgerProvider(otherService, importer); err == nil {
		t.Fatal("Ledger provider accepted an importer owned by another service")
	}
}
