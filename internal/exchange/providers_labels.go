package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/labels"
)

var labelsRecordTypes = []string{"labels.definition", "labels.assignment"}

type LabelsProvider struct {
	service  *labels.Service
	importer labels.ExchangeImporter
}

type labelsDefinitionPayload struct {
	Name                  string `json:"name"`
	Description           string `json:"description,omitempty"`
	ValueKind             string `json:"valueKind"`
	ApplicableRecordTypes string `json:"applicableRecordTypes"`
	Options               string `json:"options,omitempty"`
	ParentID              string `json:"parentId,omitempty"`
	GoalID                string `json:"goalId,omitempty"`
	Status                string `json:"status,omitempty"`
}

type labelsAssignmentPayload struct {
	DefinitionID string `json:"definitionId"`
	RecordType   string `json:"recordType"`
	RecordID     string `json:"recordId"`
	ValueText    string `json:"valueText,omitempty"`
	Values       string `json:"values,omitempty"`
}

func NewLabelsProvider(service *labels.Service, importer labels.ExchangeImporter) (*LabelsProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Labels service and its construction-time Exchange importer are required")
	}
	return &LabelsProvider{service: service, importer: importer}, nil
}

func (*LabelsProvider) Types() []string { return append([]string(nil), labelsRecordTypes...) }

func (p *LabelsProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Definitions)+len(snapshot.Assignments) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(snapshot.Definitions)+len(snapshot.Assignments))
	for _, item := range snapshot.Definitions {
		payload, err := marshalLabelsPayload(labelsDefinitionPayload{
			Name: item.Name, Description: item.Description, ValueKind: string(item.ValueKind),
			ApplicableRecordTypes: strings.Join(item.ApplicableRecordTypes, ","),
			Options: strings.Join(item.Options, ","), ParentID: item.ParentID, GoalID: item.GoalID,
			Status: string(item.Status),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{
			Type: "labels.definition", ID: item.ID, Revision: item.Revision,
			Dependencies: labelsDefinitionDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload,
		})
	}
	for _, item := range snapshot.Assignments {
		payload, err := marshalLabelsPayload(labelsAssignmentPayload{
			DefinitionID: item.DefinitionID, RecordType: item.RecordType, RecordID: item.RecordID,
			ValueText: item.ValueText, Values: strings.Join(item.Values, ","),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{
			Type: "labels.assignment", ID: labels.AssignmentRecordID(item.DefinitionID, item.RecordType, item.RecordID), Revision: item.Revision,
			Dependencies: labelsAssignmentDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *LabelsProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	switch reference.Type {
	case "labels.definition":
		_, err := p.service.GetDefinition(ctx, reference.ID)
		if errors.Is(err, labels.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	case "labels.assignment":
		records, err := p.ListRecords(ctx)
		if err != nil {
			return false, err
		}
		for _, record := range records {
			if record.Type == reference.Type && record.ID == reference.ID {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

func (p *LabelsProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeLabelsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch value := candidate.(type) {
	case labels.Definition:
		current, err := p.service.GetDefinition(ctx, record.ID)
		if errors.Is(err, labels.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameLabelsDefinition(current, value), err
	case labels.Assignment:
		snapshot, err := p.service.Snapshot(ctx)
		if err != nil {
			return false, err
		}
		for _, current := range snapshot.Assignments {
			if labels.AssignmentRecordID(current.DefinitionID, current.RecordType, current.RecordID) == record.ID {
				return sameLabelsAssignment(current, value), nil
			}
		}
		return false, nil
	default:
		return false, ErrInvalidInput
	}
}

func (p *LabelsProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
	if !operation.ExpectedCreated {
		exact, err := p.ImportRecordExists(ctx, record, nil)
		if err != nil {
			return ProviderImportResult{}, err
		}
		if !exact {
			return ProviderImportResult{}, ErrConflict
		}
		return ProviderImportResult{Committed: true}, nil
	}
	candidate, dependencies, err := decodeLabelsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := labels.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result labels.ExchangeImportResult
	switch value := candidate.(type) {
	case labels.Definition:
		result, err = p.importer.ImportDefinition(ctx, domainOperation, value)
	case labels.Assignment:
		result, err = p.importer.ImportAssignment(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, labels.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, labels.ErrConflict):
		return providerResult, ErrConflict
	case errors.Is(err, labels.ErrNotFound):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeLabelsRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "labels.definition":
		payload, err := decodeLabelsPayload[labelsDefinitionPayload](record.Payload)
		if err != nil || !labelsTextRange(payload.Name, 1, 100) || !labelsTextRange(payload.Description, 0, 500) ||
			payload.Name != strings.TrimSpace(payload.Name) || payload.Description != strings.TrimSpace(payload.Description) ||
			!validLabelsValueKind(payload.ValueKind) {
			return nil, nil, ErrInvalidInput
		}
		status := labels.StatusActive
		if payload.Status != "" {
			status = labels.DefinitionStatus(strings.ToLower(strings.TrimSpace(payload.Status)))
			if status != labels.StatusActive && status != labels.StatusRetired {
				return nil, nil, ErrInvalidInput
			}
		}
		value := labels.Definition{
			ID: record.ID, Name: payload.Name, Description: payload.Description,
			ValueKind: labels.ValueKind(strings.ToLower(strings.TrimSpace(payload.ValueKind))),
			ApplicableRecordTypes: splitCSV(payload.ApplicableRecordTypes),
			Options: splitCSV(payload.Options), ParentID: strings.TrimSpace(payload.ParentID),
			GoalID: strings.TrimSpace(payload.GoalID), Status: status, Revision: record.Revision,
		}
		if len(value.ApplicableRecordTypes) == 0 {
			return nil, nil, ErrInvalidInput
		}
		if (value.ValueKind == labels.ValueSelect || value.ValueKind == labels.ValueMultiSelect) && len(value.Options) == 0 {
			return nil, nil, ErrInvalidInput
		}
		if (value.ParentID != "" && !stableIDPattern.MatchString(value.ParentID)) ||
			(value.GoalID != "" && !stableIDPattern.MatchString(value.GoalID)) ||
			(value.ParentID != "" && value.ParentID == value.ID) {
			return nil, nil, ErrInvalidInput
		}
		return value, labelsDefinitionDependencies(value), nil
	case "labels.assignment":
		payload, err := decodeLabelsPayload[labelsAssignmentPayload](record.Payload)
		value := labels.Assignment{
			DefinitionID: strings.TrimSpace(payload.DefinitionID), RecordType: strings.TrimSpace(payload.RecordType),
			RecordID: strings.TrimSpace(payload.RecordID), ValueText: strings.TrimSpace(payload.ValueText),
			Values: splitCSV(payload.Values), Revision: record.Revision,
		}
		if err != nil || !stableIDPattern.MatchString(value.DefinitionID) || !resourceTypePattern.MatchString(value.RecordType) ||
			!stableIDPattern.MatchString(value.RecordID) ||
			record.ID != labels.AssignmentRecordID(value.DefinitionID, value.RecordType, value.RecordID) {
			return nil, nil, ErrInvalidInput
		}
		return value, labelsAssignmentDependencies(value), nil
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodeLabelsPayload[T any](payload []byte) (T, error) {
	var result T
	if len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return result, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, ErrInvalidInput
	}
	if !canonicalJSONEqual(payload, result) {
		return result, ErrInvalidInput
	}
	return result, nil
}

func marshalLabelsPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func labelsDefinitionDependencies(value labels.Definition) []Reference {
	refs := make([]Reference, 0, 2)
	if value.ParentID != "" {
		refs = append(refs, Reference{Type: "labels.definition", ID: value.ParentID})
	}
	if value.GoalID != "" {
		refs = append(refs, Reference{Type: "threads.goal", ID: value.GoalID})
	}
	return normalizeReferences(refs)
}

func labelsAssignmentDependencies(value labels.Assignment) []Reference {
	return normalizeReferences([]Reference{
		{Type: "labels.definition", ID: value.DefinitionID},
		{Type: "stewardmesh.record", ID: value.RecordID},
	})
}

func labelsTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

func validLabelsValueKind(raw string) bool {
	switch labels.ValueKind(strings.ToLower(strings.TrimSpace(raw))) {
	case labels.ValueFlag, labels.ValueText, labels.ValueSelect, labels.ValueMultiSelect:
		return true
	default:
		return false
	}
}

func sameLabelsDefinition(left, right labels.Definition) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Description == right.Description &&
		left.ValueKind == right.ValueKind && left.ParentID == right.ParentID && left.GoalID == right.GoalID &&
		left.Status == right.Status && left.Revision == right.Revision &&
		slices.Equal(left.ApplicableRecordTypes, right.ApplicableRecordTypes) && slices.Equal(left.Options, right.Options)
}

func sameLabelsAssignment(left, right labels.Assignment) bool {
	return left.DefinitionID == right.DefinitionID && left.RecordType == right.RecordType && left.RecordID == right.RecordID &&
		left.ValueText == right.ValueText && slices.Equal(left.Values, right.Values) && left.Revision == right.Revision
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
