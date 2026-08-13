package contracttest

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
)

func ExchangeStore(t *testing.T, store exchange.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	packageID := "exchange-" + suffix
	value := exchange.Package{
		OrganizationID: organizationID, PackageID: packageID, Direction: exchange.DirectionImport,
		SchemaVersion: exchange.SchemaVersion, SourceSystemID: "source-" + suffix,
		ArchiveSHA256: strings.Repeat("a", 64), SizeBytes: 1024, FileMode: exchange.FileModeMetadata,
		Status: exchange.StatusProcessing, RecordCount: 1, Records: []exchange.RecordOutcome{},
		CreatedBy: "contract-actor", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	created, wasCreated, err := store.CreatePackage(ctx, value)
	if err != nil || !wasCreated || created.PackageID != packageID {
		t.Fatalf("create Exchange package: created=%#v wasCreated=%t err=%v", created, wasCreated, err)
	}
	replayed, wasCreated, err := store.CreatePackage(ctx, value)
	if err != nil || wasCreated || replayed.ArchiveSHA256 != value.ArchiveSHA256 {
		t.Fatalf("replay Exchange package: replayed=%#v wasCreated=%t err=%v", replayed, wasCreated, err)
	}
	conflict := value
	conflict.ArchiveSHA256 = strings.Repeat("b", 64)
	if _, _, err := store.CreatePackage(ctx, conflict); !errors.Is(err, exchange.ErrConflict) {
		t.Fatalf("expected archive identity conflict, got %v", err)
	}

	holding := value
	holding.Status = exchange.StatusHolding
	holding.HoldingCount = 1
	holding.Records = []exchange.RecordOutcome{{
		Type: "stack.version", ID: "version-one", Revision: 1, Checksum: strings.Repeat("c", 64),
		Status: exchange.OutcomeHolding, MissingDependencies: []exchange.Reference{{Type: "stack.product", ID: "missing-product"}},
	}}
	holding.UpdatedAt = createdAt.Add(time.Second)
	updated, err := store.UpdatePackage(ctx, holding, createdAt)
	if err != nil || updated.Status != exchange.StatusHolding || len(updated.Records) != 1 {
		t.Fatalf("complete held Exchange package: %#v err=%v", updated, err)
	}
	holding.Records[0].MissingDependencies[0].ID = "mutated"
	loaded, err := store.GetPackage(ctx, organizationID, exchange.DirectionImport, packageID)
	if err != nil || loaded.Records[0].MissingDependencies[0].ID != "missing-product" {
		t.Fatalf("Exchange package was not defensively copied: %#v err=%v", loaded, err)
	}
	stale := updated
	stale.UpdatedAt = updated.UpdatedAt.Add(time.Second)
	if _, err := store.UpdatePackage(ctx, stale, createdAt); !errors.Is(err, exchange.ErrConflict) {
		t.Fatalf("expected stale Exchange update conflict, got %v", err)
	}
	retryHolding := updated
	retryHolding.Status = exchange.StatusProcessing
	retryHolding.HoldingCount = 0
	retryHolding.Records = []exchange.RecordOutcome{}
	retryHolding.UpdatedAt = updated.UpdatedAt.Add(time.Second)
	if _, err := store.UpdatePackage(ctx, retryHolding, updated.UpdatedAt); err != nil {
		t.Fatalf("retry held Exchange package: %v", err)
	}

	failed := value
	failed.PackageID += "-failed"
	failed.ArchiveSHA256 = strings.Repeat("d", 64)
	if _, ok, err := store.CreatePackage(ctx, failed); err != nil || !ok {
		t.Fatalf("create retryable Exchange package: ok=%t err=%v", ok, err)
	}
	failed.Status = exchange.StatusFailed
	failed.ErrorCode = "import_failed"
	failed.UpdatedAt = createdAt.Add(time.Second)
	failedStored, err := store.UpdatePackage(ctx, failed, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	retry := failedStored
	retry.Status = exchange.StatusProcessing
	retry.ErrorCode = ""
	retry.UpdatedAt = failedStored.UpdatedAt.Add(time.Second)
	if _, err := store.UpdatePackage(ctx, retry, failedStored.UpdatedAt); err != nil {
		t.Fatalf("retry failed Exchange package: %v", err)
	}

	listed, err := store.ListPackages(ctx, organizationID, exchange.MaximumHistory)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list Exchange packages: %#v err=%v", listed, err)
	}
	other, err := store.ListPackages(ctx, "other-organization", exchange.MaximumHistory)
	if err != nil || len(other) != 0 || other == nil {
		t.Fatalf("expected non-nil isolated empty list, got %#v err=%v", other, err)
	}
}
