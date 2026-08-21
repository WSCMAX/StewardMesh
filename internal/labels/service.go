package labels

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var recordTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$`)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	records        RecordValidator
	goals          GoalValidator
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, records RecordValidator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, records, nil, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, records RecordValidator, goals GoalValidator, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || records == nil || auditor == nil {
		return nil, nil, errors.New("Labels store, record validator, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, errors.New("Labels organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	service := &Service{
		store: store, records: records, goals: goals, writes: writes, auditor: auditor,
		organizationID: configuration.OrganizationID, now: configuration.Now,
	}
	return service, &exchangeImporter{service: service}, nil
}

func (s *Service) OwnsExchangeImporter(importer ExchangeImporter) bool {
	typed, ok := importer.(*exchangeImporter)
	return ok && typed.service == s
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.store.Snapshot(ctx, s.organizationID)
}

func (s *Service) ListDefinitions(ctx context.Context) ([]Definition, error) {
	return s.store.ListDefinitions(ctx, s.organizationID)
}

func (s *Service) GetDefinition(ctx context.Context, id string) (Definition, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Definition{}, ErrInvalidInput
	}
	return s.store.GetDefinition(ctx, s.organizationID, id)
}

func (s *Service) CreateDefinition(ctx context.Context, input CreateDefinitionInput) (Definition, error) {
	normalized, err := normalizeDefinitionInput(input)
	if err != nil {
		return Definition{}, err
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Definition{}, fmt.Errorf("create label definition id: %w", err)
		}
	}
	if err := s.checkWrite(ctx, "labels.definition", id); err != nil {
		return Definition{}, err
	}
	if err := s.validateDefinitionRelations(ctx, id, normalized.ParentID, normalized.GoalID); err != nil {
		return Definition{}, err
	}
	now, revision := s.creationState(ctx)
	created, err := s.store.CreateDefinition(ctx, Definition{
		ID: id, OrganizationID: s.organizationID, Name: normalized.Name, Description: normalized.Description,
		ValueKind: normalized.ValueKind, ApplicableRecordTypes: normalized.ApplicableRecordTypes,
		Options: normalized.Options, ParentID: normalized.ParentID, GoalID: normalized.GoalID,
		Status: StatusActive, Revision: revision, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Definition{}, err
	}
	if err := s.audit(ctx, "labels.definition.created", "label_definition", created.ID, map[string]string{
		"valueKind": string(created.ValueKind), "targets": fmt.Sprintf("%d", len(created.ApplicableRecordTypes)),
	}); err != nil {
		return Definition{}, fmt.Errorf("audit label definition creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateDefinition(ctx context.Context, input UpdateDefinitionInput) (Definition, error) {
	normalized, err := normalizeDefinitionUpdate(input)
	if err != nil {
		return Definition{}, err
	}
	if err := s.checkWrite(ctx, "labels.definition", normalized.ID); err != nil {
		return Definition{}, err
	}
	existing, err := s.store.GetDefinition(ctx, s.organizationID, normalized.ID)
	if err != nil {
		return Definition{}, err
	}
	if existing.Revision != normalized.Revision {
		return Definition{}, ErrConflict
	}
	if normalized.ParentID == normalized.ID {
		return Definition{}, ErrInvalidInput
	}
	if err := s.validateDefinitionRelations(ctx, normalized.ID, normalized.ParentID, normalized.GoalID); err != nil {
		return Definition{}, err
	}
	now := s.now().UTC()
	updated, err := s.store.UpdateDefinition(ctx, Definition{
		ID: normalized.ID, OrganizationID: s.organizationID, Name: normalized.Name, Description: normalized.Description,
		ValueKind: normalized.ValueKind, ApplicableRecordTypes: normalized.ApplicableRecordTypes,
		Options: normalized.Options, ParentID: normalized.ParentID, GoalID: normalized.GoalID, Status: normalized.Status,
		Revision: existing.Revision + 1, CreatedAt: existing.CreatedAt, UpdatedAt: now,
	}, existing.Revision)
	if err != nil {
		return Definition{}, err
	}
	if err := s.audit(ctx, "labels.definition.updated", "label_definition", updated.ID, map[string]string{
		"status": string(updated.Status),
	}); err != nil {
		return Definition{}, fmt.Errorf("audit label definition update: %w", err)
	}
	return updated, nil
}

func (s *Service) PreviewDeleteDefinition(ctx context.Context, id string) (DeleteDefinitionPreview, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return DeleteDefinitionPreview{}, ErrInvalidInput
	}
	definition, err := s.store.GetDefinition(ctx, s.organizationID, id)
	if err != nil {
		return DeleteDefinitionPreview{}, err
	}
	allDefinitions, err := s.store.ListDefinitions(ctx, s.organizationID)
	if err != nil {
		return DeleteDefinitionPreview{}, err
	}
	children := directChildDefinitions(allDefinitions, id)
	descendants := collectDescendantIDs(allDefinitions, id)
	orphanIDs := []string{id}
	cascadeIDs := append([]string{id}, descendants...)
	orphanImpact, err := s.deleteImpact(ctx, allDefinitions, orphanIDs)
	if err != nil {
		return DeleteDefinitionPreview{}, err
	}
	cascadeImpact, err := s.deleteImpact(ctx, allDefinitions, cascadeIDs)
	if err != nil {
		return DeleteDefinitionPreview{}, err
	}
	childRefs := make([]DefinitionRef, 0, len(children))
	for _, child := range children {
		childRefs = append(childRefs, DefinitionRef{ID: child.ID, Name: child.Name})
	}
	return DeleteDefinitionPreview{
		Definition:            DefinitionRef{ID: definition.ID, Name: definition.Name},
		ChildDefinitions:      childRefs,
		HasChildren:           len(children) > 0,
		OrphanChildrenOption:  orphanImpact,
		CascadeChildrenOption: cascadeImpact,
	}, nil
}

func (s *Service) DeleteDefinition(ctx context.Context, input DeleteDefinitionInput) error {
	input.ID = strings.TrimSpace(input.ID)
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 {
		return ErrInvalidInput
	}
	if !input.Confirm {
		return ErrConfirmationRequired
	}
	mode := DeleteDefinitionMode(strings.ToLower(strings.TrimSpace(string(input.Mode))))
	if mode == "" {
		mode = DeleteModeStrict
	}
	switch mode {
	case DeleteModeStrict, DeleteModeOrphanChildren, DeleteModeCascadeChildren:
	default:
		return ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "labels.definition", input.ID); err != nil {
		return err
	}
	existing, err := s.store.GetDefinition(ctx, s.organizationID, input.ID)
	if err != nil {
		return err
	}
	if existing.Revision != input.Revision {
		return ErrConflict
	}
	allDefinitions, err := s.store.ListDefinitions(ctx, s.organizationID)
	if err != nil {
		return err
	}
	children := directChildDefinitions(allDefinitions, input.ID)
	if len(children) > 0 && mode == DeleteModeStrict {
		return ErrHasChildren
	}
	deleteIDs := []string{input.ID}
	orphanRemaining := false
	if len(children) > 0 {
		switch mode {
		case DeleteModeOrphanChildren:
			orphanRemaining = true
		case DeleteModeCascadeChildren:
			deleteIDs = append(deleteIDs, collectDescendantIDs(allDefinitions, input.ID)...)
		}
	}
	deleteIDs = definitionDeleteOrder(allDefinitions, deleteIDs)
	if err := s.store.DeleteDefinitions(ctx, s.organizationID, input.ID, input.Revision, deleteIDs, orphanRemaining); err != nil {
		return err
	}
	return s.audit(ctx, "labels.definition.deleted", "label_definition", input.ID, map[string]string{
		"mode": string(mode), "definitionsRemoved": fmt.Sprint(len(deleteIDs)),
	})
}

func (s *Service) deleteImpact(ctx context.Context, definitions []Definition, deleteIDs []string) (DeleteDefinitionImpact, error) {
	names := definitionNameIndex(definitions)
	removed := make([]DefinitionRef, 0, len(deleteIDs))
	for _, id := range deleteIDs {
		removed = append(removed, DefinitionRef{ID: id, Name: names[id]})
	}
	assignments := make([]AffectedAssignment, 0)
	for _, id := range deleteIDs {
		items, err := s.store.ListAssignmentsForDefinition(ctx, s.organizationID, id)
		if err != nil {
			return DeleteDefinitionImpact{}, err
		}
		for _, item := range items {
			assignments = append(assignments, AffectedAssignment{
				DefinitionID: id, DefinitionName: names[id], RecordType: item.RecordType, RecordID: item.RecordID,
			})
		}
	}
	sort.Slice(assignments, func(i, j int) bool {
		left, right := assignments[i], assignments[j]
		return left.RecordType+left.RecordID+left.DefinitionID < right.RecordType+right.RecordID+right.DefinitionID
	})
	return DeleteDefinitionImpact{DefinitionsRemoved: removed, AssignmentsRemoved: assignments}, nil
}

func directChildDefinitions(definitions []Definition, parentID string) []Definition {
	children := make([]Definition, 0)
	for _, definition := range definitions {
		if definition.ParentID == parentID {
			children = append(children, definition)
		}
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
	return children
}

func collectDescendantIDs(definitions []Definition, rootID string) []string {
	byParent := map[string][]string{}
	for _, definition := range definitions {
		if definition.ParentID != "" {
			byParent[definition.ParentID] = append(byParent[definition.ParentID], definition.ID)
		}
	}
	result := make([]string, 0)
	var walk func(id string)
	walk = func(id string) {
		for _, childID := range byParent[id] {
			result = append(result, childID)
			walk(childID)
		}
	}
	walk(rootID)
	return result
}

func definitionNameIndex(definitions []Definition) map[string]string {
	names := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		names[definition.ID] = definition.Name
	}
	return names
}

func definitionDeleteOrder(definitions []Definition, deleteIDs []string) []string {
	idSet := make(map[string]struct{}, len(deleteIDs))
	for _, id := range deleteIDs {
		idSet[id] = struct{}{}
	}
	byID := make(map[string]Definition, len(deleteIDs))
	for _, definition := range definitions {
		if _, ok := idSet[definition.ID]; ok {
			byID[definition.ID] = definition
		}
	}
	depth := func(id string) int {
		depth := 0
		current, ok := byID[id]
		for ok && current.ParentID != "" {
			if _, inSet := idSet[current.ParentID]; !inSet {
				break
			}
			depth++
			current, ok = byID[current.ParentID]
		}
		return depth
	}
	ordered := append([]string(nil), deleteIDs...)
	sort.Slice(ordered, func(i, j int) bool { return depth(ordered[i]) > depth(ordered[j]) })
	return ordered
}

func (s *Service) ListAssignments(ctx context.Context, recordType, recordID string) ([]Assignment, error) {
	recordType = strings.TrimSpace(recordType)
	recordID = strings.TrimSpace(recordID)
	if !recordTypePattern.MatchString(recordType) || !stableIDPattern.MatchString(recordID) {
		return nil, ErrInvalidInput
	}
	return s.store.ListAssignments(ctx, s.organizationID, recordType, recordID)
}

func (s *Service) ListAssignmentsForDefinition(ctx context.Context, definitionID, recordType string) ([]Assignment, error) {
	definitionID = strings.TrimSpace(definitionID)
	recordType = strings.TrimSpace(recordType)
	if !stableIDPattern.MatchString(definitionID) {
		return nil, ErrInvalidInput
	}
	if recordType != "" && !recordTypePattern.MatchString(recordType) {
		return nil, ErrInvalidInput
	}
	items, err := s.store.ListAssignmentsForDefinition(ctx, s.organizationID, definitionID)
	if err != nil {
		return nil, err
	}
	if recordType == "" {
		return items, nil
	}
	filtered := make([]Assignment, 0, len(items))
	for _, item := range items {
		if item.RecordType == recordType {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) SetAssignment(ctx context.Context, input SetAssignmentInput) (Assignment, error) {
	normalized, err := normalizeAssignmentInput(input)
	if err != nil {
		return Assignment{}, err
	}
	definition, err := s.store.GetDefinition(ctx, s.organizationID, normalized.DefinitionID)
	if err != nil {
		return Assignment{}, err
	}
	if definition.Status != StatusActive {
		return Assignment{}, ErrConflict
	}
	if !definitionAppliesTo(definition, normalized.RecordType) {
		return Assignment{}, ErrInvalidInput
	}
	value, err := NormalizeValue(definition, normalized.ValueText, normalized.Values)
	if err != nil {
		return Assignment{}, err
	}
	if err := s.records.ValidateRecord(ctx, s.organizationID, normalized.RecordType, normalized.RecordID); err != nil {
		return Assignment{}, err
	}
	resourceID := assignmentResourceID(normalized.DefinitionID, normalized.RecordType, normalized.RecordID)
	if err := s.checkWrite(ctx, "labels.assignment", resourceID); err != nil {
		return Assignment{}, err
	}
	now := s.now().UTC()
	actor := actorID(ctx)
	assignment := Assignment{
		OrganizationID: s.organizationID, DefinitionID: normalized.DefinitionID,
		RecordType: normalized.RecordType, RecordID: normalized.RecordID,
		ValueText: value.ValueText, Values: value.Values, UpdatedBy: actor,
		CreatedAt: now, UpdatedAt: now,
	}
	if normalized.Revision > 0 {
		assignment.Revision = normalized.Revision + 1
	} else {
		assignment.Revision = 1
	}
	created, err := s.store.PutAssignment(ctx, assignment, normalized.Revision)
	if err != nil {
		return Assignment{}, err
	}
	if err := s.audit(ctx, "labels.assignment.set", "label_assignment", resourceID, map[string]string{
		"definitionId": created.DefinitionID, "recordType": created.RecordType,
	}); err != nil {
		return Assignment{}, fmt.Errorf("audit label assignment: %w", err)
	}
	return created, nil
}

func (s *Service) DeleteAssignment(ctx context.Context, definitionID, recordType, recordID string, revision int64) error {
	definitionID = strings.TrimSpace(definitionID)
	recordType = strings.TrimSpace(recordType)
	recordID = strings.TrimSpace(recordID)
	if !stableIDPattern.MatchString(definitionID) || !recordTypePattern.MatchString(recordType) || !stableIDPattern.MatchString(recordID) || revision < 1 {
		return ErrInvalidInput
	}
	resourceID := assignmentResourceID(definitionID, recordType, recordID)
	if err := s.checkWrite(ctx, "labels.assignment", resourceID); err != nil {
		return err
	}
	if err := s.store.DeleteAssignment(ctx, s.organizationID, definitionID, recordType, recordID, revision); err != nil {
		return err
	}
	return s.audit(ctx, "labels.assignment.deleted", "label_assignment", resourceID, nil)
}

// NormalizeFieldValue validates a Patterns tag-field value against a definition.
func (s *Service) NormalizeFieldValue(ctx context.Context, definitionID string, raw any) (NormalizedValue, error) {
	definition, err := s.store.GetDefinition(ctx, s.organizationID, strings.TrimSpace(definitionID))
	if err != nil {
		return NormalizedValue{}, err
	}
	text, values, err := rawAssignmentParts(raw)
	if err != nil {
		return NormalizedValue{}, ErrInvalidInput
	}
	return NormalizeValue(definition, text, values)
}

func NormalizeValue(definition Definition, valueText string, values []string) (NormalizedValue, error) {
	switch definition.ValueKind {
	case ValueFlag:
		return NormalizedValue{}, nil
	case ValueText:
		valueText = strings.TrimSpace(valueText)
		if !validText(valueText, 1, 500) {
			return NormalizedValue{}, ErrInvalidInput
		}
		return NormalizedValue{ValueText: valueText}, nil
	case ValueSelect:
		valueText = strings.TrimSpace(valueText)
		if !optionAllowed(definition.Options, valueText) {
			return NormalizedValue{}, ErrInvalidInput
		}
		return NormalizedValue{ValueText: valueText}, nil
	case ValueMultiSelect:
		if len(values) == 0 && strings.TrimSpace(valueText) != "" {
			values = splitMultiValue(valueText)
		}
		normalized := normalizeOptions(values)
		if len(normalized) == 0 {
			return NormalizedValue{}, ErrInvalidInput
		}
		for _, value := range normalized {
			if !optionAllowed(definition.Options, value) {
				return NormalizedValue{}, ErrInvalidInput
			}
		}
		return NormalizedValue{Values: normalized}, nil
	default:
		return NormalizedValue{}, ErrInvalidInput
	}
}

func normalizeDefinitionInput(input CreateDefinitionInput) (CreateDefinitionInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.ParentID = strings.TrimSpace(input.ParentID)
	input.GoalID = strings.TrimSpace(input.GoalID)
	input.ValueKind = ValueKind(strings.ToLower(strings.TrimSpace(string(input.ValueKind))))
	input.ApplicableRecordTypes = normalizeRecordTypes(input.ApplicableRecordTypes)
	input.Options = normalizeOptions(input.Options)
	if input.ID != "" && !stableIDPattern.MatchString(input.ID) {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	if (input.ParentID != "" && !stableIDPattern.MatchString(input.ParentID)) ||
		(input.GoalID != "" && !stableIDPattern.MatchString(input.GoalID)) ||
		(input.ID != "" && input.ParentID == input.ID) {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	if !validText(input.Name, 1, 100) || !validText(input.Description, 0, 500) {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	if !validValueKind(input.ValueKind) || len(input.ApplicableRecordTypes) == 0 {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	if (input.ValueKind == ValueSelect || input.ValueKind == ValueMultiSelect) && len(input.Options) == 0 {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	if input.ValueKind == ValueFlag && len(input.Options) > 0 {
		return CreateDefinitionInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeDefinitionUpdate(input UpdateDefinitionInput) (UpdateDefinitionInput, error) {
	created, err := normalizeDefinitionInput(CreateDefinitionInput{
		ID: input.ID, Name: input.Name, Description: input.Description, ValueKind: input.ValueKind,
		ApplicableRecordTypes: input.ApplicableRecordTypes, Options: input.Options,
		ParentID: input.ParentID, GoalID: input.GoalID,
	})
	if err != nil {
		return UpdateDefinitionInput{}, err
	}
	input.ID = strings.TrimSpace(input.ID)
	input.Status = DefinitionStatus(strings.ToLower(strings.TrimSpace(string(input.Status))))
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 || (input.Status != StatusActive && input.Status != StatusRetired) {
		return UpdateDefinitionInput{}, ErrInvalidInput
	}
	input.Name = created.Name
	input.Description = created.Description
	input.ValueKind = created.ValueKind
	input.ApplicableRecordTypes = created.ApplicableRecordTypes
	input.Options = created.Options
	input.ParentID = created.ParentID
	input.GoalID = created.GoalID
	return input, nil
}

func (s *Service) validateDefinitionRelations(ctx context.Context, definitionID, parentID, goalID string) error {
	if parentID != "" {
		if definitionID != "" && parentID == definitionID {
			return ErrInvalidInput
		}
		if err := s.validateDefinitionParent(ctx, definitionID, parentID); err != nil {
			return err
		}
	}
	if goalID == "" {
		return nil
	}
	if s.goals == nil {
		return ErrInvalidInput
	}
	if err := s.goals.ValidateGoal(ctx, s.organizationID, goalID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Service) validateDefinitionParent(ctx context.Context, definitionID, parentID string) error {
	seen := map[string]struct{}{}
	if definitionID != "" {
		seen[definitionID] = struct{}{}
	}
	for parentID != "" {
		if _, exists := seen[parentID]; exists {
			return ErrCycle
		}
		seen[parentID] = struct{}{}
		parent, err := s.store.GetDefinition(ctx, s.organizationID, parentID)
		if err != nil {
			return err
		}
		parentID = parent.ParentID
	}
	return nil
}

func normalizeAssignmentInput(input SetAssignmentInput) (SetAssignmentInput, error) {
	input.DefinitionID = strings.TrimSpace(input.DefinitionID)
	input.RecordType = strings.TrimSpace(input.RecordType)
	input.RecordID = strings.TrimSpace(input.RecordID)
	input.ValueText = strings.TrimSpace(input.ValueText)
	input.Values = normalizeOptions(input.Values)
	if !stableIDPattern.MatchString(input.DefinitionID) || !recordTypePattern.MatchString(input.RecordType) ||
		!stableIDPattern.MatchString(input.RecordID) || input.Revision < 0 {
		return SetAssignmentInput{}, ErrInvalidInput
	}
	return input, nil
}

func definitionAppliesTo(definition Definition, recordType string) bool {
	for _, candidate := range definition.ApplicableRecordTypes {
		if candidate == recordType {
			return true
		}
	}
	return false
}

func normalizeRecordTypes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !recordTypePattern.MatchString(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeOptions(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !validText(value, 1, 100) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func splitMultiValue(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	return normalizeOptions(parts)
}

func rawAssignmentParts(raw any) (string, []string, error) {
	switch value := raw.(type) {
	case nil:
		return "", nil, nil
	case string:
		return strings.TrimSpace(value), nil, nil
	case []string:
		return "", normalizeOptions(value), nil
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return "", nil, errors.New("invalid tag list value")
			}
			parts = append(parts, text)
		}
		return "", normalizeOptions(parts), nil
	default:
		return "", nil, errors.New("unsupported tag value type")
	}
}

func optionAllowed(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}

func validValueKind(kind ValueKind) bool {
	return kind == ValueFlag || kind == ValueText || kind == ValueSelect || kind == ValueMultiSelect
}

func validText(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}

func AssignmentRecordID(definitionID, recordType, recordID string) string {
	return definitionID + ":" + recordType + ":" + recordID
}

func assignmentResourceID(definitionID, recordType, recordID string) string {
	return AssignmentRecordID(definitionID, recordType, recordID)
}

func (s *Service) checkWrite(ctx context.Context, resourceType, resourceID string) error {
	if s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, resourceType, resourceID)
}

func (s *Service) creationState(ctx context.Context) (time.Time, int64) {
	now := s.now().UTC()
	revision := int64(1)
	if correlation, ok := foundation.ScopeFromContext(ctx); ok && correlation.ActorID != "" {
		_ = correlation
	}
	return now, revision
}

func actorID(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return strings.TrimSpace(scope.ActorID)
	}
	return "system:labels"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	correlationID := ""
	actorIDValue := actorID(ctx)
	if scope, ok := foundation.ScopeFromContext(ctx); ok {
		correlationID = scope.CorrelationID
		if scope.ActorID != "" {
			actorIDValue = scope.ActorID
		}
	}
	if correlationID == "" {
		id, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		correlationID = id
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: correlationID, OrganizationID: s.organizationID, ActorID: actorIDValue, CorrelationID: correlationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata,
	})
}
