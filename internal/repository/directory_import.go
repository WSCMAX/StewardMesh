package repository

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003.
// Features: integrations.protocols, identity.directory.

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/directoryexpansion"
)

type MemoryDirectoryImportStore struct {
	mu       sync.RWMutex
	batches  map[string]directoryexpansion.Batch
	items    map[string][]directoryexpansion.Item
	attempts map[string][]directoryexpansion.Attempt
	idem     map[string]directoryexpansion.Attempt
	mappings map[string]directoryexpansion.Mapping
}

var _ directoryexpansion.Store = (*MemoryDirectoryImportStore)(nil)

func NewMemoryDirectoryImportStore() *MemoryDirectoryImportStore {
	return &MemoryDirectoryImportStore{
		batches: make(map[string]directoryexpansion.Batch), items: make(map[string][]directoryexpansion.Item),
		attempts: make(map[string][]directoryexpansion.Attempt), idem: make(map[string]directoryexpansion.Attempt),
		mappings: make(map[string]directoryexpansion.Mapping),
	}
}

func (s *MemoryDirectoryImportStore) FindAttempt(_ context.Context, organizationID string, operation directoryexpansion.Operation, hash string) (directoryexpansion.Attempt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	attempt, ok := s.idem[idemKey(organizationID, operation, hash)]
	if !ok {
		return directoryexpansion.Attempt{}, directoryexpansion.ErrNotFound
	}
	return cloneAttempt(attempt), nil
}

func (s *MemoryDirectoryImportStore) CreatePreview(_ context.Context, batch directoryexpansion.Batch, items []directoryexpansion.Item, attempt directoryexpansion.Attempt) (directoryexpansion.OperationResult, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := idemKey(batch.OrganizationID, attempt.Operation, attempt.IdempotencyHash)
	if existing, ok := s.idem[key]; ok {
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrConflict
		}
		if existing.Result == nil {
			return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrBusy
		}
		result := cloneOperationResult(*existing.Result)
		return result, true, nil
	}
	if _, exists := s.batches[batch.ID]; exists {
		return directoryexpansion.OperationResult{}, false, directoryexpansion.ErrConflict
	}
	s.batches[batch.ID] = cloneBatch(batch)
	s.items[batch.ID] = cloneItems(items)
	s.attempts[batch.ID] = []directoryexpansion.Attempt{cloneAttempt(attempt)}
	s.idem[key] = cloneAttempt(attempt)
	return directoryexpansion.OperationResult{Batch: cloneBatch(batch)}, false, nil
}

func (s *MemoryDirectoryImportStore) GetBatch(_ context.Context, organizationID, batchID string) (directoryexpansion.BatchDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.detailLocked(organizationID, batchID)
}

func (s *MemoryDirectoryImportStore) ListBatches(_ context.Context, organizationID string, query directoryexpansion.ListQuery) (directoryexpansion.BatchPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	batches := make([]directoryexpansion.Batch, 0)
	for _, batch := range s.batches {
		if batch.OrganizationID == organizationID {
			batches = append(batches, cloneBatch(batch))
		}
	}
	sort.Slice(batches, func(i, j int) bool {
		if batches[i].CreatedAt.Equal(batches[j].CreatedAt) {
			return batches[i].ID > batches[j].ID
		}
		return batches[i].CreatedAt.After(batches[j].CreatedAt)
	})
	start := 0
	if query.Cursor != "" {
		for index := range batches {
			if batches[index].ID == query.Cursor {
				start = index + 1
				break
			}
		}
	}
	end := start + query.Limit
	if end > len(batches) {
		end = len(batches)
	}
	pageBatches := make([]directoryexpansion.Batch, end-start)
	copy(pageBatches, batches[start:end])
	page := directoryexpansion.BatchPage{Batches: pageBatches}
	if end < len(batches) && end > 0 {
		page.NextCursor = batches[end-1].ID
	}
	return page, nil
}

func (s *MemoryDirectoryImportStore) ListMappings(_ context.Context, organizationID, sourceSystemID string) ([]directoryexpansion.Mapping, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mappings := make([]directoryexpansion.Mapping, 0)
	for _, mapping := range s.mappings {
		if mapping.OrganizationID == organizationID && mapping.SourceSystemID == sourceSystemID {
			mappings = append(mappings, cloneMapping(mapping))
		}
	}
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].SourceRecordID < mappings[j].SourceRecordID })
	return mappings, nil
}

func (s *MemoryDirectoryImportStore) BeginOperation(_ context.Context, organizationID, batchID string, attempt directoryexpansion.Attempt, leaseToken string, leaseStartedAt, leaseUntil time.Time) (directoryexpansion.BatchDetail, *directoryexpansion.OperationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := idemKey(organizationID, attempt.Operation, attempt.IdempotencyHash)
	existingPending := false
	if existing, ok := s.idem[key]; ok {
		if existing.RequestFingerprint != attempt.RequestFingerprint {
			return directoryexpansion.BatchDetail{}, nil, directoryexpansion.ErrConflict
		}
		if existing.Result != nil {
			result := cloneOperationResult(*existing.Result)
			return directoryexpansion.BatchDetail{}, &result, nil
		}
		existingPending = true
	}
	batch, ok := s.batches[batchID]
	if !ok || batch.OrganizationID != organizationID {
		return directoryexpansion.BatchDetail{}, nil, directoryexpansion.ErrNotFound
	}
	if batch.LeaseToken != "" && batch.LeaseExpiresAt != nil && batch.LeaseExpiresAt.After(leaseStartedAt) {
		return directoryexpansion.BatchDetail{}, nil, directoryexpansion.ErrBusy
	}
	batch.LeaseToken, batch.LeaseExpiresAt, batch.Status, batch.UpdatedAt, batch.CompletedAt = leaseToken, &leaseUntil, directoryexpansion.BatchApplying, leaseStartedAt, nil
	s.batches[batchID] = batch
	if !existingPending {
		s.attempts[batchID] = append(s.attempts[batchID], cloneAttempt(attempt))
		s.idem[key] = cloneAttempt(attempt)
	}
	detail, err := s.detailLocked(organizationID, batchID)
	return detail, nil, err
}

func (s *MemoryDirectoryImportStore) SaveItem(_ context.Context, organizationID, batchID, leaseToken string, item directoryexpansion.Item, mapping *directoryexpansion.Mapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[batchID]
	if !ok || batch.OrganizationID != organizationID {
		return directoryexpansion.ErrNotFound
	}
	if batch.LeaseToken != leaseToken {
		return directoryexpansion.ErrLeaseLost
	}
	items := s.items[batchID]
	found := false
	for index := range items {
		if items[index].ID == item.ID {
			// The preview plan is immutable. Apply and retry may persist only
			// reconciliation outcome fields, matching the PostgreSQL UPDATE.
			items[index].Outcome = item.Outcome
			items[index].FailureClass = item.FailureClass
			items[index].Retryable = item.Retryable
			items[index].ErrorMessage = item.ErrorMessage
			items[index].UpdatedAt = item.UpdatedAt
			found = true
			break
		}
	}
	if !found {
		return directoryexpansion.ErrNotFound
	}
	s.items[batchID] = items
	if mapping != nil {
		s.mappings[mappingKey(mapping.OrganizationID, mapping.SourceSystemID, mapping.SourceRecordID)] = cloneMapping(*mapping)
	}
	return nil
}

func (s *MemoryDirectoryImportStore) SavePlan(_ context.Context, organizationID, batchID, leaseToken string, completeSnapshot bool, items []directoryexpansion.Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[batchID]
	if !ok || batch.OrganizationID != organizationID {
		return directoryexpansion.ErrNotFound
	}
	if batch.LeaseToken != leaseToken {
		return directoryexpansion.ErrLeaseLost
	}
	if len(s.items[batchID]) != 0 {
		return directoryexpansion.ErrConflict
	}
	batch.CompleteSnapshot = completeSnapshot
	s.batches[batchID] = batch
	s.items[batchID] = cloneItems(items)
	return nil
}

func (s *MemoryDirectoryImportStore) FinishOperation(_ context.Context, organizationID, batchID, leaseToken string, attempt directoryexpansion.Attempt, result directoryexpansion.OperationResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[batchID]
	if !ok || batch.OrganizationID != organizationID {
		return directoryexpansion.ErrNotFound
	}
	if batch.LeaseToken != leaseToken {
		return directoryexpansion.ErrLeaseLost
	}
	result.Batch.LeaseToken, result.Batch.LeaseExpiresAt = "", nil
	s.batches[batchID] = cloneBatch(result.Batch)
	attempt.Result = &result
	attempts := s.attempts[batchID]
	for index := range attempts {
		if attempts[index].ID == attempt.ID {
			attempts[index] = cloneAttempt(attempt)
			break
		}
	}
	s.attempts[batchID] = attempts
	s.idem[idemKey(organizationID, attempt.Operation, attempt.IdempotencyHash)] = cloneAttempt(attempt)
	return nil
}

func (s *MemoryDirectoryImportStore) detailLocked(organizationID, batchID string) (directoryexpansion.BatchDetail, error) {
	batch, ok := s.batches[batchID]
	if !ok || batch.OrganizationID != organizationID {
		return directoryexpansion.BatchDetail{}, directoryexpansion.ErrNotFound
	}
	return directoryexpansion.BatchDetail{Batch: cloneBatch(batch), Items: cloneItems(s.items[batchID]), Attempts: cloneAttempts(s.attempts[batchID])}, nil
}

func idemKey(org string, operation directoryexpansion.Operation, hash string) string {
	return org + "\x00" + string(operation) + "\x00" + hash
}
func mappingKey(org, source, record string) string { return org + "\x00" + source + "\x00" + record }
func cloneItems(items []directoryexpansion.Item) []directoryexpansion.Item {
	result := make([]directoryexpansion.Item, len(items))
	for index := range items {
		result[index] = items[index]
		result[index].Record = cloneRecord(items[index].Record)
	}
	return result
}

func cloneMapping(mapping directoryexpansion.Mapping) directoryexpansion.Mapping {
	mapping.LastRecord = cloneRecord(mapping.LastRecord)
	return mapping
}

func cloneRecord(record directoryexpansion.Record) directoryexpansion.Record {
	if record.DirectoryAttributes != nil {
		attributes := make(map[string]string, len(record.DirectoryAttributes))
		for key, value := range record.DirectoryAttributes {
			attributes[key] = value
		}
		record.DirectoryAttributes = attributes
	}
	if record.GroupSourceIDs != nil {
		record.GroupSourceIDs = append([]string(nil), record.GroupSourceIDs...)
	}
	return record
}
func cloneAttempts(attempts []directoryexpansion.Attempt) []directoryexpansion.Attempt {
	result := make([]directoryexpansion.Attempt, len(attempts))
	for index := range attempts {
		result[index] = cloneAttempt(attempts[index])
	}
	return result
}
func cloneAttempt(attempt directoryexpansion.Attempt) directoryexpansion.Attempt {
	if attempt.Result != nil {
		result := cloneOperationResult(*attempt.Result)
		attempt.Result = &result
	}
	return attempt
}
func cloneOperationResult(result directoryexpansion.OperationResult) directoryexpansion.OperationResult {
	result.Batch = cloneBatch(result.Batch)
	return result
}
func cloneBatch(batch directoryexpansion.Batch) directoryexpansion.Batch {
	if batch.LeaseExpiresAt != nil {
		leaseExpiresAt := *batch.LeaseExpiresAt
		batch.LeaseExpiresAt = &leaseExpiresAt
	}
	if batch.CompletedAt != nil {
		completedAt := *batch.CompletedAt
		batch.CompletedAt = &completedAt
	}
	return batch
}
