package people

// Requirement: REQ-PEOPLE-001. Feature: identity.directory.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

const (
	maximumEventSummary = 500
	maximumBulkAssets   = 200
)

func assignmentWindow(assignment AssetAssignment) (time.Time, time.Time) {
	from := assignment.EffectiveFrom
	until := maximumPortableInstant
	if assignment.EffectiveTo != nil {
		until = *assignment.EffectiveTo
	} else if assignment.DueAt != nil {
		until = *assignment.DueAt
	}
	return from, until
}

func assignmentWindowsOverlap(left, right AssetAssignment) bool {
	leftFrom, leftUntil := assignmentWindow(left)
	rightFrom, rightUntil := assignmentWindow(right)
	return leftFrom.Before(rightUntil) && rightFrom.Before(leftUntil)
}

func normalizeAssignmentPurpose(purpose AssignmentPurpose) (AssignmentPurpose, error) {
	switch purpose {
	case "", PurposeCheckout:
		return PurposeCheckout, nil
	case PurposeReservation:
		return PurposeReservation, nil
	default:
		return "", ErrInvalidInput
	}
}

func normalizeEventSummary(value string) (string, error) {
	summary := strings.TrimSpace(value)
	if summary == "" {
		return "", nil
	}
	if !utf8.ValidString(summary) || utf8.RuneCountInString(summary) > maximumEventSummary {
		return "", ErrInvalidInput
	}
	return summary, nil
}

func normalizeConflictPolicy(policy ConflictPolicy) (ConflictPolicy, error) {
	switch policy {
	case "", ConflictReplace, ConflictGroup, ConflictProceed:
		return policy, nil
	default:
		return "", ErrInvalidInput
	}
}

func (s *Service) overlappingAssignments(ctx context.Context, proposed AssetAssignment) ([]AssetAssignment, error) {
	existing, err := s.store.ListAssetAssignments(ctx, s.organizationID, proposed.AssetID)
	if err != nil {
		return nil, err
	}
	overlaps := make([]AssetAssignment, 0)
	for _, item := range existing {
		if item.ID == proposed.ID {
			continue
		}
		if assignmentWindowsOverlap(proposed, item) {
			overlaps = append(overlaps, item)
		}
	}
	return overlaps, nil
}

func splitAssignmentOverlaps(overlaps []AssetAssignment) (checkouts, reservations []AssetAssignment) {
	for _, item := range overlaps {
		purpose := item.Purpose
		if purpose == "" {
			purpose = PurposeCheckout
		}
		if purpose == PurposeReservation {
			reservations = append(reservations, item)
			continue
		}
		checkouts = append(checkouts, item)
	}
	return checkouts, reservations
}

func (s *Service) overlapDetails(ctx context.Context, assignments []AssetAssignment) ([]AssignmentOverlap, error) {
	details := make([]AssignmentOverlap, 0, len(assignments))
	for _, assignment := range assignments {
		label, err := s.assigneeLabel(ctx, assignment.AssigneeKind, assignment.AssigneeID)
		if err != nil {
			return nil, err
		}
		purpose := assignment.Purpose
		if purpose == "" {
			purpose = PurposeCheckout
		}
		details = append(details, AssignmentOverlap{
			AssetID:       assignment.AssetID,
			AssignmentID:  assignment.ID,
			AssigneeKind:  assignment.AssigneeKind,
			AssigneeID:    assignment.AssigneeID,
			AssigneeLabel: label,
			Role:          assignment.Role,
			Purpose:       purpose,
			EventSummary:  assignment.EventSummary,
			EffectiveFrom: assignment.EffectiveFrom,
			DueAt:         assignment.DueAt,
			EffectiveTo:   assignment.EffectiveTo,
		})
	}
	return details, nil
}

func (s *Service) assigneeLabel(ctx context.Context, kind AssigneeKind, id string) (string, error) {
	switch kind {
	case AssigneeIdentity:
		identity, err := s.store.GetIdentity(ctx, s.organizationID, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "Unknown identity", nil
			}
			return "", err
		}
		return identity.DisplayName, nil
	case AssigneeDepartment:
		department, err := s.store.GetDepartment(ctx, s.organizationID, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "Unknown department", nil
			}
			return "", err
		}
		return department.Name, nil
	case AssigneeGroup:
		group, err := s.store.GetCheckoutGroup(ctx, s.organizationID, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "Unknown checkout group", nil
			}
			return "", err
		}
		return group.Name, nil
	default:
		return "Unknown assignee", nil
	}
}

func (s *Service) overlapError(ctx context.Context, kind AssignmentPurpose, assignments []AssetAssignment) error {
	details, err := s.overlapDetails(ctx, assignments)
	if err != nil {
		return err
	}
	return &OverlapError{ConflictKind: string(kind), Conflicts: details}
}

func (s *Service) CreateCheckoutGroup(ctx context.Context, input CreateCheckoutGroupInput) (CheckoutGroup, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 {
		return CheckoutGroup{}, ErrInvalidInput
	}
	description := strings.TrimSpace(input.Description)
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > maximumEventSummary {
		return CheckoutGroup{}, ErrInvalidInput
	}
	memberIDs := uniqueNonEmpty(input.MemberIDs)
	if len(memberIDs) == 0 {
		return CheckoutGroup{}, ErrInvalidInput
	}
	for _, memberID := range memberIDs {
		if !recordIDPattern.MatchString(memberID) {
			return CheckoutGroup{}, ErrInvalidInput
		}
		identity, err := s.store.GetIdentity(ctx, s.organizationID, memberID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return CheckoutGroup{}, ErrReferenceMissing
			}
			return CheckoutGroup{}, err
		}
		if identity.Status != StatusActive {
			return CheckoutGroup{}, ErrConflict
		}
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return CheckoutGroup{}, fmt.Errorf("create checkout group id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.checkout_group", id); err != nil {
		return CheckoutGroup{}, err
	}
	now := s.now()
	created, err := s.store.CreateCheckoutGroup(ctx, CheckoutGroup{
		ID:             id,
		OrganizationID: s.organizationID,
		Name:           name,
		Description:    description,
		MemberIDs:      memberIDs,
		Status:         StatusActive,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return CheckoutGroup{}, err
	}
	if err := s.audit(ctx, "people.checkout_group.created", "checkout_group", created.ID, map[string]string{
		"memberCount": fmt.Sprintf("%d", len(created.MemberIDs)),
	}); err != nil {
		return CheckoutGroup{}, fmt.Errorf("audit checkout group: %w", err)
	}
	return created, nil
}

func (s *Service) ListCheckoutGroups(ctx context.Context) ([]CheckoutGroup, error) {
	return s.store.ListCheckoutGroups(ctx, s.organizationID)
}

func (s *Service) GetCheckoutGroup(ctx context.Context, id string) (CheckoutGroup, error) {
	id = strings.TrimSpace(id)
	if !recordIDPattern.MatchString(id) {
		return CheckoutGroup{}, ErrInvalidInput
	}
	return s.store.GetCheckoutGroup(ctx, s.organizationID, id)
}

func (s *Service) AddCheckoutGroupMember(ctx context.Context, groupID, identityID string) (CheckoutGroup, error) {
	groupID = strings.TrimSpace(groupID)
	identityID = strings.TrimSpace(identityID)
	if !recordIDPattern.MatchString(groupID) || !recordIDPattern.MatchString(identityID) {
		return CheckoutGroup{}, ErrInvalidInput
	}
	if _, err := s.store.GetIdentity(ctx, s.organizationID, identityID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return CheckoutGroup{}, ErrReferenceMissing
		}
		return CheckoutGroup{}, err
	}
	if err := s.checkWrite(ctx, "people.checkout_group", groupID); err != nil {
		return CheckoutGroup{}, err
	}
	updated, err := s.store.AddCheckoutGroupMember(ctx, s.organizationID, groupID, identityID)
	if err != nil {
		return CheckoutGroup{}, err
	}
	if err := s.audit(ctx, "people.checkout_group.member_added", "checkout_group", updated.ID, map[string]string{}); err != nil {
		return CheckoutGroup{}, fmt.Errorf("audit checkout group member: %w", err)
	}
	return updated, nil
}

func (s *Service) ListBulkCheckouts(ctx context.Context) ([]BulkCheckout, error) {
	return s.store.ListBulkCheckouts(ctx, s.organizationID)
}

func (s *Service) RankCheckoutCandidates(ctx context.Context, input RankCheckoutInput) ([]CheckoutCandidate, error) {
	assetIDs := uniqueNonEmpty(input.AssetIDs)
	if len(assetIDs) == 0 {
		return []CheckoutCandidate{}, nil
	}
	from := portabletime.Normalize(input.From)
	to := portabletime.Normalize(input.To)
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return nil, ErrInvalidInput
	}
	assignments, err := s.store.ListAssetAssignmentsForAssets(ctx, s.organizationID, assetIDs)
	if err != nil {
		return nil, err
	}
	byAsset := make(map[string][]AssetAssignment, len(assetIDs))
	for _, assignment := range assignments {
		byAsset[assignment.AssetID] = append(byAsset[assignment.AssetID], assignment)
	}
	preferred := make(map[string]struct{}, len(input.PreferredModelIDs))
	for _, modelID := range input.PreferredModelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID != "" {
			preferred[modelID] = struct{}{}
		}
	}
	window := AssetAssignment{EffectiveFrom: from, DueAt: &to}
	now := s.now()
	candidates := make([]CheckoutCandidate, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		asset, err := s.assets.Get(ctx, assetID)
		if err != nil {
			continue
		}
		overlaps := make([]AssetAssignment, 0)
		for _, assignment := range byAsset[assetID] {
			if assignmentWindowsOverlap(window, assignment) {
				overlaps = append(overlaps, assignment)
			}
		}
		details, err := s.overlapDetails(ctx, overlaps)
		if err != nil {
			return nil, err
		}
		rating := 100
		if _, ok := preferred[asset.ModelID]; ok {
			rating += 25
		}
		hasCurrentCheckout := false
		hasReservation := false
		for _, overlap := range overlaps {
			purpose := overlap.Purpose
			if purpose == "" {
				purpose = PurposeCheckout
			}
			overlapFrom, overlapUntil := assignmentWindow(overlap)
			if purpose == PurposeReservation {
				hasReservation = true
				continue
			}
			if !now.Before(overlapFrom) && now.Before(overlapUntil) {
				hasCurrentCheckout = true
			} else {
				hasReservation = true
			}
		}
		if hasCurrentCheckout {
			rating -= 50
		} else if hasReservation {
			rating -= 25
		}
		if rating < 1 {
			rating = 1
		}
		modelName := ""
		if asset.ModelContext != nil {
			modelName = strings.TrimSpace(asset.ModelContext.Manufacturer + " " + asset.ModelContext.Name)
		}
		candidates = append(candidates, CheckoutCandidate{
			AssetID:   asset.ID,
			Name:      asset.Name,
			Kind:      asset.Kind,
			AssetTag:  asset.AssetTag,
			ModelID:   asset.ModelID,
			ModelName: strings.TrimSpace(modelName),
			Rating:    rating,
			Overlaps:  details,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Rating != candidates[j].Rating {
			return candidates[i].Rating > candidates[j].Rating
		}
		if candidates[i].Name != candidates[j].Name {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].AssetID < candidates[j].AssetID
	})
	return candidates, nil
}

func (s *Service) CreateBulkCheckout(ctx context.Context, input CreateBulkCheckoutInput) (BulkCheckout, []AssetAssignment, error) {
	assetIDs := uniqueNonEmpty(input.AssetIDs)
	if len(assetIDs) == 0 || len(assetIDs) > maximumBulkAssets {
		return BulkCheckout{}, nil, ErrInvalidInput
	}
	purpose, err := normalizeAssignmentPurpose(input.Purpose)
	if err != nil {
		return BulkCheckout{}, nil, err
	}
	eventSummary, err := normalizeEventSummary(input.EventSummary)
	if err != nil {
		return BulkCheckout{}, nil, err
	}
	if purpose == PurposeReservation && eventSummary == "" {
		return BulkCheckout{}, nil, ErrInvalidInput
	}
	if input.AssigneeKind != AssigneeIdentity && input.AssigneeKind != AssigneeGroup {
		return BulkCheckout{}, nil, ErrInvalidInput
	}
	assigneeID := strings.TrimSpace(input.AssigneeID)
	if !recordIDPattern.MatchString(assigneeID) {
		return BulkCheckout{}, nil, ErrInvalidInput
	}
	effectiveFrom := input.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = s.now()
	}
	effectiveFrom = portabletime.Normalize(effectiveFrom)
	dueAt, err := normalizeAssignmentDueAt(effectiveFrom, input.DueAt)
	if err != nil {
		return BulkCheckout{}, nil, err
	}
	if purpose == PurposeReservation && dueAt == nil {
		return BulkCheckout{}, nil, ErrInvalidInput
	}
	id, err := foundation.NewCorrelationID()
	if err != nil {
		return BulkCheckout{}, nil, fmt.Errorf("create bulk checkout id: %w", err)
	}
	if err := s.checkWrite(ctx, "people.bulk_checkout", id); err != nil {
		return BulkCheckout{}, nil, err
	}
	item := BulkCheckout{
		ID:                id,
		OrganizationID:    s.organizationID,
		AssigneeKind:      input.AssigneeKind,
		AssigneeID:        assigneeID,
		Purpose:           purpose,
		EffectiveFrom:     effectiveFrom,
		DueAt:             dueAt,
		EventSummary:      eventSummary,
		LabelDefinitionID: strings.TrimSpace(input.LabelDefinitionID),
		LabelValue:        strings.TrimSpace(input.LabelValue),
		RequestedCount:    len(assetIDs),
		CreatedBy:         actorFromContext(ctx),
		CreatedAt:         s.now(),
	}
	created, err := s.store.CreateBulkCheckout(ctx, item)
	if err != nil {
		return BulkCheckout{}, nil, err
	}
	role := AssignmentUser
	if input.AssigneeKind == AssigneeGroup {
		role = AssignmentPrimary
	}
	assignments := make([]AssetAssignment, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		assignment, createErr := s.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
			AssetID:        assetID,
			AssigneeKind:   input.AssigneeKind,
			AssigneeID:     assigneeID,
			Role:           role,
			Purpose:        purpose,
			EventSummary:   eventSummary,
			BulkCheckoutID: created.ID,
			EffectiveFrom:  effectiveFrom,
			DueAt:          dueAt,
			ConflictPolicy: input.ConflictPolicy,
		})
		if createErr != nil {
			return BulkCheckout{}, nil, createErr
		}
		assignments = append(assignments, assignment)
	}
	if err := s.audit(ctx, "people.bulk_checkout.created", "bulk_checkout", created.ID, map[string]string{
		"assetCount": fmt.Sprintf("%d", len(assignments)),
		"purpose":    string(created.Purpose),
	}); err != nil {
		return BulkCheckout{}, nil, fmt.Errorf("audit bulk checkout: %w", err)
	}
	return created, assignments, nil
}

func (s *Service) assignAsGroupCheckout(ctx context.Context, input CreateAssetAssignmentInput, assignment AssetAssignment, checkoutOverlaps []AssetAssignment) (AssetAssignment, error) {
	memberIDs := make([]string, 0, len(checkoutOverlaps)+1)
	var existingGroup CheckoutGroup
	hasExistingGroup := false
	for _, overlap := range checkoutOverlaps {
		switch overlap.AssigneeKind {
		case AssigneeIdentity:
			memberIDs = append(memberIDs, overlap.AssigneeID)
		case AssigneeGroup:
			group, err := s.store.GetCheckoutGroup(ctx, s.organizationID, overlap.AssigneeID)
			if err != nil {
				return AssetAssignment{}, err
			}
			existingGroup = group
			hasExistingGroup = true
			memberIDs = append(memberIDs, group.MemberIDs...)
		}
	}
	if input.AssigneeKind == AssigneeIdentity {
		memberIDs = append(memberIDs, input.AssigneeID)
	}
	memberIDs = uniqueNonEmpty(memberIDs)
	if len(memberIDs) < 2 && !hasExistingGroup {
		return AssetAssignment{}, ErrInvalidInput
	}
	group := existingGroup
	if !hasExistingGroup {
		name := strings.TrimSpace(assignment.EventSummary)
		if name == "" {
			name = "Shared checkout"
		}
		created, err := s.CreateCheckoutGroup(ctx, CreateCheckoutGroupInput{Name: name, Description: assignment.EventSummary, MemberIDs: memberIDs})
		if err != nil {
			return AssetAssignment{}, err
		}
		group = created
	} else {
		for _, memberID := range memberIDs {
			already := false
			for _, existing := range group.MemberIDs {
				if existing == memberID {
					already = true
					break
				}
			}
			if already {
				continue
			}
			updated, err := s.AddCheckoutGroupMember(ctx, group.ID, memberID)
			if err != nil {
				return AssetAssignment{}, err
			}
			group = updated
		}
	}
	endAt := assignment.EffectiveFrom
	for _, overlap := range checkoutOverlaps {
		if overlap.AssigneeKind == AssigneeGroup && overlap.AssigneeID == group.ID && overlap.EffectiveTo == nil {
			return overlap, nil
		}
		if overlap.EffectiveTo != nil {
			continue
		}
		if !endAt.After(overlap.EffectiveFrom) {
			endAt = overlap.EffectiveFrom.Add(time.Microsecond)
		}
		if _, err := s.EndAssetAssignment(ctx, EndAssetAssignmentInput{
			AssetID: overlap.AssetID, AssignmentID: overlap.ID, EffectiveTo: endAt,
		}); err != nil {
			return AssetAssignment{}, err
		}
	}
	return s.CreateAssetAssignment(ctx, CreateAssetAssignmentInput{
		AssetID:        assignment.AssetID,
		AssigneeKind:   AssigneeGroup,
		AssigneeID:     group.ID,
		Role:           AssignmentPrimary,
		Purpose:        assignment.Purpose,
		EventSummary:   assignment.EventSummary,
		BulkCheckoutID: assignment.BulkCheckoutID,
		EffectiveFrom:  assignment.EffectiveFrom,
		DueAt:          assignment.DueAt,
		ConflictPolicy: ConflictProceed,
	})
}

func (s *Service) replaceOverlappingCheckouts(ctx context.Context, assignment AssetAssignment, overlaps []AssetAssignment) error {
	endAt := assignment.EffectiveFrom
	for _, overlap := range overlaps {
		if overlap.EffectiveTo != nil {
			continue
		}
		if overlap.AssigneeKind == assignment.AssigneeKind && overlap.AssigneeID == assignment.AssigneeID {
			return ErrConflict
		}
		if !endAt.After(overlap.EffectiveFrom) {
			endAt = overlap.EffectiveFrom.Add(time.Microsecond)
		}
		if _, err := s.EndAssetAssignment(ctx, EndAssetAssignmentInput{
			AssetID: overlap.AssetID, AssignmentID: overlap.ID, EffectiveTo: endAt,
		}); err != nil {
			return err
		}
	}
	return nil
}
