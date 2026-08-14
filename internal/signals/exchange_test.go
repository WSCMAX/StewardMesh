package signals_test

// Requirements: REQ-SIGNALS-001, REQ-EXCHANGE-001.
// Features: alerts.rules, migration.packages. GitHub: #9, #11.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type signalsExchangeAuditor struct {
	events   map[string]foundation.AuditEvent
	attempts []foundation.AuditEvent
}

func (a *signalsExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.events == nil {
		a.events = map[string]foundation.AuditEvent{}
	}
	a.attempts = append(a.attempts, event)
	if existing, ok := a.events[event.ID]; ok {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("conflicting audit replay")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

type signalsExchangeWriteGate struct {
	err   error
	calls []string
}

func (g *signalsExchangeWriteGate) CheckResourceWrite(_ context.Context, recordType, id string) error {
	g.calls = append(g.calls, recordType+":"+id)
	return g.err
}

func TestSignalsExchangeImporterPreservesStateRepairsAuditAndFencesOrdinaryWrites(t *testing.T) {
	createdAt := time.Date(2022, time.January, 2, 3, 4, 5, 600_000_000, time.UTC)
	updatedAt := createdAt.Add(48 * time.Hour)
	operation := signals.ExchangeImportOperation{Token: "signals-import-one", OccurredAt: updatedAt.Add(time.Hour)}
	denied := errors.New("imported record is write locked")
	gate := &signalsExchangeWriteGate{err: denied}
	auditor := &signalsExchangeAuditor{}
	service, importer, err := signals.NewServiceWithExchangeImporter(
		repository.NewMemorySignalsStore(), &evaluator{}, gate, auditor,
		signals.ServiceConfig{OrganizationID: "target-org", SubscriptionTargets: targetCatalog{
			{TargetKind: "group", TargetID: "finance-owners", Label: "Finance owners"},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	rule := signals.Rule{
		ID: "rule-imported", Name: "Renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning,
		Enabled: true, ThresholdDays: []int{180, 60, 30}, Revision: 8, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	result, err := importer.ImportRule(context.Background(), operation, rule)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import rule: result=%#v err=%v", result, err)
	}
	subscription := signals.Subscription{
		ID: "subscription-imported", RuleID: rule.ID, TargetKind: "group", TargetID: "finance-owners", Enabled: false,
		Revision: 5, CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt,
	}
	result, err = importer.ImportSubscription(context.Background(), operation, subscription)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import subscription: result=%#v err=%v", result, err)
	}
	snapshot, err := service.ExchangeSnapshot(context.Background(), 2)
	if err != nil || len(snapshot.Rules) != 1 || len(snapshot.Subscriptions) != 1 || snapshot.Rules[0].Revision != 8 || snapshot.Subscriptions[0].Revision != 5 ||
		!snapshot.Rules[0].CreatedAt.Equal(createdAt) || !snapshot.Subscriptions[0].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("lossy Signals snapshot %#v err=%v", snapshot, err)
	}
	if _, err := service.ExchangeSnapshot(context.Background(), 1); !errors.Is(err, signals.ErrTooLarge) {
		t.Fatalf("bounded snapshot accepted too many rows: %v", err)
	}
	replay, err := importer.ImportRule(context.Background(), operation, rule)
	if err != nil || !replay.Committed || replay.Created || len(auditor.events) != 2 || len(auditor.attempts) != 3 || auditor.attempts[0].ID != auditor.attempts[2].ID {
		t.Fatalf("deterministic replay failed: result=%#v events=%#v attempts=%#v err=%v", replay, auditor.events, auditor.attempts, err)
	}
	for _, event := range auditor.events {
		if event.ActorID != "system:exchange" || event.CorrelationID != operation.Token || !event.OccurredAt.Equal(operation.OccurredAt) || event.OrganizationID != "target-org" {
			t.Fatalf("invalid Exchange audit provenance %#v", event)
		}
	}
	changed := rule
	changed.Name = "Conflicting rule"
	if _, err := importer.ImportRule(context.Background(), operation, changed); !errors.Is(err, signals.ErrConflict) {
		t.Fatalf("conflicting replay was accepted: %v", err)
	}
	if _, err := service.UpdateRule(context.Background(), rule.ID, signals.UpdateRuleInput{
		Name: rule.Name, Condition: rule.Condition, Severity: rule.Severity, Enabled: rule.Enabled,
		ThresholdDays: rule.ThresholdDays, Revision: rule.Revision,
	}); !errors.Is(err, denied) {
		t.Fatalf("ordinary rule write bypassed ownership fence: %v", err)
	}
	if _, err := service.DeleteSubscription(context.Background(), subscription.ID); !errors.Is(err, denied) {
		t.Fatalf("ordinary subscription delete bypassed ownership fence: %v", err)
	}
	if want := []string{"signals.rule:rule-imported", "signals.subscription:subscription-imported"}; !reflect.DeepEqual(gate.calls, want) {
		t.Fatalf("unexpected write-gate calls got=%#v want=%#v", gate.calls, want)
	}
}

func TestSignalsExchangeImporterRejectsMissingRuleAndReachTarget(t *testing.T) {
	instant := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	service, importer, err := signals.NewServiceWithExchangeImporter(
		repository.NewMemorySignalsStore(), &evaluator{}, nil, foundation.NopAuditor{},
		signals.ServiceConfig{OrganizationID: "target-org", SubscriptionTargets: targetCatalog{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = service
	operation := signals.ExchangeImportOperation{Token: "signals-import-two", OccurredAt: instant}
	missingRule := signals.Subscription{ID: "subscription-one", RuleID: "rule-missing", TargetKind: "group", TargetID: "group-missing", Enabled: true, Revision: 1, CreatedAt: instant, UpdatedAt: instant}
	if _, err := importer.ImportSubscription(context.Background(), operation, missingRule); !errors.Is(err, signals.ErrReferenceMissing) {
		t.Fatalf("missing rule was not reported as a reference: %v", err)
	}
	missingTarget := missingRule
	missingTarget.RuleID = ""
	if _, err := importer.ImportSubscription(context.Background(), operation, missingTarget); !errors.Is(err, signals.ErrReferenceMissing) {
		t.Fatalf("missing Reach target was not reported as a reference: %v", err)
	}
	invalid := signals.Rule{ID: "rule-one", Name: "Renewal", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning, Enabled: true, ThresholdDays: []int{30}, Revision: 1, CreatedAt: instant, UpdatedAt: instant.Add(time.Minute)}
	if _, err := importer.ImportRule(context.Background(), operation, invalid); !errors.Is(err, signals.ErrInvalidInput) {
		t.Fatalf("revision-one timestamp drift was accepted: %v", err)
	}
}
