package contracttest

// Provider-neutral Stack adapter contract.
// Requirement: REQ-STACK-001. Feature: software.licenses. GitHub: #7.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/stack"
)

func StackStore(t testing.TB, subject stack.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	product := stack.Product{ID: "stack-product-" + suffix, OrganizationID: organizationID, Name: "Writer " + suffix, Publisher: "Example", Status: "active", SourceSystemID: "catalog", SourceRecordID: "product-" + suffix, Revision: 1, CreatedAt: now, UpdatedAt: now}
	createdProduct, created, err := subject.CreateProduct(ctx, product)
	if err != nil || !created || createdProduct.ID != product.ID {
		t.Fatalf("create Stack product: %#v created=%t err=%v", createdProduct, created, err)
	}
	replay := product
	replay.CreatedAt = now.Add(time.Hour)
	replay.UpdatedAt = replay.CreatedAt
	if existing, created, err := subject.CreateProduct(ctx, replay); err != nil || created || existing.ID != product.ID {
		t.Fatalf("expected idempotent product source replay: %#v created=%t err=%v", existing, created, err)
	}
	changed := replay
	changed.Name = "Conflicting writer"
	if _, _, err := subject.CreateProduct(ctx, changed); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected changed source conflict, got %v", err)
	}

	version := stack.Version{ID: "stack-version-" + suffix, OrganizationID: organizationID, ProductID: product.ID, Name: "1.0", Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, created, err := subject.CreateVersion(ctx, version); err != nil || !created {
		t.Fatalf("create version: created=%t err=%v", created, err)
	}
	installation := stack.Installation{ID: "stack-install-" + suffix, OrganizationID: organizationID, VersionID: version.ID, AssetID: "asset-" + suffix, Status: "installed", UsageState: "used", InstalledAt: now, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, created, err := subject.CreateInstallation(ctx, installation); err != nil || !created {
		t.Fatalf("create installation: created=%t err=%v", created, err)
	}
	duplicateInstall := installation
	duplicateInstall.ID += "-duplicate"
	if _, _, err := subject.CreateInstallation(ctx, duplicateInstall); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected active installation conflict, got %v", err)
	}
	expires := now.AddDate(1, 0, 0)
	license := stack.License{ID: "stack-license-" + suffix, OrganizationID: organizationID, ProductID: product.ID, VersionID: version.ID, Name: "Device subscription", EntitlementMetric: "device", Quantity: 10, Status: "active", ExpiresOn: &expires, DocumentIDs: []string{"document-two", "document-one"}, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, created, err := subject.CreateLicense(ctx, license); err != nil || !created {
		t.Fatalf("create license: created=%t err=%v", created, err)
	}
	license.DocumentIDs[0] = "mutated"
	loadedLicense, err := subject.GetLicense(ctx, organizationID, license.ID)
	if err != nil || loadedLicense.DocumentIDs[0] != "document-two" {
		t.Fatalf("license was not defensively persisted: %#v err=%v", loadedLicense, err)
	}
	license = loadedLicense
	assignment := stack.Assignment{ID: "stack-assignment-" + suffix, OrganizationID: organizationID, LicenseID: license.ID, AssigneeKind: "asset", AssigneeID: installation.AssetID, Seats: 1, UsageState: "unknown", AssignedAt: now, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, created, err := subject.CreateAssignment(ctx, assignment); err != nil || !created {
		t.Fatalf("create assignment: created=%t err=%v", created, err)
	}
	duplicateAssignment := assignment
	duplicateAssignment.ID += "-duplicate"
	if _, _, err := subject.CreateAssignment(ctx, duplicateAssignment); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected active assignment conflict, got %v", err)
	}
	assignment.UsageState = "unused"
	assignment.Revision = 2
	assignment.UpdatedAt = now.Add(time.Minute)
	if _, err := subject.UpdateAssignment(ctx, assignment, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.UpdateAssignment(ctx, assignment, 1); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected stale assignment conflict, got %v", err)
	}
	changedAssignment := assignment
	changedAssignment.Seats++
	changedAssignment.Revision = 3
	changedAssignment.UpdatedAt = now.Add(2 * time.Minute)
	if _, err := subject.UpdateAssignment(ctx, changedAssignment, 2); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected immutable assignment conflict, got %v", err)
	}
	product.Status = "retired"
	product.Revision = 2
	product.UpdatedAt = now.Add(time.Minute)
	changedProduct := product
	changedProduct.Name = "Rewritten product"
	if _, err := subject.UpdateProduct(ctx, changedProduct, 1); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected immutable product conflict, got %v", err)
	}
	jumpedProduct := product
	jumpedProduct.Revision = 3
	if _, err := subject.UpdateProduct(ctx, jumpedProduct, 1); !errors.Is(err, stack.ErrConflict) {
		t.Fatalf("expected non-sequential product revision conflict, got %v", err)
	}
	if _, err := subject.UpdateProduct(ctx, product, 1); err != nil {
		t.Fatal(err)
	}
	version.Status = "unsupported"
	version.Revision = 2
	version.UpdatedAt = now.Add(time.Minute)
	if _, err := subject.UpdateVersion(ctx, version, 1); err != nil {
		t.Fatal(err)
	}
	removedAt := now.Add(time.Hour)
	installation.Status = "removed"
	installation.RemovedAt = &removedAt
	installation.Revision = 2
	installation.UpdatedAt = removedAt
	if _, err := subject.UpdateInstallation(ctx, installation, 1); err != nil {
		t.Fatal(err)
	}
	license.Quantity = 12
	license.Revision = 2
	license.UpdatedAt = now.Add(time.Minute)
	if _, err := subject.UpdateLicense(ctx, license, 1); err != nil {
		t.Fatal(err)
	}

	snapshot, err := subject.Snapshot(ctx, organizationID)
	if err != nil || len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 || len(snapshot.Installations) != 1 || len(snapshot.Licenses) != 1 || len(snapshot.Assignments) != 1 || snapshot.Assignments[0].UsageState != "unused" || snapshot.Products[0].Status != "retired" || snapshot.Versions[0].Status != "unsupported" || snapshot.Installations[0].Status != "removed" || snapshot.Licenses[0].Quantity != 12 {
		t.Fatalf("unexpected Stack snapshot %#v err=%v", snapshot, err)
	}
	isolated, err := subject.Snapshot(ctx, organizationID+"-other")
	if err != nil || len(isolated.Products) != 0 || len(isolated.Licenses) != 0 {
		t.Fatalf("expected Stack organization isolation %#v err=%v", isolated, err)
	}
	if _, err := subject.GetProduct(ctx, organizationID+"-other", product.ID); !errors.Is(err, stack.ErrNotFound) {
		t.Fatalf("expected isolated lookup failure, got %v", err)
	}
}
