package repository

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/labels"
)

type MemoryLabelsStore struct {
	mu          sync.Mutex
	definitions map[string]map[string]labels.Definition
	assignments map[string]map[string]labels.Assignment
}

func NewMemoryLabelsStore() *MemoryLabelsStore {
	return &MemoryLabelsStore{
		definitions: map[string]map[string]labels.Definition{},
		assignments: map[string]map[string]labels.Assignment{},
	}
}

func assignmentKey(definitionID, recordType, recordID string) string {
	return definitionID + "\x00" + recordType + "\x00" + recordID
}

func (s *MemoryLabelsStore) Snapshot(ctx context.Context, organizationID string) (labels.Snapshot, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	result := labels.Snapshot{Definitions: []labels.Definition{}, Assignments: []labels.Assignment{}}
	for _, item := range s.definitions[organizationID] {
		result.Definitions = append(result.Definitions, item)
	}
	for _, item := range s.assignments[organizationID] {
		result.Assignments = append(result.Assignments, item)
	}
	sort.Slice(result.Definitions, func(i, j int) bool { return result.Definitions[i].Name < result.Definitions[j].Name })
	sort.Slice(result.Assignments, func(i, j int) bool {
		return result.Assignments[i].RecordType+result.Assignments[i].RecordID < result.Assignments[j].RecordType+result.Assignments[j].RecordID
	})
	return result, nil
}

func (s *MemoryLabelsStore) ListDefinitions(ctx context.Context, organizationID string) ([]labels.Definition, error) {
	snapshot, err := s.Snapshot(ctx, organizationID)
	return snapshot.Definitions, err
}

func (s *MemoryLabelsStore) GetDefinition(ctx context.Context, organizationID, id string) (labels.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.definitions[organizationID][id]
	if !ok {
		return labels.Definition{}, labels.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLabelsStore) CreateDefinition(ctx context.Context, definition labels.Definition) (labels.Definition, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.definitions[definition.OrganizationID] == nil {
		s.definitions[definition.OrganizationID] = map[string]labels.Definition{}
	}
	for _, item := range s.definitions[definition.OrganizationID] {
		if strings.EqualFold(item.Name, definition.Name) {
			return labels.Definition{}, labels.ErrConflict
		}
	}
	if _, exists := s.definitions[definition.OrganizationID][definition.ID]; exists {
		return labels.Definition{}, labels.ErrConflict
	}
	s.definitions[definition.OrganizationID][definition.ID] = definition
	return definition, nil
}

func (s *MemoryLabelsStore) UpdateDefinition(ctx context.Context, definition labels.Definition, expectedRevision int64) (labels.Definition, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.definitions[definition.OrganizationID][definition.ID]
	if !ok {
		return labels.Definition{}, labels.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return labels.Definition{}, labels.ErrConflict
	}
	for id, item := range s.definitions[definition.OrganizationID] {
		if id != definition.ID && strings.EqualFold(item.Name, definition.Name) {
			return labels.Definition{}, labels.ErrConflict
		}
	}
	s.definitions[definition.OrganizationID][definition.ID] = definition
	return definition, nil
}

func (s *MemoryLabelsStore) DeleteDefinitions(ctx context.Context, organizationID, rootID string, rootExpectedRevision int64, definitionIDs []string, orphanRemainingChildren bool) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(definitionIDs) == 0 {
		return labels.ErrInvalidInput
	}
	root, ok := s.definitions[organizationID][rootID]
	if !ok {
		return labels.ErrNotFound
	}
	if root.Revision != rootExpectedRevision {
		return labels.ErrConflict
	}
	idSet := make(map[string]struct{}, len(definitionIDs))
	for _, id := range definitionIDs {
		idSet[id] = struct{}{}
	}
	if orphanRemainingChildren {
		for id, definition := range s.definitions[organizationID] {
			if definition.ParentID == rootID {
				definition.ParentID = ""
				definition.Revision++
				s.definitions[organizationID][id] = definition
			}
		}
	}
	if s.assignments[organizationID] != nil {
		for key, assignment := range s.assignments[organizationID] {
			if _, remove := idSet[assignment.DefinitionID]; remove {
				delete(s.assignments[organizationID], key)
			}
		}
	}
	for _, id := range definitionIDs {
		if _, exists := s.definitions[organizationID][id]; !exists {
			return labels.ErrConflict
		}
	}
	for _, id := range definitionIDs {
		delete(s.definitions[organizationID], id)
	}
	return nil
}

func (s *MemoryLabelsStore) ListAssignments(ctx context.Context, organizationID, recordType, recordID string) ([]labels.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]labels.Assignment, 0)
	for _, item := range s.assignments[organizationID] {
		if item.RecordType == recordType && item.RecordID == recordID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DefinitionID < items[j].DefinitionID })
	return items, nil
}

func (s *MemoryLabelsStore) ListAssignmentsForDefinition(ctx context.Context, organizationID, definitionID string) ([]labels.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]labels.Assignment, 0)
	for _, item := range s.assignments[organizationID] {
		if item.DefinitionID == definitionID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *MemoryLabelsStore) GetAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string) (labels.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.assignments[organizationID][assignmentKey(definitionID, recordType, recordID)]
	if !ok {
		return labels.Assignment{}, labels.ErrNotFound
	}
	return item, nil
}

func (s *MemoryLabelsStore) PutAssignment(ctx context.Context, assignment labels.Assignment, expectedRevision int64) (labels.Assignment, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assignments[assignment.OrganizationID] == nil {
		s.assignments[assignment.OrganizationID] = map[string]labels.Assignment{}
	}
	key := assignmentKey(assignment.DefinitionID, assignment.RecordType, assignment.RecordID)
	if expectedRevision == 0 {
		if _, exists := s.assignments[assignment.OrganizationID][key]; exists {
			return labels.Assignment{}, labels.ErrConflict
		}
		s.assignments[assignment.OrganizationID][key] = assignment
		return assignment, nil
	}
	existing, ok := s.assignments[assignment.OrganizationID][key]
	if !ok || existing.Revision != expectedRevision {
		return labels.Assignment{}, labels.ErrConflict
	}
	s.assignments[assignment.OrganizationID][key] = assignment
	return assignment, nil
}

func (s *MemoryLabelsStore) DeleteAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := assignmentKey(definitionID, recordType, recordID)
	existing, ok := s.assignments[organizationID][key]
	if !ok {
		return labels.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return labels.ErrConflict
	}
	delete(s.assignments[organizationID], key)
	return nil
}
