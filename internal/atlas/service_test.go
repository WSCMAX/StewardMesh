package atlas_test

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001. Features: inventory.assets, inventory.models.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/foundation"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type testReferenceValidator struct {
	reject bool
}

func (v testReferenceValidator) ValidateAssetReferences(context.Context, string, atlas.References) error {
	if v.reject {
		return atlas.ErrReferenceMissing
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

func TestServiceCreatesSearchesUpdatesAndAuditsAssets(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	auditor := &recordingAuditor{}
	service, err := atlas.NewService(repository.NewMemoryAtlasStore(), testReferenceValidator{}, auditor, atlas.ServiceConfig{
		OrganizationID: "example-org", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	purchased := now.AddDate(0, -1, 0)
	created, err := service.CreateAsset(foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-one", CorrelationID: "request-one",
	}), atlas.CreateAssetInput{
		ID: "asset-one", Name: "  Main Server  ", Kind: "SERVER", AssetTag: " ATLAS-001 ",
		SerialNumber: " SERIAL-001 ", Hostname: "SERVER-ONE.EXAMPLE.TEST", Status: "active",
		PurchaseDate: &purchased,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Main Server" || created.Kind != "server" || created.Hostname != "server-one.example.test" ||
		created.Revision != 1 || created.OrganizationID != "example-org" || created.PurchaseDate.Hour() != 0 {
		t.Fatalf("unexpected created asset %#v", created)
	}
	items, err := service.ListAssets(context.Background(), atlas.Query{Search: "atlas-001", Status: "active"})
	if err != nil || len(items) != 1 {
		t.Fatalf("unexpected search %#v err=%v", items, err)
	}
	now = now.Add(time.Hour)
	updated, err := service.UpdateAsset(foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-one", CorrelationID: "request-two",
	}), atlas.UpdateAssetInput{
		ID: created.ID, Name: created.Name, Kind: created.Kind, AssetTag: created.AssetTag,
		SerialNumber: created.SerialNumber, Hostname: created.Hostname, Status: "retired",
		PurchaseDate: created.PurchaseDate, Revision: created.Revision, LifecycleNote: "Replacement completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Status != "retired" {
		t.Fatalf("unexpected updated asset %#v", updated)
	}
	history, err := service.ListAssetLifecycle(context.Background(), created.ID)
	if err != nil || len(history) != 2 || history[1].Note != "Replacement completed" || history[1].ActorID != "account-one" {
		t.Fatalf("unexpected lifecycle %#v err=%v", history, err)
	}
	if len(auditor.events) != 2 || auditor.events[0].Action != "atlas.asset.created" ||
		auditor.events[1].Metadata["requirementId"] != atlas.RequirementID {
		t.Fatalf("unexpected audit events %#v", auditor.events)
	}
	if _, err := service.UpdateAsset(context.Background(), atlas.UpdateAssetInput{
		ID: created.ID, Name: created.Name, Kind: created.Kind, Status: "active", Revision: 1,
	}); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected stale revision conflict, got %v", err)
	}
}

func TestServiceMaintainsModelsAndAssetCounts(t *testing.T) {
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	auditor := &recordingAuditor{}
	service, err := atlas.NewService(repository.NewMemoryAtlasStore(), testReferenceValidator{}, auditor, atlas.ServiceConfig{
		OrganizationID: "example-org", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := service.CreateModel(foundation.WithScope(context.Background(), foundation.Scope{
		OrganizationID: "example-org", ActorID: "account-one", CorrelationID: "model-request-one",
	}), atlas.CreateModelInput{
		ID: "model-one", Manufacturer: " Framework ", Name: " Laptop 13 ", ModelNumber: " FW13 ",
		Kind: "LAPTOP", VendorIdentifier: "vendor-fw13",
		Specifications: map[string]string{"CPU": "Ryzen", "Memory": "32 GB"},
		SupportURL:     "https://support.example.test/fw13", WarrantyMonths: 36, UsefulLifeMonths: 48,
		SourceSystemID: "model-import", SourceRecordID: "framework-fw13-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.Manufacturer != "Framework" || model.Kind != "laptop" || model.Status != "active" || model.Revision != 1 {
		t.Fatalf("unexpected model %#v", model)
	}
	if _, err := service.CreateModel(context.Background(), atlas.CreateModelInput{
		ID: "model-two", Manufacturer: "framework", Name: "laptop 13", ModelNumber: "fw13", Kind: "laptop",
	}); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected duplicate model conflict, got %v", err)
	}
	asset, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{
		ID: "asset-one", ModelID: model.ID, Name: "Framework laptop", Kind: "laptop", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.ModelContext == nil || asset.ModelContext.ModelRevision != 1 || asset.ModelContext.Kind != "laptop" ||
		asset.ModelContext.SourceSystemID != "model-import" || asset.ModelContext.SourceRecordID != "framework-fw13-v1" ||
		len(asset.ModelContext.Overrides) != 0 {
		t.Fatalf("unexpected initial model context %#v", asset.ModelContext)
	}
	appliedAt := asset.ModelContext.AppliedAt
	models, err := service.ListModels(context.Background(), atlas.ModelQuery{Search: "framework"})
	if err != nil || len(models) != 1 || models[0].InstanceCount != 1 {
		t.Fatalf("unexpected models %#v err=%v", models, err)
	}
	now = now.Add(time.Hour)
	updated, err := service.UpdateModel(context.Background(), atlas.UpdateModelInput{
		ID: model.ID, Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber,
		Kind: "computer", VendorIdentifier: model.VendorIdentifier, Specifications: map[string]string{"CPU": "Ryzen 2"},
		SupportURL: model.SupportURL, WarrantyMonths: 48, UsefulLifeMonths: model.UsefulLifeMonths,
		SourceSystemID: "model-import", SourceRecordID: "framework-fw13-v2", Revision: model.Revision,
	})
	if err != nil || updated.Revision != 2 || updated.WarrantyMonths != 48 {
		t.Fatalf("unexpected updated model %#v err=%v", updated, err)
	}
	unchanged, err := service.GetAsset(context.Background(), asset.ID)
	if err != nil || unchanged.ModelContext == nil || unchanged.ModelContext.ModelRevision != 1 ||
		unchanged.ModelContext.Kind != "laptop" || unchanged.ModelContext.WarrantyMonths != 36 ||
		unchanged.ModelContext.Specifications["CPU"] != "Ryzen" || unchanged.ModelContext.SourceRecordID != "framework-fw13-v1" {
		t.Fatalf("model update rewrote asset defaults %#v err=%v", unchanged.ModelContext, err)
	}
	now = now.Add(time.Hour)
	overridden, err := service.UpdateAsset(context.Background(), atlas.UpdateAssetInput{
		ID: asset.ID, ModelID: model.ID, Name: asset.Name, Kind: "desktop", Status: asset.Status, Revision: asset.Revision,
	})
	if err != nil || overridden.ModelContext == nil || len(overridden.ModelContext.Overrides) != 1 ||
		overridden.ModelContext.Overrides[0] != "kind" || overridden.ModelContext.ModelRevision != 1 ||
		!overridden.ModelContext.AppliedAt.Equal(appliedAt) {
		t.Fatalf("unexpected model override context %#v err=%v", overridden.ModelContext, err)
	}
	retired, err := service.RetireModel(context.Background(), model.ID, updated.Revision)
	if err != nil || retired.Status != "retired" || retired.InstanceCount != 1 {
		t.Fatalf("unexpected retired model %#v err=%v", retired, err)
	}
	if _, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{
		ID: "asset-two", ModelID: model.ID, Name: "Blocked laptop", Kind: "laptop",
	}); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected retired model conflict, got %v", err)
	}
	actions := map[string]bool{}
	for _, event := range auditor.events {
		actions[event.Action] = event.Metadata["requirementId"] == atlas.ModelRequirementID
	}
	for _, action := range []string{"atlas.model.created", "atlas.model.updated", "atlas.model.retired"} {
		if !actions[action] {
			t.Fatalf("missing model audit action %s in %#v", action, auditor.events)
		}
	}
}

func TestServiceResolvesModelsAndAtomicallyCreatesBulkInstances(t *testing.T) {
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	auditor := &recordingAuditor{}
	service, err := atlas.NewService(repository.NewMemoryAtlasStore(), testReferenceValidator{}, auditor, atlas.ServiceConfig{
		OrganizationID: "example-org", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := service.CreateModel(context.Background(), atlas.CreateModelInput{
		ID: "model-bulk", Manufacturer: "Framework", Name: "Laptop 13", ModelNumber: "FW13", Kind: "laptop",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := service.ResolveModel(context.Background(), atlas.ModelIdentity{
		Manufacturer: " framework ", Name: " LAPTOP 13 ", ModelNumber: " fw13 ",
	})
	if err != nil || resolved.ID != model.ID {
		t.Fatalf("unexpected resolved model %#v err=%v", resolved, err)
	}
	result, err := service.CreateAssetsFromModel(context.Background(), atlas.BulkCreateAssetsInput{
		ModelID: model.ID,
		Items: []atlas.CreateAssetInput{
			{ID: "bulk-asset-one", Name: "Engineering laptop one", AssetTag: "BULK-001", SerialNumber: "SERIAL-BULK-001", Status: "active"},
			{ID: "bulk-asset-two", Name: "Engineering laptop two", AssetTag: "BULK-002", SerialNumber: "SERIAL-BULK-002"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].ModelID != model.ID || result.Items[0].Kind != "laptop" || result.Items[1].Status != "draft" {
		t.Fatalf("unexpected bulk result %#v", result)
	}
	loadedModel, err := service.GetModel(context.Background(), model.ID)
	if err != nil || loadedModel.InstanceCount != 2 {
		t.Fatalf("unexpected model count %#v err=%v", loadedModel, err)
	}
	for _, event := range auditor.events[1:] {
		if event.Action != "atlas.asset.created" || event.Metadata["creationMode"] != "model_bulk" ||
			event.Metadata["modelRequirementId"] != atlas.ModelRequirementID {
			t.Fatalf("unexpected bulk audit event %#v", event)
		}
	}
	if _, err := service.CreateAssetsFromModel(context.Background(), atlas.BulkCreateAssetsInput{
		ModelID: model.ID,
		Items: []atlas.CreateAssetInput{
			{ID: "bulk-asset-three", Name: "Should roll back", AssetTag: "BULK-003"},
			{ID: "bulk-asset-four", Name: "Existing identity", AssetTag: "bulk-001"},
		},
	}); !errors.Is(err, atlas.ErrConflict) {
		t.Fatalf("expected atomic conflict, got %v", err)
	}
	if _, err := service.GetAsset(context.Background(), "bulk-asset-three"); !errors.Is(err, atlas.ErrNotFound) {
		t.Fatalf("expected failed batch to leave no partial asset, got %v", err)
	}
}

func TestServiceFiltersAndGroupsModelInventory(t *testing.T) {
	service, err := atlas.NewService(repository.NewMemoryAtlasStore(), testReferenceValidator{}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: "example-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := service.CreateModel(context.Background(), atlas.CreateModelInput{
		ID: "inventory-model", Manufacturer: "Dell", Name: "PowerEdge", Kind: "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	const (
		siteOne       = "11111111111111111111111111111111"
		siteTwo       = "22222222222222222222222222222222"
		departmentOne = "33333333333333333333333333333333"
		userOne       = "44444444444444444444444444444444"
	)
	for _, input := range []atlas.CreateAssetInput{
		{ID: "inventory-one", ModelID: model.ID, Name: "Production one", Status: "active", Hostname: "prod-01.example.test", DeploymentNotes: "Rack 42", References: atlas.References{SiteID: siteOne, DepartmentID: departmentOne, UserID: userOne}},
		{ID: "inventory-two", ModelID: model.ID, Name: "Production two", Status: "active", Hostname: "prod-02.example.test", DeploymentNotes: "Rack 42", References: atlas.References{SiteID: siteOne, DepartmentID: departmentOne}},
		{ID: "inventory-three", ModelID: model.ID, Name: "Staging", Status: "draft", Hostname: "stage-01.example.test", References: atlas.References{SiteID: siteTwo}},
	} {
		if _, err := service.CreateAsset(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name       string
		query      atlas.ModelInventoryQuery
		filtered   int
		groupKey   string
		groupCount int
	}{
		{name: "lifecycle", query: atlas.ModelInventoryQuery{Status: "active", GroupBy: atlas.ModelInventoryGroupStatus}, filtered: 2, groupKey: "active", groupCount: 2},
		{name: "site", query: atlas.ModelInventoryQuery{SiteID: siteOne, GroupBy: atlas.ModelInventoryGroupSite}, filtered: 2, groupKey: siteOne, groupCount: 2},
		{name: "department", query: atlas.ModelInventoryQuery{DepartmentID: departmentOne, GroupBy: atlas.ModelInventoryGroupDepartment}, filtered: 2, groupKey: departmentOne, groupCount: 2},
		{name: "user", query: atlas.ModelInventoryQuery{UserID: userOne, GroupBy: atlas.ModelInventoryGroupUser}, filtered: 1, groupKey: userOne, groupCount: 1},
		{name: "deployment", query: atlas.ModelInventoryQuery{DeploymentContext: "RACK 42", GroupBy: atlas.ModelInventoryGroupDeployment}, filtered: 2, groupKey: "Rack 42", groupCount: 2},
		{name: "bounded details keep exact counts", query: atlas.ModelInventoryQuery{Status: "active", GroupBy: atlas.ModelInventoryGroupStatus, Limit: 1}, filtered: 2, groupKey: "active", groupCount: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory, err := service.GetModelInventory(context.Background(), model.ID, test.query)
			expectedItems := test.filtered
			if test.query.Limit > 0 && expectedItems > test.query.Limit {
				expectedItems = test.query.Limit
			}
			if err != nil || inventory.TotalCount != 3 || inventory.FilteredCount != test.filtered || len(inventory.Items) != expectedItems ||
				len(inventory.Groups) != 1 || inventory.Groups[0].Key != test.groupKey || inventory.Groups[0].Count != test.groupCount {
				t.Fatalf("unexpected model inventory %#v err=%v", inventory, err)
			}
		})
	}
}

func TestServiceRejectsInvalidInputsAndMissingReferences(t *testing.T) {
	service, err := atlas.NewService(repository.NewMemoryAtlasStore(), testReferenceValidator{reject: true}, foundation.NopAuditor{}, atlas.ServiceConfig{
		OrganizationID: "example-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{Name: "Invalid", Kind: "spaceship"}); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("expected invalid kind, got %v", err)
	}
	if _, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{
		Name: "Missing reference", Kind: "server", References: atlas.References{SiteID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}); !errors.Is(err, atlas.ErrReferenceMissing) {
		t.Fatalf("expected missing reference, got %v", err)
	}
	if _, err := service.ListAssets(context.Background(), atlas.Query{Limit: 101}); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("expected invalid limit, got %v", err)
	}
	if _, err := service.GetModelInventory(context.Background(), "model-one", atlas.ModelInventoryQuery{GroupBy: "building"}); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("expected invalid model inventory grouping, got %v", err)
	}
}

func TestServiceRequiresAllDependencies(t *testing.T) {
	if service, err := atlas.NewService(nil, testReferenceValidator{}, foundation.NopAuditor{}, atlas.ServiceConfig{OrganizationID: "org"}); err == nil || service != nil {
		t.Fatalf("expected missing store failure, service=%T err=%v", service, err)
	}
}
