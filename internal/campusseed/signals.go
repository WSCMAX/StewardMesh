package campusseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

func (s *ExtendedSeeder) seedSignals(ctx context.Context) (int, int, error) {
	fiscalPeriod := fmt.Sprintf("FY%d", s.now().Year())
	enabled := true
	ruleDefinitions := []signals.CreateRuleInput{
		{ID: campusDemoSignalsRulePrefix + "expiration", Name: "Campus license and contract expirations", Condition: signals.ConditionExpiration, Severity: signals.SeverityWarning, Enabled: &enabled, ThresholdDays: []int{180, 90, 60, 30}},
		{ID: campusDemoSignalsRulePrefix + "renewal", Name: "Campus contract renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityInfo, Enabled: &enabled, ThresholdDays: []int{180, 90, 60, 30}},
		{ID: campusDemoSignalsRulePrefix + "over-budget", Name: "Campus IT budget overrun", Condition: signals.ConditionOverBudget, Severity: signals.SeverityCritical, Enabled: &enabled, FiscalPeriod: fiscalPeriod, Scenario: "baseline"},
		{ID: campusDemoSignalsRulePrefix + "reconciliation", Name: "Campus cost reconciliation gaps", Condition: signals.ConditionReconciliation, Severity: signals.SeverityWarning, Enabled: &enabled, FiscalPeriod: fiscalPeriod, Scenario: "baseline"},
		{ID: campusDemoSignalsRulePrefix + "unused-commitment", Name: "Campus unused commitments", Condition: signals.ConditionUnusedCommitment, Severity: signals.SeverityWarning, Enabled: &enabled, FiscalPeriod: fiscalPeriod, Scenario: "baseline", ThresholdDays: []int{180, 90, 60, 30}},
	}
	createdRules := 0
	for _, input := range ruleDefinitions {
		if _, err := s.signals.CreateRule(ctx, input); err != nil && !errors.Is(err, signals.ErrConflict) {
			return createdRules, 0, fmt.Errorf("create signals rule %q: %w", input.Name, err)
		}
		createdRules++
	}
	evaluation, err := s.signals.Evaluate(ctx, s.now())
	if err != nil && !errors.Is(err, signals.ErrConflict) {
		return createdRules, 0, fmt.Errorf("evaluate campus demo signals: %w", err)
	}
	for _, input := range []signals.CreateSubscriptionInput{
		{ID: "campus-demo-over-budget-subscription", RuleID: campusDemoSignalsRulePrefix + "over-budget", TargetKind: "group", TargetID: "campus-demo-it-ops"},
		{ID: "campus-demo-expiration-subscription", RuleID: campusDemoSignalsRulePrefix + "expiration", TargetKind: "group", TargetID: "campus-demo-it-ops"},
	} {
		if _, err := s.signals.CreateSubscription(ctx, input); err != nil && !errors.Is(err, signals.ErrConflict) {
			return createdRules, evaluation.Created, fmt.Errorf("create signals subscription: %w", err)
		}
	}
	return createdRules, evaluation.Created, nil
}
