package signals

// Private Exchange import capability.
// Requirements: REQ-SIGNALS-001, REQ-EXCHANGE-001.
// Features: alerts.rules, migration.packages. GitHub: #9, #11.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

type exchangeImporter struct{ service *Service }

type activeSubscriptionTargetReferences struct{ catalog SubscriptionTargetCatalog }

func (r activeSubscriptionTargetReferences) SubscriptionTargetReferenceExists(ctx context.Context, organizationID, targetKind, targetID string) (bool, error) {
	targets, err := r.catalog.ListSubscriptionTargets(ctx, organizationID)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		if target.TargetKind == targetKind && target.TargetID == targetID {
			return true, nil
		}
	}
	return false, nil
}

func (*exchangeImporter) signalsExchangeImporter() {}

func (s *Service) OwnsExchangeImporter(candidate ExchangeImporter) bool {
	importer, ok := candidate.(*exchangeImporter)
	return ok && importer != nil && importer.service == s
}

func (s *Service) ExchangeSnapshot(ctx context.Context, maximum int) (ExchangeSnapshot, error) {
	if maximum < 1 {
		return ExchangeSnapshot{}, ErrInvalidInput
	}
	return s.store.ExchangeSnapshot(ctx, s.organizationID, maximum)
}

func (s *Service) ExchangeSubscription(ctx context.Context, id string) (Subscription, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Subscription{}, ErrInvalidInput
	}
	return s.store.GetSubscription(ctx, s.organizationID, id)
}

func (s *Service) ExchangeRule(ctx context.Context, id string) (Rule, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return Rule{}, ErrInvalidInput
	}
	return s.store.GetRule(ctx, s.organizationID, id)
}

func (s *Service) checkWrite(ctx context.Context, recordType, id string) error {
	if s.writes == nil {
		return nil
	}
	return s.writes.CheckResourceWrite(ctx, recordType, id)
}

func (i *exchangeImporter) ImportRule(ctx context.Context, operation ExchangeImportOperation, candidate Rule) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID || candidate.CreatedBy != "" {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID, candidate.CreatedBy = i.service.organizationID, "system:exchange"
	if !validExchangeRule(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetRule(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeRule(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditExchange(ctx, operation, "signals.rule.imported", "signal_rule", existing.ID, ruleExchangeAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	persisted, created, writeErr := i.service.store.ImportRule(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if writeErr != nil {
		if observed, readErr := i.service.store.GetRule(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeRule(observed, candidate) {
			result.Committed = true
			auditErr := i.service.auditExchange(ctx, operation, "signals.rule.imported", "signal_rule", observed.ID, ruleExchangeAuditMetadata(observed))
			return result, errors.Join(writeErr, auditErr)
		}
		return result, writeErr
	}
	if !sameExchangeRule(persisted, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	err = i.service.auditExchange(ctx, operation, "signals.rule.imported", "signal_rule", persisted.ID, ruleExchangeAuditMetadata(persisted))
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func (i *exchangeImporter) ImportSubscription(ctx context.Context, operation ExchangeImportOperation, candidate Subscription) (ExchangeImportResult, error) {
	operation, err := normalizeExchangeImportOperation(operation)
	if err != nil || candidate.OrganizationID != "" && candidate.OrganizationID != i.service.organizationID || candidate.CreatedBy != "" {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	candidate.OrganizationID, candidate.CreatedBy = i.service.organizationID, "system:exchange"
	if !validExchangeSubscription(candidate) {
		return ExchangeImportResult{}, ErrInvalidInput
	}
	if existing, readErr := i.service.store.GetSubscription(ctx, i.service.organizationID, candidate.ID); readErr == nil {
		if !sameExchangeSubscription(existing, candidate) {
			return ExchangeImportResult{}, ErrConflict
		}
		err = i.service.auditExchange(ctx, operation, "signals.subscription.imported", "signal_subscription", existing.ID, subscriptionExchangeAuditMetadata(existing))
		return ExchangeImportResult{Committed: true}, err
	} else if !errors.Is(readErr, ErrNotFound) {
		return ExchangeImportResult{}, readErr
	}
	if candidate.RuleID != "" {
		if _, err := i.service.store.GetRule(ctx, i.service.organizationID, candidate.RuleID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ExchangeImportResult{}, ErrReferenceMissing
			}
			return ExchangeImportResult{}, err
		}
	}
	targetFound, err := i.service.targetRefs.SubscriptionTargetReferenceExists(ctx, i.service.organizationID, candidate.TargetKind, candidate.TargetID)
	if err != nil {
		return ExchangeImportResult{}, err
	}
	if !targetFound {
		return ExchangeImportResult{}, ErrReferenceMissing
	}
	persisted, created, writeErr := i.service.store.ImportSubscription(ctx, candidate)
	result := ExchangeImportResult{Committed: persisted.ID != "", Created: created}
	if writeErr != nil {
		if observed, readErr := i.service.store.GetSubscription(ctx, i.service.organizationID, candidate.ID); readErr == nil && sameExchangeSubscription(observed, candidate) {
			result.Committed = true
			auditErr := i.service.auditExchange(ctx, operation, "signals.subscription.imported", "signal_subscription", observed.ID, subscriptionExchangeAuditMetadata(observed))
			return result, errors.Join(writeErr, auditErr)
		}
		if errors.Is(writeErr, ErrNotFound) {
			return result, ErrReferenceMissing
		}
		return result, writeErr
	}
	if !sameExchangeSubscription(persisted, candidate) {
		return ExchangeImportResult{}, ErrConflict
	}
	err = i.service.auditExchange(ctx, operation, "signals.subscription.imported", "signal_subscription", persisted.ID, subscriptionExchangeAuditMetadata(persisted))
	return ExchangeImportResult{Committed: true, Created: created}, err
}

func normalizeExchangeImportOperation(operation ExchangeImportOperation) (ExchangeImportOperation, error) {
	operation.Token = strings.TrimSpace(operation.Token)
	operation.OccurredAt = portabletime.Normalize(operation.OccurredAt)
	if !stableIDPattern.MatchString(operation.Token) || !validExchangeTime(operation.OccurredAt) {
		return ExchangeImportOperation{}, ErrInvalidInput
	}
	return operation, nil
}

func validExchangeRule(candidate Rule) bool {
	enabled := candidate.Enabled
	normalized, err := normalizeRuleInput(CreateRuleInput{
		ID: candidate.ID, Name: candidate.Name, Condition: candidate.Condition, Severity: candidate.Severity, Enabled: &enabled,
		ThresholdDays: candidate.ThresholdDays, FiscalPeriod: candidate.FiscalPeriod, Scenario: candidate.Scenario,
	})
	return err == nil && candidate.ID == normalized.ID && candidate.Name == normalized.Name && candidate.Condition == normalized.Condition &&
		candidate.Severity == normalized.Severity && candidate.Enabled == *normalized.Enabled && slices.Equal(candidate.ThresholdDays, normalized.ThresholdDays) &&
		candidate.FiscalPeriod == normalized.FiscalPeriod && candidate.Scenario == normalized.Scenario && validExchangeRevisionTimes(candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt)
}

func validExchangeSubscription(candidate Subscription) bool {
	return stableIDPattern.MatchString(candidate.ID) && optionalID(candidate.RuleID) &&
		(candidate.TargetKind == "group" || candidate.TargetKind == "webhook") && stableIDPattern.MatchString(candidate.TargetID) &&
		validExchangeRevisionTimes(candidate.Revision, candidate.CreatedAt, candidate.UpdatedAt)
}

func validExchangeRevisionTimes(revision int64, createdAt, updatedAt time.Time) bool {
	return revision > 0 && validExchangeTime(createdAt) && validExchangeTime(updatedAt) && !updatedAt.Before(createdAt) &&
		(revision != 1 || createdAt.Equal(updatedAt))
}

func validExchangeTime(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1970 && value.Year() <= 9999 && portabletime.IsCanonical(value)
}

func sameExchangeRule(left, right Rule) bool {
	return left.ID == right.ID && left.Name == right.Name && left.Condition == right.Condition && left.Severity == right.Severity &&
		left.Enabled == right.Enabled && slices.Equal(left.ThresholdDays, right.ThresholdDays) && left.FiscalPeriod == right.FiscalPeriod &&
		left.Scenario == right.Scenario && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameExchangeSubscription(left, right Subscription) bool {
	return left.ID == right.ID && left.RuleID == right.RuleID && left.TargetKind == right.TargetKind && left.TargetID == right.TargetID &&
		left.Enabled == right.Enabled && left.Revision == right.Revision && left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func ruleExchangeAuditMetadata(rule Rule) map[string]string {
	return map[string]string{"condition": string(rule.Condition), "severity": string(rule.Severity), "revision": strconv.FormatInt(rule.Revision, 10)}
}

func subscriptionExchangeAuditMetadata(subscription Subscription) map[string]string {
	return map[string]string{"targetKind": subscription.TargetKind, "ruleScoped": strconv.FormatBool(subscription.RuleID != ""), "revision": strconv.FormatInt(subscription.Revision, 10)}
}

func (s *Service) auditExchange(ctx context.Context, operation ExchangeImportOperation, action, resourceType, resourceID string, metadata map[string]string) error {
	metadata["requirementId"], metadata["featureId"] = RequirementID, FeatureID
	eventID := exchangeAuditIdentity(s.organizationID, operation.Token, action, resourceType, resourceID)
	scope := foundation.Scope{OrganizationID: s.organizationID, ActorID: "system:exchange", CorrelationID: operation.Token}
	ctx = foundation.WithScope(ctx, scope)
	return s.auditor.Record(ctx, foundation.AuditEvent{
		ID: eventID, OrganizationID: s.organizationID, ActorID: scope.ActorID, CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: operation.OccurredAt, Metadata: metadata,
	})
}

func exchangeAuditIdentity(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
