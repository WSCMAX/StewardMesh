package signals

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/portabletime"
)

var (
	stableIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	fiscalPeriodPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$`)
	scenarioPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	conditions          = map[Condition]bool{ConditionOverBudget: true, ConditionForecastOverBudget: true, ConditionUnpaid: true, ConditionOverdue: true, ConditionExpiration: true, ConditionRenewal: true, ConditionUnusedCommitment: true, ConditionReconciliation: true}
	severities          = map[Severity]bool{SeverityInfo: true, SeverityWarning: true, SeverityCritical: true}
	statuses            = map[AlertStatus]bool{StatusActive: true, StatusAcknowledged: true, StatusResolved: true}
)

type ServiceConfig struct {
	OrganizationID               string
	SubscriptionTargets          SubscriptionTargetCatalog
	SubscriptionTargetReferences SubscriptionTargetReferenceCatalog
	Now                          func() time.Time
}

type Service struct {
	store          Store
	evaluator      Evaluator
	targets        SubscriptionTargetCatalog
	targetRefs     SubscriptionTargetReferenceCatalog
	writes         WriteGate
	auditor        foundation.Auditor
	organizationID string
	now            func() time.Time
}

func NewService(store Store, evaluator Evaluator, auditor foundation.Auditor, configuration ServiceConfig) (*Service, error) {
	service, _, err := NewServiceWithExchangeImporter(store, evaluator, nil, auditor, configuration)
	return service, err
}

func NewServiceWithExchangeImporter(store Store, evaluator Evaluator, writes WriteGate, auditor foundation.Auditor, configuration ServiceConfig) (*Service, ExchangeImporter, error) {
	configuration.OrganizationID = strings.TrimSpace(configuration.OrganizationID)
	if store == nil || evaluator == nil || configuration.SubscriptionTargets == nil || auditor == nil || configuration.OrganizationID == "" {
		return nil, nil, errors.New("Signals store, evaluator, subscription targets, auditor, and organization id are required")
	}
	clock := configuration.Now
	if clock == nil {
		clock = time.Now
	}
	if configuration.SubscriptionTargetReferences == nil {
		configuration.SubscriptionTargetReferences = activeSubscriptionTargetReferences{catalog: configuration.SubscriptionTargets}
	}
	service := &Service{store: store, evaluator: evaluator, targets: configuration.SubscriptionTargets, targetRefs: configuration.SubscriptionTargetReferences, writes: writes, auditor: auditor, organizationID: configuration.OrganizationID,
		now: func() time.Time { return portabletime.Normalize(clock()) }}
	return service, &exchangeImporter{service: service}, nil
}

func (s *Service) ListRules(ctx context.Context) ([]Rule, error) {
	return s.store.ListRules(ctx, s.organizationID)
}

func (s *Service) CreateRule(ctx context.Context, input CreateRuleInput) (Rule, error) {
	normalized, err := normalizeRuleInput(input)
	if err != nil {
		return Rule{}, err
	}
	id := normalized.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Rule{}, fmt.Errorf("create Signals rule id: %w", err)
		}
	}
	if err := s.checkWrite(ctx, "signals.rule", id); err != nil {
		return Rule{}, err
	}
	now := s.now().UTC()
	enabled := true
	if normalized.Enabled != nil {
		enabled = *normalized.Enabled
	}
	rule := Rule{ID: id, OrganizationID: s.organizationID, Name: normalized.Name, Condition: normalized.Condition, Severity: normalized.Severity,
		Enabled: enabled, ThresholdDays: normalized.ThresholdDays, FiscalPeriod: normalized.FiscalPeriod, Scenario: normalized.Scenario,
		CreatedBy: actor(ctx), Revision: 1, CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateRule(ctx, rule)
	if err != nil {
		return Rule{}, err
	}
	if err := s.audit(ctx, "signals.rule.created", "signal_rule", created.ID, map[string]string{"condition": string(created.Condition), "severity": string(created.Severity)}); err != nil {
		return Rule{}, fmt.Errorf("audit Signals rule creation: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateRule(ctx context.Context, id string, input UpdateRuleInput) (Rule, error) {
	id = strings.TrimSpace(id)
	enabled := input.Enabled
	normalized, err := normalizeRuleInput(CreateRuleInput{Name: input.Name, Condition: input.Condition, Severity: input.Severity, Enabled: &enabled,
		ThresholdDays: input.ThresholdDays, FiscalPeriod: input.FiscalPeriod, Scenario: input.Scenario})
	if err != nil || !stableIDPattern.MatchString(id) || input.Revision < 1 {
		return Rule{}, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "signals.rule", id); err != nil {
		return Rule{}, err
	}
	existing, err := s.store.GetRule(ctx, s.organizationID, id)
	if err != nil {
		return Rule{}, err
	}
	updated := existing
	updated.Name, updated.Condition, updated.Severity, updated.Enabled = normalized.Name, normalized.Condition, normalized.Severity, enabled
	updated.ThresholdDays, updated.FiscalPeriod, updated.Scenario = normalized.ThresholdDays, normalized.FiscalPeriod, normalized.Scenario
	updated.Revision, updated.UpdatedAt = existing.Revision+1, portabletime.Max(s.now(), existing.UpdatedAt)
	updated, err = s.store.UpdateRule(ctx, updated, input.Revision)
	if err != nil {
		return Rule{}, err
	}
	if err := s.audit(ctx, "signals.rule.updated", "signal_rule", updated.ID, map[string]string{"condition": string(updated.Condition), "severity": string(updated.Severity), "enabled": strconv.FormatBool(updated.Enabled)}); err != nil {
		return Rule{}, fmt.Errorf("audit Signals rule update: %w", err)
	}
	return updated, nil
}

func (s *Service) ListAlerts(ctx context.Context, query AlertQuery) ([]Alert, error) {
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit < 1 || query.Limit > MaximumAlerts || !optionalID(query.RuleID) || query.Status != "" && !statuses[query.Status] || query.Severity != "" && !severities[query.Severity] || query.Condition != "" && !conditions[query.Condition] {
		return nil, ErrInvalidInput
	}
	return s.store.ListAlerts(ctx, s.organizationID, query)
}

func (s *Service) GetAlert(ctx context.Context, id string) (Alert, error) {
	if !stableIDPattern.MatchString(strings.TrimSpace(id)) {
		return Alert{}, ErrInvalidInput
	}
	return s.store.GetAlert(ctx, s.organizationID, strings.TrimSpace(id))
}

func (s *Service) ListAlertHistory(ctx context.Context, alertID string) ([]AlertHistory, error) {
	if !stableIDPattern.MatchString(strings.TrimSpace(alertID)) {
		return nil, ErrInvalidInput
	}
	return s.store.ListAlertHistory(ctx, s.organizationID, strings.TrimSpace(alertID))
}

func (s *Service) Evaluate(ctx context.Context, asOf time.Time) (EvaluationResult, error) {
	if asOf.IsZero() {
		asOf = s.now()
	}
	asOf = asOf.UTC()
	if asOf.Year() < 1970 || asOf.Year() > 9999 {
		return EvaluationResult{}, ErrInvalidInput
	}
	rules, err := s.store.ListRules(ctx, s.organizationID)
	if err != nil {
		return EvaluationResult{}, err
	}
	if len(rules) > MaximumRules {
		return EvaluationResult{}, ErrConflict
	}
	result := EvaluationResult{AsOf: asOf}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		result.Rules++
		candidates, err := s.evaluator.Evaluate(ctx, rule, asOf)
		if err != nil {
			return result, fmt.Errorf("evaluate %s rule: %w", rule.Condition, err)
		}
		if len(candidates) > MaximumAlerts {
			return result, ErrConflict
		}
		observed := make(map[string]bool, len(candidates))
		normalizedCandidates := make([]Candidate, 0, len(candidates))
		for _, candidate := range candidates {
			candidate, err = normalizeCandidate(candidate)
			if err != nil {
				return result, err
			}
			dedup := deduplicationKey(s.organizationID, rule.ID, candidate.TargetType, candidate.TargetID)
			if observed[dedup] {
				return result, ErrConflict
			}
			observed[dedup] = true
			normalizedCandidates = append(normalizedCandidates, candidate)
		}
		openAlerts := make([]Alert, 0, MaximumAlerts)
		for _, status := range []AlertStatus{StatusActive, StatusAcknowledged} {
			items, err := s.store.ListAlerts(ctx, s.organizationID, AlertQuery{RuleID: rule.ID, Status: status, Limit: MaximumAlerts})
			if err != nil {
				return result, err
			}
			openAlerts = append(openAlerts, items...)
		}
		if len(openAlerts) > MaximumAlerts {
			return result, ErrConflict
		}
		for _, candidate := range normalizedCandidates {
			dedup := deduplicationKey(s.organizationID, rule.ID, candidate.TargetType, candidate.TargetID)
			alert, created, err := s.upsertCandidate(ctx, rule, candidate, dedup, asOf)
			if err != nil {
				return result, err
			}
			if created {
				result.Created++
				if err := s.enqueueDeliveries(ctx, alert, asOf); err != nil {
					return result, err
				}
			} else {
				result.Refreshed++
			}
		}
		for _, alert := range openAlerts {
			if observed[alert.DeduplicationKey] {
				continue
			}
			resolved := alert
			resolved.Status, resolved.ResolvedAt, resolved.LastObservedAt, resolved.Revision = StatusResolved, &asOf, asOf, alert.Revision+1
			if _, err := s.store.UpdateAlert(ctx, resolved, alert.Revision, s.history(resolved, "resolved", actor(ctx), asOf)); err != nil {
				return result, err
			}
			result.Resolved++
		}
	}
	if err := s.audit(ctx, "signals.evaluation.completed", "signal_evaluation", asOf.Format("20060102T150405Z"), map[string]string{
		"rules": strconv.Itoa(result.Rules), "created": strconv.Itoa(result.Created), "refreshed": strconv.Itoa(result.Refreshed), "resolved": strconv.Itoa(result.Resolved),
	}); err != nil {
		return result, fmt.Errorf("audit Signals evaluation: %w", err)
	}
	return result, nil
}

func (s *Service) Acknowledge(ctx context.Context, alertID string, revision int64) (Alert, error) {
	alert, err := s.GetAlert(ctx, alertID)
	if err != nil {
		return Alert{}, err
	}
	if revision < 1 || alert.Revision != revision || alert.Status == StatusResolved {
		return Alert{}, ErrConflict
	}
	now := s.now().UTC()
	alert.Status, alert.AcknowledgedBy, alert.AcknowledgedAt, alert.Revision = StatusAcknowledged, actor(ctx), &now, alert.Revision+1
	updated, err := s.store.UpdateAlert(ctx, alert, revision, s.history(alert, "acknowledged", actor(ctx), now))
	if err != nil {
		return Alert{}, err
	}
	if err := s.audit(ctx, "signals.alert.acknowledged", "signal_alert", updated.ID, map[string]string{"condition": string(updated.Condition)}); err != nil {
		return Alert{}, fmt.Errorf("audit Signals acknowledgment: %w", err)
	}
	return updated, nil
}

func (s *Service) Assign(ctx context.Context, alertID string, input AssignmentInput) (Alert, error) {
	input.Kind, input.TargetID = strings.ToLower(strings.TrimSpace(input.Kind)), strings.TrimSpace(input.TargetID)
	if input.Kind != "identity" && input.Kind != "group" || !stableIDPattern.MatchString(input.TargetID) || input.Revision < 1 {
		return Alert{}, ErrInvalidInput
	}
	alert, err := s.GetAlert(ctx, alertID)
	if err != nil {
		return Alert{}, err
	}
	if alert.Revision != input.Revision || alert.Status == StatusResolved {
		return Alert{}, ErrConflict
	}
	now := s.now().UTC()
	alert.AssignedKind, alert.AssignedID, alert.Revision = input.Kind, input.TargetID, alert.Revision+1
	updated, err := s.store.UpdateAlert(ctx, alert, input.Revision, s.history(alert, "assigned", actor(ctx), now))
	if err != nil {
		return Alert{}, err
	}
	if err := s.audit(ctx, "signals.alert.assigned", "signal_alert", updated.ID, map[string]string{"assignedKind": updated.AssignedKind}); err != nil {
		return Alert{}, fmt.Errorf("audit Signals assignment: %w", err)
	}
	return updated, nil
}

func (s *Service) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	return s.store.ListSubscriptions(ctx, s.organizationID)
}

func (s *Service) ListSubscriptionTargets(ctx context.Context) ([]SubscriptionTarget, error) {
	items, err := s.targets.ListSubscriptionTargets(ctx, s.organizationID)
	if err != nil {
		return nil, err
	}
	if len(items) > MaximumSubscriptionTargets {
		return nil, ErrConflict
	}
	result, seen := make([]SubscriptionTarget, 0, len(items)), map[string]bool{}
	for _, item := range items {
		item.TargetKind = strings.ToLower(strings.TrimSpace(item.TargetKind))
		item.TargetID, item.Label = strings.TrimSpace(item.TargetID), strings.TrimSpace(item.Label)
		if (item.TargetKind != "group" && item.TargetKind != "webhook") || !stableIDPattern.MatchString(item.TargetID) || !validText(item.Label, 1, 160) {
			return nil, ErrConflict
		}
		key := item.TargetKind + "\x00" + item.TargetID
		if seen[key] {
			return nil, ErrConflict
		}
		seen[key] = true
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TargetKind == result[j].TargetKind {
			return result[i].TargetID < result[j].TargetID
		}
		return result[i].TargetKind < result[j].TargetKind
	})
	return result, nil
}

func (s *Service) CreateSubscription(ctx context.Context, input CreateSubscriptionInput) (Subscription, error) {
	input.ID, input.RuleID, input.TargetKind, input.TargetID = strings.TrimSpace(input.ID), strings.TrimSpace(input.RuleID), strings.ToLower(strings.TrimSpace(input.TargetKind)), strings.TrimSpace(input.TargetID)
	if !optionalID(input.ID) || !optionalID(input.RuleID) || (input.TargetKind != "group" && input.TargetKind != "webhook") || !stableIDPattern.MatchString(input.TargetID) {
		return Subscription{}, ErrInvalidInput
	}
	if input.RuleID != "" {
		if _, err := s.store.GetRule(ctx, s.organizationID, input.RuleID); err != nil {
			return Subscription{}, err
		}
	}
	targets, err := s.ListSubscriptionTargets(ctx)
	if err != nil {
		return Subscription{}, err
	}
	targetAvailable := false
	for _, target := range targets {
		if target.TargetKind == input.TargetKind && target.TargetID == input.TargetID {
			targetAvailable = true
			break
		}
	}
	if !targetAvailable {
		return Subscription{}, ErrInvalidInput
	}
	id := input.ID
	if id == "" {
		id, err = foundation.NewCorrelationID()
		if err != nil {
			return Subscription{}, err
		}
	}
	if err := s.checkWrite(ctx, "signals.subscription", id); err != nil {
		return Subscription{}, err
	}
	now := s.now().UTC()
	subscription := Subscription{ID: id, OrganizationID: s.organizationID, RuleID: input.RuleID, TargetKind: input.TargetKind, TargetID: input.TargetID, Enabled: true, CreatedBy: actor(ctx), Revision: 1, CreatedAt: now, UpdatedAt: now}
	created, err := s.store.CreateSubscription(ctx, subscription)
	if err != nil {
		return Subscription{}, err
	}
	if err := s.audit(ctx, "signals.subscription.created", "signal_subscription", created.ID, map[string]string{"targetKind": created.TargetKind, "ruleScoped": strconv.FormatBool(created.RuleID != "")}); err != nil {
		return Subscription{}, fmt.Errorf("audit Signals subscription: %w", err)
	}
	return created, nil
}

func (s *Service) DeleteSubscription(ctx context.Context, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if !stableIDPattern.MatchString(id) {
		return false, ErrInvalidInput
	}
	if err := s.checkWrite(ctx, "signals.subscription", id); err != nil {
		return false, err
	}
	deleted, err := s.store.DeleteSubscription(ctx, s.organizationID, id)
	if err != nil || !deleted {
		return deleted, err
	}
	if err := s.audit(ctx, "signals.subscription.deleted", "signal_subscription", id, nil); err != nil {
		return false, fmt.Errorf("audit Signals subscription deletion: %w", err)
	}
	return true, nil
}

func (s *Service) ListPendingDeliveries(ctx context.Context, asOf time.Time, limit int) ([]Delivery, error) {
	if asOf.IsZero() {
		asOf = s.now()
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidInput
	}
	return s.store.ListPendingDeliveries(ctx, s.organizationID, asOf.UTC(), limit)
}

// RecordDeliveryAttempt is the narrow Reach integration seam. ErrorCode must
// be a stable sanitized code; provider response bodies are never accepted.
func (s *Service) RecordDeliveryAttempt(ctx context.Context, deliveryID string, succeeded, retryable bool, errorCode string) (Delivery, error) {
	deliveryID, errorCode = strings.TrimSpace(deliveryID), strings.ToLower(strings.TrimSpace(errorCode))
	if !stableIDPattern.MatchString(deliveryID) || (!succeeded && !stableIDPattern.MatchString(errorCode)) || succeeded && errorCode != "" {
		return Delivery{}, ErrInvalidInput
	}
	pending, err := s.store.ListPendingDeliveries(ctx, s.organizationID, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), 500)
	if err != nil {
		return Delivery{}, err
	}
	var delivery Delivery
	for _, candidate := range pending {
		if candidate.ID == deliveryID {
			delivery = candidate
			break
		}
	}
	if delivery.ID == "" {
		return Delivery{}, ErrNotFound
	}
	now := s.now().UTC()
	expectedAttempts := delivery.Attempts
	delivery.Attempts++
	delivery.UpdatedAt, delivery.LastErrorCode, delivery.NextAttemptAt = now, errorCode, nil
	switch {
	case succeeded:
		delivery.Status = "delivered"
	case retryable && delivery.Attempts < MaximumDeliveryTries:
		delivery.Status = "pending"
		next := now.Add(retryDelay(delivery.Attempts))
		delivery.NextAttemptAt = &next
	default:
		delivery.Status = "failed"
	}
	return s.store.UpdateDelivery(ctx, delivery, expectedAttempts)
}

func (s *Service) ExportCSV(ctx context.Context, query AlertQuery) ([]byte, error) {
	alerts, err := s.ListAlerts(ctx, query)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"alert_id", "rule_id", "condition", "severity", "status", "title", "target_type", "target_id", "due_at", "assigned_kind", "assigned_id", "first_detected_at", "last_observed_at"})
	for _, alert := range alerts {
		dueAt := ""
		if alert.DueAt != nil {
			dueAt = alert.DueAt.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{safeCSV(alert.ID), safeCSV(alert.RuleID), string(alert.Condition), string(alert.Severity), string(alert.Status), safeCSV(alert.Title), safeCSV(alert.TargetType), safeCSV(alert.TargetID), dueAt, alert.AssignedKind, safeCSV(alert.AssignedID), alert.FirstDetectedAt.UTC().Format(time.RFC3339), alert.LastObservedAt.UTC().Format(time.RFC3339)})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (s *Service) upsertCandidate(ctx context.Context, rule Rule, candidate Candidate, dedup string, asOf time.Time) (Alert, bool, error) {
	existing, err := s.store.GetAlertByDeduplicationKey(ctx, s.organizationID, dedup)
	if err == nil {
		updated := existing
		updated.Condition, updated.Severity, updated.Title, updated.Summary = rule.Condition, rule.Severity, candidate.Title, candidate.Summary
		updated.DueAt, updated.ThresholdDays, updated.LastObservedAt = candidate.DueAt, candidate.ThresholdDays, asOf
		action := "refreshed"
		if updated.Status == StatusResolved {
			updated.Status, updated.ResolvedAt, updated.AcknowledgedAt, updated.AcknowledgedBy = StatusActive, nil, nil, ""
			action = "reopened"
		}
		updated.Revision++
		updated, err = s.store.UpdateAlert(ctx, updated, existing.Revision, s.history(updated, action, actor(ctx), asOf))
		return updated, false, err
	}
	if !errors.Is(err, ErrNotFound) {
		return Alert{}, false, err
	}
	id := dedup[:32]
	alert := Alert{ID: id, OrganizationID: s.organizationID, RuleID: rule.ID, Condition: rule.Condition, Severity: rule.Severity, Status: StatusActive,
		Title: candidate.Title, Summary: candidate.Summary, TargetType: candidate.TargetType, TargetID: candidate.TargetID, DueAt: candidate.DueAt,
		ThresholdDays: candidate.ThresholdDays, DeduplicationKey: dedup, FirstDetectedAt: asOf, LastObservedAt: asOf, Revision: 1}
	created, err := s.store.CreateAlert(ctx, alert, s.history(alert, "created", actor(ctx), asOf))
	if err != nil {
		return Alert{}, false, err
	}
	return created, true, nil
}

func (s *Service) enqueueDeliveries(ctx context.Context, alert Alert, now time.Time) error {
	subscriptions, err := s.store.ListSubscriptions(ctx, s.organizationID)
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		if !subscription.Enabled || subscription.RuleID != "" && subscription.RuleID != alert.RuleID {
			continue
		}
		digest := sha256.Sum256([]byte(alert.ID + "\x00" + strconv.FormatInt(alert.Revision, 10) + "\x00" + subscription.ID))
		id := hex.EncodeToString(digest[:16])
		next := now
		_, _, err := s.store.CreateDelivery(ctx, Delivery{ID: id, OrganizationID: s.organizationID, AlertID: alert.ID, SubscriptionID: subscription.ID,
			TargetKind: subscription.TargetKind, TargetID: subscription.TargetID, Status: "pending", NextAttemptAt: &next, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) history(alert Alert, action, actorID string, at time.Time) AlertHistory {
	digest := sha256.Sum256([]byte(alert.ID + "\x00" + strconv.FormatInt(alert.Revision, 10) + "\x00" + action))
	return AlertHistory{ID: hex.EncodeToString(digest[:16]), OrganizationID: s.organizationID, AlertID: alert.ID, Action: action, ActorID: actorID, OccurredAt: at.UTC(), Revision: alert.Revision}
}

func (s *Service) audit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["requirementId"], metadata["featureId"] = RequirementID, FeatureID
	scope, ok := foundation.ScopeFromContext(ctx)
	if !ok || scope.CorrelationID == "" {
		correlationID, err := foundation.NewCorrelationID()
		if err != nil {
			return err
		}
		scope = foundation.Scope{OrganizationID: s.organizationID, ActorID: actor(ctx), CorrelationID: correlationID}
		ctx = foundation.WithScope(ctx, scope)
	}
	eventID, err := foundation.NewCorrelationID()
	if err != nil {
		return err
	}
	return s.auditor.Record(ctx, foundation.AuditEvent{ID: eventID, OrganizationID: s.organizationID, ActorID: actor(ctx), CorrelationID: scope.CorrelationID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID, OccurredAt: s.now().UTC(), Metadata: metadata})
}

func normalizeRuleInput(input CreateRuleInput) (CreateRuleInput, error) {
	input.ID, input.Name = strings.TrimSpace(input.ID), strings.TrimSpace(input.Name)
	input.Condition, input.Severity = Condition(strings.ToLower(strings.TrimSpace(string(input.Condition)))), Severity(strings.ToLower(strings.TrimSpace(string(input.Severity))))
	input.FiscalPeriod, input.Scenario = strings.TrimSpace(input.FiscalPeriod), strings.ToLower(strings.TrimSpace(input.Scenario))
	if !optionalID(input.ID) || !validText(input.Name, 1, 160) || !conditions[input.Condition] || !severities[input.Severity] ||
		(input.FiscalPeriod != "" && !fiscalPeriodPattern.MatchString(input.FiscalPeriod)) || (input.Scenario != "" && !scenarioPattern.MatchString(input.Scenario)) {
		return CreateRuleInput{}, ErrInvalidInput
	}
	if (input.FiscalPeriod != "" || input.Scenario != "") && !supportsPeriodFilter(input.Condition) {
		return CreateRuleInput{}, ErrInvalidInput
	}
	if input.Condition == ConditionRenewal || input.Condition == ConditionExpiration {
		if len(input.ThresholdDays) == 0 {
			input.ThresholdDays = []int{180, 90, 60, 30}
		}
	} else if input.Condition == ConditionOverdue || input.Condition == ConditionUnpaid || input.Condition == ConditionUnusedCommitment {
		if len(input.ThresholdDays) == 0 {
			input.ThresholdDays = []int{30}
		}
	}
	var thresholdsValid bool
	input.ThresholdDays, thresholdsValid = normalizeThresholds(input.ThresholdDays)
	if !thresholdsValid {
		return CreateRuleInput{}, ErrInvalidInput
	}
	if len(input.ThresholdDays) > 8 {
		return CreateRuleInput{}, ErrInvalidInput
	}
	if needsThreshold(input.Condition) && len(input.ThresholdDays) == 0 || !needsThreshold(input.Condition) && len(input.ThresholdDays) != 0 {
		return CreateRuleInput{}, ErrInvalidInput
	}
	return input, nil
}

func normalizeCandidate(candidate Candidate) (Candidate, error) {
	candidate.TargetType, candidate.TargetID = strings.ToLower(strings.TrimSpace(candidate.TargetType)), strings.TrimSpace(candidate.TargetID)
	candidate.Title, candidate.Summary = strings.TrimSpace(candidate.Title), strings.TrimSpace(candidate.Summary)
	if !regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`).MatchString(candidate.TargetType) || !stableIDPattern.MatchString(candidate.TargetID) ||
		!validText(candidate.Title, 1, 200) || !validText(candidate.Summary, 1, 500) || candidate.ThresholdDays < 0 || candidate.ThresholdDays > 3660 {
		return Candidate{}, ErrInvalidInput
	}
	if candidate.DueAt != nil {
		due := candidate.DueAt.UTC()
		if due.Year() < 1970 || due.Year() > 9999 {
			return Candidate{}, ErrInvalidInput
		}
		candidate.DueAt = &due
	}
	return candidate, nil
}

func normalizeThresholds(values []int) ([]int, bool) {
	set := map[int]bool{}
	for _, value := range values {
		if value < 0 || value > 3660 {
			return nil, false
		}
		if set[value] {
			return nil, false
		}
		set[value] = true
	}
	result := make([]int, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(result)))
	return result, true
}

func needsThreshold(condition Condition) bool {
	return condition == ConditionRenewal || condition == ConditionExpiration || condition == ConditionOverdue || condition == ConditionUnpaid || condition == ConditionUnusedCommitment
}

func supportsPeriodFilter(condition Condition) bool {
	return condition == ConditionOverBudget || condition == ConditionForecastOverBudget || condition == ConditionUnusedCommitment || condition == ConditionReconciliation
}

func deduplicationKey(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func retryDelay(attempt int) time.Duration {
	delay := 5 * time.Minute
	for index := 1; index < attempt && delay < 24*time.Hour; index++ {
		delay *= 2
	}
	if delay > 24*time.Hour {
		return 24 * time.Hour
	}
	return delay
}

func actor(ctx context.Context) string {
	if scope, ok := foundation.ScopeFromContext(ctx); ok && strings.TrimSpace(scope.ActorID) != "" {
		return scope.ActorID
	}
	return "system:signals"
}

func optionalID(value string) bool { return value == "" || stableIDPattern.MatchString(value) }
func validText(value string, minimum, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= minimum && utf8.RuneCountInString(value) <= maximum && !strings.ContainsRune(value, '\x00')
}
func safeCSV(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}
