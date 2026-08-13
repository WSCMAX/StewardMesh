package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-PATTERNS-001. Features: migration.packages, templates.schemas. GitHub: #9, #8.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/patterns"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

func TestServiceExportsDependencyClosureImportsInOrderAndReplaysExactly(t *testing.T) {
	ctx := context.Background()
	sourceProvider := &exchangeTestProvider{types: []string{"test.parent", "test.child"}, records: []exchange.Record{
		testRecord("test.child", "child-one", []exchange.Reference{{Type: "test.parent", ID: "parent-one"}}),
		testRecord("test.parent", "parent-one", []exchange.Reference{}),
	}}
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership(), exchange.ServiceConfig{
		OrganizationID: "source-org", SourceSystemID: "steward-source", Schemas: newExchangePatterns(t, "test.parent", "test.child"), Now: fixedExchangeNow,
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
		OrganizationID: "target-org", SourceSystemID: "steward-target", Schemas: newExchangePatterns(t, "test.parent", "test.child"), Now: fixedExchangeNow,
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
		OrganizationID: "holding-source", SourceSystemID: "holding-system", Schemas: newExchangePatterns(t, "test.child"), Now: fixedExchangeNow,
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
		OrganizationID: "holding-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.child"), Now: fixedExchangeNow,
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
		OrganizationID: "hold-retry-source", SourceSystemID: "hold-retry-system", Schemas: newExchangePatterns(t, "test.child", "test.parent"), Now: fixedExchangeNow,
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
		OrganizationID: "hold-retry-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.child", "test.parent"), Now: fixedExchangeNow,
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
		OrganizationID: "retry-source", SourceSystemID: "immediate-source", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
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
		OrganizationID: "retry-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
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
		OrganizationID: "claimed-source-org", SourceSystemID: "claimed-source", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
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

func TestServiceTakesOverStaleProcessingReceiptAfterFinalUpdateFailure(t *testing.T) {
	now := fixedExchangeNow()
	record := testRecord("test.record", "leased-record", []exchange.Reference{})
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("lease-source"), exchange.ServiceConfig{
		OrganizationID: "lease-source", SourceSystemID: "lease-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: "leased-record"}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	baseStore := repository.NewMemoryExchangeStore()
	store := &terminalFailureExchangeStore{Store: baseStore, failTerminal: true}
	targetProvider := &exchangeTestProvider{types: []string{"test.record"}, exists: make(map[string]bool)}
	target, _ := exchange.NewService(store, foundation.NopAuditor{}, newExchangeOwnership("lease-target"), exchange.ServiceConfig{
		OrganizationID: "lease-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return now },
	}, targetProvider)
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); err == nil {
		t.Fatal("expected the injected terminal receipt update failure")
	}
	history, err := target.ListPackages(context.Background(), 25)
	if err != nil || len(history) != 1 || history[0].Status != exchange.StatusProcessing ||
		history[0].CreatedCount != 1 || len(history[0].Records) != 1 {
		t.Fatalf("successful provider work was not checkpointed before terminal failure: %#v err=%v", history, err)
	}
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); !errors.Is(err, exchange.ErrConflict) {
		t.Fatalf("active processing lease was not protected, got %v", err)
	}

	now = history[0].UpdatedAt.Add(exchange.ProcessingLease)
	recovered, err := target.Import(context.Background(), "restart-operator", artifact.Bytes)
	if err != nil || recovered.Replay || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 1 {
		t.Fatalf("stale processing receipt was not recovered: %#v err=%v", recovered, err)
	}
	if len(targetProvider.imported) != 1 || targetProvider.imported[0] != "test.record:leased-record" {
		t.Fatalf("checkpointed provider work was repeated during takeover: %#v", targetProvider.imported)
	}
}

func TestServicePersistsPartialOutcomesAndResumesAfterSecondRecordFailure(t *testing.T) {
	records := []exchange.Record{
		testRecord("test.record", "record-a", []exchange.Reference{}),
		testRecord("test.record", "record-b", []exchange.Reference{}),
	}
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: records}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("saga-source"), exchange.ServiceConfig{
		OrganizationID: "saga-source", SourceSystemID: "saga-system", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: "record-a"}, {Type: "test.record", ID: "record-b"}},
		FileMode:  exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	store := repository.NewMemoryExchangeStore()
	provider := &exchangeTestProvider{types: []string{"test.record"}, exists: make(map[string]bool), failOnCall: 2}
	ownership := newExchangeOwnership("saga-target")
	target, _ := exchange.NewService(store, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "saga-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
	}, provider)
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); err == nil {
		t.Fatal("expected the second provider write to fail")
	}
	history, err := target.ListPackages(context.Background(), 25)
	if err != nil || len(history) != 1 || history[0].Status != exchange.StatusFailed ||
		history[0].CreatedCount != 1 || len(history[0].Records) != 1 || history[0].Records[0].ID != "record-a" {
		t.Fatalf("failed receipt did not expose its durable first outcome: %#v err=%v", history, err)
	}
	if _, exists := ownership.values["test.record:record-b"]; exists {
		t.Fatalf("failed second record retained a new ownership lock: %#v", ownership.values)
	}

	recovered, err := target.Import(context.Background(), "retry-operator", artifact.Bytes)
	if err != nil || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 2 {
		t.Fatalf("partial import did not resume to completion: %#v err=%v", recovered, err)
	}
	if strings.Join(provider.imported, ",") != "test.record:record-a,test.record:record-b" || provider.calls != 3 {
		t.Fatalf("retry did not preserve completed provider work: imported=%#v calls=%d", provider.imported, provider.calls)
	}
}

func TestServiceRetainsCommittedOutcomeAndOwnershipUntilProviderAuditRepairs(t *testing.T) {
	record := testRecord("test.record", "committed-record", []exchange.Reference{})
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("committed-source"), exchange.ServiceConfig{
		OrganizationID: "committed-source", SourceSystemID: "committed-system", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: record.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	provider := &exchangeTestProvider{types: []string{"test.record"}, exists: make(map[string]bool), committedFailures: 1}
	ownership := newExchangeOwnership("committed-target")
	target, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "committed-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: fixedExchangeNow,
	}, provider)
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); err == nil {
		t.Fatal("expected the provider's post-commit audit failure")
	}
	history, err := target.ListPackages(context.Background(), 25)
	if err != nil || len(history) != 1 || history[0].Status != exchange.StatusFailed || history[0].CreatedCount != 1 ||
		len(history[0].Records) != 1 || history[0].Records[0].Status != exchange.OutcomeCreated || len(history[0].Progress) != 1 ||
		history[0].Progress[0].Phase != "committed" {
		t.Fatalf("committed provider truth was not recoverable: %#v err=%v", history, err)
	}
	if !ownership.values["test.record:committed-record"].WriteLocked {
		t.Fatalf("committed provider failure lost its ownership fence: %#v", ownership.values)
	}

	recovered, err := target.Import(context.Background(), "retry-operator", artifact.Bytes)
	if err != nil || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 1 || len(recovered.Package.Progress) != 0 {
		t.Fatalf("provider audit repair did not finish the receipt: %#v err=%v", recovered, err)
	}
	if len(provider.imported) != 1 || provider.calls != 2 || provider.repairCalls != 1 || len(provider.operationTokens) != 2 ||
		provider.operationTokens[0] == "" || provider.operationTokens[0] != provider.operationTokens[1] {
		t.Fatalf("provider repair was not deterministically fenced: imported=%#v calls=%d repairs=%d tokens=%#v", provider.imported, provider.calls, provider.repairCalls, provider.operationTokens)
	}
}

func TestServiceRecoversCreatedTruthAfterCrashBetweenProviderWriteAndReceiptCheckpoint(t *testing.T) {
	now := fixedExchangeNow()
	record := testRecord("test.record", "crash-record", []exchange.Reference{})
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("crash-source"), exchange.ServiceConfig{
		OrganizationID: "crash-source", SourceSystemID: "crash-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return now },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: record.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	baseStore := repository.NewMemoryExchangeStore()
	store := &postWriteCrashExchangeStore{Store: baseStore, failFromUpdate: 3, enabled: true}
	provider := &exchangeTestProvider{types: []string{"test.record"}, exists: make(map[string]bool)}
	ownership := newExchangeOwnership("crash-target")
	target, _ := exchange.NewService(store, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "crash-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return now },
	}, provider)
	if _, err := target.Import(context.Background(), "first-worker", artifact.Bytes); err == nil {
		t.Fatal("expected receipt storage to disappear after the provider commit")
	}
	history, err := target.ListPackages(context.Background(), 25)
	if err != nil || len(history) != 1 || history[0].Status != exchange.StatusProcessing || history[0].CreatedCount != 0 ||
		len(history[0].Records) != 0 || len(history[0].Progress) != 1 || history[0].Progress[0].Phase != "intent" ||
		!history[0].Progress[0].ExpectedCreated || !ownership.values["test.record:crash-record"].WriteLocked {
		t.Fatalf("pre-write intent did not survive the simulated crash: history=%#v ownership=%#v err=%v", history, ownership.values, err)
	}
	if len(provider.imported) != 1 {
		t.Fatalf("provider write did not commit before the crash: %#v", provider.imported)
	}

	store.enabled = false
	now = history[0].UpdatedAt.Add(exchange.ProcessingLease)
	recovered, err := target.Import(context.Background(), "restart-worker", artifact.Bytes)
	if err != nil || recovered.Package.Status != exchange.StatusCompleted || recovered.Package.CreatedCount != 1 || recovered.Package.UnchangedCount != 0 {
		t.Fatalf("crash recovery lost the original created outcome: %#v err=%v", recovered, err)
	}
	if len(provider.imported) != 1 || provider.calls != 2 || len(provider.operationTokens) != 2 || provider.operationTokens[0] != provider.operationTokens[1] {
		t.Fatalf("crash recovery duplicated or unfenced the provider write: imported=%#v calls=%d tokens=%#v", provider.imported, provider.calls, provider.operationTokens)
	}
}

func TestServiceRenewsLeaseWhileProviderWriteIsBlocked(t *testing.T) {
	const lease = 90 * time.Millisecond
	record := testRecord("test.record", "slow-record", []exchange.Reference{})
	sourceProvider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{record}}
	source, _ := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("slow-source"), exchange.ServiceConfig{
		OrganizationID: "slow-source", SourceSystemID: "slow-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return time.Now().UTC() },
	}, sourceProvider)
	artifact, err := source.Export(context.Background(), "source-operator", exchange.ExportRequest{
		Selection: []exchange.Reference{{Type: "test.record", ID: record.ID}}, FileMode: exchange.FileModeMetadata,
	})
	if err != nil {
		t.Fatal(err)
	}

	provider := newBlockingExchangeProvider()
	store := repository.NewMemoryExchangeStore()
	ownership := newExchangeOwnership("slow-target")
	configuration := exchange.ServiceConfig{
		OrganizationID: "slow-target", SourceSystemID: "target-system", Schemas: newExchangePatterns(t, "test.record"), Now: func() time.Time { return time.Now().UTC() }, ProcessingLease: lease,
	}
	firstWorker, _ := exchange.NewService(store, foundation.NopAuditor{}, ownership, configuration, provider)
	secondWorker, _ := exchange.NewService(store, foundation.NopAuditor{}, ownership, configuration, provider)
	firstDone := make(chan error, 1)
	go func() {
		_, importErr := firstWorker.Import(context.Background(), "first-worker", artifact.Bytes)
		firstDone <- importErr
	}()
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first provider call did not start")
	}
	time.Sleep(3 * lease)

	secondDone := make(chan error, 1)
	go func() {
		_, importErr := secondWorker.Import(context.Background(), "second-worker", artifact.Bytes)
		secondDone <- importErr
	}()
	select {
	case secondErr := <-secondDone:
		if !errors.Is(secondErr, exchange.ErrConflict) {
			close(provider.release)
			<-firstDone
			t.Fatalf("second worker bypassed the renewed lease: %v", secondErr)
		}
	case <-time.After(2 * time.Second):
		close(provider.release)
		<-firstDone
		<-secondDone
		t.Fatal("second worker reached the blocked provider instead of observing the active lease")
	}
	if calls := provider.callCount(); calls != 1 {
		close(provider.release)
		<-firstDone
		t.Fatalf("two workers invoked the same provider concurrently: calls=%d", calls)
	}
	close(provider.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first worker did not finish after release: %v", err)
	}
}

func TestServicePreflightsEveryImportSchemaBeforeFirstMutation(t *testing.T) {
	ctx := context.Background()
	fields := []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
	}
	baseSchemas := newExchangePatternSet(t, map[string]patternSchemaFixture{
		"test.first":  {id: "first-schema", fields: fields},
		"test.second": {id: "second-schema", fields: fields},
	})
	validRecords := []exchange.Record{
		testRecord("test.first", "record-a", []exchange.Reference{}),
		testRecord("test.second", "record-b", []exchange.Reference{}),
	}

	validArtifact := exportTestArtifact(t, baseSchemas, validRecords)
	invalidRecords := append([]exchange.Record(nil), validRecords...)
	invalidRecords[1] = testRecord("test.second", "record-b", []exchange.Reference{})
	invalidRecords[1].Payload = json.RawMessage(`{"id":"record-b","revision":1}`)
	invalidArtifact := exportTestArtifact(t, permissiveExchangePatterns{SchemaRegistry: baseSchemas}, invalidRecords)

	tests := []struct {
		name     string
		artifact exchange.ExportArtifact
		schemas  exchange.SchemaRegistry
	}{
		{name: "later schema id mismatch", artifact: validArtifact, schemas: selectiveExchangePatterns{SchemaRegistry: baseSchemas, recordType: "test.second", mismatch: true}},
		{name: "later schema retired", artifact: validArtifact, schemas: selectiveExchangePatterns{SchemaRegistry: baseSchemas, recordType: "test.second", retired: true}},
		{name: "later typed payload invalid", artifact: invalidArtifact, schemas: baseSchemas},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseStore := repository.NewMemoryExchangeStore()
			store := &countingExchangeStore{Store: baseStore}
			provider := &exchangeTestProvider{types: []string{"test.first", "test.second"}, exists: map[string]bool{}}
			ownership := newExchangeOwnership("schema-preflight-target")
			target, err := exchange.NewService(store, foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
				OrganizationID: "schema-preflight-target", SourceSystemID: "schema-preflight-target-system", Schemas: test.schemas, Now: fixedExchangeNow,
			}, provider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := target.Import(ctx, "target-operator", test.artifact.Bytes); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected all-record schema preflight rejection, got %v", err)
			}
			if store.creates != 1 || store.updates != 1 {
				t.Fatalf("schema failure made unexpected receipt mutations: creates=%d updates=%d", store.creates, store.updates)
			}
			history, err := target.ListPackages(ctx, 25)
			if err != nil || len(history) != 1 || history[0].Status != exchange.StatusFailed || len(history[0].Records) != 0 || len(history[0].Progress) != 0 {
				t.Fatalf("schema failure did not remain an empty failed receipt: %#v err=%v", history, err)
			}
			if provider.calls != 0 || len(provider.imported) != 0 || len(ownership.values) != 0 {
				t.Fatalf("later schema failure mutated the first record: calls=%d imports=%#v ownership=%#v", provider.calls, provider.imported, ownership.values)
			}
		})
	}
}

func TestServiceRejectsFractionalMoneyBeforeGuardOrProviderMutation(t *testing.T) {
	fields := []patterns.Field{
		{Key: "currency", Label: "Currency", Type: patterns.FieldText, Required: true, MaximumLength: 3},
		{Key: "amountMinor", Label: "Amount", Type: patterns.FieldMoney, Required: true, CurrencyField: "currency"},
	}
	schemas := newExchangePatternTemplate(t, "test.money", "money-schema", fields)
	for _, token := range []string{"9007199254740990.5", "1.0000000000000001", "0.99999999999999999"} {
		t.Run(token, func(t *testing.T) {
			record := testRecord("test.money", "money-row", nil)
			record.Payload = json.RawMessage(`{"currency":"USD","amountMinor":` + token + `}`)
			sourceProvider := &exchangeTestProvider{types: []string{"test.money"}, records: []exchange.Record{record}}
			source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("money-source"), exchange.ServiceConfig{
				OrganizationID: "money-source", SourceSystemID: "money-system", Schemas: permissiveExchangePatterns{SchemaRegistry: schemas}, Now: fixedExchangeNow,
			}, sourceProvider)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := source.Export(context.Background(), "operator", exchange.ExportRequest{Selection: []exchange.Reference{{Type: "test.money", ID: "money-row"}}, FileMode: exchange.FileModeMetadata})
			if err != nil {
				t.Fatal(err)
			}
			targetProvider := &exchangeTestProvider{types: []string{"test.money"}, exists: map[string]bool{}}
			ownership := newExchangeOwnership("money-target")
			target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
				OrganizationID: "money-target", SourceSystemID: "money-target-system", Schemas: schemas, Now: fixedExchangeNow,
			}, targetProvider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := target.Import(context.Background(), "operator", artifact.Bytes); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected exact-money preflight rejection, got %v", err)
			}
			if targetProvider.calls != 0 || len(targetProvider.imported) != 0 || len(ownership.values) != 0 {
				t.Fatalf("fractional money reached mutation: calls=%d imported=%#v ownership=%#v", targetProvider.calls, targetProvider.imported, ownership.values)
			}
		})
	}
}

func TestServiceImportsPinnedOlderCustomVersionAndPreservesCanonicalHoldingDependency(t *testing.T) {
	schemas := newExchangePatternTemplate(t, "test.versioned", "versioned-schema", []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
		{Key: "purchaseOrderId", Label: "Purchase order", Type: patterns.FieldReference, ReferenceType: "ledger.purchase-order", Required: true, AllowHolding: true},
	})
	record := testRecord("test.versioned", "versioned-row", []exchange.Reference{{Type: "ledger.purchase_order", ID: "po-1"}})
	record.Payload = json.RawMessage(`{"id":"versioned-row","revision":1,"name":"Version one row","purchaseOrderId":"po-1"}`)
	sourceProvider := &exchangeTestProvider{types: []string{"test.versioned", "ledger.purchase_order"}, records: []exchange.Record{record}}
	source, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("version-source"), exchange.ServiceConfig{
		OrganizationID: "version-source", SourceSystemID: "version-system", Schemas: schemas, Now: fixedExchangeNow,
	}, sourceProvider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.Export(context.Background(), "operator", exchange.ExportRequest{Selection: []exchange.Reference{{Type: "test.versioned", ID: "versioned-row"}}, FileMode: exchange.FileModeMetadata})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schemas.CreateVersion(context.Background(), "versioned-schema", patterns.NewVersionInput{Fields: []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
		{Key: "purchaseOrderId", Label: "Purchase order", Type: patterns.FieldReference, ReferenceType: "ledger.purchase-order", Required: true, AllowHolding: true},
		{Key: "note", Label: "Note", Type: patterns.FieldText},
	}}); err != nil {
		t.Fatal(err)
	}
	targetProvider := &exchangeTestProvider{types: []string{"test.versioned", "ledger.purchase_order"}, exists: map[string]bool{}}
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("version-target"), exchange.ServiceConfig{
		OrganizationID: "version-target", SourceSystemID: "version-target-system", Schemas: schemas, Now: fixedExchangeNow,
	}, targetProvider)
	if err != nil {
		t.Fatal(err)
	}
	held, err := target.Import(context.Background(), "operator", artifact.Bytes)
	if err != nil || held.Package.Status != exchange.StatusHolding || len(held.Package.Records) != 1 || len(held.Package.Records[0].MissingDependencies) != 1 || held.Package.Records[0].MissingDependencies[0].Type != "ledger.purchase_order" {
		t.Fatalf("pinned v1 was not held with the exact manifest dependency: %#v err=%v", held, err)
	}
	targetProvider.exists["ledger.purchase_order:po-1"] = true
	completed, err := target.Import(context.Background(), "operator", artifact.Bytes)
	if err != nil || completed.Package.Status != exchange.StatusCompleted || len(targetProvider.imported) != 1 || targetProvider.imported[0] != "test.versioned:versioned-row" {
		t.Fatalf("pinned v1 did not promote after a v2 append: %#v err=%v imports=%#v", completed, err, targetProvider.imported)
	}
}

func TestServiceRejectsOmittedRequiredReferenceEvenWhenHoldingIsAllowed(t *testing.T) {
	fields := []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
		{Key: "ownerId", Label: "Owner", Type: patterns.FieldReference, ReferenceType: "test.owner", Required: true, AllowHolding: true},
	}
	schemas := newExchangePatternTemplate(t, "test.child", "holding-schema", fields)
	record := testRecord("test.child", "omitted-owner", []exchange.Reference{})
	artifact := exportTestArtifact(t, permissiveExchangePatterns{SchemaRegistry: schemas}, []exchange.Record{record})
	provider := &exchangeTestProvider{types: []string{"test.child"}, exists: map[string]bool{}}
	ownership := newExchangeOwnership("omitted-owner-target")
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "omitted-owner-target", SourceSystemID: "omitted-owner-system", Schemas: schemas, Now: fixedExchangeNow,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected omitted holding-capable reference to fail without a resolvable target, got %v", err)
	}
	if provider.calls != 0 || len(provider.imported) != 0 || len(ownership.values) != 0 {
		t.Fatalf("holding preflight reached a mutation: calls=%d imports=%#v ownership=%#v", provider.calls, provider.imported, ownership.values)
	}
}

func TestServiceRejectsOmittedRequiredReferenceWhenHoldingIsDisallowed(t *testing.T) {
	fields := []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
		{Key: "ownerId", Label: "Owner", Type: patterns.FieldReference, ReferenceType: "test.owner", Required: true},
	}
	schemas := newExchangePatternTemplate(t, "test.child", "strict-schema", fields)
	artifact := exportTestArtifact(t, permissiveExchangePatterns{SchemaRegistry: schemas}, []exchange.Record{testRecord("test.child", "missing-owner", []exchange.Reference{})})
	provider := &exchangeTestProvider{types: []string{"test.child"}, exists: map[string]bool{}}
	ownership := newExchangeOwnership("strict-owner-target")
	target, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, ownership, exchange.ServiceConfig{
		OrganizationID: "strict-owner-target", SourceSystemID: "strict-owner-system", Schemas: schemas, Now: fixedExchangeNow,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Import(context.Background(), "target-operator", artifact.Bytes); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected disallowed holding reference to fail closed, got %v", err)
	}
	if provider.calls != 0 || len(ownership.values) != 0 {
		t.Fatalf("invalid required reference reached a mutation: calls=%d ownership=%#v", provider.calls, ownership.values)
	}
}

func TestServicePreflightsFullExportCatalogBeforeFilesOrReceipts(t *testing.T) {
	fields := []patterns.Field{
		{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
		{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
		{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
	}
	schemas := newExchangePatternTemplate(t, "test.record", "export-schema", fields)
	fileRecord := testRecord("test.record", "record-a", []exchange.Reference{})
	fileRecord.File = &exchange.FileMetadata{Mode: exchange.FileModeMetadata, Name: "row.txt", MediaType: "text/plain", SizeBytes: 4, SHA256: "9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a"}
	validLater := testRecord("test.record", "record-b", []exchange.Reference{})

	t.Run("valid catalog exports once", func(t *testing.T) {
		store := &countingExchangeStore{Store: repository.NewMemoryExchangeStore()}
		provider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{fileRecord, validLater}, fileContent: map[string][]byte{"test.record:record-a": {1, 2, 3, 4}}}
		service, err := exchange.NewService(store, foundation.NopAuditor{}, newExchangeOwnership("export-valid"), exchange.ServiceConfig{
			OrganizationID: "export-valid", SourceSystemID: "export-valid-system", Schemas: schemas, Now: fixedExchangeNow,
		}, provider)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Export(context.Background(), "operator", exchange.ExportRequest{Selection: []exchange.Reference{{Type: "test.record", ID: "record-a"}}, FileMode: exchange.FileModeInclude}); err != nil {
			t.Fatal(err)
		}
		if provider.fileReads != 1 || store.creates != 1 || store.updates != 0 {
			t.Fatalf("valid export did not perform exactly one file read and receipt create: reads=%d creates=%d updates=%d", provider.fileReads, store.creates, store.updates)
		}
	})

	t.Run("later invalid catalog row prevents file and receipt work", func(t *testing.T) {
		invalidLater := validLater
		invalidLater.Payload = json.RawMessage(`{"id":"record-b","revision":1}`)
		store := &countingExchangeStore{Store: repository.NewMemoryExchangeStore()}
		provider := &exchangeTestProvider{types: []string{"test.record"}, records: []exchange.Record{fileRecord, invalidLater}, fileContent: map[string][]byte{"test.record:record-a": {1, 2, 3, 4}}}
		service, err := exchange.NewService(store, foundation.NopAuditor{}, newExchangeOwnership("export-invalid"), exchange.ServiceConfig{
			OrganizationID: "export-invalid", SourceSystemID: "export-invalid-system", Schemas: schemas, Now: fixedExchangeNow,
		}, provider)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Export(context.Background(), "operator", exchange.ExportRequest{Selection: []exchange.Reference{{Type: "test.record", ID: "record-a"}}, FileMode: exchange.FileModeInclude}); !errors.Is(err, exchange.ErrInvalidInput) {
			t.Fatalf("expected invalid later catalog row rejection, got %v", err)
		}
		if provider.fileReads != 0 || store.creates != 0 || store.updates != 0 {
			t.Fatalf("invalid catalog reached file or receipt mutation: reads=%d creates=%d updates=%d", provider.fileReads, store.creates, store.updates)
		}
	})
}

type exchangeTestProvider struct {
	types             []string
	records           []exchange.Record
	exists            map[string]bool
	imported          []string
	failures          int
	failOnCall        int
	calls             int
	committedFailures int
	repairCalls       int
	operationTokens   []string
	fileReads         int
	fileContent       map[string][]byte
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
func (p *exchangeTestProvider) ImportRecordExists(_ context.Context, record exchange.Record, _ []byte) (bool, error) {
	return p.exists[exchange.Reference{Type: record.Type, ID: record.ID}.Key()], nil
}
func (p *exchangeTestProvider) ImportRecord(_ context.Context, operation exchange.ProviderImportOperation, _ string, record exchange.Record, _ []byte) (exchange.ProviderImportResult, error) {
	p.calls++
	p.operationTokens = append(p.operationTokens, operation.Token)
	if operation.Repair {
		p.repairCalls++
	}
	if p.failures > 0 {
		p.failures--
		return exchange.ProviderImportResult{}, errors.New("transient provider failure")
	}
	if p.failOnCall > 0 && p.calls == p.failOnCall {
		return exchange.ProviderImportResult{}, errors.New("injected provider failure")
	}
	key := exchange.Reference{Type: record.Type, ID: record.ID}.Key()
	if p.exists == nil {
		p.exists = make(map[string]bool)
	}
	if p.committedFailures > 0 {
		created := !p.exists[key]
		p.exists[key] = true
		if created {
			p.imported = append(p.imported, key)
		}
		p.committedFailures--
		return exchange.ProviderImportResult{Committed: true, Created: created}, errors.New("post-commit provider audit failure")
	}
	if p.exists[key] {
		return exchange.ProviderImportResult{Committed: true}, nil
	}
	p.exists[key] = true
	p.imported = append(p.imported, key)
	return exchange.ProviderImportResult{Committed: true, Created: true}, nil
}

func (p *exchangeTestProvider) ReadRecordFile(_ context.Context, record exchange.Record) ([]byte, error) {
	p.fileReads++
	value, ok := p.fileContent[exchange.Reference{Type: record.Type, ID: record.ID}.Key()]
	if !ok {
		return nil, exchange.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

type blockingExchangeProvider struct {
	mu      sync.Mutex
	exists  bool
	calls   int
	started chan struct{}
	release chan struct{}
}

func newBlockingExchangeProvider() *blockingExchangeProvider {
	return &blockingExchangeProvider{started: make(chan struct{}), release: make(chan struct{})}
}

func (*blockingExchangeProvider) Types() []string { return []string{"test.record"} }
func (*blockingExchangeProvider) ListRecords(context.Context) ([]exchange.Record, error) {
	return []exchange.Record{}, nil
}
func (p *blockingExchangeProvider) Exists(_ context.Context, _ exchange.Reference) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exists, nil
}
func (p *blockingExchangeProvider) ImportRecordExists(_ context.Context, _ exchange.Record, _ []byte) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exists, nil
}
func (p *blockingExchangeProvider) ImportRecord(ctx context.Context, _ exchange.ProviderImportOperation, _ string, _ exchange.Record, _ []byte) (exchange.ProviderImportResult, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	if first {
		close(p.started)
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return exchange.ProviderImportResult{}, ctx.Err()
	}
	p.mu.Lock()
	created := !p.exists
	p.exists = true
	p.mu.Unlock()
	return exchange.ProviderImportResult{Committed: true, Created: created}, nil
}
func (p *blockingExchangeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type terminalFailureExchangeStore struct {
	exchange.Store
	failTerminal bool
}

type countingExchangeStore struct {
	exchange.Store
	creates int
	updates int
}

func (s *countingExchangeStore) CreatePackage(ctx context.Context, value exchange.Package) (exchange.Package, bool, error) {
	s.creates++
	return s.Store.CreatePackage(ctx, value)
}

func (s *countingExchangeStore) UpdatePackage(ctx context.Context, value exchange.Package, expected time.Time) (exchange.Package, error) {
	s.updates++
	return s.Store.UpdatePackage(ctx, value, expected)
}

type postWriteCrashExchangeStore struct {
	exchange.Store
	updates        int
	failFromUpdate int
	enabled        bool
}

func (s *postWriteCrashExchangeStore) UpdatePackage(ctx context.Context, value exchange.Package, expected time.Time) (exchange.Package, error) {
	s.updates++
	if s.enabled && s.updates >= s.failFromUpdate {
		return exchange.Package{}, errors.New("simulated receipt store outage after provider commit")
	}
	return s.Store.UpdatePackage(ctx, value, expected)
}

func (s *terminalFailureExchangeStore) UpdatePackage(ctx context.Context, value exchange.Package, expected time.Time) (exchange.Package, error) {
	if s.failTerminal && (value.Status == exchange.StatusCompleted || value.Status == exchange.StatusHolding) {
		s.failTerminal = false
		return exchange.Package{}, errors.New("injected terminal receipt failure")
	}
	return s.Store.UpdatePackage(ctx, value, expected)
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

func newExchangePatterns(t *testing.T, recordTypes ...string) *patterns.Service {
	t.Helper()
	service, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{OrganizationID: "exchange-patterns"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, recordType := range recordTypes {
		if _, _, builtIn := patterns.BuiltInTemplateReference(recordType); builtIn || seen[recordType] {
			continue
		}
		seen[recordType] = true
		_, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
			ID: "test-schema-" + strings.ReplaceAll(recordType, ".", "-"), RecordType: recordType, Name: "Test " + recordType,
			Fields: []patterns.Field{
				{Key: "id", Label: "ID", Type: patterns.FieldText, Required: true},
				{Key: "revision", Label: "Revision", Type: patterns.FieldNumber, Required: true},
				{Key: "name", Label: "Name", Type: patterns.FieldText, Required: true},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return service
}

func newExchangePatternTemplate(t *testing.T, recordType, id string, fields []patterns.Field) *patterns.Service {
	t.Helper()
	service, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{OrganizationID: "exchange-patterns"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{ID: id, RecordType: recordType, Name: "Exchange test schema", Fields: fields}); err != nil {
		t.Fatal(err)
	}
	return service
}

type patternSchemaFixture struct {
	id     string
	fields []patterns.Field
}

func newExchangePatternSet(t *testing.T, schemas map[string]patternSchemaFixture) *patterns.Service {
	t.Helper()
	service, err := patterns.NewService(repository.NewMemoryPatternsStore(), foundation.NopAuditor{}, patterns.ServiceConfig{OrganizationID: "exchange-patterns"})
	if err != nil {
		t.Fatal(err)
	}
	for recordType, schema := range schemas {
		if _, err := service.CreateTemplate(context.Background(), patterns.CreateTemplateInput{
			ID: schema.id, RecordType: recordType, Name: "Exchange " + recordType, Fields: schema.fields,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return service
}

func exportTestArtifact(t *testing.T, schemas exchange.SchemaRegistry, records []exchange.Record) exchange.ExportArtifact {
	t.Helper()
	provider := &exchangeTestProvider{types: []string{"test.first", "test.second", "test.child"}, records: records}
	service, err := exchange.NewService(repository.NewMemoryExchangeStore(), foundation.NopAuditor{}, newExchangeOwnership("schema-fixture-source"), exchange.ServiceConfig{
		OrganizationID: "schema-fixture-source", SourceSystemID: "schema-fixture-system", Schemas: schemas, Now: fixedExchangeNow,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	selection := make([]exchange.Reference, 0, len(records))
	for _, record := range records {
		selection = append(selection, exchange.Reference{Type: record.Type, ID: record.ID})
	}
	artifact, err := service.Export(context.Background(), "fixture-operator", exchange.ExportRequest{Selection: selection, FileMode: exchange.FileModeMetadata})
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

type permissiveExchangePatterns struct{ exchange.SchemaRegistry }

func (p permissiveExchangePatterns) Validate(_ context.Context, _ string, _ int64, input patterns.ValidationInput) (patterns.ValidationResult, error) {
	return patterns.ValidationResult{
		Status: patterns.ValidationValid, NormalizedValues: input.Values,
		Errors: []patterns.FieldError{}, HoldingReferences: []patterns.HoldingReference{},
	}, nil
}

type selectiveExchangePatterns struct {
	exchange.SchemaRegistry
	recordType string
	mismatch   bool
	retired    bool
}

func (s selectiveExchangePatterns) ActiveTemplateForRecordType(ctx context.Context, recordType string) (patterns.Template, error) {
	template, err := s.SchemaRegistry.ActiveTemplateForRecordType(ctx, recordType)
	if err == nil && recordType == s.recordType {
		if s.mismatch {
			template.ID += "-different"
		}
		if s.retired {
			template.Status = patterns.StatusRetired
		}
	}
	return template, err
}

func (s selectiveExchangePatterns) GetTemplate(ctx context.Context, id string, version int64) (patterns.Template, error) {
	template, err := s.SchemaRegistry.GetTemplate(ctx, id, version)
	if err == nil && template.RecordType == s.recordType {
		if s.mismatch {
			template.ID += "-different"
		}
		if s.retired {
			template.Status = patterns.StatusRetired
		}
	}
	return template, err
}

type retiredExchangePatterns struct{ *patterns.Service }

func (r retiredExchangePatterns) ActiveTemplateForRecordType(ctx context.Context, recordType string) (patterns.Template, error) {
	template, err := r.Service.ActiveTemplateForRecordType(ctx, recordType)
	template.Status = patterns.StatusRetired
	return template, err
}

func (r retiredExchangePatterns) GetTemplate(ctx context.Context, id string, version int64) (patterns.Template, error) {
	template, err := r.Service.GetTemplate(ctx, id, version)
	template.Status = patterns.StatusRetired
	return template, err
}
