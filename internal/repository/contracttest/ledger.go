package contracttest

// Provider-neutral Ledger adapter contract. Requirement: REQ-LEDGER-001.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/ledger"
)

func LedgerStore(t testing.TB, subject ledger.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	vendorID := "vendor-" + suffix
	if _, err := subject.GetVendor(ctx, organizationID, vendorID); !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected missing vendor, got %v", err)
	}
	vendor := ledger.Vendor{ID: vendorID, OrganizationID: organizationID, Name: "Example Vendor " + suffix, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := subject.CreateVendor(ctx, vendor); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.CreateVendor(ctx, vendor); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected vendor conflict, got %v", err)
	}

	orderedOn := now
	purchaseOrder := ledger.PurchaseOrder{
		ID: "po-" + suffix, OrganizationID: organizationID, Number: "PO-" + suffix, VendorID: vendorID,
		Status: "ordered", Currency: "USD", TotalMinor: 12500, OrderedOn: &orderedOn,
		AssetIDs: []string{"asset-" + suffix}, ReceiptDocumentIDs: []string{"doc-" + suffix},
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := subject.CreatePurchaseOrder(ctx, purchaseOrder); err != nil {
		t.Fatal(err)
	}
	purchaseOrder.Status, purchaseOrder.Revision, purchaseOrder.UpdatedAt = "received", 2, now.Add(time.Minute)
	if _, err := subject.UpdatePurchaseOrder(ctx, purchaseOrder, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.UpdatePurchaseOrder(ctx, purchaseOrder, 1); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected stale purchase order conflict, got %v", err)
	}

	contract := ledger.Contract{
		ID: "contract-" + suffix, OrganizationID: organizationID, Name: "Service agreement " + suffix,
		VendorID: vendorID, OperationalStatus: "active", FinancialStatus: "committed", Currency: "USD", CeilingMinor: 500000,
		StartsOn: now, EndsOn: now.AddDate(3, 0, 0), DocumentIDs: []string{"doc-" + suffix}, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := subject.CreateContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	contract.FinancialStatus, contract.Revision, contract.UpdatedAt = "billed", 2, now.Add(time.Minute)
	if _, err := subject.UpdateContract(ctx, contract, 1); err != nil {
		t.Fatal(err)
	}

	commitment := ledger.Commitment{
		ID: "commitment-" + suffix, OrganizationID: organizationID, ContractID: contract.ID, Kind: "subscription",
		Description: "Three-year service", Currency: "USD", AmountMinor: 150000, StartsOn: now, EndsOn: now.AddDate(3, 0, 0),
		FiscalPeriod: "FY2027", Scenario: "baseline", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := subject.CreateCommitment(ctx, commitment); err != nil {
		t.Fatal(err)
	}
	budget := ledger.Budget{
		ID: "budget-" + suffix, OrganizationID: organizationID, Name: "Infrastructure " + suffix, FiscalPeriod: "FY2027",
		Scenario: "baseline", Currency: "USD", AllocatedMinor: 200000, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := subject.CreateBudget(ctx, budget); err != nil {
		t.Fatal(err)
	}

	cost := ledger.CostRecord{
		ID: "cost-" + suffix, OrganizationID: organizationID, Description: "Invoice", Kind: "billed", Currency: "USD",
		AmountMinor: 120000, FiscalPeriod: "FY2027", Scenario: "baseline", PurchaseOrderID: purchaseOrder.ID,
		SourceSystemID: "erp", SourceRecordID: "invoice-" + suffix, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := subject.CreateCost(ctx, cost); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.CreateCost(ctx, cost); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected cost source conflict, got %v", err)
	}
	loaded, err := subject.GetCostBySource(ctx, organizationID, "ERP", cost.SourceRecordID)
	if err != nil || loaded.ID != cost.ID {
		t.Fatalf("unexpected reconciled cost %#v err=%v", loaded, err)
	}
	cost.Kind, cost.Revision, cost.UpdatedAt = "paid", 2, now.Add(time.Minute)
	if _, err := subject.UpdateCost(ctx, cost, 1); err != nil {
		t.Fatal(err)
	}

	snapshot, err := subject.Snapshot(ctx, organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Vendors) != 1 || len(snapshot.PurchaseOrders) != 1 || len(snapshot.Contracts) != 1 ||
		len(snapshot.Commitments) != 1 || len(snapshot.Budgets) != 1 || len(snapshot.Costs) != 1 || snapshot.Costs[0].Kind != "paid" {
		t.Fatalf("unexpected Ledger snapshot %#v", snapshot)
	}
	isolated, err := subject.Snapshot(ctx, organizationID+"-other")
	if err != nil || len(isolated.Vendors) != 0 || len(isolated.Costs) != 0 {
		t.Fatalf("expected organization isolation, snapshot=%#v err=%v", isolated, err)
	}
}
