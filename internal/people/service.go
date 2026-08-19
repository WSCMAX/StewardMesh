package people

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

const defaultSearchLimit = 50
const maximumSearchLimit = 100

var (
	providerPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	recordIDPattern        = regexp.MustCompile(`^[a-f0-9]{32}$`)
	assetIDPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	countryCodePattern     = regexp.MustCompile(`^[A-Za-z]{2}$`)
	maximumPortableInstant = time.Date(9999, time.December, 31, 23, 59, 59, 999999000, time.UTC)
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	assets         AssetReader
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

type exchangeImporter struct{ service *Service }

func (*exchangeImporter) peopleExchangeImporter() {}

func NewService(store Store, assets AssetReader, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, assets, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, assets AssetReader, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	if store == nil || assets == nil || auditor == nil {
		return nil, nil, errors.New("people store, asset reader, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, nil, errors.New("people organization id is required")
	}
	clock := configuration.Now
	if clock == nil {
		clock = time.Now
	}
	service := &Service{
		store:          store,
		assets:         assets,
		writes:         writes,
		auditor:        auditor,
		organizationID: configuration.OrganizationID,
		now:            func() time.Time { return portabletime.Normalize(clock()) },
	}
	return service, &exchangeImporter{service: service}, nil
}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) ExchangeSnapshot(ctx context.Context, maximum int) (ExchangeSnapshot, error) {
	return s.store.ExchangeSnapshot(ctx, s.organizationID, maximum)
}

func (s *Service) GetSite(ctx context.Context, id string) (Site, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Site{}, ErrInvalidInput
	}
	return s.store.GetSite(ctx, s.organizationID, id)
}

func (s *Service) GetBuilding(ctx context.Context, id string) (Building, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Building{}, ErrInvalidInput
	}
	return s.store.GetBuilding(ctx, s.organizationID, id)
}

func (s *Service) GetRoom(ctx context.Context, id string) (Room, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Room{}, ErrInvalidInput
	}
	return s.store.GetRoom(ctx, s.organizationID, id)
}

func (s *Service) GetDepartment(ctx context.Context, id string) (Department, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Department{}, ErrInvalidInput
	}
	return s.store.GetDepartment(ctx, s.organizationID, id)
}

func (s *Service) GetIdentity(ctx context.Context, id string) (Identity, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return Identity{}, ErrInvalidInput
	}
	return s.store.GetIdentity(ctx, s.organizationID, id)
}

func (s *Service) GetAssetAssignment(ctx context.Context, id string) (AssetAssignment, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return AssetAssignment{}, ErrInvalidInput
	}
	return s.store.GetAssetAssignment(ctx, s.organizationID, id)
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func (s *Service) CreateSite(ctx context.Context, input CreateSiteInput) (Site, error) {
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Site{}, err
	}
	address, err := normalizeAddress(input.Address)
	if err != nil {
		return Site{}, err
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Site{}, fmt.Errorf("create site id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.site", id); err != nil {
		return Site{}, err
	}
	created, err := s.store.CreateSite(ctx, Site{
		ID:             id,
		OrganizationID: s.organizationID,
		Name:           name,
		NormalizedName: normalizedName,
		Address:        address,
		Status:         status,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return Site{}, err
	}
	requirementID := RequirementID
	if !created.Address.Empty() {
		requirementID = DirectoryExpansionRequirementID
	}
	if err := s.auditRequirement(ctx, requirementID, "people.site.created", "site", created.ID, nil); err != nil {
		return Site{}, fmt.Errorf("audit site creation: %w", err)
	}
	return created, nil
}

func (s *Service) ListSites(ctx context.Context, visibility Visibility) ([]Site, error) {
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	return s.store.ListSites(ctx, s.organizationID, visibility)
}

func (s *Service) UpdateSite(ctx context.Context, input UpdateSiteInput) (Site, error) {
	id := strings.TrimSpace(input.ID)
	if !recordIDPattern.MatchString(id) {
		return Site{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.site", id); err != nil {
		return Site{}, err
	}
	existing, err := s.store.GetSite(ctx, s.organizationID, id)
	if err != nil {
		return Site{}, err
	}
	if existing.Revision != input.Revision {
		return Site{}, ErrConflict
	}
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Site{}, err
	}
	address, err := normalizeAddress(input.Address)
	if err != nil {
		return Site{}, err
	}
	updated, err := s.store.UpdateSite(ctx, Site{
		ID:             existing.ID,
		OrganizationID: existing.OrganizationID,
		Name:           name,
		NormalizedName: normalizedName,
		Address:        address,
		Status:         status,
		Revision:       existing.Revision + 1,
		CreatedAt:      existing.CreatedAt,
		UpdatedAt:      s.now(),
	}, existing.Revision)
	if err != nil {
		return Site{}, err
	}
	requirementID := RequirementID
	if !updated.Address.Empty() {
		requirementID = DirectoryExpansionRequirementID
	}
	if err := s.auditRequirement(ctx, requirementID, "people.site.updated", "site", updated.ID, nil); err != nil {
		return Site{}, fmt.Errorf("audit site update: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateBuilding(ctx context.Context, input CreateBuildingInput) (Building, error) {
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Building{}, err
	}
	siteID := strings.TrimSpace(input.SiteID)
	if !recordIDPattern.MatchString(siteID) {
		return Building{}, ErrInvalidInput
	}
	if _, err := s.store.GetSite(ctx, s.organizationID, siteID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Building{}, ErrReferenceMissing
		}
		return Building{}, err
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Building{}, fmt.Errorf("create building id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.building", id); err != nil {
		return Building{}, err
	}
	created, err := s.store.CreateBuilding(ctx, Building{
		ID:             id,
		OrganizationID: s.organizationID,
		SiteID:         siteID,
		Name:           name,
		NormalizedName: normalizedName,
		Status:         status,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return Building{}, err
	}
	if err := s.auditRequirement(ctx, DirectoryExpansionRequirementID, "people.building.created", "building", created.ID, map[string]string{"siteId": created.SiteID}); err != nil {
		return Building{}, fmt.Errorf("audit building creation: %w", err)
	}
	return created, nil
}

func (s *Service) ListBuildings(ctx context.Context, siteID string, visibility Visibility) ([]Building, error) {
	siteID = strings.TrimSpace(siteID)
	if siteID != "" && !recordIDPattern.MatchString(siteID) {
		return nil, ErrInvalidInput
	}
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	return s.store.ListBuildings(ctx, s.organizationID, siteID, visibility)
}

func (s *Service) UpdateBuilding(ctx context.Context, input UpdateBuildingInput) (Building, error) {
	id := strings.TrimSpace(input.ID)
	siteID := strings.TrimSpace(input.SiteID)
	if !recordIDPattern.MatchString(id) || !recordIDPattern.MatchString(siteID) {
		return Building{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.building", id); err != nil {
		return Building{}, err
	}
	existing, err := s.store.GetBuilding(ctx, s.organizationID, id)
	if err != nil {
		return Building{}, err
	}
	if existing.Revision != input.Revision {
		return Building{}, ErrConflict
	}
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Building{}, err
	}
	if _, err := s.store.GetSite(ctx, s.organizationID, siteID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Building{}, ErrReferenceMissing
		}
		return Building{}, err
	}
	if siteID != existing.SiteID {
		rooms, listErr := s.store.ListRooms(ctx, s.organizationID, "", existing.ID, Visibility{All: true})
		if listErr != nil {
			return Building{}, listErr
		}
		if len(rooms) > 0 {
			return Building{}, ErrConflict
		}
	}
	updated, err := s.store.UpdateBuilding(ctx, Building{
		ID:             existing.ID,
		OrganizationID: existing.OrganizationID,
		SiteID:         siteID,
		Name:           name,
		NormalizedName: normalizedName,
		Status:         status,
		Revision:       existing.Revision + 1,
		CreatedAt:      existing.CreatedAt,
		UpdatedAt:      s.now(),
	}, existing.Revision)
	if err != nil {
		return Building{}, err
	}
	if err := s.auditRequirement(ctx, DirectoryExpansionRequirementID, "people.building.updated", "building", updated.ID, map[string]string{"siteId": updated.SiteID}); err != nil {
		return Building{}, fmt.Errorf("audit building update: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateRoom(ctx context.Context, input CreateRoomInput) (Room, error) {
	siteID := strings.TrimSpace(input.SiteID)
	buildingID := strings.TrimSpace(input.BuildingID)
	if !recordIDPattern.MatchString(siteID) || !recordIDPattern.MatchString(buildingID) {
		return Room{}, ErrInvalidInput
	}
	building, err := s.store.GetBuilding(ctx, s.organizationID, buildingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Room{}, ErrReferenceMissing
		}
		return Room{}, err
	}
	if building.SiteID != siteID {
		return Room{}, ErrInvalidInput
	}
	number := strings.TrimSpace(input.Number)
	if number == "" || !utf8.ValidString(number) || utf8.RuneCountInString(number) > 100 {
		return Room{}, ErrInvalidInput
	}
	name := strings.TrimSpace(input.Name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 {
		return Room{}, ErrInvalidInput
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return Room{}, ErrInvalidInput
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Room{}, fmt.Errorf("create room id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.room", id); err != nil {
		return Room{}, err
	}
	created, err := s.store.CreateRoom(ctx, Room{
		ID:               id,
		OrganizationID:   s.organizationID,
		SiteID:           siteID,
		BuildingID:       buildingID,
		Number:           number,
		NormalizedNumber: strings.ToLower(number),
		Name:             name,
		Status:           status,
		Revision:         1,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return Room{}, err
	}
	if err := s.auditRequirement(ctx, DirectoryExpansionRequirementID, "people.room.created", "room", created.ID, map[string]string{
		"buildingId": created.BuildingID,
		"siteId":     created.SiteID,
	}); err != nil {
		return Room{}, fmt.Errorf("audit room creation: %w", err)
	}
	return created, nil
}

func (s *Service) ListRooms(ctx context.Context, siteID, buildingID string, visibility Visibility) ([]Room, error) {
	siteID = strings.TrimSpace(siteID)
	buildingID = strings.TrimSpace(buildingID)
	if (siteID != "" && !recordIDPattern.MatchString(siteID)) ||
		(buildingID != "" && !recordIDPattern.MatchString(buildingID)) {
		return nil, ErrInvalidInput
	}
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	return s.store.ListRooms(ctx, s.organizationID, siteID, buildingID, visibility)
}

func (s *Service) UpdateRoom(ctx context.Context, input UpdateRoomInput) (Room, error) {
	id := strings.TrimSpace(input.ID)
	buildingID := strings.TrimSpace(input.BuildingID)
	if !recordIDPattern.MatchString(id) || !recordIDPattern.MatchString(buildingID) {
		return Room{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.room", id); err != nil {
		return Room{}, err
	}
	existing, err := s.store.GetRoom(ctx, s.organizationID, id)
	if err != nil {
		return Room{}, err
	}
	if existing.Revision != input.Revision {
		return Room{}, ErrConflict
	}
	building, err := s.store.GetBuilding(ctx, s.organizationID, buildingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Room{}, ErrReferenceMissing
		}
		return Room{}, err
	}
	siteID := strings.TrimSpace(input.SiteID)
	if siteID == "" {
		siteID = building.SiteID
	}
	if building.SiteID != siteID {
		return Room{}, ErrInvalidInput
	}
	number := strings.TrimSpace(input.Number)
	if number == "" || !utf8.ValidString(number) || utf8.RuneCountInString(number) > 100 {
		return Room{}, ErrInvalidInput
	}
	name := strings.TrimSpace(input.Name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 {
		return Room{}, ErrInvalidInput
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return Room{}, ErrInvalidInput
	}
	updated, err := s.store.UpdateRoom(ctx, Room{
		ID:               existing.ID,
		OrganizationID:   existing.OrganizationID,
		SiteID:           building.SiteID,
		BuildingID:       building.ID,
		Number:           number,
		NormalizedNumber: strings.ToLower(number),
		Name:             name,
		Status:           status,
		Revision:         existing.Revision + 1,
		CreatedAt:        existing.CreatedAt,
		UpdatedAt:        s.now(),
	}, existing.Revision)
	if err != nil {
		return Room{}, err
	}
	if err := s.auditRequirement(ctx, DirectoryExpansionRequirementID, "people.room.updated", "room", updated.ID, map[string]string{
		"buildingId": updated.BuildingID,
		"siteId":     updated.SiteID,
	}); err != nil {
		return Room{}, fmt.Errorf("audit room update: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateDepartment(ctx context.Context, input CreateDepartmentInput) (Department, error) {
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Department{}, err
	}
	siteID := strings.TrimSpace(input.SiteID)
	if siteID != "" {
		if !recordIDPattern.MatchString(siteID) {
			return Department{}, ErrInvalidInput
		}
		if _, err := s.store.GetSite(ctx, s.organizationID, siteID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Department{}, ErrReferenceMissing
			}
			return Department{}, err
		}
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Department{}, fmt.Errorf("create department id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.department", id); err != nil {
		return Department{}, err
	}
	created, err := s.store.CreateDepartment(ctx, Department{
		ID:             id,
		OrganizationID: s.organizationID,
		Name:           name,
		NormalizedName: normalizedName,
		SiteID:         siteID,
		Status:         status,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return Department{}, err
	}
	metadata := map[string]string{}
	if created.SiteID != "" {
		metadata["siteId"] = created.SiteID
	}
	if err := s.audit(ctx, "people.department.created", "department", created.ID, metadata); err != nil {
		return Department{}, fmt.Errorf("audit department creation: %w", err)
	}
	return created, nil
}

func (s *Service) ListDepartments(ctx context.Context, visibility Visibility) ([]Department, error) {
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	return s.store.ListDepartments(ctx, s.organizationID, visibility)
}

func (s *Service) UpdateDepartment(ctx context.Context, input UpdateDepartmentInput) (Department, error) {
	id := strings.TrimSpace(input.ID)
	if !recordIDPattern.MatchString(id) {
		return Department{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.department", id); err != nil {
		return Department{}, err
	}
	existing, err := s.store.GetDepartment(ctx, s.organizationID, id)
	if err != nil {
		return Department{}, err
	}
	if existing.Revision != input.Revision {
		return Department{}, ErrConflict
	}
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Department{}, err
	}
	siteID := strings.TrimSpace(input.SiteID)
	if siteID != "" {
		if !recordIDPattern.MatchString(siteID) {
			return Department{}, ErrInvalidInput
		}
		if _, err := s.store.GetSite(ctx, s.organizationID, siteID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Department{}, ErrReferenceMissing
			}
			return Department{}, err
		}
	}
	updated, err := s.store.UpdateDepartment(ctx, Department{
		ID:             existing.ID,
		OrganizationID: existing.OrganizationID,
		Name:           name,
		NormalizedName: normalizedName,
		SiteID:         siteID,
		Status:         status,
		Revision:       existing.Revision + 1,
		CreatedAt:      existing.CreatedAt,
		UpdatedAt:      s.now(),
	}, existing.Revision)
	if err != nil {
		return Department{}, err
	}
	if err := s.auditRequirement(ctx, RequirementID, "people.department.updated", "department", updated.ID, nil); err != nil {
		return Department{}, fmt.Errorf("audit department update: %w", err)
	}
	return updated, nil
}

func (s *Service) CreateIdentity(ctx context.Context, input CreateIdentityInput) (Identity, error) {
	identity, err := s.prepareIdentity(ctx, input)
	if err != nil {
		return Identity{}, err
	}
	if err := s.checkWrite(ctx, "people.identity", identity.ID); err != nil {
		return Identity{}, err
	}
	created, err := s.store.CreateIdentity(ctx, identity)
	if err != nil {
		return Identity{}, err
	}
	if err := s.audit(ctx, "people.identity.created", "identity", created.ID, map[string]string{"kind": string(created.Kind)}); err != nil {
		return Identity{}, fmt.Errorf("audit identity creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateIdentity(ctx context.Context, input UpdateIdentityInput) (Identity, error) {
	id := strings.TrimSpace(input.ID)
	if !recordIDPattern.MatchString(id) {
		return Identity{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.identity", id); err != nil {
		return Identity{}, err
	}
	existing, err := s.store.GetIdentity(ctx, s.organizationID, id)
	if err != nil {
		return Identity{}, err
	}
	if existing.Revision != input.Revision {
		return Identity{}, ErrConflict
	}
	prepared, err := s.prepareIdentity(ctx, CreateIdentityInput{
		Kind:            input.Kind,
		DisplayName:     input.DisplayName,
		Email:           input.Email,
		DepartmentID:    input.DepartmentID,
		SiteID:          input.SiteID,
		BuildingID:      input.BuildingID,
		RoomID:          input.RoomID,
		Status:          input.Status,
		Provider:        existing.Provider,
		ProviderSubject: existing.ProviderSubject,
	})
	if err != nil {
		return Identity{}, err
	}
	updated, err := s.store.UpdateIdentity(ctx, Identity{
		ID:              existing.ID,
		OrganizationID:  existing.OrganizationID,
		Kind:            prepared.Kind,
		DisplayName:     prepared.DisplayName,
		NormalizedName:  prepared.NormalizedName,
		Email:           prepared.Email,
		NormalizedEmail: prepared.NormalizedEmail,
		DepartmentID:    prepared.DepartmentID,
		SiteID:          prepared.SiteID,
		BuildingID:      prepared.BuildingID,
		RoomID:          prepared.RoomID,
		Status:          prepared.Status,
		Provider:        existing.Provider,
		ProviderSubject: existing.ProviderSubject,
		Revision:        existing.Revision + 1,
		CreatedAt:       existing.CreatedAt,
		UpdatedAt:       s.now(),
	}, existing.Revision)
	if err != nil {
		return Identity{}, err
	}
	if err := s.audit(ctx, "people.identity.updated", "identity", updated.ID, map[string]string{"kind": string(updated.Kind)}); err != nil {
		return Identity{}, fmt.Errorf("audit identity update: %w", err)
	}
	return updated, nil
}

func (s *Service) SearchIdentities(ctx context.Context, query IdentityQuery, visibility Visibility) ([]Identity, error) {
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	query.Search = strings.TrimSpace(query.Search)
	if utf8.RuneCountInString(query.Search) > 200 || !validIdentityKindOrEmpty(query.Kind) || !validStatusOrEmpty(query.Status) {
		return nil, ErrInvalidInput
	}
	query.DepartmentID = strings.TrimSpace(query.DepartmentID)
	query.SiteID = strings.TrimSpace(query.SiteID)
	if (query.DepartmentID != "" && !recordIDPattern.MatchString(query.DepartmentID)) ||
		(query.SiteID != "" && !recordIDPattern.MatchString(query.SiteID)) {
		return nil, ErrInvalidInput
	}
	var err error
	if query.IDs, err = normalizedIdentityIDs(query.IDs); err != nil {
		return nil, err
	}
	if query.Limit == 0 {
		query.Limit = defaultSearchLimit
	}
	if len(query.IDs) > 0 && query.Limit < len(query.IDs) {
		query.Limit = len(query.IDs)
	}
	if query.Limit < 1 || query.Limit > maximumSearchLimit {
		return nil, ErrInvalidInput
	}
	return s.store.SearchIdentities(ctx, s.organizationID, query, visibility)
}

func (s *Service) CreateAssetAssignment(ctx context.Context, input CreateAssetAssignmentInput) (AssetAssignment, error) {
	assetID := strings.TrimSpace(input.AssetID)
	assigneeID := strings.TrimSpace(input.AssigneeID)
	if !assetIDPattern.MatchString(assetID) || !recordIDPattern.MatchString(assigneeID) || !validAssignment(input.AssigneeKind, input.Role) {
		return AssetAssignment{}, ErrInvalidInput
	}
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return AssetAssignment{}, ErrReferenceMissing
	}
	switch input.AssigneeKind {
	case AssigneeIdentity:
		identity, err := s.store.GetIdentity(ctx, s.organizationID, assigneeID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return AssetAssignment{}, ErrReferenceMissing
			}
			return AssetAssignment{}, err
		}
		if identity.Status != StatusActive {
			return AssetAssignment{}, ErrConflict
		}
	case AssigneeDepartment:
		department, err := s.store.GetDepartment(ctx, s.organizationID, assigneeID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return AssetAssignment{}, ErrReferenceMissing
			}
			return AssetAssignment{}, err
		}
		if department.Status != StatusActive {
			return AssetAssignment{}, ErrConflict
		}
	}
	effectiveFrom := input.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = s.now()
	}
	effectiveFrom = portabletime.Normalize(effectiveFrom)
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return AssetAssignment{}, fmt.Errorf("create assignment id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.assignment", id); err != nil {
		return AssetAssignment{}, err
	}
	actorID := actorFromContext(ctx)
	assignment := AssetAssignment{
		ID:             id,
		OrganizationID: s.organizationID,
		AssetID:        assetID,
		AssigneeKind:   input.AssigneeKind,
		AssigneeID:     assigneeID,
		Role:           input.Role,
		EffectiveFrom:  effectiveFrom,
		CreatedBy:      actorID,
		CreatedAt:      s.now(),
	}
	replaceActiveRole := input.Role == AssignmentPrimary || input.Role == AssignmentDepartment
	created, err := s.store.CreateAssetAssignment(ctx, assignment, replaceActiveRole)
	if err != nil {
		return AssetAssignment{}, err
	}
	if err := s.audit(ctx, "people.asset_assignment.created", "asset_assignment", created.ID, map[string]string{
		"assigneeKind": string(created.AssigneeKind),
		"role":         string(created.Role),
	}); err != nil {
		return AssetAssignment{}, fmt.Errorf("audit asset assignment: %w", err)
	}
	return created, nil
}

func (s *Service) EndAssetAssignment(ctx context.Context, input EndAssetAssignmentInput) (AssetAssignment, error) {
	assetID := strings.TrimSpace(input.AssetID)
	assignmentID := strings.TrimSpace(input.AssignmentID)
	if !assetIDPattern.MatchString(assetID) || !recordIDPattern.MatchString(assignmentID) {
		return AssetAssignment{}, ErrInvalidInput
	}
	assignment, err := s.store.GetAssetAssignment(ctx, s.organizationID, assignmentID)
	if err != nil {
		return AssetAssignment{}, err
	}
	if assignment.AssetID != assetID {
		return AssetAssignment{}, ErrNotFound
	}
	if err := s.checkWrite(ctx, "people.assignment", assignmentID); err != nil {
		return AssetAssignment{}, err
	}
	effectiveTo := input.EffectiveTo
	defaultedEffectiveTo := effectiveTo.IsZero()
	if effectiveTo.IsZero() {
		effectiveTo = s.now()
	}
	effectiveTo = portabletime.Normalize(effectiveTo)
	if defaultedEffectiveTo && !effectiveTo.After(assignment.EffectiveFrom) {
		effectiveFrom := portabletime.Normalize(assignment.EffectiveFrom)
		if !effectiveFrom.Before(maximumPortableInstant) {
			return AssetAssignment{}, ErrConflict
		}
		effectiveTo = effectiveFrom.Add(time.Microsecond)
	}
	if effectiveTo.Year() > 9999 {
		return AssetAssignment{}, ErrConflict
	}
	ended, err := s.store.EndAssetAssignment(ctx, s.organizationID, assetID, assignmentID, effectiveTo)
	if err != nil {
		return AssetAssignment{}, err
	}
	if err := s.audit(ctx, "people.asset_assignment.ended", "asset_assignment", ended.ID, nil); err != nil {
		return AssetAssignment{}, fmt.Errorf("audit asset assignment ending: %w", err)
	}
	return ended, nil
}

func (s *Service) ListAssetAssignments(ctx context.Context, assetID string, visibility Visibility) ([]AssetAssignment, error) {
	assetID = strings.TrimSpace(assetID)
	if !assetIDPattern.MatchString(assetID) {
		return nil, ErrInvalidInput
	}
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return nil, ErrReferenceMissing
	}
	assignments, err := s.store.ListAssetAssignments(ctx, s.organizationID, assetID)
	if err != nil || visibility.All {
		return assignments, err
	}
	departmentIDs := make(map[string]struct{}, len(visibility.DepartmentIDs))
	for _, id := range visibility.DepartmentIDs {
		departmentIDs[id] = struct{}{}
	}
	siteIDs := make(map[string]struct{}, len(visibility.SiteIDs))
	for _, id := range visibility.SiteIDs {
		siteIDs[id] = struct{}{}
	}
	visible := make([]AssetAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		allowed := false
		switch assignment.AssigneeKind {
		case AssigneeIdentity:
			identity, loadErr := s.store.GetIdentity(ctx, s.organizationID, assignment.AssigneeID)
			if loadErr != nil {
				if errors.Is(loadErr, ErrNotFound) {
					continue
				}
				return nil, loadErr
			}
			_, departmentAllowed := departmentIDs[identity.DepartmentID]
			_, siteAllowed := siteIDs[identity.SiteID]
			allowed = departmentAllowed || siteAllowed
		case AssigneeDepartment:
			department, loadErr := s.store.GetDepartment(ctx, s.organizationID, assignment.AssigneeID)
			if loadErr != nil {
				if errors.Is(loadErr, ErrNotFound) {
					continue
				}
				return nil, loadErr
			}
			_, departmentAllowed := departmentIDs[department.ID]
			_, siteAllowed := siteIDs[department.SiteID]
			allowed = departmentAllowed || siteAllowed
		}
		if allowed {
			visible = append(visible, assignment)
		}
	}
	return visible, nil
}

func (s *Service) prepareIdentity(ctx context.Context, input CreateIdentityInput) (Identity, error) {
	if !validIdentityKind(input.Kind) {
		return Identity{}, ErrInvalidInput
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 200 {
		return Identity{}, ErrInvalidInput
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return Identity{}, ErrInvalidInput
	}
	email, err := normalizeEmail(input.Email)
	if err != nil || (input.Kind == IdentityPerson && email == "") {
		return Identity{}, ErrInvalidInput
	}
	departmentID := strings.TrimSpace(input.DepartmentID)
	siteID := strings.TrimSpace(input.SiteID)
	buildingID := strings.TrimSpace(input.BuildingID)
	roomID := strings.TrimSpace(input.RoomID)
	if (departmentID != "" && !recordIDPattern.MatchString(departmentID)) ||
		(siteID != "" && !recordIDPattern.MatchString(siteID)) ||
		(buildingID != "" && !recordIDPattern.MatchString(buildingID)) ||
		(roomID != "" && !recordIDPattern.MatchString(roomID)) {
		return Identity{}, ErrInvalidInput
	}
	if departmentID != "" {
		department, err := s.store.GetDepartment(ctx, s.organizationID, departmentID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return Identity{}, ErrReferenceMissing
			}
			return Identity{}, err
		}
		if department.SiteID != "" {
			if siteID != "" && siteID != department.SiteID {
				return Identity{}, ErrInvalidInput
			}
			siteID = department.SiteID
		}
	}
	if roomID != "" {
		room, err := s.store.GetRoom(ctx, s.organizationID, roomID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return Identity{}, ErrReferenceMissing
			}
			return Identity{}, err
		}
		if buildingID != "" && buildingID != room.BuildingID {
			return Identity{}, ErrInvalidInput
		}
		if siteID != "" && siteID != room.SiteID {
			return Identity{}, ErrInvalidInput
		}
		buildingID = room.BuildingID
		siteID = room.SiteID
	}
	if buildingID != "" {
		building, err := s.store.GetBuilding(ctx, s.organizationID, buildingID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return Identity{}, ErrReferenceMissing
			}
			return Identity{}, err
		}
		if siteID != "" && siteID != building.SiteID {
			return Identity{}, ErrInvalidInput
		}
		siteID = building.SiteID
	}
	if siteID != "" {
		if _, err := s.store.GetSite(ctx, s.organizationID, siteID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Identity{}, ErrReferenceMissing
			}
			return Identity{}, err
		}
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	providerSubject := strings.TrimSpace(input.ProviderSubject)
	if (provider == "") != (providerSubject == "") || (provider != "" && (!providerPattern.MatchString(provider) || len(providerSubject) > 255)) {
		return Identity{}, ErrInvalidInput
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Identity{}, fmt.Errorf("create identity id: %w", err)
	}
	return Identity{
		ID:              id,
		OrganizationID:  s.organizationID,
		Kind:            input.Kind,
		DisplayName:     displayName,
		NormalizedName:  strings.ToLower(displayName),
		Email:           email,
		NormalizedEmail: email,
		DepartmentID:    departmentID,
		SiteID:          siteID,
		BuildingID:      buildingID,
		RoomID:          roomID,
		Status:          status,
		Provider:        provider,
		ProviderSubject: providerSubject,
		Revision:        1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func validateNamedRecord(value string, status RecordStatus) (string, string, RecordStatus, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 200 {
		return "", "", "", ErrInvalidInput
	}
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return "", "", "", ErrInvalidInput
	}
	return value, strings.ToLower(value), status, nil
}

func normalizeAddress(address Address) (Address, error) {
	address = Address{
		Line1:      strings.TrimSpace(address.Line1),
		Line2:      strings.TrimSpace(address.Line2),
		City:       strings.TrimSpace(address.City),
		Region:     strings.TrimSpace(address.Region),
		PostalCode: strings.TrimSpace(address.PostalCode),
		Country:    strings.ToUpper(strings.TrimSpace(address.Country)),
	}
	if address.Empty() {
		return address, nil
	}
	if address.Line1 == "" || address.City == "" || !countryCodePattern.MatchString(address.Country) ||
		!validBoundedText(address.Line1, 200) || !validBoundedText(address.Line2, 200) ||
		!validBoundedText(address.City, 100) || !validBoundedText(address.Region, 100) ||
		!validBoundedText(address.PostalCode, 32) {
		return Address{}, ErrInvalidInput
	}
	return address, nil
}

func validBoundedText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 320 {
		return "", ErrInvalidInput
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return "", ErrInvalidInput
	}
	return strings.ToLower(value), nil
}

func validIdentityKind(kind IdentityKind) bool {
	return kind == IdentityPerson || kind == IdentityShared || kind == IdentityPublic || kind == IdentityLab
}

func validIdentityKindOrEmpty(kind IdentityKind) bool {
	return kind == "" || validIdentityKind(kind)
}

func validStatus(status RecordStatus) bool {
	return status == StatusActive || status == StatusInactive
}

func validStatusOrEmpty(status RecordStatus) bool {
	return status == "" || validStatus(status)
}

func validAssignment(kind AssigneeKind, role AssignmentRole) bool {
	switch kind {
	case AssigneeIdentity:
		return role == AssignmentPrimary || role == AssignmentUser
	case AssigneeDepartment:
		return role == AssignmentDepartment
	default:
		return false
	}
}

func normalizeVisibility(visibility Visibility) Visibility {
	if visibility.All {
		return Visibility{All: true}
	}
	visibility.DepartmentIDs = uniqueNonEmpty(visibility.DepartmentIDs)
	visibility.SiteIDs = uniqueNonEmpty(visibility.SiteIDs)
	return visibility
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:people"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	return s.auditRequirement(ctx, RequirementID, action, resourceType, resourceID, metadata)
}

func (s *Service) auditRequirement(ctx context.Context, requirementID, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{
			OrganizationID: s.organizationID,
			ActorID:        actorFromContext(ctx),
			CorrelationID:  correlationID,
		}
		ctx = foundation.WithScope(ctx, scope)
	}
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["requirementId"] = requirementID
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID:             eventID,
		OrganizationID: s.organizationID,
		ActorID:        actorFromContext(ctx),
		CorrelationID:  scope.CorrelationID,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		OccurredAt:     s.now(),
		Metadata:       metadata,
	})
}

func normalizedIdentityIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > maximumSearchLimit {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !recordIDPattern.MatchString(value) {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}
