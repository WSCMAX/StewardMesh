package repository

// Requirement: REQ-ATLAS-001. Feature: inventory.assets.

import (
	"context"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
)

type MemoryAtlasStore struct {
	mu        sync.RWMutex
	assets    map[string]domain.Asset
	lifecycle map[string][]domain.AssetLifecycleEvent
}

var _ atlas.Store = (*MemoryAtlasStore)(nil)

func NewMemoryAtlasStore() *MemoryAtlasStore {
	return &MemoryAtlasStore{
		assets: make(map[string]domain.Asset), lifecycle: make(map[string][]domain.AssetLifecycleEvent),
	}
}

func atlasMemoryKey(organizationID, id string) string {
	return organizationID + "\x00" + id
}

func (s *MemoryAtlasStore) ListAssets(_ context.Context, organizationID string, query atlas.Query) ([]domain.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	search := strings.ToLower(query.Search)
	items := make([]domain.Asset, 0)
	for _, asset := range s.assets {
		if asset.OrganizationID != organizationID || (query.Kind != "" && asset.Kind != query.Kind) ||
			(query.Status != "" && asset.Status != query.Status) || (query.SiteID != "" && asset.SiteID != query.SiteID) ||
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
