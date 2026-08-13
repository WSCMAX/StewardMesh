package atlas

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001. Features: inventory.assets, inventory.models.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
)

const (
	defaultListLimit  = 50
	maximumListLimit  = 100
	MaximumBulkAssets = 100
)

var (
	assetIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	hostnamePattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	referencePattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	urlPattern       = regexp.MustCompile(`^https://[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]{1,5})?(?:/.*)?$`)
	validKinds       = map[string]struct{}{
		"server": {}, "computer": {}, "desktop": {}, "laptop": {}, "tablet": {},
		"phone": {}, "network": {}, "peripheral": {}, "virtual": {}, "other": {},
	}
	validStatuses = map[string]struct{}{
		"draft": {}, "active": {}, "inactive": {}, "retired": {}, "disposed": {},
	}
	validModelStatuses = map[string]struct{}{"active": {}, "retired": {}}
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	references     ReferenceValidator
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, references ReferenceValidator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if store == nil || references == nil || auditor == nil {
		return nil, errors.New("Atlas store, reference validator, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("Atlas organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store: store, references: references, auditor: auditor,
		organizationID: configuration.OrganizationID, now: configuration.Now,
	}, nil
}

func (s *Service) ListAssets(ctx context.Context, query Query) ([]domain.Asset, error) {
	query, err := normalizeQuery(query)
	if err != nil {
		return nil, err
	}
	return s.store.ListAssets(ctx, s.organizationID, query)
}

func (s *Service) GetAsset(ctx context.Context, id string) (domain.Asset, error) {
	id = strings.TrimSpace(id)
	if !assetIDPattern.MatchString(id) {
		return domain.Asset{}, ErrInvalidInput
	}
	return s.store.GetAsset(ctx, s.organizationID, id)
}

func (s *Service) ListModels(ctx context.Context, query ModelQuery) ([]domain.AssetModel, error) {
	query, err := normalizeModelQuery(query)
	if err != nil {
		return nil, err
	}
	return s.store.ListModels(ctx, s.organizationID, query)
}

func (s *Service) GetModel(ctx context.Context, id string) (domain.AssetModel, error) {
	id = strings.TrimSpace(id)
	if !assetIDPattern.MatchString(id) {
		return domain.AssetModel{}, ErrInvalidInput
	}
	return s.store.GetModel(ctx, s.organizationID, id)
}

func (s *Service) GetModelInventory(ctx context.Context, id string, query ModelInventoryQuery) (ModelInventory, error) {
	id = strings.TrimSpace(id)
	if !assetIDPattern.MatchString(id) {
		return ModelInventory{}, ErrInvalidInput
	}
	normalized, err := normalizeModelInventoryQuery(query)
	if err != nil {
		return ModelInventory{}, err
	}
	return s.store.GetModelInventory(ctx, s.organizationID, id, normalized)
}

func (s *Service) ResolveModel(ctx context.Context, identity ModelIdentity) (domain.AssetModel, error) {
	normalized, err := normalizeModelIdentity(identity)
	if err != nil {
		return domain.AssetModel{}, err
	}
	return s.store.ResolveModel(ctx, s.organizationID, normalized)
}

// Get satisfies People's provider-neutral AssetReader without weakening the
// organization boundary owned by Atlas.
func (s *Service) Get(ctx context.Context, id string) (domain.Asset, error) {
	return s.GetAsset(ctx, id)
}

func (s *Service) CreateModel(ctx context.Context, input CreateModelInput) (domain.AssetModel, error) {
	normalized, err := normalizeCreateModelInput(input)
	if err != nil {
		return domain.AssetModel{}, err
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return domain.AssetModel{}, fmt.Errorf("create model id: %w", err)
		}
	}
	now := s.now().UTC()
	model := domain.AssetModel{
		ID: id, OrganizationID: s.organizationID, Manufacturer: normalized.Manufacturer, Name: normalized.Name,
		ModelNumber: normalized.ModelNumber, Kind: normalized.Kind, VendorIdentifier: normalized.VendorIdentifier,
		Specifications: cloneSpecifications(normalized.Specifications), SupportURL: normalized.SupportURL,
		WarrantyMonths: normalized.WarrantyMonths, UsefulLifeMonths: normalized.UsefulLifeMonths, Status: "active",
		SourceSystemID: normalized.SourceSystemID, SourceRecordID: normalized.SourceRecordID, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.store.CreateModel(ctx, model)
	if err != nil {
		return domain.AssetModel{}, err
	}
	if err := s.auditRecord(ctx, "atlas.model.created", "asset_model", created.ID, modelAuditMetadata(created)); err != nil {
		return domain.AssetModel{}, fmt.Errorf("audit model creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateModel(ctx context.Context, input UpdateModelInput) (domain.AssetModel, error) {
	id := strings.TrimSpace(input.ID)
	if !assetIDPattern.MatchString(id) || input.Revision < 1 {
		return domain.AssetModel{}, ErrInvalidInput
	}
	existing, err := s.store.GetModel(ctx, s.organizationID, id)
	if err != nil {
		return domain.AssetModel{}, err
	}
	if existing.Revision != input.Revision || existing.Status == "retired" {
		return domain.AssetModel{}, ErrConflict
	}
	normalized, err := normalizeCreateModelInput(CreateModelInput{
		ID: id, Manufacturer: input.Manufacturer, Name: input.Name, ModelNumber: input.ModelNumber, Kind: input.Kind,
		VendorIdentifier: input.VendorIdentifier, Specifications: input.Specifications, SupportURL: input.SupportURL,
		WarrantyMonths: input.WarrantyMonths, UsefulLifeMonths: input.UsefulLifeMonths,
		SourceSystemID: input.SourceSystemID, SourceRecordID: input.SourceRecordID,
	})
	if err != nil {
		return domain.AssetModel{}, err
	}
	updated := existing
	updated.Manufacturer = normalized.Manufacturer
	updated.Name = normalized.Name
	updated.ModelNumber = normalized.ModelNumber
	updated.Kind = normalized.Kind
	updated.VendorIdentifier = normalized.VendorIdentifier
	updated.Specifications = cloneSpecifications(normalized.Specifications)
	updated.SupportURL = normalized.SupportURL
	updated.WarrantyMonths = normalized.WarrantyMonths
	updated.UsefulLifeMonths = normalized.UsefulLifeMonths
	updated.SourceSystemID = normalized.SourceSystemID
	updated.SourceRecordID = normalized.SourceRecordID
	updated.Revision = existing.Revision + 1
	updated.UpdatedAt = s.now().UTC()
	persisted, err := s.store.UpdateModel(ctx, updated, existing.Revision)
	if err != nil {
		return domain.AssetModel{}, err
	}
	if err := s.auditRecord(ctx, "atlas.model.updated", "asset_model", persisted.ID, modelAuditMetadata(persisted)); err != nil {
		return domain.AssetModel{}, fmt.Errorf("audit model update: %w", err)
	}
	return persisted, nil
}

func (s *Service) RetireModel(ctx context.Context, id string, revision int64) (domain.AssetModel, error) {
	id = strings.TrimSpace(id)
	if !assetIDPattern.MatchString(id) || revision < 1 {
		return domain.AssetModel{}, ErrInvalidInput
	}
	retired, err := s.store.RetireModel(ctx, s.organizationID, id, revision, s.now().UTC())
	if err != nil {
		return domain.AssetModel{}, err
	}
	if err := s.auditRecord(ctx, "atlas.model.retired", "asset_model", retired.ID, modelAuditMetadata(retired)); err != nil {
		return domain.AssetModel{}, fmt.Errorf("audit model retirement: %w", err)
	}
	return retired, nil
}

func (s *Service) CreateAsset(ctx context.Context, input CreateAssetInput) (domain.Asset, error) {
	normalized, err := s.normalizeCreateInput(input)
	if err != nil {
		return domain.Asset{}, err
	}
	model, err := s.validateModelReference(ctx, normalized.ModelID)
	if err != nil {
		return domain.Asset{}, err
	}
	if normalized.Kind == "" && model.ID != "" {
		normalized.Kind = model.Kind
	}
	if err := s.references.ValidateAssetReferences(ctx, s.organizationID, normalized.References); err != nil {
		return domain.Asset{}, err
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return domain.Asset{}, fmt.Errorf("create asset id: %w", err)
		}
	}
	now := s.now().UTC()
	asset := domain.Asset{
		ID: id, OrganizationID: s.organizationID, ModelID: normalized.ModelID,
		ModelContext: snapshotModelContext(model, normalized.Kind, now), Name: normalized.Name, Kind: normalized.Kind,
		AssetTag: normalized.AssetTag, SerialNumber: normalized.SerialNumber, Hostname: normalized.Hostname, DeploymentNotes: normalized.DeploymentNotes,
		SiteID: normalized.SiteID, BuildingID: normalized.BuildingID, RoomID: normalized.RoomID,
		DepartmentID: normalized.DepartmentID, UserID: normalized.UserID, Status: normalized.Status,
		PurchaseDate: cloneDate(normalized.PurchaseDate), Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	event, err := s.lifecycleEvent(ctx, asset, "", asset.Status, "Asset registered")
	if err != nil {
		return domain.Asset{}, err
	}
	created, err := s.store.CreateAsset(ctx, asset, event)
	if err != nil {
		return domain.Asset{}, err
	}
	if err := s.auditAsset(ctx, "atlas.asset.created", created.ID, withModelContextAudit(created, map[string]string{
		"status": created.Status, "kind": created.Kind, "revision": "1",
	})); err != nil {
		return domain.Asset{}, fmt.Errorf("audit asset creation: %w", err)
	}
	return created, nil
}

func (s *Service) CreateAssetsFromModel(ctx context.Context, input BulkCreateAssetsInput) (BulkCreateAssetsResult, error) {
	input.ModelID = strings.TrimSpace(input.ModelID)
	if !assetIDPattern.MatchString(input.ModelID) || len(input.Items) < 1 || len(input.Items) > MaximumBulkAssets {
		return BulkCreateAssetsResult{}, ErrInvalidInput
	}
	model, err := s.validateModelReference(ctx, input.ModelID)
	if err != nil {
		return BulkCreateAssetsResult{}, err
	}
	now := s.now().UTC()
	assets := make([]domain.Asset, 0, len(input.Items))
	events := make([]domain.AssetLifecycleEvent, 0, len(input.Items))
	ids := make(map[string]struct{}, len(input.Items))
	assetTags := make(map[string]struct{}, len(input.Items))
	serialNumbers := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if strings.TrimSpace(item.ModelID) != "" && strings.TrimSpace(item.ModelID) != input.ModelID {
			return BulkCreateAssetsResult{}, ErrInvalidInput
		}
		item.ModelID = input.ModelID
		normalized, normalizeErr := s.normalizeCreateInput(item)
		if normalizeErr != nil {
			return BulkCreateAssetsResult{}, normalizeErr
		}
		if normalized.Kind == "" {
			normalized.Kind = model.Kind
		}
		if validateErr := s.references.ValidateAssetReferences(ctx, s.organizationID, normalized.References); validateErr != nil {
			return BulkCreateAssetsResult{}, validateErr
		}
		id := normalized.ID
		if id == "" {
			id, err = foundation.NewCorrelationID()
			if err != nil {
				return BulkCreateAssetsResult{}, fmt.Errorf("create bulk asset id: %w", err)
			}
		}
		if _, exists := ids[id]; exists || repeatedNormalizedValue(assetTags, normalized.AssetTag) || repeatedNormalizedValue(serialNumbers, normalized.SerialNumber) {
			return BulkCreateAssetsResult{}, ErrConflict
		}
		ids[id] = struct{}{}
		asset := domain.Asset{
			ID: id, OrganizationID: s.organizationID, ModelID: input.ModelID,
			ModelContext: snapshotModelContext(model, normalized.Kind, now), Name: normalized.Name, Kind: normalized.Kind,
			AssetTag: normalized.AssetTag, SerialNumber: normalized.SerialNumber, Hostname: normalized.Hostname, DeploymentNotes: normalized.DeploymentNotes,
			SiteID: normalized.SiteID, BuildingID: normalized.BuildingID, RoomID: normalized.RoomID,
			DepartmentID: normalized.DepartmentID, UserID: normalized.UserID, Status: normalized.Status,
			PurchaseDate: cloneDate(normalized.PurchaseDate), Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		event, eventErr := s.lifecycleEvent(ctx, asset, "", asset.Status, "Asset registered from model bulk intake")
		if eventErr != nil {
			return BulkCreateAssetsResult{}, eventErr
		}
		assets = append(assets, asset)
		events = append(events, event)
	}
	created, err := s.store.CreateAssets(ctx, assets, events)
	if err != nil {
		return BulkCreateAssetsResult{}, err
	}
	for _, asset := range created {
		if err := s.auditAsset(ctx, "atlas.asset.created", asset.ID, withModelContextAudit(asset, map[string]string{
			"status": asset.Status, "kind": asset.Kind, "revision": "1", "modelId": input.ModelID,
			"modelRequirementId": ModelRequirementID, "creationMode": "model_bulk",
		})); err != nil {
			return BulkCreateAssetsResult{}, fmt.Errorf("audit bulk asset creation: %w", err)
		}
	}
	return BulkCreateAssetsResult{Items: created}, nil
}

func (s *Service) UpdateAsset(ctx context.Context, input UpdateAssetInput) (domain.Asset, error) {
	id := strings.TrimSpace(input.ID)
	if !assetIDPattern.MatchString(id) || input.Revision < 1 {
		return domain.Asset{}, ErrInvalidInput
	}
	existing, err := s.store.GetAsset(ctx, s.organizationID, id)
	if err != nil {
		return domain.Asset{}, err
	}
	if existing.Revision != input.Revision {
		return domain.Asset{}, ErrConflict
	}
	normalized, err := s.normalizeCreateInput(CreateAssetInput{
		ID: id, ModelID: input.ModelID, Name: input.Name, Kind: input.Kind, AssetTag: input.AssetTag,
		SerialNumber: input.SerialNumber, Hostname: input.Hostname, DeploymentNotes: input.DeploymentNotes, References: input.References,
		Status: input.Status, PurchaseDate: input.PurchaseDate,
	})
	if err != nil {
		return domain.Asset{}, err
	}
	model, err := s.validateModelReference(ctx, normalized.ModelID)
	if err != nil {
		return domain.Asset{}, err
	}
	if normalized.Kind == "" && model.ID != "" {
		if existing.ModelID == normalized.ModelID && existing.ModelContext != nil {
			normalized.Kind = existing.ModelContext.Kind
		} else {
			normalized.Kind = model.Kind
		}
	}
	if err := s.references.ValidateAssetReferences(ctx, s.organizationID, normalized.References); err != nil {
		return domain.Asset{}, err
	}
	note := strings.TrimSpace(input.LifecycleNote)
	if !validText(note, 1000) || (note != "" && normalized.Status == existing.Status) {
		return domain.Asset{}, ErrInvalidInput
	}
	now := s.now().UTC()
	updated := existing
	updated.Name = normalized.Name
	updated.ModelID = normalized.ModelID
	updated.ModelContext = updatedModelContext(existing, model, normalized.Kind, now)
	updated.Kind = normalized.Kind
	updated.AssetTag = normalized.AssetTag
	updated.SerialNumber = normalized.SerialNumber
	updated.Hostname = normalized.Hostname
	updated.DeploymentNotes = normalized.DeploymentNotes
	updated.SiteID = normalized.SiteID
	updated.BuildingID = normalized.BuildingID
	updated.RoomID = normalized.RoomID
	updated.DepartmentID = normalized.DepartmentID
	updated.UserID = normalized.UserID
	updated.Status = normalized.Status
	updated.PurchaseDate = cloneDate(normalized.PurchaseDate)
	updated.Revision = existing.Revision + 1
	updated.UpdatedAt = now

	var event *domain.AssetLifecycleEvent
	if updated.Status != existing.Status {
		if note == "" {
			note = "Status changed"
		}
		candidate, err := s.lifecycleEvent(ctx, updated, existing.Status, updated.Status, note)
		if err != nil {
			return domain.Asset{}, err
		}
		event = &candidate
	}
	persisted, err := s.store.UpdateAsset(ctx, updated, existing.Revision, event)
	if err != nil {
		return domain.Asset{}, err
	}
	metadata := map[string]string{
		"status": persisted.Status, "kind": persisted.Kind,
		"revision": fmt.Sprintf("%d", persisted.Revision),
	}
	if existing.Status != persisted.Status {
		metadata["previousStatus"] = existing.Status
	}
	metadata = withModelContextAudit(persisted, metadata)
	if err := s.auditAsset(ctx, "atlas.asset.updated", persisted.ID, metadata); err != nil {
		return domain.Asset{}, fmt.Errorf("audit asset update: %w", err)
	}
	return persisted, nil
}

func (s *Service) ListAssetLifecycle(ctx context.Context, assetID string) ([]domain.AssetLifecycleEvent, error) {
	assetID = strings.TrimSpace(assetID)
	if !assetIDPattern.MatchString(assetID) {
		return nil, ErrInvalidInput
	}
	if _, err := s.store.GetAsset(ctx, s.organizationID, assetID); err != nil {
		return nil, err
	}
	return s.store.ListAssetLifecycle(ctx, s.organizationID, assetID)
}

func (s *Service) normalizeCreateInput(input CreateAssetInput) (CreateAssetInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.Name = strings.TrimSpace(input.Name)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.AssetTag = strings.TrimSpace(input.AssetTag)
	input.SerialNumber = strings.TrimSpace(input.SerialNumber)
	input.Hostname = strings.ToLower(strings.TrimSpace(input.Hostname))
	input.DeploymentNotes = strings.TrimSpace(input.DeploymentNotes)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "draft"
	}
	input.References = normalizeReferences(input.References)
	if (input.ID != "" && !assetIDPattern.MatchString(input.ID)) || (input.ModelID != "" && !assetIDPattern.MatchString(input.ModelID)) ||
		!validTextRange(input.Name, 1, 200) ||
		!validText(input.AssetTag, 128) || !validText(input.SerialNumber, 255) || !validText(input.Hostname, 253) ||
		!validText(input.DeploymentNotes, 2000) {
		return CreateAssetInput{}, ErrInvalidInput
	}
	if input.Kind == "" && input.ModelID == "" {
		return CreateAssetInput{}, ErrInvalidInput
	}
	if input.Kind != "" {
		if _, ok := validKinds[input.Kind]; !ok {
			return CreateAssetInput{}, ErrInvalidInput
		}
	}
	if _, ok := validStatuses[input.Status]; !ok {
		return CreateAssetInput{}, ErrInvalidInput
	}
	if input.Hostname != "" && !hostnamePattern.MatchString(input.Hostname) {
		return CreateAssetInput{}, ErrInvalidInput
	}
	if err := validateReferences(input.References); err != nil {
		return CreateAssetInput{}, err
	}
	if input.PurchaseDate != nil {
		value := input.PurchaseDate.UTC()
		if value.IsZero() || value.Year() < 1970 || value.Year() > 9999 {
			return CreateAssetInput{}, ErrInvalidInput
		}
		value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
		input.PurchaseDate = &value
	}
	return input, nil
}

func normalizeCreateModelInput(input CreateModelInput) (CreateModelInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Manufacturer = strings.TrimSpace(input.Manufacturer)
	input.Name = strings.TrimSpace(input.Name)
	input.ModelNumber = strings.TrimSpace(input.ModelNumber)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	input.VendorIdentifier = strings.TrimSpace(input.VendorIdentifier)
	input.SupportURL = strings.TrimSpace(input.SupportURL)
	input.SourceSystemID = strings.TrimSpace(input.SourceSystemID)
	input.SourceRecordID = strings.TrimSpace(input.SourceRecordID)
	if (input.ID != "" && !assetIDPattern.MatchString(input.ID)) || !validTextRange(input.Manufacturer, 1, 120) ||
		!validTextRange(input.Name, 1, 160) || !validText(input.ModelNumber, 120) ||
		!validText(input.VendorIdentifier, 160) || !validText(input.SourceSystemID, 120) ||
		!validText(input.SourceRecordID, 160) || !validKind(input.Kind) ||
		input.WarrantyMonths < 0 || input.WarrantyMonths > 1200 || input.UsefulLifeMonths < 0 || input.UsefulLifeMonths > 1200 {
		return CreateModelInput{}, ErrInvalidInput
	}
	if input.SupportURL != "" && (!validText(input.SupportURL, 500) || !urlPattern.MatchString(input.SupportURL)) {
		return CreateModelInput{}, ErrInvalidInput
	}
	if len(input.Specifications) > 25 {
		return CreateModelInput{}, ErrInvalidInput
	}
	specs := make(map[string]string, len(input.Specifications))
	for key, value := range input.Specifications {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !validTextRange(key, 1, 80) || !validText(value, 500) {
			return CreateModelInput{}, ErrInvalidInput
		}
		specs[key] = value
	}
	input.Specifications = specs
	return input, nil
}

func normalizeModelIdentity(identity ModelIdentity) (ModelIdentity, error) {
	identity.Manufacturer = strings.ToLower(strings.TrimSpace(identity.Manufacturer))
	identity.Name = strings.ToLower(strings.TrimSpace(identity.Name))
	identity.ModelNumber = strings.ToLower(strings.TrimSpace(identity.ModelNumber))
	if !validTextRange(identity.Manufacturer, 1, 120) || !validTextRange(identity.Name, 1, 160) ||
		!validText(identity.ModelNumber, 120) {
		return ModelIdentity{}, ErrInvalidInput
	}
	return identity, nil
}

func normalizeQuery(query Query) (Query, error) {
	query.Search = strings.ToLower(strings.TrimSpace(query.Search))
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.Status = strings.ToLower(strings.TrimSpace(query.Status))
	query.ModelID = strings.TrimSpace(query.ModelID)
	query.SiteID = strings.TrimSpace(query.SiteID)
	query.DepartmentID = strings.TrimSpace(query.DepartmentID)
	query.UserID = strings.TrimSpace(query.UserID)
	query.DeploymentContext = strings.ToLower(strings.TrimSpace(query.DeploymentContext))
	if !validText(query.Search, 200) || (query.Kind != "" && !validKind(query.Kind)) ||
		(query.Status != "" && !validStatus(query.Status)) ||
		(query.ModelID != "" && !assetIDPattern.MatchString(query.ModelID)) ||
		(query.SiteID != "" && !referencePattern.MatchString(query.SiteID)) ||
		(query.DepartmentID != "" && !referencePattern.MatchString(query.DepartmentID)) ||
		(query.UserID != "" && !referencePattern.MatchString(query.UserID)) ||
		!validText(query.DeploymentContext, 200) {
		return Query{}, ErrInvalidInput
	}
	if query.Limit == 0 {
		query.Limit = defaultListLimit
	}
	if query.Limit < 1 || query.Limit > maximumListLimit {
		return Query{}, ErrInvalidInput
	}
	return query, nil
}

func normalizeModelInventoryQuery(query ModelInventoryQuery) (ModelInventoryQuery, error) {
	assets, err := normalizeQuery(Query{
		Status: query.Status, SiteID: query.SiteID, DepartmentID: query.DepartmentID,
		UserID: query.UserID, DeploymentContext: query.DeploymentContext, Limit: query.Limit,
	})
	if err != nil {
		return ModelInventoryQuery{}, err
	}
	query.Status, query.SiteID, query.DepartmentID = assets.Status, assets.SiteID, assets.DepartmentID
	query.UserID, query.DeploymentContext, query.Limit = assets.UserID, assets.DeploymentContext, assets.Limit
	query.GroupBy = strings.ToLower(strings.TrimSpace(query.GroupBy))
	switch query.GroupBy {
	case "", ModelInventoryGroupStatus, ModelInventoryGroupSite, ModelInventoryGroupDepartment,
		ModelInventoryGroupUser, ModelInventoryGroupDeployment:
	default:
		return ModelInventoryQuery{}, ErrInvalidInput
	}
	return query, nil
}

func normalizeModelQuery(query ModelQuery) (ModelQuery, error) {
	query.Search = strings.ToLower(strings.TrimSpace(query.Search))
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.Status = strings.ToLower(strings.TrimSpace(query.Status))
	if query.Status == "" {
		query.Status = "active"
	}
	if !validText(query.Search, 200) || (query.Kind != "" && !validKind(query.Kind)) ||
		(query.Status != "" && !validModelStatus(query.Status)) {
		return ModelQuery{}, ErrInvalidInput
	}
	if query.Limit == 0 {
		query.Limit = defaultListLimit
	}
	if query.Limit < 1 || query.Limit > maximumListLimit {
		return ModelQuery{}, ErrInvalidInput
	}
	return query, nil
}

func normalizeReferences(references References) References {
	return References{
		SiteID: strings.TrimSpace(references.SiteID), BuildingID: strings.TrimSpace(references.BuildingID),
		RoomID: strings.TrimSpace(references.RoomID), DepartmentID: strings.TrimSpace(references.DepartmentID),
		UserID: strings.TrimSpace(references.UserID),
	}
}

func validateReferences(references References) error {
	for _, value := range []string{references.SiteID, references.BuildingID, references.RoomID, references.DepartmentID, references.UserID} {
		if value != "" && !referencePattern.MatchString(value) {
			return ErrInvalidInput
		}
	}
	if references.BuildingID != "" && references.SiteID == "" {
		return ErrInvalidInput
	}
	if references.RoomID != "" && (references.SiteID == "" || references.BuildingID == "") {
		return ErrInvalidInput
	}
	return nil
}

func validKind(value string) bool {
	_, ok := validKinds[value]
	return ok
}

func validStatus(value string) bool {
	_, ok := validStatuses[value]
	return ok
}

func validModelStatus(value string) bool {
	_, ok := validModelStatuses[value]
	return ok
}

func validText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func validTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

func cloneDate(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSpecifications(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func snapshotModelContext(model domain.AssetModel, assetKind string, appliedAt time.Time) *domain.AssetModelContext {
	if model.ID == "" {
		return nil
	}
	context := &domain.AssetModelContext{
		Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber, Kind: model.Kind,
		VendorIdentifier: model.VendorIdentifier, Specifications: cloneSpecifications(model.Specifications),
		SupportURL: model.SupportURL, WarrantyMonths: model.WarrantyMonths, UsefulLifeMonths: model.UsefulLifeMonths,
		SourceSystemID: model.SourceSystemID, SourceRecordID: model.SourceRecordID, ModelRevision: model.Revision,
		DefaultsEffectiveAt: model.UpdatedAt.UTC(), AppliedAt: appliedAt.UTC(), Overrides: []string{},
	}
	if assetKind != model.Kind {
		context.Overrides = []string{"kind"}
	}
	return context
}

func updatedModelContext(existing domain.Asset, model domain.AssetModel, assetKind string, appliedAt time.Time) *domain.AssetModelContext {
	if model.ID == "" {
		return nil
	}
	if existing.ModelID != model.ID || existing.ModelContext == nil {
		return snapshotModelContext(model, assetKind, appliedAt)
	}
	context := cloneModelContext(existing.ModelContext)
	context.Overrides = []string{}
	if assetKind != context.Kind {
		context.Overrides = []string{"kind"}
	}
	return context
}

func cloneModelContext(value *domain.AssetModelContext) *domain.AssetModelContext {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Specifications = cloneSpecifications(value.Specifications)
	cloned.Overrides = append([]string{}, value.Overrides...)
	return &cloned
}

func repeatedNormalizedValue(seen map[string]struct{}, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	if _, exists := seen[value]; exists {
		return true
	}
	seen[value] = struct{}{}
	return false
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:atlas"
}

func (s *Service) lifecycleEvent(ctx context.Context, asset domain.Asset, fromStatus, toStatus, note string) (domain.AssetLifecycleEvent, error) {
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return domain.AssetLifecycleEvent{}, fmt.Errorf("create lifecycle event id: %w", err)
	}
	return domain.AssetLifecycleEvent{
		ID: id, OrganizationID: s.organizationID, AssetID: asset.ID,
		FromStatus: fromStatus, ToStatus: toStatus, Note: note,
		Revision: asset.Revision, ActorID: actorFromContext(ctx), OccurredAt: asset.UpdatedAt,
	}, nil
}

func (s *Service) auditAsset(ctx context.Context, action, resourceID string, metadata map[string]string) error {
	return s.auditRecord(ctx, action, "asset", resourceID, metadata)
}

func (s *Service) auditRecord(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	if strings.HasPrefix(action, "atlas.model.") {
		metadata["requirementId"] = ModelRequirementID
	} else {
		metadata["requirementId"] = RequirementID
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorFromContext(ctx),
		CorrelationID: scope.CorrelationID, Action: action, ResourceType: resourceType, ResourceID: resourceID,
		OccurredAt: s.now().UTC(), Metadata: metadata,
	})
}

func (s *Service) validateModelReference(ctx context.Context, modelID string) (domain.AssetModel, error) {
	if modelID == "" {
		return domain.AssetModel{}, nil
	}
	model, err := s.store.GetModel(ctx, s.organizationID, modelID)
	if err != nil {
		return domain.AssetModel{}, err
	}
	if model.Status != "active" {
		return domain.AssetModel{}, ErrConflict
	}
	return model, nil
}

func modelAuditMetadata(model domain.AssetModel) map[string]string {
	return map[string]string{
		"manufacturer": model.Manufacturer, "modelNumber": model.ModelNumber,
		"kind": model.Kind, "status": model.Status, "revision": fmt.Sprintf("%d", model.Revision),
	}
}

func withModelContextAudit(asset domain.Asset, metadata map[string]string) map[string]string {
	if asset.ModelContext == nil {
		return metadata
	}
	metadata["modelId"] = asset.ModelID
	metadata["modelRevision"] = fmt.Sprintf("%d", asset.ModelContext.ModelRevision)
	metadata["modelOverrides"] = strings.Join(asset.ModelContext.Overrides, ",")
	metadata["modelRequirementId"] = ModelRequirementID
	return metadata
}
