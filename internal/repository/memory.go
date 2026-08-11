package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/domain"
)

var ErrNotFound = errors.New("record not found")

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

func sortAssets(items []domain.Asset) {
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Name), strings.ToLower(items[j].Name)
		if left == right {
			return items[i].ID < items[j].ID
		}
		return left < right
	})
}

func sortLifecycle(items []domain.AssetLifecycleEvent) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Revision == items[j].Revision {
			return items[i].ID < items[j].ID
		}
		return items[i].Revision < items[j].Revision
	})
}
