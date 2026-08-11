package threads

// Requirement: REQ-THREADS-001. Feature: goals.tags.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type ServiceConfig struct {
	OrganizationID string
	Now            func() time.Time
}

type Service struct {
	store          Store
	targets        TargetValidator
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, targets TargetValidator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	if store == nil || targets == nil || auditor == nil {
		return nil, errors.New("Threads store, target validator, and auditor are required")
	}
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if configuration.OrganizationID == "" {
		return nil, errors.New("Threads organization id is required")
	}
	if configuration.Now == nil {
		configuration.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, targets: targets, auditor: auditor, organizationID: configuration.OrganizationID, now: configuration.Now}, nil
}

func (s *Service) ListTags(ctx context.Context) ([]Tag, error) {
	return s.store.ListTags(ctx, s.organizationID)
}

func (s *Service) GetTag(ctx context.Context, id string) (Tag, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Tag{}, ErrInvalidInput
	}
	return s.store.GetTag(ctx, s.organizationID, id)
}

func (s *Service) CreateTag(ctx context.Context, input CreateTagInput) (Tag, error) {
	normalized, err := normalizeTagInput(input)
	if err != nil {
		return Tag{}, err
	}
	if normalized.ParentID != "" {
		if _, err := s.store.GetTag(ctx, s.organizationID, normalized.ParentID); err != nil {
			return Tag{}, err
		}
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Tag{}, fmt.Errorf("create tag id: %w", err)
		}
	}
	now := s.now().UTC()
	created, err := s.store.CreateTag(ctx, Tag{
		ID: id, OrganizationID: s.organizationID, Name: normalized.Name, ParentID: normalized.ParentID,
		InheritByDefault: normalized.InheritByDefault, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Tag{}, err
	}
	if err := s.audit(ctx, "threads.tag.created", "tag", created.ID, map[string]string{"parentId": created.ParentID}); err != nil {
		return Tag{}, fmt.Errorf("audit tag creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateTag(ctx context.Context, input UpdateTagInput) (Tag, error) {
	input.ID = strings.TrimSpace(input.ID)
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 {
		return Tag{}, ErrInvalidInput
	}
	normalized, err := normalizeTagInput(CreateTagInput{Name: input.Name, ParentID: input.ParentID, InheritByDefault: input.InheritByDefault})
	if err != nil {
		return Tag{}, err
	}
	existing, err := s.store.GetTag(ctx, s.organizationID, input.ID)
	if err != nil {
		return Tag{}, err
	}
	if existing.Revision != input.Revision {
		return Tag{}, ErrConflict
	}
	if normalized.ParentID == input.ID {
		return Tag{}, ErrCycle
	}
	if err := s.validateTagParent(ctx, input.ID, normalized.ParentID); err != nil {
		return Tag{}, err
	}
	existing.Name = normalized.Name
	existing.ParentID = normalized.ParentID
	existing.InheritByDefault = normalized.InheritByDefault
	existing.Revision++
	existing.UpdatedAt = s.now().UTC()
	updated, err := s.store.UpdateTag(ctx, existing, input.Revision)
	if err != nil {
		return Tag{}, err
	}
	if err := s.audit(ctx, "threads.tag.updated", "tag", updated.ID, map[string]string{"revision": fmt.Sprint(updated.Revision), "parentId": updated.ParentID}); err != nil {
		return Tag{}, fmt.Errorf("audit tag update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListGoals(ctx context.Context) ([]Goal, error) {
	return s.store.ListGoals(ctx, s.organizationID)
}

func (s *Service) GetGoal(ctx context.Context, id string) (Goal, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Goal{}, ErrInvalidInput
	}
	return s.store.GetGoal(ctx, s.organizationID, id)
}

func (s *Service) CreateGoal(ctx context.Context, input CreateGoalInput) (Goal, error) {
	normalized, err := normalizeGoalInput(input)
	if err != nil {
		return Goal{}, err
	}
	if normalized.ParentID != "" {
		if _, err := s.store.GetGoal(ctx, s.organizationID, normalized.ParentID); err != nil {
			return Goal{}, err
		}
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Goal{}, fmt.Errorf("create goal id: %w", err)
		}
	}
	now := s.now().UTC()
	created, err := s.store.CreateGoal(ctx, Goal{
		ID: id, OrganizationID: s.organizationID, Name: normalized.Name, Description: normalized.Description,
		ParentID: normalized.ParentID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Goal{}, err
	}
	if err := s.audit(ctx, "threads.goal.created", "goal", created.ID, map[string]string{"parentId": created.ParentID}); err != nil {
		return Goal{}, fmt.Errorf("audit goal creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateGoal(ctx context.Context, input UpdateGoalInput) (Goal, error) {
	input.ID = strings.TrimSpace(input.ID)
	if !stableIDPattern.MatchString(input.ID) || input.Revision < 1 {
		return Goal{}, ErrInvalidInput
	}
	normalized, err := normalizeGoalInput(CreateGoalInput{Name: input.Name, Description: input.Description, ParentID: input.ParentID})
	if err != nil {
		return Goal{}, err
	}
	existing, err := s.store.GetGoal(ctx, s.organizationID, input.ID)
	if err != nil {
		return Goal{}, err
	}
	if existing.Revision != input.Revision {
		return Goal{}, ErrConflict
	}
	if normalized.ParentID == input.ID {
		return Goal{}, ErrCycle
	}
	if err := s.validateGoalParent(ctx, input.ID, normalized.ParentID); err != nil {
		return Goal{}, err
	}
	existing.Name = normalized.Name
	existing.Description = normalized.Description
	existing.ParentID = normalized.ParentID
	existing.Revision++
	existing.UpdatedAt = s.now().UTC()
	updated, err := s.store.UpdateGoal(ctx, existing, input.Revision)
	if err != nil {
		return Goal{}, err
	}
	if err := s.audit(ctx, "threads.goal.updated", "goal", updated.ID, map[string]string{"revision": fmt.Sprint(updated.Revision), "parentId": updated.ParentID}); err != nil {
		return Goal{}, fmt.Errorf("audit goal update: %w", err)
	}
	return updated, nil
}

func (s *Service) SetTagRule(ctx context.Context, input SetTagRuleInput) (TagRule, error) {
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.TagID = strings.TrimSpace(input.TagID)
	if !validTagTarget(input.TargetType) || !stableIDPattern.MatchString(input.TargetID) || !stableIDPattern.MatchString(input.TagID) ||
		(input.Mode != RuleInclude && input.Mode != RuleSuppress) || input.Revision < 0 {
		return TagRule{}, ErrInvalidInput
	}
	if _, err := s.store.GetTag(ctx, s.organizationID, input.TagID); err != nil {
		return TagRule{}, err
	}
	if err := s.validateTarget(ctx, input.TargetType, input.TargetID); err != nil {
		return TagRule{}, err
	}
	now := s.now().UTC()
	rule := TagRule{
		OrganizationID: s.organizationID, TargetType: input.TargetType, TargetID: input.TargetID,
		TagID: input.TagID, Mode: input.Mode, Revision: input.Revision + 1,
		UpdatedBy: actorFromContext(ctx), CreatedAt: now, UpdatedAt: now,
	}
	stored, err := s.store.PutTagRule(ctx, rule, input.Revision)
	if err != nil {
		return TagRule{}, err
	}
	if err := s.audit(ctx, "threads.tag.rule.set", "tag-rule", input.TargetID+":"+input.TagID, map[string]string{
		"targetType": string(input.TargetType), "mode": string(input.Mode), "revision": fmt.Sprint(stored.Revision),
	}); err != nil {
		return TagRule{}, fmt.Errorf("audit tag rule: %w", err)
	}
	return stored, nil
}

func (s *Service) DeleteTagRule(ctx context.Context, targetType TargetType, targetID, tagID string, revision int64) error {
	targetID, tagID = strings.TrimSpace(targetID), strings.TrimSpace(tagID)
	if !validTagTarget(targetType) || !stableIDPattern.MatchString(targetID) || !stableIDPattern.MatchString(tagID) || revision < 1 {
		return ErrInvalidInput
	}
	if err := s.store.DeleteTagRule(ctx, s.organizationID, targetType, targetID, tagID, revision); err != nil {
		return err
	}
	return s.audit(ctx, "threads.tag.rule.deleted", "tag-rule", targetID+":"+tagID, map[string]string{"targetType": string(targetType)})
}

func (s *Service) EvaluateTags(ctx context.Context, targetType TargetType, targetID string) ([]EffectiveTag, error) {
	targetID = strings.TrimSpace(targetID)
	if !validTagTarget(targetType) || !stableIDPattern.MatchString(targetID) {
		return nil, ErrInvalidInput
	}
	if err := s.validateTarget(ctx, targetType, targetID); err != nil {
		return nil, err
	}
	tags, err := s.store.ListTags(ctx, s.organizationID)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.ListTagRules(ctx, s.organizationID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	return evaluateTags(tags, rules), nil
}

func (s *Service) LinkGoal(ctx context.Context, input LinkGoalInput) (GoalLink, error) {
	input.GoalID, input.TargetID = strings.TrimSpace(input.GoalID), strings.TrimSpace(input.TargetID)
	if !validGoalTarget(input.TargetType) || !stableIDPattern.MatchString(input.GoalID) || !stableIDPattern.MatchString(input.TargetID) {
		return GoalLink{}, ErrInvalidInput
	}
	if _, err := s.store.GetGoal(ctx, s.organizationID, input.GoalID); err != nil {
		return GoalLink{}, err
	}
	if err := s.targets.ValidateThreadTarget(ctx, s.organizationID, input.TargetType, input.TargetID); err != nil {
		return GoalLink{}, err
	}
	link := GoalLink{OrganizationID: s.organizationID, GoalID: input.GoalID, TargetType: input.TargetType, TargetID: input.TargetID, CreatedBy: actorFromContext(ctx), CreatedAt: s.now().UTC()}
	created, inserted, err := s.store.CreateGoalLink(ctx, link)
	if err != nil {
		return GoalLink{}, err
	}
	if inserted {
		if err := s.audit(ctx, "threads.goal.linked", "goal-link", input.TargetID+":"+input.GoalID, map[string]string{"targetType": string(input.TargetType)}); err != nil {
			return GoalLink{}, fmt.Errorf("audit goal link: %w", err)
		}
	}
	return created, nil
}

func (s *Service) ListGoalLinks(ctx context.Context, targetType TargetType, targetID string) ([]GoalLink, error) {
	targetID = strings.TrimSpace(targetID)
	if !validGoalTarget(targetType) || !stableIDPattern.MatchString(targetID) {
		return nil, ErrInvalidInput
	}
	if err := s.targets.ValidateThreadTarget(ctx, s.organizationID, targetType, targetID); err != nil {
		return nil, err
	}
	return s.store.ListGoalLinks(ctx, s.organizationID, targetType, targetID)
}

func (s *Service) UnlinkGoal(ctx context.Context, targetType TargetType, targetID, goalID string) error {
	targetID, goalID = strings.TrimSpace(targetID), strings.TrimSpace(goalID)
	if !validGoalTarget(targetType) || !stableIDPattern.MatchString(targetID) || !stableIDPattern.MatchString(goalID) {
		return ErrInvalidInput
	}
	removed, err := s.store.DeleteGoalLink(ctx, s.organizationID, targetType, targetID, goalID)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotFound
	}
	return s.audit(ctx, "threads.goal.unlinked", "goal-link", targetID+":"+goalID, map[string]string{"targetType": string(targetType)})
}

func (s *Service) validateTarget(ctx context.Context, targetType TargetType, targetID string) error {
	if targetType == TargetGoal {
		_, err := s.store.GetGoal(ctx, s.organizationID, targetID)
		return err
	}
	return s.targets.ValidateThreadTarget(ctx, s.organizationID, targetType, targetID)
}

func (s *Service) validateTagParent(ctx context.Context, tagID, parentID string) error {
	seen := map[string]struct{}{tagID: {}}
	for parentID != "" {
		if _, exists := seen[parentID]; exists {
			return ErrCycle
		}
		seen[parentID] = struct{}{}
		parent, err := s.store.GetTag(ctx, s.organizationID, parentID)
		if err != nil {
			return err
		}
		parentID = parent.ParentID
	}
	return nil
}

func (s *Service) validateGoalParent(ctx context.Context, goalID, parentID string) error {
	seen := map[string]struct{}{goalID: {}}
	for parentID != "" {
		if _, exists := seen[parentID]; exists {
			return ErrCycle
		}
		seen[parentID] = struct{}{}
		parent, err := s.store.GetGoal(ctx, s.organizationID, parentID)
		if err != nil {
			return err
		}
		parentID = parent.ParentID
	}
	return nil
}

func normalizeTagInput(input CreateTagInput) (CreateTagInput, error) {
	input.ID, input.Name, input.ParentID = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.ParentID)
	if (input.ID != "" && !stableIDPattern.MatchString(input.ID)) || !validTextRange(input.Name, 1, 100) ||
		(input.ParentID != "" && !stableIDPattern.MatchString(input.ParentID)) {
		return CreateTagInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeGoalInput(input CreateGoalInput) (CreateGoalInput, error) {
	input.ID, input.Name = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name)
	input.Description, input.ParentID = strings.TrimSpace(input.Description), strings.TrimSpace(input.ParentID)
	if (input.ID != "" && !stableIDPattern.MatchString(input.ID)) || !validTextRange(input.Name, 1, 160) ||
		!validText(input.Description, 2000) || (input.ParentID != "" && !stableIDPattern.MatchString(input.ParentID)) {
		return CreateGoalInput{}, ErrInvalidInput
	}
	return input, nil
}

func validTagTarget(value TargetType) bool {
	switch value {
	case TargetAsset, TargetPurchase, TargetContract, TargetSoftware, TargetBudget, TargetGoal:
		return true
	default:
		return false
	}
}

func validGoalTarget(value TargetType) bool {
	return value == TargetAsset || value == TargetPurchase
}

func validText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum
}

func validTextRange(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum
}

func evaluateTags(tags []Tag, rules []TagRule) []EffectiveTag {
	byID := make(map[string]Tag, len(tags))
	for _, tag := range tags {
		byID[tag.ID] = tag
	}
	byTag := make(map[string]TagRule, len(rules))
	for _, rule := range rules {
		byTag[rule.TagID] = rule
	}
	result := make(map[string]EffectiveTag)
	for _, rule := range rules {
		tag, exists := byID[rule.TagID]
		if !exists {
			continue
		}
		ruleCopy := rule
		if rule.Mode == RuleSuppress {
			result[tag.ID] = EffectiveTag{Tag: tag, State: "suppressed", Rule: &ruleCopy}
			continue
		}
		result[tag.ID] = EffectiveTag{Tag: tag, State: "explicit", Rule: &ruleCopy}
		sourceTagID := tag.ID
		parentID := tag.ParentID
		seen := map[string]struct{}{tag.ID: {}}
		for parentID != "" {
			if _, duplicate := seen[parentID]; duplicate {
				break
			}
			seen[parentID] = struct{}{}
			parent, ok := byID[parentID]
			if !ok {
				break
			}
			if direct, hasRule := byTag[parent.ID]; hasRule {
				directCopy := direct
				state := "explicit"
				if direct.Mode == RuleSuppress {
					state = "suppressed"
				}
				result[parent.ID] = EffectiveTag{Tag: parent, State: state, SourceTagID: sourceTagID, Rule: &directCopy}
			} else if parent.InheritByDefault {
				if current, exists := result[parent.ID]; !exists || current.State == "inherited" && sourceTagID < current.SourceTagID {
					result[parent.ID] = EffectiveTag{Tag: parent, State: "inherited", SourceTagID: sourceTagID}
				}
			}
			parentID = parent.ParentID
		}
	}
	items := make([]EffectiveTag, 0, len(result))
	for _, item := range result {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := strings.ToLower(items[i].Tag.Name), strings.ToLower(items[j].Tag.Name)
		if left == right {
			return items[i].Tag.ID < items[j].Tag.ID
		}
		return left < right
	})
	return items
}

func actorFromContext(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:threads"
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: correlationID}
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
		ID: eventID, OrganizationID: s.organizationID, ActorID: actorFromContext(ctx), CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata,
	})
}
