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
)

const defaultSearchLimit = 50
const maximumSearchLimit = 100

var (
	providerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	recordIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	assetIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	assets         AssetReader
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, assets AssetReader, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if store == nil || assets == nil || auditor == nil {
		return nil, errors.New("people store, asset reader, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("people organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:          store,
		assets:         assets,
		auditor:        auditor,
		organizationID: configuration.OrganizationID,
		now:            configuration.Now,
	}, nil
}

func (s *Service) CreateSite(ctx context.Context, input CreateSiteInput) (Site, error) {
	name, normalizedName, status, err := validateNamedRecord(input.Name, input.Status)
	if err != nil {
		return Site{}, err
	}
	now := s.now()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return Site{}, fmt.Errorf("create site id: %w", err)
	}
	created, err := s.store.CreateSite(ctx, Site{
		ID:             id,
		OrganizationID: s.organizationID,
		Name:           name,
		NormalizedName: normalizedName,
		Status:         status,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return Site{}, err
	}
	if err := s.audit(ctx, "people.site.created", "site", created.ID, nil); err != nil {
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

func (s *Service) CreateIdentity(ctx context.Context, input CreateIdentityInput) (Identity, error) {
	identity, err := s.prepareIdentity(ctx, input)
	if err != nil {
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
	if query.Limit == 0 {
		query.Limit = defaultSearchLimit
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
	effectiveFrom = effectiveFrom.UTC()
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return AssetAssignment{}, fmt.Errorf("create assignment id: %w", err)
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
	effectiveTo := input.EffectiveTo
	if effectiveTo.IsZero() {
		effectiveTo = s.now()
	}
	ended, err := s.store.EndAssetAssignment(ctx, s.organizationID, assetID, assignmentID, effectiveTo.UTC())
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
	if (departmentID != "" && !recordIDPattern.MatchString(departmentID)) ||
		(siteID != "" && !recordIDPattern.MatchString(siteID)) {
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
	metadata["requirementId"] = RequirementID
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
