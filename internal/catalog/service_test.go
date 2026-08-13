package catalog_test

// Foundation tests for REQ-ATLAS-CATALOG-001 and inventory.products.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/catalog"
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

func TestCatalogFoundationCreatesReusableRecordsAndResolvesEffectivePrice(t *testing.T) {
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	auditor := &catalogTestAuditor{}
	service := newCatalogService(t, repository.NewMemoryCatalogStore(), auditor, "org-one", now)
	ctx := foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "org-one", ActorID: "account-one", CorrelationID: "correlation-one",
	})

	specifications := map[string]string{"Memory_GB": " 64 ", "cpu.family": "Xeon"}
	product, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		ID: "product-r760", Manufacturer: " Dell ", Model: " PowerEdge R760 ", AssetKind: " SERVER ",
		Specifications: specifications, DefaultUsefulLifeMonths: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if product.Manufacturer != "Dell" || product.Model != "PowerEdge R760" || product.AssetKind != "server" ||
		product.Status != catalog.StatusActive || product.Specifications["memory_gb"] != "64" || product.Revision != 1 {
		t.Fatalf("unexpected product %#v", product)
	}
	specifications["Memory_GB"] = "1"
	product.Specifications["memory_gb"] = "2"
	loaded, err := service.GetProduct(ctx, product.ID)
	if err != nil || loaded.Specifications["memory_gb"] != "64" {
		t.Fatalf("catalog product was not defensively persisted: %#v err=%v", loaded, err)
	}
	if _, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		ID: "duplicate-model", Manufacturer: "dell", Model: "poweredge r760", AssetKind: "server",
	}); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("expected case-insensitive product conflict, got %v", err)
	}

	configuration, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-64gb", ProductID: product.ID, Name: " 64 GB standard ", SKU: " R760-64 ",
		Specifications: map[string]string{"memory_gb": "64", "storage.profile": "RAID1"},
	})
	if err != nil || configuration.ProductID != product.ID || configuration.SKU != "R760-64" {
		t.Fatalf("unexpected configuration %#v err=%v", configuration, err)
	}

	baseStart := date(2026, time.January, 1)
	baseEnd := date(2026, time.December, 31)
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-base-list", ProductID: product.ID, Kind: catalog.PriceKindList,
		AmountMinor: 800_000, Currency: "usd", EffectiveFrom: baseStart, EffectiveTo: &baseEnd,
		SourceReference: "public-list-2026",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-config-quote", ProductID: product.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindQuote,
		AmountMinor: 720_000, Currency: "USD", EffectiveFrom: date(2026, time.February, 1),
		SourceReference: "confidential-quote-17",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ID: "price-config-contract", ProductID: product.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindContract,
		AmountMinor: 690_000, Currency: "USD", EffectiveFrom: date(2026, time.March, 1),
	}); err != nil {
		t.Fatal(err)
	}

	selected, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ProductID: product.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.April, 15), Currency: "USD",
	})
	if err != nil || selected.ID != "price-config-contract" || selected.AmountMinor != 690_000 {
		t.Fatalf("unexpected preferred configuration price %#v err=%v", selected, err)
	}
	listPrice, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ProductID: product.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.April, 15),
		Currency: "USD", Kind: catalog.PriceKindList,
	})
	if err != nil || listPrice.ID != "price-base-list" {
		t.Fatalf("expected product-level list fallback, got %#v err=%v", listPrice, err)
	}
	beforeConfigurationPrice, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ProductID: product.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.January, 15), Currency: "USD",
	})
	if err != nil || beforeConfigurationPrice.ID != "price-base-list" {
		t.Fatalf("expected effective-date base fallback, got %#v err=%v", beforeConfigurationPrice, err)
	}

	target, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		ID: "product-r770", Manufacturer: "Dell", Model: "PowerEdge R770", AssetKind: "server",
		DefaultUsefulLifeMonths: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	path, err := service.CreateUpgradePath(ctx, catalog.CreateUpgradePathInput{
		ID: "path-r760-r770", FromProductID: product.ID, FromConfigurationID: configuration.ID,
		ToProductID: target.ID, Kind: catalog.UpgradeKindSuccessor, EffectiveFrom: date(2026, time.August, 1),
	})
	if err != nil || path.ToProductID != target.ID {
		t.Fatalf("unexpected upgrade path %#v err=%v", path, err)
	}
	paths, err := service.ListUpgradePaths(ctx, product.ID, configuration.ID)
	if err != nil || len(paths) != 1 || paths[0].ID != path.ID {
		t.Fatalf("unexpected path list %#v err=%v", paths, err)
	}

	if len(auditor.events) != 7 {
		t.Fatalf("expected seven creation audits, got %#v", auditor.events)
	}
	for _, event := range auditor.events {
		if event.OrganizationID != "org-one" || event.ActorID != "account-one" || event.CorrelationID != "correlation-one" ||
			event.Metadata["requirementId"] != catalog.RequirementID || event.Metadata["featureId"] != catalog.FeatureID {
			t.Fatalf("unexpected audit provenance %#v", event)
		}
		serialized := strings.ToLower(metadataText(event.Metadata))
		for _, forbidden := range []string{"690000", "720000", "800000", "xeon", "raid1", "r760-64", "confidential-quote-17"} {
			if strings.Contains(serialized, forbidden) {
				t.Fatalf("audit metadata leaked %q in %#v", forbidden, event.Metadata)
			}
		}
	}
}

func TestCatalogFoundationRejectsInvalidReferencesSelfLinksAndMixedCurrencies(t *testing.T) {
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	service := newCatalogService(t, repository.NewMemoryCatalogStore(), &catalogTestAuditor{}, "org-one", now)
	ctx := context.Background()
	product, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		ID: "product-one", Manufacturer: "Framework", Model: "Laptop 16", AssetKind: "laptop",
	})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-one", ProductID: product.ID, Name: "Graphics", SKU: "FW16-GFX",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		ID: "product-two", Manufacturer: "Framework", Model: "Laptop 13", AssetKind: "laptop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ProductID: other.ID, ConfigurationID: configuration.ID, Kind: catalog.PriceKindList,
		AmountMinor: 1, Currency: "USD", EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected product/configuration mismatch rejection, got %v", err)
	}
	if _, err := service.CreateUpgradePath(ctx, catalog.CreateUpgradePathInput{
		FromProductID: product.ID, FromConfigurationID: configuration.ID,
		ToProductID: product.ID, ToConfigurationID: configuration.ID,
		Kind: catalog.UpgradeKindUpgrade, EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected self-link rejection, got %v", err)
	}
	for _, currency := range []string{"USD", "CAD"} {
		if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
			ID: "price-" + strings.ToLower(currency), ProductID: product.ID, ConfigurationID: configuration.ID,
			Kind: catalog.PriceKindEstimate, AmountMinor: 100, Currency: currency, EffectiveFrom: date(2026, time.January, 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ProductID: product.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.February, 1),
	}); !errors.Is(err, catalog.ErrMixedCurrency) {
		t.Fatalf("expected mixed-currency rejection, got %v", err)
	}
	selected, err := service.ResolvePrice(ctx, catalog.ResolvePriceInput{
		ProductID: product.ID, ConfigurationID: configuration.ID, AsOf: date(2026, time.February, 1), Currency: "CAD",
	})
	if err != nil || selected.Currency != "CAD" {
		t.Fatalf("expected explicit currency selection, got %#v err=%v", selected, err)
	}
	if _, err := service.CreateConfiguration(ctx, catalog.CreateConfigurationInput{
		ID: "configuration-two", ProductID: other.ID, Name: "Other", SKU: "fw16-gfx",
	}); !errors.Is(err, catalog.ErrConflict) {
		t.Fatalf("expected organization-wide SKU conflict, got %v", err)
	}
	if _, err := service.CreateProduct(ctx, catalog.CreateProductInput{
		Manufacturer: "Bad", Model: "Life", AssetKind: "laptop", DefaultUsefulLifeMonths: 1201,
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected useful-life bound rejection, got %v", err)
	}
	if _, err := service.RecordPrice(ctx, catalog.RecordPriceInput{
		ProductID: product.ID, Kind: catalog.PriceKindList, AmountMinor: catalog.MaximumExactMinorUnits + 1,
		Currency: "USD", EffectiveFrom: date(2026, time.January, 1),
	}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Fatalf("expected exact-money bound rejection, got %v", err)
	}
}

func TestCatalogStoreIsOrganizationScopedAndListsDeterministically(t *testing.T) {
	store := repository.NewMemoryCatalogStore()
	now := time.Date(2026, time.August, 12, 15, 0, 0, 0, time.UTC)
	serviceOne := newCatalogService(t, store, &catalogTestAuditor{}, "org-one", now)
	serviceTwo := newCatalogService(t, store, &catalogTestAuditor{}, "org-two", now)
	for _, service := range []*catalog.Service{serviceOne, serviceTwo} {
		if _, err := service.CreateProduct(context.Background(), catalog.CreateProductInput{
			ID: "shared-id", Manufacturer: "Lenovo", Model: "ThinkPad P1", AssetKind: "laptop",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := serviceOne.GetProduct(context.Background(), "shared-id"); err != nil {
		t.Fatal(err)
	}
	items, err := serviceOne.ListProducts(context.Background(), catalog.ProductQuery{Search: "thinkpad", AssetKind: "laptop"})
	if err != nil || len(items) != 1 || items[0].OrganizationID != "org-one" {
		t.Fatalf("unexpected organization-scoped list %#v err=%v", items, err)
	}
	if _, err := serviceOne.GetProduct(context.Background(), "missing"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expected missing product, got %v", err)
	}
}

func newCatalogService(t *testing.T, store catalog.Store, auditor foundation.Auditor, organizationID string, now time.Time) *catalog.Service {
	t.Helper()
	service, err := catalog.NewService(store, auditor, catalog.ServiceConfig{
		OrganizationID: organizationID, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
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
