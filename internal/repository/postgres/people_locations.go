package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/maxlemke/stewardmesh/internal/people"
)

func (s *PeopleStore) CreateLocationReferenceType(ctx context.Context, item people.LocationReferenceType) (people.LocationReferenceType, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_location_reference_types (
			id, organization_id, name, normalized_name, description, relationship_kind, location_kind, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, name, normalized_name, description, relationship_kind, location_kind, status, revision, created_at, updated_at
	`, item.ID, item.OrganizationID, item.Name, item.NormalizedName, item.Description, item.RelationshipKind, item.LocationKind,
		item.Status, item.Revision, item.CreatedAt, item.UpdatedAt)
	created, err := scanLocationReferenceType(row)
	if err != nil {
		return people.LocationReferenceType{}, mapPeopleStoreError("create location reference type", err)
	}
	return created, nil
}

func (s *PeopleStore) GetLocationReferenceType(ctx context.Context, organizationID, id string) (people.LocationReferenceType, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, name, normalized_name, description, relationship_kind, location_kind, status, revision, created_at, updated_at
		FROM people_location_reference_types WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	item, err := scanLocationReferenceType(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.LocationReferenceType{}, people.ErrNotFound
	}
	if err != nil {
		return people.LocationReferenceType{}, fmt.Errorf("get location reference type: %w", err)
	}
	return item, nil
}

func (s *PeopleStore) UpdateLocationReferenceType(ctx context.Context, item people.LocationReferenceType, expectedRevision uint64) (people.LocationReferenceType, error) {
	if expectedRevision < 1 || item.Revision != expectedRevision+1 {
		return people.LocationReferenceType{}, people.ErrConflict
	}
	row := s.database.QueryRowContext(ctx, `
		UPDATE people_location_reference_types SET
			name = $4, normalized_name = $5, description = $6, relationship_kind = $7, location_kind = $8,
			status = $9, revision = $10, updated_at = $11
		WHERE organization_id = $1 AND id = $2 AND revision = $3
		RETURNING id, organization_id, name, normalized_name, description, relationship_kind, location_kind, status, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, expectedRevision, item.Name, item.NormalizedName, item.Description, item.RelationshipKind,
		item.LocationKind, item.Status, item.Revision, item.UpdatedAt)
	updated, err := scanLocationReferenceType(row)
	if err != nil {
		if mapped := peopleUpdateMiss(err, func() error {
			_, lookupErr := s.GetLocationReferenceType(ctx, item.OrganizationID, item.ID)
			return lookupErr
		}); mapped != nil {
			return people.LocationReferenceType{}, mapped
		}
		return people.LocationReferenceType{}, mapPeopleStoreError("update location reference type", err)
	}
	return updated, nil
}

func (s *PeopleStore) ListLocationReferenceTypes(ctx context.Context, organizationID string) ([]people.LocationReferenceType, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, organization_id, name, normalized_name, description, relationship_kind, location_kind, status, revision, created_at, updated_at
		FROM people_location_reference_types WHERE organization_id = $1 ORDER BY normalized_name, id
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list location reference types: %w", err)
	}
	defer rows.Close()
	result := make([]people.LocationReferenceType, 0)
	for rows.Next() {
		item, scanErr := scanLocationReferenceType(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan location reference type: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PeopleStore) CreateLocationReference(ctx context.Context, item people.LocationReference) (people.LocationReference, error) {
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO people_location_references (
			id, organization_id, identity_id, type_id, location_kind, location_id, priority, status, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, organization_id, identity_id, type_id, location_kind, location_id, priority, status, revision, created_at, updated_at
	`, item.ID, item.OrganizationID, item.IdentityID, item.TypeID, item.LocationKind, item.LocationID, item.Priority,
		item.Status, item.Revision, item.CreatedAt, item.UpdatedAt)
	created, err := scanLocationReference(row)
	if err != nil {
		return people.LocationReference{}, mapPeopleStoreError("create location reference", err)
	}
	return created, nil
}

func (s *PeopleStore) GetLocationReference(ctx context.Context, organizationID, id string) (people.LocationReference, error) {
	row := s.database.QueryRowContext(ctx, `
		SELECT id, organization_id, identity_id, type_id, location_kind, location_id, priority, status, revision, created_at, updated_at
		FROM people_location_references WHERE organization_id = $1 AND id = $2
	`, organizationID, id)
	item, err := scanLocationReference(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.LocationReference{}, people.ErrNotFound
	}
	if err != nil {
		return people.LocationReference{}, fmt.Errorf("get location reference: %w", err)
	}
	return item, nil
}

func (s *PeopleStore) UpdateLocationReference(ctx context.Context, item people.LocationReference, expectedRevision uint64) (people.LocationReference, error) {
	if expectedRevision < 1 || item.Revision != expectedRevision+1 {
		return people.LocationReference{}, people.ErrConflict
	}
	row := s.database.QueryRowContext(ctx, `
		UPDATE people_location_references SET
			identity_id = $4, type_id = $5, location_kind = $6, location_id = $7, priority = $8,
			status = $9, revision = $10, updated_at = $11
		WHERE organization_id = $1 AND id = $2 AND revision = $3
		RETURNING id, organization_id, identity_id, type_id, location_kind, location_id, priority, status, revision, created_at, updated_at
	`, item.OrganizationID, item.ID, expectedRevision, item.IdentityID, item.TypeID, item.LocationKind, item.LocationID,
		item.Priority, item.Status, item.Revision, item.UpdatedAt)
	updated, err := scanLocationReference(row)
	if err != nil {
		if mapped := peopleUpdateMiss(err, func() error {
			_, lookupErr := s.GetLocationReference(ctx, item.OrganizationID, item.ID)
			return lookupErr
		}); mapped != nil {
			return people.LocationReference{}, mapped
		}
		return people.LocationReference{}, mapPeopleStoreError("update location reference", err)
	}
	return updated, nil
}

func (s *PeopleStore) ListLocationReferences(ctx context.Context, organizationID string, query people.LocationReferenceQuery, visibility people.Visibility) ([]people.LocationReference, error) {
	if organizationID == "" || visibility.Empty() || query.Limit < 1 {
		return nil, people.ErrInvalidInput
	}
	sqlQuery := strings.Builder{}
	sqlQuery.WriteString(`
		SELECT r.id, r.organization_id, r.identity_id, r.type_id, r.location_kind, r.location_id, r.priority, r.status, r.revision, r.created_at, r.updated_at
		FROM people_location_references r
		JOIN people_identities i ON i.organization_id = r.organization_id AND i.id = r.identity_id
		WHERE r.organization_id = $1`)
	arguments := []any{organizationID}
	if !visibility.All {
		predicates := make([]string, 0, 2)
		if len(visibility.DepartmentIDs) > 0 {
			predicates = append(predicates, inPredicate("i.department_id", visibility.DepartmentIDs, &arguments))
		}
		if len(visibility.SiteIDs) > 0 {
			predicates = append(predicates, inPredicate("i.site_id", visibility.SiteIDs, &arguments))
		}
		if len(predicates) == 0 {
			return nil, people.ErrScopeRequired
		}
		sqlQuery.WriteString(" AND (" + strings.Join(predicates, " OR ") + ")")
	}
	if len(query.IdentityIDs) > 0 {
		sqlQuery.WriteString(" AND " + inPredicate("r.identity_id", query.IdentityIDs, &arguments))
	}
	if len(query.LocationIDs) > 0 {
		sqlQuery.WriteString(" AND " + inPredicate("r.location_id", query.LocationIDs, &arguments))
	}
	if query.TypeID != "" {
		arguments = append(arguments, query.TypeID)
		sqlQuery.WriteString(fmt.Sprintf(" AND r.type_id = $%d", len(arguments)))
	}
	if query.LocationKind != "" {
		arguments = append(arguments, query.LocationKind)
		sqlQuery.WriteString(fmt.Sprintf(" AND r.location_kind = $%d", len(arguments)))
	}
	if query.Status != "" {
		arguments = append(arguments, query.Status)
		sqlQuery.WriteString(fmt.Sprintf(" AND r.status = $%d", len(arguments)))
	}
	arguments = append(arguments, query.Limit)
	sqlQuery.WriteString(fmt.Sprintf(" ORDER BY r.updated_at DESC, r.id LIMIT $%d", len(arguments)))
	rows, err := s.database.QueryContext(ctx, sqlQuery.String(), arguments...)
	if err != nil {
		return nil, fmt.Errorf("list location references: %w", err)
	}
	defer rows.Close()
	result := make([]people.LocationReference, 0)
	for rows.Next() {
		item, scanErr := scanLocationReference(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan location reference: %w", scanErr)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanLocationReferenceType(row peopleRowScanner) (people.LocationReferenceType, error) {
	var item people.LocationReferenceType
	err := row.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.NormalizedName, &item.Description,
		&item.RelationshipKind, &item.LocationKind, &item.Status, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanLocationReference(row peopleRowScanner) (people.LocationReference, error) {
	var item people.LocationReference
	err := row.Scan(&item.ID, &item.OrganizationID, &item.IdentityID, &item.TypeID, &item.LocationKind,
		&item.LocationID, &item.Priority, &item.Status, &item.Revision, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}
