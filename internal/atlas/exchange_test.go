package atlas_test

// Requirements: REQ-ATLAS-001, REQ-ATLAS-MODELS-001, REQ-EXCHANGE-001.
// Features: inventory.assets, inventory.models, migration.packages. GitHub: #9.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/maxlemke/stewardmesh/internal/atlas"
	"github.com/maxlemke/stewardmesh/internal/domain"
	"github.com/maxlemke/stewardmesh/internal/repository"
)

type atlasExchangeWriteGate struct {
	err   error
	calls []string
}

func (g *atlasExchangeWriteGate) CheckResourceWrite(_ context.Context, recordType, id string) error {
	g.calls = append(g.calls, recordType+":"+id)
	return g.err
}

func TestExchangeImporterPreservesAtlasStateAndBypassesOnlyItsPrivateWriteGate(t *testing.T) {
	now := time.Date(2024, time.March, 2, 15, 4, 5, 600_000_000, time.UTC)
	denied := errors.New("imported resource is write locked")
	writes := &atlasExchangeWriteGate{err: denied}
	auditor := &recordingAuditor{}
	service, importer, err := atlas.NewServiceWithExchangeImporter(
		repository.NewMemoryAtlasStore(), testReferenceValidator{}, writes, auditor,
		atlas.ServiceConfig{OrganizationID: "target-org", Now: func() time.Time { return now.Add(24 * time.Hour) }},
	)
	if err != nil {
		t.Fatal(err)
	}
	operation := atlas.ExchangeImportOperation{Token: "exchange-atlas-import", OccurredAt: now.Add(12 * time.Hour)}
	model := domain.AssetModel{
		ID: "model-imported", Manufacturer: "Acme", Name: "Edge 9000", Kind: "server",
		Specifications: map[string]string{"cpu": "16 core"}, WarrantyMonths: 48, UsefulLifeMonths: 72,
		Status: "retired", SourceSystemID: "legacy-cmdb", SourceRecordID: "model/9000",
		Revision: 7, CreatedAt: now, UpdatedAt: now.Add(6 * time.Hour),
	}
	result, err := importer.ImportModel(context.Background(), operation, model)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Atlas model: result=%#v err=%v", result, err)
	}
	loadedModel, err := service.GetModel(context.Background(), model.ID)
	if err != nil {
		t.Fatal(err)
	}
	model.OrganizationID = "target-org"
	if !reflect.DeepEqual(loadedModel, model) {
		t.Fatalf("import changed Atlas model\nwant=%#v\n got=%#v", model, loadedModel)
	}
	asset := domain.Asset{
		ID: "asset-imported", Name: "Imported edge node", Kind: "computer", AssetTag: "EDGE-001",
		Status: "active", Revision: 4, CreatedAt: now.Add(time.Hour), UpdatedAt: now.Add(8 * time.Hour),
	}
	result, err = importer.ImportAsset(context.Background(), operation, asset)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Atlas asset: result=%#v err=%v", result, err)
	}
	event := domain.AssetLifecycleEvent{
		ID: "0123456789abcdef0123456789abcdef", AssetID: asset.ID, FromStatus: "draft", ToStatus: "active",
		Note: "Deployment approved", Revision: 4, ActorID: "source-operator", OccurredAt: asset.UpdatedAt,
	}
	result, err = importer.ImportLifecycleEvent(context.Background(), operation, event)
	if err != nil || !result.Committed || !result.Created {
		t.Fatalf("import Atlas lifecycle event: result=%#v err=%v", result, err)
	}
	if len(writes.calls) != 0 {
		t.Fatalf("private Exchange importer consulted the ordinary write gate: %#v", writes.calls)
	}

	replayed, err := importer.ImportModel(context.Background(), operation, model)
	if err != nil || !replayed.Committed || replayed.Created {
		t.Fatalf("replay exact Atlas model: result=%#v err=%v", replayed, err)
	}
	if len(auditor.events) != 4 || auditor.events[0].ID != auditor.events[3].ID ||
		auditor.events[0].ActorID != "system:exchange" || auditor.events[0].CorrelationID != operation.Token ||
		!auditor.events[0].OccurredAt.Equal(operation.OccurredAt) {
		t.Fatalf("expected deterministic Exchange audit repair, events=%#v", auditor.events)
	}

	if _, err := service.UpdateModel(context.Background(), atlas.UpdateModelInput{ID: model.ID, Revision: model.Revision}); !errors.Is(err, denied) {
		t.Fatalf("ordinary imported model mutation bypassed the write gate: %v", err)
	}
	if _, err := service.UpdateAsset(context.Background(), atlas.UpdateAssetInput{ID: asset.ID, Revision: asset.Revision}); !errors.Is(err, denied) {
		t.Fatalf("ordinary imported asset mutation bypassed the write gate: %v", err)
	}
	wantCalls := []string{"atlas.model:model-imported", "atlas.asset:asset-imported"}
	if !reflect.DeepEqual(writes.calls, wantCalls) {
		t.Fatalf("unexpected canonical write-gate calls: got %#v want %#v", writes.calls, wantCalls)
	}

	invalidModel := domain.AssetModel{
		ID: "invalid-retired-model", Manufacturer: "Acme", Name: "Impossible retirement", Kind: "server",
		Status: "retired", Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := importer.ImportModel(context.Background(), operation, invalidModel); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("accepted a retired revision-one model: %v", err)
	}
	invalidAsset := domain.Asset{
		ID: "invalid-revision-one-asset", Name: "Impossible revision one", Kind: "server", Status: "active",
		Revision: 1, CreatedAt: now, UpdatedAt: now.Add(time.Minute),
	}
	if _, err := importer.ImportAsset(context.Background(), operation, invalidAsset); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("accepted a revision-one asset with different create/update timestamps: %v", err)
	}
	invalidEvent := event
	invalidEvent.ID = "fedcba9876543210fedcba9876543210"
	invalidEvent.OccurredAt = asset.UpdatedAt.Add(time.Minute)
	if _, err := importer.ImportLifecycleEvent(context.Background(), operation, invalidEvent); !errors.Is(err, atlas.ErrInvalidInput) {
		t.Fatalf("accepted lifecycle provenance after the asset's current state: %v", err)
	}
}
