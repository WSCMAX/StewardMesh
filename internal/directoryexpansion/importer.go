package directoryexpansion

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005.
// Features: integrations.protocols, identity.directory.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/guard"
)

const leaseDuration = 2 * time.Minute

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
	MaxPages       int
	MaxRecords     int
}

type Service struct {
	store          Store
	target         Target
	auditor        foundation.Auditor
	registry       *Registry
	organizationID string
	now            func() time.Time
	maxPages       int
	maxRecords     int
}

func NewService(store Store, target Target, auditor foundation.Auditor, registry *Registry, config ServiceConfig) (*Service, error) {
	if store == nil || target == nil || auditor == nil || registry == nil {
		return nil, errors.New("directory import store, target, auditor, and connector registry are required")
	}
	config.OrganizationID = strings.TrimSpace(config.OrganizationID)
	if config.OrganizationID == "" {
		return nil, errors.New("directory import organization id is required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.MaxPages == 0 {
		config.MaxPages = MaximumPages
	}
	if config.MaxRecords == 0 {
		config.MaxRecords = MaximumRecords
	}
	if config.MaxPages < 1 || config.MaxPages > MaximumPages || config.MaxRecords < 1 || config.MaxRecords > MaximumRecords {
		return nil, errors.New("directory import bounds are invalid")
	}
	return &Service{store: store, target: target, auditor: auditor, registry: registry,
		organizationID: config.OrganizationID, now: config.Now, maxPages: config.MaxPages, maxRecords: config.MaxRecords}, nil
}

func (s *Service) Sources() []SourceSystem {
	return s.registry.SourceSystems()
}

func (s *Service) Preview(ctx context.Context, authentication guard.Authentication, request PreviewRequest, idempotencyKey string) (OperationResult, error) {
	if err := validateActor(authentication); err != nil {
		return OperationResult{}, err
	}
	sourceID := strings.TrimSpace(request.SourceSystemID)
	keyHash, err := validateIdempotencyKey(idempotencyKey)
	if err != nil || !sourceSystemIDPattern.MatchString(sourceID) {
		return OperationResult{}, fmt.Errorf("%w: sourceSystemId and Idempotency-Key are required", ErrInvalidInput)
	}
	fingerprint := digestStrings(string(OperationPreview), sourceID)
	if replay, replayAttempt, ok, err := s.exactReplay(ctx, OperationPreview, keyHash, fingerprint); err != nil || ok {
		if ok {
			replay.Replay = true
			if auditErr := s.auditPreview(ctx, replayAttempt, replay, keyHash); auditErr != nil {
				return OperationResult{}, auditErr
			}
		}
		return replay, err
	}
	connector, ok := s.registry.Connector(sourceID)
	if !ok {
		return OperationResult{}, ErrConnectorMissing
	}
	system := connector.SourceSystem()
	batchID, err := foundation.NewCorrelationID()
	if err != nil {
		return OperationResult{}, err
	}
	now := s.now()
	batch := Batch{ID: batchID, OrganizationID: s.organizationID, SourceSystemID: system.ID, Provider: system.Provider,
		ConfigRevision: system.ConfigRevision, Status: BatchPreviewed, CreatedAt: now, UpdatedAt: now}
	attempt, err := s.newAttempt(ctx, authentication, batchID, OperationPreview, keyHash, fingerprint, 1)
	if err != nil {
		return OperationResult{}, err
	}
	records, complete, pullErr := s.pull(ctx, connector)
	if pullErr != nil {
		return s.persistFailedPreview(ctx, attempt, batch, keyHash, pullErr)
	}
	batch.CompleteSnapshot = complete
	items, err := s.plan(ctx, batch, system, records)
	if err != nil {
		var classified *ClassifiedError
		if errors.As(err, &classified) {
			return s.persistFailedPreview(ctx, attempt, batch, keyHash, classified)
		}
		return OperationResult{}, err
	}
	batch.Counts = countItems(items)
	completed := s.now()
	batch.UpdatedAt, batch.CompletedAt = completed, &completed
	attempt.CompletedAt, attempt.Status = &completed, BatchPreviewed
	result := OperationResult{Batch: batch}
	attempt.Result = &result
	stored, replayed, err := s.store.CreatePreview(ctx, batch, items, attempt)
	if err != nil {
		return OperationResult{}, err
	}
	stored.Replay = replayed
	auditAttempt := attempt
	if replayed {
		auditAttempt, err = s.store.FindAttempt(ctx, s.organizationID, OperationPreview, keyHash)
		if err != nil {
			return OperationResult{}, err
		}
	}
	if err := s.auditPreview(ctx, auditAttempt, stored, keyHash); err != nil {
		return OperationResult{}, err
	}
	return stored, nil
}

func (s *Service) persistFailedPreview(ctx context.Context, attempt Attempt, batch Batch, keyHash string, operationError error) (OperationResult, error) {
	var classified *ClassifiedError
	if !errors.As(operationError, &classified) {
		return OperationResult{}, operationError
	}
	classified = classify(classified)
	batch.Status, batch.CompleteSnapshot, batch.Counts = BatchFailed, false, Counts{Failed: 1}
	completed := s.now()
	batch.UpdatedAt, batch.CompletedAt = completed, &completed
	attempt.Status, attempt.FailureClass, attempt.Retryable, attempt.ErrorMessage, attempt.CompletedAt =
		BatchFailed, classified.Class, classified.Retryable, classified.Message, &completed
	result := OperationResult{Batch: batch}
	attempt.Result = &result
	stored, replayed, err := s.store.CreatePreview(ctx, batch, nil, attempt)
	if err != nil {
		return OperationResult{}, err
	}
	stored.Replay = replayed
	if replayed {
		attempt, err = s.store.FindAttempt(ctx, s.organizationID, OperationPreview, keyHash)
		if err != nil {
			return OperationResult{}, err
		}
	}
	if err := s.auditPreview(ctx, attempt, stored, keyHash); err != nil {
		return OperationResult{}, err
	}
	return stored, nil
}

func (s *Service) auditPreview(ctx context.Context, attempt Attempt, result OperationResult, keyHash string) error {
	action := "directory_import.previewed"
	if result.Batch.Status == BatchFailed {
		action = "directory_import.failed"
	}
	if err := s.audit(ctx, attempt, result.Batch, action, string(OperationPreview)+":"+keyHash); err != nil {
		return err
	}
	if result.Batch.Counts.Conflicts > 0 {
		return s.audit(ctx, attempt, result.Batch, "directory_import.conflict_reported", string(OperationPreview)+":"+keyHash)
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, authentication guard.Authentication, batchID, idempotencyKey string) (OperationResult, error) {
	return s.run(ctx, authentication, strings.TrimSpace(batchID), idempotencyKey, OperationApply)
}

func (s *Service) Retry(ctx context.Context, authentication guard.Authentication, batchID, idempotencyKey string) (OperationResult, error) {
	return s.run(ctx, authentication, strings.TrimSpace(batchID), idempotencyKey, OperationRetry)
}

func (s *Service) Get(ctx context.Context, batchID string) (BatchDetail, error) {
	if !isRecordID(strings.TrimSpace(batchID)) {
		return BatchDetail{}, ErrInvalidInput
	}
	detail, err := s.store.GetBatch(ctx, s.organizationID, batchID)
	if err != nil {
		return BatchDetail{}, err
	}
	for index := range detail.Attempts {
		detail.Attempts[index] = detail.Attempts[index].Public()
	}
	return detail, nil
}

func (s *Service) List(ctx context.Context, query ListQuery) (BatchPage, error) {
	if query.Limit == 0 {
		query.Limit = DefaultListLimit
	}
	query.Cursor = strings.TrimSpace(query.Cursor)
	if query.Limit < 1 || query.Limit > MaximumListLimit || (query.Cursor != "" && !isRecordID(query.Cursor)) {
		return BatchPage{}, ErrInvalidInput
	}
	if query.Cursor != "" {
		if _, err := s.store.GetBatch(ctx, s.organizationID, query.Cursor); err != nil {
			return BatchPage{}, err
		}
	}
	return s.store.ListBatches(ctx, s.organizationID, query)
}

func (s *Service) run(ctx context.Context, authentication guard.Authentication, batchID, idempotencyKey string, operation Operation) (OperationResult, error) {
	if err := validateActor(authentication); err != nil {
		return OperationResult{}, err
	}
	keyHash, err := validateIdempotencyKey(idempotencyKey)
	if err != nil || !isRecordID(batchID) {
		return OperationResult{}, ErrInvalidInput
	}
	fingerprint := digestStrings(string(operation), batchID)
	if replay, replayAttempt, ok, err := s.exactReplay(ctx, operation, keyHash, fingerprint); err != nil || ok {
		if ok {
			replay.Replay = true
			if auditErr := s.audit(ctx, replayAttempt, replay.Batch, auditAction(operation, replay.Batch), string(operation)+":"+keyHash); auditErr != nil {
				return OperationResult{}, auditErr
			}
			if replay.Batch.Counts.Conflicts > 0 {
				if auditErr := s.audit(ctx, replayAttempt, replay.Batch, "directory_import.conflict_reported", string(operation)+":"+keyHash); auditErr != nil {
					return OperationResult{}, auditErr
				}
			}
		}
		return replay, err
	}
	detail, err := s.store.GetBatch(ctx, s.organizationID, batchID)
	if err != nil {
		return OperationResult{}, err
	}
	existingAttempt, existingAttemptErr := s.store.FindAttempt(ctx, s.organizationID, operation, keyHash)
	resuming := existingAttemptErr == nil && existingAttempt.Result == nil && existingAttempt.RequestFingerprint == fingerprint
	if existingAttemptErr != nil && !errors.Is(existingAttemptErr, ErrNotFound) {
		return OperationResult{}, existingAttemptErr
	}
	if len(detail.Attempts) >= MaximumAttempts && !resuming {
		return OperationResult{}, ErrNotRetryable
	}
	if operation == OperationRetry && !resuming {
		for _, existing := range detail.Attempts {
			if existing.Operation == OperationRetry && existing.Result == nil {
				return OperationResult{}, ErrBusy
			}
		}
	}
	if operation == OperationApply && detail.Batch.Status != BatchPreviewed && !(resuming && detail.Batch.Status == BatchApplying) {
		return OperationResult{}, fmt.Errorf("%w: batch cannot be applied from status %s", ErrConflict, detail.Batch.Status)
	}
	retryingPreview := operation == OperationRetry && hasRetryableAttempt(detail.Attempts) &&
		(len(detail.Items) == 0 || (resuming && allItemsPending(detail.Items)))
	if operation == OperationRetry && !resuming && !retryingPreview && !hasRetryableFailure(detail.Items) {
		return OperationResult{}, ErrNotRetryable
	}
	attempt, err := s.newAttempt(ctx, authentication, batchID, operation, keyHash, fingerprint, len(detail.Attempts)+1)
	if err != nil {
		return OperationResult{}, err
	}
	if resuming {
		attempt = existingAttempt
	}
	leaseToken, err := foundation.NewCorrelationID()
	if err != nil {
		return OperationResult{}, err
	}
	leaseStartedAt := s.now()
	detail, replay, err := s.store.BeginOperation(ctx, s.organizationID, batchID, attempt, leaseToken, leaseStartedAt, leaseStartedAt.Add(leaseDuration))
	if err != nil {
		return OperationResult{}, err
	}
	if replay != nil {
		replay.Replay = true
		replayAttempt, findErr := s.store.FindAttempt(ctx, s.organizationID, operation, keyHash)
		if findErr != nil {
			return OperationResult{}, findErr
		}
		if auditErr := s.audit(ctx, replayAttempt, replay.Batch, auditAction(operation, replay.Batch), string(operation)+":"+keyHash); auditErr != nil {
			return OperationResult{}, auditErr
		}
		if replay.Batch.Counts.Conflicts > 0 {
			if auditErr := s.audit(ctx, replayAttempt, replay.Batch, "directory_import.conflict_reported", string(operation)+":"+keyHash); auditErr != nil {
				return OperationResult{}, auditErr
			}
		}
		return *replay, nil
	}
	system := SourceSystem{ID: detail.Batch.SourceSystemID, Provider: detail.Batch.Provider, ConfigRevision: detail.Batch.ConfigRevision}
	if retryingPreview {
		if len(detail.Items) > 0 {
			return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, detail.Batch, detail.Items, true, nil)
		}
		return s.retryPreview(ctx, attempt, leaseToken, keyHash, detail.Batch, system)
	}
	for _, item := range detail.Items {
		if operation == OperationRetry && (item.Outcome != OutcomeFailed || !item.Retryable) {
			continue
		}
		if operation == OperationApply && item.Outcome != OutcomePending {
			continue
		}
		item.UpdatedAt = s.now()
		if item.Action == ActionConflict {
			item.Outcome, item.FailureClass, item.ErrorMessage = OutcomeConflict, FailureConflict, safeMessage(item.ErrorMessage, "source and target changes conflict")
			if err := s.store.SaveItem(ctx, s.organizationID, batchID, leaseToken, item, nil); err != nil {
				return OperationResult{}, err
			}
			continue
		}
		applied, applyErr := s.target.Apply(ctx, authentication, system, item)
		if applyErr == nil && !validTargetResult(item, applied) {
			applyErr = &ClassifiedError{Class: FailurePermanent, Message: "target returned an invalid reconciliation result"}
		}
		if applyErr != nil {
			classified := classify(applyErr)
			item.Outcome = OutcomeFailed
			if classified.Class == FailureConflict {
				item.Outcome = OutcomeConflict
			}
			item.FailureClass, item.Retryable, item.ErrorMessage = classified.Class, classified.Retryable, classified.Message
			if err := s.store.SaveItem(ctx, s.organizationID, batchID, leaseToken, item, nil); err != nil {
				return OperationResult{}, err
			}
			continue
		}
		if item.Action == ActionUnchanged || !applied.Changed {
			item.Outcome = OutcomeUnchanged
		} else {
			item.Outcome = OutcomeApplied
		}
		item.Retryable, item.FailureClass, item.ErrorMessage = false, "", ""
		mapping := &Mapping{OrganizationID: s.organizationID, SourceSystemID: system.ID, Provider: system.Provider,
			SourceRecordID: item.Record.SourceRecordID, Kind: item.Record.Kind, TargetID: applied.TargetID,
			SourceDigest: item.SourceDigest, AppliedTargetDigest: applied.Digest, LastRecord: item.Record,
			Active: item.Record.Status != "inactive", LastSeenBatchID: batchID, LastAppliedBatchID: batchID, UpdatedAt: s.now()}
		if err := s.store.SaveItem(ctx, s.organizationID, batchID, leaseToken, item, mapping); err != nil {
			// A lease mismatch means another worker has taken over this exact
			// attempt. Its forward-recovery path owns the target now; rolling the
			// target back here could delete state the winning worker just mapped.
			if errors.Is(err, ErrLeaseLost) {
				return OperationResult{}, err
			}
			committed, verificationErr := s.itemMappingCommitted(ctx, batchID, item, *mapping)
			if committed {
				continue
			}
			// A store outage can make commit success unknowable. Leave the
			// deterministic, write-locked target in place for forward recovery
			// instead of risking deletion of an already-committed mapping.
			if verificationErr != nil {
				return OperationResult{}, errors.Join(err, verificationErr)
			}
			compensationErr := s.target.Compensate(ctx, authentication, system, item, applied)
			return OperationResult{}, errors.Join(err, compensationErr)
		}
	}
	updated, err := s.store.GetBatch(ctx, s.organizationID, batchID)
	if err != nil {
		return OperationResult{}, err
	}
	updated.Batch.Counts = countItems(updated.Items)
	updated.Batch.UpdatedAt = s.now()
	completed := updated.Batch.UpdatedAt
	updated.Batch.CompletedAt = &completed
	if updated.Batch.Counts.Failed > 0 || updated.Batch.Counts.Conflicts > 0 {
		if updated.Batch.Counts.Failed == len(updated.Items) {
			updated.Batch.Status = BatchFailed
		} else {
			updated.Batch.Status = BatchPartial
		}
	} else {
		updated.Batch.Status = BatchApplied
	}
	result := OperationResult{Batch: updated.Batch}
	classifyAttempt(&attempt, updated.Items)
	attempt.Status, attempt.CompletedAt, attempt.Result = updated.Batch.Status, &completed, &result
	if err := s.store.FinishOperation(ctx, s.organizationID, batchID, leaseToken, attempt, result); err != nil {
		return OperationResult{}, err
	}
	if err := s.audit(ctx, attempt, result.Batch, auditAction(operation, result.Batch), string(operation)+":"+keyHash); err != nil {
		return OperationResult{}, err
	}
	if result.Batch.Counts.Conflicts > 0 {
		if err := s.audit(ctx, attempt, result.Batch, "directory_import.conflict_reported", string(operation)+":"+keyHash); err != nil {
			return OperationResult{}, err
		}
	}
	return result, nil
}

func (s *Service) itemMappingCommitted(ctx context.Context, batchID string, item Item, expected Mapping) (bool, error) {
	detail, err := s.store.GetBatch(ctx, s.organizationID, batchID)
	if err != nil {
		return false, err
	}
	itemCommitted := false
	for _, stored := range detail.Items {
		if stored.ID == item.ID {
			itemCommitted = stored.Outcome == item.Outcome && stored.FailureClass == item.FailureClass && stored.Retryable == item.Retryable
			break
		}
	}
	if !itemCommitted {
		return false, nil
	}
	mappings, err := s.store.ListMappings(ctx, s.organizationID, expected.SourceSystemID)
	if err != nil {
		return false, err
	}
	for _, stored := range mappings {
		if stored.SourceRecordID == expected.SourceRecordID {
			return stored.TargetID == expected.TargetID && stored.SourceDigest == expected.SourceDigest &&
				stored.AppliedTargetDigest == expected.AppliedTargetDigest && stored.LastAppliedBatchID == batchID, nil
		}
	}
	return false, nil
}

func (s *Service) retryPreview(ctx context.Context, attempt Attempt, leaseToken, keyHash string, batch Batch, expectedSystem SourceSystem) (OperationResult, error) {
	connector, ok := s.registry.Connector(expectedSystem.ID)
	if !ok {
		return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, batch, nil, false,
			&ClassifiedError{Class: FailurePermanent, Message: "directory source system is no longer configured", Cause: ErrConnectorMissing})
	}
	currentSystem := connector.SourceSystem()
	if currentSystem.Provider != expectedSystem.Provider || currentSystem.ConfigRevision != expectedSystem.ConfigRevision {
		return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, batch, nil, false,
			&ClassifiedError{Class: FailurePermanent, Message: "directory source configuration changed after the failed preview", Cause: ErrConflict})
	}
	records, complete, err := s.pull(ctx, connector)
	if err != nil {
		return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, batch, nil, false, classify(err))
	}
	batch.CompleteSnapshot = complete
	items, err := s.plan(ctx, batch, expectedSystem, records)
	if err != nil {
		return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, batch, nil, false, classify(err))
	}
	if err := s.store.SavePlan(ctx, s.organizationID, batch.ID, leaseToken, batch.CompleteSnapshot, items); err != nil {
		return OperationResult{}, err
	}
	return s.finishPreviewRetry(ctx, attempt, leaseToken, keyHash, batch, items, true, nil)
}

func (s *Service) finishPreviewRetry(ctx context.Context, attempt Attempt, leaseToken, keyHash string, batch Batch, items []Item, recovered bool, failure *ClassifiedError) (OperationResult, error) {
	completed := s.now()
	batch.UpdatedAt, batch.CompletedAt = completed, &completed
	if recovered {
		batch.Status, batch.Counts = BatchPreviewed, countItems(items)
		batch.Counts.Failed = 0
		attempt.Status, attempt.FailureClass, attempt.Retryable, attempt.ErrorMessage = BatchPreviewed, "", false, ""
	} else {
		failure = classify(failure)
		batch.Status, batch.CompleteSnapshot, batch.Counts = BatchFailed, false, Counts{Failed: 1}
		attempt.Status, attempt.FailureClass, attempt.Retryable, attempt.ErrorMessage = BatchFailed, failure.Class, failure.Retryable, failure.Message
	}
	result := OperationResult{Batch: batch}
	attempt.CompletedAt, attempt.Result = &completed, &result
	if err := s.store.FinishOperation(ctx, s.organizationID, batch.ID, leaseToken, attempt, result); err != nil {
		return OperationResult{}, err
	}
	action := "directory_import.failed"
	if recovered {
		action = "directory_import.previewed"
	}
	if err := s.audit(ctx, attempt, result.Batch, action, string(OperationRetry)+":"+keyHash); err != nil {
		return OperationResult{}, err
	}
	if result.Batch.Counts.Conflicts > 0 {
		if err := s.audit(ctx, attempt, result.Batch, "directory_import.conflict_reported", string(OperationRetry)+":"+keyHash); err != nil {
			return OperationResult{}, err
		}
	}
	return result, nil
}

func (s *Service) pull(ctx context.Context, connector Connector) ([]Record, bool, error) {
	cursor := ""
	seenCursors := map[string]struct{}{}
	seenRecords := map[string]struct{}{}
	records := make([]Record, 0)
	for pageNumber := 0; pageNumber < s.maxPages; pageNumber++ {
		page, err := connector.PullPage(ctx, cursor)
		if err != nil {
			return nil, false, classify(err)
		}
		for _, record := range page.Records {
			normalized, err := normalizeRecord(record)
			if err != nil {
				return nil, false, err
			}
			if _, duplicate := seenRecords[normalized.SourceRecordID]; duplicate {
				return nil, false, fmt.Errorf("%w: duplicate source record", ErrConflict)
			}
			seenRecords[normalized.SourceRecordID] = struct{}{}
			records = append(records, normalized)
			if len(records) > s.maxRecords {
				return nil, false, fmt.Errorf("%w: source exceeds record limit", ErrInvalidInput)
			}
		}
		next := strings.TrimSpace(page.NextCursor)
		if next == "" {
			return records, page.CompleteSnapshot, nil
		}
		if len(next) > 1024 {
			return nil, false, fmt.Errorf("%w: connector cursor exceeds limit", ErrInvalidInput)
		}
		if _, duplicate := seenCursors[next]; duplicate {
			return nil, false, fmt.Errorf("%w: connector repeated a cursor", ErrConflict)
		}
		seenCursors[next] = struct{}{}
		cursor = next
	}
	return nil, false, fmt.Errorf("%w: source exceeds page limit", ErrInvalidInput)
}

func (s *Service) plan(ctx context.Context, batch Batch, system SourceSystem, records []Record) ([]Item, error) {
	mappings, err := s.store.ListMappings(ctx, s.organizationID, system.ID)
	if err != nil {
		return nil, err
	}
	bySource := make(map[string]Mapping, len(mappings))
	for _, mapping := range mappings {
		bySource[mapping.SourceRecordID] = mapping
	}
	items := make([]Item, 0, len(records)+len(mappings))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		seen[record.SourceRecordID] = struct{}{}
		mapping, hasMapping := bySource[record.SourceRecordID]
		var mappingPtr *Mapping
		if hasMapping {
			mappingCopy := mapping
			mappingPtr = &mappingCopy
		}
		item, err := s.planRecord(ctx, batch, system, record, mappingPtr, len(items))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if batch.CompleteSnapshot {
		sort.Slice(mappings, func(i, j int) bool { return mappings[i].SourceRecordID < mappings[j].SourceRecordID })
		for _, mapping := range mappings {
			if !mapping.Active {
				continue
			}
			if _, present := seen[mapping.SourceRecordID]; present {
				continue
			}
			if len(items) >= s.maxRecords {
				return nil, fmt.Errorf("%w: persisted plan exceeds item limit", ErrInvalidInput)
			}
			record := mapping.LastRecord
			record.Status = "inactive"
			mappingCopy := mapping
			item, err := s.planRecord(ctx, batch, system, record, &mappingCopy, len(items))
			if err != nil {
				return nil, err
			}
			if item.Action == ActionUpdate {
				item.Action = ActionDeactivate
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *Service) planRecord(ctx context.Context, batch Batch, system SourceSystem, record Record, mapping *Mapping, ordinal int) (Item, error) {
	plan, err := s.target.Preview(ctx, s.organizationID, system, record, mapping)
	if err != nil {
		return Item{}, err
	}
	if !validTargetPlan(plan) {
		return Item{}, errors.New("directory target returned an invalid reconciliation plan")
	}
	sourceDigest := digestJSON(record)
	action := ActionCreate
	message := ""
	if plan.Conflict {
		action, message = ActionConflict, safeMessage(plan.ConflictReason, "target conflicts with the source record")
	} else if mapping != nil {
		sourceChanged := mapping.SourceDigest != sourceDigest
		targetChanged := mapping.AppliedTargetDigest != plan.CurrentDigest
		switch {
		case !plan.Found:
			action, message = ActionConflict, "mapped target no longer exists"
		case targetChanged:
			action, message = ActionConflict, "managed target changed locally"
		case sourceChanged:
			action = ActionUpdate
		default:
			action = ActionUnchanged
		}
	} else if plan.Found && plan.SourceMatched {
		if plan.CurrentDigest == plan.DesiredDigest {
			action = ActionUnchanged
		} else {
			action = ActionUpdate
		}
	} else if plan.Found {
		action, message = ActionConflict, "target record already exists"
	}
	itemID := digestStrings(batch.ID, record.SourceRecordID)[:32]
	return Item{ID: itemID, OrganizationID: s.organizationID, BatchID: batch.ID, Ordinal: ordinal, Record: record,
		TargetID: plan.TargetID, ExpectedRevision: plan.Revision, SourceDigest: sourceDigest,
		ObservedTargetDigest: plan.CurrentDigest, PlannedTargetDigest: plan.DesiredDigest,
		Action: action, Outcome: OutcomePending, ErrorMessage: message, UpdatedAt: s.now()}, nil
}

func (s *Service) exactReplay(ctx context.Context, operation Operation, keyHash, fingerprint string) (OperationResult, Attempt, bool, error) {
	attempt, err := s.store.FindAttempt(ctx, s.organizationID, operation, keyHash)
	if errors.Is(err, ErrNotFound) {
		return OperationResult{}, Attempt{}, false, nil
	}
	if err != nil {
		return OperationResult{}, Attempt{}, false, err
	}
	if attempt.RequestFingerprint != fingerprint {
		return OperationResult{}, Attempt{}, false, fmt.Errorf("%w: idempotency key was used for another request", ErrConflict)
	}
	if attempt.Result == nil {
		detail, err := s.store.GetBatch(ctx, s.organizationID, attempt.BatchID)
		if err != nil {
			return OperationResult{}, Attempt{}, false, err
		}
		if detail.Batch.LeaseToken != "" && detail.Batch.LeaseExpiresAt != nil && detail.Batch.LeaseExpiresAt.After(s.now()) {
			return OperationResult{}, Attempt{}, false, ErrBusy
		}
		return OperationResult{}, attempt, false, nil
	}
	return *attempt.Result, attempt, true, nil
}

func (s *Service) newAttempt(ctx context.Context, authentication guard.Authentication, batchID string, operation Operation, keyHash, fingerprint string, number int) (Attempt, error) {
	attemptID, err := foundation.NewCorrelationID()
	if err != nil {
		return Attempt{}, err
	}
	correlationID := attemptID
	if scope, ok := foundation.ScopeFromContext(ctx); ok && validCorrelationID(scope.CorrelationID) {
		correlationID = strings.TrimSpace(scope.CorrelationID)
	}
	return Attempt{ID: attemptID, OrganizationID: s.organizationID, BatchID: batchID, Operation: operation,
		IdempotencyHash: keyHash, RequestFingerprint: fingerprint, Number: number, Status: BatchApplying,
		ActorID: strings.TrimSpace(authentication.Principal.Subject), CorrelationID: correlationID, StartedAt: s.now()}, nil
}

func (s *Service) audit(ctx context.Context, attempt Attempt, batch Batch, action, operationKey string) error {
	actorID := attempt.ActorID
	correlationID := attempt.CorrelationID
	requirementID := RequirementID
	if batch.Provider == GrouperProvider {
		requirementID = GrouperRequirementID
	}
	eventID := digestStrings(requirementID, batch.ID, action, operationKey)[:32]
	return s.auditor.Record(foundation.WithScope(ctx, foundation.Scope{OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: correlationID}), foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorID, CorrelationID: correlationID,
		Action: action, ResourceType: "directory_import_batch", ResourceID: batch.ID, OccurredAt: batch.UpdatedAt,
		Metadata: map[string]string{"requirementId": requirementID, "provider": string(batch.Provider), "status": string(batch.Status)},
	})
}

func normalizeRecord(record Record) (Record, error) {
	record.SourceRecordID = strings.TrimSpace(record.SourceRecordID)
	record.DisplayName = strings.TrimSpace(record.DisplayName)
	record.Email = strings.ToLower(strings.TrimSpace(record.Email))
	record.Status = strings.ToLower(strings.TrimSpace(record.Status))
	record.IdentityKind = strings.ToLower(strings.TrimSpace(record.IdentityKind))
	record.Department = strings.TrimSpace(record.Department)
	record.GroupName = strings.TrimSpace(record.GroupName)
	record.Description = strings.TrimSpace(record.Description)
	record.GroupSourceID = strings.TrimSpace(record.GroupSourceID)
	record.MemberSourceID = strings.TrimSpace(record.MemberSourceID)
	record.MemberKind = MemberKind(strings.ToLower(strings.TrimSpace(string(record.MemberKind))))
	if record.Kind == "" {
		record.Kind = RecordIdentity
	}
	if record.Kind == RecordIdentity && record.IdentityKind == "" {
		record.IdentityKind = "person"
	}
	if record.Status == "" {
		record.Status = "active"
	}
	if !validSourceRecordID(record.SourceRecordID) ||
		record.DisplayName == "" || !utf8.ValidString(record.DisplayName) || utf8.RuneCountInString(record.DisplayName) > 200 ||
		(record.Status != "active" && record.Status != "inactive") {
		return Record{}, fmt.Errorf("%w: normalized record is invalid", ErrInvalidInput)
	}
	switch record.Kind {
	case RecordIdentity:
		if record.IdentityKind != "person" && record.IdentityKind != "shared" && record.IdentityKind != "public" && record.IdentityKind != "lab" ||
			record.GroupName != "" || record.Description != "" || record.GroupSourceID != "" || record.MemberSourceID != "" ||
			record.MemberKind != "" || len(record.NormalizedMetadata) != 0 {
			return Record{}, fmt.Errorf("%w: normalized identity is invalid", ErrInvalidInput)
		}
		if record.Email != "" {
			address, err := mail.ParseAddress(record.Email)
			if err != nil || address.Address != record.Email || len(record.Email) > 320 {
				return Record{}, fmt.Errorf("%w: normalized email is invalid", ErrInvalidInput)
			}
		}
		if record.IdentityKind == "person" && record.Email == "" {
			return Record{}, fmt.Errorf("%w: person email is required", ErrInvalidInput)
		}
		if !validBoundedText(record.Department, 200) {
			return Record{}, fmt.Errorf("%w: normalized department is invalid", ErrInvalidInput)
		}
		if len(record.DirectoryAttributes) > MaximumAttributes {
			return Record{}, fmt.Errorf("%w: normalized attributes exceed limit", ErrInvalidInput)
		}
		normalizedAttributes := make(map[string]string, len(record.DirectoryAttributes))
		for key, value := range record.DirectoryAttributes {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if !providerNamePattern.MatchString(key) || value == "" || !validBoundedText(value, 500) {
				return Record{}, fmt.Errorf("%w: normalized directory attribute is invalid", ErrInvalidInput)
			}
			if _, duplicate := normalizedAttributes[key]; duplicate {
				return Record{}, fmt.Errorf("%w: normalized directory attribute is duplicated", ErrInvalidInput)
			}
			normalizedAttributes[key] = value
		}
		if len(normalizedAttributes) == 0 {
			record.DirectoryAttributes = nil
		} else {
			record.DirectoryAttributes = normalizedAttributes
		}
		if len(record.GroupSourceIDs) > MaximumGroupLinks {
			return Record{}, fmt.Errorf("%w: normalized group memberships exceed limit", ErrInvalidInput)
		}
		groups := make([]string, 0, len(record.GroupSourceIDs))
		seenGroups := make(map[string]struct{}, len(record.GroupSourceIDs))
		for _, groupID := range record.GroupSourceIDs {
			groupID = strings.TrimSpace(groupID)
			if !validSourceRecordID(groupID) {
				return Record{}, fmt.Errorf("%w: normalized group membership is invalid", ErrInvalidInput)
			}
			if _, duplicate := seenGroups[groupID]; duplicate {
				return Record{}, fmt.Errorf("%w: normalized group membership is duplicated", ErrConflict)
			}
			seenGroups[groupID] = struct{}{}
			groups = append(groups, groupID)
		}
		sort.Strings(groups)
		if len(groups) == 0 {
			record.GroupSourceIDs = nil
		} else {
			record.GroupSourceIDs = groups
		}
	case RecordGroup:
		if record.IdentityKind != "" || record.Email != "" || record.Department != "" ||
			len(record.DirectoryAttributes) != 0 || len(record.GroupSourceIDs) != 0 ||
			!validRequiredGrouperText(record.GroupName, 512) || !validOptionalGrouperText(record.Description, 2000) ||
			record.GroupSourceID != "" || record.MemberSourceID != "" || record.MemberKind != "" {
			return Record{}, fmt.Errorf("%w: normalized group is invalid", ErrInvalidInput)
		}
	case RecordMembership:
		if record.IdentityKind != "" || record.Email != "" || record.Department != "" ||
			len(record.DirectoryAttributes) != 0 || len(record.GroupSourceIDs) != 0 ||
			record.GroupName != "" || record.Description != "" ||
			!validSourceRecordID(record.GroupSourceID) || !validSourceRecordID(record.MemberSourceID) ||
			(record.MemberKind != MemberSubject && record.MemberKind != MemberGroup) {
			return Record{}, fmt.Errorf("%w: normalized membership is invalid", ErrInvalidInput)
		}
	default:
		return Record{}, fmt.Errorf("%w: normalized record kind is invalid", ErrInvalidInput)
	}
	metadata, err := normalizeMetadata(record.NormalizedMetadata)
	if err != nil {
		return Record{}, err
	}
	record.NormalizedMetadata = metadata
	return record, nil
}

func normalizeMetadata(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > 16 {
		return nil, fmt.Errorf("%w: normalized metadata exceeds field limit", ErrInvalidInput)
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !providerNamePattern.MatchString(key) || !validOptionalGrouperText(value, 500) {
			return nil, fmt.Errorf("%w: normalized metadata is invalid", ErrInvalidInput)
		}
		if _, duplicate := normalized[key]; duplicate {
			return nil, fmt.Errorf("%w: normalized metadata contains a duplicate key", ErrConflict)
		}
		normalized[key] = value
	}
	return normalized, nil
}

func validBoundedText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validRequiredGrouperText(value string, maximum int) bool {
	return value != "" && validOptionalGrouperText(value, maximum)
}

func validOptionalGrouperText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\t' {
			return false
		}
	}
	return true
}

func validSourceRecordID(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 255 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validateActor(authentication guard.Authentication) error {
	actorID := strings.TrimSpace(authentication.Principal.Subject)
	if actorID == "" || !utf8.ValidString(actorID) || utf8.RuneCountInString(actorID) > 128 {
		return fmt.Errorf("%w: authenticated actor is required", ErrInvalidInput)
	}
	return nil
}

func validCorrelationID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validateIdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 200 || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	return digestStrings(value), nil
}

func digestJSON(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func digestStrings(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
func isRecordID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validTargetPlan(plan TargetPlan) bool {
	if !isRecordID(plan.TargetID) || !isDigest(plan.DesiredDigest) || (plan.SourceMatched && !plan.Found) {
		return false
	}
	if plan.Found {
		return plan.Revision > 0 && isDigest(plan.CurrentDigest)
	}
	return plan.Revision == 0 && plan.CurrentDigest == ""
}

func validTargetResult(item Item, result TargetResult) bool {
	return result.TargetID == item.TargetID && isRecordID(result.TargetID) && result.Revision > 0 && isDigest(result.Digest)
}

func classify(err error) *ClassifiedError {
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		copy := *classified
		copy.Message = safeMessage(copy.Message, "directory operation failed")
		return &copy
	}
	if errors.Is(err, ErrConflict) {
		return &ClassifiedError{Class: FailureConflict, Message: "target conflicts with the persisted plan", Cause: err}
	}
	return &ClassifiedError{Class: FailurePermanent, Message: "directory operation failed", Cause: err}
}

func safeMessage(value, fallback string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if value == "" {
		value = fallback
	}
	runes := []rune(value)
	if len(runes) > 240 {
		value = string(runes[:240])
	}
	return value
}

func countItems(items []Item) Counts {
	var counts Counts
	for _, item := range items {
		switch item.Action {
		case ActionCreate:
			counts.Created++
		case ActionUpdate:
			counts.Updated++
		case ActionDeactivate:
			counts.Deactivated++
		case ActionUnchanged:
			counts.Unchanged++
		case ActionConflict:
			counts.Conflicts++
		}
		if item.Action != ActionConflict && (item.Outcome == OutcomeConflict || item.FailureClass == FailureConflict) {
			counts.Conflicts++
		}
		if item.Outcome == OutcomeFailed {
			counts.Failed++
		}
	}
	return counts
}

func hasRetryableFailure(items []Item) bool {
	for _, item := range items {
		if item.Outcome == OutcomeFailed && item.Retryable {
			return true
		}
	}
	return false
}

func allItemsPending(items []Item) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item.Outcome != OutcomePending {
			return false
		}
	}
	return true
}

func hasRetryableAttempt(attempts []Attempt) bool {
	for index := len(attempts) - 1; index >= 0; index-- {
		if attempts[index].Result != nil {
			return attempts[index].Retryable
		}
	}
	return false
}

func classifyAttempt(attempt *Attempt, items []Item) {
	if attempt == nil {
		return
	}
	for _, item := range items {
		if item.Outcome == OutcomeFailed {
			attempt.FailureClass = item.FailureClass
			attempt.Retryable = attempt.Retryable || item.Retryable
			attempt.ErrorMessage = "one or more directory actions failed"
			if item.Retryable {
				attempt.FailureClass = FailureTransient
			}
		}
		if item.Outcome == OutcomeConflict && attempt.ErrorMessage == "" {
			attempt.FailureClass = FailureConflict
			attempt.ErrorMessage = "one or more directory actions conflict with authoritative state"
		}
	}
}
func auditAction(operation Operation, batch Batch) string {
	if batch.Counts.Failed > 0 {
		return "directory_import.failed"
	}
	if operation == OperationRetry {
		if batch.Status == BatchPreviewed {
			return "directory_import.previewed"
		}
		return "directory_import.retried"
	}
	return "directory_import.applied"
}
