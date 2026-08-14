package postgres

// Requirements: REQ-EXCHANGE-001, REQ-SIGNALS-001.
// Features: migration.packages, alerts.rules. GitHub: #9, #11.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type signalsExchangeEvaluator struct{}

func (signalsExchangeEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return nil, nil
}

type signalsExchangeTargets []signals.SubscriptionTarget

func (c signalsExchangeTargets) ListSubscriptionTargets(context.Context, string) ([]signals.SubscriptionTarget, error) {
	return append([]signals.SubscriptionTarget(nil), c...), nil
}

func TestSignalsExchangeImporterPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("STEWARDMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("STEWARDMESH_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	organizationID := fmt.Sprintf("signals-exchange-%d", time.Now().UnixNano())
	organizations, _ := NewOrganizationRepository(database)
	organizationService, _ := bootstrap.NewOrganizationService(organizations)
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Signals Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewSignalsStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := signals.NewServiceWithExchangeImporter(
		store, signalsExchangeEvaluator{}, nil, auditor,
		signals.ServiceConfig{OrganizationID: organizationID, SubscriptionTargets: signalsExchangeTargets{
			{TargetKind: "webhook", TargetID: "operations-hook", Label: "Operations webhook"},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2023, time.March, 4, 5, 6, 7, 800_000_000, time.UTC)
	updatedAt := createdAt.Add(48 * time.Hour)
	operation := signals.ExchangeImportOperation{Token: "signals-postgres-import", OccurredAt: updatedAt.Add(time.Hour)}
	rule := signals.Rule{ID: "rule-portable", Name: "Renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning,
		Enabled: false, ThresholdDays: []int{365, 90, 30}, Revision: 11, CreatedAt: createdAt, UpdatedAt: updatedAt}
	if result, err := importer.ImportRule(ctx, operation, rule); err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Signals rule: %#v err=%v", result, err)
	}
	subscription := signals.Subscription{ID: "subscription-portable", RuleID: rule.ID, TargetKind: "webhook", TargetID: "operations-hook",
		Enabled: false, Revision: 7, CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt}
	if result, err := importer.ImportSubscription(ctx, operation, subscription); err != nil || !result.Committed || !result.Created {
		t.Fatalf("import PostgreSQL Signals subscription: %#v err=%v", result, err)
	}
	snapshot, err := service.ExchangeSnapshot(ctx, 2)
	if err != nil || len(snapshot.Rules) != 1 || len(snapshot.Subscriptions) != 1 || snapshot.Rules[0].Revision != 11 ||
		snapshot.Subscriptions[0].Revision != 7 || !snapshot.Subscriptions[0].CreatedAt.Equal(subscription.CreatedAt) ||
		!snapshot.Subscriptions[0].UpdatedAt.Equal(subscription.UpdatedAt) {
		t.Fatalf("PostgreSQL Signals snapshot was lossy: %#v err=%v", snapshot, err)
	}
	if replay, err := importer.ImportSubscription(ctx, operation, subscription); err != nil || !replay.Committed || replay.Created {
		t.Fatalf("replay PostgreSQL Signals subscription: %#v err=%v", replay, err)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 2 {
		t.Fatalf("Signals exact replay duplicated or omitted audits: %d", auditCount)
	}
}
