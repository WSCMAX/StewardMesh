package contracttest

// Requirement: REQ-DIRECTORY-EXPANSION-005. Feature: integrations.protocols.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

func DirectoryGroupTargetStore(t *testing.T, store directoryexpansion.GroupTargetStore, organizationID, unique string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	group := directoryexpansion.ManagedGroup{ID: contractDirectoryID("group", unique), OrganizationID: organizationID,
		SourceSystemID: "grouper-" + unique, SourceRecordID: "group-source-" + unique, Name: "app:staff",
		DisplayName: "Staff", Description: "Imported staff", Status: "active", Metadata: map[string]string{"classification": "internal"},
		Revision: 1, CreatedAt: now, UpdatedAt: now}
	created, err := store.CreateManagedGroup(ctx, group)
	if err != nil || created.DisplayName != group.DisplayName {
		t.Fatalf("create managed group: %#v err=%v", created, err)
	}
	if _, err := store.CreateManagedGroup(ctx, group); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected duplicate managed group conflict, got %v", err)
	}
	loaded, err := store.GetManagedGroup(ctx, organizationID, group.ID)
	if err != nil || loaded.Metadata["classification"] != "internal" {
		t.Fatalf("load managed group: %#v err=%v", loaded, err)
	}
	loaded.Metadata["classification"] = "tampered"
	reloaded, _ := store.GetManagedGroup(ctx, organizationID, group.ID)
	if reloaded.Metadata["classification"] != "internal" {
		t.Fatal("managed group metadata alias escaped repository boundary")
	}
	group.DisplayName, group.Status, group.Revision, group.UpdatedAt = "Former staff", "inactive", 2, now.Add(time.Second)
	if _, err := store.ReconcileManagedGroup(ctx, group, 99); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected stale group revision conflict, got %v", err)
	}
	updated, err := store.ReconcileManagedGroup(ctx, group, 1)
	if err != nil || updated.Revision != 2 || updated.Status != "inactive" {
		t.Fatalf("reconcile managed group: %#v err=%v", updated, err)
	}
	membership := directoryexpansion.ManagedMembership{ID: contractDirectoryID("membership", unique), OrganizationID: organizationID,
		SourceSystemID: group.SourceSystemID, SourceRecordID: "membership-source-" + unique, GroupID: group.ID,
		GroupSourceID: group.SourceRecordID, MemberID: contractDirectoryID("subject", unique), MemberSourceID: "subject-" + unique,
		MemberKind: directoryexpansion.MemberSubject, MemberDisplayName: "Ada", Status: "active", Metadata: map[string]string{"type": "immediate"},
		Revision: 1, CreatedAt: now, UpdatedAt: now}
	orphan := membership
	orphan.ID, orphan.SourceRecordID, orphan.GroupID = contractDirectoryID("orphan", unique), "orphan-source-"+unique, contractDirectoryID("missing-group", unique)
	if _, err := store.CreateManagedMembership(ctx, orphan); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected orphan membership conflict, got %v", err)
	}
	createdMembership, err := store.CreateManagedMembership(ctx, membership)
	if err != nil || createdMembership.GroupID != group.ID {
		t.Fatalf("create managed membership: %#v err=%v", createdMembership, err)
	}
	if _, err := store.CreateManagedMembership(ctx, membership); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected duplicate managed membership conflict, got %v", err)
	}
	membership.Status, membership.Revision, membership.UpdatedAt = "inactive", 2, now.Add(time.Second)
	if _, err := store.ReconcileManagedMembership(ctx, membership, 1); err != nil {
		t.Fatalf("reconcile managed membership: %v", err)
	}
	groups, err := store.ListManagedGroups(ctx, organizationID)
	if err != nil || len(groups) != 1 || groups[0].ID != group.ID {
		t.Fatalf("list managed groups: %#v err=%v", groups, err)
	}
	memberships, err := store.ListManagedMemberships(ctx, organizationID)
	if err != nil || len(memberships) != 1 || memberships[0].Status != "inactive" {
		t.Fatalf("list managed memberships: %#v err=%v", memberships, err)
	}
	if err := store.DeleteManagedMembership(ctx, organizationID, membership.ID, 1); !errors.Is(err, directoryexpansion.ErrConflict) {
		t.Fatalf("expected stale membership delete conflict, got %v", err)
	}
	if err := store.DeleteManagedMembership(ctx, organizationID, membership.ID, 2); err != nil {
		t.Fatalf("delete managed membership: %v", err)
	}
	if err := store.DeleteManagedGroup(ctx, organizationID, group.ID, 2); err != nil {
		t.Fatalf("delete managed group: %v", err)
	}
	cascade := membership
	cascade.ID, cascade.SourceRecordID, cascade.Revision = contractDirectoryID("cascade", unique), "cascade-source-"+unique, 1
	// Recreate the group and attach a membership solely to verify the repository
	// contract's parent-delete behavior matches PostgreSQL ON DELETE CASCADE.
	group.Revision = 1
	if _, err := store.CreateManagedGroup(ctx, group); err != nil {
		t.Fatalf("recreate managed group for cascade: %v", err)
	}
	if _, err := store.CreateManagedMembership(ctx, cascade); err != nil {
		t.Fatalf("create cascade membership: %v", err)
	}
	if err := store.DeleteManagedGroup(ctx, organizationID, group.ID, 1); err != nil {
		t.Fatalf("delete cascade group: %v", err)
	}
	if _, err := store.GetManagedMembership(ctx, organizationID, cascade.ID); !errors.Is(err, directoryexpansion.ErrNotFound) {
		t.Fatalf("expected group delete to cascade membership, got %v", err)
	}
}

func DirectoryGroupMappingStore(t *testing.T, store directoryexpansion.Store, organizationID, unique string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 13, 0, 0, 0, time.UTC)
	batchID := contractDirectoryID("group-mapping-batch", unique)
	record := directoryexpansion.Record{SourceRecordID: "group-mapping-source-" + unique, Kind: directoryexpansion.RecordGroup,
		GroupName: "app:researchers", DisplayName: "Researchers", Status: "active"}
	item := directoryexpansion.Item{ID: contractDirectoryID("group-mapping-item", unique), OrganizationID: organizationID,
		BatchID: batchID, Record: record, TargetID: contractDirectoryID("group-mapping-target", unique),
		SourceDigest:        contractDirectoryDigest("group-mapping-source-digest", unique),
		PlannedTargetDigest: contractDirectoryDigest("group-mapping-target-digest", unique),
		Action:              directoryexpansion.ActionCreate, Outcome: directoryexpansion.OutcomePending, UpdatedAt: now}
	batch := directoryexpansion.Batch{ID: batchID, OrganizationID: organizationID, SourceSystemID: "grouper-" + unique,
		Provider: directoryexpansion.GrouperProvider, ConfigRevision: "v1", Status: directoryexpansion.BatchPreviewed,
		Counts: directoryexpansion.Counts{Created: 1}, CreatedAt: now, UpdatedAt: now}
	result := directoryexpansion.OperationResult{Batch: batch}
	previewAttempt := directoryexpansion.Attempt{ID: contractDirectoryID("group-mapping-preview", unique), OrganizationID: organizationID,
		BatchID: batchID, Operation: directoryexpansion.OperationPreview, IdempotencyHash: contractDirectoryDigest("group-mapping-preview-key", unique),
		RequestFingerprint: contractDirectoryDigest("group-mapping-preview-fingerprint", unique), Number: 1,
		Status: directoryexpansion.BatchPreviewed, ActorID: "account:test", CorrelationID: contractDirectoryID("group-mapping-correlation", unique),
		StartedAt: now, CompletedAt: &now, Result: &result}
	if _, replay, err := store.CreatePreview(ctx, batch, []directoryexpansion.Item{item}, previewAttempt); err != nil || replay {
		t.Fatalf("create group mapping preview: replay=%v err=%v", replay, err)
	}
	applyAttempt := directoryexpansion.Attempt{ID: contractDirectoryID("group-mapping-apply", unique), OrganizationID: organizationID,
		BatchID: batchID, Operation: directoryexpansion.OperationApply, IdempotencyHash: contractDirectoryDigest("group-mapping-apply-key", unique),
		RequestFingerprint: contractDirectoryDigest("group-mapping-apply-fingerprint", unique), Number: 2,
		Status: directoryexpansion.BatchApplying, ActorID: "account:test", CorrelationID: contractDirectoryID("group-mapping-apply-correlation", unique), StartedAt: now}
	lease := contractDirectoryID("group-mapping-lease", unique)
	if _, replay, err := store.BeginOperation(ctx, organizationID, batchID, applyAttempt, lease, now, now.Add(time.Minute)); err != nil || replay != nil {
		t.Fatalf("begin group mapping apply: replay=%#v err=%v", replay, err)
	}
	item.Outcome = directoryexpansion.OutcomeApplied
	mapping := directoryexpansion.Mapping{OrganizationID: organizationID, SourceSystemID: batch.SourceSystemID,
		Provider: batch.Provider, SourceRecordID: record.SourceRecordID, Kind: record.Kind, TargetID: item.TargetID,
		SourceDigest: item.SourceDigest, AppliedTargetDigest: item.PlannedTargetDigest, LastRecord: record, Active: true,
		LastSeenBatchID: batchID, LastAppliedBatchID: batchID, UpdatedAt: now}
	if err := store.SaveItem(ctx, organizationID, batchID, lease, item, &mapping); err != nil {
		t.Fatalf("save durable group mapping: %v", err)
	}
	mappings, err := store.ListMappings(ctx, organizationID, batch.SourceSystemID)
	if err != nil || len(mappings) != 1 || mappings[0].Kind != directoryexpansion.RecordGroup {
		t.Fatalf("list durable group mapping: %#v err=%v", mappings, err)
	}
}
