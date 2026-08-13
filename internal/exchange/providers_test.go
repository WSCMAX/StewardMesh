package exchange_test

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

func TestStackProviderRoundTripPreservesEarliestProvenance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)
	sourceStack, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := sourceStack.CreateProduct(ctx, stack.CreateProductInput{
		ID: "portable-product", Name: "Portable product", Publisher: "Example",
		SourceSystemID: "original-catalog", SourceRecordID: "catalog:product:42",
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := sourceStack.CreateVersion(ctx, stack.CreateVersionInput{ID: "portable-version", ProductID: product.ID, Name: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewStackProvider(sourceStack)
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("stack-source"), exchange.ServiceConfig{
		OrganizationID: "stack-source", SourceSystemID: "source-appliance", Now: func() time.Time { return now },
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(ctx, "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "stack.version", ID: version.ID}}, IncludeDependencies: true, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := exchangeManifestBytes(t, artifact.Bytes)
	for _, privateField := range []string{`"organizationId"`, `"createdAt"`, `"updatedAt"`} {
		if bytes.Contains(manifest, []byte(privateField)) {
			t.Fatalf("Stack Exchange payload retained transport-local field %s: %s", privateField, manifest)
		}
	}

	targetStack, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider, _ := exchange.NewStackProvider(targetStack)
	ownership := newExchangeOwnership("stack-target")
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stack-target", SourceSystemID: "target-appliance", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Import(ctx, "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 2 {
		t.Fatalf("unexpected Stack provider import %#v err=%v", result, err)
	}
	snapshot, err := targetStack.Snapshot(ctx)
	if err != nil || len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 {
		t.Fatalf("unexpected target Stack snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Products[0].SourceSystemID != "original-catalog" || snapshot.Products[0].SourceRecordID != "catalog:product:42" {
		t.Fatalf("earliest product provenance was lost: %#v", snapshot.Products[0])
	}
	if snapshot.Versions[0].SourceSystemID != "source-appliance" || snapshot.Versions[0].SourceRecordID != "stack.version:portable-version" {
		t.Fatalf("local source provenance was not assigned: %#v", snapshot.Versions[0])
	}
	for _, key := range []string{"stack.product:portable-product", "stack.version:portable-version"} {
		if !ownership.values[key].WriteLocked {
			t.Fatalf("imported Stack record %s was not write-locked", key)
		}
	}
	replayed, err := target.Import(ctx, "import-operator", artifact.Bytes)
	if err != nil || !replayed.Replay || replayed.Package.CreatedCount != 2 {
		t.Fatalf("Stack provider replay changed results: %#v err=%v", replayed, err)
	}
}

func TestStackProviderRepairsAuditAfterCommittedImportWithoutLosingCreatedTruth(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 10, 0, 0, time.UTC)
	sourceStack, _ := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-audit-source", Now: func() time.Time { return now },
	})
	product, err := sourceStack.CreateProduct(context.Background(), stack.CreateProductInput{
		ID: "audit-product", Name: "Audited product", Publisher: "Example", SourceSystemID: "catalog", SourceRecordID: "catalog:audit-product",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewStackProvider(sourceStack)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("stack-audit-source"), exchange.ServiceConfig{
		OrganizationID: "stack-audit-source", SourceSystemID: "stack-audit-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "stack.product", ID: product.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	auditor := &failOnceExchangeAuditor{failures: 1}
	targetStack, _ := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, auditor, stack.ServiceConfig{
		OrganizationID: "stack-audit-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	targetProvider, _ := exchange.NewStackProvider(targetStack)
	ownership := newExchangeOwnership("stack-audit-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stack-audit-target", SourceSystemID: "target-system", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if _, err := target.Import(context.Background(), "import-operator", artifact.Bytes); err == nil {
		t.Fatal("expected the injected Stack audit failure")
	}
	if _, err := targetStack.ExportRecord(context.Background(), "stack.product", product.ID); err != nil {
		t.Fatalf("Stack domain write did not survive its audit failure: %v", err)
	}
	history, _ := target.ListPackages(context.Background(), 25)
	if len(history) != 1 || history[0].Status != exchange.StatusFailed || history[0].CreatedCount != 1 ||
		len(history[0].Progress) != 1 || !ownership.values["stack.product:audit-product"].WriteLocked {
		t.Fatalf("Stack committed receipt or ownership was lost: history=%#v ownership=%#v", history, ownership.values)
	}
	recovered, err := target.Import(context.Background(), "retry-operator", artifact.Bytes)
	if err != nil || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 1 {
		t.Fatalf("Stack audit repair failed: %#v err=%v", recovered, err)
	}
	if len(auditor.attempts) != 2 || auditor.attempts[0].ID != auditor.attempts[1].ID ||
		auditor.attempts[0].CorrelationID != auditor.attempts[1].CorrelationID {
		t.Fatalf("Stack audit retry was not deterministic: %#v", auditor.attempts)
	}
}

func TestStackProviderUsesBoundedExactLookupsForLargeImports(t *testing.T) {
	const records = 256
	now := time.Date(2026, time.August, 13, 15, 15, 0, 0, time.UTC)
	sourceStack, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "large-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetStore := &countingStackStore{Store: repository.NewMemoryStackStore()}
	targetStack, err := stack.NewService(targetStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "large-target", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < records; index++ {
		input := stack.CreateProductInput{
			ID: fmt.Sprintf("bounded-product-%03d", index), Name: fmt.Sprintf("Bounded product %03d", index), Publisher: "Example",
			SourceSystemID: "shared-catalog", SourceRecordID: fmt.Sprintf("catalog:%03d", index),
		}
		if _, err := sourceStack.CreateProduct(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if _, err := targetStack.CreateProduct(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	sourceProvider, _ := exchange.NewStackProvider(sourceStack)
	portable, err := sourceProvider.ListRecords(context.Background())
	if err != nil || len(portable) != records {
		t.Fatalf("unexpected source catalog size %d err=%v", len(portable), err)
	}
	targetStore.snapshotCalls, targetStore.exactReads = 0, 0
	targetProvider, _ := exchange.NewStackProvider(targetStack)
	for _, record := range portable {
		result, err := targetProvider.ImportRecord(context.Background(), exchange.ProviderImportOperation{Token: "direct-import", OccurredAt: now}, "shared-catalog", record, nil)
		if err != nil || result.Created || !result.Committed {
			t.Fatalf("exact replay %s was not unchanged: result=%#v err=%v", record.ID, result, err)
		}
	}
	if targetStore.snapshotCalls != 0 || targetStore.exactReads != records {
		t.Fatalf("large import used unbounded inventory reads: snapshots=%d exact=%d records=%d", targetStore.snapshotCalls, targetStore.exactReads, records)
	}
}

func TestExchangeStackImportUsesOnlyKeyedReadsAcrossDependencyChecksAndWrites(t *testing.T) {
	const records = 64
	now := time.Date(2026, time.August, 13, 15, 20, 0, 0, time.UTC)
	sourceStack, _ := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "operation-source", Now: func() time.Time { return now },
	})
	product, err := sourceStack.CreateProduct(context.Background(), stack.CreateProductInput{
		ID: "operation-product", Name: "Operation product", Publisher: "Example", SourceSystemID: "catalog", SourceRecordID: "catalog:operation-product",
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := make([]exchange.Reference, 0, records)
	for index := 0; index < records; index++ {
		id := fmt.Sprintf("operation-version-%03d", index)
		if _, err := sourceStack.CreateVersion(context.Background(), stack.CreateVersionInput{ID: id, ProductID: product.ID, Name: fmt.Sprintf("1.%d", index)}); err != nil {
			t.Fatal(err)
		}
		selection = append(selection, exchange.Reference{Type: "stack.version", ID: id})
	}
	sourceProvider, _ := exchange.NewStackProvider(sourceStack)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("operation-source"), exchange.ServiceConfig{
		OrganizationID: "operation-source", SourceSystemID: "operation-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: selection, IncludeDependencies: false, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	countingStore := &countingStackStore{Store: repository.NewMemoryStackStore()}
	targetStack, _ := stack.NewService(countingStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "operation-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if _, err := targetStack.CreateProduct(context.Background(), stack.CreateProductInput{
		ID: product.ID, Name: product.Name, Publisher: product.Publisher, SourceSystemID: product.SourceSystemID, SourceRecordID: product.SourceRecordID,
	}); err != nil {
		t.Fatal(err)
	}
	countingStore.snapshotCalls, countingStore.exactReads = 0, 0
	targetProvider, _ := exchange.NewStackProvider(targetStack)
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("operation-target"), exchange.ServiceConfig{
		OrganizationID: "operation-target", SourceSystemID: "target-system", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != records {
		t.Fatalf("full Stack import failed: %#v err=%v", result, err)
	}
	if countingStore.snapshotCalls != 0 || countingStore.exactReads != records*4 {
		t.Fatalf("full Exchange import used unexpected Stack reads: snapshots=%d exact=%d wantExact=%d", countingStore.snapshotCalls, countingStore.exactReads, records*4)
	}
}

func TestStackProviderExposesEveryCrossDomainRelationship(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 30, 0, 0, time.UTC)
	service, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "dependency-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, _ := service.CreateProduct(context.Background(), stack.CreateProductInput{ID: "dependency-product", Name: "Product", Publisher: "Example"})
	version, _ := service.CreateVersion(context.Background(), stack.CreateVersionInput{ID: "dependency-version", ProductID: product.ID, Name: "1.0"})
	license, err := service.CreateLicense(context.Background(), stack.CreateLicenseInput{
		ID: "dependency-license", ProductID: product.ID, VersionID: version.ID, Name: "Enterprise",
		EntitlementMetric: "enterprise", Quantity: 10, VendorID: "vendor-one", PurchaseOrderID: "po-one",
		ContractID: "contract-one", CostRecordID: "cost-one", DocumentIDs: []string{"document-one", "document-two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := service.CreateAssignment(context.Background(), stack.CreateAssignmentInput{
		ID: "dependency-assignment", LicenseID: license.ID, AssigneeKind: "identity", AssigneeID: "person-one", Seats: 1, AssignedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := exchange.NewStackProvider(service)
	records, err := provider.ListRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]exchange.Record)
	for _, record := range records {
		byKey[exchange.Reference{Type: record.Type, ID: record.ID}.Key()] = record
	}
	wantLicense := []exchange.Reference{
		{Type: "ledger.contract", ID: "contract-one"}, {Type: "ledger.cost", ID: "cost-one"},
		{Type: "ledger.purchase_order", ID: "po-one"}, {Type: "ledger.vendor", ID: "vendor-one"},
		{Type: "stack.product", ID: product.ID}, {Type: "stack.version", ID: version.ID},
		{Type: "vault.blob", ID: "document-one"}, {Type: "vault.blob", ID: "document-two"},
	}
	if !slices.Equal(byKey["stack.license:"+license.ID].Dependencies, wantLicense) {
		t.Fatalf("license relationships were incomplete: %#v", byKey["stack.license:"+license.ID].Dependencies)
	}
	wantAssignment := []exchange.Reference{{Type: "people.identity", ID: "person-one"}, {Type: "stack.license", ID: license.ID}}
	if !slices.Equal(byKey["stack.assignment:"+assignment.ID].Dependencies, wantAssignment) {
		t.Fatalf("assignment relationship was not canonical: %#v", byKey["stack.assignment:"+assignment.ID].Dependencies)
	}
}

func TestVaultProviderRoundTripIncludesVerifiedBytesAndExcludesPrivateTransportData(t *testing.T) {
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "vault-source", ActorID: "private-source-operator", CorrelationID: "exchange-vault-source",
	})
	now := time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
	content := []byte("portable evidence with verified bytes")
	sourceObjects := newExchangeObjectStore("s3", 1<<20)
	sourceVault, err := storage.NewService(repository.NewMemoryStorageStore(), sourceObjects, foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "vault-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := sourceVault.CreateBlob(ctx, storage.CreateBlobInput{
		Name: "evidence.txt", MediaType: "text/plain", Content: bytes.NewReader(content),
		SourceSystemID: "original-vault", SourceRecordID: "evidence:42",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("vault-source"), exchange.ServiceConfig{
		OrganizationID: "vault-source", SourceSystemID: "source-appliance", Now: func() time.Time { return now },
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(ctx, "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeInclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := exchangeManifestBytes(t, artifact.Bytes)
	for _, forbidden := range []string{"private-source-operator", "vault-source", "objectKey", "signed.example.test", "X-Amz-Signature", "credential"} {
		if bytes.Contains(manifest, []byte(forbidden)) {
			t.Fatalf("Exchange manifest leaked %q: %s", forbidden, manifest)
		}
	}
	if !bytes.Contains(manifest, []byte(`"provider":"s3"`)) || !bytes.Contains(manifest, []byte(`"entry":"files/`)) {
		t.Fatalf("S3 metadata and included file entry were not explicit: %s", manifest)
	}

	targetObjects := newExchangeObjectStore("local", 1<<20)
	targetVault, err := storage.NewService(repository.NewMemoryStorageStore(), targetObjects, foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "vault-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("vault-target"), exchange.ServiceConfig{
		OrganizationID: "vault-target", SourceSystemID: "target-appliance", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 1 || result.Package.FileCount != 1 {
		t.Fatalf("unexpected Vault import %#v err=%v", result, err)
	}
	imported, reader, err := targetVault.OpenBlob(context.Background(), blob.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actual, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(actual, content) || imported.SHA256 != blob.SHA256 ||
		imported.SourceSystemID != "original-vault" || imported.SourceRecordID != "evidence:42" {
		t.Fatalf("Vault bytes or provenance did not round trip: %#v bytes=%q err=%v", imported, actual, err)
	}
}

func TestVaultProviderRepairsAuditAfterCommittedImportWithoutLosingCreatedTruth(t *testing.T) {
	now := time.Date(2026, time.August, 13, 16, 30, 0, 0, time.UTC)
	content := []byte("durable bytes before audit delivery")
	sourceVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("s3", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "vault-audit-source", Now: func() time.Time { return now },
	})
	blob, err := sourceVault.CreateBlob(context.Background(), storage.CreateBlobInput{
		Name: "audit.txt", MediaType: "text/plain", Content: bytes.NewReader(content), SourceSystemID: "archive", SourceRecordID: "archive:audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("vault-audit-source"), exchange.ServiceConfig{
		OrganizationID: "vault-audit-source", SourceSystemID: "vault-audit-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeInclude,
	})
	if err != nil {
		t.Fatal(err)
	}

	auditor := &failOnceExchangeAuditor{failures: 1}
	targetVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("local", 1<<20), auditor, storage.ServiceConfig{
		OrganizationID: "vault-audit-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	ownership := newExchangeOwnership("vault-audit-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "vault-audit-target", SourceSystemID: "target-system", Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	if _, err := target.Import(context.Background(), "import-operator", artifact.Bytes); err == nil {
		t.Fatal("expected the injected Vault audit failure")
	}
	_, reader, err := targetVault.OpenBlob(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("Vault domain write did not survive its audit failure: %v", err)
	}
	actual, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(actual, content) {
		t.Fatalf("Vault committed bytes changed: %q err=%v", actual, readErr)
	}
	history, _ := target.ListPackages(context.Background(), 25)
	if len(history) != 1 || history[0].Status != exchange.StatusFailed || history[0].CreatedCount != 1 ||
		len(history[0].Progress) != 1 || !ownership.values["vault.blob:"+blob.ID].WriteLocked {
		t.Fatalf("Vault committed receipt or ownership was lost: history=%#v ownership=%#v", history, ownership.values)
	}
	recovered, err := target.Import(context.Background(), "retry-operator", artifact.Bytes)
	if err != nil || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 1 {
		t.Fatalf("Vault audit repair failed: %#v err=%v", recovered, err)
	}
	if len(auditor.attempts) != 2 || auditor.attempts[0].ID != auditor.attempts[1].ID ||
		auditor.attempts[0].CorrelationID != auditor.attempts[1].CorrelationID {
		t.Fatalf("Vault audit retry was not deterministic: %#v", auditor.attempts)
	}
}

func TestVaultMetadataOnlyImportRemainsReadableInHoldingWithoutWritingBytes(t *testing.T) {
	now := time.Date(2026, time.August, 13, 17, 0, 0, 0, time.UTC)
	sourceVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("s3", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-source", Now: func() time.Time { return now },
	})
	blob, err := sourceVault.CreateBlob(context.Background(), storage.CreateBlobInput{
		Name: "metadata.txt", MediaType: "text/plain", Content: strings.NewReader("metadata only"),
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("metadata-source"), exchange.ServiceConfig{
		OrganizationID: "metadata-source", SourceSystemID: "metadata-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("local", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-target", Now: func() time.Time { return now },
	})
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	ownership := newExchangeOwnership("metadata-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "metadata-target", SourceSystemID: "target-system", Now: func() time.Time { return now },
	}, targetProvider)
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusHolding || result.Package.HoldingCount != 1 ||
		len(result.Package.Records[0].MissingDependencies) != 1 || result.Package.Records[0].MissingDependencies[0].Type != "exchange.file" {
		t.Fatalf("metadata-only Vault record was not visible in holding: %#v err=%v", result, err)
	}
	if _, err := targetVault.GetBlob(context.Background(), blob.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("metadata-only import wrote a Vault record without bytes: %v", err)
	}
	if len(ownership.values) != 0 {
		t.Fatalf("holding record acquired an ownership lock: %#v", ownership.values)
	}
}

func TestVaultMetadataOnlyImportIsUnchangedWhenExactTargetContentExists(t *testing.T) {
	now := time.Date(2026, time.August, 13, 17, 30, 0, 0, time.UTC)
	content := []byte("already present exact content")
	sourceVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("s3", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-exact-source", Now: func() time.Time { return now },
	})
	blob, err := sourceVault.CreateBlob(context.Background(), storage.CreateBlobInput{
		Name: "exact.txt", MediaType: "text/plain", Content: bytes.NewReader(content),
		SourceSystemID: "original-vault", SourceRecordID: "exact-record",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("metadata-exact-source"), exchange.ServiceConfig{
		OrganizationID: "metadata-exact-source", SourceSystemID: "metadata-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	targetVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("local", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-exact-target", Now: func() time.Time { return now },
	})
	if _, created, err := targetVault.ImportBlob(context.Background(), storage.ImportBlobInput{
		ID: blob.ID, Name: blob.Name, MediaType: blob.MediaType, SizeBytes: blob.SizeBytes, SHA256: blob.SHA256,
		SourceSystemID: blob.SourceSystemID, SourceRecordID: blob.SourceRecordID, Content: bytes.NewReader(content),
	}); err != nil || !created {
		t.Fatalf("seed exact target Vault blob: created=%t err=%v", created, err)
	}
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	ownership := newExchangeOwnership("metadata-exact-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "metadata-exact-target", SourceSystemID: "target-system", Now: func() time.Time { return now },
	}, targetProvider)
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted || result.Package.UnchangedCount != 1 || result.Package.HoldingCount != 0 {
		t.Fatalf("exact metadata-only Vault record was not unchanged: %#v err=%v", result, err)
	}
	if !ownership.values["vault.blob:"+blob.ID].WriteLocked {
		t.Fatalf("exact imported Vault record was not write-locked: %#v", ownership.values)
	}
	_, reader, err := targetVault.OpenBlob(context.Background(), blob.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	actual, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("metadata-only replay changed target content: %q err=%v", actual, err)
	}
}

func TestVaultMetadataOnlyImportHoldsWhenTargetMetadataOutlivesContent(t *testing.T) {
	now := time.Date(2026, time.August, 13, 17, 45, 0, 0, time.UTC)
	content := []byte("content removed after metadata was stored")
	sourceVault, _ := storage.NewService(repository.NewMemoryStorageStore(), newExchangeObjectStore("s3", 1<<20), foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-stale-source", Now: func() time.Time { return now },
	})
	blob, err := sourceVault.CreateBlob(context.Background(), storage.CreateBlobInput{
		Name: "stale.txt", MediaType: "text/plain", Content: bytes.NewReader(content),
		SourceSystemID: "original-vault", SourceRecordID: "stale-record",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider, _ := exchange.NewVaultProvider(sourceVault)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("metadata-stale-source"), exchange.ServiceConfig{
		OrganizationID: "metadata-stale-source", SourceSystemID: "metadata-system", Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "vault.blob", ID: blob.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	targetObjects := newExchangeObjectStore("local", 1<<20)
	targetVault, _ := storage.NewService(repository.NewMemoryStorageStore(), targetObjects, foundation.NopAuditor{}, storage.ServiceConfig{
		OrganizationID: "metadata-stale-target", Now: func() time.Time { return now },
	})
	if _, created, err := targetVault.ImportBlob(context.Background(), storage.ImportBlobInput{
		ID: blob.ID, Name: blob.Name, MediaType: blob.MediaType, SizeBytes: blob.SizeBytes, SHA256: blob.SHA256,
		SourceSystemID: blob.SourceSystemID, SourceRecordID: blob.SourceRecordID, Content: bytes.NewReader(content),
	}); err != nil || !created {
		t.Fatalf("seed target Vault metadata and content: created=%t err=%v", created, err)
	}
	for key := range targetObjects.objects {
		delete(targetObjects.objects, key)
	}
	targetProvider, _ := exchange.NewVaultProvider(targetVault)
	ownership := newExchangeOwnership("metadata-stale-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "metadata-stale-target", SourceSystemID: "target-system", Now: func() time.Time { return now },
	}, targetProvider)
	result, err := target.Import(context.Background(), "import-operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusHolding || result.Package.HoldingCount != 1 ||
		len(result.Package.Records[0].MissingDependencies) != 1 || result.Package.Records[0].MissingDependencies[0].Type != "exchange.file" {
		t.Fatalf("metadata-only record with missing target content was not held: %#v err=%v", result, err)
	}
	if len(ownership.values) != 0 {
		t.Fatalf("content-missing holding record acquired an ownership lock: %#v", ownership.values)
	}
}

type allowExchangeStackReferences struct{}

func (allowExchangeStackReferences) ResolveAsset(_ context.Context, id string) (stack.AssetContext, error) {
	return stack.AssetContext{ID: id}, nil
}
func (allowExchangeStackReferences) ValidateAssignee(context.Context, string, string) error {
	return nil
}
func (allowExchangeStackReferences) ValidateFinancialReferences(context.Context, string, string, string, string) error {
	return nil
}
func (allowExchangeStackReferences) ValidateDocuments(context.Context, []string) error { return nil }

type failOnceExchangeAuditor struct {
	failures int
	attempts []foundation.AuditEvent
}

func (a *failOnceExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.attempts = append(a.attempts, event)
	if a.failures > 0 {
		a.failures--
		return errors.New("injected audit delivery failure")
	}
	return nil
}

type countingStackStore struct {
	stack.Store
	snapshotCalls int
	exactReads    int
}

func (s *countingStackStore) Snapshot(ctx context.Context, organizationID string) (stack.Snapshot, error) {
	s.snapshotCalls++
	return s.Store.Snapshot(ctx, organizationID)
}

func (s *countingStackStore) GetProduct(ctx context.Context, organizationID, id string) (stack.Product, error) {
	s.exactReads++
	return s.Store.GetProduct(ctx, organizationID, id)
}

func (s *countingStackStore) GetVersion(ctx context.Context, organizationID, id string) (stack.Version, error) {
	s.exactReads++
	return s.Store.GetVersion(ctx, organizationID, id)
}

func (s *countingStackStore) GetInstallation(ctx context.Context, organizationID, id string) (stack.Installation, error) {
	s.exactReads++
	return s.Store.GetInstallation(ctx, organizationID, id)
}

func (s *countingStackStore) GetLicense(ctx context.Context, organizationID, id string) (stack.License, error) {
	s.exactReads++
	return s.Store.GetLicense(ctx, organizationID, id)
}

func (s *countingStackStore) GetAssignment(ctx context.Context, organizationID, id string) (stack.Assignment, error) {
	s.exactReads++
	return s.Store.GetAssignment(ctx, organizationID, id)
}

type exchangeObjectStore struct {
	provider string
	maximum  int64
	objects  map[string][]byte
}

func newExchangeObjectStore(provider string, maximum int64) *exchangeObjectStore {
	return &exchangeObjectStore{provider: provider, maximum: maximum, objects: make(map[string][]byte)}
}

func (s *exchangeObjectStore) Provider() string    { return s.provider }
func (s *exchangeObjectStore) MaximumBytes() int64 { return s.maximum }
func (s *exchangeObjectStore) Put(_ context.Context, key, _ string, content io.Reader) (storage.StoredObject, error) {
	if _, exists := s.objects[key]; exists {
		return storage.StoredObject{}, storage.ErrConflict
	}
	value, err := io.ReadAll(io.LimitReader(content, s.maximum+1))
	if err != nil {
		return storage.StoredObject{}, err
	}
	if int64(len(value)) > s.maximum {
		return storage.StoredObject{}, storage.ErrTooLarge
	}
	digest := sha256.Sum256(value)
	s.objects[key] = append([]byte(nil), value...)
	return storage.StoredObject{SizeBytes: int64(len(value)), SHA256: hex.EncodeToString(digest[:])}, nil
}
func (s *exchangeObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := s.objects[key]
	if !exists {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}
func (s *exchangeObjectStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}
func (*exchangeObjectStore) AuthorizeDownload(context.Context, string, string, time.Duration) (storage.ObjectDownloadAuthorization, error) {
	return storage.ObjectDownloadAuthorization{URL: "https://signed.example.test/file?X-Amz-Signature=must-not-export"}, nil
}
func (*exchangeObjectStore) ValidateDownload(context.Context, string, string) error { return nil }

func exchangeManifestBytes(t *testing.T, contents []byte) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		if entry.Name != "manifest.json" {
			continue
		}
		opened, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer opened.Close()
		value, err := io.ReadAll(opened)
		if err != nil || !json.Valid(value) {
			t.Fatalf("invalid Exchange manifest: %v", err)
		}
		return value
	}
	t.Fatal("manifest.json was not found")
	return nil
}
