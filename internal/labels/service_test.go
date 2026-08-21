package labels

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

func TestDeleteDefinitionRequiresConfirmation(t *testing.T) {
	store := newTestLabelsStore()
	auditor := foundation.NopAuditor{}
	service, err := NewService(store, noopRecordValidator{}, auditor, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Program", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteDefinition(ctx, DeleteDefinitionInput{ID: created.ID, Revision: created.Revision, Confirm: false}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("expected confirmation required, got %v", err)
	}
}

func TestDeleteDefinitionRejectsParentWithChildrenUnlessModeSelected(t *testing.T) {
	store := newTestLabelsStore()
	auditor := foundation.NopAuditor{}
	service, err := NewService(store, noopRecordValidator{}, auditor, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	parent, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Parent", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Child", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"}, ParentID: parent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteDefinition(ctx, DeleteDefinitionInput{
		ID: parent.ID, Revision: parent.Revision, Confirm: true, Mode: DeleteModeStrict,
	}); !errors.Is(err, ErrHasChildren) {
		t.Fatalf("expected has children, got %v", err)
	}
	if err := service.DeleteDefinition(ctx, DeleteDefinitionInput{
		ID: parent.ID, Revision: parent.Revision, Confirm: true, Mode: DeleteModeOrphanChildren,
	}); err != nil {
		t.Fatalf("orphan delete failed: %v", err)
	}
	if _, err := service.GetDefinition(ctx, parent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected parent deleted, got %v", err)
	}
	remaining, err := service.GetDefinition(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if remaining.ParentID != "" {
		t.Fatalf("expected child orphaned, parentId=%q", remaining.ParentID)
	}
}

func TestDeleteDefinitionCascadeRemovesChildrenAndAssignments(t *testing.T) {
	store := newTestLabelsStore()
	auditor := foundation.NopAuditor{}
	service, err := NewService(store, noopRecordValidator{}, auditor, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	parent, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Parent", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Child", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"}, ParentID: parent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetAssignment(ctx, SetAssignmentInput{
		DefinitionID: child.ID, RecordType: "atlas.asset", RecordID: "asset-one", Revision: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteDefinition(ctx, DeleteDefinitionInput{
		ID: parent.ID, Revision: parent.Revision, Confirm: true, Mode: DeleteModeCascadeChildren,
	}); err != nil {
		t.Fatalf("cascade delete failed: %v", err)
	}
	for _, id := range []string{parent.ID, child.ID} {
		if _, err := service.GetDefinition(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected %s deleted, got %v", id, err)
		}
	}
	assignments, err := service.ListAssignments(ctx, "atlas.asset", "asset-one")
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 0 {
		t.Fatalf("expected assignments removed, got %d", len(assignments))
	}
}

func TestUpdateDefinitionRejectsDuplicateName(t *testing.T) {
	store := newTestLabelsStore()
	service, err := NewService(store, noopRecordValidator{}, foundation.NopAuditor{}, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Campus", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		Name: "Lab", ValueKind: ValueFlag, ApplicableRecordTypes: []string{"atlas.asset"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDefinition(ctx, UpdateDefinitionInput{
		ID: second.ID, Name: first.Name, ValueKind: second.ValueKind,
		ApplicableRecordTypes: second.ApplicableRecordTypes, Status: second.Status, Revision: second.Revision,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate rename conflict, got %v", err)
	}
}

type noopRecordValidator struct{}

func (noopRecordValidator) ValidateRecord(context.Context, string, string, string) error { return nil }

type testLabelsStore struct {
	mu          sync.Mutex
	definitions map[string]map[string]Definition
	assignments map[string]map[string]Assignment
}

func newTestLabelsStore() *testLabelsStore {
	return &testLabelsStore{
		definitions: map[string]map[string]Definition{},
		assignments: map[string]map[string]Assignment{},
	}
}

func (s *testLabelsStore) Snapshot(ctx context.Context, organizationID string) (Snapshot, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	result := Snapshot{Definitions: []Definition{}, Assignments: []Assignment{}}
	for _, item := range s.definitions[organizationID] {
		result.Definitions = append(result.Definitions, item)
	}
	for _, item := range s.assignments[organizationID] {
		result.Assignments = append(result.Assignments, item)
	}
	return result, nil
}

func (s *testLabelsStore) ListDefinitions(ctx context.Context, organizationID string) ([]Definition, error) {
	snapshot, err := s.Snapshot(ctx, organizationID)
	return snapshot.Definitions, err
}

func (s *testLabelsStore) GetDefinition(ctx context.Context, organizationID, id string) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.definitions[organizationID][id]
	if !ok {
		return Definition{}, ErrNotFound
	}
	return item, nil
}

func (s *testLabelsStore) CreateDefinition(ctx context.Context, definition Definition) (Definition, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.definitions[definition.OrganizationID] == nil {
		s.definitions[definition.OrganizationID] = map[string]Definition{}
	}
	for _, item := range s.definitions[definition.OrganizationID] {
		if strings.EqualFold(item.Name, definition.Name) {
			return Definition{}, ErrConflict
		}
	}
	if _, exists := s.definitions[definition.OrganizationID][definition.ID]; exists {
		return Definition{}, ErrConflict
	}
	s.definitions[definition.OrganizationID][definition.ID] = definition
	return definition, nil
}

func (s *testLabelsStore) UpdateDefinition(ctx context.Context, definition Definition, expectedRevision int64) (Definition, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.definitions[definition.OrganizationID][definition.ID]
	if !ok {
		return Definition{}, ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return Definition{}, ErrConflict
	}
	for id, item := range s.definitions[definition.OrganizationID] {
		if id != definition.ID && strings.EqualFold(item.Name, definition.Name) {
			return Definition{}, ErrConflict
		}
	}
	s.definitions[definition.OrganizationID][definition.ID] = definition
	return definition, nil
}

func (s *testLabelsStore) DeleteDefinitions(ctx context.Context, organizationID, rootID string, rootExpectedRevision int64, definitionIDs []string, orphanRemainingChildren bool) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	root, ok := s.definitions[organizationID][rootID]
	if !ok {
		return ErrNotFound
	}
	if root.Revision != rootExpectedRevision {
		return ErrConflict
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
	for key, assignment := range s.assignments[organizationID] {
		if _, remove := idSet[assignment.DefinitionID]; remove {
			delete(s.assignments[organizationID], key)
		}
	}
	for _, id := range definitionIDs {
		if _, exists := s.definitions[organizationID][id]; !exists {
			return ErrConflict
		}
		delete(s.definitions[organizationID], id)
	}
	return nil
}

func testAssignmentKey(definitionID, recordType, recordID string) string {
	return definitionID + "\x00" + recordType + "\x00" + recordID
}

func (s *testLabelsStore) ListAssignments(ctx context.Context, organizationID, recordType, recordID string) ([]Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Assignment, 0)
	for _, item := range s.assignments[organizationID] {
		if item.RecordType == recordType && item.RecordID == recordID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *testLabelsStore) ListAssignmentsForDefinition(ctx context.Context, organizationID, definitionID string) ([]Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Assignment, 0)
	for _, item := range s.assignments[organizationID] {
		if item.DefinitionID == definitionID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *testLabelsStore) GetAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string) (Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.assignments[organizationID][testAssignmentKey(definitionID, recordType, recordID)]
	if !ok {
		return Assignment{}, ErrNotFound
	}
	return item, nil
}

func (s *testLabelsStore) PutAssignment(ctx context.Context, assignment Assignment, expectedRevision int64) (Assignment, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assignments[assignment.OrganizationID] == nil {
		s.assignments[assignment.OrganizationID] = map[string]Assignment{}
	}
	key := testAssignmentKey(assignment.DefinitionID, assignment.RecordType, assignment.RecordID)
	if expectedRevision == 0 {
		s.assignments[assignment.OrganizationID][key] = assignment
		return assignment, nil
	}
	existing, ok := s.assignments[assignment.OrganizationID][key]
	if !ok || existing.Revision != expectedRevision {
		return Assignment{}, ErrConflict
	}
	s.assignments[assignment.OrganizationID][key] = assignment
	return assignment, nil
}

func (s *testLabelsStore) DeleteAssignment(ctx context.Context, organizationID, definitionID, recordType, recordID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := testAssignmentKey(definitionID, recordType, recordID)
	existing, ok := s.assignments[organizationID][key]
	if !ok {
		return ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return ErrConflict
	}
	delete(s.assignments[organizationID], key)
	return nil
}

func TestListAssignmentsForDefinitionFiltersRecordType(t *testing.T) {
	store := newTestLabelsStore()
	auditor := foundation.NopAuditor{}
	service, err := NewService(store, noopRecordValidator{}, auditor, ServiceConfig{OrganizationID: "org-one"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	definition, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		ID: "deployment-group", Name: "Deployment group", ValueKind: ValueMultiSelect,
		ApplicableRecordTypes: []string{"atlas.asset", "atlas.model"}, Options: []string{"Lab A"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetAssignment(ctx, SetAssignmentInput{
		DefinitionID: definition.ID, RecordType: "atlas.asset", RecordID: "asset-one", Values: []string{"Lab A"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetAssignment(ctx, SetAssignmentInput{
		DefinitionID: definition.ID, RecordType: "atlas.model", RecordID: "model-one", Values: []string{"Lab A"},
	}); err != nil {
		t.Fatal(err)
	}
	all, err := service.ListAssignmentsForDefinition(ctx, definition.ID, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("expected two assignments, got %#v err=%v", all, err)
	}
	filtered, err := service.ListAssignmentsForDefinition(ctx, definition.ID, "atlas.asset")
	if err != nil || len(filtered) != 1 || filtered[0].RecordID != "asset-one" {
		t.Fatalf("unexpected filtered assignments %#v err=%v", filtered, err)
	}
}
