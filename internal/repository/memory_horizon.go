package repository

// In-memory Horizon adapter. Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"sort"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/horizon"
)

type MemoryHorizonStore struct {
	mu           sync.RWMutex
	plans        map[string]horizon.Plan
	versions     map[string][]horizon.PlanVersion
	kindDefaults map[string]horizon.KindDefault
}

func NewMemoryHorizonStore() *MemoryHorizonStore {
	return &MemoryHorizonStore{
		plans: make(map[string]horizon.Plan), versions: make(map[string][]horizon.PlanVersion),
		kindDefaults: make(map[string]horizon.KindDefault),
	}
}

func horizonKindDefaultKey(organizationID, assetKind, scenario string) string {
	return organizationID + "\x00" + assetKind + "\x00" + scenario
}

func horizonKey(organizationID, id string) string { return organizationID + "\x00" + id }

func (s *MemoryHorizonStore) ListPlans(_ context.Context, organizationID string, query horizon.ListPlansQuery) ([]horizon.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]horizon.Plan, 0)
	for _, item := range s.plans {
		if item.OrganizationID == organizationID && (query.AssetID == "" || item.AssetID == query.AssetID) && (query.Scenario == "" || item.Scenario == query.Scenario) {
			items = append(items, cloneHorizonPlan(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AssetID == items[j].AssetID {
			if items[i].Scenario == items[j].Scenario {
				return items[i].ID < items[j].ID
			}
			return items[i].Scenario < items[j].Scenario
		}
		return items[i].AssetID < items[j].AssetID
	})
	return items, nil
}

func (s *MemoryHorizonStore) GetPlan(_ context.Context, organizationID, id string) (horizon.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.plans[horizonKey(organizationID, id)]
	if !exists {
		return horizon.Plan{}, horizon.ErrNotFound
	}
	return cloneHorizonPlan(item), nil
}

func (s *MemoryHorizonStore) CreatePlan(_ context.Context, item horizon.Plan, version horizon.PlanVersion) (horizon.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKey(item.OrganizationID, item.ID)
	if _, exists := s.plans[key]; exists {
		return horizon.Plan{}, horizon.ErrConflict
	}
	for _, existing := range s.plans {
		if existing.OrganizationID == item.OrganizationID && existing.AssetID == item.AssetID && existing.Scenario == item.Scenario {
			return horizon.Plan{}, horizon.ErrConflict
		}
	}
	s.plans[key] = cloneHorizonPlan(item)
	s.versions[key] = []horizon.PlanVersion{cloneHorizonVersion(version)}
	return cloneHorizonPlan(item), nil
}

func (s *MemoryHorizonStore) UpdatePlan(_ context.Context, item horizon.Plan, expectedRevision int64, version horizon.PlanVersion) (horizon.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKey(item.OrganizationID, item.ID)
	existing, exists := s.plans[key]
	if !exists {
		return horizon.Plan{}, horizon.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return horizon.Plan{}, horizon.ErrConflict
	}
	for candidateKey, candidate := range s.plans {
		if candidateKey != key && candidate.OrganizationID == item.OrganizationID && candidate.AssetID == item.AssetID && candidate.Scenario == item.Scenario {
			return horizon.Plan{}, horizon.ErrConflict
		}
	}
	s.plans[key] = cloneHorizonPlan(item)
	s.versions[key] = append(s.versions[key], cloneHorizonVersion(version))
	return cloneHorizonPlan(item), nil
}

func (s *MemoryHorizonStore) ListPlanVersions(_ context.Context, organizationID, planID string) ([]horizon.PlanVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := horizonKey(organizationID, planID)
	if _, exists := s.plans[key]; !exists {
		return nil, horizon.ErrNotFound
	}
	items := append([]horizon.PlanVersion(nil), s.versions[key]...)
	for index := range items {
		items[index] = cloneHorizonVersion(items[index])
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Revision > items[j].Revision })
	return items, nil
}

func (s *MemoryHorizonStore) ListKindDefaults(_ context.Context, organizationID, scenario string) ([]horizon.KindDefault, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]horizon.KindDefault, 0)
	for _, item := range s.kindDefaults {
		if item.OrganizationID == organizationID && (scenario == "" || item.Scenario == scenario) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AssetKind == items[j].AssetKind {
			return items[i].Scenario < items[j].Scenario
		}
		return items[i].AssetKind < items[j].AssetKind
	})
	return items, nil
}

func (s *MemoryHorizonStore) UpsertKindDefault(_ context.Context, item horizon.KindDefault) (horizon.KindDefault, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKindDefaultKey(item.OrganizationID, item.AssetKind, item.Scenario)
	if existing, ok := s.kindDefaults[key]; ok {
		if item.Revision != existing.Revision {
			return horizon.KindDefault{}, horizon.ErrConflict
		}
		item.Revision = existing.Revision + 1
		item.CreatedAt = existing.CreatedAt
	}
	s.kindDefaults[key] = item
	return item, nil
}

func cloneHorizonPlan(item horizon.Plan) horizon.Plan {
	if item.ReplacementDate != nil {
		date := *item.ReplacementDate
		item.ReplacementDate = &date
	}
	if item.DerivedReplacementDate != nil {
		date := *item.DerivedReplacementDate
		item.DerivedReplacementDate = &date
	}
	return item
}

func cloneHorizonVersion(item horizon.PlanVersion) horizon.PlanVersion {
	if item.ReplacementDate != nil {
		date := *item.ReplacementDate
		item.ReplacementDate = &date
	}
	return item
}
