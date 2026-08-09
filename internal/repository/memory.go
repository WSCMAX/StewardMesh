package repository

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

var ErrNotFound = errors.New("record not found")

type MemoryAssetRepository struct {
	mu     sync.RWMutex
	assets map[string]domain.Asset
}

type MemoryCatalog struct {
	tags  []domain.Tag
	goals []domain.Goal
}

type MemoryOrganizationRepository struct {
	mu            sync.RWMutex
	organizations map[string]domain.Organization
}

func NewMemoryOrganizationRepository() *MemoryOrganizationRepository {
	return &MemoryOrganizationRepository{organizations: make(map[string]domain.Organization)}
}

func (r *MemoryOrganizationRepository) GetOrganization(_ context.Context, id string) (domain.Organization, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	organization, ok := r.organizations[id]
	if !ok {
		return domain.Organization{}, ErrNotFound
	}
	return organization, nil
}

func (r *MemoryOrganizationRepository) BootstrapOrganization(_ context.Context, organization domain.Organization) (domain.Organization, bool, error) {
	if organization.ID == "" || organization.Name == "" || organization.CreatedAt.IsZero() ||
		organization.UpdatedAt.IsZero() || organization.UpdatedAt.Before(organization.CreatedAt) {
		return domain.Organization{}, false, errors.New("valid organization identity and timestamps are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.organizations[organization.ID]; ok {
		existing.Name = organization.Name
		existing.UpdatedAt = organization.UpdatedAt
		r.organizations[organization.ID] = existing
		return existing, false, nil
	}
	r.organizations[organization.ID] = organization
	return organization, true, nil
}

func NewMemoryCatalog() *MemoryCatalog {
	return &MemoryCatalog{}
}

func (c *MemoryCatalog) ListTags(_ context.Context) ([]domain.Tag, error) {
	return append([]domain.Tag(nil), c.tags...), nil
}

func (c *MemoryCatalog) ListGoals(_ context.Context) ([]domain.Goal, error) {
	return append([]domain.Goal(nil), c.goals...), nil
}

func NewMemoryAssetRepository() *MemoryAssetRepository {
	return &MemoryAssetRepository{assets: make(map[string]domain.Asset)}
}

func (r *MemoryAssetRepository) List(_ context.Context) ([]domain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Asset, 0, len(r.assets))
	for _, asset := range r.assets {
		result = append(result, asset)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (r *MemoryAssetRepository) Get(_ context.Context, id string) (domain.Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	asset, ok := r.assets[id]
	if !ok {
		return domain.Asset{}, ErrNotFound
	}
	return asset, nil
}

func (r *MemoryAssetRepository) Create(_ context.Context, asset domain.Asset) (domain.Asset, error) {
	if asset.ID == "" || asset.Name == "" || asset.Kind == "" {
		return domain.Asset{}, errors.New("id, name, and kind are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.assets[asset.ID]; exists {
		return domain.Asset{}, errors.New("asset already exists")
	}
	r.assets[asset.ID] = asset
	return asset, nil
}
