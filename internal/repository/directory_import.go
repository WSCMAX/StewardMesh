package repository

// Requirements: REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-003, REQ-DIRECTORY-EXPANSION-005, REQ-DIRECTORY-EXPANSION-008, REQ-EXCHANGE-001.
// Features: integrations.protocols, identity.directory, threads.relationships, migration.packages.

import (
	"context"
	"sort"
	"strings"
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
	groups   map[string]directoryexpansion.ManagedGroup
	members  map[string]directoryexpansion.ManagedMembership
	// Source indexes make authoritative connector rediscovery a bounded exact
	// lookup rather than an ambiguous scan across every managed record.
	groupSources  map[string]string
	memberSources map[string]string
}

var _ directoryexpansion.Store = (*MemoryDirectoryImportStore)(nil)
var _ directoryexpansion.GroupTargetStore = (*MemoryDirectoryImportStore)(nil)
var _ directoryexpansion.GroupExchangeStore = (*MemoryDirectoryImportStore)(nil)

func NewMemoryDirectoryImportStore() *MemoryDirectoryImportStore {
	return &MemoryDirectoryImportStore{
		batches: make(map[string]directoryexpansion.Batch), items: make(map[string][]directoryexpansion.Item),
		attempts: make(map[string][]directoryexpansion.Attempt), idem: make(map[string]directoryexpansion.Attempt),
		mappings: make(map[string]directoryexpansion.Mapping), groups: make(map[string]directoryexpansion.ManagedGroup),
		members: make(map[string]directoryexpansion.ManagedMembership), groupSources: make(map[string]string), memberSources: make(map[string]string),
	}
}

func (s *MemoryDirectoryImportStore) GetManagedGroup(_ context.Context, organizationID, id string) (directoryexpansion.ManagedGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, ok := s.groups[managedDirectoryKey(organizationID, id)]
	if !ok || group.OrganizationID != organizationID {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrNotFound
	}
	return cloneManagedGroup(group), nil
}

func (s *MemoryDirectoryImportStore) GetManagedGroupBySource(_ context.Context, organizationID, sourceSystemID, sourceRecordID string) (directoryexpansion.ManagedGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.groupSources[managedDirectorySourceKey(organizationID, sourceSystemID, sourceRecordID)]
	if !ok {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrNotFound
	}
	group, ok := s.groups[key]
	if !ok || group.OrganizationID != organizationID || group.SourceSystemID != sourceSystemID || group.SourceRecordID != sourceRecordID {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrNotFound
	}
	return cloneManagedGroup(group), nil
}

func (s *MemoryDirectoryImportStore) CreateManagedGroup(_ context.Context, group directoryexpansion.ManagedGroup) (directoryexpansion.ManagedGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(group.OrganizationID, group.ID)
	sourceKey := managedDirectorySourceKey(group.OrganizationID, group.SourceSystemID, group.SourceRecordID)
	if _, exists := s.groups[key]; exists {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrConflict
	}
	if _, exists := s.groupSources[sourceKey]; exists {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrConflict
	}
	s.groups[key] = cloneManagedGroup(group)
	s.groupSources[sourceKey] = key
	return cloneManagedGroup(group), nil
}

func (s *MemoryDirectoryImportStore) ReconcileManagedGroup(_ context.Context, group directoryexpansion.ManagedGroup, expectedRevision uint64) (directoryexpansion.ManagedGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(group.OrganizationID, group.ID)
	existing, ok := s.groups[key]
	if !ok || existing.OrganizationID != group.OrganizationID {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrNotFound
	}
	if existing.Revision != expectedRevision || group.Revision != expectedRevision+1 ||
		existing.SourceSystemID != group.SourceSystemID || existing.SourceRecordID != group.SourceRecordID {
		return directoryexpansion.ManagedGroup{}, directoryexpansion.ErrConflict
	}
	s.groups[key] = cloneManagedGroup(group)
	return cloneManagedGroup(group), nil
}

func (s *MemoryDirectoryImportStore) DeleteManagedGroup(_ context.Context, organizationID, id string, expectedRevision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(organizationID, id)
	existing, ok := s.groups[key]
	if !ok || existing.OrganizationID != organizationID {
		return directoryexpansion.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return directoryexpansion.ErrConflict
	}
	delete(s.groups, key)
	delete(s.groupSources, managedDirectorySourceKey(existing.OrganizationID, existing.SourceSystemID, existing.SourceRecordID))
	for membershipID, membership := range s.members {
		if membership.OrganizationID == organizationID && (membership.GroupID == id ||
			membership.MemberKind == directoryexpansion.MemberGroup && membership.MemberID == id) {
			delete(s.members, membershipID)
			delete(s.memberSources, managedDirectorySourceKey(membership.OrganizationID, membership.SourceSystemID, membership.SourceRecordID))
		}
	}
	return nil
}

func (s *MemoryDirectoryImportStore) GetManagedMembership(_ context.Context, organizationID, id string) (directoryexpansion.ManagedMembership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	membership, ok := s.members[managedDirectoryKey(organizationID, id)]
	if !ok || membership.OrganizationID != organizationID {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrNotFound
	}
	return cloneManagedMembership(membership), nil
}

func (s *MemoryDirectoryImportStore) GetManagedMembershipBySource(_ context.Context, organizationID, sourceSystemID, sourceRecordID string) (directoryexpansion.ManagedMembership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.memberSources[managedDirectorySourceKey(organizationID, sourceSystemID, sourceRecordID)]
	if !ok {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrNotFound
	}
	membership, ok := s.members[key]
	if !ok || membership.OrganizationID != organizationID || membership.SourceSystemID != sourceSystemID || membership.SourceRecordID != sourceRecordID {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrNotFound
	}
	return cloneManagedMembership(membership), nil
}

func (s *MemoryDirectoryImportStore) CreateManagedMembership(_ context.Context, membership directoryexpansion.ManagedMembership) (directoryexpansion.ManagedMembership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(membership.OrganizationID, membership.ID)
	sourceKey := managedDirectorySourceKey(membership.OrganizationID, membership.SourceSystemID, membership.SourceRecordID)
	if _, exists := s.members[key]; exists {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrConflict
	}
	if _, exists := s.memberSources[sourceKey]; exists {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrConflict
	}
	parent, exists := s.groups[managedDirectoryKey(membership.OrganizationID, membership.GroupID)]
	if !exists || parent.OrganizationID != membership.OrganizationID {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrConflict
	}
	s.members[key] = cloneManagedMembership(membership)
	s.memberSources[sourceKey] = key
	return cloneManagedMembership(membership), nil
}

func (s *MemoryDirectoryImportStore) ReconcileManagedMembership(_ context.Context, membership directoryexpansion.ManagedMembership, expectedRevision uint64) (directoryexpansion.ManagedMembership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(membership.OrganizationID, membership.ID)
	existing, ok := s.members[key]
	if !ok || existing.OrganizationID != membership.OrganizationID {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrNotFound
	}
	if existing.Revision != expectedRevision || membership.Revision != expectedRevision+1 ||
		existing.SourceSystemID != membership.SourceSystemID || existing.SourceRecordID != membership.SourceRecordID {
		return directoryexpansion.ManagedMembership{}, directoryexpansion.ErrConflict
	}
	s.members[key] = cloneManagedMembership(membership)
	return cloneManagedMembership(membership), nil
}

func (s *MemoryDirectoryImportStore) DeleteManagedMembership(_ context.Context, organizationID, id string, expectedRevision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(organizationID, id)
	existing, ok := s.members[key]
	if !ok || existing.OrganizationID != organizationID {
		return directoryexpansion.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return directoryexpansion.ErrConflict
	}
	delete(s.members, key)
	delete(s.memberSources, managedDirectorySourceKey(existing.OrganizationID, existing.SourceSystemID, existing.SourceRecordID))
	return nil
}

func (s *MemoryDirectoryImportStore) ListManagedGroups(_ context.Context, organizationID string) ([]directoryexpansion.ManagedGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups := make([]directoryexpansion.ManagedGroup, 0)
	for _, group := range s.groups {
		if group.OrganizationID == organizationID {
			groups = append(groups, cloneManagedGroup(group))
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups, nil
}

func (s *MemoryDirectoryImportStore) ListManagedMemberships(_ context.Context, organizationID string) ([]directoryexpansion.ManagedMembership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	memberships := make([]directoryexpansion.ManagedMembership, 0)
	for _, membership := range s.members {
		if membership.OrganizationID == organizationID {
			memberships = append(memberships, cloneManagedMembership(membership))
		}
	}
	sort.Slice(memberships, func(i, j int) bool { return memberships[i].ID < memberships[j].ID })
	return memberships, nil
}

func (s *MemoryDirectoryImportStore) ExchangeSnapshot(_ context.Context, organizationID string, maximum int) (directoryexpansion.ExchangeSnapshot, error) {
	if strings.TrimSpace(organizationID) == "" || maximum < 1 {
		return directoryexpansion.ExchangeSnapshot{}, directoryexpansion.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := directoryexpansion.ExchangeSnapshot{Groups: []directoryexpansion.ManagedGroup{}, Memberships: []directoryexpansion.ManagedMembership{}}
	for _, group := range s.groups {
		if group.OrganizationID == organizationID {
			result.Groups = append(result.Groups, cloneManagedGroup(group))
		}
	}
	for _, membership := range s.members {
		if membership.OrganizationID == organizationID {
			result.Memberships = append(result.Memberships, cloneManagedMembership(membership))
		}
	}
	if len(result.Groups)+len(result.Memberships) > maximum {
		return directoryexpansion.ExchangeSnapshot{}, directoryexpansion.ErrTooLarge
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].ID < result.Groups[j].ID })
	sort.Slice(result.Memberships, func(i, j int) bool { return result.Memberships[i].ID < result.Memberships[j].ID })
	return result, nil
}

func (s *MemoryDirectoryImportStore) ImportManagedGroup(_ context.Context, candidate directoryexpansion.ManagedGroup) (directoryexpansion.ManagedGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(candidate.OrganizationID, candidate.ID)
	if existing, ok := s.groups[key]; ok {
		return cloneManagedGroup(existing), false, nil
	}
	sourceKey := managedDirectorySourceKey(candidate.OrganizationID, candidate.SourceSystemID, candidate.SourceRecordID)
	if _, exists := s.groupSources[sourceKey]; exists {
		return directoryexpansion.ManagedGroup{}, false, directoryexpansion.ErrConflict
	}
	s.groups[key] = cloneManagedGroup(candidate)
	s.groupSources[sourceKey] = key
	return cloneManagedGroup(candidate), true, nil
}

func (s *MemoryDirectoryImportStore) ImportManagedMembership(_ context.Context, candidate directoryexpansion.ManagedMembership) (directoryexpansion.ManagedMembership, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := managedDirectoryKey(candidate.OrganizationID, candidate.ID)
	if existing, ok := s.members[key]; ok {
		return cloneManagedMembership(existing), false, nil
	}
	sourceKey := managedDirectorySourceKey(candidate.OrganizationID, candidate.SourceSystemID, candidate.SourceRecordID)
	if _, exists := s.memberSources[sourceKey]; exists {
		return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrConflict
	}
	parent, ok := s.groups[managedDirectoryKey(candidate.OrganizationID, candidate.GroupID)]
	if !ok || parent.OrganizationID != candidate.OrganizationID {
		return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrReferenceMissing
	}
	if parent.SourceSystemID != candidate.SourceSystemID || parent.SourceRecordID != candidate.GroupSourceID {
		return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrConflict
	}
	if candidate.MemberKind == directoryexpansion.MemberGroup {
		member, exists := s.groups[managedDirectoryKey(candidate.OrganizationID, candidate.MemberID)]
		if !exists || member.OrganizationID != candidate.OrganizationID {
			return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrReferenceMissing
		}
		if member.SourceSystemID != candidate.SourceSystemID || member.SourceRecordID != candidate.MemberSourceID {
			return directoryexpansion.ManagedMembership{}, false, directoryexpansion.ErrConflict
		}
	}
	s.members[key] = cloneManagedMembership(candidate)
	s.memberSources[sourceKey] = key
	return cloneManagedMembership(candidate), true, nil
}

func (s *MemoryDirectoryImportStore) ListGraphManagedGroups(_ context.Context, organizationID string, query directoryexpansion.ManagedGroupGraphQuery) ([]directoryexpansion.ManagedGroup, error) {
	if strings.TrimSpace(organizationID) == "" || !query.Valid() {
		return nil, directoryexpansion.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	label := strings.ToLower(query.LabelSearch)
	groups := make([]directoryexpansion.ManagedGroup, 0)
	for _, group := range s.groups {
		if group.OrganizationID != organizationID || group.Status != "active" ||
			label != "" && !strings.Contains(strings.ToLower(group.DisplayName), label) ||
			len(query.GroupIDs) > 0 && !sliceContains(query.GroupIDs, group.ID) {
			continue
		}
		groups = append(groups, cloneManagedGroup(group))
	}
	sort.Slice(groups, func(i, j int) bool {
		left, right := strings.ToLower(groups[i].DisplayName), strings.ToLower(groups[j].DisplayName)
		if left == right {
			return groups[i].ID < groups[j].ID
		}
		return left < right
	})
	if len(groups) > query.Limit {
		groups = groups[:query.Limit]
	}
	return groups, nil
}

func (s *MemoryDirectoryImportStore) ListGraphManagedMemberships(_ context.Context, organizationID string, query directoryexpansion.ManagedMembershipGraphQuery) ([]directoryexpansion.ManagedMembership, error) {
	if strings.TrimSpace(organizationID) == "" || !query.Valid() {
		return nil, directoryexpansion.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	label := strings.ToLower(query.LabelSearch)
	memberships := make([]directoryexpansion.ManagedMembership, 0)
	for _, membership := range s.members {
		selected := len(query.GroupIDs)+len(query.MemberIDs) == 0 ||
			sliceContains(query.GroupIDs, membership.GroupID) || sliceContains(query.MemberIDs, membership.MemberID)
		if membership.OrganizationID != organizationID || membership.Status != "active" || !selected ||
			label != "" && !strings.Contains(strings.ToLower(membership.MemberDisplayName), label) {
			continue
		}
		memberships = append(memberships, cloneManagedMembership(membership))
	}
	sort.Slice(memberships, func(i, j int) bool {
		left, right := strings.ToLower(memberships[i].MemberDisplayName), strings.ToLower(memberships[j].MemberDisplayName)
		if left == right {
			return memberships[i].ID < memberships[j].ID
		}
		return left < right
	})
	if len(memberships) > query.Limit {
		memberships = memberships[:query.Limit]
	}
	return memberships, nil
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
func mappingKey(org, source, record string) string         { return org + "\x00" + source + "\x00" + record }
func managedDirectoryKey(organizationID, id string) string { return organizationID + "\x00" + id }
func managedDirectorySourceKey(organizationID, sourceSystemID, sourceRecordID string) string {
	return organizationID + "\x00" + sourceSystemID + "\x00" + sourceRecordID
}
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
	record.NormalizedMetadata = cloneDirectoryMetadata(record.NormalizedMetadata)
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

func cloneManagedGroup(group directoryexpansion.ManagedGroup) directoryexpansion.ManagedGroup {
	group.Metadata = cloneDirectoryMetadata(group.Metadata)
	return group
}

func cloneManagedMembership(membership directoryexpansion.ManagedMembership) directoryexpansion.ManagedMembership {
	membership.Metadata = cloneDirectoryMetadata(membership.Metadata)
	return membership
}

func cloneDirectoryMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
