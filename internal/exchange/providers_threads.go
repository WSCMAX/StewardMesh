package exchange

// Requirements: REQ-EXCHANGE-001, REQ-THREADS-001, REQ-PATTERNS-001. Features: migration.packages, goals.tags, templates.schemas. GitHub: #9.

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

	"github.com/maxlemke/stewardmesh/internal/threads"
)

var threadsRecordTypes = []string{"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link"}

type ThreadsProvider struct {
	service  *threads.Service
	importer threads.ExchangeImporter
}

type threadsTagPayload struct {
	Name        string `json:"name"`
	ParentID    string `json:"parentId,omitempty"`
	Inheritance string `json:"inheritance"`
}

type threadsGoalPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    string `json:"parentId,omitempty"`
}

type threadsTagRulePayload struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	TagID      string `json:"tagId"`
	Rule       string `json:"rule"`
}

type threadsGoalLinkPayload struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	GoalID     string `json:"goalId"`
}

func NewThreadsProvider(service *threads.Service, importer threads.ExchangeImporter) (*ThreadsProvider, error) {
	if service == nil || importer == nil || !service.OwnsExchangeImporter(importer) {
		return nil, errors.New("Threads service and its construction-time Exchange importer are required")
	}
	return &ThreadsProvider{service: service, importer: importer}, nil
}

func (*ThreadsProvider) Types() []string { return append([]string(nil), threadsRecordTypes...) }

func (p *ThreadsProvider) ListRecords(ctx context.Context) ([]Record, error) {
	snapshot, err := p.service.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Tags)+len(snapshot.Goals)+len(snapshot.TagRules)+len(snapshot.GoalLinks) > MaximumRecords {
		return nil, ErrTooLarge
	}
	result := make([]Record, 0, len(snapshot.Tags)+len(snapshot.Goals)+len(snapshot.TagRules)+len(snapshot.GoalLinks))
	for _, item := range snapshot.Tags {
		inheritance := "suppress"
		if item.InheritByDefault {
			inheritance = "include"
		}
		payload, err := marshalThreadsPayload(threadsTagPayload{Name: item.Name, ParentID: item.ParentID, Inheritance: inheritance})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "threads.tag", ID: item.ID, Revision: item.Revision,
			Dependencies: threadsParentDependency("threads.tag", item.ParentID), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.Goals {
		payload, err := marshalThreadsPayload(threadsGoalPayload{Name: item.Name, Description: item.Description, ParentID: item.ParentID})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "threads.goal", ID: item.ID, Revision: item.Revision,
			Dependencies: threadsParentDependency("threads.goal", item.ParentID), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.TagRules {
		payload, err := marshalThreadsPayload(threadsTagRulePayload{
			TargetType: string(item.TargetType), TargetID: item.TargetID, TagID: item.TagID, Rule: string(item.Mode),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "threads.tag-rule", ID: threads.TagRuleRecordID(item.TargetType, item.TargetID, item.TagID), Revision: item.Revision,
			Dependencies: threadsTagRuleDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	for _, item := range snapshot.GoalLinks {
		payload, err := marshalThreadsPayload(threadsGoalLinkPayload{
			TargetType: string(item.TargetType), TargetID: item.TargetID, GoalID: item.GoalID,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{Type: "threads.goal-link", ID: threads.GoalLinkRecordID(item.TargetType, item.TargetID, item.GoalID), Revision: 1,
			Dependencies: threadsGoalLinkDependencies(item), Ownership: OwnershipMetadata{State: "local"}, Payload: payload})
	}
	sort.Slice(result, func(i, j int) bool {
		return (Reference{Type: result[i].Type, ID: result[i].ID}).Key() < (Reference{Type: result[j].Type, ID: result[j].ID}).Key()
	})
	return result, nil
}

func (p *ThreadsProvider) Exists(ctx context.Context, reference Reference) (bool, error) {
	switch reference.Type {
	case "threads.tag":
		_, err := p.service.GetTag(ctx, reference.ID)
		if errors.Is(err, threads.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	case "threads.goal":
		_, err := p.service.GetGoal(ctx, reference.ID)
		if errors.Is(err, threads.ErrNotFound) {
			return false, nil
		}
		return err == nil, err
	case "threads.tag-rule", "threads.goal-link":
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

func (p *ThreadsProvider) ImportRecordExists(ctx context.Context, record Record, _ []byte) (bool, error) {
	candidate, dependencies, err := decodeThreadsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return false, ErrInvalidInput
	}
	switch value := candidate.(type) {
	case threads.Tag:
		current, err := p.service.GetTag(ctx, record.ID)
		if errors.Is(err, threads.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameThreadsTag(current, value), err
	case threads.Goal:
		current, err := p.service.GetGoal(ctx, record.ID)
		if errors.Is(err, threads.ErrNotFound) {
			return false, nil
		}
		return err == nil && sameThreadsGoal(current, value), err
	case threads.TagRule:
		snapshot, err := p.service.Snapshot(ctx)
		if err != nil {
			return false, err
		}
		for _, current := range snapshot.TagRules {
			if threads.TagRuleRecordID(current.TargetType, current.TargetID, current.TagID) == record.ID {
				return sameThreadsTagRule(current, value), nil
			}
		}
		return false, nil
	case threads.GoalLink:
		snapshot, err := p.service.Snapshot(ctx)
		if err != nil {
			return false, err
		}
		for _, current := range snapshot.GoalLinks {
			if threads.GoalLinkRecordID(current.TargetType, current.TargetID, current.GoalID) == record.ID {
				return sameThreadsGoalLink(current, value), nil
			}
		}
		return false, nil
	default:
		return false, ErrInvalidInput
	}
}

func (p *ThreadsProvider) ImportRecord(ctx context.Context, operation ProviderImportOperation, _ string, record Record, _ []byte) (ProviderImportResult, error) {
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
	candidate, dependencies, err := decodeThreadsRecord(record)
	if err != nil || !slices.Equal(dependencies, record.Dependencies) {
		return ProviderImportResult{}, ErrInvalidInput
	}
	domainOperation := threads.ExchangeImportOperation{Token: operation.Token, OccurredAt: operation.OccurredAt}
	var result threads.ExchangeImportResult
	switch value := candidate.(type) {
	case threads.Tag:
		result, err = p.importer.ImportTag(ctx, domainOperation, value)
	case threads.Goal:
		result, err = p.importer.ImportGoal(ctx, domainOperation, value)
	case threads.TagRule:
		result, err = p.importer.ImportTagRule(ctx, domainOperation, value)
	case threads.GoalLink:
		result, err = p.importer.ImportGoalLink(ctx, domainOperation, value)
	default:
		return ProviderImportResult{}, ErrInvalidInput
	}
	providerResult := ProviderImportResult{Committed: result.Committed, Created: result.Created}
	switch {
	case errors.Is(err, threads.ErrInvalidInput):
		return providerResult, ErrInvalidInput
	case errors.Is(err, threads.ErrConflict), errors.Is(err, threads.ErrCycle):
		return providerResult, ErrConflict
	case errors.Is(err, threads.ErrNotFound):
		return providerResult, ErrDependencyMissing
	default:
		return providerResult, err
	}
}

func decodeThreadsRecord(record Record) (any, []Reference, error) {
	if record.Revision < 1 || !stableIDPattern.MatchString(record.ID) {
		return nil, nil, ErrInvalidInput
	}
	switch record.Type {
	case "threads.tag":
		payload, err := decodeThreadsPayload[threadsTagPayload](record.Payload)
		if err != nil || !threadsTextRange(payload.Name, 1, 100) || payload.Name != strings.TrimSpace(payload.Name) ||
			payload.ParentID != strings.TrimSpace(payload.ParentID) || payload.ParentID != "" && !stableIDPattern.MatchString(payload.ParentID) ||
			(payload.Inheritance != "include" && payload.Inheritance != "suppress") {
			return nil, nil, ErrInvalidInput
		}
		value := threads.Tag{ID: record.ID, Name: payload.Name, ParentID: payload.ParentID,
			InheritByDefault: payload.Inheritance == "include", Revision: record.Revision}
		return value, threadsParentDependency(record.Type, value.ParentID), nil
	case "threads.goal":
		payload, err := decodeThreadsPayload[threadsGoalPayload](record.Payload)
		if err != nil || !threadsTextRange(payload.Name, 1, 160) || !threadsTextRange(payload.Description, 0, 2_000) ||
			payload.Name != strings.TrimSpace(payload.Name) || payload.Description != strings.TrimSpace(payload.Description) ||
			payload.ParentID != strings.TrimSpace(payload.ParentID) || payload.ParentID != "" && !stableIDPattern.MatchString(payload.ParentID) {
			return nil, nil, ErrInvalidInput
		}
		value := threads.Goal{ID: record.ID, Name: payload.Name, Description: payload.Description, ParentID: payload.ParentID, Revision: record.Revision}
		return value, threadsParentDependency(record.Type, value.ParentID), nil
	case "threads.tag-rule":
		payload, err := decodeThreadsPayload[threadsTagRulePayload](record.Payload)
		value := threads.TagRule{TargetType: threads.TargetType(payload.TargetType), TargetID: payload.TargetID,
			TagID: payload.TagID, Mode: threads.RuleMode(payload.Rule), Revision: record.Revision}
		if err != nil || payload.TargetType != strings.ToLower(strings.TrimSpace(payload.TargetType)) || payload.TargetID != strings.TrimSpace(payload.TargetID) ||
			payload.TagID != strings.TrimSpace(payload.TagID) || !stableIDPattern.MatchString(payload.TagID) ||
			payload.Rule != strings.ToLower(strings.TrimSpace(payload.Rule)) ||
			record.ID != threads.TagRuleRecordID(value.TargetType, value.TargetID, value.TagID) {
			return nil, nil, ErrInvalidInput
		}
		dependencies, err := threadsTagRuleDependenciesChecked(value)
		return value, dependencies, err
	case "threads.goal-link":
		if record.Revision != 1 {
			return nil, nil, ErrInvalidInput
		}
		payload, err := decodeThreadsPayload[threadsGoalLinkPayload](record.Payload)
		value := threads.GoalLink{TargetType: threads.TargetType(payload.TargetType), TargetID: payload.TargetID, GoalID: payload.GoalID}
		if err != nil || payload.TargetType != strings.ToLower(strings.TrimSpace(payload.TargetType)) || payload.TargetID != strings.TrimSpace(payload.TargetID) ||
			payload.GoalID != strings.TrimSpace(payload.GoalID) || !stableIDPattern.MatchString(payload.GoalID) ||
			record.ID != threads.GoalLinkRecordID(value.TargetType, value.TargetID, value.GoalID) {
			return nil, nil, ErrInvalidInput
		}
		dependencies, err := threadsGoalLinkDependenciesChecked(value)
		return value, dependencies, err
	default:
		return nil, nil, ErrInvalidInput
	}
}

func decodeThreadsPayload[T any](payload []byte) (T, error) {
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

func marshalThreadsPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 || len(payload) > MaximumPayloadBytes {
		return nil, ErrInvalidInput
	}
	return payload, nil
}

func threadsParentDependency(recordType, parentID string) []Reference {
	if parentID == "" {
		return []Reference{}
	}
	return []Reference{{Type: recordType, ID: parentID}}
}

func threadsTagRuleDependencies(value threads.TagRule) []Reference {
	result, _ := threadsTagRuleDependenciesChecked(value)
	return result
}

func threadsTagRuleDependenciesChecked(value threads.TagRule) ([]Reference, error) {
	target, ok := threadsTargetReference(value.TargetType, value.TargetID)
	if !ok || (value.Mode != threads.RuleInclude && value.Mode != threads.RuleSuppress) {
		return nil, ErrInvalidInput
	}
	return normalizeReferences([]Reference{{Type: "threads.tag", ID: value.TagID}, target}), nil
}

func threadsGoalLinkDependencies(value threads.GoalLink) []Reference {
	result, _ := threadsGoalLinkDependenciesChecked(value)
	return result
}

func threadsGoalLinkDependenciesChecked(value threads.GoalLink) ([]Reference, error) {
	target, ok := threadsTargetReference(value.TargetType, value.TargetID)
	if !ok || (value.TargetType != threads.TargetAsset && value.TargetType != threads.TargetPurchase) {
		return nil, ErrInvalidInput
	}
	return normalizeReferences([]Reference{{Type: "threads.goal", ID: value.GoalID}, target}), nil
}

func threadsTargetReference(targetType threads.TargetType, targetID string) (Reference, bool) {
	var recordType string
	switch targetType {
	case threads.TargetAsset:
		recordType = "atlas.asset"
	case threads.TargetPurchase:
		recordType = "ledger.purchase-order"
	case threads.TargetContract:
		recordType = "ledger.contract"
	case threads.TargetSoftware:
		recordType = "stack.product"
	case threads.TargetBudget:
		recordType = "ledger.budget"
	case threads.TargetGoal:
		recordType = "threads.goal"
	default:
		return Reference{}, false
	}
	if strings.TrimSpace(targetID) != targetID || !stableIDPattern.MatchString(targetID) {
		return Reference{}, false
	}
	return Reference{Type: recordType, ID: targetID}, true
}

func threadsTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

func sameThreadsTag(left, right threads.Tag) bool {
	return left.ID == right.ID && left.Name == right.Name && left.ParentID == right.ParentID &&
		left.InheritByDefault == right.InheritByDefault && left.Revision == right.Revision
}

func sameThreadsGoal(left, right threads.Goal) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Description == right.Description &&
		left.ParentID == right.ParentID && left.Revision == right.Revision
}

func sameThreadsTagRule(left, right threads.TagRule) bool {
	return left.TargetType == right.TargetType && left.TargetID == right.TargetID && left.TagID == right.TagID &&
		left.Mode == right.Mode && left.Revision == right.Revision
}

func sameThreadsGoalLink(left, right threads.GoalLink) bool {
	return left.TargetType == right.TargetType && left.TargetID == right.TargetID && left.GoalID == right.GoalID
}
