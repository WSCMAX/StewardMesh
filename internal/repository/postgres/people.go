package postgres

// Requirements: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-002, REQ-DIRECTORY-EXPANSION-008. Features: identity.directory, integrations.protocols, threads.relationships.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxlemke/stewardmesh/internal/people"
)

type PeopleStore struct {
	database *sql.DB
}

var _ people.Store = (*PeopleStore)(nil)

func NewPeopleStore(database *sql.DB) (*PeopleStore, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &PeopleStore{database: database}, nil
}

func (s *PeopleStore) CreateSite(ctx context.Context, site people.Site) (people.Site, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_sites (
			id, organization_id, name, normalized_name, address_line1, address_line2,
			address_city, address_region, address_postal_code, address_country,
			status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, organization_id, name, normalized_name, address_line1, address_line2,
		          address_city, address_region, address_postal_code, address_country,
		          status, revision, created_at, updated_at
	`, site.ID, site.OrganizationID, site.Name, site.NormalizedName, site.Address.Line1, site.Address.Line2,
		site.Address.City, site.Address.Region, site.Address.PostalCode, site.Address.Country,
		site.Status, site.Revision, site.CreatedAt, site.UpdatedAt)
	created, err := scanPeopleSite(row)
	if err != nil {
		return people.Site{}, mapPeopleStoreError("create site", err)
	}
	return created, nil
}

func (s *PeopleStore) GetSite(ctx context.Context, organizationID, id string) (people.Site, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, name, normalized_name, address_line1, address_line2,
		       address_city, address_region, address_postal_code, address_country,
		       status, revision, created_at, updated_at
		FROM people_sites
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	site, err := scanPeopleSite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Site{}, people.ErrNotFound
	}
	if err != nil {
		return people.Site{}, fmt.Errorf("get site: %w", err)
	}
	return site, nil
}

func (s *PeopleStore) ListSites(ctx context.Context, organizationID string, visibility people.Visibility) ([]people.Site, error) {
	if organizationID == "" || visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT s.id, s.organization_id, s.name, s.normalized_name, s.address_line1, s.address_line2,
		       s.address_city, s.address_region, s.address_postal_code, s.address_country,
		       s.status, s.revision, s.created_at, s.updated_at
		FROM people_sites s
		WHERE s.organization_id = $1`)
	arguments := []any{organizationID}
	if !visibility.All {
		predicates := make([]string, 0, 2)
		if len(visibility.SiteIDs) > 0 {
			predicates = append(predicates, inPredicate("s.id", visibility.SiteIDs, &arguments))
		}
		if len(visibility.DepartmentIDs) > 0 {
			departmentPredicate := inPredicate("visible_department.id", visibility.DepartmentIDs, &arguments)
			predicates = append(predicates, `EXISTS (
				SELECT 1 FROM people_departments visible_department
				WHERE visible_department.organization_id = s.organization_id
				  AND visible_department.site_id = s.id
				  AND `+departmentPredicate+`
			)`)
		}
		if len(predicates) == 0 {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + strings.Join(predicates, " OR ") + ")")
	}
	query.WriteString(" ORDER BY s.normalized_name, s.id")
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()
	result := make([]people.Site, 0)
	for rows.Next() {
		site, err := scanPeopleSite(rows)
		if err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		result = append(result, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sites: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) CreateBuilding(ctx context.Context, building people.Building) (people.Building, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_buildings (
			id, organization_id, site_id, name, normalized_name, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, organization_id, site_id, name, normalized_name, status, revision, created_at, updated_at
	`, building.ID, building.OrganizationID, building.SiteID, building.Name, building.NormalizedName,
		building.Status, building.Revision, building.CreatedAt, building.UpdatedAt)
	created, err := scanPeopleBuilding(row)
	if err != nil {
		return people.Building{}, mapPeopleStoreError("create building", err)
	}
	return created, nil
}

func (s *PeopleStore) GetBuilding(ctx context.Context, organizationID, id string) (people.Building, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, site_id, name, normalized_name, status, revision, created_at, updated_at
		FROM people_buildings
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	building, err := scanPeopleBuilding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Building{}, people.ErrNotFound
	}
	if err != nil {
		return people.Building{}, fmt.Errorf("get building: %w", err)
	}
	return building, nil
}

func (s *PeopleStore) ListBuildings(ctx context.Context, organizationID, siteID string, visibility people.Visibility) ([]people.Building, error) {
	if organizationID == "" {
		return nil, people.ErrInvalidInput
	}
	if visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT b.id, b.organization_id, b.site_id, b.name, b.normalized_name,
		       b.status, b.revision, b.created_at, b.updated_at
		FROM people_buildings b
		WHERE b.organization_id = $1`)
	arguments := []any{organizationID}
	if siteID != "" {
		arguments = append(arguments, siteID)
		query.WriteString(fmt.Sprintf(" AND b.site_id = $%d", len(arguments)))
	}
	if !visibility.All {
		predicate, ok := locationVisibilityPredicate("b.organization_id", "b.site_id", visibility, &arguments)
		if !ok {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + predicate + ")")
	}
	query.WriteString(" ORDER BY b.normalized_name, b.id")
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list buildings: %w", err)
	}
	defer rows.Close()
	result := make([]people.Building, 0)
	for rows.Next() {
		building, err := scanPeopleBuilding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan building: %w", err)
		}
		result = append(result, building)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate buildings: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) CreateRoom(ctx context.Context, room people.Room) (people.Room, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_rooms (
			id, organization_id, site_id, building_id, room_number, normalized_number,
			name, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, site_id, building_id, room_number, normalized_number,
		          name, status, revision, created_at, updated_at
	`, room.ID, room.OrganizationID, room.SiteID, room.BuildingID, room.Number, room.NormalizedNumber,
		room.Name, room.Status, room.Revision, room.CreatedAt, room.UpdatedAt)
	created, err := scanPeopleRoom(row)
	if err != nil {
		return people.Room{}, mapPeopleStoreError("create room", err)
	}
	return created, nil
}

func (s *PeopleStore) GetRoom(ctx context.Context, organizationID, id string) (people.Room, error) {
	room, err := scanPeopleRoom(s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, site_id, building_id, room_number, normalized_number,
		       name, status, revision, created_at, updated_at
		FROM people_rooms
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return people.Room{}, people.ErrNotFound
	}
	if err != nil {
		return people.Room{}, fmt.Errorf("get room: %w", err)
	}
	return room, nil
}

func (s *PeopleStore) ListRooms(ctx context.Context, organizationID, siteID, buildingID string, visibility people.Visibility) ([]people.Room, error) {
	if organizationID == "" {
		return nil, people.ErrInvalidInput
	}
	if visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT r.id, r.organization_id, r.site_id, r.building_id, r.room_number, r.normalized_number,
		       r.name, r.status, r.revision, r.created_at, r.updated_at
		FROM people_rooms r
		WHERE r.organization_id = $1`)
	arguments := []any{organizationID}
	if siteID != "" {
		arguments = append(arguments, siteID)
		query.WriteString(fmt.Sprintf(" AND r.site_id = $%d", len(arguments)))
	}
	if buildingID != "" {
		arguments = append(arguments, buildingID)
		query.WriteString(fmt.Sprintf(" AND r.building_id = $%d", len(arguments)))
	}
	if !visibility.All {
		predicate, ok := locationVisibilityPredicate("r.organization_id", "r.site_id", visibility, &arguments)
		if !ok {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + predicate + ")")
	}
	query.WriteString(" ORDER BY r.normalized_number, r.id")
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()
	result := make([]people.Room, 0)
	for rows.Next() {
		room, err := scanPeopleRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		result = append(result, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) CreateDepartment(ctx context.Context, department people.Department) (people.Department, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_departments (
			id, organization_id, name, normalized_name, site_id, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
		RETURNING id, organization_id, name, normalized_name, COALESCE(site_id, ''), status, revision, created_at, updated_at
	`, department.ID, department.OrganizationID, department.Name, department.NormalizedName, department.SiteID,
		department.Status, department.Revision, department.CreatedAt, department.UpdatedAt)
	created, err := scanPeopleDepartment(row)
	if err != nil {
		return people.Department{}, mapPeopleStoreError("create department", err)
	}
	return created, nil
}

func (s *PeopleStore) GetDepartment(ctx context.Context, organizationID, id string) (people.Department, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, name, normalized_name, COALESCE(site_id, ''), status, revision, created_at, updated_at
		FROM people_departments
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	department, err := scanPeopleDepartment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Department{}, people.ErrNotFound
	}
	if err != nil {
		return people.Department{}, fmt.Errorf("get department: %w", err)
	}
	return department, nil
}

func (s *PeopleStore) ListDepartments(ctx context.Context, organizationID string, visibility people.Visibility) ([]people.Department, error) {
	if organizationID == "" || visibility.Empty() {
		return nil, people.ErrScopeRequired
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, organization_id, name, normalized_name, COALESCE(site_id, ''), status, revision, created_at, updated_at
		FROM people_departments
		WHERE organization_id = $1`)
	arguments := []any{organizationID}
	if !visibility.All {
		predicates := make([]string, 0, 2)
		if len(visibility.DepartmentIDs) > 0 {
			predicates = append(predicates, inPredicate("id", visibility.DepartmentIDs, &arguments))
		}
		if len(visibility.SiteIDs) > 0 {
			predicates = append(predicates, inPredicate("site_id", visibility.SiteIDs, &arguments))
		}
		if len(predicates) == 0 {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + strings.Join(predicates, " OR ") + ")")
	}
	query.WriteString(" ORDER BY normalized_name, id")
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	defer rows.Close()
	result := make([]people.Department, 0)
	for rows.Next() {
		department, err := scanPeopleDepartment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		result = append(result, department)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) CreateIdentity(ctx context.Context, identity people.Identity) (people.Identity, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_identities (
			id, organization_id, kind, display_name, normalized_name, email, normalized_email,
			department_id, site_id, status, provider, provider_subject, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, $11, $12, $13, $14, $15)
		RETURNING id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		          COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		          revision, created_at, updated_at
	`, identity.ID, identity.OrganizationID, identity.Kind, identity.DisplayName, identity.NormalizedName,
		identity.Email, identity.NormalizedEmail, identity.DepartmentID, identity.SiteID, identity.Status,
		identity.Provider, identity.ProviderSubject, identity.Revision, identity.CreatedAt, identity.UpdatedAt)
	created, err := scanPeopleIdentity(row)
	if err != nil {
		return people.Identity{}, mapPeopleStoreError("create identity", err)
	}
	return created, nil
}

func (s *PeopleStore) GetIdentity(ctx context.Context, organizationID, id string) (people.Identity, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		       COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		       revision, created_at, updated_at
		FROM people_identities
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	identity, err := scanPeopleIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Identity{}, people.ErrNotFound
	}
	if err != nil {
		return people.Identity{}, fmt.Errorf("get identity: %w", err)
	}
	return identity, nil
}

func (s *PeopleStore) GetIdentityByProvider(ctx context.Context, organizationID, provider, providerSubject string) (people.Identity, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		       COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		       revision, created_at, updated_at
		FROM people_identities WHERE organization_id = $1 AND provider = $2 AND provider_subject = $3
	`, organizationID, provider, providerSubject)
	identity, err := scanPeopleIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Identity{}, people.ErrNotFound
	}
	if err != nil {
		return people.Identity{}, fmt.Errorf("get identity by provider: %w", err)
	}
	return identity, nil
}

func (s *PeopleStore) GetIdentityByEmail(ctx context.Context, organizationID, normalizedEmail string) (people.Identity, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		       COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		       revision, created_at, updated_at
		FROM people_identities WHERE organization_id = $1 AND normalized_email = $2
	`, organizationID, normalizedEmail)
	identity, err := scanPeopleIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Identity{}, people.ErrNotFound
	}
	if err != nil {
		return people.Identity{}, fmt.Errorf("get identity by email: %w", err)
	}
	return identity, nil
}

func (s *PeopleStore) ReconcileIdentity(ctx context.Context, identity people.Identity, expectedRevision uint64) (people.Identity, error) {
	if expectedRevision == 0 || identity.Revision != expectedRevision+1 {
		return people.Identity{}, people.ErrConflict
	}
	row := s.database.QueryRowContext(ctx, `
		UPDATE people_identities SET
			kind = $4, display_name = $5, normalized_name = $6, email = $7, normalized_email = $8,
			department_id = NULLIF($9, ''), site_id = NULLIF($10, ''), status = $11,
			revision = $12, updated_at = $13
		WHERE organization_id = $1 AND id = $2 AND revision = $3 AND provider = $14 AND provider_subject = $15 AND created_at = $16
		RETURNING id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		          COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		          revision, created_at, updated_at
	`, identity.OrganizationID, identity.ID, expectedRevision, identity.Kind, identity.DisplayName, identity.NormalizedName,
		identity.Email, identity.NormalizedEmail, identity.DepartmentID, identity.SiteID, identity.Status,
		identity.Revision, identity.UpdatedAt, identity.Provider, identity.ProviderSubject, identity.CreatedAt)
	updated, err := scanPeopleIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Identity{}, people.ErrConflict
	}
	if err != nil {
		return people.Identity{}, mapPeopleStoreError("reconcile identity", err)
	}
	return updated, nil
}

func (s *PeopleStore) DeleteIdentity(ctx context.Context, organizationID, id string, expectedRevision uint64) error {
	result, err := s.database.ExecContext(ctx, `DELETE FROM people_identities WHERE organization_id=$1 AND id=$2 AND revision=$3`, organizationID, id, expectedRevision)
	if err != nil {
		return mapPeopleStoreError("delete identity", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect identity deletion: %w", err)
	}
	if rows == 0 {
		return people.ErrConflict
	}
	return nil
}

func (s *PeopleStore) SearchIdentities(ctx context.Context, organizationID string, filter people.IdentityQuery, visibility people.Visibility) ([]people.Identity, error) {
	if organizationID == "" || visibility.Empty() || filter.Limit < 1 || filter.Limit > 100 {
		return nil, people.ErrInvalidInput
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		       COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		       revision, created_at, updated_at
		FROM people_identities
		WHERE organization_id = $1`)
	arguments := []any{organizationID}
	if !visibility.All {
		predicates := make([]string, 0, 2)
		if len(visibility.DepartmentIDs) > 0 {
			predicates = append(predicates, inPredicate("department_id", visibility.DepartmentIDs, &arguments))
		}
		if len(visibility.SiteIDs) > 0 {
			predicates = append(predicates, inPredicate("site_id", visibility.SiteIDs, &arguments))
		}
		if len(predicates) == 0 {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + strings.Join(predicates, " OR ") + ")")
	}
	if filter.Search != "" {
		arguments = append(arguments, escapeLike(strings.ToLower(filter.Search)))
		placeholder := fmt.Sprintf("$%d", len(arguments))
		query.WriteString(" AND (normalized_name LIKE '%' || " + placeholder + " || '%' ESCAPE '\\' OR normalized_email LIKE '%' || " + placeholder + " || '%' ESCAPE '\\')")
	}
	if filter.Kind != "" {
		arguments = append(arguments, filter.Kind)
		query.WriteString(fmt.Sprintf(" AND kind = $%d", len(arguments)))
	}
	if filter.Status != "" {
		arguments = append(arguments, filter.Status)
		query.WriteString(fmt.Sprintf(" AND status = $%d", len(arguments)))
	}
	if filter.DepartmentID != "" {
		arguments = append(arguments, filter.DepartmentID)
		query.WriteString(fmt.Sprintf(" AND department_id = $%d", len(arguments)))
	}
	if filter.SiteID != "" {
		arguments = append(arguments, filter.SiteID)
		query.WriteString(fmt.Sprintf(" AND site_id = $%d", len(arguments)))
	}
	arguments = append(arguments, filter.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY normalized_name, id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("search identities: %w", err)
	}
	defer rows.Close()
	result := make([]people.Identity, 0)
	for rows.Next() {
		identity, err := scanPeopleIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		result = append(result, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identities: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) ListGraphIdentities(ctx context.Context, organizationID string, filter people.GraphIdentityQuery, visibility people.Visibility) ([]people.Identity, error) {
	if organizationID == "" || visibility.Empty() || !filter.Valid() {
		return nil, people.ErrInvalidInput
	}
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, organization_id, kind, display_name, normalized_name, email, normalized_email,
		       COALESCE(department_id, ''), COALESCE(site_id, ''), status, provider, provider_subject,
		       revision, created_at, updated_at
		FROM people_identities
		WHERE organization_id = $1 AND status = 'active'`)
	arguments := []any{organizationID}
	if !visibility.All {
		predicates := make([]string, 0, 2)
		if len(visibility.DepartmentIDs) > 0 {
			predicates = append(predicates, inPredicate("department_id", visibility.DepartmentIDs, &arguments))
		}
		if len(visibility.SiteIDs) > 0 {
			predicates = append(predicates, inPredicate("site_id", visibility.SiteIDs, &arguments))
		}
		if len(predicates) == 0 {
			return nil, people.ErrScopeRequired
		}
		query.WriteString(" AND (" + strings.Join(predicates, " OR ") + ")")
	}
	if filter.LabelSearch != "" {
		arguments = append(arguments, strings.ToLower(filter.LabelSearch))
		query.WriteString(fmt.Sprintf(" AND strpos(lower(display_name), $%d) > 0", len(arguments)))
	}
	if filter.Kind != "" {
		arguments = append(arguments, filter.Kind)
		query.WriteString(fmt.Sprintf(" AND kind = $%d", len(arguments)))
	}
	selectors := make([]string, 0, 3)
	if len(filter.IdentityIDs) > 0 {
		selectors = append(selectors, inPredicate("id", filter.IdentityIDs, &arguments))
	}
	if len(filter.DepartmentIDs) > 0 {
		selectors = append(selectors, inPredicate("department_id", filter.DepartmentIDs, &arguments))
	}
	if len(filter.SiteIDs) > 0 {
		selectors = append(selectors, inPredicate("site_id", filter.SiteIDs, &arguments))
	}
	if len(selectors) > 0 {
		query.WriteString(" AND (" + strings.Join(selectors, " OR ") + ")")
	}
	arguments = append(arguments, filter.Limit)
	query.WriteString(fmt.Sprintf(" ORDER BY lower(display_name), id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, query.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list graph identities: %w", err)
	}
	defer rows.Close()
	result := make([]people.Identity, 0)
	for rows.Next() {
		identity, err := scanPeopleIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan graph identity: %w", err)
		}
		result = append(result, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate graph identities: %w", err)
	}
	return result, nil
}

func (s *PeopleStore) CreateAssetAssignment(ctx context.Context, assignment people.AssetAssignment, replaceActiveRole bool) (people.AssetAssignment, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return people.AssetAssignment{}, fmt.Errorf("begin asset assignment: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, hashtextextended($2, 0)))", assignment.OrganizationID, assignment.AssetID); err != nil {
		return people.AssetAssignment{}, fmt.Errorf("lock asset assignments: %w", err)
	}
	if err := verifyPeopleAssignee(ctx, transaction, assignment); err != nil {
		return people.AssetAssignment{}, err
	}
	if replaceActiveRole {
		var activeID string
		var activeFrom time.Time
		err := transaction.QueryRowContext(ctx, `
			SELECT id, effective_from
			FROM people_asset_assignments
			WHERE organization_id = $1 AND asset_id = $2 AND role = $3 AND effective_to IS NULL
			FOR UPDATE
		`, assignment.OrganizationID, assignment.AssetID, assignment.Role).Scan(&activeID, &activeFrom)
		switch {
		case err == nil:
			if !assignment.EffectiveFrom.After(activeFrom) {
				return people.AssetAssignment{}, people.ErrConflict
			}
			if _, err := transaction.ExecContext(ctx, `
				UPDATE people_asset_assignments SET effective_to = $2 WHERE id = $1
			`, activeID, assignment.EffectiveFrom); err != nil {
				return people.AssetAssignment{}, fmt.Errorf("close active assignment: %w", err)
			}
		case !errors.Is(err, sql.ErrNoRows):
			return people.AssetAssignment{}, fmt.Errorf("read active assignment: %w", err)
		}
	}
	var identityID any
	var departmentID any
	if assignment.AssigneeKind == people.AssigneeIdentity {
		identityID = assignment.AssigneeID
	} else {
		departmentID = assignment.AssigneeID
	}
	row := transaction.QueryRowContext(ctx, `
		INSERT INTO people_asset_assignments (
			id, organization_id, asset_id, assignee_kind, identity_id, department_id,
			role, effective_from, effective_to, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9, $10)
		RETURNING id, organization_id, asset_id, assignee_kind, COALESCE(identity_id, department_id),
		          role, effective_from, effective_to, created_by, created_at
	`, assignment.ID, assignment.OrganizationID, assignment.AssetID, assignment.AssigneeKind,
		identityID, departmentID, assignment.Role, assignment.EffectiveFrom, assignment.CreatedBy, assignment.CreatedAt)
	created, err := scanPeopleAssignment(row)
	if err != nil {
		return people.AssetAssignment{}, mapPeopleStoreError("create asset assignment", err)
	}
	if err := transaction.Commit(); err != nil {
		return people.AssetAssignment{}, fmt.Errorf("commit asset assignment: %w", err)
	}
	return created, nil
}

func (s *PeopleStore) EndAssetAssignment(ctx context.Context, organizationID, assetID, assignmentID string, effectiveTo time.Time) (people.AssetAssignment, error) {
	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return people.AssetAssignment{}, fmt.Errorf("begin ending asset assignment: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, hashtextextended($2, 0)))", organizationID, assetID); err != nil {
		return people.AssetAssignment{}, fmt.Errorf("lock asset assignments: %w", err)
	}
	row := transaction.QueryRowContext(ctx, `
		UPDATE people_asset_assignments
		SET effective_to = $4
		WHERE organization_id = $1 AND asset_id = $2 AND id = $3
		  AND effective_to IS NULL AND effective_from < $4
		RETURNING id, organization_id, asset_id, assignee_kind, COALESCE(identity_id, department_id),
		          role, effective_from, effective_to, created_by, created_at
	`, organizationID, assetID, assignmentID, effectiveTo)
	ended, err := scanPeopleAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if checkErr := transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM people_asset_assignments
				WHERE organization_id = $1 AND asset_id = $2 AND id = $3
			)
		`, organizationID, assetID, assignmentID).Scan(&exists); checkErr != nil {
			return people.AssetAssignment{}, fmt.Errorf("verify asset assignment: %w", checkErr)
		}
		if exists {
			return people.AssetAssignment{}, people.ErrConflict
		}
		return people.AssetAssignment{}, people.ErrNotFound
	}
	if err != nil {
		return people.AssetAssignment{}, fmt.Errorf("end asset assignment: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return people.AssetAssignment{}, fmt.Errorf("commit ended asset assignment: %w", err)
	}
	return ended, nil
}

func (s *PeopleStore) ListAssetAssignments(ctx context.Context, organizationID, assetID string) ([]people.AssetAssignment, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, asset_id, assignee_kind, COALESCE(identity_id, department_id),
		       role, effective_from, effective_to, created_by, created_at
		FROM people_asset_assignments
		WHERE organization_id = $1 AND asset_id = $2
		ORDER BY effective_from DESC, id DESC
	`, organizationID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list asset assignments: %w", err)
	}
	defer rows.Close()
	result := make([]people.AssetAssignment, 0)
	for rows.Next() {
		assignment, err := scanPeopleAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset assignment: %w", err)
		}
		result = append(result, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset assignments: %w", err)
	}
	return result, nil
}

func verifyPeopleAssignee(ctx context.Context, transaction *sql.Tx, assignment people.AssetAssignment) error {
	var exists bool
	var err error
	switch assignment.AssigneeKind {
	case people.AssigneeIdentity:
		err = transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM people_identities WHERE organization_id = $1 AND id = $2
			)
		`, assignment.OrganizationID, assignment.AssigneeID).Scan(&exists)
	case people.AssigneeDepartment:
		err = transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM people_departments WHERE organization_id = $1 AND id = $2
			)
		`, assignment.OrganizationID, assignment.AssigneeID).Scan(&exists)
	default:
		return people.ErrInvalidInput
	}
	if err != nil {
		return fmt.Errorf("verify assignment reference: %w", err)
	}
	if !exists {
		return people.ErrReferenceMissing
	}
	return nil
}

type peopleRowScanner interface {
	Scan(dest ...any) error
}

func scanPeopleSite(row peopleRowScanner) (people.Site, error) {
	var site people.Site
	err := row.Scan(&site.ID, &site.OrganizationID, &site.Name, &site.NormalizedName,
		&site.Address.Line1, &site.Address.Line2, &site.Address.City, &site.Address.Region,
		&site.Address.PostalCode, &site.Address.Country, &site.Status, &site.Revision,
		&site.CreatedAt, &site.UpdatedAt)
	return site, err
}

func scanPeopleBuilding(row peopleRowScanner) (people.Building, error) {
	var building people.Building
	err := row.Scan(&building.ID, &building.OrganizationID, &building.SiteID, &building.Name,
		&building.NormalizedName, &building.Status, &building.Revision, &building.CreatedAt, &building.UpdatedAt)
	return building, err
}

func scanPeopleRoom(row peopleRowScanner) (people.Room, error) {
	var room people.Room
	err := row.Scan(&room.ID, &room.OrganizationID, &room.SiteID, &room.BuildingID, &room.Number,
		&room.NormalizedNumber, &room.Name, &room.Status, &room.Revision, &room.CreatedAt, &room.UpdatedAt)
	return room, err
}

func scanPeopleDepartment(row peopleRowScanner) (people.Department, error) {
	var department people.Department
	err := row.Scan(&department.ID, &department.OrganizationID, &department.Name, &department.NormalizedName,
		&department.SiteID, &department.Status, &department.Revision, &department.CreatedAt, &department.UpdatedAt)
	return department, err
}

func scanPeopleIdentity(row peopleRowScanner) (people.Identity, error) {
	var identity people.Identity
	err := row.Scan(&identity.ID, &identity.OrganizationID, &identity.Kind, &identity.DisplayName, &identity.NormalizedName,
		&identity.Email, &identity.NormalizedEmail, &identity.DepartmentID, &identity.SiteID, &identity.Status,
		&identity.Provider, &identity.ProviderSubject, &identity.Revision, &identity.CreatedAt, &identity.UpdatedAt)
	return identity, err
}

func scanPeopleAssignment(row peopleRowScanner) (people.AssetAssignment, error) {
	var assignment people.AssetAssignment
	var effectiveTo sql.NullTime
	err := row.Scan(&assignment.ID, &assignment.OrganizationID, &assignment.AssetID, &assignment.AssigneeKind,
		&assignment.AssigneeID, &assignment.Role, &assignment.EffectiveFrom, &effectiveTo,
		&assignment.CreatedBy, &assignment.CreatedAt)
	if effectiveTo.Valid {
		assignment.EffectiveTo = &effectiveTo.Time
	}
	return assignment, err
}

func inPredicate(column string, values []string, arguments *[]any) string {
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		*arguments = append(*arguments, value)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(*arguments)))
	}
	return column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func locationVisibilityPredicate(organizationColumn, siteColumn string, visibility people.Visibility, arguments *[]any) (string, bool) {
	predicates := make([]string, 0, 2)
	if len(visibility.SiteIDs) > 0 {
		predicates = append(predicates, inPredicate(siteColumn, visibility.SiteIDs, arguments))
	}
	if len(visibility.DepartmentIDs) > 0 {
		departmentPredicate := inPredicate("visible_location_department.id", visibility.DepartmentIDs, arguments)
		predicates = append(predicates, `EXISTS (
			SELECT 1 FROM people_departments visible_location_department
			WHERE visible_location_department.organization_id = `+organizationColumn+`
			  AND visible_location_department.site_id = `+siteColumn+`
			  AND `+departmentPredicate+`
		)`)
	}
	return strings.Join(predicates, " OR "), len(predicates) > 0
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func mapPeopleStoreError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return people.ErrConflict
		case "23503":
			return people.ErrReferenceMissing
		case "23514", "23502", "22001":
			return people.ErrInvalidInput
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
