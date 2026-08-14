package ledger_test

// Requirements: REQ-LEDGER-001, REQ-EXCHANGE-001. Features: procurement.finance, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type ledgerExchangeAuditor struct {
	events   map[string]foundation.AuditEvent
	attempts []foundation.AuditEvent
	failNext error
}

func (a *ledgerExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.attempts = append(a.attempts, event)
	if a.failNext != nil {
		err := a.failNext
		a.failNext = nil
		return err
	}
	if a.events == nil {
		a.events = make(map[string]foundation.AuditEvent)
	}
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("audit event id conflicts with different immutable content")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

type ledgerWriteGate struct {
	err      error
	requests [][2]string
}

func (g *ledgerWriteGate) CheckResourceWrite(_ context.Context, recordType, recordID string) error {
	g.requests = append(g.requests, [2]string{recordType, recordID})
	return g.err
}

func newLedgerExchangeService(t *testing.T, organizationID string, auditor foundation.Auditor, gate ledger.WriteGate) (*ledger.Service, ledger.ExchangeImporter, *repository.MemoryLedgerStore) {
	t.Helper()
	store := repository.NewMemoryLedgerStore()
	service, importer, err := ledger.NewServiceWithExchangeImporter(store, &ledgerReferences{}, gate, auditor, ledger.ServiceConfig{
		OrganizationID: organizationID, Now: func() time.Time { return time.Date(2026, time.August, 13, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, store
}

func TestExchangeImporterPreservesAllLedgerRecordTypesAndReplaysAudits(t *testing.T) {
	auditor := &ledgerExchangeAuditor{}
	service, importer, _ := newLedgerExchangeService(t, "ledger-target", auditor, nil)
	operation := ledger.ExchangeImportOperation{Token: "ledger-import-operation", OccurredAt: time.Date(2026, time.August, 13, 19, 0, 0, 0, time.UTC)}
	createdAt := time.Date(2022, time.February, 3, 4, 5, 6, 700_000, time.UTC)
	updatedAt := time.Date(2025, time.March, 4, 5, 6, 7, 800_000, time.UTC)
	orderedOn := time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC)
	startsOn := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	endsOn := time.Date(2028, time.December, 31, 0, 0, 0, 0, time.UTC)
	renewsOn := time.Date(2028, time.June, 30, 0, 0, 0, 0, time.UTC)

	vendor := ledger.Vendor{ID: "vendor-portable", Name: "Portable Vendor", ExternalID: "vendor/42", Status: "active", Revision: 9, CreatedAt: createdAt, UpdatedAt: updatedAt}
	purchase := ledger.PurchaseOrder{ID: "purchase-portable", Number: "PO-0042", VendorID: vendor.ID, Status: "received", Currency: "USD", TotalMinor: 75_000,
		OrderedOn: &orderedOn, AssetIDs: []string{"asset-1", "asset-2"}, ReceiptDocumentIDs: []string{"document-1"}, Revision: 8, CreatedAt: createdAt, UpdatedAt: updatedAt}
	contract := ledger.Contract{ID: "contract-portable", Name: "Portable contract", VendorID: vendor.ID, OperationalStatus: "active", FinancialStatus: "paid",
		Currency: "USD", CeilingMinor: 300_000, StartsOn: startsOn, EndsOn: endsOn, RenewsOn: &renewsOn, DocumentIDs: []string{"document-2"}, Revision: 7, CreatedAt: createdAt, UpdatedAt: updatedAt}
	commitment := ledger.Commitment{ID: "commitment-portable", ContractID: contract.ID, Kind: "subscription", Description: "Portable commitment", Currency: "USD",
		AmountMinor: 55_000, StartsOn: startsOn, EndsOn: endsOn, FiscalPeriod: "FY2027", Scenario: "baseline", Revision: 6, CreatedAt: createdAt, UpdatedAt: updatedAt}
	budget := ledger.Budget{ID: "budget-portable", Name: "Portable budget", FiscalPeriod: "FY2027", Scenario: "baseline", DepartmentID: "department-1", SiteID: "site-1",
		Currency: "USD", AllocatedMinor: 500_000, Revision: 5, CreatedAt: createdAt, UpdatedAt: updatedAt}
	cost := ledger.CostRecord{ID: "cost-portable", Description: "Portable cost", Kind: "actual", Currency: "USD", AmountMinor: 42_000,
		FiscalPeriod: "FY2027", Scenario: "baseline", PurchaseOrderID: purchase.ID, ContractID: contract.ID, AssetID: "asset-1",
		DepartmentID: "department-1", SiteID: "site-1", DocumentID: "document-3", ExternalReference: "INV-42",
		SourceSystemID: "legacy-erp", SourceRecordID: "invoice/42", Revision: 4, CreatedAt: createdAt, UpdatedAt: updatedAt}

	tests := []struct {
		name         string
		importRecord func() (ledger.ExchangeImportResult, error)
		get          func() (any, error)
		want         any
	}{
		{"vendor", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportVendor(context.Background(), operation, vendor)
		}, func() (any, error) { return service.GetVendor(context.Background(), vendor.ID) }, vendor},
		{"purchase order", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportPurchaseOrder(context.Background(), operation, purchase)
		}, func() (any, error) { return service.GetPurchaseOrder(context.Background(), purchase.ID) }, purchase},
		{"contract", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportContract(context.Background(), operation, contract)
		}, func() (any, error) { return service.GetContract(context.Background(), contract.ID) }, contract},
		{"commitment", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportCommitment(context.Background(), operation, commitment)
		}, func() (any, error) { return service.GetCommitment(context.Background(), commitment.ID) }, commitment},
		{"budget", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportBudget(context.Background(), operation, budget)
		}, func() (any, error) { return service.GetBudget(context.Background(), budget.ID) }, budget},
		{"cost", func() (ledger.ExchangeImportResult, error) {
			return importer.ImportCost(context.Background(), operation, cost)
		}, func() (any, error) { return service.GetCost(context.Background(), cost.ID) }, cost},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.importRecord()
			if err != nil || !result.Committed || !result.Created {
				t.Fatalf("first import: %#v err=%v", result, err)
			}
			stored, err := test.get()
			if err != nil {
				t.Fatal(err)
			}
			want := withLedgerOrganization(test.want, "ledger-target")
			if !reflect.DeepEqual(stored, want) {
				t.Fatalf("Ledger import was not lossless\n got: %#v\nwant: %#v", stored, want)
			}
			replay, err := test.importRecord()
			if err != nil || !replay.Committed || replay.Created {
				t.Fatalf("exact replay: %#v err=%v", replay, err)
			}
		})
	}
	if len(auditor.events) != len(tests) {
		t.Fatalf("expected one deterministic audit per imported record, got %d", len(auditor.events))
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "ledger-target" || event.ActorID != "system:exchange" || event.CorrelationID != operation.Token || !event.OccurredAt.Equal(operation.OccurredAt) {
			t.Fatalf("unexpected imported Ledger audit %#v", event)
		}
	}
}

func TestExchangeImporterReportsCommittedAuditFailureAndRepairsOrganizationScopedEvent(t *testing.T) {
	auditFailure := errors.New("audit temporarily unavailable")
	auditor := &ledgerExchangeAuditor{failNext: auditFailure}
	_, importer, store := newLedgerExchangeService(t, "ledger-audit-org", auditor, nil)
	operation := ledger.ExchangeImportOperation{Token: "ledger-audit-repair", OccurredAt: time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)}
	candidate := ledger.Vendor{ID: "vendor-audit", Name: "Audit Vendor", Status: "active", Revision: 11,
		CreatedAt: time.Date(2021, time.January, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)}
	result, err := importer.ImportVendor(context.Background(), operation, candidate)
	if !errors.Is(err, auditFailure) || !result.Committed || !result.Created {
		t.Fatalf("expected truthful post-commit failure: %#v err=%v", result, err)
	}
	if _, err := store.GetVendor(context.Background(), "ledger-audit-org", candidate.ID); err != nil {
		t.Fatalf("committed Ledger write was lost: %v", err)
	}
	repaired, err := importer.ImportVendor(context.Background(), operation, candidate)
	if err != nil || !repaired.Committed || repaired.Created || len(auditor.events) != 1 || len(auditor.attempts) != 2 || auditor.attempts[0].ID != auditor.attempts[1].ID {
		t.Fatalf("audit repair did not converge: %#v attempts=%#v err=%v", repaired, auditor.attempts, err)
	}
	otherAuditor := &ledgerExchangeAuditor{}
	_, otherImporter, _ := newLedgerExchangeService(t, "ledger-other-org", otherAuditor, nil)
	if _, err := otherImporter.ImportVendor(context.Background(), operation, candidate); err != nil {
		t.Fatal(err)
	}
	if auditor.attempts[1].ID == otherAuditor.attempts[0].ID {
		t.Fatalf("organization-scoped imports reused audit id %q", auditor.attempts[1].ID)
	}
}

func TestExchangeImporterRejectsRevisionOneTimestampDrift(t *testing.T) {
	createdAt := time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
	service, importer, _ := newLedgerExchangeService(t, "ledger-revision-org", foundation.NopAuditor{}, nil)
	candidate := ledger.Vendor{ID: "vendor-revision-one", Name: "Revision Vendor", Status: "active", Revision: 1,
		CreatedAt: createdAt, UpdatedAt: createdAt.Add(time.Minute)}
	if result, err := importer.ImportVendor(context.Background(), ledger.ExchangeImportOperation{
		Token: "ledger-revision-one-drift", OccurredAt: createdAt.Add(time.Hour),
	}, candidate); !errors.Is(err, ledger.ErrInvalidInput) || result.Committed {
		t.Fatalf("accepted revision-one Ledger timestamp drift: %#v err=%v", result, err)
	}
	if _, err := service.GetVendor(context.Background(), candidate.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("invalid Ledger record was persisted: %v", err)
	}
}

func TestOrdinaryLedgerWritesCheckImportedOwnershipFence(t *testing.T) {
	locked := errors.New("resource is externally write-locked")
	gate := &ledgerWriteGate{err: locked}
	service, _, store := newLedgerExchangeService(t, "ledger-locked-org", foundation.NopAuditor{}, gate)
	if _, err := service.CreateVendor(context.Background(), ledger.CreateVendorInput{ID: "locked-vendor", Name: "Local overwrite"}); !errors.Is(err, locked) {
		t.Fatalf("ordinary Ledger create bypassed ownership fence: %v", err)
	}
	if len(gate.requests) != 1 || gate.requests[0] != [2]string{"ledger.vendor", "locked-vendor"} {
		t.Fatalf("unexpected Ledger write gate calls %#v", gate.requests)
	}
	if _, err := store.GetVendor(context.Background(), "ledger-locked-org", "locked-vendor"); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("denied Ledger write changed durable state: %v", err)
	}
}

func withLedgerOrganization(value any, organizationID string) any {
	switch item := value.(type) {
	case ledger.Vendor:
		item.OrganizationID = organizationID
		return item
	case ledger.PurchaseOrder:
		item.OrganizationID = organizationID
		return item
	case ledger.Contract:
		item.OrganizationID = organizationID
		return item
	case ledger.Commitment:
		item.OrganizationID = organizationID
		return item
	case ledger.Budget:
		item.OrganizationID = organizationID
		return item
	case ledger.CostRecord:
		item.OrganizationID = organizationID
		return item
	}
	return value
}
