package repository

// In-memory Horizon adapter. Requirement: REQ-HORIZON-001. Feature: lifecycle.planning.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/maxlemke/stewardmesh/internal/horizon"
)

type MemoryHorizonStore struct {
	mu                sync.RWMutex
	plans             map[string]horizon.Plan
	versions          map[string][]horizon.PlanVersion
	kindDefaults      map[string]horizon.KindDefault
	replacementPlans  map[string]horizon.ReplacementPlan
	replacementAssets map[string]string
}

func NewMemoryHorizonStore() *MemoryHorizonStore {
	return &MemoryHorizonStore{
		plans: make(map[string]horizon.Plan), versions: make(map[string][]horizon.PlanVersion),
		kindDefaults:      make(map[string]horizon.KindDefault),
		replacementPlans:  make(map[string]horizon.ReplacementPlan),
		replacementAssets: make(map[string]string),
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

func (s *MemoryHorizonStore) ListReplacementPlans(_ context.Context, organizationID string) ([]horizon.ReplacementPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]horizon.ReplacementPlan, 0)
	for _, item := range s.replacementPlans {
		if item.OrganizationID == organizationID {
			items = append(items, s.cloneReplacementPlanLocked(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *MemoryHorizonStore) GetReplacementPlan(_ context.Context, organizationID, id string) (horizon.ReplacementPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, exists := s.replacementPlans[horizonKey(organizationID, id)]
	if !exists {
		return horizon.ReplacementPlan{}, horizon.ErrNotFound
	}
	return s.cloneReplacementPlanLocked(item), nil
}

func (s *MemoryHorizonStore) CreateReplacementPlan(_ context.Context, item horizon.ReplacementPlan) (horizon.ReplacementPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKey(item.OrganizationID, item.ID)
	if _, exists := s.replacementPlans[key]; exists {
		return horizon.ReplacementPlan{}, horizon.ErrConflict
	}
	item.AssetCount = 0
	s.replacementPlans[key] = item
	return item, nil
}

func (s *MemoryHorizonStore) UpdateReplacementPlan(_ context.Context, item horizon.ReplacementPlan, expectedRevision int64) (horizon.ReplacementPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKey(item.OrganizationID, item.ID)
	existing, exists := s.replacementPlans[key]
	if !exists {
		return horizon.ReplacementPlan{}, horizon.ErrNotFound
	}
	if existing.Revision != expectedRevision {
		return horizon.ReplacementPlan{}, horizon.ErrConflict
	}
	item.AssetCount = s.replacementPlanCountLocked(item.OrganizationID, item.ID)
	s.replacementPlans[key] = item
	return item, nil
}

func (s *MemoryHorizonStore) AssetReplacementPlanIDs(_ context.Context, organizationID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	assigned := make(map[string]string)
	prefix := organizationID + "\x00"
	for key, planID := range s.replacementAssets {
		if strings.HasPrefix(key, prefix) {
			assigned[strings.TrimPrefix(key, prefix)] = planID
		}
	}
	return assigned, nil
}

func (s *MemoryHorizonStore) ListReplacementPlanAssetIDs(_ context.Context, organizationID, planID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := organizationID + "\x00"
	ids := make([]string, 0)
	for key, assigned := range s.replacementAssets {
		if assigned == planID && strings.HasPrefix(key, prefix) {
			ids = append(ids, strings.TrimPrefix(key, prefix))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *MemoryHorizonStore) SetAssetReplacementPlan(_ context.Context, organizationID, assetID, planID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := horizonKey(organizationID, assetID)
	if planID == "" {
		delete(s.replacementAssets, key)
		return nil
	}
	if _, exists := s.replacementPlans[horizonKey(organizationID, planID)]; !exists {
		return horizon.ErrNotFound
	}
	s.replacementAssets[key] = planID
	return nil
}

func (s *MemoryHorizonStore) cloneReplacementPlanLocked(item horizon.ReplacementPlan) horizon.ReplacementPlan {
	item.AssetCount = s.replacementPlanCountLocked(item.OrganizationID, item.ID)
	return item
}

func (s *MemoryHorizonStore) replacementPlanCountLocked(organizationID, planID string) int {
	prefix := organizationID + "\x00"
	count := 0
	for key, assigned := range s.replacementAssets {
		if assigned == planID && strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
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
