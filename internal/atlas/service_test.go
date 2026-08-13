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
	if _, err := service.CreateAsset(context.Background(), atlas.CreateAssetInput{
		ID: "asset-one", ModelID: model.ID, Name: "Framework laptop", Kind: "laptop", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	models, err := service.ListModels(context.Background(), atlas.ModelQuery{Search: "framework"})
	if err != nil || len(models) != 1 || models[0].InstanceCount != 1 {
		t.Fatalf("unexpected models %#v err=%v", models, err)
	}
	now = now.Add(time.Hour)
	updated, err := service.UpdateModel(context.Background(), atlas.UpdateModelInput{
		ID: model.ID, Manufacturer: model.Manufacturer, Name: model.Name, ModelNumber: model.ModelNumber,
		Kind: model.Kind, VendorIdentifier: model.VendorIdentifier, Specifications: model.Specifications,
		SupportURL: model.SupportURL, WarrantyMonths: 48, UsefulLifeMonths: model.UsefulLifeMonths, Revision: model.Revision,
	})
	if err != nil || updated.Revision != 2 || updated.WarrantyMonths != 48 {
		t.Fatalf("unexpected updated model %#v err=%v", updated, err)
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
}

func TestServiceRequiresAllDependencies(t *testing.T) {
	if service, err := atlas.NewService(nil, testReferenceValidator{}, foundation.NopAuditor{}, atlas.ServiceConfig{OrganizationID: "org"}); err == nil || service != nil {
		t.Fatalf("expected missing store failure, service=%T err=%v", service, err)
	}
}
