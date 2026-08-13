package repository

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001. Features: inventory.assets, inventory.models.

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
)

type MemoryAtlasStore struct {
	mu        sync.RWMutex
	models    map[string]domain.AssetModel
	assets    map[string]domain.Asset
	lifecycle map[string][]domain.AssetLifecycleEvent
}

var _ atlas.Store = (*MemoryAtlasStore)(nil)

func NewMemoryAtlasStore() *MemoryAtlasStore {
	return &MemoryAtlasStore{
		models: make(map[string]domain.AssetModel), assets: make(map[string]domain.Asset),
		lifecycle: make(map[string][]domain.AssetLifecycleEvent),
	}
}

func atlasMemoryKey(organizationID, id string) string {
	return organizationID + "\x00" + id
}

func (s *MemoryAtlasStore) ListModels(_ context.Context, organizationID string, query atlas.ModelQuery) ([]domain.AssetModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	search := strings.ToLower(query.Search)
	items := make([]domain.AssetModel, 0)
	for _, model := range s.models {
		if model.OrganizationID != organizationID || (query.Kind != "" && model.Kind != query.Kind) ||
			(query.Status != "" && model.Status != query.Status) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{
			model.Manufacturer, model.Name, model.ModelNumber, model.VendorIdentifier,
		}, "\n")), search) {
			continue
		}
		model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
		items = append(items, cloneAssetModel(model))
	}
	sortAssetModels(items)
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryAtlasStore) GetModel(_ context.Context, organizationID, id string) (domain.AssetModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	model, ok := s.models[atlasMemoryKey(organizationID, id)]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	model.InstanceCount = s.modelInstanceCountLocked(organizationID, id)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) CreateModel(_ context.Context, model domain.AssetModel) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(model.OrganizationID, model.ID)
	if _, exists := s.models[key]; exists || s.modelIdentityConflict(model, "") {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	s.models[key] = cloneAssetModel(model)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) UpdateModel(_ context.Context, model domain.AssetModel, expectedRevision int64) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(model.OrganizationID, model.ID)
	existing, ok := s.models[key]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if existing.Revision != expectedRevision || model.Revision != expectedRevision+1 || s.modelIdentityConflict(model, model.ID) {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	s.models[key] = cloneAssetModel(model)
	model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) RetireModel(_ context.Context, organizationID, id string, expectedRevision int64, retiredAt time.Time) (domain.AssetModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(organizationID, id)
	model, ok := s.models[key]
	if !ok {
		return domain.AssetModel{}, atlas.ErrNotFound
	}
	if model.Revision != expectedRevision || model.Status == "retired" {
		return domain.AssetModel{}, atlas.ErrConflict
	}
	model.Status = "retired"
	model.Revision++
	model.UpdatedAt = retiredAt
	s.models[key] = cloneAssetModel(model)
	model.InstanceCount = s.modelInstanceCountLocked(model.OrganizationID, model.ID)
	return cloneAssetModel(model), nil
}

func (s *MemoryAtlasStore) ListAssets(_ context.Context, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	search := strings.ToLower(query.Search)
	items := make([]domain.Asset, 0)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || (query.Kind != "" && asset.Kind != query.Kind) ||
			(query.Status != "" && asset.Status != query.Status) || (query.ModelID != "" && asset.ModelID != query.ModelID) ||
			(query.SiteID != "" && asset.SiteID != query.SiteID) ||
			(query.DepartmentID != "" && asset.DepartmentID != query.DepartmentID) || (query.UserID != "" && asset.UserID != query.UserID) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{
			asset.Name, asset.AssetTag, asset.SerialNumber, asset.Hostname,
		}, "\n")), search) {
			continue
		}
		items = append(items, cloneAsset(asset))
	}
	sortAssets(items)
	if len(items) > query.Limit {
		items = items[:query.Limit]
	}
	return items, nil
}

func (s *MemoryAtlasStore) GetAsset(_ context.Context, organizationID, id string) (domain.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.assets[atlasMemoryKey(organizationID, id)]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	return cloneAsset(asset), nil
}

func (s *MemoryAtlasStore) CreateAsset(_ context.Context, asset domain.Asset, initialEvent domain.AssetLifecycleEvent) (domain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(asset.OrganizationID, asset.ID)
	if asset.ModelID != "" {
		model, exists := s.models[atlasMemoryKey(asset.OrganizationID, asset.ModelID)]
		if !exists || model.Status != "active" {
			return domain.Asset{}, atlas.ErrReferenceMissing
		}
	}
	if _, exists := s.assets[key]; exists || s.assetIdentityConflict(asset, "") {
		return domain.Asset{}, atlas.ErrConflict
	}
	s.assets[key] = cloneAsset(asset)
	s.lifecycle[key] = []domain.AssetLifecycleEvent{initialEvent}
	return cloneAsset(asset), nil
}

func (s *MemoryAtlasStore) UpdateAsset(_ context.Context, asset domain.Asset, expectedRevision int64, lifecycleEvent *domain.AssetLifecycleEvent) (domain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := atlasMemoryKey(asset.OrganizationID, asset.ID)
	existing, ok := s.assets[key]
	if !ok {
		return domain.Asset{}, atlas.ErrNotFound
	}
	if existing.Revision != expectedRevision || asset.Revision != expectedRevision+1 || s.assetIdentityConflict(asset, asset.ID) {
		return domain.Asset{}, atlas.ErrConflict
	}
	if asset.ModelID != "" {
		model, exists := s.models[atlasMemoryKey(asset.OrganizationID, asset.ModelID)]
		if !exists || model.Status != "active" {
			return domain.Asset{}, atlas.ErrReferenceMissing
		}
	}
	s.assets[key] = cloneAsset(asset)
	if lifecycleEvent != nil {
		s.lifecycle[key] = append(s.lifecycle[key], *lifecycleEvent)
	}
	return cloneAsset(asset), nil
}

func (s *MemoryAtlasStore) ListAssetLifecycle(_ context.Context, organizationID, assetID string) ([]domain.AssetLifecycleEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := atlasMemoryKey(organizationID, assetID)
	if _, ok := s.assets[key]; !ok {
		return nil, atlas.ErrNotFound
	}
	items := append([]domain.AssetLifecycleEvent(nil), s.lifecycle[key]...)
	sortLifecycle(items)
	return items, nil
}

func (s *MemoryAtlasStore) modelIdentityConflict(candidate domain.AssetModel, excludingID string) bool {
	for _, existing := range s.models {
		if existing.OrganizationID != candidate.OrganizationID || existing.ID == excludingID {
			continue
		}
		if strings.EqualFold(existing.Manufacturer, candidate.Manufacturer) &&
			strings.EqualFold(existing.Name, candidate.Name) &&
			strings.EqualFold(existing.ModelNumber, candidate.ModelNumber) {
			return true
		}
	}
	return false
}

func (s *MemoryAtlasStore) modelInstanceCountLocked(organizationID, modelID string) int {
	count := 0
	for _, asset := range s.assets {
		if asset.OrganizationID == organizationID && asset.ModelID == modelID {
			count++
		}
	}
	return count
}

func (s *MemoryAtlasStore) assetIdentityConflict(candidate domain.Asset, excludingID string) bool {
	for _, existing := range s.assets {
		if existing.OrganizationID != candidate.OrganizationID || existing.ID == excludingID {
			continue
		}
		if candidate.AssetTag != "" && strings.EqualFold(existing.AssetTag, candidate.AssetTag) {
			return true
		}
		if candidate.SerialNumber != "" && strings.EqualFold(existing.SerialNumber, candidate.SerialNumber) {
			return true
		}
	}
	return false
}

func cloneAsset(asset domain.Asset) domain.Asset {
	if asset.PurchaseDate != nil {
		value := *asset.PurchaseDate
		asset.PurchaseDate = &value
	}
	return asset
}

func cloneAssetModel(model domain.AssetModel) domain.AssetModel {
	if len(model.Specifications) > 0 {
		specifications := make(map[string]string, len(model.Specifications))
		for key, value := range model.Specifications {
			specifications[key] = value
		}
		model.Specifications = specifications
	}
	return model
}
