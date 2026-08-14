package contracttest

// Provider-neutral Threads adapter contract. Requirement: REQ-THREADS-001.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/threads"
)

func ThreadsStore(t testing.TB, subject threads.Store, organizationID, suffix string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	parentID, childID, goalID := "tag-parent-"+suffix, "tag-child-"+suffix, "goal-"+suffix
	if _, err := subject.GetTag(ctx, organizationID, parentID); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("expected missing tag, got %v", err)
	}
	parent := threads.Tag{ID: parentID, OrganizationID: organizationID, Name: "Governance " + suffix, InheritByDefault: true, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := subject.CreateTag(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.CreateTag(ctx, parent); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected duplicate tag conflict, got %v", err)
	}
	child := threads.Tag{ID: childID, OrganizationID: organizationID, Name: "Security " + suffix, ParentID: parentID, InheritByDefault: true, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := subject.CreateTag(ctx, child); err != nil {
		t.Fatal(err)
	}
	parent.Name = "Governance updated " + suffix
	parent.Revision = 2
	parent.UpdatedAt = now.Add(time.Minute)
	if _, err := subject.UpdateTag(ctx, parent, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.UpdateTag(ctx, parent, 1); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected stale tag conflict, got %v", err)
	}
	tags, err := subject.ListTags(ctx, organizationID)
	if err != nil || len(tags) != 2 {
		t.Fatalf("unexpected tags %#v err=%v", tags, err)
	}

	goal := threads.Goal{ID: goalID, OrganizationID: organizationID, Name: "Reduce risk " + suffix, Description: "Contract goal", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := subject.CreateGoal(ctx, goal); err != nil {
		t.Fatal(err)
	}
	goal.Description = "Updated goal"
	goal.Revision = 2
	goal.UpdatedAt = now.Add(time.Minute)
	if _, err := subject.UpdateGoal(ctx, goal, 1); err != nil {
		t.Fatal(err)
	}

	rule := threads.TagRule{OrganizationID: organizationID, TargetType: threads.TargetAsset, TargetID: "asset-" + suffix, TagID: childID, Mode: threads.RuleInclude, Revision: 1, UpdatedBy: "contract-user", CreatedAt: now, UpdatedAt: now}
	createdRule, err := subject.PutTagRule(ctx, rule, 0)
	if err != nil || createdRule.Revision != 1 {
		t.Fatalf("unexpected tag rule %#v err=%v", createdRule, err)
	}
	rule.Mode, rule.Revision, rule.UpdatedAt = threads.RuleSuppress, 2, now.Add(time.Minute)
	if _, err := subject.PutTagRule(ctx, rule, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := subject.PutTagRule(ctx, rule, 1); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected stale rule conflict, got %v", err)
	}
	rules, err := subject.ListTagRules(ctx, organizationID, threads.TargetAsset, rule.TargetID)
	if err != nil || len(rules) != 1 || rules[0].Mode != threads.RuleSuppress {
		t.Fatalf("unexpected rules %#v err=%v", rules, err)
	}
	importRule := threads.TagRule{OrganizationID: organizationID, TargetType: threads.TargetGoal, TargetID: goalID, TagID: childID,
		Mode: threads.RuleInclude, Revision: 7, UpdatedBy: "system:exchange", CreatedAt: now, UpdatedAt: now}
	if created, err := subject.CreateTagRule(ctx, importRule); err != nil || created.Revision != importRule.Revision {
		t.Fatalf("create arbitrary-revision Threads rule: %#v err=%v", created, err)
	}
	if _, err := subject.CreateTagRule(ctx, importRule); !errors.Is(err, threads.ErrConflict) {
		t.Fatalf("expected duplicate imported Threads rule conflict, got %v", err)
	}
	if err := subject.DeleteTagRule(ctx, organizationID, threads.TargetAsset, rule.TargetID, childID, 2); err != nil {
		t.Fatal(err)
	}

	link := threads.GoalLink{OrganizationID: organizationID, GoalID: goalID, TargetType: threads.TargetAsset, TargetID: "asset-" + suffix, CreatedBy: "contract-user", CreatedAt: now}
	if _, inserted, err := subject.CreateGoalLink(ctx, link); err != nil || !inserted {
		t.Fatalf("unexpected goal link insertion inserted=%t err=%v", inserted, err)
	}
	if _, inserted, err := subject.CreateGoalLink(ctx, link); err != nil || inserted {
		t.Fatalf("expected idempotent goal link inserted=%t err=%v", inserted, err)
	}
	links, err := subject.ListGoalLinks(ctx, organizationID, threads.TargetAsset, link.TargetID)
	if err != nil || len(links) != 1 || links[0].GoalID != goalID {
		t.Fatalf("unexpected goal links %#v err=%v", links, err)
	}
	snapshot, err := subject.Snapshot(ctx, organizationID)
	if err != nil || len(snapshot.Tags) != 2 || len(snapshot.Goals) != 1 || len(snapshot.TagRules) != 1 || len(snapshot.GoalLinks) != 1 || snapshot.TagRules[0].Revision != 7 {
		t.Fatalf("unexpected Threads snapshot %#v err=%v", snapshot, err)
	}
	if removed, err := subject.DeleteGoalLink(ctx, organizationID, threads.TargetAsset, link.TargetID, goalID); err != nil || !removed {
		t.Fatalf("unexpected goal unlink removed=%t err=%v", removed, err)
	}
}
