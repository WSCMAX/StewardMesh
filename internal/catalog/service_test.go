package catalog_test

// Foundation tests for REQ-ATLAS-CATALOG-001 and inventory.catalog.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/catalog"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type catalogTestAuditor struct {
	events []foundation.AuditEvent
}

func (a *catalogTestAuditor) Record(_ context.Context, event foundation.AuditEvent) error {
	event.Metadata = cloneMetadata(event.Metadata)
	a.events = append(a.events, event)
	return nil
}

type catalogTestReferences struct{}

func (catalogTestReferences) ValidateAssetReferences(context.Context, string, atlas.References) error {
	return nil
}
func (catalogTestReferences) ValidateIdentities(context.Context, string, []string) error { return nil }

type catalogTestWriteGate struct {
	err   error
	calls []string
}

func (g *catalogTestWriteGate) CheckResourceWrite(_ context.Context, recordType, recordID string) error {
	g.calls = append(g.calls, recordType+"/"+recordID)
	return g.err
}

func TestCatalogFoundationExtendsAtlasModelsAndResolvesEffectivePrice(t *testing.T) {
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	auditor := &catalogTestAuditor{}
	service, models := newCatalogService(t, repository.NewMemoryCatalogStore(), auditor, "org-one", now)
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "correlation-one",
	})
	current := createModel(t, models, ctx, "model-r760", "PowerEdge R760")
	target := createModel(t, models, ctx, "model-r770", "PowerEdge R770")

	specifications := map[string]string{"Memory_GB": " 64 ", "storage.profile": "RAID1"}
	configuration, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-64gb", ModelID: current.ID, Name: " 64 GB standard ", SKU: " R760-64 ",
		Specifications: specifications,
	})
	if err != nil || configuration.ModelID != current.ID || configuration.SKU != "R760-64" ||
		configuration.Specifications["memory_gb"] != "64" {
		t.Fatalf("unexpected configuration %#v err=%v", configuration, err)
	}
	specifications["Memory_GB"] = "1"
	configuration.Specifications["memory_gb"] = "2"
	listed, err := service.ListConfigurations(ctx, current.ID)
	if err != nil || len(listed) != 1 || listed[0].Specifications["memory_gb"] != "64" {
		t.Fatalf("configuration was not defensively persisted: %#v err=%v", listed, err)
	}

	baseStart := date(2026, time.January, 1)
	baseEnd := date(2026, time.December, 31)
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-base-list", ModelID: current.ID, Kind: catalog.PriceKindList,
		AmountMinor: 800_000, Currency: "usd", EffectiveFrom: baseStart, EffectiveTo: &baseEnd,
		SourceReference: "public-list-2026",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-config-quote", ModelID: current.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindQuote,
		AmountMinor: 720_000, Currency: "USD", EffectiveFrom: date(2026, time.February, 1),
		SourceReference: "confidential-quote-17",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-config-contract", ModelID: current.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindContract,
		AmountMinor: 690_000, Currency: "USD", EffectiveFrom: date(2026, time.March, 1),
	}); err != nil {
		t.Fatal(err)
	}

	selected, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ModelID: current.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.April, 15), Currency: "USD",
	})
	if err != nil || selected.ID != "price-config-contract" || selected.AmountMinor != 690_000 {
		t.Fatalf("unexpected preferred configuration price %#v err=%v", selected, err)
	}
	listPrice, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ModelID: current.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.April, 15),
		Currency: "USD", Kind: catalog.PriceKindList,
	})
	if err != nil || listPrice.ID != "price-base-list" {
		t.Fatalf("expected model-level list fallback, got %#v err=%v", listPrice, err)
	}
	beforeConfigurationPrice, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ModelID: current.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.January, 15), Currency: "USD",
	})
	if err != nil || beforeConfigurationPrice.ID != "price-base-list" {
		t.Fatalf("expected effective-date model fallback, got %#v err=%v", beforeConfigurationPrice, err)
	}

	path, err := service.CreateUpgradePath(ctx, catalog.CreateUpgradePathInput{
		ID: "path-r760-r770", FromModelID: current.ID, FromConfigurationID: configuration.ID,
		ToModelID: target.ID, Kind: catalog.UpgradeKindSuccessor, EffectiveFrom: date(2026, time.August, 1),
	})
	if err != nil || path.ToModelID != target.ID {
		t.Fatalf("unexpected upgrade path %#v err=%v", path, err)
	}
	paths, err := service.ListUpgradePaths(ctx, current.ID, configuration.ID)
	if err != nil || len(paths) != 1 || paths[0].ID != path.ID {
		t.Fatalf("unexpected path list %#v err=%v", paths, err)
	}
	snapshot, err := service.Snapshot(ctx)
	if err != nil || len(snapshot.Configurations) != 1 || len(snapshot.Prices) != 3 || len(snapshot.UpgradePaths) != 1 ||
		snapshot.Configurations[0].ID != configuration.ID || snapshot.UpgradePaths[0].ID != path.ID {
		t.Fatalf("unexpected bounded Catalog snapshot %#v err=%v", snapshot, err)
	}
	if exact, err := service.GetConfiguration(ctx, configuration.ID); err != nil || exact.ID != configuration.ID {
		t.Fatalf("get exact configuration %#v err=%v", exact, err)
	}
	if exact, err := service.GetPrice(ctx, "price-config-contract"); err != nil || exact.ID != "price-config-contract" {
		t.Fatalf("get exact price %#v err=%v", exact, err)
	}
	if exact, err := service.GetUpgradePath(ctx, path.ID); err != nil || exact.ID != path.ID {
		t.Fatalf("get exact upgrade path %#v err=%v", exact, err)
	}

	if len(auditor.events) != 5 {
		t.Fatalf("expected five Catalog creation audits, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "org-one" || event.ActorID != "account-one" || event.CorrelationID != "correlation-one" ||
			event.Metadata["requirementId"] != catalog.RequirementID || event.Metadata["featureId"] != catalog.FeatureID {
			t.Fatalf("unexpected audit provenance %#v", event)
		}
		serialized := strings.ToLower(metadataText(event.Metadata))
		for _, forbidden := range []string{"690000", "720000", "800000", "raid1", "r760-64", "confidential-quote-17"} {
			if strings.Contains(serialized, forbidden) {
				t.Fatalf("audit metadata leaked %q in %#v", forbidden, event.Metadata)
			}
		}
	}
}

func TestCatalogFoundationRejectsInvalidModelReferencesSelfLinksAndMixedCurrencies(t *testing.T) {
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	service, models := newCatalogService(t, repository.NewMemoryCatalogStore(), &catalogTestAuditor{}, "org-one", now)
	ctx := context.Background()
	current := createModel(t, models, ctx, "model-one", "Laptop 16")
	other := createModel(t, models, ctx, "model-two", "Laptop 13")

	if _, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "missing-configuration", ModelID: "missing-model", Name: "Missing",
	}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expected missing Atlas model rejection, got %v", err)
	}
	configuration, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-one", ModelID: current.ID, Name: "Graphics", SKU: "FW16-GFX",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ModelID: other.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindList,
		AmountMinor: 1, Currency: "USD", EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected model/configuration mismatch rejection, got %v", err)
	}
	if _, err := service.CreateUpgradePath(ctx, catalog.CreateUpgradePathInput{
		FromModelID: current.ID, FromConfigurationID: configuration.ID,
		ToModelID: current.ID, ToConfigurationID: configuration.ID,
		Kind: catalog.UpgradeKindUpgrade, EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected self-link rejection, got %v", err)
	}
	for _, currency := range []string{"USD", "CAD"} {
		if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
			ID: "price-" + strings.ToLower(currency), ModelID: current.ID, ConfigurationID: configuration.ID,
			Kind: catalog.PriceKindEstimate, AmountMinor: 100, Currency: currency, EffectiveFrom: date(2026, time.January, 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ModelID: current.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.February, 1),
	}); !errors.Is(err, catalog.ErrMixedCurrency) {
		t.Fatalf("expected mixed-currency rejection, got %v", err)
	}
	selected, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ModelID: current.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.February, 1), Currency: "CAD",
	})
	if err != nil || selected.Currency != "CAD" {
		t.Fatalf("expected explicit currency selection, got %#v err=%v", selected, err)
	}
	if _, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-two", ModelID: other.ID, Name: "Other", SKU: "fw16-gfx",
	}); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("expected organization-wide SKU conflict, got %v", err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ModelID: current.ID, Kind: catalog.PriceKindList, AmountMinor: catalog.MaximumExactMinorUnits + 1,
		Currency: "USD", EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected exact-money bound rejection, got %v", err)
	}
}

func TestCatalogStoreRemainsOrganizationScopedAcrossAtlasModelReaders(t *testing.T) {
	store := repository.NewMemoryCatalogStore()
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	serviceOne, modelsOne := newCatalogService(t, store, &catalogTestAuditor{}, "org-one", now)
	serviceTwo, modelsTwo := newCatalogService(t, store, &catalogTestAuditor{}, "org-two", now)
	modelOne := createModel(t, modelsOne, context.Background(), "shared-model", "ThinkPad P1")
	modelTwo := createModel(t, modelsTwo, context.Background(), "shared-model", "ThinkPad P1")

	for _, fixture := range []struct {
		service *catalog.Service
		modelID string
		name    string
	}{
		{serviceOne, modelOne.ID, "Performance"},
		{serviceTwo, modelTwo.ID, "Mobile"},
	} {
		if _, err := fixture.service.CreateConfiguration(context.Background(), catalog.CreateConfigurationInput{
			ID: "shared-configuration", ModelID: fixture.modelID, Name: fixture.name,
		}); err != nil {
			t.Fatal(err)
		}
	}
	itemsOne, err := serviceOne.ListConfigurations(context.Background(), modelOne.ID)
	if err != nil || len(itemsOne) != 1 || itemsOne[0].OrganizationID != "org-one" || itemsOne[0].Name != "Performance" {
		t.Fatalf("unexpected organization-one configurations %#v err=%v", itemsOne, err)
	}
	itemsTwo, err := serviceTwo.ListConfigurations(context.Background(), modelTwo.ID)
	if err != nil || len(itemsTwo) != 1 || itemsTwo[0].OrganizationID != "org-two" || itemsTwo[0].Name != "Mobile" {
		t.Fatalf("unexpected organization-two configurations %#v err=%v", itemsTwo, err)
	}

	crossOrganization, err := catalog.NewService(store, modelsOne, &catalogTestAuditor{}, catalog.ServiceConfig{
		OrganizationID: "org-two", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crossOrganization.ListConfigurations(context.Background(), modelOne.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expected cross-organization model rejection, got %v", err)
	}
}

func TestCatalogExchangeImporterPreservesRevisionAndReplaysDeterministicAudit(t *testing.T) {
	now := time.Date(2026, time.August, 13, 18, 30, 0, 0, time.UTC)
	models, err := atlas.NewService(repository.NewMemoryAtlasStore(), catalogTestReferences{}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	createModel(t, models, context.Background(), "model-one", "Portable model")
	auditor := &catalogTestAuditor{}
	service, importer, err := catalog.NewServiceWithExchangeImporter(repository.NewMemoryCatalogStore(), models, nil, auditor, catalog.ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	operation := catalog.ExchangeImportOperation{Token: "exchange-catalog-import", OccurredAt: now}
	candidate := catalog.Configuration{ID: "configuration-one", ModelID: "model-one", Name: "Portable", Status: catalog.StatusActive,
		Specifications: map[string]string{"memory_gb": "64"}, Revision: 7}
	result, err := importer.ImportConfiguration(context.Background(), operation, candidate)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("unexpected imported Catalog result %#v err=%v", result, err)
	}
	created, err := service.GetConfiguration(context.Background(), candidate.ID)
	if err != nil || created.Revision != 7 || !created.CreatedAt.Equal(now) || !created.UpdatedAt.Equal(now) {
		t.Fatalf("Catalog import did not preserve revision/time %#v err=%v", created, err)
	}
	replayed, err := importer.ImportConfiguration(context.Background(), operation, candidate)
	if err != nil || !replayed.Committed || replayed.Created {
		t.Fatalf("unexpected Catalog replay %#v err=%v", replayed, err)
	}
	if len(auditor.events) != 2 || auditor.events[0].ID != auditor.events[1].ID || auditor.events[0].CorrelationID != operation.Token ||
		!auditor.events[0].OccurredAt.Equal(operation.OccurredAt) || auditor.events[0].ActorID != "system:exchange" {
		t.Fatalf("Catalog audit replay was not deterministic: %#v", auditor.events)
	}
	changed := candidate
	changed.Name = "Changed"
	if _, err := importer.ImportConfiguration(context.Background(), operation, changed); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("expected changed stable-ID import conflict, got %v", err)
	}
}

func TestCatalogServiceChecksOwnershipForEveryLocalWriteAndExchangeBypassesOnlyThroughImporter(t *testing.T) {
	now := time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
	models, err := atlas.NewService(repository.NewMemoryAtlasStore(), catalogTestReferences{}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: "org-one", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	createModel(t, models, context.Background(), "model-one", "Source model")
	createModel(t, models, context.Background(), "model-two", "Target model")
	locked := errors.New("resource ownership is locked")
	gate := &catalogTestWriteGate{err: locked}
	service, importer, err := catalog.NewServiceWithExchangeImporter(
		repository.NewMemoryCatalogStore(), models, gate, foundation.NopAuditor{},
		catalog.ServiceConfig{OrganizationID: "org-one", Now: func() time.Time { return now.Add(time.Hour) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateConfiguration(context.Background(), catalog.CreateConfigurationInput{
		ID: "configuration-one", ModelID: "model-one", Name: "Portable", Status: catalog.StatusActive,
	}); !errors.Is(err, locked) {
		t.Fatalf("expected configuration ownership denial, got %v", err)
	}
	operation := catalog.ExchangeImportOperation{Token: "exchange-catalog-ownership", OccurredAt: now}
	configuration := catalog.Configuration{ID: "configuration-one", ModelID: "model-one", Name: "Portable", Status: catalog.StatusActive, Revision: 3}
	if result, err := importer.ImportConfiguration(context.Background(), operation, configuration); err != nil || !result.Committed || !result.Created {
		t.Fatalf("Exchange importer did not use its private ownership capability: result=%#v err=%v", result, err)
	}
	if _, err := service.RecordPrice(context.Background(), catalog.RecordPriceInput{
		ID: "price-one", ModelID: "model-one", ConfigurationID: configuration.ID, Kind: catalog.PriceKindList,
		AmountMinor: 10_000, Currency: "USD", EffectiveFrom: now,
	}); !errors.Is(err, locked) {
		t.Fatalf("expected price ownership denial, got %v", err)
	}
	if _, err := service.CreateUpgradePath(context.Background(), catalog.CreateUpgradePathInput{
		ID: "upgrade-one", FromModelID: "model-one", FromConfigurationID: configuration.ID,
		ToModelID: "model-two", Kind: catalog.UpgradeKindUpgrade, EffectiveFrom: now,
	}); !errors.Is(err, locked) {
		t.Fatalf("expected upgrade-path ownership denial, got %v", err)
	}
	want := []string{
		"atlas.catalog-configuration/configuration-one",
		"atlas.catalog-price/price-one",
		"atlas.catalog-upgrade-path/upgrade-one",
	}
	if strings.Join(gate.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected ownership checks %#v", gate.calls)
	}
}

func newCatalogService(
	t *testing.T,
	store catalog.Store,
	auditor foundation.Auditor,
	organizationID string,
	now time.Time,
) (*catalog.Service, *atlas.Service) {
	t.Helper()
	models, err := atlas.NewService(repository.NewMemoryAtlasStore(), catalogTestReferences{}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: organizationID, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.NewService(store, models, auditor, catalog.ServiceConfig{
		OrganizationID: organizationID, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, models
}

func createModel(t *testing.T, models *atlas.Service, ctx context.Context, id, name string) domain.AssetModel {
	t.Helper()
	model, err := models.CreateModel(ctx, atlas.CreateModelInput{
		ID: id, Manufacturer: "Example", Name: name, ModelNumber: id, Kind: "server",
		Specifications: map[string]string{"cpu.family": "Xeon"}, UsefulLifeMonths: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func cloneMetadata(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func metadataText(input map[string]string) string {
	var result strings.Builder
	for key, value := range input {
		result.WriteString(key)
		result.WriteString("=")
		result.WriteString(value)
		result.WriteString("\n")
	}
	return result.String()
}
