package threads_test

// Requirements: REQ-THREADS-001, REQ-EXCHANGE-001. Features: goals.tags, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type threadsExchangeAuditor struct {
	events   map[string]foundation.AuditEvent
	failNext error
}

func (a *threadsExchangeAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	if a.failNext != nil {
		err := a.failNext
		a.failNext = nil
		return err
	}
	if a.events == nil {
		a.events = make(map[string]foundation.AuditEvent)
	}
	if existing, exists := a.events[event.ID]; exists {
		if !reflect.DeepEqual(existing, event) {
			return errors.New("audit event id conflicts with different immutable content")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}

type threadsExchangeWriteGate struct {
	err      error
	requests [][2]string
}

func (g *threadsExchangeWriteGate) CheckResourceWrite(_ context.Context, recordType, recordID string) error {
	g.requests = append(g.requests, [2]string{recordType, recordID})
	return g.err
}

func newThreadsExchangeService(t *testing.T, organizationID string, auditor foundation.Auditor, gate threads.WriteGate) (*threads.Service, threads.ExchangeImporter, *repository.MemoryThreadsStore) {
	t.Helper()
	store := repository.NewMemoryThreadsStore()
	service, importer, err := threads.NewServiceWithExchangeImporter(store, targetValidator{}, gate, auditor, threads.ServiceConfig{OrganizationID: organizationID})
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, store
}

func TestExchangeImporterPreservesThreadsRevisionsRelationshipsAndDeterministicAudits(t *testing.T) {
	auditor := &threadsExchangeAuditor{}
	service, importer, store := newThreadsExchangeService(t, "threads-target", auditor, nil)
	operation := threads.ExchangeImportOperation{Token: "threads-portable-import", OccurredAt: time.Date(2026, time.August, 13, 19, 0, 0, 0, time.UTC)}
	tag := threads.Tag{ID: "tag-portable", Name: "Portable tag", InheritByDefault: true, Revision: 7}
	goal := threads.Goal{ID: "goal-portable", Name: "Portable goal", Description: "Portable strategy", Revision: 5}
	rule := threads.TagRule{TargetType: threads.TargetGoal, TargetID: goal.ID, TagID: tag.ID, Mode: threads.RuleSuppress, Revision: 9}
	link := threads.GoalLink{TargetType: threads.TargetPurchase, TargetID: "purchase-portable", GoalID: goal.ID}

	imports := []struct {
		name string
		run  func() (threads.ExchangeImportResult, error)
	}{
		{name: "tag", run: func() (threads.ExchangeImportResult, error) {
			return importer.ImportTag(context.Background(), operation, tag)
		}},
		{name: "goal", run: func() (threads.ExchangeImportResult, error) {
			return importer.ImportGoal(context.Background(), operation, goal)
		}},
		{name: "tag rule", run: func() (threads.ExchangeImportResult, error) {
			return importer.ImportTagRule(context.Background(), operation, rule)
		}},
		{name: "goal link", run: func() (threads.ExchangeImportResult, error) {
			return importer.ImportGoalLink(context.Background(), operation, link)
		}},
	}
	for _, item := range imports {
		result, err := item.run()
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s: %#v err=%v", item.name, result, err)
		}
	}
	snapshot, err := store.Snapshot(context.Background(), "threads-target")
	if err != nil || len(snapshot.Tags) != 1 || len(snapshot.Goals) != 1 || len(snapshot.TagRules) != 1 || len(snapshot.GoalLinks) != 1 {
		t.Fatalf("unexpected Threads import snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Tags[0].Revision != tag.Revision || snapshot.Goals[0].Revision != goal.Revision || snapshot.TagRules[0].Revision != rule.Revision ||
		!snapshot.Tags[0].CreatedAt.Equal(operation.OccurredAt) || !snapshot.Goals[0].UpdatedAt.Equal(operation.OccurredAt) ||
		snapshot.TagRules[0].UpdatedBy != "system:exchange" || snapshot.GoalLinks[0].CreatedBy != "system:exchange" {
		t.Fatalf("Threads importer lost revision or deterministic local provenance: %#v", snapshot)
	}
	if len(auditor.events) != 4 {
		t.Fatalf("expected one audit per imported Threads record, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "threads-target" || event.ActorID != "system:exchange" || event.CorrelationID != operation.Token ||
			!event.OccurredAt.Equal(operation.OccurredAt) || event.Metadata["requirementId"] != threads.RequirementID {
			t.Fatalf("unexpected deterministic Threads audit %#v", event)
		}
	}

	for _, item := range imports {
		result, err := item.run()
		if err != nil || !result.Committed || result.Created {
			t.Fatalf("replay %s: %#v err=%v", item.name, result, err)
		}
	}
	if len(auditor.events) != 4 {
		t.Fatalf("Threads replay duplicated deterministic audits: %#v", auditor.events)
	}
	changed := tag
	changed.Name = "Changed tag"
	if _, err := importer.ImportTag(context.Background(), operation, changed); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected changed Threads replay conflict, got %v", err)
	}
	if !service.OwnsExchangeImporter(importer) {
		t.Fatal("Threads service rejected its own importer")
	}
}

func TestExchangeImporterReportsCommittedAuditFailureAndRepairsIt(t *testing.T) {
	auditFailure := errors.New("audit unavailable")
	auditor := &threadsExchangeAuditor{failNext: auditFailure}
	_, importer, store := newThreadsExchangeService(t, "threads-repair", auditor, nil)
	operation := threads.ExchangeImportOperation{Token: "threads-audit-repair", OccurredAt: time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)}
	candidate := threads.Tag{ID: "repair-tag", Name: "Repair tag", Revision: 11}

	result, err := importer.ImportTag(context.Background(), operation, candidate)
	if !errors.Is(err, auditFailure) || !result.Committed || !result.Created {
		t.Fatalf("expected truthful post-commit audit failure: %#v err=%v", result, err)
	}
	if stored, readErr := store.GetTag(context.Background(), "threads-repair", candidate.ID); readErr != nil || stored.Revision != candidate.Revision {
		t.Fatalf("Threads importer lost committed record: %#v err=%v", stored, readErr)
	}
	repaired, err := importer.ImportTag(context.Background(), operation, candidate)
	if err != nil || !repaired.Committed || repaired.Created || len(auditor.events) != 1 {
		t.Fatalf("Threads audit repair did not converge: %#v audits=%#v err=%v", repaired, auditor.events, err)
	}
}

func TestThreadsOrdinaryWritesUseImportedResourceFenceWhileImporterBypassesIt(t *testing.T) {
	locked := errors.New("resource is externally write-locked")
	gate := &threadsExchangeWriteGate{}
	service, importer, store := newThreadsExchangeService(t, "threads-gated", foundation.NopAuditor{}, gate)
	operation := threads.ExchangeImportOperation{Token: "threads-gated-import", OccurredAt: time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC)}
	tag := threads.Tag{ID: "gated-tag", Name: "Gated tag", Revision: 3}
	goal := threads.Goal{ID: "gated-goal", Name: "Gated goal", Revision: 4}
	if result, err := importer.ImportTag(context.Background(), operation, tag); err != nil || !result.Created {
		t.Fatalf("import gated tag: %#v err=%v", result, err)
	}
	if result, err := importer.ImportGoal(context.Background(), operation, goal); err != nil || !result.Created {
		t.Fatalf("import gated goal: %#v err=%v", result, err)
	}
	if len(gate.requests) != 0 {
		t.Fatalf("opaque importer unexpectedly traversed ordinary gate: %#v", gate.requests)
	}
	gate.err = locked
	if _, err := service.UpdateTag(context.Background(), threads.UpdateTagInput{ID: tag.ID, Name: tag.Name, InheritByDefault: tag.InheritByDefault, Revision: tag.Revision}); !errors.Is(err, locked) {
		t.Fatalf("expected tag ownership fence, got %v", err)
	}
	if _, err := service.UpdateGoal(context.Background(), threads.UpdateGoalInput{ID: goal.ID, Name: goal.Name, Revision: goal.Revision}); !errors.Is(err, locked) {
		t.Fatalf("expected goal ownership fence, got %v", err)
	}
	if _, err := service.SetTagRule(context.Background(), threads.SetTagRuleInput{TargetType: threads.TargetGoal, TargetID: goal.ID, TagID: tag.ID, Mode: threads.RuleInclude}); !errors.Is(err, locked) {
		t.Fatalf("expected tag-rule ownership fence, got %v", err)
	}
	if _, err := service.LinkGoal(context.Background(), threads.LinkGoalInput{TargetType: threads.TargetAsset, TargetID: "asset-gated", GoalID: goal.ID}); !errors.Is(err, locked) {
		t.Fatalf("expected goal-link ownership fence, got %v", err)
	}
	want := [][2]string{
		{"threads.tag", tag.ID},
		{"threads.goal", goal.ID},
		{"threads.tag-rule", threads.TagRuleRecordID(threads.TargetGoal, goal.ID, tag.ID)},
		{"threads.goal-link", threads.GoalLinkRecordID(threads.TargetAsset, "asset-gated", goal.ID)},
	}
	if !reflect.DeepEqual(gate.requests, want) {
		t.Fatalf("unexpected Threads write-gate calls: got %#v want %#v", gate.requests, want)
	}
	snapshot, err := store.Snapshot(context.Background(), "threads-gated")
	if err != nil || len(snapshot.Tags) != 1 || len(snapshot.Goals) != 1 || len(snapshot.TagRules) != 0 || len(snapshot.GoalLinks) != 0 {
		t.Fatalf("denied Threads writes mutated state: %#v err=%v", snapshot, err)
	}
}

func TestThreadsDeterministicAuditIdentityIsOrganizationScoped(t *testing.T) {
	auditor := &threadsExchangeAuditor{}
	operation := threads.ExchangeImportOperation{Token: "threads-shared-token", OccurredAt: time.Date(2026, time.August, 13, 22, 0, 0, 0, time.UTC)}
	candidate := threads.Tag{ID: "shared-tag", Name: "Shared tag", Revision: 2}
	for _, organizationID := range []string{"threads-org-one", "threads-org-two"} {
		_, importer, _ := newThreadsExchangeService(t, organizationID, auditor, nil)
		if result, err := importer.ImportTag(context.Background(), operation, candidate); err != nil || !result.Created {
			t.Fatalf("import %s: %#v err=%v", organizationID, result, err)
		}
	}
	if len(auditor.events) != 2 {
		t.Fatalf("organization-scoped audit IDs collided: %#v", auditor.events)
	}
}
