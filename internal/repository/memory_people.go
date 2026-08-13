package repository

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-008. Features: identity.directory, integrations.protocols, threads.relationships.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/people"
)

type MemoryPeopleStore struct {
	mu                 sync.RWMutex
	sites              map[string]people.Site
	buildings          map[string]people.Building
	rooms              map[string]people.Room
	departments        map[string]people.Department
	identities         map[string]people.Identity
	assignments        map[string]people.AssetAssignment
	siteByName         map[string]string
	buildingByName     map[string]string
	roomByNumber       map[string]string
	departmentByName   map[string]string
	identityByEmail    map[string]string
	identityByProvider map[string]string
}

var _ people.Store = (*MemoryPeopleStore)(nil)

func NewMemoryPeopleStore() *MemoryPeopleStore {
	return &MemoryPeopleStore{
		sites:              make(map[string]people.Site),
		buildings:          make(map[string]people.Building),
		rooms:              make(map[string]people.Room),
		departments:        make(map[string]people.Department),
		identities:         make(map[string]people.Identity),
		assignments:        make(map[string]people.AssetAssignment),
		siteByName:         make(map[string]string),
		buildingByName:     make(map[string]string),
		roomByNumber:       make(map[string]string),
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

func (s *MemoryPeopleStore) CreateBuilding(_ context.Context, building people.Building) (people.Building, error) {
	if !validMemoryBuilding(building) {
		return people.Building{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	site, exists := s.sites[building.SiteID]
	if !exists || site.OrganizationID != building.OrganizationID {
		return people.Building{}, people.ErrReferenceMissing
	}
	nameKey := peopleKey(building.OrganizationID, building.SiteID, building.NormalizedName)
	if _, exists := s.buildings[building.ID]; exists {
		return people.Building{}, people.ErrConflict
	}
	if _, exists := s.buildingByName[nameKey]; exists {
		return people.Building{}, people.ErrConflict
	}
	s.buildings[building.ID] = building
	s.buildingByName[nameKey] = building.ID
	return building, nil
}

func (s *MemoryPeopleStore) GetBuilding(_ context.Context, organizationID, id string) (people.Building, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	building, exists := s.buildings[id]
	if !exists || building.OrganizationID != organizationID {
		return people.Building{}, people.ErrNotFound
	}
	return building, nil
}

func (s *MemoryPeopleStore) ListBuildings(_ context.Context, organizationID, siteID string, visibility people.Visibility) ([]people.Building, error) {
	if organizationID == "" {
		return nil, people.ErrInvalidInput
	}
	if visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]people.Building, 0)
	for _, building := range s.buildings {
		if building.OrganizationID != organizationID || (siteID != "" && building.SiteID != siteID) {
			continue
		}
		if !s.siteVisible(organizationID, building.SiteID, visibility) {
			continue
		}
		result = append(result, building)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NormalizedName == result[j].NormalizedName {
			return result[i].ID < result[j].ID
		}
		return result[i].NormalizedName < result[j].NormalizedName
	})
	return result, nil
}

func (s *MemoryPeopleStore) CreateRoom(_ context.Context, room people.Room) (people.Room, error) {
	if !validMemoryRoom(room) {
		return people.Room{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	building, exists := s.buildings[room.BuildingID]
	if !exists || building.OrganizationID != room.OrganizationID || building.SiteID != room.SiteID {
		return people.Room{}, people.ErrReferenceMissing
	}
	numberKey := peopleKey(room.OrganizationID, room.BuildingID, room.NormalizedNumber)
	if _, exists := s.rooms[room.ID]; exists {
		return people.Room{}, people.ErrConflict
	}
	if _, exists := s.roomByNumber[numberKey]; exists {
		return people.Room{}, people.ErrConflict
	}
	s.rooms[room.ID] = room
	s.roomByNumber[numberKey] = room.ID
	return room, nil
}

func (s *MemoryPeopleStore) GetRoom(_ context.Context, organizationID, id string) (people.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	room, exists := s.rooms[id]
	if !exists || room.OrganizationID != organizationID {
		return people.Room{}, people.ErrNotFound
	}
	return room, nil
}

func (s *MemoryPeopleStore) ListRooms(_ context.Context, organizationID, siteID, buildingID string, visibility people.Visibility) ([]people.Room, error) {
	if organizationID == "" {
		return nil, people.ErrInvalidInput
	}
	if visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]people.Room, 0)
	for _, room := range s.rooms {
		if room.OrganizationID != organizationID || (siteID != "" && room.SiteID != siteID) ||
			(buildingID != "" && room.BuildingID != buildingID) {
			continue
		}
		if !s.siteVisible(organizationID, room.SiteID, visibility) {
			continue
		}
		result = append(result, room)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NormalizedNumber == result[j].NormalizedNumber {
			return result[i].ID < result[j].ID
		}
		return result[i].NormalizedNumber < result[j].NormalizedNumber
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

func (s *MemoryPeopleStore) ListGraphLocations(_ context.Context, organizationID string, query people.GraphLocationQuery, visibility people.Visibility) (people.GraphLocations, error) {
	if organizationID == "" || visibility.Empty() || !query.Valid() {
		return people.GraphLocations{}, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	visibleSites := stringSet(visibility.SiteIDs)
	visibleDepartments := stringSet(visibility.DepartmentIDs)
	if !visibility.All {
		for _, department := range s.departments {
			if department.OrganizationID == organizationID && department.SiteID != "" {
				if _, allowed := visibleDepartments[department.ID]; allowed {
					visibleSites[department.SiteID] = struct{}{}
				}
			}
		}
	}
	siteIDs, buildingIDs, roomIDs := stringSet(query.SiteIDs), stringSet(query.BuildingIDs), stringSet(query.RoomIDs)
	departmentIDs := stringSet(query.DepartmentIDs)
	parentSiteIDs, parentBuildingIDs := stringSet(query.ParentSiteIDs), stringSet(query.ParentBuildingIDs)
	hasSelector := query.DirectOrganizationChildren || len(siteIDs)+len(buildingIDs)+len(roomIDs)+len(departmentIDs)+len(parentSiteIDs)+len(parentBuildingIDs) > 0
	search := strings.ToLower(query.LabelSearch)
	type candidate struct {
		kind       people.GraphLocationKind
		id, label  string
		site       *people.Site
		building   *people.Building
		room       *people.Room
		department *people.Department
	}
	candidates := make([]candidate, 0, query.Limit)
	if query.Kind == "" || query.Kind == people.GraphLocationSite {
		for _, item := range s.sites {
			if item.OrganizationID != organizationID || item.Status != people.StatusActive || !visibility.All && !setContains(visibleSites, item.ID) {
				continue
			}
			if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
				continue
			}
			if hasSelector && !setContains(siteIDs, item.ID) && !query.DirectOrganizationChildren {
				continue
			}
			copy := item
			candidates = append(candidates, candidate{kind: people.GraphLocationSite, id: item.ID, label: item.Name, site: &copy})
		}
	}
	if query.Kind == "" || query.Kind == people.GraphLocationBuilding {
		for _, item := range s.buildings {
			if item.OrganizationID != organizationID || item.Status != people.StatusActive || !s.siteVisible(organizationID, item.SiteID, visibility) {
				continue
			}
			if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
				continue
			}
			if hasSelector && !setContains(buildingIDs, item.ID) && !setContains(parentSiteIDs, item.SiteID) {
				continue
			}
			copy := item
			candidates = append(candidates, candidate{kind: people.GraphLocationBuilding, id: item.ID, label: item.Name, building: &copy})
		}
	}
	if query.Kind == "" || query.Kind == people.GraphLocationRoom {
		for _, item := range s.rooms {
			if item.OrganizationID != organizationID || item.Status != people.StatusActive || !s.siteVisible(organizationID, item.SiteID, visibility) {
				continue
			}
			label := graphRoomLabel(item)
			if search != "" && !strings.Contains(strings.ToLower(label), search) {
				continue
			}
			if hasSelector && !setContains(roomIDs, item.ID) && !setContains(parentSiteIDs, item.SiteID) && !setContains(parentBuildingIDs, item.BuildingID) {
				continue
			}
			copy := item
			candidates = append(candidates, candidate{kind: people.GraphLocationRoom, id: item.ID, label: label, room: &copy})
		}
	}
	if query.Kind == "" || query.Kind == people.GraphLocationDepartment {
		for _, item := range s.departments {
			if item.OrganizationID != organizationID || item.Status != people.StatusActive {
				continue
			}
			if !visibility.All && !setContains(visibleDepartments, item.ID) && !setContains(visibleSites, item.SiteID) {
				continue
			}
			if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
				continue
			}
			direct := query.DirectOrganizationChildren && item.SiteID == ""
			if hasSelector && !setContains(departmentIDs, item.ID) && !setContains(parentSiteIDs, item.SiteID) && !direct {
				continue
			}
			copy := item
			candidates = append(candidates, candidate{kind: people.GraphLocationDepartment, id: item.ID, label: item.Name, department: &copy})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].kind != candidates[j].kind {
			return candidates[i].kind < candidates[j].kind
		}
		left, right := strings.ToLower(candidates[i].label), strings.ToLower(candidates[j].label)
		if left != right {
			return left < right
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}
	result := people.GraphLocations{Sites: []people.Site{}, Buildings: []people.Building{}, Rooms: []people.Room{}, Departments: []people.Department{}}
	for _, item := range candidates {
		switch item.kind {
		case people.GraphLocationSite:
			result.Sites = append(result.Sites, *item.site)
		case people.GraphLocationBuilding:
			result.Buildings = append(result.Buildings, *item.building)
		case people.GraphLocationRoom:
			result.Rooms = append(result.Rooms, *item.room)
		case people.GraphLocationDepartment:
			result.Departments = append(result.Departments, *item.department)
		}
	}
	return result, nil
}

func graphRoomLabel(room people.Room) string {
	label := "Room " + room.Number
	if room.Name != "" {
		label += " · " + room.Name
	}
	return label
}

func setContains(values map[string]struct{}, value string) bool {
	_, present := values[value]
	return present
}

// GraphIdentityVisible is the O(1) in-memory companion to PostgreSQL's
// pre-limit identity EXISTS predicate for user-linked graph assets.
func (s *MemoryPeopleStore) GraphIdentityVisible(organizationID, identityID string, visibility people.Visibility) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	identity, present := s.identities[identityID]
	if !present || identity.OrganizationID != organizationID || identity.Status != people.StatusActive {
		return false
	}
	return visibility.All || setContains(stringSet(visibility.SiteIDs), identity.SiteID) ||
		setContains(stringSet(visibility.DepartmentIDs), identity.DepartmentID)
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

func (s *MemoryPeopleStore) GetIdentityByProvider(_ context.Context, organizationID, provider, providerSubject string) (people.Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, exists := s.identityByProvider[peopleKey(organizationID, provider, providerSubject)]
	if !exists {
		return people.Identity{}, people.ErrNotFound
	}
	identity, exists := s.identities[id]
	if !exists || identity.OrganizationID != organizationID {
		return people.Identity{}, people.ErrNotFound
	}
	return identity, nil
}

func (s *MemoryPeopleStore) GetIdentityByEmail(_ context.Context, organizationID, normalizedEmail string) (people.Identity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, exists := s.identityByEmail[peopleKey(organizationID, normalizedEmail)]
	if !exists {
		return people.Identity{}, people.ErrNotFound
	}
	identity, exists := s.identities[id]
	if !exists || identity.OrganizationID != organizationID {
		return people.Identity{}, people.ErrNotFound
	}
	return identity, nil
}

func (s *MemoryPeopleStore) ReconcileIdentity(_ context.Context, identity people.Identity, expectedRevision uint64) (people.Identity, error) {
	if !validMemoryIdentity(identity) {
		return people.Identity{}, people.ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.identities[identity.ID]
	if !exists || existing.OrganizationID != identity.OrganizationID {
		return people.Identity{}, people.ErrNotFound
	}
	if existing.Revision != expectedRevision || identity.Revision != expectedRevision+1 || identity.CreatedAt != existing.CreatedAt {
		return people.Identity{}, people.ErrConflict
	}
	if identity.NormalizedEmail != existing.NormalizedEmail && identity.NormalizedEmail != "" {
		if otherID, duplicate := s.identityByEmail[peopleKey(identity.OrganizationID, identity.NormalizedEmail)]; duplicate && otherID != identity.ID {
			return people.Identity{}, people.ErrConflict
		}
	}
	if identity.Provider != existing.Provider || identity.ProviderSubject != existing.ProviderSubject {
		return people.Identity{}, people.ErrConflict
	}
	if existing.NormalizedEmail != "" {
		delete(s.identityByEmail, peopleKey(existing.OrganizationID, existing.NormalizedEmail))
	}
	if identity.NormalizedEmail != "" {
		s.identityByEmail[peopleKey(identity.OrganizationID, identity.NormalizedEmail)] = identity.ID
	}
	s.identities[identity.ID] = identity
	return identity, nil
}

func (s *MemoryPeopleStore) DeleteIdentity(_ context.Context, organizationID, id string, expectedRevision uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, exists := s.identities[id]
	if !exists || identity.OrganizationID != organizationID {
		return people.ErrNotFound
	}
	if identity.Revision != expectedRevision {
		return people.ErrConflict
	}
	if identity.NormalizedEmail != "" {
		delete(s.identityByEmail, peopleKey(identity.OrganizationID, identity.NormalizedEmail))
	}
	if identity.Provider != "" {
		delete(s.identityByProvider, peopleKey(identity.OrganizationID, identity.Provider, identity.ProviderSubject))
	}
	delete(s.identities, id)
	return nil
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

func (s *MemoryPeopleStore) ListGraphIdentities(_ context.Context, organizationID string, query people.GraphIdentityQuery, visibility people.Visibility) ([]people.Identity, error) {
	if organizationID == "" || visibility.Empty() || !query.Valid() {
		return nil, people.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	directoryDepartments := stringSet(visibility.DepartmentIDs)
	directorySites := stringSet(visibility.SiteIDs)
	identityIDs := stringSet(query.IdentityIDs)
	departmentIDs := stringSet(query.DepartmentIDs)
	siteIDs := stringSet(query.SiteIDs)
	hasSelector := len(identityIDs)+len(departmentIDs)+len(siteIDs) > 0
	search := strings.ToLower(query.LabelSearch)
	result := make([]people.Identity, 0, query.Limit)
	for _, identity := range s.identities {
		if identity.OrganizationID != organizationID || identity.Status != people.StatusActive {
			continue
		}
		if !visibility.All {
			_, departmentAllowed := directoryDepartments[identity.DepartmentID]
			_, siteAllowed := directorySites[identity.SiteID]
			if !departmentAllowed && !siteAllowed {
				continue
			}
		}
		if query.Kind != "" && identity.Kind != query.Kind || search != "" && !strings.Contains(strings.ToLower(identity.DisplayName), search) {
			continue
		}
		if query.DirectOrganizationChildren && (identity.DepartmentID != "" || identity.SiteID != "") {
			continue
		}
		if hasSelector {
			_, identityMatch := identityIDs[identity.ID]
			_, departmentMatch := departmentIDs[identity.DepartmentID]
			_, siteMatch := siteIDs[identity.SiteID]
			if !identityMatch && !departmentMatch && !siteMatch {
				continue
			}
		}
		result = append(result, identity)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].DisplayName), strings.ToLower(result[j].DisplayName)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
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

// siteVisible applies the same site/department scope expansion used for sites.
// The caller must hold at least a read lock.
func (s *MemoryPeopleStore) siteVisible(organizationID, siteID string, visibility people.Visibility) bool {
	if visibility.All {
		return true
	}
	for _, visibleSiteID := range visibility.SiteIDs {
		if visibleSiteID == siteID {
			return true
		}
	}
	for _, departmentID := range visibility.DepartmentIDs {
		department, exists := s.departments[departmentID]
		if exists && department.OrganizationID == organizationID && department.SiteID == siteID {
			return true
		}
	}
	return false
}

func validMemorySite(site people.Site) bool {
	return site.ID != "" && site.OrganizationID != "" && site.Name != "" && site.NormalizedName != "" &&
		(site.Status == people.StatusActive || site.Status == people.StatusInactive) && site.Revision > 0 &&
		!site.CreatedAt.IsZero() && !site.UpdatedAt.Before(site.CreatedAt) && validMemoryAddress(site.Address)
}

func validMemoryAddress(address people.Address) bool {
	if address.Empty() {
		return true
	}
	return address.Line1 != "" && address.City != "" && asciiCountryCode(address.Country) &&
		validMemoryText(address.Line1, 200) && validMemoryText(address.Line2, 200) &&
		validMemoryText(address.City, 100) && validMemoryText(address.Region, 100) &&
		validMemoryText(address.PostalCode, 32)
}

func validMemoryBuilding(building people.Building) bool {
	return building.ID != "" && building.OrganizationID != "" && building.SiteID != "" &&
		building.Name != "" && building.NormalizedName != "" &&
		(building.Status == people.StatusActive || building.Status == people.StatusInactive) && building.Revision > 0 &&
		!building.CreatedAt.IsZero() && !building.UpdatedAt.Before(building.CreatedAt)
}

func validMemoryRoom(room people.Room) bool {
	return room.ID != "" && room.OrganizationID != "" && room.SiteID != "" && room.BuildingID != "" &&
		room.Number != "" && room.NormalizedNumber != "" && validMemoryText(room.Number, 100) && validMemoryText(room.Name, 200) &&
		(room.Status == people.StatusActive || room.Status == people.StatusInactive) && room.Revision > 0 &&
		!room.CreatedAt.IsZero() && !room.UpdatedAt.Before(room.CreatedAt)
}

func validMemoryText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func asciiCountryCode(value string) bool {
	if len(value) != 2 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
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
