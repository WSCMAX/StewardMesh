package contracttest

// Provider-neutral Signals adapter contract.
// Requirement: REQ-SIGNALS-001. Feature: alerts.rules. GitHub: #11.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/signals"
)

func SignalsStore(t testing.TB, subject signals.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	rule := signals.Rule{ID: "signal-rule-" + suffix, OrganizationID: organizationID, Name: "Renewals " + suffix, Condition: signals.ConditionRenewal,
		Severity: signals.SeverityWarning, Enabled: true, ThresholdDays: []int{180, 90, 60, 30}, CreatedBy: "account-one", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := subject.GetRule(ctx, organizationID, rule.ID); !errors.Is(err, signals.ErrNotFound) {
		t.Fatalf("expected missing Signals rule, got %v", err)
	}
	created, err := subject.CreateRule(ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	rule.ThresholdDays[0] = 1
	loaded, err := subject.GetRule(ctx, organizationID, created.ID)
	if err != nil || loaded.ThresholdDays[0] != 180 {
		t.Fatalf("Signals thresholds were not defensively persisted: %#v err=%v", loaded, err)
	}
	if _, err := subject.CreateRule(ctx, loaded); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected duplicate Signals rule conflict, got %v", err)
	}
	items, err := subject.ListRules(ctx, organizationID)
	if err != nil || len(items) != 1 || items[0].ID != loaded.ID {
		t.Fatalf("unexpected Signals rules %#v err=%v", items, err)
	}
	if isolated, err := subject.ListRules(ctx, organizationID+"-other"); err != nil || len(isolated) != 0 {
		t.Fatalf("Signals organization isolation failed: %#v err=%v", isolated, err)
	}
	updatedRule := loaded
	updatedRule.Name, updatedRule.Revision, updatedRule.UpdatedAt = "Updated renewals "+suffix, 2, now.Add(time.Minute)
	updatedRule, err = subject.UpdateRule(ctx, updatedRule, 1)
	if err != nil || updatedRule.Revision != 2 {
		t.Fatalf("update Signals rule %#v err=%v", updatedRule, err)
	}
	if _, err := subject.UpdateRule(ctx, updatedRule, 1); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected stale Signals rule conflict, got %v", err)
	}

	history := signals.AlertHistory{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OrganizationID: organizationID, AlertID: "signal-alert-" + suffix, Action: "created", ActorID: "account-one", OccurredAt: now, Revision: 1}
	alert := signals.Alert{ID: history.AlertID, OrganizationID: organizationID, RuleID: loaded.ID, Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning,
		Status: signals.StatusActive, Title: "Renewal approaching", Summary: "The contract renewal is approaching.", TargetType: "contract", TargetID: "contract-" + suffix,
		ThresholdDays: 30, DeduplicationKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", FirstDetectedAt: now, LastObservedAt: now, Revision: 1}
	createdAlert, err := subject.CreateAlert(ctx, alert, history)
	if err != nil || createdAlert.ID != alert.ID {
		t.Fatalf("create Signals alert %#v err=%v", createdAlert, err)
	}
	byDedup, err := subject.GetAlertByDeduplicationKey(ctx, organizationID, alert.DeduplicationKey)
	if err != nil || byDedup.ID != alert.ID {
		t.Fatalf("get Signals alert by deduplication %#v err=%v", byDedup, err)
	}
	queue, err := subject.ListAlerts(ctx, organizationID, signals.AlertQuery{Status: signals.StatusActive, Limit: 10})
	if err != nil || len(queue) != 1 {
		t.Fatalf("list Signals alert queue %#v err=%v", queue, err)
	}
	acknowledged := createdAlert
	acknowledged.Status, acknowledged.AcknowledgedBy, acknowledged.Revision = signals.StatusAcknowledged, "account-one", 2
	ackAt := now.Add(time.Minute)
	acknowledged.AcknowledgedAt, acknowledged.LastObservedAt = &ackAt, ackAt
	ackHistory := signals.AlertHistory{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", OrganizationID: organizationID, AlertID: alert.ID, Action: "acknowledged", ActorID: "account-one", OccurredAt: ackAt, Revision: 2}
	acknowledged, err = subject.UpdateAlert(ctx, acknowledged, 1, ackHistory)
	if err != nil || acknowledged.Status != signals.StatusAcknowledged {
		t.Fatalf("acknowledge Signals alert %#v err=%v", acknowledged, err)
	}
	if _, err := subject.UpdateAlert(ctx, acknowledged, 1, ackHistory); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected stale Signals alert conflict, got %v", err)
	}
	historyItems, err := subject.ListAlertHistory(ctx, organizationID, alert.ID)
	if err != nil || len(historyItems) != 2 || historyItems[0].Revision != 2 {
		t.Fatalf("unexpected Signals history %#v err=%v", historyItems, err)
	}

	subscription := signals.Subscription{ID: "signal-subscription-" + suffix, OrganizationID: organizationID, RuleID: loaded.ID, TargetKind: "group", TargetID: "finance-owners", Enabled: true, CreatedBy: "account-one", CreatedAt: now}
	createdSubscription, err := subject.CreateSubscription(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := subject.CreateSubscription(ctx, subscription); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected duplicate Signals subscription conflict, got %v", err)
	}
	subscriptions, err := subject.ListSubscriptions(ctx, organizationID)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].ID != subscription.ID {
		t.Fatalf("unexpected Signals subscriptions %#v err=%v", subscriptions, err)
	}
	next := now
	delivery := signals.Delivery{ID: "cccccccccccccccccccccccccccccccc", OrganizationID: organizationID, AlertID: alert.ID, SubscriptionID: subscription.ID,
		TargetKind: subscription.TargetKind, TargetID: subscription.TargetID, Status: "pending", NextAttemptAt: &next, CreatedAt: now, UpdatedAt: now}
	createdDelivery, wasCreated, err := subject.CreateDelivery(ctx, delivery)
	if err != nil || !wasCreated || createdDelivery.ID != delivery.ID {
		t.Fatalf("create Signals delivery %#v created=%v err=%v", createdDelivery, wasCreated, err)
	}
	if replay, wasCreated, err := subject.CreateDelivery(ctx, delivery); err != nil || wasCreated || replay.ID != delivery.ID {
		t.Fatalf("idempotent Signals delivery %#v created=%v err=%v", replay, wasCreated, err)
	}
	conflicting := delivery
	conflicting.TargetID = "different-target"
	if _, _, err := subject.CreateDelivery(ctx, conflicting); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected conflicting Signals delivery replay, got %v", err)
	}
	pending, err := subject.ListPendingDeliveries(ctx, organizationID, now, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("unexpected pending Signals deliveries %#v err=%v", pending, err)
	}
	delivery.Status, delivery.Attempts, delivery.NextAttemptAt, delivery.UpdatedAt = "delivered", 1, nil, now.Add(time.Minute)
	if updated, err := subject.UpdateDelivery(ctx, delivery, 0); err != nil || updated.Status != "delivered" {
		t.Fatalf("update Signals delivery %#v err=%v", updated, err)
	}
	if _, err := subject.UpdateDelivery(ctx, delivery, 0); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("expected stale Signals delivery conflict, got %v", err)
	}
	deleted, err := subject.DeleteSubscription(ctx, organizationID, createdSubscription.ID)
	if err != nil || !deleted {
		t.Fatalf("delete Signals subscription deleted=%v err=%v", deleted, err)
	}
}
