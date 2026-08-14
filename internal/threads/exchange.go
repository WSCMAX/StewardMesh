package threads

// Requirements: REQ-THREADS-001, REQ-EXCHANGE-001. Features: goals.tags, migration.packages. GitHub: #9.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

type exchangeImporter struct{ service *Service }

func (*exchangeImporter) threadsExchangeImporter() {}

type exchangeImportContextKey struct{}

type exchangeImportContext struct {
	operation ExchangeImportOperation
	revision  int64
}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

// TagRuleRecordID converts Threads' compound tag-rule key into one bounded,
// unambiguous Exchange/Guard resource identity.
func TagRuleRecordID(targetType TargetType, targetID, tagID string) string {
	return relationshipRecordID("tag-rule", string(targetType), targetID, tagID)
}

// GoalLinkRecordID converts Threads' compound goal-link key into one bounded,
// unambiguous Exchange/Guard resource identity.
func GoalLinkRecordID(targetType TargetType, targetID, goalID string) string {
	return relationshipRecordID("goal-link", string(targetType), targetID, goalID)
}

func relationshipRecordID(kind string, parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return kind + "-" + hex.EncodeToString(digest[:])
}

func (i *exchangeImporter) ImportTag(ctx context.Context, operation ExchangeImportOperation, candidate Tag) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || !validExchangeTag(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = withExchangeImport(ctx, operation, candidate.Revision)
	existing, err := i.service.GetTag(ctx, candidate.ID)
	if err == nil {
		if !sameExchangeTag(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "threads.tag.created", "tag", existing.ID, map[string]string{
			"parentId": existing.ParentID, "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.CreateTag(ctx, CreateTagInput{
		ID: candidate.ID, Name: candidate.Name, ParentID: candidate.ParentID, InheritByDefault: candidate.InheritByDefault,
	})
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.GetTag(ctx, candidate.ID); readErr == nil && sameExchangeTag(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func (i *exchangeImporter) ImportGoal(ctx context.Context, operation ExchangeImportOperation, candidate Goal) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || !validExchangeGoal(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = withExchangeImport(ctx, operation, candidate.Revision)
	existing, err := i.service.GetGoal(ctx, candidate.ID)
	if err == nil {
		if !sameExchangeGoal(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.audit(ctx, "threads.goal.created", "goal", existing.ID, map[string]string{
			"parentId": existing.ParentID, "revision": strconv.FormatInt(existing.Revision, 10),
		})
		return ExchangeImportResult{Committed: true}, err
	}
	if !errors.Is(err, ErrNotFound) {
		return ExchangeImportResult{}, err
	}
	_, err = i.service.CreateGoal(ctx, CreateGoalInput{
		ID: candidate.ID, Name: candidate.Name, Description: candidate.Description, ParentID: candidate.ParentID,
	})
	if err == nil {
		return ExchangeImportResult{Committed: true, Created: true}, nil
	}
	if observed, readErr := i.service.GetGoal(ctx, candidate.ID); readErr == nil && sameExchangeGoal(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func (i *exchangeImporter) ImportTagRule(ctx context.Context, operation ExchangeImportOperation, candidate TagRule) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || !validExchangeTagRule(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = withExchangeImport(ctx, operation, candidate.Revision)
	existing, found, err := i.service.getTagRule(ctx, candidate.TargetType, candidate.TargetID, candidate.TagID)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if found {
		if !sameExchangeTagRule(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditTagRuleImport(ctx, existing)
		return ExchangeImportResult{Committed: true}, err
	}
	if _, err := i.service.store.GetTag(ctx, i.service.organizationID, candidate.TagID); err != nil {
		return ExchangeImportResult{}, err
	}
	if err := i.service.validateTarget(ctx, candidate.TargetType, candidate.TargetID); err != nil {
		return ExchangeImportResult{}, err
	}
	created := candidate
	created.OrganizationID = i.service.organizationID
	created.UpdatedBy = "system:exchange"
	created.CreatedAt = operation.OccurredAt
	created.UpdatedAt = operation.OccurredAt
	created, err = i.service.store.CreateTagRule(ctx, created)
	if err == nil {
		err = i.service.auditTagRuleImport(ctx, created)
		if err == nil {
			return ExchangeImportResult{Committed: true, Created: true}, nil
		}
	}
	if observed, exists, readErr := i.service.getTagRule(ctx, candidate.TargetType, candidate.TargetID, candidate.TagID); readErr == nil && exists && sameExchangeTagRule(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: true}, err
	}
	return ExchangeImportResult{}, err
}

func (i *exchangeImporter) ImportGoalLink(ctx context.Context, operation ExchangeImportOperation, candidate GoalLink) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || !validExchangeGoalLink(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	ctx = withExchangeImport(ctx, operation, 1)
	existing, found, err := i.service.getGoalLink(ctx, candidate.TargetType, candidate.TargetID, candidate.GoalID)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if found {
		if !sameExchangeGoalLink(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditGoalLinkImport(ctx, existing)
		return ExchangeImportResult{Committed: true}, err
	}
	if _, err := i.service.store.GetGoal(ctx, i.service.organizationID, candidate.GoalID); err != nil {
		return ExchangeImportResult{}, err
	}
	if err := i.service.targets.ValidateThreadTarget(ctx, i.service.organizationID, candidate.TargetType, candidate.TargetID); err != nil {
		return ExchangeImportResult{}, err
	}
	created := candidate
	created.OrganizationID = i.service.organizationID
	created.CreatedBy = "system:exchange"
	created.CreatedAt = operation.OccurredAt
	created, inserted, err := i.service.store.CreateGoalLink(ctx, created)
	if err == nil && !inserted && !sameExchangeGoalLink(created, candidate) {
		err = ErrConflict
	}
	if err == nil {
		err = i.service.auditGoalLinkImport(ctx, created)
		if err == nil {
			return ExchangeImportResult{Committed: true, Created: inserted}, nil
		}
	}
	if observed, exists, readErr := i.service.getGoalLink(ctx, candidate.TargetType, candidate.TargetID, candidate.GoalID); readErr == nil && exists && sameExchangeGoalLink(observed, candidate) {
		return ExchangeImportResult{Committed: true, Created: inserted}, err
	}
	return ExchangeImportResult{}, err
}

func (s *Service) auditTagRuleImport(ctx context.Context, rule TagRule) error {
	return s.audit(ctx, "threads.tag.rule.set", "tag-rule", rule.TargetID+":"+rule.TagID, map[string]string{
		"targetType": string(rule.TargetType), "mode": string(rule.Mode), "revision": strconv.FormatInt(rule.Revision, 10),
	})
}

func (s *Service) auditGoalLinkImport(ctx context.Context, link GoalLink) error {
	return s.audit(ctx, "threads.goal.linked", "goal-link", link.TargetID+":"+link.GoalID, map[string]string{"targetType": string(link.TargetType)})
}

func (s *Service) getTagRule(ctx context.Context, targetType TargetType, targetID, tagID string) (TagRule, bool, error) {
	items, err := s.store.ListTagRules(ctx, s.organizationID, targetType, targetID)
	if err != nil {
		return TagRule{}, false, err
	}
	for _, item := range items {
		if item.TagID == tagID {
			return item, true, nil
		}
	}
	return TagRule{}, false, nil
}

func (s *Service) getGoalLink(ctx context.Context, targetType TargetType, targetID, goalID string) (GoalLink, bool, error) {
	items, err := s.store.ListGoalLinks(ctx, s.organizationID, targetType, targetID)
	if err != nil {
		return GoalLink{}, false, err
	}
	for _, item := range items {
		if item.GoalID == goalID {
			return item, true, nil
		}
	}
	return GoalLink{}, false, nil
}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = operation.OccurredAt.UTC()
	if !stableIDPattern.MatchString(operation.Token) || operation.OccurredAt.IsZero() || operation.OccurredAt.Year() < 2000 || operation.OccurredAt.Year() > 9999 {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func withExchangeImport(ctx context.Context, operation ExchangeImportOperation, revision int64) context.Context {
	return context.WithValue(ctx, exchangeImportContextKey{}, exchangeImportContext{operation: operation, revision: revision})
}

func (s *Service) creationState(ctx context.Context) (time.Time, int64) {
	if state, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); ok && state.revision > 0 {
		return state.operation.OccurredAt.UTC(), state.revision
	}
	return s.now().UTC(), 1
}

func (s *Service) checkWrite(ctx context.Context, recordType, recordID string) error {
	if _, importing := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext); importing || s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, recordID)
}

func validExchangeTag(candidate Tag) bool {
	normalized, err := normalizeTagInput(CreateTagInput{ID: candidate.ID, Name: candidate.Name, ParentID: candidate.ParentID, InheritByDefault: candidate.InheritByDefault})
	return err == nil && candidate.OrganizationID == "" && candidate.CreatedAt.IsZero() && candidate.UpdatedAt.IsZero() && candidate.Revision >= 1 &&
		normalized.ID == candidate.ID && normalized.Name == candidate.Name && normalized.ParentID == candidate.ParentID
}

func validExchangeGoal(candidate Goal) bool {
	normalized, err := normalizeGoalInput(CreateGoalInput{ID: candidate.ID, Name: candidate.Name, Description: candidate.Description, ParentID: candidate.ParentID})
	return err == nil && candidate.OrganizationID == "" && candidate.CreatedAt.IsZero() && candidate.UpdatedAt.IsZero() && candidate.Revision >= 1 &&
		normalized.ID == candidate.ID && normalized.Name == candidate.Name && normalized.Description == candidate.Description && normalized.ParentID == candidate.ParentID
}

func validExchangeTagRule(candidate TagRule) bool {
	return candidate.OrganizationID == "" && validTagTarget(candidate.TargetType) && stableIDPattern.MatchString(candidate.TargetID) &&
		stableIDPattern.MatchString(candidate.TagID) && (candidate.Mode == RuleInclude || candidate.Mode == RuleSuppress) && candidate.Revision >= 1 &&
		candidate.UpdatedBy == "" && candidate.CreatedAt.IsZero() && candidate.UpdatedAt.IsZero()
}

func validExchangeGoalLink(candidate GoalLink) bool {
	return candidate.OrganizationID == "" && validGoalTarget(candidate.TargetType) && stableIDPattern.MatchString(candidate.TargetID) &&
		stableIDPattern.MatchString(candidate.GoalID) && candidate.CreatedBy == "" && candidate.CreatedAt.IsZero()
}

func sameExchangeTag(left, right Tag) bool {
	return left.ID == right.ID && left.Name == right.Name && left.ParentID == right.ParentID &&
		left.InheritByDefault == right.InheritByDefault && left.Revision == right.Revision
}

func sameExchangeGoal(left, right Goal) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Description == right.Description &&
		left.ParentID == right.ParentID && left.Revision == right.Revision
}

func sameExchangeTagRule(left, right TagRule) bool {
	return left.TargetType == right.TargetType && left.TargetID == right.TargetID && left.TagID == right.TagID &&
		left.Mode == right.Mode && left.Revision == right.Revision
}

func sameExchangeGoalLink(left, right GoalLink) bool {
	return left.GoalID == right.GoalID && left.TargetType == right.TargetType && left.TargetID == right.TargetID
}

func (s *Service) exchangeAuditEventID(operation ExchangeImportOperation, action, resourceType, resourceID string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{s.organizationID, operation.Token, action, resourceType, resourceID}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func exchangeImportState(ctx context.Context) (exchangeImportContext, bool) {
	state, ok := ctx.Value(exchangeImportContextKey{}).(exchangeImportContext)
	return state, ok
}
