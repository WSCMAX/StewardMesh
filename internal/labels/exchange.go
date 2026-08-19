package labels

import (
	"context"
	"errors"
)

type exchangeImporter struct{ service *Service }

func (i *exchangeImporter) labelsExchangeImporter() {}

func (i *exchangeImporter) ImportDefinition(ctx context.Context, operation ExchangeImportOperation, candidate Definition) (ExchangeImportResult, error) {
	_ = operation
	candidate.OrganizationID = i.service.organizationID
	if err := i.service.validateDefinitionRelations(ctx, candidate.ID, candidate.ParentID, candidate.GoalID); err != nil {
		return ExchangeImportResult{}, err
	}
	existing, err := i.service.store.GetDefinition(ctx, candidate.OrganizationID, candidate.ID)
	switch {
	case err == nil:
		if sameDefinition(existing, candidate) {
			return ExchangeImportResult{}, nil
		}
		if _, updateErr := i.service.store.UpdateDefinition(ctx, candidate, existing.Revision); updateErr != nil {
			return ExchangeImportResult{}, updateErr
		}
		return ExchangeImportResult{Committed: true}, nil
	case !errors.Is(err, ErrNotFound):
		return ExchangeImportResult{}, err
	}
	if _, err := i.service.store.CreateDefinition(ctx, candidate); err != nil {
		return ExchangeImportResult{}, err
	}
	return ExchangeImportResult{Committed: true, Created: true}, nil
}

func (i *exchangeImporter) ImportAssignment(ctx context.Context, operation ExchangeImportOperation, candidate Assignment) (ExchangeImportResult, error) {
	_ = operation
	candidate.OrganizationID = i.service.organizationID
	definition, err := i.service.store.GetDefinition(ctx, candidate.OrganizationID, candidate.DefinitionID)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	value, err := NormalizeValue(definition, candidate.ValueText, candidate.Values)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	candidate.ValueText = value.ValueText
	candidate.Values = value.Values
	existing, err := i.service.store.GetAssignment(ctx, candidate.OrganizationID, candidate.DefinitionID, candidate.RecordType, candidate.RecordID)
	switch {
	case err == nil:
		if sameAssignment(existing, candidate) {
			return ExchangeImportResult{}, nil
		}
		candidate.Revision = existing.Revision + 1
		if _, updateErr := i.service.store.PutAssignment(ctx, candidate, existing.Revision); updateErr != nil {
			return ExchangeImportResult{}, updateErr
		}
		return ExchangeImportResult{Committed: true}, nil
	case !errors.Is(err, ErrNotFound):
		return ExchangeImportResult{}, err
	}
	if candidate.Revision < 1 {
		candidate.Revision = 1
	}
	if _, err := i.service.store.PutAssignment(ctx, candidate, 0); err != nil {
		return ExchangeImportResult{}, err
	}
	return ExchangeImportResult{Committed: true, Created: true}, nil
}

func sameAssignment(left, right Assignment) bool {
	return left.DefinitionID == right.DefinitionID && left.RecordType == right.RecordType && left.RecordID == right.RecordID &&
		left.ValueText == right.ValueText && stringSliceEqual(left.Values, right.Values)
}

func sameDefinition(left, right Definition) bool {
	return left.Name == right.Name && left.Description == right.Description && left.ValueKind == right.ValueKind &&
		left.ParentID == right.ParentID && left.GoalID == right.GoalID && left.Status == right.Status &&
		stringSliceEqual(left.ApplicableRecordTypes, right.ApplicableRecordTypes) &&
		stringSliceEqual(left.Options, right.Options)
}

func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
