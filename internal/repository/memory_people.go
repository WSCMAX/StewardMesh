package repository

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxlemke/stewardmesh/internal/people"
)

type MemoryPeopleStore struct {
	mu                 sync.RWMutex
	sites              map[string]people.Site
	departments        map[string]people.Department
	identities         map[string]people.Identity
	assignments        map[string]people.AssetAssignment
	siteByName         map[string]string
	departmentByName   map[string]string
	identityByEmail    map[string]string
	identityByProvider map[string]string
}

var _ people.Store = (*MemoryPeopleStore)(nil)

func NewMemoryPeopleStore() *MemoryPeopleStore {
	return &MemoryPeopleStore{
		sites:              make(map[string]people.Site),
		departments:        make(map[string]people.Department),
		identities:         make(map[string]people.Identity),
		assignments:        make(map[string]people.AssetAssignment),
		siteByName:         make(map[string]string),
		departmentByName:   make(map[string]string),
		identityByEmail:    make(map[string]string),
		identityByProvider: make(map[string]string),
	}
}

func (s *MemoryPeopleStore) CreateSite(_ context.Context, site people.Site) (people.Site, error) {
	if !validMemorySite(site) {
		return people.Site{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nameKey := peopleKey(site.OrganizationID, site.NormalizedName)
	if _, exists := s.sites[site.ID]; exists {
		return people.Site{}, people.ErrConflict
	}
	if _, exists := s.siteByName[nameKey]; exists {
		return people.Site{}, people.ErrConflict
	}
	s.sites[site.ID] = site
	s.siteByName[nameKey] = site.ID
	return site, nil
}

func (s *MemoryPeopleStore) GetSite(_ context.Context, organizationID, id string) (people.Site, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	site, exists := s.sites[id]
	if !exists || site.OrganizationID != organizationID {
		return people.Site{}, people.ErrNotFound
	}
	return site, nil
}

func (s *MemoryPeopleStore) ListSites(_ context.Context, organizationID string, visibility people.Visibility) ([]people.Site, error) {
	if organizationID == "" || visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	visibleSites := stringSet(visibility.SiteIDs)
	if !visibility.All {
		visibleDepartments := stringSet(visibility.DepartmentIDs)
		for _, department := range s.departments {
			if department.OrganizationID == organizationID && department.SiteID != "" {
				if _, allowed := visibleDepartments[department.ID]; allowed {
					visibleSites[department.SiteID] = struct{}{}
				}
			}
		}
	}
	result := make([]people.Site, 0)
	for _, site := range s.sites {
		if site.OrganizationID != organizationID {
			continue
		}
		if !visibility.All {
			if _, allowed := visibleSites[site.ID]; !allowed {
				continue
			}
		}
		result = append(result, site)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NormalizedName == result[j].NormalizedName {
			return result[i].ID < result[j].ID
		}
		return result[i].NormalizedName < result[j].NormalizedName
	})
	return result, nil
}

func (s *MemoryPeopleStore) CreateDepartment(_ context.Context, department people.Department) (people.Department, error) {
	if !validMemoryDepartment(department) {
		return people.Department{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if department.SiteID != "" {
		site, exists := s.sites[department.SiteID]
		if !exists || site.OrganizationID != department.OrganizationID {
			return people.Department{}, people.ErrReferenceMissing
		}
	}
	nameKey := peopleKey(department.OrganizationID, department.NormalizedName)
	if _, exists := s.departments[department.ID]; exists {
		return people.Department{}, people.ErrConflict
	}
	if _, exists := s.departmentByName[nameKey]; exists {
		return people.Department{}, people.ErrConflict
	}
	s.departments[department.ID] = department
	s.departmentByName[nameKey] = department.ID
	return department, nil
}

func (s *MemoryPeopleStore) GetDepartment(_ context.Context, organizationID, id string) (people.Department, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	department, exists := s.departments[id]
	if !exists || department.OrganizationID != organizationID {
		return people.Department{}, people.ErrNotFound
	}
	return department, nil
}

func (s *MemoryPeopleStore) ListDepartments(_ context.Context, organizationID string, visibility people.Visibility) ([]people.Department, error) {
	if organizationID == "" || visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	departments := stringSet(visibility.DepartmentIDs)
	sites := stringSet(visibility.SiteIDs)
	result := make([]people.Department, 0)
	for _, department := range s.departments {
		if department.OrganizationID != organizationID {
			continue
		}
		if !visibility.All {
			_, departmentAllowed := departments[department.ID]
			_, siteAllowed := sites[department.SiteID]
			if !departmentAllowed && !siteAllowed {
				continue
			}
		}
		result = append(result, department)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NormalizedName == result[j].NormalizedName {
			return result[i].ID < result[j].ID
		}
		return result[i].NormalizedName < result[j].NormalizedName
	})
	return result, nil
}

func (s *MemoryPeopleStore) CreateIdentity(_ context.Context, identity people.Identity) (people.Identity, error) {
	if !validMemoryIdentity(identity) {
		return people.Identity{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if identity.DepartmentID != "" {
		department, exists := s.departments[identity.DepartmentID]
		if !exists || department.OrganizationID != identity.OrganizationID {
			return people.Identity{}, people.ErrReferenceMissing
		}
	}
	if identity.SiteID != "" {
		site, exists := s.sites[identity.SiteID]
		if !exists || site.OrganizationID != identity.OrganizationID {
			return people.Identity{}, people.ErrReferenceMissing
		}
	}
	if _, exists := s.identities[identity.ID]; exists {
		return people.Identity{}, people.ErrConflict
	}
	if identity.NormalizedEmail != "" {
		key := peopleKey(identity.OrganizationID, identity.NormalizedEmail)
		if _, exists := s.identityByEmail[key]; exists {
			return people.Identity{}, people.ErrConflict
		}
		s.identityByEmail[key] = identity.ID
	}
	if identity.Provider != "" {
		key := peopleKey(identity.OrganizationID, identity.Provider, identity.ProviderSubject)
		if _, exists := s.identityByProvider[key]; exists {
			return people.Identity{}, people.ErrConflict
		}
		s.identityByProvider[key] = identity.ID
	}
	s.identities[identity.ID] = identity
	return identity, nil
}

func (s *MemoryPeopleStore) GetIdentity(_ context.Context, organizationID, id string) (people.Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	identity, exists := s.identities[id]
	if !exists || identity.OrganizationID != organizationID {
		return people.Identity{}, people.ErrNotFound
	}
	return identity, nil
}

func (s *MemoryPeopleStore) SearchIdentities(_ context.Context, organizationID string, query people.IdentityQuery, visibility people.Visibility) ([]people.Identity, error) {
	if organizationID == "" || visibility.Empty() || query.Limit < 1 || query.Limit > 100 {
		return nil, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	departments := stringSet(visibility.DepartmentIDs)
	sites := stringSet(visibility.SiteIDs)
	search := strings.ToLower(query.Search)
	result := make([]people.Identity, 0, query.Limit)
	for _, identity := range s.identities {
		if identity.OrganizationID != organizationID {
			continue
		}
		if !visibility.All {
			_, departmentAllowed := departments[identity.DepartmentID]
			_, siteAllowed := sites[identity.SiteID]
			if !departmentAllowed && !siteAllowed {
				continue
			}
		}
		if query.Kind != "" && identity.Kind != query.Kind || query.Status != "" && identity.Status != query.Status ||
			query.DepartmentID != "" && identity.DepartmentID != query.DepartmentID || query.SiteID != "" && identity.SiteID != query.SiteID {
			continue
		}
		if search != "" && !strings.Contains(identity.NormalizedName, search) && !strings.Contains(identity.NormalizedEmail, search) {
			continue
		}
		result = append(result, identity)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NormalizedName == result[j].NormalizedName {
			return result[i].ID < result[j].ID
		}
		return result[i].NormalizedName < result[j].NormalizedName
	})
	if len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func (s *MemoryPeopleStore) CreateAssetAssignment(_ context.Context, assignment people.AssetAssignment, replaceActiveRole bool) (people.AssetAssignment, error) {
	if !validMemoryAssignment(assignment) {
		return people.AssetAssignment{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.assignments[assignment.ID]; exists {
		return people.AssetAssignment{}, people.ErrConflict
	}
	if !s.assigneeExists(assignment) {
		return people.AssetAssignment{}, people.ErrReferenceMissing
	}
	for id, existing := range s.assignments {
		if existing.OrganizationID != assignment.OrganizationID || existing.AssetID != assignment.AssetID || existing.EffectiveTo != nil {
			continue
		}
		if replaceActiveRole && existing.Role == assignment.Role {
			if !assignment.EffectiveFrom.After(existing.EffectiveFrom) {
				return people.AssetAssignment{}, people.ErrConflict
			}
			endedAt := assignment.EffectiveFrom
			existing.EffectiveTo = &endedAt
			s.assignments[id] = existing
			continue
		}
		if !replaceActiveRole && existing.Role == assignment.Role && existing.AssigneeKind == assignment.AssigneeKind && existing.AssigneeID == assignment.AssigneeID {
			return people.AssetAssignment{}, people.ErrConflict
		}
	}
	s.assignments[assignment.ID] = clonePeopleAssignment(assignment)
	return clonePeopleAssignment(assignment), nil
}

func (s *MemoryPeopleStore) EndAssetAssignment(_ context.Context, organizationID, assetID, assignmentID string, effectiveTo time.Time) (people.AssetAssignment, error) {
	if organizationID == "" || assetID == "" || assignmentID == "" || effectiveTo.IsZero() {
		return people.AssetAssignment{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	assignment, exists := s.assignments[assignmentID]
	if !exists || assignment.OrganizationID != organizationID || assignment.AssetID != assetID {
		return people.AssetAssignment{}, people.ErrNotFound
	}
	if assignment.EffectiveTo != nil || !effectiveTo.After(assignment.EffectiveFrom) {
		return people.AssetAssignment{}, people.ErrConflict
	}
	effectiveTo = effectiveTo.UTC()
	assignment.EffectiveTo = &effectiveTo
	s.assignments[assignmentID] = assignment
	return clonePeopleAssignment(assignment), nil
}

func (s *MemoryPeopleStore) ListAssetAssignments(_ context.Context, organizationID, assetID string) ([]people.AssetAssignment, error) {
	if organizationID == "" || assetID == "" {
		return nil, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]people.AssetAssignment, 0)
	for _, assignment := range s.assignments {
		if assignment.OrganizationID == organizationID && assignment.AssetID == assetID {
			result = append(result, clonePeopleAssignment(assignment))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].EffectiveFrom.Equal(result[j].EffectiveFrom) {
			return result[i].ID > result[j].ID
		}
		return result[i].EffectiveFrom.After(result[j].EffectiveFrom)
	})
	return result, nil
}

func (s *MemoryPeopleStore) assigneeExists(assignment people.AssetAssignment) bool {
	switch assignment.AssigneeKind {
	case people.AssigneeIdentity:
		identity, exists := s.identities[assignment.AssigneeID]
		return exists && identity.OrganizationID == assignment.OrganizationID
	case people.AssigneeDepartment:
		department, exists := s.departments[assignment.AssigneeID]
		return exists && department.OrganizationID == assignment.OrganizationID
	default:
		return false
	}
}

func validMemorySite(site people.Site) bool {
	return site.ID != "" && site.OrganizationID != "" && site.Name != "" && site.NormalizedName != "" &&
		(site.Status == people.StatusActive || site.Status == people.StatusInactive) && site.Revision > 0 &&
		!site.CreatedAt.IsZero() && !site.UpdatedAt.Before(site.CreatedAt)
}

func validMemoryDepartment(department people.Department) bool {
	return department.ID != "" && department.OrganizationID != "" && department.Name != "" && department.NormalizedName != "" &&
		(department.Status == people.StatusActive || department.Status == people.StatusInactive) && department.Revision > 0 &&
		!department.CreatedAt.IsZero() && !department.UpdatedAt.Before(department.CreatedAt)
}

func validMemoryIdentity(identity people.Identity) bool {
	return identity.ID != "" && identity.OrganizationID != "" && identity.DisplayName != "" && identity.NormalizedName != "" &&
		(identity.Kind == people.IdentityPerson || identity.Kind == people.IdentityShared || identity.Kind == people.IdentityPublic || identity.Kind == people.IdentityLab) &&
		(identity.Status == people.StatusActive || identity.Status == people.StatusInactive) && identity.Revision > 0 &&
		!identity.CreatedAt.IsZero() && !identity.UpdatedAt.Before(identity.CreatedAt)
}

func validMemoryAssignment(assignment people.AssetAssignment) bool {
	return assignment.ID != "" && assignment.OrganizationID != "" && assignment.AssetID != "" && assignment.AssigneeID != "" &&
		assignment.CreatedBy != "" && !assignment.EffectiveFrom.IsZero() && !assignment.CreatedAt.IsZero() &&
		((assignment.AssigneeKind == people.AssigneeIdentity && (assignment.Role == people.AssignmentPrimary || assignment.Role == people.AssignmentUser)) ||
			(assignment.AssigneeKind == people.AssigneeDepartment && assignment.Role == people.AssignmentDepartment))
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func peopleKey(values ...string) string {
	return strings.Join(values, "\x00")
}

func clonePeopleAssignment(assignment people.AssetAssignment) people.AssetAssignment {
	if assignment.EffectiveTo != nil {
		effectiveTo := *assignment.EffectiveTo
		assignment.EffectiveTo = &effectiveTo
	}
	return assignment
}
