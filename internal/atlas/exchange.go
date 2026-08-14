package atlas

// Private Exchange import capability.
// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-EXCHANGE-001.
// Features: inventory.assets, inventory.models, migration.packages. GitHub: #9.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

type exchangeImporter struct{ service *Service }

type exchangeImportContextKey struct{}

type exchangeImportContext struct{ operation ExchangeImportOperation }

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) ExchangeSnapshot(ctx context.Context, maximum int) (ExchangeSnapshot, error) {
	if maximum < 1 {
		return ExchangeSnapshot{}, ErrInvalidInput
	}
	return s.store.ExchangeSnapshot(ctx, s.organizationID, maximum)
}

func (s *Service) ExchangeLifecycleEvent(ctx context.Context, id string) (domain.AssetLifecycleEvent, error) {
	id = strings.TrimSpace(id)
	if !referencePattern.MatchString(id) {
		return domain.AssetLifecycleEvent{}, ErrInvalidInput
	}
	return s.store.GetAssetLifecycleEvent(ctx, s.organizationID, id)
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func (i *exchangeImporter) ImportModel(ctx context.Context, operation ExchangeImportOperation, candidate domain.AssetModel) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	if !i.service.validExchangeModel(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	if existing, readErr := i.service.store.GetModel(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		existing.InstanceCount = 0
		if !sameExchangeModel(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditRecord(ctx, "atlas.model.imported", "atlas.model", existing.ID, modelAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, err := i.service.store.ImportModel(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if err != nil {
		if observed, readErr := i.service.store.GetModel(ctx, i.service.organizationID, candidate.ID); readErr == nil {
			observed.InstanceCount = 0
			if sameExchangeModel(observed, candidate) {
				result.Committed, result.Created = true, true
			}
		}
		return result, err
	}
	err = i.service.auditRecord(ctx, "atlas.model.imported", "atlas.model", persisted.ID, modelAuditMetadata(persisted))
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (i *exchangeImporter) ImportAsset(ctx context.Context, operation ExchangeImportOperation, candidate domain.Asset) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	if !i.service.validExchangeAsset(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	if candidate.ModelID != "" {
		model, err := i.service.store.GetModel(ctx, i.service.organizationID, candidate.ModelID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ExchangeImportResult{}, ErrReferenceMissing
			}
			return ExchangeImportResult{}, err
		}
		if candidate.ModelContext == nil || candidate.ModelContext.ModelRevision > model.Revision {
			return ExchangeImportResult{}, ErrInvalidInput
		}
	}
	if err := i.service.references.ValidateAssetReferences(ctx, i.service.organizationID, References{
		SiteID: candidate.SiteID, BuildingID: candidate.BuildingID, RoomID: candidate.RoomID,
		DepartmentID: candidate.DepartmentID, UserID: candidate.UserID,
	}); err != nil {
		return ExchangeImportResult{}, err
	}
	if existing, readErr := i.service.store.GetAsset(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeAsset(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditRecord(ctx, "atlas.asset.imported", "atlas.asset", existing.ID, exchangeAssetAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, err := i.service.store.ImportAsset(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if err != nil {
		if observed, readErr := i.service.store.GetAsset(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeAsset(observed, candidate) {
			result.Committed, result.Created = true, true
		}
		return result, err
	}
	err = i.service.auditRecord(ctx, "atlas.asset.imported", "atlas.asset", persisted.ID, exchangeAssetAuditMetadata(persisted))
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (i *exchangeImporter) ImportLifecycleEvent(ctx context.Context, operation ExchangeImportOperation, candidate domain.AssetLifecycleEvent) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID = i.service.organizationID
	if !i.service.validExchangeLifecycle(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation})
	asset, err := i.service.store.GetAsset(ctx, i.service.organizationID, candidate.AssetID)
	if errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, ErrReferenceMissing
	}
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if candidate.Revision > asset.Revision || candidate.OccurredAt.Before(asset.CreatedAt) || candidate.OccurredAt.After(asset.UpdatedAt) ||
		candidate.Revision == asset.Revision && (!candidate.OccurredAt.Equal(asset.UpdatedAt) || candidate.ToStatus != asset.Status) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetAssetLifecycleEvent(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if existing != candidate {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditRecord(ctx, "atlas.lifecycle.imported", "atlas.lifecycle-event", existing.ID, lifecycleAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, err := i.service.store.ImportAssetLifecycleEvent(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if err != nil {
		if observed, readErr := i.service.store.GetAssetLifecycleEvent(ctx, i.service.organizationID, candidate.ID); readErr == nil && observed == candidate {
			result.Committed, result.Created = true, true
		}
		return result, err
	}
	err = i.service.auditRecord(ctx, "atlas.lifecycle.imported", "atlas.lifecycle-event", persisted.ID, lifecycleAuditMetadata(persisted))
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (*exchangeImporter) atlasExchangeImporter() {}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !assetIDPattern.MatchString(operation.Token) || !validExchangeTime(operation.OccurredAt) {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func (s *Service) validExchangeModel(candidate domain.AssetModel) bool {
	if candidate.OrganizationID != s.organizationID || !assetIDPattern.MatchString(candidate.ID) || candidate.InstanceCount != 0 ||
		candidate.Revision < 1 || !validExchangeTimes(candidate.CreatedAt, candidate.UpdatedAt) ||
		candidate.Revision == 1 && !candidate.CreatedAt.Equal(candidate.UpdatedAt) {
		return false
	}
	if _, ok := validModelStatuses[candidate.Status]; !ok {
		return false
	}
	if candidate.Status == "retired" && candidate.Revision < 2 {
		return false
	}
	normalized, err := normalizeCreateModelInput(CreateModelInput{
		ID: candidate.ID, Manufacturer: candidate.Manufacturer, Name: candidate.Name, ModelNumber: candidate.ModelNumber,
		Kind: candidate.Kind, VendorIdentifier: candidate.VendorIdentifier, Specifications: candidate.Specifications,
		SupportURL: candidate.SupportURL, WarrantyMonths: candidate.WarrantyMonths, UsefulLifeMonths: candidate.UsefulLifeMonths,
		SourceSystemID: candidate.SourceSystemID, SourceRecordID: candidate.SourceRecordID,
	})
	return err == nil && normalized.ID == candidate.ID && normalized.Manufacturer == candidate.Manufacturer &&
		normalized.Name == candidate.Name && normalized.ModelNumber == candidate.ModelNumber && normalized.Kind == candidate.Kind &&
		normalized.VendorIdentifier == candidate.VendorIdentifier && maps.Equal(normalized.Specifications, candidate.Specifications) &&
		normalized.SupportURL == candidate.SupportURL && normalized.WarrantyMonths == candidate.WarrantyMonths &&
		normalized.UsefulLifeMonths == candidate.UsefulLifeMonths && normalized.SourceSystemID == candidate.SourceSystemID &&
		normalized.SourceRecordID == candidate.SourceRecordID
}

func (s *Service) validExchangeAsset(candidate domain.Asset) bool {
	if candidate.OrganizationID != s.organizationID || !assetIDPattern.MatchString(candidate.ID) || candidate.Revision < 1 ||
		!validExchangeTimes(candidate.CreatedAt, candidate.UpdatedAt) || candidate.Revision == 1 && !candidate.CreatedAt.Equal(candidate.UpdatedAt) ||
		!validExchangeModelContext(candidate.ModelID, candidate.ModelContext) || candidate.ModelContext != nil &&
		(candidate.ModelContext.AppliedAt.Before(candidate.CreatedAt) || candidate.ModelContext.AppliedAt.After(candidate.UpdatedAt)) {
		return false
	}
	normalized, err := s.normalizeCreateInput(CreateAssetInput{
		ID: candidate.ID, ModelID: candidate.ModelID, Name: candidate.Name, Kind: candidate.Kind,
		AssetTag: candidate.AssetTag, SerialNumber: candidate.SerialNumber, Hostname: candidate.Hostname,
		DeploymentNotes: candidate.DeploymentNotes, References: References{SiteID: candidate.SiteID, BuildingID: candidate.BuildingID,
			RoomID: candidate.RoomID, DepartmentID: candidate.DepartmentID, UserID: candidate.UserID},
		Status: candidate.Status, PurchaseDate: candidate.PurchaseDate,
	})
	return err == nil && normalized.ID == candidate.ID && normalized.ModelID == candidate.ModelID && normalized.Name == candidate.Name &&
		normalized.Kind == candidate.Kind && normalized.AssetTag == candidate.AssetTag && normalized.SerialNumber == candidate.SerialNumber &&
		normalized.Hostname == candidate.Hostname && normalized.DeploymentNotes == candidate.DeploymentNotes &&
		normalized.References == (References{SiteID: candidate.SiteID, BuildingID: candidate.BuildingID, RoomID: candidate.RoomID,
			DepartmentID: candidate.DepartmentID, UserID: candidate.UserID}) && normalized.Status == candidate.Status &&
		equalExchangeOptionalTime(normalized.PurchaseDate, candidate.PurchaseDate)
}

func (s *Service) validExchangeLifecycle(candidate domain.AssetLifecycleEvent) bool {
	if candidate.OrganizationID != s.organizationID || !referencePattern.MatchString(candidate.ID) ||
		!assetIDPattern.MatchString(candidate.AssetID) || candidate.Revision < 1 || !validText(candidate.Note, 1000) ||
		!validTextRange(candidate.ActorID, 1, 128) || !validExchangeTime(candidate.OccurredAt) {
		return false
	}
	if _, ok := validStatuses[candidate.ToStatus]; !ok {
		return false
	}
	if candidate.FromStatus == "" {
		return candidate.Revision == 1
	}
	_, ok := validStatuses[candidate.FromStatus]
	return ok && candidate.Revision > 1 && candidate.FromStatus != candidate.ToStatus
}

func validExchangeModelContext(modelID string, value *domain.AssetModelContext) bool {
	if modelID == "" {
		return value == nil
	}
	if value == nil || value.ModelRevision < 1 || !validExchangeTime(value.DefaultsEffectiveAt) ||
		!validExchangeTime(value.AppliedAt) || value.AppliedAt.Before(value.DefaultsEffectiveAt) ||
		!slices.Equal(value.Overrides, []string{}) && !slices.Equal(value.Overrides, []string{"kind"}) {
		return false
	}
	normalized, err := normalizeCreateModelInput(CreateModelInput{
		ID: modelID, Manufacturer: value.Manufacturer, Name: value.Name, ModelNumber: value.ModelNumber,
		Kind: value.Kind, VendorIdentifier: value.VendorIdentifier, Specifications: value.Specifications,
		SupportURL: value.SupportURL, WarrantyMonths: value.WarrantyMonths, UsefulLifeMonths: value.UsefulLifeMonths,
		SourceSystemID: value.SourceSystemID, SourceRecordID: value.SourceRecordID,
	})
	return err == nil && normalized.Manufacturer == value.Manufacturer && normalized.Name == value.Name &&
		normalized.ModelNumber == value.ModelNumber && normalized.Kind == value.Kind && normalized.VendorIdentifier == value.VendorIdentifier &&
		maps.Equal(normalized.Specifications, value.Specifications) && normalized.SupportURL == value.SupportURL &&
		normalized.WarrantyMonths == value.WarrantyMonths && normalized.UsefulLifeMonths == value.UsefulLifeMonths &&
		normalized.SourceSystemID == value.SourceSystemID && normalized.SourceRecordID == value.SourceRecordID
}

func validExchangeTimes(createdAt, updatedAt time.Time) bool {
	return validExchangeTime(createdAt) && validExchangeTime(updatedAt) && !updatedAt.Before(createdAt)
}

func validExchangeTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1970 && value.Year() <= 9999 && portabletime.IsCanonical(value)
}

func equalExchangeOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func sameExchangeModel(left, right domain.AssetModel) bool {
	left.InstanceCount, right.InstanceCount = 0, 0
	return reflect.DeepEqual(left, right)
}

func sameExchangeAsset(left, right domain.Asset) bool { return reflect.DeepEqual(left, right) }

func exchangeAssetAuditMetadata(asset domain.Asset) map[string]string {
	return withModelContextAudit(asset, map[string]string{
		"status": asset.Status, "kind": asset.Kind, "revision": strconv.FormatInt(asset.Revision, 10),
	})
}

func lifecycleAuditMetadata(event domain.AssetLifecycleEvent) map[string]string {
	return map[string]string{
		"assetId": event.AssetID, "fromStatus": event.FromStatus, "toStatus": event.ToStatus,
		"revision": strconv.FormatInt(event.Revision, 10),
	}
}

func exchangeAuditIdentity(operation ExchangeImportOperation, action, resourceType, resourceID string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{operation.Token, action, resourceType, resourceID}, "\x00")))
	return hex.EncodeToString(digest[:])
}
