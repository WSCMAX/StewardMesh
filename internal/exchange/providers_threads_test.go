package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-THREADS-001, REQ-PATTERNS-001. Features: migration.packages, goals.tags, templates.schemas. GitHub: #9.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/threads"
)

type threadsProviderTargets struct{ reject bool }

func (v threadsProviderTargets) ValidateThreadTarget(context.Context, string, threads.TargetType, string) error {
	if v.reject {
		return threads.ErrNotFound
	}
	return nil
}

func newThreadsProviderService(t *testing.T, organizationID string, targets threads.TargetValidator) (*threads.Service, threads.ExchangeImporter) {
	t.Helper()
	service, importer, err := threads.NewServiceWithExchangeImporter(
		repository.NewMemoryThreadsStore(), targets, nil, foundation.NopAuditor{},
		threads.ServiceConfig{OrganizationID: organizationID, Now: func() time.Time {
			return time.Date(2026, time.August, 13, 16, 0, 0, 0, time.UTC)
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return service, importer
}

func TestThreadsProviderRoundTripPreservesPortableFieldsRevisionsAndDependencies(t *testing.T) {
	ctx := context.Background()
	sourceService, sourceImporter := newThreadsProviderService(t, "threads-source", threadsProviderTargets{})
	if _, err := exchange.NewThreadsProvider(sourceService, nil); err == nil {
		t.Fatal("expected Threads provider to require its opaque importer")
	}
	provider, err := exchange.NewThreadsProvider(sourceService, sourceImporter)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := sourceService.CreateTag(ctx, threads.CreateTagInput{ID: "tag-parent", Name: "Governance", InheritByDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := sourceService.CreateTag(ctx, threads.CreateTagInput{ID: "tag-child", Name: "Security", ParentID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	child, err = sourceService.UpdateTag(ctx, threads.UpdateTagInput{ID: child.ID, Name: "Security controls", ParentID: parent.ID, Revision: child.Revision})
	if err != nil || child.Revision != 2 {
		t.Fatalf("prepare Threads tag: %#v err=%v", child, err)
	}
	goal, err := sourceService.CreateGoal(ctx, threads.CreateGoalInput{ID: "goal-portable", Name: "Reduce risk", Description: "Reduce operational risk"})
	if err != nil {
		t.Fatal(err)
	}
	goal, err = sourceService.UpdateGoal(ctx, threads.UpdateGoalInput{ID: goal.ID, Name: goal.Name, Description: "Reduce measurable operational risk", Revision: goal.Revision})
	if err != nil || goal.Revision != 2 {
		t.Fatalf("prepare Threads goal: %#v err=%v", goal, err)
	}
	rule, err := sourceService.SetTagRule(ctx, threads.SetTagRuleInput{TargetType: threads.TargetAsset, TargetID: "asset-portable", TagID: child.ID, Mode: threads.RuleInclude})
	if err != nil {
		t.Fatal(err)
	}
	rule, err = sourceService.SetTagRule(ctx, threads.SetTagRuleInput{TargetType: rule.TargetType, TargetID: rule.TargetID, TagID: rule.TagID, Mode: threads.RuleSuppress, Revision: rule.Revision})
	if err != nil || rule.Revision != 2 {
		t.Fatalf("prepare Threads tag rule: %#v err=%v", rule, err)
	}
	link, err := sourceService.LinkGoal(ctx, threads.LinkGoalInput{GoalID: goal.ID, TargetType: threads.TargetAsset, TargetID: "asset-portable"})
	if err != nil {
		t.Fatal(err)
	}

	records, err := provider.ListRecords(ctx)
	if err != nil || len(records) != 5 {
		t.Fatalf("list Threads provider records: %#v err=%v", records, err)
	}
	if got, want := provider.Types(), []string{"threads.tag", "threads.goal", "threads.tag-rule", "threads.goal-link"}; !slices.Equal(got, want) {
		t.Fatalf("unexpected Threads provider registry %#v", got)
	}
	byTypeID := make(map[string]exchange.Record, len(records))
	for _, record := range records {
		byTypeID[record.Type+":"+record.ID] = record
		for _, forbidden := range [][]byte{[]byte("organizationId"), []byte("createdAt"), []byte("updatedAt"), []byte("createdBy"), []byte("updatedBy")} {
			if bytes.Contains(record.Payload, forbidden) {
				t.Fatalf("Threads payload leaked destination-local state %q: %s", forbidden, record.Payload)
			}
		}
	}
	parentRecord := byTypeID["threads.tag:"+parent.ID]
	childRecord := byTypeID["threads.tag:"+child.ID]
	goalRecord := byTypeID["threads.goal:"+goal.ID]
	ruleRecord := byTypeID["threads.tag-rule:"+threads.TagRuleRecordID(rule.TargetType, rule.TargetID, rule.TagID)]
	linkRecord := byTypeID["threads.goal-link:"+threads.GoalLinkRecordID(link.TargetType, link.TargetID, link.GoalID)]
	if childRecord.Revision != child.Revision || !slices.Equal(childRecord.Dependencies, []exchange.Reference{{Type: "threads.tag", ID: parent.ID}}) {
		t.Fatalf("unexpected child tag projection %#v", childRecord)
	}
	if goalRecord.Revision != goal.Revision || len(goalRecord.Dependencies) != 0 {
		t.Fatalf("unexpected goal projection %#v", goalRecord)
	}
	if ruleRecord.Revision != rule.Revision || !slices.Equal(ruleRecord.Dependencies, []exchange.Reference{{Type: "atlas.asset", ID: "asset-portable"}, {Type: "threads.tag", ID: child.ID}}) {
		t.Fatalf("unexpected tag-rule dependencies %#v", ruleRecord)
	}
	if linkRecord.Revision != 1 || !slices.Equal(linkRecord.Dependencies, []exchange.Reference{{Type: "atlas.asset", ID: "asset-portable"}, {Type: "threads.goal", ID: goal.ID}}) {
		t.Fatalf("unexpected goal-link dependencies %#v", linkRecord)
	}

	targetService, targetImporter := newThreadsProviderService(t, "threads-target", threadsProviderTargets{})
	targetProvider, err := exchange.NewThreadsProvider(targetService, targetImporter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchange.NewThreadsProvider(sourceService, targetImporter); err == nil {
		t.Fatal("expected Threads provider to reject an importer owned by another service")
	}
	ordered := []exchange.Record{parentRecord, childRecord, goalRecord, ruleRecord, linkRecord}
	for index, record := range ordered {
		result, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{
			Token: "threads-provider-" + record.ID, OccurredAt: time.Date(2026, time.August, 13, 17, index, 0, 0, time.UTC), ExpectedCreated: true,
		}, "threads-source-system", record, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import Threads record %s: %#v err=%v", record.ID, result, err)
		}
		if exact, err := targetProvider.ImportRecordExists(ctx, record, nil); err != nil || !exact {
			t.Fatalf("Threads exact readback %s exact=%t err=%v", record.ID, exact, err)
		}
	}
	snapshot, err := targetService.Snapshot(ctx)
	if err != nil || len(snapshot.Tags) != 2 || len(snapshot.Goals) != 1 || len(snapshot.TagRules) != 1 || len(snapshot.GoalLinks) != 1 {
		t.Fatalf("unexpected target Threads snapshot %#v err=%v", snapshot, err)
	}
	if snapshot.Tags[1].Revision != child.Revision || snapshot.Goals[0].Revision != goal.Revision || snapshot.TagRules[0].Revision != rule.Revision ||
		snapshot.TagRules[0].Mode != rule.Mode || snapshot.GoalLinks[0].GoalID != link.GoalID {
		t.Fatalf("Threads provider did not round trip losslessly: %#v", snapshot)
	}
	for _, record := range ordered {
		replay, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{ExpectedCreated: false}, "threads-source-system", record, nil)
		if err != nil || !replay.Committed || replay.Created {
			t.Fatalf("replay Threads record %s: %#v err=%v", record.ID, replay, err)
		}
	}
}

func TestThreadsProviderStrictlyRejectsEnvelopePayloadAndDependencyMismatch(t *testing.T) {
	service, importer := newThreadsProviderService(t, "threads-strict", threadsProviderTargets{})
	provider, err := exchange.NewThreadsProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	valid := exchange.Record{
		Type: "threads.tag", ID: "strict-tag", Revision: 7, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"name":"Strict tag","inheritance":"include"}`),
	}
	for name, mutate := range map[string]func(exchange.Record) exchange.Record{
		"unknown field": func(record exchange.Record) exchange.Record {
			record.Payload = []byte(`{"name":"Strict tag","inheritance":"include","organizationId":"foreign"}`)
			return record
		},
		"noncanonical name": func(record exchange.Record) exchange.Record {
			record.Payload = []byte(`{"name":" Strict tag ","inheritance":"include"}`)
			return record
		},
		"trailing json": func(record exchange.Record) exchange.Record {
			record.Payload = append(record.Payload, []byte(` {}`)...)
			return record
		},
		"noncanonical top-level json": func(record exchange.Record) exchange.Record {
			record.Payload = append([]byte(" "), record.Payload...)
			return record
		},
		"wrong dependency": func(record exchange.Record) exchange.Record {
			record.Dependencies = []exchange.Reference{{Type: "threads.tag", ID: "unexpected"}}
			return record
		},
		"invalid id":    func(record exchange.Record) exchange.Record { record.ID = "invalid id"; return record },
		"zero revision": func(record exchange.Record) exchange.Record { record.Revision = 0; return record },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{
				Token: "threads-strict-record", OccurredAt: time.Date(2026, time.August, 13, 16, 30, 0, 0, time.UTC), ExpectedCreated: true,
			}, "source", mutate(valid), nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("expected strict Threads rejection, got %v", err)
			}
		})
	}

	rule := exchange.Record{
		Type: "threads.tag-rule", ID: threads.TagRuleRecordID(threads.TargetAsset, "asset-one", "tag-one"), Revision: 3,
		Dependencies: []exchange.Reference{{Type: "atlas.asset", ID: "asset-one"}, {Type: "threads.tag", ID: "tag-one"}},
		Payload:      []byte(`{"targetType":"asset","targetId":"asset-one","tagId":"tag-one","rule":"include"}`),
	}
	rule.ID = threads.TagRuleRecordID(threads.TargetAsset, "asset-other", "tag-one")
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{ExpectedCreated: true}, "source", rule, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("expected compound Threads identity mismatch rejection, got %v", err)
	}
}

func TestThreadsProviderMapsMissingHierarchyAndTargetDependencies(t *testing.T) {
	service, importer := newThreadsProviderService(t, "threads-missing", threadsProviderTargets{reject: true})
	provider, err := exchange.NewThreadsProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	child := exchange.Record{
		Type: "threads.tag", ID: "child", Revision: 2,
		Dependencies: []exchange.Reference{{Type: "threads.tag", ID: "missing-parent"}},
		Payload:      []byte(`{"name":"Child","parentId":"missing-parent","inheritance":"include"}`),
	}
	if result, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{
		Token: "threads-missing-parent", OccurredAt: time.Date(2026, time.August, 13, 18, 0, 0, 0, time.UTC), ExpectedCreated: true,
	}, "source", child, nil); !errors.Is(err, exchange.ErrDependencyMissing) || result.Committed {
		t.Fatalf("expected missing Threads parent dependency: %#v err=%v", result, err)
	}
}
