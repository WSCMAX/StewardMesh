package signals_test

// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type evaluator struct{ candidates []signals.Candidate }

func (e *evaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return append([]signals.Candidate(nil), e.candidates...), nil
}

func TestRulesDefaultRenewalThresholdsAndDeduplicateEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	source := &evaluator{candidates: []signals.Candidate{{TargetType: "contract", TargetID: "contract-1", Title: "Renewal approaching", Summary: "Contract renews soon."}}}
	service, err := signals.NewService(repository.NewMemorySignalsStore(), source, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "organization-1", ActorID: "account-1", CorrelationID: "correlation-1"})
	rule, err := service.CreateRule(ctx, signals.CreateRuleInput{ID: "rule-1", Name: "Renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning})
	if err != nil {
		t.Fatal(err)
	}
	if got := rule.ThresholdDays; len(got) != 4 || got[0] != 180 || got[1] != 90 || got[2] != 60 || got[3] != 30 {
		t.Fatalf("unexpected thresholds %#v", got)
	}
	first, err := service.Evaluate(ctx, now)
	if err != nil || first.Created != 1 {
		t.Fatalf("first evaluation %#v err=%v", first, err)
	}
	second, err := service.Evaluate(ctx, now.Add(time.Hour))
	if err != nil || second.Created != 0 || second.Refreshed != 1 {
		t.Fatalf("deduplicated evaluation %#v err=%v", second, err)
	}
	alerts, err := service.ListAlerts(ctx, signals.AlertQuery{})
	if err != nil || len(alerts) != 1 || alerts[0].Revision != 2 {
		t.Fatalf("alerts %#v err=%v", alerts, err)
	}
}

func TestAcknowledgmentAssignmentResolutionHistoryAndCSVProtection(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	source := &evaluator{candidates: []signals.Candidate{{TargetType: "cost", TargetID: "cost-1", Title: "=unsafe title", Summary: "Cost needs reconciliation."}}}
	service, _ := signals.NewService(repository.NewMemorySignalsStore(), source, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1", Now: func() time.Time { return now }})
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "organization-1", ActorID: "account-1", CorrelationID: "correlation-1"})
	_, _ = service.CreateRule(ctx, signals.CreateRuleInput{ID: "rule-1", Name: "Reconciliation", Condition: signals.ConditionReconciliation, Severity: signals.SeverityCritical})
	_, _ = service.Evaluate(ctx, now)
	alerts, _ := service.ListAlerts(ctx, signals.AlertQuery{})
	alert := alerts[0]
	ack, err := service.Acknowledge(ctx, alert.ID, alert.Revision)
	if err != nil || ack.Status != signals.StatusAcknowledged || ack.AcknowledgedBy != "account-1" {
		t.Fatalf("ack %#v err=%v", ack, err)
	}
	assigned, err := service.Assign(ctx, alert.ID, signals.AssignmentInput{Kind: "group", TargetID: "finance-ops", Revision: ack.Revision})
	if err != nil || assigned.AssignedID != "finance-ops" {
		t.Fatalf("assignment %#v err=%v", assigned, err)
	}
	source.candidates = nil
	result, err := service.Evaluate(ctx, now.Add(time.Hour))
	if err != nil || result.Resolved != 1 {
		t.Fatalf("resolution %#v err=%v", result, err)
	}
	history, err := service.ListAlertHistory(ctx, alert.ID)
	if err != nil || len(history) != 4 || history[0].Action != "resolved" {
		t.Fatalf("history %#v err=%v", history, err)
	}
	csv, err := service.ExportCSV(ctx, signals.AlertQuery{Limit: 10})
	if err != nil || !strings.Contains(string(csv), "'=unsafe title") {
		t.Fatalf("CSV was not spreadsheet-safe: %q err=%v", csv, err)
	}
}

func TestSubscriptionsCreateReachHandoffAndBoundRetries(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	source := &evaluator{candidates: []signals.Candidate{{TargetType: "budget", TargetID: "budget-1", Title: "Budget exceeded", Summary: "Recognized costs exceed allocation."}}}
	store := repository.NewMemorySignalsStore()
	service, _ := signals.NewService(store, source, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1", Now: func() time.Time { return now }})
	ctx := context.Background()
	_, _ = service.CreateRule(ctx, signals.CreateRuleInput{ID: "rule-1", Name: "Budget", Condition: signals.ConditionOverBudget, Severity: signals.SeverityCritical})
	_, err := service.CreateSubscription(ctx, signals.CreateSubscriptionInput{ID: "subscription-1", RuleID: "rule-1", TargetKind: "webhook", TargetID: "finance-webhook"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Evaluate(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := service.ListPendingDeliveries(ctx, now, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].TargetID != "finance-webhook" {
		t.Fatalf("deliveries %#v err=%v", deliveries, err)
	}
	updated, err := service.RecordDeliveryAttempt(ctx, deliveries[0].ID, false, true, "provider_unavailable")
	if err != nil || updated.Status != "pending" || updated.Attempts != 1 || updated.NextAttemptAt == nil {
		t.Fatalf("retry %#v err=%v", updated, err)
	}
	if _, err := service.RecordDeliveryAttempt(ctx, deliveries[0].ID, false, true, "secret provider response body"); !errors.Is(err, signals.ErrInvalidInput) {
		t.Fatalf("expected bounded error code rejection, got %v", err)
	}
	for attempt := 2; attempt <= signals.MaximumDeliveryTries; attempt++ {
		updated, err = service.RecordDeliveryAttempt(ctx, deliveries[0].ID, false, true, "provider_unavailable")
		if err != nil {
			t.Fatalf("delivery attempt %d: %v", attempt, err)
		}
	}
	if updated.Status != "failed" || updated.Attempts != signals.MaximumDeliveryTries || updated.NextAttemptAt != nil {
		t.Fatalf("delivery did not stop after bounded attempts %#v", updated)
	}
	if _, err := service.RecordDeliveryAttempt(ctx, deliveries[0].ID, false, true, "provider_unavailable"); !errors.Is(err, signals.ErrNotFound) {
		t.Fatalf("terminal delivery should not be replayable, got %v", err)
	}
}

func TestInvalidRulesAndDuplicateCandidatesFailClosed(t *testing.T) {
	service, _ := signals.NewService(repository.NewMemorySignalsStore(), &evaluator{}, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1"})
	if _, err := service.CreateRule(context.Background(), signals.CreateRuleInput{Name: "Bad", Condition: "shell", Severity: signals.SeverityWarning}); !errors.Is(err, signals.ErrInvalidInput) {
		t.Fatalf("expected invalid condition, got %v", err)
	}
	duplicate := signals.Candidate{TargetType: "contract", TargetID: "contract-1", Title: "A", Summary: "B"}
	source := &evaluator{candidates: []signals.Candidate{duplicate, duplicate}}
	service, _ = signals.NewService(repository.NewMemorySignalsStore(), source, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1"})
	_, _ = service.CreateRule(context.Background(), signals.CreateRuleInput{ID: "rule-1", Name: "Renew", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning})
	if _, err := service.Evaluate(context.Background(), time.Now().UTC()); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if alerts, err := service.ListAlerts(context.Background(), signals.AlertQuery{}); err != nil || len(alerts) != 0 {
		t.Fatalf("duplicate candidate validation partially wrote alerts %#v err=%v", alerts, err)
	}
}

func TestRuleThresholdAndFilterValidationFailsClosed(t *testing.T) {
	service, _ := signals.NewService(repository.NewMemorySignalsStore(), &evaluator{}, foundation.NopAuditor{}, signals.ServiceConfig{OrganizationID: "organization-1"})
	for _, input := range []signals.CreateRuleInput{
		{Name: "Negative", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning, ThresholdDays: []int{-1, 30}},
		{Name: "Too large", Condition: signals.ConditionExpiration, Severity: signals.SeverityWarning, ThresholdDays: []int{3661}},
		{Name: "Unsupported filter", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning, FiscalPeriod: "FY2027"},
		{Name: "Thresholdless condition", Condition: signals.ConditionOverBudget, Severity: signals.SeverityWarning, ThresholdDays: []int{30}},
		{Name: "Duplicate thresholds", Condition: signals.ConditionExpiration, Severity: signals.SeverityWarning, ThresholdDays: []int{30, 30}},
	} {
		if _, err := service.CreateRule(context.Background(), input); !errors.Is(err, signals.ErrInvalidInput) {
			t.Fatalf("expected invalid Signals rule %#v, got %v", input, err)
		}
	}
	created, err := service.CreateRule(context.Background(), signals.CreateRuleInput{Name: "Filtered commitments", Condition: signals.ConditionUnusedCommitment, Severity: signals.SeverityWarning, FiscalPeriod: "FY2027", Scenario: "baseline"})
	if err != nil || created.FiscalPeriod != "FY2027" || created.Scenario != "baseline" {
		t.Fatalf("supported Signals filter was rejected %#v err=%v", created, err)
	}
}
