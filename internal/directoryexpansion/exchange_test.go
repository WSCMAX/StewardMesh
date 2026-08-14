package directoryexpansion_test

// Requirements: REQ-DIRECTORY-EXPANSION-005, REQ-EXCHANGE-001. Features: integrations.protocols, migration.packages.

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type directoryExchangeHasher struct{}

func (directoryExchangeHasher) Hash(string) (string, error) { return "directory-exchange-test", nil }
func (directoryExchangeHasher) Verify(string, string) (bool, bool, error) {
	return true, false, nil
}

type directoryExchangeConnector struct {
	system  directoryexpansion.SourceSystem
	records []directoryexpansion.Record
}

func (c directoryExchangeConnector) SourceSystem() directoryexpansion.SourceSystem { return c.system }
func (c directoryExchangeConnector) PullPage(_ context.Context, cursor string) (directoryexpansion.Page, error) {
	if cursor != "" {
		return directoryexpansion.Page{}, directoryexpansion.ErrInvalidInput
	}
	return directoryexpansion.Page{Records: append([]directoryexpansion.Record(nil), c.records...)}, nil
}

type directoryExchangeAuditor struct {
	mu       sync.Mutex
	events   map[string]foundation.AuditEvent
	failNext bool
}

func (a *directoryExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failNext {
		a.failNext = false
		return errors.New("temporary audit outage")
	}
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("audit replay conflict")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

func TestDirectoryExchangeImporterPreservesStateRepairsAuditAndKeepsConnectorSemantics(t *testing.T) {
	ctx := context.Background()
	organizationID := "example-org"
	operationAt := time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC)
	createdAt := operationAt.Add(24 * time.Hour)
	updatedAt := createdAt.Add(24 * time.Hour)
	store := repository.NewMemoryDirectoryImportStore()
	guardService, err := guard.NewService(repository.NewMemoryGuardStore(), directoryExchangeHasher{}, foundation.NopAuditor{}, nil,
		guard.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return operationAt }})
	if err != nil {
		t.Fatal(err)
	}
	auditor := &directoryExchangeAuditor{events: make(map[string]foundation.AuditEvent), failNext: true}
	target, importer, err := directoryexpansion.NewGroupTargetWithExchangeImporter(store, guardService, auditor,
		directoryexpansion.GroupTargetExchangeConfig{OrganizationID: organizationID, Now: func() time.Time { return operationAt }})
	if err != nil {
		t.Fatal(err)
	}
	group := directoryexpansion.ManagedGroup{ID: "11111111111111111111111111111111", SourceSystemID: "directory-source", SourceRecordID: "group-one",
		Name: "portable-group", DisplayName: "Portable group", Description: "Lossless group", Status: "active",
		Metadata: map[string]string{"origin": "grouper", "scope": "all"}, Revision: 17, CreatedAt: createdAt, UpdatedAt: updatedAt}
	operation := directoryexpansion.ExchangeImportOperation{Token: "directory-import-operation", OccurredAt: operationAt}
	result, err := importer.ImportManagedGroup(ctx, operation, group)
	if err == nil || !result.Committed || !result.Created {
		t.Fatalf("expected post-commit audit failure, result=%#v err=%v", result, err)
	}
	stored, err := target.GetManagedGroup(ctx, group.ID)
	if err != nil || stored.OrganizationID != organizationID || stored.Revision != group.Revision ||
		!stored.CreatedAt.Equal(createdAt) || !stored.UpdatedAt.Equal(updatedAt) || stored.Metadata["scope"] != "all" {
		t.Fatalf("managed group import was not lossless: %#v err=%v", stored, err)
	}
	repaired, err := importer.ImportManagedGroup(ctx, operation, group)
	if err != nil || !repaired.Committed || repaired.Created {
		t.Fatalf("repair exact group audit: %#v err=%v", repaired, err)
	}
	if len(auditor.events) != 1 {
		t.Fatalf("expected one deterministic repaired audit, got %d", len(auditor.events))
	}
	conflict := group
	conflict.Description = "different immutable content"
	if _, err := importer.ImportManagedGroup(ctx, operation, conflict); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected exact import conflict, got %v", err)
	}
	membership := directoryexpansion.ManagedMembership{ID: "22222222222222222222222222222222", SourceSystemID: group.SourceSystemID,
		SourceRecordID: "membership-one", GroupID: group.ID, GroupSourceID: group.SourceRecordID,
		MemberID: "33333333333333333333333333333333", MemberSourceID: "embedded-subject", MemberKind: directoryexpansion.MemberSubject,
		MemberDisplayName: "Embedded subject", Status: "active", Metadata: map[string]string{"membership": "direct"},
		Revision: 23, CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt}
	result, err = importer.ImportManagedMembership(ctx, operation, membership)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import subject membership: %#v err=%v", result, err)
	}
	snapshot, err := target.ExchangeSnapshot(ctx, 2)
	if err != nil || len(snapshot.Groups) != 1 || len(snapshot.Memberships) != 1 || snapshot.Memberships[0].Revision != membership.Revision {
		t.Fatalf("unexpected bounded Directory snapshot %#v err=%v", snapshot, err)
	}
	if _, err := target.ExchangeSnapshot(ctx, 1); !errors.Is(err, directoryexpansion.ErrTooLarge) {
		t.Fatalf("expected bounded snapshot rejection, got %v", err)
	}

	credentials, err := guardService.Bootstrap(ctx, guard.BootstrapInput{Username: "administrator", Email: "administrator@example.test",
		DisplayName: "Administrator", Password: "correct horse battery staple"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := guardService.RegisterImportedResourceOwnership(ctx, credentials.Authentication.Principal.Subject, guard.ResourceOwnershipInput{
		ResourceType: "directory.group", ResourceID: group.ID, SourceSystemID: group.SourceSystemID, SourceRecordID: group.SourceRecordID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := guardService.RegisterImportedResourceOwnership(ctx, credentials.Authentication.Principal.Subject, guard.ResourceOwnershipInput{
		ResourceType: "directory.membership", ResourceID: membership.ID, SourceSystemID: membership.SourceSystemID, SourceRecordID: membership.SourceRecordID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := guardService.CheckResourceWrite(ctx, credentials.Authentication, "directory.group", group.ID); !errors.Is(err, guard.ErrResourceWriteLocked) {
		t.Fatalf("ordinary write was not fenced: %v", err)
	}
	system := directoryexpansion.SourceSystem{ID: group.SourceSystemID, Provider: directoryexpansion.GrouperProvider, ConfigRevision: "config-v2"}
	connectorRecord := directoryexpansion.Record{SourceRecordID: group.SourceRecordID, Kind: directoryexpansion.RecordGroup,
		GroupName: group.Name, DisplayName: "Connector reconciliation", Description: group.Description, Status: group.Status,
		NormalizedMetadata: group.Metadata}
	connectorMembershipRecord := directoryexpansion.Record{SourceRecordID: membership.SourceRecordID, Kind: directoryexpansion.RecordMembership,
		DisplayName: "Connector subject reconciliation", Status: membership.Status, GroupSourceID: membership.GroupSourceID,
		MemberSourceID: membership.MemberSourceID, MemberKind: membership.MemberKind, NormalizedMetadata: membership.Metadata}
	registry, err := directoryexpansion.NewRegistry(directoryExchangeConnector{system: system,
		records: []directoryexpansion.Record{connectorRecord, connectorMembershipRecord}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := directoryexpansion.NewService(store, target, foundation.NopAuditor{}, registry,
		directoryexpansion.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time { return operationAt }})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(ctx, credentials.Authentication,
		directoryexpansion.PreviewRequest{SourceSystemID: system.ID}, "directory-exchange-preview")
	if err != nil {
		t.Fatalf("preview imported Directory group through full service: %v", err)
	}
	detail, err := store.GetBatch(ctx, organizationID, preview.Batch.ID)
	if err != nil || len(detail.Items) != 2 {
		t.Fatalf("service did not rediscover imported group by source: %#v err=%v", detail, err)
	}
	plannedTargets := make(map[string]directoryexpansion.Item, len(detail.Items))
	for _, item := range detail.Items {
		plannedTargets[item.Record.SourceRecordID] = item
	}
	if plannedTargets[group.SourceRecordID].TargetID != group.ID || plannedTargets[group.SourceRecordID].Action != directoryexpansion.ActionUpdate ||
		plannedTargets[membership.SourceRecordID].TargetID != membership.ID || plannedTargets[membership.SourceRecordID].Action != directoryexpansion.ActionUpdate {
		t.Fatalf("service did not rediscover imported records by authoritative source: %#v", detail.Items)
	}
	applied, err := service.Apply(ctx, credentials.Authentication, preview.Batch.ID, "directory-exchange-apply")
	if err != nil || applied.Batch.Status != directoryexpansion.BatchApplied || applied.Batch.Counts.Updated != 2 {
		t.Fatalf("full connector reconciliation failed: %#v err=%v", applied, err)
	}
	reconciled, err := target.GetManagedGroup(ctx, group.ID)
	if err != nil || reconciled.Revision != group.Revision+1 || reconciled.DisplayName != connectorRecord.DisplayName ||
		reconciled.UpdatedAt.Before(reconciled.CreatedAt) || !reconciled.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("future-dated imported group reconciled invalidly: %#v err=%v", reconciled, err)
	}
	reconciledMembership, err := target.GetManagedMembership(ctx, membership.ID)
	if err != nil || reconciledMembership.Revision != membership.Revision+1 ||
		reconciledMembership.MemberDisplayName != connectorMembershipRecord.DisplayName || reconciledMembership.GroupID != membership.GroupID ||
		reconciledMembership.MemberID != membership.MemberID || reconciledMembership.UpdatedAt.Before(reconciledMembership.CreatedAt) ||
		!reconciledMembership.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("future-dated imported membership reconciled invalidly: %#v err=%v", reconciledMembership, err)
	}
	mappings, err := store.ListMappings(ctx, organizationID, system.ID)
	mappedTargets := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		mappedTargets[mapping.SourceRecordID] = mapping.TargetID
	}
	if err != nil || len(mappings) != 2 || mappedTargets[group.SourceRecordID] != group.ID || mappedTargets[membership.SourceRecordID] != membership.ID {
		t.Fatalf("full connector apply did not create the normal mapping: %#v err=%v", mappings, err)
	}
	snapshot, err = target.ExchangeSnapshot(ctx, 2)
	if err != nil || len(snapshot.Groups) != 1 || len(snapshot.Memberships) != 1 {
		t.Fatalf("connector reconciliation duplicated imported Directory state: %#v err=%v", snapshot, err)
	}
}

func TestMemoryDirectoryExchangeImportIsAtomicUnderExactConcurrency(t *testing.T) {
	store := repository.NewMemoryDirectoryImportStore()
	now := time.Date(2026, time.August, 13, 22, 30, 0, 0, time.UTC)
	group := directoryexpansion.ManagedGroup{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OrganizationID: "example-org",
		SourceSystemID: "directory-source", SourceRecordID: "concurrent-group", Name: "concurrent", DisplayName: "Concurrent",
		Status: "active", Revision: 8, CreatedAt: now, UpdatedAt: now}
	const workers = 16
	var wait sync.WaitGroup
	wait.Add(workers)
	created := make(chan bool, workers)
	errorsSeen := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			stored, wasCreated, err := store.ImportManagedGroup(context.Background(), group)
			if err == nil && (stored.ID != group.ID || stored.Revision != group.Revision) {
				err = errors.New("atomic import returned different state")
			}
			created <- wasCreated
			errorsSeen <- err
		}()
	}
	wait.Wait()
	close(created)
	close(errorsSeen)
	createdCount := 0
	for value := range created {
		if value {
			createdCount++
		}
	}
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if createdCount != 1 {
		t.Fatalf("expected one atomic create, got %d", createdCount)
	}
}
