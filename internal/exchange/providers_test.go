package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001, REQ-STACK-001. Features: migration.packages, templates.schemas, software.licenses. GitHub: #9, #8, #7.

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
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/stack"
	"github.com/maxlemke/stewardmesh/internal/storage"
)

type exchangeStackFixture struct {
	*stack.Service
	importer stack.ExchangeImporter
}

func newExchangeStackService(store stack.Store, references stack.ReferenceValidator, auditor foundation.Auditor, configuration stack.ServiceConfig) (*exchangeStackFixture, error) {
	service, importer, err := stack.NewServiceWithExchangeImporter(store, references, auditor, configuration)
	if err != nil {
		return nil, err
	}
	return &exchangeStackFixture{Service: service, importer: importer}, nil
}

func newExchangeStackProvider(t *testing.T, fixture *exchangeStackFixture) *exchange.StackProvider {
	t.Helper()
	provider, err := exchange.NewStackProvider(fixture.Service, fixture.importer)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestStackProviderRoundTripPreservesEarliestProvenance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 15, 0, 0, 0, time.UTC)
	sourceStack, err := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
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
	sourceProvider := newExchangeStackProvider(t, sourceStack)
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("stack-source"), exchange.ServiceConfig{
		OrganizationID: "stack-source", SourceSystemID: "source-appliance", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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

	targetStack, err := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("stack-target")
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stack-target", SourceSystemID: "target-appliance", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
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

func TestDirectStackImportUsesDurableReceiptOwnershipAndExactReplay(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 2, 0, 0, time.UTC)
	sourceStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-source", Now: func() time.Time { return now },
	})
	product, err := sourceStack.CreateProduct(context.Background(), stack.CreateProductInput{ID: "direct-product", Name: "Durable product", Publisher: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceStack.CreateVersion(context.Background(), stack.CreateVersionInput{ID: "direct-version", ProductID: product.ID, Name: "1.0"}); err != nil {
		t.Fatal(err)
	}
	records, err := sourceStack.ExportRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	provider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("direct-target")
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "direct-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
	}, provider)

	result, err := service.ImportStackRecords(context.Background(), "direct-operator", "DIRECT-CATALOG", reverseStackRecords(records))
	if err != nil || result.Status != exchange.StatusCompleted || result.Created != 2 || result.Unchanged != 0 || result.Holding != 0 || result.Replay || len(result.Records) != 2 {
		t.Fatalf("unexpected durable direct import %#v err=%v", result, err)
	}
	if !strings.HasPrefix(result.PackageID, "stack-import-") || result.PackageID != strings.ToLower(result.PackageID) {
		t.Fatalf("direct import package identity was not deterministic and stable: %q", result.PackageID)
	}
	for _, key := range []string{"stack.product:direct-product", "stack.version:direct-version"} {
		locked := ownership.values[key]
		if locked.OrganizationID != "direct-target" || locked.SourceSystemID != "direct-catalog" || locked.SourceRecordID != key || !locked.WriteLocked {
			t.Fatalf("direct import did not establish target-scoped ownership for %s: %#v", key, locked)
		}
	}
	snapshot, err := targetStack.Snapshot(context.Background())
	if err != nil || len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 || snapshot.Products[0].OrganizationID != "direct-target" || snapshot.Versions[0].OrganizationID != "direct-target" {
		t.Fatalf("direct import escaped target organization: %#v err=%v", snapshot, err)
	}

	replay, err := service.ImportStackRecords(context.Background(), "retry-operator", "direct-catalog", records)
	if err != nil || !replay.Replay || replay.PackageID != result.PackageID || replay.Created != 2 || len(replay.Records) != 2 {
		t.Fatalf("exact direct import did not replay the same receipt: %#v err=%v", replay, err)
	}
	if snapshot, err = targetStack.Snapshot(context.Background()); err != nil || len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 {
		t.Fatalf("exact retry duplicated Stack records: %#v err=%v", snapshot, err)
	}
	changed := append([]stack.ExchangeRecord(nil), records...)
	for index := range changed {
		changed[index].Dependencies = append([]string{}, changed[index].Dependencies...)
		changed[index].Payload = append(json.RawMessage(nil), changed[index].Payload...)
		if changed[index].Type == "stack.product" {
			var payload map[string]any
			if err := json.Unmarshal(changed[index].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload["name"] = "Conflicting durable product"
			changed[index].Payload, _ = json.Marshal(payload)
		}
	}
	if _, normalizeErr := stack.NormalizeImportRecords("direct-catalog", changed); normalizeErr != nil {
		t.Fatalf("changed fixture stopped being a valid Stack envelope: records=%#v err=%v", changed, normalizeErr)
	}
	conflicted, err := service.ImportStackRecords(context.Background(), "retry-operator", "direct-catalog", changed)
	if !errors.Is(err, exchange.ErrConflict) || conflicted.Status != exchange.StatusFailed || conflicted.PackageID == result.PackageID || conflicted.Created != 0 {
		t.Fatalf("changed replay did not fail under a distinct disclosed receipt: %#v err=%v", conflicted, err)
	}
	if snapshot, err = targetStack.Snapshot(context.Background()); err != nil || snapshot.Products[0].Name != "Durable product" || len(snapshot.Versions) != 1 {
		t.Fatalf("changed replay mutated target data: %#v err=%v", snapshot, err)
	}
}

func TestDirectStackImportPreservesHigherSourceRevision(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 2, 30, 0, time.UTC)
	source, err := stack.NewService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "revision-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := source.CreateProduct(context.Background(), stack.CreateProductInput{ID: "revision-product", Name: "Revision product", Publisher: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	product, err = source.UpdateProductStatus(context.Background(), stack.UpdateProductStatusInput{ID: product.ID, Status: "retired", Revision: product.Revision})
	if err != nil || product.Revision != 2 {
		t.Fatalf("seed higher source revision: product=%#v err=%v", product, err)
	}
	records, err := source.ExportRecords(context.Background())
	if err != nil || len(records) != 1 || records[0].Revision != 2 {
		t.Fatalf("export higher source revision: %#v err=%v", records, err)
	}

	target, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "revision-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	provider := newExchangeStackProvider(t, target)
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("revision-target"), exchange.ServiceConfig{
		OrganizationID: "revision-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
	}, provider)
	result, err := service.ImportStackRecords(context.Background(), "operator", "revision-source", records)
	if err != nil || result.Status != exchange.StatusCompleted || result.Created != 1 || len(result.Records) != 1 || result.Records[0].Revision != 2 {
		t.Fatalf("higher revision direct import failed: %#v err=%v", result, err)
	}
	snapshot, err := target.Snapshot(context.Background())
	if err != nil || len(snapshot.Products) != 1 || snapshot.Products[0].Revision != 2 || snapshot.Products[0].Status != "retired" {
		t.Fatalf("higher source revision was not persisted exactly: %#v err=%v", snapshot, err)
	}
	replay, err := service.ImportStackRecords(context.Background(), "retry-operator", "revision-source", records)
	if err != nil || !replay.Replay || replay.PackageID != result.PackageID || replay.Created != 1 {
		t.Fatalf("higher revision replay drifted: %#v err=%v", replay, err)
	}
}

func TestDirectStackImportPinsLegacySchemaAcrossBuiltInEvolution(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 2, 45, 0, time.UTC)
	records := directProductRecords(t, now)
	target, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "schema-pin-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	schemas := &evolvingStackSchemas{base: newExchangePatterns(t), activeVersion: 1}
	service, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("schema-pin-target"), exchange.ServiceConfig{
		OrganizationID: "schema-pin-target", SourceSystemID: "target-system", Schemas: schemas, Now: func() time.Time { return now },
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || first.Status != exchange.StatusCompleted || first.Created != 1 || first.Replay {
		t.Fatalf("initial pinned-schema import failed: %#v err=%v", first, err)
	}

	// Simulate a deployment in which the generic active-schema resolver now
	// selects built-in v2. The schema-less legacy Stack envelope must still
	// regenerate its v1 archive and resume the original receipt.
	schemas.activeVersion = 2
	replayed, err := service.ImportStackRecords(context.Background(), "retry-operator", "direct-source", records)
	if err != nil || !replayed.Replay || replayed.PackageID != first.PackageID || replayed.Created != 1 {
		t.Fatalf("built-in evolution changed exact retry identity: first=%#v replay=%#v err=%v", first, replayed, err)
	}
	if schemas.activeCalls != 0 {
		t.Fatalf("legacy Stack import consulted mutable active schema selection %d times", schemas.activeCalls)
	}
}

func TestDirectStackImportDisclosesAndResumesMidBatchStoreFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 3, 0, 0, time.UTC)
	records := directProductVersionRecords(t, now)
	domainStore := &failStackCreateStore{Store: repository.NewMemoryStackStore(), failVersionOnce: true}
	targetStack, _ := newExchangeStackService(domainStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-store-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("direct-store-target")
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "direct-store-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.PackageID == "" || failed.Created != 1 || len(failed.Records) != 1 || failed.Records[0].ID != "direct-product" {
		t.Fatalf("mid-batch store failure did not disclose its committed prefix: %#v err=%v", failed, err)
	}
	if _, ok := ownership.values["stack.product:direct-product"]; !ok {
		t.Fatalf("committed product ownership was lost: %#v", ownership.values)
	}
	if _, ok := ownership.values["stack.version:direct-version"]; ok {
		t.Fatalf("failed version retained a phantom ownership lock: %#v", ownership.values)
	}
	snapshot, _ := targetStack.Snapshot(context.Background())
	if len(snapshot.Products) != 1 || len(snapshot.Versions) != 0 {
		t.Fatalf("store failure wrote an undisclosed domain suffix: %#v", snapshot)
	}

	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 2 || recovered.Replay {
		t.Fatalf("exact retry did not resume the durable store failure: %#v err=%v", recovered, err)
	}
	snapshot, _ = targetStack.Snapshot(context.Background())
	if len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 || len(ownership.values) != 2 {
		t.Fatalf("recovery was not idempotent: snapshot=%#v ownership=%#v", snapshot, ownership.values)
	}
}

func TestDirectStackImportRetainsIdentityAcrossReceiptReservationFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 3, 30, 0, time.UTC)
	records := directProductVersionRecords(t, now)
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-receipt-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("direct-receipt-target")
	receipts := &failOnceExchangeReceiptStore{Store: repository.NewMemoryExchangeStore(), failCreateOnce: true}
	service, _ := exchange.NewService(receipts, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "direct-receipt-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.PackageID == "" || failed.Status != exchange.StatusProcessing || failed.ErrorCode != "receipt_read_failed" || failed.Created != 0 || len(failed.Records) != 0 {
		t.Fatalf("receipt reservation failure did not expose its deterministic retry identity: %#v err=%v", failed, err)
	}
	if snapshot, snapshotErr := targetStack.Snapshot(context.Background()); snapshotErr != nil || len(snapshot.Products) != 0 || len(snapshot.Versions) != 0 || len(ownership.values) != 0 {
		t.Fatalf("failed receipt reservation reached ownership or provider mutation: snapshot=%#v ownership=%#v err=%v", snapshot, ownership.values, snapshotErr)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 2 {
		t.Fatalf("exact retry after receipt recovery was not deterministic: %#v err=%v", recovered, err)
	}
}

func TestDirectStackImportReportsCommittedPrefixWhenReceiptCheckpointIsStale(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 3, 45, 0, time.UTC)
	records := directProductRecords(t, now)
	target, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stale-receipt-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	baseReceipts := repository.NewMemoryExchangeStore()
	receipts := &failAfterExchangeReceiptStore{Store: baseReceipts, failFromUpdate: 3, enabled: true}
	ownership := newExchangeOwnership("stale-receipt-target")
	service, _ := exchange.NewService(receipts, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stale-receipt-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 1 || len(failed.Records) != 1 || failed.Records[0].Status != exchange.OutcomeCreated {
		t.Fatalf("stale receipt checkpoint hid its committed provider result: %#v err=%v", failed, err)
	}
	history, historyErr := service.ListPackages(context.Background(), 25)
	if historyErr != nil || len(history) != 1 || history[0].Status != exchange.StatusProcessing || history[0].CreatedCount != 0 {
		t.Fatalf("test did not preserve a readable stale checkpoint: %#v err=%v", history, historyErr)
	}
	snapshot, _ := target.Snapshot(context.Background())
	if len(snapshot.Products) != 1 || !ownership.values["stack.product:direct-product"].WriteLocked {
		t.Fatalf("committed prefix was not actually durable: snapshot=%#v ownership=%#v", snapshot, ownership.values)
	}

	receipts.enabled = false
	now = history[0].UpdatedAt.Add(exchange.ProcessingLease)
	recovered, err := service.ImportStackRecords(context.Background(), "retry-operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 1 {
		t.Fatalf("stale receipt checkpoint did not resume exactly: %#v err=%v", recovered, err)
	}
	snapshot, _ = target.Snapshot(context.Background())
	if len(snapshot.Products) != 1 {
		t.Fatalf("stale receipt recovery duplicated provider data: %#v", snapshot)
	}
}

func TestDirectStackImportReconcilesStackWriteThatReturnsAnError(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 3, 50, 0, time.UTC)
	records := directProductRecords(t, now)
	domainStore := &writeThenErrorStackStore{Store: repository.NewMemoryStackStore(), failProductOnce: true}
	target, _ := newExchangeStackService(domainStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "ambiguous-stack-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	ownership := newExchangeOwnership("ambiguous-stack-target")
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "ambiguous-stack-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 1 || len(failed.Records) != 1 ||
		failed.Records[0].Status != exchange.OutcomeCreated || len(failed.PendingOwnership) != 0 {
		t.Fatalf("Stack write-then-error was not reconciled as committed: %#v err=%v", failed, err)
	}
	snapshot, snapshotErr := target.Snapshot(context.Background())
	if snapshotErr != nil || len(snapshot.Products) != 1 || !ownership.values["stack.product:direct-product"].WriteLocked {
		t.Fatalf("Stack write-then-error lost its row or ownership fence: snapshot=%#v ownership=%#v err=%v", snapshot, ownership.values, snapshotErr)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "retry-operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 1 {
		t.Fatalf("Stack write-then-error did not repair exactly: %#v err=%v", recovered, err)
	}
	snapshot, _ = target.Snapshot(context.Background())
	if len(snapshot.Products) != 1 {
		t.Fatalf("Stack write-then-error recovery duplicated data: %#v", snapshot)
	}
}

func TestDirectStackImportDisclosesCommittedAuditFailureAndRepairsDeterministically(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 4, 0, 0, time.UTC)
	records := directProductVersionRecords(t, now)
	auditor := &failNthExchangeAuditor{failAt: 2}
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, auditor, stack.ServiceConfig{
		OrganizationID: "direct-audit-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("direct-audit-target")
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "direct-audit-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 2 || len(failed.Records) != 2 || len(ownership.values) != 2 {
		t.Fatalf("post-commit audit failure was not truthful: %#v ownership=%#v err=%v", failed, ownership.values, err)
	}
	snapshot, _ := targetStack.Snapshot(context.Background())
	if len(snapshot.Products) != 1 || len(snapshot.Versions) != 1 {
		t.Fatalf("committed audit failure was not reflected in Stack: %#v", snapshot)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 2 {
		t.Fatalf("audit repair did not resume: %#v err=%v", recovered, err)
	}
	if len(auditor.attempts) != 3 || auditor.attempts[1].ID != auditor.attempts[2].ID || auditor.attempts[1].CorrelationID != auditor.attempts[2].CorrelationID {
		t.Fatalf("audit repair did not reuse the exact event identity: %#v", auditor.attempts)
	}
}

func TestDirectStackImportRepairsCompletedExchangeAuditOnExactReplay(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 4, 30, 0, time.UTC)
	records := directProductRecords(t, now)
	target, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "terminal-audit-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	auditor := &failOnceExchangeAuditor{failures: 1}
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), auditor, newExchangeOwnership("terminal-audit-target"), exchange.ServiceConfig{
		OrganizationID: "terminal-audit-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	completed, err := service.ImportStackRecords(context.Background(), "original-operator", "direct-source", records)
	if err == nil || completed.Status != exchange.StatusCompleted || completed.Created != 1 || len(completed.Records) != 1 {
		t.Fatalf("terminal audit failure obscured completed import truth: %#v err=%v", completed, err)
	}
	replayed, err := service.ImportStackRecords(context.Background(), "different-retry-operator", "direct-source", records)
	if err != nil || !replayed.Replay || replayed.Status != exchange.StatusCompleted || replayed.PackageID != completed.PackageID || replayed.Created != 1 {
		t.Fatalf("completed terminal audit did not repair on exact replay: %#v err=%v", replayed, err)
	}
	if len(auditor.attempts) != 2 || auditor.attempts[0].ID != auditor.attempts[1].ID ||
		auditor.attempts[0].CorrelationID != auditor.attempts[1].CorrelationID || auditor.attempts[1].ActorID != "original-operator" ||
		!auditor.attempts[0].OccurredAt.Equal(auditor.attempts[1].OccurredAt) {
		t.Fatalf("terminal audit repair did not replay the immutable event: %#v", auditor.attempts)
	}
	snapshot, _ := target.Snapshot(context.Background())
	if len(snapshot.Products) != 1 {
		t.Fatalf("terminal audit replay duplicated provider data: %#v", snapshot)
	}
}

func TestDirectStackImportDisclosesAndResumesOwnershipFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 5, 0, 0, time.UTC)
	records := directProductVersionRecords(t, now)
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-owner-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, targetStack)
	ownership := &failNthExchangeOwnership{exchangeOwnership: newExchangeOwnership("direct-owner-target"), failAt: 2}
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "direct-owner-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 1 || len(failed.Records) != 1 || len(ownership.values) != 1 {
		t.Fatalf("ownership failure did not disclose the committed prefix: %#v ownership=%#v err=%v", failed, ownership.values, err)
	}
	snapshot, _ := targetStack.Snapshot(context.Background())
	if len(snapshot.Products) != 1 || len(snapshot.Versions) != 0 {
		t.Fatalf("ownership failure reached the unowned provider mutation: %#v", snapshot)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 2 || len(ownership.values) != 2 {
		t.Fatalf("ownership retry did not resume safely: %#v ownership=%#v err=%v", recovered, ownership.values, err)
	}
}

func TestDirectStackImportDisclosesOwnershipWhenCompensationFails(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 5, 30, 0, time.UTC)
	records := directProductRecords(t, now)
	domainStore := &failStackCreateStore{Store: repository.NewMemoryStackStore(), failProductOnce: true}
	target, _ := newExchangeStackService(domainStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "compensation-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	ownership := &failDeleteExchangeOwnership{exchangeOwnership: newExchangeOwnership("compensation-target"), failures: 1}
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "compensation-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 0 || len(failed.Records) != 0 || len(failed.PendingOwnership) != 1 ||
		failed.PendingOwnership[0].Type != "stack.product" || failed.PendingOwnership[0].ID != "direct-product" || !failed.PendingOwnership[0].WriteLocked {
		t.Fatalf("failed ownership compensation was not disclosed: %#v err=%v", failed, err)
	}
	if snapshot, snapshotErr := target.Snapshot(context.Background()); snapshotErr != nil || len(snapshot.Products) != 0 ||
		!ownership.values["stack.product:direct-product"].WriteLocked {
		t.Fatalf("compensation fixture did not retain only the Guard fence: snapshot=%#v ownership=%#v err=%v", snapshot, ownership.values, snapshotErr)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 1 || len(recovered.PendingOwnership) != 0 {
		t.Fatalf("failed ownership compensation did not recover exactly: %#v err=%v", recovered, err)
	}
}

func TestDirectStackImportReconcilesGuardAuditAndRollbackFailure(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 5, 45, 0, time.UTC)
	records := directProductRecords(t, now)
	target, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "guard-ambiguity-target", Now: func() time.Time { return now },
	})
	provider := newExchangeStackProvider(t, target)
	baseGuardStore := repository.NewMemoryGuardStore()
	guardStore := &failDeleteGuardStore{Store: baseGuardStore, failures: 1}
	guardAuditor := &failOnceExchangeAuditor{failures: 1}
	guardService, err := guard.NewService(guardStore, exchangeTestHasher{}, guardAuditor, nil, guard.ServiceConfig{
		OrganizationID: "guard-ambiguity-target", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, guardService, exchange.ServiceConfig{
		OrganizationID: "guard-ambiguity-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, provider)

	failed, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err == nil || failed.Status != exchange.StatusFailed || failed.Created != 0 || len(failed.Records) != 0 || len(failed.PendingOwnership) != 1 ||
		failed.PendingOwnership[0].ID != "direct-product" || !failed.PendingOwnership[0].WriteLocked {
		t.Fatalf("Guard post-commit failure was not reconciled and disclosed: %#v err=%v", failed, err)
	}
	lock, lockErr := guardService.ImportedResourceOwnership(context.Background(), "guard-ambiguity-target", "stack.product", "direct-product")
	if lockErr != nil || !lock.WriteLocked || lock.SourceSystemID != "direct-source" || lock.SourceRecordID != "stack.product:direct-product" {
		t.Fatalf("Guard post-commit fixture did not retain the exact lock: %#v err=%v", lock, lockErr)
	}
	if snapshot, snapshotErr := target.Snapshot(context.Background()); snapshotErr != nil || len(snapshot.Products) != 0 {
		t.Fatalf("provider ran after ambiguous Guard registration: %#v err=%v", snapshot, snapshotErr)
	}
	recovered, err := service.ImportStackRecords(context.Background(), "operator", "direct-source", records)
	if err != nil || recovered.Status != exchange.StatusCompleted || recovered.PackageID != failed.PackageID || recovered.Created != 1 || len(recovered.PendingOwnership) != 0 {
		t.Fatalf("Guard post-commit ambiguity did not recover exactly: %#v err=%v", recovered, err)
	}
	if len(guardAuditor.attempts) != 1 {
		t.Fatalf("idempotent Guard re-registration unexpectedly emitted another lock audit: %#v", guardAuditor.attempts)
	}
}

func TestStackProviderRejectsImporterCapabilityFromAnotherService(t *testing.T) {
	first, firstImporter, err := stack.NewServiceWithExchangeImporter(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{OrganizationID: "capability-first"})
	if err != nil {
		t.Fatal(err)
	}
	_, secondImporter, err := stack.NewServiceWithExchangeImporter(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{OrganizationID: "capability-second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewStackProvider(first, secondImporter); err == nil {
		t.Fatal("Stack provider accepted an importer capability issued for another service")
	}
	if _, err := exchange.NewStackProvider(first, firstImporter); err != nil {
		t.Fatalf("Stack provider rejected its construction-issued importer capability: %v", err)
	}
}

func TestStackProviderRoundTripPreservesMaximumDocumentIdentifiers(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 5, 0, 0, time.UTC)
	sourceStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "documents-source", Now: func() time.Time { return now },
	})
	product, err := sourceStack.CreateProduct(context.Background(), stack.CreateProductInput{ID: "documents-product", Name: "Documents product", Publisher: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	documentIDs := make([]string, 100)
	for index := range documentIDs {
		prefix := fmt.Sprintf("document-%03d-", index)
		documentIDs[index] = prefix + strings.Repeat("x", 128-len(prefix))
	}
	license, err := sourceStack.CreateLicense(context.Background(), stack.CreateLicenseInput{
		ID: "documents-license", ProductID: product.ID, Name: "Document-heavy license", EntitlementMetric: "device", Quantity: 100, DocumentIDs: documentIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider := newExchangeStackProvider(t, sourceStack)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("documents-source"), exchange.ServiceConfig{
		OrganizationID: "documents-source", SourceSystemID: "documents-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "stack.license", ID: license.ID}}, IncludeDependencies: true, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exchangeManifestBytes(t, artifact.Bytes), []byte(strings.Join(documentIDs, ","))) {
		t.Fatal("maximum reversible document identifier list was not exported")
	}
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "documents-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	targetProvider := newExchangeStackProvider(t, targetStack)
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("documents-target"), exchange.ServiceConfig{
		OrganizationID: "documents-target", SourceSystemID: "documents-target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
	}, targetProvider)
	result, err := target.Import(context.Background(), "operator", artifact.Bytes)
	if err != nil || result.Package.Status != exchange.StatusCompleted {
		t.Fatalf("maximum document identifiers did not import: %#v err=%v", result, err)
	}
	snapshot, err := targetStack.Snapshot(context.Background())
	if err != nil || len(snapshot.Licenses) != 1 || !slices.Equal(snapshot.Licenses[0].DocumentIDs, documentIDs) {
		t.Fatalf("document identifier round trip drifted: %#v err=%v", snapshot.Licenses, err)
	}
}

func TestStackProviderRepairsAuditAfterCommittedImportWithoutLosingCreatedTruth(t *testing.T) {
	now := time.Date(2026, time.August, 13, 15, 10, 0, 0, time.UTC)
	sourceStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "stack-audit-source", Now: func() time.Time { return now },
	})
	product, err := sourceStack.CreateProduct(context.Background(), stack.CreateProductInput{
		ID: "audit-product", Name: "Audited product", Publisher: "Example", SourceSystemID: "catalog", SourceRecordID: "catalog:audit-product",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceProvider := newExchangeStackProvider(t, sourceStack)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("stack-audit-source"), exchange.ServiceConfig{
		OrganizationID: "stack-audit-source", SourceSystemID: "stack-audit-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "stack.product", ID: product.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	auditor := &failOnceExchangeAuditor{failures: 1}
	targetStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, auditor, stack.ServiceConfig{
		OrganizationID: "stack-audit-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	targetProvider := newExchangeStackProvider(t, targetStack)
	ownership := newExchangeOwnership("stack-audit-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "stack-audit-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
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
	sourceStack, err := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "large-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	targetStore := &countingStackStore{Store: repository.NewMemoryStackStore()}
	targetStack, err := newExchangeStackService(targetStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
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
	sourceProvider := newExchangeStackProvider(t, sourceStack)
	portable, err := sourceProvider.ListRecords(context.Background())
	if err != nil || len(portable) != records {
		t.Fatalf("unexpected source catalog size %d err=%v", len(portable), err)
	}
	targetStore.snapshotCalls, targetStore.exactReads = 0, 0
	targetProvider := newExchangeStackProvider(t, targetStack)
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
	sourceStack, _ := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
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
	sourceProvider := newExchangeStackProvider(t, sourceStack)
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("operation-source"), exchange.ServiceConfig{
		OrganizationID: "operation-source", SourceSystemID: "operation-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "export-operator", exchange.ExportRequest{
		Selection: selection, IncludeDependencies: false, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	countingStore := &countingStackStore{Store: repository.NewMemoryStackStore()}
	targetStack, _ := newExchangeStackService(countingStore, allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "operation-target", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if _, err := targetStack.CreateProduct(context.Background(), stack.CreateProductInput{
		ID: product.ID, Name: product.Name, Publisher: product.Publisher, SourceSystemID: product.SourceSystemID, SourceRecordID: product.SourceRecordID,
	}); err != nil {
		t.Fatal(err)
	}
	countingStore.snapshotCalls, countingStore.exactReads = 0, 0
	targetProvider := newExchangeStackProvider(t, targetStack)
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("operation-target"), exchange.ServiceConfig{
		OrganizationID: "operation-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
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
	service, err := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
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
	provider := newExchangeStackProvider(t, service)
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
		{Type: "ledger.purchase-order", ID: "po-one"}, {Type: "ledger.vendor", ID: "vendor-one"},
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
		OrganizationID: "vault-source", SourceSystemID: "source-appliance", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "vault-target", SourceSystemID: "target-appliance", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
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
		OrganizationID: "vault-audit-source", SourceSystemID: "vault-audit-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "vault-audit-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now.Add(time.Hour) },
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
		OrganizationID: "metadata-source", SourceSystemID: "metadata-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "metadata-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "metadata-exact-source", SourceSystemID: "metadata-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "metadata-exact-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "metadata-stale-source", SourceSystemID: "metadata-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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
		OrganizationID: "metadata-stale-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t), Now: func() time.Time { return now },
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

type failNthExchangeAuditor struct {
	failAt   int
	calls    int
	attempts []foundation.AuditEvent
}

func (a *failNthExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.calls++
	a.attempts = append(a.attempts, event)
	if a.calls == a.failAt {
		return errors.New("injected nth audit delivery failure")
	}
	return nil
}

type failStackCreateStore struct {
	stack.Store
	failProductOnce bool
	failVersionOnce bool
}

type writeThenErrorStackStore struct {
	stack.Store
	failProductOnce bool
}

func (s *writeThenErrorStackStore) CreateProduct(ctx context.Context, value stack.Product) (stack.Product, bool, error) {
	created, inserted, err := s.Store.CreateProduct(ctx, value)
	if err != nil {
		return created, inserted, err
	}
	if s.failProductOnce {
		s.failProductOnce = false
		return stack.Product{}, false, errors.New("injected ambiguous Stack store response after write")
	}
	return created, inserted, nil
}

type failOnceExchangeReceiptStore struct {
	exchange.Store
	failCreateOnce bool
}

func (s *failOnceExchangeReceiptStore) CreatePackage(ctx context.Context, value exchange.Package) (exchange.Package, bool, error) {
	if s.failCreateOnce {
		s.failCreateOnce = false
		return exchange.Package{}, false, errors.New("injected Exchange receipt reservation failure")
	}
	return s.Store.CreatePackage(ctx, value)
}

type failAfterExchangeReceiptStore struct {
	exchange.Store
	updates        int
	failFromUpdate int
	enabled        bool
}

func (s *failAfterExchangeReceiptStore) UpdatePackage(ctx context.Context, value exchange.Package, expected time.Time) (exchange.Package, error) {
	s.updates++
	if s.enabled && s.updates >= s.failFromUpdate {
		return exchange.Package{}, errors.New("injected persistent Exchange receipt checkpoint failure")
	}
	return s.Store.UpdatePackage(ctx, value, expected)
}

func (s *failStackCreateStore) CreateProduct(ctx context.Context, value stack.Product) (stack.Product, bool, error) {
	if s.failProductOnce {
		s.failProductOnce = false
		return stack.Product{}, false, errors.New("injected Stack product store failure")
	}
	return s.Store.CreateProduct(ctx, value)
}

func (s *failStackCreateStore) CreateVersion(ctx context.Context, value stack.Version) (stack.Version, bool, error) {
	if s.failVersionOnce {
		s.failVersionOnce = false
		return stack.Version{}, false, errors.New("injected mid-batch Stack store failure")
	}
	return s.Store.CreateVersion(ctx, value)
}

type failNthExchangeOwnership struct {
	*exchangeOwnership
	failAt int
	calls  int
}

type failDeleteExchangeOwnership struct {
	*exchangeOwnership
	failures int
}

func (o *failDeleteExchangeOwnership) DeleteImportedResourceOwnership(ctx context.Context, ownership guard.ResourceOwnership) error {
	if o.failures > 0 {
		o.failures--
		return errors.New("injected ownership compensation failure")
	}
	return o.exchangeOwnership.DeleteImportedResourceOwnership(ctx, ownership)
}

type failDeleteGuardStore struct {
	guard.Store
	failures int
}

func (s *failDeleteGuardStore) DeleteResourceOwnership(ctx context.Context, ownership guard.ResourceOwnership) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("injected Guard ownership rollback failure")
	}
	return s.Store.DeleteResourceOwnership(ctx, ownership)
}

type exchangeTestHasher struct{}

func (exchangeTestHasher) Hash(password string) (string, error) { return "test:" + password, nil }
func (exchangeTestHasher) Verify(password, encodedHash string) (bool, bool, error) {
	return encodedHash == "test:"+password, false, nil
}

type evolvingStackSchemas struct {
	base          *patterns.Service
	activeVersion int64
	activeCalls   int
}

func (s *evolvingStackSchemas) ActiveTemplateForRecordType(ctx context.Context, recordType string) (patterns.Template, error) {
	s.activeCalls++
	template, err := s.base.ActiveTemplateForRecordType(ctx, recordType)
	if err == nil && s.activeVersion > 0 {
		template.Version = s.activeVersion
	}
	return template, err
}

func (s *evolvingStackSchemas) GetTemplate(ctx context.Context, id string, version int64) (patterns.Template, error) {
	lookupVersion := version
	if version == 2 && strings.HasPrefix(id, "builtin-stack-") {
		lookupVersion = 1
	}
	template, err := s.base.GetTemplate(ctx, id, lookupVersion)
	if err == nil {
		template.Version = version
	}
	return template, err
}

func (s *evolvingStackSchemas) Validate(ctx context.Context, id string, version int64, input patterns.ValidationInput) (patterns.ValidationResult, error) {
	if version == 2 && strings.HasPrefix(id, "builtin-stack-") {
		version = 1
	}
	return s.base.Validate(ctx, id, version, input)
}

func (o *failNthExchangeOwnership) RegisterImportedResourceOwnership(ctx context.Context, actorID string, input guard.ResourceOwnershipInput) (guard.ResourceOwnership, bool, error) {
	o.calls++
	if o.calls == o.failAt {
		return guard.ResourceOwnership{}, false, errors.New("injected mid-batch ownership failure")
	}
	return o.exchangeOwnership.RegisterImportedResourceOwnership(ctx, actorID, input)
}

func directProductVersionRecords(t *testing.T, now time.Time) []stack.ExchangeRecord {
	t.Helper()
	source, err := newExchangeStackService(repository.NewMemoryStackStore(), allowExchangeStackReferences{}, foundation.NopAuditor{}, stack.ServiceConfig{
		OrganizationID: "direct-fixture-source", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := source.CreateProduct(context.Background(), stack.CreateProductInput{ID: "direct-product", Name: "Durable product", Publisher: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateVersion(context.Background(), stack.CreateVersionInput{ID: "direct-version", ProductID: product.ID, Name: "1.0"}); err != nil {
		t.Fatal(err)
	}
	records, err := source.ExportRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func directProductRecords(t *testing.T, now time.Time) []stack.ExchangeRecord {
	t.Helper()
	for _, record := range directProductVersionRecords(t, now) {
		if record.Type == "stack.product" {
			return []stack.ExchangeRecord{record}
		}
	}
	t.Fatal("direct product fixture did not export its product")
	return nil
}

func reverseStackRecords(values []stack.ExchangeRecord) []stack.ExchangeRecord {
	result := append([]stack.ExchangeRecord(nil), values...)
	slices.Reverse(result)
	return result
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
