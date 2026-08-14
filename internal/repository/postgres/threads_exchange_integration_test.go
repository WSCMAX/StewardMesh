package postgres

// Requirements: REQ-EXCHANGE-001, REQ-THREADS-001. Features: migration.packages, goals.tags. GitHub: #9.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/bootstrap"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type threadsExchangeTargets struct{}

func (threadsExchangeTargets) ValidateThreadTarget(context.Context, string, threads.TargetType, string) error {
	return nil
}

func TestThreadsExchangeImporterPostgresIntegration(t *testing.T) {
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
	organizationID := fmt.Sprintf("threads-exchange-%d", time.Now().UnixNano())
	organizations, err := NewOrganizationRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	organizationService, err := bootstrap.NewOrganizationService(organizations)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := organizationService.EnsureOrganization(ctx, organizationID, "Threads Exchange Integration"); err != nil {
		t.Fatal(err)
	}
	store, err := NewThreadsStore(database)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := NewAuditor(database)
	if err != nil {
		t.Fatal(err)
	}
	service, importer, err := threads.NewServiceWithExchangeImporter(
		store, threadsExchangeTargets{}, nil, auditor, threads.ServiceConfig{OrganizationID: organizationID},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := threads.ExchangeImportOperation{Token: "threads-postgres-import", OccurredAt: time.Date(2026, time.August, 13, 23, 30, 0, 0, time.UTC)}
	tag := threads.Tag{ID: "portable-tag", Name: "Portable tag", InheritByDefault: true, Revision: 7}
	goal := threads.Goal{ID: "portable-goal", Name: "Portable goal", Description: "Portable strategy", Revision: 5}
	rule := threads.TagRule{TargetType: threads.TargetGoal, TargetID: goal.ID, TagID: tag.ID, Mode: threads.RuleSuppress, Revision: 9}
	link := threads.GoalLink{TargetType: threads.TargetAsset, TargetID: "portable-asset", GoalID: goal.ID}
	imports := []struct {
		name string
		run  func() (threads.ExchangeImportResult, error)
	}{
		{name: "tag", run: func() (threads.ExchangeImportResult, error) { return importer.ImportTag(ctx, operation, tag) }},
		{name: "goal", run: func() (threads.ExchangeImportResult, error) { return importer.ImportGoal(ctx, operation, goal) }},
		{name: "tag rule", run: func() (threads.ExchangeImportResult, error) { return importer.ImportTagRule(ctx, operation, rule) }},
		{name: "goal link", run: func() (threads.ExchangeImportResult, error) { return importer.ImportGoalLink(ctx, operation, link) }},
	}
	for _, item := range imports {
		result, err := item.run()
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import PostgreSQL Threads %s: %#v err=%v", item.name, result, err)
		}
	}
	snapshot, err := service.Snapshot(ctx)
	if err != nil || len(snapshot.Tags) != 1 || len(snapshot.Goals) != 1 || len(snapshot.TagRules) != 1 || len(snapshot.GoalLinks) != 1 {
		t.Fatalf("unexpected PostgreSQL Threads snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Tags[0].OrganizationID != organizationID || snapshot.Tags[0].Revision != tag.Revision ||
		snapshot.Goals[0].Revision != goal.Revision || snapshot.TagRules[0].Revision != rule.Revision ||
		!snapshot.Tags[0].CreatedAt.Equal(operation.OccurredAt) || !snapshot.TagRules[0].UpdatedAt.Equal(operation.OccurredAt) ||
		snapshot.TagRules[0].UpdatedBy != "system:exchange" || snapshot.GoalLinks[0].CreatedBy != "system:exchange" {
		t.Fatalf("PostgreSQL Threads import was not lossless: %#v", snapshot)
	}
	var auditCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 {
		t.Fatalf("expected four PostgreSQL Threads import audits, got %d", auditCount)
	}
	for _, item := range imports {
		result, err := item.run()
		if err != nil || !result.Committed || result.Created {
			t.Fatalf("replay PostgreSQL Threads %s: %#v err=%v", item.name, result, err)
		}
	}
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE organization_id=$1 AND correlation_id=$2`, organizationID, operation.Token).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 {
		t.Fatalf("PostgreSQL Threads replay duplicated audits: %d", auditCount)
	}
}
