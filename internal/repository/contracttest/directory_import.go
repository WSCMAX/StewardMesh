package contracttest

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-006.
// Features: integrations.protocols, identity.directory.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

// DirectoryImportStore verifies durable idempotency, lease, exact-plan,
// mapping, list, and attempt-history behavior shared by every repository.
func DirectoryImportStore(t *testing.T, store directoryexpansion.Store, organizationID, unique string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	empty, err := store.ListBatches(ctx, organizationID, directoryexpansion.ListQuery{Limit: 1})
	if err != nil || empty.Batches == nil || len(empty.Batches) != 0 {
		t.Fatalf("empty batch collection must be a non-nil array: %#v err=%v", empty, err)
	}
	batchID := contractDirectoryID("batch", unique)
	itemID := contractDirectoryID("item", unique)
	previewAttemptID := contractDirectoryID("preview-attempt", unique)
	applyAttemptID := contractDirectoryID("apply-attempt", unique)
	retryAttemptID := contractDirectoryID("retry-attempt", unique)
	record := directoryexpansion.Record{
		SourceRecordID: "source-" + unique, Kind: directoryexpansion.RecordIdentity, IdentityKind: "person", DisplayName: "Ada",
		Email: "ada-" + contractDirectoryID("email", unique)[:8] + "@example.test", Status: "active", Department: "Information Technology",
		DirectoryAttributes: map[string]string{"job-title": "Engineer", "office-location": "Main Campus"},
		GroupSourceIDs:      []string{"group:directory-operators", "group:staff"},
	}
	batch := directoryexpansion.Batch{ID: batchID, OrganizationID: organizationID, SourceSystemID: "hr-primary", Provider: "example", ConfigRevision: "v1", Status: directoryexpansion.BatchPreviewed, CompleteSnapshot: false, Counts: directoryexpansion.Counts{Created: 1}, CreatedAt: now, UpdatedAt: now}
	item := directoryexpansion.Item{ID: itemID, OrganizationID: organizationID, BatchID: batchID, Record: record, TargetID: contractDirectoryID("target", unique), SourceDigest: contractDirectoryDigest("source", unique), PlannedTargetDigest: contractDirectoryDigest("target-digest", unique), Action: directoryexpansion.ActionCreate, Outcome: directoryexpansion.OutcomePending, UpdatedAt: now}
	previewResult := directoryexpansion.OperationResult{Batch: batch}
	previewAttempt := directoryexpansion.Attempt{ID: previewAttemptID, OrganizationID: organizationID, BatchID: batchID, Operation: directoryexpansion.OperationPreview, IdempotencyHash: contractDirectoryDigest("preview-key", unique), RequestFingerprint: contractDirectoryDigest("preview-fingerprint", unique), Number: 1, Status: directoryexpansion.BatchPreviewed, ActorID: "account:test", CorrelationID: previewAttemptID, StartedAt: now, CompletedAt: &now, Result: &previewResult}
	created, replay, err := store.CreatePreview(ctx, batch, []directoryexpansion.Item{item}, previewAttempt)
	if err != nil || replay || created.Batch.ID != batchID {
		t.Fatalf("create preview: %#v replay=%v err=%v", created, replay, err)
	}
	record.DirectoryAttributes["job-title"] = "tampered after create"
	record.GroupSourceIDs[0] = "group:tampered-after-create"
	replayed, replay, err := store.CreatePreview(ctx, batch, []directoryexpansion.Item{item}, previewAttempt)
	if err != nil || !replay || replayed.Batch.ID != batchID {
		t.Fatalf("replay preview: %#v replay=%v err=%v", replayed, replay, err)
	}
	loadedAttempt, err := store.FindAttempt(ctx, organizationID, directoryexpansion.OperationPreview, previewAttempt.IdempotencyHash)
	if err != nil || loadedAttempt.Result == nil || loadedAttempt.Result.Batch.ID != batchID {
		t.Fatalf("find preview attempt: %#v err=%v", loadedAttempt, err)
	}
	detail, err := store.GetBatch(ctx, organizationID, batchID)
	if err != nil || len(detail.Items) != 1 || len(detail.Attempts) != 1 || detail.Items[0].SourceDigest != item.SourceDigest ||
		detail.Items[0].Record.DirectoryAttributes["job-title"] != "Engineer" || detail.Items[0].Record.GroupSourceIDs[0] != "group:directory-operators" {
		t.Fatalf("get exact preview: %#v err=%v", detail, err)
	}
	record.DirectoryAttributes["job-title"] = "Engineer"
	record.GroupSourceIDs[0] = "group:directory-operators"
	detail.Items[0].Record.DirectoryAttributes["job-title"] = "tampered returned record"
	detail.Items[0].Record.GroupSourceIDs[0] = "group:tampered-returned-record"
	detail, err = store.GetBatch(ctx, organizationID, batchID)
	if err != nil || !reflect.DeepEqual(detail.Items[0].Record, record) {
		t.Fatalf("returned item mutated authoritative plan: %#v err=%v", detail.Items, err)
	}

	applyAttempt := directoryexpansion.Attempt{ID: applyAttemptID, OrganizationID: organizationID, BatchID: batchID, Operation: directoryexpansion.OperationApply, IdempotencyHash: contractDirectoryDigest("apply-key", unique), RequestFingerprint: contractDirectoryDigest("apply-fingerprint", unique), Number: 2, Status: directoryexpansion.BatchApplying, ActorID: "account:test", CorrelationID: applyAttemptID, StartedAt: now.Add(time.Second)}
	leaseToken := contractDirectoryID("lease", unique)
	detail, operationReplay, err := store.BeginOperation(ctx, organizationID, batchID, applyAttempt, leaseToken, now, now.Add(time.Minute))
	if err != nil || operationReplay != nil || detail.Batch.LeaseToken != leaseToken || len(detail.Attempts) != 2 {
		t.Fatalf("begin apply lease: %#v replay=%#v err=%v", detail, operationReplay, err)
	}
	retryAttempt := directoryexpansion.Attempt{ID: retryAttemptID, OrganizationID: organizationID, BatchID: batchID, Operation: directoryexpansion.OperationRetry, IdempotencyHash: contractDirectoryDigest("retry-key", unique), RequestFingerprint: contractDirectoryDigest("retry-fingerprint", unique), Number: 3, Status: directoryexpansion.BatchApplying, ActorID: "account:test", CorrelationID: retryAttemptID, StartedAt: now.Add(2 * time.Second)}
	if _, _, err := store.BeginOperation(ctx, organizationID, batchID, retryAttempt, contractDirectoryID("other-lease", unique), now.Add(time.Second), now.Add(time.Minute)); !errors.Is(err, directoryexpansion.ErrBusy) {
		t.Fatalf("expected active lease conflict, got %v", err)
	}
	resumed, resumedReplay, err := store.BeginOperation(ctx, organizationID, batchID, applyAttempt, contractDirectoryID("resumed-lease", unique), now.Add(2*time.Minute), now.Add(3*time.Minute))
	if err != nil || resumedReplay != nil || resumed.Batch.LeaseToken != contractDirectoryID("resumed-lease", unique) || len(resumed.Attempts) != 2 {
		t.Fatalf("resume expired exact attempt: %#v replay=%#v err=%v", resumed, resumedReplay, err)
	}
	leaseToken = contractDirectoryID("resumed-lease", unique)
	item.Outcome, item.UpdatedAt = directoryexpansion.OutcomeApplied, now.Add(3*time.Second)
	item.Record.DisplayName = "tampered after preview"
	item.Action = directoryexpansion.ActionUpdate
	mapping := directoryexpansion.Mapping{OrganizationID: organizationID, SourceSystemID: batch.SourceSystemID, Provider: batch.Provider, SourceRecordID: record.SourceRecordID, Kind: record.Kind, TargetID: item.TargetID, SourceDigest: item.SourceDigest, AppliedTargetDigest: item.PlannedTargetDigest, LastRecord: record, Active: true, LastSeenBatchID: batchID, LastAppliedBatchID: batchID, UpdatedAt: item.UpdatedAt}
	if err := store.SaveItem(ctx, organizationID, batchID, "wrong-lease", item, &mapping); !errors.Is(err, directoryexpansion.ErrLeaseLost) {
		t.Fatalf("expected wrong lease rejection, got %v", err)
	}
	if err := store.SaveItem(ctx, organizationID, batchID, leaseToken, item, &mapping); err != nil {
		t.Fatalf("save item and mapping: %v", err)
	}
	completed := now.Add(4 * time.Second)
	batch.Status, batch.CompleteSnapshot, batch.UpdatedAt, batch.CompletedAt = directoryexpansion.BatchApplied, true, completed, &completed
	result := directoryexpansion.OperationResult{Batch: batch}
	applyAttempt.Status, applyAttempt.CompletedAt, applyAttempt.Result = directoryexpansion.BatchApplied, &completed, &result
	if err := store.FinishOperation(ctx, organizationID, batchID, leaseToken, applyAttempt, result); err != nil {
		t.Fatalf("finish apply: %v", err)
	}
	finished, err := store.GetBatch(ctx, organizationID, batchID)
	if err != nil || !finished.Batch.CompleteSnapshot || finished.Batch.CompletedAt == nil || !finished.Batch.CompletedAt.Equal(completed) {
		t.Fatalf("finish persisted recovered batch metadata: %#v err=%v", finished.Batch, err)
	}
	if len(finished.Items) != 1 || finished.Items[0].Record.DisplayName != record.DisplayName || finished.Items[0].Action != directoryexpansion.ActionCreate || finished.Items[0].Outcome != directoryexpansion.OutcomeApplied {
		t.Fatalf("persisted preview plan was mutated by outcome update: %#v", finished.Items)
	}
	mappings, err := store.ListMappings(ctx, organizationID, batch.SourceSystemID)
	if err != nil || len(mappings) != 1 || mappings[0].TargetID != item.TargetID || !mappings[0].Active ||
		!reflect.DeepEqual(mappings[0].LastRecord, record) {
		t.Fatalf("durable mapping: %#v err=%v", mappings, err)
	}
	_, exactReplay, err := store.BeginOperation(ctx, organizationID, batchID, applyAttempt, contractDirectoryID("unused", unique), now.Add(5*time.Second), now.Add(time.Minute))
	if err != nil || exactReplay == nil || exactReplay.Batch.ID != batchID {
		t.Fatalf("exact apply replay: %#v err=%v", exactReplay, err)
	}
	page, err := store.ListBatches(ctx, organizationID, directoryexpansion.ListQuery{Limit: 1})
	if err != nil || len(page.Batches) != 1 || page.Batches[0].ID != batchID {
		t.Fatalf("list batches: %#v err=%v", page, err)
	}

	failedAt := now.Add(6 * time.Second)
	failedBatchID := contractDirectoryID("failed-batch", unique)
	failedBatch := directoryexpansion.Batch{ID: failedBatchID, OrganizationID: organizationID, SourceSystemID: "hr-secondary", Provider: "example", ConfigRevision: "v2", Status: directoryexpansion.BatchFailed, Counts: directoryexpansion.Counts{Failed: 1}, CreatedAt: failedAt, UpdatedAt: failedAt, CompletedAt: &failedAt}
	failedResult := directoryexpansion.OperationResult{Batch: failedBatch}
	failedAttempt := directoryexpansion.Attempt{ID: contractDirectoryID("failed-preview-attempt", unique), OrganizationID: organizationID, BatchID: failedBatchID, Operation: directoryexpansion.OperationPreview, IdempotencyHash: contractDirectoryDigest("failed-preview-key", unique), RequestFingerprint: contractDirectoryDigest("failed-preview-fingerprint", unique), Number: 1, Status: directoryexpansion.BatchFailed, FailureClass: directoryexpansion.FailureTransient, Retryable: true, ErrorMessage: "source temporarily unavailable", ActorID: "account:test", CorrelationID: contractDirectoryID("failed-correlation", unique), StartedAt: failedAt, CompletedAt: &failedAt, Result: &failedResult}
	if _, replay, err := store.CreatePreview(ctx, failedBatch, nil, failedAttempt); err != nil || replay {
		t.Fatalf("create failed preview: replay=%v err=%v", replay, err)
	}
	failedDetail, err := store.GetBatch(ctx, organizationID, failedBatchID)
	if err != nil || failedDetail.Batch.CompletedAt == nil || !failedDetail.Batch.CompletedAt.Equal(failedAt) {
		t.Fatalf("failed preview completion was not durable: %#v err=%v", failedDetail.Batch, err)
	}
}

func contractDirectoryID(prefix, unique string) string {
	return contractDirectoryDigest(prefix, unique)[:32]
}
func contractDirectoryDigest(prefix, unique string) string {
	sum := sha256.Sum256([]byte(prefix + "\x00" + unique))
	return hex.EncodeToString(sum[:])
}
