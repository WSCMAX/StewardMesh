package threads_test

// Requirements: REQ-THREADS-001, A11Y-001, SEC-GUARD-001.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type targetValidator struct {
	reject bool
}

func (v targetValidator) ValidateThreadTarget(context.Context, string, threads.TargetType, string) error {
	if v.reject {
		return threads.ErrNotFound
	}
	return nil
}

type recordingAuditor struct {
	events []foundation.AuditEvent
}

func (a *recordingAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func TestServiceCreatesHierarchiesEvaluatesProvenanceAndLinksGoals(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	auditor := &recordingAuditor{}
	service, err := threads.NewService(repository.NewMemoryThreadsStore(), targetValidator{}, auditor, threads.ServiceConfig{
		OrganizationID: "example-org", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "example-org", ActorID: "account-one", CorrelationID: "request-one"})
	governance, err := service.CreateTag(ctx, threads.CreateTagInput{ID: "governance", Name: " Governance ", InheritByDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	security, err := service.CreateTag(ctx, threads.CreateTagInput{ID: "security", Name: "Security", ParentID: governance.ID, InheritByDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetTagRule(ctx, threads.SetTagRuleInput{TargetType: threads.TargetAsset, TargetID: "asset-one", TagID: security.ID, Mode: threads.RuleInclude}); err != nil {
		t.Fatal(err)
	}
	effective, err := service.EvaluateTags(ctx, threads.TargetAsset, "asset-one")
	if err != nil || len(effective) != 2 || effective[0].Tag.ID != governance.ID || effective[0].State != "inherited" || effective[0].SourceTagID != security.ID || effective[1].State != "explicit" {
		t.Fatalf("unexpected effective tags %#v err=%v", effective, err)
	}
	suppressed, err := service.SetTagRule(ctx, threads.SetTagRuleInput{TargetType: threads.TargetAsset, TargetID: "asset-one", TagID: governance.ID, Mode: threads.RuleSuppress})
	if err != nil {
		t.Fatal(err)
	}
	effective, err = service.EvaluateTags(ctx, threads.TargetAsset, "asset-one")
	if err != nil || len(effective) != 2 || effective[0].State != "suppressed" {
		t.Fatalf("expected visible suppression provenance, got %#v err=%v", effective, err)
	}
	if err := service.DeleteTagRule(ctx, threads.TargetAsset, "asset-one", governance.ID, suppressed.Revision); err != nil {
		t.Fatal(err)
	}
	goal, err := service.CreateGoal(ctx, threads.CreateGoalInput{ID: "reduce-risk", Name: "Reduce risk", Description: "Strategic objective"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LinkGoal(ctx, threads.LinkGoalInput{GoalID: goal.ID, TargetType: threads.TargetAsset, TargetID: "asset-one"}); err != nil {
		t.Fatal(err)
	}
	links, err := service.ListGoalLinks(ctx, threads.TargetAsset, "asset-one")
	if err != nil || len(links) != 1 || links[0].GoalID != goal.ID {
		t.Fatalf("unexpected goal links %#v err=%v", links, err)
	}
	if len(auditor.events) != 7 || auditor.events[0].Metadata["requirementId"] != threads.RequirementID {
		t.Fatalf("unexpected audit events %#v", auditor.events)
	}
}

func TestServiceRejectsCyclesStaleRevisionsAndMissingTargets(t *testing.T) {
	store := repository.NewMemoryThreadsStore()
	service, err := threads.NewService(store, targetValidator{}, foundation.NopAuditor{}, threads.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := service.CreateTag(context.Background(), threads.CreateTagInput{ID: "parent", Name: "Parent", InheritByDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.CreateTag(context.Background(), threads.CreateTagInput{ID: "child", Name: "Child", ParentID: parent.ID, InheritByDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateTag(context.Background(), threads.UpdateTagInput{ID: parent.ID, Name: parent.Name, ParentID: child.ID, InheritByDefault: true, Revision: parent.Revision}); !errors.Is(err, threads.ErrCycle) {
		t.Fatalf("expected tag cycle, got %v", err)
	}
	if _, err := service.UpdateTag(context.Background(), threads.UpdateTagInput{ID: child.ID, Name: child.Name, ParentID: parent.ID, InheritByDefault: true, Revision: 99}); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected stale revision, got %v", err)
	}
	rejected, err := threads.NewService(store, targetValidator{reject: true}, foundation.NopAuditor{}, threads.ServiceConfig{OrganizationID: "example-org"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rejected.SetTagRule(context.Background(), threads.SetTagRuleInput{TargetType: threads.TargetAsset, TargetID: "missing", TagID: child.ID, Mode: threads.RuleInclude}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("expected missing target, got %v", err)
	}
}

func TestServiceRequiresDependencies(t *testing.T) {
	if service, err := threads.NewService(nil, targetValidator{}, foundation.NopAuditor{}, threads.ServiceConfig{OrganizationID: "org"}); err == nil || service != nil {
		t.Fatalf("expected missing dependency failure, service=%T err=%v", service, err)
	}
}
