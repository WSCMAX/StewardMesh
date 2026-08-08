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
	departments []domain.Department
	users       []domain.User
	tags        []domain.Tag
	goals       []domain.Goal
}

func NewMemoryCatalog() *MemoryCatalog {
	return &MemoryCatalog{}
}

func (c *MemoryCatalog) ListDepartments(_ context.Context) ([]domain.Department, error) {
	return append([]domain.Department(nil), c.departments...), nil
}

func (c *MemoryCatalog) ListUsers(_ context.Context) ([]domain.User, error) {
	return append([]domain.User(nil), c.users...), nil
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
