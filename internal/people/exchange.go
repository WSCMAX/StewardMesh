package people

// Requirements: REQ-PEOPLE-001, REQ-EXCHANGE-001. Features: identity.directory, migration.packages. GitHub: #9.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

const exchangeActorID = "system:exchange"

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !assetIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func validExchangeState(id string, revision uint64, createdAt, updatedAt time.Time) bool {
	return recordIDPattern.MatchString(id) && revision > 0 && !createdAt.IsZero() && !updatedAt.Before(createdAt) &&
		(revision != 1 || createdAt.Equal(updatedAt)) && portabletime.IsCanonical(createdAt) && portabletime.IsCanonical(updatedAt) &&
		createdAt.Year() >= 2000 && updatedAt.Year() <= 9999
}

func validExchangeCandidateOrganization(organizationID string) bool {
	return strings.TrimSpace(organizationID) == ""
}

func (i *exchangeImporter) ImportSite(ctx context.Context, operation ExchangeImportOperation, candidate Site) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	name, normalizedName, status, validationErr := validateNamedRecord(candidate.Name, candidate.Status)
	address, addressErr := normalizeAddress(candidate.Address)
	if err != nil || validationErr != nil || addressErr != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) ||
		name != candidate.Name || normalizedName != candidate.NormalizedName || status != candidate.Status || address != candidate.Address {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	return importPeopleRecord(ctx, i.service, operation, "people.site.created", "site", candidate.ID, candidate,
		func(ctx context.Context) (Site, error) {
			return i.service.store.GetSite(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Site, error) { return i.service.store.CreateSite(ctx, candidate) },
		func(left, right Site) bool { return sameSite(left, right) }, map[string]string{"revision": strconv.FormatUint(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportBuilding(ctx context.Context, operation ExchangeImportOperation, candidate Building) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	name, normalizedName, status, validationErr := validateNamedRecord(candidate.Name, candidate.Status)
	if err != nil || validationErr != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) ||
		!recordIDPattern.MatchString(candidate.SiteID) || name != candidate.Name || normalizedName != candidate.NormalizedName || status != candidate.Status {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if _, err := i.service.store.GetSite(ctx, i.service.organizationID, candidate.SiteID); err != nil {
		return ExchangeImportResult{}, mapExchangeReferenceError(err)
	}
	candidate.OrganizationID = i.service.organizationID
	return importPeopleRecord(ctx, i.service, operation, "people.building.created", "building", candidate.ID, candidate,
		func(ctx context.Context) (Building, error) {
			return i.service.store.GetBuilding(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Building, error) { return i.service.store.CreateBuilding(ctx, candidate) },
		func(left, right Building) bool { return sameBuilding(left, right) },
		map[string]string{"siteId": candidate.SiteID, "revision": strconv.FormatUint(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportRoom(ctx context.Context, operation ExchangeImportOperation, candidate Room) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	number, name := strings.TrimSpace(candidate.Number), strings.TrimSpace(candidate.Name)
	if err != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) ||
		!recordIDPattern.MatchString(candidate.SiteID) || !recordIDPattern.MatchString(candidate.BuildingID) || number == "" || number != candidate.Number ||
		strings.ToLower(number) != candidate.NormalizedNumber || name != candidate.Name || !validBoundedText(number, 100) || !validBoundedText(name, 200) || !validStatus(candidate.Status) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	building, err := i.service.store.GetBuilding(ctx, i.service.organizationID, candidate.BuildingID)
	if err != nil {
		return ExchangeImportResult{}, mapExchangeReferenceError(err)
	}
	if building.SiteID != candidate.SiteID {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	return importPeopleRecord(ctx, i.service, operation, "people.room.created", "room", candidate.ID, candidate,
		func(ctx context.Context) (Room, error) {
			return i.service.store.GetRoom(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Room, error) { return i.service.store.CreateRoom(ctx, candidate) },
		func(left, right Room) bool { return sameRoom(left, right) },
		map[string]string{"siteId": candidate.SiteID, "buildingId": candidate.BuildingID, "revision": strconv.FormatUint(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportDepartment(ctx context.Context, operation ExchangeImportOperation, candidate Department) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	name, normalizedName, status, validationErr := validateNamedRecord(candidate.Name, candidate.Status)
	if err != nil || validationErr != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) ||
		candidate.SiteID != "" && !recordIDPattern.MatchString(candidate.SiteID) || name != candidate.Name || normalizedName != candidate.NormalizedName || status != candidate.Status {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if candidate.SiteID != "" {
		if _, err := i.service.store.GetSite(ctx, i.service.organizationID, candidate.SiteID); err != nil {
			return ExchangeImportResult{}, mapExchangeReferenceError(err)
		}
	}
	candidate.OrganizationID = i.service.organizationID
	metadata := map[string]string{"revision": strconv.FormatUint(candidate.Revision, 10)}
	if candidate.SiteID != "" {
		metadata["siteId"] = candidate.SiteID
	}
	return importPeopleRecord(ctx, i.service, operation, "people.department.created", "department", candidate.ID, candidate,
		func(ctx context.Context) (Department, error) {
			return i.service.store.GetDepartment(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Department, error) { return i.service.store.CreateDepartment(ctx, candidate) },
		func(left, right Department) bool { return sameDepartment(left, right) }, metadata)
}

func (i *exchangeImporter) ImportIdentity(ctx context.Context, operation ExchangeImportOperation, candidate Identity) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	displayName := strings.TrimSpace(candidate.DisplayName)
	email, emailErr := normalizeEmail(candidate.Email)
	provider, providerSubject := strings.ToLower(strings.TrimSpace(candidate.Provider)), strings.TrimSpace(candidate.ProviderSubject)
	if err != nil || emailErr != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !validExchangeState(candidate.ID, candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt) ||
		!validIdentityKind(candidate.Kind) || displayName == "" || displayName != candidate.DisplayName || strings.ToLower(displayName) != candidate.NormalizedName ||
		!validBoundedText(displayName, 200) || email != candidate.Email || candidate.NormalizedEmail != email || candidate.Kind == IdentityPerson && email == "" ||
		candidate.DepartmentID != "" && !recordIDPattern.MatchString(candidate.DepartmentID) || candidate.SiteID != "" && !recordIDPattern.MatchString(candidate.SiteID) ||
		!validStatus(candidate.Status) || provider != candidate.Provider || providerSubject != candidate.ProviderSubject ||
		(provider == "") != (providerSubject == "") || provider != "" && (!providerPattern.MatchString(provider) || len(providerSubject) > 255) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if candidate.DepartmentID != "" {
		department, err := i.service.store.GetDepartment(ctx, i.service.organizationID, candidate.DepartmentID)
		if err != nil {
			return ExchangeImportResult{}, mapExchangeReferenceError(err)
		}
		if department.SiteID != "" && candidate.SiteID != department.SiteID {
			return ExchangeImportResult{}, ErrInvalidInput
		}
	}
	if candidate.SiteID != "" {
		if _, err := i.service.store.GetSite(ctx, i.service.organizationID, candidate.SiteID); err != nil {
			return ExchangeImportResult{}, mapExchangeReferenceError(err)
		}
	}
	candidate.OrganizationID = i.service.organizationID
	return importPeopleRecord(ctx, i.service, operation, "people.identity.created", "identity", candidate.ID, candidate,
		func(ctx context.Context) (Identity, error) {
			return i.service.store.GetIdentity(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (Identity, error) { return i.service.store.CreateIdentity(ctx, candidate) },
		func(left, right Identity) bool { return sameIdentity(left, right) },
		map[string]string{"kind": string(candidate.Kind), "revision": strconv.FormatUint(candidate.Revision, 10)})
}

func (i *exchangeImporter) ImportAssetAssignment(ctx context.Context, operation ExchangeImportOperation, candidate AssetAssignment) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || !validExchangeCandidateOrganization(candidate.OrganizationID) || !recordIDPattern.MatchString(candidate.ID) || !assetIDPattern.MatchString(candidate.AssetID) ||
		!recordIDPattern.MatchString(candidate.AssigneeID) || !validAssignment(candidate.AssigneeKind, candidate.Role) ||
		candidate.EffectiveFrom.IsZero() || !portabletime.IsCanonical(candidate.EffectiveFrom) ||
		candidate.EffectiveTo != nil && (!portabletime.IsCanonical(*candidate.EffectiveTo) || !candidate.EffectiveTo.After(candidate.EffectiveFrom) || candidate.EffectiveTo.Year() > 9999) ||
		candidate.CreatedAt.IsZero() || !portabletime.IsCanonical(candidate.CreatedAt) || candidate.CreatedAt.Year() < 2000 || candidate.CreatedAt.Year() > 9999 ||
		candidate.CreatedBy != exchangeActorID {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if _, err := i.service.assets.Get(ctx, candidate.AssetID); err != nil {
		return ExchangeImportResult{}, ErrReferenceMissing
	}
	var referenceErr error
	switch candidate.AssigneeKind {
	case AssigneeIdentity:
		_, referenceErr = i.service.store.GetIdentity(ctx, i.service.organizationID, candidate.AssigneeID)
	case AssigneeDepartment:
		_, referenceErr = i.service.store.GetDepartment(ctx, i.service.organizationID, candidate.AssigneeID)
	}
	if referenceErr != nil {
		return ExchangeImportResult{}, mapExchangeReferenceError(referenceErr)
	}
	candidate.OrganizationID = i.service.organizationID
	return importPeopleRecord(ctx, i.service, operation, "people.asset_assignment.created", "asset_assignment", candidate.ID, candidate,
		func(ctx context.Context) (AssetAssignment, error) {
			return i.service.store.GetAssetAssignment(ctx, i.service.organizationID, candidate.ID)
		},
		func(ctx context.Context) (AssetAssignment, error) {
			return i.service.store.ImportAssetAssignment(ctx, candidate)
		},
		func(left, right AssetAssignment) bool { return sameAssetAssignment(left, right) },
		map[string]string{"assigneeKind": string(candidate.AssigneeKind), "role": string(candidate.Role)})
}

func importPeopleRecord[T any](ctx context.Context, service *Service, operation ExchangeImportOperation, action, resourceType, resourceID string, candidate T,
	get func(context.Context) (T, error), create func(context.Context) (T, error), same func(T, T) bool, metadata map[string]string,
) (ExchangeImportResult, error) {
	existing, err := get(ctx)
	if err == nil {
		if !same(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		if err := service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata); err != nil {
			return ExchangeImportResult{Committed: true}, err
		}
		return ExchangeImportResult{Committed: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = create(ctx)
	if err != nil {
		observed, readErr := get(ctx)
		if readErr == nil && same(observed, candidate) {
			auditErr := service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata)
			return ExchangeImportResult{Committed: true, Created: true}, errors.Join(err, auditErr)
		}
		return ExchangeImportResult{}, errors.Join(err, readErr)
	}
	if err := service.auditExchange(ctx, operation, action, resourceType, resourceID, metadata); err != nil {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{Committed: true, Created: true}, nil
}

func mapExchangeReferenceError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrReferenceMissing) {
		return ErrReferenceMissing
	}
	return err
}

func (s *Service) auditExchange(ctx context.Context, operation ExchangeImportOperation, action, resourceType, resourceID string, metadata map[string]string) error {
	digest := sha256.Sum256([]byte(strings.Join([]string{s.organizationID, operation.Token, action, resourceType, resourceID}, "\x00")))
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["requirementId"] = RequirementID
	metadata["exchangeRequirementId"] = "REQ-EXCHANGE-001"
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: fmt.Sprintf("%x", digest[:]), OrganizationID: s.organizationID, ActorID: exchangeActorID,
		CorrelationID: operation.Token, Action: action, ResourceType: resourceType, ResourceID: resourceID,
		OccurredAt: operation.OccurredAt, Metadata: metadata,
	})
}

func sameSite(left, right Site) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.NormalizedName == right.NormalizedName &&
		left.Address == right.Address && left.Status == right.Status && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameBuilding(left, right Building) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.SiteID == right.SiteID && left.Name == right.Name &&
		left.NormalizedName == right.NormalizedName && left.Status == right.Status && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameRoom(left, right Room) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.SiteID == right.SiteID && left.BuildingID == right.BuildingID &&
		left.Number == right.Number && left.NormalizedNumber == right.NormalizedNumber && left.Name == right.Name && left.Status == right.Status &&
		left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameDepartment(left, right Department) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Name == right.Name && left.NormalizedName == right.NormalizedName &&
		left.SiteID == right.SiteID && left.Status == right.Status && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameIdentity(left, right Identity) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.Kind == right.Kind && left.DisplayName == right.DisplayName &&
		left.NormalizedName == right.NormalizedName && left.Email == right.Email && left.NormalizedEmail == right.NormalizedEmail &&
		left.DepartmentID == right.DepartmentID && left.SiteID == right.SiteID && left.Status == right.Status && left.Provider == right.Provider &&
		left.ProviderSubject == right.ProviderSubject && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameAssetAssignment(left, right AssetAssignment) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID && left.AssetID == right.AssetID && left.AssigneeKind == right.AssigneeKind &&
		left.AssigneeID == right.AssigneeID && left.Role == right.Role && left.EffectiveFrom.Equal(right.EffectiveFrom) && equalPeopleOptionalTime(left.EffectiveTo, right.EffectiveTo) &&
		left.CreatedBy == right.CreatedBy && left.CreatedAt.Equal(right.CreatedAt)
}

func equalPeopleOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}
