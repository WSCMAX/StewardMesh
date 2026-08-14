package exchange_test

// Requirements: REQ-EXCHANGE-001, REQ-SIGNALS-001, REQ-PATTERNS-001.
// Features: migration.packages, alerts.rules, templates.schemas. GitHub: #9, #11.

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

type signalsProviderEvaluator struct{}

func (signalsProviderEvaluator) Evaluate(context.Context, signals.Rule, time.Time) ([]signals.Candidate, error) {
	return nil, nil
}

type signalsProviderTargets []signals.SubscriptionTarget

func (c signalsProviderTargets) ListSubscriptionTargets(context.Context, string) ([]signals.SubscriptionTarget, error) {
	return append([]signals.SubscriptionTarget(nil), c...), nil
}

func newSignalsProvider(t *testing.T, organizationID string, targets signalsProviderTargets) (*signals.Service, signals.ExchangeImporter, *exchange.SignalsProvider) {
	t.Helper()
	service, importer, err := signals.NewServiceWithExchangeImporter(
		repository.NewMemorySignalsStore(), signalsProviderEvaluator{}, nil, foundation.NopAuditor{},
		signals.ServiceConfig{OrganizationID: organizationID, SubscriptionTargets: targets},
	)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := exchange.NewSignalsProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	return service, importer, provider
}

func TestSignalsProviderRoundTripPreservesPortableStateAndTypedReachDependencies(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2022, time.April, 3, 2, 1, 0, 987_000_000, time.UTC)
	updatedAt := createdAt.Add(72 * time.Hour)
	targets := signalsProviderTargets{{TargetKind: "group", TargetID: "finance-owners", Label: "Finance owners"}}
	_, sourceImporter, sourceProvider := newSignalsProvider(t, "source-org", targets)
	operation := signals.ExchangeImportOperation{Token: "seed-signals-provider", OccurredAt: updatedAt.Add(time.Hour)}
	rule := signals.Rule{
		ID: "renewal-rule", Name: "Contract renewals", Condition: signals.ConditionRenewal, Severity: signals.SeverityCritical,
		Enabled: false, ThresholdDays: []int{365, 90, 30}, Revision: 9, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	if result, err := sourceImporter.ImportRule(ctx, operation, rule); err != nil || !result.Created {
		t.Fatalf("seed source rule: %#v err=%v", result, err)
	}
	subscription := signals.Subscription{
		ID: "finance-renewals", RuleID: rule.ID, TargetKind: "group", TargetID: "finance-owners", Enabled: false,
		Revision: 4, CreatedAt: createdAt.Add(time.Hour), UpdatedAt: updatedAt,
	}
	if result, err := sourceImporter.ImportSubscription(ctx, operation, subscription); err != nil || !result.Created {
		t.Fatalf("seed source subscription: %#v err=%v", result, err)
	}
	if got, want := sourceProvider.Types(), []string{"signals.rule", "signals.subscription"}; !slices.Equal(got, want) {
		t.Fatalf("unexpected Signals provider types %#v", got)
	}
	records, err := sourceProvider.ListRecords(ctx)
	if err != nil || len(records) != 2 {
		t.Fatalf("list Signals records %#v err=%v", records, err)
	}
	if records[0].Type != "signals.rule" || records[0].Revision != 9 || records[1].Type != "signals.subscription" || records[1].Revision != 4 {
		t.Fatalf("unexpected Signals records %#v", records)
	}
	for _, record := range records {
		if bytes.Contains(record.Payload, []byte("organizationId")) || bytes.Contains(record.Payload, []byte("createdBy")) || bytes.Contains(record.Payload, []byte("system:exchange")) {
			t.Fatalf("Signals payload leaked deployment or operator state: %s", record.Payload)
		}
	}
	wantDependencies := []exchange.Reference{{Type: "reach.subscriber-group", ID: "finance-owners"}, {Type: "signals.rule", ID: "renewal-rule"}}
	if !slices.Equal(records[1].Dependencies, wantDependencies) {
		t.Fatalf("subscription dependencies got=%#v want=%#v", records[1].Dependencies, wantDependencies)
	}
	_, _, targetProvider := newSignalsProvider(t, "target-org", targets)
	for index, record := range records {
		result, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{
			Token: "import-signals-" + record.ID, OccurredAt: updatedAt.Add(time.Duration(index+2) * time.Hour), ExpectedCreated: true,
		}, "source-system", record, nil)
		if err != nil || !result.Committed || !result.Created {
			t.Fatalf("import %s: result=%#v err=%v", record.Type, result, err)
		}
	}
	for _, record := range records {
		if exact, err := targetProvider.ImportRecordExists(ctx, record, nil); err != nil || !exact {
			t.Fatalf("exact readback %s: exact=%t err=%v", record.Type, exact, err)
		}
		if replay, err := targetProvider.ImportRecord(ctx, exchange.ProviderImportOperation{ExpectedCreated: false}, "source-system", record, nil); err != nil || !replay.Committed || replay.Created {
			t.Fatalf("replay %s: %#v err=%v", record.Type, replay, err)
		}
	}
	targetRecords, err := targetProvider.ListRecords(ctx)
	if err != nil || !reflect.DeepEqual(records, targetRecords) {
		t.Fatalf("Signals re-export changed bytes/state\nsource=%#v\ntarget=%#v\nerr=%v", records, targetRecords, err)
	}
}

func TestSignalsProviderRejectsForeignCapabilityStrictPayloadsAndDependencyDrift(t *testing.T) {
	first, firstImporter, provider := newSignalsProvider(t, "first-org", signalsProviderTargets{{TargetKind: "webhook", TargetID: "operations-hook", Label: "Operations"}})
	_, secondImporter, _ := newSignalsProvider(t, "second-org", signalsProviderTargets{})
	if _, err := exchange.NewSignalsProvider(first, secondImporter); err == nil {
		t.Fatal("Signals provider accepted an importer from another service")
	}
	if _, err := exchange.NewSignalsProvider(first, firstImporter); err != nil {
		t.Fatal(err)
	}
	base := exchange.Record{Type: "signals.rule", ID: "rule-one", Revision: 1, Dependencies: []exchange.Reference{},
		Payload: []byte(`{"name":"Renewals","condition":"renewal","severity":"warning","enabled":"true","thresholdDays":"[30]","createdAt":"2026-08-13T12:00:00Z","updatedAt":"2026-08-13T12:00:00Z"}`)}
	for name, record := range map[string]exchange.Record{
		"unknown field": func() exchange.Record {
			value := base
			value.Payload = []byte(`{"name":"Renewals","condition":"renewal","severity":"warning","enabled":"true","thresholdDays":"[30]","createdAt":"2026-08-13T12:00:00Z","updatedAt":"2026-08-13T12:00:00Z","createdBy":"operator"}`)
			return value
		}(),
		"noncanonical thresholds": func() exchange.Record {
			value := base
			value.Payload = []byte(`{"name":"Renewals","condition":"renewal","severity":"warning","enabled":"true","thresholdDays":"[30,90]","createdAt":"2026-08-13T12:00:00Z","updatedAt":"2026-08-13T12:00:00Z"}`)
			return value
		}(),
		"revision one timestamp drift": func() exchange.Record {
			value := base
			value.Payload = []byte(`{"name":"Renewals","condition":"renewal","severity":"warning","enabled":"true","thresholdDays":"[30]","createdAt":"2026-08-13T12:00:00Z","updatedAt":"2026-08-13T12:01:00Z"}`)
			return value
		}(),
		"sub-microsecond timestamp": func() exchange.Record {
			value := base
			value.Payload = []byte(`{"name":"Renewals","condition":"renewal","severity":"warning","enabled":"true","thresholdDays":"[30]","createdAt":"2026-08-13T12:00:00.000000001Z","updatedAt":"2026-08-13T12:00:00.000000001Z"}`)
			return value
		}(),
		"dependency injection": func() exchange.Record {
			value := base
			value.Dependencies = []exchange.Reference{{Type: "reach.provider", ID: "operations-hook"}}
			return value
		}(),
		"noncanonical top-level json": func() exchange.Record {
			value := base
			value.Payload = append([]byte(" "), value.Payload...)
			return value
		}(),
		"invalid id": func() exchange.Record {
			value := base
			value.ID = "invalid id"
			return value
		}(),
		"zero revision": func() exchange.Record {
			value := base
			value.Revision = 0
			return value
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{
				Token: "signals-strict-record", OccurredAt: time.Date(2026, time.August, 13, 13, 0, 0, 0, time.UTC), ExpectedCreated: true,
			}, "source", record, nil); !errors.Is(err, exchange.ErrInvalidInput) {
				t.Fatalf("invalid Signals record was accepted: %v", err)
			}
		})
	}
	subscription := exchange.Record{Type: "signals.subscription", ID: "subscription-one", Revision: 1,
		Dependencies: []exchange.Reference{{Type: "reach.subscriber-group", ID: "operations-hook"}},
		Payload:      []byte(`{"targetKind":"webhook","targetId":"operations-hook","enabled":"true","createdAt":"2026-08-13T12:00:00Z","updatedAt":"2026-08-13T12:00:00Z"}`)}
	if _, err := provider.ImportRecord(context.Background(), exchange.ProviderImportOperation{ExpectedCreated: true}, "source", subscription, nil); !errors.Is(err, exchange.ErrInvalidInput) {
		t.Fatalf("mismatched Reach dependency kind was accepted: %v", err)
	}
}

func TestSignalsImporterAcceptsInertImportedReachProviderAndGroupReferences(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	reachStore := repository.NewMemoryReachStore()
	provider := reach.Provider{ID: "imported-hook", OrganizationID: "target-org", Name: "Imported hook", Kind: reach.ProviderWebhook,
		Enabled: false, Revision: 3, CreatedBy: "system:exchange", UpdatedBy: "system:exchange", CreatedAt: now, UpdatedAt: now.Add(time.Hour)}
	if _, created, err := reachStore.ImportProvider(ctx, provider); err != nil || !created {
		t.Fatalf("seed inert Reach provider: created=%t err=%v", created, err)
	}
	template := reach.Template{ID: "imported-template", OrganizationID: "target-org", Name: "Imported template", Subject: "{{title}}", Body: "{{summary}}",
		Revision: 2, CreatedBy: "system:exchange", UpdatedBy: "system:exchange", CreatedAt: now, UpdatedAt: now.Add(time.Hour)}
	if _, created, err := reachStore.ImportTemplate(ctx, template); err != nil || !created {
		t.Fatalf("seed imported Reach template: created=%t err=%v", created, err)
	}
	group := reach.SubscriberGroup{ID: "imported-group", OrganizationID: "target-org", Name: "Imported group", ProviderID: provider.ID, TemplateID: template.ID,
		Recipients: []reach.Recipient{{Kind: reach.RecipientEmail, Address: "finance@example.test"}}, Revision: 4,
		CreatedBy: "system:exchange", UpdatedBy: "system:exchange", CreatedAt: now, UpdatedAt: now.Add(time.Hour)}
	if _, created, err := reachStore.ImportGroup(ctx, group); err != nil || !created {
		t.Fatalf("seed inert Reach group: created=%t err=%v", created, err)
	}
	endpoints, err := reach.NewEndpointCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	targetCatalog, err := reach.NewSubscriptionTargetCatalog(reachStore, endpoints)
	if err != nil {
		t.Fatal(err)
	}
	for targetKind, targetID := range map[string]string{"group": group.ID, "webhook": provider.ID} {
		exists, err := targetCatalog.SubscriptionTargetReferenceExists(ctx, "target-org", targetKind, targetID)
		if err != nil || !exists {
			t.Fatalf("inert %s reference was not visible in its organization: exists=%t err=%v", targetKind, exists, err)
		}
		exists, err = targetCatalog.SubscriptionTargetReferenceExists(ctx, "other-org", targetKind, targetID)
		if err != nil || exists {
			t.Fatalf("inert %s reference escaped its organization: exists=%t err=%v", targetKind, exists, err)
		}
	}
	if active, err := targetCatalog.ListSubscriptionTargets(ctx, "target-org"); err != nil || len(active) != 0 {
		t.Fatalf("inert Reach records became operational targets: %#v err=%v", active, err)
	}
	signalService, signalImporter, err := signals.NewServiceWithExchangeImporter(
		repository.NewMemorySignalsStore(), signalsProviderEvaluator{}, nil, foundation.NopAuditor{}, signals.ServiceConfig{
			OrganizationID: "target-org", SubscriptionTargets: targetCatalog, SubscriptionTargetReferences: targetCatalog,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signalService.CreateSubscription(ctx, signals.CreateSubscriptionInput{ID: "ordinary-denied", TargetKind: "group", TargetID: group.ID}); !errors.Is(err, signals.ErrInvalidInput) {
		t.Fatalf("ordinary subscription creation accepted inert Reach target: %v", err)
	}
	rule := signals.Rule{ID: "imported-rule", Name: "Imported rule", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning,
		Enabled: true, ThresholdDays: []int{30}, Revision: 2, CreatedAt: now, UpdatedAt: now.Add(time.Hour)}
	operation := signals.ExchangeImportOperation{Token: "signals-inert-reach", OccurredAt: now.Add(2 * time.Hour)}
	if result, err := signalImporter.ImportRule(ctx, operation, rule); err != nil || !result.Created {
		t.Fatalf("import Signals rule: %#v err=%v", result, err)
	}
	for _, subscription := range []signals.Subscription{
		{ID: "group-subscription", RuleID: rule.ID, TargetKind: "group", TargetID: group.ID, Enabled: true, Revision: 2, CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
		{ID: "webhook-subscription", RuleID: rule.ID, TargetKind: "webhook", TargetID: provider.ID, Enabled: true, Revision: 2, CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if result, err := signalImporter.ImportSubscription(ctx, operation, subscription); err != nil || !result.Committed || !result.Created {
			t.Fatalf("import Signals subscription against inert %s: %#v err=%v", subscription.TargetKind, result, err)
		}
	}
}
