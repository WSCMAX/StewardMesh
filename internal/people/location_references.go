package people

// Location reference types and occupancy links. Requirement: REQ-PEOPLE-001, REQ-DIRECTORY-EXPANSION-001.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

func validLocationKind(kind LocationKind) bool {
	switch kind {
	case LocationKindSite, LocationKindBuilding, LocationKindRoom:
		return true
	default:
		return false
	}
}

func validLocationPriority(priority LocationPriority) bool {
	return priority == LocationPriorityPrimary || priority == LocationPrioritySecondary
}

func validRelationshipKind(kind string) bool {
	switch kind {
	case "located_at", RelationshipUsesOffice, RelationshipTeachesIn, RelationshipAttendsClass, RelationshipResidesIn, RelationshipUsesLab:
		return true
	default:
		return false
	}
}

func (s *Service) CreateLocationReferenceType(ctx context.Context, input CreateLocationReferenceTypeInput) (LocationReferenceType, error) {
	item, err := s.prepareLocationReferenceType(input)
	if err != nil {
		return LocationReferenceType{}, err
	}
	if err := s.checkWrite(ctx, "people.location-reference-type", item.ID); err != nil {
		return LocationReferenceType{}, err
	}
	created, err := s.store.CreateLocationReferenceType(ctx, item)
	if err != nil {
		return LocationReferenceType{}, err
	}
	if err := s.audit(ctx, "people.location_reference_type.created", "location-reference-type", created.ID, map[string]string{"relationshipKind": created.RelationshipKind}); err != nil {
		return LocationReferenceType{}, fmt.Errorf("audit location reference type creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateLocationReferenceType(ctx context.Context, input UpdateLocationReferenceTypeInput) (LocationReferenceType, error) {
	id := strings.TrimSpace(input.ID)
	if !recordIDPattern.MatchString(id) {
		return LocationReferenceType{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.location-reference-type", id); err != nil {
		return LocationReferenceType{}, err
	}
	existing, err := s.store.GetLocationReferenceType(ctx, s.organizationID, id)
	if err != nil {
		return LocationReferenceType{}, err
	}
	if existing.Revision != input.Revision {
		return LocationReferenceType{}, ErrConflict
	}
	prepared, err := s.prepareLocationReferenceType(CreateLocationReferenceTypeInput{
		Name: input.Name, Description: input.Description, RelationshipKind: input.RelationshipKind,
		LocationKind: input.LocationKind, Status: input.Status,
	})
	if err != nil {
		return LocationReferenceType{}, err
	}
	updated, err := s.store.UpdateLocationReferenceType(ctx, LocationReferenceType{
		ID: existing.ID, OrganizationID: existing.OrganizationID, Name: prepared.Name, NormalizedName: prepared.NormalizedName,
		Description: prepared.Description, RelationshipKind: prepared.RelationshipKind, LocationKind: prepared.LocationKind,
		Status: prepared.Status, Revision: existing.Revision + 1, CreatedAt: existing.CreatedAt, UpdatedAt: s.now(),
	}, existing.Revision)
	if err != nil {
		return LocationReferenceType{}, err
	}
	if err := s.audit(ctx, "people.location_reference_type.updated", "location-reference-type", updated.ID, map[string]string{"relationshipKind": updated.RelationshipKind}); err != nil {
		return LocationReferenceType{}, fmt.Errorf("audit location reference type update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListLocationReferenceTypes(ctx context.Context) ([]LocationReferenceType, error) {
	return s.store.ListLocationReferenceTypes(ctx, s.organizationID)
}

func (s *Service) CreateLocationReference(ctx context.Context, input CreateLocationReferenceInput) (LocationReference, error) {
	item, err := s.prepareLocationReference(ctx, input)
	if err != nil {
		return LocationReference{}, err
	}
	if err := s.checkWrite(ctx, "people.location-reference", item.ID); err != nil {
		return LocationReference{}, err
	}
	created, err := s.store.CreateLocationReference(ctx, item)
	if err != nil {
		return LocationReference{}, err
	}
	if err := s.audit(ctx, "people.location_reference.created", "location-reference", created.ID, map[string]string{
		"typeId": created.TypeID, "locationKind": string(created.LocationKind), "priority": string(created.Priority),
	}); err != nil {
		return LocationReference{}, fmt.Errorf("audit location reference creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateLocationReference(ctx context.Context, input UpdateLocationReferenceInput) (LocationReference, error) {
	id := strings.TrimSpace(input.ID)
	if !recordIDPattern.MatchString(id) {
		return LocationReference{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "people.location-reference", id); err != nil {
		return LocationReference{}, err
	}
	existing, err := s.store.GetLocationReference(ctx, s.organizationID, id)
	if err != nil {
		return LocationReference{}, err
	}
	if existing.Revision != input.Revision {
		return LocationReference{}, ErrConflict
	}
	prepared, err := s.prepareLocationReference(ctx, CreateLocationReferenceInput{
		IdentityID: input.IdentityID, TypeID: input.TypeID, LocationKind: input.LocationKind,
		LocationID: input.LocationID, Priority: input.Priority, Status: input.Status,
	})
	if err != nil {
		return LocationReference{}, err
	}
	updated, err := s.store.UpdateLocationReference(ctx, LocationReference{
		ID: existing.ID, OrganizationID: existing.OrganizationID, IdentityID: prepared.IdentityID, TypeID: prepared.TypeID,
		LocationKind: prepared.LocationKind, LocationID: prepared.LocationID, Priority: prepared.Priority,
		Status: prepared.Status, Revision: existing.Revision + 1, CreatedAt: existing.CreatedAt, UpdatedAt: s.now(),
	}, existing.Revision)
	if err != nil {
		return LocationReference{}, err
	}
	if err := s.audit(ctx, "people.location_reference.updated", "location-reference", updated.ID, map[string]string{
		"typeId": updated.TypeID, "priority": string(updated.Priority),
	}); err != nil {
		return LocationReference{}, fmt.Errorf("audit location reference update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListLocationReferences(ctx context.Context, query LocationReferenceQuery, visibility Visibility) ([]LocationReference, error) {
	visibility = normalizeVisibility(visibility)
	if visibility.Empty() {
		return nil, ErrScopeRequired
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		query.Limit = 500
	}
	return s.store.ListLocationReferences(ctx, s.organizationID, query, visibility)
}

func (s *Service) prepareLocationReferenceType(input CreateLocationReferenceTypeInput) (LocationReferenceType, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 {
		return LocationReferenceType{}, ErrInvalidInput
	}
	description := strings.TrimSpace(input.Description)
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 500 {
		return LocationReferenceType{}, ErrInvalidInput
	}
	kind := strings.TrimSpace(input.RelationshipKind)
	if !validRelationshipKind(kind) || !validLocationKind(input.LocationKind) {
		return LocationReferenceType{}, ErrInvalidInput
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return LocationReferenceType{}, ErrInvalidInput
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return LocationReferenceType{}, fmt.Errorf("create location reference type id: %w", err)
	}
	now := s.now()
	return LocationReferenceType{
		ID: id, OrganizationID: s.organizationID, Name: name, NormalizedName: strings.ToLower(name),
		Description: description, RelationshipKind: kind, LocationKind: input.LocationKind,
		Status: status, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Service) prepareLocationReference(ctx context.Context, input CreateLocationReferenceInput) (LocationReference, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	typeID := strings.TrimSpace(input.TypeID)
	locationID := strings.TrimSpace(input.LocationID)
	if !recordIDPattern.MatchString(identityID) || !recordIDPattern.MatchString(typeID) || !recordIDPattern.MatchString(locationID) {
		return LocationReference{}, ErrInvalidInput
	}
	priority := input.Priority
	if priority == "" {
		priority = LocationPrioritySecondary
	}
	if !validLocationPriority(priority) || !validLocationKind(input.LocationKind) {
		return LocationReference{}, ErrInvalidInput
	}
	status := input.Status
	if status == "" {
		status = StatusActive
	}
	if !validStatus(status) {
		return LocationReference{}, ErrInvalidInput
	}
	if _, err := s.store.GetIdentity(ctx, s.organizationID, identityID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return LocationReference{}, ErrReferenceMissing
		}
		return LocationReference{}, err
	}
	referenceType, err := s.store.GetLocationReferenceType(ctx, s.organizationID, typeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return LocationReference{}, ErrReferenceMissing
		}
		return LocationReference{}, err
	}
	if referenceType.Status != StatusActive {
		return LocationReference{}, ErrInvalidInput
	}
	if referenceType.LocationKind != input.LocationKind {
		return LocationReference{}, ErrInvalidInput
	}
	switch input.LocationKind {
	case LocationKindSite:
		if _, err := s.store.GetSite(ctx, s.organizationID, locationID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return LocationReference{}, ErrReferenceMissing
			}
			return LocationReference{}, err
		}
	case LocationKindBuilding:
		if _, err := s.store.GetBuilding(ctx, s.organizationID, locationID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return LocationReference{}, ErrReferenceMissing
			}
			return LocationReference{}, err
		}
	case LocationKindRoom:
		if _, err := s.store.GetRoom(ctx, s.organizationID, locationID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return LocationReference{}, ErrReferenceMissing
			}
			return LocationReference{}, err
		}
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return LocationReference{}, fmt.Errorf("create location reference id: %w", err)
	}
	now := s.now()
	return LocationReference{
		ID: id, OrganizationID: s.organizationID, IdentityID: identityID, TypeID: typeID,
		LocationKind: input.LocationKind, LocationID: locationID, Priority: priority,
		Status: status, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}
