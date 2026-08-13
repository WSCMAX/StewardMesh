package exchange_test

// Requirement: REQ-EXCHANGE-001. Feature: migration.packages. GitHub: #9.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestServiceExportsDependencyClosureImportsInOrderAndReplaysExactly(t *testing.T) {
	ctx := context.Background()
	sourceProvider := &exchangeTestProvider{types: []string{"test.parent", "test.child"}, records: []exchange.Record{
		testRecord("test.child", "child-one", []exchange.Reference{{Type: "test.parent", ID: "parent-one"}}),
		testRecord("test.parent", "parent-one", []exchange.Reference{}),
	}}
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership(), exchange.ServiceConfig{
		OrganizationID: "source-org", SourceSystemID: "steward-source", Now: fixedExchangeNow,
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(ctx, "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.child", ID: "child-one"}}, IncludeDependencies: true, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Export(ctx, "source-operator", exchange.ExportRequest{
		Selection:           []exchange.Reference{{Type: "test.child", ID: "child-one"}, {Type: "test.child", ID: "child-one"}},
		IncludeDependencies: true, FileMode: exchange.FileModeMetadata,
	}); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected duplicate selection rejection, got %v", err)
	}

	targetProvider := &exchangeTestProvider{types: []string{"test.parent", "test.child"}, exists: make(map[string]bool)}
	ownership := newExchangeOwnership("target-org")
	targetStore := repository.NewMemoryExchangeStore()
	target, err := exchange.NewService(targetStore, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "target-org", SourceSystemID: "steward-target", Now: fixedExchangeNow,
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	result, err := target.Import(ctx, "target-operator", artifact.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replay || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 2 || result.Package.HoldingCount != 0 {
		t.Fatalf("unexpected import result %#v", result)
	}
	if strings.Join(targetProvider.imported, ",") != "test.parent:parent-one,test.child:child-one" {
		t.Fatalf("dependencies were not imported first: %#v", targetProvider.imported)
	}
	for _, key := range targetProvider.imported {
		locked := ownership.values[key]
		if !locked.WriteLocked || locked.SourceSystemID != "steward-source" {
			t.Fatalf("imported record was not source-preserving and locked: %#v", locked)
		}
	}
	replayed, err := target.Import(ctx, "target-operator", artifact.Bytes)
	if err != nil || !replayed.Replay || replayed.Package.CreatedCount != 2 || len(targetProvider.imported) != 2 {
		t.Fatalf("exact import replay was not unchanged: %#v err=%v calls=%#v", replayed, err, targetProvider.imported)
	}
	history, err := target.ListPackages(ctx, 25)
	if err != nil || len(history) != 1 || history[0].ArchiveSHA256 != artifact.SHA256 {
		t.Fatalf("unexpected durable history %#v err=%v", history, err)
	}
}

func TestServiceKeepsMissingProvidersReferencesAndMetadataOnlyFilesVisibleInHolding(t *testing.T) {
	record := testRecord("test.child", "held-one", []exchange.Reference{{Type: "unknown.record", ID: "missing-one"}})
	record.File = &exchange.FileMetadata{
		Mode: exchange.FileModeMetadata, Name: "evidence.txt", MediaType: "text/plain", SizeBytes: 4, SHA256: strings.Repeat("a", 64),
	}
	sourceProvider := &exchangeTestProvider{types: []string{"test.child"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("holding-source"), exchange.ServiceConfig{
		OrganizationID: "holding-source", SourceSystemID: "holding-system", Now: fixedExchangeNow,
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.child", ID: "held-one"}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider := &exchangeTestProvider{types: []string{"test.child"}, exists: make(map[string]bool)}
	ownership := newExchangeOwnership("holding-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "holding-target", SourceSystemID: "target-system", Now: fixedExchangeNow,
	}, targetProvider)
	result, err := target.Import(context.Background(), "target-operator", artifact.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if result.Package.Status != exchange.StatusHolding || result.Package.HoldingCount != 1 || len(result.Package.Records) != 1 ||
		len(result.Package.Records[0].MissingDependencies) != 2 || len(targetProvider.imported) != 0 || len(ownership.values) != 0 {
		t.Fatalf("missing dependencies were not held visibly: %#v imported=%#v ownership=%#v", result, targetProvider.imported, ownership.values)
	}
}

func TestServiceRetriesHoldingWhenDependenciesBecomeAvailable(t *testing.T) {
	record := testRecord("test.child", "retry-held", []exchange.Reference{{Type: "test.parent", ID: "later-parent"}})
	sourceProvider := &exchangeTestProvider{types: []string{"test.child"}, records: []exchange.Record{record}}
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("hold-retry-source"), exchange.ServiceConfig{
		OrganizationID: "hold-retry-source", SourceSystemID: "hold-retry-system", Now: fixedExchangeNow,
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.child", ID: "retry-held"}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider := &exchangeTestProvider{
		types: []string{"test.child", "test.parent"}, exists: map[string]bool{"test.parent:later-parent": false},
	}
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("hold-retry-target"), exchange.ServiceConfig{
		OrganizationID: "hold-retry-target", SourceSystemID: "target-system", Now: fixedExchangeNow,
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	first, err := target.Import(context.Background(), "target-operator", artifact.Bytes)
	if err != nil || first.Replay || first.Package.Status != exchange.StatusHolding || first.Package.HoldingCount != 1 {
		t.Fatalf("expected initial holding result, got %#v err=%v", first, err)
	}
	targetProvider.exists["test.parent:later-parent"] = true
	retried, err := target.Import(context.Background(), "target-operator", artifact.Bytes)
	if err != nil || retried.Replay || retried.Package.Status != exchange.StatusCompleted || retried.Package.CreatedCount != 1 {
		t.Fatalf("expected resolved holding retry, got %#v err=%v", retried, err)
	}
	replayed, err := target.Import(context.Background(), "target-operator", artifact.Bytes)
	if err != nil || !replayed.Replay || len(targetProvider.imported) != 1 {
		t.Fatalf("completed package did not become an exact replay: %#v err=%v imports=%#v", replayed, err, targetProvider.imported)
	}
}

func TestServiceRetriesFailedPackageAndPreservesClaimedOwnership(t *testing.T) {
	record := testRecord("test.record", "record-one", []exchange.Reference{})
	record.Provenance = exchange.Provenance{SourceSystemID: "original-system", SourceRecordID: "original-record"}
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("retry-source"), exchange.ServiceConfig{
		OrganizationID: "retry-source", SourceSystemID: "immediate-source", Now: fixedExchangeNow,
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: "record-one"}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider := &exchangeTestProvider{types: []string{"test.record"}, exists: make(map[string]bool), failures: 1}
	ownership := newExchangeOwnership("retry-target")
	store := repository.NewMemoryExchangeStore()
	target, _ := exchange.NewService(store, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "retry-target", SourceSystemID: "target-system", Now: fixedExchangeNow,
	}, targetProvider)
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); err == nil {
		t.Fatal("expected first provider attempt to fail")
	}
	history, _ := target.ListPackages(context.Background(), 25)
	if len(history) != 1 || history[0].Status != exchange.StatusFailed || history[0].ErrorCode != "import_failed" || len(ownership.values) != 0 {
		t.Fatalf("failed import did not compensate its ownership lock: %#v ownership=%#v", history, ownership.values)
	}
	result, err := target.Import(context.Background(), "target-operator", artifact.Bytes)
	if err != nil || result.Replay || result.Package.Status != exchange.StatusCompleted || result.Package.CreatedCount != 1 {
		t.Fatalf("failed package did not retry: %#v err=%v", result, err)
	}
	locked := ownership.values["test.record:record-one"]
	if locked.SourceSystemID != "original-system" || locked.SourceRecordID != "original-record" || !locked.WriteLocked {
		t.Fatalf("original provenance was not preserved: %#v", locked)
	}

	// A previously claimed record remains readable and locally writable; an
	// exact import never silently re-locks it.
	claimedAt := fixedExchangeNow().Add(-time.Hour)
	ownership.values["test.record:record-two"] = guard.ResourceOwnership{
		OrganizationID: "retry-target", ResourceType: "test.record", ResourceID: "record-two",
		SourceSystemID: "claimed-source", SourceRecordID: "claimed-record", RegisteredAt: claimedAt.Add(-time.Hour),
		ClaimedBy: "administrator", ClaimedAt: &claimedAt,
	}
	claimedRecord := testRecord("test.record", "record-two", []exchange.Reference{})
	claimedRecord.Provenance = exchange.Provenance{SourceSystemID: "claimed-source", SourceRecordID: "claimed-record"}
	claimedSourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{claimedRecord}}
	claimedSource, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("claimed-source-org"), exchange.ServiceConfig{
		OrganizationID: "claimed-source-org", SourceSystemID: "claimed-source", Now: fixedExchangeNow,
	}, claimedSourceProvider)
	claimedArtifact, err := claimedSource.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: "record-two"}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProvider.exists["test.record:record-two"] = true
	claimedResult, err := target.Import(context.Background(), "target-operator", claimedArtifact.Bytes)
	if err != nil || claimedResult.Package.Status != exchange.StatusCompleted || claimedResult.Package.UnchangedCount != 1 ||
		claimedResult.Package.Records[0].WriteLocked {
		t.Fatalf("claimed ownership was unexpectedly re-locked: %#v err=%v", claimedResult, err)
	}
	if ownership.values["test.record:record-two"].WriteLocked {
		t.Fatalf("claimed ownership state was changed: %#v", ownership.values["test.record:record-two"])
	}
}

type exchangeTestProvider struct {
	types    []string
	records  []exchange.Record
	exists   map[string]bool
	imported []string
	failures int
}

func (p *exchangeTestProvider) Types() []string { return append([]string(nil), p.types...) }
func (p *exchangeTestProvider) ListRecords(context.Context) ([]exchange.Record, error) {
	result := make([]exchange.Record, len(p.records))
	copy(result, p.records)
	return result, nil
}
func (p *exchangeTestProvider) Exists(_ context.Context, reference exchange.Reference) (bool, error) {
	return p.exists[reference.Key()], nil
}
func (p *exchangeTestProvider) ImportRecord(_ context.Context, _ string, record exchange.Record, _ []byte) (bool, error) {
	if p.failures > 0 {
		p.failures--
		return false, errors.New("transient provider failure")
	}
	key := exchange.Reference{Type: record.Type, ID: record.ID}.Key()
	if p.exists == nil {
		p.exists = make(map[string]bool)
	}
	if p.exists[key] {
		return false, nil
	}
	p.exists[key] = true
	p.imported = append(p.imported, key)
	return true, nil
}

type exchangeOwnership struct {
	organizationID string
	values         map[string]guard.ResourceOwnership
}

func newExchangeOwnership(organizationID ...string) *exchangeOwnership {
	value := "source-org"
	if len(organizationID) > 0 {
		value = organizationID[0]
	}
	return &exchangeOwnership{organizationID: value, values: make(map[string]guard.ResourceOwnership)}
}
func (o *exchangeOwnership) ImportedResourceOwnership(_ context.Context, organizationID, resourceType, resourceID string) (guard.ResourceOwnership, error) {
	value, ok := o.values[exchange.Reference{Type: resourceType, ID: resourceID}.Key()]
	if !ok || value.OrganizationID != organizationID {
		return guard.ResourceOwnership{}, guard.ErrNotFound
	}
	return value, nil
}
func (o *exchangeOwnership) RegisterImportedResourceOwnership(_ context.Context, _ string, input guard.ResourceOwnershipInput) (guard.ResourceOwnership, bool, error) {
	key := exchange.Reference{Type: input.ResourceType, ID: input.ResourceID}.Key()
	if existing, ok := o.values[key]; ok {
		if existing.SourceSystemID != input.SourceSystemID || existing.SourceRecordID != input.SourceRecordID {
			return guard.ResourceOwnership{}, false, guard.ErrConflict
		}
		return existing, false, nil
	}
	value := guard.ResourceOwnership{
		OrganizationID: o.organizationID, ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID, WriteLocked: true, RegisteredAt: fixedExchangeNow(),
	}
	o.values[key] = value
	return value, true, nil
}
func (o *exchangeOwnership) DeleteImportedResourceOwnership(_ context.Context, ownership guard.ResourceOwnership) error {
	delete(o.values, exchange.Reference{Type: ownership.ResourceType, ID: ownership.ResourceID}.Key())
	return nil
}

func testRecord(recordType, id string, dependencies []exchange.Reference) exchange.Record {
	payload, _ := json.Marshal(map[string]any{"id": id, "revision": 1, "name": "Portable " + id})
	return exchange.Record{Type: recordType, ID: id, Revision: 1, Dependencies: dependencies, Ownership: exchange.OwnershipMetadata{State: "local"}, Payload: payload}
}

func fixedExchangeNow() time.Time { return time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC) }
