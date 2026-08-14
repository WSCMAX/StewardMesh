package exchange_test

// Requirements: REQ-EXCHANGE-001. Feature: migration.packages.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/atlascodes"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/exchange"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/ledger"
	"github.com/maxlemke/stewardmesh/internal/people"
	"github.com/maxlemke/stewardmesh/internal/reach"
	"github.com/maxlemke/stewardmesh/internal/repository"
	"github.com/maxlemke/stewardmesh/internal/signals"
)

func TestLedgerOrdinaryClockIsPortableBeforeExport(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.FixedZone("source", -5*60*60))
	service, importer, err := ledger.NewServiceWithExchangeImporter(
		repository.NewMemoryLedgerStore(), allowLedgerReferences{}, nil, foundation.NopAuditor{},
		ledger.ServiceConfig{OrganizationID: "ledger-portable-clock", Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	vendor, err := service.CreateVendor(context.Background(), ledger.CreateVendorInput{ID: "vendor-one", Name: "Vendor one"})
	if err != nil {
		t.Fatal(err)
	}
	assertPortableServiceTime(t, vendor.CreatedAt)
	assertPortableServiceTime(t, vendor.UpdatedAt)
	provider, err := exchange.NewLedgerProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	if records, err := provider.ListRecords(context.Background()); err != nil || len(records) != 1 {
		t.Fatalf("export normalized Ledger timestamp: records=%#v err=%v", records, err)
	}
}

func TestSignalsOrdinaryClockIsPortableBeforeExport(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.FixedZone("source", 2*60*60))
	service, importer, err := signals.NewServiceWithExchangeImporter(
		repository.NewMemorySignalsStore(), signalsProviderEvaluator{}, nil, foundation.NopAuditor{},
		signals.ServiceConfig{OrganizationID: "signals-portable-clock", SubscriptionTargets: signalsProviderTargets{}, Now: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := service.CreateRule(context.Background(), signals.CreateRuleInput{
		ID: "rule-one", Name: "Rule one", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPortableServiceTime(t, rule.CreatedAt)
	assertPortableServiceTime(t, rule.UpdatedAt)
	provider, err := exchange.NewSignalsProvider(service, importer)
	if err != nil {
		t.Fatal(err)
	}
	if records, err := provider.ListRecords(context.Background()); err != nil || len(records) != 1 {
		t.Fatalf("export normalized Signals timestamp: records=%#v err=%v", records, err)
	}
}

func TestAtlasOrdinaryWritesDoNotRegressImportedFutureTimes(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	service, importer, err := atlas.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasStore(), allowAtlasReferences{}, nil, foundation.NopAuditor{},
		atlas.ServiceConfig{OrganizationID: "atlas-future", Now: func() time.Time { return clock }},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := atlas.ExchangeImportOperation{Token: "atlas-future-import", OccurredAt: clock}
	model := domain.AssetModel{ID: "future-model", Manufacturer: "Example", Name: "Future model", Kind: "server", Status: "active", Revision: 1, CreatedAt: future, UpdatedAt: future}
	if result, err := importer.ImportModel(context.Background(), operation, model); err != nil || !result.Created {
		t.Fatalf("import future Atlas model: %#v err=%v", result, err)
	}
	updatedModel, err := service.UpdateModel(context.Background(), atlas.UpdateModelInput{
		ID: model.ID, Manufacturer: model.Manufacturer, Name: "Updated future model", Kind: model.Kind, Revision: model.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedModel.UpdatedAt.Before(future) {
		t.Fatalf("Atlas model update regressed imported time: got %v want >= %v", updatedModel.UpdatedAt, future)
	}
	createdAsset, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{ID: "future-model-asset", ModelID: model.ID, Name: "Future model asset", Kind: "server", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if createdAsset.ModelContext == nil || createdAsset.CreatedAt.Before(updatedModel.UpdatedAt) || createdAsset.ModelContext.AppliedAt.Before(createdAsset.ModelContext.DefaultsEffectiveAt) {
		t.Fatalf("asset from future model has regressive context: %#v", createdAsset)
	}

	assetFuture := future.Add(time.Hour)
	asset := domain.Asset{ID: "future-asset", Name: "Future asset", Kind: "server", Status: "active", Revision: 1, CreatedAt: assetFuture, UpdatedAt: assetFuture}
	if result, err := importer.ImportAsset(context.Background(), operation, asset); err != nil || !result.Created {
		t.Fatalf("import future Atlas asset: %#v err=%v", result, err)
	}
	updatedAsset, err := service.UpdateAsset(context.Background(), atlas.UpdateAssetInput{
		ID: asset.ID, Name: "Updated future asset", Kind: asset.Kind, Status: asset.Status, Revision: asset.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedAsset.UpdatedAt.Before(assetFuture) {
		t.Fatalf("Atlas asset update regressed imported time: got %v want >= %v", updatedAsset.UpdatedAt, assetFuture)
	}
}

func TestAtlasCodesOrdinaryWritesDoNotRegressImportedFutureTimes(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	atlasService, err := atlas.NewService(repository.NewMemoryAtlasStore(), allowAtlasReferences{}, foundation.NopAuditor{}, atlas.ServiceConfig{OrganizationID: "codes-future", Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := atlasService.CreateAsset(context.Background(), atlas.CreateAssetInput{ID: "codes-asset", Name: "Codes asset", Kind: "server", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	codes, importer, err := atlascodes.NewServiceWithExchangeImporter(repository.NewMemoryAtlasCodesStore(), atlasService, nil, foundation.NopAuditor{}, atlascodes.ServiceConfig{OrganizationID: "codes-future", Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	identifier := atlascodes.Identifier{
		ID: "future-identifier", AssetID: asset.ID, Symbology: atlascodes.SymbologyCode128, NormalizedValue: "CODE-FUTURE", DisplayValue: "Code future",
		Source: atlascodes.SourceImported, Primary: true, Status: atlascodes.StatusActive, Revision: 1,
		CreatedBy: "source-operator", CreatedCorrelationID: "source-correlation", UpdatedBy: "source-operator", UpdatedCorrelationID: "source-correlation",
		CreatedAt: future, UpdatedAt: future,
	}
	if result, err := importer.ImportIdentifierChain(context.Background(), atlascodes.ExchangeImportOperation{Token: "codes-future-import", OccurredAt: clock}, atlascodes.IdentifierChain{TerminalID: identifier.ID, Items: []atlascodes.Identifier{identifier}}); err != nil || !result.Created {
		t.Fatalf("import future Atlas Codes identifier: %#v err=%v", result, err)
	}
	ctx := foundation.WithScope(context.Background(), foundation.Scope{OrganizationID: "codes-future", ActorID: "local-operator", CorrelationID: "local-correlation"})
	deactivated, changed, err := codes.DeactivateIdentifier(ctx, atlascodes.DeactivateIdentifierInput{AssetID: asset.ID, IdentifierID: identifier.ID, Revision: identifier.Revision})
	if err != nil || !changed {
		t.Fatalf("deactivate future Atlas Codes identifier: %#v changed=%t err=%v", deactivated, changed, err)
	}
	if deactivated.UpdatedAt.Before(future) || deactivated.DeactivatedAt == nil || deactivated.DeactivatedAt.Before(future) {
		t.Fatalf("Atlas Codes update regressed imported time: %#v", deactivated)
	}
}

func TestLedgerOrdinaryMutationDoesNotRegressImportedFutureTime(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	service, importer, err := ledger.NewServiceWithExchangeImporter(repository.NewMemoryLedgerStore(), allowLedgerReferences{}, nil, foundation.NopAuditor{}, ledger.ServiceConfig{OrganizationID: "ledger-future", Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	operation := ledger.ExchangeImportOperation{Token: "ledger-future-import", OccurredAt: clock}
	vendor := ledger.Vendor{ID: "future-vendor", Name: "Future vendor", Status: "active", Revision: 1, CreatedAt: future, UpdatedAt: future}
	if result, err := importer.ImportVendor(context.Background(), operation, vendor); err != nil || !result.Created {
		t.Fatalf("import future Ledger vendor: %#v err=%v", result, err)
	}
	purchase := ledger.PurchaseOrder{ID: "future-purchase", Number: "PO-FUTURE", VendorID: vendor.ID, Status: "draft", Currency: "USD", Revision: 1, CreatedAt: future, UpdatedAt: future}
	if result, err := importer.ImportPurchaseOrder(context.Background(), operation, purchase); err != nil || !result.Created {
		t.Fatalf("import future Ledger purchase: %#v err=%v", result, err)
	}
	updated, err := service.UpdatePurchaseOrderStatus(context.Background(), ledger.UpdatePurchaseOrderStatusInput{ID: purchase.ID, Status: "approved", Revision: purchase.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt.Before(future) {
		t.Fatalf("Ledger update regressed imported time: got %v want >= %v", updated.UpdatedAt, future)
	}
}

func TestPeopleDefaultAssignmentEndDoesNotRegressImportedFutureTime(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	assets := peopleProviderAssets{items: map[string]domain.Asset{"future-asset": {ID: "future-asset", OrganizationID: "people-future", Status: "active"}}}
	service, importer, _ := newPeopleProviderService(t, "people-future", assets, clock)
	identity, err := service.CreateIdentity(context.Background(), people.CreateIdentityInput{Kind: people.IdentityPerson, DisplayName: "Future assignee", Email: "future@example.test", Status: people.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	assignment := people.AssetAssignment{
		ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AssetID: "future-asset", AssigneeKind: people.AssigneeIdentity, AssigneeID: identity.ID,
		Role: people.AssignmentUser, EffectiveFrom: future, CreatedBy: "system:exchange", CreatedAt: future,
	}
	if result, err := importer.ImportAssetAssignment(context.Background(), people.ExchangeImportOperation{Token: "people-future-import", OccurredAt: clock}, assignment); err != nil || !result.Created {
		t.Fatalf("import future People assignment: %#v err=%v", result, err)
	}
	ended, err := service.EndAssetAssignment(context.Background(), people.EndAssetAssignmentInput{AssetID: assignment.AssetID, AssignmentID: assignment.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ended.EffectiveTo == nil || !ended.EffectiveTo.After(future) {
		t.Fatalf("People default assignment end regressed imported effective time: %#v", ended)
	}
}

func TestPeopleDefaultAssignmentEndRejectsMaximumPortableEffectiveFrom(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	maximum := time.Date(9999, time.December, 31, 23, 59, 59, 999999000, time.UTC)
	assets := peopleProviderAssets{items: map[string]domain.Asset{"maximum-asset": {ID: "maximum-asset", OrganizationID: "people-maximum", Status: "active"}}}
	service, importer, _ := newPeopleProviderService(t, "people-maximum", assets, clock)
	identity, err := service.CreateIdentity(context.Background(), people.CreateIdentityInput{Kind: people.IdentityPerson, DisplayName: "Maximum assignee", Email: "maximum@example.test", Status: people.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	assignment := people.AssetAssignment{
		ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AssetID: "maximum-asset", AssigneeKind: people.AssigneeIdentity, AssigneeID: identity.ID,
		Role: people.AssignmentUser, EffectiveFrom: maximum, CreatedBy: "system:exchange", CreatedAt: maximum,
	}
	if result, err := importer.ImportAssetAssignment(context.Background(), people.ExchangeImportOperation{Token: "people-maximum-import", OccurredAt: clock}, assignment); err != nil || !result.Created {
		t.Fatalf("import maximum People assignment: %#v err=%v", result, err)
	}
	if _, err := service.EndAssetAssignment(context.Background(), people.EndAssetAssignmentInput{AssetID: assignment.AssetID, AssignmentID: assignment.ID}); !errors.Is(err, people.ErrConflict) {
		t.Fatalf("default assignment end at maximum portable instant: got %v, want conflict", err)
	}
	stored, err := service.ListAssetAssignments(context.Background(), assignment.AssetID, people.Visibility{All: true})
	if err != nil || len(stored) != 1 || stored[0].EffectiveTo != nil {
		t.Fatalf("maximum assignment was mutated after rejected end: %#v err=%v", stored, err)
	}
}

func TestSignalsOrdinaryMutationDoesNotRegressImportedFutureTime(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	service, importer, err := signals.NewServiceWithExchangeImporter(repository.NewMemorySignalsStore(), signalsProviderEvaluator{}, nil, foundation.NopAuditor{}, signals.ServiceConfig{
		OrganizationID: "signals-future", SubscriptionTargets: signalsProviderTargets{}, Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	rule := signals.Rule{ID: "future-rule", Name: "Future rule", Condition: signals.ConditionRenewal, Severity: signals.SeverityWarning, Enabled: true,
		ThresholdDays: []int{180, 90, 60, 30}, Revision: 1, CreatedAt: future, UpdatedAt: future}
	if result, err := importer.ImportRule(context.Background(), signals.ExchangeImportOperation{Token: "signals-future-import", OccurredAt: clock}, rule); err != nil || !result.Created {
		t.Fatalf("import future Signals rule: %#v err=%v", result, err)
	}
	updated, err := service.UpdateRule(context.Background(), rule.ID, signals.UpdateRuleInput{Name: "Updated future rule", Condition: rule.Condition, Severity: rule.Severity, Enabled: true, Revision: rule.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt.Before(future) {
		t.Fatalf("Signals update regressed imported time: got %v want >= %v", updated.UpdatedAt, future)
	}
}

func TestReachOrdinaryMutationDoesNotRegressImportedFutureTime(t *testing.T) {
	clock := time.Date(2026, time.August, 13, 12, 0, 0, 123456789, time.UTC)
	future := time.Date(2030, time.August, 13, 12, 0, 0, 654321000, time.UTC)
	service, importer, provider := newReachExchangeProvider(t, "reach-future", clock)
	template := reach.Template{ID: "future-template", Name: "Future template", Subject: "Subject", Body: "Body", Revision: 1, CreatedAt: future, UpdatedAt: future}
	if result, err := importer.ImportTemplate(context.Background(), reach.ExchangeImportOperation{Token: "reach-future-import", OccurredAt: clock}, template); err != nil || !result.Created {
		t.Fatalf("import future Reach template: %#v err=%v", result, err)
	}
	updated, err := service.UpdateTemplate(context.Background(), template.ID, reach.UpdateTemplateInput{Name: "Updated future template", Subject: template.Subject, Body: template.Body, Revision: template.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UpdatedAt.Before(future) {
		t.Fatalf("Reach update regressed imported time: got %v want >= %v", updated.UpdatedAt, future)
	}
	if records, err := provider.ListRecords(context.Background()); err != nil || len(records) != 1 {
		t.Fatalf("export updated future Reach timestamp: records=%#v err=%v", records, err)
	}
}

func assertPortableServiceTime(t *testing.T, value time.Time) {
	t.Helper()
	if value.Location() != time.UTC || value.Nanosecond()%int(time.Microsecond) != 0 {
		t.Fatalf("service timestamp = %v (%v); want UTC microsecond precision", value, value.Location())
	}
}
